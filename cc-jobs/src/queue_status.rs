//! Admission and resource wait diagnostics; status never stops a process.
use crate::*;
use std::{
    collections::{BTreeMap, HashSet},
    fs,
};

impl Context {
    pub(crate) fn record_admission_error(&self, id: &str, error: Option<&Error>) {
        // Best effort and nonblocking: diagnosis must never delay admission.
        if let Ok(_state) = FileLock::try_acquire(&self.job_dir(id).join("state.lock"))
            && let Ok(mut job) = self.load(id)
            && s(&job, "status") == "queued"
        {
            let value = error
                .map(|e| json!(redact_local_log_secrets(&e.message, &self.rt.env())))
                .unwrap_or(Value::Null);
            if job["queue_admission_error"] != value
                || (error.is_some()
                    && parse_iso(s(&job, "queue_admission_error_at"))
                        .is_none_or(|at| self.rt.epoch() - at >= 60.))
            {
                job["queue_admission_error"] = value;
                job["queue_admission_error_at"] = json!(self.rt.now());
                let _ = self.save(&job);
            }
        }
    }
    pub fn queue_report(&self, jobs: &[Value]) -> Result<Value> {
        let (cfg, config_error) = match self.queue_config() {
            Ok(cfg) => (Some(cfg), Value::Null),
            Err(error) => (
                None,
                json!(redact_local_log_secrets(&error.message, &self.rt.env())),
            ),
        };
        let active: Vec<_> = jobs
            .iter()
            .filter(|j| {
                LIVE.contains(&s(j, "status"))
                    || s(j, "status") == "starting"
                    || j["queue_unreadable"] == true
                    || self.running_job(j)
            })
            .cloned()
            .collect();
        let decisions = cfg
            .as_ref()
            .map(|cfg| self.queue_decisions(&active, cfg, None))
            .unwrap_or_default();
        let mut running = 0;
        let mut families = HashSet::new();
        let mut provider_families = BTreeMap::<&str, HashSet<String>>::new();
        let mut queued = Vec::new();
        let mut resources = Vec::new();
        let mut counts = BTreeMap::<String, usize>::new();
        for job in jobs {
            let id = s(job, "id");
            if job["queue_unreadable"] == true
                || s(job, "status") == "starting"
                || self.running_job(job)
            {
                running += 1;
                if !self.cargo_waiting(job) {
                    families.insert(self.queue_root(job));
                    provider_families
                        .entry(
                            if s(job, "model").starts_with("anthropic/")
                                && job["queue_unreadable"] != true
                            {
                                "anthropic"
                            } else {
                                "openai"
                            },
                        )
                        .or_default()
                        .insert(self.queue_root(job));
                }
                let reasons = self.resource_waits(job);
                if !reasons.is_empty() {
                    resources.push(json!({"id":id,"reasons":reasons}));
                }
            }
            if s(job, "status") != "queued" {
                continue;
            }
            let mut reason = decisions.get(id).cloned().unwrap_or_else(
                || json!({"code":"ready","message":"起動可能: 見張りの次の確認待ち"}),
            );
            if !config_error.is_null() {
                reason = json!({"code":"config_error","message":config_error});
            }
            if parse_iso(s(job, "queue_admission_error_at"))
                .is_some_and(|at| self.rt.epoch() - at >= 0. && self.rt.epoch() - at < 90.)
                && !s(job, "queue_admission_error").is_empty()
            {
                reason = json!({"code":"admission_error","message":job["queue_admission_error"],"policy_reason":reason});
            }
            *counts.entry(s(&reason, "message").into()).or_default() += 1;
            queued.push(json!({"id":id,"reason":reason,"yield_resume":job["yield_resume"]==true}));
        }
        let limit = cfg.as_ref().map(|cfg| self.limit_status(cfg)).transpose()?;
        let limits = cfg.as_ref().map(|cfg| self.provider_limits(cfg));
        Ok(
            json!({"running":running,"occupied_slots":families.len(),"max_running":limits.map(|v|v.0),
            "provider_slots":{"openai":provider_families.get("openai").map_or(0,HashSet::len),"anthropic":provider_families.get("anthropic").map_or(0,HashSet::len)},
            "provider_limits":limits.map(|v|json!({"openai":v.1,"anthropic":v.2})),
            "limit":limit,"limit_label":limit.as_ref().map(|v|v["label"].clone()),
            "queued_count":queued.len(),"queued":queued,"wait_reason_counts":counts,"resource_waits":resources,"config_error":config_error}),
        )
    }

    pub fn queue_report_line(&self, report: &Value) -> String {
        let reasons = report["wait_reason_counts"]
            .as_object()
            .map(|counts| {
                counts
                    .iter()
                    .map(|(reason, n)| format!("{} {n}件", reason.replace(['\r', '\n'], " ")))
                    .collect::<Vec<_>>()
                    .join("; ")
            })
            .unwrap_or_default();
        let resources = report["resource_waits"]
            .as_array()
            .map(|items| {
                items
                    .iter()
                    .map(|j| {
                        format!(
                            "{}: {}",
                            s(j, "id"),
                            j["reasons"]
                                .as_array()
                                .unwrap()
                                .iter()
                                .filter_map(Value::as_str)
                                .collect::<Vec<_>>()
                                .join("・")
                        )
                    })
                    .collect::<Vec<_>>()
                    .join("; ")
            })
            .unwrap_or_default();
        let limit = report["max_running"]
            .as_u64()
            .map(|n| n.to_string())
            .unwrap_or_else(|| "不明（設定エラー）".into());
        format!(
            "実行中 {}/{}枠（子を含む{}件） / {} / 順番待ち {}件 [{}] / 共有資源待ち [{}]",
            report["occupied_slots"],
            limit,
            report["running"],
            s(report, "limit_label"),
            report["queued_count"],
            reasons,
            resources
        )
    }

