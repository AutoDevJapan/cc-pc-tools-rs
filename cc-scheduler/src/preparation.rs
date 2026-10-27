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
    crate::policy::continuation_session(job, cwd, jobs)
        .or_else(|| answered_conversation_session(item, job, cwd, jobs))
        .or_else(|| {
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

/// Written by cc-todo's verified `question-answer` for the exact launch that
/// asked the question (see cc-todo conversation_wait SESSION_PROOF_KEY).
const CONVERSATION_ANSWER: &str = "conversation_answer";

/// A job that ended with a concrete "状態: 要判断" question is not a continuation
/// by itself. Its own conversation may continue only for the owner answer that
/// the ToDo layer proved for that same job and launch key, while the answer
/// text is still the pending instruction. Any later job, another launch, a stop
/// or a live user of the session never matches.
fn answered_conversation_session<'a>(
    item: &Value,
    job: &'a Value,
    cwd: &str,
    jobs: &[Value],
) -> Option<&'a str> {
    let proof = &item["scheduler"][CONVERSATION_ANSWER];
    let session = cc_ask::job_policy::launch_context::usable_thread_session(job)?;
    (proof.is_object()
        && !s(&job["id"]).is_empty()
        && proof["job"] == job["id"]
        && !s(&job["launch_key"]).is_empty()
        && proof["launch_key"] == job["launch_key"]
        && !s(&proof["request_id"]).is_empty()
        && !s(&proof["question_receipt_sha256"]).is_empty()
        && item["scheduler"]["last_job"] == job["id"]
        && truth(&item["scheduler"]["resume"])
        && !s(&item["scheduler"]["owner_instruction"]).trim().is_empty()
        // The ToDo layer records "conversation_answer"; a result the scheduler had
        // already imported as a decision is recorded as "block".
        && matches!(
            s(&item["processed_results"][s(&job["id"])]["reason"]),
            "conversation_answer" | "block"
        )
        && s(&job["status"]) == "succeeded"
        && cc_ask::core::result_state(s(&job["last_message"])) == "状態: 要判断"
        && !cc_job_policy::cancellation::blocks_recovery(item, job)
        && crate::policy::valid_session(session)
        && norm(s(&job["cwd"])) == norm(cwd)
        && !jobs.iter().any(|j| {
            crate::policy::live(j)
                && (s(&j["thread_id"]) == session || s(&j["resume_session"]) == session)
        }))
    .then_some(session)
}

/// A historical manual review's read source is not its independent execution checkout.
pub fn inherited_review_workspace(item: &Value, jobs: &[Value]) -> bool {
    item["subwork"]["kind"] == "review"
        && !s(&item["subwork"]["owner"]["worktree"]).is_empty()
        && workspace_source(item, jobs).is_some_and(|job| {
            norm(s(&job["cwd"])) == norm(s(&item["subwork"]["owner"]["worktree"]))
                || recorded_parent_review_source(item, job)
        })
}

// A resumed writer can execute in a different checkout from the fixed source
// used by its earlier manual review. Only that review's recorded Git ownership
// can identify the other parent checkout; a matching repository alone cannot.
fn recorded_parent_review_source(item: &Value, job: &Value) -> bool {
    let owner = &item["subwork"]["owner"];
    let recorded = &job["work_owner"];
    let Some(parent) = item["subwork"]["parent_todo_id"].as_i64() else {
        return false;
    };
    if !recorded.is_object()
        || parent <= 0
        || ["repository", "git_common"]
            .iter()
            .any(|key| s(&owner[*key]).is_empty() || recorded[*key] != owner[*key])
        || !s(&recorded["branch"]).starts_with(&format!("scheduler/todo-{parent}-"))
        || (owner["pr"].is_u64() && recorded["pr"] != owner["pr"])
    {
        return false;
    }
    let actual = cc_job_policy::work_owner::checkout(s(&job["cwd"]));
    ["repository", "git_common"]
        .iter()
        .all(|key| actual[*key] == owner[*key])
        && !s(&actual["worktree"]).is_empty()
        && actual["worktree"] == recorded["worktree"]
        && s(&actual["worktree"]) == cc_job_policy::work_owner::norm(Path::new(s(&job["cwd"])))
        && actual["branch"] == recorded["branch"]
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
    let source = workspace_source(item, jobs);
    if let Some(relocated) = crate::workspace_relocation::recovered(runner, repo, item, source)? {
        return Ok(Some(relocated));
    }
    let Some(job) = source else { return Ok(None); };
    let base = resolve(Path::new(repo))?;
    if git_root(runner, &base)?.is_none() {
        // Non-Git projects always use the bounded dedicated workspace; source files stay in place.
        return Ok(None);
    }
    let target = resolve(Path::new(s(&job["cwd"])))?;
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
    if inherited_review_workspace(item, jobs) {
        let container = std::path::PathBuf::from(format!("{}-wt", base.display()));
        let saved = s(&item["scheduler"]["worktree"]);
        if !saved.is_empty() && resolve(Path::new(saved))? != target {
            // Preserve an already separated reservation. ensure_workspace() and
            // subwork::resolve() verify the actual checkout before launch.
            let reserved = resolve(Path::new(saved))?;
            if reserved.parent() == Some(container.as_path())
                && s(&item["scheduler"]["branch"])
                    .starts_with(&format!("scheduler/todo-{}-", item["id"]))
            {
                return Ok(None);
            }
            return Err(Error("InputRejected"));
        }
        let name = format!("todo-{}-review", item["id"]);
        let previous = json!({"worktree":item["scheduler"]["worktree"],
            "branch":item["scheduler"]["branch"],"workspace_receipt":item["scheduler"]["workspace_receipt"]});
        let migration = json!({"source_job":job["id"],"source_worktree":job["cwd"],
            "source_branch":branch,"previous":previous});
        if item["scheduler"]["subwork_workspace_migration"].is_object()
            && item["scheduler"]["subwork_workspace_migration"] != migration
        {
            return Err(Error("InputRejected"));
        }
        if norm(s(&job["cwd"])) != norm(s(&item["subwork"]["owner"]["worktree"])) {
            eprintln!("subwork workspace migration source=verified_parent_checkout");
        }
        return Ok(Some(
            json!({"worktree":container.join(&name),"branch":format!("scheduler/{name}"),
            "subwork_workspace_migration":migration}),
        ));
    }
    Ok(Some(json!({"worktree":target,"branch":branch,
        "workspace_receipt":{"job":job["id"],"todo_id":item["id"],"cwd":job["cwd"]}})))
}

