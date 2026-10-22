//! Runtime-supplied native tool receipts, separate from managed CC jobs.
//! This receiver never opens session transcripts or accepts message/status prose.
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    collections::BTreeMap,
    fs,
    io::{Read, Write},
    path::PathBuf,
    sync::{
        Mutex,
        atomic::{AtomicU64, Ordering},
    },
};

pub const FRESH_MS: u64 = 30_000;
const MAX_BYTES: u64 = 2 * 1024 * 1024;
static TEMP: AtomicU64 = AtomicU64::new(0);

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Receipt {
    actual_at_ms: u64,
    event_id: String,
    tool: String,
    kind: String,
    exit_code: Option<i32>,
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Entry {
    generation: u64,
    todo_id: u64,
    role: String,
    business_project: Option<String>,
    bound_at_ms: u64,
    min_activity_at_ms: u64,
    receipt: Option<Receipt>,
    closed_at_ms: Option<u64>,
    outcome: Option<String>,
    #[serde(default)]
    runtime: Option<crate::native_runtime::Cursor>,
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Ledger {
    version: u32,
    #[serde(default = "initial_generation")]
    next_generation: u64,
    entries: BTreeMap<String, Entry>,
}
fn initial_generation() -> u64 {
    1
}

impl Default for Ledger {
    fn default() -> Self {
        Self {
            version: 1,
            next_generation: 1,
            entries: BTreeMap::new(),
        }
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Register {
    pub session_id: String,
    pub todo_id: u64,
    pub role: String,
    #[serde(default)]
    pub business_project: Option<String>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Activity {
    pub session_id: String,
    pub generation: u64,
    pub actual_at_ms: u64,
    pub event_id: String,
    pub tool: String,
    pub kind: String,
    pub executed: bool,
    #[serde(default)]
    pub exit_code: Option<i32>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Close {
    pub session_id: String,
    pub generation: u64,
    pub ended_at_ms: u64,
    pub outcome: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Connect {
    pub session_id: String,
    pub generation: u64,
    pub rollout_path: PathBuf,
}

pub struct Registry {
    path: PathBuf,
    ledger: Mutex<Result<Ledger, &'static str>>,
    writes: Mutex<()>,
    runtime_cursor: AtomicU64,
    runtime_error: Mutex<Option<&'static str>>,
}

fn session_id(value: &str) -> Result<String, &'static str> {
    if value.len() != 36
        || !value.bytes().enumerate().all(|(i, b)| {
            if [8, 13, 18, 23].contains(&i) {
                b == b'-'
            } else {
                b.is_ascii_hexdigit()
            }
        })
    {
        return Err("invalid_native_session");
    }
    Ok(value.to_ascii_lowercase())
}

fn opaque(id: &str, generation: u64) -> String {
    let digest = ring::digest::digest(
        &ring::digest::SHA256,
        format!("native:{id}:{generation}").as_bytes(),
    );
    format!(
        "native-{}",
        digest
            .as_ref()
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect::<String>()
    )
}

fn response(id: &str, entry: &Entry) -> Value {
    json!({"ok":true,"source":"native","executionKey":opaque(id,entry.generation),"generation":entry.generation,
        "todoId":entry.todo_id,"state":if entry.closed_at_ms.is_some(){"closed"}else if entry.receipt.is_some(){"observed"}else{"registered"}})
}

impl Registry {
    pub fn new(root: &std::path::Path) -> Self {
        let path = root.join("agent-progress/native-activity.json");
        let load = || -> Result<Ledger, &'static str> {
            let mut file = match fs::File::open(&path) {
                Ok(file) => file,
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                    return Ok(Ledger::default());
                }
                Err(_) => return Err("native_store_unavailable"),
            };
            if fs::symlink_metadata(&path)
                .map_err(|_| "native_store_unavailable")?
                .file_type()
                .is_symlink()
            {
                return Err("native_store_unavailable");
            }
            let mut bytes = Vec::new();
            Read::by_ref(&mut file)
                .take(MAX_BYTES + 1)
                .read_to_end(&mut bytes)
                .map_err(|_| "native_store_unavailable")?;
            if bytes.len() as u64 > MAX_BYTES {
                return Err("native_store_unavailable");
            }
            let ledger: Ledger =
                serde_json::from_slice(&bytes).map_err(|_| "native_store_unavailable")?;
            if ledger.version != 1 || ledger.entries.len() > 2048 {
                return Err("native_store_unavailable");
            }
            Ok(ledger)
        };
        let ledger = load();
        Self {
            path,
            ledger: Mutex::new(ledger),
            writes: Mutex::new(()),
            runtime_cursor: AtomicU64::new(0),
            runtime_error: Mutex::new(None),
        }
    }

    fn update(
        &self,
        change: impl FnOnce(&mut Ledger) -> Result<(Value, bool), &'static str>,
    ) -> Result<Value, &'static str> {
        let _writer = self.writes.lock().unwrap_or_else(|e| e.into_inner());
        let mut next = self
            .ledger
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .as_ref()
            .map_err(|error| *error)?
            .clone();
        let (result, changed) = change(&mut next)?;
        if !changed {
            return Ok(result);
        }
        let bytes = serde_json::to_vec(&next).map_err(|_| "native_store_unavailable")?;
        if bytes.len() as u64 > MAX_BYTES {
            return Err("native_store_full");
        }
        fs::create_dir_all(self.path.parent().ok_or("native_store_unavailable")?)
            .map_err(|_| "native_store_unavailable")?;
        let temporary = self.path.with_extension(format!(
            "{}-{}.tmp",
            std::process::id(),
            TEMP.fetch_add(1, Ordering::Relaxed)
        ));
        let save = || -> Result<(), std::io::Error> {
            let mut file = fs::OpenOptions::new()
                .create_new(true)
                .write(true)
                .open(&temporary)?;
            file.write_all(&bytes)?;
            file.sync_all()?;
            drop(file);
            fs::rename(&temporary, &self.path)
        };
        if save().is_err() {
            let _ = fs::remove_file(&temporary);
            return Err("native_store_unavailable");
        }
        *self.ledger.lock().unwrap_or_else(|e| e.into_inner()) = Ok(next);
        Ok(result)
    }

    fn archive_closed(&self, entries: BTreeMap<String, Entry>) -> Result<(), &'static str> {
        if entries.is_empty() {
            return Ok(());
        }
        let bytes = serde_json::to_vec(&Ledger {
            version: 1,
            entries,
            ..Ledger::default()
        })
        .map_err(|_| "native_store_unavailable")?;
        let directory = self.path.with_file_name("native-activity-closed");
        match fs::symlink_metadata(&directory) {
            Ok(meta) if !meta.is_dir() || meta.file_type().is_symlink() => {
                return Err("native_store_unavailable");
            }
            Err(error) if error.kind() != std::io::ErrorKind::NotFound => {
                return Err("native_store_unavailable");
            }
            _ => (),
        }
        fs::create_dir_all(&directory).map_err(|_| "native_store_unavailable")?;
        let hash = ring::digest::digest(&ring::digest::SHA256, &bytes)
            .as_ref()
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect::<String>();
        let path = directory.join(format!("{hash}.json"));
        let mut temporary =
            tempfile::NamedTempFile::new_in(&directory).map_err(|_| "native_store_unavailable")?;
        temporary
            .write_all(&bytes)
            .and_then(|_| temporary.as_file().sync_all())
            .map_err(|_| "native_store_unavailable")?;
        match temporary.persist_noclobber(&path) {
            Ok(_) => (),
            Err(error) if error.error.kind() == std::io::ErrorKind::AlreadyExists => {
                let meta = fs::symlink_metadata(&path).map_err(|_| "native_store_unavailable")?;
                if !meta.is_file()
                    || meta.file_type().is_symlink()
                    || meta.len() != bytes.len() as u64
                    || fs::read(&path).map_err(|_| "native_store_unavailable")? != bytes
                {
                    return Err("native_store_unavailable");
                }
            }
            Err(_) => return Err("native_store_unavailable"),
        }
        Ok(())
    }

    pub fn register(&self, request: Register, now: u64) -> Result<Value, &'static str> {
        let id = session_id(&request.session_id)?;
        if request.todo_id == 0
            || !matches!(
                request.role.as_str(),
                "work" | "review" | "audit" | "supervision"
            )
        {
            return Err("invalid_native_binding");
        }
        self.update(|ledger| {
            if let Some(entry) = ledger.entries.get(&id) {
                if entry.closed_at_ms.is_none()
                    && entry.todo_id == request.todo_id
                    && entry.role == request.role
                    && entry.business_project == request.business_project
                {
                    return Ok((response(&id, entry), false));
                }
            }
            // Persist a global epoch before reclaiming UUIDs: an archived UUID
            // must never restart at generation 1 and accept an old receipt.
            let generation = ledger.next_generation.max(
                ledger
                    .entries
                    .values()
                    .map(|entry| entry.generation)
                    .max()
                    .unwrap_or(0)
                    .checked_add(1)
                    .ok_or("native_generation_exhausted")?,
            );
            ledger.next_generation = generation
                .checked_add(1)
                .ok_or("native_generation_exhausted")?;
            if ledger.entries.len() >= 2048 && !ledger.entries.contains_key(&id) {
                let mut closed: Vec<_> = ledger
                    .entries
                    .iter()
                    .filter_map(|(id, entry)| entry.closed_at_ms.map(|at| (at, id.clone())))
                    .collect();
                closed.sort();
                let archived: BTreeMap<_, _> = closed
                    .into_iter()
                    .take(128)
                    .map(|(_, id)| (id.clone(), ledger.entries[&id].clone()))
                    .collect();
                if archived.is_empty() {
                    return Err("native_store_full");
                }
                self.archive_closed(archived.clone())?;
                for id in archived.keys() {
                    ledger.entries.remove(id);
                }
            }
            let previous = ledger.entries.get(&id);
            if let Some(old) = previous.filter(|entry| entry.closed_at_ms.is_some()) {
                self.archive_closed(BTreeMap::from([(id.clone(), old.clone())]))?;
            }
            let entry = Entry {
                generation,
                todo_id: request.todo_id,
                role: request.role,
                business_project: request.business_project,
                bound_at_ms: now,
                min_activity_at_ms: now,
                receipt: None,
                closed_at_ms: None,
                outcome: None,
                runtime: None,
            };
            let result = response(&id, &entry);
            ledger.entries.insert(id, entry);
            Ok((result, true))
        })
    }

    pub fn activity(&self, request: Activity, now: u64) -> Result<Value, &'static str> {
        let id = session_id(&request.session_id)?;
        if !request.executed
            || request.actual_at_ms > now
            || request.event_id.is_empty()
            || request.event_id.len() > 128
            || !request
                .event_id
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"-_:".contains(&b))
            || !matches!(
                request.tool.as_str(),
                "exec_command" | "apply_patch" | "cua_repl" | "runtime_tool_metadata"
            )
            || !matches!(
                request.kind.as_str(),
                "edit" | "test" | "build" | "inspect" | "publish" | "review"
            )
        {
            return Err("invalid_native_tool_receipt");
        }
        self.update(|ledger| {
            let entry = ledger
                .entries
                .get_mut(&id)
                .ok_or("native_binding_missing")?;
            if entry.generation != request.generation
                || entry.closed_at_ms.is_some()
                || request.actual_at_ms < entry.min_activity_at_ms
            {
                return Err("native_binding_changed");
            }
            if let Some(old) = &entry.receipt {
                if old.event_id == request.event_id {
                    if old.actual_at_ms == request.actual_at_ms
                        && old.tool == request.tool
                        && old.kind == request.kind
                        && old.exit_code == request.exit_code
                    {
                        return Ok((response(&id, entry), false));
                    }
                    return Err("native_event_conflict");
                }
                if request.actual_at_ms < old.actual_at_ms {
                    return Ok((response(&id, entry), false));
                }
            }
            entry.receipt = Some(Receipt {
                actual_at_ms: request.actual_at_ms,
                event_id: request.event_id,
                tool: request.tool,
                kind: request.kind,
                exit_code: request.exit_code,
            });
            Ok((response(&id, entry), true))
        })
    }

    pub fn close(&self, request: Close, now: u64) -> Result<Value, &'static str> {
        let id = session_id(&request.session_id)?;
        if request.ended_at_ms > now
            || !matches!(
                request.outcome.as_str(),
                "success" | "waiting" | "failed" | "cancelled" | "refused"
            )
        {
            return Err("invalid_native_end");
        }
        self.update(|ledger| {
            let entry = ledger
                .entries
                .get_mut(&id)
                .ok_or("native_binding_missing")?;
            if entry.generation != request.generation
                || request.ended_at_ms < entry.bound_at_ms
                || entry
                    .receipt
                    .as_ref()
                    .is_some_and(|r| request.ended_at_ms < r.actual_at_ms)
            {
                return Err("native_binding_changed");
            }
            if entry.closed_at_ms.is_some() {
                return Ok((response(&id, entry), false));
            }
            entry.closed_at_ms = Some(request.ended_at_ms);
            entry.outcome = Some(request.outcome);
            Ok((response(&id, entry), true))
        })
    }

    pub fn view(&self, now: u64) -> Value {
        let runtime_failed = self
            .runtime_error
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .is_some();
        let guard = self.ledger.lock().unwrap_or_else(|e| e.into_inner());
        let Ok(ledger) = guard.as_ref() else {
            return json!({"source":"native","state":"unavailable","complete":false,"sessions":[],"bindings":[]});
        };
        let mut sessions = Vec::new();
        let mut bindings = Vec::new();
        for (id, entry) in &ledger.entries {
            let key = opaque(id, entry.generation);
            bindings.push(json!({"key":key,"generation":entry.generation,"todoId":entry.todo_id,"businessProject":entry.business_project,"role":entry.role,
                "state":if entry.closed_at_ms.is_some(){"closed"}else if entry.receipt.is_none(){"unobserved"}else{"observed"},
                "lastActualAt":entry.receipt.as_ref().map(|r|r.actual_at_ms),"endedAt":entry.closed_at_ms,"outcome":entry.outcome}));
            if entry.closed_at_ms.is_some()
                || !matches!(entry.role.as_str(), "work" | "review")
                || entry
                    .runtime
                    .as_ref()
                    .is_some_and(|source| source.error.is_some())
                || (runtime_failed && entry.runtime.is_some())
            {
                continue;
            }
            if let Some(receipt) = &entry.receipt {
                if receipt.actual_at_ms <= now && now - receipt.actual_at_ms <= FRESH_MS {
                    sessions.push(json!({"key":key,"source":"native","role":entry.role,"todoId":entry.todo_id,"actualAt":receipt.actual_at_ms,
                        "validUntil":receipt.actual_at_ms.saturating_add(FRESH_MS),"tool":receipt.tool,"kind":receipt.kind,
                        "verification":"runtime_tool_receipt","generation":entry.generation}));
                }
            }
        }
        let mut issues: Vec<_> = ledger
            .entries
            .values()
            .filter(|entry| {
                entry.closed_at_ms.is_none() && matches!(entry.role.as_str(), "work" | "review")
            })
            .filter_map(|entry| entry.runtime.as_ref())
            .filter_map(|source| {
                source
                    .error
                    .as_ref()
                    .map(|error| json!({"reason":error,"lastSuccessAt":source.last_success_at}))
            })
            .collect();
        if self
            .runtime_error
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .is_some()
        {
            issues.push(json!({"reason":"record_failed","lastSuccessAt":null}));
        }
        json!({"source":"native","state":if issues.is_empty(){"complete"}else{"partial"},"complete":issues.is_empty(),
            "verifiedCount":sessions.len(),"sessions":sessions,"bindings":bindings,"knownSources":ledger.entries.len(),"scope":"connected_sessions",
            "observedAt":now,"collectionIssues":issues,
            "runtimeConnections":ledger.entries.values().filter(|entry|entry.closed_at_ms.is_none() && entry.runtime.is_some()).count(),
            "globalInventoryComplete":false})
    }

    pub fn connect(&self, request: Connect, now: u64) -> Result<Value, &'static str> {
        let profile = std::env::var_os("USERPROFILE")
            .or_else(|| std::env::var_os("HOME"))
            .ok_or("native_runtime_source_unavailable")?;
        self.connect_at(
            request,
            &PathBuf::from(profile).join(".codex/sessions"),
            now,
        )
    }

    fn connect_at(
        &self,
        request: Connect,
        root: &std::path::Path,
        now: u64,
    ) -> Result<Value, &'static str> {
        let id = session_id(&request.session_id)?;
        self.update(|ledger| {
            let entry = ledger
                .entries
                .get_mut(&id)
                .ok_or("native_binding_missing")?;
            if entry.generation != request.generation || entry.closed_at_ms.is_some() {
                return Err("native_binding_changed");
            }
            let source =
                crate::native_runtime::Cursor::attach(&request.rollout_path, root, &id, now)
                    .map_err(|_| "native_runtime_source_unavailable")?;
            if entry
                .runtime
                .as_ref()
                .is_some_and(|current| current.same_source(&source))
            {
                return Ok((response(&id, entry), false));
            }
            entry.runtime = Some(source);
            entry.min_activity_at_ms = now;
            entry.receipt = None;
            Ok((response(&id, entry), true))
        })
    }

    pub fn refresh_runtime(&self, now: u64) {
        let start = self.runtime_cursor.fetch_add(8, Ordering::Relaxed) as usize;
        let result = self.update(|ledger| {
            let keys: Vec<_> = ledger
                .entries
                .iter()
                .filter(|(_, entry)| entry.closed_at_ms.is_none() && entry.runtime.is_some())
                .map(|(id, _)| id.clone())
                .collect();
            if keys.is_empty() {
                return Ok((Value::Null, false));
            }
            let mut changed = false;
            for key in keys
                .iter()
                .cycle()
                .skip(start % keys.len())
                .take(keys.len().min(8))
            {
                let entry = ledger.entries.get_mut(key).unwrap();
                let runtime = entry.runtime.as_mut().unwrap();
                match runtime.poll(entry.min_activity_at_ms, now) {
                    Ok(observed) => {
                        changed |= observed.changed;
                        if let Some((at, call)) = observed.receipt.filter(|(at, _)| {
                            entry
                                .receipt
                                .as_ref()
                                .is_none_or(|old| *at >= old.actual_at_ms)
                        }) {
                            entry.receipt = Some(Receipt {
                                actual_at_ms: at,
                                event_id: format!("runtime:{call}"),
                                tool: "runtime_tool_metadata".into(),
                                kind: "inspect".into(),
                                exit_code: None,
                            });
                            changed = true;
                        }
                        if let Some((at, outcome)) = observed.ended {
                            entry.closed_at_ms = Some(at);
                            entry.outcome = Some(outcome.into());
                            changed = true;
                        }
                    }
                    Err(error) => {
                        if runtime.error.as_deref() != Some(error) {
                            runtime.error = Some(error.into());
                            changed = true;
                        }
                    }
                }
            }
            Ok((Value::Null, changed))
        });
        *self.runtime_error.lock().unwrap_or_else(|e| e.into_inner()) = result.err();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    const ID: &str = "00000000-0000-7000-8000-000000000001";
    #[test]
    fn connected_runtime_collects_one_real_result_then_terminal_zero_without_manual_activity() {
        use std::io::Write;
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        let source_root = temp.path().join("sessions");
        fs::create_dir(&source_root).unwrap();
        let source = source_root.join(format!("rollout-{ID}.jsonl"));
        fs::write(
            &source,
            format!("{{\"type\":\"session_meta\",\"payload\":{{\"id\":\"{ID}\"}}}}\n"),
        )
        .unwrap();
        register(&registry, 100);
        registry
            .connect_at(
                Connect {
                    session_id: ID.into(),
                    generation: 1,
                    rollout_path: source.clone(),
                },
                &source_root,
                101,
            )
            .unwrap();
        assert_eq!(registry.view(101)["verifiedCount"], 0);
        let append = |value: Value| {
            writeln!(
                fs::OpenOptions::new().append(true).open(&source).unwrap(),
                "{value}"
            )
            .unwrap();
        };
        append(
            json!({"timestamp":"1970-01-01T00:00:00.102Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec_command","call_id":"real_call"}}),
        );
        append(
            json!({"timestamp":"1970-01-01T00:00:00.103Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"real_call","output":[{"type":"input_text","text":"Script completed"}]}}),
        );
        registry.refresh_runtime(104);
        let live = registry.view(104);
        assert_eq!(live["verifiedCount"], 1);
        assert_eq!(live["runtimeConnections"], 1);
        registry
            .connect_at(
                Connect {
                    session_id: ID.into(),
                    generation: 1,
                    rollout_path: source.clone(),
                },
                &source_root,
                105,
            )
            .unwrap();
        assert_eq!(
            registry.view(105)["verifiedCount"],
            1,
            "repeated connection cannot discard a real receipt"
        );
        registry.refresh_runtime(105);
        assert_eq!(registry.view(105)["verifiedCount"], 1);
        append(
            json!({"timestamp":"1970-01-01T00:00:00.106Z","type":"event_msg","payload":{"type":"task_complete"}}),
        );
        registry.refresh_runtime(107);
        assert_eq!(registry.view(107)["verifiedCount"], 0);
        assert_eq!(registry.view(107)["bindings"][0]["state"], "closed");
        assert!(!live.to_string().contains(source.to_str().unwrap()));
    }
    #[test]
    fn connected_runtime_read_failure_is_not_reported_as_a_successful_zero() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        let source_root = temp.path().join("sessions");
        fs::create_dir(&source_root).unwrap();
        let source = source_root.join(format!("rollout-{ID}.jsonl"));
        fs::write(
            &source,
            format!("{{\"type\":\"session_meta\",\"payload\":{{\"id\":\"{ID}\"}}}}\n"),
        )
        .unwrap();
        register(&registry, 100);
        registry
            .connect_at(
                Connect {
                    session_id: ID.into(),
                    generation: 1,
                    rollout_path: source.clone(),
                },
                &source_root,
                101,
            )
            .unwrap();
        use std::io::Write;
        writeln!(
            fs::OpenOptions::new().append(true).open(&source).unwrap(),
            "broken record"
        )
        .unwrap();
        registry.refresh_runtime(102);
        assert_eq!(
            registry.view(102)["collectionIssues"][0]["reason"],
            "source_record_invalid"
        );
        let backup = source_root.join("owned-fixture-paused");
        fs::rename(&source, &backup).unwrap();
        registry.refresh_runtime(102);
        let failure = registry.view(102);
        assert_eq!(failure["state"], "partial");
        assert_eq!(failure["collectionIssues"][0]["reason"], "read_failed");
        assert_eq!(failure["collectionIssues"][0]["lastSuccessAt"], 101);
        fs::rename(&backup, &source).unwrap();
        registry.refresh_runtime(103);
        assert_eq!(
            registry.view(103)["state"],
            "partial",
            "recovered I/O does not erase an earlier observation gap"
        );
        assert_eq!(
            registry.view(103)["collectionIssues"][0]["reason"],
            "source_record_invalid"
        );
    }
    #[test]
    fn healthy_empty_and_closed_native_collections_are_real_zero_with_bounded_scope() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        let empty = registry.view(100);
        assert_eq!(empty["state"], "complete");
        assert_eq!(empty["verifiedCount"], 0);
        assert_eq!(empty["scope"], "connected_sessions");
        register(&registry, 100);
        registry.activity(result(1, 101), 101).unwrap();
        assert_eq!(registry.view(101)["verifiedCount"], 1);
        registry
            .close(
                Close {
                    session_id: ID.into(),
                    generation: 1,
                    ended_at_ms: 102,
                    outcome: "success".into(),
                },
                102,
            )
            .unwrap();
        assert_eq!(registry.view(102)["state"], "complete");
        assert_eq!(registry.view(102)["verifiedCount"], 0);
    }
    fn register(registry: &Registry, now: u64) -> Value {
        registry
            .register(
                Register {
                    session_id: ID.into(),
                    todo_id: 1,
                    role: "work".into(),
                    business_project: None,
                },
                now,
            )
            .unwrap()
    }
    fn result(generation: u64, at: u64) -> Activity {
        Activity {
            session_id: ID.into(),
            generation,
            actual_at_ms: at,
            event_id: format!("tool-{at}"),
            tool: "exec_command".into(),
            kind: "test".into(),
            executed: true,
            exit_code: Some(0),
        }
    }
    #[test]
    fn registration_and_file_heartbeat_are_not_work() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        assert_eq!(registry.view(100)["sessions"], json!([]));
        register(&registry, 200);
        assert_eq!(registry.view(200)["sessions"], json!([]));
    }
    #[test]
    fn actual_receipt_expires_at_its_source_time_and_close_removes_live() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        registry.activity(result(1, 100), 100).unwrap();
        assert_eq!(registry.view(100)["sessions"].as_array().unwrap().len(), 1);
        assert_eq!(registry.view(30_101)["sessions"], json!([]));
        registry
            .close(
                Close {
                    session_id: ID.into(),
                    generation: 1,
                    ended_at_ms: 200,
                    outcome: "success".into(),
                },
                200,
            )
            .unwrap();
        assert_eq!(registry.view(200)["sessions"], json!([]));
        assert_eq!(registry.view(200)["bindings"][0]["outcome"], "success");
    }
    #[test]
    fn rebind_invalidates_old_receipts_and_old_close() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        registry.activity(result(1, 100), 100).unwrap();
        let next = registry
            .register(
                Register {
                    session_id: ID.into(),
                    todo_id: 2,
                    role: "work".into(),
                    business_project: None,
                },
                200,
            )
            .unwrap();
        assert_eq!(next["generation"], 2);
        assert_eq!(registry.view(200)["sessions"], json!([]));
        assert!(registry.activity(result(1, 201), 201).is_err());
        assert!(
            registry
                .close(
                    Close {
                        session_id: ID.into(),
                        generation: 1,
                        ended_at_ms: 201,
                        outcome: "success".into()
                    },
                    201
                )
                .is_err()
        );
    }
    #[test]
    fn old_snapshot_and_supervision_never_become_current_work() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        registry
            .register(
                Register {
                    session_id: ID.into(),
                    todo_id: 1,
                    role: "supervision".into(),
                    business_project: None,
                },
                50_000,
            )
            .unwrap();
        assert!(registry.activity(result(1, 100), 50_000).is_err());
        assert_eq!(registry.view(50_000)["sessions"], json!([]));
        assert!(registry.view(50_000)["bindings"][0]["lastActualAt"].is_null());
    }
    #[test]
    fn corrupt_state_is_preserved_and_get_does_not_create_state() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        registry.view(1);
        assert!(!registry.path.exists());
        fs::create_dir_all(registry.path.parent().unwrap()).unwrap();
        fs::write(&registry.path, b"broken").unwrap();
        let broken = Registry::new(temp.path());
        assert!(
            broken
                .register(
                    Register {
                        session_id: ID.into(),
                        todo_id: 1,
                        role: "work".into(),
                        business_project: None
                    },
                    1
                )
                .is_err()
        );
        assert_eq!(fs::read(&registry.path).unwrap(), b"broken");
    }
    #[test]
    fn status_only_or_rejected_operations_are_not_receipts() {
        assert!(
            serde_json::from_value::<Activity>(
                json!({"session_id":ID,"generation":1,"status":"working"})
            )
            .is_err()
        );
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        let mut refused = result(1, 100);
        refused.executed = false;
        assert!(registry.activity(refused, 100).is_err());
        assert_eq!(registry.view(100)["sessions"], json!([]));
    }

    #[test]
    fn first_binding_cannot_relabel_a_tool_result_from_before_its_scope() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 200);
        assert!(registry.activity(result(1, 199), 200).is_err());
        assert!(
            registry.view(200)["sessions"]
                .as_array()
                .unwrap()
                .is_empty()
        );
    }

    #[test]
    fn unavailable_native_source_is_unknown_instead_of_measured_zero() {
        let mut board = projected_board();
        crate::workforce::merge_native(
            &mut board,
            &json!({"source":"native","state":"unavailable","sessions":[]}),
            100,
        );
        assert!(board["workforce"]["processing"]["bySource"]["nativeToolReceipts"].is_null());
    }

    fn projected_board() -> Value {
        let mut board = json!({"fetchedAt":100,"metadataOk":true,"sourceOk":false,"protectionMode":"masked_metadata","metrics":{},
            "departments":[{"id":"department-5","name":"情報システム部","products":[{"id":"product-0000000000000001","name":"PCの道具","workers":[
                {"id":"todo-1","status":"waiting","recordedStatus":"doing","primaryJob":null,"detailAvailable":false},
                {"id":"todo-2","status":"waiting","recordedStatus":"doing","primaryJob":null,"detailAvailable":false}]}]}],"unassignedProducts":[]});
        board["workforce"] = crate::workforce::project(&board, None, 100);
        board
    }

    #[test]
    fn native_projection_is_one_session_and_never_borrows_related_tasks() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        registry.activity(result(1, 100), 100).unwrap();
        let mut native = registry.view(100);
        let duplicate = native["sessions"][0].clone();
        native["sessions"].as_array_mut().unwrap().push(duplicate);
        let mut board = projected_board();
        crate::workforce::merge_native(&mut board, &native, 100);
        crate::workforce::apply_to_board(&mut board);
        assert_eq!(board["workforce"]["processing"]["verifiedCount"], 1);
        assert!(board["workforce"]["processing"]["total"].is_null());
        let rows = &board["departments"][0]["products"][0]["workers"];
        assert_eq!(rows[0]["workState"], "processing");
        assert_ne!(rows[1]["workState"], "processing");
        assert!(rows[0]["primaryJob"].is_null());
        assert_eq!(rows[0]["executionSource"]["source"], "native");
        assert_eq!(rows[0]["detailAvailable"], false);
        assert!(!board.to_string().contains(ID));
        registry
            .close(
                Close {
                    session_id: ID.into(),
                    generation: 1,
                    ended_at_ms: 200,
                    outcome: "success".into(),
                },
                200,
            )
            .unwrap();
        let mut closed = projected_board();
        crate::workforce::merge_native(&mut closed, &registry.view(200), 200);
        crate::workforce::apply_to_board(&mut closed);
        assert_ne!(
            closed["departments"][0]["products"][0]["workers"][0]["workState"],
            "processing"
        );
    }

    #[test]
    fn cc_evidence_and_native_receipt_do_not_double_count_one_current_task() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        registry.activity(result(1, 100), 100).unwrap();
        let mut board = projected_board();
        let key = format!("job-{}", "a".repeat(64));
        board["workforce"]["tasks"][0]["state"] = json!("processing");
        board["workforce"]["tasks"][0]["references"]["sessions"] = json!([key]);
        board["workforce"]["processing"] = json!({"state":"complete","verifiedCount":1,"sessions":[{"key":key,"validUntil":200}],"validUntil":200});
        crate::workforce::merge_native(&mut board, &registry.view(100), 100);
        assert_eq!(board["workforce"]["processing"]["verifiedCount"], 1);
        assert_eq!(
            board["workforce"]["processing"]["bySource"]["nativeToolReceipts"],
            0
        );
    }

    #[test]
    fn unavailable_records_are_not_rewritten_as_measured_zero() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        let mut board = json!({"workforce":{"state":"unavailable","states":{"processing":null},"processing":{"state":"unavailable","verifiedCount":null},"tasks":[]}});
        crate::workforce::merge_native(&mut board, &registry.view(100), 100);
        assert!(board["workforce"]["states"]["processing"].is_null());
        assert!(board["workforce"]["processing"]["verifiedCount"].is_null());
    }

    #[test]
    fn two_native_sessions_keep_the_original_state_and_distinct_execution_proofs() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        registry.activity(result(1, 100), 100).unwrap();
        let second = "00000000-0000-7000-8000-000000000002";
        let second_registration = registry
            .register(
                Register {
                    session_id: second.into(),
                    todo_id: 1,
                    role: "review".into(),
                    business_project: None,
                },
                100,
            )
            .unwrap();
        let mut receipt = result(second_registration["generation"].as_u64().unwrap(), 101);
        receipt.session_id = second.into();
        registry.activity(receipt, 101).unwrap();
        let mut board = projected_board();
        crate::workforce::merge_native(&mut board, &registry.view(101), 101);
        crate::workforce::apply_to_board(&mut board);
        let task = &board["workforce"]["tasks"][0];
        assert_eq!(task["recordedWorkState"], "execution_unverified");
        assert_eq!(task["nativeExecutions"].as_array().unwrap().len(), 2);
        assert_eq!(board["workforce"]["processing"]["verifiedCount"], 2);
        assert!(
            board["nativeActivity"].get("bindings").is_none(),
            "the ordinary board must not expose all native history"
        );
    }

    #[test]
    fn native_does_not_resume_recorded_waits_or_make_a_stale_cc_observation_fresh() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        registry.activity(result(1, 100), 100).unwrap();
        for status in ["wait", "block", "recovery_unverified", "done"] {
            let mut board = projected_board();
            board["departments"][0]["products"][0]["workers"][0]["recordedStatus"] = json!(status);
            crate::workforce::merge_native(&mut board, &registry.view(100), 100);
            assert_eq!(
                board["workforce"]["processing"]["bySource"]["nativeToolReceipts"],
                0
            );
        }
        let mut board = projected_board();
        board["workforce"]["processing"]["state"] = json!("stale");
        crate::workforce::merge_native(&mut board, &registry.view(40_000), 40_000);
        assert_eq!(board["workforce"]["processing"]["state"], "stale");
    }

    #[test]
    fn closed_sources_are_preserved_in_immutable_segments_before_reclaiming_live_capacity() {
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        registry
            .update(|ledger| {
                for n in 0..2048 {
                    ledger.entries.insert(
                        format!("00000000-0000-7000-8000-{n:012x}"),
                        Entry {
                            generation: 1,
                            todo_id: 1,
                            role: "work".into(),
                            business_project: None,
                            bound_at_ms: 1,
                            min_activity_at_ms: 0,
                            receipt: None,
                            closed_at_ms: Some(20),
                            outcome: Some("success".into()),
                            runtime: None,
                        },
                    );
                }
                Ok((Value::Null, true))
            })
            .unwrap();
        let next = "00000000-0000-7000-8000-000000010000";
        registry
            .register(
                Register {
                    session_id: next.into(),
                    todo_id: 2,
                    role: "work".into(),
                    business_project: None,
                },
                30,
            )
            .unwrap();
        assert_eq!(registry.view(30)["knownSources"], 1921);
        let path = registry.path.with_file_name("native-activity-closed");
        let files = fs::read_dir(path)
            .unwrap()
            .collect::<Result<Vec<_>, _>>()
            .unwrap();
        assert_eq!(files.len(), 1);
        let archived: Ledger = serde_json::from_slice(&fs::read(files[0].path()).unwrap()).unwrap();
        assert_eq!(archived.entries.len(), 128);
        assert!(
            archived
                .entries
                .values()
                .all(|entry| entry.outcome.as_deref() == Some("success"))
        );
        let returned = registry
            .register(
                Register {
                    session_id: ID.into(),
                    todo_id: 7,
                    role: "work".into(),
                    business_project: None,
                },
                31,
            )
            .unwrap();
        assert!(
            returned["generation"].as_u64().unwrap() > 1,
            "an archived UUID cannot restart its epoch"
        );
        assert!(
            registry.activity(result(1, 31), 31).is_err(),
            "old generation cannot certify another task after archive/reuse"
        );
        let reloaded = Registry::new(temp.path());
        assert!(
            reloaded.activity(result(1, 32), 32).is_err(),
            "the epoch survives a process restart"
        );
    }
}
