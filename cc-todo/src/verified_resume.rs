//! Per-item receipt checks. This never changes classification or dispatch policy.
use crate::{Paths, Result};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::fs;

fn bytes(path: &std::path::Path) -> Result<Vec<u8>> {
    fs::read(path).map_err(|_| "resume receipt unavailable".into())
}
fn hash(data: &[u8]) -> String {
    format!("{:x}", Sha256::digest(data))
}
fn valid_hash(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn valid_job_id(value: &str) -> bool {
    !value.is_empty()
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}
pub fn verify(
    db: &Value,
    paths: &Paths,
    id: i64,
    status: &str,
    request_hash: &str,
    job_hash: &str,
    resumed_job: &str,
) -> Result<Value> {
    let mismatch = || "resume receipt mismatch".to_owned();
    let item = db["items"]
        .as_array()
        .and_then(|a| a.iter().find(|i| i["id"] == id))
        .ok_or_else(mismatch)?;
    let recovery = &item["recovery"];
    if !matches!(status, "todo" | "doing")
        || item["status"] != "recovery_unverified"
        || recovery["verified_status"] != status
        || recovery["original_id"] != id
        || !valid_hash(request_hash)
        || !valid_hash(job_hash)
        || recovery["preserved_worker_request_sha256"] != request_hash
        || recovery["source_job_sha256"] != job_hash
        || !item["decision_wait"].is_null()
        || item["owner_postponement_recorded"] == true
        || item["stop_kind"].as_str().is_some_and(|s| !s.is_empty())
    {
        return Err(mismatch());
    }
    let request_path = recovery["preserved_worker_request"]
        .as_str()
        .ok_or_else(mismatch)?;
    let request = bytes(std::path::Path::new(request_path))?;
    if hash(&request) != request_hash
        || request.is_empty()
        || std::str::from_utf8(&request).is_err()
    {
        return Err(mismatch());
    }
    let source = recovery["request_source_job_id"]
        .as_str()
        .ok_or_else(mismatch)?;
    if !valid_job_id(source) {
        return Err(mismatch());
    }
    let raw = bytes(&paths.jobs.join(source).join("job.json"))?;
    if hash(&raw) != job_hash {
        return Err(mismatch());
    }
    // Parse exactly the bytes that were hashed; errors never expose job contents.
    let job: Value = serde_json::from_slice(&raw).map_err(|_| mismatch())?;
    if job["id"] != source
        || !(job["todo_id"] == id
            || job["related_todo_ids"]
                .as_array()
                .is_some_and(|ids| ids.contains(&json!(id))))
        || !matches!(
            job["status"].as_str(),
            Some("succeeded" | "failed" | "cancelled" | "canceled")
        )
        || cc_job_policy::cancellation::blocks_recovery(item, &job)
    {
        return Err(mismatch());
    }
    let cwd = job["cwd"].as_str().ok_or_else(mismatch)?;
    if !std::path::Path::new(cwd).is_dir() {
        return Err("resume worktree unavailable".into());
    }
    if !valid_job_id(resumed_job) || resumed_job == source {
        return Err(mismatch());
    }
    let current: Value =
        serde_json::from_slice(&bytes(&paths.jobs.join(resumed_job).join("job.json"))?)
            .map_err(|_| mismatch())?;
    let session = job["thread_id"].as_str().ok_or_else(mismatch)?;
    if uuid::Uuid::parse_str(session).is_err()
        || current["id"] != resumed_job
        || current["todo_id"] != id
        || current["resume_session"] != session
        || current["cwd"].as_str().map(crate::normalized) != Some(crate::normalized(cwd))
        || !matches!(
            current["status"].as_str(),
            Some("queued" | "starting" | "running")
        )
    {
        return Err(mismatch());
    }
    Ok(json!({"source_job_id":source,"source_job_sha256":job_hash,
        "request_sha256":request_hash,"verified_status":status,"verified_at":crate::now(),
        "resumed_job_id":resumed_job,"resume_session":session}))
}
