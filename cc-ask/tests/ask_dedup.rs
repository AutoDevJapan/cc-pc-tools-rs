mod common;
use cc_ask::{commands::*, core::*};
use common::Harness;
use common::SharedFake;
use serde_json::json;

struct LostCreation {
    shared: SharedFake,
    lose: bool,
}
impl Effects for LostCreation {
    fn now(&self) -> f64 {
        self.shared.now()
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&serde_json::Value>,
        discord: bool,
    ) -> Result<serde_json::Value> {
        let result = self.shared.http(method, url, body, discord)?;
        if self.lose && method == "POST" && url.ends_with("/channels/T/threads") {
            self.lose = false;
            return Err("fixture: created thread but response lost".into());
        }
        Ok(result)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.shared.run(argv, timeout)
    }
}

struct TextLoss {
    shared: SharedFake,
    lose_starter: bool,
    lose_thread: bool,
}

struct RegistrationRace {
    shared: SharedFake,
    here: std::path::PathBuf,
    close: bool,
}
impl Effects for RegistrationRace {
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
        body: Option<&serde_json::Value>,
        discord: bool,
    ) -> Result<serde_json::Value> {
        let result = self.shared.http(method, url, body, discord)?;
        if method == "POST" && url.ends_with("/channels/300/messages") {
            let path = self.here.join("state.json");
            let mut state = read_json(&path)?;
            state["questions"][0]["after"] = json!("999");
            if self.close {
                state["questions"][0]["open"] = json!(false);
                let mut f = self.shared.0.borrow_mut();
                f.sequence += 1;
                let mid = f.sequence.to_string();
                f.messages.get_mut("300").unwrap().push(common::message(
                    &mid,
                    "a 月額1000円",
                    "OWN",
                ));
            }
            atomic_json(&path, &state, 2)?;
        }
        Ok(result)
    }
}

struct RejectOnce {
    shared: SharedFake,
    error: Option<String>,
}
impl Effects for RejectOnce {
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
        body: Option<&serde_json::Value>,
        discord: bool,
    ) -> Result<serde_json::Value> {
        if method == "POST" && url.ends_with("/channels/T/threads") {
            if let Some(error) = self.error.take() {
                return Err(error);
            }
        }
        self.shared.http(method, url, body, discord)
    }
}
impl Effects for TextLoss {
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
        body: Option<&serde_json::Value>,
        discord: bool,
    ) -> Result<serde_json::Value> {
        let path = url.trim_start_matches("https://discord.com/api/v10");
        if method == "GET" && path == "/channels/T" {
            return Ok(json!({"type":0,"id":"T"}));
        }
        if method == "GET"
            && path.starts_with("/channels/")
            && path.split('/').count() == 3
            && !self
                .shared
                .0
                .borrow()
                .threads
                .contains_key(path.split('/').nth(2).unwrap())
        {
            return Err(format!(
                "Discord API GET {path} failed: 404 Unknown Channel"
            ));
        }
        if method == "POST"
            && path.starts_with("/channels/T/messages/")
            && path.ends_with("/threads")
        {
            let mut f = self.shared.0.borrow_mut();
            f.http_calls
                .push(json!({"method":method,"url":url,"body":body,"discord":discord}));
            let mid = path.split('/').nth(4).unwrap();
            let mut starter = f.messages["T"]
                .iter()
                .find(|m| m["id"] == mid)
                .unwrap()
                .clone();
            let thread = json!({"id":mid,"guild_id":"G","type":11,"parent_id":"T","owner_id":"BOT","name":body.unwrap()["name"],"thread_metadata":{"archived":false}});
            starter["type"] = json!(21);
            starter["content"] = json!("");
            starter["message_reference"] = json!({"message_id":mid,"channel_id":"T"});
            f.threads.insert(mid.into(), thread.clone());
            f.messages.insert(mid.into(), vec![starter]);
            if self.lose_thread {
                self.lose_thread = false;
                return Err("fixture: text thread response lost".into());
            }
            return Ok(thread);
        }
        let result = self.shared.http(method, url, body, discord)?;
        if method == "POST" && path == "/channels/T/messages" {
            self.shared
                .0
                .borrow_mut()
                .messages
                .get_mut("T")
                .unwrap()
                .last_mut()
                .unwrap()["author"]["bot"] = json!(true);
            if self.lose_starter {
                self.lose_starter = false;
                return Err("fixture: text starter response lost".into());
            }
        }
        Ok(result)
    }
}

