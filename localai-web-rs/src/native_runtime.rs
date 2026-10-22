//! Incremental metadata for explicitly connected sessions. No directory scan,
//! message bodies, command arguments or tool output are retained or returned.
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{
    collections::BTreeMap,
    fs,
    io::{BufRead, BufReader, Read, Seek, SeekFrom},
    path::{Path, PathBuf},
};

const LIMIT: u64 = 1024 * 1024;

#[derive(Clone, Serialize, Deserialize)]
pub(crate) struct Cursor {
    path: PathBuf,
    root: PathBuf,
    offset: u64,
    #[serde(default)]
    pending: BTreeMap<String, String>,
    pub last_success_at: u64,
    pub error: Option<String>,
}

pub(crate) struct Observation {
    pub receipt: Option<(u64, String)>,
    pub ended: Option<(u64, &'static str)>,
    pub changed: bool,
}

#[derive(Deserialize)]
struct Event {
    timestamp: Option<String>,
    #[serde(rename = "type")]
    kind: String,
    payload: Payload,
}

#[derive(Deserialize)]
struct Payload {
    #[serde(rename = "type", default)]
    kind: String,
    #[serde(default)]
    id: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    call_id: String,
    #[serde(default)]
    output: Value,
    #[serde(default, rename = "input", deserialize_with = "execution_input")]
    executes_tool: bool,
}

fn execution_input<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<bool, D::Error> {
    let value = String::deserialize(deserializer)?;
    Ok(value.contains("tools.exec_command(") || value.contains("tools.apply_patch("))
}

fn ordinary(path: &Path, root: &Path) -> Result<PathBuf, &'static str> {
    if !path.is_absolute() {
        return Err("source_outside_sessions");
    }
    let canonical = fs::canonicalize(path).map_err(|_| "read_failed")?;
    let root = fs::canonicalize(root).map_err(|_| "read_failed")?;
    if !canonical.starts_with(&root) || canonical == root {
        return Err("source_outside_sessions");
    }
    for part in path.ancestors() {
        if fs::symlink_metadata(part)
            .map_err(|_| "read_failed")?
            .file_type()
            .is_symlink()
        {
            return Err("source_link");
        }
    }
    if !fs::metadata(&canonical)
        .map_err(|_| "read_failed")?
        .is_file()
    {
        return Err("read_failed");
    }
    Ok(canonical)
}

impl Cursor {
    pub fn attach(path: &Path, root: &Path, session: &str, now: u64) -> Result<Self, &'static str> {
        let canonical = ordinary(path, root)?;
        if !canonical
            .file_name()
            .and_then(|s| s.to_str())
            .is_some_and(|s| s.ends_with(&format!("-{session}.jsonl")))
        {
            return Err("source_identity_mismatch");
        }
        let file = fs::File::open(&canonical).map_err(|_| "read_failed")?;
        let length = file.metadata().map_err(|_| "read_failed")?.len();
        let mut first = Vec::new();
        BufReader::new(file.take(65537))
            .read_until(b'\n', &mut first)
            .map_err(|_| "read_failed")?;
        if first.len() > 65536 {
            return Err("source_identity_mismatch");
        }
        let event: Event =
            serde_json::from_slice(&first).map_err(|_| "source_identity_mismatch")?;
        if event.kind != "session_meta" || event.payload.id != session {
            return Err("source_identity_mismatch");
        }
        Ok(Self {
            path: canonical,
            root: fs::canonicalize(root).map_err(|_| "read_failed")?,
            offset: length,
            pending: BTreeMap::new(),
            last_success_at: now,
            error: None,
        })
    }

    pub fn poll(&mut self, floor: u64, now: u64) -> Result<Observation, &'static str> {
        if ordinary(&self.path, &self.root)? != self.path {
            return Err("source_identity_changed");
        }
        let mut file = fs::File::open(&self.path).map_err(|_| "read_failed")?;
        let length = file.metadata().map_err(|_| "read_failed")?.len();
        if length < self.offset {
            return Err("source_truncated");
        }
        file.seek(SeekFrom::Start(self.offset))
            .map_err(|_| "read_failed")?;
        let mut bytes = Vec::new();
        file.take(LIMIT + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| "read_failed")?;
        let backlog = bytes.len() as u64 > LIMIT;
        bytes.truncate(LIMIT as usize);
        let end = bytes
            .iter()
            .rposition(|b| *b == b'\n')
            .map_or(0, |at| at + 1);
        if end == 0 && backlog {
            return Err("source_line_too_large");
        }
        let old_offset = self.offset;
        let old_error = self.error.clone();
        let mut result = Observation {
            receipt: None,
            ended: None,
            changed: false,
        };
        for line in bytes[..end]
            .split(|b| *b == b'\n')
            .filter(|line| !line.is_empty())
        {
            let event: Event = serde_json::from_slice(line).map_err(|_| "source_record_invalid")?;
            let Some(at) = event
                .timestamp
                .as_deref()
                .and_then(|at| chrono::DateTime::parse_from_rfc3339(at).ok())
                .and_then(|at| u64::try_from(at.timestamp_millis()).ok())
            else {
                continue;
            };
            if at < floor || at > now {
                continue;
            }
            let payload = event.payload;
            if event.kind == "event_msg"
                && matches!(
                    payload.kind.as_str(),
                    "task_complete" | "task_aborted" | "turn_aborted"
                )
            {
                result.ended = Some((
                    at,
                    if payload.kind == "task_complete" {
                        "success"
                    } else {
                        "cancelled"
                    },
                ));
                self.pending.clear();
                break;
            }
            if event.kind != "response_item" {
                continue;
            }
            match payload.kind.as_str() {
                "function_call" | "custom_tool_call"
                    if (matches!(
                        payload.name.as_str(),
                        "exec_command" | "apply_patch" | "js" | "mcp__cua_repl.js"
                    ) || (payload.name == "exec" && payload.executes_tool))
                        && payload.call_id.len() <= 128
                        && !payload.call_id.is_empty() =>
                {
                    if self.pending.len() >= 128 {
                        return Err("source_pending_limit");
                    }
                    self.pending.insert(payload.call_id, payload.name);
                }
                "function_call_output" | "custom_tool_call_output" => {
                    if self.pending.remove(&payload.call_id).is_none() {
                        continue;
                    }
                    let output = output_text(&payload.output);
                    if output.trim().is_empty()
                        || output.contains("blocked by policy")
                        || output.contains("Rejected(")
                        || output.contains("tool call rejected")
                    {
                        continue;
                    }
                    result.receipt = Some((at, payload.call_id));
                }
                _ => {}
            }
        }
        self.offset += end as u64;
        self.error = backlog.then(|| "source_backlog".into());
        self.last_success_at = now;
        result.changed = self.offset != old_offset || self.error != old_error;
        Ok(result)
    }
}

