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
    drop(denied);
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
