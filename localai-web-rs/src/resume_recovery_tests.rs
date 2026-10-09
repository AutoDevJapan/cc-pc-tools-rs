fn shared_source(f: &Fixture, binding: Value, receipt: bool) {
    let path = f.console.paths.jobs.join("source/job.json");
    let mut job: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
    job["todo_id"] = binding;
    fs::write(path, job.to_string()).unwrap();
    let mut db = f.console.todos().unwrap();
    if receipt {
        db["items"][0]["scheduler"] = json!({"job":"source","phase":"finished"});
    }
    db["items"].as_array_mut().unwrap().push(json!({
        "id":2,"project":"demo","status":"wait","jobs":["source"],"wait_on":[1]
    }));
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
}

#[test]
fn todo2172_shared_history_uses_explicit_binding_receipt_or_dependency() {
    for (binding, receipt) in [(json!(1), false), (Value::Null, true), (Value::Null, false)] {
        let f = todo1597_fixture("todo");
        shared_source(&f, binding, receipt);
        let mut s = f.console.read_state().unwrap();
        let body = s.queue[0].prompt.clone();
        f.console
            .dispatch(&mut s, &f.console.jobs().unwrap())
            .unwrap();
        f.console
            .dispatch(&mut s, &f.console.jobs().unwrap())
            .unwrap();
        assert_eq!(f.launches(), 1);
        assert_eq!(f.console.todos().unwrap()["items"][1]["status"], "wait");
        assert_eq!(
            f.console.todos().unwrap()["items"][1]["jobs"],
            json!(["source"])
        );
        let call = model_run(&f);
        assert_eq!(model_arg(&call, "--todo-id").as_deref(), Some("1"));
        let prompt = fs::read_to_string(model_arg(&call, "--prompt-file").unwrap()).unwrap();
        assert!(prompt.contains(&body));
    }
}

#[test]
fn todo2172_binding_never_guesses_missing_item_or_crosses_saved_association() {
    let f = todo1597_fixture("todo");
    shared_source(&f, json!(99), false);
    let mut s = f.console.read_state().unwrap();
    let original = s.queue[0].clone();
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(f.launches(), 0);
    assert_eq!(s.queue[0].prompt, original.prompt);
    assert!(!s.handled.contains("decision:source"));

    shared_source(&f, json!(1), true);
    s.queue[0].todo_id = Some(2);
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(f.launches(), 0);
    assert_eq!(s.queue[0].todo_id, Some(2));
}

#[test]
fn todo2172_cycle_and_two_independent_receipts_remain_retriable() {
    for cyclic in [false, true] {
        let f = todo1597_fixture("todo");
        shared_source(&f, Value::Null, false);
        let mut db = f.console.todos().unwrap();
        if cyclic {
            db["items"][0]["wait_on"] = json!([2]);
        } else {
            db["items"][1]["wait_on"] = json!([]);
            for item in db["items"].as_array_mut().unwrap() {
                item["scheduler"] = json!({"job":"source"});
            }
        }
        fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
        let mut s = f.console.read_state().unwrap();
        f.console
            .dispatch(&mut s, &f.console.jobs().unwrap())
            .unwrap();
        assert_eq!(f.launches(), 0);
        assert_eq!(s.queue.len(), 1);
        assert!(!s.handled.contains("decision:source"));
    }
}

#[test]
fn todo2172_structural_failure_waits_for_saved_workspace_repair() {
    let f = todo1597_fixture("todo");
    let restored = f.root.join("saved-checkout");
    let mut db = f.console.todos().unwrap();
    db["items"][0]["scheduler"] = json!({"worktree":restored});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let mut s = f.console.read_state().unwrap();
    let original = s.queue[0].prompt.clone();
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(f.launches(), 0);
    assert_eq!(
        s.preparation_recovery["decision:source"]["code"],
        "source_workspace_unavailable"
    );
    s.preparation_failures.get_mut("decision:source").unwrap().0 = 0;
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(s.preparation_recovery["decision:source"]["attempts"], 1);
    // Repair the exact saved checkout; its dirty file must survive, and the
    // original request must resume immediately without a five-minute delay.
    fs::create_dir(&restored).unwrap();
    fs::write(restored.join("pending-change.rs"), "uncommitted").unwrap();
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(
        f.launches(),
        1,
        "holds={:?}, failures={:?}",
        s.suppressed,
        s.preparation_recovery
    );
    assert_eq!(
        fs::read_to_string(restored.join("pending-change.rs")).unwrap(),
        "uncommitted"
    );
    assert_eq!(
        s.preparation_recovery["decision:source"]["state"],
        "launched"
    );
    let call = model_run(&f);
    let launched = PathBuf::from(model_arg(&call, "--cwd").unwrap());
    assert_eq!(
        fs::read_to_string(launched.join("pending-change.rs")).unwrap(),
        "uncommitted"
    );
    assert!(
        fs::read_to_string(model_arg(&call, "--prompt-file").unwrap())
            .unwrap()
            .contains(&original)
    );
}

