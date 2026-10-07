mod common;
use cc_ask::{core::*, intake, project::*, steering};
use common::*;
use serde_json::json;

#[test]
fn repair_delivers_once_and_recovers_a_failed_same_key_launch() {
    let mut h = Harness::new();
    h.tick();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let mid = h.reply("もっと見た目を作りこんでほしい", "100");
    h.fake.borrow_mut().launch_transport_fail = true;
    assert!(intake::deliver_reply(&mut h.ctx, "100", &mid, Some("job1")).is_err());
    assert!(!truth(&h.state()["answer_receipts"][&mid]["handled_by"]));
    let pending_key = h.state()["requests"]["100"]["owner_update"]["key"].clone();
    h.fake.borrow_mut().launch_transport_fail = false;
    h.fake.borrow_mut().now += cc_ask::reply_recovery::RETRY_INTERVAL_S;
    let delivered = intake::deliver_reply(&mut h.ctx, "100", &mid, Some("job1")).unwrap();
    assert_eq!(delivered["delivery"], "delivered");
    assert_eq!(
        h.fake.borrow().launches.last().unwrap()["launch_key"],
        pending_key
    );
    let count = h.fake.borrow().launches.len();
    let confirmed = intake::deliver_reply(&mut h.ctx, "100", &mid, Some("job1")).unwrap();
    assert_eq!(confirmed["job"], delivered["job"]);
    assert_eq!(confirmed["delivery"], "confirmed");
    assert_eq!(h.fake.borrow().launches.len(), count);
}

#[test]
fn owning_job_read_confirms_legacy_content_and_timestamp_without_restarting() {
    let mut h = Harness::new();
    h.tick();
    let mid = h.reply("a", "100");
    let message = h.fake.borrow().messages["100"].last().unwrap().clone();
    let output = serde_json::to_string(&json!([{"thread_id":"100","replies":[{"content":message["content"],"timestamp":message["timestamp"]}]}])).unwrap();
    let path = h.ctx.jobs.join("jobs/job1/events.jsonl");
    std::fs::write(&path, serde_json::to_string(&json!({"item":{"command":"discord_ask.exe replies --thread 100","exit_code":0,"aggregated_output":format!("prior command output\n{output}")}})).unwrap()).unwrap();
    let mut state = h.state();
    let question = json!({"thread_id":"100","seen_reply":mid,"job":"job1"});
    steering::confirm_recorded_reads(
        &h.ctx,
        &mut state,
        &question,
        std::slice::from_ref(&message),
    )
    .unwrap();
    assert_eq!(h.state()["answer_receipts"][&mid]["handled_by"], "job1");
    assert_eq!(h.fake.borrow().launches.len(), 1);
    let mut other = message.clone();
    other["timestamp"] = json!("2026-10-05T08:08:43.264+00:00");
    assert!(!steering::recorded_read(&h.ctx, "job1", "100", &other).unwrap());
    std::fs::write(&path, serde_json::to_string(&json!({"item":{"command":"discord_ask.exe replies --thread 100 --read-only","exit_code":0,"aggregated_output":output}})).unwrap()).unwrap();
    assert!(!steering::recorded_read(&h.ctx, "job1", "100", &message).unwrap());
}

#[test]
fn title_label_repairs_unknown_metadata_and_redacts_clients() {
    let mut h = Harness::new();
    std::fs::write(&h.ctx.client_projects, "HiddenClient\n").unwrap();
    let names = ProjectNames::load(&h.ctx.client_projects).unwrap();
    assert_eq!(
        question_project(
            &mut h.ctx,
            &json!({"title":"【OmegaPortal】製品の仕上げ","project":"プロジェクト不明"}),
            &names
        ),
        "OmegaPortal"
    );
    assert_eq!(
        question_project(
            &mut h.ctx,
            &json!({"title":"[Codex] 【HiddenClient】公開判断"}),
            &names
        ),
        "クライアント案件"
    );
    assert!(h.fake.borrow().git_calls.is_empty());
}

#[test]
fn two_running_workers_receive_replies_on_separate_ticks() {
    let mut h = Harness::new();
    h.tick();
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![message("200", "2件目", "OWN")]);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    let first = h.reply("一つ目の追記", "100");
    let second = h.reply("二つ目の追記", "200");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert_eq!(
        [&first, &second]
            .iter()
            .filter(|mid| truth(&h.state()["answer_receipts"][mid.as_str()]["handled_by"]))
            .count(),
        1
    );
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 4);
    assert!(truth(&h.state()["answer_receipts"][&first]["handled_by"]));
    assert!(truth(&h.state()["answer_receipts"][&second]["handled_by"]));
}

