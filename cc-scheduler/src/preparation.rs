//! Bounded workspace preparation. No resets, checkouts, deletion or repository publication.
use crate::{backend::run_git, common::*};
use serde_json::{json, Value};
use std::{fs, path::Path};

/// Identity comes from exact persisted receipts or job metadata, never prompt text or a PID.
pub fn owns_workspace(item: &Value, job: &Value) -> bool {
    !s(&job["id"]).is_empty()
        && (job["todo_id"] == item["id"]
            || arr(&job["related_todo_ids"]).contains(&item["id"])
            || (item["worker_receipt"]["job"] == job["id"]
                && item["worker_receipt"]["generation"] == item["resume_generation"])
            || item["scheduler"]["job"] == job["id"]
            || item["scheduler"]["last_job"] == job["id"])
}

pub fn workspace_source<'a>(item: &Value, jobs: &'a [Value]) -> Option<&'a Value> {
    // A current receipt beats historical jobs. Failed/cancelled workers still own their files.
    for field in ["job", "last_job"] {
        if let Some(job) = jobs.iter().find(|j| {
            !s(&item["scheduler"][field]).is_empty()
                && j["id"] == item["scheduler"][field]
                && owns_workspace(item, j)
                && !s(&j["cwd"]).is_empty()
        }) {
            return Some(job);
        }
    }
    arr(&item["jobs"]).iter().rev().find_map(|id| {
        jobs.iter()
            .find(|j| j["id"] == *id && owns_workspace(item, j) && !s(&j["cwd"]).is_empty())
    })
}

pub fn recovery_session<'a>(
    item: &Value,
    job: &'a Value,
    cwd: &str,
    jobs: &[Value],
) -> Option<&'a str> {
    if !owns_workspace(item, job) {
        return None;
    }
    crate::policy::continuation_session(job, cwd, jobs).or_else(|| {
        let session = cc_ask::job_policy::launch_context::usable_thread_session(job)?;
        (truth(&item["scheduler"]["resume"])
            && owns_workspace(item, job)
            && matches!(
                s(&job["status"]),
                "failed" | "lost" | "cancelled" | "canceled"
            )
            && crate::policy::valid_session(session)
            && norm(s(&job["cwd"])) == norm(cwd)
            && !jobs.iter().any(|j| {
                crate::policy::live(j)
                    && (s(&j["thread_id"]) == session || s(&j["resume_session"]) == session)
            }))
        .then_some(session)
    })
}

pub(crate) fn git_root(runner: &mut dyn Runner, path: &Path) -> Result<Option<String>> {
    if !path.is_dir() {
        return Err(Error("CwdMissing"));
    }
    let mut c = invocation(
        &[
            "git",
            "-C",
            path.to_str().ok_or(Error("InputRejected"))?,
            "rev-parse",
            "--show-toplevel",
        ],
        Some(40),
    );
    c.git_prompt_off = true;
    let out = runner.run(&c).map_err(tool_error)?;
    if out.code == 0 {
        let root = out.stdout.trim();
        // Git may discover an unrelated repository in an ancestor of a reports folder.
        return Ok((norm(root) == norm(path.to_str().unwrap())).then(|| root.to_owned()));
    }
    if out.stderr.to_lowercase().contains("not a git repository") {
        return Ok(None);
    }
    Err(classify_output(&out))
}

pub(crate) fn same_common(runner: &mut dyn Runner, base: &Path, target: &Path) -> Result<()> {
    let left = resolve(&target.join(run_git(runner, target, &["rev-parse", "--git-common-dir"])?))?;
    let right = resolve(&base.join(run_git(runner, base, &["rev-parse", "--git-common-dir"])?))?;
    if norm(left.to_str().unwrap()) != norm(right.to_str().unwrap()) {
        return Err(Error("RepositoryMismatch"));
    }
    Ok(())
}

