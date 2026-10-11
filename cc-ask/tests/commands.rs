mod common;
use cc_ask::{commands::*, core::*};
use common::{message, Harness, SharedFake};
use serde_json::{json, Value};
use std::{collections::BTreeMap, path::PathBuf};

#[test]
fn ask_existing_thread_creates_separate_question_and_progress_with_work_link() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let body = h.root.path().join("install.md");
    std::fs::write(&body,"【Chromeブックマーク】拡張機能を導入してもらえますか？\n導入したら「入れた」と返信してください。").unwrap();
    let result = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            thread: Some("100".into()),
            title: "Chrome拡張の導入".into(),
            body_file: body,
            agent: "Codex".into(),
            cwd: Some("fixture/bookmarks".into()),
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(result["thread_id"], "300");
    assert_eq!(result["progress_thread_id"], "301");
    assert_eq!(result["work_thread_id"], "100");
    let fake = h.fake.borrow();
    assert!(
        fake.http_calls
            .iter()
            .filter(|r| r["method"] == "POST" && s(&r["url"]).ends_with("/threads"))
            .count()
            == 2
    );
    assert!(fake.posts.is_empty());
    let posted = &fake.messages["300"][0];
    assert!(s(&posted["content"]).starts_with("【bookmarks】\n<@OWN> "));
    assert_eq!(s(&posted["content"]).matches("<@OWN>").count(), 1);
    assert!(s(&posted["content"]).contains("【bookmarks】"));
    assert_eq!(posted["allowed_mentions"]["users"], json!(["OWN"]));
    let progress = &fake.messages["301"][0];
    assert_eq!(progress["flags"], SILENT | 4);
    assert_eq!(progress["allowed_mentions"]["users"], json!([]));
    assert!(s(&progress["content"]).contains("https://discord.com/channels/G/100"));
    assert!(s(&progress["content"]).contains("https://discord.com/channels/G/300"));
    let state = h.state();
    let recorded = state["questions"]
        .as_array()
        .unwrap()
        .iter()
        .rev()
        .find(|q| q["thread_id"] == "300")
        .unwrap();
    assert_eq!(recorded["cwd"], "fixture/bookmarks");
    assert_eq!(recorded["agent"], "Codex");
    assert!(state["questions"]
        .as_array()
        .unwrap()
        .iter()
        .any(|q| q["thread_id"] == "300" && q["open"] == true));
}

#[test]
fn ask_existing_thread_rejects_foreign_server_and_nonquestion() {
    for (guild, text) in [
        ("OTHER", "Chrome拡張を導入してもらえますか？"),
        ("G", "導入が完了しました。"),
    ] {
        let mut h = Harness::new();
        h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!(guild);
        let body = h.root.path().join("question.md");
        std::fs::write(&body, text).unwrap();
        assert!(cmd_ask(
            &mut h.ctx,
            &AskArgs {
                thread: Some("100".into()),
                title: "導入".into(),
                body_file: body,
                ..Default::default()
            }
        )
        .is_err());
        assert!(h.fake.borrow().posts.is_empty());
    }
}

#[test]
fn ask_existing_thread_dry_run_sends_nothing() {
    let mut h = Harness::new();
    let body = h.root.path().join("question.md");
    std::fs::write(&body, "Chrome拡張を導入してもらえますか？").unwrap();
    cmd_ask(
        &mut h.ctx,
        &AskArgs {
            thread: Some("100".into()),
            title: "導入".into(),
            body_file: body,
            dry_run: true,
            ..Default::default()
        },
    )
    .unwrap();
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[test]
fn new_caller_question_preserves_answered_question_assignment() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let body = h.root.path().join("question.md");
    std::fs::write(&body, "Chrome拡張を導入してもらえますか？").unwrap();
    let mut args = AskArgs {
        thread: Some("100".into()),
        body_file: body,
        title: "導入".into(),
        agent: "Codex".into(),
        cwd: Some("fixture/bookmarks".into()),
        ..Default::default()
    };
    let first = cmd_ask(&mut h.ctx, &args).unwrap();
    {
        let mut fake = h.fake.borrow_mut();
        let history = fake.messages.get_mut(s(&first["thread_id"])).unwrap();
        history.last_mut().unwrap()["author"]["bot"] = json!(true);
        history.push(message("2000", "入れた", "OWN"));
    }
    let before = h.state()["questions"][0].clone();
    args.cwd = Some("another/bookmarks".into());
    args.agent = "Claude".into();
    let result = cmd_ask(&mut h.ctx, &args).unwrap();
    assert_ne!(result["thread_id"], first["thread_id"]);
    assert_ne!(result["progress_thread_id"], first["progress_thread_id"]);
    let state = h.state();
    let q = state["questions"]
        .as_array()
        .unwrap()
        .iter()
        .rev()
        .find(|q| q["thread_id"] == first["thread_id"])
        .unwrap();
    assert_eq!(q["cwd"], "fixture/bookmarks");
    assert_eq!(q["agent"], "Codex");
    assert_eq!(*q, before);
    let new = state["questions"].as_array().unwrap().last().unwrap();
    assert_eq!(new["cwd"], "another/bookmarks");
    assert_eq!(new["agent"], "Claude");
    assert_eq!(new["work_thread_id"], "100");
    assert!(owner_replies(&mut h.ctx, new, "OWN").unwrap().is_empty());
}

