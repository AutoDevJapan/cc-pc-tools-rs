//! Console commands compatible with the shared Python Discord question tool.
use crate::core::*;
use crate::project::*;
use serde_json::{json, Value};
use std::{
    collections::{BTreeMap, HashSet},
    io::{self, Write},
    path::{Path, PathBuf},
    time::Duration,
};

#[derive(Clone, Debug, Default, clap::Args)]
pub struct AskArgs {
    /// Link original work; only a genuinely new decision scope gets a new post.
    #[arg(long)]
    pub thread: Option<String>,
    #[arg(long = "attach")]
    pub attach: Vec<PathBuf>,
    #[arg(long, required = true)]
    pub title: String,
    #[arg(long, required = true)]
    pub body_file: PathBuf,
    /// Stable original ToDo, concrete resources, decision action and permission scope (JSON).
    #[arg(long)]
    pub decision_file: Option<PathBuf>,
    #[arg(long, default_value = "")]
    pub agent: String,
    #[arg(long)]
    pub cwd: Option<String>,
    #[arg(long)]
    pub source: Option<String>,
    #[arg(long)]
    pub scheduler_item: Option<u64>,
    #[arg(long)]
    pub scheduler_job: Option<String>,
    /// Owner decision only: money, publish, rights, customer, direction, irreversible.
    #[arg(long, value_enum)]
    pub owner_only: Option<crate::ask_gate::OwnerOnly>,
    /// Print the exact question formatting without posting or changing state.
    #[arg(long)]
    pub dry_run: bool,
}

fn lock(ctx: &Context) -> Result<ByteLock> {
    ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))
}

/// Serialize with Python's default comma/colon spacing and unescaped Unicode.
pub fn python_json(value: &Value, indent: Option<usize>) -> Result<String> {
    if let Some(width) = indent {
        let mut bytes = Vec::new();
        let spaces = vec![b' '; width];
        let mut ser = serde_json::Serializer::with_formatter(
            &mut bytes,
            serde_json::ser::PrettyFormatter::with_indent(&spaces),
        );
        serde::Serialize::serialize(value, &mut ser).map_err(|_| "ValueError".to_string())?;
        return String::from_utf8(bytes).map_err(|_| "UnicodeError".into());
    }
    fn render(v: &Value) -> String {
        match v {
            Value::Array(values) => format!(
                "[{}]",
                values.iter().map(render).collect::<Vec<_>>().join(", ")
            ),
            Value::Object(values) => format!(
                "{{{}}}",
                values
                    .iter()
                    .map(|(k, v)| format!("{}: {}", json!(k), render(v)))
                    .collect::<Vec<_>>()
                    .join(", ")
            ),
            _ => v.to_string(),
        }
    }
    Ok(render(value))
}

fn print_json(value: &Value, indent: Option<usize>) -> Result<()> {
    let text = python_json(value, indent)?;
    let mut out = io::stdout().lock();
    writeln!(out, "{text}")
        .and_then(|_| out.flush())
        .map_err(|_| "stdout write failed".into())
}

/// Show the bot's message-content permission and the configured task channel.
pub fn cmd_check(ctx: &mut Context) -> Result<Value> {
    let app = ctx.call("GET", "/applications/@me", None)?;
    let channel = ctx.call(
        "GET",
        &format!("/channels/{}", s(&ctx.cfg["task_channel_id"])),
        None,
    )?;
    let value = json!({"app":app["name"], "message_content_intent": n(&app["flags"]) & ((1 << 18) | (1 << 19)) != 0,
        "channel":channel["name"], "channel_type":channel["type"]});
    print_json(&value, None)?;
    Ok(value)
}

