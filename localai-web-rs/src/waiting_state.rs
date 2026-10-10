//! Do not infer a CPU wait merely from an unstarted worker.
use serde_json::Value;
pub fn kind(
    item: &Value,
    current: Option<&Value>,
    queue: &Value,
    capacity_blocked: bool,
) -> &'static str {
    if current.is_some_and(|job| job["status"] == "queued") {
        return "job_queue";
    }
    let preparation_failed = queue["queue"].as_array().is_some_and(|triggers| {
        triggers.iter().any(|t| {
            t["todo_id"].is_u64()
                && t["todo_id"] == item["id"]
                && t["key"]
                    .as_str()
                    .is_some_and(|key| queue["preparation_failures"][key].is_array())
        })
    });
    if preparation_failed {
        "startup_error"
    } else if capacity_blocked {
        "resource"
    } else {
        "start"
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn separates_unstarted_work_job_queue_and_current_preparation_failure() {
        let item = json!({"id":12});
        let queue = json!({"queue":[{"todo_id":12,"key":"current"}],"preparation_failures":{"old":[1,"tool_failed"]}});
        assert_eq!(kind(&item, None, &queue, false), "start");
        assert_eq!(kind(&item, None, &queue, true), "resource");
        assert_eq!(
            kind(&item, Some(&json!({"status":"queued"})), &queue, false),
            "job_queue"
        );
        let mut queue = queue;
        queue["preparation_failures"]["current"] = json!([1, "tool_failed"]);
        assert_eq!(kind(&item, None, &queue, true), "startup_error");
    }
}
