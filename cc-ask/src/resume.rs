use crate::core::*;
use regex::Regex;
use serde_json::{json, Value};
use std::path::{Component, Path, PathBuf};

pub fn git(ctx: &mut Context, cwd: &str, arguments: &[&str]) -> Result<Output> {
    let mut argv = vec!["git".into(), "-C".into(), cwd.into()];
    argv.extend(arguments.iter().map(|arg| arg.to_string()));
    crate::state_work::check(ctx)?;
    ctx.run(&argv, 45)
}

pub fn path_key(path: &str) -> String {
    let path = PathBuf::from(path);
    let absolute = if path.is_absolute() {
        path
    } else {
        std::env::current_dir().unwrap_or_default().join(path)
    };
    let mut normalized = PathBuf::new();
    for component in absolute.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                normalized.pop();
            }
            other => normalized.push(other.as_os_str()),
        }
    }
    let key = normalized
        .to_string_lossy()
        .trim_end_matches(['/', '\\'])
        .to_string();
    #[cfg(windows)]
    let key = key.replace('/', "\\").to_lowercase();
    key
}

pub fn in_project(cwd: &str, base: &str) -> bool {
    if cwd.is_empty() || base.is_empty() {
        return false;
    }
    let (path, root) = (path_key(cwd), path_key(base));
    let centralized = Path::new(base).file_name().map(|name| {
        path_key(
            &PathBuf::from("D:/CC/Worktrees")
                .join(name)
                .to_string_lossy(),
        )
    });
    path == root
        || path.starts_with(&format!("{root}{}", std::path::MAIN_SEPARATOR))
        || path.starts_with(&format!("{root}-wt{}", std::path::MAIN_SEPARATOR))
        || centralized.is_some_and(|worktrees| {
            path.starts_with(&format!("{worktrees}{}", std::path::MAIN_SEPARATOR))
        })
}

