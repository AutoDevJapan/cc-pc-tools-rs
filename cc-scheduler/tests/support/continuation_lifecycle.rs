use super::*;

fn later_result(status: &str) -> (Fixture, subwork_support::Fixture) {
    let (mut f, mut sub) = dependency_continuation_fixture();
    let mut source = f.e.backend.records[1].clone();
    source["id"] = json!("latest-result");
    source["status"] = json!(status);
    source["launch_key"] = json!("latest-result-key");
    source["finite_subwork"]["launch_key"] = source["launch_key"].clone();
    source["work_owner"] = cc_job_policy::work_owner::checkout(s(&source["cwd"]));
    source["todo_resume_generation"] = json!(5);
    source["todo_dependency_generation"] = json!(1);
    source["effort"] = json!("max");
    source["launch_policy_version"] = json!(cc_job_policy::launch_context::VERSION);
    source["thread_id"] = json!("12345678-1234-4321-9234-123456789abc");
    source["last_message"] = json!("状態: 継続中\nLATEST_OWN_RESULT");
    if status == "cancelled" {
        source["recovery_cancel"] =
            json!({"key":"recovery-latest","committed_at":"2026-10-26T01:01:00Z"});
    }
    let row = &mut sub.db["items"][1];
    row["resume_generation"] = json!(5);
    row["worker_receipt"] = json!({"job":source["id"],"generation":5});
    row["jobs"]
        .as_array_mut()
        .unwrap()
        .push(source["id"].clone());
    row["scheduler"]["last_job"] = source["id"].clone();
    row["scheduler"]["key"] = Value::Null;
    row["scheduler"]["resume"] = json!(true);
    f.e.backend.records.push(source);
    (f, sub)
}

#[test]
fn stale_request_is_inert_for_every_ordinary_result_and_preserves_its_session() {
    for case in [
        "success",
        "lost",
        "recovery_cancel",
        "transient",
        "request_failed",
        "auth",
        "cancel_failure",
        "unclassified",
        "spec_changed",
        "owner_model",
        "parent_absent",
        "parent_generation",
        "unknown_quota",
    ] {
        let status = match case {
            "lost" => "lost",
            "recovery_cancel" => "cancelled",
            "transient" | "request_failed" | "auth" | "cancel_failure" | "unclassified" => "failed",
            _ => "succeeded",
        };
        let (mut f, mut sub) = later_result(status);
        let row = &mut sub.db["items"][1];
        let source = f.e.backend.records.last_mut().unwrap();
        match case {
            "transient" | "request_failed" | "auth" => source["last_failure_kind"] = json!(case),
            "cancel_failure" => source["last_failure_kind"] = json!("cancel"),
            "unclassified" => source["last_failure_kind"] = Value::Null,
            "spec_changed" => {
                std::fs::write(s(&row["spec"]), "Updated ordinary specification.").unwrap()
            }
            "owner_model" => {
                row["model_origin"] = json!("owner_request");
                source["model_origin"] = json!("owner_request");
            }
            "parent_absent" => {
                sub.db["items"][0]["scheduler"]["job"] = Value::Null;
            }
            "parent_generation" => {
                sub.db["items"][0]["resume_generation"] = json!(2);
                sub.db["items"][0]["worker_receipt"]["job"] = Value::Null;
            }
            "unknown_quota" => f.e.backend.quota_inventory = Value::Null,
            _ => (),
        }
        let original = f.e.backend.records.clone();
        let retained = sub.db["items"][1]["subwork_continuation"].clone();
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let prepared =
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .unwrap()
                .expect(case);
        let selection = &prepared["scheduler"]["selection"];
        assert!(
            selection["subwork_continuation"].is_null(),
            "{case}: stale contract was reused"
        );
        assert!(
            selection["quota_recovery"].is_null(),
            "{case}: old quota metadata became a failure"
        );
        assert_eq!(
            prepared["scheduler"]["resume_session"], "12345678-1234-4321-9234-123456789abc",
            "{case}"
        );
        assert_eq!(prepared["subwork_continuation"], retained);
        assert_eq!(f.e.backend.records, original);
    }
}

#[test]
fn successful_lost_and_recovery_cancel_results_do_not_exhaust_a_quota_budget() {
    for status in ["succeeded", "lost", "cancelled"] {
        let (mut f, mut sub) = later_result(status);
        let source = f.e.backend.records.last_mut().unwrap();
        source["model"] = json!("anthropic/claude-opus-5-5");
        source["capacity_same_model_retry_count"] = json!(3);
        source["capacity_retry_count"] = json!(3);
        sub.db["items"][1]["model"] = source["model"].clone();
        let original = source.clone();
        assert_eq!(
            cc_job_policy::quota_retry::terminal_reason(source),
            Some("finite_retry_budget_exhausted")
        );
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let prepared =
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .unwrap()
                .unwrap();
        assert!(prepared["scheduler"]["selection"]["subwork_continuation"].is_null());
        assert!(prepared["scheduler"]["selection"]["quota_recovery"].is_null());
        assert!(prepared["scheduler"]["quota_retry_terminal"].is_null());
        assert_eq!(f.e.backend.records.last().unwrap(), &original);
    }
}

