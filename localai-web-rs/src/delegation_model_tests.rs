fn model_item(f: &Fixture, model: Value, effort: Value) {
    let mut db = f.console.todos().unwrap();
    db["items"][0]["model"] = model;
    db["items"][0]["effort"] = effort;
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
}
fn watchdog_request(item: &Value, pending: &Value) -> Value {
    cc_todo::recovery::delegation_request(item, pending)
}
fn model_run(f: &Fixture) -> Vec<String> {
    let calls = f.fake.calls.lock().unwrap();
    let runs: Vec<_> = calls
        .iter()
        .filter(|call| call.get(1).is_some_and(|v| v == "run"))
        .collect();
    assert_eq!(
        runs.len(),
        1,
        "must invoke the independent runner exactly once"
    );
    runs[0].clone()
}
fn model_arg(args: &[String], key: &str) -> Option<String> {
    args.iter()
        .position(|a| a == key)
        .map(|i| args[i + 1].clone())
}
fn model_source(f: &Fixture) -> Value {
    f.job(
        "source",
        "work",
        "succeeded",
        "状態: 継続中\n残りを検査する",
        "",
    );
    let mut source = json!({"id":"source","name":"work","status":"succeeded","cwd":f.root,
        "todo_id":1,"model":"gpt-6.1-sol","effort":"high","thread_id":"12345678-1234-4321-9234-123456789abc"});
    source["launch_key"] = json!("previous-launch");
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    let mut db = f.console.todos().unwrap();
    db["items"][0]["jobs"] = json!(["source"]);
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    source
}

#[test]
fn todo1624_fake_runner_covers_partial_and_default_settings() {
    for (model, effort, expected_model, expected_effort) in [
        (json!("gpt-6-luna"), Value::Null, "gpt-6-luna", "high"),
        (Value::Null, json!("max"), "gpt-6.1-sol", "max"),
        (json!("gpt-6-luna"), json!("max"), "gpt-6-luna", "max"),
        (Value::Null, Value::Null, "gpt-6.1-sol", "high"),
    ] {
        let f = Fixture::new();
        f.on();
        model_item(&f, model, effort);
        f.delegate("partial").unwrap();
        let before = f.console.read_state().unwrap().fingerprints.clone();
        f.console.tick().unwrap();
        assert_eq!(
            model_arg(&model_run(&f), "--model").as_deref(),
            Some(expected_model)
        );
        assert_eq!(
            model_arg(&model_run(&f), "--effort").as_deref(),
            Some(expected_effort)
        );
        assert_eq!(f.console.read_state().unwrap().fingerprints, before);
        f.delegate("partial").unwrap();
        f.console.tick().unwrap();
        assert_eq!(f.launches(), 1);
    }
}

#[test]
fn todo1624_explicit_request_is_durable_and_current_todo_wins_at_launch() {
    let f = Fixture::new();
    f.on();
    let body = json!({"id":1,"prompt":"候補をレビューする","request_id":"explicit",
        "model":"gpt-6-luna","effort":"max"});
    f.console
        .api(&Method::POST, "delegate", body.clone())
        .unwrap();
    let accepted = f.console.read_state().unwrap();
    assert_eq!(
        accepted.request_options["manual:explicit"]["model"],
        "gpt-6-luna"
    );
    model_item(&f, json!("gpt-6-astra"), json!("high"));
    let restarted = Console {
        paths: f.console.paths.clone(),
        runner: f.fake.clone(),
        lock: Mutex::new(()),
    };
    restarted.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6-astra"));
    assert_eq!(model_arg(&args, "--effort").as_deref(), Some("high"));
    restarted
        .api(&Method::POST, "delegate", body.clone())
        .unwrap();
    assert_eq!(
        restarted.read_state().unwrap().fingerprints,
        accepted.fingerprints
    );
    let mut changed = body;
    changed["effort"] = json!("low");
    assert_eq!(
        restarted
            .api(&Method::POST, "delegate", changed)
            .unwrap_err()
            .code,
        "request_id_conflict"
    );
    assert_eq!(f.launches(), 1);
}

