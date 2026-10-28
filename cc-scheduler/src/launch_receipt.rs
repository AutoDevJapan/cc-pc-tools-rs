//! Bounded evidence of a normal launch return. This module never launches work.
use crate::{admission, common::*};
use cc_job_policy::launch_outcome::{Outcome, path_fingerprint};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{fs, io::Read, path::Path, time::Duration};

pub const MAX_ENTRIES: usize = 128;
pub const MAX_BYTES: u64 = 256 * 1024;
pub const TTL: f64 = 86400.;
pub const MAX_NO_ACTION_REPLAYS: u64 = 3;
const IMPLEMENTATION_TASK_ID: u64 = 4119;
const FILE: &str = "launch-outcomes.json";

pub(crate) fn job_state_fingerprint(jobs: &[Value], id: &str) -> String {
    let observed = jobs
        .iter()
        .find(|job| job["id"] == id)
        .map(|job| {
            serde_json::to_value(Outcome::from_job(
                cc_job_policy::launch_outcome::Kind::Unknown,
                job,
            ))
            .unwrap()
        })
        .unwrap_or_else(|| json!({"job_id":id,"metadata_missing":true}));
    cc_job_policy::product_retry::digest(&serde_json::to_vec(&observed).unwrap())
}
pub(crate) fn deferred_unchanged(item: &Value, root: &Value, jobs: &[Value]) -> bool {
    let deferred = &item["scheduler"]["launch_outcome_deferred"];
    matches!(
        s(&deferred["reason"]),
        "launch_outcome_unverified" | "existing_job_no_action"
    ) && !s(&deferred["job"]).is_empty()
        && deferred["launch_key"] == item["scheduler"]["key"]
        && deferred["request_fingerprint"] == admission::defer_fingerprint(item, root)
        && deferred["job_state_fingerprint"] == job_state_fingerprint(jobs, s(&deferred["job"]))
}

pub(crate) fn linked_to_return(row: &Value, job: &str, key: &Value) -> bool {
    row["status"] == "doing"
        && row["scheduler"]["job"] == job
        && arr(&row["jobs"]).contains(&json!(job))
        && (row["scheduler"]["key"] == *key
            || (row["worker_receipt"]["job"] == job
                && row["resume_generation"].is_u64()
                && row["worker_receipt"]["generation"] == row["resume_generation"]))
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum Binding {
    Matching,
    Mismatch,
    Unknown,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, PartialEq, Eq, PartialOrd, Ord)]
