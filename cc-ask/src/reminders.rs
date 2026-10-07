//! Bounded hourly reminders, with a durable outbox and recovery of lost POST responses.
use crate::{core::*, project::*};
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};

pub const MAX_PER_RUN: usize = 3;
pub const MAX_PER_HOUR: usize = 5;
const MAX_GROUP: usize = 6;

#[derive(Clone, Debug, clap::Args)]
pub struct RemindArgs {
    /// Inspect due reminders without writing state or sending/acknowledging anything.
    #[arg(long)]
    pub dry_run: bool,
    /// At most three notification messages per invocation, five per rolling hour globally.
    #[arg(long, default_value_t = MAX_PER_RUN)]
    pub limit: usize,
    #[arg(long)]
    pub thread: Vec<String>,
}

impl Default for RemindArgs {
    fn default() -> Self {
        Self {
            dry_run: false,
            limit: MAX_PER_RUN,
            thread: vec![],
        }
    }
}

fn question_index(state: &Value, reference: &Value) -> Option<usize> {
    state["questions"]
        .as_array()?
        .iter()
        .rposition(|q| q["thread_id"] == reference["thread_id"] && q["after"] == reference["after"])
}

fn answered(history: &[Value], after: &Value, owner: &str) -> bool {
    history.iter().any(|m| {
        n(&m["id"]) > n(after) && s(&m["author"]["id"]) == owner && !crate::snooze::control_only(m)
    })
}

fn hourly_used(state: &Value, now: f64) -> usize {
    let mut messages = BTreeSet::new();
    for sent in state["reminder_sent"].as_array().into_iter().flatten() {
        if sent["at"].as_f64().is_some_and(|at| now - at < 3600.) {
            messages.insert(s(&sent["message_id"]).to_owned());
        }
    }
    // Include the old reminder ledger during deployment; changing binaries must
    // not reset the global hourly budget. Grouped items share their message ID.
    for q in state["questions"].as_array().into_iter().flatten() {
        if q["last_ping"]
            .as_f64()
            .is_some_and(|at| at > 0. && now - at < 3600.)
        {
            messages.insert(if s(&q["last_ping_message"]).is_empty() {
                format!("legacy:{}:{}", s(&q["thread_id"]), s(&q["after"]))
            } else {
                s(&q["last_ping_message"]).to_owned()
            });
        }
    }
    messages.len()
}

fn finalize(ctx: &Context, state: &mut Value, pending: &Value, message: &Value) -> Result<()> {
    pending["attempted_at"]
        .as_f64()
        .ok_or("reminder outbox has no timestamp")?;
    let at = ctx.now();
    for reference in pending["questions"]
        .as_array()
        .ok_or("invalid reminder outbox")?
    {
        if let Some(index) = question_index(state, reference) {
            state["questions"][index]["last_ping"] = json!(at);
            state["questions"][index]["last_ping_message"] = message["id"].clone();
            for key in ["project", "title"] {
                if let Some(value) = reference.get(key).filter(|v| v.is_string()) {
                    state["questions"][index][key] = value.clone();
                }
            }
        }
    }
    if !state["reminder_sent"].is_array() {
        state["reminder_sent"] = json!([]);
    }
    state["reminder_sent"]
        .as_array_mut()
        .unwrap()
        .push(json!({"at":at,"message_id":message["id"],"nonce":pending["nonce"]}));
    state["reminder_outbox"]
        .as_object_mut()
        .ok_or("invalid reminder outbox")?
        .remove(s(&pending["nonce"]));
    // Checkpoint every successful send before attempting the next group.
    ctx.save(state)
}