/// Ask in a forum or text channel, retaining the original working directory.
pub fn cmd_ask(ctx: &mut Context, args: &AskArgs) -> Result<Value> {
    let (raw_body, embedded) = crate::decision_identity::extract(&read_text(&args.body_file)?)?;
    if args.decision_file.is_some() && embedded.is_some() {
        return Err("判断の記録は1つだけ指定してください".into());
    }
    let decision = match &args.decision_file {
        Some(path) => Some(read_json(path)?),
        None => embedded,
    };
    crate::ask_gate::enforce(&args.title, &raw_body, args.owner_only)?;
    let cwd = match args.cwd.as_deref().filter(|v| !v.is_empty()) {
        Some(v) => v.to_owned(),
        None => std::env::current_dir()
            .map_err(|_| "OSError".to_string())?
            .to_string_lossy()
            .into_owned(),
    };
    let names = ProjectNames::load(&ctx.client_projects)?;
    let mut project = determine_project(ctx, &cwd, &names);
    let body = names.redact(&raw_body);
    let owner = ctx.owner();
    let mut title = thread_title(
        &project,
        &names.redact(&args.agent),
        &names.redact(&args.title),
    );
    if crate::task_title::clean(&args.title).is_empty() {
        let todo = if ctx.todo.is_file() {
            read_json(&ctx.todo)?
        } else {
            Value::Null
        };
        let privacy = crate::task_title::Names::load(&ctx.client_projects)
            .map_err(|_| "client project list unavailable")?;
        title = crate::task_title::job_title(
            &json!({"name":args.title,"cwd":cwd,"project":project}),
            &body,
            &todo,
            &privacy,
        );
        if let Some((label, _)) = title.strip_prefix('【').and_then(|v| v.split_once('】')) {
            project = label.into();
        }
    }
    let content = question_body(&project, &owner, &body);
    if args.dry_run {
        let value = json!({"project":project,"cwd":names.redact(&cwd),"title":title,"body":content,
            "reminder":reminder_text(&project, &owner, &title),"owner_only":args.owner_only.map(|r| r.as_str()),
            "decision_identity":decision.as_ref().map(|v| crate::decision_identity::normalize(ctx,v)).transpose()?});
        print_json(&value, Some(2))?;
        return Ok(value);
    }
    if let Some(origin) = &args.thread {
        if origin.is_empty() || !origin.bytes().all(|c| c.is_ascii_digit()) {
            return Err("元の作業投稿にはDiscordの数値IDを指定してください".into());
        }
        let channel = ctx.call("GET", &format!("/channels/{origin}"), None)?;
        if ![10, 11, 12].contains(&n(&channel["type"]))
            || channel["guild_id"] != ctx.cfg["guild_id"]
        {
            return Err("元の作業投稿は設定されたサーバーのスレッドではありません".into());
        }
        if !crate::questions::is_question(&body) {
            return Err("ask本文には具体的な質問を書いてください".into());
        }
    }
    let mut context = json!({"title":title,"ask_title":names.redact(&args.title),"project":project,
        "agent":args.agent,"cwd":cwd,"asked_at":timestamp(),"source":"posted"});
    if let Some(value) = decision {
        context["decision_identity"] = crate::decision_identity::normalize(ctx, &value)?;
        context["todo_id"] = context["decision_identity"]["todo_id"].clone();
    }
    if let Some(reason) = args.owner_only {
        context["owner_only"] = json!(reason.as_str());
    }
    if let Ok(session) = std::env::var("CODEX_THREAD_ID") {
        context["session_id"] = json!(session);
    }
    let caller = std::env::var("CC_JOB_ID")
        .ok()
        .filter(|j| valid_job(j))
        .or(crate::questions::posting_job(ctx)?);
    let mut origin = args.thread.clone().unwrap_or_default();
    if let Some(job) = caller {
        let info = job_info(ctx, &job);
        context["posting_job"] = json!(job);
        context["job"] = json!(job);
        context["job_info"] = info.clone();
        if !context["decision_identity"].is_object() {
            context["todo_id"] = info["todo_id"].clone();
        } else if n(&info["todo_id"]) > 0 && info["todo_id"] != context["todo_id"] {
            return Err("判断の元ToDoと担当のToDoが違います".into());
        }
        context["related_todo_ids"] = info["related_todo_ids"].clone();
        if origin.is_empty() {
            origin = s(&info["discord_thread_id"]).into();
        }
    }
    if args.source.as_deref() == Some("scheduler") {
        context["source"] = json!("scheduler");
        context["scheduler_item"] = json!(args.scheduler_item);
        context["scheduler_job"] = json!(args.scheduler_job);
    }
    // Recover only legacy interrupted creations; do not append a new question to
    // an old title match. New posting is keyed by the complete question intent.
    crate::ask_dedup::recover(ctx, &cwd, &names.redact(&args.title))?;
    if args.thread.is_none() && args.attach.is_empty() && !context["decision_identity"].is_object()
    {
        if let Some(q) =
            crate::ask_dedup::find_open(&ctx.load()?, &cwd, &title, &names.redact(&args.title))
        {
            let exact = if let Some(prior) = q["question_body"].as_str() {
                prior == body
            } else {
                let history = thread_history(ctx, s(&q["thread_id"]))?;
                history
                    .iter()
                    .any(|m| m["id"] == q["after"] && s(&m["content"]).trim() == body.trim())
            };
            if exact && (!truth(&q["separated_question"]) || truth(&q["posting_complete"])) {
                let replies = owner_replies(ctx, &q, &owner)?;
                let mut result = json!({"thread_id":q["thread_id"],"reused":true,"tracked":true,
                    "url":format!("https://discord.com/channels/{}/{}",s(&ctx.cfg["guild_id"]),s(&q["thread_id"])),
                    "progress_thread_id":q["progress_thread_id"],"suppressed":"same_question"});
                if let Some(answer) = replies
                    .iter()
                    .rev()
                    .find(|m| !crate::snooze::control_only(m))
                {
                    result["suppressed"] = json!("already_answered");
                    result["latest_owner_reply"] = json!({"id":answer["id"],"timestamp":answer["timestamp"],"content":safe_text(s(&answer["content"]))});
                }
                print_json(&result, None)?;
                return Ok(result);
            }
        }
    }
    let result = crate::question_progress::ask(ctx, &context, &origin, &body, &args.attach)?;
    print_json(&result, None)?;
    Ok(result)
}
/// Fetch without the writer lock; claim and return only undelivered replies under
/// that lock. Inspection of previously consumed replies uses --read-only.
pub fn cmd_replies(ctx: &mut Context, thread: Option<&str>) -> Result<Value> {
    let owner = ctx.owner();
    // Writers publish state.json by atomic replacement. Read a complete snapshot,
    // without queuing behind intake/resume's network and subprocess work.
    let state = ctx.load()?;
    let mut returned = Vec::new();
    for (index, question) in state["questions"]
        .as_array()
        .ok_or("ValueError")?
        .iter()
        .enumerate()
    {
        if !truth(&question["open"]) || thread.is_some_and(|tid| s(&question["thread_id"]) != tid) {
            continue;
        }
        if !crate::routing::question_allowed(ctx, question)? {
            continue;
        }
        let messages = owner_replies(ctx, question, &owner)?;
        returned.push((index, question.clone(), messages));
    }
    if !returned.iter().any(|(_, q, ms)| {
        ms.iter().any(|m| {
            (n(&m["id"]) > n(&q["seen_reply"])
                || reply_reserved(&state, s(&m["id"]))
                || truth(&state["answer_receipts"][s(&m["id"])]["read_retry"]))
                && !reply_delivered(&state, s(&m["id"]))
        })
    }) {
        let value = json!(returned.iter().map(|(_, q, _)| json!({"thread_id":q["thread_id"],"title":safe_text(s(&q["title"])),"replies":[]})).collect::<Vec<_>>());
        print_json(&value, Some(1))?;
        return Ok(value);
    }
    let _work = crate::state_work::StateWork::begin_on(ctx, "reply-work.lock")?;
    let mut state = ctx.load()?;
    recover_reply_reads(ctx, &mut state)?;
    let mut deliveries = Vec::new();
    let mut out = Vec::new();
    let mut claimed = std::collections::BTreeSet::new();
    for (index, original, messages) in returned {
        let current = state["questions"]
            .get(index)
            .ok_or("question changed while replies were read; retry before acknowledging")?;
        // Do not acknowledge a replacement question or revive one closed by the
        // scheduled writer. Merge only into the current generation under lock.
        if ["thread_id", "after", "asked_at", "source"]
            .iter()
            .any(|key| current[*key] != original[*key])
        {
            return Err(
                "question changed while replies were read; retry before acknowledging".into(),
            );
        }
        if !truth(&current["open"]) {
            continue;
        }
        let messages: Vec<_> = messages
            .into_iter()
            .filter(|m| {
                (n(&m["id"]) > n(&current["seen_reply"])
                    || truth(&state["answer_receipts"][s(&m["id"])]["read_retry"]))
                    && !reply_delivered(&state, s(&m["id"]))
                    && !reply_reserved(&state, s(&m["id"]))
                    && claimed.insert(s(&m["id"]).to_owned())
            })
            .collect();
        let replies: Vec<_> = messages.iter().map(|m| json!({"id":m["id"],"timestamp":m["timestamp"],"content":safe_text(s(&m["content"])),
            "attachments":m["attachments"].as_array().map(|a| a.iter().map(|a|safe_text(s(&a["url"]))).collect::<Vec<_>>()).unwrap_or_default()})).collect();
        out.push(json!({"thread_id":current["thread_id"],"title":safe_text(s(&current["title"])),"replies":replies}));
        if !messages.is_empty() {
            deliveries.push((index, messages));
        }
    }
    let value = json!(out);
    if deliveries.is_empty() {
        print_json(&value, Some(1))?;
        return Ok(value);
    }
    // Reserve before stdout, commit only after successful output. A crash or
    // broken pipe leaves a recoverable reservation rather than losing input.
    let reader = std::env::var("CODEX_THREAD_ID").unwrap_or_else(|_| "replies-command".into());
    let job = crate::questions::posting_job(ctx)?;
    for (index, messages) in &deliveries {
        let tid = s(&state["questions"][*index]["thread_id"]).to_owned();
        record_owner_answers(&mut state, &tid, messages, &owner);
        for message in messages {
            let id = s(&message["id"]);
            state["answer_receipts"][id]["thread"] = json!(tid);
            state["answer_receipts"][id]["read_reservation"] =
                json!({"reader":reader,"job":job,"expires_at":ctx.now()+120.});
        }
    }
    ctx.save(&state)?;
    // Reserve before any receipt HTTP or output can wait, so the background
    // worker cannot launch a second consumer while this reader is in flight.
    for (index, messages) in &deliveries {
        if let Err(error) = receive_answers(ctx, &mut state, *index, messages) {
            // No stdout has been delivered. Release this reader's reservations
            // immediately; durable acknowledgement receipts remain retryable.
            for (_, pending) in &deliveries {
                for message in pending {
                    let receipt = &mut state["answer_receipts"][s(&message["id"])];
                    if let Some(receipt) = receipt.as_object_mut() {
                        receipt.remove("read_reservation");
                        receipt.insert("read_retry".into(), json!(true));
                    }
                }
            }
            ctx.save(&state)?;
            return Err(error);
        }
    }
    ctx.effects.reply_output(&python_json(&value, Some(1))?)?;
    // Successful output is an irreversible delivery fact, independent of a
    // concurrently changed snooze or question generation. Commit only that fact
    // into fresh state instead of retrying the worker's stale whole question.
    let _state_guard = lock(ctx)?;
    let mut current = ctx.load_fresh()?;
    for (index, messages) in &deliveries {
        let original = &state["questions"][*index];
        for message in messages {
            let id = s(&message["id"]);
            if current["answer_receipts"][id]["read_by"].is_null() {
                current["answer_receipts"][id]["read_by"] = json!(reader);
            }
            if s(&current["answer_receipts"][id]["read_reservation"]["reader"]) == reader {
                current["answer_receipts"][id]
                    .as_object_mut()
                    .unwrap()
                    .remove("read_reservation");
            }
            current["answer_receipts"][id]
                .as_object_mut()
                .unwrap()
                .remove("read_retry");
        }
        if let Some(question) = current["questions"]
            .as_array_mut()
            .ok_or("missing questions")?
            .iter_mut()
            .find(|q| {
                [
                    "thread_id",
                    "after",
                    "asked_at",
                    "source",
                    "question_message",
                ]
                .iter()
                .all(|k| q[*k] == original[*k])
            })
            .filter(|q| !crate::snooze::paused(q, ctx.now()))
        {
            acknowledge_reply(question, s(&messages.last().unwrap()["id"]));
        }
    }
    atomic_json(&ctx.here.join("state.json"), &current, 2)?;
    drop(_state_guard);
    Ok(value)
}

