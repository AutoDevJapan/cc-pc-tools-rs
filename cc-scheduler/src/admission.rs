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

mod metadata;
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
    Launched,
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
}
impl Failure {
    pub fn from_error(e: &Error) -> Self {
        match e.0 {
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
                _ => Self::ToolFailed,
            },
        }
    }
}
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Entry {
    pub id: i64,
    pub metadata_fingerprint: String,
    pub eligible: Option<bool>,
    pub candidate: Option<bool>,
    pub queue_rank: Option<usize>,
    pub stage: Stage,
    pub reason: Reason,
    pub root_guard_allowed: Option<bool>,
    pub root_excluded: Option<bool>,
    pub preparation_code: Option<Failure>,
    pub recheck_capacity: Option<Capacity>,
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
    expires_at: f64,
    ordinary: bool,
    pub complete: bool,
    pub truncated: bool,
    pub dispatch_enabled: bool,
    pub capacity: Capacity,
    pub entries: BTreeMap<i64, Entry>,
    pub dispatch_error: Option<Failure>,
}
impl Observation {
    pub fn new(at: f64, ordinary: bool, enabled: bool) -> Self {
        Self {
            schema: 1,
            at,
            expires_at: at + TTL,
            ordinary,
            complete: false,
            truncated: false,
            dispatch_enabled: enabled,
            capacity: Capacity::default(),
            entries: BTreeMap::new(),
            dispatch_error: None,
        }
    }
    /// Called only while dispatch is already iterating the original items.
    pub fn item(&mut self, item: &Value, root: &Value) {
        if !self.ordinary {
            return;
        }
        if self.entries.len() >= MAX_ENTRIES {
            self.truncated = true;
            return;
        }
        let Some(id) = item["id"].as_i64().filter(|id| *id > 0) else {
            return;
        };
        let fingerprint = metadata::fingerprint(&metadata::project(item), root);
        self.entries.insert(
            id,
            Entry {
                id,
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
    pub fn failed(&mut self, id: &Value, error: &Error) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            e.preparation_code = Some(Failure::from_error(error));
            // Errors can occur before preparation, or after the launch call.
            e.reason = Reason::PreparationFailed;
        }
    }
    pub fn recheck(&mut self, id: &Value, report: &Value) {
        if let Some(e) = id.as_i64().and_then(|id| self.entries.get_mut(&id)) {
            e.recheck_capacity = Some(Capacity::observed(report));
        }
    }
    pub fn publish(&self, root: &Path) -> Result<()> {
        if !self.ordinary {
            return Ok(());
        }
        let bytes = serde_json::to_vec(self)?;
        if bytes.len() as u64 > MAX_BYTES {
            return Err(Error("AdmissionTooLarge"));
        }
        // One replaceable temp, no backup series or append-only diagnostic log.
        let temp = root.join("admission.tmp");
        let mut file = fs::File::create(&temp)?;
        file.write_all(&bytes)?;
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
        let replaced = unsafe {
            ReplaceFileW(
                wide(&root.join(FILE)).as_ptr(),
                wide(&temp).as_ptr(),
                std::ptr::null(),
                0,
                std::ptr::null(),
                std::ptr::null(),
            )
        } != 0;
        // ReplaceFile supports a reader retaining the previous file handle.
        // Only the initial missing destination uses a rename.
        if !replaced
            && (std::io::Error::last_os_error().raw_os_error() != Some(2)
                || unsafe {
                    MoveFileExW(
                        wide(&temp).as_ptr(),
                        wide(&root.join(FILE)).as_ptr(),
                        MOVEFILE_REPLACE_EXISTING,
                    )
                } == 0)
        {
            return Err(Error(
                match std::io::Error::last_os_error().raw_os_error() {
                    Some(32) | Some(33) => "AdmissionSharingViolation",
                    Some(5) => "AdmissionAccessDenied",
                    _ => "AdmissionWriteFailed",
                },
            ));
        }
        Ok(())
    }
}

/// No Scheduler/RealBackend, locks, directory creation, backups, guards or processes.
pub fn explain(root: &Path, todo: &Path, id: i64, now: f64) -> Value {
    let mut out = json!({"id":id,"observed_at":null,"expires_at":null,
        "metadata_scope":"task_control_metadata_and_registered_root",
        "metadata_fingerprint":null,"current_metadata_fingerprint":null,
        "validity":"missing","observation":null});
    let read = || -> Result<Observation> {
        let file = fs::File::open(root.join(FILE))?;
        if file.metadata()?.len() > MAX_BYTES {
            return Err(Error("AdmissionTooLarge"));
        }
        let snapshot: Observation = serde_json::from_reader(file.take(MAX_BYTES + 1))?;
        if snapshot.schema != 1
            || !snapshot.ordinary
            || snapshot.entries.len() > MAX_ENTRIES
            || !snapshot.at.is_finite()
            || snapshot.expires_at != snapshot.at + TTL
            || snapshot.entries.iter().any(|(id, e)| {
                *id <= 0
                    || *id != e.id
                    || e.metadata_fingerprint.len() != 64
                    || !e
                        .metadata_fingerprint
                        .bytes()
                        .all(|b| b.is_ascii_hexdigit())
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
    out["expires_at"] = json!(snapshot.expires_at);
    out["dispatch_enabled"] = json!(snapshot.dispatch_enabled);
    out["complete"] = json!(snapshot.complete);
    out["truncated"] = json!(snapshot.truncated);
    if now < snapshot.at || !now.is_finite() {
        out["validity"] = json!("clock_mismatch");
        return out;
    }
    let Some(entry) = snapshot.entries.get(&id) else {
        out["validity"] = json!(if now > snapshot.expires_at {
            "expired"
        } else if !snapshot.dispatch_enabled {
            "dispatch_disabled"
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
