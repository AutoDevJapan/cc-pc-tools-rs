//! Immutable note history segments. Listing and scheduling never hydrate them.
//! Callers hold the ordinary ToDo lock when archiving and saving the snapshot.
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    sync::atomic::{AtomicU64, Ordering},
};

type Result<T> = std::result::Result<T, String>;
static SEQUENCE: AtomicU64 = AtomicU64::new(0);
const FIELD: &str = "note_history_archives";
const DOMAIN: &str = "cc-todo-note-history-v1";

fn sha(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn fail(e: impl std::fmt::Display) -> String {
    format!("todo_history: {e}")
}
fn ordinary(path: &Path, directory: bool) -> Result<()> {
    let meta = fs::symlink_metadata(path).map_err(fail)?;
    #[cfg(windows)]
    {
        use std::os::windows::fs::MetadataExt;
        if meta.file_attributes() & 0x400 != 0 {
            return Err(fail("linked archive path"));
        }
    }
    if meta.file_type().is_symlink()
        || if directory {
            !meta.is_dir()
        } else {
            !meta.is_file()
        }
    {
        return Err(fail("unexpected archive path type"));
    }
    Ok(())
}
fn root(store: &Path) -> Result<PathBuf> {
    Ok(store
        .parent()
        .ok_or_else(|| fail("store parent missing"))?
        .join("note-history-v1"))
}
fn refs(item: &Value) -> Result<&[Value]> {
    match item.get(FIELD) {
        None => Ok(&[]),
        Some(Value::Array(items)) => Ok(items),
        _ => Err(fail("invalid archive references")),
    }
}
fn inline(item: &Value) -> Result<&[Value]> {
    match item.get("note_history") {
        None => Ok(&[]),
        Some(Value::Array(items)) => Ok(items),
        _ => Err(fail("invalid note history")),
    }
}
fn reference(item: &Value, value: &Value) -> Result<(String, usize)> {
    let id = item["id"]
        .as_i64()
        .filter(|id| *id > 0)
        .ok_or_else(|| fail("invalid item id"))?;
    let hash = value["sha256"]
        .as_str()
        .filter(|h| {
            h.len() == 64
                && h.bytes()
                    .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
        })
        .ok_or_else(|| fail("invalid archive digest"))?;
    let count = value["entries"]
        .as_u64()
        .and_then(|n| usize::try_from(n).ok())
        .filter(|n| *n > 0)
        .ok_or_else(|| fail("invalid archive count"))?;
    let file = format!("{id}-{hash}.json");
    if value["domain"] != DOMAIN || value["todo_id"] != id || value["file"] != file {
        return Err(fail("archive identity differs"));
    }
    Ok((file, count))
}
fn read_segment(store: &Path, item: &Value, value: &Value) -> Result<Vec<Value>> {
    let (name, count) = reference(item, value)?;
    let folder = root(store)?;
    ordinary(&folder, true)?;
    let path = folder.join(name);
    ordinary(&path, false)?;
    let bytes = crate::todo_store::read_bytes(&path).map_err(fail)?;
    if value["bytes"].as_u64() != Some(bytes.len() as u64) || value["sha256"] != sha(&bytes) {
        return Err(fail("archive bytes differ"));
    }
    let document: Value = serde_json::from_slice(&bytes).map_err(fail)?;
    if document["domain"] != DOMAIN || document["todo_id"] != item["id"] {
        return Err(fail("archive document identity differs"));
    }
    let entries = document["entries"]
        .as_array()
        .ok_or_else(|| fail("archive entries missing"))?;
    if entries.len() != count {
        return Err(fail("archive entry count differs"));
    }
    Ok(entries.clone())
}
/// Count only metadata; the board must not open every historical segment.
pub fn archived_count(item: &Value) -> Result<usize> {
    refs(item)?.iter().try_fold(0usize, |total, r| {
        total
            .checked_add(reference(item, r)?.1)
            .ok_or_else(|| fail("history count overflow"))
    })
}
/// Read only the segments intersecting the requested page, in original order.
pub fn page(store: &Path, item: &Value, offset: usize, limit: usize) -> Result<Value> {
    let total = archived_count(item)?
        .checked_add(inline(item)?.len())
        .ok_or_else(|| fail("history count overflow"))?;
    let mut skip = offset;
    let mut entries = Vec::new();
    for r in refs(item)? {
        let count = reference(item, r)?.1;
        if skip >= count {
            skip -= count;
            continue;
        }
        if entries.len() == limit {
            break;
        }
        entries.extend(
            read_segment(store, item, r)?
                .into_iter()
                .skip(skip)
                .take(limit - entries.len()),
        );
        skip = 0;
    }
    if entries.len() < limit {
        entries.extend(
            inline(item)?
                .iter()
                .skip(skip)
                .take(limit - entries.len())
                .cloned(),
        );
    }
    let next = offset.saturating_add(entries.len());
    Ok(
        json!({"id":item["id"],"entries":entries,"offset":offset,"total":total,"next_offset":if next < total { Some(next) } else { None }}),
    )
}
/// An explicit detail/history read may materialize a clone, never the hot store.
pub fn materialized(store: &Path, item: &Value) -> Result<Value> {
    let mut result = item.clone();
    if !refs(item)?.is_empty() {
        result["note_history"] = page(store, item, 0, usize::MAX)?["entries"].take();
        result
            .as_object_mut()
            .ok_or_else(|| fail("item is not an object"))?
            .remove(FIELD);
    }
    Ok(result)
}
#[cfg(windows)]
fn private(store: &Path, path: &Path) -> Result<()> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::Security::{
        DACL_SECURITY_INFORMATION, GetFileSecurityW, PROTECTED_DACL_SECURITY_INFORMATION,
        SetFileSecurityW,
    };
    // Copy the original store's audience, not the broader parent directory ACL.
    // Tests may stage an in-memory snapshot before creating their store file.
    let source = if store.try_exists().map_err(fail)? {
        store
    } else {
        store.parent().ok_or_else(|| fail("store parent missing"))?
    };
    let from: Vec<u16> = source.as_os_str().encode_wide().chain(Some(0)).collect();
    let to: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
    let mut needed = 0;
    unsafe {
        GetFileSecurityW(
            from.as_ptr(),
            DACL_SECURITY_INFORMATION,
            std::ptr::null_mut(),
            0,
            &mut needed,
        );
    }
    if needed == 0 || needed > 1024 * 1024 {
        return Err(fail("source permissions unavailable"));
    }
    let mut descriptor = vec![0u32; (needed as usize).div_ceil(4)];
    let pointer = descriptor.as_mut_ptr().cast();
    if unsafe {
        GetFileSecurityW(
            from.as_ptr(),
            DACL_SECURITY_INFORMATION,
            pointer,
            (descriptor.len() * 4) as u32,
            &mut needed,
        )
    } == 0
    {
        return Err(fail(std::io::Error::last_os_error()));
    }
    if unsafe {
        SetFileSecurityW(
            to.as_ptr(),
            DACL_SECURITY_INFORMATION | PROTECTED_DACL_SECURITY_INFORMATION,
            pointer,
        )
    } == 0
    {
        return Err(fail(std::io::Error::last_os_error()));
    }
    Ok(())
}
#[cfg(not(windows))]
fn private(_store: &Path, path: &Path) -> Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = if path.is_dir() { 0o700 } else { 0o600 };
        fs::set_permissions(path, fs::Permissions::from_mode(mode)).map_err(fail)?;
    }
    Ok(())
}
fn publish(store: &Path, folder: &Path, path: &Path, bytes: &[u8]) -> Result<()> {
    fs::create_dir_all(folder).map_err(fail)?;
    ordinary(folder, true)?;
    private(store, folder)?;
    if path.try_exists().map_err(fail)? {
        ordinary(path, false)?;
        if crate::todo_store::read_bytes(path).map_err(fail)? != bytes {
            return Err(fail("archive collision"));
        }
        private(store, path)?;
        return Ok(());
    }
    let temp = folder.join(format!(
        ".publish-{}-{}-{}.tmp",
        std::process::id(),
        SEQUENCE.fetch_add(1, Ordering::Relaxed),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_err(fail)?
            .as_nanos()
    ));
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&temp)
        .map_err(fail)?;
    let result = (|| {
        private(store, &temp)?;
        file.write_all(bytes).map_err(fail)?;
        file.sync_all().map_err(fail)?;
        // Same-volume create-if-absent publication. A crash never exposes half JSON.
        match fs::hard_link(&temp, path) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
                ordinary(path, false)?;
                if crate::todo_store::read_bytes(path).map_err(fail)? == bytes {
                    Ok(())
                } else {
                    Err(fail("archive collision"))
                }
            }
            Err(e) => Err(fail(e)),
        }
    })();
    drop(file);
    fs::remove_file(&temp).map_err(fail)?;
    result?;
    #[cfg(unix)]
    std::fs::File::open(folder)
        .and_then(|directory| directory.sync_all())
        .map_err(fail)?;
    Ok(())
}
/// Preview by default. Applying changes only the selected item's note history
/// and its immutable references. Persist the returned DB under the same lock.
pub fn archive(store: &Path, db: &mut Value, id: i64, apply: bool) -> Result<Value> {
    let item = db["items"]
        .as_array()
        .and_then(|items| items.iter().find(|i| i["id"] == id))
        .ok_or_else(|| fail("item not found"))?;
    let entries = inline(item)?;
    let existing_count = archived_count(item)?;
    let mut result = json!({"id":id,"apply":apply,"saved":false,"inline_entries":entries.len(),"archived_entries":existing_count});
    if entries.is_empty() {
        return Ok(result);
    }
    // Validate old references before adding another segment; never hide corruption.
    for r in refs(item)? {
        read_segment(store, item, r)?;
    }
    let bytes = serde_json::to_vec(&json!({"domain":DOMAIN,"todo_id":id,"entries":entries}))
        .map_err(fail)?;
    crate::todo_store::check_size(bytes.len()).map_err(fail)?;
    let hash = sha(&bytes);
    let file = format!("{id}-{hash}.json");
    let entry = json!({"domain":DOMAIN,"todo_id":id,"file":file,"sha256":hash,"bytes":bytes.len(),"entries":entries.len()});
    result["archive"] = entry.clone();
    if !apply {
        return Ok(result);
    }
    let folder = root(store)?;
    publish(store, &folder, &folder.join(&file), &bytes)?;
    // Independently reread the immutable file before the original history is removed.
    if read_segment(store, item, &entry)? != entries {
        return Err(fail("archive roundtrip differs"));
    }
    let item = db["items"]
        .as_array_mut()
        .unwrap()
        .iter_mut()
        .find(|i| i["id"] == id)
        .unwrap();
    let mut previous = refs(item)?.to_vec();
    previous.push(entry);
    item[FIELD] = Value::Array(previous);
    item["note_history"] = json!([]);
    result["saved"] = json!(true);
    Ok(result)
}

/// Reverse archival without deleting immutable segments. The normal snapshot
/// limit still applies; callers save under the ordinary shared lock.
pub fn restore(store: &Path, db: &mut Value, id: i64, apply: bool) -> Result<Value> {
    let item = db["items"]
        .as_array()
        .and_then(|items| items.iter().find(|i| i["id"] == id))
        .ok_or_else(|| fail("item not found"))?;
    let count = archived_count(item)?;
    let restored = materialized(store, item)?;
    let result = json!({"id":id,"apply":apply,"saved":apply && count > 0,"restored_entries":count});
    if apply && count > 0 {
        let item = db["items"]
            .as_array_mut()
            .unwrap()
            .iter_mut()
            .find(|i| i["id"] == id)
            .unwrap();
        *item = restored;
    }
    Ok(result)
}
