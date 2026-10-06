mod common;
use cc_ask::core::*;
use common::*;
use serde_json::{json, Value};
use std::panic::{catch_unwind, AssertUnwindSafe};

struct ReplyAtArchive {
    shared: SharedFake,
    injected: bool,
}
impl Effects for ReplyAtArchive {
    fn now(&self) -> f64 {
        self.shared.now()
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.shared.run(argv, timeout)
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if !self.injected && method == "PATCH" && body.is_some_and(|b| b["archived"] == true) {
            self.injected = true;
            let mut fake = self.shared.0.borrow_mut();
            fake.sequence += 1;
            let id = fake.sequence.to_string();
            let tid = url.split('/').next_back().unwrap();
            fake.messages.entry(tid.into()).or_default().push(message(
                &id,
                "終了と同時に届いた追加指示を必ず反映して",
                "OWN",
            ));
        }
        self.shared.http(method, url, body, discord)
    }
}

fn source(h: &Harness, context: u64) {
    let file = h.ctx.jobs.join("jobs/job1/job.json");
    let mut job = read_json(&file).unwrap();
    job["cwd"] = json!(h.root.path().to_string_lossy());
    job["sandbox"] = json!("danger-full-access");
    job["network"] = json!(false);
    job["context_pct"] = json!(context);
    atomic_json(&file, &job, 2).unwrap();
}
fn question(h: &Harness) {
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"100","after":"100","title":"依頼A","project":"テスト","open":true,"continuation":{"job":"job1","after":"100","selection":{"model":"gpt-6.1-sol","effort":"max"}}}]);
    h.save(&state);
}
fn run(h: &mut Harness, question: bool) {
    if question {
        cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    } else {
        h.tick();
    }
}
#[test]
fn message_during_run_cancels_only_source_and_resumes_with_verbatim_quote_and_same_settings() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 40);
        if q {
            question(&h);
        }
        let text = "a なぜ追加費用がかかる？\n前の指示を優先しないで。";
        let mid = h.reply(text, "100");
        run(&mut h, q);
        let f = h.fake.borrow();
        assert_eq!(f.launches.len(), 2);
        let launch = &f.launches[1];
        assert_eq!(launch["resume_session"], "session-job1");
        assert_eq!(launch["sandbox"], "danger-full-access");
        assert_eq!(launch["cwd"], h.root.path().to_string_lossy().as_ref());
        assert_eq!(launch["model"], f.launches[0]["model"]);
        assert_eq!(launch["effort"], f.launches[0]["effort"]);
        assert!(s(&launch["prompt"]).contains(text));
        assert!(s(&launch["prompt"]).contains("以前の指示より優先"));
        let cancelled: Vec<_> = f
            .runs
            .iter()
            .filter(|(a, _)| a.iter().any(|a| a == "cancel"))
            .collect();
        assert_eq!(cancelled.len(), 1);
        let at = cancelled[0].0.iter().position(|a| a == "cancel").unwrap();
        assert_eq!(cancelled[0].0[at + 1], "job1");
        let receipt = f
            .posts
            .iter()
            .find(|(_, p)| p["message_reference"]["message_id"] == mid)
            .unwrap();
        assert_eq!(receipt.1["flags"], SILENT);
        assert_eq!(receipt.1["allowed_mentions"]["users"], json!([]));
    }
}
#[test]
fn high_context_or_missing_session_uses_fresh_handoff_with_all_owner_messages_in_order() {
    for context in [75, 90, 10] {
        let mut h = Harness::new();
        h.tick();
        source(&h, context);
        if context == 10 {
            let p = h.ctx.jobs.join("jobs/job1/job.json");
            let mut j = read_json(&p).unwrap();
            j["thread_id"] = Value::Null;
            atomic_json(&p, &j, 2).unwrap();
        }
        h.finish("job1", "running", Some("前の作業で修正済みの内容"), None);
        h.reply("追加指示その一", "100");
        h.reply("追加指示その二", "100");
        h.tick();
        let f = h.fake.borrow();
        let p = s(&f.launches[1]["prompt"]);
        assert_eq!(f.launches[1]["resume_session"], "");
        assert!(p.contains("READMEを直して"));
        assert!(p.contains("前の作業で修正済みの内容"));
        assert!(p.contains(h.root.path().to_str().unwrap()));
        let section = p.split("オーナーの全投稿（古い順）:").nth(1).unwrap();
        assert!(section.find("READMEを直して").unwrap() < section.find("追加指示その一").unwrap());
        assert!(section.find("追加指示その一").unwrap() < section.find("追加指示その二").unwrap());
    }
}
#[test]
fn finished_with_unseen_message_follows_up_immediately_and_reply_identifies_the_message() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        if q {
            question(&h);
        }
        h.finish("job1", "succeeded", Some("状態: 完了\n古い結果"), None);
        let mid = h.reply(
            "どういう意味？前回の指示がまったく反映されてないじゃん。",
            "100",
        );
        run(&mut h, q);
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(!h
            .fake
            .borrow()
            .posts
            .iter()
            .any(|(_, p)| s(&p["content"]).contains("古い結果")));
        h.finish(
            "job2",
            "succeeded",
            Some("状態: 完了\n指示を反映しました"),
            None,
        );
        run(&mut h, q);
        assert!(h.text().contains("今回反映したオーナーの投稿"));
        assert!(h.text().contains(&mid));
        assert!(h.text().contains("どういう意味？"));
    }
}
#[test]
fn receipt_is_once_even_after_checkpoint_loss_and_new_message_triggers_another_resume() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    let mid = h.reply("指示を追加", "100");
    h.tick();
    let mut st = h.state();
    st["answer_receipts"][&mid]["posted"] = json!(false);
    h.save(&st);
    h.tick();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake
            .borrow()
            .posts
            .iter()
            .filter(|(_, p)| p["message_reference"]["message_id"] == mid)
            .count(),
        1
    );
    h.reply("再開中にもう一件", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert!(s(&h.fake.borrow().launches[2]["prompt"]).contains("再開中にもう一件"));
}
#[test]
fn lost_launch_response_and_crash_adopt_one_reserved_successor() {
    for crash in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        h.reply("追加指示", "100");
        if crash {
            h.fake.borrow_mut().crash_after_launch = true;
            assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
        } else {
            h.fake.borrow_mut().lose_launch_response = true;
            h.tick();
        }
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    }
}
#[test]
fn failed_resume_session_starts_fresh_immediately() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    h.reply("追加指示", "100");
    h.tick();
    h.finish("job2", "failed", None, Some("no rollout found for session"));
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 3);
    assert_eq!(f.launches[2]["resume_session"], "");
    assert!(s(&f.launches[2]["prompt"]).contains("READMEを直して"));
    assert!(s(&f.launches[2]["prompt"]).contains("追加指示"));
}
#[test]
fn final_reply_gate_rechecks_after_result_preparation() {
    for snapshot in [2, 3] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        h.finish("job1", "succeeded", Some("状態: 完了\n古い結果"), None);
        h.fake.borrow_mut().history_injection =
            Some((snapshot, "100".into(), "最終返信の直前の指示".into()));
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(!h
            .fake
            .borrow()
            .posts
            .iter()
            .any(|(_, p)| s(&p["content"]).contains("古い結果")));
        assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("最終返信の直前の指示"));
    }
}
#[test]
fn unrelated_thread_job_is_never_cancelled_even_in_same_folder() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    let dir = h.ctx.jobs.join("jobs/other");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"),&json!({"id":"other","status":"running","discord_thread_id":"200","cwd":h.root.path().to_string_lossy()}),2).unwrap();
    h.reply("追加指示", "100");
    h.tick();
    assert_eq!(
        read_json(&dir.join("job.json")).unwrap()["status"],
        "running"
    );
    assert!(!h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(a, _)| a.iter().any(|x| x == "cancel") && a.iter().any(|x| x == "other")));
}

