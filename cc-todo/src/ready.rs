//! A scheduled checker releases the existing task; polling does not need an AI worker.
use crate::{Result, find, now};
use serde_json::{Value, json};
use std::{fs, path::Path};

pub const STATE: &str = "D:/CC/Apps/chatgpt-web/state.json";

pub fn read_state(path: &Path) -> Result<Value> {
    match fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(|e| e.to_string()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Value::Null),
        Err(e) => Err(e.to_string()),
    }
}

pub fn confirmed(wait: &Value, state: &Value, at: u64) -> bool {
    let Some(since) = wait["since"].as_u64() else {
        return false;
    };
    let Some(checked) = state["checked_at"].as_u64() else {
        return false;
    };
    wait["condition"] == "chatgpt-plus"
        && state["backend"] == "owner_chrome"
        && state["login"] == "logged_in"
        && checked >= since
        && checked <= at
        && at - checked <= 26 * 3600
}

pub fn register(db: &mut Value, id: i64, condition: &str) -> Result<()> {
    if condition != "chatgpt-plus" {
        return Err("未対応の再開条件です".into());
    }
    let row = find(db, id)?;
    if row["status"] == "done" {
        return Err("完了したToDoには待機を設定できません".into());
    }
    // Repeating the registration must not keep moving the acceptance timestamp forward.
    if row["wait_ready"]["condition"] != condition {
        row["wait_ready"] = json!({"condition":condition,"since":now_epoch()});
    }
    if row["status"] != "block" {
        row["status"] = json!("wait");
    }
    row.as_object_mut()
        .ok_or("invalid item")?
        .remove("work_owner_job");
    row.as_object_mut()
        .ok_or("invalid item")?
        .remove("work_owner_wait_on");
    if let Some(recovery) = row["stall_recovery"].as_object_mut() {
        recovery.remove("pending");
        recovery.remove("report");
    }
    row["updated"] = json!(now());
    Ok(())
}

fn now_epoch() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

pub fn resume(db: &mut Value, id: i64, condition: &str, state: &Value, at: u64) -> Result<bool> {
    let row = find(db, id)?.clone();
    if row["status"] != "wait"
        || row["wait_ready"]["condition"] != condition
        || !confirmed(&row["wait_ready"], state, at)
        || cc_job_policy::todo_wait::waiting(db, &row)
    {
        return Ok(false);
    }
    let row = find(db, id)?;
    row.as_object_mut()
        .ok_or("invalid item")?
        .remove("wait_ready");
    row.as_object_mut().ok_or("invalid item")?.remove("wait_on");
    cc_job_policy::todo_wait::release_item(row);
    row["updated"] = json!(now());
    Ok(true)
}

pub fn resume_production(db: &mut Value, id: i64, condition: &str) -> Result<bool> {
    let state = read_state(Path::new(STATE))?;
    resume(db, id, condition, &state, now_epoch())
}

pub fn confirmed_production(wait: &Value) -> Result<bool> {
    Ok(confirmed(wait, &read_state(Path::new(STATE))?, now_epoch()))
}
