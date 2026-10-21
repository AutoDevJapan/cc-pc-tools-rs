use cc_job_policy::review_result::{COMPLETE, completed, matches_item};
use serde_json::json;

#[test]
fn finite_root_review_closes_only_its_bound_spec_and_keeps_parent_work_separate() {
    let job = json!({"id":"reader","todo_id":3067,"task_type":"code_review","sandbox":"read-only",
        "launch_key":"fixed-request","finite_review":{"spec":"fixed-head.md","todo_id":3067,"launch_key":"fixed-request"}});
    assert!(completed(
        &job,
        &format!("{COMPLETE}\nMust0。親の検査と配布は未完です。")
    ));
    let item = json!({"id":3067,"spec":"fixed-head.md"});
    assert!(matches_item(&job, &item));
    let mut changed = item.clone();
    changed["spec"] = json!("new-head.md");
    assert!(!matches_item(&job, &changed));
    let mut writer = job.clone();
    writer["sandbox"] = json!("workspace-write");
    assert!(!completed(&writer, COMPLETE));
    let mut unbound = job.clone();
    unbound["finite_review"] = serde_json::Value::Null;
    assert!(!completed(&unbound, COMPLETE));
}
