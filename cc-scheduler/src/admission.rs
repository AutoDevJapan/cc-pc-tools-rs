//! Observations of ordinary dispatch, never a second admission implementation.
use crate::common::*;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    collections::BTreeMap,
    fs,
    io::{Read, Write},
    path::Path,
};

pub(crate) mod inputs;
pub(crate) mod metadata;
pub(crate) fn result_fingerprint(item: &Value, root: &Value) -> String {
    metadata::fingerprint(&metadata::project(item), root)
}
pub const MAX_ENTRIES: usize = 8192;
pub const MAX_BYTES: u64 = 4 * 1024 * 1024;
pub const TTL: f64 = 180.;
const FILE: &str = "admission.json";

#[derive(Clone, Copy, Debug, Default, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum Stage {
    #[default]
    Unknown,
    Filtering,
    Priority,
    Capacity,
    Resource,
    Preparation,
    Recheck,
    Launch,
}
#[derive(Clone, Copy, Debug, Default, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum Reason {
    #[default]
    Unknown,
    OwnerProject,
    StatusNotTodo,
    NotDispatchable,
    MissingSpecMetadata,
    DependencyWait,
    TargetedHold,
    QuotaRetryEnded,
    ReviewPreparationExhausted,
    RootExcluded,
    RecoveryReservation,
    RelatedOwner,
    QuestionClosePending,
    NotBefore,
    Capacity,
    ResourceBusy,
    NotReached,
    EarlierCandidateLaunched,
    PreparationDeferred,
    QuotaRecheckWait,
    QuotaExhausted,
    QuotaProofMissing,
    InputsChanged,
    PreparedStatusChanged,
    CapacityChanged,
    ResourceChanged,
    DependencyChanged,
    HoldChanged,
    RootChanged,
    ModelSelectionChanged,
    PreparationFailed,
    StageFailed,
    LaunchFailed,
    LaunchedUnlinked,
    QueuedUnlinked,
    Launched,
    ExistingJobNoAction,
    ExistingJobReserved,
    Queued,
    Accepted,
    LaunchOutcomeUnknown,
}
#[derive(Clone, Copy, Debug, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum Failure {
    CwdMissing,
    NonGit,
    OriginMissing,
    BranchMismatch,
    RepositoryMismatch,
    ToolMissing,
    ResourceWait,
    InputRejected,
    ExcludedResource,
    ToolFailed,
    QuotaRetryEnded,
    ReviewModelConflict,
    ReviewModelUnavailable,
    ReviewSourceUnavailable,
    Interrupted,
    Other,
}
impl Failure {
    pub fn from_error(e: &Error) -> Self {
        match e.0 {
            "KeyboardInterrupt" => Self::Interrupted,
            "QuotaRetryEnded" => Self::QuotaRetryEnded,
            "ReviewModelConflict" => Self::ReviewModelConflict,
            "ReviewModelUnavailable" => Self::ReviewModelUnavailable,
            "ReviewSourceUnavailable" => Self::ReviewSourceUnavailable,
            _ => match crate::preparation::failure_reason(e).0 {
                "cwd_missing" => Self::CwdMissing,
                "non_git" => Self::NonGit,
                "origin_missing" => Self::OriginMissing,
                "branch_mismatch" => Self::BranchMismatch,
                "repository_mismatch" => Self::RepositoryMismatch,
                "tool_missing" => Self::ToolMissing,
                "resource_wait" => Self::ResourceWait,
                "input_rejected" => Self::InputRejected,
                "excluded_resource" => Self::ExcludedResource,
                _ if [
                    "OSError",
                    "FileNotFoundError",
                    "PermissionError",
                    "JSONDecodeError",
                    "TimeoutError",
                    "RuntimeError",
                ]
                .contains(&e.0) =>
                {
                    Self::ToolFailed
                }
                _ => Self::Other,
            },
        }
    }
}
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Entry {
    pub id: i64,
    pub input_metadata_fingerprint: String,
    pub metadata_fingerprint: String,
    pub eligible: Option<bool>,
    pub candidate: Option<bool>,
    pub queue_rank: Option<usize>,
    pub stage: Stage,
    pub reason: Reason,
    pub root_guard_allowed: Option<bool>,
    pub root_excluded: Option<bool>,
    pub preparation_code: Option<Failure>,
    pub failure_code: Option<Failure>,
    pub recheck_capacity: Option<Capacity>,
    #[serde(default)]
    pub launch_receipt_id: Option<String>,
    #[serde(default)]
    pub returned_job_id: Option<String>,
    #[serde(default)]
    pub launch_kind: Option<cc_job_policy::launch_outcome::Kind>,
}
#[derive(Clone, Copy, Debug, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CapacityReason {
    RamBelowMinimum,
    RamReserve,
    CommitLimit,
    CpuLimit,
    DiskReserve,
    HardCap,
    LaunchSpacing,
}
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Capacity {
    pub allowance_now: Option<f64>,
    pub cpu_slots: Option<f64>,
    pub memory_slots: Option<f64>,
    pub running_jobs: Option<f64>,
    pub hard_cap: Option<f64>,
    pub launch_wait_s: Option<f64>,
    pub reasons: Vec<CapacityReason>,
}
impl Capacity {
    fn observed(report: &Value) -> Self {
        Self {
            allowance_now: report["allowance_now"].as_f64(),
            cpu_slots: report["cpu_slots"].as_f64(),
            memory_slots: report["memory_slots"].as_f64(),
            running_jobs: report["running_jobs"].as_f64(),
            hard_cap: report["hard_cap"].as_f64(),
            launch_wait_s: report["launch_wait_s"].as_f64(),
            reasons: arr(&report["reasons"])
                .iter()
                .filter_map(|r| {
                    Some(match s(r) {
                        "RAM below 8 GiB" => CapacityReason::RamBelowMinimum,
                        "RAM reserve" => CapacityReason::RamReserve,
                        "commit limit" => CapacityReason::CommitLimit,
                        "CPU rolling limit" => CapacityReason::CpuLimit,
                        "disk reserve" => CapacityReason::DiskReserve,
                        "hard cap" => CapacityReason::HardCap,
                        "launch spacing" => CapacityReason::LaunchSpacing,
                        _ => return None,
                    })
                })
                .collect(),
        }
    }
}
#[derive(Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Observation {
    schema: u32,
    at: f64,
    recorded_at: f64,
    expires_at: f64,
    ordinary: bool,
    pub complete: bool,
    pub scan_complete: bool,
    pub truncated: bool,
    pub dispatch_enabled: bool,
    pub capacity: Capacity,
    pub entries: BTreeMap<i64, Entry>,
    pub dispatch_error: Option<Failure>,
    #[serde(default)]
    pub cycle_id: Option<String>,
}
impl Observation {
    pub fn new(at: f64, ordinary: bool, enabled: bool) -> Self {
        Self {
            schema: 1,
            at,
            recorded_at: at,
            expires_at: at + TTL,
            ordinary,
            complete: false,
            scan_complete: false,
            truncated: false,
            dispatch_enabled: enabled,
            capacity: Capacity::default(),
            entries: BTreeMap::new(),
            dispatch_error: None,
            cycle_id: Some(uuid::Uuid::new_v4().simple().to_string()),
        }
    }
    /// Called only while dispatch is already iterating the original items.
    pub fn item(&mut self, item: &Value, root: &Value) {
        if !self.ordinary {
            return;
        }
        if self.entries.len() >= MAX_ENTRIES {
            self.truncated = true;
            // Old completed/held rows must not crowd out new runnable work.
            let replace = (item["status"] == "todo")
                .then(|| {
                    self.entries
                        .iter()
                        .find(|(_, e)| e.reason == Reason::StatusNotTodo)
                        .map(|(id, _)| *id)
                })
                .flatten();
            if let Some(id) = replace {
                self.entries.remove(&id);
            } else {
                return;
            }
        }
        let Some(id) = item["id"].as_i64().filter(|id| *id > 0) else {
            return;
        };
        let fingerprint = metadata::fingerprint(&metadata::project(item), root);
        self.entries.insert(
            id,
            Entry {
                id,
                input_metadata_fingerprint: fingerprint.clone(),
                metadata_fingerprint: fingerprint,
                eligible: Some(false),
                candidate: Some(false),
                stage: Stage::Filtering,
                ..Entry::default()
            },
        );
    }
    pub fn decide(&mut self, id: &Value, stage: Stage, reason: Reason) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            e.stage = stage;
            e.reason = reason;
        }
    }
    pub fn launch(&mut self, id: &Value, receipt: &crate::launch_receipt::Receipt) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            e.launch_receipt_id = Some(receipt.receipt_id.clone());
            e.returned_job_id = Some(receipt.outcome.job_id.clone());
            e.launch_kind = Some(receipt.outcome.kind);
        }
    }
    pub fn root(&mut self, id: &Value, guard: Option<bool>, excluded: bool) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            e.root_guard_allowed = guard;
            e.root_excluded = Some(excluded);
        }
    }
    pub fn eligible(&mut self, id: &Value) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            e.eligible = Some(true);
        }
    }
    pub fn queue(&mut self, queue: &[Value], report: &Value) {
        self.scan_complete = true;
        self.capacity = Capacity::observed(report);
        for (rank, item) in queue.iter().enumerate() {
            if let Some(e) = item["id"].as_i64().and_then(|id| self.entries.get_mut(&id)) {
                e.candidate = Some(true);
                e.queue_rank = Some(rank + 1);
                e.stage = Stage::Priority;
                e.reason = Reason::NotReached;
            }
        }
    }
    pub fn capacity_wait(&mut self) {
        for e in self
            .entries
            .values_mut()
            .filter(|e| e.candidate == Some(true))
        {
            e.stage = Stage::Capacity;
            e.reason = Reason::Capacity;
        }
    }
    pub fn earlier_launch(&mut self, id: &Value) {
        let rank = id
            .as_i64()
            .and_then(|id| self.entries.get(&id))
            .and_then(|e| e.queue_rank);
        if let Some(rank) = rank {
            for e in self.entries.values_mut().filter(|e| {
                e.reason == Reason::NotReached && e.queue_rank.is_some_and(|r| r > rank)
            }) {
                e.reason = Reason::EarlierCandidateLaunched;
            }
        }
    }
    pub fn failed(&mut self, id: &Value, error: &Error) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            let code = Failure::from_error(error);
            e.failure_code = Some(code);
            if e.stage == Stage::Preparation {
                e.preparation_code = Some(code);
            }
            e.reason = match (e.stage, e.reason) {
                (Stage::Launch, Reason::LaunchedUnlinked) => Reason::LaunchedUnlinked,
                (Stage::Launch, Reason::QueuedUnlinked) => Reason::QueuedUnlinked,
                (Stage::Launch, _) => Reason::LaunchFailed,
                (Stage::Preparation, _) => Reason::PreparationFailed,
                _ => Reason::StageFailed,
            };
        }
    }
    pub fn recheck(&mut self, id: &Value, report: &Value) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            e.recheck_capacity = Some(Capacity::observed(report));
        }
    }
    pub fn result(&mut self, id: i64, fingerprint: String) {
        if let Some(e) = self.entries.get_mut(&id) {
            e.metadata_fingerprint = fingerprint;
        }
    }
    pub fn finish(&mut self, at: f64) {
        self.recorded_at = at;
        self.expires_at = at + TTL;
    }
    pub fn publish(&self, root: &Path) -> Result<()> {
        if !self.ordinary {
            return Ok(());
        }
        let bytes = serde_json::to_vec(self)?;
        if bytes.len() as u64 > MAX_BYTES {
            return Err(Error("AdmissionTooLarge"));
        }
        atomic_publish(root, FILE, &bytes)
    }
}

