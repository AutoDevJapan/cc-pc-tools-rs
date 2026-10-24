//! Byte contract for the complete shared ToDo snapshot, never a filtered view.
use serde_json::Value;
use std::{
    fmt,
    fs::{self, File, OpenOptions},
    io::{self, Read, Write},
    path::Path,
    time::Duration,
};

/// Hard ceiling for the hot snapshot and each immutable history segment.
pub const MAX_BYTES: usize = 64 * 1024 * 1024;

#[derive(Debug)]
pub enum Error {
    TooLarge,
    Io(io::Error),
    InvalidJson(serde_json::Error),
    History(String),
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::TooLarge => write!(
                f,
                "todo_store_too_large: shared ToDo exceeds {MAX_BYTES} bytes (64 MiB)"
            ),
            Self::Io(e) => write!(f, "todo_store_io: {e}"),
            Self::InvalidJson(e) => write!(f, "todo_store_invalid_json: {e}"),
            Self::History(e) => write!(f, "todo_store_archive_failed: {e}"),
        }
    }
}
impl std::error::Error for Error {}

pub fn check_size(bytes: usize) -> Result<(), Error> {
    if bytes > MAX_BYTES {
        Err(Error::TooLarge)
    } else {
        Ok(())
    }
}

/// Save the complete canonical store without presentation redaction.
/// Callers hold the shared ToDo lock across their read, mutation and this write.
pub fn write(path: &Path, db: &Value) -> Result<(), Error> {
    write_with_limit(path, db, MAX_BYTES)
}

fn encode(db: &Value) -> Result<Vec<u8>, Error> {
    let mut bytes = Vec::new();
    let formatter = serde_json::ser::PrettyFormatter::with_indent(b" ");
    let mut serializer = serde_json::Serializer::with_formatter(&mut bytes, formatter);
    serde::Serialize::serialize(db, &mut serializer).map_err(Error::InvalidJson)?;
    Ok(bytes)
}

/// Explicit restoration must fit in full, without immediately archiving it again.
pub fn write_exact(path: &Path, db: &Value) -> Result<(), Error> {
    let bytes = encode(db)?;
    check_size(bytes.len())?;
    write_encoded(path, &bytes)
}

fn write_with_limit(path: &Path, db: &Value, limit: usize) -> Result<(), Error> {
    // Match todo.exe's UTF-8, LF, one-space indent and lack of a final newline.
    let mut bytes = encode(db)?;
    if bytes.len() <= limit {
        return write_encoded(path, &bytes);
    }
    // Clone only at the ceiling. The caller's canonical input is never truncated.
    let mut compacted = db.clone();
    let mut candidates = Vec::new();
    if let Some(items) = db["items"].as_array() {
        for (index, item) in items.iter().enumerate() {
            match item.get("note_history") {
                None => (),
                Some(Value::Array(entries)) if entries.is_empty() => (),
                Some(Value::Array(entries)) => candidates.push((
                    index,
                    serde_json::to_vec(entries)
                        .map_err(Error::InvalidJson)?
                        .len(),
                )),
                _ => return Err(Error::History("todo_history: invalid note history".into())),
            }
        }
    }
    // Stable ties preserve the original row order. Prefer fewer, larger segments.
    candidates.sort_by_key(|(_, size)| std::cmp::Reverse(*size));
    let goal = limit - limit / 16;
    let mut pending = Vec::new();
    for (index, _) in candidates {
        let item = &compacted["items"][index];
        if db["items"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|i| i["id"] == item["id"])
            .count()
            != 1
        {
            return Err(Error::History("todo_history: ambiguous item id".into()));
        }
        let Some(plan) = crate::todo_history::prepare(path, item).map_err(Error::History)? else {
            continue;
        };
        let original = std::mem::replace(&mut compacted["items"][index], plan.item.clone());
        let next = encode(&compacted)?;
        if next.len() >= bytes.len() {
            compacted["items"][index] = original;
            continue;
        }
        bytes = next;
        pending.push(plan);
        if bytes.len() <= goal {
            break;
        }
    }
    if bytes.len() > limit {
        return Err(Error::TooLarge);
    }
    // No archives are published for an irreducibly oversized active snapshot.
    // A crash after publication leaves reusable, unreferenced immutable segments.
    for plan in pending {
        plan.publish(path).map_err(Error::History)?;
    }
    write_encoded(path, &bytes)
}

