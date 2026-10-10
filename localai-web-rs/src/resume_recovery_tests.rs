#[test]
fn todo2299_resolved_receipt_is_todo_class_even_without_stored_id() {
    let f=Fixture::new(); f.on();
    f.job("source","todo-1","succeeded","状態: 継続中","");
    let source_path=f.console.paths.jobs.join("source/job.json");
    let mut source:Value=serde_json::from_slice(&fs::read(&source_path).unwrap()).unwrap();
    source["todo_id"]=json!(1);source["model"]=json!("gpt-6-luna");source["effort"]=json!("high");source["sandbox"]=json!("danger-full-access");source["task_type"]=json!("implementation");
    fs::write(&source_path,source.to_string()).unwrap();
    let mut state=f.console.read_state().unwrap();
    for key in ["manual:orphan-a","manual:orphan-b","manual:orphan-c","decision:source"] {
        state.queue.push(Trigger{key:key.into(),name:format!("orch-{key}"),cwd:f.root.to_string_lossy().into(),prompt:"原本文".into(),todo_id:None,options:json!({"model":"gpt-6-luna","effort":"high","sandbox":"danger-full-access","isolate_worktree":false})});
    }
    let _deadline=cc_ask::tick_runtime::Deadline::new(Duration::from_secs(30));
    f.console.dispatch(&mut state,&f.console.jobs().unwrap()).unwrap();
    assert!(state.launches.contains_key("decision:source"));
    assert_eq!(state.launches.len(),2);
    let calls = f.fake.calls.lock().unwrap();
    let runs: Vec<_> = calls.iter().filter(|call|
        call.get(1).is_some_and(|arg| arg == "run")
        && model_arg(call, "--launch-key").as_deref() == Some(key_hash("decision:source").as_str())
    ).collect();
    assert_eq!(runs.len(), 1, "the resolved receipt must launch exactly once");
    let call=runs[0];
    assert_eq!(model_arg(call,"--todo-id").as_deref(),Some("1"));
    assert!(f.console.todos().unwrap()["items"][0]["jobs"].as_array().unwrap().contains(&json!(state.launches["decision:source"])));
}

#[test]
fn todo2299_unresolved_source_is_parked_before_checkout_probe() {
    let f=Fixture::new();f.on();fs::create_dir(f.root.join(".git")).unwrap();
    let mut state=f.console.read_state().unwrap();
    let trigger=Trigger{key:"decision:missing-source".into(),name:"orch-missing".into(),cwd:f.root.to_string_lossy().into(),prompt:"原本文".into(),todo_id:None,options:Value::Null};
    state.queue.push(trigger.clone());
    state.preparation_failures.insert(trigger.key.clone(),(now()+3600,"source_workspace_unavailable".into()));
    let db=f.console.todos().unwrap();
    // Positive control: this evidence boundary really invokes our Git spy.
    f.console.recovery_evidence_from(&trigger,Some(&db),Some(&[]),&mut BTreeMap::new());
    assert!(f.fake.calls.lock().unwrap().iter().any(|call|call[0]=="git"));
    f.fake.calls.lock().unwrap().clear();
    let _deadline=cc_ask::tick_runtime::Deadline::new(Duration::from_secs(30));
    f.console.dispatch(&mut state,&[]).unwrap();
    assert!(!f.fake.calls.lock().unwrap().iter().any(|call|call[0]=="git"));
    assert_eq!(state.queue[0].prompt,trigger.prompt);
    assert_eq!(f.launches(),0);
}

