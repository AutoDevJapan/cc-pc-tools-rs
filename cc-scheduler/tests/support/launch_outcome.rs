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
    f.e.state["project_last_launch"] = json!({"civ6":T-100.,"other":T-200.});
    let before = f.e.state.clone();
    f.dispatch();
    assert!(f.e.report["launched"].is_null());
    assert_eq!(f.get(1)["status"], "todo");
    assert_eq!(f.e.state["last_launch"], before["last_launch"]);
    assert_eq!(
        f.e.state["project_last_launch"],
        before["project_last_launch"]
    );
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
    assert_eq!(report["validity"], "input_not_recorded");
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
        "cwd":row["scheduler"]["worktree"],"status":"queued","todo_resume_generation":8,
        "launch_dispatch_sha256":row["scheduler"]["launch_input"]["dispatch_sha256"]});
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
        r#"{"schema":1,"receipts":[],"note":"SECRET_BODY_SENTINEL"}"#,
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
        let observation = read_json(&f.e.root.join("admission.json"), Value::Null).unwrap();
        assert_eq!(
            observation["entries"]["2"]["reason"],
            "earlier_candidate_reserved"
        );
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
        assert_eq!(
            f.e.dispatch(&f.cfg, &f.pri),
            Err(Error("LaunchOutcomeUnverified"))
        );
        assert!(f.e.backend.launched.is_empty());
        assert_eq!(f.get(1)["status"], "todo");
        assert!(f.e.report["launched"].is_null());
        assert_eq!(
            read_receipt(&f, 1)["validity"],
            match change {
                "generation" => "registration_generation_mismatch",
                "attempt" => "registration_attempt_mismatch",
                "branch" => "branch_mismatch",
                _ => "owner_mismatch",
            },
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
    let successful = std::thread::scope(|scope| {
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
                    (id, launch_receipt::publish(root, &r))
                }
            }));
        }
        let mut successful = std::collections::BTreeSet::new();
        for handle in handles {
            let (id, result) = handle.join().unwrap();
            assert!(result.is_ok() || result == Err(Error("LaunchReceiptBusy")));
            if result.is_ok() {
                successful.insert(id);
            }
        }
        successful
    });
    assert!(
        !successful.is_empty(),
        "the test must exercise successful concurrent publication"
    );
    let saved = read_json(&f.e.root.join("launch-outcomes.json"), Value::Null).unwrap();
    let mut expected = successful;
    expected.insert(1);
    assert_eq!(
        saved["receipts"]
            .as_array()
            .unwrap()
            .iter()
            .map(|r| r["target_todo_id"].as_i64().unwrap())
            .collect::<std::collections::BTreeSet<_>>(),
        expected
    );
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
    receipt.outcome.dispatch_sha256 = None;
    receipt.input_binding = Binding::Unknown;
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

#[test]
fn unverified_return_defers_same_state_and_next_pass_admits_later_candidate() {
    for scenario in ["missing_pending", "owner_mismatch", "legacy_id"] {
        let mut f = Fixture::new();
        terminal_same_key(&mut f);
        match scenario {
            "missing_pending" => {
                f.e.backend.records[0]
                    .as_object_mut()
                    .unwrap()
                    .remove("queue_restart_pending");
            }
            "owner_mismatch" => f.e.backend.records[0]["todo_id"] = json!(999),
            _ => f.e.backend.replay_kind_override = Some(Kind::Unknown),
        }
        f.put(item(2, "other", "todo", &[]));
        assert_eq!(
            f.e.dispatch(&f.cfg, &f.pri),
            Err(Error("LaunchOutcomeUnverified")),
            "{scenario}"
        );
        assert_eq!(f.get(1)["status"], "todo");
        assert_eq!(
            f.get(1)["scheduler"]["launch_outcome_deferred"]["reason"],
            "launch_outcome_unverified"
        );
        assert!(n(&f.get(1)["scheduler"]["not_before"]) > T);
        assert!(f.e.backend.launched.is_empty());
        f.e.backend.clock += 310.;
        f.e.backend.m["replayed"] = json!(false);
        f.dispatch();
        assert_eq!(
            f.e.backend.m["replayed"], false,
            "{scenario}: repeated the deferred launch"
        );
        let observation = read_json(&f.e.root.join("admission.json"), Value::Null).unwrap();
        assert_eq!(
            observation["entries"]["1"]["reason"], "launch_outcome_deferred",
            "{scenario}"
        );
        assert_eq!(f.get(2)["status"], "doing", "{scenario}");
        assert_eq!(f.e.backend.launched.len(), 1, "{scenario}");
    }
}