/// Inspect one trusted thread without consuming replies, receipts, or question state.
/// This also covers request/task threads that are absent from the open-question ledger.
pub fn cmd_replies_read_only(ctx: &mut Context, thread: &str) -> Result<Value> {
    if thread.is_empty() || !thread.chars().all(|c| c.is_ascii_digit()) {
        return Err("threadにはDiscordの数値IDを指定してください".into());
    }
    let state = ctx.load()?;
    let questions = state["questions"].as_array().ok_or("ValueError")?;
    let known = questions
        .iter()
        .rev()
        .find(|q| s(&q["thread_id"]) == thread && s(&q["source"]) != "routing");
    let channel = ctx.call("GET", &format!("/channels/{thread}"), None)?;
    if s(&channel["id"]) != thread
        || ![10, 11, 12].contains(&n(&channel["type"]))
        || channel["guild_id"] != ctx.cfg["guild_id"]
    {
        return Err("指定されたIDは設定されたサーバーのスレッドではありません".into());
    }
    if thread == crate::board::registered_id(ctx)?
        && !crate::board::matches_registration(ctx, &channel)?
    {
        return Err("一覧スレッドの登録先が一致しません".into());
    }
    if known.is_none()
        && s(&channel["parent_id"]) != REQUEST_CHANNEL_ID
        && !crate::board::matches_registration(ctx, &channel)?
    {
        if s(&channel["parent_id"]).is_empty() || channel["parent_id"] != ctx.cfg["task_channel_id"]
        {
            return Err("registered question, request, or bot task thread required".into());
        }
        let me = ctx.call("GET", "/users/@me", None)?;
        if me["bot"] != true || s(&me["id"]).is_empty() || channel["owner_id"] != me["id"] {
            return Err("registered question, request, or bot task thread required".into());
        }
    }
    let owner = ctx.owner();
    let messages: Vec<_> = thread_history(ctx, thread)?
        .into_iter()
        .filter(|m| s(&m["author"]["id"]) == owner)
        .collect();
    let replies: Vec<_> = messages.iter().map(|m| json!({"id":m["id"],"timestamp":m["timestamp"],"content":safe_text(s(&m["content"])),
        "attachments":m["attachments"].as_array().map(|a| a.iter().map(|a|safe_text(s(&a["url"]))).collect::<Vec<_>>()).unwrap_or_default()})).collect();
    let value = json!([{"thread_id":thread,"title":safe_text(s(&channel["name"])),"read_only":true,"question_open":known.map(|q|truth(&q["open"])),"replies":replies}]);
    print_json(&value, Some(1))?;
    Ok(value)
}