fn args(h: &Harness, title: &str, cwd: &str) -> AskArgs {
    let body = h.root.path().join("question.md");
    std::fs::write(&body, "料金を決めてください。\na 月額1000円\nb 月額2000円").unwrap();
    AskArgs {
        title: title.into(),
        body_file: body,
        cwd: Some(cwd.into()),
        agent: "Codex".into(),
        ..Default::default()
    }
}

fn ready(h: &Harness) {
    let mut fake = h.fake.borrow_mut();
    fake.threads.get_mut("300").unwrap()["guild_id"] = json!("G");
    fake.messages.get_mut("300").unwrap()[0]["author"]["bot"] = json!(true);
}

fn creates(h: &Harness) -> usize {
    h.fake
        .borrow()
        .http_calls
        .iter()
        .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/threads"))
        .count()
}

#[test]
fn similar_open_question_reuses_thread_preserves_assignment_and_does_not_ping_again() {
    let mut h = Harness::new();
    let mut a = args(&h, "Q12 Steam のサインイン", "D:/CC/game");
    a.source = Some("scheduler".into());
    a.scheduler_item = Some(42);
    a.scheduler_job = Some("original".into());
    cmd_ask(&mut h.ctx, &a).unwrap();
    ready(&h);
    let mut b = args(
        &h,
        "Q13 Steam ログイン",
        if cfg!(windows) {
            "d:\\cc\\game\\.\\"
        } else {
            "D:/CC/game/."
        },
    );
    b.agent = "Claude".into();
    b.scheduler_job = Some("second".into());
    let result = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(result["thread_id"], "300");
    assert_eq!(result["reused"], true);
    assert_eq!(creates(&h), 1);
    let fake = h.fake.borrow();
    assert!(fake.posts.iter().all(|(tid, _)| tid == "300"));
    assert!(fake
        .posts
        .iter()
        .all(|(_, p)| p["allowed_mentions"]["users"].is_null() && p["flags"] == SILENT));
    drop(fake);
    let state = h.state();
    let open: Vec<_> = state["questions"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|q| q["open"] == true)
        .collect();
    assert_eq!(open.len(), 1);
    assert_eq!(open[0]["agent"], "Codex");
    assert_eq!(open[0]["source"], "scheduler");
    assert_eq!(open[0]["scheduler_job"], "original");
    assert_eq!(open[0]["scheduler_item"], 42);
}

#[test]
fn created_thread_with_lost_response_is_recovered_without_second_creation() {
    let mut h = Harness::new();
    let a = args(&h, "Steamのサインイン", "D:/CC/game");
    h.ctx.effects = Box::new(LostCreation {
        shared: SharedFake(h.fake.clone()),
        lose: true,
    });
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(creates(&h), 1);
    assert_eq!(h.state()["ask_creations"].as_object().unwrap().len(), 1);
    ready(&h);
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["thread_id"], "300");
    assert_eq!(creates(&h), 1);
    let state = h.state();
    assert!(state["ask_creations"].as_object().unwrap().is_empty());
    assert_eq!(state["questions"].as_array().unwrap().len(), 1);
}

#[test]
fn unknown_creation_outcome_is_not_treated_as_permission_to_resend() {
    let mut h = Harness::new();
    let a = args(&h, "Steamのサインイン", "D:/CC/game");
    h.ctx.effects = Box::new(LostCreation {
        shared: SharedFake(h.fake.clone()),
        lose: true,
    });
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    h.fake.borrow_mut().threads.remove("300");
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(creates(&h), 1);
    assert_eq!(h.state()["ask_creations"].as_object().unwrap().len(), 1);
}

#[test]
fn text_starter_and_thread_response_loss_each_recover_without_new_starter() {
    for lose_starter in [true, false] {
        let mut h = Harness::new();
        let a = args(&h, "Steamのサインイン", "D:/CC/game");
        h.ctx.effects = Box::new(TextLoss {
            shared: SharedFake(h.fake.clone()),
            lose_starter,
            lose_thread: !lose_starter,
        });
        assert!(cmd_ask(&mut h.ctx, &a).is_err());
        let result = cmd_ask(&mut h.ctx, &a).unwrap();
        assert_eq!(result["thread_id"], "1001");
        let f = h.fake.borrow();
        assert_eq!(
            f.http_calls
                .iter()
                .filter(|r| r["method"] == "POST" && s(&r["url"]).ends_with("/channels/T/messages"))
                .count(),
            1
        );
        assert_eq!(
            f.http_calls
                .iter()
                .filter(|r| r["method"] == "POST" && s(&r["url"]).ends_with("/threads"))
                .count(),
            1
        );
        drop(f);
        assert!(h.state()["ask_creations"].as_object().unwrap().is_empty());
        assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    }
}

