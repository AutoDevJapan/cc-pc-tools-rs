use cc_scheduler::policy::Processes;
use cc_todo::{Command, Paths, execute, ready, recovery::plan};
use clap::Parser;
use serde_json::{Value, json};
use std::path::Path;

#[test]
fn actual_cli_releases_only_due_technical_waits() {
    let folder = tempfile::tempdir().unwrap();
    let paths = Paths::at(folder.path(), &folder.path().join("jobs"));
    let at = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs();
    for (status, until, dependency, expected) in [
        ("wait", at - 60, false, true),
        ("wait", at + 3600, false, false),
        ("block", at - 60, false, false),
        ("wait", at - 60, true, false),
    ] {
        let mut before = db();
        before["items"][0]["status"] = json!(status);
        before["items"][0]["wait_ready"] =
            json!({"condition":"retry-time","since":at-120,"not_before":until});
        if dependency {
            before["items"][0]["wait_on"] = json!([2]);
            before["items"]
                .as_array_mut()
                .unwrap()
                .push(json!({"id":2,"status":"todo"}));
        }
        std::fs::write(&paths.store, serde_json::to_vec(&before).unwrap()).unwrap();
        let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_todo"));
        child
            .args(["resume-ready", "1", "--condition", "retry-time"])
            .env("CC_TODO_HOME", folder.path())
            .env("CC_TODO_JOBS", folder.path().join("jobs"));
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            child.creation_flags(0x08000000);
        }
        let output = child.output().unwrap();
        assert!(
            output.status.success(),
            "CLI rejected the technical condition: {}",
            String::from_utf8_lossy(&output.stderr)
        );
        let receipt: Value = serde_json::from_slice(&output.stdout).unwrap();
        assert_eq!(receipt["id"], 1);
        assert_eq!(receipt["resumed"], expected);
        let after = cc_todo::read_store(&paths.store).unwrap();
        if expected {
            assert_eq!(after["items"][0]["status"], "todo");
            assert_eq!(after["items"][0]["jobs"], before["items"][0]["jobs"]);
            assert_eq!(
                after["items"][0]["scheduler"]["worktree"],
                before["items"][0]["scheduler"]["worktree"]
            );
            assert!(after["items"][0]["wait_ready"].is_null());
        } else {
            assert_eq!(after, before);
        }
    }
    assert!(
        cc_todo::Cli::try_parse_from(["todo", "resume-ready", "1", "--condition", "unknown"])
            .is_err()
    );
}

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

#[test]
fn timed_retry_suppresses_repeated_launches_and_wakes_original_task_once() {
    let mut d = db();
    ready::register_retry(&mut d, 1, 3600, "画面操作の接続用パイプが見つからない", 100).unwrap();
    let wait = d["items"][0]["wait_ready"].clone();
    ready::register_retry(&mut d, 1, 3600, "同じ不足を再確認", 1000).unwrap();
    assert_eq!(d["items"][0]["wait_ready"], wait);
    assert!(
        plan(&mut d, &[], &Processes::new(), 3699., "")
            .unwrap()
            .is_empty()
    );
    let mut future = db();
    let actual_now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs();
    ready::register_retry(&mut future, 1, 3600, "接続不足", actual_now).unwrap();
    assert!(
        execute(
            &mut future,
            &Command::Start {
                id: 1,
                job: "new".into(),
                progress: None
            },
            &paths()
        )
        .is_err()
    );
    assert!(!ready::confirmed(&wait, &state(3700), 3699));
    // Reaching the deadline makes a new check due even with no external readiness file.
    assert!(ready::confirmed(&wait, &Value::Null, 3700));
    assert_eq!(ready::release_retries(&mut d, 3700).unwrap(), vec![1]);
    assert!(ready::release_retries(&mut d, 3700).unwrap().is_empty());
    assert_eq!(d["items"][0]["status"], "todo");
    assert_eq!(d["items"][0]["spec"], "saved.md");
    assert_eq!(d["items"][0]["note"], "既存メモ");
    assert_eq!(d["items"][0]["jobs"], json!(["old"]));
    assert_eq!(d["items"][0]["progress"], 60);
    assert_eq!(d["items"][0]["scheduler"]["worktree"], "C:/repo-wt/task");
    assert!(d["items"][0]["wait_ready"].is_null());
    assert_eq!(
        plan(&mut d, &[], &Processes::new(), 3700., "").unwrap(),
        vec![1]
    );
}

#[test]
fn retry_timer_keeps_owner_stop_dependencies_and_plus_wait() {
    let mut d = db();
    d["items"][0]["status"] = json!("block");
    ready::register_retry(&mut d, 1, 3600, "接続不足", 100).unwrap();
    assert_eq!(d["items"][0]["status"], "block");
    assert!(ready::release_retries(&mut d, 3700).unwrap().is_empty());
    d["items"][0]["status"] = json!("wait");
    d["items"][0]["wait_on"] = json!([2]);
    d["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"status":"todo"}));
    assert!(ready::release_retries(&mut d, 3700).unwrap().is_empty());
    d["items"][1]["status"] = json!("done");
    assert!(cc_job_policy::todo_wait::release_ready(&mut d).is_empty());
    assert_eq!(ready::release_retries(&mut d, 3700).unwrap(), vec![1]);
    d["items"][0]["status"] = json!("done");
    let before = d.clone();
    assert!(ready::register_retry(&mut d, 1, 3600, "接続不足", 100).is_err());
    assert_eq!(d, before);
    d["items"][0]["status"] = json!("wait");
    d["items"][0]["wait_ready"] = json!({"condition":"chatgpt-plus","since":100});
    let before = d.clone();
    assert!(ready::register_retry(&mut d, 1, 3600, "接続不足", 100).is_err());
    assert!(ready::release_retries(&mut d, u64::MAX).unwrap().is_empty());
    assert_eq!(d, before);
}

#[test]
fn invalid_retry_markers_and_cli_arguments_never_release_work() {
    for wait in [
        json!({"condition":"retry-time","since":100}),
        json!({"condition":"retry-time","since":100,"not_before":99}),
        json!({"condition":"retry-time","since":100,"not_before":100}),
        json!({"condition":"retry-time","since":100,"not_before":86501}),
        json!({"condition":"retry-time","since":-1,"not_before":3700}),
    ] {
        assert!(!ready::confirmed(&wait, &Value::Null, u64::MAX));
    }
    for (seconds, reason, at) in [
        (59, "接続不足", 100),
        (86401, "接続不足", 100),
        (3600, " ", 100),
        (3600, "接続不足", u64::MAX),
    ] {
        let mut d = db();
        let before = d.clone();
        assert!(ready::register_retry(&mut d, 1, seconds, reason, at).is_err());
        assert_eq!(d, before);
    }
    assert!(
        cc_todo::Cli::try_parse_from(["todo", "wait-retry", "1", "--reason", "接続不足"]).is_ok()
    );
    assert!(
        cc_todo::Cli::try_parse_from([
            "todo",
            "wait-retry",
            "1",
            "--after",
            "59",
            "--reason",
            "接続不足"
        ])
        .is_err()
    );
    assert!(cc_todo::Cli::try_parse_from(["todo", "wait-retry", "1"]).is_err());
}
