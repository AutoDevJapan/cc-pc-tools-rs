//! New, explicitly requested owner messages while the original question ledger is lost.
//!
//! This is a separate message domain, not a recovery of state.json. It never loads,
//! changes, unlocks or dispatches that ledger. HTTP writes are limited to a newly
//! reserved forum post, its verified recovery threads and an explicitly supplied
//! owner request thread. Close uses one exact verified question PATCH only.
//! No scheduler, model or DELETE is used. Only new conversation-log image
//! requests may run the fixed, bounded, read-only Go media verifier subprocess.
//!
//! Storage uses recovery-messages.json/lock and recovery-close.json/lock, with
//! domain-prefixed temporary snapshots. Message events are append-only; close
//! snapshots preserve PATCH intent/outcome even when a pending reply is recorded.
//! Each snapshot is flushed and atomically replaced under its dedicated byte lock.
//! Token/config contents and attachment paths/bytes are never receipt fields.
use crate::core::{self, ByteLock, Context, Result, MAX_CONTENT, SILENT};
use clap::Args;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    fs,
    io::{Read, Write},
    path::{Path, PathBuf},
    time::Duration,
};

const API: &str = "https://discord.com/api/v10";
mod clarification;
mod program_question;
const LEDGER: &str = "recovery-messages.json";
const LOCK: &str = "recovery-messages.lock";
const DOMAIN: &str = "new-owner-recovery-messages";

mod decision_lookup;
pub use decision_lookup::lookup_decision;
const EMBEDS: u64 = 4;
const TEXT_BYTES: usize = 128 * 1024;
const LEDGER_BYTES: usize = 32 * 1024 * 1024;
const IMAGE_BYTES: usize = 10 * 1024 * 1024;
const UPLOAD_BYTES: usize = 23 * 1024 * 1024;
const HISTORY_PAGES: usize = 100;
const PENDING: &str = "RecoveryMessagePending: delivery is unconfirmed; retained receipt requires GET inspection, never another POST";

#[derive(Args, Debug)]
pub struct LogArgs {
    #[arg(long)]
    pub request_id: String,
    /// Explicit topic; never inferred from lost questions.
    #[arg(long)]
    pub title: String,
    #[arg(long)]
    pub body_file: PathBuf,
    /// Only a thread already verified in the independent recovery ledger.
    #[arg(long)]
    pub thread: Option<String>,
    #[arg(long)]
    pub notify: bool,
    /// Images only, validated and held in memory before any HTTP.
    #[arg(long = "attach")]
    pub attach: Vec<PathBuf>,
}

#[derive(Args, Debug)]
pub struct AskArgs {
    #[arg(long)]
    pub request_id: String,
    #[arg(long)]
    pub todo_id: u64,
    #[arg(long)]
    pub decision_file: PathBuf,
    #[arg(long)]
    pub body_file: PathBuf,
    /// Defaults to the original question's first nonempty line.
    #[arg(long)]
    pub title: Option<String>,
    /// Required here or in decision-file.origin_thread_id.
    #[arg(long)]
    pub origin_thread: Option<String>,
    /// Images shown directly in the question starter, verified before HTTP.
    #[arg(long = "attach")]
    pub attach: Vec<PathBuf>,
    #[arg(long = "expected-image-sha256")]
    pub expected_image_sha256: Vec<String>,
    /// Read-only native Go drawing evidence for this exact question only.
    #[arg(long)]
    pub program_evidence: Option<PathBuf>,
}

/// Rebind only a not-yet-sent ask to an exact previously verified progress post.
#[derive(Args, Debug)]
pub struct RepairOriginArgs {
    #[arg(long)]
    pub request_id: String,
    #[arg(long)]
    pub todo_id: u64,
    #[arg(long)]
    pub expected_origin_thread: String,
    /// Exact owner reply in the original request thread; verified with Discord GET.
    #[arg(long)]
    pub owner_reply_message: String,
    /// Exact verified progress starter already present in the independent ledger.
    #[arg(long)]
    pub progress_message: String,
}

/// Resume exactly the saved ask after its origin repair; no body/decision input.
#[derive(Args, Debug)]
pub struct ResumeAskArgs {
    #[arg(long)]
    pub request_id: String,
    #[arg(long)]
    pub todo_id: u64,
    #[arg(long = "attach")]
    pub attach: Vec<PathBuf>,
}

#[derive(Args, Debug)]
pub struct RepliesArgs {
    #[arg(long)]
    pub thread: String,
    /// Optional exact starter receipt ID check; never reads another thread.
    #[arg(long)]
    pub message: Option<String>,
    /// Optional extra exact association check.
    #[arg(long)]
    pub request_id: Option<String>,
    #[arg(long)]
    pub todo_id: Option<u64>,
    /// This command is always read-only, including its local ledger.
    #[arg(long)]
    pub read_only: bool,
}

