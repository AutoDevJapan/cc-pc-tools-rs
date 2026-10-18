//! The new conversation bridge supplies API-verified metadata, never private reply text.
//! `run` holds the canonical ToDo lock across these transitions and atomic persistence.
use crate::{Paths, Result};
use chrono::{DateTime, SecondsFormat, Utc};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{fs, io::Read, path::Path};

const MAX_BYTES: u64 = 1024 * 1024;
const QUESTION: &str = "discord-conversation-question-v1";
const ANSWER: &str = "discord-conversation-answer-v1";
const WAIT: &str = "todo-owner-question-wait-v1";
const RECEIPT: &str = "todo-owner-question-answer-receipt-v1";
const WAIT_KEY: &str = "owner_question_wait";
const ANSWERS_KEY: &str = "owner_question_answer_receipts";
const QUESTION_FIELDS: &[&str] = &[
    "domain",
    "request_id",
    "todo_id",
    "guild_id",
    "owner_id",
    "bot_id",
    "thread_id",
    "question_message_id",
    "progress_thread_id",
    "question_fingerprint",
    "created_at",
];
const ANSWER_FIELDS: &[&str] = &[
    "domain",
    "request_id",
    "todo_id",
    "thread_id",
    "question_message_id",
    "question_fingerprint",
    "question_receipt_sha256",
    "message_id",
    "author_id",
    "source",
    "reply_sha256",
    "acquired_at",
    "receipt_file_sha256",
];

