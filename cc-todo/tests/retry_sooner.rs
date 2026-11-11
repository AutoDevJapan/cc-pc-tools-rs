use serde_json::{Value, json};
use std::{fs, path::Path, process::Output};

fn cli(home: &Path, args: &[&str]) -> Output {
    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_todo"));
    child
        .args(args)
        .env("CC_TODO_HOME", home)
        .env("CC_TODO_JOBS", home.join("jobs"));
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        child.creation_flags(0x08000000);
    }
    child.output().unwrap()
}

fn success(output: Output) -> Value {
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    serde_json::from_slice(&output.stdout).unwrap()
}

fn fixture(home: &Path) -> Value {
    let at = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs();
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
    success(cli(
        home,
        &[
            "retry-sooner",
            "1",
            "--after",
            "60",
            "--reason",
            "代替レビュー完了を確認した",
        ],
    ))
}

fn apply(home: &Path, hash: &str) -> Output {
    cli(
        home,
        &[
            "retry-sooner",
            "1",
            "--after",
            "60",
            "--reason",
            "代替レビュー完了を確認した",
            "--apply",
            "--expected-state-sha256",
            hash,
        ],
    )
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
    assert!(
        after["items"][0]["wait_ready"]["not_before"]
            .as_u64()
            .unwrap()
            < before["items"][0]["wait_ready"]["not_before"]
                .as_u64()
                .unwrap()
    );
    assert_eq!(
        after["items"][0]["wait_ready"]["reason"],
        before["items"][0]["wait_ready"]["reason"]
    );
    assert_eq!(
        after["items"][0]["wait_ready"]["recheck"]["reason"],
        "代替レビュー完了を確認した"
    );
    let mut expected = before;
    expected["items"][0]["wait_ready"] = after["items"][0]["wait_ready"].clone();
    expected["items"][0]["updated"] = after["items"][0]["updated"].clone();
    assert_eq!(after, expected);
    // Registration does not consume the new deadline, accept a review, or launch work.
    let receipt = success(cli(
        home,
        &["resume-ready", "1", "--condition", "retry-time"],
    ));
    assert_eq!(receipt["resumed"], false);
    assert_eq!(cc_todo::read_store(&home.join("todo.json")).unwrap(), after);
    let repeated = success(cli(
        home,
        &[
            "wait-retry",
            "1",
            "--after",
            "86400",
            "--reason",
            "同じ不足を再確認",
        ],
    ));
    assert_eq!(repeated["wait_ready"], after["items"][0]["wait_ready"]);
    assert!(
        !apply(home, checked["state_sha256"].as_str().unwrap())
            .status
            .success()
    );
}

#[test]
fn stale_snapshot_source_and_other_worker_generation_cannot_change_the_wait() {
    for change in [
        "note",
        "spec",
        "generation",
        "worker",
        "wait",
        "source",
        "project",
    ] {
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
        let target = if change == "source" {
            other.path()
        } else {
            home
        };
        fs::write(target.join("todo.json"), serde_json::to_vec(&db).unwrap()).unwrap();
        let bytes = fs::read(target.join("todo.json")).unwrap();
        assert!(
            !apply(target, receipt["state_sha256"].as_str().unwrap())
                .status
                .success(),
            "{change}"
        );
        assert_eq!(
            fs::read(target.join("todo.json")).unwrap(),
            bytes,
            "{change}"
        );
    }
}

#[test]
fn owner_holds_other_conditions_and_unknown_dependencies_survive_both_operations() {
    for gate in [
        "block",
        "done",
        "doing",
        "stop",
        "cancel",
        "pause",
        "postpone",
        "stop_kind",
        "decision",
        "question",
        "scheduler_question",
        "scheduler_stop",
        "external",
        "plus",
        "unfinished",
        "missing",
        "malformed",
        "self",
        "owner_dependency",
        "duplicate_dependency",
        "malformed_decision",
        "malformed_postponement",
        "unknown_external",
    ] {
        let folder = tempfile::tempdir().unwrap();
        let home = folder.path();
        let mut db = fixture(home);
        match gate {
            "block" | "done" | "doing" => db["items"][0]["status"] = json!(gate),
            "stop" => db["items"][0]["stopped"] = json!(true),
            "cancel" => db["items"][0]["cancel_requested"] = json!(true),
            "pause" => db["items"][0]["paused"] = json!(true),
            "postpone" => db["items"][0]["owner_postponement_recorded"] = json!(true),
            "stop_kind" => db["items"][0]["stop_kind"] = json!("owner"),
            "decision" => db["items"][0]["decision_wait"] = json!({"question":"unanswered"}),
            "question" => {
                db["items"][0]["owner_question_wait"] = json!({"question":"other-unanswered"})
            }
            "scheduler_question" => db["items"][0]["scheduler"]["question"] = json!("other"),
            "scheduler_stop" => db["items"][0]["scheduler"]["owner_stop_job"] = json!("original"),
            "external" => db["items"][0]["external_resume_condition"] = json!("independent"),
            "plus" => db["items"][0]["wait_ready"]["condition"] = json!("chatgpt-plus"),
            "unfinished" => db["items"][1]["status"] = json!("todo"),
            "missing" => db["items"][0]["wait_on"] = json!([99]),
            "malformed" => db["items"][0]["wait_on"] = json!(["2"]),
            "self" => db["items"][0]["wait_on"] = json!([1]),
            "owner_dependency" => db["items"][0]["work_owner_wait_on"] = json!(99),
            "duplicate_dependency" => db["items"]
                .as_array_mut()
                .unwrap()
                .push(json!({"id":2,"status":"done"})),
            "malformed_decision" => db["items"][0]["decision_wait"] = json!(false),
            "malformed_postponement" => db["items"][0]["owner_postponement_recorded"] = json!(""),
            "unknown_external" => db["items"][0]["external_resume_condition"] = json!(""),
            _ => unreachable!(),
        }
        fs::write(home.join("todo.json"), serde_json::to_vec(&db).unwrap()).unwrap();
        let bytes = fs::read(home.join("todo.json")).unwrap();
        let preview = cli(
            home,
            &["retry-sooner", "1", "--reason", "別条件は解除しない"],
        );
        assert!(!preview.status.success(), "{gate}");
        assert_eq!(fs::read(home.join("todo.json")).unwrap(), bytes, "{gate}");
        // A real deadline cannot erase a separate owner hold either.
        let until = db["items"][0]["wait_ready"]["not_before"].as_u64().unwrap();
        let before = db.clone();
        assert!(
            cc_todo::ready::release_retries(&mut db, until)
                .unwrap()
                .is_empty(),
            "{gate}"
        );
        assert_eq!(db, before, "{gate}");
    }
}

