use serde::{Deserialize, Serialize};
use std::io::{Read, Write};
use std::net::{Ipv4Addr, SocketAddr, TcpStream};
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

#[cfg(windows)]
pub mod windows;

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Config {
    pub home: PathBuf,
    pub expected_exe: PathBuf,
    pub port: u16,
    pub task: String,
    pub log_dir: PathBuf,
    pub discord_exe: Option<PathBuf>,
    pub timeout_ms: u64,
    pub interval_ms: u64,
    pub startup_grace_ms: u64,
    pub cooldown_ms: u64,
}

impl Config {
    pub fn validate(&self) -> Result<(), String> {
        if self.port == 0 || self.timeout_ms == 0 || self.interval_ms == 0 {
            return Err("port, timeout and interval must be positive".into());
        }
        for path in [&self.home, &self.expected_exe, &self.log_dir] {
            if !path.is_absolute() {
                return Err("all paths must be absolute".into());
            }
        }
        if self.discord_exe.as_ref().is_some_and(|p| !p.is_absolute()) {
            return Err("discord path must be absolute".into());
        }
        if self.task.is_empty() {
            return Err("task must be supplied".into());
        }
        Ok(())
    }
}

// A new process incarnation cannot inherit an old process's failures, even if Windows reuses its PID.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
pub struct Identity {
    pub pid: u32,
    pub born: u64,
}

#[derive(Default)]
pub struct FailureStreak {
    identity: Option<Identity>,
    failures: u8,
}

impl FailureStreak {
    pub fn observe(&mut self, identity: Option<Identity>, healthy: bool, eligible: bool) -> bool {
        if self.identity != identity {
            self.identity = identity;
            self.failures = 0;
        }
        if identity.is_none() || healthy || !eligible {
            self.failures = 0;
            return false;
        }
        self.failures = self.failures.saturating_add(1);
        self.failures == 3
    }
    pub fn failures(&self) -> u8 {
        self.failures
    }
    pub fn reset(&mut self) {
        self.identity = None;
        self.failures = 0;
    }
}

// The wall-clock deadline spans connect, write, headers and body, including slow trickles.
// Never obey a redirect, consult a system HTTP proxy, or read an unbounded response.
pub fn probe(port: u16, timeout: Duration, expected_pid: u32) -> Result<(), String> {
    let deadline = Instant::now() + timeout;
    let address = SocketAddr::from((Ipv4Addr::LOCALHOST, port));
    let mut stream =
        TcpStream::connect_timeout(&address, timeout).map_err(|_| "connect".to_owned())?;
    stream
        .set_write_timeout(Some(remaining(deadline)?))
        .map_err(|_| "socket".to_owned())?;
    let request =
        format!("GET /healthz HTTP/1.0\r\nHost: 127.0.0.1:{port}\r\nConnection: close\r\n\r\n");
    stream
        .write_all(request.as_bytes())
        .map_err(|_| "write".to_owned())?;
    let mut data = Vec::new();
    loop {
        stream
            .set_read_timeout(Some(remaining(deadline)?))
            .map_err(|_| "socket".to_owned())?;
        let mut chunk = [0u8; 1024];
        let read = stream
            .read(&mut chunk)
            .map_err(|_| "read_timeout_or_error".to_owned())?;
        if read == 0 {
            break;
        }
        data.extend_from_slice(&chunk[..read]);
        if data.len() > 16 * 1024 {
            return Err("response_too_large".into());
        }
        if response_complete(&data)? {
            break;
        }
    }
    validate_health(&data, expected_pid)
}

fn response_complete(data: &[u8]) -> Result<bool, String> {
    let Some(boundary) = data.windows(4).position(|w| w == b"\r\n\r\n") else {
        return Ok(false);
    };
    let headers = std::str::from_utf8(&data[..boundary]).map_err(|_| "headers")?;
    let mut length = None;
    for line in headers.lines().skip(1) {
        let Some((name, value)) = line.split_once(':') else {
            continue;
        };
        if name.eq_ignore_ascii_case("content-length") {
            let bytes: usize = value.trim().parse().map_err(|_| "content_length")?;
            if bytes > 16 * 1024 || length.is_some() {
                return Err("content_length".into());
            }
            length = Some(bytes);
        }
    }
    Ok(length.is_some_and(|n| data.len() >= boundary + 4 + n))
}

