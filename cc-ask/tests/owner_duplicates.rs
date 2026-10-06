mod common;
use cc_ask::{commands::*, core::*, project::*, questions, reminders, steering};
use common::*;
use serde_json::{json, Value};

fn job(h: &Harness, id: &str, info: Value, prompt: &str) {
    let dir = h.ctx.jobs.join("jobs").join(id);
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"), &info, 2).unwrap();
    std::fs::write(dir.join("prompt.md"), prompt).unwrap();
}

fn question() -> &'static str {
    "【OmegaPortal】どちらの画面にしますか？\na 一覧と編集欄を同時に表示する\nb 詳細を別画面に表示する"
}

#[test]
fn consuming_reply_is_returned_once_across_readers_and_automatic_delivery() {
    let mut h = Harness::new();
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"200","title":"【OmegaPortal】画面の選択","after":"200","open":true,"cwd":h.root.path().to_string_lossy()}]);
    h.save(&state);
    let id = h.reply("a", "200");
    let first = cmd_replies(&mut h.ctx, Some("200")).unwrap();
    let second = cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert_eq!(first[0]["replies"][0]["id"], id);
    assert_eq!(second[0]["replies"], json!([]));
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert!(reply_delivered(&h.state(), &id));
    assert!(h.fake.borrow().launches.is_empty());
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(
        h.fake.borrow().posts[0].1["content"],
        "【OmegaPortal】受け取りました。今の作業に反映します。"
    );
    assert_eq!(h.fake.borrow().posts[0].1["flags"], SILENT);
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    let repair = cc_ask::intake::deliver_reply(&mut h.ctx, "200", &id, None).unwrap();
    assert_eq!(repair["delivery"], "confirmed");
    assert!(h.fake.borrow().launches.is_empty());
}

#[test]
fn answered_direct_question_is_suppressed_even_for_silent_posts() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    let mut old = message("201", question(), "BOT");
    old["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![old, message("202", "a", "OWN")]);
    let body = h.root.path().join("question.txt");
    std::fs::write(&body, question()).unwrap();
    let result = cmd_post(&mut h.ctx, "200", &body, &[], false).unwrap();
    assert_eq!(result["suppressed"], "already_answered");
    assert_eq!(result["latest_owner_reply"]["id"], "202");
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.fake.borrow().patches.is_empty());
    assert!(h.state()["questions"].as_array().unwrap().is_empty());
}

