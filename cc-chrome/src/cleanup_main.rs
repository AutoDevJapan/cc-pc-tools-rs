//! Run independently of Discord's network-bound tick. No console or child processes.
#![cfg_attr(windows, windows_subsystem = "windows")]
use std::{
    fs::{File, OpenOptions},
    io::{self, Write},
    path::Path,
};

fn open_log(path: &Path) -> io::Result<File> {
    // Windows requires read access when locking a file opened for append.
    let log = OpenOptions::new()
        .create(true)
        .read(true)
        .append(true)
        .open(path)?;
    log.try_lock()?;
    Ok(log)
}

fn run() -> io::Result<bool> {
    let jobs = Path::new("D:/CC/Tools/codex-jobs/jobs");
    // Require access to the result log before closing anything.
    let mut log = open_log(Path::new("D:/CC/Tools/owner-chrome/cleanup.log"))?;
    let result = cc_chrome::sweep(jobs, None, false);
    let (line, success) = match result {
        Ok(s) => (
            format!(
                "chrome-tabs: closed={} gone={} kept={} failed={} deferred={}",
                s.closed, s.gone, s.kept, s.failed, s.deferred
            ),
            s.failed == 0 && !s.deferred,
        ),
        // Error kinds are enough for diagnosis here; don't copy external RPC text into logs.
        Err(e) => (
            format!("chrome-tabs: sweep failed kind={:?}", e.kind()),
            false,
        ),
    };
    writeln!(log, "{} {line}", chrono::Local::now().to_rfc3339())?;
    log.sync_data()?;
    #[cfg(windows)]
    cc_chrome::lifecycle::notices(jobs)?;
    Ok(success)
}

fn main() {
    if !matches!(run(), Ok(true)) {
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scheduled_log_can_be_locked_and_appended_on_windows() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join("cleanup.log");
        std::fs::write(&path, "earlier result\n").unwrap();
        let mut log = open_log(&path).unwrap();
        writeln!(log, "next result").unwrap();
        log.sync_data().unwrap();
        // A second execution must not enter until this one's log handle is dropped.
        assert!(open_log(&path).is_err());
        drop(log);
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            "earlier result\nnext result\n"
        );
        drop(open_log(&path).unwrap());
    }
}
