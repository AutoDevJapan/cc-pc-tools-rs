mod common;
use cc_ask::{commands::*, reply_recovery::*};
use common::{message, Harness};
use serde_json::json;

#[test]
fn absolute_retry_deadline_survives_restart_and_never_closes() {
    let mut q = json!({"thread_id":"200","after":"200","open":true});
    failed(&mut q, 1000., "URLError: transport failed", "300");
    let deadline = q["reply_resume_retry"]["deadline"].clone();
    assert!(!due(&mut q, 1059.));
    assert!(due(&mut q, 1060.));
    let mut q = serde_json::from_str(&q.to_string()).unwrap();
    failed(&mut q, 1060., "URLError: transport failed", "300");
    assert_eq!(q["reply_resume_retry"]["deadline"], deadline);
    assert!(!due(&mut q, 1900.));
    assert!(!due(&mut q, 10000.));
    assert_eq!(q["open"], true);
    assert_ne!(q["recovery_closed"], true);
    assert_eq!(q["reply_resume_retry"]["exhausted"], true);
    delivered(&mut q);
    assert!(due(&mut q, 10001.));
    assert!(q["resume_status"].is_null());
}

#[test]
fn new_answers_and_manual_reads_clear_only_the_old_answers_failure() {
    for after in ["200", "250"] {
        let mut record =
            json!({"thread_id":"200","after":after,"answered_reply":"300","open":true});
        failed(&mut record, 1000., "URLError: transport failed", "300");
        let mut state =
            json!({"owner_answers":{"200":"300"},"answer_receipts":{"300":{"thread":"200"}}});
        assert!(!due(&mut record, 2000.));
        assert!(prevents_close(&state, &record));
        state["answer_receipts"]["300"]["read_by"] = json!("reader");
        refresh(&state, &mut record, "200");
        assert!(due(&mut record, 2001.));
        assert!(!prevents_close(&state, &record));
        state["owner_answers"]["200"] = json!("400");
        state["answer_receipts"]["400"] = json!({"thread":"200"});
        assert!(!prevents_close(&state, &record));
        failed(&mut record, 2100., "URLError: transport failed", "400");
        assert_eq!(record["reply_resume_retry"]["reply"], "400");
        assert_eq!(record["reply_resume_retry"]["deadline"], 3000.);
        state["owner_answers"]["200"] = json!("500");
        refresh(&state, &mut record, "200");
        assert!(due(&mut record, 3100.));
        assert!(!prevents_close(&state, &record));
    }
}

#[test]
fn nontransient_launch_errors_do_not_enter_a_permanent_deadline_block() {
    let mut record = json!({"thread_id":"200","after":"200","open":true});
    failed(
        &mut record,
        1000.,
        "launch blocked: active job writer owns this thread",
        "300",
    );
    assert!(record["reply_resume_retry"].is_null());
    assert!(due(&mut record, 10000.));
}

#[test]
fn explicit_retry_rejects_request_stop_and_manually_cancelled_owner_job() {
    for manual_job in [false, true] {
        let mut h = Harness::new();
        let mut q = json!({"thread_id":"200","after":"200","open":false,
            "answered_reply":"300","last_resume_error":"URLError: transport failed"});
        let mut request = json!({});
        if manual_job {
            let dir = h.ctx.jobs.join("jobs/stopped");
            std::fs::create_dir_all(&dir).unwrap();
            std::fs::write(
                dir.join("job.json"),
                json!({"status":"cancelled","cancel_requested":true,
                "cancel_reason":"manual","thread_id":"stopped-session"})
                .to_string(),
            )
            .unwrap();
            q["continuation"] = json!({"job":"stopped"});
        } else {
            request["owner_stopped"] = json!(true);
        }
        h.save(&json!({"questions":[q],"requests":{"200":request}}));
        let before = h.state();
        assert!(retry_reply(&mut h.ctx, "200").is_err());
        assert_eq!(h.state(), before);
        assert!(h.fake.borrow().launches.is_empty());
    }
}

#[test]
fn request_retry_expires_without_closing_and_a_new_answer_resumes_the_same_author() {
    use cc_ask::{restart::Slot, steering};
    let mut h = Harness::new();
    let thread = json!({"id":"200","name":"配布方法"});
    let dir = h.ctx.jobs.join("jobs/original");
    std::fs::create_dir_all(&dir).unwrap();
    let source = json!({"status":"running","model":"gpt-6.1-sol","effort":"max",
        "cwd":h.root.path(),"sandbox":"danger-full-access","discord_thread_id":"200","thread_id":"authors-session"});
    std::fs::write(dir.join("job.json"), source.to_string()).unwrap();
    let mut record = json!({"thread_id":"200","title":"配布方法","after":"200","job":"original","selection":source});
    let mut state = json!({"questions":[],"requests":{"200":record}});
    h.save(&state);
    let first = h.reply("aで進めて", "200");
    h.fake.borrow_mut().launch_transport_fail = true;
    assert!(steering::deliver(
        &mut h.ctx,
        &mut state,
        &thread,
        &mut record,
        Slot::Request,
        "OWN",
        &mut false
    )
    .is_err());
    let deadline = record["reply_resume_retry"]["deadline"].clone();
    h.fake.borrow_mut().now += 901.;
    assert!(steering::deliver(
        &mut h.ctx,
        &mut state,
        &thread,
        &mut record,
        Slot::Request,
        "OWN",
        &mut false
    )
    .unwrap());
    assert!(h.fake.borrow().launches.is_empty());
    assert_eq!(record["open"], true);
    assert_eq!(record["reply_resume_retry"]["deadline"], deadline);
    state["answer_receipts"][first]["read_by"] = json!("reader");
    let second = h.reply("bに変更して続けて", "200");
    h.fake.borrow_mut().launch_transport_fail = false;
    assert!(steering::deliver(
        &mut h.ctx,
        &mut state,
        &thread,
        &mut record,
        Slot::Request,
        "OWN",
        &mut false
    )
    .unwrap());
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(
        h.fake.borrow().launches[0]["resume_session"],
        "authors-session"
    );
    assert_eq!(state["answer_receipts"][second]["handled_by"], "job1");
}

