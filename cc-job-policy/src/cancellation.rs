//! Cancellation intent survives removal of the launch reservation.
use serde_json::Value;

fn text(value: &Value) -> &str {
    value.as_str().unwrap_or("").trim()
}

pub fn cancelled(job: &Value) -> bool {
    matches!(text(&job["status"]), "cancelled" | "canceled")
}

/// Explicit stops always win, including when an older automatic intent exists.
pub fn owner_stopped(item: &Value, job: &Value) -> bool {
    item["cancel_requested"] == true
        || item["stopped"] == true
        || job["cancel_requested"] == true
        || job["stopped"] == true
        || matches!(text(&job["cancel_reason"]), "manual" | "person")
}

/// Job-side intent is written under the job lock before automatic termination.
/// Legacy item-side intent is accepted only when bound to this exact job.
pub fn recovery_cancel(item: &Value, job: &Value) -> bool {
    let id = text(&job["id"]);
    if id.is_empty() || owner_stopped(item, job) {
        return false;
    }
    let durable = !text(&job["recovery_cancel"]["key"]).is_empty()
        && chrono::DateTime::parse_from_rfc3339(text(&job["recovery_cancel"]["committed_at"]))
            .is_ok();
    let watchdog = text(&item["stall_recovery"]["pending"]["recovery_cancel"]) == id;
    let scheduler = text(&item["scheduler"]["job"]) == id
        && matches!(
            text(&item["scheduler"]["pending_cancel"]),
            "hung" | "lost" | "memory" | "resource_wait"
        );
    durable || watchdog || scheduler
}

/// Unknown cancellations are holds, not permission to launch another worker.
pub fn blocks_recovery(item: &Value, job: &Value) -> bool {
    owner_stopped(item, job) || (cancelled(job) && !recovery_cancel(item, job))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn durable_intent_survives_reservation_removal_but_explicit_stops_win() {
        let job = json!({"id":"source","status":"cancelled",
            "recovery_cancel":{"key":"original-key","committed_at":"2026-10-11T02:01:36+09:00"}});
        assert!(!blocks_recovery(&json!({}), &job));
        for field in ["cancel_requested", "stopped"] {
            assert!(blocks_recovery(&json!({field:true}), &job));
            let mut explicit = job.clone();
            explicit[field] = json!(true);
            assert!(blocks_recovery(&json!({}), &explicit));
        }
        for reason in ["manual", "person"] {
            let mut explicit = job.clone();
            explicit["cancel_reason"] = json!(reason);
            assert!(blocks_recovery(&json!({}), &explicit));
        }
    }

    #[test]
    fn unknown_or_incomplete_intent_and_unrelated_legacy_holds_are_not_automatic() {
        for status in ["cancelled", "canceled"] {
            for intent in [
                json!(null),
                json!({}),
                json!({"key":"key"}),
                json!({"key":" ","committed_at":"time"}),
                json!({"key":"key","committed_at":null}),
                json!({"key":"key","committed_at":"invalid"}),
                json!({"key":"key","committed_at":"2026-13-40T00:00:00+09:00"}),
            ] {
                let job = json!({"id":"source","status":status,"recovery_cancel":intent});
                assert!(blocks_recovery(&json!({}), &job));
                assert!(blocks_recovery(
                    &json!({"scheduler":{"job":"other","pending_cancel":"hung"}}),
                    &job
                ));
                assert!(blocks_recovery(
                    &json!({"stall_recovery":{"pending":{"recovery_cancel":"other"}}}),
                    &job
                ));
            }
            let job = json!({"id":"source","status":status});
            assert!(!blocks_recovery(
                &json!({"scheduler":{"job":"source","pending_cancel":"memory"}}),
                &job
            ));
            assert!(!blocks_recovery(
                &json!({"stall_recovery":{"pending":{"recovery_cancel":"source"}}}),
                &job
            ));
            assert!(blocks_recovery(
                &json!({"scheduler":{"job":"source","pending_cancel":"unknown"}}),
                &job
            ));
        }
    }
}
