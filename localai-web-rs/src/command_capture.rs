//! Bounded file capture avoids inherited pipes blocking the resident indefinitely.
use std::{
    io::{Read, Seek},
    path::Path,
    process::{Command, Stdio},
    time::{Duration, Instant},
};

pub fn run(program: &Path, args: &[String], cwd: &Path) -> Result<String, &'static str> {
    let name = program.file_stem().and_then(|v| v.to_str()).unwrap_or("");
    let limit = match name {
        "gh" => 8,
        "cc-inbox" => 20,
        _ => 45,
    };
    let timeout =
        cc_ask::tick_runtime::timeout(Duration::from_secs(limit)).map_err(|_| "command_timeout")?;
    let start = Instant::now();
    let mut output = tempfile::tempfile().map_err(|_| "tool_failed")?;
    let mut command = Command::new(program);
    command
        .args(args)
        .current_dir(cwd)
        .stdin(Stdio::null())
        .stdout(output.try_clone().map_err(|_| "tool_failed")?)
        .stderr(Stdio::null());
    let (mut child, group) =
        cc_ask::ProcessGroup::spawn(&mut command).map_err(|_| "tool_failed")?;
    let status = loop {
        if output.metadata().map_err(|_| "tool_failed")?.len() > 2 * 1024 * 1024 {
            group.stop(&mut child);
            return Err("output_limit_exceeded");
        }
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if start.elapsed() < timeout => std::thread::sleep(Duration::from_millis(20)),
            _ => {
                group.stop(&mut child);
                return Err("command_timeout");
            }
        }
    };
    if !status.success() {
        group.stop(&mut child);
        return Err("tool_failed");
    }
    group.completed().map_err(|_| "tool_failed")?;
    output.rewind().map_err(|_| "tool_failed")?;
    let mut bytes = Vec::new();
    output
        .take(2 * 1024 * 1024 + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "tool_failed")?;
    if bytes.len() > 2 * 1024 * 1024 {
        return Err("tool_failed");
    }
    String::from_utf8(bytes).map_err(|_| "tool_failed")
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    #[test]
    #[ignore = "child fixture"]
    fn fixture_large_output() {
        use std::io::Write;
        let mut output = std::io::stdout().lock();
        let block = [b'x'; 128 * 1024];
        for _ in 0..32 {
            output.write_all(&block).unwrap();
        }
        std::thread::sleep(Duration::from_secs(30));
    }
    #[test]
    fn stops_large_output_before_waiting_for_process_exit() {
        let args = vec![
            "--exact".into(),
            "agent::command_capture::tests::fixture_large_output".into(),
            "--ignored".into(),
            "--nocapture".into(),
        ];
        let start = Instant::now();
        let result = run(
            &std::env::current_exe().unwrap(),
            &args,
            Path::new(env!("CARGO_MANIFEST_DIR")),
        );
        assert_eq!(result, Err("output_limit_exceeded"));
        assert!(start.elapsed() < Duration::from_secs(5));
    }
    #[test]
    fn real_command_success_failure_and_whole_flight_timeout() {
        let cwd = Path::new(env!("CARGO_MANIFEST_DIR"));
        let args = |code: &str| vec!["/c".to_owned(), code.to_owned()];
        assert!(
            run(Path::new("cmd.exe"), &args("echo capture-ok"), cwd)
                .unwrap()
                .contains("capture-ok")
        );
        assert_eq!(
            run(Path::new("cmd.exe"), &args("exit 7"), cwd),
            Err("tool_failed")
        );
        let deadline = cc_ask::tick_runtime::Deadline::new(Duration::from_millis(200));
        let start = Instant::now();
        assert_eq!(
            run(Path::new("cmd.exe"), &args("ping -n 30 127.0.0.1"), cwd),
            Err("command_timeout")
        );
        assert!(start.elapsed() < Duration::from_secs(5));
        drop(deadline);
        assert!(
            run(Path::new("cmd.exe"), &args("echo recovered"), cwd)
                .unwrap()
                .contains("recovered")
        );
    }
}
