//! Bounded, silent recovery shared by request intake and question continuations.
use crate::core::*;
use crate::recovery_failure::{self as failure, Failure};
use serde_json::{json, Value};
use std::{fs, time::Duration};

pub const WINDOW_S: f64 = 6. * 3600.;
pub const RETRY_DELAY_S: f64 = 60.;
pub const MAX_RESTARTS: usize = 2;

#[derive(Clone, Copy)]
pub enum Slot {
    Question(usize),
    Request,
}

fn save(
    ctx: &Context,
    state: &mut Value,
    thread: &Value,
    record: &Value,
    slot: Slot,
) -> Result<()> {
    match slot {
        Slot::Question(i) => state["questions"][i] = record.clone(),
        Slot::Request => state["requests"][s(&thread["id"])] = record.clone(),
    }
    ctx.save(state)
}

fn jobs(ctx: &Context) -> Result<Vec<Value>> {
    let root = ctx.jobs.join("jobs");
    if !root.exists() {
        return Ok(vec![]);
    }
    let mut out = vec![];
    for dir in fs::read_dir(root).map_err(|e| e.to_string())? {
        let dir = dir.map_err(|e| e.to_string())?;
        let file = dir.path().join("job.json");
        if !file.is_file() {
            continue;
        }
        let mut job = read_json(&file)?;
        if !job.is_object() {
            return Err("invalid job record".into());
        }
        job["id"] = json!(dir.file_name().to_string_lossy());
        out.push(job);
    }
    Ok(out)
}

fn links(item: &Value, job: &Value) -> bool {
    (item["id"].as_i64().is_some()
        && (job["todo_id"] == item["id"]
            || job["related_todo_ids"]
                .as_array()
                .is_some_and(|ids| ids.contains(&item["id"]))))
        || item["jobs"]
            .as_array()
            .into_iter()
            .flatten()
            .any(|id| id == &job["id"] || (truth(&job["thread_id"]) && id == &job["thread_id"]))
        || (truth(&item["scheduler"]["job"]) && item["scheduler"]["job"] == job["id"])
}

pub fn session_busy(ctx: &Context, session: &str, exclude: &str) -> Result<bool> {
    if session.is_empty() {
        return Ok(false);
    }
    Ok(jobs(ctx)?.iter().any(|job| {
        s(&job["id"]) != exclude
            && RUNNING.contains(&s(&job["status"]))
            && [s(&job["thread_id"]), s(&job["resume_session"])].contains(&session)
    }))
}

/// Link the existing task only after its answer worker actually started.
pub fn link_answer_todo(ctx: &mut Context, question: &mut Value, jid: &str) -> Result<()> {
    if s(&question["continuation"]["todo_linked_job"]) == jid || !ctx.todo.is_file() {
        return Ok(());
    }
    let db = read_json(&ctx.todo)?;
    let source = job_info(ctx, s(&question["job"]));
    let session = s(&question["session_id"]);
    for item in db["items"].as_array().into_iter().flatten() {
        let associated = links(item, &source)
            || item["id"] == question["restart"]["todo"]
            || item["jobs"].as_array().into_iter().flatten().any(|id| {
                s(id) == jid
                    || (!s(&question["continuation"]["selection"]["source_job_id"]).is_empty()
                        && id == &question["continuation"]["selection"]["source_job_id"])
            })
            || item["jobs"].as_array().into_iter().flatten().any(|id| {
                !session.is_empty()
                    && (s(id) == session || {
                        let job = job_info(ctx, s(id));
                        [s(&job["thread_id"]), s(&job["resume_session"])].contains(&session)
                    })
            });
        let newer_block = s(&item["status"]) == "block"
            && crate::automation::epoch(&item["updated"])
                > snowflake_time(s(&question["continuation"]["after"]));
        if associated && !newer_block && ["todo", "doing", "block"].contains(&s(&item["status"])) {
            todo_run(
                ctx,
                &[
                    "start".into(),
                    n(&item["id"]).to_string(),
                    "--job".into(),
                    jid.into(),
                ],
            )?;
        }
    }
    question["continuation"]["todo_linked_job"] = json!(jid);
    Ok(())
}

