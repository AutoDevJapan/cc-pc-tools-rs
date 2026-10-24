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
    // A delivery to an already answered question carries one extra marker. Older
    // records keep their exact shape and the strict generation rule.
    let after_answer = record.get("after_answer").is_some();
    let mut fields = vec![
        "domain",
        "wait",
        "answer",
        "answer_file_sha256",
        "recipient",
        "expected_state_sha256",
        "note_sha256",
        "instruction_sha256",
        "delivered_at",
    ];
    if after_answer {
        fields.push("after_answer");
    }
    shape(record, &fields)?;
    validate_wait(&record["wait"], id, at)?;
    validate(&record["answer"], id, true, at)?;
    matches_wait(&record["wait"], &record["answer"], at)?;
    shape(
        &record["recipient"],
        &["resume_generation", "worker_receipt", "job", "key"],
    )?;
    if after_answer {
        if record["after_answer"] != true {
            return fail("delivery_receipt_invalid");
        }
        answered_generations(&record["wait"], &record["recipient"])?;
    } else {
        generations(&record["wait"], &record["recipient"])?;
    }
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

/// A reply to a question that was already applied may arrive when no worker was
/// retired, so the generation can be equal. It can never be older.
fn answered_generations(wait: &Value, recipient: &Value) -> Result<()> {
    match (
        wait["origin"]["resume_generation"].as_u64(),
        recipient["resume_generation"].as_u64(),
    ) {
        (Some(original), Some(current)) if current >= original => Ok(()),
        // A first-launch task can have no numeric generation yet. Its exact
        // original owner tuple must still match; missing on only one side or
        // a changed worker/key cannot stand in for a known generation.
        (None, None) if wait["origin"].is_object() && wait["origin"] == *recipient => Ok(()),
        // A legacy wait has no origin; its stored answer receipt is the proof.
        (None, _) if wait["origin"].is_null() => Ok(()),
        _ => fail("delivery_generation_mismatch"),
    }
}

fn result(id: i64, wait: &Value, changed: bool, after_answer: bool, replaced: usize) -> Value {
    let mut value = metadata(
        id,
        &wait["question"],
        text(wait, "question_receipt_sha256"),
        changed,
        false,
        false,
    );
    value["delivered"] = json!(true);
    // Receipt of the reply is not the same as applying it to the next prompt:
    // "changed" is true only when this call wrote the note and instruction.
    value["after_answer"] = json!(after_answer);
    value["replaced_blocks"] = json!(replaced);
    value
}

// The Go bridge writes each owner reply as one versioned block:
//   conversation-owner-reply:<sender>:<message>:<version16>
//   ...metadata and the raw reply text...
//   conversation-owner-reply-end:<sender>:<message>:<version16>
// A later or edited reply to the same sender and message replaces the older
// block in the pending instruction. The note keeps every version as history.
const BLOCK_START: &str = "conversation-owner-reply:";
const BLOCK_END: &str = "conversation-owner-reply-end:";

#[derive(Clone, PartialEq)]
struct BlockId {
    sender: String,
    message: String,
    version: String,
}

fn token(v: &str) -> bool {
    !v.is_empty()
        && v.len() <= 64
        && v.bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}

fn parse_id(rest: &str) -> Option<BlockId> {
    let mut parts = rest.split(':');
    let (sender, message, version) = (parts.next()?, parts.next()?, parts.next()?);
    if parts.next().is_some()
        || !token(sender)
        || !token(message)
        || version.len() != 16
        || !version.bytes().all(|b| b.is_ascii_hexdigit())
    {
        return None;
    }
    Some(BlockId {
        sender: sender.to_owned(),
        message: message.to_owned(),
        version: version.to_owned(),
    })
}

/// Complete blocks in order as (first line, last line, id). The scan is
/// sequential and the text inside a block is opaque, so a reply body that
/// imitates a start or end line of another block is never interpreted. A start
/// line without its own end line (an older, reference-only reply) is plain text.
fn complete_blocks(lines: &[&str]) -> Vec<(usize, usize, BlockId)> {
    let mut found = Vec::new();
    let mut i = 0;
    while i < lines.len() {
        let line = lines[i].trim_end_matches('\r');
        if let Some(id) = line.strip_prefix(BLOCK_START).and_then(parse_id) {
            let end = format!("{BLOCK_END}{}:{}:{}", id.sender, id.message, id.version);
            if let Some(offset) = lines[i + 1..]
                .iter()
                .position(|l| l.trim_end_matches('\r') == end)
            {
                let last = i + 1 + offset;
                found.push((i, last, id));
                i = last + 1;
                continue;
            }
        }
        i += 1;
    }
    found
}