#[test]
fn linked_worker_generation_is_explicit_replay_evidence() {
    let mut f = Fixture::new();
    let mut old = terminal_same_key(&mut f);
    old["launch_request_generation"] = json!(2);
    old["todo_resume_generation"] = json!(3);
    f.e.edit(&json!(1), |r| {
        r["resume_generation"] = json!(3);
        r["worker_receipt"] = json!({"job":"old-terminal","generation":3});
    })
    .unwrap();
    let row = f.get(1);
    let receipt = Receipt::new(
        &row,
        &json!(f.repo),
        &"ab".repeat(16),
        true,
        T,
        Outcome::from_job(Kind::ExistingJobNoAction, &old),
    );
    assert_eq!(receipt.binding, Binding::Matching);
    assert!(receipt.valid());
    old["todo_resume_generation"] = json!(4);
    let mismatch = Receipt::new(
        &row,
        &json!(f.repo),
        &"ab".repeat(16),
        true,
        T,
        Outcome::from_job(Kind::ExistingJobNoAction, &old),
    );
    assert_eq!(mismatch.binding, Binding::Mismatch);
}

#[test]
fn fake_and_normal_registration_have_the_same_generation_attempt_and_branch_contract() {
    let mut f = Fixture::new();
    let mut row = item(1, "other", "todo", &[]);
    row["resume_generation"] = json!(6);
    f.put(row);
    let prepared =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    f.dispatch();
    let report = read_receipt(&f, 1);
    let receipt = &report["receipt"];
    assert_eq!(receipt["binding"], "matching");
    assert_eq!(receipt["linked"], true);
    assert_eq!(receipt["result_generation"], 6);
    assert_eq!(receipt["outcome"]["registration_generation"], 6);
    assert_eq!(
        receipt["outcome"]["registration_attempt"],
        prepared["scheduler"]["attempt"]
    );
    assert_eq!(receipt["outcome"]["job_resume_generation"], 7);
    assert_eq!(
        receipt["outcome"]["branch_sha256"],
        receipt["request_branch_sha256"]
    );
}

#[test]
fn accepted_and_unknown_return_reasons_and_later_candidates_remain_explicit() {
    for (kind, reason, later) in [
        (Kind::Accepted, "accepted", "earlier_candidate_accepted"),
        (
            Kind::Unknown,
            "launch_outcome_unknown",
            "earlier_outcome_unknown",
        ),
    ] {
        let mut f = Fixture::new();
        terminal_same_key(&mut f);
        f.e.backend.replay_kind_override = Some(kind);
        f.put(item(2, "other", "todo", &[]));
        assert_eq!(
            f.e.dispatch(&f.cfg, &f.pri),
            Err(Error("LaunchOutcomeUnverified"))
        );
        let observation = read_json(&f.e.root.join("admission.json"), Value::Null).unwrap();
        assert_eq!(observation["entries"]["1"]["reason"], reason);
        assert_eq!(observation["entries"]["2"]["reason"], later);
        assert_eq!(
            f.e.report["launch_return"]["kind"],
            serde_json::to_value(kind).unwrap()
        );
        assert_eq!(f.e.report["launch_return"]["linked"], false);
        assert!(f.e.report["launched"].is_null());
    }
}

#[test]
fn no_action_does_not_bypass_capacity_or_targeted_priority() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.put(item(2, "other", "todo", &[]));
    f.e.backend.pressure_after_replay = true;
    f.dispatch();
    assert_eq!(f.get(2)["status"], "todo");
    assert!(f.e.backend.launched.is_empty());
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.put(item(2, "other", "todo", &[]));
    f.e.dispatch_for(&f.cfg, &f.pri, Some(2)).unwrap();
    assert_eq!(f.e.report["priority_wait"], 2);
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(f.get(2)["status"], "todo");
}

