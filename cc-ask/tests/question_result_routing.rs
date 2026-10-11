mod common;
use cc_ask::{
    core::*,
    question_progress, questions,
    result_delivery::{self, Delivery},
};
use common::*;
use serde_json::{json, Value};

fn separated(h: &Harness) {
    let route = json!({"guild":"G","parent":"T","question_thread_id":"100",
        "work_thread_id":"50","progress_thread_id":"500"});
    let dir = h.ctx.here.join("question-progress");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("route.json"), &route, 2).unwrap();
    h.fake.borrow_mut().threads.insert("500".into(),
        json!({"id":"500","type":11,"parent_id":"T","owner_id":"BOT","name":"進捗","thread_metadata":{"archived":false}}));
    h.fake.borrow_mut().messages.insert("500".into(), vec![]);
    std::fs::create_dir_all(h.ctx.jobs.join("jobs/job1")).unwrap();
}

#[test]
fn separated_result_is_silent_and_ledger_generation_stays_on_origin() {
    let mut h = Harness::new();
    separated(&h);
    let result = result_delivery::post_with_cursor(
        &mut h.ctx,
        "100",
        "状態: 完了\n修正しました。<@OWN>",
        true,
        "job1",
        Some("100"),
    )
    .unwrap();
    assert!(matches!(result, Delivery::Sent(Some(_))));
    let f = h.fake.borrow();
    assert_eq!(f.posts.len(), 1);
    assert_eq!(f.posts[0].0, "500");
    assert_eq!(f.posts[0].1["flags"].as_u64().unwrap() & SILENT, SILENT);
    assert_eq!(f.posts[0].1["allowed_mentions"]["users"], json!([]));
    assert_ne!(
        n(&f.posts[0].1["flags"]) & question_progress::SUPPRESS_EMBEDS,
        0
    );
    assert!(!s(&f.posts[0].1["content"]).contains("<@"));
    drop(f);
    let ledger = read_json(&result_delivery::ledger_path(&h.ctx, "100", "job1").unwrap()).unwrap();
    assert_eq!(ledger["thread_id"], "100");
    assert_eq!(ledger["destination_thread_id"], "500");
    assert_eq!(
        questions::result_generation(&h.ctx, "100", "job1").unwrap()["first"],
        ledger["first"]
    );
    result_delivery::post(
        &mut h.ctx,
        "100",
        "状態: 完了\n修正しました。",
        true,
        "job1",
    )
    .unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
}

