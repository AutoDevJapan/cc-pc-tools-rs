//! Shared production contract for watchdog requests and their durable retries.
use serde_json::{Value, json};

fn valid_settings(value: &Value) -> bool {
    (value["model"].is_null()
        || value["model"]
            .as_str()
            .is_some_and(cc_scheduler::backend::valid_model))
        && (value["effort"].is_null()
            || value["effort"]
                .as_str()
                .is_some_and(cc_scheduler::backend::valid_effort))
}

pub fn delegation_request(item: &Value, pending: &Value) -> Value {
    let saved = &pending["delegation_request"];
    if saved.is_object()
        && saved["id"] == item["id"]
        && saved["request_id"] == pending["request"]
        && valid_settings(saved)
    {
        return saved.clone();
    }
    let specified = |field: &str| {
        if item[field].is_null() {
            pending["job"][field].clone()
        } else {
            item[field].clone()
        }
    };
    let model = specified("model");
    let effort = specified("effort");
    let changed = model != pending["job"]["model"] || effort != pending["job"]["effort"];
    let isolated = changed || pending["workspace"]["isolate_worktree"] != false;
    let session = if isolated {
        None
    } else {
        ["thread_id", "resume_session"].iter().find_map(|field| {
            uuid::Uuid::parse_str(pending["job"][*field].as_str()?)
                .ok()
                .map(|id| id.to_string())
        })
    };
    json!({"id":item["id"],"request_id":pending["request"],"prompt":pending["prompt"],
        "source_job_id":pending["job"]["id"],"resume_session":session,"cwd":pending["workspace"]["cwd"],
        "isolate_worktree":isolated,"model":model,"effort":effort})
}

/// Called under todo.lock before sending. A malformed request has never been
/// accepted, so it must not become an immutable retry. Valid requests are frozen
/// before I/O to retain their identity even when the acceptance response is lost.
pub fn freeze_delegation_request(
    item: &mut Value,
    request: &Value,
) -> Result<Option<Value>, String> {
    if item["stall_recovery"]["pending"]["request"] != *request
        || !matches!(item["status"].as_str(), Some("todo" | "doing"))
    {
        return Ok(None);
    }
    let payload = delegation_request(item, &item["stall_recovery"]["pending"]);
    if !valid_settings(&payload) {
        return Err(
            "委譲するモデルまたは強度の指定が不正です。指定を修正して再試行してください。".into(),
        );
    }
    item["stall_recovery"]["pending"]["delegation_request"] = payload.clone();
    Ok(Some(payload))
}
