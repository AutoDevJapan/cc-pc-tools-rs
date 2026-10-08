//! Queue-lock holder diagnostics: a timed-out waiter must learn who holds the
//! lock (pid and operation only) so a long holder can be proven, not guessed.
#![cfg(windows)]
use cc_jobs::FileLock;
use std::time::Duration;

fn temp_lock(name: &str) -> std::path::PathBuf {
    let dir =
        std::env::temp_dir().join(format!("cc-jobs-queue-lock-{name}-{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    dir.join(".queue.lock")
}

#[test]
fn timeout_names_holder_pid_and_operation_and_release_clears_it() {
    let path = temp_lock("owner");
    let mut holder = FileLock::acquire_queue(&path).unwrap();
    holder.set_owner(&path, "maintain_yields:job-1");
    let error = FileLock::acquire_queue_timeout(&path, Duration::from_millis(200))
        .err()
        .expect("second exclusive holder must time out");
    assert_eq!(error.kind(), std::io::ErrorKind::TimedOut);
    let message = error.to_string();
    assert!(message.contains("maintain_yields:job-1"), "{message}");
    assert!(
        message.contains(&format!("\"pid\":{}", std::process::id())),
        "{message}"
    );
    drop(holder);
    assert!(!path.with_file_name(".queue.lock.owner").exists());
    drop(FileLock::acquire_queue_timeout(&path, Duration::from_millis(200)).unwrap());
    std::fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn waiter_without_recorded_holder_gets_plain_timeout() {
    let path = temp_lock("plain");
    let holder = FileLock::acquire_queue(&path).unwrap();
    let error = FileLock::acquire_queue_timeout(&path, Duration::from_millis(100))
        .err()
        .unwrap();
    assert!(!error.to_string().contains("last recorded holder"));
    drop(holder);
    std::fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