#[test]
fn stale_parallel_worker_must_read_the_latest_answer_before_reasking() {
    let mut h = Harness::new();
    let answer_id = "1556585064337248357";
    let mut old = message("1556580835400876032", question(), "BOT");
    old["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![old, message(answer_id, "a", "OWN")]);
    job(
        &h,
        "parallel",
        json!({"created_at":"2026-10-05T16:32:44+09:00","status":"running","thread_id":"worker-session"}),
        "画面案を作る",
    );
    let changed = "どちらを選びますか？\na 一覧と右側の詳細を同時に表示する\nb 一覧を広げ、詳細は別画面にする";
    assert_eq!(
        questions::answered_repost(&mut h.ctx, "200", changed, Some("parallel"), &[])
            .unwrap()
            .unwrap()["id"],
        answer_id
    );
    let mut state = h.state();
    state["answer_receipts"][answer_id] = json!({"read_by":"worker-session"});
    h.save(&state);
    assert!(
        questions::answered_repost(&mut h.ctx, "200", changed, Some("parallel"), &[])
            .unwrap()
            .is_none()
    );
    job(
        &h,
        "fresh",
        json!({"created_at":"2026-10-05T18:00:00+09:00","status":"running"}),
        "新しい依頼",
    );
    assert!(
        questions::answered_repost(&mut h.ctx, "200", changed, Some("fresh"), &[])
            .unwrap()
            .is_none()
    );
}

#[test]
fn completed_manual_job_input_prevents_colonization_replay() {
    let mut h = Harness::new();
    let tid = "1556306797252444230";
    let mid = "1556592867940765737";
    let mut reply = message(mid, "<@1554269962229649478> \nなんか違うんよな…\n子供らしい見た目で大剣を2本持ってほしいんよな。", "OWN");
    reply["timestamp"] = json!("2026-10-05T18:04:19.814+09:00");
    h.fake
        .borrow_mut()
        .messages
        .insert(tid.into(), vec![reply.clone()]);
    h.fake.borrow_mut().threads.insert(tid.into(), json!({"id":tid,"name":"【アイデア】Colonization（武力制圧に特化したCiv系ゲーム）","parent_id":REQUEST_CHANNEL_ID,"owner_id":"OWN","type":11}));
    job(&h, "20261005-180809-req-444230", json!({"created_at":"2026-10-05T18:08:09+09:00","status":"succeeded"}), "オーナーの指摘（2026-10-05 18:04 JST）：「なんか違うんよな…子供らしい見た目で大剣を2本持ってほしいんよな。」\n画像を作る");
    let mut state = h.state();
    state["requests"][tid] = json!({"after":"0","title":"Colonization","job":null});
    h.save(&state);
    cc_ask::intake::intake_thread(
        &mut h.ctx,
        &mut state,
        &json!({"id":tid,"name":"Colonization","parent_id":REQUEST_CHANNEL_ID}),
        "OWN",
        &mut false,
    )
    .unwrap();
    assert_eq!(
        state["answer_receipts"][mid]["handled_by"],
        "20261005-180809-req-444230"
    );
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn quote_in_copied_history_wrong_thread_or_wrong_minute_is_not_delivery() {
    for (header, scope, status) in [
        (
            "以前の会話:\nオーナーの指摘（2026-10-05 18:04 JST）：「同じ文章」",
            "100",
            "succeeded",
        ),
        (
            "オーナーの指摘（2026-10-05 18:04 JST）：「同じ文章」",
            "200",
            "succeeded",
        ),
        (
            "オーナーの指摘（2026-10-05 18:03 JST）：「同じ文章」",
            "100",
            "succeeded",
        ),
        (
            "オーナーの指摘（2026-10-05 18:04 JST）：「別の文章」",
            "100",
            "succeeded",
        ),
        (
            "オーナーの指摘（2026-10-05 18:04 JST）：「同じ文章」",
            "100",
            "failed",
        ),
    ] {
        let h = Harness::new();
        job(
            &h,
            "manual",
            json!({"created_at":"2026-10-05T18:08:09+09:00","status":status,"discord_thread_id":scope}),
            header,
        );
        let mut reply = message("300", "同じ文章", "OWN");
        reply["timestamp"] = json!("2026-10-05T18:04:19.814+09:00");
        let mut state = h.state();
        steering::confirm_original_inputs(&h.ctx, &mut state, "100", &[reply]).unwrap();
        assert!(!reply_delivered(&state, "300"), "{header} {scope} {status}");
    }
}

#[test]
fn cached_latest_answer_suppresses_a_recovered_older_generation() {
    let mut h = Harness::new();
    let mut state = h.state();
    record_owner_answers(&mut state, "200", &[message("500", "a", "OWN")], "OWN");
    questions::register_posted(
        &mut state,
        &json!({"id":"200","name":"【OmegaPortal】画面"}),
        "400",
        &["400".into()],
    );
    assert_eq!(state["questions"][0]["answered_reply"], "500");
    h.save(&state);
    let report = reminders::run(
        &mut h.ctx,
        &reminders::RemindArgs {
            dry_run: true,
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(report["due_questions"], 0);
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn project_titles_override_unknown_or_generic_folders() {
    let mut h = Harness::new();
    let names = ProjectNames::load(&h.ctx.client_projects).unwrap();
    assert_eq!(
        question_project(
            &mut h.ctx,
            &json!({"title":"【OmegaPortal】画面","project":"プロジェクト不明","cwd":REQUEST_CWD}),
            &names
        ),
        "OmegaPortal"
    );
    assert_eq!(
        question_project(
            &mut h.ctx,
            &json!({"title":"【アイデア】Colonization（武力制圧）","cwd":REQUEST_CWD}),
            &names
        ),
        "Colonization"
    );
    assert_eq!(question_project(&mut h.ctx, &json!({}), &names), "この作業");
}

struct OutputFault {
    fake: SharedFake,
    crash: bool,
}
impl Effects for OutputFault {
    fn now(&self) -> f64 {
        self.fake.now()
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        self.fake.http(method, url, body, discord)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.fake.run(argv, timeout)
    }
    fn reply_output(&mut self, _: &str) -> Result<()> {
        if self.crash {
            panic!("reader interrupted before stdout");
        }
        Err("broken stdout pipe".into())
    }
}

#[test]
fn interrupted_or_broken_stdout_reservation_recovers_without_losing_or_duplicating_reply() {
    for crash in [false, true] {
        let mut h = Harness::new();
        let mut state = h.state();
        state["questions"] = json!([{"thread_id":"200","after":"200","title":"【製品】選択","cwd":h.root.path().to_string_lossy(),"open":true}]);
        h.save(&state);
        let id = h.reply("a", "200");
        h.ctx.effects = Box::new(OutputFault {
            fake: SharedFake(h.fake.clone()),
            crash,
        });
        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            cmd_replies(&mut h.ctx, Some("200"))
        }));
        assert!(if crash {
            result.is_err()
        } else {
            result.unwrap().is_err()
        });
        assert!(!reply_delivered(&h.state(), &id));
        assert!(reply_reserved(&h.state(), &id));
        assert!(h.state()["questions"][0].get("seen_reply").is_none());
        h.ctx.effects = Box::new(SharedFake(h.fake.clone()));
        cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
        assert!(h.fake.borrow().launches.is_empty());
        h.fake.borrow_mut().now += 121.;
        assert_eq!(
            cmd_replies(&mut h.ctx, Some("200")).unwrap()[0]["replies"][0]["id"],
            id
        );
        assert_eq!(
            cmd_replies(&mut h.ctx, Some("200")).unwrap()[0]["replies"],
            json!([])
        );
        assert_eq!(h.fake.borrow().posts.len(), 1);
    }
}

#[test]
fn successful_output_event_recovers_a_missing_delivery_checkpoint_without_replay() {
    let h = Harness::new();
    job(&h, "reader", json!({"status":"running"}), "元の作業");
    std::fs::write(h.ctx.jobs.join("jobs/reader/events.jsonl"),serde_json::to_string(&json!({"item":{"command":"discord_ask.exe replies --thread 200","exit_code":0,"aggregated_output":"[{\"thread_id\":\"200\",\"replies\":[{\"id\":\"201\"}]}]"}})).unwrap()).unwrap();
    let mut state = h.state();
    state["answer_receipts"]["201"] = json!({"thread":"200","read_reservation":{"reader":"session","job":"reader","expires_at":h.ctx.now()+120.}});
    recover_reply_reads(&h.ctx, &mut state).unwrap();
    assert!(reply_delivered(&state, "201"));
    assert!(!reply_reserved(&state, "201"));
}

#[test]
fn expired_read_before_seen_cursor_is_returned_even_after_a_newer_reply_was_consumed() {
    let mut h = Harness::new();
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"200","after":"200","open":true,"cwd":h.root.path().to_string_lossy()}]);
    h.save(&state);
    let first = h.reply("earlier-input", "200");
    h.ctx.effects = Box::new(OutputFault {
        fake: SharedFake(h.fake.clone()),
        crash: false,
    });
    assert!(cmd_replies(&mut h.ctx, Some("200")).is_err());
    h.ctx.effects = Box::new(SharedFake(h.fake.clone()));
    let second = h.reply("later-input", "200");
    assert_eq!(
        cmd_replies(&mut h.ctx, Some("200")).unwrap()[0]["replies"][0]["id"],
        second
    );
    assert_eq!(h.state()["questions"][0]["seen_reply"], second);
    h.fake.borrow_mut().now += 121.;
    assert_eq!(
        cmd_replies(&mut h.ctx, Some("200")).unwrap()[0]["replies"][0]["id"],
        first
    );
    assert_eq!(
        cmd_replies(&mut h.ctx, Some("200")).unwrap()[0]["replies"],
        json!([])
    );
    assert_eq!(h.state()["questions"][0]["seen_reply"], second);
}

