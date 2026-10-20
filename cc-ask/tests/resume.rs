mod common;
use cc_ask::{core::*, resume::*};
use common::{message, Harness, SharedFake};
use serde_json::{json, Value};
use std::{
    panic::{catch_unwind, AssertUnwindSafe},
    path::{Path, PathBuf},
};

#[test]
fn test_strip_verbatim_preserves_unc_and_dos_paths() {
    assert_eq!(
        strip_verbatim(r"\\?\UNC\server\share\repo"),
        r"\\server\share\repo"
    );
    assert_eq!(strip_verbatim(r"\\?\D:\repo"), r"D:\repo");
    assert_eq!(
        strip_verbatim(r"\\server\share\repo"),
        r"\\server\share\repo"
    );
    assert_eq!(strip_verbatim(r"D:\repo"), r"D:\repo");
}

#[test]
fn real_reply_shapes_after_reask_in_central_worktree() {
    for open in [false, true] {
        let mut a = AnswerResume::new();
        let repo = a.repository();
        // This regression owns its fake Git roots. Do not write to a real
        // machine-wide worktree folder just because it exists on the runner.
        let central = a.root.path().to_path_buf();
        let worktree_fixture = tempfile::Builder::new()
            .prefix("cc-ask-resume-regression-")
            .tempdir_in(central)
            .unwrap();
        let worktree = worktree_fixture
            .path()
            .join("GoDD-MCP-Alpha/cycle83-issue1056-stage3");
        std::fs::create_dir_all(&worktree).unwrap();
        std::fs::write(worktree.join(".git"), "mock gitdir").unwrap();
        a.fake.borrow_mut().git_roots.insert(
            worktree.to_string_lossy().into(),
            repo.to_string_lossy().into(),
        );
        let tid = "1555332315964641414";
        let first = "1555370834103767162";
        let newest = "1555372689806590108";
        a.change(json!({"thread_id":tid,"after":tid,"seen_reply":"1555369686609301649","cwd":worktree.to_string_lossy(),"agent":"Codex","open":open}));
        {
            let mut f = a.fake.borrow_mut();
            f.now = snowflake_time(first) + 601.;
            f.sequence = newest.parse().unwrap();
            f.messages.insert(
                tid.into(),
                vec![
                    message(tid, "質問", "BOT"),
                    message("1555369686609301649", "意味わからん", "OWN"),
                    message("1555369924346777692", "質問を言い直します。a/b/c/d", "BOT"),
                    message(first, "C", "OWN"),
                    message(newest, "前回の回答を確認してね。", "OWN"),
                ],
            );
        }
        a.resume();
        a.resume();
        assert_eq!(a.count(), usize::from(open));
        assert_eq!(a.posts(), if open { 2 } else { 0 });
        if open {
            assert_eq!(
                path_key(s(&a.last()["cwd"])),
                path_key(&worktree.to_string_lossy())
            );
            assert_eq!(a.question()["seen_reply"], newest);
            assert!(s(&a.last()["prompt"]).contains("C"));
            assert!(s(&a.last()["prompt"]).contains("質問を言い直します"));
        }
    }
}

#[test]
fn receipt_is_immediate_silent_redacted_unicode_and_once_across_commands() {
    let mut a = AnswerResume::new();
    std::fs::write(&a.ctx.client_projects, "SecretClient\n").unwrap();
    a.change(json!({"project":"SecretClient","agent":"Codex"}));
    let text = "あ".repeat(41);
    let id = a.answer(0., &text, "OWN");
    a.resume();
    assert_eq!(a.count(), 0);
    a.replies(Some("200"));
    a.replies(Some("200"));
    a.resume();
    let f = a.fake.borrow();
    assert_eq!(f.posts.len(), 1);
    let b = &f.posts[0].1;
    assert_eq!(
        b["content"],
        "【クライアント案件】受け取りました。今の作業に反映します。"
    );
    assert_eq!(b["flags"], SILENT);
    assert_eq!(
        b["allowed_mentions"],
        json!({"parse":[],"users":[],"replied_user":false})
    );
    assert_eq!(b["nonce"], id);
    assert_eq!(b["message_reference"]["message_id"], id);
    assert_eq!(b["enforce_nonce"], true);
}

#[test]
fn receipt_recovers_lost_checkpoint_even_after_nonce_window() {
    let mut a = AnswerResume::new();
    let id = a.answer(0., "<@1554269962229649478> C", "OWN");
    a.resume();
    assert_eq!(a.posts(), 1);
    let mut state = a.state();
    state["answer_receipts"][&id]["posted"] = json!(false);
    a.save(&state);
    a.advance(3600.);
    a.resume();
    assert_eq!(a.posts(), 1);
    assert_eq!(a.count(), 1);
    assert!(a.text().contains("受け取りました。今の作業に反映します。"));
    assert!(!a.text().contains("<@"));
    assert_eq!(a.state()["answer_receipts"][&id]["posted"], true);
}

#[test]
fn replies_alone_posts_receipt_once_and_records_success_before_seen() {
    let mut a = AnswerResume::new();
    let id = a.answer(0., "C", "OWN");
    a.fake.borrow_mut().post_error = true;
    assert!(cc_ask::commands::cmd_replies(&mut a.ctx, Some("200")).is_err());
    assert!(a.question().get("seen_reply").is_none());
    a.replies(Some("200"));
    a.replies(Some("200"));
    a.resume();
    assert_eq!(a.posts(), 1);
    assert_eq!(a.count(), 0);
    assert_eq!(a.question()["seen_reply"], id);
}

#[test]
fn centralized_worktrees_match_their_repository_only() {
    assert!(in_project(
        "D:/CC/Worktrees/GoDD-MCP-Alpha/cycle83-issue1056-stage3",
        "D:/CC/GoDD-MCP-Alpha"
    ));
    assert!(!in_project(
        "D:/CC/Worktrees/GoDD-MCP-Alpha-other/cycle83",
        "D:/CC/GoDD-MCP-Alpha"
    ));
}

struct AnswerResume {
    h: Harness,
    cwd: PathBuf,
}

#[test]
fn transient_answer_launch_retries_same_reservation_and_stays_open_until_delivery() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().launch_transport_fail = true;
    a.resume();
    assert_eq!(a.count(), 0);
    let failed = a.question();
    let key = failed["continuation"]["launch"]["key"].clone();
    assert_eq!(failed["resume_status"], "再開失敗");
    assert_eq!(failed["open"], true);
    assert_ne!(failed["recovery_closed"], true);
    a.fake.borrow_mut().launch_transport_fail = false;
    a.resume();
    assert_eq!(a.count(), 0);
    a.advance(60.);
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.last()["launch_key"], key);
    assert!(a.question()["resume_status"].is_null());
    a.resume();
    assert_eq!(a.count(), 1);
}

#[test]
fn exhausted_answer_retry_does_not_close_or_launch_after_absolute_deadline() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().launch_transport_fail = true;
    a.resume();
    let deadline = a.question()["reply_resume_retry"]["deadline"].clone();
    a.advance(60.);
    a.resume();
    assert_eq!(a.question()["reply_resume_retry"]["deadline"], deadline);
    a.advance(900.);
    a.fake.borrow_mut().launch_transport_fail = false;
    a.resume();
    a.advance(3600.);
    a.resume();
    assert_eq!(a.count(), 0);
    assert_eq!(a.question()["open"], true);
    assert_eq!(a.question()["reply_resume_retry"]["exhausted"], true);
    assert!(a.fake.borrow().patches.is_empty());
    assert!(a.question()["continuation"]["launch"].is_object());
}

#[test]
fn answer_after_expired_retry_and_manual_read_is_delivered_with_a_new_reservation() {
    let mut a = AnswerResume::new();
    let first = a.answered();
    a.fake.borrow_mut().launch_transport_fail = true;
    a.resume();
    a.advance(901.);
    a.resume();
    let mut state = a.state();
    state["answer_receipts"][&first]["read_by"] = json!("manual-reader");
    a.save(&state);
    let second = a.answer(601., "こちらの追加回答も反映してください", "OWN");
    a.fake.borrow_mut().launch_transport_fail = false;
    a.resume();
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.state()["answer_receipts"][second]["handled_by"], "job1");
    assert!(a.last()["prompt"]
        .as_str()
        .unwrap()
        .contains("追加回答も反映"));
    assert!(a.question()["resume_status"].is_null());
}

#[test]
fn posted_and_request_answer_without_a_source_remains_in_inbox_without_new_worker() {
    for source in ["posted", "request"] {
        let mut a = AnswerResume::new();
        a.change(json!({"source":source,"question_message":"200"}));
        a.answered();
        a.resume();
        a.resume();
        assert_eq!(a.count(), 0);
        assert_eq!(a.question()["resume_status"], "再開先なし");
        assert_eq!(a.question()["open"], true);
        assert!(a.fake.borrow().patches.is_empty());
    }
}

#[test]
fn explicit_retry_restores_transport_closed_question_but_not_completed_or_stopped_questions() {
    for stopped in [false, true] {
        for completed in [false, true] {
            let mut a = AnswerResume::new();
            let answer = a.answered();
            a.change(
                json!({"open":false,"recovery_closed":true,"closed_at":a.ctx.now(),
                "answered_reply":answer,"owner_stopped":stopped,"session_id":"original-session",
                "last_resume_error":"URLError: transport failed"}),
            );
            if completed {
                let mid = (answer.parse::<u64>().unwrap() + 1).to_string();
                let mut done = message(&mid, "状態: 完了\n回答を反映して完了しました。", "BOT");
                done["author"]["bot"] = json!(true);
                a.fake
                    .borrow_mut()
                    .messages
                    .get_mut("200")
                    .unwrap()
                    .push(done);
            }
            a.resume();
            assert_eq!(a.count(), 0);
            let retry = cc_ask::reply_recovery::retry_reply(&mut a.ctx, "200");
            assert_eq!(retry.is_ok(), !stopped && !completed);
            a.resume();
            assert_eq!(a.count(), usize::from(!stopped && !completed));
            if !stopped && !completed {
                assert_eq!(a.last()["resume_session"], "original-session");
                assert_eq!(a.question()["open"], true);
                assert!(a.question()["recovery_closed"].is_null());
            } else {
                assert_eq!(a.question()["open"], false);
            }
        }
    }
}

#[test]
fn resume_inherits_permissions_and_routing_from_source_without_preamble_classification() {
    for sandbox in ["workspace-write", "read-only"] {
        let mut a = AnswerResume::new();
        a.change(json!({"job_info":{"task_type":"implementation","sandbox":sandbox}}));
        a.answered();
        a.resume();
        assert_eq!(a.last()["task_type"], "implementation");
        assert_eq!(
            a.last()["sandbox"],
            if sandbox == "read-only" {
                "read-only"
            } else {
                "danger-full-access"
            }
        );
    }
}

