//! Bound the native client's reconnect loop before its first operation. This is
//! part of the existing supervisor, not another monitor or a model retry budget.
use crate::*;
use std::{
    fs,
    io::{BufRead, Read, Seek, SeekFrom},
};

pub(crate) const GRACE_S: f64 = 30.;

#[derive(Clone)]
pub(crate) struct Failure {
    kind: &'static str,
    message: String,
    deadline: f64,
}

#[derive(Default)]
pub(crate) struct PretoolFailure {
    failure: Option<Failure>,
    operation_seen: bool,
    completed: bool,
}
impl PretoolFailure {
    pub(crate) fn event(&mut self, ev: &Value, now: f64) {
        let t = s(ev, "type");
        if t == "turn.completed" {
            self.completed = true;
            self.failure = None;
        } else if t == "turn.started" {
            self.failure = None;
        } else if t.starts_with("item.") && s(&ev["item"], "type") != "error" {
            // Also protect unknown future tool kinds and tool completions that
            // arrived without a start. Completed-step/token counters cannot
            // establish that no operation has begun.
            self.operation_seen = true;
            self.failure = None;
        } else if let Some(kind) = crate::capacity::failure_kind(ev) {
            match kind {
                "quota" | "capacity" if !self.operation_seen && !self.completed => {
                    // Reconnecting notifications must not renew the deadline.
                    self.failure.get_or_insert_with(|| Failure {
                        kind,
                        message: if t == "error" {
                            s(ev, "message").into()
                        } else {
                            err_text(&ev["error"])
                        },
                        deadline: now + GRACE_S,
                    });
                    if let Some(failure) = self.failure.as_mut()
                        && failure.kind == "quota"
                        && kind == "capacity"
                    {
                        failure.kind = kind;
                        failure.message = if t == "error" {
                            s(ev, "message").into()
                        } else {
                            err_text(&ev["error"])
                        };
                    }
                }
                "auth" | "cancel" => self.failure = None,
                _ => (), // A generic terminal error does not erase the cause.
            }
        }
    }
    pub(crate) fn due(&self, now: f64) -> Option<&Failure> {
        self.failure.as_ref().filter(|f| now >= f.deadline)
    }
}

impl Context {
    /// Re-read the exact current attempt after freezing controllers. A partial,
    /// malformed or oversized stream is uncertainty, not an idle tool boundary.
    fn pretool_evidence(&self, job: &Value, expected: &Failure) -> Result<bool> {
        let mut file = fs::File::open(self.job_dir(s(job, "id")).join("events.jsonl"))?;
        let offset = job["attempt_events_offset"]
            .as_u64()
            .ok_or_else(|| Error::new("missing attempt offset"))?;
        let len = file.metadata()?.len();
        if len < offset || len - offset > 1024 * 1024 {
            return Ok(false);
        }
        file.seek(SeekFrom::Start(offset))?;
        let mut reader = std::io::BufReader::new(file.take(len - offset));
        let mut evidence = PretoolFailure::default();
        loop {
            let mut line = Vec::new();
            if reader.read_until(b'\n', &mut line)? == 0 {
                break;
            }
            if !line.ends_with(b"\n") {
                return Ok(false);
            }
            let Ok(ev) = serde_json::from_slice::<Value>(&line) else {
                return Ok(false);
            };
            if !ev.is_object() {
                return Ok(false);
            }
            evidence.event(&ev, 0.);
        }
        Ok(evidence
            .failure
            .is_some_and(|f| f.kind == expected.kind && f.message == expected.message))
    }

