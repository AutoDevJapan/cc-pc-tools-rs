//! Tool boundaries and durable handoff, including already running old supervisors.
use crate::queue::prepare_resume;
use crate::*;
use std::{
    collections::{HashMap, HashSet},
    fs,
    io::{BufRead, Seek, SeekFrom},
    time::Duration,
};

#[derive(Default, serde::Serialize, serde::Deserialize)]
#[serde(default)]
struct Boundary {
    schema: u32,
    attempt: String,
    offset: u64,
    active: HashSet<String>,
    completed: Option<String>,
    terminal: bool,
    review_terminal: bool,
    invalid: bool,
    first_item_at: Option<u64>,
    agents: HashMap<String, AgentRollout>,
    finished: HashMap<String, AgentRollout>,
}
#[derive(Default, serde::Serialize, serde::Deserialize)]
#[serde(default)]
struct AgentRollout {
    fresh_spawn: bool,
    path: Option<std::path::PathBuf>,
    offset: u64,
    born: Option<f64>,
    turn: Option<String>,
    complete: bool,
    message: String,
}
impl AgentRollout {
    fn completed(&mut self, id: &str, ctx: &Context, job: &Value) -> Result<bool> {
        if !self.fresh_spawn {
            return Ok(false);
        }
        if self.path.is_none() {
            let mut child = job.clone();
            child["thread_id"] = json!(id);
            child["resume_session"] = json!(id);
            child["started_at"] = json!(ctx.rt.now());
            self.path = ctx.session_file(&child);
        }
        let Some(path) = &self.path else {
            return Ok(false);
        };
        let file = fs::File::open(path)?;
        if file.metadata()?.len() < self.offset {
            return Err(Error::new("subagent rollout truncated"));
        }
        let mut reader = std::io::BufReader::new(file);
        reader.seek(SeekFrom::Start(self.offset))?;
        loop {
            let mut raw = Vec::new();
            if reader.read_until(b'\n', &mut raw)? == 0 {
                break;
            }
            if !raw.ends_with(b"\n") {
                return Ok(false);
            }
            let ev: Value = serde_json::from_slice(&raw)?;
            self.offset += raw.len() as u64;
            let payload = &ev["payload"];
            if s(&ev, "type") == "session_meta" && s(payload, "id") == id {
                self.born = parse_iso(s(payload, "timestamp"));
            }
            if s(&ev, "type") != "event_msg"
                || parse_iso(s(&ev, "timestamp"))
                    .zip(self.born)
                    .is_none_or(|(at, born)| at < born)
            {
                continue;
            }
            match s(payload, "type") {
                "task_started" if !s(payload, "turn_id").is_empty() => {
                    self.turn = Some(s(payload, "turn_id").into());
                    self.complete = false;
                    self.message.clear();
                }
                "task_complete" if self.turn.as_deref() == Some(s(payload, "turn_id")) => {
                    self.complete = true;
                    self.message = s(payload, "last_agent_message").into();
                }
                _ => (),
            }
        }
        Ok(self.complete)
    }
}
impl Boundary {
    fn terminal_for(&self, job: &Value) -> bool {
        self.terminal
            || (self.review_terminal && cc_job_policy::review_result::is_review_subwork(job))
    }
    fn event(&mut self, ev: &Value) {
        let t = s(ev, "type");
        if t == "turn.started" {
            self.terminal = false;
            self.invalid = false;
            self.active.clear();
            self.completed = None;
        }
        if matches!(t, "turn.failed" | "turn.completed")
            || (t == "error" && !s(ev, "message").starts_with("Reconnecting..."))
        {
            self.terminal = true;
            self.completed = None;
        }
        let item = &ev["item"];
        if t == "item.completed" && s(item, "type") == "collab_tool_call" {
            let ids = item["receiver_thread_ids"]
                .as_array()
                .cloned()
                .unwrap_or_default();
            match s(item, "tool") {
                "spawn_agent" | "resume_agent" | "send_input" => {
                    for id in &ids {
                        if let Some(id) = id.as_str() {
                            if normalize_uuid(id).is_none() {
                                self.invalid = true;
                                continue;
                            }
                            let entry = self.agents.entry(id.into()).or_default();
                            entry.fresh_spawn = s(item, "tool") == "spawn_agent";
                            self.finished.remove(id);
                        }
                    }
                }
                "close_agent" if s(item, "status") == "completed" => {
                    for id in &ids {
                        if let Some(id) = id.as_str() {
                            self.agents.remove(id);
                            self.finished.remove(id);
                        }
                    }
                }
                _ => (),
            }
            if let Some(states) = item["agents_states"].as_object() {
                for (id, state) in states {
                    if ["completed", "errored", "shutdown", "not_found"]
                        .contains(&s(state, "status"))
                    {
                        self.agents.remove(id);
                        self.finished.remove(id);
                    } else if normalize_uuid(id).is_some() {
                        self.agents.entry(id.clone()).or_default();
                    } else {
                        self.invalid = true;
                    }
                }
            }
        }
        if t == "item.started" {
            if self.first_item_at.is_none() {
                self.first_item_at = ev["_filetime"].as_u64().or_else(|| {
                    parse_iso(s(ev, "_ts")).map(|at| ((at + 11_644_473_600.) * 10_000_000.) as u64)
                });
            }
            self.completed = None;
            if TOOL_KINDS.contains(&s(item, "type")) {
                if s(item, "id").is_empty() {
                    self.invalid = true;
                } else {
                    self.active.insert(s(item, "id").into());
                }
            }
        }
        if t == "item.completed" {
            self.completed = None;
            if TOOL_KINDS.contains(&s(item, "type")) && !s(item, "id").is_empty() {
                self.active.remove(s(item, "id"));
                if self.active.is_empty() {
                    self.completed = Some(s(item, "id").into());
                }
            }
            if s(item, "type") == "agent_message"
                && s(item, "text")
                    .lines()
                    .next()
                    .is_some_and(|line| ["状態: 完了", "状態: 要判断"].contains(&line.trim()))
            {
                self.terminal = true;
            }
            if s(item, "type") == "agent_message"
                && s(item, "text")
                    .lines()
                    .next()
                    .is_some_and(|line| line.trim() == cc_job_policy::review_result::COMPLETE)
            {
                self.review_terminal = true;
            }
        }
    }
}
impl Context {
    fn own_supervisor(&self, job: &Value) -> bool {
        job["pid"].as_u64() == Some(self.rt.pid() as u64)
            && job["pid_created"].as_u64() == self.rt.proc_created(self.rt.pid())
            && job["pid_created"].is_u64()
    }
    fn rollout_ready(&self, job: &Value) -> Result<bool> {
        let Some(path) = self.session_file(job) else {
            return Ok(true);
        };
        let mut file = fs::File::open(path)?;
        if file.metadata()?.len() == 0 {
            return Ok(false);
        }
        file.seek(SeekFrom::End(-1))?;
        let mut last = [0];
        std::io::Read::read_exact(&mut file, &mut last)?;
        Ok(last[0] == b'\n')
    }
    pub(crate) fn tool_handoff_pending(&self, job: &Value) -> bool {
        self.read_json(&self.job_dir(s(job, "id")).join("yield-transition.json"))
            .is_ok_and(|v| v["complete"] == false && s(&v["source"], "id") == s(job, "id"))
    }
    fn tool_boundary(&self, job: &Value) -> Result<Option<(String, Option<u64>)>> {
        let dir = self.job_dir(s(job, "id"));
        let cache = dir.join("yield-boundary.json");
        let mut boundary: Boundary = self
            .read_json(&cache)
            .ok()
            .and_then(|v| serde_json::from_value(v).ok())
            .unwrap_or_default();
        let attempt = format!("{}:{}", s(job, "started_at"), job["codex_pid_created"]);
        let Ok(file) = fs::File::open(dir.join("events.jsonl")) else {
            return Ok(None);
        };
        if boundary.schema != 4
            || boundary.attempt != attempt
            || file.metadata()?.len() < boundary.offset
        {
            boundary = Boundary {
                schema: 4,
                attempt,
                offset: job["attempt_events_offset"].as_u64().unwrap_or(0),
                ..Boundary::default()
            };
        }
        let mut reader = std::io::BufReader::new(file);
        reader.seek(SeekFrom::Start(boundary.offset))?;
        loop {
            let mut line = Vec::new();
            if reader.read_until(b'\n', &mut line)? == 0 {
                break;
            }
            if !line.ends_with(b"\n") {
                return Ok(None);
            }
            boundary.offset += line.len() as u64;
            match serde_json::from_slice::<Value>(&line) {
                Ok(ev) if ev.is_object() => {
                    if parse_iso(s(&ev, "_ts"))
                        .zip(parse_iso(s(job, "started_at")))
                        .is_some_and(|(event, start)| event >= start)
                    {
                        boundary.event(&ev);
                    }
                }
                _ => {
                    boundary.invalid = true;
                    boundary.completed = None;
                }
            }
        }
        self.write_json(&cache, &serde_json::to_value(&boundary)?)?;
        if !boundary.agents.is_empty() {
            let mut complete = Vec::new();
            for (id, state) in &mut boundary.agents {
                if state.completed(id, self, job)? {
                    complete.push(id.clone());
                }
            }
            for id in complete {
                if let Some(result) = boundary.agents.remove(&id) {
                    boundary.finished.insert(id, result);
                }
            }
            self.write_json(&cache, &serde_json::to_value(&boundary)?)?;
        }
        Ok(
            if boundary.terminal_for(job)
                || boundary.invalid
                || !boundary.active.is_empty()
                || !boundary.agents.is_empty()
            {
                None
            } else {
                boundary.completed.map(|id| (id, boundary.first_item_at))
            },
        )
    }

