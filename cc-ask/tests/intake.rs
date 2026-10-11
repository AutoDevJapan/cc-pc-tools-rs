mod common;
use cc_ask::core::*;
use common::*;
use serde_json::json;
use std::{
    panic::{catch_unwind, AssertUnwindSafe},
    path::Path,
};
const COMPLETE: &str = "状態: 完了\n直しました https://example.test/pr/1";

#[test]
fn request_and_resume_use_implementation_permissions_but_reviews_stay_readonly() {
    for prefix in ["req", "resume"] {
        for (task, requested, expected) in [
            ("implementation", "workspace-write", "danger-full-access"),
            ("review", "read-only", "read-only"),
            ("code_review", "", "danger-full-access"),
        ] {
            let mut h = Harness::new();
            let cwd = h.root.path().to_string_lossy().into_owned();
            launch_job(&mut h.ctx, &json!({"id":"123","name":"ツール修正"}), "依頼の作業を進める",
                &json!({"model":"gpt-6.1-sol","effort":"high","task_type":task,"sandbox":requested}),
                "1234567890abcdef1234567890abcdef", false, Some(&cwd), Some(prefix)).unwrap();
            assert_eq!(h.fake.borrow().launches[0]["sandbox"], expected);
        }
    }
}

struct SlowDiscovery(SharedFake);
impl Effects for SlowDiscovery {
    fn now(&self) -> f64 {
        self.0.now()
    }
    fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
        self.0.run(args, timeout)
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&serde_json::Value>,
        discord: bool,
    ) -> Result<serde_json::Value> {
        if url.contains("threads/active") || url.contains("threads/archived") {
            return Ok(json!({"threads":[]}));
        }
        if url.ends_with("/channels/900") {
            // A real transport consumes its enclosing deadline. This legacy
            // destination must not consume the known reply's entire phase.
            std::thread::sleep(
                cc_ask::tick_runtime::remaining().unwrap() + std::time::Duration::from_millis(10),
            );
            return Err("fixture discovery timeout".into());
        }
        self.0.http(method, url, body, discord)
    }
}

#[test]
fn slow_destination_discovery_leaves_time_to_deliver_a_known_owner_reply() {
    let mut h = Harness::new();
    h.tick();
    assert_eq!(h.state()["requests"]["100"]["thread_info"]["id"], "100");
    let path = h.ctx.jobs.join("jobs/job1/job.json");
    let mut source = read_json(&path).unwrap();
    source["cwd"] = json!(h.root.path().to_string_lossy());
    atomic_json(&path, &source, 2).unwrap();
    let mid = h.reply("実行中に届いたこの指示を渡して", "100");
    let mut state = h.state();
    state["requests"]["900"] = json!({"job":"historical-worker","after":"900"});
    h.save(&state);
    h.ctx.effects = Box::new(SlowDiscovery(SharedFake(h.fake.clone())));
    let deadline = cc_ask::tick_runtime::Deadline::new(std::time::Duration::from_secs(15));
    cc_ask::intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    drop(deadline);
    assert_eq!(h.state()["answer_receipts"][&mid]["handled_by"], "job2");
    assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("実行中に届いたこの指示を渡して"));
}

#[test]
fn new_request_in_registered_question_is_not_suppressed_by_a_stopped_previous_job() {
    for suppressed in [false, true] {
        let mut h = Harness::new();
        h.tick();
        h.finish("job1", "cancelled", None, None);
        let path = h.ctx.jobs.join("jobs/job1/job.json");
        let mut info = read_json(&path).unwrap();
        if !suppressed {
            info["cancel_requested"] = json!(true);
        }
        atomic_json(&path, &info, 2).unwrap();
        let mut state = h.state();
        if suppressed {
            state["requests"]["100"]["restart"] = json!({"suppressed":"job1"});
        }
        register_question(
            &mut state,
            &json!({"id":"100","name":"Skills化依頼"}),
            "1000",
        );
        h.save(&state);
        let reply = h.reply("次のSkill化依頼を処理して。", "100");
        // Both entry points run in production, in this order, under the shared state lock.
        let mut budget = false;
        cc_ask::resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
        cc_ask::intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("次のSkill化依頼"));
        assert_eq!(h.state()["requests"]["100"]["after"], reply);
        assert_eq!(h.state()["questions"][0]["open"], false);
        let posts = h.fake.borrow().posts.len();
        h.tick();
        cc_ask::commands::cmd_remind(&mut h.ctx).unwrap();
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert_eq!(h.fake.borrow().posts.len(), posts);
    }
}

#[test]
fn quoted_bug_report_does_not_claim_or_suppress_a_request() {
    let mut h = Harness::new();
    for id in ["repair", "unrelated"] {
        let dir = h.ctx.jobs.join("jobs").join(id);
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(
            &dir.join("job.json"),
            &json!({"id":id,"status":"running"}),
            2,
        )
        .unwrap();
        std::fs::write(
            dir.join("prompt.md"),
            "Fix intake; affected threads e.g. 100, 200.\n```text\nスレッドID: 100\n```\n",
        )
        .unwrap();
    }
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.state()["requests"]["100"]["job"], "job1");
    h.finish("job1", "failed", None, None);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(job_status(&h.ctx, "repair"), "running");
}

#[test]
fn a_real_multi_owner_conflict_records_the_jobs_and_retries_after_resolution() {
    let mut h = Harness::new();
    for id in ["owner-a", "owner-b"] {
        let dir = h.ctx.jobs.join("jobs").join(id);
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(
            &dir.join("job.json"),
            &json!({"id":id,"status":"running","discord_thread_id":"100"}),
            2,
        )
        .unwrap();
    }
    h.tick();
    let state = h.state();
    let error = s(&state["requests"]["100"]["last_error"]);
    assert!(error.starts_with("RuntimeError: multiple running jobs"));
    assert!(error.contains("100") && error.contains("owner-a") && error.contains("owner-b"));
    assert_eq!(state["requests"]["100"]["after"], "0");
    assert!(h.fake.borrow().launches.is_empty() && h.fake.borrow().posts.is_empty());
    h.finish("owner-a", "cancelled", None, None);
    h.finish("owner-b", "cancelled", None, None);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert!(h.state()["requests"]["100"].get("last_error").is_none());
}

#[test]
fn stale_job_adopts_the_existing_worker_and_receipt_does_not_consume_undelivered_reply() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "cancelled", None, None);
    external_worker(&h, true);
    let reply = h.reply("新しい指示を既存の担当に届けて。", "100");
    let mut state = h.state();
    state["requests"]["100"]["restart"] = json!({"suppressed":"job1"});
    state["answer_receipts"][&reply] = json!({"thread":"100","posted":true});
    h.save(&state);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake.borrow().launches[1]["resume_session"],
        "external-session"
    );
    assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("新しい指示を既存の担当に届けて。"));
    assert_eq!(h.state()["answer_receipts"][&reply]["handled_by"], "job2");
    assert!(h.state()["requests"]["100"].get("restart").is_none());
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
}

#[test]
fn manually_handled_request_is_not_restarted_by_resume_or_intake() {
    let mut h = Harness::new();
    h.tick();
    let reply = h.reply("この投稿は手動起動済みです。", "100");
    let mut state = h.state();
    state["answer_receipts"][&reply] =
        json!({"thread":"100","posted":true,"handled_by":"job1","source":"manual"});
    register_question(&mut state, &json!({"id":"100","name":"依頼"}), "1000");
    h.save(&state);
    let posts = h.fake.borrow().posts.len();
    for _ in 0..2 {
        let mut budget = false;
        cc_ask::resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
        cc_ask::intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
    }
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.fake.borrow().posts.len(), posts);
    assert_eq!(h.state()["answer_receipts"][&reply]["handled_by"], "job1");
}

