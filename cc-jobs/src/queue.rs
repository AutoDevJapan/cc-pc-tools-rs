use crate::*;
use std::io::{Read, Seek, SeekFrom};
use std::path::Path;
use std::{
    collections::{HashMap, HashSet},
    fs,
};

#[derive(Clone, serde::Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct QueueConfig {
    #[serde(skip)]
    pub(crate) admission_limits: Option<(usize, usize, usize, bool)>,
    pub context_limit_percent: u8,
    pub max_running: usize,
    pub fixed_max_running: Option<usize>,
    pub adaptive: Option<crate::adaptive::AdaptiveConfig>,
    pub cargo_slots: Option<usize>,
    pub cargo_jobs_per_build: Option<usize>,
    pub test_threads: Option<usize>,
    pub max_children_per_parent: usize,
    pub max_429_retries: u32,
    pub retry_delay_seconds: u64,
    pub max_retry_delay_seconds: u64,
    pub lowest_priority_todo_ids: Vec<u64>,
    /// Explicit exceptions to both the ID list and text matching.
    pub normal_priority_todo_ids: Vec<u64>,
}
impl Default for QueueConfig {
    fn default() -> Self {
        Self {
            admission_limits: None,
            context_limit_percent: 50,
            max_running: 6,
            fixed_max_running: None,
            adaptive: None,
            cargo_slots: None,
            cargo_jobs_per_build: None,
            test_threads: None,
            max_children_per_parent: 2,
            max_429_retries: 5,
            retry_delay_seconds: 60,
            max_retry_delay_seconds: 900,
            lowest_priority_todo_ids: Vec::new(),
            normal_priority_todo_ids: Vec::new(),
        }
    }
}
/// Apply a partial config update, retaining every existing field. A running
/// owner's fixed limit survives omitted or null fields in deployment patches.
pub fn merge_queue_config(current: &Value, patch: &Value, clear_fixed: bool) -> Result<Value> {
    if !current.is_object() || !patch.is_object() {
        return Err(Error::new("queue configuration and patch must be objects"));
    }
    if !clear_fixed
        && !current["fixed_max_running"].is_null()
        && !patch["fixed_max_running"].is_null()
        && current["fixed_max_running"] != patch["fixed_max_running"]
    {
        return Err(Error::new(
            "fixed_max_running is already set; retain the owner's override",
        ));
    }
    let mut merged = current.clone();
    for (key, value) in patch.as_object().unwrap() {
        if key == "fixed_max_running" {
            continue;
        }
        if key == "adaptive" && value.is_object() && merged[key].is_object() {
            for (name, value) in value.as_object().unwrap() {
                merged[key][name] = value.clone();
            }
        } else {
            merged[key] = value.clone();
        }
    }
    if clear_fixed {
        merged.as_object_mut().unwrap().remove("fixed_max_running");
    } else if current["fixed_max_running"].is_null() && !patch["fixed_max_running"].is_null() {
        merged["fixed_max_running"] = patch["fixed_max_running"].clone();
    }
    let cfg: QueueConfig = serde_json::from_value(merged.clone())?;
    cfg.validate()?;
    Ok(merged)
}
impl QueueConfig {
    pub fn validate(&self) -> Result<()> {
        cc_job_policy::cargo::QueueCargoLimits {
            cargo_slots: self.cargo_slots,
            cargo_jobs_per_build: self.cargo_jobs_per_build,
            test_threads: self.test_threads,
        }
        .validate()?;
        if self.max_running == 0
            || !(1..=100).contains(&self.context_limit_percent)
            || self.max_children_per_parent == 0
            || self.retry_delay_seconds == 0
            || self.max_retry_delay_seconds < self.retry_delay_seconds
            || self.fixed_max_running == Some(0)
        {
            return Err(Error::new("invalid queue configuration"));
        }
        if let Some(adaptive) = &self.adaptive {
            adaptive.validate()?;
        }
        Ok(())
    }
}
impl Context {
    pub fn update_queue_config(&self, patch: &Value, clear_fixed: bool) -> Result<Value> {
        fs::create_dir_all(&self.root)?;
        let _lock = FileLock::acquire(&self.root.join("queue-config.lock"))?;
        let path = self.root.join("queue-config.json");
        let current = match self.read_file(&path) {
            Ok(bytes) => serde_json::from_slice(&bytes)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => json!({}),
            Err(e) => return Err(Error::file_io("read queue configuration", &path, e)),
        };
        let merged = merge_queue_config(&current, patch, clear_fixed)?;
        if clear_fixed && !current["fixed_max_running"].is_null() {
            let cfg: QueueConfig = serde_json::from_value(merged.clone())?;
            let adaptive = cfg
                .adaptive
                .as_ref()
                .ok_or_else(|| Error::new("automatic control is disabled"))?;
            let state: crate::adaptive::LimitState = self
                .read_json(&self.root.join("queue-limit-state.json"))
                .and_then(|v| serde_json::from_value(v).map_err(Error::from))?;
            let now = self.rt.epoch();
            if state.hardware_max_running < 16
                || state.auto_ready_samples < 3
                || state.settings.as_ref() != Some(adaptive)
                || now < state.checked_at
                || now - state.checked_at > adaptive.stale_after_seconds()
            {
                return Err(Error::new(
                    "fixed limit retained: need three fresh healthy automatic samples at 16 or more",
                ));
            }
        }
        if merged != current {
            let changes = if clear_fixed && !current["fixed_max_running"].is_null() {
                Some(
                    fs::OpenOptions::new()
                        .create(true)
                        .append(true)
                        .open(self.root.join("queue-config.CHANGES.txt"))?,
                )
            } else {
                None
            };
            self.write_json(&path, &merged)?;
            if let Some(mut changes) = changes {
                use std::io::Write;
                writeln!(
                    changes,
                    "{} fixed_max_runningを解除: CPU・空きメモリ・応答を実測し自動上限16以上を連続3回確認",
                    self.rt.now()
                )?;
                changes.sync_all()?;
            }
        }
        Ok(merged)
    }
    /// Admission gate serializes this counter, including equal-second arrivals.
    pub(crate) fn next_queue_sequence(&self) -> Result<u64> {
        let path = self.jobs.join(".queue-sequence.json");
        let previous = if path.exists() {
            self.read_json(&path)?["sequence"]
                .as_u64()
                .ok_or_else(|| Error::new("invalid queue sequence"))?
        } else {
            0
        };
        let sequence = previous
            .checked_add(1)
            .ok_or_else(|| Error::new("queue sequence exhausted"))?;
        self.write_json(&path, &json!({"sequence":sequence}))?;
        Ok(sequence)
    }
    /// Called WITHOUT the global gate. Slow work (pausing, stopping, restarting)
    /// runs under each job's own yield reservation; only the short recovery and
    /// final commit steps take the gate, with bounded holds.
    fn maintain_tool_yields(&self, jobs: &[Value]) {
        for job in jobs {
            if s(job, "status") == "succeeded"
                && job["queue_fairness_version"].as_u64().unwrap_or(0) < 1
                && let Err(error) = self.prefetch_context_inventory(job)
            {
                self.yield_diagnostic(&error);
                continue;
            }
            let recovered = {
                let Some(_reservation) = self.yield_reservation(s(job, "id")) else {
                    continue;
                };
                let fresh = match self.load(s(job, "id")) {
                    Ok(fresh) => fresh,
                    Err(error) => {
                        self.yield_diagnostic(&error);
                        continue;
                    }
                };
                let job = &fresh;
                if !self.tool_handoff_pending(job)
                    && let Err(error) = self
                        .rt
                        .recover_paused(&self.job_dir(s(job, "id")).join("yield-paused.json"))
                {
                    self.yield_diagnostic(&error.into());
                    continue;
                }
                if self.tool_handoff_pending(job) {
                    self.queue_lock_for("recover_tool_yield", std::time::Duration::from_secs(60))
                        .and_then(|_gate| self.recover_tool_yield(job))
                } else {
                    Ok(false)
                }
            };
            let result = recovered.and_then(|recovered| {
                if recovered {
                    Ok(recovered)
                } else {
                    self.yield_tool_boundary(job, jobs)
                }
            });
            if let Err(error) = result {
                self.yield_diagnostic(&error);
            }
        }
    }
    /// Called under the admission gate: only the short legacy ledger remains. A
    /// separate ledger survives writes by supervisors deployed before
    /// tool-boundary fairness existed.
    fn maintain_yields(&self, jobs: &[Value]) -> Result<()> {
        let path = self.jobs.join(".queue-yields.json");
        let mut ledger = match self.read_file(&path) {
            Ok(bytes) => serde_json::from_slice::<HashMap<String, Value>>(&bytes)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => HashMap::new(),
            Err(e) => return Err(Error::file_io("read entire file", &path, e)),
        };
        let before = ledger.clone();
        // Snapshot before taking any per-job lock (global gate always comes first).
        for job in jobs {
            if job["queue_fairness_version"].as_u64().unwrap_or(0) < 1
                && self.should_yield(job, jobs)
            {
                let reservation = ledger.entry(s(job, "id").into()).or_insert_with(
                    || json!({"started_at":job["started_at"],"thread_id":job["thread_id"],"requested_at":self.rt.now()}),
                );
                reservation["waiters"] = json!(
                    jobs.iter()
                        .filter(|other| {
                            s(other, "status") == "queued" && s(other, "parent_job_id").is_empty()
                        })
                        .map(|other| json!({"id":other["id"],"queued_at":queue_time(other)}))
                        .collect::<Vec<_>>()
                );
            }
        }
        for id in ledger.keys().cloned().collect::<Vec<_>>() {
            if !valid_job_id(&id) {
                self.rt
                    .diagnostic("invalid yield ledger job id; discarding reservation");
                ledger.remove(&id);
                continue;
            }
            if !self.job_dir(&id).is_dir() {
                ledger.remove(&id);
                continue;
            }
            match self.resume_legacy_yield(&id, &ledger[&id], jobs) {
                Ok(true) => {
                    ledger.remove(&id);
                }
                Ok(false) => (),
                Err(error) => self.yield_diagnostic(&error),
            }
        }
        if ledger != before {
            self.write_json(&path, &json!(ledger))?;
        }
        Ok(())
    }

