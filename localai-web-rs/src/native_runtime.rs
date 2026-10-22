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
    #[serde(default)]
    discarding: bool,
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
    #[serde(rename = "type", default, deserialize_with = "metadata_string")]
    kind: String,
    #[serde(default, deserialize_with = "metadata_string")]
    id: String,
    #[serde(default, deserialize_with = "metadata_string")]
    name: String,
    #[serde(default, deserialize_with = "metadata_string")]
    call_id: String,
    #[serde(default)]
    output: Value,
    #[serde(default, rename = "input", deserialize_with = "execution_input")]
    executes_tool: bool,
}

fn execution_input<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<bool, D::Error> {
    let value = Value::deserialize(deserializer)?;
    Ok(value.as_str().is_some_and(|value| {
        value.contains("tools.exec_command(") || value.contains("tools.apply_patch(")
    }))
}

fn metadata_string<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<String, D::Error> {
    Ok(Value::deserialize(deserializer)?
        .as_str()
        .unwrap_or_default()
        .to_owned())
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
        let mut file = fs::File::open(&canonical).map_err(|_| "read_failed")?;
        let length = file.metadata().map_err(|_| "read_failed")?.len();
        let mut first = Vec::new();
        BufReader::new((&mut file).take(LIMIT + 1))
            .read_until(b'\n', &mut first)
            .map_err(|_| "read_failed")?;
        if first.len() as u64 > LIMIT {
            return Err("source_identity_mismatch");
        }
        let event: Event =
            serde_json::from_slice(&first).map_err(|_| "source_identity_mismatch")?;
        if event.kind != "session_meta" || event.payload.id != session {
            return Err("source_identity_mismatch");
        }
        // A producer may be appending now. Start at a complete record boundary;
        // the generation's timestamp floor still rejects earlier activity.
        let start = length.saturating_sub(LIMIT);
        file.seek(SeekFrom::Start(start))
            .map_err(|_| "read_failed")?;
        let mut tail = Vec::new();
        file.take(length - start)
            .read_to_end(&mut tail)
            .map_err(|_| "read_failed")?;
        let boundary = tail.iter().rposition(|b| *b == b'\n');
        let offset = boundary.map_or(length, |at| start + at as u64 + 1);
        Ok(Self {
            path: canonical,
            root: fs::canonicalize(root).map_err(|_| "read_failed")?,
            offset,
            pending: BTreeMap::new(),
            discarding: boundary.is_none(),
            last_success_at: now,
            error: boundary.is_none().then(|| "source_line_too_large".into()),
        })
    }

    pub fn same_source(&self, other: &Self) -> bool {
        self.path == other.path && self.root == other.root
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
        file.take(LIMIT)
            .read_to_end(&mut bytes)
            .map_err(|_| "read_failed")?;
        let old_offset = self.offset;
        let old_error = self.error.clone();
        let old_success = self.last_success_at;
        let mut gap = self.error.clone().filter(|code| {
            matches!(
                code.as_str(),
                "source_line_too_large" | "source_record_invalid" | "source_pending_limit"
            )
        });
        let mut result = Observation {
            receipt: None,
            ended: None,
            changed: false,
        };
        let mut consumed = 0;
        if self.discarding {
            if let Some(at) = bytes.iter().position(|b| *b == b'\n') {
                consumed = at + 1;
                self.discarding = false;
            } else {
                self.offset += bytes.len() as u64;
                self.error = Some("source_line_too_large".into());
                result.changed = self.offset != old_offset || self.error != old_error;
                return Ok(result);
            }
        }
        let end = bytes[consumed..]
            .iter()
            .rposition(|b| *b == b'\n')
            .map_or(consumed, |at| consumed + at + 1);
        for line in bytes[consumed..end].split_inclusive(|b| *b == b'\n') {
            if line.iter().all(u8::is_ascii_whitespace) {
                consumed += line.len();
                continue;
            }
            let event: Event = match serde_json::from_slice(line) {
                Ok(event) => event,
                Err(_) => {
                    consumed += line.len();
                    gap = Some("source_record_invalid".into());
                    self.pending.clear();
                    continue;
                }
            };
            let Some(at) = event
                .timestamp
                .as_deref()
                .and_then(|at| chrono::DateTime::parse_from_rfc3339(at).ok())
                .and_then(|at| u64::try_from(at.timestamp_millis()).ok())
            else {
                consumed += line.len();
                continue;
            };
            if at > now {
                // Read-time events keep their original time and are retried on
                // the next bounded pass instead of being clamped or dropped.
                if at - now > 2_000 {
                    gap = Some("source_clock_ahead".into());
                }
                break;
            }
            consumed += line.len();
            if at < floor {
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
                gap = None;
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
                    if self.pending.len() >= 128 && !self.pending.contains_key(&payload.call_id) {
                        self.pending.clear();
                        gap = Some("source_pending_limit".into());
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
                    gap = None;
                }
                _ => {}
            }
        }
        if consumed == 0 && end == 0 && bytes.len() as u64 == LIMIT {
            consumed = bytes.len();
            self.discarding = true;
            self.pending.clear();
            gap = Some("source_line_too_large".into());
        }
        self.offset += consumed as u64;
        self.error = gap.or_else(|| {
            (length.saturating_sub(old_offset) > LIMIT).then(|| "source_backlog".into())
        });
        if self.error.is_none()
            && (consumed > 0 || old_error.is_some() || now.saturating_sub(old_success) >= 60_000)
        {
            self.last_success_at = now;
        }
        result.changed = self.offset != old_offset
            || self.error != old_error
            || self.last_success_at != old_success;
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

    fn real_pair(path: &Path) {
        append(
            path,
            serde_json::json!({"timestamp":"1970-01-01T00:00:00.103Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"fresh"}}),
        );
        append(
            path,
            serde_json::json!({"timestamp":"1970-01-01T00:00:00.104Z","type":"response_item","payload":{"type":"function_call_output","call_id":"fresh","output":"completed"}}),
        );
    }

    #[test]
    fn connection_during_partial_append_resumes_at_the_previous_record_boundary() {
        let (tmp, path) = setup();
        let prefix =
            b"{\"timestamp\":\"1970-01-01T00:00:00.099Z\",\"type\":\"event_msg\",\"payload\":{";
        fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all(prefix)
            .unwrap();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all(b"\"type\":\"message\"}}\n")
            .unwrap();
        real_pair(&path);
        assert_eq!(
            cursor.poll(100, 105).unwrap().receipt,
            Some((104, "fresh".into()))
        );
    }

    #[test]
    fn real_sized_oversized_records_make_bounded_progress_and_recover_at_a_fresh_result() {
        for size in [1_898_514, 2_765_098] {
            let (tmp, path) = setup();
            let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
            let mut line = serde_json::to_vec(&serde_json::json!({"timestamp":"1970-01-01T00:00:00.101Z","type":"event_msg","payload":{"type":"message","text":"x".repeat(size)}})).unwrap();
            line.push(b'\n');
            fs::OpenOptions::new()
                .append(true)
                .open(&path)
                .unwrap()
                .write_all(&line)
                .unwrap();
            real_pair(&path);
            let before = cursor.offset;
            let first = cursor.poll(100, 105).unwrap();
            assert!(first.receipt.is_none());
            assert_eq!(cursor.error.as_deref(), Some("source_line_too_large"));
            assert!(cursor.offset > before && cursor.offset - before <= LIMIT);
            let mut found = None;
            for _ in 0..4 {
                let before = cursor.offset;
                let result = cursor.poll(100, 106).unwrap();
                assert!(cursor.offset - before <= LIMIT);
                found = found.or(result.receipt);
            }
            assert_eq!(found, Some((104, "fresh".into())));
            assert!(cursor.error.is_none());
        }
    }

    #[test]
    fn malformed_record_advances_without_claiming_successful_zero_then_recovers() {
        let (tmp, path) = setup();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all(b"broken record\n")
            .unwrap();
        assert!(cursor.poll(100, 101).unwrap().receipt.is_none());
        assert_eq!(cursor.error.as_deref(), Some("source_record_invalid"));
        let offset = cursor.offset;
        assert!(cursor.poll(100, 102).unwrap().receipt.is_none());
        assert_eq!(cursor.offset, offset);
        assert!(
            cursor.error.is_some(),
            "an observation gap is not a healthy zero"
        );
        real_pair(&path);
        assert_eq!(
            cursor.poll(100, 105).unwrap().receipt,
            Some((104, "fresh".into()))
        );
        assert!(cursor.error.is_none());
    }

    #[test]
    fn unrelated_nullable_fields_do_not_freeze_real_results() {
        let (tmp, path) = setup();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        append(
            &path,
            serde_json::json!({"timestamp":"1970-01-01T00:00:00.101Z","type":"event_msg","payload":{"type":null,"id":null,"name":null,"call_id":null,"input":["unrelated"]}}),
        );
        real_pair(&path);
        assert_eq!(
            cursor.poll(100, 105).unwrap().receipt,
            Some((104, "fresh".into()))
        );
    }

    #[test]
    fn future_source_time_is_deferred_and_never_rewritten_or_dropped() {
        let (tmp, path) = setup();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        append(
            &path,
            serde_json::json!({"timestamp":"1970-01-01T00:00:00.105Z","type":"event_msg","payload":{"type":"task_complete"}}),
        );
        let before = cursor.offset;
        assert!(cursor.poll(100, 104).unwrap().ended.is_none());
        assert_eq!(cursor.offset, before);
        assert_eq!(cursor.poll(100, 106).unwrap().ended, Some((105, "success")));
    }

    #[test]
    fn quiet_success_is_periodically_saved_without_a_tool_heartbeat() {
        let (tmp, path) = setup();
        let mut cursor = Cursor::attach(&path, tmp.path(), ID, 100).unwrap();
        assert!(!cursor.poll(100, 101).unwrap().changed);
        let tick = cursor.poll(100, 60_101).unwrap();
        assert!(tick.changed);
        assert!(tick.receipt.is_none());
        assert_eq!(cursor.last_success_at, 60_101);
    }
}
