use super::*;
use serde_json::json;
use std::fs;

struct Fixture {
    temp: tempfile::TempDir,
    db: Value,
    paths: Paths,
}
impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let paths = Paths::at(temp.path(), &temp.path().join("jobs"));
        let db = json!({"projects":{"tools":temp.path()},"items":[
            {"id":42,"project":"tools","status":"wait","note":"unfinished original",
             "note_history":[{"note":"prior"}],"updated":"unchanged","progress":70,
             "wait_on":[17],"jobs":["original"],"resume_generation":7,
             "worker_receipt":{"job":"original","generation":7},"owner_stopped":true,
             "scheduler":{"phase":"waiting","job":"original","key":"keep-key"}},
            {"id":17,"project":"tools","status":"todo","note":"other row"}]});
        let mut f = Self { temp, db, paths };
        f.write("input.json", &json!({
            "domain":"discord-native-partial-implementation-v1","kind":"native_partial_implementation",
            "request_id":"normal-ask-native","message_id":"400000000000000001",
            "source_receipt_sha256":"a".repeat(64),"reply_sha256":"b".repeat(64),
            "origin_todo_id":"42","project":"tools","revision":"c".repeat(40),
            "parent_thread_id":"00000000-0000-4000-8000-000000000001",
            "child_thread_id":"00000000-0000-4000-8000-000000000002",
            "assignment":{"turn_id":"00000000-0000-4000-8000-000000000003",
                "item_id":"sendInput-native-assignment","prompt_sha256":"d".repeat(64)},
            "verification_commands":[{"turn_id":"00000000-0000-4000-8000-000000000003",
                "item_id":"exec-00000000-0000-4000-8000-000000000004",
                "command_sha256":"e".repeat(64),"output_sha256":"f".repeat(64)}],
            "artifact":{"path":f.temp.path().join("binding.json"),"sha256":"1".repeat(64)},
            "evidence":{"completion":"partial source result","execution":"verified commands",
                "origin_saved":"GUI and dependency retained","identity":"native source qualification",
                "applied_at":"2026-11-10T00:00:00Z"}
        }));
        f.snapshot();
        f
    }
    fn file(&self, name: &str) -> std::path::PathBuf {
        self.temp.path().join(name)
    }
    fn write(&self, name: &str, value: &Value) {
        fs::write(self.file(name), serde_json::to_vec(value).unwrap()).unwrap();
    }
    fn input(&self) -> Value {
        serde_json::from_slice(&fs::read(self.file("input.json")).unwrap()).unwrap()
    }
    fn snapshot(&mut self) {
        self.write("state.json", &self.db["items"][0]);
        self.write("scope.json", &json!({"domain":"discord-conversation-todo-scope-v1",
            "todo_id":"42","project":"tools","project_path_sha256":sha(self.db["projects"]["tools"].as_str().unwrap().as_bytes())}));
    }
    fn proof(&self) -> Value {
        let input = self.input();
        json!({"domain":"discord-native-partial-verification-v1","verified":true,
            "input_sha256":sha(&fs::read(self.file("input.json")).unwrap()),
            "origin_state_sha256":sha(&fs::read(self.file("state.json")).unwrap()),
            "project_path_sha256":sha(self.db["projects"]["tools"].as_str().unwrap().as_bytes()),
            "source_receipt_sha256":input["source_receipt_sha256"],"reply_sha256":input["reply_sha256"],
            "native_evidence_sha256":"2".repeat(64),"acceptance":"pending",
            "application_inferred":false,"question_closed_get_verified":false})
    }
    fn call(&mut self, verify: bool, proof: Result<Vec<u8>>) -> Result<Value> {
        let input = self.file("input.json");
        let scope = self.file("scope.json");
        let state = self.file("state.json");
        record_with(&mut self.db, &input, &scope, &state, verify, &mut |_, _| {
            proof.clone()
        })
    }
    fn good(&mut self, verify: bool) -> Result<Value> {
        self.call(verify, Ok(serde_json::to_vec(&self.proof()).unwrap()))
    }
}