#[derive(Args, Debug)]
pub struct CloseArgs {
    /// Reviewed ID/hash-only binding to an existing applied receipt and reply.
    #[arg(long)]
    pub manifest: PathBuf,
    /// Earlier RFC3339 text of the reviewed reply timestamp. It is accepted only when it
    /// is the same instant as the Discord API timestamp and reproduces the manifest hash.
    #[arg(long)]
    pub legacy_reply_timestamp: Option<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct CloseBinding {
    request_id: String,
    thread_id: String,
    question_message_id: String,
    origin_todo_id: u64,
    todo_id: u64,
    message_id: String,
    author_id: String,
    job_id: String,
    revision: String,
    question_fingerprint: String,
    reply_sha256: String,
    receipt_sha256: String,
    verification_sha256: String,
    /// Exact completion statement: no inference from mere receipt existence.
    completion: String,
}

const CLOSE_LEDGER: &str = "recovery-close.json";
const CLOSE_LOCK: &str = "recovery-close.lock";
const EQUIVALENCE_LEDGER: &str = "recovery-close-equivalence.json";

/// ID/hash/time-only evidence that one reviewed reply hash was made from an earlier text form
/// of the same timestamp instant. The close binding and applied receipt bytes are never changed.
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct TimestampEquivalence {
    kind: String,
    request_id: String,
    thread_id: String,
    message_id: String,
    author_id: String,
    origin_todo_id: u64,
    todo_id: u64,
    api_timestamp: String,
    legacy_timestamp: String,
    instant_utc: String,
    api_reply_sha256: String,
    binding_reply_sha256: String,
    receipt_sha256: String,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct EquivalenceLedger {
    domain: String,
    version: u32,
    records: BTreeMap<String, TimestampEquivalence>,
}

impl EquivalenceLedger {
    fn load(home: &Path) -> Result<Self> {
        match fs::symlink_metadata(home.join(EQUIVALENCE_LEDGER)) {
            Ok(m) if !m.is_file() || m.file_type().is_symlink() => {
                return Err("equivalence storage must be an independent regular file".into());
            }
            Ok(_) => {}
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(_) => return Err("cannot inspect equivalence storage".into()),
        }
        match fs::File::open(home.join(EQUIVALENCE_LEDGER)) {
            Ok(file) => {
                let value: Self = serde_json::from_slice(&read_bounded(file, LEDGER_BYTES)?)
                    .map_err(|_| "equivalence ledger malformed; never treated as empty")?;
                if value.domain != DOMAIN
                    || value.version != 1
                    || value.records.iter().any(|(id, r)| id != &r.thread_id)
                {
                    return Err("equivalence ledger domain/version/record differs".into());
                }
                Ok(value)
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Self {
                domain: DOMAIN.into(),
                version: 1,
                records: BTreeMap::new(),
            }),
            Err(_) => Err("cannot read equivalence ledger".into()),
        }
    }

    /// Append-only per thread: an identical record is a no-op; a different one is refused.
    fn record(&mut self, home: &Path, evidence: &TimestampEquivalence) -> Result<()> {
        if let Some(saved) = self.records.get(&evidence.thread_id) {
            return if saved == evidence {
                Ok(())
            } else {
                Err("timestamp equivalence evidence differs from the retained record".into())
            };
        }
        self.records
            .insert(evidence.thread_id.clone(), evidence.clone());
        let bytes = serde_json::to_vec_pretty(self).map_err(|_| "cannot encode equivalence")?;
        let mut temp = tempfile::Builder::new()
            .prefix("recovery-close-equivalence.")
            .suffix(".tmp")
            .tempfile_in(home)
            .map_err(|_| "cannot stage equivalence record")?;
        temp.write_all(&bytes)
            .map_err(|_| "cannot write equivalence record")?;
        temp.as_file()
            .sync_all()
            .map_err(|_| "cannot flush equivalence record")?;
        core::replace_file(temp.path(), &home.join(EQUIVALENCE_LEDGER))
            .map_err(|_| "cannot publish equivalence record")?;
        #[cfg(not(windows))]
        fs::File::open(home)
            .and_then(|dir| dir.sync_all())
            .map_err(|_| "cannot flush equivalence directory")?;
        Ok(())
    }
}

enum ReplyMatch {
    Exact,
    Equivalent(Box<TimestampEquivalence>),
    Different,
}

/// The reply must equal the reviewed one. The only tolerated difference is the timestamp
/// text: the explicit earlier text must be the same instant, and the API reply with only that
/// text swapped in must reproduce the reviewed hash. Any other field difference stays rejected.
fn match_reply(reply: &Reply, binding: &CloseBinding, legacy: Option<&str>) -> Result<ReplyMatch> {
    let value = serde_json::to_value(reply).map_err(|_| "cannot encode owner reply")?;
    let api_hash = digest(&value)?;
    if api_hash == binding.reply_sha256 {
        return Ok(ReplyMatch::Exact);
    }
    let Some(legacy) = legacy else {
        return Ok(ReplyMatch::Different);
    };
    if legacy == reply.timestamp || legacy.len() > 64 {
        return Ok(ReplyMatch::Different);
    }
    let (Ok(before), Ok(after)) = (
        chrono::DateTime::parse_from_rfc3339(legacy),
        chrono::DateTime::parse_from_rfc3339(&reply.timestamp),
    ) else {
        return Ok(ReplyMatch::Different);
    };
    if before != after {
        return Ok(ReplyMatch::Different);
    }
    let mut earlier = value;
    earlier["timestamp"] = json!(legacy);
    if digest(&earlier)? != binding.reply_sha256 {
        return Ok(ReplyMatch::Different);
    }
    Ok(ReplyMatch::Equivalent(Box::new(TimestampEquivalence {
        kind: "reply_timestamp_text_same_instant".into(),
        request_id: binding.request_id.clone(),
        thread_id: binding.thread_id.clone(),
        message_id: binding.message_id.clone(),
        author_id: binding.author_id.clone(),
        origin_todo_id: binding.origin_todo_id,
        todo_id: binding.todo_id,
        api_timestamp: reply.timestamp.clone(),
        legacy_timestamp: legacy.into(),
        instant_utc: after
            .with_timezone(&chrono::Utc)
            .to_rfc3339_opts(chrono::SecondsFormat::Nanos, true),
        api_reply_sha256: api_hash,
        binding_reply_sha256: binding.reply_sha256.clone(),
        receipt_sha256: binding.receipt_sha256.clone(),
    })))
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct CloseRecord {
    binding: CloseBinding,
    phase: String,
    outcome: Option<String>,
    pending_reply_ids: Vec<String>,
    #[serde(default)]
    patch_attempted: bool,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct CloseLedger {
    domain: String,
    version: u32,
    records: BTreeMap<String, CloseRecord>,
}

impl CloseLedger {
    fn load(home: &Path) -> Result<Self> {
        for name in [CLOSE_LEDGER, CLOSE_LOCK] {
            match fs::symlink_metadata(home.join(name)) {
                Ok(m) if !m.is_file() || m.file_type().is_symlink() => {
                    return Err("close storage must be an independent regular file".into());
                }
                Ok(_) => {}
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
                Err(_) => return Err("cannot inspect close storage".into()),
            }
        }
        match fs::File::open(home.join(CLOSE_LEDGER)) {
            Ok(file) => {
                let value: Self = serde_json::from_slice(&read_bounded(file, LEDGER_BYTES)?)
                    .map_err(|_| "close ledger malformed; never treated as empty")?;
                if value.domain != DOMAIN
                    || value.version != 1
                    || value.records.iter().any(|(id, r)| {
                        id != &r.binding.thread_id
                            || !matches!(
                                r.phase.as_str(),
                                "patch_pending" | "verified" | "reply_pending"
                            )
                    })
                {
                    return Err("close ledger domain/version/record differs".into());
                }
                Ok(value)
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Self {
                domain: DOMAIN.into(),
                version: 1,
                records: BTreeMap::new(),
            }),
            Err(_) => Err("cannot read close ledger".into()),
        }
    }

    fn save(&mut self, home: &Path, record: CloseRecord) -> Result<()> {
        self.records
            .insert(record.binding.thread_id.clone(), record);
        let bytes = serde_json::to_vec_pretty(self).map_err(|_| "cannot encode close record")?;
        if bytes.len() > LEDGER_BYTES {
            return Err("close ledger capacity exceeded".into());
        }
        let mut temp = tempfile::Builder::new()
            .prefix("recovery-close.")
            .suffix(".tmp")
            .tempfile_in(home)
            .map_err(|_| "cannot stage close record")?;
        temp.write_all(&bytes)
            .map_err(|_| "cannot write close record")?;
        temp.as_file()
            .sync_all()
            .map_err(|_| "cannot flush close record")?;
        core::replace_file(temp.path(), &home.join(CLOSE_LEDGER))
            .map_err(|_| "cannot publish close record")?;
        #[cfg(not(windows))]
        fs::File::open(home)
            .and_then(|dir| dir.sync_all())
            .map_err(|_| "cannot flush close directory")?;
        Ok(())
    }
}

fn close_application(home: &Path, binding: &CloseBinding) -> Result<Value> {
    request_key(&binding.request_id)?;
    request_key(&binding.job_id)?;
    for id in [
        &binding.thread_id,
        &binding.question_message_id,
        &binding.message_id,
        &binding.author_id,
    ] {
        snowflake(id)?;
    }
    if binding.thread_id != binding.question_message_id
        || binding.todo_id == 0
        || binding.origin_todo_id == 0
        || binding.revision.len() != 40
        || !binding.revision.bytes().all(|c| c.is_ascii_hexdigit())
        || binding.completion.trim().is_empty()
    {
        return Err("close binding is incomplete".into());
    }
    for hash in [
        &binding.question_fingerprint,
        &binding.reply_sha256,
        &binding.receipt_sha256,
        &binding.verification_sha256,
    ] {
        if hash.len() != 64 || !hash.bytes().all(|c| c.is_ascii_hexdigit()) {
            return Err("close binding requires exact SHA256 evidence".into());
        }
    }
    // Never accept an arbitrary receipt file, nor create one when absent.
    let file = fs::File::open(
        home.join("unattended/applied")
            .join(format!("{}.json", binding.message_id)),
    )
    .map_err(|_| "existing applied receipt is unavailable")?;
    let bytes = read_bounded(file, TEXT_BYTES)?;
    if format!("{:x}", Sha256::digest(&bytes)) != binding.receipt_sha256 {
        return Err("applied receipt bytes differ from reviewed evidence".into());
    }
    let value: Value = serde_json::from_slice(&bytes).map_err(|_| "applied receipt malformed")?;
    if value["source"] != "discord-api-v10"
        || value["request_id"] != binding.request_id
        || value["thread_id"] != binding.thread_id
        || value["question_message_id"] != binding.question_message_id
        || value["todo_id"] != binding.todo_id
        || value["origin_todo_id"] != binding.origin_todo_id
        || value["message_id"] != binding.message_id
        || value["author_id"] != binding.author_id
        || value["job_id"] != binding.job_id
        || value["revision"] != binding.revision
        || value["completion"] != binding.completion
        || value["verification"]
            .as_object()
            .is_none_or(|v| v.is_empty())
        || ["execution", "origin_saved", "identity"].iter().any(|key| {
            value["verification"][*key]
                .as_str()
                .is_none_or(|v| v.trim().is_empty())
        })
        || digest(&value["verification"])? != binding.verification_sha256
    {
        return Err("applied receipt association or execution verification differs".into());
    }
    chrono::DateTime::parse_from_rfc3339(core::s(&value["applied_at"]))
        .map_err(|_| "applied receipt timestamp missing")?;
    Ok(value)
}

#[derive(Clone, Copy)]
struct CloseEnv<'a> {
    home: &'a Path,
    /// Explicit (or retained) earlier text of the reviewed reply timestamp.
    legacy: Option<&'a str>,
}

fn close_replies(
    api: &mut RecoveryApi<'_>,
    ledger: &Ledger,
    request: &Request,
    binding: &CloseBinding,
    applied: &Value,
    state: &mut CloseLedger,
    env: CloseEnv<'_>,
) -> Result<bool> {
    let CloseEnv { home, legacy } = env;
    let replies = owner_replies(api, ledger, request)?;
    let raw = api.get(&format!(
        "/channels/{}/messages/{}",
        binding.thread_id, binding.message_id
    ))?;
    let selected = replies.iter().find(|r| r.message_id == binding.message_id);
    let matched = match selected {
        Some(r) if r.author_id == binding.author_id => match_reply(r, binding, legacy)?,
        _ => ReplyMatch::Different,
    };
    let exact = !matches!(matched, ReplyMatch::Different);
    let raw_matches = selected.is_some_and(|r| {
        raw["id"] == r.message_id
            && raw["channel_id"] == r.question_thread_id
            && raw["author"]["id"] == r.author_id
            && raw["author"]["bot"] != true
            && raw["content"] == r.body
            && raw["timestamp"] == r.timestamp
            && matches!(raw["type"].as_u64(), Some(0 | 19))
            && raw.get("webhook_id").is_none_or(Value::is_null)
    });
    let edited = raw.get("edited_timestamp").is_some_and(|v| !v.is_null());
    // The reviewed receipt covers exactly one reply. A later reply is not implied applied.
    let mut pending: Vec<_> = replies
        .iter()
        .filter(|r| r.message_id != binding.message_id)
        .map(|r| r.message_id.clone())
        .collect();
    if !exact || !raw_matches || edited {
        pending.push(binding.message_id.clone());
    }
    if !pending.is_empty() {
        let prior = state.records.get(&binding.thread_id).cloned();
        state.save(
            home,
            CloseRecord {
                binding: binding.clone(),
                phase: "reply_pending".into(),
                outcome: prior.as_ref().and_then(|r| r.outcome.clone()),
                pending_reply_ids: pending,
                patch_attempted: prior.as_ref().is_some_and(|r| r.patch_attempted),
            },
        )?;
        return Err("RecoveryCloseReplyPending: unapplied or edited owner reply retained; parent must inspect recovery-replies".into());
    }
    if let ReplyMatch::Equivalent(evidence) = &matched {
        // Retained before any PATCH, so the audit trail exists even if later steps fail.
        EquivalenceLedger::load(home)?.record(home, evidence)?;
    }
    let applied_at = chrono::DateTime::parse_from_rfc3339(core::s(&applied["applied_at"]))
        .map_err(|_| "invalid application time")?;
    let reply_at =
        chrono::DateTime::parse_from_rfc3339(&selected.ok_or("applied reply missing")?.timestamp)
            .map_err(|_| "invalid reply time")?;
    if applied_at < reply_at {
        return Err("application predates owner reply".into());
    }
    Ok(matches!(matched, ReplyMatch::Equivalent(_)))
}

/// Close only an applied, exact new-domain question. No old-state load/save or dispatch.
pub fn close(ctx: &mut Context, args: &CloseArgs) -> Result<Value> {
    let binding: CloseBinding = serde_json::from_slice(&read_bounded(
        fs::File::open(&args.manifest).map_err(|_| "close manifest unavailable")?,
        TEXT_BYTES,
    )?)
    .map_err(|_| "close manifest malformed")?;
    ensure_storage(&ctx.here)?;
    let _ = CloseLedger::load(&ctx.here)?; // inspect files before acquiring their locks
    let _close_lock = ByteLock::acquire(&ctx.here.join(CLOSE_LOCK), Duration::ZERO)?;
    let mut state = CloseLedger::load(&ctx.here)?;
    let reused = state.records.contains_key(&binding.thread_id);
    let _messages_lock = ByteLock::acquire(&ctx.here.join(LOCK), Duration::ZERO)?;
    let ledger = Ledger::load(&ctx.here)?;
    if state
        .records
        .get(&binding.thread_id)
        .is_some_and(|r| r.binding != binding)
    {
        return Err("close retry binding differs; same receipt must be retained".into());
    }
    let applied = close_application(&ctx.here, &binding)?;
    // An explicit earlier timestamp text, or the one already retained for this exact reply.
    let legacy = match &args.legacy_reply_timestamp {
        Some(text) => Some(text.clone()),
        None => EquivalenceLedger::load(&ctx.here)?
            .records
            .get(&binding.thread_id)
            .filter(|r| {
                r.message_id == binding.message_id
                    && r.binding_reply_sha256 == binding.reply_sha256
                    && r.receipt_sha256 == binding.receipt_sha256
            })
            .map(|r| r.legacy_timestamp.clone()),
    };
    let destination = Destination::from_context(ctx)?;
    let request = ledger
        .request(&binding.request_id)
        .ok_or("new recovery question not found")?;
    let starter = ledger
        .receipt(&request.request_id, "question:0")
        .ok_or("question not GET verified")?;
    if request.request_id != binding.request_id
        || request.destination != destination
        || binding.author_id != destination.owner
        || request.fingerprint != binding.question_fingerprint
        || !ledger.complete(&request.request_id)
        || starter.thread_id != binding.thread_id
        || starter.message_id != binding.question_message_id
        || !matches!(&request.operation,Kind::Ask{todo_id,..} if *todo_id == binding.origin_todo_id)
    {
        return Err("close question generation/owner/todo/scope differs".into());
    }
    let bot = ledger
        .bot(&request.request_id)
        .ok_or("question bot missing")?;
    let me = ctx
        .effects
        .http("GET", &format!("{API}/users/@me"), None, true)
        .map_err(|_| "cannot verify close bot")?;
    if me["bot"] != true || me["id"] != bot {
        return Err("close bot differs".into());
    }
    let mut api = RecoveryApi {
        effects: ctx.effects.as_mut(),
        destination,
        bot: bot.into(),
    };
    let forum = api.get(&format!("/channels/{}", api.destination.forum))?;
    if forum["id"] != api.destination.forum
        || forum["guild_id"] != api.destination.guild
        || forum["type"] != 15
    {
        return Err("close configured forum differs".into());
    }
    let mut equivalent = close_replies(
        &mut api,
        &ledger,
        request,
        &binding,
        &applied,
        &mut state,
        CloseEnv {
            home: &ctx.here,
            legacy: legacy.as_deref(),
        },
    )?;
    let channel = api.channel(&binding.thread_id)?;
    let archived = channel["thread_metadata"]["archived"]
        .as_bool()
        .ok_or("archive state missing")?;
    let prior = state.records.get(&binding.thread_id).cloned();
    let outcome = if archived {
        if let Some(outcome) = prior.as_ref().and_then(|r| r.outcome.clone()) {
            outcome
        } else if prior
            .as_ref()
            .is_some_and(|r| r.patch_attempted || r.phase == "patch_pending")
        {
            "recovered_after_patch".into()
        } else {
            "already_archived".into()
        }
    } else {
        if prior
            .as_ref()
            .is_some_and(|r| r.phase == "verified" || r.outcome.is_some())
        {
            return Err(
                "verified question was reopened; explicit parent inspection required".into(),
            );
        }
        close_application(&ctx.here, &binding)?;
        state.save(
            &ctx.here,
            CloseRecord {
                binding: binding.clone(),
                phase: "patch_pending".into(),
                outcome: None,
                pending_reply_ids: vec![],
                patch_attempted: true,
            },
        )?;
        // An error is an unknown outcome. GET, never the PATCH response, determines success.
        let response = api.effects.http(
            "PATCH",
            &format!("{API}/channels/{}", binding.thread_id),
            Some(&json!({"archived":true})),
            true,
        );
        if response.is_ok() {
            "archived".into()
        } else {
            "recovered_after_patch".into()
        }
    };
    api.assert_bot_thread(&binding.thread_id)?;
    if api.channel(&binding.thread_id)?["thread_metadata"]["archived"] != true {
        return Err(
            "RecoveryClosePending: GET has not confirmed archived; retry same receipt".into(),
        );
    }
    equivalent |= close_replies(
        &mut api,
        &ledger,
        request,
        &binding,
        &applied,
        &mut state,
        CloseEnv {
            home: &ctx.here,
            legacy: legacy.as_deref(),
        },
    )?;
    // Check the same bytes were applied throughout this attempt.
    close_application(&ctx.here, &binding)?;
    state.save(
        &ctx.here,
        CloseRecord {
            binding: binding.clone(),
            phase: "verified".into(),
            outcome: Some(outcome.clone()),
            pending_reply_ids: vec![],
            patch_attempted: !archived
                || prior
                    .as_ref()
                    .is_some_and(|r| r.patch_attempted || r.phase == "patch_pending"),
        },
    )?;
    Ok(
        json!({"domain":DOMAIN,"request_id":binding.request_id,"thread_id":binding.thread_id,
        "question_message_id":binding.question_message_id,"message_id":binding.message_id,
        "todo_id":binding.todo_id,"origin_todo_id":binding.origin_todo_id,"job_id":binding.job_id,
        "revision":binding.revision,"receipt_sha256":binding.receipt_sha256,"source":"discord-api-v10",
        "archived":true,"verification":"GET","outcome":outcome,"reused":reused,"legacy_state":"unchanged","dispatched":false,
        "reply_timestamp":if equivalent {"same_instant_text_equivalent"} else {"exact"}}),
    )
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Destination {
    guild: String,
    forum: String,
    owner: String,
    request_forum: String,
}

impl Destination {
    fn from_context(ctx: &Context) -> Result<Self> {
        let value = Self {
            guild: snowflake(core::s(&ctx.cfg["guild_id"]))?,
            forum: snowflake(core::s(&ctx.cfg["task_channel_id"]))?,
            owner: snowflake(&ctx.owner())?,
            request_forum: snowflake(
                ctx.cfg["request_channel_id"]
                    .as_str()
                    .unwrap_or(core::REQUEST_CHANNEL_ID),
            )?,
        };
        Ok(value)
    }
    fn url(&self, thread: &str) -> String {
        format!("https://discord.com/channels/{}/{thread}", self.guild)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct ImageReceipt {
    filename: String,
    mime: String,
    size: usize,
    sha256: String,
}

struct Image {
    receipt: ImageReceipt,
    bytes: Vec<u8>,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
enum Kind {
    Log {
        thread: Option<String>,
    },
    Ask {
        todo_id: u64,
        origin_thread: String,
        decision_original: Value,
        decision_identity: Value,
    },
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
struct Request {
    request_id: String,
    title: String,
    body: String,
    destination: Destination,
    notify: bool,
    images: Vec<ImageReceipt>,
    operation: Kind,
    fingerprint: String,
}

impl Request {
    fn fingerprint(&self) -> Result<String> {
        // The request ID is a retry key, not permission or presentation identity.
        // Raw decision metadata stays intact in the intent but only normalized
        // scope participates in deduplication.
        let operation = match &self.operation {
            Kind::Log { thread } => json!({"kind":"log","thread":thread}),
            Kind::Ask {
                decision_identity,
                origin_thread,
                ..
            } => {
                json!({"kind":"ask","decision_identity":decision_identity,"origin_thread":origin_thread})
            }
        };
        digest(
            &json!({"domain":DOMAIN,"destination":self.destination,"topic":self.title,
            "body":self.body,"notify":self.notify,"images":self.images,"operation":operation}),
        )
    }
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
enum Target {
    Forum { title: String },
    Thread { thread: String },
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
struct Effect {
    target: Target,
    content: String,
    notify: bool,
    images: Vec<ImageReceipt>,
}

impl Effect {
    fn payload(&self, owner: &str) -> Value {
        json!({"content":self.content,"flags":EMBEDS | if self.notify {0} else {SILENT},
            "allowed_mentions":{"parse":[],"users":if self.notify {vec![owner]} else {vec![]},
                "roles":[],"replied_user":false},"tts":false})
    }
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
struct Receipt {
    thread_id: String,
    message_id: String,
    author_id: String,
    timestamp: String,
    flags: u64,
    owner_mentions: usize,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
struct Reply {
    request_id: String,
    todo_id: u64,
    question_thread_id: String,
    question_message_id: String,
    author_id: String,
    message_id: String,
    timestamp: String,
    body: String,
    decision_identity: Value,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(tag = "event", rename_all = "snake_case", deny_unknown_fields)]
enum Event {
    Intent {
        request: Box<Request>,
    },
    /// Only an unsent question's mistaken question-thread origin can be moved
    /// to that exact question's already verified silent progress thread.
    OriginCorrected {
        request: Box<Request>,
    },
    Alias {
        request_id: String,
        canonical_request_id: String,
        fingerprint: String,
    },
    Bot {
        request_id: String,
        author_id: String,
    },
    OriginRepair {
        request_id: String,
        todo_id: u64,
        expected_origin_thread: String,
        owner_reply_message: String,
        progress_request_id: String,
        progress_thread: String,
        progress_message: String,
        original_fingerprint: String,
    },
    Pending {
        request_id: String,
        phase: String,
        effect: Effect,
    },
    Posted {
        request_id: String,
        phase: String,
        thread_id: String,
        message_id: String,
    },
    Verified {
        request_id: String,
        phase: String,
        receipt: Receipt,
    },
    DeliveryVerified {
        request_id: String,
    },
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Ledger {
    domain: String,
    version: u32,
    events: Vec<Event>,
}

impl Ledger {
    fn load(home: &Path) -> Result<Self> {
        ensure_storage(home)?;
        let path = home.join(LEDGER);
        let bytes = match fs::File::open(path) {
            Ok(file) => read_bounded(file, LEDGER_BYTES)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                return Ok(Self {
                    domain: DOMAIN.into(),
                    version: 1,
                    events: vec![],
                });
            }
            Err(_) => return Err("recovery ledger cannot be read".into()),
        };
        let ledger: Self = serde_json::from_slice(&bytes)
            .map_err(|_| "recovery ledger is malformed; never treated as empty")?;
        if ledger.domain != DOMAIN || ledger.version != 1 {
            return Err("recovery ledger domain/version differs".into());
        }
        ledger.validate()?;
        Ok(ledger)
    }

    fn append(&mut self, home: &Path, event: Event) -> Result<()> {
        ensure_storage(home)?;
        self.events.push(event);
        self.validate()?;
        let bytes =
            serde_json::to_vec_pretty(self).map_err(|_| "cannot encode recovery receipt")?;
        if bytes.len() > LEDGER_BYTES {
            return Err("recovery ledger capacity exceeded; history retained".into());
        }
        // The temporary file and replacement belong exclusively to this domain.
        let mut temp = tempfile::Builder::new()
            .prefix("recovery-messages.")
            .suffix(".tmp")
            .tempfile_in(home)
            .map_err(|_| "cannot stage recovery receipt")?;
        temp.write_all(&bytes)
            .map_err(|_| "cannot write recovery receipt")?;
        temp.as_file()
            .sync_all()
            .map_err(|_| "cannot flush recovery receipt")?;
        core::replace_file(temp.path(), &home.join(LEDGER))
            .map_err(|_| "cannot publish recovery receipt; no new HTTP allowed")?;
        #[cfg(not(windows))]
        fs::File::open(home)
            .and_then(|dir| dir.sync_all())
            .map_err(|_| "cannot flush recovery receipt directory")?;
        Ok(())
    }

    fn request(&self, id: &str) -> Option<&Request> {
        let canonical = self
            .events
            .iter()
            .find_map(|event| match event {
                Event::Alias {
                    request_id,
                    canonical_request_id,
                    ..
                } if request_id == id => Some(canonical_request_id.as_str()),
                _ => None,
            })
            .unwrap_or(id);
        self.events.iter().rev().find_map(|event| match event {
            Event::Intent { request } | Event::OriginCorrected { request }
                if request.request_id == canonical =>
            {
                Some(request.as_ref())
            }
            _ => None,
        })
    }

    fn bot(&self, id: &str) -> Option<&str> {
        self.events.iter().find_map(|event| match event {
            Event::Bot {
                request_id,
                author_id,
            } if request_id == id => Some(author_id.as_str()),
            _ => None,
        })
    }

    fn effect(&self, id: &str, phase: &str) -> Option<&Effect> {
        self.events.iter().find_map(|event| match event {
            Event::Pending {
                request_id,
                phase: p,
                effect,
            } if request_id == id && p == phase => Some(effect),
            _ => None,
        })
    }

    fn posted(&self, id: &str, phase: &str) -> Option<(String, String)> {
        self.events.iter().find_map(|event| match event {
            Event::Posted {
                request_id,
                phase: p,
                thread_id,
                message_id,
            } if request_id == id && p == phase => Some((thread_id.clone(), message_id.clone())),
            _ => None,
        })
    }

    fn receipt(&self, id: &str, phase: &str) -> Option<&Receipt> {
        self.events.iter().find_map(|event| match event {
            Event::Verified {
                request_id,
                phase: p,
                receipt,
            } if request_id == id && p == phase => Some(receipt),
            _ => None,
        })
    }

    fn complete(&self, id: &str) -> bool {
        self.events
            .iter()
            .any(|e| matches!(e, Event::DeliveryVerified { request_id } if request_id == id))
    }

    fn repaired_origin(&self, id: &str) -> Option<&str> {
        self.events.iter().find_map(|event| match event {
            Event::OriginRepair {
                request_id,
                progress_thread,
                ..
            } if request_id == id => Some(progress_thread.as_str()),
            _ => None,
        })
    }

    fn effective_request(&self, request: &Request) -> Result<Request> {
        let Some(origin) = self.repaired_origin(&request.request_id) else {
            return Ok(request.clone());
        };
        let mut effective = request.clone();
        let Kind::Ask { origin_thread, .. } = &mut effective.operation else {
            return Err("origin repair is attached to a non-question request".into());
        };
        *origin_thread = origin.to_owned();
        effective.fingerprint = effective.fingerprint()?;
        Ok(effective)
    }

    fn validate(&self) -> Result<()> {
        let mut requests = BTreeMap::new();
        let mut aliases = BTreeMap::new();
        let mut bots: BTreeMap<String, String> = BTreeMap::new();
        let mut phases: BTreeMap<(String, String), &Effect> = BTreeMap::new();
        let mut posted = BTreeMap::new();
        let mut verified: BTreeMap<(String, String), &Receipt> = BTreeMap::new();
        let mut complete = BTreeMap::new();
        let mut origin_repairs = BTreeMap::new();
        for event in &self.events {
            match event {
                Event::Intent { request } => {
                    request_key(&request.request_id)?;
                    if request.fingerprint != request.fingerprint()?
                        || requests
                            .insert(request.request_id.clone(), request)
                            .is_some()
                        || aliases.contains_key(&request.request_id)
                    {
                        return Err("invalid or duplicate recovery intent".into());
                    }
                }
                Event::OriginCorrected { request } => {
                    let prior = requests
                        .get(&request.request_id)
                        .ok_or("origin correction has no original intent")?;
                    if !only_origin_changed(prior, request)?
                        || phases.keys().any(|(id, _)| id == &request.request_id)
                        || complete.contains_key(&request.request_id)
                        || origin_repairs.contains_key(&request.request_id)
                    {
                        return Err(
                            "origin correction changed scope or follows a possible POST".into()
                        );
                    }
                    let (old_origin, new_origin) = ask_origins(prior, request)?;
                    let linked = verified.iter().any(|((question_id, phase), receipt)| {
                        phase == "question:0"
                            && receipt.thread_id == old_origin
                            && requests
                                .get(question_id)
                                .is_some_and(|original| original.destination == request.destination)
                            && bots.get(question_id) == Some(&receipt.author_id)
                            && verified
                                .get(&(question_id.clone(), "progress".into()))
                                .is_some_and(|progress| {
                                    progress.thread_id == new_origin
                                        && progress.author_id == receipt.author_id
                                        && progress.flags & SILENT != 0
                                        && progress.owner_mentions == 0
                                })
                    });
                    if !linked {
                        return Err(
                            "origin correction lacks the exact question/progress receipts".into(),
                        );
                    }
                    requests.insert(request.request_id.clone(), request);
                }
                Event::Alias {
                    request_id,
                    canonical_request_id,
                    fingerprint,
                } => {
                    request_key(request_id)?;
                    if fingerprint.len() != 64
                        || !fingerprint.bytes().all(|c| c.is_ascii_hexdigit())
                        || !requests.contains_key(canonical_request_id)
                        || requests.contains_key(request_id)
                        || aliases
                            .insert(request_id.clone(), canonical_request_id)
                            .is_some()
                    {
                        return Err("invalid recovery alias".into());
                    }
                }
                Event::Bot {
                    request_id,
                    author_id,
                } => {
                    snowflake(author_id)?;
                    if !requests.contains_key(request_id)
                        || bots.insert(request_id.clone(), author_id.clone()).is_some()
                    {
                        return Err("invalid recovery bot receipt".into());
                    }
                }
                Event::OriginRepair {
                    request_id,
                    todo_id,
                    expected_origin_thread,
                    owner_reply_message,
                    progress_request_id,
                    progress_thread,
                    progress_message,
                    original_fingerprint,
                } => {
                    let request = requests
                        .get(request_id)
                        .ok_or("origin repair has no original intent")?;
                    let Kind::Ask {
                        todo_id: original_todo,
                        origin_thread,
                        ..
                    } = &request.operation
                    else {
                        return Err("origin repair requires an original question".into());
                    };
                    snowflake(expected_origin_thread)?;
                    snowflake(owner_reply_message)?;
                    snowflake(progress_thread)?;
                    snowflake(progress_message)?;
                    let source = verified
                        .get(&(progress_request_id.clone(), "progress".to_owned()))
                        .ok_or("origin repair target is not a verified progress post")?;
                    let source_request = requests
                        .get(progress_request_id)
                        .ok_or("origin repair progress request is unavailable")?;
                    let related_origin = matches!(
                        (&source_request.operation, &request.operation),
                        (
                            Kind::Ask {
                                todo_id: source_todo,
                                origin_thread: source_origin,
                                ..
                            },
                            Kind::Ask { todo_id: target_todo, .. }
                        ) if source_todo == target_todo && source_origin == expected_origin_thread
                    );
                    if *todo_id == 0
                        || todo_id != original_todo
                        || expected_origin_thread != origin_thread
                        || !related_origin
                        || owner_reply_message == expected_origin_thread
                        || source.thread_id != *progress_thread
                        || source.message_id != *progress_message
                        || source.author_id
                            != *bots
                                .get(progress_request_id)
                                .ok_or("progress author is missing")?
                        || bots.get(progress_request_id) != bots.get(request_id)
                        || source_request.destination != request.destination
                        || original_fingerprint != &request.fingerprint
                        || !bots.contains_key(request_id)
                        || phases.keys().any(|(id, _)| id == request_id)
                        || posted.keys().any(|(id, _)| id == request_id)
                        || verified.keys().any(|(id, _)| id == request_id)
                        || complete.contains_key(request_id)
                        || origin_repairs.insert(request_id.clone(), true).is_some()
                    {
                        return Err("origin repair does not match the exact unsent request".into());
                    }
                }
                Event::Pending {
                    request_id,
                    phase,
                    effect,
                } => {
                    if !bots.contains_key(request_id)
                        || phases
                            .insert((request_id.clone(), phase.clone()), effect)
                            .is_some()
                        || complete.contains_key(request_id)
                    {
                        return Err("invalid recovery effect reservation".into());
                    }
                }
                Event::Posted {
                    request_id,
                    phase,
                    thread_id,
                    message_id,
                } => {
                    snowflake(thread_id)?;
                    snowflake(message_id)?;
                    if !phases.contains_key(&(request_id.clone(), phase.clone()))
                        || posted
                            .insert((request_id.clone(), phase.clone()), true)
                            .is_some()
                    {
                        return Err("invalid recovery POST receipt".into());
                    }
                }
                Event::Verified {
                    request_id,
                    phase,
                    receipt,
                } => {
                    snowflake(&receipt.thread_id)?;
                    snowflake(&receipt.message_id)?;
                    if !phases.contains_key(&(request_id.clone(), phase.clone()))
                        || bots.get(request_id) != Some(&receipt.author_id)
                        || verified
                            .insert((request_id.clone(), phase.clone()), receipt)
                            .is_some()
                    {
                        return Err("invalid recovery GET receipt".into());
                    }
                }
                Event::DeliveryVerified { request_id } => {
                    if !requests.contains_key(request_id)
                        || complete.insert(request_id.clone(), true).is_some()
                        || !phases.keys().any(|(id, _)| id == request_id)
                        || phases
                            .keys()
                            .filter(|(id, _)| id == request_id)
                            .any(|key| !verified.contains_key(key))
                    {
                        return Err("unconfirmed recovery delivery".into());
                    }
                }
            }
        }
        Ok(())
    }
}

fn ensure_storage(home: &Path) -> Result<()> {
    if !fs::metadata(home)
        .map_err(|_| "recovery message home is unavailable")?
        .is_dir()
    {
        return Err("recovery message home must already exist".into());
    }
    for name in [LEDGER, LOCK] {
        match fs::symlink_metadata(home.join(name)) {
            Ok(meta) if !meta.is_file() || meta.file_type().is_symlink() => {
                return Err("recovery storage must be an independent regular file".into());
            }
            Ok(_) => {}
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(_) => return Err("cannot inspect recovery message storage".into()),
        }
    }
    Ok(())
}

fn read_bounded(reader: impl Read, limit: usize) -> Result<Vec<u8>> {
    let mut bytes = Vec::new();
    reader
        .take(limit as u64 + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "cannot read recovery input")?;
    if bytes.len() > limit {
        return Err("recovery input exceeds its size limit".into());
    }
    Ok(bytes)
}

fn request_key(value: &str) -> Result<()> {
    if value.is_empty()
        || value.len() > 128
        || !value
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || b"-_.:".contains(&c))
    {
        return Err("explicit bounded recovery request-id required".into());
    }
    Ok(())
}

fn snowflake(value: &str) -> Result<String> {
    if value.is_empty()
        || value.len() > 20
        || !value.bytes().all(|c| c.is_ascii_digit())
        || value.parse::<u64>().ok().filter(|n| *n > 0).is_none()
    {
        return Err("exact numeric Discord ID required".into());
    }
    Ok(value.into())
}

fn exact_owner_reply(message: &Value, thread: &str, message_id: &str, owner: &str) -> bool {
    message["id"] == message_id
        && message["channel_id"] == thread
        && message["author"]["id"] == owner
        && message["author"]["bot"] != true
        && message
            .get("webhook_id")
            .is_none_or(|value| value.is_null())
        && matches!(message["type"].as_u64(), Some(0 | 19))
        && message["content"].as_str().is_some()
}

fn canonical(value: &Value) -> Value {
    match value {
        Value::Object(map) => Value::Object(
            map.iter()
                .map(|(k, v)| (k.clone(), canonical(v)))
                .collect::<BTreeMap<_, _>>()
                .into_iter()
                .collect(),
        ),
        Value::Array(values) => Value::Array(values.iter().map(canonical).collect()),
        other => other.clone(),
    }
}

fn digest(value: &Value) -> Result<String> {
    Ok(format!(
        "{:x}",
        Sha256::digest(
            serde_json::to_vec(&canonical(value))
                .map_err(|_| "cannot fingerprint recovery input")?
        )
    ))
}

struct SecretCheck {
    known: Vec<String>,
    pattern: regex::Regex,
}

impl SecretCheck {
    fn new() -> Self {
        let names = regex::Regex::new("(?i)TOKEN|PASSWORD|SECRET|API_KEY|WEBHOOK").unwrap();
        let mut known: Vec<_> = std::env::vars()
            .filter(|(name, value)| names.is_match(name) && value.len() >= 8)
            .map(|(_, value)| value)
            .collect();
        if let Ok(value) = core::token() {
            known.push(value);
        }
        Self {
            known,
            pattern: regex::Regex::new(concat!(
                r#"(?i)authorization["']?\s*[:=]\s*["']?(?:Bearer|Bot)\s+\S+|"#,
                r#"(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|passwd|pwd|secret|bot[_-]?token|private[_-]?key|connection[_-]?string|_auth)["']?\s*[:=]\s*["']?[^\s"',}]+|"#,
                r#"[a-z][a-z0-9+.-]*://[^\s/"'<>:@]*:[^\s/"'<>@]+@|"#,
                r"-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY|",
                r"https://(?:discord\.com|discordapp\.com)/api/webhooks/|",
                r"\b(?:sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[A-Z0-9]{16}|mfa\.[A-Za-z0-9_-]{40,})\b"
            )).unwrap(),
        }
    }
    fn text(&self, value: &str) -> Result<()> {
        if self
            .known
            .iter()
            .any(|secret| !secret.is_empty() && value.contains(secret))
            || self.pattern.is_match(value)
        {
            return Err(
                "credential-like input is excluded from recovery messages and receipts".into(),
            );
        }
        Ok(())
    }
    fn bytes(&self, bytes: &[u8]) -> Result<()> {
        self.text(&String::from_utf8_lossy(bytes).replace('\0', ""))?;
        for skip in 0..2 {
            for little in [true, false] {
                let words: Vec<_> = bytes
                    .get(skip..)
                    .unwrap_or_default()
                    .as_chunks::<2>()
                    .0
                    .iter()
                    .map(|b| {
                        if little {
                            u16::from_le_bytes([b[0], b[1]])
                        } else {
                            u16::from_be_bytes([b[0], b[1]])
                        }
                    })
                    .collect();
                self.text(&String::from_utf16_lossy(&words).replace('\0', ""))?;
            }
        }
        Ok(())
    }
}

fn text_input(path: &Path, secrets: &SecretCheck) -> Result<String> {
    let bytes = read_bounded(
        fs::File::open(path).map_err(|_| "cannot read recovery text input")?,
        TEXT_BYTES,
    )?;
    let text = String::from_utf8(bytes)
        .map_err(|_| "recovery input must be UTF-8")?
        .trim_start_matches('\u{feff}')
        .to_owned();
    secrets.text(&text)?;
    if text.trim().is_empty() {
        return Err("recovery body must be nonempty".into());
    }
    Ok(text)
}

fn no_mentions(text: &str) -> Result<()> {
    if regex::Regex::new(r"<@(?:!|&)?[^>]*>|@everyone|@here")
        .unwrap()
        .is_match(text)
    {
        return Err(
            "input must have no mentions; recovery supplies the owner mention exactly once".into(),
        );
    }
    Ok(())
}

fn title_input(text: &str, secrets: &SecretCheck) -> Result<String> {
    secrets.text(text)?;
    no_mentions(text)?;
    let text = text.trim();
    if text.is_empty() || text.chars().count() > 100 || text.chars().any(char::is_control) {
        return Err("explicit recovery title must be 1..100 characters on one line".into());
    }
    Ok(text.into())
}

// One newly generated, visually inspected owner UI proposal has independent
// provenance even while the original exclusion file is missing. This bounded
// receipt is not a replacement deny list and never qualifies another image.
const UI_PROPOSAL_REQUEST: &str = "parallel-agent-progress-tui-2543-proposal-v1-20261013";
const UI_PROPOSAL_SOURCE: &str = "C:/Users/makhmeto/.codex/generated_images/01a120c7-be21-7b52-810d-d24af607e71e/exec-05d94163-1125-44e2-8920-91123f5c5825.png";
const UI_PROPOSAL_COPY: &str = "D:/CC/Work/parallel-agent-progress-tui-20261013/ui-proposal-v1.png";
const UI_PROPOSAL_SHA256: &str = "b4f333116dae118b3c9ca8d51892849d5552b48c009729256ebecfd76944a5d8";

fn image_path_key(path: &Path) -> String {
    path.to_string_lossy()
        .trim_start_matches(r"\\?\")
        .replace('\\', "/")
        .to_lowercase()
}

fn proposal_receipt_matches(
    request: &str,
    requested: &str,
    resolved: &str,
    source_resolved: &str,
    sha256: &str,
    now: chrono::DateTime<chrono::Utc>,
) -> bool {
    let starts = chrono::DateTime::parse_from_rfc3339("2026-10-13T00:00:00Z").unwrap();
    let expires = chrono::DateTime::parse_from_rfc3339("2026-10-21T00:00:00Z").unwrap();
    request == UI_PROPOSAL_REQUEST
        && now >= starts
        && now < expires
        && [
            UI_PROPOSAL_SOURCE.to_lowercase(),
            UI_PROPOSAL_COPY.to_lowercase(),
        ]
        .contains(&requested.to_owned())
        && requested == resolved
        && source_resolved == UI_PROPOSAL_SOURCE.to_lowercase()
        && sha256 == UI_PROPOSAL_SHA256
}

fn verify_generated_proposal(
    request: &str,
    path: &Path,
    resolved: &Path,
    bytes: &[u8],
) -> Result<()> {
    if request != UI_PROPOSAL_REQUEST || !path.is_absolute() {
        return Err("image protection list is unavailable; no image is sent".into());
    }
    let source = Path::new(UI_PROPOSAL_SOURCE);
    let source_resolved =
        fs::canonicalize(source).map_err(|_| "cannot verify original generated proposal")?;
    let source_bytes = read_bounded(
        fs::File::open(&source_resolved).map_err(|_| "cannot read original generated proposal")?,
        IMAGE_BYTES,
    )?;
    if bytes != source_bytes
        || !proposal_receipt_matches(
            request,
            &image_path_key(path),
            &image_path_key(resolved),
            &image_path_key(&source_resolved),
            &format!("{:x}", Sha256::digest(bytes)),
            chrono::Utc::now(),
        )
    {
        return Err(
            "generated proposal does not match its creation receipt; no image is sent".into(),
        );
    }
    Ok(())
}

fn conversation_media_request(request: &str) -> bool {
    request
        .strip_prefix("conversation-log-")
        .or_else(|| request.strip_prefix("conversation-v1-"))
        .is_some_and(|suffix| {
            suffix.len() == 32
                && suffix
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
        })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ConversationMediaVerification {
    domain: String,
    verified: bool,
    sha256: String,
    size: u64,
    mime: String,
    extension: String,
    #[serde(default)]
    question_qualification: Option<crate::question_images::Qualification>,
}

fn verify_conversation_image(
    ctx: &mut Context,
    request: &str,
    path: &Path,
    bytes: &[u8],
    mime: &str,
    extension: &str,
    scope: Option<&crate::question_images::Scope>,
) -> Result<()> {
    const FAILED: &str = "new conversation image verification unavailable; no image is sent";
    let mut args = vec![
        "D:/CC/Tools/discord-ask/unattended-runner.exe".into(),
        "--verify-conversation-media".into(),
        "--path".into(),
        path.to_str().ok_or(FAILED)?.into(),
        "--request-id".into(),
        request.into(),
    ];
    if let Some(scope) = scope {
        scope.append_args(&mut args)
    }
    let output = ctx.effects.run(&args, 20).map_err(|_| FAILED)?;
    if output.code != 0 || output.stdout.len() > 64 * 1024 {
        return Err(FAILED.into());
    }
    let proof: ConversationMediaVerification =
        serde_json::from_str(&output.stdout).map_err(|_| FAILED)?;
    if proof.domain != "discord-conversation-media-v1"
        || !proof.verified
        || proof.sha256 != format!("{:x}", Sha256::digest(bytes))
        || proof.size != bytes.len() as u64
        || proof.mime != mime
        || proof.extension != extension
    {
        return Err(FAILED.into());
    }
    if let Some(qualified) = proof.question_qualification {
        if !scope.is_some_and(|scope| qualified.matches(scope))
            || qualified.sha256 != proof.sha256
            || qualified.size != proof.size
            || qualified.mime != mime
            || qualified.extension != extension
        {
            return Err(FAILED.into());
        }
    }
    Ok(())
}

fn images(
    ctx: &mut Context,
    request: &str,
    paths: &[PathBuf],
    secrets: &SecretCheck,
    scope: Option<&crate::question_images::Scope>,
) -> Result<Vec<Image>> {
    if paths.is_empty() {
        return Ok(vec![]);
    }
    if paths.len() > 10 {
        return Err("at most ten recovery images are supported".into());
    }
    let conversation = conversation_media_request(request);
    // This deny list is read only into memory; never returned or copied to receipts.
    let clients = match fs::read_to_string(&ctx.client_projects) {
        Ok(clients) => Some(clients),
        Err(error)
            if error.kind() == std::io::ErrorKind::NotFound
                && (conversation || (request == UI_PROPOSAL_REQUEST && paths.len() == 1)) =>
        {
            None
        }
        Err(_) => return Err("image protection list is unavailable; no image is sent".into()),
    };
    let denied: Vec<_> = clients
        .as_deref()
        .into_iter()
        .flat_map(str::lines)
        .map(str::trim)
        .filter(|line| !line.is_empty() && !line.starts_with('#'))
        .map(|line| line.replace('\\', "/").to_lowercase())
        .collect();
    let mut result = Vec::new();
    let mut total = 0;
    for path in paths {
        let resolved = fs::canonicalize(path).map_err(|_| "cannot resolve recovery image")?;
        // This is a path/time preflight only. The original source and the
        // actual candidate bytes are independently verified below before HTTP.
        if clients.is_none()
            && !conversation
            && (!path.is_absolute()
                || !proposal_receipt_matches(
                    request,
                    &image_path_key(path),
                    &image_path_key(&resolved),
                    &UI_PROPOSAL_SOURCE.to_lowercase(),
                    UI_PROPOSAL_SHA256,
                    chrono::Utc::now(),
                ))
        {
            return Err("image is outside the generated proposal receipt; no image is sent".into());
        }
        for checked in [path, &resolved] {
            let key = checked.to_string_lossy().replace('\\', "/").to_lowercase();
            if denied.iter().any(|entry| key.contains(entry))
                || key.split('/').any(|part| {
                    part.starts_with(".env")
                        || [
                            ".ssh",
                            ".aws",
                            ".azure",
                            ".gnupg",
                            "credentials",
                            "secrets",
                            "id_rsa",
                            "id_ed25519",
                        ]
                        .contains(&part)
                        || part.contains("password")
                        || part.contains("secret")
                        || part.contains("token")
                })
            {
                return Err("protected files cannot be recovery attachments".into());
            }
        }
        let file = fs::File::open(&resolved).map_err(|_| "cannot read recovery image")?;
        if !file
            .metadata()
            .map_err(|_| "cannot inspect recovery image")?
            .is_file()
        {
            return Err("a recovery attachment must be a regular image file".into());
        }
        let bytes = read_bounded(file, IMAGE_BYTES)?;
        if clients.is_none() && !conversation {
            verify_generated_proposal(request, path, &resolved, &bytes)?;
        }
        let (mime, extension) = if bytes.starts_with(b"\x89PNG\r\n\x1a\n") {
            ("image/png", "png")
        } else if bytes.starts_with(b"\xff\xd8\xff") {
            ("image/jpeg", "jpg")
        } else if bytes.starts_with(b"GIF87a") || bytes.starts_with(b"GIF89a") {
            ("image/gif", "gif")
        } else if bytes.starts_with(b"RIFF") && bytes.get(8..12) == Some(&b"WEBP"[..]) {
            ("image/webp", "webp")
        } else if bytes.starts_with(b"BM") {
            ("image/bmp", "bmp")
        } else if bytes.starts_with(b"II\x2a\0") || bytes.starts_with(b"MM\0\x2a") {
            ("image/tiff", "tiff")
        } else {
            return Err("recovery attachments support images only".into());
        };
        secrets.bytes(&bytes)?;
        total += bytes.len();
        if total > UPLOAD_BYTES {
            return Err("recovery images exceed the total upload size limit".into());
        }
        if conversation {
            verify_conversation_image(ctx, request, path, &bytes, mime, extension, scope)?;
        }
        let sha256 = format!("{:x}", Sha256::digest(&bytes));
        result.push(Image {
            receipt: ImageReceipt {
                filename: format!("image-{}.{}", &sha256[..20], extension),
                mime: mime.into(),
                size: bytes.len(),
                sha256,
            },
            bytes,
        });
    }
    Ok(result)
}

fn ask_origins<'a>(prior: &'a Request, incoming: &'a Request) -> Result<(&'a str, &'a str)> {
    match (&prior.operation, &incoming.operation) {
        (
            Kind::Ask {
                origin_thread: old, ..
            },
            Kind::Ask {
                origin_thread: new, ..
            },
        ) if old != new => Ok((old, new)),
        _ => Err("origin correction requires two distinct question origins".into()),
    }
}

fn only_origin_changed(prior: &Request, incoming: &Request) -> Result<bool> {
    let (_, new_origin) = ask_origins(prior, incoming)?;
    let mut allowed = prior.clone();
    let Kind::Ask {
        origin_thread,
        decision_original,
        ..
    } = &mut allowed.operation
    else {
        return Ok(false);
    };
    *origin_thread = new_origin.into();
    // Preserve all original decision data except its explicit origin metadata.
    if decision_original.get("origin_thread_id").is_some() {
        decision_original["origin_thread_id"] = json!(new_origin);
    }
    if decision_original["scope"].get("origin_thread_id").is_some() {
        decision_original["scope"]["origin_thread_id"] = json!(new_origin);
    }
    allowed.fingerprint = allowed.fingerprint()?;
    Ok(allowed == *incoming)
}

fn correct_unposted_origin(
    ctx: &mut Context,
    ledger: &mut Ledger,
    incoming: &Request,
) -> Result<()> {
    let Some(prior) = ledger.request(&incoming.request_id).cloned() else {
        return Ok(());
    };
    // A semantic alias keeps its original canonical input. Normal reserve
    // checks its own bound fingerprint; aliases cannot correct that origin.
    if prior.request_id != incoming.request_id {
        return Ok(());
    }
    if prior.fingerprint == incoming.fingerprint {
        return Ok(());
    }
    // Aliases and already reserved effects remain immutable. This is a linkage
    // correction before any submit, never a new question generation or scope.
    if !only_origin_changed(&prior, incoming)? {
        return Err("request-id is already bound to different recovery input".into());
    }
    let correction = Event::OriginCorrected {
        request: Box::new(incoming.clone()),
    };
    let mut checked = Ledger {
        domain: ledger.domain.clone(),
        version: ledger.version,
        events: ledger.events.clone(),
    };
    checked.events.push(correction.clone());
    checked.validate()?;
    let (old_origin, new_origin) = ask_origins(&prior, incoming)?;
    let mut api = RecoveryApi::bind(ctx, ledger, &prior)?;
    api.assert_bot_thread(old_origin)?;
    api.assert_origin(ledger, new_origin)?;
    ledger.append(&ctx.here, correction)
}

fn reserve(ledger: &mut Ledger, home: &Path, incoming: &Request) -> Result<Request> {
    if let Some(existing) = ledger.request(&incoming.request_id) {
        let bound_fingerprint = ledger
            .events
            .iter()
            .find_map(|event| match event {
                Event::Alias {
                    request_id,
                    fingerprint,
                    ..
                } if request_id == &incoming.request_id => Some(fingerprint),
                _ => None,
            })
            .unwrap_or(&existing.fingerprint);
        if *bound_fingerprint != incoming.fingerprint {
            return Err("request-id is already bound to different recovery input".into());
        }
        return Ok(existing.clone());
    }
    let duplicate = ledger.events.iter().find_map(|event| {
        let Event::Intent { request } = event else {
            return None;
        };
        let same = match (&incoming.operation, &request.operation) {
            (
                Kind::Ask {
                    decision_identity: a,
                    ..
                },
                Kind::Ask {
                    decision_identity: b,
                    ..
                },
            ) => incoming.destination == request.destination && a == b,
            (Kind::Log { .. }, Kind::Log { .. }) => incoming.fingerprint == request.fingerprint,
            _ => false,
        };
        same.then(|| ledger.request(&request.request_id).cloned())
            .flatten()
    });
    if let Some(request) = duplicate {
        if request.images != incoming.images {
            let url = ledger
                .receipt(&request.request_id, "question:0")
                .map(|receipt| request.destination.url(&receipt.thread_id))
                .unwrap_or_else(|| "original question is still pending".into());
            return Err(format!("same decision is already bound to different question images: {url}; original question retained"));
        }
        ledger.append(
            home,
            Event::Alias {
                request_id: incoming.request_id.clone(),
                canonical_request_id: request.request_id.clone(),
                fingerprint: incoming.fingerprint.clone(),
            },
        )?;
        return Ok(request);
    }
    // The pending intent precedes even GET /users/@me or channel inspection.
    ledger.append(
        home,
        Event::Intent {
            request: Box::new(incoming.clone()),
        },
    )?;
    Ok(incoming.clone())
}

/// Narrow Discord transport for this independent domain. Arbitrary URLs/methods
/// are not exposed; every POST additionally requires the saved exact effect.
struct RecoveryApi<'a> {
    effects: &'a mut dyn core::Effects,
    destination: Destination,
    bot: String,
}

impl<'a> RecoveryApi<'a> {
    fn bind(ctx: &'a mut Context, ledger: &mut Ledger, request: &Request) -> Result<Self> {
        Self::bind_inner(ctx, ledger, request, true)
    }

    fn inspect(ctx: &'a mut Context, ledger: &mut Ledger, request: &Request) -> Result<Self> {
        Self::bind_inner(ctx, ledger, request, false)
    }

    fn bind_inner(
        ctx: &'a mut Context,
        ledger: &mut Ledger,
        request: &Request,
        persist: bool,
    ) -> Result<Self> {
        if Destination::from_context(ctx)? != request.destination {
            return Err("recovery destination changed; original receipt scope retained".into());
        }
        let me = ctx
            .effects
            .http("GET", &format!("{API}/users/@me"), None, true)
            .map_err(|_| "cannot verify recovery bot identity")?;
        let bot = snowflake(core::s(&me["id"]))?;
        if me["bot"] != true
            || ledger
                .bot(&request.request_id)
                .is_some_and(|old| old != bot)
        {
            return Err("authenticated recovery bot differs from saved author".into());
        }
        if persist && ledger.bot(&request.request_id).is_none() {
            ledger.append(
                &ctx.here,
                Event::Bot {
                    request_id: request.request_id.clone(),
                    author_id: bot.clone(),
                },
            )?;
        }
        let mut api = Self {
            effects: ctx.effects.as_mut(),
            destination: request.destination.clone(),
            bot,
        };
        let channel = api.get(&format!("/channels/{}", api.destination.forum))?;
        if channel["id"] != api.destination.forum
            || channel["guild_id"] != api.destination.guild
            || channel["type"] != 15
        {
            return Err("recovery destination must be the configured guild task forum".into());
        }
        Ok(api)
    }

    fn get(&mut self, path: &str) -> Result<Value> {
        self.effects
            .http("GET", &format!("{API}{path}"), None, true)
            .map_err(|_| "recovery GET inspection failed; existing receipts retained".into())
    }

    fn channel(&mut self, thread: &str) -> Result<Value> {
        snowflake(thread)?;
        self.get(&format!("/channels/{thread}"))
    }

    fn assert_bot_thread(&mut self, thread: &str) -> Result<()> {
        let channel = self.channel(thread)?;
        if channel["id"] != thread
            || channel["type"] != 11
            || channel["guild_id"] != self.destination.guild
            || channel["parent_id"] != self.destination.forum
            || channel["owner_id"] != self.bot
        {
            return Err("thread is outside the configured bot recovery forum".into());
        }
        Ok(())
    }

    fn known_progress(
        ledger: &Ledger,
        destination: &Destination,
        bot: &str,
        thread: &str,
    ) -> Option<bool> {
        // An explanation receipt must not promote a question to a progress post.
        if ledger.events.iter().any(|event| {
            matches!(event,
            Event::Verified { phase, receipt, .. }
                if phase == "question:0" && receipt.thread_id == thread)
        }) {
            return None;
        }
        ledger.events.iter().find_map(|event| {
            let Event::Verified {
                request_id,
                phase,
                receipt,
            } = event
            else {
                return None;
            };
            let request = ledger.request(request_id)?;
            (request.destination == *destination
                && receipt.author_id == bot
                && receipt.thread_id == thread
                && (phase == "log:0" || phase == "progress"))
                .then_some(phase == "progress")
        })
    }

    fn assert_origin(&mut self, ledger: &Ledger, thread: &str) -> Result<()> {
        if Self::known_progress(ledger, &self.destination, &self.bot, thread).is_some() {
            return self.assert_bot_thread(thread);
        }
        let channel = self.channel(thread)?;
        if channel["id"] != thread
            || channel["type"] != 11
            || channel["guild_id"] != self.destination.guild
            || channel["parent_id"] != self.destination.request_forum
            || channel["owner_id"] != self.destination.owner
        {
            return Err("origin must be the exact owner request or a verified recovery post; legacy questions are excluded".into());
        }
        Ok(())
    }

    fn verify_owner_reply(&mut self, thread: &str, message_id: &str) -> Result<()> {
        let channel = self.channel(thread)?;
        if channel["id"] != thread
            || channel["type"] != 11
            || channel["guild_id"] != self.destination.guild
            || channel["parent_id"] != self.destination.request_forum
            || ![self.destination.owner.as_str(), self.bot.as_str()]
                .contains(&core::s(&channel["owner_id"]))
        {
            return Err("owner reply source is outside the exact configured request thread".into());
        }
        let message = self.get(&format!("/channels/{thread}/messages/{message_id}"))?;
        if !exact_owner_reply(&message, thread, message_id, &self.destination.owner) {
            return Err("exact owner reply could not be verified".into());
        }
        Ok(())
    }

    fn history(&mut self, thread: &str) -> Result<Vec<Value>> {
        snowflake(thread)?;
        let mut all = BTreeMap::new();
        let mut before = String::new();
        for _ in 0..HISTORY_PAGES {
            let suffix = if before.is_empty() {
                String::new()
            } else {
                format!("&before={}", crate::history_audit::query_component(&before))
            };
            let page = self.get(&format!("/channels/{thread}/messages?limit=100{suffix}"))?;
            let rows = page.as_array().ok_or("invalid recovery message history")?;
            if rows.len() > 100 {
                return Err("invalid recovery history page size".into());
            }
            let mut next = u64::MAX;
            for message in rows {
                let id = snowflake(core::s(&message["id"]))?;
                let numeric = id
                    .parse::<u64>()
                    .map_err(|_| "invalid recovery message ID")?;
                if !before.is_empty() && numeric >= before.parse::<u64>().unwrap_or(0) {
                    return Err("recovery history cursor did not advance".into());
                }
                if message["channel_id"] != thread {
                    return Err("message history returned a different thread".into());
                }
                next = next.min(numeric);
                all.insert(numeric, message.clone());
            }
            if rows.len() < 100 {
                return Ok(all.into_values().collect());
            }
            before = next.to_string();
        }
        Err("recovery history limit reached; completeness remains unconfirmed".into())
    }

    fn inventory(&mut self, title: &str) -> Result<Vec<String>> {
        let mut threads = Vec::new();
        let active = self.get(&format!(
            "/guilds/{}/threads/active",
            self.destination.guild
        ))?;
        threads.extend(
            active["threads"]
                .as_array()
                .ok_or("invalid active recovery inventory")?
                .iter()
                .cloned(),
        );
        let mut before = String::new();
        let mut finished = false;
        for _ in 0..HISTORY_PAGES {
            let suffix = if before.is_empty() {
                String::new()
            } else {
                format!("&before={}", crate::history_audit::query_component(&before))
            };
            let page = self.get(&format!(
                "/channels/{}/threads/archived/public?limit=100{suffix}",
                self.destination.forum
            ))?;
            let rows = page["threads"]
                .as_array()
                .ok_or("invalid archived recovery inventory")?;
            threads.extend(rows.iter().cloned());
            let more = page["has_more"]
                .as_bool()
                .ok_or("archive completeness missing")?;
            if !more {
                finished = true;
                break;
            }
            let cursor = rows
                .last()
                .and_then(|r| r["thread_metadata"]["archive_timestamp"].as_str())
                .ok_or("archive cursor missing")?;
            let time = chrono::DateTime::parse_from_rfc3339(cursor)
                .map_err(|_| "invalid archive cursor")?;
            if !before.is_empty()
                && time
                    >= chrono::DateTime::parse_from_rfc3339(&before)
                        .map_err(|_| "invalid archive cursor")?
            {
                return Err("archive cursor did not advance".into());
            }
            before = cursor.into();
        }
        if !finished {
            return Err("archive limit reached; recovery POST remains unconfirmed".into());
        }
        let mut ids = std::collections::BTreeSet::new();
        for thread in threads {
            if thread["name"] == title
                && thread["parent_id"] == self.destination.forum
                && thread["owner_id"] == self.bot
            {
                ids.insert(snowflake(core::s(&thread["id"]))?);
            }
        }
        Ok(ids.into_iter().collect())
    }

    fn candidates(&mut self, effect: &Effect) -> Result<Vec<(String, String)>> {
        let mut out = Vec::new();
        match &effect.target {
            Target::Forum { title } => {
                for thread in self.inventory(title)? {
                    self.assert_bot_thread(&thread)?;
                    let message = self.get(&format!("/channels/{thread}/messages/{thread}"))?;
                    if message["author"]["id"] == self.bot && message["content"] == effect.content {
                        out.push((thread.clone(), snowflake(core::s(&message["id"]))?));
                    }
                }
            }
            Target::Thread { thread } => {
                for message in self.history(thread)? {
                    if message["author"]["id"] == self.bot && message["content"] == effect.content {
                        out.push((thread.clone(), snowflake(core::s(&message["id"]))?));
                    }
                }
            }
        }
        Ok(out)
    }

    fn verify(
        &mut self,
        ledger: &Ledger,
        request: &Request,
        effect: &Effect,
        thread: &str,
        message_id: &str,
    ) -> Result<Receipt> {
        match &request.operation {
            Kind::Ask { origin_thread, .. } if origin_thread == thread => {
                self.assert_origin(ledger, thread)?
            }
            _ => self.assert_bot_thread(thread)?,
        }
        if let Target::Thread { thread: expected } = &effect.target {
            if expected != thread {
                return Err("recovery receipt belongs to a different thread".into());
            }
        } else if thread != message_id
            || self.channel(thread)?["name"]
                != match &effect.target {
                    Target::Forum { title } => title.as_str(),
                    _ => "",
                }
        {
            return Err("recovery forum starter/title differs".into());
        }
        let message = self.get(&format!(
            "/channels/{thread}/messages/{}",
            snowflake(message_id)?
        ))?;
        if message["id"] != message_id
            || message["channel_id"] != thread
            || message["author"]["id"] != self.bot
            || message.get("webhook_id").is_some_and(|v| !v.is_null())
            || message["content"] != effect.content
        {
            return Err("recovery GET body/author/thread verification failed".into());
        }
        let flags = message["flags"]
            .as_u64()
            .ok_or("recovery GET flags missing")?;
        if flags & (SILENT | EMBEDS) != ((if effect.notify { 0 } else { SILENT }) | EMBEDS) {
            return Err("recovery GET notification flags differ".into());
        }
        let mentions = message["mentions"]
            .as_array()
            .ok_or("recovery GET mentions missing")?;
        let wanted = usize::from(effect.notify);
        if mentions.len() != wanted
            || mentions.iter().any(|m| m["id"] != self.destination.owner)
            || message["mention_everyone"] != false
            || !message["mention_roles"]
                .as_array()
                .is_some_and(Vec::is_empty)
        {
            return Err("recovery GET owner mention verification failed".into());
        }
        let actual_files = message["attachments"]
            .as_array()
            .ok_or("recovery GET attachments missing")?;
        if actual_files.len() != effect.images.len()
            || actual_files
                .iter()
                .zip(&effect.images)
                .any(|(actual, expected)| {
                    actual["filename"] != expected.filename
                        || actual["size"].as_u64() != Some(expected.size as u64)
                })
        {
            return Err("recovery GET attachment verification failed".into());
        }
        let timestamp = message["timestamp"]
            .as_str()
            .ok_or("recovery GET timestamp missing")?;
        chrono::DateTime::parse_from_rfc3339(timestamp)
            .map_err(|_| "invalid recovery timestamp")?;
        Ok(Receipt {
            thread_id: thread.into(),
            message_id: message_id.into(),
            author_id: self.bot.clone(),
            timestamp: timestamp.into(),
            flags,
            owner_mentions: wanted,
        })
    }

    fn submit(
        &mut self,
        ledger: &Ledger,
        request: &Request,
        phase: &str,
        effect: &Effect,
        images: &[Image],
    ) -> Result<Value> {
        if ledger.effect(&request.request_id, phase) != Some(effect)
            || ledger.receipt(&request.request_id, phase).is_some()
        {
            return Err("a durable exact recovery reservation is required before POST".into());
        }
        let route = match &effect.target {
            Target::Forum { .. } => format!("/channels/{}/threads", self.destination.forum),
            Target::Thread { thread } => {
                match &request.operation {
                    Kind::Ask { origin_thread, .. } if origin_thread == thread => {
                        self.assert_origin(ledger, thread)?
                    }
                    _ => self.assert_bot_thread(thread)?,
                }
                format!("/channels/{}/messages", snowflake(thread)?)
            }
        };
        let payload = effect.payload(&self.destination.owner);
        let mut body = match &effect.target {
            Target::Forum { title } => {
                json!({"name":title,"message":payload,"auto_archive_duration":1440})
            }
            Target::Thread { .. } => payload,
        };
        if matches!(effect.target, Target::Thread { .. }) {
            body["nonce"] =
                json!(&digest(&json!({"fingerprint":request.fingerprint,"phase":phase}))?[..24]);
            body["enforce_nonce"] = json!(true);
        }
        if effect.images.is_empty() {
            return self
                .effects
                .http("POST", &format!("{API}{route}"), Some(&body), true)
                .map_err(|_| PENDING.into());
        }
        if effect.images
            != images
                .iter()
                .map(|image| image.receipt.clone())
                .collect::<Vec<_>>()
        {
            return Err("validated recovery image bytes differ from saved receipt".into());
        }
        let metadata: Vec<_> = images
            .iter()
            .enumerate()
            .map(|(id, image)| json!({"id":id,"filename":image.receipt.filename}))
            .collect();
        if body.get("message").is_some() {
            body["message"]["attachments"] = json!(metadata);
        } else {
            body["attachments"] = json!(metadata);
        }
        let boundary = format!("recovery-{}", uuid::Uuid::new_v4().simple());
        let mut bytes = format!("--{boundary}\r\nContent-Disposition: form-data; name=\"payload_json\"\r\nContent-Type: application/json\r\n\r\n{body}\r\n").into_bytes();
        for (id, image) in images.iter().enumerate() {
            bytes.extend_from_slice(format!("--{boundary}\r\nContent-Disposition: form-data; name=\"files[{id}]\"; filename=\"{}\"\r\nContent-Type: {}\r\n\r\n",
                image.receipt.filename, image.receipt.mime).as_bytes());
            bytes.extend_from_slice(&image.bytes);
            bytes.extend_from_slice(b"\r\n");
        }
        bytes.extend_from_slice(format!("--{boundary}--\r\n").as_bytes());
        self.effects
            .multipart(
                &format!("{API}{route}"),
                &format!("multipart/form-data; boundary={boundary}"),
                &bytes,
            )
            .map_err(|_| PENDING.into())
    }
}

fn parts(body: &str) -> Vec<String> {
    // Reserve room for the phase marker, origin URL and one owner mention.
    // Use UTF-16 units so astral characters cannot exceed Discord's limit.
    let mut result = Vec::new();
    let mut part = String::new();
    let mut units = 0;
    for character in body.chars() {
        if units + character.len_utf16() > MAX_CONTENT - 350 {
            result.push(std::mem::take(&mut part));
            units = 0;
        }
        units += character.len_utf16();
        part.push(character);
    }
    if !part.is_empty() {
        result.push(part);
    }
    result
}

fn effect(
    request: &Request,
    phase: &str,
    target: Target,
    body: &str,
    notify: bool,
    with_images: bool,
) -> Result<Effect> {
    let content = format!(
        "{body}{}\n\n-# recovery:{}:{phase}",
        if notify {
            format!("\n<@{}>", request.destination.owner)
        } else {
            String::new()
        },
        &request.fingerprint[..24]
    );
    if content.encode_utf16().count() > MAX_CONTENT {
        return Err("recovery message content exceeds Discord's limit".into());
    }
    Ok(Effect {
        target,
        content,
        notify,
        images: if with_images {
            request.images.clone()
        } else {
            vec![]
        },
    })
}

fn ensure(
    api: &mut RecoveryApi<'_>,
    ledger: &mut Ledger,
    home: &Path,
    request: &Request,
    phase: &str,
    effect: Effect,
    images: &[Image],
) -> Result<Receipt> {
    let id = &request.request_id;
    if let Some(existing) = ledger.effect(id, phase) {
        if *existing != effect {
            return Err("saved recovery phase differs; original effect retained".into());
        }
    }
    if let Some(receipt) = ledger.receipt(id, phase) {
        // Even a saved success must still agree with a fresh authenticated GET.
        return api.verify(
            ledger,
            request,
            &effect,
            &receipt.thread_id,
            &receipt.message_id,
        );
    }
    if let Some((thread, message)) = ledger.posted(id, phase) {
        let receipt = api.verify(ledger, request, &effect, &thread, &message)?;
        ledger.append(
            home,
            Event::Verified {
                request_id: id.clone(),
                phase: phase.into(),
                receipt: receipt.clone(),
            },
        )?;
        return Ok(receipt);
    }
    let recovering = ledger.effect(id, phase).is_some();
    // Every submit requires a flushed Pending event under this same byte lock.
    // Without it this phase has never submitted: bind/origin/thread GETs already
    // verified its target, and scanning all archived posts cannot add evidence.
    // An uncertain prior submit still requires the complete exact GET search;
    // it must never become permission to POST again.
    let selected = if recovering {
        let candidates = api.candidates(&effect)?;
        match candidates.as_slice() {
            [one] => Some(one.clone()),
            [] => return Err(PENDING.into()),
            _ => return Err("RecoveryMessagePending: duplicate matching bot messages require inspection; no POST is retried".into()),
        }
    } else {
        None
    };
    if !recovering {
        ledger.append(
            home,
            Event::Pending {
                request_id: id.clone(),
                phase: phase.into(),
                effect: effect.clone(),
            },
        )?;
    }
    let (thread, message) = if let Some(ids) = selected {
        ids
    } else {
        let response = api.submit(ledger, request, phase, &effect, images);
        let ids = response.ok().and_then(|response| {
            let message = match &effect.target {
                Target::Forum { .. } => &response["message"]["id"],
                Target::Thread { .. } => &response["id"],
            };
            let thread = match &effect.target {
                Target::Forum { .. } => snowflake(core::s(&response["id"])).ok()?,
                Target::Thread { thread } => thread.clone(),
            };
            Some((thread, snowflake(core::s(message)).ok()?))
        });
        if let Some((thread, message)) = ids {
            // Retain the POST IDs before the separate GET. GET failure must not
            // make a later invocation POST again.
            ledger.append(
                home,
                Event::Posted {
                    request_id: id.clone(),
                    phase: phase.into(),
                    thread_id: thread.clone(),
                    message_id: message.clone(),
                },
            )?;
            (thread, message)
        } else {
            // Discord may have accepted a POST whose response was lost. Only
            // exact content plus authenticated bot author can resolve it.
            let found = api.candidates(&effect)?;
            match found.as_slice() {
                [one] => one.clone(),
                _ => return Err(PENDING.into()),
            }
        }
    };
    let receipt = api.verify(ledger, request, &effect, &thread, &message)?;
    ledger.append(
        home,
        Event::Verified {
            request_id: id.clone(),
            phase: phase.into(),
            receipt: receipt.clone(),
        },
    )?;
    Ok(receipt)
}

fn prepare_log(ctx: &mut Context, args: &LogArgs) -> Result<(Request, Vec<Image>)> {
    request_key(&args.request_id)?;
    let secrets = SecretCheck::new();
    let body = text_input(&args.body_file, &secrets)?;
    prepare_log_body(ctx, args, body)
}

fn prepare_log_body(
    ctx: &mut Context,
    args: &LogArgs,
    body: String,
) -> Result<(Request, Vec<Image>)> {
    request_key(&args.request_id)?;
    let secrets = SecretCheck::new();
    secrets.text(&body)?;
    if body.trim().is_empty() {
        return Err("recovery body must be nonempty".into());
    }
    no_mentions(&body)?;
    if args.notify {
        log_requires_owner_action(&body)?;
    }
    let images = images(ctx, &args.request_id, &args.attach, &secrets, None)?;
    let mut request = Request {
        request_id: args.request_id.clone(),
        title: title_input(&args.title, &secrets)?,
        body,
        destination: Destination::from_context(ctx)?,
        notify: args.notify,
        images: images.iter().map(|image| image.receipt.clone()).collect(),
        operation: Kind::Log {
            thread: args.thread.as_deref().map(snowflake).transpose()?,
        },
        fingerprint: String::new(),
    };
    request.fingerprint = request.fingerprint()?;
    Ok((request, images))
}

fn prepare_ask(ctx: &Context, args: &AskArgs) -> Result<Request> {
    request_key(&args.request_id)?;
    let secrets = SecretCheck::new();
    let body = text_input(&args.body_file, &secrets)?;
    no_mentions(&body)?;
    let decision_text = text_input(&args.decision_file, &secrets)?;
    let decision: Value =
        serde_json::from_str(&decision_text).map_err(|_| "invalid recovery decision JSON")?;
    if args.todo_id == 0 || core::n(&decision["todo_id"]) != args.todo_id {
        return Err("recovery todo-id must equal the original decision todo_id".into());
    }
    let origin_in_file = decision["origin_thread_id"]
        .as_str()
        .or_else(|| decision["scope"]["origin_thread_id"].as_str());
    if args
        .origin_thread
        .as_deref()
        .zip(origin_in_file)
        .is_some_and(|(a, b)| a != b)
    {
        return Err("recovery origin differs from the original decision".into());
    }
    let origin = snowflake(
        args.origin_thread
            .as_deref()
            .or(origin_in_file)
            .ok_or("exact new origin-thread required; never inferred from lost state")?,
    )?;
    let identity = crate::decision_identity::normalize(ctx, &decision)?;
    secrets.text(&identity.to_string())?;
    let fallback = body
        .lines()
        .find(|line| !line.trim().is_empty())
        .unwrap_or("");
    let mut request = Request {
        request_id: args.request_id.clone(),
        title: title_input(args.title.as_deref().unwrap_or(fallback), &secrets)?,
        body,
        destination: Destination::from_context(ctx)?,
        notify: true,
        images: vec![],
        operation: Kind::Ask {
            todo_id: args.todo_id,
            origin_thread: origin,
            decision_original: decision,
            decision_identity: identity,
        },
        fingerprint: String::new(),
    };
    request.fingerprint = request.fingerprint()?;
    Ok(request)
}

fn report(ledger: &Ledger, request: &Request, reused: bool) -> Value {
    let pending = ledger
        .events
        .iter()
        .filter(|event| {
            matches!(event,
        Event::Pending { request_id, phase, .. } if request_id == &request.request_id
            && ledger.receipt(request_id, phase).is_none())
        })
        .count();
    let start_phase = if matches!(request.operation, Kind::Ask { .. }) {
        "question:0"
    } else {
        "log:0"
    };
    let start = ledger.receipt(&request.request_id, start_phase);
    let progress = ledger.receipt(&request.request_id, "progress");
    let original_fingerprint = ledger
        .request(&request.request_id)
        .map(|original| &original.fingerprint);
    json!({"domain":DOMAIN,"request_id":request.request_id,"fingerprint":request.fingerprint,
        "delivery":if ledger.complete(&request.request_id) {"verified"} else {"pending"},
        "new_pending_effects":pending,"legacy_pending":"unknown","legacy_state":"unchanged",
        "reused":reused,"original_fingerprint":original_fingerprint,
        "requested_notify":request.notify,
        "effective_notify":ledger.effect(&request.request_id, start_phase).map(|effect| effect.notify),
        "thread_id":start.map(|r| &r.thread_id),"message_id":start.map(|r| &r.message_id),
        "url":start.map(|r| request.destination.url(&r.thread_id)),
        "progress_thread_id":progress.map(|r| &r.thread_id),
        "progress_message_id":progress.map(|r| &r.message_id),
        "progress_url":progress.map(|r| request.destination.url(&r.thread_id)),
        "original_question":if matches!(request.operation, Kind::Ask { .. }) {Some(&request.body)} else {None},
        "image_sha256":request.images.iter().map(|image| &image.sha256).collect::<Vec<_>>(),
        "decision":match &request.operation {Kind::Ask {decision_identity,..} => Some(decision_identity), _ => None},
        "reply_delivery":"not_dispatched","approval":"not_inferred",
        "saved_files":[LEDGER,LOCK],"http_scope":"configured task forum, verified new recovery threads, exact owner request"})
}

/// POST receipts never change the original question state. Long logs targeted to
/// a progress thread are split there and are always silent.
pub fn log(ctx: &mut Context, args: &LogArgs) -> Result<Value> {
    let (incoming, images) = prepare_log(ctx, args)?;
    execute(ctx, incoming, &images)
}

/// Resume an exact explanation through the same log delivery journal.
pub fn log_clarification(ctx: &mut Context, args: &LogArgs, manifest: &Path) -> Result<Value> {
    if args.notify || !args.attach.is_empty() || args.thread.is_none() {
        return Err("clarification requires the exact silent text-only destination".into());
    }
    let binding = clarification::Binding::read(manifest)?;
    let (request, _) = prepare_clarification(ctx, args)?;
    binding.check_input(&request)?;
    execute_inner(ctx, Some(request), None, &[], Some(&binding))
}

fn prepare_clarification(ctx: &mut Context, args: &LogArgs) -> Result<(Request, String)> {
    let bytes = read_bounded(
        fs::File::open(&args.body_file).map_err(|_| "cannot read clarification input")?,
        TEXT_BYTES,
    )?;
    let raw_sha = format!("{:x}", Sha256::digest(&bytes));
    let body = String::from_utf8(bytes)
        .map_err(|_| "clarification input must be UTF-8")?
        .trim_start_matches('\u{feff}')
        .replace("\r\n", "\n")
        .replace('\r', "\n");
    let (request, _) = prepare_log_body(ctx, args, body)?;
    Ok((request, raw_sha))
}

/// The same association checks, but no journal update and no send operation.
pub fn check_clarification(ctx: &mut Context, args: &LogArgs, manifest: &Path) -> Result<Value> {
    if args.notify || !args.attach.is_empty() || args.thread.is_none() {
        return Err("clarification check requires the exact silent text-only destination".into());
    }
    let (binding, binding_sha) = clarification::Binding::read_with_hash(manifest)?;
    let (request, source_sha) = prepare_clarification(ctx, args)?;
    binding.check_input(&request)?;
    let home = ctx.here.clone();
    ensure_storage(&home)?;
    // A valid original question already owns this lock file. Do not create a
    // file even for an invalid check directed to an empty home.
    if !fs::symlink_metadata(home.join(LOCK))
        .is_ok_and(|m| m.is_file() && !m.file_type().is_symlink())
    {
        return Err("clarification requires an existing original question lock".into());
    }
    let _lock = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
    let mut ledger = Ledger::load(&home)?;
    let mut api = RecoveryApi::inspect(ctx, &mut ledger, &request)?;
    clarification::authorize(&mut api, &ledger, &home, &request, &binding, false, false)?;
    Ok(
        json!({"domain":DOMAIN,"request_id":request.request_id,"delivery":"not_sent",
        "check_only":true,"original_intent_retained":true,
        "body_sha256":format!("{:x}",Sha256::digest(request.body.as_bytes())),
        "source_body_sha256":source_sha,"binding_sha256":binding_sha}),
    )
}

/// The original decision scope and question text are retained even when another
/// title/request ID is a semantic duplicate. Question text stays in its thread.
pub fn ask(ctx: &mut Context, args: &AskArgs) -> Result<Value> {
    let mut incoming = prepare_ask(ctx, args)?;
    let images = if let Some(saved) = pending_question_image_receipts(ctx, args)? {
        incoming.images = saved;
        vec![]
    } else if let Some(evidence) = args.program_evidence.as_deref() {
        let images = program_question::images(ctx, &incoming, args, evidence)?;
        incoming.images = images.iter().map(|image| image.receipt.clone()).collect();
        images
    } else {
        let scope = question_image_scope(ctx, &incoming);
        let images = images(
            ctx,
            &args.request_id,
            &args.attach,
            &SecretCheck::new(),
            scope.as_ref(),
        )?;
        if !args.expected_image_sha256.is_empty()
            && images
                .iter()
                .map(|image| &image.receipt.sha256)
                .collect::<Vec<_>>()
                != args.expected_image_sha256.iter().collect::<Vec<_>>()
        {
            return Err("question images changed before upload; no message is sent".into());
        }
        incoming.images = images.iter().map(|image| image.receipt.clone()).collect();
        images
    };
    incoming.fingerprint = incoming.fingerprint()?;
    execute(ctx, incoming, &images)
}

// A durable question:0 reservation can only be inspected with GET by ensure;
// it can never grant another upload. Recovery therefore needs no source bytes.
fn pending_question_image_receipts(
    ctx: &Context,
    args: &AskArgs,
) -> Result<Option<Vec<ImageReceipt>>> {
    if args.expected_image_sha256.is_empty() {
        return Ok(None);
    }
    if args.expected_image_sha256.iter().any(|sha| {
        sha.len() != 64
            || !sha
                .bytes()
                .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
    }) {
        return Err("expected question image SHA is invalid".into());
    }
    let home = ctx.here.clone();
    let _lock = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
    let ledger = Ledger::load(&home)?;
    let Some(request) = ledger.request(&args.request_id) else {
        return Ok(None);
    };
    if ledger.effect(&request.request_id, "question:0").is_none() {
        return Ok(None);
    }
    if request
        .images
        .iter()
        .map(|image| &image.sha256)
        .collect::<Vec<_>>()
        != args.expected_image_sha256.iter().collect::<Vec<_>>()
    {
        return Err("saved question image SHA differs; original request retained".into());
    }
    Ok(Some(request.images.clone()))
}

pub fn resume_ask(ctx: &mut Context, args: &ResumeAskArgs) -> Result<Value> {
    request_key(&args.request_id)?;
    if args.todo_id == 0 {
        return Err("original ToDo ID is required".into());
    }
    let (scope, recovering) = {
        let home = ctx.here.clone();
        let _lock = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
        let ledger = Ledger::load(&home)?;
        let request = ledger
            .request(&args.request_id)
            .ok_or("saved question unavailable")?;
        (
            question_image_scope(ctx, request),
            ledger.effect(&request.request_id, "question:0").is_some(),
        )
    };
    let images = if recovering {
        vec![]
    } else {
        images(
            ctx,
            &args.request_id,
            &args.attach,
            &SecretCheck::new(),
            scope.as_ref(),
        )?
    };
    execute_saved(ctx, &args.request_id, args.todo_id, &images)
}

fn question_image_scope(ctx: &Context, request: &Request) -> Option<crate::question_images::Scope> {
    if let Kind::Ask {
        todo_id,
        decision_original,
        ..
    } = &request.operation
    {
        Some(crate::question_images::Scope::for_ask(
            ctx,
            *todo_id,
            &request.body,
            decision_original,
        ))
    } else {
        None
    }
}

/// Repair only the origin of an exact, unsent recovery ask. The original intent
/// remains immutable; the corrected source must be a prior verified progress
/// post in this ledger, and the owner reply is rechecked by exact Discord GET.
pub fn repair_origin(ctx: &mut Context, args: &RepairOriginArgs) -> Result<Value> {
    request_key(&args.request_id)?;
    let expected = snowflake(&args.expected_origin_thread)?;
    let owner_reply = snowflake(&args.owner_reply_message)?;
    let progress_message = snowflake(&args.progress_message)?;
    if args.todo_id == 0 {
        return Err("original ToDo ID is required".into());
    }
    if owner_reply == expected {
        return Err("owner reply must be a distinct exact message ID".into());
    }
    let home = ctx.here.clone();
    ensure_storage(&home)?;
    let _lock = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
    let mut ledger = Ledger::load(&home)?;
    if ledger.events.iter().any(|event| {
        matches!(event, Event::OriginCorrected { request } if request.request_id == args.request_id)
    }) {
        return Err("request already has an origin correction; reuse its corrected input".into());
    }
    let request = ledger
        .events
        .iter()
        .find_map(|event| match event {
            Event::Intent { request } if request.request_id == args.request_id => {
                Some(request.as_ref().clone())
            }
            _ => None,
        })
        .ok_or("exact original request intent is unavailable")?;
    if let Some(existing) = ledger.events.iter().find(|event| {
        matches!(event, Event::OriginRepair { request_id, .. } if request_id == &args.request_id)
    }) {
        let Event::OriginRepair {
            todo_id,
            expected_origin_thread,
            owner_reply_message,
            progress_thread,
            progress_message: saved_progress_message,
            ..
        } = existing
        else {
            unreachable!()
        };
        let saved_progress_thread = ledger.events.iter().find_map(|event| match event {
            Event::Verified { phase, receipt, .. }
                if phase == "progress" && receipt.message_id == *saved_progress_message =>
            {
                Some(receipt.thread_id.as_str())
            }
            _ => None,
        });
        if *todo_id != args.todo_id
            || expected_origin_thread != &expected
            || owner_reply_message != &owner_reply
            || saved_progress_message != &progress_message
            || saved_progress_thread != Some(progress_thread.as_str())
        {
            return Err("a different origin repair is already recorded for this request".into());
        }
        let effective = ledger.effective_request(&request)?;
        return Ok(json!({"request_id":args.request_id,"todo_id":args.todo_id,
            "repair":"already_recorded","origin_thread":progress_thread,
            "owner_reply_message":owner_reply,"progress_message":progress_message,
            "original_fingerprint":request.fingerprint,
            "repaired_fingerprint":effective.fingerprint}));
    }
    let Kind::Ask {
        todo_id,
        origin_thread,
        ..
    } = &request.operation
    else {
        return Err("origin repair requires an exact question request".into());
    };
    if *todo_id != args.todo_id || origin_thread != &expected {
        return Err("request ID, original ToDo or expected origin differs".into());
    }
    if ledger.bot(&args.request_id).is_none()
        || ledger.events.iter().any(|event| match event {
            Event::Pending { request_id, .. }
            | Event::Posted { request_id, .. }
            | Event::Verified { request_id, .. }
            | Event::DeliveryVerified { request_id } => request_id == &args.request_id,
            _ => false,
        })
    {
        return Err("only an exact unsent intent with its bot receipt can be repaired".into());
    }
    let mut progress_source = None;
    for event in &ledger.events {
        if let Event::Verified {
            request_id,
            phase,
            receipt,
        } = event
        {
            if phase == "progress" && receipt.message_id == progress_message {
                if progress_source.is_some() {
                    return Err("progress message receipt is ambiguous".into());
                }
                progress_source = Some((request_id.clone(), receipt.clone()));
            }
        }
    }
    let (progress_request_id, progress_receipt) =
        progress_source.ok_or("exact verified progress receipt is unavailable")?;
    let source_request = ledger
        .request(&progress_request_id)
        .ok_or("verified progress request is unavailable")?;
    let source_is_related = matches!(
        (&source_request.operation, &request.operation),
        (
            Kind::Ask {
                todo_id: source_todo,
                origin_thread: source_origin,
                ..
            },
            Kind::Ask {
                todo_id: target_todo,
                origin_thread: target_origin,
                ..
            }
        ) if source_todo == target_todo && source_origin == target_origin
    );
    if !source_is_related
        || source_request.destination != request.destination
        || ledger.bot(&progress_request_id) != ledger.bot(&args.request_id)
        || progress_receipt.author_id != ledger.bot(&progress_request_id).unwrap_or("")
        || RecoveryApi::known_progress(
            &ledger,
            &request.destination,
            ledger.bot(&progress_request_id).unwrap_or(""),
            &progress_receipt.thread_id,
        ) != Some(true)
    {
        return Err("repair target is not the same bot's verified progress post".into());
    }
    let mut api = RecoveryApi::bind(ctx, &mut ledger, &request)?;
    api.verify_owner_reply(&expected, &owner_reply)?;
    api.assert_bot_thread(&progress_receipt.thread_id)?;
    ledger.append(
        &home,
        Event::OriginRepair {
            request_id: args.request_id.clone(),
            todo_id: args.todo_id,
            expected_origin_thread: expected,
            owner_reply_message: owner_reply,
            progress_request_id,
            progress_thread: progress_receipt.thread_id.clone(),
            progress_message,
            original_fingerprint: request.fingerprint.clone(),
        },
    )?;
    let effective = ledger.effective_request(&request)?;
    Ok(json!({"request_id":args.request_id,"todo_id":args.todo_id,
        "repair":"recorded","origin_thread":progress_receipt.thread_id,
        "owner_reply_message":args.owner_reply_message,"progress_message":args.progress_message,
        "original_fingerprint":request.fingerprint,"repaired_fingerprint":effective.fingerprint}))
}

fn execute(ctx: &mut Context, incoming: Request, images: &[Image]) -> Result<Value> {
    execute_inner(ctx, Some(incoming), None, images, None)
}

// --notify requests attention, but a report alone never requires owner action.
// Real decisions normally use Ask. A log may point to its concrete existing
// question; a heading alone is insufficient. No-action wording always wins.
pub(crate) fn report_declares_no_action(body: &str) -> bool {
    let first = body
        .lines()
        .map(str::trim)
        .find(|line| !line.is_empty())
        .unwrap_or("");
    [
        "対応不要",
        "対応は不要",
        "対応は必要ありません",
        "報告のみ",
        "本人対応不要",
    ]
    .iter()
    .any(|marker| first.starts_with(marker))
}

pub(crate) fn log_requires_owner_action(body: &str) -> Result<bool> {
    let mut lines = body.lines().map(str::trim).filter(|line| !line.is_empty());
    let first = lines.next().unwrap_or("");
    if report_declares_no_action(body) {
        return Ok(false);
    }
    let Some(question) = first
        .strip_prefix("回答が必要：")
        .or_else(|| first.strip_prefix("回答が必要:"))
    else {
        if first.starts_with("回答が必要") {
            return Err(
                "owner action heading requires a concrete question and distinct a/b choices".into(),
            );
        }
        // Reports may quote questions or contain a diagnostic question mark.
        // Only an explicit owner-action heading grants report notification;
        // real questions without that report heading use Ask/decision instead.
        return Ok(false);
    };
    if question.trim().is_empty() {
        return Err("owner action heading requires a concrete question".into());
    }
    let choices: Vec<_> = lines.collect();
    let choice = |prefix| {
        choices.iter().find_map(|line| {
            line.strip_prefix(prefix)
                .or_else(|| line.strip_prefix(if prefix == "a:" { "a：" } else { "b：" }))
                .map(str::trim)
                .filter(|choice| !choice.is_empty())
        })
    };
    if choice("a:").zip(choice("b:")).is_some_and(|(a, b)| a != b) {
        Ok(true)
    } else {
        Err("owner action heading requires distinct nonempty a/b choices".into())
    }
}

fn execute_saved(
    ctx: &mut Context,
    request_id: &str,
    todo_id: u64,
    images: &[Image],
) -> Result<Value> {
    execute_inner(ctx, None, Some((request_id, todo_id)), images, None)
}

fn execute_inner(
    ctx: &mut Context,
    incoming: Option<Request>,
    saved: Option<(&str, u64)>,
    images: &[Image],
    clarification: Option<&clarification::Binding>,
) -> Result<Value> {
    let home = ctx.here.clone();
    ensure_storage(&home)?;
    if clarification.is_some()
        && !fs::symlink_metadata(home.join(LOCK))
            .is_ok_and(|m| m.is_file() && !m.file_type().is_symlink())
    {
        return Err("clarification requires an existing original question lock".into());
    }
    let _lock = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
    let mut ledger = Ledger::load(&home)?;
    if let (Some(binding), Some(request)) = (clarification, incoming.as_ref()) {
        let mut api = RecoveryApi::inspect(ctx, &mut ledger, request)?;
        // Authenticate against the independent question before creating the
        // new intent/bot record. Pin the entire validated intent atomically in
        // the purpose journal first, so a crash cannot permit rebinding.
        clarification::authorize(&mut api, &ledger, &home, request, binding, true, false)?;
    }
    let incoming = match (incoming, saved) {
        (Some(request), None) => request,
        (None, Some((request_id, todo_id))) => {
            let request = ledger
                .events
                .iter()
                .find_map(|event| match event {
                    Event::Intent { request } if request.request_id == request_id => {
                        Some(request.as_ref().clone())
                    }
                    _ => None,
                })
                .ok_or("exact saved recovery ask is unavailable")?;
            if !matches!(&request.operation, Kind::Ask { todo_id: saved_todo, .. }
                if *saved_todo == todo_id)
                || ledger.repaired_origin(request_id).is_none()
            {
                return Err("saved ask, original ToDo or origin repair differs".into());
            }
            if ledger.effect(request_id, "question:0").is_none()
                && images
                    .iter()
                    .map(|image| &image.receipt)
                    .collect::<Vec<_>>()
                    != request.images.iter().collect::<Vec<_>>()
            {
                return Err(
                    "saved question images differ or are unavailable; original request retained"
                        .into(),
                );
            }
            request
        }
        _ => return Err("exact recovery ask input is required".into()),
    };
    if let Some(report) = different_question_images(ctx, &mut ledger, &incoming)? {
        return Ok(report);
    }
    correct_unposted_origin(ctx, &mut ledger, &incoming)?;
    let reused = ledger.request(&incoming.request_id).is_some()
        || ledger.events.iter().any(|event| {
            matches!(event, Event::Intent {request} if
            request.fingerprint == incoming.fingerprint
            || matches!((&request.operation, &incoming.operation),
                (Kind::Ask {decision_identity:a,..},Kind::Ask {decision_identity:b,..})
                    if request.destination == incoming.destination && a == b))
        });
    let reserved = reserve(&mut ledger, &home, &incoming)?;
    let request = ledger.effective_request(&reserved)?;
    let mut api = RecoveryApi::bind(ctx, &mut ledger, &request)?;
    let text = parts(&request.body);
    match &request.operation {
        Kind::Log { thread } => {
            // The immutable input can retain an old or mistaken --notify.
            // Only a concrete owner action may create a new notifying effect.
            // A saved effect may already have reached Discord: recover it by
            // GET without changing/reposting that historical reservation.
            let notify = if let Some(effect) = ledger.effect(&request.request_id, "log:0") {
                effect.notify
            } else {
                request.notify && log_requires_owner_action(&request.body)?
            };
            if let Some(thread) = thread {
                if clarification.is_none() {
                    let is_progress =
                        RecoveryApi::known_progress(&ledger, &api.destination, &api.bot, thread)
                            .ok_or(
                                "explicit log thread has no verified independent recovery receipt",
                            )?;
                    if is_progress && notify {
                        return Err("recovery progress posts must stay silent".into());
                    }
                }
                api.assert_bot_thread(thread)?;
            }
            let mut destination = thread.clone();
            for (index, body) in text.iter().enumerate() {
                let phase = format!("log:{index}");
                if ledger.effect(&request.request_id, &phase).is_none() {
                    if let Some(binding) = clarification {
                        clarification::authorize(
                            &mut api, &ledger, &home, &request, binding, true, true,
                        )?;
                    }
                }
                let target = match &destination {
                    Some(thread) => Target::Thread {
                        thread: thread.clone(),
                    },
                    None => Target::Forum {
                        title: request.title.clone(),
                    },
                };
                let delivery = effect(
                    &request,
                    &phase,
                    target,
                    body,
                    index == 0 && notify,
                    index == 0,
                )?;
                let receipt = ensure(
                    &mut api,
                    &mut ledger,
                    &home,
                    &request,
                    &phase,
                    delivery,
                    if index == 0 { images } else { &[] },
                )?;
                destination = Some(receipt.thread_id);
            }
        }
        Kind::Ask { origin_thread, .. } => {
            api.assert_origin(&ledger, origin_thread)?;
            let first = format!(
                "{}\n\n元の投稿: <{}>",
                text[0],
                request.destination.url(origin_thread)
            );
            let question = ensure(
                &mut api,
                &mut ledger,
                &home,
                &request,
                "question:0",
                effect(
                    &request,
                    "question:0",
                    Target::Forum {
                        title: request.title.clone(),
                    },
                    &first,
                    true,
                    true,
                )?,
                images,
            )?;
            let bridge = format!(
                "元の投稿: <{}>\n質問: <{}>\n以後の進捗と結果はこの投稿へ記録します。",
                request.destination.url(origin_thread),
                request.destination.url(&question.thread_id)
            );
            for (index, body) in text.iter().enumerate().skip(1) {
                let phase = format!("question-continuation:{index}");
                // Never rewrite an already reserved legacy continuation. New
                // questions keep all decision content inside the question.
                let continuation = ledger
                    .effect(&request.request_id, &phase)
                    .cloned()
                    .map(Ok)
                    .unwrap_or_else(|| {
                        effect(
                            &request,
                            &phase,
                            Target::Thread {
                                thread: question.thread_id.clone(),
                            },
                            body,
                            false,
                            false,
                        )
                    })?;
                ensure(
                    &mut api,
                    &mut ledger,
                    &home,
                    &request,
                    &phase,
                    continuation,
                    &[],
                )?;
            }
            let progress_title: String = format!("進捗 {}", request.title)
                .chars()
                .take(100)
                .collect();
            let progress = ensure(
                &mut api,
                &mut ledger,
                &home,
                &request,
                "progress",
                effect(
                    &request,
                    "progress",
                    Target::Forum {
                        title: progress_title,
                    },
                    &bridge,
                    false,
                    false,
                )?,
                &[],
            )?;
            let question_links = format!(
                "元の投稿: <{}>\n進捗: <{}>",
                request.destination.url(origin_thread),
                request.destination.url(&progress.thread_id)
            );
            let links = ledger
                .effect(&request.request_id, "question-links")
                .cloned()
                .map(Ok)
                .unwrap_or_else(|| {
                    effect(
                        &request,
                        "question-links",
                        Target::Thread {
                            thread: question.thread_id.clone(),
                        },
                        &question_links,
                        false,
                        false,
                    )
                })?;
            ensure(
                &mut api,
                &mut ledger,
                &home,
                &request,
                "question-links",
                links,
                &[],
            )?;
            let origin_links = format!(
                "新しい質問: <{}>\n進捗: <{}>",
                request.destination.url(&question.thread_id),
                request.destination.url(&progress.thread_id)
            );
            ensure(
                &mut api,
                &mut ledger,
                &home,
                &request,
                "origin-links",
                effect(
                    &request,
                    "origin-links",
                    Target::Thread {
                        thread: origin_thread.clone(),
                    },
                    &origin_links,
                    false,
                    false,
                )?,
                &[],
            )?;
        }
    }
    if !ledger.complete(&request.request_id) {
        ledger.append(
            &home,
            Event::DeliveryVerified {
                request_id: request.request_id.clone(),
            },
        )?;
    }
    Ok(report(&ledger, &request, reused))
}

// A semantic image conflict is terminal for this new input, including when the
// original question is still reserved. Never reserve it or prompt endless POST
// retries. Any returned URL is independently rechecked with GET.
fn different_question_images(
    ctx: &mut Context,
    ledger: &mut Ledger,
    incoming: &Request,
) -> Result<Option<Value>> {
    if ledger.request(&incoming.request_id).is_some() {
        return Ok(None);
    }
    let Some(original) = ledger.events.iter().find_map(|event| {
        let Event::Intent { request } = event else {
            return None;
        };
        match (&request.operation, &incoming.operation) {
            (
                Kind::Ask {
                    decision_identity: a,
                    ..
                },
                Kind::Ask {
                    decision_identity: b,
                    ..
                },
            ) if request.destination == incoming.destination
                && a == b
                && request.images != incoming.images =>
            {
                Some(request.as_ref().clone())
            }
            _ => None,
        }
    }) else {
        return Ok(None);
    };
    let mut thread = String::new();
    let mut url = String::new();
    if let Some(receipt) = ledger.receipt(&original.request_id, "question:0").cloned() {
        let effect = ledger
            .effect(&original.request_id, "question:0")
            .cloned()
            .ok_or("saved original question effect unavailable")?;
        let mut api = RecoveryApi::bind(ctx, ledger, &original)?;
        api.verify(
            ledger,
            &original,
            &effect,
            &receipt.thread_id,
            &receipt.message_id,
        )?;
        thread = receipt.thread_id;
        url = original.destination.url(&thread);
    }
    let todo = match original.operation {
        Kind::Ask { todo_id, .. } => todo_id,
        _ => unreachable!(),
    };
    Ok(Some(
        json!({"domain":DOMAIN,"request_id":incoming.request_id,"outcome":"existing_question_images_differ",
        "delivery":"not_sent","todo_id":todo,"existing_request_id":original.request_id,
        "thread_id":thread,"url":url,"existing_metadata_verified":!url.is_empty(),"wait_requested":false}),
    ))
}

fn owner_replies(
    api: &mut RecoveryApi<'_>,
    ledger: &Ledger,
    request: &Request,
) -> Result<Vec<Reply>> {
    let Kind::Ask {
        todo_id,
        decision_identity,
        ..
    } = &request.operation
    else {
        return Err("exact new recovery question required".into());
    };
    let receipt = ledger
        .receipt(&request.request_id, "question:0")
        .ok_or("question starter is not verified")?;
    let expected = ledger
        .effect(&request.request_id, "question:0")
        .ok_or("question body receipt is missing")?;
    api.verify(
        ledger,
        request,
        expected,
        &receipt.thread_id,
        &receipt.message_id,
    )?;
    let start_id = receipt
        .message_id
        .parse::<u64>()
        .map_err(|_| "invalid saved starter ID")?;
    let secrets = SecretCheck::new();
    let mut replies = Vec::new();
    for message in api.history(&receipt.thread_id)? {
        if message["author"]["id"] != request.destination.owner
            || message["author"]["bot"] == true
            || message.get("webhook_id").is_some_and(|v| !v.is_null())
            || core::n(&message["id"]) <= start_id
            || !matches!(message["type"].as_u64(), Some(0 | 19))
        {
            continue;
        }
        let body = message["content"]
            .as_str()
            .ok_or("owner reply body missing")?;
        if body.len() > TEXT_BYTES {
            return Err("owner reply exceeds the evidence size limit".into());
        }
        secrets.text(body)?;
        let timestamp = message["timestamp"]
            .as_str()
            .ok_or("owner reply timestamp missing")?;
        let time = chrono::DateTime::parse_from_rfc3339(timestamp)
            .map_err(|_| "invalid owner reply timestamp")?;
        if time
            < chrono::DateTime::parse_from_rfc3339(&receipt.timestamp)
                .map_err(|_| "invalid starter timestamp")?
        {
            return Err("owner reply predates this question generation".into());
        }
        replies.push(Reply {
            request_id: request.request_id.clone(),
            todo_id: *todo_id,
            question_thread_id: receipt.thread_id.clone(),
            question_message_id: receipt.message_id.clone(),
            author_id: request.destination.owner.clone(),
            message_id: snowflake(core::s(&message["id"]))?,
            timestamp: timestamp.into(),
            body: body.into(),
            decision_identity: decision_identity.clone(),
        });
    }
    Ok(replies)
}

/// Strictly read-only: no lock creation, ledger append, acknowledgement, message,
/// PATCH, delivery, consumed marker, old-state read or worker dispatch.
/// Returned evidence carries the exact original scope for the parent to record.
pub fn replies(ctx: &mut Context, args: &RepliesArgs) -> Result<Value> {
    let thread = snowflake(&args.thread)?;
    let ledger = Ledger::load(&ctx.here)?;
    let destination = Destination::from_context(ctx)?;
    let candidates: Vec<_> = ledger
        .events
        .iter()
        .filter_map(|event| {
            let Event::Intent { request } = event else {
                return None;
            };
            (matches!(request.operation, Kind::Ask { .. })
                && request.destination == destination
                && ledger
                    .receipt(&request.request_id, "question:0")
                    .is_some_and(|r| r.thread_id == thread))
            .then_some(request)
        })
        .collect();
    let [request] = candidates.as_slice() else {
        return Err("exact thread has no unique new recovery question receipt".into());
    };
    if args.request_id.as_deref().is_some_and(|id| {
        ledger
            .request(id)
            .is_none_or(|record| record.request_id != request.request_id)
    }) || args
        .todo_id
        .is_some_and(|id| !matches!(&request.operation,Kind::Ask {todo_id,..} if *todo_id == id))
        || args.message.as_deref().is_some_and(|id| {
            ledger
                .receipt(&request.request_id, "question:0")
                .is_none_or(|r| r.message_id != id)
        })
    {
        return Err("exact recovery reply request/todo/starter association differs".into());
    }
    let bot = ledger
        .bot(&request.request_id)
        .ok_or("saved recovery bot receipt missing")?;
    let me = ctx
        .effects
        .http("GET", &format!("{API}/users/@me"), None, true)
        .map_err(|_| "cannot verify read-only recovery bot")?;
    if me["bot"] != true || me["id"] != bot {
        return Err("read-only recovery bot differs from saved author".into());
    }
    let mut api = RecoveryApi {
        effects: ctx.effects.as_mut(),
        destination,
        bot: bot.into(),
    };
    let evidence = owner_replies(&mut api, &ledger, request)?;
    // Extra version evidence is outside the existing reply hashes/close inputs.
    let mut reply_versions = BTreeMap::new();
    for reply in &evidence {
        let raw = api.get(&format!(
            "/channels/{}/messages/{}",
            thread, reply.message_id
        ))?;
        if !exact_owner_reply(&raw, &thread, &reply.message_id, &request.destination.owner)
            || raw["content"] != reply.body
            || raw["timestamp"] != reply.timestamp
        {
            return Err("owner reply changed while reading version evidence; retry GET".into());
        }
        let edited = &raw["edited_timestamp"];
        if !edited.is_null() {
            chrono::DateTime::parse_from_rfc3339(
                edited.as_str().ok_or("invalid reply edit timestamp")?,
            )
            .map_err(|_| "invalid reply edit timestamp")?;
        }
        reply_versions.insert(reply.message_id.clone(), json!({"edited_timestamp":edited}));
    }
    Ok(
        json!({"domain":DOMAIN,"read_only":true,"ledger_written":false,"legacy_state":"unchanged",
        "thread_id":thread,"request_id":request.request_id,"original_question":request.body,
        "source":"discord-api-v10","owner_id":request.destination.owner,
        "question_fingerprint":request.fingerprint,
        "question_message_id":ledger.receipt(&request.request_id,"question:0").map(|r|&r.message_id),
        "question_timestamp":ledger.receipt(&request.request_id,"question:0").map(|r|&r.timestamp),
        "decision_identity":match &request.operation {Kind::Ask{decision_identity,..}=>Some(decision_identity),_=>None},
        "progress_thread_id":ledger.receipt(&request.request_id,"progress").map(|r|&r.thread_id),
        "original_decision":match &request.operation {Kind::Ask {decision_original,..}=>Some(decision_original),_=>None},
        "owner_replies":evidence,"reply_versions":reply_versions,"delivery":"inspection_only","approval":"not_inferred"}),
    )
}

pub(crate) fn validate_followup_text(text: &str) -> Result<()> {
    SecretCheck::new().text(text)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::{Effects, Output};
    use std::sync::{mpsc, Arc, Mutex};

    const GUILD: &str = "102";
    const FORUM: &str = "103";
    const OWNER: &str = "100";
    const BOT: &str = "101";
    const REQUEST_FORUM: &str = "104";
    const ORIGIN: &str = "105";

    #[test]
    fn generated_proposal_receipt_binds_request_both_paths_hash_and_expiry() {
        let now = chrono::DateTime::parse_from_rfc3339("2026-10-13T09:40:00Z")
            .unwrap()
            .to_utc();
        let source = UI_PROPOSAL_SOURCE.to_lowercase();
        let copy = UI_PROPOSAL_COPY.to_lowercase();
        let allowed = |request, path, resolved, original, hash, at| {
            proposal_receipt_matches(request, path, resolved, original, hash, at)
        };
        assert!(allowed(
            UI_PROPOSAL_REQUEST,
            &copy,
            &copy,
            &source,
            UI_PROPOSAL_SHA256,
            now
        ));
        assert!(allowed(
            UI_PROPOSAL_REQUEST,
            &source,
            &source,
            &source,
            UI_PROPOSAL_SHA256,
            now
        ));
        for (request, path, resolved, original, hash, at) in [
            (
                "another-request",
                copy.as_str(),
                copy.as_str(),
                source.as_str(),
                UI_PROPOSAL_SHA256,
                now,
            ),
            (
                UI_PROPOSAL_REQUEST,
                "d:/other.png",
                copy.as_str(),
                source.as_str(),
                UI_PROPOSAL_SHA256,
                now,
            ),
            (
                UI_PROPOSAL_REQUEST,
                copy.as_str(),
                "d:/linked.png",
                source.as_str(),
                UI_PROPOSAL_SHA256,
                now,
            ),
            (
                UI_PROPOSAL_REQUEST,
                copy.as_str(),
                copy.as_str(),
                "d:/other-source.png",
                UI_PROPOSAL_SHA256,
                now,
            ),
            (
                UI_PROPOSAL_REQUEST,
                copy.as_str(),
                copy.as_str(),
                source.as_str(),
                "changed-content",
                now,
            ),
            (
                UI_PROPOSAL_REQUEST,
                copy.as_str(),
                copy.as_str(),
                source.as_str(),
                UI_PROPOSAL_SHA256,
                chrono::DateTime::parse_from_rfc3339("2026-10-12T23:59:59Z")
                    .unwrap()
                    .to_utc(),
            ),
            (
                UI_PROPOSAL_REQUEST,
                copy.as_str(),
                copy.as_str(),
                source.as_str(),
                UI_PROPOSAL_SHA256,
                chrono::DateTime::parse_from_rfc3339("2026-10-21T00:00:00Z")
                    .unwrap()
                    .to_utc(),
            ),
        ] {
            assert!(!allowed(request, path, resolved, original, hash, at));
        }
    }

    #[test]
    fn generated_proposal_missing_registry_still_rejects_other_images_before_http() {
        let fixture = Fixture::new();
        let image = fixture.root.path().join("other.png");
        fs::write(&image, b"\x89PNG\r\n\x1a\nother generated image").unwrap();
        let mut ctx = fixture.context();
        ctx.client_projects = fixture.root.path().join("missing-exclusions.txt");
        for request in [UI_PROPOSAL_REQUEST, "ordinary-image-request"] {
            let args = LogArgs {
                attach: vec![image.clone()],
                ..fixture.log(request, "確認用画像です。", false)
            };
            assert!(log(&mut ctx, &args).is_err());
            assert!(fixture.server.lock().unwrap().calls.is_empty());
            assert!(!fixture.home.join(LEDGER).exists());
        }
        assert!(!ctx.client_projects.exists());
        fixture.unchanged();
    }

    #[test]
    fn generated_proposal_missing_registry_rejects_multiple_attachments_before_http() {
        let fixture = Fixture::new();
        let image = fixture.root.path().join("other.png");
        fs::write(&image, b"\x89PNG\r\n\x1a\nimage").unwrap();
        let mut ctx = fixture.context();
        ctx.client_projects = fixture.root.path().join("missing-exclusions.txt");
        let args = LogArgs {
            attach: vec![image.clone(), image],
            ..fixture.log(UI_PROPOSAL_REQUEST, "確認用画像です。", false)
        };
        assert!(log(&mut ctx, &args).is_err());
        assert!(fixture.server.lock().unwrap().calls.is_empty());
        assert!(!fixture.home.join(LEDGER).exists());
        fixture.unchanged();
    }

    #[test]
    fn generated_proposal_request_does_not_override_an_available_denial() {
        let fixture = Fixture::new();
        let image = fixture.root.path().join("denied.png");
        fs::write(&image, b"\x89PNG\r\n\x1a\nimage").unwrap();
        let mut ctx = fixture.context();
        ctx.client_projects = fixture.root.path().join("specific-exclusions.txt");
        let exclusions = image.to_string_lossy().to_string();
        fs::write(&ctx.client_projects, &exclusions).unwrap();
        let args = LogArgs {
            attach: vec![image],
            ..fixture.log(UI_PROPOSAL_REQUEST, "確認用画像です。", false)
        };
        assert_eq!(
            log(&mut ctx, &args).unwrap_err(),
            "protected files cannot be recovery attachments"
        );
        assert!(fixture.server.lock().unwrap().calls.is_empty());
        assert!(!fixture.home.join(LEDGER).exists());
        assert_eq!(
            fs::read_to_string(&ctx.client_projects).unwrap(),
            exclusions
        );
        fixture.unchanged();
    }

    #[derive(Default)]
    struct Server {
        channels: BTreeMap<String, Value>,
        messages: BTreeMap<String, Vec<Value>>,
        calls: Vec<(String, String, Option<Value>)>,
        posts: Vec<(String, Value)>,
        next: u64,
        drop_response: bool,
        reject_post: bool,
        reject_progress: bool,
        block_reads_after_post: bool,
        fail_forum_once: bool,
        archive_cursor_page: bool,
        blocked: bool,
        wrong_flags: bool,
        wrong_mentions: bool,
        forbidden_effects: usize,
        network_without_intent: usize,
        posts_without_pending: usize,
        multipart_uploads: usize,
        inject_owner_reply: Option<(String, String)>,
        close_drop_response: bool,
        close_block_reads: bool,
        close_reject_patch: bool,
        close_no_effect: bool,
        bot_override: Option<String>,
    }

    struct Mock {
        server: Arc<Mutex<Server>>,
        home: PathBuf,
        pause: Option<(mpsc::Sender<()>, mpsc::Receiver<()>)>,
    }

    impl Mock {
        fn check_pending(&self, state: &mut Server, method: &str, body: Option<&Value>) {
            let ledger = fs::read(self.home.join(LEDGER))
                .ok()
                .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok());
            let events = ledger.as_ref().and_then(|value| value["events"].as_array());
            if !events.is_some_and(|events| events.iter().any(|event| event["event"] == "intent")) {
                state.network_without_intent += 1;
            }
            if method == "POST" {
                let content = body
                    .and_then(|value| value.get("message").unwrap_or(value)["content"].as_str());
                if !events.is_some_and(|events| {
                    events.iter().any(|event| {
                        event["event"] == "pending"
                            && event["effect"]["content"].as_str() == content
                    })
                }) {
                    state.posts_without_pending += 1;
                }
            }
        }
    }

    fn server_message(state: &mut Server, thread: &str, id: &str, payload: &Value) -> Value {
        let mentions = payload["allowed_mentions"]["users"]
            .as_array()
            .unwrap()
            .iter()
            .map(|id| json!({"id":id}))
            .collect::<Vec<_>>();
        let mut message = json!({"id":id,"channel_id":thread,"author":{"id":BOT,"bot":true},
            "content":payload["content"],"flags":payload["flags"],"mentions":mentions,
            "mention_everyone":false,"mention_roles":[],"type":0,"timestamp":"2026-10-12T00:00:00+00:00",
            "attachments":payload.get("attachments").cloned().unwrap_or(json!([]))});
        if state.wrong_flags {
            message["flags"] = json!(core::n(&message["flags"]) ^ SILENT);
        }
        if state.wrong_mentions {
            message["mentions"] = json!([{"id":"999"}]);
        }
        message
    }

    impl Effects for Mock {
        fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
            self.server.lock().unwrap().forbidden_effects += 1;
            Err("no subprocess is part of recovery messages".into())
        }
        fn http(
            &mut self,
            method: &str,
            url: &str,
            body: Option<&Value>,
            discord: bool,
        ) -> Result<Value> {
            if method == "POST" {
                if let Some((entered, release)) = self.pause.take() {
                    let _ = entered.send(());
                    release
                        .recv_timeout(Duration::from_secs(30))
                        .map_err(|_| "test release missing")?;
                }
            }
            let path = url.strip_prefix(API).ok_or("unexpected non-Discord URL")?;
            let mut state = self.server.lock().unwrap();
            if matches!(method, "POST" | "PATCH") {
                if let Some((thread, content)) = state.inject_owner_reply.take() {
                    state.next += 1;
                    let id = state.next.to_string();
                    state.messages.entry(thread.clone()).or_default().push(json!({"id":id,"channel_id":thread,
                        "author":{"id":OWNER,"bot":false},"content":content,"type":0,"timestamp":"2026-10-12T00:00:03Z"}));
                }
            }
            state
                .calls
                .push((method.into(), path.into(), body.cloned()));
            if !discord || !matches!(method, "GET" | "POST" | "PATCH") {
                state.forbidden_effects += 1;
                return Err("unexpected recovery effect".into());
            }
            self.check_pending(&mut state, method, body);
            if method == "GET" && state.blocked {
                return Err("simulated interrupted GET".into());
            }
            if method == "GET" && path == format!("/channels/{FORUM}") && state.fail_forum_once {
                state.fail_forum_once = false;
                return Err("simulated preflight forum GET failure".into());
            }
            if method == "GET" && path == "/users/@me" {
                return Ok(json!({"id":state.bot_override.as_deref().unwrap_or(BOT),"bot":true}));
            }
            if method == "GET" && path == format!("/guilds/{GUILD}/threads/active") {
                return Ok(json!({"threads":state.channels.values()
                    .filter(|channel|channel["type"]==11).cloned().collect::<Vec<_>>()}));
            }
            if method == "GET"
                && path.starts_with(&format!("/channels/{FORUM}/threads/archived/public"))
            {
                if state.archive_cursor_page {
                    if !path.contains("before=") {
                        return Ok(json!({"threads":[{"id":"444","name":"older fixture",
                            "parent_id":FORUM,"owner_id":BOT,"thread_metadata":{"archive_timestamp":"2026-10-09T12:00:00.000000+00:00"}}],"has_more":true}));
                    }
                    if !path.contains("%2B00%3A00") || path.contains("+00:00") {
                        return Err("archive timestamp is not a query component".into());
                    }
                }
                return Ok(json!({"threads":[],"has_more":false}));
            }
            let route = path.split('?').next().unwrap_or(path);
            let segments: Vec<_> = route
                .split('/')
                .filter(|segment| !segment.is_empty())
                .collect();
            if method == "PATCH"
                && segments.len() == 2
                && segments[0] == "channels"
                && body == Some(&json!({"archived":true}))
            {
                let close: Value = core::read_json(&self.home.join(CLOSE_LEDGER))?;
                if close["records"][segments[1]]["phase"] != "patch_pending" {
                    return Err("PATCH without durable close intent".into());
                }
                if state.close_reject_patch {
                    return Err("mock rejected PATCH".into());
                }
                let no_effect = state.close_no_effect;
                let channel = state
                    .channels
                    .get_mut(segments[1])
                    .ok_or("close channel missing")?;
                if !no_effect {
                    channel["thread_metadata"]["archived"] = json!(true);
                }
                let out = channel.clone();
                if state.close_block_reads {
                    state.blocked = true;
                }
                if state.close_drop_response {
                    return Err("mock accepted PATCH response lost".into());
                }
                return Ok(out);
            }
            if method == "GET" && segments.len() == 2 && segments[0] == "channels" {
                return state
                    .channels
                    .get(segments[1])
                    .cloned()
                    .ok_or("missing mock channel".into());
            }
            if method == "GET" && segments.len() == 4 && segments[2] == "messages" {
                return state
                    .messages
                    .get(segments[1])
                    .into_iter()
                    .flatten()
                    .find(|message| message["id"] == segments[3])
                    .cloned()
                    .ok_or("missing mock message".into());
            }
            if method == "GET" && segments.len() == 3 && segments[2] == "messages" {
                let before = path
                    .split("&before=")
                    .nth(1)
                    .and_then(|id| id.parse::<u64>().ok())
                    .unwrap_or(u64::MAX);
                let mut rows = state.messages.get(segments[1]).cloned().unwrap_or_default();
                rows.retain(|message| core::n(&message["id"]) < before);
                rows.sort_by_key(|message| std::cmp::Reverse(core::n(&message["id"])));
                rows.truncate(100);
                return Ok(json!(rows));
            }
            if method == "POST" && segments.len() == 3 && segments[0] == "channels" {
                let body = body.ok_or("POST body missing")?.clone();
                state.posts.push((path.into(), body.clone()));
                if state.reject_progress && core::s(&body["name"]).starts_with("進捗 ") {
                    return Err("simulated progress failure".into());
                }
                if state.reject_post {
                    return Err("simulated unknown POST result".into());
                }
                state.next += 1;
                let id = state.next.to_string();
                let output = if segments[1] == FORUM && segments[2] == "threads" {
                    let channel = json!({"id":id,"type":11,"guild_id":GUILD,"parent_id":FORUM,
                        "owner_id":BOT,"name":body["name"],"thread_metadata":{"archived":false}});
                    let message = server_message(&mut state, &id, &id, &body["message"]);
                    state.channels.insert(id.clone(), channel.clone());
                    state.messages.insert(id.clone(), vec![message.clone()]);
                    let mut output = channel;
                    output["message"] = message;
                    output
                } else if segments[2] == "messages" && state.channels.contains_key(segments[1]) {
                    let message = server_message(&mut state, segments[1], &id, &body);
                    state
                        .messages
                        .entry(segments[1].into())
                        .or_default()
                        .push(message.clone());
                    message
                } else {
                    state.forbidden_effects += 1;
                    return Err("POST outside mock message scope".into());
                };
                if state.block_reads_after_post {
                    state.blocked = true;
                }
                if state.drop_response {
                    state.drop_response = false;
                    return Err("simulated accepted POST with lost response".into());
                }
                return Ok(output);
            }
            state.forbidden_effects += 1;
            Err("unexpected recovery HTTP route".into())
        }
        fn multipart(&mut self, url: &str, content_type: &str, bytes: &[u8]) -> Result<Value> {
            fn find(bytes: &[u8], needle: &[u8]) -> Result<usize> {
                bytes
                    .windows(needle.len())
                    .position(|part| part == needle)
                    .ok_or("mock multipart boundary missing".into())
            }
            let boundary = content_type
                .split("boundary=")
                .nth(1)
                .ok_or("mock boundary missing")?;
            let start = find(bytes, b"\r\n\r\n")? + 4;
            let separator = format!("\r\n--{boundary}");
            let end = start + find(&bytes[start..], separator.as_bytes())?;
            let mut payload: Value = serde_json::from_slice(&bytes[start..end])
                .map_err(|_| "mock multipart JSON missing")?;
            let message = if payload.get("message").is_some() {
                &mut payload["message"]
            } else {
                &mut payload
            };
            for (id, file) in message["attachments"]
                .as_array_mut()
                .ok_or("mock attachments missing")?
                .iter_mut()
                .enumerate()
            {
                let label = format!("name=\"files[{id}]\"");
                let header = find(bytes, label.as_bytes())?;
                let data = header + find(&bytes[header..], b"\r\n\r\n")? + 4;
                let size = find(&bytes[data..], separator.as_bytes())?;
                file["size"] = json!(size);
            }
            self.server.lock().unwrap().multipart_uploads += 1;
            self.http("POST", url, Some(&payload), true)
        }
    }

    struct Fixture {
        root: tempfile::TempDir,
        home: PathBuf,
        server: Arc<Mutex<Server>>,
        guards: Vec<(PathBuf, Vec<u8>)>,
    }

    impl Fixture {
        fn new() -> Self {
            let root = tempfile::tempdir().unwrap();
            let home = root.path().join("message-home");
            fs::create_dir(&home).unwrap();
            let mut state = Server {
                next: 1000,
                ..Server::default()
            };
            state
                .channels
                .insert(FORUM.into(), json!({"id":FORUM,"guild_id":GUILD,"type":15}));
            state.channels.insert(
                ORIGIN.into(),
                json!({"id":ORIGIN,"type":11,"guild_id":GUILD,
                "parent_id":REQUEST_FORUM,"owner_id":OWNER,"name":"本人の新しい作業指示"}),
            );
            // This old question is visible remotely but has no independent recovery receipt.
            state.channels.insert(
                "106".into(),
                json!({"id":"106","type":11,"guild_id":GUILD,
                "parent_id":FORUM,"owner_id":BOT,"name":"停止した旧質問"}),
            );
            let guards = [
                (
                    "state.json",
                    b"{\"questions\":[],\"sentinel\":\"lost-not-authoritative\"}\n".to_vec(),
                ),
                (
                    "question-recovery-required.json",
                    b"{\"recovery_required\":true,\"pending\":\"unknown\"}\n".to_vec(),
                ),
                (
                    "state-work.lock",
                    b"original worker lock sentinel\n".to_vec(),
                ),
            ]
            .into_iter()
            .map(|(name, bytes)| {
                let path = home.join(name);
                fs::write(&path, &bytes).unwrap();
                (path, bytes)
            })
            .collect();
            fs::write(
                root.path().join("clients.txt"),
                "# no protected fixture paths\n",
            )
            .unwrap();
            Self {
                root,
                home,
                server: Arc::new(Mutex::new(state)),
                guards,
            }
        }
        fn context(&self) -> Context {
            context(&self.home, self.root.path(), self.server.clone(), None)
        }
        fn log(&self, id: &str, body: &str, notify: bool) -> LogArgs {
            let path = self.root.path().join(format!("{id}.txt"));
            fs::write(&path, body).unwrap();
            LogArgs {
                request_id: id.into(),
                title: "【情報システム部】作業完了".into(),
                body_file: path,
                thread: None,
                notify,
                attach: vec![],
            }
        }
        fn ask(&self, id: &str, body: &str) -> AskArgs {
            let body_file = self.root.path().join(format!("{id}.txt"));
            let decision_file = self.root.path().join(format!("{id}.json"));
            fs::write(&body_file, body).unwrap();
            fs::write(&decision_file,serde_json::to_vec(&json!({"todo_id":2472,"origin_thread_id":ORIGIN,
                "resources":[{"kind":"worker-registration","id":"opaque-94"}],"decision":"direction",
                "scope":{"registration_ids":["opaque-94"],"retain_incomplete":true}})).unwrap()).unwrap();
            AskArgs {
                request_id: id.into(),
                todo_id: 2472,
                decision_file,
                body_file,
                title: Some("【情報システム部】指定外の登録の扱い".into()),
                origin_thread: None,
                attach: vec![],
                expected_image_sha256: vec![],
                program_evidence: None,
            }
        }
        fn unchanged(&self) {
            for (path, bytes) in &self.guards {
                assert_eq!(&fs::read(path).unwrap(), bytes);
            }
            let state = self.server.lock().unwrap();
            assert_eq!(state.forbidden_effects, 0);
            assert_eq!(state.network_without_intent, 0);
            assert_eq!(state.posts_without_pending, 0);
        }
        fn add_reply(&self, thread: &str, author: &str, body: &str, bot: bool) -> String {
            let mut state = self.server.lock().unwrap();
            state.next += 1;
            let id = state.next.to_string();
            state.messages.entry(thread.into()).or_default().push(
                json!({"id":id,"channel_id":thread,
                "author":{"id":author,"bot":bot},"content":body,"type":0,
                "timestamp":"2026-10-12T00:00:01+00:00"}),
            );
            id
        }
    }

    fn context(
        home: &Path,
        root: &Path,
        server: Arc<Mutex<Server>>,
        pause: Option<(mpsc::Sender<()>, mpsc::Receiver<()>)>,
    ) -> Context {
        Context {
            here: home.into(),
            client_projects: root.join("clients.txt"),
            jobs: root.join("jobs"),
            discord_tasks: root.join("tasks"),
            todo: root.join("todo.json"),
            civ6_home: root.join("civ6"),
            cfg: json!({"guild_id":GUILD,"task_channel_id":FORUM,"owner_user_id":OWNER,"request_channel_id":REQUEST_FORUM}),
            effects: Box::new(Mock {
                server,
                home: home.into(),
                pause,
            }),
            codex_command: root.join("no-codex"),
            scheduler_command: root.join("no-scheduler"),
            shepherd_command: root.join("no-shepherd"),
            todo_command: root.join("no-todo"),
        }
    }

    fn reply_args(result: &Value) -> RepliesArgs {
        RepliesArgs {
            thread: core::s(&result["thread_id"]).into(),
            message: Some(core::s(&result["message_id"]).into()),
            request_id: Some(core::s(&result["request_id"]).into()),
            todo_id: Some(2472),
            read_only: true,
        }
    }

    mod decision_lookup_tests {
        include!("recovery_messages/decision_lookup_tests.rs");
    }

    fn clarification_fixture(f: &Fixture, ctx: &mut Context) -> (LogArgs, PathBuf, Value) {
        clarification_fixture_body(f, ctx, "これは元の質問の背景を補う説明です。")
    }

    fn clarification_fixture_body(
        f: &Fixture,
        ctx: &mut Context,
        body: &str,
    ) -> (LogArgs, PathBuf, Value) {
        clarification_case(f, ctx, body, true)
    }

    fn clarification_case(
        f: &Fixture,
        ctx: &mut Context,
        body: &str,
        resume: bool,
    ) -> (LogArgs, PathBuf, Value) {
        let question = f.ask("clarification-question", "元の仕様を選んでください。");
        let sent = ask(ctx, &question).unwrap();
        let thread = core::s(&sent["thread_id"]);
        let reply = f.add_reply(thread, OWNER, "何のための質問か説明してください。", false);
        let mut args = f.log("clarification-log", body, false);
        args.thread = Some(thread.into());
        // The ordinary request already failed before reserving any POST.
        if resume {
            assert!(log(ctx, &args).unwrap_err().contains("explicit log thread"));
        }
        let ledger = Ledger::load(&f.home).unwrap();
        let original = ledger.request(&question.request_id).unwrap();
        let hash = |body: &str| format!("{:x}", Sha256::digest(body.as_bytes()));
        let binding = json!({"domain":"discord-question-clarification-v1",
            "purpose":"explain_original_question", "request_id":args.request_id,
            "question_request_id":question.request_id,"todo_id":question.todo_id,
            "thread_id":thread,"question_message_id":sent["message_id"],
            "question_fingerprint":original.fingerprint,"question_body_sha256":hash(&original.body),
            "bot_id":BOT,"owner_id":OWNER,"reply_message_id":reply,
            "reply_body_sha256":hash("何のための質問か説明してください。"),
            "reply_timestamp":"2026-10-12T00:00:01+00:00","reply_edited_timestamp":null,
            "body_sha256":hash(body)});
        let manifest = f.root.path().join("clarification.json");
        fs::write(&manifest, serde_json::to_vec(&binding).unwrap()).unwrap();
        (args, manifest, binding)
    }

    #[test]
    fn first_clarification_checks_without_intent_then_delivers_once() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) = clarification_case(&f, &mut ctx, "費用の内訳の説明です。", false);
        let before = fs::read(f.home.join(LEDGER)).unwrap();
        let posts = f.server.lock().unwrap().posts.len();
        let check = check_clarification(&mut ctx, &args, &manifest).unwrap();
        assert_eq!(check["delivery"], "not_sent");
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
        assert!(!f.home.join("recovery-clarifications.json").exists());
        let first = log_clarification(&mut ctx, &args, &manifest).unwrap();
        let again = log_clarification(&mut ctx, &args, &manifest).unwrap();
        assert_eq!(first["delivery"], "verified");
        assert_eq!(first["message_id"], again["message_id"]);
        assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
        let ledger = Ledger::load(&f.home).unwrap();
        assert_eq!(
            ledger.request(&args.request_id).unwrap().fingerprint,
            first["original_fingerprint"]
        );
        f.unchanged();
    }

    #[test]
    fn first_clarification_rejects_invalid_scope_before_any_journal_write() {
        for field in [
            "purpose",
            "question_request_id",
            "todo_id",
            "question_fingerprint",
            "question_body_sha256",
            "bot_id",
            "owner_id",
            "reply_message_id",
            "reply_body_sha256",
            "reply_timestamp",
            "reply_edited_timestamp",
            "body_sha256",
            "thread_id",
            "question_message_id",
        ] {
            let f = Fixture::new();
            let mut ctx = f.context();
            let (args, manifest, mut proof) =
                clarification_case(&f, &mut ctx, "元質問への説明です。", false);
            proof[field] = if field == "todo_id" {
                json!(999)
            } else if field.ends_with("sha256") || field == "question_fingerprint" {
                json!("c".repeat(64))
            } else if field == "reply_edited_timestamp" {
                json!("2026-10-12T00:00:02Z")
            } else if field == "reply_timestamp" {
                json!("2026-10-12T00:00:03Z")
            } else if field.ends_with("_id") {
                json!("999")
            } else {
                json!("wrong")
            };
            fs::write(&manifest, proof.to_string()).unwrap();
            let before = fs::read(f.home.join(LEDGER)).unwrap();
            let posts = f.server.lock().unwrap().posts.len();
            assert!(
                check_clarification(&mut ctx, &args, &manifest).is_err(),
                "{field}"
            );
            assert!(
                log_clarification(&mut ctx, &args, &manifest).is_err(),
                "{field}"
            );
            assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before, "{field}");
            assert!(
                !f.home.join("recovery-clarifications.json").exists(),
                "{field}"
            );
            assert_eq!(f.server.lock().unwrap().posts.len(), posts);
            f.unchanged();
        }
    }

    #[test]
    fn first_clarification_owner_edit_after_check_stops_first_send() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, proof) =
            clarification_case(&f, &mut ctx, "元質問への説明です。", false);
        check_clarification(&mut ctx, &args, &manifest).unwrap();
        let before = fs::read(f.home.join(LEDGER)).unwrap();
        let posts = f.server.lock().unwrap().posts.len();
        let mut server = f.server.lock().unwrap();
        let row = server
            .messages
            .get_mut(proof["thread_id"].as_str().unwrap())
            .unwrap()
            .iter_mut()
            .find(|v| v["id"] == proof["reply_message_id"])
            .unwrap();
        row["edited_timestamp"] = json!("2026-10-12T00:00:03Z");
        drop(server);
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
        assert_eq!(f.server.lock().unwrap().posts.len(), posts);
        assert!(!f.home.join("recovery-clarifications.json").exists());
        f.unchanged();
    }

    #[test]
    fn first_clarification_accepts_a_question_about_necessity_but_not_a_choice() {
        let mut mismatches = Vec::new();
        let issue_cases: Vec<(String, bool)> =
            serde_json::from_str(include_str!("../tests/fixtures/clarification-414.json")).unwrap();
        for (reply, accepted) in [
            ("これでやって？", false),
            ("作って？", false),
            ("送って？", false),
            ("使って？", false),
            ("待って？", false),
            ("作業領域ってなに？", true),
            ("作業領域って何？", true),
            ("作業領域とは何ですか", true),
            (
                "まず、作業領域ってなに？そして、作業領域を二種類に制限したい。",
                true,
            ),
            ("作業領域とは？", true),
            ("作業領域ってなにか変わったね。", false),
            ("「作業領域ってなに？」という質問を保存して", false),
            ("作業領域ってなに？ aで進めてください", false),
            ("その費用は必要ではないの？", true),
            ("なんで？", true),
            ("どういうこと？", true),
            ("これ要る？", true),
            ("何の費用？", true),
            ("なぜ必要なのか教えて", true),
            ("どうして必要", true),
            ("なんで", true),
            ("内訳を教えて", true),
            ("内訳を教えてほしいです", true),
            ("もっと詳しく教えてください", true),
            ("bを選んだら費用はいくら？", true),
            ("bを選べば費用はどうなる？", true),
            ("aにしたら何の機能が止まる？", true),
            ("aであれば費用は必要なの？", true),
            ("なんでaがいいの？", true),
            ("aがいい理由は？", true),
            ("aが良いのはなぜ？", true),
            ("なぜaにしたの？", true),
            ("cにした理由は？", true),
            ("aにすると何の機能が止まる？", true),
            ("aにすると、何の機能が止まる？", true),
            ("なぜaにしたらいいの？", true),
            ("なぜか教えて", true),
            ("なぜかな？", true),
            ("なぜ、aにしたの？", true),
            ("aにするのはなぜ？", true),
            ("aにする場合、費用は？", true),
            ("aにするとき、費用は？", true),
            ("aにする時、費用は？", true),
            ("aが良いのはなぜか？", true),
            ("なぜかを教えて", true),
            ("aでも必要？", true),
            ("aでの費用は？", true),
            ("aで進めたら費用はいくら？", true),
            ("aでいいのはなぜ？", true),
            ("aがいいと思う理由は？", true),
            ("aを採用すると費用は？", true),
            ("aにしたらいい理由は？", true),
            ("aにしたのはなぜか教えて", true),
            ("なぜaにしたのか教えて？", true),
            ("なんで、勝手にaにしたの？", true),
            ("aにしたのは、なぜですか？", true),
            ("なぜaにしたのでしょうか？", true),
            ("なぜaで進めているのですか", true),
            ("なんでaで固定しているんですか", true),
            ("なぜaにしたのですか", true),
            ("aで大丈夫なんでお願いします", false),
            ("aで大丈夫なんですが、進めてください", false),
            ("bで決まりなんで進めて", false),
            ("aで十分なんですよ。進めて", false),
            ("aでいいよなんで？", false),
            ("bでOKなんで聞くの？", false),
            ("aでいいよなんで必要？", false),
            ("aで大丈夫なんで、お願いします", false),
            ("bで決まりなんで 進めて", false),
            ("aで十分なんで。進めて", false),
            ("aでいいよなぜ必要？", false),
            ("bでOKどうして聞くの？", false),
            ("どうして止まったか知らないがaで進めてもらえますか", false),
            ("なぜ止まったか知らないがbで進めていただけますか", false),
            ("なぜaで十分なんですか", true),
            ("bで大丈夫なんですか", true),
            ("cで決まりなんでしょうか", true),
            ("aで同じものなんで、お願いします", false),
            ("bで別のものなんで 進めて", false),
            ("aにするとなんでダメなの？", true),
            ("aでもなんで必要なの？", true),
            ("aにしてもなんで必要？", true),
            ("aで払うのなんで必要？", true),
            ("aで使うのなんで？", true),
            ("aで買うのどうして？", true),
            ("なぜaで十分なんですかー？", true),
            ("なぜaで十分なんですか笑", true),
            ("aで進めてもらえる理由は？", true),
            ("aで進めていただける理由は？", true),
            ("aで払うがなんで必要？", false),
            ("なぜaにするほうが安いの？", true),
            ("aにするほうが安い理由は？", true),
            ("aを選ぶほうが良い理由は？", true),
            ("aで多くが安いのはなぜ？", true),
            ("aでひとつが高い理由は？", true),
            ("aで費用はなんで必要？", true),
            ("aで進めたら費用はなんで必要？", true),
            ("aで進めてもなんで必要なの？", true),
            ("aで進めてはなんで必要？", true),
            ("aで月5ドルぐらいが必要な理由は？", true),
            ("aでどれくらいが必要な理由は？", true),
            ("aで少しぐらいが必要な理由は？", true),
            ("aでくらいがそれはなんで必要？", false),
            ("aでわたしがなんで払うの？", true),
            ("aでまんがなんで、お願いします", false),
            ("aでこれがなんで必要？", true),
            ("aでStripeがなんで必要？", true),
            ("aでAPIもなんで要るの？", true),
            ("aで費用を確認して進めてもらえますか？", false),
            ("aで必要な手続きを進めてもらえませんか？", false),
            ("aで進めてはいけない理由は？", true),
            ("aでこれってなんで必要なの？", true),
            ("aで進めてはならない理由は？", true),
            ("aで進めてはだめな理由は？", true),
            ("aでいつもどうして必要？", true),
            ("aで費用の理由を説明してもらえますか？", true),
            ("aで費用の理由を教えてもらえませんか？", true),
            ("aで費用の内訳を見せてもらえる？", true),
            ("aで費用ってなんで必要？", true),
            ("aでAPIってなんで必要？", true),
            ("aでやってなんで必要？", false),
            ("aで走ってなんで必要？", false),
            ("aにしたら費用を確認して進めてもらえますか？", false),
            ("aで進めてはいけない、理由も教えて", false),
            ("なぜaで費用が必要か教えてもらえますか？", true),
            ("どうして止まったか知らないがaで進めては？", false),
            ("aでこれはどうして必要？", true),
            ("aでシステムがなんで必要？", true),
            ("aでプランもどうして必要？", true),
            ("aでこれをなんで使うの？", true),
            (
                "どうして止まったか知らないがaで進めてもらえませんか？",
                false,
            ),
            ("なぜ止まったか知らないがbで進めていただけませんか？", false),
            ("どうして止まったか知らないがaで進めてもらえる？", false),
            ("aで進めてもらえませんか理由も教えて", false),
            ("aにしてもらえませんか", false),
            ("aにしてもらえませんか理由も教えて", false),
            ("どうして止まったか知らないがaにしてもらえますか", false),
            ("aでしろうとなんで、お願いします", false),
            ("aでいつもなんで、お願いします", false),
            ("なぜなら安い", false),
            ("どうしてもaにしたい", false),
            ("どうしても必要", false),
            ("なぜか止まった", false),
            ("進捗を教えてください", false),
            ("進捗を詳しく教えてください", false),
            ("a", false),
            ("bで進めて", false),
            ("必要です", false),
            ("aで。なぜなら安い", false),
            ("aで、なんで？", false),
            ("aで なんで？", false),
            ("aで　なんで？", false),
            ("aで…なんで？", false),
            ("aにするけどなんで必要？", false),
            ("aでお願いします 理由も教えて", false),
            ("aにしたので費用は？", false),
            ("aにしたらいい なんで迷うの？", false),
            ("aにすればいい。なんで迷うの？", false),
            ("aであればいい。理由を教えて", false),
            ("aにすればよい。理由を教えて", false),
            ("なぜaがいいの？ bにするけど費用は？", false),
            ("aで、なぜbがいいの？", false),
            ("なんで迷うの、aにしたらいいでしょ？", false),
            ("なんで止まってるの、aで進めてって言ったよね？", false),
            ("aにするのは確定、なんで聞くの？", false),
            ("aで時間かかってもいいから進めて、理由も教えて", false),
            ("aで決まっているよね？", false),
            ("aにするが費用は？", false),
            ("aで行くけれど費用は？", false),
            ("aでいいよ理由も教えて", false),
            ("bを選ぶ。理由を教えて", false),
            ("今回はaで、なんで？", false),
            ("cで、なんで？", false),
            ("aにする。なんで必要？", false),
            ("dがいい。理由を教えて", false),
            ("aにしたらいい。なんで迷うの？", false),
            ("bを選べばいい。理由を教えて", false),
            ("aにすると決めた。なんで必要？", false),
            ("必要じゃない。止めて", false),
            ("説明は不要、aで", false),
        ]
        .into_iter()
        .chain(
            issue_cases
                .iter()
                .map(|(reply, accepted)| (reply.as_str(), *accepted)),
        ) {
            let f = Fixture::new();
            let mut ctx = f.context();
            let (args, manifest, mut proof) =
                clarification_case(&f, &mut ctx, "内訳を説明します。", false);
            let mut server = f.server.lock().unwrap();
            server
                .messages
                .get_mut(proof["thread_id"].as_str().unwrap())
                .unwrap()
                .iter_mut()
                .find(|v| v["id"] == proof["reply_message_id"])
                .unwrap()["content"] = json!(reply);
            drop(server);
            proof["reply_body_sha256"] = json!(format!("{:x}", Sha256::digest(reply.as_bytes())));
            fs::write(&manifest, proof.to_string()).unwrap();
            let actual = check_clarification(&mut ctx, &args, &manifest).is_ok();
            if actual != accepted {
                mismatches.push((reply, accepted, actual));
            }
        }
        assert!(mismatches.is_empty(), "{mismatches:?}");
    }

    #[test]
    fn first_clarification_lost_response_and_edit_recovers_with_only_get() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) = clarification_case(&f, &mut ctx, "初回の説明です。", false);
        let posts = f.server.lock().unwrap().posts.len();
        {
            let mut server = f.server.lock().unwrap();
            server.drop_response = true;
            server.block_reads_after_post = true;
        }
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        {
            let mut server = f.server.lock().unwrap();
            server.blocked = false;
            server
                .messages
                .get_mut(args.thread.as_ref().unwrap())
                .unwrap()
                .iter_mut()
                .find(|v| v["author"]["id"] == OWNER)
                .unwrap()["content"] = json!("編集済みです。");
        }
        assert_eq!(
            log_clarification(&mut ctx, &args, &manifest).unwrap()["delivery"],
            "verified"
        );
        assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
        f.unchanged();
    }

    #[test]
    fn first_clarification_does_not_reopen_an_explicitly_closed_question() {
        for (phase, outcome, attempted, accepted) in [
            ("reply_pending", None, false, true),
            ("reply_pending", Some("archived"), false, false),
            ("reply_pending", None, true, false),
            ("patch_pending", None, true, false),
            ("patch_pending", None, false, false),
            ("verified", None, false, false),
            ("verified", Some("archived"), false, false),
        ] {
            let f = Fixture::new();
            let mut ctx = f.context();
            let (args, manifest, _) = clarification_case(&f, &mut ctx, "元の説明です。", false);
            f.server
                .lock()
                .unwrap()
                .channels
                .get_mut(args.thread.as_ref().unwrap())
                .unwrap()["thread_metadata"] = json!({"archived":true});
            fs::write(f.home.join(CLOSE_LEDGER),json!({"domain":DOMAIN,"version":1,"records":{args.thread.as_ref().unwrap():{
            "binding":{"request_id":"clarification-question","thread_id":args.thread,"question_message_id":args.thread,
            "origin_todo_id":2472,"todo_id":2472,"message_id":"9999","author_id":OWNER,"job_id":"fixture-job","revision":"a".repeat(40),
            "question_fingerprint":"a".repeat(64),"reply_sha256":"a".repeat(64),"receipt_sha256":"a".repeat(64),"verification_sha256":"a".repeat(64),"completion":"synthetic completion"},
            "phase":phase,"outcome":outcome,"pending_reply_ids":[],"patch_attempted":attempted}}}).to_string()).unwrap();
            let before = fs::read(f.home.join(LEDGER)).unwrap();
            let posts = f.server.lock().unwrap().posts.len();
            let result = log_clarification(&mut ctx, &args, &manifest);
            if accepted {
                assert_eq!(result.unwrap()["delivery"], "verified", "{phase}");
            } else {
                assert!(
                    result.unwrap_err().contains("explicit close record"),
                    "{phase}"
                );
                assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
            }
            assert_eq!(
                f.server.lock().unwrap().posts.len(),
                posts + usize::from(accepted)
            );
            f.unchanged();
        }
    }

    #[test]
    fn first_clarification_accepts_discord_auto_archive_without_a_close_record() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) = clarification_case(&f, &mut ctx, "元の説明です。", false);
        f.server
            .lock()
            .unwrap()
            .channels
            .get_mut(args.thread.as_ref().unwrap())
            .unwrap()["thread_metadata"] = json!({"archived":true});
        let posts = f.server.lock().unwrap().posts.len();
        assert_eq!(
            log_clarification(&mut ctx, &args, &manifest).unwrap()["delivery"],
            "verified"
        );
        assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
        assert!(!f.home.join(CLOSE_LEDGER).exists());
        f.unchanged();
    }

