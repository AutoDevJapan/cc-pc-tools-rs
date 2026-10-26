use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{fs, process::Command};
fn cli(home: &std::path::Path, args: &[&str]) -> std::process::Output {
    let mut command = Command::new(env!("CARGO_BIN_EXE_todo"));
    command
        .args(args)
        .env("CC_TODO_HOME", home)
        .env("CC_TODO_JOBS", home.join("jobs"));
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    command.output().unwrap()
}
fn success(home: &std::path::Path, args: &[&str]) -> Value {
    let out = cli(home, args);
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    serde_json::from_slice(&out.stdout).unwrap()
}
#[test]
fn real_cli_previews_archives_reads_edits_and_restores_without_changing_ownership() {
    let temp = tempfile::tempdir().unwrap();
    let home = temp.path();
    let store = home.join("todo.json");
    let before = json!({"projects":{"fixture":"D:/fixture"},"items":[
        {"id":2626,"project":"fixture","status":"wait","updated":"old","note":"現在のメモ",
         "wait_on":[7],"wait_ready":{"condition":"retry-time","not_before":99999},
         "scheduler":{"job":"unchanged","key":"same"},"unknown":{"Key":1,"key":2},
         "note_history":[{"note":"旧文\n🦀","updated":"2026-10-17T01:02:03+09:00"},"second"]},
        {"id":7,"status":"todo","note_history":["untouched"]}],"next_id":8});
    fs::write(&store, serde_json::to_vec(&before).unwrap()).unwrap();
    let original = fs::read(&store).unwrap();
    assert_eq!(success(home, &["archive-history", "2626"])["saved"], false);
    assert_eq!(fs::read(&store).unwrap(), original);
    assert!(!home.join("note-history-v1").exists());
    assert_eq!(
        success(home, &["archive-history", "2626", "--apply"])["saved"],
        true
    );
    let archived = cc_todo::load(&store).unwrap();
    // A valid compact snapshot exposes an unnecessary canonical rewrite.
    fs::write(&store, serde_json::to_vec(&archived).unwrap()).unwrap();
    let unchanged = fs::read(&store).unwrap();
    assert_eq!(
        success(home, &["archive-history", "2626", "--apply"])["saved"],
        false
    );
    assert_eq!(fs::read(&store).unwrap(), unchanged);
    assert_eq!(
        cc_todo::todo_history::materialized(&store, &archived["items"][0]).unwrap(),
        before["items"][0]
    );
    assert_eq!(archived["items"][1], before["items"][1]);
    let archived_bytes = fs::read(&store).unwrap();
    assert_eq!(
        success(home, &["history", "2626", "--offset", "1", "--limit", "1"])["entries"],
        json!(["second"])
    );
    assert_eq!(fs::read(&store).unwrap(), archived_bytes);
    let note = home.join("new.txt");
    fs::write(&note, "新しい現在のメモ").unwrap();
    assert!(
        cli(
            home,
            &["edit", "2626", "--note-file", note.to_str().unwrap()]
        )
        .status
        .success()
    );
    let all = success(home, &["history", "2626"]);
    assert_eq!(all["total"], 3);
    assert_eq!(all["entries"][2]["note"], "現在のメモ");
    assert_eq!(success(home, &["restore-history", "2626"])["saved"], false);
    assert_eq!(
        success(home, &["restore-history", "2626", "--apply"])["saved"],
        true
    );
    let restored = cc_todo::load(&store).unwrap();
    assert!(restored["items"][0].get("note_history_archives").is_none());
    assert_eq!(restored["items"][0]["note_history"], all["entries"]);
    assert_eq!(
        restored["items"][0]["scheduler"],
        before["items"][0]["scheduler"]
    );
    assert_eq!(
        restored["items"][0]["wait_ready"],
        before["items"][0]["wait_ready"]
    );
}
#[test]
fn real_cli_corruption_is_not_an_empty_success_or_a_store_rewrite() {
    let temp = tempfile::tempdir().unwrap();
    let home = temp.path();
    let store = home.join("todo.json");
    fs::write(&store, br#"{"items":[{"id":1,"note_history":["old"]}]}"#).unwrap();
    let r = success(home, &["archive-history", "1", "--apply"]);
    let before = fs::read(&store).unwrap();
    let path = home
        .join("note-history-v1")
        .join(r["archive"]["file"].as_str().unwrap());
    fs::write(path, b"{}").unwrap();
    assert!(!cli(home, &["history", "1"]).status.success());
    assert!(
        !cli(home, &["restore-history", "1", "--apply"])
            .status
            .success()
    );
    assert_eq!(fs::read(&store).unwrap(), before);
}

#[test]
fn concurrent_cas_edits_at_the_real_ceiling_archive_once_and_reject_oversized_restore() {
    // Entirely synthetic bytes. No production snapshot is copied into this fixture.
    let temp = tempfile::tempdir().unwrap();
    let home = temp.path();
    let store = home.join("todo.json");
    let initial_note = "before";
    let old_text = "x".repeat(cc_todo::STORE_MAX_BYTES - 4096);
    let old_hash = Sha256::digest(old_text.as_bytes());
    let before = json!({"items":[{"id":1,"project":"fixture","status":"wait","note":initial_note,
        "spec":"original.md","jobs":["original-job"],"wait_on":[2],"owner_reply_id":"original-answer",
        "scheduler":{"job":"original-job","phase":"waiting"},"unknown":{"Key":1,"key":2},
        "note_history":[{"note":old_text,"unknown":[1,null]},"duplicate","duplicate"]},
        {"id":2,"status":"doing"}],"projects":{"fixture":"D:/fixture"},"next_id":3});
    cc_todo::save(&store, &before).unwrap();
    assert!(fs::metadata(&store).unwrap().len() as usize <= cc_todo::STORE_MAX_BYTES);
    let expected = format!("{:x}", Sha256::digest(initial_note.as_bytes()));
    let note_a = home.join("a.txt");
    let note_b = home.join("b.txt");
    fs::write(&note_a, "a".repeat(8192)).unwrap();
    fs::write(&note_b, "b".repeat(8192)).unwrap();
    let run = |note: &std::path::Path| {
        cli(
            home,
            &[
                "edit",
                "1",
                "--note-file",
                note.to_str().unwrap(),
                "--expected-note-sha256",
                &expected,
            ],
        )
    };
    let (a, b) = std::thread::scope(|scope| {
        let a = scope.spawn(|| run(&note_a));
        let b = scope.spawn(|| run(&note_b));
        (a.join().unwrap(), b.join().unwrap())
    });
    assert_eq!(
        usize::from(a.status.success()) + usize::from(b.status.success()),
        1,
        "a: {} b: {}",
        String::from_utf8_lossy(&a.stderr),
        String::from_utf8_lossy(&b.stderr)
    );
    let loser = if a.status.success() { b } else { a };
    assert!(String::from_utf8_lossy(&loser.stderr).contains("note_changed"));
    let saved = cc_todo::load(&store).unwrap();
    assert!(fs::metadata(&store).unwrap().len() < 16384);
    assert_eq!(
        saved["items"][0]["note_history_archives"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    for key in [
        "spec",
        "status",
        "jobs",
        "wait_on",
        "owner_reply_id",
        "scheduler",
        "unknown",
    ] {
        assert_eq!(saved["items"][0][key], before["items"][0][key], "{key}");
    }
    assert_eq!(saved["items"][1], before["items"][1]);
    let first = cc_todo::todo_history::page(&store, &saved["items"][0], 0, 1).unwrap();
    assert_eq!(
        Sha256::digest(first["entries"][0]["note"].as_str().unwrap().as_bytes()),
        old_hash
    );
    assert_eq!(first["entries"][0]["unknown"], json!([1, null]));
    let tail = success(home, &["history", "1", "--offset", "1", "--limit", "10"]);
    assert_eq!(tail["total"], 4);
    assert_eq!(tail["entries"][0], "duplicate");
    assert_eq!(tail["entries"][1], "duplicate");
    assert_eq!(tail["entries"][2]["note"], initial_note);
    let stable = fs::read(&store).unwrap();
    let restore = cli(home, &["restore-history", "1", "--apply"]);
    assert!(!restore.status.success());
    assert!(String::from_utf8_lossy(&restore.stderr).contains("todo_store_too_large"));
    assert_eq!(fs::read(&store).unwrap(), stable);
    assert_eq!(
        fs::read_dir(home.join("note-history-v1")).unwrap().count(),
        1
    );
}

#[test]
fn scheduler_and_answer_writers_use_the_canonical_unredacted_snapshot_contract() {
    let temp = tempfile::tempdir().unwrap();
    let db =
        json!({"items":[{"id":1,"note":"token: synthetic-literal","unknown":{"Key":1,"key":2}}]});
    let expected = temp.path().join("expected.json");
    cc_todo::save(&expected, &db).unwrap();
    let bytes = fs::read(expected).unwrap();
    for kind in ["scheduler", "answer"] {
        let home = temp.path().join(kind);
        fs::create_dir(&home).unwrap();
        let store = home.join("todo.json");
        fs::write(&store, b"{}").unwrap();
        let _lock =
            cc_todo::Lock::acquire(&home.join("todo.lock"), std::time::Duration::from_secs(1))
                .unwrap();
        if kind == "scheduler" {
            cc_scheduler::common::write_json(&store, &db, 2).unwrap();
        } else {
            cc_ask::core::atomic_json(&store, &db, 2).unwrap();
        }
        assert_eq!(fs::read(&store).unwrap(), bytes, "{kind}");
        assert_eq!(
            fs::read(home.join("todo.json.bak-20260930d")).unwrap(),
            b"{}",
            "{kind}"
        );
    }
}