/// Override only selected effects; all Discord and child process work stays fake.
struct Overrides {
    shared: SharedFake,
    routes: BTreeMap<String, Value>,
    run_error: bool,
}
impl Effects for Overrides {
    fn now(&self) -> f64 {
        self.shared.now()
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        let path = url.trim_start_matches("https://discord.com/api/v10");
        if let Some(value) = self.routes.get(&format!("{method} {path}")) {
            self.shared
                .0
                .borrow_mut()
                .http_calls
                .push(json!({"method":method,"url":url,"body":body,"discord":discord}));
            return Ok(value.clone());
        }
        self.shared.http(method, url, body, discord)
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        if self.run_error && argv.iter().any(|arg| arg == "recover-reboot") {
            // These cases exercise failures after startup recovery. A separate
            // regression verifies deferred recovery suppresses replacements.
            return self.shared.run(argv, timeout);
        }
        if let Some(report) = self.routes.get("RUN") {
            self.shared
                .0
                .borrow_mut()
                .runs
                .push((argv.to_vec(), timeout));
            return Ok(Output {
                code: report["code"].as_i64().unwrap_or(0) as i32,
                stdout: s(&report["stdout"]).into(),
                stderr: s(&report["stderr"]).into(),
            });
        }
        if self.run_error {
            self.shared
                .0
                .borrow_mut()
                .runs
                .push((argv.to_vec(), timeout));
            return Ok(Output {
                code: 7,
                stdout: "fixture progress\n{\"status\": \"failed\"}\n".into(),
                stderr: "fixture diagnostic\n".into(),
            });
        }
        self.shared.run(argv, timeout)
    }
}
fn overrides(h: &mut Harness, routes: Vec<(&str, Value)>, run_error: bool) {
    h.ctx.effects = Box::new(Overrides {
        shared: SharedFake(h.fake.clone()),
        routes: routes.into_iter().map(|(k, v)| (k.into(), v)).collect(),
        run_error,
    });
}
fn question(thread: &str) -> Value {
    json!({"thread_id":thread,"after":"200","title":"[Codex] 方針の確認","open":true})
}
fn snowflake(now: f64, age: f64) -> String {
    (((now - age) * 1000.) as u64 - 1420070400000u64)
        .checked_shl(22)
        .unwrap()
        .to_string()
}

#[test]
fn test_archives_quiet_threads_regardless_of_long_jobs() {
    let mut h = Harness::new();
    let now = h.ctx.now();
    let quiet = snowflake(now, 7200.);
    let open = snowflake(now, 9000.);
    let busy = snowflake(now, 7201.);
    let other = snowflake(now, 9001.);
    {
        let mut fake = h.fake.borrow_mut();
        fake.threads.clear();
        for (id, latest, parent, name) in [
            (&quiet, quiet.clone(), "T", "quiet"),
            (&busy, snowflake(now, 60.), "T", "busy"),
            (&open, open.clone(), "T", "open question"),
            (&other, other.clone(), "X", "other channel"),
        ] {
            fake.threads.insert(id.clone(),json!({"id":id,"last_message_id":latest,"parent_id":parent,"name":name,"thread_metadata":{"archived":false}}));
        }
    }
    h.save(&json!({"questions":[{"thread_id":open,"open":true}],"requests":{"some":{"job":"very-long-job"}}}));
    cmd_sweep(&mut h.ctx).unwrap();
    assert_eq!(
        h.fake.borrow().patches,
        vec![(quiet, json!({"archived":true}))]
    );
}

