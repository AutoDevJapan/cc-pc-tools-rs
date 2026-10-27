use crate::{Paths, Result};
use cc_job_policy::subwork_continuation as continuation;
use cc_scheduler::{common, native, policy};
use serde_json::{Value, json};

#[allow(clippy::too_many_arguments)]
pub(crate) fn record(
    db: &mut Value,
    paths: &Paths,
    id: i64,
    source_id: &str,
    actor_id: &str,
    resume: u64,
    dependency: u64,
    spec_sha: &str,
) -> Result<Value> {
    // These values identify what the actual caller observed; none is authority by itself.
    if std::env::var("CC_JOB_ID").ok().as_deref() != Some(actor_id) {
        return Err("subwork_continuation_actor_mismatch".into());
    }
    let row = db["items"]
        .as_array()
        .and_then(|items| items.iter().find(|i| i["id"] == id))
        .ok_or("subwork_continuation_missing")?
        .clone();
    if row["resume_generation"] != resume
        || row["dependency_generation"] != dependency
        || cc_job_policy::todo_wait::waiting(db, &row)
    {
        return Err("subwork_continuation_generation_or_dependency".into());
    }
    let mut jobs = common::jobs_from(&paths.jobs, true).map_err(|e| e.0.to_owned())?;
    let ps = native::process_snapshot().map_err(|e| e.0.to_owned())?;
    cc_job_policy::subwork::mark_live(&mut jobs, |j| policy::alive(j, &ps, crate::time_s()));
    let parent = cc_job_policy::subwork::resolve(db, &row, &jobs, true)?;
    if parent.as_deref() != Some(actor_id) {
        return Err("subwork_continuation_parent_not_current".into());
    }
    let parent_row = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|p| p["id"] == row["subwork"]["parent_todo_id"])
        .ok_or("subwork_continuation_parent_missing")?;
    let current_actor = if parent_row["worker_receipt"].is_object() {
        parent_row["worker_receipt"]["job"] == actor_id
            && parent_row["worker_receipt"]["generation"] == parent_row["resume_generation"]
    } else {
        parent_row["scheduler"]["job"] == actor_id
    };
    if !current_actor {
        return Err("subwork_continuation_parent_generation_changed".into());
    }
    if jobs.iter().any(|j| {
        (j["todo_id"] == id
            || row["jobs"]
                .as_array()
                .is_some_and(|ids| ids.contains(&j["id"]))
            || j["related_todo_ids"]
                .as_array()
                .is_some_and(|ids| ids.contains(&json!(id))))
            && (policy::live(j) || policy::alive(j, &ps, crate::time_s()))
    }) {
        return Err("subwork_continuation_not_drained".into());
    }
    let source = jobs
        .iter()
        .find(|j| j["id"] == source_id)
        .ok_or("subwork_continuation_source_missing")?;
    let actor = jobs
        .iter()
        .find(|j| j["id"] == actor_id)
        .ok_or("subwork_continuation_actor_missing")?;
    if !continuation::parent_registered(db, &row, actor) {
        return Err("subwork_continuation_parent_invalid".into());
    }
    let proof = continuation::request(&row, source, actor)
        .filter(|p| p["spec_sha256"] == spec_sha)
        .ok_or("subwork_continuation_contract_rejected")?;
    crate::find(db, id)?["subwork_continuation"] = proof;
    Ok(json!({"id":id,"accepted":true,"launched":false,"source_unchanged":true}))
}