/// Defer one question's reminders without sending a message or closing the question.
pub fn cmd_snooze(ctx: &mut Context, thread: &str, until: &str) -> Result<()> {
    let deadline = chrono::DateTime::parse_from_rfc3339(until)
        .map_err(|_| "until must be an RFC3339 timestamp with a timezone".to_string())?;
    let not_before = deadline.timestamp_millis() as f64 / 1000.;
    let _guard = lock(ctx)?;
    if not_before <= ctx.now() {
        return Err("until must be in the future".into());
    }
    let mut state = ctx.load()?;
    let question = state["questions"]
        .as_array_mut()
        .ok_or("ValueError")?
        .iter_mut()
        .find(|q| s(&q["thread_id"]) == thread && truth(&q["open"]))
        .ok_or("open question not found")?;
    // Unlike last_ping, this deadline survives continuation result registration.
    question["remind_not_before"] = json!(not_before);
    question["remind_not_before_iso"] = json!(deadline.to_rfc3339());
    question["remind_set_at"] = json!(ctx.now());
    ctx.save(&state)?;
    drop(_guard);
    print_json(
        &json!({"thread_id":thread,"remind_not_before":deadline.to_rfc3339()}),
        None,
    )
}

/// Re-mention the owner on open questions that got no reply for an hour (owner rule, 2026-09-29).
pub fn cmd_remind(ctx: &mut Context) -> Result<()> {
    let _work = crate::state_work::StateWork::begin(ctx)?;
    remind_locked(ctx)
}

pub fn remind_locked(ctx: &mut Context) -> Result<()> {
    crate::reminders::run_locked(ctx, &crate::reminders::RemindArgs::default()).map(|_| ())
}

/// Archive quiet task threads, except open questions and threads owned by active jobs.
/// Progress edits do not advance last_message_id, so message age alone cannot show idleness.
pub fn cmd_sweep(ctx: &mut Context) -> Result<()> {
    let _work = crate::state_work::StateWork::begin(ctx)?;
    let state = ctx.load()?;
    let questions: HashSet<_> = state["questions"]
        .as_array()
        .ok_or("ValueError")?
        .iter()
        .filter(|q| truth(&q["open"]))
        .map(|q| s(&q["thread_id"]).to_owned())
        .collect();
    let cutoff = ctx.now() - 3600.;
    let active = ctx.call(
        "GET",
        &format!("/guilds/{}/threads/active", s(&ctx.cfg["guild_id"])),
        None,
    )?;
    // Read after the network request, and before any mutations. Unreadable state aborts cleanup.
    let protected = crate::sweep::protected_threads(ctx, &state)?;
    for thread in active["threads"].as_array().into_iter().flatten() {
        crate::tick_runtime::check()?;
        let tid = s(&thread["id"]);
        if s(&thread["parent_id"]) != s(&ctx.cfg["task_channel_id"])
            || questions.contains(tid)
            || protected.contains(tid)
        {
            continue;
        }
        let latest = if truth(&thread["last_message_id"]) {
            s(&thread["last_message_id"])
        } else {
            tid
        };
        if snowflake_time(latest) < cutoff {
            let Ok(_question_delivery) =
                crate::attachments::question_lock_for(ctx, tid, Duration::ZERO)
            else {
                continue;
            };
            // A foreground post may have registered an open question while the
            // sweep was fetching Discord. Protect only this destination until PATCH.
            if ctx.load_fresh()?["questions"].as_array().is_some_and(|qs| {
                qs.iter()
                    .any(|q| s(&q["thread_id"]) == tid && truth(&q["open"]))
            }) {
                continue;
            }
            let Some(_ownership) = crate::sweep::archive_guard(ctx, tid)? else {
                continue;
            };
            // Notifier progress may have changed while another candidate was being processed.
            // Keep its lock through the freshness read and archive request.
            let fresh = ctx.call("GET", &format!("/channels/{tid}"), None)?;
            let latest = if truth(&fresh["last_message_id"]) {
                s(&fresh["last_message_id"])
            } else {
                tid
            };
            if snowflake_time(latest) >= cutoff {
                continue;
            }
            ctx.call(
                "PATCH",
                &format!("/channels/{tid}"),
                Some(&json!({"archived":true})),
            )?;
            println!(
                "archived {tid} {}",
                s(&thread["name"]).chars().take(60).collect::<String>()
            );
        }
    }
    Ok(())
}

/// Archive matching registered questions and persist their closed state.
pub fn cmd_close(ctx: &mut Context, thread: &str) -> Result<()> {
    let _question_delivery = crate::attachments::question_lock(ctx, thread)?;
    let _work = crate::state_work::StateWork::begin(ctx)?;
    let mut state = ctx.load()?;
    if state["questions"].as_array().is_some_and(|qs| {
        qs.iter().any(|q| {
            s(&q["thread_id"]) == thread && crate::reply_recovery::prevents_close(&state, q)
        })
    }) {
        return Err(
            "回答を担当へ届けられていないため、質問を閉じません。inboxで再開状況を確認してください"
                .into(),
        );
    }
    for question in state["questions"].as_array_mut().ok_or("ValueError")? {
        if s(&question["thread_id"]) == thread {
            crate::restart::stop_todo(ctx, question)?;
            question["recovery_closed"] = json!(true);
            question["open"] = json!(false);
            question["closed_at"] = json!(ctx.now());
            ctx.call(
                "PATCH",
                &format!("/channels/{thread}"),
                Some(&json!({"archived":true})),
            )?;
        }
    }
    ctx.save(&state)?;
    println!("closed");
    Ok(())
}

/// Record something the owner may read later but must not be alerted about (owner, 2026-10-01: cleanup
///     reports go into one thread, no mention, no notification). One long-lived thread per --topic.
pub fn cmd_log(
    ctx: &mut Context,
    topic: &str,
    text: &str,
    body_file: Option<&Path>,
    notify: bool,
) -> Result<Value> {
    cmd_log_attached(ctx, topic, text, body_file, notify, &[])
}