/// Caller bounds the bytes and holds its ordinary writer lock.
pub(crate) fn atomic_publish(root: &Path, name: &str, bytes: &[u8]) -> Result<()> {
        // Fixed sidecar, no backup series or append-only diagnostic log.
        let temp = root.join(format!("{name}.tmp"));
        let mut file = fs::File::create(&temp)?;
        file.write_all(bytes)?;
        file.sync_all()?;
        drop(file);
        use std::os::windows::ffi::OsStrExt;
        let wide = |path: &Path| {
            path.as_os_str()
                .encode_wide()
                .chain(Some(0))
                .collect::<Vec<_>>()
        };
        use windows_sys::Win32::Storage::FileSystem::{
            MoveFileExW, ReplaceFileW, MOVEFILE_REPLACE_EXISTING,
        };
        let target_wide = wide(&root.join(name));
        let temp_wide = wide(&temp);
        let error = unsafe {
            let ok = ReplaceFileW(
                target_wide.as_ptr(),
                temp_wide.as_ptr(),
                std::ptr::null(),
                0,
                std::ptr::null(),
                std::ptr::null(),
            );
            if ok != 0 {
                None
            } else {
                Some(std::io::Error::last_os_error().raw_os_error().unwrap_or(0))
            }
        };
        // ReplaceFile supports a reader retaining the previous file handle.
        // These documented ReplaceFile failures can also leave the old name
        // absent. Finish publishing the already-flushed bounded replacement.
        let error = if error.is_some_and(|e| matches!(e, 2 | 1176 | 1177)) {
            unsafe {
                let ok = MoveFileExW(
                    temp_wide.as_ptr(),
                    target_wide.as_ptr(),
                    MOVEFILE_REPLACE_EXISTING,
                );
                if ok != 0 {
                    None
                } else {
                    Some(std::io::Error::last_os_error().raw_os_error().unwrap_or(0))
                }
            }
        } else {
            error
        };
        if let Some(code) = error {
            if code == 2 {
                if let Ok(file) = fs::File::open(root.join(name)) {
                    let mut current = vec![];
                    if file.take(bytes.len() as u64 + 1).read_to_end(&mut current).is_ok()
                        && current == bytes
                    {
                        return Ok(());
                    }
                }
            }
            return Err(Error(match code {
                32 | 33 => "AdmissionSharingViolation",
                5 => "AdmissionAccessDenied",
                _ => "AdmissionWriteFailed",
            }));
        }
        Ok(())
}

