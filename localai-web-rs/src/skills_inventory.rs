//! Count definitions in the authoritative Skills directory without publishing names or text.
use ring::digest::{digest, SHA256};
use serde_json::{json, Value};
use std::{
    collections::HashSet,
    fs,
    io::Read,
    path::{Path, PathBuf},
    sync::Mutex,
    time::{Duration, Instant},
};

const REFRESH_INTERVAL: Duration = Duration::from_secs(60);
const SCAN_BUDGET: Duration = Duration::from_secs(5);
const MAX_ENTRIES: usize = 100_000;
const MAX_DEFINITION_BYTES: u64 = 1024 * 1024;

pub(crate) struct Inventory {
    root: PathBuf,
    cached: Mutex<Option<(Instant, Value)>>,
}

impl Inventory {
    pub(crate) fn new(root: PathBuf) -> Self {
        Self {
            root,
            cached: Mutex::new(None),
        }
    }

    pub(crate) fn snapshot(&self, now: u64) -> Value {
        let mut cached = self.cached.lock().unwrap_or_else(|e| e.into_inner());
        if let Some((at, value)) = cached.as_ref() {
            if at.elapsed() < REFRESH_INTERVAL {
                return value.clone();
            }
        }
        let value = scan(&self.root, now, SCAN_BUDGET);
        *cached = Some((Instant::now(), value.clone()));
        value
    }
}

fn excluded(name: &str) -> bool {
    let name = name.to_ascii_lowercase();
    matches!(
        name.as_str(),
        ".trash"
            | ".git"
            | ".cache"
            | "cache"
            | "node_modules"
            | "target"
            | "temp"
            | "tmp"
            | ".tmp"
            | "backup"
            | "backups"
            | ".backup"
            | ".backups"
    ) || name.starts_with(".backup-")
        || name.starts_with("backups-")
        || name.ends_with(".bak")
        || name.ends_with(".backup")
}

fn reparse(metadata: &fs::Metadata) -> bool {
    #[cfg(windows)]
    {
        use std::os::windows::fs::MetadataExt;
        metadata.file_attributes() & 0x400 != 0
    }
    #[cfg(not(windows))]
    {
        metadata.file_type().is_symlink()
    }
}