/// Direct Discord/session association and shared ToDo ownership take precedence over recovery.
pub fn other_owner(
    ctx: &Context,
    thread: &Value,
    source: &Value,
    todo: &Value,
) -> Result<Option<String>> {
    if let Some(item) = todo["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|item| links(item, source) && item["stall_recovery"]["pending"].is_object())
    {
        return Ok(Some(format!("reserved-{}", n(&item["id"]))));
    }
    for job in jobs(ctx)? {
        if job["id"] == source["id"] || !RUNNING.contains(&s(&job["status"])) {
            continue;
        }
        let shared_todo = todo["items"]
            .as_array()
            .into_iter()
            .flatten()
            .any(|item| links(item, source) && links(item, &job));
        let prompt_path = ctx.jobs.join("jobs").join(s(&job["id"])).join("prompt.md");
        let prompt = if prompt_path.is_file() {
            read_text(&prompt_path)?
        } else {
            String::new()
        };
        if shared_todo
            || (thread_owner(&job_info(ctx, s(&job["id"])), &prompt, s(&thread["id"]))
                && !(RUNNING.contains(&s(&source["status"]))
                    && explicit_thread_owner(source, s(&thread["id"]))
                    && !explicit_thread_owner(&job_info(ctx, s(&job["id"])), s(&thread["id"]))))
        {
            return Ok(Some(s(&job["id"]).into()));
        }
    }
    Ok(None)
}

pub fn deliberate_cancel(info: &Value) -> bool {
    ["cancelled", "lost"].contains(&s(&info["status"]))
        && (truth(&info["cancel_requested"])
            || ["manual", "person", "duplicate", "superseded"].contains(&s(&info["cancel_reason"])))
}

fn todo_run(ctx: &mut Context, args: &[String]) -> Result<String> {
    let out = ctx.run(&command_argv(&ctx.todo_command, args), 45)?;
    if out.code != 0 {
        return Err(format!("todo exited {}", out.code));
    }
    Ok(out.stdout)
}

fn ensure_todo(
    ctx: &mut Context,
    thread: &Value,
    source: &Value,
    record: &mut Value,
    db: &Value,
) -> Result<()> {
    if truth(&record["restart"]["todo"]) {
        if truth(&record["restart"]["todo_done"]) {
            link_todo(ctx, record, s(&source["id"]))?;
            record["restart"]["todo_done"] = json!(false);
        }
        return Ok(());
    }
    let marker = format!("Discordの作業再開: {}", s(&thread["id"]));
    if let Some(item) = db["items"].as_array().into_iter().flatten().find(|item| {
        ["todo", "doing", "block"].contains(&s(&item["status"]))
            && (links(item, source)
                || links(item, &job_info(ctx, s(&record["job"])))
                || s(&item["note"]).contains(&marker))
    }) {
        record["restart"]["todo"] = item["id"].clone();
        if s(&item["note"]).contains(&marker) {
            record["restart"]["owns_todo"] = json!(true);
        }
    } else {
        let project = db["projects"]
            .as_object()
            .into_iter()
            .flatten()
            .find(|(_, path)| crate::resume::in_project(s(&source["cwd"]), s(path)))
            .map(|(name, _)| name.clone())
            .unwrap_or_else(|| "tools".into());
        if !truth(&db["projects"][&project]) {
            todo_run(
                ctx,
                &["project".into(), project.clone(), "D:/CC/Tools".into()],
            )?;
        }
        let out = todo_run(
            ctx,
            &[
                "add".into(),
                project,
                format!(
                    "Discordで受けた作業「{}」を続きから再開する",
                    safe_text(s(&thread["name"]))
                ),
                "--note".into(),
                format!("{marker}。停止した作業を停滞の見張りが再試行します。"),
            ],
        )?;
        let id = out
            .split_whitespace()
            .last()
            .and_then(|v| v.parse::<u64>().ok())
            .ok_or("todo add returned no item ID")?;
        record["restart"]["todo"] = json!(id);
        record["restart"]["owns_todo"] = json!(true);
    }
    if db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .any(|item| item["id"] == record["restart"]["todo"] && links(item, source))
    {
        return Ok(());
    }
    link_todo(ctx, record, s(&source["id"]))
}

fn link_todo(ctx: &mut Context, record: &Value, jid: &str) -> Result<()> {
    if !job_started(ctx, jid) {
        return Ok(());
    }
    let id = n(&record["restart"]["todo"]);
    if id != 0 && valid_job(jid) {
        todo_run(
            ctx,
            &["start".into(), id.to_string(), "--job".into(), jid.into()],
        )?;
    }
    Ok(())
}

/// Keep unfinished successful jobs dispatchable by the existing stall watchdog.
pub fn continue_work(
    ctx: &mut Context,
    thread: &Value,
    record: &mut Value,
    jid: &str,
) -> Result<()> {
    let mut source = job_info(ctx, jid);
    source["id"] = json!(jid);
    let db = if ctx.todo.is_file() {
        read_json(&ctx.todo)?
    } else {
        json!({})
    };
    ensure_todo(ctx, thread, &source, record, &db)?;
    record["progress_job"] = json!(jid);
    Ok(())
}

/// Reattach the watchdog's successor so its eventual result returns to the original thread.
pub fn progress_successor(ctx: &Context, record: &Value) -> Result<Option<String>> {
    if !truth(&record["progress_job"]) || !ctx.todo.is_file() {
        return Ok(None);
    }
    let db = read_json(&ctx.todo)?;
    let Some(item) = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|i| i["id"] == record["restart"]["todo"])
    else {
        return Ok(None);
    };
    let Some(ids) = item["jobs"].as_array() else {
        return Ok(None);
    };
    let source = job_info(ctx, s(&record["progress_job"]));
    let Some(offset) = ids.iter().position(|id| {
        s(id) == s(&record["progress_job"])
            || (truth(&source["thread_id"]) && id == &source["thread_id"])
    }) else {
        return Ok(None);
    };
    for id in ids.iter().skip(offset + 1).rev() {
        if s(id) == s(&record["progress_job"]) {
            continue;
        }
        if valid_job(s(id)) && !["missing", ""].contains(&s(&job_info(ctx, s(id))["status"])) {
            return Ok(Some(s(id).into()));
        }
    }
    Ok(None)
}

