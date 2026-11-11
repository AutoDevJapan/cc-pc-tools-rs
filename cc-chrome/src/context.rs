//! Reuse only a recent, successful normal Browser Use attachment for this job.
//! The extension network policy is an opaque normal-runtime result, not an
//! allow policy that a standalone caller is permitted to manufacture.
use crate::*;

const CONTEXT_FILE: &str = "chrome-browser-context.json";
const STATE_FILE: &str = "chrome-browser-context-state.json";
const MAX_CONTEXT: u64 = 128 * 1024;
const MAX_STATE: u64 = 1024 * 1024;
const MAX_AGE: i64 = 60;

#[derive(Debug)]
struct ContextFailure(&'static str);
impl std::fmt::Display for ContextFailure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "normal browser context unavailable: {}", self.0)
    }
}
impl std::error::Error for ContextFailure {}
fn failure(reason: &'static str) -> io::Error {
    io::Error::other(ContextFailure(reason))
}
pub(crate) fn diagnostic(dir: &Path, operation: &'static str, error: &io::Error) {
    let reason = error
        .get_ref()
        .and_then(|e| e.downcast_ref::<ContextFailure>())
        .map_or("io", |e| e.0);
    // Only stable classifications; never persist policy/header values, paths,
    // backend diagnostics or arbitrary error display strings.
    let _ = lifecycle::atomic_json(
        &dir.join("chrome-context-error.json"),
        &json!({"at":epoch(),"operation":operation,"reason":reason,"error_kind":format!("{:?}",error.kind())}),
    );
}
fn owner(dir: &Path) -> io::Result<Value> {
    let bytes = fs::read(dir.join("job.json")).map_err(|e| {
        if e.kind() == io::ErrorKind::NotFound {
            failure("job_missing")
        } else {
            failure("job_read")
        }
    })?;
    serde_json::from_slice(&bytes).map_err(|_| failure("job_invalid"))
}
pub(crate) fn require_owner(dir: &Path, job: &str) -> io::Result<()> {
    let value = owner(dir)?;
    if value["id"] != job
        || value["status"] != "running"
        || value["thread_id"].as_str().is_none_or(|s| s.is_empty())
    {
        return Err(failure("job_identity"));
    }
    Ok(())
}

type Leases = std::collections::HashMap<
    PathBuf,
    std::collections::HashMap<String, (String, String, fs::File)>,
>;
fn leases() -> &'static std::sync::Mutex<Leases> {
    static LEASES: std::sync::OnceLock<std::sync::Mutex<Leases>> = std::sync::OnceLock::new();
    LEASES.get_or_init(Default::default)
}

fn lease_path(dir: &Path, generation: &str) -> io::Result<PathBuf> {
    let id = uuid::Uuid::parse_str(generation)
        .map_err(|_| io::Error::other("invalid browser context lease"))?;
    Ok(dir.join(format!("chrome-browser-context-lease-{id}.lock")))
}

fn lease_alive(dir: &Path, permit: &AttachmentPermit) -> bool {
    use std::os::windows::fs::OpenOptionsExt;
    let Ok(path) = lease_path(dir, &permit.generation) else {
        return false;
    };
    fs::OpenOptions::new()
        .read(true)
        .share_mode(7)
        .open(&path)
        .and_then(|file| file.metadata())
        .is_ok_and(|meta| meta.is_file() && meta.len() == 0)
}

fn new_lease(dir: &Path, permit: &AttachmentPermit) -> io::Result<()> {
    use std::os::windows::fs::OpenOptionsExt;
    let file = fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create_new(true)
        .share_mode(5) // read/delete sharing, no external writer
        .custom_flags(0x0400_0000) // FILE_FLAG_DELETE_ON_CLOSE
        .open(lease_path(dir, &permit.generation)?)?;
    leases()
        .lock()
        .map_err(|_| io::Error::other("context lease lock poisoned"))?
        .entry(dir.to_path_buf())
        .or_default()
        .insert(
            permit.generation.clone(),
            (permit.session.clone(), permit.turn.clone(), file),
        );
    Ok(())
}

fn revoke_leases(dir: &Path, identity: Option<(&Value, &Value)>) -> io::Result<()> {
    // This does not depend on the JSON state or its serialization lock. Closing
    // the original OS handles revokes cached contexts even if state saving fails.
    let mut all = leases()
        .lock()
        .map_err(|_| io::Error::other("context lease lock poisoned"))?;
    if let Some((session, turn)) = identity {
        if let Some(entries) = all.get_mut(dir) {
            entries.retain(|_, (s, t, _)| session != s || turn != t);
        }
    } else {
        all.remove(dir);
    }
    Ok(())
}