#[test]
fn trimmed_continuation_stays_trimmed_after_the_launch_budget_defers_it() {
    let mut h = Harness::new();
    let mut state = h.state();
    let first = "1556585064337248357";
    let second = "1556592867940765737";
    h.fake.borrow_mut().now = snowflake_time(second) + 601.;
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![
            message(first, "remaining-continuation", "OWN"),
            message(second, "consumed-continuation", "OWN"),
        ],
    );
    let cwd = h.root.path().to_string_lossy().to_string();
    state["questions"] = json!([{"thread_id":"200","after":"200","title":"【製品】質問","cwd":cwd,"open":true,"continuation":{"after":second,"owner_message_ids":[first,second],"route":{"cwd":cwd,"repo":cwd,"main":false},"prompt":"remaining-continuation consumed-continuation","launch":{"key":"continuation-key","selection":{"model":"gpt-6.1-sol","effort":"max","task_type":"implementation","reason":"test"}}}}]);
    state["answer_receipts"][second] = json!({"thread":"200","handled_by":"other-worker"});
    h.save(&state);
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut true).unwrap();
    assert_eq!(
        h.state()["questions"][0]["continuation"]["owner_message_ids"],
        json!([first])
    );
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 1);
    assert_eq!(f.launches[0]["launch_key"], "continuation-key");
    assert!(s(&f.launches[0]["prompt"]).contains("remaining-continuation"));
    assert!(!s(&f.launches[0]["prompt"]).contains("consumed-continuation"));
    assert_eq!(
        h.state()["answer_receipts"][second]["handled_by"],
        "other-worker"
    );
}

