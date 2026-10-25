//! Normal adapters for one unstarted policy review. No alternate dispatcher.
use crate::{backend::Backend, common::*, engine::Scheduler, policy};
use cc_job_policy::initial_review as rule;
use serde_json::{Value, json};
use std::time::Duration;

pub(crate) struct Evidence {
    pub health: Value,
    pub source: Value,
    pub workspace: Value,
    pub spec_sha: String,
}
fn no_worker(row: &Value, jobs: &[Value], ps: &policy::Processes, now: f64) -> bool {
    !jobs.iter().any(|j| {
        j["todo_id"] == row["id"]
            || arr(&j["related_todo_ids"]).contains(&row["id"])
            || (norm(s(&j["cwd"])) == norm(s(&row["scheduler"]["worktree"]))
                && (policy::live(j) || policy::alive(j, ps, now)))
    })
}
fn collect<B: Backend>(e: &mut Scheduler<B>, row: &Value, repo: &str) -> Result<Evidence> {
    let workspace = e.backend.initial_review_workspace(repo, row)?;
    if !workspace.is_object() {
        return Err(Error("ReviewSourceUnavailable"));
    }
    let jobs = e.backend.jobs()?;
    if !no_worker(row, &jobs, &e.backend.snapshot()?, e.backend.clock()) {
        return Err(Error("ResourceWait"));
    }
    let source = rule::author(&jobs).ok_or(Error("ReviewSourceUnavailable"))?;
    let bytes = e.backend.initial_review_spec(row)?;
    let spec_sha = cc_job_policy::product_retry::digest(&bytes);
    let health = e.backend.initial_review_inventory()?;
    Ok(Evidence {
        health,
        source,
        workspace,
        spec_sha,
    })
}
pub(crate) fn prepare<B: Backend>(
    e: &mut Scheduler<B>,
    row: &Value,
    repo: &str,
) -> Result<Option<Evidence>> {
    if !row["scheduler"]["initial_review"].is_object() {
        return Ok(None);
    }
    let v = collect(e, row, repo)?;
    if !rule::current(
        row,
        &v.source,
        &v.workspace,
        &v.spec_sha,
        &v.health,
        e.backend.clock(),
        None,
    ) {
        return Err(Error("InitialReviewChanged"));
    }
    Ok(Some(v))
}
pub(crate) fn select<B: Backend>(
    e: &mut Scheduler<B>,
    row: &Value,
    selection: &mut Value,
    v: &Evidence,
    spec_sha: &str,
) -> Result<()> {
    if !rule::current(
        row,
        &v.source,
        &v.workspace,
        spec_sha,
        &v.health,
        e.backend.clock(),
        None,
    ) {
        return Err(Error("InitialReviewChanged"));
    }
    rule::select(selection, &row["scheduler"]["initial_review"]);
    Ok(())
}
/// Does not launch. Recompute normal inputs before acquiring the same scheduler
/// gate and compare the complete original row under the ordinary ToDo CAS.
pub fn run<B: Backend>(e: &mut Scheduler<B>, expected: Option<&str>) -> Result<Value> {
    let db = e.todo()?;
    let rows: Vec<_> = arr(&db["items"])
        .into_iter()
        .filter(|r| r["id"] == rule::ID)
        .collect();
    if rows.len() != 1 {
        return Err(Error("InputRejected"));
    }
    let row = &rows[0];
    let repo = s(&db["projects"]["cc-pc-tools-rs"]);
    cc_job_policy::subwork::resolve(&db, row, &[], true).map_err(|_| Error("InputRejected"))?;
    let v = collect(e, row, repo)?;
    let Some(p) = rule::proposal(
        row,
        &v.source,
        &v.workspace,
        &v.spec_sha,
        &v.health,
        true,
        e.backend.clock(),
    ) else {
        return Ok(
            json!({"id":rule::ID,"eligible":false,"saved":false,"launched":0,"reason":"current_inputs_not_eligible"}),
        );
    };
    let mut report = json!({"id":rule::ID,"eligible":true,"state_sha256":p.state_sha256(),"saved":false,"launched":0});
    let Some(expected) = expected else {
        return Ok(report);
    };
    if expected != p.state_sha256() {
        return Err(Error("InputChanged"));
    }
    let Some(_lock) = ByteLock::acquire(&e.root.join("scheduler.lock"), Duration::from_secs(30))?
    else {
        return Ok(json!({"id":rule::ID,"busy":true,"saved":false,"launched":0}));
    };
    let current = e.todo()?;
    cc_job_policy::subwork::resolve(&current, row, &[], true).map_err(|_| Error("InputChanged"))?;
    let jobs = e.backend.jobs()?;
    if !no_worker(row, &jobs, &e.backend.snapshot()?, e.backend.clock())
        || rule::author(&jobs) != Some(v.source.clone())
        || e.backend.initial_review_workspace(repo, row)? != v.workspace
        || cc_job_policy::product_retry::digest(&e.backend.initial_review_spec(row)?) != v.spec_sha
        || !cc_job_policy::quota_recovery::healthy_transition(
            &v.health,
            "claude-opus-5-5",
            "gpt-6-luna",
            e.backend.clock(),
        )
    {
        return Err(Error("InputChanged"));
    }
    let mut saved = false;
    e.edit_in_project(&json!(rule::ID), Some(repo), |r| {
        saved = rule::apply(r, &p);
    })?;
    if !saved {
        return Err(Error("InputChanged"));
    }
    report["saved"] = json!(true);
    Ok(report)
}