#[test]
fn todo1624_invalid_request_or_current_todo_never_invokes_runner() {
    for (field, value) in [
        ("model", json!("bad value")),
        ("model", json!(3)),
        ("effort", json!("ultra")),
        ("effort", json!(false)),
    ] {
        let f = Fixture::new();
        f.on();
        let mut body = json!({"id":1,"prompt":"レビューする","request_id":"invalid"});
        body[field] = value.clone();
        assert_eq!(
            f.console
                .api(&Method::POST, "delegate", body)
                .unwrap_err()
                .code,
            "invalid_request"
        );
        assert!(f.console.read_state().unwrap().queue.is_empty());
        f.delegate("valid-before-change").unwrap();
        let mut db = f.console.todos().unwrap();
        db["items"][0][field] = value;
        fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
        f.console.tick().unwrap();
        assert_eq!(
            f.console.read_state().unwrap().preparation_failures["manual:valid-before-change"].1,
            "invalid_request"
        );
        assert_eq!(f.launches(), 0);
    }
}

#[test]
fn todo1624_old_explicit_stall_and_decision_merge_into_existing_manual() {
    let f = Fixture::new();
    f.on();
    fs::write(f.root.join(".git"), "fake").unwrap();
    model_source(&f);
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    let mut db = f.console.todos().unwrap();
    db["items"][0]["stall_recovery"] = json!({"pending":{"request":"stall-1-2"}});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console
        .api(
            &Method::POST,
            "delegate",
            json!({"id":1,"prompt":"固定候補をレビューする","request_id":"canonical",
        "model":"gpt-6-luna","effort":"max","isolate_worktree":true}),
        )
        .unwrap();
    f.console
        .api(
            &Method::POST,
            "delegate",
            json!({"id":1,"prompt":"古い再開","request_id":"stall-1-2",
        "source_job_id":"source","model":"gpt-6.1-sol","effort":"high","isolate_worktree":false,
        "resume_session":"12345678-1234-4321-9234-123456789abc"}),
        )
        .unwrap();
    let before = f.console.read_state().unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6-luna"));
    assert_eq!(model_arg(&args, "--effort").as_deref(), Some("max"));
    assert_eq!(model_arg(&args, "--resume-session"), None);
    let after = f.console.read_state().unwrap();
    assert!(after.queue.is_empty());
    assert_eq!(
        after.fingerprints["manual:canonical"],
        before.fingerprints["manual:canonical"]
    );
    assert_eq!(
        after.request_options["manual:canonical"],
        before.request_options["manual:canonical"]
    );
    assert_eq!(
        after.merged_requests["manual:stall-1-2"],
        "manual:canonical"
    );
    assert_eq!(after.merged_requests["decision:source"], "manual:canonical");
    f.console.tick().unwrap();
    assert_eq!(f.launches(), 1);
}

#[test]
fn todo1624_watch_request_to_saved_queue_to_runner_uses_current_todo() {
    let f = Fixture::new();
    f.on();
    fs::write(f.root.join(".git"), "fake").unwrap();
    let source = model_source(&f);
    model_item(&f, json!("gpt-6-astra"), json!("high"));
    let pending = json!({"request":"stall-1-1","prompt":"指定モデルで再開する","job":source,
        "workspace":{"cwd":f.root,"isolate_worktree":false}});
    let mut db = f.console.todos().unwrap();
    db["items"][0]["stall_recovery"] = json!({"pending":pending});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let payload = watchdog_request(&db["items"][0], &pending);
    assert_eq!(payload["model"], "gpt-6-astra");
    assert_eq!(payload["resume_session"], Value::Null);
    assert_eq!(payload["isolate_worktree"], true);
    f.console.api(&Method::POST, "delegate", payload).unwrap();
    assert_eq!(
        f.console.read_state().unwrap().request_options["manual:stall-1-1"]["model"],
        "gpt-6-astra"
    );
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6-astra"));
    assert_eq!(model_arg(&args, "--resume-session"), None);
}

#[test]
fn todo1624_model_mismatch_also_clears_old_explicit_resume() {
    let options = launch_options(
        Some(&json!({"model":"gpt-6-luna","effort":"max"})),
        &json!({"model":"gpt-6.1-sol","effort":"high","isolate_worktree":false,
        "resume_session":"12345678-1234-4321-9234-123456789abc"}),
        Some(&json!({"model":"gpt-6.1-sol","effort":"high"})),
        true,
    )
    .unwrap();
    assert_eq!(options["model"], "gpt-6-luna");
    assert_eq!(options["effort"], "max");
    assert_eq!(options["isolate_worktree"], true);
    assert_eq!(options["resume_session"], Value::Null);
}