#[test]
fn origin_reply_blocks_result_even_when_progress_history_is_empty() {
    let mut h = Harness::new();
    separated(&h);
    h.reply("先に修正してほしい点があります", "100");
    let result = result_delivery::post_with_cursor(
        &mut h.ctx,
        "100",
        "状態: 完了\n修正しました。",
        true,
        "job1",
        Some("100"),
    )
    .unwrap();
    assert!(matches!(result, Delivery::OwnerMessagesPending));
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn progress_reply_does_not_replace_the_origin_cursor() {
    let mut h = Harness::new();
    separated(&h);
    h.reply("別の進捗への返信", "500");
    let result = result_delivery::post_with_cursor(
        &mut h.ctx,
        "100",
        "状態: 完了\n修正しました。",
        true,
        "job1",
        Some("100"),
    )
    .unwrap();
    assert!(matches!(result, Delivery::Sent(Some(_))));
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(h.fake.borrow().posts[0].0, "500");
}

#[test]
fn attachment_error_uses_the_silent_route_and_origin_ledger() {
    let mut h = Harness::new();
    separated(&h);
    let result = result_delivery::post(
        &mut h.ctx,
        "100",
        "状態: 完了\n添付ファイル: relative.png",
        true,
        "job1",
    )
    .unwrap();
    assert!(matches!(result, Delivery::PreparationFailed(_)));
    let f = h.fake.borrow();
    assert_eq!(f.posts.len(), 1);
    assert_eq!(f.posts[0].0, "500");
    assert_eq!(f.posts[0].1["flags"].as_u64().unwrap() & SILENT, SILENT);
    assert_eq!(f.posts[0].1["allowed_mentions"]["users"], json!([]));
    assert_ne!(
        n(&f.posts[0].1["flags"]) & question_progress::SUPPRESS_EMBEDS,
        0
    );
    assert!(!s(&f.posts[0].1["content"]).contains("<@"));
    drop(f);
    let path = result_delivery::ledger_path(&h.ctx, "100", "job1").unwrap();
    assert_eq!(read_json(&path).unwrap()["thread_id"], "100");
}

#[test]
fn legacy_result_stays_in_place_without_creating_questions_or_routes() {
    let mut h = Harness::new();
    std::fs::create_dir_all(h.ctx.jobs.join("jobs/job1")).unwrap();
    result_delivery::post(
        &mut h.ctx,
        "100",
        "状態: 完了\n修正しました。",
        true,
        "job1",
    )
    .unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(h.fake.borrow().posts[0].0, "100");
    assert!(!h.ctx.here.join("question-progress").exists());
    assert!(h.state()["questions"].as_array().is_none_or(Vec::is_empty));
}

#[test]
fn retry_keeps_persisted_destination_when_the_work_route_changes() {
    let mut h = Harness::new();
    separated(&h);
    h.fake
        .borrow_mut()
        .http_errors
        .insert("POST /channels/500/messages".into(), 1);
    assert!(result_delivery::post(
        &mut h.ctx,
        "100",
        "状態: 完了\n修正しました。",
        true,
        "job1"
    )
    .is_err());
    let route_path = h.ctx.here.join("question-progress/route.json");
    let mut route = read_json(&route_path).unwrap();
    route["progress_thread_id"] = json!("600");
    atomic_json(&route_path, &route, 2).unwrap();
    let result = result_delivery::post(
        &mut h.ctx,
        "100",
        "状態: 完了\n修正しました。",
        true,
        "job1",
    )
    .unwrap();
    assert!(matches!(result, Delivery::Sent(Some(_))));
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(h.fake.borrow().posts[0].0, "500");
    assert_eq!(
        read_json(&result_delivery::ledger_path(&h.ctx, "100", "job1").unwrap()).unwrap()
            ["destination_thread_id"],
        "500"
    );
}

/// Unlike the legacy fixture, forum creations need distinct IDs for each question
/// and its progress thread. Record both starter payloads to assert mentions.
struct ForumEffects(std::rc::Rc<std::cell::RefCell<Fake>>);
impl Effects for ForumEffects {
    fn now(&self) -> f64 {
        self.0.borrow().now
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.0.borrow_mut().run(argv, timeout)
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method == "POST" && url.ends_with("/channels/T/threads") {
            let mut f = self.0.borrow_mut();
            f.sequence += 1;
            let tid = f.sequence.to_string();
            let body = body.unwrap();
            if s(&body["name"]).ends_with(" 進捗") && f.post_error {
                f.post_error = false;
                return Err("mock progress creation connection lost".into());
            }
            let thread = json!({"id":tid,"guild_id":"G","type":11,"parent_id":"T","owner_id":"BOT",
                "name":body["name"],"thread_metadata":{"archived":false}});
            let mut starter = message(&tid, s(&body["message"]["content"]), "BOT");
            starter["author"]["bot"] = json!(true);
            f.threads.insert(tid.clone(), thread.clone());
            f.messages.insert(tid.clone(), vec![starter]);
            f.posts.push((tid, body["message"].clone()));
            return Ok(thread);
        }
        self.0.borrow_mut().http(method, url, body, discord)
    }
}

#[test]
fn concrete_intake_result_registers_new_question_once_and_keeps_work_context() {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(ForumEffects(h.fake.clone()));
    h.tick();
    let mut state = h.state();
    state["requests"]["100"]["todo_id"] = json!(2345);
    state["requests"]["100"]["context_marker"] = json!("same-work");
    h.save(&state);
    h.fake.borrow_mut().posts.clear();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n公開してよいですか？\na 公開する\nb まだ公開しない"),
        None,
    );
    h.tick();
    let state = h.state();
    let questions = state["questions"].as_array().unwrap();
    assert_eq!(questions.len(), 1);
    let q = &questions[0];
    assert_ne!(q["thread_id"], "100");
    assert_eq!(q["work_thread_id"], "100");
    assert_eq!(q["context_marker"], "same-work");
    assert_eq!(q["todo_id"], 2345);
    assert!(!truth(
        &h.fake.borrow().threads["100"]["thread_metadata"]["archived"]
    ));
    let qid = s(&q["thread_id"]).to_owned();
    let pid = s(&q["progress_thread_id"]).to_owned();
    assert_ne!(qid, pid);
    assert_eq!(
        question_progress::result_target(&mut h.ctx, "100", "job1").unwrap(),
        pid
    );
    let f = h.fake.borrow();
    assert_eq!(
        f.posts
            .iter()
            .filter(|(_, p)| p["allowed_mentions"]["users"] == json!(["OWN"]))
            .count(),
        1
    );
    assert!(f.posts.iter().all(|(tid, _)| tid != "100"));
    assert!(f
        .posts
        .iter()
        .filter(|(tid, _)| tid == &pid)
        .all(|(_, p)| p["allowed_mentions"]["users"] == json!([])
            && n(&p["flags"]) & SILENT == SILENT));
    assert!(f
        .posts
        .iter()
        .any(|(tid, p)| tid == &pid && s(&p["content"]).contains(&qid)));
    let count = f.posts.len();
    drop(f);
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), count);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
}

