mod common;
use cc_ask::{
    core::*,
    result_delivery::{self, Delivery},
};
use common::*;
use serde_json::{json, Value};
use std::{cell::RefCell, rc::Rc};

struct LosePostResponse {
    fake: Rc<RefCell<Fake>>,
    lose: bool,
}
impl Effects for LosePostResponse {
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
            return Err("POST response was lost".into());
        }
        Ok(response)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.fake.borrow_mut().run(argv, timeout)
    }
}

#[test]
fn plain_result_survives_lost_post_response_and_repeated_delivery_without_duplicates() {
    let mut h = Harness::new();
    let job = "text-result";
    std::fs::create_dir_all(h.ctx.jobs.join("jobs").join(job)).unwrap();
    h.ctx.effects = Box::new(LosePostResponse {
        fake: h.fake.clone(),
        lose: true,
    });
    let text = "状態: 完了\n公開準備を終えました。";
    assert!(result_delivery::post(&mut h.ctx, "200", text, true, job).is_err());
    assert_eq!(h.fake.borrow().posts.len(), 1);
    h.fake.borrow_mut().now += 900.;
    let after = match result_delivery::post(&mut h.ctx, "200", text, true, job).unwrap() {
        Delivery::Sent(Some(id)) => id,
        _ => panic!("delivery must recover the previously posted message"),
    };
    assert_eq!(after, "1001");
    assert_eq!(h.fake.borrow().posts.len(), 1);
    let ledger = read_json(&h.ctx.jobs.join("jobs/text-result/attachment-delivery.json")).unwrap();
    assert_eq!(ledger["complete"], true);
    assert_eq!(ledger["thread_id"], "200");
    result_delivery::post(&mut h.ctx, "200", text, true, job).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    result_delivery::post(&mut h.ctx, "100", text, true, job).unwrap();
    result_delivery::post(&mut h.ctx, "100", text, true, job).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 2);
    let second = read_json(&result_delivery::ledger_path(&h.ctx, "100", job).unwrap()).unwrap();
    assert_eq!(second["thread_id"], "100");
    assert_eq!(second["complete"], true);
    assert_ne!(second["first"], ledger["first"]);
}

