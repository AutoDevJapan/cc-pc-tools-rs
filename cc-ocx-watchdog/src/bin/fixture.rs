#![cfg_attr(windows, windows_subsystem = "windows")]
// Disposable isolated listener for the watchdog's process-termination acceptance check.
use std::io::{Read, Write};
use std::net::{Ipv4Addr, TcpListener};
use std::path::PathBuf;
use std::time::Duration;

fn main() {
    let mut args = std::env::args().skip(1);
    let port: u16 = args.next().unwrap().parse().unwrap();
    assert_ne!(port, 10100, "fixture cannot use the production port");
    let mode = args.next().unwrap();
    let home = PathBuf::from(args.next().unwrap());
    let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, port)).unwrap();
    if args.next().as_deref() == Some("runtime-only") {
        std::fs::write(
            home.join("runtime-port.json"),
            format!("{{\"pid\":{},\"port\":{port}}}", std::process::id()),
        )
        .unwrap();
    } else {
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
