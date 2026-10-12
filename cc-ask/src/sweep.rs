//! Read-only ownership checks for Discord cleanup. A Codex conversation id is not a
//! Discord thread id; the notifier's persisted task record supplies that mapping.
use crate::core::*;
use serde::Deserialize;
use serde_json::Value;
use std::{collections::HashSet, fs, io::ErrorKind, path::PathBuf};

#[derive(Deserialize)]
struct Task {
    id: String,
    thread_id: String,
    status: String,
}

fn tasks(ctx: &Context) -> Result<Vec<(PathBuf, Task)>> {
    crate::tick_runtime::check()?;
    let _span = crate::tick_runtime::Span::new("notifier task inventory");
    crate::tick_runtime::check()?;
    let entries = fs::read_dir(&ctx.discord_tasks)
        .map_err(|_| "Discord task state unavailable; cleanup skipped")?;
    let mut tasks = vec![];
    for entry in entries {
        crate::tick_runtime::check()?;
        let path = entry
            .map_err(|_| "Discord task state unavailable; cleanup skipped")?
            .path();
        if path.extension().and_then(|x| x.to_str()) == Some("json") {
            tasks.push((path.clone(), read_task(&path)?));
        }
    }
    Ok(tasks)
}

fn read_task(path: &std::path::Path) -> Result<Task> {
    let text =
        fs::read_to_string(path).map_err(|_| "Discord task state unavailable; cleanup skipped")?;
    let task: Task =
        serde_json::from_str(&text).map_err(|_| "Invalid Discord task state; cleanup skipped")?;
    if !["in_progress", "waiting_for_answer", "completed", "failed"].contains(&task.status.as_str())
    {
        return Err("Invalid Discord task status; cleanup skipped".into());
    }
    Ok(task)
}

fn task_active(ctx: &Context, task: &Task) -> Result<bool> {
    let active = job_active(ctx, &task.id)?;
    Ok(active == Some(true)
        || task.status == "waiting_for_answer"
        || (active.is_none() && !["completed", "failed"].contains(&task.status.as_str())))
}

/// None means no job record (also normal for tasks started outside codex-jobs).
/// Unknown job statuses remain protected; only known terminal states release ownership.
fn job_active(ctx: &Context, id: &str) -> Result<Option<bool>> {
    if !valid_job(id) {
        return Ok(None);
    }
    let path = ctx.jobs.join("jobs").join(id).join("job.json");
    let text = match fs::read_to_string(path) {
        Ok(text) => text,
        Err(err) if err.kind() == ErrorKind::NotFound => return Ok(None),
        Err(_) => return Err("Job state unavailable; cleanup skipped".into()),
    };
    let job: Value =
        serde_json::from_str(&text).map_err(|_| "Invalid job state; cleanup skipped")?;
    let status = job["status"]
        .as_str()
        .ok_or("Missing job status; cleanup skipped")?;
    Ok(Some(
        !["succeeded", "failed", "cancelled", "lost"].contains(&status),
    ))
}

pub fn protected_threads(ctx: &Context, state: &Value) -> Result<HashSet<String>> {
    let _span = crate::tick_runtime::Span::new("cleanup ownership inventory");
    let mut protected = HashSet::new();
    for (_, task) in tasks(ctx)? {
        crate::tick_runtime::check()?;
        if task.thread_id.is_empty() {
            continue;
        }
        if task_active(ctx, &task)? {
            protected.insert(task.thread_id);
        }
    }
    // Request threads have their own mapping and can outlive the notifier's task creation.
    let requests = match state.get("requests") {
        None | Some(Value::Null) => return Ok(protected),
        Some(Value::Object(requests)) => requests,
        _ => return Err("Invalid request mapping; cleanup skipped".into()),
    };
    for (thread, request) in requests {
        crate::tick_runtime::check()?;
        if !request.is_object() {
            return Err("Invalid request mapping; cleanup skipped".into());
        }
        let id = match request.get("job") {
            None | Some(Value::Null) => continue,
            Some(Value::String(id)) if id.is_empty() || id == "launch-failed" => continue,
            Some(Value::String(id)) if valid_job(id) => id,
            _ => return Err("Invalid request job; cleanup skipped".into()),
        };
        // A missing referenced job is uncertain ownership, not proof of completion.
        if job_active(ctx, id)? != Some(false) {
            protected.insert(thread.clone());
        }
    }
    Ok(protected)
}

/// Serialize a candidate with GoDD-AO's task updates, then recheck ownership.
/// File::try_lock uses the same whole-file exclusive lock as the notifier's File::lock.
/// A busy candidate stays open. Keep returned handles alive until PATCH has completed.
pub fn archive_guard(ctx: &Context, thread: &str) -> Result<Option<Vec<fs::File>>> {
    let mut guards = vec![];
    for (path, task) in tasks(ctx)? {
        crate::tick_runtime::check()?;
        if task.thread_id != thread {
            continue;
        }
        let file = fs::OpenOptions::new()
            .create(true)
            .write(true)
            .truncate(false)
            .open(path.with_extension("lock"))
            .map_err(|_| "Discord task lock unavailable; cleanup skipped")?;
        if file.try_lock().is_err() {
            return Ok(None);
        }
        let fresh = read_task(&path)?;
        if fresh.id != task.id || fresh.thread_id != thread {
            return Err("Discord task mapping changed; cleanup skipped".into());
        }
        if task_active(ctx, &fresh)? {
            return Ok(None);
        }
        guards.push(file);
    }
    Ok(Some(guards))
}
