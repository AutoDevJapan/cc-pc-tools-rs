//! Deliver owner corrections through durable short state checkpoints.
use crate::{core::*, project::ProjectNames, restart::Slot};
use serde_json::{json, Value};
use std::time::Duration;

fn save(
    ctx: &Context,
    state: &mut Value,
    thread: &Value,
    record: &Value,
    slot: Slot,
) -> Result<()> {
    match slot {
        Slot::Request => state["requests"][s(&thread["id"])] = record.clone(),
        Slot::Question(i) => state["questions"][i] = record.clone(),
    }
    ctx.save(state)
}
fn entry(record: &Value, slot: Slot) -> &Value {
    match slot {
        Slot::Request => record,
        Slot::Question(_) => &record["continuation"],
    }
}
fn entry_mut(record: &mut Value, slot: Slot) -> &mut Value {
    match slot {
        Slot::Request => record,
        Slot::Question(_) => &mut record["continuation"],
    }
}

/// A durable receipt per owner message, shared with replies/intake/resume.
pub fn acknowledge(
    ctx: &mut Context,
    state: &mut Value,
    thread: &str,
    project: &str,
    messages: &[Value],
) -> Result<()> {
    record_owner_answers(state, thread, messages, &ctx.owner());
    let names = ProjectNames::load(&ctx.client_projects)?;
    let content = format!(
        "【{}】受け取りました。今の作業に反映します。",
        names.label(project)
    );
    for message in messages.iter().filter(|m| !crate::snooze::control_only(m)) {
        let id = s(&message["id"]);
        if truth(&state["answer_receipts"][id]["posted"]) {
            continue;
        }
        if !state["answer_receipts"].is_object() {
            state["answer_receipts"] = json!({});
        }
        if state["answer_receipts"][id]["content"].is_null() {
            state["answer_receipts"][id]["thread"] = json!(thread);
            state["answer_receipts"][id]["content"] = json!(content);
            state["answer_receipts"][id]["posted"] = json!(false);
            ctx.save(state)?;
        } else if thread_history(ctx, thread)?.iter().any(|m| {
            m["message_reference"]["message_id"] == message["id"]
                && m["content"] == state["answer_receipts"][id]["content"]
                && truth(&m["author"]["bot"])
        }) {
            state["answer_receipts"][id]["posted"] = json!(true);
            ctx.save(state)?;
            continue;
        }
        let response = ctx.call("POST", &format!("/channels/{thread}/messages"), Some(&json!({
            "content":state["answer_receipts"][id]["content"],"flags":SILENT,
            "allowed_mentions":{"parse":[],"users":[],"replied_user":false},
            "nonce":id,"enforce_nonce":true,"message_reference":{"message_id":id,"channel_id":thread}
        })))?;
        if s(&response["id"]).is_empty() {
            return Err("receipt returned no message ID".into());
        }
        state["answer_receipts"][id]["posted"] = json!(true);
        ctx.save(state)?;
    }
    Ok(())
}

/// Resolve only an explicit thread association; an issue number or working folder is insufficient.
pub fn associated_job(ctx: &Context, question: &Value) -> Result<Option<String>> {
    let tid = s(&question["thread_id"]);
    let mut found = vec![];
    let mut live = vec![];
    for job in crate::automation::jobs(ctx)? {
        let id = s(&job["id"]);
        if !valid_job(id)
            || !["queued", "starting", "running", "succeeded"].contains(&s(&job["status"]))
        {
            continue;
        }
        let info = job_info(ctx, id);
        let explicit = s(&info["discord_thread_id"]);
        if !explicit.is_empty() && explicit != tid && !explicit_thread_owner(&info, tid) {
            continue;
        }
        let prompt = ctx.jobs.join("jobs").join(id).join("prompt.md");
        let refers = prompt.is_file() && prompt_thread_owner(&read_text(&prompt)?, tid);
        let session = s(&question["session_id"]);
        if explicit_thread_owner(&info, tid)
            || s(&question["job"]) == id
            || s(&question["scheduler_job"]) == id
            || refers
            || (!session.is_empty()
                && (s(&job["thread_id"]) == session || s(&job["resume_session"]) == session))
        {
            if RUNNING.contains(&s(&job["status"])) {
                live.push(id.to_owned());
            } else {
                found.push((crate::automation::epoch(&job["started_at"]), id.to_owned()));
            }
        }
    }
    if !live.is_empty() {
        return Ok(if live.len() == 1 { live.pop() } else { None });
    }
    found.sort_by(|a, b| a.0.total_cmp(&b.0));
    Ok(found.pop().map(|(_, id)| id))
}

