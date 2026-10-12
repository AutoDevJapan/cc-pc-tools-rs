mod common;
use cc_ask::{core::*, state_work::StateWork};
use common::Harness;
use serde_json::json;

#[test]
fn disappeared_ledger_is_not_recreated_by_a_stale_worker() {
    let h = Harness::new();
    h.save(&json!({"questions":[],"answer_receipts":{"123":{"read_by":"original"}}}));
    let _worker = StateWork::begin(&h.ctx).unwrap();
    let mut proposed = h.state();
    proposed["reply_delivery_version"] = json!(3);
    let path = h.ctx.here.join("state.json");
    std::fs::remove_file(&path).unwrap();
    assert!(h
        .ctx
        .load_fresh()
        .unwrap_err()
        .contains("question_state_io"));
    assert!(h.ctx.save(&proposed).is_err());
    assert!(!path.exists());
}

#[test]
fn valid_json_with_invalid_ledger_shape_is_not_an_empty_store() {
    let h = Harness::new();
    for value in [json!(1), json!({}), json!({"questions":null})] {
        atomic_json(&h.ctx.here.join("state.json"), &value, 2).unwrap();
        assert!(h.ctx.load_fresh().unwrap_err().contains("invalid_schema"));
        assert!(StateWork::begin(&h.ctx).is_err());
        assert_eq!(read_json(&h.ctx.here.join("state.json")).unwrap(), value);
    }
}

#[test]
fn explicit_recovery_latch_allows_get_but_blocks_post_launch_and_state_reset() {
    let mut h = Harness::new();
    let before = h.state();
    atomic_json(
        &h.ctx.here.join("question-recovery-required.json"),
        &json!({"reason":"lost original records"}),
        2,
    )
    .unwrap();
    assert_eq!(h.ctx.call("GET", "/users/@me", None).unwrap()["id"], "BOT");
    assert!(h
        .ctx
        .call(
            "POST",
            "/channels/100/messages",
            Some(&json!({"content":"no new question"}))
        )
        .unwrap_err()
        .contains("RecoveryRequired"));
    assert!(h
        .ctx
        .run(&["launch".into()], 1)
        .unwrap_err()
        .contains("RecoveryRequired"));
    assert!(h
        .ctx
        .save(&json!({"questions":[]}))
        .unwrap_err()
        .contains("RecoveryRequired"));
    assert!(StateWork::begin(&h.ctx).is_err());
    assert!(
        atomic_json(&h.ctx.here.join("state.json"), &json!({"questions":[]}), 2)
            .unwrap_err()
            .contains("RecoveryRequired")
    );
    assert!(cc_ask::question_progress::ask(
        &mut h.ctx,
        &json!({"todo_id":1}),
        "",
        "追加の判断ですか？",
        &[]
    )
    .unwrap_err()
    .contains("RecoveryRequired"));
    assert_eq!(h.state(), before);
    assert!(!h.ctx.here.join("question-progress").exists());
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .any(|c| c["method"] == "GET"));
    assert!(!h
        .fake
        .borrow()
        .http_calls
        .iter()
        .any(|c| c["method"] == "POST"));
    assert!(h.fake.borrow().runs.is_empty());
}
