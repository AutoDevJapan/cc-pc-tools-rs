use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{fs, process::Command};

fn run(home: &std::path::Path, args: &[&str]) -> std::process::Output {
    let mut command = Command::new(env!("CARGO_BIN_EXE_todo"));
    command
        .env("CC_TODO_HOME", home)
        .env("CC_TODO_JOBS", home)
        .args(args);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    command.output().unwrap()
}

fn sha(note: &str) -> String {
    format!("{:x}", Sha256::digest(note.as_bytes()))
}

#[test]
fn current_note_projects_only_requested_note_and_preserves_store() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    let note = format!("日本語😀\r\n{}", "fixture".repeat(3000));
    let db = json!({"items":[
        {"id":1,"note":note,"title":"fixture-private-field"},
        {"id":2,"note":"fixture-private-other"}
    ],"next_id":3});
    let bytes = serde_json::to_vec(&db).unwrap();
    fs::write(&path, &bytes).unwrap();
    let output = run(dir.path(), &["current-note", "1"]);
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    let actual: Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(actual, json!({"id":1,"note":note,"noteSha256":sha(&note)}));
    assert_eq!(fs::read(&path).unwrap(), bytes);
}

#[test]
fn current_note_rejects_corrupt_or_missing_local_store_without_success_output() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    for bytes in [b"{".as_slice(), b"{\"items\":[\xff]}".as_slice()] {
        fs::write(&path, bytes).unwrap();
        let result = run(dir.path(), &["current-note", "1"]);
        assert!(!result.status.success());
        assert!(result.stdout.is_empty());
        assert_eq!(fs::read(&path).unwrap(), bytes);
    }
    fs::remove_file(&path).unwrap();
    let result = run(dir.path(), &["current-note", "1"]);
    assert!(!result.status.success());
    assert!(result.stdout.is_empty());
    assert!(!path.exists());
}

#[test]
fn current_note_matches_edit_cas_for_missing_null_invalid_and_changed_note() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("todo.json");
    for note in [
        None,
        Some(Value::Null),
        Some(json!(42)),
        Some(json!({"bad":true})),
    ] {
        let mut item = json!({"id":1,"status":"todo"});
        if let Some(ref value) = note {
            item["note"] = value.clone();
        }
        let before = serde_json::to_vec(&json!({"items":[item],"next_id":2})).unwrap();
        fs::write(&path, &before).unwrap();
        let output = run(dir.path(), &["current-note", "1"]);
        if note.is_none() || note == Some(Value::Null) {
            assert!(output.status.success());
            let actual: Value = serde_json::from_slice(&output.stdout).unwrap();
            assert_eq!(actual["note"], "");
            assert_eq!(actual["noteSha256"], sha(""));
        } else {
            assert!(!output.status.success());
            assert!(output.stdout.is_empty());
        }
        assert_eq!(fs::read(&path).unwrap(), before);
    }
    fs::write(
        &path,
        serde_json::to_vec(&json!({"items":[{"id":1,"note":"old","status":"todo"}],"next_id":2}))
            .unwrap(),
    )
    .unwrap();
    let read = run(dir.path(), &["current-note", "1"]);
    let read: Value = serde_json::from_slice(&read.stdout).unwrap();
    let expected = read["noteSha256"].as_str().unwrap();
    let changed = json!({"items":[{"id":1,"note":"new","status":"todo"}],"next_id":2});
    let before = serde_json::to_vec(&changed).unwrap();
    fs::write(&path, &before).unwrap();
    let replacement = dir.path().join("replacement.txt");
    fs::write(&replacement, "replacement").unwrap();
    let result = run(
        dir.path(),
        &[
            "edit",
            "1",
            "--note-file",
            replacement.to_str().unwrap(),
            "--expected-note-sha256",
            expected,
        ],
    );
    assert!(!result.status.success());
    assert!(String::from_utf8_lossy(&result.stderr).contains("conversation_note_changed"));
    assert_eq!(fs::read(&path).unwrap(), before);
    let new_sha = sha("new");
    let result = run(
        dir.path(),
        &[
            "edit",
            "1",
            "--note-file",
            replacement.to_str().unwrap(),
            "--expected-note-sha256",
            &new_sha,
        ],
    );
    assert!(
        result.status.success(),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
    let saved: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
    assert_eq!(saved["items"][0]["note"], "replacement");
    assert_eq!(saved["items"][0]["status"], "todo");
    let missing = run(dir.path(), &["current-note", "999"]);
    assert!(!missing.status.success());
    assert!(missing.stdout.is_empty());
}
