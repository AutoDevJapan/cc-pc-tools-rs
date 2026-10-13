//! Resume a capacity failure inside the existing supervisor and queue entry.
use crate::{queue::prepare_resume, *};

const MODELS: [&str; 4] = [
    "anthropic/claude-sonnet-5-5",
    "gpt-6.1-sol",
    "gpt-6-astra",
    "gpt-6-luna",
];
const MAX_SWITCHES: u64 = 3;
const MAX_WAITS: u64 = 3;

pub fn provider(model: &str) -> Option<&'static str> {
    if model.starts_with("anthropic/claude-") || model.starts_with("claude-") {
        Some("anthropic")
    } else if MODELS.contains(&model) {
        Some("openai")
    } else {
        None
    }
}

/// Called only for protocol error events, never tool results or assistant text.
pub fn failure_kind(event: &Value) -> Option<&'static str> {
    let message = match s(event, "type") {
        "error" => s(event, "message").to_owned(),
        "turn.failed" => err_text(&event["error"]),
        _ => return None,
    };
    let code = if s(&event["error"], "code").is_empty() {
        s(event, "code")
    } else {
        s(&event["error"], "code")
    }
    .to_ascii_lowercase();
    let text = message.to_ascii_lowercase();
    Some(
        if code.contains("cancel") || text.contains("request cancelled") {
            "cancel"
        } else if code.contains("auth")
            || text.contains("unauthorized")
            || text.contains("authentication failed")
        {
            "auth"
        } else if is_capacity(&message) {
            "capacity"
        } else if text.contains("quota")
            || text.contains("usage limit")
            || text.contains("429 too many requests")
        {
            "quota"
        } else if code == "request_failed" {
            "request_failed"
        } else {
            "transient"
        },
    )
}

pub fn review_job(job: &Value) -> bool {
    let task = s(job, "task_type");
    task.contains("review")
        || s(job, "name").ends_with("-review")
        || s(job, "name").starts_with("rev-")
}

