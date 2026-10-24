//! Repair one stale original checkpoint from a verified successful worker transition.
//! No Git writes, launch, registration, status, dependency or stop changes occur here.
use crate::{backend::Backend, common::*, engine::Scheduler, policy, targeted_dispatch};
use cc_job_policy::product_retry::digest;
use serde_json::{json, Value};
use std::{fs::File, io::Read, path::Path, time::Duration};

fn path_digest(path: &str) -> String {
    digest(
        path.replace('\\', "/")
            .trim_end_matches('/')
            .to_lowercase()
            .as_bytes(),
    )
}

pub(crate) fn qualified_proof(item: &Value, proof: &Value) -> bool {
    qualified_proof_with_branches(
        item,
        proof,
        "5dbb53a461175821c3fd4948e143717b50c9972649795c8abe670feeb7bb518f",
        "9b56dd38ed6b918901c96f4f37ccd02629e8dcb9d5fcb827a7804bfcf3f139e3",
    )
}

fn qualified_proof_with_branches(item: &Value, proof: &Value, before: &str, after: &str) -> bool {
    item["id"] == 1626
        && proof["domain"] == "original-worker-checkpoint-v1"
        && proof["todo_id"] == item["id"]
        && proof["source_job"] == "20261020-170947-original-product-retry-1626"
        && proof["source_job"] == item["worker_receipt"]["job"]
        && arr(&item["jobs"]).contains(&proof["source_job"])
        && proof["source_session"] == "01a134e8-aac0-7942-8996-a0fc69357a3d"
        && proof["transition_call_sha256"]
            == "a056c317bdd2ffed329931a23bf5e163ecacff46e2d037d8f8fe097af8f43d5f"
        && proof["transition_result_sha256"]
            == "1ec01c00fce7a03c6a6b76a6452872c8a29ddea43cca1377bcafd605b7848a88"
        && !s(&item["scheduler"]["worktree"]).is_empty()
        && proof["worktree_sha256"] == path_digest(s(&item["scheduler"]["worktree"]))
        && proof["before_branch_sha256"] == digest(s(&item["scheduler"]["branch"]).as_bytes())
        && proof["before_branch_sha256"] == before
        && proof["after_branch_sha256"] == digest(s(&proof["after_branch"]).as_bytes())
        && proof["after_branch_sha256"] == after
        && s(&proof["head"]).len() == 40
        && s(&proof["head"]).bytes().all(|b| b.is_ascii_hexdigit())
}

pub fn read_request(path: &Path) -> Result<Value> {
    let mut bytes = Vec::new();
    File::open(path)?.take(8193).read_to_end(&mut bytes)?;
    if bytes.len() > 8192 {
        return Err(Error("InputRejected"));
    }
    let value: Value = serde_json::from_slice(&bytes).map_err(|_| Error("InputRejected"))?;
    if value["todo_id"] != 1626
        || value["actor_todo_id"] != 3650
        || s(&value["actor_job"]).is_empty()
    {
        return Err(Error("InputRejected"));
    }
    Ok(value)
}

