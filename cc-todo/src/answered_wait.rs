//! Explicit, finite migration of the two API-verified legacy Console holds.
//! This does not consume/forge a conversation answer or declare UI work applied.
use crate::{Paths, Result, conversation_wait::bytes};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{collections::BTreeSet, path::Path};

const DOMAIN: &str = "todo-answered-dependency-wait-v1";
const RECEIPT: &str = "answered_dependency_wait_receipt";
const OWNER: &str = "730822776147148835";
const REQUEST: &str = "conversation-v1-5266c54d760e3cf975c7401aaa439b0b";
const THREAD: &str = "1561951095847260160";
const MESSAGE: &str = "1561951289468911707";
const DIRECTIVE: &str =
    "元3050/3070/3076の未完機能と配布は3084で継続。古い候補を配布せず、既答の再質問をしません。";

fn fail<T>(code: &str) -> Result<T> {
    Err(format!("answered_wait_{code}"))
}
fn sha(b: &[u8]) -> String {
    format!("{:x}", Sha256::digest(b))
}
fn text<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
fn hash(s: &str) -> bool {
    s.len() == 64
        && s.bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn shape(v: &Value, fields: &[&str]) -> Result<()> {
    let o = v.as_object().ok_or("answered_wait_schema_invalid")?;
    if o.len() != fields.len() || fields.iter().any(|f| !o.contains_key(*f)) {
        return fail("schema_invalid");
    }
    Ok(())
}
// Value discards duplicate keys. Every duplicate removes at least one structural
// colon on reserialization, even when the overwritten value is a nested object.
fn colons(b: &[u8]) -> usize {
    let (mut quoted, mut escaped, mut count) = (false, false, 0);
    for b in b {
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
            count += 1;
        }
    }
    count
}
fn parse(b: &[u8]) -> Result<Value> {
    let v: Value = serde_json::from_slice(b).map_err(|_| "answered_wait_schema_invalid")?;
    if colons(b) != colons(&serde_json::to_vec(&v).map_err(|_| "answered_wait_schema_invalid")?) {
        return fail("duplicate_keys");
    }
    Ok(v)
}
fn source(v: &Value) -> Result<Vec<u8>> {
    shape(v, &["path", "sha256"])?;
    if !hash(text(v, "sha256")) || text(v, "path").is_empty() {
        return fail("source_invalid");
    }
    let b = bytes(Path::new(text(v, "path")))?;
    if sha(&b) != v["sha256"] {
        return fail("source_changed");
    }
    Ok(b)
}
fn row(db: &Value, id: i64) -> Result<&Value> {
    let mut matches = db["items"]
        .as_array()
        .ok_or("answered_wait_store_invalid")?
        .iter()
        .filter(|r| r["id"] == id);
    let r = matches.next().ok_or("answered_wait_item_missing")?;
    if matches.next().is_some()
        || r["project"] != "cc-pc-tools-rs"
        || db["projects"]["cc-pc-tools-rs"]
            .as_str()
            .is_none_or(|s| s.is_empty())
    {
        return fail("scope_mismatch");
    }
    Ok(r)
}
fn timestamp(s: &str) -> Result<chrono::DateTime<chrono::Utc>> {
    let t = chrono::DateTime::parse_from_rfc3339(s)
        .map_err(|_| "answered_wait_timestamp_invalid")?
        .with_timezone(&chrono::Utc);
    if t.timestamp() <= 0 || t.timestamp() as f64 > crate::time_s() {
        return fail("timestamp_invalid");
    }
    Ok(t)
}
fn proof(e: &Value, id: i64) -> Result<(Value, String)> {
    shape(
        e,
        &[
            "domain",
            "todo_id",
            "wait_on",
            "expected_note",
            "legacy_note",
            "api_reply",
            "current_instruction",
        ],
    )?;
    if ![3050, 3070].contains(&id)
        || e["domain"] != DOMAIN
        || e["todo_id"] != id
        || text(e, "expected_note").trim().is_empty()
    {
        return fail("scope_mismatch");
    }
    let i = &e["current_instruction"];
    shape(i, &["source", "text", "sha256", "scope"])?;
    if i["source"] != "scheduler-user-task:3126"
        || text(i, "text").trim().is_empty()
        || text(i, "text").contains('\0')
        || i["sha256"] != sha(text(i, "text").as_bytes())
        || i["scope"]
            != json!({"todo_ids":[3050,3070],"dependency_id":3084,"retain_current_layout":true,"deploy_old_candidates":false})
    {
        return fail("instruction_mismatch");
    }
    let q = parse(&source(&e["api_reply"])?)?;
    let answers = q["owner_replies"]
        .as_array()
        .ok_or("answered_wait_answer_invalid")?;
    if answers.len() != 1 {
        return fail("answer_invalid");
    }
    let a = &answers[0];
    let identity = &q["decision_identity"];
    if q["domain"] != "new-owner-recovery-messages"
        || q["read_only"] != true
        || q["ledger_written"] != false
        || q["source"] != "discord-api-v10"
        || q["owner_id"] != OWNER
        || q["request_id"] != REQUEST
        || q["thread_id"] != THREAD
        || q["question_message_id"] != THREAD
        || text(&q, "original_question").trim().is_empty()
        || identity["todo_id"] != 3050
        || identity["owner"] != OWNER
        || identity["guild"] != "1552944707464597524"
        || identity["decision"] != "direction"
        || identity["scope"]["subject"] != "console-first-view"
        || !identity["resources"].as_array().is_some_and(|r| {
            r.iter().any(|r| {
                r["kind"] == "repository"
                    && r["id"]
                        == "https://github.com/AutoDevJapan/cc-pc-tools-rs/tree/main/localai-web-rs"
            })
        })
        || a["request_id"] != REQUEST
        || a["todo_id"] != 3050
        || a["question_thread_id"] != THREAD
        || a["question_message_id"] != THREAD
        || a["message_id"] != MESSAGE
        || a["author_id"] != OWNER
        || a["decision_identity"] != *identity
        || text(a, "body").trim().is_empty()
        || timestamp(text(a, "timestamp"))? < timestamp(text(&q, "question_timestamp"))?
    {
        return fail("answer_mismatch");
    }
    let old = source(&e["legacy_note"])?;
    let old = std::str::from_utf8(&old)
        .map_err(|_| "answered_wait_legacy_note_invalid")?
        .trim_end_matches(['\r', '\n']);
    let (heading, note) = old
        .split_once(" — ")
        .ok_or("answered_wait_legacy_note_invalid")?;
    if !heading.starts_with(&format!("{id} block cc-pc-tools-rs "))
        || !note.starts_with("本人の画像付きUI確認への回答待ち。")
    {
        return fail("legacy_note_invalid");
    }
    // `todo list` was captured with Windows console CRLF. The source SHA above
    // binds the original transport bytes; compare the note's logical lines.
    Ok((q, note.replace("\r\n", "\n")))
}

