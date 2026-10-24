//! All paths and handles belong to the fixture. No production jobs or ACLs.
use super::*;
use std::os::windows::{fs::OpenOptionsExt, process::CommandExt};
use std::thread;
use windows_sys::Win32::Storage::FileSystem::{FILE_SHARE_READ, FILE_SHARE_WRITE};

fn reader(path: &Path) -> fs::File {
    fs::OpenOptions::new()
        .read(true)
        .share_mode(FILE_SHARE_READ | FILE_SHARE_WRITE)
        .open(path)
        .unwrap()
}

#[test]
fn windows_reader_without_delete_share_exhausts_budget_without_losing_old_state() {
    let t = Test::new();
    let j = t.put(new_job());
    let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
    let old = fs::read(&path).unwrap();
    let held = reader(&path);
    // Wait is controlled by actual failed replacement attempts, not a race
    // between a fixed release sleep and a worker's startup time.
    *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(|seconds| {
        if seconds == 0.02 {
            thread::sleep(std::time::Duration::from_secs_f64(seconds));
        }
    }));
    let start = std::time::Instant::now();
    let err = t.ctx.save(&j).unwrap_err();
    assert_eq!(err.code, 75);
    assert!(err.message.contains("os error 5") || err.message.contains("os error 32"));
    assert!(start.elapsed() >= std::time::Duration::from_millis(900));
    assert!(start.elapsed() < std::time::Duration::from_secs(10));
    assert_eq!(fs::read(&path).unwrap(), old);
    drop(held);
    t.ctx.save(&j).unwrap();
    assert_eq!(t.ctx.load(s(&j, "id")).unwrap(), j);
}

#[test]
fn windows_sharing_release_is_observed_by_the_next_retry() {
    let t = Test::new();
    let j = t.put(new_job());
    let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
    let held = Arc::new(Mutex::new(Some(reader(&path))));
    let release = held.clone();
    *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |_| {
        release.lock().unwrap().take();
    }));
    let before = t.rt.replace_count.load(Ordering::Relaxed);
    let mut changed = j.clone();
    changed["activity"] = json!("new snapshot");
    t.ctx.save(&changed).unwrap();
    assert!(held.lock().unwrap().is_none());
    assert_eq!(t.rt.replace_count.load(Ordering::Relaxed) - before, 2);
    assert_eq!(t.ctx.load(s(&j, "id")).unwrap(), changed);
}

#[test]
fn windows_normal_rust_reader_allows_atomic_replacement() {
    let t = Test::new();
    let j = t.put(new_job());
    let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
    let mut held = fs::File::open(&path).unwrap();
    let mut changed = j.clone();
    changed["activity"] = json!("replacement");
    t.ctx.save(&changed).unwrap();
    let mut old = Vec::new();
    held.read_to_end(&mut old).unwrap();
    assert_eq!(serde_json::from_slice::<Value>(&old).unwrap(), j);
    assert_eq!(t.ctx.load(s(&j, "id")).unwrap(), changed);
}

// Deny DELETE on this synthetic file and DELETE_CHILD on its own directory.
// Restore the deny ACEs even on assertion failure; do not grant new rights.
struct DeniedDelete {
    paths: Vec<PathBuf>,
}
impl DeniedDelete {
    fn new(directory: &Path, file: &Path) -> Self {
        let mut guard = Self { paths: Vec::new() };
        for (path, rights) in [(directory, "(DC)"), (file, "(D)")] {
            let result = std::process::Command::new("icacls.exe")
                .creation_flags(NO_WINDOW)
                .arg(path)
                .args(["/deny", &format!("*S-1-1-0:{rights}")])
                .output()
                .unwrap();
            assert!(
                result.status.success(),
                "fixture ACL deny failed: {result:?}"
            );
            guard.paths.push(path.into());
        }
        guard
    }
    fn restore(mut self) -> io::Result<()> {
        let mut failed = false;
        for path in std::mem::take(&mut self.paths).iter().rev() {
            let result = std::process::Command::new("icacls.exe")
                .creation_flags(NO_WINDOW)
                .arg(path)
                .args(["/remove:d", "*S-1-1-0"])
                .output();
            failed |= !result.is_ok_and(|output| output.status.success());
        }
        if failed {
            Err(io::Error::other("fixture ACL restoration failed"))
        } else {
            Ok(())
        }
    }
}
impl Drop for DeniedDelete {
    fn drop(&mut self) {
        for path in self.paths.iter().rev() {
            let result = std::process::Command::new("icacls.exe")
                .creation_flags(NO_WINDOW)
                .arg(path)
                .args(["/remove:d", "*S-1-1-0"])
                .output();
            if !result.is_ok_and(|result| result.status.success()) {
                eprintln!("fixture ACL restoration failed: {}", path.display());
            }
        }
    }
}

