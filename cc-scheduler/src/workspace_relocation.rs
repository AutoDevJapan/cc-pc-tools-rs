//! Explicit, provenance-checked relocation of one formerly shared repair task.
use crate::{backend::Backend, common::*, engine::Scheduler};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    fs::{File, OpenOptions},
    io::Read,
    os::windows::fs::OpenOptionsExt,
    path::{Path, PathBuf},
    time::Duration,
};

fn digest(value: &Value) -> String {
    cc_job_policy::product_retry::digest(&serde_json::to_vec(value).unwrap())
}
fn sha(value: &str, len: usize) -> bool {
    value.len() == len && value.bytes().all(|b| b.is_ascii_hexdigit())
}
fn terminal(job: &Value) -> bool {
    matches!(
        s(&job["status"]),
        "succeeded" | "failed" | "lost" | "cancelled" | "canceled"
    ) && [
        "work_owner_live",
        "queue_unreadable",
        "cancel_requested",
        "queue_restart_pending",
        "reboot_launch_pending",
    ]
    .iter()
    .all(|field| !truth(&job[*field]))
}
fn identity(job: &Value) -> Value {
    let mut out = json!({});
    for field in [
        "id",
        "status",
        "exit_code",
        "todo_id",
        "related_todo_ids",
        "project",
        "cwd",
        "work_owner",
        "pid",
        "pid_created",
        "codex_pid",
        "codex_pid_created",
        "work_owner_live",
        "queue_unreadable",
        "cancel_requested",
        "queue_restart_pending",
        "reboot_launch_pending",
    ] {
        out[field] = job[field].clone();
    }
    out
}
fn normalized_row(row: &Value) -> Value {
    let mut row = row.clone();
    remove(&mut row, "updated");
    remove(&mut row["scheduler"], "workspace_relocation");
    row
}

// Keep the exact bytes open without write/delete sharing until the store CAS.
// The saved path comes only from the selected registered row, never from the caller.
fn pinned_text(path: &Path) -> Result<(File, String)> {
    let mut file = OpenOptions::new().read(true).share_mode(1).open(path)?;
    if !file.metadata()?.is_file() || file.metadata()?.len() > 1024 * 1024 {
        return Err(Error("RelocationSpecInvalid"));
    }
    let mut bytes = Vec::new();
    (&mut file).take(1024 * 1024 + 1).read_to_end(&mut bytes)?;
    if bytes.len() > 1024 * 1024 {
        return Err(Error("RelocationSpecInvalid"));
    }
    let text = String::from_utf8(bytes).map_err(|_| Error("RelocationSpecInvalid"))?;
    Ok((file, text))
}
fn saved_spec(engine_root: &Path, item: &Value) -> Result<PathBuf> {
    let path = Path::new(s(&item["spec"]));
    let root = engine_root
        .parent()
        .ok_or(Error("InputRejected"))?
        .join("specs");
    let prefix = format!("{}-", item["id"]);
    let name = path
        .file_name()
        .and_then(|n| n.to_str())
        .ok_or(Error("RelocationSpecPath"))?;
    let suffix = name
        .strip_prefix(&prefix)
        .and_then(|n| n.strip_suffix(".md"))
        .ok_or(Error("RelocationSpecPath"))?;
    if !sha(suffix, 32)
        || absolute(path)?.parent() != Some(absolute(&root)?.as_path())
        || norm(resolve(path)?.to_str().unwrap()) != norm(absolute(path)?.to_str().unwrap())
    {
        return Err(Error("RelocationSpecPath"));
    }
    Ok(path.to_path_buf())
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub id: u64,
    pub source_job: String,
    pub shared_owner_job: String,
    pub source_branch: String,
    pub source_head: String,
    pub worktree: String,
    pub branch: String,
    pub head: String,
    pub spec_reference: String,
    pub spec_sha256: String,
}

struct Proof {
    item: Value,
    repo: String,
    binding: Value,
    repeated: bool,
    _files: (File, File),
}

fn git_state(runner: &mut dyn Runner, path: &Path) -> Result<Value> {
    use crate::backend::run_git;
    Ok(
        json!({"branch":run_git(runner,path,&["branch","--show-current"])?,
        "head":run_git(runner,path,&["rev-parse","--verify","HEAD"])?,
        "status_sha256":cc_job_policy::product_retry::digest(run_git(runner,path,&["status","--porcelain","--untracked-files=all"])?.as_bytes())}),
    )
}

