mod common;
use cc_ask::{commands::*, core::*};
use common::Harness;
use common::SharedFake;
use serde_json::json;

// ToDo1857: confirmed Discord title, thread IDs and owner answer IDs. The cwd
// and open flag below replay the state before the second ask; they are not a
// claim about the historical cwd (which was absent from the recovered record).
const GENESIS_TITLE: &str = "【GoDD-Genesis】税・返金・問い合わせの担当と方針";
const GENESIS_FIRST: &str = "1556678685426454568";
fn genesis_args(h: &Harness, cwd: &str) -> AskArgs {
    let args = args(h, GENESIS_TITLE, cwd);
    std::fs::write(
        &args.body_file,
        "税の方針を決めてください。\na 税込\nb 税抜",
    )
    .unwrap();
    args
}

fn genesis_question(h: &Harness, answered: bool) {
    let mut state = h.state();
    state["questions"] = json!([{
        "thread_id":GENESIS_FIRST,"after":GENESIS_FIRST,"title":GENESIS_TITLE,
        "cwd":"D:/CC/GoDD-Genesis","open":true,"agent":"Codex",
        "asked_at":"2026-10-05T23:45:20+09:00","source":"request",
        "seen_reply":"0","last_ping":77,"posting_job":"first-job"
    }]);
    h.save(&state);
    let mut fake = h.fake.borrow_mut();
    fake.sequence = GENESIS_FIRST.parse().unwrap();
    fake.threads.insert(
        GENESIS_FIRST.into(),
        json!({
            "id":GENESIS_FIRST,"type":11,"guild_id":"G","parent_id":"T",
            "owner_id":"BOT","name":GENESIS_TITLE,"thread_metadata":{"archived":false}
        }),
    );
    let mut starter = common::message(
        GENESIS_FIRST,
        "税の方針を決めてください。\na 税込\nb 税抜",
        "BOT",
    );
    starter["author"]["bot"] = json!(true);
    let mut history = vec![starter];
    if answered {
        let mut answer = common::message(
            "1556678932798120018",
            "<@1554269962229649478> \na税込",
            "OWN",
        );
        answer["timestamp"] = json!("2026-10-05T14:46:19.275000+00:00");
        history.push(answer);
    }
    fake.messages.insert(GENESIS_FIRST.into(), history);
}

#[test]
fn genesis_identical_unanswered_question_is_suppressed_without_mutating_assignment() {
    let mut h = Harness::new();
    genesis_question(&h, false);
    let before = h.state();
    let mut a = genesis_args(&h, "D:/CC/GoDD-Genesis");
    a.agent = "Claude".into();
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["thread_id"], GENESIS_FIRST);
    assert_eq!(result["suppressed"], "same_question");
    assert_eq!(creates(&h), 0);
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(h.state(), before);
    assert!(!h.ctx.here.join("question-progress").exists());
}

#[test]
fn genesis_incident_tax_answer_is_returned_without_asking_owner_twice() {
    let mut h = Harness::new();
    genesis_question(&h, true);
    let before = h.state();
    let a = genesis_args(&h, "D:/CC/GoDD-Genesis");
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["thread_id"], GENESIS_FIRST);
    assert_eq!(result["suppressed"], "already_answered");
    assert_eq!(result["latest_owner_reply"]["id"], "1556678932798120018");
    assert!(s(&result["latest_owner_reply"]["content"]).ends_with("a税込"));
    assert_eq!(creates(&h), 0);
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(h.state(), before);
}

#[test]
fn genesis_same_title_in_another_task_folder_remains_a_separate_question() {
    let mut h = Harness::new();
    genesis_question(&h, false);
    let a = genesis_args(&h, "D:/CC/another-task");
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_ne!(result["thread_id"], GENESIS_FIRST);
    assert_eq!(creates(&h), 2);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 2);
    assert!(h
        .fake
        .borrow()
        .posts
        .iter()
        .all(|(tid, _)| tid != GENESIS_FIRST));
}

