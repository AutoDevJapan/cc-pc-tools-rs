use super::*;

#[test]
fn grok_quota_selects_a_healthy_independent_model_for_existing_subwork() {
    let (mut f, mut sub) = grok_fixture();
    f.e.backend.records[1]["model"] = json!("grok-4.7");
    sub.db["items"][1]["model"] = json!("grok-4.7");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let selection = &prepared["scheduler"]["selection"];
    assert_eq!(selection["model"], "gpt-6-astra");
    assert_eq!(selection["review_excluded_models"], json!(["gpt-6.1-sol"]));
    assert!(selection["quota_recovery"].is_object());
    assert!(prepared["scheduler"]["resume_session"].is_null());
    assert_eq!(prepared["subwork"], sub.db["items"][1]["subwork"]);
    assert_eq!(prepared["spec"], sub.db["items"][1]["spec"]);
    assert_eq!(f.e.backend.records[1]["model"], "grok-4.7");
}

#[test]
fn grok_legacy_error_is_read_without_rewriting_history_or_backfilling_research_sha() {
    for research in [false, true] {
        let (mut f, mut sub) = grok_fixture();
        let source = &mut f.e.backend.records[1];
        source["model"] = json!("grok-4.7");
        source["last_failure_kind"] = json!("transient");
        source["last_error"] = json!(
            "Error running remote compact task: unexpected status 402 Payment Required: Grok Build usage balance exhausted"
        );
        sub.db["items"][1]["model"] = json!("grok-4.7");
        if research {
            sub.db["items"][1]["task_type"] = json!("research");
            sub.db["items"][1]["subwork"]["kind"] = json!("research");
            source["subwork"] = sub.db["items"][1]["subwork"].clone();
            source["task_type"] = json!("research");
            source.as_object_mut().unwrap().remove("finite_review");
        }
        let original = source.clone();
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let prepared =
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .unwrap()
                .unwrap();
        assert_eq!(f.e.backend.records[1], original);
        assert_eq!(
            prepared["scheduler"]["selection"]["quota_recovery"].is_object(),
            !research
        );
        if research {
            assert_eq!(prepared["scheduler"]["selection"]["model"], "grok-4.7");
            assert!(prepared["scheduler"]["selection"]["finite_review_spec_sha256"].is_string());
        }
    }
}

#[test]
fn grok_research_recovery_retains_scope_parent_and_kind_after_parent_exit() {
    let (mut f, mut sub) = grok_fixture();
    let row = &mut sub.db["items"][1];
    row["subwork"]["kind"] = json!("research");
    row["task_type"] = json!("research");
    row["model"] = json!("grok-4.7");
    let source = &mut f.e.backend.records[1];
    source["model"] = json!("grok-4.7");
    source["subwork"] = row["subwork"].clone();
    source["task_type"] = json!("research");
    source["finite_subwork"] = source["finite_review"].take();
    let original = source.clone();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let selection = &prepared["scheduler"]["selection"];
    assert_eq!(selection["model"], "gpt-6.1-sol");
    assert_eq!(selection["task_type"], "research");
    assert!(selection["quota_recovery"].is_object());
    for field in ["sandbox", "mcp_profile", "add_dirs", "network"] {
        assert_eq!(selection[field], original[field], "{field}");
    }
    assert_eq!(prepared["subwork"], original["subwork"]);
    assert_eq!(f.e.backend.records[1], original);
}

pub(super) fn inventory(at: f64) -> Value {
    json!({"at":at,"anthropic":{"at":at,"verified":true,"exhausted":true,"available":false},
        "openai":{"at":at,"verified":true,"exhausted":false,"available":true}})
}

fn grok_fixture() -> (Fixture, subwork_support::Fixture) {
    let (mut f, mut sub) = fixture();
    sub.register();
    sub.db["items"][1]["subwork"]["kind"] = json!("review");
    f.e.backend.records[1]["subwork"] = sub.db["items"][1]["subwork"].clone();
    f.e.backend.records[1]["subwork_spec"] = sub.db["items"][1]["spec"].clone();
    f.e.backend.records[1]["assigned_needs"] = sub.db["items"][1]["needs"].clone();
    f.e.backend.records[1]["subwork_validated"] = json!(true);
    sub.db["items"][0]["status"] = json!("wait");
    f.e.backend.records[0]["status"] = json!("succeeded");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    (f, sub)
}

