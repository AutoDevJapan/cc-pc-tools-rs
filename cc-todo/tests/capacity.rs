use cc_todo::{STORE_MAX_BYTES, load, save};
use serde_json::json;
use std::fs;

#[test]
fn save_load_large_complete_store_and_reject_without_overwriting() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("todo.json");
    let mut db = json!({"projects":{"demo":"demo"},"items":[{"id":1,"spec":"spec.md",
        "needs":["cargo"],"subwork":{"parent_todo_id":2},"history":["x".repeat(16*1024*1024)]}],
        "next_id":2,"stalls":{"demo":{"canonical":"manual:queued"}}});
    save(&path, &db).unwrap();
    assert!(fs::metadata(&path).unwrap().len() > 16 * 1024 * 1024);
    assert_eq!(load(&path).unwrap(), db);
    let before = fs::read(&path).unwrap();
    db["items"][0]["history"] = json!(["x".repeat(STORE_MAX_BYTES)]);
    assert!(
        save(&path, &db)
            .unwrap_err()
            .starts_with("todo_store_too_large:")
    );
    assert_eq!(fs::read(&path).unwrap(), before);
    assert_eq!(fs::read_dir(temp.path()).unwrap().count(), 1);
    fs::write(&path, "{broken").unwrap();
    assert!(
        load(&path)
            .unwrap_err()
            .starts_with("todo_store_invalid_json:")
    );
    fs::remove_file(&path).unwrap();
    fs::create_dir(&path).unwrap();
    assert!(load(&path).unwrap_err().starts_with("todo_store_io:"));
}
