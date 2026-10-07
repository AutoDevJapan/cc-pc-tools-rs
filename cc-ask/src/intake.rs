use crate::core::*;
use serde_json::{json, Value};
use std::{collections::BTreeMap, time::Duration};

const MAX_RETRIES: u64 = 2;
struct RequestWorker {
    job: Value,
    // Preserve provenance: a prompt assignment must not outrank declared ownership.
    legacy_thread_ids: Vec<String>,
}
fn remove(entry: &mut Value, key: &str) {
    entry.as_object_mut().unwrap().remove(key);
}
fn list(value: &Value) -> Vec<Value> {
    value.as_array().cloned().unwrap_or_default()
}
fn checkpoint(ctx: &Context, state: &mut Value, thread: &Value, entry: &Value) -> Result<()> {
    state["requests"][s(&thread["id"])] = entry.clone();
    state["requests"][s(&thread["id"])]["thread_info"] = thread.clone();
    ctx.save(state)
}
fn task_ja(kind: &str) -> &str {
    match kind {
        "implementation" => "実装",
        "code_fix" => "修正",
        "code_review" => "コードレビュー",
        "review" => "レビュー",
        "research" => "調査",
        "docs_edit" => "文書の作成",
        "image_generation" => "画像生成",
        "browser_automation" => "ブラウザ操作",
        "live_debug" => "障害の調査",
        "live_verification" => "実機での確認",
        "test" => "テスト",
        "ci_fix" => "CI の修正",
        other => other,
    }
}

pub fn request_threads(ctx: &mut Context, cfg: &Value, state: &mut Value) -> Result<Vec<Value>> {
    let board_id = crate::board::registered_id(ctx)?;
    let _span = crate::tick_runtime::Span::new("request thread discovery");
    let requests = state["requests"].clone();
    let mut threads = BTreeMap::new();
    let task = s(&cfg["task_channel_id"]);
    let mut paths = vec![
        format!("/guilds/{}/threads/active", s(&cfg["guild_id"])),
        format!("/channels/{REQUEST_CHANNEL_ID}/threads/archived/public?limit=25"),
    ];
    if !task.is_empty() {
        paths.push(format!("/channels/{task}/threads/archived/public?limit=25"));
    }
    let mut bot = None;
    for path in paths {
        match ctx.call("GET", &path, None) {
            Ok(page) => {
                for thread in list(&page["threads"]) {
                    if !board_id.is_empty() && s(&thread["id"]) == board_id {
                        continue;
                    }
                    let task_thread = !task.is_empty() && s(&thread["parent_id"]) == task;
                    if task_thread && bot.is_none() {
                        match ctx.call("GET", "/users/@me", None) {
                            Ok(me) => bot = Some(me),
                            Err(error) => {
                                eprintln!("bot identity unavailable: {}", error_detail(&error))
                            }
                        }
                    }
                    if s(&thread["parent_id"]) == REQUEST_CHANNEL_ID
                        || (task_thread
                            && bot.as_ref().is_some_and(|me| {
                                me["bot"] == true
                                    && !s(&me["id"]).is_empty()
                                    && thread["owner_id"] == me["id"]
                            }))
                    {
                        threads.insert(s(&thread["id"]).to_string(), thread);
                    }
                }
            }
            Err(err) => eprintln!("thread scan failed {}", error_detail(&err)),
        }
    }
    if let Some(requests) = requests.as_object() {
        let mut tracked: Vec<_> = requests.iter().collect();
        tracked.sort_by(|(_, a), (_, b)| {
            a["last_checked_at"]
                .as_f64()
                .unwrap_or(0.)
                .total_cmp(&b["last_checked_at"].as_f64().unwrap_or(0.))
        });
        if let Some(cursor) = state["tracked_scan_after"].as_str() {
            if let Some(at) = tracked.iter().position(|(tid, _)| tid.as_str() == cursor) {
                let length = tracked.len();
                tracked.rotate_left((at + 1) % length.max(1));
            }
        }
        let mut fetched = 0;
        for (tid, entry) in tracked {
            if !board_id.is_empty() && tid == &board_id {
                continue;
            }
            if !threads.contains_key(tid)
                && [
                    "job",
                    "recovery",
                    "close_pending",
                    "pending_notice",
                    "acknowledgement",
                    "launch",
                    "progress_job",
                ]
                .iter()
                .any(|k| truth(&entry[*k]))
            {
                let known = &entry["thread_info"];
                if s(&known["id"]) == tid
                    && (s(&known["parent_id"]) == REQUEST_CHANNEL_ID
                        || (!task.is_empty()
                            && s(&known["parent_id"]) == task
                            && bot.as_ref().is_none_or(|me| known["owner_id"] == me["id"])))
                {
                    threads.insert(tid.clone(), known.clone());
                    continue;
                }
                // Bootstrap old records in batches; discovering hundreds of
                // archived destinations must not consume the reply-reading phase.
                if crate::tick_runtime::active()
                    && (fetched >= 8 || crate::tick_runtime::check().is_err())
                {
                    continue;
                }
                fetched += 1;
                state["tracked_scan_after"] = json!(tid);
                ctx.save(state)?;
                match ctx.call("GET", &format!("/channels/{tid}"), None) {
                    Ok(thread)
                        if s(&thread["parent_id"]) == REQUEST_CHANNEL_ID
                            || (!task.is_empty()
                                && s(&thread["parent_id"]) == task
                                && bot
                                    .as_ref()
                                    .is_some_and(|me| thread["owner_id"] == me["id"])) =>
                    {
                        state["requests"][tid]["thread_info"] = thread.clone();
                        ctx.save(state)?;
                        threads.insert(tid.clone(), thread);
                    }
                    Err(err) => {
                        eprintln!("tracked thread unavailable {tid} {}", error_detail(&err))
                    }
                    _ => (),
                }
            }
        }
    }
    let mut out: Vec<_> = threads.into_values().collect();
    out.sort_by_key(|t| n(&t["id"]));
    Ok(out)
}

