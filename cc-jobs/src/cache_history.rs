//! Only files first created under the gate are owned. Existing cache files are never adopted.
//! Every gated command conservatively counts as use of every owned file (Cargo can reuse
//! dependencies without changing their timestamps). Thus reclamation requires seven days
//! without gated use; filesystem mtime alone is never evidence of disuse.
use crate::FileLock;
use serde::{Deserialize, Serialize};
use std::os::windows::fs::MetadataExt;
use std::{
    collections::BTreeMap,
    fs, io,
    path::{Component, Path},
    time::{SystemTime, UNIX_EPOCH},
};

const WEEK: u64 = 7 * 86400;
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Stamp {
    bytes: u64,
    modified: u64,
    created: u64,
}
impl Stamp {
    pub fn bytes(&self) -> u64 {
        self.bytes
    }
}
pub type Inventory = BTreeMap<String, Stamp>;
#[derive(Serialize, Deserialize)]
struct Entry {
    stamp: Stamp,
    last_use: u64,
    producer: String,
    branch: String,
}
#[derive(Default, Serialize, Deserialize)]
pub struct History {
    entries: BTreeMap<String, Entry>,
}

pub fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}
fn safe_relative(s: &str) -> bool {
    !s.is_empty()
        && !s.contains(':')
        && Path::new(s)
            .components()
            .all(|c| matches!(c, Component::Normal(_)))
}
fn artifact(s: &str) -> bool {
    // OUT_DIR files must never be removed independently of build-script fingerprints.
    // Keep both build/ and .fingerprint/ intact; deps output removal makes Cargo rebuild.
    safe_relative(s)
        && !Path::new(s)
            .components()
            .any(|c| matches!(c, Component::Normal(n) if n == "build" || n == ".fingerprint"))
        && !s.ends_with("lock")
        && Path::new(s).components().count() > 1
}
fn stamp(m: &fs::Metadata) -> Stamp {
    Stamp {
        bytes: m.len(),
        modified: m.last_write_time(),
        created: m.creation_time(),
    }
}
pub fn inventory(root: &Path) -> io::Result<Inventory> {
    fn walk(root: &Path, dir: &Path, out: &mut Inventory) -> io::Result<()> {
        if fs::symlink_metadata(dir)?.file_attributes() & 0x400 != 0 {
            return Err(io::Error::other("cache reparse point"));
        }
        let entries = match fs::read_dir(dir) {
            Ok(e) => e,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
            Err(e) => return Err(e),
        };
        for e in entries {
            let p = match e {
                Ok(e) => e.path(),
                Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e),
            };
            let m = match fs::symlink_metadata(&p) {
                Ok(m) => m,
                Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e),
            };
            if m.file_attributes() & 0x400 != 0 {
                return Err(io::Error::other("cache reparse point"));
            }
            if m.is_dir() {
                match walk(root, &p, out) {
                    Err(e) if e.kind() == io::ErrorKind::NotFound => (),
                    result => result?,
                }
            } else {
                out.insert(
                    p.strip_prefix(root).unwrap().to_string_lossy().into_owned(),
                    stamp(&m),
                );
            }
        }
        Ok(())
    }
    let mut out = Inventory::new();
    if root.exists() {
        walk(root, root, &mut out)?;
    }
    Ok(out)
}
impl History {
    pub fn load(path: &Path) -> io::Result<Self> {
        match fs::read(path) {
            Ok(b) => match serde_json::from_slice(&b) {
                Ok(h) => Ok(h),
                Err(e) => {
                    let backup = path.with_extension(format!(
                        "corrupt-{}.json",
                        SystemTime::now()
                            .duration_since(UNIX_EPOCH)
                            .unwrap_or_default()
                            .as_nanos()
                    ));
                    fs::write(backup, b)?;
                    eprintln!("Cargo cache ownership corrupt; retaining all unowned files: {e}");
                    Ok(Self::default())
                }
            },
            Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(Self::default()),
            Err(e) => Err(e),
        }
    }
    pub fn save(&self, path: &Path) -> io::Result<()> {
        // Under the fixed gate lock; rename is atomic, including replacement on Windows.
        let tmp = path.with_extension("tmp");
        fs::write(&tmp, serde_json::to_vec(self).map_err(io::Error::other)?)?;
        fs::rename(tmp, path)
    }
    pub fn record(
        &mut self,
        before: &Inventory,
        after: &Inventory,
        at: u64,
        producer: &str,
        branch: &str,
    ) {
        self.entries
            .retain(|p, e| before.get(p) == Some(&e.stamp) && after.contains_key(p) && artifact(p));
        for (p, e) in &mut self.entries {
            if e.stamp != after[p] {
                e.stamp = after[p].clone();
                e.producer = producer.into();
                e.branch = branch.into();
            }
        }
        for (p, s) in after {
            // Locks and Cargo's root bookkeeping are not build artifacts.
            if !before.contains_key(p) && artifact(p) {
                self.entries.insert(
                    p.clone(),
                    Entry {
                        stamp: s.clone(),
                        last_use: at,
                        producer: producer.into(),
                        branch: branch.into(),
                    },
                );
            }
        }
        for e in self.entries.values_mut() {
            e.last_use = at;
        }
    }
    pub fn reclaim(&mut self, root: &Path, at: u64, needed: u64) -> io::Result<u64> {
        if needed == 0 || !root.exists() {
            return Ok(0);
        }
        // Also take Cargo's own profile locks: direct Cargo users do not take our gate.
        // Hold build and artifact lock bytes for every profile until reclamation finishes.
        let scan = inventory(root)?;
        let mut locks = Vec::new();
        for p in scan.keys().filter(|p| {
            p.ends_with(".cargo-lock")
                || p.ends_with(".cargo-build-lock")
                || p.ends_with(".cargo-artifact-lock")
        }) {
            match FileLock::try_acquire(&root.join(p)) {
                Ok(l) => locks.push(l),
                Err(_) => return Ok(0),
            }
        }
        let mut candidates: Vec<_> = self
            .entries
            .iter()
            .filter(|(p, e)| {
                artifact(p)
                    && at.saturating_sub(e.last_use) >= WEEK
                    && scan.get(*p) == Some(&e.stamp)
            })
            .map(|(p, e)| (p.clone(), e.last_use))
            .collect();
        candidates.sort_by_key(|(_, time)| *time);
        let mut reclaimed = 0;
        for (p, _) in candidates {
            if reclaimed >= needed {
                break;
            }
            let path = root.join(&p);
            // Never unlink hardlinks: this deliberately retains even links internal to target.
            // It avoids touching names outside this tree without weakening safe-delete rules.
            let file = match fs::File::open(&path) {
                Ok(f) => f,
                Err(_) => continue,
            };
            let m = match file.metadata() {
                Ok(m) => m,
                Err(_) => continue,
            };
            if m.file_attributes() & (0x400 | 1 | 2) != 0 || links(&file).ok() != Some(1) {
                continue;
            }
            if stamp(&m) != self.entries[&p].stamp {
                continue;
            }
            drop(file);
            match fs::remove_file(&path) {
                Ok(()) => {
                    reclaimed += m.len();
                    self.entries.remove(&p);
                }
                Err(e) => {
                    // Same-drive quarantine, without retrying deletion or overwriting a name.
                    match quarantine(root, &path, m.len(), &e) {
                        Ok(()) => {
                            reclaimed += m.len();
                            self.entries.remove(&p);
                        }
                        Err(move_error) => eprintln!(
                            "Cargo cache cleanup retained {}: delete={e}; quarantine={move_error}",
                            path.display()
                        ),
                    }
                }
            }
        }
        Ok(reclaimed)
    }
}
fn quarantine(root: &Path, path: &Path, bytes: u64, reason: &io::Error) -> io::Result<()> {
    let volume = root
        .ancestors()
        .last()
        .ok_or_else(|| io::Error::other("missing volume"))?;
    let parent = volume.join("削除希望");
    fs::create_dir_all(&parent)?;
    for p in parent.ancestors() {
        if fs::symlink_metadata(p)?.file_attributes() & 0x400 != 0 {
            return Err(io::Error::other("quarantine reparse point"));
        }
    }
    let id = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(io::Error::other)?
        .as_nanos();
    let dest = parent.join(format!(
        "{}-cargo-cache-生成物-{id}",
        chrono::Local::now().format("%Y%m%d")
    ));
    fs::create_dir(&dest)?;
    let receipt = dest.join("理由.txt");
    fs::write(
        &receipt,
        format!(
            "元の場所: {}\n大きさ: {bytes} bytes\n削除できなかった理由: {reason}\n移動: 未完了\n",
            path.display()
        ),
    )?;
    fs::rename(path, dest.join(path.file_name().unwrap()))?;
    use std::io::Write;
    fs::OpenOptions::new()
        .append(true)
        .open(receipt)?
        .write_all("移動: 完了\n".as_bytes())?;
    Ok(())
}
fn links(file: &fs::File) -> io::Result<u32> {
    use std::os::windows::io::AsRawHandle;
    use windows_sys::Win32::Storage::FileSystem::{
        BY_HANDLE_FILE_INFORMATION, GetFileInformationByHandle,
    };
    let mut info: BY_HANDLE_FILE_INFORMATION = unsafe { std::mem::zeroed() };
    if unsafe { GetFileInformationByHandle(file.as_raw_handle() as _, &mut info) } == 0 {
        return Err(io::Error::last_os_error());
    }
    Ok(info.nNumberOfLinks)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn test_root() -> std::path::PathBuf {
        std::env::temp_dir().join(format!(
            "cc-cache-history-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ))
    }
    #[test]
    fn legacy_is_not_owned_and_new_files_need_seven_days() {
        let root = test_root();
        fs::create_dir_all(root.join("debug/deps")).unwrap();
        fs::write(root.join("debug/deps/legacy.rlib"), b"legacy").unwrap();
        let before = inventory(&root).unwrap();
        fs::write(root.join("debug/deps/new.rlib"), b"new").unwrap();
        let after = inventory(&root).unwrap();
        let mut h = History::default();
        h.record(&before, &after, 100, "repo", "branch");
        assert_eq!(h.entries.len(), 1);
        assert_eq!(h.reclaim(&root, 100 + WEEK - 1, 1).unwrap(), 0);
        assert_eq!(h.reclaim(&root, 100 + WEEK, 1).unwrap(), 3);
        assert!(root.join("debug/deps/legacy.rlib").exists());
        assert!(!root.join("debug/deps/new.rlib").exists());
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn reuse_refreshes_age_and_external_hardlinks_are_retained() {
        let root = test_root();
        fs::create_dir_all(root.join("debug/deps")).unwrap();
        let before = inventory(&root).unwrap();
        let p = root.join("debug/deps/new.rlib");
        fs::write(&p, b"data").unwrap();
        let after = inventory(&root).unwrap();
        let mut h = History::default();
        h.record(&before, &after, 100, "repo", "branch");
        h.record(&after, &after, 100 + WEEK, "other", "branch");
        assert_eq!(h.reclaim(&root, 100 + WEEK + 1, 1).unwrap(), 0);
        fs::hard_link(&p, root.join("outside.rlib")).unwrap();
        assert_eq!(h.reclaim(&root, 100 + WEEK * 2, 1).unwrap(), 0);
        assert!(p.exists());
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn path_escape_and_replaced_files_are_retained() {
        assert!(!safe_relative("../file"));
        assert!(!safe_relative(r"C:\file"));
        assert!(!safe_relative(r"debug\item:stream"));
        let root = test_root();
        fs::create_dir_all(root.join("debug")).unwrap();
        let before = inventory(&root).unwrap();
        fs::write(root.join("debug/new"), b"new").unwrap();
        let after = inventory(&root).unwrap();
        let mut h = History::default();
        h.record(&before, &after, 100, "repo", "branch");
        fs::write(root.join("debug/new"), b"changed").unwrap();
        assert_eq!(h.reclaim(&root, 100 + WEEK, 1).unwrap(), 0);
        assert_eq!(fs::read(root.join("debug/new")).unwrap(), b"changed");
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn build_script_outputs_and_fingerprints_are_never_owned() {
        let root = test_root();
        fs::create_dir_all(root.join("debug/build/foo/out")).unwrap();
        fs::create_dir_all(root.join("debug/.fingerprint/foo")).unwrap();
        let before = inventory(&root).unwrap();
        fs::write(root.join("debug/build/foo/out/generated.rs"), b"generated").unwrap();
        fs::write(
            root.join("debug/.fingerprint/foo/run-build-script"),
            b"fresh",
        )
        .unwrap();
        let after = inventory(&root).unwrap();
        let mut h = History::default();
        h.record(&before, &after, 100, "repo", "branch");
        assert!(h.entries.is_empty());
        assert_eq!(h.reclaim(&root, 100 + WEEK, 1).unwrap(), 0);
        assert!(root.join("debug/build/foo/out/generated.rs").exists());
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn active_cargo_profile_lock_prevents_reclamation() {
        let root = test_root();
        fs::create_dir_all(root.join("debug/deps")).unwrap();
        let before = inventory(&root).unwrap();
        fs::write(root.join("debug/deps/new.rlib"), b"new").unwrap();
        let after = inventory(&root).unwrap();
        let mut h = History::default();
        h.record(&before, &after, 100, "repo", "branch");
        let lock = FileLock::try_acquire(&root.join("debug/.cargo-lock")).unwrap();
        assert_eq!(h.reclaim(&root, 100 + WEEK, 1).unwrap(), 0);
        drop(lock);
        assert_eq!(h.reclaim(&root, 100 + WEEK, 1).unwrap(), 3);
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn damaged_history_retains_legacy_and_preserves_bad_record() {
        let root = test_root();
        fs::create_dir_all(&root).unwrap();
        let p = root.join("ownership.json");
        fs::write(&p, b"broken").unwrap();
        let h = History::load(&p).unwrap();
        assert!(h.entries.is_empty());
        assert!(fs::read_dir(&root).unwrap().any(|e| {
            e.unwrap()
                .file_name()
                .to_string_lossy()
                .contains("corrupt-")
        }));
        fs::remove_dir_all(root).unwrap();
    }
}
