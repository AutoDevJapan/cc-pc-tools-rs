//! Shared Cargo cache budget and conservative, owned-file reclamation.
//! Environment setup and read-only budget checks are portable. The fixed
//! Windows shared-volume ownership and reclamation path remains Windows-only.
#[cfg(windows)]
use crate::FileLock;
use crate::gate;
#[cfg(any(windows, test))]
use std::time::{SystemTime, UNIX_EPOCH};
use std::{collections::BTreeMap, fs, io, path::Path};
pub const SHARED: &str = r"D:\CC\BuildCache\cargo-shared";
#[cfg(windows)]
const GUARD_DIR: &str = r"D:\CC\BuildCache\cargo-shared-guard";
/// Job environment: the gate picks a per-workspace target, so an inherited legacy
/// shared target is dropped; explicit user targets are kept.
pub fn environment(env: &mut BTreeMap<String, String>) {
    env.retain(|k, v| {
        !k.eq_ignore_ascii_case("CARGO_TARGET_DIR") || resolved(v) != resolved(SHARED)
    });
    build_environment(env, Path::new(gate::SCCACHE));
}
/// Compiler cache settings shared by jobs and the gate; explicit values win.
/// sccache is used only when installed, so other machines (and CI) build normally.
pub fn build_environment(env: &mut BTreeMap<String, String>, sccache: &Path) {
    // Old supervisors injected a global 0. Keep dev path crates incremental,
    // while registry dependencies and the final test profile remain cacheable.
    env.retain(|k, v| !k.eq_ignore_ascii_case("CARGO_INCREMENTAL") || v != "0");
    let mut defaults = vec![
        ("CARGO_PROFILE_DEV_INCREMENTAL", "true".to_string()),
        ("CARGO_PROFILE_DEV_CODEGEN_UNITS", "16".to_string()),
        ("CARGO_PROFILE_TEST_INCREMENTAL", "false".to_string()),
    ];
    if sccache.is_file() {
        defaults.extend([
            ("RUSTC_WRAPPER", sccache.to_string_lossy().into_owned()),
            ("SCCACHE_DIR", r"D:\CC\BuildCache\sccache".into()),
            ("SCCACHE_CACHE_SIZE", "30G".into()),
            ("SCCACHE_SERVER_PORT", "42259".into()),
        ]);
    }
    for (name, value) in defaults {
        if !env.keys().any(|k| k.eq_ignore_ascii_case(name)) {
            env.insert(name.into(), value);
        }
    }
}
/// Case-insensitive canonical form used to compare and lock target directories.
pub fn resolved(p: impl AsRef<Path>) -> Option<String> {
    let p = std::path::absolute(p).ok()?;
    let p = p.canonicalize().unwrap_or(p);
    Some(
        p.to_string_lossy()
            .trim_start_matches(r"\\?\")
            .trim_end_matches('\\')
            .to_lowercase(),
    )
}
fn no_link(p: &Path) -> io::Result<()> {
    no_link_metadata(&fs::symlink_metadata(p)?)
}
fn no_link_metadata(m: &fs::Metadata) -> io::Result<()> {
    #[cfg(windows)]
    let linked = {
        use std::os::windows::fs::MetadataExt;
        m.file_attributes() & 0x400 != 0
    };
    #[cfg(not(windows))]
    let linked = m.file_type().is_symlink();
    if linked {
        return Err(io::Error::other("cache contains a reparse point"));
    }
    Ok(())
}
pub fn size(p: &Path) -> io::Result<u64> {
    no_link(p)?;
    let mut bytes = 0u64;
    for e in fs::read_dir(p)? {
        let e = match e {
            Ok(e) => e,
            Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
            Err(e) => return Err(e),
        };
        let m = match fs::symlink_metadata(e.path()) {
            Ok(m) => m,
            Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
            Err(e) => return Err(e),
        };
        no_link_metadata(&m)?;
        bytes = bytes
            .checked_add(if m.is_dir() {
                match size(&e.path()) {
                    Ok(n) => n,
                    Err(e) if e.kind() == io::ErrorKind::NotFound => 0,
                    Err(e) => return Err(e),
                }
            } else {
                m.len()
            })
            .ok_or_else(|| io::Error::other("size overflow"))?;
    }
    Ok(bytes)
}
pub fn shared_selected(args: &[String], env: &BTreeMap<String, String>) -> bool {
    let selected = gate::explicit_target(args).or_else(|| {
        env.iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("CARGO_TARGET_DIR"))
            .map(|(_, v)| v.as_str())
    });
    selected.is_some_and(|p| resolved(p) == resolved(SHARED))
}
pub fn guard_needed(args: &[String], env: &BTreeMap<String, String>) -> bool {
    gate::build_like(args)
        && shared_selected(args, env)
        && env
            .get("CODEX_JOBS_CACHE_LOCK_HELD")
            .is_none_or(|v| v != "1")
}
const GIB: u64 = 1 << 30;
pub fn adaptive_limit(bytes: u64, free: u64) -> io::Result<u64> {
    let available = bytes.saturating_add(free);
    let cap = (available / 4)
        .min(512 * GIB)
        .min(available.saturating_sub(100 * GIB));
    if cap == 0 {
        return Err(io::Error::other(
            "Cargo cache requires 100 GiB of free disk reserve",
        ));
    }
    Ok(cap)
}
#[cfg(windows)]
fn free_space(path: &Path) -> io::Result<u64> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::Storage::FileSystem::GetDiskFreeSpaceExW;
    let path: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
    let mut free = 0;
    if unsafe {
        GetDiskFreeSpaceExW(
            path.as_ptr(),
            &mut free,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
        )
    } == 0
    {
        return Err(io::Error::last_os_error());
    }
    Ok(free)
}
#[cfg(windows)]
fn limit(env: &BTreeMap<String, String>, bytes: u64) -> io::Result<u64> {
    let free = free_space(Path::new(SHARED).parent().unwrap())?;
    let Some(value) = env.get("CODEX_JOBS_CACHE_MAX_GIB") else {
        return adaptive_limit(bytes, free);
    };
    let n = value.parse::<u64>().map_err(io::Error::other)?;
    let cap = n
        .checked_mul(GIB)
        .filter(|n| *n > 0)
        .ok_or_else(|| io::Error::other("invalid cache budget"))?;
    let reserve_cap = bytes.saturating_add(free).saturating_sub(100 * GIB);
    if reserve_cap == 0 {
        return Err(io::Error::other(
            "Cargo cache requires 100 GiB of free disk reserve",
        ));
    }
    Ok(cap.min(reserve_cap))
}
#[cfg(windows)]
fn prepare() -> io::Result<()> {
    let parent = Path::new(SHARED).parent().unwrap();
    fs::create_dir_all(parent)?;
    for p in parent.ancestors() {
        no_link(p)?;
    }
    fs::create_dir_all(GUARD_DIR)?;
    no_link(Path::new(GUARD_DIR))
}
pub fn budget_check(cache: &Path, cap: u64) -> io::Result<u64> {
    budget_check_with_metadata(cache, cap, fs::symlink_metadata(cache))
}
fn budget_check_with_metadata(
    cache: &Path,
    cap: u64,
    metadata: io::Result<fs::Metadata>,
) -> io::Result<u64> {
    let bytes = match metadata {
        Ok(_) => size(cache)?,
        Err(error) if error.kind() == io::ErrorKind::NotFound => 0,
        Err(error) => return Err(error),
    };
    if bytes >= cap {
        return Err(io::Error::other(format!(
            "共有Cargo生成物は上限に達しています（{} GiB / {} GiB）。所有記録で整理できる生成物が不足しているため、新しいビルドを停止します。",
            bytes / (1 << 30),
            cap / (1 << 30)
        )));
    }
    Ok(bytes)
}
#[cfg(windows)]
fn receipt(bytes: u64, cap: u64) -> io::Result<()> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();
    fs::write(
        Path::new(GUARD_DIR).join("usage.json"),
        format!(
            "{{\"bytes\":{bytes},\"limit_bytes\":{cap},\"checked_unix\":{now},\"over_limit\":{}}}\n",
            bytes >= cap
        ),
    )
}
#[cfg(windows)]
pub fn guarded<T>(
    env: &BTreeMap<String, String>,
    args: &[String],
    body: impl FnOnce() -> io::Result<T>,
) -> io::Result<T> {
    prepare()?;
    // Fixed one-slot lock: every gated shared-cache build uses it, regardless of job slot settings.
    let wait = std::cell::RefCell::new(None);
    gate::with_slot(
        Path::new(GUARD_DIR),
        1,
        |s| {
            if wait.borrow().is_none() {
                *wait.borrow_mut() =
                    crate::queue_status::ResourceWait::new(env, "cargoの共有キャッシュ枠待ち");
            }
            std::thread::sleep(std::time::Duration::from_secs_f64(s));
        },
        || {
            drop(wait.borrow_mut().take());
            let cache = Path::new(SHARED);
            let mut before = crate::cache_history::inventory(cache)?;
            let bytes: u64 = before.values().map(|s| s.bytes()).sum();
            let cap = limit(env, bytes)?;
            let history_path = Path::new(GUARD_DIR).join("ownership.json");
            let mut history = crate::cache_history::History::load(&history_path)?;
            let at = crate::cache_history::now();
            if bytes >= cap.saturating_mul(4) / 5 {
                let reclaimed =
                    history.reclaim(cache, at, bytes.saturating_sub(cap.saturating_mul(3) / 4))?;
                if reclaimed > 0 {
                    before = crate::cache_history::inventory(cache)?;
                }
                history.save(&history_path)?;
            }
            // Recompute from actual free space: quarantine removes cache names but does
            // not free disk space, so it must never bypass the disk reserve.
            let bytes: u64 = before.values().map(|s| s.bytes()).sum();
            let cap = limit(env, bytes)?;
            let bytes = budget_check(cache, cap)?;
            receipt(bytes, cap)?;
            // Persist use BEFORE Cargo starts, even if the process crashes during the build.
            history.record(&before, &before, at, "", "");
            history.save(&history_path)?;
            let producer = source_dir(args)?.to_string_lossy().into_owned();
            let branch = crate::hidden_command("git")
                .args(["-C", &producer, "branch", "--show-current"])
                .output()
                .ok()
                .filter(|o| o.status.success())
                .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_owned())
                .unwrap_or_default();
            let result = body();
            // One build can cross the limit; all later gated builds are refused until space is reclaimed.
            if let Ok(bytes) = size(cache) {
                let _ = receipt(bytes, cap);
            }
            if let Ok(after) = crate::cache_history::inventory(cache) {
                history.record(
                    &before,
                    &after,
                    crate::cache_history::now(),
                    &producer,
                    &branch,
                );
                if let Err(e) = history.save(&history_path) {
                    eprintln!("Cargo cache ownership not saved: {e}");
                }
            }
            result
        },
    )
}
#[cfg(windows)]
fn source_dir(args: &[String]) -> io::Result<std::path::PathBuf> {
    let mut manifest = None;
    for (i, arg) in args.iter().enumerate() {
        if arg == "--" {
            break;
        }
        if arg == "--manifest-path" {
            manifest = args.get(i + 1).map(String::as_str);
        }
        if let Some(value) = arg.strip_prefix("--manifest-path=") {
            manifest = Some(value);
        }
    }
    if let Some(manifest) = manifest {
        let path = std::path::absolute(manifest)?;
        return path
            .parent()
            .map(Path::to_path_buf)
            .ok_or_else(|| io::Error::other("manifest has no parent"));
    }
    std::env::current_dir()
}
#[cfg(windows)]
pub fn finish(_root: &Path, env: &BTreeMap<String, String>) -> io::Result<()> {
    if !shared_selected(&[], env) || !Path::new(SHARED).exists() {
        return Ok(());
    }
    prepare()?;
    let _guard = match FileLock::try_acquire(&Path::new(GUARD_DIR).join("cargo-build.lock.0")) {
        Ok(v) => v,
        Err(_) => return Ok(()),
    };
    let bytes = size(Path::new(SHARED))?;
    receipt(bytes, limit(env, bytes)?)
}
// Selecting the fixed Windows cache on another OS must fail before any
// callback or fixed-volume I/O; ordinary target/environment APIs remain usable.
#[cfg(not(windows))]
fn unsupported_shared_cache() -> io::Error {
    io::Error::new(
        io::ErrorKind::Unsupported,
        "shared Cargo cache maintenance is Windows-only",
    )
}
#[cfg(not(windows))]
pub fn guarded<T>(
    _env: &BTreeMap<String, String>,
    _args: &[String],
    _body: impl FnOnce() -> io::Result<T>,
) -> io::Result<T> {
    Err(unsupported_shared_cache())
}
#[cfg(not(windows))]
pub fn finish(_root: &Path, env: &BTreeMap<String, String>) -> io::Result<()> {
    reject_unsupported_shared_target(&[], env)?;
    Ok(())
}

