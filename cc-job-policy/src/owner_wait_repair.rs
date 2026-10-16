//! Explicit repair of one never-started finite worker's remembered automatic edge.
//! This does not grant subwork, a live-owner exemption, or execution admission.
use crate::{product_retry::digest, todo_wait, work_owner};
use serde_json::{Value, json};

fn s<'a>(v: &'a Value, k: &str) -> &'a str {
    v[k].as_str().unwrap_or("")
}
fn row<'a>(db: &'a Value, id: &Value) -> Result<&'a Value, String> {
    db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|i| i["id"] == *id)
        .ok_or_else(|| "repair ToDo is missing".into())
}
fn linked(item: &Value, job: &Value) -> bool {
    item["id"] == job["todo_id"]
        && item["jobs"]
            .as_array()
            .is_some_and(|a| a.contains(&job["id"]))
}

/// The caller holds queue, both job state locks and the ToDo lock, and verifies
/// the queued controller's creation time and both workers' absence independently.
/// Hashes pin the operator-reviewed finite instructions, not a guessed task title.
pub fn plan(
    db: &Value,
    job: &Value,
    owner: &Value,
    request: &Value,
    child_spec: &[u8],
    parent_spec: &[u8],
    prompt: &[u8],
) -> Result<Value, String> {
    let child = row(db, &request["todo_id"])?;
    let parent = row(db, &request["parent_todo_id"])?;
    if !["research", "check", "review"].contains(&s(request, "kind"))
        || child["id"] == parent["id"]
        || request["job_id"] != job["id"]
        || request["owner_job_id"] != owner["id"]
        || !linked(child, job)
        || !linked(parent, owner)
        || !["todo", "doing"].contains(&s(child, "status"))
        || child["dispatchable"] != true
        || !child["wait_ready"].is_null()
        || child["owner_stopped"] == true
        || parent["status"] != "wait"
        || !parent["wait_ready"].is_null()
        || !todo_wait::pending_on(db, parent)
            .contains(&child["id"].as_i64().ok_or("invalid child id")?)
        || !child["work_owner_job"].is_null()
        || child["subwork"].is_object()
        || parent["subwork"].is_object()
        || job["subwork"].is_object()
        || !s(job, "parent_job_id").is_empty()
        || !s(owner, "parent_job_id").is_empty()
        || job["status"] != "queued"
        || job["cancel_requested"] == true
        || !job["started_at"].is_null()
        || !job["thread_id"].is_null()
        || !job["codex_pid"].is_null()
        || job["steps"].as_u64().unwrap_or(0) != 0
        || owner["status"] != "succeeded"
        || owner["exit_code"] != 0
        || s(owner, "finished_at").is_empty()
        || child["scheduler"]["job"] != job["id"]
        || parent["scheduler"]["job"] != owner["id"]
        || s(job, "launch_key").is_empty()
        || child["scheduler"]["key"] != job["launch_key"]
        || s(child, "spec").is_empty()
        || s(parent, "spec").is_empty()
        || child_spec.is_empty()
        || parent_spec.is_empty()
        || prompt.is_empty()
        || request["child_spec_sha256"] != digest(child_spec)
        || request["parent_spec_sha256"] != digest(parent_spec)
        || request["prompt_sha256"] != digest(prompt)
    {
        return Err(
            "repair requires the exact unstarted finite job and successful waiting parent".into(),
        );
    }
    // Manual prerequisites are never removed, including indirect parent edges.
    let mut stack = todo_wait::pending_on(db, child);
    let mut seen = std::collections::BTreeSet::new();
    while let Some(id) = stack.pop() {
        if id == parent["id"] {
            return Err("manual parent dependency is not repairable".into());
        }
        if seen.insert(id) {
            stack.extend(todo_wait::pending_on(db, row(db, &json!(id))?));
        }
    }
    let evidence = &job["work_wait_evidence"];
    if !job["work_wait_on"]
        .as_array()
        .is_some_and(|a| a.contains(&parent["id"]) && a.iter().all(Value::is_i64))
        || evidence["owner_job_id"] != owner["id"]
        || evidence["owner"] != work_owner::owner_evidence(owner)
        || evidence["candidate_identity"] != job["work_owner"]
    {
        return Err("exact automatic owner provenance is required".into());
    }
    let a = work_owner::from_job(job);
    let b = work_owner::from_job(owner);
    if !["repository", "git_common"]
        .iter()
        .all(|k| !s(&a, k).is_empty() && a[*k] == b[*k])
        || !a["pr"].is_u64()
        || a["pr"] != b["pr"]
        || a["worktree"] == b["worktree"]
    {
        return Err("matching PR alone is not ownership evidence".into());
    }
    for (item, worker, identity) in [(child, job, &a), (parent, owner, &b)] {
        let actual = work_owner::checkout(s(worker, "cwd"));
        if !["repository", "git_common", "worktree", "branch"]
            .iter()
            .all(|k| !s(identity, k).is_empty() && identity[*k] == actual[*k])
            || work_owner::norm(std::path::Path::new(s(&item["scheduler"], "worktree")))
                != actual["worktree"]
            || item["scheduler"]["branch"] != actual["branch"]
            || !s(&actual, "branch").starts_with(&format!("scheduler/todo-{}-", item["id"]))
        {
            return Err("repair checkout no longer matches its registered history".into());
        }
    }
    let mut repaired = job.clone();
    if !repaired["work_wait_history"].is_array() {
        repaired["work_wait_history"] = json!([]);
    }
    repaired["work_wait_history"]
        .as_array_mut()
        .unwrap()
        .push(json!({
        "reason":"finite_parent_cycle_repair", "request":request,
        "wait_on":job["work_wait_on"], "evidence":evidence,
        "queue_wait_reason":job["queue_wait_reason"], "parent_wait_on":parent["wait_on"],
        "parent_finished_at":owner["finished_at"]}));
    repaired["work_wait_on"] = json!(
        job["work_wait_on"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|id| **id != parent["id"])
            .cloned()
            .collect::<Vec<_>>()
    );
    if repaired["work_wait_on"].as_array().unwrap().is_empty() {
        repaired["work_wait_evidence"] = Value::Null;
    }
    // The next ordinary admission rebuilds this diagnostic and all live-owner checks.
    repaired["queue_wait_reason"] = Value::Null;
    Ok(repaired)
}
