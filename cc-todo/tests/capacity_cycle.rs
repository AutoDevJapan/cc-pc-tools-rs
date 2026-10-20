use cc_todo::{Paths, capacity_cycle};
use serde_json::{Value, json};
use std::{fs, path::PathBuf};

const AT: u64 = 1792488318;
struct Fixture {
    _dir: tempfile::TempDir,
    paths: Paths,
    report: PathBuf,
    spec: PathBuf,
    db: Value,
}
impl Fixture {
    fn new() -> Self {
        let dir = tempfile::tempdir().unwrap();
        let paths = Paths::at(dir.path(), &dir.path().join("jobs"));
        let report = dir.path().join("fresh.json");
        let spec = dir.path().join("finite.md");
        fs::write(&spec, "所有・終了を確認できる資源だけ有限に判定する。").unwrap();
        let f = Self {
            _dir: dir,
            paths,
            report,
            spec,
            db: json!({"projects":{"fixture":"fixture"},"items":[],"next_id":1}),
        };
        f.write(AT);
        f
    }
    fn write(&self, at: u64) {
        fs::write(&self.report,json!({"schema":1,"time":chrono::DateTime::from_timestamp(at as i64,0).unwrap().to_rfc3339(),"whole_drive_scan":false,"bytes_reclaimed":0,
            "categories":[{"category":"windows","status":"partial","logical_bytes":12},{"category":"owner_data","status":"protected_unclassified","logical_bytes":null}],"private_path":"MUST_NOT_ENTER_SPEC"}).to_string()).unwrap();
    }
    fn ingest(&mut self, at: u64) -> Value {
        capacity_cycle::ingest(
            &mut self.db,
            &self.paths,
            "fixture",
            &self.report,
            &self.spec,
            at,
        )
        .unwrap()
    }
}

