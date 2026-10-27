use super::*;
use cc_job_policy::launch_outcome::{Kind, Outcome};
use cc_scheduler::launch_receipt::{self, Binding, Receipt};

fn terminal_same_key(f: &mut Fixture) -> Value {
    f.put(item(1, "civ6", "todo", &[]));
    let prepared =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let mut old = job("old-terminal", 999, "failed");
    old["launch_key"] = prepared["scheduler"]["key"].clone();
    old["todo_id"] = json!(1);
    old["cwd"] = prepared["scheduler"]["worktree"].clone();
    old["queue_restart_pending"] = json!(false);
    old["reboot_launch_pending"] = json!(false);
    f.e.backend.records.push(old.clone());
    old
}

#[test]
fn terminal_same_key_no_action_does_not_mark_launched_or_consume_spacing() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    let before = f.e.state.clone();
    f.dispatch();
    assert!(f.e.report["launched"].is_null());
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.e.state["last_launch"], before["last_launch"]);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn terminal_same_key_no_action_allows_next_qualified_candidate() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.put(item(2, "other", "todo", &[]));
    f.dispatch();
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.get(2)["status"], "doing");
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 2);
}

fn read_receipt(f: &Fixture, id: i64) -> Value {
    launch_receipt::explain(&f.e.root, &f.e.todo_path, id, T)
}

#[test]
fn no_action_receipt_links_exact_job_key_generation_and_observation_cycle() {
    let mut f = Fixture::new();
    let old = terminal_same_key(&mut f);
    f.dispatch();
    let report = read_receipt(&f, 1);
    assert_eq!(report["validity"], "matching_task_metadata");
    let r = &report["receipt"];
    assert_eq!(r["outcome"]["job_id"], old["id"]);
    assert_eq!(r["outcome"]["launch_key"], old["launch_key"]);
    assert_eq!(r["outcome"]["kind"], "existing_job_no_action");
    assert_eq!(r["linked"], false);
    let observation = read_json(&f.e.root.join("admission.json"), Value::Null).unwrap();
    assert_eq!(observation["cycle_id"], r["cycle_id"]);
    assert_eq!(
        observation["entries"]["1"]["launch_receipt_id"],
        r["receipt_id"]
    );
    assert_eq!(observation["entries"]["1"]["returned_job_id"], old["id"]);
    assert_eq!(
        observation["entries"]["1"]["reason"],
        "existing_job_no_action"
    );
}

#[test]
fn no_action_preserves_every_later_candidates_normal_gates() {
    for gate in ["stop", "wait", "resource", "model", "input", "disabled"] {
        let mut f = Fixture::new();
        terminal_same_key(&mut f);
        let mut row = item(
            2,
            "other",
            "todo",
            if gate == "resource" { &["civ6"] } else { &[] },
        );
        if gate == "stop" {
            row["status"] = json!("block");
            row["owner_stopped"] = json!(true);
        }
        if gate == "wait" {
            row["wait_on"] = json!([999]);
        }
        if gate == "disabled" {
            row["dispatchable"] = json!(false);
        }
        if gate == "model" {
            row["model"] = json!("gpt-6.1-sol");
            f.e.backend.m["quota_block_model"] = json!("gpt-6.1-sol");
        }
        f.put(row);
        if gate == "resource" {
            f.e.backend.ps.insert(44, proc(44, 0, "CivilizationVI.exe"));
        }
        if gate == "input" {
            f.e.edit(&json!(2), |r| r["spec"] = Value::Null).unwrap();
        }
        f.dispatch();
        assert!(f.e.backend.launched.is_empty(), "{gate}");
        assert_ne!(f.get(2)["status"], "doing", "{gate}");
        assert_eq!(
            read_receipt(&f, 1)["receipt"]["outcome"]["kind"],
            "existing_job_no_action",
            "{gate}"
        );
    }
}