    #[test]
    fn first_clarification_partial_lost_response_recovers_before_rejecting_edited_remainder() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) =
            clarification_case(&f, &mut ctx, &"元の説明です。".repeat(500), false);
        assert!(parts(&fs::read_to_string(&args.body_file).unwrap()).len() > 1);
        let posts = f.server.lock().unwrap().posts.len();
        {
            let mut server = f.server.lock().unwrap();
            server.drop_response = true;
            server.block_reads_after_post = true;
        }
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        assert!(Ledger::load(&f.home)
            .unwrap()
            .receipt(&args.request_id, "log:0")
            .is_none());
        {
            let mut server = f.server.lock().unwrap();
            server.blocked = false;
            server
                .messages
                .get_mut(args.thread.as_ref().unwrap())
                .unwrap()
                .iter_mut()
                .find(|v| v["author"]["id"] == OWNER)
                .unwrap()["edited_timestamp"] = json!("2026-10-12T00:00:03Z");
        }
        assert_eq!(
            check_clarification(&mut ctx, &args, &manifest).unwrap()["delivery"],
            "not_sent"
        );
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        let ledger = Ledger::load(&f.home).unwrap();
        assert!(
            ledger.receipt(&args.request_id, "log:0").is_some(),
            "sent first part was not recovered"
        );
        assert!(ledger.effect(&args.request_id, "log:1").is_none());
        assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
        f.unchanged();
    }

    #[test]
    fn first_clarification_parallel_send_uses_the_existing_byte_lock() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) = clarification_case(&f, &mut ctx, "初回の説明です。", false);
        let home = f.home.clone();
        let root = f.root.path().to_owned();
        let server = f.server.clone();
        let worker_args = LogArgs {
            request_id: args.request_id.clone(),
            title: args.title.clone(),
            body_file: args.body_file.clone(),
            thread: args.thread.clone(),
            notify: false,
            attach: vec![],
        };
        let worker_manifest = manifest.clone();
        let (entered_tx, entered_rx) = mpsc::channel();
        let (release_tx, release_rx) = mpsc::channel();
        let worker = std::thread::spawn(move || {
            let mut ctx = context(&home, &root, server, Some((entered_tx, release_rx)));
            log_clarification(&mut ctx, &worker_args, &worker_manifest)
        });
        let entered = entered_rx.recv_timeout(Duration::from_secs(30));
        let collision = log_clarification(&mut ctx, &args, &manifest);
        let _ = release_tx.send(());
        let first = worker.join().unwrap();
        assert!(entered.is_ok());
        assert!(collision.unwrap_err().contains("busy"));
        assert_eq!(
            first.unwrap()["message_id"],
            log_clarification(&mut ctx, &args, &manifest).unwrap()["message_id"]
        );
        f.unchanged();
    }

    struct EditClarificationReply {
        inner: Box<dyn Effects>,
        server: Arc<Mutex<Server>>,
        thread: String,
        reply: String,
    }
    impl Effects for EditClarificationReply {
        fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
            self.inner.run(args, timeout)
        }
        fn http(
            &mut self,
            method: &str,
            url: &str,
            body: Option<&Value>,
            discord: bool,
        ) -> Result<Value> {
            let result = self.inner.http(method, url, body, discord);
            if method == "POST" {
                self.server
                    .lock()
                    .unwrap()
                    .messages
                    .get_mut(&self.thread)
                    .unwrap()
                    .iter_mut()
                    .find(|v| v["id"] == self.reply)
                    .unwrap()["edited_timestamp"] = json!("2026-10-12T00:00:03Z");
            }
            result
        }
    }

    #[test]
    fn first_clarification_rechecks_owner_before_reserving_each_remaining_part() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, proof) =
            clarification_case(&f, &mut ctx, &"説明の背景。".repeat(500), false);
        assert!(parts(&fs::read_to_string(&args.body_file).unwrap()).len() > 1);
        let posts = f.server.lock().unwrap().posts.len();
        let inner = std::mem::replace(
            &mut ctx.effects,
            Box::new(Mock {
                server: f.server.clone(),
                home: f.home.clone(),
                pause: None,
            }),
        );
        ctx.effects = Box::new(EditClarificationReply {
            inner,
            server: f.server.clone(),
            thread: proof["thread_id"].as_str().unwrap().into(),
            reply: proof["reply_message_id"].as_str().unwrap().into(),
        });
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        let ledger = Ledger::load(&f.home).unwrap();
        assert!(ledger.effect(&args.request_id, "log:0").is_some());
        assert!(ledger.effect(&args.request_id, "log:1").is_none());
        assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
        f.unchanged();
    }

    #[test]
    fn clarification_resumes_original_log_once_and_never_grants_progress() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) = clarification_fixture(&f, &mut ctx);
        let before = f.server.lock().unwrap().posts.len();
        let original = Ledger::load(&f.home)
            .unwrap()
            .request(&args.request_id)
            .unwrap()
            .clone();
        let first = log_clarification(&mut ctx, &args, &manifest).unwrap();
        let again = log_clarification(&mut ctx, &args, &manifest).unwrap();
        assert_eq!(first["message_id"], again["message_id"]);
        assert_eq!(f.server.lock().unwrap().posts.len(), before + 1);
        assert_eq!(
            *Ledger::load(&f.home)
                .unwrap()
                .request(&args.request_id)
                .unwrap(),
            original
        );
        let mut progress = f.log("ordinary-progress", "作業が進みました。", false);
        progress.thread = args.thread.clone();
        assert!(log(&mut ctx, &progress).is_err());
        assert_eq!(f.server.lock().unwrap().posts.len(), before + 1);
        f.unchanged();
    }

    #[test]
    fn clarification_check_uses_get_without_changing_intents_or_sending() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) = clarification_fixture(&f, &mut ctx);
        let before = fs::read(f.home.join(LEDGER)).unwrap();
        let posts = f.server.lock().unwrap().posts.len();
        let checked = check_clarification(&mut ctx, &args, &manifest).unwrap();
        assert_eq!(checked["check_only"], true);
        assert_eq!(checked["delivery"], "not_sent");
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
        assert!(!f.home.join("recovery-clarifications.json").exists());
        assert_eq!(f.server.lock().unwrap().posts.len(), posts);
        f.unchanged();
    }

    #[test]
    fn clarification_rejects_different_scope_and_changed_api_evidence() {
        for field in [
            "purpose",
            "request_id",
            "question_request_id",
            "todo_id",
            "thread_id",
            "question_message_id",
            "question_fingerprint",
            "question_body_sha256",
            "bot_id",
            "owner_id",
            "reply_message_id",
            "reply_body_sha256",
            "reply_timestamp",
            "reply_edited_timestamp",
            "body_sha256",
            "unknown_field",
            "api-question",
            "api-reply",
            "api-author",
        ] {
            let f = Fixture::new();
            let mut ctx = f.context();
            let (args, manifest, mut binding) = clarification_fixture(&f, &mut ctx);
            let before = f.server.lock().unwrap().posts.len();
            match field {
                "api-question" | "api-reply" | "api-author" => {
                    let mut server = f.server.lock().unwrap();
                    let rows = server
                        .messages
                        .get_mut(args.thread.as_ref().unwrap())
                        .unwrap();
                    let message = if field == "api-question" {
                        &mut rows[0]
                    } else {
                        rows.last_mut().unwrap()
                    };
                    if field == "api-author" {
                        message["author"]["id"] = json!("999");
                    } else {
                        message["content"] = json!("changed original");
                    }
                }
                "todo_id" => binding[field] = json!(999),
                "reply_timestamp" | "reply_edited_timestamp" => {
                    binding[field] = json!("2026-10-12T00:00:02Z")
                }
                key if key.ends_with("sha256") || key.ends_with("fingerprint") => {
                    binding[field] = json!("f".repeat(64))
                }
                _ => binding[field] = json!("999"),
            }
            fs::write(&manifest, serde_json::to_vec(&binding).unwrap()).unwrap();
            assert!(
                log_clarification(&mut ctx, &args, &manifest).is_err(),
                "{field}"
            );
            assert_eq!(f.server.lock().unwrap().posts.len(), before, "{field}");
            assert!(
                !f.home.join("recovery-clarifications.json").exists(),
                "{field}"
            );
            f.unchanged();
        }
    }

    #[test]
    fn clarification_rejects_notify_mentions_attachments_and_requestion() {
        for body in [
            "この機能を継続しますか？",
            "a. 停止\nb. 継続",
            "どちらかを選んでください。",
            "ご回答ください。",
            "どちらにしますか。",
            "(a) 停止\n(b) 継続",
            "1. 停止\n2. 継続",
            "選択をお願いします。",
        ] {
            let f = Fixture::new();
            let mut ctx = f.context();
            // Bind the original intent to this very body, so a fingerprint
            // mismatch cannot accidentally make the rejection test pass.
            let (args, manifest, _) = clarification_fixture_body(&f, &mut ctx, body);
            let before = f.server.lock().unwrap().calls.len();
            assert!(
                log_clarification(&mut ctx, &args, &manifest)
                    .unwrap_err()
                    .contains("cannot contain another question"),
                "{body}"
            );
            assert_eq!(f.server.lock().unwrap().calls.len(), before);
        }
        let f = Fixture::new();
        let mut ctx = f.context();
        let (mut args, manifest, _) = clarification_fixture(&f, &mut ctx);
        fs::write(&args.body_file, "<@100> 説明です。").unwrap();
        assert!(log_clarification(&mut ctx, &args, &manifest)
            .unwrap_err()
            .contains("mentions"));
        fs::write(&args.body_file, "これは元の質問の背景を補う説明です。").unwrap();
        args.notify = true;
        let before = f.server.lock().unwrap().calls.len();
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        args.notify = false;
        args.attach.push(f.root.path().join("missing.png"));
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        assert_eq!(f.server.lock().unwrap().calls.len(), before);
        f.unchanged();
    }

    #[test]
    fn clarification_lost_post_response_recovers_without_another_post() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, _) = clarification_fixture(&f, &mut ctx);
        let before = f.server.lock().unwrap().posts.len();
        {
            let mut server = f.server.lock().unwrap();
            server.drop_response = true;
            server.block_reads_after_post = true;
        }
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        assert_eq!(f.server.lock().unwrap().posts.len(), before + 1);
        f.server.lock().unwrap().blocked = false;
        // The authorized POST is already durable. An owner edit cannot turn
        // its GET-only recovery into a new send or permanently hide its receipt.
        f.server
            .lock()
            .unwrap()
            .messages
            .get_mut(args.thread.as_ref().unwrap())
            .unwrap()
            .iter_mut()
            .find(|row| row["author"]["id"] == OWNER)
            .unwrap()["content"] = json!("後から編集した返信です。");
        assert_eq!(
            log_clarification(&mut ctx, &args, &manifest).unwrap()["delivery"],
            "verified"
        );
        assert_eq!(f.server.lock().unwrap().posts.len(), before + 1);
        f.unchanged();
    }

    #[test]
    fn clarification_invalid_binding_can_be_corrected_before_authorization() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, valid) = clarification_fixture(&f, &mut ctx);
        let mut invalid = valid.clone();
        invalid["reply_timestamp"] = json!("2026-10-12T09:00:01+09:00");
        fs::write(&manifest, serde_json::to_vec(&invalid).unwrap()).unwrap();
        assert!(log_clarification(&mut ctx, &args, &manifest).is_err());
        assert!(!f.home.join("recovery-clarifications.json").exists());
        let before = f.server.lock().unwrap().posts.len();
        fs::write(&manifest, serde_json::to_vec(&valid).unwrap()).unwrap();
        log_clarification(&mut ctx, &args, &manifest).unwrap();
        assert_eq!(f.server.lock().unwrap().posts.len(), before + 1);
        f.unchanged();
    }

    #[test]
    fn clarification_reader_exposes_edit_version_without_changing_reply_hash_shape() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (_, _, binding) = clarification_fixture(&f, &mut ctx);
        let thread = binding["thread_id"].as_str().unwrap();
        let reply = binding["reply_message_id"].as_str().unwrap();
        f.server
            .lock()
            .unwrap()
            .messages
            .get_mut(thread)
            .unwrap()
            .iter_mut()
            .find(|row| row["id"] == reply)
            .unwrap()["edited_timestamp"] = json!("2026-10-12T00:00:02Z");
        let result = replies(
            &mut ctx,
            &RepliesArgs {
                thread: thread.into(),
                message: Some(thread.into()),
                request_id: Some("clarification-question".into()),
                todo_id: Some(2472),
                read_only: true,
            },
        )
        .unwrap();
        assert_eq!(
            result["reply_versions"][reply]["edited_timestamp"],
            "2026-10-12T00:00:02Z"
        );
        assert!(result["owner_replies"][0].get("edited_timestamp").is_none());
        f.unchanged();
    }

    #[test]
    fn clarification_cannot_rebind_same_owner_request_to_new_id_or_body() {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, manifest, mut binding) = clarification_fixture(&f, &mut ctx);
        log_clarification(&mut ctx, &args, &manifest).unwrap();
        let before = f.server.lock().unwrap().posts.len();
        let mut copy = f.log("different-log", "変更した別の説明です。", false);
        copy.thread = args.thread;
        assert!(log(&mut ctx, &copy).is_err());
        binding["request_id"] = json!(copy.request_id);
        binding["body_sha256"] = json!(format!(
            "{:x}",
            Sha256::digest(fs::read(&copy.body_file).unwrap())
        ));
        let another = f.root.path().join("another-binding.json");
        fs::write(&another, serde_json::to_vec(&binding).unwrap()).unwrap();
        assert!(log_clarification(&mut ctx, &copy, &another).is_err());
        assert_eq!(f.server.lock().unwrap().posts.len(), before);
        f.unchanged();
    }

    struct MediaVerifierMock {
        inner: Mock,
        output: Output,
        calls: Arc<Mutex<Vec<Vec<String>>>>,
    }

    impl Effects for MediaVerifierMock {
        fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
            assert_eq!(timeout, 20);
            assert_eq!(
                args.first().map(String::as_str),
                Some("D:/CC/Tools/discord-ask/unattended-runner.exe")
            );
            assert_eq!(
                args.get(1).map(String::as_str),
                Some("--verify-conversation-media")
            );
            self.calls.lock().unwrap().push(args.to_vec());
            Ok(self.output.clone())
        }
        fn http(
            &mut self,
            method: &str,
            url: &str,
            body: Option<&Value>,
            discord: bool,
        ) -> Result<Value> {
            self.inner.http(method, url, body, discord)
        }
        fn multipart(&mut self, url: &str, content_type: &str, body: &[u8]) -> Result<Value> {
            self.inner.multipart(url, content_type, body)
        }
    }

    fn media_mock(
        fixture: &Fixture,
        ctx: &mut Context,
        proof: Value,
        code: i32,
    ) -> Arc<Mutex<Vec<Vec<String>>>> {
        let calls = Arc::new(Mutex::new(Vec::new()));
        ctx.effects = Box::new(MediaVerifierMock {
            inner: Mock {
                server: fixture.server.clone(),
                home: fixture.home.clone(),
                pause: None,
            },
            output: Output {
                code,
                stdout: proof.to_string(),
                stderr: "private verifier diagnostic".into(),
            },
            calls: calls.clone(),
        });
        calls
    }

    fn media_proof(bytes: &[u8]) -> Value {
        json!({"domain":"discord-conversation-media-v1","verified":true,"sha256":format!("{:x}",Sha256::digest(bytes)),"size":bytes.len(),"mime":"image/png","extension":"png"})
    }

    struct ProgramVerifierMock {
        inner: Mock,
        output: Value,
    }

    impl Effects for ProgramVerifierMock {
        fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
            assert_eq!(timeout, 25);
            assert_eq!(args[1], "--check-program-question-evidence");
            assert_eq!(args[2], "--evidence");
            assert_eq!(args.len(), 4);
            Ok(Output {
                code: 0,
                stdout: self.output.to_string(),
                stderr: String::new(),
            })
        }
        fn http(
            &mut self,
            method: &str,
            url: &str,
            body: Option<&Value>,
            discord: bool,
        ) -> Result<Value> {
            self.inner.http(method, url, body, discord)
        }
        fn multipart(&mut self, url: &str, content_type: &str, body: &[u8]) -> Result<Value> {
            self.inner.multipart(url, content_type, body)
        }
    }

    #[test]
    fn recovery_messages_program_drawing_binds_every_upload_and_question_field() {
        for change in [
            "none",
            "domain",
            "validated",
            "send_allowed",
            "request_id",
            "origin_thread_id",
            "todo_id",
            "question_sha256",
            "decision_sha256",
            "owner_id",
            "guild_id",
            "forum_id",
            "path",
            "sha",
            "size",
            "order",
            "count",
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            fs::remove_file(&ctx.client_projects).unwrap();
            let mut args = fixture.ask(
                "program-original-question",
                "画像案のaまたはbを選んでください。",
            );
            args.program_evidence = Some(fixture.root.path().join("evidence.json"));
            let incoming = prepare_ask(&ctx, &args).unwrap();
            let mut bound = Vec::new();
            // CI's TEMP can be an 8.3 alias. Qualify the physical original
            // spelling rather than relaxing the sender's alias checks.
            let image_root = fs::canonicalize(fixture.root.path()).unwrap();
            for i in 0..2 {
                let bytes = [
                    b"\x89PNG\r\n\x1a\n".as_slice(),
                    format!("new-drawing-{i}").as_bytes(),
                ]
                .concat();
                let path = image_root.join(format!("drawing-{i}.png"));
                fs::write(&path, &bytes).unwrap();
                let sha = format!("{:x}", Sha256::digest(&bytes));
                args.attach.push(path.clone());
                args.expected_image_sha256.push(sha.clone());
                bound.push(json!({"path":path,"sha256":sha,"size":bytes.len()}));
            }
            let mut proof = json!({"domain":"owner-program-question-image-v1","validated":true,"send_allowed":true,
                "request_id":args.request_id,"origin_thread_id":ORIGIN,"scope":question_image_scope(&ctx,&incoming).unwrap(),"images":bound});
            match change {
                "none" => {}
                "validated" | "send_allowed" => proof[change] = json!(false),
                "domain" | "request_id" | "origin_thread_id" => proof[change] = json!("different"),
                "todo_id" => proof["scope"][change] = json!(999),
                "path" => proof["images"][0]["path"] = json!(fixture.root.path().join("copy.png")),
                "sha" => proof["images"][0]["sha256"] = json!("a".repeat(64)),
                "size" => proof["images"][0]["size"] = json!(1),
                "order" => proof["images"].as_array_mut().unwrap().reverse(),
                "count" => {
                    proof["images"].as_array_mut().unwrap().pop();
                }
                _ => proof["scope"][change] = json!("different"),
            }
            ctx.effects = Box::new(ProgramVerifierMock {
                inner: Mock {
                    server: fixture.server.clone(),
                    home: fixture.home.clone(),
                    pause: None,
                },
                output: proof,
            });
            let result = ask(&mut ctx, &args);
            if change == "none" {
                assert_eq!(result.unwrap()["delivery"], "verified");
                assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
                // A repeated exact request reads back the saved attachment receipt.
                assert_eq!(ask(&mut ctx, &args).unwrap()["delivery"], "verified");
                assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
            } else {
                assert!(result.is_err(), "{change}");
                assert!(fixture.server.lock().unwrap().calls.is_empty(), "{change}");
            }
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_messages_image_result_recovers_after_original_disappears() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let image = fixture.root.path().join("original.png");
        let bytes = b"\x89PNG\r\n\x1a\noriginal-image";
        fs::write(&image, bytes).unwrap();
        let mut args = fixture.ask(
            "recover-image-result",
            "画面案を選んでください。a案またはb案。",
        );
        args.attach.push(image.clone());
        args.expected_image_sha256
            .push(format!("{:x}", Sha256::digest(bytes)));
        {
            let mut state = fixture.server.lock().unwrap();
            state.drop_response = true;
            state.block_reads_after_post = true;
        }
        assert!(ask(&mut ctx, &args).is_err());
        assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
        fs::remove_file(image).unwrap();
        {
            let mut state = fixture.server.lock().unwrap();
            state.block_reads_after_post = false;
            state.blocked = false;
        }
        let result = ask(&mut ctx, &args).unwrap();
        assert_eq!(result["delivery"], "verified");
        assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_changed_image_is_rejected_before_first_upload() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let image = fixture.root.path().join("original.png");
        fs::write(&image, b"\x89PNG\r\n\x1a\nchanged-image").unwrap();
        let mut args = fixture.ask(
            "expected-image-before-upload",
            "画面案を選んでください。a案またはb案。",
        );
        args.attach.push(image);
        args.expected_image_sha256.push(format!(
            "{:x}",
            Sha256::digest(b"\x89PNG\r\n\x1a\noriginal-image")
        ));
        assert!(ask(&mut ctx, &args).is_err());
        assert!(fixture.server.lock().unwrap().calls.is_empty());
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_question_is_complete_even_when_progress_creation_fails() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let args = fixture.ask(
            "question-before-progress",
            &format!("背景{}\nb 最後の選択肢", "説明".repeat(1500)),
        );
        fixture.server.lock().unwrap().reject_progress = true;
        assert!(ask(&mut ctx, &args).is_err());
        let ledger = Ledger::load(&fixture.home).unwrap();
        let receipt = ledger.receipt(&args.request_id, "question:0").unwrap();
        let state = fixture.server.lock().unwrap();
        assert!(state.messages[&receipt.thread_id]
            .iter()
            .any(|message| core::s(&message["content"]).contains("b 最後の選択肢")));
        drop(state);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_generated_qualification_matches_the_actual_question_context() {
        for mismatch in [
            "none",
            "todo_id",
            "question_sha256",
            "decision_sha256",
            "owner_id",
            "guild_id",
            "forum_id",
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            fs::remove_file(&ctx.client_projects).unwrap();
            let bytes = b"\x89PNG\r\n\x1a\nnew-design";
            let image = fixture.root.path().join("new-design.png");
            fs::write(&image, bytes).unwrap();
            let mut args = fixture.ask(
                &format!("conversation-v1-{}", "d".repeat(32)),
                "画面案を選んでください。aまたはb。",
            );
            args.attach.push(image);
            let incoming = prepare_ask(&ctx, &args).unwrap();
            let mut scope =
                serde_json::to_value(question_image_scope(&ctx, &incoming).unwrap()).unwrap();
            if mismatch == "todo_id" {
                scope[mismatch] = json!(3085)
            } else if mismatch != "none" {
                scope[mismatch] = json!("different")
            }
            let mut proof = media_proof(bytes);
            proof["question_qualification"] = json!({"domain":"owner-generated-question-image-v1","qualified":true,"scope":scope,"sha256":proof["sha256"],"size":bytes.len(),"mime":"image/png","extension":"png"});
            media_mock(&fixture, &mut ctx, proof, 0);
            let result = ask(&mut ctx, &args);
            if mismatch == "none" {
                assert_eq!(result.unwrap()["delivery"], "verified")
            } else {
                assert!(result.is_err());
                assert!(fixture.server.lock().unwrap().calls.is_empty())
            };
            fixture.unchanged();
        }
    }

    #[test]
    fn different_question_images_are_terminal_even_for_reserved_or_semantic_duplicates() {
        for posted in [false, true] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let original = fixture.ask(
                &format!("conversation-v1-{}", "a".repeat(32)),
                "a または b を選んでください。",
            );
            if posted {
                ask(&mut ctx, &original).unwrap();
            } else {
                ensure_storage(&fixture.home).unwrap();
                let incoming = prepare_ask(&ctx, &original).unwrap();
                reserve(
                    &mut Ledger::load(&fixture.home).unwrap(),
                    &fixture.home,
                    &incoming,
                )
                .unwrap();
            }
            let original_posts = fixture.server.lock().unwrap().posts.len();
            let before = Ledger::load(&fixture.home).unwrap().events.len();
            let mut next = fixture.ask(
                &format!("conversation-v1-{}", "b".repeat(32)),
                "同じ判断で別画像です。",
            );
            let mut decision: Value =
                serde_json::from_str(&fs::read_to_string(&next.decision_file).unwrap()).unwrap();
            decision["presentation_note"] = json!("ignored by normalized decision identity");
            fs::write(&next.decision_file, decision.to_string()).unwrap();
            fs::remove_file(&ctx.client_projects).unwrap();
            let bytes = b"\x89PNG\r\n\x1a\nnew-image";
            let image = fixture.root.path().join("new-image.png");
            fs::write(&image, bytes).unwrap();
            media_mock(&fixture, &mut ctx, media_proof(bytes), 0);
            next.attach.push(image);
            let result = ask(&mut ctx, &next).unwrap();
            assert_eq!(result["outcome"], "existing_question_images_differ");
            assert_eq!(result["delivery"], "not_sent");
            assert_eq!(result["existing_request_id"], original.request_id);
            assert_eq!(result["url"].as_str().unwrap().is_empty(), !posted);
            let ledger = Ledger::load(&fixture.home).unwrap();
            assert!(ledger.request(&next.request_id).is_none());
            assert_eq!(ledger.events.len(), before);
            assert_eq!(fixture.server.lock().unwrap().posts.len(), original_posts);
            fixture.unchanged();
        }
    }

    #[test]
    fn reserved_question_rejects_different_expected_sha_before_http() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let mut args = fixture.ask("expected-sha-conflict", "a または b を選んでください。");
        ask(&mut ctx, &args).unwrap();
        let before = fixture.server.lock().unwrap().calls.len();
        args.expected_image_sha256 = vec!["a".repeat(64)];
        assert!(ask(&mut ctx, &args)
            .unwrap_err()
            .contains("saved question image SHA differs"));
        assert_eq!(fixture.server.lock().unwrap().calls.len(), before);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_question_image_and_choices_stay_inside_question() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        fs::remove_file(&ctx.client_projects).unwrap();
        let bytes = b"\x89PNG\r\n\x1a\nquestion-image";
        let image = fixture.root.path().join("question.png");
        fs::write(&image, bytes).unwrap();
        let calls = media_mock(&fixture, &mut ctx, media_proof(bytes), 0);
        let mut args = fixture.ask(
            &format!("conversation-v1-{}", "b".repeat(32)),
            &format!(
                "画面案を選んでください。{}\na 上段に名前、下段に件数\nb 正常版を保ち別案",
                "背景".repeat(1400)
            ),
        );
        args.attach.push(image);
        let result = ask(&mut ctx, &args).unwrap();
        assert_eq!(result["delivery"], "verified");
        let state = fixture.server.lock().unwrap();
        let question = &state.messages[core::s(&result["thread_id"])];
        assert_eq!(question[0]["attachments"].as_array().unwrap().len(), 1);
        assert!(question
            .iter()
            .any(|message| core::s(&message["content"]).contains("b 正常版")));
        let progress = &state.messages[core::s(&result["progress_thread_id"])];
        assert!(progress
            .iter()
            .all(|message| !core::s(&message["content"]).contains("b 正常版")));
        assert_eq!(state.multipart_uploads, 1);
        drop(state);
        assert_eq!(calls.lock().unwrap().len(), 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_conversation_media_qualified_without_legacy_registry_is_get_verified() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        fs::remove_file(&ctx.client_projects).unwrap();
        let bytes = b"\x89PNG\r\n\x1a\nfixture-image";
        let image = fixture.root.path().join("current.png");
        fs::write(&image, bytes).unwrap();
        let calls = media_mock(&fixture, &mut ctx, media_proof(bytes), 0);
        let mut args = fixture.log(
            &format!("conversation-log-{}", "a".repeat(32)),
            "作業が完了しました。",
            false,
        );
        args.attach.push(image.clone());
        let result = log(&mut ctx, &args).unwrap();
        assert_eq!(result["delivery"], "verified");
        assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
        let captured = calls.lock().unwrap();
        assert_eq!(captured.len(), 1);
        assert_eq!(Path::new(&captured[0][3]), image);
        assert_eq!(captured[0][5], args.request_id);
        assert!(!result.to_string().contains(image.to_str().unwrap()));
        drop(captured);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_conversation_media_failed_or_changed_proof_never_posts() {
        for field in [
            "verified",
            "sha256",
            "size",
            "mime",
            "extension",
            "domain",
            "exit_code",
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            fs::remove_file(&ctx.client_projects).unwrap();
            let bytes = b"\x89PNG\r\n\x1a\nfixture-image";
            let image = fixture.root.path().join("current.png");
            fs::write(&image, bytes).unwrap();
            let mut proof = media_proof(bytes);
            match field {
                "verified" => proof[field] = json!(false),
                "size" => proof[field] = json!(bytes.len() + 1),
                "exit_code" => {}
                _ => proof[field] = json!("private-verifier-detail"),
            }
            let calls = media_mock(
                &fixture,
                &mut ctx,
                proof,
                if field == "exit_code" { 1 } else { 0 },
            );
            let mut args = fixture.log(
                &format!("conversation-log-{}", "a".repeat(32)),
                "作業が完了しました。",
                false,
            );
            args.attach.push(image);
            let error = log(&mut ctx, &args).unwrap_err();
            assert!(!error.contains("private-verifier-detail"));
            assert_eq!(calls.lock().unwrap().len(), 1);
            assert!(fixture.server.lock().unwrap().calls.is_empty());
            assert!(!fixture.home.join(LEDGER).exists());
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_messages_conversation_media_preserves_existing_exclusions_and_legacy_denial() {
        for legacy in [false, true] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let bytes = b"\x89PNG\r\n\x1a\nfixture-image";
            let image = fixture.root.path().join("current.png");
            fs::write(&image, bytes).unwrap();
            if legacy {
                fs::remove_file(&ctx.client_projects).unwrap();
            } else {
                fs::write(&ctx.client_projects, "current.png\n").unwrap();
            }
            let calls = media_mock(&fixture, &mut ctx, media_proof(bytes), 0);
            let id = if legacy {
                "legacy-image".into()
            } else {
                format!("conversation-log-{}", "a".repeat(32))
            };
            let mut args = fixture.log(&id, "作業が完了しました。", false);
            args.attach.push(image);
            assert!(log(&mut ctx, &args).is_err());
            assert!(calls.lock().unwrap().is_empty());
            assert!(fixture.server.lock().unwrap().calls.is_empty());
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_messages_conversation_media_cannot_skip_secret_byte_checks_or_old_namespace() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        fs::remove_file(&ctx.client_projects).unwrap();
        let bytes = b"\x89PNG\r\n\x1a\npassword=synthetic-private-value";
        let image = fixture.root.path().join("current.png");
        fs::write(&image, bytes).unwrap();
        let calls = media_mock(&fixture, &mut ctx, media_proof(bytes), 0);
        let mut args = fixture.log(
            &format!("conversation-log-{}", "a".repeat(32)),
            "作業が完了しました。",
            false,
        );
        args.attach.push(image);
        assert!(log(&mut ctx, &args).is_err());
        assert!(calls.lock().unwrap().is_empty());
        assert!(fixture.server.lock().unwrap().calls.is_empty());
        fixture.unchanged();
        for id in [
            "conversation-log-old",
            "conversation-log-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
            "conversation-log-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-extra",
        ] {
            assert!(!conversation_media_request(id));
        }
    }

    #[test]
    fn recovery_messages_notify_prefix_owner_and_silent_defaults_are_get_verified() {
        let fixture = Fixture::new();
        let args = fixture.log(
            "status-2472",
            "回答が必要：公開範囲をどちらにしますか？\na: この範囲を公開する\nb: 保持する",
            true,
        );
        let mut ctx = fixture.context();
        let out = log(&mut ctx, &args).unwrap();
        assert_eq!(out["delivery"], "verified");
        assert_eq!(out["new_pending_effects"], 0);
        assert_eq!(out["legacy_pending"], "unknown");
        let state = fixture.server.lock().unwrap();
        let messages = &state.messages[core::s(&out["thread_id"])];
        assert!(core::s(&messages[0]["content"]).starts_with("回答が必要：公開範囲"));
        assert_eq!(
            core::s(&messages[0]["content"])
                .matches(&format!("<@{OWNER}>"))
                .count(),
            1
        );
        assert_eq!(messages[0]["mentions"], json!([{"id":OWNER}]));
        assert_eq!(core::n(&messages[0]["flags"]) & SILENT, 0);
        assert!(state.calls.iter().any(
            |(method, path, _)| method == "GET" && path.ends_with(core::s(&out["message_id"]))
        ));
        drop(state);
        let silent = fixture.log("status-silent", "進捗を記録しました。", false);
        let out = log(&mut ctx, &silent).unwrap();
        let state = fixture.server.lock().unwrap();
        let message = &state.messages[core::s(&out["thread_id"])][0];
        assert_eq!(message["mentions"], json!([]));
        assert_eq!(
            core::n(&message["flags"]) & (SILENT | EMBEDS),
            SILENT | EMBEDS
        );
        drop(state);
        // The normal context remains blocked after the new isolated send.
        let calls = fixture.server.lock().unwrap().calls.len();
        assert!(ctx
            .call(
                "POST",
                "/channels/106/messages",
                Some(&json!({"content":"legacy"}))
            )
            .is_err());
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_reports_ignore_notify_without_losing_delivery() {
        for body in [
            "対応不要です。AI側で再開します。",
            "報告のみです。修正を終えました。",
            "本人対応不要。先行作業を待っています。",
            "通常の進捗を記録しました。",
            "調査の経過です。\n原因は何でしょう？AI側で修正を続けます。",
        ] {
            for post in [false, true] {
                let fixture = Fixture::new();
                let mut ctx = fixture.context();
                let thread = if post {
                    let first =
                        log(&mut ctx, &fixture.log("seed", "開始しました。", false)).unwrap();
                    Some(core::s(&first["thread_id"]).to_string())
                } else {
                    None
                };
                let mut args = fixture.log("quiet-notify", body, true);
                args.thread = thread.clone();
                let out = log(&mut ctx, &args).unwrap();
                assert_eq!(out["delivery"], "verified");
                assert_eq!(out["requested_notify"], true);
                assert_eq!(out["effective_notify"], false);
                let state = fixture.server.lock().unwrap();
                let (_, posted) = state.posts.last().unwrap();
                let payload = if post { posted } else { &posted["message"] };
                assert_eq!(
                    payload["allowed_mentions"],
                    json!({"parse":[],"users":[],"roles":[],"replied_user":false})
                );
                assert_eq!(
                    core::n(&payload["flags"]) & (SILENT | EMBEDS),
                    SILENT | EMBEDS
                );
                assert!(!core::s(&payload["content"]).contains("<@"));
                let message = state.messages[core::s(&out["thread_id"])].last().unwrap();
                assert_eq!(message["mentions"], json!([]));
                assert_eq!(message["channel_id"], out["thread_id"]);
                if let Some(thread) = thread {
                    assert_eq!(out["thread_id"], thread);
                }
                let count = state.posts.len();
                drop(state);
                assert_eq!(
                    log(&mut ctx, &args).unwrap()["message_id"],
                    out["message_id"]
                );
                assert_eq!(fixture.server.lock().unwrap().posts.len(), count);
                fixture.unchanged();
            }
        }
    }

    #[test]
    fn recovery_messages_go_question_format_keeps_no_action_words_inside_choices() {
        for (heading, a, b) in [
            ("回答が必要：", " a:", " b:"),
            ("回答が必要:", " a：", " b："),
        ] {
            let fixture = Fixture::new();
            let body=format!("{heading}この対象の扱いを選んでください。\n{a} 調査を続ける\n{b} 対応不要として保持する\n回答先: <https://discord.com/channels/102/200/201>\nこの対象だけ回答を待ち、AI側はほかの作業を続けます。\n\n状態: 継続中\n本人の判断: 必要");
            let args = fixture.log("go-owner-question", &body, true);
            let mut ctx = fixture.context();
            let out = log(&mut ctx, &args).unwrap();
            assert_eq!(out["effective_notify"], true);
            let state = fixture.server.lock().unwrap();
            let payload = &state.posts[0].1["message"];
            assert_eq!(payload["allowed_mentions"]["users"], json!([OWNER]));
            assert_eq!(payload["allowed_mentions"]["replied_user"], false);
            assert_eq!(core::n(&payload["flags"]) & SILENT, 0);
            drop(state);
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_messages_malformed_owner_action_is_rejected_before_post() {
        for body in [
            "回答が必要：\na: \nb: ",
            "回答が必要：選んでください。\na: 続行\nb: 続行",
            "回答が必要 選んでください。\na: 続行\nb: 保持",
        ] {
            let fixture = Fixture::new();
            let args = fixture.log("malformed-owner-action", body, true);
            let mut ctx = fixture.context();
            assert!(log(&mut ctx, &args)
                .unwrap_err()
                .contains("owner action heading"));
            assert!(fixture.server.lock().unwrap().calls.is_empty());
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_messages_old_unsent_intent_is_delivered_quietly_without_rewriting_input() {
        let fixture = Fixture::new();
        let args = fixture.log("old-unsent", "対応不要。依存先の完了後に再開します。", true);
        let mut ctx = fixture.context();
        let (incoming, _) = prepare_log(&mut ctx, &args).unwrap();
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        reserve(&mut ledger, &fixture.home, &incoming).unwrap();
        let old = serde_json::to_value(&ledger.events).unwrap();
        let out = log(&mut ctx, &args).unwrap();
        assert_eq!(out["delivery"], "verified");
        assert_eq!(out["requested_notify"], true);
        assert_eq!(out["effective_notify"], false);
        let saved = Ledger::load(&fixture.home).unwrap();
        assert_eq!(saved.request(&args.request_id), Some(&incoming));
        assert_eq!(
            serde_json::to_value(&saved.events[..old.as_array().unwrap().len()]).unwrap(),
            old
        );
        let state = fixture.server.lock().unwrap();
        let payload = &state.posts[0].1["message"];
        assert_eq!(payload["allowed_mentions"]["users"], json!([]));
        assert_eq!(payload["allowed_mentions"]["replied_user"], false);
        assert_eq!(core::n(&payload["flags"]) & SILENT, SILENT);
        drop(state);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_old_sent_effect_is_only_read_back_and_never_reposted() {
        let fixture = Fixture::new();
        let args = fixture.log("old-sent", "対応不要。以前の結果を保持します。", true);
        let mut ctx = fixture.context();
        let (incoming, _) = prepare_log(&mut ctx, &args).unwrap();
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        let request = reserve(&mut ledger, &fixture.home, &incoming).unwrap();
        let mut api = RecoveryApi::bind(&mut ctx, &mut ledger, &request).unwrap();
        let delivery = effect(
            &request,
            "log:0",
            Target::Forum {
                title: request.title.clone(),
            },
            &request.body,
            true,
            true,
        )
        .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Pending {
                    request_id: request.request_id.clone(),
                    phase: "log:0".into(),
                    effect: delivery.clone(),
                },
            )
            .unwrap();
        // Historical fixture: transport an actual pre-correction payload,
        // without passing it through the new submission policy.
        let posted = api
            .effects
            .http(
                "POST",
                &format!("{API}/channels/{FORUM}/threads"),
                Some(&json!({"name":request.title,"message":delivery.payload(OWNER)})),
                true,
            )
            .unwrap();
        let thread = core::s(&posted["id"]);
        let message = core::s(&posted["message"]["id"]);
        ledger
            .append(
                &fixture.home,
                Event::Posted {
                    request_id: request.request_id.clone(),
                    phase: "log:0".into(),
                    thread_id: thread.into(),
                    message_id: message.into(),
                },
            )
            .unwrap();
        let receipt = api
            .verify(&ledger, &request, &delivery, thread, message)
            .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Verified {
                    request_id: request.request_id.clone(),
                    phase: "log:0".into(),
                    receipt,
                },
            )
            .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::DeliveryVerified {
                    request_id: request.request_id.clone(),
                },
            )
            .unwrap();
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let posts = fixture.server.lock().unwrap().posts.len();
        let out = log(&mut ctx, &args).unwrap();
        assert_eq!(out["delivery"], "verified");
        assert_eq!(out["effective_notify"], true); // Historical fact, no new POST.
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_lost_accepted_response_is_resolved_by_get_without_repost() {
        let fixture = Fixture::new();
        fixture.server.lock().unwrap().drop_response = true;
        let args = fixture.log("lost-response", "完了しました。", true);
        let mut ctx = fixture.context();
        let out = log(&mut ctx, &args).unwrap();
        assert_eq!(out["delivery"], "verified");
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let again = log(&mut ctx, &args).unwrap();
        assert_eq!(again["thread_id"], out["thread_id"]);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_unknown_outcome_remains_pending_never_reported_zero_or_completed() {
        let fixture = Fixture::new();
        fixture.server.lock().unwrap().reject_post = true;
        let args = fixture.log("unknown-post", "完了通知の本文です。", true);
        let mut ctx = fixture.context();
        assert!(log(&mut ctx, &args).unwrap_err().contains("Pending"));
        fixture.server.lock().unwrap().reject_post = false;
        assert!(log(&mut ctx, &args).unwrap_err().contains("Pending"));
        let ledger = Ledger::load(&fixture.home).unwrap();
        let request = ledger.request(&args.request_id).unwrap();
        let result = report(&ledger, request, true);
        assert_eq!(result["delivery"], "pending");
        assert_eq!(result["new_pending_effects"], 1);
        assert_eq!(result["legacy_pending"], "unknown");
        assert!(!ledger.complete(&args.request_id));
        assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_failed_preflight_is_resumable_and_reserves_only_before_post() {
        let fixture = Fixture::new();
        fixture.server.lock().unwrap().fail_forum_once = true;
        let args = fixture.log(
            "preflight-failure",
            "管理画面の修復を再開しています。",
            false,
        );
        let mut ctx = fixture.context();
        assert!(log(&mut ctx, &args).is_err());
        assert!(fixture.server.lock().unwrap().posts.is_empty());
        assert!(Ledger::load(&fixture.home)
            .unwrap()
            .effect(&args.request_id, "log:0")
            .is_none());
        assert_eq!(log(&mut ctx, &args).unwrap()["delivery"], "verified");
        assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
        assert_eq!(log(&mut ctx, &args).unwrap()["delivery"], "verified");
        assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_archive_cursor_encodes_timezone_plus() {
        let fixture = Fixture::new();
        {
            let mut state = fixture.server.lock().unwrap();
            state.archive_cursor_page = true;
            state.drop_response = true;
        }
        let args = fixture.log("archive-cursor", "指定一覧を反映しました。", false);
        let mut ctx = fixture.context();
        assert_eq!(log(&mut ctx, &args).unwrap()["delivery"], "verified");
        let state = fixture.server.lock().unwrap();
        assert!(state
            .calls
            .iter()
            .any(|(_, path, _)| path.contains("before=") && path.contains("%2B00%3A00")));
        assert_eq!(state.posts.len(), 1);
        drop(state);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_first_delivery_does_not_scan_unrelated_archived_history() {
        let fixture = Fixture::new();
        let args = fixture.log(
            "fresh-no-history",
            "新しい依頼を担当へ反映しました。",
            false,
        );
        let mut ctx = fixture.context();
        assert_eq!(log(&mut ctx, &args).unwrap()["delivery"], "verified");
        let state = fixture.server.lock().unwrap();
        assert_eq!(state.posts.len(), 1);
        assert_eq!(state.posts_without_pending, 0);
        assert!(state
            .calls
            .iter()
            .any(|(method, path, _)| method == "GET" && path == &format!("/channels/{FORUM}")));
        assert!(!state
            .calls
            .iter()
            .any(|(_, path, _)| path.ends_with("/threads/active")
                || path.contains("/threads/archived/")
                || path.contains("/messages?")));
        drop(state);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_unsent_question_origin_moves_only_to_its_paired_progress() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source = ask(
            &mut ctx,
            &fixture.ask("source-question", "元の質問です。\na 保持\nb 変更"),
        )
        .unwrap();
        let mut next = fixture.ask(
            "unsent-question",
            "別の具体的な質問です。\na はい\nb いいえ",
        );
        let mut decision: Value =
            serde_json::from_slice(&fs::read(&next.decision_file).unwrap()).unwrap();
        decision["resources"][0]["id"] = json!("different-specific-target");
        decision["origin_thread_id"] = source["thread_id"].clone();
        fs::write(&next.decision_file, decision.to_string()).unwrap();
        next.origin_thread = Some(core::s(&source["thread_id"]).into());
        assert!(ask(&mut ctx, &next).unwrap_err().contains("origin"));
        let before_posts = fixture.server.lock().unwrap().posts.len();
        let original = Ledger::load(&fixture.home)
            .unwrap()
            .request(&next.request_id)
            .unwrap()
            .clone();
        assert!(Ledger::load(&fixture.home)
            .unwrap()
            .effect(&next.request_id, "question:0")
            .is_none());

        let unrelated = log(
            &mut ctx,
            &fixture.log("other-topic", "別の作業の進捗です。", false),
        )
        .unwrap();
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        next.origin_thread = Some(core::s(&unrelated["thread_id"]).into());
        decision["origin_thread_id"] = unrelated["thread_id"].clone();
        fs::write(&next.decision_file, decision.to_string()).unwrap();
        assert!(ask(&mut ctx, &next).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);

        next.origin_thread = Some(core::s(&source["progress_thread_id"]).into());
        decision["origin_thread_id"] = source["progress_thread_id"].clone();
        fs::write(&next.decision_file, decision.to_string()).unwrap();
        let original_body_path = next.body_file.clone();
        next.body_file = fixture.home.join("changed-question.txt");
        fs::write(
            &next.body_file,
            "異なる意味の質問です。\na 別の条件\nb 取消",
        )
        .unwrap();
        assert!(ask(&mut ctx, &next).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        next.body_file = original_body_path;

        let delivered = ask(&mut ctx, &next).unwrap();
        assert_eq!(delivered["delivery"], "verified");
        assert_eq!(delivered["original_question"], original.body);
        let posts = fixture.server.lock().unwrap().posts.len();
        assert!(posts > before_posts);
        assert_eq!(
            ask(&mut ctx, &next).unwrap()["thread_id"],
            delivered["thread_id"]
        );
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts);
        let ledger = Ledger::load(&fixture.home).unwrap();
        assert!(ledger
            .events
            .iter()
            .any(|event| matches!(event, Event::Intent { request } if **request == original)));
        assert_eq!(ledger.events.iter().filter(|event| matches!(event, Event::OriginCorrected { request } if request.request_id == next.request_id)).count(), 1);
        let calls = fixture.server.lock().unwrap().calls.len();
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let error = repair_origin(
            &mut ctx,
            &RepairOriginArgs {
                request_id: next.request_id.clone(),
                todo_id: 1,
                expected_origin_thread: core::s(&source["thread_id"]).into(),
                owner_reply_message: "999999999999999999".into(),
                progress_message: "888888888888888888".into(),
            },
        )
        .unwrap_err();
        assert!(
            error.contains("already has an origin correction"),
            "{error}"
        );
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls);
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_possible_prior_post_prevents_origin_correction() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source = ask(
            &mut ctx,
            &fixture.ask("prior-source", "元の質問です。\na 保持\nb 変更"),
        )
        .unwrap();
        let mut next = fixture.ask("prior-uncertain", "新しい質問です。\na はい\nb いいえ");
        let mut decision: Value =
            serde_json::from_slice(&fs::read(&next.decision_file).unwrap()).unwrap();
        decision["resources"][0]["id"] = json!("different-uncertain-target");
        decision["origin_thread_id"] = source["thread_id"].clone();
        fs::write(&next.decision_file, decision.to_string()).unwrap();
        next.origin_thread = Some(core::s(&source["thread_id"]).into());
        assert!(ask(&mut ctx, &next).is_err());
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        let request = ledger.request(&next.request_id).unwrap().clone();
        ledger
            .append(
                &fixture.home,
                Event::Pending {
                    request_id: request.request_id.clone(),
                    phase: "question:0".into(),
                    effect: effect(
                        &request,
                        "question:0",
                        Target::Forum {
                            title: request.title.clone(),
                        },
                        &request.body,
                        true,
                        false,
                    )
                    .unwrap(),
                },
            )
            .unwrap();
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let posts = fixture.server.lock().unwrap().posts.len();
        next.origin_thread = Some(core::s(&source["progress_thread_id"]).into());
        decision["origin_thread_id"] = source["progress_thread_id"].clone();
        fs::write(&next.decision_file, decision.to_string()).unwrap();
        assert!(ask(&mut ctx, &next).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_post_ids_survive_get_failure_and_recover_without_another_send() {
        let fixture = Fixture::new();
        fixture.server.lock().unwrap().block_reads_after_post = true;
        let args = fixture.log("get-failure", "作業完了を通知します。", true);
        let mut ctx = fixture.context();
        assert!(log(&mut ctx, &args).is_err());
        let ledger = Ledger::load(&fixture.home).unwrap();
        assert!(ledger.posted(&args.request_id, "log:0").is_some());
        assert!(ledger.receipt(&args.request_id, "log:0").is_none());
        fixture.server.lock().unwrap().blocked = false;
        assert_eq!(log(&mut ctx, &args).unwrap()["delivery"], "verified");
        assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_get_flags_and_mentions_must_match_before_completion() {
        for wrong_mentions in [false, true] {
            let fixture = Fixture::new();
            {
                let mut state = fixture.server.lock().unwrap();
                state.wrong_flags = !wrong_mentions;
                state.wrong_mentions = wrong_mentions;
            }
            let args = fixture.log("wrong-observed-metadata", "完了しました。", true);
            let mut ctx = fixture.context();
            assert!(log(&mut ctx, &args).is_err());
            let ledger = Ledger::load(&fixture.home).unwrap();
            assert!(!ledger.complete(&args.request_id));
            assert!(ledger.receipt(&args.request_id, "log:0").is_none());
            assert!(log(&mut ctx, &args).is_err());
            assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_messages_legacy_reserved_question_layout_is_retried_without_reposting() {
        let fixture = Fixture::new();
        let args = fixture.ask(
            "legacy-question-layout",
            &"長い質問の背景です。".repeat(400),
        );
        let mut ctx = fixture.context();
        let first = ask(&mut ctx, &args).unwrap();
        let question_id = core::s(&first["thread_id"]);
        let progress_id = core::s(&first["progress_thread_id"]);
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        for event in &mut ledger.events {
            match event {
                Event::Pending { phase, effect, .. }
                    if phase.starts_with("question-continuation:") =>
                {
                    effect.target = Target::Thread {
                        thread: progress_id.into(),
                    };
                }
                Event::Pending { phase, effect, .. } if phase == "question-links" => {
                    effect.content = effect.content.replace("進捗:", "進捗と本文の続き:");
                }
                Event::Posted {
                    phase, thread_id, ..
                } if phase.starts_with("question-continuation:") => *thread_id = progress_id.into(),
                Event::Verified { phase, receipt, .. }
                    if phase.starts_with("question-continuation:") =>
                {
                    receipt.thread_id = progress_id.into()
                }
                _ => {}
            }
        }
        ledger.validate().unwrap();
        fs::write(
            fixture.home.join(LEDGER),
            serde_json::to_vec_pretty(&ledger).unwrap(),
        )
        .unwrap();
        {
            let mut state = fixture.server.lock().unwrap();
            let mut moved = vec![];
            for message in state.messages.get_mut(question_id).unwrap().iter_mut() {
                if core::s(&message["content"]).contains(":question-continuation:") {
                    message["channel_id"] = json!(progress_id);
                    moved.push(message.clone());
                } else if core::s(&message["content"]).contains(":question-links") {
                    message["content"] =
                        json!(core::s(&message["content"]).replace("進捗:", "進捗と本文の続き:"));
                }
            }
            state
                .messages
                .get_mut(question_id)
                .unwrap()
                .retain(|message| {
                    !core::s(&message["content"]).contains(":question-continuation:")
                });
            state.messages.get_mut(progress_id).unwrap().extend(moved);
        }
        let posts = fixture.server.lock().unwrap().posts.len();
        let retry = ask(&mut ctx, &args).unwrap();
        assert_eq!(retry["thread_id"], first["thread_id"]);
        assert_eq!(retry["delivery"], "verified");
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_long_owner_question_continues_in_question_without_extra_mentions() {
        let fixture = Fixture::new();
        let body = format!(
            "指定外の登録の扱いを決めてください。\na 保留して担当を残す\nb 終了した登録だけ整理する\n{}",
            "未確認名はIDで表示します。📝\n".repeat(220)
        );
        let args = fixture.ask("unlisted-2472", &body);
        let mut ctx = fixture.context();
        let out = ask(&mut ctx, &args).unwrap();
        let state = fixture.server.lock().unwrap();
        let question = &state.messages[core::s(&out["thread_id"])];
        let progress = &state.messages[core::s(&out["progress_thread_id"])];
        assert!(question.len() > 2);
        assert_eq!(progress.len(), 1);
        let posted_mentions: usize = state
            .messages
            .values()
            .flatten()
            .map(|m| m["mentions"].as_array().map_or(0, Vec::len))
            .sum();
        assert_eq!(posted_mentions, 1);
        for message in progress
            .iter()
            .chain(question.iter().skip(1))
            .chain(state.messages[ORIGIN].iter())
        {
            assert_eq!(message["mentions"], json!([]));
            assert_eq!(
                core::n(&message["flags"]) & (SILENT | EMBEDS),
                SILENT | EMBEDS
            );
        }
        for message in state.messages.values().flatten() {
            assert!(core::s(&message["content"]).encode_utf16().count() <= MAX_CONTENT);
        }
        assert!(core::s(&progress[0]["content"]).contains(core::s(&out["url"])));
        assert!(
            core::s(&question.last().unwrap()["content"]).contains(core::s(&out["progress_url"]))
        );
        assert!(core::s(&state.messages[ORIGIN][0]["content"]).contains(core::s(&out["url"])));
        drop(state);
        assert_eq!(out["original_question"], body);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_explicit_progress_log_splits_and_deduplicates_same_hash() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let out = ask(
            &mut ctx,
            &fixture.ask(
                "question-for-list",
                "部署を決めてください。\na 現状を保持\nb 一部を整理",
            ),
        )
        .unwrap();
        let args = LogArgs {
            thread: Some(core::s(&out["progress_thread_id"]).into()),
            ..fixture.log(
                "all-registration-ids",
                &"登録IDとToDo番号を記録します。🧾\n".repeat(250),
                false,
            )
        };
        let before = fixture.server.lock().unwrap().posts.len();
        let result = log(&mut ctx, &args).unwrap();
        let after = fixture.server.lock().unwrap().posts.len();
        assert!(after > before + 1);
        assert_eq!(result["thread_id"], out["progress_thread_id"]);
        assert_eq!(
            log(&mut ctx, &args).unwrap()["thread_id"],
            result["thread_id"]
        );
        assert_eq!(fixture.server.lock().unwrap().posts.len(), after);
        let same = LogArgs {
            request_id: "same-body-different-request".into(),
            ..args
        };
        assert_eq!(
            log(&mut ctx, &same).unwrap()["thread_id"],
            result["thread_id"]
        );
        assert_eq!(fixture.server.lock().unwrap().posts.len(), after);
        let state = fixture.server.lock().unwrap();
        for message in &state.messages[core::s(&out["progress_thread_id"])] {
            assert_eq!(message["mentions"], json!([]));
            assert_ne!(core::n(&message["flags"]) & SILENT, 0);
        }
        drop(state);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_existing_progress_cannot_notify_and_legacy_thread_cannot_be_targeted() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let out = ask(
            &mut ctx,
            &fixture.ask(
                "question-before-reject",
                "判断をお願いします。\na 保持\nb 整理",
            ),
        )
        .unwrap();
        let before = fixture.server.lock().unwrap().posts.len();
        let notify = LogArgs {
            thread: Some(core::s(&out["progress_thread_id"]).into()),
            ..fixture.log(
                "forbidden-progress-notify",
                "回答が必要：公開範囲を選んでください。\na: 公開する\nb: 保持する",
                true,
            )
        };
        assert!(log(&mut ctx, &notify).is_err());
        let legacy = LogArgs {
            thread: Some("106".into()),
            ..fixture.log("legacy-rejected", "進捗", false)
        };
        assert!(log(&mut ctx, &legacy).is_err());
        assert_eq!(fixture.server.lock().unwrap().posts.len(), before);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_exact_decision_dedup_keeps_original_wording_and_scope() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let original = fixture.ask(
            "first-decision",
            "登録の扱いを決めてください。\na 残す\nb 整理する",
        );
        let out = ask(&mut ctx, &original).unwrap();
        let before = fixture.server.lock().unwrap().posts.len();
        let duplicate = fixture.ask("renamed-decision", "別の題名です。\na 別の意味\nb 言い換え");
        let result = ask(&mut ctx, &duplicate).unwrap();
        assert_eq!(result["thread_id"], out["thread_id"]);
        assert_eq!(result["original_question"], out["original_question"]);
        assert_eq!(result["decision"], out["decision"]);
        assert_eq!(result["approval"], "not_inferred");
        assert_eq!(
            ask(&mut ctx, &duplicate).unwrap()["thread_id"],
            out["thread_id"]
        );
        assert_eq!(fixture.server.lock().unwrap().posts.len(), before);
        let mut changed = fixture.ask(
            "different-scope",
            "登録の扱いを決めてください。\na 残す\nb 整理する",
        );
        let mut decision: Value =
            serde_json::from_slice(&fs::read(&changed.decision_file).unwrap()).unwrap();
        decision["scope"]["retain_incomplete"] = json!(false);
        fs::write(
            &changed.decision_file,
            serde_json::to_vec(&decision).unwrap(),
        )
        .unwrap();
        changed.title = original.title.clone();
        let different = ask(&mut ctx, &changed).unwrap();
        assert_ne!(different["thread_id"], out["thread_id"]);
        assert!(fixture.server.lock().unwrap().posts.len() > before);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_same_request_id_cannot_change_body_or_todo_scope() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let args = fixture.log("stable-id", "最初の本文", false);
        log(&mut ctx, &args).unwrap();
        let calls = fixture.server.lock().unwrap().calls.len();
        fs::write(&args.body_file, "後の別の本文").unwrap();
        assert!(log(&mut ctx, &args).is_err());
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls);
        let mut wrong = fixture.ask("wrong-todo", "判断してください。\na 保持\nb 整理");
        wrong.todo_id = 2467;
        assert!(ask(&mut ctx, &wrong).is_err());
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_readonly_replies_filter_owner_and_keep_all_files_unchanged() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let args = fixture.ask(
            "reply-question",
            "登録の扱いを決めてください。\na 残す\nb 整理する",
        );
        let out = ask(&mut ctx, &args).unwrap();
        let thread = core::s(&out["thread_id"]);
        fixture.add_reply(thread, "999", "a", false);
        fixture.add_reply(thread, OWNER, "bot spoof", true);
        let owner_id = fixture.add_reply(thread, OWNER, "a 残す方で", false);
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let posts = fixture.server.lock().unwrap().posts.len();
        let inspect = replies(&mut ctx, &reply_args(&out)).unwrap();
        assert_eq!(inspect["owner_replies"].as_array().unwrap().len(), 1);
        assert_eq!(inspect["owner_replies"][0]["message_id"], owner_id);
        assert_eq!(inspect["owner_replies"][0]["author_id"], OWNER);
        assert_eq!(inspect["owner_replies"][0]["todo_id"], 2472);
        assert_eq!(
            inspect["owner_replies"][0]["decision_identity"],
            out["decision"]
        );
        assert_eq!(inspect["owner_replies"][0]["body"], "a 残す方で");
        assert_eq!(
            inspect["owner_replies"][0]["timestamp"],
            "2026-10-12T00:00:01+00:00"
        );
        assert_eq!(inspect["original_question"], out["original_question"]);
        assert_eq!(inspect["delivery"], "inspection_only");
        assert_eq!(inspect["ledger_written"], false);
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts);
        let after = replies(&mut ctx, &reply_args(&out)).unwrap();
        assert_eq!(after["owner_replies"], inspect["owner_replies"]); // never consumed
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_replies_require_exact_new_thread_starter_and_todo() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let out = ask(
            &mut ctx,
            &fixture.ask("exact-reply", "判断をお願いします。\na 残す\nb 整理"),
        )
        .unwrap();
        let calls = fixture.server.lock().unwrap().calls.len();
        for selected in [
            RepliesArgs {
                thread: "106".into(),
                ..reply_args(&out)
            },
            RepliesArgs {
                message: Some("999".into()),
                ..reply_args(&out)
            },
            RepliesArgs {
                todo_id: Some(2467),
                ..reply_args(&out)
            },
            RepliesArgs {
                request_id: Some("unrelated".into()),
                ..reply_args(&out)
            },
            RepliesArgs {
                thread: "../105".into(),
                ..reply_args(&out)
            },
        ] {
            assert!(replies(&mut ctx, &selected).is_err());
        }
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_corrupt_independent_ledger_is_not_empty_and_old_state_is_not_fallback() {
        let fixture = Fixture::new();
        fs::write(fixture.home.join(LEDGER), b"{interrupted").unwrap();
        let mut ctx = fixture.context();
        assert!(log(&mut ctx, &fixture.log("cannot-reset", "本文", false)).is_err());
        assert_eq!(
            fs::read(fixture.home.join(LEDGER)).unwrap(),
            b"{interrupted"
        );
        assert!(fixture.server.lock().unwrap().calls.is_empty());
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_credentials_never_reach_http_or_receipts() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let args = fixture.log("unsafe-body", "password=synthetic-private-value", false);
        let error = log(&mut ctx, &args).unwrap_err();
        assert!(!error.contains("synthetic-private-value"));
        assert!(!fixture.home.join(LEDGER).exists());
        assert!(fixture.server.lock().unwrap().calls.is_empty());
        let image = fixture.root.path().join("fixture.png");
        fs::write(&image, b"\x89PNG\r\n\x1a\npassword=synthetic-private-value").unwrap();
        let args = LogArgs {
            attach: vec![image],
            ..fixture.log("unsafe-image", "画像です。", false)
        };
        assert!(log(&mut ctx, &args).is_err());
        assert!(!fixture.home.join(LEDGER).exists());
        assert!(fixture.server.lock().unwrap().calls.is_empty());
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_image_uses_validated_memory_snapshot_and_get_file_receipt() {
        let fixture = Fixture::new();
        let image = fixture.root.path().join("fixture.png");
        fs::write(&image, b"\x89PNG\r\n\x1a\nsynthetic image fixture").unwrap();
        let args = LogArgs {
            attach: vec![image.clone()],
            ..fixture.log("image-message", "確認用画像です。", false)
        };
        let mut ctx = fixture.context();
        let result = log(&mut ctx, &args).unwrap();
        let state = fixture.server.lock().unwrap();
        assert_eq!(state.multipart_uploads, 1);
        let attachment = &state.messages[core::s(&result["thread_id"])][0]["attachments"][0];
        assert!(core::s(&attachment["filename"]).starts_with("image-"));
        assert_eq!(attachment["size"], fs::metadata(&image).unwrap().len());
        drop(state);
        let raw = fs::read_to_string(fixture.home.join(LEDGER)).unwrap();
        assert!(!raw.contains("synthetic image fixture"));
        assert!(!raw.contains(&image.to_string_lossy().to_string()));
        assert_eq!(
            log(&mut ctx, &args).unwrap()["thread_id"],
            result["thread_id"]
        );
        assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_live_byte_lock_race_makes_one_post_then_same_receipt() {
        let fixture = Fixture::new();
        let args = fixture.log("racing-status", "作業が完了しました。", true);
        let home = fixture.home.clone();
        let root = fixture.root.path().to_path_buf();
        let server = fixture.server.clone();
        let body_file = args.body_file.clone();
        let title = args.title.clone();
        let (entered_send, entered_recv) = mpsc::channel();
        let (release_send, release_recv) = mpsc::channel();
        let worker = std::thread::spawn(move || {
            let mut ctx = context(&home, &root, server, Some((entered_send, release_recv)));
            let args = LogArgs {
                request_id: "racing-status".into(),
                title,
                body_file,
                thread: None,
                notify: true,
                attach: vec![],
            };
            log(&mut ctx, &args)
        });
        let entered = entered_recv.recv_timeout(Duration::from_secs(30));
        let mut ctx = fixture.context();
        let calls = fixture.server.lock().unwrap().calls.len();
        let collision = log(&mut ctx, &args);
        let during = fixture.server.lock().unwrap().calls.len();
        let _ = release_send.send(());
        let first = worker.join().unwrap();
        assert!(entered.is_ok());
        assert!(collision.unwrap_err().contains("busy"));
        assert_eq!(during, calls);
        let first = first.unwrap();
        let next = log(&mut ctx, &args).unwrap();
        assert_eq!(next["thread_id"], first["thread_id"]);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_duplicate_remote_matches_stay_pending_without_a_second_post() {
        let fixture = Fixture::new();
        {
            let mut state = fixture.server.lock().unwrap();
            state.drop_response = true;
            state.block_reads_after_post = true;
        }
        let args = fixture.log("remote-duplicates", "完了通知の本文です。", true);
        let mut ctx = fixture.context();
        assert!(log(&mut ctx, &args).is_err());
        {
            let mut state = fixture.server.lock().unwrap();
            state.blocked = false;
            let mut channel = state.channels["1001"].clone();
            channel["id"] = json!("2001");
            let mut message = state.messages["1001"][0].clone();
            message["id"] = json!("2001");
            message["channel_id"] = json!("2001");
            state.channels.insert("2001".into(), channel);
            state.messages.insert("2001".into(), vec![message]);
        }
        let result = log(&mut ctx, &args);
        assert!(result.unwrap_err().contains("duplicate"));
        assert_eq!(fixture.server.lock().unwrap().posts.len(), 1);
        assert!(!Ledger::load(&fixture.home)
            .unwrap()
            .complete(&args.request_id));
        fixture.unchanged();
    }

    #[test]
    fn recovery_followup_real_api_contract_parent_steering_and_callback_retry_once() {
        use crate::recovery_followup as followup;
        let fixture = Fixture::new();
        let args = fixture.ask("followup-one", "a: 指定外は別欄で保持\nb: 部署へ追加\n");
        let mut decision: Value = core::read_json(&args.decision_file).unwrap();
        decision["scope"]["original_work_todo"] = json!(2471);
        fs::write(&args.decision_file, serde_json::to_vec(&decision).unwrap()).unwrap();
        let mut ctx = fixture.context();
        let question = ask(&mut ctx, &args).unwrap();
        let thread = core::s(&question["thread_id"]);
        fixture.add_reply(thread, "999", "b", false);
        let reply = fixture.add_reply(thread, OWNER, "a", false);
        fs::write(
            &ctx.todo,
            serde_json::to_vec(
                &json!({"items":[{"id":2471,"jobs":[],"note":"元の作業本文"},
            {"id":2472,"status":"done","progress":100}]}),
            )
            .unwrap(),
        )
        .unwrap();
        let config = fixture.root.path().join("followup.json");
        fs::write(
            &config,
            serde_json::to_vec(&followup::Watch {
                request_id: args.request_id,
                thread: thread.into(),
                question_message: core::s(&question["message_id"]).into(),
                todo_id: 2472,
                work_todo: 2471,
                parent_lock: fixture.root.path().join("parent.active"),
                worker_cwd: fixture.root.path().to_path_buf(),
                parent_messages: vec![reply.clone()],
            })
            .unwrap(),
        )
        .unwrap();
        let registered = followup::register(&mut ctx, &config).unwrap();
        assert_eq!(registered["scheduled_observations"], 0);
        for _ in 0..2 {
            followup::tick(&mut ctx).unwrap();
        }
        let observed = followup::inspect(&ctx).unwrap();
        assert_eq!(observed["replies"].as_array().unwrap().len(), 1);
        assert_eq!(observed["scheduled_observations"], 2);
        assert_eq!(observed["replies"][0]["phase"], "parent_pending");
        let state_path = fixture.home.join("recovery-followup.json");
        let saved = fs::read(&state_path).unwrap();
        let mut wrong: Value = serde_json::from_slice(&saved).unwrap();
        wrong["replies"][&reply]["steering_handler"] = json!("unrelated-test-job");
        fs::write(&state_path, serde_json::to_vec(&wrong).unwrap()).unwrap();
        let before_claim = fs::read(&state_path).unwrap();
        assert!(followup::claim(&mut ctx, &reply, "unrelated-test-job")
            .unwrap_err()
            .contains("claimant"));
        assert_eq!(fs::read(&state_path).unwrap(), before_claim);
        fs::write(&state_path, &saved).unwrap();
        let result = fixture.root.path().join("applied.json");
        fs::write(&result,serde_json::to_vec(&json!({"handler":"parent","revision":"349ed9ccf7c3ee1699601d749b1a2b5f7504c73a",
            "verification":"表示の17検査成功","summary":"指定外を別欄に残しました。","applied_at":"2026-10-12T00:00:02+00:00"})).unwrap()).unwrap();
        fixture.server.lock().unwrap().block_reads_after_post = true;
        let posts_before = fixture.server.lock().unwrap().posts.len();
        assert!(followup::applied(&mut ctx, &reply, &result).is_err());
        assert_eq!(
            followup::inspect(&ctx).unwrap()["replies"][0]["phase"],
            "result_pending"
        );
        {
            let mut server = fixture.server.lock().unwrap();
            server.blocked = false;
            server.block_reads_after_post = false;
        }
        let mut restarted = fixture.context();
        let finished = followup::applied(&mut restarted, &reply, &result).unwrap();
        assert_eq!(finished["replies"][0]["phase"], "applied");
        followup::applied(&mut restarted, &reply, &result).unwrap();
        followup::tick(&mut restarted).unwrap();
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before + 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_followup_reply_arriving_during_result_post_prevents_decision_done() {
        use crate::recovery_followup as followup;
        let fixture = Fixture::new();
        let args = fixture.ask("followup-late", "a: 別欄で保持\nb: 部署へ追加\n");
        let mut decision = core::read_json(&args.decision_file).unwrap();
        decision["scope"]["original_work_todo"] = json!(2471);
        fs::write(&args.decision_file, serde_json::to_vec(&decision).unwrap()).unwrap();
        let mut ctx = fixture.context();
        let question = ask(&mut ctx, &args).unwrap();
        let thread = core::s(&question["thread_id"]);
        let mid = fixture.add_reply(thread, OWNER, "a", false);
        fs::write(
            &ctx.todo,
            serde_json::to_vec(
                &json!({"items":[{"id":2471,"jobs":[]},{"id":2472,"status":"block"}]}),
            )
            .unwrap(),
        )
        .unwrap();
        let config = fixture.root.path().join("watch-late.json");
        fs::write(
            &config,
            serde_json::to_vec(&followup::Watch {
                request_id: args.request_id,
                thread: thread.into(),
                question_message: core::s(&question["message_id"]).into(),
                todo_id: 2472,
                work_todo: 2471,
                parent_lock: fixture.root.path().join("parent-late.active"),
                worker_cwd: fixture.root.path().to_path_buf(),
                parent_messages: vec![mid.clone()],
            })
            .unwrap(),
        )
        .unwrap();
        followup::register(&mut ctx, &config).unwrap();
        followup::tick(&mut ctx).unwrap();
        let result = fixture.root.path().join("result-late.json");
        fs::write(&result,serde_json::to_vec(&json!({"handler":"parent","revision":"349ed9ccf7c3ee1699601d749b1a2b5f7504c73a",
            "verification":"表示の17検査成功","summary":"別欄へ反映しました。","applied_at":"2026-10-12T00:00:02Z"})).unwrap()).unwrap();
        fixture.server.lock().unwrap().inject_owner_reply =
            Some((thread.into(), "b: 登録番号の指定を追記します".into()));
        let out = followup::applied(&mut ctx, &mid, &result).unwrap();
        assert_eq!(out["replies"].as_array().unwrap().len(), 2);
        assert_eq!(out["replies"][0]["phase"], "applied");
        assert_eq!(out["replies"][1]["phase"], "reserved");
        assert_eq!(
            core::read_json(&ctx.todo).unwrap()["items"][1]["status"],
            "block"
        );
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_credential_reply_is_never_recorded_or_echoed() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let out = ask(
            &mut ctx,
            &fixture.ask("reply-privacy", "判断をお願いします。\na 保持\nb 整理"),
        )
        .unwrap();
        fixture.add_reply(
            core::s(&out["thread_id"]),
            OWNER,
            "password=private-fixture-value",
            false,
        );
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let error = replies(&mut ctx, &reply_args(&out)).unwrap_err();
        assert!(!error.contains("private-fixture-value"));
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        fixture.unchanged();
    }

    fn failed_unsent_ask(fixture: &Fixture, id: &str, origin: &str, reply_author: &str) -> AskArgs {
        failed_unsent_ask_images(fixture, id, origin, reply_author, vec![])
    }

    fn failed_unsent_ask_images(
        fixture: &Fixture,
        id: &str,
        origin: &str,
        reply_author: &str,
        attach: Vec<PathBuf>,
    ) -> AskArgs {
        let mut args = fixture.ask(
            id,
            "端末の承認をお願いします。\nGoogle Workspaceの指定アカウントで続行します。",
        );
        args.attach = attach;
        let mut decision: Value =
            serde_json::from_slice(&fs::read(&args.decision_file).unwrap()).unwrap();
        decision["origin_thread_id"] = json!(origin);
        decision["resources"][0]["id"] = json!(id);
        fs::write(&args.decision_file, serde_json::to_vec(&decision).unwrap()).unwrap();
        {
            let mut state = fixture.server.lock().unwrap();
            state.channels.insert(
                origin.into(),
                json!({"id":origin,"type":11,"guild_id":GUILD,
                "parent_id":REQUEST_FORUM,"owner_id":BOT,"name":"元の依頼"}),
            );
            state.messages.insert(
                origin.into(),
                vec![json!({"id":"108","channel_id":origin,
                "author":{"id":reply_author,"bot":false},"content":"指定済みの回答",
                "type":0,"timestamp":"2026-10-13T00:00:00Z"})],
            );
        }
        let mut ctx = fixture.context();
        assert!(ask(&mut ctx, &args).is_err());
        let ledger: Value =
            serde_json::from_slice(&fs::read(fixture.home.join(LEDGER)).unwrap()).unwrap();
        assert_eq!(
            ledger["events"]
                .as_array()
                .unwrap()
                .iter()
                .filter(|e| e["event"] == "pending" && e["request_id"] == id)
                .count(),
            0
        );
        args
    }

    #[test]
    fn owner_reply_must_match_exact_thread_message_and_non_bot_author() {
        let valid = json!({"id":"108","channel_id":"107",
            "author":{"id":OWNER,"bot":false},"type":0,"content":"verified"});
        assert!(exact_owner_reply(&valid, "107", "108", OWNER));
        for invalid in [
            json!({"id":"109","channel_id":"107","author":{"id":OWNER},"type":0,"content":"x"}),
            json!({"id":"108","channel_id":"109","author":{"id":OWNER},"type":0,"content":"x"}),
            json!({"id":"108","channel_id":"107","author":{"id":"999"},"type":0,"content":"x"}),
            json!({"id":"108","channel_id":"107","author":{"id":OWNER,"bot":true},"type":0,"content":"x"}),
            json!({"id":"108","channel_id":"107","author":{"id":OWNER},"webhook_id":"999","type":0,"content":"x"}),
            json!({"id":"108","channel_id":"107","author":{"id":OWNER},"type":1,"content":"x"}),
            json!({"id":"108","channel_id":"107","author":{"id":OWNER},"type":0}),
        ] {
            assert!(!exact_owner_reply(&invalid, "107", "108", OWNER));
        }
    }

    fn repair_args(id: &str, origin: &str, progress_message: &str) -> RepairOriginArgs {
        RepairOriginArgs {
            request_id: id.into(),
            todo_id: 2472,
            expected_origin_thread: origin.into(),
            owner_reply_message: "108".into(),
            progress_message: progress_message.into(),
        }
    }

    fn verified_progress_source(
        fixture: &Fixture,
        ctx: &mut Context,
        id: &str,
        origin: &str,
        todo_id: u64,
    ) -> Value {
        let args = fixture.ask(id, "親依頼の進捗");
        let mut decision: Value =
            serde_json::from_slice(&fs::read(&args.decision_file).unwrap()).unwrap();
        decision["origin_thread_id"] = json!(origin);
        decision["todo_id"] = json!(todo_id);
        decision["resources"][0]["id"] = json!(id);
        let mut args = args;
        args.todo_id = todo_id;
        fs::write(&args.decision_file, serde_json::to_vec(&decision).unwrap()).unwrap();
        fixture.server.lock().unwrap().channels.insert(
            origin.into(),
            json!({"id":origin,"type":11,"guild_id":GUILD,
                "parent_id":REQUEST_FORUM,"owner_id":OWNER,"name":"元の依頼"}),
        );
        ask(ctx, &args).unwrap()
    }

    #[test]
    fn repaired_image_question_resumes_with_the_same_verified_bytes() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source =
            verified_progress_source(&fixture, &mut ctx, "image-repair-progress", "107", 2472);
        let image = fixture.root.path().join("question.png");
        fs::write(&image, b"\x89PNG\r\n\x1a\nquestion-image").unwrap();
        failed_unsent_ask_images(
            &fixture,
            "image-repair-question",
            "107",
            OWNER,
            vec![image.clone()],
        );
        repair_origin(
            &mut ctx,
            &repair_args(
                "image-repair-question",
                "107",
                core::s(&source["progress_message_id"]),
            ),
        )
        .unwrap();
        let args = ResumeAskArgs {
            request_id: "image-repair-question".into(),
            todo_id: 2472,
            attach: vec![image.clone()],
        };
        let result = resume_ask(&mut ctx, &args).unwrap();
        assert_eq!(result["delivery"], "verified");
        assert_eq!(
            fixture.server.lock().unwrap().messages[core::s(&result["thread_id"])][0]
                ["attachments"]
                .as_array()
                .unwrap()
                .len(),
            1
        );
        resume_ask(&mut ctx, &args).unwrap();
        assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
        fs::write(image, b"\x89PNG\r\n\x1a\nchanged-image").unwrap();
        assert_eq!(resume_ask(&mut ctx, &args).unwrap()["delivery"], "verified");
        assert_eq!(fixture.server.lock().unwrap().multipart_uploads, 1);
        fixture.unchanged();
    }

    #[test]
    fn exact_unsent_origin_repair_posts_once_then_get_verifies_question_and_progress() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source =
            verified_progress_source(&fixture, &mut ctx, "verified-progress-source", "107", 2472);
        let _args = failed_unsent_ask(&fixture, "one-device-approval", "107", OWNER);
        let before_unrepaired_resume = fs::read(fixture.home.join(LEDGER)).unwrap();
        let posts_before_unrepaired_resume = fixture.server.lock().unwrap().posts.len();
        assert!(resume_ask(
            &mut ctx,
            &ResumeAskArgs {
                request_id: "one-device-approval".into(),
                todo_id: 2472,
                attach: vec![],
            },
        )
        .is_err());
        assert_eq!(
            fs::read(fixture.home.join(LEDGER)).unwrap(),
            before_unrepaired_resume
        );
        assert_eq!(
            fixture.server.lock().unwrap().posts.len(),
            posts_before_unrepaired_resume
        );
        let before: Value =
            serde_json::from_slice(&fs::read(fixture.home.join(LEDGER)).unwrap()).unwrap();
        let posts_before = fixture.server.lock().unwrap().posts.len();
        let repair = repair_args(
            "one-device-approval",
            "107",
            core::s(&source["progress_message_id"]),
        );
        let repaired = repair_origin(&mut ctx, &repair).unwrap();
        assert_eq!(repaired["repair"], "recorded");
        assert_ne!(
            repaired["original_fingerprint"],
            repaired["repaired_fingerprint"]
        );
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before);
        let repaired_bytes = fs::read(fixture.home.join(LEDGER)).unwrap();
        let calls_after_repair = fixture.server.lock().unwrap().calls.len();
        let repeated = repair_origin(&mut ctx, &repair).unwrap();
        assert_eq!(repeated["repair"], "already_recorded");
        assert_eq!(
            repeated["repaired_fingerprint"],
            repaired["repaired_fingerprint"]
        );
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), repaired_bytes);
        let different_repair = repair_args("one-device-approval", "107", "9999");
        assert!(repair_origin(&mut ctx, &different_repair).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), repaired_bytes);
        assert_eq!(
            fixture.server.lock().unwrap().calls.len(),
            calls_after_repair
        );
        let after_repair: Value =
            serde_json::from_slice(&fs::read(fixture.home.join(LEDGER)).unwrap()).unwrap();
        let original = before["events"]
            .as_array()
            .unwrap()
            .iter()
            .find(|e| e["event"] == "intent" && e["request"]["request_id"] == "one-device-approval")
            .unwrap();
        let repaired_intent = after_repair["events"]
            .as_array()
            .unwrap()
            .iter()
            .find(|e| e["event"] == "intent" && e["request"]["request_id"] == "one-device-approval")
            .unwrap();
        assert_eq!(
            original, repaired_intent,
            "the immutable original intent is retained"
        );

        let before_resume = fs::read(fixture.home.join(LEDGER)).unwrap();
        let posts_before_resume = fixture.server.lock().unwrap().posts.len();
        assert!(resume_ask(
            &mut ctx,
            &ResumeAskArgs {
                request_id: "one-device-approval".into(),
                todo_id: 2498,
                attach: vec![],
            },
        )
        .is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before_resume);
        assert_eq!(
            fixture.server.lock().unwrap().posts.len(),
            posts_before_resume
        );

        let out = resume_ask(
            &mut ctx,
            &ResumeAskArgs {
                request_id: "one-device-approval".into(),
                todo_id: 2472,
                attach: vec![],
            },
        )
        .unwrap();
        assert_eq!(out["delivery"], "verified");
        assert_eq!(
            out["original_fingerprint"],
            original["request"]["fingerprint"]
        );
        assert_ne!(out["fingerprint"], out["original_fingerprint"]);
        assert_eq!(out["fingerprint"], repaired["repaired_fingerprint"]);
        assert!(!out["thread_id"].as_str().unwrap().is_empty());
        assert!(out["url"]
            .as_str()
            .unwrap()
            .contains(core::s(&out["thread_id"])));
        let state = fixture.server.lock().unwrap();
        let question_posts: Vec<_> = state
            .posts
            .iter()
            .filter(|(_, body)| {
                body["message"]["content"]
                    .as_str()
                    .is_some_and(|content| content.contains("端末の承認をお願いします。"))
            })
            .collect();
        assert_eq!(question_posts.len(), 1);
        assert_eq!(
            question_posts[0].1["message"]["allowed_mentions"]["users"],
            json!([OWNER])
        );
        assert_eq!(
            core::n(&question_posts[0].1["message"]["flags"]) & SILENT,
            0
        );
        let posts_after = state.posts.len();
        drop(state);
        resume_ask(
            &mut ctx,
            &ResumeAskArgs {
                request_id: "one-device-approval".into(),
                todo_id: 2472,
                attach: vec![],
            },
        )
        .unwrap();
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_after);
        assert!(fixture
            .server
            .lock()
            .unwrap()
            .calls
            .iter()
            .any(
                |(method, path, _)| method == "GET" && path.ends_with(core::s(&out["message_id"]))
            ));
        fixture.unchanged();
    }

    #[test]
    fn resumed_repaired_ask_recovers_lost_post_response_without_duplicate_notification() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source =
            verified_progress_source(&fixture, &mut ctx, "progress-lost-post", "107", 2472);
        let _ = failed_unsent_ask(&fixture, "lost-post-target", "107", OWNER);
        let repair = repair_args(
            "lost-post-target",
            "107",
            core::s(&source["progress_message_id"]),
        );
        repair_origin(&mut ctx, &repair).unwrap();
        let posts_before = fixture.server.lock().unwrap().posts.len();
        {
            let mut server = fixture.server.lock().unwrap();
            server.drop_response = true;
            server.block_reads_after_post = true;
        }
        let resume = ResumeAskArgs {
            request_id: "lost-post-target".into(),
            todo_id: 2472,
            attach: vec![],
        };
        assert!(resume_ask(&mut ctx, &resume).is_err());
        let pending: Value =
            serde_json::from_slice(&fs::read(fixture.home.join(LEDGER)).unwrap()).unwrap();
        assert!(pending["events"]
            .as_array()
            .unwrap()
            .iter()
            .any(|event| event["event"] == "pending"
                && event["request_id"] == "lost-post-target"
                && event["phase"] == "question:0"));
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before + 1);
        {
            let mut server = fixture.server.lock().unwrap();
            server.blocked = false;
            server.block_reads_after_post = false;
            server.drop_response = false;
        }
        let out = resume_ask(&mut ctx, &resume).unwrap();
        assert_eq!(out["delivery"], "verified");
        let state = fixture.server.lock().unwrap();
        let question_posts: Vec<_> = state
            .posts
            .iter()
            .filter(|(_, body)| {
                body["message"]["content"]
                    .as_str()
                    .is_some_and(|content| content.contains("端末の承認をお願いします。"))
            })
            .collect();
        assert_eq!(question_posts.len(), 1);
        let posts_after = state.posts.len();
        drop(state);
        resume_ask(&mut ctx, &resume).unwrap();
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_after);
        fixture.unchanged();
    }

    #[test]
    fn origin_repair_rejects_wrong_owner_todo_origin_body_target_and_started_send() {
        for (case, author) in [
            ("wrong-owner", "999"),
            ("wrong-todo", OWNER),
            ("wrong-origin", OWNER),
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let source = verified_progress_source(
                &fixture,
                &mut ctx,
                &format!("progress-{case}"),
                "107",
                2472,
            );
            let _args = failed_unsent_ask(&fixture, &format!("repair-{case}"), "107", author);
            let mut repair = repair_args(
                &format!("repair-{case}"),
                "107",
                core::s(&source["progress_message_id"]),
            );
            if case == "wrong-todo" {
                repair.todo_id = 2498;
            }
            if case == "wrong-origin" {
                repair.expected_origin_thread = "109".into();
            }
            let before = fs::read(fixture.home.join(LEDGER)).unwrap();
            let posts_before = fixture.server.lock().unwrap().posts.len();
            assert!(repair_origin(&mut ctx, &repair).is_err(), "{case}");
            assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before);
        }

        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source =
            verified_progress_source(&fixture, &mut ctx, "progress-target-guard", "107", 2472);
        let mut args = failed_unsent_ask(&fixture, "target-guard", "107", OWNER);
        let mut altered = fs::read(&args.body_file).unwrap();
        altered.extend_from_slice("追加".as_bytes());
        fs::write(&args.body_file, altered).unwrap();
        assert!(
            ask(&mut ctx, &args).is_err(),
            "changed body cannot reuse reserved request identity"
        );
        args.body_file = fixture.ask("unused-target-body", "未使用").body_file;
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let unverified_target = repair_args("target-guard", "107", "9999");
        assert!(repair_origin(&mut ctx, &unverified_target).is_err());
        let old_question_target =
            repair_args("target-guard", "107", core::s(&source["message_id"]));
        assert!(repair_origin(&mut ctx, &old_question_target).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);

        let mut ledger = Ledger::load(&fixture.home).unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Pending {
                    request_id: "target-guard".into(),
                    phase: "question:0".into(),
                    effect: Effect {
                        target: Target::Forum {
                            title: "pending".into(),
                        },
                        content: "pending".into(),
                        notify: true,
                        images: vec![],
                    },
                },
            )
            .unwrap();
        let repair = repair_args(
            "target-guard",
            "107",
            core::s(&source["progress_message_id"]),
        );
        let posts_before = fixture.server.lock().unwrap().posts.len();
        let before_started = fs::read(fixture.home.join(LEDGER)).unwrap();
        assert!(
            repair_origin(&mut ctx, &repair).is_err(),
            "pending send must never be rebound"
        );
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before_started);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before);
    }

    #[test]
    fn origin_repair_requires_progress_from_the_same_task_and_original_thread() {
        for (case, source_origin, source_todo) in [
            ("different-origin", "109", 2472),
            ("different-todo", "107", 2498),
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let source = verified_progress_source(
                &fixture,
                &mut ctx,
                &format!("progress-{case}"),
                source_origin,
                source_todo,
            );
            let _ = failed_unsent_ask(&fixture, &format!("repair-{case}"), "107", OWNER);
            let repair = repair_args(
                &format!("repair-{case}"),
                "107",
                core::s(&source["progress_message_id"]),
            );
            let before = fs::read(fixture.home.join(LEDGER)).unwrap();
            let http_before = fixture.server.lock().unwrap().calls.len();
            assert!(repair_origin(&mut ctx, &repair).is_err());
            assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            assert_eq!(fixture.server.lock().unwrap().calls.len(), http_before);
            let mut starter_as_reply = repair_args(
                &format!("repair-{case}"),
                "107",
                core::s(&source["progress_message_id"]),
            );
            starter_as_reply.owner_reply_message = "107".into();
            assert!(repair_origin(&mut ctx, &starter_as_reply).is_err());
            assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            assert_eq!(fixture.server.lock().unwrap().calls.len(), http_before);
        }
    }

    #[test]
    fn duplicate_progress_message_receipts_are_ambiguous_and_never_repaired() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source = verified_progress_source(&fixture, &mut ctx, "progress-unique", "107", 2472);
        let _ = failed_unsent_ask(&fixture, "duplicate-progress-target", "107", OWNER);
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        let source_request = ledger
            .request(core::s(&source["request_id"]))
            .unwrap()
            .clone();
        let source_bot = ledger.bot(&source_request.request_id).unwrap().to_owned();
        let source_effect = ledger
            .effect(&source_request.request_id, "progress")
            .unwrap()
            .clone();
        let (thread_id, message_id) = ledger
            .posted(&source_request.request_id, "progress")
            .unwrap();
        let receipt = ledger
            .receipt(&source_request.request_id, "progress")
            .unwrap()
            .clone();
        let mut duplicate = source_request;
        duplicate.request_id = "duplicate-progress-source".into();
        ledger
            .append(
                &fixture.home,
                Event::Intent {
                    request: Box::new(duplicate.clone()),
                },
            )
            .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Bot {
                    request_id: duplicate.request_id.clone(),
                    author_id: source_bot,
                },
            )
            .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Pending {
                    request_id: duplicate.request_id.clone(),
                    phase: "progress".into(),
                    effect: source_effect,
                },
            )
            .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Posted {
                    request_id: duplicate.request_id.clone(),
                    phase: "progress".into(),
                    thread_id,
                    message_id,
                },
            )
            .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Verified {
                    request_id: duplicate.request_id,
                    phase: "progress".into(),
                    receipt,
                },
            )
            .unwrap();
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let calls_before = fixture.server.lock().unwrap().calls.len();
        let repair = repair_args(
            "duplicate-progress-target",
            "107",
            core::s(&source["progress_message_id"]),
        );
        assert!(repair_origin(&mut ctx, &repair).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls_before);
    }

    #[test]
    fn progress_from_a_different_bot_or_destination_is_rejected_before_http() {
        for mismatch in ["bot", "destination"] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let source = verified_progress_source(
                &fixture,
                &mut ctx,
                &format!("progress-{mismatch}"),
                "107",
                2472,
            );
            let request_id = format!("target-{mismatch}");
            let _ = failed_unsent_ask(&fixture, &request_id, "107", OWNER);
            let mut ledger = Ledger::load(&fixture.home).unwrap();
            let source_id = core::s(&source["request_id"]);
            for event in &mut ledger.events {
                match event {
                    Event::Intent { request }
                        if request.request_id == source_id && mismatch == "destination" =>
                    {
                        request.destination.forum = "999".into();
                        request.fingerprint = request.fingerprint().unwrap();
                    }
                    Event::Bot {
                        request_id,
                        author_id,
                    } if mismatch == "bot" && request_id == source_id => {
                        *author_id = "999".into();
                    }
                    Event::Verified {
                        request_id,
                        receipt,
                        ..
                    } if mismatch == "bot" && request_id == source_id => {
                        receipt.author_id = "999".into();
                    }
                    _ => {}
                }
            }
            let bytes = serde_json::to_vec(&ledger).unwrap();
            fs::write(fixture.home.join(LEDGER), bytes).unwrap();
            Ledger::load(&fixture.home).unwrap();
            let before = fs::read(fixture.home.join(LEDGER)).unwrap();
            let calls_before = fixture.server.lock().unwrap().calls.len();
            let repair = repair_args(&request_id, "107", core::s(&source["progress_message_id"]));
            assert!(repair_origin(&mut ctx, &repair).is_err());
            assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            assert_eq!(fixture.server.lock().unwrap().calls.len(), calls_before);
        }
    }

    #[test]
    fn repair_event_cannot_be_attached_after_any_send_reservation() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source =
            verified_progress_source(&fixture, &mut ctx, "progress-posted-guard", "107", 2472);
        let _args = failed_unsent_ask(&fixture, "posted-guard", "107", OWNER);
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        let effect = Effect {
            target: Target::Forum {
                title: "pending".into(),
            },
            content: "pending".into(),
            notify: true,
            images: vec![],
        };
        ledger
            .append(
                &fixture.home,
                Event::Pending {
                    request_id: "posted-guard".into(),
                    phase: "question:0".into(),
                    effect,
                },
            )
            .unwrap();
        ledger
            .append(
                &fixture.home,
                Event::Posted {
                    request_id: "posted-guard".into(),
                    phase: "question:0".into(),
                    thread_id: "30".into(),
                    message_id: "31".into(),
                },
            )
            .unwrap();
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let posts_before = fixture.server.lock().unwrap().posts.len();
        let repair = repair_args(
            "posted-guard",
            "107",
            core::s(&source["progress_message_id"]),
        );
        assert!(repair_origin(&mut ctx, &repair).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before);
    }

    #[test]
    fn ledger_validation_rejects_directly_appended_repair_that_changes_or_repeats_evidence() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source =
            verified_progress_source(&fixture, &mut ctx, "progress-validation", "107", 2472);
        let _ = failed_unsent_ask(&fixture, "validation-target", "107", OWNER);
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        let original = ledger.request("validation-target").unwrap();
        let source_id = core::s(&source["request_id"]);
        let source_receipt = ledger.receipt(source_id, "progress").unwrap();
        let event = Event::OriginRepair {
            request_id: "validation-target".into(),
            todo_id: 2472,
            expected_origin_thread: "107".into(),
            owner_reply_message: "108".into(),
            progress_request_id: source_id.into(),
            progress_thread: source_receipt.thread_id.clone(),
            progress_message: source_receipt.message_id.clone(),
            original_fingerprint: original.fingerprint.clone(),
        };
        ledger.events.push(event.clone());
        assert!(ledger.validate().is_ok());

        for mutation in [
            "todo",
            "origin",
            "fingerprint",
            "duplicate",
            "after-pending",
        ] {
            let bytes = serde_json::to_vec(&ledger).unwrap();
            let mut candidate: Ledger = serde_json::from_slice(&bytes).unwrap();
            match mutation {
                "duplicate" => candidate.events.push(event.clone()),
                "todo" => {
                    if let Some(Event::OriginRepair { todo_id, .. }) = candidate.events.last_mut() {
                        *todo_id = 2498;
                    }
                }
                "origin" => {
                    if let Some(Event::OriginRepair {
                        expected_origin_thread,
                        ..
                    }) = candidate.events.last_mut()
                    {
                        *expected_origin_thread = "109".into();
                    }
                }
                "fingerprint" => {
                    if let Some(Event::OriginRepair {
                        original_fingerprint,
                        ..
                    }) = candidate.events.last_mut()
                    {
                        *original_fingerprint = "f".repeat(64);
                    }
                }
                "after-pending" => {
                    let event_index = candidate.events.len() - 1;
                    candidate.events.insert(
                        event_index,
                        Event::Pending {
                            request_id: "validation-target".into(),
                            phase: "question:0".into(),
                            effect: Effect {
                                target: Target::Forum {
                                    title: "pending".into(),
                                },
                                content: "pending".into(),
                                notify: true,
                                images: vec![],
                            },
                        },
                    );
                }
                _ => unreachable!(),
            }
            let error = candidate.validate().unwrap_err();
            assert_eq!(
                error, "origin repair does not match the exact unsent request",
                "mutation={mutation}"
            );
        }
    }

    #[test]
    fn verified_or_completed_question_cannot_be_rebound() {
        for completed in [false, true] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let source = verified_progress_source(
                &fixture,
                &mut ctx,
                if completed {
                    "progress-completed"
                } else {
                    "progress-verified"
                },
                "107",
                2472,
            );
            let _ = failed_unsent_ask(
                &fixture,
                if completed {
                    "completed-question"
                } else {
                    "verified-question"
                },
                "107",
                OWNER,
            );
            let id = if completed {
                "completed-question"
            } else {
                "verified-question"
            };
            let mut ledger = Ledger::load(&fixture.home).unwrap();
            ledger
                .append(
                    &fixture.home,
                    Event::Pending {
                        request_id: id.into(),
                        phase: "question:0".into(),
                        effect: Effect {
                            target: Target::Forum {
                                title: "question".into(),
                            },
                            content: "question".into(),
                            notify: true,
                            images: vec![],
                        },
                    },
                )
                .unwrap();
            ledger
                .append(
                    &fixture.home,
                    Event::Posted {
                        request_id: id.into(),
                        phase: "question:0".into(),
                        thread_id: "30".into(),
                        message_id: "31".into(),
                    },
                )
                .unwrap();
            ledger
                .append(
                    &fixture.home,
                    Event::Verified {
                        request_id: id.into(),
                        phase: "question:0".into(),
                        receipt: Receipt {
                            thread_id: "30".into(),
                            message_id: "31".into(),
                            author_id: BOT.into(),
                            timestamp: "2026-10-13T00:00:00Z".into(),
                            flags: 4,
                            owner_mentions: 1,
                        },
                    },
                )
                .unwrap();
            if completed {
                ledger
                    .append(
                        &fixture.home,
                        Event::DeliveryVerified {
                            request_id: id.into(),
                        },
                    )
                    .unwrap();
            }
            let before = fs::read(fixture.home.join(LEDGER)).unwrap();
            let posts_before = fixture.server.lock().unwrap().posts.len();
            let repair = repair_args(id, "107", core::s(&source["progress_message_id"]));
            assert!(repair_origin(&mut ctx, &repair).is_err());
            assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before);
        }
    }
    fn close_fixture(fixture: &Fixture, ctx: &mut Context) -> (CloseArgs, CloseBinding, Value) {
        let question = ask(
            ctx,
            &fixture.ask("close-exact", "指定の閲覧をお願いします。\na 閲覧\nb 保留"),
        )
        .unwrap();
        let mid = fixture.add_reply(core::s(&question["thread_id"]), OWNER, "a", false);
        let inspection = replies(ctx, &reply_args(&question)).unwrap();
        let application = json!({"source":"discord-api-v10","todo_id":2544,"origin_todo_id":2472,
            "job_id":"test-parent-2544","revision":"349ed9ccf7c3ee1699601d749b1a2b5f7504c73a",
            "request_id":"close-exact","thread_id":question["thread_id"],"question_message_id":question["message_id"],
            "message_id":mid,"author_id":OWNER,"applied_at":"2026-10-12T00:00:02Z",
            "completion":"requested read applied; sheet work stays pending",
            "verification":{"execution":"read verified","origin_saved":"original spec and reply reread", "identity":"configured owner/API/question/todo matched", "private_evidence":"kept by parent", "sheet":"remains with original todo", "tabs":"parent closed its tabs", "other_work":"preserved"}});
        let applied_home = fixture.home.join("unattended/applied");
        fs::create_dir_all(&applied_home).unwrap();
        let bytes = serde_json::to_vec(&application).unwrap();
        fs::write(applied_home.join(format!("{mid}.json")), &bytes).unwrap();
        let ledger = Ledger::load(&fixture.home).unwrap();
        let binding = CloseBinding {
            request_id: "close-exact".into(),
            thread_id: core::s(&question["thread_id"]).into(),
            question_message_id: core::s(&question["message_id"]).into(),
            origin_todo_id: 2472,
            todo_id: 2544,
            message_id: mid,
            author_id: OWNER.into(),
            job_id: "test-parent-2544".into(),
            revision: core::s(&application["revision"]).into(),
            question_fingerprint: ledger.request("close-exact").unwrap().fingerprint.clone(),
            reply_sha256: digest(&inspection["owner_replies"][0]).unwrap(),
            receipt_sha256: format!("{:x}", Sha256::digest(&bytes)),
            verification_sha256: digest(&application["verification"]).unwrap(),
            completion: core::s(&application["completion"]).into(),
        };
        let args = CloseArgs {
            manifest: fixture.root.path().join("close-manifest.json"),
            legacy_reply_timestamp: None,
        };
        fs::write(&args.manifest, serde_json::to_vec(&binding).unwrap()).unwrap();
        (args, binding, application)
    }

    fn patch_count(fixture: &Fixture) -> usize {
        fixture
            .server
            .lock()
            .unwrap()
            .calls
            .iter()
            .filter(|(method, _, _)| method == "PATCH")
            .count()
    }

    const LEGACY_SAME_INSTANT: &str = "2026-10-12T09:00:01+09:00";

    /// Rewrites the manifest hash as if the reviewed reply had been serialized with another
    /// timestamp text (and optionally another field), as the PowerShell round trip did.
    fn rehash_reply(
        ctx: &mut Context,
        args: &CloseArgs,
        binding: &CloseBinding,
        legacy: &str,
        mutate: impl FnOnce(&mut Value),
    ) -> CloseBinding {
        let inspection = replies(
            ctx,
            &RepliesArgs {
                thread: binding.thread_id.clone(),
                message: Some(binding.question_message_id.clone()),
                request_id: Some(binding.request_id.clone()),
                todo_id: Some(binding.origin_todo_id),
                read_only: true,
            },
        )
        .unwrap();
        let mut reply = inspection["owner_replies"][0].clone();
        assert_ne!(reply["timestamp"], legacy);
        reply["timestamp"] = json!(legacy);
        mutate(&mut reply);
        let mut changed = binding.clone();
        changed.reply_sha256 = digest(&reply).unwrap();
        fs::write(&args.manifest, serde_json::to_vec(&changed).unwrap()).unwrap();
        changed
    }

    fn with_legacy(args: &CloseArgs, text: &str) -> CloseArgs {
        CloseArgs {
            manifest: args.manifest.clone(),
            legacy_reply_timestamp: Some(text.into()),
        }
    }

    #[test]
    fn recovery_close_accepts_only_explicit_same_instant_timestamp_text_with_audit_and_unchanged_receipt(
    ) {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, binding, _) = close_fixture(&fixture, &mut ctx);
        let changed = rehash_reply(&mut ctx, &args, &binding, LEGACY_SAME_INSTANT, |_| {});
        let manifest = fs::read(&args.manifest).unwrap();
        let posts = fixture.server.lock().unwrap().posts.len();
        let receipt_path = fixture
            .home
            .join("unattended/applied")
            .join(format!("{}.json", binding.message_id));
        let receipt = fs::read(&receipt_path).unwrap();
        // Without the explicit earlier text the retained behavior is unchanged.
        assert!(close(&mut ctx, &args).unwrap_err().contains("ReplyPending"));
        assert_eq!(patch_count(&fixture), 0);
        assert!(!fixture.home.join(EQUIVALENCE_LEDGER).exists());
        let flagged = with_legacy(&args, LEGACY_SAME_INSTANT);
        let result = close(&mut ctx, &flagged).unwrap();
        assert_eq!(result["archived"], true);
        assert_eq!(result["outcome"], "archived");
        assert_eq!(result["reply_timestamp"], "same_instant_text_equivalent");
        assert_eq!(patch_count(&fixture), 1);
        assert_eq!(fs::read(&args.manifest).unwrap(), manifest);
        assert_eq!(fs::read(&receipt_path).unwrap(), receipt);
        let saved = CloseLedger::load(&fixture.home).unwrap();
        assert_eq!(saved.records[&binding.thread_id].binding, changed);
        assert_eq!(saved.records[&binding.thread_id].phase, "verified");
        let audit = EquivalenceLedger::load(&fixture.home).unwrap();
        let evidence = &audit.records[&binding.thread_id];
        assert_eq!(evidence.legacy_timestamp, LEGACY_SAME_INSTANT);
        assert_eq!(evidence.api_timestamp, "2026-10-12T00:00:01+00:00");
        assert_eq!(evidence.instant_utc, "2026-10-12T00:00:01.000000000Z");
        assert_eq!(evidence.binding_reply_sha256, changed.reply_sha256);
        assert_ne!(evidence.api_reply_sha256, changed.reply_sha256);
        assert_eq!(evidence.message_id, binding.message_id);
        let text = fs::read_to_string(fixture.home.join(EQUIVALENCE_LEDGER)).unwrap();
        assert!(!text.contains("content") && !text.contains("\"body\""));
        // A later run of the same manifest reuses the retained evidence without the flag.
        let retry = close(&mut ctx, &args).unwrap();
        assert_eq!(retry["reused"], true);
        assert_eq!(retry["reply_timestamp"], "same_instant_text_equivalent");
        assert_eq!(patch_count(&fixture), 1);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts);
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_timestamp_text_exception_rejects_any_other_difference() {
        for scenario in [
            "different-instant",
            "different-fraction",
            "not-rfc3339",
            "body-differs",
            "author-differs",
            "todo-differs",
            "edited",
            "late-reply",
            "same-text-wrong-hash",
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let (args, binding, _) = close_fixture(&fixture, &mut ctx);
            let legacy = match scenario {
                "different-instant" => "2026-10-12T09:00:02+09:00",
                "different-fraction" => "2026-10-12T09:00:01.001+09:00",
                "not-rfc3339" => "2026-10-12 09:00:01",
                _ => LEGACY_SAME_INSTANT,
            };
            if scenario == "same-text-wrong-hash" {
                // The API text itself as "earlier text" is not an exception.
                fs::write(&args.manifest, {
                    let mut value = serde_json::to_value(&binding).unwrap();
                    value["reply_sha256"] = json!("0".repeat(64));
                    serde_json::to_vec(&value).unwrap()
                })
                .unwrap();
            } else {
                rehash_reply(&mut ctx, &args, &binding, legacy, |reply| match scenario {
                    "body-differs" => reply["body"] = json!("b"),
                    "author-differs" => reply["author_id"] = json!("999"),
                    "todo-differs" => reply["todo_id"] = json!(999),
                    _ => {}
                });
            }
            let legacy_arg = if scenario == "same-text-wrong-hash" {
                "2026-10-12T00:00:01+00:00"
            } else {
                legacy
            };
            if scenario == "edited" {
                let mut state = fixture.server.lock().unwrap();
                let reply = state
                    .messages
                    .get_mut(&binding.thread_id)
                    .unwrap()
                    .iter_mut()
                    .find(|r| r["id"] == binding.message_id)
                    .unwrap();
                reply["edited_timestamp"] = json!("2026-10-12T00:00:05Z");
            } else if scenario == "late-reply" {
                fixture.add_reply(&binding.thread_id, OWNER, "b", false);
            }
            assert!(
                close(&mut ctx, &with_legacy(&args, legacy_arg)).is_err(),
                "accepted {scenario}"
            );
            assert_eq!(patch_count(&fixture), 0, "{scenario}");
            assert!(
                !fixture.home.join(EQUIVALENCE_LEDGER).exists(),
                "{scenario}"
            );
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_close_exact_hash_ignores_the_timestamp_text_option_and_writes_no_audit() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, _, _) = close_fixture(&fixture, &mut ctx);
        let result = close(&mut ctx, &with_legacy(&args, LEGACY_SAME_INSTANT)).unwrap();
        assert_eq!(result["reply_timestamp"], "exact");
        assert!(!fixture.home.join(EQUIVALENCE_LEDGER).exists());
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_retained_equivalence_cannot_be_replaced_by_a_different_text() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, binding, _) = close_fixture(&fixture, &mut ctx);
        rehash_reply(&mut ctx, &args, &binding, LEGACY_SAME_INSTANT, |_| {});
        close(&mut ctx, &with_legacy(&args, LEGACY_SAME_INSTANT)).unwrap();
        let before = fs::read(fixture.home.join(EQUIVALENCE_LEDGER)).unwrap();
        assert!(close(&mut ctx, &with_legacy(&args, "2026-10-12T00:00:01.000Z")).is_err());
        assert_eq!(
            fs::read(fixture.home.join(EQUIVALENCE_LEDGER)).unwrap(),
            before
        );
        // The retained-record guard itself: evidence that differs in any field is refused.
        let mut other =
            EquivalenceLedger::load(&fixture.home).unwrap().records[&binding.thread_id].clone();
        other.api_reply_sha256 = "0".repeat(64);
        assert!(EquivalenceLedger::load(&fixture.home)
            .unwrap()
            .record(&fixture.home, &other)
            .is_err());
        assert_eq!(
            fs::read(fixture.home.join(EQUIVALENCE_LEDGER)).unwrap(),
            before
        );
    }

    #[test]
    fn recovery_close_archives_one_and_retry_preserves_old_files_and_does_not_post_or_dispatch() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, binding, _) = close_fixture(&fixture, &mut ctx);
        let posts = fixture.server.lock().unwrap().posts.len();
        let before = fs::read(fixture.home.join(LEDGER)).unwrap();
        let result = close(&mut ctx, &args).unwrap();
        assert_eq!(result["archived"], true);
        assert_eq!(result["verification"], "GET");
        assert_eq!(result["outcome"], "archived");
        assert_eq!(result["thread_id"], binding.thread_id);
        assert_eq!(result["revision"], binding.revision);
        assert_eq!(result["reused"], false);
        let retry = close(&mut fixture.context(), &args).unwrap();
        assert_eq!(retry["reused"], true);
        assert_eq!(retry["outcome"], result["outcome"]);
        assert_eq!(patch_count(&fixture), 1);
        let state = fixture.server.lock().unwrap();
        assert_eq!(state.posts.len(), posts);
        assert_eq!(state.forbidden_effects, 0);
        assert_eq!(
            state.calls.iter().find(|(m, _, _)| m == "PATCH").unwrap().2,
            Some(json!({"archived":true}))
        );
        drop(state);
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_lost_response_get_recovery_and_preexisting_archive_are_distinct() {
        for scenario in ["lost-response", "interrupted-get", "preexisting"] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let (args, binding, _) = close_fixture(&fixture, &mut ctx);
            {
                let mut state = fixture.server.lock().unwrap();
                if scenario == "preexisting" {
                    state.channels.get_mut(&binding.thread_id).unwrap()["thread_metadata"]
                        ["archived"] = json!(true);
                } else {
                    state.close_drop_response = true;
                    state.close_block_reads = scenario == "interrupted-get";
                }
            }
            if scenario == "interrupted-get" {
                assert!(close(&mut ctx, &args).is_err());
                assert_eq!(
                    CloseLedger::load(&fixture.home).unwrap().records[&binding.thread_id].phase,
                    "patch_pending"
                );
                fixture.server.lock().unwrap().blocked = false;
            }
            let result = close(&mut fixture.context(), &args).unwrap();
            assert_eq!(
                result["outcome"],
                if scenario == "preexisting" {
                    "already_archived"
                } else {
                    "recovered_after_patch"
                }
            );
            assert_eq!(
                patch_count(&fixture),
                usize::from(scenario != "preexisting")
            );
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_close_wrong_binding_and_unapplied_receipt_are_rejected_before_patch() {
        for field in [
            "request_id",
            "thread_id",
            "question_message_id",
            "origin_todo_id",
            "todo_id",
            "message_id",
            "author_id",
            "job_id",
            "revision",
            "question_fingerprint",
            "reply_sha256",
            "receipt_sha256",
            "verification_sha256",
            "completion",
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let (args, binding, _) = close_fixture(&fixture, &mut ctx);
            let mut value = serde_json::to_value(&binding).unwrap();
            value[field] = match field {
                "origin_todo_id" | "todo_id" => json!(999),
                "revision" => json!("0".repeat(40)),
                "question_fingerprint"
                | "reply_sha256"
                | "receipt_sha256"
                | "verification_sha256" => json!("0".repeat(64)),
                _ => json!("999"),
            };
            fs::write(&args.manifest, serde_json::to_vec(&value).unwrap()).unwrap();
            assert!(close(&mut ctx, &args).is_err(), "accepted wrong {field}");
            assert_eq!(patch_count(&fixture), 0);
            fixture.unchanged();
        }
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, binding, _) = close_fixture(&fixture, &mut ctx);
        fs::remove_file(
            fixture
                .home
                .join("unattended/applied")
                .join(format!("{}.json", binding.message_id)),
        )
        .unwrap();
        assert!(close(&mut ctx, &args).is_err());
        assert_eq!(patch_count(&fixture), 0);
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_remote_owner_bot_forum_generation_changes_prevent_patch() {
        for scenario in [
            "owner",
            "bot",
            "forum",
            "guild",
            "parent",
            "starter",
            "reply-body",
            "edited",
            "late-reply",
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let (args, binding, _) = close_fixture(&fixture, &mut ctx);
            if scenario == "owner" {
                ctx.cfg["owner_user_id"] = json!("999");
            } else if scenario == "late-reply" {
                fixture.add_reply(&binding.thread_id, OWNER, "b", false);
            } else {
                let mut state = fixture.server.lock().unwrap();
                match scenario {
                    "bot" => state.bot_override = Some("999".into()),
                    "forum" => state.channels.get_mut(FORUM).unwrap()["type"] = json!(0),
                    "guild" => {
                        state.channels.get_mut(&binding.thread_id).unwrap()["guild_id"] =
                            json!("999")
                    }
                    "parent" => {
                        state.channels.get_mut(&binding.thread_id).unwrap()["parent_id"] =
                            json!("999")
                    }
                    "starter" => {
                        state.messages.get_mut(&binding.thread_id).unwrap()[0]["content"] =
                            json!("edited question")
                    }
                    "reply-body" | "edited" => {
                        let reply = state
                            .messages
                            .get_mut(&binding.thread_id)
                            .unwrap()
                            .iter_mut()
                            .find(|r| r["id"] == binding.message_id)
                            .unwrap();
                        reply[if scenario == "edited" {
                            "edited_timestamp"
                        } else {
                            "content"
                        }] = json!("2026-10-12T00:00:03Z");
                    }
                    _ => unreachable!(),
                }
            }
            assert!(close(&mut ctx, &args).is_err(), "accepted {scenario}");
            assert_eq!(patch_count(&fixture), 0);
            fixture.unchanged();
            if matches!(scenario, "reply-body" | "edited" | "late-reply") {
                let saved = CloseLedger::load(&fixture.home).unwrap();
                assert_eq!(saved.records[&binding.thread_id].phase, "reply_pending");
                assert!(!saved.records[&binding.thread_id]
                    .pending_reply_ids
                    .is_empty());
                assert!(!fs::read_to_string(fixture.home.join(CLOSE_LEDGER))
                    .unwrap()
                    .contains("edited question"));
            }
        }
    }

    #[test]
    fn recovery_close_get_false_never_reports_success_and_absent_legacy_state_stays_absent() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, _, _) = close_fixture(&fixture, &mut ctx);
        fixture.server.lock().unwrap().close_reject_patch = true;
        assert!(close(&mut ctx, &args)
            .unwrap_err()
            .contains("GET has not confirmed"));
        assert_eq!(patch_count(&fixture), 1);
        fixture.unchanged();
        fs::remove_file(fixture.home.join("state.json")).unwrap();
        fs::remove_file(fixture.home.join("question-recovery-required.json")).unwrap();
        fixture.server.lock().unwrap().close_reject_patch = false;
        assert_eq!(close(&mut ctx, &args).unwrap()["archived"], true);
        assert!(!fixture.home.join("state.json").exists());
        assert!(!fixture
            .home
            .join("question-recovery-required.json")
            .exists());
    }

    #[test]
    fn recovery_close_live_lock_and_corrupt_record_do_not_allow_patch() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, _, _) = close_fixture(&fixture, &mut ctx);
        let lock = ByteLock::acquire(&fixture.home.join(CLOSE_LOCK), Duration::ZERO).unwrap();
        assert!(close(&mut ctx, &args).is_err());
        assert_eq!(patch_count(&fixture), 0);
        drop(lock);
        fs::write(fixture.home.join(CLOSE_LEDGER), b"{broken").unwrap();
        assert!(close(&mut ctx, &args).is_err());
        assert_eq!(patch_count(&fixture), 0);
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_reply_arriving_during_patch_is_retained_without_success_or_reopen() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, binding, _) = close_fixture(&fixture, &mut ctx);
        fixture.server.lock().unwrap().inject_owner_reply =
            Some((binding.thread_id.clone(), "b: 追記".into()));
        assert!(close(&mut ctx, &args).unwrap_err().contains("ReplyPending"));
        assert_eq!(patch_count(&fixture), 1);
        let saved = CloseLedger::load(&fixture.home).unwrap();
        assert_eq!(saved.records[&binding.thread_id].phase, "reply_pending");
        assert_eq!(saved.records[&binding.thread_id].pending_reply_ids.len(), 1);
        assert!(saved.records[&binding.thread_id].patch_attempted);
        assert_eq!(
            fixture.server.lock().unwrap().channels[&binding.thread_id]["thread_metadata"]
                ["archived"],
            true
        );
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_verified_then_pending_reply_preserves_outcome_and_reopen_is_rejected() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, binding, _) = close_fixture(&fixture, &mut ctx);
        assert_eq!(close(&mut ctx, &args).unwrap()["outcome"], "archived");
        let added = fixture.add_reply(
            &binding.thread_id,
            OWNER,
            "additional unapplied reply",
            false,
        );
        assert!(close(&mut ctx, &args).unwrap_err().contains("ReplyPending"));
        let saved = CloseLedger::load(&fixture.home).unwrap();
        assert_eq!(
            saved.records[&binding.thread_id].outcome.as_deref(),
            Some("archived")
        );
        assert!(saved.records[&binding.thread_id].patch_attempted);
        {
            let mut server = fixture.server.lock().unwrap();
            server
                .messages
                .get_mut(&binding.thread_id)
                .unwrap()
                .retain(|m| m["id"] != added);
        }
        assert_eq!(close(&mut ctx, &args).unwrap()["outcome"], "archived");
        fixture
            .server
            .lock()
            .unwrap()
            .channels
            .get_mut(&binding.thread_id)
            .unwrap()["thread_metadata"]["archived"] = json!(false);
        assert!(close(&mut ctx, &args).unwrap_err().contains("reopened"));
        assert_eq!(patch_count(&fixture), 1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_ignores_nonowner_messages_but_does_not_trust_success_response_or_changed_retry(
    ) {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let (args, binding, _) = close_fixture(&fixture, &mut ctx);
        fixture.add_reply(&binding.thread_id, "999", "unrelated reply", false);
        fixture.add_reply(&binding.thread_id, OWNER, "bot is not owner evidence", true);
        fixture.server.lock().unwrap().close_no_effect = true;
        assert!(close(&mut ctx, &args)
            .unwrap_err()
            .contains("GET has not confirmed"));
        fixture.server.lock().unwrap().close_no_effect = false;
        assert_eq!(close(&mut ctx, &args).unwrap()["archived"], true);
        let mut altered = binding;
        altered.job_id = "another-parent".into();
        fs::write(&args.manifest, serde_json::to_vec(&altered).unwrap()).unwrap();
        assert!(close(&mut ctx, &args)
            .unwrap_err()
            .contains("binding differs"));
        assert_eq!(patch_count(&fixture), 2);
        fixture.unchanged();
    }

    #[test]
    fn recovery_close_applied_receipt_requires_execution_identity_and_origin_verification() {
        for key in ["execution", "identity", "origin_saved"] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let (args, mut binding, mut application) = close_fixture(&fixture, &mut ctx);
            application["verification"]
                .as_object_mut()
                .unwrap()
                .remove(key);
            let bytes = serde_json::to_vec(&application).unwrap();
            binding.receipt_sha256 = format!("{:x}", Sha256::digest(&bytes));
            binding.verification_sha256 = digest(&application["verification"]).unwrap();
            fs::write(
                fixture
                    .home
                    .join("unattended/applied")
                    .join(format!("{}.json", binding.message_id)),
                bytes,
            )
            .unwrap();
            fs::write(&args.manifest, serde_json::to_vec(&binding).unwrap()).unwrap();
            assert!(close(&mut ctx, &args).is_err(), "missing {key}");
            assert_eq!(patch_count(&fixture), 0);
            fixture.unchanged();
        }
    }

    #[test]
    fn recovery_close_reads_existing_origin_corrected_without_changing_ledger_or_scope() {
        for scenario in [
            "valid",
            "body_changed",
            "unrelated_progress",
            "post_reserved",
        ] {
            let fixture = Fixture::new();
            let mut ctx = fixture.context();
            let (args, binding, _) = close_fixture(&fixture, &mut ctx);
            let mut ledger = Ledger::load(&fixture.home).unwrap();
            let progress = ledger
                .receipt(&binding.request_id, "progress")
                .unwrap()
                .thread_id
                .clone();
            let mut original = prepare_ask(
                &ctx,
                &fixture.ask(
                    "old-runtime-unsent",
                    "別の未送信の質問です。\na 保持\nb 保留",
                ),
            )
            .unwrap();
            let Kind::Ask {
                origin_thread,
                decision_original,
                ..
            } = &mut original.operation
            else {
                unreachable!()
            };
            *origin_thread = binding.thread_id.clone();
            decision_original["origin_thread_id"] = json!(binding.thread_id);
            original.fingerprint = original.fingerprint().unwrap();
            ledger
                .append(
                    &fixture.home,
                    Event::Intent {
                        request: Box::new(original.clone()),
                    },
                )
                .unwrap();
            ledger
                .append(
                    &fixture.home,
                    Event::Bot {
                        request_id: original.request_id.clone(),
                        author_id: BOT.into(),
                    },
                )
                .unwrap();
            if scenario == "post_reserved" {
                ledger
                    .append(
                        &fixture.home,
                        Event::Pending {
                            request_id: original.request_id.clone(),
                            phase: "question:0".into(),
                            effect: Effect {
                                target: Target::Forum {
                                    title: original.title.clone(),
                                },
                                content: original.body.clone(),
                                notify: true,
                                images: vec![],
                            },
                        },
                    )
                    .unwrap();
            }
            let mut corrected = original;
            let Kind::Ask {
                origin_thread,
                decision_original,
                ..
            } = &mut corrected.operation
            else {
                unreachable!()
            };
            *origin_thread = if scenario == "unrelated_progress" {
                "106".into()
            } else {
                progress
            };
            decision_original["origin_thread_id"] = json!(origin_thread);
            if scenario == "body_changed" {
                corrected.body = "changed scope".into();
            }
            corrected.fingerprint = corrected.fingerprint().unwrap();
            if scenario == "valid" {
                ledger
                    .append(
                        &fixture.home,
                        Event::OriginCorrected {
                            request: Box::new(corrected),
                        },
                    )
                    .unwrap();
                let before = fs::read(fixture.home.join(LEDGER)).unwrap();
                assert_eq!(close(&mut ctx, &args).unwrap()["archived"], true);
                assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            } else {
                ledger.events.push(Event::OriginCorrected {
                    request: Box::new(corrected),
                });
                fs::write(
                    fixture.home.join(LEDGER),
                    serde_json::to_vec(&ledger).unwrap(),
                )
                .unwrap();
                assert!(close(&mut ctx, &args).is_err());
                assert_eq!(patch_count(&fixture), 0);
            }
            fixture.unchanged();
        }
    }
}