#[test]
fn windows_acl_denial_without_a_reader_is_not_bypassed() {
    let t = Test::new();
    let j = t.put(new_job());
    let dir = t.ctx.job_dir(s(&j, "id"));
    let path = dir.join("job.json");
    let original = fs::read(&path).unwrap();
    let denied = DeniedDelete::new(&dir, &path);
    let mut changed = j.clone();
    changed["status"] = json!("succeeded");
    let error = t.ctx.save(&changed).unwrap_err();
    assert!(error.message.contains("os error 5"), "{error:?}");
    denied.restore().unwrap();
    assert_eq!(fs::read(&path).unwrap(), original);
    assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "queued");
    t.ctx.save(&changed).unwrap();
    assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "succeeded");
}

#[test]
fn windows_progress_sharing_failure_does_not_end_the_attempt_or_lose_its_reply() {
    let t = Test::new();
    let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
    let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
    let held = Arc::new(Mutex::new(None::<fs::File>));
    let armed = Arc::new(AtomicBool::new(false));
    let release = held.clone();
    let arm = armed.clone();
    *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |file, candidate| {
        if file != path {
            return Ok(());
        }
        if candidate["last_message"] == "DONE" {
            release.lock().unwrap().take();
        } else if number(candidate, "steps") > 0 && !arm.swap(true, Ordering::SeqCst) {
            *release.lock().unwrap() = Some(reader(file));
        }
        Ok(())
    }));
    *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(|seconds| {
        if seconds == 0.02 {
            thread::sleep(std::time::Duration::from_secs_f64(seconds));
        }
    }));
    let result = t.ctx.supervise(s(&j, "id"));
    held.lock().unwrap().take();
    assert!(result.is_ok(), "{result:?}");
    assert!(armed.load(Ordering::SeqCst));
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    assert!(t.rt.kills.lock().unwrap().is_empty());
    let saved = t.ctx.load(s(&j, "id")).unwrap();
    assert_eq!(saved["status"], "succeeded");
    assert_eq!(saved["steps"], 2);
    assert_eq!(saved["last_message"], "DONE");
    assert_eq!(
        fs::read_to_string(t.ctx.job_dir(s(&j, "id")).join("last_message.md")).unwrap(),
        "DONE"
    );
    assert!(
        t.rt.diagnostics
            .lock()
            .unwrap()
            .iter()
            .any(|s| s.contains("進捗"))
    );
}

#[test]
fn mandatory_resume_identity_and_terminal_saves_are_never_reported_as_success() {
    for field in ["thread_id", "status"] {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |_, candidate| {
            if (field == "thread_id" && truth(&candidate[field]))
                || (field == "status" && s(candidate, "status") == "succeeded")
            {
                Err(io::Error::from_raw_os_error(5))
            } else {
                Ok(())
            }
        }));
        let result = t.ctx.supervise(s(&j, "id"));
        assert!(result.is_err(), "{field} must not produce false success");
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_ne!(saved["status"], "succeeded");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        if field == "status" {
            assert_eq!(
                fs::read_to_string(t.ctx.job_dir(s(&j, "id")).join("last_message.md")).unwrap(),
                "DONE"
            );
            assert!(
                t.rt.diagnostics
                    .lock()
                    .unwrap()
                    .iter()
                    .any(|s| s.contains("終了状態を保存できません"))
            );
        }
    }
}

