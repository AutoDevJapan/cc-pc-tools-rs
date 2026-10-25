use super::*;

#[test]
fn unstarted_policy_review_does_not_wait_for_a_depleted_default_when_oauth_is_available() {
    let mut sub = subwork_support::Fixture::new();
    sub.db["items"][1]["task_type"] = json!("review");
    sub.db["items"][1]["model"] = json!("gpt-6-luna");
    sub.db["items"][1]["model_origin"] = json!("policy");
    sub.db["items"][1]["effort"] = json!("max");
    sub.db["items"][1]["effort_origin"] = json!("policy");
    let mut f = Fixture::new();
    f.e.backend.records = sub.jobs.clone();
    f.e.backend.ps.clear();
    f.e.backend.quota_inventory = quota_recovery_tests::inventory(T);
    f.e.backend.m["quota_block_model"] = json!("anthropic/claude-opus-5-5");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let result = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap();
    assert!(result.is_some(), "healthy OAuth was never considered before waiting on the review default");
    assert_eq!(result.unwrap()["scheduler"]["selection"]["model"], "gpt-6-luna");
}
