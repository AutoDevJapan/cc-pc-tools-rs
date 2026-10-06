mod common;
use cc_ask::{
    commands::*,
    core::*,
    questions::{self, RecoverArgs},
};
use common::*;
use serde_json::{json, Value};

fn args(apply: bool) -> RecoverArgs {
    RecoverArgs {
        thread: vec!["200".into()],
        message: None,
        apply,
        dry_run: !apply,
        limit: 1,
    }
}

fn setup(text: &str) -> Harness {
    let h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![message("201", text, "BOT")]);
    h
}

#[test]
fn notified_question_first_line_is_tracked_but_reports_and_quoted_questions_are_not() {
    for (text, expected) in [
        ("公開してよいですか？\na 公開する\nb 準備を続ける", 1),
        ("検査が終わりました。", 0),
        ("確認結果：\na. 料金一致\nb. 期間一致",0),
        ("状態: 要判断\na. 予算1000円で公開\nb. 公開を停止",1),
        ("判断をお願いしたいです！🙇\na. 予算1000円で公開\nb. 公開を停止",1),
        ("Stripeにはログインしていただいたので、料金を確認できました。", 0),
        ("前にログインをお願いしていた作業の進捗です。", 0),
        ("【GoDD-Genesis】このPCのChromeで、Stripeにログインをお願いしたいです！\nログインしたら、このスレッドに一言ください。", 1),
        ("【健康チャンネル】試作動画ができました。\n前に送った「投稿してよいか・公開範囲」の質問への返事を待っています。", 0),
        ("状態: 完了\n公開してよいですか？", 0),
        (
            "ログの報告です。\n> 公開してよいですか？\n```\na 公開する\nb 準備を続ける\n```",
            0,
        ),
    ] {
        let mut h = setup("");
        let path = h.root.path().join("post.txt");
        std::fs::write(&path, text).unwrap();
        let out = cmd_post(&mut h.ctx, "200", &path, &[], true).unwrap();
        assert_eq!(
            h.state()["questions"].as_array().unwrap().len(),
            expected,
            "{text}"
        );
        if expected == 1 {
            assert_eq!(
                h.state()["questions"][0]["question_message"],
                out["message_ids"][0]
            );
            let before = h.state();
            let result = questions::recover(&mut h.ctx, &args(true)).unwrap();
            assert_eq!(result["registered"], 0);
            assert_eq!(h.state(), before);
        }
    }
}

#[test]
fn plain_first_line_and_existing_post_ids_can_be_recovered_without_sending() {
    let mut h = setup("どちらを公開しますか？\na 見本のみ\nb 完成版");
    h.save(&json!({"questions":[{"thread_id":"200","after":"200","seen_reply":"200","open":false,"cwd":"work","project":"製品A"}],"future":{"keep":true}}));
    let original = h.state();
    let bytes = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let preview = questions::recover(&mut h.ctx, &args(false)).unwrap();
    assert_eq!(preview["missing"], 1);
    assert_eq!(preview["registered"], 0);
    assert_eq!(std::fs::read(h.ctx.here.join("state.json")).unwrap(), bytes);
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .all(|c| c["method"] == "GET"));
    let mut selected = args(true);
    selected.message = Some("201".into());
    let result = questions::recover(&mut h.ctx, &selected).unwrap();
    assert_eq!(result["registered"], 1);
    assert_eq!(h.state()["questions"][1]["after"], "201");
    assert_eq!(h.state()["questions"][1]["cwd"], "work");
    assert_eq!(h.state()["future"], original["future"]);
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.fake.borrow().patches.is_empty());
    assert!(h.fake.borrow().runs.is_empty());
    assert_eq!(
        questions::recover(&mut h.ctx, &selected).unwrap()["registered"],
        0
    );
}

#[test]
fn historical_answered_completed_closed_and_stopped_questions_are_never_reopened() {
    for (state, later, expected) in [
        (
            json!({"questions":[]}),
            Some(message("202", "aで進めて", "OWN")),
            "answered",
        ),
        (
            json!({"questions":[]}),
            Some(message("202", "終わりました。公開を反映しました。", "BOT")),
            "completed",
        ),
        (
            json!({"questions":[{"thread_id":"200","after":"201","open":false}]}),
            None,
            "tracked_or_closed",
        ),
        (
            json!({"questions":[],"requests":{"200":{"owner_stopped":true}}}),
            None,
            "stopped",
        ),
        (
            json!({"questions":[{"thread_id":"200","after":"200","open":false,"closed_at":1790773200.}]}),
            None,
            "tracked_or_closed",
        ),
    ] {
        let mut h = setup("判断をお願いしたいです！🙇\n公開してよいですか？\na 公開\nb 続ける");
        if let Some(later) = later {
            h.fake
                .borrow_mut()
                .messages
                .get_mut("200")
                .unwrap()
                .push(later);
        }
        h.save(&state);
        let result = questions::recover(&mut h.ctx, &args(true)).unwrap();
        assert_eq!(result["threads"][0]["status"], expected);
        assert_eq!(result["registered"], 0);
        assert_eq!(h.state(), state);
    }
}

