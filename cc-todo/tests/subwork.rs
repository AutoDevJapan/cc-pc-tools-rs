#[path = "../../cc-job-policy/tests/support/mod.rs"]
mod support;
use serde_json::{Value, json};
use std::os::windows::process::CommandExt;
use std::{
    fs,
    process::{Command, Output},
};
use support::Fixture;

fn run(f: &Fixture, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_todo"))
        .args(args)
        .env("CC_TODO_HOME", f.dir.path())
        .env("CC_TODO_JOBS", f.dir.path().join("jobs"))
        .creation_flags(0x08000000)
        .output()
        .unwrap()
}
fn save(f: &Fixture) {
    fs::write(
        f.dir.path().join("todo.json"),
        serde_json::to_vec(&f.db).unwrap(),
    )
    .unwrap();
    fs::create_dir_all(f.dir.path().join("jobs/parent")).unwrap();
    fs::write(
        f.dir.path().join("jobs/parent/job.json"),
        serde_json::to_vec(&f.jobs[0]).unwrap(),
    )
    .unwrap();
}
fn read(f: &Fixture) -> Value {
    serde_json::from_slice(&fs::read(f.dir.path().join("todo.json")).unwrap()).unwrap()
}
#[test]
fn cli_registers_subwork_at_add_with_real_parent_identity() {
    let mut f = Fixture::new();
    f.jobs[0]["pid"] = json!(std::process::id());
    f.jobs[0]["pid_created"] =
        json!(cc_scheduler::native::process_birth(std::process::id()).unwrap());
    save(&f);
    let output = run(
        &f,
        &[
            "add",
            "alias",
            "PR86 review",
            "--spec",
            f.db["items"][1]["spec"].as_str().unwrap(),
            "--subwork-of",
            "1",
            "--parent-job-id",
            "parent",
            "--subwork-kind",
            "review",
        ],
    );
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    let db = read(&f);
    assert_eq!(db["items"][2]["subwork"]["parent_todo_id"], 1);
    assert_eq!(db["items"][2]["subwork"]["kind"], "review");
    assert_eq!(db["items"][2]["status"], "todo");
    assert!(db["items"][2]["wait_on"].is_null());
}
#[test]
fn repair_removes_only_recorded_owner_dependency_and_preserves_stops() {
    for (status, waits, expected) in [
        ("wait", vec![1], "todo"),
        ("wait", vec![1, 3], "wait"),
        ("block", vec![1], "block"),
    ] {
        let mut f = Fixture::new();
        f.jobs[0]["status"] = json!("succeeded");
        f.db["items"]
            .as_array_mut()
            .unwrap()
            .push(json!({"id":3,"status":"todo"}));
        f.db["items"][1]["status"] = json!(status);
        f.db["items"][1]["wait_on"] = json!(waits);
        f.db["items"][1]["work_owner_job"] = json!("parent");
        f.db["items"][1]["progress"] = json!(25);
        let original = f.db["items"][1].clone();
        save(&f);
        let output = run(
            &f,
            &[
                "subwork",
                "2",
                "--subwork-of",
                "1",
                "--parent-job-id",
                "parent",
                "--subwork-kind",
                "check",
                "--repair-owner-wait",
            ],
        );
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let db = read(&f);
        let child = &db["items"][1];
        assert_eq!(child["status"], expected);
        for key in ["note", "spec", "jobs", "progress", "needs"] {
            assert_eq!(child[key], original[key]);
        }
        assert_eq!(
            child["scheduler"]["worktree"],
            original["scheduler"]["worktree"]
        );
        if waits.len() == 2 {
            assert_eq!(child["wait_on"], json!([3]));
        } else {
            assert!(child["wait_on"].is_null());
        }
        assert_eq!(db["items"][0], f.db["items"][0]);
    }
}
#[test]
fn invalid_parent_or_unproven_repair_never_changes_store() {
    let f = Fixture::new();
    save(&f);
    let before = fs::read(f.dir.path().join("todo.json")).unwrap();
    for extra in [vec![], vec!["--repair-owner-wait"]] {
        let mut args = vec![
            "subwork",
            "2",
            "--subwork-of",
            "1",
            "--parent-job-id",
            "parent",
            "--subwork-kind",
            "check",
        ];
        args.extend(extra);
        assert!(!run(&f, &args).status.success());
        assert_eq!(fs::read(f.dir.path().join("todo.json")).unwrap(), before);
    }
}
