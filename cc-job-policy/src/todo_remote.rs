//! Optional remote authority for the legacy shared ToDo byte-lock transaction.
//! A per-cache activation marker is installed only when every writer is upgraded.
//! HTTP and owner-protected credentials belong to the Go bridge, not this crate.
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    collections::HashMap,
    fs::{self, File, OpenOptions},
    io::{self, BufRead, BufReader, Read, Write},
    path::{Path, PathBuf},
    process::{Child, ChildStdin, ChildStdout, Command, Stdio},
    sync::{Arc, Mutex, OnceLock, mpsc},
    thread::{self, JoinHandle},
    time::{Duration, Instant},
};

#[cfg(windows)]
#[path = "todo_remote/capacity_process_windows.rs"]
mod capacity_process;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Settings {
    bridge: PathBuf,
    credentials: PathBuf,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FailureKind {
    CommitUnconfirmed,
    CacheResyncRequired,
}
impl FailureKind {
    pub fn code(self) -> &'static str {
        match self {
            Self::CommitUnconfirmed => "todo_remote_commit_unconfirmed",
            Self::CacheResyncRequired => "todo_remote_cache_resync_required",
        }
    }
}
#[derive(Debug)]
struct RemoteFailure(FailureKind);
impl std::fmt::Display for RemoteFailure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.0.code())
    }
}
impl std::error::Error for RemoteFailure {}
pub fn failure_kind(message: &str) -> Option<FailureKind> {
    let code = message.strip_prefix("todo_store_io: ").unwrap_or(message);
    match code {
        "todo_remote_commit_unconfirmed" | "TodoRemoteCommitUnconfirmed" => {
            Some(FailureKind::CommitUnconfirmed)
        }
        "todo_remote_cache_resync_required" | "TodoRemoteCacheResyncRequired" => {
            Some(FailureKind::CacheResyncRequired)
        }
        _ => None,
    }
}
pub fn io_failure_kind(error: &io::Error) -> Option<FailureKind> {
    error
        .get_ref()
        .and_then(|e| e.downcast_ref::<RemoteFailure>())
        .map(|e| e.0)
        .or_else(|| failure_kind(&error.to_string()))
}
fn error(code: &'static str) -> io::Error {
    if let Some(kind) = failure_kind(code) {
        io::Error::other(RemoteFailure(kind))
    } else {
        io::Error::other(code)
    }
}
fn is_store(path: &Path) -> bool {
    path.file_name().is_some_and(|n| n == "todo.json")
}
fn settings(path: &Path) -> io::Result<Option<Settings>> {
    if !is_store(path) {
        return Ok(None);
    }
    let marker = path.with_file_name("todo.remote.json");
    let file = match fs::File::open(marker) {
        Ok(f) => f,
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            let revision = path.with_file_name("todo.json.remote-revision");
            match fs::metadata(revision) {
                Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(None),
                _ => return Err(error("todo_remote_activation_marker_missing")),
            }
        }
        Err(_) => return Err(error("todo_remote_configuration_unavailable")),
    };
    let mut bytes = Vec::new();
    file.take(16 * 1024 + 1).read_to_end(&mut bytes)?;
    if bytes.len() > 16 * 1024 {
        return Err(error("todo_remote_invalid_configuration"));
    }
    let cfg: Settings =
        serde_json::from_slice(&bytes).map_err(|_| error("todo_remote_invalid_configuration"))?;
    if !cfg.bridge.is_absolute() || !cfg.credentials.is_absolute() {
        return Err(error("todo_remote_configuration_paths_must_be_absolute"));
    }
    Ok(Some(cfg))
}

/// Used only to make a missing remote cache fail closed, never initialize a
/// second local ledger. Old readers use the one shared cache without HTTP.
pub fn enabled(path: &Path) -> io::Result<bool> {
    Ok(settings(path)?.is_some())
}

/// One note and its digest from a single remote read transaction.
#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct CurrentNote {
    pub id: i64,
    pub note: String,
    pub note_sha256: String,
    pub revision: u64,
    pub state_id: String,
}

