#![cfg(windows)]
use cc_ocx_watchdog::windows::{TerminationOutcome, discover};
use cc_ocx_watchdog::{Config, hidden_command, probe};
use std::net::{Ipv4Addr, TcpListener};
use std::path::PathBuf;
use std::process::Child;
use std::time::{Duration, Instant};

struct Fixture {
    child: Child,
    home: PathBuf,
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = std::fs::remove_dir_all(&self.home);
    }
}

#[test]
fn pinned_process_with_missing_records_is_proven_and_rechecked() {
    let port = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))
        .unwrap()
        .local_addr()
        .unwrap()
        .port();
    assert_ne!(port, 10100);
    let home = std::env::temp_dir().join(format!("ocx-process-owner-{}", std::process::id()));
    std::fs::create_dir(&home).unwrap();
    let cli = home.join("inert cli.ts");
    std::fs::write(&cli, "// synthetic test only").unwrap();
    let exe = PathBuf::from(env!("CARGO_BIN_EXE_ocx_watchdog_fixture"));
    let mut command = hidden_command(&exe);
    command
        .args([
            cli.as_os_str(),
            "start".as_ref(),
            "--port".as_ref(),
            port.to_string().as_ref(),
        ])
        .env("OPENCODEX_HOME", &home);
    let mut fixture = Fixture {
        child: command.spawn().unwrap(),
        home,
    };
    let config = Config {
        home: fixture.home.clone(),
        expected_exe: exe,
        expected_cli: Some(cli),
        port,
        task: "unused-in-unit-test".into(),
        log_dir: fixture.home.clone(),
        discord_exe: None,
        timeout_ms: 1000,
        interval_ms: 10,
        startup_grace_ms: 0,
        cooldown_ms: 0,
    };
    let deadline = Instant::now() + Duration::from_secs(10);
    let held = loop {
        match discover(&config) {
            Ok(Some(process)) => break process,
            outcome => {
                assert!(
                    Instant::now() < deadline,
                    "discovery failed: {:?}",
                    outcome.err()
                );
                std::thread::sleep(Duration::from_millis(10));
            }
        }
    };
    assert_eq!(held.identity.pid, fixture.child.id());
    assert!(!config.home.join("ocx.pid").exists());
    assert!(!config.home.join("runtime-port.json").exists());
    assert!(probe(port, Duration::from_secs(3), held.identity.pid).is_ok());
    let mut disabled = config.clone();
    disabled.expected_cli = None;
    assert_eq!(
        discover(&disabled).err().as_deref(),
        Some("owner_record_missing")
    );
    let mut foreign_cli = config.clone();
    let other = config.home.join("other.ts");
    std::fs::write(&other, "// inert").unwrap();
    foreign_cli.expected_cli = Some(other);
    assert_eq!(
        discover(&foreign_cli).err().as_deref(),
        Some("owner_process_command")
    );
    let mut foreign_home = config.clone();
    foreign_home.home = config.home.join("foreign");
    std::fs::create_dir(&foreign_home.home).unwrap();
    assert_eq!(
        discover(&foreign_home).err().as_deref(),
        Some("owner_process_home")
    );
    assert_eq!(
        held.terminate_if_still_owner(&foreign_home)
            .err()
            .as_deref(),
        Some("owner_process_home")
    );
    assert!(fixture.child.try_wait().unwrap().is_none());
    let record = config.home.join("runtime-port.json");
    std::fs::write(
        &record,
        format!("{{\"pid\":{},\"port\":{port}}}", held.identity.pid + 1),
    )
    .unwrap();
    assert_eq!(
        discover(&config).err().as_deref(),
        Some("runtime_record_disagrees")
    );
    assert_eq!(
        held.terminate_if_still_owner(&config).err().as_deref(),
        Some("runtime_record_disagrees")
    );
    std::fs::write(&record, "not json").unwrap();
    assert_eq!(
        held.terminate_if_still_owner(&config).err().as_deref(),
        Some("runtime_record_invalid")
    );
    assert!(fixture.child.try_wait().unwrap().is_none());
    std::fs::remove_file(record).unwrap();
    assert_eq!(
        held.terminate_if_still_owner(&config).unwrap(),
        TerminationOutcome::Confirmed
    );
    assert!(!fixture.child.wait().unwrap().success());
}
