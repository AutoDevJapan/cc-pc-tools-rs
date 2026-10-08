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

fn answer_snowflake(at: f64) -> String {
    ((((at * 1000.) as u64) - 1420070400000) << 22).to_string()
}

#[test]
fn answered_open_questions_stay_visible_regardless_of_destination_or_delivery() {
    let at = 1790773200.;
    let reply = answer_snowflake(at);
    for metadata in [
        json!({"agent":"main"}),
        json!({"resume_status":"再開先なし"}),
        json!({"agent":"worker","resume_status":"再開失敗","last_resume_error":"transport failed"}),
        json!({"agent":"worker","resumed_reply":reply}),
    ] {
        let mut q = json!({"thread_id":"10","after":answer_snowflake(at - 1.),
            "project":"製品","title":"公開してよいか","open":true});
        q.as_object_mut()
            .unwrap()
            .extend(metadata.as_object().unwrap().clone());
        let mut state = json!({"guild_id":"G","questions":[q],"owner_answers":{"10":reply},
            "answer_receipts":{}});
        // A read receipt or successful delivery must not close an open question on the board.
        for receipt in [
            json!({}),
            json!({"thread":"10","read_by":"reader"}),
            json!({"thread":"10","handled_by":"worker","posted":true}),
        ] {
            state["answer_receipts"][&reply] = receipt;
            let before = state.clone();
            assert!(pending(&state).is_empty());
            let text = render(
                &json!({}),
                &pending(&state),
                &state,
                &Names::parse(""),
                at + 600.,
            );
            assert!(text.contains("あなたの返事待ち 0件（質問）"));
            assert!(text.contains("## あなたの返事待ち\n今はありません"));
            assert!(text.contains("## 答えあり・未反映（1件）\n• 製品：公開してよいか"));
            assert_eq!(text.matches("https://discord.com/channels/G/10").count(), 1);
            assert_eq!(state, before, "render must not close or deliver questions");
        }
        state["questions"][0]["open"] = json!(false);
        let closed = render(&json!({}), &[], &state, &Names::parse(""), at + 3600.);
        assert!(!closed.contains("答えあり・未反映"));
        assert!(!closed.contains("公開してよいか"));
    }
}

#[test]
fn answer_age_uses_the_newest_recorded_reply_with_an_inclusive_ten_minute_boundary() {
    let at = 1790773200.125;
    for (owner, recorded) in [
        (json!(answer_snowflake(at)), Value::Null),
        (json!({"id":answer_snowflake(at)}), Value::Null),
        (
            json!(answer_snowflake(at).parse::<u64>().unwrap()),
            Value::Null,
        ),
        (Value::Null, json!(answer_snowflake(at))),
        (
            json!(answer_snowflake(at - 120.)),
            json!(answer_snowflake(at)),
        ),
        (
            json!({"id":answer_snowflake(at)}),
            json!(answer_snowflake(at - 120.)),
        ),
    ] {
        let state = json!({"questions":[{"thread_id":"10","after":answer_snowflake(at - 300.),
            "title":"公開してよいか","open":true,"answered_reply":recorded}],
            "owner_answers":{"10":owner}});
        for (now, visible) in [
            (at - 1., false),
            (at + 599.999, false),
            (at + 600., true),
            (at + 3600., true),
        ] {
            let text = render(&json!({}), &pending(&state), &state, &Names::parse(""), now);
            assert_eq!(
                text.contains("答えあり・未反映"),
                visible,
                "now={now}, state={state}"
            );
            assert!(text.contains("あなたの返事待ち 0件（質問）"));
        }
    }
}

#[test]
fn old_answers_do_not_answer_a_new_question_and_threads_are_counted_once() {
    let at = 1790773200.;
    for answer in [answer_snowflake(at - 1.), answer_snowflake(at)] {
        let state = json!({"guild_id":"G","questions":[
            {"thread_id":"10","after":answer_snowflake(at),"title":"新しい質問","open":true,"answered_reply":answer},
            {"thread_id":"10","after":answer_snowflake(at - 120.),"title":"以前の質問","open":true},
            {"thread_id":"20","after":answer_snowflake(at - 120.),"title":"古い未回答","open":true},
            {"thread_id":"20","after":answer_snowflake(at),"title":"新しい回答済み","open":true},
            {"thread_id":"","after":"0","title":"届け先のスレッド不明","open":true}],
            "owner_answers":{"10":{"id":answer},"20":answer_snowflake(at + 1.)}});
        let q = pending(&state);
        assert_eq!(q.len(), 1);
        assert_eq!(q[0]["title"], "新しい質問");
        let text = render(&json!({}), &q, &state, &Names::parse(""), at + 601.);
        assert!(text.contains("あなたの返事待ち 1件（質問）"));
        assert!(text.contains("## 答えあり・未反映（1件）"));
        assert!(text.contains("新しい回答済み"));
        assert!(!text.contains("以前の質問"));
        assert!(!text.contains("古い未回答"));
        assert_eq!(text.matches("[質問を開く]").count(), 2);
    }
}