/// Whether recovery will consume an old checkout. Non-Git projects never read
/// their historical shared cwd; ordinary preparation uses their dedicated root.
pub fn recovery_uses_checkout(runner: &mut dyn Runner, repo: &str) -> Result<bool> {
    let base = resolve(Path::new(repo))?;
    Ok(git_root(runner, &base)?.is_some())
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
        "LaunchOutcomeUnverified" => ("launch_outcome_unverified","返却は記録しましたが起動の照合が未確認です"),
        "LaunchOutcomeUnsupported" => ("launch_outcome_unsupported","起動ツールを先に更新する必要があります"),
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
    let explicit = job["resource_wait"]["until"].as_f64().filter(|until| until.is_finite());
    let out = Output {
        code: 1,
        stdout: String::new(),
        stderr: s(&job["last_error"]).into(),
    };
    let classified = classify_output(&out).0 == "ResourceWait";
    let terminal = matches!(s(&job["status"]), "failed" | "lost" | "cancelled" | "canceled");
    if explicit.is_none() && !classified && (!terminal || (n(&job["retry_not_before"]) <= now && n(&job["rate_limit_reset_at"]) <= now)) {
        return None;
    }
    // A historical failure is a fixed observation. Re-reading its text must
    // not manufacture another hour of exhaustion after its deadline elapsed.
    let observed = epoch(&job["finished_at"]);
    let fallback = if classified && observed > 0. && observed.is_finite() { observed + 3600. } else { 0. };
    let until =
        [
            explicit.unwrap_or(fallback),
            n(&job["retry_not_before"]),
            n(&job["rate_limit_reset_at"]),
        ]
        .into_iter()
        .filter(|t| t.is_finite())
        .fold(0., f64::max);
    (now.is_finite() && until > now).then_some(until)
}

/// Candidate selection only. prepare must still prove fresh quota, exact
/// scope/spec and source drain before this automatic wait can reach a launch.
pub fn quota_recheck_candidate(item: &Value, jobs: &[Value], now: f64) -> bool {
    let m = &item["scheduler"];
    let preparation = m["phase"] == "preparing"
        && m["preparation_failure"]["reason"] == "resource_wait"
        && m["resource_wait"]["reason"] == "http_429"
        && m["resource_wait"]["until"] == m["not_before"]
        && m["preparation_failure"]["retry_at"] == m["not_before"];
    let retry = m["phase"] == "retry" && m["last_stop"] == "failed"
        && m["resource_wait"].is_null() && m["preparation_failure"].is_null();
    n(&m["not_before"]) > now && item["subwork"].is_object()
        && !truth(&m["question_close_pending"])
        && n(&m["quota_recheck_after"]) <= now
        && (m["preparation_failure"]["reason"] != "resource_wait"
            || n(&m["preparation_failure"]["at"]) + 300. <= now)
        && (retry || preparation) && !crate::targeted_dispatch::held(item)
        && jobs.iter().any(|source| {
            source["id"] == m["last_job"] && owns_workspace(item, source)
                && source["status"] == "failed"
                && cc_job_policy::quota_recovery::quota_source(source)
                && ((retry && cc_job_policy::quota_recovery::provider(s(&source["model"])) == Some("xai"))
                    || (cc_job_policy::quota_recovery::provider(s(&source["model"])) == Some("openai")
                        && cc_job_policy::quota_recovery::eligible_source(item, source)
                        && resource_deadline(source, now).is_none()))
        })
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
