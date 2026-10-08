mod common;
use cc_ask::{commands::cmd_inbox, core::*, resume};
use common::{message, Harness};
use serde_json::json;

#[test]
fn missing_destination_answer_is_received_and_replayed_with_its_content() {
    for source in ["scheduler", "request", ""] {
        let mut h = Harness::new();
        h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":true,"source":source,"title":"製品の方針"}]}));
        h.fake
            .borrow_mut()
            .messages
            .insert("200".into(), vec![message("300", "aで進めて", "OWN")]);
        resume::cmd_resume(&mut h.ctx, &mut true).unwrap();
        assert_eq!(h.state()["questions"][0]["answered_reply"], "300");
        let mark = h.root.path().join("inbox.mark");
        std::fs::write(&mark, "999999").unwrap();
        for _ in 0..2 {
            let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
            assert_eq!(out.len(), 1);
            assert_eq!(out[0]["text"], "aで進めて");
            assert_eq!(out[0]["id"], "300");
            assert_eq!(out[0]["delivered"], false);
        }
        assert_eq!(std::fs::read_to_string(mark).unwrap(), "999999");
        assert!(h.fake.borrow().launches.is_empty());
        assert!(!reply_delivered(&h.state(), "300"));
    }
}

#[test]
fn answer_behind_full_pages_and_global_cursor_is_found_without_using_old_generation() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[{"thread_id":"200","after":"250","open":true,"agent":"claude-main","title":"新しい質問"}]}));
    let mut messages = vec![
        message("220", "前の回答", "OWN"),
        message("300", "新しい回答", "OWN"),
    ];
    messages.extend((301..510).map(|id| message(&id.to_string(), "通知", "BOT")));
    h.fake.borrow_mut().messages.insert("200".into(), messages);
    let mark = h.root.path().join("inbox.mark");
    std::fs::write(&mark, "999999").unwrap();
    let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
    assert_eq!(out.len(), 1);
    assert_eq!(out[0]["id"], "300");
    assert_eq!(out[0]["text"], "新しい回答");
    assert!(h.fake.borrow().posts.is_empty() && h.fake.borrow().launches.is_empty());
}

#[test]
fn split_answers_and_attachment_only_messages_survive_cursor_and_are_not_duplicated() {
    let mut h = Harness::new();
    h.save(
        &json!({"questions":[{"thread_id":"200","after":"200","open":true,"agent":"claude-main"},
        {"thread_id":"200","after":"220","open":true,"agent":"claude-main"}]}),
    );
    let mut attachment = message("320", "", "OWN");
    attachment["attachments"] = json!([{"url":"https://example.com/answer.png"}]);
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![
            message("300", "1a", "OWN"),
            message("310", "2b", "OWN"),
            attachment,
        ],
    );
    let mark = h.root.path().join("inbox.mark");
    std::fs::write(&mark, "999999").unwrap();
    for _ in 0..2 {
        let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
        assert_eq!(out.len(), 3);
        assert_eq!(out[0]["text"], "1a");
        assert_eq!(out[1]["text"], "2b");
        assert!(out[2]["text"].as_str().unwrap().contains("answer.png"));
        assert_eq!(out[0]["questions"].as_array().unwrap().len(), 2);
    }
    assert_eq!(h.state()["questions"][0]["first_owner_reply"], "300");
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn legacy_answer_objects_are_visible_during_discord_read_failure() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":true}],"owner_answers":{"200":{"id":"300"}}}));
    let mark = h.root.path().join("inbox.mark");
    std::fs::write(&mark, "999999").unwrap();
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/200/messages?after=200&limit=100".into(), 1);
    let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
    assert_eq!(out[0]["id"], "300");
    assert_eq!(out[0]["status"], "答えあり・未反映");
}

#[test]
fn inventory_failure_keeps_registered_saved_answers_visible_without_advancing_cursor() {
    let mut h = Harness::new();
    h.save(
        &json!({"questions":[{"thread_id":"200","after":"200","answered_reply":"300","open":true,
        "owner_reply":{"id":"300","content":"aを採用"}}]}),
    );
    let mark = h.root.path().join("inbox.mark");
    std::fs::write(&mark, "999999").unwrap();
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/guilds/G/channels".into(), 1);
    let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
    assert_eq!(out.len(), 1);
    assert_eq!(out[0]["text"], "aを採用");
    assert_eq!(out[0]["saved_only"], true);
    assert_eq!(std::fs::read_to_string(mark).unwrap(), "999999");
    assert!(h.fake.borrow().launches.is_empty());
}