fn production(paths: &Paths) -> bool {
    let real = Paths::production().store;
    crate::normalized(&paths.store.to_string_lossy()) == crate::normalized(&real.to_string_lossy())
        || std::fs::canonicalize(&paths.store)
            .ok()
            .zip(std::fs::canonicalize(real).ok())
            .is_some_and(|(a, b)| a == b)
}
fn frozen(id: i64) -> Result<(&'static str, &'static str)> {
    match id {
        3050 => Ok((
            "db25fd71aad05c4500b7d6f5c7cdf46ea653a534b3de18077da6c2eca76a817d",
            "d47fde1b2329e0760396611dd55512094db115bbe46773ebd605bed221f56ce2",
        )),
        3070 => Ok((
            "caeace0713a7e89981b577151edd50ba60e9d17233728ecff71e155fe64b274d",
            "41aa9e437ebb335f130f7d0e06f74e62c0221ffe5432d53b31247dd63ca81c73",
        )),
        _ => fail("scope_mismatch"),
    }
}
fn state_sha(row: &Value) -> Result<String> {
    Ok(sha(
        &serde_json::to_vec(row).map_err(|_| "answered_wait_state_invalid")?
    ))
}
fn check_frozen_state(row: &Value, expected: &str) -> Result<()> {
    let actual = if row[RECEIPT].is_null() {
        state_sha(row)?
    } else {
        text(&row[RECEIPT], "state_sha256").to_owned()
    };
    if actual != expected {
        return fail("production_state_changed");
    }
    Ok(())
}
fn production_scope(db: &Value, paths: &Paths, id: i64, e: &Value) -> Result<()> {
    if !production(paths) {
        return Ok(());
    }
    // The operator's manifest is not a new authentication mechanism. Production
    // accepts only the exact original artifacts already verified for this task.
    let legacy = if id == 3050 {
        "eb924dedb030f6b2eec446c908ffb62efeff7e21ee354112a8636ca920c21294"
    } else {
        "71e2a86d67749c346560e8c3d4ae19e8746478347fe9d91abf775392d9628b49"
    };
    if e["legacy_note"]["sha256"] != legacy
        || e["api_reply"]["sha256"]
            != "d161c2c79dbc52e145fb5d4829d5cdbf808bbf4e74e603e77ffbbc2c1f3c9834"
        || e["current_instruction"]["text"] != DIRECTIVE
        || sha(text(e, "expected_note").as_bytes()) != frozen(id)?.0
        || crate::normalized(&paths.jobs.to_string_lossy())
            != crate::normalized(&Paths::production().jobs.to_string_lossy())
    {
        return fail("production_evidence_mismatch");
    }
    let guard = cc_ask::core::run_hidden_json(
        &[
            "D:/CC/Tools/client-records/scrub391.exe".into(),
            "--guard-path".into(),
            text(&db["projects"], "cc-pc-tools-rs").into(),
        ],
        10,
    )
    .map_err(|_| "answered_wait_guard_unavailable")?;
    if guard["ok"] != true
        || guard["client_resource"] != false
        || guard["classification_unverified"] != false
    {
        return fail("scope_protected");
    }
    Ok(())
}

