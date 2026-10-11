#[test]
fn todo1842_null_object_upgrade_and_original_replay_keep_one_launch() {
    for legacy in [false, true] {
        let f = Fixture::new();
        f.on();
        model_item(&f, json!("gpt-6.1-sol"), json!("high"));
        let original =
            json!({"id":1,"request_id":"upgrade-1842","prompt":"指定モデルでレビューする"});
        f.console
            .api(&Method::POST, "delegate", original.clone())
            .unwrap();
        if legacy {
            let mut state = f.console.read_state().unwrap();
            // A persisted pre-update receipt has no task_type/sandbox members.
            state.queue[0]
                .options
                .as_object_mut()
                .unwrap()
                .remove("task_type");
            state.queue[0]
                .options
                .as_object_mut()
                .unwrap()
                .remove("sandbox");
            state.queue[0]
                .options
                .as_object_mut()
                .unwrap()
                .remove("_todo_defaults");
            let trigger = state.queue[0].clone();
            state
                .request_options
                .insert(trigger.key.clone(), trigger.options.clone());
            state
                .fingerprints
                .insert(trigger.key.clone(), trigger_signature(&trigger));
            f.console.save(&state).unwrap();
        }
        let mut explicit = original.clone();
        explicit["model"] = json!("gpt-6-astra");
        explicit["effort"] = json!("high");
        f.console
            .api(&Method::POST, "delegate", explicit.clone())
            .unwrap();
        f.console.tick().unwrap();
        let args = model_run(&f);
        assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6-astra"));
        let receipt = f.console.api(&Method::POST, "delegate", original).unwrap();
        assert!(receipt["job_id"].is_string());
        f.console
            .api(&Method::POST, "delegate", explicit.clone())
            .unwrap();
        for (field, value) in [
            ("model", json!("gpt-6.1-sol")),
            ("prompt", json!("別の作業")),
            ("id", json!(2)),
            ("cwd", json!(f.root.join("different"))),
        ] {
            let mut changed = explicit.clone();
            changed[field] = value;
            assert!(f.console.api(&Method::POST, "delegate", changed).is_err());
        }
        assert_eq!(f.launches(), 1);
    }
}

#[test]
fn todo1842_legacy_partial_request_replays_after_dispatch() {
    let f = Fixture::new();
    f.on();
    model_item(&f, json!("gpt-6-astra"), json!("high"));
    let request = json!({"id":1,"request_id":"legacy-1842","prompt":"指定モデルで再開する","isolate_worktree":true});
    f.console
        .api(&Method::POST, "delegate", request.clone())
        .unwrap();
    let mut state = f.console.read_state().unwrap();
    for field in ["task_type", "sandbox", "_todo_defaults"] {
        state.queue[0]
            .options
            .as_object_mut()
            .unwrap()
            .remove(field);
    }
    let old = state.queue[0].clone();
    state
        .request_options
        .insert(old.key.clone(), old.options.clone());
    state
        .fingerprints
        .insert(old.key.clone(), trigger_signature(&old));
    f.console.save(&state).unwrap();
    let before = f.console.read_state().unwrap().fingerprints;
    f.console
        .api(&Method::POST, "delegate", request.clone())
        .unwrap();
    f.console.tick().unwrap();
    let reply = f.console.api(&Method::POST, "delegate", request).unwrap();
    assert!(reply["job_id"].is_string());
    assert_eq!(f.console.read_state().unwrap().fingerprints, before);
    assert_eq!(
        model_arg(&model_run(&f), "--model").as_deref(),
        Some("gpt-6-astra")
    );
}

#[test]
fn todo1842_explicit_intake_wins_over_unchanged_todo_default() {
    let f = Fixture::new();
    f.on();
    model_item(&f, json!("gpt-6.1-sol"), json!("max"));
    f.console.api(&Method::POST, "delegate", json!({"id":1,"request_id":"priority-1842","prompt":"別モデルでレビューする","model":"gpt-6-astra","effort":"high"})).unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--model").as_deref(), Some("gpt-6-astra"));
    assert_eq!(model_arg(&args, "--effort").as_deref(), Some("high"));
}