#[test]
fn native_partial_records_only_origin_and_keeps_acceptance_pending() {
    let mut f = Fixture::new();
    let before = f.db.clone();
    let response = f.good(false).unwrap();
    assert_eq!(
        response["domain"],
        "todo-native-partial-implementation-record-v1"
    );
    assert_eq!(response["kind"], "native_partial_implementation");
    assert_eq!(response["changed"], true);
    assert_eq!(response["acceptance"], "pending");
    assert_eq!(response["application_inferred"], false);
    assert_eq!(response["question_closed_get_verified"], false);
    let saved = f.db["items"][0][RECORDS][0].clone();
    assert_eq!(
        saved["verification"]["native_evidence_sha256"],
        "2".repeat(64)
    );
    assert!(
        f.db["items"][0]
            .get("owner_implementation_records")
            .is_none()
    );
    let mut restored = f.db.clone();
    restored["items"][0]
        .as_object_mut()
        .unwrap()
        .remove(RECORDS);
    assert_eq!(
        restored, before,
        "status, owners, holds, dependencies, notes and other rows must stay exact"
    );
}

#[test]
fn native_partial_retry_and_verify_use_current_snapshot_without_duplicate() {
    let mut f = Fixture::new();
    let original = f.good(false).unwrap();
    let recorded = f.db.clone();
    assert!(
        f.good(false).is_err(),
        "pre-record snapshot must not read back a changed origin"
    );
    assert_eq!(f.db, recorded);
    f.snapshot();
    for verify in [false, true] {
        let output = f.good(verify).unwrap();
        assert_eq!(output["changed"], false);
        assert_eq!(output["receipt_sha256"], original["receipt_sha256"]);
        assert_eq!(f.db, recorded);
    }
}

#[test]
fn native_partial_rejects_unverified_or_mismatched_projection_without_mutation() {
    for key in [
        "domain",
        "verified",
        "input_sha256",
        "origin_state_sha256",
        "project_path_sha256",
        "source_receipt_sha256",
        "reply_sha256",
        "native_evidence_sha256",
        "acceptance",
        "application_inferred",
        "question_closed_get_verified",
    ] {
        for change in [0, 1, 2] {
            let mut f = Fixture::new();
            let before = f.db.clone();
            let mut proof = f.proof();
            if change == 0 {
                proof.as_object_mut().unwrap().remove(key);
            } else if change == 1 {
                proof[key] = Value::Null;
            } else {
                proof[key] = match key {
                    "verified" => json!(false),
                    "application_inferred" | "question_closed_get_verified" => json!(true),
                    "native_evidence_sha256" => json!("invalid"),
                    _ => json!("mismatch"),
                };
            }
            assert!(
                f.call(false, Ok(serde_json::to_vec(&proof).unwrap()))
                    .is_err(),
                "bad field {key} change{change}"
            );
            assert_eq!(f.db, before);
        }
    }
    for proof in [
        Err("private verification error that must not leak".into()),
        Ok(b"not json".to_vec()),
    ] {
        let mut f = Fixture::new();
        let before = f.db.clone();
        let error = f.call(false, proof).unwrap_err();
        assert!(!error.contains("private verification error"));
        assert_eq!(f.db, before);
    }
}

#[test]
fn native_partial_rejects_cas_scope_and_same_version_conflicts() {
    for field in [
        "status",
        "resume_generation",
        "jobs",
        "wait_on",
        "worker_receipt",
        "owner_stopped",
    ] {
        let mut f = Fixture::new();
        f.db["items"][0][field] = json!("concurrent change");
        let before = f.db.clone();
        assert!(f.good(false).is_err());
        assert_eq!(f.db, before);
    }
    let mut f = Fixture::new();
    f.db["projects"]["tools"] = json!("different root");
    let before = f.db.clone();
    assert!(f.good(false).is_err());
    assert_eq!(f.db, before);
    let mut f = Fixture::new();
    f.good(false).unwrap();
    f.snapshot();
    let mut input = f.input();
    input["revision"] = json!("3".repeat(40));
    f.write("input.json", &input);
    let before = f.db.clone();
    assert!(f.good(false).unwrap_err().contains("conflict"));
    assert_eq!(f.db, before);
    let mut f = Fixture::new();
    let before = f.db.clone();
    assert!(f.good(true).is_err());
    assert_eq!(f.db, before);
}

