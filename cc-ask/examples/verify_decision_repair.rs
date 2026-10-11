//! Bounded read-only acceptance of an explicitly repaired decision and its summary.
use cc_ask::core::*;
use serde_json::{json, Value};
use std::path::PathBuf;

struct ReadOnly;
impl Effects for ReadOnly {
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        if m != "GET" {
            return Err("verification may only GET".into());
        }
        RealEffects.http(m, u, b, d)
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        Err("verification cannot launch".into())
    }
}
fn verify() -> Result<Value> {
    let mut args = std::env::args().skip(1);
    let manifest = read_json(&PathBuf::from(args.next().ok_or("manifest required")?))?;
    let summary = args.next().ok_or("summary message ID required")?;
    if summary.is_empty() || !summary.bytes().all(|b| b.is_ascii_digit()) {
        return Err("numeric summary ID required".into());
    }
    let home = std::env::var_os("USERPROFILE").ok_or("home unavailable")?;
    let cfg = read_json(&PathBuf::from(home).join(".godd/discord-notify.json"))?;
    let here = PathBuf::from("D:/CC/Tools/discord-ask");
    let mut ctx = Context {
        here: here.clone(),
        client_projects: PathBuf::from("D:/CC/Tools/client-projects.txt"),
        civ6_home: here.clone(),
        jobs: PathBuf::from("D:/CC/Tools/codex-jobs"),
        discord_tasks: here.clone(),
        todo: PathBuf::from("D:/CC/Tools/todo/todo.json"),
        cfg,
        effects: Box::new(ReadOnly),
        codex_command: here.clone(),
        scheduler_command: here.clone(),
        shepherd_command: here.clone(),
        todo_command: here,
    };
    let ids = manifest["threads"]
        .as_array()
        .ok_or("explicit threads required")?;
    if ids.is_empty() || ids.len() > 10 {
        return Err("bounded thread set required".into());
    }
    let state = ctx.load()?;
    let owner = ctx.owner();
    let sender = cc_ask::question_progress::route(&ctx, s(&ids[0]))?
        .ok_or("question route unavailable")?["sender"]
        .clone();
    let mut channels = Vec::new();
    let mut progress = String::new();
    for id in ids {
        let q = state["questions"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|q| q["thread_id"] == *id)
            .ok_or("question unregistered")?;
        if truth(&q["open"]) {
            return Err("question still open".into());
        }
        let channel = ctx.call("GET", &format!("/channels/{}", s(id)), None)?;
        if channel["guild_id"] != ctx.cfg["guild_id"]
            || channel["parent_id"] != ctx.cfg["task_channel_id"]
            || !truth(&channel["thread_metadata"]["archived"])
        {
            return Err("question archive not verified".into());
        }
        let replies = owner_replies(&mut ctx, q, &owner)?;
        if replies.is_empty() {
            return Err("original owner reply missing".into());
        }
        channels.push(json!({"thread_id":id,"archived":true,"owner_reply_ids":replies.iter().map(|m|m["id"].clone()).collect::<Vec<_>>()}));
        if progress.is_empty() {
            progress = s(&q["progress_thread_id"]).into();
        }
    }
    let message = ctx.call(
        "GET",
        &format!("/channels/{progress}/messages/{summary}"),
        None,
    )?;
    if message["author"]["id"] != sender
        || n(&message["flags"]) & (SILENT | 4) != (SILENT | 4)
        || message["mentions"].as_array().is_none_or(|a| !a.is_empty())
        || truth(&message["mention_everyone"])
        || truth(&message["message_reference"])
    {
        return Err("summary not silent".into());
    }
    Ok(
        json!({"archived_questions":channels,"summary":{"thread_id":progress,"id":message["id"],"flags":message["flags"],"mentions":0,"reply_reference":false}}),
    )
}
fn main() {
    match verify() {
        Ok(v) => println!("{v}"),
        Err(e) => {
            eprintln!("{e}");
            std::process::exit(1)
        }
    }
}