/// Only a successful replies command in the owning job proves a manual delivery.
pub fn recorded_read(ctx: &Context, jid: &str, tid: &str, message: &Value) -> Result<bool> {
    reply_seen_in_job(ctx, jid, tid, message, false)
}

pub fn reply_seen_in_job(
    ctx: &Context,
    jid: &str,
    tid: &str,
    message: &Value,
    inspection: bool,
) -> Result<bool> {
    let path = ctx.jobs.join("jobs").join(jid).join("events.jsonl");
    if !path.is_file() {
        return Ok(false);
    }
    use std::io::{BufRead, BufReader};
    let file = std::fs::File::open(path).map_err(|e| e.to_string())?;
    for line in BufReader::new(file).lines() {
        crate::tick_runtime::check()?;
        let line = line.map_err(|e| e.to_string())?;
        let Ok(event) = serde_json::from_str::<Value>(&line) else {
            continue;
        };
        let item = &event["item"];
        let command = s(&item["command"]);
        if item["exit_code"] != 0
            || !(command.contains("replies") || (inspection && command.contains("post")))
            || !command.contains(tid)
            || (!inspection && command.contains("--read-only"))
        {
            continue;
        }
        let output = s(&item["aggregated_output"]);
        for (offset, _) in output
            .char_indices()
            .filter(|(_, c)| ['[', '{'].contains(c))
        {
            let Some(Ok(value)) = serde_json::Deserializer::from_str(&output[offset..])
                .into_iter::<Value>()
                .next()
            else {
                continue;
            };
            if inspection
                && s(&value["thread_id"]) == tid
                && value["latest_owner_reply"]["id"] == message["id"]
            {
                return Ok(true);
            }
            for record in value
                .as_array()
                .into_iter()
                .flatten()
                .filter(|r| s(&r["thread_id"]) == tid)
            {
                if record["replies"].as_array().into_iter().flatten().any(|r| {
                    if r["id"] == message["id"] {
                        return true;
                    }
                    let parse = |v: &Value| {
                        chrono::DateTime::parse_from_rfc3339(s(v))
                            .ok()
                            .map(|t| t.timestamp_millis())
                    };
                    r["content"] == json!(safe_text(s(&message["content"])))
                        && parse(&r["timestamp"]).is_some()
                        && parse(&r["timestamp"]) == parse(&message["timestamp"])
                }) {
                    return Ok(true);
                }
            }
        }
    }
    Ok(false)
}

pub fn confirm_recorded_reads(
    ctx: &Context,
    state: &mut Value,
    question: &Value,
    messages: &[Value],
) -> Result<()> {
    confirm_original_inputs(ctx, state, s(&question["thread_id"]), messages)?;
    let pending: Vec<_> = messages
        .iter()
        .filter(|m| {
            n(&m["id"]) <= n(&question["seen_reply"])
                && !truth(&state["answer_receipts"][s(&m["id"])]["handled_by"])
        })
        .collect();
    if pending.is_empty() {
        return Ok(());
    }
    let Some(jid) = associated_job(ctx, question)? else {
        return Ok(());
    };
    for message in pending {
        if recorded_read(ctx, &jid, s(&question["thread_id"]), message)? {
            let id = s(&message["id"]);
            state["answer_receipts"][id]["thread"] = question["thread_id"].clone();
            state["answer_receipts"][id]["handled_by"] = json!(jid);
            state["answer_receipts"][id]["handled_via"] = json!("replies-event");
            println!(
                "owner reply confirmed {} {id} {jid}",
                s(&question["thread_id"])
            );
        }
    }
    ctx.save(state)
}