pub fn deliver_notice(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    entry: &mut Value,
) -> Result<()> {
    if !truth(&entry["pending_notice"]) {
        return Ok(());
    }
    // Drop an undelivered question emitted by the old retry-cap policy.
    if truth(&entry["failures"]) && truth(&entry["pending_notice"]["question"]) {
        remove(entry, "pending_notice");
        return checkpoint(ctx, state, thread, entry);
    }
    let notice = entry["pending_notice"].clone();
    let after = post(
        ctx,
        s(&thread["id"]),
        s(&notice["text"]),
        truth(&notice["question"]),
    )?;
    if truth(&notice["question"]) {
        register_question(
            state,
            thread,
            after.as_deref().unwrap_or(s(&entry["after"])),
        );
    }
    remove(entry, "pending_notice");
    checkpoint(ctx, state, thread, entry)
}

pub fn failure_notice(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    entry: &mut Value,
    failed: &Value,
) -> Result<()> {
    // No session exists if the initial launch failed; retain its selection for a fresh retry.
    entry["job"] = failed["job"].clone();
    remove(entry, "job_info");
    remove(entry, "launch");
    remove(entry, "recovery");
    checkpoint(ctx, state, thread, entry)
}
pub fn launch_request(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    entry: &mut Value,
    messages: &[Value],
    owner: &str,
    retry: bool,
) -> Result<()> {
    let exclude: Vec<_> = list(&entry["exclude"])
        .iter()
        .map(|x| s(x).to_string())
        .collect();
    let context = crate::prompt_context::conversation(ctx, thread, messages, owner, 32 * 1024)?;
    let mut selection = pick_model(ctx, &context, &exclude, None)?;
    let newest = messages
        .iter()
        .rev()
        .find(|m| s(&m["author"]["id"]) == owner);
    apply_job_scope(
        &mut selection,
        &entry["selection"],
        newest.map(|m| s(&m["content"])),
    );
    entry["selection"] = selection.clone();
    if retry {
        entry["retries"] = json!(n(&entry["retries"]) + 1);
    }
    entry["launch"] = json!({"key":uuid::Uuid::new_v4().simple().to_string(),"kind":"request","selection":selection,"retry":retry});
    // Durable reservation before the launcher can start a supervisor.
    checkpoint(ctx, state, thread, entry)?;
    execute_launch(ctx, state, thread, entry, owner)
}

pub fn adopt_launch(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    entry: &mut Value,
    jid: &str,
) -> Result<()> {
    let launch = entry["launch"].clone();
    remove(entry, "launch");
    let selection = &launch["selection"];
    if s(&launch["kind"]) == "diagnosis" {
        let failed = list(&entry["failures"]);
        let diagnosis = json!({"job":jid,"selection":selection,"for_job":failed.last().unwrap()["job"],"reported":false});
        let mut diagnoses = list(&entry["diagnoses"]);
        diagnoses.push(diagnosis.clone());
        entry["diagnoses"] = json!(diagnoses);
        entry["recovery"]["phase"] = json!("waiting");
        entry["recovery"]["diagnosis"] = diagnosis;
        checkpoint(ctx, state, thread, entry)?;
        println!("diagnosis started {} {jid}", s(&thread["id"]));
        return Ok(());
    }
    entry["job"] = json!(jid);
    entry["job_info"] = job_info(ctx, jid);
    if let Some(after) = entry.get("request_after").cloned() {
        entry["after"] = after;
    }
    for id in entry["request_message_ids"]
        .as_array()
        .into_iter()
        .flatten()
    {
        state["answer_receipts"][s(id)]["thread"] = thread["id"].clone();
        state["answer_receipts"][s(id)]["handled_by"] = json!(jid);
    }
    remove(entry, "recovery");
    if truth(&state["answer_receipts"][s(&entry["after"])]["posted"]) {
        checkpoint(ctx, state, thread, entry)?;
        println!("started {} {jid}", s(&thread["id"]));
        return Ok(());
    }
    let retry = if truth(&launch["retry"]) {
        format!("・再試行 {}/{MAX_RETRIES}", n(&entry["retries"]))
    } else {
        String::new()
    };
    entry["acknowledgement"] = json!(format!(
        "受け付けました。{}として、{}（推論 {}）で進めます。\n-# ジョブ {jid}{retry}",
        task_ja(s(&selection["task_type"])),
        s(&selection["model"]),
        s(&selection["effort"])
    ));
    checkpoint(ctx, state, thread, entry)?;
    post(ctx, s(&thread["id"]), s(&entry["acknowledgement"]), false)?;
    remove(entry, "acknowledgement");
    println!("started {} {jid}", s(&thread["id"]));
    Ok(())
}