#[test]
fn owner_reply_changes_owner_scope_but_cannot_promote_a_launcher_reader() {
    for (sandbox, source, reply, expected) in [
        (
            "read-only",
            "owner_marker",
            "直してPRをマージ",
            "danger-full-access",
        ),
        (
            "danger-full-access",
            "owner_request",
            "読むだけ: 差分を調べる",
            "read-only",
        ),
        ("read-only", "launcher", "直して", "read-only"),
    ] {
        let mut a = AnswerResume::new();
        a.change(json!({"job_info":{"task_type":"implementation","sandbox":sandbox,"scope_source":source}}));
        a.answer(601., reply, "OWN");
        a.resume();
        assert_eq!(a.last()["sandbox"], expected);
    }
}

#[test]
fn continuation_progress_and_missing_question_do_not_ping_or_remind_owner() {
    for result in [
        "状態: 継続中\ncargoの空き待ち。テストを続けます。",
        "状態: 要判断\ncargoの空き待ち。テストを続けます。",
    ] {
        let mut a = AnswerResume::new();
        a.answered();
        a.resume();
        a.finish("job1", "succeeded", Some(result), None);
        a.resume();
        let f = a.fake.borrow();
        let post = &f.posts.last().unwrap().1;
        assert_eq!(post["flags"], SILENT);
        assert_eq!(post["allowed_mentions"]["users"], json!([]));
        assert!(!s(&post["content"]).contains("判断をお願い"));
        assert!(f.patches.is_empty());
        drop(f);
        assert_eq!(a.question()["open"], false);
        assert_eq!(a.question()["continuation"]["reported"], true);
        let db = read_json(&a.ctx.todo).unwrap();
        assert_eq!(db["items"][0]["status"], "doing");
        assert_eq!(db["items"][0]["jobs"], json!(["job1"]));
        let count = a.posts();
        a.advance(3601.);
        cc_ask::commands::cmd_remind(&mut a.ctx).unwrap();
        a.resume();
        assert_eq!(a.posts(), count);
        let dir = a.ctx.jobs.join("jobs/next");
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(
            &dir.join("job.json"),
            &json!({"id":"next","status":"running","cwd":a.cwd}),
            2,
        )
        .unwrap();
        a.finish(
            "next",
            "succeeded",
            Some("状態: 完了\n検証を終えました。"),
            None,
        );
        let mut db = read_json(&a.ctx.todo).unwrap();
        db["items"][0]["jobs"]
            .as_array_mut()
            .unwrap()
            .push(json!("next"));
        atomic_json(&a.ctx.todo, &db, 2).unwrap();
        a.resume();
        assert_eq!(a.question()["open"], false);
        assert_eq!(
            read_json(&a.ctx.todo).unwrap()["items"][0]["status"],
            "done"
        );
        assert_eq!(
            a.fake.borrow().patches.last().unwrap().1,
            json!({"archived":true})
        );
    }
}
impl std::ops::Deref for AnswerResume {
    type Target = Harness;
    fn deref(&self) -> &Harness {
        &self.h
    }
}
impl std::ops::DerefMut for AnswerResume {
    fn deref_mut(&mut self) -> &mut Harness {
        &mut self.h
    }
}
impl AnswerResume {
    fn new() -> Self {
        let mut h = Harness::new();
        // Harness keeps fixtures in the writable tool directory, outside the user's home checkout.
        let root = h.root.path().to_path_buf();
        h.ctx.here = root.clone();
        h.ctx.client_projects = root.join("client-projects.txt");
        std::fs::write(&h.ctx.client_projects, "").unwrap();
        h.ctx.jobs = root.clone();
        h.ctx.todo = root.join("todo.json");
        h.fake.borrow_mut().root = root.clone();
        let cwd = root.join("tools");
        std::fs::create_dir(&cwd).unwrap();
        let title = "[Claude] Issue #3467 の実装方針";
        {
            let mut f = h.fake.borrow_mut();
            f.now = 1790773200.;
            f.threads.get_mut("200").unwrap()["name"] = json!(title);
            f.threads.get_mut("200").unwrap()["parent_id"] = json!("T");
            f.messages.insert(
                "200".into(),
                vec![message(
                    "200",
                    "Issue #3467 の元の作業。最適案を選んでよいですか。",
                    "BOT",
                )],
            );
        }
        h.save(&json!({"questions":[{"thread_id":"200","after":"200","title":title,"agent":"Claude","cwd":cwd.to_string_lossy(),"open":true}]}));
        Self { h, cwd }
    }
    fn answer(&self, age: f64, text: &str, owner: &str) -> String {
        let mut f = self.fake.borrow_mut();
        let at = f.now - age;
        let mid = (((at * 1000.) as u64 - 1420070400000) << 22).to_string();
        let mut m = message(&mid, text, owner);
        m["timestamp"] = json!(chrono::DateTime::from_timestamp(at as i64, 0)
            .unwrap()
            .to_rfc3339());
        f.messages.get_mut("200").unwrap().push(m);
        f.sequence = f.sequence.max(mid.parse().unwrap());
        mid
    }
    fn answered(&self) -> String {
        self.answer(601., "調査して最適案を選んで実装", "OWN")
    }
    fn question(&self) -> Value {
        self.state()["questions"][0].clone()
    }
    fn change(&self, values: Value) {
        let mut state = self.state();
        for (k, v) in values.as_object().unwrap() {
            state["questions"][0][k] = v.clone();
        }
        self.save(&state);
    }
    fn resume(&mut self) {
        let before = self.fake.borrow().launches.len();
        cmd_resume(&mut self.ctx, &mut false).unwrap();
        assert!(self.fake.borrow().launches.len() - before <= 1);
    }
    fn count(&self) -> usize {
        self.fake.borrow().launches.len()
    }
    fn last(&self) -> Value {
        self.fake.borrow().launches.last().unwrap().clone()
    }
    fn posts(&self) -> usize {
        self.fake.borrow().posts.len()
    }
    fn todo(&self, age: f64, status: &str, title: &str, path: Option<&Path>) {
        let at = self.fake.borrow().now - age;
        atomic_json(&self.ctx.todo,&json!({"projects":{"P":path.unwrap_or(&self.cwd).to_string_lossy()},"items":[{"id":43,"project":"P","status":status,"title":title,"note":"","updated":chrono::DateTime::from_timestamp(at as i64,0).unwrap().to_rfc3339()}]}),2).unwrap();
    }
    fn running(&self, cwd: Option<&Path>, text: &str, status: &str) {
        let dir = self.ctx.jobs.join("jobs/other");
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(&dir.join("job.json"),&json!({"status":status,"cwd":cwd.unwrap_or(&self.cwd).to_string_lossy(),"name":"other"}),2).unwrap();
        std::fs::write(dir.join("prompt.md"), text).unwrap();
    }
    fn repository(&self) -> PathBuf {
        let repo = self.root.path().join("repo");
        std::fs::create_dir_all(repo.join(".git")).unwrap();
        self.fake
            .borrow_mut()
            .git_roots
            .insert(repo.to_string_lossy().into(), repo.to_string_lossy().into());
        self.change(json!({"cwd":repo.to_string_lossy()}));
        repo
    }
    fn finish_ok(&self) {
        self.finish(
            "job1",
            "succeeded",
            Some("状態: 完了\n直しました https://example.test/pr/1"),
            None,
        );
    }
    fn advance(&self, seconds: f64) {
        self.fake.borrow_mut().now += seconds;
    }
    fn replies(&mut self, thread: Option<&str>) -> Value {
        cc_ask::commands::cmd_replies(&mut self.ctx, thread).unwrap()
    }
}

#[test]
fn test_ask_records_cwd_and_agent() {
    let mut a = AnswerResume::new();
    let body = a.root.path().join("question.md");
    std::fs::write(&body, "作業の方針を教えてください").unwrap();
    let args = cc_ask::commands::AskArgs {
        thread: None,
        attach: vec![],
        agent: "Codex".into(),
        title: "質問".into(),
        body_file: body,
        decision_file: None,
        cwd: None,
        source: None,
        scheduler_item: None,
        scheduler_job: None,
        owner_only: None,
        dry_run: false,
    };
    cc_ask::commands::cmd_ask(&mut a.ctx, &args).unwrap();
    let state = a.state();
    let question = state["questions"].as_array().unwrap().last().unwrap();
    assert_eq!(
        question["cwd"],
        std::env::current_dir().unwrap().to_string_lossy().as_ref()
    );
    assert_eq!(question["agent"], "Codex");
    assert!(s(&question["title"]).starts_with('【'));
    assert!(s(&question["title"]).ends_with("】質問"));
}