#[test]
fn finite_recovery_cancel_is_ordinary_with_or_without_a_historical_request() {
    for case in ["remaining", "exhausted", "dependency_changed"] {
        let mut outcomes = Vec::new();
        for retained in [false, true] {
            // Given the same finite worker was cancelled for recovery. Its
            // historical quota classification is not a new quota failure.
            let (mut f, mut sub) = later_result("cancelled");
            let source = f.e.backend.records.last_mut().unwrap();
            if case == "exhausted" {
                source["model"] = json!("anthropic/claude-opus-5-5");
                source["capacity_same_model_retry_count"] = json!(3);
                source["capacity_retry_count"] = json!(3);
                sub.db["items"][1]["model"] = source["model"].clone();
            }
            if case == "dependency_changed" {
                sub.db["items"][1]["dependency_generation"] = json!(2);
            }
            if !retained {
                sub.db["items"][1]["subwork_continuation"] = Value::Null;
            }
            let original = f.e.backend.records.clone();
            let row = &sub.db["items"][1];
            let request = row["subwork_continuation"].clone();
            write_json(&f.e.todo_path, &sub.db, 1).unwrap();
            // When the normal scheduler prepares the next ordinary worker,
            // then neither request presence nor spent quota counters stop it.
            let prepared = f.e.prepare(row, sub.repo.to_str().unwrap())
                .unwrap_or_else(|e| panic!("{case}/{retained}: {}", e.0))
                .unwrap();
            let selection = &prepared["scheduler"]["selection"];
            assert!(selection["quota_recovery"].is_null(), "{case}/{retained}");
            assert!(selection["subwork_continuation"].is_null());
            assert!(prepared["scheduler"]["quota_retry_terminal"].is_null());
            assert_eq!(prepared["subwork_continuation"], request);
            assert_eq!(f.e.backend.records, original);
            outcomes.push(json!({"model":selection["model"],
                "session":prepared["scheduler"]["resume_session"]}));
        }
        assert_eq!(outcomes[0], outcomes[1], "{case}");
        assert_eq!(outcomes[0]["session"], "12345678-1234-4321-9234-123456789abc");
    }
}

#[test]
fn nonquota_cancellation_after_dependency_release_keeps_ordinary_preparation() {
    for ordinary_review in [false, true] {
        // Given an ordinary cancellation with no quota/request authority,
        // occurring while a task is already waiting for a prerequisite.
        let (mut f, mut sub) = later_result("cancelled");
        let source = f.e.backend.records.last_mut().unwrap();
        source["last_failure_kind"] = Value::Null;
        source["recovery_cancel"] = Value::Null;
        let row = &mut sub.db["items"][1];
        row["subwork_continuation"] = Value::Null;
        row["status"] = json!("wait");
        row["wait_on"] = json!([99]);
        row["scheduler"]["job"] = source["id"].clone();
        if ordinary_review {
            row["subwork"] = Value::Null;
            row["task_type"] = json!("review");
            source["subwork"] = Value::Null;
            source["task_type"] = json!("review");
        }
        sub.db["items"].as_array_mut().unwrap().push(json!({"id":99,"status":"todo"}));
        let waiting = sub.db["items"][1].clone();
        assert!(cc_job_policy::todo_wait::release_ready(&mut sub.db).is_empty());
        assert_eq!(sub.db["items"][1], waiting);
        // When the actual prerequisite completes, normal release retains the
        // cancelled pointer. Then preparation preserves pre-existing behavior.
        sub.db["items"][2]["status"] = json!("done");
        assert_eq!(cc_job_policy::todo_wait::release_ready(&mut sub.db), vec![2]);
        let original = f.e.backend.records.clone();
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let prepared = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap_or_else(|e| panic!("ordinary_review={ordinary_review}: {}", e.0))
            .unwrap();
        assert!(prepared["scheduler"]["selection"]["quota_recovery"].is_null());
        assert!(prepared["scheduler"]["selection"]["subwork_continuation"].is_null());
        assert_eq!(prepared["scheduler"]["last_job"], "latest-result");
        assert_eq!(f.e.backend.records, original);
    }
}

