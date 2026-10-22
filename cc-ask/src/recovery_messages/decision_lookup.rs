use super::*;

/// The normal preflight must search the same message domain as normal ask.
/// Reuse its immutable intent and existing exact read-only API proof; never
/// reserve, append, consume, close, dispatch, or fall back to the old ledger.
pub fn lookup_decision(ctx: &mut Context, value: &Value) -> Result<Value> {
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
            "ledger_written":false,"legacy_state":"unchanged"}),
            )
        }
        [request] => *request,
        _ => {
            return Err(
                "decision has multiple canonical questions; explicit reconciliation required"
                    .into(),
            )
        }
    };
    let Some(receipt) = ledger.receipt(&request.request_id, "question:0") else {
        return Ok(
            json!({"domain":DOMAIN,"found":true,"suppressed":"existing_question_pending",
            "request_id":request.request_id,"decision_identity":identity,"read_only":true,
            "ledger_written":false,"legacy_state":"unchanged","delivery":"not_sent",
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
        "元質問・現在の本人回答と編集版を読み、元ToDoと担当へ引き継いで作業を続けてください。添付だけの回答は既存readerで確認してください。取得を実読・適用・承認へ読み替えず、同じ判断を再質問しないでください。"
    } else {
        "既存の質問URLを保持してください。同じ判断を新しい質問として送らないでください。"
    });
    Ok(proof)
}