fn output_text(value: &Value) -> String {
    if let Some(text) = value.as_str() {
        return text.to_owned();
    }
    value
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|part| part["text"].as_str())
        .collect::<Vec<_>>()
        .join("\n")
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    const ID: &str = "00000000-0000-7000-8000-000000000001";
    fn setup() -> (tempfile::TempDir, PathBuf) {
        let tmp = tempfile::tempdir().unwrap();
        let path = tmp.path().join(format!("rollout-{ID}.jsonl"));
        fs::write(
            &path,
            format!("{{\"type\":\"session_meta\",\"payload\":{{\"id\":\"{ID}\"}}}}\n"),
        )
        .unwrap();
        (tmp, path)
    }
    fn append(path: &Path, value: Value) {
        let mut file = fs::OpenOptions::new().append(true).open(path).unwrap();
        writeln!(file, "{value}").unwrap();
    }
    #[test]
    fn connected_actual_call_result_is_observed_automatically_and_end_is_terminal() {
        let (tmp, path) = setup();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        append(
            &path,
            serde_json::json!({"timestamp":"1970-01-01T00:00:00.101Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","call_id":"call_one","input":"await tools.exec_command({cmd:'secret command must never be copied'})"}}),
        );
        append(
            &path,
            serde_json::json!({"timestamp":"1970-01-01T00:00:00.102Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_one","output":[{"type":"input_text","text":"Script completed"}]}}),
        );
        let observed = cursor.poll(100, 103).unwrap();
        assert_eq!(observed.receipt, Some((102, "call_one".into())));
        let stored = serde_json::to_string(&cursor).unwrap();
        assert!(!stored.contains("secret"));
        assert!(!stored.contains("Script completed"));
        assert!(cursor.poll(100, 104).unwrap().receipt.is_none());
        append(
            &path,
            serde_json::json!({"timestamp":"1970-01-01T00:00:00.105Z","type":"event_msg","payload":{"type":"task_complete"}}),
        );
        assert_eq!(cursor.poll(100, 106).unwrap().ended, Some((105, "success")));
    }
    #[test]
    fn old_results_status_prose_unmatched_and_refused_calls_do_not_count() {
        let (tmp, path) = setup();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        for (at, kind, call, text) in [
            (101, "custom_tool_call_output", "missing", "ok"),
            (102, "message", "", "working"),
            (99, "custom_tool_call", "old", ""),
            (103, "custom_tool_call_output", "old", "ok"),
            (104, "custom_tool_call", "denied", ""),
            (
                105,
                "custom_tool_call_output",
                "denied",
                "rejected: blocked by policy",
            ),
        ] {
            append(
                &path,
                serde_json::json!({"timestamp":format!("1970-01-01T00:00:00.{at:03}Z"),"type":"response_item","payload":{"type":kind,"name":"exec_command","call_id":call,"output":text}}),
            );
        }
        assert!(cursor.poll(100, 106).unwrap().receipt.is_none());
        fs::write(&path, b"{}").unwrap();
        assert!(
            cursor.poll(100, 107).is_err(),
            "truncated source is failure, not zero"
        );
    }
    #[test]
    fn supervisor_waits_and_metadata_only_execs_are_not_runtime_work() {
        let (tmp, path) = setup();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        for (name, input) in [
            ("wait", ""),
            ("write_stdin", ""),
            ("exec", "text(await tools.clock__curr_time({}));"),
        ] {
            append(
                &path,
                serde_json::json!({"timestamp":"1970-01-01T00:00:00.101Z","type":"response_item","payload":{"type":"custom_tool_call","name":name,"call_id":"waiting","input":input}}),
            );
            append(
                &path,
                serde_json::json!({"timestamp":"1970-01-01T00:00:00.102Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"waiting","output":"still running"}}),
            );
        }
        assert!(cursor.poll(100, 103).unwrap().receipt.is_none());
    }
}
