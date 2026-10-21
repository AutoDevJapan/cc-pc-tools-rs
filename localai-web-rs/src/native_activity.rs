//! Runtime-supplied native tool receipts, separate from managed CC jobs.
//! This receiver never opens session transcripts or accepts message/status prose.
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    collections::BTreeMap,
    fs,
    io::{Read, Write},
    path::PathBuf,
    sync::{
        atomic::{AtomicU64, Ordering},
        Mutex,
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
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Ledger {
    version: u32,
    entries: BTreeMap<String, Entry>,
}

impl Default for Ledger {
    fn default() -> Self {
        Self {
            version: 1,
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

pub struct Registry {
    path: PathBuf,
    ledger: Mutex<Result<Ledger, &'static str>>,
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
        }
    }

    fn update(
        &self,
        change: impl FnOnce(&mut Ledger) -> Result<(Value, bool), &'static str>,
    ) -> Result<Value, &'static str> {
        let mut guard = self.ledger.lock().unwrap_or_else(|e| e.into_inner());
        let mut next = guard.as_ref().map_err(|error| *error)?.clone();
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
        *guard = Ok(next);
        Ok(result)
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
            if ledger.entries.len() >= 2048 && !ledger.entries.contains_key(&id) {
                return Err("native_store_full");
            }
            let previous = ledger.entries.get(&id);
            let generation = match previous {
                Some(old) => old
                    .generation
                    .checked_add(1)
                    .ok_or("native_generation_exhausted")?,
                None => 1,
            };
            let entry = Entry {
                generation,
                todo_id: request.todo_id,
                role: request.role,
                business_project: request.business_project,
                bound_at_ms: now,
                min_activity_at_ms: if previous.is_some() { now } else { 0 },
                receipt: None,
                closed_at_ms: None,
                outcome: None,
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
            if entry.closed_at_ms.is_some() || !matches!(entry.role.as_str(), "work" | "review") {
                continue;
            }
            if let Some(receipt) = &entry.receipt {
                if receipt.actual_at_ms <= now && now - receipt.actual_at_ms <= FRESH_MS {
                    sessions.push(json!({"key":key,"source":"native","todoId":entry.todo_id,"actualAt":receipt.actual_at_ms,
                        "validUntil":receipt.actual_at_ms.saturating_add(FRESH_MS),"tool":receipt.tool,"kind":receipt.kind,
                        "verification":"runtime_tool_receipt","generation":entry.generation}));
                }
            }
        }
        json!({"source":"native","state":if ledger.entries.is_empty(){"unobserved"}else{"partial"},"complete":false,
            "verifiedCount":sessions.len(),"sessions":sessions,"bindings":bindings,"knownSources":ledger.entries.len(),"scope":"instrumented runtime receipts only; no transcript scan or global inventory"})
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    const ID: &str = "00000000-0000-7000-8000-000000000001";
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
        assert!(registry
            .close(
                Close {
                    session_id: ID.into(),
                    generation: 1,
                    ended_at_ms: 201,
                    outcome: "success".into()
                },
                201
            )
            .is_err());
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
        registry.activity(result(1, 100), 50_000).unwrap();
        assert_eq!(registry.view(50_000)["sessions"], json!([]));
        assert_eq!(registry.view(50_000)["bindings"][0]["lastActualAt"], 100);
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
        assert!(broken
            .register(
                Register {
                    session_id: ID.into(),
                    todo_id: 1,
                    role: "work".into(),
                    business_project: None
                },
                1
            )
            .is_err());
        assert_eq!(fs::read(&registry.path).unwrap(), b"broken");
    }
    #[test]
    fn status_only_or_rejected_operations_are_not_receipts() {
        assert!(serde_json::from_value::<Activity>(
            json!({"session_id":ID,"generation":1,"status":"working"})
        )
        .is_err());
        let temp = tempfile::tempdir().unwrap();
        let registry = Registry::new(temp.path());
        register(&registry, 100);
        let mut refused = result(1, 100);
        refused.executed = false;
        assert!(registry.activity(refused, 100).is_err());
        assert_eq!(registry.view(100)["sessions"], json!([]));
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
}