#[test]
fn corrupt_or_oversized_auxiliary_snapshot_recovers_and_future_schema_is_preserved() {
    for oversized in [false, true] {
        let mut f = Fixture::new();
        let receipt = sample_receipt(&mut f, 1);
        let path = f.e.root.join("launch-outcomes.json");
        fs::write(
            &path,
            if oversized {
                vec![b'x'; launch_receipt::MAX_BYTES as usize + 1]
            } else {
                b"broken".to_vec()
            },
        )
        .unwrap();
        assert_eq!(read_receipt(&f, 1)["validity"], "corrupt_or_unreadable");
        launch_receipt::publish(&f.e.root, &receipt).unwrap();
        let report = read_receipt(&f, 1);
        assert_eq!(report["validity"], "matching_task_metadata");
        assert!(!report["store_recovery"].is_null());
        assert!(fs::metadata(&path).unwrap().len() <= launch_receipt::MAX_BYTES);
    }
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    let path = f.e.root.join("launch-outcomes.json");
    fs::write(&path, b"{\"schema\":2,\"receipts\":[]}").unwrap();
    let before = fs::read(&path).unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "schema_unsupported");
    assert_eq!(
        launch_receipt::publish(&f.e.root, &receipt),
        Err(Error("LaunchReceiptSchemaUnsupported"))
    );
    assert_eq!(fs::read(&path).unwrap(), before);
}