fn sample_receipt(f: &mut Fixture, id: i64) -> Receipt {
    f.put(item(id as u64, "other", "todo", &[]));
    let row =
        f.e.prepare(&f.get(id as u64), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let job = json!({"id":format!("job-{id}"),"launch_key":row["scheduler"]["key"],"todo_id":id,
        "cwd":row["scheduler"]["worktree"],"status":"queued","todo_resume_generation":8});
    Receipt::new(
        &row,
        &json!(f.repo),
        &"ab".repeat(16),
        true,
        T,
        Outcome::from_job(Kind::Queued, &job),
    )
}

#[test]
fn receipt_reader_is_body_free_and_detects_missing_changed_generation_input_and_expiry() {
    let mut f = Fixture::new();
    assert_eq!(read_receipt(&f, 1)["validity"], "missing");
    let receipt = sample_receipt(&mut f, 1);
    assert_eq!(receipt.binding, Binding::Matching);
    launch_receipt::publish(&f.e.root, &receipt).unwrap();
    f.e.edit(&json!(1), |r| {
        r["note"] = json!("SECRET_BODY_SENTINEL");
        r["Source"] = json!("SECRET_BODY_SENTINEL");
    })
    .unwrap();
    let paths = [
        f.e.todo_path.clone(),
        f.e.root.join("launch-outcomes.json"),
        f.e.root.join("state.json"),
    ];
    let before = paths.iter().map(|p| fs::read(p).ok()).collect::<Vec<_>>();
    let report = read_receipt(&f, 1);
    assert_eq!(report["validity"], "matching_task_metadata");
    assert!(!report.to_string().contains("SECRET_BODY_SENTINEL"));
    assert_eq!(
        paths.iter().map(|p| fs::read(p).ok()).collect::<Vec<_>>(),
        before
    );
    assert_eq!(read_receipt(&f, 2)["validity"], "not_recorded");
    assert_eq!(
        launch_receipt::explain(&f.e.root, &f.e.todo_path, 1, T + launch_receipt::TTL + 1.)["validity"],
        "expired"
    );
    f.e.edit(&json!(1), |r| r["resume_generation"] = json!(9))
        .unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "generation_changed");
    f.e.edit(&json!(1), |r| {
        r.as_object_mut().unwrap().remove("resume_generation");
        r["needs"] = json!(["cargo"]);
    })
    .unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "metadata_changed");
    f.e.edit(&json!(1), |r| {
        r["scheduler"]["key"] = json!("cd".repeat(16))
    })
    .unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "launch_key_changed");
    assert!(f.e.backend.launched.is_empty());
    assert!(f.e.backend.cancelled.is_empty());
}

#[test]
fn receipt_snapshot_is_bounded_atomic_and_rejects_corruption_or_failed_replacement() {
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    for id in 1..=launch_receipt::MAX_ENTRIES as i64 + 2 {
        let mut next = receipt.clone();
        next.target_todo_id = id;
        next.receipt_id = uuid::Uuid::new_v4().simple().to_string();
        next.outcome.todo_id = Some(id as u64);
        launch_receipt::publish(&f.e.root, &next).unwrap();
    }
    let path = f.e.root.join("launch-outcomes.json");
    let store = read_json(&path, Value::Null).unwrap();
    assert_eq!(
        store["receipts"].as_array().unwrap().len(),
        launch_receipt::MAX_ENTRIES
    );
    assert!(fs::metadata(&path).unwrap().len() <= launch_receipt::MAX_BYTES);
    assert_eq!(read_receipt(&f, 1)["validity"], "not_recorded");
    let before = fs::read(&path).unwrap();
    let _lock = ByteLock::acquire(&f.e.root.join("launch-outcomes.lock"), Duration::ZERO)
        .unwrap()
        .unwrap();
    assert!(launch_receipt::publish(&f.e.root, &receipt).is_err());
    assert_eq!(fs::read(&path).unwrap(), before);
    drop(_lock);
    fs::create_dir(f.e.root.join("launch-outcomes.json.tmp")).unwrap();
    assert!(launch_receipt::publish(&f.e.root, &receipt).is_err());
    assert_eq!(fs::read(&path).unwrap(), before);
    fs::write(
        &path,
        r#"{"schema":999,"receipts":[],"note":"SECRET_BODY_SENTINEL"}"#,
    )
    .unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "corrupt_or_unreadable");
    assert!(
        !read_receipt(&f, 1)
            .to_string()
            .contains("SECRET_BODY_SENTINEL")
    );
}

#[test]
fn receipt_write_failure_does_not_change_actual_launch_or_no_action() {
    for no_action in [false, true] {
        let mut f = Fixture::new();
        if no_action {
            terminal_same_key(&mut f);
        } else {
            f.put(item(1, "civ6", "todo", &[]));
        }
        fs::create_dir(f.e.root.join("launch-outcomes.json")).unwrap();
        f.dispatch();
        assert_eq!(f.e.report["launch_receipt_recorded"], false);
        assert_eq!(f.e.backend.launched.len(), usize::from(!no_action));
        assert_eq!(f.get(1)["status"], if no_action { "todo" } else { "doing" });
    }
}

#[test]
fn existing_pending_reservation_stops_without_second_slot_or_supervisor() {
    for field in ["queue_restart_pending", "reboot_launch_pending"] {
        let mut f = Fixture::new();
        terminal_same_key(&mut f);
        f.e.backend.records[0][field] = json!(true);
        f.put(item(2, "other", "todo", &[]));
        let before = f.e.state.clone();
        f.dispatch();
        assert!(f.e.backend.launched.is_empty());
        assert!(f.e.report["launched"].is_null());
        assert_eq!(f.e.state["last_launch"], before["last_launch"]);
        assert_eq!(f.get(2)["status"], "todo");
        let observation=read_json(&f.e.root.join("admission.json"),Value::Null).unwrap();
        assert_eq!(observation["entries"]["2"]["reason"],"earlier_candidate_reserved");
        let r = read_receipt(&f, 1);
        assert_eq!(r["receipt"]["outcome"][field], true);
        assert_eq!(r["receipt"]["outcome"]["kind"], "existing_job_no_action");
    }
}