pub fn cmd_log_attached(
    ctx: &mut Context,
    topic: &str,
    text: &str,
    body_file: Option<&Path>,
    notify: bool,
    attach: &[PathBuf],
) -> Result<Value> {
    let channel_topic = crate::channel_reports::canonical(topic);
    let topic = channel_topic.unwrap_or(topic);
    let notify = notify && channel_topic.is_none();
    let body = match body_file {
        Some(path) => read_text(path)?.trim().to_owned(),
        None => text.to_owned(),
    };
    let prepared = crate::attachments::prepare(ctx, attach)?;
    if body.is_empty() && prepared.is_none() {
        return Err("本文または添付ファイルを指定してください".into());
    }
    let thread_id;
    {
        let _guard =
            ByteLock::acquire(&ctx.here.join("log-threads.lock"), Duration::from_secs(60))?;
        let path = ctx.here.join("log_threads.json");
        let mut known = if path.exists() {
            read_json(&path)?
        } else {
            json!({})
        };
        let mut rename = false;
        if channel_topic.is_some() && !truth(&known[topic]) {
            let prior = known.as_object().and_then(|topics| {
                topics.iter().find_map(|(alias, id)| {
                    (crate::channel_reports::canonical(alias) == channel_topic && truth(id))
                        .then(|| id.clone())
                })
            });
            if let Some(id) = prior {
                ctx.call(
                    "PATCH",
                    &format!("/channels/{}", s(&id)),
                    Some(&json!({"archived":false,"name":topic})),
                )?;
                rename = true;
                known[topic] = id;
                atomic_json(&path, &known, 1)?;
            }
        }
        if truth(&known[topic]) {
            thread_id = s(&known[topic]).to_owned();
            if !rename {
                ctx.call(
                    "PATCH",
                    &format!("/channels/{thread_id}"),
                    Some(&json!({"archived":false})),
                )?;
            }
        } else {
            let thread = ctx.call("POST", &format!("/channels/{}/threads",s(&ctx.cfg["task_channel_id"])), Some(&json!({"name":topic.chars().take(100).collect::<String>(),
                "message":{"content":format!("「{topic}」の記録です。通知は出しません。"),"flags":SILENT,"allowed_mentions":{"parse":[]}}})))?;
            thread_id = s(&thread["id"]).to_owned();
            known[topic] = json!(thread_id);
            atomic_json(&path, &known, 1)?;
        }
    }
    if notify && crate::questions::is_question(&body) {
        return post_text(ctx, &thread_id, &body, attach, true, None);
    }
    let question = if notify && crate::questions::is_question(&body) {
        Some(ctx.call("GET", &format!("/channels/{thread_id}"), None)?)
    } else {
        None
    };
    let message_ids = crate::attachments::post_tracked(
        ctx,
        &thread_id,
        &body,
        prepared.as_ref(),
        notify,
        question.as_ref(),
    )?;
    let value = json!({"thread_id":thread_id,"url":format!("https://discord.com/channels/{}/{}",s(&ctx.cfg["guild_id"]),thread_id),"message_id":message_ids.last(),"message_ids":message_ids});
    print_json(&value, None)?;
    Ok(value)
}

pub fn cmd_post(
    ctx: &mut Context,
    thread: &str,
    body_file: &Path,
    attach: &[PathBuf],
    notify: bool,
) -> Result<Value> {
    post_text(ctx, thread, &read_text(body_file)?, attach, notify, None)
}

fn post_text(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    attach: &[PathBuf],
    notify: bool,
    context: Option<&Value>,
) -> Result<Value> {
    if thread.is_empty() || !thread.chars().all(|c| c.is_ascii_digit()) {
        return Err("threadにはDiscordの数値IDを指定してください".into());
    }
    if crate::questions::is_question(body) {
        crate::ask_gate::enforce("", body, None)?;
        let mut metadata = context.cloned().unwrap_or_else(|| json!({}));
        let channel = ctx.call("GET", &format!("/channels/{thread}"), None)?;
        if ![10, 11, 12].contains(&n(&channel["type"]))
            || channel["guild_id"] != ctx.cfg["guild_id"]
        {
            return Err("指定されたIDは設定されたサーバーのスレッドではありません".into());
        }
        {
            // Reconcile only old interrupted message ledgers before deciding
            // whether this is a new question. Recovery never appends a question.
            let prepared = crate::attachments::prepare(ctx, attach)?;
            let _guard = crate::attachments::question_lock(ctx, thread)?;
            let ids = crate::attachments::recover_posted(
                ctx,
                thread,
                body,
                prepared.as_ref(),
                notify,
                &channel,
                context,
            )?;
            let caller = crate::questions::posting_job(ctx)?;
            if let Some(answer) = crate::questions::answered_repost(
                ctx,
                thread,
                body,
                caller.as_deref(),
                &crate::attachments::fingerprints(prepared.as_ref())?,
            )? {
                let value = json!({"thread_id":thread,"message_ids":ids,"suppressed":"already_answered","latest_owner_reply":answer});
                print_json(&value, None)?;
                return Ok(value);
            }
        }
        if s(&metadata["title"]).is_empty() {
            metadata["title"] = channel["name"].clone();
        }
        if s(&metadata["cwd"]).is_empty() {
            metadata["cwd"] = json!(std::env::current_dir()
                .map_err(|e| e.to_string())?
                .to_string_lossy());
        }
        metadata["asked_at"] = json!(timestamp());
        metadata["source"] = json!("posted");
        if let Ok(session) = std::env::var("CODEX_THREAD_ID") {
            metadata["session_id"] = json!(session);
        }
        if let Ok(job) = std::env::var("CC_JOB_ID") {
            if valid_job(&job) {
                metadata["posting_job"] = json!(job);
                metadata["job"] = json!(job);
                metadata["job_info"] = job_info(ctx, &job);
                metadata["todo_id"] = metadata["job_info"]["todo_id"].clone();
            }
        }
        let result = crate::question_progress::ask(ctx, &metadata, thread, body, attach)?;
        print_json(&result, None)?;
        return Ok(result);
    }
    let destination = crate::question_progress::result_target(ctx, thread, "")?;
    let separated = destination != thread || crate::question_progress::is_separated(ctx, thread)?;
    let thread = destination.as_str();
    let silent_body = crate::question_progress::silent_text(body);
    let body = if separated {
        silent_body.as_str()
    } else {
        body
    };
    let notify = notify && !separated;
    let prepared = crate::attachments::prepare(ctx, attach)?;
    let channel = ctx.call("GET", &format!("/channels/{thread}"), None)?;
    if ![10, 11, 12].contains(&n(&channel["type"]))
        || s(&channel["guild_id"]) != s(&ctx.cfg["guild_id"])
    {
        return Err("指定されたIDは設定されたサーバーのスレッドではありません".into());
    }
    let is_question = crate::questions::is_question(body);
    let _guard = if is_question {
        Some(crate::attachments::question_lock(ctx, thread)?)
    } else {
        None
    };
    if is_question {
        let recovered = crate::attachments::recover_posted(
            ctx,
            thread,
            body,
            prepared.as_ref(),
            notify,
            &channel,
            context,
        )?;
        let caller = crate::questions::posting_job(ctx)?;
        if let Some(answer) = crate::questions::answered_repost(
            ctx,
            thread,
            body,
            caller.as_deref(),
            &crate::attachments::fingerprints(prepared.as_ref())?,
        )? {
            let value = json!({"thread_id":thread,"message_ids":recovered,"suppressed":"already_answered","latest_owner_reply":answer});
            print_json(&value, None)?;
            return Ok(value);
        }
    }
    if truth(&channel["thread_metadata"]["archived"]) {
        ctx.call(
            "PATCH",
            &format!("/channels/{thread}"),
            Some(&json!({"archived":false})),
        )?;
    }
    let question = is_question.then_some(&channel);
    let ids = crate::attachments::post_tracked_locked_with_context(
        ctx,
        thread,
        body,
        prepared.as_ref(),
        notify,
        question,
        context,
    )?;
    let mut value = json!({"thread_id":thread,"message_ids":ids});
    if context.is_some_and(|c| c["reuse_question"].is_object()) {
        value["reused"] = json!(true);
        let identity = &context.unwrap()["reuse_question"];
        let state = ctx.load_fresh()?;
        value["tracked"] = json!(state["questions"]
            .as_array()
            .into_iter()
            .flatten()
            .any(|q| q["thread_id"] == identity["thread_id"] && truth(&q["open"])));
        let owner = ctx.owner();
        let replies = owner_replies(ctx, identity, &owner)?;
        if let Some(answer) = replies
            .iter()
            .rev()
            .find(|m| !crate::snooze::control_only(m))
        {
            value["suppressed"] = json!("already_answered");
            value["latest_owner_reply"] = json!({"id":answer["id"],"timestamp":answer["timestamp"],"content":safe_text(s(&answer["content"]))});
        } else if value["tracked"] == false {
            value["suppressed"] = json!("question_closed");
        }
    }
    print_json(&value, None)?;
    Ok(value)
}