// These checks reject only the fixed Windows cache namespace. They neither
// qualify arbitrary paths nor interpret an inherited lock marker as authority.
#[cfg(not(windows))]
fn fixed_windows_shared_target(path: &Path) -> bool {
    let value = path.to_string_lossy().replace('\\', "/");
    let value = value.strip_prefix("//?/").unwrap_or(&value);
    let Some(drive) = value.get(..2) else {
        return false;
    };
    if !drive.eq_ignore_ascii_case("D:") || !value[2..].starts_with('/') {
        return false;
    }
    let mut parts = Vec::new();
    for part in value[3..].split('/') {
        match part {
            "" | "." => {}
            ".." => {
                let _ = parts.pop();
            }
            other => parts.push(other),
        }
    }
    parts.len() >= 3
        && parts[0].eq_ignore_ascii_case("CC")
        && parts[1].eq_ignore_ascii_case("BuildCache")
        && parts[2].eq_ignore_ascii_case("cargo-shared")
}
// Only rejection matching is normalized. It never grants access to a path.
#[cfg(not(windows))]
fn normalized_native_target(path: &Path, cwd: &Path) -> std::path::PathBuf {
    use std::path::Component;
    let absolute = if path.is_absolute() {
        path.to_path_buf()
    } else {
        cwd.join(path)
    };
    let mut normalized = std::path::PathBuf::new();
    for component in absolute.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                let _ = normalized.pop();
            }
            other => normalized.push(other.as_os_str()),
        }
    }
    normalized
}