#[test]
fn another_workers_read_only_view_allows_a_new_question_without_consuming_again() {
    let mut h = Harness::new();
    let mid = "1556585064337248357";
    let mut old = message("1556580835400876032", question(), "BOT");
    old["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![old, message(mid, "a", "OWN")]);
    job(
        &h,
        "viewer",
        json!({"created_at":"2026-10-05T16:00:00+09:00","status":"running"}),
        "作業",
    );
    std::fs::write(h.ctx.jobs.join("jobs/viewer/events.jsonl"),serde_json::to_string(&json!({"item":{"command":"discord_ask.exe replies --thread 200 --read-only","exit_code":0,"aggregated_output":serde_json::to_string(&json!([{"thread_id":"200","replies":[{"id":mid}]}])).unwrap()}})).unwrap()).unwrap();
    let mut state = h.state();
    state["answer_receipts"][mid] = json!({"handled_by":"other-worker"});
    h.save(&state);
    assert!(questions::answered_repost(
        &mut h.ctx,
        "200",
        "どちらの料金にしますか？\na 月額\nb 買い切り",
        Some("viewer"),
        &[]
    )
    .unwrap()
    .is_none());
    assert!(!steering::recorded_read(&h.ctx, "viewer", "200", &json!({"id":mid})).unwrap());
}

#[test]
fn revised_image_is_a_new_generation_but_the_same_image_is_not_reasked() {
    let mut h = Harness::new();
    let mut old = message("201", question(), "BOT");
    old["author"]["bot"] = json!(true);
    old["attachments"] = json!([{"filename":"image.png"}]);
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![old, message("202", "b 修正してください", "OWN")],
    );
    let mut state = h.state();
    state["questions"] =
        json!([{"thread_id":"200","question_message":"201","attachment_hashes":["original-hash"]}]);
    h.save(&state);
    assert!(questions::answered_repost(
        &mut h.ctx,
        "200",
        question(),
        None,
        &["revised-hash".into()]
    )
    .unwrap()
    .is_none());
    assert!(questions::answered_repost(
        &mut h.ctx,
        "200",
        question(),
        None,
        &["original-hash".into()]
    )
    .unwrap()
    .is_some());
}

