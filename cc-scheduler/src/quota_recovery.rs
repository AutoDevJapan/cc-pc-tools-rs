use crate::{backend::Backend, common::*, policy::*};
use serde_json::{Value, json};

pub(crate) fn prepare(
    backend: &mut impl Backend,
    item: &Value,
    source: Option<&Value>,
    jobs: &[Value],
    selection: &mut Value,
    spec_sha256: &str,
) -> Result<bool> {
    // A cached capsule is not authority for a new launch generation.
    selection.as_object_mut().unwrap().remove("quota_recovery");
    selection
        .as_object_mut()
        .unwrap()
        .remove("model_recovery_from");
    // A completed new worker may retain historical failure metadata. Its own
    // successful result is an ordinary continuation, not another quota source.
    if source.is_some_and(|source| source["status"]=="succeeded"
        && cc_job_policy::subwork_continuation::consumed(item,source)) {return Ok(false);}
    if crate::subwork_continuation::requested(backend,item,source,jobs)? {
        let source=source.ok_or(Error("InputRejected"))?;
        for field in ["sandbox","mcp_profile","network","add_dirs"] {selection[field]=source[field].clone();}
        selection["task_type"]=item["subwork"]["kind"].clone();
        return Ok(false);
    }
    let Some(source) = source.filter(|s| cc_job_policy::quota_recovery::quota_source(s)) else {
        return Ok(false);
    };
    if selection["model_origin"] == "owner_request" {
        return Ok(false);
    }
    // Legacy research has no proof of the originally executed body. Let its
    // next ordinary attempt bind the real bytes; never backfill the old job.
    if source["last_failure_kind"] == "transient"
        && source["subwork"]["kind"] == "research"
        && source["finite_subwork"].is_null()
    {
        return Ok(false);
    }
    let now = backend.clock();
    if !cc_job_policy::quota_recovery::eligible_source(item, source) {
        return Err(Error("InputRejected"));
    }
    if cc_job_policy::quota_retry::tracked(source) && cc_job_policy::quota_retry::terminal_reason(source).is_some() {
        return Err(Error("QuotaRetryEnded"));
    }
    let ps = backend.snapshot()?;
    if alive(source, &ps, now)
        || jobs.iter().any(|j| {
            j["id"] != source["id"]
                && (j["todo_id"] == item["id"] || arr(&item["jobs"]).contains(&j["id"]))
                && (live(j) || alive(j, &ps, now))
        })
    {
        return Err(Error("ResourceWait"));
    }
    // Use the original execution scope, allowing only identical explicit inputs.
    let options = &item["scheduler"]["console_request"]["options"];
    for k in ["sandbox", "mcp_profile", "network", "add_dirs"] {
        selection[k] = item
            .get(k)
            .filter(|v| !v.is_null())
            .or_else(|| options.get(k).filter(|v| !v.is_null()))
            .unwrap_or(&source[k])
            .clone();
    }
    if source["subwork"]["kind"] == "research" {
        selection["task_type"] = json!("research");
    } else {
        selection["task_type"] = json!("review");
    }
    let health = backend.quota_recovery_inventory()?;
    if cc_job_policy::quota_recovery::provider(s(&source["model"])) == Some("openai") {
        if let Some(selector) = cc_job_policy::quota_retry::selector(source, &health, backend.clock()) {
            selection["model"] = source["model"].clone();
            selection["capacity_account_selector"] = json!(selector);
        } else {
            let excluded = arr(&selection["review_excluded_models"]);
            let next = ["anthropic/claude-opus-5-5", "anthropic/claude-sonnet-5-5"].into_iter().find(|model| {
                let mut target = selection.clone(); target["model"] = json!(model);
                !excluded.iter().any(|author| cc_job_policy::same_model(s(author), model))
                    && cc_job_policy::quota_recovery::transition(source, &target, &health, backend.clock())
                    && backend.model_exists(model)
            }).ok_or(Error("ResourceWait"))?;
            selection["model"] = json!(next);
            selection.as_object_mut().unwrap().remove("capacity_account_selector");
        }
        selection["model_origin"] = json!("policy");
    } else if cc_job_policy::quota_retry::tracked(source)
        && cc_job_policy::quota_recovery::provider(s(&source["model"])) != Some("xai")
        && cc_job_policy::quota_recovery::provider(s(&selection["model"])) == Some("openai")
    {
        let selector = cc_job_policy::quota_retry::available_selector(source,&health, backend.clock())
            .ok_or(Error("ResourceWait"))?;
        selection["capacity_account_selector"] = json!(selector);
    }
    if cc_job_policy::quota_recovery::provider(s(&source["model"])) == Some("xai") {
        let excluded = arr(&selection["review_excluded_models"]);
        let next = [
            "anthropic/claude-opus-5-5",
            "anthropic/claude-sonnet-5-5",
            "gpt-6.1-sol",
            "gpt-6-astra",
            "gpt-6-luna",
        ]
        .into_iter()
        .find_map(|model| {
            if excluded.iter().any(|author|cc_job_policy::same_model(s(author),model)) || !backend.model_exists(model) {return None;}
            let mut target=selection.clone();target["model"]=json!(model);
            let selector=if cc_job_policy::quota_retry::tracked(source) && cc_job_policy::quota_recovery::provider(model)==Some("openai") {
                Some(cc_job_policy::quota_retry::available_selector(source,&health,backend.clock())?)
            }else{None};
            target["capacity_account_selector"]=json!(selector);
            cc_job_policy::quota_recovery::transition(source,&target,&health,backend.clock()).then_some((model,selector))
        })
        .ok_or(Error("ResourceWait"))?;
        selection["model"] = json!(next.0);
        selection["capacity_account_selector"] = json!(next.1);
        selection["model_recovery_from"] = source["model"].clone();
    }
    let mut target = selection.clone();
    target["cwd"] = item["scheduler"]["worktree"].clone();
    target["launch_key"] = item["scheduler"]["key"].clone();
    let policy = cc_job_policy::for_sandbox(s(&target["sandbox"]));
    target["approval_policy"] = json!(policy.approval);
    target["approvals_reviewer"] = json!(policy.reviewer);
    let proof =
        cc_job_policy::quota_recovery::receipt(item, source, &target, &health, backend.clock())
            .ok_or(Error("ResourceWait"))?;
    if proof["spec_sha256"] != spec_sha256 {
        return Err(Error("InputRejected"));
    }
    selection["quota_recovery"] = proof;
    Ok(true)
}
