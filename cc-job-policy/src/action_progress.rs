//! Bounded, content-free evidence of tool activity. Assistant prose is never progress.
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    collections::BTreeSet,
    fs::File,
    io::{self, BufRead, Read, Seek, SeekFrom},
    path::Path,
};

pub const QUIET_S: f64 = 1800.;
pub const MAX_BYTES: u64 = 1024 * 1024;
pub const MAX_LINE: u64 = 256 * 1024;
pub const MAX_EVENTS: usize = 512;

#[derive(Default, Clone, Serialize, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct Progress {
    pub version: u32,
    pub attempt: Value,
    pub file: String,
    pub offset: u64,
    pub active: BTreeSet<String>,
    pub invalid: bool,
    pub quiet_since: f64,
    pub messages: u32,
    pub caught_up: bool,
}
#[derive(Default, Deserialize)]
#[serde(default)]
struct Event {
    r#type: String,
    item: Item,
}
#[derive(Default, Deserialize)]
#[serde(default)]
struct Item {
    r#type: String,
    id: String,
    status: String,
}

/// Only fixed metadata; no prompt, event payload, command, tool arguments or output.
pub fn attempt(job: &Value) -> Value {
    let mut v = json!({});
    for k in [
        "id",
        "pid",
        "pid_created",
        "codex_pid",
        "codex_pid_created",
        "started_at",
        "attempt_events_offset",
    ] {
        v[k] = job[k].clone();
    }
    v
}

pub fn eligible(db: &Value, row: &Value, job: &Value) -> bool {
    row["status"] == "doing"
        && row["project"] != "owner"
        && row["dispatchable"] == true
        && row["spec"].as_str().is_some_and(|path| !path.is_empty())
        && job["status"] == "running"
        // ci_fix is the saved implementation label on the observed ToDo2783 worker.
        && matches!(job["task_type"].as_str(), Some("implementation" | "ci_fix"))
        // The ordinary implementation launcher preserves this scope exactly.
        // Restricted or unknown legacy scopes must not be widened on recovery.
        && job["sandbox"] == "danger-full-access"
        && row["sandbox"] != "read-only"
        && row["scheduler"]["console_request"]["read_only"] != true
        && !["review", "research"].contains(&row["subwork"]["kind"].as_str().unwrap_or(""))
        && row["scheduler"]["job"] == job["id"]
        && (row["worker_receipt"].is_null()
            || (row["worker_receipt"]["job"] == job["id"]
                && row["worker_receipt"]["generation"] == row["resume_generation"]))
        && (job["todo_id"] == row["id"] || row["worker_receipt"]["job"] == job["id"])
        && !crate::cancellation::owner_stopped(row, job)
        && !crate::product_retry::stopped(row)
        && row["owner_question_wait"].is_null()
        && row["decision_wait"].is_null()
        && row["scheduler"]["console_request"]["options"]["sandbox"] != "read-only"
        && job["fixed_read_only_review"].is_null()
        && job["fixed_review_requalification"].is_null()
        && job["context_phase"] != "summarize"
        && !crate::todo_wait::waiting(db, row)
        && row["wait_ready"].is_null()
        && row["wait_on"].as_array().is_none_or(Vec::is_empty)
        && !row["stall_recovery"]["pending"].is_object()
        && row["scheduler"]["owner_stop_job"].is_null()
        && row["scheduler"]["question"].is_null()
        && row["scheduler"]["pending_cancel"].is_null()
        && job["capacity_wait_pending"] != true
        && job["waiting_for_work_owner"] != true
        && job["work_wait_ready"].is_null()
        && job["work_wait_on"].as_array().is_none_or(Vec::is_empty)
        && job["resource_wait"].is_null()
        && row["scheduler"]["resource_wait"].is_null()
        && job["pid_created"].as_u64().is_some()
        && job["codex_pid_created"].as_u64().is_some()
        && job["started_at"].is_string()
}

/// Metadata CAS is rechecked under the normal queue -> job -> ToDo locks.
pub fn ticket(db: &Value, row: &Value, job: &Value) -> Value {
    use sha2::{Digest, Sha256};
    let metadata = json!({"id":row["id"],"generation":row["resume_generation"],
        "receipt":row["worker_receipt"],"job":row["scheduler"]["job"],"key":row["scheduler"]["key"],
        "branch":row["scheduler"]["branch"],"cwd":row["scheduler"]["worktree"],
        "repo":db["projects"][row["project"].as_str().unwrap_or("")],"attempt":attempt(job)});
    // Persist only a digest, so paths and branch names do not enter activity receipts.
    json!(format!(
        "{:x}",
        Sha256::digest(serde_json::to_vec(&metadata).unwrap())
    ))
}