#[serde(rename_all = "snake_case")]
pub enum MismatchField {
    LaunchKey,
    TodoOwner,
    Worktree,
    Branch,
    Generation,
    Attempt,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct WorkerLink {
    pub job_id: String,
    pub generation: u64,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Receipt {
    pub schema: u32,
    pub implementation_todo_id: u64,
    pub target_todo_id: i64,
    pub receipt_id: String,
    pub cycle_id: String,
    pub ordinary_cycle: bool,
    pub observed_at: f64,
    pub expires_at: f64,
    pub request_launch_key: String,
    pub request_generation: Option<u64>,
    pub result_generation: Option<u64>,
    pub result_launch_key: Option<String>,
    pub request_attempt: Option<u64>,
    pub request_spec_sha256: Option<String>,
    pub request_dispatch_sha256: Option<String>,
    pub input_binding: Binding,
    pub request_worktree_sha256: Option<String>,
    pub request_branch_sha256: Option<String>,
    pub input_metadata_fingerprint: String,
    pub result_metadata_fingerprint: String,
    pub outcome: Outcome,
    pub binding: Binding,
    pub linked: bool,
    #[serde(default)]
    pub mismatches: Vec<MismatchField>,
    #[serde(default)]
    pub worker_link: Option<WorkerLink>,
    #[serde(default)]
    pub replay_count: Option<u64>,
    #[serde(default)]
    pub replay_first_at: Option<f64>,
}
fn hex(value: &str, len: usize) -> bool {
    value.len() == len
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
impl Receipt {
    pub fn new(
        item: &Value,
        root: &Value,
        cycle: &str,
        ordinary: bool,
        at: f64,
        outcome: Outcome,
    ) -> Self {
        let worktree = s(&item["scheduler"]["worktree"]);
        let expected_cwd = (!worktree.is_empty())
            .then(|| resolve(Path::new(worktree)).ok())
            .flatten()
            .and_then(|path| path_fingerprint(&path.to_string_lossy()));
        let branch = item["scheduler"]["branch"]
            .as_str()
            .map(|b| cc_job_policy::product_retry::digest(b.as_bytes()));
        let worker_link = item["resume_generation"]
            .as_u64()
            .filter(|generation| {
                outcome.kind == cc_job_policy::launch_outcome::Kind::ExistingJobNoAction
                    && item["worker_receipt"]["job"] == outcome.job_id
                    && item["worker_receipt"]["generation"].as_u64() == Some(*generation)
                    && outcome.job_resume_generation == Some(*generation)
            })
            .map(|generation| WorkerLink {
                job_id: outcome.job_id.clone(),
                generation,
            });
        let mut mismatches = vec![];
        for (changed, field) in [
            (
                outcome
                    .launch_key
                    .as_deref()
                    .is_some_and(|key| key != s(&item["scheduler"]["key"])),
                MismatchField::LaunchKey,
            ),
            (
                outcome
                    .todo_id
                    .is_some_and(|id| Some(id) != item["id"].as_u64()),
                MismatchField::TodoOwner,
            ),
            (
                outcome.registration_generation.is_some_and(|generation| {
                    Some(generation) != item["resume_generation"].as_u64()
                }) && worker_link.is_none(),
                MismatchField::Generation,
            ),
            (
                outcome
                    .registration_attempt
                    .is_some_and(|attempt| Some(attempt) != item["scheduler"]["attempt"].as_u64()),
                MismatchField::Attempt,
            ),
            (
                outcome
                    .branch_sha256
                    .as_ref()
                    .zip(branch.as_ref())
                    .is_some_and(|(a, b)| a != b),
                MismatchField::Branch,
            ),
            (
                outcome
                    .cwd_sha256
                    .as_ref()
                    .zip(expected_cwd.as_ref())
                    .is_some_and(|(a, b)| a != b),
                MismatchField::Worktree,
            ),
        ] {
            if changed {
                mismatches.push(field);
            }
        }
        let binding = if outcome.kind == cc_job_policy::launch_outcome::Kind::Unknown {
            Binding::Unknown
        } else if !mismatches.is_empty() {
            Binding::Mismatch
        } else if outcome.launch_key.is_some()
            && outcome.todo_id.is_some()
            && outcome.cwd_sha256.is_some()
            && expected_cwd.is_some()
        {
            Binding::Matching
        } else {
            Binding::Unknown
        };
        let fingerprint = admission::result_fingerprint(item, root);
        let input = &item["scheduler"]["launch_input"];
        let dispatch_sha256 = (input["launch_key"] == item["scheduler"]["key"])
            .then(|| input["dispatch_sha256"].as_str().map(str::to_string))
            .flatten();
        let input_binding = match dispatch_sha256
            .as_ref()
            .zip(outcome.dispatch_sha256.as_ref())
        {
            Some((a, b)) if a == b => Binding::Matching,
            Some(_) => Binding::Mismatch,
            None => Binding::Unknown,
        };
        Self {
            schema: 1,
            implementation_todo_id: IMPLEMENTATION_TASK_ID,
            target_todo_id: item["id"].as_i64().unwrap_or(0),
            receipt_id: uuid::Uuid::new_v4().simple().to_string(),
            cycle_id: cycle.to_string(),
            ordinary_cycle: ordinary,
            observed_at: at,
            expires_at: at + TTL,
            request_launch_key: s(&item["scheduler"]["key"]).to_string(),
            request_generation: item["resume_generation"].as_u64(),
            result_generation: item["resume_generation"].as_u64(),
            result_launch_key: item["scheduler"]["key"].as_str().map(str::to_string),
            request_attempt: item["scheduler"]["attempt"].as_u64(),
            request_spec_sha256: (input["launch_key"] == item["scheduler"]["key"])
                .then(|| input["spec_sha256"].as_str().map(str::to_string))
                .flatten(),
            request_dispatch_sha256: dispatch_sha256,
            input_binding,
            request_worktree_sha256: expected_cwd,
            request_branch_sha256: branch,
            input_metadata_fingerprint: fingerprint.clone(),
            result_metadata_fingerprint: fingerprint,
            outcome,
            binding,
            linked: false,
            mismatches,
            worker_link,
            replay_count: None,
            replay_first_at: None,
        }
    }
    pub fn valid(&self) -> bool {
        self.schema == 1
            && self.implementation_todo_id > 0
            && self.target_todo_id > 0
            && hex(&self.receipt_id, 32)
            && hex(&self.cycle_id, 32)
            && hex(&self.request_launch_key, 32)
            && self
                .result_launch_key
                .as_ref()
                .is_none_or(|key| hex(key, 32))
            && (self.linked
                || self.result_launch_key.as_deref() == Some(self.request_launch_key.as_str()))
            && self.observed_at.is_finite()
            && self.expires_at == self.observed_at + TTL
            && hex(&self.input_metadata_fingerprint, 64)
            && hex(&self.result_metadata_fingerprint, 64)
            && [
                &self.request_worktree_sha256,
                &self.request_branch_sha256,
                &self.request_spec_sha256,
                &self.request_dispatch_sha256,
            ]
            .iter()
            .all(|v| v.as_ref().is_none_or(|v| hex(v, 64)))
            && self.outcome.valid()
            && self.replay_count.is_none_or(|count| count > 0)
            && self
                .replay_first_at
                .is_none_or(|at| at.is_finite() && at <= self.observed_at)
            && self.mismatches.len() <= 6
            && self
                .mismatches
                .iter()
                .collect::<std::collections::BTreeSet<_>>()
                .len()
                == self.mismatches.len()
            && self.worker_link.as_ref().is_none_or(|link| {
                link.job_id == self.outcome.job_id
                    && Some(link.generation) == self.request_generation
                    && Some(link.generation) == self.outcome.job_resume_generation
                    && self.outcome.kind == cc_job_policy::launch_outcome::Kind::ExistingJobNoAction
            })
            && (!self.linked || self.binding == Binding::Matching)
            && (self.input_binding != Binding::Matching
                || (self.request_dispatch_sha256.is_some()
                    && self.request_dispatch_sha256 == self.outcome.dispatch_sha256))
            && (self.binding != Binding::Matching
                || (self.mismatches.is_empty()
                    && self.outcome.launch_key.as_deref()
                        == Some(self.request_launch_key.as_str())
                    && self.outcome.todo_id == Some(self.target_todo_id as u64)
                    && self.outcome.cwd_sha256.is_some()
                    && self.outcome.cwd_sha256 == self.request_worktree_sha256
                    && self
                        .outcome
                        .registration_generation
                        .is_none_or(|generation| {
                            Some(generation) == self.request_generation
                                || self.worker_link.is_some()
                        })
                    && self
                        .outcome
                        .registration_attempt
                        .is_none_or(|attempt| Some(attempt) == self.request_attempt)
                    && self
                        .outcome
                        .branch_sha256
                        .as_ref()
                        .zip(self.request_branch_sha256.as_ref())
                        .is_none_or(|(a, b)| a == b)))
    }
}

#[derive(Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Store {
    schema: u32,
    receipts: Vec<Receipt>,
    #[serde(default)]
    recovery: Option<Recovery>,
}
#[derive(Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum RecoveryKind {
    Invalid,
    TooLarge,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Recovery {
    reason: RecoveryKind,
    at: f64,
}

fn read(root: &Path) -> Result<Store> {
    read_store(root, false)
}
fn read_store(root: &Path, for_publication: bool) -> Result<Store> {
    let file = fs::File::open(root.join(FILE))?;
    if file.metadata()?.len() > MAX_BYTES {
        return Err(Error("LaunchReceiptTooLarge"));
    }
    let mut bytes = vec![];
    file.take(MAX_BYTES + 1).read_to_end(&mut bytes)?;
    if bytes.len() as u64 > MAX_BYTES {
        return Err(Error("LaunchReceiptTooLarge"));
    }
    #[derive(Deserialize)]
    struct Schema {
        schema: u32,
    }
    if serde_json::from_slice::<Schema>(&bytes)?.schema != 1 {
        return Err(Error("LaunchReceiptSchemaUnsupported"));
    }
    let store: Store = serde_json::from_slice(&bytes)?;
    if store.schema != 1
        || (!for_publication && store.receipts.len() > MAX_ENTRIES)
        || store.receipts.iter().any(|r| !r.valid())
        || store.recovery.as_ref().is_some_and(|r| !r.at.is_finite())
    {
        return Err(Error("LaunchReceiptInvalid"));
    }
    let mut ids = std::collections::BTreeSet::new();
    let mut receipt_ids = std::collections::BTreeSet::new();
    if store
        .receipts
        .iter()
        .any(|r| !ids.insert(r.target_todo_id) || !receipt_ids.insert(r.receipt_id.as_str()))
    {
        return Err(Error("LaunchReceiptInvalid"));
    }
    Ok(store)
}

/// A single bounded snapshot, serialized independently of the canonical Root/job writes.
pub fn publish(root: &Path, receipt: &Receipt) -> Result<()> {
    if !receipt.valid() {
        return Err(Error("LaunchReceiptInvalid"));
    }
    let _lock = ByteLock::acquire(&root.join("launch-outcomes.lock"), Duration::ZERO)?
        .ok_or(Error("LaunchReceiptBusy"))?;
    let mut store = match read_store(root, true) {
        Ok(store) => store,
        Err(e) if e.0 == "FileNotFoundError" => Store {
            schema: 1,
            receipts: vec![],
            recovery: None,
        },
        Err(e)
            if matches!(
                e.0,
                "LaunchReceiptInvalid" | "JSONDecodeError" | "LaunchReceiptTooLarge"
            ) =>
        {
            Store {
                schema: 1,
                receipts: vec![],
                recovery: Some(Recovery {
                    at: receipt.observed_at,
                    reason: if e.0 == "LaunchReceiptTooLarge" {
                        RecoveryKind::TooLarge
                    } else {
                        RecoveryKind::Invalid
                    },
                }),
            }
        }
        Err(e) => return Err(e),
    };
    if store.receipts.iter().any(|r| {
        r.target_todo_id == receipt.target_todo_id
            && r.receipt_id != receipt.receipt_id
            && r.observed_at > receipt.observed_at
    }) {
        return Err(Error("LaunchReceiptSuperseded"));
    }
    store.receipts.retain(|r| {
        r.target_todo_id != receipt.target_todo_id && r.expires_at >= receipt.observed_at
    });
    if store
        .receipts
        .iter()
        .any(|r| r.receipt_id == receipt.receipt_id)
    {
        return Err(Error("LaunchReceiptInvalid"));
    }
    store.receipts.push(receipt.clone());
    store
        .receipts
        .sort_by(|a, b| a.observed_at.total_cmp(&b.observed_at));
    while store.receipts.len() > MAX_ENTRIES {
        let oldest = store
            .receipts
            .iter()
            .position(|r| r.receipt_id != receipt.receipt_id)
            .ok_or(Error("LaunchReceiptTooLarge"))?;
        store.receipts.remove(oldest);
    }
    let mut bytes = serde_json::to_vec(&store)?;
    while bytes.len() as u64 > MAX_BYTES && store.receipts.len() > 1 {
        let oldest = store
            .receipts
            .iter()
            .position(|r| r.receipt_id != receipt.receipt_id)
            .ok_or(Error("LaunchReceiptTooLarge"))?;
        store.receipts.remove(oldest);
        bytes = serde_json::to_vec(&store)?;
    }
    if bytes.len() as u64 > MAX_BYTES {
        return Err(Error("LaunchReceiptTooLarge"));
    }
    admission::atomic_publish(root, FILE, &bytes)
}

/// Read only saved evidence and the existing body-free control projection.
/// No Scheduler, Backend, locks, initialization, Source, models, auth or process queries.
pub fn explain(root: &Path, todo: &Path, id: i64, now: f64) -> Value {
    let mut out = json!({"namespace":"SchedulerLaunchOutcomeReceipt","id":id,"checked_at":now,
        "validity":"missing","receipt":null,"current_metadata_fingerprint":null,"input_validity":"not_recorded","store_recovery":null,
        "metadata_scope":"body_free_control_projection_v1_and_registered_root",
        "input_evidence_scope":"saved_normal_registration_only","current_source_bytes_checked":false});
    let store = match read(root) {
        Ok(store) => store,
        Err(e) => {
            if e.0 == "LaunchReceiptSchemaUnsupported" {
                out["validity"] = json!("schema_unsupported");
            } else if e.0 != "FileNotFoundError" {
                out["validity"] = json!("corrupt_or_unreadable");
            }
            return out;
        }
    };
    out["store_recovery"] = json!(store.recovery);
    let Some(receipt) = store.receipts.iter().find(|r| r.target_todo_id == id) else {
        out["validity"] = json!("not_recorded");
        return out;
    };
    out["receipt"] = json!(receipt);
    out["input_validity"] = json!(match receipt.input_binding {
        Binding::Matching => "matching_saved_input",
        Binding::Mismatch => "input_mismatch",
        Binding::Unknown => "not_recorded",
    });
    if !now.is_finite() || now < receipt.observed_at {
        out["validity"] = json!("clock_mismatch");
        return out;
    }
    if now > receipt.expires_at {
        out["validity"] = json!("expired");
        return out;
    }
    let (item, root) = match admission::metadata::read(todo, id) {
        Ok(value) => value,
        Err(_) => {
            out["validity"] = json!("metadata_unavailable");
            return out;
        }
    };
    let current = admission::metadata::fingerprint(&item, &root);
    out["current_metadata_fingerprint"] = json!(current);
    out["validity"] = json!(if item["scheduler"]["key"].as_str()
        != receipt.result_launch_key.as_deref()
    {
        "launch_key_changed"
    } else if item["resume_generation"].as_u64() != receipt.result_generation {
        "generation_changed"
    } else if current != receipt.result_metadata_fingerprint {
        "metadata_changed"
    } else if receipt.input_binding == Binding::Mismatch {
        "input_mismatch"
    } else if receipt.binding == Binding::Mismatch {
        if receipt.mismatches.contains(&MismatchField::Generation) {
            "registration_generation_mismatch"
        } else if receipt.mismatches.contains(&MismatchField::Attempt) {
            "registration_attempt_mismatch"
        } else if receipt.mismatches.contains(&MismatchField::Branch) {
            "branch_mismatch"
        } else if receipt.mismatches.contains(&MismatchField::LaunchKey) {
            "returned_launch_key_mismatch"
        } else {
            "owner_mismatch"
        }
    } else if receipt.binding == Binding::Unknown {
        "unknown"
    } else {
        if receipt.input_binding == Binding::Unknown {
            "input_not_recorded"
        } else {
            "matching_task_metadata"
        }
    });
    out
}