#[test]
fn test_replies_acknowledges_only_returned_owner_messages_and_thread() {
    let mut a = AnswerResume::new();
    let repo = a.repository();
    let first = a.answer(1200., "先の回答", "OWN");
    let last = a.answer(601., "最後の回答", "OWN");
    a.answer(0., "他人の発言", "OTHER");
    let mut state = a.state();
    state["questions"]
        .as_array_mut()
        .unwrap()
        .push(json!({"thread_id":"100","after":"0","title":"別の質問","cwd":repo,"open":true}));
    a.save(&state);
    let returned = a.replies(Some("200"));
    assert_eq!(
        returned[0]["replies"]
            .as_array()
            .unwrap()
            .iter()
            .map(|r| s(&r["content"]))
            .collect::<Vec<_>>(),
        vec!["先の回答", "最後の回答"]
    );
    assert_eq!(a.question()["seen_reply"], last);
    assert!(a.state()["questions"][1].get("seen_reply").is_none());
    assert!(last.parse::<u64>().unwrap() > first.parse().unwrap());
    a.resume();
    // Only thread 200 was consumed. Thread 100 still owns its pending owner reply
    // and must resume regardless of the runner's surrounding repositories.
    assert_eq!(a.count(), 1);
    assert!(a.question()["continuation"]["job"].is_null());
    assert_eq!(a.state()["questions"][1]["continuation"]["after"], "100");
}
#[test]
fn test_replies_without_thread_acknowledges_all_returned_questions() {
    let mut a = AnswerResume::new();
    let newest = a.answered();
    let mut state = a.state();
    state["questions"]
        .as_array_mut()
        .unwrap()
        .push(json!({"thread_id":"100","after":"0","title":"他の質問","open":true}));
    a.save(&state);
    a.replies(None);
    assert_eq!(a.state()["questions"][0]["seen_reply"], newest);
    assert_eq!(a.state()["questions"][1]["seen_reply"], "100");
}
#[test]
fn test_empty_replies_preserve_existing_acknowledgement() {
    let mut a = AnswerResume::new();
    a.change(json!({"seen_reply":"500"}));
    a.replies(Some("200"));
    assert_eq!(a.question()["seen_reply"], "500");
}
#[test]
fn test_waits_ten_minutes_and_dispatches_at_boundary() {
    let mut a = AnswerResume::new();
    let newest = a.answer(599., "回答", "OWN");
    a.resume();
    assert_eq!(
        (a.count(), a.posts(), a.fake.borrow().picks.len()),
        (0, 1, 0)
    );
    a.advance(1.);
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.question()["seen_reply"], newest);
}
#[test]
fn test_followup_does_not_restart_wait_for_first_unseen_answer() {
    let mut a = AnswerResume::new();
    a.answer(1800., "回答", "OWN");
    a.answer(20., "回答の補足", "OWN");
    a.resume();
    assert_eq!((a.count(), a.posts()), (1, 2));
}
#[test]
fn test_non_owner_reply_does_not_trigger_continuation() {
    let mut a = AnswerResume::new();
    a.answer(601., "回答", "OTHER");
    a.resume();
    assert_eq!(a.count(), 0);
}
#[test]
fn test_dispatch_uses_recorded_cwd_jev_and_resume_instructions() {
    let mut a = AnswerResume::new();
    let newest = a.answered();
    a.resume();
    let launch = a.last();
    assert_eq!(launch["cwd"], a.cwd.to_string_lossy().as_ref());
    assert_eq!(
        (&launch["name"], &launch["model"], &launch["effort"]),
        (&json!("resume-200"), &json!("gpt-6.1-sol"), &json!("max"))
    );
    assert_eq!(a.question()["seen_reply"], newest);
    assert!(a.text().contains("受け取りました。今の作業に反映します。"));
    for text in [
        "元の作業",
        "調査して最適案を選んで実装",
        "Claude",
        a.cwd.to_str().unwrap(),
        "todo.exe list",
        "別モデル",
        "Must",
        "人間またはオーナー",
        "config.godd",
        "根本原因",
        "状態: 完了",
        "状態: 要判断",
        "状態: 継続中",
    ] {
        assert!(s(&launch["prompt"]).contains(text), "missing {text}");
    }
    let task = a.fake.borrow().picks[0]["task"].clone();
    assert!(s(&task).contains("Issue #3467 の元の作業"));
    assert!(s(&task).contains("調査して最適案を選んで実装"));
    assert!(!s(&task).contains(RESUME_PREAMBLE));
    assert!(!Path::new(s(&launch["prompt_path"])).exists());
    a.resume();
    assert_eq!((a.count(), a.posts()), (1, 1));
}

#[test]
fn managed_answer_resume_migrates_high_hints_but_preserves_individual_owner_effort() {
    for (origin, effort, session) in [
        ("policy", "max", ""),
        ("owner_request", "high", "original-session"),
    ] {
        let mut a = AnswerResume::new();
        a.change(
            json!({"session_id":"original-session","job_info":{"model":"gpt-6.1-sol",
            "effort":"high","effort_origin":origin,"thread_id":"original-session",
            "sandbox":"read-only","scope_source":"launcher"}}),
        );
        a.answered();
        a.resume();
        assert_eq!(a.count(), 1);
        assert_eq!(a.last()["effort"], effort);
        assert_eq!(a.last()["resume_session"], session);
        assert_eq!(a.last()["sandbox"], "read-only");
        assert_eq!(a.last()["cwd"], a.cwd.to_string_lossy().as_ref());
    }
}
#[test]
fn test_full_thread_is_paginated_and_included() {
    let mut a = AnswerResume::new();
    for i in 0..205 {
        a.fake
            .borrow_mut()
            .messages
            .get_mut("200")
            .unwrap()
            .push(message(
                &(201 + i).to_string(),
                &format!("古い補足{i}"),
                "BOT",
            ));
    }
    let newest = a.answered();
    a.resume();
    let launch = a.last();
    let prompt = s(&launch["prompt"]);
    assert!(prompt.contains("古い補足0\n"));
    assert!(prompt.contains("古い補足204\n"));
    assert_eq!(prompt.matches("古い補足0\n").count(), 1);
    assert_eq!(a.question()["seen_reply"], newest);
}

#[test]
fn every_discord_resume_launcher_applies_policy_before_passing_a_native_session() {
    for (origin, expected_model, expected_effort, expected_session) in [
        ("policy", "gpt-6.1-sol", "max", ""),
        ("owner_request", "gpt-6-luna", "high", "original-session"),
    ] {
        let mut a = AnswerResume::new();
        let cwd = a.cwd.to_string_lossy().into_owned();
        let source = json!({"model":"gpt-6-luna","effort":"high","model_origin":origin,
            "effort_origin":origin,"task_type":"implementation","sandbox":"read-only",
            "scope_source":"launcher","cwd":cwd,"thread_id":"original-session"});
        launch_job_with_session(&mut a.ctx, &json!({"id":"200","name":"再開検査"}),
            "元の仕様と途中成果から続行する。", &source,
            "0123456789abcdef0123456789abcdef", false, Some(&cwd), Some("resume"),
            Some("original-session")).unwrap();
        assert_eq!(a.count(), 1);
        assert_eq!(a.last()["model"], expected_model);
        assert_eq!(a.last()["effort"], expected_effort);
        assert_eq!(a.last()["resume_session"], expected_session);
        assert_eq!(a.last()["sandbox"], "read-only");
        assert_eq!(a.last()["cwd"], cwd);
    }
}

#[test]
fn answer_reselection_compares_with_the_original_worker_instead_of_the_cached_selection() {
    for (origin, expected_effort, expected_session) in [
        ("policy", "max", ""),
        ("owner_request", "high", "original-session"),
    ] {
        let mut a = AnswerResume::new();
        a.change(json!({"session_id":"original-session","job_info":{
            "model":"gpt-6.1-sol","effort":"high","effort_origin":origin,
            "thread_id":"original-session","sandbox":"read-only","scope_source":"launcher"}}));
        a.answered();
        a.fake.borrow_mut().crash_before_launch = true;
        assert!(catch_unwind(AssertUnwindSafe(|| a.resume())).is_err());
        assert_eq!(a.count(), 0);
        assert_eq!(
            a.question()["continuation"]["launch"]["selection"]["effort"],
            expected_effort
        );
        a.answer(0., "起動前の追加回答も同じ依頼へ反映する", "OWN");
        a.advance(601.);
        a.resume();
        assert_eq!(a.count(), 1);
        assert_eq!(a.last()["effort"], expected_effort);
        assert_eq!(a.last()["resume_session"], expected_session);
        assert_eq!(a.last()["sandbox"], "read-only");
    }
}

