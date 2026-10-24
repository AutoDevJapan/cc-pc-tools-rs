use super::*;

fn setup() -> (Test, Value, Value) {
    let t = Test::new();
    let at = t.rt.epoch();
    let health = json!({"at":at,"anthropic":{"at":at,"verified":true,"exhausted":true,"available":false},
        "openai":{"at":at,"verified":true,"exhausted":false,"available":true}});
    *t.rt.quota_recovery_inventory.lock().unwrap() = health.clone();
    let spec = t.ctx.root.join("review-spec.md");
    fs::write(&spec, "Review this exact fixed head.").unwrap();
    let mut row = json!({"id":2,"project":"p","status":"todo","dispatchable":true,"needs":[],"jobs":["quota-reader"],
        "spec":spec,"resume_generation":0,"model":"gpt-6-luna","effort":"max","model_origin":"policy",
        "sandbox":"read-only","task_type":"review","review_source_jobs":["author"],
        "scheduler":{"key":"22222222222222222222222222222222","phase":"prepared","last_job":"quota-reader",
            "worktree":t.ctx.root,"branch":"scheduler/todo-2-review"}});
    let source=t.put(json!({"id":"quota-reader","todo_id":2,"status":"failed","last_failure_kind":"quota",
        "launch_key":"11111111111111111111111111111111","model":"anthropic/claude-opus-5-5","model_origin":"policy",
        "effort":"max","cwd":t.ctx.root,"sandbox":"read-only","task_type":"review","mcp_profile":"minimal",
        "approval_policy":"never","approvals_reviewer":"user","add_dirs":[],"network":false,
        "started_at":"2026-09-28T12:00:00Z","finished_at":"2026-09-28T12:01:00Z",
        "todo_resume_generation":0,"todo_dependency_generation":0,
        "finite_review":{"todo_id":2,"spec":spec,"launch_key":"11111111111111111111111111111111"}}));
    t.put(json!({"id":"author","status":"succeeded","model":"gpt-6.1-sol","cwd":t.ctx.root.join("author")}));
    let target = json!({"model":"gpt-6-luna","model_origin":"policy","effort":"max","cwd":t.ctx.root,
        "launch_key":row["scheduler"]["key"],"task_type":"review","sandbox":"read-only","network":false,
        "mcp_profile":"minimal","approval_policy":"never","approvals_reviewer":"user","add_dirs":[]});
    let proof =
        cc_job_policy::quota_recovery::receipt(&row, &source, &target, &health, at).unwrap();
    row["scheduler"]["selection"] = target;
    row["scheduler"]["selection"]["quota_recovery"] = proof;
    save(&t, &row);
    (t, row, source)
}
fn save(t: &Test, row: &Value) {
    fs::write(
        t.ctx.root.join("todo.json"),
        json!({"projects":{"p":t.ctx.root},"items":[row],"next_id":3}).to_string(),
    )
    .unwrap();
}
fn register(t: &Test) -> Result<i32> {
    t.run(
        &[
            "--todo-id",
            "2",
            "--launch-key",
            "22222222222222222222222222222222",
            "--model",
            "gpt-6-luna",
            "--model-origin",
            "policy",
            "--effort",
            "max",
            "--task-type",
            "review",
            "--review-source-jobs",
            "author",
            "--sandbox",
            "read-only",
            "--mcp-profile",
            "minimal",
        ],
        b"Review the fixed head",
    )
}
fn child(t: &Test) -> Value {
    t.ctx
        .list_jobs(&HashSet::new())
        .into_iter()
        .find(|j| j["todo_id"] == 2 && j["id"] != "quota-reader")
        .unwrap()
}

#[test]
fn verified_quota_review_keeps_luna_max_fast_through_cli_and_queue_admission() {
    let (t, row, _) = setup();
    register(&t).unwrap();
    let j = child(&t);
    assert_eq!(j["model"], "gpt-6-luna");
    assert_eq!(j["effort"], "max");
    assert_eq!(j["model_origin"], "policy");
    assert_eq!(j["review_author_models"], json!(["gpt-6.1-sol"]));
    assert_eq!(j["review_excluded_models"], json!(["gpt-6.1-sol"]));
    let args = codex_args(&j);
    assert!(args.windows(2).any(|a| a == ["-m", "gpt-6-luna"]));
    for value in [
        "model_reasoning_effort=max",
        "service_tier=fast",
        "features.fast_mode=true",
    ] {
        assert!(args.iter().any(|a| a == value));
    }
    // Normal ToDo assignment changes the ownership generation once. The same
    // prepared transition survives that assignment, never an arbitrary resume.
    let paths = cc_todo::Paths::at(&t.ctx.root, &t.ctx.jobs);
    let mut db = cc_todo::load(&paths.store).unwrap();
    cc_todo::execute(
        &mut db,
        &cc_todo::Command::Start {
            id: 2,
            job: s(&j, "id").into(),
            progress: None,
        },
        &paths,
    )
    .unwrap();
    fs::write(&paths.store, db.to_string()).unwrap();
    let state = t.ctx.admit(s(&j, "id")).unwrap();
    assert!(state.is_some(), "{}", t.ctx.load(s(&j, "id")).unwrap());
    drop(state);
    let running = t.ctx.load(s(&j, "id")).unwrap();
    assert_eq!(running["status"], "running");
    assert_eq!(running["model"], "gpt-6-luna");
    assert!(running["execution_observed"].is_null()); // a launch request is not a native observation
    assert_eq!(row["model_origin"], "policy");
}

