//! Limit concurrent Cargo build-like commands across codex-jobs supervisors on Windows.
//!
//! A build first locks its effective target directory, then takes one of
//! dynamically admitted machine slots, so a build waiting for a busy
//! target never holds a slot. The logical cores are split across active builds
//! (CARGO_BUILD_JOBS and explicit job counts are clamped), and a build without an
//! explicit target gets a stable per-workspace directory shared with sccache.
use crate::*;
pub mod admission;
use std::{
    collections::BTreeMap,
    io,
    path::{Path, PathBuf},
    process::Stdio,
};
pub const UNLOCKED: &[&str] = &[
    "-V",
    "--version",
    "--help",
    "help",
    "version",
    "fmt",
    "metadata",
    "locate-project",
    "tree",
    "add",
    "remove",
    "update",
    "fetch",
    "search",
    "info",
    "vendor",
    "new",
    "init",
    "generate-lockfile",
];
pub const SCCACHE: &str = r"D:\CC\Tools\sccache\sccache.exe";
/// Target key locked by the outer gate. Nested gates reuse the outer slot.
pub const HELD_TARGET: &str = "CODEX_JOBS_CARGO_TARGET_HELD";
/// CARGO_TARGET_DIR value chosen by a gate rather than the user; nested gates
/// recompute it for their own workspace.
pub const INJECTED_TARGET: &str = "CODEX_JOBS_CARGO_TARGET_INJECTED";
/// Explicit user target inherited by nested gates without entering Rust cache keys.
pub const FORWARDED_TARGET: &str = "CODEX_JOBS_CARGO_TARGET_OVERRIDE";
const LOCK_HELD: &str = "CODEX_JOBS_CARGO_LOCK_HELD";
const HELD_JOBS: &str = "CODEX_JOBS_CARGO_JOBS_LIMIT";
const HELD_TESTS: &str = "CODEX_JOBS_CARGO_TEST_LIMIT";
const SLOT_WAIT: &str = "cargoの共有ビルド枠待ち";
const TARGET_WAIT: &str = "cargoの同じビルド先の使用終了待ち";
/// Windows CPython's sys.exit converts its argument through a signed C long.
/// A child DWORD above LONG_MAX becomes -1 at this CLI boundary; keep the stored
/// process status unsigned, but preserve the original cargo_gate.py exit code.
pub fn python_cli_exit(code: i64) -> i32 {
    i32::try_from(code).unwrap_or(-1)
}
pub fn first_command(args: &[String]) -> &str {
    let mut rest = args.iter();
    while let Some(arg) = rest.next() {
        if arg == "--" {
            break;
        }
        if ["--manifest-path", "--config", "-Z", "-C", "--color"].contains(&arg.as_str()) {
            rest.next();
        } else if !arg.starts_with(['-', '+']) {
            return arg;
        }
    }
    ""
}
pub fn build_like(args: &[String]) -> bool {
    if first_command(args) == "nextest"
        && nextest_command(args)
            .is_some_and(|s| ["show-config", "self", "store", "help"].contains(&s))
    {
        return false;
    }
    if args.is_empty()
        || args
            .iter()
            .take_while(|arg| *arg != "--")
            .any(|arg| arg == "--list")
    {
        return false;
    }
    if args
        .iter()
        .take_while(|arg| *arg != "--")
        .any(|arg| matches!(arg.as_str(), "--version" | "--help" | "-V" | "-h"))
    {
        return false;
    }
    let command = args
        .first()
        .filter(|s| UNLOCKED.contains(&s.as_str()))
        .map(String::as_str)
        .unwrap_or(first_command(args));
    !UNLOCKED.contains(&command)
}
fn nextest_command(args: &[String]) -> Option<&str> {
    args.iter()
        .position(|s| s == "nextest")
        .and_then(|i| args.get(i + 1))
        .map(String::as_str)
}
pub fn needs_lock(args: &[String], env: &BTreeMap<String, String>) -> bool {
    build_like(args) && env.get(LOCK_HELD).is_none_or(|v| v != "1")
}
/// New commands share the CPU budget according to the actual active count.
/// Running Cargo jobservers keep their initial budget until the command ends.
pub fn jobs_limit(cores: usize, active: usize) -> usize {
    (cores / active.max(1)).clamp(1, 10)
}
fn clamp(value: &str, limit: usize) -> usize {
    value
        .parse::<usize>()
        .ok()
        .filter(|n| *n > 0)
        .map_or(limit, |n| n.min(limit))
}
/// Clamp explicit build job counts before `--`: `-j`/`--jobs`, or `--build-jobs`
/// for cargo-nextest (whose `-j` is the test thread count, not a build setting).
pub fn clamp_jobs(args: &[String], limit: usize) -> Vec<String> {
    clamp_parallel_args(args, limit, limit).0
}
fn clamp_parallel_args(
    args: &[String],
    build_limit: usize,
    test_limit: usize,
) -> (Vec<String>, usize, usize) {
    let nextest = first_command(args) == "nextest";
    let flags: &[&str] = if nextest {
        &["--build-jobs", "--test-threads", "--jobs", "-j"]
    } else {
        &["--jobs", "-j"]
    };
    let mut build = build_limit;
    let mut tests = test_limit;
    let mut out = Vec::with_capacity(args.len());
    let mut rest = args.iter();
    while let Some(arg) = rest.next() {
        if arg == "--" {
            out.push(arg.clone());
            out.extend(rest.by_ref().cloned());
            break;
        }
        // A flag-looking option value is data, not another parallelism flag.
        if [
            "--manifest-path",
            "--config",
            "-Z",
            "-C",
            "--color",
            "--target",
            "--target-dir",
            "--package",
            "-p",
            "--exclude",
            "--features",
            "-F",
            "--message-format",
            "--profile",
            "--bin",
            "--example",
            "--test",
            "--bench",
            "--filter-expr",
            "-E",
            "--config-file",
            "--status-level",
            "--final-status-level",
            "--failure-output",
            "--success-output",
            "--retries",
            "--run-ignored",
            "--partition",
            "--archive-file",
            "--archive-format",
            "--workspace-remap",
            "--extract-to",
            "--stress-count",
            "--stress-duration",
        ]
        .contains(&arg.as_str())
        {
            out.push(arg.clone());
            out.extend(rest.next().cloned());
            continue;
        }
        let mut update = |flag: &str, value: &str| {
            if nextest && flag != "--build-jobs" {
                tests = clamp(value, test_limit);
                tests
            } else {
                build = clamp(value, build_limit);
                build
            }
        };
        if flags.contains(&arg.as_str()) {
            out.push(arg.clone());
            out.extend(rest.next().map(|value| update(arg, value).to_string()));
            continue;
        }
        let inline = flags.iter().find_map(|flag| {
            let value = arg.strip_prefix(flag)?;
            if flag.starts_with("--") {
                Some((flag, "=", value.strip_prefix('=')?))
            } else {
                Some((flag, "", value.strip_prefix('=').unwrap_or(value)))
            }
        });
        out.push(match inline {
            Some((flag, sep, value)) => format!("{flag}{sep}{}", update(flag, value)),
            None => arg.clone(),
        });
    }
    (out, build, tests)
}
/// Do not invent arguments for custom harnesses. libtest honors the bounded
/// RUST_TEST_THREADS environment when no explicit thread argument is supplied.
fn clamp_harness_threads(args: &[String], limit: usize) -> (Vec<String>, usize) {
    let mut out = args.to_vec();
    let Some(start) = out.iter().position(|s| s == "--").map(|i| i + 1) else {
        return (out, limit);
    };
    let mut effective = limit;
    let mut i = start;
    while i < out.len() && out[i] != "--" {
        if [
            "--skip",
            "--logfile",
            "--format",
            "--color",
            "--shuffle-seed",
        ]
        .contains(&out[i].as_str())
        {
            i += 2;
        } else if out[i] == "--test-threads" {
            if let Some(value) = out.get_mut(i + 1)
                && value.parse::<usize>().is_ok_and(|n| n > 0)
            {
                effective = clamp(value, limit);
                *value = effective.to_string();
            }
            i += 2;
        } else {
            if let Some(value) = out[i].strip_prefix("--test-threads=")
                && value.parse::<usize>().is_ok_and(|n| n > 0)
            {
                effective = clamp(value, limit);
                out[i] = format!("--test-threads={effective}");
            }
            i += 1;
        }
    }
    (out, effective)
}
fn var<'a>(env: &'a BTreeMap<String, String>, name: &str) -> Option<&'a String> {
    env.iter()
        .find(|(k, _)| k.eq_ignore_ascii_case(name))
        .map(|(_, v)| v)
}
fn take_var(env: &mut BTreeMap<String, String>, name: &str) -> Option<String> {
    let mut value = None;
    env.retain(|k, v| {
        let keep = !k.eq_ignore_ascii_case(name);
        if !keep {
            value = Some(v.clone());
        }
        keep
    });
    value
}
pub fn explicit_target(args: &[String]) -> Option<&str> {
    let mut found = None;
    for (i, arg) in args.iter().enumerate() {
        if arg == "--" {
            break;
        }
        if arg == "--target-dir" {
            found = args.get(i + 1).map(String::as_str);
        }
        if let Some(v) = arg.strip_prefix("--target-dir=") {
            found = Some(v);
        }
    }
    found
}
fn fnv(text: &str) -> u64 {
    text.bytes().fold(0xcbf2_9ce4_8422_2325, |h, b| {
        (h ^ u64::from(b)).wrapping_mul(0x0100_0000_01b3)
    })
}
fn key(path: &Path) -> String {
    crate::cache::resolved(path).unwrap_or_else(|| path.to_string_lossy().to_lowercase())
}
/// Stable per-workspace default, so worktrees never share (or fight over) a target.
pub fn default_target(workspace_root: &Path) -> PathBuf {
    // Cargo's ordinary root/target is already private to each worktree.
    // Keeping it also honors a configuration explicitly selecting that path.
    PathBuf::from(key(workspace_root)).join("target")
}
pub fn target_lock(dir: &Path, target_key: &str) -> PathBuf {
    dir.join(format!("cargo-target-{:016x}.lock", fnv(target_key)))
}
/// Decide the effective target directory and rewrite `env` for Cargo.
/// Explicit choices (argument, user environment, Cargo configuration) are kept.
/// The legacy shared cache inherited from older job environments and a target
/// injected by an outer gate are replaced by this workspace's own directory.
/// `metadata` returns (workspace_root, target_directory) for the given environment.
pub fn prepare_target(
    args: &[String],
    env: &mut BTreeMap<String, String>,
    metadata: impl FnOnce(&BTreeMap<String, String>) -> Option<(PathBuf, PathBuf)>,
) -> Option<PathBuf> {
    if let Some(dir) = explicit_target(args) {
        return std::path::absolute(dir).ok();
    }
    let inherited = var(env, "CARGO_TARGET_DIR")
        .filter(|v| !v.is_empty())
        .cloned();
    let stale = inherited.as_ref().is_some_and(|v| {
        crate::cache::resolved(v) == crate::cache::resolved(crate::cache::SHARED)
            || var(env, INJECTED_TARGET) == Some(v)
    });
    if let Some(dir) = inherited.as_ref().filter(|_| !stale) {
        return std::path::absolute(dir).ok();
    }
    if inherited.is_none()
        && var(env, LOCK_HELD).is_some_and(|v| v == "1")
        && let Some(dir) = var(env, FORWARDED_TARGET).filter(|v| !v.is_empty())
    {
        return std::path::absolute(dir).ok();
    }
    let mut fresh = env.clone();
    take_var(&mut fresh, "CARGO_TARGET_DIR");
    if let Some(dir) = var(&fresh, "CARGO_BUILD_TARGET_DIR").filter(|v| !v.is_empty()) {
        let dir = std::path::absolute(dir).ok();
        *env = fresh;
        return dir;
    }
    let Some((root, target)) = metadata(&fresh) else {
        // Unknown layout: keep what was inherited; the legacy cache stays guarded.
        return inherited.and_then(|v| std::path::absolute(v).ok());
    };
    *env = fresh;
    if key(&target) != key(&root.join("target")) {
        return Some(target); // build.target-dir from Cargo configuration
    }
    let dir = default_target(&root);
    let value = dir.to_string_lossy().into_owned();
    env.insert("CARGO_TARGET_DIR".into(), value.clone());
    env.insert(INJECTED_TARGET.into(), value);
    Some(dir)
}
/// Cargo's target environment variables are hashed by sccache for every Rust
/// dependency. Pass the already resolved target as a Cargo option instead, so
/// registry crates can hit across worktrees without changing their rustc inputs.
pub fn target_arguments(
    args: &[String],
    env: &mut BTreeMap<String, String>,
    target: &Path,
) -> Vec<String> {
    // External Cargo subcommands need not accept Cargo's --target-dir option.
    if first_command(args) == "nextest"
        && !nextest_command(args).is_some_and(|s| ["run", "list", "archive", "bench"].contains(&s))
    {
        return args.to_vec();
    }
    if ![
        "build", "check", "test", "bench", "rustc", "rustdoc", "run", "clippy", "clean", "nextest",
        "doc", "fix", "install",
    ]
    .contains(&first_command(args))
    {
        return args.to_vec();
    }
    // Keep the preexisting inheritance of an explicit user environment target.
    // Automatically chosen workspace targets must still be recomputed by a
    // nested gate in another workspace.
    let user_target = explicit_target(args).is_some()
        || var(env, "CARGO_TARGET_DIR").is_some_and(|v| {
            var(env, INJECTED_TARGET) != Some(v)
                && crate::cache::resolved(v) != crate::cache::resolved(crate::cache::SHARED)
        })
        || var(env, "CARGO_BUILD_TARGET_DIR").is_some();
    if user_target {
        env.insert(
            FORWARDED_TARGET.into(),
            target.to_string_lossy().into_owned(),
        );
    }
    take_var(env, "CARGO_TARGET_DIR");
    take_var(env, "CARGO_BUILD_TARGET_DIR");
    take_var(env, INJECTED_TARGET);
    let mut args = args.to_vec();
    if explicit_target(&args).is_none() {
        let end = args.iter().position(|a| a == "--").unwrap_or(args.len());
        args.splice(
            end..end,
            ["--target-dir".into(), target.to_string_lossy().into_owned()],
        );
    }
    args
}
/// Global options that change which workspace or configuration Cargo reads.
fn metadata_args(args: &[String]) -> Vec<String> {
    let mut out: Vec<String> = args
        .first()
        .filter(|a| a.starts_with('+'))
        .cloned()
        .into_iter()
        .collect();
    out.extend(["metadata", "--format-version", "1", "--no-deps"].map(String::from));
    let mut rest = args.iter();
    while let Some(arg) = rest.next() {
        if arg == "--" {
            break;
        }
        if ["--locked", "--frozen", "--offline"].contains(&arg.as_str()) {
            out.push(arg.clone());
        } else if ["--manifest-path", "--config", "-Z", "-C"].contains(&arg.as_str()) {
            out.push(arg.clone());
            out.extend(rest.next().cloned());
        } else if ["--manifest-path=", "--config=", "-Z"]
            .iter()
            .any(|p| arg.starts_with(p))
        {
            out.push(arg.clone());
        }
    }
    out
}
/// Cargo command with `env` applied on top of the inherited `base` environment.
fn command(
    cargo: &str,
    base: &BTreeMap<String, String>,
    env: &BTreeMap<String, String>,
) -> std::process::Command {
    let mut c = hidden_command(cargo);
    for k in base.keys().filter(|k| !env.contains_key(*k)) {
        c.env_remove(k);
    }
    c.envs(env.iter().filter(|(k, v)| base.get(*k) != Some(*v)));
    c
}
fn metadata(
    cargo: &str,
    args: &[String],
    base: &BTreeMap<String, String>,
    env: &BTreeMap<String, String>,
) -> Option<(PathBuf, PathBuf)> {
    // The real Cargo is called directly, so this never re-enters the gate.
    let out = command(cargo, base, env)
        .args(metadata_args(args))
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let v: Value = serde_json::from_slice(&out.stdout).ok()?;
    Some((
        PathBuf::from(v["workspace_root"].as_str()?),
        PathBuf::from(v["target_directory"].as_str()?),
    ))
}
fn acquire_any(paths: &[PathBuf], mut sleep: impl FnMut(f64)) -> io::Result<FileLock> {
    loop {
        for path in paths {
            match FileLock::try_acquire(path) {
                Ok(f) => return Ok(f),
                Err(e) if matches!(e.raw_os_error(), Some(33) | Some(32)) => (),
                Err(e) => return Err(e),
            }
        }
        sleep(0.5);
    }
}
fn slot_paths(dir: &Path, slots: usize) -> Vec<PathBuf> {
    (0..slots.max(1))
        .map(|i| dir.join(format!("cargo-build.lock.{i}")))
        .collect()
}
pub fn with_slot<T>(
    dir: &Path,
    slots: usize,
    sleep: impl FnMut(f64),
    body: impl FnOnce() -> io::Result<T>,
) -> io::Result<T> {
    let held = acquire_any(&slot_paths(dir, slots), sleep)?;
    let result = body();
    drop(held);
    result
}