    fn resume_legacy_yield(&self, id: &str, reservation: &Value, jobs: &[Value]) -> Result<bool> {
        let _state = self.job_lock(id, "state.lock")?;
        let mut job = self.load(id)?;
        if job["queue_fairness_version"].as_u64().unwrap_or(0) >= 1
            || job["started_at"] != reservation["started_at"]
            || job["thread_id"] != reservation["thread_id"]
        {
            return Ok(true);
        }
        if s(&job, "status") == "running" {
            return Ok(!self.should_yield(&job, jobs));
        }
        if s(&job, "status") != "succeeded" || job["cancel_requested"] == true {
            return Ok(true);
        }
        // End time (not today's time) governs eligibility. A future waiter
        // cannot resurrect an already completed and notified legacy job.
        if parse_iso(s(&job, "finished_at")).is_none()
            || self.list_jobs(&HashSet::new()).iter().any(|other| {
                s(other, "id") != id
                    && same_task(&job, other)
                    && (LIVE.contains(&s(other, "status"))
                        || (s(other, "created_at"), s(other, "id")) > (s(&job, "created_at"), id))
            })
        {
            return Ok(true);
        }
        if self.is_ours(&job, "codex_pid") {
            return Ok(false);
        }
        let Ok(owner) = FileLock::try_acquire(&self.job_dir(id).join("supervisor.lock")) else {
            return Ok(false);
        };
        let original = job.clone();
        let boundary_jobs: Vec<_> = jobs
            .iter()
            .filter(|other| {
                s(other, "status") != "queued"
                    || !s(other, "parent_job_id").is_empty()
                    || reservation["waiters"].as_array().is_some_and(|waiters| {
                        waiters.iter().any(|waiter| {
                            s(waiter, "id") == s(other, "id")
                                && s(waiter, "queued_at") == queue_time(other)
                        })
                    })
            })
            .cloned()
            .collect();
        if !self.yield_completed_turn(&mut job, &boundary_jobs)? {
            return Ok(true);
        }
        // The legacy supervisor finished; take ownership without stopping it.
        job["pid"] = Value::Null;
        job["pid_created"] = Value::Null;
        self.track_queued_job(id)?;
        self.save(&job)?;
        drop(owner);
        if let Err(error) = self.restart_queued_supervisor(&mut job) {
            self.save(&original)?;
            return Err(error);
        }
        Ok(true)
    }

    pub(crate) fn yield_diagnostic(&self, error: &Error) {
        self.rt.diagnostic(&format!(
            "枠を譲る処理を延期: {}",
            redact_local_log_secrets(&error.message, &self.rt.env())
        ));
    }

    fn has_pending_descendants(&self, job: &Value, jobs: &[Value]) -> bool {
        jobs.iter().any(|other| {
            s(other, "id") != s(job, "id")
                && (other["queue_unreadable"] == true
                    || ((LIVE.contains(&s(other, "status")) || self.is_ours(other, "codex_pid"))
                        && self.queue_root(other) == s(job, "id")))
        })
    }

