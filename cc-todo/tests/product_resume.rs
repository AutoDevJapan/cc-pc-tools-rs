use cc_todo::product_resume::{qualify, snapshot};
use serde_json::{Value, json};
fn db() -> Value {
    json!({"projects":{"PhosGaea-Vol1":"original"},"items":[
        {"id":1626,"project":"PhosGaea-Vol1","status":"recovery_unverified","dispatchable":false,
         "spec":"original-spec","jobs":["old"],"wait_on":[2407],"resume_generation":7,
         "recovery":{"verified_status":"wait","source":"keep"},"scheduler":{"worktree":"original-tree"}},
        {"id":2407,"status":"todo","jobs":[]},
        {"id":17,"status":"recovery_unverified","private":"never change"}]})
}
fn receipt(db: &Value) -> Value {
    json!({"id":1626,"verified":true,"snapshot":snapshot(db,&db["items"][0]),
        "request_sha256":"verified-hash","session":"original-session","worktree":"original-tree"})
}
#[test]
fn qualification_preserves_dependency_original_values_and_every_other_item() {
    let mut db = db();
    let original = db.clone();
    let r = receipt(&db);
    let preview = qualify(&mut db, 1626, &r, false).unwrap();
    assert_eq!(db, original);
    assert_eq!(preview["pending_on"], json!([2407]));
    let report = qualify(&mut db, 1626, &r, true).unwrap();
    assert_eq!(report["launched"], 0);
    assert_eq!(db["items"][0]["status"], "wait");
    assert_eq!(
        db["items"][0]["product_resume_history"][0]["before"],
        original["items"][0]
    );
    for field in [
        "wait_on",
        "dispatchable",
        "spec",
        "jobs",
        "scheduler",
        "recovery",
        "resume_generation",
    ] {
        assert_eq!(db["items"][0][field], original["items"][0][field]);
    }
    assert_eq!(db["items"][1], original["items"][1]);
    assert_eq!(db["items"][2], original["items"][2]);
    let repeated = receipt(&db);
    let after = db.clone();
    assert_eq!(
        qualify(&mut db, 1626, &repeated, true).unwrap()["saved"],
        false
    );
    assert_eq!(db, after);
}
#[test]
fn stale_receipt_stops_completed_unknown_and_missing_dependency_state_never_mutate() {
    for field in ["spec", "jobs", "wait_on", "scheduler", "recovery"] {
        let mut db = db();
        let r = receipt(&db);
        db["items"][0][field] = json!("changed");
        let before = db.clone();
        assert!(qualify(&mut db, 1626, &r, true).is_err());
        assert_eq!(db, before);
    }
    for patch in [
        json!({"status":"done"}),
        json!({"status":"block"}),
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
        json!({"scheduler":{"owner_stop_job":"owner"}}),
        json!({"wait_on":[]}),
    ] {
        let mut db = db();
        for (k, v) in patch.as_object().unwrap() {
            db["items"][0][k] = v.clone();
        }
        let r = receipt(&db);
        let before = db.clone();
        assert!(qualify(&mut db, 1626, &r, true).is_err());
        assert_eq!(db, before);
    }
    let mut db = db();
    let r = receipt(&db);
    let before = db.clone();
    assert!(qualify(&mut db, 17, &r, true).is_err());
    assert_eq!(db, before);
}