#[test]
fn todo1624_linked_worker_blocks_queued_review_without_extra_start() {
    let f = Fixture::new();
    f.on();
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    f.delegate("waiting-review").unwrap();
    f.job("existing", "worker", "running", "", "");
    let mut db = f.console.todos().unwrap();
    db["items"][0]["jobs"] = json!(["existing"]);
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console.tick().unwrap();
    assert_eq!(f.launches(), 0);
    assert_eq!(f.console.read_state().unwrap().queue.len(), 1);
}

#[test]
fn todo1624_final_recheck_defers_changed_model_and_racing_owner() {
    for change in ["model", "owner", "done"] {
        let f = Fixture::new();
        f.on();
        model_item(&f, json!("gpt-6-luna"), json!("max"));
        f.delegate("recheck").unwrap();
        let mut changed = f.console.todos().unwrap();
        match change {
            "model" => changed["items"][0]["model"] = json!("gpt-6-astra"),
            "owner" => changed["items"][0]["jobs"] = json!(["racing-worker"]),
            "done" => changed["items"][0]["status"] = json!("done"),
            _ => unreachable!(),
        }
        *f.fake.before_admission.lock().unwrap() = Some(changed);
        f.console.tick().unwrap();
        if change == "model" {
            assert_eq!(
                model_arg(&model_run(&f), "--model").as_deref(),
                Some("gpt-6-astra")
            );
        } else if change == "owner" {
            assert_eq!(f.launches(), 0);
            assert_eq!(f.console.read_state().unwrap().queue.len(), 1);
        } else {
            assert_eq!(f.launches(), 0);
            assert!(f.console.read_state().unwrap().queue.is_empty());
        }
    }
}

#[test]
fn todo1624_watch_partial_settings_and_invalid_values_are_not_dropped() {
    let pending = json!({"job":{"model":"gpt-6.1-sol","effort":"high"},"workspace":{"isolate_worktree":false}});
    for (item, model, effort, isolated) in [
        (json!({}), json!("gpt-6.1-sol"), json!("high"), false),
        (
            json!({"model":"gpt-6-luna"}),
            json!("gpt-6-luna"),
            json!("high"),
            true,
        ),
        (
            json!({"effort":"max"}),
            json!("gpt-6.1-sol"),
            json!("max"),
            true,
        ),
        (json!({"effort":4}), json!("gpt-6.1-sol"), json!(4), true),
    ] {
        let request = watchdog_request(&item, &pending);
        assert_eq!(request["model"], model);
        assert_eq!(request["effort"], effort);
        assert_eq!(request["isolate_worktree"], isolated);
    }
}

#[test]
fn todo1624_watch_retry_freezes_request_but_launch_rechecks_todo() {
    let f = Fixture::new();
    f.on();
    let source = model_source(&f);
    let mut pending = json!({"request":"stall-1-1","prompt":"再開する","job":source,
        "workspace":{"cwd":f.root,"isolate_worktree":false}});
    let mut db = f.console.todos().unwrap();
    let original = watchdog_request(&db["items"][0], &pending);
    pending["delegation_request"] = original.clone();
    db["items"][0]["stall_recovery"] = json!({"pending":pending});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console
        .api(&Method::POST, "delegate", original.clone())
        .unwrap();
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    fs::write(f.root.join(".git"), "fake").unwrap();
    let updated = f.console.todos().unwrap();
    let retry = watchdog_request(&updated["items"][0], &pending);
    assert_eq!(retry, original);
    f.console.api(&Method::POST, "delegate", retry).unwrap();
    // A legacy watcher without a frozen payload also reuses this reservation.
    pending
        .as_object_mut()
        .unwrap()
        .remove("delegation_request");
    let legacy_retry = watchdog_request(&updated["items"][0], &pending);
    f.console
        .api(&Method::POST, "delegate", legacy_retry)
        .unwrap();
    f.console.tick().unwrap();
    assert_eq!(
        model_arg(&model_run(&f), "--model").as_deref(),
        Some("gpt-6-luna")
    );
    assert_eq!(model_arg(&model_run(&f), "--resume-session"), None);
}