pub fn recover_workspace(
    runner: &mut dyn Runner,
    repo: &str,
    item: &Value,
    jobs: &[Value],
) -> Result<Option<Value>> {
    let Some(job) = workspace_source(item, jobs) else {
        return Ok(None);
    };
    let base = resolve(Path::new(repo))?;
    let target = resolve(Path::new(s(&job["cwd"])))?;
    if git_root(runner, &base)?.is_none() {
        // Non-Git projects always use the bounded dedicated workspace; source files stay in place.
        return Ok(None);
    }
    let root = git_root(runner, &target)?.ok_or(Error("NonGit"))?;
    if norm(&root) != norm(target.to_str().unwrap()) {
        return Err(Error("RepositoryMismatch"));
    }
    same_common(runner, &base, &target)?;
    let branch = run_git(runner, &target, &["branch", "--show-current"])?;
    if branch.is_empty() {
        return Err(Error("BranchMismatch"));
    }
    if !s(&item["scheduler"]["worktree"]).is_empty()
        && resolve(Path::new(s(&item["scheduler"]["worktree"])))? == target
        && !s(&item["scheduler"]["branch"]).is_empty()
        && s(&item["scheduler"]["branch"]) != branch
    {
        return Err(Error("BranchMismatch"));
    }
    Ok(Some(
        json!({"worktree":target,"branch":branch,"workspace_receipt":{"job":job["id"],"todo_id":item["id"],"cwd":job["cwd"]}}),
    ))
}

pub fn ensure_workspace(
    runner: &mut dyn Runner,
    repo: &str,
    item: &Value,
    jobs: &[Value],
) -> Result<()> {
    let base = resolve(Path::new(repo))?;
    if !base.is_dir() {
        return Err(Error("CwdMissing"));
    }
    let target = resolve(Path::new(s(&item["scheduler"]["worktree"])))?;
    let container = std::path::PathBuf::from(format!("{}-wt", base.display()));
    let branch = s(&item["scheduler"]["branch"]);
    let bounded = target.parent() == Some(container.as_path())
        && branch.starts_with(&format!("scheduler/todo-{}-", item["id"]));
    let receipt = &item["scheduler"]["workspace_receipt"];
    let owned = jobs.iter().any(|j| {
        receipt["job"] == j["id"]
            && receipt["todo_id"] == item["id"]
            && owns_workspace(item, j)
            && !s(&j["cwd"]).is_empty()
            && resolve(Path::new(s(&j["cwd"]))).is_ok_and(|p| p == target)
            && receipt["cwd"] == j["cwd"]
    });
    if !bounded && !owned {
        return Err(Error("InputRejected"));
    }
    let base_git = git_root(runner, &base)?;
    if item["product_review_allocation"].is_object() {
        if !crate::product_review::eligible(item, crate::common::now()) {
            return Err(Error("InputRejected"));
        }
        if target.exists()
            && run_git(runner, &target, &["rev-parse", "HEAD"])? != crate::product_review::HEAD
        {
            return Err(Error("InputRejected"));
        }
    }
    if base_git.is_none() {
        if !bounded {
            return Err(Error("InputRejected"));
        }
        if target.exists() {
            if !target.is_dir() {
                return Err(Error("InputRejected"));
            }
            if git_root(runner, &target)?.is_some() {
                return Err(Error("RepositoryMismatch"));
            }
        } else {
            fs::create_dir_all(&target)?;
        }
        return Ok(());
    }
    if target.exists() {
        let top = git_root(runner, &target)?.ok_or(Error("NonGit"))?;
        if norm(&top) != norm(target.to_str().unwrap()) {
            return Err(Error("RepositoryMismatch"));
        }
        if run_git(runner, &target, &["branch", "--show-current"])? != branch {
            return Err(Error("BranchMismatch"));
        }
        same_common(runner, &base, &target)?;
        return Ok(());
    }
    // A source receipt must never be replaced with an empty checkout.
    if owned {
        return Err(Error("CwdMissing"));
    }
    // Preparation can lose its directory after reserving the branch. Reuse
    // that exact ref, preserving its commit instead of recreating it from main.
    let reference = format!("refs/heads/{branch}");
    let mut check = invocation(
        &[
            "git",
            "-C",
            base.to_str().unwrap(),
            "show-ref",
            "--exists",
            &reference,
        ],
        Some(40),
    );
    check.git_prompt_off = true;
    let result = runner.run(&check).map_err(tool_error)?;
    match result.code {
        0 => {
            if item["product_review_allocation"].is_object()
                && run_git(runner, &base, &["rev-parse", &reference])?
                    != crate::product_review::HEAD
            {
                return Err(Error("InputRejected"));
            }
            fs::create_dir_all(container)?;
            return run_git(
                runner,
                &base,
                &["worktree", "add", target.to_str().unwrap(), branch],
            )
            .map(|_| ());
        }
        2 => (), // Git's --exists distinguishes a missing ref from lookup failure.
        _ => return Err(classify_output(&result)),
    }
    run_git(runner, &base, &["remote", "get-url", "origin"])?;
    if item["product_review_allocation"].is_object() {
        if !crate::product_review::eligible(item, crate::common::now()) {
            return Err(Error("InputRejected"));
        }
        run_git(
            runner,
            &base,
            &["cat-file", "-e", crate::product_review::HEAD],
        )?;
        fs::create_dir_all(container)?;
        return run_git(
            runner,
            &base,
            &[
                "worktree",
                "add",
                "-b",
                branch,
                target.to_str().unwrap(),
                crate::product_review::HEAD,
            ],
        )
        .map(|_| ());
    }
    run_git(runner, &base, &["fetch", "--quiet", "origin", "main"])?;
    run_git(
        runner,
        &base,
        &["rev-parse", "--verify", "refs/remotes/origin/main"],
    )?;
    fs::create_dir_all(container)?;
    run_git(
        runner,
        &base,
        &[
            "worktree",
            "add",
            "-b",
            branch,
            target.to_str().unwrap(),
            "origin/main",
        ],
    )?;
    Ok(())
}

