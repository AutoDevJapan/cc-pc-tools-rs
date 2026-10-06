use crate::*;
use std::io::{Read, Seek, SeekFrom};
use std::path::Path;
use std::{
    collections::{HashMap, HashSet},
    fs,
};

#[derive(serde::Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct QueueConfig {
    pub max_running: usize,
    pub max_children_per_parent: usize,
    pub max_429_retries: u32,
    pub retry_delay_seconds: u64,
    pub max_retry_delay_seconds: u64,
}
impl Default for QueueConfig {
    fn default() -> Self {
        Self {
            max_running: 6,
            max_children_per_parent: 2,
            max_429_retries: 5,
            retry_delay_seconds: 60,
            max_retry_delay_seconds: 900,
        }
    }
}
impl Context {
    /// Called under the admission gate. A separate ledger survives writes by
    /// supervisors deployed before fairness existed. Never interrupt a turn.
    fn maintain_yields(&self, jobs: &[Value]) -> Result<()> {
        let path = self.jobs.join(".queue-yields.json");
        let mut ledger = match fs::read(&path) {
            Ok(bytes) => serde_json::from_slice::<HashMap<String, Value>>(&bytes)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => HashMap::new(),
            Err(e) => return Err(e.into()),
        };
        let before = ledger.clone();
        // Snapshot before taking any per-job lock (global gate always comes first).
        for job in jobs {
            if self.should_yield(job, jobs) {
                ledger.entry(s(job, "id").into()).or_insert_with(
                    || json!({"started_at":job["started_at"],"thread_id":job["thread_id"],"requested_at":self.rt.now()}),
                );
            }
        }
        for id in ledger.keys().cloned().collect::<Vec<_>>() {
            if !valid_job_id(&id) {
                return Err(Error::new("invalid yield ledger job id"));
            }
            let _state = self.job_lock(&id, "state.lock")?;
            let mut job = self.load(&id)?;
            if s(&job, "status") == "running" {
                // A withdrawn request must not affect a later turn/attempt.
                if job["started_at"] != ledger[&id]["started_at"]
                    || job["thread_id"] != ledger[&id]["thread_id"]
                    || !self.should_yield(&job, jobs)
                {
                    ledger.remove(&id);
                }
                continue;
            }
            if s(&job, "status") != "succeeded" || job["cancel_requested"] == true {
                ledger.remove(&id);
                continue;
            }
            if job["started_at"] != ledger[&id]["started_at"]
                || job["thread_id"] != ledger[&id]["thread_id"]
                || !self.yield_completed_turn(&mut job, jobs)?
            {
                ledger.remove(&id);
                continue;
            }
            if self.is_ours(&job, "codex_pid") {
                continue;
            }
            let Ok(owner) = FileLock::try_acquire(&self.job_dir(&id).join("supervisor.lock"))
            else {
                continue;
            };
            // The legacy supervisor finished; take ownership without stopping it.
            let original = self.load(&id)?;
            job["pid"] = Value::Null;
            job["pid_created"] = Value::Null;
            self.save(&job)?;
            self.track_queued_job(&id)?;
            drop(owner);
            if let Err(error) = self.restart_queued_supervisor(&mut job) {
                self.save(&original)?;
                return Err(error);
            }
            ledger.remove(&id);
        }
        if ledger != before {
            self.write_json(&path, &json!(ledger))?;
        }
        Ok(())
    }

    fn has_pending_descendants(&self, job: &Value, jobs: &[Value]) -> bool {
        jobs.iter().any(|other| {
            s(other, "id") != s(job, "id")
                && (other["queue_unreadable"] == true
                    || ((LIVE.contains(&s(other, "status")) || self.is_ours(other, "codex_pid"))
                        && self.queue_root(other) == s(job, "id")))
        })
    }

    fn should_yield(&self, job: &Value, jobs: &[Value]) -> bool {
        s(job, "parent_job_id").is_empty()
            && s(job, "status") == "running"
            && job["cancel_requested"] != true
            && normalize_uuid(s(job, "thread_id")).is_some()
            && parse_iso(s(job, "started_at")).is_some_and(|start| self.rt.epoch() - start >= 7200.)
            && !self.has_pending_descendants(job, jobs)
            && jobs.iter().any(|other| {
                s(other, "id") != s(job, "id")
                    && s(other, "parent_job_id").is_empty()
                    && s(other, "status") == "queued"
                    && other["cancel_requested"] != true
                    && self.supervisor_alive(other)
                    && other["retry_not_before"].as_f64().unwrap_or(0.) <= self.rt.epoch()
            })
    }

