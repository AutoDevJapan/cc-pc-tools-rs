mod common;
use cc_ask::{attachments, core::*, state_work::StateWork};
use common::{Fake, Harness, SharedFake};
use serde_json::{json, Value};
use std::{cell::RefCell, path::PathBuf, rc::Rc, time::Duration};

struct DuringWait {
    fake: SharedFake,
    home: PathBuf,
    on_run: bool,
    injected: bool,
}
impl DuringWait {
    fn inject(&mut self) -> Result<()> {
        if self.injected {
            return Ok(());
        }
        self.injected = true;
        // Same OS lock used by a different process, acquired without waiting.
        drop(ByteLock::acquire(
            &self.home.join("state.lock"),
            Duration::ZERO,
        )?);
        let home = self.home.clone();
        std::thread::spawn(move || {
            let mut sender = Harness::new();
            sender.ctx.here = home;
            let channel = json!({"id":"100","name":"公開方針","parent_id":"T"});
            let ids = attachments::post_tracked(
                &mut sender.ctx,
                "100",
                "<@OWN> どちらを公開しますか？\na 見本\nb 完成版",
                None,
                true,
                Some(&channel),
            )
            .unwrap();
            assert_eq!(ids.len(), 1);
            assert_eq!(
                sender.fake.borrow().posts[0].1["allowed_mentions"],
                json!({"parse":[],"users":["OWN"],"replied_user":false})
            );
        })
        .join()
        .unwrap();
        // The ZERO acquisition above proves no state writer lock spans HTTP.
        // Wall-clock speed of fixture setup is unrelated to this invariant.
        // The background operation is still waiting when the sender finishes.
        std::thread::sleep(Duration::from_millis(40));
        Ok(())
    }
}
impl Effects for DuringWait {
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
        if !self.on_run && method == "GET" {
            self.inject()?;
        }
        self.fake.http(method, url, body, discord)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        if self.on_run {
            self.inject()?;
        }
        self.fake.run(argv, timeout)
    }
}

#[test]
fn resume_http_wait_accepts_one_question_and_preserves_it_across_checkpoints() {
    let mut h = Harness::new();
    h.save(
        &json!({"questions":[{"thread_id":"200","after":"200","open":true,"source":"request"}]}),
    );
    h.ctx.effects = Box::new(DuringWait {
        fake: SharedFake(h.fake.clone()),
        home: h.ctx.here.clone(),
        on_run: false,
        injected: false,
    });
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 2);
    assert_eq!(h.state()["questions"][1]["question_message"], "1001");
}

#[test]
fn intake_subprocess_wait_accepts_one_question_without_losing_requests() {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(DuringWait {
        fake: SharedFake(h.fake.clone()),
        home: h.ctx.here.clone(),
        on_run: true,
        injected: false,
    });
    cc_ask::intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    assert!(!h.state()["requests"]["100"].is_null());
}

#[test]
fn worker_merge_preserves_new_questions_receipts_unknown_fields_and_repeated_saves() {
    let h = Harness::new();
    h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":true}],"requests":{}}));
    let work = StateWork::begin(&h.ctx).unwrap();
    let mut worker = h.ctx.load().unwrap();
    {
        let _state = ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).unwrap();
        let mut current = h.ctx.load_fresh().unwrap();
        current["questions"]
            .as_array_mut()
            .unwrap()
            .push(json!({"thread_id":"100","after":"101","open":true}));
        current["answer_receipts"] = json!({"201":{"read_by":"session"}});
        current["unknown"] = json!(9007199254740993u64);
        atomic_json(&h.ctx.here.join("state.json"), &current, 2).unwrap();
    }
    worker["resume_scan_after"] = json!(0);
    h.ctx.save(&worker).unwrap();
    worker["requests"]["200"] = json!({"launch":{"key":"stable"}});
    h.ctx.save(&worker).unwrap();
    worker["questions"]
        .as_array_mut()
        .unwrap()
        .push(json!({"thread_id":"300","after":"301","open":true}));
    h.ctx.save(&worker).unwrap();
    worker["questions"][1]["last_ping"] = json!(123);
    h.ctx.save(&worker).unwrap();
    drop(work);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 3);
    assert_eq!(h.state()["questions"][2]["thread_id"], "300");
    assert_eq!(h.state()["questions"][2]["last_ping"], 123);
    assert_eq!(h.state()["answer_receipts"]["201"]["read_by"], "session");
    assert_eq!(h.state()["unknown"], 9007199254740993u64);
    assert_eq!(h.state()["requests"]["200"]["launch"]["key"], "stable");
}

