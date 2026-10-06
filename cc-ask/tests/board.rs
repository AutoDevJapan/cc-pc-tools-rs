mod common;
use cc_ask::{board::*, core::*};
use cc_task_title::Names;
use common::Harness;
use serde_json::{json, Value};

fn setup() -> Harness {
    let h = Harness::new();
    h.fake.borrow_mut().channels = vec![json!({"id":"GENERAL","name":"一般","type":0})];
    atomic_json(&h.ctx.todo, &json!({"projects":{},"items":[{"id":1,"project":"GoDD-AO","title":"画面の読みやすさを改善する","status":"doing","updated":"2026-09-30 22:00"}]}), 2).unwrap();
    h
}
#[test]
fn splits_at_lines_and_never_exceeds_2000_utf16_units() {
    let input = format!(
        "{}\n{}\n{}",
        "あ".repeat(1999),
        "次の項目",
        "😀".repeat(2200)
    );
    let parts = split(&input);
    assert_eq!(parts.concat(), input);
    assert!(parts.iter().all(|s| s.encode_utf16().count() <= 2000));
    assert_eq!(parts[0], format!("{}\n", "あ".repeat(1999)));
    assert!(parts[1].starts_with("次の項目"));
}
#[test]
fn client_names_are_removed_from_all_display_fields_and_pr_urls() {
    let names = Names::parse("AcmeClient\nAnotherClient\n");
    let db = json!({"projects":{"service":"D:/AcmeClient"},"items":[
        {"id":10,"project":"service","status":"block","title":"ACMECLIENTの認証を直す","note":"AnotherClientの返事待ち https://github.com/Org/AcmeClient/pull/3","updated":"2026-09-30 22:00"}]});
    let q = vec![
        json!({"thread_id":"123","after":"123","project":"AcmeClient","title":"AnotherClientを公開するか","open":true}),
    ];
    let state = json!({"guild_id":"G","labels":{"todo:10":"AcmeClientの接続を直す"}});
    let text = render(&db, &q, &state, &names, 1790773200.);
    assert!(text.contains("## クライアント案件"));
    assert!(text.contains("https://discord.com/channels/G/123"));
    assert!(!text.to_lowercase().contains("acmeclient"));
    assert!(!text.to_lowercase().contains("anotherclient"));
    assert!(!text.contains("/pull/3"));
}
#[test]
fn unanswered_questions_appear_before_projects_and_answered_questions_are_excluded() {
    let db = json!({"items":[{"id":1,"project":"製品A","title":"表示を改善する","status":"doing","updated":"2026-09-30 22:00"}]});
    let raw = json!({"questions":[
        {"thread_id":"10","after":"11","project":"製品A","title":"公開してよいか","open":true},
        {"thread_id":"20","after":"21","open":true,"answered_reply":"22"},
        {"thread_id":"30","after":"31","open":true},
        {"thread_id":"40","after":"41","open":false},
        {"thread_id":"50","after":"51","open":true}],"owner_answers":{"30":{"id":"32"},"50":"52"}});
    let q = pending(&raw);
    assert_eq!(q.len(), 1);
    assert_eq!(q[0]["thread_id"], "10");
    let text = render(
        &db,
        &q,
        &json!({"guild_id":"G"}),
        &Names::parse(""),
        1790773200.,
    );
    assert!(text.find("## あなたの返事待ち").unwrap() < text.find("## 製品A").unwrap());
    assert!(text.contains("https://discord.com/channels/G/10"));
    assert!(render(&db, &[], &json!({}), &Names::parse(""), 1790773200.).contains("今はありません"));
}
#[test]
fn unchanged_board_makes_no_discord_calls_even_after_five_minutes() {
    let mut h = setup();
    assert_eq!(update(&mut h.ctx).unwrap()["changed"], true);
    let state = read_json(&h.ctx.here.join("board.json")).unwrap();
    assert_eq!(state["channel_id"], "GENERAL");
    assert_eq!(state["thread_id"], "300");
    assert_eq!(state["messages"].as_array().unwrap().len(), 1);
    let calls = h.fake.borrow().http_calls.len();
    h.fake.borrow_mut().now += 300.;
    assert_eq!(update(&mut h.ctx).unwrap()["changed"], false);
    assert_eq!(h.fake.borrow().http_calls.len(), calls);
    for call in &h.fake.borrow().http_calls {
        if call["body"]["content"].is_string() {
            if call["method"] == "POST" {
                assert_eq!(n(&call["body"]["flags"]) & SILENT, SILENT);
            } else {
                assert!(call["body"].get("flags").is_none());
            }
            assert_eq!(call["body"]["allowed_mentions"]["parse"], json!([]));
        }
    }
}
#[test]
fn growing_shrinking_and_reusing_slots_never_replaces_messages_or_thread() {
    let mut h = setup();
    let items: Vec<Value> = (1..55).map(|id| json!({"id":id,"project":"製品A","title":format!("表示の問題を修正する（画面{id}）"),"status":"doing","updated":"2026-09-30 22:00"})).collect();
    atomic_json(&h.ctx.todo, &json!({"items":items}), 1).unwrap();
    update(&mut h.ctx).unwrap();
    let first = read_json(&h.ctx.here.join("board.json")).unwrap();
    assert!(first["messages"].as_array().unwrap().len() > 1);
    let ids: Vec<_> = first["messages"]
        .as_array()
        .unwrap()
        .iter()
        .map(|m| m["id"].clone())
        .collect();
    let posts = h.fake.borrow().posts.len();
    atomic_json(&h.ctx.todo, &json!({"items":[]}), 1).unwrap();
    update(&mut h.ctx).unwrap();
    let smaller = read_json(&h.ctx.here.join("board.json")).unwrap();
    assert_eq!(
        smaller["messages"]
            .as_array()
            .unwrap()
            .iter()
            .map(|m| m["id"].clone())
            .collect::<Vec<_>>(),
        ids
    );
    assert!(smaller["messages"]
        .as_array()
        .unwrap()
        .iter()
        .skip(1)
        .all(|m| m["content"] == "（空き）"));
    atomic_json(&h.ctx.todo, &json!({"items":items}), 1).unwrap();
    update(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), posts);
}
#[test]
fn stalled_marker_changes_once_at_one_hour_not_on_every_refresh() {
    let db = json!({"items":[{"id":1,"project":"製品","title":"機能を直す","status":"doing","updated":"2026-09-30T21:59:00+09:00"}]});
    let start = chrono::DateTime::parse_from_rfc3339("2026-09-30T21:59:00+09:00")
        .unwrap()
        .timestamp() as f64;
    let a = render(&db, &[], &json!({}), &Names::parse(""), start + 3599.);
    let b = render(&db, &[], &json!({}), &Names::parse(""), start + 3600.);
    assert!(!a.contains("1時間以上動きなし"));
    assert!(b.contains("1時間以上動きなし"));
    assert_eq!(
        b,
        render(&db, &[], &json!({}), &Names::parse(""), start + 7200.)
    );
}
#[test]
fn schedule_is_durable_and_only_due_every_five_minutes() {
    let mut h = setup();
    launch_due(&mut h.ctx).unwrap();
    let path = h.ctx.here.join("board-schedule.json");
    let first = std::fs::read(&path).unwrap();
    h.fake.borrow_mut().now += 299.;
    launch_due(&mut h.ctx).unwrap();
    assert_eq!(std::fs::read(&path).unwrap(), first);
    h.fake.borrow_mut().now += 1.;
    launch_due(&mut h.ctx).unwrap();
    assert_ne!(std::fs::read(&path).unwrap(), first);
}
#[test]
fn general_channel_lookup_excludes_voice_and_refuses_ambiguous_matches() {
    assert_eq!(
        find_general(
            &json!([{"id":"1","type":2,"name":"一般"},{"id":"2","type":0,"name":"general"}])
        )
        .unwrap(),
        "2"
    );
    assert!(find_general(&json!([])).is_err());
    assert!(find_general(
        &json!([{"id":"1","type":0,"name":"一般"},{"id":"2","type":0,"name":"general"}])
    )
    .is_err());
}
#[test]
fn edit_failure_is_retried_without_new_posts() {
    let mut h = setup();
    update(&mut h.ctx).unwrap();
    let posts = h.fake.borrow().posts.len();
    atomic_json(&h.ctx.todo, &json!({"items":[]}), 1).unwrap();
    h.fake
        .borrow_mut()
        .http_errors
        .insert("PATCH /channels/300/messages/1001".into(), 1);
    assert!(update(&mut h.ctx).is_err());
    assert_eq!(update(&mut h.ctx).unwrap()["changed"], true);
    assert_eq!(h.fake.borrow().posts.len(), posts);
}
#[test]
fn lost_post_is_recovered_and_cleared_when_the_board_shrinks() {
    let mut h = setup();
    update(&mut h.ctx).unwrap();
    let p = h.ctx.here.join("board.json");
    let mut state = read_json(&p).unwrap();
    state["pending_message"] = json!({"content":"古い続きの項目","nonce":"recover-nonce"});
    atomic_json(&p, &state, 1).unwrap();
    h.fake.borrow_mut().messages.get_mut("300").unwrap().push(json!({"id":"9999","content":"古い続きの項目","author":{"bot":true},"nonce":"recover-nonce"}));
    atomic_json(&h.ctx.todo, &json!({"items":[]}), 1).unwrap();
    let posts = h.fake.borrow().posts.len();
    update(&mut h.ctx).unwrap();
    let state = read_json(&p).unwrap();
    assert!(state["pending_message"].is_null());
    assert_eq!(state["messages"][1]["id"], "9999");
    assert_eq!(state["messages"][1]["content"], "（空き）");
    assert_eq!(h.fake.borrow().posts.len(), posts);
}
#[test]
fn translations_and_failed_fallbacks_are_cached_once() {
    let mut h = setup();
    let db = json!({"items":[{"id":1,"project":"製品","title":"fix shep-123 worktree deployment","status":"doing","updated":"2026-09-30 22:00"}]});
    atomic_json(&h.ctx.todo, &db, 1).unwrap();
    h.fake.borrow_mut().codex_labels =
        json!({"item":{"type":"agent_message","text":"{\"todo:1\":\"公開の不具合を直す\"}"}})
            .to_string();
    update(&mut h.ctx).unwrap();
    let calls = h.fake.borrow().runs.len();
    let state = read_json(&h.ctx.here.join("board.json")).unwrap();
    // On machines without native Codex the sanitized fallback is still cached.
    if calls > 0 {
        assert_eq!(state["labels"]["todo:1"], "公開の不具合を直す");
    }
    update(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().runs.len(), calls);
    assert!(!s(&state["labels"]["todo:1"]).contains("shep"));
    assert!(!s(&state["labels"]["todo:1"]).contains("worktree"));
    let db = json!({"items":[{"id":2,"project":"製品","title":"fix deployment failure","status":"doing","updated":"2026-09-30 22:00"}]});
    atomic_json(&h.ctx.todo, &db, 1).unwrap();
    h.fake.borrow_mut().codex_labels.clear();
    update(&mut h.ctx).unwrap();
    let calls = h.fake.borrow().runs.len();
    update(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().runs.len(), calls);
    assert_eq!(
        read_json(&h.ctx.here.join("board.json")).unwrap()["labels"]["todo:2"],
        "fix deployment failure"
    );
}
#[test]
fn changed_server_binding_stops_before_calls_or_state_rebinding() {
    let mut h = setup();
    update(&mut h.ctx).unwrap();
    let before = std::fs::read(h.ctx.here.join("board.json")).unwrap();
    let calls = h.fake.borrow().http_calls.len();
    h.ctx.cfg["guild_id"] = json!("OTHER");
    assert!(update(&mut h.ctx).is_err());
    assert_eq!(h.fake.borrow().http_calls.len(), calls);
    assert_eq!(
        std::fs::read(h.ctx.here.join("board.json")).unwrap(),
        before
    );
}
#[test]
fn existing_board_wrong_server_or_private_type_is_not_edited() {
    for (field, value) in [("guild_id", json!("OTHER")), ("type", json!(12))] {
        let mut h = setup();
        update(&mut h.ctx).unwrap();
        let posts = h.fake.borrow().posts.len();
        let edits = h.fake.borrow().patches.len();
        h.fake.borrow_mut().threads.get_mut("300").unwrap()[field] = value;
        atomic_json(&h.ctx.todo, &json!({"items":[]}), 1).unwrap();
        assert!(update(&mut h.ctx).is_err());
        assert_eq!(h.fake.borrow().posts.len(), posts);
        assert_eq!(h.fake.borrow().patches.len(), edits);
    }
}
#[test]
fn display_strips_discord_ids_and_internal_words_next_to_japanese() {
    let text = prose(
        "依頼1556306984981102642: worktree確認・新shep-42の作業 ToDo123 作業20261006-155437-req-board",
        &Names::parse(""),
        48,
    );
    assert!(!text.contains("1556306984981102642"));
    assert!(!text.contains("20261006"));
    assert!(!text.contains("worktree"));
    assert!(!text.contains("shep"));
    assert!(!text.contains("ToDo"));
    assert!(text.contains("確認"));
}