#[test]
fn todo1624_legacy_stall_with_old_model_starts_one_new_conversation() {
    let f = Fixture::new();
    f.on();
    fs::write(f.root.join(".git"), "fake").unwrap();
    let source = model_source(&f);
    model_item(&f, json!("gpt-6-astra"), json!("high"));
    let mut db = f.console.todos().unwrap();
    db["items"][0]["stall_recovery"] = json!({"pending":{"request":"stall-1-1","job":source,
        "workspace":{"cwd":f.root,"isolate_worktree":false}}});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let trigger = Trigger {
        key: "manual:stall-1-1".into(),
        name: "orch-todo-1".into(),
        cwd: f.root.to_string_lossy().into(),
        prompt: "古い待ち行列の再開".into(),
        todo_id: Some(1),
        options: Value::Null,
    };
    let mut state = f.console.read_state().unwrap();
    f.console.enqueue(&mut state, trigger).unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6-astra"));
    assert_eq!(model_arg(&args, "--resume-session"), None);
    assert_eq!(
        f.console.read_state().unwrap().launch_options["manual:stall-1-1"]["isolate_worktree"],
        true
    );
    assert_eq!(f.launches(), 1);
}

#[test]
fn todo1624_review_m1_linked_source_in_another_checkout_does_not_wait_forever() {
    let f = Fixture::new();
    f.on();
    let mut source = model_source(&f);
    source["cwd"] = json!(f.root.join("another-work"));
    source["model"] = json!("gpt-6-luna");
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    f.delegate("different-checkout").unwrap();
    f.console.tick().unwrap();
    assert_eq!(
        model_arg(&model_run(&f), "--model").as_deref(),
        Some("gpt-6-luna")
    );
    assert_eq!(model_arg(&model_run(&f), "--resume-session"), None);
}

#[test]
fn todo1624_review_m2_invalid_queued_item_does_not_starve_other_todos() {
    let f = Fixture::new();
    f.on();
    f.delegate("invalid-later").unwrap();
    let mut db = f.console.todos().unwrap();
    db["items"][0]["model"] = json!("bad value");
    db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"project":"demo","title":"別の検査","status":"todo","jobs":[]}));
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console
        .api(
            &Method::POST,
            "delegate",
            json!({"id":2,"prompt":"別の検査を進める","request_id":"unrelated"}),
        )
        .unwrap();
    f.console.tick().unwrap();
    assert_eq!(model_arg(&model_run(&f), "--todo-id").as_deref(), Some("2"));
    let state = f.console.read_state().unwrap();
    assert_eq!(state.queue.len(), 1);
    assert_eq!(
        state.preparation_failures["manual:invalid-later"].1,
        "invalid_request"
    );
}

#[test]
fn todo1624_review_m3_current_stall_survives_older_reservation_and_aliases_flatten() {
    let f = Fixture::new();
    f.on();
    fs::write(f.root.join(".git"), "fake").unwrap();
    let source = model_source(&f);
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    let mut db = f.console.todos().unwrap();
    db["items"][0]["stall_recovery"] = json!({"pending":{"request":"stall-1-1","job":source,"workspace":{"cwd":f.root,"isolate_worktree":true}}});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.delegate("stall-1-1").unwrap();
    db["items"][0]["stall_recovery"]["pending"]["request"] = json!("stall-1-2");
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.delegate("stall-1-2").unwrap();
    *f.fake.stall_allow.lock().unwrap() = false;
    f.console.tick().unwrap();
    let state = f.console.read_state().unwrap();
    assert!(state.suppressed.contains_key("manual:stall-1-1"));
    assert_eq!(state.queue.len(), 1);
    assert_eq!(state.queue[0].key, "manual:stall-1-2");
    assert_eq!(state.merged_requests["decision:source"], "manual:stall-1-2");
    f.delegate("canonical-new").unwrap();
    f.console.tick().unwrap();
    assert_eq!(
        f.console.read_state().unwrap().merged_requests["decision:source"],
        "manual:canonical-new"
    );
    *f.fake.stall_allow.lock().unwrap() = true;
    f.console.tick().unwrap();
    assert_eq!(
        model_arg(&model_run(&f), "--model").as_deref(),
        Some("gpt-6-luna")
    );
    assert_eq!(f.launches(), 1);
}

#[test]
fn todo1624_cancelled_stall_is_rechecked_after_capacity_admission() {
    let f = Fixture::new();
    f.on();
    let mut db = f.console.todos().unwrap();
    db["items"][0]["stall_recovery"] = json!({"pending":{"request":"stall-1-1"}});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.delegate("stall-1-1").unwrap();
    db["items"][0]["stall_recovery"]["pending"] = Value::Null;
    *f.fake.before_admission.lock().unwrap() = Some(db);
    f.console.tick().unwrap();
    assert_eq!(f.launches(), 0);
    assert!(
        f.console
            .read_state()
            .unwrap()
            .suppressed
            .contains_key("manual:stall-1-1")
    );
}

