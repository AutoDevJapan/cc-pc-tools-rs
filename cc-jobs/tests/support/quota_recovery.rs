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
        "finite_review":{"todo_id":2,"spec":spec,"launch_key":"11111111111111111111111111111111",
            "spec_sha256":cc_job_policy::product_retry::digest(&fs::read(&spec).unwrap())}}));
    t.put(json!({"id":"author","status":"succeeded","model":"gpt-6.1-sol","cwd":t.ctx.root.join("author")}));
    let target = json!({"model":"gpt-6-luna","model_origin":"policy","effort":"max","cwd":t.ctx.root,
        "launch_key":row["scheduler"]["key"],"task_type":"review","sandbox":"read-only","network":false,
        "mcp_profile":"minimal","approval_policy":"never","approvals_reviewer":"user","add_dirs":[]});
    let proof =
        cc_job_policy::quota_recovery::receipt(&row, &source, &target, &health, at).unwrap();
    row["scheduler"]["selection"] = target;
    row["scheduler"]["selection"]["finite_review_spec_sha256"] = json!(
        cc_job_policy::product_retry::digest(&fs::read(&spec).unwrap())
    );
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

#[test]
fn quota_status_reads_one_job_without_returning_bodies_paths_or_account_metadata() {
    let (t, _, mut source) = setup();
    source["last_error"] = json!("PRIVATE_ERROR_BODY");
    source["last_message"] = json!("PRIVATE_RESPONSE");
    source["capacity_inventory"] = json!({"account_id":"PRIVATE_ACCOUNT"});
    t.put(source.clone());
    let (code, output, _) = t.cli(&["status", "--job-id", "quota-reader", "--json"]);
    assert_eq!(code.unwrap(), 0);
    let metadata: Value = serde_json::from_str(&output).unwrap();
    assert_eq!(metadata["id"], "quota-reader");
    assert_eq!(metadata["spec_sha256"], source["finite_review"]["spec_sha256"]);
    assert!(metadata["metadata_only"] == true);
    assert!(!output.contains("PRIVATE_"));
    assert!(!output.contains(&t.ctx.root.to_string_lossy().to_string()));
    assert!(t.cli(&["status", "--job-id", "../other", "--json"]).0.is_err());
    assert!(t.cli(&["status", "--job-id", "quota-reader"]).0.is_err());
    assert!(t.cli(&["status", "--job-id", "quota-reader", "--json", "--all"]).0.is_err());
}
fn register(t: &Test) -> Result<i32> {
    let prompt = format!(
        "Scheduler prompt\n依頼:\n{}",
        fs::read_to_string(t.ctx.root.join("review-spec.md")).unwrap()
    );
    register_prompt(t, prompt.as_bytes())
}
fn register_prompt(t: &Test, prompt: &[u8]) -> Result<i32> {
    register_prompt_model(t,prompt,"gpt-6-luna")
}
fn register_prompt_model(t: &Test, prompt: &[u8],model:&str) -> Result<i32> {
    t.run(
        &[
            "--todo-id",
            "2",
            "--launch-key",
            "22222222222222222222222222222222",
            "--model",
            model,
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
        prompt,
    )
}

#[test]
fn ordinary_review_registration_binds_the_executed_spec_before_any_recovery() {
    let (t, mut row, _) = setup();
    row["scheduler"]["selection"]
        .as_object_mut()
        .unwrap()
        .remove("quota_recovery");
    save(&t, &row);
    register(&t).unwrap();
    let registered = child(&t);
    assert!(registered["quota_recovery"].is_null());
    assert_eq!(
        registered["finite_review"]["spec_sha256"],
        cc_job_policy::product_retry::digest(&fs::read(row["spec"].as_str().unwrap()).unwrap())
    );
}

#[test]
fn changed_spec_between_scheduler_prompt_and_registration_is_rejected() {
    let (t, mut row, _) = setup();
    row["scheduler"]["selection"]
        .as_object_mut()
        .unwrap()
        .remove("quota_recovery");
    fs::write(
        row["spec"].as_str().unwrap(),
        "Changed before ordinary registration",
    )
    .unwrap();
    save(&t, &row);
    assert!(register(&t).is_err());
    assert_eq!(
        t.ctx
            .list_jobs(&HashSet::new())
            .iter()
            .filter(|j| j["todo_id"] == 2)
            .count(),
        1
    );
}

#[test]
fn registration_rejects_a_prompt_without_the_prepared_spec_body() {
    let (t, mut row, _) = setup();
    row["scheduler"]["selection"]
        .as_object_mut()
        .unwrap()
        .remove("quota_recovery");
    save(&t, &row);
    assert!(register_prompt(&t, b"Different review instructions").is_err());
    assert_eq!(
        t.ctx
            .list_jobs(&HashSet::new())
            .iter()
            .filter(|j| j["todo_id"] == 2)
            .count(),
        1
    );
}
fn child(t: &Test) -> Value {
    t.ctx
        .list_jobs(&HashSet::new())
        .into_iter()
        .find(|j| j["todo_id"] == 2 && j["launch_key"] == "22222222222222222222222222222222")
        .unwrap()
}

#[test]
fn grok_existing_research_is_admitted_through_registration_start_and_final_queue_scope() {
    research_admission(false, "normal");
}

#[test]
fn gpt_quota_research_preserves_its_selected_route_and_budget_through_final_queue_admission() {
    for case in ["normal", "capacity_same", "capacity_switch", "capacity_anthropic", "capacity_wait", "selected_unknown", "budget_changed", "history_changed", "source_changed"] {
        research_admission(true, case);
    }
}

#[test]
fn quota_metadata_episodes_reset_after_verified_admission_without_resetting_native_budget() {
    research_admission(true,"capacity_metadata");
}

#[test]
fn legacy_claude_reentry_checks_its_current_health_after_the_old_provider_recovers() {
    let (t,mut row,mut source)=setup();let at=t.rt.epoch();
    source["model"]=json!("gpt-6-luna");t.put(source.clone());
    row["model"]=json!("anthropic/claude-opus-5-5");
    let health=json!({"at":at,"openai":{"at":at,"verified":true,"exhausted":true,"available":false},
        "anthropic":{"at":at,"verified":true,"exhausted":false,"available":true}});
    *t.rt.quota_recovery_inventory.lock().unwrap()=health.clone();
    let mut target=row["scheduler"]["selection"].clone();target["model"]=row["model"].clone();
    let mut old=cc_job_policy::quota_recovery::receipt(&row,&source,&target,&health,at).unwrap();
    // Synthetic historical v1 serialization, never a production record edit.
    old["version"]=json!(1);old.as_object_mut().unwrap().remove("retry");old.as_object_mut().unwrap().remove("retry_source_sha256");
    old["health"]["openai"].as_object_mut().unwrap().remove("safe_routes");
    target["quota_recovery"]=old.clone();row["scheduler"]["selection"]=target;save(&t,&row);
    let prompt=format!("Scheduler prompt\n依頼:\n{}",fs::read_to_string(t.ctx.root.join("review-spec.md")).unwrap());
    register_prompt_model(&t,prompt.as_bytes(),"anthropic/claude-opus-5-5").unwrap();let j=child(&t);
    let slot=t.ctx.admit(s(&j,"id")).unwrap();assert!(slot.is_some());drop(slot);
    let mut next=t.ctx.load(s(&j,"id")).unwrap();cc_jobs::queue::prepare_resume(&mut next,"",&t.rt.now());t.put(next);
    let now=at+31.;*t.rt.epoch_base.lock().unwrap()=Some(now);
    {let mut fresh=t.rt.quota_recovery_inventory.lock().unwrap();fresh["at"]=json!(now);fresh["openai"]=json!({"at":now,"verified":true,"available":true,"exhausted":false});fresh["anthropic"]["at"]=json!(now);}
    let slot=t.ctx.admit(s(&j,"id")).unwrap();assert!(slot.is_some());drop(slot);
    let result=t.ctx.load(s(&j,"id")).unwrap();assert_eq!(result["quota_recovery"],old);assert_eq!(result["capacity_retry_count"],1);
    assert_eq!(t.ctx.load("quota-reader").unwrap(),source);
}

fn research_admission(gpt: bool, case: &str) {
    use cc_job_policy::subwork;
    let (t, mut row, mut source) = setup();
    let ordinary=case.starts_with("ordinary_");
    let continuation=case.starts_with("continuation_");
    let owner_model=case.ends_with("owner_model");
    let quota_dependency=case.contains("quota_dependency");
    let mut f = subwork_support::Fixture::new();
    f.register();
    f.db["items"][0]["status"] = json!("wait");
    f.jobs[0]["status"] = json!("succeeded");
    f.jobs[0]["subwork_live"] = json!(false);
    if continuation || ordinary {
        f.jobs[0]["todo_resume_generation"]=json!(1);
        f.db["items"][0]["resume_generation"]=json!(1);
        f.db["items"][0]["worker_receipt"]=json!({"job":"parent","generation":1});
    }
    t.put(f.jobs[0].clone());
    let mut relation = f.db["items"][1]["subwork"].clone();
    relation["kind"] = json!("research");
    row["subwork"] = relation.clone();
    row["project"] = json!("alias");
    row["task_type"] = json!("research");
    row["model"] = json!("gpt-6.1-sol");
    row["scheduler"]["worktree"] = f.db["items"][1]["scheduler"]["worktree"].clone();
    row["scheduler"]["branch"] = f.db["items"][1]["scheduler"]["branch"].clone();
    source["model"] = json!(if gpt {"gpt-6.1-sol"} else {"grok-4.7"});
    source["task_type"] = json!("research");
    source["subwork"] = relation.clone();
    source["cwd"] = row["scheduler"]["worktree"].clone();
    source["finite_subwork"] = source["finite_review"].take();
    source["subwork_spec"] = row["spec"].clone();
    if gpt {
        source["capacity_same_model_retry_count"] = json!(1);
        source["capacity_retry_count"] = json!(0);
        source["capacity_wait_count"] = json!(1);
        source["capacity_account_selector"] = json!("p000001");
        source["capacity_tried_accounts"] = json!(["p000001"]);
        source["capacity_history"] = json!([{"kind":"same_model_account_retry","account_selector":"p000001"}]);
        let mut health = t.rt.quota_recovery_inventory.lock().unwrap();
        health["openai"]["safe_routes"] = json!([
            {"selector":"p000002","aliases":["p000002"],"at":t.rt.epoch(),"verified":true},
            {"selector":"p000003","aliases":["p000003"],"at":t.rt.epoch(),"verified":true}
        ]);
    }
    if case=="continuation_no_retry" {for key in cc_job_policy::subwork_continuation::RETRY_FIELDS {source.as_object_mut().unwrap().remove(key);}source.as_object_mut().unwrap().remove("capacity_account_selector");}
    if case.starts_with("continuation_legacy") {
        // v1's already restored provider switch remains spent in the new contract.
        source["quota_recovery"]=json!({"version":1,"source_job":"previous-quota","model":"gpt-6.1-sol"});
        source["quota_retry_seeded_version"]=json!(1);
        source["capacity_retry_count"]=json!(1);
        source["capacity_history"].as_array_mut().unwrap().push(json!({"kind":"provider_switch",
            "from_model":"anthropic/claude-opus-5-5","to_model":"gpt-6.1-sol",
            "failure_kind":"quota","source_job":"previous-quota"}));
    }
    t.put(source.clone());
    let mut target = row["scheduler"]["selection"].clone();
    target["cwd"] = source["cwd"].clone();
    target["model"] = row["model"].clone();
    target["task_type"] = json!("research");
    if gpt { target["capacity_account_selector"] = json!("p000002"); }
    if continuation {
        row["resume_generation"]=json!(3);row["dependency_generation"]=json!(1);
        source["todo_resume_generation"]=json!(2);
        if case.starts_with("continuation_result_") {
            source["work_owner"] = cc_job_policy::work_owner::checkout(s(&source, "cwd"));
            let status = case.strip_prefix("continuation_result_").unwrap();
            source["status"] = json!(if quota_dependency {"failed"} else if status == "reset" { "lost" } else { status });
            source["todo_resume_generation"] = json!(3);
            source["todo_dependency_generation"] = json!(if quota_dependency {0} else {1});
            row["worker_receipt"] = json!({"job":source["id"],"generation":3});
            if status == "cancelled" { source["recovery_cancel"] = json!({"key":"recovery-source","committed_at":"2026-09-28T12:01:00Z"}); }
        }
        if case == "continuation_old_request" {
            row["worker_receipt"] = json!({"job":source["id"],"generation":3});
        }
        if case == "continuation_result_quota_dependency_non_openai" {
            source["model"] = json!("anthropic/claude-opus-5-5");
            source["capacity_same_model_retry_count"] = json!(3);
            source["capacity_retry_count"] = json!(1);
            source["capacity_history"].as_array_mut().unwrap().push(json!({"kind":"provider_switch","from_model":"gpt-6.1-sol","to_model":"anthropic/claude-opus-5-5"}));
        }
        if owner_model {
            row["model"] = json!("anthropic/claude-opus-5-5"); row["model_origin"] = json!("owner_request");
            target["model"] = row["model"].clone(); target["model_origin"] = row["model_origin"].clone();
            target["capacity_account_selector"] = Value::Null;
            t.rt.quota_recovery_inventory.lock().unwrap()["anthropic"] = json!({"at":t.rt.epoch(),"verified":true,"available":true,"exhausted":false});
        }
        t.put(source.clone());
        target["effort_origin"]=json!("policy");
        target.as_object_mut().unwrap().remove("quota_recovery");
        let actor=&f.jobs[0];
        let mut request=cc_job_policy::subwork_continuation::request(&row,&source,actor).unwrap();
        if case == "continuation_old_request" { request.as_object_mut().unwrap().remove("source_receipt"); }
        target["subwork_continuation"]=cc_job_policy::subwork_continuation::receipt(&request,&row,&source,actor,&target,
            &t.rt.quota_recovery_inventory.lock().unwrap(),t.rt.epoch()).unwrap();
        row["subwork_continuation"]=request;
    } else if ordinary {
        let mut previous = source.clone(); previous["id"] = json!("earlier-quota"); previous["todo_resume_generation"] = json!(2);
        let mut old_row = row.clone(); old_row["resume_generation"] = json!(3); old_row["dependency_generation"] = json!(1);
        old_row["jobs"] = json!([previous["id"]]); old_row["scheduler"]["last_job"] = previous["id"].clone();
        row["subwork_continuation"] = cc_job_policy::subwork_continuation::request(&old_row,&previous,&f.jobs[0]).unwrap();
        t.put(previous); row["jobs"] = json!(["earlier-quota",source["id"]]);
        row["resume_generation"] = json!(5); row["dependency_generation"] = json!(1);
        row["worker_receipt"] = json!({"job":source["id"],"generation":5});
        source["todo_resume_generation"] = json!(5); source["todo_dependency_generation"] = json!(1);
        source["status"] = json!(if case == "ordinary_lost" {"lost"} else {"succeeded"});
        t.put(source.clone()); target.as_object_mut().unwrap().remove("quota_recovery");
    } else {
    target["quota_recovery"] = cc_job_policy::quota_recovery::receipt(
        &row,
        &source,
        &target,
        &t.rt.quota_recovery_inventory.lock().unwrap(),
        t.rt.epoch(),
    )
    .unwrap();
    }
    row["scheduler"]["selection"] = target;
    row["scheduler"]["subwork_admission"] = subwork::admission(&row);
    f.db["items"][1] = row.clone();
    let save = |db: &Value| fs::write(t.ctx.root.join("todo.json"), db.to_string()).unwrap();
    save(&f.db);
    let prompt = format!(
        "Scheduler prompt\n依頼:\n{}",
        fs::read_to_string(row["spec"].as_str().unwrap()).unwrap()
    );
    let mut args = vec![
            "--todo-id",
            "2",
            "--launch-key",
            s(&row["scheduler"], "key"),
            "--subwork-of",
            "1",
            "--assigned-needs",
            "",
            "--cwd",
            s(&source, "cwd"),
            "--model",
            s(&row,"model"),
            "--model-origin",
            s(&row,"model_origin"),
            "--effort",
            "max",
            "--task-type",
            "research",
            "--sandbox",
            "read-only",
            "--mcp-profile",
            "minimal",
        ];
    if !gpt { args.extend(["--model-recovery-from", "grok-4.7"]); }
    if continuation {args.extend(["--effort-origin","policy"]);}
    if case=="continuation_parent_before_registration" {
        f.db["items"][0]["resume_generation"]=json!(2);
        f.db["items"][0]["worker_receipt"]["generation"]=json!(2);
        save(&f.db);
        assert!(t.run(&args,prompt.as_bytes()).is_err(),"old parent contract registered a new worker");
        assert_eq!(t.ctx.list_jobs(&HashSet::new()).into_iter().filter(|j|j["todo_id"]==2).count(),1);
        return;
    }
    t.run(&args, prompt.as_bytes()).unwrap();
    let j = child(&t);
    assert_eq!(j["subwork"], source["subwork"]);
    assert_eq!(
        j["finite_subwork"]["spec_sha256"],
        source["finite_subwork"]["spec_sha256"]
    );
    assert!(j["parent_job_id"].is_null());
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
    if continuation {
        let non_openai=case.ends_with("non_openai");
        assert_eq!(j["capacity_same_model_retry_count"],if quota_dependency && !owner_model && !non_openai {json!(2)} else {source["capacity_same_model_retry_count"].clone()});
        assert_eq!(j["capacity_retry_count"],if non_openai {json!(2)} else {source["capacity_retry_count"].clone()});
        assert_eq!(j["capacity_wait_count"],source["capacity_wait_count"]);
        if quota_dependency && !owner_model { assert_eq!(j["capacity_history"].as_array().unwrap().len(), source["capacity_history"].as_array().unwrap().len()+1); }
        else { assert_eq!(j["capacity_history"],source["capacity_history"]); }
        assert!(j["quota_recovery"].is_null());assert!(j["subwork_continuation"].is_object());
        match case {
            "continuation_health"=>t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["verified"]=json!(false),
            "continuation_selected_unknown"=>t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["safe_routes"][0]["verified"]=json!(false),
            "continuation_source"=>{let mut changed=source.clone();changed["finite_subwork"]["spec_sha256"]=json!("wrong");t.put(changed);},
            "continuation_generation"=>{db["items"][1]["resume_generation"]=json!(5);fs::write(&paths.store,db.to_string()).unwrap();},
            "continuation_stop"=>{db["items"][1]["owner_stopped"]=json!(true);fs::write(&paths.store,db.to_string()).unwrap();},
            "continuation_scope"=>{let mut changed=j.clone();changed["network"]=json!(true);t.put(changed);},
            "continuation_legacy_reset"=>{let mut changed=j.clone();changed["capacity_retry_count"]=json!(0);t.put(changed);},
            "continuation_result_reset"=>{let mut changed=j.clone();changed["capacity_same_model_retry_count"]=json!(0);t.put(changed);},
            "continuation_result_quota_dependency_reset"=>{let mut changed=j.clone();changed["capacity_same_model_retry_count"]=json!(0);t.put(changed);},
            "continuation_parent_generation"=>{
                db["items"][0]["resume_generation"]=json!(2);
                db["items"][0]["worker_receipt"]["generation"]=json!(2);
                fs::write(&paths.store,db.to_string()).unwrap();
            },
            _=>(),
        }
    } else if ordinary {
        assert!(j["subwork_continuation"].is_null()); assert!(j["quota_recovery"].is_null());
        if case == "ordinary_parent_changed" { db["items"][0]["scheduler"]["job"]=Value::Null; fs::write(&paths.store,db.to_string()).unwrap(); }
    } else if gpt {
        assert_eq!(j["capacity_same_model_retry_count"], 2);
        assert_eq!(j["capacity_wait_count"], 1);
        assert_eq!(j["capacity_history"].as_array().unwrap().len(), 2);
        assert_eq!(j["capacity_account_selector"], "p000002");
        let native = codex_args(&j);
        assert!(native.windows(2).any(|args| args == ["-m", "p000002/gpt-6.1-sol"]));
        match case {
            "selected_unknown" => t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["safe_routes"][0]["verified"] = json!(false),
            "source_changed" => {
                let mut changed = source.clone(); changed["capacity_wait_count"] = json!(2); t.put(changed);
            }
            "budget_changed" | "history_changed" => {
                let mut changed = j.clone();
                if case == "budget_changed" {changed["capacity_same_model_retry_count"] = json!(0);}
                else {changed["capacity_history"] = json!([]);}
                t.put(changed);
            }
            _ => (),
        }
    }
    if case=="capacity_metadata" {
        let at=t.rt.epoch();t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["available"]=json!(false);
        assert!(t.ctx.admit(s(&j,"id")).unwrap().is_none());
        assert_eq!(t.ctx.load(s(&j,"id")).unwrap()["quota_recovery_recheck_count"],1);
        *t.rt.epoch_base.lock().unwrap()=Some(at+901.);
        let mut health=t.rt.quota_recovery_inventory.lock().unwrap();let now=t.rt.epoch();
        health["at"]=json!(now);health["openai"]["at"]=json!(now);health["openai"]["available"]=json!(true);
        for route in health["openai"]["safe_routes"].as_array_mut().unwrap(){route["at"]=json!(now);}
    }
    let slot = t.ctx.admit(s(&j, "id"));
    if !ordinary && !["normal","continuation_normal","continuation_legacy","continuation_old_request","continuation_no_retry","continuation_result_succeeded","continuation_result_lost","continuation_result_cancelled","continuation_result_quota_dependency","continuation_result_quota_dependency_owner_model","continuation_result_quota_dependency_non_openai","capacity_same","capacity_switch","capacity_anthropic","capacity_metadata","capacity_wait"].contains(&case) {
        assert!(slot.is_err() || slot.unwrap().is_none(), "{case}");
        assert_ne!(t.ctx.load(s(&j, "id")).unwrap()["status"], "running");
        return;
    }
    let slot = slot.unwrap();
    assert!(slot.is_some());
    let admitted = t.ctx.load(s(&j, "id")).unwrap();
    assert_eq!(admitted["status"], "running");
    assert_eq!(admitted["model"], row["model"]);
    assert_eq!(admitted["subwork"], source["subwork"]);
    assert_eq!(admitted["approval_policy"], source["approval_policy"]);
    assert_eq!(admitted["subwork_spec"], source["subwork_spec"]);
    assert_eq!(t.ctx.load("quota-reader").unwrap(), source);
    drop(slot);
    if case.starts_with("capacity_") {
        let mut current=t.ctx.load(s(&j,"id")).unwrap();
        let cycles=if case=="capacity_anthropic" {3}else if case=="capacity_switch" {2}else{1};
        for step in 0..cycles {
            let at=t.rt.epoch();
            current["status"]=json!("failed");current["last_failure_kind"]=json!("capacity");
            current["last_error"]=json!("Selected model is at capacity");
            current["capacity_inventory"]=json!({"at":at,"anthropic":true,"anthropic_at":at,"openai":true,"openai_at":at,
                "pool":{"at":at,"verified":case!="capacity_wait","eligible":["p000003"],"failed":["p000002"]}});
            assert!(t.ctx.retry_capacity(&mut current));
            if case=="capacity_wait" {
                assert_eq!(current["capacity_wait_pending"],true);
                *t.rt.epoch_base.lock().unwrap()=Some(at+901.);
                current["capacity_inventory"]["pool"]["at"]=json!(t.rt.epoch());
                current["capacity_inventory"]["pool"]["verified"]=json!(true);
                assert!(t.ctx.recheck_capacity_wait(&mut current));
            }
            let next=t.rt.epoch()+31.;*t.rt.epoch_base.lock().unwrap()=Some(next);
            {
                let mut health=t.rt.quota_recovery_inventory.lock().unwrap();health["at"]=json!(next);
                for p in ["openai","anthropic"] {health[p]["at"]=json!(next);}
                health["anthropic"]["available"]=json!(true);health["anthropic"]["exhausted"]=json!(false);
                for route in health["openai"]["safe_routes"].as_array_mut().unwrap(){route["at"]=json!(next);}
            }
            t.put(current.clone());
            if case=="capacity_metadata" {
                assert!(current["quota_recovery_recheck_count"].is_null());
                for expected in 1..=2 {
                    t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["available"]=json!(false);
                    assert!(t.ctx.admit(s(&j,"id")).unwrap().is_none());
                    let waiting=t.ctx.load(s(&j,"id")).unwrap();assert_eq!(waiting["status"],"queued");
                    assert_eq!(waiting["quota_recovery_recheck_count"],expected);
                    let now=t.rt.epoch()+901.;*t.rt.epoch_base.lock().unwrap()=Some(now);
                    let mut health=t.rt.quota_recovery_inventory.lock().unwrap();health["at"]=json!(now);health["openai"]["at"]=json!(now);
                    for route in health["openai"]["safe_routes"].as_array_mut().unwrap(){route["at"]=json!(now);}
                }
                t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["available"]=json!(true);
            }
            let slot=t.ctx.admit(s(&j,"id")).unwrap();assert!(slot.is_some(),"{case} step {step}: {}",t.ctx.load(s(&j,"id")).unwrap());drop(slot);
            current=t.ctx.load(s(&j,"id")).unwrap();
            assert_eq!(current["status"],"running");
        }
        assert_eq!(current["capacity_same_model_retry_count"],3);
        if case=="capacity_switch" {assert!(s(&current,"model").starts_with("anthropic/"));assert_eq!(current["capacity_retry_count"],1);}
        if case=="capacity_anthropic" {assert_eq!(current["model"],"anthropic/claude-opus-5-5");assert_eq!(current["capacity_retry_count"],2);}
        if case=="capacity_metadata" {assert!(current["quota_recovery_recheck_count"].is_null());assert_eq!(current["quota_recovery_recheck_total"],3);}
        assert_eq!(t.ctx.load("quota-reader").unwrap(),source);
    }
}

#[test]
fn dependency_continuation_registers_links_and_rechecks_before_real_queue_admission() {
    for case in ["continuation_normal","continuation_legacy","continuation_old_request","continuation_legacy_reset","continuation_no_retry","continuation_health","continuation_selected_unknown","continuation_source","continuation_generation","continuation_parent_generation","continuation_parent_before_registration","continuation_stop","continuation_scope"] {
        research_admission(true,case);
    }
}

#[test]
fn current_finished_result_keeps_its_budget_through_registration_start_and_final_queue() {
    for case in ["continuation_result_succeeded", "continuation_result_lost", "continuation_result_cancelled", "continuation_result_reset", "continuation_result_quota_dependency", "continuation_result_quota_dependency_reset","continuation_result_quota_dependency_owner_model","continuation_result_quota_dependency_non_openai"] {
        research_admission(true, case);
    }
}

#[test]
fn ordinary_current_result_ignores_stale_contract_through_registration_start_and_final_queue() {
    for case in ["ordinary_succeeded","ordinary_lost","ordinary_parent_changed"] {
        research_admission(true,case);
    }
}

#[test]
fn quota_registration_rejects_a_different_actual_model_before_creating_a_worker() {
    let (t,_,_)=setup();
    let prompt=format!("Scheduler prompt\n依頼:\n{}",fs::read_to_string(t.ctx.root.join("review-spec.md")).unwrap());
    assert!(register_prompt_model(&t,prompt.as_bytes(),"gpt-6-astra").is_err());
    assert_eq!(t.ctx.list_jobs(&HashSet::new()).into_iter().filter(|j|j["todo_id"]==2).count(),1);
}

#[test]
fn registered_legacy_v1_capsule_keeps_its_proof_and_budget_after_capacity_retry() {
    let (t,row,source)=setup();
    let now=t.rt.epoch();
    t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["safe_routes"]=json!([
        {"selector":"p000002","aliases":["p000002"],"at":now,"verified":true}]);
    register(&t).unwrap();let mut job=child(&t);
    assert_eq!(job["quota_recovery"]["version"],1);
    for key in ["quota_retry_seeded_version","capacity_retry_count","capacity_same_model_retry_count","capacity_history","capacity_tried_accounts","requested_model"] {job.as_object_mut().unwrap().remove(key);}
    t.put(job.clone());
    let first=t.ctx.admit(s(&job,"id")).unwrap();assert!(first.is_some());drop(first);
    let mut next=t.ctx.load(s(&job,"id")).unwrap();
    assert_eq!(next["capacity_retry_count"],1);
    next["status"]=json!("failed");next["last_failure_kind"]=json!("capacity");next["last_error"]=json!("Selected model is at capacity");
    next["capacity_inventory"]=json!({"at":now,"pool":{"at":now,"verified":true,"eligible":["p000002"],"failed":["p000001"]}});
    assert!(t.ctx.retry_capacity(&mut next));
    *t.rt.epoch_base.lock().unwrap()=Some(now+31.);
    t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["at"]=json!(now+31.);
    t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["safe_routes"][0]["at"]=json!(now+31.);
    t.put(next);
    let slot=t.ctx.admit(s(&job,"id")).unwrap();assert!(slot.is_some());drop(slot);
    let final_job=t.ctx.load(s(&job,"id")).unwrap();
    assert_eq!(final_job["quota_recovery"],job["quota_recovery"]);
    assert_eq!(final_job["capacity_retry_count"],1);assert_eq!(final_job["capacity_same_model_retry_count"],1);
    assert_eq!(t.ctx.load("quota-reader").unwrap(),source);assert_eq!(row["resume_generation"],0);
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
