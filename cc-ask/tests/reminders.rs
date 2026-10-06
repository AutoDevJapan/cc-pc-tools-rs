mod common;
use cc_ask::{
    core::*,
    reminders::{self, RemindArgs},
};
use common::*;
use serde_json::{json, Value};

fn setup(count: usize, one_project: bool) -> Harness {
    let h = Harness::new();
    let mut qs = vec![];
    for i in 0..count {
        let tid = (300 + i).to_string();
        let project = if one_project {
            "製品A".to_owned()
        } else {
            format!("製品{i:03}")
        };
        h.fake.borrow_mut().threads.insert(tid.clone(), json!({"id":tid,"type":11,"guild_id":"G","parent_id":"T","owner_id":"BOT","name":"公開の判断"}));
        h.fake.borrow_mut().messages.insert(tid.clone(), vec![]);
        qs.push(json!({"thread_id":tid,"after":"200","title":"公開の判断","project":project,"open":true}));
    }
    h.save(&json!({"questions":qs,"future":{"keep":true}}));
    h
}

#[test]
fn backlog_of_182_items_is_capped_across_repeated_ticks_for_an_hour() {
    let mut h = setup(182, false);
    for _ in 0..10 {
        let before = h.fake.borrow().posts.len();
        reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap();
        assert!(h.fake.borrow().posts.len() - before <= 3);
    }
    assert_eq!(h.fake.borrow().posts.len(), 5);
    assert_eq!(
        h.state()["questions"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|q| q["last_ping"].is_number())
            .count(),
        5
    );
    h.fake.borrow_mut().now += 3599.;
    reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 5);
    h.fake.borrow_mut().now += 1.;
    reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 8);
    assert!(
        h.state()["questions"][5]["last_ping"].is_number(),
        "older unnotified items must not starve behind hourly repeats"
    );
}

#[test]
fn same_project_is_grouped_and_dry_run_and_small_limit_are_read_only_or_bounded() {
    let mut h = setup(3, true);
    let state = h.state();
    let preview = reminders::run(
        &mut h.ctx,
        &RemindArgs {
            dry_run: true,
            limit: 1,
            thread: vec![],
        },
    )
    .unwrap();
    assert_eq!(preview["due_questions"], 3);
    assert_eq!(preview["selected_groups"].as_array().unwrap().len(), 1);
    assert_eq!(h.state(), state);
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .all(|c| c["method"] == "GET"));
    let out = reminders::run(
        &mut h.ctx,
        &RemindArgs {
            dry_run: false,
            limit: 1,
            thread: vec![],
        },
    )
    .unwrap();
    assert_eq!(out["sent"], 1);
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert!(h.text().contains("/300"));
    assert!(h.text().contains("/301"));
    assert!(h.text().contains("/302"));
    assert!(h.state()["questions"]
        .as_array()
        .unwrap()
        .iter()
        .all(|q| q["last_ping"] == h.ctx.now()));
    reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert!(reminders::run(
        &mut h.ctx,
        &RemindArgs {
            limit: 182,
            ..Default::default()
        }
    )
    .is_err());
}

#[test]
fn reply_arriving_after_planning_prevents_a_reminder_and_terminal_state_is_skipped() {
    let mut h = setup(1, false);
    h.fake.borrow_mut().history_injection = Some((3, "300".into(), "aで進めて".into()));
    assert_eq!(
        reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap()["sent"],
        0
    );
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.state()["reminder_outbox"].as_object().unwrap().is_empty());
    let mut h = setup(3, false);
    let mut state = h.state();
    state["questions"][0]["seen_reply"] = json!("201");
    state["questions"][1]["continuation"] = json!({"close_pending":true});
    state["requests"] = json!({"302":{"owner_stopped":true}});
    h.save(&state);
    reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap();
    assert!(h.fake.borrow().posts.is_empty());
}

struct LoseOnce {
    fake: std::rc::Rc<std::cell::RefCell<Fake>>,
    lose: bool,
}
impl Effects for LoseOnce {
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
        let response = self.fake.borrow_mut().http(method, url, body, discord)?;
        if method == "POST" && self.lose {
            self.lose = false;
            return Err("response lost after successful POST".into());
        }
        Ok(response)
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        panic!("no process expected")
    }
}

#[test]
fn lost_response_is_recovered_from_history_without_a_duplicate_even_after_nonce_window() {
    let mut h = setup(1, false);
    h.ctx.effects = Box::new(LoseOnce {
        fake: h.fake.clone(),
        lose: true,
    });
    assert!(reminders::run(&mut h.ctx, &RemindArgs::default()).is_err());
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert!(h.state()["questions"][0]["last_ping"].is_null());
    assert_eq!(h.state()["reminder_outbox"].as_object().unwrap().len(), 1);
    h.fake.borrow_mut().now += 900.;
    reminders::run(
        &mut h.ctx,
        &RemindArgs {
            limit: 1,
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(h.state()["questions"][0]["last_ping"], h.ctx.now());
    assert!(h.state()["reminder_outbox"].as_object().unwrap().is_empty());
    assert_eq!(h.state()["reminder_sent"].as_array().unwrap().len(), 1);
}

#[test]
fn failed_post_keeps_reservation_and_retry_sends_exactly_once() {
    let mut h = setup(1, false);
    h.fake.borrow_mut().post_error = true;
    assert!(reminders::run(&mut h.ctx, &RemindArgs::default()).is_err());
    let state = h.state();
    assert_eq!(state["reminder_outbox"].as_object().unwrap().len(), 1);
    assert!(state["questions"][0]["last_ping"].is_null());
    reminders::run(
        &mut h.ctx,
        &RemindArgs {
            limit: 1,
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    let post = &h.fake.borrow().posts[0].1;
    assert_eq!(post["enforce_nonce"], true);
    assert_eq!(
        post["allowed_mentions"],
        json!({"parse":[],"users":["OWN"]})
    );
    assert_eq!(h.state()["future"], json!({"keep":true}));
}

#[test]
fn deployment_preserves_legacy_hourly_budget_and_corrupt_outbox_is_not_replaced() {
    let mut h = setup(8, false);
    let mut state = h.state();
    for index in 0..5 {
        state["questions"][index]["last_ping"] = json!(h.ctx.now() - 10.);
    }
    h.save(&state);
    assert_eq!(
        reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap()["sent"],
        0
    );
    assert!(h.fake.borrow().posts.is_empty());
    state["reminder_outbox"] = json!(["broken"]);
    h.save(&state);
    assert!(reminders::run(&mut h.ctx, &RemindArgs::default()).is_err());
    assert_eq!(h.state(), state);
}

#[test]
fn replacing_a_question_generation_preserves_its_threads_hourly_interval() {
    let mut h = setup(1, false);
    let mut state = h.state();
    let mut previous = state["questions"][0].clone();
    previous["open"] = json!(false);
    previous["last_ping"] = json!(h.ctx.now() - 3599.);
    let mut recovered = state["questions"][0].clone();
    recovered["after"] = json!("201");
    recovered["original_question"] = json!("200");
    state["questions"] = json!([previous, recovered]);
    h.save(&state);
    assert_eq!(
        reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap()["sent"],
        0
    );
    assert!(h.fake.borrow().posts.is_empty());
    h.fake.borrow_mut().now += 1.;
    assert_eq!(
        reminders::run(&mut h.ctx, &RemindArgs::default()).unwrap()["sent"],
        1
    );
    assert_eq!(h.fake.borrow().posts.len(), 1);
}
