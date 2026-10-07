//! Recover already delivered questions. Inspection uses only GET; registration never posts.
use crate::{core::*, project::ProjectNames};
use serde_json::{json, Value};
use std::collections::BTreeSet;

/// Before a delayed worker posts a question, check the owner's actual thread
/// history. An old worker must read a newer answer before asking again.
pub fn answered_repost(
    ctx: &mut Context,
    thread: &str,
    body: &str,
    caller: Option<&str>,
    attachment_hashes: &[String],
) -> Result<Option<Value>> {
    if !is_question(body) {
        return Ok(None);
    }
    let history = thread_history(ctx, thread)?;
    let owner = ctx.owner();
    let Some(answer) = history
        .iter()
        .rev()
        .find(|m| s(&m["author"]["id"]) == owner && !crate::snooze::control_only(m))
    else {
        return Ok(None);
    };
    let previous: Vec<_> = history
        .iter()
        .filter(|m| {
            truth(&m["author"]["bot"])
                && n(&m["id"]) < n(&answer["id"])
                && is_question(s(&m["content"]))
        })
        .collect();
    if previous.is_empty() {
        return Ok(None);
    }
    let normalized = |text: &str| {
        regex::Regex::new(r"(?m)^-#.*$|<@!?[0-9]+>|\s+")
            .unwrap()
            .replace_all(text, "")
            .into_owned()
    };
    let state = ctx.load()?;
    let same_attachments = if attachment_hashes.is_empty() {
        previous
            .last()
            .is_some_and(|m| m["attachments"].as_array().is_none_or(Vec::is_empty))
    } else {
        previous.last().is_some_and(|m| {
            state["questions"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|q| {
                    s(&q["thread_id"]) == thread
                        && q["question_message"] == m["id"]
                        && q["attachment_hashes"] == json!(attachment_hashes)
                })
        })
    };
    let identical = same_attachments
        && previous
            .last()
            .is_some_and(|m| normalized(s(&m["content"])) == normalized(body));
    let stale = if let Some(jid) = caller.filter(|id| valid_job(id)) {
        let info = job_info(ctx, jid);
        let started = crate::automation::epoch(&info["created_at"]);
        let receipt = &state["answer_receipts"][s(&answer["id"])];
        let read = crate::steering::reply_seen_in_job(ctx, jid, thread, answer, true)?
            || s(&receipt["handled_by"]) == jid
            || (!s(&receipt["read_by"]).is_empty()
                && [s(&info["thread_id"]), s(&info["resume_session"])]
                    .contains(&s(&receipt["read_by"])));
        started > 0. && started < snowflake_time(s(&answer["id"])) && !read
    } else {
        false
    };
    Ok((identical || stale).then(|| json!({"id":answer["id"],"timestamp":answer["timestamp"],"content":safe_text(s(&answer["content"]))})))
}

pub fn posting_job(ctx: &Context) -> Result<Option<String>> {
    let Ok(session) = std::env::var("CODEX_THREAD_ID") else {
        return Ok(None);
    };
    Ok(crate::automation::jobs(ctx)?
        .iter()
        .filter(|job| [s(&job["thread_id"]), s(&job["resume_session"])].contains(&session.as_str()))
        .max_by(|a, b| {
            crate::automation::epoch(&a["created_at"])
                .total_cmp(&crate::automation::epoch(&b["created_at"]))
        })
        .map(|job| s(&job["id"]).to_owned()))
}

pub fn result_generation(ctx: &Context, thread: &str, job: &str) -> Result<Value> {
    if !valid_job(job) {
        return Err("invalid result job".into());
    }
    let path = crate::result_delivery::ledger_path(ctx, thread, job)?;
    if !path.is_file() {
        return Ok(json!({}));
    }
    let ledger = read_json(&path)?;
    if s(&ledger["thread_id"]) != thread {
        return Err("result belongs to another thread".into());
    }
    Ok(json!({"first":ledger["first"],"attachment_hashes":ledger["attachment_hashes"]}))
}

pub fn register_result(
    ctx: &Context,
    state: &mut Value,
    thread: &Value,
    after: &str,
    job: &str,
) -> Result<()> {
    let generation = result_generation(ctx, s(&thread["id"]), job)?;
    let first = generation["first"]
        .as_str()
        .filter(|id| !id.is_empty())
        .unwrap_or(after);
    register_question(state, thread, first);
    if let Some(q) = state["questions"]
        .as_array_mut()
        .unwrap()
        .iter_mut()
        .rev()
        .find(|q| q["thread_id"] == thread["id"] && s(&q["after"]) == first)
    {
        q["question_message"] = json!(first);
        q["attachment_hashes"] = generation["attachment_hashes"].clone();
    }
    Ok(())
}