    pub(crate) fn should_yield(&self, job: &Value, jobs: &[Value]) -> bool {
        let Ok(cfg) = self.queue_config() else {
            return false;
        };
        let priorities = self.queue_priorities(jobs, &cfg);
        let dependency_waits = self.dependency_waits(jobs);
        let priority = |j: &Value| priorities.get(s(j, "id")).copied().unwrap_or(1002);
        let at = parse_iso(s(job, "finished_at")).unwrap_or(self.rt.epoch());
        let long_running = parse_iso(s(job, "started_at")).is_some_and(|start| {
            job["duration_ms"]
                .as_f64()
                .map(|ms| ms / 1000.)
                .unwrap_or(at - start)
                >= 7200.
        });
        // Lowest-tier work never keeps a slot from higher tiers, however short
        // its run so far; equal tiers share slots after two hours.
        s(job, "parent_job_id").is_empty()
            && s(job, "context_phase") != "summarize"
            && s(job, "status") == "running"
            && job["cancel_requested"] != true
            && normalize_uuid(s(job, "thread_id")).is_some()
            && parse_iso(s(job, "started_at")).is_some()
            && !self.has_pending_descendants(job, jobs)
            && !jobs
                .iter()
                .any(|other| s(other, "id") != s(job, "id") && same_task(job, other))
            && jobs.iter().any(|other| {
                s(other, "id") != s(job, "id")
                    && s(other, "parent_job_id").is_empty()
                    && s(other, "status") == "queued"
                    && !dependency_waits.contains_key(s(other, "id"))
                    && priority(other) <= priority(job)
                    && (long_running || priority(other) / 10 < priority(job) / 10
                        || (priority(job) % 10 == 3 && priority(other) < priority(job)))
                    && other["cancel_requested"] != true
                    && self.supervisor_alive(other)
                    && other["retry_not_before"].as_f64().unwrap_or(0.) <= at
                    && (s(job, "finished_at").is_empty()
                        || parse_iso(queue_time(other)).is_some_and(|queued| queued <= at))
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
                .is_some_and(|line| ["状態: 完了", "状態: 要判断"].contains(&line.trim()))
        {
            return Ok(false);
        }
        let mut running = job.clone();
        running["status"] = json!("running");
        let rotate = self.context_rotation_due(&running, jobs);
        if !rotate && !self.should_yield(&running, jobs) {
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
        job["queue_sequence"] = json!(self.next_queue_sequence()?);
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!("順番待ち: 長時間の作業が枠を譲りました");
        if rotate {
            self.prepare_context_summary(job, &session)?;
        }
        Ok(true)
    }
    /// Upgrade waiting supervisors without stopping children or changing sessions.
    pub fn reload_queue(&self) -> Result<Value> {
        self.reload_queue_older_than(None, None)
    }
    /// Refresh only this registered controller; execution admission is still checked by its successor.
    pub fn reload_queue_job(&self, id: &str) -> Result<Value> {
        if !valid_job_id(id) {
            return Err(Error {
                code: 2,
                message: "invalid --job-id".into(),
            });
        }
        self.reload_queue_older_than(None, Some(id))
    }
    pub(crate) fn reload_legacy_queue(&self) -> Result<Value> {
        self.reload_queue_older_than(Some(5), None)
    }
    fn reload_queue_older_than(&self, version: Option<u64>, target: Option<&str>) -> Result<Value> {
        let mut restarted = Vec::new();
        let snapshots = match target {
            Some(id) => {
                let job = self.load(id)?;
                if job["id"] != id {
                    return Err(Error::new(
                        "requested supervisor record has a different job id",
                    ));
                }
                vec![job]
            }
            None => self.list_jobs(&HashSet::new()),
        };
        // Snapshot reads need no global gate. Release the gate between jobs so
        // a long reload cannot prevent unrelated registration for a full minute.
        for snapshot in snapshots {
            if s(&snapshot, "status") != "queued"
                || version.is_some_and(|v| {
                    snapshot["queue_supervisor_version"].as_u64().unwrap_or(0) >= v
                        && snapshot["queue_restart_pending"] != true
                })
            {
                continue;
            }
            let jid = s(&snapshot, "id");
            // Replacing a controller allocates no execution slot. A concurrent history scan
            // merges tracked IDs at commit; the new controller must still pass admission.
            let _gate = self.queue_lock_for("reload_queue", std::time::Duration::from_secs(60))?;
            let path = self.job_dir(jid).join("state.lock");
            let _state = match FileLock::try_acquire(&path) {
                Ok(state) => state,
                Err(error) if matches!(error.raw_os_error(), Some(32 | 33)) => {
                    self.rt
                        .diagnostic(&format!("見張りの更新を延期（状態保存中）: {jid}"));
                    continue;
                }
                Err(error) => {
                    return Err(Error::file_io(
                        "acquire exclusive lock byte range [0, 1)",
                        &path,
                        error,
                    ));
                }
            };
            let mut job = self.load(jid)?;
            if job["id"] != snapshot["id"] {
                return Err(Error::new("supervisor identity changed during refresh"));
            }
            if s(&job, "status") != "queued"
                || version.is_some_and(|v| {
                    job["queue_supervisor_version"].as_u64().unwrap_or(0) >= v
                        && job["queue_restart_pending"] != true
                })
                || job["cancel_requested"] == true
                || self.is_ours(&job, "codex_pid")
            {
                continue;
            }
            // Persist this registered ID before stopping the controller. A missing/corrupt
            // derived index is initialized as incomplete; execution still requires reconciliation.
            let refresh = self
                .track_queued_job(jid)
                .and_then(|()| self.restart_queued_supervisor(&mut job));
            match refresh {
                Ok(()) => restarted.push(json!(jid)),
                Err(error) => {
                    self.rt.diagnostic(&format!(
                        "見張りの更新を延期: {jid}: {}",
                        redact_local_log_secrets(&error.message, &self.rt.env())
                    ));
                    restarted.push(json!({"id":jid,"action":"deferred","error":redact_local_log_secrets(&error.message,&self.rt.env())}));
                }
            }
        }
        Ok(json!(restarted))
    }
    /// Caller holds the global gate and this job's state lock; no Codex is live.
    pub(crate) fn restart_queued_supervisor(&self, job: &mut Value) -> Result<()> {
        let executable = fs::metadata(&self.executable).map_err(|e| {
            Error::file_io(
                "inspect replacement supervisor executable",
                &self.executable,
                e,
            )
        })?;
        if !executable.is_file() {
            return Err(Error::new(
                "replacement supervisor executable is not a file",
            ));
        }
        job["queue_restart_pending"] = json!(true);
        self.save(job)?;
        let jid = s(job, "id").to_string();
        if self.is_ours(job, "pid") {
            // Detached supervisors launched by this process are independent
            // owners. Never follow Windows parent PID links during an upgrade.
            self.stop_supervisor_owner(job)?;
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
        let owner_path = self.job_dir(&jid).join("supervisor.lock");
        let owner = FileLock::acquire(&owner_path).map_err(|e| {
            Error::file_io("acquire exclusive lock byte range [0, 1)", &owner_path, e)
        })?;
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
        // Only the new controller acknowledges its version and clears pending.
        if let Some(pid) = pid {
            job["pid"] = json!(pid);
            job["pid_created"] = json!(self.rt.proc_created(pid));
        }
        self.save(job)?;
        Ok(())
    }
    pub(crate) fn stop_supervisor_owner(&self, job: &Value) -> Result<()> {
        match self
            .rt
            .pause_supervisor(
                job["pid"].as_u64().unwrap() as u32,
                job["pid_created"].as_u64().unwrap(),
            )
            .and_then(|mut owner| owner.stop())
        {
            Ok(()) => Ok(()),
            Err(_) if !self.is_ours(job, "pid") => Ok(()),
            Err(error) => Err(error.into()),
        }
    }
    fn index_ids<'a>(&self, index: &'a Value) -> Result<Vec<&'a str>> {
        index["ids"]
            .as_array()
            .ok_or_else(|| Error::new("queue index is not prepared"))?
            .iter()
            .map(|id| {
                id.as_str()
                    .filter(|id| valid_job_id(id))
                    .ok_or_else(|| Error::new("invalid queue index id"))
            })
            .collect()
    }
    fn read_admission_index(&self) -> Result<Value> {
        let path = self.jobs.join(".queue-index.json");
        match self.read_file(&path) {
            Ok(bytes) => Ok(serde_json::from_slice(&bytes).unwrap_or(Value::Null)),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Value::Null),
            Err(e) => Err(Error::file_io("read queue index", &path, e)),
        }
    }
    fn index_complete(&self, index: &Value) -> bool {
        self.index_ids(index).is_ok()
            && index["scan_complete"] == true
            && index["scanned_at"]
                .as_f64()
                .is_some_and(|at| at > 0. && at.is_finite())
    }
    fn index_fresh(&self, index: &Value) -> bool {
        self.index_complete(index)
            && index["scanned_at"].as_f64().is_some_and(|at| {
                let now = self.rt.epoch();
                now >= at && now - at < 30.
            })
    }
    /// Prepare history WITHOUT the queue gate. One scanner serves concurrent
    /// launchers; only active candidates and registrations are reread at commit.
    /// Lock order is scan -> queue -> state, never queue -> scan.
    pub(crate) fn prepare_admission_index(&self) -> Result<()> {
        fs::create_dir_all(&self.jobs)?;
        let path = self.jobs.join(".queue-index.json");
        if self.index_fresh(&self.read_admission_index()?) {
            return Ok(());
        }
        let scan_path = self.jobs.join(".queue-scan.lock");
        let started = std::time::Instant::now();
        let mut _scan = FileLock::try_acquire(&scan_path).map_err(|e| Error {
            code: 75,
            message: format!("history reconciliation is already running: {e}"),
        })?;
        _scan.set_owner(&scan_path, "history_scan:begin");
        let lock_wait_ms = started.elapsed().as_millis();
        if self.index_fresh(&self.read_admission_index()?) {
            return Ok(());
        }
        let begin_started = std::time::Instant::now();
        {
            let _gate =
                self.queue_lock_for("begin_history_scan", std::time::Duration::from_secs(60))?;
            let mut index = self.read_admission_index()?;
            if self.index_ids(&index).is_err() {
                index = json!({"ids":[], "scanned_at":0., "owners":{}});
            }
            // Durable publication point: registrations append here while the
            // scanner is outside the gate. A crashed scan remains incomplete.
            index["scan_complete"] = json!(false);
            self.write_json(&path, &index)?;
        }
        let begin_ms = begin_started.elapsed().as_millis();
        let mut candidates = HashSet::new();
        _scan.set_owner(&scan_path, "history_scan:enumerate");
        let enumerate_started = std::time::Instant::now();
        let entries = fs::read_dir(&self.jobs)?.collect::<std::io::Result<Vec<_>>>()?;
        let enumerate_ms = enumerate_started.elapsed().as_millis();
        let mut record_count = 0usize;
        let mut max_record_ms = 0u128;
        let records_started = std::time::Instant::now();
        _scan.set_owner(&scan_path, "history_scan:records");
        for entry in entries {
            if entry.file_type()?.is_dir() {
                let id = entry.file_name().to_string_lossy().into_owned();
                if valid_job_id(&id) && entry.path().join("job.json").is_file() {
                    let record_started = std::time::Instant::now();
                    let job = self.admission_record(&id);
                    record_count += 1;
                    if record_count.is_multiple_of(128) {
                        _scan
                            .set_owner(&scan_path, &format!("history_scan:records:{record_count}"));
                    }
                    max_record_ms = max_record_ms.max(record_started.elapsed().as_millis());
                    if self.admission_active(&job) {
                        candidates.insert(id);
                    }
                }
            }
        }
        let records_ms = records_started.elapsed().as_millis();
        _scan.set_owner(&scan_path, "history_scan:commit");
        let commit_started = std::time::Instant::now();
        let _gate =
            self.queue_lock_for("commit_history_scan", std::time::Duration::from_secs(60))?;
        // Do not turn access/parse failure into an empty index: it may contain
        // registrations made after the directory snapshot was collected.
        let index = self.read_json(&path)?;
        // Registrations/requeues occurring during the scan must not be lost,
        // including a formerly terminal job scanned before it was requeued.
        candidates.extend(self.index_ids(&index)?.into_iter().map(str::to_owned));
        let mut ids: Vec<_> = candidates.into_iter().collect();
        ids.sort();
        let active = ids.iter().map(|id| self.admission_record(id)).collect();
        self.save_admission_index(&index, active, self.rt.epoch())?;
        drop(_gate);
        let metrics = json!({"pid":std::process::id(),"record_count":record_count,"lock_wait_ms":lock_wait_ms as u64,"begin_ms":begin_ms as u64,"enumerate_ms":enumerate_ms as u64,"records_ms":records_ms as u64,"max_record_ms":max_record_ms as u64,"commit_ms":commit_started.elapsed().as_millis() as u64,"total_ms":started.elapsed().as_millis() as u64});
        let _ = self.write_json(&self.jobs.join(".queue-scan-metrics.json"), &metrics);
        self.rt
            .diagnostic(&format!("history scan metrics: {metrics}"));
        Ok(())
    }
    /// Called under the gate after preparation. Never reads terminal history.
    pub(crate) fn admission_jobs(&self) -> Result<Vec<Value>> {
        let index = self.read_json(&self.jobs.join(".queue-index.json"))?;
        // Preparation and gate acquisition are separate. A writer can repair
        // a damaged cache to a partial index between them; never admit from it.
        if !self.index_complete(&index) {
            return Err(Error {
                code: 75,
                message: "queue index reconciliation required; retry outside queue gate".into(),
            });
        }
        let active = self
            .index_ids(&index)?
            .into_iter()
            .map(|id| self.admission_record(id))
            .collect();
        self.save_admission_index(&index, active, index["scanned_at"].as_f64().unwrap_or(0.))
    }
    fn save_admission_index(
        &self,
        index: &Value,
        jobs: Vec<Value>,
        scanned_at: f64,
    ) -> Result<Vec<Value>> {
        let mut active: Vec<_> = jobs
            .into_iter()
            .filter(|j| self.admission_active(j))
            .collect();
        for job in &mut active {
            self.record_work_owner(job);
        }
        let owners: serde_json::Map<String, Value> = active
            .iter()
            .map(|j| (s(j, "id").to_owned(), admission_owner(j)))
            .collect();
        let next = json!({"scan_complete":true,"scanned_at":scanned_at,"ids":active.iter().map(|j|s(j,"id")).collect::<Vec<_>>(),"owners":owners});
        if next != *index {
            self.write_json(&self.jobs.join(".queue-index.json"), &next)?;
        }
        Ok(active)
    }
    fn admission_active(&self, j: &Value) -> bool {
        LIVE.contains(&s(j, "status"))
            || s(j, "status") == "starting"
            || self.is_ours(j, "codex_pid")
            || self.tool_handoff_pending(j)
    }
    /// Registration is durable intake, not execution admission. Read only the
    /// already published IDs; a supervisor reconciles legacy history before it
    /// may reserve any running slot. Never prune or complete a partial index.
    pub(crate) fn registration_jobs(&self) -> Result<Vec<Value>> {
        let index = self.read_admission_index()?;
        if self.index_ids(&index).is_err() {
            return Ok(Vec::new());
        }
        Ok(self
            .index_ids(&index)?
            .into_iter()
            .map(|id| self.admission_record(id))
            .filter(|job| self.admission_active(job))
            .collect())
    }
    /// Lock-free view of the active index for pre-checks. Never rescans the
    /// whole history or writes; None when no index exists yet.
    pub(crate) fn admission_snapshot(&self) -> Option<Vec<Value>> {
        let index = self.read_json(&self.jobs.join(".queue-index.json")).ok()?;
        if !self.index_complete(&index) {
            return None;
        }
        let ids = index["ids"].as_array()?;
        Some(
            ids.iter()
                .filter_map(Value::as_str)
                .filter(|id| valid_job_id(id))
                .map(|id| {
                    // A record being replaced reads as occupied, like admission does.
                    self.try_load(id).unwrap_or_else(
                        || json!({"id":id,"status":"running","queue_unreadable":true}),
                    )
                })
                .filter(|j| self.admission_active(j))
                .collect(),
        )
    }
    /// Lock-free estimate of `admit`: true only when the snapshot says this job
    /// still waits for exactly the reason already recorded, so the gate can be
    /// skipped. Any possible admission or changed reason goes through the gate.
    pub(crate) fn admission_wait_unchanged(&self, job: &Value, cfg: &QueueConfig) -> bool {
        let Some(mut jobs) = self.admission_snapshot() else {
            return false;
        };
        let jid = s(job, "id");
        jobs.retain(|j| s(j, "id") != jid);
        jobs.push(job.clone());
        self.queue_decisions(&jobs, cfg, Some(jid))
            .get(jid)
            .is_some_and(|reason| *reason == job["queue_wait_reason"])
    }
    /// Lock-free pre-check for a running job's yield attempt. Without an index
    /// the gated path decides.
    pub(crate) fn yield_may_be_due(&self, job: &Value) -> bool {
        self.tool_handoff_pending(job)
            || (!self.yield_backoff_active(job)
                && self.admission_snapshot().is_none_or(|jobs| {
                    self.context_rotation_due(job, &jobs) || self.should_yield(job, &jobs)
                }))
    }
    fn admission_record(&self, id: &str) -> Value {
        self.try_load(id).unwrap_or_else(|| {
            self.rt.diagnostic(&format!(
                "unreadable queue record {id}; retaining its root slot"
            ));
            let mut fallback = self
                .read_json(&self.jobs.join(".queue-index.json"))
                .ok()
                .and_then(|index| index["owners"][id].as_object().cloned())
                .map(Value::Object)
                .unwrap_or_else(|| json!({}));
            fallback["id"] = json!(id);
            fallback["status"] = json!("running");
            fallback["queue_unreadable"] = json!(true);
            fallback
        })
    }
    /// Register before exposing a newly queued record to a supervisor. Preserve
    /// registrations even while another process bootstraps a missing index.
    pub fn track_queued_job(&self, jid: &str) -> Result<()> {
        let path = self.jobs.join(".queue-index.json");
        let mut index = self.read_admission_index()?;
        if self.index_ids(&index).is_err() {
            // Keep the new ID durable; a stale timestamp forces reconciliation.
            index = json!({"ids":[], "scan_complete":false, "scanned_at":0., "owners":{}});
        }
        if !self.index_complete(&index) {
            // Preserve registrations, but only a completed full scan can publish completion.
            index["scan_complete"] = json!(false);
            index["scanned_at"] = json!(0.);
        }
        let ids = index["ids"].as_array_mut().unwrap();
        if !ids.iter().any(|id| id.as_str() == Some(jid)) {
            ids.push(json!(jid));
        }
        let mut job = self.load(jid)?;
        self.record_work_owner(&mut job);
        if !index["owners"].is_object() {
            index["owners"] = json!({});
        }
        index["owners"][jid] = admission_owner(&job);
        self.write_json(&path, &index)?;
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
        let mut cfg: QueueConfig = match self.read_file(&path) {
            Ok(bytes) => serde_json::from_slice(&bytes)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => QueueConfig::default(),
            Err(e) => return Err(Error::file_io("read entire file", &path, e)),
        };
        if cfg.max_running == 0
            || !(1..=100).contains(&cfg.context_limit_percent)
            || cfg.max_children_per_parent == 0
            || cfg.retry_delay_seconds == 0
            || cfg.max_retry_delay_seconds < cfg.retry_delay_seconds
        {
            return Err(Error::new(
                "queue-config.json: max_running, max_children_per_parent and retry_delay_seconds must be positive; max_retry_delay_seconds must be >= retry_delay_seconds",
            ));
        }
        if cfg.fixed_max_running == Some(0) {
            return Err(Error::new("fixed_max_running must be positive"));
        }
        if let Some(adaptive) = &cfg.adaptive {
            adaptive.validate()?;
        }
        cfg.max_running = self.effective_limit(&cfg)?;
        Ok(cfg)
    }

    /// The global gate covers admission and registration (including manual resumes).
    #[track_caller]
    pub fn queue_lock(&self) -> Result<FileLock> {
        self.prepare_admission_index()?;
        let caller = std::panic::Location::caller();
        self.queue_lock_for(
            &format!("{}:{}", caller.file(), caller.line()),
            std::time::Duration::from_secs(60),
        )
    }

    /// Return the state lock with a durably reserved slot, or None while waiting.
    /// Lock order everywhere is global gate, then job state.
    pub fn admit(&self, jid: &str) -> Result<Option<FileLock>> {
        let cfg = self.queue_config()?;
        self.admit_configured(jid, &cfg)
    }
    pub fn admit_configured(&self, jid: &str, cfg: &QueueConfig) -> Result<Option<FileLock>> {
        // The separate minute task samples; admission only reads durable state.
        let mut cfg = cfg.clone();
        // Slow yield work happens before the gate, from a lock-free snapshot.
        // First admission must also inspect completed boundaries. Build a
        // missing index under a brief gate, then release it before yield work.
        let snapshot = self.yield_snapshot()?;
        self.maintain_tool_yields(&snapshot);
        let _gate = self.queue_lock_for("admit_configured", std::time::Duration::from_secs(60))?;
        cfg.max_running = self.effective_limit(&cfg)?;
        cfg.admission_limits = None;
        cfg.admission_limits = Some(self.provider_limits(&cfg));
        let jobs = self.admission_jobs()?;
        if let Err(error) = self.maintain_yields(&jobs) {
            self.yield_diagnostic(&error);
        }
        // A live controller may still be saving/starting this job. Do not hold
        // the whole queue hostage while waiting for that private lock.
        let state = match FileLock::try_acquire(&self.job_dir(jid).join("state.lock")) {
            Ok(state) => state,
            Err(e)
                if matches!(e.raw_os_error(), Some(32 | 33))
                    || e.kind() == std::io::ErrorKind::WouldBlock =>
            {
                return Ok(None);
            }
            Err(e) => return Err(e.into()),
        };
        let mut job = self.load(jid)?;
        if s(&job, "status") != "queued" || job["cancel_requested"] == true {
            return Ok(None);
        }
        let _subwork_store_lock = job["subwork"]
            .is_object()
            .then(|| {
                FileLock::acquire(&self.todo_store().with_file_name("todo.lock"))
                    .map_err(Error::from)
            })
            .transpose()?;
        self.record_work_owner(&mut job);
        let mut jobs = self.admission_jobs()?;
        // Recheck only the exact remembered owner, never scan terminal history
        // under the admission gate. Missing/unreadable evidence remains waiting.
        let owner_id = job["work_wait_evidence"]["owner_job_id"]
            .as_str()
            .or_else(|| job["queue_wait_reason"]["owner_job_id"].as_str())
            .unwrap_or("");
        if valid_job_id(owner_id)
            && !jobs.iter().any(|j| s(j, "id") == owner_id)
            && let Ok(mut owner) = self.load(owner_id)
        {
            owner["work_owner_live"] = json!(self.is_ours(&owner, "codex_pid"));
            jobs.push(owner);
        }
        if let Ok(db) = self.read_json(&self.todo_store()) {
            if let Some(owner_id) = db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|i| i["id"] == job["todo_id"])
                .and_then(|i| i["work_owner_job"].as_str())
                && valid_job_id(owner_id)
                && !jobs.iter().any(|j| s(j, "id") == owner_id)
                && let Ok(mut owner) = self.load(owner_id)
            {
                owner["work_owner_live"] = json!(self.is_ours(&owner, "codex_pid"));
                jobs.push(owner);
            }
            let on = cc_job_policy::work_owner::remembered_pending(&db, &job, &jobs);
            if job["work_wait_on"].is_array() && job["work_wait_on"] != json!(on) {
                if !job["work_wait_history"].is_array() {
                    job["work_wait_history"] = json!([]);
                }
                let history = json!({"wait_on":job["work_wait_on"],"evidence":job["work_wait_evidence"],"reason":"owner_claim_reevaluated","checked_at":self.rt.now()});
                job["work_wait_history"]
                    .as_array_mut()
                    .unwrap()
                    .push(history);
                job["work_wait_on"] = json!(on);
                self.save(&job)?;
            }
        }
        if let Some(current) = jobs.iter_mut().find(|j| s(j, "id") == jid) {
            *current = job.clone();
        }
        if !jobs.iter().any(|j| s(j, "id") == jid) {
            jobs.push(job.clone());
        }
        let decision = self.queue_decisions(&jobs, &cfg, Some(jid));
        if let Some(reason) = decision.get(jid) {
            let previous_wait = job["work_wait_on"].clone();
            let previous_evidence = job["work_wait_evidence"].clone();
            // Keep task ownership across the original worker's context rotation or exit.
            if reason["code"] == "work_owner"
                && let (Some(id), Some(on)) =
                    (job["todo_id"].as_i64(), reason["owner_todo_id"].as_i64())
                && let Ok(db) = self.read_json(&self.todo_store())
                && cc_job_policy::todo_wait::validate(&db, id, &[on]).is_ok()
            {
                job["work_wait_on"] = json!([on]);
                if let Some(owner) = jobs.iter().find(|j| j["id"] == reason["owner_job_id"]) {
                    job["work_wait_evidence"] = json!({"owner_job_id":owner["id"],"owner":cc_job_policy::work_owner::owner_evidence(owner),"candidate_identity":job["work_owner"],"recorded_at":self.rt.now()});
                }
            }
            if job["queue_wait_reason"] != *reason
                || previous_wait != job["work_wait_on"]
                || previous_evidence != job["work_wait_evidence"]
            {
                job["queue_wait_reason"] = reason.clone();
                job["queue_wait_checked_at"] = json!(self.rt.now());
                self.save(&job)?;
            }
            return Ok(None);
        }
        // Preparation/queue status is a snapshot, not admission. Serialize the
        // final ToDo check and running publication with wait registration.
        // Lock order remains queue -> job state -> ToDo, and the slow preparation
        // above does not hold the shared ToDo lock for ordinary jobs.
        let store = self.todo_store();
        let _ready_store_lock = if _subwork_store_lock.is_none() {
            // The lock also serializes first creation of the store with running publication.
            if let Some(parent) = store.parent() {
                fs::create_dir_all(parent)?;
            }
            match FileLock::try_acquire(&store.with_file_name("todo.lock")) {
                Ok(lock) => Some(lock),
                Err(e) if matches!(e.raw_os_error(), Some(32 | 33)) || e.kind() == std::io::ErrorKind::WouldBlock => return Ok(None),
                Err(e) => return Err(e.into()),
            }
        } else { None };
        if store.exists() {
            let db = self.read_json(&store)?;
            let reason = self.dependency_reason(&db, &job, &jobs);
            if let Some(reason) = reason {
                job["queue_wait_reason"] = reason;
                job["queue_wait_checked_at"] = json!(self.rt.now());
                self.save(&job)?;
                return Ok(None);
            }
        }
        if job["subwork"].is_object() {
            cc_job_policy::subwork::mark_live(&mut jobs, |j| self.running_job(j));
            cc_job_policy::subwork::refresh_job(
                &self.read_json(&self.todo_store())?,
                &jobs,
                &mut job,
            )
            .map_err(Error::new)?;
        }
        job["queue_wait_reason"] = Value::Null;
        job["queue_admission_error"] = Value::Null;
        job["department_priority"] = json!(self.queue_priorities(&jobs, &cfg).get(jid).map(|rank| rank / 10));
        let (_, _, _, prefer_claude) = self.provider_limits(&cfg);
        let model = self.launch_model(&job, prefer_claude);
        if model != s(&job, "model") {
            job["model"] = json!(model);
            job["model_routed_at"] = json!(self.rt.now());
            job["model_route_reason"] = json!("Codexの週の利用ペースを抑えてClaudeへ割り当て");
        }
        job["status"] = json!("running");
        job["started_at"] = json!(self.rt.now());
        job["last_event_at"] = json!(self.rt.now());
        job["activity"] = json!("starting");
        self.save(&job)?;
        Ok(Some(state))
    }

