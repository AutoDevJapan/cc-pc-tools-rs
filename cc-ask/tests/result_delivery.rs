mod common;
use cc_ask::{core::*, result_delivery};
use common::*;
use serde_json::{json, Value};
use std::{
    cell::{Cell, RefCell},
    rc::Rc,
};

struct UploadEffects {
    fake: Rc<RefCell<Fake>>,
    uploads: Rc<RefCell<Vec<Vec<u8>>>>,
    fail_at: Rc<Cell<Option<usize>>>,
    permanent: bool,
}

impl Effects for UploadEffects {
    fn multipart(&mut self, url: &str, _: &str, body: &[u8]) -> Result<Value> {
        self.uploads.borrow_mut().push(body.to_vec());
        if self.fail_at.get() == Some(self.uploads.borrow().len()) {
            self.fail_at.set(None);
            return Err(if self.permanent {
                "Discord upload failed: 403 Missing Permissions"
            } else {
                "mock upload connection failed"
            }
            .into());
        }
        let text = String::from_utf8_lossy(body);
        let (_, rest) = text.split_once("\r\n\r\n").unwrap();
        let (payload, _) = rest.split_once("\r\n--").unwrap();
        let payload: Value = serde_json::from_str(payload).unwrap();
        self.fake
            .borrow_mut()
            .http("POST", url, Some(&payload), true)
    }

    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        self.fake.borrow_mut().http(method, url, body, discord)
    }

    fn run(&mut self, argv: &[String], timeout_s: u64) -> Result<Output> {
        self.fake.borrow_mut().run(argv, timeout_s)
    }

    fn now(&self) -> f64 {
        self.fake.borrow().now
    }
}

#[test]
fn explicit_attachment_lines_are_removed_and_deduplicated() {
    let root = tempfile::tempdir().unwrap();
    let path = root.path().join("試写版.mp4");
    let text = format!(
        "状態: 要判断\n試写版をお届けします。\n\n添付ファイル: {}\n添付ファイル: {}\n",
        path.display(),
        path.display()
    );
    let (body, paths) = result_delivery::extract(&text).unwrap();
    assert_eq!(body, "状態: 要判断\n試写版をお届けします。");
    assert_eq!(paths, vec![path]);
}

#[test]
fn source_archives_and_executables_are_never_attached() {
    let root = tempfile::tempdir().unwrap();
    let shot = root.path().join("board.png");
    let text = format!(
        "状態: 完了\n試作です。\n添付ファイル: {}\n添付ファイル: {}\n添付ファイル: {}\n",
        root.path().join("src.zip").display(),
        root.path().join("Game.exe").display(),
        shot.display()
    );
    assert_eq!(
        result_delivery::extract(&text).unwrap(),
        ("状態: 完了\n試作です。".to_owned(), vec![shot])
    );
    let only_zip = format!(
        "状態: 完了\n添付ファイル: {}",
        root.path().join("a.zip").display()
    );
    assert_eq!(
        result_delivery::extract(&only_zip).unwrap(),
        ("状態: 完了".to_owned(), vec![])
    );
}

#[test]
fn list_fullwidth_colon_and_quoted_paths_are_attached() {
    let root = tempfile::tempdir().unwrap();
    let path = root.path().join("preview.mp4");
    for line in [
        format!("- 添付ファイル： `{}`", path.display()),
        format!("* 添付ファイル: \"{}\"", path.display()),
        format!("+ 添付ファイル : '{}'", path.display()),
    ] {
        assert_eq!(
            result_delivery::extract(&format!("説明\n{line}")).unwrap(),
            ("説明".to_owned(), vec![path.clone()])
        );
    }
}

#[test]
fn code_examples_and_reports_without_directives_remain_text() {
    let text = "状態: 完了\n```text\n添付ファイル: relative.mp4\n```\n";
    assert_eq!(
        result_delivery::extract(text).unwrap(),
        (text.to_owned(), vec![])
    );
    let text = "状態: 完了\n直しました。\n";
    assert_eq!(
        result_delivery::extract(text).unwrap(),
        (text.to_owned(), vec![])
    );
    assert!(result_delivery::extract("状態: 完了\n添付ファイル: relative.mp4").is_err());
}

