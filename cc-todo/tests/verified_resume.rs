use cc_todo::{Command, Paths, execute};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::fs;

fn digest(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
struct Fixture {
    _temp: tempfile::TempDir,
    paths: Paths,
    db: Value,
    command: Command,
}
fn fixture() -> Fixture {
    let temp = tempfile::tempdir().unwrap();
    let jobs = temp.path().join("jobs");
    let dir = jobs.join("source");
    fs::create_dir_all(&dir).unwrap();
    let request = temp.path().join("request.md");
    fs::write(
        &request,
        b"Continue the existing implementation and its tests.",
    )
    .unwrap();
    let request_hash = digest(&fs::read(&request).unwrap());
    let raw = serde_json::to_vec(&json!({"id":"source","todo_id":1,"status":"succeeded",
        "cwd":temp.path(),"thread_id":"00000000-0000-4000-8000-000000000001"}))
    .unwrap();
    fs::write(dir.join("job.json"), &raw).unwrap();
    fs::create_dir_all(jobs.join("current")).unwrap();
    fs::write(
        jobs.join("current/job.json"),
        serde_json::to_vec(&json!({"id":"current",
        "todo_id":1,"status":"running","cwd":temp.path(),
        "resume_session":"00000000-0000-4000-8000-000000000001"}))
        .unwrap(),
    )
    .unwrap();
    let command = Command::ResumeVerified {
        id: 1,
        expected_status: "todo".into(),
        request_sha256: request_hash.clone(),
        source_job_sha256: digest(&raw),
        job: "current".into(),
    };
    let db = json!({"items":[{"id":1,"status":"recovery_unverified","jobs":["source"],
        "spec":"missing-original.md","note":"retain original note","progress":25,
        "scheduler":{"branch":"original-branch","worktree":temp.path()},
        "recovery":{"original_id":1,"verified_status":"todo","preserved_worker_request":request,
        "preserved_worker_request_sha256":request_hash,"request_source_job_id":"source",
        "source_job_sha256":digest(&raw)}},
        {"id":2,"status":"wait","wait_on":[3],"note":"unrelated ready wait"},
        {"id":3,"status":"done"}]});
    let paths = Paths::at(temp.path(), &jobs);
    Fixture {
        _temp: temp,
        paths,
        db,
        command,
    }
}
#[test]
fn matching_receipts_resume_one_item_and_keep_original_work() {
    let mut f = fixture();
    let before = f.db.clone();
    let output = execute(&mut f.db, &f.command, &f.paths).unwrap();
    assert_eq!(output, vec![r#"{"id":1,"matching":true}"#]);
    assert_eq!(f.db["items"][0]["status"], "doing");
    for key in ["spec", "note", "progress", "recovery"] {
        assert_eq!(f.db["items"][0][key], before["items"][0][key]);
    }
    assert_eq!(f.db["items"][0]["scheduler"]["branch"], "original-branch");
    assert_eq!(f.db["items"][1], before["items"][1]);
    assert_eq!(f.db["items"][2], before["items"][2]);
    assert_eq!(
        f.db["items"][0]["recovery_resume_receipt"]["source_job_id"],
        "source"
    );
}
#[test]
fn legacy_start_cannot_skip_recovered_state_checks() {
    let mut f = fixture();
    let before = f.db.clone();
    assert!(
        execute(
            &mut f.db,
            &Command::Start {
                id: 1,
                job: "current".into(),
                progress: None
            },
            &f.paths
        )
        .is_err()
    );
    assert_eq!(f.db, before);
}
#[test]
fn changed_request_or_source_receipt_is_rejected_without_mutation() {
    for change_request in [true, false] {
        let mut f = fixture();
        let before = f.db.clone();
        let path = if change_request {
            std::path::PathBuf::from(
                f.db["items"][0]["recovery"]["preserved_worker_request"]
                    .as_str()
                    .unwrap(),
            )
        } else {
            f.paths.jobs.join("source/job.json")
        };
        fs::write(path, b"changed").unwrap();
        assert!(execute(&mut f.db, &f.command, &f.paths).is_err());
        assert_eq!(f.db, before);
    }
}
#[test]
fn completion_deferral_stops_and_pending_dependencies_are_kept() {
    for variant in 0..7 {
        let mut f = fixture();
        match variant {
            0 => f.db["items"][0]["status"] = json!("done"),
            1 => f.db["items"][0]["recovery"]["verified_status"] = json!("block"),
            2 => f.db["items"][0]["decision_wait"] = json!({"kind":"deferred"}),
            3 => f.db["items"][0]["stopped"] = json!(true),
            4 => f.db["items"][0]["wait_on"] = json!([2]),
            5 => f.db["items"][0]["owner_postponement_recorded"] = json!(true),
            _ => {
                f.db["items"][0]["wait_ready"] =
                    json!({"condition":"retry-time","not_before":u64::MAX})
            }
        }
        let before = f.db.clone();
        assert!(execute(&mut f.db, &f.command, &f.paths).is_err());
        assert_eq!(f.db, before, "variant {variant}");
    }
}
#[test]
fn unrelated_or_running_source_is_rejected_even_with_matching_hash() {
    for running in [true, false] {
        let mut f = fixture();
        let path = f.paths.jobs.join("source/job.json");
        let mut job: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
        if running {
            job["status"] = json!("running")
        } else {
            job["todo_id"] = json!(99)
        };
        let raw = serde_json::to_vec(&job).unwrap();
        fs::write(&path, &raw).unwrap();
        if let Command::ResumeVerified {
            source_job_sha256, ..
        } = &mut f.command
        {
            *source_job_sha256 = digest(&raw);
        }
        f.db["items"][0]["recovery"]["source_job_sha256"] = json!(digest(&raw));
        let before = f.db.clone();
        assert!(execute(&mut f.db, &f.command, &f.paths).is_err());
        assert_eq!(f.db, before);
    }
}
#[test]
fn missing_or_changed_pinned_job_hash_cannot_be_replaced_by_callers_current_hash() {
    for pinned in [Value::Null, json!("0".repeat(64))] {
        let mut f = fixture();
        f.db["items"][0]["recovery"]["source_job_sha256"] = pinned;
        let before = f.db.clone();
        assert!(execute(&mut f.db, &f.command, &f.paths).is_err());
        assert_eq!(f.db, before);
    }
}
#[test]
fn changed_session_worktree_or_assignee_cannot_attach_a_different_worker() {
    for field in ["resume_session", "cwd", "todo_id", "status"] {
        let mut f = fixture();
        let path = f.paths.jobs.join("current/job.json");
        let mut job: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
        job[field] = json!("different");
        fs::write(&path, serde_json::to_vec(&job).unwrap()).unwrap();
        let before = f.db.clone();
        assert!(execute(&mut f.db, &f.command, &f.paths).is_err());
        assert_eq!(f.db, before);
    }
}
