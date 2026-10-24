use super::*;

pub(super) fn inventory(at: f64) -> Value {
    json!({"at":at,"anthropic":{"at":at,"verified":true,"exhausted":true,"available":false},
        "openai":{"at":at,"verified":true,"exhausted":false,"available":true}})
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
        "sandbox":"read-only","mcp_profile":"none","network":false,"add_dirs":[],
        "approval_policy":"on-request","approvals_reviewer":"auto_review",
        "launch_key":"11111111111111111111111111111111",
        "started_at":"2026-10-24T12:00:00Z","finished_at":"2026-10-24T12:01:00Z",
        "finite_review":{"todo_id":2,"spec":row["spec"],"launch_key":"11111111111111111111111111111111"},
        "review_source_jobs":["parent"],"review_author_models":["gpt-6.1-sol"],
        "todo_dependency_generation":0,"todo_resume_generation":0}));
    let mut f = Fixture::new();
    f.e.backend.quota_inventory = inventory(T);
    f.e.backend.records = sub.jobs.clone();
    f.e.backend.ps.clear();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    (f, sub)
}

#[test]
fn verified_quota_review_keeps_policy_luna_in_scheduler_preparation() {
    let (mut f, sub) = fixture();
    let prepared = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap().unwrap();
    let selected = &prepared["scheduler"]["selection"];
    assert_eq!(selected["model"], "gpt-6-luna", "verified quota recovery returned to the depleted review default");
    assert_eq!(selected["model_origin"], "policy");
    assert_eq!(selected["effort"], "max");
    assert_eq!(selected["review_excluded_models"], json!(["gpt-6.1-sol"]));
    assert!(selected["quota_recovery"].is_object());
}
