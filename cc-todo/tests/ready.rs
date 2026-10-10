use cc_scheduler::policy::Processes;
use cc_todo::{Command, Paths, execute, ready, recovery::plan};
use clap::Parser;
use serde_json::{Value, json};
use std::path::Path;

fn db() -> Value {
    json!({"projects":{"demo":"C:/repo"},"items":[
        {"id":1,"project":"demo","title":"Plusで実送受信する","status":"doing","jobs":["old"],"note":"既存メモ","progress":60,"spec":"saved.md","dispatchable":true,
         "scheduler":{"job":"old","worktree":"C:/repo-wt/task","not_before":999999},
         "stall_recovery":{"history":[1,2],"next_at":999999,"pending":{"request":"stale"}}}
    ],"next_id":2})
}
fn state(checked: u64) -> Value {
    json!({"backend":"owner_chrome","login":"logged_in","checked_at":checked})
}
fn paths() -> Paths {
    Paths::at(Path::new("unused"), Path::new("unused/jobs"))
}

#[test]
fn pro_old_fixture_missing_and_future_checks_cannot_release_a_task() {
    let wait = json!({"condition":"chatgpt-plus","since":100});
    assert!(ready::confirmed(&wait, &state(100), 100));
    assert!(ready::confirmed(&wait, &state(100), 100 + 26 * 3600));
    for st in [
        Value::Null,
        state(99),
        state(201),
        state(0),
        json!({"backend":"owner_chrome","login":"pending_plus","checked_at":100}),
        json!({"backend":"fixture","login":"logged_in","checked_at":100}),
        json!({"backend":"dedicated_chrome","login":"logged_in","checked_at":100}),
    ] {
        let mut d = db();
        d["items"][0]["status"] = json!("wait");
        d["items"][0]["wait_ready"] = wait.clone();
        let before = d.clone();
        assert!(!ready::resume(&mut d, 1, "chatgpt-plus", &st, 200).unwrap());
        assert_eq!(d, before);
    }
    assert!(!ready::confirmed(&wait, &state(100), 101 + 26 * 3600));
}

#[test]
fn registration_parks_watchdog_and_release_preserves_the_original_work() {
    let mut d = db();
    execute(
        &mut d,
        &Command::WaitReady {
            id: 1,
            condition: "chatgpt-plus".into(),
            progress: None,
        },
        &paths(),
    )
    .unwrap();
    let registered = d["items"][0]["wait_ready"].clone();
    execute(
        &mut d,
        &Command::WaitReady {
            id: 1,
            condition: "chatgpt-plus".into(),
            progress: None,
        },
        &paths(),
    )
    .unwrap();
    assert_eq!(d["items"][0]["wait_ready"], registered);
    assert_eq!(d["items"][0]["status"], "wait");
    assert!(d["items"][0]["stall_recovery"]["pending"].is_null());
    assert!(
        plan(&mut d, &[], &Processes::new(), 999999., "")
            .unwrap()
            .is_empty()
    );
    assert!(
        execute(
            &mut d,
            &Command::Start {
                id: 1,
                job: "new".into(),
                progress: None
            },
            &paths()
        )
        .is_err()
    );
    let checked = registered["since"].as_u64().unwrap();
    assert!(ready::resume(&mut d, 1, "chatgpt-plus", &state(checked), checked).unwrap());
    assert_eq!(d["items"][0]["status"], "todo");
    assert_eq!(d["items"][0]["scheduler"]["not_before"], 0);
    assert_eq!(d["items"][0]["stall_recovery"]["next_at"], 0);
    assert_eq!(d["items"][0]["scheduler"]["worktree"], "C:/repo-wt/task");
    assert_eq!(d["items"][0]["jobs"], json!(["old"]));
    assert_eq!(d["items"][0]["spec"], "saved.md");
    assert_eq!(d["items"][0]["note"], "既存メモ");
    assert_eq!(d["items"][0]["progress"], 60);
    assert!(!ready::resume(&mut d, 1, "chatgpt-plus", &state(checked), checked).unwrap());
    assert_eq!(
        plan(&mut d, &[], &Processes::new(), 999999., "").unwrap(),
        vec![1]
    );
}

#[test]
fn owner_stop_done_and_other_prerequisites_are_never_bypassed() {
    for status in ["block", "done", "doing", "todo"] {
        let mut d = db();
        d["items"][0]["status"] = json!(status);
        d["items"][0]["wait_ready"] = json!({"condition":"chatgpt-plus","since":100});
        let before = d.clone();
        assert!(!ready::resume(&mut d, 1, "chatgpt-plus", &state(100), 100).unwrap());
        assert_eq!(d, before);
    }
    let mut d = db();
    d["items"][0]["status"] = json!("block");
    ready::register(&mut d, 1, "chatgpt-plus").unwrap();
    assert_eq!(d["items"][0]["status"], "block");
    d["items"][0]["status"] = json!("done");
    let before = d.clone();
    assert!(ready::register(&mut d, 1, "chatgpt-plus").is_err());
    assert_eq!(d, before);

    d["items"][0]["status"] = json!("wait");
    d["items"][0]["wait_ready"] = json!({"condition":"chatgpt-plus","since":100});
    d["items"][0]["wait_on"] = json!([2]);
    d["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"status":"todo"}));
    assert!(!ready::resume(&mut d, 1, "chatgpt-plus", &state(100), 100).unwrap());
    d["items"][1]["status"] = json!("done");
    assert!(cc_job_policy::todo_wait::release_ready(&mut d).is_empty());
    cc_job_policy::todo_wait::release_item(&mut d["items"][0]);
    assert_eq!(d["items"][0]["status"], "wait");
    assert!(ready::resume(&mut d, 1, "chatgpt-plus", &state(100), 100).unwrap());
}

#[test]
fn malformed_state_is_an_error_and_unknown_conditions_are_rejected() {
    let temp = tempfile::tempdir().unwrap();
    assert!(
        ready::read_state(&temp.path().join("missing.json"))
            .unwrap()
            .is_null()
    );
    assert!(ready::read_state(temp.path()).is_err());
    let file = temp.path().join("invalid.json");
    std::fs::write(&file, b"{").unwrap();
    assert!(ready::read_state(&file).is_err());
    assert!(
        cc_todo::Cli::try_parse_from(["todo", "wait-ready", "1", "--condition", "unknown"])
            .is_err()
    );
    let mut d = db();
    let before = d.clone();
    assert!(ready::register(&mut d, 1, "unknown").is_err());
    assert_eq!(d, before);
}