#[test]
fn completed_capacity_history_does_not_exempt_an_ordinary_discord_resume() {
    let mut a = AnswerResume::new();
    let cwd = a.cwd.to_string_lossy().into_owned();
    let source = json!({"id":"old-worker","status":"succeeded","model":"gpt-6-luna",
        "effort":"high","model_origin":"policy","effort_origin":"policy",
        "task_type":"implementation","sandbox":"read-only","scope_source":"launcher",
        "cwd":cwd,"thread_id":"original-session","capacity_wait_pending":false,
        "capacity_retry_count":1,"capacity_same_model_retry_count":2,
        "capacity_history":[{"kind":"same_model_account_retry","attempt":2}]});
    let saved = source.clone();
    launch_job_with_session(&mut a.ctx, &json!({"id":"200","name":"通常の再開検査"}),
        "元の仕様と途中成果から通常の作業を再開する。", &source,
        "0123456789abcdef0123456789abcdef", false, Some(&cwd), Some("resume"),
        Some("original-session")).unwrap();
    assert_eq!(a.count(), 1);
    assert_eq!(a.last()["model"], "gpt-6.1-sol");
    assert_eq!(a.last()["effort"], "max");
    assert_eq!(a.last()["resume_session"], "");
    assert_eq!(source, saved, "original retry budget and evidence must remain intact");
}
#[test]
fn test_doing_todo_updated_after_reply_without_live_worker_does_not_consume_answer() {
    for format in ["%+", "%Y-%m-%d %H:%M"] {
        let mut a = AnswerResume::new();
        let newest = a.answered();
        a.todo(0., "doing", "#3467 の実装を再開", None);
        let mut db = read_json(&a.ctx.todo).unwrap();
        db["items"][0]["updated"] = json!(chrono::DateTime::from_timestamp(
            a.fake.borrow().now as i64,
            0
        )
        .unwrap()
        .with_timezone(&chrono::Local)
        .format(format)
        .to_string());
        atomic_json(&a.ctx.todo, &db, 2).unwrap();
        a.resume();
        assert_eq!((a.count(), a.fake.borrow().picks.len()), (1, 1));
        assert_eq!(a.question()["seen_reply"], newest);
        assert_eq!(a.question()["open"], true);
        a.resume();
        assert_eq!(a.posts(), 1);
    }
}
#[test]
fn test_stale_doing_todo_does_not_suppress_launch() {
    let mut a = AnswerResume::new();
    a.answered();
    a.todo(1200., "doing", "#3467 の実装を再開", None);
    a.resume();
    assert_eq!(a.count(), 1);
}
#[test]
fn test_unrelated_todo_or_other_project_does_not_suppress_launch() {
    let mut a = AnswerResume::new();
    a.answered();
    a.todo(0., "doing", "#13467 の実装", None);
    a.resume();
    assert_eq!(a.count(), 1);
    assert!(!task_matches(
        &a.question(),
        &a.fake.borrow().messages["200"],
        "#34670"
    ));
    assert!(!in_project(
        &format!("{}-wt-other/branch", a.cwd.display()),
        a.cwd.to_str().unwrap()
    ));
}
#[test]
fn test_related_todo_in_other_repository_does_not_suppress_launch() {
    let mut a = AnswerResume::new();
    a.answered();
    a.todo(
        0.,
        "doing",
        "#3467 の実装を再開",
        Some(&a.root.path().join("elsewhere")),
    );
    a.resume();
    assert_eq!(a.count(), 1);
}
#[test]
fn test_blocked_todo_does_not_suppress_launch() {
    let mut a = AnswerResume::new();
    a.answered();
    a.todo(0., "block", "#3467 の実装を再開", None);
    a.resume();
    assert_eq!(a.count(), 1);
}
#[test]
fn test_running_job_in_same_repository_suppresses_launch() {
    let mut a = AnswerResume::new();
    let newest = a.answered();
    a.running(
        Some(&PathBuf::from(format!("{}-wt/branch", a.cwd.display()))),
        "Issue #3467 を再開",
        "running",
    );
    a.resume();
    assert_eq!((a.count(), a.fake.borrow().picks.len()), (0, 0));
    assert!(a.text().contains("ジョブ other"));
    assert!(a.question().get("seen_reply").is_none());
    assert_eq!(a.question()["deferred_reply"], newest);
    a.resume();
    assert_eq!(a.posts(), 2);
    a.finish("other", "succeeded", Some("状態: 完了"), None);
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.question()["seen_reply"], newest);
}
#[test]
fn test_running_unrelated_job_or_finished_related_job_does_not_suppress() {
    let mut a = AnswerResume::new();
    a.answered();
    a.running(None, "#7777 の別作業", "running");
    a.resume();
    assert_eq!(a.count(), 1);
    assert!(!task_matches(
        &a.question(),
        &a.fake.borrow().messages["200"],
        "unrelated work"
    ));
}
#[test]
fn test_finished_related_job_does_not_suppress() {
    let mut a = AnswerResume::new();
    a.answered();
    a.running(None, "Issue #3467 を再開", "succeeded");
    a.resume();
    assert_eq!(a.count(), 1);
}
#[test]
fn test_legacy_question_without_cwd_infers_the_project_without_asking_owner() {
    let mut a = AnswerResume::new();
    a.answered();
    a.change(json!({"cwd":""}));
    a.todo(0., "todo", "元の作業", Some(&a.cwd));
    a.change(json!({"project":"P"}));
    a.resume();
    a.resume();
    assert_eq!(
        (a.count(), a.fake.borrow().picks.len(), a.posts()),
        (1, 1, 1)
    );
    assert_eq!(
        a.fake.borrow().posts[0].1["allowed_mentions"],
        json!({"parse":[],"users":[],"replied_user":false})
    );
    assert_eq!(a.question()["open"], true);
    assert!(truth(&a.question()["cwd_inferred"]));
    assert_eq!(a.question()["cwd"], a.cwd.to_string_lossy().as_ref());
}
#[test]
fn test_main_repository_creates_isolated_worktree_from_origin_main() {
    let mut a = AnswerResume::new();
    let repo = a.repository();
    a.answered();
    a.resume();
    let launch = a.last();
    let key = s(&launch["launch_key"]);
    let target =
        PathBuf::from(format!("{}-wt", repo.display())).join(format!("reply-{}", &key[..12]));
    assert_eq!(launch["cwd"], target.to_string_lossy().as_ref());
    let calls = &a.fake.borrow().git_calls;
    assert!(calls.contains(&(
        repo.to_string_lossy().into(),
        vec!["fetch".into(), "origin".into(), "main".into()]
    )));
    assert!(calls.contains(&(
        repo.to_string_lossy().into(),
        vec![
            "worktree".into(),
            "add".into(),
            "-b".into(),
            format!("resume/reply-{}", &key[..12]),
            target.to_string_lossy().into(),
            "origin/main".into()
        ]
    )));
    assert_eq!(
        calls
            .iter()
            .filter(|(_, args)| args[0] == "worktree")
            .count(),
        1
    );
}
#[test]
fn test_existing_worktree_is_reused() {
    let mut a = AnswerResume::new();
    let repo = a.repository();
    let worktree = PathBuf::from(format!("{}-wt", repo.display())).join("original");
    std::fs::create_dir_all(&worktree).unwrap();
    std::fs::write(worktree.join(".git"), "mock gitdir").unwrap();
    a.fake.borrow_mut().git_roots.insert(
        worktree.to_string_lossy().into(),
        repo.to_string_lossy().into(),
    );
    a.change(json!({"cwd":worktree.to_string_lossy()}));
    a.answered();
    a.resume();
    assert_eq!(a.last()["cwd"], worktree.to_string_lossy().as_ref());
    assert!(!a
        .fake
        .borrow()
        .git_calls
        .iter()
        .any(|(_, args)| ["fetch", "worktree"].contains(&args[0].as_str())));
}
#[test]
fn test_launch_failure_preserves_attempt_key_and_does_not_mark_seen() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    let key = a.question()["continuation"]["launch"]["key"].clone();
    assert!(a.question().get("seen_reply").is_none());
    a.resume();
    assert_eq!(a.last()["launch_key"], key);
    assert_eq!(a.fake.borrow().picks.len(), 1);
    assert_eq!(a.question()["continuation"]["job"], "job2");
}
#[test]
fn test_crash_after_launch_adopts_existing_job_without_duplicate() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().crash_after_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| a.resume())).is_err());
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.question()["continuation"]["job"], "job1");
}
#[test]
fn test_consumed_reply_cancels_a_reserved_launch_that_has_not_started() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().crash_before_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| a.resume())).is_err());
    a.replies(Some("200"));
    a.resume();
    assert_eq!(a.count(), 0);
    assert!(a.question().get("continuation").is_none());
}
#[test]
fn test_reserved_launch_includes_followup_without_resetting_wait() {
    let mut a = AnswerResume::new();
    a.answer(1800., "回答", "OWN");
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    let key = a.question()["continuation"]["launch"]["key"].clone();
    let newest = a.answer(0., "追加回答に従って進めて", "OWN");
    a.resume();
    assert_eq!(a.count(), 2);
    a.advance(600.);
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.last()["launch_key"], key);
    assert!(s(&a.last()["prompt"]).contains("追加回答に従って進めて"));
    assert_eq!(a.question()["seen_reply"], newest);
}
#[test]
fn test_reserved_launch_rechecks_if_another_session_has_started() {
    let mut a = AnswerResume::new();
    let newest = a.answered();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    a.todo(0., "doing", "#3467 の実装を再開", None);
    a.running(None, "#3467 の実装を再開", "running");
    a.resume();
    assert_eq!(a.count(), 1);
    assert!(a.question().get("seen_reply").is_none());
    assert_eq!(a.question()["deferred_reply"], newest);
    assert!(a.question().get("continuation").is_none());
    assert!(a.text().contains("ジョブ other"));
}
#[test]
fn test_manually_closed_question_cancels_reserved_launch() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    a.change(json!({"open":false}));
    a.resume();
    assert_eq!(a.count(), 1);
    assert!(a.question().get("continuation").is_none());
}
#[test]
fn test_issue_in_other_context_does_not_match_question_with_a_specific_issue() {
    let mut a = AnswerResume::new();
    a.fake
        .borrow_mut()
        .messages
        .get_mut("200")
        .unwrap()
        .push(message("201", "関連する別のIssue #7777", "BOT"));
    a.answered();
    a.running(None, "関連Issue #7777 の別作業", "running");
    a.resume();
    assert_eq!(a.count(), 1);
}
#[test]
fn test_ack_delivery_failure_does_not_relaunch() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().post_error = true;
    a.resume();
    assert_eq!(a.count(), 0);
    assert_eq!(
        a.state()["answer_receipts"]
            .as_object()
            .unwrap()
            .values()
            .next()
            .unwrap()["posted"],
        false
    );
    a.resume();
    assert_eq!((a.count(), a.posts()), (1, 1));
    assert_eq!(
        a.state()["answer_receipts"]
            .as_object()
            .unwrap()
            .values()
            .next()
            .unwrap()["posted"],
        true
    );
}
#[test]
fn test_completed_job_posts_final_with_mention_and_closes_question() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.finish(
        "job1",
        "succeeded",
        Some("状態: 完了\n元の作業を再開して完了しました。"),
        None,
    );
    a.resume();
    assert_eq!(a.text().lines().next().unwrap(), "終わりました。");
    assert!(a.text().contains("元の作業を再開して完了しました。"));
    assert_eq!(
        a.fake.borrow().posts.last().unwrap().1["allowed_mentions"],
        json!({"parse":[],"users":["OWN"]})
    );
    assert_eq!(a.question()["open"], false);
    assert_eq!(
        a.fake.borrow().patches,
        vec![("200".into(), json!({"archived":true}))]
    );
    a.resume();
    assert_eq!((a.posts(), a.count()), (2, 1));
}
#[test]
fn snooze_defers_result_but_new_answer_is_received_and_delivered_immediately() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.answer(0., "再通知は2026/10/3 9時にして欲しい", "OWN");
    a.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n再通知を明日に延ばしました。設定確認結果を待っています。"),
        None,
    );
    let posts = a.posts();
    a.resume();
    assert_eq!(a.posts(), posts);
    assert_ne!(a.question()["continuation"]["reported"], true);
    assert_eq!(a.question()["open"], true);
    assert!(a.question().get("last_ping").is_none());
    assert_eq!(a.question()["remind_not_before"], 1790985600.);
    a.advance(3600.);
    cc_ask::commands::cmd_remind(&mut a.ctx).unwrap();
    assert_eq!(a.posts(), posts);
    a.answer(601., "顧客の設定を確認できました", "OWN");
    a.resume();
    assert_eq!((a.posts(), a.count()), (posts + 1, 2));
    a.fake.borrow_mut().now = 1790985600.;
    a.resume();
    a.resume();
    assert_eq!(a.count(), 2);
    assert!(s(&a.last()["prompt"]).contains("顧客の設定を確認できました"));
}

#[test]
fn postponement_only_is_silent_and_reminds_at_deadline_without_a_worker() {
    let mut a = AnswerResume::new();
    let id = a.answer(0., "再通知は明日の9時にして", "OWN");
    a.resume();
    let at = a.question()["remind_not_before"].as_f64().unwrap();
    assert_eq!(
        a.question()["remind_not_before_iso"],
        "2026-10-01T09:00:00+09:00"
    );
    assert_eq!(a.question()["remind_reply"], id);
    assert_eq!((a.count(), a.posts()), (0, 0));
    a.advance(601.);
    a.resume();
    a.replies(Some("200"));
    a.resume();
    assert_eq!((a.count(), a.posts()), (0, 0));
    assert_eq!(a.question()["remind_not_before"], at);
    a.fake.borrow_mut().now = at - 1.;
    cc_ask::commands::cmd_remind(&mut a.ctx).unwrap();
    assert_eq!(a.posts(), 0);
    a.fake.borrow_mut().now = at;
    a.resume();
    cc_ask::commands::cmd_remind(&mut a.ctx).unwrap();
    cc_ask::commands::cmd_remind(&mut a.ctx).unwrap();
    assert_eq!((a.count(), a.posts()), (0, 1));
}

