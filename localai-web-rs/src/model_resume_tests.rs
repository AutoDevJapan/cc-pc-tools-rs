#[test]
fn explicit_console_resume_excludes_retired_threads_and_keeps_compatible_threads() {
    let old = "12345678-1234-4321-9234-123456789abc";
    let fresh = "22345678-1234-4321-9234-123456789abc";
    for (thread, retired, expected_resume) in
        [(old, false, true), (old, true, false), (fresh, true, true)]
    {
        let f = Fixture::new();
        f.on();
        f.job(
            "source",
            "todo-1",
            "failed",
            "状態: 継続中\n保存した作業から再開します。",
            "",
        );
        let path = f.console.paths.jobs.join("source/job.json");
        let mut source: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
        source["thread_id"] = json!(thread);
        source["model"] = json!("gpt-6.1-sol");
        source["effort"] = json!("max");
        source["policy_resume_reset"] = if retired {
            json!({"session":old})
        } else {
            Value::Null
        };
        fs::write(path, source.to_string()).unwrap();
        f.console
            .api(
                &Method::POST,
                "delegate",
                json!({"id":1,"request_id":"resume-model-check",
            "prompt":"保存した仕様と成果から続行する。","cwd":f.root,"source_job_id":"source",
            "resume_session":thread,"isolate_worktree":false,"model":"gpt-6.1-sol","effort":"max"}),
            )
            .unwrap();
        f.console.tick().unwrap();
        assert_eq!(f.launches(), 1);
        let calls = f.fake.calls.lock().unwrap();
        let run = calls
            .iter()
            .find(|call| call.get(1).is_some_and(|arg| arg == "run"))
            .unwrap();
        assert_eq!(
            run.windows(2)
                .any(|pair| pair == ["--resume-session", thread]),
            expected_resume
        );
    }
}

#[test]
fn watchdog_review_model_recovery_requires_the_current_saved_transition() {
    let source = json!({"id":"source","todo_id":1,"status":"failed","cwd":"D:/fixture/work",
        "model":"gpt-6.1-sol","effort":"max","task_type":"review","sandbox":"read-only",
        "review_source_jobs":["original-writer"],"review_excluded_models":["anthropic/claude-opus-5-5"]});
    let repaired =
        cc_ask::recovery_failure::repaired_job(&source, cc_ask::recovery_failure::Failure::Model);
    let trigger = Trigger {
        key: "manual:stall-1-1".into(),
        name: "stall-todo-1".into(),
        cwd: "D:/fixture/work".into(),
        prompt: "保存したレビューを続ける。".into(),
        todo_id: Some(1),
        options: json!({"source_job_id":"source","model":"gpt-6-astra","effort":"max"}),
    };
    let item = json!({"id":1,"jobs":["source"],"stall_recovery":{"pending":{
        "request":"stall-1-1","job":repaired}}});
    let options =
        trigger_launch_options(&trigger, Some(&item), &trigger.options, Some(&source)).unwrap();
    assert_eq!(options["model_recovery_from"], "gpt-6.1-sol");
    assert_eq!(options["review_source_jobs"], source["review_source_jobs"]);
    assert_eq!(
        options["review_excluded_models"],
        source["review_excluded_models"]
    );
    assert_eq!(options["sandbox"], "read-only");
    assert_eq!(options["model_origin"], "policy");
    assert_eq!(options["effort_origin"], "policy");
    let mut owner_effort = source.clone();
    owner_effort["effort_origin"] = json!("owner_request");
    let owner =
        trigger_launch_options(&trigger, Some(&item), &trigger.options, Some(&owner_effort))
            .unwrap();
    assert_eq!(owner["effort_origin"], "owner_request");
    for field in ["request", "job"] {
        let mut changed = item.clone();
        changed["stall_recovery"]["pending"][field] = Value::Null;
        let next =
            trigger_launch_options(&trigger, Some(&changed), &trigger.options, Some(&source))
                .unwrap();
        assert!(next["model_recovery_from"].is_null(), "{field}");
    }
    let ordinary = Trigger {
        key: "manual:another-request".into(),
        ..trigger.clone()
    };
    let next = trigger_launch_options(&ordinary, Some(&item), &options, Some(&source)).unwrap();
    assert!(next["model_recovery_from"].is_null());
    for (origin, next_model, expected, hint) in [
        ("policy", "gpt-6.1-sol", "gpt-6-astra", true),
        ("owner_request", "gpt-6.1-sol", "gpt-6.1-sol", false),
        (
            "policy",
            "anthropic/claude-sonnet-5-5",
            "anthropic/claude-sonnet-5-5",
            false,
        ),
    ] {
        let mut edited = item.clone();
        edited["model"] = json!(next_model);
        edited["model_origin"] = json!(origin);
        let next = trigger_launch_options(&trigger, Some(&edited), &trigger.options, Some(&source))
            .unwrap();
        assert_eq!(next["model"], expected);
        assert_eq!(next["model_recovery_from"].is_string(), hint);
        let mut frozen_default = trigger.options.clone();
        frozen_default["model"] = json!("gpt-6.1-sol");
        let next = trigger_launch_options(&trigger, Some(&edited), &frozen_default, Some(&source)).unwrap();
        assert_eq!(next["model"], expected);
        assert_eq!(next["model_recovery_from"].is_string(), hint);
    }
}