pub fn execute_launch(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    entry: &mut Value,
    owner: &str,
) -> Result<()> {
    let launch = entry["launch"].clone();
    let diagnosis = s(&launch["kind"]) == "diagnosis";
    let prompt = if diagnosis {
        diagnosis_prompt(ctx, thread, entry)
    } else {
        format!(
            "{REQUEST_PREAMBLE}\nスレッドID: {}\n{}",
            s(&thread["id"]),
            crate::prompt_context::conversation(
                ctx,
                thread,
                &list(&entry["request_messages"]),
                owner,
                32 * 1024
            )?
        )
    };
    match launch_job(
        ctx,
        thread,
        &prompt,
        &launch["selection"],
        s(&launch["key"]),
        diagnosis,
        None,
        None,
    ) {
        Ok(jid) => adopt_launch(ctx, state, thread, entry, &jid),
        Err(err) => {
            if let Some(jid) = find_launch_job(ctx, s(&launch["key"])) {
                return adopt_launch(ctx, state, thread, entry, &jid);
            }
            if launch_blocked(&err) {
                eprintln!(
                    "request launch deferred {}: {}; retry next tick",
                    s(&thread["id"]),
                    error_detail(&err)
                );
                return checkpoint(ctx, state, thread, entry);
            }
            if diagnosis {
                remove(entry, "launch");
                entry["recovery"]["phase"] = json!("retry");
                eprintln!(
                    "diagnosis launch failed {} {}",
                    s(&thread["id"]),
                    error_detail(&err)
                );
                // Persist the transition even if its notification fails.
                checkpoint(ctx, state, thread, entry)?;
                post(ctx,s(&thread["id"]),"原因の調査を始められませんでした。次の確認（1分後）で、元の依頼をやり直します。",false)?;
                Ok(())
            } else {
                eprintln!(
                    "request launch failed {}: {}; retry next tick",
                    s(&thread["id"]),
                    error_detail(&err)
                );
                // Preserve the initial key, including when a detached response was lost.
                entry["job"] = json!("launch-failed");
                let mut info = launch["selection"].clone();
                info["id"] = json!("launch-failed");
                info["status"] = json!("missing");
                info["cwd"] = json!(REQUEST_CWD);
                info["last_error"] = json!(safe_text(&err));
                entry["job_info"] = info;
                checkpoint(ctx, state, thread, entry)
            }
        }
    }
}

pub fn intake_thread(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    owner: &str,
    budget: &mut bool,
) -> Result<()> {
    let workers = request_workers(ctx)?;
    intake_thread_with_workers(ctx, state, thread, owner, budget, &workers)
}

fn intake_thread_with_workers(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    owner: &str,
    budget: &mut bool,
    workers: &[RequestWorker],
) -> Result<()> {
    let tid = s(&thread["id"]);
    // Check before pending launches, adopted workers, and closed-question intake.
    // Leave cursors and answer receipts untouched for the main session's inbox.
    if crate::routing::thread_inbox_only(ctx, state, tid) {
        return Ok(());
    }
    let task_thread = !s(&ctx.cfg["task_channel_id"]).is_empty()
        && thread["parent_id"] == ctx.cfg["task_channel_id"];
    if state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|q| {
            s(&q["thread_id"]) == tid
                && s(&q["source"]) != "request"
                && truth(&q["open"])
                && truth(&q["cwd"])
        })
        .cloned()
        .map(|q| {
            if valid_job(s(&q["continuation"]["job"])) && state["requests"][tid].is_object() {
                let mut observed = state["requests"][tid].clone();
                observed["job"] = q["continuation"]["job"].clone();
                observed["job_info"] = job_info(ctx, s(&observed["job"]));
                observed["after"] = json!(n(&observed["after"])
                    .max(n(&q["continuation"]["after"]))
                    .to_string());
                observed["external_owner"] = json!(true);
                observed["reported"] = json!(false);
                for field in [
                    "launch",
                    "owner_update",
                    "restart",
                    "recovery",
                    "close_pending",
                    "progress_job",
                ] {
                    remove(&mut observed, field);
                }
                state["requests"][tid] = observed;
            }
            true
        })
        .unwrap_or(false)
    {
        return Ok(());
    }
    if s(&thread["parent_id"]) != REQUEST_CHANNEL_ID && !task_thread {
        return Ok(());
    }
    if task_thread {
        let me = ctx.call("GET", "/users/@me", None)?;
        if me["bot"] != true || s(&me["id"]).is_empty() || thread["owner_id"] != me["id"] {
            return Ok(());
        }
    }
    if !state["requests"].is_object() {
        state["requests"] = json!({});
    }
    let mut entry = state["requests"][tid].clone();
    if entry.is_null() {
        let after = if task_thread {
            n(&state["reply_intake_since"])
                .max(n(&thread["id"]))
                .to_string()
        } else {
            "0".into()
        };
        entry = json!({"after":after,"job":null,"title":thread["name"]});
        if task_thread {
            let question = state["questions"]
                .as_array()
                .into_iter()
                .flatten()
                .rev()
                .find(|q| s(&q["thread_id"]) == tid && s(&q["source"]) != "routing")
                .cloned();
            if let Some(question) = question {
                let history = thread_history(ctx, tid)?;
                crate::steering::confirm_recorded_reads(ctx, state, &question, &history)?;
                // resume owns active questions with a known working directory.
                if truth(&question["open"]) && truth(&question["cwd"]) {
                    return Ok(());
                }
                if let Some(message) = history.iter().find(|m| {
                    s(&m["author"]["id"]) == owner
                        && n(&m["id"]) > n(&question["after"])
                        && truth(&state["answer_receipts"][s(&m["id"])]["posted"])
                        && !reply_unavailable(state, s(&m["id"]))
                }) {
                    entry["after"] = json!((n(&message["id"]) - 1).to_string());
                }
                let has_pending = history.iter().any(|m| {
                    s(&m["author"]["id"]) == owner
                        && n(&m["id"]) > n(&entry["after"])
                        && !reply_unavailable(state, s(&m["id"]))
                });
                if let Some(jid) = if has_pending {
                    crate::steering::associated_job(ctx, &question)?
                } else {
                    None
                } {
                    entry["job"] = json!(jid);
                    entry["job_info"] = job_info(ctx, &jid);
                    entry["selection"] = entry["job_info"].clone();
                    entry["reported"] = json!(false);
                }
            }
        }
    }
    // Keep the detached entry in state on every exit, including a failed effect.
    state["requests"][tid] = entry.clone();
    let result = intake_entry(ctx, state, thread, &mut entry, owner, budget, workers);
    entry["last_checked_at"] = json!(ctx.now());
    entry["thread_info"] = thread.clone();
    match &result {
        Err(error) => entry["last_error"] = json!(error_detail(error)),
        Ok(()) => {
            remove(&mut entry, "last_error");
        }
    }
    state["requests"][tid] = entry;
    result
}

