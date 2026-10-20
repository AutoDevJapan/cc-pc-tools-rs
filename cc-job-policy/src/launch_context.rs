//! Keep the native conversation's configuration aligned with its next launch.
use serde_json::{Value, json};
pub const VERSION: &str = "max-resume-v1";

pub fn capacity_attempt(job: &Value) -> bool {
    job["capacity_wait_pending"] == true
        || job["last_failure_kind"] == "capacity"
        || job["capacity_same_model_retry_count"].as_u64().unwrap_or(0) > 0
        || job["capacity_retry_count"].as_u64().unwrap_or(0) > 0
        || (!job["capacity_history"].is_null()
            && job["capacity_history"]
                .as_array()
                .is_none_or(|history| !history.is_empty()))
}

/// These existing gates bind an exact source/model/effort permission tuple.
/// Only their own normal requalification may replace that tuple.
pub fn fixed_execution_contract(job: &Value) -> bool {
    [
        "original_product_retry",
        "fixed_read_only_review",
        "fixed_review_requalification",
    ]
    .iter()
    .any(|field| job[*field].is_object())
}

pub fn usable_resume_session(job: &Value) -> Option<&str> {
    job["resume_session"]
        .as_str()
        .filter(|session| !session.is_empty() && job["policy_resume_reset"]["session"] != *session)
}

pub(crate) fn source_settings(source: &Value) -> Value {
    let mut settings = json!({"model":source["model"],"effort":source["effort"]});
    // Older executables already saved the normalized settings but left the
    // pre-policy conversation attached. Retire that context once; an existing
    // reset receipt identifies the later, compatible native conversation.
    if source["launch_policy_version"] != VERSION
        && !source["policy_resume_reset"].is_object()
        && !capacity_attempt(source)
        && !fixed_execution_contract(source)
    {
        for field in ["model", "effort"] {
            let origin = format!("{field}_origin");
            let hint = format!("legacy_{field}_hint");
            if source[&origin] != "owner_request"
                && source[&hint]
                    .as_str()
                    .is_some_and(|value| !value.is_empty())
            {
                settings[field] = source[&hint].clone();
            }
        }
    }
    settings
}

/// `source` is the original conversation's execution record, not an already
/// normalized cached selection. No permissions, work or retry budget changes.
pub fn reconcile_resume(job: &mut Value, source: &Value) {
    let Some(session) = job["resume_session"]
        .as_str()
        .filter(|session| !session.is_empty())
        .map(str::to_owned)
    else {
        return;
    };
    // A requeue/reboot can reconstruct the old ID from thread_id after the
    // first normalization. Remember that ID without discarding its history.
    if job["policy_resume_reset"]["session"] == session {
        job["resume_session"] = Value::Null;
        return;
    }
    if source["policy_resume_reset"]["session"] == session {
        job["policy_resume_reset"] = source["policy_resume_reset"].clone();
        job["resume_session"] = Value::Null;
        return;
    }
    if capacity_attempt(job) || fixed_execution_contract(job) {
        return;
    }
    let source = source_settings(source);
    let changed = ["model", "effort"].iter().any(|field| {
        let Some(before) = source[*field].as_str().filter(|value| !value.is_empty()) else {
            return false;
        };
        let Some(after) = job[*field].as_str().filter(|value| !value.is_empty()) else {
            return false;
        };
        if *field == "model" {
            !crate::same_model(before, after)
        } else {
            before != after
        }
    });
    if changed {
        job["policy_resume_reset"] = json!({"session":session,"from_model":source["model"],
            "from_effort":source["effort"],"to_model":job["model"],"to_effort":job["effort"]});
        job["resume_session"] = Value::Null;
    }
}
