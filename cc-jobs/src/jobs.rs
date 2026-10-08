use crate::notify::*;
use crate::*;
use regex::Regex;
use std::{
    collections::{BTreeMap, HashMap, HashSet},
    fs,
    io::{Read, Seek, SeekFrom},
    path::{Path, PathBuf},
    sync::{Arc, mpsc},
    thread,
};

#[derive(Clone)]
pub struct Context {
    pub root: PathBuf,
    pub jobs: PathBuf,
    pub home: PathBuf,
    pub codex_home: PathBuf,
    pub gto: PathBuf,
    pub executable: PathBuf,
    pub rt: SharedRuntime,
}
impl Context {
    pub fn installed() -> std::io::Result<Self> {
        let exe = std::env::current_exe()?;
        let rt: SharedRuntime = Arc::new(System::default());
        let env = rt.env();
        let home = PathBuf::from(
            env.get("USERPROFILE")
                .or(env.get("HOME"))
                .cloned()
                .unwrap_or_default(),
        );
        let root = exe.parent().unwrap().to_path_buf();
        Ok(Self {
            jobs: root.join("jobs"),
            codex_home: PathBuf::from(
                env.get("CODEX_HOME")
                    .filter(|s| !s.is_empty())
                    .cloned()
                    .unwrap_or(home.join(".codex").to_string_lossy().into()),
            ),
            gto: PathBuf::from(
                env.get("CODEX_JOBS_GTO_BIN")
                    .filter(|s| !s.is_empty())
                    .cloned()
                    .unwrap_or(home.join(".gto/bin/gto.exe").to_string_lossy().into()),
            ),
            root,
            home,
            executable: exe,
            rt,
        })
    }
    pub fn job_dir(&self, jid: &str) -> PathBuf {
        self.jobs.join(jid)
    }
    pub fn job_lock(&self, jid: &str, name: &str) -> std::io::Result<FileLock> {
        let path = self.job_dir(jid).join(name);
        FileLock::acquire(&path).map_err(|e| {
            std::io::Error::new(
                e.kind(),
                format!(
                    "acquire exclusive lock {} byte range [0, 1): {e}",
                    path.display()
                ),
            )
        })
    }
    /// Atomic write: readers see either the old or the new file, never half of one.
    pub fn write_json(&self, path: &Path, v: &Value) -> Result<()> {
        self.rt
            .before_save(path, v)
            .map_err(|e| Error::file_io("prepare write", path, e))?;
        let tmp = path.with_file_name(format!(
            "{}.{}.tmp",
            path.file_name().unwrap().to_string_lossy(),
            self.rt.pid()
        ));
        let until = std::time::Instant::now() + std::time::Duration::from_secs(1);
        let bytes = pretty(v).replace("\n", "\r\n");
        for i in 0..50 {
            match fs::write(&tmp, &bytes) {
                Ok(()) => break,
                Err(e)
                    if transient_file_access(&e) && i < 49 && std::time::Instant::now() < until =>
                {
                    self.rt.sleep(0.02)
                }
                Err(e) => {
                    let transient = transient_file_access(&e);
                    let mut error = Error::file_io("write temporary snapshot", &tmp, e);
                    if transient {
                        error.code = 75;
                    }
                    return Err(error);
                }
            }
        }
        let until = std::time::Instant::now() + std::time::Duration::from_secs(1);
        for i in 0..50 {
            match self.rt.replace(&tmp, path) {
                Ok(()) => return Ok(()),
                Err(e)
                    if transient_file_access(&e) && i < 49 && std::time::Instant::now() < until =>
                {
                    self.rt.sleep(0.02)
                }
                Err(e) => {
                    let code = if transient_file_access(&e) { 75 } else { 1 };
                    return Err(Error {
                        code,
                        message: format!("replace {} -> {}: {e}", tmp.display(), path.display()),
                    });
                }
            }
        }
        unreachable!()
    }
    pub fn read_json(&self, path: &Path) -> Result<Value> {
        let bytes = self
            .read_file(path)
            .map_err(|e| Error::file_io("read entire file", path, e))?;
        serde_json::from_slice(&bytes)
            .map_err(|e| Error::new(format!("parse JSON {}: {e}", path.display())))
    }
    /// JSON readers never take state.lock. Writers replace complete snapshots.
    /// Retry only transient access failures, with a one-second absolute budget.
    pub fn read_file(&self, path: &Path) -> std::io::Result<Vec<u8>> {
        let until = std::time::Instant::now() + std::time::Duration::from_secs(1);
        for i in 0..50 {
            match self.rt.before_load(path).and_then(|_| fs::read(path)) {
                Ok(b) => return Ok(b),
                Err(e)
                    if transient_file_access(&e) && i < 49 && std::time::Instant::now() < until =>
                {
                    self.rt.sleep(0.02)
                }
                Err(e) if transient_file_access(&e) => {
                    return Err(std::io::Error::new(
                        std::io::ErrorKind::TimedOut,
                        format!("read retry budget exhausted: {e}"),
                    ));
                }
                Err(e) => return Err(e),
            }
        }
        unreachable!()
    }
    pub fn save(&self, job: &Value) -> Result<()> {
        self.write_json(&self.job_dir(s(job, "id")).join("job.json"), job)
    }
    /// job.json as a dict, or None if it is missing, deleted meanwhile or broken.
    pub fn try_load(&self, jid: &str) -> Option<Value> {
        self.read_json(&self.job_dir(jid).join("job.json"))
            .ok()
            .filter(|v| v.is_object() && v.get("id").is_some() && v.get("status").is_some())
    }
    pub fn load(&self, jid: &str) -> Result<Value> {
        let path = self.job_dir(jid).join("job.json");
        let job = self.read_json(&path).map_err(|error| Error {
            code: error.code,
            message: format!(
                "no such job (or unreadable job.json): {jid}; {}",
                error.message
            ),
        })?;
        if !job.is_object() || job.get("id").is_none() || job.get("status").is_none() {
            return Err(Error::new(format!("invalid job record {}", path.display())));
        }
        Ok(job)
    }
    pub fn list_jobs(&self, skip: &HashSet<String>) -> Vec<Value> {
        let mut jobs = vec![];
        if let Ok(entries) = fs::read_dir(&self.jobs) {
            for entry in entries.flatten() {
                let name = entry.file_name().to_string_lossy().into_owned();
                if skip.contains(&name) {
                    continue;
                }
                if let Some(j) = self.try_load(&name) {
                    jobs.push(j);
                }
            }
        }
        jobs.sort_by(|a, b| s(a, "created_at").cmp(s(b, "created_at")));
        jobs
    }
    /// True if job[key] ('pid' or 'codex_pid') is still the very process we started.
    pub fn is_ours(&self, j: &Value, key: &str) -> bool {
        let Some(pid) = j[key].as_u64().and_then(|x| u32::try_from(x).ok()) else {
            return false;
        };
        let created = self.rt.proc_created(pid);
        created.is_some() && created == j[format!("{key}_created")].as_u64()
    }
    pub fn supervisor_alive(&self, j: &Value) -> bool {
        // An intentional replacement is durable queue work, not an orphan.
        if s(j, "status") == "queued" && j["queue_restart_pending"] == true {
            return true;
        }
        if truth(&j["pid"]) {
            self.is_ours(j, "pid")
        } else {
            parse_iso(if s(j, "queued_at").is_empty() {
                s(j, "created_at")
            } else {
                s(j, "queued_at")
            })
            .is_some_and(|created| self.rt.epoch() - created <= QUEUED_GRACE_S)
        }
    }
    /// Mark a live job whose supervisor is gone as 'lost' (persisted, so it can never come back).
    pub fn refresh(&self, mut job: Value, auto_observe: bool) -> Result<Value> {
        let pause = self.job_dir(s(&job, "id")).join("yield-paused.json");
        if self.tool_handoff_pending(&job) {
            let _gate = self.queue_lock()?;
            if let Err(error) = self.recover_tool_yield(&job) {
                self.yield_diagnostic(&error);
            }
            job = self.load(s(&job, "id"))?;
        }
        if !self.tool_handoff_pending(&job)
            && self.read_json(&pause).is_ok_and(|v| v["complete"] == false)
        {
            let _gate = self.queue_lock()?;
            self.rt.recover_paused(&pause)?;
        }
        if LIVE.contains(&s(&job, "status")) && !self.supervisor_alive(&job) {
            let fresh = {
                let _state = match self.job_lock(s(&job, "id"), "state.lock") {
                    Ok(lock) => lock,
                    Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(job),
                    Err(e) => return Err(e.into()),
                };
                self.refresh_locked(job, false)?
            };
            // Rating saves acquire state.lock independently.
            return self.refresh_locked(fresh, auto_observe);
        }
        self.refresh_locked(job, auto_observe)
    }
    pub(crate) fn refresh_locked(&self, mut job: Value, auto_observe: bool) -> Result<Value> {
        if !LIVE.contains(&s(&job, "status")) {
            if auto_observe && TERMINAL.contains(&s(&job, "status")) && !truth(&job["gto"]) {
                let last = parse_iso(s(&job, "gto_auto_attempt_at"));
                if last.is_none_or(|t| self.rt.epoch() - t >= 60.) {
                    self.auto_rate(&mut job, None);
                }
            }
            return Ok(job);
        }
        if self.supervisor_alive(&job) {
            return Ok(job);
        }
        let Some(mut fresh) = self.try_load(s(&job, "id")) else {
            return Ok(job);
        };
        if LIVE.contains(&s(&fresh, "status")) && !self.supervisor_alive(&fresh) {
            if self.tool_handoff_pending(&fresh) {
                return Ok(fresh);
            }
            fresh["status"] = json!("lost");
            fresh["finished_at"] = json!(self.rt.now());
            if !truth(&fresh["last_error"]) {
                fresh["last_error"] = json!("supervisor exited without recording a result");
            }
            self.save(&fresh)?;
            self.close_chrome_tabs(s(&fresh, "id"));
            if auto_observe {
                self.auto_rate(&mut fresh, None);
            }
        }
        Ok(fresh)
    }
    pub fn elapsed_s(&self, j: &Value) -> Option<f64> {
        if let Some(ms) = j["duration_ms"].as_f64() {
            return Some(ms / 1000.);
        }
        let start = parse_iso(if truth(&j["started_at"]) {
            s(j, "started_at")
        } else {
            s(j, "created_at")
        })?;
        Some(parse_iso(s(j, "finished_at")).unwrap_or(self.rt.epoch()) - start)
    }
    /// Seconds since codex last showed any sign of life (running jobs only).
    pub fn idle_s(&self, j: &Value) -> Option<f64> {
        if s(j, "status") != "running" {
            return None;
        }
        parse_iso(s(j, "last_event_at")).map(|t| self.rt.epoch() - t)
    }
    /// codex's own rollout log for this thread: sessions/YYYY/MM/DD (local start date)/rollout-*-<thread>.jsonl.
    pub fn session_file(&self, j: &Value) -> Option<PathBuf> {
        let start = chrono::DateTime::parse_from_rfc3339(if s(j, "started_at").is_empty() {
            ""
        } else {
            s(j, "started_at")
        })
        .ok()
        .unwrap_or_else(|| chrono::DateTime::parse_from_rfc3339(&self.rt.now()).unwrap());
        for offset in [0, 1] {
            let day = start + chrono::Duration::days(offset);
            let dir = self
                .codex_home
                .join("sessions")
                .join(day.format("%Y/%m/%d").to_string());
            if let Some(p) = find_session(&dir, s(j, "thread_id"), false) {
                return Some(p);
            }
        }
        ["thread_id", "resume_session"].iter().find_map(|key| {
            normalize_uuid(s(j, key))
                .and_then(|id| find_session(&self.codex_home.join("sessions"), &id, true))
        })
    }
    /// Read what codex appended to its session log since last time. `codex exec --json` reports usage only
    /// at the very end, the session log has it after every model call. Returns True if job changed.
    pub fn poll_session(&self, j: &mut Value, sess: &mut Session) -> Result<bool> {
        if s(j, "thread_id").is_empty() {
            return Ok(false);
        }
        if sess.path.is_none() {
            sess.path = self.session_file(j);
            if sess.path.is_none() {
                return Ok(false);
            }
        }
        let data = read_from(sess.path.as_ref().unwrap(), sess.pos)?;
        let Some(end) = data.iter().rposition(|c| *c == b'\n') else {
            return Ok(false);
        };
        sess.pos += end as u64 + 1;
        j["last_event_at"] = json!(self.rt.now());
        for raw in data[..=end].split(|c| *c == b'\n') {
            if raw.windows(13).any(|s| s == b"\"token_count\"")
                && let Ok(v) = serde_json::from_slice::<Value>(raw)
            {
                apply_token_count(j, &v["payload"]);
            }
        }
        Ok(true)
    }
    pub fn fmt_table(&self, jobs: &[Value]) -> String {
        let head = [
            "ID",
            "NAME",
            "MODEL/EFFORT",
            "STATE",
            "ELAPSED",
            "IDLE",
            "STEPS",
            "TOKENS",
            "ACTIVITY",
            "CWD",
        ];
        let mut rows = vec![head.iter().map(|s| s.to_string()).collect::<Vec<_>>()];
        for j in jobs {
            rows.push(vec![
                s(j, "id").into(),
                // JSON quoting keeps spaces, tabs and newlines within one cell.
                serde_json::to_string(s(j, "name")).unwrap(),
                format!("{}/{}", getstr(j, "model"), getstr(j, "effort")),
                crate::queue::display_state(j).into(),
                fmt_elapsed(self.elapsed_s(j)),
                fmt_elapsed(self.idle_s(j)),
                number(j, "steps").to_string(),
                fmt_tokens(number(&j["tokens"], "total")),
                short(s(j, "activity"), 60),
                tail_path(s(j, "cwd"), 40),
            ]);
        }
        let widths: Vec<_> = (0..head.len())
            .map(|i| rows.iter().map(|r| width(&r[i])).max().unwrap())
            .collect();
        rows.iter()
            .map(|r| {
                r.iter()
                    .zip(&widths)
                    .map(|(c, w)| format!("{c}{}", " ".repeat(w - width(c))))
                    .collect::<Vec<_>>()
                    .join("  ")
                    .trim_end()
                    .to_string()
            })
            .collect::<Vec<_>>()
            .join("\n")
    }
    pub fn summary(&self, j: &Value) -> String {
        let mut line = format!(
            "== {} {} {}/{} {}, {} steps, {} tokens",
            s(j, "id"),
            crate::queue::display_state(j),
            getstr(j, "model"),
            getstr(j, "effort"),
            fmt_elapsed(self.elapsed_s(j)),
            number(j, "steps"),
            comma(number(&j["tokens"], "total"))
        );
        if !j["context_pct"].is_null() {
            line += &format!(", ctx {}%", pystr(&j["context_pct"]));
        }
        if LIVE.contains(&s(j, "status")) {
            if let Some(idle) = self.idle_s(j) {
                line += &format!(", idle {}", fmt_elapsed(Some(idle)));
            }
            line += &format!(" | {}", getstr(j, "activity"));
        } else if s(j, "status") != "succeeded" && truth(&j["last_error"]) {
            line += &format!(" | {}", getstr(j, "last_error"));
        }
        line
    }
    pub fn watch_line(&self, j: &Value) -> String {
        let detail = if ["failed", "lost"].contains(&s(j, "status")) {
            s(j, "last_error")
        } else {
            s(j, "activity")
        };
        let now = self.rt.now();
        format!(
            "{} {} {} {}/{} {} {} tok | {}",
            now.get(11..19).unwrap_or(""),
            s(j, "id"),
            crate::queue::display_state(j),
            getstr(j, "model"),
            getstr(j, "effort"),
            fmt_elapsed(self.elapsed_s(j)),
            fmt_tokens(number(&j["tokens"], "total")),
            short(detail, 150)
        )
    }
    /// One poll: returns lines for jobs whose state changed (queued is not worth a notification).
    pub fn watch_step(&self, seen: &mut HashMap<String, String>) -> Result<Vec<String>> {
        let skip = seen
            .iter()
            .filter(|(_, st)| !LIVE.contains(&st.as_str()))
            .map(|(id, _)| id.clone())
            .collect();
        let mut lines = vec![];
        for j in self.list_jobs(&skip) {
            let j = self.refresh(j, true)?;
            if seen.get(s(&j, "id")).is_none_or(|st| st != s(&j, "status")) {
                seen.insert(s(&j, "id").into(), s(&j, "status").into());
                if s(&j, "status") != "queued" {
                    lines.push(self.watch_line(&j));
                }
            }
        }
        Ok(lines)
    }
    pub fn ao_executable(&self) -> Option<String> {
        ao_executable(&self.root, &self.home, self.rt.as_ref())
    }
    pub fn gto_payload(
        &self,
        j: &Value,
        quality: i64,
        task_type: Option<&str>,
        notes: Option<&str>,
    ) -> Value {
        json!({"kind":"model_fit_observation","model":j["model"],"reasoning_effort":j["effort"],"task_type":task_type.filter(|s|!s.is_empty()).unwrap_or(if s(j,"task_type").is_empty(){"general"}else{s(j,"task_type")}),"duration_ms":self.elapsed_s(j).map(|s|(s*1000.) as i64),"tokens":number(&j["tokens"],"total"),"result_quality":quality,"notes":notes.filter(|s|!s.is_empty()).unwrap_or(s(j,"notes_hint")),"source":"codex-jobs"})
    }
    /// Read only judgement metadata, using the container's environment without exposing credentials.
    pub fn jev_evaluations(
        &self,
        thread_id: Option<&str>,
        since: Option<&str>,
    ) -> HashMap<String, String> {
        let cond = if let Some(id) = thread_id {
            let Some(id) = normalize_uuid(id) else {
                return HashMap::new();
            };
            let suffix = format!("{id}.jsonl");
            format!(
                "right(replace(session_path, chr(92), '/'), {}) = '{suffix}'",
                suffix.len()
            )
        } else if let Some(since) = since {
            format!("completed_at >= '{since}'::timestamptz")
        } else {
            "TRUE".into()
        };
        let sql = format!(
            "SELECT coalesce(json_agg(t), '[]'::json) FROM (SELECT DISTINCT ON (session_path) session_path, verdict FROM turn_evaluations WHERE {cond} ORDER BY session_path, completed_at DESC, evaluated_at DESC) t;"
        );
        let script = r#"exec psql -X -w -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A -v ON_ERROR_STOP=1 -c "$1""#;
        let call = Call::new(
            vec![
                "docker".into(),
                "exec".into(),
                "godd-ao-postgres".into(),
                "sh".into(),
                "-c".into(),
                script.into(),
                "sh".into(),
                sql,
            ],
            vec![],
            10.,
        );
        let mut out = HashMap::new();
        let rows = self
            .rt
            .call(call)
            .ok()
            .filter(|r| r.code == 0)
            .and_then(|r| serde_json::from_slice::<Vec<Value>>(&r.stdout).ok());
        if let Some(rows) = rows {
            let re = Regex::new(r"([0-9a-fA-F-]{36})\.jsonl$").unwrap();
            for row in rows {
                if let Some(c) = re.captures(s(&row, "session_path"))
                    && let Some(id) = normalize_uuid(&c[1])
                {
                    out.insert(id, s(&row, "verdict").into());
                }
            }
        } else {
            self.rt
                .diagnostic("auto evaluation unavailable: RuntimeError");
        }
        out
    }
    pub fn send_gto(&self, payload: &Value) -> bool {
        if !self.gto.is_file() {
            self.rt
                .diagnostic(&format!("gto not found at {}; skipped", self.gto.display()));
            return false;
        }
        match self.rt.call(Call::new(
            vec![
                self.gto.to_string_lossy().into(),
                "internal".into(),
                "ingest-turn".into(),
            ],
            compact(payload).into_bytes(),
            20.,
        )) {
            Ok(r) if r.code == 0 => true,
            Ok(r) => {
                self.rt
                    .diagnostic(&format!("gto ingest-turn failed ({})", r.code));
                false
            }
            Err(e) => {
                self.rt.diagnostic(&format!(
                    "gto ingest-turn failed: {}",
                    if e.kind() == std::io::ErrorKind::TimedOut {
                        "TimeoutExpired"
                    } else {
                        "OSError"
                    }
                ));
                false
            }
        }
    }
    pub fn store_rating(&self, job: &mut Value, payload: Value, automatic: bool) -> Result<()> {
        let _lock = self.job_lock(s(job, "id"), "state.lock")?;
        let mut fresh = self.load(s(job, "id"))?;
        if truth(&fresh["gto"]) {
            let old = fresh["gto"].clone();
            if fresh.get("gto_history").is_none() {
                fresh["gto_history"] = json!([]);
            }
            fresh["gto_history"].as_array_mut().unwrap().push(old);
        }
        fresh["gto"] = json!({"sent_at":self.rt.now(),"payload":payload,"automatic":automatic});
        fresh.as_object_mut().unwrap().remove("gto_auto_pending");
        fresh.as_object_mut().unwrap().remove("gto_auto_error");
        self.save(&fresh)?;
        job.as_object_mut()
            .unwrap()
            .extend(fresh.as_object().unwrap().clone()); // dict.update retains stale keys, as in Python.
        Ok(())
    }
    /// Best effort, once per terminal job. A failed send remains pending for status/backfill to retry.
    /// `evaluation`: None queries Jev; Some(None) explicitly supplies no evaluation.
    pub fn auto_rate(&self, job: &mut Value, evaluation: Option<Option<&str>>) -> bool {
        if !TERMINAL.contains(&s(job, "status")) || truth(&job["gto"]) {
            return false;
        }
        let result = (|| -> Result<bool> {
            let _gto = self.job_lock(s(job, "id"), "gto.lock")?;
            let mut fresh = self.load(s(job, "id"))?;
            if truth(&fresh["gto"]) || !TERMINAL.contains(&s(&fresh, "status")) {
                job.as_object_mut()
                    .unwrap()
                    .extend(fresh.as_object().unwrap().clone());
                return Ok(false);
            }
            let evaluations =
                if evaluation.is_none() && !s(&fresh, "thread_id").is_empty() && self.gto.is_file()
                {
                    self.jev_evaluations(Some(s(&fresh, "thread_id")), None)
                } else {
                    HashMap::new()
                };
            let ev = evaluation
                .unwrap_or_else(|| evaluations.get(s(&fresh, "thread_id")).map(String::as_str));
            let (q, n) = auto_quality(&fresh, ev);
            let payload = self.gto_payload(&fresh, q, None, Some(&n));
            if self.send_gto(&payload) {
                self.store_rating(job, payload, true)?;
                return Ok(true);
            }
            let _state = self.job_lock(s(job, "id"), "state.lock")?;
            fresh = self.load(s(job, "id"))?;
            fresh["gto_auto_pending"] = json!(true);
            fresh["gto_auto_attempt_at"] = json!(self.rt.now());
            fresh["gto_auto_error"] = json!("ingest unavailable");
            self.save(&fresh)?;
            job.as_object_mut()
                .unwrap()
                .extend(fresh.as_object().unwrap().clone());
            Ok(false)
        })();
        match result {
            Ok(b) => b,
            Err(_) => {
                self.rt.diagnostic("auto rating failed: OSError");
                false
            }
        }
    }
    pub fn job_environment(&self) -> BTreeMap<String, String> {
        let mut env = self.rt.env();
        crate::cache::environment(&mut env);
        for name in [
            "CODEX_JOBS_DISCORD_WEBHOOK",
            "GODD_DISCORD_BOT_TOKEN",
            "GODD_DISCORD_WEBHOOK_URL",
            "DISCORD_WEBHOOK_URL",
        ] {
            env.retain(|key, _| !key.eq_ignore_ascii_case(name));
        }
        let slots = crate::gate::slots(&env).unwrap_or(3);
        env.insert(
            "CARGO_BUILD_JOBS".into(),
            crate::gate::jobs_limit(self.rt.cores(), slots).to_string(),
        );
        if let Some(real) = self.rt.which("cargo.exe") {
            env.insert("CODEX_JOBS_REAL_CARGO".into(), real);
            env.insert(
                "CODEX_JOBS_CARGO_GATE".into(),
                self.root.join("cargo_gate.exe").to_string_lossy().into(),
            );
            let old = env
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case("PATH"))
                .map(|(_, v)| v.clone())
                .unwrap_or_default();
            env.retain(|k, _| !k.eq_ignore_ascii_case("PATH"));
            env.insert(
                "PATH".into(),
                format!("{};{old}", self.root.join("bin").display()),
            );
        }
        env
    }
    pub fn close_chrome_tabs(&self, jid: &str) {
        match cc_chrome::sweep(&self.jobs, Some(jid), false) {
            Ok(report) => {
                if self.job_dir(jid).join("chrome-tabs").exists() {
                    let _ = self.write_json(&self.job_dir(jid).join("chrome-cleanup.json"), &json!({"closed":report.closed,"gone":report.gone,"kept":report.kept,"failed":report.failed,"deferred":report.deferred,"at":self.rt.now()}));
                }
                if report.failed != 0 || report.deferred {
                    self.rt.diagnostic(&format!(
                        "Chrome cleanup deferred to minute sweep: {report:?}"
                    ));
                }
            }
            Err(e) => self
                .rt
                .diagnostic(&format!("Chrome cleanup deferred to minute sweep: {e}")),
        }
    }
    pub fn supervise(&self, jid: &str) -> Result<()> {
        // A duplicate supervisor cannot start the same job while another waits.
        let owner_path = self.job_dir(jid).join("supervisor.lock");
        let _owner = FileLock::try_acquire(&owner_path).map_err(|e| {
            Error::file_io("acquire exclusive lock byte range [0, 1)", &owner_path, e)
        })?;
        if let Err(error) = self.supervise_owned(jid) {
            let mut job = self.load(jid)?;
            if s(&job, "status") == "queued" && job["cancel_requested"] != true {
                job["last_error"] = json!(format!(
                    "supervisor error: {}",
                    redact_local_log_secrets(&error.message, &self.rt.env())
                ));
                finish(&mut job, None, 0, &self.rt.now());
                self.save(&job)?;
                self.close_chrome_tabs(jid);
                return Ok(());
            }
            return Err(error);
        }
        Ok(())
    }
    fn supervise_owned(&self, jid: &str) -> Result<()> {
        {
            let _state = self.job_lock(jid, "state.lock")?;
            let mut job = self.load(jid)?;
            if s(&job, "status") != "queued" || job["cancel_requested"] == true {
                return Ok(());
            }
            job["pid"] = json!(self.rt.pid());
            job["pid_created"] = json!(self.rt.proc_created(self.rt.pid()));
            job["queue_supervisor_version"] = json!(5);
            job["queue_restart_pending"] = json!(false);
            job["queue_dependency_version"] = json!(1);
            job["last_event_at"] = json!(self.rt.now());
            job["activity"] = json!(if truth(&job["retry_count"]) {
                s(&job, "activity")
            } else {
                "順番待ち"
            });
            self.save(&job)?;
        }
        // Recovered supervisors may start with tabs from a yielded or rate-limited worker.
        self.close_chrome_tabs(jid);
        let mut next_diagnostic = 0.;
        let mut next_heartbeat = self.rt.mono() + 60.;
        let mut had_admission_error = false;
        let mut next_gated_check = 0.;
        loop {
            if self.rt.mono() >= next_heartbeat {
                let heartbeat = (|| -> Result<()> {
                    let _state = self.job_lock(jid, "state.lock")?;
                    let mut waiting = self.load(jid)?;
                    if s(&waiting, "status") == "queued" && waiting["cancel_requested"] != true {
                        waiting["last_event_at"] = json!(self.rt.now());
                        self.save(&waiting)?;
                    }
                    Ok(())
                })();
                if let Err(e) = heartbeat {
                    self.rt.diagnostic(&format!(
                        "順番待ちの生存記録を再試行: {}",
                        redact_local_log_secrets(&e.message, &self.rt.env())
                    ));
                }
                next_heartbeat = self.rt.mono() + 60.;
            }
            let job = match self.load(jid) {
                Ok(job) => job,
                Err(error) if error.code == 75 => {
                    if self.rt.mono() >= next_diagnostic {
                        self.rt.diagnostic(&format!(
                            "順番待ちの状態読取りを再試行: {}",
                            redact_local_log_secrets(&error.message, &self.rt.env())
                        ));
                        next_diagnostic = self.rt.mono() + 60.;
                    }
                    self.rt.sleep(3.);
                    continue;
                }
                Err(error) => return Err(error),
            };
            if s(&job, "status") != "queued" || job["cancel_requested"] == true {
                return Ok(());
            }
            if job["retry_not_before"]
                .as_f64()
                .is_some_and(|t| t > self.rt.epoch())
            {
                self.rt.sleep(3.);
                continue;
            }
            // Most polls end in "keep waiting". Decide that from a lock-free
            // snapshot and take the global gate only when this job may start,
            // its wait reason changed, or the periodic gated pass is due (that
            // pass also runs yield maintenance). Holding the gate for every
            // poll of every waiter starved `run` past its 60 s lock wait.
            if !had_admission_error
                && self.rt.mono() < next_gated_check
                && self
                    .queue_config()
                    .is_ok_and(|cfg| self.admission_wait_unchanged(&job, &cfg))
            {
                self.rt.sleep(3.);
                continue;
            }
            next_gated_check = self.rt.mono() + jitter(45., 90.);
            let admission = self
                .queue_config()
                .and_then(|cfg| self.admit_configured(jid, &cfg).map(|state| (cfg, state)));
            let (cfg, state) = match admission {
                Ok((cfg, Some(state))) => (cfg, state),
                Ok((_, None)) => {
                    // A successful assessment supersedes an earlier lock error.
                    if had_admission_error {
                        self.record_admission_error(jid, None);
                        had_admission_error = false;
                    }
                    self.rt.sleep(3.);
                    continue;
                }
                Err(error) => {
                    self.record_admission_error(jid, Some(&error));
                    had_admission_error = true;
                    // Settings being saved and lock contention must never fail
                    // waiting jobs. Keep them queued until admission works.
                    if self.rt.mono() >= next_diagnostic {
                        self.rt.diagnostic(&format!(
                            "順番待ちを継続: {}",
                            redact_local_log_secrets(&error.message, &self.rt.env())
                        ));
                        next_diagnostic = self.rt.mono() + 60.;
                    }
                    self.rt.sleep(3.);
                    continue;
                }
            };
            if !self.supervise_attempt(jid, state, &cfg)? {
                return Ok(());
            }
        }
    }
    fn supervise_attempt(
        &self,
        jid: &str,
        state: FileLock,
        cfg: &crate::queue::QueueConfig,
    ) -> Result<bool> {
        let dir = self.job_dir(jid);
        let mut job = self.load(jid)?;
        let t0 = self.rt.mono();
        let mut proc: Option<Box<dyn Running>> = None;
        let mut notifier = None;
        let mut exit = None;
        let mut cleanup_failed = false;
        let mut tool_yielded = false;
        #[cfg(windows)]
        let mut chrome_proxy = None;
        let work = (|| -> Result<()> {
            let lock = state;
            job = self.load(jid)?;
            if s(&job, "status") != "running" || job["cancel_requested"] == true {
                return Ok(());
            }
            let me = self.rt.pid();
            job["status"] = json!("running");
            job["queue_fairness_version"] = json!(3);
            job["pid"] = json!(me);
            job["pid_created"] = json!(self.rt.proc_created(me));
            job["started_at"] = json!(self.rt.now());
            if !truth(&job["first_started_at"]) {
                job["first_started_at"] = job["started_at"].clone();
            }
            job["last_event_at"] = json!(self.rt.now());
            job["activity"] = json!("starting");
            job["attempt_events_offset"] = json!(
                fs::metadata(dir.join("events.jsonl"))
                    .map(|m| m.len())
                    .unwrap_or(0)
            );
            self.save(&job)?;
            #[cfg(windows)]
            if s(&job, "context_phase") != "summarize" {
                cc_chrome::install_plugin_hook(&self.home)?;
                let proxy = cc_chrome::native::start(&dir, jid)?;
                job["chrome_pipe"] = json!(proxy.pipe);
                chrome_proxy = Some(proxy);
            }
            let mut environment = self.job_environment();
            environment.insert("CC_JOB_ID".into(), jid.into());
            environment.insert("CODEX_JOB_ID".into(), jid.into());
            if !s(&job, "chrome_pipe").is_empty() {
                environment.insert(
                    "CDP_BROWSER_BACKEND_PIPE_PATH".into(),
                    s(&job, "chrome_pipe").into(),
                );
                environment.insert(
                    "BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET".into(),
                    "true".into(),
                );
            }
            proc = Some(self.rt.spawn(&Spawn {
                argv: codex_args(&job, &dir),
                prompt: dir.join(if s(&job, "context_phase") == "summarize" {
                    "context-summary-prompt.md"
                } else if s(&job, "context_phase") == "continue" && job["yield_resume"] != true {
                    "context-continue.md"
                } else if job["yield_resume"] == true {
                    "yield-resume.md"
                } else {
                    "prompt.md"
                }),
                cwd: PathBuf::from(s(&job, "cwd")),
                env: environment,
                flags: NO_WINDOW,
            })?);
            let p = proc.as_mut().unwrap();
            job["codex_pid"] = json!(p.pid());
            job["codex_pid_created"] = json!(self.rt.proc_created(p.pid()));
            self.save(&job)?;
            drop(lock);
            let mut n = TaskProgress::new(self.ao_executable(), self.jobs.clone(), self.rt.clone());
            n.start(&job);
            notifier = Some(n);
            let (qtx, qrx) = mpsc::channel();
            let (etx, erx) = mpsc::channel();
            let mut readers = Vec::new();
            if let Some(stdout) = p.stdout() {
                let path = dir.join("events.jsonl");
                let rt = self.rt.clone();
                let etx = etx.clone();
                readers.push(thread::spawn(move || {
                    if let Err(e) = pump(stdout, &path, rt.as_ref(), |ev| {
                        let _ = qtx.send(ev);
                    }) {
                        let _ = etx.send(e);
                    }
                }));
            }
            if let Some(stderr) = p.stderr() {
                let path = dir.join("stderr.log");
                let rt = self.rt.clone();
                let etx = etx.clone();
                readers.push(thread::spawn(move || {
                    if let Err(e) = pump_stderr(stderr, &path, rt.as_ref()) {
                        let _ = etx.send(e);
                    }
                }));
            }
            let mut sess = Session::default();
            let mut next_poll = 0.;
            let mut exited_at = None;
            let mut next_yield_check = 0.;
            loop {
                let ev = match qrx.recv_timeout(std::time::Duration::from_millis(500)) {
                    Ok(ev) => Some(ev),
                    Err(mpsc::RecvTimeoutError::Timeout) => None,
                    Err(mpsc::RecvTimeoutError::Disconnected) => {
                        self.rt.sleep(0.5);
                        None
                    }
                };
                let had_event = ev.is_some();
                let mut changed = had_event;
                // The rollout usage can arrive while a long tool is running.
                // Refresh before deciding at its completion, not after the agent
                // has already started its next tool. This also reads resumed logs.
                let completed_tool = ev.as_ref().is_some_and(|ev| {
                    s(ev, "type") == "item.completed"
                        && TOOL_KINDS.contains(&s(&ev["item"], "type"))
                });
                if self.rt.mono() >= next_poll || completed_tool {
                    next_poll = self.rt.mono() + SESSION_POLL_S;
                    match self.poll_session(&mut job, &mut sess) {
                        Ok(b) => changed |= b,
                        Err(e) => self.rt.diagnostic(&format!(
                            "session log: {}",
                            redact_local_log_secrets(&e.message, &self.rt.env())
                        )),
                    }
                }
                if let Some(ev) = ev {
                    job["last_event_at"] = ev["_ts"].clone();
                    apply_event(&mut job, &ev);
                    if s(&ev, "type") == "item.completed"
                        && TOOL_KINDS.contains(&s(&ev["item"], "type"))
                        && (self.context_limit_reached(&job)
                            || parse_iso(s(&job, "started_at"))
                                .is_some_and(|at| self.rt.epoch() - at >= 7200.))
                    {
                        self.save(&job)?;
                        let attempt = (|| -> Result<bool> {
                            // Skip the global gate unless a yield is plausible.
                            if !self.yield_may_be_due(&job) {
                                return Ok(false);
                            }
                            let _gate = self.queue_lock()?;
                            self.yield_tool_boundary(&job, &self.admission_jobs()?)
                        })();
                        match attempt {
                            Ok(true) => {
                                tool_yielded = true;
                                break;
                            }
                            Ok(false) => (),
                            Err(error) => {
                                self.yield_diagnostic(&error);
                            }
                        }
                    }
                }
                if changed {
                    self.save(&job)?;
                }
                if !had_event
                    && self.rt.mono() >= next_yield_check
                    && (self.context_limit_reached(&job)
                        || parse_iso(s(&job, "started_at"))
                            .is_some_and(|at| self.rt.epoch() - at >= 7200.))
                {
                    next_yield_check = self.rt.mono() + 3.;
                    let attempt = (|| -> Result<bool> {
                        // Skip the global gate unless a yield is plausible.
                        if !self.yield_may_be_due(&job) {
                            return Ok(false);
                        }
                        let _gate = self.queue_lock()?;
                        if self.tool_handoff_pending(&job) {
                            self.recover_tool_yield(&self.load(jid)?)?;
                            return Ok(s(&self.load(jid)?, "status") == "queued");
                        }
                        self.yield_tool_boundary(&job, &self.admission_jobs()?)
                    })();
                    match attempt {
                        Ok(true) => {
                            tool_yielded = true;
                            break;
                        }
                        Ok(false) => (),
                        Err(error) => self.yield_diagnostic(&error),
                    }
                }
                if let Some(n) = &mut notifier {
                    n.update(&job, false);
                }
                if let Ok(e) = erx.try_recv() {
                    return Err(e.into());
                }
                if !had_event && let Some(code) = p.poll()? {
                    if self.tool_handoff_pending(&job) {
                        continue;
                    }
                    let at = *exited_at.get_or_insert(self.rt.mono());
                    if readers.iter().all(thread::JoinHandle::is_finished)
                        || self.rt.mono() - at > EXIT_DRAIN_S
                    {
                        // Events can arrive between recv_timeout and the EOF/process check.
                        // Drain them after observing exit so turn.completed is never lost.
                        while let Ok(ev) = qrx.try_recv() {
                            job["last_event_at"] = ev["_ts"].clone();
                            apply_event(&mut job, &ev);
                        }
                        exit = Some(code);
                        break;
                    }
                }
            }
            Ok(())
        })();
        let setup_failed = work.is_err();
        if tool_yielded {
            if let Some(n) = &mut notifier {
                n.close();
            }
            #[cfg(windows)]
            drop(chrome_proxy);
            self.close_chrome_tabs(jid);
            return Ok(true);
        }
        if let Err(e) = work {
            job["last_error"] = json!(format!(
                "supervisor error: {}",
                redact_local_log_secrets(&e.message, &self.rt.env())
            ));
            if let Some(p) = proc.as_mut()
                && p.poll()?.is_none()
            {
                let _ = self.rt.kill_tree(p.pid());
                let end = self.rt.mono() + 2.;
                while p.poll()?.is_none() && self.rt.mono() < end {
                    self.rt.sleep(0.05);
                }
                if p.poll()?.is_none() {
                    let _ = p.kill();
                }
                cleanup_failed = p.poll()?.is_none();
            }
        }
        // A queued job cancelled before the second load must never be rewritten by this supervisor.
        if !setup_failed && proc.is_none() && s(&job, "status") != "running" {
            return Ok(false);
        }
        finish(
            &mut job,
            exit,
            ((self.rt.mono() - t0) * 1000.) as i64,
            &self.rt.now(),
        );
        if cleanup_failed {
            job["status"] = json!("lost");
            job["activity"] = json!("child process could not be stopped");
        }
        // Cancellation may have been recorded while the child was draining.
        let gate = match self.queue_lock() {
            Ok(gate) => Some(gate),
            Err(error) => {
                self.yield_diagnostic(&error);
                None
            }
        };
        let state = self.job_lock(jid, "state.lock")?;
        let fresh = self.load(jid)?;
        if fresh["cancel_requested"] == true || s(&fresh, "status") == "cancelled" {
            job = fresh;
        }
        let retry = !cleanup_failed && !setup_failed && self.retry_429(&mut job, cfg);
        let mut yielded = false;
        if gate.is_some() && !cleanup_failed && !setup_failed && !retry {
            let mut candidate = job.clone();
            let attempt = (|| -> Result<bool> {
                if !self.complete_context_summary(&mut candidate)?
                    && !self.yield_completed_turn(&mut candidate, &self.admission_jobs()?)?
                {
                    job = candidate.clone();
                    return Ok(false);
                }
                self.track_queued_job(jid)?;
                Ok(true)
            })();
            match attempt {
                Ok(true) => {
                    job = candidate;
                    yielded = true;
                }
                Ok(false) => (),
                Err(error) => {
                    if s(&job, "context_phase") == "summarize" {
                        job["status"] = json!("failed");
                        job["last_error"] = json!(format!(
                            "context handoff could not be persisted: {}",
                            redact_local_log_secrets(&error.message, &self.rt.env())
                        ));
                    }
                    self.yield_diagnostic(&error);
                }
            }
        }
        self.save(&job)?;
        drop(state);
        drop(gate);
        #[cfg(windows)]
        drop(chrome_proxy);
        self.close_chrome_tabs(jid);
        if retry || yielded {
            if let Some(n) = &mut notifier {
                n.close();
            }
            return Ok(true);
        }
        self.auto_rate(&mut job, None);
        // Maintenance is detached and quiet; an unavailable cache never changes job results.
        let cache_finish = self.root.join("cc_cache_finish.exe");
        if cache_finish.is_file() {
            let mut command = hidden_command(&cache_finish.to_string_lossy());
            let _ = command
                .envs(self.job_environment())
                .stdin(std::process::Stdio::null())
                .stdout(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .spawn();
        }
        if let Some(n) = &mut notifier {
            n.update(&job, true);
            n.close();
        }
        Ok(false)
    }
}
pub fn transient_file_access(e: &std::io::Error) -> bool {
    e.kind() == std::io::ErrorKind::PermissionDenied
        || matches!(e.raw_os_error(), Some(5 | 32 | 33))
}
#[derive(Default)]
pub struct Session {
    pub path: Option<PathBuf>,
    pub pos: u64,
}
fn find_session(dir: &Path, id: &str, recursive: bool) -> Option<PathBuf> {
    for e in fs::read_dir(dir).ok()?.flatten() {
        let p = e.path();
        if p.is_dir()
            && recursive
            && let Some(p) = find_session(&p, id, true)
        {
            return Some(p);
        }
        let name = e.file_name().to_string_lossy().into_owned();
        if name.starts_with("rollout-") && name.ends_with(&format!("-{id}.jsonl")) {
            return Some(p);
        }
    }
    None
}
pub fn codex_args(j: &Value, d: &Path) -> Vec<String> {
    let summary = s(j, "context_phase") == "summarize";
    let policy = cc_job_policy::for_sandbox(if summary {
        "read-only"
    } else {
        s(j, "sandbox")
    });
    let mut args = vec![
        s(j, "codex").into(),
        "exec".into(),
        "--json".into(),
        "--skip-git-repo-check".into(),
        "-c".into(),
        format!("approval_policy={}", policy.approval),
        "-c".into(),
        format!("approvals_reviewer={}", policy.reviewer),
        "-m".into(),
        getstr(j, "model"),
        "-c".into(),
        format!("model_reasoning_effort={}", getstr(j, "effort")),
        "-s".into(),
        policy.sandbox.into(),
        "-o".into(),
        d.join(if summary {
            "context-summary-output.md"
        } else {
            "last_message.md"
        })
        .to_string_lossy()
        .into(),
    ];
    if summary {
        // Disable the whole plugin/app features and every configured server
        // discovered in this job's working directory before the stop.
        args.extend([
            "-c".into(),
            "features.plugins=false".into(),
            "-c".into(),
            "features.apps=false".into(),
            "-c".into(),
            "features.shell_tool=false".into(),
            "-c".into(),
            "web_search=disabled".into(),
        ]);
        for server in j["context_disabled_mcp_servers"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(Value::as_str)
        {
            args.extend(["-c".into(), format!("mcp_servers.{server}.enabled=false")]);
        }
        if !s(j, "resume_session").is_empty() {
            args.extend(["resume".into(), s(j, "resume_session").into()]);
        }
        args.push("-".into());
        return args;
    }
    if let Some(dirs) = j["add_dirs"].as_array() {
        for d in dirs {
            args.extend(["--add-dir".into(), pystr(d)]);
        }
    }
    if truth(&j["network"]) && policy.sandbox == "workspace-write" {
        args.extend([
            "-c".into(),
            "sandbox_workspace_write.network_access=true".into(),
        ]);
    }
    let disabled: &[&str] = match s(j, "mcp_profile") {
        "full" => &[],
        "browser" => &["godd-a", "headroom", "pencil"],
        _ => &["godd-a", "headroom", "pencil", "node_repl"],
    };
    // Plugins start their own node runtimes (cua-repl + node_repl per codex process) even when
    // mcp_servers.node_repl is off; jobs rarely need them (owner, 2026-10-02: node helpers waste CPU).
    let plugins_off: &[&str] = match s(j, "mcp_profile") {
        "full" => &[],
        "browser" => &["visualize@openai-bundled"],
        _ => &[
            "browser@openai-bundled",
            "chrome@openai-bundled",
            "computer-use@openai-bundled",
            "unified-computer-use@openai-bundled",
            "visualize@openai-bundled",
        ],
    };
    for plugin in plugins_off {
        args.extend(["-c".into(), format!("plugins.'{plugin}'.enabled=false")]);
    }
    for server in disabled {
        args.extend(["-c".into(), format!("mcp_servers.{server}.enabled=false")]);
    }
    if matches!(s(j, "mcp_profile"), "browser" | "full") {
        args.extend(["-c".into(), "mcp_servers.node_repl.enabled=true".into()]);
        for plugin in [
            "chrome@openai-bundled",
            "browser@openai-bundled",
            "computer-use@openai-bundled",
            "unified-computer-use@openai-bundled",
        ] {
            args.extend(["-c".into(), format!("plugins.'{plugin}'.enabled=true")]);
        }
    }
    if !s(j, "chrome_pipe").is_empty() {
        for server in ["node_repl"] {
            for (key, value) in [
                ("CDP_BROWSER_BACKEND_PIPE_PATH", s(j, "chrome_pipe")),
                ("BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET", "true"),
                ("BROWSER_USE_AVAILABLE_BACKENDS", "chrome"),
            ] {
                args.extend([
                    "-c".into(),
                    format!("mcp_servers.{server}.env.{key}='{value}'"),
                ]);
            }
        }
    }
    if !s(j, "resume_session").is_empty() {
        args.extend(["resume".into(), s(j, "resume_session").into()]);
    }
    args.push("-".into());
    args
}
/// Evidence mapping, independent of I/O. Unknown failure causes do not imply infrastructure.
pub fn auto_quality(j: &Value, evaluation: Option<&str>) -> (i64, String) {
    let status = s(j, "status");
    let infra=Regex::new(r"(?i)supervisor|proxy|transport|connection|connecterror|timeout|timed out|rate.?limit|quota|provider error|model .*not found|model .*unavailable|spawn|filenotfound|permissionerror|without turn\.completed|could not be stopped|\b(?:401|403|404|429|502|503|504)\b|接続|通信|タイムアウト|利用枠").unwrap();
    if status == "lost"
        || (["failed", "cancelled"].contains(&status) && infra.is_match(s(j, "last_error")))
    {
        return (1, format!("auto: infra; {status}"));
    }
    match evaluation {
        Some("partial" | "途中で止まった") => (2, "auto: Jev 途中で止まった".into()),
        Some("not_done" | "未完了") => (2, "auto: Jev 未完了".into()),
        Some("needs_input" | "判断待ち") => (3, "auto: Jev 判断待ち".into()),
        Some("completed" | "完了") if status == "succeeded" => (4, "auto: Jev 完了".into()),
        _ if status == "succeeded" => (3, "auto: no evaluation".into()),
        _ => (
            2,
            format!(
                "auto: {}; no completion evidence",
                if status.is_empty() { "unknown" } else { status }
            ),
        ),
    }
}
/// (last n progress lines, offset to resume from, unterminated remainder), reading backwards from the end.
/// partial=True also shows an unterminated last line (it is complete once nothing is writing anymore).
pub fn read_tail(path: &Path, n: usize, partial: bool) -> Result<(Vec<String>, u64, Vec<u8>)> {
    let mut f = match fs::File::open(path) {
        Ok(f) => f,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok((vec![], 0, vec![])),
        Err(e) => return Err(e.into()),
    };
    let end = f.seek(SeekFrom::End(0))?;
    let mut size = 1 << 16;
    loop {
        let start = end.saturating_sub(size);
        f.seek(SeekFrom::Start(start))?;
        let mut data = vec![];
        f.read_to_end(&mut data)?;
        let mut raws = data.split(|c| *c == b'\n').collect::<Vec<_>>();
        let rest = raws.pop().unwrap_or(&[]).to_vec();
        if start > 0 && !raws.is_empty() {
            raws.remove(0);
        }
        if partial {
            raws.push(&rest);
        }
        let lines = event_lines(raws);
        if lines.len() >= n || start == 0 {
            let skip = lines.len().saturating_sub(n);
            return Ok((
                lines.into_iter().skip(skip).collect(),
                end - rest.len() as u64,
                rest,
            ));
        }
        size = size.saturating_mul(8);
    }
}
pub fn read_from(path: &Path, pos: u64) -> Result<Vec<u8>> {
    let mut f = match fs::File::open(path) {
        Ok(f) => f,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(vec![]),
        Err(e) => return Err(e.into()),
    };
    f.seek(SeekFrom::Start(pos))?;
    let mut b = vec![];
    f.read_to_end(&mut b)?;
    Ok(b)
}
