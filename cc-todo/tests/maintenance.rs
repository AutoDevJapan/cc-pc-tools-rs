use cc_scheduler::policy::Processes;
use cc_todo::{Api, Paths, Result, recovery, summary};
use serde_json::{Value, json};
use std::fs;

fn database() -> Value {
    json!({"projects":{"demo":"D:/demo"},"items":[
        {"id":1,"project":"demo","status":"doing","title":"実装","spec":"saved","jobs":["old1"],"created_s":1},
        {"id":2,"project":"demo","status":"doing","title":"検証","spec":"saved","jobs":["old2"],"created_s":1}],"stalls":{}})
}

#[test]
fn two_synthetic_items_preview_then_admission_survive_restart_without_two_owners() {
    let mut db = database();
    let jobs = vec![
        json!({"id":"old1","status":"failed","todo_id":1,"cwd":"D:/demo","created_at":1,"finished_at":1}),
        json!({"id":"old2","status":"succeeded","todo_id":2,"cwd":"D:/demo","created_at":1,"finished_at":1}),
    ];
    let queue = json!({"queue":[],"launches":{}});
    let before = db.clone();
    let preview =
        recovery::preview(&db, &jobs, &Processes::new(), &queue, 10000., "", &[1, 2]).unwrap();
    assert_eq!(preview["would_reserve"], json!([1, 2]));
    assert_eq!(preview["items"].as_array().unwrap().len(), 2);
    assert_eq!(db, before);
    assert_eq!(
        recovery::plan(&mut db, &jobs, &Processes::new(), 10000., "").unwrap(),
        vec![1, 2]
    );
    let pending = db["items"].clone();
    assert!(
        recovery::plan(&mut db, &jobs, &Processes::new(), 10001., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(db["items"], pending);
    let accepted = json!({"queue":[{"todo_id":1,"key":"manual:one","cwd":"D:/demo"},{"todo_id":2,"key":"manual:two","cwd":"D:/demo"}],"launches":{}});
    let preview = recovery::preview(
        &db,
        &jobs,
        &Processes::new(),
        &accepted,
        11000.,
        "",
        &[1, 2],
    )
    .unwrap();
    assert_eq!(preview["would_reserve"], json!([]));
    assert!(
        preview["items"]
            .as_array()
            .unwrap()
            .iter()
            .all(|r| r["accepted"] == true)
    );
    assert_eq!(db["items"], pending);
    let owned = vec![
        json!({"id":"one","status":"queued","todo_id":1}),
        json!({"id":"two","status":"starting","todo_id":2}),
    ];
    // Remove reservations so an accepted live owner is the only exclusion.
    db = before.clone();
    let preview =
        recovery::preview(&db, &owned, &Processes::new(), &queue, 11000., "", &[1, 2]).unwrap();
    assert!(
        preview["items"]
            .as_array()
            .unwrap()
            .iter()
            .all(|r| r["owned_jobs"].as_array().unwrap().len() == 1)
    );
    assert_eq!(preview["would_reserve"], json!([]));
}

#[test]
fn acceptance_summary_requires_measured_two_item_result_and_exactly_one_owner() {
    let valid = json!({"dry_run":true,"sent":0,"launched":0,"saved":false,"would_reserve":[],"items":[{"id":57,"status":"doing","owned_jobs":["actual-job"]},{"id":791,"status":"done","owned_jobs":[]}]});
    assert!(
        recovery::acceptance_summary(&valid)
            .unwrap()
            .contains("実物2件")
    );
    for (field, value) in [
        ("sent", json!(1)),
        ("launched", json!(1)),
        ("saved", json!(true)),
        ("would_reserve", json!([57])),
    ] {
        let mut invalid = valid.clone();
        invalid[field] = value;
        assert!(recovery::acceptance_summary(&invalid).is_err());
    }
    for owned in [json!([]), json!(["one", "two"])] {
        let mut invalid = valid.clone();
        invalid["items"][0]["owned_jobs"] = owned;
        assert!(recovery::acceptance_summary(&invalid).is_err());
    }
    let mut invalid = valid.clone();
    invalid["items"][1]["status"] = json!("doing");
    assert!(recovery::acceptance_summary(&invalid).is_err());
}

#[test]
fn old_182_completed_or_owner_held_items_never_enter_admission() {
    let mut db = database();
    db["items"] = json!((1..=182).map(|id|json!({"id":id,"status":if id%2==0 {"done"} else {"block"},"project":"demo","jobs":[],"spec":"saved","stall_recovery":{"report":"古い質問"}})).collect::<Vec<_>>());
    let before = db.clone();
    let out = recovery::preview(
        &db,
        &[],
        &Processes::new(),
        &json!({"queue":[],"launches":{}}),
        10000.,
        "",
        &[],
    )
    .unwrap();
    assert_eq!(out["items"].as_array().unwrap().len(), 182);
    assert_eq!(out["would_reserve"], json!([]));
    assert_eq!(db, before);
}

#[test]
fn selecting_one_item_preserves_its_completed_dependency_and_rejects_missing_ids() {
    let mut db = database();
    db["items"][0]["wait_on"] = json!([2]);
    db["items"][1]["status"] = json!("done");
    let queue = json!({"queue":[],"launches":{}});
    let out = recovery::preview(&db, &[], &Processes::new(), &queue, 10000., "", &[1]).unwrap();
    assert_eq!(out["items"].as_array().unwrap().len(), 1);
    assert_eq!(out["would_reserve"], json!([1]));
    assert!(recovery::preview(&db, &[], &Processes::new(), &queue, 10000., "", &[999]).is_err());
    db["items"][0]["id"] = json!("bad");
    assert!(recovery::preview(&db, &[], &Processes::new(), &queue, 10000., "", &[]).is_err());
}

struct Discord {
    calls: Vec<(String, String, Value)>,
    message: Value,
    lose_patch: bool,
}
impl Discord {
    fn new() -> Self {
        Self {
            calls: vec![],
            message: json!({"id":"123","author":{"id":"BOT"},"content":"既存の記録"}),
            lose_patch: false,
        }
    }
}
impl Api for Discord {
    fn call(&mut self, method: &str, path: &str, body: &Value) -> Result<Value> {
        self.calls.push((method.into(), path.into(), body.clone()));
        match (method, path) {
            ("GET", "/users/@me") => Ok(json!({"id":"BOT","bot":true})),
            ("GET", "/channels/123/messages/123") => Ok(self.message.clone()),
            ("PATCH", "/channels/123") => Ok(json!({"id":"123"})),
            ("PATCH", "/channels/123/messages/123") => {
                self.message["content"] = body["content"].clone();
                if self.lose_patch {
                    self.lose_patch = false;
                    Err("PATCH succeeded but response was lost".into())
                } else {
                    Ok(self.message.clone())
                }
            }
            _ => panic!("unexpected side effect: {method} {path}"),
        }
    }
}
#[test]
fn two_reports_edit_one_existing_message_and_restart_does_not_replay_a_lost_response() {
    let temp = tempfile::tempdir().unwrap();
    let paths = Paths::at(temp.path(), temp.path());
    fs::write(&paths.thread, "123").unwrap();
    let mut api = Discord::new();
    summary::publish_report(
        &mut api,
        &json!({}),
        &paths,
        "1:source-post-111",
        "作業1を再開しました",
    )
    .unwrap();
    api.lose_patch = true;
    assert!(
        summary::publish_report(
            &mut api,
            &json!({}),
            &paths,
            "2:source-post-222",
            "作業2を再開しました"
        )
        .is_err()
    );
    let patch_count = api
        .calls
        .iter()
        .filter(|(m, p, _)| m == "PATCH" && p.ends_with("/messages/123"))
        .count();
    // Drop memory of the previous attempt. The persisted desired content and
    // actual Discord starter, not a nonce window, decide whether another edit is due.
    summary::publish_report(
        &mut api,
        &json!({}),
        &paths,
        "2:source-post-222",
        "作業2を再開しました",
    )
    .unwrap();
    summary::publish_report(
        &mut api,
        &json!({}),
        &paths,
        "1:source-post-111",
        "作業1を再開しました",
    )
    .unwrap();
    assert_eq!(
        api.calls
            .iter()
            .filter(|(m, p, _)| m == "PATCH" && p.ends_with("/messages/123"))
            .count(),
        patch_count
    );
    assert_eq!(patch_count, 2);
    assert!(!api.calls.iter().any(|(m, _, _)| m == "POST"));
    let text = api.message["content"].as_str().unwrap();
    assert!(text.contains("作業1") && text.contains("作業2"));
    for (_, _, body) in api
        .calls
        .iter()
        .filter(|(m, p, _)| m == "PATCH" && p.ends_with("/messages/123"))
    {
        assert_eq!(body["allowed_mentions"], json!({"parse":[]}));
        assert!(
            body.get("flags").is_none(),
            "editing content must preserve the existing silent message flags"
        );
    }
}

#[test]
fn missing_or_foreign_starter_and_corrupt_ledger_never_create_or_edit_a_message() {
    let temp = tempfile::tempdir().unwrap();
    let paths = Paths::at(temp.path(), temp.path());
    let mut api = Discord::new();
    assert!(summary::publish_report(&mut api, &json!({}), &paths, "1:source", "記録").is_err());
    assert!(api.calls.is_empty());
    fs::write(&paths.thread, "123").unwrap();
    api.message["author"]["id"] = json!("OTHER");
    assert!(summary::publish_report(&mut api, &json!({}), &paths, "1:source", "記録").is_err());
    assert!(api.calls.iter().all(|(m, _, _)| m == "GET"));
    fs::write(temp.path().join("stall_summary.json"), "{broken").unwrap();
    api.calls.clear();
    assert!(summary::publish_report(&mut api, &json!({}), &paths, "1:source", "記録").is_err());
    assert!(api.calls.is_empty());
}
