use cc_job_policy::{todo_history as history, todo_store};
use serde_json::{Value, json};
use std::fs;

fn fixture() -> Value {
    json!({"items":[{"id":2626,"note":"current unchanged","spec":"request.md","status":"wait",
        "wait_ready":{"condition":"retry-time","not_before":9000},"wait_on":[7],
        "scheduler":{"job":"original","key":"same"},"unknown":{"Key":1,"key":2},
        "note_history":[{"note":"原文\n日本語 token=fixture-only","updated":"2026-10-17T08:00:00.000+09:00","extra":[1,null]},"old note"]},
        {"id":7,"status":"doing","note_history":["other item remains"]}],"unknownRoot":42})
}
#[test]
fn preview_is_read_only_and_apply_preserves_every_original_value() {
    let temp = tempfile::tempdir().unwrap();
    let store = temp.path().join("todo.json");
    let original = fixture();
    let mut db = original.clone();
    todo_store::write(&store, &db).unwrap();
    let bytes = fs::read(&store).unwrap();
    let preview = history::archive(&store, &mut db, 2626, false).unwrap();
    assert_eq!(preview["inline_entries"], 2);
    assert_eq!(preview["saved"], false);
    assert_eq!(db, original);
    assert_eq!(fs::read(&store).unwrap(), bytes);
    assert!(!temp.path().join("note-history-v1").exists());
    let receipt = history::archive(&store, &mut db, 2626, true).unwrap();
    assert_eq!(receipt["saved"], true);
    assert_eq!(db["items"][0]["note_history"], json!([]));
    assert_eq!(
        history::materialized(&store, &db["items"][0]).unwrap(),
        original["items"][0]
    );
    assert_eq!(db["items"][1], original["items"][1]);
    assert_eq!(db["unknownRoot"], original["unknownRoot"]);
    todo_store::write(&store, &db).unwrap();
    let reread = todo_store::read(&store).unwrap();
    assert_eq!(
        history::materialized(&store, &reread["items"][0]).unwrap(),
        original["items"][0]
    );
}
#[test]
fn interrupted_publication_is_reused_and_new_segments_keep_the_order() {
    let temp = tempfile::tempdir().unwrap();
    let store = temp.path().join("todo.json");
    let original = fixture();
    todo_store::write(&store, &original).unwrap();
    let mut abandoned = original.clone();
    history::archive(&store, &mut abandoned, 2626, true).unwrap();
    // Simulate a crash before saving the changed snapshot.
    let mut db = todo_store::read(&store).unwrap();
    history::archive(&store, &mut db, 2626, true).unwrap();
    assert_eq!(
        fs::read_dir(temp.path().join("note-history-v1"))
            .unwrap()
            .count(),
        1
    );
    assert_eq!(
        history::archive(&store, &mut db, 2626, true).unwrap()["saved"],
        false
    );
    db["items"][0]["note_history"] = json!(["new entry"]);
    history::archive(&store, &mut db, 2626, true).unwrap();
    db["items"][0]["note_history"] = json!(["latest inline"]);
    let page = history::page(&store, &db["items"][0], 1, 2).unwrap();
    assert_eq!(page["entries"], json!(["old note", "new entry"]));
    assert_eq!(page["total"], 4);
    assert_eq!(page["next_offset"], 3);
    assert_eq!(
        history::page(&store, &db["items"][0], 3, 20).unwrap()["entries"],
        json!(["latest inline"])
    );
    assert_eq!(
        history::page(&store, &db["items"][0], 99, 20).unwrap()["next_offset"],
        Value::Null
    );
}
#[test]
fn corruption_is_an_error_and_pages_skip_unneeded_archives() {
    let temp = tempfile::tempdir().unwrap();
    let store = temp.path().join("todo.json");
    let mut db = fixture();
    let r = history::archive(&store, &mut db, 2626, true).unwrap();
    let path = temp
        .path()
        .join("note-history-v1")
        .join(r["archive"]["file"].as_str().unwrap());
    db["items"][0]["note_history"] = json!(["new inline"]);
    fs::write(path, b"{}").unwrap();
    assert!(history::page(&store, &db["items"][0], 0, 1).is_err());
    assert_eq!(
        history::page(&store, &db["items"][0], 2, 1).unwrap()["entries"],
        json!(["new inline"])
    );
    assert_eq!(history::archived_count(&db["items"][0]).unwrap(), 2);
    let before = db.clone();
    assert!(history::archive(&store, &mut db, 2626, true).is_err());
    assert_eq!(db, before);
}
#[test]
fn malformed_identity_and_failed_archive_directory_never_mutate_the_snapshot() {
    let temp = tempfile::tempdir().unwrap();
    let store = temp.path().join("todo.json");
    let mut db = fixture();
    fs::write(temp.path().join("note-history-v1"), b"existing file").unwrap();
    let before = db.clone();
    assert!(history::archive(&store, &mut db, 2626, true).is_err());
    assert_eq!(db, before);
    db["items"][0]["note_history_archives"] = json!([{"domain":"cc-todo-note-history-v1","todo_id":7,
        "file":"../secret","sha256":"0".repeat(64),"entries":1,"bytes":2}]);
    assert!(history::page(&store, &db["items"][0], 0, 1).is_err());
}
#[test]
fn archive_frees_capacity_while_preserving_history_above_the_console_old_limit() {
    let temp = tempfile::tempdir().unwrap();
    let store = temp.path().join("todo.json");
    let mut db = fixture();
    let note = "x".repeat(17 * 1024 * 1024);
    db["items"][0]["note_history"] = json!([{"note":note}]);
    history::archive(&store, &mut db, 2626, true).unwrap();
    todo_store::write(&store, &db).unwrap();
    assert!(fs::metadata(&store).unwrap().len() < 4096);
    assert_eq!(
        history::page(&store, &db["items"][0], 0, 1).unwrap()["entries"][0]["note"]
            .as_str()
            .unwrap()
            .len(),
        17 * 1024 * 1024
    );
}