#[test]
fn todo2172_broad_old_source_uses_registered_project_and_keeps_review_role() {
    let f = todo1597_fixture("todo");
    let broad = f.root.parent().unwrap();
    let source_file = f.console.paths.jobs.join("source/job.json");
    let mut source: Value = serde_json::from_slice(&fs::read(&source_file).unwrap()).unwrap();
    source["cwd"] = json!(broad);
    source["todo_id"] = json!(1);
    source["task_type"] = json!("review");
    source["sandbox"] = json!("read-only");
    fs::write(source_file, source.to_string()).unwrap();
    // This is an old broad non-Git launch, not a dedicated owned checkout.
    let mut s = f.console.read_state().unwrap();
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(
        f.launches(),
        1,
        "holds={:?}, failures={:?}",
        s.suppressed,
        s.preparation_recovery
    );
    let call = model_run(&f);
    assert_eq!(model_arg(&call, "--sandbox").as_deref(), Some("read-only"));
    assert_eq!(model_arg(&call, "--task-type").as_deref(), Some("review"));
    assert!(model_arg(&call, "--resume-session").is_none());
    assert_eq!(
        s.preparation_recovery["decision:source"]["workspace_resolution"]["reason"],
        "source_outside_registered_project"
    );
    assert_ne!(
        model_arg(&call, "--cwd"),
        Some(broad.to_string_lossy().into_owned())
    );
}

#[test]
fn todo2172_lost_tool_response_retains_receipt_and_runs_independent_request() {
    let f = Fixture::new();
    f.on();
    f.delegate("lost").unwrap();
    let independent = f.root.join("independent");
    fs::create_dir(&independent).unwrap();
    let mut db = f.console.todos().unwrap();
    db["projects"]["independent"] = json!(independent);
    db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"project":"independent",
        "status":"todo","jobs":[],"note":"","title":"別の作業"}));
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let mut s = f.console.read_state().unwrap();
    s.queue.push(Trigger {
        key: "manual:independent".into(),
        name: "independent".into(),
        cwd: independent.to_string_lossy().into_owned(),
        prompt: "別の独立作業を進める".into(),
        todo_id: Some(2),
        options: json!({"isolate_worktree":false}),
    });
    *f.fake.lose_receipt.lock().unwrap() = true;
    assert!(
        f.console
            .dispatch(&mut s, &f.console.jobs().unwrap())
            .is_err()
    );
    assert_eq!(
        f.console
            .jobs()
            .unwrap()
            .iter()
            .filter(|j| active(j))
            .count(),
        2,
        "holds={:?}, failures={:?}",
        s.suppressed,
        s.preparation_recovery
    );
    assert_eq!(s.preparation_recovery["manual:lost"]["code"], "tool_failed");
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(
        f.console
            .jobs()
            .unwrap()
            .iter()
            .filter(|j| active(j))
            .count(),
        2
    );
    assert_eq!(s.preparation_recovery["manual:lost"]["state"], "launched");
    assert!(s.queue.is_empty());
}

#[test]
fn todo2172_missing_or_invalid_running_store_never_becomes_an_empty_queue() {
    let f = Fixture::new();
    f.on();
    f.delegate("preserved").unwrap();
    let original = fs::read(f.console.paths.db()).unwrap();
    let backup = f.root.join("saved-original.json");
    fs::rename(f.console.paths.db(), &backup).unwrap();
    assert_eq!(
        f.console.read_state().err().unwrap().code,
        "state_unavailable"
    );
    fs::write(f.console.paths.db(), "invalid json").unwrap();
    assert_eq!(
        f.console.read_state().err().unwrap().code,
        "state_unavailable"
    );
    assert_eq!(fs::read(&backup).unwrap(), original);
    fs::write(f.console.paths.db(), &original).unwrap();
    f.console
        .dispatch(
            &mut f.console.read_state().unwrap(),
            &f.console.jobs().unwrap(),
        )
        .unwrap();
    assert_eq!(f.launches(), 1);
}

#[test]
fn todo2172_legacy_child_receipt_cannot_override_resumed_parent() {
    for done in [false, true] {
        let f = todo1597_fixture("doing");
        shared_source(&f, Value::Null, false);
        let mut db = f.console.todos().unwrap();
        db["items"][0]["resume_generation"] = json!(2);
        db["items"][0]["worker_receipt"] = json!({"job":null,"generation":2});
        db["items"][1]["scheduler"] = json!({"job":"source","phase":"finished"});
        if done {
            db["items"][1]["status"] = json!("done");
            db["items"][1]["wait_on"] = Value::Null;
        }
        fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
        let mut s = f.console.read_state().unwrap();
        s.queue[0].todo_id = Some(1);
        f.console
            .dispatch(&mut s, &f.console.jobs().unwrap())
            .unwrap();
        assert_eq!(
            f.launches(),
            1,
            "must choose resumed parent even if child done={done}"
        );
        assert_eq!(f.console.todos().unwrap()["items"][1], db["items"][1]);
    }
}

#[test]
fn todo2172_registered_source_repair_unparks_the_same_handoff() {
    let f = todo1597_fixture("doing");
    let wrong = f.root.join("wrong-registered-root");
    fs::create_dir(&wrong).unwrap();
    let mut db = f.console.todos().unwrap();
    db["projects"]["demo"] = json!(wrong);
    db["items"][0]["scheduler"] = json!({"worktree":f.root});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let mut s = f.console.read_state().unwrap();
    let original = s.queue[0].prompt.clone();
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(f.launches(), 0);
    db["projects"]["demo"] = json!(f.root);
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.console
        .dispatch(&mut s, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(f.launches(), 1);
    assert_eq!(
        s.preparation_recovery["decision:source"]["state"],
        "launched"
    );
    let call = model_run(&f);
    assert!(
        fs::read_to_string(model_arg(&call, "--prompt-file").unwrap())
            .unwrap()
            .contains(&original)
    );
}