// Unknown row/scheduler fields may be protections. Fail closed rather than
// silently changing the meaning of a block introduced by a newer tool.
fn known(v: &Value, fields: &[&str]) -> Result<()> {
    if v.is_null() {
        return Ok(());
    }
    let o = v.as_object().ok_or("answered_wait_other_hold")?;
    let unknown: Vec<_> = o
        .keys()
        .filter(|k| !fields.contains(&k.as_str()))
        .cloned()
        .collect();
    if !unknown.is_empty() {
        // Names only, never private field values. Useful when a future schema is reviewed.
        return Err(format!("answered_wait_unknown_field:{}", unknown.join(",")));
    }
    Ok(())
}
fn clear(r: &Value) -> Result<()> {
    known(
        r,
        &[
            "id",
            "project",
            "title",
            "status",
            "note",
            "updated",
            "created",
            "created_s",
            "jobs",
            "spec",
            "spec_history",
            "dispatchable",
            "needs",
            "model",
            "effort",
            "progress",
            "note_history",
            "note_history_archives",
            "wait_on",
            "scheduler",
            "stall_recovery",
            "resume_generation",
            "dependency_generation",
            "worker_receipt",
            "processed_results",
            "owner_question_answer_receipts",
            "owner_question_withdrawal_receipts",
            RECEIPT,
        ],
    )?;
    known(
        &r["scheduler"],
        &[
            "job",
            "key",
            "phase",
            "worktree",
            "branch",
            "not_before",
            "resume",
            "selection",
            "owner_instruction",
            "last_error",
            "attempts",
            // Persisted preparation/checkpoint metadata; none is a hold.
            "started_s",
            "attempt",
            "workspace_checkpoint",
            "prompt",
            "resume_session",
            "selection_input",
            "workspace_receipt",
            "last_job",
        ],
    )?;
    known(
        &r["stall_recovery"],
        &[
            "history",
            "next_at",
            "since",
            "recent_continuations",
            "last_job",
            "last_finished_at",
            "last_kind",
        ],
    )?;
    known(&r["worker_receipt"], &["job", "generation"])?;
    if !r["scheduler"]["owner_instruction"].is_null()
        && !r["scheduler"]["owner_instruction"].is_string()
    {
        return fail("other_hold");
    }
    for t in [
        &r["scheduler"]["not_before"],
        &r["stall_recovery"]["next_at"],
    ] {
        if !t.is_null()
            && !t
                .as_f64()
                .is_some_and(|n| n.is_finite() && n >= 0. && n <= crate::time_s())
        {
            return fail("other_hold");
        }
    }
    Ok(())
}
fn dependencies(db: &Value, r: &Value, e: &Value) -> Result<()> {
    let on = r["wait_on"]
        .as_array()
        .ok_or("answered_wait_dependencies_changed")?;
    if on.is_empty() || r["wait_on"] != e["wait_on"] || !on.contains(&json!(3084)) {
        return fail("dependencies_changed");
    }
    let mut ids = BTreeSet::new();
    for id in on {
        let id = id
            .as_i64()
            .filter(|id| *id > 0)
            .ok_or("answered_wait_dependencies_invalid")?;
        if !ids.insert(id) {
            return fail("dependencies_invalid");
        }
        row(db, id)?; // exact existence, project and uniqueness, not first-match success
    }
    cc_job_policy::todo_wait::validate(
        db,
        r["id"].as_i64().unwrap(),
        &ids.iter().copied().collect::<Vec<_>>(),
    )?;
    if cc_job_policy::todo_wait::pending_on(db, r).is_empty() {
        return fail("dependencies_already_done");
    }
    Ok(())
}
// Read-only query. Failure to open/query is unknown, never proof of exit.
// Birth comparison distinguishes PID reuse from the actual original worker.
fn process_birth(pid: u32) -> Result<Option<u64>> {
    use windows_sys::Win32::{Foundation::*, System::Threading::*};
    if pid == 0 {
        return fail("owner_process_unavailable");
    }
    unsafe {
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if handle.is_null() {
            return if std::io::Error::last_os_error().raw_os_error()
                == Some(ERROR_INVALID_PARAMETER as i32)
            {
                Ok(None)
            } else {
                fail("owner_process_unavailable")
            };
        }
        struct Handle(HANDLE);
        impl Drop for Handle {
            fn drop(&mut self) {
                unsafe {
                    CloseHandle(self.0);
                }
            }
        }
        let h = Handle(handle);
        let mut times = [FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        }; 4];
        if GetProcessTimes(
            h.0,
            &mut times[0],
            &mut times[1],
            &mut times[2],
            &mut times[3],
        ) == 0
        {
            return fail("owner_process_unavailable");
        }
        let ticks = |t: FILETIME| ((t.dwHighDateTime as u64) << 32) | u64::from(t.dwLowDateTime);
        // Exit time is undefined until exit. A matching creation time is a hold
        // even if the original process exited but another handle retains it.
        // https://learn.microsoft.com/windows/win32/api/processthreadsapi/nf-processthreadsapi-getprocesstimes
        Ok(Some(ticks(times[0])))
    }
}
fn finished_owner(paths: &Paths, r: &Value) -> Result<()> {
    let scheduler_job = r["scheduler"]["job"].as_str().filter(|s| !s.is_empty());
    let receipt_job = r["worker_receipt"]["job"]
        .as_str()
        .filter(|s| !s.is_empty());
    if receipt_job.is_some() && receipt_job != scheduler_job {
        return fail("owner_changed");
    }
    if scheduler_job.is_none() && r["jobs"].as_array().is_some_and(Vec::is_empty) {
        if receipt_job.is_some()
            || !r["scheduler"]["key"].is_null()
            || !r["scheduler"]["last_job"].is_null()
            || !matches!(
                text(&r["scheduler"], "phase"),
                "" | "finished" | "waiting" | "resumed"
            )
        {
            return fail("owner_changed");
        }
        return Ok(());
    }
    let retired = scheduler_job.is_none();
    let id = scheduler_job
        .or_else(|| r["scheduler"]["last_job"].as_str())
        .ok_or("answered_wait_owner_unverified")?;
    if !crate::job_refs::valid_id(id) {
        return fail("owner_unverified");
    }
    if retired {
        let result = &r["processed_results"][id];
        if !r["scheduler"]["job"].is_null()
            || !r["worker_receipt"]["job"].is_null()
            || r["scheduler"]["phase"] != "resumed"
            || !r["resume_generation"].is_u64()
            || r["worker_receipt"]["generation"] != r["resume_generation"]
            || result["generation"]
                .as_u64()
                .is_none_or(|n| n >= r["resume_generation"].as_u64().unwrap())
            || !matches!(
                text(result, "reason"),
                "conversation_answer" | "conversation_withdrawal" | "superseded" | "block"
            )
        {
            return fail("retired_owner_unverified");
        }
    }
    let j = parse(&bytes(&paths.jobs.join(id).join("job.json"))?)?;
    if j["id"] != id
        || !(j["todo_id"] == r["id"]
            || j["related_todo_ids"]
                .as_array()
                .is_some_and(|a| a.contains(&r["id"])))
        || r["jobs"].as_array().and_then(|a| a.last()) != Some(&j["id"])
    {
        return fail("owner_identity_unverified");
    }
    if !matches!(text(&j, "status"), "succeeded" | "failed" | "lost") {
        return fail("owner_not_finished");
    }
    if cc_job_policy::cancellation::blocks_recovery(r, &j) {
        return fail("owner_stopped");
    }
    if !retired {
        if !matches!(text(&r["scheduler"], "phase"), "finished" | "running") {
            return fail("owner_phase_not_finished");
        }
        // Canonical `start --job` clears the scheduler launch key and binds the
        // worker receipt instead. Preserve that real assignment, never invent a key.
        let manual_assignment = r["scheduler"]["key"].is_null()
            && r["worker_receipt"]["job"] == j["id"]
            && r["resume_generation"].is_u64()
            && r["worker_receipt"]["generation"] == r["resume_generation"]
            && j["todo_id"] == r["id"];
        if r["scheduler"]["key"] != j["launch_key"] && !manual_assignment {
            return fail("owner_key_unverified");
        }
    }
    for key in ["pid", "codex_pid"] {
        if j[key].is_null() {
            continue;
        }
        let pid = j[key]
            .as_u64()
            .and_then(|p| u32::try_from(p).ok())
            .filter(|p| *p > 0)
            .ok_or("answered_wait_owner_process_unavailable")?;
        if let Some(created) = process_birth(pid)? {
            let original = j[format!("{key}_created")]
                .as_u64()
                .filter(|b| *b > 0)
                .ok_or("answered_wait_owner_process_unavailable")?;
            if original == created {
                return fail("owner_process_present");
            }
        }
    }
    Ok(())
}