fn wait_for(
    paths: &[PathBuf],
    env: &BTreeMap<String, String>,
    reason: &str,
) -> io::Result<FileLock> {
    let mut wait = None;
    let lock = acquire_any(paths, |s| {
        if wait.is_none() {
            wait = crate::queue_status::ResourceWait::new(env, reason);
        }
        std::thread::sleep(std::time::Duration::from_secs_f64(s));
    });
    // Markers are ignored after this process exits. Clear the marker before
    // Cargo starts, so compilation is never reported as waiting.
    drop(wait);
    lock
}
pub fn run(args: &[String], dir: &Path, env: &BTreeMap<String, String>) -> io::Result<i64> {
    run_with_admitter(args, dir, env, admission::wait)
}

/// Inject only the machine measurement for a process fixture; production uses `run`.
pub fn run_with_admitter(
    args: &[String],
    dir: &Path,
    env: &BTreeMap<String, String>,
    mut admit: impl FnMut(&Path, &BTreeMap<String, String>, usize) -> io::Result<admission::Permit>,
) -> io::Result<i64> {
    #[cfg(not(windows))]
    let target_checks = std::cell::RefCell::new(crate::cache::SharedTargetChecks::default());
    #[cfg(not(windows))]
    target_checks.borrow_mut().check_request(args, env)?;
    let Some(cargo) = env
        .get("CODEX_JOBS_REAL_CARGO")
        .filter(|s| Path::new(s).is_file())
    else {
        eprintln!("cargo gate: CODEX_JOBS_REAL_CARGO is missing");
        return Ok(127);
    };
    let execute = |args: &[String], child: &BTreeMap<String, String>| {
        #[cfg(not(windows))]
        target_checks.borrow_mut().check_request(args, child)?;
        // Decided from the migrated environment: only an explicit legacy cache is guarded.
        let guard = crate::cache::guard_needed(args, child);
        let run = || -> io::Result<i64> {
            let _active = build_like(args)
                .then(|| crate::queue_status::ResourceWait::active(child))
                .flatten();
            let mut c = command(cargo, env, child);
            c.args(args)
                .stdin(Stdio::inherit())
                .stdout(Stdio::inherit())
                .stderr(Stdio::inherit());
            if guard {
                c.env("CODEX_JOBS_CACHE_LOCK_HELD", "1");
            }
            #[cfg(not(windows))]
            target_checks.borrow_mut().check_request(args, child)?;
            Ok(c.status()?.code().map(|c| c as u32 as i64).unwrap_or(1))
        };
        if guard {
            crate::cache::guarded(child, args, run)
        } else {
            run()
        }
    };
    if !build_like(args) {
        return execute(args, env);
    }
    let cores = std::thread::available_parallelism().map_or(1, |n| n.get());
    let mut child = env.clone();
    crate::cache::build_environment(&mut child, Path::new(SCCACHE));
    let nested = env.get(LOCK_HELD).is_some_and(|v| v == "1");
    let held = env.get(HELD_TARGET).map(String::as_str);
    if nested && held.is_none() {
        // The outer gate predates target locks; it already owns a slot.
        let limit = nested_jobs_limit(&child, cores);
        let cfg = admission::Config::read(dir)?;
        let tests = nested_test_limit(&child, cores).min(cfg.test_threads);
        let args = apply_limits(args, &mut child, limit.min(cfg.jobs_per_build), tests);
        return execute(&args, &child);
    }
    // Only a real Cargo is asked for metadata; test doubles and other commands
    // keep the inherited target unchanged.
    let real = Path::new(cargo)
        .file_stem()
        .is_some_and(|s| s.eq_ignore_ascii_case("cargo"));
    let target_dir = prepare_target(args, &mut child, |fresh| {
        real.then(|| metadata(cargo, args, env, fresh)).flatten()
    });
    #[cfg(not(windows))]
    if let Some(path) = target_dir.as_deref() {
        target_checks.borrow_mut().check_path(path)?;
    }
    #[cfg(not(windows))]
    target_checks.borrow_mut().recheck()?;
    let target = target_dir.as_ref().map(|p| {
        // Create first, so the key is canonical before and after Cargo creates it.
        let _ = std::fs::create_dir_all(p);
        key(p)
    });
    #[cfg(not(windows))]
    target_checks.borrow_mut().recheck()?;
    if target.as_deref() == crate::cache::resolved(crate::cache::SHARED).as_deref() {
        child.insert("CARGO_TARGET_DIR".into(), crate::cache::SHARED.into());
    }
    child.insert(LOCK_HELD.into(), "1".into());
    child.insert(HELD_TARGET.into(), target.clone().unwrap_or_default());
    // Lock the target before a slot, so a build waiting for its target never holds
    // a slot. Nested Cargo already runs inside an ancestor's slot (taking another
    // could deadlock), but an independent target must still be locked.
    let target_guard = target.filter(|k| !nested || held != Some(k.as_str()))
        .map(|k| {
            #[cfg(not(windows))]
            target_checks.borrow_mut().recheck()?;
            let path = target_lock(dir, &k);
            if nested {
                FileLock::try_acquire(&path).map_err(|e| {
                    if matches!(e.raw_os_error(), Some(33) | Some(32)) {
                        io::Error::new(io::ErrorKind::WouldBlock,
                            "nested Cargo cannot wait for another target while its parent holds a build slot; run it after the parent command")
                    } else { e }
                })
            } else { wait_for(&[path], env, TARGET_WAIT) }
        }).transpose()?;
    #[cfg(not(windows))]
    target_checks.borrow_mut().recheck()?;
    let slot = (!nested).then(|| admit(dir, env, cores)).transpose()?;
    let (limit, tests) = match slot.as_ref() {
        Some(p) => (p.jobs, p.test_threads),
        None => {
            let cfg = admission::Config::read(dir)?;
            (
                nested_jobs_limit(&child, cores).min(cfg.jobs_per_build),
                nested_test_limit(&child, cores).min(cfg.test_threads),
            )
        }
    };
    let args = apply_limits(args, &mut child, limit, tests);
    let args = if real {
        target_dir
            .as_ref()
            .map_or_else(|| args.clone(), |p| target_arguments(&args, &mut child, p))
    } else {
        args
    };
    let result = execute(&args, &child);
    drop((slot, target_guard));
    result
}

#[cfg(test)]
fn apply_jobs(args: &[String], child: &mut BTreeMap<String, String>, limit: usize) -> Vec<String> {
    apply_limits(args, child, limit, limit)
}
fn apply_limits(
    args: &[String],
    child: &mut BTreeMap<String, String>,
    build_limit: usize,
    test_limit: usize,
) -> Vec<String> {
    // Resolve all sources once before rewriting arguments or publishing child
    // limits. Neither an absent CLI flag nor a final env write can raise these.
    let build_cap =
        take_var(child, "CARGO_BUILD_JOBS").map_or(build_limit, |v| clamp(&v, build_limit));
    let rust_cap =
        take_var(child, "RUST_TEST_THREADS").map_or(test_limit, |v| clamp(&v, test_limit));
    let nextest = first_command(args) == "nextest";
    let test_cap = if nextest {
        take_var(child, "NEXTEST_TEST_THREADS").map_or(rust_cap, |v| clamp(&v, rust_cap))
    } else {
        rust_cap
    };
    let (out, build, tests) = clamp_parallel_args(args, build_cap, test_cap);
    let (out, tests) = if first_command(args) == "test" {
        clamp_harness_threads(&out, tests)
    } else {
        (out, tests)
    };
    child.insert(HELD_JOBS.into(), build.to_string());
    child.insert("CARGO_BUILD_JOBS".into(), build.to_string());
    child.insert(HELD_TESTS.into(), tests.to_string());
    child.insert("RUST_TEST_THREADS".into(), tests.to_string());
    if nextest {
        child.insert("NEXTEST_TEST_THREADS".into(), tests.to_string());
    }
    out
}

fn nested_test_limit(child: &BTreeMap<String, String>, cores: usize) -> usize {
    let fallback = nested_jobs_limit(child, cores);
    var(child, HELD_TESTS).map_or(fallback, |v| clamp(v, jobs_limit(cores, 1)))
}

fn nested_jobs_limit(child: &BTreeMap<String, String>, cores: usize) -> usize {
    let fallback = jobs_limit(cores, 1);
    var(child, HELD_JOBS)
        .or_else(|| var(child, "CARGO_BUILD_JOBS"))
        .map_or(fallback, |v| clamp(v, fallback))
}

#[cfg(test)]
mod tests {
    use super::*;
    fn args(items: &[&str]) -> Vec<String> {
        items.iter().map(|s| (*s).into()).collect()
    }
    #[test]
    fn nextest_build_and_test_limits_are_independent() {
        let mut env = BTreeMap::new();
        assert_eq!(
            apply_limits(
                &args(&[
                    "+stable",
                    "nextest",
                    "run",
                    "--build-jobs=32",
                    "-j32",
                    "--test-threads",
                    "32"
                ]),
                &mut env,
                8,
                3
            ),
            args(&[
                "+stable",
                "nextest",
                "run",
                "--build-jobs=8",
                "-j3",
                "--test-threads",
                "3"
            ])
        );
        assert_eq!(env["CARGO_BUILD_JOBS"], "8");
        assert_eq!(env["NEXTEST_TEST_THREADS"], "3");
        assert_eq!(env["RUST_TEST_THREADS"], "3");
        assert_eq!(nested_test_limit(&env, 32), 3);
        assert_eq!(
            apply_limits(
                &args(&["nextest", "run", "--build-jobs", "1", "--jobs=1"]),
                &mut env,
                8,
                3
            ),
            args(&["nextest", "run", "--build-jobs", "1", "--jobs=1"])
        );
    }
    #[test]
    fn cargo_test_clamps_harness_flags_and_preserves_serial_tests() {
        let mut env = BTreeMap::new();
        assert_eq!(
            apply_limits(
                &args(&[
                    "test",
                    "-j32",
                    "--",
                    "filter",
                    "--test-threads=32",
                    "--nocapture"
                ]),
                &mut env,
                8,
                3
            ),
            args(&[
                "test",
                "-j8",
                "--",
                "filter",
                "--test-threads=3",
                "--nocapture"
            ])
        );
        assert_eq!(
            apply_limits(
                &args(&["test", "--", "--test-threads", "1"]),
                &mut env,
                8,
                3
            ),
            args(&["test", "--", "--test-threads", "1"])
        );
        assert_eq!(
            apply_limits(&args(&["+stable", "test", "--doc"]), &mut env, 8, 3),
            args(&["+stable", "test", "--doc"])
        );
        assert_eq!(
            apply_limits(
                &args(&["test", "--", "--", "--test-threads=32"]),
                &mut env,
                8,
                3
            ),
            args(&["test", "--", "--", "--test-threads=32"])
        );
        assert_eq!(
            apply_limits(&args(&["test", "--no-run"]), &mut env, 8, 3),
            args(&["test", "--no-run"])
        );
    }
    #[test]
    fn harness_option_values_and_custom_arguments_are_kept() {
        for input in [
            args(&["test", "--", "--skip", "--test-threads=32"]),
            args(&["test", "--", "--custom-output", "json"]),
            args(&["bench", "--", "--custom-output", "json"]),
            args(&["nextest", "run", "-E", "--test-threads=32"]),
            args(&["build", "--config", "--jobs=32"]),
        ] {
            let mut env = BTreeMap::new();
            assert_eq!(apply_limits(&input, &mut env, 8, 3), input);
        }
        let mut env = BTreeMap::new();
        assert_eq!(
            apply_limits(
                &args(&["test", "--", "--skip", "--", "--test-threads=32"]),
                &mut env,
                8,
                3
            ),
            args(&["test", "--", "--skip", "--", "--test-threads=3"])
        );
    }
    #[test]
    fn serial_parent_cli_sets_the_nested_test_environment() {
        for input in [
            args(&["test", "--", "--test-threads=1"]),
            args(&["nextest", "run", "-j1"]),
        ] {
            let mut env = BTreeMap::new();
            assert_eq!(apply_limits(&input, &mut env, 8, 8), input);
            assert_eq!(env[HELD_TESTS], "1");
            assert_eq!(env["RUST_TEST_THREADS"], "1");
            let tests = nested_test_limit(&env, 32);
            assert_eq!(tests, 1);
            assert_eq!(
                apply_limits(
                    &args(&["nextest", "run", "--test-threads=32"]),
                    &mut env,
                    8,
                    tests
                ),
                args(&["nextest", "run", "--test-threads=1"])
            );
        }
        let mut env = BTreeMap::new();
        apply_limits(&args(&["build", "-j1"]), &mut env, 8, 8);
        assert_eq!(nested_jobs_limit(&env, 32), 1);
    }
    #[test]
    fn smaller_inherited_limits_are_never_increased_by_absent_or_explicit_cli_flags() {
        for input in [
            args(&["build"]),
            args(&["build", "-j32"]),
            args(&["nextest", "run"]),
            args(&["nextest", "run", "--build-jobs=32", "--test-threads=32"]),
        ] {
            let mut env = BTreeMap::from([
                ("cargo_build_jobs".into(), "1".into()),
                ("rust_test_threads".into(), "1".into()),
                ("NEXTEST_TEST_THREADS".into(), "8".into()),
            ]);
            apply_limits(&input, &mut env, 8, 8);
            assert_eq!(env["CARGO_BUILD_JOBS"], "1");
            assert_eq!(env["RUST_TEST_THREADS"], "1");
            assert_eq!(env[HELD_JOBS], "1");
            assert_eq!(env[HELD_TESTS], "1");
            assert!(!env.contains_key("cargo_build_jobs"));
            if first_command(&input) == "nextest" {
                assert_eq!(env["NEXTEST_TEST_THREADS"], "1");
            }
        }
    }
    #[test]
    fn nested_explicit_jobs_never_exceed_the_parent_budget() {
        let mut child = BTreeMap::from([("CARGO_BUILD_JOBS".into(), "4".into())]);
        let limit = nested_jobs_limit(&child, 32);
        assert_eq!(
            apply_jobs(&["build".into(), "-j64".into()], &mut child, limit),
            ["build", "-j4"]
        );
        assert_eq!(child[HELD_JOBS], "4");
    }
    #[test]
    fn metadata_keeps_lockfile_network_and_toolchain_constraints() {
        let args = [
            "+stable",
            "build",
            "--locked",
            "--offline",
            "--frozen",
            "--manifest-path",
            "repo/Cargo.toml",
            "--",
            "--config=ignored",
        ]
        .map(String::from);
        assert_eq!(
            metadata_args(&args),
            [
                "+stable",
                "metadata",
                "--format-version",
                "1",
                "--no-deps",
                "--locked",
                "--offline",
                "--frozen",
                "--manifest-path",
                "repo/Cargo.toml"
            ]
        );
        assert_eq!(
            first_command(&["+stable", "--config", "settings.toml", "fmt"].map(String::from)),
            "fmt"
        );
    }


    #[cfg(target_os = "linux")]
    const GATE_FIXTURE_STDOUT_LIMIT: usize = 64 << 10;

    #[cfg(target_os = "linux")]
    #[derive(Default, Debug)]
    struct GateFixtureOutput {
        bytes: Vec<u8>,
        overflow: bool,
        eof: bool,
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_nonblocking(stdout: &std::process::ChildStdout) -> io::Result<()> {
        use std::os::fd::AsRawFd;
        unsafe extern "C" {
            fn fcntl(fd: i32, command: i32, ...) -> i32;
        }
        // Linux F_GETFL/F_SETFL/O_NONBLOCK; this helper is Linux-only.
        let flags = unsafe { fcntl(stdout.as_raw_fd(), 3) };
        if flags < 0 || unsafe { fcntl(stdout.as_raw_fd(), 4, flags | 0o4000) } < 0 {
            return Err(io::Error::last_os_error());
        }
        Ok(())
    }

