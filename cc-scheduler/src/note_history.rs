//! Preserve the locked pre-edit note, including deliveries made after an observation.
use crate::common::*;
use cc_job_policy::{todo_history, todo_store};
use serde_json::{json, Value};
use std::path::Path;

pub(super) fn preserve(before: &Value, row: &mut Value) -> Result<bool> {
    if before.get("note") == row.get("note") {
        return Ok(false);
    }
    let Some(note) = before.get("note") else {
        return Ok(false);
    };
    if row.get("note_history").is_some_and(|v| !v.is_array()) {
        return Err(Error("todo_history_invalid"));
    }
    push(
        &mut row["note_history"],
        json!({"note":note,"updated":before["updated"]}),
    );
    Ok(true)
}

/// Keep the snapshot limit and the ordinary store lock. Publish and independently
/// verify immutable segments before saving their references in the canonical DB.
pub(super) fn make_room(store: &Path, db: &mut Value) -> Result<()> {
    // A short changed note alone may not free enough room for its archive reference.
    // Reclaim the largest inline histories first; never hydrate archived histories.
    let mut candidates: Vec<_> = db["items"]
        .as_array()
        .ok_or(Error("ValueError"))?
        .iter()
        .filter_map(|row| {
            let id = row["id"].as_i64().filter(|id| *id > 0)?;
            row["note_history"].as_array().filter(|v| !v.is_empty())?;
            Some((id, encode(&row["note_history"], 1).len()))
        })
        .collect();
    candidates.sort_by_key(|entry| std::cmp::Reverse(entry.1));
    for (id, _) in candidates {
        // Keep errors classified, without emitting a note, archive path or store value.
        todo_history::archive(store, db, id, true)
            .map_err(|_| Error("todo_history_archive_failed"))?;
        if encode(db, 1).len() <= todo_store::MAX_BYTES {
            return Ok(());
        }
    }
    Err(Error("todo_store_too_large: limit 64 MiB"))
}