#[test]
fn ask_records_context_and_starter_cursor_across_silent_continuations() {
    let mut h = Harness::new();
    let body = h.root.path().join("question.md");
    std::fs::write(&body, format!("  {}  ", "質問".repeat(2200))).unwrap();
    let args = AskArgs {
        thread: None,
        attach: vec![],
        title: "質問".repeat(100),
        body_file: body,
        agent: "Codex".into(),
        cwd: Some("fixture/worktree".into()),
        source: Some("scheduler".into()),
        scheduler_item: Some(12),
        scheduler_job: Some("job1".into()),
        owner_only: None,
        dry_run: false,
    };
    h.fake.borrow_mut().history_injection = Some((1, "300".into(), "a 先に公開".into()));
    let out = cmd_ask(&mut h.ctx, &args).unwrap();
    assert_eq!(out["thread_id"], "300");
    assert_eq!(out["after"], "300");
    assert_eq!(out["url"], "https://discord.com/channels/G/300");
    assert_eq!(out["progress_thread_id"], "301");
    assert_eq!(out["progress_url"], "https://discord.com/channels/G/301");
    assert_eq!(out["work_thread_id"], "");
    let state = h.state();
    let q = &state["questions"][0];
    assert_eq!(q["cwd"], "fixture/worktree");
    assert_eq!(q["agent"], "Codex");
    assert_eq!(s(&q["title"]).chars().count(), 60);
    assert!(s(&q["title"]).starts_with("【worktree】"));
    assert_eq!(q["project"], "worktree");
    assert_eq!(q["source"], "scheduler");
    assert_eq!(q["scheduler_item"], 12);
    assert_eq!(q["scheduler_job"], "job1");
    assert_eq!(q["after"], out["after"]);
    assert_eq!(q["question_message"], out["after"]);
    assert_eq!(q["progress_thread_id"], out["progress_thread_id"]);
    assert_eq!(q["separated_question"], true);
    assert_ne!(
        q["after"],
        h.fake.borrow().messages["300"].last().unwrap()["id"]
    );
    let fake = h.fake.borrow();
    assert!(fake.posts.len() >= 2);
    let starter = &fake
        .http_calls
        .iter()
        .find(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads"))
        .unwrap()["body"]["message"];
    assert_eq!(
        starter["allowed_mentions"],
        json!({"parse":[],"users":["OWN"],"replied_user":false})
    );
    assert!(s(&starter["content"]).starts_with(&format!("【{}】\n<@OWN> ", s(&q["project"]))));
    assert_eq!(s(&starter["content"]).matches("<@OWN>").count(), 1);
    for (_, post) in &fake.posts {
        assert!(s(&post["content"]).chars().count() <= 2000);
        assert_eq!(
            post["allowed_mentions"],
            json!({"parse":[],"users":[],"replied_user":false})
        );
        assert_eq!(post["flags"], SILENT | 4);
        assert!(!s(&post["content"]).contains("<@OWN>"));
    }
    drop(fake);
    // An answer sent between the starter and the final bot chunk remains visible.
    let replies = owner_replies(&mut h.ctx, q, "OWN").unwrap();
    assert_eq!(replies.len(), 1);
    assert_eq!(replies[0]["content"], "a 先に公開");
}

#[test]
fn text_channel_ask_creates_thread_from_starter_and_defaults_to_current_directory() {
    let mut h = Harness::new();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "  方針を教えてください  ").unwrap();
    h.ctx.effects = Box::new(TextChannel(SharedFake(h.fake.clone())));
    let result = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "質問".into(),
            body_file: body,
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(result["thread_id"], "1001");
    assert_eq!(result["progress_thread_id"], "1002");
    let state = h.state();
    let q = &state["questions"][0];
    assert_eq!(q["after"], "1001");
    assert_eq!(q["question_message"], "1001");
    assert_eq!(
        q["cwd"],
        std::env::current_dir().unwrap().to_string_lossy().as_ref()
    );
    assert_eq!(q["agent"], "");
    let project =
        cc_ask::project::project_from_folder(&std::env::current_dir().unwrap().to_string_lossy());
    assert_eq!(
        q["title"],
        format!("【{}】質問", project.chars().take(22).collect::<String>())
    );
    assert_eq!(q["project"], project);
    assert_eq!(q["source"], "posted");
    let fake = h.fake.borrow();
    let first = &fake
        .http_calls
        .iter()
        .find(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/messages"))
        .unwrap()["body"];
    assert_eq!(
        first["content"],
        format!(
            "【{project}】\n<@OWN> 方針を教えてください\n\n-# このスレッドに返信してください。"
        )
    );
    let starters = &fake.messages["T"];
    assert_eq!(starters.len(), 2);
    assert_eq!(starters[0]["id"], result["thread_id"]);
    assert_eq!(starters[1]["id"], result["progress_thread_id"]);
    for id in ["1001", "1002"] {
        assert!(fake
            .http_calls
            .iter()
            .any(|c| c["method"] == "GET" && s(&c["url"]).ends_with(&format!("/channels/{id}"))));
        assert_eq!(
            fake.http_calls
                .iter()
                .filter(|c| c["method"] == "POST"
                    && s(&c["url"]).ends_with(&format!("/channels/T/messages/{id}/threads")))
                .count(),
            1
        );
    }
    assert!(s(&starters[1]["content"]).contains("https://discord.com/channels/G/1001"));
}

#[test]
fn replies_acknowledge_only_returned_owner_messages_and_matching_thread() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[question("200"),{"thread_id":"100","after":"0","title":"別の質問","open":true}]}));
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![
            message("200", "元の質問", "BOT"),
            message("201", "先の回答", "OWN"),
            message("202", "最後の回答", "OWN"),
            message("203", "他人の発言", "OTHER"),
        ],
    );
    let out = cmd_replies(&mut h.ctx, Some("200")).unwrap();
    assert_eq!(out.as_array().unwrap().len(), 1);
    assert_eq!(
        out[0]["replies"]
            .as_array()
            .unwrap()
            .iter()
            .map(|r| s(&r["content"]))
            .collect::<Vec<_>>(),
        ["先の回答", "最後の回答"]
    );
    let state = h.state();
    assert_eq!(state["questions"][0]["seen_reply"], "202");
    assert!(state["questions"][1].get("seen_reply").is_none());
    cmd_replies(&mut h.ctx, None).unwrap();
    assert_eq!(h.state()["questions"][1]["seen_reply"], "100");
}

#[test]
fn empty_replies_preserve_existing_ack_and_closed_questions() {
    let mut h = Harness::new();
    let mut q = question("200");
    q["seen_reply"] = json!("500");
    h.save(&json!({"questions":[q,{"thread_id":"100","title":"closed","open":false}]}));
    let out = cmd_replies(&mut h.ctx, None).unwrap();
    assert_eq!(out.as_array().unwrap().len(), 1);
    assert_eq!(out[0]["replies"], json!([]));
    assert_eq!(h.state()["questions"][0]["seen_reply"], "500");
}

#[test]
fn replies_redact_content_title_and_attachment_urls() {
    let mut h = Harness::new();
    let mut q = question("200");
    q["title"] = json!("password=fixture-sensitive");
    h.save(&json!({"questions":[q]}));
    let mut reply = message("201", "api_key=fixture-sensitive", "OWN");
    reply["attachments"] =
        json!([{"url":"https://discord.com/api/webhooks/123/fixture-sensitive"}]);
    h.fake
        .borrow_mut()
        .messages
        .insert("200".into(), vec![message("200", "starter", "BOT"), reply]);
    let out = cmd_replies(&mut h.ctx, None).unwrap();
    let text = out.to_string();
    assert!(!text.contains("fixture-sensitive"));
    assert!(text.contains("[秘密]"));
    assert_eq!(h.state()["questions"][0]["seen_reply"], "201");
}

#[test]
fn snooze_updates_only_one_question_without_network_or_job_effects() {
    let mut h = Harness::new();
    let mut q = question("200");
    q["continuation"] = json!({"job":"existing-job","after":"201"});
    q["seen_reply"] = json!("201");
    let original = json!({"questions":[q,question("100")],"future":{"keep":true}});
    h.save(&original);
    cmd_snooze(&mut h.ctx, "200", "2026-10-03T09:00:00+09:00").unwrap();
    let state = h.state();
    assert_eq!(state["questions"][0]["remind_not_before"], 1790985600.);
    assert_eq!(
        state["questions"][0]["remind_not_before_iso"],
        "2026-10-03T09:00:00+09:00"
    );
    let mut restored = state.clone();
    restored["questions"][0]
        .as_object_mut()
        .unwrap()
        .remove("remind_not_before");
    restored["questions"][0]
        .as_object_mut()
        .unwrap()
        .remove("remind_not_before_iso");
    restored["questions"][0]
        .as_object_mut()
        .unwrap()
        .remove("remind_set_at");
    assert_eq!(restored, original);
    assert!(h.fake.borrow().http_calls.is_empty());
    assert!(h.fake.borrow().runs.is_empty());
}

