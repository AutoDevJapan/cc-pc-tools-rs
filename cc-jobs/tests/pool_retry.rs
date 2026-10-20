use cc_jobs::{Value, json, pool_retry::*};

fn accounts() -> Value {
    json!({"accounts":[
        {"id":"private-a","type":"codex","health":"Healthy","creditsAfterLimit":false,"quota":{"weeklyPercent":20.,"weeklyResetAt":2000.}},
        {"id":"private-b","type":"codex","health":"Healthy","creditsAfterLimit":false,"quota":{"weeklyPercent":2.,"weeklyResetAt":2000.}}
    ]})
}
fn job() -> Value {
    json!({"model":"gpt-6.1-sol","effort":"max","started_at":"1970-01-01T00:16:30Z"})
}
fn namespaces() -> Value {
    json!({"p000001":"private-a","p000002":"private-b"})
}
fn logs() -> Value {
    json!({"logs":[{"model":"gpt-6.1-sol","timestamp":999000.,"status":503,
    "accountLogLabel":"p000001","requestedEffort":"max","requestedServiceTier":"priority",
    "email":"not-for-storage","token":"not-for-storage","upstreamError":"not-for-storage"}]})
}

#[test]
fn public_metadata_selects_a_distinct_oauth_account_without_retaining_private_values() {
    let pool = snapshot(&accounts(), &namespaces(), &logs(), &job(), 1000.);
    assert_eq!(pool["verified"], true);
    assert_eq!(
        next_selector(&job(), &pool, 1000.),
        Some(Some("p000002".into()))
    );
    let text = pool.to_string();
    for private in [
        "private-a",
        "private-b",
        "not-for-storage",
        "email",
        "token",
        "upstreamError",
    ] {
        assert!(!text.contains(private), "{private}");
    }
    assert_eq!(pool["observation"]["requestedEffort"], "max");
}

#[test]
fn paused_exhausted_paid_keys_and_missing_permission_cannot_supply_an_alternate() {
    for (field, value) in [
        ("health", Value::Null),
        ("hasCredential", json!(false)),
        ("paused", json!(true)),
        ("needsReauth", json!(true)),
        ("type", json!("api_key")),
        ("creditsAfterLimit", json!(true)),
        ("creditsAfterLimit", Value::Null),
        ("quota", json!({"weeklyPercent":100.,"weeklyResetAt":2000.})),
        (
            "quota",
            json!({"weeklyPercent":5.,"weeklyResetAt":2000.,"fiveHourPercent":100.,"fiveHourResetAt":1500.}),
        ),
        (
            "quota",
            json!({"weeklyPercent":5.,"weeklyResetAt":2000.,"updatedAt":900.}),
        ),
    ] {
        let mut rows = accounts();
        rows["accounts"][1][field] = value;
        let pool = snapshot(&rows, &namespaces(), &logs(), &job(), 1000.);
        assert_eq!(next_selector(&job(), &pool, 1000.), Some(None), "{field}");
    }
}

#[test]
fn tool_text_stale_other_model_and_success_logs_never_establish_failed_account() {
    for patch in [
        json!({"status":200}),
        json!({"timestamp":800000.}),
        json!({"timestamp":1100000.}),
        json!({"model":"gpt-6-luna"}),
        json!({"accountLogLabel":"private-account"}),
    ] {
        let mut log = logs();
        log["logs"][0]
            .as_object_mut()
            .unwrap()
            .extend(patch.as_object().unwrap().clone());
        let pool = snapshot(&accounts(), &namespaces(), &log, &job(), 1000.);
        assert_eq!(next_selector(&job(), &pool, 1000.), None, "{patch}");
    }
}

#[test]
fn opaque_route_keeps_native_identity_and_never_rewrites_unknown_models() {
    let mut j = job();
    j["capacity_account_selector"] = json!("p000002");
    assert_eq!(launch_model(&j), "p000002/gpt-6.1-sol");
    j["model"] = json!("openai/gpt-6.1-sol");
    assert_eq!(launch_model(&j), "p000002/gpt-6.1-sol");
    j["model"] = json!("owner-special-model");
    assert_eq!(launch_model(&j), "owner-special-model");
    assert!(!public_selector("p000002/other"));
}

#[test]
fn retry_publication_rechecks_worker_identity_todo_owner_and_generation() {
    let original = json!({"id":"worker","status":"running","todo_id":3081,"pid":7,"pid_created":8,
        "codex_pid":9,"codex_pid_created":10,"todo_dependency_generation":2});
    assert!(same_attempt(&original, &original));
    for patch in [
        json!({"pid_created":9}),
        json!({"codex_pid_created":11}),
        json!({"cancel_requested":true}),
        json!({"status":"queued"}),
        json!({"launch_key":"new"}),
    ] {
        let mut fresh = original.clone();
        fresh
            .as_object_mut()
            .unwrap()
            .extend(patch.as_object().unwrap().clone());
        assert!(!same_attempt(&original, &fresh), "{patch}");
    }
    let db = json!({"items":[{"id":3081,"status":"doing","dependency_generation":2,"scheduler":{"job":"worker"}}]});
    assert!(todo_owns_attempt(&db, &original));
    for patch in [
        json!({"status":"block"}),
        json!({"status":"wait"}),
        json!({"stopped":true}),
        json!({"dependency_generation":3}),
        json!({"scheduler":{"job":"replacement"}}),
    ] {
        let mut changed = db.clone();
        changed["items"][0]
            .as_object_mut()
            .unwrap()
            .extend(patch.as_object().unwrap().clone());
        assert!(!todo_owns_attempt(&changed, &original), "{patch}");
    }
}

#[test]
fn observed_native_effort_is_separate_from_requested_effort_and_upstream_tier() {
    let mut requested = job();
    observe_turn_context(
        &mut requested,
        &json!({"model":"gpt-6.1-sol","effort":"high","token":"private"}),
    );
    assert_eq!(requested["effort"], "max");
    assert_eq!(requested["execution_observed"]["effort"], "high");
    assert!(requested["execution_observed"]["service_tier"].is_null());
    assert!(
        !requested["execution_observed"]
            .to_string()
            .contains("private")
    );
    cc_jobs::queue::prepare_resume(&mut requested, "", "1970-01-01T00:20:00Z");
    assert!(requested["execution_observed"].is_null());
}

#[test]
fn per_job_account_catalog_preserves_native_capabilities_and_explicit_effort() {
    let source = json!({"models":[{"slug":"gpt-6.1-sol","context_window":872000,
        "supports_reasoning_summaries":true,"default_reasoning_level":"low",
        "supported_reasoning_levels":[{"effort":"high"},{"effort":"max"}],
        "base_instructions":"native instructions"}]});
    let mut request = job();
    request["capacity_account_selector"] = json!("p000002");
    for effort in ["max", "high"] {
        request["effort"] = json!(effort);
        let alias = account_catalog(&source, &request).unwrap();
        assert_eq!(alias["models"][0]["slug"], "p000002/gpt-6.1-sol");
        assert_eq!(alias["models"][0]["context_window"], 872000);
        assert_eq!(alias["models"][0]["supports_reasoning_summaries"], true);
        assert_eq!(
            alias["models"][0]["base_instructions"],
            "native instructions"
        );
        assert_eq!(alias["models"][0]["default_reasoning_level"], effort);
    }
    assert_eq!(source["models"][0]["slug"], "gpt-6.1-sol");
    request["model"] = json!("owner-special-model");
    assert!(account_catalog(&source, &request).is_none());
}
