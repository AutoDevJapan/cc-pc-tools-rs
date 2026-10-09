//! Current-boot recovery. Normal failures and deliberate cancellation are terminal.
use crate::{
    queue::{prepare_resume, same_task},
    *,
};
use std::{
    fs,
    io::{Read, Seek, SeekFrom},
    path::Path,
    time::{Duration, Instant},
};

fn same_boot(value: &Value, boot: f64) -> bool {
    value
        .as_f64()
        .is_some_and(|saved| (saved - boot).abs() < 5.)
}

fn before_boot(job: &Value, boot: f64) -> bool {
    // Creation alone is insufficient: a years-old session may have run today.
    parse_iso(s(job, "created_at")).is_some_and(|created| created < boot)
        && ["created_at", "queued_at", "started_at", "last_event_at"]
            .iter()
            .all(|key| !truth(&job[*key]) || parse_iso(s(job, key)).is_some_and(|at| at < boot))
}

fn candidate(job: &Value, boot: f64) -> bool {
    if job["cancel_requested"] == true || truth(&job["turn_completed"]) {
        return false;
    }
    if s(job, "status") == "queued" && job["reboot_launch_pending"] == true {
        return true;
    }
    if same_boot(&job["reboot_recovered_boot"], boot) || !before_boot(job, boot) {
        return false;
    }
    if LIVE.contains(&s(job, "status")) {
        return true;
    }
    // Legacy lost records have no provenance field. Require the disappearance
    // reason plus a post-boot observation, never an ordinary failed record.
    s(job, "status") == "lost"
        && parse_iso(s(job, "finished_at")).is_some_and(|at| at >= boot)
        && (job["reboot_interrupted"] == true
            || [
                "scheduler: both job roots are gone",
                "supervisor exited without recording a result",
            ]
            .contains(&s(job, "last_error"))
            || s(job, "last_error").contains("502"))
}

fn same_assignment(a: &Value, b: &Value) -> bool {
    (a["todo_id"].as_u64().is_some() && a["todo_id"] == b["todo_id"])
        || (!s(a, "launch_key").is_empty() && a["launch_key"] == b["launch_key"])
        || ["thread_id", "resume_session"].iter().any(|key| {
            normalize_uuid(s(a, key)).is_some_and(|id| {
                ["thread_id", "resume_session"]
                    .iter()
                    .any(|other| normalize_uuid(s(b, other)).as_ref() == Some(&id))
            })
        })
}

fn ended(path: &Path, job: &Value) -> Result<bool> {
    // Ignore a partial final line from a power loss, but retain the last complete
    // event. A reported transient error is not a turn.failed event.
    let mut file = match fs::File::open(path) {
        Ok(file) => file,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(false),
        Err(e) => return Err(e.into()),
    };
    let end = file.seek(SeekFrom::End(0))?;
    let start = end.saturating_sub(65536);
    file.seek(SeekFrom::Start(start))?;
    let mut bytes = Vec::new();
    file.read_to_end(&mut bytes)?;
    let event = bytes
        .split(|byte| *byte == b'\n')
        .rev()
        .find_map(|line| serde_json::from_slice::<Value>(line).ok());
    Ok(event.is_some_and(|event| {
        let attempt = if truth(&job["started_at"]) {
            "started_at"
        } else {
            "queued_at"
        };
        let earlier_attempt = parse_iso(s(&event, "_ts"))
            .zip(parse_iso(s(job, attempt)))
            .is_some_and(|(event, started)| event < started);
        !earlier_attempt && ["turn.completed", "turn.failed"].contains(&s(&event, "type"))
    }))
}

