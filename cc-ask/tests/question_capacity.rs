use cc_ask::{
    core::{atomic_json, read_json},
    question_store::{self, Error, MAX_BYTES},
};
use serde_json::json;
use std::fs;

#[test]
fn question_capacity_preserves_complete_snapshot_above_old_eight_mib() {
    let tmp = tempfile::tempdir().unwrap();
    let p = tmp.path().join("state.json");
    let db = json!({"questions":[{"thread_id":"123","needs":["cargo"],"unknown":{"keep":true}}],
        "requests":{"456":{"parts":[1,2]}},"answer_receipts":{"789":{"delivered":true}},
        "history":"x".repeat(8*1024*1024)});
    atomic_json(&p, &db, 2).unwrap();
    assert!(fs::metadata(&p).unwrap().len() > 8 * 1024 * 1024);
    assert_eq!(question_store::read(&p).unwrap(), db);
    assert_eq!(read_json(&p).unwrap(), db);
    let before = fs::read(&p).unwrap();
    let oversized = json!({"questions":[],"history":"x".repeat(MAX_BYTES)});
    assert!(atomic_json(&p, &oversized, 2)
        .unwrap_err()
        .starts_with("question_state_too_large:"));
    assert_eq!(fs::read(&p).unwrap(), before);
    assert_eq!(fs::read_dir(tmp.path()).unwrap().count(), 1);
}

#[test]
fn question_capacity_boundary_corruption_and_io_reject_without_partial_state() {
    let tmp = tempfile::tempdir().unwrap();
    let p = tmp.path().join("state.json");
    let mut bytes = b"{}".to_vec();
    bytes.resize(MAX_BYTES, b' ');
    fs::write(&p, &bytes).unwrap();
    assert_eq!(question_store::read(&p).unwrap(), json!({}));
    bytes.push(b' ');
    fs::write(&p, &bytes).unwrap();
    assert!(matches!(question_store::read(&p), Err(Error::TooLarge)));
    fs::write(&p, "{broken").unwrap();
    assert!(matches!(
        question_store::read(&p),
        Err(Error::InvalidJson(_))
    ));
    fs::remove_file(&p).unwrap();
    fs::create_dir(&p).unwrap();
    assert!(matches!(question_store::read(&p), Err(Error::Io(_))));
}

#[test]
fn question_capacity_exclusive_update_rejects_before_journal_or_writer() {
    let tmp = tempfile::tempdir().unwrap();
    let p = tmp.path().join("state.json");
    let original = br#"{"questions":[],"keep":true}"#;
    fs::write(&p, original).unwrap();
    let called = std::cell::Cell::new(false);
    let result = cc_ask::core::update_json_exclusive_with_writer(
        &p,
        |v| {
            v["history"] = json!("x".repeat(MAX_BYTES));
            Ok(())
        },
        |_, _| {
            called.set(true);
            Ok(())
        },
    );
    assert!(result.unwrap_err().starts_with("question_state_too_large:"));
    assert!(!called.get());
    assert_eq!(fs::read(&p).unwrap(), original);
    assert_eq!(fs::read_dir(tmp.path()).unwrap().count(), 1);
}