    pub(crate) fn reap_pretool_failure(
        &self,
        snapshot: &Value,
        failure: &Failure,
    ) -> Result<Option<Value>> {
        let jid = s(snapshot, "id");
        let Some(_reservation) = self.yield_reservation(jid) else {
            return Ok(None);
        };
        let pid = number(snapshot, "codex_pid") as u32;
        let Some(created) = snapshot["codex_pid_created"].as_u64() else {
            return Ok(None);
        };
        // No baseline exemption: every descendant must be an idle controller.
        // The existing retained-handle guard rejects racing/new tool workers.
        let Some(mut child) = self.rt.pause_idle_agent(
            pid,
            created,
            None,
            // The existing refresh/admission paths recover this marker if
            // the owning supervisor crashes while controllers are frozen.
            Some(&self.job_dir(jid).join("yield-paused.json")),
        )?
        else {
            return Ok(None);
        };
        // Use the same bounded pump settling window as the existing handoff.
        // The native controllers remain frozen throughout this revalidation.
        let path = self.job_dir(jid).join("events.jsonl");
        let mut length = fs::metadata(&path)?.len();
        let mut stable = 0;
        for _ in 0..20 {
            self.rt.sleep(0.05);
            let current = fs::metadata(&path)?.len();
            stable = if current == length { stable + 1 } else { 0 };
            length = current;
            if stable >= 4 {
                break;
            }
        }
        if stable < 4 || !self.pretool_evidence(snapshot, failure)? {
            return Ok(None);
        }
        let _state = self.job_lock(jid, "state.lock")?;
        let mut fresh = self.load(jid)?;
        if !crate::pool_retry::same_attempt(snapshot, &fresh)
            || ["thread_id", "resume_session"]
                .iter()
                .any(|k| snapshot[*k] != fresh[*k])
        {
            return Ok(None);
        }
        let _todo = if fresh["todo_id"].is_number() {
            Some(FileLock::acquire(
                &self.todo_store().with_file_name("todo.lock"),
            )?)
        } else {
            None
        };
        if _todo.is_some()
            && !crate::pool_retry::todo_owns_attempt(&self.read_json(&self.todo_store())?, &fresh)
        {
            return Ok(None);
        }
        fresh["protocol_stall"] = json!({"version":1,"stage":"stopping","failure_kind":failure.kind,
            "started_at":fresh["started_at"],"child_pid":pid,"child_created":created,
            "attempt_events_offset":fresh["attempt_events_offset"],"grace_seconds":GRACE_S,"at":self.rt.now()});
        fresh["last_failure_kind"] = json!(failure.kind);
        fresh["last_error"] = json!(failure.message);
        // Strict durable intent before stopping; Paused::stop waits for the
        // retained tree, including descendants, before reporting success.
        self.save(&fresh)?;
        child.stop()?;
        fresh["protocol_stall"]["stage"] = json!("drained");
        self.save(&fresh)?;
        Ok(Some(fresh))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn retries_do_not_renew_the_monotonic_deadline() {
        let ev = json!({"type":"error","message":"Reconnecting... 1/5 (429 Too Many Requests)"});
        let mut guard = PretoolFailure::default();
        guard.event(&ev, 7.);
        assert!(guard.due(36.999).is_none());
        guard.event(&ev, 36.);
        assert!(guard.due(37.).is_some());
    }
    #[test]
    fn progress_auth_cancel_and_tools_are_not_idle_quota_failures() {
        let quota = json!({"type":"error","message":"429 Too Many Requests"});
        for event in [
            json!({"type":"turn.started"}),
            json!({"type":"turn.completed"}),
            json!({"type":"turn.failed","error":{"code":"auth_failed","message":"429 Too Many Requests"}}),
            json!({"type":"turn.failed","error":{"code":"cancelled","message":"429 Too Many Requests"}}),
            json!({"type":"item.started","item":{"id":"t","type":"mcp_tool_call"}}),
            json!({"type":"item.completed","item":{"id":"t","type":"future_tool"}}),
        ] {
            let mut guard = PretoolFailure::default();
            guard.event(&quota, 0.);
            guard.event(&event, 29.);
            assert!(guard.due(31.).is_none(), "{event}");
        }
        for kind in ["command_execution", "mcp_tool_call", "agent_message"] {
            let mut guard = PretoolFailure::default();
            guard.event(&json!({"type":"item.completed","item":{"id":"t","type":kind,"aggregated_output":"429 Too Many Requests"}}), 0.);
            assert!(guard.due(500.).is_none());
            guard.event(&quota, 501.);
            assert!(guard.due(1000.).is_none(), "a tool already ran");
        }
        let mut guard = PretoolFailure::default();
        guard.event(&json!({"type":"error","message":"connection reset"}), 0.);
        assert!(guard.due(1000.).is_none());
    }
}
