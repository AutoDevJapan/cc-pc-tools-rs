mod common;
use cc_ask::{
    commands::{cmd_replies, cmd_replies_read_only},
    core::*,
};
use common::{message, Harness, SharedFake};
use serde_json::{json, Value};
use std::{
    path::PathBuf,
    time::{Duration, Instant},
};

struct UnlockedRead {
    fake: SharedFake,
    home: PathBuf,
}

struct ConcurrentWriter {
    fake: SharedFake,
    home: PathBuf,
    replacement: Option<Value>,
}
impl Effects for ConcurrentWriter {
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
        if method == "GET" {
            if let Some(replacement) = self.replacement.take() {
                let _writer = ByteLock::acquire(&self.home.join("state.lock"), Duration::ZERO)?;
                atomic_json(&self.home.join("state.json"), &replacement, 2)?;
            }
        }
        self.fake.http(method, url, body, discord)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.fake.run(argv, timeout)
    }
}

fn open_question() -> Value {
    json!({"thread_id":"200","after":"200","asked_at":"generation-1","source":"request","open":true})
}

fn concurrent(h: &mut Harness, replacement: Value) {
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![message("201", "回答", "OWN")]);
    h.ctx.effects = Box::new(ConcurrentWriter {
        fake: SharedFake(h.fake.clone()),
        home: h.ctx.here.clone(),
        replacement: Some(replacement),
    });
}

#[test]
fn commit_preserves_concurrent_requests_unknown_fields_and_newer_ack() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[open_question()]}));
    let mut latest = h.state();
    latest["questions"][0]["seen_reply"] = json!("205");
    latest["questions"][0]["continuation"] = json!({"pending_owner_reply":"204","job":"keep"});
    latest["questions"][0]["remind_not_before"] = json!(h.ctx.now() + 86400.);
    latest["requests"] = json!({"100":{"job":"other","pending":"keep"}});
    latest["future"] = json!({"integer":9007199254740993u64});
    concurrent(&mut h, latest.clone());
    cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert_eq!(h.state(), latest);
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn commit_merges_into_current_state_instead_of_overwriting_writer() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[open_question()]}));
    let mut latest = h.state();
    latest["requests"] = json!({"100":{"pending":"keep"}});
    latest["future"] = json!([1, 2, 3]);
    concurrent(&mut h, latest.clone());
    cmd_replies(&mut h.ctx, Some("200")).unwrap();
    let final_state = h.state();
    assert_eq!(final_state["requests"], latest["requests"]);
    assert_eq!(final_state["future"], latest["future"]);
    assert_eq!(final_state["questions"][0]["seen_reply"], "201");
    assert_eq!(h.fake.borrow().posts.len(), 1);
    cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1, "receipt was duplicated");
}

#[test]
fn closed_or_replaced_generation_cannot_be_acknowledged() {
    for replace in [false, true] {
        let mut h = Harness::new();
        h.save(&json!({"questions":[open_question()]}));
        let mut latest = h.state();
        if replace {
            latest["questions"][0]["asked_at"] = json!("generation-2");
        } else {
            latest["questions"][0]["open"] = json!(false);
        }
        concurrent(&mut h, latest.clone());
        let result = cmd_replies(&mut h.ctx, Some("200"));
        assert_eq!(result.is_err(), replace);
        assert_eq!(h.state(), latest);
        assert!(h.fake.borrow().posts.is_empty());
    }
}

#[test]
fn read_only_inspection_bypasses_live_lock_and_preserves_pending_answer_bytes() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[open_question()],"answer_receipts":{"201":{"posted":false}},"requests":{"200":{"pending":"201"}}}));
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![
            message("201", "未反映の回答", "OWN"),
            message("202", "他人", "OTHER"),
        ],
    );
    let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let _held = ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).unwrap();
    let out = cmd_replies_read_only(&mut h.ctx, "200").unwrap();
    assert_eq!(out[0]["read_only"], true);
    assert_eq!(out[0]["question_open"], true);
    assert_eq!(out[0]["replies"].as_array().unwrap().len(), 1);
    assert_eq!(out[0]["replies"][0]["id"], "201");
    assert_eq!(
        std::fs::read(h.ctx.here.join("state.json")).unwrap(),
        before
    );
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .all(|c| c["method"] == "GET"));
    assert!(h.fake.borrow().runs.is_empty());
    assert!(ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).is_err());
}

#[test]
fn read_only_handles_request_threads_without_open_questions() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[],"requests":{"100":{"after":"100"}}}));
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let out = cmd_replies_read_only(&mut h.ctx, "100").unwrap();
    assert_eq!(out[0]["question_open"], Value::Null);
    assert_eq!(out[0]["replies"][0]["id"], "100");
    assert_eq!(
        std::fs::read(h.ctx.here.join("state.json")).unwrap(),
        before
    );
}

#[test]
fn read_only_rejects_untrusted_threads_and_invalid_id_before_history() {
    for registered_legacy in [false, true] {
        let mut h = Harness::new();
        h.save(&json!({"questions":if registered_legacy { vec![json!({"thread_id":"200","source":"routing","open":true})] } else { vec![] }}));
        let mut fake = h.fake.borrow_mut();
        let thread = fake.threads.get_mut("200").unwrap();
        thread["guild_id"] = json!("G");
        thread["parent_id"] = json!("personal");
        drop(fake);
        assert!(cmd_replies_read_only(&mut h.ctx, "200").is_err());
        assert_eq!(h.fake.borrow().http_calls.len(), 1);
        assert!(cmd_replies_read_only(&mut h.ctx, "200/messages").is_err());
        assert_eq!(h.fake.borrow().http_calls.len(), 1);
    }
}