#[test]
fn a_question_continuation_and_intake_share_one_delivery_for_the_same_forum_replies() {
    let mut h = Harness::new();
    let external_work = tempfile::tempdir_in(r"C:\Windows\Temp").unwrap();
    h.tick();
    h.finish("job1", "cancelled", None, None);
    let path = h.ctx.jobs.join("jobs/job1/job.json");
    let mut info = read_json(&path).unwrap();
    info["cancel_requested"] = json!(true);
    atomic_json(&path, &info, 2).unwrap();
    let first = (((h.ctx.now() - 602.) * 1000. - 1420070400000.) as u64) << 22;
    let second = first + (1000 << 22);
    {
        let mut fake = h.fake.borrow_mut();
        fake.messages.get_mut("100").unwrap().push(message(
            &first.to_string(),
            "回答aで進めて。",
            "OWN",
        ));
        fake.messages.get_mut("100").unwrap().push(message(
            &second.to_string(),
            "次の条件も反映して。",
            "OWN",
        ));
        fake.sequence = second;
    }
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"100","title":"フォーラム内の質問",
        "after":"1000","open":true,"cwd":external_work.path().to_string_lossy()}]);
    h.save(&state);
    let mut budget = false;
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
    cc_ask::intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    let state = h.state();
    for id in [first, second] {
        assert_eq!(
            state["answer_receipts"][id.to_string()]["handled_by"],
            "job2"
        );
    }
    assert_eq!(state["requests"]["100"]["job"], "job2");
    complete(&h, "job2");
    for _ in 0..2 {
        let mut budget = false;
        cc_ask::resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
        cc_ask::intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
    }
    assert_eq!(h.fake.borrow().launches.len(), 2);
    let results = h
        .fake
        .borrow()
        .posts
        .iter()
        .filter(|(_, p)| s(&p["content"]).contains("直しました https://example.test/pr/1"))
        .count();
    assert_eq!(results, 1);
}

#[test]
fn legacy_multi_thread_assignments_survive_a_handoff_to_the_other_thread() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, false);
    h.reply("この追加指示を反映して。", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(
        job_info(&h.ctx, "job1")["discord_thread_ids"],
        json!(["100", "200"])
    );
    h.reply("もう一つの案もこの条件で。", "200");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake.borrow().launches[1]["resume_session"],
        "session-job1"
    );
    assert!(h.fake.borrow().picks.is_empty());
    // Intake launched this successor and therefore owns its result publication.
    assert!(!truth(&h.state()["requests"]["200"]["external_owner"]));
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
}

#[test]
fn already_adopted_legacy_worker_repairs_its_multi_thread_associations_before_handoff() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, false);
    let info = job_info(&h.ctx, "idea-brainstorm");
    h.save(&json!({"questions":[],"requests":{
        "100":{"after":"100","job":"idea-brainstorm","external_owner":true,"job_info":info,"selection":info},
        "200":{"after":"0","job":"idea-brainstorm","external_owner":true,"job_info":info,"selection":info}
    }}));
    h.reply("既に担当している方へ新しい条件です。", "100");
    h.tick();
    assert_eq!(
        job_info(&h.ctx, "job1")["discord_thread_ids"],
        json!(["100", "200"])
    );
    h.reply("もう一つの案にも新しい条件です。", "200");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake.borrow().launches[1]["resume_session"],
        "session-job1"
    );
    assert!(h.fake.borrow().picks.is_empty());
}

#[test]
fn declared_owner_takes_priority_over_a_labeled_prompt_even_after_legacy_metadata_was_saved() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, true);
    let dir = h.ctx.jobs.join("jobs/labeled-repair");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"labeled-repair","status":"running"}),
        2,
    )
    .unwrap();
    std::fs::write(
        dir.join("prompt.md"),
        "スレッドID: 100\nここでの失敗を調査する",
    )
    .unwrap();
    atomic_json(
        &dir.join("discord-threads.json"),
        &json!({"thread_ids":["100"],"source":"prompt"}),
        2,
    )
    .unwrap();
    h.reply("正規の担当へこの条件を伝えて。", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(
        h.fake.borrow().launches[0]["resume_session"],
        "external-session"
    );
    assert_eq!(job_status(&h.ctx, "labeled-repair"), "running");
    assert!(h.state()["requests"]["100"].get("last_error").is_none());
}

#[test]
fn handoff_preserves_inferred_assignment_priority_when_another_thread_has_a_declared_owner() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, false);
    let dir = h.ctx.jobs.join("jobs/declared-200");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"declared-200","status":"running",
        "discord_thread_id":"200","model":"gpt-6.1-sol","effort":"high","task_type":"research",
        "cwd":h.root.path(),"thread_id":"declared-session"}),
        2,
    )
    .unwrap();
    h.reply("100の追加指示です。", "100");
    h.tick();
    assert_eq!(
        job_info(&h.ctx, "job1")["discord_thread_ids_source"],
        "prompt"
    );
    h.reply("200は正規の担当へ届けて。", "200");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake.borrow().launches[1]["resume_session"],
        "declared-session"
    );
    assert_eq!(job_status(&h.ctx, "job1"), "running");
    assert!(h.state()["requests"]["200"].get("last_error").is_none());
}

#[test]
fn a_pending_recovery_does_not_overlap_a_running_inferred_owner() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    h.fake.borrow_mut().crash_before_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
    let mut state = h.state();
    state["requests"]["100"]["restart"]["pending"]["tried"] = json!(false);
    h.save(&state);
    let dir = h.ctx.jobs.join("jobs/legacy-owner");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"), &json!({"id":"legacy-owner","status":"running",
        "discord_thread_id":"200","discord_thread_ids":["100","200"],"discord_thread_ids_source":"prompt",
        "thread_id":"other-session"}), 2).unwrap();
    atomic_json(
        &dir.join("discord-threads.json"),
        &json!({"thread_ids":["100","200"],"source":"prompt"}),
        2,
    )
    .unwrap();
    h.fake.borrow_mut().now += 61.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(job_status(&h.ctx, "legacy-owner"), "running");
    assert_eq!(
        h.state()["requests"]["100"]["restart"]["suppressed"],
        "job1"
    );
}

#[test]
fn interrupted_recovery_adopts_its_own_child_and_keeps_result_delivery_and_retry() {
    for succeed in [false, true] {
        let mut h = Harness::new();
        h.tick();
        h.finish("job1", "failed", None, None);
        h.fake.borrow_mut().crash_after_launch = true;
        assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
        let reservation = h.state();
        assert!(truth(
            &reservation["requests"]["100"]["restart"]["pending"]["key"]
        ));
        // Complete the metadata write which the ordinary launcher performs
        // just before returning to the adoption checkpoint.
        let path = h.ctx.jobs.join("jobs/job2/job.json");
        let mut info = read_json(&path).unwrap();
        info["discord_thread_id"] = json!("100");
        atomic_json(&path, &info, 2).unwrap();
        assert_eq!(job_info(&h.ctx, "job2")["discord_thread_id"], "100");
        // Crash after the launcher saved thread metadata, before intake's adoption checkpoint.
        h.save(&reservation);
        h.tick();
        assert_eq!(h.state()["requests"]["100"]["job"], "job2");
        assert!(!truth(&h.state()["requests"]["100"]["external_owner"]));
        if succeed {
            complete(&h, "job2");
            h.tick();
            h.tick();
            assert_eq!(
                h.fake
                    .borrow()
                    .posts
                    .iter()
                    .filter(
                        |(_, p)| s(&p["content"]).contains("直しました https://example.test/pr/1")
                    )
                    .count(),
                1
            );
            assert_eq!(h.fake.borrow().launches.len(), 2);
        } else {
            h.finish("job2", "failed", None, None);
            h.fake.borrow_mut().now += 60.;
            h.tick();
            assert_eq!(h.fake.borrow().launches.len(), 3);
            assert_eq!(h.state()["requests"]["100"]["job"], "job3");
        }
    }
}