#[derive(Clone, Debug, clap::Args)]
pub struct RecoverArgs {
    /// Inspect only these threads. Without this flag, inspect known question/request/log threads.
    #[arg(long)]
    pub thread: Vec<String>,
    /// Track this already posted message (requires exactly one --thread).
    #[arg(long, requires = "thread")]
    pub message: Option<String>,
    /// Register candidates in state.json; sends no Discord messages.
    #[arg(long, conflicts_with = "dry_run")]
    pub apply: bool,
    #[arg(long)]
    pub dry_run: bool,
    /// Maximum number of threads inspected/registered, capped at 100.
    #[arg(long, default_value_t = 5)]
    pub limit: usize,
}

pub fn is_question(body: &str) -> bool {
    let first = body.lines().next().unwrap_or("").trim();
    if first.starts_with("状態:") {
        return result_state(body) == "状態: 要判断";
    }
    !first.starts_with("終わりました。")
        && !body.contains("（1時間たったので再送です）")
        && !body.contains("未回答の質問をまとめて再通知します")
        && (concrete_question_body(body)
            || (first.starts_with("判断をお願いしたいです") && choice_question_body(body)))
}

/// Restore an existing question referenced by a wait report only when a matching
/// human-wait ToDo confirms it is still outstanding. Reports alone are insufficient.
pub fn pending_reference(ctx: &Context, state: &Value, channel: &Value, body: &str) -> bool {
    let tid = s(&channel["id"]);
    if body.lines().next().is_some_and(|line| {
        line.trim().starts_with("状態:") || line.trim().starts_with("終わりました。")
    }) {
        return false;
    }
    let mut fenced = false;
    let reference = body.lines().any(|line| {
        if line.starts_with("    ") || line.starts_with('\t') {
            return false;
        }
        let line = line.trim();
        if line.starts_with("```") || line.starts_with("~~~") {
            fenced = !fenced;
            return false;
        }
        !fenced && !line.starts_with('>') && crate::owner_wait::waiting_notice(line)
    });
    reference
        && crate::owner_wait::pending(ctx, tid, s(&channel["name"]))
        && state["questions"].as_array().is_some_and(|qs| {
            qs.iter().any(|q| {
                s(&q["thread_id"]) == tid && n(&q["after"]) > 0 && !s(&q["title"]).is_empty()
            })
        })
}

fn epoch(value: &Value) -> f64 {
    value.as_f64().unwrap_or_else(|| {
        chrono::DateTime::parse_from_rfc3339(s(value))
            .map(|d| d.timestamp_millis() as f64 / 1000.)
            .unwrap_or(0.)
    })
}

pub fn completed_after(history: &[Value], after: &Value) -> bool {
    history.iter().any(|m| {
        n(&m["id"]) > n(after)
            && truth(&m["author"]["bot"])
            && (s(&m["content"]).starts_with("終わりました。")
                || s(&m["content"]).starts_with("状態: 完了"))
    })
}

/// Explicit terminal state must suppress reminders and historical recovery alike.
pub fn stopped(ctx: &Context, state: &Value, thread: &str) -> bool {
    let request = &state["requests"][thread];
    let records = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|q| s(&q["thread_id"]) == thread)
        .rev()
        .take(1)
        .chain(std::iter::once(request));
    records.into_iter().any(|record| {
        ["owner_stopped", "stopped", "cancelled"]
            .iter()
            .any(|key| truth(&record[*key]))
            || [record, &record["continuation"]].iter().any(|entry| {
                let job = s(&entry["job"]);
                let info = job_info(ctx, job);
                !job.is_empty()
                    && crate::restart::deliberate_cancel(&info)
                    && !["superseded", "duplicate"].contains(&s(&info["cancel_reason"]))
                    && (["manual", "person"].contains(&s(&info["cancel_reason"]))
                        || !crate::owner_wait::pending(ctx, thread, s(&record["title"])))
            })
    })
}

