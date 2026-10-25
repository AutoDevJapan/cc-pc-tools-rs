use super::*;

// Inspection only: these facts never append DeliveryVerified or authorize POST.
// Reconstruct every mandatory effect from the immutable intent, rather than
// treating an arbitrary Pending/Verified phase name as proof of its contents.
pub(super) fn inspect(
    api: &mut RecoveryApi<'_>,
    ledger: &Ledger,
    request: &Request,
) -> Result<Vec<String>> {
    let Kind::Ask { origin_thread, .. } = &request.operation else {
        return Err("question delivery inspection requires an ask".into());
    };
    if ledger.repaired_origin(&request.request_id).is_some() {
        return Err("origin-repaired questions retain their original recovery path".into());
    }
    let question = ledger
        .receipt(&request.request_id, "question:0")
        .ok_or("question starter is unconfirmed")?;
    let progress = ledger
        .receipt(&request.request_id, "progress")
        .ok_or("question progress is unconfirmed")?;
    let text = parts(&request.body);
    let mut required = BTreeMap::new();
    for (index, body) in text.iter().enumerate() {
        let phase = if index == 0 {
            "question:0".into()
        } else {
            format!("question-continuation:{index}")
        };
        let expected = if index == 0 {
            effect(
                request,
                &phase,
                Target::Forum {
                    title: request.title.clone(),
                },
                &format!(
                    "{body}\n\n元の投稿: <{}>",
                    request.destination.url(origin_thread)
                ),
                true,
                true,
            )?
        } else {
            effect(
                request,
                &phase,
                Target::Thread {
                    thread: question.thread_id.clone(),
                },
                body,
                false,
                false,
            )?
        };
        required.insert(phase, expected);
    }
    let title = format!("進捗 {}", request.title)
        .chars()
        .take(100)
        .collect();
    let bridge = format!(
        "元の投稿: <{}>\n質問: <{}>\n以後の進捗と結果はこの投稿へ記録します。",
        request.destination.url(origin_thread),
        request.destination.url(&question.thread_id)
    );
    required.insert(
        "progress".into(),
        effect(
            request,
            "progress",
            Target::Forum { title },
            &bridge,
            false,
            false,
        )?,
    );
    for (phase, expected) in &required {
        if ledger.effect(&request.request_id, phase) != Some(expected) {
            return Err("mandatory question effect differs".into());
        }
        let receipt = ledger
            .receipt(&request.request_id, phase)
            .ok_or("mandatory question effect is unconfirmed")?;
        api.verify(
            ledger,
            request,
            expected,
            &receipt.thread_id,
            &receipt.message_id,
        )?;
    }
    let auxiliary = [
        (
            "question-links",
            question.thread_id.clone(),
            format!(
                "元の投稿: <{}>\n進捗: <{}>",
                request.destination.url(origin_thread),
                request.destination.url(&progress.thread_id)
            ),
        ),
        (
            "origin-links",
            origin_thread.clone(),
            format!(
                "新しい質問: <{}>\n進捗: <{}>",
                request.destination.url(&question.thread_id),
                request.destination.url(&progress.thread_id)
            ),
        ),
    ];
    let mut unconfirmed = Vec::new();
    for (phase, thread, body) in auxiliary {
        let expected = effect(
            request,
            phase,
            Target::Thread { thread },
            &body,
            false,
            false,
        )?;
        if let Some(saved) = ledger.effect(&request.request_id, phase) {
            if saved != &expected {
                return Err("auxiliary question effect differs".into());
            }
        }
        if let Some(receipt) = ledger.receipt(&request.request_id, phase) {
            api.verify(
                ledger,
                request,
                &expected,
                &receipt.thread_id,
                &receipt.message_id,
            )?;
        } else {
            unconfirmed.push(phase.to_owned());
        }
    }
    for event in &ledger.events {
        if let Event::Pending {
            request_id, phase, ..
        } = event
        {
            if request_id == &request.request_id
                && !required.contains_key(phase)
                && phase != "question-links"
                && phase != "origin-links"
            {
                return Err("unknown question effect is not an auxiliary link".into());
            }
        }
    }
    Ok(unconfirmed)
}