#[test]
fn postponement_after_completion_holds_result_then_reports_exactly_once() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.finish_ok();
    a.answer(0., "再通知は1時間後にして", "OWN");
    let posts = a.posts();
    a.resume();
    assert_eq!(a.posts(), posts);
    assert!(a.fake.borrow().patches.is_empty());
    a.advance(3600.);
    a.resume();
    a.resume();
    assert_eq!(a.posts(), posts + 1);
    assert_eq!(a.question()["open"], false);
    assert_eq!(a.fake.borrow().patches.len(), 1);
}

#[test]
fn postponed_failed_worker_does_not_restart_or_post_until_release() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.answer(0., "再通知は2時間後にして", "OWN");
    a.finish("job1", "failed", None, None);
    let posts = a.posts();
    a.resume();
    a.advance(1.);
    a.answer(0., "再通知は今すぐにして", "OWN");
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.posts(), posts);
}

#[test]
fn mixed_answer_and_postponement_keeps_answer_and_releases_it_at_deadline() {
    let mut a = AnswerResume::new();
    a.answer(601., "aの案で実装して。\n再通知は1時間後にして", "OWN");
    a.resume();
    assert_eq!((a.count(), a.posts()), (0, 1));
    assert!(a.question().get("seen_reply").is_none());
    let at = a.question()["remind_not_before"].as_f64().unwrap();
    a.fake.borrow_mut().now = at;
    a.resume();
    assert_eq!((a.count(), a.posts()), (1, 1));
    assert!(s(&a.last()["prompt"]).contains("aの案で実装して。"));
    a.resume();
    assert_eq!((a.count(), a.posts()), (1, 1));
}

#[test]
fn test_needs_decision_stays_open_and_next_answer_can_resume() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n次の方針を選んでください。"),
        None,
    );
    a.resume();

    let state = a.state();
    assert_eq!(state["questions"][0]["open"], false);
    let next = state["questions"][1].clone();
    let qid = s(&next["thread_id"]).to_owned();
    assert_ne!(qid, "200");
    assert_eq!(next["open"], true);
    assert_eq!(next["work_thread_id"], "200");
    {
        let f = a.fake.borrow();
        let creations: Vec<_> = f
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads"))
            .collect();
        assert_eq!(creations.len(), 2);
        let starter = &creations[0]["body"]["message"];
        assert_eq!(
            starter["allowed_mentions"],
            json!({"parse":[],"users":["OWN"],"replied_user":false})
        );
        assert_eq!(starter["flags"], 4);
        assert_eq!(s(&starter["content"]).matches("<@OWN>").count(), 1);
        assert_eq!(creations[1]["body"]["message"]["flags"], SILENT | 4);
    }
    assert_eq!(next["after"], a.fake.borrow().messages[&qid][0]["id"]);
    assert!(!a.fake.borrow().patches.iter().any(|(tid, _)| tid == &qid));
    let previous = a.last();
    a.advance(601.);
    let reply = a.h.reply("推奨案で進めて", &qid);
    a.advance(600.);
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.last()["resume_session"], "session-job1");
    assert_eq!(a.last()["cwd"], previous["cwd"]);
    assert!(s(&a.last()["prompt"]).contains("推奨案で進めて"));
    assert_eq!(a.state()["answer_receipts"][&reply]["handled_by"], "job2");
}
#[test]
fn test_failed_job_resumes_automatically_without_owner_mention() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    let posts = a.posts();
    a.finish("job1", "failed", None, None);
    a.resume();
    assert_eq!(a.posts(), posts);
    assert_eq!(a.count(), 2);
    assert_eq!(a.last()["resume_session"], "session-job1");
    assert_eq!(a.question()["open"], true);
    assert!(a.fake.borrow().patches.is_empty());
}

#[test]
fn failed_writer_conflict_waits_without_consuming_answer_then_recovers_fresh() {
    let mut a = AnswerResume::new();
    let answer = a.answered();
    a.resume();
    let posts = a.posts();
    a.finish(
        "job1",
        "failed",
        None,
        Some("codex exited with 1 without turn.completed"),
    );
    std::fs::write(
        a.ctx.jobs.join("jobs/job1/stderr.log"),
        "thread-store conflict: thread session-job1 already has an active writer",
    )
    .unwrap();
    let dir = a.ctx.jobs.join("jobs/other-writer");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"running","discord_thread_id":"200","resume_session":"session-job1"}),
        2,
    )
    .unwrap();
    a.resume();
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.posts(), posts);
    assert_ne!(a.question()["continuation"]["reported"], true);
    assert_eq!(a.question()["restart"]["waiting_for"], "other-writer");
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"failed","resume_session":"session-job1"}),
        2,
    )
    .unwrap();
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.last()["resume_session"], "");
    assert_eq!(a.state()["answer_receipts"][&answer]["handled_by"], "job2");
    a.resume();
    assert_eq!(a.count(), 2);
}

#[test]
fn successful_manual_writer_is_not_restarted_or_reported_twice() {
    let mut a = AnswerResume::new();
    let answer = a.answered();
    a.resume();
    let posts = a.posts();
    a.finish(
        "job1",
        "failed",
        None,
        Some("thread already has an active writer"),
    );
    let dir = a.ctx.jobs.join("jobs/manual-writer");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"running","discord_thread_id":"200","resume_session":"session-job1"}),
        2,
    )
    .unwrap();
    a.resume();
    assert_eq!(a.question()["restart"]["waiting_for"], "manual-writer");
    std::fs::copy(
        a.ctx.jobs.join("jobs/job1/prompt.md"),
        dir.join("prompt.md"),
    )
    .unwrap();
    std::fs::write(
        dir.join("prompt.md"),
        format!("オーナー投稿 {answer}\n調査して最適案を選んで実装"),
    )
    .unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"succeeded","steps":1,"resume_session":"session-job1"}),
        2,
    )
    .unwrap();
    a.resume();
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.posts(), posts);
    assert_eq!(a.question()["open"], false);
    assert_eq!(a.question()["restart"]["transferred_to"], "manual-writer");
    assert_eq!(
        a.state()["answer_receipts"][answer]["handled_by"],
        "manual-writer"
    );
}

#[test]
fn reply_arriving_during_manual_writer_is_followed_up_after_success() {
    let mut a = AnswerResume::new();
    let first = a.answered();
    a.resume();
    a.finish(
        "job1",
        "failed",
        None,
        Some("thread already has an active writer"),
    );
    let dir = a.ctx.jobs.join("jobs/manual-writer");
    std::fs::create_dir_all(&dir).unwrap();
    let started = chrono::DateTime::from_timestamp(a.ctx.now() as i64, 0)
        .unwrap()
        .to_rfc3339();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"running","discord_thread_id":"200","resume_session":"session-job1","started_at":started}),
        2,
    )
    .unwrap();
    a.resume();
    a.advance(60.);
    std::fs::copy(
        a.ctx.jobs.join("jobs/job1/prompt.md"),
        dir.join("prompt.md"),
    )
    .unwrap();
    let pending = a.answer(0., "実行中に届いた新しい回答を反映する", "OWN");
    std::fs::write(
        dir.join("prompt.md"),
        format!("オーナー投稿 {first}\n調査して最適案を選んで実装"),
    )
    .unwrap();
    a.resume();
    assert_eq!(a.count(), 1);
    atomic_json(&dir.join("job.json"), &json!({"status":"succeeded","steps":1,"resume_session":"session-job1","started_at":started,
        "finished_at":chrono::DateTime::from_timestamp(a.ctx.now() as i64 + 60, 0).unwrap().to_rfc3339()}), 2).unwrap();
    a.resume();
    assert!(!truth(
        &a.state()["answer_receipts"][&pending]["handled_by"]
    ));
    assert_eq!(a.question()["open"], true);
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.last()["resume_session"], "session-job1");
    assert!(s(&a.last()["prompt"]).contains("実行中に届いた新しい回答を反映する"));
    assert_eq!(a.state()["answer_receipts"][pending]["handled_by"], "job2");
    a.resume();
    assert_eq!(a.count(), 2);
}

#[test]
fn duplicate_or_superseded_writer_keeps_a_failed_jobs_answer_dispatchable() {
    for reason in ["duplicate", "superseded"] {
        let mut a = AnswerResume::new();
        let answer = a.answered();
        a.resume();
        a.finish(
            "job1",
            "failed",
            None,
            Some("thread already has an active writer"),
        );
        let dir = a.ctx.jobs.join("jobs/manual-writer");
        std::fs::create_dir_all(&dir).unwrap();
        let started = chrono::DateTime::from_timestamp(a.ctx.now() as i64 - 1200, 0)
            .unwrap()
            .to_rfc3339();
        atomic_json(
            &dir.join("job.json"),
            &json!({"status":"running","discord_thread_id":"200","resume_session":"session-job1","started_at":started}),
            2,
        )
        .unwrap();
        a.resume();
        atomic_json(&dir.join("job.json"), &json!({"status":"cancelled","cancel_requested":true,"cancel_reason":reason,"resume_session":"session-job1","started_at":started}), 2).unwrap();
        a.resume();
        assert!(n(&a.question()["seen_reply"]) < answer.parse::<u64>().unwrap());
        assert!(!truth(&a.state()["answer_receipts"][&answer]["handled_by"]));
        a.resume();
        assert_eq!(a.count(), 2);
        assert_eq!(a.state()["answer_receipts"][&answer]["handled_by"], "job2");
        a.resume();
        assert_eq!(a.count(), 2);
    }
}

#[test]
fn test_continuation_backoff_and_intentional_duplicate_cancel_are_silent() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    let posts = a.posts();
    a.finish("job1", "failed", None, None);
    a.resume();
    a.finish("job2", "lost", None, None);
    a.resume();
    assert_eq!(a.count(), 2);
    a.advance(60.);
    a.resume();
    assert_eq!(a.count(), 3);
    a.finish("job3", "succeeded", Some("  "), None);
    a.resume();
    a.resume();
    assert_eq!(a.count(), 3);
    assert_eq!(a.posts(), posts + 1);
    let f = a.fake.borrow();
    assert_eq!(f.posts.last().unwrap().1["flags"], SILENT);
    assert_eq!(
        f.posts.last().unwrap().1["allowed_mentions"]["users"],
        json!([])
    );
    drop(f);

    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    let posts = a.posts();
    a.finish("job1", "cancelled", None, None);
    let dir = a.ctx.jobs.join("jobs/other");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"running","discord_thread_id":"200","resume_session":"session-job1"}),
        2,
    )
    .unwrap();
    a.resume();
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.posts(), posts);
    assert_eq!(a.question()["open"], false);
}

