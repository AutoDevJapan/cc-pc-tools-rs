use super::*;

pub(super) fn threads<'a>(prior: &'a Request, incoming: &'a Request) -> Result<(&'a str, &'a str)> {
    match (&prior.operation, &incoming.operation) {
        (Kind::Log { thread: Some(old) }, Kind::Log { thread: Some(new) }) if old != new => Ok((old, new)),
        _ => Err("normal log target correction requires two distinct explicit log targets".into()),
    }
}

pub(super) fn only_thread_changed(prior: &Request, incoming: &Request) -> Result<bool> {
    let Ok((_, new)) = threads(prior, incoming) else { return Ok(false); };
    if prior.request_id != incoming.request_id || prior.notify || incoming.notify
        || !prior.images.is_empty() || !incoming.images.is_empty()
        || prior.program_evidence.is_some() || incoming.program_evidence.is_some()
        || prior.native_evidence.is_some() || incoming.native_evidence.is_some()
        || prior.send_before.is_some() || incoming.send_before.is_some() {
        return Ok(false);
    }
    let mut allowed = prior.clone();
    allowed.operation = Kind::Log { thread: Some(new.into()) };
    allowed.fingerprint = allowed.fingerprint()?;
    Ok(allowed == *incoming)
}

pub(super) fn possible_post(ledger: &Ledger, id: &str) -> bool {
    ledger.events.iter().any(|event| match event {
        Event::Pending { request_id, .. } | Event::Posted { request_id, .. }
        | Event::Verified { request_id, .. } | Event::DeliveryVerified { request_id }
        => request_id == id,
        _ => false,
    })
}

pub(super) fn alias_related(ledger: &Ledger, id: &str) -> bool {
    ledger.events.iter().any(|event| matches!(event,
        Event::Alias { request_id, canonical_request_id, .. }
            if request_id == id || canonical_request_id == id))
}

fn duplicate_input(ledger: &Ledger, incoming: &Request) -> bool {
    ledger.current_requests().values().any(|current|
        current.request_id != incoming.request_id && current.fingerprint == incoming.fingerprint)
}

fn authority(ledger: &Ledger, incoming: &Request, bot: &str) -> Result<(String, String)> {
    let Kind::Log { thread: Some(thread) } = &incoming.operation else {
        return Err("normal log target correction requires an existing explicit log thread".into());
    };
    // Keep the ordinary allowlist and exclude question/progress/read-receipt purposes.
    if RecoveryApi::known_progress(ledger, &incoming.destination, bot, thread) != Some(false) {
        return Err("normal log correction target has no verified ordinary log authority".into());
    }
    ledger.events.iter().find_map(|event| {
        let Event::Verified { request_id, phase, receipt } = event else { return None; };
        let request = ledger.request(request_id)?;
        (request_id != &incoming.request_id && phase == "log:0"
            && matches!(request.operation, Kind::Log { .. })
            && request.destination == incoming.destination
            && receipt.author_id == bot && receipt.thread_id == *thread
            && !ledger.effect(request_id,phase).is_some_and(|effect| effect.content.lines().any(|line|line.starts_with("-# owner-receipt:"))))
            .then(|| (request_id.clone(), phase.clone()))
    }).ok_or("normal log correction requires independent ordinary log authority".into())
}

fn purpose_bound(home: &Path, id: &str) -> Result<bool> {
    Ok(clarification::has_saved_request(home, id)?
        || acknowledgement::has_saved_request(home, id)?)
}

pub(super) fn correct(ctx: &mut Context, ledger: &mut Ledger, prior: &Request, incoming: &Request) -> Result<()> {
    if !only_thread_changed(prior, incoming)? {
        return Err("request-id is already bound to different recovery input".into());
    }
    if purpose_bound(&ctx.here, &incoming.request_id)? {
        return Err("purpose-bound log target cannot be corrected as an ordinary report".into());
    }
    if alias_related(ledger, &incoming.request_id) {
        return Err("normal log target correction has immutable aliases".into());
    }
    if duplicate_input(ledger, incoming) {
        return Err("normal log corrected input is already bound to another request; no POST".into());
    }
    if possible_post(ledger, &incoming.request_id) {
        return Err("normal log target correction follows a possible POST; existing input retained".into());
    }
    let (old, new) = threads(prior, incoming)?;
    let bot = ledger.bot(&incoming.request_id).ok_or("normal log original author is unavailable")?.to_owned();
    if RecoveryApi::known_progress(ledger, &prior.destination, &bot, old).is_some() {
        return Err("normal log original target was already authorized; input retained".into());
    }
    let (authority_request_id, authority_phase) = authority(ledger, incoming, &bot)?;
    let event = Event::LogTargetCorrected {
        request: Box::new(incoming.clone()), previous_fingerprint: prior.fingerprint.clone(),
        authority_request_id, authority_phase,
    };
    let mut checked = Ledger { domain: ledger.domain.clone(), version: ledger.version, events: ledger.events.clone() };
    checked.events.push(event.clone());
    checked.validate()?;
    let home = ctx.here.clone();
    let mut api = RecoveryApi::inspect(ctx, ledger, incoming)?;
    if api.bot != bot { return Err("normal log correction bot changed".into()); }
    api.assert_bot_thread(new)?;
    drop(api);
    // Append through the same locked, validated journal. The original Intent stays intact.
    ledger.append(&home, event)
}