#[test]
fn todo2299_wait_diagnostics_preserve_receipt_and_failure_history() {
    let f = Fixture::new(); f.on();
    let source = model_source(&f);
    f.job("other-session", "work", "running", "", "");
    fs::write(f.console.paths.jobs.join("other-session/job.json"),
        json!({"id":"other-session","status":"running","cwd":f.root,"todo_id":2,"thread_id":source["thread_id"]}).to_string()).unwrap();
    let body = json!({"id":1,"request_id":"original-wait","prompt":"元の作業を続ける","source_job_id":"source","resume_session":source["thread_id"],"isolate_worktree":false,"model":"gpt-6.1-sol","effort":"high","sandbox":"danger-full-access"});
    f.console.api(&Method::POST,"delegate",body).unwrap();
    let key="manual:original-wait";
    let mut state=f.console.read_state().unwrap();
    let accepted=state.request_options[key].clone();let prompt=state.queue[0].prompt.clone();
    state.preparation_recovery.insert(key.into(),json!({"attempts":3,"legacy_timeout":{"attempts":3,"retry_at":1}}));
    f.console.dispatch(&mut state,&f.console.jobs().unwrap()).unwrap();
    assert_eq!(f.launches(),0);
    let saved=f.console.read_state().unwrap();
    assert_eq!(saved.preparation_recovery[key]["last_wait"]["kind"],"source_session_busy");
    assert_eq!(saved.preparation_recovery[key]["attempts"],3);
    assert_eq!(saved.preparation_recovery[key]["legacy_timeout"]["retry_at"],1);
    assert!(!saved.preparation_failures.contains_key(key));
    assert_eq!(saved.request_options[key],accepted);
    assert_eq!(saved.queue[0].prompt,prompt);
    f.console.note_wait(&mut state,key,"late_scope_or_session",json!({"changed_fields":["effort"]}));
    assert_eq!(state.preparation_recovery[key]["attempts"],3);
}

#[test]
fn todo2299_capacity_detail_needs_same_named_target_and_this_measurement() {
    let report=json!({"todo_id":1,"allowed":false,"at":now(),"at_nanos":"101","report":{"resource_slots":0}});
    assert_eq!(capacity_wait_detail(report.clone(),Some(1),100),report);
    for (id,field,value) in [(None,"todo_id",Value::Null),(Some(1),"todo_id",Value::Null),
        (Some(2),"todo_id",json!(1)),(Some(1),"allowed",json!(true)),
        (Some(1),"at_nanos",json!("99")),(Some(1),"at",json!(now()-20))] {
        let mut observation=report.clone();observation[field]=value;
        assert!(capacity_wait_detail(observation,id,100).is_null(),"{id:?}: {field}");
    }
}

#[test]
fn todo2299_patrol_alternates_todo_and_orphan_ready_classes() {
    let f = Fixture::new();
    f.on();
    let mut db = f.console.todos().unwrap();
    for id in [2,3] { db["items"].as_array_mut().unwrap().push(json!({"id":id,"project":"demo","status":"todo","title":"検証","jobs":[]})); }
    fs::write(f.console.paths.todo.join("todo.json"),db.to_string()).unwrap();
    let mut state = f.console.read_state().unwrap();
    for (key,id) in [("manual:orphan-a",None),("manual:orphan-b",None),("manual:orphan-c",None),("manual:todo-a",Some(1)),("manual:todo-b",Some(2)),("manual:todo-c",Some(3))] {
        state.queue.push(Trigger {key:key.into(), name:format!("orch-{key}"),cwd:f.root.to_string_lossy().into(),prompt:format!("原本文 {key}"),todo_id:id,options:json!({"model":"gpt-6-luna","effort":"high","sandbox":"read-only","isolate_worktree":false})});
    }
    let _deadline = cc_ask::tick_runtime::Deadline::new(Duration::from_secs(30));
    f.console.dispatch(&mut state,&[]).unwrap();
    assert!(state.launches.contains_key("manual:todo-a"));
    assert!(state.launches.contains_key("manual:todo-b"));
    assert_eq!(state.launches.len(),2);
    state=f.console.read_state().unwrap();
    f.console.dispatch(&mut state,&f.console.jobs().unwrap()).unwrap();
    assert!(state.launches.contains_key("manual:orphan-a"));
    assert!(state.launches.contains_key("manual:orphan-b"));
    assert_eq!(state.launches.len(),4);
    state=f.console.read_state().unwrap();
    f.console.dispatch(&mut state,&f.console.jobs().unwrap()).unwrap();
    assert!(state.launches.contains_key("manual:todo-c"));
    assert!(state.launches.contains_key("manual:orphan-c"));
    assert_eq!(f.launches(),6);
}