#[test]
fn test_initial_continuation_launch_failure_enters_bounded_recovery() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    assert_eq!(a.question()["continuation"]["job"], "launch-failed");
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.question()["continuation"]["job"], "job2");
    assert_eq!(
        a.question()["restart"]["attempts"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert_eq!(a.posts(), 1); // only the silent answer receipt
}

#[test]
fn test_own_recovery_todo_does_not_block_retry_and_close_disables_watchdog_item() {
    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    assert_eq!(a.count(), 2);
    a.advance(60.);
    a.resume();
    assert_eq!(a.count(), 3);

    let mut a = AnswerResume::new();
    a.answered();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    a.fake.borrow_mut().launch_error = true;
    a.resume();
    cc_ask::commands::cmd_close(&mut a.ctx, "200").unwrap();
    let posts = a.posts();
    a.advance(3600.);
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.posts(), posts);
    assert_eq!(
        read_json(&a.ctx.todo).unwrap()["items"][0]["status"],
        "done"
    );
}
#[test]
fn test_new_owner_reply_during_job_prevents_close_and_is_not_lost() {
    let mut a = AnswerResume::new();
    a.answer(1200., "回答", "OWN");
    a.resume();
    let newest = a.answer(601., "追加作業もお願い", "OWN");
    a.finish_ok();
    a.resume();
    assert_eq!(a.question()["open"], true);
    assert!(a.fake.borrow().patches.is_empty());
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.question()["seen_reply"], newest);
}
#[test]
fn test_new_owner_reply_is_preserved_when_job_needs_decision() {
    let mut a = AnswerResume::new();
    a.answer(1200., "回答", "OWN");
    a.resume();
    let newest = a.answer(601., "次の方針も説明済みです", "OWN");
    a.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n次の方針を教えてください。"),
        None,
    );
    a.resume();
    assert_eq!(a.question()["continuation"]["after"], newest);
    assert_eq!(a.count(), 2); // follow-up launches in the completion pass
    assert!(!a
        .fake
        .borrow()
        .posts
        .iter()
        .any(|(_, p)| s(&p["content"]).contains("次の方針を教えてください。")));
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.question()["seen_reply"], newest);
}
struct FailQuestionStateSave {
    fake: std::rc::Rc<std::cell::RefCell<common::Fake>>,
    state_path: PathBuf,
    failed: bool,
}
impl Effects for FailQuestionStateSave {
    fn now(&self) -> f64 {
        self.fake.borrow().now
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.fake.borrow_mut().run(argv, timeout)
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        let response = self.fake.borrow_mut().http(method, url, body, discord)?;
        if !self.failed && method == "POST" && url.ends_with("/channels/T/threads") {
            self.failed = true;
            // Fail installation after Discord accepted the starter, while its ID is durable.
            std::fs::write(&self.state_path, "{").unwrap();
        }
        Ok(response)
    }
}
#[test]
fn test_question_registration_retries_after_state_save_failure_without_reposting() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n次の方針を教えてください。"),
        None,
    );
    let state_path = a.ctx.here.join("state.json");
    let saved = std::fs::read(&state_path).unwrap();
    a.ctx.effects = Box::new(FailQuestionStateSave {
        fake: a.fake.clone(),
        state_path: state_path.clone(),
        failed: false,
    });
    assert!(cmd_resume(&mut a.ctx, &mut false).is_err());
    assert_eq!(std::fs::read(&state_path).unwrap(), b"{");
    let records: Vec<_> = std::fs::read_dir(a.ctx.here.join("question-progress"))
        .unwrap()
        .map(|e| read_json(&e.unwrap().path()).unwrap())
        .collect();
    assert_eq!(records.len(), 1);
    let qid = s(&records[0]["question"]["starter"]).to_owned();
    assert!(!qid.is_empty());
    assert_eq!(
        a.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads"))
            .count(),
        1
    );
    std::fs::write(&state_path, saved).unwrap();
    a.resume();
    a.resume();
    assert_eq!(
        a.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads"))
            .count(),
        2
    );
    let state = a.state();
    assert_eq!(state["questions"].as_array().unwrap().len(), 2);
    assert_eq!(state["questions"][0]["open"], false);
    assert_eq!(state["questions"][1]["thread_id"], qid);
    assert_eq!(state["questions"][1]["open"], true);
    let count = a.posts();
    a.resume();
    assert_eq!(a.posts(), count);
}
#[test]
fn test_result_delivery_failure_retries_and_keeps_question_open() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.finish_ok();
    a.fake.borrow_mut().post_error = true;
    a.resume();
    assert_eq!(a.question()["open"], true);
    assert!(!truth(&a.question()["continuation"]["reported"]));
    a.resume();
    assert_eq!(a.question()["open"], false);
    assert_eq!((a.count(), a.posts()), (1, 2));
}
#[test]
fn test_archive_failure_retries_without_reposting_result() {
    let mut a = AnswerResume::new();
    a.answered();
    a.resume();
    a.finish_ok();
    a.fake.borrow_mut().archive_error = true;
    a.resume();
    let count = a.posts();
    assert_eq!(a.question()["continuation"]["close_pending"], true);
    a.resume();
    assert_eq!(a.posts(), count);
    assert_eq!(a.question()["open"], false);
}
#[test]
fn test_tick_shares_one_launch_budget_with_intake() {
    let mut a = AnswerResume::new();
    a.answered();
    let mut budget = false;
    cmd_resume(&mut a.ctx, &mut budget).unwrap();
    cc_ask::intake::cmd_intake(&mut a.ctx, &mut budget).unwrap();
    assert_eq!(a.count(), 1);
    let mut budget = false;
    cmd_resume(&mut a.ctx, &mut budget).unwrap();
    cc_ask::intake::cmd_intake(&mut a.ctx, &mut budget).unwrap();
    assert_eq!(a.count(), 2);
    assert_eq!(
        a.fake
            .borrow()
            .launches
            .iter()
            .map(|j| s(&j["name"]))
            .collect::<Vec<_>>(),
        vec!["resume-200", "req-100"]
    );
}
#[test]
fn test_request_window_questions_are_left_to_existing_intake() {
    let mut a = AnswerResume::new();
    a.answered();
    a.change(json!({"source":"request","cwd":""}));
    a.resume();
    assert_eq!(
        (a.count(), a.posts(), a.fake.borrow().picks.len()),
        (0, 1, 0)
    );
}
#[test]
fn test_secrets_in_context_and_replies_are_redacted() {
    let mut a = AnswerResume::new();
    a.answer(601., "api_key=mock-sensitive-value", "OWN");
    let returned = a.replies(Some("200")).to_string();
    assert!(!returned.contains("mock-sensitive-value"));
    assert!(returned.contains("[秘密]"));
}

struct GitEffects {
    fake: SharedFake,
    fail_fetch: bool,
    real: bool,
}

#[test]
fn no_origin_repository_creates_and_reuses_worktree_from_committed_head() {
    let mut a = AnswerResume::new();
    let repo = a.root.path().join("no-origin");
    std::fs::create_dir(&repo).unwrap();
    fixture_git(&repo, &["init", "-b", "main"]);
    std::fs::write(repo.join("fixture.txt"), "committed content").unwrap();
    fixture_git(&repo, &["add", "fixture.txt"]);
    let hooks = a.root.path().join("empty-hooks");
    std::fs::create_dir(&hooks).unwrap();
    fixture_git(
        &repo,
        &[
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "-c",
            "commit.gpgsign=false",
            "-c",
            &format!("core.hooksPath={}", hooks.display()),
            "commit",
            "-m",
            "fixture",
        ],
    );
    a.ctx.effects = Box::new(GitEffects {
        fake: SharedFake(a.fake.clone()),
        fail_fetch: false,
        real: true,
    });
    let route = continuation_route(&mut a.ctx, repo.to_str().unwrap()).unwrap();
    let target = prepare_continuation_cwd(&mut a.ctx, &route, "1742-no-origin").unwrap();
    assert_eq!(
        std::fs::read_to_string(Path::new(&target).join("fixture.txt")).unwrap(),
        "committed content"
    );
    assert_eq!(
        fixture_git(Path::new(&target), &["rev-parse", "HEAD"]).stdout,
        fixture_git(&repo, &["rev-parse", "HEAD"]).stdout
    );
    assert_eq!(
        prepare_continuation_cwd(&mut a.ctx, &route, "1742-no-origin").unwrap(),
        target
    );
    // A failed attempt can leave its branch after its worktree directory is removed.
    fixture_git(&repo, &["worktree", "remove", &target]);
    assert_eq!(
        prepare_continuation_cwd(&mut a.ctx, &route, "1742-no-origin").unwrap(),
        target
    );
}
impl Effects for GitEffects {
    fn now(&self) -> f64 {
        self.fake.now()
    }
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        self.fake.http(m, u, b, d)
    }
    fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
        if args[0] == "git" {
            if self.fail_fetch && args.get(3).is_some_and(|a| a == "fetch") {
                self.fail_fetch = false;
                return Ok(Output {
                    code: 1,
                    stderr: "mock fetch failed".into(),
                    ..Default::default()
                });
            }
            if self.real {
                let mut isolated = vec![args[0].clone()];
                isolated.extend(fixture_git_options());
                isolated.extend_from_slice(&args[1..]);
                return run_hidden(&isolated, timeout);
            }
        }
        self.fake.run(args, timeout)
    }
}
#[test]
fn test_worktree_failure_consumes_tick_launch_budget_and_preserves_reply() {
    let mut a = AnswerResume::new();
    a.repository();
    let newest = a.answered();
    a.ctx.effects = Box::new(GitEffects {
        fake: SharedFake(a.fake.clone()),
        fail_fetch: true,
        real: false,
    });
    let mut budget = false;
    cmd_resume(&mut a.ctx, &mut budget).unwrap();
    assert!(budget);
    cc_ask::intake::cmd_intake(&mut a.ctx, &mut budget).unwrap();
    assert_eq!(a.count(), 0);
    assert!(a.question().get("seen_reply").is_none());
    assert!(a.question()["continuation"].get("launch").is_some());
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.question()["seen_reply"], newest);
}

fn fixture_git_options() -> Vec<String> {
    [
        "-c",
        "filter.lfs.process=",
        "-c",
        "filter.lfs.smudge=",
        "-c",
        "filter.lfs.clean=",
        "-c",
        "filter.lfs.required=false",
        "-c",
        "core.fsmonitor=false",
        "-c",
        "core.hooksPath=",
    ]
    .iter()
    .map(|s| s.to_string())
    .collect()
}

