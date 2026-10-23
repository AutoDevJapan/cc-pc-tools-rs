//! Delivery is independent of releasing a wait or applying the owner's decision.
use super::*;

const DELIVERIES: &str = "owner_question_delivery_receipts";
const DELIVERY: &str = "todo-owner-question-delivery-receipt-v1";

fn records(row: &Value) -> Result<&[Value]> {
    if row[DELIVERIES].is_null() {
        return Ok(&[]);
    }
    row[DELIVERIES]
        .as_array()
        .map(Vec::as_slice)
        .ok_or_else(|| "conversation_delivery_receipts_invalid".into())
}

fn generations(wait: &Value, recipient: &Value) -> Result<()> {
    let original = wait["origin"]["resume_generation"].as_u64();
    let current = recipient["resume_generation"].as_u64();
    if original.zip(current).is_none_or(|(a, b)| b <= a) {
        return fail("delivery_generation_mismatch");
    }
    Ok(())
}

fn validate_record(record: &Value, id: i64, at: DateTime<Utc>) -> Result<()> {
    shape(
        record,
        &[
            "domain",
            "wait",
            "answer",
            "answer_file_sha256",
            "recipient",
            "expected_state_sha256",
            "note_sha256",
            "instruction_sha256",
            "delivered_at",
        ],
    )?;
    validate_wait(&record["wait"], id, at)?;
    validate(&record["answer"], id, true, at)?;
    matches_wait(&record["wait"], &record["answer"], at)?;
    shape(
        &record["recipient"],
        &["resume_generation", "worker_receipt", "job", "key"],
    )?;
    generations(&record["wait"], &record["recipient"])?;
    if record["domain"] != DELIVERY
        || [
            "answer_file_sha256",
            "expected_state_sha256",
            "note_sha256",
            "instruction_sha256",
        ]
        .iter()
        .any(|k| !hash(text(record, k)))
        || timestamp(record, "delivered_at", at)? < timestamp(&record["answer"], "acquired_at", at)?
    {
        return fail("delivery_receipt_invalid");
    }
    Ok(())
}

fn result(id: i64, wait: &Value, changed: bool) -> Value {
    let mut value = metadata(
        id,
        &wait["question"],
        text(wait, "question_receipt_sha256"),
        changed,
        false,
        false,
    );
    value["delivered"] = json!(true);
    value
}

/// The dispatcher holds todo.lock across the original-row CAS, append and receipt.
/// Never change a generation, owner, status, hold or the original wait here.
pub fn deliver(
    db: &mut Value,
    paths: &Paths,
    id: i64,
    answer_file: &Path,
    note_file: &Path,
    expected_file: &Path,
) -> Result<Value> {
    let (answer, sha) = read(answer_file, ANSWER_FIELDS)?;
    let at = clock()?;
    validate(&answer, id, true, at)?;
    let index = row_index(db, id)?;
    let row = &db["items"][index];
    for record in records(row)? {
        validate_record(record, id, at)?;
        // The source SHA distinguishes even text-preserving edits. An exact
        // replay confirms only the old delivery, regardless of later stops.
        if record["answer"]["request_id"] == answer["request_id"]
            && record["answer"]["message_id"] == answer["message_id"]
            && record["answer"]["reply_sha256"] == answer["reply_sha256"]
            && record["answer"]["receipt_file_sha256"] == answer["receipt_file_sha256"]
        {
            if record["answer"] != answer || record["answer_file_sha256"] != sha {
                return fail("answer_conflict");
            }
            check_config(paths, &record["wait"]["question"])?;
            return Ok(result(id, &record["wait"], false));
        }
    }
    let expected_bytes = snapshot_bytes(expected_file)?;
    let expected: Value =
        serde_json::from_slice(&expected_bytes).map_err(|_| "conversation_state_invalid")?;
    if *row != expected {
        return fail("state_changed");
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
    if !matches!(text(row, "status"), "todo" | "doing") {
        return fail("status_not_eligible");
    }
    let recipient = origin_identity(row);
    generations(&wait, &recipient)?;
    // Delivery never releases a clock-based backoff. Preserve it while making
    // the answer reachable before the next worker starts.
    holds_clear(db, row)?;
    owner_clear(paths, row)?;
    let previous = row["note"].as_str().ok_or("conversation_note_invalid")?;
    let data = bytes(note_file)?;
    let note = std::str::from_utf8(&data).map_err(|_| "conversation_note_invalid")?;
    let suffix = note
        .strip_prefix(previous)
        .and_then(|s| s.strip_prefix("\n\n"))
        .filter(|s| !s.trim().is_empty() && !s.contains('\0'))
        .ok_or("conversation_note_invalid")?;
    if suffix.contains("[discord-conversation-question-v1 ") {
        return fail("note_invalid");
    }
    let instruction = match &row["scheduler"]["owner_instruction"] {
        Value::Null => suffix.to_owned(),
        Value::String(previous) => format!("{previous}\n\n{suffix}"),
        _ => return fail("note_invalid"),
    };
    if instruction.len() as u64 > MAX_BYTES {
        return fail("file_too_large");
    }
    let delivered_at = at.to_rfc3339_opts(SecondsFormat::Micros, true);
    let record = json!({"domain":DELIVERY, "wait":wait, "answer":answer,
        "answer_file_sha256":sha, "recipient":recipient, "expected_state_sha256":digest(&expected_bytes),
        "note_sha256":digest(&data), "instruction_sha256":digest(instruction.as_bytes()), "delivered_at":delivered_at});
    let mut saved = records(row)?.to_vec();
    saved.push(record);
    let row = &mut db["items"][index];
    row[DELIVERIES] = json!(saved);
    row["note"] = json!(note);
    row["scheduler"]["owner_instruction"] = json!(instruction);
    row["updated"] = json!(delivered_at);
    Ok(result(id, &wait, true))
}