/// Remove whole line ranges and one adjacent blank separator for each.
fn remove_ranges(text: &str, ranges: &[(usize, usize)]) -> String {
    if ranges.is_empty() {
        return text.to_owned();
    }
    let lines: Vec<&str> = text.split('\n').collect();
    let mut drop = vec![false; lines.len()];
    for &(first, last) in ranges {
        for flag in &mut drop[first..=last] {
            *flag = true;
        }
        if first > 0 && lines[first - 1].is_empty() && !drop[first - 1] {
            drop[first - 1] = true;
        } else if last + 1 < lines.len() && lines[last + 1].is_empty() && !drop[last + 1] {
            drop[last + 1] = true;
        }
    }
    lines
        .iter()
        .zip(&drop)
        .filter(|(_, dropped)| !**dropped)
        .map(|(line, _)| *line)
        .collect::<Vec<_>>()
        .join("\n")
}

/// Keep only the newest complete block of each sender and message.
pub(super) fn latest_blocks_only(text: &str) -> String {
    let lines: Vec<&str> = text.split('\n').collect();
    let blocks = complete_blocks(&lines);
    let stale: Vec<(usize, usize)> = blocks
        .iter()
        .enumerate()
        .filter(|(n, (_, _, id))| {
            blocks[n + 1..]
                .iter()
                .any(|(_, _, later)| later.sender == id.sender && later.message == id.message)
        })
        .map(|(_, (first, last, _))| (*first, *last))
        .collect();
    remove_ranges(text, &stale)
}

