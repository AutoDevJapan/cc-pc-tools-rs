use cc_job_policy::todo_store::{self, Error, MAX_BYTES};
use serde_json::{Value, json};
use std::{fs, path::Path};

fn temporary(path: &Path) -> std::path::PathBuf {
    path.with_file_name(format!(
        "{}.{}.tmp",
        path.file_name().unwrap().to_string_lossy(),
        std::process::id()
    ))
}

#[test]
fn canonical_snapshot_keeps_literal_values_key_order_unicode_and_first_backup() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    let backup = dir.path().join("todo.json.bak-20260930d");
    let original = b"{\"previous\":true}\r\n";
    fs::write(&path, original).unwrap();
    let expected = r#"{
 "version": 1,
 "items": [
  {
   "id": 1,
   "note": "token: fixture-value",
   "licenseKey": "fixture-value",
   "Key": "first",
   "key": "second",
   "未知": {
    "z": "日本語🦀",
    "a": [
     true,
     null,
     0.12345678901234566
    ]
   }
  }
 ],
 "unknownRoot": "末尾"
}"#;
    let db: Value = serde_json::from_str(expected).unwrap();
    todo_store::write(&path, &db).unwrap();
    assert_eq!(fs::read(&path).unwrap(), expected.as_bytes());
    assert_eq!(todo_store::read(&path).unwrap(), db);
    assert_eq!(fs::read(&backup).unwrap(), original);
    assert!(!temporary(&path).exists());

    todo_store::write(&path, &json!({"next":true})).unwrap();
    assert_eq!(fs::read(&backup).unwrap(), original);
    assert!(!temporary(&path).exists());
}

#[test]
fn formatted_size_limit_rejects_a_smaller_compact_snapshot_without_touching_files() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    let backup = dir.path().join("todo.json.bak-20260930d");
    let tmp = temporary(&path);
    let compact_overhead = serde_json::to_vec(&json!({"items":[{"note":""}]}))
        .unwrap()
        .len();
    let db = json!({"items":[{"note":"x".repeat(MAX_BYTES - compact_overhead - 1)}]});
    assert_eq!(serde_json::to_vec(&db).unwrap().len(), MAX_BYTES - 1);

    assert!(matches!(
        todo_store::write(&path, &db),
        Err(Error::TooLarge)
    ));
    assert_eq!(fs::read_dir(dir.path()).unwrap().count(), 0);

    let original = b"{\"items\":[{\"id\":1,\"note\":\"original\"}]}";
    let old_backup = b"previous backup bytes\r\n";
    let old_temp = b"pending temporary bytes\n";
    fs::write(&path, original).unwrap();
    fs::write(&backup, old_backup).unwrap();
    fs::write(&tmp, old_temp).unwrap();
    assert!(matches!(
        todo_store::write(&path, &db),
        Err(Error::TooLarge)
    ));
    assert_eq!(fs::read(&path).unwrap(), original);
    assert_eq!(fs::read(&backup).unwrap(), old_backup);
    assert_eq!(fs::read(&tmp).unwrap(), old_temp);
}

#[test]
fn temporary_write_failure_keeps_the_original_backup_and_existing_temporary() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    let backup = dir.path().join("todo.json.bak-20260930d");
    let tmp = temporary(&path);
    let original = b"{\"previous\":true}\r\n";
    let old_backup = b"first backup bytes";
    fs::write(&path, original).unwrap();
    fs::write(&backup, old_backup).unwrap();
    fs::create_dir(&tmp).unwrap();
    fs::write(tmp.join("pending"), b"pending temporary bytes").unwrap();

    assert!(matches!(
        todo_store::write(&path, &json!({"replacement":true})),
        Err(Error::Io(_))
    ));
    assert_eq!(fs::read(&path).unwrap(), original);
    assert_eq!(fs::read(&backup).unwrap(), old_backup);
    assert_eq!(
        fs::read(tmp.join("pending")).unwrap(),
        b"pending temporary bytes"
    );
}

#[test]
fn exact_formatted_limit_is_saved_and_one_more_byte_preserves_every_file() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    // Count the exact one-space formatted overhead, including the line breaks.
    let db = json!({"note":"x".repeat(MAX_BYTES - b"{\n \"note\": \"\"\n}".len())});
    todo_store::write(&path, &db).unwrap();
    let original = fs::read(&path).unwrap();
    assert_eq!(original.len(), MAX_BYTES);
    assert_eq!(todo_store::read(&path).unwrap(), db);
    assert!(!temporary(&path).exists());

    let backup = dir.path().join("todo.json.bak-20260930d");
    let mut too_large = db;
    too_large["note"] = json!(format!("{}x", too_large["note"].as_str().unwrap()));
    assert!(matches!(
        todo_store::write(&path, &too_large),
        Err(Error::TooLarge)
    ));
    assert_eq!(fs::read(&path).unwrap(), original);
    assert!(!backup.exists());
    assert!(!temporary(&path).exists());
}

#[test]
fn unreadable_original_does_not_create_an_empty_first_backup_or_touch_temporary() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    let tmp = temporary(&path);
    fs::create_dir(&path).unwrap();
    fs::write(path.join("original"), b"original fixture bytes").unwrap();
    fs::write(&tmp, b"pending temporary bytes").unwrap();
    assert!(matches!(
        todo_store::write(&path, &json!({"replacement":true})),
        Err(Error::Io(_))
    ));
    assert_eq!(
        fs::read(path.join("original")).unwrap(),
        b"original fixture bytes"
    );
    assert!(!dir.path().join("todo.json.bak-20260930d").exists());
    assert_eq!(fs::read(&tmp).unwrap(), b"pending temporary bytes");
}

#[cfg(windows)]
#[test]
fn sharing_retry_exhaustion_keeps_original_first_backup_and_complete_temporary() {
    use std::os::windows::fs::OpenOptionsExt;

    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    let backup = dir.path().join("todo.json.bak-20260930d");
    let original = b"{\"original\":true}\r\n";
    fs::write(&path, original).unwrap();
    // Reading and writing remain possible, but MoveFileExW cannot replace this file.
    let handle = fs::OpenOptions::new()
        .read(true)
        .share_mode(3)
        .open(&path)
        .unwrap();
    let replacement = json!({"replacement":"日本語🦀"});
    let error = todo_store::write(&path, &replacement).unwrap_err();
    assert!(matches!(error, Error::Io(ref e) if matches!(e.raw_os_error(), Some(5 | 32 | 33))));
    assert_eq!(fs::read(&path).unwrap(), original);
    assert_eq!(fs::read(&backup).unwrap(), original);
    assert_eq!(todo_store::read(&temporary(&path)).unwrap(), replacement);

    drop(handle);
    todo_store::write(&path, &replacement).unwrap();
    assert_eq!(todo_store::read(&path).unwrap(), replacement);
    assert_eq!(fs::read(&backup).unwrap(), original);
    assert!(!temporary(&path).exists());
}
