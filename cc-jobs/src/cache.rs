//! Shared Cargo cache budget and conservative, owned-file reclamation.
use crate::{FileLock, gate};
use std::{
    collections::BTreeMap,
    fs, io,
    path::Path,
    time::{SystemTime, UNIX_EPOCH},
};
pub const SHARED: &str = r"D:\CC\BuildCache\cargo-shared";
const GUARD_DIR: &str = r"D:\CC\BuildCache\cargo-shared-guard";
pub fn environment(env: &mut BTreeMap<String, String>) {
    if !env
        .keys()
        .any(|k| k.eq_ignore_ascii_case("CARGO_TARGET_DIR"))
    {
        env.insert("CARGO_TARGET_DIR".into(), SHARED.into());
    }
    if !env
        .keys()
        .any(|k| k.eq_ignore_ascii_case("CARGO_INCREMENTAL"))
    {
        env.insert("CARGO_INCREMENTAL".into(), "0".into());
    }
}
fn no_link(p: &Path) -> io::Result<()> {
    use std::os::windows::fs::MetadataExt;
    if fs::symlink_metadata(p)?.file_attributes() & 0x400 != 0 {
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
        use std::os::windows::fs::MetadataExt;
        if m.file_attributes() & 0x400 != 0 {
            return Err(io::Error::other("cache contains a reparse point"));
        }
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
    let mut override_dir = None;
    for (i, arg) in args.iter().enumerate() {
        if arg == "--" {
            break;
        }
        if arg == "--target-dir" {
            override_dir = args.get(i + 1).map(String::as_str);
        }
        if let Some(v) = arg.strip_prefix("--target-dir=") {
            override_dir = Some(v);
        }
    }
    let selected = override_dir.or_else(|| {
        env.iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("CARGO_TARGET_DIR"))
            .map(|(_, v)| v.as_str())
    });
    fn resolved(p: &str) -> Option<String> {
        let p = std::path::absolute(p).ok()?;
        let p = p.canonicalize().unwrap_or(p);
        Some(
            p.to_string_lossy()
                .trim_start_matches(r"\\?\")
                .trim_end_matches('\\')
                .to_lowercase(),
        )
    }
    selected.is_some_and(|p| resolved(p) == resolved(SHARED))
}
pub fn guard_needed(args: &[String], env: &BTreeMap<String, String>) -> bool {
    let command = args
        .first()
        .filter(|s| gate::UNLOCKED.contains(&s.as_str()))
        .map(String::as_str)
        .unwrap_or(gate::first_command(args));
    !gate::UNLOCKED.contains(&command)
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
    let bytes = if cache.exists() { size(cache)? } else { 0 };
    if bytes >= cap {
        return Err(io::Error::other(format!(
            "共有Cargo生成物は上限に達しています（{} GiB / {} GiB）。所有記録で整理できる生成物が不足しているため、新しいビルドを停止します。",
            bytes / (1 << 30),
            cap / (1 << 30)
        )));
    }
    Ok(bytes)
}
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
    fn default_is_shared_and_incremental_disabled() {
        let mut e = BTreeMap::new();
        environment(&mut e);
        assert_eq!(e["CARGO_TARGET_DIR"], SHARED);
        assert_eq!(e["CARGO_INCREMENTAL"], "0");
    }
    #[test]
    fn explicit_case_insensitive_settings_are_preserved() {
        let mut e = BTreeMap::from([
            ("cargo_target_dir".into(), "D:\\custom".into()),
            ("cargo_incremental".into(), "1".into()),
        ]);
        environment(&mut e);
        assert_eq!(e.len(), 2);
        assert_eq!(e["cargo_target_dir"], "D:\\custom");
        assert_eq!(e["cargo_incremental"], "1");
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
        assert!(shared_selected(
            &["--target-dir=d:/CC/BuildCache/cargo-shared".into()],
            &BTreeMap::new()
        ));
        assert!(shared_selected(
            &["run".into(), "--".into(), "--target-dir=D:/private".into()],
            &e
        ));
        assert!(shared_selected(
            &["--target-dir=D:/CC/BuildCache/./cargo-shared".into()],
            &BTreeMap::new()
        ));
        assert!(shared_selected(
            &["--target-dir=D:/CC/BuildCache/other/../cargo-shared".into()],
            &BTreeMap::new()
        ));
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
}