#[test]
fn lock_timeout_never_steals_writer_or_changes_pending_state() {
    let h = Harness::new();
    h.save(&json!({"questions":[open_question()],"requests":{"pending":true}}));
    let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let _held = ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).unwrap();
    let started = Instant::now();
    assert_eq!(
        ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::from_millis(50))
            .err()
            .unwrap(),
        "state.lock is busy"
    );
    assert!(started.elapsed() >= Duration::from_millis(50));
    assert!(started.elapsed() < Duration::from_secs(1));
    assert!(ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).is_err());
    assert_eq!(
        std::fs::read(h.ctx.here.join("state.json")).unwrap(),
        before
    );
}

#[test]
fn server_retry_delays_are_finite_and_bounded() {
    assert_eq!(
        discord_retry_delay(&json!({"retry_after":60})).unwrap(),
        Duration::from_secs(60)
    );
    assert_eq!(
        discord_retry_delay(&json!({})).unwrap(),
        Duration::from_secs(1)
    );
    for seconds in [-1., 60.01, 1e100] {
        assert!(discord_retry_delay(&json!({"retry_after":seconds})).is_err());
    }
}

#[test]
fn dns_timeout_releases_lock_keeps_state_and_cannot_send_late_post() {
    use std::sync::{atomic::AtomicBool, Arc};
    let h = Harness::new();
    h.save(&json!({"questions":[open_question()],"answer_receipts":{"201":{"posted":false}}}));
    let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    listener.set_nonblocking(true).unwrap();
    let address = listener.local_addr().unwrap();
    let slot = Arc::new(AtomicBool::new(false));
    let resolver_slot = slot.clone();
    let (release, wait) = std::sync::mpsc::sync_channel(1);
    let wait = Arc::new(std::sync::Mutex::new(Some(wait)));
    let (finished, completed) = std::sync::mpsc::sync_channel(1);
    let agent = ureq::AgentBuilder::new()
        .try_proxy_from_env(false)
        .resolver(move |_: &str| {
            let wait = wait.lock().unwrap().take().unwrap();
            let finished = finished.clone();
            resolve_with_deadline(
                Duration::from_millis(50),
                resolver_slot.clone(),
                move || {
                    wait.recv().unwrap();
                    finished.send(()).unwrap();
                    Ok(vec![address])
                },
            )
        })
        .timeout(Duration::from_secs(1))
        .build();
    let started = Instant::now();
    {
        let _held = ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).unwrap();
        assert!(agent
            .post("http://fixture.invalid/receipt")
            .send_string("fixture only")
            .is_err());
    }
    assert!(started.elapsed() < Duration::from_millis(500));
    let _available = ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).unwrap();
    let error = resolve_with_deadline(Duration::from_millis(50), slot, || {
        panic!("second DNS worker started")
    })
    .unwrap_err();
    assert_eq!(error.kind(), std::io::ErrorKind::WouldBlock);
    release.send(()).unwrap();
    completed.recv_timeout(Duration::from_secs(1)).unwrap();
    assert_eq!(
        listener.accept().unwrap_err().kind(),
        std::io::ErrorKind::WouldBlock
    );
    assert_eq!(
        std::fs::read(h.ctx.here.join("state.json")).unwrap(),
        before
    );
}

#[test]
fn bounded_dns_returns_addresses_and_releases_worker_slot() {
    use std::sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    };
    let slot = Arc::new(AtomicBool::new(false));
    let result = resolve_with_deadline(Duration::from_secs(1), slot.clone(), || {
        Ok(vec!["127.0.0.1:80".parse().unwrap()])
    })
    .unwrap();
    assert_eq!(
        result,
        vec!["127.0.0.1:80".parse::<std::net::SocketAddr>().unwrap()]
    );
    assert!(!slot.load(Ordering::Acquire));
}
impl Effects for UnlockedRead {
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method == "GET" {
            let _writer = ByteLock::acquire(&self.home.join("state.lock"), Duration::ZERO)
                .map_err(|_| "reply history read held the mutation lock")?;
        }
        self.fake.http(method, url, body, discord)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.fake.run(argv, timeout)
    }
}

#[test]
fn reply_history_does_not_hold_mutation_lock() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":true}]}));
    h.ctx.effects = Box::new(UnlockedRead {
        fake: SharedFake(h.fake.clone()),
        home: h.ctx.here.clone(),
    });
    cmd_replies(&mut h.ctx, Some("200")).unwrap();
}

#[test]
fn empty_reply_check_bypasses_live_writer_without_rewriting_state() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[],"requests":{"100":{"pending":"keep"}},"unknown":[1,2]}));
    let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let held = ByteLock::acquire(&h.ctx.here.join("state.lock"), Duration::ZERO).unwrap();
    let release = std::thread::spawn(move || {
        std::thread::sleep(Duration::from_millis(800));
        drop(held);
    });
    let started = Instant::now();
    let out = cmd_replies(&mut h.ctx, Some("200")).unwrap();
    let elapsed = started.elapsed();
    release.join().unwrap();
    assert_eq!(out, json!([]));
    assert!(
        elapsed < Duration::from_millis(400),
        "empty read waited {elapsed:?} for a writer"
    );
    assert_eq!(
        std::fs::read(h.ctx.here.join("state.json")).unwrap(),
        before
    );
}