pub(crate) fn release(dir: &Path) {
    let _ = revoke_leases(dir, None);
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct AttachmentPermit {
    session: String,
    turn: String,
    generation: String,
}

#[derive(Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct ContextState {
    active: Option<AttachmentPermit>,
    ended: std::collections::BTreeSet<(String, String)>,
}

fn lock(dir: &Path) -> io::Result<fs::File> {
    let file = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(dir.join("chrome-browser-context.lock"))?;
    // Local bookkeeping must not hang an ordinary Browser Use end notification.
    file.try_lock().map_err(|_| failure("state_lock"))?;
    Ok(file)
}

fn state(dir: &Path) -> io::Result<ContextState> {
    let path = dir.join(STATE_FILE);
    match fs::symlink_metadata(&path) {
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(ContextState::default()),
        Err(e) => return Err(e),
        Ok(meta) if !meta.is_file() || meta.file_type().is_symlink() || meta.len() > MAX_STATE => {
            return Err(failure("state_invalid"));
        }
        Ok(_) => {}
    }
    serde_json::from_slice(&fs::read(path)?).map_err(|_| failure("state_invalid"))
}

fn current(dir: &Path, state: &ContextState, permit: &AttachmentPermit) -> bool {
    state.active.as_ref() == Some(permit)
        && !state
            .ended
            .contains(&(permit.session.clone(), permit.turn.clone()))
        && lease_alive(dir, permit)
}

