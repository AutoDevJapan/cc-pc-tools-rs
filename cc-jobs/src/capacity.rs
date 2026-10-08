//! Resume a capacity failure inside the existing supervisor and queue entry.
use crate::{queue::prepare_resume, *};

const MODELS: [&str; 3] = ["gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna"];
const MAX_SWITCHES: u64 = 3;

pub fn is_capacity(message: &str) -> bool {
    message
        .to_ascii_lowercase()
        .contains("selected model is at capacity")
}

impl Context {
    /// The caller has drained the exited child and holds the job's state lock.
    /// Preserve permissions, prompt, working directory and ToDo ownership.
    pub fn retry_capacity(&self, job: &mut Value) -> bool {
        if s(job, "status") != "failed"
            || !is_capacity(s(job, "last_error"))
            || job["cancel_requested"] == true
        {
            return false;
        }
        let count = job["capacity_retry_count"].as_u64().unwrap_or(0);
        if count >= MAX_SWITCHES {
            return false;
        }
        let Some(index) = MODELS.iter().position(|model| *model == s(job, "model")) else {
            return false;
        };
        let Some(session) = ["thread_id", "resume_session"]
            .iter()
            .find_map(|key| normalize_uuid(s(job, key)))
        else {
            return false;
        };
        if !job["capacity_history"].is_null() && !job["capacity_history"].is_array() {
            return false;
        }
        let next = MODELS[(index + 1) % MODELS.len()];
        let now = self.rt.now();
        let entry = json!({
            "attempt": count + 1, "at": now, "session": session,
            "from_model": job["model"], "to_model": next,
            "error": redact_local_log_secrets(s(job, "last_error"), &self.rt.env())
        });
        if job["capacity_history"].is_null() {
            job["capacity_history"] = json!([]);
        }
        job["capacity_history"].as_array_mut().unwrap().push(entry);
        job["capacity_retry_count"] = json!(count + 1);
        job["model"] = json!(next);
        prepare_resume(job, &session, &now);
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!(format!(
            "順番待ち: モデルの混雑により{next}で再開（{}回目、最大3回）",
            count + 1
        ));
        true
    }
}