#[test]
fn changed_generation_cursor_close_or_snooze_cannot_be_overwritten_or_trigger_effects() {
    for key in ["after", "seen_reply", "open", "remind_not_before"] {
        let mut h = Harness::new();
        h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":true}]}));
        let work = StateWork::begin(&h.ctx).unwrap();
        let mut worker = h.ctx.load().unwrap();
        let mut current = h.ctx.load_fresh().unwrap();
        current["questions"][0][key] = match key {
            "open" => json!(false),
            "remind_not_before" => json!(h.ctx.now() + 86400.),
            _ => json!("205"),
        };
        atomic_json(&h.ctx.here.join("state.json"), &current, 2).unwrap();
        worker["questions"][0]["continuation"] = json!({"launch":{"key":"old"}});
        assert!(h
            .ctx
            .save(&worker)
            .unwrap_err()
            .starts_with("StateConflict:"));
        assert!(h
            .ctx
            .call(
                "POST",
                "/channels/200/messages",
                Some(&json!({"content":"stale"}))
            )
            .is_err());
        drop(work);
        assert_eq!(h.state(), current);
        assert!(h.fake.borrow().posts.is_empty());
    }
}

#[test]
fn a_main_post_reservation_invalidates_a_worker_started_before_it_even_after_checkpoint() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[],"requests":{}}));
    let work = StateWork::begin(&h.ctx).unwrap();
    let mut worker = h.ctx.load().unwrap();
    let mut fresh = h.ctx.load_fresh().unwrap();
    fresh["inbox_only_post_contexts"] = json!({"200":{"agent":"claude-main","session_id":"main"}});
    atomic_json(&h.ctx.here.join("state.json"), &fresh, 2).unwrap();
    assert!(cc_ask::state_work::check(&h.ctx)
        .unwrap_err()
        .starts_with("StateConflict:"));
    assert!(h.ctx.run(&["mock-launch".into()], 45).is_err());
    assert!(h
        .ctx
        .call(
            "POST",
            "/channels/200/messages",
            Some(&json!({"content":"stale"}))
        )
        .is_err());
    worker["resume_scan_after"] = json!(0);
    h.ctx.save(&worker).unwrap();
    assert!(cc_ask::state_work::check(&h.ctx).is_err());
    assert!(h.fake.borrow().runs.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    drop(work);
    assert_eq!(
        h.state()["inbox_only_post_contexts"],
        fresh["inbox_only_post_contexts"]
    );
}

#[test]
fn main_question_registration_still_invalidates_old_workers_after_the_reservation_is_released() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[],"requests":{},"inbox_only_post_contexts":{}}));
    let work = StateWork::begin(&h.ctx).unwrap();
    let before = h.ctx.load().unwrap();
    let mut current = h.ctx.load_fresh().unwrap();
    current["inbox_only_post_contexts"]["200"] = json!({"agent":"claude-main"});
    atomic_json(&h.ctx.here.join("state.json"), &current, 2).unwrap();
    current["questions"]
        .as_array_mut()
        .unwrap()
        .push(json!({"thread_id":"200","after":"201","agent":"claude-main","open":true}));
    current["inbox_only_post_contexts"] = json!({});
    atomic_json(&h.ctx.here.join("state.json"), &current, 2).unwrap();
    assert_eq!(
        before["inbox_only_post_contexts"],
        current["inbox_only_post_contexts"]
    );
    assert!(cc_ask::state_work::check(&h.ctx)
        .unwrap_err()
        .starts_with("StateConflict:"));
    assert!(h.ctx.run(&["mock-launch".into()], 45).is_err());
    assert!(h.fake.borrow().runs.is_empty());
    drop(work);
    assert_eq!(h.state(), current);
}

