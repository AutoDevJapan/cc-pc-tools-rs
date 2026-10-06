mod common;
use cc_ask::{core::*, helper_reaper::flush_log};
use common::Harness;
use serde_json::{json, Value};

fn pending(h: &Harness, helpers: u64, steam: u64) {
    atomic_json(
        &h.ctx.jobs.join("helper-memory-watch.json"),
        &json!({
            "samples":{}, "pending_helpers":helpers, "pending_steam":steam,
            "pending_bytes":5368709120u64, "last_log":0
        }),
        2,
    )
    .unwrap();
}
fn state(h: &Harness) -> Value {
    read_json(&h.ctx.jobs.join("helper-memory-watch.json")).unwrap()
}
#[test]
fn cleanup_batches_counts_silently_reuses_topic_and_does_not_replay_success() {
    let mut h = Harness::new();
    pending(&h, 3, 1);
    flush_log(&mut h.ctx).unwrap();
    assert_eq!(state(&h)["pending_helpers"], 0);
    assert_eq!(state(&h)["pending_steam"], 0);
    let count = h.fake.borrow().posts.len();
    assert_eq!(count, 1);
    for call in h
        .fake
        .borrow()
        .http_calls
        .iter()
        .filter(|call| call["method"] == "POST")
    {
        let body = &call["body"];
        let message = body.get("message").unwrap_or(body);
        assert_eq!(message["flags"], SILENT);
        assert_eq!(message["allowed_mentions"]["parse"], json!([]));
    }
    assert!(h.text().contains("3件") && h.text().contains("1件"));
    flush_log(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), count);
    let mut next = state(&h);
    next["pending_helpers"] = json!(2);
    atomic_json(&h.ctx.jobs.join("helper-memory-watch.json"), &next, 2).unwrap();
    h.fake.borrow_mut().now += 899.;
    flush_log(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), count);
    h.fake.borrow_mut().now += 1.;
    flush_log(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), count + 1);
    assert_eq!(state(&h)["pending_helpers"], 0);
}
#[test]
fn failed_delivery_preserves_counts_and_backs_off_for_fifteen_minutes() {
    let mut h = Harness::new();
    pending(&h, 4, 2);
    h.fake.borrow_mut().post_error = true;
    assert!(flush_log(&mut h.ctx).is_err());
    assert_eq!(state(&h)["pending_helpers"], 4);
    assert_eq!(state(&h)["pending_steam"], 2);
    let count = h.fake.borrow().http_calls.len();
    h.fake.borrow_mut().now += 60.;
    flush_log(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().http_calls.len(), count);
    h.fake.borrow_mut().post_error = false;
    h.fake.borrow_mut().now += 840.;
    flush_log(&mut h.ctx).unwrap();
    assert_eq!(state(&h)["pending_helpers"], 0);
    assert_eq!(state(&h)["pending_steam"], 0);
}