#[test]
fn followup_owner_messages_do_not_reset_the_unreflected_answer_deadline() {
    let at = 1790773200.;
    let state = json!({"questions":[{"thread_id":"10","after":answer_snowflake(at-1.),
        "title":"製品の方針","open":true,"first_owner_reply":answer_snowflake(at),
        "answered_reply":answer_snowflake(at+590.)}]});
    let text = render(
        &json!({}),
        &pending(&state),
        &state,
        &Names::parse(""),
        at + 600.,
    );
    assert!(text.contains("答えあり・未反映（1件）"));
    assert!(text.contains("あなたの返事待ち 0件"));
}

#[test]
fn answered_question_does_not_keep_its_task_marked_as_waiting_for_the_owner() {
    let at = 1790773200.;
    let state = json!({"questions":[
        {"thread_id":"123","after":answer_snowflake(at - 1.),"open":true},
        {"thread_id":"456","after":answer_snowflake(at - 1.),"open":true}],
        "owner_answers":{"123":answer_snowflake(at)}});
    let mut db = json!({"items":[{"id":1,"project":"製品","title":"公開を進める",
        "status":"block","note":"オーナー判断待ち。質問スレッド123"}]});
    let text = render(&db, &pending(&state), &state, &Names::parse(""), at + 600.);
    assert!(text.contains("止まっている（オーナー判断待ち）"));
    assert!(!text.contains("｜返事待ち"));
    db["items"][0]["note"] = json!("オーナー判断待ち。質問スレッド123と456");
    let text = render(&db, &pending(&state), &state, &Names::parse(""), at + 600.);
    assert!(text.contains("｜返事待ち（オーナー判断待ち）"));
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
fn schedule_is_durable_and_only_due_every_minute() {
    let mut h = setup();
    launch_due(&mut h.ctx).unwrap();
    let path = h.ctx.here.join("board-schedule.json");
    let first = std::fs::read(&path).unwrap();
    h.fake.borrow_mut().now += 59.;
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

#[test]
fn dependency_waits_include_overwritten_status_and_missing_targets_without_stale_warnings() {
    let db = json!({"items":[
        {"id":1,"project":"製品","title":"依存作業を待つ","status":"wait","wait_on":[10,11,99],"updated":"2026-09-30 22:00"},
        {"id":2,"project":"製品","title":"以前の作業中表示を直す","status":"doing","wait_on":[99],"updated":"2026-09-30 22:00"},
        {"id":3,"project":"製品","title":"未着手の依存作業を待つ","status":"todo","wait_on":[99],"board_unassigned":true},
        {"id":4,"project":"製品","title":"完了した作業は表示しない","status":"done","wait_on":[99]},
        {"id":10,"status":"done"},
        {"id":11,"status":"todo"}
    ]});
    let before = db.clone();
    let text = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
    assert!(text.starts_with("作業中 0件・止まっている 0件・完了待ち 3件"));
    assert!(text.contains("依存作業を待つ\n進み具合：未記録｜ToDo 11 待ち、ToDo 99 待ち｜"));
    assert!(text.contains("以前の作業中表示を直す\n進み具合：未記録｜ToDo 99 待ち｜"));
    assert!(text.contains("未着手の依存作業を待つ\n進み具合：未記録｜ToDo 99 待ち｜"));
    assert!(!text.contains("完了した作業は表示しない"));
    assert!(!text.contains("ToDo 10 待ち"));
    assert!(!text.contains("1時間以上動きなし"));
    assert!(!text.contains("担当なし"));
    assert!(text.contains("あなたの返事待ち 0件（質問）\n\n## あなたの返事待ち\n今はありません"));
    assert!(!text.contains("質問を開く"));
    assert_eq!(db, before, "render must not release or mutate dependencies");
}

#[test]
fn finished_targets_stop_counting_as_pending_before_wait_metadata_is_cleared() {
    let mut db = json!({"items":[
        {"id":1,"project":"製品","title":"作業の再開を待つ","status":"wait","wait_on":[2],"updated":"2026-09-30 22:00"},
        {"id":2,"status":"todo"}
    ]});
    let waiting = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
    db["items"][1]["status"] = json!("done");
    let ready = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
    assert_ne!(waiting, ready);
    assert!(ready.starts_with("作業中 0件・止まっている 0件・完了待ち 0件・再開待ち 1件"));
    assert!(ready.contains("作業の再開を待つ\n進み具合：未記録｜再開待ち（待機先は完了）｜"));
    assert!(!ready.contains("ToDo 2 待ち"));
    assert!(!ready.contains("1時間以上動きなし"));
    assert_eq!(db["items"][0]["wait_on"], json!([2]));

    db["items"][0]["status"] = json!("doing");
    let doing = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
    assert!(doing.starts_with("作業中 1件・止まっている 0件・完了待ち 0件"));
    assert!(doing.contains("1時間以上動きなし"));
    assert!(!doing.contains("ToDo 2 待ち"));
}

#[test]
fn wait_status_without_targets_remains_visible_without_inventing_dependencies() {
    for wait_on in [json!([]), Value::Null] {
        let db = json!({"items":[{"id":1,"project":"製品","title":"再開を確認する","status":"wait","wait_on":wait_on,"updated":"2026-09-30 22:00"}]});
        let text = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
        assert!(text.contains("再開を確認する\n進み具合：未記録｜再開待ち｜"));
        assert!(text.contains("完了待ち 0件・再開待ち 1件"));
        assert!(!text.contains("待機先は完了"));
        assert!(!text.contains("1時間以上動きなし"));
        assert!(!text.contains("質問を開く"));
    }
}

#[test]
fn owner_block_and_real_question_remain_visible_alongside_dependency_wait() {
    let mut db = json!({"items":[
        {"id":1,"project":"製品","title":"公開を進める","status":"block","wait_on":[2],"progress":60,"note":"オーナー判断待ち。質問スレッド123","updated":"2026-09-30 22:00"},
        {"id":2,"status":"todo"}
    ]});
    let questions = vec![
        json!({"thread_id":"123","after":"124","project":"製品","title":"公開してよいか","open":true}),
    ];
    let text = render(
        &db,
        &questions,
        &json!({"guild_id":"G"}),
        &Names::parse(""),
        1791270000.,
    );
    assert!(text.contains("完了待ち 1件・あなたの返事待ち 1件（質問）"));
    assert!(text.contains("60%｜返事待ち（オーナー判断待ち）・ToDo 2 待ち｜"));
    assert_eq!(text.matches("[質問を開く]").count(), 1);
    assert!(text.contains("https://discord.com/channels/G/123"));
    db["items"][1]["status"] = json!("done");
    let ready = render(
        &db,
        &questions,
        &json!({"guild_id":"G"}),
        &Names::parse(""),
        1791270000.,
    );
    assert!(ready.starts_with("作業中 0件・止まっている 1件・完了待ち 0件"));
    assert!(ready.contains("60%｜返事待ち（オーナー判断待ち）｜"));
    assert!(!ready.contains("ToDo 2 待ち"));
    assert_eq!(db["items"][0]["status"], "block");
}

#[test]
fn completion_wait_does_not_turn_into_unassigned_work() {
    let mut db = json!({"items":[
        {"id":1,"project":"tools","title":"待機状態の作業","status":"wait","wait_on":[99],"created_s":1.},
        {"id":2,"project":"tools","title":"未着手に戻った待機作業","status":"todo","wait_on":[99],"created_s":1.},
        {"id":3,"project":"tools","title":"通常の未担当作業","status":"todo","created_s":1.}
    ]});
    mark_unassigned(&mut db, &[], &json!({"queue":[]}), 1791270000.);
    assert_eq!(db["items"][0]["board_unassigned"], false);
    assert_eq!(db["items"][1]["board_unassigned"], false);
    assert_eq!(db["items"][2]["board_unassigned"], true);
    let text = render(&db, &[], &json!({}), &Names::parse(""), 1791270000.);
    assert!(text.starts_with("作業中 0件・止まっている 1件・完了待ち 2件"));
    assert_eq!(text.matches("止まっている（担当なし）").count(), 1);
}