fn remaining(deadline: Instant) -> Result<Duration, String> {
    deadline
        .checked_duration_since(Instant::now())
        .filter(|d| !d.is_zero())
        .ok_or_else(|| "deadline".to_owned())
}

pub fn validate_health(data: &[u8], expected_pid: u32) -> Result<(), String> {
    let boundary = data
        .windows(4)
        .position(|w| w == b"\r\n\r\n")
        .ok_or("headers")?;
    let headers = std::str::from_utf8(&data[..boundary]).map_err(|_| "headers")?;
    let status = headers
        .lines()
        .next()
        .unwrap_or("")
        .split_whitespace()
        .nth(1);
    // HTTP/1.0 asks for a close-delimited response. Bun returns a plain JSON body.
    // Reject unexpected framing rather than interpreting transfer-encoded bytes as JSON.
    if headers
        .lines()
        .skip(1)
        .any(|s| s.to_ascii_lowercase().starts_with("transfer-encoding:"))
    {
        return Err("transfer_encoding".into());
    }
    let body: serde_json::Value =
        serde_json::from_slice(&data[boundary + 4..]).map_err(|_| "json")?;
    if body["service"] != "opencodex" || body["pid"].as_u64() != Some(expected_pid as u64) {
        return Err("identity_or_status".into());
    }
    if status != Some("200") || body["status"] != "ok" {
        // A complete, identified degraded response is alive, including drain/restart-required.
        return Err("responding_degraded".into());
    }
    Ok(())
}

pub fn unresponsive(outcome: &Result<(), String>) -> bool {
    matches!(
        outcome.as_ref().err().map(String::as_str),
        Some("connect" | "write" | "read_timeout_or_error" | "deadline")
    )
}

pub fn hidden_command(program: impl AsRef<std::ffi::OsStr>) -> Command {
    let mut command = Command::new(program);
    command
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000); // CREATE_NO_WINDOW, even when Windows Terminal is the default.
    }
    command
}

