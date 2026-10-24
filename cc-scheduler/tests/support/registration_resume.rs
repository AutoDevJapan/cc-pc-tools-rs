use super::*;
use cc_scheduler::registration_resume;

fn setup() -> (Fixture, Value) {
    let mut f = Fixture::new();
    f.e.backend.repair_allowed = true;
    let mut db = f.e.todo().unwrap();
    db["projects"]["cc-pc-tools-rs"] = json!(f.repo);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    let mut row = item(2467, "cc-pc-tools-rs", "todo", &[]);
    row["dispatchable"] = json!(false);
    row["scheduler"] = json!({"phase":"retry","resume":true,"not_before":0});
    row["note"] = json!("Keep the original unfinished work.");
    row["sandbox"] = json!("read-only");
    row["network"] = json!(false);
    f.put(row);
    f.put(item(99, "other", "todo", &[]));
    (
        f,
        json!({"todo_id":2467,"intent":"resume_registration_only",
        "request":"Resume the original task.","source_ref":"current request"}),
    )
}

#[test]
fn registration_resume_restores_only_registration_and_normal_dispatch_still_runs() {
    let (mut f, request) = setup();
    let before = f.get(2467);
    let other = f.get(99);
    let preview = registration_resume::run(&mut f.e, 2467, &request, None).unwrap();
    assert_eq!(preview["eligible"], true);
    assert_eq!(f.get(2467), before);
    assert!(f.e.backend.launched.is_empty());
    let applied =
        registration_resume::run(&mut f.e, 2467, &request, preview["state_sha256"].as_str())
            .unwrap();
    assert_eq!(applied["saved"], true);
    assert_eq!(applied["launched"], 0);
    let mut after = f.get(2467);
    assert_eq!(after["dispatchable"], true);
    assert_eq!(
        after["registration_resume_history"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    after["dispatchable"] = json!(false);
    after
        .as_object_mut()
        .unwrap()
        .remove("registration_resume_history");
    after.as_object_mut().unwrap().remove("updated");
    let mut comparable = before;
    comparable.as_object_mut().unwrap().remove("updated");
    assert_eq!(after, comparable);
    assert_eq!(f.get(99), other);
    let repeated = registration_resume::run(&mut f.e, 2467, &request, None).unwrap();
    assert_eq!(repeated["eligible"], false);
    assert!(f.e.backend.launched.is_empty());
    write_json(&f.e.root.join("config.json"), &f.cfg, 1).unwrap();
    write_json(&f.e.root.join("priorities.json"), &f.pri, 1).unwrap();
    let result = cc_scheduler::targeted_dispatch::run(&mut f.e, 2467).unwrap();
    assert_eq!(result["launched"], "launched1");
    assert_eq!(f.e.backend.launched[0]["item"]["id"], 2467);
    assert!(f.e.backend.cancelled.is_empty());
    assert!(f.e.backend.killed.is_empty());
    assert!(f.e.backend.notices.is_empty());
}

#[test]
fn registration_resume_binds_both_the_record_and_request_and_keeps_unknown_scope() {
    let (mut f, mut request) = setup();
    let before = f.get(2467);
    let preview = registration_resume::run(&mut f.e, 2467, &request, None).unwrap();
    request["request"] = json!("A different request.");
    assert_eq!(
        registration_resume::run(&mut f.e, 2467, &request, preview["state_sha256"].as_str())
            .unwrap_err()
            .0,
        "InputChanged"
    );
    assert_eq!(f.get(2467), before);
    f.e.backend.repair_allowed = false;
    let preview = registration_resume::run(&mut f.e, 2467, &request, None).unwrap();
    assert_eq!(preview["checks"]["source_scope"], false);
    let result =
        registration_resume::run(&mut f.e, 2467, &request, preview["state_sha256"].as_str())
            .unwrap();
    assert_eq!(result["saved"], false);
    assert_eq!(f.get(2467), before);
}

#[test]
fn registration_resume_does_not_overwrite_a_concurrent_owner_stop() {
    let (mut f, request) = setup();
    let preview = registration_resume::run(&mut f.e, 2467, &request, None).unwrap();
    f.e.edit(&json!(2467), |row| row["owner_stopped"] = json!(true))
        .unwrap();
    let stopped = f.get(2467);
    assert_eq!(
        registration_resume::run(&mut f.e, 2467, &request, preview["state_sha256"].as_str())
            .unwrap_err()
            .0,
        "InputChanged"
    );
    assert_eq!(f.get(2467), stopped);
}

#[test]
fn registration_resume_reports_a_race_inside_the_apply_call_without_false_success() {
    let (mut f, request) = setup();
    let preview = registration_resume::run(&mut f.e, 2467, &request, None).unwrap();
    let mut changed = f.e.todo().unwrap();
    changed["items"][0]["owner_stopped"] = json!(true);
    let stopped = changed["items"][0].clone();
    f.e.backend.repair_edit_after = 0;
    f.e.backend.repair_edit = Some((f.e.todo_path.clone(), changed));
    let error =
        registration_resume::run(&mut f.e, 2467, &request, preview["state_sha256"].as_str())
            .unwrap_err();
    assert_eq!(error.0, "InputChanged");
    assert_eq!(f.get(2467), stopped);
    assert_ne!(f.get(2467)["dispatchable"], true);
}

#[test]
fn registration_resume_checks_the_saved_workspace_without_changing_it() {
    let (mut f, request) = setup();
    f.e.edit(&json!(2467), |row| {
        row["scheduler"]["worktree"] = json!("unqualified-original")
    })
    .unwrap();
    f.e.backend.repair_denied = Some("unqualified-original".into());
    let before = f.get(2467);
    let preview = registration_resume::run(&mut f.e, 2467, &request, None).unwrap();
    assert_eq!(preview["checks"]["source_scope"], false);
    let result =
        registration_resume::run(&mut f.e, 2467, &request, preview["state_sha256"].as_str())
            .unwrap();
    assert_eq!(result["saved"], false);
    assert_eq!(f.get(2467), before);
}
