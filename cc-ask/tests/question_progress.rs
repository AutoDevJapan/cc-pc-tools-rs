mod common;
use cc_ask::{commands::*, core::*, question_progress::*};
use common::{Harness, SharedFake};
use serde_json::{json, Value};

fn args(h: &Harness, body: &str) -> AskArgs {
    let path = h.root.path().join("question.txt");
    std::fs::write(&path, body).unwrap();
    let mut args = AskArgs {
        title: "【ProductA】料金を決めてください".into(),
        body_file: path,
        cwd: Some("D:/ProductA".into()),
        thread: Some("100".into()),
        agent: "Codex".into(),
        ..Default::default()
    };
    common::decision(h, &mut args, body);
    args
}
fn thread_creates(h: &Harness) -> Vec<Value> {
    h.fake
        .borrow()
        .http_calls
        .iter()
        .filter(|v| v["method"] == "POST" && s(&v["url"]).ends_with("/channels/T/threads"))
        .cloned()
        .collect()
}
#[test]
fn each_new_question_gets_one_mention_and_a_separate_silent_bridge() {
    let mut h = Harness::new();
    let a = args(&h, "月額料金を決めてください。\na 1000円\nb 2000円");
    let first = cmd_ask(&mut h.ctx, &a).unwrap();
    let a = args(&h, "別の料金を決めてください。\na 3000円\nb 4000円");
    let second = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_ne!(first["thread_id"], second["thread_id"]);
    assert_ne!(first["progress_thread_id"], second["progress_thread_id"]);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 2);
    let creates = thread_creates(&h);
    assert_eq!(creates.len(), 4);
    let payload = |i: usize| &creates[i]["body"]["message"];
    for i in [0, 2] {
        assert_eq!(payload(i)["allowed_mentions"]["users"], json!(["OWN"]));
        assert_eq!(s(&payload(i)["content"]).matches("<@OWN>").count(), 1);
    }
    for i in [1, 3] {
        assert_eq!(payload(i)["flags"], SILENT | SUPPRESS_EMBEDS);
        assert_eq!(
            payload(i)["allowed_mentions"],
            json!({"parse":[],"users":[],"replied_user":false})
        );
        assert!(!s(&payload(i)["content"]).contains("<@"));
        assert!(s(&payload(i)["content"]).contains("<https://discord.com/channels/G/100>"));
    }
    assert!(h.fake.borrow().posts.is_empty());
}
#[test]
fn retry_after_state_save_failure_does_not_repeat_the_question_or_mention() {
    let mut h = Harness::new();
    let a = args(&h, "月額料金を決めてください。\na 1000円\nb 2000円");
    // state save fails after a real successful fake POST; creation ledger survives.
    std::fs::create_dir(h.root.path().join("state.json")).unwrap();
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    // Initial legacy recovery reads state before POST, so remove this obstruction
    // and inject the failure precisely after posting via Effects below.
    std::fs::remove_dir(h.root.path().join("state.json")).unwrap();
    h.ctx.effects = Box::new(FailAfterCreate {
        inner: SharedFake(h.fake.clone()),
        root: h.root.path().to_path_buf(),
        failure: Failure::State,
        done: false,
    });
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(thread_creates(&h).len(), 1);
    std::fs::remove_dir(h.root.path().join("state.json")).unwrap();
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(thread_creates(&h).len(), 2);
    assert_ne!(result["thread_id"], result["progress_thread_id"]);
    cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(thread_creates(&h).len(), 2);
}
#[test]
fn lost_forum_response_is_recovered_without_another_post() {
    let mut h = Harness::new();
    let a = args(&h, "月額料金を決めてください。\na 1000円\nb 2000円");
    h.ctx.effects = Box::new(FailAfterCreate {
        inner: SharedFake(h.fake.clone()),
        root: h.root.path().to_path_buf(),
        failure: Failure::Response,
        done: false,
    });
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    assert_eq!(thread_creates(&h).len(), 1);
    cmd_ask(&mut h.ctx, &a).unwrap();
    assert_eq!(thread_creates(&h).len(), 2);
}
#[test]
fn receipt_and_post_progress_use_silent_destination_and_original_reply_identity() {
    let mut h = Harness::new();
    let a = args(&h, "月額料金を決めてください。\na 1000円\nb 2000円");
    let result = cmd_ask(&mut h.ctx, &a).unwrap();
    let question = s(&result["thread_id"]);
    let progress = s(&result["progress_thread_id"]);
    let reply = h.reply("a", question);
    let answer = common::message(&reply, "a", "OWN");
    let mut state = h.state();
    cc_ask::steering::acknowledge(&mut h.ctx, &mut state, question, "ProductA", &[answer]).unwrap();
    assert_eq!(h.state()["answer_receipts"][&reply]["thread"], question);
    assert_eq!(h.fake.borrow().posts.last().unwrap().0, progress);
    assert!(h.fake.borrow().posts.last().unwrap().1["message_reference"].is_null());
    let body = h.root.path().join("progress.txt");
    std::fs::write(&body, "進めています。<@OWN> @everyone https://example.com").unwrap();
    cmd_post(&mut h.ctx, question, &body, &[], true).unwrap();
    let fake = h.fake.borrow();
    let (target, payload) = fake.posts.last().unwrap();
    assert_eq!(target, progress);
    assert_eq!(payload["flags"], SILENT | SUPPRESS_EMBEDS);
    assert!(!s(&payload["content"]).contains("<@"));
    assert!(!s(&payload["content"]).contains("@everyone"));
    assert!(s(&payload["content"]).contains("<https://example.com>"));
}
#[test]
fn reminders_do_not_mention_new_questions_again_and_legacy_is_not_migrated() {
    let mut h = Harness::new();
    let a = args(&h, "月額料金を決めてください。\na 1000円\nb 2000円");
    cmd_ask(&mut h.ctx, &a).unwrap();
    let mut state = h.state();
    state["questions"][0]["asked_at"] = json!("2020-01-01T00:00:00Z");
    h.save(&state);
    cc_ask::reminders::run(&mut h.ctx, &Default::default()).unwrap();
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(result_target(&mut h.ctx, "200", "").unwrap(), "200");
    assert_eq!(thread_creates(&h).len(), 2);
}