fn bot_thread(h: &Harness) {
    let mut f = h.fake.borrow_mut();
    f.threads.get_mut("100").unwrap()["owner_id"] = json!("BOT");
    f.threads.get_mut("100").unwrap()["name"] = json!("【Colonization】ゲーム案を壁打ちする");
    let mut opening = message(
        "100",
        "オーナーのアイデア原文: 敵国を早く全滅させるゲームにしたい。",
        "BOT",
    );
    opening["author"]["bot"] = json!(true);
    f.messages.insert("100".into(), vec![opening]);
}

fn external_worker(h: &Harness, explicit: bool) {
    let dir = h.ctx.jobs.join("jobs/idea-brainstorm");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"idea-brainstorm","status":"running",
        "model":"gpt-6.1-sol","effort":"high","task_type":"research",
        "cwd":h.root.path(),"thread_id":"external-session",
        "discord_thread_id":if explicit { "100" } else { "" }}),
        2,
    )
    .unwrap();
    std::fs::write(dir.join("prompt.md"), "複数のアイデアを担当する。100|Colonization;200|別のアイデア; 原文を踏まえて壁打ちを続ける。").unwrap();
}

#[test]
fn bot_created_request_starts_only_on_owner_post_and_keeps_opening_context() {
    let mut h = Harness::new();
    bot_thread(&h);
    h.tick();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    let reply = h.reply("Civ6と7のUXを掛け合わせたい。", "100");
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 1);
    assert!(s(&f.launches[0]["prompt"])
        .contains("オーナーのアイデア原文: 敵国を早く全滅させるゲームにしたい。"));
    assert!(s(&f.launches[0]["prompt"]).contains("Civ6と7のUXを掛け合わせたい。"));
    assert_eq!(f.posts[0].1["message_reference"]["message_id"], reply);
    assert_eq!(f.posts[0].1["flags"], SILENT);
    drop(f);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
}

#[test]
fn another_running_worker_gets_same_session_delivery_without_a_request_launch() {
    for explicit in [false, true] {
        let mut h = Harness::new();
        bot_thread(&h);
        external_worker(&h, explicit);
        let reply = h.reply("UXをCiv6と7に寄せたい。", "100");
        h.tick();
        let f = h.fake.borrow();
        assert!(f.picks.is_empty());
        assert_eq!(f.launches.len(), 1);
        assert_eq!(f.launches[0]["resume_session"], "external-session");
        assert_eq!(f.launches[0]["model"], "gpt-6.1-sol");
        assert_eq!(f.launches[0]["effort"], "high");
        assert!(s(&f.launches[0]["prompt"]).contains("200|別のアイデア"));
        assert!(s(&f.launches[0]["prompt"]).contains("UXをCiv6と7に寄せたい。"));
        assert!(s(&f.launches[0]["prompt"])
            .contains("オーナーのアイデア原文: 敵国を早く全滅させるゲームにしたい。"));
        assert!(f
            .runs
            .iter()
            .any(|(args, _)| args.windows(2).any(|a| a == ["cancel", "idea-brainstorm"])));
        drop(f);
        assert_eq!(h.state()["answer_receipts"][&reply]["handled_by"], "job1");
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 1);
    }
}

#[test]
fn another_worker_already_acknowledged_replies_are_not_delivered_again() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, false);
    let reply = h.reply("これは担当ジョブが回答中です。", "100");
    let mut state = h.state();
    state["answer_receipts"] =
        json!({reply.clone():{"thread":"100","posted":true,"handled_by":"idea-brainstorm"}});
    h.save(&state);
    h.tick();
    h.tick();
    assert_eq!(h.state()["requests"]["100"]["job"], "idea-brainstorm");
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.fake.borrow().runs.is_empty());
    h.finish("idea-brainstorm", "succeeded", Some(COMPLETE), None);
    h.tick();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    h.reply("次の追加案も検討して。", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
}

#[test]
fn external_delivery_blocked_after_receipt_is_retried_without_consuming_reply() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, true);
    let reply = h.reply("この追加指示を反映して。", "100");
    let gate = ByteLock::acquire(
        &h.root.path().join("scheduler.lock"),
        std::time::Duration::ZERO,
    )
    .unwrap();
    h.tick();
    assert_eq!(h.state()["answer_receipts"][&reply]["posted"], true);
    assert!(h.fake.borrow().launches.is_empty());
    drop(gate);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.fake.borrow().posts.len(), 1);
}

#[test]
fn external_acknowledged_messages_after_a_gap_are_not_replayed() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, false);
    h.reply("未配送の追加指示です。", "100");
    let handled = h.reply("これは既に担当が受け取っています。", "100");
    let mut state = h.state();
    state["answer_receipts"] =
        json!({handled.clone():{"thread":"100","posted":true,"handled_by":"idea-brainstorm"}});
    h.save(&state);
    h.tick();
    let f = h.fake.borrow();
    let prompt = s(&f.launches[0]["prompt"]);
    let new_messages = prompt.split("最終返信には").next().unwrap();
    assert!(new_messages.contains("未配送の追加指示です。"));
    assert!(!new_messages.contains("これは既に担当が受け取っています。"));
    assert_eq!(f.posts.len(), 1);
}

#[test]
fn external_delivery_launch_failure_reuses_its_reservation() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, true);
    h.reply("この追加指示を配送して。", "100");
    h.fake.borrow_mut().launch_transport_fail = true;
    h.tick();
    let key = h.state()["requests"]["100"]["owner_update"]["key"].clone();
    assert!(key.is_string());
    assert_eq!(
        read_json(&h.ctx.jobs.join("jobs/idea-brainstorm/job.json")).unwrap()["status"],
        "cancelled"
    );
    h.fake.borrow_mut().launch_transport_fail = false;
    h.tick();
    assert!(h.fake.borrow().launches.is_empty());
    h.fake.borrow_mut().now += cc_ask::reply_recovery::RETRY_INTERVAL_S;
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 1);
    assert_eq!(f.launches[0]["launch_key"], key);
    assert_eq!(f.posts.len(), 1);
}