pub fn tool_error(e: Error) -> Error {
    match e.0 {
        "FileNotFoundError" => Error("ToolMissing"),
        "ValueError" => Error("InputRejected"),
        _ => e,
    }
}
/// Only fixed reason codes leave this boundary. Never echo stderr, arguments or paths.
pub fn classify_output(out: &Output) -> Error {
    let text = out.stderr.to_lowercase();
    if text.contains("not a git repository") { Error("NonGit") }
    else if text.contains("no such remote") || text.contains("no such remote 'origin'") { Error("OriginMissing") }
    else if text.contains("cannot change to") || text.contains("cwd does not exist") { Error("CwdMissing") }
    else if regex::Regex::new(r"(?i)\b(?:http\s*429|status(?:\s+code)?[:= ]+429|too many requests|insufficient quota|quota exhausted)\b").unwrap().is_match(&text) { Error("ResourceWait") }
    else if out.code == 2 { Error("InputRejected") }
    else { Error("ToolFailed") }
}

pub fn failure_reason(e: &Error) -> (&'static str, &'static str) {
    match e.0 {
        "CwdMissing" => ("cwd_missing", "作業フォルダがありません"),
        "NonGit" => ("non_git", "Gitの作業場所として確認できません"),
        "OriginMissing" => ("origin_missing", "Gitの取得元がありません"),
        "BranchMismatch" => ("branch_mismatch", "保存されたブランチと一致しません"),
        "RepositoryMismatch" => (
            "repository_mismatch",
            "登録されたGitの管理領域と一致しません",
        ),
        "ToolMissing" => ("tool_missing", "必要なツールがありません"),
        "ResourceWait" | "ModelUnavailable" => {
            ("resource_wait", "指定モデルの利用枠を待っています")
        }
        "InputRejected" | "ValueError" | "InvalidSubwork" => {
            ("input_rejected", "保存された起動情報を受け付けられません")
        }
        "ClientResource" => (
            "excluded_resource",
            "この担当の作業対象から除外されています",
        ),
        _ => ("tool_failed", "準備に必要な処理が失敗しました"),
    }
}

pub fn resource_deadline(job: &Value, now: f64) -> Option<f64> {
    if job["status"] == "succeeded" {
        return None;
    }
    let explicit = job["resource_wait"]["until"]
        .as_f64()
        .filter(|until| until.is_finite() && *until > now);
    let out = Output {
        code: 1,
        stdout: String::new(),
        stderr: s(&job["last_error"]).into(),
    };
    if explicit.is_none() && classify_output(&out).0 != "ResourceWait" {
        return None;
    }
    Some(
        [
            explicit.unwrap_or(now + 3600.),
            n(&job["retry_not_before"]),
            n(&job["rate_limit_reset_at"]),
        ]
        .into_iter()
        .filter(|t| t.is_finite())
        .fold(now, f64::max),
    )
}

/// Reuse recorded quota evidence; never probe a depleted subscription on every scheduler tick.
pub fn cached_exhausted(state: &Value, model: &str, now: f64) -> Option<f64> {
    if !(model.starts_with("anthropic/") || model.starts_with("claude-")) {
        return None;
    }
    let quota = &state["anthropic"];
    let accounts = arr(&quota["accounts"]);
    if quota["complete"] != true
        || accounts.is_empty()
        || accounts.iter().any(|a| {
            a["used_percent"].as_f64().is_none_or(|used| used < 100.)
                || a["reset_at"]
                    .as_f64()
                    .is_none_or(|at| !at.is_finite() || at <= now)
        })
    {
        return None;
    }
    accounts
        .iter()
        .filter_map(|a| a["reset_at"].as_f64())
        .min_by(f64::total_cmp)
}