#[test]
fn appending_preserves_question_generation_and_all_delivery_fields() {
    let mut h = Harness::new();
    let a = args(&h, "Steamのサインイン", "D:/CC/game");
    cmd_ask(&mut h.ctx, &a).unwrap();
    ready(&h);
    let mut state = h.state();
    let q = &mut state["questions"][0];
    q["session_id"] = json!("original-session");
    q["posting_job"] = json!("original-job");
    q["seen_reply"] = json!("301");
    q["last_ping"] = json!(77);
    q["restart"] = json!({"opaque":true});
    q["continuation"] = json!("kept");
    h.save(&state);
    let b = args(&h, "Steamログイン", "D:/CC/game");
    cmd_ask(&mut h.ctx, &b).unwrap();
    let mut before = state["questions"][0].clone();
    let mut after = h.state()["questions"][0].clone();
    before.as_object_mut().unwrap().remove("message_ids");
    after.as_object_mut().unwrap().remove("message_ids");
    assert_eq!(after, before);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
}

#[test]
fn worker_advancing_cursor_or_closing_during_append_does_not_trap_the_ledger() {
    for close in [false, true] {
        let mut h = Harness::new();
        let a = args(&h, "Steamのサインイン", "D:/CC/game");
        cmd_ask(&mut h.ctx, &a).unwrap();
        ready(&h);
        h.ctx.effects = Box::new(RegistrationRace {
            shared: SharedFake(h.fake.clone()),
            here: h.ctx.here.clone(),
            close,
        });
        let b = args(&h, "Steamログイン", "D:/CC/game");
        let result = cmd_ask(&mut h.ctx, &b).unwrap();
        assert_eq!(result["reused"], true);
        assert_eq!(h.state()["questions"][0]["after"], "999");
        assert_eq!(h.state()["questions"][0]["open"], !close);
        assert_eq!(creates(&h), 1);
        if close {
            assert_eq!(result["suppressed"], "already_answered");
            assert_eq!(result["latest_owner_reply"]["content"], "a 月額1000円");
        } else {
            assert_eq!(result["tracked"], true);
            cmd_ask(&mut h.ctx, &b).unwrap();
            assert_eq!(creates(&h), 1);
        }
    }
}

#[test]
fn explicit_rejection_clears_intent_and_a_fixed_request_can_create_normally() {
    for error in [
        "Discord API POST /channels/T/threads failed: 403 Forbidden",
        "Discord upload failed: 413 Too big",
        "Discord API rate limited",
    ] {
        let mut h = Harness::new();
        let a = args(&h, "Steamのサインイン", "D:/CC/game");
        h.ctx.effects = Box::new(RejectOnce {
            shared: SharedFake(h.fake.clone()),
            error: Some(error.into()),
        });
        assert!(cmd_ask(&mut h.ctx, &a).is_err());
        assert!(h.state()["ask_creations"].as_object().unwrap().is_empty());
        assert!(h.state()["questions"].as_array().unwrap().is_empty());
        cmd_ask(&mut h.ctx, &a).unwrap();
        assert_eq!(creates(&h), 1);
    }
}

#[test]
fn existing_answer_is_returned_without_new_post_or_consuming_it() {
    let mut h = Harness::new();
    let a = args(&h, "Steam のサインイン", "D:/CC/game");
    cmd_ask(&mut h.ctx, &a).unwrap();
    ready(&h);
    h.reply("a 月額1000円", "300");
    let before = h.state();
    let b = args(&h, "Steam ログイン", "D:/CC/game");
    let result = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(result["suppressed"], "already_answered");
    assert_eq!(result["latest_owner_reply"]["content"], "a 月額1000円");
    assert_eq!(creates(&h), 1);
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(h.state(), before);
}

#[test]
fn closed_other_folder_and_different_question_create_their_own_threads() {
    for mode in ["closed", "folder", "title", "unknown"] {
        let mut h = Harness::new();
        let a = args(&h, "Steam のサインイン", "D:/CC/game");
        cmd_ask(&mut h.ctx, &a).unwrap();
        ready(&h);
        let mut state = h.state();
        if mode == "closed" {
            state["questions"][0]["open"] = json!(false);
        }
        if mode == "unknown" {
            state["questions"][0].as_object_mut().unwrap().remove("cwd");
            state["questions"][0]
                .as_object_mut()
                .unwrap()
                .remove("cwd_key");
        }
        h.save(&state);
        let b = args(
            &h,
            if mode == "title" {
                "料金の確認"
            } else {
                "Steam のサインイン"
            },
            if mode == "folder" {
                "D:/CC/other"
            } else {
                "D:/CC/game"
            },
        );
        cmd_ask(&mut h.ctx, &b).unwrap();
        assert_eq!(creates(&h), 2, "{mode}");
    }
}