/// Preserve explicit lightweight and independent review constraints.
pub fn candidates(job: &Value) -> Vec<&'static str> {
    let requested = if s(job, "requested_model").is_empty() {
        s(job, "model")
    } else {
        s(job, "requested_model")
    };
    let lightweight = requested.contains("haiku");
    let current = s(job, "model");
    let mut options: Vec<_> = MODELS
        .into_iter()
        .filter(|model| {
            *model != current
                && (!lightweight
                    || ((review_job(job) || s(job, "task_type").starts_with("single_file_"))
                        && s(job, "sandbox") == "read-only"
                        && *model == "gpt-6-luna"))
                && (!review_job(job)
                    || ["anthropic/claude-sonnet-5-5", "gpt-6-luna"].contains(model))
                && (!review_job(job) || provider(model) != provider(requested))
                && !job["review_excluded_models"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .any(|v| v.as_str() == Some(model))
                && !job["capacity_history"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .any(|entry| s(entry, "from_model") == *model || s(entry, "to_model") == *model)
        })
        .collect();
    options.sort_by_key(|model| provider(model) == provider(current));
    options
}

pub fn is_capacity(message: &str) -> bool {
    message
        .to_ascii_lowercase()
        .contains("selected model is at capacity")
}

impl Context {
    /// Query before taking queue/state locks. Persist only safe provider metadata.
    pub fn capacity_inventory(&self) -> Value {
        let at = self.rt.epoch();
        let mut inventory = json!({"at":at});
        for provider in ["openai", "anthropic"] {
            let q = self.rt.queue_quota_sample(provider, at, 15);
            inventory[provider] = json!(
                q.accounts
                    .iter()
                    .any(|a| a.reset_at > at && a.used_percent < 100. && !a.short_exhausted)
            );
        }
        inventory
    }

    /// A due cooldown rechecks availability; it never starts a depleted model.
    pub fn recheck_capacity_wait(&self, job: &mut Value) -> bool {
        if s(job, "status") != "queued"
            || job["capacity_wait_pending"] != true
            || job["cancel_requested"] == true
        {
            return false;
        }
        if job["retry_not_before"]
            .as_f64()
            .is_some_and(|at| at > self.rt.epoch())
        {
            return false;
        }
        job["status"] = json!("failed");
        job["last_error"] = job["capacity_wait_error"].clone();
        job["last_failure_kind"] = json!("capacity");
        self.retry_capacity(job)
    }
    /// The caller has drained the exited child and holds the job's state lock.
    /// Preserve permissions, prompt, working directory and ToDo ownership.
    pub fn retry_capacity(&self, job: &mut Value) -> bool {
        if s(job, "status") != "failed"
            || !is_capacity(s(job, "last_error"))
            || s(job, "last_failure_kind") != "capacity"
            || job["cancel_requested"] == true
        {
            return false;
        }
        let stop = |reason: &str| {
            self.rt
                .diagnostic(&format!("モデルの混雑による自動再開を終了: {reason}"));
            false
        };
        let count = job["capacity_retry_count"].as_u64().unwrap_or(0);
        if count >= MAX_SWITCHES {
            job["capacity_exhausted"] = json!(true);
            return stop("切り替えが最大3回に達しました");
        }
        if provider(s(job, "model")).is_none() {
            return stop("指定モデルは切り替えの対象外です");
        }
        let session = ["thread_id", "resume_session"]
            .iter()
            .find_map(|key| normalize_uuid(s(job, key)));
        if session.is_none()
            && ["thread_id", "resume_session"]
                .iter()
                .any(|key| !s(job, key).is_empty())
        {
            return stop("有効なセッションIDがありません");
        }
        let session = session.unwrap_or_default();
        if !job["capacity_history"].is_null() && !job["capacity_history"].is_array() {
            return stop("保存された切り替え履歴の形式が不正です");
        }
        // The worker has exited. Do not discard a still-live identity, including
        // an AccessDenied inspection, and accidentally start a second worker.
        if let Some(pid) = job["codex_pid"]
            .as_u64()
            .and_then(|p| u32::try_from(p).ok())
        {
            match self.rt.proc_created_checked(pid) {
                Ok(None) => (),
                Ok(Some(at))
                    if job["codex_pid_created"]
                        .as_u64()
                        .is_some_and(|old| old != at) =>
                {
                    ()
                }
                _ => return stop("元の子処理の終了を確認できません"),
            }
        }
        let at = self.rt.epoch();
        // A review must remain different from the model that wrote the source.
        // Read only its persisted model metadata, never copy the parent's prompt.
        let mut selection = job.clone();
        if review_job(job) {
            if s(job, "parent_job_id").is_empty() && !job["review_excluded_models"].is_array() {
                job["capacity_exhausted"] = json!(true);
                return stop("別モデルレビューの実装担当を確認できません");
            }
        }
        if review_job(job) && !s(job, "parent_job_id").is_empty() {
            let parent = match self.load(s(job, "parent_job_id")) {
                Ok(parent) => parent,
                Err(_) => {
                    job["capacity_exhausted"] = json!(true);
                    return stop("実装担当のモデル情報を確認できません");
                }
            };
            let mut excluded = selection["review_excluded_models"]
                .as_array()
                .cloned()
                .unwrap_or_default();
            for field in ["model", "requested_model"] {
                if !s(&parent, field).is_empty() {
                    excluded.push(parent[field].clone());
                }
            }
            for entry in parent["capacity_history"].as_array().into_iter().flatten() {
                for field in ["from_model", "to_model"] {
                    if entry[field].is_string() {
                        excluded.push(entry[field].clone());
                    }
                }
            }
            selection["review_excluded_models"] = json!(excluded);
        }
        let mut snapshots = std::collections::HashMap::new();
        let next = if count < MAX_SWITCHES {
            candidates(&selection).into_iter().find(|model| {
                let provider = provider(model).unwrap();
                if job["capacity_inventory"]["at"]
                    .as_f64()
                    .is_some_and(|checked| at >= checked && at - checked <= 60.)
                {
                    return job["capacity_inventory"][provider] == true;
                }
                let q = snapshots
                    .entry(provider)
                    .or_insert_with(|| self.rt.queue_quota_sample(provider, at, 15));
                // One verified eligible OAuth account suffices even when a
                // different account's quota is unavailable. Never guess a quota.
                q.accounts
                    .iter()
                    .any(|a| a.reset_at > at && a.used_percent < 100. && !a.short_exhausted)
            })
        } else {
            None
        };
        let Some(next) = next else {
            let waits = job["capacity_wait_count"].as_u64().unwrap_or(0);
            if waits >= MAX_WAITS || candidates(&selection).is_empty() {
                job["capacity_exhausted"] = json!(true);
                return stop("有限の候補と待機回数を使い切りました");
            }
            job["capacity_wait_error"] = job["last_error"].clone();
            prepare_resume(job, &session, &self.rt.now());
            job["capacity_wait_pending"] = json!(true);
            job["capacity_wait_count"] = json!(waits + 1);
            job["retry_not_before"] = json!(at + 900.);
            job["activity"] = json!(format!(
                "モデルの混雑: 15分後に既存OAuth枠を再確認（{}回目、最大3回）",
                waits + 1
            ));
            return true;
        };
        let now = self.rt.now();
        let entry = json!({
            "attempt": count + 1, "at": now, "session": session,
            "from_model": job["model"], "to_model": next,
            "from_provider":provider(s(job,"model")), "to_provider":provider(next),
            "error": short(&redact_local_log_secrets(s(job, "last_error"), &self.rt.env()), 1000)
        });
        if job["capacity_history"].is_null() {
            job["capacity_history"] = json!([]);
        }
        job["capacity_history"].as_array_mut().unwrap().push(entry);
        job["capacity_retry_count"] = json!(count + 1);
        if selection["review_excluded_models"].is_array() {
            job["review_excluded_models"] = selection["review_excluded_models"].clone();
        }
        if s(job, "requested_model").is_empty() {
            job["requested_model"] = job["model"].clone();
        }
        job["model"] = json!(next);
        prepare_resume(job, &session, &now);
        job["last_failure_kind"] = Value::Null;
        job["capacity_error"] = Value::Null;
        job["capacity_wait_pending"] = json!(false);
        job["capacity_wait_error"] = Value::Null;
        job["resource_wait"] = Value::Null;
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!(format!(
            "順番待ち: モデルの混雑により{next}で再開（{}回目、最大3回）",
            count + 1
        ));
        self.rt.diagnostic(s(job, "activity"));
        true
    }
}
