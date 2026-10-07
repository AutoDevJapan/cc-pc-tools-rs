use cc_todo::recovery::reconcile_pending_intake;
use serde_json::{Value, json};
fn db() -> Value {
    json!({"projects":{"tools":"D:/CC/Tools"},"items":[{"id":1,"project":"tools","status":"todo","jobs":[],"title":"作業を続ける","note":"指示を保持","progress":30,"created_s":1.,"stall_recovery":{"history":[50.],"next_at":30000.,"pending":{"request":"stall-1-1","key":"old-key","reserved_at":100.,"prompt":"元の受入条件"}}}]})
}
fn queue() -> Value {
    json!({"queue":[{"todo_id":1,"key":"manual:req-todo-1-triage-20261007","cwd":"D:/CC/Tools"}],"launches":{}})
}
#[test]
fn accepted_receipt_supersedes_only_the_same_stale_unlaunched_pending() {
    let mut d = db();
    let before = d["items"][0].clone();
    let state = queue();
    let result = reconcile_pending_intake(&mut d, &state, &[], 10000., "").unwrap();
    assert_eq!(result.migrated, vec![1]);
    assert!(result.accepted.contains(&1));
    assert!(d["items"][0]["stall_recovery"]["pending"].is_null());
    for field in ["status", "jobs", "note", "title", "progress", "created_s"] {
        assert_eq!(d["items"][0][field], before[field]);
    }
    assert_eq!(
        d["items"][0]["stall_recovery"]["intake_reconciliation"]["pending"],
        before["stall_recovery"]["pending"]
    );
    assert_eq!(d["items"][0]["stall_recovery"]["next_at"], 30000.);
    assert_eq!(
        state,
        queue(),
        "the accepted queue and its prompt remain unchanged"
    );
    assert!(
        reconcile_pending_intake(&mut d, &state, &[], 10100., "")
            .unwrap()
            .migrated
            .is_empty()
    );
}
#[test]
fn pending_without_a_durable_matching_receipt_is_not_modified() {
    for state in [
        json!({"queue":[],"launches":{}}),
        json!({"queue":[{"todo_id":2,"key":"manual:req-todo-2-triage-20261007"}],"launches":{}}),
    ] {
        let mut d = db();
        let before = d.clone();
        assert!(
            reconcile_pending_intake(&mut d, &state, &[], 10000., "")
                .unwrap()
                .migrated
                .is_empty()
        );
        assert_eq!(d, before);
    }
}
#[test]
fn old_queue_or_started_receipt_and_jobs_protect_the_original_reservation() {
    for proof in ["queue", "receipt", "job", "linked"] {
        let mut d = db();
        let before = d.clone();
        let mut state = queue();
        let mut jobs = vec![];
        match proof {
            "queue" => state["queue"]
                .as_array_mut()
                .unwrap()
                .push(json!({"todo_id":1,"key":"manual:stall-1-1"})),
            "receipt" => {
                state["launches"]["manual:stall-1-1"] = json!("started-but-record-missing")
            }
            "job" => jobs.push(json!({"id":"old","launch_key":"old-key","status":"finished"})),
            _ => jobs.push(json!({"id":"live","todo_id":1,"status":"queued"})),
        }
        assert!(
            reconcile_pending_intake(&mut d, &state, &jobs, 10000., "")
                .unwrap()
                .migrated
                .is_empty(),
            "{proof}"
        );
        assert_eq!(d, before);
    }
}
#[test]
fn fresh_or_inflight_claim_is_protected_until_expiry() {
    let mut d = db();
    d["items"][0]["stall_recovery"]["pending"]["reserved_at"] = json!(9800.);
    assert!(
        reconcile_pending_intake(&mut d, &queue(), &[], 10000., "")
            .unwrap()
            .migrated
            .is_empty()
    );
    assert_eq!(
        reconcile_pending_intake(&mut d, &queue(), &[], 10100., "")
            .unwrap()
            .migrated,
        vec![1]
    );
    let mut d = db();
    d["items"][0]["stall_recovery"]["pending"]["claim"] = json!("in-flight");
    d["items"][0]["stall_recovery"]["pending"]["claim_until"] = json!(10001.);
    assert!(
        reconcile_pending_intake(&mut d, &queue(), &[], 10000., "")
            .unwrap()
            .migrated
            .is_empty()
    );
    assert_eq!(
        reconcile_pending_intake(&mut d, &queue(), &[], 10001., "")
            .unwrap()
            .migrated,
        vec![1]
    );
}
#[test]
fn stops_owner_and_client_exclusions_and_unknown_receipts_are_preserved() {
    for patch in [
        json!({"status":"done"}),
        json!({"status":"block"}),
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
        json!({"project":"owner"}),
    ] {
        let mut d = db();
        d["items"][0]
            .as_object_mut()
            .unwrap()
            .extend(patch.as_object().unwrap().clone());
        let before = d.clone();
        assert!(
            reconcile_pending_intake(&mut d, &queue(), &[], 10000., "")
                .unwrap()
                .migrated
                .is_empty()
        );
        assert_eq!(d, before);
    }
    let mut d = db();
    let before = d.clone();
    assert!(
        reconcile_pending_intake(&mut d, &queue(), &[], 10000., "tools")
            .unwrap()
            .migrated
            .is_empty()
    );
    assert_eq!(d, before);
    assert!(reconcile_pending_intake(&mut d, &json!({"queue":[]}), &[], 10000., "").is_err());
    assert_eq!(d, before);
}
#[test]
fn file_reconciliation_defers_for_console_lock_and_never_edits_the_queue() {
    use cc_todo::recovery::reconcile_intake_files;
    use cc_todo::{Paths, load, save};
    use std::{fs, time::Duration};
    let root = std::env::temp_dir().join(format!("cc-intake-{}", uuid::Uuid::new_v4()));
    fs::create_dir(&root).unwrap();
    let jobs = root.join("jobs");
    fs::create_dir(&jobs).unwrap();
    let paths = Paths::at(&root, &jobs);
    save(&paths.store, &db()).unwrap();
    let state = root.join("console");
    fs::create_dir(&state).unwrap();
    save(&state.join("console-state.json"), &queue()).unwrap();
    let queue_bytes = fs::read(state.join("console-state.json")).unwrap();
    let guard =
        cc_scheduler::common::ByteLock::acquire(&state.join("console-state.lock"), Duration::ZERO)
            .unwrap();
    assert!(
        reconcile_intake_files(&paths, &state, 10000., "")
            .unwrap()
            .deferred
    );
    assert_eq!(load(&paths.store).unwrap(), db());
    drop(guard);
    assert_eq!(
        reconcile_intake_files(&paths, &state, 10000., "")
            .unwrap()
            .migrated,
        vec![1]
    );
    assert_eq!(
        fs::read(state.join("console-state.json")).unwrap(),
        queue_bytes
    );
    assert!(
        fs::read_dir(&jobs).unwrap().next().is_none(),
        "migration must not create a job"
    );
    save(&paths.store, &db()).unwrap();
    let broken = jobs.join("broken");
    fs::create_dir(&broken).unwrap();
    fs::write(broken.join("job.json"), "corrupt").unwrap();
    let unavailable = reconcile_intake_files(&paths, &state, 10000., "").unwrap();
    assert!(unavailable.migrated.is_empty());
    assert!(unavailable.accepted.contains(&1));
    assert!(
        !unavailable.deferred,
        "broken job info must not stop the whole watcher"
    );
    assert_eq!(load(&paths.store).unwrap(), db());
    fs::write(state.join("console-state.json"), "corrupt").unwrap();
    assert!(reconcile_intake_files(&paths, &state, 10000., "").is_err());
    assert_eq!(load(&paths.store).unwrap(), db());
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn console_lock_child_fixture() {
    use std::{io::Read, time::Duration};
    let Some(root) = std::env::var_os("CC_RECONCILE_LOCK_ROOT") else {
        return;
    };
    let root = std::path::PathBuf::from(root);
    let _guard =
        cc_scheduler::common::ByteLock::acquire(&root.join("console-state.lock"), Duration::ZERO)
            .unwrap()
            .unwrap();
    std::fs::write(root.join("ready"), b"locked").unwrap();
    let _ = std::io::stdin().read(&mut [0u8; 1]);
}
#[test]
fn another_process_owns_the_console_lock_until_explicit_release() {
    use cc_todo::recovery::reconcile_intake_files;
    use cc_todo::{Paths, load, save};
    use std::os::windows::process::CommandExt;
    use std::{
        fs,
        io::Write,
        process::{Command, Stdio},
        time::{Duration, Instant},
    };
    let root = std::env::temp_dir().join(format!("cc-intake-lock-{}", uuid::Uuid::new_v4()));
    fs::create_dir(&root).unwrap();
    let jobs = root.join("jobs");
    fs::create_dir(&jobs).unwrap();
    let paths = Paths::at(&root, &jobs);
    save(&paths.store, &db()).unwrap();
    let state = root.join("console");
    fs::create_dir(&state).unwrap();
    save(&state.join("console-state.json"), &queue()).unwrap();
    let mut child = Command::new(std::env::current_exe().unwrap())
        .args(["--exact", "console_lock_child_fixture", "--nocapture"])
        .env("CC_RECONCILE_LOCK_ROOT", &state)
        .creation_flags(0x08000000)
        .stdin(Stdio::piped())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let until = Instant::now() + Duration::from_secs(5);
    while !state.join("ready").exists() && Instant::now() < until {
        std::thread::sleep(Duration::from_millis(10));
    }
    if !state.join("ready").exists() {
        let _ = child.kill();
        let _ = child.wait();
    }
    assert!(
        state.join("ready").exists(),
        "child never acquired console lock"
    );
    assert!(
        reconcile_intake_files(&paths, &state, 10000., "")
            .unwrap()
            .deferred
    );
    assert_eq!(load(&paths.store).unwrap(), db());
    child.stdin.as_mut().unwrap().write_all(b"release").unwrap();
    assert!(child.wait().unwrap().success());
    assert_eq!(
        reconcile_intake_files(&paths, &state, 10000., "")
            .unwrap()
            .migrated,
        vec![1]
    );
    fs::remove_dir_all(root).unwrap();
}