#[test]
fn deferred_progress_cannot_undo_stop_terminal_or_owner_saved_during_backoff() {
    for state in ["cancelled", "succeeded", "running"] {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
        let faulted = Arc::new(AtomicBool::new(false));
        let fault = faulted.clone();
        *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |_, candidate| {
            if candidate["activity_detail"] == "fixture work" && !fault.swap(true, Ordering::SeqCst)
            {
                return Err(io::Error::from_raw_os_error(32));
            }
            Ok(())
        }));
        t.rt.procs.lock().unwrap().insert(321, 456);
        t.rt.child_alive.store(true, Ordering::SeqCst);
        let alive = t.rt.child_alive.clone();
        let output = [
        json!({"type":"thread.started","thread_id":"01a0e883-7628-7121-9540-6780fd4cd6f4"}),
        json!({"type":"turn.started"}),
        json!({"type":"item.started","item":{"type":"command_execution","command":"fixture work"}}),
    ].into_iter().map(|ev| format!("{ev}\n")).collect::<String>();
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            Box::new(FakeChild {
                alive: alive.clone(),
                out: Some(Box::new(std::io::Cursor::new(output.clone().into_bytes()))),
            })
        }));
        let ticks = Arc::new(AtomicUsize::new(0));
        let clock = ticks.clone();
        let alive = t.rt.child_alive.clone();
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds == 0.5 && faulted.load(Ordering::SeqCst) {
                let tick = clock.fetch_add(1, Ordering::SeqCst) + 1;
                if tick == 6 {
                    let _state = FileLock::try_acquire(&path.with_file_name("state.lock")).unwrap();
                    let mut fresh: Value =
                        serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
                    fresh["status"] = json!(state);
                    fresh["preserved_control_record"] = json!("fixture controller update");
                    if state == "cancelled" {
                        fresh["cancel_requested"] = json!(true);
                        fresh["cancel_reason"] = json!("fixture owner stop");
                    } else if state == "running" {
                        fresh["pid"] = json!(555);
                        fresh["pid_created"] = json!(777);
                    }
                    fs::write(&path, pretty(&fresh)).unwrap();
                } else if tick >= 7 {
                    alive.store(false, Ordering::SeqCst);
                }
            }
        }));
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert!(ticks.load(Ordering::SeqCst) >= 6);
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], state);
        assert_eq!(
            saved["preserved_control_record"],
            "fixture controller update"
        );
        if state == "cancelled" {
            assert_eq!(saved["cancel_requested"], true);
            assert_eq!(saved["cancel_reason"], "fixture owner stop");
        } else if state == "running" {
            assert_eq!(saved["pid"], 555);
            assert_eq!(saved["pid_created"], 777);
        }
        assert_eq!(t.rt.spawns.load(Ordering::SeqCst), 1);
        assert!(!t.rt.kills.lock().unwrap().contains(&555));
    }
}

#[test]
fn live_result_snapshot_failure_does_not_turn_a_completed_child_into_setup_failure() {
    let t = Test::new();
    let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
    let failures = Arc::new(AtomicUsize::new(0));
    let count = failures.clone();
    *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |_, candidate| {
        if candidate["last_message"] == "DONE" && candidate["status"] == "running" {
            count.fetch_add(1, Ordering::SeqCst);
            return Err(io::Error::from_raw_os_error(32));
        }
        Ok(())
    }));
    t.ctx.supervise(s(&j, "id")).unwrap();
    assert!(failures.load(Ordering::SeqCst) > 0);
    let saved = t.ctx.load(s(&j, "id")).unwrap();
    assert_eq!(saved["status"], "succeeded");
    assert_eq!(saved["last_message"], "DONE");
    assert_eq!(saved["turn_completed"], true);
    assert_eq!(saved["exit_code"], 0);
    assert!(t.rt.kills.lock().unwrap().is_empty());
}
