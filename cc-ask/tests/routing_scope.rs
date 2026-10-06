mod common;
use cc_ask::{commands::*, core::*, resume, routing};
use common::{message, Harness};
use serde_json::{json, Value};

fn cursor() -> Value {
    json!({"messages":{},"cursor":{"since":"100","places":{"memo":"#メモ","legacy":"メモ > 追加の依頼"},"channels":{"memo":"99"}}})
}
fn inventory(h: &mut Harness) {
    let mut f = h.fake.borrow_mut();
    f.threads.clear();
    f.messages.clear();
    f.channels = vec![
        json!({"id":"memo","name":"メモ","type":0}),
        json!({"id":"news","name":"news","type":5}),
        json!({"id":"other","name":"private forum","type":15}),
        json!({"id":"T","name":"作業","type":15}),
        json!({"id":REQUEST_CHANNEL_ID,"name":"依頼","type":15}),
    ];
    for (id, parent, owner, archived) in [
        ("request", REQUEST_CHANNEL_ID, "OWN", false),
        ("task", "T", "BOT", true),
        ("human", "T", "OWN", false),
        ("otherbot", "T", "OTHERBOT", false),
        ("legacy", "memo", "BOT", false),
        ("personal", "other", "OWN", false),
        ("question", "memo", "BOT", false),
    ] {
        f.threads.insert(id.into(), json!({"id":id,"parent_id":parent,"owner_id":owner,"type":11,"name":id,"last_message_id":"1000","thread_metadata":{"archived":archived}}));
    }
    for (i, id) in [
        "memo", "news", "request", "task", "human", "otherbot", "legacy", "personal", "question",
    ]
    .iter()
    .enumerate()
    {
        f.messages.insert(
            (*id).into(),
            vec![message(&(101 + i).to_string(), "続けて", "OWN")],
        );
    }
    drop(f);
    h.save(&json!({"questions":[{"thread_id":"question","open":false,"after":"1"},{"thread_id":"legacy","open":true,"source":"routing","after":"1"}]}));
}
fn assert_no_personal_reads(h: &Harness) {
    let calls = &h.fake.borrow().http_calls;
    for id in ["memo", "news", "human", "otherbot", "legacy", "personal"] {
        assert!(
            !calls
                .iter()
                .any(|c| s(&c["url"]).contains(&format!("/channels/{id}/messages"))),
            "unexpected message read: {id}"
        );
    }
    for id in ["memo", "news", "other"] {
        assert!(
            !calls
                .iter()
                .any(|c| s(&c["url"]).contains(&format!("/channels/{id}/threads/archived"))),
            "unexpected archive read: {id}"
        );
    }
}
#[test]
fn collector_rebuilds_cached_places_and_only_reads_authorized_threads() {
    let mut h = Harness::new();
    inventory(&mut h);
    let mut ledger = cursor();
    assert_eq!(routing::collect(&mut h.ctx, &mut ledger).unwrap(), 3);
    let mut ids = ledger["messages"]
        .as_object()
        .unwrap()
        .keys()
        .cloned()
        .collect::<Vec<_>>();
    ids.sort();
    assert_eq!(ids, ["103", "104", "109"]);
    assert_eq!(
        ledger["cursor"]["places"]
            .as_object()
            .unwrap()
            .keys()
            .cloned()
            .collect::<Vec<_>>(),
        ["question", "request", "task"]
    );
    assert_eq!(ledger["cursor"]["channels"]["memo"], "99");
    assert_no_personal_reads(&h);
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
}
#[test]
fn main_inbox_uses_the_same_scope_including_closed_questions() {
    let mut h = Harness::new();
    inventory(&mut h);
    let mark = h.root.path().join("inbox.txt");
    std::fs::write(&mark, "100").unwrap();
    let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
    assert_eq!(
        out.iter().map(|m| s(&m["id"])).collect::<Vec<_>>(),
        ["103", "104", "109"]
    );
    assert_eq!(std::fs::read_to_string(mark).unwrap(), "109");
    assert_no_personal_reads(&h);
}
#[test]
fn legacy_preclassified_backlog_never_posts_selects_or_launches() {
    for kind in [Value::Null, json!("new"), json!("task"), json!("question")] {
        let mut h = Harness::new();
        inventory(&mut h);
        h.fake.borrow_mut().messages.clear();
        let now = h.ctx.now();
        let mid = (((now * 1000. - 1420070400000.) as u64) << 22).to_string();
        atomic_json(
            &h.ctx.here.join("automation.json"),
            &json!({"enabled":true,"since":now-3600.}),
            2,
        )
        .unwrap();
        let mut ledger = cursor();
        let original = json!({"message":{"id":mid,"channel":"memo","text":"個人のメモ"},"kind":kind,"key":"old-key","thread":{"id":"legacy","type":11},"cwd":h.root.path(),"acknowledged":true,"ready":true});
        ledger["messages"][&mid] = original.clone();
        atomic_json(&h.ctx.here.join("routing_state.json"), &ledger, 2).unwrap();
        let report = routing::run(&mut h.ctx, &mut false).unwrap();
        assert_eq!(
            report,
            json!({"received":0,"launched":[],"errors":0,"excluded":1})
        );
        let stored = read_json(&h.ctx.here.join("routing_state.json")).unwrap();
        assert_eq!(stored["messages"][&mid]["message"], original["message"]);
        assert_eq!(
            stored["messages"][&mid]["excluded"],
            "outside_request_surfaces"
        );
        assert_eq!(stored["messages"][&mid]["done"], true);
        assert_eq!(stored["messages"][&mid]["ready"], false);
        let f = h.fake.borrow();
        assert!(f.posts.is_empty());
        assert!(f.patches.is_empty());
        assert!(f.picks.is_empty());
        assert!(f.launches.is_empty());
        drop(f);
        assert_no_personal_reads(&h);
    }
}
#[test]
fn legacy_routing_question_does_not_reenter_reply_resume_or_reminder_handlers() {
    let mut h = Harness::new();
    inventory(&mut h);
    h.save(&json!({"questions":[{"thread_id":"legacy","open":true,"source":"routing","after":"1","cwd":h.root.path(),"continuation":{"launch":{"key":"old-launch"},"after":"1"}}]}));
    assert_eq!(cmd_replies(&mut h.ctx, None).unwrap(), json!([]));
    cmd_remind(&mut h.ctx).unwrap();
    resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    let f = h.fake.borrow();
    assert!(f.posts.is_empty());
    assert!(f.patches.is_empty());
    assert!(f.picks.is_empty());
    assert!(f.launches.is_empty());
    drop(f);
    assert_no_personal_reads(&h);
    assert_eq!(h.state()["questions"][0]["after"], "1");
}
#[test]
fn unavailable_bot_identity_never_reads_any_messages_or_advances_cursor() {
    let mut h = Harness::new();
    inventory(&mut h);
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/users/@me".into(), 1);
    let mut ledger = cursor();
    let before = ledger.clone();
    assert!(routing::collect(&mut h.ctx, &mut ledger).is_err());
    assert_eq!(ledger, before);
    assert!(!h
        .fake
        .borrow()
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).contains("/messages")));
}
#[test]
fn routing_generated_question_on_bot_task_still_accepts_replies() {
    let mut h = Harness::new();
    inventory(&mut h);
    h.save(
        &json!({"questions":[{"thread_id":"task","source":"routing","open":true,"after":"100"}]}),
    );
    let out = cmd_replies(&mut h.ctx, Some("task")).unwrap();
    assert_eq!(out[0]["thread_id"], "task");
    assert_eq!(out[0]["replies"][0]["content"], "続けて");
    assert_eq!(h.state()["questions"][0]["seen_reply"], "104");
}