#[test]
fn request_results_keep_request_routing_and_registration_retries_keep_closed_state() {
    let mut h = setup("状態: 要判断\nどちらにしますか？\na 公開\nb 続ける");
    h.save(&json!({"questions":[],"requests":{"200":{"result_after":"201","after":"200"}}}));
    assert_eq!(
        questions::recover(&mut h.ctx, &args(true)).unwrap()["registered"],
        1
    );
    assert_eq!(h.state()["questions"][0]["source"], "request");
    let thread = h.fake.borrow().threads["200"].clone();
    let mut state = h.state();
    state["questions"][0]["open"] = json!(false);
    let before = state.clone();
    register_question(&mut state, &thread, "201");
    assert_eq!(state, before);
}

#[test]
fn recovery_checks_all_reply_pages_and_rejects_foreign_authors_guilds_and_ids() {
    let mut h = setup("公開してよいですか？");
    for id in 202..310 {
        h.fake
            .borrow_mut()
            .messages
            .get_mut("200")
            .unwrap()
            .push(message(&id.to_string(), "進捗を記録します。", "BOT"));
    }
    h.fake
        .borrow_mut()
        .messages
        .get_mut("200")
        .unwrap()
        .push(message("310", "公開して", "OWN"));
    let out = questions::recover(&mut h.ctx, &args(true)).unwrap();
    assert_eq!(out["threads"][0]["status"], "answered");
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![message("201", "公開してよいですか？", "OTHER")],
    );
    assert_eq!(
        questions::recover(&mut h.ctx, &args(true)).unwrap()["threads"][0]["status"],
        "no_concrete_question"
    );
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("FOREIGN");
    assert!(questions::recover(&mut h.ctx, &args(true)).is_err());
    let mut invalid = args(false);
    invalid.thread = vec!["../../state".into()];
    assert!(questions::recover(&mut h.ctx, &invalid).is_err());
    assert!(h.fake.borrow().posts.is_empty());
}

struct FailSecond {
    fake: std::rc::Rc<std::cell::RefCell<Fake>>,
    posts: usize,
}
impl Effects for FailSecond {
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method == "POST" {
            self.posts += 1;
            if self.posts == 2 {
                return Err("lost upload".into());
            }
        }
        self.fake.borrow_mut().http(method, url, body, discord)
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        panic!("no process expected")
    }
}

#[test]
fn failed_second_post_chunk_leaves_first_question_registered() {
    let mut h = setup("");
    h.ctx.effects = Box::new(FailSecond {
        fake: h.fake.clone(),
        posts: 0,
    });
    let path = h.root.path().join("post.txt");
    std::fs::write(
        &path,
        format!("公開してよいですか？\n{}", "本文".repeat(1400)),
    )
    .unwrap();
    assert!(cmd_post(&mut h.ctx, "200", &path, &[], true).is_err());
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    assert_eq!(h.state()["questions"][0]["after"], "1001");
    assert_eq!(h.state()["questions"][0]["message_ids"], json!(["1001"]));
}

#[test]
fn publication_wait_report_recovers_only_an_existing_question_with_a_matching_pending_todo() {
    let report = "【健康チャンネル】試作動画ができました。\n前に送った「投稿してよいか・公開範囲」の質問への返事を待っています。";
    for (todo_status, have_original, expected) in
        [("block", true, 1), ("done", true, 0), ("block", false, 0)]
    {
        let mut h = setup(report);
        h.fake.borrow_mut().messages.insert(
            "200".into(),
            vec![
                message("200", "公開してよいですか？\na 限定公開\nb 保管する", "BOT"),
                message("201", "声を作り直して", "OWN"),
                message("202", report, "BOT"),
            ],
        );
        h.save(&json!({"questions":if have_original { vec![json!({"thread_id":"200","after":"200","seen_reply":"201","title":"健康動画の公開範囲","open":false,"cwd":"work"})] } else {vec![]}}));
        std::fs::write(&h.ctx.todo, json!({"items":[{"status":todo_status,"note":"質問200の公開範囲の回答を待ちます","project":"ADJ"}]}).to_string()).unwrap();
        let preview = questions::recover(&mut h.ctx, &args(false)).unwrap();
        assert_eq!(preview["missing"], expected);
        let out = questions::recover(&mut h.ctx, &args(true)).unwrap();
        assert_eq!(out["registered"], expected);
        if expected == 1 {
            assert_eq!(h.state()["questions"][1]["original_question"], "200");
            assert_eq!(h.state()["questions"][1]["after"], "202");
            assert_eq!(
                h.state()["questions"][1]["message_ids"],
                json!(["200", "202"])
            );
            assert_eq!(
                questions::recover(&mut h.ctx, &args(true)).unwrap()["registered"],
                0
            );
        }
        assert!(h.fake.borrow().posts.is_empty());
    }
}

