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
fn todo2341_legacy_artifact_requires_current_unique_generation_receipt() {
    let (f, mut db, mut source, registered, artifact) = receipt_workspace_fixture();
    source["todo_id"] = Value::Null;
    source["related_todo_ids"] = json!([]);
    db["items"][0]["resume_generation"] = json!(7);
    db["items"][0]["scheduler"]["job"] = json!("source");
    db["items"][0]["worker_receipt"] = json!({"job":"source","generation":7});
    let item = &db["items"][0];
    let check = |row: &Value, job: &Value, store: &Value| {
        f.console
            .receipt_artifact_directory(
                artifact.to_str().unwrap(),
                row,
                job,
                store,
                registered.to_str().unwrap(),
            )
            .unwrap()
    };
    assert!(check(item, &source, &db));
    assert!(check(item, &source, &db)); // read-only; no synthetic job ownership
    assert!(source["todo_id"].is_null());
    for patch in [
        json!({"worker_receipt":{"job":"source","generation":6}}),
        json!({"worker_receipt":{"job":"another","generation":7}}),
        json!({"scheduler":{"job":"another"}}),
        json!({"resume_generation":0}),
        json!({"jobs":[]}),
    ] {
        let mut changed = db.clone();
        for (key, value) in patch.as_object().unwrap() {
            changed["items"][0][key] = value.clone();
        }
        assert!(!check(&changed["items"][0], &source, &changed), "{patch}");
    }
    for patch in [
        json!({"todo_id":2}),
        json!({"todo_id":"1"}),
        json!({"related_todo_ids":[2]}),
        json!({"related_todo_ids":"bad"}),
        json!({"status":"running"}),
        json!({"id":"another"}),
    ] {
        let mut other = source.clone();
        for (key, value) in patch.as_object().unwrap() {
            other[key] = value.clone();
        }
        assert!(!check(item, &other, &db), "{patch}");
    }
    let mut shared = db.clone();
    let mut other = item.clone();
    other["id"] = json!(2);
    shared["items"].as_array_mut().unwrap().push(other);
    assert!(!check(&shared["items"][0], &source, &shared));
    let mut fabricated = item.clone();
    fabricated["resume_generation"] = json!(8);
    fabricated["worker_receipt"]["generation"] = json!(8);
    assert!(!check(&fabricated, &source, &db));
    fs::write(f.root.join("excluded-projects.txt"), "demo\n").unwrap();
    assert!(!check(item, &source, &db));
}

#[test]
fn todo2341_legacy_artifact_dispatch_retains_bytes_and_latest_permission_note() {
    let (f, mut db, mut source, _, artifact) = receipt_workspace_fixture();
    source["todo_id"] = Value::Null;
    source["related_todo_ids"] = json!([]);
    db["items"][0]["resume_generation"] = json!(7);
    db["items"][0]["scheduler"]["job"] = json!("source");
    db["items"][0]["worker_receipt"] = json!({"job":"source","generation":7});
    db["items"][0]["note"] =
        json!("本人回答 message 123: 通常検査送信を許可。元音源と元宛先だけ。");
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
    fs::write(artifact.join("result.mp4"), b"owned bytes").unwrap();
    let mut state = f.console.read_state().unwrap();
    state.queue.push(Trigger {
        key: "decision:source".into(),
        name: "orch-todo-1".into(),
        cwd: artifact.to_string_lossy().into(),
        prompt: "残る検査を進める".into(),
        todo_id: Some(1),
        options: Value::Null,
    });
    f.console
        .dispatch(&mut state, &f.console.jobs().unwrap())
        .unwrap();
    assert_eq!(f.launches(), 1);
    assert_eq!(
        model_arg(&model_run(&f), "--cwd"),
        Some(artifact.to_string_lossy().into())
    );
    assert_eq!(
        fs::read(artifact.join("result.mp4")).unwrap(),
        b"owned bytes"
    );
    let prompts = fs::read_dir(f.console.paths.state.join("console-prompts")).unwrap();
    assert!(prompts
        .filter_map(|e| fs::read_to_string(e.unwrap().path()).ok())
        .any(|p| p.contains("本人回答 message 123") && p.contains("元音源と元宛先だけ")));
}
