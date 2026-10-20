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
fn later_cycles_keep_bounded_history_and_do_not_require_archived_job_files() {
    let mut f = Fixture::new();
    let first = f.ingest(AT);
    let id = "20261020-200000-todo-1";
    fs::create_dir_all(f.paths.jobs.join(id)).unwrap();
    fs::write(
        f.paths.jobs.join(id).join("job.json"),
        json!({"status":"succeeded"}).to_string(),
    )
    .unwrap();
    f.db["items"][0]["jobs"] = json!([id]);
    for cycle in 1..=18 {
        f.db["items"][0]["status"] = json!("done");
        f.write(AT + cycle * 10800);
        assert_eq!(f.ingest(AT + cycle * 10800)["action"], "reused_completed");
        if cycle == 1 {
            fs::remove_file(f.paths.jobs.join(id).join("job.json")).unwrap();
        }
    }
    assert_eq!(f.db["items"].as_array().unwrap().len(), 1);
    assert_eq!(f.db["items"][0]["jobs"], json!([]));
    let first_archive = f
        .paths
        .store
        .parent()
        .unwrap()
        .join("capacity-decisions")
        .join(format!("{}.json", first["generation"].as_str().unwrap()));
    let first_archive: Value = serde_json::from_slice(&fs::read(first_archive).unwrap()).unwrap();
    assert_eq!(first_archive["jobs"], json!([id]));
    assert_eq!(
        f.db["items"][0]["capacity_decision"]["history"]
            .as_array()
            .unwrap()
            .len(),
        16
    );
    assert_eq!(
        fs::read_dir(f.paths.store.parent().unwrap().join("capacity-decisions"))
            .unwrap()
            .count(),
        18
    );
}

#[test]
fn explicit_stop_flags_on_completed_row_or_job_survive_next_cycle() {
    for field in ["stopped", "cancel_requested", "owner_stopped"] {
        let mut f = Fixture::new();
        f.ingest(AT);
        f.db["items"][0]["status"] = json!("done");
        f.db["items"][0][field] = json!(true);
        f.write(AT + 10800);
        assert_eq!(
            f.ingest(AT + 10800)["action"],
            "retained_unfinished_or_same_generation"
        );
        assert_eq!(f.db["items"][0]["status"], "done");
        assert_eq!(f.db["items"][0][field], true);
    }
    for flag in [
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
        json!({"owner_stopped":true}),
        json!({"cancel_reason":"manual"}),
    ] {
        let mut f = Fixture::new();
        f.ingest(AT);
        let id = "20261020-200000-todo-1";
        fs::create_dir_all(f.paths.jobs.join(id)).unwrap();
        let mut job = json!({"status":"succeeded"});
        for (key, value) in flag.as_object().unwrap() {
            job[key] = value.clone();
        }
        fs::write(f.paths.jobs.join(id).join("job.json"), job.to_string()).unwrap();
        f.db["items"][0]["jobs"] = json!([id]);
        f.db["items"][0]["status"] = json!("done");
        f.write(AT + 10800);
        assert_eq!(
            f.ingest(AT + 10800)["action"],
            "retained_unfinished_or_same_generation"
        );
    }
}

#[test]
fn anonymous_projection_keeps_actual_scan_counts_and_volume_changes() {
    let input = json!({"schema":1,"time":chrono::DateTime::from_timestamp(AT as i64,0).unwrap().to_rfc3339(),"whole_drive_scan":false,"bytes_reclaimed":0,
        "categories":[{"category":"windows","status":"partial","logical_bytes":12,"files":3,"directories":2,"errors":1,"scan_started":"2026-10-19T18:06:47Z","private_name":"MUST_NOT_ENTER_SPEC"}],
        "free_bytes":{"C":123,"D":456,"E":null,"private_name":"MUST_NOT_ENTER_SPEC"},"volume_used_change_bytes":{"C":-8,"D":4},"known_root_file_bytes":{"pagefile":9,"private_name":"MUST_NOT_ENTER_SPEC"}});
    let got = capacity_cycle::measurement(&input, AT).unwrap();
    assert_eq!(got["categories"][0]["files"], 3);
    assert_eq!(got["categories"][0]["scan_started"], "2026-10-19T18:06:47Z");
    assert_eq!(got["free_bytes"]["C"], 123);
    assert!(got["free_bytes"]["E"].is_null());
    assert_eq!(got["volume_used_change_bytes"]["C"], -8);
    assert!(!got.to_string().contains("MUST_NOT_ENTER_SPEC"));
}