#[test]
fn grok_normal_scheduler_link_transfers_the_generation_and_quota_capsule_once() {
    let (mut f, mut sub) = grok_fixture();
    sub.db["items"][1]["model"] = json!("grok-4.7");
    f.e.backend.records[1]["model"] = json!("grok-4.7");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let key = prepared["scheduler"]["key"].clone();
    let mut next = f.e.backend.records[1].clone();
    next["id"] = json!("new-reader");
    next["status"] = json!("queued");
    next["launch_key"] = key.clone();
    next["model"] = prepared["scheduler"]["selection"]["model"].clone();
    next["todo_resume_generation"] = json!(1);
    f.e.link(&json!(2), &next, &key).unwrap();
    let assigned = f.get(2);
    assert_eq!(assigned["resume_generation"], 1);
    assert_eq!(
        assigned["worker_receipt"],
        json!({"job":"new-reader","generation":1})
    );
    assert!(assigned["scheduler"]["key"].is_null());
    assert!(cc_job_policy::quota_recovery::verify(
        &prepared["scheduler"]["selection"]["quota_recovery"],
        &assigned,
        &f.e.backend.records[1],
        &next,
        T
    ));
    f.e.link(&json!(2), &next, &key).unwrap();
    assert_eq!(f.get(2), assigned);
}

#[test]
fn grok_legacy_unreceived_generation_stays_historical_while_new_assignment_is_verified() {
    let (mut f, mut sub) = grok_fixture();
    sub.db["items"][1]["model"] = json!("grok-4.7");
    f.e.backend.records[1]["model"] = json!("grok-4.7");
    f.e.backend.records[1]["todo_resume_generation"] = json!(1);
    let original = f.e.backend.records[1].clone();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let proof = &prepared["scheduler"]["selection"]["quota_recovery"];
    assert_eq!(proof["legacy_unreceived_source"], true);
    assert_eq!(proof["resume_generation"], 0);
    let mut next = original.clone();
    next["id"] = json!("new-reader");
    next["status"] = json!("queued");
    next["model"] = prepared["scheduler"]["selection"]["model"].clone();
    next["launch_key"] = prepared["scheduler"]["key"].clone();
    f.e.link(&json!(2), &next, &next["launch_key"]).unwrap();
    assert!(cc_job_policy::quota_recovery::verify(
        proof,
        &f.get(2),
        &original,
        &next,
        T
    ));
    assert_eq!(f.e.backend.records[1], original);
    for change in ["generation", "receipt", "relation", "needs", "checkout"] {
        let mut bad = prepared.clone();
        match change {
            "generation" => bad["resume_generation"] = json!(2),
            "receipt" => bad["worker_receipt"] = json!({"job":null,"generation":0}),
            "relation" => bad["subwork"]["origin_job_id"] = json!("other-author"),
            "needs" => bad["needs"] = json!(["cargo"]),
            "checkout" => bad["scheduler"]["worktree"] = json!(sub.repo),
            _ => unreachable!(),
        }
        assert!(
            !cc_job_policy::quota_recovery::eligible_source(&bad, &original),
            "{change}"
        );
    }
}