    /// Pure admission assessment shared by the launcher and status. Earlier
    /// candidates reserve capacity in this snapshot; only admit persists it.
    pub(crate) fn queue_decisions(
        &self,
        jobs: &[Value],
        cfg: &QueueConfig,
        caller: Option<&str>,
    ) -> HashMap<String, Value> {
        let mut observed = jobs.to_vec();
        cc_job_policy::subwork::mark_live(&mut observed, |j| self.running_job(j));
        let db = observed
            .iter()
            .any(|j| j["subwork"].is_object())
            .then(|| self.read_json(&self.todo_store()));
        let mut subwork_waits = HashMap::new();
        let normalized: Vec<_> = observed
            .iter()
            .map(|j| {
                let mut candidate = j.clone();
                if j["subwork"].is_object() {
                    candidate["subwork_validated"] = json!(false);
                    candidate["subwork_owner_job_id"] = Value::Null;
                    candidate["parent_job_id"] = Value::Null;
                    let result = match &db {
                        Some(Ok(db)) => {
                            cc_job_policy::subwork::refresh_job(db, &observed, &mut candidate)
                        }
                        _ => Err("subwork ToDo store is unavailable".into()),
                    };
                    if let Err(message) = result {
                        subwork_waits.insert(
                            s(j, "id").to_owned(),
                            json!({"code":"subwork_invalid","message":message}),
                        );
                    }
                }
                candidate
            })
            .collect();
        let jobs = normalized.as_slice();
        let priorities = self.queue_priorities(jobs, cfg);
        let dependency_waits = self.dependency_waits(jobs);
        let ownership_jobs: Vec<_> = jobs
            .iter()
            .filter(|j| {
                (!dependency_waits.contains_key(s(j, "id"))
                    && !subwork_waits.contains_key(s(j, "id")))
                    || self.running_job(j)
                    || s(j, "status") == "starting"
            })
            .map(|j| {
                let mut j = j.clone();
                if self.is_ours(&j, "codex_pid") {
                    j["work_owner_live"] = json!(true);
                }
                j
            })
            .collect();
        let mut decisions = HashMap::new();
        // A normal waiter blocks opportunistic roots even during cooldown or
        // when its child/family limit is full. Never reserve phantom spare slots
        // for low-priority work while another owner is actually waiting.
        let normal_waiting = jobs
            .iter()
            .filter(|j| {
                s(j, "status") == "queued"
                    && j["cancel_requested"] != true
                    && self.supervisor_alive(j)
                    && !dependency_waits.contains_key(s(j, "id"))
                    && !subwork_waits.contains_key(s(j, "id"))
                    && cc_job_policy::work_owner::blocker(j, &ownership_jobs).is_none()
                    && priorities.get(s(j, "id")).copied().unwrap_or(1002) % 10 < 3
            })
            .filter_map(|j| priorities.get(s(j, "id")).copied())
            .min();
        let active: Vec<_> = jobs
            .iter()
            .filter(|j| {
                j["queue_unreadable"] == true || s(j, "status") == "starting" || self.running_job(j)
            })
            .collect();
        // Count occupied families even when the root/intermediate supervisor has
        // exited. A surviving descendant must not silently free a root slot.
        // A job idle in the Cargo gate frees its family unless another member runs.
        let (hardware_limit, codex_limit, claude_limit, prefer_claude) = self.provider_limits(cfg);
        let mut families = HashSet::new();
        let mut codex_families = HashSet::new();
        let mut claude_families = HashSet::new();
        let mut children = HashMap::<String, usize>::new();
        for j in &active {
            if !self.cargo_waiting(j) {
                let root = self.queue_root(j);
                families.insert(root.clone());
                if s(j, "model").starts_with("anthropic/") {
                    claude_families.insert(root);
                } else {
                    codex_families.insert(root.clone());
                    if j["queue_unreadable"] == true && s(j, "model").is_empty() {
                        claude_families.insert(root);
                    }
                }
            }
            if !s(j, "parent_job_id").is_empty() {
                *children.entry(s(j, "parent_job_id").into()).or_default() += 1;
            }
        }
        let mut waiting = Vec::new();
        for j in jobs.iter().filter(|j| s(j, "status") == "queued") {
            if let Some(reason) = subwork_waits.get(s(j, "id")) {
                decisions.insert(s(j, "id").into(), reason.clone());
                continue;
            }
            if let Some(reason) = dependency_waits.get(s(j, "id")) {
                decisions.insert(s(j, "id").into(), reason.clone());
                continue;
            }
            // Intake may precede discovery of a legacy writer. Recheck session
            // ownership under the admission gate, including real process identity.
            // Earlier queued writers also reserve their session, even with spare
            // hardware capacity. A display name is not a session identity.
            if let Some(owner) = jobs.iter().find(|other| {
                s(other, "id") != s(j, "id")
                    && ((intake_todos_overlap(j, other)
                        // Existing child work in the same live family shares
                        // its parent's ToDo, but never its writable session.
                        && (s(j, "parent_job_id").is_empty()
                            || self.queue_root(j) != self.queue_root(other)))
                        || ["thread_id", "resume_session"].iter().any(|key| {
                            normalize_uuid(s(j, key)).is_some_and(|session| {
                                ["thread_id", "resume_session"].iter().any(|other_key| {
                                    normalize_uuid(s(other, other_key)).as_ref() == Some(&session)
                                })
                            })
                        }))
                    && (other["queue_unreadable"] == true
                        || s(other, "status") == "starting"
                        || self.running_job(other)
                        || (s(other, "status") == "queued"
                            && other["cancel_requested"] != true
                            && (
                                queue_time(other),
                                number(other, "queue_sequence"),
                                s(other, "id"),
                            ) < (queue_time(j), number(j, "queue_sequence"), s(j, "id"))))
            }) {
                decisions.insert(s(j, "id").into(), json!({"code":"intake_owner","message":"同じToDo・セッションの担当が先","owner_job_id":owner["id"]}));
                continue;
            }
            if let Some(owner) = cc_job_policy::work_owner::blocker(j, &ownership_jobs) {
                decisions.insert(s(j, "id").into(), cc_job_policy::work_owner::reason(owner));
                continue;
            }
            let reason = if j["cancel_requested"] == true {
                Some(("cancel_requested", "取り消し処理待ち"))
            } else if caller != Some(s(j, "id"))
                && (j["queue_restart_pending"] == true || !self.supervisor_alive(j))
            {
                Some(("supervisor_missing", "見張りの再起動待ち"))
            } else if j["retry_not_before"].as_f64().unwrap_or(0.) > self.rt.epoch() {
                Some(("retry_cooldown", "利用枠の再試行時刻待ち"))
            } else if s(j, "parent_job_id").is_empty()
                && priorities.get(s(j, "id")).is_some_and(|rank| {
                    rank % 10 == 3 && normal_waiting.is_some_and(|other| other < *rank)
                })
            {
                Some((
                    "lowest_priority",
                    "最低順位: 通常の待機ジョブが先（再試行待ちも含む）",
                ))
            } else {
                None
            };
            if let Some((code, text)) = reason {
                decisions.insert(s(j, "id").into(), json!({"code":code,"message":text}));
            } else {
                waiting.push(j);
            }
        }
        waiting.sort_by_key(|j| {
            (
                priorities.get(s(j, "id")).copied().unwrap_or(1002) / 10,
                s(j, "parent_job_id").is_empty(),
                priorities.get(s(j, "id")).copied().unwrap_or(1002) % 10,
                queue_time(j),
                j["queue_sequence"].as_u64().unwrap_or(0),
                s(j, "id"),
            )
        });
        let mut child_first = false;
        for candidate in waiting {
            let parent = s(candidate, "parent_job_id");
            if parent.is_empty() && child_first {
                decisions.insert(s(candidate,"id").into(), json!({"code":"child_priority","message":"子ジョブが先に枠を確保するのを待っています"}));
                continue;
            }
            if !parent.is_empty()
                && children.get(parent).copied().unwrap_or(0) >= cfg.max_children_per_parent
            {
                decisions.insert(s(candidate,"id").into(), json!({"code":"child_limit","message":"同じ親の子ジョブ上限待ち","parent_job_id":parent,"max_children":cfg.max_children_per_parent}));
                continue;
            }
            let root = self.queue_root(candidate);
            let model = self.launch_model(candidate, prefer_claude);
            let claude = model.starts_with("anthropic/");
            let provider_families = if claude {
                &mut claude_families
            } else {
                &mut codex_families
            };
            let provider_limit = if claude { claude_limit } else { codex_limit };
            let hardware_blocked = !families.contains(&root) && families.len() >= hardware_limit;
            let provider_blocked = !provider_families.contains(&root)
                && provider_families.len() >= provider_limit
                && (parent.is_empty() || !families.contains(&root));
            if hardware_blocked || provider_blocked {
                decisions.insert(s(candidate,"id").into(), json!({"code":"running_limit","message":"実行枠待ち（先に起動するジョブの予約を含む）","max_running":hardware_limit,"provider":if claude {"anthropic"} else {"openai"},"provider_limit":provider_limit,
                    "hardware_limited":hardware_blocked && (!provider_blocked || provider_limit >= hardware_limit)}));
                continue;
            }
            // Children have first claim; wait for the earlier eligible job to
            // reserve its slot durably instead of allowing roots to overtake it.
            provider_families.insert(root.clone());
            families.insert(root);
            if !parent.is_empty() {
                child_first = true;
                *children.entry(parent.into()).or_default() += 1;
            }
        }
        decisions
    }