/// No Scheduler/RealBackend, locks, directory creation, backups, guards or processes.
pub fn explain(root: &Path, todo: &Path, id: i64, now: f64) -> Value {
    let mut out = json!({"id":id,"observed_at":null,"recorded_at":null,"checked_at":now,"expires_at":null,
        "metadata_scope":"body_free_control_projection_v1_and_registered_root",
        "metadata_fingerprint":null,"current_metadata_fingerprint":null,
        "dispatch_enabled":null,"complete":null,"incomplete":null,"truncated":null,"dispatch_error":null,
        "cycle_id":null,
        "validity":"missing","observation":null});
    let read = || -> Result<Observation> {
        let file = fs::File::open(root.join(FILE))?;
        if file.metadata()?.len() > MAX_BYTES {
            return Err(Error("AdmissionTooLarge"));
        }
        let snapshot: Observation =
            serde_json::from_reader(std::io::BufReader::new(file.take(MAX_BYTES + 1)))?;
        if snapshot.schema != 1
            || !snapshot.ordinary
            || snapshot.entries.len() > MAX_ENTRIES
            || !snapshot.at.is_finite()
            || snapshot.recorded_at < snapshot.at
            || snapshot.expires_at != snapshot.recorded_at + TTL
            || snapshot.cycle_id.as_ref().is_some_and(|id| id.len()!=32 || !id.bytes().all(|b|b.is_ascii_hexdigit()))
            || snapshot.entries.iter().any(|(id, e)| {
                *id <= 0
                    || *id != e.id
                    || [&e.metadata_fingerprint, &e.input_metadata_fingerprint]
                        .iter()
                        .any(|fp| fp.len() != 64 || !fp.bytes().all(|b| b.is_ascii_hexdigit()))
                    || e.launch_receipt_id.as_ref().is_some_and(|id|id.len()!=32 || !id.bytes().all(|b|b.is_ascii_hexdigit()))
                    || e.returned_job_id.as_ref().is_some_and(|id|id.is_empty() || id.len()>128 || !id.bytes().all(|b|b.is_ascii_alphanumeric() || b"_-".contains(&b)))
            })
        {
            return Err(Error("AdmissionInvalid"));
        }
        Ok(snapshot)
    };
    let snapshot = match read() {
        Ok(s) => s,
        Err(e) => {
            if e.0 != "FileNotFoundError" {
                out["validity"] = json!("corrupt_or_unreadable");
            }
            return out;
        }
    };
    out["observed_at"] = json!(snapshot.at);
    out["cycle_id"] = json!(snapshot.cycle_id);
    out["recorded_at"] = json!(snapshot.recorded_at);
    out["expires_at"] = json!(snapshot.expires_at);
    out["dispatch_enabled"] = json!(snapshot.dispatch_enabled);
    out["complete"] = json!(snapshot.complete);
    out["truncated"] = json!(snapshot.truncated);
    out["incomplete"] = json!(!snapshot.scan_complete);
    out["dispatch_error"] = json!(snapshot.dispatch_error);
    if now < snapshot.at || !now.is_finite() {
        out["validity"] = json!("clock_mismatch");
        return out;
    }
    let Some(entry) = snapshot.entries.get(&id) else {
        out["validity"] = json!(if now > snapshot.expires_at {
            "expired"
        } else if !snapshot.dispatch_enabled {
            "dispatch_disabled"
        } else if !snapshot.scan_complete {
            "incomplete"
        } else if snapshot.truncated {
            "not_recorded_limit"
        } else {
            "not_recorded"
        });
        return out;
    };
    out["metadata_fingerprint"] = json!(entry.metadata_fingerprint);
    out["observation"] = json!({"entry":entry,"capacity":snapshot.capacity,"dispatch_error":snapshot.dispatch_error});
    let current = match metadata::read(todo, id) {
        Ok((item, root)) => metadata::fingerprint(&item, &root),
        Err(_) => {
            out["validity"] = json!("metadata_unavailable");
            return out;
        }
    };
    out["current_metadata_fingerprint"] = json!(current);
    out["validity"] = json!(if now < snapshot.at || !now.is_finite() {
        "clock_mismatch"
    } else if now > snapshot.expires_at {
        "expired"
    } else if current != entry.metadata_fingerprint {
        "metadata_changed"
    } else {
        "matching_task_metadata"
    });
    out
}
