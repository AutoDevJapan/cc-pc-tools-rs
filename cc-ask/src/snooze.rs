//! Explicit owner instructions only; relative dates use the message's time in Japan.
use crate::core::{n, s, snowflake_time, truth};
use chrono::{DateTime, Datelike, Duration, FixedOffset, NaiveDate, TimeZone};
use regex::Regex;
use serde_json::{json, Value};
use std::sync::LazyLock;

static INSTRUCTION: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^(?:再通知|再送|再メンション|リマインド)(?:は|を)\s*(.+?)(?:にして|に変更|まで待って|まで止めて|まで停止)(?:ください|下さい|ほしい|欲しい|お願い|ね|くれ|もらえますか)?[。！!\s]*$").unwrap()
});
static RELATIVE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^(\d+)(分|時間|日)後$").unwrap());
static DATED: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^(?:(\d{4})(?:年|/|-))?(\d{1,2})(?:月|/|-)(\d{1,2})(?:日)?(.*)$").unwrap()
});
static TIME: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"^(?:(午前|午後))?(\d{1,2})(?::(\d{1,2})|時(?:(\d{1,2})分|半)?)[\s]*(?:JST|日本時間)?$",
    )
    .unwrap()
});

fn jst() -> FixedOffset {
    FixedOffset::east_opt(9 * 3600).unwrap()
}

/// Date-only instructions mean 09:00 JST. Invalid/past explicit dates are not guessed.
pub fn parse(text: &str, sent_at: f64) -> Option<DateTime<FixedOffset>> {
    let base = DateTime::from_timestamp_millis((sent_at * 1000.) as i64)?.with_timezone(&jst());
    let normalized: String = text
        .chars()
        .map(|c| match c {
            '０'..='９' => char::from_u32(c as u32 - '０' as u32 + '0' as u32).unwrap(),
            '：' => ':',
            '／' => '/',
            '　' => ' ',
            _ => c,
        })
        .collect();
    let mut fence: Option<(char, usize)> = None;
    let mut found = None;
    for line in normalized.lines() {
        if line.starts_with("    ") || line.starts_with('\t') {
            continue;
        }
        let line = line.trim();
        let delimiter = line.chars().next().unwrap_or(' ');
        let width = line.chars().take_while(|c| *c == delimiter).count();
        if matches!(delimiter, '`' | '~') && width >= 3 {
            if let Some((opening, count)) = fence {
                if delimiter == opening && width >= count && line[width..].trim().is_empty() {
                    fence = None;
                }
            } else {
                fence = Some((delimiter, width));
            }
            continue;
        }
        if fence.is_some() {
            continue;
        }
        let Some(c) = INSTRUCTION.captures(line) else {
            continue;
        };
        // Multiple instructions in one message are ambiguous; leave them as normal replies.
        if found.is_some() {
            return None;
        }
        found = Some(c[1].trim().to_string());
    }
    let target = found?;
    if ["今", "今すぐ", "すぐ"].contains(&target.as_str()) {
        return Some(base);
    }
    if let Ok(at) = DateTime::parse_from_rfc3339(&target) {
        return (at >= base).then_some(at);
    }
    if let Some(c) = RELATIVE.captures(&target) {
        let amount: i64 = c[1].parse().ok()?;
        let seconds = amount.checked_mul(match &c[2] {
            "分" => 60,
            "時間" => 3600,
            _ => 86400,
        })?;
        return base.checked_add_signed(Duration::try_seconds(seconds)?);
    }
    let mut rest = target.as_str();
    let mut date = None;
    for (word, days) in [
        ("明後日", 2),
        ("あさって", 2),
        ("明日", 1),
        ("あした", 1),
        ("今日", 0),
        ("本日", 0),
    ] {
        if let Some(tail) = rest.strip_prefix(word) {
            date = Some(base.date_naive().checked_add_signed(Duration::days(days))?);
            rest = tail.trim_start_matches(['の', ' ', '、']);
            break;
        }
    }
    if date.is_none() {
        if let Some(c) = DATED.captures(rest) {
            let year = c
                .get(1)
                .map(|m| m.as_str().parse().ok())
                .unwrap_or(Some(base.year()))?;
            date = Some(NaiveDate::from_ymd_opt(
                year,
                c[2].parse().ok()?,
                c[3].parse().ok()?,
            )?);
            rest = c
                .get(4)?
                .as_str()
                .trim_start_matches(['の', ' ', 'T', '、']);
        }
    }
    let (hour, minute) = if rest.is_empty() {
        (9, 0)
    } else {
        let c = TIME.captures(rest)?;
        let mut hour: u32 = c[2].parse().ok()?;
        if let Some(period) = c.get(1) {
            if hour > 12 {
                return None;
            }
            hour %= 12;
            if period.as_str() == "午後" {
                hour += 12;
            }
        }
        let minute = c
            .get(3)
            .or_else(|| c.get(4))
            .map(|m| m.as_str().parse().ok())
            .unwrap_or(Some(if rest.contains('半') { 30 } else { 0 }))?;
        (hour, minute)
    };
    let explicit_date = date.is_some();
    let day = date.unwrap_or(base.date_naive());
    let mut at = jst()
        .from_local_datetime(&day.and_hms_opt(hour, minute, 0)?)
        .single()?;
    if !explicit_date && at < base {
        at = at.checked_add_signed(Duration::days(1))?;
    }
    (at >= base).then_some(at)
}

pub fn control(message: &Value) -> Option<DateTime<FixedOffset>> {
    parse(s(&message["content"]), snowflake_time(s(&message["id"])))
}

/// A mixed reply still carries a substantive answer; defer it, then deliver it normally.
pub fn control_only(message: &Value) -> bool {
    let lines: Vec<_> = s(&message["content"])
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .collect();
    lines.len() == 1 && INSTRUCTION.is_match(lines[0]) && control(message).is_some()
}

pub fn paused(question: &Value, now: f64) -> bool {
    question["remind_not_before"]
        .as_f64()
        .is_some_and(|at| now < at)
}

pub fn thread_paused(state: &Value, thread: &str, now: f64) -> bool {
    state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .rev()
        .find(|q| s(&q["thread_id"]) == thread)
        .is_some_and(|q| paused(q, now))
}

/// Does not consume substantive answers, post a receipt, or start a worker.
pub fn apply(question: &mut Value, messages: &[Value], owner: &str) {
    let latest = messages
        .iter()
        .filter(|m| {
            s(&m["author"]["id"]) == owner
                && !truth(&m["author"]["bot"])
                && n(&m["id"]) > n(&question["remind_reply"])
                && question["remind_set_at"]
                    .as_f64()
                    .is_none_or(|set_at| snowflake_time(s(&m["id"])) > set_at)
        })
        .filter_map(|m| control(m).map(|at| (m, at)))
        .max_by_key(|(m, _)| n(&m["id"]));
    if let Some((message, at)) = latest {
        question["remind_not_before"] = json!(at.timestamp_millis() as f64 / 1000.);
        question["remind_not_before_iso"] = json!(at.to_rfc3339());
        question["remind_reply"] = message["id"].clone();
    }
}

pub fn apply_thread(state: &mut Value, thread: &str, messages: &[Value], owner: &str) {
    if let Some(question) = state["questions"]
        .as_array_mut()
        .into_iter()
        .flatten()
        .rev()
        .find(|q| s(&q["thread_id"]) == thread)
    {
        apply(question, messages, owner);
    }
}