/// Owner messages on trusted request/question/bot task threads, oldest first.
pub fn cmd_inbox(ctx: &mut Context, state_file: &Path) -> Result<Vec<Value>> {
    let owner = ctx.owner();
    // Receive every open generation independently of the ordinary inbox cursor.
    // This is observation only: no acknowledgement, delivery or launch occurs.
    {
        let _work = crate::state_work::StateWork::begin_on(ctx, "inbox-work.lock")?;
        let mut state = ctx.load()?;
        let questions = state["questions"]
            .as_array()
            .ok_or("invalid questions ledger")?
            .clone();
        for q in questions.iter().filter(|q| truth(&q["open"])) {
            match crate::routing::question_allowed(ctx, q) {
                Ok(true) => (),
                Ok(false) => continue,
                Err(error) => {
                    eprintln!("inbox question scope failed: {}", error_detail(&error));
                    continue;
                }
            }
            match owner_replies(ctx, q, &owner) {
                Ok(messages) => {
                    record_owner_answers(&mut state, s(&q["thread_id"]), &messages, &owner);
                    if let Err(error) = ctx.save(&state) {
                        eprintln!("inbox answer checkpoint failed: {}", error_detail(&error));
                        break;
                    }
                }
                Err(error) => eprintln!("inbox answer scan failed: {}", error_detail(&error)),
            }
        }
    }
    let last: u64 = if state_file.exists() {
        std::fs::read_to_string(state_file)
            .map_err(|_| "OSError".to_string())?
            .trim()
            .parse()
            .map_err(|_| "ValueError".to_string())?
    } else {
        ((ctx.now() * 1000. - 1420070400000. - 3600000.) as u64) << 22
    };
    let mut places = BTreeMap::new();
    let mut inbox_only = std::collections::BTreeSet::new();
    let allowed = match crate::routing::allowed_places(ctx, last) {
        Ok(allowed) => allowed,
        Err(error) => {
            eprintln!(
                "inbox inventory failed; showing saved answers: {}",
                error_detail(&error)
            );
            BTreeMap::new()
        }
    };
    for (id, thread) in &allowed {
        let latest = if truth(&thread["last_message_id"]) {
            &thread["last_message_id"]
        } else {
            &thread["id"]
        };
        if n(latest) > last {
            if truth(&thread["inbox_only"]) {
                inbox_only.insert(id.clone());
            }
            places.insert(id.clone(), s(&thread["routing_label"]).to_owned());
        }
    }
    let mut found = Vec::new();
    for (cid, label) in places {
        let result = if inbox_only.contains(&cid) {
            crate::board::inbox_history(ctx, &cid, last)
        } else {
            ctx.call(
                "GET",
                &format!("/channels/{cid}/messages?after={last}&limit=100"),
                None,
            )
        };
        // The shared cursor must never skip an unread board post after another thread advances it.
        if inbox_only.contains(&cid) {
            if let Err(error) = &result {
                return Err(error.clone());
            }
        }
        if let Ok(messages) = result {
            for m in messages
                .as_array()
                .into_iter()
                .flatten()
                .filter(|m| s(&m["author"]["id"]) == owner)
            {
                found.push((n(&m["id"]), cid.clone(), label.clone(), m.clone()));
            }
        }
    }
    found.sort_by(|a, b| (&a.0, &a.1, &a.2).cmp(&(&b.0, &b.1, &b.2)));
    let mut out = Vec::new();
    for (mid, cid, label, m) in &found {
        let files = m["attachments"]
            .as_array()
            .into_iter()
            .flatten()
            .map(|a| s(&a["url"]))
            .collect::<Vec<_>>()
            .join(" ");
        let text = format!(
            "{}{}",
            s(&m["content"]),
            if files.is_empty() {
                String::new()
            } else {
                format!(" {files}")
            }
        );
        let value = json!({"id":mid.to_string(),"channel":cid,"where":label,"at":s(&m["timestamp"]).chars().take(16).collect::<String>(),"text":text});
        out.push(value);
    }
    // All unanswered delivery paths remain visible after the cursor advances,
    // including main sessions, missing destinations and answers already handed
    // to a worker whose question has not yet been resolved.
    let state = ctx.load()?;
    for q in state["questions"].as_array().into_iter().flatten() {
        let tid = s(&q["thread_id"]);
        if !truth(&q["open"])
            || !question_answered(&state, q)
            || (s(&q["source"]) == "routing" && !allowed.contains_key(tid))
        {
            continue;
        }
        let failure = if truth(&q["resume_status"]) || truth(&q["last_resume_error"]) {
            q
        } else {
            &state["requests"][tid]
        };
        let status = if s(&failure["resume_status"]).is_empty() {
            "答えあり・未反映"
        } else {
            s(&failure["resume_status"])
        };
        let latest = question_answer_id(&state, q).to_string();
        let replies = q["owner_replies"]
            .as_array()
            .filter(|ms| !ms.is_empty())
            .cloned()
            .unwrap_or_else(|| {
                vec![if s(&q["owner_reply"]["id"]) == latest {
                    q["owner_reply"].clone()
                } else {
                    json!({"id":latest})
                }]
            });
        for reply in replies {
            let id = s(&reply["id"]).to_owned();
            if n(&reply["id"]) <= n(&q["after"]) {
                continue;
            }
            let reference = json!({"after":q["after"],"title":q["title"]});
            if let Some(existing) = out
                .iter_mut()
                .find(|m| m["id"] == id && m["channel"] == tid)
            {
                existing["status"] = json!(status);
                if !existing["questions"].is_array() {
                    existing["questions"] = json!([]);
                }
                if !existing["questions"]
                    .as_array()
                    .unwrap()
                    .contains(&reference)
                {
                    existing["questions"]
                        .as_array_mut()
                        .unwrap()
                        .push(reference);
                }
                continue;
            }
            let files = reply["attachments"]
                .as_array()
                .into_iter()
                .flatten()
                .map(|a| safe_text(s(&a["url"])))
                .filter(|url| !url.is_empty())
                .collect::<Vec<_>>()
                .join(" ");
            let text = if !s(&reply["content"]).is_empty() || !files.is_empty() {
                format!(
                    "{}{}{}",
                    safe_text(s(&reply["content"])),
                    if files.is_empty() { "" } else { " " },
                    files
                )
            } else {
                format!(
                    "{status}: {} {}",
                    safe_text(s(&q["title"])),
                    safe_text(s(&failure["last_resume_error"]))
                )
            };
            let label = allowed
                .get(tid)
                .map(|place| place["routing_label"].clone())
                .unwrap_or_else(|| json!(safe_text(s(&q["title"]))));
            let value = json!({"id":id,"channel":tid,"where":label,"saved_only":!allowed.contains_key(tid),
            "status":status,"text":text,"question":q["title"],"questions":[reference],"reply":reply,
            "delivered":reply_delivered(&state,&id),"destination":state["answer_receipts"][&id],
            "retry_exhausted":failure["reply_resume_retry"]["exhausted"],"retry_deadline":failure["reply_resume_retry"]["deadline"]});
            out.push(value);
        }
    }
    for value in &out {
        print_json(value, None)?;
    }
    if let Some((mid, _, _, _)) = found.last() {
        std::fs::write(state_file, mid.to_string()).map_err(|_| "OSError".to_string())?;
    }
    Ok(out)
}

