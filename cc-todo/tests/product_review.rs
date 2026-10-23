use cc_scheduler::product_review as scope;
use cc_todo::product_resume::{bind_review_at, snapshot};
use serde_json::{Value, json};
const WITHIN_REVIEW_WINDOW: f64 = 1791849601.;
const AFTER_REVIEW_WINDOW: f64 = 1792540800.;
fn fixture() -> (Value, Value, Value) {
    let owner = json!({"repository":"autodevjapan/phosgaea-vol1","git_common":"recovered-common",
        "worktree":"original-tree","branch":"original-branch"});
    let mut db = json!({"projects":{"PhosGaea-Vol1":scope::ROOT},"items":[
        {"id":1626,"status":"wait","wait_on":[2407],"jobs":[scope::ORIGIN]},
        {"id":2407,"project":"PhosGaea-Vol1","status":"todo","dispatchable":false,"spec":"original-copy",
         "jobs":[],"needs":null,"scheduler":{"key":scope::KEY,"console_request":{"original":"keep"}},
         "subwork":{"parent_todo_id":1626,"origin_job_id":scope::ORIGIN,"kind":"review","owner":owner}},
        {"id":17,"status":"recovery_unverified","dispatchable":false}]});
    db["items"][1]["subwork"]["owner"]["git_common"] = json!("old-common");
    let receipt = json!({"verified":true,"id":2407,"snapshot":snapshot(&db,&db["items"][1]),
        "request_sha256":scope::HASH,"job":scope::ORIGIN});
    (db, receipt, owner)
}
#[test]
fn only_original_review_changes_and_history_is_complete() {
    let (mut db, r, owner) = fixture();
    let before = db.clone();
    assert!(bind_review_at(&mut db, &r, &owner, WITHIN_REVIEW_WINDOW).unwrap());
    assert_eq!(db["items"][0], before["items"][0]);
    assert_eq!(db["items"][2], before["items"][2]);
    let row = &db["items"][1];
    assert_eq!(
        row["product_review_allocation"]["before"],
        before["items"][1]
    );
    assert_eq!(row["scheduler"]["key"], scope::KEY);
    assert_eq!(
        row["scheduler"]["console_request"],
        before["items"][1]["scheduler"]["console_request"]
    );
    assert!(row["scheduler"]["subwork_admission"].is_null());
    assert!(scope::eligible(row, 1791849601.));
    assert!(!scope::eligible(row, 1792540800.));
    let mut r = r;
    r["snapshot"] = snapshot(&db, &db["items"][1]);
    let after = db.clone();
    assert!(!bind_review_at(&mut db, &r, &owner, WITHIN_REVIEW_WINDOW).unwrap());
    assert_eq!(db, after);
    assert!(bind_review_at(&mut db, &r, &owner, AFTER_REVIEW_WINDOW).is_err());
    assert_eq!(db, after);
}
#[test]
fn changed_scope_stops_dependencies_workers_and_preparation_never_write() {
    for patch in [
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
        json!({"decision_wait":{}}),
        json!({"status":"done"}),
        json!({"owner_postponement_recorded":true}),
        json!({"stop_kind":"manual"}),
        json!({"needs":["cargo"]}),
        json!({"wait_on":[1626]}),
        json!({"jobs":["already"]}),
        json!({"scheduler":{"key":"different"}}),
        json!({"scheduler":{"key":scope::KEY,"worktree":"prepared"}}),
        json!({"subwork":{"parent_todo_id":17,"origin_job_id":scope::ORIGIN,"kind":"review"}}),
    ] {
        let (mut db, mut r, owner) = fixture();
        for (k, v) in patch.as_object().unwrap() {
            db["items"][1][k] = v.clone();
        }
        r["snapshot"] = snapshot(&db, &db["items"][1]);
        let before = db.clone();
        assert!(
            bind_review_at(&mut db, &r, &owner, WITHIN_REVIEW_WINDOW).is_err(),
            "{patch}"
        );
        assert_eq!(db, before);
    }
    for field in ["spec", "jobs", "scheduler", "subwork"] {
        let (mut db, r, owner) = fixture();
        db["items"][1][field] = json!("changed");
        let before = db.clone();
        assert!(bind_review_at(&mut db, &r, &owner, WITHIN_REVIEW_WINDOW).is_err());
        assert_eq!(db, before);
    }
}
#[test]
fn prepared_binding_cannot_be_reused_with_other_inputs() {
    let (mut db, r, owner) = fixture();
    bind_review_at(&mut db, &r, &owner, WITHIN_REVIEW_WINDOW).unwrap();
    let row = &db["items"][1];
    let job = json!({"todo_id":2407,"launch_key":scope::KEY});
    assert!(cc_job_policy::subwork::finite_review_launch(
        row, &job, 1791849601
    ));
    assert!(!cc_job_policy::subwork::finite_review_launch(
        row, &job, 1792540800
    ));
    let mut other = job.clone();
    other["launch_key"] = json!("new-key");
    assert!(!cc_job_policy::subwork::finite_review_launch(
        row, &other, 1791849601
    ));
    for field in ["spec", "needs", "subwork", "id", "project"] {
        let mut row = db["items"][1].clone();
        row[field] = json!("other");
        assert!(!scope::eligible(&row, 1791849601.));
    }
}

#[test]
fn repeated_binding_expiry_uses_observed_time_and_never_changes_state() {
    let (mut db, mut receipt, owner) = fixture();
    bind_review_at(&mut db, &receipt, &owner, WITHIN_REVIEW_WINDOW).unwrap();
    receipt["snapshot"] = snapshot(&db, &db["items"][1]);
    let before = db.clone();
    assert!(!bind_review_at(&mut db, &receipt, &owner, 1792540799.).unwrap());
    for at in [1792540800., 1792540801.] {
        assert!(bind_review_at(&mut db, &receipt, &owner, at).is_err());
        assert_eq!(db, before);
    }
}