struct LoseResponse {
    fake: Rc<RefCell<Fake>>,
    home: PathBuf,
    lose: bool,
    answer_between_batches: bool,
}
impl Effects for LoseResponse {
    fn now(&self) -> f64 {
        self.fake.borrow().now
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method == "POST" {
            drop(ByteLock::acquire(
                &self.home.join("state.lock"),
                Duration::ZERO,
            )?);
            if self.answer_between_batches {
                let mut current = read_json(&self.home.join("state.json"))?;
                if current["questions"]
                    .as_array()
                    .is_some_and(|q| !q.is_empty())
                {
                    current["questions"][0]["seen_reply"] = json!("1005");
                    current["questions"][0]["remind_not_before"] = json!(2000000000);
                    atomic_json(&self.home.join("state.json"), &current, 2)?;
                }
            }
        }
        let mut response = self.fake.borrow_mut().http(method, url, body, discord)?;
        if method == "POST" && body.is_some_and(|b| truth(&b["nonce"])) {
            response["nonce"] = body.unwrap()["nonce"].clone();
            if let Some(m) = self
                .fake
                .borrow_mut()
                .messages
                .get_mut("200")
                .and_then(|ms| ms.last_mut())
            {
                // Discord history need not retain nonce after its short window.
                m.as_object_mut().unwrap().remove("nonce");
            }
        }
        if method == "POST" && self.lose {
            self.lose = false;
            return Err("lost response".into());
        }
        Ok(response)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.fake.borrow_mut().run(argv, timeout)
    }
}

#[test]
fn question_post_recovers_lost_response_without_second_post_and_allows_only_owner() {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(LoseResponse {
        fake: h.fake.clone(),
        home: h.ctx.here.clone(),
        lose: true,
        answer_between_batches: false,
    });
    let channel = json!({"id":"200","name":"公開方針","parent_id":"T"});
    let body = "<@OWN> <@OTHER> @everyone どちらにしますか？\na 見本\nb 完成版";
    assert!(
        attachments::post_tracked(&mut h.ctx, "200", body, None, true, Some(&channel)).is_err()
    );
    assert_eq!(h.fake.borrow().posts.len(), 1);
    h.fake.borrow_mut().now += 900.;
    let ids =
        attachments::post_tracked(&mut h.ctx, "200", body, None, true, Some(&channel)).unwrap();
    assert_eq!(ids, vec!["1001"]);
    attachments::post_tracked(&mut h.ctx, "200", body, None, true, Some(&channel)).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(
        h.fake.borrow().posts[0].1["allowed_mentions"],
        json!({"parse":[],"users":["OWN"],"replied_user":false})
    );
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    assert_eq!(h.state()["questions"][0]["question_message"], "1001");
}

#[test]
fn answer_cursor_and_snooze_between_question_chunks_survive() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[]}));
    h.ctx.effects = Box::new(LoseResponse {
        fake: h.fake.clone(),
        home: h.ctx.here.clone(),
        lose: false,
        answer_between_batches: true,
    });
    let channel = json!({"id":"200","name":"公開方針","parent_id":"T"});
    let body = "a".repeat(2005);
    let ids =
        attachments::post_tracked(&mut h.ctx, "200", &body, None, true, Some(&channel)).unwrap();
    assert_eq!(ids.len(), 2);
    assert_eq!(h.state()["questions"][0]["seen_reply"], "1005");
    assert_eq!(h.state()["questions"][0]["remind_not_before"], 2000000000);
    assert_eq!(h.state()["questions"][0]["after"], "1001");
    assert_eq!(
        h.fake.borrow().posts[1].1["allowed_mentions"],
        json!({"parse":[]})
    );
}

#[test]
fn concurrent_first_generations_for_the_same_thread_are_rejected() {
    let h = Harness::new();
    let work = StateWork::begin(&h.ctx).unwrap();
    let mut worker = h.ctx.load().unwrap();
    let mut current = h.ctx.load_fresh().unwrap();
    let thread = json!({"id":"200","name":"公開"});
    register_question(&mut current, &thread, "202");
    atomic_json(&h.ctx.here.join("state.json"), &current, 2).unwrap();
    register_question(&mut worker, &thread, "201");
    assert!(h
        .ctx
        .save(&worker)
        .unwrap_err()
        .starts_with("StateConflict:"));
    drop(work);
    assert_eq!(h.state(), current);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
}