pub fn run_bounded(mut command: Command, timeout: Duration) -> Result<(), String> {
    let mut child = command.spawn().map_err(|e| format!("spawn: {e}"))?;
    let deadline = Instant::now() + timeout;
    loop {
        match child.try_wait() {
            Ok(Some(status)) => {
                return if status.success() {
                    Ok(())
                } else {
                    Err(format!("exit: {status}"))
                };
            }
            Ok(None) if Instant::now() < deadline => std::thread::sleep(Duration::from_millis(100)),
            outcome => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(if let Err(e) = outcome {
                    format!("wait: {e}")
                } else {
                    "child_timeout".into()
                });
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::TcpListener;
    fn identity(pid: u32, born: u64) -> Identity {
        Identity { pid, born }
    }
    #[test]
    fn three_failures_only() {
        let mut s = FailureStreak::default();
        assert!(!s.observe(Some(identity(3, 5)), false, true));
        assert!(!s.observe(Some(identity(3, 5)), false, true));
        assert!(s.observe(Some(identity(3, 5)), false, true));
        assert!(!s.observe(Some(identity(3, 5)), false, true));
    }
    #[test]
    fn success_resets_failures() {
        let mut s = FailureStreak::default();
        for _ in 0..2 {
            assert!(!s.observe(Some(identity(3, 5)), false, true));
        }
        assert!(!s.observe(Some(identity(3, 5)), true, true));
        assert!(!s.observe(Some(identity(3, 5)), false, true));
        assert_eq!(s.failures(), 1);
    }
    #[test]
    fn absent_grace_and_new_incarnation_reset() {
        for change in [None, Some(identity(4, 5)), Some(identity(3, 6))] {
            let mut s = FailureStreak::default();
            for _ in 0..2 {
                s.observe(Some(identity(3, 5)), false, true);
            }
            assert!(!s.observe(change, false, true));
        }
        let mut s = FailureStreak::default();
        s.observe(Some(identity(3, 5)), false, true);
        s.observe(Some(identity(3, 5)), false, false);
        assert_eq!(s.failures(), 0);
    }
    #[test]
    fn health_needs_both_process_and_service_identity() {
        let good =
            b"HTTP/1.0 200 OK\r\n\r\n{\"service\":\"opencodex\",\"status\":\"ok\",\"pid\":7}";
        assert!(validate_health(good, 7).is_ok());
        assert!(validate_health(good, 8).is_err());
        let degraded = b"HTTP/1.0 503 Unavailable\r\n\r\n{\"service\":\"opencodex\",\"status\":\"restart_required\",\"pid\":7}";
        assert_eq!(
            validate_health(degraded, 7).unwrap_err(),
            "responding_degraded"
        );
        assert!(!unresponsive(&validate_health(degraded, 7)));
        for bad in [
            b"HTTP/1.0 200 OK\r\n\r\n{}".as_slice(),
            b"HTTP/1.0 503 Failed\r\n\r\n{}",
            b"broken",
            b"HTTP/1.0 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n",
        ] {
            assert!(validate_health(bad, 7).is_err());
        }
    }
    #[test]
    fn complete_content_length_does_not_wait_for_connection_close() {
        let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap();
        let port = listener.local_addr().unwrap().port();
        let server = std::thread::spawn(move || {
            let (mut socket, _) = listener.accept().unwrap();
            let mut req = [0; 1024];
            assert!(socket.read(&mut req).unwrap() > 0);
            let body = "{\"service\":\"opencodex\",\"status\":\"ok\",\"pid\":7}";
            write!(
                socket,
                "HTTP/1.1 200 OK\r\nContent-Length: {}\r\n\r\n{body}",
                body.len()
            )
            .unwrap();
            std::thread::sleep(Duration::from_secs(2));
        });
        assert!(probe(port, Duration::from_millis(500), 7).is_ok());
        server.join().unwrap();
    }
    #[test]
    fn actual_hung_socket_obeys_deadline() {
        let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap();
        let port = listener.local_addr().unwrap().port();
        let server = std::thread::spawn(move || {
            let (_socket, _) = listener.accept().unwrap();
            std::thread::sleep(Duration::from_millis(250));
        });
        let start = Instant::now();
        assert!(probe(port, Duration::from_millis(70), 7).is_err());
        assert!(start.elapsed() < Duration::from_millis(200));
        server.join().unwrap();
    }
    #[test]
    fn slow_trickle_cannot_extend_deadline() {
        let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap();
        let port = listener.local_addr().unwrap().port();
        let server = std::thread::spawn(move || {
            let (mut socket, _) = listener.accept().unwrap();
            for _ in 0..12 {
                let _ = socket.write_all(b"x");
                std::thread::sleep(Duration::from_millis(15));
            }
        });
        let start = Instant::now();
        assert!(probe(port, Duration::from_millis(60), 7).is_err());
        assert!(start.elapsed() < Duration::from_millis(150));
        server.join().unwrap();
    }
    #[test]
    fn actual_health_response_and_cap() {
        for oversize in [false, true] {
            let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap();
            let port = listener.local_addr().unwrap().port();
            let server = std::thread::spawn(move || {
                let (mut socket, _) = listener.accept().unwrap();
                let mut req = [0; 1024];
                assert!(socket.read(&mut req).unwrap() > 0);
                let bytes = if oversize {
                    vec![b'x'; 20 * 1024]
                } else {
                    b"HTTP/1.0 200 OK\r\n\r\n{\"service\":\"opencodex\",\"status\":\"ok\",\"pid\":7}".to_vec()
                };
                let _ = socket.write_all(&bytes);
            });
            let result = probe(port, Duration::from_secs(2), 7);
            assert_eq!(result.is_ok(), !oversize, "{result:?}");
            server.join().unwrap();
        }
    }
}