#[cfg(not(windows))]
#[derive(Clone, Debug, PartialEq, Eq)]
struct SharedTargetNode {
    device: u64,
    inode: u64,
    kind: u32,
}

#[cfg(not(windows))]
fn shared_target_node(metadata: &fs::Metadata) -> io::Result<SharedTargetNode> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt;
        Ok(SharedTargetNode {
            device: metadata.dev(),
            inode: metadata.ino(),
            kind: metadata.mode() & 0o170000,
        })
    }
    #[cfg(not(unix))]
    {
        let _ = metadata;
        Err(shared_target_unknown())
    }
}

#[cfg(not(windows))]
#[derive(Clone, Debug, PartialEq, Eq)]
struct SharedTargetObservation {
    resolved: std::path::PathBuf,
    nodes: BTreeMap<std::path::PathBuf, SharedTargetNode>,
    links: BTreeMap<std::path::PathBuf, (SharedTargetNode, std::path::PathBuf)>,
    target_ancestors: Vec<SharedTargetNode>,
    fixed_roots: BTreeMap<std::path::PathBuf, (SharedTargetNode, std::path::PathBuf, SharedTargetNode)>,
}

#[cfg(not(windows))]
fn shared_target_unknown() -> io::Error {
    io::Error::new(
        io::ErrorKind::InvalidInput,
        "shared cache target identity changed or is unresolved",
    )
}