#[test]
fn close_refuses_an_undelivered_transport_failure_but_allows_a_delivered_answer() {
    let mut h = Harness::new();
    let q = json!({"thread_id":"200","after":"200","answered_reply":"300","open":true,
        "last_resume_error":"URLError: transport failed"});
    h.save(&json!({"questions":[q]}));
    assert!(cmd_close(&mut h.ctx, "200").is_err());
    assert_eq!(h.state()["questions"][0]["open"], true);
    assert!(h.fake.borrow().patches.is_empty());
    let mut state = h.state();
    state["answer_receipts"] = json!({"300":{"handled_by":"resumed"}});
    h.save(&state);
    cmd_close(&mut h.ctx, "200").unwrap();
    assert_eq!(h.state()["questions"][0]["open"], false);
}

#[test]
fn inbox_shows_old_failed_or_unroutable_answers_on_every_inspection_without_advancing_cursor() {
    for status in ["再開失敗", "再開先なし"] {
        let mut h = Harness::new();
        let cursor = h.root.path().join("cursor.txt");
        std::fs::write(&cursor, "999999").unwrap();
        h.fake
            .borrow_mut()
            .messages
            .insert("200".into(), vec![message("300", "aで進めて", "OWN")]);
        h.save(&json!({"questions":[{"thread_id":"200","after":"200","answered_reply":"300",
            "title":"配布方法","open":true,"resume_status":status,"last_resume_error":"transport failed"}]}));
        for _ in 0..2 {
            let out = cmd_inbox(&mut h.ctx, &cursor).unwrap();
            assert_eq!(out.len(), 1);
            assert_eq!(out[0]["status"], status);
            assert_eq!(out[0]["id"], "300");
        }
        assert_eq!(std::fs::read_to_string(&cursor).unwrap(), "999999");
        assert!(h.fake.borrow().launches.is_empty());
        let mut state = h.state();
        state["answer_receipts"] = json!({"300":{"read_by":"owner-session"}});
        h.save(&state);
        assert_eq!(
            cmd_inbox(&mut h.ctx, &cursor).unwrap()[0]["delivered"],
            true
        );
        let mut state = h.state();
        state["questions"][0]["open"] = json!(false);
        h.save(&state);
        assert!(cmd_inbox(&mut h.ctx, &cursor).unwrap().is_empty());
    }
}

#[test]
fn sessionless_posted_and_request_answers_resume_the_failed_author_session_once() {
    for source in ["posted", "request"] {
        for session_field in ["thread_id", "resume_session"] {
            let mut h = Harness::new();
            let dir = h.ctx.jobs.join("jobs/original");
            std::fs::create_dir_all(&dir).unwrap();
            let mut job = json!({"status":"failed","model":"gpt-6.1-sol","effort":"max",
                "cwd":h.root.path(),"sandbox":"danger-full-access","discord_thread_id":"200"});
            job[session_field] = json!("authors-session");
            std::fs::write(dir.join("job.json"), job.to_string()).unwrap();
            std::fs::write(dir.join("prompt.md"), "元の作業を進める").unwrap();
            let mut post = message(
                "200",
                "a/bで返信してください\n-# ジョブ original。続きはこのスレッドに返信してください。",
                "BOT",
            );
            post["author"]["bot"] = json!(true);
            h.fake
                .borrow_mut()
                .messages
                .insert("200".into(), vec![post]);
            h.save(
                &json!({"questions":[{"thread_id":"200","after":"200","question_message":"200",
                "cwd":h.root.path(),"title":"配布方法","open":true,"source":source}]}),
            );
            let mid = h.reply("aで進めてください", "200");
            cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
            cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
            assert_eq!(h.fake.borrow().launches.len(), 1);
            let f = h.fake.borrow();
            assert_eq!(f.launches[0]["resume_session"], "authors-session");
            assert!(f.launches[0]["prompt"]
                .as_str()
                .unwrap()
                .contains("aで進めてください"));
            assert_eq!(h.state()["answer_receipts"][mid]["handled_by"], "job1");
        }
    }
}

#[test]
fn request_workers_posted_question_has_one_handoff_across_resume_and_intake() {
    use cc_ask::{core::*, intake, resume};
    let mut h = Harness::new();
    h.tick();
    let path = h.ctx.jobs.join("jobs/job1/job.json");
    let mut job = read_json(&path).unwrap();
    job["cwd"] = json!(h.root.path());
    atomic_json(&path, &job, 2).unwrap();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let body = h.root.path().join("question.txt");
    std::fs::write(&body, "a/bで返信してください").unwrap();
    cmd_post(&mut h.ctx, "100", &body, &[], false).unwrap();
    let reply = h.reply("aで進めて", "100");
    for _ in 0..2 {
        let mut budget = false;
        resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
        intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
    }
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake.borrow().launches[1]["resume_session"],
        "session-job1"
    );
    assert_eq!(h.state()["answer_receipts"][reply]["handled_by"], "job2");
    assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    let posts = h.fake.borrow().posts.len();
    h.fake.borrow_mut().now += 3601.;
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), posts);
}