fn intake_entry(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    entry: &mut Value,
    owner: &str,
    budget: &mut bool,
    workers: &[RequestWorker],
) -> Result<()> {
    let tid = s(&thread["id"]);
    let mut messages = list(&ctx.call(
        "GET",
        &format!(
            "/channels/{tid}/messages?after={}&limit=100",
            s(&entry["after"])
        ),
        None,
    )?);
    if messages.len() == 100 {
        messages = thread_history(ctx, tid)?
            .into_iter()
            .filter(|m| n(&m["id"]) > n(&entry["after"]))
            .collect();
    }
    messages.sort_by_key(|m| n(&m["id"]));
    crate::steering::confirm_original_inputs(ctx, state, tid, &messages)?;
    // A question continuation owns both delivery and its final result. Intake
    // may observe it, but must not restart it or publish its result a second time.
    if !truth(&entry["launch"]) && !truth(&entry["restart"]["pending"]) {
        let continuation = state["questions"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|q| {
                s(&q["thread_id"]) == tid
                    && s(&q["source"]) != "request"
                    && (truth(&q["continuation"]["launch"])
                        || truth(&q["continuation"]["owner_update"])
                        || (!truth(&q["continuation"]["reported"])
                            && truth(&q["continuation"]["job"])))
            })
            .map(|q| q["continuation"].clone());
        if let Some(continuation) = continuation {
            if valid_job(s(&continuation["job"])) && s(&continuation["job"]) != "launch-failed" {
                entry["job"] = continuation["job"].clone();
                entry["job_info"] = job_info(ctx, s(&continuation["job"]));
                entry["external_owner"] = json!(true);
                entry["reported"] = json!(false);
                entry["after"] = json!(n(&entry["after"])
                    .max(n(&continuation["after"]))
                    .to_string());
                for field in ["restart", "recovery", "progress_job", "close_pending"] {
                    remove(entry, field);
                }
            }
            return checkpoint(ctx, state, thread, entry);
        }
    }
    // Workers started outside intake may already be answering this forum thread.
    // Discover them before acknowledging messages or reserving a new launch.
    let tracked = job_info(ctx, s(&entry["job"]));
    let recoverable_failure = s(&tracked["status"]) == "failed"
        && matches!(
            crate::recovery_failure::classify(&crate::recovery_failure::evidence(
                &tracked,
                &ctx.jobs.join("jobs").join(s(&entry["job"]))
            )),
            crate::recovery_failure::Failure::Context | crate::recovery_failure::Failure::Writer
        );
    if !truth(&entry["launch"])
        && !truth(&entry["owner_update"])
        && !truth(&entry["restart"]["pending"])
        && (!recoverable_failure || truth(&entry["external_owner"]))
        && (!truth(&entry["job"])
            || truth(&entry["external_owner"])
            || !RUNNING.contains(&s(&job_info(ctx, s(&entry["job"]))["status"])))
    {
        if let Some(worker) = running_request_owner(ctx, tid, workers)? {
            let jid = s(&worker.job["id"]);
            let mut info = job_info(ctx, jid);
            // Fill missing legacy metadata even when this worker was adopted
            // before the fix and its ID has not changed.
            if !worker.legacy_thread_ids.is_empty() {
                let sidecar = ctx.jobs.join("jobs").join(jid).join("discord-threads.json");
                let associations = json!({"thread_ids":worker.legacy_thread_ids,"source":"prompt"});
                if !sidecar.is_file() || read_json(&sidecar)? != associations {
                    atomic_json(&sidecar, &associations, 2)?;
                }
                info["discord_thread_ids"] = json!(worker.legacy_thread_ids);
                info["discord_thread_ids_source"] = json!("prompt");
            }
            if s(&entry["job"]) != jid {
                entry["job"] = json!(jid);
                entry["job_info"] = info;
                entry["selection"] = entry["job_info"].clone();
                entry["external_owner"] = json!(true);
                for field in ["restart", "recovery", "close_pending", "progress_job"] {
                    remove(entry, field);
                }
                checkpoint(ctx, state, thread, entry)?;
            } else if info["discord_thread_ids"] != entry["job_info"]["discord_thread_ids"]
                || info["discord_thread_ids_source"]
                    != entry["job_info"]["discord_thread_ids_source"]
            {
                entry["job_info"]["discord_thread_ids"] = info["discord_thread_ids"].clone();
                entry["job_info"]["discord_thread_ids_source"] =
                    info["discord_thread_ids_source"].clone();
                entry["selection"]["discord_thread_ids"] = info["discord_thread_ids"].clone();
                entry["selection"]["discord_thread_ids_source"] =
                    info["discord_thread_ids_source"].clone();
                checkpoint(ctx, state, thread, entry)?;
            }
        }
    }
    let owner_messages: Vec<_> = messages
        .iter()
        .filter(|m| {
            s(&m["author"]["id"]) == owner
                && !crate::snooze::control_only(m)
                && !reply_unavailable(state, s(&m["id"]))
        })
        .cloned()
        .collect();
    crate::snooze::apply_thread(state, tid, &messages, owner);
    crate::snooze::apply(entry, &messages, owner);
    checkpoint(ctx, state, thread, entry)?;
    let has_question = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .any(|q| s(&q["thread_id"]) == tid && truth(&q["open"]));
    // Receipt never consumes the delivery cursor. A running worker must see the answer.
    record_owner_answers(state, tid, &messages, owner);
    if !owner_messages.is_empty() {
        let names = crate::project::ProjectNames::load(&ctx.client_projects)?;
        let project = crate::project::question_project(
            ctx,
            &json!({"title":thread["name"],"cwd":entry["job_info"]["cwd"],"project":entry["project"]}),
            &names,
        );
        crate::steering::acknowledge(ctx, state, tid, &project, &owner_messages)?;
    }
    // Closing an answered question must not depend on a successful worker handoff.
    close_answered_questions(state, tid, &messages, owner);
    checkpoint(ctx, state, thread, entry)?;
    if truth(&entry["external_owner"]) {
        let info = job_info(ctx, s(&entry["job"]));
        if RUNNING.contains(&s(&info["status"]))
            || truth(&entry["owner_update"])
            || (s(&info["status"]) == "succeeded" && !owner_messages.is_empty())
        {
            // One external worker can own several idea threads. Deliver through
            // the existing same-session handoff, preserving its entire prompt.
            if !*budget {
                crate::steering::deliver(
                    ctx,
                    state,
                    thread,
                    entry,
                    crate::restart::Slot::Request,
                    owner,
                    budget,
                )?;
            }
            return Ok(());
        }
        // External workers publish their own results. New replies can start a
        // request after they finish, but old replies must never be replayed.
        if owner_messages.is_empty() {
            return Ok(());
        }
        entry["job"] = Value::Null;
        remove(entry, "external_owner");
    }
    if crate::steering::deliver(
        ctx,
        state,
        thread,
        entry,
        crate::restart::Slot::Request,
        owner,
        budget,
    )? {
        return Ok(());
    }
    if crate::snooze::thread_paused(state, tid, ctx.now())
        || (!has_question && crate::snooze::paused(entry, ctx.now()))
    {
        return Ok(());
    }
    let previous = s(&entry["job"]).to_owned();
    let info = job_info(ctx, &previous);
    // Stopping an old job suppresses its recovery, not the owner's next request.
    if !owner_messages.is_empty()
        && !previous.is_empty()
        && !truth(&entry["launch"])
        && !truth(&entry["owner_update"])
        && !RUNNING.contains(&s(&info["status"]))
        && (crate::restart::deliberate_cancel(&info)
            || s(&entry["restart"]["suppressed"]) == previous)
    {
        entry["job"] = Value::Null;
        for field in ["restart", "recovery", "job_info", "progress_job"] {
            remove(entry, field);
        }
        checkpoint(ctx, state, thread, entry)?;
    }
    if truth(&entry["launch"]) {
        if let Some(jid) = find_launch_job(ctx, s(&entry["launch"]["key"])) {
            adopt_launch(ctx, state, thread, entry, &jid)?;
        } else {
            if s(&entry["launch"]["kind"]) == "request" {
                let original = list(&entry["request_message_ids"]);
                if original.iter().any(|id| reply_reserved(state, s(id))) {
                    return Ok(());
                }
                let remaining: Vec<_> = original
                    .iter()
                    .filter(|id| !reply_delivered(state, s(id)))
                    .cloned()
                    .collect();
                if !original.is_empty() && remaining.is_empty() {
                    remove(entry, "launch");
                    entry["after"] = entry["request_after"].clone();
                    entry["job"] = Value::Null;
                    return checkpoint(ctx, state, thread, entry);
                }
                if remaining.len() != original.len() {
                    entry["request_message_ids"] = json!(remaining);
                    entry["request_messages"] = json!(list(&entry["request_messages"])
                        .into_iter()
                        .filter(|m| !original.contains(&m["id"])
                            || !reply_delivered(state, s(&m["id"])))
                        .collect::<Vec<_>>());
                    checkpoint(ctx, state, thread, entry)?;
                }
            }
            if s(&entry["job"]) == "launch-failed" {
                crate::restart::recover(
                    ctx,
                    state,
                    thread,
                    entry,
                    crate::restart::Slot::Request,
                    "launch-failed",
                    owner,
                    budget,
                )?;
            } else if !*budget {
                *budget = true;
                execute_launch(ctx, state, thread, entry, owner)?;
            }
        }
        return Ok(());
    }
    deliver_notice(ctx, state, thread, entry)?;
    if truth(&entry["acknowledgement"]) {
        post(ctx, tid, s(&entry["acknowledgement"]), false)?;
        remove(entry, "acknowledgement");
        checkpoint(ctx, state, thread, entry)?;
    }
    if !truth(&entry["job"]) {
        if let Some(jid) = crate::restart::progress_successor(ctx, entry)? {
            entry["job"] = json!(jid);
            entry["job_info"] = job_info(ctx, &jid);
            remove(entry, "progress_job");
            checkpoint(ctx, state, thread, entry)?;
        }
    }
    if truth(&entry["job"]) {
        let jid = s(&entry["job"]).to_string();
        let info = job_info(ctx, &jid);
        let status = info["status"].as_str().unwrap_or("missing");
        if RUNNING.contains(&status) {
            return Ok(());
        }
        let result = final_message(ctx, &jid);
        if status != "succeeded" || result.trim().is_empty() {
            return crate::restart::recover(
                ctx,
                state,
                thread,
                entry,
                crate::restart::Slot::Request,
                &jid,
                owner,
                budget,
            );
        }
        let disposition = result_state(&result);
        log_result_misuse(&jid, &result);
        if disposition == "状態: 継続中" {
            crate::restart::continue_work(ctx, thread, entry, &jid)?;
            checkpoint(ctx, state, thread, entry)?;
        }
        // Re-read immediately before the first final POST, after preparing the result.
        if crate::steering::deliver(
            ctx,
            state,
            thread,
            entry,
            crate::restart::Slot::Request,
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
                crate::steering::addressed(entry),
                result_footer(&jid, disposition)
            ),
            disposition != "状態: 継続中",
            &jid,
            Some(s(&entry["after"])),
        )?;
        let after = match delivery {
            crate::result_delivery::Delivery::Sent(after) => after,
            crate::result_delivery::Delivery::OwnerMessagesPending => {
                crate::steering::deliver(
                    ctx,
                    state,
                    thread,
                    entry,
                    crate::restart::Slot::Request,
                    owner,
                    budget,
                )?;
                return Ok(());
            }
            crate::result_delivery::Delivery::PreparationFailed(error) => {
                entry["attachment_error"] = json!(error);
                checkpoint(ctx, state, thread, entry)?;
                return crate::restart::recover(
                    ctx,
                    state,
                    thread,
                    entry,
                    crate::restart::Slot::Request,
                    &jid,
                    owner,
                    budget,
                );
            }
        };
        entry.as_object_mut().unwrap().remove("attachment_error");
        if disposition == "状態: 完了" {
            crate::restart::finish_todo(ctx, entry)?;
        }
        entry["job"] = Value::Null;
        entry["last_job"] = json!(jid);
        entry["result_after"] = json!(after);
        entry["close_pending"] = json!(disposition == "状態: 完了" && owner_messages.is_empty());
        if disposition == "状態: 要判断" {
            crate::questions::register_result(
                ctx,
                state,
                thread,
                after.as_deref().unwrap_or(s(&entry["after"])),
                &jid,
            )?;
        }
        checkpoint(ctx, state, thread, entry)?;
        if truth(&entry["close_pending"]) {
            archive_completed(ctx, thread, entry, owner)?;
        }
        println!("result delivered {tid} {jid}");
        if owner_messages.is_empty() {
            return Ok(());
        }
    }
    if truth(&entry["close_pending"]) {
        if owner_messages.is_empty() {
            archive_completed(ctx, thread, entry, owner)?;
        }
        entry["close_pending"] = json!(false);
    }
    // Migrate an interrupted legacy diagnostic/retry record into bounded same-session recovery.
    if truth(&entry["recovery"]) {
        if let Some(failed) = entry["failures"].as_array().and_then(|a| a.last()) {
            entry["job"] = failed["job"].clone();
        } else {
            entry["job"] = json!("launch-failed");
        }
        remove(entry, "recovery");
        let jid = s(&entry["job"]).to_string();
        return crate::restart::recover(
            ctx,
            state,
            thread,
            entry,
            crate::restart::Slot::Request,
            &jid,
            owner,
            budget,
        );
    }
    if *budget || owner_messages.is_empty() {
        if *budget && !owner_messages.is_empty() {
            eprintln!("owner reply deferred {tid}: tick launch budget used; retry next tick");
        }
        return Ok(());
    }
    *budget = true;
    let latest = owner_messages.iter().map(|m| n(&m["id"])).max().unwrap();
    let history = clean_history(ctx, tid, latest)?;
    entry["request_after"] = json!(latest.to_string());
    entry["request_message_ids"] = json!(owner_messages
        .iter()
        .map(|m| m["id"].clone())
        .collect::<Vec<_>>());
    entry["last_launch_attempt_at"] = json!(ctx.now());
    entry["request_messages"] = json!(history);
    entry["retries"] = json!(0);
    entry["exclude"] = json!([]);
    entry["failures"] = json!([]);
    remove(entry, "recovery");
    launch_request(ctx, state, thread, entry, &history, owner, false)
}

