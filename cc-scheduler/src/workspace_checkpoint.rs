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
        let owned_branch = |branch: &str| {
            branch.starts_with(&prefix)
                && branch.len() > prefix.len()
                && branch.len() <= 240
                && branch
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b"_/-".contains(&b))
        };
        self.id > 0
            && !self.job.is_empty()
            && self.job.len() <= 240
            && self
                .job
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b))
            && (self.expected_branch == "main" || owned_branch(&self.expected_branch))
            && owned_branch(&self.branch)
            && [40, 64].contains(&self.head.len())
            && self.head.bytes().all(|b| b.is_ascii_hexdigit())
    }
}

pub fn checkpoint<B: Backend>(
    engine: &mut Scheduler<B>,
    runner: &mut dyn Runner,
    request: &Request,
) -> Result<Value> {
    run(engine, runner, request, false)
}

/// The same validation as saving, with no ToDo edit or job operation.
pub fn inspect<B: Backend>(
    engine: &mut Scheduler<B>,
    runner: &mut dyn Runner,
    request: &Request,
) -> Result<Value> {
    run(engine, runner, request, true)
}

fn require(checks: &mut Value, key: &str, matches: bool, reason: &'static str) -> Result<()> {
    checks[key] = json!(matches);
    if matches {
        Ok(())
    } else {
        Err(Error(reason))
    }
}

fn possible_child(child: &Value, parent: u64, born: u64) -> bool {
    child["ppid"].as_u64() == Some(parent)
        && child["created"].as_u64().is_none_or(|birth| birth >= born)
}

fn console_leaf(child: &Value, ps: &policy::Processes, monitor_birth: u64) -> bool {
    let Some(pid) = child["pid"]
        .as_u64()
        .filter(|pid| *pid > 0 && *pid <= u32::MAX as u64)
    else {
        return false;
    };
    let Some(born) = child["created"]
        .as_u64()
        .filter(|born| *born >= monitor_birth)
    else {
        return false;
    };
    s(&child["name"]).eq_ignore_ascii_case("conhost.exe")
        && !ps
            .values()
            .any(|descendant| possible_child(descendant, pid, born))
}

/// A queued supervisor may have a birth-verified, leaf console host. This is
/// plumbing, not execution: a live codex_pid, unknown child or host descendant
/// still blocks, and the caller separately verifies the exact owner wait.
fn waiting_processes_safe(job: &Value, ps: &policy::Processes, at: f64) -> bool {
    for key in ["pid", "codex_pid"] {
        if !job[key].is_null()
            && job[key]
                .as_u64()
                .is_none_or(|pid| pid == 0 || pid > u32::MAX as u64)
        {
            return false;
        }
    }
    if job["codex_pid"]
        .as_u64()
        .is_some_and(|pid| ps.contains_key(&(pid as u32)))
    {
        return false;
    }
    let Some((pid, monitor)) = job["pid"]
        .as_u64()
        .and_then(|pid| ps.get(&(pid as u32)).map(|p| (pid, p)))
    else {
        // Retain the existing recent-registration grace and reboot reservations.
        return !policy::alive(job, ps, at);
    };
    let Some(created) = monitor["created"].as_u64() else {
        return false;
    };
    job["pid_created"].as_u64() == Some(created)
        && ps
            .values()
            .filter(|child| possible_child(child, pid, created))
            .all(|child| console_leaf(child, ps, created))
}