#[test]
fn bot_task_reply_is_received_when_routing_is_disabled() {
    let mut h = Harness::new();
    h.fake.borrow_mut().messages.insert("100".into(), vec![]);
    let mut state = h.state();
    state["reply_intake_since"] = json!("300");
    h.save(&state);
    h.fake.borrow_mut().threads.insert("300".into(),json!({"id":"300","guild_id":"G","type":11,"parent_id":"T","owner_id":"BOT","name":"【OmegaPortal】作業"}));
    h.fake.borrow_mut().messages.insert(
        "300".into(),
        vec![message("400", "cloneしなきゃなんじゃない？", "OWN")],
    );
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.state()["answer_receipts"]["400"]["handled_by"], "job1");
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 1);
}

#[test]
fn cutover_does_not_treat_receipt_cursor_as_delivery_evidence() {
    let h = Harness::new();
    h.fake.borrow_mut().messages.insert("100".into(), vec![]);
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"100","after":"100","seen_reply":"500","open":true}]);
    state["answer_receipts"] =
        json!({"400":{"thread":"100","posted":true},"600":{"thread":"100","posted":true}});
    migrate_reply_delivery(&h.ctx, &mut state).unwrap();
    assert!(!reply_delivered(&state, "400"));
    assert!(!reply_delivered(&state, "600"));
    state["questions"][0]["seen_reply"] = json!("600");
    migrate_reply_delivery(&h.ctx, &mut state).unwrap();
    assert!(!reply_delivered(&state, "600"));
}

#[test]
fn old_legacy_seen_receipt_is_recovered_while_proven_delivery_is_preserved() {
    let h = Harness::new();
    let mut state = h.state();
    state["reply_delivery_version"] = json!(2);
    state["answer_receipts"] = json!({"400":{"thread":"100","posted":true,"handled_via":"legacy-seen"},"500":{"thread":"100","handled_by":"job1","handled_via":"manual-confirmation"},"600":{"thread":"100","read_by":"reader"}});
    migrate_reply_delivery(&h.ctx, &mut state).unwrap();
    assert!(!reply_delivered(&state, "400"));
    assert!(reply_delivered(&state, "500"));
    assert!(reply_delivered(&state, "600"));
    assert_eq!(state["reply_delivery_version"], 3);
    assert_eq!(state["answer_receipts"]["400"]["read_retry"], true);
}

#[test]
fn explicit_repair_keeps_earlier_pending_replies_and_the_question_owner() {
    let mut h = Harness::new();
    h.tick();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let first = h.reply("先に届いた未処理の指示", "100");
    let second = h.reply("後に届いた指示", "100");
    let mut state = h.state();
    let old_after = state["requests"]["100"]["after"].clone();
    state["questions"] = json!([{"thread_id":"100","title":"【製品】質問","after":old_after,"open":true,"cwd":h.root.path().to_string_lossy(),"source":"posted"}]);
    h.save(&state);
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    let result = intake::deliver_reply(&mut h.ctx, "100", &second, Some("job1")).unwrap();
    assert_eq!(result["job"], "job2");
    assert_eq!(h.state()["questions"][0]["continuation"]["job"], "job2");
    assert_eq!(h.state()["requests"]["100"]["after"], old_after);
    assert_eq!(h.state()["answer_receipts"][&first]["handled_by"], "job2");
    assert_eq!(h.state()["answer_receipts"][&second]["handled_by"], "job2");
    let mut budget = false;
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
    intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 2);
}

#[test]
fn internally_launched_successor_of_an_external_worker_publishes_its_result_once() {
    let mut h = Harness::new();
    h.tick();
    let mut state = h.state();
    state["requests"]["100"]["external_owner"] = json!(true);
    h.save(&state);
    h.reply("新しい追加指示", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert!(!truth(&h.state()["requests"]["100"]["external_owner"]));
    h.finish(
        "job2",
        "succeeded",
        Some("状態: 完了\n追加指示の結果を返します。"),
        None,
    );
    h.tick();
    h.tick();
    assert_eq!(
        h.fake
            .borrow()
            .posts
            .iter()
            .filter(|(_, p)| s(&p["content"]).contains("追加指示の結果を返します。"))
            .count(),
        1
    );
    assert_eq!(h.fake.borrow().launches.len(), 2);
}