#[test]
fn native_partial_rejects_extra_duplicate_keys_and_false_kind() {
    for invalid in [
        "extra",
        "kind",
        "commands0",
        "commands17",
        "same_actor",
        "origin_number",
        "artifact_relative",
    ] {
        let mut f = Fixture::new();
        let mut input = f.input();
        match invalid {
            "extra" => input["extra"] = json!(false),
            "kind" => input["kind"] = json!("implementation"),
            "commands0" => input["verification_commands"] = json!([]),
            "commands17" => {
                input["verification_commands"] =
                    json!(vec![input["verification_commands"][0].clone(); 17])
            }
            "same_actor" => input["child_thread_id"] = input["parent_thread_id"].clone(),
            "origin_number" => input["origin_todo_id"] = json!(42),
            "artifact_relative" => input["artifact"]["path"] = json!("relative.json"),
            _ => unreachable!(),
        }
        f.write("input.json", &input);
        let before = f.db.clone();
        assert!(f.good(false).is_err(), "{invalid}");
        assert_eq!(f.db, before);
    }
    let mut f = Fixture::new();
    let mut raw = serde_json::to_string(&f.input()).unwrap();
    raw.insert_str(
        1,
        "\"domain\":\"discord-native-partial-implementation-v1\",",
    );
    fs::write(f.file("input.json"), raw).unwrap();
    let before = f.db.clone();
    assert!(f.good(false).is_err());
    assert_eq!(f.db, before);
    let mut f = Fixture::new();
    let mut proof = f.proof();
    proof["extra"] = json!(true);
    let before = f.db.clone();
    assert!(
        f.call(false, Ok(serde_json::to_vec(&proof).unwrap()))
            .is_err()
    );
    assert_eq!(f.db, before);
}

#[test]
fn native_partial_verifier_is_called_for_replay_and_saved_receipt_cannot_be_forged() {
    let mut f = Fixture::new();
    f.good(false).unwrap();
    f.snapshot();
    let before = f.db.clone();
    assert!(f.call(true, Err("checker failed".into())).is_err());
    assert_eq!(f.db, before);
    f.db["items"][0][RECORDS][0]["acceptance"] = json!("accepted");
    f.snapshot();
    let before = f.db.clone();
    assert!(f.good(true).is_err());
    assert_eq!(f.db, before);
}

#[test]
fn native_partial_production_has_no_checker_or_store_override() {
    let mut f = Fixture::new();
    let before = f.db.clone();
    let (input, scope, state) = (
        f.file("input.json"),
        f.file("scope.json"),
        f.file("state.json"),
    );
    let error = record(&mut f.db, &f.paths, &input, &scope, &state, false).unwrap_err();
    assert_eq!(error, "native_implementation_production_store_required");
    assert_eq!(f.db, before);
    use clap::Parser;
    for flag in ["--checker-path", "--config", "--actor-todo-id", "--job-id"] {
        assert!(
            crate::Cli::try_parse_from([
                "todo",
                "native-implementation-record",
                "--input-file",
                "a",
                "--origin-scope-file",
                "b",
                "--origin-state-file",
                "c",
                flag,
                "fake"
            ])
            .is_err()
        );
    }
}

#[test]
fn native_partial_snapshot_hash_is_raw_bytes_and_saved_evidence_must_be_stable() {
    let mut f = Fixture::new();
    let compact = fs::read(f.file("state.json")).unwrap();
    let pretty = serde_json::to_vec_pretty(&f.db["items"][0]).unwrap();
    assert_ne!(sha(&compact), sha(&pretty));
    let stale = f.proof();
    fs::write(f.file("state.json"), pretty).unwrap();
    let before = f.db.clone();
    assert!(
        f.call(false, Ok(serde_json::to_vec(&stale).unwrap()))
            .is_err()
    );
    assert_eq!(f.db, before);
    f.good(false).unwrap();
    f.snapshot();
    let before = f.db.clone();
    let mut changed = f.proof();
    changed["native_evidence_sha256"] = json!("3".repeat(64));
    assert!(
        f.call(true, Ok(serde_json::to_vec(&changed).unwrap()))
            .unwrap_err()
            .contains("native_evidence_changed")
    );
    assert_eq!(f.db, before);
}

#[test]
fn native_partial_receipt_identity_survives_canonical_store_key_reordering() {
    fn reorder(value: &Value) -> Value {
        match value {
            Value::Object(map) => Value::Object(
                map.iter()
                    .rev()
                    .map(|(key, value)| (key.clone(), reorder(value)))
                    .collect(),
            ),
            Value::Array(values) => Value::Array(values.iter().map(reorder).collect()),
            _ => value.clone(),
        }
    }
    let mut f = Fixture::new();
    let original = f.good(false).unwrap();
    f.db = reorder(&f.db);
    f.snapshot();
    let before = f.db.clone();
    let readback = f.good(true).unwrap();
    assert_eq!(f.db, before);
    assert_eq!(readback["receipt_sha256"], original["receipt_sha256"]);
}