/// Caller holds state.lock. Preserve routing/snooze metadata, but never old answer cursors.
pub fn register_posted(state: &mut Value, channel: &Value, first: &str, ids: &[String]) {
    let tid = s(&channel["id"]);
    let previous = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .rev()
        .find(|q| s(&q["thread_id"]) == tid)
        .cloned();
    let exists = state["questions"].as_array().is_some_and(|qs| {
        qs.iter()
            .any(|q| s(&q["thread_id"]) == tid && s(&q["question_message"]) == first)
    });
    if !exists {
        let len = state["questions"].as_array().map_or(0, Vec::len);
        register_question(state, channel, first);
        if state["questions"].as_array().map_or(0, Vec::len) == len {
            return;
        }
        if s(&channel["parent_id"]) == REQUEST_CHANNEL_ID && state["requests"][tid].is_null() {
            if !state["requests"].is_object() {
                state["requests"] = json!({});
            }
            // Intake must not replay messages preceding a newly recovered question.
            state["requests"][tid] = json!({"after":first,"job":null,"title":channel["name"],"result_after":ids.last(),"recovered_question":first});
        }
        let q = state["questions"]
            .as_array_mut()
            .unwrap()
            .iter_mut()
            .find(|q| s(&q["thread_id"]) == tid && s(&q["after"]) == first)
            .unwrap();
        q["question_message"] = json!(first);
        q["source"] = json!(if s(&channel["parent_id"]) == REQUEST_CHANNEL_ID {
            "request"
        } else {
            "posted"
        });
        if let Some(previous) = previous {
            for key in ["cwd", "project", "session_id", "agent"] {
                if let Some(value) = previous.get(key) {
                    q[key] = value.clone();
                }
            }
        }
    }
    if let Some(q) = state["questions"]
        .as_array_mut()
        .unwrap()
        .iter_mut()
        .rev()
        .find(|q| s(&q["thread_id"]) == tid && s(&q["question_message"]) == first)
    {
        q["message_ids"] = json!(ids);
        // Keep the first posted ID: an answer between chunks must not disappear.
    }
}

fn exclusion(
    ctx: &Context,
    state: &Value,
    channel: &Value,
    history: &[Value],
    candidate: &Value,
) -> &'static str {
    let tid = s(&channel["id"]);
    let id = n(&candidate["id"]);
    if stopped(ctx, state, tid) {
        return "stopped";
    }
    if state["questions"].as_array().is_some_and(|qs| {
        qs.iter().any(|q| {
            s(&q["thread_id"]) == tid
                && epoch(&q["closed_at"]) >= snowflake_time(s(&candidate["id"]))
        })
    }) {
        return "tracked_or_closed";
    }
    if state["questions"].as_array().is_some_and(|qs| {
        qs.iter().any(|q| {
            s(&q["thread_id"]) == tid
                && (id <= n(&q["after"])
                    || id <= n(&q["question_message"])
                    || id <= n(&q["seen_reply"])
                    || epoch(&q["closed_at"]) >= snowflake_time(s(&candidate["id"])))
                && (truth(&q["open"]) || !crate::owner_wait::pending(ctx, tid, s(&channel["name"])))
        })
    }) {
        return "tracked_or_closed";
    }
    let owner = ctx.owner();
    if history.iter().any(|m| {
        n(&m["id"]) > id && s(&m["author"]["id"]) == owner && !crate::snooze::control_only(m)
    }) {
        return "answered";
    }
    let request = &state["requests"][tid];
    if (truth(&request["close_pending"]) && n(&request["result_after"]) >= id)
        || history.iter().any(|m| {
            n(&m["id"]) > id
                && m["author"]["id"] == candidate["author"]["id"]
                && (s(&m["content"]).starts_with("終わりました。")
                    || s(&m["content"]).starts_with("状態: 完了"))
        })
    {
        return "completed";
    }
    "missing"
}

