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
    let Some(cargo) = env
        .get("CODEX_JOBS_REAL_CARGO")
        .filter(|s| Path::new(s).is_file())
    else {
        eprintln!("cargo gate: CODEX_JOBS_REAL_CARGO is missing");
        return Ok(127);
    };
    let execute = |args: &[String], child: &BTreeMap<String, String>| {
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
    let target = target_dir.as_ref().map(|p| {
        // Create first, so the key is canonical before and after Cargo creates it.
        let _ = std::fs::create_dir_all(p);
        key(p)
    });
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
    let slot = (!nested)
        .then(|| admission::wait(dir, env, cores))
        .transpose()?;
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
    child.insert(HELD_JOBS.into(), build_limit.to_string());
    for name in [
        "CARGO_BUILD_JOBS",
        "RUST_TEST_THREADS",
        "NEXTEST_TEST_THREADS",
    ] {
        if name == "NEXTEST_TEST_THREADS" && first_command(args) != "nextest" {
            continue;
        }
        let limit = if name == "CARGO_BUILD_JOBS" {
            build_limit
        } else {
            test_limit
        };
        let jobs = take_var(child, name).map_or(limit, |v| clamp(&v, limit));
        child.insert(name.into(), jobs.to_string());
    }
    let test_var = if first_command(args) == "nextest" {
        "NEXTEST_TEST_THREADS"
    } else {
        "RUST_TEST_THREADS"
    };
    let test_limit = clamp(&child[test_var], test_limit);
    let (out, build, tests) = clamp_parallel_args(args, build_limit, test_limit);
    let (out, tests) = if first_command(args) == "test" {
        clamp_harness_threads(&out, tests)
    } else {
        (out, tests)
    };
    child.insert(HELD_JOBS.into(), build.to_string());
    child.insert("CARGO_BUILD_JOBS".into(), build.to_string());
    child.insert(HELD_TESTS.into(), tests.to_string());
    child.insert("RUST_TEST_THREADS".into(), tests.to_string());
    if first_command(args) == "nextest" {
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
}
