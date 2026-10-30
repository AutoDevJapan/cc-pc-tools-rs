use clap::Parser;
#[path = "../../cc-job-policy/tests/support/mod.rs"]
mod support;
use serde_json::{Value, json};
use std::{
    fs,
    os::windows::process::CommandExt,
    process::{Child, Command, Stdio},
};

struct LiveSibling(Child);
impl Drop for LiveSibling {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn fixture() -> support::Fixture {
    let mut f = support::Fixture::new();
    f.register();
    f.jobs[0]["todo_resume_generation"] = json!(1);
    f.db["items"][0]["resume_generation"] = json!(1);
    f.db["items"][0]["worker_receipt"] = json!({"job":"parent","generation":1});
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

fn current_result(status: &str) -> support::Fixture {
    let mut f = fixture();
    f.jobs[1]["work_owner"] = cc_job_policy::work_owner::checkout(f.jobs[1]["cwd"].as_str().unwrap());
    f.jobs[1]["status"] = json!(status);
    if status == "failed" { f.jobs[1]["last_failure_kind"] = json!("transient"); }
    f.jobs[1]["todo_resume_generation"] = json!(3);
    f.jobs[1]["todo_dependency_generation"] = json!(1);
    f.db["items"][1]["worker_receipt"] = json!({"job":"old-quota","generation":3});
    // Simulate a wait release which used to leave last_job pointing further back.
    f.db["items"][1]["scheduler"]["last_job"] = json!("older-quota");
    if status == "cancelled" {
        f.jobs[1]["recovery_cancel"] = json!({"key":"recovery-old-quota","committed_at":"2026-10-26T01:01:00Z"});
    }
    f
}

#[test]
fn current_parent_rebinds_the_exact_finished_worker_after_cancel_or_pc_return() {
    for status in ["succeeded", "lost", "cancelled", "failed"] {
        let f = current_result(status);
        save(&f);
        let original = fs::read(f.dir.path().join("jobs/old-quota/job.json")).unwrap();
        let out = call(&f, "parent");
        assert!(out.status.success(), "{status}: {}", String::from_utf8_lossy(&out.stderr));
        let db: Value = serde_json::from_slice(&fs::read(f.dir.path().join("todo.json")).unwrap()).unwrap();
        assert_eq!(db["items"][1]["subwork_continuation"]["source_job"], "old-quota");
        assert_eq!(db["items"][1]["subwork_continuation"]["source_kind"], "current_result");
        assert_eq!(db["items"][1]["subwork_continuation"]["source_receipt"], f.db["items"][1]["worker_receipt"]);
        assert_eq!(fs::read(f.dir.path().join("jobs/old-quota/job.json")).unwrap(), original);
    }
}

#[test]
fn finished_worker_rebinding_cannot_replace_current_ownership_or_quota_retry() {
    for case in ["unknown_cancel", "quota_failure", "receipt_job", "receipt_generation", "source_generation", "dependency", "spec", "scope", "source_owner", "branch", "git_common", "worktree", "latest", "future_dependency", "budget", "drain", "stop", "parent"] {
        let mut f = current_result("cancelled");
        match case {
            "unknown_cancel" => { f.jobs[1].as_object_mut().unwrap().remove("recovery_cancel"); },
            "quota_failure" => f.jobs[1]["status"] = json!("failed"),
            "receipt_job" => f.db["items"][1]["worker_receipt"]["job"] = json!("different"),
            "receipt_generation" => f.db["items"][1]["worker_receipt"]["generation"] = json!(2),
            "source_generation" => f.jobs[1]["todo_resume_generation"] = json!(2),
            "dependency" => f.db["items"][1]["wait_on"] = json!([99]),
            "spec" => fs::write(f.db["items"][1]["spec"].as_str().unwrap(), "Changed").unwrap(),
            "scope" => f.db["items"][1]["network"] = json!(true),
            "source_owner" => f.jobs[1]["work_owner"]["repository"] = json!("different"),
            "branch" => f.jobs[1]["work_owner"]["branch"] = json!("different"),
            "git_common" => f.jobs[1]["work_owner"]["git_common"] = json!("different"),
            "worktree" => f.jobs[1]["work_owner"]["worktree"] = json!("different"),
            "latest" => f.db["items"][1]["jobs"].as_array_mut().unwrap().push(json!("later")),
            "future_dependency" => f.jobs[1]["todo_dependency_generation"] = json!(2),
            "budget" => { f.jobs[1]["status"] = json!("failed"); f.jobs[1]["todo_dependency_generation"] = json!(0); f.jobs[1]["capacity_exhausted"] = json!(true); },
            "drain" => { f.jobs[1]["pid"] = f.jobs[0]["pid"].clone(); f.jobs[1]["pid_created"] = f.jobs[0]["pid_created"].clone(); },
            "stop" => f.db["items"][1]["owner_stopped"] = json!(true),
            "parent" => f.db["items"][0]["resume_generation"] = json!(2),
            _ => unreachable!(),
        }
        save(&f);
        let before = fs::read(f.dir.path().join("todo.json")).unwrap();
        let out = call(&f, "parent");
        assert!(!out.status.success(), "{case}");
        let expected = match case {
            "dependency" => "subwork_continuation_generation_or_dependency",
            "drain" => "subwork_continuation_not_drained",
            "parent" => "subwork_continuation_parent_invalid",
            _ => "subwork_continuation_contract_rejected",
        };
        assert!(String::from_utf8_lossy(&out.stderr).contains(expected), "{case}: {}", String::from_utf8_lossy(&out.stderr));
        assert_eq!(fs::read(f.dir.path().join("todo.json")).unwrap(), before, "{case}");
    }
}

#[test]
fn actual_quota_result_after_dependency_release_binds_a_new_contract_without_resetting_history() {
    let mut f = current_result("failed");
    f.jobs[1]["last_failure_kind"] = json!("quota");
    f.jobs[1]["todo_dependency_generation"] = json!(0);
    f.jobs[1]["capacity_same_model_retry_count"] = json!(1);
    f.jobs[1]["capacity_history"] = json!([{"kind":"same_model_account_retry","account_selector":"p000001"}]);
    save(&f);
    let original = fs::read(f.dir.path().join("jobs/old-quota/job.json")).unwrap();
    let out = call(&f, "parent");
    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
    let db: Value = serde_json::from_slice(&fs::read(f.dir.path().join("todo.json")).unwrap()).unwrap();
    assert_eq!(db["items"][1]["subwork_continuation"]["source_kind"], "current_quota");
    assert_eq!(fs::read(f.dir.path().join("jobs/old-quota/job.json")).unwrap(), original);
}

#[test]
fn old_quota_source_cannot_be_reused_after_a_new_worker_owned_the_generation() {
    for receipt in [json!({"job":"new-current","generation":3}), Value::Null] {
        let mut f = fixture();
        f.db["items"][1]["worker_receipt"] = receipt;
        f.db["items"][1]["jobs"].as_array_mut().unwrap().push(json!("new-current"));
        save(&f);
        let before = fs::read(f.dir.path().join("todo.json")).unwrap();
        assert!(!call(&f, "parent").status.success(), "old quota source was reused");
        assert_eq!(fs::read(f.dir.path().join("todo.json")).unwrap(), before);
    }
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
        "parent_stop",
        "parent_generation",
        "parent_birth",
        "legacy_unseeded",
        "legacy_missing_history",
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
            "parent_stop" => f.db["items"][0]["stopped"] = json!(true),
            "parent_generation" => f.db["items"][0]["scheduler"]["job"] = json!("new-owner"),
            "parent_birth" => f.jobs[0]["pid_created"] = json!(1),
            "legacy_unseeded" => {
                f.jobs[1]["quota_recovery"] =
                    json!({"version":1,"source_job":"previous","model":"gpt-6.1-sol"});
            }
            "legacy_missing_history" => {
                f.jobs[1]["quota_recovery"] =
                    json!({"version":1,"source_job":"previous","model":"gpt-6.1-sol"});
                f.jobs[1]["quota_retry_seeded_version"] = json!(1);
                f.jobs[1]["capacity_retry_count"] = json!(1);
            }
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
fn another_process_cannot_impersonate_a_live_parent_with_its_environment() {
    // The real registered parent is alive, but is a sibling of this CLI caller.
    let parent = LiveSibling(
        Command::new("powershell.exe")
            .args([
                "-NoProfile",
                "-NonInteractive",
                "-Command",
                "[Console]::In.ReadLine() | Out-Null",
            ])
            .stdin(Stdio::piped())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .creation_flags(0x08000000)
            .spawn()
            .unwrap(),
    );
    let mut f = fixture();
    f.jobs[0]["pid"] = json!(parent.0.id());
    f.jobs[0]["pid_created"] = json!(cc_scheduler::native::process_birth(parent.0.id()).unwrap());
    save(&f);
    let before = fs::read(f.dir.path().join("todo.json")).unwrap();
    let out = call(&f, "parent");
    assert!(
        !out.status.success(),
        "a live sibling accepted the forged parent environment"
    );
    assert!(
        String::from_utf8_lossy(&out.stderr).contains("subwork_continuation_caller_not_parent")
    );
    assert_eq!(fs::read(f.dir.path().join("todo.json")).unwrap(), before);
}

#[test]
fn current_parent_can_request_a_new_contract_without_rewriting_the_old_source() {
    let parsed = cc_todo::Cli::try_parse_from([
        "todo",
        "continue-subwork",
        "2",
        "--source-job",
        "20000101-010000-todo-2",
        "--parent-job-id",
        "20000101-000000-todo-1",
        "--resume-generation",
        "3",
        "--dependency-generation",
        "1",
        "--spec-sha256",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    ]);
    assert!(
        parsed.is_ok(),
        "no ordinary continuation intake: {parsed:?}"
    );
}
