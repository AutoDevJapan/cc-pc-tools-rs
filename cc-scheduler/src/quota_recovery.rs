use crate::{backend::Backend, common::*, policy::*};
use serde_json::{json, Value};

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
    let Some(source) = source.filter(|s| s["last_failure_kind"] == "quota") else {
        return Ok(false);
    };
    if selection["model_origin"] == "owner_request" {
        return Ok(false);
    }
    let now = backend.clock();
    if !cc_job_policy::quota_recovery::eligible_source(item, source) {
        return Err(Error("InputRejected"));
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
    selection["task_type"] = json!("review");
    let mut target = selection.clone();
    target["cwd"] = item["scheduler"]["worktree"].clone();
    target["launch_key"] = item["scheduler"]["key"].clone();
    let policy = cc_job_policy::for_sandbox(s(&target["sandbox"]));
    target["approval_policy"] = json!(policy.approval);
    target["approvals_reviewer"] = json!(policy.reviewer);
    let health = backend.quota_recovery_inventory()?;
    let proof =
        cc_job_policy::quota_recovery::receipt(item, source, &target, &health, backend.clock())
            .ok_or(Error("ResourceWait"))?;
    if proof["spec_sha256"] != spec_sha256 {
        return Err(Error("InputRejected"));
    }
    selection["quota_recovery"] = proof;
    Ok(true)
}