#[test]
fn expired_receipts_remain_readable_until_next_bounded_publication() {
    let mut f = Fixture::new();
    let old = sample_receipt(&mut f, 1);
    let mut next = sample_receipt(&mut f, 2);
    launch_receipt::publish(&f.e.root, &old).unwrap();
    next.observed_at = T + launch_receipt::TTL + 1.;
    next.expires_at = next.observed_at + launch_receipt::TTL;
    assert_eq!(
        launch_receipt::explain(&f.e.root, &f.e.todo_path, 1, next.observed_at)["validity"],
        "expired"
    );
    launch_receipt::publish(&f.e.root, &next).unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "not_recorded");
    assert_eq!(
        read_json(&f.e.root.join("launch-outcomes.json"), Value::Null).unwrap()["receipts"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
}

#[test]
fn compatible_extra_outcome_fields_are_ignored_but_output_stays_fixed_and_producer_id_is_not_schema()
 {
    let mut f = Fixture::new();
    let mut receipt = sample_receipt(&mut f, 1);
    receipt.implementation_todo_id = 9000;
    assert!(receipt.valid());
    let mut extra = serde_json::to_value(&receipt.outcome).unwrap();
    extra["future_field"] = json!("MUST_NOT_BE_EMITTED");
    let mut backend = RealBackend::new(f.e.root.clone());
    backend.runner = Box::new(ReturnOutput(extra.to_string()));
    let parsed = backend
        .launch(&f.get(1), Path::new("fixture.md"), &json!({}))
        .unwrap();
    assert!(
        !serde_json::to_string(&parsed)
            .unwrap()
            .contains("MUST_NOT_BE_EMITTED")
    );
}

struct UnsupportedLauncher(std::sync::Arc<std::sync::atomic::AtomicUsize>);
impl Runner for UnsupportedLauncher {
    fn run(&mut self, _: &Invocation) -> Result<Output> {
        self.0.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        Ok(Output {
            code: 2,
            stdout: String::new(),
            stderr: "unrecognized arguments: --launch-outcome".into(),
        })
    }
}
#[test]
fn an_old_launcher_is_reported_without_retrying_a_second_invocation() {
    let mut f = Fixture::new();
    sample_receipt(&mut f, 1);
    let mut backend = RealBackend::new(f.e.root.clone());
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    backend.runner = Box::new(UnsupportedLauncher(calls.clone()));
    assert_eq!(
        backend.launch(&f.get(1), Path::new("fixture.md"), &json!({})),
        Err(Error("LaunchOutcomeUnsupported"))
    );
    assert_eq!(calls.load(std::sync::atomic::Ordering::Relaxed), 1);
}

#[test]
fn saved_returns_keep_their_reason_and_unlinked_evidence_after_read_or_link_failure() {
    for failure in ["jobs", "missing", "changed", "link"] {
        let mut f = Fixture::new();
        f.put(item(1, "other", "todo", &[]));
        match failure {
            "jobs" => f.e.backend.m["jobs_error_after_launch"] = json!(true),
            "missing" => f.e.backend.m["launched_job_missing"] = json!(true),
            "changed" => f.e.backend.m["launched_job_changed"] = json!(true),
            _ => {
                f.e.backend.m["hold_todo_on_launch"] =
                    json!(f.e.todo_path.with_file_name("todo.lock"))
            }
        }
        let error = f.e.dispatch(&f.cfg, &f.pri).unwrap_err();
        if failure == "missing" {
            assert_eq!(error, Error("LaunchJobMissing"));
        }
        if failure == "changed" {
            assert_eq!(error, Error("LaunchJobChanged"));
        }
        let saved = read_json(&f.e.root.join("launch-outcomes.json"), Value::Null).unwrap();
        assert_eq!(saved["receipts"][0]["linked"], false);
        assert_eq!(saved["receipts"][0]["outcome"]["kind"], "queued");
        let observation = read_json(&f.e.root.join("admission.json"), Value::Null).unwrap();
        assert_eq!(observation["entries"]["1"]["reason"], "queued_unlinked");
    }
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.e.backend.records[0]["queue_restart_pending"] = json!(true);
    f.e.backend.m["jobs_error_after_replay"] = json!(true);
    assert_eq!(f.e.dispatch(&f.cfg, &f.pri), Err(Error("OSError")));
    let observation = read_json(&f.e.root.join("admission.json"), Value::Null).unwrap();
    assert_eq!(
        observation["entries"]["1"]["reason"],
        "existing_job_reserved"
    );
}

#[test]
fn repeated_known_terminal_no_action_is_bounded_and_ordinary_reconcile_can_settle_it() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    for count in 1..=3 {
        f.dispatch();
        assert_eq!(read_receipt(&f, 1)["receipt"]["replay_count"], count);
    }
    let first = read_receipt(&f, 1)["receipt"]["replay_first_at"].clone();
    assert_eq!(
        f.get(1)["scheduler"]["launch_outcome_deferred"]["reason"],
        "existing_job_no_action"
    );
    assert!(n(&f.get(1)["scheduler"]["not_before"]) > T);
    f.e.backend.clock += 310.;
    f.put(item(2, "other", "todo", &[]));
    f.e.backend.m["replayed"] = json!(false);
    f.dispatch();
    assert_eq!(f.e.backend.m["replayed"], false);
    assert_eq!(read_receipt(&f, 1)["receipt"]["replay_count"], 3);
    assert_eq!(
        read_json(&f.e.root.join("admission.json"), Value::Null).unwrap()["entries"]["1"]["reason"],
        "launch_outcome_deferred"
    );
    assert_eq!(f.get(2)["status"], "doing");
    assert_eq!(read_receipt(&f, 1)["receipt"]["replay_first_at"], first);
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.dispatch();
    f.reconcile();
    assert!(arr(&f.get(1)["jobs"]).contains(&json!("old-terminal")));
    assert_ne!(
        f.get(1)["scheduler"]["key"],
        f.e.backend.records[0]["launch_key"]
    );
}

#[test]
fn link_success_survives_subwork_assignment_and_worker_start_consuming_the_key() {
    for subwork in [false, true] {
        let mut f = Fixture::new();
        let mut sub_fixture = subwork.then(subwork_support::Fixture::new);
        let id = if subwork {
            let sub = sub_fixture.as_mut().unwrap();
            sub.register();
            sub.jobs[0]["pid"] = json!(1);
            sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
            sub.jobs[0]["created_at"] = json!(T - 1200.);
            f.e.backend.records = sub.jobs.clone();
            write_json(&f.e.todo_path, &sub.db, 1).unwrap();
            2
        } else {
            f.put(item(1, "other", "todo", &[]));
            f.e.backend.launch_start = Some(f.e.todo_path.clone());
            1
        };
        f.dispatch();
        let row = f.get(id);
        assert!(
            row["scheduler"]["key"].is_null(),
            "fixture must consume the launch key"
        );
        assert_eq!(f.e.report["launch_return"]["linked"], true);
        assert_eq!(f.e.report["launched"]["job"], "launched1");
        let report = read_receipt(&f, id as i64);
        assert_eq!(report["receipt"]["linked"], true);
        assert_eq!(
            report["receipt"]["result_generation"],
            row["resume_generation"]
        );
        assert_eq!(report["validity"], "matching_task_metadata");
    }
}

