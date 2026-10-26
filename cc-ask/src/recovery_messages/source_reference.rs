//! An old bot thread may be a qualified read-only source, never a write target.
//! The fixed Go verifier owns the finite native actor/input contract. Retained
//! sender intents and their fingerprints are unchanged; no flag grants a scope.
use super::*;

const FAILED: &str = "conversation source reference is unverified";

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(super) struct Proof {
    domain: String,
    verified: bool,
    normal_request_id: String,
    sender_request_id: String,
    todo_id: u64,
    actor_todo_id: u64,
    actor_job_id: String,
    origin_thread_id: String,
    input_sha256: String,
    body_sha256: String,
    decision_sha256: String,
    title: String,
    succession_sha256: String,
    guild_id: String,
    forum_id: String,
    owner_id: String,
    bot_id: String,
}

pub(super) fn qualify(ctx: &mut Context, request: &Request, normal: &str) -> Result<Proof> {
    if normal != "normal-ask-9620ba9f12adf45e1a01ee67010dafe616750231a1625ffcca0c7ed470a6a862"
        || !request.images.is_empty()
        || request.program_evidence.is_some()
    {
        return Err(FAILED.into());
    }
    let Kind::Ask {
        todo_id,
        origin_thread,
        decision_original,
        ..
    } = &request.operation
    else {
        return Err(FAILED.into());
    };
    let output = ctx
        .effects
        .run(
            &[
                "D:/CC/Tools/discord-ask/unattended-runner.exe".into(),
                "--check-conversation-origin-reference".into(),
                "--request-id".into(),
                normal.into(),
            ],
            20,
        )
        .map_err(|_| FAILED)?;
    if output.code != 0 || output.stdout.len() > 16 * 1024 {
        return Err(FAILED.into());
    }
    let proof: Proof = serde_json::from_str(&output.stdout).map_err(|_| FAILED)?;
    let hash = |bytes: &[u8]| format!("{:x}", Sha256::digest(bytes));
    let sha = |s: &str| {
        s.len() == 64
            && s.bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    };
    if proof.domain != "discord-conversation-origin-reference-v1"
        || !proof.verified
        || proof.normal_request_id != normal
        || proof.sender_request_id != request.request_id
        || proof.todo_id != *todo_id
        || *todo_id != 1171
        || proof.actor_todo_id != 3277
        || proof.actor_job_id.is_empty()
        || proof.origin_thread_id != *origin_thread
        || proof.body_sha256 != hash(request.body.as_bytes())
        || proof.decision_sha256 != digest(decision_original)?
        || proof.title != request.title
        || !sha(&proof.input_sha256)
        || !sha(&proof.succession_sha256)
        || proof.guild_id != request.destination.guild
        || proof.forum_id != request.destination.forum
        || proof.owner_id != request.destination.owner
    {
        return Err(FAILED.into());
    }
    Ok(proof)
}

pub(super) fn verify_source(api: &mut RecoveryApi<'_>, proof: &Proof) -> Result<()> {
    let thread = &proof.origin_thread_id;
    let channel = api.channel(thread)?;
    if proof.bot_id != api.bot
        || channel["id"] != *thread
        || channel["type"] != 11
        || channel["guild_id"] != api.destination.guild
        || channel["parent_id"] != api.destination.forum
        || channel["owner_id"] != api.bot
    {
        return Err(FAILED.into());
    }
    let starter = api.get(&format!("/channels/{thread}/messages/{thread}"))?;
    if starter["id"] != *thread
        || starter["channel_id"] != *thread
        || starter["author"]["id"] != api.bot
        || starter["author"]["bot"] != true
        || starter.get("webhook_id").is_some_and(|v| !v.is_null())
    {
        return Err(FAILED.into());
    }
    Ok(())
}

pub(super) fn failure_stage(error: &str) -> &'static str {
    match error {
        "origin must be the exact owner request or a verified recovery post; legacy questions are excluded" => "origin_scope",
        FAILED => "source_scope",
        "recovery-messages.lock is busy" | "todo.lock is busy" => "busy",
        "Discord API rate limited" => "rate_limited",
        _ if error.starts_with("URLError:") => "network",
        _ => "operation_pending",
    }
}

pub fn failure_report(error: &str) -> Value {
    json!({"domain": DOMAIN, "delivery": "not_verified", "failure_stage": failure_stage(error)})
}