    fn owns_civ6_lease(&self, job: &Value) -> Result<bool> {
        let path = self
            .rt
            .env()
            .get("CODEX_JOBS_CIV6_LOCK_PATH")
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| {
                self.root
                    .parent()
                    .unwrap_or(&self.root)
                    .join("civ6-live/civ6.lock")
            });
        if !path.exists() {
            return Ok(false);
        }
        let lease = self.read_json(&path)?;
        Ok(["job", "job_id", "owner_job_id"].iter().any(|key| {
            !s(&lease, key).is_empty() && [s(job, "id"), s(job, "name")].contains(&s(&lease, key))
        }))
    }
    /// Caller does not hold the queue gate. Freeze controllers before checking again;
    /// never kill a process simply because its tool returned a background session.
    /// A failed attempt at a completed boundary backs off for 15–30 s (shared by every process through
    /// the job directory). Slow preparation runs under a per-job reservation;
    /// the shared gate covers only revalidation and the durable transition.
    pub(crate) fn yield_tool_boundary(&self, snapshot: &Value, jobs: &[Value]) -> Result<bool> {
        // Per-job reservation: slow pause/stop work for one job never needs the
        // global gate, and two supervisors never run the same job's yield.
        let Some(_reservation) = self.yield_reservation(s(snapshot, "id")) else {
            return Ok(false);
        };
        if self.yield_backoff_active(snapshot)
            || !(self.context_rotation_due(snapshot, jobs) || self.should_yield(snapshot, jobs))
            || !self.is_ours(snapshot, "codex_pid")
            || self.owns_civ6_lease(snapshot)?
        {
            return Ok(false);
        }
        // Not at a completed tool boundary yet: cheap, retry on the next event.
        let Some((_, baseline)) = self.tool_boundary(snapshot)? else {
            return Ok(false);
        };
        let result = self
            .prefetch_context_inventory(snapshot)
            .and_then(|()| self.attempt_tool_yield(snapshot, baseline));
        if !matches!(result, Ok(true)) {
            let not_before = self.rt.epoch() + crate::jitter(15., 30.);
            // Losing the backoff only costs throughput; keep the attempt result.
            if let Err(error) = self.write_json(
                &self.job_dir(s(snapshot, "id")).join("yield-retry.json"),
                &json!({"not_before": not_before, "attempt": yield_attempt_key(snapshot)}),
            ) {
                self.yield_diagnostic(&error);
            }
        }
        result
    }
    /// Non-blocking per-job yield reservation; None while another yield is running.
    pub(crate) fn yield_reservation(&self, jid: &str) -> Option<FileLock> {
        let path = self.job_dir(jid).join("yield.lock");
        let mut lock = FileLock::try_acquire(&path).ok()?;
        lock.set_owner(&path, "yield_reservation");
        Some(lock)
    }
    /// Global gate with holder diagnostics (pid + operation) and a bounded wait.
    pub(crate) fn queue_lock_for(&self, operation: &str, wait: Duration) -> Result<FileLock> {
        fs::create_dir_all(&self.jobs)?;
        let path = self.jobs.join(".queue.lock");
        let mut lock = FileLock::acquire_queue_timeout(&path, wait)
            .map_err(|e| Error::file_io("acquire exclusive lock byte range [0, 1)", &path, e))?;
        lock.set_owner(&path, operation);
        Ok(lock)
    }
    fn try_yield_state(&self, jid: &str) -> Result<FileLock> {
        let path = self.job_dir(jid).join("state.lock");
        FileLock::try_acquire(&path)
            .map_err(|e| Error::file_io("acquire exclusive lock byte range [0, 1)", &path, e))
    }
    /// Advisory candidates only: a running worker must never initiate history
    /// reconciliation at a tool boundary. Missing/partial history postpones the
    /// yield; execution admission prepares and rechecks it independently.
    pub(crate) fn yield_snapshot(&self) -> Result<Vec<Value>> {
        Ok(self.admission_snapshot().unwrap_or_default())
    }
    pub(crate) fn yield_backoff_active(&self, job: &Value) -> bool {
        self.read_json(&self.job_dir(s(job, "id")).join("yield-retry.json"))
            .is_ok_and(|v| {
                // Scoped to one Codex attempt; a requeued job starts fresh.
                v["attempt"] == yield_attempt_key(job)
                    && v["not_before"]
                        .as_f64()
                        .is_some_and(|t| t > self.rt.epoch())
            })
    }
    fn attempt_tool_yield(&self, snapshot: &Value, baseline: Option<u64>) -> Result<bool> {
        self.rt.sleep(0.1);
        if !self.rollout_ready(snapshot)? {
            return Ok(false);
        }
        let Some(mut agent) = self.rt.pause_idle_agent(
            number(snapshot, "codex_pid") as u32,
            snapshot["codex_pid_created"]
                .as_u64()
                .ok_or_else(|| Error::new("missing process identity"))?,
            baseline,
            Some(&self.job_dir(s(snapshot, "id")).join("yield-paused.json")),
        )?
        else {
            return Ok(false);
        };
        // The old reader must finish writing events already in its pipe before
        // its supervisor is paused. A newly started item invalidates the boundary.
        let mut length = fs::metadata(self.job_dir(s(snapshot, "id")).join("events.jsonl"))?.len();
        let mut stable = 0;
        for _ in 0..20 {
            self.rt.sleep(0.05);
            let current = fs::metadata(self.job_dir(s(snapshot, "id")).join("events.jsonl"))?.len();
            stable = if current == length { stable + 1 } else { 0 };
            length = current;
            if stable >= 4 {
                break;
            }
        }
        if stable < 4 {
            return Ok(false);
        }
        let external = !self.own_supervisor(snapshot);
        let mut owner = if external && self.is_ours(snapshot, "pid") {
            agent.arm_process(
                number(snapshot, "pid") as u32,
                snapshot["pid_created"]
                    .as_u64()
                    .ok_or_else(|| Error::new("missing supervisor identity"))?,
            )?;
            Some(
                self.rt.pause_supervisor(
                    number(snapshot, "pid") as u32,
                    snapshot["pid_created"]
                        .as_u64()
                        .ok_or_else(|| Error::new("missing supervisor identity"))?,
                )?,
            )
        } else {
            None
        };
        // Frozen controllers may themselves hold the gate or the state lock, so
        // after any freeze only bounded waits are allowed; failure unfreezes
        // everything on drop and the caller backs off. Order: gate, then state.
        let wait = Duration::from_secs(if owner.is_some() { 3 } else { 5 });
        let gate = self.queue_lock_for(&format!("yield_commit:{}", s(snapshot, "id")), wait)?;
        let state = self.try_yield_state(s(snapshot, "id"))?;
        let job = self.load(s(snapshot, "id"))?;
        let fresh_jobs = self.admission_jobs()?;
        if job["started_at"] != snapshot["started_at"]
            || job["codex_pid_created"] != snapshot["codex_pid_created"]
            || job["cancel_requested"] == true
            || !(self.context_rotation_due(&job, &fresh_jobs)
                || self.should_yield(&job, &fresh_jobs))
            || self.owns_civ6_lease(&job)?
            || !self.rollout_ready(&job)?
        {
            return Ok(false);
        }
        let Some((item, _)) = self.tool_boundary(&job)? else {
            return Ok(false);
        };
        let mut candidate = job.clone();
        self.prepare_tool_yield(&mut candidate, &item)?;
        let journal_path = self.job_dir(s(&job, "id")).join("yield-transition.json");
        // This record is written before any stop. A failed stop/write/spawn is
        // retried by a waiter without turning the task into failure/cancellation.
        let mut journal =
            json!({"source":job,"queued":candidate,"processes":agent.processes(),"complete":false});
        self.write_json(&journal_path, &journal)?;
        if let Some(owner) = owner.as_mut() {
            owner.stop()?;
        }
        if let Err(error) = agent.stop() {
            agent.keep_paused();
            return Err(error.into());
        }
        journal["stopped"] = json!(true);
        self.write_json(&journal_path, &journal)?;
        self.commit_tool_yield(&journal)?;
        if external {
            drop(state);
            drop(gate);
            self.close_chrome_tabs(s(&job, "id"));
        }
        Ok(true)
    }
    fn prepare_tool_yield(&self, job: &mut Value, item: &str) -> Result<()> {
        let session = normalize_uuid(s(job, "thread_id"))
            .ok_or_else(|| Error::new("missing yield session"))?;
        fs::write(
            self.job_dir(s(job, "id")).join("yield-resume.md"),
            format!(
                "実行枠を他の作業に譲った後の再開です。同じセッションと作業フォルダで、ここまでの成果・最新の指示を引き継いで未完了の作業を続けてください。停止前のコマンドとツールは完了しています。直前の項目は {item} です。完了した操作と結果は {} に保存済みです。再実行せず、先にこの項目の結果と共有ToDoを確認し、完了済みの作業や送信を繰り返さず、続きを進めてください。\n",
                self.job_dir(s(job, "id")).join("events.jsonl").display()
            ),
        )?;
        let path = self.job_dir(s(job, "id")).join("yield-resume.md");
        let mut resume = fs::read_to_string(&path)?;
        let boundary: Boundary = serde_json::from_value(
            self.read_json(&self.job_dir(s(job, "id")).join("yield-boundary.json"))?,
        )?;
        let redactor = LogRedactor::new(&self.rt.env());
        job["yield_subagent_results"] = json!({});
        for (id, result) in boundary.finished {
            let message = redactor.redact(&result.message);
            let source = result
                .path
                .as_ref()
                .map(|p| p.display().to_string())
                .unwrap_or_default();
            resume.push_str(&format!("\n停止前に完了し、親がまだ受け取っていないサブエージェント {id} の結果:\n{message}\n全文の記録: {source}\nwaitで取得し直すのではなく、この結果を使って作業を続け、同じ委任・送信を繰り返さないでください。\n"));
            job["yield_subagent_results"][&id] = json!({"path":source,"message":message});
        }
        fs::write(path, resume)?;
        job["yield_last_message"] = job["last_message"].clone();
        job["yield_count"] = json!(number(job, "yield_count") + 1);
        job["last_yield_at"] = json!(self.rt.now());
        job["yield_boundary_item"] = json!(item);
        job["yield_resume"] = json!(true);
        prepare_resume(job, &session, &self.rt.now());
        job["queue_sequence"] = json!(self.next_queue_sequence()?);
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!("順番待ち: ツール完了後に枠を譲りました");
        if self.context_rotation_due(&self.load(s(job, "id"))?, &self.admission_jobs()?) {
            self.prepare_context_summary(job, &session)?;
        }
        Ok(())
    }
    fn commit_tool_yield(&self, journal: &Value) -> Result<()> {
        let source = journal["source"].clone();
        let mut job = journal["queued"].clone();
        let id = s(&source, "id");
        if self.is_ours(&source, "codex_pid")
            || (!self.own_supervisor(&source) && self.is_ours(&source, "pid"))
        {
            return Err(Error::new("yield processes still alive"));
        }
        if self
            .journal_processes(journal)?
            .iter()
            .any(|(pid, created)| self.rt.proc_created(*pid) == Some(*created))
        {
            return Err(Error::new("yield descendant still alive"));
        }
        self.track_queued_job(id)?;
        self.save(&job)?;
        if !self.own_supervisor(&source) {
            job["pid"] = Value::Null;
            job["pid_created"] = Value::Null;
            self.restart_queued_supervisor(&mut job)?;
        }
        self.write_json(
            &self.job_dir(id).join("yield-transition.json"),
            &json!({"complete":true}),
        )?;
        Ok(())
    }
    pub(crate) fn recover_tool_yield(&self, job: &Value) -> Result<bool> {
        let path = self.job_dir(s(job, "id")).join("yield-transition.json");
        let Ok(mut journal) = self.read_json(&path) else {
            return Ok(false);
        };
        if journal["complete"] != false {
            return Ok(false);
        }
        let _state = self.job_lock(s(job, "id"), "state.lock")?;
        let fresh = self.load(s(job, "id"))?;
        let job = &fresh;
        let source = journal["source"].clone();
        if s(&source, "id") != s(job, "id") {
            return Err(Error::new("invalid yield journal identity"));
        }
        if job["cancel_requested"] == true || s(job, "status") == "cancelled" {
            self.write_json(&path, &json!({"complete":true}))?;
            return Ok(true);
        }
        self.tool_boundary(&source)?;
        if self
            .read_json(&self.job_dir(s(job, "id")).join("yield-boundary.json"))
            .is_ok_and(|v| v["terminal"] == true)
        {
            self.write_json(&path, &json!({"complete":true}))?;
            return Ok(true);
        }
        // A restarted queued supervisor or new attempt already owns this task.
        if (s(job, "status") == "queued" && self.is_ours(job, "pid"))
            || (s(job, "status") == "running"
                && job["codex_pid_created"] != source["codex_pid_created"])
        {
            self.write_json(&path, &json!({"complete":true}))?;
            return Ok(true);
        }
        if self.is_ours(&source, "pid") && !self.own_supervisor(&source) {
            self.rt
                .recover_paused(&self.job_dir(s(job, "id")).join("yield-paused.json"))?;
            if self
                .read_json(&self.job_dir(s(job, "id")).join("yield-paused.json"))
                .is_ok_and(|v| v["complete"] == true)
            {
                self.write_json(&path, &json!({"complete":true}))?;
            }
            return Ok(false);
        }
        for (pid, created) in self.journal_processes(&journal)? {
            if self.rt.proc_created(pid) != Some(created) {
                continue;
            }
            if self.owns_civ6_lease(&source)? {
                return Ok(true);
            }
            let Some((_, baseline)) = self.tool_boundary(&source)? else {
                return Ok(true);
            };
            let Some(mut agent) = self.rt.pause_idle_agent(
                pid,
                created,
                baseline,
                Some(&self.job_dir(s(job, "id")).join("yield-paused.json")),
            )?
            else {
                return Ok(true);
            };
            if self.tool_boundary(&source)?.is_none() {
                return Ok(true);
            }
            let mut processes = self.journal_processes(&journal)?;
            for process in agent.processes() {
                if !processes.contains(&process) {
                    processes.push(process);
                }
            }
            journal["processes"] = serde_json::to_value(processes)?;
            self.write_json(&path, &journal)?;
            agent.stop()?;
        }
        self.commit_tool_yield(&journal)?;
        Ok(true)
    }
    fn journal_processes(&self, journal: &Value) -> Result<Vec<(u32, u64)>> {
        serde_json::from_value(journal["processes"].clone()).map_err(Error::from)
    }
}

