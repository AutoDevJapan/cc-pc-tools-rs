//! Bind an API-verified ordinary intake to its own independently guarded actor.
use crate::{Paths, Result, conversation_wait};
use chrono::DateTime;
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::path::Path;

fn text<'a>(v: &'a Value, k: &str) -> &'a str {
    v[k].as_str().unwrap_or("")
}
fn fail<T>() -> Result<T> {
    Err("implementation_ordinary_source_invalid".into())
}

pub(crate) fn qualify(paths: &Paths, actor: &Value, input: &Value) -> Result<()> {
    if input["kind"] != "implementation"
        || text(actor, "project").eq_ignore_ascii_case("owner")
        || actor["project"] == input["project"]
    {
        return fail();
    }
    let path = Path::new(text(input, "intake_source_file"));
    let expected_name = format!("{}.json", text(input, "message_id"));
    // Production reads the canonical runner namespace. Isolated CLI fixtures
    // use their separate ToDo home, never an arbitrary source ancestor.
    let home = if crate::normalized(&paths.store.to_string_lossy())
        == crate::normalized(&Paths::production().store.to_string_lossy())
    {
        std::path::PathBuf::from("D:/CC/Tools/discord-ask/unattended")
    } else {
        paths
            .store
            .parent()
            .ok_or("implementation_ordinary_source_invalid")?
            .to_path_buf()
    };
    if crate::normalized(&path.to_string_lossy())
        != crate::normalized(&home.join("requests").join(&expected_name).to_string_lossy())
    {
        return fail();
    }
    if !path.is_absolute()
        || path.file_name().and_then(|s| s.to_str()) != Some(expected_name.as_str())
        || path
            .parent()
            .and_then(Path::file_name)
            .and_then(|s| s.to_str())
            != Some("requests")
    {
        return fail();
    }
    let bytes = conversation_wait::bytes(path)?;
    if format!("{:x}", Sha256::digest(&bytes)) != text(input, "intake_source_sha256") {
        return fail();
    }
    let raw: Value =
        serde_json::from_slice(&bytes).map_err(|_| "implementation_ordinary_source_invalid")?;
    let context: Value = serde_json::from_str(text(&raw, "question_context"))
        .map_err(|_| "implementation_ordinary_source_invalid")?;
    let at = |s: &str| DateTime::parse_from_rfc3339(s).ok();
    let key = format!(
        "{:x}",
        Sha256::digest(
            format!(
                "unattended-owner-v1\0{}\0{}",
                text(input, "thread_id"),
                text(input, "message_id")
            )
            .as_bytes()
        )
    );
    let marker = format!("unattended-owner-receipt:{}", &key[..32]);
    if raw["source"] != "discord-api-v10"
        || raw["record_version"] != 2
        || text(&raw, "key") != &key[..32]
        || !text(actor, "note")
            .lines()
            .any(|line| line.trim_end_matches('\r') == marker)
        || raw["thread_id"] != input["thread_id"]
        || raw["message_id"] != input["message_id"]
        || raw["author_id"] != input["author_id"]
        || raw["text"] != input["owner_reply"]
        || at(text(&raw, "created_at")).is_none()
        || at(text(&raw, "created_at")) != at(text(input, "created_at"))
        || text(&raw, "edited_at") != text(input, "edited_at")
        || context["source"] != "discord-api-v10"
        || context["read_only"] != true
        || context["request_id"] != input["sender_request_id"]
        || context["thread_id"] != input["thread_id"]
        || context["question_message_id"] != input["question_message_id"]
        || context["owner_id"] != input["author_id"]
        || context["original_question"] != input["original_question"]
        || format!(
            "{:x}",
            Sha256::digest(
                serde_json::to_vec(&context["decision_identity"])
                    .map_err(|_| "implementation_ordinary_source_invalid")?
            )
        ) != text(input, "decision_identity_sha256")
    {
        return fail();
    }
    let replies = context["owner_replies"]
        .as_array()
        .ok_or("implementation_ordinary_source_invalid")?;
    if !replies.iter().any(|reply| {
        reply["message_id"] == input["message_id"]
            && reply["author_id"] == input["author_id"]
            && reply["question_thread_id"] == input["thread_id"]
            && reply["question_message_id"] == input["question_message_id"]
            && reply["body"] == input["owner_reply"]
            && reply["todo_id"].as_i64().map(|n| n.to_string()).as_deref()
                == Some(text(input, "origin_todo_id"))
            && at(text(reply, "timestamp")) == at(text(input, "created_at"))
    }) {
        return fail();
    }
    Ok(())
}

pub(crate) fn matches(paths: &Paths, actor: &Value, input: &Value, body: &str) -> bool {
    if qualify(paths, actor, input).is_err() {
        return false;
    }
    let normal = body.replace('\\', "/").to_lowercase();
    let path = text(input, "intake_source_file")
        .replace('\\', "/")
        .to_lowercase();
    let before = |c: char| c.is_whitespace() || "\"'`(".contains(c);
    let after = |c: char| c.is_whitespace() || "\"'`),;。".contains(c);
    normal.match_indices(&path).any(|(i, _)| {
        (i == 0 || normal[..i].chars().next_back().is_some_and(before))
            && (i + path.len() == normal.len()
                || normal[i + path.len()..].chars().next().is_some_and(after))
    }) && decimal_token(&normal.replace(&path, ""), text(input, "message_id"))
        && decimal_token(&normal.replace(&path, ""), text(input, "thread_id"))
}

fn decimal_token(body: &str, id: &str) -> bool {
    body.match_indices(id).any(|(i, _)| {
        (i == 0 || !body[..i].chars().next_back().unwrap().is_ascii_digit())
            && (i + id.len() == body.len()
                || !body[i + id.len()..]
                    .chars()
                    .next()
                    .unwrap()
                    .is_ascii_digit())
    })
}