    #[cfg(target_os = "linux")]
    fn drain_gate_fixture_output(
        reader: &mut impl std::io::Read,
        output: &mut GateFixtureOutput,
    ) -> io::Result<()> {
        let mut chunk = [0u8; 8192];
        // A noisy writer cannot starve the deadline or process-group collector.
        for _ in 0..16 {
            match reader.read(&mut chunk) {
                Ok(0) => {
                    output.eof = true;
                    break;
                }
                Ok(count) => {
                    let keep = count.min(GATE_FIXTURE_STDOUT_LIMIT.saturating_sub(output.bytes.len()));
                    output.bytes.extend_from_slice(&chunk[..keep]);
                    if keep != count { output.overflow = true; }
                }
                Err(error) if error.kind() == io::ErrorKind::Interrupted => continue,
                Err(error) if error.kind() == io::ErrorKind::WouldBlock => break,
                Err(error) => return Err(error),
            }
        }
        Ok(())
    }

    #[cfg(target_os = "linux")]
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    enum GateFixturePhase { Signalable, ReapOnly, Reaped }

    #[cfg(target_os = "linux")]
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    enum GateFixtureEvent { ExitObserved, GroupSignalled, GroupAbsent, SignalFailed, WaitAttempted, Reaped }

    #[cfg(target_os = "linux")]
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    struct GateFixtureTicket(u64);

    #[cfg(target_os = "linux")]
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    enum GateFixtureStage {
        Spawn, GroupBinding, StdoutSetup, StdoutRead, OutputBound, RootObserve, RootExit, GroupSignal, RootWait,
        GroupObserve, CollectionDeadline, ExecutionDeadline, OwnerUnavailable,
    }

    #[cfg(target_os = "linux")]
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    enum GateFixtureCleanup {
        Collected,
        Pending { kind: io::ErrorKind, stage: GateFixtureStage },
    }

    #[cfg(target_os = "linux")]
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    enum GateFixtureGroupObservation { Unobserved, Absent, PresentOrReused, Failed }

    #[cfg(target_os = "linux")]
    #[derive(Clone, Debug)]
    struct GateFixtureFacts {
        pid: u32,
        stdout_fd: Option<i32>,
        exit_code: Option<i32>,
        exit_signal: Option<i32>,
        stage: GateFixtureStage,
        phase: GateFixturePhase,
        root_reaped: bool,
        root_retained: bool,
        group_fd_bound: bool,
        stdout_eof: bool,
        stdout_complete: bool,
        group_observation: GateFixtureGroupObservation,
        cleanup_expired: bool,
        timed_out: bool,
        completion_checked: bool,
        completion_late: bool,
        error_checked: bool,
        error_late: bool,
        signal_calls: usize,
        wait_calls: usize,
        events: Vec<GateFixtureEvent>,
    }

    #[cfg(target_os = "linux")]
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    struct GateFixturePrimary { kind: io::ErrorKind, stage: GateFixtureStage }

    #[cfg(target_os = "linux")]
    struct GateFixtureState {
        child: std::process::Child,
        root: std::sync::Arc<tempfile::TempDir>,
        group_fd: Option<std::os::fd::OwnedFd>,
        binding: std::os::unix::net::UnixDatagram,
        primary: Option<GateFixturePrimary>,
        stdout: Option<std::process::ChildStdout>,
        output: GateFixtureOutput,
        stage: GateFixtureStage,
        phase: GateFixturePhase,
        status: Option<std::process::ExitStatus>,
        exit_observed: bool,
        stdout_complete: bool,
        group_observation: GateFixtureGroupObservation,
        cleanup_expired: bool,
        timed_out: bool,
        completion_checked: bool,
        completion_late: bool,
        error_checked: bool,
        error_late: bool,
        signal_calls: usize,
        wait_calls: usize,
        events: Vec<GateFixtureEvent>,
    }

    #[cfg(target_os = "linux")]
    impl GateFixtureState {
        fn facts(&self) -> GateFixtureFacts {
            use std::os::fd::AsRawFd;
            use std::os::unix::process::ExitStatusExt;
            GateFixtureFacts {
                pid: self.child.id(), stdout_fd: self.stdout.as_ref().map(AsRawFd::as_raw_fd),
                exit_code: self.status.and_then(|s| s.code()),
                exit_signal: self.status.and_then(|s| s.signal()),
                stage: self.stage, phase: self.phase,
                root_reaped: self.status.is_some(),
                root_retained: std::sync::Arc::strong_count(&self.root) > 0,
                group_fd_bound: self.group_fd.is_some(), stdout_eof: self.output.eof,
                stdout_complete: self.stdout_complete, group_observation: self.group_observation,
                cleanup_expired: self.cleanup_expired, timed_out: self.timed_out,
                completion_checked: self.completion_checked, completion_late: self.completion_late,
                error_checked: self.error_checked, error_late: self.error_late,
                signal_calls: self.signal_calls,
                wait_calls: self.wait_calls, events: self.events.clone(),
            }
        }
        fn record(&mut self, event: GateFixtureEvent) {
            if self.events.last() != Some(&event) && self.events.len() < 32 {
                self.events.push(event);
            }
        }
    }

    // A concrete, one-shot fixture rendezvous. No callback, arbitrary reader,
    // normal receiver thread or external clock input is installed.
    #[cfg(target_os = "linux")]
    struct GateFixtureErrorBarrier {
        stage: GateFixtureStage,
        entered: std::sync::mpsc::SyncSender<std::time::Instant>,
        release: std::sync::mpsc::Receiver<std::time::Instant>,
    }

    #[cfg(target_os = "linux")]
    #[derive(Default)]
    struct GateFixtureFaults {
        setup_error: bool,
        peek_error: bool,
        signal_error: bool,
        wait_error: bool,
        wait_pending: bool,
        read_error: bool,
        hide_eof: bool,
        group_query_error: bool,
        reused_group_model: bool,
        binding_unproven: bool,
        external_reap_observation: bool,
        completion_clock: Option<std::time::Instant>,
        error_clock: Option<std::time::Instant>,
        error_barrier: Option<GateFixtureErrorBarrier>,
    }

    // Linux64 SCM_RIGHTS binds a pidfd opened by the child itself to this
    // controller's private stdin datagram. No numeric-pid open is attempted by
    // the parent, and no SIGCHLD disposition is changed.
    #[cfg(target_os = "linux")]
    #[repr(C)]
    struct GateIovec { base: *mut std::ffi::c_void, length: usize }
    #[cfg(target_os = "linux")]
    #[repr(C)]
    struct GateMessage {
        name: *mut std::ffi::c_void, name_length: u32,
        vectors: *mut GateIovec, vector_count: usize,
        control: *mut std::ffi::c_void, control_length: usize, flags: i32,
    }
    #[cfg(target_os = "linux")]
    #[repr(C)]
    struct GateControl { length: usize, level: i32, kind: i32 }
    #[cfg(target_os = "linux")]
    fn gate_fixture_pidfd_signal(fd: &std::os::fd::OwnedFd, signal: i32) -> io::Result<bool> {
        use std::os::fd::AsRawFd;
        if !cfg!(any(target_arch = "x86_64", target_arch = "aarch64")) {
            return Err(io::Error::new(io::ErrorKind::Unsupported, "fixture pidfd ABI unavailable"));
        }
        unsafe extern "C" { fn syscall(number: std::ffi::c_long, ...) -> std::ffi::c_long; }
        // PIDFD_SIGNAL_PROCESS_GROUP (Linux6.9). The kernel resolves the pid
        // object behind this fd; it never re-resolves a userspace numeric PGID.
        let result = unsafe { syscall(424, fd.as_raw_fd(), signal, std::ptr::null::<std::ffi::c_void>(), 4u32) };
        if result == 0 { return Ok(true); }
        let error = io::Error::last_os_error();
        if error.raw_os_error() == Some(3) { return Ok(false); }
        Err(error)
    }
    #[cfg(target_os = "linux")]
    fn gate_fixture_pidfd_preflight() -> io::Result<()> {
        use std::os::fd::{FromRawFd, OwnedFd};
        if !cfg!(any(target_arch = "x86_64", target_arch = "aarch64")) {
            return Err(io::Error::new(io::ErrorKind::Unsupported, "fixture pidfd ABI unavailable"));
        }
        unsafe extern "C" { fn syscall(number: std::ffi::c_long, ...) -> std::ffi::c_long; }
        // This process cannot have been reaped while executing this call.
        // signal0 only checks native support; it changes no process/group state.
        let raw = unsafe { syscall(434, std::process::id(), 0u32) };
        if raw < 0 { return Err(io::Error::last_os_error()); }
        let fd = unsafe { OwnedFd::from_raw_fd(i32::try_from(raw)
            .map_err(|_| io::Error::other("fixture pidfd invalid"))?) };
        let _ = gate_fixture_pidfd_signal(&fd, 0)?;
        Ok(())
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_publish_self_pidfd() -> io::Result<()> {
        use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
        if !cfg!(any(target_arch = "x86_64", target_arch = "aarch64")) {
            return Err(io::Error::new(io::ErrorKind::Unsupported, "fixture pidfd ABI unavailable"));
        }
        unsafe extern "C" {
            fn syscall(number: std::ffi::c_long, ...) -> std::ffi::c_long;
            fn sendmsg(socket: i32, message: *const GateMessage, flags: i32) -> isize;
            fn getpgrp() -> i32;
        }
        if unsafe { getpgrp() } != std::process::id() as i32 {
            return Err(io::Error::other("fixture is not the expected group leader"));
        }
        let raw = unsafe { syscall(434, std::process::id(), 0u32) };
        if raw < 0 { return Err(io::Error::last_os_error()); }
        let fd = unsafe { OwnedFd::from_raw_fd(i32::try_from(raw).map_err(|_| io::Error::other("fixture pidfd invalid"))?) };
        let mut payload = [0u8; 12];
        payload[..8].copy_from_slice(b"KARAPFD1");
        payload[8..].copy_from_slice(&std::process::id().to_le_bytes());
        let mut vector = GateIovec { base: payload.as_mut_ptr().cast(), length: payload.len() };
        let mut control = [0usize; 3];
        let header = control.as_mut_ptr().cast::<GateControl>();
        unsafe {
            (*header).length = std::mem::size_of::<GateControl>() + std::mem::size_of::<i32>();
            (*header).level = 1; (*header).kind = 1;
            control.as_mut_ptr().cast::<u8>().add(std::mem::size_of::<GateControl>())
                .cast::<i32>().write_unaligned(fd.as_raw_fd());
        }
        let message = GateMessage { name: std::ptr::null_mut(), name_length: 0,
            vectors: &mut vector, vector_count: 1, control: control.as_mut_ptr().cast(),
            control_length: std::mem::size_of_val(&control), flags: 0 };
        // One small datagram into a newly-created socket, no prior payload.
        let sent = unsafe { sendmsg(0, &message, 0x4000) };
        if sent != payload.len() as isize { return Err(io::Error::other("fixture pidfd handoff failed")); }
        Ok(())
    }
    #[cfg(target_os = "linux")]
    fn gate_fixture_receive_pidfd(
        socket: &std::os::unix::net::UnixDatagram,
        expected_pid: u32,
    ) -> io::Result<Option<std::os::fd::OwnedFd>> {
        use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
        unsafe extern "C" { fn recvmsg(socket: i32, message: *mut GateMessage, flags: i32) -> isize; }
        let mut payload = [0u8; 12];
        let mut vector = GateIovec { base: payload.as_mut_ptr().cast(), length: payload.len() };
        let mut control = [0usize; 16];
        let mut message = GateMessage { name: std::ptr::null_mut(), name_length: 0,
            vectors: &mut vector, vector_count: 1, control: control.as_mut_ptr().cast(),
            control_length: std::mem::size_of_val(&control), flags: 0 };
        let count = unsafe { recvmsg(socket.as_raw_fd(), &mut message, 0x40 | 0x4000_0000) };
        if count < 0 {
            let error = io::Error::last_os_error();
            return if error.kind() == io::ErrorKind::WouldBlock { Ok(None) } else { Err(error) };
        }
        let mut descriptors = Vec::new();
        if message.control_length > std::mem::size_of_val(&control) {
            return Err(io::Error::other("fixture ancillary bound invalid"));
        }
        let mut offset = 0usize; let mut shape = true;
        while offset + std::mem::size_of::<GateControl>() <= message.control_length {
            let header = unsafe { &*control.as_ptr().cast::<u8>().add(offset).cast::<GateControl>() };
            if header.length < std::mem::size_of::<GateControl>()
                || header.length > message.control_length - offset { shape = false; break; }
            if header.level == 1 && header.kind == 1 {
                let bytes = header.length - std::mem::size_of::<GateControl>();
                if bytes % std::mem::size_of::<i32>() != 0 { shape = false; }
                for index in 0..bytes / std::mem::size_of::<i32>() {
                    let raw = unsafe { control.as_ptr().cast::<u8>()
                        .add(offset + std::mem::size_of::<GateControl>() + index * 4)
                        .cast::<i32>().read_unaligned() };
                    if raw < 0 { shape = false; } else { descriptors.push(unsafe { OwnedFd::from_raw_fd(raw) }); }
                }
            } else { shape = false; }
            offset += (header.length + std::mem::size_of::<usize>() - 1) & !(std::mem::size_of::<usize>() - 1);
        }
        if count != 12 || message.flags & (8 | 0x20) != 0 || !shape
            || payload[..8] != *b"KARAPFD1"
            || u32::from_le_bytes(payload[8..].try_into().unwrap()) != expected_pid
            || descriptors.len() != 1 { return Err(io::Error::other("fixture pidfd handoff invalid")); }
        let fd = descriptors.pop().unwrap();
        // A real pidfd and kernel process-group scope are required. EINVAL on
        // old kernels fails closed; ESRCH is a bound-but-already-empty group.
        let _ = gate_fixture_pidfd_signal(&fd, 0)?;
        Ok(Some(fd))
    }
    #[cfg(target_os = "linux")]
    #[repr(C, align(16))]
    struct GateWaitInfo { head: [i32; 4], pid: i32, uid: u32, status: i32, rest: [u8; 100] }
    #[cfg(target_os = "linux")]
    fn gate_fixture_pidfd_wait(fd: &std::os::fd::OwnedFd, consume: bool) -> io::Result<Option<std::process::ExitStatus>> {
        use std::os::fd::AsRawFd;
        use std::os::unix::process::ExitStatusExt;
        unsafe extern "C" { fn waitid(kind: i32, id: u32, info: *mut GateWaitInfo, flags: i32) -> i32; }
        let mut info = GateWaitInfo { head: [0; 4], pid: 0, uid: 0, status: 0, rest: [0; 100] };
        if std::mem::size_of::<GateWaitInfo>() != 128
            || !cfg!(any(target_arch = "x86_64", target_arch = "aarch64")) {
            return Err(io::Error::new(io::ErrorKind::Unsupported, "fixture wait ABI unavailable"));
        }
        let flags = 4 | 1 | if consume { 0 } else { 0x0100_0000 };
        if unsafe { waitid(3, fd.as_raw_fd() as u32, &mut info, flags) } < 0 {
            return Err(io::Error::last_os_error());
        }
        if info.pid == 0 { return Ok(None); }
        let status = match info.head[2] {
            1 => (info.status & 255) << 8,
            2 => info.status & 127,
            3 => (info.status & 127) | 128,
            _ => return Err(io::Error::other("fixture wait status invalid")),
        };
        Ok(Some(std::process::ExitStatus::from_raw(status)))
    }

    #[cfg(target_os = "linux")]
    struct GateFixtureRegistry {
        sender: std::sync::mpsc::Sender<(GateFixtureTicket, GateFixtureState)>,
        receiver: std::sync::Mutex<std::sync::mpsc::Receiver<(GateFixtureTicket, GateFixtureState)>>,
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_registry() -> &'static GateFixtureRegistry {
        static REGISTRY: std::sync::OnceLock<GateFixtureRegistry> = std::sync::OnceLock::new();
        REGISTRY.get_or_init(|| {
            let (sender, receiver) = std::sync::mpsc::channel();
            GateFixtureRegistry { sender, receiver: std::sync::Mutex::new(receiver) }
        })
    }

    #[cfg(target_os = "linux")]
    fn retain_gate_fixture(ticket: GateFixtureTicket, state: GateFixtureState) {
        // The receiver is private, cannot be removed and has process lifetime.
        // Sender::send does not wait for space. Drop does no lock/wait/signal.
        let result = gate_fixture_registry().sender.send((ticket, state));
        assert!(result.is_ok(), "process-lifetime fixture receiver disconnected");
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_pending_batch() -> Result<Vec<(GateFixtureTicket, GateFixtureState)>, GateFixtureFailure> {
        let receiver = match gate_fixture_registry().receiver.try_lock() {
            Ok(receiver) => receiver,
            Err(std::sync::TryLockError::Poisoned(error)) => error.into_inner(),
            Err(std::sync::TryLockError::WouldBlock) => return Err(
                GateFixtureFailure::without_owner(io::ErrorKind::WouldBlock, GateFixtureStage::OwnerUnavailable)),
        };
        let mut batch = Vec::new();
        // The closed fixture suite has finite roots. Never turn a queue sweep
        // into an unbounded monitor; owners beyond this batch stay in the channel.
        for _ in 0..256 {
            match receiver.try_recv() {
                Ok(pending) => batch.push(pending),
                Err(std::sync::mpsc::TryRecvError::Empty) => break,
                Err(std::sync::mpsc::TryRecvError::Disconnected) => break,
            }
        }
        Ok(batch)
    }

    #[cfg(target_os = "linux")]
    fn retained_gate_fixture_facts(ticket: GateFixtureTicket) -> Result<Option<GateFixtureFacts>, GateFixtureFailure> {
        let batch = gate_fixture_pending_batch()?;
        let facts = batch.iter().find(|(id, _)| *id == ticket).map(|(_, state)| state.facts());
        for (id, state) in batch { retain_gate_fixture(id, state); }
        Ok(facts)
    }

    #[cfg(target_os = "linux")]
    fn retained_gate_fixture_count() -> Result<usize, GateFixtureFailure> {
        let batch = gate_fixture_pending_batch()?;
        let count = batch.len();
        for (id, state) in batch { retain_gate_fixture(id, state); }
        Ok(count)
    }

    #[cfg(target_os = "linux")]
    struct OwnedGateFixture {
        ticket: GateFixtureTicket,
        state: Option<GateFixtureState>,
    }

    #[cfg(target_os = "linux")]
    impl Drop for OwnedGateFixture {
        fn drop(&mut self) {
            // Move actual ownership, not a PID label. Never signal or wait in Drop.
            if let Some(state) = self.state.take() {
                retain_gate_fixture(self.ticket, state);
            }
        }
    }

    #[cfg(target_os = "linux")]
    #[derive(Debug)]
    struct GateFixtureFailure {
        kind: io::ErrorKind,
        stage: GateFixtureStage,
        cleanup: Option<GateFixtureCleanup>,
        ticket: Option<GateFixtureTicket>,
        facts: Option<Box<GateFixtureFacts>>,
    }

    #[cfg(target_os = "linux")]
    impl GateFixtureFailure {
        fn kind(&self) -> io::ErrorKind { self.kind }
        fn without_owner(kind: io::ErrorKind, stage: GateFixtureStage) -> Self {
            Self { kind, stage, cleanup: None, ticket: None, facts: None }
        }
    }

    #[cfg(target_os = "linux")]
    struct GateFixtureCollection {
        status: std::process::ExitStatus,
        facts: GateFixtureFacts,
        primary: Option<GateFixturePrimary>,
        owner: Option<OwnedGateFixture>,
    }
    #[cfg(target_os = "linux")]
    impl std::fmt::Debug for GateFixtureCollection {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            f.debug_struct("GateFixtureCollection").field("facts", &self.facts)
                .field("primary", &self.primary).finish()
        }
    }
    #[cfg(target_os = "linux")]
    impl GateFixtureCollection {
        // Explicit caller consumption is the only release path for the root.
        // Unwind before this point drops OwnedGateFixture into the pending queue.
        fn output(&self) -> &GateFixtureOutput {
            &self.owner.as_ref().expect("collected native owner missing")
                .state.as_ref().expect("collected native state missing").output
        }
        fn consume(mut self) -> (std::process::ExitStatus, GateFixtureOutput, GateFixtureFacts) {
            let mut owner = self.owner.take().expect("collected native owner missing");
            let state = owner.state.as_ref().expect("collected native state missing");
            assert_eq!(state.phase, GateFixturePhase::Reaped);
            assert!(state.status.is_some() && state.stdout_complete);
            assert_eq!(state.group_observation, GateFixtureGroupObservation::Absent);
            // Finish all checks/clones before moving state. Unwind retains the
            // same root AND bounded stdout/EOF/overflow until explicit consume.
            let facts = self.facts.clone();
            let mut state = owner.state.take().unwrap();
            let output = std::mem::take(&mut state.output);
            drop(state);
            (self.status, output, facts)
        }
    }


    #[cfg(target_os = "linux")]
    static GATE_FIXTURE_SPAWNS: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);

