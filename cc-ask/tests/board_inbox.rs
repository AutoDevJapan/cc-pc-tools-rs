mod common;
use cc_ask::{board, commands::*, core::*, routing};
use common::{message, Harness};
use serde_json::json;

fn setup(archived: bool) -> Harness {
    let h = Harness::new();
    h.save(&json!({"questions":[],"requests":{}}));
    atomic_json(
        &h.ctx.here.join("board.json"),
        &json!({"guild_id":"G","thread_id":"500","channel_id":"10"}),
        1,
    )
    .unwrap();
    let mut f = h.fake.borrow_mut();
    f.channels = vec![json!({"id":"10","type":0,"name":"一般"})];
    f.threads.clear();
    f.messages.clear();
    f.threads.insert("500".into(), json!({"id":"500","guild_id":"G","parent_id":"10","type":11,"owner_id":"BOT","name":board::TITLE,"last_message_id":"130","thread_metadata":{"archived":archived}}));
    f.threads.insert("501".into(), json!({"id":"501","guild_id":"G","parent_id":"10","type":11,"owner_id":"BOT","name":board::TITLE,"last_message_id":"140","thread_metadata":{"archived":false}}));
    f.messages.insert(
        "500".into(),
        vec![
            message("110", "今の進捗を教えて", "OWN"),
            message("120", "一覧の本文", "BOT"),
            message("125", "別の人の投稿", "OTHER"),
            message("130", "この作業はどこまで進んだ？", "OWN"),
        ],
    );
    f.messages.insert(
        "501".into(),
        vec![message("140", "一般の別スレッド", "OWN")],
    );
    drop(f);
    h
}

#[test]
fn board_owner_messages_are_in_inbox_once_with_exact_where_label() {
    for archived in [false, true] {
        let mut h = setup(archived);
        let mark = h.root.path().join("inbox.mark");
        std::fs::write(&mark, "100").unwrap();
        let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
        assert_eq!(
            out.iter().map(|m| s(&m["id"])).collect::<Vec<_>>(),
            ["110", "130"]
        );
        assert!(out
            .iter()
            .all(|m| m["where"] == board::TITLE && m["channel"] == "500"));
        assert_eq!(out[0]["text"], "今の進捗を教えて");
        assert_eq!(std::fs::read_to_string(&mark).unwrap(), "130");
        assert!(cmd_inbox(&mut h.ctx, &mark).unwrap().is_empty());
        let f = h.fake.borrow();
        assert!(!f
            .http_calls
            .iter()
            .any(|c| s(&c["url"]).contains("/channels/501/messages")
                || s(&c["url"]).contains("/channels/10/messages")));
        assert!(f.posts.is_empty() && f.picks.is_empty() && f.launches.is_empty());
    }
}

#[test]
fn board_replies_read_only_is_allowed_without_registering_a_question() {
    let mut h = setup(false);
    let before = h.state();
    let board_before = std::fs::read(h.ctx.here.join("board.json")).unwrap();
    let out = cmd_replies_read_only(&mut h.ctx, "500").unwrap();
    assert_eq!(
        out[0]["replies"]
            .as_array()
            .unwrap()
            .iter()
            .map(|m| s(&m["id"]))
            .collect::<Vec<_>>(),
        ["110", "130"]
    );
    assert_eq!(out[0]["read_only"], true);
    assert_eq!(h.state(), before);
    assert_eq!(
        std::fs::read(h.ctx.here.join("board.json")).unwrap(),
        board_before
    );
    assert!(cmd_replies_read_only(&mut h.ctx, "501").is_err());
}

#[test]
fn board_registration_cannot_authorize_a_different_parent_or_server() {
    for (field, value) in [
        ("parent_id", json!("20")),
        ("guild_id", json!("OTHER")),
        ("type", json!(12)),
    ] {
        let mut h = setup(false);
        h.save(&json!({"questions":[{"thread_id":"500","open":false}],"requests":{}}));
        h.fake.borrow_mut().threads.get_mut("500").unwrap()[field] = value;
        let mark = h.root.path().join("inbox.mark");
        std::fs::write(&mark, "100").unwrap();
        assert!(cmd_inbox(&mut h.ctx, &mark).unwrap().is_empty());
        assert_eq!(std::fs::read_to_string(&mark).unwrap(), "100");
        assert!(cmd_replies_read_only(&mut h.ctx, "500").is_err());
    }
}