#[test]
fn intake_registers_plain_decision_result_after_response_loss_without_reposting() {
    let mut h = Harness::new();
    h.tick();
    let job = s(&h.state()["requests"]["100"]["job"]).to_owned();
    h.finish(
        &job,
        "succeeded",
        Some("状態: 要判断\nどちらを公開しますか？\na 見本\nb 完成版"),
        None,
    );
    h.ctx.effects = Box::new(LosePostResponse {
        fake: h.fake.clone(),
        lose: true,
    });
    h.tick();
    assert!(h.state()["questions"].as_array().unwrap().is_empty());
    let count = h.fake.borrow().posts.len();
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), count + 1);
    assert_eq!(h.fake.borrow().http_calls.iter().filter(|c|
        c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads")).count(), 2);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
    assert_eq!(h.state()["questions"][0]["source"], "posted");
    assert_eq!(h.state()["questions"][0]["work_thread_id"], "100");
    assert_eq!(h.state()["questions"][0]["posting_job"], job);
    assert_eq!(h.state()["questions"][0]["open"], true);
    assert!(h.state()["requests"]["100"]["job"].is_null());
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), count + 1);
    assert_eq!(h.fake.borrow().http_calls.iter().filter(|c|
        c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads")).count(), 2);
    assert_eq!(h.state()["questions"].as_array().unwrap().len(), 1);
}

#[test]
fn each_plain_text_chunk_has_a_durable_cursor_and_only_first_chunk_mentions() {
    let mut h = Harness::new();
    std::fs::create_dir_all(h.ctx.jobs.join("jobs/text-parts")).unwrap();
    let text = format!(
        "状態: 完了\n公開準備を終えました。\n{}",
        "根拠".repeat(1800)
    );
    let first = match result_delivery::post(&mut h.ctx, "200", &text, true, "text-parts").unwrap() {
        Delivery::Sent(Some(id)) => id,
        _ => panic!("text delivery was not recorded"),
    };
    let posts = h.fake.borrow().posts.clone();
    // Header and the two long prose segments are three Discord messages.
    assert_eq!(posts.len(), 3);
    assert_eq!(posts[0].1["allowed_mentions"]["users"], json!(["OWN"]));
    assert_eq!(posts[1].1["allowed_mentions"]["users"], json!([]));
    assert_eq!(posts[1].1["flags"], SILENT);
    assert_eq!(posts[2].1["allowed_mentions"]["users"], json!([]));
    assert_eq!(posts[2].1["flags"], SILENT);
    result_delivery::post(&mut h.ctx, "200", &text, true, "text-parts").unwrap();
    assert_eq!(h.fake.borrow().posts.len(), posts.len());
    let ledger = read_json(&h.ctx.jobs.join("jobs/text-parts/attachment-delivery.json")).unwrap();
    assert_eq!(ledger["last"], first);
    assert_eq!(ledger["next"], 3);
}

#[test]
fn already_delivered_reply_to_this_job_does_not_block_its_final_result() {
    let mut h = Harness::new();
    h.tick();
    let mid = h.reply("a 承認した内容で進めて", "100");
    let mut state = h.state();
    state["answer_receipts"][&mid] =
        json!({"thread":"100","posted":true,"handled_by":"job1","handled_via":"replies-event"});
    h.save(&state);
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 完了\n承認を反映した結果です"),
        None,
    );
    h.tick();
    assert!(h.text().contains("承認を反映した結果です"));
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert!(h.state()["requests"]["100"]["job"].is_null());
    assert_eq!(
        read_json(&result_delivery::ledger_path(&h.ctx, "100", "job1").unwrap()).unwrap()
            ["complete"],
        true
    );
}

#[test]
fn reply_delivered_to_another_job_still_withholds_this_jobs_stale_result() {
    let mut h = Harness::new();
    h.tick();
    let mid = h.reply("別の担当が反映する修正", "100");
    let mut state = h.state();
    state["answer_receipts"][&mid] = json!({"thread":"100","handled_by":"another-worker"});
    h.save(&state);
    let before = h.fake.borrow().posts.len();
    let result = result_delivery::post_with_cursor(
        &mut h.ctx,
        "100",
        "状態: 完了\n古い結果",
        false,
        "job1",
        Some("100"),
    )
    .unwrap();
    assert!(matches!(result, Delivery::OwnerMessagesPending));
    assert_eq!(h.fake.borrow().posts.len(), before);
}

#[test]
fn confirmed_first_page_cannot_hide_a_pending_reply_outside_the_page() {
    let mut h = Harness::new();
    h.tick();
    let mut messages = vec![message("300", "ページ外に残った別担当への返信", "OWN")];
    let mut state = h.state();
    state["answer_receipts"]["300"] = json!({"thread":"100","handled_by":"another-worker"});
    for id in 400..500 {
        messages.push(message(&id.to_string(), "元の担当が読んだ返信", "OWN"));
        state["answer_receipts"][id.to_string()] = json!({"thread":"100","handled_by":"job1"});
    }
    h.fake.borrow_mut().messages.insert("100".into(), messages);
    h.save(&state);
    let pending = pending_owner_messages_for_job(&mut h.ctx, "100", "100", "OWN", "job1").unwrap();
    assert_eq!(
        pending.iter().map(|m| s(&m["id"])).collect::<Vec<_>>(),
        ["300"]
    );
    let thread = h.fake.borrow().threads["100"].clone();
    let mut entry = json!({"job":"job1","after":"100","reported":true});
    assert!(!archive_completed(&mut h.ctx, &thread, &mut entry, "OWN").unwrap());
    assert_eq!(entry["after"], "100");
    assert!(h.fake.borrow().patches.is_empty());
}
