use crate::queue::{same_task, valid_job_id};
use crate::*;
use std::{
    collections::{HashMap, HashSet},
    fs,
    io::{BufRead, BufReader},
    path::Path,
};

impl Context {
    /// Legacy parentage requires a successful launcher command printing the ID.
    /// Mere mentions in status/cancel commands or job names are not evidence.
    pub fn recover_children(&self, since: &str, apply: bool) -> Result<Value> {
        let since = parse_iso(since).ok_or_else(|| Error::new("--since must be RFC3339"))?;
        self.queue_config()?;
        let _gate = self.queue_lock()?;
        let mut jobs = self.list_jobs(&HashSet::new());
        let mut launches = HashMap::<String, HashSet<String>>::new();
        for parent in jobs.iter().filter(|j| self.running_job(j)) {
            let path = self.job_dir(s(parent, "id")).join("events.jsonl");
            let file = match fs::File::open(path) {
                Ok(file) => file,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e.into()),
            };
            for line in BufReader::new(file).lines() {
                let Ok(event) = serde_json::from_str::<Value>(&line?) else {
                    continue;
                };
                let item = &event["item"];
                if s(&event, "type") != "item.completed"
                    || s(item, "type") != "command_execution"
                    || item["exit_code"] != 0
                    || !s(item, "command").contains("codex_jobs")
                    || !s(item, "command").contains(" run ")
                {
                    continue;
                }
                for id in s(item, "aggregated_output").lines().map(str::trim) {
                    if valid_job_id(id) {
                        launches
                            .entry(id.into())
                            .or_default()
                            .insert(s(parent, "id").into());
                    }
                }
            }
        }
        let mut report = Vec::new();
        // Latest attempts first; do not resurrect superseded review work.
        for index in (0..jobs.len()).rev() {
            let job = jobs[index].clone();
            let cancelled = s(&job, "status") == "cancelled"
                && job["cancel_requested"] == true
                && !truth(&job["started_at"])
                && !truth(&job["first_started_at"])
                && parse_iso(s(&job, "cancel_requested_at")).is_some_and(|t| t >= since);
            if !cancelled && s(&job, "status") != "queued" {
                continue;
            }
            let id = s(&job, "id");
            let parent = if !s(&job, "parent_job_id").is_empty() {
                s(&job, "parent_job_id").to_string()
            } else if let Some(ids) = launches.get(id).filter(|ids| ids.len() == 1) {
                ids.iter().next().unwrap().clone()
            } else {
                continue;
            };
            if id == parent || !self.running_job(&self.load(&parent)?) {
                continue;
            }
            if cancelled
                && jobs.iter().any(|other| {
                    s(other, "id") != id
                        && (LIVE.contains(&s(other, "status"))
                            || self.is_ours(other, "codex_pid")
                            || (s(other, "created_at"), s(other, "id"))
                                > (s(&job, "created_at"), id))
                        && same_task(&job, other)
                })
            {
                report.push(json!({"id":id,"action":"skip","reason":"後続の担当がある"}));
                continue;
            }
            if apply {
                let _state = self.job_lock(id, "state.lock")?;
                let mut fresh = self.load(id)?;
                if cancelled {
                    if truth(&fresh["started_at"])
                        || truth(&fresh["first_started_at"])
                        || s(&fresh, "status") != "cancelled"
                        || fresh["cancel_requested"] != true
                        || self.is_ours(&fresh, "pid")
                        || self.is_ours(&fresh, "codex_pid")
                        || !self.job_dir(id).join("prompt.md").is_file()
                        || !Path::new(s(&fresh, "cwd")).is_dir()
                        || !self.running_job(&self.load(&parent)?)
                    {
                        continue;
                    }
                    let Ok(owner) =
                        FileLock::try_acquire(&self.job_dir(id).join("supervisor.lock"))
                    else {
                        continue;
                    };
                    fresh["recovered_child_at"] = json!(self.rt.now());
                    fresh["cancel_requested"] = json!(false);
                    fresh["cancel_requested_at"] = Value::Null;
                    fresh["status"] = json!("queued");
                    fresh["queued_at"] = json!(self.rt.now());
                    fresh["activity"] = json!("順番待ち");
                    fresh["last_event_at"] = json!(self.rt.now());
                    for field in [
                        "pid",
                        "pid_created",
                        "finished_at",
                        "exit_code",
                        "duration_ms",
                        "last_error",
                        "gto",
                        "retry_not_before",
                    ] {
                        fresh[field] = Value::Null;
                    }
                    fresh["parent_job_id"] = json!(parent);
                    self.save(&fresh)?;
                    self.track_queued_job(id)?;
                    drop(owner);
                    match self.rt.spawn_supervisor_identified(
                        &self.executable,
                        &self.root,
                        &self.job_dir(id),
                        id,
                    ) {
                        Ok((_, pid)) => {
                            if let Some(pid) = pid {
                                fresh["pid"] = json!(pid);
                                fresh["pid_created"] = json!(self.rt.proc_created(pid));
                                self.save(&fresh)?;
                            }
                        }
                        Err(e) => {
                            self.save(&job)?;
                            return Err(e.into());
                        }
                    }
                } else {
                    if s(&fresh, "status") != "queued" || fresh["cancel_requested"] == true {
                        continue;
                    }
                    fresh["parent_job_id"] = json!(parent);
                    self.save(&fresh)?;
                }
                jobs[index] = fresh;
            }
            report.push(json!({"id":id,"parent_job_id":parent,"action":if cancelled {if apply {"queued"} else {"would_queue"}} else {if apply {"linked"} else {"would_link"}}}));
        }
        Ok(json!(report))
    }
}