fn verify<B: Backend>(
    engine: &mut Scheduler<B>,
    runner: &mut dyn Runner,
    request: &Request,
) -> Result<Proof> {
    let prefix = format!("scheduler/todo-{}-", request.id);
    if request.id == 0
        || !request.branch.starts_with(&prefix)
        || request.branch.len() <= prefix.len()
        || !sha(&request.head, 40)
        || !sha(&request.source_head, 40)
        || !sha(&request.spec_sha256, 64)
    {
        return Err(Error("InputRejected"));
    }
    let db = engine.todo()?;
    let rows = arr(&db["items"]);
    let selected: Vec<_> = rows.iter().filter(|row| row["id"] == request.id).collect();
    if selected.len() != 1 {
        return Err(Error("RelocationTaskMissing"));
    }
    let item = selected[0].clone();
    if item["project"] != "cc-pc-tools-rs"
        || !item["subwork"].is_null()
        || item["status"] != "todo"
        || item["dispatchable"] != true
        || item["scheduler"]["phase"] != "retry"
        || truth(&item["scheduler"]["job"])
        || !arr(&item["wait_on"]).is_empty()
        || crate::targeted_dispatch::held(&item)
        || truth(&item["scheduler"]["question_close_pending"])
        || truth(&item["stall_recovery"]["pending"])
        || item["stopped"] == true
        || item["stop_requested"] == true
    {
        return Err(Error("RelocationTaskHeld"));
    }
    let repeated = item["scheduler"]["workspace_relocation"]["request"] == json!(request);
    if item["scheduler"]["workspace_relocation"].is_object() && !repeated {
        return Err(Error("RelocationReceiptMismatch"));
    }
    let repo = s(&db["projects"][s(&item["project"])]).to_owned();
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    let at = engine.backend.clock();
    let source = jobs
        .iter()
        .find(|j| j["id"] == request.source_job)
        .ok_or(Error("RelocationSourceMissing"))?;
    let owner = jobs
        .iter()
        .find(|j| j["id"] == request.shared_owner_job)
        .ok_or(Error("RelocationSharedOwnerMissing"))?;
    if source["todo_id"] != item["id"]
        || source["project"] != item["project"]
        || source["status"] != "succeeded"
        || source["exit_code"] != 0
        || !terminal(source)
        || crate::policy::alive(source, &ps, at)
        || !arr(&item["jobs"]).contains(&source["id"])
        || crate::preparation::workspace_source(&item, &jobs)
            .is_none_or(|job| job["id"] != source["id"])
    {
        return Err(Error("RelocationSourceOwnership"));
    }
    let old = if repeated {
        &item["scheduler"]["workspace_relocation"]["previous_scheduler"]
    } else {
        &item["scheduler"]
    };
    let shared = resolve(Path::new(s(&source["cwd"])))?;
    let target = resolve(Path::new(&request.worktree))?;
    let base = resolve(Path::new(&repo))?;
    let container = PathBuf::from(format!("{}-wt", base.display()));
    if target.parent() != Some(container.as_path())
        || !target
            .file_name()
            .and_then(|n| n.to_str())
            .is_some_and(|n| n.starts_with(&format!("todo-{}-", request.id)))
        || target == shared
        || !target.is_dir()
        || resolve(Path::new(s(&old["worktree"])))? != shared
        || s(&old["branch"]).starts_with(&prefix)
        || source["work_owner"]["branch"] != old["branch"]
    {
        return Err(Error("RelocationWorkspaceIdentity"));
    }
    for path in [
        &repo,
        s(&source["cwd"]),
        &request.worktree,
        &request.spec_reference,
    ] {
        if path.is_empty() || !engine.backend.repair_scope_allowed(path) {
            return Err(Error("ClientResource"));
        }
    }
    for path in [&base, &shared, &target] {
        if crate::preparation::git_root(runner, path)?.is_none() {
            return Err(Error("NonGit"));
        }
        crate::preparation::same_common(runner, &base, path)?;
    }
    let base_owner = cc_job_policy::work_owner::checkout(&repo);
    let current = cc_job_policy::work_owner::checkout(s(&source["cwd"]));
    let target_owner = cc_job_policy::work_owner::checkout(&request.worktree);
    if base_owner["repository"] != "autodevjapan/cc-pc-tools-rs"
        || ["repository", "git_common"].iter().any(|key| {
            base_owner[*key] != current[*key]
                || base_owner[*key] != target_owner[*key]
                || base_owner[*key] != source["work_owner"][*key]
        })
        || current["worktree"] != source["work_owner"]["worktree"]
    {
        return Err(Error("RelocationRepositoryIdentity"));
    }
    let owner_rows: Vec<_> = rows
        .iter()
        .filter(|r| r["id"] == owner["todo_id"])
        .collect();
    if owner["todo_id"] == item["id"]
        || owner["project"] != item["project"]
        || owner_rows.len() != 1
        || owner_rows[0]["project"] != item["project"]
        || owner_rows[0]["scheduler"]["job"] != owner["id"]
        || resolve(Path::new(s(&owner_rows[0]["scheduler"]["worktree"])))? != shared
        || owner_rows[0]["scheduler"]["branch"] != request.source_branch
        || !arr(&owner_rows[0]["jobs"]).contains(&owner["id"])
        || resolve(Path::new(s(&owner["cwd"])))? != shared
        || owner["work_owner"] != current
        || !matches!(s(&owner["status"]), "running" | "succeeded")
        || (owner["status"] == "running" && !crate::policy::alive(owner, &ps, at))
    {
        return Err(Error("RelocationSharedOwnerIdentity"));
    }
    for id in arr(&item["jobs"]) {
        if !jobs.iter().any(|j| j["id"] == id) {
            return Err(Error("RelocationWorkerUnknown"));
        }
    }
    for job in &jobs {
        let belongs = job["todo_id"] == item["id"]
            || arr(&job["related_todo_ids"]).contains(&item["id"])
            || arr(&item["jobs"]).contains(&job["id"]);
        let touches_target = (!s(&job["cwd"]).is_empty()
            && resolve(Path::new(s(&job["cwd"]))).is_ok_and(|p| p == target))
            || job["work_owner"]["worktree"] == target_owner["worktree"]
            || (job["work_owner"]["git_common"] == target_owner["git_common"]
                && job["work_owner"]["branch"] == request.branch);
        if (belongs || touches_target) && (!terminal(job) || crate::policy::alive(job, &ps, at)) {
            return Err(Error("RelocationCompetingWorker"));
        }
        let shared_competitor = job["id"] != owner["id"]
            && job["id"] != source["id"]
            && ((!s(&job["cwd"]).is_empty()
                && resolve(Path::new(s(&job["cwd"]))).is_ok_and(|p| p == shared))
                || job["work_owner"]["worktree"] == current["worktree"]);
        if shared_competitor && (!terminal(job) || crate::policy::alive(job, &ps, at)) {
            return Err(Error("RelocationSharedOwnerUnknown"));
        }
        if !repeated && truth(&old["key"]) && job["launch_key"] == old["key"] {
            return Err(Error("RelocationReservationOwned"));
        }
    }
    let (spec_file, spec) = pinned_text(&saved_spec(&engine.todo_path, &item)?)?;
    let (reference_file, reference) = pinned_text(Path::new(&request.spec_reference))?;
    if spec != reference
        || cc_job_policy::product_retry::digest(spec.as_bytes()) != request.spec_sha256
        || spec.lines().next() != Some(format!("ToDo{}", request.id).as_str())
        || !spec
            .replace('\\', "/")
            .contains(&request.worktree.replace('\\', "/"))
        || !spec.contains(&request.branch)
    {
        return Err(Error("RelocationSpecMismatch"));
    }
    let shared_state = git_state(runner, &shared)?;
    let target_state = git_state(runner, &target)?;
    if shared_state["branch"] != request.source_branch
        || shared_state["head"] != request.source_head
        || target_state["branch"] != request.branch
        || target_state["head"] != request.head
    {
        return Err(Error("RelocationGitChanged"));
    }
    if repeated
        && (item["scheduler"]["worktree"] != request.worktree
            || item["scheduler"]["branch"] != request.branch
            || digest(&normalized_row(&item))
                != item["scheduler"]["workspace_relocation"]["post_state_sha256"])
    {
        return Err(Error("RelocationStateChanged"));
    }
    let binding = json!({"item":if repeated {normalized_row(&item)} else {item.clone()},"repo":repo,"source":identity(source),"shared_owner":identity(owner),
        "shared_owner_assignment":owner_rows[0]["scheduler"]["job"],"shared":shared_state,"target":target_state,"spec_sha256":request.spec_sha256});
    Ok(Proof {
        item,
        repo,
        binding,
        repeated,
        _files: (spec_file, reference_file),
    })
}

