use serde_json::{Value, json};
use std::{fs, path::Path, process::Output};

fn cli(home: &Path, args: &[&str]) -> Output {
    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_todo"));
    child.args(args).env("CC_TODO_HOME", home).env("CC_TODO_JOBS", home.join("jobs"));
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        child.creation_flags(0x08000000);
    }
    child.output().unwrap()
}

fn success(output: Output) -> Value {
    assert!(output.status.success(), "{}", String::from_utf8_lossy(&output.stderr));
    serde_json::from_slice(&output.stdout).unwrap()
}

fn fixture(home: &Path) -> Value {
    let at = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_secs();
    let db = json!({"projects":{"demo":"C:/repo"},"items":[
        {"id":1,"project":"demo","status":"wait","spec":"original.md","note":"元の仕事と未完了を保持",
         "jobs":["original"],"resume_generation":8,"worker_receipt":{"job":"original","generation":8},
         "wait_on":[2],"progress":65,"unknown":{"keep":true},
         "wait_ready":{"condition":"retry-time","since":at-600,"not_before":at+7200,"reason":"旧レビューと同じ再確認時刻"},
         "scheduler":{"job":"original","key":"generation-8","worktree":"C:/repo-wt/original","not_before":0},
         "stall_recovery":{"history":[1],"next_at":0}},
        {"id":2,"status":"done","note":"代替レビューは完了。製品作業は元担当が継続"}],"next_id":3});
    fs::write(home.join("todo.json"), serde_json::to_vec(&db).unwrap()).unwrap();
    db
}

fn preview(home: &Path) -> Value {
    success(cli(home, &["retry-sooner", "1", "--after", "60", "--reason", "代替レビュー完了を確認した"]))
}

fn apply(home: &Path, hash: &str) -> Output {
    cli(home, &["retry-sooner", "1", "--after", "60", "--reason", "代替レビュー完了を確認した", "--apply", "--expected-state-sha256", hash])
}

#[test]
fn explicit_changed_condition_shortens_without_accepting_recovery_or_changing_work() {
    let folder = tempfile::tempdir().unwrap();
    let home = folder.path();
    let before = fixture(home);
    let bytes = fs::read(home.join("todo.json")).unwrap();
    let checked = preview(home);
    assert_eq!(checked["changed"], false);
    assert_eq!(fs::read(home.join("todo.json")).unwrap(), bytes);
    assert!(!home.join("todo.json.bak-20260930d").exists());
    let saved = success(apply(home, checked["state_sha256"].as_str().unwrap()));
    assert_eq!(saved["changed"], true);
    assert_eq!(saved["recheck_only"], true);
    let after = cc_todo::read_store(&home.join("todo.json")).unwrap();
    assert!(after["items"][0]["wait_ready"]["not_before"].as_u64().unwrap() < before["items"][0]["wait_ready"]["not_before"].as_u64().unwrap());
    assert_eq!(after["items"][0]["wait_ready"]["reason"], before["items"][0]["wait_ready"]["reason"]);
    assert_eq!(after["items"][0]["wait_ready"]["recheck"]["reason"], "代替レビュー完了を確認した");
    let mut expected = before;
    expected["items"][0]["wait_ready"] = after["items"][0]["wait_ready"].clone();
    expected["items"][0]["updated"] = after["items"][0]["updated"].clone();
    assert_eq!(after, expected);
    // Registration does not consume the new deadline, accept a review, or launch work.
    let receipt = success(cli(home, &["resume-ready", "1", "--condition", "retry-time"]));
    assert_eq!(receipt["resumed"], false);
    assert_eq!(cc_todo::read_store(&home.join("todo.json")).unwrap(), after);
    let repeated = success(cli(home, &["wait-retry", "1", "--after", "86400", "--reason", "同じ不足を再確認"]));
    assert_eq!(repeated["wait_ready"], after["items"][0]["wait_ready"]);
    assert!(!apply(home, checked["state_sha256"].as_str().unwrap()).status.success());
}

#[test]
fn stale_snapshot_source_and_other_worker_generation_cannot_change_the_wait() {
    for change in ["note", "spec", "generation", "worker", "wait", "source", "project"] {
        let folder = tempfile::tempdir().unwrap();
        let home = folder.path();
        let mut db = fixture(home);
        let receipt = preview(home);
        match change {
            "note" => db["items"][0]["note"] = json!("別担当が追記"),
            "spec" => db["items"][0]["spec"] = json!("new-source.md"),
            "generation" => db["items"][0]["resume_generation"] = json!(9),
            "worker" => db["items"][0]["scheduler"]["job"] = json!("other"),
            "wait" => db["items"][0]["wait_ready"]["reason"] = json!("新しい待機"),
            "project" => db["projects"]["demo"] = json!("C:/other"),
            "source" => (),
            _ => unreachable!(),
        }
        let other = tempfile::tempdir().unwrap();
        let target = if change == "source" { other.path() } else { home };
        fs::write(target.join("todo.json"), serde_json::to_vec(&db).unwrap()).unwrap();
        let bytes = fs::read(target.join("todo.json")).unwrap();
        assert!(!apply(target, receipt["state_sha256"].as_str().unwrap()).status.success(), "{change}");
        assert_eq!(fs::read(target.join("todo.json")).unwrap(), bytes, "{change}");
    }
}