    #[cfg(target_os = "linux")]
    fn start_gate_fixture(
        command: &mut std::process::Command,
        root: &std::sync::Arc<tempfile::TempDir>,
    ) -> Result<OwnedGateFixture, GateFixtureFailure> {
        use std::os::fd::OwnedFd;
        use std::os::unix::process::CommandExt;
        if !cfg!(any(target_arch = "x86_64", target_arch = "aarch64")) {
            return Err(GateFixtureFailure::without_owner(io::ErrorKind::Unsupported, GateFixtureStage::GroupBinding));
        }
        gate_fixture_pidfd_preflight().map_err(|error|
            GateFixtureFailure::without_owner(error.kind(), GateFixtureStage::GroupBinding))?;
        let _ = gate_fixture_registry();
        static NEXT: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1);
        let id = NEXT.fetch_update(std::sync::atomic::Ordering::Relaxed,
            std::sync::atomic::Ordering::Relaxed, |n| n.checked_add(1))
            .map_err(|_| GateFixtureFailure::without_owner(io::ErrorKind::Other, GateFixtureStage::Spawn))?;
        let (receiver, child_socket) = std::os::unix::net::UnixDatagram::pair()
            .map_err(|error| GateFixtureFailure::without_owner(error.kind(), GateFixtureStage::GroupBinding))?;
        receiver.set_nonblocking(true)
            .map_err(|error| GateFixtureFailure::without_owner(error.kind(), GateFixtureStage::GroupBinding))?;
        let socket: OwnedFd = child_socket.into();
        command.current_dir(root.path()).process_group(0)
            .env("CC_GATE_PUBLISH_SELF_PIDFD", "1")
            .stdin(Stdio::from(socket)).stdout(Stdio::piped()).stderr(Stdio::null());
        let mut child = command.spawn().map_err(|error|
            GateFixtureFailure::without_owner(error.kind(), GateFixtureStage::Spawn))?;
        let stdout = child.stdout.take();
        GATE_FIXTURE_SPAWNS.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        Ok(OwnedGateFixture { ticket: GateFixtureTicket(id), state: Some(GateFixtureState {
            child, root: root.clone(), group_fd: None, binding: receiver, primary: None,
            stdout, output: GateFixtureOutput::default(),
            stage: GateFixtureStage::StdoutSetup, phase: GateFixturePhase::Signalable, status: None,
            exit_observed: false, stdout_complete: false,
            group_observation: GateFixtureGroupObservation::Unobserved,
            cleanup_expired: false, timed_out: false, completion_checked: false, completion_late: false,
            error_checked: false, error_late: false,
            signal_calls: 0, wait_calls: 0,
            events: Vec::new(),
        }) })
    }

