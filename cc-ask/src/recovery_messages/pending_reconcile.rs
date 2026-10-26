//! Explicit, one-slot recovery. Ordinary ensure() remains GET-only for Pending.
use super::*;

const QUAL_DOMAIN: &str = "discord-pending-log-reconcile-v1";
const INVALID: &str = "pending reconcile qualification, current record or lease differs";
const RELAY: &str = "01a16dad-9ec4-76a1-9586-1f819c9330b1";
const RUN: &str = "automation:2026-10-26T18:34:26.422Z";
const OUTER: &str = "normal-log-de5446468f897e711e09ddd6fdd1c185fc4bcf26f71c49ed001029b2b3fe5f1b";
const REQUEST: &str = "conversation-log-1ff3c6481a24325fcd88363de3a7593c";
const PHASE: &str = "log:0";

#[derive(Args, Debug)]
pub struct ReconcilePendingArgs {
    #[arg(long)]
    pub qualification_file: PathBuf,
    #[arg(long, conflicts_with = "expected_precheck")]
    pub check_only: bool,
    /// Exact command_id returned by a fresh check-only of this same input.
    #[arg(long, required_unless_present = "check_only")]
    pub expected_precheck: Option<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Lease {
    kind: String,
    holder_pid: u32,
    holder_created_filetime: u64,
    expires_at: String,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub(super) struct Qualification {
    domain: String,
    purpose: String,
    run: String,
    outer_request_id: String,
    request_id: String,
    fingerprint: String,
    phase: String,
    body_sha256: String,
    body_utf8_bytes: usize,
    guild_id: String,
    thread_id: String,
    bot_id: String,
    notify: bool,
    media_count: usize,
    journal_sha256: String,
    registry_sha256: String,
    transport_input_sha256: String,
    relay_session: String,
    lease: Lease,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(super) struct QualificationReceipt {
    binding: Qualification,
    input_sha256: String,
    effect_sha256: String,
    history_sha256: String,
    command_id: String,
}

pub(super) struct Scope<'a> {
    pub(super) run: &'a str,
    pub(super) outer: &'a str,
    pub(super) request: &'a str,
    pub(super) fingerprint: &'a str,
    pub(super) body_sha256: &'a str,
    pub(super) body_bytes: usize,
    pub(super) guild: &'a str,
    pub(super) thread: &'a str,
    pub(super) bot: &'a str,
}

const SCOPE: Scope<'static> = Scope {
    run: RUN,
    outer: OUTER,
    request: REQUEST,
    fingerprint: "31ba4a80d7d0e199c499dc66afda6dd78b9add80265ce333f2f6d3b077237b68",
    body_sha256: "554a2b7851d18bf84d0762b982b9ec29233ae8013ce6406f3b8ec7156d5cb0e9",
    body_bytes: 2292,
    guild: "1552944707464597524",
    thread: "1563566177358454935",
    bot: "1554269962229649478",
};

fn sha(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

fn ordinary(path: &Path) -> Result<()> {
    // Refuse redirected ancestors as well as a redirected final component.
    for ancestor in path.ancestors() {
        let m = fs::symlink_metadata(ancestor).map_err(|_| INVALID)?;
        if m.file_type().is_symlink() {
            return Err(INVALID.into());
        }
        #[cfg(windows)]
        {
            use std::os::windows::fs::MetadataExt;
            if m.file_attributes() & 0x400 != 0 {
                return Err(INVALID.into());
            }
        }
    }
    if !fs::metadata(path).map_err(|_| INVALID)?.is_file() {
        return Err(INVALID.into());
    }
    Ok(())
}

fn regular(path: &Path, limit: usize) -> Result<Vec<u8>> {
    ordinary(path)?;
    read_bounded(fs::File::open(path).map_err(|_| INVALID)?, limit)
}

impl Qualification {
    fn identity(&self, scope: &Scope<'_>) -> Result<()> {
        if self.domain != QUAL_DOMAIN
            || self.purpose != "reconcile_exact_pending_text_log"
            || self.run != scope.run
            || self.outer_request_id != scope.outer
            || self.request_id != scope.request
            || self.phase != PHASE
            || self.fingerprint != scope.fingerprint
            || self.body_sha256 != scope.body_sha256
            || self.body_utf8_bytes != scope.body_bytes
            || self.guild_id != scope.guild
            || self.thread_id != scope.thread
            || self.bot_id != scope.bot
            || self.notify
            || self.media_count != 0
            || self.relay_session != RELAY
            || self.lease.kind != "existing-runner-and-sender-locks-v1"
            || self.lease.holder_pid == 0
            || self.lease.holder_created_filetime == 0
        {
            return Err(INVALID.into());
        }
        for s in [
            &self.journal_sha256,
            &self.registry_sha256,
            &self.transport_input_sha256,
        ] {
            if s.len() != 64
                || !s
                    .bytes()
                    .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
            {
                return Err(INVALID.into());
            }
        }
        Ok(())
    }

    fn request(&self, ledger: &Ledger) -> Result<(Request, Effect)> {
        let request = ledger.request(&self.request_id).ok_or(INVALID)?.clone();
        if request.request_id != self.request_id
            || request.fingerprint != self.fingerprint
            || request.fingerprint()? != self.fingerprint
            || sha(request.body.as_bytes()) != self.body_sha256
            || request.body.len() != self.body_utf8_bytes
            || request.notify
            || !request.images.is_empty()
            || request.program_evidence.is_some()
            || request.destination.guild != self.guild_id
            || ledger.bot(&self.request_id) != Some(self.bot_id.as_str())
            || !matches!(&request.operation, Kind::Log {thread: Some(t)} if t == &self.thread_id)
            || parts(&request.body).len() != 1
        {
            return Err(INVALID.into());
        }
        let expected = effect(
            &request,
            PHASE,
            Target::Thread {
                thread: self.thread_id.clone(),
            },
            &request.body,
            false,
            false,
        )?;
        if ledger.effect(&self.request_id, PHASE) != Some(&expected)
            || ledger.events.iter().any(|e| {
                matches!(e, Event::Pending {request_id, phase, ..}
                if request_id == &self.request_id && phase != PHASE)
            })
        {
            return Err(INVALID.into());
        }
        Ok((request, expected))
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Registration {
    input_sha256: String,
    body_file: PathBuf,
    destination_thread_id: String,
    #[serde(default)]
    message_id: String,
    #[serde(default)]
    verified_thread_id: String,
    effective_notify: Option<bool>,
}

fn registration(home: &Path, q: &Qualification, request: &Request) -> Result<String> {
    let registry = home.join("unattended/conversation-questions.json");
    let bytes = regular(&registry, LEDGER_BYTES)?;
    if sha(&bytes) != q.registry_sha256 {
        return Err("pending reconcile registry CAS changed".into());
    }
    let index: Value = serde_json::from_slice(&bytes).map_err(|_| INVALID)?;
    if index["domain"] != "discord-conversation-v1"
        || index["destination"]["guild_id"] != q.guild_id
        || index["destination"]["application_id"] != q.bot_id
        || index["destination"]["task_channel_id"] != request.destination.forum
        || index["destination"]["owner_user_id"] != request.destination.owner
    {
        return Err(INVALID.into());
    }
    let record: Registration =
        serde_json::from_value(index["logs"][&q.outer_request_id].clone()).map_err(|_| INVALID)?;
    // Registry has no stored lease. The shared runner byte lock is its live
    // writer authority. Reject unexpected record fields (including a new lease).
    let expected_path = home
        .join("unattended/conversation-logs")
        .join(format!("{}.txt", q.outer_request_id));
    if record.body_file != expected_path
        || record.destination_thread_id != q.thread_id
        || record.effective_notify == Some(true)
        || !record.message_id.is_empty()
        || !record.verified_thread_id.is_empty()
        || record.input_sha256 != q.transport_input_sha256
        || format!(
            "normal-log-{}",
            sha(json!([
                "normal-log-v1",
                request.title,
                q.body_sha256,
                q.thread_id,
                false,
                []
            ])
            .to_string()
            .as_bytes())
        ) != q.outer_request_id
        || format!(
            "conversation-log-{}",
            &sha(q.outer_request_id.as_bytes())[..32]
        ) != q.request_id
    {
        return Err(INVALID.into());
    }
    let body = regular(&record.body_file, TEXT_BYTES)?;
    if sha(&body) != q.body_sha256
        || body.len() != q.body_utf8_bytes
        || body != request.body.as_bytes()
    {
        return Err(INVALID.into());
    }
    // Go hashes a different domain: [title, body SHA, thread, notify, media hashes].
    // Go initializes an empty slice. Never compare this hash to body SHA.
    let transport = digest(&json!([
        request.title,
        q.body_sha256,
        q.thread_id,
        false,
        []
    ]))?;
    if transport != record.input_sha256 {
        return Err(INVALID.into());
    }
    Ok(sha(&bytes))
}

#[cfg(windows)]
pub(super) fn lease(q: &Qualification) -> Result<()> {
    use windows_sys::Win32::{
        Foundation::*,
        System::{Diagnostics::ToolHelp::*, Threading::*},
    };
    let expires = chrono::DateTime::parse_from_rfc3339(&q.lease.expires_at).map_err(|_| INVALID)?;
    let left = expires.signed_duration_since(chrono::Utc::now());
    if left.num_seconds() <= 0 || left.num_seconds() > 300 {
        return Err("pending reconcile lease expired or unbounded".into());
    }
    // The lease holder must be the actual direct caller, not an asserted idle
    // process, expired PID or another running worker. Both shared locks remain
    // held for the entire transaction; this does not replace the runner lease.
    unsafe {
        let snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
        if snapshot == INVALID_HANDLE_VALUE {
            return Err(INVALID.into());
        }
        let mut entry: PROCESSENTRY32W = std::mem::zeroed();
        entry.dwSize = std::mem::size_of_val(&entry) as u32;
        let mut ok = Process32FirstW(snapshot, &mut entry);
        let mut parent = 0;
        while ok != 0 {
            if entry.th32ProcessID == std::process::id() {
                parent = entry.th32ParentProcessID;
                break;
            }
            ok = Process32NextW(snapshot, &mut entry);
        }
        CloseHandle(snapshot);
        if parent != q.lease.holder_pid {
            return Err("pending reconcile lease holder is not this caller".into());
        }
        let process = OpenProcess(
            PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
            0,
            parent,
        );
        if process.is_null() {
            return Err(INVALID.into());
        }
        let mut birth: FILETIME = std::mem::zeroed();
        let mut end = birth;
        let mut kernel = birth;
        let mut user = birth;
        let read = GetProcessTimes(process, &mut birth, &mut end, &mut kernel, &mut user);
        let alive = WaitForSingleObject(process, 0) == WAIT_TIMEOUT;
        CloseHandle(process);
        let stamp = ((birth.dwHighDateTime as u64) << 32) | birth.dwLowDateTime as u64;
        if read == 0 || !alive || stamp != q.lease.holder_created_filetime {
            return Err(INVALID.into());
        }
    }
    Ok(())
}

#[cfg(not(windows))]
pub(super) fn lease(_: &Qualification) -> Result<()> {
    Err("pending reconciliation requires the existing Windows caller lease".into())
}

// A short/empty page alone is never absence evidence. The complete scan must
// include the forum starter and the channel's unchanged last-message watermark.
fn history(
    api: &mut RecoveryApi<'_>,
    ledger: &Ledger,
    request: &Request,
    q: &Qualification,
    effect: &Effect,
) -> Result<(String, Vec<(String, String)>)> {
    api.assert_bot_thread(&q.thread_id)?;
    // Prove that this bot can read the exact known starter, including content.
    // Discord can return an empty history when READ_MESSAGE_HISTORY is absent.
    let (anchor_id, anchor_phase) = ledger
        .events
        .iter()
        .find_map(|event| match event {
            Event::Verified {
                request_id,
                phase,
                receipt,
            } if receipt.thread_id == q.thread_id
                && receipt.message_id == q.thread_id
                && matches!(phase.as_str(), "log:0" | "progress") =>
            {
                Some((request_id, phase))
            }
            _ => None,
        })
        .ok_or("pending reconcile has no original verified thread starter")?;
    api.verify(
        ledger,
        ledger.request(anchor_id).ok_or(INVALID)?,
        ledger.effect(anchor_id, anchor_phase).ok_or(INVALID)?,
        &q.thread_id,
        &q.thread_id,
    )?;
    let channel = api.channel(&q.thread_id)?;
    let last = snowflake(core::s(&channel["last_message_id"]))?;
    let mut rows = Vec::new();
    let mut ids = std::collections::BTreeSet::new();
    let mut before = u64::MAX;
    let mut complete = false;
    for page in 0..HISTORY_PAGES {
        let suffix = if page == 0 {
            String::new()
        } else {
            format!("&before={before}")
        };
        let value = api.get(&format!(
            "/channels/{}/messages?limit=100{suffix}",
            q.thread_id
        ))?;
        let items = value
            .as_array()
            .ok_or("pending reconcile history is not an array")?;
        if items.len() > 100 {
            return Err(INVALID.into());
        }
        for item in items {
            let id = snowflake(core::s(&item["id"]))?
                .parse::<u64>()
                .map_err(|_| INVALID)?;
            if id >= before || !ids.insert(id) || item["channel_id"] != q.thread_id {
                return Err("pending reconcile history cursor or identity is ambiguous".into());
            }
            before = id;
            rows.push(item.clone());
        }
        if items.len() < 100 {
            complete = true;
            break;
        }
    }
    if !complete {
        return Err("pending reconcile history limit reached".into());
    }
    let mut found = Vec::new();
    let tag = format!("recovery:{}:{}", &q.fingerprint[..24], PHASE);
    for message in &rows {
        let content = message["content"]
            .as_str()
            .ok_or("pending reconcile history content unavailable")?;
        snowflake(core::s(&message["author"]["id"]))?;
        if content.contains(&tag) {
            if content != effect.content || message["author"]["id"] != q.bot_id {
                return Err(
                    "pending reconcile found a conflicting tag; retained without POST".into(),
                );
            }
            // Full receipt validation, including media/mentions/flags, precedes
            // qualification. A near match never counts as proof of absence.
            api.verify(
                ledger,
                request,
                effect,
                &q.thread_id,
                core::s(&message["id"]),
            )?;
            found.push((q.thread_id.clone(), snowflake(core::s(&message["id"]))?));
        }
    }
    if !rows.iter().any(|r| r["id"] == q.thread_id)
        || !rows.iter().any(|r| r["id"] == last)
        || api.channel(&q.thread_id)?["last_message_id"] != last
    {
        return Err("pending reconcile history has no stable complete range".into());
    }
    if found.len() > 1 {
        return Err("pending reconcile duplicate candidates retained; no POST".into());
    }
    Ok((
        digest(&json!({"thread":q.thread_id,"last":last,"messages":rows}))?,
        found,
    ))
}

fn attempted(ledger: &Ledger, id: &str) -> bool {
    ledger.events.iter().any(|e| matches!(e, Event::PendingRecoveryAttempt {request_id, phase, ..} if request_id == id && phase == PHASE))
}

pub(super) fn validate_events(ledger: &Ledger) -> Result<()> {
    let mut qualified = BTreeMap::new();
    let mut attempts = BTreeMap::new();
    let mut results = BTreeMap::new();
    let mut latest = BTreeMap::new();
    for (i, event) in ledger.events.iter().enumerate() {
        match event {
            Event::PendingQualification { qualification: qr } => {
                let prefix = Ledger {
                    domain: ledger.domain.clone(),
                    version: ledger.version,
                    events: ledger.events[..i].to_vec(),
                };
                let q = &qr.binding;
                let (_, effect) = q.request(&prefix)?;
                let mut for_command = qr.as_ref().clone();
                for_command.command_id.clear();
                if q.domain != QUAL_DOMAIN
                    || q.purpose != "reconcile_exact_pending_text_log"
                    || q.phase != PHASE
                    || q.relay_session != RELAY
                    || q.lease.kind != "existing-runner-and-sender-locks-v1"
                    || prefix.posted(&q.request_id, PHASE).is_some()
                    || prefix.receipt(&q.request_id, PHASE).is_some()
                    || prefix.complete(&q.request_id)
                    || attempted(&prefix, &q.request_id)
                    || qr.command_id
                        != digest(&serde_json::to_value(for_command).map_err(|_| INVALID)?)?
                    || [
                        &qr.input_sha256,
                        &qr.history_sha256,
                        &q.journal_sha256,
                        &q.registry_sha256,
                    ]
                    .iter()
                    .any(|s| {
                        s.len() != 64
                            || !s
                                .bytes()
                                .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
                    })
                    || chrono::DateTime::parse_from_rfc3339(&q.lease.expires_at).is_err()
                    || qr.effect_sha256
                        != digest(&serde_json::to_value(effect).map_err(|_| INVALID)?)?
                    || qualified
                        .insert(
                            sha(&serde_json::to_vec(qr).map_err(|_| INVALID)?),
                            q.request_id.clone(),
                        )
                        .is_some()
                {
                    return Err(INVALID.into());
                }
                latest.insert(
                    q.request_id.clone(),
                    sha(&serde_json::to_vec(qr).map_err(|_| INVALID)?),
                );
            }
            Event::PendingRecoveryAttempt {
                request_id,
                phase,
                qualification_sha256,
                attempt_id,
            } => {
                let invalid_attempt = phase != PHASE
                    || qualified.get(qualification_sha256) != Some(request_id)
                    || latest.get(request_id) != Some(qualification_sha256)
                    || ledger.events[..i].iter().any(|e| matches!(e,
                        Event::Posted {request_id:id, phase:p, ..} | Event::Verified {request_id:id, phase:p, ..}
                        if id == request_id && p == phase))
                    || attempt_id != qualification_sha256
                    || attempts
                        .insert(request_id.clone(), attempt_id.clone())
                        .is_some();
                if invalid_attempt {
                    return Err("invalid or duplicate pending recovery attempt".into());
                }
            }
            Event::PendingRecoveryResult {
                request_id,
                phase,
                attempt_id,
                outcome,
            } => {
                let invalid_result = phase != PHASE
                    || attempts.get(request_id) != Some(attempt_id)
                    || !["response_received", "unconfirmed"].contains(&outcome.as_str())
                    || results.insert(request_id.clone(), true).is_some();
                if invalid_result {
                    return Err("invalid pending recovery result".into());
                }
            }
            _ => {}
        }
    }
    Ok(())
}

pub fn reconcile_pending(ctx: &mut Context, args: &ReconcilePendingArgs) -> Result<Value> {
    reconcile(ctx, args, &SCOPE, lease)
}

pub(super) fn reconcile(
    ctx: &mut Context,
    args: &ReconcilePendingArgs,
    scope: &Scope<'_>,
    verify_lease: fn(&Qualification) -> Result<()>,
) -> Result<Value> {
    reconcile_with_store(ctx, args, scope, verify_lease, Ledger::append)
}

type Save = fn(&mut Ledger, &Path, Event) -> Result<()>;

pub(super) fn reconcile_with_store(
    ctx: &mut Context,
    args: &ReconcilePendingArgs,
    scope: &Scope<'_>,
    verify_lease: fn(&Qualification) -> Result<()>,
    save: Save,
) -> Result<Value> {
    let input = regular(&args.qualification_file, 16 * 1024)?;
    let q: Qualification = serde_json::from_slice(&input).map_err(|_| INVALID)?;
    q.identity(scope)?;
    verify_lease(&q)?;
    let home = ctx.here.clone();
    ensure_storage(&home)?;
    // Reuse, never create or steal, both normal writer locks in Go -> Rust order.
    let runner_lock = home.join("unattended/runner.lock");
    for path in [&runner_lock, &home.join(LOCK)] {
        ordinary(path)?;
    }
    let _runner = ByteLock::acquire(&runner_lock, Duration::ZERO)?;
    let _sender = ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?;
    let original = regular(&home.join(LEDGER), LEDGER_BYTES)?;
    if sha(&original) != q.journal_sha256 {
        return Err("pending reconcile journal CAS changed".into());
    }
    let mut ledger = Ledger::load(&home)?;
    let (request, effect) = q.request(&ledger)?;
    let registry_sha = registration(&home, &q, &request)?;
    let mut api = RecoveryApi::inspect(ctx, &mut ledger, &request)?;
    if api.bot != q.bot_id {
        return Err(INVALID.into());
    }
    let (history_sha, candidates) = history(&mut api, &ledger, &request, &q, &effect)?;
    let qr = QualificationReceipt {
        binding: q.clone(),
        input_sha256: sha(&input),
        effect_sha256: digest(&serde_json::to_value(&effect).map_err(|_| INVALID)?)?,
        history_sha256: history_sha,
        command_id: String::new(),
    };
    let command_id = digest(&serde_json::to_value(&qr).map_err(|_| INVALID)?)?;
    let report = json!({"domain":QUAL_DOMAIN,"request_id":q.request_id,"phase":PHASE,
        "check_only":args.check_only,"command_id":command_id,"qualification_sha256":sha(&input),
        "journal_sha256":q.journal_sha256,"registry_sha256":registry_sha,"history_sha256":qr.history_sha256,
        "exact_candidates":candidates.len(),"attempt_recorded":attempted(&ledger,&q.request_id),
        "lease_verified":true,"delivery":"pending","new_posts":0});
    if args.check_only {
        return Ok(report);
    }
    if args.expected_precheck.as_deref() != Some(command_id.as_str()) {
        return Err("pending reconcile precheck CAS changed".into());
    }
    // Recheck bytes and caller after all GETs, immediately before journal writes.
    verify_lease(&q)?;
    if regular(&args.qualification_file, 16 * 1024)? != input
        || regular(&home.join(LEDGER), LEDGER_BYTES)? != original
        || registration(&home, &q, &request)? != registry_sha
    {
        return Err("pending reconcile apply CAS changed".into());
    }
    if let Some((thread, message)) = candidates.first() {
        let receipt = api.verify(&ledger, &request, &effect, thread, message)?;
        return finish(&mut ledger, &home, &request, receipt, report, false, save);
    }
    if ledger.posted(&q.request_id, PHASE).is_some()
        || ledger.receipt(&q.request_id, PHASE).is_some()
        || ledger.complete(&q.request_id)
        || attempted(&ledger, &q.request_id)
    {
        return Err(PENDING.into());
    }
    let mut qr = qr;
    qr.command_id = command_id;
    let attempt_id = sha(&serde_json::to_vec(&qr).map_err(|_| INVALID)?);
    save(
        &mut ledger,
        &home,
        Event::PendingQualification {
            qualification: Box::new(qr),
        },
    )?;
    save(
        &mut ledger,
        &home,
        Event::PendingRecoveryAttempt {
            request_id: q.request_id.clone(),
            phase: PHASE.into(),
            qualification_sha256: attempt_id.clone(),
            attempt_id: attempt_id.clone(),
        },
    )?;
    // Both durable saves must have succeeded. This is the only submit site.
    verify_lease(&q)?;
    let response = api.submit(&ledger, &request, PHASE, &effect, &[]);
    save(
        &mut ledger,
        &home,
        Event::PendingRecoveryResult {
            request_id: q.request_id.clone(),
            phase: PHASE.into(),
            attempt_id,
            outcome: if response.is_ok() {
                "response_received"
            } else {
                "unconfirmed"
            }
            .into(),
        },
    )?;
    if let Ok(response) = response {
        if let Ok(message) = snowflake(core::s(&response["id"])) {
            save(
                &mut ledger,
                &home,
                Event::Posted {
                    request_id: q.request_id.clone(),
                    phase: PHASE.into(),
                    thread_id: q.thread_id.clone(),
                    message_id: message,
                },
            )?;
        }
    }
    // Even a received response is only a hint. Never POST again on GET failure.
    let (_, found) = history(&mut api, &ledger, &request, &q, &effect)?;
    let [(thread, message)] = found.as_slice() else {
        return Err(PENDING.into());
    };
    let receipt = api.verify(&ledger, &request, &effect, thread, message)?;
    finish(&mut ledger, &home, &request, receipt, report, true, save)
}

fn finish(
    ledger: &mut Ledger,
    home: &Path,
    request: &Request,
    receipt: Receipt,
    mut report: Value,
    posted: bool,
    save: Save,
) -> Result<Value> {
    if let Some(saved) = ledger.receipt(&request.request_id, PHASE) {
        if saved != &receipt {
            return Err(INVALID.into());
        }
    } else {
        save(
            ledger,
            home,
            Event::Verified {
                request_id: request.request_id.clone(),
                phase: PHASE.into(),
                receipt: receipt.clone(),
            },
        )?;
    }
    if !ledger.complete(&request.request_id) {
        save(
            ledger,
            home,
            Event::DeliveryVerified {
                request_id: request.request_id.clone(),
            },
        )?;
    }
    report["delivery"] = json!("verified");
    report["message_id"] = json!(receipt.message_id);
    report["thread_id"] = json!(receipt.thread_id);
    report["timestamp"] = json!(receipt.timestamp);
    report["flags"] = json!(receipt.flags);
    report["owner_mentions"] = json!(receipt.owner_mentions);
    report["new_posts"] = json!(usize::from(posted));
    Ok(report)
}