fn deliver(ctx: &mut Context, state: &mut Value, pending: &Value) -> Result<bool> {
    let tid = s(&pending["thread_id"]);
    let history = thread_history(ctx, tid)?;
    // Nonce alone is insufficient after Discord's short deduplication window.
    // The durable pre-send history cursor also identifies an already delivered message.
    if let Some(message) = history.iter().find(|m| {
        truth(&m["author"]["bot"])
            && (m["nonce"] == pending["nonce"]
                || (n(&m["id"]) > n(&pending["history_after"])
                    && m["content"] == pending["content"]))
    }) {
        finalize(ctx, state, pending, message)?;
        return Ok(false);
    }
    let owner = ctx.owner();
    // Recheck all group members immediately before POST. If the group changed,
    // drop it and rebuild next time; never notify with stale answered items.
    for reference in pending["questions"]
        .as_array()
        .ok_or("invalid reminder outbox")?
    {
        let Some(index) = question_index(state, reference) else {
            return discard(ctx, state, pending);
        };
        let question = state["questions"][index].clone();
        let member = s(&question["thread_id"]);
        if !truth(&question["open"])
            || crate::questions::stopped(ctx, state, member)
            || crate::snooze::paused(&question, ctx.now())
            || n(&question["seen_reply"]) > n(&question["after"])
            || question_answered(state, &question)
        {
            return discard(ctx, state, pending);
        }
        let member_history = if member == tid {
            history.clone()
        } else {
            thread_history(ctx, member)?
        };
        crate::snooze::apply(&mut state["questions"][index], &member_history, &owner);
        if crate::snooze::paused(&state["questions"][index], ctx.now())
            || answered(&member_history, &question["after"], &owner)
            || crate::questions::completed_after(&member_history, &question["after"])
        {
            return discard(ctx, state, pending);
        }
    }
    let response = ctx.call(
        "POST",
        &format!("/channels/{tid}/messages"),
        Some(&json!({
            "content":pending["content"],"allowed_mentions":{"parse":[],"users":[owner]},
            "nonce":pending["nonce"],"enforce_nonce":true
        })),
    )?;
    if s(&response["id"]).is_empty() {
        return Err("reminder returned no message ID".into());
    }
    finalize(ctx, state, pending, &response)?;
    Ok(true)
}

fn discard(ctx: &Context, state: &mut Value, pending: &Value) -> Result<bool> {
    state["reminder_outbox"]
        .as_object_mut()
        .ok_or("invalid reminder outbox")?
        .remove(s(&pending["nonce"]));
    ctx.save(state)?;
    Ok(false)
}

pub fn run(ctx: &mut Context, args: &RemindArgs) -> Result<Value> {
    let _guard = if args.dry_run {
        None
    } else {
        Some(crate::state_work::StateWork::begin(ctx)?)
    };
    run_locked(ctx, args)
}