// Resolve each existing redirect through metadata only, recording every link and
// directory identity. This is bounded rejection evidence, never a path grant.
#[cfg(not(windows))]
fn trace_shared_target(
    path: &Path,
    observation: &mut SharedTargetObservation,
    links_left: &mut usize,
) -> io::Result<std::path::PathBuf> {
    use std::path::Component;
    let mut resolved = std::path::PathBuf::new();
    for component in path.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                let metadata = fs::symlink_metadata(&resolved)
                    .map_err(|_| shared_target_unknown())?;
                if !metadata.is_dir() || metadata.file_type().is_symlink() {
                    return Err(shared_target_unknown());
                }
                record_shared_target_node(observation, &resolved, &metadata)?;
                let _ = resolved.pop();
            }
            other => {
                resolved.push(other.as_os_str());
                match fs::symlink_metadata(&resolved) {
                    Ok(metadata) => {
                        let identity = record_shared_target_node(observation, &resolved, &metadata)?;
                        if metadata.file_type().is_symlink() {
                            if *links_left == 0 {
                                return Err(shared_target_unknown());
                            }
                            *links_left -= 1;
                            let target = fs::read_link(&resolved)
                                .map_err(|_| shared_target_unknown())?;
                            if let Some(previous) = observation.links.insert(
                                resolved.clone(), (identity.clone(), target.clone()),
                            ) {
                                if previous != (identity, target.clone()) {
                                    return Err(shared_target_unknown());
                                }
                            }
                            let redirect = if target.is_absolute() {
                                target
                            } else {
                                resolved.parent().ok_or_else(shared_target_unknown)?.join(target)
                            };
                            resolved = trace_shared_target(&redirect, observation, links_left)?;
                            // A dangling redirect cannot become an ordinary missing target.
                            let destination = fs::symlink_metadata(&resolved)
                                .map_err(|_| shared_target_unknown())?;
                            if destination.file_type().is_symlink() {
                                return Err(shared_target_unknown());
                            }
                            record_shared_target_node(observation, &resolved, &destination)?;
                        }
                    }
                    Err(error) if error.kind() == io::ErrorKind::NotFound => {}
                    Err(error) => return Err(error),
                }
            }
        }
    }
    Ok(resolved)
}

#[cfg(not(windows))]
fn record_shared_target_node(
    observation: &mut SharedTargetObservation,
    path: &Path,
    metadata: &fs::Metadata,
) -> io::Result<SharedTargetNode> {
    let identity = shared_target_node(metadata)?;
    if observation.nodes.get(path).is_some_and(|previous| previous != &identity) {
        return Err(shared_target_unknown());
    }
    observation.nodes.insert(path.to_path_buf(), identity.clone());
    Ok(identity)
}