#[test]
fn explicit_multi_thread_owner_wins_over_a_job_only_quoting_the_bug_report() {
    let mut h = Harness::new();
    bot_thread(&h);
    external_worker(&h, false);
    let path = h.ctx.jobs.join("jobs/idea-brainstorm/job.json");
    let mut info = read_json(&path).unwrap();
    info["discord_thread_ids"] = json!(["100", "200"]);
    atomic_json(&path, &info, 2).unwrap();
    atomic_json(
        &h.ctx.jobs.join("jobs/idea-brainstorm/discord-threads.json"),
        &json!({"thread_ids":["100","200"]}),
        2,
    )
    .unwrap();
    // Simulate a supervisor checkpoint overwriting metadata added by intake.
    info.as_object_mut().unwrap().remove("discord_thread_ids");
    atomic_json(&path, &info, 2).unwrap();
    let repair = h.ctx.jobs.join("jobs/intake-repair");
    std::fs::create_dir_all(&repair).unwrap();
    atomic_json(
        &repair.join("job.json"),
        &json!({"id":"intake-repair","status":"running"}),
        2,
    )
    .unwrap();
    std::fs::write(
        repair.join("prompt.md"),
        "Fix intake; affected threads e.g. 100, 200.",
    )
    .unwrap();
    h.reply("追加の指示を担当へ届けて。", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(
        h.fake.borrow().launches[0]["resume_session"],
        "external-session"
    );
    assert_eq!(
        read_json(&h.ctx.jobs.join("jobs/job1/job.json")).unwrap()["discord_thread_ids"],
        json!(["100", "200"])
    );
    assert_eq!(
        read_json(&h.ctx.jobs.join("jobs/job1/discord-threads.json")).unwrap()["thread_ids"],
        json!(["100", "200"])
    );
    assert_eq!(
        read_json(&repair.join("job.json")).unwrap()["status"],
        "running"
    );
}

#[test]
fn valid_question_only_association_does_not_block_unrelated_intake_or_claim_request_ownership() {
    let mut h = Harness::new();
    let dir = h.ctx.jobs.join("jobs/question-worker");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"question-worker","status":"running","cwd":"D:/ProductA"}),
        2,
    )
    .unwrap();
    let sidecar = json!({"question_thread_ids":["700"],"question_routes":{"700":{"question_thread_id":"700","posting_job":"question-worker","work_thread_id":"100"}}});
    atomic_json(&dir.join("discord-threads.json"), &sidecar, 2).unwrap();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.state()["requests"]["100"]["job"], "job1");
    assert!(h.state()["requests"]["100"]["external_owner"].is_null());
    assert_eq!(
        read_json(&dir.join("discord-threads.json")).unwrap(),
        sidecar
    );
}

#[test]
fn malformed_request_association_cannot_be_hidden_by_valid_question_fields() {
    let mut h = Harness::new();
    let dir = h.ctx.jobs.join("jobs/question-worker");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"question-worker","status":"running"}),
        2,
    )
    .unwrap();
    atomic_json(&dir.join("discord-threads.json"),&json!({"thread_ids":"100","question_thread_ids":["700"],"question_routes":{"700":{"question_thread_id":"700","posting_job":"question-worker"}}}),2).unwrap();
    assert!(cc_ask::intake::cmd_intake(&mut h.ctx, &mut false).is_err());
    assert!(h.fake.borrow().launches.is_empty());
}

struct QuestionDuringLaunch(common::SharedFake);
impl Effects for QuestionDuringLaunch {
    fn http(
        &mut self,
        m: &str,
        u: &str,
        b: Option<&serde_json::Value>,
        d: bool,
    ) -> Result<serde_json::Value> {
        self.0.http(m, u, b, d)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        let out = self.0.run(a, t)?;
        if a.iter().any(|v| v == "--launch-key") && out.code == 0 {
            let job = out.stdout.trim();
            let dir = self.0 .0.borrow().root.join("jobs").join(job);
            atomic_json(
                &dir.join("discord-threads.json"),
                &json!({"question_thread_ids":["700"],"question_routes":{"700":{"question_thread_id":"700","posting_job":job}}}),
                2,
            )?;
        }
        Ok(out)
    }
}
#[test]
fn request_launch_registration_keeps_questions_created_before_its_checkpoint() {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(QuestionDuringLaunch(common::SharedFake(h.fake.clone())));
    let cwd = h.root.path().to_string_lossy().into_owned();
    let job=launch_job(&mut h.ctx,&json!({"id":"100","name":"ツール修正"}),"依頼を進める",
        &json!({"model":"gpt-6.1-sol","effort":"high","task_type":"implementation","sandbox":"danger-full-access"}),
        "1234567890abcdef1234567890abcdef",false,Some(&cwd),Some("req")).unwrap();
    let sidecar = read_json(
        &h.ctx
            .jobs
            .join("jobs")
            .join(&job)
            .join("discord-threads.json"),
    )
    .unwrap();
    assert_eq!(sidecar["thread_ids"], json!(["100"]));
    assert_eq!(sidecar["question_thread_ids"], json!(["700"]));
    assert_eq!(sidecar["question_routes"]["700"]["posting_job"], job);
}
#[test]
fn website_login_and_screen_check_enable_owner_chrome() {
    for task in [
        "https://example.com の表示を確認して",
        "サイトにログインして",
        "画面確認して",
    ] {
        let mut h = Harness::new();
        h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] = json!(task);
        h.tick();
        assert_eq!(h.fake.borrow().launches[0]["mcp_profile"], "browser");
    }
}
fn complete(h: &Harness, jid: &str) {
    h.finish(jid, "succeeded", Some(COMPLETE), None)
}

#[test]
fn test_model_effort_and_type_from_pick_are_shown_and_used() {
    let mut h = Harness::new();
    h.tick();
    assert!(h.text().contains("受け取りました。今の作業に反映します。"));
    {
        let f = h.fake.borrow();
        assert_eq!(f.launches[0]["model"], "gpt-6.1-sol");
        assert_eq!(f.launches[0]["task_type"], "implementation");
        assert!(s(&f.picks[0]["task"]).contains("READMEを直して"));
        assert!(!Path::new(s(&f.launches[0]["prompt_path"])).exists());
    }
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.state()["requests"]["200"]["job"], serde_json::Value::Null);
}

#[test]
fn automatic_router_choices_use_the_implementation_default_and_high() {
    let mut h = Harness::new();
    h.fake.borrow_mut().pick_replies.push_back(json!({
        "model":"gpt-6-astra",
        "effort":"max",
        "task_type":"implementation",
        "reason":"実装向け",
        "source":"jev"
    }));
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches[0]["model"], "gpt-6.1-sol");
    assert_eq!(f.launches[0]["effort"], "high");
}
#[test]
fn test_fallback_when_pick_is_down_and_fallback_is_logged() {
    let mut h = Harness::new();
    {
        let mut f = h.fake.borrow_mut();
        f.pick_down = true;
        f.messages.get_mut("100").unwrap()[0]["content"] = json!("機能を実装して");
    }
    h.tick();
    assert!(h.text().contains("受け取りました。今の作業に反映します。"));
    assert_eq!(
        h.state()["requests"]["100"]["selection"]["source"],
        "fallback"
    );
    assert_eq!(h.fake.borrow().picks.len(), 1);
}
#[test]
fn test_review_fallback_uses_luna() {
    let mut h = Harness::new();
    {
        let mut f = h.fake.borrow_mut();
        f.pick_down = true;
        f.messages.get_mut("100").unwrap()[0]["content"] = json!("変更をレビューして");
    }
    h.tick();
    assert_eq!(h.fake.borrow().launches[0]["model"], "gpt-6-luna");
    assert_eq!(h.fake.borrow().launches[0]["effort"], "high");
}