#[test]
fn snooze_rejects_invalid_past_missing_and_closed_requests_without_mutation() {
    let mut h = Harness::new();
    let mut closed = question("100");
    closed["open"] = json!(false);
    let original = json!({"questions":[question("200"),closed]});
    h.save(&original);
    for (thread, until, error) in [
        (
            "200",
            "tomorrow",
            "until must be an RFC3339 timestamp with a timezone",
        ),
        (
            "200",
            "2026-10-03T09:00:00",
            "until must be an RFC3339 timestamp with a timezone",
        ),
        (
            "200",
            "2026-09-01T09:00:00+09:00",
            "until must be in the future",
        ),
        (
            "missing",
            "2026-10-03T09:00:00+09:00",
            "open question not found",
        ),
        (
            "100",
            "2026-10-03T09:00:00+09:00",
            "open question not found",
        ),
    ] {
        assert_eq!(cmd_snooze(&mut h.ctx, thread, until).unwrap_err(), error);
        assert_eq!(h.state(), original);
    }
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[test]
fn snooze_suppresses_due_reminders_until_deadline_then_keeps_hourly_interval() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[question("200")]}));
    cmd_snooze(&mut h.ctx, "200", "2026-10-03T09:00:00+09:00").unwrap();
    h.fake.borrow_mut().now = 1790985599.;
    cmd_remind(&mut h.ctx).unwrap();
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .all(|c| c["method"] == "GET"));
    assert!(h.fake.borrow().posts.is_empty());
    h.fake.borrow_mut().now = 1790985600.;
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    h.fake.borrow_mut().now += 3600.;
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 2);
}

#[test]
fn reminder_waits_an_hour_skips_owner_answers_and_strips_agent_prefix() {
    let mut h = Harness::new();
    let now = h.ctx.now();
    let after = snowflake(now, 3600.);
    h.save(&json!({"questions":[{"thread_id":"200","after":after,"title":"[Claude] 実装方針","open":true},{"thread_id":"100","after":"0","title":"回答済み","open":true}]}));
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert!(h.text().contains("「実装方針」の件"));
    assert!(!h.text().contains("[Claude]"));
    assert_eq!(
        h.fake.borrow().posts[0].1["allowed_mentions"],
        json!({"parse":[],"users":["OWN"]})
    );
    assert_eq!(h.state()["questions"][0]["last_ping"], now);
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    h.fake.borrow_mut().now += 3600.;
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 2);
}

#[test]
fn close_archives_only_matching_questions_and_preserves_unknown_state() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[question("200"),question("100")],"future":{"keep":true}}));
    cmd_close(&mut h.ctx, "200").unwrap();
    let state = h.state();
    assert_eq!(state["questions"][0]["open"], false);
    assert_eq!(state["questions"][1]["open"], true);
    assert_eq!(state["future"], json!({"keep":true}));
    assert_eq!(
        h.fake.borrow().patches,
        vec![("200".into(), json!({"archived":true}))]
    );
}

#[test]
fn close_failure_does_not_persist_a_closed_question() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[question("200")]}));
    h.fake.borrow_mut().archive_error = true;
    assert!(cmd_close(&mut h.ctx, "200").is_err());
    assert_eq!(h.state()["questions"][0]["open"], true);
    cmd_close(&mut h.ctx, "200").unwrap();
    assert_eq!(h.state()["questions"][0]["open"], false);
}

#[test]
fn log_creates_silent_starter_reuses_topic_and_notify_never_mentions() {
    let mut h = Harness::new();
    let first = cmd_log(&mut h.ctx, "後片付けの記録", "最初の記録", None, false).unwrap();
    let body = h.root.path().join("record.md");
    std::fs::write(&body, "  次の記録  ").unwrap();
    let second = cmd_log(&mut h.ctx, "後片付けの記録", "ignored", Some(&body), true).unwrap();
    assert_eq!(first["thread_id"], second["thread_id"]);
    assert_ne!(first["message_id"], second["message_id"]);
    assert_eq!(
        read_json(&h.root.path().join("log_threads.json")).unwrap(),
        json!({"後片付けの記録":"300"})
    );
    let fake = h.fake.borrow();
    assert_eq!(fake.posts.len(), 2);
    assert_eq!(fake.posts[0].1["flags"], SILENT);
    assert_eq!(fake.posts[1].1["flags"], 0);
    assert_eq!(fake.posts[1].1["content"], "次の記録");
    for (_, post) in &fake.posts {
        assert_eq!(post["allowed_mentions"], json!({"parse":[]}));
    }
    assert_eq!(
        fake.patches,
        vec![("300".into(), json!({"archived":false}))]
    );
    let creations: Vec<_> = fake
        .http_calls
        .iter()
        .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/T/threads"))
        .collect();
    assert_eq!(creations.len(), 1);
    assert_eq!(creations[0]["body"]["message"]["flags"], SILENT);
    assert_eq!(
        creations[0]["body"]["message"]["allowed_mentions"],
        json!({"parse":[]})
    );
}

