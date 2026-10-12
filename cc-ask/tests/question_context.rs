mod common;
use cc_ask::{ask_gate::OwnerOnly, commands::*, core::*, intake, resume};
use common::{Harness, SharedFake};
use serde_json::{json, Value};
use std::{ffi::OsString, sync::Mutex};

static SESSION_ENV: Mutex<()> = Mutex::new(());
struct RestoreSession(Option<OsString>);
impl Drop for RestoreSession {
    fn drop(&mut self) {
        match &self.0 {
            Some(value) => std::env::set_var("CODEX_THREAD_ID", value),
            None => std::env::remove_var("CODEX_THREAD_ID"),
        }
    }
}

struct PartialAsk {
    shared: SharedFake,
    posts: usize,
    lost_first: bool,
}
impl Effects for PartialAsk {
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
        if method == "POST" && (url.ends_with("/messages") || url.ends_with("/channels/T/threads"))
        {
            self.posts += 1;
            if !self.lost_first && self.posts == 2 {
                return Err("URLError: transport failed".into());
            }
            let response = self.shared.http(method, url, body, discord)?;
            if self.lost_first && self.posts == 1 {
                return Err("URLError: response lost".into());
            }
            return Ok(response);
        }
        self.shared.http(method, url, body, discord)
    }
}

#[test]
fn first_registered_chunk_has_main_routing_even_if_the_second_chunk_fails() {
    let _session = SESSION_ENV.lock().unwrap();
    let mut h = Harness::new();
    h.fake.borrow_mut().messages.get_mut("100").unwrap().clear();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    h.ctx.effects = Box::new(PartialAsk {
        shared: SharedFake(h.fake.clone()),
        posts: 0,
        lost_first: false,
    });
    let body = h.root.path().join("question.txt");
    std::fs::write(
        &body,
        format!("公開してよいですか？\n{}", "根拠".repeat(2500)),
    )
    .unwrap();
    let args = AskArgs {
        thread: Some("200".into()),
        title: "公開方針".into(),
        agent: "claude-main".into(),
        cwd: Some(h.root.path().to_string_lossy().into_owned()),
        body_file: body,
        owner_only: Some(OwnerOnly::Publish),
        ..Default::default()
    };
    assert!(cmd_ask(&mut h.ctx, &args).is_err());
    let q = h.state()["questions"]
        .as_array()
        .unwrap()
        .last()
        .unwrap()
        .clone();
    assert_eq!(q["thread_id"], "300");
    assert_eq!(q["work_thread_id"], "200");
    assert_eq!(q["agent"], "claude-main");
    assert_eq!(q["owner_only"], "publish");
    assert_eq!(q["cwd"], h.root.path().to_string_lossy().as_ref());
    let reply = h.reply("aで公開して", "300");
    resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.state()["answer_receipts"][reply].is_null());
    assert_eq!(h.state()["questions"][0]["agent"], "claude-main");
}

#[test]
fn lost_first_response_recovers_original_context_without_reposting_question() {
    let _session = SESSION_ENV.lock().unwrap();
    let mut h = Harness::new();
    h.fake.borrow_mut().messages.get_mut("100").unwrap().clear();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    h.ctx.effects = Box::new(PartialAsk {
        shared: SharedFake(h.fake.clone()),
        posts: 0,
        lost_first: true,
    });
    let body = h.root.path().join("question.txt");
    std::fs::write(&body, "公開してよいですか？").unwrap();
    let mut args = AskArgs {
        thread: Some("200".into()),
        title: "公開方針".into(),
        agent: "claude-main".into(),
        body_file: body,
        owner_only: Some(OwnerOnly::Publish),
        ..Default::default()
    };
    assert!(cmd_ask(&mut h.ctx, &args).is_err());
    assert!(h.state()["questions"].as_array().unwrap().is_empty());
    let reply = h.reply("aで公開して", "300");
    resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.state()["answer_receipts"][&reply].is_null());
    let cursor = h.root.path().join("inbox.txt");
    std::fs::write(&cursor, "0").unwrap();
    let inbox = cmd_inbox(&mut h.ctx, &cursor).unwrap();
    assert!(inbox.iter().any(|row| row["id"] == reply));
    args.agent = "Codex".into();
    let result = cmd_ask(&mut h.ctx, &args).unwrap();
    assert_eq!(result["thread_id"], "300");
    assert_ne!(result["progress_thread_id"], result["thread_id"]);
    assert_eq!(h.state()["questions"][0]["agent"], "claude-main");
    assert_eq!(h.state()["questions"][0]["owner_only"], "publish");
    assert_eq!(
        h.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST" && cc_ask::core::s(&c["url"]).ends_with("/threads"))
            .count(),
        2
    );
    resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.state()["answer_receipts"][reply].is_null());
}

#[test]
fn new_posting_session_reuses_same_question_without_overwriting_original_context() {
    let _session = SESSION_ENV.lock().unwrap();
    let _restore = RestoreSession(std::env::var_os("CODEX_THREAD_ID"));
    let _restore_job = RestoreJob(std::env::var_os("CC_JOB_ID"));
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    for (job, session) in [
        ("author", "session-author"),
        ("recoverer", "session-recoverer"),
    ] {
        let dir = h.ctx.jobs.join("jobs").join(job);
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(
            dir.join("job.json"),
            json!({
                "status":"succeeded","thread_id":session,
                "created_at":"2026-09-30T00:00:00Z","model":"gpt-6.1-sol",
                "effort":"high","cwd":h.root.path(),"sandbox":"danger-full-access"
            })
            .to_string(),
        )
        .unwrap();
    }
    std::env::set_var("CODEX_THREAD_ID", "session-author");
    std::env::set_var("CC_JOB_ID", "author");
    h.ctx.effects = Box::new(PartialAsk {
        shared: SharedFake(h.fake.clone()),
        posts: 0,
        lost_first: true,
    });
    let body = h.root.path().join("post.txt");
    std::fs::write(&body, "a/bで返信してください").unwrap();
    assert!(cmd_post(&mut h.ctx, "200", &body, &[], false).is_err());
    assert!(h.state()["questions"].as_array().unwrap().is_empty());
    // Recover using the original intent first. The failed response must not
    // produce another question or lose the original author/session.
    let original = cmd_post(&mut h.ctx, "200", &body, &[], false).unwrap();
    let before = h.state()["questions"][0].clone();
    assert_eq!(before["posting_job"], "author");
    assert_eq!(before["session_id"], "session-author");
    std::env::set_var("CODEX_THREAD_ID", "session-recoverer");
    std::env::set_var("CC_JOB_ID", "recoverer");
    let next = cmd_post(&mut h.ctx, "200", &body, &[], false).unwrap();
    assert_eq!(next["thread_id"], original["thread_id"]);
    assert_eq!(next["progress_thread_id"], original["progress_thread_id"]);
    let state = h.state();
    assert_eq!(state["questions"][0], before);
    assert_eq!(state["questions"].as_array().unwrap().len(), 1);
    let retry = cmd_post(&mut h.ctx, "200", &body, &[], false).unwrap();
    assert_eq!(retry["thread_id"], next["thread_id"]);
    assert_eq!(
        h.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/threads"))
            .count(),
        2
    );
}

struct RestoreJob(Option<OsString>);
impl Drop for RestoreJob {
    fn drop(&mut self) {
        match &self.0 {
            Some(value) => std::env::set_var("CC_JOB_ID", value),
            None => std::env::remove_var("CC_JOB_ID"),
        }
    }
}
