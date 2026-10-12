//! Explicit, small live acceptance probe. All three posts are silent; no owner
//! question enters production state. Created threads are archived after reading
//! back their flags/mentions. Run only with an owned, isolated scratch directory.
use cc_ask::{commands, core::*, question_progress};
use serde_json::{json, Value};
use std::path::PathBuf;
use std::{cell::Cell, rc::Rc};

struct SilentEffects {
    real: RealEffects,
    forum_posts: Rc<Cell<usize>>,
    message_posts: Rc<Cell<usize>>,
}
impl Effects for SilentEffects {
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        let mut body = b.cloned();
        if d && m == "POST" {
            if u.ends_with("/threads") {
                self.forum_posts.set(self.forum_posts.get() + 1);
                if let Some(payload) = body.as_mut().and_then(|v| v.get_mut("message")) {
                    payload["content"] =
                        json!(question_progress::silent_text(s(&payload["content"])));
                    payload["flags"] = json!(SILENT | 4);
                    payload["allowed_mentions"] =
                        json!({"parse":[],"users":[],"replied_user":false});
                }
            } else if u.ends_with("/messages") {
                self.message_posts.set(self.message_posts.get() + 1);
            }
        }
        self.real.http(m, u, body.as_ref(), d)
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        Err("live probe must not launch jobs".into())
    }
}
fn probe() -> Result<()> {
    let root = std::env::args()
        .nth(1)
        .map(PathBuf::from)
        .ok_or("supply an owned isolated directory")?;
    if !root.is_dir() || root.join("state.json").exists() {
        return Err("probe directory must be new and isolated".into());
    }
    std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(root.join("probe-started"))
        .map_err(|_| "this probe directory has already been used")?;
    let user = std::env::var_os("USERPROFILE")
        .map(PathBuf::from)
        .ok_or("missing home")?;
    let cfg = read_json(&user.join(".godd/discord-notify.json"))?;
    let forum_posts = Rc::new(Cell::new(0));
    let message_posts = Rc::new(Cell::new(0));
    let mut ctx = Context {
        here: root.clone(),
        client_projects: root.join("empty-client-list"),
        civ6_home: root.clone(),
        jobs: root.clone(),
        discord_tasks: root.clone(),
        todo: root.join("todo.json"),
        cfg,
        effects: Box::new(SilentEffects {
            real: RealEffects,
            forum_posts: forum_posts.clone(),
            message_posts: message_posts.clone(),
        }),
        codex_command: root.clone(),
        scheduler_command: root.clone(),
        shepherd_command: root.clone(),
        todo_command: root.clone(),
    };
    std::fs::write(&ctx.client_projects, "").map_err(|e| e.to_string())?;
    let parent = s(&ctx.cfg["task_channel_id"]).to_owned();
    // The exclusive probe-started marker prevents rerunning this scaffold POST.
    // Store its ID immediately for cleanup if a later verification fails.
    let original = ctx.call(
        "POST",
        &format!("/channels/{parent}/threads"),
        Some(&json!({
        "name":"【cc-ask】通知しない投稿の確認（元作業）",
        "message":{"content":"質問と進捗の投稿先を少数で確認しています。返信は不要です。",
            "flags":SILENT|4,"allowed_mentions":{"parse":[],"users":[],"replied_user":false}}})),
    )?;
    let origin = s(&original["id"]).to_owned();
    atomic_json(
        &root.join("probe-created.json"),
        &json!({"original":origin}),
        2,
    )?;
    let metadata = json!({"title":"【cc-ask】通知しない投稿の確認","project":"cc-ask",
        "cwd":root.to_string_lossy(),"source":"probe","asked_at":timestamp()});
    let result = question_progress::ask(
        &mut ctx,
        &metadata,
        &origin,
        "投稿先の確認です。返信は不要です。",
        &[],
    )?;
    let again = question_progress::ask(
        &mut ctx,
        &metadata,
        &origin,
        "投稿先の確認です。返信は不要です。",
        &[],
    )?;
    if result != again {
        return Err("retry changed the created threads".into());
    }
    if forum_posts.get() != 3 {
        return Err("retry created extra posts".into());
    }
    let body = root.join("progress.txt");
    std::fs::write(&body, "通知しない進捗投稿への配送を確認しました。")
        .map_err(|e| e.to_string())?;
    let progress = s(&result["progress_thread_id"]);
    let sent = commands::cmd_post(&mut ctx, s(&result["thread_id"]), &body, &[], true)?;
    if sent["thread_id"] != progress {
        return Err("progress reached the question thread".into());
    }
    let mut samples = vec![];
    for id in [&origin, s(&result["thread_id"]), progress] {
        let starter = ctx.call("GET", &format!("/channels/{id}/messages/{id}"), None)?;
        if n(&starter["flags"]) & (SILENT | 4) != SILENT | 4
            || starter["mentions"].as_array().is_none_or(|m| !m.is_empty())
            || truth(&starter["mention_everyone"])
            || s(&starter["content"]).contains("<@")
        {
            return Err("a probe starter was not silent".into());
        }
        samples.push(json!({"thread_id":id,"flags":starter["flags"],"mentions":0}));
    }
    let message = s(&sent["message_ids"][0]);
    let delivered = ctx.call(
        "GET",
        &format!("/channels/{progress}/messages/{message}"),
        None,
    )?;
    if n(&delivered["flags"]) & (SILENT | 4) != SILENT | 4
        || !delivered["mentions"].as_array().is_some_and(Vec::is_empty)
    {
        return Err("progress did not remain silent".into());
    }
    let report = json!({"posts":forum_posts.get(),"retry_created_posts":0,"progress_messages":message_posts.get(),"mentions":0,
        "reply_ping":false,"suppress_notifications":true,"suppress_embeds":true,"samples":samples,
        "question_thread_id":result["thread_id"],"progress_thread_id":result["progress_thread_id"],"original_thread_id":origin});
    atomic_json(&root.join("probe-observation.json"), &report, 2)?;
    for id in [&origin, s(&result["thread_id"]), progress] {
        ctx.call(
            "PATCH",
            &format!("/channels/{id}"),
            Some(&json!({"archived":true})),
        )?;
        let channel = ctx.call("GET", &format!("/channels/{id}"), None)?;
        if !truth(&channel["thread_metadata"]["archived"]) {
            return Err("probe thread was not archived".into());
        }
    }
    println!(
        "{}",
        json!({"posts":3,"retry_created_posts":0,"progress_messages":1,"mentions":0,"archived":3})
    );
    Ok(())
}
fn main() {
    if let Err(e) = probe() {
        eprintln!("{}", safe_text(e));
        std::process::exit(1);
    }
}
