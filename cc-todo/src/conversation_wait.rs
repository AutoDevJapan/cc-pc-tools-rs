//! The new conversation bridge supplies API-verified metadata, never private reply text.
//! `run` holds the canonical ToDo lock across these transitions and atomic persistence.
use crate::{Paths, Result};
use chrono::{DateTime, SecondsFormat, Utc};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{fs, io::Read, path::Path};

mod continued;
pub use continued::deliver;

const MAX_BYTES: u64 = 1024 * 1024;
const QUESTION: &str = "discord-conversation-question-v1";
const ANSWER: &str = "discord-conversation-answer-v1";
const WAIT: &str = "todo-owner-question-wait-v1";
const RECEIPT: &str = "todo-owner-question-answer-receipt-v1";
const WAIT_KEY: &str = "owner_question_wait";
const ANSWERS_KEY: &str = "owner_question_answer_receipts";
const WITHDRAWALS_KEY: &str = "owner_question_withdrawal_receipts";
const WITHDRAWAL: &str = "todo-question-withdrawal-receipt-v1";
const IMPORTED_BLOCK_NOTE: &str = "ジョブの結果に判断が必要です。";
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
pub(crate) fn bytes(path: &Path) -> Result<Vec<u8>> {
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

/// A caller's guard result is not a Rust identity proof. Bind the exact project
/// and registered path which that caller checked, under the canonical store lock.
pub fn check_scope(db: &Value, id: i64, path: &Path) -> Result<()> {
    let (scope, _) = read(
        path,
        &["domain", "todo_id", "project", "project_path_sha256"],
    )?;
    let index = row_index(db, id)?;
    let project = text(&db["items"][index], "project");
    if scope["domain"] != "discord-conversation-todo-scope-v1"
        || text(&scope, "todo_id") != id.to_string()
        || text(&scope, "project") != project
        || !hash(text(&scope, "project_path_sha256"))
        || text(&scope, "project_path_sha256")
            != digest(db["projects"][project].as_str().unwrap().as_bytes())
    {
        return fail("scope_changed");
    }
    Ok(())
}

pub fn check_snapshot(db: &Value, id: i64, path: &Path) -> Result<()> {
    let expected: Value =
        serde_json::from_slice(&bytes(path)?).map_err(|_| "conversation_state_invalid")?;
    if db["items"][row_index(db, id)?] != expected {
        return fail("state_changed");
    }
    Ok(())
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
fn holds_clear(db: &Value, row: &Value) -> Result<()> {
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
    Ok(())
}
fn clear(db: &Value, row: &Value, at: DateTime<Utc>) -> Result<()> {
    holds_clear(db, row)?;
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
    owner_record(paths, row).map(|_| ())
}
fn owner_record(paths: &Paths, row: &Value) -> Result<Option<Value>> {
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
        return Ok(None);
    };
    if !crate::job_refs::valid_id(owner) {
        return fail("owner_record_invalid");
    }
    let path = paths.jobs.join(owner).join("job.json");
    match fs::symlink_metadata(&path) {
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
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
    Ok(Some(job))
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
    // New waits remember the registering owner/generation. Existing v1 waits
    // retain their original shape and need the explicit original-row CAS.
    let mut envelope = wait.clone();
    if let Some(origin) = envelope
        .as_object_mut()
        .and_then(|obj| obj.remove("origin"))
    {
        shape(
            &origin,
            &["resume_generation", "worker_receipt", "job", "key"],
        )?;
    }
    shape(
        &envelope,
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
fn origin_identity(row: &Value) -> Value {
    json!({"resume_generation":row["resume_generation"], "worker_receipt":row["worker_receipt"],
        "job":row["scheduler"]["job"], "key":row["scheduler"]["key"]})
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
fn withdrawals(row: &Value) -> Result<&[Value]> {
    if row[WITHDRAWALS_KEY].is_null() {
        return Ok(&[]);
    }
    row[WITHDRAWALS_KEY]
        .as_array()
        .map(Vec::as_slice)
        .ok_or_else(|| "conversation_receipts_invalid".into())
}
// A normal resume can retire the worker before the old successful result is
// imported. Only legacy waits lack an immutable registering origin. Keep the
// actual null receipt in the withdrawal evidence; never rebind the old worker.
fn retired_legacy_worker(wait: &Value, worker: &Value, imported: bool) -> bool {
    imported && wait.get("origin").is_none() && worker.get("job") == Some(&Value::Null)
}
fn validate_withdrawal(receipt: &Value, id: i64, at: DateTime<Utc>) -> Result<()> {
    shape(
        receipt,
        &[
            "domain",
            "wait",
            "reason",
            "reason_sha256",
            "expected_state_sha256",
            "origin",
            "released_note",
            "release_kind",
            "applied_at",
        ],
    )?;
    validate_wait(&receipt["wait"], id, at)?;
    shape(
        &receipt["origin"],
        &["resume_generation", "worker_receipt", "job", "launch_key"],
    )?;
    let origin = &receipt["origin"];
    if receipt["domain"] != WITHDRAWAL
        || text(receipt, "reason").trim().is_empty()
        || text(receipt, "reason").contains('\0')
        || !hash(text(receipt, "reason_sha256"))
        || digest(text(receipt, "reason").as_bytes()) != receipt["reason_sha256"]
        || !hash(text(receipt, "expected_state_sha256"))
        || !receipt["released_note"].is_string()
        || !matches!(text(receipt, "release_kind"), "question_marker" | "scheduler_block_result")
        || (receipt["release_kind"] == "scheduler_block_result" && receipt["released_note"] != IMPORTED_BLOCK_NOTE)
        || !origin["resume_generation"].is_u64()
        || !crate::job_refs::valid_id(text(origin, "job"))
        || text(origin, "launch_key").is_empty()
        || (origin["worker_receipt"]["job"] != origin["job"]
            && !retired_legacy_worker(&receipt["wait"], &origin["worker_receipt"],
                receipt["release_kind"] == "scheduler_block_result"))
        || origin["worker_receipt"]["generation"] != origin["resume_generation"]
        || receipt["wait"].get("origin").is_some_and(|saved| *saved != json!({
            "resume_generation":origin["resume_generation"], "worker_receipt":origin["worker_receipt"],
            "job":origin["job"], "key":origin["launch_key"]}))
        || timestamp(receipt, "applied_at", at)? < timestamp(&receipt["wait"], "blocked_at", at)?
    {
        return fail("withdrawal_invalid");
    }
    Ok(())
}

fn finished_question_owner(
    db: &Value,
    paths: &Paths,
    row: &Value,
    wait: &Value,
    id: i64,
    at: DateTime<Utc>,
) -> Result<(Value, String, bool)> {
    if wait
        .get("origin")
        .is_some_and(|origin| *origin != origin_identity(row))
    {
        return fail("owner_changed_or_active");
    }
    clear(db, row, at)?;
    let owner = owner_record(paths, row)?.ok_or("conversation_owner_record_unavailable")?;
    let job = text(&owner, "id");
    let (note, imported) = withdrawal_note(row, wait, &owner)?;
    if !matches!(
        text(&owner, "status"),
        "succeeded" | "failed" | "lost" | "cancelled" | "canceled"
    ) || row["scheduler"]["job"] != job
        || (row["worker_receipt"]["job"] != job
            && !retired_legacy_worker(wait, &row["worker_receipt"], imported))
        || !row["resume_generation"].is_u64()
        || row["worker_receipt"]["generation"] != row["resume_generation"]
        || (!row["processed_results"][job].is_null()
            && row["processed_results"][job]["generation"] != row["resume_generation"])
        || text(&row["scheduler"], "key").is_empty()
        || owner["launch_key"] != row["scheduler"]["key"]
        || !(owner["todo_id"] == id
            || owner["related_todo_ids"]
                .as_array()
                .is_some_and(|ids| ids.contains(&json!(id))))
        || row["jobs"].as_array().and_then(|jobs| jobs.last()) != Some(&owner["id"])
    {
        return fail("owner_changed_or_active");
    }
    let question_created = timestamp(&wait["question"], "created_at", at)?;
    // cc-jobs writes RFC3339 strings (including the local UTC offset). Keep
    // numeric epochs readable for older records, without guessing from file times.
    let created_later = if owner["created_at"].is_string() {
        timestamp(&owner, "created_at", at)? > question_created
    } else {
        let created = owner["created_at"]
            .as_f64()
            .filter(|n| n.is_finite() && *n > 0.)
            .ok_or("conversation_owner_record_invalid")?;
        created > question_created.timestamp_micros() as f64 / 1_000_000.
    };
    if created_later {
        return fail("owner_changed_or_active");
    }
    Ok((owner, note, imported))
}

fn withdrawal_note(row: &Value, wait: &Value, owner: &Value) -> Result<(String, bool)> {
    if text(row, "note").starts_with(text(wait, "block_note")) {
        return answered_note(row, wait).map(|note| (note, false));
    }
    let result = &row["processed_results"][text(owner, "id")];
    // The scheduler's successful-result import is the only markerless path.
    // It has a fixed note and a per-owner, per-generation result receipt. An
    // arbitrary edit, appended stop, old result or another owner's block fails.
    if row["note"] != IMPORTED_BLOCK_NOTE
        || row["scheduler"]["phase"] != "finished"
        || owner["status"] != "succeeded"
        || result["reason"] != "block"
        || result["generation"] != row["resume_generation"]
    {
        return fail("block_changed");
    }
    Ok((text(wait, "previous_note").to_owned(), true))
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
        for receipt in withdrawals(row)? {
            validate_withdrawal(
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
    if withdrawals(row)?
        .iter()
        .any(|r| r["wait"]["question"] == question)
    {
        let mut result = metadata(id, &question, &sha, false, false, false);
        result["withdrawn"] = json!(true);
        return Ok(result);
    }
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
    let wait = json!({"domain":WAIT, "question":question, "question_receipt_sha256":sha, "origin":origin_identity(row),
        "previous_status":row["status"], "previous_note":row["note"],
        "previous_note_present":row.get("note").is_some(), "block_note":note, "blocked_at":blocked_at});
    let row = &mut db["items"][index];
    row[WAIT_KEY] = wait;
    row["status"] = json!("block");
    row["note"] = json!(note);
    row["updated"] = json!(blocked_at);
    Ok(metadata(id, &question, &sha, true, false, false))
}

/// The caller holds the ToDo lock. A durable snapshot binds this operation to
/// its original generation and owner; a retry never releases a later hold.
pub fn withdraw(
    db: &mut Value,
    paths: &Paths,
    id: i64,
    question_file: &Path,
    reason_file: &Path,
    expected_file: &Path,
) -> Result<Value> {
    let (question, sha) = read(question_file, QUESTION_FIELDS)?;
    let reason_bytes = bytes(reason_file)?;
    let reason = std::str::from_utf8(&reason_bytes).map_err(|_| "conversation_reason_invalid")?;
    if reason.trim().is_empty() || reason.contains('\0') {
        return fail("reason_invalid");
    }
    let reason_sha = digest(&reason_bytes);
    let expected_bytes = bytes(expected_file)?;
    let expected: Value =
        serde_json::from_slice(&expected_bytes).map_err(|_| "conversation_state_invalid")?;
    let expected_sha = digest(&expected_bytes);
    let at = clock()?;
    validate(&question, id, false, at)?;
    check_config(paths, &question)?;
    let index = row_index(db, id)?;
    unique(db, id, &question, &sha, at)?;
    let row = &db["items"][index];
    for receipt in withdrawals(row)? {
        if receipt["wait"]["question"]["request_id"] == question["request_id"] {
            if receipt["wait"]["question"] != question
                || receipt["wait"]["question_receipt_sha256"] != sha
                || receipt["reason_sha256"] != reason_sha
                || receipt["expected_state_sha256"] != expected_sha
            {
                return fail("withdrawal_conflict");
            }
            return Ok(
                json!({"id":id,"request_id":question["request_id"],"question_receipt_sha256":sha,
                "reason_sha256":reason_sha,"changed":false,"withdrawn":true,"answered":false,"resumed":false}),
            );
        }
    }
    // Exact CAS includes note, wait, resume_generation, worker and scheduler.
    // Concurrent private deliveries are never overwritten by a stale snapshot.
    if *row != expected {
        return fail("state_changed");
    }
    let wait = row[WAIT_KEY].clone();
    validate_wait(&wait, id, at)?;
    if wait["question"] != question
        || wait["question_receipt_sha256"] != sha
        || row["status"] != "block"
    {
        return fail("block_changed");
    }
    let (owner, note, imported) = finished_question_owner(db, paths, row, &wait, id, at)?;
    if !row["scheduler"]["owner_instruction"].is_null()
        && !row["scheduler"]["owner_instruction"].is_string()
    {
        return fail("owner_record_invalid");
    }
    let origin = json!({"resume_generation":row["resume_generation"],"worker_receipt":row["worker_receipt"],
        "job":owner["id"],"launch_key":owner["launch_key"]});
    let instruction = format!(
        "{}\n元ToDo {id}の不要な再確認質問を取り下げました。本人回答は記録していません。既存の依頼と許可の範囲で元の作業を続け、受領と履行を分けて記録してください。\n理由: {reason}\n{note}",
        text(&row["scheduler"], "owner_instruction")
    );
    if instruction.len() as u64 > MAX_BYTES {
        return fail("file_too_large");
    }
    let applied_at = at.to_rfc3339_opts(SecondsFormat::Micros, true);
    let receipt = json!({"domain":WITHDRAWAL,"wait":wait,"reason":reason,"reason_sha256":reason_sha,
        "expected_state_sha256":expected_sha,"origin":origin,"applied_at":applied_at,
        "released_note":row["note"],"release_kind":if imported {"scheduler_block_result"} else {"question_marker"}});
    let mut saved = withdrawals(row)?.to_vec();
    saved.push(receipt);
    let row = &mut db["items"][index];
    row[WITHDRAWALS_KEY] = json!(saved);
    row.as_object_mut().unwrap().remove(WAIT_KEY);
    if imported && wait["previous_note_present"] == false {
        row.as_object_mut().unwrap().remove("note");
    } else if (imported || row["note"] == wait["block_note"])
        && wait["previous_note_present"] == true
    {
        row["note"] = wait["previous_note"].clone();
    } else if row["note"] != wait["block_note"] || !note.is_empty() {
        row["note"] = json!(note);
    } else {
        row.as_object_mut().unwrap().remove("note");
    }
    row["status"] = json!("todo");
    row["updated"] = json!(applied_at);
    cc_scheduler::common::record_result(row, &owner["id"], "conversation_withdrawal");
    cc_scheduler::common::resume_without_worker(row);
    row["scheduler"]["owner_instruction"] = json!(instruction);
    Ok(
        json!({"id":id,"request_id":question["request_id"],"question_receipt_sha256":sha,
        "reason_sha256":reason_sha,"changed":true,"withdrawn":true,"answered":false,"resumed":true}),
    )
}

/// Consume exactly once. Later retries cannot release a new stop or a completed task.
pub fn answer(
    db: &mut Value,
    paths: &Paths,
    id: i64,
    path: &Path,
    delivery: Option<(&Path, &Path)>,
) -> Result<Value> {
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
    if wait
        .get("origin")
        .is_some_and(|origin| *origin != origin_identity(row))
    {
        return fail("owner_changed_or_active");
    }
    let note = if let Some((note_file, expected_file)) = delivery {
        let expected: Value = serde_json::from_slice(&bytes(expected_file)?)
            .map_err(|_| "conversation_state_invalid")?;
        if *row != expected {
            return fail("state_changed");
        }
        let (_, previous, imported) = finished_question_owner(db, paths, row, &wait, id, at)?;
        if !imported {
            return fail("block_changed");
        }
        let data = bytes(note_file)?;
        let delivered = std::str::from_utf8(&data).map_err(|_| "conversation_note_invalid")?;
        let suffix = delivered
            .strip_prefix(&format!("{IMPORTED_BLOCK_NOTE}\n\n"))
            .filter(|s| !s.trim().is_empty() && !s.contains('\0'))
            .ok_or("conversation_note_invalid")?;
        let combined = format!("{previous}\n\n{suffix}");
        if combined.len() as u64 > MAX_BYTES {
            return fail("file_too_large");
        }
        combined
    } else {
        answered_note(row, &wait)?
    };
    clear(db, row, at)?;
    let owner = owner_record(paths, row)?;
    let retire = owner.as_ref().is_some_and(|job| {
        matches!(
            text(job, "status"),
            "succeeded" | "failed" | "lost" | "cancelled" | "canceled"
        ) && row["scheduler"]["job"] == job["id"]
            && (row["worker_receipt"]["job"].is_null() || row["worker_receipt"]["job"] == job["id"])
            && (job["todo_id"].is_null()
                || job["todo_id"] == id
                || job["related_todo_ids"]
                    .as_array()
                    .is_some_and(|ids| ids.contains(&row["id"])))
    });
    // A finished launch key is an immutable receipt for the old turn. Retire
    // only that verified current owner, preserving workspace and job history.
    // The replay branch above must never rotate a later worker or release a stop.
    let resume_instruction = if retire {
        if !row["scheduler"]["owner_instruction"].is_null()
            && !row["scheduler"]["owner_instruction"].is_string()
        {
            return fail("owner_record_invalid");
        }
        let instruction = format!(
            "{}\n元ToDo {id}の質問への本人回答を正規経路で適用しました。次の私的参照から回答原文と元質問・判断範囲を読み、元の履行を続けてください。同じ判断を再質問しないでください。\n{note}",
            text(&row["scheduler"], "owner_instruction")
        );
        if instruction.len() as u64 > MAX_BYTES {
            return fail("file_too_large");
        }
        Some(instruction)
    } else {
        None
    };
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
    if let Some(instruction) = resume_instruction {
        let job = owner.as_ref().unwrap()["id"].clone();
        cc_scheduler::common::record_result(row, &job, "conversation_answer");
        cc_scheduler::common::resume_without_worker(row);
        row["scheduler"]["owner_instruction"] = json!(instruction);
    }
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