pub fn finish_todo(ctx: &mut Context, record: &mut Value) -> Result<()> {
    end_todo(ctx, record, "Discordで受けた作業の完了報告を確認しました。")
}
pub fn stop_todo(ctx: &mut Context, record: &mut Value) -> Result<()> {
    end_todo(
        ctx,
        record,
        "質問または自動再開を明示的に終了したため、この項目を閉じました。",
    )
}

fn end_todo(ctx: &mut Context, record: &mut Value, note: &str) -> Result<()> {
    if truth(&record["restart"]["owns_todo"]) && !truth(&record["restart"]["todo_done"]) {
        todo_run(
            ctx,
            &[
                "done".into(),
                n(&record["restart"]["todo"]).to_string(),
                "--note".into(),
                note.into(),
            ],
        )?;
        record["restart"]["todo_done"] = json!(true);
    }
    Ok(())
}

fn adopt(
    ctx: &Context,
    state: &mut Value,
    thread: &Value,
    record: &mut Value,
    jid: &str,
    slot: Slot,
) {
    let pending = record["restart"]["pending"].clone();
    match slot {
        Slot::Question(_) => {
            record["continuation"]["job"] = json!(jid);
            if n(&pending["after"]) > n(&record["continuation"]["after"]) {
                record["continuation"]["after"] = pending["after"].clone();
                if job_started(ctx, jid) {
                    acknowledge_reply(record, s(&pending["after"]));
                }
            }
        }
        Slot::Request => {
            record["job"] = json!(jid);
            if n(&pending["after"]) > n(&record["after"]) {
                record["after"] = pending["after"].clone();
            }
            record.as_object_mut().unwrap().remove("launch");
        }
    }
    for id in pending["owner_message_ids"]
        .as_array()
        .into_iter()
        .flatten()
    {
        record_launch_delivery(ctx, state, s(id), s(&thread["id"]), jid);
    }
    record["restart"].as_object_mut().unwrap().remove("pending");
    for field in [
        "last_error",
        "failed_source",
        "last_selection",
        "last_prompt",
    ] {
        record["restart"].as_object_mut().unwrap().remove(field);
    }
}

fn attempts(record: &Value, db: &Value, now: f64) -> Vec<Value> {
    let item = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|item| item["id"] == record["restart"]["todo"]);
    let external = item.map(|item| {
        if item["discord_restart_history"].is_array() {
            &item["discord_restart_history"]
        } else {
            &item["stall_recovery"]["history"]
        }
    });
    let mut attempts: Vec<Value> = record["restart"]["attempts"]
        .as_array()
        .into_iter()
        .flatten()
        .chain(
            external
                .into_iter()
                .flat_map(|history| history.as_array().into_iter().flatten()),
        )
        .filter(|at| at.as_f64().is_some_and(|at| now - at < WINDOW_S))
        .cloned()
        .collect();
    attempts.sort_by(|a, b| a.as_f64().unwrap().total_cmp(&b.as_f64().unwrap()));
    attempts.dedup_by(|a, b| a.as_f64() == b.as_f64());
    attempts
}

fn share_attempts(ctx: &Context, thread: &Value, record: &Value, history: &[Value]) -> Result<()> {
    let lock = ctx
        .todo
        .parent()
        .ok_or("missing ToDo directory")?
        .join("todo.lock");
    let _lock = ByteLock::acquire(&lock, Duration::from_secs(45))?;
    let mut db = read_json(&ctx.todo)?;
    let item = db["items"]
        .as_array_mut()
        .ok_or("missing ToDo items")?
        .iter_mut()
        .find(|item| item["id"] == record["restart"]["todo"])
        .ok_or("recovery ToDo missing")?;
    item["discord_restart_thread"] = thread["id"].clone();
    item["discord_restart_history"] = json!(history);
    item["discord_restart_not_before"] = json!(failure::retry_at(history, ctx.now()));
    if !item["stall_recovery"].is_object() {
        item["stall_recovery"] = json!({});
    }
    item["stall_recovery"]["next_at"] = json!(item["stall_recovery"]["next_at"]
        .as_f64()
        .unwrap_or(0.)
        .max(record["restart"]["next_at"].as_f64().unwrap_or(0.)));
    if truth(&record["restart"]["notice_sent"]) {
        if !item["stall_recovery"].is_object() {
            item["stall_recovery"] = json!({});
        }
        item["stall_recovery"]["notice_sent"] = json!(true);
    }
    atomic_json(&ctx.todo, &db, 2)
}

