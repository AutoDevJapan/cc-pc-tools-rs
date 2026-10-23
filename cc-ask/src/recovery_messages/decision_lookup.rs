use super::*;

/// The normal preflight must search the same message domain as normal ask.
/// Reuse its immutable intent and existing exact read-only API proof; never
/// reserve, append, consume, close, dispatch, or fall back to the old ledger.
pub fn lookup_decision(ctx: &mut Context, value: &Value) -> Result<Value> {
    let proof = lookup_decision_inner(ctx, value)?;
    match crate::decision_identity::redact(ctx, &proof) {
        Ok(mut redacted) => {
            redacted["text_available"] = json!(true);
            Ok(redacted)
        }
        // The original name registry can be unavailable. Keep the positive
        // answer/intent reference, without exposing unredacted human text or
        // reconstructing an empty registry. The task's existing private reply
        // receipt remains the context source, not this metadata projection.
        Err(_) => Ok(reference_only(&proof)),
    }
}

fn reference_only(proof: &Value) -> Value {
    let mut result = json!({"domain":DOMAIN,"found":proof["found"],
        "read_only":true,"ledger_written":false,"legacy_state":"unchanged",
        "legacy_searched":false,"text_available":false,"redaction":"registry_unavailable",
        "approval":"not_inferred","next_action":"この照会は新方式の質問を対象にします。本文の伏せ字確認に必要な原名簿が利用できないため、本文は表示せず元URLと返信IDを返します。元ToDoに配送済みの私的返信原票を正規の保護判定で確認して読み、元質問と現在版を照合して続行してください。未読や未反映を理由に同じ質問を送らず、旧記録の回答も元ToDoで確認してください。画像は返されたSHAと今回の画像を照合し、異なる場合は旧回答を流用しないでください。"});
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
            result[format!("{key}_sha256")] = json!(format!(
                "{:x}",
                Sha256::digest(value.to_string().as_bytes())
            ));
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