#[test]
fn shorter_timer_wakes_only_the_original_task_once_at_the_new_deadline() {
    let folder = tempfile::tempdir().unwrap();
    let home = folder.path();
    let mut db = fixture(home);
    let paths = cc_todo::Paths::at(home, &home.join("jobs"));
    let at = db["items"][0]["wait_ready"]["since"].as_u64().unwrap() + 600;
    let checked =
        cc_todo::ready::retry_sooner(&mut db, &paths, 1, 60, "代替レビュー完了", None, at).unwrap();
    cc_todo::ready::retry_sooner(
        &mut db,
        &paths,
        1,
        60,
        "代替レビュー完了",
        checked["state_sha256"].as_str(),
        at,
    )
    .unwrap();
    assert!(
        cc_todo::ready::release_retries(&mut db, at + 59)
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        cc_todo::ready::release_retries(&mut db, at + 60).unwrap(),
        vec![1]
    );
    assert!(
        cc_todo::ready::release_retries(&mut db, at + 60)
            .unwrap()
            .is_empty()
    );
    let row = &db["items"][0];
    assert_eq!(row["status"], "todo");
    assert_eq!(row["jobs"], json!(["original"]));
    assert_eq!(
        row["worker_receipt"],
        json!({"job":"original","generation":8})
    );
    assert_eq!(row["resume_generation"], 8);
    assert_eq!(row["note"], "元の仕事と未完了を保持");
    assert_eq!(row["spec"], "original.md");
    assert_eq!(row["dependency_ready"], true);
}

#[test]
fn later_deadlines_bad_inputs_and_unconfirmed_apply_never_write() {
    let folder = tempfile::tempdir().unwrap();
    let home = folder.path();
    fixture(home);
    let checked = preview(home);
    for extra in [
        vec!["--after", "86400"],
        vec!["--after", "59"],
        vec!["--reason", " "],
        vec!["--apply"],
        vec!["--expected-state-sha256", "wrong"],
    ] {
        let mut args = vec!["retry-sooner", "1", "--reason", "状態変化"];
        args.extend(extra);
        let bytes = fs::read(home.join("todo.json")).unwrap();
        assert!(!cli(home, &args).status.success(), "{args:?}");
        assert_eq!(fs::read(home.join("todo.json")).unwrap(), bytes);
    }
    let bytes = fs::read(home.join("todo.json")).unwrap();
    assert!(!apply(home, "wrong").status.success());
    assert_eq!(fs::read(home.join("todo.json")).unwrap(), bytes);
    assert_eq!(checked["changed"], false);
}

#[test]
fn concurrent_note_change_under_the_normal_lock_rejects_old_preview() {
    use std::process::Stdio;
    use std::time::Duration;
    let folder = tempfile::tempdir().unwrap();
    let home = folder.path();
    let mut db = fixture(home);
    let checked = preview(home);
    let lock = cc_todo::Lock::acquire(&home.join("todo.lock"), Duration::ZERO).unwrap();
    let mut command = std::process::Command::new(env!("CARGO_BIN_EXE_todo"));
    command
        .args([
            "retry-sooner",
            "1",
            "--reason",
            "代替レビュー完了",
            "--apply",
            "--expected-state-sha256",
            checked["state_sha256"].as_str().unwrap(),
        ])
        .env("CC_TODO_HOME", home)
        .env("CC_TODO_JOBS", home.join("jobs"))
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let child = command.spawn().unwrap();
    db["items"][0]["note"] = json!("別担当の追記を消さない");
    cc_todo::save(&home.join("todo.json"), &db).unwrap();
    let bytes = fs::read(home.join("todo.json")).unwrap();
    drop(lock);
    let result = child.wait_with_output().unwrap();
    assert!(!result.status.success());
    assert!(String::from_utf8_lossy(&result.stderr).contains("retry_snapshot_changed"));
    assert_eq!(fs::read(home.join("todo.json")).unwrap(), bytes);
}

#[cfg(windows)]
#[test]
fn failed_atomic_save_preserves_the_original_wait_and_work() {
    use std::os::windows::fs::OpenOptionsExt;
    let folder = tempfile::tempdir().unwrap();
    let home = folder.path();
    fixture(home);
    let checked = preview(home);
    let bytes = fs::read(home.join("todo.json")).unwrap();
    let pinned = fs::OpenOptions::new()
        .read(true)
        .share_mode(1)
        .open(home.join("todo.json"))
        .unwrap();
    let result = apply(home, checked["state_sha256"].as_str().unwrap());
    assert!(!result.status.success());
    assert!(String::from_utf8_lossy(&result.stderr).contains("todo_store_io"));
    assert!(result.stdout.is_empty());
    assert_eq!(fs::read(home.join("todo.json")).unwrap(), bytes);
    drop(pinned);
}