#[test]
fn reused_row_waits_for_scheduler_without_watchdog_resuming_old_conversation() {
    let mut f = Fixture::new();
    f.ingest(AT);
    let id = "20261020-200000-todo-1";
    fs::create_dir_all(f.paths.jobs.join(id)).unwrap();
    let job = json!({"id":id,"todo_id":1,"project":"fixture","status":"succeeded","created_at":AT-100,"finished_at":AT,"thread_id":"old-conversation","cwd":"fixture"});
    fs::write(f.paths.jobs.join(id).join("job.json"), job.to_string()).unwrap();
    f.db["items"][0]["jobs"] = json!([id]);
    f.db["items"][0]["status"] = json!("done");
    f.write(AT + 10800);
    f.ingest(AT + 10800);
    let mut ordinary = f.db.clone();
    ordinary["items"][0]
        .as_object_mut()
        .unwrap()
        .remove("capacity_decision");
    let ps = cc_scheduler::policy::Processes::new();
    assert_eq!(
        cc_todo::recovery::plan(
            &mut ordinary,
            std::slice::from_ref(&job),
            &ps,
            (AT + 10860) as f64,
            ""
        )
        .unwrap(),
        vec![1],
        "positive control must exercise the ordinary direct-recovery route"
    );
    let selected =
        cc_todo::recovery::plan(&mut f.db, &[job], &ps, (AT + 10860) as f64, "").unwrap();
    assert!(
        selected.is_empty(),
        "watchdog reserved an old-conversation restart instead of waiting for the normal scheduler"
    );
    assert!(f.db["items"][0]["stall_recovery"]["pending"].is_null());
    assert_eq!(f.db["items"][0]["status"], "todo");
    assert_eq!(f.db["items"][0]["dispatchable"], true);
}

#[test]
fn completed_generation_with_finished_failures_can_advance_without_carrying_dispatch() {
    for status in ["failed", "lost", "cancelled"] {
        let mut f = Fixture::new();
        let first = f.ingest(AT);
        let id = "20261020-200000-todo-1";
        fs::create_dir_all(f.paths.jobs.join(id)).unwrap();
        let job = json!({"id":id,"todo_id":1,"status":status,"cwd":"fixture","thread_id":"01a0fa4b-2898-7553-889f-0e934f0d32f8","recovery_cancel":{"key":"automatic","committed_at":"2026-10-20T09:00:00Z"}});
        fs::write(f.paths.jobs.join(id).join("job.json"), job.to_string()).unwrap();
        f.db["items"][0]["jobs"] = json!([id]);
        f.db["items"][0]["status"] = json!("done");
        f.db["items"][0]["scheduler"] = json!({"last_job":id,"resume":true,"continuation_count":3,"worktree":"fixture","branch":"scheduler/fixture","owner_instruction":"previous cycle instruction","workspace_receipt":{"job":id}});
        f.db["items"][0]["stall_recovery"] = json!({"history":[1,2],"next_at":99999999999_u64});
        f.write(AT + 10800);
        assert_eq!(f.ingest(AT + 10800)["action"], "reused_completed");
        let row = &f.db["items"][0];
        assert_eq!(row["jobs"], json!([]));
        assert!(row["dependency_ready"].is_null());
        assert!(row["scheduler"]["resume"].is_null());
        assert!(row["scheduler"]["last_job"].is_null());
        assert!(row["scheduler"]["owner_instruction"].is_null());
        assert!(row["stall_recovery"].is_null());
        assert_eq!(row["scheduler"]["worktree"], "fixture");
        assert!(
            cc_scheduler::preparation::workspace_source(row, std::slice::from_ref(&job)).is_none()
        );
        let archive = f
            .paths
            .store
            .parent()
            .unwrap()
            .join("capacity-decisions")
            .join(format!("{}.json", first["generation"].as_str().unwrap()));
        let archived: Value = serde_json::from_slice(&fs::read(archive).unwrap()).unwrap();
        assert_eq!(archived["jobs"], json!([id]));
        assert_eq!(
            archived["dispatch"]["owner_instruction"],
            "previous cycle instruction"
        );
    }
}

#[test]
fn result_keeps_unknowns_and_rejects_false_full_scan_and_private_labels() {
    let mut f = Fixture::new();
    let receipt = f.ingest(AT);
    assert!(capacity_cycle::validate_completion(&f.db["items"][0]).is_err());
    let state = capacity_cycle::status(&f.db).unwrap();
    assert_eq!(state["todo_id"], 1);
    assert!(state["result"].is_null());
    assert_eq!(
        state["active_measurement"]["categories"][0]["status"],
        "protected_unclassified"
    );
    let path = f.paths.store.parent().unwrap().join("result.json");
    let mut result = json!({"schema":1,"generation":receipt["generation"],"uninvestigated_categories":2,"investigated_resources":0,"candidates":0,"retained":0,"reclaimed_bytes":null,"archive_verified":null,"questions_created":0,"questions_sent":0,"answers_applied":null,"retention_reasons":{"protected_unclassified":1}});
    fs::write(&path, result.to_string()).unwrap();
    capacity_cycle::record_result(&mut f.db, 1, &path).unwrap();
    assert!(capacity_cycle::validate_completion(&f.db["items"][0]).is_ok());
    assert!(f.db["items"][0]["capacity_decision"]["result"]["reclaimed_bytes"].is_null());
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
