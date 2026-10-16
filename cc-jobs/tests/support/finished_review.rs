use super::*;
use cc_job_policy::{finished_review::Request, product_retry::digest, work_owner};

fn setup() -> (Test, subwork_support::Fixture, Request) {
    let (t, mut f, _) = finite_owner_wait_repair::setup();
    let tree = subwork_support::tree(&f.repo, "todo-3-writer", "scheduler/todo-3-writer");
    let mut w = f.jobs[0].clone();
    w["id"] = json!("writer");
    w["todo_id"] = json!(3);
    w["cwd"] = json!(tree);
    w["work_owner"] = work_owner::checkout(tree.to_str().unwrap());
    w["work_owner"]["pr"] = json!(86);
    t.put(w.clone());
    f.db["items"].as_array_mut().unwrap().extend([
        json!({"id":3,"status":"wait","wait_on":[77],"jobs":["writer"],"scheduler":{"job":"writer","worktree":tree,"branch":"scheduler/todo-3-writer"}}),
        json!({"id":77,"status":"block","note":"unfinished cleanup"})]);
    fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
    let mut j = t.ctx.load("child").unwrap();
    j["steps"] = json!(0);
    j["network"] = json!(false);
    j["sandbox"] = json!("danger-full-access");
    j["scope_source"] = json!("launcher");
    j["add_dirs"] = json!(["original-launch-write-hint"]);
    j["fixed_review_protocol"] = json!({"version":1,"pid":j["pid"],"created":j["pid_created"]});
    j["work_wait_on"] = json!([3]);
    j["work_wait_evidence"] = json!({"owner_job_id":"writer","owner":work_owner::owner_evidence(&w),"candidate_identity":j["work_owner"]});
    t.put(j);
    let spec = fs::read(f.db["items"][1]["spec"].as_str().unwrap()).unwrap();
    let r = Request {
        job_id: "child".into(),
        todo_id: 2,
        parent_job_id: "parent".into(),
        parent_todo_id: 1,
        owner_job_id: "writer".into(),
        owner_todo_id: 3,
        read_only: true,
        child_spec_sha256: digest(&spec),
        parent_spec_sha256: digest(&spec),
        prompt_sha256: digest(b"fixture task"),
        fixed_head: "a".repeat(40),
        fixed_base: "b".repeat(40),
    };
    t.rt.handler.lock().unwrap().replace(Box::new(|c| {
        Ok(Reply {
            code: 0,
            stderr: vec![],
            stdout: if c.argv.iter().any(|a| a == "rev-parse") {
                format!("{}\n", "a".repeat(40)).into_bytes()
            } else {
                vec![]
            },
        })
    }));
    (t, f, r)
}

#[test]
fn original_reader_admits_and_receives_fixed_read_only_source_while_cleanup_stays_unfinished() {
    let (t, f, r) = setup();
    let request = t.ctx.root.join("request.json");
    fs::write(&request, serde_json::to_vec(&r).unwrap()).unwrap();
    let before = t
        .ctx
        .read_json(&t.ctx.job_dir("child").join("job.json"))
        .unwrap();
    let todo_before = fs::read(f.dir.path().join("todo.json")).unwrap();
    let prompt_before = fs::read(t.ctx.job_dir("child").join("prompt.md")).unwrap();
    let args = [
        "qualify-fixed-review",
        "--request-file",
        request.to_str().unwrap(),
    ];
    assert_eq!(t.cli(&args).0.unwrap(), 0);
    assert_eq!(
        t.ctx
            .read_json(&t.ctx.job_dir("child").join("job.json"))
            .unwrap(),
        before
    );
    let mut args = args.to_vec();
    args.push("--apply");
    assert_eq!(t.cli(&args).0.unwrap(), 0);
    let after = t
        .ctx
        .read_json(&t.ctx.job_dir("child").join("job.json"))
        .unwrap();
    let allowed = [
        "work_wait_on",
        "work_wait_evidence",
        "work_wait_history",
        "queue_wait_reason",
        "fixed_read_only_review",
        "sandbox",
        "approval_policy",
        "approvals_reviewer",
    ];
    for (k, v) in before.as_object().unwrap() {
        if !allowed.contains(&k.as_str()) {
            assert_eq!(&after[k], v, "{k}");
        }
    }
    assert_eq!(
        fs::read(f.dir.path().join("todo.json")).unwrap(),
        todo_before
    );
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    // No new job, duplicate review or synthetic writer->reader wait was needed.
    assert!(t.ctx.admit("child").unwrap().is_some());
    let running = t.ctx.load("child").unwrap();
    assert_eq!(running["status"], "running");
    assert_eq!(running["work_wait_on"], json!([]));
    assert_eq!(running["sandbox"], "read-only");
    let args = codex_args(&running);
    assert!(args.windows(2).any(|a| a == ["-s", "read-only"]));
    assert!(args.iter().any(|a| a == "approval_policy=never"));
    assert!(args.iter().any(|a| a == "approvals_reviewer=user"));
    assert!(
        !args
            .iter()
            .any(|a| a == "--add-dir" || a.contains("network_access=true"))
    );
    let path = t.ctx.prompt_for_attempt(&running).unwrap();
    let text = fs::read_to_string(path).unwrap();
    assert!(text.contains(&r.fixed_head));
    assert!(text.contains(&r.fixed_base));
    assert!(text.ends_with(cc_job_policy::READ_GUIDANCE));
    assert_eq!(
        fs::read(t.ctx.job_dir("child").join("prompt.md")).unwrap(),
        prompt_before
    );
    let db = t.ctx.read_json(&f.dir.path().join("todo.json")).unwrap();
    assert_eq!(db["items"][2]["status"], "wait");
    assert_eq!(db["items"][2]["wait_on"], json!([77]));
    assert_eq!(db["items"][3]["status"], "block");
}