/// Append the new reply text to the pending instruction. Complete blocks of the same
/// sender and message already in the instruction are replaced, so a stale
/// earlier answer never reaches the next prompt. Returns the new instruction
/// and the number of replaced blocks.
pub(super) fn merge_instruction(previous: &Value, suffix: &str) -> Result<(String, usize)> {
    let Value::String(previous) = previous else {
        return if previous.is_null() {
            Ok((suffix.to_owned(), 0))
        } else {
            fail("note_invalid")
        };
    };
    let incoming: Vec<BlockId> = complete_blocks(&suffix.split('\n').collect::<Vec<_>>())
        .into_iter()
        .map(|(_, _, id)| id)
        .collect();
    let lines: Vec<&str> = previous.split('\n').collect();
    let stale: Vec<(usize, usize)> = complete_blocks(&lines)
        .into_iter()
        .filter(|(_, _, id)| {
            incoming
                .iter()
                .any(|new| new.sender == id.sender && new.message == id.message)
        })
        .map(|(first, last, _)| (first, last))
        .collect();
    if stale.is_empty() {
        return Ok((format!("{previous}\n\n{suffix}"), 0));
    }
    let kept = remove_ranges(previous, &stale);
    let merged = if kept.trim().is_empty() {
        suffix.to_owned()
    } else {
        format!("{kept}\n\n{suffix}")
    };
    Ok((merged, stale.len()))
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
            return Ok(result(
                id,
                &record["wait"],
                false,
                record.get("after_answer").is_some(),
                0,
            ));
        }
    }
    // The reply that released the question is already applied in full.
    for receipt in receipts(row)? {
        validate_receipt(receipt, id, at)?;
        if receipt["answer"] == answer && receipt["answer_file_sha256"] == sha {
            check_config(paths, &receipt["wait"]["question"])?;
            return Ok(result(id, &receipt["wait"], false, true, 0));
        }
    }
    let expected_bytes = snapshot_bytes(expected_file)?;
    let expected: Value =
        serde_json::from_slice(&expected_bytes).map_err(|_| "conversation_state_invalid")?;
    if *row != expected {
        return fail("state_changed");
    }
    // A continuing wait is the normal case. Once the question has been answered,
    // the exact applied answer receipt of the same question generation takes the
    // place of the wait; a different or newer question never matches it.
    let (wait, after_answer) = if row[WAIT_KEY].is_null() {
        let receipt = receipts(row)?
            .iter()
            .rev()
            .find(|r| r["wait"]["question"]["request_id"] == answer["request_id"])
            .ok_or("conversation_delivery_wait_missing")?;
        (receipt["wait"].clone(), true)
    } else {
        (row[WAIT_KEY].clone(), false)
    };
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
    // A reply to an already applied question is metadata only: it may be parked
    // behind an unfinished dependency wait. The dependency itself is neither
    // released nor edited, and every other hold still rejects the delivery.
    let dependency_wait = after_answer
        && text(row, "status") == "wait"
        && row["wait_on"].as_array().is_some_and(|on| !on.is_empty());
    if !(matches!(text(row, "status"), "todo" | "doing") || dependency_wait) {
        return fail("status_not_eligible");
    }
    let recipient = origin_identity(row);
    if after_answer {
        answered_generations(&wait, &recipient)?;
    } else {
        generations(&wait, &recipient)?;
    }
    // Delivery never releases a clock-based backoff. Preserve it while making
    // the answer reachable before the next worker starts.
    holds_clear_with(db, row, after_answer)?;
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
    let (instruction, replaced) =
        merge_instruction(&row["scheduler"]["owner_instruction"], suffix)?;
    if instruction.len() as u64 > MAX_BYTES {
        return fail("file_too_large");
    }
    let delivered_at = at.to_rfc3339_opts(SecondsFormat::Micros, true);
    let mut record = json!({"domain":DELIVERY, "wait":wait, "answer":answer,
        "answer_file_sha256":sha, "recipient":recipient, "expected_state_sha256":digest(&expected_bytes),
        "note_sha256":digest(&data), "instruction_sha256":digest(instruction.as_bytes()), "delivered_at":delivered_at});
    if after_answer {
        record["after_answer"] = json!(true);
    }
    let mut saved = records(row)?.to_vec();
    saved.push(record);
    let row = &mut db["items"][index];
    row[DELIVERIES] = json!(saved);
    row["note"] = json!(note);
    row["scheduler"]["owner_instruction"] = json!(instruction);
    row["updated"] = json!(delivered_at);
    Ok(result(id, &wait, true, after_answer, replaced))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn block(message: &str, version: &str, body: &str) -> String {
        format!(
            "conversation-owner-reply:9:{message}:{version}\nmeta\n{body}\nconversation-owner-reply-end:9:{message}:{version}"
        )
    }
    const A: &str = "aaaaaaaaaaaaaaaa";
    const B: &str = "bbbbbbbbbbbbbbbb";
    const C: &str = "cccccccccccccccc";

    #[test]
    fn a_new_version_replaces_only_the_same_sender_and_message() {
        let previous = format!(
            "先の指示\n\n{}\n\n{}\n\n末尾",
            block("1", A, "旧1"),
            block("2", A, "別の返信")
        );
        let (merged, replaced) =
            merge_instruction(&json!(previous), &block("1", B, "新1")).unwrap();
        assert_eq!(replaced, 1);
        assert!(!merged.contains("旧1"));
        assert!(merged.contains("別の返信") && merged.contains("新1"));
        assert!(merged.starts_with("先の指示\n\n"));
        assert!(merged.contains("末尾"));
        assert!(!merged.contains("\n\n\n"), "removal left a gap");
    }

    #[test]
    fn without_a_matching_block_the_text_is_appended_exactly_as_before() {
        let (merged, replaced) = merge_instruction(&json!("a"), "b").unwrap();
        assert_eq!((merged.as_str(), replaced), ("a\n\nb", 0));
        assert_eq!(merge_instruction(&Value::Null, "b").unwrap().0, "b");
        assert!(merge_instruction(&json!(5), "b").is_err());
        // A reference-only older reply has no end line and is kept as history text.
        let legacy = "conversation-owner-reply:9:1:aaaaaaaaaaaaaaaa\nprivate reference";
        let (merged, replaced) = merge_instruction(&json!(legacy), &block("1", B, "新")).unwrap();
        assert_eq!(replaced, 0);
        assert!(merged.starts_with(legacy));
    }

    #[test]
    fn only_the_last_block_of_a_message_survives() {
        let text = format!(
            "{}\n{}\n{}",
            block("1", A, "旧"),
            block("1", B, "中"),
            block("1", C, "新")
        );
        let kept = latest_blocks_only(&text);
        assert!(kept.contains("新") && !kept.contains("旧") && !kept.contains("中"));
        assert_eq!(latest_blocks_only("no blocks"), "no blocks");
    }

    #[test]
    fn text_inside_a_block_is_opaque_to_imitated_markers() {
        // The body of message 1 imitates a complete block of message 2 and a
        // premature end line for another version.
        let fake = block("2", A, "偽の返信");
        let outer = block(
            "1",
            A,
            &format!("{fake}\nconversation-owner-reply-end:9:1:{B}"),
        );
        let (merged, replaced) =
            merge_instruction(&json!(outer.clone()), &block("2", C, "本物")).unwrap();
        assert_eq!(
            replaced, 0,
            "an imitated block inside another reply was replaced"
        );
        assert!(merged.starts_with(&outer));
        // Replacing the outer message removes the imitation with it.
        let (merged, replaced) = merge_instruction(&json!(outer), &block("1", C, "新")).unwrap();
        assert_eq!(replaced, 1);
        assert!(!merged.contains("偽の返信"));
    }

    #[test]
    fn malformed_markers_are_plain_text() {
        for line in [
            "conversation-owner-reply:9:1",
            "conversation-owner-reply:9:1:short",
            "conversation-owner-reply:9:1:aaaaaaaaaaaaaaaa:extra",
            "conversation-owner-reply::1:aaaaaaaaaaaaaaaa",
            "conversation-owner-reply:9:1:zzzzzzzzzzzzzzzz",
        ] {
            let text = format!("{line}\nbody\nconversation-owner-reply-end:9:1:aaaaaaaaaaaaaaaa");
            let lines: Vec<&str> = text.split('\n').collect();
            assert!(complete_blocks(&lines).is_empty(), "{line}");
        }
    }
}
