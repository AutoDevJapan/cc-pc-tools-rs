use super::*;

/// The normal preflight must search the same message domain as normal ask.
/// Reuse its immutable intent and existing exact read-only API proof; never
/// reserve, append, consume, close, dispatch, or fall back to the old ledger.
pub fn lookup_decision(ctx: &mut Context, value: &Value) -> Result<Value> {
    let proof = lookup_decision_inner(ctx, value)?;
    match crate::decision_identity::redact(ctx, &proof) {
        Ok(mut redacted) if protocol_preserved(&proof, &redacted) => {
            let changed = redacted != proof;
            let has_text = proof["original_question"].is_string();
            redacted["text_available"] = json!(has_text);
            redacted["text_redacted"] = json!(changed && has_text);
            if changed && has_text {
                redacted["next_action"] = json!(format!("{} 伏せ字を含む表示です。適用時は元ToDoの私的原票で原文と現在版を確認し、この表示を本人の原文やcanonicalな反映票に使わないでください。", core::s(&proof["next_action"])));
            }
            Ok(redacted)
        }
        Ok(_) => Ok(reference_only(&proof, "metadata_changed")),
        // The original name registry can be unavailable. Keep the positive
        // answer/intent reference, without exposing unredacted human text or
        // reconstructing an empty registry. The task's existing private reply
        // receipt remains the context source, not this metadata projection.
        Err(_) => Ok(reference_only(&proof, "unavailable")),
    }
}

fn same_keys(a: &Value, b: &Value) -> bool {
    a.as_object()
        .zip(b.as_object())
        .is_some_and(|(a, b)| a.len() == b.len() && a.keys().all(|key| b.contains_key(key)))
}

fn machine_identity_preserved(a: &Value, b: &Value) -> bool {
    same_keys(a, b)
        && [
            "owner",
            "guild",
            "parent",
            "todo_id",
            "decision",
            "resources",
        ]
        .iter()
        .all(|key| a.get(key) == b.get(key))
}

// The legacy display redactor works on JSON text. A matching name must never
// rename protocol keys, change an enum, or damage an ID/version. In that case
// use the existing typed reference projection, not the corrupted display.
fn protocol_preserved(proof: &Value, redacted: &Value) -> bool {
    if !same_keys(proof, redacted)
        || !machine_identity_preserved(&proof["decision_identity"], &redacted["decision_identity"])
        || !proof.as_object().unwrap().iter().all(|(key, value)| {
            matches!(
                key.as_str(),
                "original_question" | "original_decision" | "decision_identity" | "owner_replies"
            ) || redacted.get(key) == Some(value)
        })
    {
        return false;
    }
    match (
        proof["owner_replies"].as_array(),
        redacted["owner_replies"].as_array(),
    ) {
        (None, None) => true,
        (Some(a), Some(b)) if a.len() == b.len() => a.iter().zip(b).all(|(a, b)| {
            same_keys(a, b)
                && machine_identity_preserved(&a["decision_identity"], &b["decision_identity"])
                && a.as_object().unwrap().iter().all(|(key, value)| {
                    matches!(key.as_str(), "body" | "decision_identity")
                        || b.get(key) == Some(value)
                })
        }),
        _ => false,
    }
}

fn reference_only(proof: &Value, reason: &str) -> Value {
    let has_context = proof["original_question"].is_string()
        && proof["thread_id"].as_str().is_some_and(|id| !id.is_empty())
        && proof["url"].as_str().is_some_and(|url| !url.is_empty());
    let action = if has_context {
        format!("{} 本文は保護確認のため表示していません。元ToDoの私的原票で原文と現在版を確認してください。まだ原票がなければ、通常conversation statusでthreadに対応するrequest_idを確認し、conversation replies --request-id <ID> --output <新しい絶対パス.private.json>で取得してください。添付はその返信のhistory-readでID・サイズ・本文SHAと編集版を取得してからhistory-attachmentへ渡してください。", core::s(&proof["next_action"]))
    } else {
        core::s(&proof["next_action"]).to_owned()
    };
    let mut result = json!({"domain":DOMAIN,"found":proof["found"],
        "read_only":true,"ledger_written":false,"legacy_state":"unchanged",
        "legacy_searched":false,"text_available":false,"text_redacted":has_context,"redaction":reason,
        "approval":"not_inferred","next_action":action});
    if proof["source"] == "discord-api-v10" {
        result["source"] = json!("discord-api-v10");
    }
    if proof["decision_identity"]["todo_id"].as_u64().is_some() {
        result["todo_id"] = proof["decision_identity"]["todo_id"].clone();
    }
    for key in [
        "suppressed",
        "delivery",
        "url",
        "thread_id",
        "owner_id",
        "question_message_id",
        "question_timestamp",
        "question_fingerprint",
        "progress_thread_id",
        "reply_versions",
        "image_sha256",
    ] {
        if let Some(value) = proof.get(key) {
            result[key] = value.clone();
        }
    }
    for key in ["decision_identity", "request_id", "original_question"] {
        if let Some(value) = proof.get(key) {
            let bytes = value
                .as_str()
                .map(str::as_bytes)
                .map(Vec::from)
                .unwrap_or_else(|| value.to_string().into_bytes());
            result[format!("{key}_sha256")] = json!(format!("{:x}", Sha256::digest(bytes)));
        }
    }
    if let Some(replies) = proof["owner_replies"].as_array() {
        result["owner_replies"] = json!(replies
            .iter()
            .map(|reply| {
                let mut reference = json!({"text_available":false,
                "body_sha256":format!("{:x}", Sha256::digest(core::s(&reply["body"]).as_bytes()))});
                for key in [
                    "todo_id",
                    "question_thread_id",
                    "question_message_id",
                    "author_id",
                    "message_id",
                    "timestamp",
                ] {
                    reference[key] = reply[key].clone();
                }
                reference
            })
            .collect::<Vec<_>>());
    }
    result
}