#[test]
fn execution_checks_the_consumed_prompt_even_if_the_original_path_is_replaced() {
    let (t, f, r) = setup();
    t.ctx.qualify_finished_review(&r, true).unwrap();
    let prompt = t.ctx.job_dir("child").join("prompt.md");
    fs::write(&prompt, b"unverified input that must never be consumed").unwrap();
    let spec = std::path::PathBuf::from(f.db["items"][1]["spec"].as_str().unwrap());
    *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |path, _| {
        if path == spec {
            fs::write(&prompt, b"fixture task").unwrap();
        }
    }));
    // The second read would match the saved hash, but the input already read
    // for execution did not. The old two-read implementation accepted it.
    let job = t.ctx.load("child").unwrap();
    let error = t.ctx.prompt_for_attempt(&job).unwrap_err();
    assert!(
        error.message.contains("changed before execution"),
        "{}",
        error.message
    );
    assert_eq!(
        fs::read(t.ctx.job_dir("child").join("prompt.md")).unwrap(),
        b"fixture task"
    );
    assert!(!t.ctx.job_dir("child").join("execution-prompt.md").exists());
}

#[test]
fn qualifying_refuses_old_or_reused_controller_live_writer_protected_source_and_git_changes() {
    for case in [
        "old_protocol",
        "reused_controller",
        "writer_alive",
        "writer_probe_denied",
        "protected",
        "wrong_head",
        "dirty",
        "unknown_request",
    ] {
        let (t, f, r) = setup();
        match case {
            "old_protocol" => {
                let mut j = t.ctx.load("child").unwrap();
                j["fixed_review_protocol"] = Value::Null;
                t.put(j);
            }
            "reused_controller" => {
                let mut j = t.ctx.load("child").unwrap();
                j["pid_created"] = json!(999);
                t.put(j);
            }
            "writer_alive" => {
                let mut w = t.ctx.load("writer").unwrap();
                w["pid"] = json!(std::process::id());
                w["pid_created"] = json!(123);
                t.put(w);
            }
            "writer_probe_denied" => {
                let mut w = t.ctx.load("writer").unwrap();
                w["pid"] = json!(123456);
                w["pid_created"] = json!(123);
                t.put(w);
                t.rt.proc_probe_errors.lock().unwrap().insert(123456, 5);
            }
            "protected" => {
                t.rt.recovery_guards
                    .lock()
                    .unwrap()
                    .insert(s(&f.jobs[0], "cwd").into(), Some(true));
            }
            "wrong_head" => {
                t.rt.handler.lock().unwrap().replace(Box::new(|_| {
                    Ok(Reply {
                        code: 0,
                        stdout: b"different\n".to_vec(),
                        stderr: vec![],
                    })
                }));
            }
            "dirty" => {
                t.rt.handler.lock().unwrap().replace(Box::new(|c| {
                    Ok(Reply {
                        code: if c.argv.iter().any(|a| a == "diff") {
                            1
                        } else {
                            0
                        },
                        stdout: format!("{}\n", "a".repeat(40)).into_bytes(),
                        stderr: vec![],
                    })
                }));
            }
            "unknown_request" => {
                let mut raw = serde_json::to_value(&r).unwrap();
                raw["grant_write"] = json!(true);
                assert!(serde_json::from_value::<Request>(raw).is_err());
                continue;
            }
            _ => unreachable!(),
        }
        let before = t.ctx.load("child").unwrap();
        assert!(t.ctx.qualify_finished_review(&r, true).is_err(), "{case}");
        assert_eq!(t.ctx.load("child").unwrap(), before, "{case}");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
}