#[test]
fn todo2299_recovery_reuses_the_batch_and_refreshes_checkout_after_launch() {
    let f = todo1597_fixture("doing");
    fs::create_dir_all(f.root.join(".git")).unwrap();
    let db = f.console.todos().unwrap();
    let jobs = f.console.jobs().unwrap();
    let mut state = f.console.read_state().unwrap();
    let trigger = state.queue[0].clone();
    let mut cache = BTreeMap::new();
    let first = f.console.recovery_evidence_from(&trigger, Some(&db), Some(&jobs), &mut cache);
    let calls = f.fake.calls.lock().unwrap().len();
    let second = f.console.recovery_evidence_from(&trigger, Some(&db), Some(&jobs), &mut cache);
    assert_eq!(first, second);
    assert_eq!(f.fake.calls.lock().unwrap().len(), calls, "same checkout must be measured once per batch");
    state.preparation_failures.insert(trigger.key.clone(), (now() + 3600, "source_workspace_unavailable".into()));
    state.preparation_recovery.insert(trigger.key.clone(), json!({"evidence":first}));
    fs::write(f.console.paths.jobs.join("source/job.json"), "broken").unwrap();
    assert!(!f.console.recovery_ready(&mut state, &trigger, &db, &jobs, &mut cache).unwrap(), "must use the acquired batch rather than reopen all job files");
    fs::write(f.root.join("pending-change.rs"), "changed").unwrap();
    cache.clear();
    let fresh = f.console.recovery_evidence_from(&trigger, Some(&db), Some(&jobs), &mut cache);
    assert_ne!(fresh["workspace"]["dirty"], second["workspace"]["dirty"]);
}

#[test]
fn todo2299_stage_does_not_renew_start_or_clear_previous_error() {
    let f = Fixture::new();
    let path = f.console.paths.state.join("commander-health.json");
    atomic_write(&path, json!({"started_at":1,"pid":std::process::id(),"previous_error":"command_timeout"}).to_string().as_bytes()).unwrap();
    let _deadline = cc_ask::tick_runtime::Deadline::new(Duration::from_secs(1));
    f.console.patrol_stage("jobs");
    let record: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
    assert_eq!(record["started_at"], 1);
    assert_eq!(record["previous_error"], "command_timeout");
    assert_eq!(record["stage"], "jobs");
    assert!(record["finished_at"].is_null());
}

#[test]
fn todo2299_parked_candidates_do_not_consume_ready_attempts() {
    let f = Fixture::new();
    f.on();
    let mut state = f.console.read_state().unwrap();
    let original = |key: &str| Trigger { key:key.into(), name:format!("orch-{key}"), cwd:f.root.to_string_lossy().into(), prompt:format!("元の本文 {key}"), todo_id:None, options:json!({"model":"gpt-6-luna","effort":"high","sandbox":"read-only","isolate_worktree":false}) };
    for key in ["manual:a","manual:b","manual:c","manual:d","manual:e","manual:f"] {
        state.queue.push(original(key));
        state.preparation_failures.insert(key.into(), (now()+3600,"source_workspace_unavailable".into()));
    }
    for key in ["manual:ready-a","manual:ready-b","manual:ready-c"] { state.queue.push(original(key)); }
    let _deadline = cc_ask::tick_runtime::Deadline::new(Duration::from_secs(30));
    f.console.dispatch(&mut state, &[]).unwrap();
    assert_eq!(f.launches(),2);
    assert!(state.launches.contains_key("manual:ready-a"));
    assert!(state.launches.contains_key("manual:ready-b"));
    assert_eq!(state.queue[0].key,"manual:ready-c");
    f.console.dispatch(&mut state, &f.console.jobs().unwrap()).unwrap();
    assert_eq!(f.launches(),3);
    for trigger in &state.queue {
        assert_eq!(trigger.prompt,original(&trigger.key).prompt);
        assert_eq!(trigger.options,original(&trigger.key).options);
    }
}

#[test]
fn todo2299_patrol_budget_deferral_keeps_receipts_and_failure_attempts() {
    let f = Fixture::new();
    f.on();
    let mut state = f.console.read_state().unwrap();
    let trigger = Trigger { key:"manual:budget".into(), name:"orch-budget".into(), cwd:f.root.to_string_lossy().into(), prompt:"原本文".into(), todo_id:None, options:json!({"model":"gpt-6-luna","effort":"high","sandbox":"read-only","isolate_worktree":false}) };
    state.queue.push(trigger.clone());
    state.preparation_recovery.insert(trigger.key.clone(),json!({"attempts":5,"code":"tool_failed","observed_at":1}));
    state.merged_requests.insert("manual:old-pending".into(),trigger.key.clone());
    let db = f.console.todos().unwrap();
    let _deadline = cc_ask::tick_runtime::Deadline::new(Duration::from_secs(10));
    assert_eq!(f.console.dispatch(&mut state,&[]).unwrap_err().code,"patrol_deferred");
    f.console.defer_failure(&mut state,&trigger.key,"patrol_deferred",&db,&[],&mut BTreeMap::new()).unwrap();
    let saved = f.console.read_state().unwrap();
    assert_eq!(saved.preparation_recovery[&trigger.key]["attempts"],5);
    assert_eq!(saved.preparation_recovery[&trigger.key]["code"],"tool_failed");
    assert!(!saved.preparation_failures.contains_key(&trigger.key));
    assert_eq!(saved.queue[0].prompt,trigger.prompt);
    assert_eq!(saved.queue[0].options,trigger.options);
    assert_eq!(saved.merged_requests["manual:old-pending"],trigger.key);
    assert_eq!(f.launches(),0);
}