fn yield_attempt_key(job: &Value) -> String {
    format!("{}:{}", s(job, "started_at"), job["codex_pid_created"])
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn review_only_completion_is_terminal_only_for_validated_review_subwork() {
        let mut b = Boundary::default();
        b.event(&json!({"type":"item.completed","item":{"type":"agent_message","text":cc_job_policy::review_result::COMPLETE}}));
        let mut job = json!({"todo_id":2813,"sandbox":"read-only","subwork_validated":true,"subwork_spec":"fixed-head.md",
            "subwork":{"kind":"review","parent_todo_id":2757,"origin_job_id":"writer",
                "owner":{"repository":"fixture/repo","git_common":"fixture-common","worktree":"fixture-tree","branch":"fixture-branch"}}});
        assert!(b.terminal_for(&job));
        job.as_object_mut().unwrap().remove("subwork");
        assert!(!b.terminal_for(&job));
        let mut partial = Boundary::default();
        partial.event(&json!({"type":"item.completed","item":{"type":"agent_message","text":"状態: 完了（実装だけ・検証未了）"}}));
        assert!(!partial.terminal_for(&job));
    }
    #[test]
    fn reconnecting_notice_keeps_a_completed_tool_boundary_but_failure_is_terminal() {
        let mut b = Boundary::default();
        b.event(&ev("item.started", "cmd", "command_execution"));
        b.event(&ev("item.completed", "cmd", "command_execution"));
        b.event(&json!({"type":"error","message":"Reconnecting... 2/5 (stream disconnected before completion: Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.)"}));
        assert!(!b.terminal);
        assert_eq!(b.completed.as_deref(), Some("cmd"));
        b.event(&json!({"type":"turn.failed","error":{"message":"retries exhausted"}}));
        assert!(b.terminal);
        assert!(b.completed.is_none());
    }
    fn ev(t: &str, id: &str, kind: &str) -> Value {
        json!({"type":t,"item":{"id":id,"type":kind}})
    }
    #[test]
    fn completes_only_after_all_overlapping_tools_and_before_any_new_item() {
        let mut b = Boundary::default();
        b.event(&ev("item.started", "cargo", "command_execution"));
        b.event(&ev("item.started", "civ6", "mcp_tool_call"));
        b.event(&ev("item.completed", "cargo", "command_execution"));
        assert!(b.completed.is_none());
        b.event(&ev("item.completed", "civ6", "mcp_tool_call"));
        assert_eq!(b.completed.as_deref(), Some("civ6"));
        b.event(&ev("item.started", "next", "reasoning"));
        assert!(b.completed.is_none());
    }
    #[test]
    fn failures_completion_and_judgment_never_yield() {
        for end in [
            json!({"type":"error"}),
            json!({"type":"turn.failed"}),
            json!({"type":"turn.completed"}),
            json!({"type":"item.completed","item":{"type":"agent_message","text":"状態: 完了\n済み"}}),
            json!({"type":"item.completed","item":{"type":"agent_message","text":"状態: 要判断\n選択"}}),
        ] {
            let mut b = Boundary::default();
            b.event(&ev("item.completed", "tool", "command_execution"));
            b.event(&end);
            assert!(b.terminal);
            b.event(&ev("item.completed", "tool2", "command_execution"));
            assert!(b.terminal);
        }
    }
}