fn fixture_git(cwd: &Path, args: &[&str]) -> Output {
    let argv = std::iter::once("git".into())
        .chain(fixture_git_options())
        .chain(["-C".into(), cwd.to_string_lossy().into()])
        .chain(args.iter().map(|a| a.to_string()))
        .collect::<Vec<_>>();
    let output = run_hidden(&argv, 45).unwrap();
    assert_eq!(output.code, 0, "{}", output.stderr);
    output
}
/// Use a local bundle transport, which needs no POSIX shell; all external services remain mocked.
#[test]
fn test_git_worktree_setup_and_recovery_with_real_local_repository() {
    let mut a = AnswerResume::new();
    let source = a.root.path().join("origin");
    std::fs::create_dir(&source).unwrap();
    fixture_git(&source, &["init", "-b", "main"]);
    std::fs::create_dir(source.join("subdir")).unwrap();
    std::fs::write(source.join("subdir/fixture.txt"), "fixture").unwrap();
    fixture_git(&source, &["add", "subdir/fixture.txt"]);
    let hooks = a.root.path().join("empty-hooks");
    std::fs::create_dir(&hooks).unwrap();
    fixture_git(
        &source,
        &[
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "-c",
            "commit.gpgsign=false",
            "-c",
            &format!("core.hooksPath={}", hooks.display()),
            "commit",
            "-m",
            "fixture",
        ],
    );
    let bundle = a.root.path().join("origin.bundle");
    fixture_git(
        &source,
        &["bundle", "create", bundle.to_str().unwrap(), "--all"],
    );
    let repo = a.root.path().join("clone");
    fixture_git(
        a.root.path(),
        &[
            "clone",
            "--no-hardlinks",
            bundle.to_str().unwrap(),
            repo.to_str().unwrap(),
        ],
    );
    a.change(json!({"cwd":repo.join("subdir").to_string_lossy()}));
    a.answered();
    a.fake.borrow_mut().launch_error = true;
    a.ctx.effects = Box::new(GitEffects {
        fake: SharedFake(a.fake.clone()),
        fail_fetch: false,
        real: true,
    });
    a.resume();
    assert_eq!(a.count(), 1);
    let key = s(&a.question()["continuation"]["launch"]["key"]).to_string();
    a.resume();
    let target =
        PathBuf::from(format!("{}-wt", repo.display())).join(format!("reply-{}", &key[..12]));
    assert_eq!(
        a.fake
            .borrow()
            .launches
            .iter()
            .map(|j| s(&j["cwd"]).to_string())
            .collect::<Vec<_>>(),
        vec![target.join("subdir").to_string_lossy().to_string(); 2]
    );
    assert_eq!(
        a.fake
            .borrow()
            .launches
            .iter()
            .map(|j| s(&j["launch_key"]).to_string())
            .collect::<Vec<_>>(),
        vec![key.clone(); 2]
    );
    assert_eq!(
        fixture_git(&target, &["rev-parse", "HEAD"]).stdout,
        fixture_git(&repo, &["rev-parse", "origin/main"]).stdout
    );
    assert_eq!(
        fixture_git(&target, &["branch", "--show-current"])
            .stdout
            .trim(),
        format!("resume/reply-{}", &key[..12])
    );
    assert_eq!(
        fixture_git(&repo, &["branch", "--show-current"])
            .stdout
            .trim(),
        "main"
    );
    assert_eq!(a.question()["continuation"]["job"], "job2");
}

#[test]
fn occupied_unrelated_session_uses_fresh_answer_worker_once() {
    let mut a = AnswerResume::new();
    a.change(json!({"session_id":"original-session","job_info":{"model":"gpt-6.1-sol","effort":"high","thread_id":"original-session","sandbox":"read-only","scope_source":"launcher"}}));
    let dir = a.ctx.jobs.join("jobs/unrelated-writer");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"), &json!({"status":"running","thread_id":"original-session","discord_thread_id":"300","cwd":a.cwd}), 2).unwrap();
    let answer = a.answered();
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(a.last()["resume_session"], "");
    assert_eq!(a.last()["sandbox"], "read-only");
    assert!(s(&a.last()["prompt"]).contains("調査して最適案を選んで実装"));
    assert_eq!(job_info(&a.ctx, "unrelated-writer")["status"], "running");
    assert_eq!(a.state()["answer_receipts"][&answer]["handled_by"], "job1");
    a.resume();
    assert_eq!(a.count(), 1);
}

#[test]
fn queued_answer_is_reserved_until_start_then_unblocks_existing_todo() {
    let mut a = AnswerResume::new();
    a.change(json!({"job":"original","session_id":"original-session"}));
    let dir = a.ctx.jobs.join("jobs/original");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"), &json!({"id":"original","status":"succeeded","model":"gpt-6.1-sol","effort":"high","thread_id":"original-session","cwd":a.cwd}),2).unwrap();
    a.todo(800., "block", "承認待ち", None);
    let mut db = read_json(&a.ctx.todo).unwrap();
    db["items"][0]["jobs"] = json!(["original"]);
    atomic_json(&a.ctx.todo, &db, 2).unwrap();
    a.fake.borrow_mut().launch_queued = true;
    let answer = a.answered();
    a.resume();
    assert_eq!(a.count(), 1);
    assert!(!reply_delivered(&a.state(), &answer));
    assert!(reply_reserved(&a.state(), &answer));
    assert!(n(&a.question()["seen_reply"]) < answer.parse::<u64>().unwrap());
    a.resume();
    assert_eq!(a.count(), 1);
    assert_eq!(
        read_json(&a.ctx.todo).unwrap()["items"][0]["status"],
        "block"
    );
    let path = a.ctx.jobs.join("jobs/job1/job.json");
    let mut job = read_json(&path).unwrap();
    job["status"] = json!("running");
    job["activity"] = json!("thinking");
    atomic_json(&path, &job, 2).unwrap();
    a.resume();
    assert!(!reply_delivered(&a.state(), &answer));
    assert!(reply_reserved(&a.state(), &answer));
    assert_eq!(
        read_json(&a.ctx.todo).unwrap()["items"][0]["status"],
        "block"
    );
    std::fs::write(path.with_file_name("events.jsonl"), "{\"type\":\"item.started\",\"item\":{\"type\":\"command_execution\",\"command\":\"work\"}}\n").unwrap();
    a.resume();
    assert!(reply_delivered(&a.state(), &answer));
    assert!(!reply_reserved(&a.state(), &answer));
    assert_eq!(a.question()["seen_reply"], answer);
    let db = read_json(&a.ctx.todo).unwrap();
    assert_eq!(db["items"].as_array().unwrap().len(), 1);
    assert_eq!(db["items"][0]["status"], "doing");
    assert!(db["items"][0]["jobs"]
        .as_array()
        .unwrap()
        .contains(&json!("job1")));
}

#[test]
fn zero_step_writer_failure_does_not_consume_answer_or_wait_for_unrelated_writer() {
    let mut a = AnswerResume::new();
    a.fake.borrow_mut().launch_queued = true;
    let answer = a.answered();
    a.resume();
    a.finish(
        "job1",
        "failed",
        None,
        Some("thread-store conflict: already has an active writer"),
    );
    // Reproduce the previous version's premature delivery receipt.
    let mut state = a.state();
    state["answer_receipts"][&answer]
        .as_object_mut()
        .unwrap()
        .remove("launch_job");
    state["answer_receipts"][&answer]["handled_by"] = json!("job1");
    a.save(&state);
    let dir = a.ctx.jobs.join("jobs/unrelated");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"running","resume_session":"session-job1","discord_thread_id":"300"}),
        2,
    )
    .unwrap();
    a.resume();
    assert_eq!(a.count(), 2);
    assert_eq!(a.last()["resume_session"], "");
    assert!(!reply_delivered(&a.state(), &answer));
    assert_eq!(a.state()["answer_receipts"][&answer]["launch_job"], "job2");
    assert_eq!(job_info(&a.ctx, "unrelated")["status"], "running");
    a.resume();
    assert_eq!(a.count(), 2);
}

#[test]
fn legacy_queued_receipt_is_reserved_and_unreadable_job_cannot_release_it() {
    let mut a = AnswerResume::new();
    a.fake.borrow_mut().launch_queued = true;
    let answer = a.answered();
    a.resume();
    let mut state = a.state();
    state["answer_receipts"][&answer]
        .as_object_mut()
        .unwrap()
        .remove("launch_job");
    state["answer_receipts"][&answer]["handled_by"] = json!("job1");
    a.save(&state);
    a.resume();
    assert!(!reply_delivered(&a.state(), &answer));
    assert_eq!(a.state()["answer_receipts"][&answer]["launch_job"], "job1");
    std::fs::write(a.ctx.jobs.join("jobs/job1/job.json"), "{").unwrap();
    assert!(cmd_resume(&mut a.ctx, &mut false).is_err());
    assert_eq!(a.count(), 1);
    assert_eq!(a.state()["answer_receipts"][&answer]["launch_job"], "job1");
}

#[test]
fn same_task_writer_success_without_answer_input_does_not_consume_it() {
    let mut a = AnswerResume::new();
    a.fake.borrow_mut().launch_queued = true;
    let answer = a.answered();
    a.resume();
    a.finish("job1", "failed", None, Some("already has an active writer"));
    let dir = a.ctx.jobs.join("jobs/manual-writer");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"),&json!({"status":"running","discord_thread_id":"200","thread_id":"session-job1","model":"gpt-6.1-sol","effort":"high","cwd":a.cwd}),2).unwrap();
    a.resume();
    assert_eq!(a.count(), 1);
    atomic_json(&dir.join("job.json"),&json!({"status":"succeeded","steps":1,"thread_id":"session-job1","model":"gpt-6.1-sol","effort":"high","cwd":a.cwd}),2).unwrap();
    a.resume();
    assert!(!reply_delivered(&a.state(), &answer));
    assert_eq!(a.question()["open"], true);
    a.resume();
    assert_eq!(a.count(), 2);
    assert!(reply_reserved(&a.state(), &answer));
    a.resume();
    assert_eq!(a.count(), 2);
}