#[test]
fn watchdog_review_recovery_passes_scoped_routing_and_provenance_through_tick() {
    for current in [true, false] {
        let f = Fixture::new();
        f.on();
        f.job(
            "source",
            "todo-1",
            "failed",
            "状態: 継続中\nモデルを復旧してレビューを続ける。",
            "",
        );
        let source = json!({"id":"source","name":"todo-1","status":"failed","todo_id":1,
            "cwd":f.root,"model":"gpt-6.1-sol","effort":"max","model_origin":"policy",
            "effort_origin":"policy","task_type":"review","sandbox":"read-only",
            "review_source_jobs":["original-writer"],"review_excluded_models":["anthropic/claude-opus-5-5"]});
        fs::write(
            f.console.paths.jobs.join("source/job.json"),
            source.to_string(),
        )
        .unwrap();
        let mut repaired = cc_ask::recovery_failure::repaired_job(
            &source,
            cc_ask::recovery_failure::Failure::Model,
        );
        repaired["model_repair_pending"] = json!(current);
        let mut db = f.console.todos().unwrap();
        db["items"][0]["jobs"] = json!(["source"]);
        db["items"][0]["model"] = json!("gpt-6.1-sol");
        db["items"][0]["model_origin"] = json!("policy");
        let pending = json!({"request":"stall-1-1","job":repaired,
            "prompt":"保存した仕様と成果からレビューを再開する。",
            "workspace":{"cwd":f.root,"isolate_worktree":false}});
        db["items"][0]["stall_recovery"] = json!({"pending":pending});
        fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
        let request = watchdog_request(&db["items"][0], &pending);
        assert_eq!(request["model"], "gpt-6.1-sol", "use the production frozen request");
        f.console.api(&Method::POST, "delegate", request).unwrap();
        f.console.tick().unwrap();
        assert_eq!(f.launches(), 1, "current={current}");
        let call = model_run(&f);
        assert_eq!(
            model_arg(&call, "--model-recovery-from").as_deref(),
            current.then_some("gpt-6.1-sol")
        );
        assert_eq!(
            model_arg(&call, "--model").as_deref(),
            Some(if current {
                "gpt-6-astra"
            } else {
                "gpt-6.1-sol"
            })
        );
        assert_eq!(
            model_arg(&call, "--review-source-jobs").as_deref(),
            Some("original-writer")
        );
        assert_eq!(
            model_arg(&call, "--review-excluded-models").as_deref(),
            Some("anthropic/claude-opus-5-5")
        );
        assert_eq!(
            model_arg(&call, "--model-origin").as_deref(),
            Some("policy")
        );
        assert_eq!(
            model_arg(&call, "--effort-origin").as_deref(),
            Some("policy")
        );
        assert_eq!(model_arg(&call, "--effort").as_deref(), Some("max"));
        assert_eq!(model_arg(&call, "--sandbox").as_deref(), Some("read-only"));
        assert_eq!(f.console.todos().unwrap()["items"][0]["model"], "gpt-6.1-sol");
    }
}