fn external_step(ctx: &mut Context, path: PathBuf, verb: &str, report_json: bool) -> Result<()> {
    let argv = command_argv(&path, &[verb.to_owned()]);
    let output = ctx.run(&argv, 20)?;
    // scheduler.main prints the returned report; scheduler.step itself does not.
    let stdout = if report_json {
        inprocess_stdout(&output.stdout)
    } else {
        output.stdout.clone()
    };
    if !stdout.is_empty() {
        let mut out = io::stdout().lock();
        write!(out, "{stdout}")
            .and_then(|_| out.flush())
            .map_err(|_| "stdout write failed".to_string())?;
    }
    if !output.stderr.is_empty() {
        let mut err = io::stderr().lock();
        write!(err, "{}", safe_text(&output.stderr))
            .and_then(|_| err.flush())
            .map_err(|_| "stderr write failed".to_string())?;
    }
    let swallowed_scheduler_error = report_json
        && output.code == 1
        && trailing_report(&output.stdout)
            .is_some_and(|(_, report)| report.is_object() && report.get("error").is_some());
    let scheduler_busy = report_json
        && trailing_report(&output.stdout).is_some_and(|(_, report)| report["busy"] == true);
    if swallowed_scheduler_error || scheduler_busy {
        ctx.cfg["__scheduler_step_failed"] = json!(true);
    }
    if output.code != 0 && !swallowed_scheduler_error {
        return Err(format!("{verb} exited {}", output.code));
    }
    Ok(())
}

/// Stall watchdog of the shared to-do list (D:/CC/Tools/todo/todo.py).
pub fn cmd_todo_watch(ctx: &mut Context) -> Result<()> {
    external_step(ctx, ctx.todo_command.clone(), "watch", false)
}
/// Capacity, dispatch and recovery; scheduler.step isolates and logs its own failures.
pub fn cmd_scheduler_step(ctx: &mut Context) -> Result<()> {
    ctx.cfg["__scheduler_step_failed"] = json!(false);
    external_step(ctx, ctx.scheduler_command.clone(), "step", true)
}
// Observation must continue even while boot ownership prevents new launches.
// Discard the report: watch-preview can contain private work metadata.
fn observe_only(ctx: &mut Context, path: PathBuf, verb: &str) -> Result<()> {
    let output = ctx.run(&command_argv(&path, &[verb.to_owned()]), 20)?;
    if verb == "status"
        && trailing_report(&output.stdout)
            .is_some_and(|(_, report)| report["busy"] == true || !report["error"].is_null())
    {
        return Err("observation unavailable".into());
    }
    if output.code == 0 {
        Ok(())
    } else {
        Err("observation failed".into())
    }
}
/// PR shepherds (D:/CC/Tools/shepherd): one Codex job per idle open PR drives it to merge (2026-10-01).
pub fn cmd_shepherd_step(ctx: &mut Context) -> Result<()> {
    external_step(ctx, ctx.shepherd_command.clone(), "step", false)
}

/// Remove only the trailing CLI JSON report, retaining in-process diagnostic lines.
pub fn inprocess_stdout(stdout: &str) -> String {
    trailing_report(stdout)
        .map(|(start, _)| stdout[..start].to_owned())
        .unwrap_or_else(|| stdout.to_owned())
}

fn trailing_report(stdout: &str) -> Option<(usize, Value)> {
    for (start, _) in stdout
        .char_indices()
        .filter(|(_, ch)| *ch == '{' || *ch == '[')
    {
        if start > 0 && !stdout[..start].ends_with('\n') {
            continue;
        }
        if let Ok(report) = serde_json::from_str::<Value>(stdout[start..].trim()) {
            return Some((start, report));
        }
    }
    None
}