#[test]
fn original_question_worker_receives_answer_despite_old_receipt_seen_or_finished_history() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 20);
    let dir = h.ctx.jobs.join("jobs/older");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"older","status":"succeeded","discord_thread_id":"100"}),
        2,
    )
    .unwrap();
    let mid = h.reply("前回の回答は反映済み扱いだったが作業では未反映", "100");
    let mut state = h.state();
    state["questions"] =
        json!([{"thread_id":"100","after":"100","seen_reply":mid,"title":"質問","open":true}]);
    h.save(&state);
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake.borrow().launches[1]["resume_session"],
        "session-job1"
    );
    assert!(s(&h.fake.borrow().launches[1]["prompt"])
        .contains("前回の回答は反映済み扱いだったが作業では未反映"));
}

#[test]
fn updates_for_two_running_threads_share_one_launch_per_tick() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["owner_id"] = json!("OWN");
    h.fake
        .borrow_mut()
        .messages
        .get_mut("200")
        .unwrap()
        .push(message("200", "別の依頼", "OWN"));
    h.tick();
    h.tick();
    let first = h.reply("依頼Aの追加", "100");
    let second = h.reply("別の依頼の追加", "200");
    cc_ask::intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert_eq!(h.state()["answer_receipts"][&first]["handled_by"], "job3");
    assert!(!truth(&h.state()["answer_receipts"][&second]["handled_by"]));
    assert_eq!(job_status(&h.ctx, "job2"), "running");
    cc_ask::intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 4);
    assert!(s(&h.fake.borrow().launches[2]["prompt"]).contains("依頼Aの追加"));
    assert!(s(&h.fake.borrow().launches[3]["prompt"]).contains("別の依頼の追加"));
}