#[test]
fn grok_verified_quota_rechecks_the_old_transient_cooldown_without_releasing_other_waits() {
    for case in ["quota", "transient", "owner_stop", "resource_wait"] {
        let (mut f, mut sub) = grok_fixture();
        sub.db["items"][1]["model"] = json!("grok-4.7");
        sub.db["items"][1]["scheduler"]["not_before"] = json!(T + 3600.);
        sub.db["items"][1]["scheduler"]["phase"] = json!("retry");
        sub.db["items"][1]["scheduler"]["last_stop"] = json!("failed");
        f.e.backend.records[1]["model"] = json!("grok-4.7");
        match case {
            "transient" => f.e.backend.records[1]["last_failure_kind"] = json!("transient"),
            "owner_stop" => sub.db["items"][1]["stopped"] = json!(true),
            "resource_wait" => {
                sub.db["items"][1]["scheduler"]["resource_wait"] = json!({"until":T+3600.})
            }
            _ => (),
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let prepared =
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .unwrap();
        assert_eq!(prepared.is_some(), case == "quota", "{case}");
        assert_eq!(f.get(2)["scheduler"]["not_before"], T + 3600.);
    }
}

fn fixture() -> (Fixture, subwork_support::Fixture) {
    let mut sub = subwork_support::Fixture::new();
    sub.db["items"][1]["task_type"] = json!("review");
    sub.db["items"][1]["sandbox"] = json!("read-only");
    sub.db["items"][1]["model"] = json!("gpt-6-luna");
    sub.db["items"][1]["model_origin"] = json!("policy");
    sub.db["items"][1]["effort"] = json!("max");
    sub.db["items"][1]["review_source_jobs"] = json!(["parent"]);
    sub.db["items"][1]["jobs"] = json!(["quota-reader"]);
    sub.db["items"][1]["scheduler"]["last_job"] = json!("quota-reader");
    let row = &sub.db["items"][1];
    sub.jobs.push(json!({"id":"quota-reader","todo_id":2,"status":"failed",
        "last_failure_kind":"quota","model":"anthropic/claude-opus-5-5","model_origin":"policy",
        "effort":"max","cwd":row["scheduler"]["worktree"],"task_type":"review",
        "sandbox":"read-only","mcp_profile":"minimal","network":false,"add_dirs":[],
        "approval_policy":"never","approvals_reviewer":"user",
        "launch_key":"11111111111111111111111111111111",
        "started_at":iso(T-120.),"finished_at":iso(T-60.),
        "finite_review":{"todo_id":2,"spec":row["spec"],"launch_key":"11111111111111111111111111111111",
            "spec_sha256":cc_job_policy::product_retry::digest(&std::fs::read(s(&row["spec"])).unwrap())},
        "review_source_jobs":["parent"],"review_author_models":["gpt-6.1-sol"],
        "todo_dependency_generation":0,"todo_resume_generation":0}));
    let mut f = Fixture::new();
    f.e.backend.quota_inventory = inventory(T);
    f.e.backend.records = sub.jobs.clone();
    f.e.backend.ps.clear();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    (f, sub)
}

fn gpt_research_fixture() -> (Fixture, subwork_support::Fixture) {
    let (mut f, mut sub) = grok_fixture();
    let row = &mut sub.db["items"][1];
    row["subwork"]["kind"] = json!("research");
    row["task_type"] = json!("research");
    row["model"] = json!("gpt-6.1-sol");
    let source = &mut f.e.backend.records[1];
    source["model"] = row["model"].clone();
    source["task_type"] = row["task_type"].clone();
    source["subwork"] = row["subwork"].clone();
    source["finite_subwork"] = source["finite_review"].take();
    source["capacity_same_model_retry_count"] = json!(1);
    source["capacity_account_selector"] = json!("p000001");
    source["capacity_tried_accounts"] = json!(["p000001"]);
    source["capacity_history"] = json!([{"kind":"same_model_account_retry","account_selector":"p000001"}]);
    f.e.backend.quota_inventory["openai"]["safe_routes"] = json!([
        {"selector":"p000001","aliases":["p000001"],"at":T,"verified":true},
        {"selector":"p000002","aliases":["p000002"],"at":T,"verified":true}
    ]);
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    (f, sub)
}

fn dependency_continuation_fixture() -> (Fixture, subwork_support::Fixture) {
    let (mut f,mut sub)=gpt_research_fixture();
    current_continuation_parent(&mut f,&mut sub);
    let row=&mut sub.db["items"][1];
    row["resume_generation"]=json!(3);row["dependency_generation"]=json!(1);
    f.e.backend.records[1]["todo_resume_generation"]=json!(2);
    let request=cc_job_policy::subwork_continuation::request(row,&f.e.backend.records[1],&f.e.backend.records[0]).unwrap();
    row["subwork_continuation"]=request;
    write_json(&f.e.todo_path,&sub.db,1).unwrap();(f,sub)
}
fn current_continuation_parent(f:&mut Fixture,sub:&mut subwork_support::Fixture) {
    f.e.backend.records[0]["todo_resume_generation"]=json!(1);
    sub.db["items"][0]["resume_generation"]=json!(1);
    sub.db["items"][0]["worker_receipt"]=json!({"job":f.e.backend.records[0]["id"],"generation":1});
}
#[test]
fn dependency_continuation_rejects_parent_generation_replaced_after_intake() {
    for case in ["generation","worker","receipt"] {
        let (mut f,mut sub)=dependency_continuation_fixture();
        let parent=&mut sub.db["items"][0];
        match case {
            "generation"=> {parent["resume_generation"]=json!(2);parent["worker_receipt"]["generation"]=json!(2);},
            "worker"=> {parent["scheduler"]["job"]=json!("new-parent");parent["worker_receipt"]["job"]=json!("new-parent");},
            "receipt"=>parent["worker_receipt"]["generation"]=json!(2),
            _=>unreachable!(),
        }
        write_json(&f.e.todo_path,&sub.db,1).unwrap();
        let out=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap());
        assert!(out.is_err() || out.unwrap().is_none(),"{case}: old parent request remained valid");
        assert!(f.e.backend.launched.is_empty());
    }
}
#[test]
fn dependency_continuation_prepares_current_research_without_reviving_stale_quota() {
    let (mut f,sub)=dependency_continuation_fixture();let original=f.e.backend.records[1].clone();
    assert!(!cc_job_policy::quota_recovery::eligible_source(&sub.db["items"][1],&original));
    let prepared=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap()).unwrap().unwrap();
    let selected=&prepared["scheduler"]["selection"];
    assert_eq!(selected["model"],"gpt-6.1-sol");assert_eq!(selected["effort"],"max");assert_eq!(selected["model_origin"],"policy");
    assert!(selected["quota_recovery"].is_null());assert!(selected["subwork_continuation"].is_object());
    assert!(prepared["scheduler"]["resume_session"].is_null());assert_eq!(f.e.backend.records[1],original);
}
#[test]
fn dependency_continuation_rechecks_request_drain_and_current_oauth() {
    for case in ["flag","generation","dependency","owner","source","scope","spec","stop","pending","live","unknown","exhausted","stale"] {
        let (mut f,mut sub)=dependency_continuation_fixture();
        let row=&mut sub.db["items"][1];
        match case {
            "flag"=>row["subwork_continuation"]=json!(true),
            "generation"=>row["resume_generation"]=json!(4),
            "dependency"=>row["dependency_generation"]=json!(2),
            "owner"=>f.e.backend.records[0]["work_owner"]["repository"]=json!("different"),
            "source"=>f.e.backend.records[1]["todo_id"]=json!(99),
            "scope"=>row["network"]=json!(true),
            "spec"=>std::fs::write(s(&row["spec"]),"Changed").unwrap(),
            "stop"=>row["owner_stopped"]=json!(true),
            "pending"=>row["wait_on"]=json!([99]),
            "live"=>f.e.backend.records[1]["status"]=json!("running"),
            "unknown"=>f.e.backend.quota_inventory["openai"]["verified"]=json!(false),
            "exhausted"=>f.e.backend.quota_inventory["openai"]["available"]=json!(false),
            "stale"=>f.e.backend.quota_inventory["openai"]["at"]=json!(T-61.),_=>unreachable!(),
        }
        write_json(&f.e.todo_path,&sub.db,1).unwrap();
        let out=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap());
        assert!(out.is_err() || out.unwrap().is_none(),"{case}");assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn dependency_continuation_is_consumed_once_and_later_work_uses_its_own_result() {
    let (mut f,mut sub)=dependency_continuation_fixture();
    let first=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap()).unwrap().unwrap();
    let proof=first["scheduler"]["selection"]["subwork_continuation"].clone();
    let mut next=f.e.backend.records[1].clone();next["id"]=json!("new-current");next["status"]=json!("succeeded");
    next["launch_key"]=first["scheduler"]["key"].clone();next["subwork_continuation"]=proof;
    next["todo_resume_generation"]=json!(4);next["todo_dependency_generation"]=json!(1);
    let row=&mut sub.db["items"][1];row["resume_generation"]=json!(4);row["worker_receipt"]=json!({"job":"new-current","generation":4});
    row["jobs"].as_array_mut().unwrap().push(next["id"].clone());row["scheduler"]["last_job"]=next["id"].clone();
    row["scheduler"]["key"]=Value::Null;f.e.backend.records.push(next);
    write_json(&f.e.todo_path,&sub.db,1).unwrap();
    let later=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap()).unwrap().unwrap();
    assert!(later["scheduler"]["selection"]["subwork_continuation"].is_null());
    assert!(later["scheduler"]["selection"]["quota_recovery"].is_null());
}

