//! Restore an existing repair task's registration after an explicit resume request.
//! This records intent; the ordinary dispatcher still decides whether it can run.
use crate::{backend::Backend, common::*, engine::Scheduler, policy, targeted_dispatch};
use serde_json::{json, Value};
use std::{fs::File, io::Read, path::Path, time::Duration};

pub fn read_request(path: &Path, id: i64) -> Result<Value> {
    let mut bytes = Vec::new();
    File::open(path)?.take(8193).read_to_end(&mut bytes)?;
    if bytes.len() > 8192 {
        return Err(Error("InputRejected"));
    }
    let request: Value = serde_json::from_slice(&bytes).map_err(|_| Error("InputRejected"))?;
    if request["todo_id"] != id
        || request["intent"] != "resume_registration_only"
        || s(&request["request"]).trim().is_empty()
        || s(&request["source_ref"]).trim().is_empty()
    {
        return Err(Error("InputRejected"));
    }
    Ok(request)
}

fn inspect(
    db: &Value,
    item: &Value,
    request: &Value,
    jobs: &[Value],
    ps: &policy::Processes,
    at: f64,
    source_allowed: bool,
    current_request: bool,
) -> Value {
    let cwd = s(&item["scheduler"]["worktree"]);
    let active = jobs.iter().any(|job| {
        (job["todo_id"] == item["id"]
            || arr(&item["jobs"]).contains(&job["id"])
            || arr(&job["related_todo_ids"]).contains(&item["id"])
            || (!cwd.is_empty() && norm(s(&job["cwd"])) == norm(cwd)))
            && (policy::live(job) || policy::alive(job, ps, at))
    });
    let checks = json!({
        "repair_project":(["cc-pc-tools-rs","pc-capacity-audit","localai-web-rs"].contains(&s(&item["project"])) || current_request),
        "status":item["status"] == "todo",
        "paused_registration":item["dispatchable"] == false,
        "ordinary":item["subwork"].is_null(),
        "spec_recorded":!s(&item["spec"]).is_empty(),
        "source_scope":source_allowed,
        "no_stop":!targeted_dispatch::held(item) && item["stopped"] != true,
        "dependency_ready":!cc_job_policy::todo_wait::waiting(db,item),
        "unlinked":!truth(&item["scheduler"]["job"]) && !truth(&item["scheduler"]["key"]),
        "no_active_worker":!active,
        "receipt_history":item["registration_resume_history"].is_null() || item["registration_resume_history"].is_array()
    });
    let failed: Vec<_> = checks
        .as_object()
        .unwrap()
        .iter()
        .filter(|(_, value)| **value != true)
        .map(|(key, _)| key.clone())
        .collect();
    let digest = cc_job_policy::product_retry::digest(
        &serde_json::to_vec(&json!({"item":item,"request":request})).unwrap(),
    );
    json!({"id":item["id"],"eligible":failed.is_empty(),"checks":checks,"failed":failed,
        "state_sha256":digest,"saved":false,"launched":0})
}