pub fn run<B: Backend>(
    engine: &mut Scheduler<B>,
    request: &Value,
    expected: Option<&str>,
) -> Result<Value> {
    if request["todo_id"] != 1626
        || request["actor_todo_id"] != 3650
        || s(&request["actor_job"]).is_empty()
    {
        return Err(Error("InputRejected"));
    }
    let Some(_lock) =
        ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::from_secs(30))?
    else {
        return Ok(json!({"id":1626,"busy":true,"saved":false,"launched":0}));
    };
    let db = engine.todo()?;
    let item = arr(&db["items"])
        .into_iter()
        .find(|i| i["id"] == 1626)
        .ok_or(Error("InputRejected"))?;
    let actor = arr(&db["items"])
        .into_iter()
        .find(|i| i["id"] == 3650)
        .ok_or(Error("InputRejected"))?;
    let root = s(&db["projects"][s(&item["project"])]);
    let proof = engine.backend.current_workspace_checkpoint(&item, root);
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    let at = engine.backend.clock();
    let cwd = s(&item["scheduler"]["worktree"]);
    let active = jobs.iter().any(|j| {
        (j["todo_id"] == item["id"]
            || arr(&item["jobs"]).contains(&j["id"])
            || (!cwd.is_empty() && norm(s(&j["cwd"])) == norm(cwd)))
            && (policy::live(j) || policy::alive(j, &ps, at))
    });
    let checks = json!({"original_status":item["status"]=="todo", "ordinary":item["subwork"].is_null(),
        "actor":actor["status"]=="doing" && actor["scheduler"]["phase"]=="running" && actor["scheduler"]["job"]==request["actor_job"],
        "no_stop":!targeted_dispatch::held(&item) && item["stopped"]!=true,
        "dependencies":!cc_job_policy::todo_wait::waiting(&db,&item), "no_active_worker":!active,
        "source":proof.is_some(), "history":item["workspace_checkpoint_history"].is_null()||item["workspace_checkpoint_history"].is_array()});
    let eligible = checks.as_object().unwrap().values().all(|v| *v == true);
    let state = digest(
        &serde_json::to_vec(
            &json!({"item":item,"proof":proof,"actor_job":actor["scheduler"]["job"]}),
        )
        .unwrap(),
    );
    let mut report = json!({"id":1626,"eligible":eligible,"checks":checks,"state_sha256":state,"saved":false,"launched":0});
    let Some(expected) = expected else {
        return Ok(report);
    };
    if !state.eq_ignore_ascii_case(expected) {
        return Err(Error("InputChanged"));
    }
    if !eligible {
        return Ok(report);
    }
    let proof = proof.unwrap();
    if engine
        .backend
        .current_workspace_checkpoint(&item, root)
        .as_ref()
        != Some(&proof)
    {
        return Err(Error("InputChanged"));
    }
    let mut saved = false;
    engine.edit_in_project(&json!(1626), Some(root), |row| {
        if *row!=item { return }
        if row["workspace_checkpoint_history"].is_null() {row["workspace_checkpoint_history"]=json!([])}
        let receipt = json!({"before_branch":row["scheduler"]["branch"],"proof":proof,"actor_todo_id":3650,"actor_job":request["actor_job"],"recorded_at":at});
        row["workspace_checkpoint_history"].as_array_mut().unwrap().push(receipt);
        row["scheduler"]["branch"]=proof["after_branch"].clone();
        saved=true;
    })?;
    if !saved {
        return Err(Error("InputChanged"));
    }
    report["saved"] = json!(true);
    Ok(report)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn checkpoint_proof_binds_original_worker_and_each_current_value() {
        let job = "20261020-170947-original-product-retry-1626";
        let item = json!({"id":1626,"worker_receipt":{"job":job},"jobs":[job],"scheduler":{"branch":"before","worktree":"D:/original"}});
        let proof = json!({"domain":"original-worker-checkpoint-v1","todo_id":1626,"source_job":job,"source_session":"01a134e8-aac0-7942-8996-a0fc69357a3d",
            "transition_call_sha256":"a056c317bdd2ffed329931a23bf5e163ecacff46e2d037d8f8fe097af8f43d5f",
            "transition_result_sha256":"1ec01c00fce7a03c6a6b76a6452872c8a29ddea43cca1377bcafd605b7848a88",
            "worktree_sha256":path_digest("D:/original"),"before_branch_sha256":digest(b"before"),"after_branch_sha256":digest(b"after"),"after_branch":"after","head":"a".repeat(40)});
        assert!(qualified_proof_with_branches(
            &item,
            &proof,
            &digest(b"before"),
            &digest(b"after")
        ));
        for key in [
            "domain",
            "todo_id",
            "source_job",
            "source_session",
            "transition_call_sha256",
            "transition_result_sha256",
            "worktree_sha256",
            "before_branch_sha256",
            "after_branch_sha256",
            "after_branch",
            "head",
        ] {
            let mut changed = proof.clone();
            changed[key] = Value::Null;
            assert!(
                !qualified_proof_with_branches(
                    &item,
                    &changed,
                    &digest(b"before"),
                    &digest(b"after")
                ),
                "{key}"
            );
        }
        for changed in [
            json!({"id":1626}),
            json!({"id":3277,"jobs":[job],"worker_receipt":{"job":job}}),
        ] {
            assert!(!qualified_proof_with_branches(
                &changed,
                &proof,
                &digest(b"before"),
                &digest(b"after")
            ));
        }
    }
}
