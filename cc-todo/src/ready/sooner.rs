//! Compare and shorten one technical wait. The caller holds todo.lock through save.
use crate::{Paths, Result, now};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};

fn dependency_done(db: &Value, row: &Value, id: &Value) -> bool {
    if id.as_i64().is_none_or(|n| n <= 0) || *id == row["id"] {
        return false;
    }
    let mut matches = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|r| r["id"] == *id);
    matches.next().is_some_and(|r| r["status"] == "done") && matches.next().is_none()
}

pub(super) fn holds_clear(db: &Value, row: &Value) -> bool {
    let active = |v: &Value| !v.is_null() && *v != false && *v != "";
    let flags = [
        "stopped",
        "cancel_requested",
        "owner_stopped",
        "paused",
        "owner_postponement_recorded",
    ];
    let conditions = [
        "decision_wait",
        "owner_question_wait",
        "resume_condition",
        "external_resume_condition",
    ];
    if flags.iter().any(|k| !row[*k].is_null() && row[*k] != false)
        || active(&row["stop_kind"])
        || conditions.iter().any(|k| !row[*k].is_null())
        || [
            "owner_stop_job",
            "question",
            "question_close_pending",
            "pending_cancel",
        ]
        .iter()
        .any(|k| active(&row["scheduler"][*k]))
        || (!row["scheduler"].is_null() && !row["scheduler"].is_object())
        || (!row["stall_recovery"].is_null() && !row["stall_recovery"].is_object())
    {
        return false;
    }
    let on = &row["wait_on"];
    (on.is_null()
        || on
            .as_array()
            .is_some_and(|ids| ids.iter().all(|id| dependency_done(db, row, id))))
        && (row["work_owner_wait_on"].is_null()
            || dependency_done(db, row, &row["work_owner_wait_on"]))
}

/// A preview fingerprint is scoped to the canonical store, complete original row,
/// project and dependency states. No caller-supplied replacement row is consumed.
fn fingerprint(db: &Value, paths: &Paths, row: &Value) -> Result<String> {
    let source = std::fs::canonicalize(&paths.store).map_err(|_| "retry_source_unavailable")?;
    let dependencies: Vec<_> = db["items"]
        .as_array()
        .ok_or("invalid items")?
        .iter()
        .filter(|r| {
            row["wait_on"]
                .as_array()
                .is_some_and(|ids| ids.contains(&r["id"]))
                || (!row["work_owner_wait_on"].is_null() && r["id"] == row["work_owner_wait_on"])
        })
        .collect();
    let project = row["project"].as_str().and_then(|p| db["projects"].get(p));
    let snapshot = json!({"version":"todo-retry-sooner-v1", "source":source, "item":row,
        "project":project, "dependencies":dependencies});
    let bytes = serde_json::to_vec(&snapshot).map_err(|_| "retry_snapshot_invalid")?;
    Ok(format!("{:x}", Sha256::digest(bytes)))
}

/// `expected` is supplied only for an explicit apply; previews never mutate db.
/// This does not clear a prerequisite, release work or confirm a capability.
pub fn retry_sooner(
    db: &mut Value,
    paths: &Paths,
    id: i64,
    seconds: u64,
    reason: &str,
    expected: Option<&str>,
    at: u64,
) -> Result<Value> {
    if !(60..=86400).contains(&seconds) || reason.trim().is_empty() || reason.len() > 8000 {
        return Err(
            "再確認までの秒数は60〜86400、状態変化の理由は空でない8000バイト以内で指定してください"
                .into(),
        );
    }
    let until = at.checked_add(seconds).ok_or("再確認の時刻が範囲外です")?;
    let items = db["items"].as_array().ok_or("invalid items")?;
    let mut rows = items.iter().enumerate().filter(|(_, r)| r["id"] == id);
    let (index, row) = rows.next().ok_or("retry_item_missing")?;
    if rows.next().is_some() {
        return Err("retry_item_ambiguous".into());
    }
    let wait = &row["wait_ready"];
    let before = wait["not_before"].as_u64().ok_or("retry_wait_invalid")?;
    // retry_due at the old deadline also validates the original 60..86400 interval.
    if row["status"] != "wait"
        || !holds_clear(db, row)
        || !cc_job_policy::todo_wait::retry_due(wait, before)
        || wait["since"].as_u64().is_none_or(|since| since > at)
    {
        return Err("retry_other_hold_or_invalid_wait".into());
    }
    let state_sha256 = fingerprint(db, paths, row)?;
    if expected.is_some_and(|hash| hash != state_sha256) {
        return Err("retry_snapshot_changed".into());
    }
    if until >= before {
        return Err("retry_deadline_must_be_earlier".into());
    }
    if expected.is_some() {
        let row = &mut db["items"][index];
        row["wait_ready"]["not_before"] = json!(until);
        row["wait_ready"]["recheck"] = json!({"at":at,"previous_not_before":before,
            "reason":reason,"state_sha256":state_sha256});
        row["updated"] = json!(now());
    }
    Ok(
        json!({"id":id,"changed":expected.is_some(),"state_sha256":state_sha256,
        "previous_not_before":before,"not_before":until,"recheck_only":true}),
    )
}