#[test]
fn stale_generation_blocks_multipart_and_actual_job_launcher() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":true}]}));
    let work = StateWork::begin(&h.ctx).unwrap();
    let mut current = h.ctx.load_fresh().unwrap();
    current["questions"][0]["remind_not_before"] = json!(h.ctx.now() + 86400.);
    atomic_json(&h.ctx.here.join("state.json"), &current, 2).unwrap();
    let path = h.root.path().join("fixture.png");
    std::fs::write(&path, b"file").unwrap();
    let file = attachments::File {
        path,
        name: "fixture.png".into(),
        mime: "image/png",
        size: 4,
    };
    let error = attachments::send(
        &mut h.ctx,
        "/channels/200/messages",
        &json!({"content":"old"}),
        &[file],
        10,
    )
    .unwrap_err();
    assert!(error.starts_with("StateConflict:"), "{error}");
    let cwd = h.root.path().to_string_lossy().into_owned();
    let error = launch_job(
        &mut h.ctx,
        &json!({"id":"200","name":"公開"}),
        "作業",
        &json!({"model":"gpt-6-astra","effort":"high","task_type":"implementation"}),
        "stable-key",
        false,
        Some(&cwd),
        None,
    )
    .unwrap_err();
    assert!(error.starts_with("StateConflict:"), "{error}");
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    drop(work);
}

#[test]
fn cmd_post_recovers_untracked_delivery_before_suppressing_answered_retry() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    h.ctx.effects = Box::new(LoseResponse {
        fake: h.fake.clone(),
        home: h.ctx.here.clone(),
        lose: true,
        answer_between_batches: false,
    });
    let path = h.root.path().join("question.md");
    std::fs::write(&path, "どちらにしますか？\na 見本\nb 完成版").unwrap();
    assert!(cc_ask::commands::cmd_post(&mut h.ctx, "200", &path, &[], true).is_err());
    // The successful but response-lost creation is the independent question.
    // Its answer must remain attached there, not to the original work thread.
    h.reply("a", "300");
    h.fake.borrow_mut().now += 900.;
    let result = cc_ask::commands::cmd_post(&mut h.ctx, "200", &path, &[], true).unwrap();
    assert_eq!(result["thread_id"], "300");
    assert_ne!(result["progress_thread_id"], "300");
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(
        h.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads"))
            .count(),
        2
    );
    assert_eq!(
        owner_replies(&mut h.ctx, &json!({"thread_id":"300","after":"300"}), "OWN")
            .unwrap()
            .len(),
        1
    );
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
}

struct ReplyWait {
    fake: SharedFake,
    home: PathBuf,
}
impl Effects for ReplyWait {
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
        if method == "POST" {
            drop(ByteLock::acquire(
                &self.home.join("state.lock"),
                Duration::ZERO,
            )?);
        }
        self.fake.http(method, url, body, discord)
    }
    fn reply_output(&mut self, _: &str) -> Result<()> {
        drop(ByteLock::acquire(
            &self.home.join("state.lock"),
            Duration::ZERO,
        )?);
        Ok(())
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.fake.run(argv, timeout)
    }
}
#[test]
fn reply_receipt_http_and_output_never_hold_state_lock() {
    let mut h = Harness::new();
    h.save(
        &json!({"questions":[{"thread_id":"200","after":"200","open":true,"source":"request"}]}),
    );
    h.reply("a", "200");
    h.ctx.effects = Box::new(ReplyWait {
        fake: SharedFake(h.fake.clone()),
        home: h.ctx.here.clone(),
    });
    cc_ask::commands::cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(h.state()["questions"][0]["seen_reply"], "1001");
}