#[test]
fn quota_metadata_rechecks_are_paced_and_finite_without_rewriting_the_original_failure() {
    let (t, row, source) = setup();
    register(&t).unwrap();
    let j = child(&t);
    t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["available"] = json!(false);
    for check in 1..=3 {
        assert!(t.ctx.admit(s(&j, "id")).unwrap().is_none());
        let waiting = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(waiting["quota_recovery_recheck_count"], check);
        if check < 3 {
            let queries = t.rt.quota_recovery_samples.load(Ordering::Relaxed);
            assert!(t.ctx.admit(s(&j, "id")).unwrap().is_none());
            assert_eq!(t.rt.quota_recovery_samples.load(Ordering::Relaxed), queries);
            let next = t.rt.epoch() + 901.;
            *t.rt.epoch_base.lock().unwrap() = Some(next);
        } else {
            assert_eq!(waiting["status"], "failed");
            assert_eq!(waiting["last_failure_kind"], "quota_recovery");
        }
    }
    assert_eq!(t.ctx.load("quota-reader").unwrap(), source);
    assert_eq!(
        cc_todo::load(&t.ctx.root.join("todo.json")).unwrap()["items"][0],
        row
    );
}

#[test]
fn forged_stale_or_changed_quota_registration_is_rejected_without_a_new_job() {
    for case in [
        "forged",
        "stale",
        "generation",
        "scope",
        "worktree",
        "source_id",
        "live",
        "stop",
        "unintentional_cancel",
        "empty_target",
        "same_author",
        "spec",
    ] {
        let (t, mut row, mut source) = setup();
        match case {
            "forged" => {
                row["scheduler"]["selection"]["quota_recovery"]["source_sha256"] = json!("forged")
            }
            "stale" => *t.rt.epoch_base.lock().unwrap() = Some(t.rt.epoch() + 61.),
            "generation" => row["resume_generation"] = json!(7),
            "scope" => source["network"] = json!(true),
            "worktree" => source["cwd"] = json!(t.ctx.root.join("different")),
            "source_id" => {
                row["scheduler"]["selection"]["quota_recovery"]["source_job"] = json!("author")
            }
            "live" => {
                source["codex_pid"] = json!(444);
                source["codex_pid_created"] = json!(55);
                t.rt.procs.lock().unwrap().insert(444, 55);
            }
            "stop" => row["stopped"] = json!(true),
            "unintentional_cancel" => source["status"] = json!("cancelled"),
            "empty_target" => {
                t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["available"] = json!(false)
            }
            "same_author" => {
                t.put(json!({"id":"author","status":"succeeded","model":"gpt-6-luna"}));
            }
            "spec" => fs::write(row["spec"].as_str().unwrap(), "Changed fixed head").unwrap(),
            _ => unreachable!(),
        }
        t.put(source);
        save(&t, &row);
        assert!(register(&t).is_err(), "{case}");
        assert_eq!(
            t.ctx
                .list_jobs(&HashSet::new())
                .iter()
                .filter(|j| j["todo_id"] == 2)
                .count(),
            1,
            "{case}"
        );
    }
}

#[test]
fn queued_quota_recovery_rechecks_health_stop_dependencies_and_exact_generation() {
    for case in [
        "unavailable",
        "stop",
        "dependency",
        "generation",
        "source",
        "spec",
    ] {
        let (t, mut row, mut source) = setup();
        register(&t).unwrap();
        let j = child(&t);
        match case {
            "unavailable" => {
                t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["available"] = json!(false)
            }
            "stop" => row["stopped"] = json!(true),
            "dependency" => row["wait_on"] = json!([99]),
            "generation" => row["resume_generation"] = json!(99),
            "source" => source["finished_at"] = json!("2026-09-28T12:02:00Z"),
            "spec" => fs::write(row["spec"].as_str().unwrap(), "Another head").unwrap(),
            _ => unreachable!(),
        }
        save(&t, &row);
        t.put(source);
        assert!(t.ctx.admit(s(&j, "id")).unwrap().is_none(), "{case}");
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "queued");
    }
}