impl Context {
    /// Called before the existing minute tick reconciles lost work. Keeping the
    /// same ID preserves scheduler/ToDo/launch-key references and queue limits.
    pub fn recover_reboot(&self, apply: bool) -> Result<Value> {
        // Start the clock before preparation/locks, reserving time for the
        // single quiet report inside the caller's 20-second recovery stage.
        let deadline = Instant::now() + Duration::from_secs(12);
        let Some(boot) = self.rt.boot_epoch().filter(|at| at.is_finite() && *at > 0.) else {
            return Ok(json!([]));
        };
        fs::create_dir_all(&self.root)?;
        let _recovery = match FileLock::try_acquire(&self.root.join("reboot-recovery.lock")) {
            Ok(lock) => lock,
            Err(e)
                if matches!(e.raw_os_error(), Some(32 | 33))
                    || e.kind() == std::io::ErrorKind::WouldBlock =>
            {
                return Ok(json!([]));
            }
            Err(e) => return Err(e.into()),
        };
        let _gate = self.queue_lock_for("recover-reboot", Duration::from_secs(2))?;
        let receipt = self.root.join("reboot-recovery.json");
        if apply && receipt.exists() {
            let saved = self.read_json(&receipt)?;
            if same_boot(&saved["boot"], boot) && saved["complete"] == true {
                return Ok(json!([]));
            }
        }
        let mut jobs = self.reboot_inventory()?;
        let todo_path = self
            .root
            .parent()
            .unwrap_or(&self.root)
            .join("todo/todo.json");
        let todo = if todo_path.exists() {
            self.read_json(&todo_path)?
        } else {
            Value::Null
        };
        let mut report = Vec::new();
        // Latest attempts first, so recovering one excludes its older duplicate.
        for index in (0..jobs.len()).rev() {
            let old = &jobs[index];
            if old["reboot_inventory_error"] == true {
                report.push(json!({"id":old["id"],"action":"deferred","reason":"履歴を読み取れないため次回再確認"}));
                continue;
            }
            if !candidate(old, boot) {
                continue;
            }
            let id = s(old, "id").to_string();
            if deadline.saturating_duration_since(Instant::now()) < Duration::from_secs(2) {
                report.push(json!({"id":id,"action":"deferred","reason":"今回の復旧時間の上限"}));
                continue;
            }
            match self.rt.recovery_protected(s(old, "cwd")) {
                Ok(true) => continue,
                Ok(false) => {}
                Err(_) => {
                    report.push(json!({"id":id,"action":"deferred","reason":"作業範囲を確認できないため次回再確認"}));
                    continue;
                }
            }
            if apply && self.tool_handoff_pending(old) {
                // The existing transition has its own state lock and durable
                // spawn protocol. Resolve it before claiming reboot recovery.
                match self.recover_tool_yield(old) {
                    Ok(_) => {
                        let fresh = self.load(&id)?;
                        jobs[index] = fresh;
                        report.push(json!({"id":id,"action":"deferred","reason":"既存の引き継ぎを確認したため次回再確認"}));
                    }
                    Err(_) => report
                        .push(json!({"id":id,"action":"deferred","reason":"既存の引き継ぎ処理中"})),
                }
                continue;
            }
            let _state = match FileLock::try_acquire(&self.job_dir(&id).join("state.lock")) {
                Ok(lock) => lock,
                Err(e)
                    if matches!(e.raw_os_error(), Some(32 | 33))
                        || e.kind() == std::io::ErrorKind::WouldBlock =>
                {
                    report.push(json!({"id":id,"action":"deferred","reason":"状態の更新中"}));
                    continue;
                }
                Err(e) => return Err(e.into()),
            };
            let mut job = self.load(&id)?;
            if s(&job, "cwd") != s(old, "cwd") {
                report.push(json!({"id":id,"action":"deferred","reason":"作業フォルダーが変更されたため次回再確認"}));
                continue;
            }
            if !candidate(&job, boot)
                || self.is_ours(&job, "pid")
                || self.is_ours(&job, "codex_pid")
            {
                continue;
            }
            if self.tool_handoff_pending(&job) {
                report.push(json!({"id":id,"action":"deferred","reason":"既存の引き継ぎ処理中"}));
                continue; // The durable yield protocol owns this transition.
            }
            let owner = match FileLock::try_acquire(&self.job_dir(&id).join("supervisor.lock")) {
                Ok(lock) => lock,
                Err(e)
                    if matches!(e.raw_os_error(), Some(32 | 33))
                        || e.kind() == std::io::ErrorKind::WouldBlock =>
                {
                    report.push(json!({"id":id,"action":"deferred","reason":"監督の交代中"}));
                    continue;
                }
                Err(e) => return Err(e.into()),
            };
            let item = todo["items"].as_array().and_then(|items| {
                items.iter().find(|item| {
                    item["id"] == job["todo_id"]
                        || item["jobs"]
                            .as_array()
                            .is_some_and(|ids| ids.contains(&job["id"]))
                })
            });
            let reason = if item
                .is_some_and(|item| ["done", "block", "wait"].contains(&s(item, "status")))
            {
                Some("共有ToDoが完了、判断待ち、または別の作業の完了待ち")
            } else if jobs.iter().any(|other| {
                s(other, "id") != id
                    && ((same_task(&job, other)
                        && LIVE.contains(&s(other, "status"))
                        && !candidate(other, boot))
                        || (same_assignment(&job, other)
                            && (s(other, "created_at"), s(other, "id"))
                                > (s(&job, "created_at"), id.as_str())))
            }) {
                Some("後続の担当がある")
            } else if !self.job_dir(&id).join("prompt.md").is_file()
                || !Path::new(s(&job, "cwd")).is_dir()
            {
                Some("元の依頼文または作業フォルダーがない")
            } else if ended(&self.job_dir(&id).join("events.jsonl"), &job)? {
                Some("終了イベントを記録済み")
            } else {
                None
            };
            if let Some(reason) = reason {
                if reason == "元の依頼文または作業フォルダーがない" {
                    report.push(json!({"id":id,"action":"deferred","reason":reason}));
                    continue;
                }
                if apply && job["reboot_launch_pending"] == true {
                    // A terminal ToDo or successor invalidates an unlaunched
                    // reservation. Release its slot without starting work.
                    job["reboot_launch_pending"] = json!(false);
                    job["queue_restart_pending"] = json!(false);
                    job["reboot_log_pending"] = json!(false);
                    job["status"] = json!("lost");
                    job["finished_at"] = json!(self.rt.now());
                    job["last_error"] = job["reboot_original_error"].clone();
                    job["reboot_recovery_aborted"] = json!(reason);
                    self.save(&job)?;
                    jobs[index] = job;
                }
                report.push(json!({"id":id,"action":"skip","reason":reason}));
                continue;
            }
            if !apply {
                report.push(json!({"id":id,"name":job["name"],"action":"would_queue"}));
                continue;
            }
            if job["reboot_launch_pending"] != true
                || !same_boot(&job["reboot_recovered_boot"], boot)
            {
                let session = ["thread_id", "resume_session"]
                    .iter()
                    .find_map(|key| normalize_uuid(s(&job, key)));
                job["reboot_original_status"] = job["status"].clone();
                job["reboot_original_error"] = job["last_error"].clone();
                prepare_resume(&mut job, session.as_deref().unwrap_or(""), &self.rt.now());
                job["reboot_recovered_boot"] = json!(boot);
                job["reboot_recovered_at"] = json!(self.rt.now());
                job["reboot_launch_pending"] = json!(true);
                job["reboot_log_pending"] = json!(true);
                job["queue_restart_pending"] = json!(true);
                job["retry_not_before"] = Value::Null;
                job["pid"] = Value::Null;
                job["pid_created"] = Value::Null;
                self.save(&job)?;
            }
            self.track_queued_job(&id)?;
            drop(owner);
            match self.rt.spawn_supervisor_identified(
                &self.executable,
                &self.root,
                &self.job_dir(&id),
                &id,
            ) {
                Ok((_, pid)) => {
                    if let Some(pid) = pid {
                        job["pid"] = json!(pid);
                        job["pid_created"] = json!(self.rt.proc_created(pid));
                    }
                    job["reboot_launch_pending"] = json!(false);
                    self.save(&job)?;
                    report.push(json!({"id":id,"name":job["name"],"action":"queued"}));
                }
                Err(error) => {
                    // Keep the durable reservation; do not revert to lost or
                    // permit the ToDo watchdog to invent a second job.
                    job["reboot_launch_error"] =
                        json!(redact_local_log_secrets(&error.to_string(), &self.rt.env()));
                    self.save(&job)?;
                    report.push(json!({"id":id,"action":"pending"}));
                }
            }
            jobs[index] = job;
        }
        drop(_gate);
        let launches_pending = jobs.iter().any(|job| {
            s(job, "status") == "queued"
                && job["cancel_requested"] != true
                && job["reboot_launch_pending"] == true
        });
        let deferred = report.iter().any(|entry| entry["action"] == "deferred");
        if apply && !launches_pending && !deferred {
            self.log_reboot_recovery(&jobs)?;
            if !report.iter().any(|entry| entry["action"] == "pending") {
                // Avoid reading thousands of historical jobs on every minute
                // tick after this boot's recovery and quiet report are complete.
                self.write_json(
                    &receipt,
                    &json!({"boot":boot,"complete":true,
                    "completed_at":self.rt.now(),"report":report}),
                )?;
            }
        }
        Ok(json!(report))
    }