/// Manual launches used a dated owner quote before Discord input IDs existed.
/// Recognize only that input header, exact text/minute and matching thread scope;
/// copied conversation later in the prompt is never evidence of delivery.
pub fn confirm_original_inputs(
    ctx: &Context,
    state: &mut Value,
    thread: &str,
    messages: &[Value],
) -> Result<()> {
    let owner = ctx.owner();
    let pending: Vec<_> = messages
        .iter()
        .filter(|m| {
            s(&m["author"]["id"]) == owner
                && !crate::snooze::control_only(m)
                && !reply_unavailable(state, s(&m["id"]))
        })
        .collect();
    if pending.is_empty() {
        return Ok(());
    }
    let quote = regex::Regex::new(r#"^(?:オーナーの(?:指摘|回答|依頼)|Owner request)\s*[（(]\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}) JST[）)]\s*[:：]\s*[「"](.+)[」"]\s*$"#).unwrap();
    let normalize = |text: &str| {
        regex::Regex::new(r"<@!?[0-9]+>|\s+")
            .unwrap()
            .replace_all(text, "")
            .into_owned()
    };
    let mut jobs = crate::automation::jobs(ctx)?;
    jobs.sort_by(|a, b| {
        crate::automation::epoch(&a["created_at"])
            .total_cmp(&crate::automation::epoch(&b["created_at"]))
    });
    let mut changed = false;
    for job in jobs
        .iter()
        .filter(|j| RUNNING.contains(&s(&j["status"])) || s(&j["status"]) == "succeeded")
    {
        let jid = s(&job["id"]);
        if !valid_job(jid) {
            continue;
        }
        let info = job_info(ctx, jid);
        let job = &info;
        let declared =
            !s(&job["discord_thread_id"]).is_empty() || truth(&job["discord_thread_ids"]);
        let short = &thread[thread.len().saturating_sub(6)..];
        if !(explicit_thread_owner(job, thread)
            || (!declared
                && (jid.ends_with(&format!("req-{short}"))
                    || s(&job["name"]) == format!("req-{short}"))))
        {
            continue;
        }
        let path = ctx.jobs.join("jobs").join(jid).join("prompt.md");
        if !path.is_file() {
            continue;
        }
        let input = read_text(&path)?;
        let Some(header) = input.lines().find(|line| !line.trim().is_empty()) else {
            continue;
        };
        let Some(capture) = quote.captures(header.trim()) else {
            continue;
        };
        let text = normalize(&capture[2]);
        for message in &pending {
            if reply_delivered(state, s(&message["id"])) {
                continue;
            }
            let Ok(at) = chrono::DateTime::parse_from_rfc3339(s(&message["timestamp"])) else {
                continue;
            };
            let local = at
                .with_timezone(&chrono::FixedOffset::east_opt(9 * 3600).unwrap())
                .format("%Y-%m-%d %H:%M")
                .to_string();
            let created = crate::automation::epoch(&job["created_at"]);
            let answered = at.timestamp_millis() as f64 / 1000.;
            if local != capture[1]
                || normalize(s(&message["content"])) != text
                || created < answered
                || created - answered > 86400.
            {
                continue;
            }
            let id = s(&message["id"]);
            state["answer_receipts"][id]["thread"] = json!(thread);
            state["answer_receipts"][id]["handled_by"] = json!(jid);
            state["answer_receipts"][id]["handled_via"] = json!("original-owner-input");
            changed = true;
        }
    }
    if changed {
        ctx.save(state)?;
    }
    Ok(())
}

pub fn resume_unavailable(info: &Value) -> bool {
    let error = s(&info["last_error"]).to_lowercase();
    [
        "session not found",
        "no rollout found",
        "cannot resume",
        "could not resume",
        "failed to resume",
        "session does not exist",
        "session file not found",
    ]
    .iter()
    .any(|x| error.contains(x))
}

fn quotes(messages: &[Value]) -> String {
    messages
        .iter()
        .map(|m| {
            let files = m["attachments"]
                .as_array()
                .into_iter()
                .flatten()
                .map(|a| format!("\n添付: {}", s(&a["url"])))
                .collect::<String>();
            format!(
                "\nオーナー投稿 {}（{}）の原文:\n{}{}\n",
                s(&m["id"]),
                s(&m["timestamp"]),
                s(&m["content"]),
                files
            )
        })
        .collect()
}

pub fn addressed(entry: &Value) -> String {
    let Some(messages) = entry["steering_messages"].as_array() else {
        return String::new();
    };
    let lines = messages
        .iter()
        .map(|m| {
            format!(
                "- {} の投稿「{}」（投稿 {}）",
                s(&m["timestamp"]),
                safe_text(s(&m["content"]))
                    .chars()
                    .take(60)
                    .collect::<String>()
                    .replace(['\r', '\n'], " "),
                s(&m["id"])
            )
        })
        .collect::<Vec<_>>()
        .join("\n");
    format!("\n\n今回反映したオーナーの投稿:\n{lines}")
}

/// True means the old job's reply must be withheld, even if delivery is temporarily blocked.
pub fn deliver(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    record: &mut Value,
    slot: Slot,
    owner: &str,
    budget: &mut bool,
) -> Result<bool> {
    let tid = s(&thread["id"]);
    // Recover a launch response/checkpoint loss before considering another cancellation.
    let pending = entry(record, slot)["owner_update"].clone();
    if pending.is_object() {
        if let Some(jid) = find_launch_job(ctx, s(&pending["key"])) {
            adopt(ctx, state, thread, record, slot, &pending, &jid)?;
        } else if let Some(messages) = pending["messages"].as_array().filter(|ms| !ms.is_empty()) {
            if messages.iter().any(|m| reply_reserved(state, s(&m["id"]))) {
                return Ok(true);
            }
            let remaining: Vec<_> = messages
                .iter()
                .filter(|m| !reply_delivered(state, s(&m["id"])))
                .cloned()
                .collect();
            if remaining.is_empty() {
                entry_mut(record, slot)
                    .as_object_mut()
                    .unwrap()
                    .remove("owner_update");
                save(ctx, state, thread, record, slot)?;
            } else if remaining.len() != messages.len() {
                let mut pending = pending.clone();
                pending["messages"] = json!(remaining);
                pending["prompt"] = json!(format!("オーナーからの今回未配送の追加指示を最優先してください。\n{}\n{REQUEST_PREAMBLE}\nスレッドID: {tid}\n作業フォルダ: {}\n元の依頼・作業指示:\n{}", quotes(&remaining),s(&pending["source"]["cwd"]),s(&pending["original_request"])));
                entry_mut(record, slot)["owner_update"] = pending;
                save(ctx, state, thread, record, slot)?;
            }
        }
    }
    let current = entry(record, slot).clone();
    let jid = s(&current["job"]);
    if jid.is_empty() || jid == "launch-failed" || truth(&current["reported"]) {
        return Ok(false);
    }
    let history = thread_history(ctx, tid)?;
    let unseen: Vec<_> = history
        .iter()
        .filter(|m| {
            s(&m["author"]["id"]) == owner
                && n(&m["id"]) > n(&current["after"])
                && !crate::snooze::control_only(m)
                && !reply_unavailable(state, s(&m["id"]))
        })
        .cloned()
        .collect();
    let info = job_info(ctx, jid);
    let unavailable = truth(&current["steering_resume"])
        && s(&info["status"]) == "failed"
        && resume_unavailable(&info);
    if !RUNNING.contains(&s(&info["status"]))
        && s(&info["status"]) != "succeeded"
        && !current["owner_update"].is_object()
        && !unavailable
    {
        return Ok(false);
    }
    if unseen.is_empty() && !current["owner_update"].is_object() && !unavailable {
        return Ok(false);
    }
    let names = ProjectNames::load(&ctx.client_projects)?;
    let mut labelled = record.clone();
    if s(&labelled["title"]).is_empty() {
        labelled["title"] = thread["name"].clone();
    }
    if s(&labelled["cwd"]).is_empty() {
        labelled["cwd"] = info["cwd"].clone();
    }
    let project = crate::project::question_project(ctx, &labelled, &names);
    acknowledge(ctx, state, tid, &project, &unseen)?;
    // Keep the same start gate as scheduled recovery; no other thread/job is cancelled.
    let root = ctx
        .scheduler_command
        .parent()
        .ok_or("missing scheduler directory")?;
    let Ok(_gate) = ByteLock::acquire(&root.join("scheduler.lock"), Duration::ZERO) else {
        eprintln!("owner update deferred {tid}: scheduler.lock is busy; retry next tick");
        return Ok(true);
    };
    if !truth(&current["external_owner"])
        && !s(&info["discord_thread_id"]).is_empty()
        && s(&info["discord_thread_id"]) != tid
        && !explicit_thread_owner(&info, tid)
    {
        return Err("tracked job belongs to another Discord thread".into());
    }
    // Waiting for another writer must not turn into a fresh duplicate worker.
    let mut ownership = info.clone();
    ownership["id"] = json!(jid);
    let db = if ctx.todo.is_file() {
        read_json(&ctx.todo)?
    } else {
        json!({})
    };
    if let Some(other) = crate::restart::other_owner(ctx, thread, &ownership, &db)? {
        eprintln!("owner update deferred {tid}: other owner {other}; retry next tick");
        return Ok(true);
    }
    if *budget {
        eprintln!("owner update deferred {tid}: tick launch budget used; retry next tick");
        return Ok(true);
    }
    if !current["owner_update"].is_object() {
        let mut source = info.clone();
        if s(&source["status"]) == "missing" {
            source = current["job_info"].clone();
        }
        source["id"] = json!(jid);
        for field in ["model", "effort", "task_type", "sandbox", "scope_source"] {
            if !truth(&source[field]) {
                source[field] = current["selection"][field].clone();
            }
        }
        if !truth(&source["cwd"]) {
            source["cwd"] = current["cwd"].clone();
        }
        if !truth(&source["cwd"]) {
            source["cwd"] = json!(REQUEST_CWD);
        }
        if !truth(&source["model"]) || !truth(&source["effort"]) {
            return Err("owner update missing job configuration".into());
        }
        let session = source["thread_id"]
            .as_str()
            .filter(|x| !x.is_empty())
            .or_else(|| source["resume_session"].as_str())
            .unwrap_or("");
        let session_busy = crate::automation::jobs(ctx)?.iter().any(|j| {
            s(&j["id"]) != jid
                && RUNNING.contains(&s(&j["status"]))
                && !session.is_empty()
                && (s(&j["thread_id"]) == session || s(&j["resume_session"]) == session)
        });
        let resume = !unavailable
            && !session_busy
            && !session.is_empty()
            && source["context_pct"].as_f64().unwrap_or(0.) < 75.;
        let original_path = ctx.jobs.join("jobs").join(jid).join("prompt.md");
        let original = if let Some(original) = current["original_request"].as_str() {
            original.to_owned()
        } else if original_path.is_file() {
            read_text(&original_path)?
        } else {
            s(&current["prompt"]).to_owned()
        };
        let owners: Vec<_> = history
            .iter()
            .filter(|m| s(&m["author"]["id"]) == owner)
            .cloned()
            .collect();
        let mut addressed_messages = current["steering_messages"]
            .as_array()
            .cloned()
            .unwrap_or_default();
        addressed_messages.extend(unseen.clone());
        let latest = owners
            .last()
            .map(|m| m["id"].clone())
            .unwrap_or(current["after"].clone());
        let last = final_message(ctx, jid);
        let last = if last.is_empty() {
            s(&source["last_message"]).to_owned()
        } else {
            last
        };
        let key = uuid::Uuid::new_v4().simple().to_string();
        let original =
            crate::prompt_context::source(ctx, &key, &original, &original_path, 12 * 1024)?;
        let conversation =
            crate::prompt_context::conversation(ctx, thread, &history, owner, 16 * 1024)?;
        let owners = crate::prompt_context::conversation(ctx, thread, &owners, owner, 8 * 1024)?;
        let last = crate::prompt_context::bounded(
            &last,
            4 * 1024,
            &ctx.jobs
                .join("jobs")
                .join(jid)
                .join("last_message.md")
                .to_string_lossy(),
        );
        let handoff = format!("{REQUEST_PREAMBLE}\nスレッドID: {tid}\n作業フォルダ: {}\n元の依頼・作業指示:\n{original}\nスレッドの冒頭を含む会話:\n{conversation}\nオーナーの全投稿（古い順）:\n{owners}\n前のジョブの最後のメッセージ:\n{last}\n", s(&source["cwd"]));
        let prompt = format!("オーナーから追加の指示が届きました。以下の新しい投稿を以前の指示より優先して作業に反映してください。投稿は原文です。\n{}\n最終返信には今回反映した投稿の日時と内容を明記してください。\n{handoff}", quotes(&unseen));
        entry_mut(record, slot)["owner_update"] = json!({"key":key,"source":source,"session":session,"resume":resume,"prompt":prompt,"original_request":original,"after":latest,"messages":addressed_messages});
        save(ctx, state, thread, record, slot)?;
    }
    let mut pending = entry(record, slot)["owner_update"].clone();
    // New arrivals during a failed launch are added to the same durable reservation.
    let included = n(&pending["after"]);
    let extra: Vec<_> = unseen
        .iter()
        .filter(|m| n(&m["id"]) > included)
        .cloned()
        .collect();
    if let Some(last) = extra.last() {
        pending["prompt"] = json!(format!(
            "さらに届いた次の原文を最優先してください。\n{}\n{}",
            quotes(&extra),
            s(&pending["prompt"])
        ));
        pending["after"] = last["id"].clone();
        pending["messages"].as_array_mut().unwrap().extend(extra);
        entry_mut(record, slot)["owner_update"] = pending.clone();
        save(ctx, state, thread, record, slot)?;
    }
    let source = &pending["source"];
    let old = s(&source["id"]).to_owned();
    if RUNNING.contains(&s(&job_info(ctx, &old)["status"])) {
        let out = ctx.run(
            &command_argv(
                &ctx.codex_command,
                &["cancel".into(), old.clone(), "--recovery".into()],
            ),
            45,
        )?;
        // A completion racing cancellation is harmless, but a live process may not overlap the resume.
        if out.code != 0 && RUNNING.contains(&s(&job_info(ctx, &old)["status"])) {
            return Err("could not stop owner update source".into());
        }
    }
    let mut selection = pending["source"].clone();
    let newest = pending["messages"]
        .as_array()
        .and_then(|messages| messages.last());
    apply_job_scope(
        &mut selection,
        &pending["source"],
        newest.map(|m| s(&m["content"])),
    );
    selection["verbatim_owner_messages"] = json!(true);
    *budget = true;
    record["last_launch_attempt_at"] = json!(ctx.now());
    save(ctx, state, thread, record, slot)?;
    let launch = |ctx: &mut Context, p: &Value| {
        launch_job_with_session(
            ctx,
            thread,
            s(&p["prompt"]),
            &selection,
            s(&p["key"]),
            false,
            Some(s(&selection["cwd"])),
            Some("resume"),
            if truth(&p["resume"]) {
                Some(s(&p["session"]))
            } else {
                None
            },
        )
    };
    let result = launch(ctx, &pending);
    let result = result.or_else(|error| find_launch_job(ctx, s(&pending["key"])).ok_or(error));
    let jid = match result {
        Ok(jid) => jid,
        Err(error)
            if truth(&pending["resume"]) && resume_unavailable(&json!({"last_error":error})) =>
        {
            pending["resume"] = json!(false);
            pending["key"] = json!(uuid::Uuid::new_v4().simple().to_string());
            entry_mut(record, slot)["owner_update"] = pending.clone();
            save(ctx, state, thread, record, slot)?;
            launch(ctx, &pending)?
        }
        Err(error) => return Err(error),
    };
    adopt(ctx, state, thread, record, slot, &pending, &jid)?;
    println!("owner update delivered {tid}: {old} -> {jid}");
    Ok(true)
}

fn adopt(
    ctx: &mut Context,
    state: &mut Value,
    thread: &Value,
    record: &mut Value,
    slot: Slot,
    pending: &Value,
    jid: &str,
) -> Result<()> {
    let e = entry_mut(record, slot);
    e["job"] = json!(jid);
    e["job_info"] = job_info(ctx, jid);
    e["selection"] = pending["source"].clone();
    e["original_request"] = pending["original_request"].clone();
    e["after"] = pending["after"].clone();
    e["steering_messages"] = pending["messages"].clone();
    for message in pending["messages"].as_array().into_iter().flatten() {
        state["answer_receipts"][s(&message["id"])]["handled_by"] = json!(jid);
    }
    e["steering_resume"] = pending["resume"].clone();
    e["reported"] = json!(false);
    for field in [
        "owner_update",
        "close_pending",
        "question_pending",
        "launch",
        "acknowledgement",
    ] {
        e.as_object_mut().unwrap().remove(field);
    }
    if matches!(slot, Slot::Request) {
        e.as_object_mut().unwrap().remove("external_owner");
    }
    if let Slot::Question(_) = slot {
        record["open"] = json!(true);
        acknowledge_reply(record, s(&pending["after"]));
    }
    save(ctx, state, thread, record, slot)?;
    crate::automation::link_todos(ctx, &[s(&pending["source"]["id"]).to_owned()], jid)
}