#[test]
fn review_word_inside_implementation_request_keeps_implementation_profile() {
    let mut h = Harness::new();
    {
        let mut f = h.fake.borrow_mut();
        f.pick_down = true;
        f.messages.get_mut("100").unwrap()[0]["content"] =
            json!("実装してください。画面に「レビュー」という文字を表示してください。");
    }
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches[0]["model"], "gpt-6.1-sol");
    assert_eq!(f.launches[0]["effort"], "high");
    assert_eq!(f.launches[0]["task_type"], "implementation");
}
#[test]
fn test_complete_archives_and_owner_reply_reopens_and_launches() {
    let mut h = Harness::new();
    h.tick();
    complete(&h, "job1");
    h.tick();
    assert_eq!(h.text().lines().next(), Some("終わりました。"));
    {
        let f = h.fake.borrow();
        assert_eq!(
            f.posts.last().unwrap().1["allowed_mentions"],
            json!({"parse":[],"users":["OWN"]})
        );
        assert_eq!(f.patches, vec![("100".into(), json!({"archived":true}))]);
    }
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    h.reply("テストも足して", "100");
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 2);
    let p = s(&f.launches.last().unwrap()["prompt"]);
    assert!(p.contains("READMEを直して"));
    assert!(p.contains("テストも足して"));
}
#[test]
fn test_recently_archived_reply_is_also_seen() {
    let mut h = Harness::new();
    h.tick();
    complete(&h, "job1");
    h.tick();
    h.reply("追加で実装して", "100");
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["thread_metadata"]["archived"] =
        json!(true);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
}
#[test]
fn test_needs_decision_is_registered_and_closed_on_owner_reply() {
    let mut h = Harness::new();
    h.tick();
    let job_path = h.ctx.jobs.join("jobs/job1/job.json");
    let mut info = read_json(&job_path).unwrap();
    info["cwd"] = json!(h.root.path().to_string_lossy());
    atomic_json(&job_path, &info, 2).unwrap();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n方針Aで進めてよいですか。"),
        None,
    );
    h.tick();
    {
        let f = h.fake.borrow();
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
        let progress = &creations[1]["body"]["message"];
        assert_eq!(progress["flags"], SILENT | 4);
        assert_eq!(progress["allowed_mentions"]["users"], json!([]));
        assert_eq!(progress["allowed_mentions"]["replied_user"], false);
        assert!(f.posts.iter().skip(1).all(|(tid, _)| tid == "301"));
    }
    let state = h.state();
    let q = &state["questions"][0];
    assert_eq!(q["thread_id"], "300");
    assert_eq!(q["work_thread_id"], "100");
    assert_eq!(q["progress_thread_id"], "301");
    assert_eq!(q["posting_job"], "job1");
    assert_eq!(q["open"], true);
    assert!(q.get("asked_at").is_some());
    assert_eq!(q["after"], h.fake.borrow().messages["300"][0]["id"]);
    assert!(h.fake.borrow().patches.is_empty());
    h.reply("Aで進めて", "300");
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.state()["questions"][0]["open"], true);
    h.finish(
        "job2",
        "succeeded",
        Some("状態: 完了\n方針Aで進めました。"),
        None,
    );
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    h.tick();
    assert_eq!(h.state()["questions"][0]["open"], false);
}
#[test]
fn test_one_hour_reminder_uses_registered_request_question() {
    let mut h = Harness::new();
    h.tick();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n月額1000円で公開してよいですか。\na. 公開する\nb. 公開を止める"),
        None,
    );
    h.tick();
    let posts = h.fake.borrow().posts.len();
    let creations = h
        .fake
        .borrow()
        .http_calls
        .iter()
        .filter(|c| c["method"] == "POST")
        .count();
    for elapsed in [3600., 7200.] {
        h.fake.borrow_mut().now += elapsed;
        cc_ask::commands::cmd_remind(&mut h.ctx).unwrap();
    }
    assert_eq!(h.fake.borrow().posts.len(), posts);
    assert_eq!(
        h.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST")
            .count(),
        creations
    );
    assert!(h.state()["questions"][0].get("last_ping").is_none());
    assert_eq!(h.state()["questions"][0]["open"], true);
}
#[test]
fn test_failure_resumes_same_session_model_effort_cwd_without_mention() {
    let mut h = Harness::new();
    h.tick();
    let posts = h.fake.borrow().posts.len();
    assert_eq!(h.recover("job1"), "job2");
    let f = h.fake.borrow();
    assert_eq!(f.picks.len(), 1);
    assert_eq!(f.launches[1]["resume_session"], "session-job1");
    for field in ["model", "effort", "cwd"] {
        assert_eq!(f.launches[1][field], f.launches[0][field]);
    }
    assert!(s(&f.launches[1]["prompt"])
        .starts_with("前回のジョブが止まったので続きから再開してください"));
    assert!(s(&f.launches[1]["prompt"]).contains("READMEを直して"));
    assert_eq!(f.posts.len(), posts);
    assert_eq!(h.state()["requests"]["100"]["job"], "job2");
}
#[test]
fn test_auto_model_is_preserved_on_restart() {
    let mut h = Harness::new();
    h.fake.borrow_mut().pick_replies.push_back(json!({"model":"auto","effort":"max","task_type":"implementation","reason":"内容で選びます。"}));
    h.tick();
    let info = h.ctx.jobs.join("jobs/job1/job.json");
    let mut saved = read_json(&info).unwrap();
    saved["model"] = json!("auto");
    atomic_json(&info, &saved, 2).unwrap();
    h.recover("job1");
    assert_eq!(h.fake.borrow().launches[1]["model"], "auto");
    assert_eq!(h.fake.borrow().picks.len(), 1);
}
#[test]
fn test_retry_cap_posts_one_silent_line_and_keeps_todo_then_expires() {
    let mut h = Harness::new();
    h.tick();
    h.recover("job1");
    h.recover("job2");
    h.finish("job3", "cancelled", None, None);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    let posts = h.fake.borrow().posts.len();
    for _ in 0..3 {
        h.tick();
    }
    assert_eq!(h.fake.borrow().posts.len(), posts);
    assert_eq!(h.state()["questions"], json!([]));
    let f = h.fake.borrow();
    let notice = &f.posts.last().unwrap().1;
    assert_eq!(notice["flags"], SILENT);
    assert_eq!(
        notice["allowed_mentions"],
        json!({"parse":[],"users":[],"replied_user":false})
    );
    assert_eq!(s(&notice["content"]).lines().count(), 1);
    assert!(s(&notice["content"]).contains("1時間ごとに再試行"));
    assert!(s(&notice["content"]).contains("原因:"));
    assert!(s(&notice["content"]).contains("試した処置:"));
    assert!(!s(&notice["content"]).contains("<@"));
    drop(f);
    let db = read_json(&h.ctx.todo).unwrap();
    assert_eq!(db["items"].as_array().unwrap().len(), 1);
    assert_eq!(db["items"][0]["status"], "doing");
    assert_eq!(
        db["items"][0]["discord_restart_history"]
            .as_array()
            .unwrap()
            .len(),
        2
    );
    assert!(
        db["items"][0]["discord_restart_not_before"]
            .as_f64()
            .unwrap()
            > h.fake.borrow().now
    );
    assert!(db["items"][0]["jobs"]
        .as_array()
        .unwrap()
        .contains(&json!("job3")));
    h.reply("再開すればいいじゃん", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    h.fake.borrow_mut().now += cc_ask::recovery_failure::SLOW_RETRY_S;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 4);
    assert!(s(&h.fake.borrow().launches[3]["prompt"]).contains("再開すればいいじゃん"));
}
#[test]
fn startup_access_denied_runs_scheduled_remedy_then_resumes_without_an_owner_question() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    std::fs::write(
        h.ctx.jobs.join("jobs/job1/stderr.log"),
        "failed to initialize in-process app-server client: Access is denied",
    )
    .unwrap();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert!(h.fake.borrow().runs.iter().any(|(argv, _)| argv
        == &[
            "schtasks.exe",
            "/Run",
            "/TN",
            "local-ai-web-autostart-10012"
        ]));
    assert_eq!(
        h.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|v| v["url"] == "http://127.0.0.1:10012/api/resident/restart")
            .count(),
        1
    );
    h.fake.borrow_mut().now += 60.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.state()["questions"], json!([]));
}
#[test]
fn context_and_model_failures_launch_fresh_with_original_work_instructions() {
    for error in ["context window exceeded", "requested model is unavailable"] {
        let mut h = Harness::new();
        h.tick();
        h.finish("job1", "failed", None, None);
        std::fs::write(h.ctx.jobs.join("jobs/job1/stderr.log"), error).unwrap();
        h.tick();
        let f = h.fake.borrow();
        assert_eq!(f.launches.len(), 2);
        assert_eq!(f.launches[1]["resume_session"], "");
        assert!(s(&f.launches[1]["prompt"]).contains("READMEを直して"));
        if error.contains("model") {
            assert_eq!(f.launches[1]["model"], "gpt-6-astra");
        } else {
            assert_eq!(f.launches[1]["model"], f.launches[0]["model"]);
        }
    }
}
#[test]
fn initial_launcher_error_and_restart_launcher_error_select_a_different_model() {
    for initial in [true, false] {
        let mut h = Harness::new();
        h.fake.borrow_mut().launch_error_message = "model unavailable".into();
        if initial {
            h.fake.borrow_mut().launch_error = true;
        }
        h.tick();
        if !initial {
            h.finish("job1", "failed", None, None);
            h.fake.borrow_mut().launch_error = true;
            h.tick();
            h.fake.borrow_mut().now += 60.;
        }
        h.tick();
        let f = h.fake.borrow();
        let launch = f.launches.last().unwrap();
        assert_eq!(launch["model"], "gpt-6-astra");
        assert_eq!(launch["resume_session"], "");
        assert!(s(&launch["prompt"]).contains("READMEを直して"));
    }
}
#[test]
fn scheduled_console_start_does_not_require_the_exit_api_to_be_ready() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    std::fs::write(h.ctx.jobs.join("jobs/job1/stderr.log"), "Access is denied").unwrap();
    h.fake
        .borrow_mut()
        .http_errors
        .insert("POST http://127.0.0.1:10012/api/resident/restart".into(), 1);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    h.fake.borrow_mut().now += 60.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake
            .borrow()
            .runs
            .iter()
            .filter(|(argv, _)| argv.first().is_some_and(|s| s == "schtasks.exe"))
            .count(),
        1
    );
}
#[test]
fn fresh_session_and_handoff_survive_a_later_console_launcher_failure() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    std::fs::write(
        h.ctx.jobs.join("jobs/job1/stderr.log"),
        "context window exceeded",
    )
    .unwrap();
    std::fs::write(
        h.ctx.jobs.join("jobs/job1/prompt.md"),
        "元の受入条件：引き継ぎを最後まで保持する",
    )
    .unwrap();
    h.fake.borrow_mut().launch_error = true;
    h.fake.borrow_mut().launch_error_message = "app-server client: Access is denied".into();
    h.tick();
    h.fake.borrow_mut().now += 60.;
    h.tick();
    h.fake.borrow_mut().now += 60.;
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 3);
    assert_eq!(f.launches[2]["resume_session"], "");
    assert!(s(&f.launches[2]["prompt"]).contains("元の受入条件"));
}
#[test]
fn test_cancelled_lost_missing_and_empty_final_all_recover() {
    for status in ["cancelled", "lost", "missing", "succeeded"] {
        let mut h = Harness::new();
        h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] = json!("実装して");
        h.tick();
        if status == "missing" {
            std::fs::remove_file(h.ctx.jobs.join("jobs/job1/job.json")).unwrap();
        } else {
            h.finish("job1", status, None, None);
        }
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 2, "{status}");
        assert_eq!(h.fake.borrow().launches[1]["diagnosis"], false, "{status}");
    }
}
#[test]
fn test_owner_reply_during_running_job_is_not_lost_or_archived() {
    let mut h = Harness::new();
    h.tick();
    h.reply("追加の作業もお願い", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    complete(&h, "job1");
    h.tick();
    assert!(h.fake.borrow().patches.is_empty());
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 2);
    assert!(s(&f.launches.last().unwrap()["prompt"]).contains("追加の作業もお願い"));
}
#[test]
fn test_one_launch_per_tick_across_threads() {
    let mut h = Harness::new();
    {
        let mut f = h.fake.borrow_mut();
        f.threads.get_mut("200").unwrap()["owner_id"] = json!("OWN");
        f.messages
            .insert("200".into(), vec![message("200", "別の依頼", "OWN")]);
    }
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
}
#[test]
fn test_ack_failure_does_not_launch_twice() {
    let mut h = Harness::new();
    h.fake.borrow_mut().post_error = true;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 0);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert!(h.text().contains("受け取りました。今の作業に反映します。"));
    assert!(h.state()["requests"]["100"]
        .get("acknowledgement")
        .is_none());
}
#[test]
fn test_failure_recovers_even_when_discord_post_is_unavailable() {
    let mut h = Harness::new();
    h.tick();
    h.fake.borrow_mut().post_error = true;
    h.recover("job1");
    assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    assert!(h.fake.borrow().post_error);
}
#[test]
fn test_retry_cap_notice_delivery_error_does_not_exceed_cap() {
    let mut h = Harness::new();
    h.tick();
    h.recover("job1");
    h.recover("job2");
    h.finish("job3", "failed", None, None);
    h.fake.borrow_mut().post_error = true;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert_ne!(h.state()["requests"]["100"]["restart"]["notice_sent"], true);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert_eq!(h.state()["requests"]["100"]["restart"]["notice_sent"], true);
    let count = h.fake.borrow().posts.len();
    let mut state = h.state();
    state["requests"]["100"]["restart"]["notice_sent"] = json!(false);
    h.save(&state);
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), count);
}
#[test]
fn test_archive_failure_retries_without_reposting_result() {
    let mut h = Harness::new();
    h.tick();
    complete(&h, "job1");
    h.fake.borrow_mut().archive_error = true;
    h.tick();
    let count = h.fake.borrow().posts.len();
    assert_eq!(h.state()["requests"]["100"]["close_pending"], true);
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), count);
    assert_eq!(
        h.fake.borrow().patches,
        vec![("100".into(), json!({"archived":true}))]
    );
}
#[test]
fn test_initial_launch_failure_retries_without_owner_question() {
    let mut h = Harness::new();
    h.fake.borrow_mut().launch_error = true;
    h.tick();
    assert_eq!(h.state()["requests"]["100"]["job"], "launch-failed");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.fake.borrow().launches[1]["model"], "gpt-6.1-sol");
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(h.state()["questions"], json!([]));
}
#[test]
fn test_second_restart_waits_for_backoff() {
    let mut h = Harness::new();
    h.tick();
    h.recover("job1");
    h.finish("job2", "failed", None, None);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    h.fake.borrow_mut().now += 59.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    h.fake.borrow_mut().now += 1.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
}
#[test]
fn test_new_owner_answer_does_not_reset_restart_limit() {
    let mut h = Harness::new();
    h.tick();
    h.recover("job1");
    h.reply("推奨案で直して再試行して", "100");
    h.recover("job2");
    assert_eq!(
        h.state()["requests"]["100"]["restart"]["attempts"]
            .as_array()
            .unwrap()
            .len(),
        2
    );
    assert!(s(&h.fake.borrow().launches[2]["prompt"]).contains("推奨案で直して再試行して"));
    h.reply("次の推奨案も試して", "100");
    h.finish("job3", "failed", None, None);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert_eq!(h.state()["questions"], json!([]));
}
#[test]
fn test_crash_after_request_launch_is_adopted_without_another_launch() {
    let mut h = Harness::new();
    h.fake.borrow_mut().crash_after_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
    let reserved = h.state()["requests"]["100"]["launch"]["key"].clone();
    assert_eq!(reserved, h.fake.borrow().launches[0]["launch_key"]);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.state()["requests"]["100"]["job"], "job1");
    assert!(h.state()["requests"]["100"].get("launch").is_none());
}
#[test]
fn test_crash_before_launcher_reuses_the_durable_attempt_key() {
    let mut h = Harness::new();
    h.fake.borrow_mut().crash_before_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
    let reserved = h.state()["requests"]["100"]["launch"]["key"].clone();
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 1);
    assert_eq!(f.launches[0]["launch_key"], reserved);
    assert_eq!(f.picks.len(), 1);
}
#[test]
fn test_crash_after_restart_launch_is_adopted_without_another_launch() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    h.fake.borrow_mut().crash_after_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
    let key = h.state()["requests"]["100"]["restart"]["pending"]["key"].clone();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.fake.borrow().launches[1]["launch_key"], key);
    assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    assert!(h.state()["requests"]["100"]["restart"]
        .get("pending")
        .is_none());
}
#[test]
fn test_invalid_pick_and_excluded_pick_use_fallback() {
    let mut h = Harness::new();
    h.fake
        .borrow_mut()
        .pick_replies
        .push_back(json!({"model":"invalid model"}));
    h.tick();
    assert_eq!(
        h.fake.borrow().launches.last().unwrap()["model"],
        "gpt-6-luna"
    );
    h.fake.borrow_mut().pick_replies.push_back(
        json!({"model":"gpt-6.1-sol","effort":"max","task_type":"implementation","reason":"選択"}),
    );
    let picked = pick_model(&mut h.ctx, "実装して", &["gpt-6.1-sol".into()], None).unwrap();
    assert_eq!(picked["model"], "gpt-6-sol");
}
#[test]
fn test_preamble_requires_status_and_honours_project_review_rule() {
    for text in [
        "別モデル",
        "Must",
        "マージ",
        "人間またはオーナー",
        "config.godd",
        "根本原因",
        "状態: 完了",
        "状態: 要判断",
        "状態: 継続中",
    ] {
        assert!(REQUEST_PREAMBLE.contains(text), "{text}");
    }
    assert_eq!(result_state("状態: 完了\n結果"), "状態: 完了");
    assert_eq!(result_state("結果のみ"), "状態: 継続中");
    assert_eq!(result_state("状態: 完了\n状態: 要判断"), "状態: 継続中");
}