#[test]
fn todo2299_legacy_timeout_is_rechecked_but_scoped_timeout_keeps_retry() {
    let f = Fixture::new();
    let mut state = f.console.read_state().unwrap();
    let trigger = Trigger { key:"manual:timeout".into(), name:"orch-timeout".into(), cwd:f.root.to_string_lossy().into(), prompt:"原本文".into(), todo_id:None, options:Value::Null };
    state.queue.push(trigger.clone());
    state.preparation_failures.insert(trigger.key.clone(),(now()+3600,"command_timeout".into()));
    state.preparation_recovery.insert(trigger.key.clone(),json!({"attempts":3,"code":"command_timeout","observed_at":1}));
    let db = f.console.todos().unwrap();
    let mut cache = BTreeMap::new();
    assert!(f.console.recovery_ready(&mut state,&trigger,&db,&[],&mut cache).unwrap());
    assert!(!state.preparation_failures.contains_key(&trigger.key));
    assert_eq!(state.preparation_recovery[&trigger.key]["legacy_timeout"]["attempts"],3);
    f.console.defer_failure(&mut state,&trigger.key,"command_timeout",&db,&[],&mut cache).unwrap();
    assert_eq!(state.preparation_recovery[&trigger.key]["timeout_scope"],"command");
    assert!(!f.console.recovery_ready(&mut state,&trigger,&db,&[],&mut cache).unwrap());
}
#[test]
fn todo2299_legacy_metadata_reconciles_before_capacity_without_checkout_probe() {
    let f = Fixture::new(); f.on();
    let mut source = model_source(&f);
    source["todo_id"] = Value::Null; // Older source has one explicit linked receipt.
    fs::write(f.console.paths.jobs.join("source/job.json"),source.to_string()).unwrap();
    fs::create_dir(f.root.join(".git")).unwrap();
    *f.fake.stall_allow.lock().unwrap() = false;
    let mut state = f.console.read_state().unwrap();
    for (key, scope) in [("decision:source", Value::Null), ("manual:real-timeout", json!("command")), ("decision:missing", Value::Null)] {
        state.queue.push(Trigger { key:key.into(), name:"orch-test".into(), cwd:f.root.to_string_lossy().into(), prompt:"原本文".into(), todo_id:None, options:Value::Null });
        state.preparation_failures.insert(key.into(), (now()+3600,"command_timeout".into()));
        state.preparation_recovery.insert(key.into(),json!({"attempts":3,"code":"command_timeout","observed_at":1,"timeout_scope":scope}));
    }
    state.merged_requests.insert("manual:original".into(), "decision:source".into());
    let before = state.queue.iter().map(|t| (t.key.clone(),t.prompt.clone(),t.options.clone())).collect::<Vec<_>>();
    let _deadline = cc_ask::tick_runtime::Deadline::new(Duration::from_secs(30));
    f.console.dispatch(&mut state,&f.console.jobs().unwrap()).unwrap();
    assert!(!state.preparation_failures.contains_key("decision:source"));
    assert_eq!(state.preparation_recovery["decision:source"]["legacy_timeout"]["attempts"],3);
    assert!(state.preparation_failures.contains_key("manual:real-timeout"));
    assert!(state.preparation_failures.contains_key("decision:missing"));
    assert_eq!(state.queue.iter().map(|t| (t.key.clone(),t.prompt.clone(),t.options.clone())).collect::<Vec<_>>(),before);
    assert_eq!(state.merged_requests["manual:original"],"decision:source");
    assert!(f.fake.calls.lock().unwrap().is_empty(), "metadata repair must not probe Git or launch");
    assert!(!f.console.read_state().unwrap().preparation_failures.contains_key("decision:source"));
}

