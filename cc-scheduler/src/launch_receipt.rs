//! Bounded evidence of a normal launch return. This module never launches work.
use crate::{admission, common::*};
use cc_job_policy::launch_outcome::{Outcome, path_fingerprint};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{fs, io::Read, path::Path, time::Duration};

pub const MAX_ENTRIES: usize = 128;
pub const MAX_BYTES: u64 = 256 * 1024;
pub const TTL: f64 = 86400.;
const FILE: &str = "launch-outcomes.json";

#[derive(Clone, Copy, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum Binding {
    Matching,
    Mismatch,
    Unknown,
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
        let expected_cwd = path_fingerprint(worktree);
        let branch = item["scheduler"]["branch"]
            .as_str()
            .map(|b| cc_job_policy::product_retry::digest(b.as_bytes()));
        let binding = if outcome.kind == cc_job_policy::launch_outcome::Kind::Unknown {
            Binding::Unknown
        } else if outcome
            .launch_key
            .as_deref()
            .is_some_and(|key| key != s(&item["scheduler"]["key"]))
            || outcome
                .todo_id
                .is_some_and(|id| Some(id) != item["id"].as_u64())
            || outcome
                .registration_generation
                .is_some_and(|generation| Some(generation) != item["resume_generation"].as_u64())
            || outcome
                .registration_attempt
                .is_some_and(|attempt| Some(attempt) != item["scheduler"]["attempt"].as_u64())
            || outcome
                .branch_sha256
                .as_ref()
                .zip(branch.as_ref())
                .is_some_and(|(a, b)| a != b)
            || outcome
                .cwd_sha256
                .as_ref()
                .zip(expected_cwd.as_ref())
                .is_some_and(|(a, b)| a != b)
        {
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
            implementation_todo_id: 4119,
            target_todo_id: item["id"].as_i64().unwrap_or(0),
            receipt_id: uuid::Uuid::new_v4().simple().to_string(),
            cycle_id: cycle.to_string(),
            ordinary_cycle: ordinary,
            observed_at: at,
            expires_at: at + TTL,
            request_launch_key: s(&item["scheduler"]["key"]).to_string(),
            request_generation: item["resume_generation"].as_u64(),
            result_generation: item["resume_generation"].as_u64(),
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
        }
    }
    pub fn valid(&self) -> bool {
        self.schema == 1
            && self.implementation_todo_id == 4119
            && self.target_todo_id > 0
            && hex(&self.receipt_id, 32)
            && hex(&self.cycle_id, 32)
            && hex(&self.request_launch_key, 32)
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
            && (!self.linked || self.binding == Binding::Matching)
            && (self.input_binding != Binding::Matching
                || (self.request_dispatch_sha256.is_some()
                    && self.request_dispatch_sha256 == self.outcome.dispatch_sha256))
            && (self.binding != Binding::Matching
                || (self.outcome.launch_key.as_deref() == Some(self.request_launch_key.as_str())
                    && self.outcome.todo_id == Some(self.target_todo_id as u64)
                    && self.outcome.cwd_sha256.is_some()
                    && self.outcome.cwd_sha256 == self.request_worktree_sha256
                    && self
                        .outcome
                        .registration_generation
                        .is_none_or(|generation| Some(generation) == self.request_generation)
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
}

fn read(root: &Path) -> Result<Store> {
    let file = fs::File::open(root.join(FILE))?;
    if file.metadata()?.len() > MAX_BYTES {
        return Err(Error("LaunchReceiptTooLarge"));
    }
    let store: Store = serde_json::from_reader(std::io::BufReader::new(file.take(MAX_BYTES + 1)))?;
    if store.schema != 1
        || store.receipts.len() > MAX_ENTRIES
        || store.receipts.iter().any(|r| !r.valid())
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
    let mut store = match read(root) {
        Ok(store) => store,
        Err(e) if e.0 == "FileNotFoundError" => Store {
            schema: 1,
            receipts: vec![],
        },
        Err(e) => return Err(e),
    };
    if store.receipts.iter().any(|r| {
        r.target_todo_id == receipt.target_todo_id
            && r.receipt_id != receipt.receipt_id
            && r.observed_at > receipt.observed_at
    }) {
        return Err(Error("LaunchReceiptSuperseded"));
    }
    store
        .receipts
        .retain(|r| r.target_todo_id != receipt.target_todo_id);
    store.receipts.push(receipt.clone());
    if store.receipts.len() > MAX_ENTRIES {
        store.receipts.remove(0);
    }
    let bytes = serde_json::to_vec(&store)?;
    if bytes.len() as u64 > MAX_BYTES {
        return Err(Error("LaunchReceiptTooLarge"));
    }
    admission::atomic_publish(root, FILE, &bytes)
}

/// Read only saved evidence and the existing body-free control projection.
/// No Scheduler, Backend, locks, initialization, Source, models, auth or process queries.
pub fn explain(root: &Path, todo: &Path, id: i64, now: f64) -> Value {
    let mut out = json!({"namespace":"SchedulerLaunchOutcomeReceipt","id":id,"checked_at":now,
        "validity":"missing","receipt":null,"current_metadata_fingerprint":null,
        "metadata_scope":"body_free_control_projection_v1_and_registered_root",
        "input_evidence_scope":"saved_normal_registration_only","current_source_bytes_checked":false});
    let store = match read(root) {
        Ok(store) => store,
        Err(e) => {
            if e.0 != "FileNotFoundError" {
                out["validity"] = json!("corrupt_or_unreadable");
            }
            return out;
        }
    };
    let Some(receipt) = store.receipts.iter().find(|r| r.target_todo_id == id) else {
        out["validity"] = json!("not_recorded");
        return out;
    };
    out["receipt"] = json!(receipt);
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
    out["validity"] = json!(if item["scheduler"]["key"] != receipt.request_launch_key {
        "launch_key_changed"
    } else if item["resume_generation"].as_u64() != receipt.result_generation {
        "generation_changed"
    } else if current != receipt.result_metadata_fingerprint {
        "metadata_changed"
    } else if receipt.input_binding == Binding::Mismatch {
        "input_mismatch"
    } else if receipt.binding == Binding::Mismatch {
        "owner_mismatch"
    } else if receipt.binding == Binding::Unknown {
        "unknown"
    } else {
        "matching_task_metadata"
    });
    out
}