    /// A completed turn and exited process are the only yield boundary. The
    /// caller holds the gate and state lock, preventing child registration races.
    pub(crate) fn yield_completed_turn(&self, job: &mut Value, jobs: &[Value]) -> Result<bool> {
        if s(job, "status") != "succeeded"
            || !truth(&job["turn_completed"])
            || self.is_ours(job, "codex_pid")
            || s(job, "last_message")
                .lines()
                .next()
                .is_some_and(|line| line.trim() == "状態: 完了")
        {
            return Ok(false);
        }
        let mut running = job.clone();
        running["status"] = json!("running");
        if !self.should_yield(&running, jobs) {
            return Ok(false);
        }
        let Some(session) = ["thread_id", "resume_session"]
            .iter()
            .find_map(|key| normalize_uuid(s(job, key)))
        else {
            return Ok(false);
        };
        let prompt = self.job_dir(s(job, "id")).join("yield-resume.md");
        fs::write(
            &prompt,
            "実行枠を他の作業に譲った後の再開です。同じセッションと作業フォルダで、ここまでの成果・最新の指示を引き継いで未完了の作業を続けてください。完了済みの作業を繰り返さず、元の依頼が完了していれば完了を報告してください。\n",
        )?;
        job["yield_last_message"] = job["last_message"].clone();
        job["yield_count"] = json!(number(job, "yield_count") + 1);
        job["last_yield_at"] = json!(self.rt.now());
        job["yield_resume"] = json!(true);
        prepare_resume(job, &session, &self.rt.now());
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!("順番待ち: 長時間の作業が枠を譲りました");
        Ok(true)
    }
    /// Upgrade waiting supervisors without stopping children or changing sessions.
    pub fn reload_queue(&self) -> Result<Value> {
        let _gate = self.queue_lock()?;
        let mut restarted = Vec::new();
        for snapshot in self.list_jobs(&HashSet::new()) {
            if s(&snapshot, "status") != "queued" {
                continue;
            }
            let jid = s(&snapshot, "id");
            let _state = self.job_lock(jid, "state.lock")?;
            let mut job = self.load(jid)?;
            if s(&job, "status") != "queued"
                || job["cancel_requested"] == true
                || self.is_ours(&job, "codex_pid")
            {
                continue;
            }
            self.restart_queued_supervisor(&mut job)?;
            restarted.push(json!(jid));
        }
        Ok(json!(restarted))
    }
    /// Caller holds the global gate and this job's state lock; no Codex is live.
    pub(crate) fn restart_queued_supervisor(&self, job: &mut Value) -> Result<()> {
        let jid = s(job, "id").to_string();
        if self.is_ours(job, "pid") {
            self.rt.kill_tree(job["pid"].as_u64().unwrap() as u32)?;
            for _ in 0..50 {
                if !self.is_ours(job, "pid") {
                    break;
                }
                self.rt.sleep(0.1);
            }
            if self.is_ours(job, "pid") {
                return Err(Error::new(format!(
                    "queued supervisor {jid} could not be stopped"
                )));
            }
        }
        job["pid"] = Value::Null;
        // Wait until the exited owner's OS lock is released before spawning.
        let owner = FileLock::acquire(&self.job_dir(&jid).join("supervisor.lock"))?;
        job["pid_created"] = Value::Null;
        job["last_event_at"] = json!(self.rt.now());
        self.save(job)?;
        self.track_queued_job(&jid)?;
        drop(owner);
        let (_, pid) = self.rt.spawn_supervisor_identified(
            &self.executable,
            &self.root,
            &self.job_dir(&jid),
            &jid,
        )?;
        if let Some(pid) = pid {
            job["pid"] = json!(pid);
            job["pid_created"] = json!(self.rt.proc_created(pid));
            self.save(job)?;
        }
        Ok(())
    }
    /// Called under the global gate. Active records are reread every poll;
    /// history is reconciled at most once per 30 seconds across all waiters.
    pub(crate) fn admission_jobs(&self) -> Result<Vec<Value>> {
        let path = self.jobs.join(".queue-index.json");
        let index = self.read_json(&path).unwrap_or(Value::Null);
        let known = if let Some(ids) = index["ids"].as_array() {
            ids.iter()
                .map(|id| {
                    let id = id
                        .as_str()
                        .filter(|id| valid_job_id(id))
                        .ok_or_else(|| Error::new("invalid queue index id"))?;
                    Ok(self.admission_record(id))
                })
                .collect::<Result<Vec<_>>>()?
        } else {
            Vec::new()
        };
        let jobs = if index["scanned_at"]
            .as_f64()
            .is_some_and(|t| self.rt.epoch() >= t && self.rt.epoch() - t < 30.)
            && index["ids"].is_array()
        {
            known
        } else {
            let mut records = Vec::new();
            for entry in fs::read_dir(&self.jobs)? {
                let entry = entry?;
                if entry.path().join("job.json").is_file() {
                    records.push(self.admission_record(&entry.file_name().to_string_lossy()));
                }
            }
            records
        };
        let active: Vec<_> = jobs
            .into_iter()
            .filter(|j| LIVE.contains(&s(j, "status")) || self.is_ours(j, "codex_pid"))
            .collect();
        let scanned_at = index["scanned_at"]
            .as_f64()
            .filter(|t| self.rt.epoch() >= *t && self.rt.epoch() - *t < 30.)
            .unwrap_or(self.rt.epoch());
        let next = json!({"scanned_at":scanned_at,"ids":active.iter().map(|j|s(j,"id")).collect::<Vec<_>>()});
        if next != index {
            self.write_json(&path, &next)?;
        }
        Ok(active)
    }
    fn admission_record(&self, id: &str) -> Value {
        self.try_load(id).unwrap_or_else(|| {
            self.rt.diagnostic(&format!(
                "unreadable queue record {id}; retaining its root slot"
            ));
            json!({"id":id,"status":"running","queue_unreadable":true})
        })
    }
    /// Register before exposing a newly queued record to a supervisor. If no
    /// index exists, admission will build it from all records on the first poll.
    pub fn track_queued_job(&self, jid: &str) -> Result<()> {
        let path = self.jobs.join(".queue-index.json");
        if !path.exists() {
            return Ok(());
        }
        let mut index = self.read_json(&path).unwrap_or(Value::Null);
        let Some(ids) = index["ids"].as_array_mut() else {
            // Rebuild on the next admission; a damaged cache is never authority.
            self.write_json(&path, &Value::Null)?;
            return Ok(());
        };
        if !ids.iter().any(|id| id.as_str() == Some(jid)) {
            ids.push(json!(jid));
            self.write_json(&path, &index)?;
        }
        Ok(())
    }
    /// Resurrect the existing record so ToDo/scheduler references remain valid.
    /// All launches share the global gate, and every candidate is rechecked there.
    pub fn recover_429(&self, since: &str, todo_file: &Path, apply: bool) -> Result<Value> {
        let since = parse_iso(since).ok_or_else(|| Error::new("--since must be RFC3339"))?;
        self.queue_config()?;
        let _gate = self.queue_lock()?;
        let db = self.read_json(todo_file)?;
        let items = db["items"]
            .as_array()
            .ok_or_else(|| Error::new("todo file has no items array"))?;
        let mut jobs = self.list_jobs(&HashSet::new());
        let mut result = Vec::new();
        // Latest failures first: selecting one prevents earlier attempts competing.
        for index in (0..jobs.len()).rev() {
            let mut job = jobs[index].clone();
            if s(&job, "status") != "failed"
                && !(s(&job, "status") == "cancelled"
                    && truth(&job["recovered_429_at"])
                    && job["cancel_requested"] != true)
            {
                continue;
            }
            let event = last_event(&self.job_dir(s(&job, "id")).join("events.jsonl"))?;
            let text = if s(&event, "type") == "turn.failed" {
                err_text(&event["error"])
            } else if s(&event, "type") == "error" {
                s(&event, "message").into()
            } else {
                continue;
            };
            if !is_429(&text)
                || parse_iso(s(&event, "_ts"))
                    .or_else(|| parse_iso(s(&job, "finished_at")))
                    .is_none_or(|t| t < since)
            {
                continue;
            }
            let item = items.iter().find(|item| {
                item["id"] == job["todo_id"]
                    || item["jobs"]
                        .as_array()
                        .is_some_and(|ids| ids.iter().any(|id| id == &job["id"]))
            });
            if let Some(item) = item {
                job["todo_id"] = item["id"].clone();
            }
            let session = ["thread_id", "resume_session"]
                .iter()
                .find_map(|k| normalize_uuid(s(&job, k)));
            let reason = if job["cancel_requested"] == true {
                Some("取り消し済み")
            } else if self.is_ours(&job, "pid") || self.is_ours(&job, "codex_pid") {
                Some("元のプロセスが終了していない")
            } else if item.is_some_and(|i| ["done", "block"].contains(&s(i, "status"))) {
                Some("ToDoが完了または判断待ち")
            } else if session.is_none() {
                Some("再開するセッションがない")
            } else if !self.job_dir(s(&job, "id")).join("prompt.md").is_file()
                || !Path::new(s(&job, "cwd")).is_dir()
            {
                Some("プロンプトまたは作業フォルダがない")
            } else if jobs.iter().any(|other| {
                s(other, "id") != s(&job, "id")
                    && (LIVE.contains(&s(other, "status"))
                        || (s(other, "created_at"), s(other, "id"))
                            > (s(&job, "created_at"), s(&job, "id")))
                    && (same_task(&job, other)
                        || item.is_some_and(|i| {
                            i["jobs"]
                                .as_array()
                                .is_some_and(|ids| ids.iter().any(|id| id == &other["id"]))
                        }))
            }) {
                Some("後続の担当がある")
            } else {
                None
            };
            let jid = s(&job, "id").to_string();
            if let Some(reason) = reason {
                result.push(
                    json!({"id":jid,"action":"skip","reason":reason,"todo_id":job["todo_id"]}),
                );
                continue;
            }
            if apply {
                let _state = self.job_lock(&jid, "state.lock")?;
                let fresh = self.load(&jid)?;
                if (s(&fresh, "status") != "failed"
                    && !(s(&fresh, "status") == "cancelled" && truth(&fresh["recovered_429_at"])))
                    || fresh["cancel_requested"] == true
                {
                    continue;
                }
                // Ensure a previously ending supervisor no longer owns the record.
                let owner = match FileLock::try_acquire(&self.job_dir(&jid).join("supervisor.lock"))
                {
                    Ok(lock) => lock,
                    Err(_) => {
                        result.push(
                            json!({"id":jid,"action":"skip","reason":"終了処理が続いている"}),
                        );
                        continue;
                    }
                };
                let todo_id = job["todo_id"].clone();
                job = fresh.clone();
                job["todo_id"] = todo_id;
                job["recovered_429_at"] = json!(self.rt.now());
                job["retry_count"] = json!(0);
                job["retry_not_before"] = Value::Null;
                job["pid"] = Value::Null;
                job["pid_created"] = Value::Null;
                prepare_resume(&mut job, session.as_deref().unwrap(), &self.rt.now());
                self.save(&job)?;
                self.track_queued_job(&jid)?;
                drop(owner);
                match self.rt.spawn_supervisor_identified(
                    &self.executable,
                    &self.root,
                    &self.job_dir(&jid),
                    &jid,
                ) {
                    Ok((_, pid)) => {
                        if let Some(pid) = pid {
                            job["pid"] = json!(pid);
                            job["pid_created"] = json!(self.rt.proc_created(pid));
                            self.save(&job)?;
                        }
                    }
                    Err(e) => {
                        self.save(&fresh)?;
                        return Err(e.into());
                    }
                }
                jobs[index] = job.clone();
            }
            result.push(json!({"id":jid,"action":if apply {"queued"} else {"would_queue"},"session":session,"todo_id":job["todo_id"]}));
        }
        Ok(json!(result))
    }
    pub fn queue_config(&self) -> Result<QueueConfig> {
        let path = self.root.join("queue-config.json");
        let cfg: QueueConfig = match fs::read(&path) {
            Ok(bytes) => serde_json::from_slice(&bytes)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => QueueConfig::default(),
            Err(e) => return Err(e.into()),
        };
        if cfg.max_running == 0
            || cfg.max_children_per_parent == 0
            || cfg.retry_delay_seconds == 0
            || cfg.max_retry_delay_seconds < cfg.retry_delay_seconds
        {
            return Err(Error::new(
                "queue-config.json: max_running, max_children_per_parent and retry_delay_seconds must be positive; max_retry_delay_seconds must be >= retry_delay_seconds",
            ));
        }
        Ok(cfg)
    }