#[tokio::test]
async fn todo2299_artifact_intake_uses_same_receipt_and_rejects_foreign_scope() {
    for case in ["receipt", "related", "foreign", "ancestor", "unrelated", "other-todo", "no-source", "typed-source", "no-receipt", "excluded", "done", "block", "wait"] {
        let f = Fixture::new(); f.on();
        let base = f.root.join("registered"); let artifact = f.root.join("artifact");
        fs::create_dir(&base).unwrap(); fs::create_dir(&artifact).unwrap();
        fs::write(artifact.join("retained.txt"), "original artifact").unwrap();
        f.job("source","work","succeeded","状態: 継続中","");
        let cwd = if case == "ancestor" {f.root.clone()} else {artifact.clone()};
        let mut source=json!({"id":"source","status":"succeeded","name":"work","cwd":cwd,"todo_id":1,"model":"gpt-6-luna","effort":"high","sandbox":"read-only"});
        if case == "related" {source["todo_id"]=json!(2);source["related_todo_ids"]=json!([1]);}
        if matches!(case,"unrelated"|"other-todo") {source["todo_id"]=json!(2);}
        if case == "foreign" {source["cwd"]=json!(base);}
        fs::write(f.console.paths.jobs.join("source/job.json"),source.to_string()).unwrap();
        let mut db=f.console.todos().unwrap();db["projects"]["demo"]=json!(base);db["items"][0]["jobs"]=json!(["source"]);
        if case == "no-receipt" {db["items"][0]["jobs"]=json!([]);}
        if matches!(case,"done"|"block"|"wait") {db["items"][0]["status"]=json!(case);}
        if case == "excluded" {db["projects"]["excluded-project"]=json!(artifact);fs::write(f.console.paths.root.join("excluded-projects.txt"),"excluded-project\n").unwrap();}
        fs::write(f.console.paths.todo.join("todo.json"),db.to_string()).unwrap();
        let mut body=json!({"id":1,"request_id":"original-artifact","prompt":"独立作業だけ続行。回答待ち操作は保留","cwd":cwd,"source_job_id":"source","model":"gpt-6-luna","effort":"high","sandbox":"read-only","isolate_worktree":false});
        if case == "no-source" {body["source_job_id"]=Value::Null;}
        if case == "typed-source" {body["source_job_id"]=json!(1);}
        let (status,result)=f.http(Method::POST,"delegate",&body.to_string()).await;
        if matches!(case,"receipt"|"related") {
            assert_eq!(status,StatusCode::OK,"{case}: {result}"); assert_eq!(result["data"]["accepted"],true);
            let accepted=f.console.read_state().unwrap();let key="manual:original-artifact";
            assert_eq!(accepted.request_cwds[key],cwd.to_string_lossy());
            for option in ["source_job_id","model","effort","sandbox","isolate_worktree"] {assert_eq!(accepted.request_options[key][option],body[option],"{option}");}
            assert!(accepted.queue.iter().find(|t|t.key==key).unwrap().prompt.contains(body["prompt"].as_str().unwrap()));
            assert_eq!(f.launches(),0,"intake alone must never start work");
            let mut state=accepted;
            f.console.dispatch(&mut state,&f.console.jobs().unwrap()).unwrap();
            assert_eq!(f.launches(),1,"{case}: same receipt must reach launch");
            let call=model_run(&f);
            assert_eq!(model_arg(&call,"--cwd").as_deref(),Some(cwd.to_string_lossy().as_ref()));
            assert_eq!(model_arg(&call,"--todo-id").as_deref(),Some("1"));
            assert_eq!(model_arg(&call,"--sandbox").as_deref(),Some("read-only"));
            assert_eq!(state.request_options[key]["source_job_id"],body["source_job_id"]);
            assert!(f.console.todos().unwrap()["items"][0]["jobs"].as_array().unwrap().contains(&json!(state.launches[key])));
        } else {
            assert_eq!(status,StatusCode::BAD_REQUEST,"{case}: {result}");
            assert!(!f.console.read_state().unwrap().fingerprints.contains_key("manual:original-artifact"));
        }
        assert_eq!(f.launches(),usize::from(matches!(case,"receipt"|"related")),"invalid intake must never start work");
        assert_eq!(fs::read_to_string(artifact.join("retained.txt")).unwrap(),"original artifact");
    }
}

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