#[test]
fn one_ordinary_row_retains_current_worker_and_advances_only_after_finished() {
    let mut f = Fixture::new();
    let first = f.ingest(AT);
    assert_eq!(first["action"], "queued");
    assert_eq!(first["worker_launched"], false);
    let spec = fs::read_to_string(f.db["items"][0]["spec"].as_str().unwrap()).unwrap();
    assert!(spec.contains("protected_unclassified"));
    assert!(!spec.contains("MUST_NOT_ENTER_SPEC"));
    assert_eq!(f.db["items"][0]["model"], "gpt-6.1-sol");
    assert_eq!(f.db["items"][0]["effort"], "max");
    f.ingest(AT);
    f.db["items"][0]["status"] = json!("doing");
    f.db["items"][0]["work_owner_job"] = json!("owner-job");
    let old_spec = f.db["items"][0]["spec"].clone();
    f.write(AT + 10800);
    let next = f.ingest(AT + 10800);
    assert_eq!(next["action"], "retained_unfinished_or_same_generation");
    assert_eq!(f.db["items"][0]["spec"], old_spec);
    assert_eq!(f.db["items"][0]["work_owner_job"], "owner-job");
    assert_ne!(
        f.db["items"][0]["capacity_decision"]["active_generation"],
        next["generation"]
    );
    f.db["items"][0]["status"] = json!("done");
    assert_eq!(f.ingest(AT + 10800)["action"], "reused_completed");
    assert_eq!(f.db["next_id"], 2);
    assert_eq!(f.db["items"].as_array().unwrap().len(), 1);
    assert_eq!(f.db["items"][0]["status"], "todo");
    assert!(f.db["items"][0]["work_owner_job"].is_null());
    assert_eq!(
        f.db["items"][0]["capacity_decision"]["history"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert_eq!(
        fs::read_dir(f.paths.store.parent().unwrap().join("capacity-decisions"))
            .unwrap()
            .count(),
        1
    );
}

#[test]
fn no_wait_or_running_job_is_released_by_new_measurement() {
    for status in ["block", "wait", "recovery_unverified", "todo", "doing"] {
        let mut f = Fixture::new();
        f.ingest(AT);
        f.db["items"][0]["status"] = json!(status);
        f.db["items"][0]["wait_on"] = json!([90]);
        f.db["items"][0]["conversation_wait"] = json!({"question_id":"kept"});
        let expected = f.db["items"][0].clone();
        f.write(AT + 10800);
        f.ingest(AT + 10800);
        for key in ["status", "spec", "wait_on", "conversation_wait"] {
            assert_eq!(f.db["items"][0][key], expected[key]);
        }
    }
    let mut f = Fixture::new();
    f.ingest(AT);
    let id = "20261020-200000-todo-1";
    fs::create_dir_all(f.paths.jobs.join(id)).unwrap();
    fs::write(
        f.paths.jobs.join(id).join("job.json"),
        json!({"status":"running"}).to_string(),
    )
    .unwrap();
    f.db["items"][0]["jobs"] = json!([id]);
    f.db["items"][0]["status"] = json!("done");
    f.write(AT + 10800);
    assert_eq!(
        f.ingest(AT + 10800)["action"],
        "retained_unfinished_or_same_generation"
    );
    fs::write(
        f.paths.jobs.join(id).join("job.json"),
        json!({"status":"succeeded"}).to_string(),
    )
    .unwrap();
    assert_eq!(f.ingest(AT + 10800)["action"], "reused_completed");
}

#[test]
fn invalid_stale_conflicting_or_duplicate_input_never_changes_queue() {
    let mut f = Fixture::new();
    f.ingest(AT);
    let before = f.db.clone();
    assert!(
        capacity_cycle::ingest(
            &mut f.db,
            &f.paths,
            "fixture",
            &f.report,
            &f.spec,
            AT + 14401
        )
        .is_err()
    );
    assert_eq!(f.db, before);
    f.write(AT + 10800);
    f.ingest(AT + 10800);
    let before = f.db.clone();
    f.write(AT);
    assert!(
        capacity_cycle::ingest(
            &mut f.db,
            &f.paths,
            "fixture",
            &f.report,
            &f.spec,
            AT + 10800
        )
        .is_err()
    );
    assert_eq!(f.db, before);
    f.write(AT + 10800);
    let mut value: Value = serde_json::from_slice(&fs::read(&f.report).unwrap()).unwrap();
    value["categories"][0]["logical_bytes"] = json!(13);
    fs::write(&f.report, value.to_string()).unwrap();
    assert!(
        capacity_cycle::ingest(
            &mut f.db,
            &f.paths,
            "fixture",
            &f.report,
            &f.spec,
            AT + 10800
        )
        .is_err()
    );
    assert_eq!(f.db, before);
    f.db["items"]
        .as_array_mut()
        .unwrap()
        .push(before["items"][0].clone());
    let before = f.db.clone();
    assert!(
        capacity_cycle::ingest(
            &mut f.db,
            &f.paths,
            "fixture",
            &f.report,
            &f.spec,
            AT + 10800
        )
        .is_err()
    );
    assert_eq!(f.db, before);
}

#[test]
fn result_keeps_unknowns_and_rejects_false_full_scan_and_private_labels() {
    let mut f = Fixture::new();
    let receipt = f.ingest(AT);
    let path = f.paths.store.parent().unwrap().join("result.json");
    let mut result = json!({"schema":1,"generation":receipt["generation"],"uninvestigated_categories":2,"investigated_resources":0,"candidates":0,"retained":0,"reclaimed_bytes":0,"archive_verified":null,"questions_created":0,"questions_sent":0,"answers_applied":null,"retention_reasons":{"protected_unclassified":1}});
    fs::write(&path, result.to_string()).unwrap();
    capacity_cycle::record_result(&mut f.db, 1, &path).unwrap();
    assert!(f.db["items"][0]["capacity_decision"]["result"]["archive_verified"].is_null());
    let before = f.db.clone();
    result["uninvestigated_categories"] = json!(0);
    fs::write(&path, result.to_string()).unwrap();
    assert!(capacity_cycle::record_result(&mut f.db, 1, &path).is_err());
    assert_eq!(f.db, before);
    result["uninvestigated_categories"] = json!(2);
    result["retention_reasons"] = json!({"private-name":1});
    fs::write(&path, result.to_string()).unwrap();
    assert!(capacity_cycle::record_result(&mut f.db, 1, &path).is_err());
    assert_eq!(f.db, before);
}