pub fn strip_verbatim(path: &str) -> String {
    if let Some(unc) = path.strip_prefix(r"\\?\UNC\") {
        format!(r"\\{unc}")
    } else {
        path.strip_prefix(r"\\?\").unwrap_or(path).to_string()
    }
}

fn resolved(path: &Path) -> Result<PathBuf> {
    let directory = path
        .canonicalize()
        .map_err(|_| "recorded working directory is missing".to_string())?;
    // Git returns ordinary DOS or UNC paths, rather than Windows verbatim paths.
    #[cfg(windows)]
    let directory = PathBuf::from(strip_verbatim(&directory.to_string_lossy()));
    Ok(directory)
}

pub fn continuation_route(ctx: &mut Context, cwd: &str) -> Result<Value> {
    let directory = resolved(Path::new(cwd))?;
    if !directory.is_dir() {
        return Err("recorded working directory is missing".into());
    }
    let cwd = directory.to_string_lossy().to_string();
    let probe = git(
        ctx,
        &cwd,
        &[
            "rev-parse",
            "--path-format=absolute",
            "--show-toplevel",
            "--git-common-dir",
        ],
    )?;
    if probe.code != 0 {
        if directory.ancestors().any(|p| p.join(".git").exists()) {
            return Err("cannot resolve repository".into());
        }
        return Ok(json!({"cwd":cwd,"repo":cwd,"main":false}));
    }
    let lines: Vec<_> = probe.stdout.trim().lines().collect();
    if lines.len() != 2 {
        return Err("cannot resolve repository".into());
    }
    let root = resolved(Path::new(lines[0]))?;
    let common = Path::new(lines[1]);
    let repo = common.parent().ok_or("cannot resolve repository")?;
    let relative = directory
        .strip_prefix(&root)
        .or_else(|_| {
            // Git uses forward slashes on Windows; Path comparison handles those.
            directory.strip_prefix(PathBuf::from(
                lines[0].replace('/', std::path::MAIN_SEPARATOR_STR),
            ))
        })
        .map_err(|_| "recorded directory is outside repository".to_string())?;
    let repo = resolved(repo)?;
    Ok(
        json!({"cwd":cwd,"repo":repo.to_string_lossy(),"main":path_key(&root.join(".git").to_string_lossy())==path_key(lines[1]),"root":lines[0],"relative":if relative.as_os_str().is_empty(){".".to_string()}else{relative.to_string_lossy().to_string()}}),
    )
}

pub fn prepare_continuation_cwd(ctx: &mut Context, route: &Value, key: &str) -> Result<String> {
    if !truth(&route["main"]) {
        return Ok(s(&route["cwd"]).into());
    }
    let short = format!("reply-{}", key.chars().take(12).collect::<String>());
    let target = PathBuf::from(format!("{}-wt", s(&route["repo"]))).join(&short);
    if target.exists() {
        let existing = continuation_route(ctx, &target.to_string_lossy())?;
        if truth(&existing["main"]) || path_key(s(&existing["repo"])) != path_key(s(&route["repo"]))
        {
            return Err("continuation worktree path belongs to another repository".into());
        }
    } else {
        let branch = format!("resume/{short}");
        let repo = s(&route["repo"]);
        let origin = git(ctx, repo, &["remote", "get-url", "origin"])?;
        let base = if origin.code == 0 && !origin.stdout.trim().is_empty() {
            checked_git(ctx, repo, &["fetch", "origin", "main"])?;
            "origin/main"
        } else if origin.code == 2 || (origin.code == 0 && origin.stdout.trim().is_empty()) {
            // A repository without a remote still has an authoritative committed HEAD.
            "HEAD"
        } else {
            return Err(format!(
                "continuation remote probe failed: {}",
                error_detail(&origin.stderr)
            ));
        };
        let existing = git(
            ctx,
            repo,
            &[
                "show-ref",
                "--verify",
                "--quiet",
                &format!("refs/heads/{branch}"),
            ],
        )?;
        if existing.code == 0 {
            checked_git(
                ctx,
                repo,
                &[
                    "worktree",
                    "add",
                    target.to_str().ok_or("invalid worktree path")?,
                    &branch,
                ],
            )?;
        } else if existing.code == 1 || (existing.code == 0 && existing.stdout.trim().is_empty()) {
            checked_git(
                ctx,
                repo,
                &[
                    "worktree",
                    "add",
                    "-b",
                    &branch,
                    target.to_str().ok_or("invalid worktree path")?,
                    base,
                ],
            )?;
        } else {
            return Err(format!(
                "continuation branch probe failed: {}",
                error_detail(&existing.stderr)
            ));
        }
    }
    let relative = if s(&route["relative"]).is_empty() {
        "."
    } else {
        s(&route["relative"])
    };
    let directory = if relative == "." {
        target
    } else {
        target.join(relative)
    };
    if !directory.is_dir() {
        return Err("recorded subdirectory is missing in origin/main".into());
    }
    Ok(directory.to_string_lossy().into())
}

fn checked_git(ctx: &mut Context, cwd: &str, arguments: &[&str]) -> Result<Output> {
    let out = git(ctx, cwd, arguments)?;
    if out.code != 0 {
        return Err(format!(
            "continuation worktree setup failed: git -C {cwd} {} exited {}: {}",
            arguments.join(" "),
            out.code,
            error_detail(&out.stderr)
        ));
    }
    Ok(out)
}

fn contains_number(text: &str, number: &str) -> bool {
    text.match_indices(number).any(|(index, _)| {
        !text[..index]
            .chars()
            .next_back()
            .is_some_and(|c| c.is_ascii_digit())
            && !text[index + number.len()..]
                .chars()
                .next()
                .is_some_and(|c| c.is_ascii_digit())
    })
}

pub fn task_matches(question: &Value, history: &[Value], candidate: &str) -> bool {
    let candidate = candidate.to_lowercase();
    if !s(&question["thread_id"]).is_empty()
        && contains_number(&candidate, s(&question["thread_id"]))
    {
        return true;
    }
    let issues = Regex::new(r"(?i)(?:#|(?:issues?|pull)/)(\d+)\b").unwrap();
    let title = s(&question["title"]);
    let context = history
        .iter()
        .filter(|m| n(&m["id"]) <= n(&question["after"]))
        .map(|m| s(&m["content"]))
        .collect::<Vec<_>>()
        .join("\n");
    let source = if issues.is_match(title) {
        title
    } else {
        &context
    };
    if issues
        .captures_iter(source)
        .any(|c| contains_number(&candidate, &c[1]))
    {
        return true;
    }
    let title = Regex::new(r"^\[[^\]]+\]\s*").unwrap().replace(title, "");
    let title = Regex::new(r"(?i)^Q\d+\s*[:：]?\s*")
        .unwrap()
        .replace(&title, "");
    let normalize = |text: &str| {
        text.to_lowercase()
            .chars()
            .filter(|c| c.is_alphanumeric())
            .collect::<String>()
    };
    let title = normalize(&title);
    title.chars().count() >= 8 && normalize(&candidate).contains(&title)
}

fn epoch(value: &str) -> Option<f64> {
    chrono::DateTime::parse_from_rfc3339(value)
        .map(|d| d.timestamp_millis() as f64 / 1000.)
        .ok()
        .or_else(|| {
            [
                "%Y-%m-%dT%H:%M:%S%.f",
                "%Y-%m-%d %H:%M:%S%.f",
                "%Y-%m-%d %H:%M",
                "%Y-%m-%dT%H:%M",
            ]
            .iter()
            .find_map(|format| chrono::NaiveDateTime::parse_from_str(value, format).ok())
            .and_then(|d| d.and_local_timezone(chrono::Local).single())
            .map(|d| d.timestamp_millis() as f64 / 1000.)
        })
}

pub fn already_handled(
    ctx: &mut Context,
    question: &Value,
    history: &[Value],
    reply: &Value,
    route: &Value,
) -> Result<Option<String>> {
    let db = if ctx.todo.exists() {
        read_json(&ctx.todo)?
    } else {
        json!({"projects":{},"items":[]})
    };
    let projects: Vec<_> = db["projects"]
        .as_object()
        .into_iter()
        .flatten()
        .filter(|(_, path)| in_project(s(&route["cwd"]), s(path)))
        .map(|(name, _)| name.as_str())
        .collect();
    for item in db["items"].as_array().into_iter().flatten() {
        if truth(&question["restart"]["todo"]) && item["id"] == question["restart"]["todo"] {
            continue;
        }
        if s(&item["status"]) != "doing" || !projects.contains(&s(&item["project"])) {
            continue;
        }
        if epoch(s(&item["updated"])).is_some_and(|at| at > snowflake_time(s(&reply["id"])))
            && item["jobs"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|id| RUNNING.contains(&s(&job_info(ctx, s(id))["status"])))
            && task_matches(
                question,
                history,
                &format!("{}\n{}", s(&item["title"]), s(&item["note"])),
            )
        {
            return Ok(Some(format!(
                "共有 ToDo #{}（{}）が回答後に更新され、別セッションで着手中です。",
                n(&item["id"]),
                safe_text(s(&item["title"]))
            )));
        }
    }
    if let Ok(directories) = std::fs::read_dir(ctx.jobs.join("jobs")) {
        for directory in directories.flatten() {
            if !directory.path().join("job.json").is_file() {
                continue;
            }
            let id = directory.file_name().to_string_lossy().to_string();
            let job = job_info(ctx, &id);
            if !RUNNING.contains(&s(&job["status"]))
                || !in_project(s(&job["cwd"]), s(&route["repo"]))
            {
                continue;
            }
            let mut candidate = ["name", "activity", "notes_hint"]
                .iter()
                .map(|key| s(&job[key]))
                .collect::<Vec<_>>()
                .join("\n");
            let prompt = directory.path().join("prompt.md");
            if prompt.exists() {
                candidate.push('\n');
                candidate.push_str(&read_text(&prompt)?);
            }
            if task_matches(question, history, &candidate) {
                return Ok(Some(format!(
                    "ジョブ {id} が同じリポジトリでこの作業を実行中です。"
                )));
            }
        }
    }
    Ok(None)
}

pub fn continuation_prompt(
    ctx: &mut Context,
    question: &Value,
    history: &[Value],
    owner: &str,
) -> Result<String> {
    let thread = json!({"id":question["thread_id"],"name":question["title"]});
    let agent = if s(&question["agent"]).is_empty() {
        "未記録"
    } else {
        s(&question["agent"])
    };
    let original_path = ctx
        .jobs
        .join("jobs")
        .join(s(&question["job"]))
        .join("prompt.md");
    let original = if valid_job(s(&question["job"])) && original_path.is_file() {
        crate::prompt_context::bounded(
            &read_text(&original_path)?,
            12 * 1024,
            &original_path.to_string_lossy(),
        )
    } else {
        String::new()
    };
    let links = if truth(&question["separated_question"]) {
        format!("\n元の作業投稿: {}\n質問投稿: {}\n通知しない進捗投稿: {}\n元ToDo: {}\n質問だけを独立投稿にしています。元の依頼と回答をこの作業へ反映してください。\n",
            s(&question["work_url"]),s(&question["question_url"]),s(&question["progress_url"]),question["todo_id"])
    } else {
        String::new()
    };
    let owner_context = crate::decision_identity::task_handoff(ctx, question)?;
    Ok(safe_text(format!(
        "{RESUME_PREAMBLE}\n元の質問者: {agent}\n質問時の作業フォルダ: {}\nスレッドID: {}{links}\n{owner_context}\n元の作業指示:\n{original}\n\n{}",
        s(&question["cwd"]),
        s(&thread["id"]),
        crate::prompt_context::conversation(ctx, &thread, history, owner, 32 * 1024)?
    )))
}

fn remove(value: &mut Value, key: &str) {
    if let Some(object) = value.as_object_mut() {
        object.remove(key);
    }
}

fn resume_due(now: f64, question: &Value, history: &[Value], owner: &str, state: &Value) -> bool {
    history
        .iter()
        .find(|m| {
            s(&m["author"]["id"]) == owner
                && !crate::snooze::control_only(m)
                && n(&m["id"]) > n(&question["after"])
                && !reply_unavailable(state, s(&m["id"]))
        })
        .is_some_and(|m| now - snowflake_time(s(&m["id"])) >= RESUME_AFTER_S)
}

fn checkpoint(ctx: &Context, state: &mut Value, index: usize, question: &Value) -> Result<()> {
    state["questions"][index] = question.clone();
    ctx.save(state)
}

fn adopt_continuation(
    ctx: &mut Context,
    state: &mut Value,
    index: usize,
    question: &mut Value,
    jid: &str,
) -> Result<()> {
    question["continuation"]["selection"] = question["continuation"]["launch"]["selection"].clone();
    question["continuation"]["job"] = json!(jid);
    question["continuation"]["job_info"] = job_info(ctx, jid);
    remove(&mut question["continuation"], "launch");
    let after = s(&question["continuation"]["after"]).to_string();
    // The same forum reply may also be visible to intake. Reserve its delivery
    // once the question's child exists, independently of the receipt cursor.
    if let Some(ids) = question["continuation"]["owner_message_ids"].as_array() {
        for id in ids {
            record_launch_delivery(ctx, state, s(id), s(&question["thread_id"]), jid);
        }
    }
    if job_started(ctx, jid) {
        acknowledge_reply(question, &after);
        crate::reply_recovery::delivered(question);
    }
    checkpoint(ctx, state, index, question)?;
    Ok(())
}

fn continuation_selection(
    ctx: &mut Context,
    question: &Value,
    history: &[Value],
    owner: &str,
) -> Result<Value> {
    let mut source_job = if !s(&question["continuation"]["job"]).is_empty() {
        job_info(ctx, s(&question["continuation"]["job"]))
    } else {
        job_info(ctx, s(&question["job"]))
    };
    if s(&source_job["status"]) == "missing" && !s(&question["session_id"]).is_empty() {
        let session = s(&question["session_id"]);
        if let Some(job) = crate::automation::jobs(ctx)?
            .into_iter()
            .filter(|job| [s(&job["thread_id"]), s(&job["resume_session"])].contains(&session))
            .filter(|job| {
                s(&job["discord_thread_id"]).is_empty()
                    || explicit_thread_owner(job, s(&question["thread_id"]))
            })
            .max_by(|a, b| {
                crate::automation::epoch(&a["created_at"])
                    .total_cmp(&crate::automation::epoch(&b["created_at"]))
            })
        {
            source_job = job;
        }
        if s(&source_job["status"]) == "missing" {
            source_job = question.clone();
            source_job["thread_id"] = json!(session);
        }
    }
    let sources = [
        &question["continuation"]["launch"]["selection"],
        &source_job,
        &question["continuation"]["job_info"],
        &question["continuation"]["selection"],
        &question["job_info"],
        &question["selection"],
        question,
    ];
    let kind = sources
        .iter()
        .find_map(|source| source["task_type"].as_str().filter(|v| !v.is_empty()));
    // Route from the actual discussion, excluding generic resume instructions.
    let task = format!(
        "{}\n{}",
        s(&question["title"]),
        history
            .iter()
            .map(|m| s(&m["content"]))
            .collect::<Vec<_>>()
            .join("\n")
    );
    let mut selection = pick_model(ctx, &task, &[], kind)?;
    selection["source_job_id"] = source_job["id"].clone();
    for field in ["model", "effort"] {
        if let Some(saved) = sources
            .iter()
            .find_map(|source| source[field].as_str().filter(|value| !value.is_empty()))
        {
            selection[field] = json!(saved);
        }
    }
    let source = sources
        .iter()
        .find(|source| source["sandbox"].as_str().is_some_and(|v| !v.is_empty()))
        .copied()
        .unwrap_or(&Value::Null);
    let newest = history
        .iter()
        .rev()
        .find(|m| s(&m["author"]["id"]) == owner && n(&m["id"]) > n(&question["after"]));
    apply_job_scope(&mut selection, source, newest.map(|m| s(&m["content"])));
    selection["resume_session"] = sources
        .iter()
        .find_map(|source| {
            source["resume_session"]
                .as_str()
                .filter(|id| !id.is_empty())
                .or_else(|| {
                    source["thread_id"]
                        .as_str()
                        .filter(|id| !id.is_empty() && *id != s(&question["thread_id"]))
                })
        })
        .map(|id| json!(id))
        .unwrap_or(Value::Null);
    if crate::restart::session_busy(ctx, s(&selection["resume_session"]), "")? {
        selection["resume_session"] = Value::Null;
    }
    Ok(selection)
}

fn resume_at(
    ctx: &mut Context,
    state: &mut Value,
    index: usize,
    question: &mut Value,
    owner: &str,
    budget: &mut bool,
) -> Result<()> {
    let thread = json!({"id":question["thread_id"],"name":question["title"]});
    if crate::routing::thread_inbox_only(ctx, state, s(&question["thread_id"])) {
        return Ok(());
    }
    if !crate::routing::question_allowed(ctx, question)? {
        return Ok(());
    }
    let jid = s(&question["continuation"]["job"]).to_owned();
    if !jid.is_empty() && job_started(ctx, &jid) {
        let after = s(&question["continuation"]["after"]).to_owned();
        acknowledge_reply(question, &after);
        crate::restart::link_answer_todo(ctx, question, &jid)?;
        crate::reply_recovery::delivered(question);
        checkpoint(ctx, state, index, question)?;
    }
    crate::reply_recovery::refresh(state, question, s(&thread["id"]));
    if !crate::reply_recovery::due(question, ctx.now()) {
        if let Some(jid) = find_launch_job(ctx, s(&question["continuation"]["launch"]["key"])) {
            return adopt_continuation(ctx, state, index, question, &jid);
        }
        if !s(&question["continuation"]["owner_update"]["key"]).is_empty()
            && find_launch_job(ctx, s(&question["continuation"]["owner_update"]["key"])).is_some()
        {
            crate::steering::deliver(
                ctx,
                state,
                &thread,
                question,
                crate::restart::Slot::Question(index),
                owner,
                budget,
            )?;
        }
        return checkpoint(ctx, state, index, question);
    }
    let tid = s(&thread["id"]);
    if let Some(jid) = crate::restart::progress_successor(ctx, question)? {
        question["continuation"]["job"] = json!(jid);
        question["continuation"]["job_info"] = job_info(ctx, &jid);
        question["continuation"]["reported"] = json!(false);
        remove(question, "progress_job");
        checkpoint(ctx, state, index, question)?;
    }
    if !truth(&question["continuation"]["job"])
        && !truth(&question["continuation"]["launch"])
        && !owner_replies(ctx, question, owner)?
            .iter()
            .filter(|m| !crate::snooze::control_only(m))
            .filter(|m| !reply_unavailable(state, s(&m["id"])))
            .collect::<Vec<_>>()
            .is_empty()
    {
        if let Some(jid) = crate::steering::associated_job(ctx, question)? {
            // Attach only a worker explicitly associated with this Discord thread.
            question["continuation"] = json!({"job":jid,"job_info":job_info(ctx,&jid),"after":question["after"],"reported":false});
            question["continuation"]["answer_source_resolved"] = json!(true);
            checkpoint(ctx, state, index, question)?;
        } else if ["posted", "request"].contains(&s(&question["source"]))
            && s(&question["session_id"]).is_empty()
        {
            question["resume_status"] = json!("再開先なし");
            question["resume_status_reply"] =
                json!(crate::reply_recovery::pending_reply(state, question, tid));
            return checkpoint(ctx, state, index, question);
        }
    }
    if crate::steering::deliver(
        ctx,
        state,
        &thread,
        question,
        crate::restart::Slot::Question(index),
        owner,
        budget,
    )? {
        return Ok(());
    }
    if crate::snooze::paused(question, ctx.now()) {
        return Ok(());
    }
    if truth(&question["continuation"]["launch"]) {
        let key = s(&question["continuation"]["launch"]["key"]).to_string();
        if let Some(jid) = find_launch_job(ctx, &key) {
            return adopt_continuation(ctx, state, index, question, &jid);
        }
        let original = question["continuation"]["owner_message_ids"]
            .as_array()
            .cloned()
            .unwrap_or_default();
        if original.iter().any(|id| reply_reserved(state, s(id))) {
            return Ok(());
        }
        let remaining: Vec<_> = original
            .iter()
            .filter(|id| !reply_delivered(state, s(id)))
            .cloned()
            .collect();
        if !truth(&question["open"])
            || (!original.is_empty() && remaining.is_empty())
            || (original.is_empty()
                && reply_delivered(state, s(&question["continuation"]["after"])))
        {
            crate::restart::stop_todo(ctx, question)?;
            remove(question, "continuation");
            return Ok(());
        }
        let history: Vec<_> = thread_history(ctx, tid)?
            .into_iter()
            .filter(|m| {
                !(s(&m["author"]["id"]) == owner
                    && n(&m["id"]) > n(&question["after"])
                    && reply_unavailable(state, s(&m["id"])))
            })
            .collect();
        if remaining.len() != original.len() {
            question["continuation"]["owner_message_ids"] = json!(remaining);
            question["continuation"]["after"] = remaining
                .iter()
                .max_by_key(|id| n(id))
                .cloned()
                .unwrap_or_default();
            question["continuation"]["prompt"] =
                json!(continuation_prompt(ctx, question, &history, owner)?);
            checkpoint(ctx, state, index, question)?;
        }
        let Some(newest) = history.iter().rev().find(|m| {
            s(&m["author"]["id"]) == owner
                && n(&m["id"]) >= n(&question["continuation"]["after"])
                && !crate::snooze::control_only(m)
                && !reply_unavailable(state, s(&m["id"]))
        }) else {
            return Ok(());
        };
        if !resume_due(ctx.now(), question, &history, owner, state) {
            return Ok(());
        }
        if let Some(handled) = already_handled(
            ctx,
            question,
            &history,
            newest,
            &question["continuation"]["route"],
        )? {
            defer_reply(ctx, question, tid, newest, &handled)?;
            remove(question, "continuation");
            return Ok(());
        }
        if s(&question["continuation"]["job"]) == "launch-failed" {
            if n(&newest["id"]) > n(&question["continuation"]["after"]) {
                question["continuation"]["after"] = newest["id"].clone();
                question["continuation"]["prompt"] =
                    json!(continuation_prompt(ctx, question, &history, owner)?);
            }
            crate::restart::recover(
                ctx,
                state,
                &thread,
                question,
                crate::restart::Slot::Question(index),
                "launch-failed",
                owner,
                budget,
            )?;
            let jid = s(&question["continuation"]["job"]).to_string();
            if jid != "launch-failed" {
                return adopt_continuation(ctx, state, index, question, &jid);
            }
            return Ok(());
        }
        if *budget {
            return Ok(());
        }
        *budget = true;
        question["last_launch_attempt_at"] = json!(ctx.now());
        checkpoint(ctx, state, index, question)?;
        if n(&newest["id"]) > n(&question["continuation"]["after"]) {
            question["continuation"]["after"] = newest["id"].clone();
            question["continuation"]["owner_message_ids"] = json!(history
                .iter()
                .filter(|m| s(&m["author"]["id"]) == owner
                    && n(&m["id"]) > n(&question["after"])
                    && !crate::snooze::control_only(m)
                    && !reply_unavailable(state, s(&m["id"])))
                .map(|m| m["id"].clone())
                .collect::<Vec<_>>());
            let prompt = continuation_prompt(ctx, question, &history, owner)?;
            question["continuation"]["launch"]["selection"] =
                continuation_selection(ctx, question, &history, owner)?;
            question["continuation"]["prompt"] = json!(prompt);
            checkpoint(ctx, state, index, question)?;
        }
        question["continuation"]["cwd"] = json!(prepare_continuation_cwd(
            ctx,
            &question["continuation"]["route"],
            &key
        )?);
        checkpoint(ctx, state, index, question)?;
        if crate::restart::session_busy(
            ctx,
            s(&question["continuation"]["launch"]["selection"]["resume_session"]),
            "",
        )? {
            question["continuation"]["launch"]["selection"]["resume_session"] = Value::Null;
            checkpoint(ctx, state, index, question)?;
        }
        let entry = &question["continuation"];
        let launched = launch_job_with_session(
            ctx,
            &thread,
            s(&entry["prompt"]),
            &entry["launch"]["selection"],
            &key,
            false,
            Some(s(&entry["cwd"])),
            Some("resume"),
            entry["launch"]["selection"]["resume_session"].as_str(),
        );
        let jid = match launched {
            Ok(jid) => jid,
            Err(error) => {
                if let Some(jid) = find_launch_job(ctx, &key) {
                    jid
                } else {
                    eprintln!(
                        "answer launch failed {tid}: {}; retry next tick",
                        error_detail(&error)
                    );
                    if launch_blocked(&error) {
                        return checkpoint(ctx, state, index, question);
                    }
                    if crate::reply_recovery::transient(&error) {
                        return Err(error);
                    }
                    question["continuation"]["selection"] =
                        question["continuation"]["launch"]["selection"].clone();
                    question["continuation"]["job"] = json!("launch-failed");
                    let mut info = question["continuation"]["selection"].clone();
                    info["id"] = json!("launch-failed");
                    info["status"] = json!("missing");
                    info["cwd"] = question["continuation"]["cwd"].clone();
                    info["last_error"] = json!(safe_text(error));
                    question["continuation"]["job_info"] = info;
                    checkpoint(ctx, state, index, question)?;
                    return Ok(());
                }
            }
        };
        return adopt_continuation(ctx, state, index, question, &jid);
    }
    if truth(&question["continuation"]["acknowledgement"]) {
        post(
            ctx,
            tid,
            s(&question["continuation"]["acknowledgement"]),
            false,
        )?;
        remove(&mut question["continuation"], "acknowledgement");
        checkpoint(ctx, state, index, question)?;
    }
    if truth(&question["continuation"]["job"]) && !truth(&question["continuation"]["reported"]) {
        let jid = s(&question["continuation"]["job"]).to_string();
        let status = job_status(ctx, &jid);
        if RUNNING.contains(&status.as_str()) {
            return Ok(());
        }
        let result = final_message(ctx, &jid);
        if status != "succeeded" || result.trim().is_empty() {
            return crate::restart::recover(
                ctx,
                state,
                &thread,
                question,
                crate::restart::Slot::Question(index),
                &jid,
                owner,
                budget,
            );
        }
        let disposition = result_state(&result).to_string();
        log_result_misuse(&jid, &result);
        if disposition == "状態: 継続中" {
            crate::restart::continue_work(ctx, &thread, question, &jid)?;
            checkpoint(ctx, state, index, question)?;
        }
        if crate::steering::deliver(
            ctx,
            state,
            &thread,
            question,
            crate::restart::Slot::Question(index),
            owner,
            budget,
        )? {
            return Ok(());
        }
        let delivery = crate::result_delivery::post_with_cursor(
            ctx,
            tid,
            &format!(
                "{}{}\n\n{}",
                for_owner(&result),
                crate::steering::addressed(&question["continuation"]),
                result_footer(&jid, &disposition)
            ),
            disposition != "状態: 継続中",
            &jid,
            Some(s(&question["continuation"]["after"])),
        );
        // Routing APIs save new questions/routes. Reload before checkpointing this
        // origin generation so its snapshot cannot erase the newly registered one.
        *state = ctx.load()?;
        let after = match delivery? {
            crate::result_delivery::Delivery::Sent(after) => after,
            crate::result_delivery::Delivery::OwnerMessagesPending => {
                crate::steering::deliver(
                    ctx,
                    state,
                    &thread,
                    question,
                    crate::restart::Slot::Question(index),
                    owner,
                    budget,
                )?;
                return Ok(());
            }
            crate::result_delivery::Delivery::PreparationFailed(error) => {
                question["attachment_error"] = json!(error);
                checkpoint(ctx, state, index, question)?;
                return crate::restart::recover(
                    ctx,
                    state,
                    &thread,
                    question,
                    crate::restart::Slot::Question(index),
                    &jid,
                    owner,
                    budget,
                );
            }
        };
        question.as_object_mut().unwrap().remove("attachment_error");
        if disposition == "状態: 完了" {
            crate::restart::finish_todo(ctx, question)?;
        }
        let separated_question =
            disposition == "状態: 要判断" && crate::question_progress::is_separated(ctx, tid)?;
        let is_question_thread = truth(&question["separated_question"])
            || crate::question_progress::route(ctx, tid)?
                .is_some_and(|r| s(&r["question_thread_id"]) == tid);
        question["open"] = json!(disposition != "状態: 継続中");
        let entry = &mut question["continuation"];
        entry["reported"] = json!(true);
        entry["result_after"] = json!(after);
        entry["close_pending"] =
            json!(disposition == "状態: 完了" || is_question_thread || separated_question);
        entry["question_pending"] = json!(disposition == "状態: 要判断" && !separated_question);
        checkpoint(ctx, state, index, question)?;
    }
    if truth(&question["continuation"]["question_pending"]) {
        let entry = &question["continuation"];
        let generation = crate::questions::result_generation(ctx, tid, s(&entry["job"]))?;
        let pending =
            pending_owner_messages_for_job(ctx, tid, s(&entry["after"]), owner, s(&entry["job"]))?;
        question["after"] = if !pending.is_empty() {
            entry["after"].clone()
        } else if truth(&entry["result_after"]) {
            if truth(&generation["first"]) {
                generation["first"].clone()
            } else {
                entry["result_after"].clone()
            }
        } else {
            question["after"].clone()
        };
        question["question_message"] = generation["first"].clone();
        question["attachment_hashes"] = generation["attachment_hashes"].clone();
        question["asked_at"] = json!(timestamp());
        remove(question, "last_ping");
        remove(&mut question["continuation"], "question_pending");
        checkpoint(ctx, state, index, question)?;
        return Ok(());
    }
    if truth(&question["continuation"]["close_pending"]) {
        if archive_completed(ctx, &thread, &mut question["continuation"], owner)? {
            question["open"] = json!(false);
        }
        checkpoint(ctx, state, index, question)?;
        return Ok(());
    }
    if !truth(&question["open"]) {
        return Ok(());
    }
    let history = thread_history(ctx, tid)?;
    let Some(newest) = history.iter().rev().find(|m| {
        s(&m["author"]["id"]) == owner
            && n(&m["id"]) > n(&question["after"])
            && !crate::snooze::control_only(m)
            && !reply_unavailable(state, s(&m["id"]))
    }) else {
        return Ok(());
    };
    if !resume_due(ctx.now(), question, &history, owner, state) {
        return Ok(());
    }
    if !truth(&question["cwd"]) {
        question["cwd"] = json!(crate::project::infer_question_cwd(ctx, state, question));
        question["cwd_inferred"] = json!(true);
        checkpoint(ctx, state, index, question)?;
    }
    let route = continuation_route(ctx, s(&question["cwd"]))?;
    if let Some(handled) = already_handled(ctx, question, &history, newest, &route)? {
        defer_reply(ctx, question, tid, newest, &handled)?;
        return Ok(());
    }
    if *budget {
        eprintln!("owner reply deferred {tid}: tick launch budget used; retry next tick");
        return Ok(());
    }
    let prompt = continuation_prompt(ctx, question, &history, owner)?;
    let selection = continuation_selection(ctx, question, &history, owner)?;
    let owner_message_ids: Vec<_> = history
        .iter()
        .filter(|m| {
            s(&m["author"]["id"]) == owner
                && n(&m["id"]) > n(&question["after"])
                && !crate::snooze::control_only(m)
                && !reply_unavailable(state, s(&m["id"]))
        })
        .map(|m| m["id"].clone())
        .collect();
    question["continuation"] = json!({"after":newest["id"],"owner_message_ids":owner_message_ids,"route":route,"prompt":safe_text(&prompt),"launch":{"key":uuid::Uuid::new_v4().simple().to_string(),"selection":selection}});
    checkpoint(ctx, state, index, question)?;
    resume_at(ctx, state, index, question, owner, budget)
}

/// Waiting for a live worker is receipt, not proof that it applied the owner's answer.
fn defer_reply(
    ctx: &mut Context,
    question: &mut Value,
    tid: &str,
    newest: &Value,
    handled: &str,
) -> Result<()> {
    if question["deferred_reply"] != newest["id"] {
        let content = safe_text(format!(
            "元の作業は別セッションが担当しています。担当が終わった後に回答の反映を確認します。\n{handled}"
        ));
        let mid = s(&newest["id"]);
        let target = crate::question_progress::result_target(ctx, tid, "")?;
        let nonce = crate::automation::key(&format!("deferred|{tid}|{mid}"));
        if !thread_history(ctx, &target)?.iter().any(|m| {
            (s(&m["message_reference"]["message_id"]) == mid || s(&m["nonce"]) == &nonce[..24])
                && s(&m["content"]) == content
                && truth(&m["author"]["bot"])
        }) {
            let mut payload = json!({"content":crate::question_progress::silent_text(&content),"flags":SILENT|4,"nonce":&nonce[..24],"enforce_nonce":true,"allowed_mentions":{"parse":[],"users":[],"replied_user":false}});
            if target == tid {
                payload["message_reference"] = json!({"message_id":mid,"channel_id":tid});
            }
            ctx.call(
                "POST",
                &format!("/channels/{target}/messages"),
                Some(&payload),
            )?;
        }
        question["deferred_reply"] = newest["id"].clone();
    }
    Ok(())
}

pub fn resume_question(
    ctx: &mut Context,
    state: &mut Value,
    question: &mut Value,
    owner: &str,
    budget: &mut bool,
) -> Result<()> {
    let index = state["questions"]
        .as_array()
        .ok_or("missing questions")?
        .iter()
        .position(|q| q == question)
        .ok_or("question not in state")?;
    let result = resume_at(ctx, state, index, question, owner, budget);
    checkpoint(ctx, state, index, question)?;
    result
}

/// Resume replies with serialized decisions and short fresh-state checkpoints.
pub fn cmd_resume(ctx: &mut Context, budget: &mut bool) -> Result<()> {
    let _work = crate::state_work::StateWork::begin(ctx)?;
    let owner = ctx.owner();
    let mut state = ctx.load()?;
    recover_reply_reads(ctx, &mut state)?;
    migrate_reply_delivery(ctx, &mut state)?;
    let length = state["questions"]
        .as_array()
        .ok_or("missing questions")?
        .len();
    let mut indices: Vec<_> = (0..length).collect();
    // A broken old worktree must not take the only launch slot on every tick.
    indices.sort_by(|a, b| {
        state["questions"][*a]["last_launch_attempt_at"]
            .as_f64()
            .unwrap_or(0.)
            .total_cmp(
                &state["questions"][*b]["last_launch_attempt_at"]
                    .as_f64()
                    .unwrap_or(0.),
            )
    });
    if let Some(cursor) = state["resume_scan_after"].as_u64() {
        if let Some(at) = indices.iter().position(|i| *i as u64 == cursor) {
            let length = indices.len();
            indices.rotate_left((at + 1) % length.max(1));
        }
    }
    for index in indices {
        if crate::tick_runtime::check().is_err() {
            break;
        }
        state["resume_scan_after"] = json!(index);
        let mut question = state["questions"][index].clone();
        if !(truth(&question["open"])
            || (truth(&question["continuation"])
                && (!truth(&question["continuation"]["reported"])
                    || truth(&question["continuation"]["close_pending"])
                    || truth(&question["continuation"]["question_pending"])
                    || truth(&question["progress_job"]))))
        {
            continue;
        }
        let _span =
            crate::tick_runtime::Span::new(format!("resume thread {}", s(&question["thread_id"])));
        match crate::routing::question_allowed(ctx, &question) {
            Ok(true) => (),
            Ok(false) => continue,
            Err(error) => {
                eprintln!("question scope unavailable: {}", error_detail(&error));
                continue;
            }
        }
        // Main-session answers still need durable receipt and visibility, but
        // must never trigger acknowledgements or worker launches.
        if crate::routing::thread_inbox_only(ctx, &state, s(&question["thread_id"])) {
            if truth(&question["open"]) {
                match owner_replies(ctx, &question, &owner) {
                    Ok(messages) => {
                        record_owner_answers(
                            &mut state,
                            s(&question["thread_id"]),
                            &messages,
                            &owner,
                        );
                        ctx.save(&state)?;
                    }
                    Err(error) => eprintln!("main answer receipt failed: {}", error_detail(&error)),
                }
            }
            continue;
        }
        if truth(&question["open"]) {
            let received = owner_replies(ctx, &question, &owner).and_then(|messages| {
                crate::steering::confirm_recorded_reads(ctx, &mut state, &question, &messages)?;
                receive_answers(ctx, &mut state, index, &messages)
            });
            if let Err(error) = received {
                eprintln!(
                    "answer receipt failed {} {}",
                    s(&question["thread_id"]),
                    error_detail(&error)
                );
                continue;
            }
        }
        // Questions emitted by intake's own result still use its worker handoff.
        // Orphaned/posted request questions must reach the source resolver below.
        if s(&question["source"]) == "request"
            && !truth(&question["continuation"])
            && (s(&question["question_message"]).is_empty()
                || (valid_job(s(&state["requests"][s(&question["thread_id"])]["job"]))
                    && n(&state["requests"][s(&question["thread_id"])]["result_after"])
                        >= n(&question["question_message"])
                    && !truth(&state["requests"][s(&question["thread_id"])]["recovered_question"])))
        {
            continue;
        }
        if s(&question["source"]) == "scheduler"
            && !truth(&question["continuation"]["job"])
            && crate::steering::associated_job(ctx, &question)?.is_none()
        {
            continue;
        }
        // Receipt may have stored a new postponement; do not overwrite it with the old clone.
        question = state["questions"][index].clone();
        if let Err(error) = resume_at(ctx, &mut state, index, &mut question, &owner, budget) {
            if crate::reply_recovery::transient(&error)
                && crate::reply_recovery::undelivered(&state, &question)
            {
                let reply = crate::reply_recovery::pending_reply(
                    &state,
                    &question,
                    s(&question["thread_id"]),
                );
                crate::reply_recovery::failed(&mut question, ctx.now(), &error, &reply);
            } else {
                question["last_resume_error"] = json!(error_detail(&error));
            }
            eprintln!(
                "answer resume failed {} {}",
                s(&question["thread_id"]),
                error_detail(&error)
            );
        } else if !truth(&question["resume_status"]) {
            remove(&mut question, "last_resume_error");
        }
        checkpoint(ctx, &mut state, index, &question)?;
    }
    ctx.save(&state)?;
    Ok(())
}