#[test]
fn failed_log_delivery_reuses_saved_topic_without_new_starter() {
    let mut h = Harness::new();
    h.fake.borrow_mut().post_error = true;
    assert!(cmd_log(&mut h.ctx, "記録", "本文", None, false).is_err());
    cmd_log(&mut h.ctx, "記録", "本文", None, false).unwrap();
    let fake = h.fake.borrow();
    assert_eq!(fake.posts.len(), 1);
    assert_eq!(
        fake.http_calls
            .iter()
            .filter(|c| s(&c["url"]).ends_with("/channels/T/threads"))
            .count(),
        1
    );
}

#[test]
fn inbox_reads_allowed_threads_and_attachments_oldest_first() {
    let mut h = Harness::new();
    let mark = h.root.path().join("cursor.txt");
    std::fs::write(&mark, "100").unwrap();
    overrides(
        &mut h,
        vec![
            (
                "GET /guilds/G/channels",
                json!([{"id":"10","name":"メモ","type":0},{"id":"T","name":"tasks","type":15},{"id":"30","name":"announcements","type":5},{"id":"40","name":"voice","type":2}]),
            ),
            (
                "GET /guilds/G/threads/active",
                json!({"threads":[{"id":"101","type":11,"owner_id":"BOT","last_message_id":"105","parent_id":"T","name":"active"},{"id":"90","type":11,"owner_id":"BOT","last_message_id":"99","parent_id":"T","name":"old"}]}),
            ),
            (
                "GET /channels/T/threads/archived/public?limit=25",
                json!({"threads":[{"id":"102","type":11,"owner_id":"BOT","last_message_id":"107","parent_id":"T","name":"archived"}]}),
            ),
            (
                "GET /channels/10/threads/archived/public?limit=25",
                json!({"threads":[]}),
            ),
            (
                "GET /channels/30/threads/archived/public?limit=25",
                json!({"threads":[]}),
            ),
        ],
        false,
    );
    {
        let mut fake = h.fake.borrow_mut();
        fake.messages.clear();
        fake.messages.insert(
            "10".into(),
            vec![
                message("103", "channel reply", "OWN"),
                message("104", "ignore bot", "BOT"),
            ],
        );
        fake.messages
            .insert("101".into(), vec![message("105", "active reply", "OWN")]);
        let mut archived = message("107", "archived reply", "OWN");
        archived["attachments"] = json!([{"url":"https://example.test/file"}]);
        fake.messages.insert("102".into(), vec![archived]);
    }
    let out = cmd_inbox(&mut h.ctx, &mark).unwrap();
    assert_eq!(
        out.iter().map(|v| s(&v["id"])).collect::<Vec<_>>(),
        ["105", "107"]
    );
    assert_eq!(out[0]["where"], "tasks > active");
    assert_eq!(out[1]["where"], "tasks > archived");
    assert_eq!(out[1]["text"], "archived reply https://example.test/file");
    assert_eq!(out[0]["at"], "2026-09-30T10:00");
    assert_eq!(std::fs::read_to_string(&mark).unwrap(), "107");
    assert!(cmd_inbox(&mut h.ctx, &mark).unwrap().is_empty());
    assert_eq!(std::fs::read_to_string(&mark).unwrap(), "107");
    assert!(!h
        .fake
        .borrow()
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).contains("/channels/10/")
            || s(&c["url"]).contains("/channels/30/")
            || s(&c["url"]).contains("/channels/90/messages")
            || s(&c["url"]).contains("/channels/40/messages")));
}

#[test]
fn inbox_skips_inaccessible_places_and_leaves_empty_cursor_untouched() {
    let mut h = Harness::new();
    let mark = h.root.path().join("cursor.txt");
    std::fs::write(&mark, "100").unwrap();
    overrides(
        &mut h,
        vec![
            (
                "GET /guilds/G/channels",
                json!([{"id":"10","name":"general","type":0}]),
            ),
            ("GET /guilds/G/threads/active", json!({"threads":[]})),
        ],
        false,
    );
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/10/threads/archived/public?limit=25".into(), 1);
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/10/messages?after=100&limit=100".into(), 1);
    assert!(cmd_inbox(&mut h.ctx, &mark).unwrap().is_empty());
    assert_eq!(std::fs::read_to_string(&mark).unwrap(), "100");
}

#[test]
fn check_reports_intent_bits_and_channel_details() {
    let mut h = Harness::new();
    overrides(
        &mut h,
        vec![
            (
                "GET /applications/@me",
                json!({"name":"fixture bot","flags":1<<19}),
            ),
            ("GET /channels/T", json!({"name":"tasks","type":15})),
        ],
        false,
    );
    assert_eq!(
        cmd_check(&mut h.ctx).unwrap(),
        json!({"app":"fixture bot","message_content_intent":true,"channel":"tasks","channel_type":15})
    );
}

#[test]
fn external_steps_use_configured_single_paths_and_propagate_failure() {
    let mut h = Harness::new();
    h.ctx.scheduler_command = PathBuf::from("fixture scheduler.exe");
    h.ctx.shepherd_command = PathBuf::from("fixture shepherd.exe");
    h.ctx.todo_command = PathBuf::from("fixture todo.exe");
    cmd_scheduler_step(&mut h.ctx).unwrap();
    cmd_shepherd_step(&mut h.ctx).unwrap();
    cmd_todo_watch(&mut h.ctx).unwrap();
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .all(|(_, timeout)| *timeout == 20));
    assert_eq!(
        h.fake
            .borrow()
            .runs
            .iter()
            .map(|(argv, _)| argv.clone())
            .collect::<Vec<_>>(),
        vec![
            vec!["fixture scheduler.exe", "step"],
            vec!["fixture shepherd.exe", "step"],
            vec!["fixture todo.exe", "watch"]
        ]
    );
    overrides(&mut h, vec![], true);
    assert_eq!(cmd_scheduler_step(&mut h.ctx).unwrap_err(), "step exited 7");
}

