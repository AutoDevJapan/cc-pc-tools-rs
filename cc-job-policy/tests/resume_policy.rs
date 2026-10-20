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