#[test]
fn history_failure_never_falls_back_to_creating_a_duplicate() {
    let mut h = Harness::new();
    let a = args(&h, "Steam のサインイン", "D:/CC/game");
    cmd_ask(&mut h.ctx, &a).unwrap();
    ready(&h);
    h.fake
        .borrow_mut()
        .http_errors
        .insert("GET /channels/300/messages?limit=100".into(), 1);
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(creates(&h), 1);
}

#[test]
fn dry_run_never_locks_posts_or_changes_state() {
    let mut h = Harness::new();
    let mut a = args(&h, "Steam のサインイン", "D:/CC/game");
    a.dry_run = true;
    cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(creates(&h), 0);
    assert!(!h.ctx.here.join("ask-create.lock").exists());
    assert!(!h.ctx.here.join("state.json").exists());
}

#[test]
fn simultaneous_callers_create_only_one_thread() {
    use std::sync::{Arc, Barrier};
    let shared = Harness::new();
    let barrier = Arc::new(Barrier::new(3));
    let workers: Vec<_> = (0..2).map(|_| {
        let barrier = barrier.clone();
        let here = shared.ctx.here.clone();
        std::thread::spawn(move || {
            // Build non-Send effects inside each thread; only the real state and
            // native lock paths are shared between these independent callers.
            let mut h = Harness::new();
            h.ctx.here = here;
            let a = args(&h, "Steamのサインイン", "D:/CC/game");
            {
                let mut f = h.fake.borrow_mut();
                f.threads.insert("300".into(), json!({"id":"300","guild_id":"G","type":11,"parent_id":"T","name":"Steam","thread_metadata":{"archived":false}}));
                f.messages.insert("300".into(), vec![]);
            }
            barrier.wait();
            let result = cmd_ask(&mut h.ctx, &a).unwrap();
            (creates(&h), result["thread_id"].clone())
        })
    }).collect();
    barrier.wait();
    let results: Vec<_> = workers.into_iter().map(|w| w.join().unwrap()).collect();
    assert_eq!(results.iter().map(|r| r.0).sum::<usize>(), 1);
    assert!(results.iter().all(|r| r.1 == "300"));
    assert_eq!(
        shared.state()["questions"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|q| q["open"] == true)
            .count(),
        1
    );
}

#[test]
fn long_titles_with_distinct_prices_are_not_merged_after_display_truncation() {
    let mut h = Harness::new();
    let prefix = "Steamサインイン後の長い製品名と問い合わせと税と返金の月額料金を";
    let a = args(&h, &format!("{prefix}1000円にする"), "D:/CC/game");
    cmd_ask(&mut h.ctx, &a).unwrap();
    ready(&h);
    let b = args(&h, &format!("{prefix}2000円にする"), "D:/CC/game");
    cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(creates(&h), 2);
}

#[test]
fn answer_racing_append_remains_readable_from_original_cursor() {
    let mut h = Harness::new();
    let a = args(&h, "Steamのサインイン", "D:/CC/game");
    cmd_ask(&mut h.ctx, &a).unwrap();
    ready(&h);
    // The second history GET is inside post_text, after initial answer lookup.
    h.fake.borrow_mut().history_injection = Some((2, "300".into(), "a 月額1000円".into()));
    let b = args(&h, "Steamログイン", "D:/CC/game");
    std::fs::write(
        &b.body_file,
        "料金を決めてください。\na 月額1000円\nb 月額2000円\n追加説明です。",
    )
    .unwrap();
    cmd_ask(&mut h.ctx, &b).unwrap();
    let state = h.state();
    let q = state["questions"]
        .as_array()
        .unwrap()
        .iter()
        .rev()
        .find(|q| q["open"] == true)
        .unwrap();
    assert_eq!(q["after"], "300");
    assert_eq!(
        owner_replies(&mut h.ctx, q, "OWN").unwrap().last().unwrap()["content"],
        "a 月額1000円"
    );
    assert_eq!(creates(&h), 1);
}