#[test]
fn reminder_persists_success_before_later_failure_and_does_not_replay_next_tick() {
    let mut h = Harness::new();
    let now = h.ctx.now();
    let after = snowflake(now, 3600.);
    h.save(&json!({"questions":[
        {"thread_id":"200","after":after,"title":"最初の質問","project":"製品A","open":true},
        {"thread_id":"100","after":after,"title":"次の質問","project":"製品B","open":true}]}));
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/100/messages".into(), 1);
    assert!(cmd_remind(&mut h.ctx).is_err());
    let state = h.state();
    assert_eq!(state["questions"][0]["last_ping"], now);
    assert!(state["questions"][1].get("last_ping").is_none());
    assert_eq!(h.fake.borrow().posts.len(), 1);
    assert_eq!(h.fake.borrow().posts[0].0, "200");
    cmd_remind(&mut h.ctx).unwrap();
    let fake = h.fake.borrow();
    assert_eq!(
        fake.posts
            .iter()
            .map(|(tid, _)| tid.as_str())
            .collect::<Vec<_>>(),
        ["200", "100"]
    );
    assert_eq!(
        fake.http_calls
            .iter()
            .filter(|c| c["method"] == "POST" && s(&c["url"]).ends_with("/channels/200/messages"))
            .count(),
        1
    );
    assert_eq!(h.state()["questions"][1]["last_ping"], now);
}

#[test]
fn scheduler_error_report_matches_swallowed_inprocess_error_but_other_failures_propagate() {
    let mut h = Harness::new();
    overrides(
        &mut h,
        vec![("RUN", json!({"code":0,"stdout":"{\"busy\":true}\n"}))],
        false,
    );
    cmd_scheduler_step(&mut h.ctx).unwrap();
    assert_eq!(h.ctx.cfg["__scheduler_step_failed"], true);
    h.fake.borrow_mut().runs.clear();
    overrides(
        &mut h,
        vec![(
            "RUN",
            json!({"code":1,"stdout":"scheduler progress\n{\n  \"error\": \"RuntimeError\"\n}\n","stderr":"scheduler tick failed RuntimeError\n"}),
        )],
        false,
    );
    cmd_scheduler_step(&mut h.ctx).unwrap();
    assert_eq!(h.ctx.cfg["__scheduler_step_failed"], true);
    assert_eq!(h.fake.borrow().runs[0].1, 20);
    assert!(cmd_shepherd_step(&mut h.ctx).is_err());
    assert!(cmd_todo_watch(&mut h.ctx).is_err());
    overrides(
        &mut h,
        vec![(
            "RUN",
            json!({"code":1,"stdout":"{\"status\":\"failed\"}\n"}),
        )],
        false,
    );
    assert_eq!(cmd_scheduler_step(&mut h.ctx).unwrap_err(), "step exited 1");
    overrides(
        &mut h,
        vec![(
            "RUN",
            json!({"code":7,"stdout":"{\"error\":\"RuntimeError\"}\n"}),
        )],
        false,
    );
    assert_eq!(cmd_scheduler_step(&mut h.ctx).unwrap_err(), "step exited 7");
}

#[test]
fn tick_ignores_old_enabled_scanners_and_preserves_ledgers_while_running_watch() {
    const CHILD: &str = "CC_TEST_1095_TICK_CHILD";
    if std::env::var_os(CHILD).is_none() {
        // Capture the actual tick diagnostics with fixture effects, not live services.
        let mut child = std::process::Command::new(std::env::current_exe().unwrap());
        child
            .args([
                "--exact",
                "tick_ignores_old_enabled_scanners_and_preserves_ledgers_while_running_watch",
                "--nocapture",
            ])
            .env(CHILD, "1");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            child.creation_flags(0x08000000);
        }
        let output = child.output().unwrap();
        let stderr = String::from_utf8_lossy(&output.stderr);
        assert!(
            output.status.success(),
            "{}\n{stderr}",
            String::from_utf8_lossy(&output.stdout)
        );
        assert!(!stderr.contains("routing failed"), "{stderr}");
        assert!(!stderr.contains("patrol failed"), "{stderr}");
        return;
    }
    let mut h = Harness::new();
    // An old enabled flag and invalid ledgers must never enter either scanner.
    atomic_json(
        &h.ctx.here.join("automation.json"),
        &json!({"enabled":true,"since":1}),
        2,
    )
    .unwrap();
    let names = ["automation.json", "routing_state.json", "patrol_state.json"];
    for name in &names[1..] {
        std::fs::write(h.ctx.here.join(name), b"invalid legacy ledger").unwrap();
    }
    let before: Vec<_> = names
        .iter()
        .map(|name| std::fs::read(h.ctx.here.join(name)).unwrap())
        .collect();
    // Isolate existing intake/resume from this regression case.
    h.fake.borrow_mut().threads.clear();
    h.fake.borrow_mut().messages.clear();
    cmd_tick(&mut h.ctx).unwrap();
    for (name, bytes) in names.iter().zip(before) {
        assert_eq!(std::fs::read(h.ctx.here.join(name)).unwrap(), bytes);
    }
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().picks.is_empty());
    // A valid backlog would be changed by the old routing scanner, even when
    // its entry is excluded from request surfaces. Detect that entry point too.
    let backlog = json!({"messages":{"900":{"ready":true,"message":{"channel":"outside"}}},"cursor":{"since":"1","places":{},"channels":{}}});
    atomic_json(&h.ctx.here.join("routing_state.json"), &backlog, 2).unwrap();
    let bytes = std::fs::read(h.ctx.here.join("routing_state.json")).unwrap();
    cmd_tick(&mut h.ctx).unwrap();
    assert_eq!(
        std::fs::read(h.ctx.here.join("routing_state.json")).unwrap(),
        bytes
    );
    assert_eq!(
        h.fake
            .borrow()
            .runs
            .iter()
            .filter(|(args, _)| args.last().is_some_and(|arg| arg == "watch"))
            .count(),
        2
    );
}