#[test]
fn todo1624_invalid_watch_settings_can_be_fixed_without_changing_request_id() {
    let f = Fixture::new();
    f.on();
    fs::write(f.root.join(".git"), "fake").unwrap();
    let source = model_source(&f);
    let mut item = f.console.todos().unwrap()["items"][0].clone();
    item["model"] = json!("bad value");
    item["stall_recovery"] = json!({"pending":{"request":"stall-1-1","prompt":"再開する","job":source,
        "workspace":{"cwd":f.root,"isolate_worktree":false}}});
    assert!(recovery_contract::freeze_delegation_request(&mut item, &json!("stall-1-1")).is_err());
    assert!(item["stall_recovery"]["pending"]["delegation_request"].is_null());
    // A legacy invalid snapshot was never accepted and is recoverable too.
    item["stall_recovery"]["pending"]["delegation_request"] =
        watchdog_request(&item, &item["stall_recovery"]["pending"]);
    item["model"] = json!("gpt-6-astra");
    let payload = recovery_contract::freeze_delegation_request(&mut item, &json!("stall-1-1"))
        .unwrap()
        .unwrap();
    assert_eq!(payload["model"], "gpt-6-astra");
    let mut db = f.console.todos().unwrap();
    db["items"][0] = item;
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console
        .api(&Method::POST, "delegate", payload.clone())
        .unwrap();
    f.console.api(&Method::POST, "delegate", payload).unwrap();
    f.console.tick().unwrap();
    assert_eq!(
        model_arg(&model_run(&f), "--model").as_deref(),
        Some("gpt-6-astra")
    );
    assert_eq!(f.launches(), 1);
}

#[test]
fn todo1624_new_model_keeps_uncommitted_work_in_completed_owners_checkout() {
    let f = Fixture::new();
    f.on();
    let cwd = f.root.join("previous-wt/task");
    fs::create_dir_all(&cwd).unwrap();
    let content = "// 前担当の未コミット作業\nfn changed() {}\n";
    fs::write(cwd.join("pending-change.rs"), content).unwrap();
    let mut source = model_source(&f);
    source["cwd"] = json!(cwd);
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    f.console
        .api(
            &Method::POST,
            "delegate",
            json!({"id":1,"prompt":"残作業を引き継ぐ","request_id":"same-owned-checkout",
        "cwd":cwd,"source_job_id":"source","isolate_worktree":true}),
        )
        .unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(
        model_arg(&args, "--cwd"),
        Some(cwd.to_string_lossy().into())
    );
    assert_eq!(model_arg(&args, "--resume-session"), None);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6-luna"));
    assert_eq!(
        fs::read_to_string(cwd.join("pending-change.rs")).unwrap(),
        content
    );
}

#[test]
fn todo1624_dirty_non_dedicated_source_is_preserved_instead_of_forking_head() {
    let f = Fixture::new();
    f.on();
    fs::write(f.root.join(".git"), "fake").unwrap();
    model_source(&f);
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    fs::write(f.root.join("pending-change.rs"), "// 未コミットの変更").unwrap();
    f.delegate("dirty-root").unwrap();
    f.console.tick().unwrap();
    assert_eq!(f.launches(), 0);
    assert_eq!(
        f.console.read_state().unwrap().preparation_failures["manual:dirty-root"].1,
        "source_changes_require_transfer"
    );
    assert_eq!(
        fs::read_to_string(f.root.join("pending-change.rs")).unwrap(),
        "// 未コミットの変更"
    );
}

#[test]
fn todo1624_omitted_cwd_restores_finished_owners_worktree_and_uncommitted_bytes() {
    let f = Fixture::new();
    f.on();
    let cwd = f.root.join("previous-wt/task");
    fs::create_dir_all(&cwd).unwrap();
    fs::write(cwd.join("pending-change.rs"), "// 引き継ぐ変更").unwrap();
    let mut source = model_source(&f);
    source["cwd"] = json!(cwd);
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    f.delegate("restore-implicit-cwd").unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(
        model_arg(&args, "--cwd"),
        Some(cwd.to_string_lossy().into())
    );
    assert_eq!(model_arg(&args, "--resume-session"), None);
    assert_eq!(
        fs::read_to_string(cwd.join("pending-change.rs")).unwrap(),
        "// 引き継ぐ変更"
    );
}

