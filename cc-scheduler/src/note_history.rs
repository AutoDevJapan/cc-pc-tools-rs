//! Preserve the locked pre-edit note, including deliveries made after an observation.
use crate::common::*;
use serde_json::{json, Value};

pub(super) fn preserve(before: &Value, row: &mut Value) -> Result<()> {
    if before.get("note") == row.get("note") {
        return Ok(());
    }
    let Some(note) = before.get("note") else {
        return Ok(());
    };
    if row.get("note_history").is_some_and(|v| !v.is_array()) {
        return Err(Error("todo_history_invalid"));
    }
    push(
        &mut row["note_history"],
        json!({"note":note,"updated":before["updated"]}),
    );
    Ok(())
}