    /// The global gate covers admission and registration (including manual resumes).
    pub fn queue_lock(&self) -> Result<FileLock> {
        fs::create_dir_all(&self.jobs)?;
        Ok(FileLock::acquire_queue(&self.jobs.join(".queue.lock"))?)
    }

    /// Return the state lock with a durably reserved slot, or None while waiting.
    /// Lock order everywhere is global gate, then job state.
    pub fn admit(&self, jid: &str) -> Result<Option<FileLock>> {
        let cfg = self.queue_config()?;
        self.admit_configured(jid, &cfg)
    }
    pub fn admit_configured(&self, jid: &str, cfg: &QueueConfig) -> Result<Option<FileLock>> {
        let _gate = self.queue_lock()?;
        let jobs = self.admission_jobs()?;
        self.maintain_yields(&jobs)?;
        let state = self.job_lock(jid, "state.lock")?;
        let mut job = self.load(jid)?;
        if s(&job, "status") != "queued" || job["cancel_requested"] == true {
            return Ok(None);
        }
        let jobs = self.admission_jobs()?;
        let active: Vec<_> = jobs
            .iter()
            .filter(|j| j["queue_unreadable"] == true || self.running_job(j))
            .collect();
        // Count occupied families even when the root/intermediate supervisor has
        // exited. A surviving descendant must not silently free a root slot.
        let mut families = HashSet::new();
        let mut children = HashMap::<String, usize>::new();
        for j in &active {
            families.insert(self.queue_root(j));
            if !s(j, "parent_job_id").is_empty() {
                *children.entry(s(j, "parent_job_id").into()).or_default() += 1;
            }
        }
        let mut waiting: Vec<_> = jobs
            .iter()
            .filter(|j| {
                s(j, "status") == "queued"
                    && (s(j, "id") == jid || self.supervisor_alive(j))
                    && j["cancel_requested"] != true
                    && j["retry_not_before"].as_f64().unwrap_or(0.) <= self.rt.epoch()
            })
            .collect();
        waiting.sort_by_key(|j| {
            (
                s(j, "parent_job_id").is_empty(),
                queue_priority(j),
                queue_time(j),
                s(j, "id"),
            )
        });
        let mut selected = false;
        let mut child_first = false;
        for candidate in waiting {
            let parent = s(candidate, "parent_job_id");
            if parent.is_empty() && child_first {
                continue;
            }
            if !parent.is_empty()
                && children.get(parent).copied().unwrap_or(0) >= cfg.max_children_per_parent
            {
                continue;
            }
            let root = self.queue_root(candidate);
            if !families.contains(&root) && families.len() >= cfg.max_running {
                continue;
            }
            // Children have first claim; wait for the earlier eligible job to
            // reserve its slot durably instead of allowing roots to overtake it.
            if s(candidate, "id") == jid {
                selected = true;
                break;
            }
            families.insert(root);
            if !parent.is_empty() {
                child_first = true;
                *children.entry(parent.into()).or_default() += 1;
            }
        }
        if !selected {
            return Ok(None);
        }
        job["status"] = json!("running");
        job["started_at"] = json!(self.rt.now());
        job["last_event_at"] = json!(self.rt.now());
        job["activity"] = json!("starting");
        self.save(&job)?;
        Ok(Some(state))
    }