#[test]
fn patrol_observes_work_before_discord_history_and_records_real_core_results() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[question("200")]}));
    cmd_tick(&mut h.ctx).unwrap();
    let f = h.fake.borrow();
    let history = f
        .chronology
        .iter()
        .position(|event| event.starts_with("http:GET:") && event.contains("/messages?limit="))
        .unwrap();
    let watch = f
        .chronology
        .iter()
        .position(|event| event.starts_with("run:") && event.ends_with(":watch"))
        .unwrap();
    let scheduler = f
        .chronology
        .iter()
        .position(|event| {
            event.starts_with("run:") && event.contains("scheduler") && event.ends_with(":step")
        })
        .unwrap();
    assert!(watch < history && scheduler < history, "{:?}", f.chronology);
    let health = read_json(&h.ctx.here.join("tick-health.json")).unwrap();
    assert_eq!(health["stages"]["cmd_todo_watch"], true);
    assert_eq!(health["stages"]["cmd_scheduler_step"], true);
    assert!(health["finished_at"].is_u64());
    assert!(health["error"].is_null());
}

#[test]
fn tick_shares_launch_budget_and_runs_every_step_after_errors() {
    let mut h = Harness::new();
    h.save(&json!({"questions":[question("200")]}));
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/200/messages?limit=100".into(), 1);
    overrides(&mut h, vec![], true);
    cmd_tick(&mut h.ctx).unwrap();
    // Even failed resume, intake launch, scheduler and shepherd leave reminder/sweep/watch runnable.
    let fake = h.fake.borrow();
    assert!(fake
        .runs
        .iter()
        .any(|(a, _)| a.last().is_some_and(|s| s == "watch")));
    assert_eq!(
        fake.runs
            .iter()
            .filter(|(a, _)| a.last().is_some_and(|s| s == "step"))
            .count(),
        2
    );
    assert!(
        fake.http_calls
            .iter()
            .filter(|c| s(&c["url"]).contains("/channels/200/messages?limit=100"))
            .count()
            >= 2
    );
    assert!(fake
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).ends_with("/threads/active")));
}

#[test]
fn python_json_preserves_whitespace_unicode_and_string_punctuation() {
    let value = json!({"thread_id":"300","text":"日本語: a,b","values":[1,true]});
    assert_eq!(
        python_json(&value, None).unwrap(),
        "{\"thread_id\": \"300\", \"text\": \"日本語: a,b\", \"values\": [1, true]}"
    );
    assert_eq!(python_json(&json!([]), Some(1)).unwrap(), "[]");
    assert_eq!(
        python_json(&json!([{ "replies": [] }]), Some(1)).unwrap(),
        "[\n {\n  \"replies\": []\n }\n]"
    );
}

#[test]
fn external_stdout_retains_inprocess_notices_and_removes_only_cli_report() {
    assert_eq!(inprocess_stdout("{\n  \"status\": \"ok\"\n}\n"), "");
    assert_eq!(
        inprocess_stdout("scheduler progress\n{\n  \"status\": \"ok\"\n}\n"),
        "scheduler progress\n"
    );
    assert_eq!(
        inprocess_stdout(
            "【tools】#3 作業：担当ジョブが終了したため、再起動しました（ジョブ job3）\n"
        ),
        "【tools】#3 作業：担当ジョブが終了したため、再起動しました（ジョブ job3）\n"
    );
    assert_eq!(
        inprocess_stdout("shepherd started repo#1 job1\n"),
        "shepherd started repo#1 job1\n"
    );
    assert_eq!(
        inprocess_stdout("[warning] continue\n"),
        "[warning] continue\n"
    );
}

#[test]
fn tick_launches_resume_before_intake_and_shares_one_attempt_budget() {
    let mut h = Harness::new();
    let external_work = if cfg!(windows) {
        tempfile::tempdir_in(r"C:\Windows\Temp").unwrap()
    } else {
        tempfile::tempdir().unwrap()
    };
    let work = external_work.path().join("work");
    std::fs::create_dir(&work).unwrap();
    let newest = snowflake(h.ctx.now(), 601.);
    let mut q = question("200");
    q["cwd"] = json!(work.to_string_lossy());
    q["agent"] = json!("Codex");
    h.save(&json!({"questions":[q]}));
    {
        let mut fake = h.fake.borrow_mut();
        fake.threads.get_mut("200").unwrap()["parent_id"] = json!("T");
        fake.messages.insert(
            "200".into(),
            vec![
                message("200", "元の質問", "BOT"),
                message(&newest, "推奨案で進めて", "OWN"),
            ],
        );
        fake.sequence = newest.parse().unwrap();
    }
    cmd_tick(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.fake.borrow().launches[0]["name"], "resume-200");
    assert_eq!(h.state()["questions"][0]["seen_reply"], newest);
    cmd_tick(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.fake.borrow().launches[1]["name"], "req-100");
}

#[test]
fn missing_config_does_not_starve_external_tick_steps_or_touch_questions() {
    let mut h = Harness::new();
    let state = json!({"questions":[question("200")]});
    h.save(&state);
    h.ctx.cfg = json!({"__cc_ask_config_error":"OSError"});
    cmd_tick(&mut h.ctx).unwrap();
    assert_eq!(h.state(), state);
    assert!(h.fake.borrow().http_calls.is_empty());
    assert_eq!(h.fake.borrow().runs.len(), 7);
    assert_eq!(
        h.fake
            .borrow()
            .runs
            .iter()
            .map(|(a, _)| a.last().unwrap().as_str())
            .collect::<Vec<_>>(),
        ["--apply", "recover", "watch", "step", "step", "/NH", "/NH"]
    );
}

