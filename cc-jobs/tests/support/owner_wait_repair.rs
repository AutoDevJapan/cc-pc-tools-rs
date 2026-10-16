use super::*;
use cc_job_policy::{product_retry::digest, work_owner};

fn setup() -> (Test, subwork_support::Fixture, Value) {
    let t = Test::new();
    let mut f = subwork_support::Fixture::new();
    let p = subwork_support::tree(&f.repo, "todo-1-parent", "scheduler/todo-1-parent");
    f.jobs[0]["cwd"] = json!(p);
    f.jobs[0]["work_owner"] = work_owner::checkout(p.to_str().unwrap());
    f.jobs[0]["work_owner"]["pr"] = json!(86);
    f.jobs[0]["status"] = json!("succeeded");
    f.jobs[0]["exit_code"] = json!(0);
    f.jobs[0]["finished_at"] = json!("2026-10-10T01:02:00+09:00");
    f.jobs[0]["subwork_live"] = json!(false);
    f.db["items"][0]["scheduler"]["worktree"] = json!(p);
    f.db["items"][0]["scheduler"]["branch"] = json!("scheduler/todo-1-parent");
    f.db["items"][0]["spec"] = f.db["items"][1]["spec"].clone();
    f.db["items"][0]["status"] = json!("wait");
    f.db["items"][0]["wait_on"] = json!([2]);
    f.db["items"][1]["status"] = json!("doing");
    f.db["items"][1]["jobs"] = json!(["child"]);
    f.db["items"][1]["scheduler"]["job"] = json!("child");
    let mut j = f.child_job();
    j["pid"] = json!(std::process::id());
    j["pid_created"] = json!(123);
    j["work_wait_on"] = json!([1]);
    j["work_wait_evidence"] = json!({"owner_job_id":"parent","owner":work_owner::owner_evidence(&f.jobs[0]),"candidate_identity":j["work_owner"]});
    j["queue_wait_reason"] = json!({"code":"todo_dependency","wait_on":[1]});
    t.put(j);
    t.put(f.jobs[0].clone());
    let spec = fs::read(f.db["items"][1]["spec"].as_str().unwrap()).unwrap();
    let r = json!({"job_id":"child","owner_job_id":"parent","todo_id":2,"parent_todo_id":1,"kind":"review",
        "child_spec_sha256":digest(&spec),"parent_spec_sha256":digest(&spec),"prompt_sha256":digest(b"fixture task")});
    t.rt.env
        .lock()
        .unwrap()
        .insert("CC_TODO_HOME".into(), f.dir.path().to_string_lossy().into());
    fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
    (t, f, r)
}

#[test]
fn finite_repair_cli_previews_then_removes_only_job_edge_and_normal_admission_checks_capacity() {
    let (t, f, r) = setup();
    let request = t.ctx.root.join("repair.json");
    fs::write(&request, r.to_string()).unwrap();
    let before = t.ctx.load("child").unwrap();
    let todo_bytes = fs::read(f.dir.path().join("todo.json")).unwrap();
    let args = [
        "repair-owner-wait",
        "--request-file",
        request.to_str().unwrap(),
    ];
    let (result, out, _) = t.cli(&args);
    assert_eq!(result.unwrap(), 0);
    assert_eq!(
        serde_json::from_str::<Value>(&out).unwrap()["applied"],
        false
    );
    assert_eq!(t.ctx.load("child").unwrap(), before);
    let mut args = args.to_vec();
    args.push("--apply");
    assert_eq!(t.cli(&args).0.unwrap(), 0);
    let after = t.ctx.load("child").unwrap();
    assert_eq!(after["work_wait_on"], json!([]));
    assert_eq!(
        after["work_wait_history"][0]["evidence"],
        before["work_wait_evidence"]
    );
    assert_eq!(
        fs::read(f.dir.path().join("todo.json")).unwrap(),
        todo_bytes
    );
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    // A separate live writer still owns the PR, with no exemption from this repair.
    let mut writer = f.jobs[0].clone();
    writer["id"] = json!("other-author");
    writer["todo_id"] = json!(77);
    writer["status"] = json!("running");
    writer["pid"] = json!(std::process::id());
    writer["pid_created"] = json!(123);
    t.put(writer);
    assert!(t.ctx.admit("child").unwrap().is_none());
    assert_eq!(
        t.ctx.load("child").unwrap()["queue_wait_reason"]["code"],
        "work_owner"
    );
}

#[test]
fn finite_repair_rejects_live_parent_pid_reuse_probe_failure_and_lock_contention() {
    for case in [
        "parent_alive",
        "reused_controller",
        "uninspectable",
        "child_started",
        "todo_lock",
    ] {
        let (t, f, r) = setup();
        let _lock = if case == "todo_lock" {
            Some(FileLock::try_acquire(&f.dir.path().join("todo.lock")).unwrap())
        } else {
            None
        };
        match case {
            "parent_alive" => {
                let mut p = t.ctx.load("parent").unwrap();
                p["pid"] = json!(std::process::id());
                p["pid_created"] = json!(123);
                t.put(p);
            }
            "reused_controller" => {
                let mut j = t.ctx.load("child").unwrap();
                j["pid_created"] = json!(999);
                t.put(j);
            }
            "uninspectable" => {
                t.rt.proc_probe_errors
                    .lock()
                    .unwrap()
                    .insert(std::process::id(), 5);
            }
            "child_started" => {
                let mut j = t.ctx.load("child").unwrap();
                j["started_at"] = json!("started");
                t.put(j);
            }
            _ => (),
        }
        let before = fs::read(t.ctx.job_dir("child").join("job.json")).unwrap();
        assert!(t.ctx.repair_owner_wait(&r, true).is_err(), "{case}");
        assert_eq!(
            fs::read(t.ctx.job_dir("child").join("job.json")).unwrap(),
            before,
            "{case}"
        );
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
}