    /// Markers of this job's processes that are still alive with the same identity.
    fn live_wait_markers(&self, job: &Value) -> Vec<Value> {
        let Ok(entries) = fs::read_dir(self.job_dir(s(job, "id")).join("resource-waits")) else {
            return Vec::new();
        };
        entries
            .flatten()
            .filter_map(|entry| self.read_json(&entry.path()).ok())
            .filter(|wait| s(wait, "job_id") == s(job, "id") && self.is_ours(wait, "pid"))
            .collect()
    }

    /// A job blocked in the Cargo gate is idle, so it does not occupy a queue
    /// slot; it counts again as soon as Cargo is admitted and the marker is gone.
    pub(crate) fn cargo_waiting(&self, job: &Value) -> bool {
        let markers = self.live_wait_markers(job);
        !markers.iter().any(|wait| s(wait, "kind") == "cargo-active")
            && markers.iter().any(|wait| {
                s(wait, "kind") == "cargo"
                    || (s(wait, "kind").is_empty()
                        && matches!(
                            s(wait, "reason"),
                            "cargoの共有ビルド枠待ち" | "cargoの共有キャッシュ枠待ち"
                        ))
            })
    }

    pub fn resource_waits(&self, job: &Value) -> Vec<String> {
        let mut reasons: Vec<String> = self
            .live_wait_markers(job)
            .iter()
            .filter(|wait| s(wait, "kind") != "cargo-active")
            .map(|wait| s(wait, "reason").to_string())
            .collect();
        // live_chain already publishes exact job and process identities.
        if let Some(tools) = self.root.parent()
            && let Ok(entries) = fs::read_dir(tools.join("civ6-live/queue"))
        {
            for entry in entries.flatten() {
                if let Ok(wait) = self.read_json(&entry.path())
                    && s(&wait, "job") == s(job, "id")
                    && let Some(pid) = wait["pid"].as_u64().and_then(|p| u32::try_from(p).ok())
                    && wait["pid_created_filetime"]
                        .as_u64()
                        .is_some_and(|created| self.rt.proc_created(pid) == Some(created))
                {
                    reasons.push("Civ6の共有枠待ち".into());
                }
            }
        }
        reasons.sort();
        reasons.dedup();
        reasons
    }

    /// Only a terminal owner's controller may be reaped. Detached jobs and
    /// work processes are excluded, even if Windows retains a parent PID link.
    pub fn cleanup_supervisors(&self, apply: bool) -> Result<Value> {
        let mut cleaned = Vec::new();
        for snapshot in self.list_jobs(&HashSet::new()) {
            if !TERMINAL.contains(&s(&snapshot, "status")) || !self.is_ours(&snapshot, "pid") {
                continue;
            }
            let _gate = self.queue_lock()?;
            let _state = self.job_lock(s(&snapshot, "id"), "state.lock")?;
            let mut job = self.load(s(&snapshot, "id"))?;
            if !TERMINAL.contains(&s(&job, "status"))
                || !self.is_ours(&job, "pid")
                || self.is_ours(&job, "codex_pid")
                || self.tool_handoff_pending(&job)
                || job["pid"].as_u64() == Some(self.rt.pid() as u64)
                // Normal finalization still closes Chrome and delivers the last
                // notification after the terminal record has been saved.
                || parse_iso(s(&job,"finished_at")).is_none_or(|at|self.rt.epoch()-at < 120.)
            {
                continue;
            }
            let before = json!({"pid":job["pid"],"pid_created":job["pid_created"],"finished_at":job["finished_at"]});
            if apply {
                self.stop_supervisor_owner(&job)?;
                if self.is_ours(&job, "pid") {
                    return Err(Error::new("terminal supervisor did not exit"));
                }
                job["supervisor_cleanup"] = json!({"owner":before,"stopped_at":self.rt.now()});
                self.save(&job)?;
            }
            cleaned.push(json!({"id":job["id"],"owner":before,"action":if apply {"stopped"} else {"would_stop"}}));
        }
        Ok(json!(cleaned))
    }
}

/// Per-process marker avoids competing with supervisors writing job.json.
pub(crate) struct ResourceWait(std::path::PathBuf);
impl ResourceWait {
    pub(crate) fn new(env: &BTreeMap<String, String>, reason: &str) -> Option<Self> {
        Self::state(env, reason, "cargo")
    }
    pub(crate) fn active(env: &BTreeMap<String, String>) -> Option<Self> {
        Self::state(env, "cargoのビルド・テスト実行中", "cargo-active")
    }
    fn state(env: &BTreeMap<String, String>, reason: &str, kind: &str) -> Option<Self> {
        let id = env.get("CC_JOB_ID").or_else(|| env.get("CODEX_JOB_ID"))?;
        if !crate::queue::valid_job_id(id) {
            return None;
        }
        let gate = env.get("CODEX_JOBS_CARGO_GATE")?;
        let dir = std::path::Path::new(gate)
            .parent()?
            .join("jobs")
            .join(id)
            .join("resource-waits");
        if !dir.parent()?.join("job.json").is_file() {
            return None;
        }
        fs::create_dir_all(&dir).ok()?;
        let path = dir.join(format!("cargo-{}.json", std::process::id()));
        let data = json!({"job_id":id,"kind":kind,"pid":std::process::id(),"pid_created":proc_created(std::process::id()),"reason":reason,"since":now_iso()});
        fs::write(&path, serde_json::to_vec(&data).ok()?).ok()?;
        Some(Self(path))
    }
}
impl Drop for ResourceWait {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}