#[test]
fn question_only_worker_sidecar_has_empty_request_ids_and_preserves_existing_ownership() {
    let mut h = Harness::new();
    let dir = h.ctx.jobs.join("jobs/worker");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"worker","status":"running"}),
        2,
    )
    .unwrap();
    let context = json!({"title":"料金判断","cwd":"D:/ProductA","posting_job":"worker",
        "decision_identity":cc_ask::decision_identity::normalize(&h.ctx,&json!({"todo_id":1,"resources":[{"kind":"service","id":"ProductA"}],"decision":"money","scope":{"price":1000}})).unwrap()});
    let first = ask(
        &mut h.ctx,
        &context,
        "",
        "料金を決めてください。\na 1000円\nb 2000円",
        &[],
    )
    .unwrap();
    let path = dir.join("discord-threads.json");
    let mut sidecar = read_json(&path).unwrap();
    assert_eq!(sidecar["thread_ids"], json!([]));
    assert_eq!(sidecar["question_thread_ids"], json!([first["thread_id"]]));
    sidecar["thread_ids"] = json!(["100"]);
    sidecar["source"] = json!("declared");
    sidecar["opaque"] = json!({"kept":true});
    atomic_json(&path, &sidecar, 2).unwrap();
    let mut context = context;
    context["decision_identity"]=cc_ask::decision_identity::normalize(&h.ctx,&json!({"todo_id":1,"resources":[{"kind":"service","id":"ProductB"}],"decision":"money","scope":{"price":3000}})).unwrap();
    let result = ask(
        &mut h.ctx,
        &context,
        "",
        "別製品の料金を決めてください。\na 3000円\nb 4000円",
        &[],
    )
    .unwrap();
    let next = read_json(&path).unwrap();
    assert_eq!(next["thread_ids"], json!(["100"]));
    assert_eq!(next["opaque"], json!({"kept":true}));
    assert!(next["question_thread_ids"]
        .as_array()
        .unwrap()
        .contains(&result["thread_id"]));
}
enum Failure {
    State,
    Response,
}

#[test]
fn periodic_recovery_finishes_a_saved_question_without_asking_again() {
    let mut h = Harness::new();
    let a = args(&h, "月額料金を決めてください。\na 1000円\nb 2000円");
    h.ctx.effects = Box::new(FailAfterCreate {
        inner: SharedFake(h.fake.clone()),
        root: h.root.path().to_path_buf(),
        failure: Failure::State,
        done: false,
    });
    assert!(cmd_ask(&mut h.ctx, &a).is_err());
    std::fs::remove_dir(h.root.path().join("state.json")).unwrap();
    recover_pending(&mut h.ctx).unwrap();
    assert_eq!(thread_creates(&h).len(), 2);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    let question = h.state()["questions"][0].clone();
    assert!(!s(&question["progress_thread_id"]).is_empty());
    recover_pending(&mut h.ctx).unwrap();
    assert_eq!(thread_creates(&h).len(), 2);
}
struct FailAfterCreate {
    inner: SharedFake,
    root: std::path::PathBuf,
    failure: Failure,
    done: bool,
}
impl Effects for FailAfterCreate {
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        let value = self.inner.http(m, u, b, d)?;
        if !self.done && m == "POST" && u.ends_with("/channels/T/threads") {
            self.done = true;
            match self.failure {
                Failure::State => {
                    std::fs::create_dir(self.root.join("state.json")).unwrap();
                }
                Failure::Response => return Err("lost response".into()),
            }
        }
        Ok(value)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        self.inner.run(a, t)
    }
}
