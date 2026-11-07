//! Optional remote authority for the legacy shared ToDo byte-lock transaction.
//! A per-cache activation marker is installed only when every writer is upgraded.
//! HTTP and owner-protected credentials belong to the Go bridge, not this crate.
use serde::Deserialize;
use serde_json::{Value, json};
use std::{
    collections::HashMap,
    fs::{self, OpenOptions},
    io::{self, BufRead, BufReader, Read, Write},
    path::{Path, PathBuf},
    process::{Child, ChildStdin, Command, Stdio},
    sync::{Arc, Mutex, OnceLock, mpsc},
    thread::{self, JoinHandle},
    time::{Duration, Instant},
};

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

struct Bridge {
    child: Child,
    input: Option<ChildStdin>,
    replies: mpsc::Receiver<io::Result<Value>>,
    reader: Option<JoinHandle<()>>,
    failed: bool,
    closed: bool,
    unconfirmed: bool,
}
impl Bridge {
    fn spawn(cfg: &Settings) -> io::Result<Self> {
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
        let stdout = child
            .stdout
            .take()
            .ok_or_else(|| error("todo_remote_bridge_pipe_failed"))?;
        let (send, replies) = mpsc::channel();
        let reader = thread::spawn(move || {
            let mut stdout = BufReader::new(stdout);
            loop {
                let mut line = Vec::new();
                let result = (&mut stdout).take(4097).read_until(b'\n', &mut line);
                if matches!(result, Ok(0)) {
                    break;
                }
                let result = match result {
                    Ok(_) if line.len() <= 4096 => serde_json::from_slice(&line)
                        .map_err(|_| error("todo_remote_invalid_control_reply")),
                    _ => Err(error("todo_remote_bridge_read_failed")),
                };
                let failed = result.is_err();
                if send.send(result).is_err() || failed {
                    break;
                }
            }
        });
        Ok(Self {
            child,
            input,
            replies,
            reader: Some(reader),
            failed: false,
            closed: false,
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
        let input = self
            .input
            .as_mut()
            .ok_or_else(|| error("todo_remote_bridge_closed"))?;
        serde_json::to_writer(&mut *input, &request)
            .map_err(|_| error("todo_remote_bridge_write_failed"))?;
        input
            .write_all(b"\n")
            .and_then(|_| input.flush())
            .map_err(|_| error("todo_remote_bridge_write_failed"))?;
        let reply = self
            .replies
            .recv_timeout(timeout)
            .map_err(|_| error("todo_remote_bridge_unavailable"))??;
        if reply["ok"] != true {
            // Never repeat arbitrary helper stdout into a command's output.
            let code = match reply["error"].as_str().unwrap_or("") {
                "todo_remote_http_401" => "todo_remote_http_401",
                "todo_remote_http_403" => "todo_remote_http_403",
                "todo_remote_http_409" => "todo_remote_http_409",
                "todo_remote_http_413" => "todo_remote_http_413",
                "todo_remote_http_423" => "todo_remote_http_423",
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
        Ok(())
    }
    fn close(&mut self) {
        if self.closed {
            return;
        }
        self.closed = true;
        // EOF also asks the bridge to release its lease, even after a failed
        // operation. Keep the byte lock until that process has exited.
        self.input.take();
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            match self.child.try_wait() {
                Ok(Some(_)) => break,
                _ if Instant::now() < deadline => thread::sleep(Duration::from_millis(10)),
                _ => {
                    let _ = self.child.kill();
                    let _ = self.child.wait();
                    break;
                }
            }
        }
        if let Some(reader) = self.reader.take() {
            let _ = reader.join();
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