fn fail<T>(code: &str) -> Result<T> {
    Err(format!("conversation_{code}"))
}
fn text<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
fn digest(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn hash(v: &str) -> bool {
    v.len() == 64
        && v.bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn decimal(v: &str) -> bool {
    !v.starts_with('0')
        && v.bytes().all(|b| b.is_ascii_digit())
        && v.parse::<u64>().is_ok_and(|n| n > 0)
}
fn request(v: &str) -> bool {
    (1..=96).contains(&v.len())
        && v.bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}
fn shape(v: &Value, fields: &[&str]) -> Result<()> {
    let obj = v.as_object().ok_or("conversation_schema_invalid")?;
    if obj.len() != fields.len() || fields.iter().any(|key| !obj.contains_key(*key)) {
        return fail("schema_invalid");
    }
    Ok(())
}

fn reparse(meta: &fs::Metadata) -> bool {
    #[cfg(windows)]
    {
        use std::os::windows::fs::MetadataExt;
        meta.file_attributes() & 0x400 != 0
    }
    #[cfg(not(windows))]
    {
        meta.file_type().is_symlink()
    }
}

/// Bound the read, reject reparse points/non-files, and hash and parse this one buffer.
fn bytes(path: &Path) -> Result<Vec<u8>> {
    let absolute = std::path::absolute(path).map_err(|_| "conversation_file_unavailable")?;
    for ancestor in absolute.ancestors() {
        let meta = fs::symlink_metadata(ancestor).map_err(|_| "conversation_file_unavailable")?;
        if reparse(&meta) {
            return fail("file_not_regular");
        }
    }
    let meta = fs::symlink_metadata(&absolute).map_err(|_| "conversation_file_unavailable")?;
    if !meta.is_file() {
        return fail("file_not_regular");
    }
    let mut options = fs::OpenOptions::new();
    options.read(true);
    #[cfg(windows)]
    {
        use std::os::windows::fs::OpenOptionsExt;
        use windows_sys::Win32::Storage::FileSystem::{
            FILE_FLAG_OPEN_REPARSE_POINT, FILE_SHARE_READ,
        };
        options
            .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT)
            .share_mode(FILE_SHARE_READ);
    }
    #[cfg(target_os = "linux")]
    {
        use std::os::unix::fs::OpenOptionsExt;
        // O_NOFOLLOW | O_NONBLOCK: a raced symlink/FIFO must not be followed or hang.
        options.custom_flags(0o400000 | 0o4000);
    }
    let file = options
        .open(&absolute)
        .map_err(|_| "conversation_file_unavailable")?;
    let meta = file
        .metadata()
        .map_err(|_| "conversation_file_unavailable")?;
    if !meta.is_file() || reparse(&meta) {
        return fail("file_not_regular");
    }
    if meta.len() > MAX_BYTES {
        return fail("file_too_large");
    }
    let mut data = Vec::new();
    file.take(MAX_BYTES + 1)
        .read_to_end(&mut data)
        .map_err(|_| "conversation_file_unavailable")?;
    if data.len() as u64 > MAX_BYTES {
        return fail("file_too_large");
    }
    Ok(data)
}

fn read(path: &Path, fields: &[&str]) -> Result<(Value, String)> {
    let data = bytes(path)?;
    let value: Value = serde_json::from_slice(&data).map_err(|_| "conversation_schema_invalid")?;
    shape(&value, fields)?;
    if value.as_object().unwrap().values().any(|v| !v.is_string()) {
        return fail("schema_invalid");
    }
    // These envelopes are flat and contain only strings. Count separators outside
    // JSON strings before accepting Value, whose map would otherwise erase duplicates.
    let (mut quoted, mut escaped, mut separators) = (false, false, 0);
    for b in &data {
        if quoted {
            if escaped {
                escaped = false;
            } else if *b == b'\\' {
                escaped = true;
            } else if *b == b'"' {
                quoted = false;
            }
        } else if *b == b'"' {
            quoted = true;
        } else if *b == b':' {
            separators += 1;
        }
    }
    if separators != fields.len() {
        return fail("schema_invalid");
    }
    Ok((value, digest(&data)))
}

fn clock() -> Result<DateTime<Utc>> {
    // Same runtime clock API as the existing Rust retry transaction kernel.
    let at = crate::time_s();
    if !at.is_finite() || at <= 0. || at * 1_000_000. >= i64::MAX as f64 {
        return fail("clock_unavailable");
    }
    DateTime::from_timestamp_micros((at * 1_000_000.) as i64)
        .ok_or_else(|| "conversation_clock_unavailable".into())
}
fn timestamp(v: &Value, key: &str, at: DateTime<Utc>) -> Result<DateTime<Utc>> {
    let time = DateTime::parse_from_rfc3339(text(v, key))
        .map_err(|_| "conversation_timestamp_invalid")?
        .with_timezone(&Utc);
    if time.timestamp() <= 0 || time > at {
        return fail("timestamp_invalid");
    }
    Ok(time)
}
fn validate(v: &Value, id: i64, answer: bool, at: DateTime<Utc>) -> Result<()> {
    let fields = if answer {
        ANSWER_FIELDS
    } else {
        QUESTION_FIELDS
    };
    shape(v, fields)?;
    if fields.iter().any(|k| !v[*k].is_string())
        || id <= 0
        || text(v, "todo_id") != id.to_string()
        || !request(text(v, "request_id"))
        || text(v, "domain") != if answer { ANSWER } else { QUESTION }
    {
        return fail("schema_invalid");
    }
    let ids: &[&str] = if answer {
        &[
            "todo_id",
            "thread_id",
            "question_message_id",
            "message_id",
            "author_id",
        ]
    } else {
        &[
            "todo_id",
            "guild_id",
            "owner_id",
            "bot_id",
            "thread_id",
            "question_message_id",
            "progress_thread_id",
        ]
    };
    if ids.iter().any(|k| !decimal(text(v, k)))
        || v["question_message_id"] != v["thread_id"]
        || !hash(text(v, "question_fingerprint"))
    {
        return fail("schema_invalid");
    }
    if answer {
        if text(v, "source") != "discord-api-v10"
            || [
                "reply_sha256",
                "question_receipt_sha256",
                "receipt_file_sha256",
            ]
            .iter()
            .any(|k| !hash(text(v, k)))
            || text(v, "message_id").parse::<u64>().unwrap()
                <= text(v, "question_message_id").parse::<u64>().unwrap()
        {
            return fail("schema_invalid");
        }
        timestamp(v, "acquired_at", at)?;
    } else {
        if v["progress_thread_id"] == v["thread_id"] || v["owner_id"] == v["bot_id"] {
            return fail("schema_invalid");
        }
        timestamp(v, "created_at", at)?;
    }
    Ok(())
}

fn configured(v: &Value, cfg: &Value) -> Result<()> {
    if !cfg.is_object()
        || !cfg["guild_id"].as_str().is_some_and(decimal)
        || !cfg["owner_user_id"].as_str().is_some_and(decimal)
        || !["application_id", "bot_user_id", "bot_id"]
            .iter()
            .any(|key| cfg.get(*key).is_some())
    {
        return fail("config_invalid");
    }
    for (setting, field) in [
        ("guild_id", "guild_id"),
        ("owner_user_id", "owner_id"),
        ("application_id", "bot_id"),
        ("bot_user_id", "bot_id"),
        ("bot_id", "bot_id"),
    ] {
        if let Some(expected) = cfg.get(setting)
            && (!expected.as_str().is_some_and(decimal) || *expected != v[field])
        {
            return fail("config_mismatch");
        }
    }
    Ok(())
}
fn check_config(paths: &Paths, v: &Value) -> Result<()> {
    let explicit = std::env::var_os("CC_ASK_CONFIG");
    let production = Paths::production().store;
    let same = paths
        .store
        .to_string_lossy()
        .replace('\\', "/")
        .eq_ignore_ascii_case(&production.to_string_lossy().replace('\\', "/"))
        || fs::canonicalize(&paths.store)
            .ok()
            .zip(fs::canonicalize(&production).ok())
            .is_some_and(|(a, b)| a == b);
    if !same && explicit.is_none() {
        return Ok(());
    }
    let config = explicit.clone().map(std::path::PathBuf::from).or_else(|| {
        std::env::var_os("USERPROFILE")
            .or_else(|| std::env::var_os("HOME"))
            .map(|p| Path::new(&p).join(".godd/discord-notify.json"))
    });
    let Some(config) = config else {
        return Ok(());
    };
    match fs::symlink_metadata(&config) {
        Err(e) if e.kind() == std::io::ErrorKind::NotFound && explicit.is_none() => return Ok(()),
        Err(_) => return fail("config_unavailable"),
        Ok(_) => (),
    }
    let cfg =
        serde_json::from_slice(&bytes(&config)?).map_err(|_| "conversation_config_invalid")?;
    configured(v, &cfg)
}

fn row_index(db: &Value, id: i64) -> Result<usize> {
    let rows = db["items"].as_array().ok_or("conversation_store_invalid")?;
    let mut matches = rows
        .iter()
        .enumerate()
        .filter(|(_, v)| v["id"].as_i64() == Some(id));
    let (index, row) = matches.next().ok_or("conversation_item_unavailable")?;
    let project = text(row, "project");
    if matches.next().is_some()
        || project.is_empty()
        || project.eq_ignore_ascii_case("owner")
        || db["projects"]
            .get(project)
            .and_then(Value::as_str)
            .is_none_or(|path| path.trim().is_empty() || path.contains('\0'))
    {
        return fail("scope_mismatch");
    }
    Ok(index)
}

fn dependencies_clear(db: &Value, row: &Value, key: &str) -> bool {
    if row[key].is_null() {
        return true;
    }
    row[key].as_array().is_some_and(|on| {
        on.iter().all(|id| {
            if id.as_i64().is_none_or(|id| id <= 0) || *id == row["id"] {
                return false;
            }
            let mut rows = db["items"]
                .as_array()
                .unwrap()
                .iter()
                .filter(|v| v["id"] == *id);
            rows.next().is_some_and(|v| v["status"] == "done") && rows.next().is_none()
        })
    })
}
fn clear(db: &Value, row: &Value, at: DateTime<Utc>) -> Result<()> {
    if cc_job_policy::cancellation::owner_stopped(row, &Value::Null)
        || [
            "stopped",
            "cancel_requested",
            "owner_stopped",
            "paused",
            "owner_postponement_recorded",
        ]
        .iter()
        .any(|k| !row[*k].is_null() && row[*k] != false)
        || (!row["stop_kind"].is_null() && row["stop_kind"] != "")
        || [
            "decision_wait",
            "wait_ready",
            "resume_condition",
            "external_resume_condition",
        ]
        .iter()
        .any(|k| !row[*k].is_null())
        || !dependencies_clear(db, row, "wait_on")
        || !dependencies_clear(db, row, "work_owner_wait_on")
        || (!row["scheduler"].is_null() && !row["scheduler"].is_object())
        || (!row["stall_recovery"].is_null() && !row["stall_recovery"].is_object())
        || [
            "owner_stop_job",
            "question",
            "question_close_pending",
            "pending_cancel",
        ]
        .iter()
        .any(|k| {
            !row["scheduler"][*k].is_null()
                && row["scheduler"][*k] != false
                && row["scheduler"][*k] != ""
        })
    {
        return fail("other_hold");
    }
    for value in [
        &row["scheduler"]["not_before"],
        &row["stall_recovery"]["next_at"],
        &row["discord_restart_not_before"],
    ] {
        if !value.is_null()
            && !value.as_f64().is_some_and(|n| {
                n.is_finite() && n >= 0. && n <= at.timestamp_micros() as f64 / 1_000_000.
            })
        {
            return fail("other_hold");
        }
    }
    Ok(())
}
fn owner_clear(paths: &Paths, row: &Value) -> Result<()> {
    // Only the exact current owner is relevant. Do not scan historical jobs or
    // use a stopped unrelated worker to reinterpret the selected task.
    let owner = row["scheduler"]["job"]
        .as_str()
        .filter(|s| !s.is_empty())
        .or_else(|| {
            row["worker_receipt"]["job"]
                .as_str()
                .filter(|s| !s.is_empty())
        });
    let Some(owner) = owner else {
        return Ok(());
    };
    if !crate::job_refs::valid_id(owner) {
        return fail("owner_record_invalid");
    }
    let path = paths.jobs.join(owner).join("job.json");
    match fs::symlink_metadata(&path) {
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(_) => return fail("owner_record_unavailable"),
        Ok(_) => (),
    }
    let job: Value =
        serde_json::from_slice(&bytes(&path)?).map_err(|_| "conversation_owner_record_invalid")?;
    if job["id"] != owner || !job.is_object() {
        return fail("owner_record_invalid");
    }
    if cc_job_policy::cancellation::blocks_recovery(row, &job) {
        return fail("owner_stopped");
    }
    Ok(())
}
fn block_note(question: &Value, sha: &str, previous: &Value) -> String {
    let marker = marker(question, sha);
    match previous.as_str().filter(|s| !s.is_empty()) {
        Some(note) => format!("{note}\n{marker}"),
        None => marker,
    }
}
fn marker(question: &Value, sha: &str) -> String {
    format!(
        "[discord-conversation-question-v1 request_id={} sha256={sha}]",
        text(question, "request_id")
    )
}
fn answered_note(row: &Value, wait: &Value) -> Result<String> {
    let note = row["note"].as_str().ok_or("conversation_block_changed")?;
    let marker = marker(&wait["question"], text(wait, "question_receipt_sha256"));
    let mut occurrences = note.match_indices(&marker);
    let (start, _) = occurrences.next().ok_or("conversation_block_changed")?;
    let end = start + marker.len();
    if occurrences.next().is_some()
        || (start > 0 && note.as_bytes()[start - 1] != b'\n')
        || (end < note.len() && note.as_bytes()[end] != b'\n')
    {
        return fail("block_changed");
    }
    // Remove exactly the line inserted by register. Keep the original note and
    // the parent's canonical edit containing the API-verified reply verbatim.
    let (from, until) = if start > 0 {
        (start - 1, end)
    } else if end < note.len() {
        (start, end + 1)
    } else {
        (start, end)
    };
    Ok(format!("{}{}", &note[..from], &note[until..]))
}
fn validate_wait(wait: &Value, id: i64, at: DateTime<Utc>) -> Result<()> {
    shape(
        wait,
        &[
            "domain",
            "question",
            "question_receipt_sha256",
            "previous_status",
            "previous_note",
            "previous_note_present",
            "block_note",
            "blocked_at",
        ],
    )?;
    validate(&wait["question"], id, false, at)?;
    if wait["domain"] != WAIT
        || !hash(text(wait, "question_receipt_sha256"))
        || !matches!(text(wait, "previous_status"), "todo" | "doing")
        || !wait["previous_note_present"].is_boolean()
        || !(wait["previous_note"].is_string() || wait["previous_note"].is_null())
        || (wait["previous_note_present"] == false && !wait["previous_note"].is_null())
        || wait["block_note"]
            != block_note(
                &wait["question"],
                text(wait, "question_receipt_sha256"),
                &wait["previous_note"],
            )
        || timestamp(wait, "blocked_at", at)? < timestamp(&wait["question"], "created_at", at)?
    {
        return fail("wait_invalid");
    }
    Ok(())
}
fn matches_wait(wait: &Value, answer: &Value, at: DateTime<Utc>) -> Result<()> {
    let q = &wait["question"];
    if [
        "request_id",
        "todo_id",
        "thread_id",
        "question_message_id",
        "question_fingerprint",
    ]
    .iter()
    .any(|k| q[*k] != answer[*k])
        || wait["question_receipt_sha256"] != answer["question_receipt_sha256"]
        || q["owner_id"] != answer["author_id"]
        || timestamp(answer, "acquired_at", at)? < timestamp(q, "created_at", at)?
    {
        return fail("answer_mismatch");
    }
    Ok(())
}
fn receipts(row: &Value) -> Result<&[Value]> {
    if row[ANSWERS_KEY].is_null() {
        return Ok(&[]);
    }
    row[ANSWERS_KEY]
        .as_array()
        .map(Vec::as_slice)
        .ok_or_else(|| "conversation_receipts_invalid".into())
}
fn validate_receipt(receipt: &Value, id: i64, at: DateTime<Utc>) -> Result<()> {
    shape(
        receipt,
        &[
            "domain",
            "wait",
            "answer",
            "answer_file_sha256",
            "applied_at",
        ],
    )?;
    validate_wait(&receipt["wait"], id, at)?;
    validate(&receipt["answer"], id, true, at)?;
    matches_wait(&receipt["wait"], &receipt["answer"], at)?;
    if receipt["domain"] != RECEIPT
        || !hash(text(receipt, "answer_file_sha256"))
        || timestamp(receipt, "applied_at", at)? < timestamp(&receipt["answer"], "acquired_at", at)?
    {
        return fail("receipt_invalid");
    }
    Ok(())
}

/// An identity cannot be assigned to another ToDo, question or historical receipt.
fn unique(db: &Value, id: i64, q: &Value, sha: &str, at: DateTime<Utc>) -> Result<()> {
    let mut seen = 0;
    for row in db["items"].as_array().ok_or("conversation_store_invalid")? {
        let mut waits = Vec::new();
        if !row[WAIT_KEY].is_null() {
            waits.push(&row[WAIT_KEY]);
        }
        for receipt in receipts(row)? {
            validate_receipt(
                receipt,
                row["id"].as_i64().ok_or("conversation_store_invalid")?,
                at,
            )?;
            waits.push(&receipt["wait"]);
        }
        for wait in waits {
            validate_wait(
                wait,
                row["id"].as_i64().ok_or("conversation_store_invalid")?,
                at,
            )?;
            let old = &wait["question"];
            if old["request_id"] == q["request_id"] || old["thread_id"] == q["thread_id"] {
                seen += 1;
                if row["id"] != id || old != q || wait["question_receipt_sha256"] != sha || seen > 1
                {
                    return fail("question_conflict");
                }
            }
        }
    }
    Ok(())
}
fn metadata(id: i64, q: &Value, sha: &str, changed: bool, answered: bool, resumed: bool) -> Value {
    json!({"id":id, "request_id":q["request_id"], "question_receipt_sha256":sha,
        "changed":changed, "answered":answered, "resumed":resumed})
}

/// Called by the canonical command dispatcher with the shared store lock held.
pub fn register(db: &mut Value, paths: &Paths, id: i64, path: &Path) -> Result<Value> {
    let (question, sha) = read(path, QUESTION_FIELDS)?;
    let at = clock()?;
    validate(&question, id, false, at)?;
    check_config(paths, &question)?;
    let index = row_index(db, id)?;
    unique(db, id, &question, &sha, at)?;
    let row = &db["items"][index];
    if !row[WAIT_KEY].is_null() {
        if row[WAIT_KEY]["question"] == question && row[WAIT_KEY]["question_receipt_sha256"] == sha
        {
            return Ok(metadata(id, &question, &sha, false, false, false));
        }
        return fail("question_conflict");
    }
    if receipts(row)?
        .iter()
        .any(|r| r["wait"]["question"]["request_id"] == question["request_id"])
    {
        return Ok(metadata(id, &question, &sha, false, true, false));
    }
    if !matches!(text(row, "status"), "todo" | "doing") {
        return fail("status_not_eligible");
    }
    clear(db, row, at)?;
    owner_clear(paths, row)?;
    if !row["note"].is_null() && !row["note"].is_string() {
        return fail("note_invalid");
    }
    let note = block_note(&question, &sha, &row["note"]);
    let blocked_at = at.to_rfc3339_opts(SecondsFormat::Micros, true);
    let wait = json!({"domain":WAIT, "question":question, "question_receipt_sha256":sha,
        "previous_status":row["status"], "previous_note":row["note"],
        "previous_note_present":row.get("note").is_some(), "block_note":note, "blocked_at":blocked_at});
    let row = &mut db["items"][index];
    row[WAIT_KEY] = wait;
    row["status"] = json!("block");
    row["note"] = json!(note);
    row["updated"] = json!(blocked_at);
    Ok(metadata(id, &question, &sha, true, false, false))
}

/// Consume exactly once. Later retries cannot release a new stop or a completed task.
pub fn answer(db: &mut Value, paths: &Paths, id: i64, path: &Path) -> Result<Value> {
    let (answer, sha) = read(path, ANSWER_FIELDS)?;
    let at = clock()?;
    validate(&answer, id, true, at)?;
    let index = row_index(db, id)?;
    let row = &db["items"][index];
    for receipt in receipts(row)? {
        validate_receipt(receipt, id, at)?;
        if receipt["answer"]["request_id"] == answer["request_id"] {
            let wait = &receipt["wait"];
            unique(
                db,
                id,
                &wait["question"],
                text(wait, "question_receipt_sha256"),
                at,
            )?;
            if receipt["answer"] != answer || receipt["answer_file_sha256"] != sha {
                return fail("answer_conflict");
            }
            return Ok(metadata(
                id,
                &wait["question"],
                text(wait, "question_receipt_sha256"),
                false,
                true,
                false,
            ));
        }
    }
    let wait = row[WAIT_KEY].clone();
    validate_wait(&wait, id, at)?;
    matches_wait(&wait, &answer, at)?;
    check_config(paths, &wait["question"])?;
    unique(
        db,
        id,
        &wait["question"],
        text(&wait, "question_receipt_sha256"),
        at,
    )?;
    if row["status"] != "block" {
        return fail("block_changed");
    }
    let note = answered_note(row, &wait)?;
    clear(db, row, at)?;
    owner_clear(paths, row)?;
    let applied_at = at.to_rfc3339_opts(SecondsFormat::Micros, true);
    let receipt = json!({"domain":RECEIPT, "wait":wait, "answer":answer,
        "answer_file_sha256":sha, "applied_at":applied_at});
    let row = &mut db["items"][index];
    let mut saved = receipts(row)?.to_vec();
    saved.push(receipt);
    row[ANSWERS_KEY] = json!(saved);
    row.as_object_mut().unwrap().remove(WAIT_KEY);
    if row["note"] == wait["block_note"] && wait["previous_note_present"] == true {
        row["note"] = wait["previous_note"].clone();
    } else if row["note"] != wait["block_note"] || !note.is_empty() {
        row["note"] = json!(note);
    } else {
        row.as_object_mut().unwrap().remove("note");
    }
    row["status"] = json!("todo");
    row["updated"] = json!(applied_at);
    Ok(metadata(
        id,
        &wait["question"],
        text(&wait, "question_receipt_sha256"),
        true,
        true,
        true,
    ))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn available_production_identity_fields_must_match() {
        let q = json!({"guild_id":"100", "owner_id":"101", "bot_id":"102"});
        assert!(
            configured(
                &q,
                &json!({"guild_id":"100", "owner_user_id":"101", "application_id":"102"})
            )
            .is_ok()
        );
        for cfg in [
            json!({"guild_id":"999"}),
            json!({"owner_user_id":"999"}),
            json!({"bot_id":"999"}),
            json!({"bot_user_id":"999"}),
            json!({"guild_id":100}),
        ] {
            assert!(configured(&q, &cfg).is_err());
        }
    }
}