/// Execute every periodic step even after failure, sharing one launch-attempt budget.
pub fn cmd_tick(ctx: &mut Context) -> Result<()> {
    // Serialize manual verification with the per-minute task, including watchdog actions.
    let Ok(_tick) = ByteLock::acquire(&ctx.here.join("tick.lock"), Duration::ZERO) else {
        return Ok(());
    };
    let _deadline = crate::tick_runtime::Deadline::new(crate::tick_runtime::TICK_LIMIT);
    let mut health = json!({"started_at":ctx.now() as u64,"stages":{}});
    atomic_json(&ctx.here.join("tick-health.json"), &health, 2)?;
    let _finish = crate::tick_runtime::Span::new("tick total");
    println!(
        "{} tick start; prompt_limit_bytes={}; writer_guard=enabled; pid={}; limit_ms={}",
        timestamp(),
        crate::prompt_context::MAX_PROMPT_BYTES,
        std::process::id(),
        crate::tick_runtime::TICK_LIMIT.as_millis()
    );
    // Reclaim memory before any Discord requests. Network timeouts must not
    // consume the scheduled task's entire execution window before observation.
    if let Err(error) = crate::tick_runtime::stage("helper-reaper", Duration::from_secs(5), || {
        cmd_helper_reaper(ctx)
    }) {
        eprintln!("cmd_helper_reaper failed: {}", error_detail(&error));
    }
    let mut budget = false;
    if let Err(error) =
        crate::tick_runtime::stage("work-status-board", Duration::from_secs(2), || {
            crate::board::launch_due(ctx)
        })
    {
        eprintln!("board launch failed: {}", error_detail(&error));
    }
    // Recovery is required for direct CLI ticks as well as the scheduled VBS caller.
    // A failed inventory recovery permits receipts but blocks new children this pass.
    let recovered = crate::tick_runtime::stage("startup-recovery", Duration::from_secs(20), || {
        Ok(crate::automation::recover_before_launch(ctx))
    })
    .unwrap_or(false);
    health["stages"]["startup_recovery"] = json!(recovered);
    if let Err(error) =
        crate::tick_runtime::stage("question-progress-recovery", Duration::from_secs(5), || {
            crate::question_progress::recover_pending(ctx)
        })
    {
        eprintln!(
            "question progress recovery failed: {}",
            error_detail(&error)
        );
    }
    if !recovered {
        budget = true;
        eprintln!("startup recovery failed; launches deferred");
    }
    // Observe/recover work before Discord scans can consume the whole deadline.
    // Reuse this tick and its launch gates; never add another polling loop.
    for (name, step) in [
        (
            "cmd_todo_watch",
            cmd_todo_watch as fn(&mut Context) -> Result<()>,
        ),
        (
            "cmd_scheduler_step",
            cmd_scheduler_step as fn(&mut Context) -> Result<()>,
        ),
    ] {
        let deferred = truth(&ctx.cfg["__reboot_recovery_deferred"]);
        // The metadata inventory spans thousands of receipts; retain its
        // normal 20s ceiling within the existing whole-tick deadline.
        let limit = if name == "cmd_todo_watch" { 20 } else { 8 };
        let result = crate::tick_runtime::stage(name, Duration::from_secs(limit), || {
            if deferred {
                if name == "cmd_todo_watch" {
                    observe_only(ctx, ctx.todo_command.clone(), "watch-preview")
                } else {
                    observe_only(ctx, ctx.scheduler_command.clone(), "status")
                }
            } else {
                step(ctx)
            }
        });
        health["stages"][name] = json!(
            result.is_ok()
                && !(name == "cmd_scheduler_step" && truth(&ctx.cfg["__scheduler_step_failed"]))
        );
        if let Err(error) = result {
            eprintln!("{name} failed: {}", error_detail(&error));
        }
    }
    health["observed_at"] = json!(ctx.now() as u64);
    health["mode"] = json!(if truth(&ctx.cfg["__reboot_recovery_deferred"]) {
        "observe_only"
    } else {
        "recovery"
    });
    health["error"] = if health["stages"]
        .as_object()
        .unwrap()
        .values()
        .all(|v| v == true)
    {
        Value::Null
    } else {
        json!("core_stage_failed")
    };
    atomic_json(&ctx.here.join("tick-health.json"), &health, 2)?;
    let config_error = ctx.cfg["__cc_ask_config_error"].as_str().map(str::to_owned);
    let intake_first = if config_error.is_none() {
        let _state_lock = lock(ctx)?;
        let mut state = ctx.load()?;
        let intake_first = truth(&state["reply_intake_first"]);
        state["reply_intake_first"] = json!(!intake_first);
        ctx.save(&state)?;
        intake_first
    } else {
        false
    };
    let steps = if intake_first {
        ["intake", "resume"]
    } else {
        ["resume", "intake"]
    };
    for step in steps {
        let result =
            crate::tick_runtime::stage(step, Duration::from_secs(15), || match &config_error {
                Some(error) => Err(error.clone()),
                None if step == "resume" => crate::resume::cmd_resume(ctx, &mut budget),
                None => crate::intake::cmd_intake(ctx, &mut budget),
            });
        if let Err(error) = result {
            eprintln!("cmd_{step} failed: {}", error_detail(&error));
        }
    }
    // Stopped ToDo work goes through watch -> console delegation. The old
    // patrol/routing scanners must stay retired even if automation.json is enabled.
    for (name, step) in [
        (
            "cmd_civ6_fairness",
            crate::civ6_idle::cmd_civ6_fairness as fn(&mut Context) -> Result<()>,
        ),
        ("cmd_shepherd_step", cmd_shepherd_step),
        ("cmd_remind", cmd_remind),
        ("cmd_sweep", cmd_sweep),
        ("cmd_civ6_idle", crate::civ6_idle::cmd_civ6_idle),
    ] {
        let result = crate::tick_runtime::stage(name, Duration::from_secs(5), || {
            if ["cmd_remind", "cmd_sweep"].contains(&name) {
                match &config_error {
                    Some(error) => Err(error.clone()),
                    None => step(ctx),
                }
            } else {
                step(ctx)
            }
        });
        if let Err(error) = result {
            eprintln!("{name} failed: {}", error_detail(&error));
        }
    }
    health["finished_at"] = json!(ctx.now() as u64);
    atomic_json(&ctx.here.join("tick-health.json"), &health, 2)?;
    Ok(())
}

fn cmd_helper_reaper(ctx: &mut Context) -> Result<()> {
    let report = ctx.effects.reap_helpers(&ctx.jobs).unwrap_or_else(|error| {
        format!(
            "helper-reaper: killed=0 freed_mb=0.0 scan_failed=true error={}",
            safe_text(&error).replace(['\r', '\n'], " ")
        )
    });
    if !report.is_empty() {
        // The task already redirects this stream to tick.log; do not reopen it.
        println!("{} {report}", timestamp());
        crate::helper_reaper::flush_log(ctx)?;
    }
    Ok(())
}