#[derive(Clone, Copy)]
enum ReplyKind {
    Control,
    CurrentNote,
    CapacityVersion,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NoteSuccess {
    ok: bool,
    #[serde(rename = "currentNote")]
    current_note: CurrentNote,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ReadFailure {
    ok: bool,
    error: String,
}

#[derive(Deserialize)]
#[serde(untagged)]
enum NoteReply {
    Success(NoteSuccess),
    Failure(ReadFailure),
}

#[derive(Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct CacheVersion {
    revision: u64,
    state_id: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct VersionSuccess {
    ok: bool,
    version: CacheVersion,
}

#[derive(Deserialize)]
#[serde(untagged)]
enum VersionReply {
    Success(VersionSuccess),
    Failure(ReadFailure),
}

impl ReplyKind {
    fn limit(self) -> usize {
        match self {
            Self::Control | Self::CapacityVersion => 4096,
            Self::CurrentNote => 6 * crate::todo_store::MAX_BYTES + 8192,
        }
    }
}

fn read_bridge_reply(reader: &mut impl BufRead, kind: ReplyKind) -> io::Result<Option<Value>> {
    let limit = kind.limit();
    let mut line = Vec::new();
    match reader.take(limit as u64 + 1).read_until(b'\n', &mut line) {
        Ok(0) => Ok(None),
        Ok(_) if line.len() <= limit => {
            if matches!(kind, ReplyKind::CapacityVersion) && !line.ends_with(b"\n") {
                return Err(error("todo_remote_invalid_control_reply"));
            }
            let reply = match kind {
                ReplyKind::Control => serde_json::from_slice(&line),
                ReplyKind::CurrentNote => {
                    serde_json::from_slice::<NoteReply>(&line).and_then(|reply| match reply {
                        NoteReply::Success(reply) if reply.ok => {
                            Ok(json!({"ok":true,"currentNote":reply.current_note}))
                        }
                        NoteReply::Failure(reply) if !reply.ok && !reply.error.is_empty() => {
                            Ok(json!({"ok":false,"error":reply.error}))
                        }
                        _ => Err(serde::de::Error::custom("inconsistent note reply")),
                    })
                }
                ReplyKind::CapacityVersion => {
                    serde_json::from_slice::<VersionReply>(&line).and_then(|reply| match reply {
                        VersionReply::Success(reply)
                            if reply.ok && valid_digest(&reply.version.state_id) =>
                        {
                            Ok(json!({"ok":true,"version":reply.version}))
                        }
                        VersionReply::Failure(reply) if !reply.ok && !reply.error.is_empty() => {
                            Ok(json!({"ok":false,"error":reply.error}))
                        }
                        _ => Err(serde::de::Error::custom("inconsistent version reply")),
                    })
                }
            };
            reply
                .map(Some)
                .map_err(|_| error("todo_remote_invalid_control_reply"))
        }
        _ => Err(error("todo_remote_bridge_read_failed")),
    }
}

/// The caller owns todo.lock. Unlike a write guard, this never begins a lease
/// or refreshes the complete local cache.
pub fn current_note_after_lock(path: &Path, id: i64) -> io::Result<Option<CurrentNote>> {
    use sha2::{Digest, Sha256};
    if id <= 0 {
        return Err(error("todo_remote_invalid_note_request"));
    }
    let Some(cfg) = settings(path)? else {
        return Ok(None);
    };
    let mut bridge = Bridge::spawn_with_reply_kind(&cfg, ReplyKind::CurrentNote)?;
    let reply = bridge.exchange_value(
        json!({"op":"current-note","cachePath":std::path::absolute(path)?,"itemId":id}),
        Duration::from_secs(30),
    )?;
    let projection: CurrentNote = serde_json::from_value(
        reply
            .get("currentNote")
            .cloned()
            .ok_or_else(|| error("todo_remote_invalid_control_reply"))?,
    )
    .map_err(|_| error("todo_remote_invalid_control_reply"))?;
    if projection.id != id
        || projection.note.len() > crate::todo_store::MAX_BYTES
        || !valid_digest(&projection.state_id)
        || !valid_digest(&projection.note_sha256)
        || format!("{:x}", Sha256::digest(projection.note.as_bytes())) != projection.note_sha256
    {
        return Err(error("todo_remote_invalid_control_reply"));
    }
    Ok(Some(projection))
}

fn valid_digest(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct CacheRevision {
    revision: u64,
    state_id: String,
    sha256: String,
    bytes: u64,
}

/// The caller holds todo.lock. Return the same cache bytes verified against
/// the cloud version, or None when the store has never enabled cloud sync.
pub(crate) fn verified_capacity_cache_after_lock(
    path: &Path,
) -> Result<Option<Vec<u8>>, crate::todo_store::Error> {
    use crate::todo_store::Error;

    if !is_store(path) {
        return Ok(None);
    }
    if ["todo.json.remote-pending", "todo.json.remote-invalid"]
        .iter()
        .any(|name| !marker_absent(&path.with_file_name(name)))
    {
        return Err(Error::Io(error("todo_remote_cache_revision_mismatch")));
    }

    let Some(cfg) = settings(path).map_err(Error::Io)? else {
        // Do not use the local path if a dangling activation or revision
        // marker was treated as absent when settings followed it.
        if ["todo.remote.json", "todo.json.remote-revision"]
            .iter()
            .any(|name| !marker_absent(&path.with_file_name(name)))
        {
            return Err(Error::Io(error("todo_remote_activation_marker_missing")));
        }
        return Ok(None);
    };
    let absolute = std::path::absolute(path).map_err(Error::Io)?;
    let mut cache = absolute
        .parent()
        .ok_or_else(|| Error::Io(error("todo_remote_cache_unavailable")))?
        .canonicalize()
        .map_err(|_| Error::Io(error("todo_remote_cache_unavailable")))?;
    cache.push("todo.json");
    verify_capacity_cache(&cache, || capacity_version(&cfg, &cache)).map(Some)
}

fn marker_absent(path: &Path) -> bool {
    matches!(fs::symlink_metadata(path), Err(err) if err.kind() == io::ErrorKind::NotFound)
}

// The byte lock keeps the sidecar and cache stable while the server supplies
// the observation point. No server lease is needed to read that snapshot.
fn verify_capacity_cache(
    path: &Path,
    version: impl FnOnce() -> io::Result<CacheVersion>,
) -> Result<Vec<u8>, crate::todo_store::Error> {
    use crate::todo_store::{self, Error};
    use sha2::{Digest, Sha256};

    let mismatch = || Error::Io(error("todo_remote_cache_revision_mismatch"));
    let cache_io = |err: io::Error| {
        if err.kind() == io::ErrorKind::NotFound {
            Error::Io(error("todo_remote_cache_unavailable"))
        } else {
            Error::Io(err)
        }
    };
    let mut sidecar = Vec::new();
    File::open(path.with_file_name("todo.json.remote-revision"))
        .map_err(cache_io)?
        .take(4097)
        .read_to_end(&mut sidecar)
        .map_err(Error::Io)?;
    if sidecar.len() > 4096 {
        return Err(mismatch());
    }
    let cache: CacheRevision = serde_json::from_slice(&sidecar).map_err(|_| mismatch())?;
    if !valid_digest(&cache.state_id)
        || !valid_digest(&cache.sha256)
        || cache.bytes == 0
        || cache.bytes > todo_store::MAX_BYTES as u64
    {
        return Err(mismatch());
    }

    let remote = version().map_err(Error::Io)?;
    if cache.revision != remote.revision || cache.state_id != remote.state_id {
        return Err(mismatch());
    }
    let bytes = todo_store::read_bytes(path).map_err(|err| match err {
        Error::Io(err) => cache_io(err),
        other => other,
    })?;
    if bytes.len() as u64 != cache.bytes || format!("{:x}", Sha256::digest(&bytes)) != cache.sha256 {
        return Err(mismatch());
    }
    Ok(bytes)
}

fn capacity_version(cfg: &Settings, path: &Path) -> io::Result<CacheVersion> {
    let mut bridge = Bridge::spawn_with_reply_kind(cfg, ReplyKind::CapacityVersion)?;
    let request = json!({"op":"capacity-version","cachePath":path});
    let mut input = bridge
        .input
        .take()
        .ok_or_else(|| error("todo_remote_bridge_closed"))?;
    // Keep the deadline effective even if the child stops reading stdin.
    // Closing the pipe after this frame prevents another operation on it.
    let writer = thread::Builder::new()
        .spawn(move || Bridge::send(&mut input, request))
        .map_err(|_| error("todo_remote_bridge_write_failed"))?;
    let reply = bridge.receive(Duration::from_secs(30));
    let clean_exit = bridge.close();
    #[cfg(windows)]
    let written = capacity_process::join_pipe(writer)
        .ok_or_else(|| error("todo_remote_bridge_write_failed"))?;
    #[cfg(not(windows))]
    let written = writer
        .join()
        .map_err(|_| error("todo_remote_bridge_write_failed"))?;
    written?;
    let reply = reply?;
    if !clean_exit {
        return Err(error("todo_remote_bridge_unavailable"));
    }
    if !matches!(bridge.replies.try_recv(), Err(mpsc::TryRecvError::Disconnected)) {
        return Err(error("todo_remote_invalid_control_reply"));
    }
    serde_json::from_value(reply["version"].clone())
        .map_err(|_| error("todo_remote_invalid_control_reply"))
}

type Shared = Arc<Mutex<Bridge>>;
fn sessions() -> &'static Mutex<HashMap<PathBuf, Shared>> {
    static SESSIONS: OnceLock<Mutex<HashMap<PathBuf, Shared>>> = OnceLock::new();
    SESSIONS.get_or_init(|| Mutex::new(HashMap::new()))
}
fn key(path: &Path) -> io::Result<PathBuf> {
    let parent = path
        .parent()
        .ok_or_else(|| error("todo_remote_invalid_path"))?;
    let canonical = fs::canonicalize(parent)?.join("todo.json");
    #[cfg(windows)]
    let canonical = PathBuf::from(canonical.to_string_lossy().to_lowercase());
    Ok(canonical)
}

/// The caller owns the existing offset-0, length-1 OS lock throughout this
/// guard's lifetime. Drop this guard *before* unlocking the local byte.
pub struct Guard {
    key: PathBuf,
    bridge: Shared,
}
impl Guard {
    pub fn after_lock(lock: &Path) -> io::Result<Option<Self>> {
        if lock.file_name().is_none_or(|n| n != "todo.lock") {
            return Ok(None);
        }
        let cache = lock.with_file_name("todo.json");
        let Some(cfg) = settings(&cache)? else {
            return Ok(None);
        };
        let key = key(&cache)?;
        let mut bridge = Bridge::spawn(&cfg)?;
        let cache = std::path::absolute(cache)?;
        bridge.request(
            json!({"op":"begin","cachePath":cache}),
            Duration::from_secs(75),
        )?;
        let bridge = Arc::new(Mutex::new(bridge));
        let mut registry = sessions()
            .lock()
            .map_err(|_| error("todo_remote_registry_poisoned"))?;
        if registry.contains_key(&key) {
            return Err(error("todo_remote_duplicate_guard"));
        }
        registry.insert(key.clone(), bridge.clone());
        Ok(Some(Self { key, bridge }))
    }
}
impl Drop for Guard {
    fn drop(&mut self) {
        if let Ok(mut registry) = sessions().lock()
            && registry
                .get(&self.key)
                .is_some_and(|s| Arc::ptr_eq(s, &self.bridge))
        {
            registry.remove(&self.key);
        }
        if let Ok(mut bridge) = self.bridge.lock() {
            bridge.close();
        }
    }
}

/// Returns true after a confirmed remote commit. A deferred cache refresh is
/// recorded by the bridge and never turns that confirmed domain result into failure.
/// A configured remote store without its live guard is always an error.
pub fn commit(path: &Path, bytes: &[u8]) -> io::Result<bool> {
    if !is_store(path) {
        return Ok(false);
    }
    let active = sessions()
        .lock()
        .map_err(|_| error("todo_remote_registry_poisoned"))?
        .get(&key(path)?)
        .cloned();
    let Some(active) = active else {
        return if enabled(path)? {
            Err(error("todo_remote_guard_required"))
        } else {
            Ok(false)
        };
    };
    let mut bridge = active
        .lock()
        .map_err(|_| error("todo_remote_session_poisoned"))?;
    if bridge.failed {
        if bridge.unconfirmed {
            return Err(error("todo_remote_commit_unconfirmed"));
        }
        return Err(error("todo_remote_session_failed"));
    }
    static SERIAL: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    let serial = SERIAL.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    let candidate = path.with_file_name(format!(".todo-remote-{}-{serial}", std::process::id()));
    let mut file = OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(&candidate)?;
    let _cleanup = Candidate(candidate.clone());
    file.write_all(bytes)?;
    file.sync_all()?;
    drop(file);
    let candidate = std::path::absolute(candidate)?;
    bridge.request(
        json!({"op":"commit","documentPath":candidate}),
        Duration::from_secs(75),
    )?;
    Ok(true)
}
struct Candidate(PathBuf);
impl Drop for Candidate {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

enum BridgeChild {
    Standard(Child),
    #[cfg(windows)]
    Capacity(capacity_process::CapacityProcess),
}

impl BridgeChild {
    fn stop(self) {
        match self {
            Self::Standard(mut child) => {
                let _ = child.kill();
                let _ = child.wait();
            }
            #[cfg(windows)]
            Self::Capacity(child) => child.stop(),
        }
    }
}

fn spawn_standard_bridge(
    cfg: &Settings,
) -> io::Result<(BridgeChild, Option<ChildStdin>, ChildStdout)> {
    let mut cmd = Command::new(&cfg.bridge);
    cmd.arg("session")
        .arg("--config")
        .arg(&cfg.credentials)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x08000000);
    }
    let mut child = cmd
        .spawn()
        .map_err(|_| error("todo_remote_bridge_start_failed"))?;
    let input = child.stdin.take();
    let child_stdout = child.stdout.take();
    let child = BridgeChild::Standard(child);
    let Some(stdout) = child_stdout else {
        drop(input);
        child.stop();
        return Err(error("todo_remote_bridge_pipe_failed"));
    };
    Ok((child, input, stdout))
}

struct Bridge {
    child: Option<BridgeChild>,
    input: Option<ChildStdin>,
    replies: mpsc::Receiver<io::Result<Value>>,
    reader: Option<JoinHandle<()>>,
    failed: bool,
    unconfirmed: bool,
}
impl Bridge {
    fn spawn(cfg: &Settings) -> io::Result<Self> {
        Self::spawn_with_reply_kind(cfg, ReplyKind::Control)
    }
    fn spawn_with_reply_kind(cfg: &Settings, kind: ReplyKind) -> io::Result<Self> {
        let (child, input, stdout) = match kind {
            #[cfg(windows)]
            ReplyKind::CapacityVersion => {
                let (process, input, stdout) =
                    capacity_process::CapacityProcess::spawn(&cfg.bridge, &cfg.credentials)
                        .map_err(|_| error("todo_remote_bridge_start_failed"))?;
                (BridgeChild::Capacity(process), Some(input), stdout)
            }
            _ => spawn_standard_bridge(cfg)?,
        };
        let (send, replies) = mpsc::channel();
        let reader = thread::Builder::new().spawn(move || {
            let mut stdout = BufReader::new(stdout);
            let mut received_version = false;
            loop {
                let result = match read_bridge_reply(&mut stdout, kind) {
                    Ok(None) => break,
                    Ok(Some(reply)) => Ok(reply),
                    Err(failure) => Err(failure),
                };
                let failed = result.is_err();
                if send.send(result).is_err() || failed || received_version {
                    break;
                }
                // A one-shot reply may have a second frame only as evidence
                // of a protocol error. Do not keep buffering extra frames.
                received_version = matches!(kind, ReplyKind::CapacityVersion);
            }
        });
        let reader = match reader {
            Ok(reader) => reader,
            Err(_) => {
                drop(input);
                child.stop();
                return Err(error("todo_remote_bridge_pipe_failed"));
            }
        };
        Ok(Self {
            child: Some(child),
            input,
            replies,
            reader: Some(reader),
            failed: false,
            unconfirmed: false,
        })
    }
    fn request(&mut self, request: Value, timeout: Duration) -> io::Result<()> {
        let committing = request["op"] == "commit";
        let result = self.exchange(request, timeout).map_err(|e| {
            if committing
                && matches!(
                    e.to_string().as_str(),
                    "todo_remote_bridge_write_failed"
                        | "todo_remote_bridge_read_failed"
                        | "todo_remote_bridge_unavailable"
                        | "todo_remote_invalid_control_reply"
                        | "todo_remote_bridge_closed"
                )
            {
                error("todo_remote_commit_unconfirmed")
            } else {
                e
            }
        });
        if result.is_err() {
            self.failed = true;
            self.unconfirmed = result
                .as_ref()
                .err()
                .is_some_and(|e| io_failure_kind(e) == Some(FailureKind::CommitUnconfirmed));
        }
        result
    }
    fn exchange(&mut self, request: Value, timeout: Duration) -> io::Result<()> {
        self.exchange_value(request, timeout).map(|_| ())
    }
    fn exchange_value(&mut self, request: Value, timeout: Duration) -> io::Result<Value> {
        let input = self
            .input
            .as_mut()
            .ok_or_else(|| error("todo_remote_bridge_closed"))?;
        Self::send(input, request)?;
        self.receive(timeout)
    }
    fn send(input: &mut ChildStdin, request: Value) -> io::Result<()> {
        serde_json::to_writer(&mut *input, &request)
            .map_err(|_| error("todo_remote_bridge_write_failed"))?;
        input
            .write_all(b"\n")
            .and_then(|_| input.flush())
            .map_err(|_| error("todo_remote_bridge_write_failed"))
    }
    fn receive(&self, timeout: Duration) -> io::Result<Value> {
        let reply = self
            .replies
            .recv_timeout(timeout)
            .map_err(|_| error("todo_remote_bridge_unavailable"))??;
        if reply["ok"] != true {
            // Never repeat arbitrary helper stdout into a command's output.
            let code = match reply["error"].as_str().unwrap_or("") {
                "todo_remote_http_401" => "todo_remote_http_401",
                "todo_remote_http_403" => "todo_remote_http_403",
                "todo_remote_http_404" => "todo_remote_http_404",
                "todo_remote_http_409" => "todo_remote_http_409",
                "todo_remote_http_413" => "todo_remote_http_413",
                "todo_remote_http_423" => "todo_remote_http_423",
                "todo_remote_http_500" => "todo_remote_http_500",
                "todo_remote_network_failed" => "todo_remote_network_failed",
                "todo_remote_invalid_response" => "todo_remote_invalid_response",
                "todo_remote_document_too_large" => "todo_remote_document_too_large",
                "todo_remote_commit_unconfirmed" => "todo_remote_commit_unconfirmed",
                "todo_remote_cache_resync_required" => "todo_remote_cache_resync_required",
                "todo_remote_committed_cache_refresh_required" => {
                    "todo_remote_committed_cache_refresh_required"
                }
                "todo_remote_lease_expired" => "todo_remote_lease_expired",
                "todo_remote_lease_renew_failed" => "todo_remote_lease_renew_failed",
                "todo_remote_snapshot_revision_mismatch" => {
                    "todo_remote_snapshot_revision_mismatch"
                }
                "todo_remote_cache_ahead_of_server" => "todo_remote_cache_ahead_of_server",
                "todo_remote_change_gap" => "todo_remote_change_gap",
                "todo_remote_invalid_cursor" => "todo_remote_invalid_cursor",
                _ => "todo_remote_operation_failed",
            };
            return Err(error(code));
        }
        Ok(reply)
    }
    fn close(&mut self) -> bool {
        let Some(child) = self.child.take() else {
            return false;
        };
        // EOF also asks the bridge to release its lease, even after a failed
        // operation. Keep the byte lock until that process has exited.
        self.input.take();
        let reader = self.reader.take();
        match child {
            #[cfg(windows)]
            BridgeChild::Capacity(child) => {
                let clean_exit = child.finish();
                let reader_joined = reader.and_then(capacity_process::join_pipe).is_some();
                clean_exit && reader_joined
            }
            BridgeChild::Standard(mut child) => {
                let deadline = Instant::now() + Duration::from_secs(5);
                let clean_exit = loop {
                    match child.try_wait() {
                        Ok(Some(status)) => break status.success(),
                        _ if Instant::now() < deadline => {
                            thread::sleep(Duration::from_millis(10));
                        }
                        _ => {
                            let _ = child.kill();
                            let _ = child.wait();
                            break false;
                        }
                    }
                };
                let reader_joined = reader.is_some_and(|reader| reader.join().is_ok());
                clean_exit && reader_joined
            }
        }
    }
}
impl Drop for Bridge {
    fn drop(&mut self) {
        self.close();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn note_projection() -> Value {
        use sha2::{Digest, Sha256};
        json!({"id":7,"note":"fixture","noteSha256":format!("{:x}",Sha256::digest(b"fixture")),
            "revision":3,"stateId":"a".repeat(64)})
    }

    #[test]
    fn current_note_bridge_reader_rejects_duplicate_and_inconsistent_envelopes() {
        let projection = note_projection().to_string();
        let duplicate_note = projection.replacen(
            "\"note\":\"fixture\"",
            "\"note\":\"old\",\"note\":\"fixture\"",
            1,
        );
        let duplicate_id = projection.replacen("\"id\":7", "\"id\":8,\"id\":7", 1);
        for raw in [
            format!(r#"{{"ok":false,"ok":true,"currentNote":{projection}}}"#),
            format!(r#"{{"ok":true,"currentNote":{projection},"currentNote":{projection}}}"#),
            format!(r#"{{"ok":true,"currentNote":{duplicate_note}}}"#),
            format!(r#"{{"ok":true,"currentNote":{duplicate_id}}}"#),
            format!(r#"{{"ok":true,"currentNote":{projection},"unknown":0}}"#),
            format!(r#"{{"ok":false,"error":"todo_remote_http_404","currentNote":{projection}}}"#),
            format!(r#"{{"ok":true,"error":"todo_remote_http_404","currentNote":{projection}}}"#),
            format!(r#"{{"ok":true,"error":null,"currentNote":{projection}}}"#),
            r#"{"ok":true}"#.to_owned(),
            r#"{"ok":false,"error":"one","error":"two"}"#.to_owned(),
            r#"{"ok":false,"error":null}"#.to_owned(),
            r#"{"ok":true,"currentNote":null}"#.to_owned(),
        ] {
            assert!(
                read_bridge_reply(&mut std::io::Cursor::new(raw), ReplyKind::CurrentNote).is_err(),
                "inconsistent note reply was accepted"
            );
        }
    }

    #[test]
    fn current_note_bridge_reader_preserves_valid_reply_and_control_cap() {
        let projection = note_projection();
        let raw = json!({"ok":true,"currentNote":projection}).to_string();
        let reply = read_bridge_reply(&mut std::io::Cursor::new(raw), ReplyKind::CurrentNote)
            .unwrap()
            .unwrap();
        assert_eq!(reply["ok"], true);
        assert_eq!(reply["currentNote"], projection);
        let failure = r#"{"ok":false,"error":"todo_remote_http_404"}"#;
        let reply = read_bridge_reply(&mut std::io::Cursor::new(failure), ReplyKind::CurrentNote)
            .unwrap()
            .unwrap();
        assert_eq!(reply["ok"], false);
        assert_eq!(reply["error"], "todo_remote_http_404");
        let mut projection = projection;
        projection["note"] = json!("x".repeat(5000));
        let raw = json!({"ok":true,"currentNote":projection}).to_string();
        assert!(read_bridge_reply(&mut std::io::Cursor::new(&raw), ReplyKind::CurrentNote).is_ok());
        assert!(read_bridge_reply(&mut std::io::Cursor::new(&raw), ReplyKind::Control).is_err());
        let reply = read_bridge_reply(
            &mut std::io::Cursor::new(r#"{"ok":true,"revision":2}"#),
            ReplyKind::Control,
        )
        .unwrap()
        .unwrap();
        assert_eq!(reply["revision"], 2);
    }

    #[test]
    fn capacity_version_requires_one_explicit_identity_and_newline() {
        let state = "a".repeat(64);
        let frame = format!("{{\"ok\":true,\"version\":{{\"revision\":0,\"stateId\":\"{state}\"}}}}\n");
        let reply = read_bridge_reply(&mut std::io::Cursor::new(&frame), ReplyKind::CapacityVersion)
            .unwrap()
            .unwrap();
        assert_eq!(reply["version"]["revision"], 0);
        for malformed in [
            frame.trim_end().to_string(),
            frame.replace("\"revision\":0,", ""),
            frame.replace("\"revision\":0", "\"revision\":null"),
            frame.replace("\"revision\":0", "\"revision\":\"0\""),
            frame.replace("\"revision\":0", "\"revision\":1,\"revision\":0"),
            frame.replace("\"revision\":0", "\"Revision\":0"),
            frame.replace("\"revision\":0", "\"revision\":18446744073709551616"),
            frame.replace(state.as_str(), ""),
            frame.replace("\"ok\":true", "\"ok\":false"),
            frame.replace("\"ok\":true", "\"ok\":true,\"error\":\"unexpected\""),
            frame.replace('\n', "{}\n"),
        ] {
            assert!(
                read_bridge_reply(&mut std::io::Cursor::new(malformed), ReplyKind::CapacityVersion).is_err()
            );
        }
    }

    #[test]
    fn capacity_cache_accepts_only_the_matching_verified_buffer() {
        use sha2::{Digest, Sha256};

        let home = tempfile::tempdir().unwrap();
        let path = home.path().join("todo.json");
        let sidecar = home.path().join("todo.json.remote-revision");
        let source = b"\xef\xbb\xbf{\"items\":[]}";
        let state = "a".repeat(64);
        fs::write(&path, source).unwrap();
        let mut metadata = json!({
            "revision": 9007199254740993u64,
            "stateId": state,
            "sha256": format!("{:x}", Sha256::digest(source)),
            "bytes": source.len(),
        });
        let revision = 9007199254740993;
        let version = || Ok(CacheVersion { revision, state_id: state.clone() });
        fs::write(&sidecar, metadata.to_string()).unwrap();
        assert_eq!(verify_capacity_cache(&path, version).unwrap().as_slice(), source);
        let mismatch = verify_capacity_cache(&path, || Ok(CacheVersion {
            revision,
            state_id: "b".repeat(64),
        })).unwrap_err();
        assert!(mismatch.to_string().contains("todo_remote_cache_revision_mismatch"));
        metadata.as_object_mut().unwrap().remove("bytes");
        fs::write(&sidecar, metadata.to_string()).unwrap();
        assert!(
            verify_capacity_cache(&path, || panic!("invalid metadata reached the bridge")).is_err()
        );
        assert_eq!(fs::read(&path).unwrap(), source);
    }

    #[test]
    fn capacity_cache_pending_or_invalid_never_enters_local_mode() {
        let home = tempfile::tempdir().unwrap();
        let path = home.path().join("todo.json");
        assert!(verified_capacity_cache_after_lock(&path).unwrap().is_none());

        for name in ["todo.json.remote-pending", "todo.json.remote-invalid", "todo.json.remote-revision"] {
            let marker = home.path().join(name);
            fs::write(&marker, b"not read as data").unwrap();
            assert!(verified_capacity_cache_after_lock(&path).is_err());
            assert_eq!(fs::read(&marker).unwrap(), b"not read as data");
            fs::remove_file(&marker).unwrap();
        }
    }

    #[test]
    fn uncertain_outcome_remains_typed_through_store_and_string_boundaries() {
        let uncertain = error("todo_remote_commit_unconfirmed");
        assert_eq!(
            io_failure_kind(&uncertain),
            Some(FailureKind::CommitUnconfirmed)
        );
        let wrapped = crate::todo_store::Error::Io(uncertain);
        assert_eq!(
            failure_kind(&wrapped.to_string()),
            Some(FailureKind::CommitUnconfirmed)
        );
        assert_eq!(
            failure_kind("TodoRemoteCacheResyncRequired"),
            Some(FailureKind::CacheResyncRequired)
        );
        assert_eq!(
            failure_kind("validation failed: todo_remote_commit_unconfirmed"),
            None
        );
    }
    #[test]
    fn activation_is_per_cache_and_missing_marker_cannot_reenable_local_writes() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("todo.json");
        assert!(!enabled(&path).unwrap());
        assert!(!commit(&path, b"{}").unwrap());
        fs::write(path.with_file_name("todo.json.remote-revision"), b"{}").unwrap();
        assert!(enabled(&path).is_err());
        assert!(commit(&path, b"{}").is_err());
        assert!(!enabled(&dir.path().join("state.json")).unwrap());
    }
    #[test]
    fn invalid_or_unlocked_remote_configuration_never_touches_cache() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("todo.json");
        let marker = path.with_file_name("todo.remote.json");
        fs::write(&path, b"original-cache").unwrap();
        fs::write(&marker, b"{").unwrap();
        assert!(commit(&path, b"{}").is_err());
        fs::write(&marker, serde_json::to_vec(&json!({
            "bridge":dir.path().join("bridge.exe"), "credentials":dir.path().join("config.dpapi")
        })).unwrap()).unwrap();
        assert!(enabled(&path).unwrap());
        assert!(commit(&path, b"{}").is_err());
        assert_eq!(fs::read(&path).unwrap(), b"original-cache");
        assert!(
            Guard::after_lock(&dir.path().join("state.lock"))
                .unwrap()
                .is_none()
        );
    }
}
