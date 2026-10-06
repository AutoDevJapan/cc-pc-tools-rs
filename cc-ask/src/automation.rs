//! Opt-in cutover and common guards for the existing minute tick.
use crate::{
    core::*,
    resume::{in_project, path_key},
};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::path::Path;

pub fn key(text: &str) -> String {
    format!("{:x}", Sha256::digest(text.as_bytes()))[..32].into()
}
pub fn recover_before_launch(ctx: &mut Context) -> bool {
    ctx.effects
        .run(
            &command_argv(&ctx.scheduler_command, &["recover".into()]),
            45,
        )
        .is_ok_and(|out| out.code == 0)
}
pub fn epoch(value: &Value) -> f64 {
    value.as_f64().unwrap_or_else(|| {
        chrono::DateTime::parse_from_rfc3339(s(value))
            .map(|d| d.timestamp_millis() as f64 / 1000.)
            .unwrap_or(0.)
    })
}
pub fn read_or(path: &Path, default: Value) -> Result<Value> {
    if path.exists() {
        read_json(path)
    } else {
        Ok(default)
    }
}
pub fn jobs(ctx: &Context) -> Result<Vec<Value>> {
    let root = ctx.jobs.join("jobs");
    if !root.exists() {
        return Ok(vec![]);
    }
    let mut out = vec![];
    for dir in std::fs::read_dir(root).map_err(|_| "job inventory unavailable")? {
        crate::tick_runtime::check()?;
        let path = dir
            .map_err(|_| "job inventory unavailable")?
            .path()
            .join("job.json");
        if path.exists() {
            let mut job = read_json(&path)?;
            job["id"] = json!(path
                .parent()
                .unwrap()
                .file_name()
                .unwrap()
                .to_string_lossy());
            out.push(job);
        }
    }
    Ok(out)
}
pub fn cooldown(jobs: &[Value], now: f64) -> bool {
    jobs.iter()
        .any(|j| now - epoch(&j["created_at"]).max(epoch(&j["started_at"])) < 45.)
}

/// Link existing unfinished work to its successor. CLI takes the shared ToDo lock.
pub fn link_todos(ctx: &mut Context, originals: &[String], jid: &str) -> Result<()> {
    let db = read_or(&ctx.todo, json!({"items":[]}))?;
    for item in db["items"].as_array().into_iter().flatten() {
        if s(&item["status"]) != "doing" || n(&item["id"]) == 57 {
            continue;
        }
        let ids = item["jobs"].as_array().cloned().unwrap_or_default();
        if ids.iter().any(|id| s(id) == jid)
            || !ids.iter().any(|id| originals.iter().any(|o| o == s(id)))
        {
            continue;
        }
        let args = vec![
            "start".into(),
            n(&item["id"]).to_string(),
            "--job".into(),
            jid.into(),
        ];
        if ctx
            .effects
            .run(&command_argv(&ctx.todo_command, &args), 45)?
            .code
            != 0
        {
            return Err("ToDo successor link failed".into());
        }
    }
    Ok(())
}
/// Resolve paths before inspecting the target. An unreadable deny list blocks effects.
pub fn protected(ctx: &Context, cwd: &str) -> Result<bool> {
    let text = read_text(&ctx.client_projects)?;
    let lexical = path_key(cwd).replace('\\', "/").to_lowercase();
    if lexical.contains("openmausbot") {
        return Ok(true);
    }
    let canonical = Path::new(cwd)
        .canonicalize()
        .map_err(|_| "working directory unavailable")?;
    let resolved = path_key(&canonical.to_string_lossy())
        .replace('\\', "/")
        .to_lowercase();
    let todo = read_or(&ctx.todo, json!({"projects":{},"items":[]}))?;
    for name in text
        .trim_start_matches('\u{feff}')
        .lines()
        .map(str::trim)
        .filter(|x| !x.is_empty() && !x.starts_with('#'))
    {
        let needle = name.replace('\\', "/").to_lowercase();
        if lexical.contains(&needle) || resolved.contains(&needle) {
            return Ok(true);
        }
        if todo["projects"].as_object().is_some_and(|ps| {
            ps.iter().any(|(n, p)| {
                n.eq_ignore_ascii_case(name)
                    && (in_project(cwd, s(p)) || in_project(&canonical.to_string_lossy(), s(p)))
            })
        }) {
            return Ok(true);
        }
    }
    Ok(false)
}
pub fn busy(cwd: &str, jobs: &[Value], processes: &[String]) -> bool {
    let inside = |a: &str, b: &str| in_project(a, b) || in_project(b, a);
    processes.iter().any(|p| p.is_empty() || inside(p, cwd))
        || jobs
            .iter()
            .any(|j| RUNNING.contains(&s(&j["status"])) && inside(s(&j["cwd"]), cwd))
}
pub fn enabled(ctx: &Context) -> Result<Option<f64>> {
    let control = read_or(&ctx.here.join("automation.json"), json!({}))?;
    if !truth(&control["enabled"]) {
        return Ok(None);
    }
    let since = epoch(&control["since"]);
    if since <= 0. {
        return Err("automation start time missing".into());
    }
    Ok(Some(since))
}
/// The ToDo watchdog submits stopped work to the console's durable queue.
/// Never restore the competing session scanner or replay its historical inbox.
pub fn reject_legacy_launch() -> Result<()> {
    Err("旧巡回と重複した返信処理は終了しました。作業の再開は共有ToDoの停滞監視からコンソールへ委譲します。".into())
}

pub fn start(_ctx: &Context) -> Result<()> {
    reject_legacy_launch()
}