#[allow(clippy::too_many_arguments)]
fn recovery_prompt(
    ctx: &mut Context,
    thread: &Value,
    record: &Value,
    slot: Slot,
    source: &Value,
    jid: &str,
    owner: &str,
    key: &str,
) -> Result<Value> {
    let bytes = crate::prompt_context::limit(source);
    let history = thread_history(ctx, s(&thread["id"]))?;
    let owner_message_ids: Vec<_> = history
        .iter()
        .filter(|m| s(&m["author"]["id"]) == owner && !crate::snooze::control_only(m))
        .map(|m| m["id"].clone())
        .collect();
    let after = owner_message_ids
        .iter()
        .max_by_key(|id| n(id))
        .cloned()
        .unwrap_or(Value::Null);
    let latest: Vec<_> = history
        .iter()
        .rev()
        .filter(|m| s(&m["author"]["id"]) == owner && !crate::snooze::control_only(m))
        .take(3)
        .cloned()
        .collect::<Vec<_>>()
        .into_iter()
        .rev()
        .collect();
    let latest =
        crate::prompt_context::conversation(ctx, thread, &latest, owner, (bytes / 10).min(1024))?;
    let preamble = match slot {
        Slot::Question(_) => RESUME_PREAMBLE,
        Slot::Request => REQUEST_PREAMBLE,
    };
    let prefix = format!("前回のジョブが止まったので続きから再開してください。\n最新のオーナーの回答・依頼:\n{latest}\n{preamble}\nスレッドID: {}\n作業フォルダ: {}\n原因: {}\n",
        s(&thread["id"]), s(&source["cwd"]), s(&record["restart"]["cause"]));
    // Leave space for headings, omission references and the small scoped ToDo.
    let available = bytes.saturating_sub(prefix.len() + 1536);
    let history = crate::prompt_context::conversation(ctx, thread, &history, owner, available / 2)?;
    let path = ctx.jobs.join("jobs").join(jid).join("prompt.md");
    let original = if path.is_file() {
        read_text(&path)?
    } else if truth(&record["restart"]["last_prompt"]) {
        s(&record["restart"]["last_prompt"]).into()
    } else {
        match slot {
            Slot::Question(_) => s(&record["continuation"]["prompt"]).into(),
            Slot::Request => conversation(
                thread,
                &record["request_messages"]
                    .as_array()
                    .cloned()
                    .unwrap_or_default(),
                owner,
            ),
        }
    };
    let original = crate::prompt_context::source(ctx, key, &original, &path, available / 3)?;
    let last = crate::prompt_context::bounded(
        &final_message(ctx, jid),
        available / 6,
        &ctx.jobs
            .join("jobs")
            .join(jid)
            .join("last_message.md")
            .to_string_lossy(),
    );
    let mut prompt = format!("{prefix}\nオーナーの回答・依頼を含むこのスレッドの会話:\n{history}\n元の作業指示:\n{original}\n前回の結果:\n{last}");
    if !truth(&source["thread_id"]) && !truth(&source["resume_session"]) {
        prompt.push_str("\n新しいセッションで再開します。\n");
    }
    if truth(&record["attachment_error"]) {
        prompt.push_str(&format!("\n最終報告の添付を受付で送れませんでした。理由: {}\n指定した実ファイルと添付の書式・容量を修復し、再び最終報告で渡してください。オーナーの判断待ちにはしない。",
            safe_text(s(&record["attachment_error"]))));
    }
    let prompt = crate::prompt_context::prepare(
        ctx,
        thread,
        &safe_text(prompt),
        source,
        s(&source["cwd"]),
        key,
    )?;
    Ok(json!({"prompt":prompt,"after":after,"owner_message_ids":owner_message_ids}))
}