    pub(crate) fn todo_store(&self) -> std::path::PathBuf {
        self.rt
            .env()
            .get("CC_TODO_HOME")
            .map(|home| std::path::PathBuf::from(home).join("todo.json"))
            .unwrap_or_else(|| {
                self.root
                    .parent()
                    .unwrap_or(&self.root)
                    .join("todo/todo.json")
            })
    }

    pub(crate) fn record_work_owner(&self, job: &mut Value) {
        if let Ok(db) = self.read_json(&self.todo_store()) {
            *job = cc_job_policy::work_owner::registered_job(&db, job);
        }
        if s(job, "task_title").is_empty()
            && let Ok(db) = self.read_json(&self.todo_store())
            && let Some(item) = db["items"].as_array().into_iter().flatten().find(|i| {
                (job["todo_id"].is_number() && job["todo_id"] == i["id"])
                    || (job["id"].is_string()
                        && i["jobs"]
                            .as_array()
                            .is_some_and(|ids| ids.contains(&job["id"])))
            })
        {
            job["task_title"] = item["title"].clone();
        }
        let mut identity = if job["work_owner"].is_object() {
            job["work_owner"].clone()
        } else {
            self.rt.work_checkout(s(job, "cwd"))
        };
        if let Some(pr) = cc_job_policy::work_owner::pr_number(s(job, "task_title")) {
            identity["pr"] = json!(pr);
        }
        job["work_owner"] = identity;
        job["work_owner"] = cc_job_policy::work_owner::from_job(job);
    }