#[test]
fn progress_and_mislabelled_decision_are_silent_and_remain_dispatchable() {
    for result in [
        "状態: 継続中\ncargoの空き待ちです。テストが残っています。",
        "状態: 要判断\ncargoの空き待ちです。テストが残っています。",
    ] {
        let mut h = Harness::new();
        h.tick();
        h.finish("job1", "succeeded", Some(result), None);
        h.tick();
        let f = h.fake.borrow();
        let post = &f.posts.last().unwrap().1;
        assert_eq!(post["flags"], SILENT);
        assert_eq!(post["allowed_mentions"]["users"], json!([]));
        assert!(!s(&post["content"]).contains("判断をお願い"));
        assert!(!s(&post["content"]).contains("状態を確認してください"));
        assert!(s(&post["content"]).contains("自動で再開"));
        assert!(f.patches.is_empty());
        drop(f);
        assert!(h.state()["questions"].as_array().unwrap().is_empty());
        let db = read_json(&h.ctx.todo).unwrap();
        let item = &db["items"][0];
        assert_eq!(item["status"], "doing");
        assert!(item["jobs"].as_array().unwrap().contains(&json!("job1")));
        let count = h.fake.borrow().posts.len();
        h.tick();
        cc_ask::commands::cmd_remind(&mut h.ctx).unwrap();
        assert_eq!(h.fake.borrow().posts.len(), count);
        // A scheduler/watchdog successor is picked up and its result closes the original thread.
        let next = h.ctx.jobs.join("jobs/job-next");
        std::fs::create_dir_all(&next).unwrap();
        let mut successor = read_json(&h.ctx.jobs.join("jobs/job1/job.json")).unwrap();
        successor["id"] = json!("job-next");
        successor["status"] = json!("running");
        atomic_json(&next.join("job.json"), &successor, 2).unwrap();
        let dir = h.ctx.jobs.join("jobs/job-next");
        std::fs::create_dir_all(&dir).unwrap();
        atomic_json(
            &dir.join("job.json"),
            &json!({"id":"job-next","status":"running","cwd":h.fake.borrow().launches[0]["cwd"]}),
            2,
        )
        .unwrap();
        h.finish("job-next", "succeeded", Some(COMPLETE), None);
        let mut db = read_json(&h.ctx.todo).unwrap();
        db["items"][0]["jobs"]
            .as_array_mut()
            .unwrap()
            .push(json!("job-next"));
        db["items"][0]["jobs"]
            .as_array_mut()
            .unwrap()
            .push(json!("job1"));
        atomic_json(&h.ctx.todo, &db, 2).unwrap();
        h.tick();
        assert_eq!(
            read_json(&h.ctx.todo).unwrap()["items"][0]["status"],
            "done"
        );
        assert_eq!(
            h.fake.borrow().patches.last().unwrap().1,
            json!({"archived":true})
        );
    }
}

