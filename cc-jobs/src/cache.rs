//! Shared Cargo cache budget. Never rotate or delete automatically.
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
        let e = e?;
        let m = e.metadata()?;
        use std::os::windows::fs::MetadataExt;
        if m.file_attributes() & 0x400 != 0 {
            return Err(io::Error::other("cache contains a reparse point"));
        }
        bytes = bytes
            .checked_add(if m.is_dir() {
                size(&e.path())?
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
fn limit(env: &BTreeMap<String, String>) -> io::Result<u64> {
    let n = env
        .get("CODEX_JOBS_CACHE_MAX_GIB")
        .map(|s| s.parse::<u64>())
        .transpose()
        .map_err(io::Error::other)?
        .unwrap_or(128);
    n.checked_mul(1 << 30)
        .filter(|n| *n > 0)
        .ok_or_else(|| io::Error::other("invalid cache budget"))
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
            "共有Cargo生成物は上限に達しています（{} GiB / {} GiB）。生成物を保持して新しいビルドを停止します。削除希望への退避と同じDドライブ上での再生成は容量を増やすため自動では行いません。",
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
    body: impl FnOnce() -> io::Result<T>,
) -> io::Result<T> {
    prepare()?;
    // Fixed one-slot lock: every gated shared-cache build uses it, regardless of job slot settings.
    gate::with_slot(
        Path::new(GUARD_DIR),
        1,
        |s| std::thread::sleep(std::time::Duration::from_secs_f64(s)),
        || {
            let cap = limit(env)?;
            let cache = Path::new(SHARED);
            let bytes = budget_check(cache, cap)?;
            receipt(bytes, cap)?;
            let result = body();
            // One build can cross the limit; all later gated builds are refused until space is reclaimed.
            if let Ok(bytes) = size(cache) {
                let _ = receipt(bytes, cap);
            }
            result
        },
    )
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
    receipt(size(Path::new(SHARED))?, limit(env)?)
}
#[cfg(test)]
mod tests {
    use super::*;
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
