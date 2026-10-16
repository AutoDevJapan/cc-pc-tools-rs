//! Explicitly bind an existing, unstarted reader to fixed source after its writer exits.
//! Only its proven automatic owner edge changes; no ToDo or live-owner exemption is created.
use crate::{product_retry::digest, todo_wait, work_owner};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

/// Version 2 enforces external-tool isolation in the actual spawn builder.
pub const EXECUTION_PROTOCOL: u64 = 2;

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub job_id: String,
    pub todo_id: i64,
    pub parent_job_id: String,
    pub parent_todo_id: i64,
    pub owner_job_id: String,
    pub owner_todo_id: i64,
    pub read_only: bool,
    pub child_spec_sha256: String,
    pub parent_spec_sha256: String,
    pub prompt_sha256: String,
    pub fixed_base: String,
    pub fixed_head: String,
}
fn s<'a>(v: &'a Value, k: &str) -> &'a str {
    v[k].as_str().unwrap_or("")
}
fn row(db: &Value, id: i64) -> Result<&Value, String> {
    db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|i| i["id"] == id)
        .ok_or_else(|| "fixed review ToDo missing".into())
}
fn linked(row: &Value, job: &Value) -> bool {
    row["id"] == job["todo_id"]
        && row["scheduler"]["job"] == job["id"]
        && row["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&job["id"]))
}
fn checkout(row: &Value, job: &Value) -> Result<Value, String> {
    let actual = work_owner::checkout(s(job, "cwd"));
    let saved = work_owner::from_job(job);
    if !["repository", "git_common", "worktree", "branch"]
        .iter()
        .all(|k| !s(&saved, k).is_empty() && saved[*k] == actual[*k])
        || row["scheduler"]["branch"] != actual["branch"]
        || work_owner::norm(std::path::Path::new(s(&row["scheduler"], "worktree")))
            != actual["worktree"]
        || !s(&actual, "branch").starts_with(&format!("scheduler/todo-{}-", row["id"]))
    {
        return Err("fixed review checkout changed".into());
    }
    Ok(saved)
}
fn stopped(job: &Value) -> bool {
    job["status"] == "succeeded"
        && job["exit_code"] == 0
        && chrono::DateTime::parse_from_rfc3339(s(job, "finished_at")).is_ok()
        && s(job, "parent_job_id").is_empty()
        && !job["subwork"].is_object()
}
pub fn hex(s: &str, size: usize) -> bool {
    s.len() == size && s.bytes().all(|c| c.is_ascii_hexdigit())
}

/// Process absence, matching controller creation time, resource protection and Git
/// base/head/clean source are checked by the CLI while holding queue and state locks.
pub fn plan(
    db: &Value,
    job: &Value,
    parent: &Value,
    owner: &Value,
    r: &Request,
    inputs: [&[u8]; 3],
) -> Result<Value, String> {
    let child = row(db, r.todo_id)?;
    let root = row(db, r.parent_todo_id)?;
    let writer = row(db, r.owner_todo_id)?;
    if !r.read_only
        || [r.todo_id, r.parent_todo_id, r.owner_todo_id]
            .iter()
            .any(|id| *id <= 0)
        || r.todo_id == r.parent_todo_id
        || r.todo_id == r.owner_todo_id
        || r.parent_todo_id == r.owner_todo_id
        || job["id"] != r.job_id
        || parent["id"] != r.parent_job_id
        || owner["id"] != r.owner_job_id
        || !linked(child, job)
        || !linked(root, parent)
        || !linked(writer, owner)
        || !["todo", "doing"].contains(&s(child, "status"))
        || child["dispatchable"] != true
        || child["owner_stopped"] == true
        || !child["wait_ready"].is_null()
        || !child["work_owner_job"].is_null()
        || todo_wait::waiting(db, child)
        || root["status"] != "wait"
        || root["owner_stopped"] == true
        || !root["wait_ready"].is_null()
        || !todo_wait::pending_on(db, root).contains(&r.todo_id)
        || !["todo", "doing", "wait"].contains(&s(writer, "status"))
        || writer["owner_stopped"] == true
        || !stopped(parent)
        || !stopped(owner)
        || job["status"] != "queued"
        || job["cancel_requested"] == true
        || !job["started_at"].is_null()
        || !job["thread_id"].is_null()
        || !job["codex_pid"].is_null()
        || job["steps"] != 0
        || !s(job, "parent_job_id").is_empty()
        || job["subwork"].is_object()
        || job["related_todo_ids"]
            .as_array()
            .is_some_and(|a| !a.is_empty())
        || s(job, "launch_key").is_empty()
        || child["scheduler"]["key"] != job["launch_key"]
        || !hex(&r.fixed_head, 40)
        || !hex(&r.fixed_base, 40)
        || [child, root, writer]
            .iter()
            .any(|row| row["subwork"].is_object())
    {
        return Err(
            "fixed review needs its exact unstarted worker, waiting root and finished writer"
                .into(),
        );
    }
    if inputs.iter().any(|b| b.is_empty())
        || digest(inputs[0]) != r.child_spec_sha256
        || digest(inputs[1]) != r.parent_spec_sha256
        || digest(inputs[2]) != r.prompt_sha256
    {
        return Err("fixed review instructions changed".into());
    }
    // An explicit role can narrow an old launcher's default, never a writer's owner request.
    if job["sandbox"] != "read-only"
        && (job["scope_source"] != "launcher"
            || !["workspace-write", "danger-full-access"].contains(&s(job, "sandbox")))
        || job["network"] != false
    {
        return Err("fixed review cannot widen or replace an explicit execution scope".into());
    }
    let identities = [
        checkout(child, job)?,
        checkout(root, parent)?,
        checkout(writer, owner)?,
    ];
    let a = &identities[0];
    if !a["pr"].is_u64()
        || identities[1..].iter().any(|b| {
            ["repository", "git_common", "pr"]
                .iter()
                .any(|k| a[*k] != b[*k])
                || a["worktree"] == b["worktree"]
        })
    {
        return Err("fixed review source and writer do not share verified ownership".into());
    }
    if job["work_wait_on"] != json!([r.owner_todo_id])
        || job["work_wait_evidence"]["owner_job_id"] != owner["id"]
        || job["work_wait_evidence"]["owner"] != work_owner::owner_evidence(owner)
        || job["work_wait_evidence"]["candidate_identity"] != job["work_owner"]
    {
        return Err("fixed review requires the exact automatic writer edge".into());
    }
    let mut result = job.clone();
    if !result["work_wait_history"].is_array() {
        result["work_wait_history"] = json!([]);
    }
    result["work_wait_history"].as_array_mut().unwrap().push(json!({
        "reason":"fixed_read_only_review", "request":r, "wait_on":job["work_wait_on"],
        "evidence":job["work_wait_evidence"], "owner_finished_at":owner["finished_at"],
        "prior_scope":{"sandbox":job["sandbox"],"approval_policy":job["approval_policy"],"approvals_reviewer":job["approvals_reviewer"]}}));
    result["work_wait_on"] = json!([]);
    result["work_wait_evidence"] = Value::Null;
    result["queue_wait_reason"] = Value::Null;
    result["fixed_read_only_review"] = json!({"request":r,"source_cwd":parent["cwd"],"spec":child["spec"],"identity":job["work_owner"]});
    result["sandbox"] = json!("read-only");
    result["approval_policy"] = json!("never");
    result["approvals_reviewer"] = json!("user");
    Ok(result)
}

/// Checked again immediately before the original worker executes. No persisted
/// classification may turn into write access or consume a different instruction set.
pub fn binding(db: &Value, job: &Value, spec: &[u8], prompt: &[u8]) -> Result<Request, String> {
    let b = &job["fixed_read_only_review"];
    let r: Request =
        serde_json::from_value(b["request"].clone()).map_err(|_| "invalid fixed review binding")?;
    let child = row(db, r.todo_id)?;
    let root = row(db, r.parent_todo_id)?;
    if r.job_id != s(job, "id")
        || !r.read_only
        || !linked(child, job)
        || !["todo", "doing"].contains(&s(child, "status"))
        || child["owner_stopped"] == true
        || child["dispatchable"] != true
        || root["owner_stopped"] == true
        || !["todo", "doing", "wait"].contains(&s(root, "status"))
        || !root["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&json!(r.parent_job_id)))
        || work_owner::norm(std::path::Path::new(s(&root["scheduler"], "worktree")))
            != work_owner::norm(std::path::Path::new(s(b, "source_cwd")))
        || job["related_todo_ids"]
            .as_array()
            .is_some_and(|ids| !ids.is_empty())
        || !child["wait_ready"].is_null()
        || todo_wait::waiting(db, child)
        || b["spec"] != child["spec"]
        || digest(spec) != r.child_spec_sha256
        || digest(prompt) != r.prompt_sha256
        || b["identity"] != job["work_owner"]
        || job["sandbox"] != "read-only"
        || job["approval_policy"] != "never"
        || job["approvals_reviewer"] != "user"
        || job["network"] != false
        || checkout(child, job)? != b["identity"]
        || !hex(&r.fixed_head, 40)
        || !hex(&r.fixed_base, 40)
    {
        return Err("fixed read-only review changed before execution".into());
    }
    Ok(r)
}