#[test]
fn status_contract_requires_a_real_question_and_keeps_completion() {
    assert_eq!(
        result_state("状態: 完了\nテストを終えました。"),
        "状態: 完了"
    );
    assert_eq!(result_state("状態: 継続中\n空き待ち"), "状態: 継続中");
    assert_eq!(result_state("状態: 要判断\n空き待ち"), "状態: 継続中");
    assert_eq!(
        result_state("状態: 要判断\nこの予算で公開してよいですか。"),
        "状態: 要判断"
    );
    assert_eq!(
        result_state("状態: 要判断\na. 予算1000円で公開\nb. 公開を停止"),
        "状態: 要判断"
    );
    assert_eq!(
        result_state("状態: 要判断\n『公開する』でよいですか。"),
        "状態: 要判断"
    );
    assert_eq!(
        result_state("状態: 要判断\na. 「公開する」\nb. 「停止する」"),
        "状態: 要判断"
    );
    assert_eq!(
        result_state("状態: 要判断\n対象 `release で公開してよいですか？"),
        "状態: 要判断"
    );
    assert_eq!(
        result_state("状態: 要判断\n対象 『release で公開してよいですか？"),
        "状態: 要判断"
    );
    for body in [
        "ログの ``release?`` を調査します。",
        "a. ``古い案``\nb. ``別の案``",
        "ログの ``a`?`b`` を調査します。",
    ] {
        assert_eq!(
            result_state(&format!("状態: 要判断\n{body}")),
            "状態: 継続中",
            "{body}"
        );
    }
    for body in [
        "a/b",
        "a. 空き待ち",
        "```\na. 検証ログ\nb. 検証ログ\n```",
        "> 進めますか？",
        "添付ファイル: C:/result?.txt",
        "ログに『本番公開してよいですか』と出ています。調査を続けます。",
        "ログの `本番公開してよいですか？` を調査します。",
        "    本番公開してよいですか？",
    ] {
        assert_eq!(
            result_state(&format!("状態: 要判断\n{body}")),
            "状態: 継続中",
            "{body}"
        );
    }
}
#[test]
fn test_restart_prompt_redacts_owner_secrets() {
    let mut h = Harness::new();
    h.tick();
    h.reply("api_key=mock-sensitive-value", "100");
    h.recover("job1");
    let f = h.fake.borrow();
    let prompt = s(&f.launches[1]["prompt"]);
    assert!(!prompt.contains("mock-sensitive-value"));
    assert!(prompt.contains("[秘密]"));
}

