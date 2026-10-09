//! Item-level, read-only detection for the silent Discord board.
use crate::core::s;
use serde_json::Value;

fn arr(v: &Value) -> Vec<Value> {
    v.as_array().cloned().unwrap_or_default()
}
fn epoch(v: &Value) -> f64 {
    v.as_f64().unwrap_or_else(|| {
        chrono::DateTime::parse_from_rfc3339(s(v))
            .map(|at| at.timestamp() as f64)
            .unwrap_or(0.)
    })
}

pub const AGE_S: f64 = 1800.;

pub fn unspecified(item: &Value) -> bool {
    item["status"] == "todo" && s(&item["spec"]).trim().is_empty()
}

pub fn linked(item: &Value, job: &Value) -> bool {
    (item["id"].is_number() && arr(&job["related_todo_ids"]).contains(&item["id"]))
        || (item["id"].is_number()
            && item["id"] == job["todo_id"]
            && (s(&job["project"]).is_empty() || item["project"] == job["project"]))
        || (!s(&job["id"]).is_empty()
            && (arr(&item["jobs"]).contains(&job["id"]) || item["scheduler"]["job"] == job["id"]))
        || (!s(&job["thread_id"]).is_empty() && arr(&item["jobs"]).contains(&job["thread_id"]))
        || (!s(&job["launch_key"]).is_empty()
            && (item["scheduler"]["key"] == job["launch_key"]
                || item["stall_recovery"]["pending"]["key"] == job["launch_key"]))
}

/// A queue receipt is ownership even before there is a process or job ID.
pub fn detected(item: &Value, jobs: &[Value], intake: &Value, now: f64) -> bool {
    if !unspecified(item)
        || s(&item["project"]).eq_ignore_ascii_case("owner")
        || item["cancel_requested"] == true
        || item["stopped"] == true
        || arr(&intake["queue"])
            .iter()
            .chain(arr(&intake["queued"]).iter())
            .any(|q| item["id"].is_number() && q["todo_id"] == item["id"])
        || jobs.iter().any(|j| {
            linked(item, j) && matches!(s(&j["status"]), "queued" | "starting" | "running")
        })
    {
        return false;
    }
    let created = epoch(&item["created_s"]);
    let created = if created > 0. {
        created
    } else {
        // Older todo.exe records use Japan time with minute precision.
        chrono::NaiveDateTime::parse_from_str(s(&item["created"]), "%Y-%m-%d %H:%M")
            .ok()
            .map(|at| at.and_utc().timestamp() as f64 - 9. * 3600.)
            .unwrap_or_else(|| epoch(&item["created"]))
    };
    created > 0. && now - created >= AGE_S
}