#[test]
fn genesis_recovered_question_uses_its_recorded_request_job_folder() {
    let mut h = Harness::new();
    genesis_question(&h, false);
    let mut state = h.state();
    // The recovered incident record has no cwd. Its request job does have an
    // explicit working folder; this is evidence, not inference from the title.
    state["questions"][0].as_object_mut().unwrap().remove("cwd");
    let cwd = "C:/Users/makhmeto/Desktop/dev/ADJ";
    state["requests"][GENESIS_FIRST] = json!({"job_info":{"cwd":cwd}});
    h.save(&state);
    let a = genesis_args(&h, cwd);
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["thread_id"], GENESIS_FIRST);
    assert_eq!(result["suppressed"], "same_question");
    assert_eq!(creates(&h), 0);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
}

#[test]
fn genesis_newer_answer_is_returned_instead_of_the_old_tax_answer() {
    let mut h = Harness::new();
    genesis_question(&h, true);
    h.fake
        .borrow_mut()
        .messages
        .get_mut(GENESIS_FIRST)
        .unwrap()
        .push(common::message("1556808758590578721", "b", "OWN"));
    let a = genesis_args(&h, "D:/CC/GoDD-Genesis");
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["latest_owner_reply"]["id"], "1556808758590578721");
    assert_eq!(result["latest_owner_reply"]["content"], "b");
    assert_eq!(creates(&h), 0);
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn genesis_routing_evidence_never_overrides_an_explicit_question_folder() {
    for recorded in [Some("D:/CC/another-task"), None] {
        let mut h = Harness::new();
        genesis_question(&h, false);
        let mut state = h.state();
        if let Some(cwd) = recorded {
            state["questions"][0]["cwd"] = json!(cwd);
            state["requests"][GENESIS_FIRST] = json!({"job_info":{"cwd":"D:/CC/GoDD-Genesis"}});
        } else {
            state["questions"][0].as_object_mut().unwrap().remove("cwd");
            // Same product label with no explicit route is not proof of same work.
        }
        h.save(&state);
        let a = genesis_args(&h, "D:/CC/GoDD-Genesis");
        cmd_ask(&mut h.ctx, &a).unwrap();
        assert_eq!(creates(&h), 2);
        assert!(h
            .fake
            .borrow()
            .posts
            .iter()
            .all(|(tid, _)| tid != GENESIS_FIRST));
    }
}

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
    progress_only: bool,
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
        if method == "POST" && url.ends_with("/channels/T/threads") {
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
            if self.lose_thread && (!self.progress_only || f.messages["T"].len() == 2) {
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
            if self.lose_starter
                && (!self.progress_only || self.shared.0.borrow().messages["T"].len() == 2)
            {
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
fn different_body_with_same_title_creates_isolated_question_and_progress() {
    let mut h = Harness::new();
    let mut a = args(&h, "Steam のサインイン", "D:/CC/game");
    common::decision(&h, &mut a, "original prices");
    a.source = Some("scheduler".into());
    a.scheduler_item = Some(42);
    a.scheduler_job = Some("original".into());
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let before = h.state()["questions"][0].clone();
    let mut b = a.clone();
    common::decision(&h, &mut b, "new prices");
    b.agent = "Claude".into();
    b.scheduler_job = Some("second".into());
    std::fs::write(
        &b.body_file,
        "別の料金を決めてください。\na 月額3000円\nb 月額4000円",
    )
    .unwrap();
    let result = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_ne!(result["thread_id"], first["thread_id"]);
    assert_ne!(result["progress_thread_id"], first["progress_thread_id"]);
    assert_eq!(creates(&h), 4);
    let state = h.state();
    assert_eq!(state["questions"][0], before);
    assert_eq!(state["questions"].as_array().unwrap().len(), 2);
    assert_eq!(state["questions"][1]["agent"], "Claude");
    assert_eq!(state["questions"][1]["scheduler_job"], "second");
    assert_eq!(state["questions"][1]["scheduler_item"], 42);
    assert!(s(&state["questions"][1]["question_body"]).contains("月額3000円"));
    assert!(!s(&state["questions"][1]["question_body"]).contains("月額1000円"));
    let retry = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(retry["thread_id"], result["thread_id"]);
    assert_eq!(creates(&h), 4);
    let f = h.fake.borrow();
    assert!(f.posts.is_empty());
    for tid in [s(&first["thread_id"]), s(&result["thread_id"])] {
        assert!(s(&f.messages[tid][0]["content"]).starts_with("【game】\n<@OWN> "));
        assert_eq!(
            s(&f.messages[tid][0]["content"]).matches("<@OWN>").count(),
            1
        );
        assert_eq!(
            f.messages[tid][0]["allowed_mentions"]["users"],
            json!(["OWN"])
        );
    }
    for tid in [
        s(&first["progress_thread_id"]),
        s(&result["progress_thread_id"]),
    ] {
        assert_eq!(f.messages[tid][0]["flags"], SILENT | 4);
        assert_eq!(f.messages[tid][0]["allowed_mentions"]["users"], json!([]));
        assert!(!s(&f.messages[tid][0]["content"]).contains("<@OWN>"));
    }
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
    assert_eq!(intent_records(&h).len(), 1);
    ready(&h);
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["thread_id"], "300");
    assert_eq!(creates(&h), 2);
    let state = h.state();
    assert_eq!(
        intent_records(&h)[0]["progress_thread_id"],
        result["progress_thread_id"]
    );
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
    assert_eq!(intent_records(&h).len(), 1);
}

#[test]
fn text_starter_and_thread_response_loss_each_recover_without_new_starter() {
    for (lose_starter, progress_only) in
        [(true, false), (false, false), (true, true), (false, true)]
    {
        let mut h = Harness::new();
        let a = args(&h, "Steamのサインイン", "D:/CC/game");
        h.ctx.effects = Box::new(TextLoss {
            shared: SharedFake(h.fake.clone()),
            lose_starter,
            lose_thread: !lose_starter,
            progress_only,
        });
        assert!(cmd_ask(&mut h.ctx, &a).is_err());
        let result = cmd_ask(&mut h.ctx, &a).unwrap();
        assert_eq!(result["thread_id"], "1001");
        assert_eq!(result["progress_thread_id"], "1002");
        let f = h.fake.borrow();
        assert_eq!(
            f.http_calls
                .iter()
                .filter(|r| r["method"] == "POST" && s(&r["url"]).ends_with("/channels/T/messages"))
                .count(),
            2
        );
        assert_eq!(
            f.http_calls
                .iter()
                .filter(|r| r["method"] == "POST" && s(&r["url"]).ends_with("/threads"))
                .count(),
            2
        );
        assert_ne!(result["thread_id"], result["progress_thread_id"]);
        assert_eq!(f.messages["T"].len(), 2);
        assert!(s(&f.messages["T"][1]["content"]).contains("https://discord.com/channels/G/1001"));
        drop(f);
        assert_eq!(intent_records(&h).len(), 1);
        assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
        assert_eq!(h.state()["questions"][0]["posting_complete"], true);
        let retry = cmd_ask(&mut h.ctx, &a).unwrap();
        assert_eq!(retry["thread_id"], result["thread_id"]);
        assert_eq!(retry["progress_thread_id"], result["progress_thread_id"]);
        assert_eq!(creates(&h), 2);
        assert_eq!(h.fake.borrow().messages["T"].len(), 2);
    }
}

#[test]
fn new_question_preserves_original_generation_and_all_delivery_fields() {
    let mut h = Harness::new();
    let mut a = args(&h, "Steamのサインイン", "D:/CC/game");
    common::decision(&h, &mut a, "original scope");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let mut state = h.state();
    let q = &mut state["questions"][0];
    q["session_id"] = json!("original-session");
    q["posting_job"] = json!("original-job");
    q["seen_reply"] = json!("301");
    q["last_ping"] = json!(77);
    q["restart"] = json!({"opaque":true});
    q["continuation"] = json!("kept");
    h.save(&state);
    let mut b = args(&h, "Steamログイン", "D:/CC/game");
    common::decision(&h, &mut b, "publish scope");
    std::fs::write(
        &b.body_file,
        "新しい方針を決めてください。\na 先に公開\nb 準備を続ける",
    )
    .unwrap();
    let next = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_ne!(next["thread_id"], first["thread_id"]);
    assert_ne!(next["progress_thread_id"], first["progress_thread_id"]);
    assert_eq!(h.state()["questions"][0], state["questions"][0]);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 2);
    assert!(h.state()["questions"][1]["continuation"].is_null());
    assert!(h.state()["questions"][1]["restart"].is_null());
    assert_eq!(creates(&h), 4);
}

#[test]
fn retry_preserves_worker_cursor_closed_status_and_assignment() {
    for close in [false, true] {
        let mut h = Harness::new();
        let a = args(&h, "Steamのサインイン", "D:/CC/game");
        let first = cmd_ask(&mut h.ctx, &a).unwrap();
        let mut state = h.state();
        state["questions"][0]["after"] = json!("999");
        state["questions"][0]["open"] = json!(!close);
        state["questions"][0]["seen_reply"] = json!("1000");
        state["questions"][0]["posting_job"] = json!("original");
        h.save(&state);
        let result = cmd_ask(&mut h.ctx, &a).unwrap();
        assert_eq!(result["thread_id"], first["thread_id"]);
        assert_eq!(result["progress_thread_id"], first["progress_thread_id"]);
        assert_eq!(h.state()["questions"], state["questions"]);
        assert_eq!(creates(&h), 2);
    }
}

#[test]
fn explicit_rejection_clears_intent_and_a_fixed_request_can_create_normally() {
    for error in [
        "Discord API POST /channels/T/threads failed: 403 Forbidden",
        "Discord upload failed: 413 Too big",
        "Discord API POST /channels/T/threads failed: 429 Too many requests",
    ] {
        let mut h = Harness::new();
        let a = args(&h, "Steamのサインイン", "D:/CC/game");
        h.ctx.effects = Box::new(RejectOnce {
            shared: SharedFake(h.fake.clone()),
            error: Some(error.into()),
        });
        assert!(cmd_ask(&mut h.ctx, &a).is_err());
        assert_eq!(intent_records(&h).len(), 1);
        assert!(h.state()["questions"].as_array().unwrap().is_empty());
        cmd_ask(&mut h.ctx, &a).unwrap();
        assert_eq!(creates(&h), 2);
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
    let b = args(&h, "Steam のサインイン", "D:/CC/game");
    let result = cmd_ask(&mut h.ctx, &b).unwrap();
    assert_eq!(result["suppressed"], "already_answered");
    assert_eq!(result["latest_owner_reply"]["content"], "a 月額1000円");
    assert_eq!(creates(&h), 2);
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(h.state(), before);
}

#[test]
fn closed_or_presentation_changes_reuse_the_same_explicit_scope() {
    for mode in ["closed", "folder", "title", "unknown"] {
        let mut h = Harness::new();
        let mut a = args(&h, "Steam のサインイン", "D:/CC/game");
        common::decision(&h, &mut a, "unchanged original target and scope");
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
        let mut b = args(
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
        common::decision(&h, &mut b, "unchanged original target and scope");
        cmd_ask(&mut h.ctx, &b).unwrap();
        assert_eq!(
            creates(&h),
            2,
            "{mode}: title and folder changes alone do not prove new scope"
        );
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
        .insert("GET /channels/300/messages?after=300&limit=100".into(), 1);
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(creates(&h), 2);
    assert_eq!(
        h.fake.borrow().http_errors["GET /channels/300/messages?after=300&limit=100"],
        0
    );
}

#[test]
fn dry_run_never_locks_posts_or_changes_state() {
    let mut h = Harness::new();
    // Preview still works without a ledger; it must not implicitly initialize one.
    std::fs::remove_file(h.ctx.here.join("state.json")).unwrap();
    let mut a = args(&h, "Steam のサインイン", "D:/CC/game");
    a.dry_run = true;
    cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(creates(&h), 0);
    assert!(!h.ctx.here.join("ask-create.lock").exists());
    assert!(!h.ctx.here.join("state.json").exists());
}

#[test]
fn simultaneous_callers_create_only_one_question_progress_pair() {
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
            h.ctx.here = here.clone();
            h.ctx.effects = Box::new(common::RouteServer(SharedFake(h.fake.clone()), here));
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
    assert_eq!(results.iter().map(|r| r.0).sum::<usize>(), 2);
    assert_eq!(results[0].1, "301");
    assert!(results.iter().all(|r| r.1 == results[0].1));
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
fn different_truncated_titles_do_not_create_a_new_permission_for_identical_body() {
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
fn new_question_keeps_original_answer_readable_and_isolates_reply_history() {
    let mut h = Harness::new();
    let mut a = args(&h, "Steamのサインイン", "D:/CC/game");
    common::decision(&h, &mut a, "original prices");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let original = h.state()["questions"][0].clone();
    h.reply("a 月額1000円", s(&first["thread_id"]));
    common::decision(&h, &mut a, "new prices");
    std::fs::write(
        &a.body_file,
        "別の料金を決めてください。\na 月額3000円\nb 月額4000円",
    )
    .unwrap();
    common::review_owner_context(&mut h, &a);
    let second = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_ne!(first["thread_id"], second["thread_id"]);
    let state = h.state();
    assert_eq!(state["questions"][0], original);
    assert_eq!(
        owner_replies(&mut h.ctx, &original, "OWN")
            .unwrap()
            .last()
            .unwrap()["content"],
        "a 月額1000円"
    );
    assert!(owner_replies(&mut h.ctx, &state["questions"][1], "OWN")
        .unwrap()
        .is_empty());
    assert_eq!(creates(&h), 4);
}

fn intent_records(h: &Harness) -> Vec<serde_json::Value> {
    std::fs::read_dir(h.ctx.here.join("question-progress"))
        .unwrap()
        .map(|entry| read_json(&entry.unwrap().path()).unwrap())
        .collect()
}
#[test]
fn worker_updates_original_cursor_during_new_creation_without_crossing_questions() {
    for close in [false, true] {
        let mut h = Harness::new();
        let mut a = args(&h, "Steamのサインイン", "D:/CC/game");
        common::decision(&h, &mut a, "original scope");
        cmd_ask(&mut h.ctx, &a).unwrap();
        let original = h.state()["questions"][0].clone();
        h.ctx.effects = Box::new(RegistrationRace {
            shared: SharedFake(h.fake.clone()),
            here: h.ctx.here.clone(),
            close,
        });
        common::decision(&h, &mut a, "new scope");
        std::fs::write(
            &a.body_file,
            "別の方針を決めてください。\na 公開する\nb 続ける",
        )
        .unwrap();
        let next = cmd_ask(&mut h.ctx, &a).unwrap();
        let state = h.state();
        let mut expected = original;
        expected["after"] = json!("999");
        if close {
            expected["open"] = json!(false);
        }
        assert_eq!(state["questions"][0], expected);
        assert_eq!(state["questions"][1]["thread_id"], next["thread_id"]);
        assert_eq!(state["questions"][1]["after"], next["after"]);
        assert_eq!(state["questions"][1]["open"], true);
        assert_ne!(next["thread_id"], "300");
        assert_eq!(creates(&h), 4);
    }
}

#[test]
fn legacy_creation_reader_recovers_without_migrating_or_posting() {
    let mut h = Harness::new();
    genesis_question(&h, false);
    let mut state = h.state();
    let mut original = state["questions"][0].clone();
    original["ask_title"] = json!(GENESIS_TITLE);
    state["questions"] = json!([]);
    state["ask_creations"] = json!({"legacy":{
        "question":original,"first":"税の方針を決めてください。\na 税込\nb 税抜",
        "files":[],"sender":"BOT","parent":"T","guild":"G","before":"200",
        "forum":true,"starter_only":false,"thread_id":GENESIS_FIRST,
        "message_id":GENESIS_FIRST
    }});
    h.save(&state);
    let a = genesis_args(&h, "D:/CC/GoDD-Genesis");
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(result["thread_id"], GENESIS_FIRST);
    assert_eq!(result["suppressed"], "same_question");
    assert_eq!(creates(&h), 0);
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    assert!(h.state()["ask_creations"].as_object().unwrap().is_empty());
    assert!(!h.ctx.here.join("question-progress").exists());
}
