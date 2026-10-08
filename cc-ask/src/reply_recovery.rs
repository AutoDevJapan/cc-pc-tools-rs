//! Durable, bounded retries for delivering an owner's answer. Receipt is not delivery.
use crate::core::*;
use serde_json::{json, Value};

pub const RETRY_WINDOW_S: f64 = 900.;
pub const RETRY_INTERVAL_S: f64 = 60.;

pub fn transient(error: &str) -> bool {
    let error = error.to_lowercase();
    [
        "transport failed",
        "urlerror",
        "oserror",
        "timed out",
        "timeout",
        "connection reset",
        "connection refused",
        "connection aborted",
        "network",
        "dns",
        "http 502",
        "http 503",
        "http 504",
        "bad gateway",
        "service unavailable",
    ]
    .iter()
    .any(|part| error.contains(part))
}

pub fn failed(record: &mut Value, now: f64, error: &str, reply: &str) {
    if s(&record["reply_resume_retry"]["reply"]) != reply {
        delivered(record);
    }
    record["resume_status_reply"] = json!(reply);
    record["last_resume_error"] = json!(error_detail(error));
    record["resume_status"] = json!("再開失敗");
    record["open"] = json!(true);
    if !transient(error) {
        return;
    }
    let retry = &mut record["reply_resume_retry"];
    if !retry.is_object() {
        *retry = json!({"reply":reply,"started_at":now,"deadline":now + RETRY_WINDOW_S});
    }
    retry["last_error"] = json!(error_detail(error));
    retry["transient"] = json!(transient(error));
    retry["next_at"] = json!(now + RETRY_INTERVAL_S);
    retry["exhausted"] = json!(now >= retry["deadline"].as_f64().unwrap_or(now));
}

pub fn pending_reply(state: &Value, record: &Value, thread: &str) -> String {
    let latest = question_answer_id(state, record);
    let receipts = state["answer_receipts"]
        .as_object()
        .into_iter()
        .flatten()
        .filter(|(id, r)| s(&r["thread"]) == thread && !reply_delivered(state, id))
        .filter_map(|(id, _)| id.parse::<u64>().ok());
    receipts
        .chain(std::iter::once(latest).filter(|id| !reply_delivered(state, &id.to_string())))
        .filter(|id| *id > n(&record["after"]))
        .max()
        .map(|id| id.to_string())
        .unwrap_or_default()
}

/// A manual read resolves the old failure. A new answer gets a new retry window.
pub fn refresh(state: &Value, record: &mut Value, thread: &str) {
    let pending = pending_reply(state, record, thread);
    let failure = s(&record["resume_status_reply"]);
    if (!failure.is_empty() && failure != pending)
        || (truth(&record["resume_status"]) && pending.is_empty())
    {
        delivered(record);
    }
}

/// No sleeps: the existing minute tick owns the next attempt. The deadline is
/// absolute and survives process restarts, including ticks after its expiry.
pub fn due(record: &mut Value, now: f64) -> bool {
    if !record["reply_resume_retry"].is_object() {
        return true;
    }
    if now
        >= record["reply_resume_retry"]["deadline"]
            .as_f64()
            .unwrap_or(now)
    {
        record["reply_resume_retry"]["exhausted"] = json!(true);
        return false;
    }
    now >= record["reply_resume_retry"]["next_at"]
        .as_f64()
        .unwrap_or(0.)
}

pub fn delivered(record: &mut Value) {
    if let Some(object) = record.as_object_mut() {
        for key in [
            "reply_resume_retry",
            "resume_status",
            "resume_status_reply",
            "last_resume_error",
        ] {
            object.remove(key);
        }
    }
}

pub fn undelivered(state: &Value, record: &Value) -> bool {
    let reply = question_answer_id(state, record);
    reply > n(&record["after"]) && !reply_delivered(state, &reply.to_string())
}

pub fn prevents_close(state: &Value, record: &Value) -> bool {
    let thread = s(&record["thread_id"]);
    let pending = pending_reply(state, record, thread);
    let relevant = |r: &Value| {
        s(&r["resume_status_reply"]).is_empty() || s(&r["resume_status_reply"]) == pending
    };
    !pending.is_empty()
        && ((relevant(record)
            && (truth(&record["resume_status"]) || transient(s(&record["last_resume_error"]))))
            || (relevant(&state["requests"][thread])
                && truth(&state["requests"][thread]["resume_status"])))
}

/// Intake closes a question only after its answer has been handed to a worker.
pub fn close_delivered_questions(state: &mut Value, thread: &str) {
    let finished: Vec<_> = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .enumerate()
        .filter(|(_, q)| {
            s(&q["thread_id"]) == thread
                && truth(&q["open"])
                && !truth(&q["continuation"])
                && question_answered(state, q)
                && !undelivered(state, q)
                && !state["answer_receipts"]
                    .as_object()
                    .into_iter()
                    .flatten()
                    .any(|(id, r)| {
                        s(&r["thread"]) == thread
                            && n(&json!(id)) > n(&q["after"])
                            && !reply_delivered(state, id)
                    })
        })
        .map(|(index, _)| index)
        .collect();
    for index in finished {
        let q = &mut state["questions"][index];
        delivered(q);
        q["open"] = json!(false);
        q["answered_at"] = json!(timestamp());
    }
}

/// An explicit operator retry repairs a specified legacy close or expired retry.
/// It never launches jobs; the next minute tick still applies ownership checks.
pub fn retry_reply(ctx: &mut Context, thread: &str) -> Result<Value> {
    if thread.is_empty() || !thread.bytes().all(|b| b.is_ascii_digit()) {
        return Err("スレッドにはDiscordの数値IDを指定してください".into());
    }
    let _work = crate::state_work::StateWork::begin(ctx)?;
    let mut state = ctx.load()?;
    if crate::routing::thread_inbox_only(ctx, &state, thread) {
        return Err("メインへの回答はinboxから受け取ってください".into());
    }
    if crate::questions::stopped(ctx, &state, thread) {
        return Err("オーナーが停止した質問は再開しません".into());
    }
    let index = state["questions"]
        .as_array()
        .ok_or("invalid question state")?
        .iter()
        .rposition(|q| s(&q["thread_id"]) == thread)
        .ok_or("question not found")?;
    let mut question = state["questions"][index].clone();
    if ["owner_stopped", "stopped", "cancelled"]
        .iter()
        .any(|key| truth(&question[*key]))
    {
        return Err("オーナーが停止した質問は再開しません".into());
    }
    if !crate::routing::question_allowed(ctx, &question)? {
        return Err("question destination is not allowed".into());
    }
    let history = thread_history(ctx, thread)?;
    record_owner_answers(&mut state, thread, &history, &ctx.owner());
    question = state["questions"][index].clone();
    let reply = pending_reply(&state, &question, thread);
    if reply.is_empty() || crate::questions::completed_after(&history, &json!(reply)) {
        return Err("未送達の回答がないか、回答後に完了した質問です".into());
    }
    delivered(&mut question);
    question["open"] = json!(true);
    question["reply_retry_requested_at"] = json!(ctx.now());
    for key in ["recovery_closed", "closed_at"] {
        question.as_object_mut().unwrap().remove(key);
    }
    state["questions"][index] = question;
    if state["requests"][thread].is_object() {
        delivered(&mut state["requests"][thread]);
    }
    ctx.save(&state)?;
    Ok(json!({"thread_id":thread,"reply":reply,"retry":"next_tick"}))
}