#[test]
fn intake_uploads_the_bytes_in_the_original_thread_then_records_delivery() {
    let mut h = Harness::new();
    let uploads = Rc::new(RefCell::new(Vec::new()));
    h.ctx.effects = Box::new(UploadEffects {
        fake: h.fake.clone(),
        uploads: uploads.clone(),
        fail_at: Rc::new(Cell::new(None)),
        permanent: false,
    });
    h.tick();
    let path = h.root.path().join("preview.mp4");
    let bytes = b"exact-preview-bytes\0test-data";
    std::fs::write(&path, bytes).unwrap();
    h.finish(
        "job1",
        "succeeded",
        Some(&format!(
            "状態: 完了\n試写版をお届けします。\n\n添付ファイル: {}",
            path.display()
        )),
        None,
    );
    h.tick();
    assert_eq!(uploads.borrow().len(), 1);
    assert!(uploads.borrow()[0].windows(bytes.len()).any(|w| w == bytes));
    let fake = h.fake.borrow();
    let (thread, payload) = fake.posts.last().unwrap();
    assert_eq!(thread, "100");
    assert_eq!(
        payload["attachments"],
        json!([{"id":0,"filename":"preview.mp4"}])
    );
    assert_eq!(
        payload["allowed_mentions"],
        json!({"parse":[],"users":["OWN"]})
    );
    assert_eq!(payload["enforce_nonce"], true);
    assert_eq!(s(&payload["nonce"]).len(), 24);
    assert!(!s(&payload["content"]).contains("添付ファイル:"));
    assert!(!s(&payload["content"]).contains(&path.to_string_lossy().to_string()));
    assert!(s(&payload["content"]).contains("試写版をお届けします。"));
    assert_eq!(fake.patches, vec![("100".into(), json!({"archived":true}))]);
    drop(fake);
    assert!(h.state()["requests"]["100"]["job"].is_null());
}

#[test]
fn progress_attachments_are_silent_and_have_no_owner_mention() {
    let mut h = Harness::new();
    let uploads = Rc::new(RefCell::new(Vec::new()));
    h.ctx.effects = Box::new(UploadEffects {
        fake: h.fake.clone(),
        uploads: uploads.clone(),
        fail_at: Rc::new(Cell::new(None)),
        permanent: false,
    });
    h.tick();
    let path = h.root.path().join("preview.mp4");
    std::fs::write(&path, b"progress-preview").unwrap();
    h.finish(
        "job1",
        "succeeded",
        Some(&format!(
            "状態: 継続中\n試写を作成しました。検証を続けます。\n添付ファイル: {}",
            path.display()
        )),
        None,
    );
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(uploads.borrow().len(), 1);
    let post = &f.posts.last().unwrap().1;
    assert_eq!(post["flags"], SILENT);
    assert_eq!(post["allowed_mentions"]["users"], json!([]));
    assert!(!s(&post["content"]).contains("判断をお願い"));
    assert!(f.patches.is_empty());
}

#[test]
fn missing_attachment_notifies_once_keeps_todo_open_and_restarts_to_repair() {
    let mut h = Harness::new();
    let uploads = Rc::new(RefCell::new(Vec::new()));
    h.ctx.effects = Box::new(UploadEffects {
        fake: h.fake.clone(),
        uploads: uploads.clone(),
        fail_at: Rc::new(Cell::new(None)),
        permanent: false,
    });
    h.tick();
    let mut state = h.state();
    state["requests"]["100"]["restart"] = json!({"owns_todo":true,"todo":888});
    h.save(&state);
    atomic_json(
        &h.ctx.todo,
        &json!({"projects":{},"next_id":889,"items":[{
            "id":888,"project":"requests","title":"添付を修復する","state":"doing","jobs":["job1"]
        }]}),
        2,
    )
    .unwrap();
    let before = h.fake.borrow().posts.len();
    let path = h.root.path().join("missing.mp4");
    h.finish(
        "job1",
        "succeeded",
        Some(&format!(
            "状態: 完了\n試写版をお届けします。\n添付ファイル: {}",
            path.display()
        )),
        None,
    );
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), before + 1);
    assert!(uploads.borrow().is_empty());
    assert!(h.fake.borrow().patches.is_empty());
    assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    assert!(!truth(
        &h.state()["requests"]["100"]["restart"]["todo_done"]
    ));
    assert!(h.text().contains("添付ファイルを送れませんでした"));
    assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("添付を受付で送れませんでした"));
    assert!(!h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(args, _)| args.iter().any(|arg| arg == "done")));
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), before + 1);
}

#[test]
fn partial_upload_resumes_after_the_saved_part_without_repeating_the_mention() {
    let mut h = Harness::new();
    h.tick();
    let uploads = Rc::new(RefCell::new(Vec::new()));
    let fail_at = Rc::new(Cell::new(Some(2)));
    h.ctx.effects = Box::new(UploadEffects {
        fake: h.fake.clone(),
        uploads: uploads.clone(),
        fail_at,
        permanent: false,
    });
    let mut body = "完成しました。".to_owned();
    for i in 0..11 {
        let path = h.root.path().join(format!("frame{i}.png"));
        std::fs::write(&path, format!("frame-{i}")).unwrap();
        body.push_str(&format!("\n添付ファイル: {}", path.display()));
    }
    let before = h.fake.borrow().posts.len();
    assert!(result_delivery::post(&mut h.ctx, "100", &body, true, "job1").is_err());
    h.fake.borrow_mut().now += 600.;
    assert!(matches!(
        result_delivery::post(&mut h.ctx, "100", &body, true, "job1").unwrap(),
        result_delivery::Delivery::Sent(_)
    ));
    assert_eq!(uploads.borrow().len(), 3);
    let fake = h.fake.borrow();
    let delivered = &fake.posts[before..];
    assert_eq!(delivered.len(), 2);
    assert_eq!(delivered[0].1["attachments"].as_array().unwrap().len(), 10);
    assert_eq!(
        delivered[1].1["attachments"],
        json!([{"id":0,"filename":"frame10.png"}])
    );
    assert_eq!(
        delivered
            .iter()
            .filter(|(_, p)| p["allowed_mentions"]["users"] == json!(["OWN"]))
            .count(),
        1
    );
    drop(fake);
    result_delivery::post(&mut h.ctx, "100", &body, true, "job1").unwrap();
    assert_eq!(uploads.borrow().len(), 3);
}

