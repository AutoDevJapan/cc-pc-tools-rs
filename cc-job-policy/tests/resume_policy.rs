use cc_job_policy::apply_managed_launch_policy;
use serde_json::json;

#[test]
fn policy_migration_starts_fresh_and_a_later_requeue_cannot_restore_the_old_context() {
    for model in ["gpt-6-luna", "gpt-6-astra", "gpt-6.1-sol"] {
        let mut job = json!({"todo_id":3081,"model":model,"effort":"high",
            "model_origin":"policy","effort_origin":"policy","model_explicit":false,
            "resume_session":"old-session","thread_id":"old-session",
            "first_started_at":"saved-start","cwd":"D:/fixture/work","sandbox":"read-only",
            "prompt":"original.md","capacity_retry_count":0});
        apply_managed_launch_policy(&mut job);
        assert_eq!(job["model"], "gpt-6.1-sol");
        assert_eq!(job["effort"], "max");
        assert!(
            job["resume_session"].is_null(),
            "old model/effort context survived: {model}"
        );
        assert_eq!(job["cwd"], "D:/fixture/work");
        assert_eq!(job["sandbox"], "read-only");
        assert_eq!(job["prompt"], "original.md");
        assert_eq!(job["first_started_at"], "saved-start");
        assert_eq!(job["capacity_retry_count"], 0);

        // A restart can reconstruct resume_session from the old thread ID
        // before the first fresh native worker has published its new ID.
        job["resume_session"] = json!("old-session");
        apply_managed_launch_policy(&mut job);
        assert!(job["resume_session"].is_null());
        job["thread_id"] = json!("new-session");
        job["resume_session"] = json!("new-session");
        apply_managed_launch_policy(&mut job);
        assert_eq!(job["resume_session"], "new-session");
    }
}

#[test]
fn individual_owner_selection_keeps_the_model_effort_and_compatible_context() {
    let mut job = json!({"todo_id":3081,"model":"gpt-6-luna","effort":"high",
        "model_origin":"owner_request","effort_origin":"owner_request",
        "resume_session":"owner-session","thread_id":"owner-session"});
    let original = job.clone();
    apply_managed_launch_policy(&mut job);
    assert_eq!(job, original);
}

#[test]
fn an_eligible_same_model_capacity_attempt_keeps_its_context_and_durable_budget() {
    let mut job = json!({"todo_id":3081,"model":"gpt-6.1-sol","effort":"max",
        "model_origin":"policy","effort_origin":"policy","resume_session":"same-session",
        "capacity_wait_pending":true,"capacity_same_model_retry_count":2,
        "capacity_retry_count":0,"retry_not_before":12345,"cwd":"D:/fixture/work",
        "sandbox":"read-only","capacity_history":[{"attempt":2,"model":"gpt-6.1-sol"}]});
    let original = job.clone();
    apply_managed_launch_policy(&mut job);
    assert_eq!(job, original);
}

#[test]
fn haiku_does_not_acquire_an_unverified_max_requirement() {
    let mut job = json!({"todo_id":3081,"model":"anthropic/claude-haiku-5-5",
        "effort":"high","model_origin":"policy","effort_origin":"policy"});
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["model"], "anthropic/claude-haiku-5-5");
    assert_eq!(job["effort"], "high");
}

#[test]
fn exact_source_contracts_are_not_rewritten_as_unqualified_launches() {
    for field in [
        "original_product_retry",
        "fixed_read_only_review",
        "fixed_review_requalification",
    ] {
        let mut job = json!({"todo_id":3081,"model":"gpt-6-luna","effort":"high",
            "model_origin":"policy","effort_origin":"policy","resume_session":"bound-session"});
        job[field] = json!({"source_job_id":"fixed-source"});
        let original = job.clone();
        apply_managed_launch_policy(&mut job);
        assert_eq!(job, original);
        assert_eq!(cc_job_policy::effective_effort(&job), "high");
    }
}

#[test]
fn a_previously_normalized_legacy_job_retires_its_context_only_once() {
    let mut job = json!({"todo_id":3081,"model":"gpt-6.1-sol","effort":"max",
        "model_origin":"policy","effort_origin":"policy","legacy_model_hint":"gpt-6-luna",
        "legacy_effort_hint":"high","thread_id":"old-session","resume_session":"old-session"});
    apply_managed_launch_policy(&mut job);
    assert!(job["resume_session"].is_null());
    job["thread_id"] = json!("fresh-session");
    job["resume_session"] = json!("fresh-session");
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["resume_session"], "fresh-session");
}

#[test]
fn a_freshly_migrated_launch_keeps_its_first_compatible_resume() {
    let mut job = json!({"todo_id":3081,"model":"gpt-6-luna","effort":"high",
        "model_origin":"policy","effort_origin":"policy"});
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["model"], "gpt-6.1-sol");
    assert_eq!(job["effort"], "max");
    job["thread_id"] = json!("first-native-session");
    job["resume_session"] = json!("first-native-session");
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["resume_session"], "first-native-session");
}

#[test]
fn automatic_cli_hints_also_use_the_managed_policy_without_a_todo() {
    let mut job = json!({"model":"gpt-6-luna","effort":"high","model_explicit":false});
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["model"], "gpt-6.1-sol");
    assert_eq!(job["effort"], "max");
}

#[test]
fn model_repair_exclusions_cannot_route_back_to_the_known_failed_default() {
    let mut job = json!({"model":"gpt-6-astra","effort":"high","model_explicit":false,
        "review_excluded_models":["openai/gpt-6.1-sol"]});
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["model"], "gpt-6-astra");
    assert_eq!(job["effort"], "max");
}

#[test]
fn retired_context_filters_both_saved_ids_but_preserves_the_next_compatible_thread() {
    use cc_job_policy::launch_context::{usable_resume_session, usable_thread_session};
    let retired = "12345678-abcd-4321-9234-123456789abc";
    let mut source = json!({"thread_id":retired.to_uppercase(),"resume_session":retired,
        "policy_resume_reset":{"session":retired}});
    assert!(usable_thread_session(&source).is_none());
    assert!(usable_resume_session(&source).is_none());
    source["thread_id"] = json!("new-thread");
    source["resume_session"] = json!("new-thread");
    assert_eq!(usable_thread_session(&source), Some("new-thread"));
    assert_eq!(usable_resume_session(&source), Some("new-thread"));
}

#[test]
fn one_launch_model_recovery_hint_does_not_change_author_exclusions_or_freeze_the_default() {
    let mut job = json!({"model":"gpt-6-astra","effort":"high","model_explicit":false,
        "model_origin":"policy","model_recovery_from":"gpt-6.1-sol"});
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["model"], "gpt-6-astra");
    assert_eq!(job["effort"], "max");
    assert!(job["review_excluded_models"].is_null());
    job.as_object_mut().unwrap().remove("model_recovery_from");
    apply_managed_launch_policy(&mut job);
    assert_eq!(job["model"], "gpt-6.1-sol");
}
