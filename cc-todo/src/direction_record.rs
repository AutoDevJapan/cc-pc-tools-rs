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
#[derive(Default)]
pub struct Readback {
    pub verify: bool,
    pub note: bool,
    pub holds: bool,
}
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
const HOLD_FIELDS: &[&str] = &[
    "stopped",
    "cancel_requested",
    "owner_stopped",
    "paused",
    "owner_postponement_recorded",
    "stop_kind",
    "decision_wait",
    "owner_question_wait",
    "wait_ready",
    "resume_condition",
    "external_resume_condition",
];
fn holds(row: &Value) -> Value {
    let mut value = json!({"blocked":row["status"]=="block","block_generation":row["resume_generation"],
        "block_question":row["scheduler"]["question"],"block_note_sha256":sha(block_note(row).as_bytes())});
    for k in HOLD_FIELDS
        .iter()
        .chain(["wait_on", "work_owner_wait_on"].iter())
    {
        value[*k] = row[*k].clone();
    }
    for k in HOLD_FIELDS {
        value["scheduler_holds"][*k] = row["scheduler"][*k].clone();
    }
    value
}
fn block_note(row: &Value) -> String {
    let mut note = text(row, "note").to_string();
    // Two recordings in one original task must not reinterpret each other's
    // append as a new block. Remove only exact canonical direction additions.
    for key in [RECORDS, "owner_implementation_records"] {
        if let Some(records) = row[key].as_array() {
            for receipt in records.iter().rev() {
                let block = text(receipt, "note_block");
                if !block.is_empty() {
                    note = note.replacen(&format!("\n\n{block}"), "", 1);
                }
            }
        }
    }
    note
}
fn no_new_holds(saved: &Value, row: &Value) -> bool {
    if !saved.is_object()
        || row["status"] == "recovery_unverified"
        || (row["status"] == "block" && saved["blocked"] != true)
    {
        return false;
    }
    if row["status"] == "block"
        && (saved["block_generation"] != row["resume_generation"]
            || saved["block_question"] != row["scheduler"]["question"]
            || saved["block_note_sha256"] != sha(block_note(row).as_bytes()))
    {
        return false;
    }
    for k in HOLD_FIELDS {
        let current = &row[*k];
        if !current.is_null() && current != false && current != "" && current != &saved[*k] {
            return false;
        }
        let current = &row["scheduler"][*k];
        if !current.is_null()
            && current != false
            && current != ""
            && current != &saved["scheduler_holds"][*k]
        {
            return false;
        }
    }
    for k in ["wait_on", "work_owner_wait_on"] {
        if row[k].is_null() {
            continue;
        }
        // Work-owner dependencies use one numeric ID, unlike wait_on's array.
        if k == "work_owner_wait_on" && row[k].as_i64().is_some_and(|n| n > 0) {
            if row[k] != saved[k] {
                return false;
            }
            continue;
        }
        let Some(current) = row[k].as_array() else {
            return false;
        };
        if !current.iter().all(|v| {
            v.as_i64().is_some_and(|id| id > 0)
                && saved[k].as_array().is_some_and(|old| old.contains(v))
        }) {
            return false;
        }
    }
    true
}
fn recorded_scope(db: &Value, id: i64) -> Result<Value> {
    let item = row(db, id)?;
    let project = text(item, "project");
    let root = db["projects"][project]
        .as_str()
        .ok_or("direction_scope_unavailable")?;
    Ok(
        json!({"domain":"discord-conversation-todo-scope-v1","todo_id":id.to_string(),
        "project":project,"project_path_sha256":sha(root.as_bytes())}),
    )
}
// Exact identifiers in both persisted assignment and executed prompt are required;
// a project name, title similarity, historical job or generic audit is insufficient.
fn assigned(body: &str, input: &Value) -> bool {
    let keys = if input["kind"] == "implementation" {
        ["origin_todo_id", "question_message_id", "message_id"]
    } else {
        ["request_id", "question_message_id", "message_id"]
    };
    keys.iter().all(|k| {
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
fn assignment_matches(paths: &Paths, actor: &Value, body: &str, input: &Value) -> bool {
    assigned(body, input) || crate::ordinary_assignment::matches(paths, actor, input, body)
}
fn actor_identity(paths: &Paths, actor: &Value, input: &Value) -> Result<(Value, Vec<u8>)> {
    let job_id = text(input, "job_id");
    if !crate::job_refs::valid_id(job_id)
        || !crate::job_refs::ids(&actor["jobs"]).any(|j| j == job_id)
    {
        return fail("actor_job_mismatch");
    }
    let job_path = paths.jobs.join(job_id).join("job.json");
    let job: Value =
        serde_json::from_slice(&bridge::bytes(&job_path)?).map_err(|_| "direction_job_invalid")?;
    if job["id"] != job_id || job["todo_id"] != actor["id"] {
        return fail("actor_job_mismatch");
    }
    let prompt = bridge::bytes(&paths.jobs.join(job_id).join("prompt.md"))?;
    if !Path::new(text(&job, "cwd")).is_absolute()
        || !std::str::from_utf8(&prompt).is_ok_and(|s| assignment_matches(paths, actor, s, input))
    {
        return fail("actor_identity_invalid");
    }
    Ok((job, prompt))
}
fn actor_proof(paths: &Paths, actor: &Value, input: &Value) -> Result<Value> {
    let job_id = text(input, "job_id");
    let (job, prompt) = actor_identity(paths, actor, input)?;
    if std::env::var("CC_JOB_ID").ok().as_deref() != Some(job_id)
        || actor["status"] != "doing"
        || job["status"] != "running"
        || cc_job_policy::cancellation::blocks_recovery(actor, &job)
        || (actor["scheduler"]["job"] != job_id && actor["worker_receipt"]["job"] != job_id)
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
    if !std::str::from_utf8(&spec_bytes).is_ok_and(|s| assignment_matches(paths, actor, s, input))
        || !std::str::from_utf8(&prompt).is_ok_and(|s| assignment_matches(paths, actor, s, input))
    {
        return fail("assignment_scope_mismatch");
    }
    Ok(json!({"spec_sha256":sha(&spec_bytes),"prompt_sha256":sha(&prompt),"cwd":cwd}))
}
fn recorded_actor_matches(
    paths: &Paths,
    actor: &Value,
    input: &Value,
    saved: &Value,
) -> Result<bool> {
    let (job, prompt) = actor_identity(paths, actor, input)?;
    // A later assignment/spec edit or retirement does not undo an earlier actual
    // write. Its identity and executed prompt remain independently verifiable.
    Ok(saved.as_object().is_some_and(|o| o.len() == 3)
        && hex(text(saved, "spec_sha256"), 64)
        && saved["prompt_sha256"] == sha(&prompt)
        && saved["cwd"] == job["cwd"])
}
fn validate(input: &Value, implementation: bool) -> Result<(i64, i64)> {
    let obj = input.as_object().ok_or("direction_input_invalid")?;
    let extra = [
        "completion",
        "execution",
        "origin_saved",
        "identity",
        "applied_at",
    ];
    let ordinary = obj.contains_key("intake_source_file");
    let expected_fields =
        FIELDS.len() + if implementation { extra.len() } else { 0 } + if ordinary { 2 } else { 0 };
    if (ordinary && !implementation)
        || obj.len() != expected_fields
        || FIELDS
            .iter()
            .any(|k| !obj.contains_key(*k) || !input[*k].is_string())
        || input["domain"]
            != if implementation {
                "discord-conversation-implementation-v1"
            } else {
                INPUT
            }
        || input["kind"]
            != if implementation {
                "implementation"
            } else {
                KIND
            }
        || input["source"] != "discord-api-v10"
    {
        return fail("input_invalid");
    }
    if implementation && extra.iter().any(|k| text(input, k).trim().is_empty()) {
        return fail("execution_evidence_required");
    }
    if ordinary
        && (!Path::new(text(input, "intake_source_file")).is_absolute()
            || !hex(text(input, "intake_source_sha256"), 64))
    {
        return fail("ordinary_source_invalid");
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
    if implementation {
        let applied = at("applied_at")?;
        if applied < a || applied > Utc::now() {
            return fail("time_invalid");
        }
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
fn block(input: &Value, input_sha: &str, implementation: bool) -> String {
    // The source input retains the exact API bytes. Render the note with the
    // normal edit CLI's newline convention so later ordinary edits preserve it.
    let lines = |s: &str| s.replace("\r\n", "\n").replace('\r', "\n");
    let marker = if implementation {
        "implementation"
    } else {
        "direction"
    };
    let summary = if implementation {
        "別担当の成果を記録。元作業の取消・待ち・停止は保持。"
    } else {
        "仕様へ記録、機能は未完了。画像案の実装承認・配布・元作業全体の完了ではありません。"
    };
    let execution = if implementation {
        format!(
            "\n実反映: {}\n実行と検証: {}\n元作業の保持: {}\n本人・出所の照合: {}\nrevision={} / applied_at={}",
            text(input, "completion"),
            text(input, "execution"),
            text(input, "origin_saved"),
            text(input, "identity"),
            text(input, "revision"),
            text(input, "applied_at")
        )
    } else {
        String::new()
    };
    let actor_label = if implementation && input.get("intake_source_file").is_some() {
        "記録担当"
    } else {
        "実担当"
    };
    format!(
        "[discord-{marker}-record-v1 sha256={input_sha}]\n{summary}\n{actor_label}: ToDo {} / job {}\n元作業・保存先: ToDo {}\n出所: Discord API v10 / author {} / thread {} / question {} / reply {}\n質問日時: {} / 回答日時: {} / 編集日時: {} / 取得日時: {}\n原質問（原文）:\n{}\n本人回答（原文）:\n{}\nsource_receipt_sha256={}{execution}\n[/discord-{marker}-record-v1 sha256={input_sha}]",
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
        lines(text(input, "original_question")),
        lines(text(input, "owner_reply")),
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
    readback: Readback,
) -> Result<Value> {
    record_for(
        db,
        paths,
        input_file,
        origin_scope,
        origin_state,
        actor_scope,
        actor_state,
        readback,
        false,
    )
}

#[allow(clippy::too_many_arguments)]
pub fn record_implementation(
    db: &mut Value,
    paths: &Paths,
    input_file: &Path,
    origin_scope: &Path,
    origin_state: &Path,
    actor_scope: &Path,
    actor_state: &Path,
    readback: Readback,
) -> Result<Value> {
    record_for(
        db,
        paths,
        input_file,
        origin_scope,
        origin_state,
        actor_scope,
        actor_state,
        readback,
        true,
    )
}

#[allow(clippy::too_many_arguments)]
fn record_for(
    db: &mut Value,
    paths: &Paths,
    input_file: &Path,
    origin_scope: &Path,
    origin_state: &Path,
    actor_scope: &Path,
    actor_state: &Path,
    readback: Readback,
    implementation: bool,
) -> Result<Value> {
    let (receipt_domain, kind, records_key) = if implementation {
        (
            "todo-implementation-record-v1",
            "implementation",
            "owner_implementation_records",
        )
    } else {
        (RECEIPT, KIND, RECORDS)
    };
    let bytes = bridge::bytes(input_file)?;
    let input: Value = serde_json::from_slice(&bytes).map_err(|_| "direction_input_invalid")?;
    let (origin_id, actor_id) = validate(&input, implementation)?;
    if origin_id == actor_id {
        return fail("actor_must_be_distinct");
    }
    let ordinary = implementation && input.get("intake_source_file").is_some();
    if ordinary {
        crate::ordinary_assignment::qualify(paths, row(db, actor_id)?, &input)?;
    }
    for (id, scope, state) in [
        (origin_id, origin_scope, origin_state),
        (actor_id, actor_scope, actor_state),
    ] {
        bridge::check_scope(db, id, scope)?;
        bridge::check_snapshot(db, id, state)?;
        if row(db, id)?["project"] != input["project"] && !(ordinary && id == actor_id) {
            return fail("project_mismatch");
        }
    }
    let actor_scope = recorded_scope(db, actor_id)?;
    let origin_scope = recorded_scope(db, origin_id)?;
    let origin = row(db, origin_id)?;
    let input_sha = sha(&bytes);
    let append = block(&input, &input_sha, implementation);
    let records = match origin.get(records_key) {
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
        if saved["domain"] != receipt_domain
            || saved["kind"] != kind
            || saved["input"] != input
            || !recorded_actor_matches(paths, row(db, actor_id)?, &input, &saved["actor"])?
            || saved["actor_scope"] != actor_scope
            || saved["origin_scope"] != origin_scope
            || saved["note_block_sha256"] != sha(append.as_bytes())
            || saved["note_block"] != append
        {
            return fail("record_mismatch");
        }
        if readback.note && text(origin, "note").matches(&append).count() != 1 {
            return fail("note_readback_changed");
        }
        if readback.holds && !no_new_holds(&saved["origin_holds"], origin) {
            return fail("close_held");
        }
        saved.clone()
    } else {
        if readback.verify
            || !matched.is_empty()
            || !matches!(text(origin, "status"), "todo" | "doing" | "wait" | "block")
        {
            return fail("record_unavailable");
        }
        if implementation {
            let project = text(row(db, actor_id)?, "project");
            let root = std::fs::canonicalize(text(&db["projects"], project))
                .map_err(|_| "implementation_root_unavailable")?;
            let (job, _) = actor_identity(paths, row(db, actor_id)?, &input)?;
            let cwd = std::fs::canonicalize(text(&job, "cwd"))
                .map_err(|_| "implementation_workspace_unavailable")?;
            let root = crate::normalized(&root.to_string_lossy());
            let cwd = crate::normalized(&cwd.to_string_lossy());
            if cwd != root
                && !cwd.starts_with(&format!("{root}\\"))
                && !cwd.starts_with(&format!("{root}-wt\\"))
            {
                return fail("actor_project_root_mismatch");
            }
        }
        let actor = actor_proof(paths, row(db, actor_id)?, &input)?;
        let note = text(origin, "note");
        if !origin["note"].is_null() && !origin["note"].is_string() {
            return fail("note_invalid");
        }
        let next = format!("{note}\n\n{append}");
        if next.len() > 1024 * 1024 {
            return fail("note_limit");
        }
        let receipt = json!({"domain":receipt_domain,"kind":kind,"input":input,"input_sha256":input_sha,
            "actor":actor,"actor_scope":actor_scope,"origin_scope":origin_scope,
            "origin_holds":holds(origin),"note_block":append,"note_block_sha256":sha(append.as_bytes()),
            "note_after_sha256":sha(next.as_bytes()),
            "recorded_at":Utc::now().to_rfc3339()});
        let mut changed = origin.clone();
        if changed.get("note_history").is_some_and(|v| !v.is_array()) {
            return fail("history_invalid");
        }
        let history = json!({"note":changed["note"],"updated":changed["updated"]});
        cc_scheduler::common::push(&mut changed["note_history"], history);
        changed["note"] = json!(next);
        changed["updated"] = json!(crate::now());
        changed[records_key] = json!(
            records
                .into_iter()
                .chain([receipt.clone()])
                .collect::<Vec<_>>()
        );
        *crate::find(db, origin_id)? = changed;
        receipt
    };
    Ok(
        json!({"domain":receipt_domain,"kind":kind,"recorded":true,"changed":changed_record,
        "input_sha256":input_sha,"receipt_sha256":sha(&serde_json::to_vec(&receipt).unwrap())}),
    )
}