#[test]
fn malformed_snapshot_variants_fail_small_and_diagnostics_keep_missing_context_unknown() {
    let mut f = Fixture::new();
    let mut receipt = sample_receipt(&mut f, 1);
    receipt.outcome.kind = Kind::Unknown;
    receipt.binding = Binding::Unknown;
    launch_receipt::publish(&f.e.root, &receipt).unwrap();
    assert_eq!(read_receipt(&f, 1)["validity"], "unknown");
    assert_eq!(
        launch_receipt::explain(&f.e.root, &f.e.todo_path, 1, T - 1.)["validity"],
        "clock_mismatch"
    );
    assert_eq!(
        launch_receipt::explain(&f.e.root, &f.temp.path().join("absent-control.json"), 1, T)["validity"],
        "metadata_unavailable"
    );
    let original = read_json(&f.e.root.join("launch-outcomes.json"), Value::Null).unwrap();
    for mutation in ["duplicate", "too_many", "unknown", "linked", "truncated"] {
        let mut value = original.clone();
        match mutation {
            "duplicate" => value["receipts"]
                .as_array_mut()
                .unwrap()
                .push(serde_json::to_value(&receipt).unwrap()),
            "too_many" => {
                value["receipts"] = json!(vec![serde_json::to_value(&receipt).unwrap(); 129])
            }
            "unknown" => value["unrecognized"] = json!("NOT_OUTPUT"),
            "linked" => value["receipts"][0]["linked"] = json!(true),
            _ => {}
        }
        let bytes = if mutation == "truncated" {
            b"{\"schema\":1,".to_vec()
        } else {
            serde_json::to_vec(&value).unwrap()
        };
        fs::write(f.e.root.join("launch-outcomes.json"), bytes).unwrap();
        let report = read_receipt(&f, 1);
        assert_eq!(report["validity"], "corrupt_or_unreadable", "{mutation}");
        assert!(!report.to_string().contains("NOT_OUTPUT"));
    }
}

#[test]
fn large_valid_entries_are_trimmed_to_both_limits() {
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    for id in 1..=140 {
        let mut entry = receipt.clone();
        entry.target_todo_id = id;
        entry.outcome.todo_id = Some(id as u64);
        entry.receipt_id = uuid::Uuid::new_v4().simple().to_string();
        entry.outcome.job_id = "j".repeat(128);
        entry.request_generation = Some(u64::MAX);
        entry.result_generation = Some(u64::MAX);
        entry.request_attempt = Some(u64::MAX);
        entry.outcome.registration_generation = Some(u64::MAX);
        entry.outcome.registration_attempt = Some(u64::MAX);
        launch_receipt::publish(&f.e.root, &entry).unwrap();
    }
    let path = f.e.root.join("launch-outcomes.json");
    assert!(fs::metadata(&path).unwrap().len() <= launch_receipt::MAX_BYTES);
    let store = read_json(&path, Value::Null).unwrap();
    assert!(store["receipts"].as_array().unwrap().len() <= launch_receipt::MAX_ENTRIES);
    assert!(
        store["receipts"]
            .as_array()
            .unwrap()
            .iter()
            .any(|r| r["target_todo_id"] == 140)
    );
}