/// Read metadata for this exact ordinary log only. Never append an intent,
/// reserve a message, call HTTP or dispatch work. Returned booleans are observations,
/// not a capability for a later send: normal delivery rechecks under the same lock.
pub(super) fn inspect(ctx: &mut Context, args: &LogArgs) -> Result<Value> {
    if args.notify || !args.attach.is_empty() || args.thread.is_none() {
        return Err("normal log inspection requires a silent text-only explicit target".into());
    }
    let (incoming, _) = prepare_log(ctx, args)?;
    let home = ctx.here.clone();
    ensure_storage(&home)?;
    for name in [LEDGER, LOCK] {
        if !fs::symlink_metadata(home.join(name)).is_ok_and(|m| m.is_file() && !m.file_type().is_symlink()) {
            return Err("normal log inspection requires existing recovery files".into());
        }
    }
    let _lock = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
    let ledger = Ledger::load(&home)?;
    let prior = ledger.request(&incoming.request_id);
    let bound_purpose = purpose_bound(&home, &incoming.request_id)?;
    let alias = prior.is_some_and(|p| p.request_id != incoming.request_id);
    let related_alias = prior.is_some_and(|p| alias_related(&ledger, &p.request_id));
    let duplicate = duplicate_input(&ledger, &incoming);
    let activity = prior.is_some_and(|p| possible_post(&ledger, &p.request_id));
    let old_known = prior.and_then(|p| {
        let Kind::Log { thread: Some(old) } = &p.operation else { return None; };
        let bot = ledger.bot(&incoming.request_id)?;
        RecoveryApi::known_progress(&ledger, &p.destination, bot, old)
    }).is_some();
    let stored_authority = ledger.bot(&incoming.request_id).and_then(|bot| authority(&ledger, &incoming, bot).ok());
    let can_retarget = match prior {
        Some(p) if !bound_purpose && !alias && !related_alias && !duplicate && !activity && !old_known && stored_authority.is_some() => only_thread_changed(p, &incoming)?,
        _ => false,
    };
    let effect_count = prior.map(|p| ledger.events.iter().filter(|event|
        matches!(event, Event::Pending { request_id, .. } if request_id == &p.request_id)).count()).unwrap_or(0);
    Ok(json!({
        "domain":DOMAIN,"request_id":incoming.request_id,"delivery":"inspection_only","check_only":true,
        "ledger_written":false,"transport":"not_invoked_by_inspection","approval":"not_inferred",
        "intent_exists":prior.is_some(),"canonical_alias":alias,"purpose_bound":bound_purpose,"alias_related":related_alias,"duplicate_input":duplicate,
        "prior_kind":prior.map(|p| if matches!(p.operation,Kind::Log{..}) {"log"} else {"ask"}),
        "original_fingerprint":prior.map(|p| &p.fingerprint),"incoming_fingerprint":incoming.fingerprint,
        "prior_body_sha256":prior.map(|p|format!("{:x}",Sha256::digest(p.body.as_bytes()))),
        "incoming_body_sha256":format!("{:x}",Sha256::digest(incoming.body.as_bytes())),
        "prior_thread":prior.and_then(|p|match &p.operation{Kind::Log{thread}=>thread.as_deref(),_=>None}),
        "requested_thread":args.thread,"possible_post":activity,"reserved_effect_count":effect_count,
        "stored_ordinary_log_authority":stored_authority,
        "retarget_eligible":can_retarget,"legacy_state":"unchanged","live_destination_verified":false
    }))
}
