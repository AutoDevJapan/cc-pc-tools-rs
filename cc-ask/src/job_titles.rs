//! Explicit, idempotent maintenance for existing job cards. Never posts messages.
use crate::core::*;
use crate::task_title::{self, Names};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, fs};

pub fn sync_job(ctx: &mut Context, job_id: &str) -> Result<()> {
    if !valid_job(job_id) {
        return Err("invalid job id".into());
    }
    let path = ctx
        .discord_tasks
        .join(format!("{:x}.json", Sha256::digest(job_id.as_bytes())));
    // An active card writer must be deferred before reading and polishing task
    // metadata. That work can otherwise consume the tick budget before the lock.
    let _held = card_lock(&path)?;
    let names = Names::load(&ctx.client_projects).map_err(|_| "client project list unavailable")?;
    let todo = if ctx.todo.is_file() {
        read_json(&ctx.todo)?
    } else {
        Value::Null
    };
    let dir = ctx.jobs.join("jobs").join(job_id);
    let job = read_json(&dir.join("job.json"))?;
    let prompt = fs::read_to_string(dir.join("prompt.md")).unwrap_or_default();
    let title =
        task_title::polish_title(&task_title::job_title(&job, &prompt, &todo, &names), &names);
    sync_card_locked(ctx, &path, &title, &names)
}

fn sync_card(ctx: &mut Context, path: &std::path::Path, title: &str, names: &Names) -> Result<()> {
    let _held = card_lock(path)?;
    sync_card_locked(ctx, path, title, names)
}

fn card_lock(path: &std::path::Path) -> Result<fs::File> {
    crate::tick_runtime::check()?;
    // AO names both the record and the whole-file lock by the SHA256 of the job ID.
    let file = fs::OpenOptions::new()
        .create(true)
        .read(true)
        .write(true)
        .truncate(false)
        .open(path.with_extension("lock"))
        .map_err(|_| "task lock unavailable")?;
    let timeout = crate::tick_runtime::timeout(std::time::Duration::from_secs(5))?;
    let _span = crate::tick_runtime::Span::new("notifier card lock");
    let start = std::time::Instant::now();
    while file.try_lock().is_err() {
        crate::tick_runtime::check()?;
        if start.elapsed() >= timeout {
            return Err("task card lock is busy; retry next update".into());
        }
        std::thread::sleep(std::time::Duration::from_millis(20));
    }
    Ok(file)
}

fn sync_card_locked(
    ctx: &mut Context,
    path: &std::path::Path,
    title: &str,
    names: &Names,
) -> Result<()> {
    let mut task = read_json(path)?;
    let tid = s(&task["thread_id"]).to_string();
    let channel = ctx.call("GET", &format!("/channels/{tid}"), None)?;
    let currently_archived = truth(&channel["thread_metadata"]["archived"]);
    // A separate journal survives AO rewriting its own typed task record.
    let restore_path = path.with_extension("restore");
    let pending = if restore_path.is_file() {
        read_json(&restore_path)?
    } else {
        Value::Null
    };
    let archived = currently_archived
        || (pending["thread_id"] == tid && truth(&pending["archived"]))
        || truth(&task["title_restore_archive"]);
    // Preserve the restoration intent before opening: survives HTTP failures or
    // helper termination, so a retry cannot mistake our open state for the original.
    if archived {
        atomic_json(&restore_path, &json!({"thread_id":tid,"archived":true}), 0)?;
    }
    if currently_archived {
        ctx.call(
            "PATCH",
            &format!("/channels/{tid}"),
            Some(&json!({"archived":false})),
        )?;
    }
    let edited = sync_contents(ctx, &task, title, names, &channel);
    let restored = if archived {
        ctx.call(
            "PATCH",
            &format!("/channels/{tid}"),
            Some(&json!({"archived":true})),
        )
        .and_then(|_| atomic_json(&restore_path, &json!({"thread_id":tid,"archived":false}), 0))
    } else {
        Ok(())
    };
    match (edited, restored) {
        (Err(edit), Err(restore)) => {
            return Err(format!("{edit}; archive restore failed: {restore}"))
        }
        (Err(edit), Ok(())) => return Err(edit),
        (Ok(()), Err(restore)) => return Err(format!("archive restore failed: {restore}")),
        (Ok(()), Ok(())) => {}
    }
    task.as_object_mut()
        .unwrap()
        .remove("title_restore_archive");
    task["title"] = json!(title);
    task["fallback_title_pending"] = json!(false);
    task["rename_pending"] = json!(false);
    atomic_json(path, &task, 0)
}