#[test]
fn resumed_question_closes_origin_generation_without_replacing_its_cursor() {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(ForumEffects(h.fake.clone()));
    h.tick();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n公開してよいですか？\na 公開する\nb まだ公開しない"),
        None,
    );
    let mut state = h.state();
    let mut origin = json!({"thread_id":"100","after":"90","question_message":"90",
        "open":true,"title":"元の質問","source":"posted","context_marker":"same-work",
        "cwd":h.root.path().to_string_lossy(),"session_id":"original-session",
        "continuation":{"job":"job1","after":"100","reported":false,
            "job_info":job_info(&h.ctx,"job1")}});
    state["questions"] = json!([origin]);
    h.save(&state);
    h.fake.borrow_mut().posts.clear();
    cc_ask::resume::resume_question(&mut h.ctx, &mut state, &mut origin, "OWN", &mut false)
        .unwrap();
    let state = h.state();
    assert_eq!(state["questions"].as_array().unwrap().len(), 2);
    assert_eq!(state["questions"][0]["thread_id"], "100");
    assert_eq!(state["questions"][0]["after"], "90");
    assert_eq!(state["questions"][0]["question_message"], "90");
    assert_eq!(state["questions"][0]["open"], false);
    assert_eq!(state["questions"][1]["open"], true);
    assert_eq!(state["questions"][1]["context_marker"], "same-work");
    assert_ne!(state["questions"][1]["thread_id"], "100");
    assert!(h
        .fake
        .borrow()
        .patches
        .iter()
        .any(|(tid, p)| tid == "100" && p["archived"] == true));
    assert_eq!(
        h.fake
            .borrow()
            .posts
            .iter()
            .filter(|(_, p)| p["allowed_mentions"]["users"] == json!(["OWN"]))
            .count(),
        1
    );
}

#[test]
fn answered_separated_question_closes_while_its_todo_continues() {
    let mut h = Harness::new();
    separated(&h);
    h.tick();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 継続中\n検査を続けます。"),
        None,
    );
    let mut state = h.state();
    let mut question = json!({"thread_id":"100","after":"90","question_message":"90","open":true,
        "title":"料金の判断","source":"posted","separated_question":true,
        "cwd":h.root.path().to_string_lossy(),"session_id":"original-session",
        "continuation":{"job":"job1","after":"100","reported":false,"job_info":job_info(&h.ctx,"job1")}});
    state["questions"] = json!([question]);
    h.save(&state);
    cc_ask::resume::resume_question(&mut h.ctx, &mut state, &mut question, "OWN", &mut false)
        .unwrap();
    assert!(!truth(&h.state()["questions"][0]["open"]));
    assert!(truth(&h.state()["questions"][0]["progress_job"]));
    assert!(h
        .fake
        .borrow()
        .patches
        .iter()
        .any(|(tid, p)| tid == "100" && p["archived"] == true));
    assert!(h
        .fake
        .borrow()
        .posts
        .iter()
        .all(|(tid, p)| tid == "500" && p["allowed_mentions"]["users"] == json!([])));
}

#[test]
fn progress_creation_failure_keeps_the_registered_question_for_recovery() {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(ForumEffects(h.fake.clone()));
    h.tick();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n公開してよいですか？\na 公開する\nb まだ公開しない"),
        None,
    );
    h.fake.borrow_mut().posts.clear();
    h.fake.borrow_mut().post_error = true;
    // Intake logs a per-thread error and checkpoints the outer state. That must
    // retain the question installed before its progress bridge failed.
    h.tick();
    let state = h.state();
    assert_eq!(state["questions"].as_array().unwrap().len(), 1);
    assert_ne!(state["questions"][0]["thread_id"], "100");
    assert_eq!(state["questions"][0]["open"], true);
    assert_eq!(state["requests"]["100"]["job"], "job1");
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(
        h.fake.borrow().posts[0].1["allowed_mentions"]["users"],
        json!(["OWN"])
    );
    let ledger = read_json(&result_delivery::ledger_path(&h.ctx, "100", "job1").unwrap()).unwrap();
    assert!(!truth(&ledger["complete"]));
}