#[test]
fn todo1624_ambiguous_decision_is_not_consumed_by_manual_canonical() {
    let f = todo1597_fixture("todo");
    let path = f.console.paths.jobs.join("source/job.json");
    let mut source: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
    source["todo_id"] = json!(2);
    fs::write(path, source.to_string()).unwrap();
    let mut db = f.console.todos().unwrap();
    db["items"].as_array_mut().unwrap().push(json!({"id":2,"project":"demo","status":"todo","jobs":[]}));
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console.api(&Method::POST, "delegate", json!({"id":1,"request_id":"canonical",
        "prompt":"明示された作業を進める","cwd":f.root,"isolate_worktree":false})).unwrap();
    let mut state = f.console.read_state().unwrap();
    f.console.dispatch(&mut state, &f.console.jobs().unwrap()).unwrap();
    assert!(!state.merged_requests.contains_key("decision:source"));
    assert!(state.queue.iter().any(|q| q.key == "decision:source"));
    assert!(state.suppressed["decision:source"].contains("複数"));
    assert_eq!(f.launches(), 1);
}
#[test]
fn todo1624_read_only_supplement_does_not_consume_implementation_recovery() {
    for options in [json!({}), json!({"sandbox":"read-only"})] {
        let f = todo1597_fixture("todo");
        let mut payload = json!({"id":1,"request_id":"supplement","cwd":f.root,
            "isolate_worktree":false,"prompt":"読むだけ: 参考レビューを報告する"});
        if options["sandbox"].is_string() {
            payload["sandbox"] = options["sandbox"].clone();
            payload["prompt"] = json!("参考レビューを報告する");
        }
        f.console.api(&Method::POST, "delegate", payload).unwrap();
        let mut state = f.console.read_state().unwrap();
        f.console.dispatch(&mut state, &f.console.jobs().unwrap()).unwrap();
        assert!(!state.merged_requests.contains_key("decision:source"));
        assert_eq!(f.launches(), 1);
        assert!(state.queue.iter().any(|q| q.key == "manual:supplement"));
        let args = model_run(&f);
        assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("danger-full-access"));
    }
}