#[test]
fn minute_tick_never_dispatches_board_even_with_legacy_question_and_request_records() {
    let mut h = setup(false);
    let original = json!({"job":"old-board-job","thread_info":{"id":"500","parent_id":REQUEST_CHANNEL_ID,"type":11},"close_pending":true});
    h.save(&json!({"questions":[{"thread_id":"500","open":true,"source":"scheduler","after":"100","cwd":h.root.path()}],"requests":{"500":original}}));
    cmd_tick(&mut h.ctx).unwrap();
    let f = h.fake.borrow();
    assert!(f.launches.is_empty(), "board must never launch a job");
    assert!(f.picks.is_empty(), "board must never select a model");
    assert!(
        f.posts.is_empty(),
        "board must not receive automatic task acknowledgements"
    );
    drop(f);
    assert_eq!(h.state()["requests"]["500"], original);
    assert_eq!(h.state()["questions"][0]["after"], "100");
}
#[test]
fn old_board_registration_still_blocks_dispatch_after_guild_settings_change() {
    let mut h = setup(false);
    h.ctx.cfg["guild_id"] = json!("OTHER");
    let q = json!({"thread_id":"500","open":true,"source":"scheduler","after":"100"});
    assert!(!routing::question_allowed(&mut h.ctx, &q).unwrap());
    let cfg = h.ctx.cfg.clone();
    let mut state = json!({"questions":[q],"requests":{"500":{"job":"old-board-job","thread_info":{"id":"500","type":11,"parent_id":REQUEST_CHANNEL_ID}}}});
    assert!(
        cc_ask::intake::request_threads(&mut h.ctx, &cfg, &mut state)
            .unwrap()
            .is_empty()
    );
    assert!(h.fake.borrow().picks.is_empty() && h.fake.borrow().launches.is_empty());
}

#[test]
fn retired_routing_cannot_dispatch_preclassified_board_backlog() {
    let mut h = setup(false);
    let now = h.ctx.now();
    let mid = (((now * 1000. - 1420070400000.) as u64) << 22).to_string();
    atomic_json(
        &h.ctx.here.join("automation.json"),
        &json!({"enabled":true,"since":now-3600.}),
        1,
    )
    .unwrap();
    atomic_json(&h.ctx.here.join("routing_state.json"), &json!({"cursor":{"since":"100","channels":{},"places":{}},"messages":{mid.clone():{"message":{"id":mid,"channel":"500","text":"進捗を確認したい"},"kind":"new","ready":true,"acknowledged":true,"key":"legacy-board"}}}), 1).unwrap();
    let report = routing::run(&mut h.ctx, &mut false).unwrap();
    assert_eq!(report["excluded"], 1);
    let f = h.fake.borrow();
    assert!(f.posts.is_empty() && f.picks.is_empty() && f.launches.is_empty());
    assert!(!f
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).contains("/channels/500/messages")));
}

#[test]
fn owner_messages_older_than_inbox_cursor_are_not_replayed() {
    let mut h = setup(false);
    let mark = h.root.path().join("inbox.mark");
    std::fs::write(&mark, "130").unwrap();
    assert!(cmd_inbox(&mut h.ctx, &mark).unwrap().is_empty());
    assert_eq!(std::fs::read_to_string(mark).unwrap(), "130");
}
#[test]
fn board_owner_post_behind_two_full_non_owner_pages_is_not_missed() {
    let mut h = setup(false);
    let mark = h.root.path().join("inbox.mark");
    std::fs::write(&mark, "100").unwrap();
    {
        let mut f = h.fake.borrow_mut();
        let mut messages = vec![message("110", "先に書いた進捗の質問", "OWN")];
        messages.extend((111..316).map(|id| message(&id.to_string(), "他の投稿", "BOT")));
        f.messages.insert("500".into(), messages);
        f.threads.get_mut("500").unwrap()["last_message_id"] = json!("315");
    }
    let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
    assert_eq!(out.len(), 1);
    assert_eq!(out[0]["id"], "110");
    assert_eq!(out[0]["where"], board::TITLE);
    assert_eq!(out[0]["text"], "先に書いた進捗の質問");
    assert_eq!(std::fs::read_to_string(mark).unwrap(), "110");
}
#[test]
fn failed_board_read_does_not_advance_global_cursor_past_its_unread_posts() {
    let mut h = setup(false);
    let mark = h.root.path().join("inbox.mark");
    std::fs::write(&mark, "100").unwrap();
    {
        let mut f = h.fake.borrow_mut();
        f.threads.insert("400".into(), json!({"id":"400","type":11,"parent_id":REQUEST_CHANNEL_ID,"last_message_id":"300","name":"依頼","thread_metadata":{"archived":false}}));
        f.messages
            .insert("400".into(), vec![message("300", "別の依頼", "OWN")]);
        f.http_errors
            .insert("GET /channels/500/messages?limit=100".into(), 1);
    }
    assert!(cmd_inbox(&mut h.ctx, &mark).is_err());
    assert_eq!(std::fs::read_to_string(&mark).unwrap(), "100");
    let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
    assert_eq!(
        out.iter().map(|m| s(&m["id"])).collect::<Vec<_>>(),
        ["110", "130", "300"]
    );
}