#[test]
fn continuation_result_uses_the_same_attachment_delivery() {
    let mut h = Harness::new();
    h.tick();
    let uploads = Rc::new(RefCell::new(Vec::new()));
    h.ctx.effects = Box::new(UploadEffects {
        fake: h.fake.clone(),
        uploads: uploads.clone(),
        fail_at: Rc::new(Cell::new(None)),
        permanent: false,
    });
    let path = h.root.path().join("preview.mp4");
    std::fs::write(&path, b"continuation-preview").unwrap();
    h.finish(
        "job1",
        "succeeded",
        Some(&format!(
            "状態: 完了\n試写版をお届けします。\n添付ファイル: {}",
            path.display()
        )),
        None,
    );
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"200","title":"試写版","open":true,"after":"201","seen_reply":"201",
        "cwd":h.root.path().to_string_lossy(),"continuation":{"job":"job1","after":"201","reported":false}}]);
    h.save(&state);
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![
            message("200", "質問", "BOT"),
            message("201", "Minecraft", "OWN"),
        ],
    );
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(uploads.borrow().len(), 1);
    assert_eq!(h.fake.borrow().posts.last().unwrap().0, "200");
    assert!(!s(&h.fake.borrow().posts.last().unwrap().1["content"]).contains("添付ファイル:"));
    assert!(truth(
        &h.state()["questions"][0]["continuation"]["reported"]
    ));
}

#[test]
fn ordinary_attachment_sentences_are_posted_without_notices_or_repair() {
    let mut h = Harness::new();
    h.tick();
    let body = "状態: 完了\n添付ファイルはありません。\n- 添付ファイルは受付から送られます。";
    assert_eq!(
        result_delivery::extract(body).unwrap(),
        (body.to_owned(), vec![])
    );
    h.finish("job1", "succeeded", Some(body), None);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert!(h.text().contains("添付ファイルはありません。"));
    assert!(!h.text().contains("送れませんでした"));
    assert_eq!(
        h.fake.borrow().patches,
        vec![("100".into(), json!({"archived":true}))]
    );
}

#[test]
fn permanent_upload_response_enters_repair_even_if_notice_is_forbidden() {
    let mut h = Harness::new();
    h.tick();
    let uploads = Rc::new(RefCell::new(Vec::new()));
    h.ctx.effects = Box::new(UploadEffects {
        fake: h.fake.clone(),
        uploads: uploads.clone(),
        fail_at: Rc::new(Cell::new(Some(1))),
        permanent: true,
    });
    let path = h.root.path().join("preview.mp4");
    std::fs::write(&path, b"preview").unwrap();
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/100/messages".into(), 1);
    let body = format!("完成しました。\n添付ファイル: {}", path.display());
    assert!(matches!(
        result_delivery::post(&mut h.ctx, "100", &body, true, "job1").unwrap(),
        result_delivery::Delivery::PreparationFailed(_)
    ));
    assert_eq!(uploads.borrow().len(), 1);
    assert!(h.fake.borrow().patches.is_empty());
}

#[test]
fn separated_question_receipt_and_result_use_only_silent_progress() {
    let mut h = Harness::new();
    h.tick();
    let job_path = h.ctx.jobs.join("jobs/job1/job.json");
    let mut info = read_json(&job_path).unwrap();
    info["cwd"] = json!(h.root.path().to_string_lossy());
    atomic_json(&job_path, &info, 2).unwrap();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\nどちらを公開しますか？\na 見本\nb 完成版"),
        None,
    );
    h.tick();
    let state = h.state();
    let q = state["questions"][0].clone();
    let qid = s(&q["thread_id"]).to_owned();
    let progress = s(&q["progress_thread_id"]).to_owned();
    assert_eq!(q["work_thread_id"], "100");
    let before = h.fake.borrow().posts.len();
    let reply = h.reply("a 見本を公開して", &qid);
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    h.fake.borrow_mut().now += 601.;
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.state()["answer_receipts"][&reply]["handled_by"], "job2");
    h.finish(
        "job2",
        "succeeded",
        Some("状態: 完了\n見本を公開しました。"),
        None,
    );
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    let fake = h.fake.borrow();
    let delivered = &fake.posts[before..];
    assert!(!delivered.is_empty());
    assert!(delivered.iter().all(|(tid, body)| {
        tid == &progress
            && body["flags"] == json!(SILENT | 4)
            && body["allowed_mentions"]["users"] == json!([])
            && body["allowed_mentions"]["replied_user"] == json!(false)
            && !s(&body["content"]).contains("<@OWN>")
    }));
    assert!(delivered
        .iter()
        .any(|(_, body)| s(&body["content"]).contains("見本を公開しました。")));
    assert!(!fake.patches.iter().any(|(tid, _)| tid == "100"));
}