fn write_encoded(path: &Path, bytes: &[u8]) -> Result<(), Error> {
    let mut backup = path.as_os_str().to_os_string();
    backup.push(".bak-20260930d");
    let backup = Path::new(&backup);
    if path.exists() && !backup.exists() {
        // Do not leave an empty backup if the original cannot be read.
        let original = fs::read(path).map_err(Error::Io)?;
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(backup)
            .map_err(Error::Io)?;
        file.write_all(&original).map_err(Error::Io)?;
    }
    let tmp = path.with_file_name(format!(
        "{}.{}.tmp",
        path.file_name().unwrap().to_string_lossy(),
        std::process::id()
    ));
    let mut file = File::create(&tmp).map_err(Error::Io)?;
    file.write_all(bytes).map_err(Error::Io)?;
    file.flush().map_err(Error::Io)?;
    file.sync_all().map_err(Error::Io)?;
    drop(file);
    for attempt in 0..50 {
        match replace(&tmp, path) {
            Ok(()) => return Ok(()),
            Err(e)
                if (e.kind() == io::ErrorKind::PermissionDenied
                    || matches!(e.raw_os_error(), Some(5 | 32 | 33)))
                    && attempt < 49 =>
            {
                std::thread::sleep(Duration::from_millis(20))
            }
            Err(e) => return Err(Error::Io(e)),
        }
    }
    unreachable!()
}

#[cfg(windows)]
fn replace(from: &Path, to: &Path) -> io::Result<()> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::Storage::FileSystem::{MOVEFILE_REPLACE_EXISTING, MoveFileExW};

    let from: Vec<u16> = from.as_os_str().encode_wide().chain(Some(0)).collect();
    let to: Vec<u16> = to.as_os_str().encode_wide().chain(Some(0)).collect();
    if unsafe { MoveFileExW(from.as_ptr(), to.as_ptr(), MOVEFILE_REPLACE_EXISTING) } == 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}

#[cfg(not(windows))]
fn replace(from: &Path, to: &Path) -> io::Result<()> {
    fs::rename(from, to)
}