pub fn relocate<B: Backend>(
    engine: &mut Scheduler<B>,
    runner: &mut dyn Runner,
    request: &Request,
    expected: Option<&str>,
) -> Result<Value> {
    let result = (|| -> Result<Value> {
        let _scheduler =
            ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::from_secs(30))?
                .ok_or(Error("RelocationBusy"))?;
        let queue = engine
            .root
            .parent()
            .ok_or(Error("InputRejected"))?
            .join("codex-jobs/jobs");
        std::fs::create_dir_all(&queue)?;
        let _queue = ByteLock::acquire(&queue.join(".queue.lock"), Duration::from_secs(30))?
            .ok_or(Error("RelocationBusy"))?;
        let proof = verify(engine, runner, request)?;
        let state = if proof.repeated {
            s(&proof.item["scheduler"]["workspace_relocation"]["source_state_sha256"]).to_owned()
        } else {
            digest(&proof.binding)
        };
        let mut report = json!({"id":request.id,"eligible":true,"saved":false,"already_recorded":proof.repeated,"launched":0,"state_sha256":state,"spec_sha256":request.spec_sha256});
        let Some(expected) = expected else {
            return Ok(report);
        };
        if !sha(expected, 64) || expected != state {
            return Err(Error("RelocationStateChanged"));
        }
        // Guard calls and Git can yield. Repeat the entire proof while ordinary
        // scheduler admission is held, then compare the selected row under todo.lock.
        let fresh = verify(engine, runner, request)?;
        if fresh.binding != proof.binding {
            return Err(Error("RelocationStateChanged"));
        }
        if proof.repeated {
            return Ok(report);
        }
        let mut applied = false;
        let at = engine.backend.clock();
        engine.edit_in_project(&proof.item["id"],Some(&proof.repo), |row| {
            if *row != proof.item { return; }
            let previous = row["scheduler"].clone();
            row["scheduler"]["worktree"] = json!(request.worktree);
            row["scheduler"]["branch"] = json!(request.branch);
            row["scheduler"]["key"] = Value::Null;
            row["scheduler"]["phase"] = json!("retry");
            row["scheduler"]["resume"] = json!(true);
            remove(&mut row["scheduler"],"selection");
            let post = digest(&normalized_row(row));
            row["scheduler"]["workspace_relocation"] = json!({"request":request,"previous_scheduler":previous,
                "source_state_sha256":state,"post_state_sha256":post,"source_identity":proof.binding["source"],
                "spec_sha256":request.spec_sha256,"at":at,"launched":0});
            applied = true;
        })?;
        if !applied {
            return Err(Error("RelocationStateChanged"));
        }
        report["saved"] = json!(true);
        Ok(report)
    })();
    match result {
        Err(error) if expected.is_none() => Ok(
            json!({"id":request.id,"eligible":false,"saved":false,"launched":0,"reason":error.0}),
        ),
        result => result,
    }
}

