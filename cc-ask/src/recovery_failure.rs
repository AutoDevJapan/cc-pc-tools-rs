//! Launcher failures and subscription-only remedies shared by the two watchdogs.
use serde_json::Value;
use std::{
    io::{Read, Seek, SeekFrom},
    path::Path,
};

pub const FAST_RETRY_S: f64 = 60.;
pub const SLOW_RETRY_S: f64 = 3600.;
pub const CONSOLE_TASK: &str = "local-ai-web-autostart-10012";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Failure {
    Console,
    Proxy,
    Model,
    Context,
    Writer,
    Task,
}
impl Failure {
    pub fn name(self) -> &'static str {
        match self {
            Self::Console => "console",
            Self::Proxy => "proxy",
            Self::Model => "model",
            Self::Context => "context",
            Self::Writer => "writer",
            Self::Task => "task",
        }
    }
    pub fn remedy(self) -> &'static str {
        match self {
            Self::Console | Self::Proxy => "定期タスクからコンソールを再起動",
            Self::Model => "別のサブスク対象モデルへ切り替え、新しいセッションで再開",
            Self::Context => "文脈を縮めて、新しいセッションで再開",
            Self::Writer => "稼働中の担当を確認し、衝突したセッションを使わず再開",
            Self::Task => "前回の結果と原因を担当へ渡して再開",
        }
    }
}
pub fn classify(text: &str) -> Failure {
    let t = text.to_lowercase();
    if [
        "context window",
        "context length",
        "context_length_exceeded",
        "too many tokens",
        "maximum context",
    ]
    .iter()
    .any(|s| t.contains(s))
    {
        Failure::Context
    } else if t.contains("active writer")
        || t.contains("thread-store conflict")
        || (t.contains("resume session ") && t.contains("already has active job"))
    {
        Failure::Writer
    } else if t.contains("model")
        && [
            "unavailable",
            "not available",
            "not found",
            "model_not_found",
            "unsupported",
            "does not exist",
            "not supported",
        ]
        .iter()
        .any(|s| t.contains(s))
    {
        Failure::Model
    } else if [
        "failed to initialize in-process app-server",
        "app-server client",
        "access is denied",
        "os error 5",
        "アクセスが拒否",
    ]
    .iter()
    .any(|s| t.contains(s))
    {
        Failure::Console
    } else if [
        "proxy 502",
        "502 bad gateway",
        "status code 502",
        "http 502",
        "http status 502",
        "unexpected status 502",
    ]
    .iter()
    .any(|s| t.contains(s))
    {
        Failure::Proxy
    } else {
        Failure::Task
    }
}
/// Read a bounded tail rather than the whole log. Last_error often only contains an exit code.
pub fn evidence(job: &Value, dir: &Path) -> String {
    let mut text = job["last_error"].as_str().unwrap_or("").to_owned();
    if let Ok(mut f) = std::fs::File::open(dir.join("stderr.log")) {
        if let Ok(len) = f.metadata().map(|m| m.len()) {
            let _ = f.seek(SeekFrom::Start(len.saturating_sub(16384)));
            let mut bytes = Vec::new();
            if f.take(16384).read_to_end(&mut bytes).is_ok() {
                text.push('\n');
                text.push_str(&String::from_utf8_lossy(&bytes));
            }
        }
    }
    if text.trim().is_empty() {
        text = "終了後に最終報告が残らなかった".into();
    }
    text
}
pub fn summary(text: &str) -> String {
    let line = text
        .lines()
        .find(|line| classify(line) != Failure::Task)
        .unwrap_or(text);
    crate::core::safe_text(line)
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .chars()
        .take(400)
        .collect()
}
pub fn retry_at(history: &[Value], now: f64) -> f64 {
    let last = history
        .iter()
        .filter_map(Value::as_f64)
        .max_by(f64::total_cmp)
        .unwrap_or(0.);
    let recent = history
        .iter()
        .filter_map(Value::as_f64)
        .filter(|at| now - at < 21600.)
        .count();
    if recent == 0 {
        0.
    } else {
        last + if recent >= 2 {
            SLOW_RETRY_S
        } else {
            FAST_RETRY_S
        }
    }
}
pub fn alternate_model(model: &str) -> &str {
    match model {
        "gpt-6.1-sol" => "gpt-6-astra",
        "gpt-6-astra" | "gpt-6-luna" => "gpt-6.1-sol",
        _ => model,
    }
}
pub fn repaired_job(job: &Value, failure: Failure) -> Value {
    let mut job = job.clone();
    if failure == Failure::Model {
        let model = job["model"].as_str().unwrap_or("");
        let alternate = alternate_model(model);
        if alternate == model {
            return job;
        }
        job["model"] = alternate.into();
        if job["effort"].as_str().is_none_or(str::is_empty) {
            job["effort"] = "max".into();
        }
    }
    if matches!(failure, Failure::Context | Failure::Model | Failure::Writer) {
        job["thread_id"] = Value::Null;
        job["resume_session"] = Value::Null;
    }
    job
}
pub fn reserve_console(root: &Path, now: f64) -> crate::core::Result<bool> {
    use crate::core::{self, ByteLock};
    let Ok(_lock) = ByteLock::acquire(
        &root.join("console-recovery.lock"),
        std::time::Duration::ZERO,
    ) else {
        return Ok(false);
    };
    let path = root.join("console-recovery.json");
    let mut state = if path.exists() {
        core::read_json(&path)?
    } else {
        serde_json::json!({})
    };
    if state["next_at"].as_f64().unwrap_or(0.) > now {
        return Ok(false);
    }
    state["next_at"] = (now + SLOW_RETRY_S).into();
    core::atomic_json(&path, &state, 2)?;
    Ok(true)
}
