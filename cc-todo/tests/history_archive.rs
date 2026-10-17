use serde_json::{Value, json};
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