struct ParkOutput {
    fake: SharedFake,
    home: PathBuf,
    messages: Vec<Value>,
}
impl Effects for ParkOutput {
    fn now(&self) -> f64 {
        self.fake.now()
    }
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        self.fake.http(m, u, b, d)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        self.fake.run(a, t)
    }
    fn reply_output(&mut self, _: &str) -> Result<()> {
        let before = read_json(&self.home.join("state.json"))?;
        assert!(reply_reserved(&before, "1001"));
        let home = self.home.clone();
        let future = self.now() + RESUME_AFTER_S + 121.;
        let messages = self.messages.clone();
        std::thread::spawn(move || {
            let mut worker = Harness::new();
            worker.ctx.here = home;
            worker.fake.borrow_mut().now = future;
            worker
                .fake
                .borrow_mut()
                .messages
                .insert("200".into(), messages);
            cc_ask::resume::cmd_resume(&mut worker.ctx, &mut false).unwrap();
            let state = worker.ctx.load_fresh().unwrap();
            assert!(
                reply_reserved(&state, "1001"),
                "live reader lost its expired claim"
            );
            assert!(reply_unavailable(&state, "1001"));
            assert!(state["answer_receipts"]["1001"]["handled_by"].is_null());
            assert!(worker.fake.borrow().launches.is_empty());
        })
        .join()
        .unwrap();
        let after = read_json(&self.home.join("state.json"))?;
        assert_eq!(
            after["answer_receipts"]["1001"]["read_reservation"],
            before["answer_receipts"]["1001"]["read_reservation"]
        );
        Ok(())
    }
}
#[test]
fn live_output_reader_keeps_expired_reservation_during_parallel_resume() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":true,"source":"posted","cwd":h.root.path()}]}));
    h.reply("a", "200");
    let messages = h.fake.borrow().messages["200"].clone();
    h.ctx.effects = Box::new(ParkOutput {
        fake: SharedFake(h.fake.clone()),
        home: h.ctx.here.clone(),
        messages,
    });
    cc_ask::commands::cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert!(h.state()["answer_receipts"]["1001"]["read_by"].is_string());
    assert!(!reply_reserved(&h.state(), "1001"));
}

#[test]
fn recovering_older_lost_post_preserves_newer_generation_cursor_and_snooze() {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(LoseResponse {
        fake: h.fake.clone(),
        home: h.ctx.here.clone(),
        lose: true,
        answer_between_batches: false,
    });
    let channel = json!({"id":"200","name":"公開方針","parent_id":"T"});
    assert!(
        attachments::post_tracked(&mut h.ctx, "200", "質問A", None, true, Some(&channel)).is_err()
    );
    attachments::post_tracked(&mut h.ctx, "200", "質問B", None, true, Some(&channel)).unwrap();
    let mut state = h.state();
    state["questions"][0]["seen_reply"] = json!("1003");
    state["questions"][0]["remind_not_before"] = json!(2000000000);
    h.save(&state);
    let newer = state["questions"][0].clone();
    h.fake.borrow_mut().now += 900.;
    attachments::post_tracked(&mut h.ctx, "200", "質問A", None, true, Some(&channel)).unwrap();
    attachments::post_tracked(&mut h.ctx, "200", "質問A", None, true, Some(&channel)).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 2);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 2);
    assert_eq!(h.state()["questions"][0]["after"], "1001");
    assert_eq!(h.state()["questions"][0]["open"], false);
    assert_eq!(h.state()["questions"][1], newer);
}

struct SnoozeOutput {
    fake: SharedFake,
    home: PathBuf,
    outputs: Rc<RefCell<usize>>,
}
impl Effects for SnoozeOutput {
    fn now(&self) -> f64 {
        self.fake.now()
    }
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        self.fake.http(m, u, b, d)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        self.fake.run(a, t)
    }
    fn reply_output(&mut self, _: &str) -> Result<()> {
        *self.outputs.borrow_mut() += 1;
        let home = self.home.clone();
        let until = self.now() + 86400.;
        std::thread::spawn(move || {
            let _lock = ByteLock::acquire(&home.join("state.lock"), Duration::ZERO).unwrap();
            let mut state = read_json(&home.join("state.json")).unwrap();
            state["questions"][0]["remind_not_before"] = json!(until);
            state["questions"][0]["unrelated"] = json!("preserve");
            atomic_json(&home.join("state.json"), &state, 2).unwrap();
        })
        .join()
        .unwrap();
        Ok(())
    }
}
#[test]
fn successful_output_is_committed_once_despite_concurrent_snooze() {
    let mut h = Harness::new();
    h.save(
        &json!({"questions":[{"thread_id":"200","after":"200","open":true,"source":"request"}]}),
    );
    h.reply("a", "200");
    let outputs = Rc::new(RefCell::new(0));
    h.ctx.effects = Box::new(SnoozeOutput {
        fake: SharedFake(h.fake.clone()),
        home: h.ctx.here.clone(),
        outputs: outputs.clone(),
    });
    cc_ask::commands::cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert!(h.state()["answer_receipts"]["1001"]["read_by"].is_string());
    assert!(!reply_reserved(&h.state(), "1001"));
    assert_eq!(h.state()["questions"][0]["unrelated"], "preserve");
    assert_eq!(
        h.state()["questions"][0]["remind_not_before"],
        h.ctx.now() + 86400.
    );
    h.fake.borrow_mut().now += 600.;
    let result = cc_ask::commands::cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert!(result[0]["replies"].as_array().unwrap().is_empty());
    assert_eq!(*outputs.borrow(), 1);
}