fn sync_contents(
    ctx: &mut Context,
    task: &Value,
    title: &str,
    names: &Names,
    channel: &Value,
) -> Result<()> {
    let tid = s(&task["thread_id"]);
    if s(&channel["name"]) != title {
        ctx.call(
            "PATCH",
            &format!("/channels/{tid}"),
            Some(&json!({"name":title})),
        )?;
        if s(&ctx.call("GET", &format!("/channels/{tid}"), None)?["name"]) != title {
            return Err("Discord title verification failed".into());
        }
    }
    let cid = task["starter_channel_id"]
        .as_str()
        .filter(|s| !s.is_empty())
        .unwrap_or(tid);
    let mid = s(&task["starter_message_id"]);
    if !mid.is_empty() {
        let route = format!("/channels/{cid}/messages/{mid}");
        let original = ctx.call("GET", &route, None)?;
        let body = card_body(&original, s(&task["title"]), title, s(&task["id"]), names);
        if original["content"] != body["content"] || original["embeds"] != body["embeds"] {
            ctx.call("PATCH", &route, Some(&body))?;
        }
    }
    Ok(())
}

pub fn card_body(message: &Value, old: &str, title: &str, id: &str, names: &Names) -> Value {
    let replace = |s: &str| {
        names.redact(&if old.is_empty() {
            s.to_string()
        } else {
            s.replace(old, title)
        })
    };
    let content = replace(s(&message["content"]));
    let note = format!("-# ジョブ {}", names.redact(id));
    let content = if content.lines().any(|line| line == note) {
        content
    } else {
        // Keep AO's recovery marker last; it is used to recover interrupted creations.
        let mut lines: Vec<_> = content.lines().map(str::to_owned).collect();
        let index = lines
            .iter()
            .position(|line| line.starts_with("-# [godd-task:"))
            .unwrap_or(lines.len());
        lines.insert(index, note);
        lines.join("\n")
    };
    let mut embeds = message["embeds"].as_array().cloned().unwrap_or_default();
    for embed in &mut embeds {
        for field in ["title", "description"] {
            if let Some(value) = embed[field].as_str() {
                embed[field] = json!(replace(value));
            }
        }
    }
    json!({"content":content,"embeds":embeds,"allowed_mentions":{"parse":[]}})
}

/// Rename only registered bot task cards in the task forum, never owner-created threads.
pub fn rename_open(ctx: &mut Context, apply: bool, limit: usize) -> Result<Value> {
    let names = Names::load(&ctx.client_projects).map_err(|_| "client project list unavailable")?;
    let todo = if ctx.todo.is_file() {
        read_json(&ctx.todo)?
    } else {
        Value::Null
    };
    let mut cards = BTreeMap::new();
    for entry in fs::read_dir(&ctx.discord_tasks).map_err(|_| "task records unavailable")? {
        crate::tick_runtime::check()?;
        let path = entry.map_err(|_| "task records unavailable")?.path();
        if path.extension().and_then(|x| x.to_str()) != Some("json") {
            continue;
        }
        let task = read_json(&path)?;
        let id = s(&task["id"]);
        let tid = s(&task["thread_id"]);
        if !valid_job(id) || tid.is_empty() {
            continue;
        }
        cards.insert(tid.to_string(), (path, id.to_string()));
    }
    let active = ctx.call(
        "GET",
        &format!("/guilds/{}/threads/active", s(&ctx.cfg["guild_id"])),
        None,
    )?;
    let mut changes = Vec::new();
    for thread in active["threads"].as_array().into_iter().flatten() {
        let tid = s(&thread["id"]);
        if s(&thread["parent_id"]) != s(&ctx.cfg["task_channel_id"]) {
            continue;
        }
        let Some((path, id)) = cards.get(tid) else {
            continue;
        };
        let pending_restore = path.with_extension("restore");
        let needs_restore =
            pending_restore.is_file() && truth(&read_json(&pending_restore)?["archived"]);
        let directory = ctx.jobs.join("jobs").join(id);
        if !directory.join("job.json").is_file() {
            continue;
        }
        let job = read_json(&directory.join("job.json"))?;
        let prompt = fs::read_to_string(directory.join("prompt.md")).unwrap_or_default();
        let title =
            task_title::polish_title(&task_title::job_title(&job, &prompt, &todo, &names), &names);
        if s(&thread["name"]) == title {
            let task = read_json(path)?;
            let cid = task["starter_channel_id"]
                .as_str()
                .filter(|v| !v.is_empty())
                .unwrap_or(tid);
            let mid = s(&task["starter_message_id"]);
            if !mid.is_empty() {
                let message = ctx.call("GET", &format!("/channels/{cid}/messages/{mid}"), None)?;
                let desired =
                    card_body(&message, s(&task["title"]), &title, s(&task["id"]), &names);
                if message["content"] == desired["content"]
                    && message["embeds"] == desired["embeds"]
                    && task["title"] == title.as_str()
                    && !truth(&task["fallback_title_pending"])
                    && !truth(&task["title_restore_archive"])
                    && !needs_restore
                {
                    continue;
                }
            } else if task["title"] == title.as_str()
                && !truth(&task["fallback_title_pending"])
                && !truth(&task["title_restore_archive"])
                && !needs_restore
            {
                continue;
            }
        }
        if changes.len() >= limit {
            break;
        }
        if apply {
            sync_card(ctx, path, &title, &names)?;
        }
        changes.push(json!({"thread_id":tid,"title":title,"applied":apply}));
    }
    Ok(json!({"changes":changes,"apply":apply}))
}
