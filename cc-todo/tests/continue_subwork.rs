use clap::Parser;
#[path = "../../cc-job-policy/tests/support/mod.rs"]
mod support;
use serde_json::{Value, json};
use std::{fs, os::windows::process::CommandExt, process::Command};

fn fixture() -> support::Fixture {
    let mut f = support::Fixture::new();
    f.register();
    f.jobs[0]["pid"] = json!(std::process::id());
    f.jobs[0]["pid_created"] =
        json!(cc_scheduler::native::process_birth(std::process::id()).unwrap());
    let row = &mut f.db["items"][1];
    row["subwork"]["kind"] = json!("research");
    row["task_type"] = json!("research");
    row["sandbox"] = json!("read-only");
    row["model"] = json!("gpt-6.1-sol");
    row["model_origin"] = json!("policy");
    row["effort"] = json!("max");
    row["resume_generation"] = json!(3);
    row["dependency_generation"] = json!(1);
    row["scheduler"]["last_job"] = json!("old-quota");
    row["jobs"] = json!(["old-quota"]);
    let spec_sha =
        cc_job_policy::product_retry::digest(&fs::read(row["spec"].as_str().unwrap()).unwrap());
    let source = json!({"id":"old-quota","todo_id":2,"status":"failed","last_failure_kind":"quota","model":"gpt-6.1-sol","model_origin":"policy",
        "subwork":row["subwork"],"sandbox":"read-only","cwd":row["scheduler"]["worktree"],"task_type":"research",
        "launch_key":"old-key","todo_resume_generation":2,"todo_dependency_generation":0,
        "started_at":"2026-10-26T01:00:00Z","finished_at":"2026-10-26T01:01:00Z",
        "finite_subwork":{"todo_id":2,"spec":row["spec"],"spec_sha256":spec_sha,"launch_key":"old-key"}});
    f.jobs.push(source);
    f
}
fn save(f: &support::Fixture) {
    fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
    for job in &f.jobs {
        let dir = f.dir.path().join("jobs").join(job["id"].as_str().unwrap());
        fs::create_dir_all(&dir).unwrap();
        fs::write(dir.join("job.json"), job.to_string()).unwrap();
    }
}
fn call(f: &support::Fixture, actor: &str) -> std::process::Output {
    Command::new(env!("CARGO_BIN_EXE_todo"))
        .args([
            "continue-subwork",
            "2",
            "--source-job",
            "old-quota",
            "--parent-job-id",
            "parent",
            "--resume-generation",
            "3",
            "--dependency-generation",
            "1",
            "--spec-sha256",
            f.jobs[1]["finite_subwork"]["spec_sha256"].as_str().unwrap(),
        ])
        .env("CC_JOB_ID", actor)
        .env("CC_TODO_HOME", f.dir.path())
        .env("CC_TODO_JOBS", f.dir.path().join("jobs"))
        .creation_flags(0x08000000)
        .output()
        .unwrap()
}
#[test]
fn real_parent_accepts_only_the_new_current_request_and_preserves_every_old_record() {
    let f = fixture();
    save(&f);
    let before_source = fs::read(f.dir.path().join("jobs/old-quota/job.json")).unwrap();
    let result = call(&f, "parent");
    assert!(
        result.status.success(),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
    let db: Value =
        serde_json::from_slice(&fs::read(f.dir.path().join("todo.json")).unwrap()).unwrap();
    let mut row = db["items"][1].clone();
    assert!(
        row.as_object_mut()
            .unwrap()
            .remove("subwork_continuation")
            .unwrap()
            .is_object()
    );
    assert_eq!(row, f.db["items"][1]);
    assert_eq!(db["items"][0], f.db["items"][0]);
    assert_eq!(
        fs::read(f.dir.path().join("jobs/old-quota/job.json")).unwrap(),
        before_source
    );
    assert!(!cc_job_policy::quota_recovery::eligible_source(
        &db["items"][1],
        &f.jobs[1]
    ));
}
#[test]
fn forged_or_stale_continuation_is_rejected_without_modifying_the_store() {
    for case in [
        "actor",
        "source",
        "generation",
        "spec",
        "owner",
        "pending",
        "stop",
        "live",
        "cancel",
        "relation",
    ] {
        let mut f = fixture();
        match case {
            "source" => f.jobs[1]["todo_id"] = json!(999),
            "generation" => f.db["items"][1]["resume_generation"] = json!(4),
            "spec" => fs::write(f.db["items"][1]["spec"].as_str().unwrap(), "Changed").unwrap(),
            "owner" => f.jobs[0]["work_owner"]["worktree"] = json!("elsewhere"),
            "pending" => f.db["items"][1]["wait_on"] = json!([99]),
            "stop" => f.db["items"][1]["owner_stopped"] = json!(true),
            "live" => {
                f.jobs[1]["pid"] = f.jobs[0]["pid"].clone();
                f.jobs[1]["pid_created"] = f.jobs[0]["pid_created"].clone();
            }
            "cancel" => f.jobs[1]["status"] = json!("cancelled"),
            "relation" => f.jobs[1]["subwork"]["origin_job_id"] = json!("other"),
            "actor" => {}
            _ => unreachable!(),
        }
        save(&f);
        let before = fs::read(f.dir.path().join("todo.json")).unwrap();
        let out = call(&f, if case == "actor" { "other" } else { "parent" });
        assert!(!out.status.success(), "{case}");
        assert_eq!(
            fs::read(f.dir.path().join("todo.json")).unwrap(),
            before,
            "{case}"
        );
    }
}

#[test]
fn current_parent_can_request_a_new_contract_without_rewriting_the_old_source() {
    let parsed = cc_todo::Cli::try_parse_from([
        "todo",
        "continue-subwork",
        "3719",
        "--source-job",
        "20261026-180903-todo-3719",
        "--parent-job-id",
        "20261027-175645-todo-3080",
        "--resume-generation",
        "3",
        "--dependency-generation",
        "1",
        "--spec-sha256",
        "4f6fe32f62db56bed2acb4d378147ff952ad06230243fb45f7674f3158313fc3",
    ]);
    assert!(
        parsed.is_ok(),
        "no ordinary continuation intake: {parsed:?}"
    );
}