fn run<B: Backend>(
    engine: &mut Scheduler<B>,
    runner: &mut dyn Runner,
    request: &Request,
    check_only: bool,
) -> Result<Value> {
    let mut checks = json!({});
    let mut waiters = 0;
    let result = (|| -> Result<bool> {
        require(&mut checks, "input_valid", request.valid(), "InputRejected")?;
        let _lock =
            ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::from_secs(30))?
                .ok_or(Error("TimeoutError"))?;
        let db = engine.todo()?;
        let item = arr(&db["items"])
            .into_iter()
            .find(|row| row["id"] == request.id)
            .ok_or(Error("CheckpointItemMissing"))?;
        let repeated = item["scheduler"]["workspace_checkpoint"]["request"] == json!(request)
            && item["scheduler"]["branch"] == request.branch;
        require(
            &mut checks,
            "task_state_allowed",
            matches!(s(&item["status"]), "todo" | "doing" | "wait") && item["project"] != "owner",
            "CheckpointTaskState",
        )?;
        require(
            &mut checks,
            "expected_branch_matches",
            repeated || item["scheduler"]["branch"] == request.expected_branch,
            "CheckpointExpectedBranch",
        )?;
        let repo = s(&db["projects"][s(&item["project"])]);
        let cwd = s(&item["scheduler"]["worktree"]);
        require(
            &mut checks,
            "scope_allowed",
            !repo.is_empty()
                && !cwd.is_empty()
                && engine.backend.repair_scope_allowed(repo)
                && engine.backend.repair_scope_allowed(cwd),
            "ClientResource",
        )?;
        let target = resolve(Path::new(cwd))?;
        let base = resolve(Path::new(repo))?;
        // Slow guards and repository discovery do not reserve global admission.
        if git_root(runner, &base)?.is_none() || git_root(runner, &target)?.is_none() {
            return Err(Error("NonGit"));
        }
        same_common(runner, &base, &target)?;
        // Registration and queued -> starting admission use this same byte [0, 1).
        // Keep it through the fresh process/branch/HEAD checks and ToDo CAS.
        let queue = engine
            .root
            .parent()
            .ok_or(Error("CheckpointQueueUnavailable"))?
            .join("codex-jobs/jobs");
        std::fs::create_dir_all(&queue)?;
        let _queue = ByteLock::acquire(&queue.join(".queue.lock"), Duration::from_secs(30))?
            .ok_or(Error("CheckpointQueueBusy"))?;
        let jobs = engine.backend.jobs()?;
        let processes = engine.backend.snapshot()?;
        let at = engine.backend.clock();
        let source = jobs
            .iter()
            .find(|job| job["id"] == request.job)
            .ok_or(Error("CheckpointSourceMissing"))?;
        require(
            &mut checks,
            "source_owned",
            owns_workspace(&item, source)
                && (source["todo_id"] == item["id"]
                    || arr(&source["related_todo_ids"]).contains(&item["id"]))
                && arr(&item["jobs"]).contains(&source["id"]),
            "CheckpointSourceOwnership",
        )?;
        require(
            &mut checks,
            "source_cwd_matches",
            !s(&source["cwd"]).is_empty() && resolve(Path::new(s(&source["cwd"])))? == target,
            "CheckpointSourceCwd",
        )?;
        require(
            &mut checks,
            "source_is_current",
            !(policy::live(source) || policy::alive(source, &processes, at))
                || item["scheduler"]["job"] == source["id"],
            "CheckpointSourceNotCurrent",
        )?;
        if request.expected_branch == "main" {
            // Only the exact currently recorded assignment may leave main. A
            // historical job or user-supplied branch name is not provenance.
            // Keep checking this on repeats, including a completed current job.
            let owner = &source["work_owner"];
            let actual = cc_job_policy::work_owner::checkout(cwd);
            require(
                &mut checks,
                "source_assignment_matches",
                item["scheduler"]["job"] == source["id"]
                    && source["todo_id"] == item["id"]
                    && owner["branch"] == request.expected_branch
                    && !s(&actual["repository"]).is_empty()
                    && owner["repository"] == actual["repository"]
                    && ["worktree", "git_common"].iter().all(|key| {
                        !s(&owner[*key]).is_empty()
                            && cc_job_policy::work_owner::norm(Path::new(s(&owner[*key])))
                                == s(&actual[*key])
                    }),
                "CheckpointSourceAssignment",
            )?;
        }
        for other in jobs.iter().filter(|job| {
            job["id"] != source["id"]
                && (policy::live(job) || policy::alive(job, &processes, at))
                && (job["todo_id"] == item["id"]
                    || (!s(&job["cwd"]).is_empty()
                        && resolve(Path::new(s(&job["cwd"]))).is_ok_and(|p| p == target)))
        }) {
            require(
                &mut checks,
                "competing_worker_inactive",
                other["status"] == "queued"
                    && waiting_processes_safe(other, &processes, at)
                    && [
                        "work_owner_live",
                        "cancel_requested",
                        "queue_restart_pending",
                        "reboot_launch_pending",
                        "queue_unreadable",
                    ]
                    .iter()
                    .all(|key| other[*key].is_null() || other[*key] == false),
                "CheckpointCompetingWorker",
            )?;
            let reason = &other["queue_wait_reason"];
            let evidence = &other["work_wait_evidence"];
            require(
                &mut checks,
                "queued_wait_owner_matches",
                item["scheduler"]["job"] == source["id"]
                    && matches!(s(&source["status"]), "starting" | "running")
                    && policy::alive(source, &processes, at)
                    && reason["code"] == "work_owner"
                    && reason["owner_job_id"] == source["id"]
                    && reason["owner_todo_id"] == item["id"]
                    && (other["todo_id"].is_null() || other["todo_id"] == item["id"])
                    && (evidence.is_null()
                        || (evidence["owner_job_id"] == source["id"]
                            && evidence["owner"]
                                == cc_job_policy::work_owner::owner_evidence(source)
                            && evidence["candidate_identity"] == other["work_owner"])),
                "CheckpointWaitUnverified",
            )?;
            let identity = &other["work_owner"];
            let owner = &source["work_owner"];
            require(
                &mut checks,
                "queued_identity_matches",
                ["repository", "git_common"]
                    .iter()
                    .all(|key| !s(&owner[*key]).is_empty() && identity[*key] == owner[*key])
                    && [
                        s(&other["cwd"]),
                        s(&identity["worktree"]),
                        s(&owner["worktree"]),
                    ]
                    .iter()
                    .all(|path| {
                        !path.is_empty() && resolve(Path::new(path)).is_ok_and(|p| p == target)
                    }),
                "CheckpointWaitIdentity",
            )?;
            waiters += 1;
        }
        checks["unverified_competing_workers_absent"] = json!(true);
        let branch = crate::backend::run_git(runner, &target, &["branch", "--show-current"])?;
        let head = crate::backend::run_git(runner, &target, &["rev-parse", "--verify", "HEAD"])?;
        require(
            &mut checks,
            "actual_branch_matches",
            branch == request.branch,
            "BranchMismatch",
        )?;
        require(
            &mut checks,
            "actual_head_matches",
            head.eq_ignore_ascii_case(&request.head),
            "CheckpointHeadMismatch",
        )?;
        if check_only {
            let fresh = engine.todo()?;
            require(
                &mut checks,
                "task_row_unchanged",
                arr(&fresh["items"]).contains(&item)
                    && fresh["projects"][s(&item["project"])] == repo,
                "CheckpointRowChanged",
            )?;
            return Ok(false);
        }
        // The full row comparison keeps intervening owner decisions, waits and new workers.
        // A subsequent dispatch still checks the actual branch through ensure_workspace().
        let mut saved = false;
        let mut matched = false;
        let row = engine
            .edit_in_project(&item["id"], Some(repo), |row| {
                if *row == item {
                    matched = true;
                }
                if matched && !repeated {
                    row["scheduler"]["branch"] = json!(branch);
                    row["scheduler"]["workspace_checkpoint"] = json!({
                        "request":request,"at":at,"launched":0
                    });
                    saved = true;
                }
            })
            .map_err(|error| {
                if error.0 == "InputRejected" {
                    Error("CheckpointRowChanged")
                } else {
                    error
                }
            })?;
        if !matched
            || row["scheduler"]["branch"] != request.branch
            || row["scheduler"]["workspace_checkpoint"]["request"] != json!(request)
        {
            return Err(Error("CheckpointRowChanged"));
        }
        checks["task_row_unchanged"] = json!(true);
        Ok(saved)
    })();
    let (eligible, reason, saved) = match result {
        Ok(saved) => (true, None, saved),
        Err(error) if !check_only => return Err(error),
        Err(error) => (false, Some(error.0), false),
    };
    Ok(json!({"id":request.id,"eligible":eligible,"reason":reason,
        "saved":saved,"check_only":check_only,"checks":checks,"verified_owner_waiters":waiters,
        "launched":0,"source_files_changed":false}))
}
