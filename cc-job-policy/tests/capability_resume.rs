use cc_job_policy::launch_context;
use serde_json::json;

#[test]
fn capability_changes_retire_the_conversation_and_keep_work_and_results() {
    let source = json!({"model":"gpt-6.1-sol","effort":"max",
        "sandbox":"read-only","mcp_profile":"minimal"});
    for (sandbox, mcp, changed) in [
        ("read-only", "minimal", false),
        ("inherit", "minimal", true),
        ("read-only", "inherit", true),
        ("inherit", "inherit", true),
    ] {
        let mut next = json!({"model":"gpt-6.1-sol","effort":"max",
            "sandbox":sandbox,"mcp_profile":mcp,"resume_session":"old-session",
            "todo_id":42,"cwd":"saved-work","result":"saved-result"});
        launch_context::reconcile_resume(&mut next, &source);
        assert_eq!(next["resume_session"].is_null(), changed);
        assert_eq!(next["todo_id"], 42);
        assert_eq!(next["cwd"], "saved-work");
        assert_eq!(next["result"], "saved-result");
        assert_eq!(
            launch_context::retired_session(&next, "old-session"),
            changed
        );
    }
}

#[test]
fn exact_contracts_and_same_job_capacity_retries_keep_their_conversation() {
    let source = json!({"sandbox":"read-only","mcp_profile":"minimal"});
    for exemption in [
        "fixed_read_only_review",
        "original_product_retry",
        "capacity_wait_pending",
    ] {
        let mut next =
            json!({"sandbox":"inherit","mcp_profile":"inherit","resume_session":"same-session"});
        next[exemption] = if exemption == "capacity_wait_pending" {
            json!(true)
        } else {
            json!({"bound":true})
        };
        launch_context::reconcile_resume(&mut next, &source);
        assert_eq!(next["resume_session"], "same-session");
    }
}
