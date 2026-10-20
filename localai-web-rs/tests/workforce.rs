use localai_web_rs::workforce::project;
use serde_json::{json, Value};

const NOW: u64 = 1_000_000;

#[test]
fn empty_department_does_not_discard_unassigned_work_or_certify_global_zero() {
    let mut input = board();
    input["departments"]
        .as_array_mut()
        .unwrap()
        .push(json!({"products":[]}));
    input["unassignedProducts"] = json!([{"workers":[
        {"id":"todo-88","priority":40,"recordedStatus":"wait","dependencies":[9]}
    ]}]);
    let mut evidence = active();
    evidence["status"] = json!("degraded");
    evidence["sources"]["jobs"]["complete"] = json!(false);
    let data = project(&input, Some(&evidence), NOW);
    assert_eq!(data["inventory"]["taskRecords"], 7);
    let task = data["tasks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|t| t["todoId"] == 88)
        .unwrap();
    assert_eq!(task["priority"], 40);
    assert_eq!(task["state"], "dependency_wait");
    assert_eq!(data["recorded"]["jobQueued"], 8);
    assert_eq!(data["processing"]["verifiedCount"], 1);
    assert!(data["processing"]["total"].is_null());
    assert_eq!(data["coverage"][1]["state"], "unobserved");
    assert_eq!(data["coverage"][2]["state"], "unobserved");
    assert_eq!(input["departments"][1]["products"], json!([]));
}

fn board() -> Value {
    json!({"fetchedAt":NOW,"sourceOk":false,"metadataOk":true,"protectionMode":"masked_metadata",
        "metrics":{"activeJobs":4,"jobQueued":8,"consoleQueued":268,"consolePreparationFailures":21,"recoveryUnverified":192,"todoDependencyWaits":69},
        "departments":[{"products":[{"workers":[
            {"id":"todo-1","recordedStatus":"recovery_unverified","status":"waiting","progress":null},
            {"id":"todo-2","recordedStatus":"wait","status":"waiting","dependencies":[9],"progress":0},
            {"id":"todo-3","recordedStatus":"doing","status":"working","jobStatus":"running","progress":42},
            {"id":"todo-4","recordedStatus":"doing","status":"waiting","jobStatus":"queued"},
            {"id":"todo-5","recordedStatus":"block","status":"waiting","waitingKind":"unverified"},
            {"id":"todo-6","recordedStatus":"done","status":"done"}
        ]}]}],"unassignedProducts":[]})
}

fn active() -> Value {
    json!({"schema_version":1,"processing_contract":"agent-progress-active-v1","captured_at":"1970-01-01T00:16:39Z","heartbeat_at":"1970-01-01T00:16:40Z",
        "status":"ok","truncated":false,"issues":[],"sources":{"todo":{"status":"ok","complete":true},"jobs":{"status":"ok","complete":true}},
        "jobs":[{"processing_verified":true,"job_key":"job-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "todo_ids":[3],"recorded_status":"running","execution":"running","worker_presence":"verified_alive",
            "phase":"tool","wait_reasons":[],"source":{"status":"ok","complete":true},
            "last_activity":{"at":"1970-01-01T00:16:38Z","state":"fresh"}}]})
}

#[test]
fn task_records_are_not_workers_and_metric_scopes_are_not_added() {
    let data = project(&board(), None, NOW);
    assert_eq!(data["inventory"]["taskRecords"], 6);
    assert_eq!(data["inventory"]["unfinishedTaskRecords"], 5);
    assert_eq!(data["states"]["recovery_unverified"], 1);
    assert_eq!(data["states"]["dependency_wait"], 1);
    assert_eq!(data["states"]["blocked_unknown"], 1);
    assert_eq!(data["recorded"]["activeJobs"], 4);
    assert_eq!(data["recorded"]["consoleQueued"], 268);
    assert_eq!(data["recorded"]["recoveryUnverified"], 192);
    assert!(data["processing"]["total"].is_null());
    assert!(data["processing"]["verifiedCount"].is_null());
}

#[test]
fn processing_requires_identity_recent_processing_and_no_wait() {
    let base = active();
    assert_eq!(
        project(&board(), Some(&base), NOW)["processing"]["verifiedCount"],
        1
    );
    for (key, value) in [
        ("worker_presence", json!("alive_identity_unknown")),
        ("execution", json!("supervising")),
        ("recorded_status", json!("queued")),
        ("phase", json!("reporting")),
        ("phase", json!("planning")),
        ("phase", json!("handoff")),
        ("wait_reasons", json!([{"kind":"cargo"}])),
    ] {
        let mut input = base.clone();
        input["jobs"][0][key] = value;
        assert_eq!(
            project(&board(), Some(&input), NOW)["processing"]["verifiedCount"],
            0,
            "{key}"
        );
    }
    let mut input = base.clone();
    input["jobs"][0]["last_activity"]["at"] = json!("1970-01-01T00:16:09Z");
    assert_eq!(
        project(&board(), Some(&input), NOW)["processing"]["verifiedCount"],
        0
    );
    input["jobs"][0]["last_activity"]["at"] = json!("1970-01-01T00:16:41Z");
    assert_eq!(
        project(&board(), Some(&input), NOW)["processing"]["verifiedCount"],
        0
    );
}

#[test]
fn incomplete_and_stale_evidence_never_certifies_a_total_or_current_zero() {
    let mut input = active();
    input["status"] = json!("degraded");
    input["sources"]["jobs"] = json!({"status":"partial","complete":false});
    let data = project(&board(), Some(&input), NOW);
    assert_eq!(data["processing"]["verifiedCount"], 1);
    assert!(data["processing"]["total"].is_null());
    assert_eq!(data["processing"]["state"], "partial");
    let data = project(&board(), Some(&input), NOW + 31_000);
    assert_eq!(data["processing"]["state"], "stale");
    assert!(data["processing"]["verifiedCount"].is_null());
    assert!(data["processing"]["total"].is_null());
    assert_eq!(data["processing"]["observedAt"], 1_000_000);
}

#[test]
fn unknown_progress_and_result_time_are_not_synthesized_from_updates() {
    let mut input = board();
    input["departments"][0]["products"][0]["workers"][0]["updatedAt"] =
        json!("1970-01-01T00:16:40Z");
    let data = project(&input, None, NOW);
    assert_eq!(data["inventory"]["measuredProgressRecords"], 2);
    assert_eq!(data["inventory"]["resultTimeRecords"], 0);
    assert!(data["tasks"][0]["progress"].is_null());
    assert_eq!(data["tasks"][1]["progress"], 0);
    assert!(data["tasks"][0]["lastResultAt"].is_null());
}

#[test]
fn protection_boundary_reprojects_only_fixed_vocabulary_and_numeric_references() {
    let mut input = board();
    input["departments"][0]["products"][0]["name"] = json!("fixture-private-name");
    input["departments"][0]["products"][0]["workers"][2]["summary"] =
        json!("fixture-private-summary");
    input["departments"][0]["products"][0]["workers"][2]["session"] = json!("fixture-private-path");
    let mut evidence = active();
    evidence["jobs"][0]["public_label"] = json!("fixture-private-name");
    evidence["jobs"][0]["source"]["error"] = json!("fixture-private-error");
    let data = project(&input, Some(&evidence), NOW);
    assert!(!data.to_string().contains("fixture-private"));
    assert_eq!(data["tasks"][2]["todoId"], 3);
    assert_eq!(data["coverage"][1]["kind"], "native_codex");
    assert_eq!(data["coverage"][1]["state"], "unobserved");
    assert_eq!(data["coverage"][2]["kind"], "claude");
    assert_eq!(data["coverage"][2]["state"], "unobserved");
}

#[test]
fn repeated_job_and_task_references_are_counted_once() {
    let mut input = board();
    input["unassignedProducts"] =
        json!([{"workers":[input["departments"][0]["products"][0]["workers"][2].clone()]}]);
    let mut evidence = active();
    let duplicate = evidence["jobs"][0].clone();
    evidence["jobs"].as_array_mut().unwrap().push(duplicate);
    let data = project(&input, Some(&evidence), NOW);
    assert_eq!(data["inventory"]["taskRecords"], 6);
    assert_eq!(data["processing"]["verifiedCount"], 1);
    assert_eq!(data["tasks"][2]["state"], "processing");
}

#[test]
fn failed_metadata_retains_unknown_counts_rather_than_empty_success() {
    let data = project(
        &json!({"metadataOk":false,"fetchedAt":NOW,"departments":[]}),
        None,
        NOW,
    );
    assert_eq!(data["state"], "unavailable");
    assert!(data["inventory"]["taskRecords"].is_null());
    assert!(data["states"]["dependency_wait"].is_null());
    assert!(data["recorded"]["activeJobs"].is_null());
}

#[test]
fn newer_canonical_wait_prevents_old_processing_cache_from_resurrecting_work() {
    let mut input = board();
    input["departments"][0]["products"][0]["workers"][2]["recordedStatus"] = json!("wait");
    input["departments"][0]["products"][0]["workers"][2]["dependencies"] = json!([9]);
    let data = project(&input, Some(&active()), NOW);
    assert_eq!(data["tasks"][2]["state"], "dependency_wait");
    assert_eq!(data["processing"]["verifiedCount"], 0);
}

#[test]
fn missing_future_and_stale_board_times_do_not_certify_current_task_counts() {
    let mut input = board();
    input["fetchedAt"] = Value::Null;
    assert_eq!(project(&input, None, NOW)["state"], "unavailable");
    input["fetchedAt"] = json!(NOW + 1);
    assert_eq!(project(&input, None, NOW)["state"], "unavailable");
    let data = project(&board(), Some(&active()), NOW + 31_000);
    assert_eq!(data["state"], "stale");
    assert_eq!(data["observedAt"], NOW);
    assert_ne!(data["tasks"][2]["state"], "processing");
}

#[test]
fn collector_must_certify_resource_and_cancellation_checks() {
    let mut input = active();
    input["jobs"][0]
        .as_object_mut()
        .unwrap()
        .remove("processing_verified");
    let data = project(&board(), Some(&input), NOW);
    assert_ne!(data["tasks"][2]["state"], "processing");
    assert_eq!(data["processing"]["state"], "partial");
    assert!(data["processing"]["total"].is_null());
}
#[test]
fn missing_or_malformed_collections_cannot_be_certified_as_complete_counts() {
    let mut absent = board();
    absent.as_object_mut().unwrap().remove("unassignedProducts");
    let mut invalid_department = board();
    invalid_department["departments"][0]["products"] = Value::Null;
    let mut invalid_product = board();
    invalid_product["departments"][0]["products"][0]["workers"] = Value::Null;
    let mut invalid_unassigned = board();
    invalid_unassigned["unassignedProducts"] = json!([{"workers":"not an array"}]);
    for input in [
        absent,
        invalid_department,
        invalid_product,
        invalid_unassigned,
    ] {
        let data = project(&input, None, NOW);
        assert_eq!(data["state"], "unavailable");
        assert!(data["inventory"]["taskRecords"].is_null());
        assert!(data["states"]["dependency_wait"].is_null());
    }
}

#[test]
fn unrecognized_records_are_unknown_instead_of_invented_jobs() {
    let mut input = board();
    input["unassignedProducts"] = json!([{"workers":[
        {"id":"unknown"},{"id":"todo-0"},{"id":"job-0123456789abcdef"},
        {"id":"job-0123456789abcdef"}
    ]}]);
    let data = project(&input, None, NOW);
    assert_eq!(data["inventory"]["jobOnlyRecords"], 1);
    assert_eq!(data["inventory"]["unknownRecords"], 2);
}