    fn reboot_inventory(&self) -> Result<Vec<Value>> {
        let entries = match fs::read_dir(&self.jobs) {
            Ok(entries) => entries,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
            Err(e) => return Err(e.into()),
        };
        let mut jobs = Vec::new();
        for entry in entries {
            let entry = entry?;
            let id = entry.file_name().to_string_lossy().into_owned();
            if !entry.file_type()?.is_dir() || !crate::queue::valid_job_id(&id) {
                continue;
            }
            match fs::metadata(entry.path().join("job.json")) {
                Ok(_) => match self.load(&id) {
                    Ok(job) => jobs.push(job),
                    Err(_) => jobs
                        .push(json!({"id":id,"status":"unreadable","reboot_inventory_error":true})),
                },
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e.into()),
            }
        }
        jobs.sort_by(|a, b| {
            (s(a, "created_at"), s(a, "id")).cmp(&(s(b, "created_at"), s(b, "id")))
        });
        Ok(jobs)
    }

    fn log_reboot_recovery(&self, jobs: &[Value]) -> Result<()> {
        let pending: Vec<_> = jobs
            .iter()
            .filter(|job| job["reboot_log_pending"] == true && job["reboot_launch_pending"] != true)
            .collect();
        if pending.is_empty() {
            return Ok(());
        }
        let names = pending
            .iter()
            .map(|job| s(job, "name").replace(['\r', '\n'], " "))
            .collect::<Vec<_>>()
            .join("、");
        let body = format!(
            "PCの再起動で途切れたCodexジョブ{}件を自動で再開しました。\n{}",
            pending.len(),
            names
        );
        let executable = self
            .root
            .parent()
            .unwrap_or(&self.root)
            .join("discord-ask/discord_ask.exe");
        let result = self.rt.call(Call::new(
            vec![
                executable.to_string_lossy().into(),
                "log".into(),
                "--topic".into(),
                "後片付けの記録".into(),
                "--text".into(),
                body,
            ],
            vec![],
            3.,
        ))?;
        if result.code != 0 {
            return Err(Error::new("reboot recovery log failed; retry next tick"));
        }
        for job in pending {
            let _state = self.job_lock(s(job, "id"), "state.lock")?;
            let mut fresh = self.load(s(job, "id"))?;
            fresh["reboot_log_pending"] = json!(false);
            fresh["reboot_logged_at"] = json!(self.rt.now());
            self.save(&fresh)?;
        }
        Ok(())
    }
}
