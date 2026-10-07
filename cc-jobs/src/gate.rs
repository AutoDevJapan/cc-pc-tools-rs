//! Limit concurrent Cargo build-like commands across codex-jobs supervisors on Windows.
//!
//! At most CODEX_JOBS_CARGO_SLOTS (default 3) builds run at once; each uses CARGO_BUILD_JOBS
//! (set by codex_jobs to half the logical cores), so the machine stays busy without thrashing.
use crate::*;
use std::{collections::BTreeMap, io, path::Path, process::Stdio};
pub const UNLOCKED: &[&str] = &[
    "-V",
    "--version",
    "--help",
    "help",
    "version",
    "fmt",
    "metadata",
    "locate-project",
];
/// Windows CPython's sys.exit converts its argument through a signed C long.
/// A child DWORD above LONG_MAX becomes -1 at this CLI boundary; keep the stored
/// process status unsigned, but preserve the original cargo_gate.py exit code.
pub fn python_cli_exit(code: i64) -> i32 {
    i32::try_from(code).unwrap_or(-1)
}
pub fn first_command(args: &[String]) -> &str {
    args.iter()
        .find(|s| !s.starts_with('-'))
        .map(String::as_str)
        .unwrap_or("")
}
pub fn needs_lock(args: &[String], env: &BTreeMap<String, String>) -> bool {
    let command = args
        .first()
        .filter(|s| UNLOCKED.contains(&s.as_str()))
        .map(String::as_str)
        .unwrap_or(first_command(args));
    !UNLOCKED.contains(&command)
        && env
            .get("CODEX_JOBS_CARGO_LOCK_HELD")
            .is_none_or(|v| v != "1")
}
pub fn with_slot<T>(
    dir: &Path,
    slots: usize,
    mut sleep: impl FnMut(f64),
    body: impl FnOnce() -> io::Result<T>,
) -> io::Result<T> {
    let held = loop {
        let mut held = None;
        for i in 0..slots.max(1) {
            match FileLock::try_acquire(&dir.join(format!("cargo-build.lock.{i}"))) {
                Ok(f) => {
                    held = Some(f);
                    break;
                }
                Err(e) if matches!(e.raw_os_error(), Some(33) | Some(32)) => (),
                Err(e) => return Err(e),
            }
        }
        if let Some(f) = held {
            break f;
        }
        sleep(0.5);
    };
    let result = body();
    drop(held);
    result
}
pub fn run(args: &[String], dir: &Path, env: &BTreeMap<String, String>) -> io::Result<i64> {
    let Some(cargo) = env
        .get("CODEX_JOBS_REAL_CARGO")
        .filter(|s| Path::new(s).is_file())
    else {
        eprintln!("cargo gate: CODEX_JOBS_REAL_CARGO is missing");
        return Ok(127);
    };
    let body = || {
        let execute = || {
            let mut c = hidden_command(cargo);
            c.args(args)
                .stdin(Stdio::inherit())
                .stdout(Stdio::inherit())
                .stderr(Stdio::inherit());
            if needs_lock(args, env) {
                c.env("CODEX_JOBS_CARGO_LOCK_HELD", "1");
            }
            if crate::cache::guard_needed(args, env) {
                c.env("CODEX_JOBS_CACHE_LOCK_HELD", "1");
            }
            Ok(c.status()?.code().map(|c| c as u32 as i64).unwrap_or(1))
        };
        if crate::cache::guard_needed(args, env) {
            crate::cache::guarded(env, args, execute)
        } else {
            execute()
        }
    };
    if !needs_lock(args, env) {
        return body();
    }
    let slots = env
        .get("CODEX_JOBS_CARGO_SLOTS")
        .filter(|s| !s.is_empty())
        .map(|s| s.parse::<isize>())
        .transpose()
        .map_err(|_| {
            io::Error::new(
                io::ErrorKind::InvalidInput,
                "invalid CODEX_JOBS_CARGO_SLOTS",
            )
        })?
        .unwrap_or(3)
        .max(1) as usize;
    with_slot(
        dir,
        slots,
        |s| std::thread::sleep(std::time::Duration::from_secs_f64(s)),
        body,
    )
}