#[test]
fn native_partial_refuses_duplicate_origin_or_duplicate_receipt() {
    let mut f = Fixture::new();
    let copy = f.db["items"][0].clone();
    f.db["items"].as_array_mut().unwrap().push(copy);
    let before = f.db.clone();
    assert!(f.good(false).is_err());
    assert_eq!(f.db, before);
    let mut f = Fixture::new();
    f.good(false).unwrap();
    let copy = f.db["items"][0][RECORDS][0].clone();
    f.db["items"][0][RECORDS].as_array_mut().unwrap().push(copy);
    f.snapshot();
    let before = f.db.clone();
    assert!(f.good(true).is_err());
    assert_eq!(f.db, before);
}

#[test]
fn native_partial_snapshot_cannot_change_during_check() {
    let mut f = Fixture::new();
    let input = f.file("input.json");
    let scope = f.file("scope.json");
    let state = f.file("state.json");
    let proof = serde_json::to_vec(&f.proof()).unwrap();
    let before = f.db.clone();
    let mut wrote = false;
    let result = record_with(&mut f.db, &input, &scope, &state, false, &mut |_, state| {
        wrote = fs::write(state, b"changed").is_ok();
        Ok(proof.clone())
    });
    if wrote {
        assert!(result.is_err());
        assert_eq!(f.db, before);
    } else {
        assert!(
            result.is_ok(),
            "share-read denial retains the exact verified file"
        );
    }
}

#[test]
fn native_partial_capture_handles_exit_deadline_limit_and_descendant() {
    let dir = tempfile::tempdir().unwrap();
    for (mode, budget, expected) in [
        (
            "failure",
            Duration::from_secs(5),
            "native_implementation_verifier_exit",
        ),
        (
            "flood",
            Duration::from_secs(5),
            "native_implementation_verification_limit",
        ),
        (
            "sleep",
            Duration::from_millis(80),
            "native_implementation_verifier_deadline",
        ),
    ] {
        let error = run_checker_command(process_fixture(mode), dir.path(), budget).unwrap_err();
        assert_eq!(error, expected);
        assert!(!error.contains("private payload"));
        assert_eq!(
            fs::read_dir(dir.path()).unwrap().count(),
            0,
            "capture removed after {mode}"
        );
    }
    let started = Instant::now();
    let out = run_checker_command(
        process_fixture("descendant"),
        dir.path(),
        Duration::from_secs(5),
    )
    .unwrap();
    assert!(!out.is_empty());
    assert!(started.elapsed() < Duration::from_secs(4));
    assert_eq!(fs::read_dir(dir.path()).unwrap().count(), 0);
}
fn process_fixture(mode: &str) -> Command {
    let mut command = Command::new(std::env::current_exe().unwrap());
    command
        .args([
            "--exact",
            "native_implementation::tests::native_process_fixture",
            "--nocapture",
        ])
        .env("CC_NATIVE_PARTIAL_TEST_FIXTURE", mode);
    command
}
#[test]
fn native_process_fixture() {
    // This test-only child lives under the real process-group capture boundary.
    match std::env::var("CC_NATIVE_PARTIAL_TEST_FIXTURE").as_deref() {
        Ok("failure") => {
            eprintln!("private payload");
            std::process::exit(7)
        }
        Ok("flood") => {
            print!("{}", "x".repeat(OUTPUT_LIMIT as usize + 4096));
        }
        Ok("sleep") => std::thread::sleep(Duration::from_secs(10)),
        Ok("descendant") => {
            let mut child = process_fixture("sleep").spawn().unwrap();
            std::thread::sleep(Duration::from_millis(80));
            let _ = child.try_wait();
        }
        _ => (),
    }
}

#[test]
fn native_partial_cli_requires_exact_origin_inputs() {
    use clap::Parser;
    let parsed = crate::Cli::try_parse_from([
        "todo",
        "native-implementation-record",
        "--input-file",
        "input.json",
        "--origin-scope-file",
        "scope.json",
        "--origin-state-file",
        "state.json",
        "--verify-only",
    ]);
    assert!(
        parsed.is_ok(),
        "the dedicated native CLI must accept current origin snapshots"
    );
}