fn lookup_decision_inner(ctx: &mut Context, value: &Value) -> Result<Value> {
    let destination = Destination::from_context(ctx)?;
    for (key, expected) in [
        ("owner", destination.owner.as_str()),
        ("guild", destination.guild.as_str()),
        ("parent", destination.forum.as_str()),
    ] {
        if value
            .get(key)
            .is_some_and(|actual| actual.as_str() != Some(expected))
        {
            return Err("decision destination differs from configured owner/guild/forum".into());
        }
    }
    SecretCheck::new().text(&value.to_string())?;
    let identity = crate::decision_identity::normalize(ctx, value)?;
    let ledger = Ledger::load(&ctx.here)?;
    let candidates: Vec<_> = ledger.events.iter().filter_map(|event| {
        let Event::Intent { request } = event else { return None };
        (request.destination == destination
            && matches!(&request.operation, Kind::Ask { decision_identity, .. } if *decision_identity == identity))
        .then_some(request.as_ref())
    }).collect();
    let request = match candidates.as_slice() {
        [] => {
            return Ok(
                json!({"found":false,"decision_identity":identity,"read_only":true,
            "ledger_written":false,"legacy_state":"unchanged","legacy_searched":false,
            "next_action":"新方式の質問には一致がありません。旧方式の記録は検索していないため、本人の未回答とは判断せず、元ToDoの既存質問と本人回答も確認してください。"}),
            )
        }
        [request] => *request,
        _ => {
            return Err(
                "decision has multiple canonical questions; inspect the existing new-domain receipts without resending"
                    .into(),
            )
        }
    };
    let Some(receipt) = ledger.receipt(&request.request_id, "question:0") else {
        // A Pending intent may already have reached Discord even if its POST
        // response or the subsequent verification GET was lost.
        let delivery = if ledger.effect(&request.request_id, "question:0").is_some() {
            "pending"
        } else {
            "not_sent"
        };
        return Ok(
            json!({"domain":DOMAIN,"found":true,"suppressed":"existing_question_pending",
            "request_id":request.request_id,"decision_identity":identity,"read_only":true,
            "ledger_written":false,"legacy_state":"unchanged","legacy_searched":false,"delivery":delivery,
            "approval":"not_inferred","next_action":"保存済みの元質問の配送を確認してください。同じ判断を新しい質問として送らないでください。"}),
        );
    };
    let mut proof = replies(
        ctx,
        &RepliesArgs {
            thread: receipt.thread_id.clone(),
            message: Some(receipt.message_id.clone()),
            request_id: Some(request.request_id.clone()),
            todo_id: Some(core::n(&identity["todo_id"])),
            read_only: true,
        },
    )?;
    let answered = proof["owner_replies"]
        .as_array()
        .is_some_and(|items| !items.is_empty());
    proof["found"] = json!(true);
    proof["legacy_searched"] = json!(false);
    proof["suppressed"] = json!(if answered {
        "already_answered"
    } else {
        "existing_question"
    });
    proof["url"] = json!(destination.url(&receipt.thread_id));
    proof["image_sha256"] = json!(request
        .images
        .iter()
        .map(|image| &image.sha256)
        .collect::<Vec<_>>());
    proof["next_action"] = json!(if answered {
        "元質問・現在の本人回答と編集版を読み、元ToDoと担当へ引き継いで作業を続けてください。添付だけの回答はunattended-runnerのhistory-attachmentで対象と版を照合して確認してください。image_sha256と今回の質問画像が異なる場合は旧回答を流用せず、判断範囲に新しい画像版を明記してください。取得を実読・適用・承認へ読み替えず、同じ判断を再質問しないでください。"
    } else {
        "既存の質問URLを保持してください。同じ判断を新しい質問として送らないでください。"
    });
    Ok(proof)
}