#[cfg(not(windows))]
fn observe_shared_target(path: &Path, cwd: &Path) -> io::Result<SharedTargetObservation> {
    let absolute = if path.is_absolute() { path.to_path_buf() } else { cwd.join(path) };
    let mut observation = SharedTargetObservation {
        resolved: std::path::PathBuf::new(),
        nodes: BTreeMap::new(),
        links: BTreeMap::new(),
        target_ancestors: Vec::new(),
        fixed_roots: BTreeMap::new(),
    };
    let resolved = trace_shared_target(&absolute, &mut observation, &mut 40)?;
    observation.resolved = resolved;
    // Keep every visited node in nodes for change detection, including routes
    // later left by "..". Scope includes only the final resolved ancestor chain.
    // Freeze it before observing fixed roots; those reads are not target ancestry.
    observation.target_ancestors = observation.resolved.ancestors()
        .filter_map(|ancestor| observation.nodes.get(ancestor).cloned())
        .collect();
    for root in [cwd.join(SHARED), cwd.join("D:/CC/BuildCache/cargo-shared")] {
        let metadata = match fs::symlink_metadata(&root) {
            Ok(metadata) => metadata,
            Err(error) if error.kind() == io::ErrorKind::NotFound => continue,
            Err(error) => return Err(error),
        };
        let identity = shared_target_node(&metadata)?;
        let physical = trace_shared_target(&root, &mut observation, &mut 40)?;
        let target = fs::symlink_metadata(&physical).map_err(|_| shared_target_unknown())?;
        observation.fixed_roots.insert(
            root, (identity, physical, shared_target_node(&target)?),
        );
    }
    Ok(observation)
}

#[cfg(not(windows))]
fn reject_fixed_observation(
    path: &Path,
    cwd: &Path,
    observation: &SharedTargetObservation,
) -> io::Result<()> {
    let resolved_fixed = observation.resolved
        .strip_prefix(cwd)
        .is_ok_and(fixed_windows_shared_target);
    let same_fixed_object = observation.target_ancestors.iter().any(|identity| {
        observation.fixed_roots.values().any(|(_, _, target)| identity == target)
    });
    if resolved_fixed || same_fixed_object {
        return Err(unsupported_shared_cache());
    }
    let lexical = normalized_native_target(path, cwd);
    if lexical.strip_prefix(cwd).is_ok_and(fixed_windows_shared_target) {
        if !observation.links.is_empty() {
            return Err(shared_target_unknown());
        }
        return Err(unsupported_shared_cache());
    }
    Ok(())
}

// This is an invocation-local rejection observer, not an issuer or a namespace
// lock. Repeated observations and final pre-start checks detect observed
// changes; no observation is reported as race-free permission.
#[cfg(not(windows))]
#[derive(Default)]
pub(crate) struct SharedTargetChecks {
    cwd: Option<std::path::PathBuf>,
    observations: BTreeMap<std::path::PathBuf, SharedTargetObservation>,
    unknown: bool,
}

#[cfg(not(windows))]
impl SharedTargetChecks {
    pub(crate) fn check_path(&mut self, path: &Path) -> io::Result<()> {
        if self.unknown {
            return Err(shared_target_unknown());
        }
        let result = self.check_path_inner(path);
        if result.is_err() {
            self.unknown = true;
        }
        result
    }

    fn check_path_inner(&mut self, path: &Path) -> io::Result<()> {
        if fixed_windows_shared_target(path) {
            return Err(unsupported_shared_cache());
        }
        let cwd = std::env::current_dir()?;
        if self.cwd.as_ref().is_some_and(|previous| previous != &cwd) {
            return Err(shared_target_unknown());
        }
        self.cwd = Some(cwd.clone());
        let key = if path.is_absolute() { path.to_path_buf() } else { cwd.join(path) };
        let before = observe_shared_target(path, &cwd)?;
        reject_fixed_observation(path, &cwd, &before)?;
        let after = observe_shared_target(path, &cwd)?;
        reject_fixed_observation(path, &cwd, &after)?;
        if before != after {
            return Err(shared_target_unknown());
        }
        if let Some(previous) = self.observations.get(&key) {
            if previous.resolved != after.resolved
                || previous.links != after.links
                || previous.fixed_roots != after.fixed_roots
                || previous.nodes.iter().any(|(name, id)| after.nodes.get(name) != Some(id))
            {
                return Err(shared_target_unknown());
            }
        }
        self.observations.insert(key, after);
        Ok(())
    }

    pub(crate) fn recheck(&mut self) -> io::Result<()> {
        let paths: Vec<_> = self.observations.keys().cloned().collect();
        for path in paths {
            self.check_path(&path)?;
        }
        if self.unknown { Err(shared_target_unknown()) } else { Ok(()) }
    }