/// Bind the request before forwarding it. The permit survives in the pending
/// response, but never survives a turn end, a detach or another active turn.
pub(crate) fn begin_attachment(
    dir: &Path,
    job: &str,
    params: &Value,
) -> io::Result<AttachmentPermit> {
    let _lock = lock(dir)?;
    let owner = owner(dir)?;
    let session = params["session_id"].as_str().filter(|s| !s.is_empty());
    let turn = params["turn_id"].as_str().filter(|s| !s.is_empty());
    let (Some(session), Some(turn)) = (session, turn) else {
        return Err(failure("identity_missing"));
    };
    if owner["id"] != job
        || owner["thread_id"] != session
        || owner["status"] != "running"
        || params["session_context"] != "live"
    {
        return Err(failure("identity_mismatch"));
    }
    let mut state = state(dir)?;
    if state.ended.contains(&(session.into(), turn.into())) {
        return Err(failure("turn_ended"));
    }
    if let Some(permit) = &state.active
        && permit.session == session
        && permit.turn == turn
    {
        if lease_alive(dir, permit) {
            return Ok(permit.clone());
        }
        return Err(failure("lease_revoked"));
    }
    let permit = AttachmentPermit {
        session: session.into(),
        turn: turn.into(),
        generation: uuid::Uuid::new_v4().to_string(),
    };
    if let Some(previous) = state.active.take()
        && (previous.session != session || previous.turn != turn)
    {
        state.ended.insert((previous.session, previous.turn));
    }
    revoke_leases(dir, None)?;
    new_lease(dir, &permit).map_err(|_| failure("lease_create"))?;
    state.active = Some(permit.clone());
    if lifecycle::atomic_json(&dir.join(STATE_FILE), &state).is_err() {
        revoke_leases(dir, None)?;
        return Err(failure("state_write"));
    }
    Ok(permit)
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct BrowserContext {
    domain: String,
    job: String,
    pipe: String,
    instance: String,
    generation: String,
    pub(crate) session: String,
    pub(crate) turn: String,
    recorded_at: i64,
    authority: AttachmentPermit,
    pub(crate) network_policy: Value,
    session_params: Value,
}

fn policy_shape(v: &Value) -> bool {
    let Some(object) = v.as_object() else {
        return false;
    };
    if object
        .keys()
        .any(|k| k != "constraints" && k != "timeoutMs")
    {
        return false;
    }
    if v.get("timeoutMs")
        .is_some_and(|n| !n.as_u64().is_some_and(|n| (1..=30_000).contains(&n)))
    {
        return false;
    }
    let Some(constraints) = v["constraints"].as_array() else {
        return false;
    };
    // A normal getNetworkPolicy result includes its requirements/config/origin
    // constraints even when each list is empty. An absent result is not [].
    if constraints.is_empty() || constraints.len() > 128 {
        return false;
    }
    let mut count = 0;
    let mut bytes = 0;
    constraints.iter().all(|c| {
        let Some(o) = c.as_object() else { return false };
        if o.len() != 3
            || o.keys()
                .any(|k| !["kind", "mode", "patterns"].contains(&k.as_str()))
            || !matches!(c["kind"].as_str(), Some("domains" | "origins"))
            || !matches!(c["mode"].as_str(), Some("denylist" | "allowlist"))
        {
            return false;
        }
        let Some(patterns) = c["patterns"].as_array() else {
            return false;
        };
        count += patterns.len();
        if count > 4096 {
            return false;
        }
        patterns.iter().all(|p| {
            let Some(s) = p.as_str() else { return false };
            bytes += s.len();
            !s.is_empty() && !s.contains('\0') && count <= 4096 && bytes <= 64 * 1024
        })
    })
}

impl BrowserContext {
    pub(crate) fn params(&self) -> Value {
        self.session_params.clone()
    }

    pub(crate) fn attachment_params(&self, record: &Record) -> io::Result<Value> {
        if self.job != record.job
            || self.session != record.session
            || self.turn != record.turn
            || self.instance != record.instance
            || self.generation != record.generation
        {
            return Err(io::Error::other(
                "normal browser attachment identity differs",
            ));
        }
        let mut params = self.params();
        params["tabId"] = json!(record.tab);
        params["networkPolicy"] = self.network_policy.clone();
        Ok(params)
    }

    pub(crate) fn matches(&self, job: &str, owner: &Value, info: &Value, now: i64) -> bool {
        self.domain == "owner-chrome-normal-context-v1"
            && self.job == job
            && owner["id"] == job
            && owner["thread_id"] == self.session
            && owner["status"] == "running"
            && !self.session.is_empty()
            && !self.turn.is_empty()
            && self.session_params["session_id"] == self.session
            && self.session_params["turn_id"] == self.turn
            && self.session_params["session_context"] == "live"
            && self.authority.session == self.session
            && self.authority.turn == self.turn
            && now >= self.recorded_at
            && now - self.recorded_at <= MAX_AGE
            && policy_shape(&self.network_policy)
            && (Record {
                job: job.into(),
                pipe: self.pipe.clone(),
                instance: self.instance.clone(),
                generation: self.generation.clone(),
                session: self.session.clone(),
                turn: self.turn.clone(),
                tab: 1,
                created: now,
            })
            .matches_backend(info)
    }
}

/// Called only for the response to a successful attach already forwarded by
/// this job's normal CUA proxy. Neither arbitrary CLI input nor a failed attach
/// can publish a context. Requirements/config/origin denies and request-header
/// metadata are preserved byte-for-byte as JSON values; never print them.
pub(crate) fn publish_attachment(
    dir: &Path,
    job: &str,
    pipe: &str,
    info: &Value,
    params: &Value,
    permit: &AttachmentPermit,
) -> io::Result<()> {
    let _lock = lock(dir)?;
    if !current(dir, &state(dir)?, permit)
        || params["session_id"] != permit.session
        || params["turn_id"] != permit.turn
    {
        return Err(failure("invalidated"));
    }
    let owner = owner(dir)?;
    if !policy_shape(&params["networkPolicy"]) {
        return Err(failure("policy_shape"));
    }
    let mut session_params = json!({});
    for key in [
        "session_id",
        "turn_id",
        "session_context",
        "request_headers",
        "agent_request_header_enabled",
        "root_session_id",
    ] {
        if let Some(v) = params.get(key) {
            session_params[key] = v.clone();
        }
    }
    let context = BrowserContext {
        domain: "owner-chrome-normal-context-v1".into(),
        job: job.into(),
        pipe: pipe.into(),
        instance: info["metadata"]["extensionInstanceId"]
            .as_str()
            .unwrap_or("")
            .into(),
        generation: info["browserTabMentions"]["generation"]
            .as_str()
            .unwrap_or("")
            .into(),
        session: params["session_id"].as_str().unwrap_or("").into(),
        turn: params["turn_id"].as_str().unwrap_or("").into(),
        recorded_at: epoch(),
        authority: permit.clone(),
        network_policy: params["networkPolicy"].clone(),
        session_params,
    };
    if !context.matches(job, &owner, info, epoch()) {
        return Err(failure("identity_mismatch"));
    }
    lifecycle::atomic_json(&dir.join(CONTEXT_FILE), &context)
}

pub(crate) fn load(dir: &Path, job: &str, info: &Value) -> io::Result<BrowserContext> {
    let _lock = lock(dir)?;
    let owner = owner(dir)?;
    let path = dir.join(CONTEXT_FILE);
    let metadata = fs::symlink_metadata(&path).map_err(|e| {
        if e.kind() == io::ErrorKind::NotFound {
            failure("not_recorded")
        } else {
            failure("context_read")
        }
    })?;
    if !metadata.is_file() || metadata.file_type().is_symlink() || metadata.len() > MAX_CONTEXT {
        return Err(failure("context_invalid"));
    }
    let context: BrowserContext =
        serde_json::from_slice(&fs::read(path)?).map_err(|_| failure("context_invalid"))?;
    if !policy_shape(&context.network_policy) {
        return Err(failure("policy_shape"));
    }
    let now = epoch();
    if now < context.recorded_at || now - context.recorded_at > MAX_AGE {
        return Err(failure("expired"));
    }
    if !context.matches(job, &owner, info, now) {
        return Err(failure("identity_mismatch"));
    }
    let state = state(dir)?;
    if state.active.as_ref() != Some(&context.authority)
        || state
            .ended
            .contains(&(context.session.clone(), context.turn.clone()))
    {
        return Err(failure("invalidated"));
    }
    if !lease_alive(dir, &context.authority) {
        return Err(failure("lease_lost"));
    }
    Ok(context)
}

pub(crate) fn attachment_params(record: &Record, info: &Value) -> io::Result<Value> {
    let dir = Path::new("D:/CC/Tools/codex-jobs/jobs").join(&record.job);
    load(&dir, &record.job, info)?.attachment_params(record)
}

pub(crate) fn invalidate(
    dir: &Path,
    session: &Value,
    turn: &Value,
    terminal: bool,
) -> io::Result<()> {
    revoke_leases(dir, Some((session, turn)))?;
    let _lock = lock(dir)?;
    let (Some(session), Some(turn)) = (session.as_str(), turn.as_str()) else {
        return Err(io::Error::other(
            "normal browser context identity unavailable",
        ));
    };
    let mut state = state(dir)?;
    if terminal {
        state.ended.insert((session.into(), turn.into()));
    }
    if state
        .active
        .as_ref()
        .is_some_and(|p| p.session == session && p.turn == turn)
    {
        state.active = None;
    }
    // Publish revocation first: even an unreadable or undeletable context file
    // cannot be loaded, and a delayed response on another proxy cannot revive it.
    lifecycle::atomic_json(&dir.join(STATE_FILE), &state)?;
    let path = dir.join(CONTEXT_FILE);
    if let Ok(bytes) = fs::read(&path)
        && let Ok(context) = serde_json::from_slice::<BrowserContext>(&bytes)
        && session == context.session
        && turn == context.turn
    {
        let _ = fs::remove_file(path);
    }
    Ok(())
}

#[cfg(test)]
pub(crate) fn observe_attachment(
    dir: &Path,
    job: &str,
    pipe: &str,
    info: &Value,
    params: &Value,
) -> io::Result<()> {
    let permit = begin_attachment(dir, job, params)?;
    publish_attachment(dir, job, pipe, info, params, &permit)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn normal_policy_is_required_and_never_replaced_by_allow() {
        let policy = json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]},{"kind":"origins","mode":"allowlist","patterns":["https://allowed.invalid"]}]});
        assert!(policy_shape(&policy));
        for bad in [
            Value::Null,
            json!({}),
            json!({"constraints":[]}),
            json!({"constraints":[{"kind":"domains","mode":"allowlist","patterns":[],"extra":true}]}),
            json!({"constraints":[{"kind":"domains","mode":"allowlist","patterns":[1]}]}),
        ] {
            assert!(!policy_shape(&bad));
        }
        assert_eq!(policy["constraints"][0]["patterns"][0], "blocked.invalid");
        assert_eq!(policy["constraints"][1]["mode"], "allowlist");
    }
    #[test]
    fn normal_context_rejects_another_job_session_generation_or_age() {
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        let owner = json!({"id":"job","thread_id":"real-session","status":"running"});
        let context = BrowserContext {
            domain: "owner-chrome-normal-context-v1".into(),
            job: "job".into(),
            pipe: "pipe".into(),
            instance: "instance".into(),
            generation: "generation".into(),
            session: "real-session".into(),
            turn: "real-turn".into(),
            recorded_at: 100,
            authority: AttachmentPermit {
                session: "real-session".into(),
                turn: "real-turn".into(),
                generation: "fixture".into(),
            },
            network_policy: json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}),
            session_params: json!({"session_id":"real-session","turn_id":"real-turn","session_context":"live"}),
        };
        assert!(context.matches("job", &owner, &info, 100));
        assert!(!context.matches("other", &owner, &info, 100));
        assert!(!context.matches(
            "job",
            &json!({"id":"job","thread_id":"other","status":"running"}),
            &info,
            100
        ));
        let mut changed = info.clone();
        changed["browserTabMentions"]["generation"] = json!("other");
        assert!(!context.matches("job", &owner, &changed, 100));
        assert!(!context.matches("job", &owner, &info, 99));
        assert!(!context.matches("job", &owner, &info, 161));
    }

    #[test]
    fn normal_attachment_keeps_full_policy_and_turn_end_invalidates_it() {
        let root = tempfile::tempdir().unwrap();
        fs::write(
            root.path().join("job.json"),
            json!({"id":"job","thread_id":"session","status":"running"}).to_string(),
        )
        .unwrap();
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        let policy = json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]},{"kind":"origins","mode":"denylist","patterns":["https://private.invalid"]}]});
        let params = json!({"session_id":"session","turn_id":"turn","session_context":"live","networkPolicy":policy});
        observe_attachment(root.path(), "job", "pipe", &info, &params).unwrap();
        let saved = load(root.path(), "job", &info).unwrap();
        assert_eq!(saved.network_policy, policy);
        invalidate(root.path(), &json!("session"), &json!("other-turn"), true).unwrap();
        assert!(load(root.path(), "job", &info).is_ok());
        invalidate(root.path(), &json!("session"), &json!("turn"), true).unwrap();
        assert!(load(root.path(), "job", &info).is_err());
    }

    #[test]
    fn shared_turn_revocation_blocks_late_publish_but_allows_a_fresh_live_turn() {
        let root = tempfile::tempdir().unwrap();
        fs::write(
            root.path().join("job.json"),
            json!({"id":"job","thread_id":"session","status":"running"}).to_string(),
        )
        .unwrap();
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        let params = json!({"session_id":"session","turn_id":"turn","session_context":"live","networkPolicy":{"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}});
        let old = begin_attachment(root.path(), "job", &params).unwrap();
        publish_attachment(root.path(), "job", "pipe", &info, &params, &old).unwrap();
        let other_dir_handle = root.path().to_path_buf();
        // A separate proxy or CLI load sees the same persisted revocation.
        invalidate(&other_dir_handle, &json!("session"), &json!("turn"), false).unwrap();
        assert!(load(root.path(), "job", &info).is_err());
        assert!(publish_attachment(root.path(), "job", "pipe", &info, &params, &old).is_err());
        let fresh = begin_attachment(root.path(), "job", &params).unwrap();
        assert_ne!(
            fresh, old,
            "detach allows a new normal attach, never its old reply"
        );
        publish_attachment(root.path(), "job", "pipe", &info, &params, &fresh).unwrap();
        assert!(load(root.path(), "job", &info).is_ok());
        invalidate(root.path(), &json!("session"), &json!("turn"), true).unwrap();
        assert!(begin_attachment(root.path(), "job", &params).is_err());
        assert!(publish_attachment(root.path(), "job", "pipe", &info, &params, &fresh).is_err());
        let mut next = params.clone();
        next["turn_id"] = json!("next-turn");
        let next_permit = begin_attachment(root.path(), "job", &next).unwrap();
        publish_attachment(root.path(), "job", "pipe", &info, &next, &next_permit).unwrap();
        assert_eq!(load(root.path(), "job", &info).unwrap().turn, "next-turn");
        assert!(publish_attachment(root.path(), "job", "pipe", &info, &params, &old).is_err());
    }

    #[test]
    fn changed_turn_and_corrupt_revocation_state_cannot_authorize_old_context() {
        let root = tempfile::tempdir().unwrap();
        fs::write(
            root.path().join("job.json"),
            json!({"id":"job","thread_id":"session","status":"running"}).to_string(),
        )
        .unwrap();
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        let params = json!({"session_id":"session","turn_id":"old","session_context":"live","networkPolicy":{"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}});
        let old = begin_attachment(root.path(), "job", &params).unwrap();
        publish_attachment(root.path(), "job", "pipe", &info, &params, &old).unwrap();
        let mut next = params.clone();
        next["turn_id"] = json!("new");
        let new = begin_attachment(root.path(), "job", &next).unwrap();
        assert!(
            begin_attachment(root.path(), "job", &params).is_err(),
            "a superseded turn cannot reactivate itself"
        );
        assert!(load(root.path(), "job", &info).is_err());
        assert!(publish_attachment(root.path(), "job", "pipe", &info, &params, &old).is_err());
        publish_attachment(root.path(), "job", "pipe", &info, &next, &new).unwrap();
        fs::write(root.path().join(STATE_FILE), "not JSON").unwrap();
        assert!(load(root.path(), "job", &info).is_err());
        assert!(begin_attachment(root.path(), "job", &next).is_err());
        assert!(publish_attachment(root.path(), "job", "pipe", &info, &next, &new).is_err());
    }

    #[test]
    fn failed_invalidation_persistence_revokes_os_lease_and_blocked_lock_does_not_wait() {
        let root = tempfile::tempdir().unwrap();
        fs::write(
            root.path().join("job.json"),
            json!({"id":"job","thread_id":"session","status":"running"}).to_string(),
        )
        .unwrap();
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        let params = json!({"session_id":"session","turn_id":"turn","session_context":"live","networkPolicy":{"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}});
        let old = begin_attachment(root.path(), "job", &params).unwrap();
        publish_attachment(root.path(), "job", "pipe", &info, &params, &old).unwrap();
        let blocked = fs::OpenOptions::new()
            .read(true)
            .write(true)
            .open(root.path().join("chrome-browser-context.lock"))
            .unwrap();
        blocked.lock().unwrap();
        let start = std::time::Instant::now();
        assert!(invalidate(root.path(), &json!("session"), &json!("turn"), true).is_err());
        assert!(start.elapsed() < std::time::Duration::from_secs(1));
        drop(blocked);
        assert!(
            !lease_alive(root.path(), &old),
            "OS revocation does not depend on state lock/write"
        );
        assert!(load(root.path(), "job", &info).is_err());
        assert!(publish_attachment(root.path(), "job", "pipe", &info, &params, &old).is_err());
        assert!(
            begin_attachment(root.path(), "job", &params).is_err(),
            "failed tombstone persistence cannot revive the same turn"
        );
        invalidate(root.path(), &json!("session"), &json!("turn"), true).unwrap();
        let mut next = params.clone();
        next["turn_id"] = json!("next");
        let fresh = begin_attachment(root.path(), "job", &next).unwrap();
        publish_attachment(root.path(), "job", "pipe", &info, &next, &fresh).unwrap();
        release(root.path());
        assert!(!lease_alive(root.path(), &fresh));
        assert!(load(root.path(), "job", &info).is_err());
    }

    #[test]
    fn context_failure_messages_distinguish_safe_fixed_reasons() {
        let root = tempfile::tempdir().unwrap();
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        assert!(
            load(root.path(), "job", &info)
                .unwrap_err()
                .to_string()
                .ends_with("job_missing")
        );
        fs::write(
            root.path().join("job.json"),
            json!({"id":"job","thread_id":"session","status":"running"}).to_string(),
        )
        .unwrap();
        assert!(
            load(root.path(), "job", &info)
                .unwrap_err()
                .to_string()
                .ends_with("not_recorded")
        );
        let params = json!({"session_id":"session","turn_id":"turn","session_context":"live","networkPolicy":{"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}});
        observe_attachment(root.path(), "job", "pipe", &info, &params).unwrap();
        let path = root.path().join(CONTEXT_FILE);
        let bytes = fs::read(&path).unwrap();
        let mut expired: Value = serde_json::from_slice(&bytes).unwrap();
        expired["recorded_at"] = json!(epoch() - 61);
        fs::write(&path, expired.to_string()).unwrap();
        assert!(
            load(root.path(), "job", &info)
                .unwrap_err()
                .to_string()
                .ends_with("expired")
        );
        fs::write(&path, &bytes).unwrap();
        let mut wrong = info.clone();
        wrong["browserTabMentions"]["generation"] = json!("other");
        assert!(
            load(root.path(), "job", &wrong)
                .unwrap_err()
                .to_string()
                .ends_with("identity_mismatch")
        );
        release(root.path());
        assert!(
            load(root.path(), "job", &info)
                .unwrap_err()
                .to_string()
                .ends_with("lease_lost")
        );
        diagnostic(
            root.path(),
            "load",
            &io::Error::other("secret fixture header should not be written"),
        );
        let saved = fs::read_to_string(root.path().join("chrome-context-error.json")).unwrap();
        assert!(!saved.contains("secret fixture"));
        assert!(saved.contains("\"reason\": \"io\""));
    }
}