#[test]
fn dependency_continuation_retains_the_ordinary_independent_review_default() {
    let (mut f,mut sub)=grok_fixture();
    current_continuation_parent(&mut f,&mut sub);
    let row=&mut sub.db["items"][1];row["resume_generation"]=json!(3);row["dependency_generation"]=json!(1);
    f.e.backend.records[1]["todo_resume_generation"]=json!(2);
    row["subwork_continuation"]=cc_job_policy::subwork_continuation::request(row,&f.e.backend.records[1],&f.e.backend.records[0]).unwrap();
    f.e.backend.quota_inventory["anthropic"]=json!({"at":T,"verified":true,"available":true,"exhausted":false});
    write_json(&f.e.todo_path,&sub.db,1).unwrap();
    let prepared=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap()).unwrap().unwrap();
    let selection=&prepared["scheduler"]["selection"];
    assert_eq!(selection["model"],"anthropic/claude-opus-5-5");assert_eq!(selection["review_excluded_models"],json!(["gpt-6.1-sol"]));
    assert!(selection["quota_recovery"].is_null());assert!(selection["subwork_continuation"].is_object());
}

#[test]
fn gpt_quota_research_uses_another_verified_oauth_route_with_the_original_budget() {
    let (mut f, sub) = gpt_research_fixture();
    let original = f.e.backend.records[1].clone();
    let prepared = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap().unwrap();
    let selected = &prepared["scheduler"]["selection"];
    assert_eq!(selected["model"], "gpt-6.1-sol");
    assert_eq!(selected["effort"], "max");
    assert_eq!(selected["capacity_account_selector"], "p000002");
    assert_eq!(selected["quota_recovery"]["retry"]["same_model_count"], 2);
    assert_eq!(prepared["subwork"], original["subwork"]);
    assert_eq!(f.e.backend.records[1], original);
}