#[test]
fn deferred_reboot_recovery_suppresses_replacement_watch_and_dispatch_for_one_tick() {
    let mut h = Harness::new();
    h.ctx.cfg = json!({"__cc_ask_config_error":"OSError"});
    h.fake.borrow_mut().reboot_recover_fail = true;
    cmd_tick(&mut h.ctx).unwrap();
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(argv, _)| argv.iter().any(|arg| arg == "recover-reboot")));
    assert!(!h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(argv, _)| argv.last().is_some_and(|arg| arg == "watch")));
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(argv, _)| argv.last().is_some_and(|arg| arg == "watch-preview")));
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(argv, _)| argv.last().is_some_and(|arg| arg == "status")));
    assert!(!h.fake.borrow().runs.iter().any(|(argv, _)| {
        argv.iter().any(|arg| arg.ends_with("scheduler.py"))
            && argv.last().is_some_and(|arg| arg == "step")
    }));
    let health = read_json(&h.ctx.here.join("tick-health.json")).unwrap();
    assert_eq!(health["mode"], "observe_only");
    assert_eq!(health["stages"]["cmd_todo_watch"], true);
    assert_eq!(health["stages"]["cmd_scheduler_step"], true);
    assert_eq!(health["error"], "core_stage_failed");
    h.fake.borrow_mut().runs.clear();
    h.fake.borrow_mut().reboot_recover_fail = false;
    cmd_tick(&mut h.ctx).unwrap();
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(argv, _)| argv.last().is_some_and(|arg| arg == "watch")));
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(argv, _)| argv.last().is_some_and(|arg| arg == "recover")));
}

#[test]
fn ask_and_log_normalize_file_line_endings_like_python_read_text() {
    let mut h = Harness::new();
    let body = h.root.path().join("windows-body.md");
    std::fs::write(&body, "  第一行\r\n第二行\r第三行  ").unwrap();
    cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "質問".into(),
            body_file: body.clone(),
            cwd: Some("fixture/work".into()),
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(
        h.fake.borrow().messages["300"][0]["content"],
        "【work】\n<@OWN> 第一行\n第二行\n第三行\n\n-# このスレッドに返信してください。"
    );
    cmd_log(&mut h.ctx, "記録", "ignored", Some(&body), false).unwrap();
    assert_eq!(h.text(), "第一行\n第二行\n第三行");
    cmd_log(&mut h.ctx, "記録", "--text keeps\r\nverbatim", None, false).unwrap();
    assert_eq!(h.text(), "--text keeps\r\nverbatim");
}

#[test]
fn separated_question_never_reminds_owner_after_one_hour() {
    let mut h = Harness::new();
    let body = h.root.path().join("question.md");
    std::fs::write(&body, "料金を決めてください。\na 月額1000円\nb 月額2000円").unwrap();
    let result = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "料金".into(),
            body_file: body,
            cwd: Some("fixture/work".into()),
            ..Default::default()
        },
    )
    .unwrap();
    let before = h.state()["questions"][0].clone();
    let calls = h
        .fake
        .borrow()
        .http_calls
        .iter()
        .filter(|c| c["method"] == "POST")
        .count();
    h.fake.borrow_mut().now += 7200.;
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.state()["questions"][0], before);
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(
        h.fake
            .borrow()
            .http_calls
            .iter()
            .filter(|c| c["method"] == "POST")
            .count(),
        calls
    );
    assert_eq!(h.fake.borrow().messages[s(&result["thread_id"])].len(), 1);
}
/// Text channels create a thread from a real starter message; Discord uses the
/// starter snowflake as the thread ID, independently for question and progress.
struct TextChannel(SharedFake);
impl Effects for TextChannel {
    fn now(&self) -> f64 {
        self.0.now()
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.0.run(argv, timeout)
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        let path = url.trim_start_matches("https://discord.com/api/v10");
        if method == "GET" && path == "/channels/T" {
            self.0
                 .0
                .borrow_mut()
                .http_calls
                .push(json!({"method":method,"url":url,"body":body,"discord":discord}));
            return Ok(json!({"type":0,"id":"T","guild_id":"G"}));
        }
        if method == "GET"
            && path.starts_with("/channels/")
            && path.split('/').count() == 3
            && !self
                .0
                 .0
                .borrow()
                .threads
                .contains_key(path.split('/').nth(2).unwrap())
        {
            self.0
                 .0
                .borrow_mut()
                .http_calls
                .push(json!({"method":method,"url":url,"body":body,"discord":discord}));
            return Err(format!(
                "Discord API GET {path} failed: 404 Unknown Channel"
            ));
        }
        if method == "POST"
            && path.starts_with("/channels/T/messages/")
            && path.ends_with("/threads")
        {
            let mut f = self.0 .0.borrow_mut();
            f.http_calls
                .push(json!({"method":method,"url":url,"body":body,"discord":discord}));
            let mid = path.split('/').nth(4).unwrap();
            let mut starter = f.messages["T"]
                .iter()
                .find(|m| m["id"] == mid)
                .unwrap()
                .clone();
            let thread = json!({"id":mid,"guild_id":"G","type":11,"parent_id":"T",
                "owner_id":"BOT","name":body.unwrap()["name"],"thread_metadata":{"archived":false}});
            starter["type"] = json!(21);
            starter["content"] = json!("");
            starter["message_reference"] = json!({"message_id":mid,"channel_id":"T"});
            f.threads.insert(mid.into(), thread.clone());
            f.messages.insert(mid.into(), vec![starter]);
            return Ok(thread);
        }
        // SharedFake assigns a fresh ID and saves each message, rather than
        // returning the same fixed ID for both independent parent posts.
        self.0.http(method, url, body, discord)
    }
}
