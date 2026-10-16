//! Explicitly checkpoint an owned task branch without switching files or admitting work.
use crate::{
    backend::Backend,
    common::*,
    engine::Scheduler,
    policy,
    preparation::{git_root, owns_workspace, same_common},
};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{path::Path, time::Duration};

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub id: u64,
    pub job: String,
    pub expected_branch: String,
    pub branch: String,
    pub head: String,
}

impl Request {
    fn valid(&self) -> bool {
        let prefix = format!("scheduler/todo-{}-", self.id);
        self.id > 0
            && !self.job.is_empty()
            && self.job.len() <= 240
            && self
                .job
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b))
            && [&self.expected_branch, &self.branch].iter().all(|branch| {
                branch.starts_with(&prefix)
                    && branch.len() > prefix.len()
                    && branch.len() <= 240
                    && branch
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b"_/-".contains(&b))
            })
            && [40, 64].contains(&self.head.len())
            && self.head.bytes().all(|b| b.is_ascii_hexdigit())
    }
}

pub fn checkpoint<B: Backend>(
    engine: &mut Scheduler<B>,
    runner: &mut dyn Runner,
    request: &Request,
) -> Result<Value> {
    if !request.valid() {
        return Err(Error("InputRejected"));
    }
    let _lock = ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::from_secs(30))?
        .ok_or(Error("TimeoutError"))?;
    let db = engine.todo()?;
    let item = arr(&db["items"])
        .into_iter()
        .find(|row| row["id"] == request.id)
        .ok_or(Error("InputRejected"))?;
    let repeated = item["scheduler"]["workspace_checkpoint"]["request"] == json!(request)
        && item["scheduler"]["branch"] == request.branch;
    if !matches!(s(&item["status"]), "todo" | "doing" | "wait")
        || item["project"] == "owner"
        || (!repeated && item["scheduler"]["branch"] != request.expected_branch)
    {
        return Err(Error("InputRejected"));
    }
    let repo = s(&db["projects"][s(&item["project"])]);
    let cwd = s(&item["scheduler"]["worktree"]);
    if repo.is_empty()
        || cwd.is_empty()
        || !engine.backend.repair_scope_allowed(repo)
        || !engine.backend.repair_scope_allowed(cwd)
    {
        return Err(Error("ClientResource"));
    }
    let jobs = engine.backend.jobs()?;
    let processes = engine.backend.snapshot()?;
    let at = engine.backend.clock();
    let source = jobs
        .iter()
        .find(|job| job["id"] == request.job)
        .filter(|job| owns_workspace(&item, job))
        .filter(|job| {
            job["todo_id"] == item["id"] || arr(&job["related_todo_ids"]).contains(&item["id"])
        })
        .filter(|job| arr(&item["jobs"]).contains(&job["id"]))
        .ok_or(Error("InputRejected"))?;
    let target = resolve(Path::new(cwd))?;
    if s(&source["cwd"]).is_empty()
        || resolve(Path::new(s(&source["cwd"])))? != target
        || jobs.iter().any(|job| {
            job["id"] != source["id"]
                && (policy::live(job) || policy::alive(job, &processes, at))
                && (job["todo_id"] == item["id"] || norm(s(&job["cwd"])) == norm(cwd))
        })
        || ((policy::live(source) || policy::alive(source, &processes, at))
            && item["scheduler"]["job"] != source["id"])
    {
        return Err(Error("InputRejected"));
    }
    let base = resolve(Path::new(repo))?;
    if git_root(runner, &base)?.is_none() || git_root(runner, &target)?.is_none() {
        return Err(Error("NonGit"));
    }
    same_common(runner, &base, &target)?;
    let branch = crate::backend::run_git(runner, &target, &["branch", "--show-current"])?;
    let head = crate::backend::run_git(runner, &target, &["rev-parse", "--verify", "HEAD"])?;
    if branch != request.branch || !head.eq_ignore_ascii_case(&request.head) {
        return Err(Error("BranchMismatch"));
    }
    // The full row comparison keeps intervening owner decisions, waits and new workers.
    // A subsequent dispatch still checks the actual branch through ensure_workspace().
    let mut saved = false;
    let row = engine.edit_in_project(&item["id"], Some(repo), |row| {
        if *row == item && !repeated {
            row["scheduler"]["branch"] = json!(branch);
            row["scheduler"]["workspace_checkpoint"] = json!({
                "request":request,"at":at,"launched":0
            });
            saved = true;
        }
    })?;
    if row["scheduler"]["branch"] != request.branch
        || row["scheduler"]["workspace_checkpoint"]["request"] != json!(request)
    {
        return Err(Error("InputRejected"));
    }
    Ok(json!({"id":request.id,"saved":saved,"launched":0,"source_files_changed":false}))
}