#[test]
fn parent_intake_reason_requires_the_same_actor_independent_source_checks_as_cli() {
    for case in ["normal", "scope", "legacy_capsule", "owner_source"] {
        let (mut f, mut sub) = later_result("failed");
        let source = f.e.backend.records.last_mut().unwrap();
        let row = &mut sub.db["items"][1];
        row["dependency_generation"] = json!(2);
        match case {
            "scope" => row["network"] = json!(true),
            "legacy_capsule" => source["quota_recovery"] = json!({"version":1}),
            "owner_source" => source["model_origin"] = json!("owner_request"),
            _ => (),
        }
        let original = f.e.backend.records.clone();
        let allowed = cc_job_policy::subwork_continuation::request(
            &sub.db["items"][1], original.last().unwrap(), &original[0]).is_some();
        assert_eq!(allowed, case == "normal", "{case}");
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let result = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap());
        assert_eq!(result.unwrap_err().0,
            if allowed {"SubworkParentContinuationRequired"} else {"InputRejected"}, "{case}");
        assert_eq!(f.e.backend.records, original);
    }
}

#[test]
fn same_generation_unconsumed_legacy_request_with_a_receipt_remains_valid() {
    let (mut f, mut sub) = dependency_continuation_fixture();
    let old = sub.db["items"][1]["subwork_continuation"].clone();
    assert!(old["source_kind"].is_null());
    assert!(old["source_receipt"].is_null());
    sub.db["items"][1]["worker_receipt"] =
        json!({"job":f.e.backend.records[1]["id"],"generation":3});
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    assert_eq!(
        prepared["scheduler"]["selection"]["subwork_continuation"]["request"],
        old
    );
    assert_eq!(prepared["subwork_continuation"], old);
}

#[test]
fn dependency_quota_uses_available_openai_route_after_non_openai_exhaustion() {
    let (mut f, mut sub) = later_result("failed");
    let source = f.e.backend.records.last_mut().unwrap();
    source["model"] = json!("anthropic/claude-opus-5-5");
    source["capacity_same_model_retry_count"] = json!(3);
    source["capacity_retry_count"] = json!(1);
    source["capacity_account_selector"] = Value::Null;
    let original = source.clone();
    let row = &mut sub.db["items"][1];
    row["dependency_generation"] = json!(2);
    row["subwork_continuation"] =
        cc_job_policy::subwork_continuation::request(row, &original, &f.e.backend.records[0])
            .unwrap();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let selection = &prepared["scheduler"]["selection"];
    assert_eq!(selection["model"], "gpt-6.1-sol");
    assert_eq!(selection["model_origin"], "policy");
    assert_eq!(
        selection["subwork_continuation"]["quota_retry"]["retry"]["same_model_count"],
        3
    );
    assert_eq!(
        selection["subwork_continuation"]["quota_retry"]["retry"]["switch_count"],
        2
    );
    assert_eq!(f.e.backend.records.last().unwrap(), &original);
}

#[test]
fn dependency_quota_keeps_the_owners_model_and_does_not_add_a_policy_retry() {
    let (mut f, mut sub) = later_result("failed");
    let source = f.e.backend.records.last().unwrap().clone();
    let row = &mut sub.db["items"][1];
    row["dependency_generation"] = json!(2);
    row["model"] = json!("anthropic/claude-opus-5-5");
    row["model_origin"] = json!("owner_request");
    row["subwork_continuation"] =
        cc_job_policy::subwork_continuation::request(row, &source, &f.e.backend.records[0])
            .unwrap();
    f.e.backend.quota_inventory["anthropic"] =
        json!({"at":T,"verified":true,"available":true,"exhausted":false});
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let selection = &prepared["scheduler"]["selection"];
    assert_eq!(selection["model"], "anthropic/claude-opus-5-5");
    assert_eq!(selection["model_origin"], "owner_request");
    assert!(selection["subwork_continuation"]["quota_retry"].is_null());
}

#[test]
fn terminal_quota_cache_matches_the_latest_receipt_before_pointer_repair() {
    for terminal in [false, true] {
        let (mut f, mut sub) = later_result("failed");
        f.e.backend.clock += 7200.;
        for source in &mut f.e.backend.records {
            source["created_at"] = json!(iso(T - 300.));
        }
        f.e.state["last_launch"] = json!(0);
        f.e.backend.records.last_mut().unwrap()["capacity_exhausted"] = json!(true);
        let row = &mut sub.db["items"][1];
        row["scheduler"]["last_job"] = json!("old-quota");
        if terminal {
            row["scheduler"]["quota_retry_terminal"] =
                json!({"source_job":"latest-result","at":T-1.});
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let result = f.e.dispatch_for(&f.cfg, &json!({}), Some(2));
        if terminal {
            assert!(result.is_ok());
        } else {
            assert_eq!(result.unwrap_err().0, "QuotaRetryEnded");
        }
        let row = f.get(2);
        if terminal {
            assert!(row["scheduler"]["attempt"].is_null());
        } else {
            assert_eq!(row["scheduler"]["attempt"], 1);
            assert_eq!(row["scheduler"]["phase"], "quota_retry_ended");
        }
        assert!(f.e.backend.launched.is_empty());
    }
}
