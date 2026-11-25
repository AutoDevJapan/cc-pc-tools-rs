use serde_json::Value;
use std::{
    fs,
    os::windows::process::CommandExt,
    path::Path,
    process::{Command, Output},
};

fn status(home: &Path) -> Output {
    Command::new(env!("CARGO_BIN_EXE_todo"))
        .creation_flags(0x08000000)
        .env("CC_TODO_HOME", home)
        .env("CC_TODO_JOBS", home)
        .arg("capacity-status")
        .output()
        .unwrap()
}

#[test]
fn capacity_status_preserves_local_bom_and_legacy_fields() {
    let home = tempfile::tempdir().unwrap();
    let lock = home.path().join("todo.lock");
    let path = home.path().join("todo.json");
    let mut before = b"\xef\xbb\xbf".to_vec();
    before.extend_from_slice(br#"{"items":[{"id":7,"status":"todo","capacity_decision":{"key":"capacity-decision-v1","futureField":1}}]}"#);
    fs::write(&lock, b"lock\n").unwrap();
    let original_permissions = fs::metadata(&lock).unwrap().permissions();
    let mut permissions = original_permissions.clone();
    permissions.set_readonly(true);
    fs::set_permissions(&lock, permissions).unwrap();
    fs::write(&path, &before).unwrap();

    let out = status(home.path());
    fs::set_permissions(&lock, original_permissions).unwrap();
    assert!(out.status.success());
    assert!(out.stderr.is_empty());
    let view: Value = serde_json::from_slice(&out.stdout).unwrap();
    assert_eq!(view["registered"], true);
    assert_eq!(view["todo_id"], 7);
    assert_eq!(view["todo_status"], "todo");
    assert_eq!(view["launch_policy"], "ordinary_scheduler");
    assert!(view["result"].is_null());
    assert!(view["active_generation"].is_null());
    assert!(view.get("futureField").is_none());
    assert_eq!(fs::read(&path).unwrap(), before);
    assert_eq!(fs::read(&lock).unwrap(), b"lock\n");
    assert_eq!(fs::read_dir(home.path()).unwrap().count(), 2);
}

#[test]
fn capacity_status_does_not_create_a_lock_or_treat_missing_state_as_empty() {
    let home = tempfile::tempdir().unwrap();
    let lock = home.path().join("todo.lock");
    let path = home.path().join("todo.json");
    let empty = br#"{"items":[]}"#;
    for (store, locked, ok) in [
        (Some(empty.as_slice()), false, false),
        (None, true, false),
        (Some(b"{".as_slice()), true, false),
        (Some(br#"{"items":{}}"#.as_slice()), true, false),
        (Some(br#"{"items":[{"capacity_decision":{"key":"capacity-decision-v1"}},{"capacity_decision":{"key":"capacity-decision-v1"}}]}"#.as_slice()), true, false),
        (Some(empty.as_slice()), true, true),
    ] {
        if locked {
            fs::write(&lock, b"").unwrap();
        } else if lock.exists() {
            fs::remove_file(&lock).unwrap();
        }
        if let Some(bytes) = store {
            fs::write(&path, bytes).unwrap();
        } else if path.exists() {
            fs::remove_file(&path).unwrap();
        }
        let out = status(home.path());
        assert_eq!(out.status.success(), ok, "{:?}", out.stderr);
        if ok {
            assert!(out.stderr.is_empty());
            let view: Value = serde_json::from_slice(&out.stdout).unwrap();
            assert_eq!(
                view,
                serde_json::json!({"schema":1,"registered":false,"launch_policy":"ordinary_scheduler"})
            );
        } else {
            assert!(out.stdout.is_empty());
            assert!(!out.stderr.is_empty());
        }
        assert_eq!(lock.exists(), locked);
        assert_eq!(fs::read(&path).ok().as_deref(), store);
        assert_eq!(
            fs::read_dir(home.path()).unwrap().count(),
            locked as usize + store.is_some() as usize
        );
    }
}