/// Only bridge the interval before a new ordinary worker has its own cwd.
/// Changed instructions or Git state require another explicit proof, never fallback.
pub(crate) fn recovered(
    runner: &mut dyn Runner,
    repo: &str,
    item: &Value,
    source: &Value,
) -> Result<Option<Value>> {
    let receipt = &item["scheduler"]["workspace_relocation"];
    if !receipt.is_object() || receipt["request"]["source_job"] != source["id"] {
        return Ok(None);
    }
    let request: Request = serde_json::from_value(receipt["request"].clone())?;
    let base = resolve(Path::new(repo))?;
    let target = resolve(Path::new(&request.worktree))?;
    if crate::preparation::git_root(runner, &target)?.is_none() {
        return Err(Error("NonGit"));
    }
    crate::preparation::same_common(runner, &base, &target)?;
    if identity(source) != receipt["source_identity"]
        || item["scheduler"]["worktree"] != request.worktree
        || item["scheduler"]["branch"] != request.branch
    {
        return Err(Error("RelocationReceiptMismatch"));
    }
    let (_file, spec) = pinned_text(Path::new(s(&item["spec"])))?;
    if cc_job_policy::product_retry::digest(spec.as_bytes()) != receipt["spec_sha256"] {
        return Err(Error("RelocationSpecMismatch"));
    }
    let actual = git_state(runner, Path::new(&request.worktree))?;
    if actual["branch"] != request.branch || actual["head"] != request.head {
        return Err(Error("RelocationGitChanged"));
    }
    Ok(Some(
        json!({"worktree":request.worktree,"branch":request.branch}),
    ))
}