    // Status snapshots and the final locked admission must use the same owner/dependency policy.
    // A legacy automatic home-Git claim may be disproved without changing the durable store;
    // a newly registered manual dependency or readiness wait must still stop admission.
    fn dependency_reason(&self, source_db: &Value, job: &Value, jobs: &[Value]) -> Option<Value> {
        let mut db = source_db.clone();
        let mut evidence_jobs = jobs.to_vec();
        let ids: Vec<_> = db["items"].as_array().into_iter().flatten()
            .filter(|i| i["id"] == job["todo_id"]
                || job["related_todo_ids"].as_array().is_some_and(|ids| ids.contains(&i["id"]))
                || i["jobs"].as_array().is_some_and(|ids| ids.contains(&job["id"])))
            .filter_map(|i| i["work_owner_job"].as_str())
            .chain(job["work_wait_evidence"]["owner_job_id"].as_str())
            .chain(job["queue_wait_reason"]["owner_job_id"].as_str()).collect();
        for id in ids {
            if valid_job_id(id) && !evidence_jobs.iter().any(|j| s(j, "id") == id)
                && let Ok(mut owner) = self.load(id) {
                owner["work_owner_live"] = json!(self.is_ours(&owner, "codex_pid"));
                evidence_jobs.push(owner);
            }
        }
        cc_job_policy::work_owner::reevaluate_todos_if(&mut db, &evidence_jobs,
            |item| item["id"] == job["todo_id"]);
        let ready = cc_job_policy::todo_wait::job_ready_waits(&db, job);
        if !ready.is_empty() {
            let message = ready.iter().filter_map(|wait| wait["message"].as_str())
                .collect::<Vec<_>>().join("、");
            return Some(json!({"code":"todo_readiness","message":message,"wait_ready":ready}));
        }
        let mut on = cc_job_policy::todo_wait::job_pending_on(&db, job);
        on.extend(cc_job_policy::work_owner::remembered_pending(source_db, job, &evidence_jobs));
        on.sort_unstable();
        on.dedup();
        if on.is_empty() { None } else {
            let message = on.iter().map(|id| format!("ToDo {id} 待ち")).collect::<Vec<_>>().join("、");
            Some(json!({"code":"todo_dependency","message":message,"wait_on":on}))
        }
    }