#[test]
fn gpt_quota_rechecks_only_an_expired_source_and_its_automatic_preparation_wait() {
    for blocked in [false, true] {
        let (mut f, mut sub) = gpt_research_fixture();
        let row = &mut sub.db["items"][1];
        row["scheduler"]["phase"] = json!("preparing");
        row["scheduler"]["not_before"] = json!(T + 3600.);
        row["scheduler"]["resource_wait"] = json!({"reason":"http_429","until":T+3600.});
        row["scheduler"]["preparation_failure"] = json!({"reason":"resource_wait","at":T-300.,"retry_at":T+3600.});
        if blocked { f.e.backend.records[1]["rate_limit_reset_at"] = json!(T+900.); }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let prepared = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap();
        assert_eq!(prepared.is_some(), !blocked);
        assert_eq!(f.get(2)["scheduler"]["not_before"], T+3600.);
    }
}

#[test]
fn gpt_quota_automatic_wait_is_rechecked_through_normal_targeted_dispatch() {
    let (mut f, mut sub) = gpt_research_fixture();
    f.e.backend.clock += 7200.;
    let now = f.e.backend.clock;
    for job in &mut f.e.backend.records { job["created_at"] = json!(iso(T-300.)); }
    let health = &mut f.e.backend.quota_inventory;
    health["at"] = json!(now);
    health["openai"]["at"] = json!(now);
    for route in health["openai"]["safe_routes"].as_array_mut().unwrap() {route["at"] = json!(now);}
    let row = &mut sub.db["items"][1];
    row["scheduler"]["phase"] = json!("preparing");
    row["scheduler"]["not_before"] = json!(now+3600.);
    row["scheduler"]["resource_wait"] = json!({"reason":"http_429","until":now+3600.});
    row["scheduler"]["preparation_failure"] = json!({"reason":"resource_wait","at":now-300.,"retry_at":now+3600.});
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    write_json(&f.e.root.join("config.json"), &f.cfg, 1).unwrap();
    write_json(&f.e.root.join("priorities.json"), &f.pri, 1).unwrap();
    f.e.state["last_launch"] = json!(0);
    f.e.persist().unwrap();
    assert!(cc_scheduler::preparation::quota_recheck_candidate(&sub.db["items"][1], &f.e.backend.records, now));
    let report = cc_scheduler::targeted_dispatch::run(&mut f.e, 2).unwrap();
    assert_eq!(report["launched"], "launched1", "report={report}, engine={}, preparation={}", f.e.report, f.get(2)["scheduler"]["preparation_failure"]);
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(f.e.backend.launched[0]["item"]["scheduler"]["selection"]["capacity_account_selector"], "p000002");
}

#[test]
fn gpt_quota_normal_candidate_keeps_question_close_and_other_resource_deadlines() {
    for case in ["question_close", "owner", "source_reset", "other_resource", "different_failure"] {
        let (mut f, mut sub) = gpt_research_fixture();
        let row = &mut sub.db["items"][1];
        row["scheduler"]["phase"] = json!("preparing");
        row["scheduler"]["not_before"] = json!(T+3600.);
        row["scheduler"]["resource_wait"] = json!({"reason":"http_429","until":T+3600.});
        row["scheduler"]["preparation_failure"] = json!({"reason":"resource_wait","retry_at":T+3600.});
        match case {
            "question_close" => row["scheduler"]["question_close_pending"] = json!(true),
            "owner" => row["owner_stopped"] = json!(true),
            "source_reset" => f.e.backend.records[1]["rate_limit_reset_at"] = json!(T+900.),
            "other_resource" => row["scheduler"]["resource_wait"]["reason"] = json!("memory"),
            "different_failure" => row["scheduler"]["preparation_failure"]["reason"] = json!("tool_failed"),
            _ => unreachable!(),
        }
        assert!(!cc_scheduler::preparation::quota_recheck_candidate(row, &f.e.backend.records, T), "{case}");
    }
}