    #[cfg(target_os = "linux")]
    fn latch_gate_fixture_deadline_at(
        state: &mut GateFixtureState, deadline: std::time::Instant, observed: std::time::Instant,
    ) {
        if observed >= deadline {
            state.timed_out = true;
            state.primary.get_or_insert(GateFixturePrimary {
                kind: io::ErrorKind::TimedOut, stage: GateFixtureStage::ExecutionDeadline,
            });
        }
    }
    #[cfg(target_os = "linux")]
    fn latch_gate_fixture_deadline(
        state: &mut GateFixtureState, deadline: std::time::Instant,
    ) -> std::time::Instant {
        let observed = std::time::Instant::now();
        latch_gate_fixture_deadline_at(state, deadline, observed);
        observed
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_error_barrier(
        faults: &mut GateFixtureFaults,
        stage: GateFixtureStage,
        entered: std::time::Instant,
        limit: std::time::Duration,
    ) -> io::Result<()> {
        if !faults.error_barrier.as_ref().is_some_and(|barrier| barrier.stage == stage) {
            return Ok(());
        }
        let barrier = faults.error_barrier.take().unwrap();
        barrier.entered.try_send(entered)
            .map_err(|_| io::Error::other("owned error-clock entry unavailable"))?;
        let clock = barrier.release.recv_timeout(limit).map_err(|error| {
            let kind = match error {
                std::sync::mpsc::RecvTimeoutError::Timeout => io::ErrorKind::TimedOut,
                std::sync::mpsc::RecvTimeoutError::Disconnected => io::ErrorKind::Other,
            };
            io::Error::new(kind, "owned error-clock release unavailable")
        })?;
        faults.error_clock = Some(clock);
        Ok(())
    }

    #[cfg(target_os = "linux")]
    fn observe_gate_fixture_error_deadline(
        state: &mut GateFixtureState,
        deadline: std::time::Instant,
        injected: Option<std::time::Instant>,
    ) {
        // This is the normal execution's error-report observation, not a
        // timestamp for the OS failure itself or a new cleanup deadline.
        let real_observed = std::time::Instant::now();
        let error_observed = injected.map_or(real_observed, |clock| clock.max(real_observed));
        state.error_checked = true;
        state.error_late = error_observed >= deadline;
        latch_gate_fixture_deadline_at(state, deadline, error_observed);
    }

    #[cfg(target_os = "linux")]
    fn defer_gate_fixture(
        mut owner: OwnedGateFixture,
        kind: io::ErrorKind,
        error_deadline: Option<std::time::Instant>,
        faults: &GateFixtureFaults,
    ) -> GateFixtureFailure {
        let current = owner.state.as_ref().map_or(GateFixtureStage::OwnerUnavailable, |state| state.stage);
        let primary = owner.state.as_mut().map(|state| {
            // Keep the observed error (or an earlier primary) before observing
            // report time. A late observation is not proof that deadline was first.
            let primary = *state.primary.get_or_insert(GateFixturePrimary { kind, stage: current });
            if let Some(deadline) = error_deadline {
                observe_gate_fixture_error_deadline(state, deadline, faults.error_clock);
            }
            primary
        }).unwrap_or(GateFixturePrimary { kind, stage: current });
        let facts = owner.state.as_ref().map(|state| Box::new(state.facts()));
        if let Some(state) = owner.state.take() { retain_gate_fixture(owner.ticket, state); }
        GateFixtureFailure { kind: primary.kind, stage: primary.stage,
            cleanup: Some(GateFixtureCleanup::Pending { kind, stage: current }),
            ticket: Some(owner.ticket), facts }
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_signal_owned_group(
        state: &mut GateFixtureState,
        faults: &GateFixtureFaults,
    ) -> io::Result<()> {
        if state.phase != GateFixturePhase::Signalable || state.status.is_some()
            || faults.binding_unproven {
            return Err(io::Error::new(io::ErrorKind::InvalidInput, "fixture group binding unavailable"));
        }
        let fd = state.group_fd.as_ref().ok_or_else(||
            io::Error::new(io::ErrorKind::InvalidInput, "fixture group binding unavailable"))?;
        if faults.signal_error {
            state.record(GateFixtureEvent::SignalFailed);
            return Err(io::Error::new(io::ErrorKind::PermissionDenied, "owned signal fault"));
        }
        // This is a kernel pid-object group signal. Auto/external reap cannot
        // retarget it to a subsequently allocated numeric PID/PGID.
        state.signal_calls += 1;
        let present = gate_fixture_pidfd_signal(fd, 9)?;
        state.record(if present { GateFixtureEvent::GroupSignalled } else { GateFixtureEvent::GroupAbsent });
        state.phase = GateFixturePhase::ReapOnly;
        Ok(())
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_group_absent_after_reap(
        state: &mut GateFixtureState,
        faults: &GateFixtureFaults,
    ) -> io::Result<bool> {
        if state.phase != GateFixturePhase::Reaped || state.status.is_none() {
            return Err(io::Error::other("fixture root wait unavailable"));
        }
        if faults.group_query_error {
            state.group_observation = GateFixtureGroupObservation::Failed;
            return Err(io::Error::other("owned group observation fault"));
        }
        if faults.reused_group_model {
            state.group_observation = GateFixtureGroupObservation::PresentOrReused;
            return Ok(false);
        }
        let fd = state.group_fd.as_ref().ok_or_else(|| io::Error::other("fixture group binding unavailable"))?;
        match gate_fixture_pidfd_signal(fd, 0) {
            Ok(false) => { state.group_observation = GateFixtureGroupObservation::Absent; Ok(true) }
            Ok(true) => { state.group_observation = GateFixtureGroupObservation::PresentOrReused; Ok(false) }
            Err(error) => { state.group_observation = GateFixtureGroupObservation::Failed; Err(error) }
        }
    }

    #[cfg(target_os = "linux")]
    fn advance_gate_fixture(
        mut owner: OwnedGateFixture,
        deadline: std::time::Instant,
        cleanup_limit: std::time::Duration,
        force_stop: bool,
        mut faults: GateFixtureFaults,
    ) -> Result<GateFixtureCollection, GateFixtureFailure> {
        let entry_observed = (!force_stop).then(std::time::Instant::now);
        let error_deadline = (!force_stop).then_some(deadline);
        if let Some(state) = owner.state.as_mut() {
            if let Some(entry_observed) = entry_observed {
                latch_gate_fixture_deadline_at(state, deadline, entry_observed);
            }
            state.stage = GateFixtureStage::StdoutSetup;
        }
        if let Some(entered) = entry_observed {
            if let Err(error) = gate_fixture_error_barrier(
                &mut faults, GateFixtureStage::StdoutSetup, entered, cleanup_limit,
            ) {
                return Err(defer_gate_fixture(owner, error.kind(), error_deadline, &faults));
            }
        }
        let setup = if faults.setup_error { Err(io::Error::other("owned stdout setup fault")) } else {
            owner.state.as_ref().and_then(|state| state.stdout.as_ref())
                .ok_or_else(|| io::Error::other("fixture stdout unavailable"))
                .and_then(gate_fixture_nonblocking)
        };
        if let Err(error) = setup { return Err(defer_gate_fixture(owner, error.kind(), error_deadline, &faults)); }
        let mut collection_deadline = if force_stop
            || owner.state.as_ref().is_some_and(|state| state.phase != GateFixturePhase::Signalable || state.primary.is_some())
        { Some(std::time::Instant::now() + cleanup_limit) } else { None };
        loop {
            let step = (|| -> io::Result<bool> {
                let state = owner.state.as_mut().ok_or_else(|| io::Error::other("fixture owner unavailable"))?;
                // Latch before any secondary I/O/stop error in this iteration.
                let iteration_observed = if force_stop { None } else {
                    Some(latch_gate_fixture_deadline(state, deadline))
                };
                if state.primary.is_some() && collection_deadline.is_none() {
                    collection_deadline = Some(std::time::Instant::now() + cleanup_limit);
                }
                state.stage = GateFixtureStage::StdoutRead;
                if let Some(entered) = iteration_observed {
                    gate_fixture_error_barrier(
                        &mut faults, GateFixtureStage::StdoutRead, entered, cleanup_limit,
                    )?;
                }
                if !state.output.eof {
                    if faults.read_error { return Err(io::Error::other("owned read fault")); }
                    drain_gate_fixture_output(state.stdout.as_mut()
                        .ok_or_else(|| io::Error::other("fixture stdout unavailable"))?, &mut state.output)?;
                }
                state.stdout_complete = state.output.eof && !faults.hide_eof;
                if state.output.overflow {
                    state.primary.get_or_insert(GateFixturePrimary {
                        kind: io::ErrorKind::InvalidData, stage: GateFixtureStage::OutputBound,
                    });
                }
                if state.group_fd.is_none() {
                    state.stage = GateFixtureStage::GroupBinding;
                    if faults.binding_unproven { return Err(io::Error::other("owned group binding unproved")); }
                    state.group_fd = gate_fixture_receive_pidfd(&state.binding, state.child.id())?;
                    if state.group_fd.is_none() { return Ok(false); }
                }
                if state.phase == GateFixturePhase::Signalable {
                    state.stage = GateFixtureStage::RootObserve;
                    if faults.peek_error { return Err(io::Error::other("owned exit observation fault")); }
                    if faults.external_reap_observation { return Err(io::Error::from_raw_os_error(10)); }
                    if let Some(status) = gate_fixture_pidfd_wait(state.group_fd.as_ref().unwrap(), false)? {
                        if !status.success() {
                            state.primary.get_or_insert(GateFixturePrimary {
                                kind: io::ErrorKind::Other, stage: GateFixtureStage::RootExit,
                            });
                        }
                        if !state.exit_observed {
                            state.exit_observed = true;
                            state.record(GateFixtureEvent::ExitObserved);
                        }
                    }
                    if force_stop || state.primary.is_some() || state.output.overflow || state.exit_observed {
                        state.stage = GateFixtureStage::GroupSignal;
                        gate_fixture_signal_owned_group(state, &faults)?;
                        if collection_deadline.is_none() {
                            collection_deadline = Some(std::time::Instant::now() + cleanup_limit);
                        }
                    }
                }
                if state.phase == GateFixturePhase::ReapOnly {
                    state.stage = GateFixtureStage::RootWait;
                    if faults.wait_error { return Err(io::Error::other("owned wait fault")); }
                    if !faults.wait_pending {
                        state.wait_calls += 1;
                        state.record(GateFixtureEvent::WaitAttempted);
                        if let Some(status) = gate_fixture_pidfd_wait(state.group_fd.as_ref().unwrap(), true)? {
                            state.status = Some(status);
                            state.phase = GateFixturePhase::Reaped;
                            state.record(GateFixtureEvent::Reaped);
                        }
                    }
                }
                if state.phase == GateFixturePhase::Reaped { state.stage = GateFixtureStage::GroupObserve; }
                Ok(state.phase == GateFixturePhase::Reaped && state.stdout_complete
                    && gate_fixture_group_absent_after_reap(state, &faults)?)
            })();
            match step {
                Err(error) => return Err(defer_gate_fixture(owner, error.kind(), error_deadline, &faults)),
                Ok(true) => {
                    let Some(status) = owner.state.as_ref().and_then(|state| state.status) else {
                        return Err(defer_gate_fixture(owner, io::ErrorKind::InvalidData, error_deadline, &faults));
                    };
                    let state = owner.state.as_mut().unwrap();
                    if !force_stop {
                        // Last monotonic decision observation after Wait/EOF/group
                        // conditions. It is not an exact kernel completion timestamp.
                        let real_observed = std::time::Instant::now();
                        // A concrete test clock may only move this observation later;
                        // it cannot turn an actually expired operation into success.
                        let completion_observed = faults.completion_clock
                            .map_or(real_observed, |injected| injected.max(real_observed));
                        state.completion_checked = true;
                        state.completion_late = completion_observed >= deadline;
                        latch_gate_fixture_deadline_at(state, deadline, completion_observed);
                    }
                    let facts = state.facts();
                    let primary = state.primary;
                    // The payload stays in the same state while this collection
                    // can still be dropped/panicked before caller consumption.
                    return Ok(GateFixtureCollection { status, facts, primary, owner: Some(owner) });
                }
                Ok(false) => {}
            }
            if collection_deadline.is_some_and(|limit| std::time::Instant::now() >= limit) {
                if let Some(state) = owner.state.as_mut() {
                    state.stage = GateFixtureStage::CollectionDeadline;
                    state.cleanup_expired = true;
                }
                return Err(defer_gate_fixture(owner, io::ErrorKind::TimedOut, error_deadline, &faults));
            }
            std::thread::sleep(std::time::Duration::from_millis(5));
        }
    }

    #[cfg(target_os = "linux")]
    fn take_retained_gate_fixture(ticket: GateFixtureTicket) -> Result<OwnedGateFixture, GateFixtureFailure> {
        let batch = gate_fixture_pending_batch().map_err(|mut error| {
            error.ticket = Some(ticket);
            error
        })?;
        let mut selected = None;
        for (id, state) in batch {
            if id == ticket && selected.is_none() {
                selected = Some(state);
            } else {
                retain_gate_fixture(id, state);
            }
        }
        let state = selected.ok_or(GateFixtureFailure {
            kind: io::ErrorKind::WouldBlock, stage: GateFixtureStage::OwnerUnavailable,
            cleanup: None, ticket: Some(ticket), facts: None,
        })?;
        Ok(OwnedGateFixture { ticket, state: Some(state) })
    }

    #[cfg(target_os = "linux")]
    fn drain_retained_gate_fixture(
        ticket: GateFixtureTicket,
        limit: std::time::Duration,
        faults: GateFixtureFaults,
    ) -> Result<GateFixtureCollection, GateFixtureFailure> {
        let owner = take_retained_gate_fixture(ticket)?;
        advance_gate_fixture(owner, std::time::Instant::now(), limit, true, faults)
    }

    #[cfg(target_os = "linux")]
    fn collect_gate_fixture(
        command: &mut std::process::Command,
        root: &std::sync::Arc<tempfile::TempDir>,
        deadline: std::time::Instant,
    ) -> Result<GateFixtureCollection, GateFixtureFailure> {
        collect_gate_fixture_with_faults(command, root, deadline, GateFixtureFaults::default())
    }

    #[cfg(target_os = "linux")]
    fn collect_gate_fixture_with_faults(
        command: &mut std::process::Command,
        root: &std::sync::Arc<tempfile::TempDir>,
        deadline: std::time::Instant,
        faults: GateFixtureFaults,
    ) -> Result<GateFixtureCollection, GateFixtureFailure> {
        let owner = start_gate_fixture(command, root)?;
        let result = advance_gate_fixture(owner, deadline, std::time::Duration::from_secs(5), false, faults);
        match result {
            Ok(collected) if collected.primary.is_some() => {
                let primary = collected.primary.unwrap();
                let (_, _, facts) = collected.consume();
                Err(GateFixtureFailure { kind: primary.kind, stage: primary.stage,
                    cleanup: Some(GateFixtureCleanup::Collected), ticket: None, facts: Some(Box::new(facts)) })
            }
            Ok(collected) => Ok(collected),
            Err(mut error) => {
                // Exactly one finite caller cleanup. Original primary never changes.
                if let Some(ticket) = error.ticket {
                    match drain_retained_gate_fixture(ticket, std::time::Duration::from_secs(5),
                        GateFixtureFaults::default())
                    {
                        Ok(collected) => {
                            let (_, _, facts) = collected.consume();
                            error.ticket = None;
                            error.cleanup = Some(GateFixtureCleanup::Collected);
                            error.facts = Some(Box::new(facts));
                        }
                        Err(pending) => {
                            error.ticket = pending.ticket;
                            error.cleanup = pending.cleanup.or(Some(GateFixtureCleanup::Pending {
                                kind: pending.kind, stage: pending.stage,
                            }));
                            if pending.facts.is_some() { error.facts = pending.facts; }
                        }
                    }
                }
                Err(error)
            }
        }
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_stage_name(stage: GateFixtureStage) -> &'static str {
        match stage {
            GateFixtureStage::Spawn => "spawn",
            GateFixtureStage::GroupBinding => "group_binding",
            GateFixtureStage::StdoutSetup => "stdout_setup",
            GateFixtureStage::StdoutRead => "stdout_read",
            GateFixtureStage::OutputBound => "output_bound",
            GateFixtureStage::RootObserve => "root_observe",
            GateFixtureStage::RootExit => "root_exit",
            GateFixtureStage::GroupSignal => "group_signal",
            GateFixtureStage::RootWait => "root_wait",
            GateFixtureStage::GroupObserve => "group_observe",
            GateFixtureStage::CollectionDeadline => "collection_deadline",
            GateFixtureStage::ExecutionDeadline => "execution_deadline",
            GateFixtureStage::OwnerUnavailable => "owner_unavailable",
        }
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_kind_name(kind: io::ErrorKind) -> &'static str {
        match kind {
            io::ErrorKind::TimedOut => "timeout",
            io::ErrorKind::PermissionDenied => "denied",
            io::ErrorKind::WouldBlock => "pending",
            io::ErrorKind::Unsupported => "unsupported",
            io::ErrorKind::InvalidInput => "invalid_input",
            io::ErrorKind::InvalidData => "invalid_data",
            io::ErrorKind::NotFound => "not_found",
            _ => "unavailable",
        }
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_failure_projection(error: &GateFixtureFailure) -> String {
        let cleanup = error.cleanup.map(|outcome| match outcome {
            GateFixtureCleanup::Collected => json!({"state":"collected"}),
            GateFixtureCleanup::Pending { kind, stage } => json!({
                "state":"pending", "kind":gate_fixture_kind_name(kind), "stage":gate_fixture_stage_name(stage),
            }),
        });
        let facts = error.facts.as_ref().map(|facts| {
            let phase = match facts.phase {
                GateFixturePhase::Signalable => "signalable",
                GateFixturePhase::ReapOnly => "reap_only",
                GateFixturePhase::Reaped => "reaped",
            };
            let group = match facts.group_observation {
                GateFixtureGroupObservation::Unobserved => "unobserved",
                GateFixtureGroupObservation::Absent => "absent",
                GateFixtureGroupObservation::PresentOrReused => "present_or_unknown",
                GateFixtureGroupObservation::Failed => "failed",
            };
            json!({"phase":phase,"root_wait":facts.root_reaped,"root_retained":facts.root_retained,
                "group_binding":facts.group_fd_bound,"raw_eof":facts.stdout_eof,
                "stdout_complete":facts.stdout_complete,"group":group,
                "execution_timeout":facts.timed_out,"cleanup_timeout":facts.cleanup_expired,
                "completion_checked":facts.completion_checked,"completion_deadline_elapsed":facts.completion_late,
                "execution_error_checked":facts.error_checked,"execution_error_deadline_elapsed":facts.error_late,
                "signal_calls":facts.signal_calls.min(4096),"wait_calls":facts.wait_calls.min(4096),
                "exit_code":facts.exit_code,"exit_signal":facts.exit_signal})
        });
        let output = json!({"domain":"owned-gate-fixture-failure-v1",
            "primary":{"kind":gate_fixture_kind_name(error.kind),"stage":gate_fixture_stage_name(error.stage)},
            "cleanup":cleanup,"retained_owner":error.ticket.is_some(),"latest":facts}).to_string();
        if output.len() > 4096 {
            return "{\"domain\":\"owned-gate-fixture-failure-v1\",\"primary\":{\"kind\":\"unavailable\"},\"latest\":null}".into();
        }
        output
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_actual_caller(
        result: Result<GateFixtureCollection, GateFixtureFailure>,
        mode: &str, cache_held: bool, nested: bool,
    ) -> GateFixtureCollection {
        match result {
            Ok(collection) => collection,
            Err(error) => panic!("owned gate fixture {mode}/{cache_held}/{nested}: {}",
                gate_fixture_failure_projection(&error)),
        }
    }

    // This fixture uses the Cargo that compiled this test, never an executable
    // written under scratch. The ordinary child binary is the same test image.
    #[cfg(target_os = "linux")]
    #[test]
    fn non_windows_fixed_target_fixture_child() {
        let Ok(mode) = std::env::var("CC_GATE_PLATFORM_FIXTURE") else {
            return;
        };
        if std::env::var("CC_GATE_PUBLISH_SELF_PIDFD").as_deref() == Ok("1") {
            gate_fixture_publish_self_pidfd().unwrap();
        }
        if mode == "stdout-silent" {
            std::thread::sleep(std::time::Duration::from_secs(90));
            return;
        }
        if mode == "stdout-leftover" {
            let child = std::process::Command::new(std::env::current_exe().unwrap())
                .args(["--exact", "gate::tests::non_windows_fixed_target_fixture_child",
                    "--nocapture", "--test-threads=1"])
                .env("CC_GATE_PLATFORM_FIXTURE", "stdout-silent")
                .env_remove("CC_GATE_PUBLISH_SELF_PIDFD")
                .stdin(Stdio::null()).stdout(Stdio::inherit()).stderr(Stdio::null())
                .spawn().unwrap();
            // Intentionally let this owned same-group descendant outlive its root.
            std::fs::write("descendant-id", child.id().to_string()).unwrap();
            drop(child);
            return;
        }
        if mode == "stdout-finite" || mode == "stdout-overflow" {
            use std::io::Write;
            let mut stdout = std::io::stdout().lock();
            if mode == "stdout-finite" {
                stdout.write_all(b"owned bounded stdout fixture\n").unwrap();
            } else {
                let chunk = [b'x'; 8192];
                while stdout.write_all(&chunk).is_ok() {}
            }
            return;
        }
        let root = std::env::current_dir().unwrap();
        let locks = root.join("locks");
        let intended_build = matches!(mode.as_str(),
            "shared-forwarded" | "shared-metadata" | "ordinary-build"
            | "shared-symlink" | "shared-symlink-env"
            | "unknown-symlink" | "changed-symlink"
            | "shared-cache-child" | "shared-cache-child-absolute"
            | "shared-cache-child-alias" | "shared-cache-child-env");
        let mut input = if intended_build {
            args(&["build", "--locked", "--offline"])
        } else {
            args(&["build", "--help", "--locked", "--offline"])
        };
        if mode == "ordinary-readonly" || mode == "ordinary-pivot-readonly"
            || mode == "ordinary-injected-readonly" || mode == "shared-pivot-readonly"
            || mode == "ordinary-symlink" || mode == "ordinary-cache-prefix"
            || mode == "ordinary-cache-prefix-alias"
            || mode == "ordinary-scope-exit-relative" || mode == "ordinary-scope-exit-absolute"
            || mode == "ordinary-scope-exit-alias"
        {
            input = args(&["metadata", "--locked", "--offline", "--no-deps", "--format-version=1"]);
        }
        let ordinary = root.join("ordinary-target").to_string_lossy().into_owned();
        match mode.as_str() {
            "ordinary-build" | "ordinary-legacy" | "ordinary-override" => {
                input.extend(["--target-dir".into(), ordinary]);
            }
            "shared-cache-child" => input.push("--target-dir=D:/CC/BuildCache/cargo-shared/debug".into()),
            "shared-cache-child-absolute" => input.extend(["--target-dir".into(),
                root.join("D:/CC/BuildCache/cargo-shared/debug").to_string_lossy().into_owned()]),
            "shared-cache-child-alias" | "shared-symlink" | "unknown-symlink" => {
                input.push("--target-dir=alias".into());
            }
            "changed-symlink" => {
                for (replacement, expected) in [
                    (root.join("alternate/nested"), io::ErrorKind::InvalidInput),
                    (root.join("D:/CC/BuildCache/cargo-shared"), io::ErrorKind::Unsupported),
                    (root.join("missing-target"), io::ErrorKind::InvalidInput),
                ] {
                    let mut checks = crate::cache::SharedTargetChecks::default();
                    checks.check_path(Path::new("alias")).unwrap();
                    std::fs::remove_file(root.join("alias")).unwrap();
                    std::os::unix::fs::symlink(replacement, root.join("alias")).unwrap();
                    assert_eq!(checks.recheck().unwrap_err().kind(), expected);
                    std::fs::remove_file(root.join("alias")).unwrap();
                    std::os::unix::fs::symlink(root.join("ordinary-target"), root.join("alias")).unwrap();
                    // Restoring a spelling is not evidence that an unknown observation recovered.
                    assert_eq!(checks.recheck().unwrap_err().kind(), io::ErrorKind::InvalidInput);
                }
                std::fs::remove_file(root.join("alias")).unwrap();
                std::os::unix::fs::symlink(
                    root.join("D:/CC/BuildCache/cargo-shared"), root.join("alias"),
                ).unwrap();
                input.push("--target-dir=alias".into());
            }
            "shared-args" => input.extend(["--target-dir".into(), crate::cache::SHARED.into()]),
            "shared-alias" => input.push("--target-dir=d:/CC/BuildCache/./cargo-shared/".into()),
            "shared-resolved" => input.extend([
                "--target-dir".into(),
                root.join(crate::cache::SHARED).to_string_lossy().into_owned(),
            ]),
            "shared-leading-dot" => input.push("--target-dir=./D:/CC/BuildCache/cargo-shared".into()),
            "shared-leading-dot-backslash" => {
                input.push(format!("--target-dir=./{}", crate::cache::SHARED));
            }
            "shared-pivot-relative" => {
                input.push("--target-dir=pivot/../D:/CC/BuildCache/cargo-shared".into());
            }
            "shared-pivot-absolute" => input.extend([
                "--target-dir".into(),
                root.join("pivot/../D:/CC/BuildCache/cargo-shared").to_string_lossy().into_owned(),
            ]),
            "shared-cwd-parent" => input.extend([
                "--target-dir".into(),
                root.join("..").join(root.file_name().unwrap())
                    .join("D:/CC/BuildCache/cargo-shared").to_string_lossy().into_owned(),
            ]),
            "shared-pivot-link" => {
                input.push("--target-dir=pivot-link/../D:/CC/BuildCache/cargo-shared".into());
            }
            "ordinary-scope-exit-relative" | "ordinary-scope-exit-absolute" | "ordinary-scope-exit-alias"
            | "ordinary-cache-prefix" | "ordinary-cache-prefix-alias" | "shared-cache-child-env"
            | "ordinary-symlink" | "shared-symlink-env" | "shared-env" | "shared-build-env" | "shared-forwarded" | "shared-metadata"
            | "shared-pivot-readonly" | "ordinary-readonly" | "ordinary-pivot-readonly"
            | "ordinary-ignored-forwarded" | "ordinary-build-ignored-forwarded"
            | "ordinary-injected-legacy" | "ordinary-injected-readonly" => {}
            _ => panic!("unknown compiled gate fixture mode"),
        }
        // Start from the actual child environment; command() compares this
        // base to the migrated environment when constructing the real child.
        assert_eq!(build_like(&input), intended_build, "fixture build branch: {mode}");
        let env = std::env::vars().collect();
        let ordinary_existed_before = root.join("ordinary-target").exists();
        let fixed_paths = [root.join("D:"), root.join("d:"), root.join(crate::cache::SHARED)];
        let fixed_existed_before = fixed_paths.each_ref().map(|p| std::fs::symlink_metadata(p).is_ok());
        let mut admissions = 0;
        let result = run_with_admitter(&input, &locks, &env, |_, _, _| {
            admissions += 1;
            Err(io::Error::other("unexpected admission in non-Windows fixture"))
        });
        let (code, error, windows_lock) = match result {
            Ok(code) => (Some(code), None, false),
            Err(error) => {
                let windows_lock = error.kind() == io::ErrorKind::Unsupported
                    && error.to_string() == "Windows byte-range lock required";
                (None, Some(format!("{:?}", error.kind())), windows_lock)
            }
        };
        let report = json!({
            "mode":mode, "build_like":build_like(&input),
            "cache_held":env.get("CODEX_JOBS_CACHE_LOCK_HELD").is_some_and(|s| s == "1"),
            "nested":env.get(LOCK_HELD).is_some_and(|s| s == "1"),
            "code":code, "error_kind":error, "admissions":admissions,
            "windows_lock_boundary":windows_lock,
            "locks_created":std::fs::read_dir(&locks).unwrap().count(),
            "ordinary_target_existed_before":ordinary_existed_before,
            "ordinary_target_created":!ordinary_existed_before && root.join("ordinary-target").exists(),
            "fixed_target_created":fixed_paths.iter().zip(fixed_existed_before)
                .any(|(path, existed)| !existed && std::fs::symlink_metadata(path).is_ok()),
            "fixed_fixture_unchanged":!matches!(mode.as_str(),
                "shared-symlink" | "shared-symlink-env" | "changed-symlink"
                    | "shared-cache-child-alias" | "ordinary-cache-prefix" | "ordinary-cache-prefix-alias"
                    | "ordinary-scope-exit-relative" | "ordinary-scope-exit-absolute" | "ordinary-scope-exit-alias")
                || (std::fs::read(root.join("D:/CC/BuildCache/cargo-shared/keep")).unwrap()
                        == b"owned fixed sentinel"
                    && std::fs::read_dir(root.join("D:/CC/BuildCache/cargo-shared")).unwrap().count()
                        == if mode == "shared-cache-child-alias" { 2 } else { 1 })
        });
        std::fs::write(root.join("result.json"), serde_json::to_vec(&report).unwrap()).unwrap();
    }

    #[cfg(target_os = "linux")]
    fn assert_fixture_data_not_executable(root: &Path) {
        use std::os::unix::fs::PermissionsExt;
        for entry in std::fs::read_dir(root).unwrap() {
            let path = entry.unwrap().path();
            let metadata = std::fs::symlink_metadata(&path).unwrap();
            if metadata.is_dir() {
                assert_fixture_data_not_executable(&path);
            } else if metadata.is_file() {
                assert_eq!(metadata.permissions().mode() & 0o111, 0,
                    "fixture scratch file unexpectedly executable");
            }
        }
    }

    #[cfg(target_os = "linux")]
    fn inspect_gate_platform_fixture(mode: &str, cache_held: bool, nested: bool) -> Value {
        inspect_gate_platform_fixture_with_faults(mode, cache_held, nested, GateFixtureFaults::default())
    }

    #[cfg(target_os = "linux")]
    fn inspect_gate_platform_fixture_with_faults(
        mode: &str, cache_held: bool, nested: bool, faults: GateFixtureFaults,
    ) -> Value {
        let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
        std::fs::create_dir(root.path().join("locks")).unwrap();
        std::fs::create_dir(root.path().join("pivot")).unwrap();
        std::fs::create_dir_all(root.path().join("alternate/nested")).unwrap();
        std::os::unix::fs::symlink(
            root.path().join("alternate/nested"),
            root.path().join("pivot-link"),
        ).unwrap();
        std::fs::create_dir(root.path().join("src")).unwrap();
        std::fs::write(root.path().join("src/lib.rs"), b"").unwrap();
        std::fs::write(
            root.path().join("Cargo.toml"),
            b"[package]\nname = \"owned_gate_fixture\"\nversion = \"0.0.0\"\nedition = \"2024\"\n[workspace]\n",
        ).unwrap();
        std::fs::write(
            root.path().join("Cargo.lock"),
            b"version = 4\n\n[[package]]\nname = \"owned_gate_fixture\"\nversion = \"0.0.0\"\n",
        ).unwrap();
        if matches!(mode, "shared-symlink" | "shared-symlink-env" | "changed-symlink"
            | "shared-cache-child-alias" | "ordinary-cache-prefix" | "ordinary-cache-prefix-alias"
            | "ordinary-scope-exit-relative" | "ordinary-scope-exit-absolute" | "ordinary-scope-exit-alias") {
            let fixed = root.path().join("D:/CC/BuildCache/cargo-shared");
            std::fs::create_dir_all(&fixed).unwrap();
            std::fs::write(fixed.join("keep"), b"owned fixed sentinel").unwrap();
            if mode == "shared-cache-child-alias" { std::fs::create_dir(fixed.join("debug")).unwrap(); }
        }
        if matches!(mode, "ordinary-cache-prefix" | "ordinary-cache-prefix-alias"
            | "ordinary-scope-exit-relative" | "ordinary-scope-exit-absolute" | "ordinary-scope-exit-alias") {
            std::fs::create_dir_all(root.path().join("D:/CC/BuildCache/cargo-shared-peer")).unwrap();
        }
        if matches!(mode, "ordinary-symlink" | "changed-symlink") {
            std::fs::create_dir(root.path().join("ordinary-target")).unwrap();
        }
        if matches!(mode, "shared-symlink" | "shared-symlink-env" | "ordinary-symlink"
            | "unknown-symlink" | "changed-symlink" | "shared-cache-child-alias" | "ordinary-cache-prefix-alias"
            | "ordinary-scope-exit-alias")
        {
            let target = match mode {
                "shared-symlink" | "shared-symlink-env" | "ordinary-scope-exit-alias" =>
                    root.path().join("D:/CC/BuildCache/cargo-shared"),
                "shared-cache-child-alias" => root.path().join("D:/CC/BuildCache/cargo-shared/debug"),
                "ordinary-cache-prefix-alias" => root.path().join("D:/CC/BuildCache/cargo-shared-peer"),
                "unknown-symlink" => root.path().join("missing-target"),
                _ => root.path().join("ordinary-target"),
            };
            std::os::unix::fs::symlink(target, root.path().join("alias")).unwrap();
        }
        if mode == "shared-metadata" {
            std::fs::create_dir(root.path().join(".cargo")).unwrap();
            let target = root.path().join("D:/CC/BuildCache/cargo-shared");
            std::fs::write(
                root.path().join(".cargo/config.toml"),
                format!("[build]\ntarget-dir = {:?}\n", target.to_string_lossy()),
            ).unwrap();
        }

        let cargo = PathBuf::from(env!("CARGO"));
        assert!(cargo.is_absolute() && cargo.is_file(), "current Cargo input missing");
        assert!(cargo.file_stem().is_some_and(|s| s == "cargo"));
        let ordinary = root.path().join("ordinary-target").to_string_lossy().into_owned();
        let mut command = std::process::Command::new(std::env::current_exe().unwrap());
        command.args([
            "--exact", "gate::tests::non_windows_fixed_target_fixture_child",
            "--nocapture", "--test-threads=1",
        ]);
        // Only fixture-local target/marker inputs are changed. The normal
        // selected toolchain and compiler environment remain inherited.
        for (key, _) in std::env::vars() {
            if ["CARGO_TARGET_DIR", "CARGO_BUILD_TARGET_DIR", LOCK_HELD, HELD_TARGET,
                INJECTED_TARGET, FORWARDED_TARGET, "CODEX_JOBS_CACHE_LOCK_HELD"]
                .iter().any(|name| key.eq_ignore_ascii_case(name))
            {
                command.env_remove(key);
            }
        }
        command.env("CODEX_JOBS_REAL_CARGO", cargo);
        command.env("CC_GATE_PLATFORM_FIXTURE", mode);
        if nested {
            command.env(LOCK_HELD, "1");
        }
        if cache_held {
            command.env("CODEX_JOBS_CACHE_LOCK_HELD", "1");
        }
        match mode {
            "ordinary-scope-exit-relative" => {
                command.env("CARGO_TARGET_DIR", "D:/CC/BuildCache/cargo-shared/../cargo-shared-peer");
            }
            "ordinary-scope-exit-absolute" => {
                command.env("CARGO_TARGET_DIR",
                    root.path().join("D:/CC/BuildCache/cargo-shared/../cargo-shared-peer"));
            }
            "ordinary-scope-exit-alias" => {
                command.env("CARGO_TARGET_DIR", root.path().join("alias/../cargo-shared-peer"));
            }
            "ordinary-cache-prefix" => {
                command.env("CARGO_TARGET_DIR", root.path().join("D:/CC/BuildCache/cargo-shared-peer"));
            }
            "shared-cache-child-env" => {
                command.env("CARGO_TARGET_DIR", root.path().join("D:/CC/BuildCache/cargo-shared/debug"));
            }
            "ordinary-cache-prefix-alias" | "ordinary-symlink" | "shared-symlink-env" => {
                command.env("CARGO_TARGET_DIR", root.path().join("alias"));
            }
            "ordinary-readonly" | "ordinary-legacy" | "ordinary-build" => {
                command.env("CARGO_TARGET_DIR", &ordinary);
            }
            "ordinary-pivot-readonly" => {
                command.env("CARGO_TARGET_DIR", root.path().join("pivot/../ordinary-target"));
            }
            "ordinary-override" | "shared-env" => {
                command.env("CARGO_TARGET_DIR", crate::cache::SHARED);
            }
            "ordinary-ignored-forwarded" => {
                command.env("CARGO_TARGET_DIR", &ordinary);
                command.env(FORWARDED_TARGET, crate::cache::SHARED);
            }
            "ordinary-build-ignored-forwarded" => {
                command.env("CARGO_BUILD_TARGET_DIR", &ordinary);
                command.env(FORWARDED_TARGET, crate::cache::SHARED);
            }
            "ordinary-injected-legacy" | "ordinary-injected-readonly" => {
                command.env("CARGO_TARGET_DIR", &ordinary);
                command.env(INJECTED_TARGET, &ordinary);
                command.env("CARGO_BUILD_TARGET_DIR", crate::cache::SHARED);
            }
            "shared-build-env" => {
                command.env("CARGO_BUILD_TARGET_DIR", crate::cache::SHARED);
            }
            "shared-forwarded" => {
                command.env(FORWARDED_TARGET, crate::cache::SHARED);
                command.env(HELD_TARGET, &ordinary);
            }
            "shared-metadata" => {
                command.env(HELD_TARGET, &ordinary);
            }
            "shared-pivot-readonly" => {
                command.env("CARGO_TARGET_DIR", root.path().join("pivot/../D:/CC/BuildCache/cargo-shared"));
            }
            _ => {}
        }
        command.current_dir(root.path());
        let collection = gate_fixture_actual_caller(collect_gate_fixture_with_faults(
            &mut command, &root,
            std::time::Instant::now() + std::time::Duration::from_secs(20), faults,
        ), mode, cache_held, nested);
        let status = collection.status;
        let stdout = collection.output();
        assert!(status.success(), "owned gate fixture failed: {mode}/{cache_held}/{nested}");
        assert!(!stdout.overflow, "owned Cargo fixture output exceeded bound: {mode}");
        assert!(stdout.eof, "owned Cargo fixture stdout not complete: {mode}");
        assert!(!root.path().join("cargo-stdout").exists());
        let mut report: Value = serde_json::from_slice(
            &std::fs::read(root.path().join("result.json")).unwrap(),
        ).unwrap();
        let output = std::str::from_utf8(&stdout.bytes).unwrap();
        let metadata = output.lines().find_map(|line| {
            let start = line.find('{')?;
            let value: Value = serde_json::from_str(&line[start..]).ok()?;
            (value["packages"].as_array()?.len() == 1
                && value["packages"][0]["name"] == "owned_gate_fixture")
                .then_some(value)
        });
        assert_fixture_data_not_executable(root.path());
        report["observed_cargo_metadata"] = json!(metadata.is_some());
        report["observed_cargo_help"] = json!(output.lines().any(|line| {
            line.contains("Usage:") && line.contains("cargo build")
        }));
        // root/result.json remains owned until the consumer finished all reads.
        let _ = collection.consume();
        report
    }

    #[cfg(target_os = "linux")]
    fn gate_fixture_command(mode: &str, root: &Path) -> std::process::Command {
        let mut command = std::process::Command::new(std::env::current_exe().unwrap());
        command.current_dir(root).args([
            "--exact", "gate::tests::non_windows_fixed_target_fixture_child",
            "--nocapture", "--test-threads=1",
        ]).env("CC_GATE_PLATFORM_FIXTURE", mode);
        command
    }

    #[cfg(target_os = "linux")]
    fn await_gate_fixture_binding(owner: &mut OwnedGateFixture) {
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(20);
        loop {
            let state = owner.state.as_mut().unwrap();
            if state.group_fd.is_none() {
                state.group_fd = gate_fixture_receive_pidfd(&state.binding, state.child.id()).unwrap();
            }
            if state.group_fd.is_some() { return; }
            assert!(std::time::Instant::now() < deadline, "owned pidfd binding deadline");
            std::thread::sleep(std::time::Duration::from_millis(5));
        }
    }

    #[cfg(target_os = "linux")]
    fn await_gate_fixture_unreaped_exit(owner: &mut OwnedGateFixture) {
        await_gate_fixture_binding(owner);
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(20);
        let state = owner.state.as_ref().unwrap();
        loop {
            if gate_fixture_pidfd_wait(state.group_fd.as_ref().unwrap(), false).unwrap().is_some() { break; }
            assert!(std::time::Instant::now() < deadline, "owned pidfd exit deadline");
            std::thread::sleep(std::time::Duration::from_millis(5));
        }
        assert!(state.status.is_none());
        assert_eq!(state.wait_calls, 0);
    }

    #[cfg(target_os = "linux")]
    fn assert_gate_fixture_signal_order(facts: &GateFixtureFacts) {
        let signal = facts.events.iter().position(|event|
            matches!(event, GateFixtureEvent::GroupSignalled | GateFixtureEvent::GroupAbsent)).unwrap();
        let wait = facts.events.iter().position(|event| *event == GateFixtureEvent::WaitAttempted).unwrap();
        let reap = facts.events.iter().position(|event| *event == GateFixtureEvent::Reaped).unwrap();
        assert!(signal < wait && wait < reap, "pidfd group/Wait ordering: {facts:?}");
        assert!(!facts.events[reap + 1..].iter().any(|event|
            matches!(event, GateFixtureEvent::GroupSignalled | GateFixtureEvent::GroupAbsent)));
        assert!(facts.root_reaped && facts.stdout_eof && facts.stdout_complete && facts.group_fd_bound);
        assert_eq!(facts.group_observation, GateFixtureGroupObservation::Absent);
    }

    #[cfg(target_os = "linux")]
    fn assert_gate_fixture_collector_lifetimes() {
        let cleanup = std::time::Duration::from_secs(5);
        // Native root-exit and descendant ownership. A different real owned group
        // stays alive through every target signal; no forced PID recycling claim.
        for mode in ["stdout-finite", "stdout-leftover"] {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let other_root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let mut other = start_gate_fixture(
                &mut gate_fixture_command("stdout-silent", other_root.path()), &other_root).unwrap();
            await_gate_fixture_binding(&mut other);
            let other_pid = other.state.as_ref().unwrap().child.id();
            let mut owner = start_gate_fixture(&mut gate_fixture_command(mode, root.path()), &root).unwrap();
            let pid = owner.state.as_ref().unwrap().child.id();
            await_gate_fixture_unreaped_exit(&mut owner);
            let collected = advance_gate_fixture(owner,
                std::time::Instant::now() + std::time::Duration::from_secs(20),
                cleanup, false, GateFixtureFaults::default()).unwrap();
            assert!(collected.status.success());
            assert_eq!(collected.facts.pid, pid);
            assert_gate_fixture_signal_order(&collected.facts);
            let other_state = other.state.as_ref().unwrap();
            assert_ne!(pid, other_pid);
            assert!(gate_fixture_pidfd_wait(other_state.group_fd.as_ref().unwrap(), false).unwrap().is_none());
            if mode == "stdout-leftover" {
                let descendant: u32 = std::fs::read_to_string(root.path().join("descendant-id"))
                    .unwrap().parse().unwrap();
                assert_ne!(descendant, pid);
            }
            let _ = collected.consume();
            let other_done = advance_gate_fixture(other, std::time::Instant::now(),
                cleanup, true, GateFixtureFaults::default()).unwrap();
            assert_gate_fixture_signal_order(&other_done.facts);
            let _ = other_done.consume();
        }

        // Inject only failures/withheld observations; no seam can assert success.
        for name in ["setup", "peek", "signal", "wait-error", "wait-pending", "read",
            "eof", "group-query", "pid-reuse-model", "unproven-binding",
            "auto-reap-observation", "external-reap-observation"]
        {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let live = matches!(name, "setup" | "peek" | "signal" | "read" | "unproven-binding"
                | "auto-reap-observation" | "external-reap-observation");
            let mut owner = start_gate_fixture(&mut gate_fixture_command(
                if live { "stdout-silent" } else { "stdout-finite" }, root.path()), &root).unwrap();
            await_gate_fixture_binding(&mut owner);
            if !live { await_gate_fixture_unreaped_exit(&mut owner); }
            let original = owner.state.as_ref().unwrap().facts();
            let faults = GateFixtureFaults {
                setup_error: name == "setup", peek_error: name == "peek",
                signal_error: name == "signal", wait_error: name == "wait-error",
                wait_pending: name == "wait-pending", read_error: name == "read",
                hide_eof: name == "eof", group_query_error: name == "group-query",
                reused_group_model: name == "pid-reuse-model",
                binding_unproven: name == "unproven-binding",
                external_reap_observation: matches!(name, "auto-reap-observation" | "external-reap-observation"),
                ..GateFixtureFaults::default()
            };
            let started = std::time::Instant::now();
            let error = advance_gate_fixture(owner,
                std::time::Instant::now() + std::time::Duration::from_secs(20),
                std::time::Duration::from_millis(25), true, faults).unwrap_err();
            assert!(started.elapsed() < cleanup);
            let ticket = error.ticket.unwrap();
            let pending = retained_gate_fixture_facts(ticket).unwrap().unwrap();
            assert_eq!(pending.pid, original.pid);
            assert_eq!(pending.stdout_fd, original.stdout_fd);
            assert!(pending.root_retained);
            if matches!(name, "auto-reap-observation" | "external-reap-observation") {
                assert_eq!(error.stage, GateFixtureStage::RootObserve);
                assert!(pending.group_fd_bound);
            }
            if live {
                assert_eq!(pending.signal_calls, 0);
                assert_eq!(pending.wait_calls, 0);
                assert!(!pending.root_reaped);
            }
            if matches!(name, "eof" | "group-query" | "pid-reuse-model") {
                assert!(pending.root_reaped);
                let mut held = take_retained_gate_fixture(ticket).unwrap();
                let state = held.state.as_mut().unwrap();
                let before = state.signal_calls;
                assert!(gate_fixture_signal_owned_group(state, &GateFixtureFaults::default()).is_err());
                assert_eq!(state.signal_calls, before);
                drop(held);
            }
            let primary = (error.kind(), error.stage);
            let done = drain_retained_gate_fixture(ticket, cleanup, GateFixtureFaults::default()).unwrap();
            assert_gate_fixture_signal_order(&done.facts);
            assert_eq!(done.facts.pid, original.pid);
            assert_eq!(done.facts.stdout_fd, original.stdout_fd);
            assert_eq!((error.kind(), error.stage), primary);
            let _ = done.consume();
            assert!(retained_gate_fixture_facts(ticket).unwrap().is_none());
        }

        // Deadline is the first primary; stop, Wait and subsequent drain errors
        // are independent cleanup facts. Both failed and successful retries keep it.
        for fault in ["signal", "wait"] {
            for second_pending in [false, true] {
                let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
                let mut owner = start_gate_fixture(
                    &mut gate_fixture_command("stdout-silent", root.path()), &root).unwrap();
                await_gate_fixture_binding(&mut owner);
                let faults = GateFixtureFaults {
                    signal_error: fault == "signal", wait_error: fault == "wait",
                    ..GateFixtureFaults::default()
                };
                let error = advance_gate_fixture(owner, std::time::Instant::now(),
                    cleanup, false, faults).unwrap_err();
                assert_eq!(error.kind(), io::ErrorKind::TimedOut);
                assert_eq!(error.stage, GateFixtureStage::ExecutionDeadline);
                let expected_cleanup = if fault == "signal" {
                    GateFixtureCleanup::Pending { kind: io::ErrorKind::PermissionDenied,
                        stage: GateFixtureStage::GroupSignal }
                } else {
                    GateFixtureCleanup::Pending { kind: io::ErrorKind::Other,
                        stage: GateFixtureStage::RootWait }
                };
                assert_eq!(error.cleanup, Some(expected_cleanup));
                let ticket = error.ticket.unwrap();
                if second_pending {
                    let pending = drain_retained_gate_fixture(ticket,
                        std::time::Duration::from_millis(25),
                        GateFixtureFaults { signal_error: fault == "signal",
                            wait_error: fault == "wait", ..GateFixtureFaults::default() }).unwrap_err();
                    assert_eq!(pending.kind(), io::ErrorKind::TimedOut);
                    assert_eq!(pending.stage, GateFixtureStage::ExecutionDeadline);
                    assert_eq!(pending.cleanup, Some(expected_cleanup));
                }
                let done = drain_retained_gate_fixture(ticket, cleanup, GateFixtureFaults::default()).unwrap();
                assert_eq!(done.primary, Some(GateFixturePrimary {
                    kind: io::ErrorKind::TimedOut, stage: GateFixtureStage::ExecutionDeadline }));
                assert!(done.facts.timed_out);
                let _ = done.consume();
            }
        }

        // Return/panic twice with a pending child. No stack-local root owner
        // survives, yet the same ticket still owns cwd/input/result namespace.
        for unwind in [false, true] {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let location = root.path().to_path_buf();
            std::fs::write(location.join("retained-sentinel"), b"same owned root").unwrap();
            let mut owner = start_gate_fixture(
                &mut gate_fixture_command("stdout-silent", root.path()), &root).unwrap();
            await_gate_fixture_binding(&mut owner);
            let original = owner.state.as_ref().unwrap().facts();
            let error = advance_gate_fixture(owner,
                std::time::Instant::now() + std::time::Duration::from_secs(20),
                cleanup, true, GateFixtureFaults { wait_pending: true, ..GateFixtureFaults::default() }).unwrap_err();
            let ticket = error.ticket.unwrap();
            drop(root);
            let pending = drain_retained_gate_fixture(ticket, std::time::Duration::from_millis(25),
                GateFixtureFaults { wait_pending: true, ..GateFixtureFaults::default() }).unwrap_err();
            assert_eq!(pending.ticket, Some(ticket));
            if unwind {
                let panic = std::panic::catch_unwind(std::panic::AssertUnwindSafe(||
                    gate_fixture_actual_caller(Err(pending), "retained-root", false, false)));
                assert!(panic.is_err());
            }
            assert_eq!(std::fs::read(location.join("retained-sentinel")).unwrap(), b"same owned root");
            let facts = retained_gate_fixture_facts(ticket).unwrap().unwrap();
            assert_eq!(facts.pid, original.pid);
            assert_eq!(facts.stdout_fd, original.stdout_fd);
            let done = drain_retained_gate_fixture(ticket, cleanup, GateFixtureFaults::default()).unwrap();
            assert!(location.exists()); // result not consumed
            let _ = done.consume();
            assert!(!location.exists()); // last root owner released only now
        }

        // A dropped or panicked unconsumed collection must recover the same
        // nonempty payload, EOF/overflow and root, without another native spawn.
        for (mode, unwind, overflow) in [
            ("stdout-finite", false, false),
            ("stdout-finite", true, false),
            ("stdout-overflow", true, true),
        ] {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let location = root.path().to_path_buf();
            std::fs::write(location.join("payload-sentinel"), b"same payload root").unwrap();
            let owner = start_gate_fixture(&mut gate_fixture_command(mode, root.path()), &root).unwrap();
            let ticket = owner.ticket;
            let deadline = std::time::Instant::now() + std::time::Duration::from_secs(20);
            let collection = advance_gate_fixture(owner, deadline, cleanup, false, GateFixtureFaults {
                // Overflow is the earlier primary; the final late clock must not replace it.
                completion_clock: overflow.then_some(deadline + std::time::Duration::from_nanos(1)),
                ..GateFixtureFaults::default()
            }).unwrap();
            let original = collection.facts.clone();
            let expected = collection.output().bytes.clone();
            assert!(!expected.is_empty());
            assert!(collection.output().eof);
            assert_eq!(collection.output().overflow, overflow);
            if overflow {
                assert_eq!(expected.len(), GATE_FIXTURE_STDOUT_LIMIT);
                assert_eq!(collection.primary, Some(GateFixturePrimary {
                    kind: io::ErrorKind::InvalidData, stage: GateFixtureStage::OutputBound }));
                assert!(collection.facts.timed_out && collection.facts.completion_late);
            }
            let spawns = GATE_FIXTURE_SPAWNS.load(std::sync::atomic::Ordering::Relaxed);
            drop(root);
            if unwind {
                let panic = std::panic::catch_unwind(std::panic::AssertUnwindSafe(move || {
                    let _still_owned = collection;
                    panic!("owned payload consumer fault");
                }));
                assert!(panic.is_err());
            } else {
                drop(collection);
            }
            assert_eq!(std::fs::read(location.join("payload-sentinel")).unwrap(), b"same payload root");
            let retained = retained_gate_fixture_facts(ticket).unwrap().unwrap();
            assert_eq!(retained.pid, original.pid);
            assert_eq!(retained.stdout_fd, original.stdout_fd);
            // EOF is already a native observation. Re-drain must not read the pipe again.
            let recovered = drain_retained_gate_fixture(ticket, cleanup,
                GateFixtureFaults { read_error: true, ..GateFixtureFaults::default() }).unwrap();
            assert_eq!(recovered.owner.as_ref().unwrap().ticket, ticket);
            assert_eq!(recovered.facts.pid, original.pid);
            assert_eq!(recovered.facts.stdout_fd, original.stdout_fd);
            assert_eq!(recovered.facts.signal_calls, original.signal_calls);
            assert_eq!(recovered.facts.wait_calls, original.wait_calls);
            assert_eq!(recovered.facts.completion_checked, original.completion_checked);
            assert_eq!(recovered.facts.completion_late, original.completion_late);
            assert_eq!(recovered.output().bytes, expected);
            assert!(recovered.output().eof);
            assert_eq!(recovered.output().overflow, overflow);
            assert_eq!(GATE_FIXTURE_SPAWNS.load(std::sync::atomic::Ordering::Relaxed), spawns);
            assert!(location.exists()); // output/root still unconsumed
            let mut once = Some(recovered);
            let (_, output, _) = once.take().unwrap().consume();
            assert!(once.take().is_none()); // no second owner transfer
            assert_eq!(output.bytes, expected);
            assert!(output.eof);
            assert_eq!(output.overflow, overflow);
            assert!(!location.exists());
            assert!(take_retained_gate_fixture(ticket).is_err()); // consumed ticket cannot replay
            assert_eq!(GATE_FIXTURE_SPAWNS.load(std::sync::atomic::Ordering::Relaxed), spawns);
        }

        // The concrete completion clock can only delay a real monotonic reading.
        // Before-deadline and after-deadline observations use the actual collector.
        for late in [false, true] {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let deadline = std::time::Instant::now() + std::time::Duration::from_secs(20);
            assert!(std::time::Instant::now() < deadline);
            let injected = if late {
                deadline + std::time::Duration::from_nanos(1)
            } else {
                deadline - std::time::Duration::from_nanos(1)
            };
            let result = collect_gate_fixture_with_faults(
                &mut gate_fixture_command("stdout-finite", root.path()), &root, deadline,
                GateFixtureFaults { completion_clock: Some(injected), ..GateFixtureFaults::default() },
            );
            if late {
                let error = result.unwrap_err();
                assert_eq!(error.kind(), io::ErrorKind::TimedOut);
                assert_eq!(error.stage, GateFixtureStage::ExecutionDeadline);
                assert_eq!(error.cleanup, Some(GateFixtureCleanup::Collected));
                assert!(error.ticket.is_none());
                let facts = error.facts.as_ref().unwrap();
                assert!(facts.completion_checked && facts.completion_late && facts.timed_out);
                assert!(facts.root_reaped && facts.stdout_complete);
                assert_eq!(facts.group_observation, GateFixtureGroupObservation::Absent);
                let panic = std::panic::catch_unwind(std::panic::AssertUnwindSafe(||
                    gate_fixture_actual_caller(Err(error), "completion-clock", false, false))).unwrap_err();
                let text = panic.downcast_ref::<String>().map(String::as_str)
                    .or_else(|| panic.downcast_ref::<&str>().copied()).unwrap();
                assert!(text.len() < 4608);
                assert!(text.contains("\"execution_deadline\""));
                assert!(text.contains("\"completion_checked\":true"));
                assert!(text.contains("\"completion_deadline_elapsed\":true"));
            } else {
                let collected = result.unwrap();
                assert!(collected.primary.is_none());
                assert!(collected.facts.completion_checked);
                assert!(!collected.facts.completion_late && !collected.facts.timed_out);
                assert!(!collected.output().bytes.is_empty());
                let _ = collected.consume();
            }
        }

        // A one-shot, bounded rendezvous occurs after the real entry check.
        // The released concrete clock delays only the later error observation.
        // These are source controls, not an experiment in kernel failure timing.
        for (stage, late) in [("setup", true), ("read", true), ("read", false)] {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let mut owner = start_gate_fixture(
                &mut gate_fixture_command("stdout-silent", root.path()), &root).unwrap();
            await_gate_fixture_binding(&mut owner);
            let original = owner.state.as_ref().unwrap().facts();
            let ticket = owner.ticket;
            let spawns = GATE_FIXTURE_SPAWNS.load(std::sync::atomic::Ordering::Relaxed);
            let deadline = std::time::Instant::now() + std::time::Duration::from_secs(20);
            let reported_at = if late {
                deadline + std::time::Duration::from_nanos(1)
            } else {
                deadline - std::time::Duration::from_nanos(1)
            };
            let expected_stage = if stage == "setup" {
                GateFixtureStage::StdoutSetup
            } else {
                GateFixtureStage::StdoutRead
            };
            let (entered_tx, entered_rx) = std::sync::mpsc::sync_channel::<std::time::Instant>(1);
            let (release_tx, release_rx) = std::sync::mpsc::sync_channel::<std::time::Instant>(1);
            let synchronizer = std::thread::spawn(move || -> Result<(), &'static str> {
                let entered = entered_rx.recv_timeout(cleanup).map_err(|_| "entry barrier unavailable")?;
                if entered >= deadline { return Err("entry was already late"); }
                release_tx.try_send(reported_at).map_err(|_| "error clock release unavailable")
            });
            let attempt = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                advance_gate_fixture(owner, deadline, cleanup, false, GateFixtureFaults {
                    setup_error: stage == "setup", read_error: stage == "read",
                    error_barrier: Some(GateFixtureErrorBarrier {
                        stage: expected_stage, entered: entered_tx, release: release_rx,
                    }),
                    ..GateFixtureFaults::default()
                })
            }));
            // Join before any assertion; retain the ticket across assertion failure.
            let synchronized = synchronizer.join();
            let verified = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                assert_eq!(synchronized.unwrap(), Ok(()));
                let error = attempt.unwrap().unwrap_err();
                assert_eq!(error.kind(), io::ErrorKind::Other);
                assert_eq!(error.stage, expected_stage);
                assert_eq!(error.cleanup, Some(GateFixtureCleanup::Pending {
                    kind: io::ErrorKind::Other, stage: expected_stage }));
                let facts = error.facts.as_ref().unwrap();
                assert!(facts.error_checked);
                assert_eq!(facts.error_late, late);
                assert_eq!(facts.timed_out, late);
                assert!(!facts.completion_checked);
                assert_eq!(facts.pid, original.pid);
                assert_eq!(facts.stdout_fd, original.stdout_fd);
                assert_eq!(error.ticket, Some(ticket));

                // A later force-stop error uses cleanup's budget, not a new
                // execution deadline, even with a deliberately future error clock.
                let pending = drain_retained_gate_fixture(ticket,
                    std::time::Duration::from_millis(25), GateFixtureFaults {
                        wait_pending: true,
                        error_clock: Some(deadline + std::time::Duration::from_secs(1)),
                        ..GateFixtureFaults::default()
                    }).unwrap_err();
                assert_eq!(pending.kind(), io::ErrorKind::Other);
                assert_eq!(pending.stage, expected_stage);
                assert_eq!(pending.ticket, Some(ticket));
                let facts = pending.facts.as_ref().unwrap();
                assert!(facts.error_checked);
                assert_eq!(facts.error_late, late);
                assert_eq!(facts.timed_out, late);
                assert!(!facts.completion_checked);
                let panic = std::panic::catch_unwind(std::panic::AssertUnwindSafe(||
                    gate_fixture_actual_caller(Err(pending), "error-boundary", false, false))).unwrap_err();
                let text = panic.downcast_ref::<String>().map(String::as_str)
                    .or_else(|| panic.downcast_ref::<&str>().copied()).unwrap();
                assert!(text.len() < 4608);
                let json_start = text.find('{').unwrap();
                let projected: Value = serde_json::from_str(&text[json_start..]).unwrap();
                assert_eq!(projected["primary"]["kind"], "unavailable");
                assert_eq!(projected["primary"]["stage"],
                    if stage == "setup" { "stdout_setup" } else { "stdout_read" });
                assert_eq!(projected["cleanup"]["state"], "pending");
                assert_eq!(projected["latest"]["execution_error_checked"], true);
                assert_eq!(projected["latest"]["execution_error_deadline_elapsed"], late);
                assert_eq!(projected["latest"]["execution_timeout"], late);
                assert_eq!(projected["latest"]["completion_checked"], false);
                assert_eq!(projected["latest"]["root_retained"], true);
                assert!(!text.contains(&root.path().to_string_lossy().to_string()));

            }));

