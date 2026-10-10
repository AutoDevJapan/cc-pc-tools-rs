fn receipt_workspace_fixture() -> (Fixture, Value, Value, PathBuf, PathBuf) {
    let f = Fixture::new();
    f.on();
    let source = model_source(&f);
    let registered = f.root.join("registered");
    let artifact = f.root.join("artifacts");
    fs::create_dir(&registered).unwrap();
    fs::create_dir(&artifact).unwrap();
    let mut db = f.console.todos().unwrap();
    db["projects"]["demo"] = json!(registered);
    let mut source = source;
    source["cwd"] = json!(artifact);
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    (f, db, source, registered, artifact)
}

#[test]
fn todo2164_missing_saved_folder_restores_existing_owned_source() {
    let f = Fixture::new();
    f.on();
    let mut source = model_source(&f);
    let cwd = f.root.join("owned-work");
    fs::create_dir(&cwd).unwrap();
    fs::write(cwd.join("pending-change.rs"), "uncommitted bytes").unwrap();
    source["cwd"] = json!(cwd);
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    let mut db = f.console.todos().unwrap();
    db["items"][0]["scheduler"] = json!({"worktree":f.root.join("missing-worktree")});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let mut state = f.console.read_state().unwrap();
    state.queue.push(Trigger {
        key: "decision:source".into(),
        name: "orch-todo-1".into(),
        cwd: f.root.to_string_lossy().into(),
        prompt: "残りを検査する".into(),
        todo_id: Some(1),
        options: Value::Null,
    });
    f.console
        .dispatch(&mut state, &f.console.jobs().unwrap())
        .unwrap();
    let run = model_run(&f);
    assert_eq!(model_arg(&run, "--cwd"), Some(cwd.to_string_lossy().into()));
    assert_eq!(f.launches(), 1);
    assert_eq!(
        fs::read(cwd.join("pending-change.rs")).unwrap(),
        b"uncommitted bytes"
    );
}

#[test]
fn todo2164_external_artifact_receipt_launches_in_place_without_copy_or_resume() {
    let (f, _, _, _, artifact) = receipt_workspace_fixture();
    let bytes = b"existing final artifact";
    fs::write(artifact.join("result.mp4"), bytes).unwrap();
    let mut state = f.console.read_state().unwrap();
    state.queue.push(Trigger {
        key: "decision:source".into(),
        name: "orch-todo-1".into(),
        cwd: artifact.to_string_lossy().into(),
        prompt: "元の成果を検査する".into(),
        todo_id: Some(1),
        options: Value::Null,
    });
    f.console
        .dispatch(&mut state, &f.console.jobs().unwrap())
        .unwrap();
    let run = model_run(&f);
    assert_eq!(
        model_arg(&run, "--cwd"),
        Some(artifact.to_string_lossy().into())
    );
    assert!(model_arg(&run, "--resume-session").is_none());
    assert_eq!(f.launches(), 1);
    assert_eq!(fs::read(artifact.join("result.mp4")).unwrap(), bytes);
    assert!(!f
        .fake
        .calls
        .lock()
        .unwrap()
        .iter()
        .any(|call| call.iter().any(|arg| arg == "worktree")));
}

#[test]
fn todo2164_artifact_requires_exact_terminal_receipt_and_keeps_exclusions() {
    let (f, db, source, registered, artifact) = receipt_workspace_fixture();
    let cwd = artifact.to_str().unwrap();
    let base = registered.to_str().unwrap();
    let item = &db["items"][0];
    assert!(f
        .console
        .receipt_artifact_directory(cwd, item, &source, &db, base)
        .unwrap());
    for patch in [
        json!({"todo_id":2}),
        json!({"status":"running"}),
        json!({"cwd":registered}),
    ] {
        let mut other = source.clone();
        for (key, value) in patch.as_object().unwrap() {
            other[key] = value.clone();
        }
        assert!(!f
            .console
            .receipt_artifact_directory(cwd, item, &other, &db, base)
            .unwrap());
    }
    let mut unrelated = item.clone();
    unrelated["jobs"] = json!([]);
    assert!(!f
        .console
        .receipt_artifact_directory(cwd, &unrelated, &source, &db, base)
        .unwrap());
    let mut related = source.clone();
    related["todo_id"] = json!(2);
    related["related_todo_ids"] = json!([1]);
    assert!(f
        .console
        .receipt_artifact_directory(cwd, item, &related, &db, base)
        .unwrap());
    related["related_todo_ids"] = json!([3]);
    assert!(!f
        .console
        .receipt_artifact_directory(cwd, item, &related, &db, base)
        .unwrap());
    let mut other_link = item.clone();
    other_link["jobs"] = json!(["different-source"]);
    assert!(!f
        .console
        .receipt_artifact_directory(cwd, &other_link, &source, &db, base)
        .unwrap());
    let mut broad = source.clone();
    broad["cwd"] = json!(f.root);
    assert!(!f
        .console
        .receipt_artifact_directory(f.root.to_str().unwrap(), item, &broad, &db, base)
        .unwrap());
    fs::write(f.root.join("excluded-projects.txt"), "demo\n").unwrap();
    assert!(!f
        .console
        .receipt_artifact_directory(cwd, item, &source, &db, base)
        .unwrap());
}

#[test]
fn todo2164_broad_saved_root_returns_to_registered_source_without_copying_ancestor() {
    let (f, mut db, mut source, registered, _) = receipt_workspace_fixture();
    source["cwd"] = json!(f.root);
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    fs::write(registered.join("source.rs"), "source").unwrap();
    db["items"][0]["stall_recovery"] = json!({"pending":{"workspace":{"cwd":f.root}}});
    let cwd = f
        .console
        .restored_workspace(
            &db["items"][0],
            Some(&source),
            &db,
            registered.to_str().unwrap(),
        )
        .unwrap();
    assert_eq!(cwd, registered.to_str().unwrap());
    assert_eq!(fs::read(registered.join("source.rs")).unwrap(), b"source");
}

#[test]
fn todo2164_subwork_is_never_launched_through_the_unallocated_console_cli() {
    let f = todo1597_fixture("todo");
    let mut db = f.console.todos().unwrap();
    db["items"][0]["subwork"] = json!({"parent_todo_id":122,"kind":"check"});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let mut state = f.console.read_state().unwrap();
    f.console
        .dispatch(&mut state, &f.console.jobs().unwrap())
        .unwrap();
    // The fake does not support Scheduler admission. Keep the accepted request
    // queued instead of falling through to a malformed/unallocated CLI launch.
    assert_eq!(f.launches(), 0);
    assert!(!state.queue.is_empty());
    assert!(!state.preparation_failures.is_empty());
}

#[test]
fn todo2164_subwork_reconciles_scheduler_key_after_console_response_loss() {
    let f = todo1597_fixture("doing");
    let mut db = f.console.todos().unwrap();
    db["items"][0]["scheduler"] = json!({"key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "console_receipts":{"decision:source":{"launch_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}});
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    f.job("scheduler-worker", "todo-1", "running", "", "");
    let path = f.console.paths.jobs.join("scheduler-worker/job.json");
    let mut job: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
    job["todo_id"] = json!(1);
    job["launch_key"] = json!("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
    fs::write(path, job.to_string()).unwrap();
    let mut state = f.console.read_state().unwrap();
    f.console
        .dispatch(&mut state, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(state.launches["decision:source"], "scheduler-worker");
    assert!(state.queue.is_empty());
    assert_eq!(f.launches(), 0);
}
