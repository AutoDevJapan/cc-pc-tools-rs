//! Explicit finite sub-work. A relation is not a prerequisite or a queue-slot exemption.
use crate::work_owner::{checkout, from_job, norm};
use serde_json::{Value, json};
use std::{collections::BTreeSet, path::Path};

fn s<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
fn item<'a>(db: &'a Value, id: &Value) -> Result<&'a Value, String> {
    db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|i| i["id"] == *id)
        .ok_or_else(|| "subwork ToDo is missing".into())
}
fn same_repo(a: &Value, b: &Value) -> bool {
    ["repository", "git_common"]
        .iter()
        .all(|k| !s(a, k).is_empty() && a[*k] == b[*k])
}
fn linked(todo: &Value, job: &Value) -> bool {
    todo["id"] == job["todo_id"]
        && todo["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&job["id"]))
}
fn root(job: &Value) -> bool {
    s(job, "parent_job_id").is_empty() && !job["subwork"].is_object()
}
/// Callers overwrite this observation from process identity, never trust it from disk.
pub fn mark_live(jobs: &mut [Value], alive: impl Fn(&Value) -> bool) {
    for job in jobs {
        let active = alive(job)
            && job["cancel_requested"] != true
            && ["starting", "running"].contains(&s(job, "status"));
        job["subwork_live"] = json!(active);
    }
}

pub fn register(
    db: &Value,
    child: &Value,
    parent_id: i64,
    parent_job: &str,
    kind: &str,
    jobs: &[Value],
    repair_owner_wait: bool,
) -> Result<Value, String> {
    if !["research", "check", "review"].contains(&kind)
        || child["status"] == "done"
        || child["dispatchable"] != true
        || s(child, "spec").is_empty()
        || child["subwork"].is_object()
        || child["stall_recovery"]["pending"].is_object()
    {
        return Err("subwork requires a new finite research/check/review with a saved spec".into());
    }
    let parent = item(db, &json!(parent_id))?;
    let source = jobs
        .iter()
        .find(|j| s(j, "id") == parent_job)
        .ok_or("parent job is missing")?;
    if !linked(parent, source)
        || !root(source)
        || parent["subwork"].is_object()
        || parent["id"] == child["id"]
        || parent["status"] == "done"
    {
        return Err("parent must be the linked root worker of a different unfinished ToDo".into());
    }
    if jobs.iter().any(|j| {
        j["todo_id"] == child["id"]
            && (["queued", "starting", "running"].contains(&s(j, "status"))
                || j["subwork_live"] == true)
    }) {
        return Err("subwork already has an active or queued worker".into());
    }
    // Narrow migration of a collision produced by work_owner. It grants no live-parent exemption.
    if repair_owner_wait {
        if child["work_owner_job"] != source["id"]
            || !child["wait_on"]
                .as_array()
                .is_some_and(|ids| ids.contains(&parent["id"]))
        {
            return Err("repair requires the exact recorded work-owner wait".into());
        }
    } else if source["subwork_live"] != true {
        return Err("new subwork requires a running parent verified by process identity".into());
    }
    let mut owner = checkout(s(source, "cwd"));
    let recorded = from_job(source);
    if !same_repo(&owner, &recorded) || owner["worktree"] != recorded["worktree"] {
        return Err("parent checkout no longer matches its ownership record".into());
    }
    owner["pr"] = recorded["pr"].clone();
    let relation =
        json!({"parent_todo_id":parent_id,"origin_job_id":parent_job,"kind":kind,"owner":owner});
    let mut proposed = child.clone();
    proposed["subwork"] = relation.clone();
    if repair_owner_wait {
        proposed["wait_on"] = json!(
            child["wait_on"]
                .as_array()
                .into_iter()
                .flatten()
                .filter(|id| **id != parent["id"])
                .cloned()
                .collect::<Vec<_>>()
        );
    }
    resolve(db, &proposed, jobs, false)?;
    Ok(relation)
}

/// Validate durable provenance and current checkout. Missing/exited parent jobs yield a normal root.
/// A resumed parent must own the same ToDo and checkout; another root on that PR still blocks.
pub fn resolve(
    db: &Value,
    child: &Value,
    jobs: &[Value],
    require_checkout: bool,
) -> Result<Option<String>, String> {
    let rel = &child["subwork"];
    if !rel.is_object() || !["research", "check", "review"].contains(&s(rel, "kind")) {
        return Err("invalid subwork relation".into());
    }
    let parent = item(db, &rel["parent_todo_id"])?;
    let owner = &rel["owner"];
    if parent["id"] == child["id"]
        || parent["subwork"].is_object()
        || !parent["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&rel["origin_job_id"]))
        || s(rel, "origin_job_id").is_empty()
        || s(owner, "worktree").is_empty()
    {
        return Err("invalid or cyclic subwork parent".into());
    }
    for row in [parent, child] {
        let project = checkout(db["projects"][s(row, "project")].as_str().unwrap_or(""));
        if !same_repo(owner, &project) {
            return Err(
                "subwork and parent must belong to the same repository and Git common directory"
                    .into(),
            );
        }
    }
    if let Some(pr) = crate::work_owner::pr_number(s(child, "title"))
        && owner["pr"].as_u64().is_some_and(|p| p != pr)
    {
        return Err("subwork names a different PR".into());
    }
    let mut seen = BTreeSet::new();
    let mut pending: Vec<_> = child["wait_on"]
        .as_array()
        .into_iter()
        .flatten()
        .cloned()
        .collect();
    while let Some(id) = pending.pop() {
        if id == parent["id"] || id == child["id"] {
            return Err("independent subwork cannot depend on its parent completion".into());
        }
        if seen.insert(id.to_string())
            && let Ok(row) = item(db, &id)
            && row["status"] != "done"
        {
            pending.extend(row["wait_on"].as_array().into_iter().flatten().cloned());
        }
    }
    let tree = s(&child["scheduler"], "worktree");
    if !tree.is_empty() && Path::new(tree).exists() {
        let actual = checkout(tree);
        if !same_repo(&actual, owner)
            || actual["worktree"] == owner["worktree"]
            || s(&actual, "worktree") != norm(Path::new(tree))
            || actual["branch"] != child["scheduler"]["branch"]
            || !s(&actual, "branch").starts_with(&format!("scheduler/todo-{}-", child["id"]))
        {
            return Err("subwork requires its own scheduler-owned worktree".into());
        }
    } else if require_checkout {
        return Err("subwork checkout is missing".into());
    }
    let mut live_parents = jobs.iter().filter(|j| {
        j["subwork_live"] == true && root(j) && linked(parent, j) && {
            let actual = checkout(s(j, "cwd"));
            same_repo(&actual, owner)
                && actual["worktree"] == owner["worktree"]
                && same_repo(&from_job(j), owner)
                && from_job(j)["worktree"] == owner["worktree"]
                && (!owner["pr"].is_u64() || from_job(j)["pr"] == owner["pr"])
        }
    });
    let current = live_parents.next().map(|j| s(j, "id").to_owned());
    if live_parents.next().is_some() {
        return Err("parent has more than one active root worker".into());
    }
    Ok(current)
}

/// Ephemeral ownership evidence must be rebuilt from the store on every admission attempt.
pub fn apply(
    db: &Value,
    child: &Value,
    jobs: &[Value],
    candidate: &mut Value,
    require_checkout: bool,
) -> Result<(), String> {
    candidate["subwork_validated"] = json!(false);
    candidate["subwork_owner_job_id"] = Value::Null;
    let parent = resolve(db, child, jobs, require_checkout)?;
    candidate["subwork"] = child["subwork"].clone();
    // A generic review title still belongs to its parent's PR ownership scope.
    if !candidate["work_owner"]["pr"].is_u64() {
        candidate["work_owner"]["pr"] = child["subwork"]["owner"]["pr"].clone();
    }
    candidate["subwork_validated"] = json!(true);
    candidate["subwork_owner_job_id"] = json!(parent);
    candidate["parent_job_id"] = json!(parent);
    Ok(())
}

pub fn permits(candidate: &Value, owner: &Value) -> bool {
    if candidate["subwork_validated"] != true {
        return false;
    }
    candidate["subwork_owner_job_id"].is_string()
        && candidate["subwork_owner_job_id"] == owner["id"]
        || (owner["subwork_validated"] == true
            && candidate["subwork"]["parent_todo_id"] == owner["subwork"]["parent_todo_id"]
            && candidate["subwork"]["owner"] == owner["subwork"]["owner"])
}

/// Validate an actual queued job against the authoritative relation and prepared launch.
pub fn admission(child: &Value) -> Value {
    json!({"key":child["scheduler"]["key"],"spec":child["spec"],"needs":child["needs"],
        "subwork":child["subwork"],"worktree":child["scheduler"]["worktree"]})
}

/// Resource admission stays bound to its launch, even after Start transfers the reservation.
/// The worker receipt is independent evidence of that transfer; preparation alone has none.
fn admitted_launch(child: &Value, jobs: &[Value], job: &Value) -> bool {
    if s(job, "launch_key").is_empty() {
        return false;
    }
    let scheduler = &child["scheduler"];
    let mut expected = admission(child);
    expected["key"] = job["launch_key"].clone();
    if scheduler["subwork_admission"] != expected {
        return false;
    }
    // Registration validates a prospective job before publishing it. Once that key has a
    // registered job, another candidate must not borrow the still-unreleased reservation.
    if jobs.iter().any(|registered| {
        registered["launch_key"] == job["launch_key"] && registered["id"] != job["id"]
    }) {
        return false;
    }
    if scheduler["key"] == job["launch_key"] {
        // Registration can precede the ToDo link. A different linked worker cannot use this key.
        return scheduler["job"].is_null() || scheduler["job"] == job["id"];
    }
    scheduler["key"].is_null()
        && child["status"] == "doing"
        && !s(job, "id").is_empty()
        && linked(child, job)
        && scheduler["job"] == job["id"]
        && scheduler["phase"] == "running"
        && child["worker_receipt"]["job"] == job["id"]
        && child["resume_generation"]
            .as_u64()
            .is_some_and(|generation| {
                child["worker_receipt"]["generation"].as_u64() == Some(generation)
            })
}

/// A launch key created by preparation alone is not evidence of resource admission.
pub fn refresh_job(db: &Value, jobs: &[Value], job: &mut Value) -> Result<(), String> {
    let child = item(db, &job["todo_id"])?;
    let ownership = from_job(job);
    if !child["subwork"].is_object()
        || child["subwork"] != job["subwork"]
        || !["todo", "doing"].contains(&s(child, "status"))
        || child["dispatchable"] != true
        || child["spec"] != job["subwork_spec"]
        || child["needs"] != job["assigned_needs"]
        || !admitted_launch(child, jobs, job)
        || !same_repo(&ownership, &child["subwork"]["owner"])
        || s(&ownership, "worktree") != norm(Path::new(s(job, "cwd")))
        || (ownership["pr"].is_u64()
            && child["subwork"]["owner"]["pr"].is_u64()
            && ownership["pr"] != child["subwork"]["owner"]["pr"])
        || crate::todo_wait::waiting(db, child)
        || s(&child["scheduler"], "worktree").is_empty()
        || norm(Path::new(s(job, "cwd"))) != norm(Path::new(s(&child["scheduler"], "worktree")))
    {
        return Err(
            "subwork launch no longer matches the saved ToDo, allocation or workspace".into(),
        );
    }
    apply(db, child, jobs, job, true)
}