#[test]
fn reply_racing_archive_is_kept_across_restart_and_reaches_next_job() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        if q {
            question(&h);
        }
        h.finish(
            "job1",
            "succeeded",
            Some("状態: 完了\n最初の作業が完了しました"),
            None,
        );
        h.ctx.effects = Box::new(ReplyAtArchive {
            shared: SharedFake(h.fake.clone()),
            injected: false,
        });
        run(&mut h, q);
        assert_eq!(
            h.fake.borrow().threads["100"]["thread_metadata"]["archived"],
            false
        );
        // Reload the durable checkpoint; no in-memory receipt can rescue the test.
        let persisted = h.state();
        h.save(&persisted);
        for _ in 0..3 {
            run(&mut h, q);
        }
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(s(&h.fake.borrow().launches[1]["prompt"])
            .contains("終了と同時に届いた追加指示を必ず反映して"));
        let receipts = h.state()["answer_receipts"].clone();
        assert!(receipts
            .as_object()
            .unwrap()
            .values()
            .any(|r| r["handled_by"] == "job2"));
    }
}

#[test]
fn secondary_explicit_thread_of_one_job_receives_owner_update() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    let path = h.ctx.jobs.join("jobs/job1/job.json");
    let mut info = read_json(&path).unwrap();
    info["discord_thread_id"] = json!("100");
    info["discord_thread_ids"] = json!(["100", "200"]);
    atomic_json(&path, &info, 2).unwrap();
    atomic_json(
        &h.ctx.jobs.join("jobs/job1/discord-threads.json"),
        &json!({"discord_thread_id":"100","thread_ids":["100","200"],"source":"explicit"}),
        2,
    )
    .unwrap();
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"200","after":"200","title":"同じ担当への質問","open":true,"continuation":{"job":"job1","after":"200","selection":{"model":"gpt-6.1-sol","effort":"max"}}}]);
    h.save(&state);
    let mid = h.reply("副スレッドの追加指示", "200");
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("副スレッドの追加指示"));
    assert_eq!(h.state()["answer_receipts"][&mid]["handled_by"], "job2");
}