#[test]
fn record_update_reads_latest_fields_and_excludes_replacement_during_commit() {
    let h = Harness::new();
    let path = h.root.path().join("delivery.json");
    atomic_json(&path, &json!({"parent_update":"latest"}), 2).unwrap();
    update_json_exclusive(&path, |record| {
        assert_eq!(record["parent_update"], "latest");
        #[cfg(windows)]
        {
            assert!(std::fs::write(&path, b"stale writer").is_err());
            let replacement = path.with_extension("next");
            std::fs::write(&replacement, b"{}").unwrap();
            assert!(std::fs::rename(&replacement, &path).is_err());
        }
        record["verification"] = json!(true);
        Ok(())
    })
    .unwrap();
    assert_eq!(
        read_json(&path).unwrap(),
        json!({"parent_update":"latest","verification":true})
    );
}

#[test]
fn partial_record_write_rolls_back_and_interrupted_commit_recovers_complete_json() {
    use std::io::Write;
    let h = Harness::new();
    let path = h.root.path().join("delivery.json");
    let before = json!({"parent_update":"日本語の親情報を保持","nested":{"unknown":123}});
    atomic_json(&path, &before, 2).unwrap();
    let error = update_json_exclusive_with_writer(
        &path,
        |record| {
            record["verification"] = json!(true);
            Ok(())
        },
        |file, bytes| {
            file.write_all(&bytes[..bytes.len() / 2])?;
            Err(std::io::Error::new(
                std::io::ErrorKind::WriteZero,
                "injected partial write",
            ))
        },
    )
    .unwrap_err();
    assert!(error.contains("recovery journal retained"));
    assert_eq!(read_json(&path).unwrap(), before);
    // Simulate process death before rollback; the already flushed journal survives.
    std::fs::write(&path, b"{\"parent_update\":\"\xe6").unwrap();
    recover_json_record(&path).unwrap();
    assert_eq!(read_json(&path).unwrap(), before);
    update_json_exclusive(&path, |record| {
        record["verification"] = json!(true);
        Ok(())
    })
    .unwrap();
    let mut after = before;
    after["verification"] = json!(true);
    assert_eq!(read_json(&path).unwrap(), after);
    assert!(!path
        .with_file_name("delivery.json.todo1818-journal.json")
        .exists());
}

#[test]
fn interrupted_valid_json_mixture_is_recovered_but_newer_parent_update_wins() {
    let h = Harness::new();
    let path = h.root.path().join("delivery.json");
    let before = json!({"parent":"old","counter":1});
    atomic_json(&path, &before, 2).unwrap();
    let before_text = std::fs::read_to_string(&path).unwrap();
    let after = json!({"parent":"old","counter":9,"verification":true});
    let after_text = serde_json::to_string_pretty(&after).unwrap();
    let journal = path.with_file_name("delivery.json.todo1818-journal.json");
    atomic_json(&journal,&json!({"target":path,"before":before,"after":after,"before_text":before_text,"after_text":after_text}),2).unwrap();
    let at = after_text.find('9').unwrap() + 1;
    let mixed = format!("{}{}", &after_text[..at], &before_text[at..]);
    assert!(serde_json::from_str::<Value>(&mixed).is_ok());
    std::fs::write(&path, mixed).unwrap();
    recover_json_record(&path).unwrap();
    assert_eq!(read_json(&path).unwrap(), before);
    atomic_json(&journal,&json!({"target":path,"before":before,"after":after,"before_text":before_text,"after_text":after_text}),2).unwrap();
    let newer = json!({"parent":"newer","counter":1,"other_owner":"keep"});
    atomic_json(&path, &newer, 2).unwrap();
    recover_json_record(&path).unwrap();
    assert_eq!(read_json(&path).unwrap(), newer);
}
