//! A direction is an append-only specification record, not an implementation receipt.
//! The dispatcher holds todo.lock across both guarded snapshots and the atomic save.
use crate::{Paths, Result, conversation_wait as bridge};
use chrono::{DateTime, Utc};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::path::Path;

const INPUT: &str = "discord-conversation-direction-v1";
const RECEIPT: &str = "todo-direction-record-v1";
const KIND: &str = "specification_direction";
const RECORDS: &str = "owner_direction_records";
const FIELDS: &[&str] = &[
    "domain",
    "kind",
    "source",
    "request_id",
    "sender_request_id",
    "origin_todo_id",
    "actor_todo_id",
    "job_id",
    "project",
    "revision",
    "thread_id",
    "question_message_id",
    "message_id",
    "author_id",
    "question_fingerprint",
    "question_receipt_sha256",
    "decision_identity_sha256",
    "reply_sha256",
    "source_receipt_sha256",
    "original_question",
    "question_created_at",
    "owner_reply",
    "created_at",
    "edited_at",
    "acquired_at",
];
fn fail<T>(s: &str) -> Result<T> {
    Err(format!("direction_{s}"))
}
fn text<'a>(v: &'a Value, k: &str) -> &'a str {
    v[k].as_str().unwrap_or("")
}
fn sha(b: &[u8]) -> String {
    format!("{:x}", Sha256::digest(b))
}
fn hex(s: &str, n: usize) -> bool {
    s.len() == n
        && s.bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn decimal(s: &str) -> bool {
    !s.starts_with('0')
        && s.parse::<u64>().is_ok_and(|v| v > 0)
        && s.bytes().all(|b| b.is_ascii_digit())
}
fn row(db: &Value, id: i64) -> Result<&Value> {
    let mut rows = db["items"]
        .as_array()
        .ok_or("direction_store_invalid")?
        .iter()
        .filter(|v| v["id"] == id);
    let selected = rows.next().ok_or("direction_task_unavailable")?;
    if rows.next().is_some() {
        return fail("duplicate_task");
    }
    Ok(selected)
}
fn control(row: &Value) -> Value {
    let mut v = row.clone();
    for k in ["note", "note_history", "updated", RECORDS] {
        v.as_object_mut().unwrap().remove(k);
    }
    v
}
// Exact identifiers in both persisted assignment and executed prompt are required;
// a project name, title similarity, historical job or generic audit is insufficient.
fn assigned(body: &str, input: &Value) -> bool {
    ["request_id", "question_message_id", "message_id"]
        .iter()
        .all(|k| {
            let id = text(input, k);
            body.match_indices(id).any(|(i, _)| {
                // Original assignments also use labels such as q123 / reply124.
                // Decimal IDs must not be a substring of a different numeric ID.
                let boundary = |c: char| {
                    if *k == "request_id" {
                        !c.is_ascii_alphanumeric() && c != '-' && c != '_'
                    } else {
                        !c.is_ascii_digit()
                    }
                };
                (i == 0 || body[..i].chars().next_back().is_some_and(boundary))
                    && (i + id.len() == body.len()
                        || body[i + id.len()..].chars().next().is_some_and(boundary))
            })
        })
}
fn actor_proof(paths: &Paths, actor: &Value, input: &Value, verify: bool) -> Result<Value> {
    let job_id = text(input, "job_id");
    if !crate::job_refs::valid_id(job_id)
        || !crate::job_refs::ids(&actor["jobs"]).any(|j| j == job_id)
    {
        return fail("actor_job_mismatch");
    }
    let job_path = paths.jobs.join(job_id).join("job.json");
    let job: Value =
        serde_json::from_slice(&bridge::bytes(&job_path)?).map_err(|_| "direction_job_invalid")?;
    if job["id"] != job_id
        || job["todo_id"] != actor["id"]
        || cc_job_policy::cancellation::blocks_recovery(actor, &job)
        || !matches!(
            text(&job, "status"),
            "running" | "succeeded" | "failed" | "lost"
        )
    {
        return fail("actor_job_mismatch");
    }
    if !verify
        && (std::env::var("CC_JOB_ID").ok().as_deref() != Some(job_id)
            || actor["status"] != "doing"
            || job["status"] != "running"
            || (actor["scheduler"]["job"] != job_id && actor["worker_receipt"]["job"] != job_id))
    {
        return fail("actor_not_current");
    }
    if ["owner_stopped", "paused", "owner_postponement_recorded"]
        .iter()
        .any(|k| !actor[*k].is_null() && actor[*k] != false)
        || (!actor["stop_kind"].is_null() && actor["stop_kind"] != "")
    {
        return fail("actor_stopped");
    }
    let cwd = text(&job, "cwd");
    let worktree = text(&actor["scheduler"], "worktree");
    if !Path::new(cwd).is_absolute()
        || worktree.is_empty()
        || crate::normalized(cwd) != crate::normalized(worktree)
    {
        return fail("actor_workspace_mismatch");
    }
    let spec = Path::new(text(actor, "spec"));
    if spec.as_os_str().is_empty() {
        return fail("assignment_unavailable");
    }
    let spec = if spec.is_absolute() {
        spec.to_path_buf()
    } else {
        paths.store.parent().unwrap().join(spec)
    };
    let spec_bytes = bridge::bytes(&spec)?;
    let prompt = bridge::bytes(&paths.jobs.join(job_id).join("prompt.md"))?;
    if !std::str::from_utf8(&spec_bytes).is_ok_and(|s| assigned(s, input))
        || !std::str::from_utf8(&prompt).is_ok_and(|s| assigned(s, input))
    {
        return fail("assignment_scope_mismatch");
    }
    Ok(json!({"spec_sha256":sha(&spec_bytes),"prompt_sha256":sha(&prompt),"cwd":cwd}))
}
fn validate(input: &Value) -> Result<(i64, i64)> {
    let obj = input.as_object().ok_or("direction_input_invalid")?;
    if obj.len() != FIELDS.len()
        || FIELDS
            .iter()
            .any(|k| !obj.contains_key(*k) || !input[*k].is_string())
        || input["domain"] != INPUT
        || input["kind"] != KIND
        || input["source"] != "discord-api-v10"
    {
        return fail("input_invalid");
    }
    for k in ["request_id", "sender_request_id"] {
        let s = text(input, k);
        if s.len() > 96 || !crate::job_refs::valid_id(s) {
            return fail("input_invalid");
        }
    }
    for k in [
        "origin_todo_id",
        "actor_todo_id",
        "thread_id",
        "question_message_id",
        "message_id",
        "author_id",
    ] {
        if !decimal(text(input, k)) {
            return fail("input_invalid");
        }
    }
    for k in [
        "question_fingerprint",
        "question_receipt_sha256",
        "decision_identity_sha256",
        "reply_sha256",
        "source_receipt_sha256",
    ] {
        if !hex(text(input, k), 64) {
            return fail("input_invalid");
        }
    }
    if input["thread_id"] != input["question_message_id"]
        || !hex(text(input, "revision"), 40)
        || text(input, "project").is_empty()
        || text(input, "project").eq_ignore_ascii_case("owner")
        || text(input, "original_question").trim().is_empty()
        || text(input, "owner_reply").trim().is_empty()
    {
        return fail("input_invalid");
    }
    let at = |k| DateTime::parse_from_rfc3339(text(input, k)).map_err(|_| "direction_time_invalid");
    let (q, c, a) = (
        at("question_created_at")?,
        at("created_at")?,
        at("acquired_at")?,
    );
    if c < q
        || a < c
        || a > Utc::now()
        || (!text(input, "edited_at").is_empty() && {
            let e = at("edited_at")?;
            e < c || e > a
        })
    {
        return fail("time_invalid");
    }
    Ok((
        text(input, "origin_todo_id")
            .parse()
            .map_err(|_| "direction_input_invalid")?,
        text(input, "actor_todo_id")
            .parse()
            .map_err(|_| "direction_input_invalid")?,
    ))
}
fn block(input: &Value, input_sha: &str) -> String {
    format!(
        "[discord-direction-record-v1 sha256={input_sha}]\n仕様へ記録、機能は未完了。画像案の実装承認・配布・元作業全体の完了ではありません。\n実担当: ToDo {} / job {}\n元作業・保存先: ToDo {}\n出所: Discord API v10 / author {} / thread {} / question {} / reply {}\n質問日時: {} / 回答日時: {} / 編集日時: {} / 取得日時: {}\n原質問（原文）:\n{}\n本人回答（原文）:\n{}\nsource_receipt_sha256={}\n[/discord-direction-record-v1 sha256={input_sha}]",
        text(input, "actor_todo_id"),
        text(input, "job_id"),
        text(input, "origin_todo_id"),
        text(input, "author_id"),
        text(input, "thread_id"),
        text(input, "question_message_id"),
        text(input, "message_id"),
        text(input, "question_created_at"),
        text(input, "created_at"),
        text(input, "edited_at"),
        text(input, "acquired_at"),
        text(input, "original_question"),
        text(input, "owner_reply"),
        text(input, "source_receipt_sha256")
    )
}
#[allow(clippy::too_many_arguments)]
pub fn record(
    db: &mut Value,
    paths: &Paths,
    input_file: &Path,
    origin_scope: &Path,
    origin_state: &Path,
    actor_scope: &Path,
    actor_state: &Path,
    verify: bool,
) -> Result<Value> {
    let bytes = bridge::bytes(input_file)?;
    let input: Value = serde_json::from_slice(&bytes).map_err(|_| "direction_input_invalid")?;
    let (origin_id, actor_id) = validate(&input)?;
    for (id, scope, state) in [
        (origin_id, origin_scope, origin_state),
        (actor_id, actor_scope, actor_state),
    ] {
        bridge::check_scope(db, id, scope)?;
        bridge::check_snapshot(db, id, state)?;
        if row(db, id)?["project"] != input["project"] {
            return fail("project_mismatch");
        }
    }
    let actor = actor_proof(paths, row(db, actor_id)?, &input, verify)?;
    let origin = row(db, origin_id)?;
    let input_sha = sha(&bytes);
    let append = block(&input, &input_sha);
    let records = match origin.get(RECORDS) {
        Some(v) => v.as_array().ok_or("direction_records_invalid")?.clone(),
        None => vec![],
    };
    let matched: Vec<_> = records
        .iter()
        .filter(|v| v["input_sha256"] == input_sha)
        .collect();
    let changed_record = matched.is_empty();
    if records.iter().any(|v| {
        v["input"]["request_id"] == input["request_id"]
            && v["input"]["message_id"] == input["message_id"]
            && v["input"]["source_receipt_sha256"] == input["source_receipt_sha256"]
            && v["input_sha256"] != input_sha
    }) {
        return fail("record_conflict");
    }
    let receipt = if matched.len() == 1 {
        let saved = matched[0];
        if saved["domain"] != RECEIPT
            || saved["kind"] != KIND
            || saved["input"] != input
            || saved["actor"] != actor
            || saved["origin_control"] != control(origin)
            || saved["note_block_sha256"] != sha(append.as_bytes())
            || text(origin, "note").matches(&append).count() != 1
        {
            return fail("record_mismatch");
        }
        saved.clone()
    } else {
        if verify
            || !matched.is_empty()
            || !matches!(text(origin, "status"), "todo" | "doing" | "wait" | "block")
        {
            return fail("record_unavailable");
        }
        let note = text(origin, "note");
        if !origin["note"].is_null() && !origin["note"].is_string() {
            return fail("note_invalid");
        }
        let next = format!("{note}\n\n{append}");
        if next.len() > 1024 * 1024 {
            return fail("note_limit");
        }
        let receipt = json!({"domain":RECEIPT,"kind":KIND,"input":input,"input_sha256":input_sha,
            "actor":actor,"origin_control":control(origin),"note_block_sha256":sha(append.as_bytes()),
            "recorded_at":Utc::now().to_rfc3339()});
        let mut changed = origin.clone();
        if changed.get("note_history").is_some_and(|v| !v.is_array()) {
            return fail("history_invalid");
        }
        let history = json!({"note":changed["note"],"updated":changed["updated"]});
        cc_scheduler::common::push(&mut changed["note_history"], history);
        changed["note"] = json!(next);
        changed["updated"] = json!(crate::now());
        changed[RECORDS] = json!(
            records
                .into_iter()
                .chain([receipt.clone()])
                .collect::<Vec<_>>()
        );
        *crate::find(db, origin_id)? = changed;
        receipt
    };
    Ok(
        json!({"domain":RECEIPT,"kind":KIND,"recorded":true,"changed":changed_record,
        "input_sha256":input_sha,"receipt_sha256":sha(&serde_json::to_vec(&receipt).unwrap())}),
    )
}