/// Called only by the normal dispatcher, with todo.lock held through persistence.
pub fn transition(
    db: &mut Value,
    paths: &Paths,
    id: i64,
    file: &Path,
    apply: bool,
    expected: Option<&str>,
) -> Result<Value> {
    let evidence = bytes(file)?;
    let evidence_sha = sha(&evidence);
    let e = parse(&evidence)?;
    production_scope(db, paths, id, &e)?;
    let (api, old_note) = proof(&e, id)?;
    let r = row(db, id)?;
    if production(paths) {
        check_frozen_state(r, frozen(id)?.1)?;
    }
    if !r[RECEIPT].is_null() {
        let saved = &r[RECEIPT];
        if saved["domain"] != DOMAIN
            || saved["evidence_sha256"] != evidence_sha
            || r["status"] != "wait"
            || saved["original_wait_on"] != r["wait_on"]
            || r["note"] != e["expected_note"]
            || expected.is_some_and(|s| saved["state_sha256"] != s)
        {
            return fail("replay_conflict");
        }
        clear(r)?;
        dependencies(db, r, &e)?;
        return Ok(
            json!({"id":id,"changed":false,"state_sha256":saved["state_sha256"],"status":"wait","wait_on":r["wait_on"]}),
        );
    }
    if r["status"] != "block" || r["note"] != e["expected_note"] {
        return fail("state_changed");
    }
    clear(r)?;
    dependencies(db, r, &e)?;
    finished_owner(paths, r)?;
    let history = crate::todo_history::page(&paths.store, r, 0, 1000)?;
    if history["next_offset"] != Value::Null
        || !(r["note"] == old_note
            || history["entries"]
                .as_array()
                .is_some_and(|h| h.iter().any(|h| h["note"] == old_note)))
    {
        return fail("legacy_history_mismatch");
    }
    let state_sha = state_sha(r)?;
    if apply && expected.is_none_or(|s| !hash(s) || s != state_sha) {
        return fail("state_changed");
    }
    let result = json!({"id":id,"changed":apply,"state_sha256":state_sha,"status":if apply {"wait"} else {"block"},"wait_on":r["wait_on"]});
    if apply {
        let previous_instruction = text(&r["scheduler"], "owner_instruction");
        let delivered = format!(
            "{previous_instruction}\n\n[answered-dependency-wait:3126 todo={id}]\n現在の指示（出所: {}）: {}\n正規Discord APIで照合した本人回答: {}\n元質問: thread/question={}、request={}、本人ID={}、reply={}。\n原質問・回答の資料: {}（SHA256={}）。旧本人待ちの資料: {}（SHA256={}）。\n3084完了後はその実装・配布の成果を元の受入条件と照合してください。同じ画像案を再質問せず、旧候補を配布しないでください。この記録は待機状態と指示配送の移行であり、機能修復・本番反映・配布完了を主張しません。",
            text(&e["current_instruction"], "source"),
            text(&e["current_instruction"], "text"),
            text(&api["owner_replies"][0], "body"),
            THREAD,
            REQUEST,
            OWNER,
            MESSAGE,
            text(&e["api_reply"], "path"),
            text(&e["api_reply"], "sha256"),
            text(&e["legacy_note"], "path"),
            text(&e["legacy_note"], "sha256")
        );
        if delivered.len() > 1024 * 1024 {
            return fail("instruction_too_large");
        }
        let at = chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Micros, true);
        let receipt = json!({"domain":DOMAIN,"state_sha256":state_sha,"evidence_sha256":evidence_sha,"original_status":r["status"],
            "original_wait_on":r["wait_on"],"legacy_note":e["legacy_note"],"legacy_note_text":old_note,"api_reply_source":e["api_reply"],
            "api_reply":api,"current_instruction":e["current_instruction"],"delivered_instruction_sha256":sha(delivered.as_bytes()),"transitioned_at":at});
        let r = db["items"]
            .as_array_mut()
            .unwrap()
            .iter_mut()
            .find(|r| r["id"] == id)
            .unwrap();
        r["status"] = json!("wait");
        r["updated"] = json!(at);
        r["scheduler"]["owner_instruction"] = json!(delivered);
        r[RECEIPT] = receipt;
    }
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn nested_duplicate_keys_cannot_be_hidden_by_value_or_json_strings() {
        assert!(parse(br#"{"a":{"id":1,"id":2}}"#).is_err());
        assert!(parse(br#"{"a":"quoted : \\\" :", "b":[{"x":1}]}"#).is_ok());
    }
    #[test]
    fn production_rejects_unpinned_operator_artifacts_before_any_guard_or_source_read() {
        assert!(
            production_scope(
                &json!({}),
                &Paths::production(),
                3050,
                &json!({"api_reply":{"sha256":"f".repeat(64)}})
            )
            .is_err()
        );
    }
    #[test]
    fn production_rejects_new_owner_note_before_running_the_guard() {
        for (id, legacy) in [
            (
                3050,
                "eb924dedb030f6b2eec446c908ffb62efeff7e21ee354112a8636ca920c21294",
            ),
            (
                3070,
                "71e2a86d67749c346560e8c3d4ae19e8746478347fe9d91abf775392d9628b49",
            ),
        ] {
            let e = json!({"legacy_note":{"sha256":legacy},"api_reply":{"sha256":"d161c2c79dbc52e145fb5d4829d5cdbf808bbf4e74e603e77ffbbc2c1f3c9834"},
                "current_instruction":{"text":DIRECTIVE},"expected_note":"a new unrelated owner stop"});
            assert_eq!(
                production_scope(&json!({}), &Paths::production(), id, &e).unwrap_err(),
                "answered_wait_production_evidence_mismatch"
            );
        }
    }
    #[test]
    fn refreshed_cas_never_requalifies_a_later_owner_block_or_changed_original_dependency() {
        let original =
            json!({"id":3050,"status":"block","note":"verified original note","wait_on":[3084]});
        let pinned = state_sha(&original).unwrap();
        assert!(check_frozen_state(&original, &pinned).is_ok());
        for key in ["note", "wait_on", "resume_generation"] {
            let mut later = original.clone();
            later[key] = match key {
                "note" => json!("a later owner stop"),
                "wait_on" => json!([999]),
                _ => json!(42),
            };
            let refreshed = state_sha(&later).unwrap();
            assert_ne!(pinned, refreshed);
            assert!(check_frozen_state(&later, &pinned).is_err());
        }
    }
    #[test]
    fn query_confirms_this_process_without_assuming_an_unknown_pid_is_gone() {
        assert!(process_birth(std::process::id()).unwrap().is_some());
        assert!(process_birth(0).is_err());
    }
}