pub fn recover(ctx: &mut Context, args: &RecoverArgs) -> Result<Value> {
    if args.apply && args.dry_run {
        return Err("--apply and --dry-run cannot be combined".into());
    }
    if args.limit == 0 || args.limit > 100 {
        return Err("limit must be between 1 and 100".into());
    }
    if args.message.is_some() && args.thread.len() != 1 {
        return Err("--message requires exactly one --thread".into());
    }
    for id in args.thread.iter().chain(args.message.iter()) {
        if id.is_empty() || !id.chars().all(|c| c.is_ascii_digit()) {
            return Err("thread/message must be a numeric Discord ID".into());
        }
    }
    let _guard = if args.apply {
        Some(crate::state_work::StateWork::begin(ctx)?)
    } else {
        None
    };
    let mut state = ctx.load()?;
    state["questions"]
        .as_array()
        .ok_or("invalid question state")?;
    let mut threads: BTreeSet<String> = args.thread.iter().cloned().collect();
    if threads.is_empty() {
        threads.extend(
            state["questions"]
                .as_array()
                .into_iter()
                .flatten()
                .map(|q| s(&q["thread_id"]).to_owned()),
        );
        threads.extend(
            state["requests"]
                .as_object()
                .into_iter()
                .flat_map(|r| r.keys().cloned()),
        );
        let logs = ctx.here.join("log_threads.json");
        if logs.is_file() {
            threads.extend(
                read_json(&logs)?
                    .as_object()
                    .into_iter()
                    .flat_map(|r| r.values().map(|v| s(v).to_owned())),
            );
        }
    }
    let total = threads.len();
    let me = ctx.call("GET", "/users/@me", None)?;
    if me["bot"] != true || s(&me["id"]).is_empty() {
        return Err("bot identity unavailable".into());
    }
    let names = ProjectNames::load(&ctx.client_projects)?;
    let mut rows = vec![];
    let mut registered = 0;
    // Recent threads first; bounded explicit inspection never replays the old 182-question batch.
    for tid in threads.into_iter().rev().take(args.limit) {
        let channel = ctx.call("GET", &format!("/channels/{tid}"), None)?;
        if channel["id"] != tid
            || ![10, 11, 12].contains(&n(&channel["type"]))
            || channel["guild_id"] != ctx.cfg["guild_id"]
        {
            return Err("thread is outside the configured guild".into());
        }
        let known = state["questions"]
            .as_array()
            .is_some_and(|qs| qs.iter().any(|q| s(&q["thread_id"]) == tid));
        if !known
            && s(&channel["parent_id"]) != REQUEST_CHANNEL_ID
            && !(channel["parent_id"] == ctx.cfg["task_channel_id"]
                && channel["owner_id"] == me["id"])
        {
            return Err("registered question, request, or bot task thread required".into());
        }
        let history = thread_history(ctx, &tid)?;
        let candidate = history.iter().rev().find(|m| {
            m["author"]["id"] == me["id"]
                && args.message.as_ref().is_none_or(|id| m["id"] == *id)
                && (is_question(s(&m["content"]))
                    || pending_reference(ctx, &state, &channel, s(&m["content"])))
        });
        let (status, message_id) = if let Some(candidate) = candidate {
            let status = exclusion(ctx, &state, &channel, &history, candidate);
            let id = s(&candidate["id"]).to_owned();
            if args.apply && status == "missing" {
                let original = if pending_reference(ctx, &state, &channel, s(&candidate["content"]))
                {
                    state["questions"]
                        .as_array()
                        .unwrap()
                        .iter()
                        .rev()
                        .find(|q| s(&q["thread_id"]) == tid)
                        .map(|q| q.get("original_question").unwrap_or(&q["after"]).clone())
                } else {
                    None
                };
                register_posted(&mut state, &channel, &id, std::slice::from_ref(&id));
                let target = state["questions"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .rposition(|q| {
                        s(&q["thread_id"]) == tid
                            && (s(&q["after"]) == id || s(&q["question_message"]) == id)
                    })
                    .ok_or("registered question generation not found")?;
                let q = state["questions"]
                    .as_array_mut()
                    .unwrap()
                    .get_mut(target)
                    .unwrap();
                q["open"] = json!(true);
                q["title"] = json!(names.redact(&safe_text(s(&channel["name"]))));
                if let Some(original) = original {
                    q["original_question"] = original.clone();
                    q["message_ids"] = json!([original, id]);
                    q["pending_owner_action"] = json!(true);
                }
                crate::snooze::apply(q, &history, &ctx.owner());
                // Re-read replies under the lock immediately before registration commits.
                // No receipt or job is started by this command.
                let owner = ctx.owner();
                let current_history = thread_history(ctx, &tid)?;
                let completed = completed_after(&current_history, &json!(id));
                let replies: Vec<_> = current_history
                    .iter()
                    .filter(|m| n(&m["id"]) > n(&json!(id)) && s(&m["author"]["id"]) == owner)
                    .collect();
                if completed || replies.iter().any(|m| !crate::snooze::control_only(m)) {
                    let q = state["questions"]
                        .as_array_mut()
                        .unwrap()
                        .get_mut(target)
                        .unwrap();
                    q["open"] = json!(false);
                    q["answered_at"] = json!(timestamp());
                    if completed {
                        q["completed_before_registration"] = json!(true);
                    }
                    if let Some(last) = replies.last() {
                        acknowledge_reply(q, s(&last["id"]));
                    }
                }
                ctx.save(&state)?;
                registered += 1;
            }
            (status, Some(id))
        } else {
            ("no_concrete_question", None)
        };
        rows.push(json!({"thread_id":tid,"message_id":message_id,"title":names.redact(&safe_text(s(&channel["name"]))),"status":status,
            "url":format!("https://discord.com/channels/{}/{tid}",s(&ctx.cfg["guild_id"]))}));
    }
    Ok(
        json!({"read_only":!args.apply,"total_threads":total,"inspected":rows.len(),"missing":rows.iter().filter(|r|r["status"]=="missing").count(),"registered":registered,"threads":rows}),
    )
}
