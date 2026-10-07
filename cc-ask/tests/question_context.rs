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
        if method == "POST" && url.ends_with("/messages") {
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
    assert_eq!(q["agent"], "claude-main");
    assert_eq!(q["owner_only"], "publish");
    assert_eq!(q["cwd"], h.root.path().to_string_lossy().as_ref());
    let reply = h.reply("aで公開して", "200");
    resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.state()["answer_receipts"][reply].is_null());
    assert_eq!(h.state()["questions"][0]["agent"], "claude-main");
}

#[test]
fn lost_first_response_recovers_original_context_before_suppressing_a_new_callers_retry() {
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
    let reply = h.reply("aで公開して", "200");
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
    assert_eq!(result["suppressed"], "already_answered");
    assert_eq!(h.state()["questions"][0]["agent"], "claude-main");
    assert_eq!(h.state()["questions"][0]["owner_only"], "publish");
    assert_eq!(h.fake.borrow().posts.len(), 1);
    resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.state()["answer_receipts"][reply].is_null());
}

#[test]
fn a_recovery_process_never_becomes_the_original_posting_job_or_session() {
    let _session = SESSION_ENV.lock().unwrap();
    let _restore = RestoreSession(std::env::var_os("CODEX_THREAD_ID"));
    for known_origin in [true, false] {
        let mut h = Harness::new();
        h.fake.borrow_mut().messages.get_mut("100").unwrap().clear();
        h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
        for (job, session) in [
            ("author", "session-author"),
            ("recoverer", "session-recoverer"),
        ] {
            let dir = h.ctx.jobs.join("jobs").join(job);
            std::fs::create_dir_all(&dir).unwrap();
            std::fs::write(
                dir.join("job.json"),
                json!({"status":"succeeded","thread_id":session,
                "created_at":"2026-09-30T00:00:00Z","model":"gpt-6.1-sol","effort":"high",
                "cwd":h.root.path(),"sandbox":"danger-full-access"})
                .to_string(),
            )
            .unwrap();
        }
        if known_origin {
            std::env::set_var("CODEX_THREAD_ID", "session-author");
        } else {
            std::env::remove_var("CODEX_THREAD_ID");
        }
        h.ctx.effects = Box::new(PartialAsk {
            shared: SharedFake(h.fake.clone()),
            posts: 0,
            lost_first: true,
        });
        let body = h.root.path().join("post.txt");
        std::fs::write(&body, "a/bで返信してください").unwrap();
        assert!(cmd_post(&mut h.ctx, "200", &body, &[], false).is_err());
        assert!(h.state()["questions"].as_array().unwrap().is_empty());
        let reply = h.reply("aで進めて", "200");
        std::env::set_var("CODEX_THREAD_ID", "session-recoverer");
        let recovered = cmd_post(&mut h.ctx, "200", &body, &[], false).unwrap();
        assert_eq!(recovered["suppressed"], "already_answered");
        let q = h.state()["questions"][0].clone();
        if known_origin {
            assert_eq!(q["posting_job"], "author");
            assert_eq!(q["session_id"], "session-author");
        } else {
            assert!(q["posting_job"].is_null());
            assert!(q["session_id"].is_null());
        }
        resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
        assert_eq!(h.fake.borrow().launches.len(), usize::from(known_origin));
        if known_origin {
            assert_eq!(
                h.fake.borrow().launches[0]["resume_session"],
                "session-author"
            );
            assert_eq!(h.state()["answer_receipts"][reply]["handled_by"], "job1");
        } else {
            assert_eq!(h.state()["questions"][0]["resume_status"], "再開先なし");
        }
    }
}