pub fn run<B: Backend>(
    engine: &mut Scheduler<B>,
    id: i64,
    request: &Value,
    expected: Option<&str>,
) -> Result<Value> {
    if id <= 0
        || request["todo_id"] != id
        || request["intent"] != "resume_registration_only"
        || s(&request["request"]).trim().is_empty()
        || s(&request["source_ref"]).trim().is_empty()
    {
        return Err(Error("InputRejected"));
    }
    let Some(_lock) =
        ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::from_secs(30))?
    else {
        return Ok(json!({"id":id,"busy":true,"saved":false,"launched":0}));
    };
    let db = engine.todo()?;
    let item = arr(&db["items"])
        .into_iter()
        .find(|row| row["id"] == id)
        .ok_or(Error("InputRejected"))?;
    let repo = s(&db["projects"][s(&item["project"])]);
    let cwd = s(&item["scheduler"]["worktree"]);
    let current_request = [1626, 3277, 3549, 3623].contains(&id)
        && engine.backend.current_product_allowed(&item, repo);
    let allowed = engine.backend.repair_scope_allowed(repo)
        && (cwd.is_empty() || engine.backend.repair_scope_allowed(cwd));
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    let mut report = inspect(
        &db,
        &item,
        request,
        &jobs,
        &ps,
        engine.backend.clock(),
        allowed,
        current_request,
    );
    let Some(expected) = expected else {
        return Ok(report);
    };
    if expected.len() != 64
        || !expected.bytes().all(|b| b.is_ascii_hexdigit())
        || !s(&report["state_sha256"]).eq_ignore_ascii_case(expected)
    {
        return Err(Error("InputChanged"));
    }
    if report["eligible"] != true {
        return Ok(report);
    }
    let receipt = json!({"source_ref":request["source_ref"],
        "request_sha256":cc_job_policy::product_retry::digest(&serde_json::to_vec(request).unwrap()),
        "state_sha256":report["state_sha256"],"recorded_at":engine.backend.clock()});
    let mut applied = false;
    engine.edit_in_project(&json!(id), Some(repo), |row| {
        if *row != item {
            return;
        }
        row["dispatchable"] = json!(true);
        if row["registration_resume_history"].is_null() {
            row["registration_resume_history"] = json!([]);
        }
        row["registration_resume_history"]
            .as_array_mut()
            .unwrap()
            .push(receipt);
        applied = true;
    })?;
    if !applied {
        return Err(Error("InputChanged"));
    }
    report["saved"] = json!(true);
    Ok(report)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn row() -> Value {
        json!({"id":2467,"project":"cc-pc-tools-rs","status":"todo",
        "dispatchable":false,"spec":"original.md","jobs":[],"scheduler":{"phase":"retry"}})
    }
    fn check(item: &Value, jobs: &[Value], allowed: bool) -> Value {
        inspect(
            &json!({"items":[item]}),
            item,
            &json!({"todo_id":2467}),
            jobs,
            &policy::Processes::new(),
            100.,
            allowed,
            false,
        )
    }
    #[test]
    fn registration_resume_accepts_only_an_idle_recorded_repair_task() {
        assert_eq!(check(&row(), &[], true)["eligible"], true);
        assert_eq!(check(&row(), &[], false)["checks"]["source_scope"], false);
        for (field, value) in [
            ("status", json!("recovery_unverified")),
            ("status", json!("done")),
            ("status", json!("doing")),
            ("dispatchable", json!(true)),
            ("project", json!("unknown")),
            ("spec", json!("")),
            ("stopped", json!(true)),
            ("owner_stopped", json!(true)),
            ("owner_question_wait", json!({"id":1})),
            ("wait_ready", json!({"condition":"retry-time"})),
            ("subwork", json!({"parent":1})),
            ("registration_resume_history", json!("invalid")),
        ] {
            let mut item = row();
            item[field] = value;
            assert_eq!(check(&item, &[], true)["eligible"], false, "{field}");
        }
    }
    #[test]
    fn registration_resume_preserves_dependency_and_launch_reservations() {
        let mut item = row();
        item["wait_on"] = json!([9]);
        let db = json!({"items":[item,{"id":9,"status":"todo"}]});
        assert_eq!(
            inspect(
                &db,
                &item,
                &Value::Null,
                &[],
                &policy::Processes::new(),
                100.,
                true,
                false,
            )["checks"]["dependency_ready"],
            false
        );
        for key in ["job", "key"] {
            let mut item = row();
            item["scheduler"][key] = json!("existing");
            assert_eq!(check(&item, &[], true)["checks"]["unlinked"], false);
        }
    }
    #[test]
    fn registration_resume_refuses_a_queued_or_still_alive_owner() {
        let job = json!({"id":"queued","todo_id":2467,"status":"queued"});
        assert_eq!(
            check(&row(), &[job], true)["checks"]["no_active_worker"],
            false
        );
        let mut item = row();
        item["scheduler"]["worktree"] = json!("C:/original");
        let job = json!({"id":"another","todo_id":1,"status":"running","cwd":"C:/original"});
        assert_eq!(check(&item, &[job], true)["eligible"], false);
    }
    #[test]
    fn registration_resume_digest_binds_the_request_and_outputs_no_body() {
        let item = row();
        let db = json!({"items":[item]});
        let ps = policy::Processes::new();
        let first = inspect(
            &db,
            &item,
            &json!({"request":"private one"}),
            &[],
            &ps,
            100.,
            true,
            false,
        );
        let other = inspect(
            &db,
            &item,
            &json!({"request":"private two"}),
            &[],
            &ps,
            100.,
            true,
            false,
        );
        assert_ne!(first["state_sha256"], other["state_sha256"]);
        assert!(!first.to_string().contains("original.md"));
        assert!(!first.to_string().contains("private one"));
    }

    #[test]
    fn current_request_extends_only_project_eligibility_and_keeps_every_other_gate() {
        let mut item = row();
        item["id"] = json!(1626);
        item["project"] = json!("original-product");
        let inspect_request = |row: &Value, source, current| {
            inspect(
                &json!({"items":[row]}),
                row,
                &Value::Null,
                &[],
                &policy::Processes::new(),
                100.,
                source,
                current,
            )
        };
        assert_eq!(inspect_request(&item, true, false)["eligible"], false);
        assert_eq!(inspect_request(&item, true, true)["eligible"], true);
        assert_eq!(inspect_request(&item, false, true)["eligible"], false);
        item["owner_stopped"] = json!(true);
        assert_eq!(inspect_request(&item, true, true)["eligible"], false);
    }
}
