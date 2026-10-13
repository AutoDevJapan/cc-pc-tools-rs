//! New, explicitly requested owner messages while the original question ledger is lost.
//!
//! This is a separate message domain, not a recovery of state.json. It never loads,
//! changes, unlocks or dispatches that ledger. HTTP writes are limited to a newly
//! reserved forum post, its verified recovery threads and an explicitly supplied
//! owner request thread. No scheduler, model, subprocess, PATCH or DELETE is used.
//!
//! Storage consists only of recovery-messages.json, recovery-messages.lock and
//! recovery-messages.*.tmp beside them. The JSON contains append-only events;
//! each snapshot is flushed and atomically replaced under the dedicated byte lock.
//! Token/config contents and attachment paths/bytes are never receipt fields.
use crate::core::{self, ByteLock, Context, MAX_CONTENT, Result, SILENT};
use clap::Args;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    fs,
    io::{Read, Write},
    path::{Path, PathBuf},
    time::Duration,
};

const API: &str = "https://discord.com/api/v10";
const LEDGER: &str = "recovery-messages.json";
const LOCK: &str = "recovery-messages.lock";
const DOMAIN: &str = "new-owner-recovery-messages";
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
        self.events.iter().find_map(|event| match event {
            Event::Intent { request } if request.request_id == canonical => Some(request.as_ref()),
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

fn images(ctx: &Context, paths: &[PathBuf], secrets: &SecretCheck) -> Result<Vec<Image>> {
    if paths.is_empty() {
        return Ok(vec![]);
    }
    if paths.len() > 10 {
        return Err("at most ten recovery images are supported".into());
    }
    // This deny list is read only into memory; never returned or copied to receipts.
    let clients = fs::read_to_string(&ctx.client_projects)
        .map_err(|_| "image protection list is unavailable; no image is sent")?;
    let denied: Vec<_> = clients
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty() && !line.starts_with('#'))
        .map(|line| line.replace('\\', "/").to_lowercase())
        .collect();
    let mut result = Vec::new();
    let mut total = 0;
    for path in paths {
        let resolved = fs::canonicalize(path).map_err(|_| "cannot resolve recovery image")?;
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
        let file = fs::File::open(resolved).map_err(|_| "cannot read recovery image")?;
        if !file
            .metadata()
            .map_err(|_| "cannot inspect recovery image")?
            .is_file()
        {
            return Err("a recovery attachment must be a regular image file".into());
        }
        let bytes = read_bounded(file, IMAGE_BYTES)?;
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
        same.then(|| request.as_ref().clone())
    });
    if let Some(request) = duplicate {
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
        if ledger.bot(&request.request_id).is_none() {
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
                format!("&before={}",crate::history_audit::query_component(&before))
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
                format!("&before={}",crate::history_audit::query_component(&before))
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
    // A failed preflight GET cannot have posted anything. Intent/Bot are already
    // durable; reserve an uncertain delivery only after GET succeeds, before POST.
    let candidates = api.candidates(&effect)?;
    let selected = match candidates.as_slice() {
        [one] => Some(one.clone()),
        [] if recovering => return Err(PENDING.into()),
        [] => None,
        _ => return Err("RecoveryMessagePending: duplicate matching bot messages require inspection; no POST is retried".into()),
    };
    if !recovering {
        ledger.append(home,Event::Pending {request_id:id.clone(),phase:phase.into(),effect:effect.clone()})?;
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

fn prepare_log(ctx: &Context, args: &LogArgs) -> Result<(Request, Vec<Image>)> {
    request_key(&args.request_id)?;
    let secrets = SecretCheck::new();
    let body = text_input(&args.body_file, &secrets)?;
    no_mentions(&body)?;
    let images = images(ctx, &args.attach, &secrets)?;
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
        "thread_id":start.map(|r| &r.thread_id),"message_id":start.map(|r| &r.message_id),
        "url":start.map(|r| request.destination.url(&r.thread_id)),
        "progress_thread_id":progress.map(|r| &r.thread_id),
        "progress_message_id":progress.map(|r| &r.message_id),
        "progress_url":progress.map(|r| request.destination.url(&r.thread_id)),
        "original_question":if matches!(request.operation, Kind::Ask { .. }) {Some(&request.body)} else {None},
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

/// The original decision scope and question text are retained even when another
/// title/request ID is a semantic duplicate. Extra question text goes to progress.
pub fn ask(ctx: &mut Context, args: &AskArgs) -> Result<Value> {
    let incoming = prepare_ask(ctx, args)?;
    execute(ctx, incoming, &[])
}

pub fn resume_ask(ctx: &mut Context, args: &ResumeAskArgs) -> Result<Value> {
    request_key(&args.request_id)?;
    if args.todo_id == 0 {
        return Err("original ToDo ID is required".into());
    }
    execute_saved(ctx, &args.request_id, args.todo_id)
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
    execute_inner(ctx, Some(incoming), None, images)
}

fn execute_saved(ctx: &mut Context, request_id: &str, todo_id: u64) -> Result<Value> {
    execute_inner(ctx, None, Some((request_id, todo_id)), &[])
}

fn execute_inner(
    ctx: &mut Context,
    incoming: Option<Request>,
    saved: Option<(&str, u64)>,
    images: &[Image],
) -> Result<Value> {
    let home = ctx.here.clone();
    ensure_storage(&home)?;
    let _lock = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
    let mut ledger = Ledger::load(&home)?;
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
            request
        }
        _ => return Err("exact recovery ask input is required".into()),
    };
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
            if let Some(thread) = thread {
                let is_progress =
                    RecoveryApi::known_progress(&ledger, &api.destination, &api.bot, thread)
                        .ok_or(
                            "explicit log thread has no verified independent recovery receipt",
                        )?;
                if is_progress && request.notify {
                    return Err("recovery progress posts must stay silent".into());
                }
                api.assert_bot_thread(thread)?;
            }
            let mut destination = thread.clone();
            for (index, body) in text.iter().enumerate() {
                let phase = format!("log:{index}");
                let target = match &destination {
                    Some(thread) => Target::Thread {
                        thread: thread.clone(),
                    },
                    None => Target::Forum {
                        title: request.title.clone(),
                    },
                };
                let receipt = ensure(
                    &mut api,
                    &mut ledger,
                    &home,
                    &request,
                    &phase,
                    effect(
                        &request,
                        &phase,
                        target,
                        body,
                        index == 0 && request.notify,
                        index == 0,
                    )?,
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
                    false,
                )?,
                &[],
            )?;
            let bridge = format!(
                "元の投稿: <{}>\n質問: <{}>\n以後の進捗と結果はこの投稿へ記録します。",
                request.destination.url(origin_thread),
                request.destination.url(&question.thread_id)
            );
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
            for (index, body) in text.iter().enumerate().skip(1) {
                let phase = format!("question-continuation:{index}");
                ensure(
                    &mut api,
                    &mut ledger,
                    &home,
                    &request,
                    &phase,
                    effect(
                        &request,
                        &phase,
                        Target::Thread {
                            thread: progress.thread_id.clone(),
                        },
                        body,
                        false,
                        false,
                    )?,
                    &[],
                )?;
            }
            let question_links = format!(
                "元の投稿: <{}>\n進捗と本文の続き: <{}>",
                request.destination.url(origin_thread),
                request.destination.url(&progress.thread_id)
            );
            ensure(
                &mut api,
                &mut ledger,
                &home,
                &request,
                "question-links",
                effect(
                    &request,
                    "question-links",
                    Target::Thread {
                        thread: question.thread_id.clone(),
                    },
                    &question_links,
                    false,
                    false,
                )?,
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
    Ok(
        json!({"domain":DOMAIN,"read_only":true,"ledger_written":false,"legacy_state":"unchanged",
        "thread_id":thread,"request_id":request.request_id,"original_question":request.body,
        "source":"discord-api-v10","owner_id":request.destination.owner,
        "question_message_id":ledger.receipt(&request.request_id,"question:0").map(|r|&r.message_id),
        "question_timestamp":ledger.receipt(&request.request_id,"question:0").map(|r|&r.timestamp),
        "decision_identity":match &request.operation {Kind::Ask{decision_identity,..}=>Some(decision_identity),_=>None},
        "progress_thread_id":ledger.receipt(&request.request_id,"progress").map(|r|&r.thread_id),
        "original_decision":match &request.operation {Kind::Ask {decision_original,..}=>Some(decision_original),_=>None},
        "owner_replies":evidence,"delivery":"inspection_only","approval":"not_inferred"}),
    )
}

pub(crate) fn validate_followup_text(text: &str) -> Result<()> {
    SecretCheck::new().text(text)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::{Effects, Output};
    use std::sync::{Arc, Mutex, mpsc};

    const GUILD: &str = "102";
    const FORUM: &str = "103";
    const OWNER: &str = "100";
    const BOT: &str = "101";
    const REQUEST_FORUM: &str = "104";
    const ORIGIN: &str = "105";

    #[derive(Default)]
    struct Server {
        channels: BTreeMap<String, Value>,
        messages: BTreeMap<String, Vec<Value>>,
        calls: Vec<(String, String, Option<Value>)>,
        posts: Vec<(String, Value)>,
        next: u64,
        drop_response: bool,
        reject_post: bool,
        block_reads_after_post: bool,
        fail_inventory_once: bool,
        archive_cursor_page: bool,
        blocked: bool,
        wrong_flags: bool,
        wrong_mentions: bool,
        forbidden_effects: usize,
        network_without_intent: usize,
        posts_without_pending: usize,
        multipart_uploads: usize,
        inject_owner_reply: Option<(String, String)>,
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
            if method == "POST" {
                if let Some((thread,content))=state.inject_owner_reply.take() {
                    state.next+=1;
                    let id=state.next.to_string();
                    state.messages.entry(thread.clone()).or_default().push(json!({"id":id,"channel_id":thread,
                        "author":{"id":OWNER,"bot":false},"content":content,"type":0,"timestamp":"2026-10-12T00:00:03Z"}));
                }
            }
            state
                .calls
                .push((method.into(), path.into(), body.cloned()));
            if !discord || !matches!(method, "GET" | "POST") {
                state.forbidden_effects += 1;
                return Err("unexpected recovery effect".into());
            }
            self.check_pending(&mut state, method, body);
            if method == "GET" && state.blocked {
                return Err("simulated interrupted GET".into());
            }
            if method=="GET"&&path.ends_with("/threads/active")&&state.fail_inventory_once {
                state.fail_inventory_once=false;
                return Err("simulated preflight inventory GET failure".into());
            }
            if method == "GET" && path == "/users/@me" {
                return Ok(json!({"id":BOT,"bot":true}));
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
                    if !path.contains("%2B00%3A00")||path.contains("+00:00") {
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

    #[test]
    fn recovery_messages_notify_prefix_owner_and_silent_defaults_are_get_verified() {
        let fixture = Fixture::new();
        let args = fixture.log(
            "status-2472",
            "【速報】作業完了のお知らせ\n指定された一覧の修正が終わりました。",
            true,
        );
        let mut ctx = fixture.context();
        let out = log(&mut ctx, &args).unwrap();
        assert_eq!(out["delivery"], "verified");
        assert_eq!(out["new_pending_effects"], 0);
        assert_eq!(out["legacy_pending"], "unknown");
        let state = fixture.server.lock().unwrap();
        let messages = &state.messages[core::s(&out["thread_id"])];
        assert!(core::s(&messages[0]["content"]).starts_with("【速報】作業完了のお知らせ"));
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
        assert!(
            ctx.call(
                "POST",
                "/channels/106/messages",
                Some(&json!({"content":"legacy"}))
            )
            .is_err()
        );
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls);
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
        let fixture=Fixture::new();
        fixture.server.lock().unwrap().fail_inventory_once=true;
        let args=fixture.log("preflight-failure","管理画面の修復を再開しています。",false);
        let mut ctx=fixture.context();
        assert!(log(&mut ctx,&args).is_err());
        assert!(fixture.server.lock().unwrap().posts.is_empty());
        assert!(Ledger::load(&fixture.home).unwrap().effect(&args.request_id,"log:0").is_none());
        assert_eq!(log(&mut ctx,&args).unwrap()["delivery"],"verified");
        assert_eq!(fixture.server.lock().unwrap().posts.len(),1);
        assert_eq!(log(&mut ctx,&args).unwrap()["delivery"],"verified");
        assert_eq!(fixture.server.lock().unwrap().posts.len(),1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_messages_archive_cursor_encodes_timezone_plus() {
        let fixture=Fixture::new();
        fixture.server.lock().unwrap().archive_cursor_page=true;
        let args=fixture.log("archive-cursor","指定一覧を反映しました。",false);
        let mut ctx=fixture.context();
        assert_eq!(log(&mut ctx,&args).unwrap()["delivery"],"verified");
        let state=fixture.server.lock().unwrap();
        assert!(state.calls.iter().any(|(_,path,_)|path.contains("before=")&&path.contains("%2B00%3A00")));
        assert_eq!(state.posts.len(),1);
        drop(state);
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
    fn recovery_messages_long_owner_question_continues_only_in_silent_progress() {
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
        assert_eq!(question.len(), 2); // starter and the silent link only
        assert!(progress.len() > 2);
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
        assert!(core::s(&question[1]["content"]).contains(core::s(&out["progress_url"])));
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
            ..fixture.log("forbidden-progress-notify", "通知", true)
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
        assert!(
            !Ledger::load(&fixture.home)
                .unwrap()
                .complete(&args.request_id)
        );
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
        fs::write(&ctx.todo,serde_json::to_vec(&json!({"items":[{"id":2471,"jobs":[],"note":"元の作業本文"},
            {"id":2472,"status":"done","progress":100}]})).unwrap()).unwrap();
        let config = fixture.root.path().join("followup.json");
        fs::write(&config, serde_json::to_vec(&followup::Watch { request_id:args.request_id,
            thread:thread.into(),question_message:core::s(&question["message_id"]).into(),todo_id:2472,work_todo:2471,
            parent_lock:fixture.root.path().join("parent.active"),worker_cwd:fixture.root.path().to_path_buf(),
            parent_messages:vec![reply.clone()] }).unwrap()).unwrap();
        let registered=followup::register(&mut ctx,&config).unwrap();
        assert_eq!(registered["scheduled_observations"],0);
        for _ in 0..2 { followup::tick(&mut ctx).unwrap(); }
        let observed=followup::inspect(&ctx).unwrap();
        assert_eq!(observed["replies"].as_array().unwrap().len(),1);
        assert_eq!(observed["scheduled_observations"],2);
        assert_eq!(observed["replies"][0]["phase"],"parent_pending");
        let state_path=fixture.home.join("recovery-followup.json");
        let saved=fs::read(&state_path).unwrap();
        let mut wrong:Value=serde_json::from_slice(&saved).unwrap();
        wrong["replies"][&reply]["steering_handler"]=json!("unrelated-test-job");
        fs::write(&state_path,serde_json::to_vec(&wrong).unwrap()).unwrap();
        let before_claim=fs::read(&state_path).unwrap();
        assert!(followup::claim(&mut ctx,&reply,"unrelated-test-job").unwrap_err().contains("claimant"));
        assert_eq!(fs::read(&state_path).unwrap(),before_claim);
        fs::write(&state_path,&saved).unwrap();
        let result=fixture.root.path().join("applied.json");
        fs::write(&result,serde_json::to_vec(&json!({"handler":"parent","revision":"349ed9ccf7c3ee1699601d749b1a2b5f7504c73a",
            "verification":"表示の17検査成功","summary":"指定外を別欄に残しました。","applied_at":"2026-10-12T00:00:02+00:00"})).unwrap()).unwrap();
        fixture.server.lock().unwrap().block_reads_after_post=true;
        let posts_before=fixture.server.lock().unwrap().posts.len();
        assert!(followup::applied(&mut ctx,&reply,&result).is_err());
        assert_eq!(followup::inspect(&ctx).unwrap()["replies"][0]["phase"],"result_pending");
        {
            let mut server=fixture.server.lock().unwrap(); server.blocked=false;server.block_reads_after_post=false;
        }
        let mut restarted=fixture.context();
        let finished=followup::applied(&mut restarted,&reply,&result).unwrap();
        assert_eq!(finished["replies"][0]["phase"],"applied");
        followup::applied(&mut restarted,&reply,&result).unwrap();
        followup::tick(&mut restarted).unwrap();
        assert_eq!(fixture.server.lock().unwrap().posts.len(),posts_before+1);
        fixture.unchanged();
    }

    #[test]
    fn recovery_followup_reply_arriving_during_result_post_prevents_decision_done() {
        use crate::recovery_followup as followup;
        let fixture=Fixture::new();
        let args=fixture.ask("followup-late","a: 別欄で保持\nb: 部署へ追加\n");
        let mut decision=core::read_json(&args.decision_file).unwrap();decision["scope"]["original_work_todo"]=json!(2471);
        fs::write(&args.decision_file,serde_json::to_vec(&decision).unwrap()).unwrap();
        let mut ctx=fixture.context();let question=ask(&mut ctx,&args).unwrap();
        let thread=core::s(&question["thread_id"]);
        let mid=fixture.add_reply(thread,OWNER,"a",false);
        fs::write(&ctx.todo,serde_json::to_vec(&json!({"items":[{"id":2471,"jobs":[]},{"id":2472,"status":"block"}]})).unwrap()).unwrap();
        let config=fixture.root.path().join("watch-late.json");
        fs::write(&config,serde_json::to_vec(&followup::Watch{request_id:args.request_id,thread:thread.into(),
            question_message:core::s(&question["message_id"]).into(),todo_id:2472,work_todo:2471,
            parent_lock:fixture.root.path().join("parent-late.active"),worker_cwd:fixture.root.path().to_path_buf(),parent_messages:vec![mid.clone()]}).unwrap()).unwrap();
        followup::register(&mut ctx,&config).unwrap();followup::tick(&mut ctx).unwrap();
        let result=fixture.root.path().join("result-late.json");
        fs::write(&result,serde_json::to_vec(&json!({"handler":"parent","revision":"349ed9ccf7c3ee1699601d749b1a2b5f7504c73a",
            "verification":"表示の17検査成功","summary":"別欄へ反映しました。","applied_at":"2026-10-12T00:00:02Z"})).unwrap()).unwrap();
        fixture.server.lock().unwrap().inject_owner_reply=Some((thread.into(),"b: 登録番号の指定を追記します".into()));
        let out=followup::applied(&mut ctx,&mid,&result).unwrap();
        assert_eq!(out["replies"].as_array().unwrap().len(),2);
        assert_eq!(out["replies"][0]["phase"],"applied");assert_eq!(out["replies"][1]["phase"],"reserved");
        assert_eq!(core::read_json(&ctx.todo).unwrap()["items"][1]["status"],"block");
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
        let args = fixture.ask(
            id,
            "端末の承認をお願いします。\nGoogle Workspaceの指定アカウントで続行します。",
        );
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
    fn exact_unsent_origin_repair_posts_once_then_get_verifies_question_and_progress() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source = verified_progress_source(&fixture, &mut ctx, "verified-progress-source", "107", 2472);
        let _args = failed_unsent_ask(&fixture, "one-device-approval", "107", OWNER);
        let before_unrepaired_resume = fs::read(fixture.home.join(LEDGER)).unwrap();
        let posts_before_unrepaired_resume = fixture.server.lock().unwrap().posts.len();
        assert!(resume_ask(
            &mut ctx,
            &ResumeAskArgs {
                request_id: "one-device-approval".into(),
                todo_id: 2472,
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
        assert_ne!(repaired["original_fingerprint"], repaired["repaired_fingerprint"]);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before);
        let repaired_bytes = fs::read(fixture.home.join(LEDGER)).unwrap();
        let calls_after_repair = fixture.server.lock().unwrap().calls.len();
        let repeated = repair_origin(&mut ctx, &repair).unwrap();
        assert_eq!(repeated["repair"], "already_recorded");
        assert_eq!(repeated["repaired_fingerprint"], repaired["repaired_fingerprint"]);
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), repaired_bytes);
        let different_repair = repair_args("one-device-approval", "107", "9999");
        assert!(repair_origin(&mut ctx, &different_repair).is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), repaired_bytes);
        assert_eq!(fixture.server.lock().unwrap().calls.len(), calls_after_repair);
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
            },
        )
        .is_err());
        assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before_resume);
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before_resume);

        let out = resume_ask(
            &mut ctx,
            &ResumeAskArgs {
                request_id: "one-device-approval".into(),
                todo_id: 2472,
            },
        )
        .unwrap();
        assert_eq!(out["delivery"], "verified");
        assert_eq!(out["original_fingerprint"], original["request"]["fingerprint"]);
        assert_ne!(out["fingerprint"], out["original_fingerprint"]);
        assert_eq!(out["fingerprint"], repaired["repaired_fingerprint"]);
        assert!(!out["thread_id"].as_str().unwrap().is_empty());
        assert!(
            out["url"]
                .as_str()
                .unwrap()
                .contains(core::s(&out["thread_id"]))
        );
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
            },
        )
        .unwrap();
        assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_after);
        assert!(
            fixture
                .server
                .lock()
                .unwrap()
                .calls
                .iter()
                .any(|(method, path, _)| method == "GET"
                    && path.ends_with(core::s(&out["message_id"])))
        );
        fixture.unchanged();
    }

    #[test]
    fn resumed_repaired_ask_recovers_lost_post_response_without_duplicate_notification() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source = verified_progress_source(&fixture,&mut ctx,"progress-lost-post","107",2472);
        let _ = failed_unsent_ask(&fixture,"lost-post-target","107",OWNER);
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
        let resume = ResumeAskArgs { request_id:"lost-post-target".into(), todo_id:2472 };
        assert!(resume_ask(&mut ctx, &resume).is_err());
        let pending: Value = serde_json::from_slice(&fs::read(fixture.home.join(LEDGER)).unwrap()).unwrap();
        assert!(pending["events"].as_array().unwrap().iter().any(|event|
            event["event"] == "pending" && event["request_id"] == "lost-post-target"
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
        let question_posts: Vec<_> = state.posts.iter().filter(|(_, body)|
            body["message"]["content"].as_str().is_some_and(|content|
                content.contains("端末の承認をお願いします。"))).collect();
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
            let source = verified_progress_source(&fixture,&mut ctx,&format!("progress-{case}"),"107",2472);
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
        let source = verified_progress_source(&fixture,&mut ctx,"progress-target-guard","107",2472);
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
            let _ = failed_unsent_ask(
                &fixture,
                &format!("repair-{case}"),
                "107",
                OWNER,
            );
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
        let source_effect = ledger.effect(&source_request.request_id, "progress").unwrap().clone();
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
            .append(&fixture.home, Event::Intent { request: Box::new(duplicate.clone()) })
            .unwrap();
        ledger
            .append(&fixture.home, Event::Bot { request_id: duplicate.request_id.clone(), author_id: source_bot })
            .unwrap();
        ledger
            .append(&fixture.home, Event::Pending {
                request_id: duplicate.request_id.clone(), phase: "progress".into(), effect: source_effect,
            })
            .unwrap();
        ledger
            .append(&fixture.home, Event::Posted {
                request_id: duplicate.request_id.clone(), phase: "progress".into(), thread_id, message_id,
            })
            .unwrap();
        ledger
            .append(&fixture.home, Event::Verified {
                request_id: duplicate.request_id, phase: "progress".into(), receipt,
            })
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
                    Event::Bot { request_id, author_id }
                        if mismatch == "bot" && request_id == source_id =>
                    {
                        *author_id = "999".into();
                    }
                    Event::Verified { request_id, receipt, .. }
                        if mismatch == "bot" && request_id == source_id =>
                    {
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
            let repair = repair_args(
                &request_id,
                "107",
                core::s(&source["progress_message_id"]),
            );
            assert!(repair_origin(&mut ctx, &repair).is_err());
            assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            assert_eq!(fixture.server.lock().unwrap().calls.len(), calls_before);
        }
    }

    #[test]
    fn repair_event_cannot_be_attached_after_any_send_reservation() {
        let fixture = Fixture::new();
        let mut ctx = fixture.context();
        let source = verified_progress_source(&fixture,&mut ctx,"progress-posted-guard","107",2472);
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
        let source = verified_progress_source(&fixture,&mut ctx,"progress-validation","107",2472);
        let _ = failed_unsent_ask(&fixture,"validation-target","107",OWNER);
        let mut ledger = Ledger::load(&fixture.home).unwrap();
        let original = ledger.request("validation-target").unwrap();
        let source_id = core::s(&source["request_id"]);
        let source_receipt = ledger.receipt(source_id,"progress").unwrap();
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

        for mutation in ["todo", "origin", "fingerprint", "duplicate", "after-pending"] {
            let bytes = serde_json::to_vec(&ledger).unwrap();
            let mut candidate: Ledger = serde_json::from_slice(&bytes).unwrap();
            match mutation {
                "duplicate" => candidate.events.push(event.clone()),
                "todo" => if let Some(Event::OriginRepair { todo_id, .. }) = candidate.events.last_mut() { *todo_id = 2498; },
                "origin" => if let Some(Event::OriginRepair { expected_origin_thread, .. }) = candidate.events.last_mut() { *expected_origin_thread = "109".into(); },
                "fingerprint" => if let Some(Event::OriginRepair { original_fingerprint, .. }) = candidate.events.last_mut() { *original_fingerprint = "f".repeat(64); },
                "after-pending" => {
                    let event_index = candidate.events.len() - 1;
                    candidate.events.insert(event_index, Event::Pending {
                        request_id: "validation-target".into(), phase: "question:0".into(),
                        effect: Effect { target: Target::Forum { title: "pending".into() },
                            content: "pending".into(), notify: true, images: vec![] },
                    });
                }
                _ => unreachable!(),
            }
            let error = candidate.validate().unwrap_err();
            assert_eq!(
                error,
                "origin repair does not match the exact unsent request",
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
                if completed { "progress-completed" } else { "progress-verified" },
                "107",
                2472,
            );
            let _ = failed_unsent_ask(
                &fixture,
                if completed { "completed-question" } else { "verified-question" },
                "107",
                OWNER,
            );
            let id = if completed { "completed-question" } else { "verified-question" };
            let mut ledger = Ledger::load(&fixture.home).unwrap();
            ledger
                .append(
                    &fixture.home,
                    Event::Pending {
                        request_id: id.into(),
                        phase: "question:0".into(),
                        effect: Effect {
                            target: Target::Forum { title: "question".into() },
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
                        Event::DeliveryVerified { request_id: id.into() },
                    )
                    .unwrap();
            }
            let before = fs::read(fixture.home.join(LEDGER)).unwrap();
            let posts_before = fixture.server.lock().unwrap().posts.len();
            let repair = repair_args(
                id,
                "107",
                core::s(&source["progress_message_id"]),
            );
            assert!(repair_origin(&mut ctx, &repair).is_err());
            assert_eq!(fs::read(fixture.home.join(LEDGER)).unwrap(), before);
            assert_eq!(fixture.server.lock().unwrap().posts.len(), posts_before);
        }
    }
}
