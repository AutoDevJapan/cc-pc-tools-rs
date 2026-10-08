//! Edit the existing forum starter. A lost PATCH response is safe to repeat.
use crate::{Api, Lock, Paths, Result, load, save};
use serde_json::{Value, json};
use std::time::Duration;

const HEADER: &str = "止まった作業の再開記録（通知なし）";

pub fn publish(api: &mut dyn Api, cfg: &Value, paths: &Paths, text: &str) -> Result<()> {
    publish_report(api, cfg, paths, text, text)
}

pub fn publish_report(
    api: &mut dyn Api,
    _cfg: &Value,
    paths: &Paths,
    key: &str,
    text: &str,
) -> Result<()> {
    if text.is_empty() {
        return Ok(());
    }
    let file = paths.store.with_file_name("stall_summary.json");
    let _guard = Lock::acquire(&file.with_extension("lock"), Duration::from_secs(2))?;
    let mut ledger = if file.try_exists().map_err(|e| e.to_string())? {
        load(&file)?
    } else {
        json!({"entries":[],"seen":[]})
    };
    if !ledger["entries"].is_array() || !ledger["seen"].is_array() {
        return Err("invalid stall summary ledger".into());
    }
    // The report includes the ToDo ID and recovery source. Keep all receipts;
    // trimming the displayed summary must not allow an old report to reappear.
    if !ledger["seen"].as_array().unwrap().iter().any(|v| v == key) {
        ledger["seen"].as_array_mut().unwrap().push(json!(key));
        ledger["entries"]
            .as_array_mut()
            .unwrap()
            .push(json!({"key":key,"text":text}));
        while ledger["entries"].as_array().unwrap().len() > 8 {
            ledger["entries"].as_array_mut().unwrap().remove(0);
        }
        // Save the desired content BEFORE PATCH. A restart continues the same edit.
        save(&file, &ledger)?;
    }
    let content = format!(
        "{HEADER}\n{}",
        ledger["entries"]
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v["text"]
                .as_str()
                .unwrap_or("")
                .chars()
                .take(220)
                .collect::<String>())
            .collect::<Vec<_>>()
            .join("\n")
    );
    let thread = std::fs::read_to_string(&paths.thread)
        .map_err(|_| "existing stall thread unavailable")?
        .trim()
        .to_owned();
    if thread.is_empty() || !thread.bytes().all(|b| b.is_ascii_digit()) {
        return Err("invalid existing stall thread ID".into());
    }
    // Discord forum starter messages have the same ID as their thread.
    let route = format!("/channels/{thread}/messages/{thread}");
    let message = api.call("GET", &route, &Value::Null)?;
    let me = api.call("GET", "/users/@me", &Value::Null)?;
    if message["id"] != thread || me["bot"] != true || message["author"]["id"] != me["id"] {
        return Err("stall summary starter is not owned by this bot".into());
    }
    if message["content"] == content {
        return Ok(());
    }
    api.call(
        "PATCH",
        &format!("/channels/{thread}"),
        &json!({"archived":false}),
    )?;
    api.call(
        "PATCH",
        &route,
        &json!({"content":content,"allowed_mentions":{"parse":[]}}),
    )?;
    Ok(())
}