fn identity(file: &File) -> io::Result<String> {
    #[cfg(windows)]
    {
        use std::os::windows::io::AsRawHandle;
        use windows_sys::Win32::Storage::FileSystem::{
            BY_HANDLE_FILE_INFORMATION, GetFileInformationByHandle,
        };
        let mut v: BY_HANDLE_FILE_INFORMATION = unsafe { std::mem::zeroed() };
        if unsafe { GetFileInformationByHandle(file.as_raw_handle(), &mut v) } == 0 {
            return Err(io::Error::last_os_error());
        }
        Ok(format!(
            "{}:{}:{}",
            v.dwVolumeSerialNumber, v.nFileIndexHigh, v.nFileIndexLow
        ))
    }
    #[cfg(not(windows))]
    {
        use std::os::unix::fs::MetadataExt;
        let m = file.metadata()?;
        Ok(format!("{}:{}", m.dev(), m.ino()))
    }
}
impl Progress {
    pub fn stalled(&self, now: f64) -> bool {
        self.version == 1
            && self.caught_up
            && !self.invalid
            && self.active.is_empty()
            && self.messages >= 3
            && self.quiet_since > 0.
            && now >= self.quiet_since + QUIET_S
    }
    fn event(&mut self, e: Event, now: f64) {
        let kind = e.item.r#type.as_str();
        let t = e.r#type.as_str();
        if matches!(t, "item.started" | "item.updated" | "item.completed") {
            if kind == "agent_message" {
                if t == "item.completed" {
                    self.messages = self.messages.saturating_add(1);
                }
                return;
            }
            if matches!(kind, "reasoning" | "todo_list") {
                return;
            }
            // Unknown item kinds and delegated work cannot establish an idle boundary.
            if !matches!(
                kind,
                "command_execution" | "file_change" | "mcp_tool_call" | "web_search"
            ) {
                self.invalid = true;
                return;
            }
            if e.item.id.is_empty()
                || e.item.id.len() > 128
                || !e
                    .item
                    .id
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b"_-.:".contains(&b))
            {
                self.invalid = true;
                return;
            }
            self.quiet_since = now;
            self.messages = 0;
            if t == "item.completed" && matches!(e.item.status.as_str(), "completed" | "failed") {
                self.active.remove(&e.item.id);
            } else if self.active.len() < 128 {
                self.active.insert(e.item.id);
            } else {
                self.invalid = true;
            }
        } else if !matches!(t, "thread.started" | "turn.started") {
            self.invalid = true;
        }
    }
}

/// At most 1 MiB / 512 events. Partial, unreadable and corrupt inputs never prove inactivity.
pub fn scan(path: &Path, job: &Value, previous: &Progress, now: f64) -> io::Result<Progress> {
    let file = File::open(path)?;
    let id = identity(&file)?;
    let len = file.metadata()?.len();
    let attempt = attempt(job);
    let mut p = if previous.version != 1 || previous.attempt != attempt || previous.file != id {
        Progress {
            version: 1,
            attempt,
            file: id,
            offset: job["attempt_events_offset"].as_u64().unwrap_or(0),
            quiet_since: now,
            ..Progress::default()
        }
    } else {
        previous.clone()
    };
    p.caught_up = false;
    if len < p.offset {
        p.invalid = true;
        return Ok(p);
    }
    let mut reader = std::io::BufReader::new(file);
    reader.seek(SeekFrom::Start(p.offset))?;
    let start = p.offset;
    for _ in 0..MAX_EVENTS {
        let mut raw = Vec::new();
        let remaining = MAX_BYTES - (p.offset - start);
        let count = (&mut reader)
            .take((MAX_LINE + 1).min(remaining))
            .read_until(b'\n', &mut raw)?;
        if count == 0 {
            break;
        }
        if raw.len() as u64 > MAX_LINE {
            p.invalid = true;
            break;
        }
        if !raw.ends_with(b"\n") {
            p.quiet_since = now;
            return Ok(p);
        }
        p.offset += raw.len() as u64;
        match serde_json::from_slice::<Event>(&raw) {
            Ok(e) => p.event(e, now),
            Err(_) => p.invalid = true,
        }
        if p.offset - start >= MAX_BYTES {
            break;
        }
    }
    p.caught_up = p.offset == reader.get_ref().metadata()?.len();
    if !p.caught_up {
        p.quiet_since = now;
    }
    Ok(p)
}