#[test]
fn test_other_job_owns_same_thread_or_todo_so_no_restart_or_post() {
    for shared_todo in [false, true] {
        let mut h = Harness::new();
        h.tick();
        h.finish("job1", "cancelled", None, None);
        let other = h.ctx.jobs.join("jobs/other");
        std::fs::create_dir_all(&other).unwrap();
        atomic_json(
            &other.join("job.json"),
            &json!({"status":"running","thread_id":"another-session"}),
            2,
        )
        .unwrap();
        if shared_todo {
            atomic_json(&h.ctx.todo, &json!({"projects":{},"items":[{"id":42,"status":"doing","jobs":["job1","other"]}]}), 2).unwrap();
        } else {
            std::fs::write(other.join("prompt.md"), "スレッドID: 100\n依頼を実装する").unwrap();
        }
        let posts = h.fake.borrow().posts.len();
        h.tick();
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 1);
        assert_eq!(h.fake.borrow().posts.len(), posts);
        let entry = &h.state()["requests"]["100"];
        if shared_todo {
            assert_eq!(entry["restart"]["suppressed"], "job1");
        } else {
            assert_eq!(entry["job"], "other");
            assert_eq!(entry["external_owner"], true);
        }
    }
}

#[test]
fn test_explicit_cancel_is_silent_even_without_another_live_job() {
    let mut h = Harness::new();
    let todo_before = std::fs::read(&h.ctx.todo).unwrap();
    h.tick();
    h.finish("job1", "cancelled", None, None);
    let file = h.ctx.jobs.join("jobs/job1/job.json");
    let mut info = read_json(&file).unwrap();
    info["cancel_requested"] = json!(true);
    atomic_json(&file, &info, 2).unwrap();
    let posts = h.fake.borrow().posts.len();
    h.tick();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.fake.borrow().posts.len(), posts);
    assert_eq!(std::fs::read(&h.ctx.todo).unwrap(), todo_before);
}

#[test]
fn test_lost_restart_response_and_crash_before_launcher_keep_one_reservation() {
    for before in [false, true] {
        let mut h = Harness::new();
        h.tick();
        h.finish("job1", "lost", None, None);
        if before {
            h.fake.borrow_mut().crash_before_launch = true;
            assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
            h.fake.borrow_mut().now += 60.;
        } else {
            h.fake.borrow_mut().lose_launch_response = true;
        }
        h.tick();
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert_eq!(
            h.state()["requests"]["100"]["restart"]["attempts"]
                .as_array()
                .unwrap()
                .len(),
            if before { 2 } else { 1 }
        );
        assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    }
}

#[test]
fn test_generated_recovery_todo_is_finished_when_request_completes() {
    let mut h = Harness::new();
    h.tick();
    h.recover("job1");
    complete(&h, "job2");
    h.tick();
    assert_eq!(
        read_json(&h.ctx.todo).unwrap()["items"][0]["status"],
        "done"
    );
}

#[test]
fn test_recovered_job_deliberately_cancelled_closes_generated_recovery_todo() {
    let mut h = Harness::new();
    h.tick();
    h.recover("job1");
    h.finish("job2", "lost", None, None);
    let file = h.ctx.jobs.join("jobs/job2/job.json");
    let mut info = read_json(&file).unwrap();
    info["cancel_requested"] = json!(true);
    atomic_json(&file, &info, 2).unwrap();
    let posts = h.fake.borrow().posts.len();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.fake.borrow().posts.len(), posts);
    assert_eq!(
        read_json(&h.ctx.todo).unwrap()["items"][0]["status"],
        "done"
    );
}

#[test]
fn test_launcher_unavailable_keeps_key_and_caps_actual_calls() {
    let mut h = Harness::new();
    h.fake.borrow_mut().launch_transport_fail = true;
    h.tick();
    let key = h.state()["requests"]["100"]["launch"]["key"].clone();
    for _ in 0..5 {
        h.fake.borrow_mut().now += 60.;
        h.tick();
    }
    let f = h.fake.borrow();
    let calls: Vec<_> = f
        .runs
        .iter()
        .filter(|(argv, _)| argv.iter().any(|a| a == "--launch-key"))
        .collect();
    assert_eq!(calls.len(), 3); // initial call plus two bounded restarts
    for (argv, _) in calls {
        let i = argv.iter().position(|a| a == "--launch-key").unwrap();
        assert_eq!(argv[i + 1], s(&key));
    }
    assert_eq!(f.posts.len(), 2); // receipt plus retry-cap notice
    assert_eq!(f.posts[0].1["flags"], SILENT);
    assert_eq!(f.posts[0].1["allowed_mentions"]["users"], json!([]));
}

#[test]
fn test_delayed_reservation_consumes_current_window_attempt() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    h.fake.borrow_mut().crash_before_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
    h.fake.borrow_mut().now += cc_ask::restart::WINDOW_S + 60.;
    h.tick();
    assert_eq!(
        h.state()["requests"]["100"]["restart"]["attempts"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    h.recover("job2");
    h.finish("job3", "failed", None, None);
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
}

#[test]
fn test_todo_add_interruption_restores_generated_item_ownership() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    h.fake.borrow_mut().crash_after_todo_add = true;
    assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
    h.tick();
    complete(&h, "job2");
    h.tick();
    let db = read_json(&h.ctx.todo).unwrap();
    assert_eq!(db["items"].as_array().unwrap().len(), 1);
    assert_eq!(db["items"][0]["status"], "done");
}

#[test]
fn test_lost_job_is_retired_before_resume_and_same_task_owner_is_detected() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "lost", None, None);
    h.tick();
    let f = h.fake.borrow();
    let retire = f
        .runs
        .iter()
        .position(|(a, _)| a.iter().any(|a| a == "--recovery"))
        .unwrap();
    let resume = f
        .runs
        .iter()
        .rposition(|(a, _)| a.iter().any(|a| a == "--resume-session"))
        .unwrap();
    assert!(retire < resume);
    drop(f);

    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, None);
    let file = h.ctx.jobs.join("jobs/job1/job.json");
    let mut source = read_json(&file).unwrap();
    source["thread_id"] = json!(null);
    source["resume_session"] = json!("original-session");
    atomic_json(&file, &source, 2).unwrap();
    let dir = h.ctx.jobs.join("jobs/other");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"status":"running","thread_id":"original-session","discord_thread_id":"100"}),
        2,
    )
    .unwrap();
    let posts = h.fake.borrow().posts.len();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.fake.borrow().posts.len(), posts);
}