/// Persist reservations before effects; ambiguous/lost launcher responses are adopted by launch key.
#[allow(clippy::too_many_arguments)]
pub fn recover(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    record: &mut Value,
    slot: Slot,
    jid: &str,
    owner: &str,
    budget: &mut bool,
) -> Result<()> {
    if truth(&record["recovery_closed"]) {
        return stop_todo(ctx, record);
    }
    let scheduler_root = ctx
        .scheduler_command
        .parent()
        .ok_or("missing scheduler directory")?;
    let Ok(_launch_lock) =
        ByteLock::acquire(&scheduler_root.join("scheduler.lock"), Duration::ZERO)
    else {
        return Ok(());
    };
    if !record["restart"].is_object() {
        record["restart"] = json!({});
    }
    let job_dir = ctx.jobs.join("jobs").join(jid);
    // A lost supervisor can leave a live child. The jobs tool validates PID
    // creation times and verifies termination before allowing another session.
    if s(&job_info(ctx, jid)["status"]) == "lost" && !deliberate_cancel(&job_info(ctx, jid)) {
        let out = ctx.run(
            &command_argv(
                &ctx.codex_command,
                &["cancel".into(), jid.into(), "--recovery".into()],
            ),
            45,
        )?;
        if out.code != 0 {
            return Err("could not retire lost job".into());
        }
    }
    let _source_lock = if job_dir.is_dir() {
        Some(ByteLock::acquire(
            &job_dir.join("state.lock"),
            Duration::from_secs(45),
        )?)
    } else {
        None
    };
    let mut source = job_info(ctx, jid);
    if s(&source["status"]) == "missing" {
        let stored = match slot {
            Slot::Question(_) => &record["continuation"]["job_info"],
            Slot::Request => &record["job_info"],
        };
        if stored.is_object() && s(&stored["id"]) == jid {
            source = stored.clone();
            source["status"] = json!("missing");
        }
    }
    source["id"] = json!(jid);
    let ownership_source = source.clone();
    let previous_error = if s(&record["restart"]["failed_source"]) == jid {
        s(&record["restart"]["last_error"])
    } else {
        ""
    };
    let evidence = format!(
        "{}\n{}",
        previous_error,
        failure::evidence(&source, &job_dir)
    );
    if !previous_error.is_empty() && truth(&record["restart"]["last_selection"]["model"]) {
        for field in [
            "model",
            "effort",
            "thread_id",
            "resume_session",
            "prompt_limit_bytes",
        ] {
            if let Some(value) = record["restart"]["last_selection"].get(field) {
                source[field] = value.clone();
            }
        }
    }
    let mut class = if failure::classify(previous_error) != Failure::Task {
        failure::classify(previous_error)
    } else {
        failure::classify(&evidence)
    };
    if class == Failure::Task
        && s(&source["status"]) != "cancelled"
        && session_busy(
            ctx,
            if truth(&source["thread_id"]) {
                s(&source["thread_id"])
            } else {
                s(&source["resume_session"])
            },
            jid,
        )?
    {
        class = Failure::Writer;
    }
    source = failure::repaired_job(&source, class);
    if class == Failure::Context {
        crate::prompt_context::reduce(&mut source);
    }
    record["restart"]["failure"] = json!(class.name());
    record["restart"]["cause"] = json!(failure::summary(&evidence));
    let db = if ctx.todo.is_file() {
        read_json(&ctx.todo)?
    } else {
        json!({})
    };
    // Adopt our reservation before checking whether its own job is a competing owner.
    let key = s(&record["restart"]["pending"]["key"]).to_string();
    if !key.is_empty() {
        if let Some(found) = find_launch_job(ctx, &key) {
            link_todo(ctx, record, &found)?;
            adopt(ctx, state, thread, record, &found, slot);
            match slot {
                Slot::Question(_) => record["continuation"]["job_info"] = job_info(ctx, &found),
                Slot::Request => record["job_info"] = job_info(ctx, &found),
            }
            return save(ctx, state, thread, record, slot);
        }
    }
    let other = other_owner(ctx, thread, &ownership_source, &db)?;
    let waiting = s(&record["restart"]["waiting_for"]).to_owned();
    let successor = job_info(ctx, &waiting);
    let transferred = !waiting.is_empty()
        && (s(&successor["status"]) == "succeeded"
            || (deliberate_cancel(&successor)
                && ["superseded", "duplicate"].contains(&s(&successor["cancel_reason"])))
            || waiting
                .strip_prefix("reserved-")
                .and_then(|id| id.parse::<u64>().ok())
                .is_some_and(|id| {
                    db["items"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .any(|item| n(&item["id"]) == id && s(&item["status"]) == "done")
                }));
    if transferred {
        // Only messages available when it started could have been delivered.
        // Later replies follow up with that worker's session, not its old task.
        let cutoff = crate::automation::epoch(&successor["started_at"])
            .max(crate::automation::epoch(&successor["created_at"]));
        let cursor = match slot {
            Slot::Question(_) => n(&record["continuation"]["after"]),
            Slot::Request => n(&record["after"]),
        };
        let history = thread_history(ctx, s(&thread["id"]))?;
        let input_path = ctx.jobs.join("jobs").join(&waiting).join("prompt.md");
        let input = if input_path.is_file() {
            read_text(&input_path)?
        } else {
            String::new()
        };
        let delivered: Vec<_> = history
            .iter()
            .filter(|m| {
                s(&m["author"]["id"]) == owner
                    && !crate::snooze::control_only(m)
                    && job_started(ctx, &waiting)
                    && (s(&state["answer_receipts"][s(&m["id"])]["handled_by"]) == waiting
                        || (input.contains(&format!("オーナー投稿 {}", s(&m["id"])))
                            && !s(&m["content"]).trim().is_empty()
                            && input.contains(s(&m["content"]))))
                    && ((cutoff == 0. && n(&m["id"]) <= cursor)
                        || (cutoff > 0. && snowflake_time(s(&m["id"])) <= cutoff))
            })
            .map(|m| m["id"].clone())
            .collect();
        let after = delivered
            .iter()
            .max_by_key(|id| n(id))
            .cloned()
            .unwrap_or(Value::Null);
        let baseline = match slot {
            Slot::Question(_) => n(&record["after"]),
            Slot::Request => cursor.saturating_sub(1),
        };
        let remaining: Vec<_> = history
            .iter()
            .filter(|m| {
                s(&m["author"]["id"]) == owner
                    && !crate::snooze::control_only(m)
                    && n(&m["id"]) > baseline
                    && !delivered.contains(&m["id"])
                    && (!truth(&state["answer_receipts"][s(&m["id"])]["handled_by"])
                        || s(&state["answer_receipts"][s(&m["id"])]["handled_by"]) == jid)
            })
            .map(|m| m["id"].clone())
            .collect();
        for id in &delivered {
            state["answer_receipts"][s(id)]["thread"] = thread["id"].clone();
            state["answer_receipts"][s(id)]["handled_by"] = json!(waiting);
        }
        for id in &remaining {
            if let Some(receipt) = state["answer_receipts"][s(id)].as_object_mut() {
                receipt.remove("handled_by");
            }
        }
        stop_todo(ctx, record)?;
        record["restart"]["suppressed"] = json!(jid);
        record["restart"]["transferred_to"] = json!(waiting);
        record["restart"].as_object_mut().unwrap().remove("pending");
        record["restart"]
            .as_object_mut()
            .unwrap()
            .remove("waiting_for");
        match slot {
            Slot::Question(_) => {
                record["continuation"]["reported"] = json!(true);
                if n(&after) > cursor {
                    record["continuation"]["after"] = after.clone();
                }
                if truth(&after) {
                    acknowledge_reply(record, s(&after));
                }
                record["open"] = json!(!remaining.is_empty());
                if !remaining.is_empty() && s(&successor["status"]) == "succeeded" {
                    record["continuation"]["job"] = json!(waiting);
                    record["continuation"]["job_info"] = successor.clone();
                    record["continuation"]["reported"] = json!(false);
                    record["continuation"]["after"] = json!(n(&after).max(baseline).to_string());
                } else if let Some(first) = remaining.iter().map(n).min() {
                    // A duplicate/superseded cancellation has no completed
                    // session to follow up. Reopen only the undelivered answers.
                    let cursor = first.saturating_sub(1).to_string();
                    record["continuation"]["after"] = json!(cursor);
                    record["seen_reply"] = json!(cursor);
                }
            }
            Slot::Request => {
                if n(&after) > cursor {
                    record["after"] = after;
                }
                if !remaining.is_empty() && s(&successor["status"]) == "succeeded" {
                    record["job"] = json!(waiting);
                    record["job_info"] = successor.clone();
                    record["after"] = json!(delivered
                        .iter()
                        .map(n)
                        .max()
                        .unwrap_or(baseline)
                        .to_string());
                }
            }
        }
        return save(ctx, state, thread, record, slot);
    }
    if !deliberate_cancel(&source)
        && matches!(class, Failure::Context | Failure::Writer)
        && other.is_some()
    {
        // A writer conflict is a wait, not proof that the answer was applied.
        // Keep the failed continuation recoverable if that writer also fails.
        record["restart"]["waiting_for"] = json!(other);
        return save(ctx, state, thread, record, slot);
    }
    if let Some(restart) = record["restart"].as_object_mut() {
        restart.remove("waiting_for");
    }
    if deliberate_cancel(&source) || other.is_some() {
        // This is a generated recovery reminder, not the original task. Do not leave
        // it dispatchable after an intentional stop or transfer to another owner.
        if other.is_none() {
            stop_todo(ctx, record)?;
        }
        record["restart"]["suppressed"] = json!(jid);
        record["restart"].as_object_mut().unwrap().remove("pending");
        if let Slot::Question(_) = slot {
            record["continuation"]["reported"] = json!(true);
            record["open"] = json!(false);
        }
        return save(ctx, state, thread, record, slot);
    }
    if s(&record["restart"]["suppressed"]) == jid {
        return Ok(());
    }
    ensure_todo(ctx, thread, &source, record, &db)?;
    if !key.is_empty()
        && class != Failure::Task
        && record["restart"]["pending"]["failure"] != class.name()
    {
        record["restart"]["pending"]["failure"] = json!(class.name());
        record["restart"]["pending"]["source"] = source.clone();
        if matches!(class, Failure::Context | Failure::Model | Failure::Writer) {
            let context = recovery_prompt(ctx, thread, record, slot, &source, jid, owner, &key)?;
            for field in ["prompt", "after", "owner_message_ids"] {
                record["restart"]["pending"][field] = context[field].clone();
            }
        }
    }
    save(ctx, state, thread, record, slot)?;
    let now = ctx.now();
    let mut attempts = attempts(record, &db, now);
    // Also let the stall watchdog know the limit when it consumed the last attempt.
    share_attempts(ctx, thread, record, &attempts)?;
    let pending = &record["restart"]["pending"];
    let reserved = pending["reserved_at"].as_f64().unwrap_or(0.);
    let unused_reservation =
        !key.is_empty() && !truth(&pending["tried"]) && now - reserved < WINDOW_S;
    if db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .any(|i| i["id"] == record["restart"]["todo"] && i["stall_recovery"]["notice_sent"] == true)
    {
        record["restart"]["notice_sent"] = json!(true);
    }
    if (attempts.len() >= MAX_RESTARTS || truth(&record["restart"]["last_error"]))
        && !unused_reservation
        && !truth(&record["restart"]["notice_sent"])
    {
        let nonce = record["restart"]["notice_nonce"]
            .as_str()
            .map(str::to_owned)
            .unwrap_or_else(|| (uuid::Uuid::new_v4().as_u128() as u64).to_string());
        record["restart"]["notice_nonce"] = json!(nonce);
        save(ctx, state, thread, record, slot)?;
        let status = match s(&source["status"]) {
            _ if truth(&record["attachment_error"]) => "添付の送信失敗",
            "cancelled" => "取消",
            "failed" => "失敗",
            "lost" | "missing" => "中断",
            _ => "最終報告なし",
        };
        let tried = if truth(&record["restart"]["tried"]) {
            s(&record["restart"]["tried"])
        } else {
            "同じ作業を再開"
        };
        let content = format!("作業「{}」を再試行しています（{status}）。原因: {}。試した処置: {}。失敗時も1時間ごとに再試行します。", s(&thread["name"]).chars().take(100).collect::<String>(), failure::summary(&evidence), tried);
        let original = s(&thread["id"]);
        let routed = crate::question_progress::result_target(ctx, original, jid)?;
        let target = if routed != original {
            routed
        } else {
            ctx.todo
                .parent()
                .and_then(|p| read_text(&p.join("stall_thread.txt")).ok())
                .map(|id| id.trim().to_owned())
                .filter(|id| !id.is_empty())
                .unwrap_or_else(|| original.into())
        };
        let content = crate::question_progress::silent_text(&content);
        let delivery = (|| -> Result<()> {
            let history = thread_history(ctx, &target)?;
            if !history
                .iter()
                .any(|m| s(&m["nonce"]) == nonce || s(&m["content"]) == content)
            {
                ctx.call(
                    "POST",
                    &format!("/channels/{target}/messages"),
                    Some(&json!({"content":content,"flags":if crate::question_progress::is_separated(ctx,original)? {SILENT|4} else {SILENT},
                        "allowed_mentions":{"parse":[],"users":[],"replied_user":false},
                        "nonce":nonce,"enforce_nonce":true})),
                )?;
            }
            Ok(())
        })();
        if delivery.is_ok() {
            record["restart"]["notice_sent"] = json!(true);
        }
        save(ctx, state, thread, record, slot)?;
        share_attempts(ctx, thread, record, &attempts)?;
    }
    if !unused_reservation && failure::retry_at(&attempts, now) > now {
        return Ok(());
    }
    if *budget
        || record["restart"]["next_at"]
            .as_f64()
            .is_some_and(|at| now < at)
    {
        return Ok(());
    }
    if matches!(class, Failure::Console | Failure::Proxy)
        && s(&record["restart"]["remedied_job"]) != jid
    {
        let scheduler_root = ctx
            .scheduler_command
            .parent()
            .ok_or("missing scheduler directory")?
            .to_path_buf();
        if !failure::reserve_console(&scheduler_root, now)? {
            return Ok(());
        }
        record["restart"]["tried"] = json!(class.remedy());
        record["restart"]["next_at"] = json!(now + 60.);
        let remedy = ctx
            .run(
                &[
                    "schtasks.exe".into(),
                    "/Run".into(),
                    "/TN".into(),
                    failure::CONSOLE_TASK.into(),
                ],
                15,
            )
            .and_then(|out| {
                if out.code == 0 {
                    Ok(())
                } else {
                    Err(format!("定期タスクの起動に失敗: {}", safe_text(out.stderr)))
                }
            });
        if let Err(error) = remedy {
            record["restart"]["last_error"] = json!(safe_text(error));
            record["restart"]["next_at"] = json!(now + failure::SLOW_RETRY_S);
        } else {
            record["restart"]["remedied_job"] = json!(jid);
            if let Err(error) = ctx.effects.http(
                "POST",
                "http://127.0.0.1:10012/api/resident/restart",
                Some(&json!({})),
                false,
            ) {
                record["restart"]["tried"] = json!(format!(
                    "定期タスクを起動。終了APIに接続できず: {}。60秒後にジョブを再試行",
                    safe_text(error)
                ));
            }
        }
        return save(ctx, state, thread, record, slot);
    }
    if key.is_empty() {
        // A missing job still has its launch selection/cwd saved in the tracked record.
        let selection = match slot {
            Slot::Request => &record["selection"],
            Slot::Question(_) => &record["continuation"]["selection"],
        };
        for field in ["model", "effort", "task_type", "sandbox", "scope_source"] {
            if !truth(&source[field]) {
                source[field] = selection[field].clone();
            }
        }
        if !truth(&source["cwd"]) {
            source["cwd"] = match slot {
                Slot::Request => json!(REQUEST_CWD),
                Slot::Question(_) => record["continuation"]["cwd"].clone(),
            };
        }
        if !truth(&source["model"]) || !truth(&source["effort"]) || !truth(&source["cwd"]) {
            return Err("restart source is missing model, effort or cwd".into());
        }
        attempts.push(json!(now));
        record["restart"]["attempts"] = json!(attempts);
        record["restart"]["next_at"] = json!(now + RETRY_DELAY_S);
        record["restart"]["tried"] = json!(class.remedy());
        let mut key = uuid::Uuid::new_v4().simple().to_string();
        if let Slot::Question(_) = slot {
            if truth(&record["continuation"]["launch"]["key"]) {
                key = s(&record["continuation"]["launch"]["key"]).into();
            }
        }
        if let Slot::Request = slot {
            if truth(&record["launch"]["key"]) {
                key = s(&record["launch"]["key"]).into();
            }
        }
        let context = recovery_prompt(ctx, thread, record, slot, &source, jid, owner, &key)?;
        record["restart"]["pending"] = json!({"key":key,"source":source,"prompt":context["prompt"],"after":context["after"],"owner_message_ids":context["owner_message_ids"],"failure":class.name(),"reserved_at":now,"tried":false});
        save(ctx, state, thread, record, slot)?;
        share_attempts(ctx, thread, record, &attempts)?;
    } else if !unused_reservation {
        attempts.push(json!(now));
        record["restart"]["attempts"] = json!(attempts);
        record["restart"]["pending"]["reserved_at"] = json!(now);
        save(ctx, state, thread, record, slot)?;
        share_attempts(ctx, thread, record, &attempts)?;
    }
    record["restart"]["pending"]["tried"] = json!(true);
    let pending_source = &record["restart"]["pending"]["source"];
    if session_busy(
        ctx,
        if truth(&pending_source["thread_id"]) {
            s(&pending_source["thread_id"])
        } else {
            s(&pending_source["resume_session"])
        },
        jid,
    )? {
        record["restart"]["pending"]["source"]["thread_id"] = Value::Null;
        record["restart"]["pending"]["source"]["resume_session"] = Value::Null;
    }
    save(ctx, state, thread, record, slot)?;
    *budget = true;
    let pending = record["restart"]["pending"].clone();
    let source = &pending["source"];
    let result = launch_job_with_session(
        ctx,
        thread,
        s(&pending["prompt"]),
        source,
        s(&pending["key"]),
        false,
        Some(s(&source["cwd"])),
        Some("resume"),
        Some(if truth(&source["thread_id"]) {
            s(&source["thread_id"])
        } else {
            s(&source["resume_session"])
        }),
    );
    let launched = result.or_else(|error| find_launch_job(ctx, s(&pending["key"])).ok_or(error));
    match launched {
        Ok(new) => {
            link_todo(ctx, record, &new)?;
            adopt(ctx, state, thread, record, &new, slot);
            match slot {
                Slot::Question(_) => record["continuation"]["job_info"] = job_info(ctx, &new),
                Slot::Request => record["job_info"] = job_info(ctx, &new),
            }
            record["restart"]["next_at"] = json!(now + RETRY_DELAY_S);
            println!("automatically resumed {} {jid} -> {new}", s(&thread["id"]));
        }
        Err(error) => {
            if launch_blocked(&error) {
                record["restart"]["pending"]["tried"] = json!(false);
                record["restart"]["pending"]["reserved_at"] = json!(now);
                return save(ctx, state, thread, record, slot);
            }
            record["restart"]["failed_source"] = json!(jid);
            record["restart"]["last_selection"] = pending["source"].clone();
            record["restart"]["last_prompt"] = pending["prompt"].clone();
            // A transport/timeout failure may conceal a detached launch. Retry the same
            // key so the launcher can adopt it, rather than reserve another job.
            if error.starts_with("codex_jobs run exited ") {
                record["restart"].as_object_mut().unwrap().remove("pending");
            }
            record["restart"]["next_at"] = json!(
                now + if attempts.len() >= MAX_RESTARTS {
                    failure::SLOW_RETRY_S
                } else {
                    RETRY_DELAY_S
                }
            );
            record["restart"]["last_error"] = json!(safe_text(error));
        }
    }
    save(ctx, state, thread, record, slot)
}
