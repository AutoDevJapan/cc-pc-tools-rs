//! Tool boundaries and durable handoff, including already running old supervisors.
use crate::queue::prepare_resume;
use crate::*;
use std::{
    collections::HashSet,
    fs,
    io::{BufRead, Seek, SeekFrom},
};

#[derive(Default, serde::Serialize, serde::Deserialize)]
#[serde(default)]
struct Boundary {
    attempt: String,
    offset: u64,
    active: HashSet<String>,
    completed: Option<String>,
    terminal: bool,
    invalid: bool,
    first_item_at: Option<u64>,
}
impl Boundary {
    fn event(&mut self, ev: &Value) {
        let t = s(ev, "type");
        if t == "turn.started" {
            self.terminal = false;
            self.invalid = false;
            self.active.clear();
            self.completed = None;
        }
        if matches!(t, "turn.failed" | "error" | "turn.completed") {
            self.terminal = true;
            self.completed = None;
        }
        let item = &ev["item"];
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
        }
    }
}
impl Context {
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
        if boundary.attempt != attempt || file.metadata()?.len() < boundary.offset {
            boundary = Boundary {
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
        Ok(
            if boundary.terminal || boundary.invalid || !boundary.active.is_empty() {
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
    /// Caller holds the queue gate. Freeze controllers before checking again;
    /// never kill a process simply because its tool returned a background session.
    pub(crate) fn yield_tool_boundary(&self, snapshot: &Value, jobs: &[Value]) -> Result<bool> {
        if !self.should_yield(snapshot, jobs)
            || !self.is_ours(snapshot, "codex_pid")
            || self.owns_civ6_lease(snapshot)?
        {
            return Ok(false);
        }
        let Some((_, baseline)) = self.tool_boundary(snapshot)? else {
            return Ok(false);
        };
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
        let _state = self.job_lock(s(snapshot, "id"), "state.lock")?;
        let external = snapshot["pid"].as_u64() != Some(self.rt.pid() as u64);
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
        let job = self.load(s(snapshot, "id"))?;
        if job["started_at"] != snapshot["started_at"]
            || job["codex_pid_created"] != snapshot["codex_pid_created"]
            || !self.should_yield(&job, &self.admission_jobs()?)
            || self.owns_civ6_lease(&job)?
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
        agent.stop()?;
        journal["stopped"] = json!(true);
        self.write_json(&journal_path, &journal)?;
        if let Some(owner) = owner.as_mut() {
            owner.stop()?;
        }
        self.commit_tool_yield(&journal)?;
        if external {
            self.close_chrome_tabs(s(&job, "id"));
        }
        Ok(true)
    }
    fn prepare_tool_yield(&self, job: &mut Value, item: &str) -> Result<()> {
        let session = normalize_uuid(s(job, "thread_id"))
            .ok_or_else(|| Error::new("missing yield session"))?;
        fs::write(
            self.job_dir(s(job, "id")).join("yield-resume.md"),
            "実行枠を他の作業に譲った後の再開です。同じセッションと作業フォルダで、ここまでの成果・最新の指示を引き継いで未完了の作業を続けてください。停止前のコマンドとツールは完了しています。成果と共有ToDoを確認し、完了済みの作業や送信を繰り返さず、続きを進めてください。\n",
        )?;
        job["yield_last_message"] = job["last_message"].clone();
        job["yield_count"] = json!(number(job, "yield_count") + 1);
        job["last_yield_at"] = json!(self.rt.now());
        job["yield_boundary_item"] = json!(item);
        job["yield_resume"] = json!(true);
        prepare_resume(job, &session, &self.rt.now());
        job["queue_sequence"] = json!(self.next_queue_sequence()?);
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!("順番待ち: ツール完了後に枠を譲りました");
        Ok(())
    }
    fn commit_tool_yield(&self, journal: &Value) -> Result<()> {
        let source = journal["source"].clone();
        let mut job = journal["queued"].clone();
        let id = s(&source, "id");
        if self.is_ours(&source, "codex_pid")
            || (source["pid"].as_u64() != Some(self.rt.pid() as u64)
                && self.is_ours(&source, "pid"))
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
        if source["pid"].as_u64() != Some(self.rt.pid() as u64) {
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
        if self.is_ours(&source, "pid") && source["pid"].as_u64() != Some(self.rt.pid() as u64) {
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

#[cfg(test)]
mod tests {
    use super::*;
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