#[test]
fn explicit_owner_or_registration_generation_mismatch_is_not_linked_or_reported_as_new_execution() {
    for change in ["owner", "generation", "attempt", "branch"] {
        let mut f = Fixture::new();
        terminal_same_key(&mut f);
        let old = &mut f.e.backend.records[0];
        match change {
            "owner" => old["todo_id"] = json!(999),
            "generation" => old["launch_request_generation"] = json!(999),
            "attempt" => old["launch_request_attempt"] = json!(999),
            _ => old["work_owner"] = json!({"branch":"another-branch"}),
        }
        f.dispatch();
        assert!(f.e.backend.launched.is_empty());
        assert_eq!(f.get(1)["status"], "todo");
        assert!(f.e.report["launched"].is_null());
        assert_eq!(
            read_receipt(&f, 1)["validity"],
            "owner_mismatch",
            "{change}"
        );
        assert_eq!(read_receipt(&f, 1)["receipt"]["linked"], false);
    }
}

struct ReturnOutput(String);
impl Runner for ReturnOutput {
    fn run(&mut self, command: &Invocation) -> Result<Output> {
        assert!(command.args.iter().any(|a| a == "--launch-outcome"));
        Ok(Output {
            code: 0,
            stdout: self.0.clone(),
            stderr: String::new(),
        })
    }
}
#[test]
fn real_backend_parses_actual_typed_result_and_keeps_legacy_id_unknown() {
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    let row = f.get(1);
    let mut backend = RealBackend::new(f.e.root.clone());
    for (text, kind) in [
        (
            serde_json::to_string(&receipt.outcome).unwrap(),
            Kind::Queued,
        ),
        ("legacy-job\n".into(), Kind::Unknown),
    ] {
        backend.runner = Box::new(ReturnOutput(text));
        let outcome = backend
            .launch(&row, Path::new("fixture.md"), &json!({}))
            .unwrap();
        assert_eq!(outcome.kind, kind);
    }
    for text in [
        "{}",
        "{\"schema\":2}",
        "accepted but no job",
        "job with spaces",
    ] {
        backend.runner = Box::new(ReturnOutput(text.into()));
        assert!(
            backend
                .launch(&row, Path::new("fixture.md"), &json!({}))
                .is_err()
        );
    }
}

#[test]
fn concurrent_receipt_writers_leave_one_complete_snapshot_and_crash_temp_is_not_read() {
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    launch_receipt::publish(&f.e.root, &receipt).unwrap();
    let barrier = std::sync::Barrier::new(4);
    let root = &f.e.root;
    std::thread::scope(|scope| {
        let mut handles = vec![];
        for id in 2..=5 {
            let mut r = receipt.clone();
            r.target_todo_id = id;
            r.receipt_id = uuid::Uuid::new_v4().simple().to_string();
            r.outcome.todo_id = Some(id as u64);
            handles.push(scope.spawn({
                let barrier = &barrier;
                move || {
                    barrier.wait();
                    launch_receipt::publish(root, &r)
                }
            }));
        }
        for handle in handles {
            let result = handle.join().unwrap();
            assert!(result.is_ok() || result == Err(Error("LaunchReceiptBusy")));
        }
    });
    assert_eq!(read_receipt(&f, 1)["validity"], "matching_task_metadata");
    let original = fs::read(f.e.root.join("launch-outcomes.json")).unwrap();
    fs::write(
        f.e.root.join("launch-outcomes.json.tmp"),
        b"incomplete crash",
    )
    .unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "matching_task_metadata");
    assert_eq!(
        fs::read(f.e.root.join("launch-outcomes.json")).unwrap(),
        original
    );
    launch_receipt::publish(&f.e.root, &receipt).unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "matching_task_metadata");
}

#[test]
fn saved_input_hash_mismatch_and_missing_historical_hash_are_distinct_without_reading_source() {
    let mut f = Fixture::new();
    let mut receipt = sample_receipt(&mut f, 1);
    assert_eq!(receipt.input_binding, Binding::Unknown);
    let row = f.get(1);
    let job = json!({"id":"old-input","todo_id":1,"cwd":row["scheduler"]["worktree"],
        "launch_key":row["scheduler"]["key"],"status":"failed","queue_restart_pending":false,
        "reboot_launch_pending":false,"launch_dispatch_sha256":"ef".repeat(32)});
    receipt = Receipt::new(
        &row,
        &json!(f.repo),
        &"ab".repeat(16),
        true,
        T,
        Outcome::from_job(Kind::ExistingJobNoAction, &job),
    );
    assert_eq!(receipt.input_binding, Binding::Mismatch);
    launch_receipt::publish(&f.e.root, &receipt).unwrap();
    let source = PathBuf::from(s(&row["spec"]));
    fs::write(&source, "SOURCE_MUST_NOT_BE_READ_TO_REPAIR_SHA").unwrap();
    let output = read_receipt(&f, 1);
    assert_eq!(output["validity"], "input_mismatch");
    assert_eq!(output["current_source_bytes_checked"], false);
    assert!(
        !output
            .to_string()
            .contains("SOURCE_MUST_NOT_BE_READ_TO_REPAIR_SHA")
    );
    assert!(f.e.backend.launched.is_empty());
}