#[test]
fn scheduler_worktree_junction_matches_the_canonical_producer_cwd() {
    use std::os::windows::process::CommandExt;
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    let real = PathBuf::from(s(&f.get(1)["scheduler"]["worktree"]));
    fs::create_dir_all(&real).unwrap();
    let alias = f.temp.path().join("cwd-alias");
    let output = std::process::Command::new("cmd.exe")
        .creation_flags(NO_WINDOW)
        .args(["/d", "/c", "mklink", "/J"])
        .arg(&alias)
        .arg(&real)
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    let mut row = f.get(1);
    row["scheduler"]["worktree"] = json!(alias);
    let mut actual = receipt.outcome;
    actual.cwd_sha256 = cc_job_policy::launch_outcome::path_fingerprint(
        &fs::canonicalize(&real).unwrap().to_string_lossy(),
    );
    let linked = Receipt::new(&row, &json!(f.repo), &"ab".repeat(16), true, T, actual);
    assert_eq!(linked.binding, Binding::Matching);
}

#[test]
fn benign_launcher_prefix_lines_do_not_hide_the_last_typed_return() {
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    let mut backend = RealBackend::new(f.e.root.clone());
    backend.runner = Box::new(ReturnOutput(format!(
        "launcher diagnostic\n{}\n\n",
        serde_json::to_string(&receipt.outcome).unwrap()
    )));
    assert_eq!(
        backend
            .launch(&f.get(1), Path::new("fixture.md"), &json!({}))
            .unwrap()
            .kind,
        Kind::Queued
    );
}

#[test]
fn receipt_sharing_failure_has_a_receipt_specific_error() {
    use std::os::windows::fs::OpenOptionsExt;
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    launch_receipt::publish(&f.e.root, &receipt).unwrap();
    let _reader = fs::OpenOptions::new()
        .read(true)
        .share_mode(3)
        .open(f.e.root.join("launch-outcomes.json"))
        .unwrap();
    assert_eq!(
        launch_receipt::publish(&f.e.root, &receipt),
        Err(Error("LaunchReceiptSharingViolation"))
    );
}

#[test]
fn targeted_output_keeps_a_returned_effect_even_when_linkage_is_deferred() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.e.backend.replay_kind_override = Some(Kind::Accepted);
    let report = cc_scheduler::targeted_dispatch::run(&mut f.e, 1).unwrap();
    assert!(report["launched"].is_null());
    assert_eq!(report["launch_return"]["kind"], "accepted");
    assert_eq!(report["launch_return"]["linked"], false);
    assert_eq!(report["launch_return_error"], "launch_outcome_unverified");
}

struct LegacyPinnedLauncher(std::sync::Arc<std::sync::atomic::AtomicUsize>);
impl Runner for LegacyPinnedLauncher {
    fn run(&mut self, command: &Invocation) -> Result<Output> {
        self.0.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        assert!(!command.args.iter().any(|arg| arg == "--launch-outcome"));
        Ok(Output {
            code: 0,
            stdout: "legacy-fixed-job\n".into(),
            stderr: String::new(),
        })
    }
}
#[test]
fn historical_fixed_launcher_remains_id_only_and_unknown_without_a_second_launch() {
    let mut f = Fixture::new();
    sample_receipt(&mut f, 1);
    let mut row = f.get(1);
    row["product_review_allocation"] = json!({});
    let mut backend = RealBackend::new(f.e.root.clone());
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    backend.runner = Box::new(LegacyPinnedLauncher(calls.clone()));
    let result = backend
        .launch(&row, Path::new("fixture.md"), &json!({}))
        .unwrap();
    assert_eq!(result.kind, Kind::Unknown);
    assert_eq!(result.job_id, "legacy-fixed-job");
    assert_eq!(calls.load(std::sync::atomic::Ordering::Relaxed), 1);
}

#[test]
fn admission_rejects_uppercase_fingerprints_in_the_shared_lowercase_domain() {
    let mut f = Fixture::new();
    f.put(item(1, "other", "todo", &[]));
    f.dispatch();
    let path = f.e.root.join("admission.json");
    let mut value = read_json(&path, Value::Null).unwrap();
    value["entries"]["1"]["metadata_fingerprint"] = json!("AB".repeat(32));
    fs::write(&path, value.to_string()).unwrap();
    assert_eq!(
        cc_scheduler::admission::explain(&f.e.root, &f.e.todo_path, 1, T)["validity"],
        "corrupt_or_unreadable"
    );
}