/// Explicit associations and legacy labeled assignments identify forum workers.
fn request_workers(ctx: &Context) -> Result<Vec<RequestWorker>> {
    let mut workers = Vec::new();
    for mut job in crate::automation::jobs(ctx)? {
        let id = s(&job["id"]);
        if !valid_job(id) || !RUNNING.contains(&s(&job["status"])) {
            continue;
        }
        // The intake repair/verification job itself can quote affected thread
        // IDs. It is not the worker answering those requests.
        if std::env::var("CC_JOB_ID").is_ok_and(|current| current == id) {
            continue;
        }
        let associations = ctx.jobs.join("jobs").join(id).join("discord-threads.json");
        let mut legacy_thread_ids = Vec::new();
        if s(&job["discord_thread_ids_source"]) == "prompt" {
            legacy_thread_ids = job["discord_thread_ids"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|id| id.as_str().map(str::to_owned))
                .collect();
            job.as_object_mut().unwrap().remove("discord_thread_ids");
        }
        if associations.is_file() {
            let threads = read_json(&associations)?;
            if !threads["thread_ids"].is_array() {
                return Err("invalid Discord thread associations".into());
            }
            if s(&threads["source"]) == "prompt" {
                legacy_thread_ids = threads["thread_ids"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .filter_map(|id| id.as_str().map(str::to_owned))
                    .collect();
            } else {
                job["discord_thread_ids"] = threads["thread_ids"].clone();
            }
        }
        let id = s(&job["id"]);
        let path = ctx.jobs.join("jobs").join(id).join("prompt.md");
        let prompt = if path.is_file() {
            read_text_detail(&path)?
        } else {
            String::new()
        };
        if s(&job["discord_thread_id"]).is_empty()
            && !truth(&job["discord_thread_ids"])
            && legacy_thread_ids.is_empty()
        {
            legacy_thread_ids = prompt_thread_ids(&prompt);
        }
        workers.push(RequestWorker {
            job,
            legacy_thread_ids,
        });
    }
    Ok(workers)
}

fn running_request_owner<'a>(
    ctx: &Context,
    tid: &str,
    workers: &'a [RequestWorker],
) -> Result<Option<&'a RequestWorker>> {
    let mut owners = Vec::new();
    let mut explicit_owners = Vec::new();
    for worker in workers {
        let job = &worker.job;
        let id = s(&job["id"]);
        if !valid_job(id) || !RUNNING.contains(&s(&job["status"])) {
            continue;
        }
        let explicit = explicit_thread_owner(job, tid);
        if (explicit || worker.legacy_thread_ids.iter().any(|id| id == tid))
            && RUNNING.contains(&s(&job_info(ctx, id)["status"]))
        {
            if explicit {
                explicit_owners.push(worker);
            } else {
                owners.push(worker);
            }
        }
    }
    if !explicit_owners.is_empty() {
        owners = explicit_owners;
    }
    match owners.len() {
        0 => Ok(None),
        1 => Ok(owners.pop()),
        _ => Err(format!(
            "multiple running jobs already own request thread {tid}: {}",
            owners
                .iter()
                .map(|worker| s(&worker.job["id"]))
                .collect::<Vec<_>>()
                .join(", ")
        )),
    }
}
fn clean_history(ctx: &mut Context, tid: &str, latest: u64) -> Result<Vec<Value>> {
    Ok(thread_history(ctx, tid)?
        .into_iter()
        .filter(|m| n(&m["id"]) <= latest)
        .map(|mut m| {
            m["content"] = json!(safe_text(s(&m["content"])));
            m
        })
        .collect())
}