#[test]
fn todo1624_source_read_only_scope_survives_new_model_conversation() {
    let f = Fixture::new();
    f.on();
    let mut source = model_source(&f);
    source["sandbox"] = json!("read-only");
    source["task_type"] = json!("review");
    fs::write(f.console.paths.jobs.join("source/job.json"), source.to_string()).unwrap();
    model_item(&f, json!("gpt-6-luna"), json!("max"));
    f.console.api(&Method::POST, "delegate", json!({"id":1,"request_id":"review","prompt":"指定レビューを再開する","source_job_id":"source"})).unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("read-only"));
    assert_eq!(model_arg(&args, "--resume-session"), None);
}
#[test]
fn todo1624_cancelled_unlaunched_canonical_restores_original_decision_body() {
    let f = Fixture::new();
    f.on();
    let source = model_source(&f);
    let mut db = f.console.todos().unwrap();
    db["items"][0]["stall_recovery"] = json!({"pending":{"request":"stall-1-1","job":source,
        "workspace":{"cwd":f.root,"isolate_worktree":false}}});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.delegate("stall-1-1").unwrap();
    *f.fake.stall_allow.lock().unwrap() = false;
    f.console.tick().unwrap();
    let state = f.console.read_state().unwrap();
    let original = state.merged_triggers["decision:source"].prompt.clone();
    assert_eq!(state.merged_requests["decision:source"], "manual:stall-1-1");
    db["items"][0]["stall_recovery"]["pending"] = Value::Null;
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console.tick().unwrap();
    let state = f.console.read_state().unwrap();
    assert!(!state.merged_requests.contains_key("decision:source"));
    assert_eq!(state.queue.iter().find(|q| q.key == "decision:source").unwrap().prompt, original);
    assert_eq!(f.launches(), 0);
    *f.fake.stall_allow.lock().unwrap() = true;
    f.console.tick().unwrap();
    assert_eq!(f.launches(), 1);
}
#[test]
fn todo1624_delegate_owner_marker_survives_wrapper_without_changing_receipt() {
    let f = Fixture::new();
    f.on();
    let payload = json!({"id":1,"request_id":"read-only-marker","prompt":"読むだけ: 変更せず所見を返す",
        "cwd":f.root,"isolate_worktree":false});
    f.console.api(&Method::POST, "delegate", payload.clone()).unwrap();
    let before = f.console.read_state().unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("read-only"));
    f.console.api(&Method::POST, "delegate", payload).unwrap();
    let after = f.console.read_state().unwrap();
    assert_eq!(after.request_options, before.request_options);
    assert_eq!(after.fingerprints, before.fingerprints);
    assert_eq!(f.launches(), 1);
}
#[test]
fn todo1624_new_manual_implementation_does_not_inherit_supplemental_review_scope() {
    let f = Fixture::new();
    f.on();
    let mut source = model_source(&f);
    source["sandbox"] = json!("read-only");
    source["task_type"] = json!("review");
    fs::write(f.console.paths.jobs.join("source/job.json"), source.to_string()).unwrap();
    model_item(&f, json!("gpt-6.1-sol"), json!("high"));
    f.delegate("implementation-after-supplement").unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("danger-full-access"));
    assert_eq!(model_arg(&args, "--resume-session"), None);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6.1-sol"));
}
#[test]
fn todo1624_expired_canonical_drops_merged_body_without_replaying_started_work() {
    let f = Fixture::new();
    f.on();
    model_source(&f);
    f.delegate("canonical").unwrap();
    f.console.tick().unwrap();
    let mut state = f.console.read_state().unwrap();
    assert!(state.merged_triggers.contains_key("decision:source"));
    let id = state.launches["manual:canonical"].clone();
    let mut jobs = f.console.jobs().unwrap();
    let job = jobs.iter_mut().find(|j| j["id"] == id).unwrap();
    job["status"] = json!("succeeded");
    job["finished_at"] = json!(chrono::DateTime::from_timestamp((now()-HISTORY_SECONDS-1) as i64, 0).unwrap().to_rfc3339());
    maintain_state(&mut state, &jobs, now());
    assert!(!state.launches.contains_key("manual:canonical"));
    assert!(!state.merged_requests.contains_key("decision:source"));
    assert!(!state.merged_triggers.contains_key("decision:source"));
}
#[test]
fn todo1624_missing_explicit_source_cannot_grant_fallback_review_scope() {
    let f = Fixture::new();
    f.on();
    let mut source = model_source(&f);
    source["sandbox"] = json!("read-only");
    source["task_type"] = json!("review");
    fs::write(f.console.paths.jobs.join("source/job.json"), source.to_string()).unwrap();
    model_item(&f, json!("gpt-6.1-sol"), json!("high"));
    f.console.api(&Method::POST, "delegate", json!({"id":1,"request_id":"missing-source",
        "source_job_id":"missing","prompt":"元の実装を進める"})).unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("danger-full-access"));
    assert_eq!(model_arg(&args, "--resume-session"), None);
}
#[test]
fn todo1624_provider_qualified_subscription_model_survives_watch_and_launch() {
    let f = Fixture::new();
    f.on();
    model_item(&f, json!("anthropic/claude-sonnet-5-5"), Value::Null);
    let pending = json!({"request":"stall-1-1","prompt":"指定モデルで再開する",
        "job":{"id":"old","model":"gpt-6.1-sol","effort":"high"},
        "workspace":{"cwd":f.root,"isolate_worktree":false}});
    let item = f.console.todos().unwrap()["items"][0].clone();
    let mut item_to_freeze = item.clone();
    item_to_freeze["stall_recovery"] = json!({"pending":pending});
    let payload = recovery_contract::freeze_delegation_request(&mut item_to_freeze, &json!("stall-1-1")).unwrap().unwrap();
    assert_eq!(payload["model"], "anthropic/claude-sonnet-5-5");
    f.console.api(&Method::POST, "delegate", json!({"id":1,"request_id":"provider-model",
        "prompt":"元の作業を進める","cwd":f.root,"isolate_worktree":false})).unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("anthropic/claude-sonnet-5-5"));
    assert_eq!(model_arg(&args, "--effort").as_deref(), Some("high"));
    for invalid in ["anthropic//claude-sonnet-5-5", "anthropic/../../file", "/claude-sonnet-5-5", "anthropic/--flag", "anthropic/model name"] {
        assert!(!cc_scheduler::backend::valid_model(invalid), "invalid model: {invalid}");
    }
    assert!(!cc_scheduler::backend::valid_model(&format!("anthropic/{}", "a".repeat(80))));
}