#[test]
fn exact_protocol_failures_and_saved_diagnostics_are_not_generic_tool_errors() {
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    let mut backend = RealBackend::new(f.e.root.clone());
    let mut future = serde_json::to_value(&receipt.outcome).unwrap();
    future["schema"] = json!(2);
    let mut invalid = serde_json::to_value(&receipt.outcome).unwrap();
    invalid["job_id"] = json!("bad/id");
    for (text, code) in [
        (
            "{\"schema\":2}".to_string(),
            "LaunchOutcomeSchemaUnsupported",
        ),
        (future.to_string(), "LaunchOutcomeSchemaUnsupported"),
        (invalid.to_string(), "LaunchOutcomeInvalid"),
        ("x".repeat(16385), "LaunchOutcomeTooLarge"),
    ] {
        backend.runner = Box::new(ReturnOutput(text));
        assert_eq!(
            backend.launch(&f.get(1), Path::new("fixture.md"), &json!({})),
            Err(Error(code))
        );
    }
    for code in ["unsupported", "schema", "invalid", "too_large"] {
        let mut f = Fixture::new();
        f.put(item(1, "other", "todo", &[]));
        f.put(item(2, "other", "todo", &[]));
        f.e.backend.m["launch_error"] = json!(code);
        assert!(f.e.dispatch(&f.cfg, &f.pri).is_err());
        let expected = if code == "schema" {
            "launch_outcome_schema_unsupported".to_string()
        } else {
            format!("launch_outcome_{code}")
        };
        assert_eq!(
            f.get(1)["scheduler"]["preparation_failure"]["reason"],
            expected
        );
        let observation = read_json(&f.e.root.join("admission.json"), Value::Null).unwrap();
        assert_eq!(observation["entries"]["1"]["failure_code"], expected);
        if code == "unsupported" {
            assert_eq!(f.e.backend.m["launch_calls"], 1);
        }
    }
}

#[test]
fn accepted_live_registration_and_persisted_defer_are_explained_without_replay() {
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.e.backend.records[0]["status"] = json!("running");
    f.e.backend.replay_kind_override = Some(Kind::Accepted);
    f.dispatch();
    assert_eq!(f.e.report["launch_return"]["kind"], "accepted");
    assert_eq!(f.e.report["launch_return"]["linked"], true);
    let mut f = Fixture::new();
    terminal_same_key(&mut f);
    f.e.backend.replay_kind_override = Some(Kind::Unknown);
    assert_eq!(
        f.e.dispatch(&f.cfg, &f.pri),
        Err(Error("LaunchOutcomeUnverified"))
    );
    let report = cc_scheduler::targeted_dispatch::run(&mut f.e, 1).unwrap();
    assert_eq!(
        report["launch_deferred_reason"],
        "launch_outcome_unverified"
    );
}

#[test]
fn count_limit_keeps_the_newly_returned_receipt_when_the_clock_moves_back() {
    let mut f = Fixture::new();
    let receipt = sample_receipt(&mut f, 1);
    for id in 1..=launch_receipt::MAX_ENTRIES as i64 {
        let mut entry = receipt.clone();
        entry.target_todo_id = id;
        entry.outcome.todo_id = Some(id as u64);
        entry.receipt_id = uuid::Uuid::new_v4().simple().to_string();
        launch_receipt::publish(&f.e.root, &entry).unwrap();
    }
    let mut earlier = receipt.clone();
    earlier.target_todo_id = 999;
    earlier.outcome.todo_id = Some(999);
    earlier.receipt_id = uuid::Uuid::new_v4().simple().to_string();
    earlier.observed_at = T - 1.;
    earlier.expires_at = earlier.observed_at + launch_receipt::TTL;
    launch_receipt::publish(&f.e.root, &earlier).unwrap();
    assert_eq!(
        read_receipt(&f, 999)["receipt"]["receipt_id"],
        earlier.receipt_id
    );
}

#[test]
fn historical_allocation_cannot_run_after_its_fixed_original_expiry() {
    assert!(cc_scheduler::product_review::in_scope(1792540799.));
    assert!(!cc_scheduler::product_review::in_scope(1792540800.));
    assert!(!cc_scheduler::product_review::in_scope(1793149200.));
}
