#![cfg_attr(windows, windows_subsystem = "windows")]
// Disposable isolated listener for the watchdog's process-termination acceptance check.
use std::io::{Read, Write};
use std::net::{Ipv4Addr, TcpListener};
use std::path::PathBuf;
use std::time::Duration;

fn main() {
    let mut args = std::env::args().skip(1);
    let first = args.next().unwrap();
    // Test-only Bun-shaped invocation: a pinned inert CLI file and home in env.
    let native_owner = first.parse::<u16>().is_err();
    let (port, mode, home, record_mode) = if native_owner {
        assert_eq!(args.next().as_deref(), Some("start"));
        assert_eq!(args.next().as_deref(), Some("--port"));
        (
            args.next().unwrap().parse::<u16>().unwrap(),
            std::env::var("OCX_FIXTURE_MODE").unwrap_or_else(|_| "healthy".into()),
            PathBuf::from(std::env::var_os("OPENCODEX_HOME").unwrap()),
            Some("none".to_owned()),
        )
    } else {
        (
            first.parse::<u16>().unwrap(),
            args.next().unwrap(),
            PathBuf::from(args.next().unwrap()),
            args.next(),
        )
    };
    assert_ne!(port, 10100, "fixture cannot use the production port");
    let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, port)).unwrap();
    if record_mode.as_deref() == Some("runtime-only") {
        std::fs::write(
            home.join("runtime-port.json"),
            format!("{{\"pid\":{},\"port\":{port}}}", std::process::id()),
        )
        .unwrap();
    } else if record_mode.as_deref() != Some("none") {
        std::fs::write(home.join("ocx.pid"), std::process::id().to_string()).unwrap();
    }
    for socket in listener.incoming() {
        let mut socket = socket.unwrap();
        if mode == "hang" {
            std::thread::sleep(Duration::from_secs(300));
            continue;
        }
        let mut request = [0; 1024];
        let _ = socket.read(&mut request);
        let body = format!(
            "{{\"service\":\"opencodex\",\"status\":\"ok\",\"pid\":{}}}",
            std::process::id()
        );
        let _ = write!(
            socket,
            "HTTP/1.0 200 OK\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
    }
}
