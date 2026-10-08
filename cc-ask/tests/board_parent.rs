mod common;
use cc_ask::{board, board_display::*, core::*};
use cc_task_title::Names;
use common::{message, Harness};
use serde_json::{json, Value};

fn setup() -> Harness {
    let h = Harness::new();
    atomic_json(&h.ctx.here.join("board.json"), &json!({"guild_id":"G","thread_id":"500","channel_id":"10","messages":[{"id":"120","content":"old"}]}), 1).unwrap();
    let mut f = h.fake.borrow_mut();
    f.threads.insert("500".into(), json!({"id":"500","guild_id":"G","parent_id":"10","type":11,"thread_metadata":{"archived":false}}));
    f.messages.insert(
        "500".into(),
        vec![
            message("100", "作業状況の一覧", "BOT"),
            message("120", "old", "BOT"),
        ],
    );
    drop(f);
    atomic_json(
        &h.ctx.here.join("board-status.json"),
        &json!({"jobs":[{"project":"tools","status":"running","task_title":"【担当】一覧を直す"}]}),
        1,
    )
    .unwrap();
    atomic_json(
        &h.ctx.here.join("board-machine.json"),
        &json!({"cpu":70,"used":59,"total":94}),
        1,
    )
    .unwrap();
    h
}

#[test]
fn a_style_groups_duplicate_tasks_and_sorts_by_running_count() {
    let status = json!({"jobs":[
        {"project":"tools","status":"queued","task_title":"道具を直す"},
        {"project":"civ6-banseiki","status":"running","task_title":"【担当1】PR #86 の確認と修正"},
        {"project":"civ6","status":"running","task_title":"【担当2】PR #86 の確認と修正"},
        {"project":"GitDo","status":"starting","task_title":"製品を仕上げる"},
        {"project":"civ6","status":"succeeded","finished_at":"2026-09-30T13:00:00+09:00"},
        {"status":"failed","finished_at":"2026-09-30T14:00:00+09:00"},
        {"status":"lost","finished_at":"2026-09-30T15:00:01+09:00"},
        {"status":"succeeded","finished_at":"2026-09-29T15:00:00+09:00"}
    ]});
    let now = chrono::DateTime::parse_from_rfc3339("2026-09-30T15:00:00+09:00")
        .unwrap()
        .timestamp() as f64;
    let body = render(
        &status,
        &json!({"cpu":70,"used":59,"total":94}),
        &Names::parse(""),
        now,
    )
    .unwrap();
    assert!(body.starts_with("CPU 70%  ▮▮▮▮▮▮▮▯▯▯   メモリ 59 / 94 GB\n実行中 3件   順番待ち 1件"));
    assert!(body.contains("・PR #86 の確認と修正（2件）"));
    assert!(!body.contains("担当"));
    assert!(body.find("■ 万世紀").unwrap() < body.find("■ GitDo").unwrap());
    assert!(body.contains("3時間で終わった作業  2件（失敗 1）"));
}

#[test]
fn long_boards_are_one_message_with_whole_lower_projects_collapsed() {
    let jobs: Vec<_> = (0..60).flat_map(|i| (0..4).map(move |j| json!({"project":format!("製品{i:03}{}", "😀".repeat(30)),"status":"running","task_title":format!("項目{j}{}", "😀".repeat(90))}))).collect();
    let body = render(&json!({"jobs":jobs}), &Value::Null, &Names::parse(""), 0.).unwrap();
    let message = stamped(&body, 0.);
    assert!(message.encode_utf16().count() <= 2000);
    assert!(body.contains("ほか "));
    assert!(body.contains("実行中 240件"));
    assert!(body.ends_with("3時間で終わった作業  0件（失敗 0）"));
    assert!(body.contains("取得できません"));
}

#[test]
fn masks_client_names_case_insensitively_in_projects_and_task_titles() {
    let names = Names::parse("AcmeClient\nOtherClient\n");
    let body = render(&json!({"jobs":[{"project":"ACMECLIENT-prod","task_title":"【OtherClient】acmeclientの接続を直す","status":"running"}]}), &Value::Null, &names, 0.).unwrap();
    assert!(body.contains("■ クライアント案件"));
    assert!(body.contains("クライアント案件の接続を直す"));
    assert!(!body.to_lowercase().contains("acmeclient"));
    assert!(!body.to_lowercase().contains("otherclient"));
}

#[test]
fn truncation_does_not_merge_distinct_projects_or_tasks() {
    let prefix = "共通".repeat(40);
    let status = json!({"jobs":[
        {"project":format!("{prefix}甲"),"task_title":format!("{prefix}保存"),"status":"running"},
        {"project":format!("{prefix}甲"),"task_title":format!("{prefix}削除"),"status":"running"},
        {"project":format!("{prefix}乙"),"task_title":"別の作業","status":"running"}
    ]});
    let body = render(&status, &Value::Null, &Names::parse(""), 0.).unwrap();
    assert_eq!(body.matches('■').count(), 2);
    assert_eq!(body.matches("    ・").count(), 3);
    assert!(!body.contains("（2件）"));
}

