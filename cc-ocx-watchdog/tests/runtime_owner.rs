#![cfg(windows)]
use cc_ocx_watchdog::windows::{TerminationOutcome, discover};
use cc_ocx_watchdog::{Config, hidden_command, probe};
use std::net::{Ipv4Addr, TcpListener};
use std::path::PathBuf;
use std::process::Child;
use std::time::{Duration, Instant};

struct Fixture {
    child: Child,
    home: Home,
}
struct Home(PathBuf);
impl Drop for Home {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

#[test]
fn runtime_only_listener_is_verified_and_rechecked_before_termination() {
    let port = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))
        .unwrap()
        .local_addr()
        .unwrap()
        .port();
    assert_ne!(port, 10100);
    let home = Home(std::env::temp_dir().join(format!("ocx-runtime-owner-{}", std::process::id())));
    std::fs::create_dir(&home.0).unwrap();
    let exe = PathBuf::from(env!("CARGO_BIN_EXE_ocx_watchdog_fixture"));
    let mut command = hidden_command(&exe);
    command
        .arg(port.to_string())
        .arg("healthy")
        .arg(&home.0)
        .arg("runtime-only");
    let mut fixture = Fixture {
        child: command.spawn().unwrap(),
        home,
    };
    let config = Config {
        home: fixture.home.0.clone(),
        expected_exe: exe,
        port,
        task: "unused-in-unit-test".into(),
        log_dir: fixture.home.0.clone(),
        discord_exe: None,
        timeout_ms: 1000,
        interval_ms: 10,
        startup_grace_ms: 0,
        cooldown_ms: 0,
    };
    let deadline = Instant::now() + Duration::from_secs(10);
    let held = loop {
        if let Ok(Some(process)) = discover(&config) {
            break process;
        }
        assert!(Instant::now() < deadline, "fixture was not discovered");
        std::thread::sleep(Duration::from_millis(10));
    };
    assert_eq!(held.identity.pid, fixture.child.id());
    assert!(!config.home.join("ocx.pid").exists());
    assert!(probe(port, Duration::from_secs(3), held.identity.pid).is_ok());
    let mut wrong_exe = config.clone();
    wrong_exe.expected_exe = PathBuf::from("C:/unrelated.exe");
    assert_eq!(
        discover(&wrong_exe).err().as_deref(),
        Some("unexpected_executable")
    );
    let runtime_path = config.home.join("runtime-port.json");
    let original = std::fs::read(&runtime_path).unwrap();
    std::fs::write(
        &runtime_path,
        format!("{{\"pid\":{},\"port\":{port}}}", held.identity.pid + 1),
    )
    .unwrap();
    assert_eq!(
        held.terminate_if_still_owner(&config).err().as_deref(),
        Some("runtime_record_disagrees")
    );
    assert!(fixture.child.try_wait().unwrap().is_none());
    std::fs::write(&runtime_path, original).unwrap();
    std::fs::write(config.home.join("ocx.pid"), "1").unwrap();
    assert_eq!(
        held.terminate_if_still_owner(&config).err().as_deref(),
        Some("pid_record_disagrees")
    );
    assert!(fixture.child.try_wait().unwrap().is_none());
    std::fs::remove_file(config.home.join("ocx.pid")).unwrap();
    assert_eq!(
        held.terminate_if_still_owner(&config).unwrap(),
        TerminationOutcome::Confirmed
    );
    assert!(!fixture.child.wait().unwrap().success());
}