/// One running request per thread; diagnoses are separate. At most one launch per tick.
pub fn cmd_intake(ctx: &mut Context, budget: &mut bool) -> Result<()> {
    let _work = crate::state_work::StateWork::begin(ctx)?;
    let mut state = ctx.load()?;
    recover_reply_reads(ctx, &mut state)?;
    migrate_reply_delivery(ctx, &mut state)?;
    if !state["requests"].is_object() {
        state["requests"] = json!({});
    }
    if state["reply_intake_since"].is_null() {
        state["reply_intake_since"] =
            json!((((ctx.now() * 1000. - 1420070400000.).max(0.) as u64) << 22).to_string());
        ctx.save(&state)?;
    }
    let cfg = ctx.cfg.clone();
    let owner = ctx.owner();
    // Discovery has a separate slice of the phase. Slow legacy destinations
    // cannot take the time reserved for known threads' owner messages.
    let mut threads = crate::tick_runtime::stage(
        "thread discovery",
        std::time::Duration::from_secs(4),
        || request_threads(ctx, &cfg, &mut state),
    )?;
    threads.sort_by(|a, b| {
        state["requests"][s(&a["id"])]["last_launch_attempt_at"]
            .as_f64()
            .unwrap_or(0.)
            .total_cmp(
                &state["requests"][s(&b["id"])]["last_launch_attempt_at"]
                    .as_f64()
                    .unwrap_or(0.),
            )
    });
    let mut workers = request_workers(ctx)?;
    if let Some(cursor) = state["intake_scan_after"].as_str() {
        if let Some(at) = threads.iter().position(|t| s(&t["id"]) == cursor) {
            let length = threads.len();
            threads.rotate_left((at + 1) % length.max(1));
        }
    }
    for thread in threads {
        if crate::tick_runtime::check().is_err() {
            break;
        }
        let _span = crate::tick_runtime::Span::new(format!("intake thread {}", s(&thread["id"])));
        let before = *budget;
        if let Err(err) =
            intake_thread_with_workers(ctx, &mut state, &thread, &owner, budget, &workers)
        {
            eprintln!(
                "intake thread failed {} {}",
                s(&thread["id"]),
                error_detail(&err)
            );
        }
        state["intake_scan_after"] = thread["id"].clone();
        ctx.save(&state)?;
        if !before && *budget {
            workers = request_workers(ctx)?;
        }
    }
    Ok(())
}