            // Recover this same native owner before propagating a failed counter.
            let recovered = drain_retained_gate_fixture(ticket, cleanup, GateFixtureFaults::default()).unwrap();
            let primary = recovered.primary;
            let facts = recovered.facts.clone();
            let _ = recovered.consume();
            if let Err(panic) = verified { std::panic::resume_unwind(panic); }
            assert_eq!(primary, Some(GateFixturePrimary {
                kind: io::ErrorKind::Other, stage: expected_stage }));
            assert!(facts.error_checked);
            assert_eq!(facts.error_late, late);
            assert_eq!(facts.timed_out, late);
            assert_eq!(facts.pid, original.pid);
            assert_eq!(facts.stdout_fd, original.stdout_fd);
            assert!(retained_gate_fixture_facts(ticket).unwrap().is_none());
            assert_eq!(GATE_FIXTURE_SPAWNS.load(std::sync::atomic::Ordering::Relaxed), spawns);
        }

        // Actual caller projection contains the compound outcome and latest
        // native facts, bounded to4KiB and without paths/free error strings.
        {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let mut owner = start_gate_fixture(
                &mut gate_fixture_command("stdout-silent", root.path()), &root).unwrap();
            await_gate_fixture_binding(&mut owner);
            let error = advance_gate_fixture(owner, std::time::Instant::now(), cleanup,
                false, GateFixtureFaults { signal_error: true, ..GateFixtureFaults::default() }).unwrap_err();
            let ticket = error.ticket.unwrap();
            let panic = std::panic::catch_unwind(std::panic::AssertUnwindSafe(||
                gate_fixture_actual_caller(Err(error), "projection", false, false))).unwrap_err();
            let text = panic.downcast_ref::<String>().map(String::as_str)
                .or_else(|| panic.downcast_ref::<&str>().copied()).unwrap();
            assert!(text.len() < 4608);
            assert!(text.contains("\"primary\"") && text.contains("\"execution_deadline\""));
            assert!(text.contains("\"cleanup\"") && text.contains("\"group_signal\""));
            assert!(text.contains("\"latest\"") && text.contains("\"root_retained\":true"));
            assert!(!text.contains(&root.path().to_string_lossy().to_string()));
            let done = drain_retained_gate_fixture(ticket, cleanup, GateFixtureFaults::default()).unwrap();
            let _ = done.consume();
        }
        for injected in ["setup", "wait", "group"] {
            let faults = GateFixtureFaults { setup_error: injected == "setup",
                wait_error: injected == "wait", group_query_error: injected == "group",
                ..GateFixtureFaults::default() };
            let panic = std::panic::catch_unwind(std::panic::AssertUnwindSafe(||
                inspect_gate_platform_fixture_with_faults("ordinary-readonly", false, false, faults))).unwrap_err();
            let text = panic.downcast_ref::<String>().map(String::as_str)
                .or_else(|| panic.downcast_ref::<&str>().copied()).unwrap();
            assert!(text.len() < 4608);
            assert!(text.contains("\"primary\"") && text.contains("\"cleanup\""));
            assert!(text.contains("\"state\":\"collected\"") && text.contains("\"latest\""));
            assert!(text.contains("\"root_wait\":true") && text.contains("\"stdout_complete\":true"));
            assert!(!text.contains("retained-sentinel") && !text.contains("D:/"));
        }
        assert_eq!(retained_gate_fixture_count().unwrap(), 0);
    }

    #[cfg(target_os = "linux")]
    fn assert_gate_fixture_stdout_is_bounded_while_running() {
        for (size, overflow) in [
            (0, false),
            (GATE_FIXTURE_STDOUT_LIMIT, false),
            (GATE_FIXTURE_STDOUT_LIMIT + 1, true),
        ] {
            let mut input = std::io::Cursor::new(vec![b'x'; size]);
            let mut output = GateFixtureOutput::default();
            drain_gate_fixture_output(&mut input, &mut output).unwrap();
            assert_eq!(output.bytes.len(), size.min(GATE_FIXTURE_STDOUT_LIMIT));
            assert_eq!(output.overflow, overflow);
            assert!(output.eof);
        }
        for mode in ["stdout-finite", "stdout-overflow"] {
            let root = std::sync::Arc::new(tempfile::tempdir().unwrap());
            let mut command = std::process::Command::new(std::env::current_exe().unwrap());
            command.current_dir(root.path()).args([
                "--exact", "gate::tests::non_windows_fixed_target_fixture_child",
                "--nocapture", "--test-threads=1",
            ]).env("CC_GATE_PLATFORM_FIXTURE", mode);
            // The raw controller result is inspected for the deliberate overflow.
            // The normal actual caller rejects that primary as a failure.
            let collection = if mode == "stdout-finite" {
                collect_gate_fixture(&mut command, &root,
                    std::time::Instant::now() + std::time::Duration::from_secs(20)).unwrap()
            } else {
                let owner = start_gate_fixture(&mut command, &root).unwrap();
                advance_gate_fixture(owner,
                    std::time::Instant::now() + std::time::Duration::from_secs(20),
                    std::time::Duration::from_secs(5), false, GateFixtureFaults::default()).unwrap()
            };
            let status = collection.status;
            let output = collection.output();
            assert!(output.eof, "collector returned without EOF: {mode}");
            assert!(output.bytes.len() <= GATE_FIXTURE_STDOUT_LIMIT);
            assert_eq!(output.overflow, mode == "stdout-overflow");
            if mode == "stdout-finite" {
                assert!(status.success());
                assert!(std::str::from_utf8(&output.bytes).unwrap().contains("owned bounded stdout fixture"));
            } else {
                assert!(!status.success(), "overflow child was not stopped");
                assert_eq!(output.bytes.len(), GATE_FIXTURE_STDOUT_LIMIT);
                assert_eq!(collection.primary, Some(GateFixturePrimary {
                    kind: io::ErrorKind::InvalidData, stage: GateFixtureStage::OutputBound }));
            }
            assert_eq!(std::fs::read_dir(root.path()).unwrap().count(), 0);
            let _ = collection.consume();
        }
    }

    #[cfg(target_os = "linux")]
    fn assert_fixed_target_rejected(report: &Value, reason: &str) {
        assert_eq!(report["error_kind"], reason, "fixture: {report}");
        assert_eq!(report["code"], Value::Null);
        assert_eq!(report["windows_lock_boundary"], false);
        assert_eq!(report["observed_cargo_help"], false);
        assert_eq!(report["admissions"], 0);
        assert_eq!(report["locks_created"], 0);
        assert_eq!(report["fixed_target_created"], false);
        assert_eq!(report["fixed_fixture_unchanged"], true);
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn non_windows_public_gate_rejects_fixed_cache_before_io_and_execution() {
        assert_gate_fixture_collector_lifetimes();
        assert_gate_fixture_stdout_is_bounded_while_running();
        for mode in ["shared-cache-child", "shared-cache-child-absolute",
            "shared-cache-child-alias", "shared-cache-child-env"]
        {
            for cache_held in [false, true] {
                for nested in [false, true] {
                    let report = inspect_gate_platform_fixture(mode, cache_held, nested);
                    assert_eq!(report["build_like"], true);
                    assert_fixed_target_rejected(&report, "Unsupported");
                    assert_eq!(report["observed_cargo_metadata"], false);
                }
            }
        }
        for mode in ["ordinary-cache-prefix", "ordinary-cache-prefix-alias",
            "ordinary-scope-exit-relative", "ordinary-scope-exit-absolute", "ordinary-scope-exit-alias"] {
            for cache_held in [false, true] {
                for nested in [false, true] {
                    let report = inspect_gate_platform_fixture(mode, cache_held, nested);
                    assert_eq!(report["code"], 0, "sibling fixture: {report}");
                    assert_eq!(report["error_kind"], Value::Null);
                    assert_eq!(report["build_like"], false);
                    assert_eq!(report["observed_cargo_metadata"], true);
                    assert_eq!(report["admissions"], 0);
                    assert_eq!(report["locks_created"], 0);
                    assert_eq!(report["fixed_target_created"], false);
                    assert_eq!(report["fixed_fixture_unchanged"], true);
                }
            }
        }
        for mode in ["shared-symlink", "shared-symlink-env", "unknown-symlink", "changed-symlink"] {
            for cache_held in [false, true] {
                for nested in [false, true] {
                    let report = inspect_gate_platform_fixture(mode, cache_held, nested);
                    assert_eq!(report["build_like"], true);
                    assert_fixed_target_rejected(&report,
                        if mode == "unknown-symlink" { "InvalidInput" } else { "Unsupported" });
                    assert_eq!(report["observed_cargo_metadata"], false);
                    assert_eq!(report["ordinary_target_created"], false);
                    assert_eq!(report["ordinary_target_existed_before"], mode == "changed-symlink");
                }
            }
        }
        for cache_held in [false, true] {
            for nested in [false, true] {
                let report = inspect_gate_platform_fixture("ordinary-symlink", cache_held, nested);
                assert_eq!(report["build_like"], false);
                assert_eq!(report["code"], 0, "fixture: {report}");
                assert_eq!(report["error_kind"], Value::Null);
                assert_eq!(report["observed_cargo_metadata"], true);
                assert_eq!(report["observed_cargo_help"], false);
                assert_eq!(report["admissions"], 0);
                assert_eq!(report["locks_created"], 0);
                assert_eq!(report["fixed_target_created"], false);
            }
        }
        for mode in ["shared-args", "shared-env", "shared-build-env", "shared-resolved",
            "shared-leading-dot", "shared-leading-dot-backslash",
            "shared-pivot-relative", "shared-pivot-absolute",
            "shared-cwd-parent"]
        {
            for cache_held in [false, true] {
                for nested in [false, true] {
                    let report = inspect_gate_platform_fixture(mode, cache_held, nested);
                    assert_eq!(report["mode"], mode, "markers: {cache_held}/{nested}");
                    assert_fixed_target_rejected(&report, "Unsupported");
                }
            }
        }
        for mode in ["shared-forwarded", "shared-alias", "shared-pivot-readonly"] {
            for cache_held in [false, true] {
                let report = inspect_gate_platform_fixture(mode, cache_held, true);
                assert_eq!(report["build_like"], mode == "shared-forwarded", "mode: {mode}");
                assert_fixed_target_rejected(&report, "Unsupported");
            }
        }
        for cache_held in [false, true] {
            let report = inspect_gate_platform_fixture("shared-pivot-link", cache_held, true);
            assert_fixed_target_rejected(&report, "InvalidInput");
            let report = inspect_gate_platform_fixture("shared-metadata", cache_held, false);
            assert_eq!(report["build_like"], true);
            assert_fixed_target_rejected(&report, "Unsupported");
            assert_eq!(report["observed_cargo_metadata"], false);
        }

        for mode in ["ordinary-readonly", "ordinary-legacy"] {
            for cache_held in [false, true] {
                let report = inspect_gate_platform_fixture(
                    mode, cache_held, mode == "ordinary-legacy",
                );
                assert_eq!(report["code"], 0);
                assert_eq!(report["error_kind"], Value::Null);
                assert_eq!(report["admissions"], 0);
                assert_eq!(report["locks_created"], 0);
                assert_eq!(report["fixed_target_created"], false);
                assert_eq!(report["observed_cargo_metadata"], mode == "ordinary-readonly");
                assert_eq!(report["observed_cargo_help"], mode == "ordinary-legacy");
            }
        }
        for mode in ["ordinary-override", "ordinary-ignored-forwarded",
            "ordinary-build-ignored-forwarded", "ordinary-injected-legacy",
            "ordinary-pivot-readonly"]
        {
            let report = inspect_gate_platform_fixture(mode, true, true);
            assert_eq!(report["code"], 0);
            assert_eq!(report["admissions"], 0);
            assert_eq!(report["locks_created"], 0);
            assert_eq!(report["fixed_target_created"], false);
            assert_eq!(report["observed_cargo_metadata"], mode == "ordinary-pivot-readonly");
            assert_eq!(report["observed_cargo_help"], mode != "ordinary-pivot-readonly");
        }

        for cache_held in [false, true] {
            let report = inspect_gate_platform_fixture("ordinary-injected-readonly", cache_held, false);
            assert_eq!(report["code"], 0);
            assert_eq!(report["observed_cargo_metadata"], true);
            assert_eq!(report["observed_cargo_help"], false);
            assert_eq!(report["admissions"], 0);
            assert_eq!(report["locks_created"], 0);
            assert_eq!(report["fixed_target_created"], false);
        }

        // The existing non-Windows lock limitation is a distinct result, not
        // successful machine admission or failure of the fixed-target filter.
        for cache_held in [false, true] {
            let report = inspect_gate_platform_fixture("ordinary-build", cache_held, false);
            assert_eq!(report["build_like"], true);
            assert_eq!(report["error_kind"], "Unsupported");
            assert_eq!(report["windows_lock_boundary"], true);
            assert_eq!(report["admissions"], 0);
            assert_eq!(report["code"], Value::Null);
            assert_eq!(report["ordinary_target_created"], true);
            assert_eq!(report["observed_cargo_metadata"], false);
            assert_eq!(report["observed_cargo_help"], false);
            assert_eq!(report["locks_created"], 1);
            assert_eq!(report["fixed_target_created"], false);
        }
    }
}