    pub(crate) fn check_request(
        &mut self,
        args: &[String],
        env: &BTreeMap<String, String>,
    ) -> io::Result<()> {
        self.recheck()?;
        let value = |name: &str| {
            env.iter()
                .find(|(key, value)| key.eq_ignore_ascii_case(name) && !value.is_empty())
                .map(|(_, value)| value.as_str())
        };
        if let Some(path) = gate::explicit_target(args) {
            return self.check_path(Path::new(path));
        }
        let inherited = value("CARGO_TARGET_DIR");
        let nested = env.get("CODEX_JOBS_CARGO_LOCK_HELD").is_some_and(|v| v == "1");
        let legacy_nested = nested && !env.contains_key(gate::HELD_TARGET);
        let direct = !gate::build_like(args) || legacy_nested;
        if let Some(path) = inherited {
            self.check_path(Path::new(path))?;
            if direct || value(gate::INJECTED_TARGET) != Some(path) {
                return Ok(());
            }
        }
        if inherited.is_none() && nested && !direct
            && let Some(path) = value(gate::FORWARDED_TARGET)
        {
            return self.check_path(Path::new(path));
        }
        if let Some(path) = value("CARGO_BUILD_TARGET_DIR") {
            self.check_path(Path::new(path))?;
        }
        Ok(())
    }
}

