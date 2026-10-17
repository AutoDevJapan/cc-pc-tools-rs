use super::*;
use cc_job_policy::{finished_work_handoff::Request, product_retry::digest, work_owner};

#[test]
fn finished_work_handoff_preserves_original_todo_and_returns_to_normal_admission() {
    let t = Test::new();
    let mut f = subwork_support::Fixture::new();
    let parent_path = subwork_support::tree(&f.repo, "todo-1-parent", "scheduler/todo-1-parent");
    let child_path = subwork_support::tree(&f.repo, "todo-2-child", "scheduler/todo-2-child");
    let writer_path = subwork_support::tree(&f.repo, "todo-3-writer", "scheduler/todo-3-writer");
    let other_path = subwork_support::tree(&f.repo, "todo-4-writer", "scheduler/todo-4-writer");
    let child_spec = b"Implement tools/product-ci as finite Go work";
    let parent_spec = b"Integrate and validate the finite child result";
    let owner_spec = b"Implement tools/pr148-windows-runner as finite Go work";
    let specs: [&[u8]; 3] = [child_spec, parent_spec, owner_spec];
    let spec_paths: Vec<_> = specs
        .iter()
        .enumerate()
        .map(|(index, bytes)| {
            let path = t.ctx.root.join(format!("handoff-spec-{index}.md"));
            fs::write(&path, bytes).unwrap();
            path
        })
        .collect();

    let mut identities = Vec::new();
    for path in [&child_path, &parent_path, &writer_path, &other_path] {
        let mut identity = work_owner::checkout(path.to_str().unwrap());
        identity["pr"] = json!(148);
        identities.push(identity);
    }
    let child_id = "20261017-023628-todo-2751";
    let parent_id = "20261017-015924-todo-2736";
    let owner_id = "20261017-022626-todo-2749";
    let old_owner_id = "20261017-000000-old-writer";
    let launch_key = "a7f201ad5b51483f8fa9e7820a1b60ff";
    let code_head = "da2b45ea76efeb14f24211ed1a156ed051027515";
    let source_head = "497b2f11a6beb2970cf3c9d383f4c334b0618209";

    let mut candidate = json!({"id":child_id,"todo_id":2,"status":"queued","cwd":child_path,
        "registered_cwd":f.repo,"launch_key":launch_key,"pid":std::process::id(),"pid_created":123,
        "codex_pid":null,"started_at":null,"thread_id":null,"steps":0,"parent_job_id":"",
        "subwork":null,"related_todo_ids":[],"work_owner":identities[0],"work_wait_on":[3],
        "queue_wait_reason":{"code":"todo_dependency","wait_on":[3]},"work_wait_evidence":null});
    candidate["work_wait_evidence"] = json!({"owner_job_id":owner_id,"owner":{
        "id":owner_id,"todo_id":3,"cwd":writer_path,"project":"alias","registered_cwd":f.repo,"work_owner":identities[2]
    },"candidate_identity":identities[0]});
    let parent = json!({"id":parent_id,"todo_id":1,"status":"succeeded","exit_code":0,
        "finished_at":"2026-10-17T07:00:00+09:00","cwd":parent_path,"registered_cwd":f.repo,
        "work_owner":identities[1]});
    let owner = json!({"id":owner_id,"todo_id":3,"status":"succeeded","exit_code":0,
        "finished_at":"2026-10-17T04:24:24+09:00","cwd":writer_path,"registered_cwd":f.repo,
        "work_owner":identities[2]});
    let marker = "2749最終レビュー配送20261017-da2b45e";
    let mut db = json!({"projects":{"demo":f.repo,"alias":f.repo},"items":[
        {"id":1,"project":"demo","status":"wait","wait_on":[2,3],"jobs":[parent_id],"note":format!("{marker} {code_head} {source_head}"),"spec":spec_paths[1],"scheduler":{"worktree":parent_path,"branch":"scheduler/todo-1-parent"}},
        {"id":2,"project":"alias","status":"doing","dispatchable":true,"wait_on":[],"jobs":[child_id],"spec":spec_paths[0],"scheduler":{"job":child_id,"key":launch_key,"worktree":child_path,"branch":"scheduler/todo-2-child"}},
        {"id":3,"project":"demo","status":"wait","wait_on":[2381],"jobs":[old_owner_id,owner_id],"spec":spec_paths[2],"scheduler":{"job":owner_id,"worktree":writer_path,"branch":"scheduler/todo-3-writer"}},
        {"id":2381,"status":"wait","wait_on":[]}
    ]});
    f.db = db.clone();
    db["items"][2]["scheduler"]["worktree"] = json!(writer_path);
    let prompt_sha = digest(b"fixture task");
    let request = Request {
        job_id: child_id.into(),
        todo_id: 2,
        parent_job_id: parent_id.into(),
        parent_todo_id: 1,
        owner_job_id: owner_id.into(),
        owner_todo_id: 3,
        child_spec_sha256: digest(child_spec),
        parent_spec_sha256: digest(parent_spec),
        owner_spec_sha256: digest(owner_spec),
        prompt_sha256: prompt_sha,
        writer_code_head: code_head.into(),
        delivered_source_head: source_head.into(),
        writer_scope: "tools/pr148-windows-runner".into(),
        candidate_scope: "tools/product-ci".into(),
        delivery_marker: marker.into(),
    };
    candidate["work_wait_evidence"]["owner"] = work_owner::owner_evidence(&owner);
    t.put(candidate.clone());
    t.put(parent.clone());
    t.put(owner.clone());
    let old_owner = json!({"id":old_owner_id,"todo_id":3,"status":"succeeded","exit_code":0,
        "finished_at":"2026-10-17T02:54:34+09:00","cwd":writer_path,"registered_cwd":f.repo,
        "work_owner":identities[2]});
    t.put(old_owner.clone());
    let other = json!({"id":"other-live-writer","todo_id":4,"status":"running",
        "pid":std::process::id(),"pid_created":123,"cwd":other_path,"registered_cwd":f.repo,
        "work_owner":identities[3]});
    t.put(other);
    t.rt.env
        .lock()
        .unwrap()
        .insert("CC_TODO_HOME".into(), f.dir.path().to_string_lossy().into());
    fs::write(f.dir.path().join("todo.json"), db.to_string()).unwrap();

    let writer_head = source_head.to_string();
    t.rt.with_handler(move |call| {
        assert_eq!(call.argv[0], "git");
        let stdout = match call.argv.get(4).map(String::as_str) {
            Some("rev-parse") if call.argv.get(5).is_some_and(|arg| arg == "HEAD") => {
                format!("{writer_head}\n").into_bytes()
            }
            Some("diff-tree") => b"tools/pr148-windows-runner/main.go\n".to_vec(),
            Some("ls-tree") => b"tools/pr148-windows-runner/main.go\n".to_vec(),
            Some("status") => vec![],
            _ => vec![],
        };
        Ok(Reply {
            code: 0,
            stdout,
            stderr: vec![],
        })
    });

    let request_path = t.ctx.root.join("handoff-request.json");
    fs::write(&request_path, serde_json::to_vec(&request).unwrap()).unwrap();
    let before_todo = fs::read(f.dir.path().join("todo.json")).unwrap();
    let args = [
        "handoff-finished-work",
        "--request-file",
        request_path.to_str().unwrap(),
    ];
    let mut live_history = old_owner.clone();
    live_history["status"] = json!("running");
    live_history["pid"] = json!(std::process::id());
    live_history["pid_created"] = json!(123);
    t.put(live_history);
    assert!(t.cli(&args).0.is_err());
    t.put(old_owner);
    let (result, output, _) = t.cli(&args);
    assert_eq!(result.unwrap(), 0);
    assert_eq!(
        serde_json::from_str::<Value>(&output).unwrap()["applied"],
        false
    );
    assert_eq!(t.ctx.load(child_id).unwrap()["work_wait_on"], json!([3]));
    assert_eq!(
        fs::read(f.dir.path().join("todo.json")).unwrap(),
        before_todo
    );

    let mut apply = args.to_vec();
    apply.push("--apply");
    assert_eq!(t.cli(&apply).0.unwrap(), 0);
    let saved = t.ctx.load(child_id).unwrap();
    assert_eq!(saved["work_wait_on"], json!([]));
    assert_eq!(
        saved["work_wait_history"][0]["reason"],
        "finished_work_handoff"
    );
    assert_eq!(
        saved["work_wait_history"][0]["evidence"],
        candidate["work_wait_evidence"]
    );
    assert_eq!(
        fs::read(f.dir.path().join("todo.json")).unwrap(),
        before_todo
    );
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    // The command only changes the exact historical edge. The regular queue still
    // checks every other owner and capacity condition before execution.
    assert!(t.ctx.admit(child_id).unwrap().is_none());
    assert_eq!(
        t.ctx.load(child_id).unwrap()["queue_wait_reason"]["code"],
        "work_owner"
    );
}