#[test]
fn sidecar_thread_scope_matches_a_finished_manual_input_and_rejects_other_scope() {
    for correct in [false, true] {
        let h = Harness::new();
        let jid = if correct {
            "finished-manual"
        } else {
            "20261005-180809-req-100"
        };
        job(
            &h,
            jid,
            json!({"created_at":"2026-10-05T18:08:09+09:00","status":"succeeded"}),
            "オーナーの指摘（2026-10-05 18:04 JST）：「指示」",
        );
        atomic_json(
            &h.ctx
                .jobs
                .join("jobs")
                .join(jid)
                .join("discord-threads.json"),
            &json!({"thread_ids":[if correct {"100"} else {"200"}],"source":"manual"}),
            2,
        )
        .unwrap();
        let mut reply = message("300", "指示", "OWN");
        reply["timestamp"] = json!("2026-10-05T18:04:19.814+09:00");
        let mut state = h.state();
        steering::confirm_original_inputs(&h.ctx, &mut state, "100", &[reply]).unwrap();
        assert_eq!(reply_delivered(&state, "300"), correct);
    }
}

#[test]
fn partially_consumed_request_reservation_dispatches_only_remaining_reply() {
    let mut h = Harness::new();
    h.fake.borrow_mut().messages.insert(
        "100".into(),
        vec![
            message("201", "remaining-action", "OWN"),
            message("202", "consumed-action", "OWN"),
        ],
    );
    let mut state = h.state();
    state["answer_receipts"]["202"] = json!({"thread":"100","handled_by":"manual-worker"});
    state["requests"]["100"] = json!({"after":"100","job":null,"title":"依頼A","launch":{"kind":"request","key":"pending-input","selection":{"model":"gpt-6.1-sol","effort":"max","task_type":"implementation","reason":"test"}},"request_after":"202","request_message_ids":["201","202"],"request_messages":h.fake.borrow().messages["100"]});
    h.save(&state);
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 1);
    assert!(s(&f.launches[0]["prompt"]).contains("remaining-action"));
    assert!(!s(&f.launches[0]["prompt"]).contains("consumed-action"));
    assert_eq!(
        h.state()["answer_receipts"]["202"]["handled_by"],
        "manual-worker"
    );
    assert_eq!(h.state()["answer_receipts"]["201"]["handled_by"], "job1");
}

#[test]
fn partially_consumed_steering_reservation_rebuilds_action_and_keeps_launch_key() {
    let mut h = Harness::new();
    h.tick();
    let m1 = h.reply("remaining-correction", "100");
    let m2 = h.reply("consumed-correction", "100");
    let messages = h.fake.borrow().messages["100"]
        .iter()
        .filter(|m| [&m1, &m2].contains(&&s(&m["id"]).to_owned()))
        .cloned()
        .collect::<Vec<_>>();
    let mut state = h.state();
    state["answer_receipts"][&m2] = json!({"thread":"100","handled_by":"manual-worker"});
    let info = job_info(&h.ctx, "job1");
    state["requests"]["100"]["owner_update"] = json!({"key":"pending-correction","source":{"id":"job1","cwd":info["cwd"],"model":info["model"],"effort":info["effort"]},"session":"","resume":false,"original_request":"元の作業","after":m2,"messages":messages,"prompt":"remaining-correction consumed-correction"});
    h.save(&state);
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 2);
    assert_eq!(f.launches[1]["launch_key"], "pending-correction");
    assert!(s(&f.launches[1]["prompt"]).contains("remaining-correction"));
    assert!(!s(&f.launches[1]["prompt"]).contains("consumed-correction"));
    assert_eq!(
        h.state()["answer_receipts"][&m2]["handled_by"],
        "manual-worker"
    );
}