fn scan(root: &Path, now: u64, budget: Duration) -> Value {
    let started = Instant::now();
    let mut hashes = HashSet::new();
    let mut failures = 0usize;
    let mut duplicates = 0usize;
    let mut excluded_entries = 0usize;
    let mut visited = 0usize;
    let mut timed_out = false;
    let mut entry_limit = false;
    let mut root_read = false;
    let mut stack = vec![root.to_path_buf()];
    while let Some(directory) = stack.pop() {
        if started.elapsed() >= budget {
            timed_out = true;
            break;
        }
        let entries = match fs::read_dir(&directory) {
            Ok(entries) => {
                if directory == root {
                    root_read = true;
                }
                entries
            }
            Err(_) => {
                failures += 1;
                continue;
            }
        };
        for entry in entries {
            if started.elapsed() >= budget {
                timed_out = true;
                break;
            }
            visited += 1;
            if visited > MAX_ENTRIES {
                entry_limit = true;
                break;
            }
            let entry = match entry {
                Ok(entry) => entry,
                Err(_) => {
                    failures += 1;
                    continue;
                }
            };
            let name = entry.file_name();
            let name = name.to_string_lossy();
            if excluded(&name) {
                excluded_entries += 1;
                continue;
            }
            let path = entry.path();
            let metadata = match fs::symlink_metadata(&path) {
                Ok(metadata) => metadata,
                Err(_) => {
                    failures += 1;
                    continue;
                }
            };
            // Central definitions live here physically. Profile aliases and junctions
            // are never another inventory root, including loops and outside targets.
            if reparse(&metadata) {
                excluded_entries += 1;
            } else if metadata.is_dir() {
                stack.push(path);
            } else if metadata.is_file() && name.eq_ignore_ascii_case("SKILL.md") {
                let hash = (|| {
                    let file = fs::File::open(&path).ok()?;
                    let before = file.metadata().ok()?;
                    let mut bytes = Vec::new();
                    let mut reader = (&file).take(MAX_DEFINITION_BYTES + 1);
                    reader.read_to_end(&mut bytes).ok()?;
                    let after = file.metadata().ok()?;
                    if bytes.len() as u64 > MAX_DEFINITION_BYTES
                        || before.len() != bytes.len() as u64
                        || before.len() != after.len()
                        || before.modified().ok()? != after.modified().ok()?
                    {
                        return None;
                    }
                    // Content fingerprints also collapse independent copies/hardlinks.
                    // Only aggregate numbers leave this module.
                    Some(digest(&SHA256, &bytes).as_ref().to_vec())
                })();
                match hash {
                    Some(hash) => {
                        if !hashes.insert(hash) {
                            duplicates += 1;
                        }
                    }
                    None => failures += 1,
                }
            }
        }
        if timed_out || entry_limit {
            break;
        }
    }
    timed_out |= started.elapsed() >= budget;
    let complete = root_read && failures == 0 && !timed_out && !entry_limit;
    let state = if complete {
        "complete"
    } else if root_read || timed_out || entry_limit {
        "partial"
    } else {
        "unavailable"
    };
    json!({
        "state":state, "count":complete.then_some(hashes.len()),
        "checkedAt":now, "updatedAt":complete.then_some(now),
        "observedCount":hashes.len(), "duplicates":duplicates,
        "excludedEntries":excluded_entries, "failures":failures,
        "timedOut":timed_out, "entryLimit":entry_limit,
        "durationMs":started.elapsed().as_millis() as u64,
        "refreshSeconds":REFRESH_INTERVAL.as_secs()
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn definition(root: &Path, directory: &str, contents: &str) {
        let directory = root.join(directory);
        fs::create_dir_all(&directory).unwrap();
        fs::write(directory.join("SKILL.md"), contents).unwrap();
    }

    #[test]
    fn counts_system_and_synced_definitions_once_and_excludes_archives() {
        let root = tempfile::tempdir().unwrap();
        definition(root.path(), "first", "first definition");
        definition(root.path(), "copied", "first definition");
        definition(root.path(), ".system/second", "system definition");
        definition(root.path(), "synced/provider/third", "synced definition");
        for excluded in [
            ".trash/conflicts",
            "backups/old",
            ".backup-20261013/old",
            ".git/cache",
            "cache/old",
        ] {
            definition(root.path(), excluded, "archived definition");
        }
        fs::write(root.path().join("README.md"), "not a definition").unwrap();
        let value = scan(root.path(), 1234, SCAN_BUDGET);
        assert_eq!(value["state"], "complete");
        assert_eq!(value["count"], 3);
        assert_eq!(value["duplicates"], 1);
        assert_eq!(value["updatedAt"], 1234);
        let output = value.to_string();
        assert!(!output.contains("definition"));
        assert!(!output.contains(root.path().to_str().unwrap()));
    }

    #[test]
    fn empty_directory_is_zero_but_missing_directory_is_unavailable() {
        let root = tempfile::tempdir().unwrap();
        assert_eq!(scan(root.path(), 1, SCAN_BUDGET)["count"], 0);
        let missing = scan(&root.path().join("missing"), 2, SCAN_BUDGET);
        assert_eq!(missing["state"], "unavailable");
        assert!(missing["count"].is_null());
        assert!(missing["updatedAt"].is_null());
        assert_eq!(missing["failures"], 1);
    }

    #[test]
    fn partial_scan_never_publishes_an_exact_count() {
        let root = tempfile::tempdir().unwrap();
        definition(root.path(), "good", "good definition");
        let directory = root.path().join("oversized");
        fs::create_dir_all(&directory).unwrap();
        fs::File::create(directory.join("SKILL.md"))
            .unwrap()
            .set_len(MAX_DEFINITION_BYTES + 1)
            .unwrap();
        let value = scan(root.path(), 3, SCAN_BUDGET);
        assert_eq!(value["state"], "partial");
        assert_eq!(value["observedCount"], 1);
        assert!(value["count"].is_null());
        assert!(value["updatedAt"].is_null());
        assert_eq!(value["failures"], 1);
    }

    #[test]
    fn expired_budget_is_not_zero() {
        let root = tempfile::tempdir().unwrap();
        definition(root.path(), "good", "good definition");
        let value = scan(root.path(), 4, Duration::ZERO);
        assert_eq!(value["state"], "partial");
        assert!(value["count"].is_null());
        assert_eq!(value["timedOut"], true);
    }

    #[test]
    fn cached_snapshot_keeps_the_scan_timestamp_until_refresh() {
        let root = tempfile::tempdir().unwrap();
        definition(root.path(), "first", "first definition");
        let inventory = Inventory::new(root.path().into());
        assert_eq!(inventory.snapshot(10)["count"], 1);
        definition(root.path(), "second", "second definition");
        assert_eq!(inventory.snapshot(20)["count"], 1);
        assert_eq!(inventory.snapshot(20)["checkedAt"], 10);
        inventory.cached.lock().unwrap().as_mut().unwrap().0 = Instant::now() - REFRESH_INTERVAL;
        assert_eq!(inventory.snapshot(30)["count"], 2);
        assert_eq!(inventory.snapshot(30)["checkedAt"], 30);
    }

    #[test]
    fn hardlinked_definition_is_counted_once() {
        let root = tempfile::tempdir().unwrap();
        definition(root.path(), "first", "first definition");
        fs::create_dir(root.path().join("alias")).unwrap();
        fs::hard_link(
            root.path().join("first/SKILL.md"),
            root.path().join("alias/SKILL.md"),
        )
        .unwrap();
        assert_eq!(scan(root.path(), 5, SCAN_BUDGET)["count"], 1);
    }

    #[cfg(windows)]
    #[test]
    fn junction_cycle_and_profile_aliases_do_not_duplicate_definitions() {
        use std::os::windows::process::CommandExt;
        let root = tempfile::tempdir().unwrap();
        definition(root.path(), "first", "first definition");
        let junction = root.path().join("profile");
        let output = std::process::Command::new("cmd.exe")
            .args(["/d", "/c", "mklink", "/J"])
            .arg(&junction)
            .arg(root.path())
            .creation_flags(0x08000000)
            .output()
            .unwrap();
        assert!(output.status.success());
        let value = scan(root.path(), 6, SCAN_BUDGET);
        assert_eq!(value["state"], "complete");
        assert_eq!(value["count"], 1);
        assert_eq!(value["excludedEntries"], 1);
        fs::remove_dir(junction).unwrap();
    }
}