#[test]
fn local_start_events_do_not_prove_input_but_dispatched_work_does() {
    let a = AnswerResume::new();
    let dir = a.ctx.jobs.join("jobs/queued");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"),&json!({"status":"running","steps":0,"thread_id":"some-session","activity":"running command"}),2).unwrap();
    std::fs::write(
        dir.join("events.jsonl"),
        "{\"type\":\"thread.started\",\"thread_id\":\"some-session\"}\n",
    )
    .unwrap();
    assert!(!job_started(&a.ctx, "queued"));
    std::fs::write(dir.join("events.jsonl"),"{\"type\":\"thread.started\",\"thread_id\":\"some-session\"}\n{\"type\":\"turn.started\"}\n{unfinished").unwrap();
    assert!(!job_started(&a.ctx, "queued"));
    std::fs::write(dir.join("events.jsonl"), "{\"type\":\"turn.started\"}\n{\"type\":\"item.started\",\"item\":{\"type\":\"command_execution\",\"command\":\"work\"}}\n").unwrap();
    assert!(job_started(&a.ctx, "queued"));
}

#[test]
fn prestart_failure_releases_legacy_receipt_but_explicit_delivery_is_monotonic() {
    for (reason, via, confirmed, expected) in [
        ("app-server init failed", "", false, false),
        ("model not found", "", false, false),
        ("app-server init failed", "replies-event", false, true),
        (
            "exceeded retry limit, last status: 429 Too Many Requests",
            "",
            true,
            false,
        ),
        ("app-server init failed", "manual-confirmation", true, true),
    ] {
        let a = AnswerResume::new();
        let dir = a.ctx.jobs.join("jobs/legacy");
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(
            &dir.join("job.json"),
            &json!({"status":"failed","steps":0,"last_error":reason}),
            2,
        )
        .unwrap();
        let mut state = a.state();
        state["answer_receipts"]["501"] = json!({"handled_by":"legacy","thread":"200","handled_via":via,"launch_confirmed":confirmed});
        a.save(&state);
        recover_reply_reads(&a.ctx, &mut state).unwrap();
        assert_eq!(
            reply_delivered(&state, "501"),
            expected,
            "{reason}/{via}/{confirmed}"
        );
        assert_eq!(reply_delivered(&a.state(), "501"), expected, "persisted");
    }
}

#[test]
fn meaningful_event_before_split_utf8_boundary_keeps_legacy_delivery_confirmed() {
    let a = AnswerResume::new();
    let dir = a.ctx.jobs.join("jobs/legacy");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"failed","steps":0,"activity":"failed"}),
        2,
    )
    .unwrap();
    let mut events = b"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"working\"}}\n".to_vec();
    events.resize(65535, b' ');
    events.extend_from_slice("あ\n".as_bytes());
    std::fs::write(dir.join("events.jsonl"), events).unwrap();
    assert!(job_started(&a.ctx, "legacy"));
    let mut state = a.state();
    state["answer_receipts"]["502"] = json!({"handled_by":"legacy","thread":"200"});
    recover_reply_reads(&a.ctx, &mut state).unwrap();
    assert!(reply_delivered(&state, "502"));
    assert_eq!(state["answer_receipts"]["502"]["launch_confirmed"], true);
}

#[test]
fn zero_step_429_releases_false_confirmation_without_reposting_or_changing_waits() {
    let a = AnswerResume::new();
    let dir = a.ctx.jobs.join("jobs/failed-launch");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"failed","steps":0,
        "last_error":"exceeded retry limit, last status: 429 Too Many Requests"}),
        2,
    )
    .unwrap();
    std::fs::write(
        dir.join("events.jsonl"),
        "{\"type\":\"thread.started\"}\n{\"type\":\"turn.started\"}\n{\"type\":\"turn.failed\"}\n",
    )
    .unwrap();
    let todo = json!({"items":[{"id":2073,"status":"wait","wait_on":[32]}]});
    atomic_json(&a.ctx.todo, &todo, 2).unwrap();
    let todo_bytes = std::fs::read(&a.ctx.todo).unwrap();
    let mut state = a.state();
    state["questions"] = json!([
        {"thread_id":"200","after":"200","seen_reply":"502","open":true,
            "continuation":{"job":"failed-launch","after":"502","job_info":{"status":"queued"},
                "steering_messages":[{"id":"501","content":"first"},{"id":"502","content":"second"}]}},
        {"thread_id":"200","after":"600","seen_reply":"700","open":true,
            "continuation":{"job":"failed-launch","after":"700"}},
        {"thread_id":"200","after":"200","seen_reply":"502","open":false,"owner_stopped":true,
            "continuation":{"job":"failed-launch","after":"502"}}
    ]);
    state["answer_receipts"] = json!({
        "501":{"thread":"200","posted":true,"handled_by":"failed-launch","launch_confirmed":true},
        "502":{"thread":"200","posted":true,"handled_by":"failed-launch","launch_confirmed":true},
        "503":{"thread":"200","read_by":"reader","posted":true,"handled_by":"failed-launch"}
    });
    let generation = state["questions"][1].clone();
    let messages = state["questions"][0]["continuation"]["steering_messages"].clone();
    a.save(&state);
    recover_reply_reads(&a.ctx, &mut state).unwrap();
    for id in ["501", "502"] {
        assert!(!reply_delivered(&state, id));
        assert!(!reply_reserved(&state, id));
        assert_eq!(state["answer_receipts"][id]["posted"], true);
        assert_eq!(
            state["answer_receipts"][id]["failed_launch_job"],
            "failed-launch"
        );
        assert!(state["answer_receipts"][id]["launch_confirmed"].is_null());
    }
    assert!(reply_delivered(&state, "503"));
    assert_eq!(state["questions"][0]["after"], "200");
    assert_eq!(state["questions"][0]["seen_reply"], "500");
    assert_eq!(state["questions"][0]["continuation"]["after"], "500");
    assert_eq!(
        state["questions"][0]["continuation"]["job_info"]["status"],
        "failed"
    );
    assert_eq!(
        state["questions"][0]["continuation"]["steering_messages"],
        messages
    );
    assert_eq!(state["questions"][1], generation);
    assert_eq!(state["questions"][2]["open"], false);
    assert_eq!(state["questions"][2]["owner_stopped"], true);
    assert_eq!(std::fs::read(&a.ctx.todo).unwrap(), todo_bytes);
    assert!(a.fake.borrow().posts.is_empty());
    assert!(a.fake.borrow().launches.is_empty());
    assert_eq!(a.state(), state);
}

#[test]
fn failed_request_cursor_is_restored_only_for_its_own_dispatch() {
    let a = AnswerResume::new();
    let dir = a.ctx.jobs.join("jobs/failed-request");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"failed","steps":0}),
        2,
    )
    .unwrap();
    let mut state = a.state();
    state["requests"] = json!({"200":{"job":"failed-request","after":"502","job_info":{"status":"queued"}},
        "300":{"job":"different-worker","after":"900"}});
    state["answer_receipts"] = json!({"501":{"thread":"200","posted":true,"handled_by":"failed-request","launch_confirmed":true}});
    let other = state["requests"]["300"].clone();
    a.save(&state);
    recover_reply_reads(&a.ctx, &mut state).unwrap();
    assert_eq!(state["requests"]["200"]["after"], "500");
    assert_eq!(state["requests"]["200"]["job_info"]["status"], "failed");
    assert_eq!(state["requests"]["300"], other);
    assert_eq!(state["answer_receipts"]["501"]["posted"], true);
    assert!(!reply_delivered(&state, "501"));
    assert!(a.fake.borrow().posts.is_empty());
    assert!(a.fake.borrow().launches.is_empty());
}

#[test]
fn late_dispatched_work_after_a_large_record_is_never_replayed() {
    let a = AnswerResume::new();
    let dir = a.ctx.jobs.join("jobs/late-work");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"failed","steps":0}),
        2,
    )
    .unwrap();
    let large = json!({"type":"thread.started","padding":"x".repeat(120000)}).to_string();
    std::fs::write(dir.join("events.jsonl"), format!("{large}\n{{\"type\":\"item.started\",\"item\":{{\"type\":\"command_execution\",\"command\":\"work\"}}}}\n")).unwrap();
    let mut state = a.state();
    state["answer_receipts"]["501"] =
        json!({"handled_by":"late-work","thread":"200","posted":true,"launch_confirmed":true});
    a.save(&state);
    assert!(job_started(&a.ctx, "late-work"));
    recover_reply_reads(&a.ctx, &mut state).unwrap();
    assert!(reply_delivered(&state, "501"));
    assert_eq!(state["answer_receipts"]["501"]["handled_by"], "late-work");
    assert!(a.fake.borrow().posts.is_empty());
    assert!(a.fake.borrow().launches.is_empty());
}

#[test]
fn unreadable_start_evidence_does_not_release_a_delivery_or_its_cursors() {
    let a = AnswerResume::new();
    let dir = a.ctx.jobs.join("jobs/unknown-work");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"failed","steps":0}),
        2,
    )
    .unwrap();
    std::fs::write(dir.join("events.jsonl"), "{incomplete\n").unwrap();
    let mut state = a.state();
    state["answer_receipts"]["501"] =
        json!({"handled_by":"unknown-work","thread":"200","posted":true,"launch_confirmed":true});
    state["questions"] = json!([{"thread_id":"200","after":"200","seen_reply":"501","open":true,
        "continuation":{"job":"unknown-work","after":"501"}}]);
    a.save(&state);
    let before = state.clone();
    recover_reply_reads(&a.ctx, &mut state).unwrap();
    assert_eq!(state, before);
    assert_eq!(a.state(), before);
    assert!(a.fake.borrow().posts.is_empty());
    assert!(a.fake.borrow().launches.is_empty());
}

#[test]
fn one_incomplete_stream_does_not_block_an_independent_failed_reply() {
    let a = AnswerResume::new();
    for (job, events) in [
        ("incomplete-work", "{incomplete\n"),
        ("empty-work", "{\"type\":\"turn.started\"}\n"),
    ] {
        let dir = a.ctx.jobs.join("jobs").join(job);
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(
            &dir.join("job.json"),
            &json!({"status":"failed","steps":0}),
            2,
        )
        .unwrap();
        std::fs::write(dir.join("events.jsonl"), events).unwrap();
    }
    let mut state = a.state();
    state["answer_receipts"] = json!({
        "501":{"handled_by":"incomplete-work","thread":"200","posted":true,"launch_confirmed":true},
        "502":{"handled_by":"empty-work","thread":"300","posted":true,"launch_confirmed":true}
    });
    let unknown = state["answer_receipts"]["501"].clone();
    a.save(&state);
    recover_reply_reads(&a.ctx, &mut state).unwrap();
    assert_eq!(state["answer_receipts"]["501"], unknown);
    assert!(!reply_delivered(&state, "502"));
    assert_eq!(state["answer_receipts"]["502"]["posted"], true);
    assert_eq!(a.state(), state);
    assert!(a.fake.borrow().posts.is_empty());
    assert!(a.fake.borrow().launches.is_empty());
}