#[test]
fn gpt_quota_does_not_admit_unknown_routes_reused_aliases_stops_or_exhausted_budgets() {
    for case in ["unknown", "no_routes", "used_alias", "used_label", "stale", "budget", "history", "stop", "dependency"] {
        let (mut f, mut sub) = gpt_research_fixture();
        match case {
            "unknown" => f.e.backend.quota_inventory["openai"]["verified"] = json!(false),
            "no_routes" => f.e.backend.quota_inventory["openai"]["safe_routes"] = json!([]),
            "used_alias" => f.e.backend.quota_inventory["openai"]["safe_routes"][1]["aliases"] = json!(["p000001","p000002"]),
            "used_label" => {f.e.backend.records[1]["capacity_tried_labels"]=json!(["pabcdef"]);f.e.backend.quota_inventory["openai"]["safe_routes"][1]["label"]=json!("pabcdef");},
            "stale" => f.e.backend.quota_inventory["openai"]["safe_routes"][1]["at"] = json!(T-61.),
            "budget" => f.e.backend.records[1]["capacity_same_model_retry_count"] = json!(3),
            "history" => f.e.backend.records[1]["capacity_same_model_retry_count"] = json!(0),
            "stop" => sub.db["items"][1]["owner_stopped"] = json!(true),
            "dependency" => sub.db["items"][1]["wait_on"] = json!([99]),
            _ => unreachable!(),
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let result = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap());
        assert!(result.is_err() || result.unwrap().is_none(), "{case}");
        assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn tracked_grok_quota_chooses_the_route_after_the_model_and_keeps_switch_budget() {
    let (mut f,mut sub)=grok_fixture();
    sub.db["items"][1]["model"]=json!("grok-4.7");
    let source=&mut f.e.backend.records[1];source["model"]=json!("grok-4.7");
    source["capacity_retry_count"]=json!(1);source["capacity_history"]=json!([{"from_model":"gpt-6.1-sol","to_model":"grok-4.7"}]);
    f.e.backend.quota_inventory["openai"]["safe_routes"]=json!([{"selector":"p000001","aliases":["p000001"],"at":T,"verified":true}]);
    write_json(&f.e.todo_path,&sub.db,1).unwrap();
    let p=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap()).unwrap().unwrap();
    assert_eq!(p["scheduler"]["selection"]["model"],"gpt-6-astra");
    assert_eq!(p["scheduler"]["selection"]["quota_recovery"]["retry"]["switch_count"],2);
    assert_eq!(p["scheduler"]["selection"]["capacity_account_selector"],"p000001");
}

#[test]
fn gpt_quota_changes_provider_only_after_same_gpt_budget_or_verified_exhaustion() {
    for exhausted in [false, true] {
        let (mut f, mut sub) = gpt_research_fixture();
        f.e.backend.quota_inventory["anthropic"] = json!({"at":T,"verified":true,"available":true,"exhausted":false});
        if exhausted {
            f.e.backend.quota_inventory["openai"]["exhausted"] = json!(true);
            f.e.backend.quota_inventory["openai"]["available"] = json!(false);
            f.e.backend.quota_inventory["openai"]["safe_routes"] = json!([]);
        } else {
            f.e.backend.records[1]["capacity_same_model_retry_count"] = json!(3);
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let p = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap().unwrap();
        let s = &p["scheduler"]["selection"];
        assert_eq!(s["model"], "anthropic/claude-opus-5-5");
        assert_eq!(s["effort"], "max");
        assert_eq!(s["model_origin"], "policy");
        assert_eq!(s["quota_recovery"]["retry"]["switch_count"], 1);
        assert_eq!(s["quota_recovery"]["retry"]["same_model_count"], if exhausted {1} else {3});
        sub.db["items"][1]["model_origin"] = json!("owner_request");
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let owner = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap().unwrap();
        assert_eq!(owner["scheduler"]["selection"]["model"], "gpt-6.1-sol");
        assert!(owner["scheduler"]["selection"]["quota_recovery"].is_null());
    }
}

#[test]
fn historic_quota_failure_never_moves_its_deadline_on_each_observation() {
    use cc_scheduler::preparation::resource_deadline;
    let source = json!({"status":"failed","finished_at":iso(T-7200.),
        "last_error":"429 Too Many Requests: usage limit reached","retry_not_before":T-7100.});
    assert_eq!(resource_deadline(&source, T-7000.), Some(T-3600.));
    assert_eq!(resource_deadline(&source, T), None);
    assert_eq!(resource_deadline(&source, T+3600.), None);
    let mut reset = source;
    reset["rate_limit_reset_at"] = json!(T+900.);
    assert_eq!(resource_deadline(&reset, T), Some(T+900.));
    assert_eq!(resource_deadline(&reset, T+901.), None);
}

#[test]
fn gpt_quota_budget_survives_a_provider_change_and_can_return_without_starting_over() {
    let (mut f, sub) = gpt_research_fixture();
    f.e.backend.records[1]["capacity_same_model_retry_count"] = json!(3);
    f.e.backend.quota_inventory["anthropic"] = json!({"at":T,"verified":true,"available":true,"exhausted":false});
    let p = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap().unwrap();
    let proof = &p["scheduler"]["selection"]["quota_recovery"];
    let mut next = f.e.backend.records[1].clone();
    next["id"] = json!("next-source");
    next["model"] = json!("anthropic/claude-opus-5-5");
    cc_job_policy::quota_retry::inherit(&mut next, &f.e.backend.records[1], proof);
    next["quota_recovery"] = proof.clone();
    assert_eq!(next["capacity_same_model_retry_count"], 3);
    assert_eq!(next["capacity_retry_count"], 1);
    let mut health = f.e.backend.quota_inventory.clone();
    health["anthropic"]["exhausted"] = json!(true);
    health["anthropic"]["available"] = json!(false);
    let target = json!({"model":"gpt-6.1-sol","capacity_account_selector":"p000002"});
    assert!(cc_job_policy::quota_recovery::transition(&next, &target, &health, T));
    next["capacity_same_model_retry_count"] = json!(0);
    assert!(!cc_job_policy::quota_recovery::transition(&next, &target, &health, T));
}

#[test]
fn gpt_quota_first_default_route_failure_can_use_fresh_verified_subscription() {
    let (mut f, sub) = gpt_research_fixture();
    let source=&mut f.e.backend.records[1];
    for key in ["capacity_account_selector","capacity_same_model_retry_count","capacity_history","capacity_tried_accounts"] {source[key]=Value::Null;}
    let p=f.e.prepare(&sub.db["items"][1],sub.repo.to_str().unwrap()).unwrap().unwrap();
    assert_eq!(p["scheduler"]["selection"]["model"],"gpt-6.1-sol");
    assert_eq!(p["scheduler"]["selection"]["quota_recovery"]["retry"]["same_model_count"],1);
}

#[test]
fn gpt_quota_unavailable_health_is_paced_without_extending_the_saved_deadline() {
    let (mut f, mut sub)=gpt_research_fixture();
    for j in &mut f.e.backend.records {j["created_at"]=json!(iso(T-300.));}
    let row=&mut sub.db["items"][1];
    row["scheduler"]["phase"]=json!("preparing");
    row["scheduler"]["not_before"]=json!(T+3600.);
    row["scheduler"]["resource_wait"]=json!({"reason":"http_429","until":T+3600.});
    row["scheduler"]["preparation_failure"]=json!({"reason":"resource_wait","at":T-300.,"retry_at":T+3600.});
    f.e.backend.quota_inventory=Value::Null;
    write_json(&f.e.todo_path,&sub.db,1).unwrap();
    for offset in [0.,1.,299.,300.,301.] {
        f.e.backend.clock=T+offset;
        let _=f.e.dispatch_for(&f.cfg,&f.pri,Some(2));
        assert_eq!(f.get(2)["scheduler"]["not_before"],T+3600.);
        assert!(f.e.backend.launched.is_empty());
        assert_eq!(f.e.backend.quota_inventory_calls,if offset>=300. {2}else{1});
    }
}

#[test]
fn gpt_quota_terminal_budget_is_recorded_without_a_fresh_job_or_refresh_loop() {
    terminal_quota_model("gpt-6.1-sol",3);
}

#[test]
fn non_gpt_quota_terminal_switch_budget_does_not_refresh_forever() {
    for model in ["anthropic/claude-opus-5-5","grok-4.7"] {terminal_quota_model(model,1);}
}
fn terminal_quota_model(model:&str,same:u64) {
    let (mut f, sub)=gpt_research_fixture();
    for j in &mut f.e.backend.records {j["created_at"]=json!(iso(T-300.));}
    f.e.backend.records[1]["model"]=json!(model);
    f.e.backend.records[1]["capacity_same_model_retry_count"]=json!(same);
    f.e.backend.records[1]["capacity_retry_count"]=json!(3);
    write_json(&f.e.todo_path,&sub.db,1).unwrap();
    let _=f.e.dispatch_for(&f.cfg,&f.pri,Some(2));
    assert_eq!(f.get(2)["scheduler"]["phase"],"quota_retry_ended");
    assert_eq!(f.e.backend.quota_inventory_calls,0);
    f.e.backend.clock+=7200.;
    let _=f.e.dispatch_for(&f.cfg,&f.pri,Some(2));
    assert_eq!(f.e.backend.quota_inventory_calls,0);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn queued_capacity_cooldown_is_never_cancelled_as_a_resource_failure() {
    let mut f=Fixture::new();f.managed(1,1,"other",0);
    f.e.backend.records[0]["status"]=json!("queued");
    f.e.backend.records[0]["last_failure_kind"]=json!("capacity");
    f.e.backend.records[0]["last_error"]=json!("Selected model is at capacity");
    f.e.backend.records[0]["retry_not_before"]=json!(T+30.);
    f.reconcile();
    assert!(f.e.backend.cancelled.is_empty());
    assert_eq!(f.get(1)["scheduler"]["job"],"j1");
    assert!(f.get(1)["scheduler"]["resource_wait"].is_null());
}

#[test]
fn verified_quota_review_keeps_policy_luna_in_scheduler_preparation() {
    let (mut f, sub) = fixture();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let selected = &prepared["scheduler"]["selection"];
    assert_eq!(
        selected["model"], "gpt-6-luna",
        "verified quota recovery returned to the depleted review default"
    );
    assert_eq!(selected["model_origin"], "policy");
    assert_eq!(selected["effort"], "max");
    assert_eq!(
        selected["finite_review_spec_sha256"],
        cc_job_policy::product_retry::digest(&std::fs::read(s(&prepared["spec"])).unwrap())
    );
    assert_eq!(selected["review_excluded_models"], json!(["gpt-6.1-sol"]));
    assert!(selected["quota_recovery"].is_object());
    assert!(prepared["scheduler"]["resume_session"].is_null());
    let invocation = RealBackend::new(f.e.root.clone()).launch_command(
        &prepared,
        Path::new("review.md"),
        selected,
    );
    for (flag, value) in [
        ("--model", "gpt-6-luna"),
        ("--model-origin", "policy"),
        ("--effort", "max"),
        ("--task-type", "review"),
        ("--sandbox", "read-only"),
    ] {
        assert!(
            invocation.args.windows(2).any(|args| args == [flag, value]),
            "{flag}"
        );
    }
}

#[test]
fn quota_selection_requires_current_drained_scope_and_fresh_oauth() {
    for case in [
        "unknown_quota",
        "stale_quota",
        "target_empty",
        "wrong_job",
        "wrong_generation",
        "changed_spec",
        "unbound_spec",
        "rewritten_spec",
        "wrong_worktree",
        "wider_scope",
        "not_drained",
        "owner_stop",
        "cancel_without_intent",
        "same_author",
        "future_finish",
    ] {
        let (mut f, mut sub) = fixture();
        match case {
            "unknown_quota" => f.e.backend.quota_inventory["openai"]["verified"] = json!(false),
            "stale_quota" => f.e.backend.quota_inventory["openai"]["at"] = json!(T - 61.),
            "target_empty" => f.e.backend.quota_inventory["openai"]["available"] = json!(false),
            "wrong_job" => f.e.backend.records[1]["todo_id"] = json!(99),
            "wrong_generation" => sub.db["items"][1]["resume_generation"] = json!(99),
            "changed_spec" => {
                f.e.backend.records[1]["finite_review"]["spec"] = json!("different.md")
            }
            "unbound_spec" => {
                f.e.backend.records[1]["finite_review"]
                    .as_object_mut()
                    .unwrap()
                    .remove("spec_sha256");
            }
            "rewritten_spec" => std::fs::write(
                s(&sub.db["items"][1]["spec"]),
                "Different instructions before recovery",
            )
            .unwrap(),
            "wrong_worktree" => f.e.backend.records[1]["cwd"] = json!(sub.repo),
            "wider_scope" => sub.db["items"][1]["add_dirs"] = json!([sub.repo]),
            "not_drained" => {
                f.e.backend.records[1]["pid"] = json!(444);
                f.e.backend.records[1]["pid_created"] = json!(100);
                let mut p = proc(444, 0, "codex.exe");
                p["created"] = json!(100);
                f.e.backend.ps.insert(444, p);
            }
            "owner_stop" => sub.db["items"][1]["stopped"] = json!(true),
            "cancel_without_intent" => f.e.backend.records[1]["status"] = json!("cancelled"),
            "same_author" => f.e.backend.records[0]["model"] = json!("gpt-6-luna"),
            "future_finish" => f.e.backend.records[1]["finished_at"] = json!(iso(T + 60.)),
            _ => unreachable!(),
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let result = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap());
        assert!(result.is_err(), "{case} unexpectedly prepared {result:?}");
        assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn ordinary_policy_review_keeps_its_default_and_owner_choice_remains_distinct() {
    for owner in [false, true] {
        let (mut f, mut sub) = fixture();
        sub.db["items"][1]["scheduler"]["last_job"] = Value::Null;
        sub.db["items"][1]["model_origin"] = json!(if owner { "owner_request" } else { "policy" });
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let p =
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .unwrap()
                .unwrap();
        assert_eq!(
            p["scheduler"]["selection"]["model"],
            if owner {
                "gpt-6-luna"
            } else {
                "anthropic/claude-opus-5-5"
            }
        );
        assert!(p["scheduler"]["selection"]["quota_recovery"].is_null());
    }
}

#[test]
fn selected_review_default_waits_on_verified_exhaustion_before_registration() {
    let (mut f, mut sub) = fixture();
    sub.db["items"][1]["scheduler"]["last_job"] = Value::Null;
    f.e.backend.m["quota_block_model"] = json!("anthropic/claude-opus-5-5");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    assert!(
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .is_none()
    );
    assert_eq!(
        f.get(2)["scheduler"]["resource_wait"]["reason"],
        "quota_exhausted"
    );
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn recovery_cancel_keeps_the_quota_classification_and_policy_choice() {
    let (mut f, sub) = fixture();
    f.e.backend.records[1]["status"] = json!("cancelled");
    f.e.backend.records[1]["recovery_cancel"] = json!({"key":"drain","committed_at":iso(T-50.)});
    let p =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    assert_eq!(p["scheduler"]["selection"]["model"], "gpt-6-luna");
    assert_eq!(f.e.backend.records[1]["last_failure_kind"], "quota");
}

#[test]
fn quota_preparation_rejects_a_spec_changed_during_metadata_collection() {
    let (mut f, sub) = fixture();
    f.e.backend.quota_spec_edit = Some((
        PathBuf::from(s(&sub.db["items"][1]["spec"])),
        "Another fixed head".into(),
    ));
    assert!(
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .is_err()
    );
    assert!(f.e.backend.launched.is_empty());
}
