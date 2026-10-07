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
fn unassigned_todo_is_silent_and_queue_receipt_removes_it_without_launching() {
    let mut h = setup();
    h.ctx.jobs = h.root.path().join("codex-jobs");
    let intake = h.root.path().join("localai-web-rs/target/release");
    std::fs::create_dir_all(&intake).unwrap();
    atomic_json(&intake.join("console-state.json"), &json!({"queue":[]}), 2).unwrap();
    let jobs = h.ctx.jobs.join("jobs/other");
    std::fs::create_dir_all(&jobs).unwrap();
    atomic_json(
        &jobs.join("job.json"),
        &json!({"id":"other","todo_id":2,"status":"running","project":"tools"}),
        2,
    )
    .unwrap();
    let created = h.fake.borrow().now - 1799.;
    let db = json!({"projects":{"tools":"D:/CC/Tools"},"items":[{"id":1,"project":"tools","title":"担当のいない作業を確認する","status":"todo","created_s":created,"jobs":[]}]});
    atomic_json(&h.ctx.todo, &db, 2).unwrap();
    update(&mut h.ctx).unwrap();
    assert!(!read_json(&h.ctx.here.join("board.json")).unwrap()["body"]
        .as_str()
        .unwrap()
        .contains("担当なし"));
    h.fake.borrow_mut().now += 1.;
    update(&mut h.ctx).unwrap();
    let state = read_json(&h.ctx.here.join("board.json")).unwrap();
    assert!(state["body"]
        .as_str()
        .unwrap()
        .contains("止まっている（担当なし）"));
    assert!(state["body"].as_str().unwrap().contains("止まっている 1件"));
    assert_eq!(
        read_json(&h.ctx.todo).unwrap(),
        db,
        "display must not persist derived metadata"
    );
    atomic_json(
        &intake.join("console-state.json"),
        &json!({"queue":[{"todo_id":1,"key":"req-todo-1-triage-20261007"}]}),
        2,
    )
    .unwrap();
    update(&mut h.ctx).unwrap();
    assert!(!read_json(&h.ctx.here.join("board.json")).unwrap()["body"]
        .as_str()
        .unwrap()
        .contains("担当なし"));
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .all(|c| c["body"]["allowed_mentions"]["parse"].is_null()
            || c["body"]["allowed_mentions"]["parse"] == json!([])));
}
#[test]
fn unreadable_ownership_does_not_break_existing_board_or_report_absence() {
    let mut h = setup();
    h.ctx.jobs = h.root.path().join("codex-jobs");
    let intake = h.root.path().join("localai-web-rs/target/release");
    std::fs::create_dir_all(&intake).unwrap();
    let mut db = read_json(&h.ctx.todo).unwrap();
    db["items"].as_array_mut().unwrap().push(json!({"id":2,"project":"tools","title":"未担当の確認","status":"todo","created_s":1.,"jobs":[]}));
    atomic_json(&h.ctx.todo, &db, 2).unwrap();
    for contents in ["broken", "{}", "{\"queue\":[]}"] {
        std::fs::write(intake.join("console-state.json"), contents).unwrap();
        let broken = h.ctx.jobs.join("jobs/broken");
        std::fs::create_dir_all(&broken).unwrap();
        std::fs::write(broken.join("job.json"), "broken").unwrap();
        update(&mut h.ctx).unwrap();
        let state = read_json(&h.ctx.here.join("board.json")).unwrap();
        assert!(state["body"].as_str().unwrap().contains("画面の読みやすさを改善する"));
        assert!(!state["body"].as_str().unwrap().contains("担当なし"));
    }
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
    h.fake.borrow_mut().llm_answer = "「公開の不具合を直す」".into();
    update(&mut h.ctx).unwrap();
    let state = read_json(&h.ctx.here.join("board.json")).unwrap();
    assert_eq!(state["labels"]["todo:1"], "公開の不具合を直す");
    assert_eq!(h.fake.borrow().llm_calls.len(), 1);
    // The model sees the sanitized title, never job names or worktree words.
    let prompt = h.fake.borrow().llm_calls[0].clone();
    assert!(!prompt.contains("shep-123") && !prompt.contains("worktree"));
    update(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().llm_calls.len(), 1);
    let db = json!({"items":[{"id":2,"project":"製品","title":"fix deployment failure","status":"doing","updated":"2026-09-30 22:00"}]});
    atomic_json(&h.ctx.todo, &db, 1).unwrap();
    // An unusable (English/multi-line) answer keeps the sanitized fallback, once.
    h.fake.borrow_mut().llm_answer = "fix deployment\nfailure".into();
    update(&mut h.ctx).unwrap();
    update(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().llm_calls.len(), 2);
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .all(|(argv, _)| !argv[0].ends_with("codex.exe")));
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
#[test]
fn progress_bars_only_use_recorded_integers_in_the_valid_range() {
    assert_eq!(progress_bar(&json!({"progress":60})), "▰▰▰▰▰▰▱▱▱▱ 60%");
    assert_eq!(progress_bar(&json!({"progress":0})), "▱▱▱▱▱▱▱▱▱▱ 0%");
    assert_eq!(progress_bar(&json!({"progress":100})), "▰▰▰▰▰▰▰▰▰▰ 100%");
    for item in [
        json!({}),
        json!({"progress":null}),
        json!({"progress":-1}),
        json!({"progress":101}),
        json!({"progress":"60"}),
        json!({"progress":60.5}),
    ] {
        assert_eq!(progress_bar(&item), "進み具合：未記録");
    }
}
#[test]
fn title_bar_pairs_have_blank_lines_and_block_reason_is_beside_bar() {
    let db = json!({"items":[
        {"id":1,"project":"製品","title":"保存画面を直す","status":"doing","progress":60,"updated":"2026-10-06 16:30"},
        {"id":2,"project":"製品","title":"認証を確認する","status":"block","progress":30,"note":"本人の承認を待っています。長い説明は載せない","updated":"2026-10-06 16:30"},
        {"id":3,"project":"製品","title":"復旧を検査する","status":"doing","updated":"2026-10-06 16:30"}]});
    let text = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
    assert!(text.contains("## 製品\n- 保存画面を直す\n▰▰▰▰▰▰▱▱▱▱ 60%"));
    assert!(text.contains(
        "\n\n- 認証を確認する\n▰▰▰▱▱▱▱▱▱▱ 30%｜止まっている（本人の承認を待っています）"
    ));
    assert!(text.contains("\n\n- 復旧を検査する\n進み具合：未記録"));
    assert!(!text.contains("長い説明"));
}
#[test]
fn title_and_bar_are_kept_together_when_splitting_messages() {
    let first = format!("{}\n\n", "あ".repeat(1970));
    let pair = "- 保存画面を直す\n▰▰▰▰▰▰▱▱▱▱ 60%｜作業中\n\n";
    let text = format!("{first}{pair}");
    let parts = split(&text);
    assert_eq!(parts.concat(), text);
    assert_eq!(parts.len(), 2);
    assert_eq!(parts[1], pair);
}
#[test]
fn task_title_has_no_internal_numbers_or_english_jargon() {
    let text = prose(
        "PR86のhelperをfixtureでreviewする（親25・ToDo324） Issue3155のruntimeを直す",
        &Names::parse(""),
        48,
    );
    assert_eq!(text, "補助処理を検証用データでレビューする 実行処理を直す");
    assert_eq!(
        prose("保存を確認する（・324", &Names::parse(""), 48),
        "保存を確認する"
    );
}
#[test]
fn blocked_owner_wait_also_shows_its_reason_beside_progress() {
    let db = json!({"items":[{"id":1,"project":"製品","title":"公開を進める","status":"block","progress":60,"note":"オーナー判断待ち。詳細","updated":"2026-10-06 16:30"}]});
    let text = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
    assert!(text.contains("▰▰▰▰▰▰▱▱▱▱ 60%｜返事待ち（オーナー判断待ち）"));
}
#[test]
fn empty_or_identifier_only_block_notes_do_not_invent_a_reason() {
    for note in ["   ", "todo:123", "worktree"] {
        let db = json!({"items":[{"id":1,"project":"製品","title":"保存を直す","status":"block","note":note}]});
        let text = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
        assert!(text.contains("止まっている（理由未記録）"));
        assert!(!text.contains("止まっている（作業内容を確認する）"));
    }
}
#[test]
fn local_labels_must_be_short_closed_japanese_lines() {
    assert!(accept_label("公開の不具合を直す"));
    assert!(accept_label("Places APIの月の上限を設定する（課金防止）"));
    assert!(!accept_label("Places API 月1,000回上限設定（課金防止"));
    assert!(!accept_label(&"長".repeat(41)));
    assert!(!accept_label("fix deployment"));
    assert!(!accept_label("shep-12 の修正"));
}