#[test]
fn retired_history_does_not_create_hundreds_of_individual_metadata_requests() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.clear();
    h.fake.borrow_mut().messages.clear();
    let qs: Vec<_> = (1..1001)
        .map(|id| json!({"thread_id":format!("retired-{id}"),"open":false}))
        .collect();
    h.save(&json!({"questions":qs}));
    let mut ledger = cursor();
    assert_eq!(routing::collect(&mut h.ctx, &mut ledger).unwrap(), 0);
    let f = h.fake.borrow();
    assert_eq!(f.http_calls.len(), 2);
    assert!(!f
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).contains("/channels/")));
}

#[test]
fn pending_private_question_absent_from_inventory_is_fetched_directly() {
    let mut h = Harness::new();
    inventory(&mut h);
    h.fake.borrow_mut().threads.get_mut("question").unwrap()["thread_metadata"]["archived"] =
        json!(true);
    h.save(&json!({"questions":[{"thread_id":"question","open":true,"after":"1"}]}));
    let mut ledger = cursor();
    assert_eq!(routing::collect(&mut h.ctx, &mut ledger).unwrap(), 3);
    assert_eq!(ledger["messages"]["109"]["message"]["channel"], "question");
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).ends_with("/channels/question")));
    assert_no_personal_reads(&h);
}
