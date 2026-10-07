use cc_ask::{
    core::*,
    tick_runtime::{Deadline, HardLimit},
};
use std::{
    path::Path,
    process::{Command, Stdio},
    time::{Duration, Instant},
};

fn fixture_command(mode: &str, root: &Path) -> Command {
    let mut command = Command::new(std::env::current_exe().unwrap());
    command
        .args(["--exact", "process_capture_fixture", "--nocapture"])
        .env("CC_ASK_CAPTURE_FIXTURE", mode)
        .env("CC_ASK_CAPTURE_ROOT", root);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    command
}

#[test]
// Intentionally detach descendants to reproduce inherited pipes and process-tree
// cleanup. The supervising tests below verify release/exit or OS job termination.
#[allow(clippy::zombie_processes)]
fn process_capture_fixture() {
    let Ok(mode) = std::env::var("CC_ASK_CAPTURE_FIXTURE") else {
        return;
    };
    let root = std::path::PathBuf::from(std::env::var_os("CC_ASK_CAPTURE_ROOT").unwrap());
    if mode == "descendant" {
        let limit = Instant::now() + Duration::from_secs(4);
        while !root.join("release").exists() && Instant::now() < limit {
            std::thread::sleep(Duration::from_millis(10));
        }
        std::fs::write(root.join("exited"), b"done").unwrap();
        return;
    }
    if mode == "parent" {
        // Keep inherited stdout/stderr open beyond the direct parent's lifetime.
        let _child = fixture_command("descendant", &root)
            .stdout(Stdio::inherit())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap();
        println!("direct child complete");
        return;
    }
    if mode == "tree" {
        let _child = fixture_command("holder", &root)
            .stdout(Stdio::inherit())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap();
        std::thread::sleep(Duration::from_secs(5));
        return;
    }
    if mode == "failed_parent" {
        let _child = fixture_command("holder", &root)
            .stdout(Stdio::inherit())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap();
        let until = Instant::now() + Duration::from_secs(2);
        while !root.join("ready").exists() && Instant::now() < until {
            std::thread::sleep(Duration::from_millis(10));
        }
        std::process::exit(7);
    }
    let _lock = ByteLock::acquire(&root.join("state.lock"), Duration::ZERO).unwrap();
    std::fs::write(root.join("ready"), b"held").unwrap();
    let _stderr_lock = if mode == "hard_stderr" {
        Some(std::io::stderr().lock())
    } else {
        None
    };
    let _hard = if mode == "hard" || mode == "hard_stderr" {
        Some(HardLimit::new(root.join("tick.log"), Duration::from_millis(150)).unwrap())
    } else {
        None
    };
    std::thread::sleep(Duration::from_secs(5));
}

fn run_fixture(mode: &str, root: &Path, timeout: u64) -> Result<Output> {
    let exe = std::env::current_exe().unwrap();
    let arguments = vec![
        exe.to_string_lossy().into_owned(),
        "--exact".into(),
        "process_capture_fixture".into(),
        "--nocapture".into(),
    ];
    run_hidden_with_env(
        &arguments,
        timeout,
        &[
            ("CC_ASK_CAPTURE_FIXTURE".into(), mode.into()),
            (
                "CC_ASK_CAPTURE_ROOT".into(),
                root.to_string_lossy().into_owned(),
            ),
        ],
    )
}

#[test]
fn zero_child_timeout_is_bounded_by_tick_and_terminates_lock_holder() {
    let root = tempfile::tempdir().unwrap();
    let start = Instant::now();
    let deadline = Deadline::new(Duration::from_millis(350));
    let result = run_fixture("hang", root.path(), 0);
    drop(deadline);
    assert!(result.unwrap_err().starts_with("TimeoutExpired:"));
    assert!(start.elapsed() < Duration::from_secs(2));
    assert!(root.path().join("ready").exists());
    assert!(ByteLock::acquire(&root.path().join("state.lock"), Duration::from_secs(1)).is_ok());
}

#[test]
fn deadline_terminates_temporary_grandchild_holding_a_lock() {
    let root = tempfile::tempdir().unwrap();
    let deadline = Deadline::new(Duration::from_millis(500));
    let result = run_fixture("tree", root.path(), 0);
    drop(deadline);
    assert!(result.unwrap_err().starts_with("TimeoutExpired:"));
    assert!(
        root.path().join("ready").exists(),
        "grandchild fixture never held the lock"
    );
    assert!(
        ByteLock::acquire(&root.path().join("state.lock"), Duration::from_secs(1)).is_ok(),
        "temporary grandchild survived the deadline"
    );
}

#[test]
fn failed_launcher_does_not_release_its_temporary_grandchild_tree() {
    let root = tempfile::tempdir().unwrap();
    let output = run_fixture("failed_parent", root.path(), 3).unwrap();
    assert_eq!(output.code, 7);
    assert!(root.path().join("ready").exists());
    assert!(ByteLock::acquire(&root.path().join("state.lock"), Duration::from_secs(1)).is_ok());
}

#[test]
fn inherited_grandchild_output_cannot_block_capture_after_parent_exit() {
    let root = tempfile::tempdir().unwrap();
    let start = Instant::now();
    let output = run_fixture("parent", root.path(), 3).unwrap();
    let elapsed = start.elapsed();
    std::fs::write(root.path().join("release"), b"stop").unwrap();
    let until = Instant::now() + Duration::from_secs(2);
    while !root.path().join("exited").exists() && Instant::now() < until {
        std::thread::sleep(Duration::from_millis(10));
    }
    assert!(
        root.path().join("exited").exists(),
        "owned descendant did not exit"
    );
    assert!(
        elapsed < Duration::from_secs(3),
        "capture waited for an inherited output handle: {elapsed:?}"
    );
    assert_eq!(output.code, 0);
    assert!(output.stdout.contains("direct child complete"));
}

#[test]
fn hard_tick_ceiling_releases_native_locks_and_records_deferral() {
    let root = tempfile::tempdir().unwrap();
    let start = Instant::now();
    let output = run_fixture("hard", root.path(), 3).unwrap();
    assert_eq!(output.code, 0);
    assert!(start.elapsed() < Duration::from_secs(3));
    assert!(ByteLock::acquire(&root.path().join("state.lock"), Duration::from_secs(1)).is_ok());
    assert!(std::fs::read_to_string(root.path().join("tick.log"))
        .unwrap()
        .contains("remaining work deferred; releasing locks"));
}

#[test]
fn hard_ceiling_does_not_wait_for_stuck_stderr_logger() {
    let root = tempfile::tempdir().unwrap();
    let start = Instant::now();
    let output = run_fixture("hard_stderr", root.path(), 3).unwrap();
    assert_eq!(output.code, 0);
    assert!(start.elapsed() < Duration::from_secs(2));
    assert!(ByteLock::acquire(&root.path().join("state.lock"), Duration::from_secs(1)).is_ok());
}

#[test]
fn tick_deadline_bounds_lock_wait_without_stealing_held_lock() {
    let root = tempfile::tempdir().unwrap();
    let path = root.path().join("state.lock");
    let _held = ByteLock::acquire(&path, Duration::ZERO).unwrap();
    let start = Instant::now();
    let _deadline = Deadline::new(Duration::from_millis(40));
    assert!(ByteLock::acquire(&path, Duration::from_secs(60)).is_err());
    assert!(start.elapsed() < Duration::from_secs(1));
    drop(_deadline);
    assert!(ByteLock::acquire(&path, Duration::ZERO).is_err());
}
