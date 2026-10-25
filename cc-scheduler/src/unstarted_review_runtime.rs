//! Normal inputs and CAS for the fixed unstarted review. No new worker or key.
use crate::{backend::Backend, common::*, engine::Scheduler, policy};
use cc_job_policy::unstarted_review_state as state;
use serde_json::{Value, json};
use std::time::Duration;

pub(crate) struct Evidence {
    pub normal: Value,
    pub health: Value,
    pub authors: Vec<String>,
    pub spec_sha: String,
}

pub(crate) fn writers(jobs: &[Value]) -> Result<Vec<String>> {
    state::observed_authors(jobs).ok_or(Error("ReviewSourceUnavailable"))
}

fn no_worker(item: &Value, jobs: &[Value], ps: &policy::Processes, now: f64) -> bool {
    !jobs.iter().any(|j| {
        j["todo_id"] == 3649
            || arr(&j["related_todo_ids"]).contains(&json!(3649))
            || (norm(s(&j["cwd"])) == norm(s(&item["scheduler"]["worktree"]))
                && (policy::live(j) || policy::alive(j, ps, now)))
    })
}

fn collect<B: Backend>(engine: &mut Scheduler<B>, item: &Value, repo: &str) -> Result<Evidence> {
    if !engine.backend.unstarted_review_workspace(repo, item)? {
        return Err(Error("InputRejected"));
    }
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    if !no_worker(item, &jobs, &ps, engine.backend.clock()) {
        return Err(Error("ResourceWait"));
    }
    let authors = writers(&jobs)?;
    let bytes = cc_job_policy::unstarted_review_spec::read(std::path::Path::new(s(&item["spec"])))?;
    if bytes.is_empty() || bytes.len() > 1024 * 1024 {
        return Err(Error("InputRejected"));
    }
    let spec_sha = cc_job_policy::product_retry::digest(&bytes);
    if spec_sha != state::SPEC_SHA {
        return Err(Error("InputChanged"));
    }
    let normal = engine.backend.unstarted_review_registration()?;
    let health = engine.backend.unstarted_review_inventory()?;
    Ok(Evidence {
        normal,
        health,
        authors,
        spec_sha,
    })
}

pub(crate) fn prepare<B: Backend>(
    engine: &mut Scheduler<B>,
    row: &Value,
    repo: &str,
) -> Result<Option<Evidence>> {
    if !row["scheduler"]["unstarted_review"].is_object() {
        return Ok(None);
    }
    let evidence = collect(engine, row, repo)?;
    if !state::current(
        row,
        &evidence.normal,
        &evidence.health,
        &evidence.authors,
        &evidence.spec_sha,
        engine.backend.clock(),
    ) {
        return Err(Error("UnstartedReviewChanged"));
    }
    Ok(Some(evidence))
}

pub(crate) fn select(
    row: &Value,
    selection: &mut Value,
    evidence: &Evidence,
    spec_sha: &str,
    now: f64,
) -> Result<()> {
    if !state::current(
        row,
        &evidence.normal,
        &evidence.health,
        &evidence.authors,
        spec_sha,
        now,
    ) {
        return Err(Error("UnstartedReviewChanged"));
    }
    selection["model"] = json!("gpt-6-luna");
    selection["effort"] = json!("max");
    selection["model_origin"] = json!("policy");
    selection["effort_origin"] = json!("policy");
    selection["task_type"] = json!("review");
    selection["sandbox"] = json!("read-only");
    selection["network"] = json!(false);
    selection["mcp_profile"] = json!("minimal");
    // Preserve ordinary scheduler's default extra directory in both consumers.
    selection["add_dirs"] = json!(["D:/CC"]);
    selection["unstarted_review"] = row["scheduler"]["unstarted_review"].clone();
    Ok(())
}

/// Inspect or apply, but never launch. The subsequent explicit/normal dispatch
/// keeps priority, global enablement, source guard, resources and final admission.
pub fn run<B: Backend>(engine: &mut Scheduler<B>, expected: Option<&str>) -> Result<Value> {
    let db = engine.todo()?;
    let items: Vec<_> = arr(&db["items"])
        .into_iter()
        .filter(|i| i["id"] == 3649)
        .collect();
    if items.len() != 1 {
        return Err(Error("InputRejected"));
    }
    let item = &items[0];
    let repo = s(&db["projects"]["localai-web-rs"]);
    let evidence = collect(engine, item, repo)?;
    let proposed = state::proposal(
        item,
        &evidence.normal,
        &evidence.health,
        &evidence.authors,
        &evidence.spec_sha,
        state::Checked {
            source_verified: true,
            live_worker_absent: true,
        },
        engine.backend.clock(),
    );
    let proposed = match proposed {
        Ok(p) => p,
        Err(reason) => {
            return Ok(
                json!({"id":3649,"eligible":false,"reason":format!("{reason:?}"),"saved":false,"launched":0}),
            );
        }
    };
    let report = json!({"id":3649,"eligible":true,"state_sha256":proposed.state_sha256(),"saved":false,"launched":0});
    let Some(expected) = expected else {
        return Ok(report);
    };
    if expected != proposed.state_sha256() {
        return Err(Error("InputChanged"));
    }
    let Some(_lock) =
        ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::from_secs(30))?
    else {
        return Ok(json!({"id":3649,"busy":true,"saved":false,"launched":0}));
    };
    // No slow network while holding todo.lock. Recheck jobs/processes before
    // CAS; jobs admission independently reloads everything before execution.
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    if !no_worker(item, &jobs, &ps, engine.backend.clock())
        || !cc_job_policy::quota_recovery::healthy_transition(
            &evidence.health,
            "claude-opus-5-5",
            "gpt-6-luna",
            engine.backend.clock(),
        )
    {
        return Err(Error("ResourceWait"));
    }
    let mut applied = false;
    engine.edit_in_project(&json!(3649), Some(repo), |row| {
        applied = state::apply(row, &proposed);
    })?;
    if !applied {
        return Err(Error("InputChanged"));
    }
    let mut report = report;
    report["saved"] = json!(true);
    Ok(report)
}