#[test]
fn exact_full_history_page_and_old_parent_in_slots_are_migrated() {
    let mut h = setup();
    let mut state = read_json(&h.ctx.here.join("board.json")).unwrap();
    state["messages"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":"100","content":"old parent"}));
    atomic_json(&h.ctx.here.join("board.json"), &state, 1).unwrap();
    h.fake
        .borrow_mut()
        .messages
        .get_mut("500")
        .unwrap()
        .extend((121..219).map(|id| message(&id.to_string(), "人の返信", "OWN")));
    assert_eq!(h.fake.borrow().messages["500"].len(), 100);
    board::update(&mut h.ctx).unwrap();
    let state = read_json(&h.ctx.here.join("board.json")).unwrap();
    assert_eq!(state["parent_message_id"], "100");
    assert_eq!(state["messages"][1]["content"], state["parent_content"]);
    assert_eq!(h.fake.borrow().patches.len(), 2);
}

#[test]
fn updates_parent_moves_old_message_once_and_sends_no_posts_or_mentions() {
    let mut h = setup();
    let shared = h.state();
    assert_eq!(board::update(&mut h.ctx).unwrap()["changed"], true);
    assert_eq!(
        h.fake.borrow().messages["500"][0]["content"]
            .as_str()
            .unwrap()
            .lines()
            .next()
            .unwrap(),
        stamped("", h.ctx.now()).trim()
    );
    assert_eq!(
        h.fake.borrow().messages["500"][1]["content"],
        "（この一覧は親の投稿に移りました）"
    );
    let calls = h.fake.borrow().http_calls.len();
    h.fake.borrow_mut().now += 60.;
    assert_eq!(board::update(&mut h.ctx).unwrap()["changed"], false);
    assert_eq!(h.fake.borrow().http_calls.len(), calls);
    atomic_json(
        &h.ctx.here.join("board-machine.json"),
        &json!({"cpu":80,"used":59,"total":94}),
        1,
    )
    .unwrap();
    assert_eq!(board::update(&mut h.ctx).unwrap()["changed"], true);
    let f = h.fake.borrow();
    assert_eq!(f.patches.len(), 3);
    assert!(f.posts.is_empty());
    assert!(f
        .http_calls
        .iter()
        .filter(|c| c["method"] == "PATCH")
        .all(|c| c["body"]["allowed_mentions"]["parse"] == json!([])));
    assert!(f.llm_calls.is_empty() && f.launches.is_empty());
    drop(f);
    assert_eq!(h.state(), shared);
}

#[test]
fn partial_migration_retries_only_unfinished_edits() {
    let mut h = setup();
    h.fake
        .borrow_mut()
        .http_errors
        .insert("PATCH /channels/500/messages/120".into(), 1);
    assert!(board::update(&mut h.ctx).is_err());
    assert_eq!(h.fake.borrow().patches.len(), 1);
    assert_eq!(board::update(&mut h.ctx).unwrap()["changed"], true);
    assert_eq!(h.fake.borrow().patches.len(), 2);
    assert_eq!(board::update(&mut h.ctx).unwrap()["changed"], false);
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn missing_registration_or_invalid_binding_never_posts_or_edits() {
    let mut h = setup();
    h.ctx.cfg["guild_id"] = json!("other");
    assert!(board::update(&mut h.ctx).is_err());
    assert!(h.fake.borrow().http_calls.is_empty());
    h.ctx.cfg["guild_id"] = json!("G");
    h.fake.borrow_mut().threads.get_mut("500").unwrap()["parent_id"] = json!("wrong");
    assert!(board::update(&mut h.ctx).is_err());
    assert!(h.fake.borrow().patches.is_empty() && h.fake.borrow().posts.is_empty());
}

#[test]
fn selects_first_message_behind_full_history_pages_and_handles_channel_starter() {
    let mut h = setup();
    h.fake
        .borrow_mut()
        .messages
        .get_mut("500")
        .unwrap()
        .extend((121..250).map(|id| message(&id.to_string(), "人の返信", "OWN")));
    board::update(&mut h.ctx).unwrap();
    assert_eq!(
        read_json(&h.ctx.here.join("board.json")).unwrap()["parent_message_id"],
        "100"
    );
    let mut h = setup();
    h.fake.borrow_mut().messages.get_mut("500").unwrap()[0]["type"] = json!(21);
    h.fake
        .borrow_mut()
        .messages
        .insert("10".into(), vec![message("500", "親投稿", "BOT")]);
    board::update(&mut h.ctx).unwrap();
    assert!(h.fake.borrow().messages["10"][0]["content"]
        .as_str()
        .unwrap()
        .starts_with("作業状況"));
}
