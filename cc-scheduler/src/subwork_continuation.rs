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
    if request.is_null() || continuation::stale_request(item) {
        return Ok(false);
    }
    let source = source.ok_or(Error("InputRejected"))?;
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
    if request["source_kind"] == "current_quota" && selection["model_origin"] != "owner_request" {
        let excluded = arr(&selection["review_excluded_models"]);
        let mut candidates = vec![
            s(&source["model"]).to_owned(),
            s(&selection["model"]).to_owned(),
        ];
        candidates.extend([
            "anthropic/claude-opus-5-5".to_owned(),
            "anthropic/claude-sonnet-5-5".to_owned(),
        ]);
        let choice = candidates
            .into_iter()
            .find_map(|model| {
                if !backend.model_exists(&model)
                    || excluded
                        .iter()
                        .any(|m| cc_job_policy::same_model(s(m), &model))
                {
                    return None;
                }
                let mut target = selection.clone();
                target["model"] = json!(model);
                target["capacity_account_selector"] =
                    if cc_job_policy::quota_recovery::provider(&model) == Some("openai") {
                        json!(
                            if cc_job_policy::quota_recovery::provider(s(&source["model"]))
                                == Some("openai")
                            {
                                cc_job_policy::quota_retry::selector(
                                    source,
                                    &health,
                                    backend.clock(),
                                )?
                            } else {
                                cc_job_policy::quota_retry::available_selector(
                                    source,
                                    &health,
                                    backend.clock(),
                                )?
                            }
                        )
                    } else {
                        Value::Null
                    };
                cc_job_policy::quota_retry::plan(source, &target, &health, backend.clock())
                    .map(|_| target)
            })
            .ok_or(Error("ResourceWait"))?;
        selection["model"] = choice["model"].clone();
        selection["model_origin"] = json!("policy");
        selection["capacity_account_selector"] = choice["capacity_account_selector"].clone();
    } else if cc_job_policy::quota_recovery::provider(s(&selection["model"])) == Some("openai") {
        let selector =
            cc_job_policy::quota_recovery::safe_openai_selector(&health, None, backend.clock())
                .ok_or(Error("ResourceWait"))?;
        selection["capacity_account_selector"] = json!(selector);
    }
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