/// Repair one specific reply through the normal durable steering launcher.
/// A receipt or a successful owning-session read is returned without another launch.
pub fn deliver_reply(
    ctx: &mut Context,
    tid: &str,
    mid: &str,
    source: Option<&str>,
) -> Result<Value> {
    deliver_reply_inner(ctx, tid, mid, source, None)
}

/// An operator can reconcile work explicitly verified as already handled.
pub fn confirm_reply(ctx: &mut Context, tid: &str, mid: &str, jid: &str) -> Result<Value> {
    deliver_reply_inner(ctx, tid, mid, None, Some(jid))
}

fn deliver_reply_inner(
    ctx: &mut Context,
    tid: &str,
    mid: &str,
    source: Option<&str>,
    confirmed: Option<&str>,
) -> Result<Value> {
    if [tid, mid].iter().any(|id| {
        id.is_empty()
            || !id.chars().all(|c| c.is_ascii_digit())
            || id.parse::<u64>().ok().is_none_or(|n| n == 0)
    }) {
        return Err("thread/message must be Discord snowflakes".into());
    }
    let _tick = ByteLock::acquire(&ctx.here.join("tick.lock"), Duration::from_secs(45))?;
    let _work = crate::state_work::StateWork::begin(ctx)?;
    let mut state = ctx.load()?;
    migrate_reply_delivery(ctx, &mut state)?;
    let thread = ctx.call("GET", &format!("/channels/{tid}"), None)?;
    let known = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .rev()
        .find(|q| s(&q["thread_id"]) == tid && s(&q["source"]) != "routing")
        .cloned();
    if s(&thread["id"]) != tid
        || ![10, 11, 12].contains(&n(&thread["type"]))
        || thread["guild_id"] != ctx.cfg["guild_id"]
    {
        return Err("configured guild thread required".into());
    }
    if s(&thread["parent_id"]) != REQUEST_CHANNEL_ID {
        if thread["parent_id"] != ctx.cfg["task_channel_id"] {
            return Err("request or bot task thread required".into());
        }
        let me = ctx.call("GET", "/users/@me", None)?;
        if me["bot"] != true || s(&me["id"]).is_empty() || thread["owner_id"] != me["id"] {
            return Err("request or bot task thread required".into());
        }
    }
    let message = ctx.call("GET", &format!("/channels/{tid}/messages/{mid}"), None)?;
    let owner = ctx.owner();
    if s(&message["id"]) != mid
        || s(&message["author"]["id"]) != owner
        || crate::snooze::control_only(&message)
    {
        return Err("owner work reply required".into());
    }
    if let Some(question) = &known {
        crate::steering::confirm_recorded_reads(
            ctx,
            &mut state,
            question,
            std::slice::from_ref(&message),
        )?;
    }
    if truth(&state["answer_receipts"][mid]["handled_by"]) {
        return Ok(
            json!({"thread":tid,"message":mid,"job":state["answer_receipts"][mid]["handled_by"],"delivery":"confirmed"}),
        );
    }
    if confirmed.is_none() && truth(&state["answer_receipts"][mid]["read_by"]) {
        return Ok(
            json!({"thread":tid,"message":mid,"read_by":state["answer_receipts"][mid]["read_by"],"delivery":"confirmed"}),
        );
    }
    if confirmed.is_none() && reply_reserved(&state, mid) {
        return Ok(json!({"thread":tid,"message":mid,"delivery":"pending"}));
    }
    if let Some(jid) = confirmed {
        if !valid_job(jid) || s(&job_info(ctx, jid)["status"]) == "missing" {
            return Err("a recorded handled job is required".into());
        }
        if !state["answer_receipts"].is_object() {
            state["answer_receipts"] = json!({});
        }
        state["answer_receipts"][mid]["thread"] = json!(tid);
        state["answer_receipts"][mid]["handled_by"] = json!(jid);
        state["answer_receipts"][mid]["handled_via"] = json!("manual-confirmation");
        ctx.save(&state)?;
        println!("owner reply manually confirmed {tid} {mid} {jid}");
        return Ok(json!({"thread":tid,"message":mid,"job":jid,"delivery":"confirmed"}));
    }
    if s(&state["answer_receipts"][mid]["handled_via"]) == "legacy-seen" {
        state["answer_receipts"][mid]
            .as_object_mut()
            .unwrap()
            .remove("handled_via");
    }
    let associated = known
        .as_ref()
        .map(|q| crate::steering::associated_job(ctx, q))
        .transpose()?
        .flatten();
    let pending_source = state["requests"][tid]["owner_update"]["source"]["id"]
        .as_str()
        .or_else(|| {
            known
                .as_ref()
                .and_then(|q| q["continuation"]["owner_update"]["source"]["id"].as_str())
        })
        .map(str::to_owned);
    let jid = source
        .or(associated.as_deref())
        .or(pending_source.as_deref())
        .ok_or("source job is missing; reply remains pending")?;
    if !valid_job(jid) {
        return Err("invalid source job".into());
    }
    let info = job_info(ctx, jid);
    let question_index = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .enumerate()
        .filter(|(_, q)| {
            s(&q["thread_id"]) == tid
                && s(&q["source"]) != "request"
                && truth(&q["open"])
                && truth(&q["cwd"])
        })
        .map(|(i, _)| i)
        .last();
    let slot = question_index
        .map(crate::restart::Slot::Question)
        .unwrap_or(crate::restart::Slot::Request);
    let mut record = question_index
        .map(|i| state["questions"][i].clone())
        .unwrap_or_else(|| state["requests"][tid].clone());
    if record.is_null() {
        record = json!({"title":thread["name"]});
    }
    let question_after = n(&record["after"]);
    let entry = if question_index.is_some() {
        if !record["continuation"].is_object() {
            record["continuation"] = json!({});
        }
        &mut record["continuation"]
    } else {
        &mut record
    };
    let reserved_update = truth(&entry["owner_update"]);
    if !reserved_update
        && !RUNNING.contains(&s(&info["status"]))
        && s(&info["status"]) != "succeeded"
    {
        return Err("source job must be running or succeeded".into());
    }
    if !state["requests"].is_object() {
        state["requests"] = json!({});
    }
    if !reserved_update {
        entry["job"] = json!(jid);
        entry["job_info"] = info;
        entry["selection"] = entry["job_info"].clone();
        let prior = entry
            .get("after")
            .map(n)
            .unwrap_or(if question_index.is_some() {
                question_after
            } else {
                mid.parse::<u64>().map_err(|e| e.to_string())? - 1
            });
        entry["after"] = json!(prior
            .min(mid.parse::<u64>().map_err(|e| e.to_string())? - 1)
            .to_string());
        entry["reported"] = json!(false);
        if question_index.is_none() {
            entry["external_owner"] = json!(true);
        }
    }
    // Link this explicit repair to the existing question owner, preventing a second
    // question handler from launching during a lost launch response.
    if let Some(i) = question_index {
        state["questions"][i] = record.clone();
    } else {
        state["requests"][tid] = record.clone();
    }
    ctx.save(&state)?;
    let result = crate::steering::deliver(
        ctx,
        &mut state,
        &thread,
        &mut record,
        slot,
        &owner,
        &mut false,
    );
    if let Err(error) = &result {
        record["last_error"] = json!(error_detail(error));
        eprintln!(
            "owner reply repair failed {tid} {mid}: {}; retry next tick",
            error_detail(error)
        );
    }
    if let Some(i) = question_index {
        state["questions"][i] = record;
    } else {
        state["requests"][tid] = record;
    }
    ctx.save(&state)?;
    result?;
    Ok(
        json!({"thread":tid,"message":mid,"job":state["answer_receipts"][mid]["handled_by"],"delivery":if truth(&state["answer_receipts"][mid]["handled_by"]) {"delivered"} else {"pending"}}),
    )
}
