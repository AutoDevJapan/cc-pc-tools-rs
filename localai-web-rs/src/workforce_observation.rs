//! Finite invocation of the existing metadata collector, once per resident refresh.
//! HTTP consumers use the resulting shared generation and never launch this tool.
use serde_json::Value;
use std::{
    io::{Read, Seek},
    path::Path,
    process::{Command, Stdio},
    time::{Duration, Instant},
};

pub fn acquire(root: &Path) -> Result<Value, &'static str> {
    let program = root.join("agent-progress/agent-progress.exe");
    if !program.is_file() {
        return Err("collector_unavailable");
    }
    let mut output = tempfile::tempfile().map_err(|_| "capture_failed")?;
    let mut command = Command::new(program);
    command
        .args([
            "--once",
            "--active-only",
            "--deadline",
            "8s",
            "--max-rows",
            "1000",
        ])
        .arg("--todo")
        .arg(root.join("todo/todo.json"))
        .arg("--jobs")
        .arg(root.join("codex-jobs/jobs"))
        .arg("--priority-file")
        .arg(root.join("scheduler/priorities.json"))
        .arg("--resource-queue")
        .arg(root.join("civ6-live/queue"))
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .stdout(output.try_clone().map_err(|_| "capture_failed")?);
    // ProcessGroup applies CREATE_NO_WINDOW and owns this finite process tree.
    let started = Instant::now();
    let (mut child, group) =
        cc_ask::ProcessGroup::spawn(&mut command).map_err(|_| "collector_failed")?;
    let status = loop {
        if output.metadata().map(|m| m.len()).unwrap_or(u64::MAX) > 4 * 1024 * 1024 {
            group.stop(&mut child);
            let _ = child.wait();
            return Err("capture_limit");
        }
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if started.elapsed() < Duration::from_secs(12) => {
                std::thread::sleep(Duration::from_millis(20))
            }
            _ => {
                group.stop(&mut child);
                let _ = child.wait();
                return Err("collector_timeout");
            }
        }
    };
    // Exit 1 carries a valid partial acquisition. Exit 2 never does.
    if !matches!(status.code(), Some(0 | 1)) {
        group.stop(&mut child);
        return Err("collector_failed");
    }
    // No subprocess is intended to outlive this read-only collector.
    group.stop(&mut child);
    output.rewind().map_err(|_| "capture_failed")?;
    let mut bytes = Vec::new();
    output
        .take(4 * 1024 * 1024 + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "capture_failed")?;
    if bytes.len() > 4 * 1024 * 1024 {
        return Err("capture_limit");
    }
    serde_json::from_slice(&bytes).map_err(|_| "collector_invalid")
}