    fn dependency_waits(&self, jobs: &[Value]) -> HashMap<String, Value> {
        let db = match fs::read(self.todo_store()) {
            Ok(bytes) => serde_json::from_slice::<Value>(&bytes).map_err(Error::from),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(json!({"items":[]})),
            Err(error) => Err(Error::from(error)),
        };
        let mut waits = HashMap::new();
        for job in jobs.iter().filter(|j| s(j, "status") == "queued") {
            let reason = match &db {
                Ok(db) => self.dependency_reason(db, job, jobs),
                Err(_) => Some(json!({"code":"todo_store_unavailable","message":"ToDoの完了待ちを確認できないため再確認待ち"})),
            };
            if let Some(reason) = reason { waits.insert(s(job, "id").into(), reason); }
        }
        waits
    }

    /// Only the configured ToDo IDs are the lowest tier. Prompt bodies and
    /// titles carry shared guidance ("複数なら--onを繰り返す") and ordinary
    /// wording ("繰り返し起動しない"); text matching once marked every job
    /// lowest and collapsed req-/shep-/other into plain FIFO.
    pub(crate) fn queue_priorities(&self, jobs: &[Value], cfg: &QueueConfig) -> HashMap<String, u32> {
        let db = self.read_json(&self.todo_store()).unwrap_or(Value::Null);
        let policy = self
            .root
            .parent()
            .unwrap_or(&self.root)
            .join("scheduler/priorities.json");
        let policy = self.read_json(&policy).unwrap_or(Value::Null);
        let departments = cc_job_policy::priority::job_ranks(jobs, &db, &policy);
        jobs.iter()
            .map(|j| {
                let id = j["todo_id"].as_u64();
                // Dependents of already running parents keep their existing child
                // admission rules so a parent can finish and release its slot.
                let low = s(j, "parent_job_id").is_empty()
                    && id.is_some_and(|id| {
                        cfg.lowest_priority_todo_ids.contains(&id)
                            && !cfg.normal_priority_todo_ids.contains(&id)
                    });
                (
                    s(j, "id").to_owned(),
                    departments.get(s(j, "id")).copied().unwrap_or(100) * 10
                        + u32::from(if low { 3 } else { queue_priority(j) }),
                )
            })
            .collect()
    }
    pub(crate) fn queue_root(&self, job: &Value) -> String {
        // Independent ToDos consume ordinary hardware/provider capacity even with a live parent.
        if job["subwork"].is_object() {
            return s(job, "id").into();
        }
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
            || (s(job, "status") != "queued" && self.tool_handoff_pending(job))
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
        || (!s(a, "name").is_empty()
            && (s(a, "name") == s(b, "name")
                || cc_task_title::canonical_text(s(a, "name"))
                    == cc_task_title::canonical_text(s(b, "name"))))
        || ["thread_id", "resume_session"].iter().any(|ka| {
            normalize_uuid(s(a, ka)).is_some_and(|id| {
                ["thread_id", "resume_session"]
                    .iter()
                    .any(|kb| normalize_uuid(s(b, kb)).as_ref() == Some(&id))
            })
        })
}
fn intake_todos_overlap(a: &Value, b: &Value) -> bool {
    let ids = |job: &Value| -> Vec<u64> {
        job["todo_id"]
            .as_u64()
            .into_iter()
            .chain(
                job["related_todo_ids"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(Value::as_u64),
            )
            .collect()
    };
    let other = ids(b);
    ids(a).iter().any(|id| other.contains(id))
}
fn admission_owner(job: &Value) -> Value {
    Value::Object(
        [
            "work_owner",
            "registered_cwd",
            "cwd",
            "project",
            "parent_job_id",
            "todo_id",
            "related_todo_ids",
            "thread_id",
            "resume_session",
            "created_at",
            "queue_sequence",
            "model",
        ]
        .into_iter()
        .map(|key| (key.to_owned(), job[key].clone()))
        .collect(),
    )
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

#[cfg(test)]
mod display_name_tests {
    use super::*;
    #[test]
    fn duplicate_task_identity_survives_product_display_rename() {
        let old = json!({"name":format!("{}の画面を直す", concat!("x", "LIFE"))});
        let renamed = json!({"name":"Re:Xの画面を直す"});
        assert!(same_task(&old, &renamed));
        assert!(same_task(&json!({"name":"Re-Xの画面を直す"}), &renamed));
        assert!(!same_task(&json!({"name":"Re:Xの別作業"}), &renamed));
        assert!(!same_task(&json!({"name":""}), &json!({"name":""})));
    }
}