/// Bound actual reads, including a file which grows after it is opened.
/// Parsing uses exactly the bytes read; redaction belongs to presentation.
pub fn read(path: &Path) -> Result<Value, Error> {
    read_bounded(path, MAX_BYTES)
}
/// Preserve immutable history bytes for independent digest validation.
pub fn read_bytes(path: &Path) -> Result<Vec<u8>, Error> {
    let mut bytes = Vec::new();
    File::open(path)
        .map_err(Error::Io)?
        .take(MAX_BYTES as u64 + 1)
        .read_to_end(&mut bytes)
        .map_err(Error::Io)?;
    check_size(bytes.len())?;
    Ok(bytes)
}
/// Shared byte primitive; each canonical store supplies its own limit and error labels.
pub fn read_bounded(path: &Path, limit: usize) -> Result<Value, Error> {
    read_from_limit(File::open(path).map_err(Error::Io)?, limit)
}
#[cfg(test)]
fn read_from(reader: impl Read) -> Result<Value, Error> {
    read_from_limit(reader, MAX_BYTES)
}
fn read_from_limit(reader: impl Read, limit: usize) -> Result<Value, Error> {
    let mut bytes = Vec::new();
    reader
        .take(limit.saturating_add(1) as u64)
        .read_to_end(&mut bytes)
        .map_err(Error::Io)?;
    if bytes.len() > limit {
        return Err(Error::TooLarge);
    }
    let bytes = bytes.strip_prefix(&[0xef, 0xbb, 0xbf]).unwrap_or(&bytes);
    serde_json::from_slice(bytes).map_err(Error::InvalidJson)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn capacity_fixture() -> Value {
        json!({"items":[{"id":1,"note":"current","spec":"request.md","status":"wait",
            "jobs":["original"],"wait_on":[2],"owner_reply_id":"original-answer",
            "unknown":{"Key":1,"key":2},"note_history":[
                {"note":"匿名の原文🦀\n".repeat(300),"at":"old","extra":[1,null]},
                "duplicate", "duplicate"]},
            {"id":2,"status":"doing","note":"other"}],"next_id":3,"unknown_root":true})
    }

    #[test]
    fn oversized_write_archives_history_and_keeps_every_current_field() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("todo.json");
        let before = capacity_fixture();
        // An ordinary mutation crosses the small test-only threshold.
        write(&path, &before).unwrap();
        let limit = fs::metadata(&path).unwrap().len() as usize;
        let mut changed = before.clone();
        changed["items"][0]["note"] = json!("new current");
        write_with_limit(&path, &changed, limit).unwrap();
        let saved = read(&path).unwrap();
        assert!(fs::metadata(&path).unwrap().len() as usize <= limit);
        assert!(saved["items"][0]["note_history_archives"].is_array());
        assert_eq!(
            crate::todo_history::materialized(&path, &saved["items"][0]).unwrap(),
            changed["items"][0]
        );
        assert_eq!(saved["items"][1], changed["items"][1]);
        assert_eq!(saved["unknown_root"], changed["unknown_root"]);
        assert_eq!(
            changed["items"][0]["note_history"],
            before["items"][0]["note_history"]
        );
    }

    #[test]
    fn repeated_overflow_pages_and_explicit_restore_keep_order_duplicates_and_unknowns() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("todo.json");
        let mut expected = capacity_fixture();
        write_with_limit(&path, &expected, 4096).unwrap();
        for cycle in 0..3 {
            let mut saved = read(&path).unwrap();
            let more = json!({"note":format!("{cycle} {}", "匿名🦀\n".repeat(400)),"unknown":[null,cycle]});
            saved["items"][0]["note_history"] = json!([more.clone()]);
            expected["items"][0]["note_history"]
                .as_array_mut()
                .unwrap()
                .push(more);
            write_with_limit(&path, &saved, 4096).unwrap();
            let saved = read(&path).unwrap();
            // Retry of the committed snapshot adds no segment or duplicate entry.
            write_with_limit(&path, &saved, 4096).unwrap();
        }
        let saved = read(&path).unwrap();
        assert_eq!(
            saved["items"][0]["note_history_archives"]
                .as_array()
                .unwrap()
                .len(),
            4
        );
        let mut all = Vec::new();
        for offset in (0..6).step_by(2) {
            all.extend(
                crate::todo_history::page(&path, &saved["items"][0], offset, 2).unwrap()["entries"]
                    .as_array()
                    .unwrap()
                    .clone(),
            );
        }
        assert_eq!(Value::Array(all), expected["items"][0]["note_history"]);
        let mut restored = saved;
        crate::todo_history::restore(&path, &mut restored, 1, true).unwrap();
        assert_eq!(restored, expected);
        write_exact(&path, &restored).unwrap();
        assert_eq!(read(&path).unwrap(), expected);
        assert_eq!(
            fs::read_dir(temp.path().join("note-history-v1"))
                .unwrap()
                .count(),
            4
        );
    }

    #[test]
    fn largest_history_is_selected_and_small_histories_and_unneeded_segments_stay_untouched() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("todo.json");
        let mut db = capacity_fixture();
        db["items"][1]["note_history"] = json!(["small"]);
        write_with_limit(&path, &db, 4096).unwrap();
        let saved = read(&path).unwrap();
        assert!(
            saved["items"][0]["note_history"]
                .as_array()
                .unwrap()
                .is_empty()
        );
        assert_eq!(saved["items"][1], db["items"][1]);
        let file = saved["items"][0]["note_history_archives"][0]["file"]
            .as_str()
            .unwrap();
        fs::remove_file(temp.path().join("note-history-v1").join(file)).unwrap();
        // An ordinary under-limit save does not hydrate unrelated history.
        write_with_limit(&path, &saved, 4096).unwrap();
        assert_eq!(read(&path).unwrap(), saved);
        assert!(crate::todo_history::page(&path, &saved["items"][0], 0, 1).is_err());
    }

    #[test]
    fn irreducible_active_data_and_expanding_tiny_archives_publish_nothing() {
        for large_history in [false, true] {
            let temp = tempfile::tempdir().unwrap();
            let path = temp.path().join("todo.json");
            let mut db = capacity_fixture();
            db["items"][0]["note"] = json!("current".repeat(2000));
            if !large_history {
                db["items"][0]["note_history"] = json!(["x"]);
            }
            write_exact(&path, &db).unwrap();
            let before = fs::read(&path).unwrap();
            let limit = if large_history {
                4096
            } else {
                before.len() - 1
            };
            assert!(matches!(
                write_with_limit(&path, &db, limit),
                Err(Error::TooLarge)
            ));
            assert_eq!(fs::read(&path).unwrap(), before);
            assert_eq!(fs::read_dir(temp.path()).unwrap().count(), 1);
        }
    }

    #[test]
    fn failed_publication_keeps_original_and_retry_reuses_the_same_history() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("todo.json");
        let db = capacity_fixture();
        write_exact(&path, &db).unwrap();
        let before = fs::read(&path).unwrap();
        let folder = temp.path().join("note-history-v1");
        fs::write(&folder, b"obstruction").unwrap();
        let error = write_with_limit(&path, &db, 4096).unwrap_err();
        assert!(matches!(error, Error::History(_)));
        assert!(
            error
                .to_string()
                .starts_with("todo_store_archive_failed: todo_history:")
        );
        assert_eq!(fs::read(&path).unwrap(), before);
        fs::remove_file(&folder).unwrap();
        write_with_limit(&path, &db, 4096).unwrap();
        assert_eq!(fs::read_dir(folder).unwrap().count(), 1);
        let saved = read(&path).unwrap();
        assert_eq!(
            crate::todo_history::materialized(&path, &saved["items"][0]).unwrap(),
            db["items"][0]
        );
    }

    #[test]
    fn failed_snapshot_after_publication_keeps_original_and_retry_has_no_double_write() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("todo.json");
        let db = capacity_fixture();
        write_exact(&path, &db).unwrap();
        let before = fs::read(&path).unwrap();
        let pending = path.with_file_name(format!("todo.json.{}.tmp", std::process::id()));
        fs::create_dir(&pending).unwrap();
        assert!(matches!(
            write_with_limit(&path, &db, 4096),
            Err(Error::Io(_))
        ));
        assert_eq!(fs::read(&path).unwrap(), before);
        let folder = temp.path().join("note-history-v1");
        assert_eq!(fs::read_dir(&folder).unwrap().count(), 1);
        fs::remove_dir(pending).unwrap();
        write_with_limit(&path, &db, 4096).unwrap();
        assert_eq!(fs::read_dir(folder).unwrap().count(), 1);
        let saved = read(&path).unwrap();
        assert_eq!(
            saved["items"][0]["note_history_archives"]
                .as_array()
                .unwrap()
                .len(),
            1
        );
        assert_eq!(
            crate::todo_history::materialized(&path, &saved["items"][0]).unwrap(),
            db["items"][0]
        );
        assert_eq!(
            fs::read(path.with_file_name("todo.json.bak-20260930d")).unwrap(),
            before
        );
    }

    #[test]
    fn corrupted_existing_segment_or_duplicate_id_does_not_commit_new_references() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("todo.json");
        let db = capacity_fixture();
        write_with_limit(&path, &db, 4096).unwrap();
        let mut saved = read(&path).unwrap();
        let file = saved["items"][0]["note_history_archives"][0]["file"]
            .as_str()
            .unwrap();
        let segment = temp.path().join("note-history-v1").join(file);
        let original_segment = fs::read(&segment).unwrap();
        saved["items"][0]["note_history"] = db["items"][0]["note_history"].clone();
        let before = fs::read(&path).unwrap();
        fs::write(&segment, b"{}").unwrap();
        let error = write_with_limit(&path, &saved, 4096).unwrap_err();
        assert!(error.to_string().contains("archive bytes differ"));
        assert_eq!(fs::read(&path).unwrap(), before);
        assert_eq!(
            fs::read_dir(temp.path().join("note-history-v1"))
                .unwrap()
                .count(),
            1
        );
        fs::write(segment, original_segment).unwrap();
        saved["items"].as_array_mut().unwrap().push(json!({"id":1}));
        assert!(
            write_with_limit(&path, &saved, 4096)
                .unwrap_err()
                .to_string()
                .contains("ambiguous item id")
        );
        assert_eq!(fs::read(&path).unwrap(), before);
    }

    #[test]
    fn complete_json_above_old_ceiling_keeps_every_field() {
        let db = json!({"projects":{"demo":"demo"},"items":[
            {"id":1,"spec":"saved.md","needs":["cargo"],"subwork":{"parent_todo_id":2},
             "console_creation_request":{"request_id":"manual:finite"},"note_history":["x".repeat(16*1024*1024)]},
            {"id":2,"status":"doing","unknown":{"retain":true}}
        ],"next_id":3,"stalls":{"demo":{"key":"queued-canonical"}}});
        let bytes = serde_json::to_vec(&db).unwrap();
        assert!(bytes.len() > 16 * 1024 * 1024);
        assert_eq!(read_from(bytes.as_slice()).unwrap(), db);
    }

    #[test]
    fn exact_limit_is_allowed_and_excess_is_not_partial_json() {
        let mut bytes = b"{}".to_vec();
        bytes.resize(MAX_BYTES, b' ');
        assert_eq!(read_from(bytes.as_slice()).unwrap(), json!({}));
        bytes.push(b' ');
        assert!(matches!(read_from(bytes.as_slice()), Err(Error::TooLarge)));
        assert!(matches!(check_size(MAX_BYTES + 1), Err(Error::TooLarge)));
    }

    #[test]
    fn corruption_and_io_have_separate_reasons() {
        assert!(matches!(
            read_from(&b"{broken"[..]),
            Err(Error::InvalidJson(_))
        ));
        struct Denied;
        impl Read for Denied {
            fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
                Err(io::ErrorKind::PermissionDenied.into())
            }
        }
        assert!(
            matches!(read_from(Denied), Err(Error::Io(e)) if e.kind() == io::ErrorKind::PermissionDenied)
        );
        let temp = tempfile::tempdir().unwrap();
        assert!(
            matches!(read(&temp.path().join("absent")), Err(Error::Io(e)) if e.kind() == io::ErrorKind::NotFound)
        );
    }
}