#[cfg(not(windows))]
pub(crate) fn reject_unsupported_shared_target(
    args: &[String],
    env: &BTreeMap<String, String>,
) -> io::Result<()> {
    SharedTargetChecks::default().check_request(args, env)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn adaptive_budget_uses_actual_volume_and_reserves_space() {
        assert_eq!(
            adaptive_limit(129 * GIB, 1700 * GIB).unwrap(),
            1829 * GIB / 4
        );
        assert_eq!(adaptive_limit(129 * GIB, 3000 * GIB).unwrap(), 512 * GIB);
        assert_eq!(adaptive_limit(80 * GIB, 25 * GIB).unwrap(), 5 * GIB);
        assert!(adaptive_limit(0, 90 * GIB).is_err());
    }
    #[test]
    fn legacy_shared_target_is_dropped_and_dev_incremental_retained() {
        let mut e = BTreeMap::from([(
            "Cargo_Target_Dir".into(),
            if cfg!(windows) {
                "d:/CC/BuildCache/cargo-shared/".into()
            } else {
                SHARED.into()
            },
        )]);
        environment(&mut e);
        assert!(!e.keys().any(|k| k.eq_ignore_ascii_case("CARGO_TARGET_DIR")));
        assert!(!e.contains_key("CARGO_INCREMENTAL"));
        assert_eq!(e["CARGO_PROFILE_DEV_INCREMENTAL"], "true");
    }
    #[test]
    fn explicit_case_insensitive_settings_are_preserved() {
        let mut e = BTreeMap::from([
            ("cargo_target_dir".into(), "D:\\custom".into()),
            ("cargo_incremental".into(), "1".into()),
        ]);
        environment(&mut e);
        assert_eq!(e["cargo_target_dir"], "D:\\custom");
        assert_eq!(e["cargo_incremental"], "1");
        assert!(!e.contains_key("CARGO_TARGET_DIR"));
        assert!(!e.contains_key("CARGO_INCREMENTAL"));
    }
    #[test]
    fn target_argument_overrides_shared_environment() {
        let e = BTreeMap::from([("CARGO_TARGET_DIR".into(), SHARED.into())]);
        assert!(shared_selected(&[], &e));
        assert!(!shared_selected(
            &["--target-dir".into(), "D:\\private".into()],
            &e
        ));
        assert!(!shared_selected(&["--target-dir=D:\\private".into()], &e));
        #[cfg(windows)]
        assert!(shared_selected(
            &["--target-dir=d:/CC/BuildCache/cargo-shared".into()],
            &BTreeMap::new()
        ));
        assert!(shared_selected(
            &["run".into(), "--".into(), "--target-dir=D:/private".into()],
            &e
        ));
        #[cfg(windows)]
        assert!(shared_selected(
            &["--target-dir=D:/CC/BuildCache/./cargo-shared".into()],
            &BTreeMap::new()
        ));
        #[cfg(windows)]
        assert!(shared_selected(
            &["--target-dir=D:/CC/BuildCache/other/../cargo-shared".into()],
            &BTreeMap::new()
        ));
        #[cfg(not(windows))]
        {
            assert!(shared_selected(
                &[format!("--target-dir={SHARED}")],
                &BTreeMap::new()
            ));
            assert!(!shared_selected(
                &["--target-dir=/tmp/cc-jobs-native".into()],
                &e
            ));
        }
    }
    #[test]
    fn sccache_is_configured_only_when_installed_and_explicit_values_win() {
        let missing = std::env::temp_dir().join("cc-jobs-no-such-sccache.exe");
        let mut e = BTreeMap::new();
        build_environment(&mut e, &missing);
        assert_eq!(
            e,
            BTreeMap::from([
                ("CARGO_PROFILE_DEV_INCREMENTAL".into(), "true".into()),
                ("CARGO_PROFILE_DEV_CODEGEN_UNITS".into(), "16".into()),
                ("CARGO_PROFILE_TEST_INCREMENTAL".into(), "false".into())
            ])
        );
        let installed = std::env::current_exe().unwrap();
        let mut e = BTreeMap::from([("sccache_cache_size".into(), "5G".into())]);
        build_environment(&mut e, &installed);
        assert_eq!(e["RUSTC_WRAPPER"], installed.to_string_lossy());
        assert_eq!(e["SCCACHE_DIR"], r"D:\CC\BuildCache\sccache");
        assert_eq!(e["SCCACHE_SERVER_PORT"], "42259");
        assert_eq!(e["sccache_cache_size"], "5G");
        assert!(!e.contains_key("SCCACHE_CACHE_SIZE"));
        let mut e = BTreeMap::new();
        build_environment(&mut e, &installed);
        assert_eq!(e["SCCACHE_CACHE_SIZE"], "30G");
        assert!(!e.contains_key("CARGO_INCREMENTAL"));
        e.insert("CARGO_INCREMENTAL".into(), "0".into());
        build_environment(&mut e, &installed);
        assert!(!e.contains_key("CARGO_INCREMENTAL"));
    }
    #[test]
    fn ordinary_slot_does_not_bypass_shared_budget() {
        let mut e = BTreeMap::from([
            ("CARGO_TARGET_DIR".into(), SHARED.into()),
            ("CODEX_JOBS_CARGO_LOCK_HELD".into(), "1".into()),
        ]);
        assert!(guard_needed(&["build".into()], &e));
        e.insert("CODEX_JOBS_CACHE_LOCK_HELD".into(), "1".into());
        assert!(!guard_needed(&["build".into()], &e));
        assert!(!guard_needed(
            &["--version".into()],
            &BTreeMap::from([("CARGO_TARGET_DIR".into(), SHARED.into())])
        ));
    }
    #[test]
    fn full_cache_is_preserved_and_over_budget_is_refused() {
        let p = std::env::temp_dir().join(format!(
            "cc-cache-budget-test-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir_all(p.join("debug")).unwrap();
        fs::write(p.join("debug/item"), b"build bytes").unwrap();
        assert_eq!(budget_check(&p, 12).unwrap(), 11);
        assert!(budget_check(&p, 11).is_err());
        assert!(budget_check(&p, 10).is_err());
        assert_eq!(fs::read(p.join("debug/item")).unwrap(), b"build bytes");
    }
    #[cfg(unix)]
    fn assert_final_target_scope_ancestors() {
        let fixture = tempfile::tempdir().unwrap();
        let cwd = fixture.path();
        let fixed = cwd.join("D:/CC/BuildCache/cargo-shared");
        let peer = cwd.join("D:/CC/BuildCache/cargo-shared-peer");
        fs::create_dir_all(fixed.join("debug")).unwrap();
        fs::create_dir(&peer).unwrap();
        fs::write(fixed.join("keep"), b"fixed sentinel").unwrap();
        fs::write(peer.join("keep"), b"ordinary sentinel").unwrap();
        std::os::unix::fs::symlink(&fixed, cwd.join("scope-alias")).unwrap();
        std::os::unix::fs::symlink(
            fixed.join("../cargo-shared-peer"), cwd.join("escape-alias"),
        ).unwrap();
        std::os::unix::fs::symlink(
            fixed.join("debug"), cwd.join("descendant-alias"),
        ).unwrap();
        let fixed_identity = shared_target_node(&fs::symlink_metadata(&fixed).unwrap()).unwrap();
        let resolved_peer = fs::canonicalize(&peer).unwrap();
        let exits = [
            std::path::PathBuf::from("D:/CC/BuildCache/cargo-shared/../cargo-shared-peer"),
            fixed.join("../cargo-shared-peer"),
            cwd.join("scope-alias/../cargo-shared-peer"),
            cwd.join("escape-alias"),
        ];
        for path in &exits {
            let observation = observe_shared_target(path, cwd).unwrap();
            assert_eq!(observation.resolved, resolved_peer);
            // The route history is still complete for change detection.
            assert!(observation.nodes.values().any(|node| node == &fixed_identity));
            // Scope contains only ancestors of the final physical target.
            assert!(!observation.target_ancestors.iter().any(|node| node == &fixed_identity));
            reject_fixed_observation(path, cwd, &observation).unwrap();
        }
        for path in [fixed.clone(), fixed.join("debug"), cwd.join("scope-alias"),
            cwd.join("descendant-alias")]
        {
            let observation = observe_shared_target(&path, cwd).unwrap();
            assert_eq!(reject_fixed_observation(&path, cwd, &observation)
                .unwrap_err().kind(), io::ErrorKind::Unsupported);
        }
        // Leaving the scope does not discard the visited fixed directory.
        // Replacing that intermediate identity remains a sticky unknown.
        let mut history = observe_shared_target(&exits[0], cwd).unwrap();
        fs::rename(&fixed, cwd.join("old-fixed")).unwrap();
        fs::create_dir(&fixed).unwrap();
        let absolute = cwd.join(&exits[0]);
        assert_eq!(trace_shared_target(&absolute, &mut history, &mut 40)
            .unwrap_err().kind(), io::ErrorKind::InvalidInput);
        assert_eq!(fs::read(peer.join("keep")).unwrap(), b"ordinary sentinel");
        assert_eq!(fs::read(cwd.join("old-fixed/keep")).unwrap(), b"fixed sentinel");
    }

    #[cfg(not(windows))]
    #[test]
    fn windows_shared_maintenance_rejects_before_any_callback() {
        #[cfg(unix)]
        assert_final_target_scope_ancestors();
        for target in ["D:/CC/BuildCache/cargo-shared/debug",
            "D:/CC/BuildCache/cargo-shared/release/deps", r"D:\CC\BuildCache\cargo-shared\debug"]
        {
            assert!(fixed_windows_shared_target(Path::new(target)));
        }
        for target in ["D:/CC/BuildCache/cargo-shared-peer",
            "D:/CC/BuildCache/cargo-shared2/debug", "/tmp/ordinary"]
        {
            assert!(!fixed_windows_shared_target(Path::new(target)));
        }
        let env = BTreeMap::from([("CARGO_TARGET_DIR".into(), SHARED.into())]);
        let called = std::cell::Cell::new(false);
        let result = guarded(&env, &["build".into()], || {
            called.set(true);
            Ok(())
        });
        assert_eq!(result.unwrap_err().kind(), io::ErrorKind::Unsupported);
        assert!(!called.get());
        assert_eq!(
            finish(Path::new("unused"), &env).unwrap_err().kind(),
            io::ErrorKind::Unsupported
        );
    }
    #[cfg(not(windows))]
    #[test]
    fn ordinary_target_environment_and_finish_remain_available() {
        let target = "/tmp/cc-jobs-portable-target";
        let mut env = BTreeMap::from([("CARGO_TARGET_DIR".into(), target.into())]);
        environment(&mut env);
        assert_eq!(env["CARGO_TARGET_DIR"], target);
        assert_eq!(env["CARGO_PROFILE_DEV_INCREMENTAL"], "true");
        assert!(!guard_needed(&["build".into()], &env));
        finish(Path::new("unused"), &env).unwrap();
    }
    #[cfg(unix)]
    #[test]
    fn read_only_budget_rejects_linked_roots_and_entries() {
        let fixture = tempfile::tempdir().unwrap();
        let root = fixture.path().join("cache");
        let external = fixture.path().join("external");
        fs::create_dir(&root).unwrap();
        fs::create_dir(&external).unwrap();
        fs::write(root.join("artifact"), b"cache").unwrap();
        fs::write(external.join("keep"), b"unchanged").unwrap();
        assert_eq!(size(&root).unwrap(), 5);
        std::os::unix::fs::symlink(&external, root.join("link")).unwrap();
        assert!(size(&root).is_err());
        assert!(budget_check(&root, 100).is_err());
        let root_link = fixture.path().join("root-link");
        std::os::unix::fs::symlink(&external, &root_link).unwrap();
        assert!(size(&root_link).is_err());
        assert_eq!(fs::read(external.join("keep")).unwrap(), b"unchanged");
    }

    #[test]
    fn only_missing_cache_metadata_counts_as_empty() {
        let fixture = tempfile::tempdir().unwrap();
        let missing = fixture.path().join("missing");
        assert_eq!(budget_check(&missing, 100).unwrap(), 0);
        for kind in [io::ErrorKind::PermissionDenied, io::ErrorKind::Other] {
            let result = budget_check_with_metadata(
                &missing,
                100,
                Err(io::Error::new(kind, "owned metadata fault")),
            );
            assert_eq!(result.unwrap_err().kind(), kind);
        }
    }
    #[cfg(unix)]
    #[test]
    fn dangling_cache_link_is_not_an_empty_cache() {
        let fixture = tempfile::tempdir().unwrap();
        let missing = fixture.path().join("missing");
        let link = fixture.path().join("dangling");
        std::os::unix::fs::symlink(&missing, &link).unwrap();
        assert!(budget_check(&link, 100).is_err());
        assert!(fs::symlink_metadata(&link).unwrap().file_type().is_symlink());
        assert!(!missing.exists());
    }
}