#[test]
fn manual_close_and_manual_cancel_win_over_stale_wait_todo_and_reply_race_is_received() {
    let mut h = setup("公開してよいですか？");
    std::fs::write(&h.ctx.todo,json!({"items":[{"status":"block","note":"質問200の公開範囲の回答を待ちます","project":"ADJ"}]}).to_string()).unwrap();
    h.save(&json!({"questions":[{"thread_id":"200","after":"200","open":false,"title":"公開範囲","closed_at":h.ctx.now()}]}));
    assert_eq!(
        questions::recover(&mut h.ctx, &args(true)).unwrap()["registered"],
        0
    );
    std::fs::create_dir_all(h.ctx.jobs.join("jobs/stopped-job")).unwrap();
    std::fs::write(
        h.ctx.jobs.join("jobs/stopped-job/job.json"),
        json!({"status":"cancelled","cancel_requested":true,"cancel_reason":"manual"}).to_string(),
    )
    .unwrap();
    h.save(&json!({"questions":[],"requests":{"200":{"job":"stopped-job"}}}));
    assert_eq!(
        questions::recover(&mut h.ctx, &args(true)).unwrap()["threads"][0]["status"],
        "stopped"
    );
    let mut h = setup("公開してよいですか？");
    h.fake.borrow_mut().history_injection = Some((2, "200".into(), "aで進めて".into()));
    assert_eq!(
        questions::recover(&mut h.ctx, &args(true)).unwrap()["registered"],
        1
    );
    assert_eq!(h.state()["questions"][0]["open"], false);
    assert_eq!(h.state()["questions"][0]["seen_reply"], "1001");
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn retry_of_a_known_post_generation_keeps_it_closed() {
    let h = setup("公開してよいですか？");
    let channel = h.fake.borrow().threads["200"].clone();
    let mut state = json!({"questions":[{"thread_id":"200","after":"201","question_message":"201","message_ids":["201"],"open":false}]});
    let before = state.clone();
    questions::register_posted(&mut state, &channel, "201", &["201".into()]);
    assert_eq!(state, before);
}
struct CompletionRace {
    fake: std::rc::Rc<std::cell::RefCell<Fake>>,
    reads: usize,
}
impl Effects for CompletionRace {
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method == "GET" && url.contains("/channels/200/messages") {
            self.reads += 1;
            if self.reads == 2 {
                let mut done = message("1001", "終わりました。公開範囲を反映しました。", "BOT");
                done["author"]["bot"] = json!(true);
                self.fake
                    .borrow_mut()
                    .messages
                    .get_mut("200")
                    .unwrap()
                    .push(done);
            }
        }
        self.fake.borrow_mut().http(method, url, body, discord)
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        panic!("completed question cannot launch work")
    }
}
#[test]
fn completion_received_during_registration_is_closed_before_commit_and_cannot_resume() {
    let mut h = setup("公開してよいですか？\na 限定公開\nb 保管する");
    h.ctx.effects = Box::new(CompletionRace {
        fake: h.fake.clone(),
        reads: 0,
    });
    questions::recover(&mut h.ctx, &args(true)).unwrap();
    assert_eq!(h.state()["questions"][0]["open"], false);
    assert_eq!(
        h.state()["questions"][0]["completed_before_registration"],
        true
    );
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.fake.borrow().runs.is_empty());
}

struct CompleteDuringRead {
    fake: std::rc::Rc<std::cell::RefCell<Fake>>,
    histories: usize,
}
impl Effects for CompleteDuringRead {
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
        if method == "GET" && url.contains("/messages?limit=100") {
            self.histories += 1;
            if self.histories == 2 {
                let mut completed = message("1001", "終わりました。公開を反映しました。", "BOT");
                completed["author"]["bot"] = json!(true);
                self.fake
                    .borrow_mut()
                    .messages
                    .get_mut("200")
                    .unwrap()
                    .push(completed);
            }
        }
        self.fake.borrow_mut().http(method, url, body, discord)
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        panic!("no process expected")
    }
}

#[test]
fn completion_arriving_during_registration_never_leaves_an_open_question() {
    let mut h = setup("公開してよいですか？");
    h.ctx.effects = Box::new(CompleteDuringRead {
        fake: h.fake.clone(),
        histories: 0,
    });
    let out = questions::recover(&mut h.ctx, &args(true)).unwrap();
    assert_eq!(out["registered"], 1);
    assert_eq!(h.state()["questions"][0]["open"], false);
    assert_eq!(
        h.state()["questions"][0]["completed_before_registration"],
        true
    );
    assert!(h.fake.borrow().posts.is_empty());
}