pub fn run_locked(ctx: &mut Context, args: &RemindArgs) -> Result<Value> {
    if args.limit == 0 || args.limit > MAX_PER_RUN {
        return Err("limit must be between 1 and 3".into());
    }
    if args
        .thread
        .iter()
        .any(|id| id.is_empty() || !id.chars().all(|c| c.is_ascii_digit()))
    {
        return Err("thread must be a numeric Discord ID".into());
    }
    let mut state = ctx.load()?;
    state["questions"]
        .as_array()
        .ok_or("invalid question state")?;
    if (!state["reminder_outbox"].is_null() && !state["reminder_outbox"].is_object())
        || (!state["reminder_sent"].is_null() && !state["reminder_sent"].is_array())
    {
        return Err("invalid reminder ledger; repair it before retrying".into());
    }
    let now = ctx.now();
    let owner = ctx.owner();
    let names = ProjectNames::load(&ctx.client_projects)?;
    let mut sent = 0;
    let mut attempts = 0;
    let pending: Vec<_> = state["reminder_outbox"]
        .as_object()
        .into_iter()
        .flat_map(|o| o.values().cloned())
        .collect();
    if !args.dry_run {
        for item in pending {
            crate::tick_runtime::check()?;
            if attempts == args.limit {
                break;
            }
            if hourly_used(&state, now) >= MAX_PER_HOUR {
                break;
            }
            if !args.thread.is_empty() && !args.thread.iter().any(|t| t == s(&item["thread_id"])) {
                continue;
            }
            attempts += 1;
            sent += usize::from(deliver(ctx, &mut state, &item)?);
        }
    }
    let recent = hourly_used(&state, now);
    let reserved = state["reminder_outbox"].as_object().map_or(0, |o| o.len());
    let available = MAX_PER_HOUR
        .saturating_sub(recent + reserved)
        .min(args.limit - attempts);
    let mut groups: BTreeMap<(String, String), Vec<Vec<Value>>> = BTreeMap::new();
    let count = state["questions"]
        .as_array()
        .ok_or("invalid question state")?
        .len();
    for index in 0..count {
        crate::tick_runtime::check()?;
        let q = state["questions"][index].clone();
        let tid = s(&q["thread_id"]);
        if !truth(&q["open"])
            || (!args.thread.is_empty() && !args.thread.iter().any(|t| t == tid))
            || state["questions"]
                .as_array()
                .unwrap()
                .iter()
                .skip(index + 1)
                .any(|later| later["thread_id"] == q["thread_id"])
            || crate::questions::stopped(ctx, &state, tid)
            || truth(&state["requests"][tid]["close_pending"])
            || truth(&q["continuation"]["close_pending"])
            || n(&q["seen_reply"]) > n(&q["after"])
            || question_answered(&state, &q)
        {
            continue;
        }
        if !crate::routing::question_allowed(ctx, &q)? {
            continue;
        }
        let history = thread_history(ctx, tid)?;
        crate::snooze::apply(&mut state["questions"][index], &history, &owner);
        if crate::snooze::paused(&state["questions"][index], now)
            || answered(&history, &q["after"], &owner)
            || crate::questions::completed_after(&history, &q["after"])
        {
            continue;
        }
        // Recovery or a continuation can replace the active ledger generation.
        // An hourly item is still the same thread: keep the most recent successful
        // ping from all its generations, including a reconciled old outbox receipt.
        let last = state["questions"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|prior| prior["thread_id"] == q["thread_id"])
            .filter_map(|prior| prior["last_ping"].as_f64().filter(|v| *v > 0.))
            .fold(snowflake_time(s(&q["after"])), f64::max);
        if now - last < 3600. {
            continue;
        }
        let project = question_project(ctx, &q, &names);
        let title = names
            .redact(&safe_text(s(&q["title"])))
            .chars()
            .take(160)
            .collect::<String>();
        // Missing project metadata in request results must not combine unrelated
        // products in the generic Tools thread. Same topic/project still batches.
        let scope = if s(&q["project"]).is_empty()
            && (s(&q["source"]) == "request" || s(&q["cwd"]).is_empty())
        {
            title.clone()
        } else {
            String::new()
        };
        let batches = groups.entry((project.clone(), scope)).or_default();
        if batches.last().is_none_or(|g| g.len() == MAX_GROUP) {
            batches.push(vec![]);
        }
        batches.last_mut().unwrap().push(
            json!({"thread_id":tid,"after":q["after"],"title":title,"project":project,"due_since":last + 3600.,
            "url":format!("https://discord.com/channels/{}/{tid}",s(&ctx.cfg["guild_id"]))}),
        );
    }
    let due = groups.values().flatten().map(Vec::len).sum::<usize>();
    let mut selected: Vec<_> = groups
        .into_iter()
        .flat_map(|((p, _), groups)| groups.into_iter().map(move |g| (p.clone(), g)))
        .collect();
    selected.sort_by(|a, b| {
        let oldest = |group: &Vec<Value>| {
            group
                .iter()
                .filter_map(|q| q["due_since"].as_f64())
                .min_by(f64::total_cmp)
                .unwrap_or(0.)
        };
        oldest(&a.1).total_cmp(&oldest(&b.1))
    });
    selected.truncate(available);
    let preview: Vec<_> = selected
        .iter()
        .map(|(p, g)| json!({"project":p,"questions":g}))
        .collect();
    if !args.dry_run {
        if state["reminder_sent"].is_array() {
            state["reminder_sent"]
                .as_array_mut()
                .unwrap()
                .retain(|v| v["at"].as_f64().is_some_and(|at| now - at < 3600.));
        }
        for (project, questions) in selected {
            crate::tick_runtime::check()?;
            let tid = s(&questions[0]["thread_id"]);
            let content = if questions.len() == 1 {
                reminder_text(&project, &owner, s(&questions[0]["title"]))
            } else {
                format!("<@{owner}> 【{project}】未回答の質問をまとめて再通知します。各スレッドへの回答をお願いします。\n{}", questions.iter().map(|q|format!("・{} {}",s(&q["title"]),s(&q["url"]))).collect::<Vec<_>>().join("\n"))
            };
            if content.chars().count() > MAX_CONTENT {
                return Err("reminder group exceeds Discord content limit".into());
            }
            let history = thread_history(ctx, tid)?;
            let nonce = uuid::Uuid::new_v4().simple().to_string()[..24].to_owned();
            let item = json!({"nonce":nonce,"thread_id":tid,"content":content,"questions":questions,"attempted_at":now,
                "history_after":history.iter().map(|m|n(&m["id"])).max().unwrap_or(0).to_string()});
            if !state["reminder_outbox"].is_object() {
                state["reminder_outbox"] = json!({});
            }
            state["reminder_outbox"][&nonce] = item.clone();
            ctx.save(&state)?;
            sent += usize::from(deliver(ctx, &mut state, &item)?);
        }
        ctx.save(&state)?;
    }
    Ok(
        json!({"dry_run":args.dry_run,"due_questions":due,"selected_groups":preview,"sent":sent,"hourly_used":recent + reserved,"hourly_limit":MAX_PER_HOUR}),
    )
}