    fn queue_root(&self, job: &Value) -> String {
        let mut current = job.clone();
        let mut seen = HashSet::new();
        loop {
            let id = s(&current, "id").to_string();
            if !seen.insert(id.clone()) {
                self.rt
                    .diagnostic("cycle in parent_job_id; retaining one root slot");
                return seen.into_iter().min().unwrap();
            }
            let parent = s(&current, "parent_job_id");
            if parent.is_empty() {
                return id;
            }
            if !valid_job_id(parent) {
                self.rt
                    .diagnostic(&format!("invalid parent for {id}; retaining its root slot"));
                return id;
            }
            let Some(ancestor) = self.try_load(parent) else {
                self.rt.diagnostic(&format!(
                    "unreadable parent {parent}; retaining its family slot"
                ));
                return parent.into();
            };
            current = ancestor;
        }
    }

    pub fn running_job(&self, job: &Value) -> bool {
        self.is_ours(job, "codex_pid")
            || (s(job, "status") == "running" && self.supervisor_alive(job))
    }

    /// Called only after the child has exited and its final events have drained.
    pub fn retry_429(&self, job: &mut Value, cfg: &QueueConfig) -> bool {
        if s(job, "status") != "failed"
            || !is_429(s(job, "last_error"))
            || job["cancel_requested"] == true
            || number(job, "retry_count") >= i64::from(cfg.max_429_retries)
        {
            return false;
        }
        let session = ["thread_id", "resume_session"]
            .iter()
            .find_map(|k| normalize_uuid(s(job, k)));
        let Some(session) = session else {
            return false;
        };
        let count = number(job, "retry_count").max(0) as u32;
        let delay = cfg
            .retry_delay_seconds
            .saturating_mul(1u64.checked_shl(count).unwrap_or(u64::MAX))
            .min(cfg.max_retry_delay_seconds);
        job["retry_count"] = json!(count + 1);
        job["retry_not_before"] = json!(self.rt.epoch() + delay as f64);
        job["retry_last_error"] = job["last_error"].clone();
        prepare_resume(job, &session, &self.rt.now());
        job["activity"] = json!(format!(
            "順番待ち: 429後の再開まで{delay}秒（{}回目）",
            count + 1
        ));
        true
    }
}
pub(crate) fn same_task(a: &Value, b: &Value) -> bool {
    (a["todo_id"].as_u64().is_some() && a["todo_id"] == b["todo_id"])
        || (!s(a, "name").is_empty() && s(a, "name") == s(b, "name"))
        || ["thread_id", "resume_session"].iter().any(|ka| {
            normalize_uuid(s(a, ka)).is_some_and(|id| {
                ["thread_id", "resume_session"]
                    .iter()
                    .any(|kb| normalize_uuid(s(b, kb)).as_ref() == Some(&id))
            })
        })
}
pub fn valid_job_id(id: &str) -> bool {
    !id.is_empty()
        && id
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}
fn last_event(path: &Path) -> Result<Value> {
    let mut file = match fs::File::open(path) {
        Ok(f) => f,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Value::Null),
        Err(e) => return Err(e.into()),
    };
    let end = file.seek(SeekFrom::End(0))?;
    file.seek(SeekFrom::Start(end.saturating_sub(65536)))?;
    let mut buf = Vec::new();
    file.read_to_end(&mut buf)?;
    Ok(buf
        .split(|b| *b == b'\n')
        .rev()
        .find(|line| !line.iter().all(u8::is_ascii_whitespace))
        .and_then(|line| serde_json::from_slice(line).ok())
        .unwrap_or(Value::Null))
}
fn queue_time(j: &Value) -> &str {
    if s(j, "queued_at").is_empty() {
        s(j, "created_at")
    } else {
        s(j, "queued_at")
    }
}
fn queue_priority(job: &Value) -> u8 {
    let name = s(job, "name");
    if name.starts_with("req-") {
        0
    } else if name.starts_with("shep-") {
        1
    } else {
        2
    }
}
pub fn is_429(message: &str) -> bool {
    message
        .to_ascii_lowercase()
        .contains("429 too many requests")
}
pub fn prepare_resume(job: &mut Value, session: &str, now: &str) {
    if !truth(&job["first_started_at"]) && truth(&job["started_at"]) {
        job["first_started_at"] = job["started_at"].clone();
    }
    job["resume_session"] = json!(session);
    job["status"] = json!("queued");
    // Requeue at failure time; cooldown controls eligibility, preserving FIFO
    // among attempts that are ready within the same priority class.
    job["queued_at"] = json!(now);
    job["activity"] = json!("順番待ち");
    job["last_event_at"] = json!(now);
    for field in [
        "codex_pid",
        "codex_pid_created",
        "started_at",
        "finished_at",
        "exit_code",
        "duration_ms",
        "last_error",
        "last_message",
        "turn_completed",
        "chrome_pipe",
        "gto",
        "gto_auto_attempt_at",
    ] {
        job[field] = Value::Null;
    }
}
pub fn display_state(job: &Value) -> &str {
    if s(job, "status") == "queued" {
        "順番待ち"
    } else {
        s(job, "status")
    }
}
