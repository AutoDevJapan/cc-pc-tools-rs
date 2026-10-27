use crate::{backend::Backend, common::*, policy::*};
use cc_job_policy::subwork_continuation as continuation;
use serde_json::{Value, json};

pub(crate) fn requested(
    backend: &mut impl Backend,
    item: &Value,
    source: Option<&Value>,
    jobs: &[Value],
) -> Result<bool> {
    let request = &item["subwork_continuation"];
    if request.is_null() {
        return Ok(false);
    }
    let source = source.ok_or(Error("InputRejected"))?;
    if continuation::consumed(item, source) {
        return Ok(false);
    }
    let actor = jobs
        .iter()
        .find(|j| j["id"] == request["actor_job"])
        .ok_or(Error("InputRejected"))?;
    if !continuation::current(request, item, source, actor)
        || epoch(&source["finished_at"]) > backend.clock() + 1.
    {
        return Err(Error("InputRejected"));
    }
    let ps = backend.snapshot()?;
    if jobs.iter().any(|j| {
        (j["todo_id"] == item["id"]
            || arr(&item["jobs"]).contains(&j["id"])
            || arr(&j["related_todo_ids"]).contains(&item["id"]))
            && (live(j) || alive(j, &ps, backend.clock()))
    }) {
        return Err(Error("ResourceWait"));
    }
    Ok(true)
}
pub(crate) fn finalize(
    backend: &mut impl Backend,
    db: &Value,
    item: &Value,
    source: Option<&Value>,
    jobs: &[Value],
    selection: &mut Value,
) -> Result<bool> {
    selection
        .as_object_mut()
        .unwrap()
        .remove("subwork_continuation");
    if !requested(backend, item, source, jobs)? {
        return Ok(false);
    }
    let source = source.ok_or(Error("InputRejected"))?;
    let request = &item["subwork_continuation"];
    let actor = jobs
        .iter()
        .find(|j| j["id"] == request["actor_job"])
        .ok_or(Error("InputRejected"))?;
    if !continuation::parent_registered(db, item, actor) {
        return Err(Error("InputRejected"));
    }
    let health = backend.quota_recovery_inventory()?;
    let mut target = selection.clone();
    target["cwd"] = item["scheduler"]["worktree"].clone();
    target["launch_key"] = item["scheduler"]["key"].clone();
    let policy = cc_job_policy::for_sandbox(s(&target["sandbox"]));
    target["approval_policy"] = json!(policy.approval);
    target["approvals_reviewer"] = json!(policy.reviewer);
    let proof = continuation::receipt(
        request,
        item,
        source,
        actor,
        &target,
        &health,
        backend.clock(),
    )
    .ok_or(Error("ResourceWait"))?;
    if request["spec_sha256"] != selection["finite_review_spec_sha256"] {
        return Err(Error("InputRejected"));
    }
    selection["subwork_continuation"] = proof;
    Ok(true)
}
