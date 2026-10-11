mod common;
use cc_ask::{core::*, prompt_context::*};
use common::{message, Harness};
use serde_json::json;

#[test]
fn utf8_cap_preserves_instructions_newest_reply_and_a_real_source_path() {
    let h = Harness::new();
    let prompt = format!(
        "最新の回答: a（米ドル契約を維持）\n{}\n最新の作業結果: 請求を確認する",
        "古い説明🚀\n".repeat(60000)
    );
    let result = prepare(
        &h.ctx,
        &json!({"id":"100"}),
        &prompt,
        &json!({}),
        "C:/repo",
        "utf8-limit",
    )
    .unwrap();
    assert!(result.len() <= 60 * 1024);
    assert!(result.starts_with("最新の回答: a（米ドル契約を維持）"));
    assert!(result.contains("最新の作業結果: 請求を確認する"));
    assert!(result.contains("省略"));
    let files: Vec<_> = std::fs::read_dir(h.ctx.here.join("prompt-context"))
        .unwrap()
        .map(|e| e.unwrap().path())
        .collect();
    assert_eq!(files.len(), 1);
    assert!(result.contains(files[0].to_str().unwrap()));
    assert_eq!(std::fs::read_to_string(&files[0]).unwrap(), prompt);
    for limit in 0..400 {
        let short = bounded(&"日本語🚀".repeat(1000), limit, "D:/source.md");
        assert!(short.len() <= limit);
    }
}

#[test]
fn console_aggregates_are_removed_and_only_the_linked_task_is_embedded() {
    let h = Harness::new();
    atomic_json(&h.ctx.todo, &json!({"projects":{},"items":[
        {"id":1166,"title":"Stripeの請求を確認する","status":"doing","jobs":["origin"]},
        {"id":18,"title":"無関係なCiv6の作業","status":"doing","jobs":["other"],"note":"https://github.com/example/civ6/pull/99"}
    ]}), 2).unwrap();
    let aggregate =
        json!([{"project":"civ6","title":"全件にだけある作業","note":"x".repeat(880000)}]);
    let prompt = format!("最新の回答: a\n元の作業指示: 請求を確認\n共有ToDoの文脈:\n{aggregate}\n\n## 引き継ぎ\n無関係な全引き継ぎ\n未完了のPR\nhttps://github.com/example/civ6/pull/99\nオーナーの全投稿（古い順）:\na\n前のジョブの最後のメッセージ:\n未処理の回答がある");
    let result = prepare(
        &h.ctx,
        &json!({"id":"100","name":"請求の確認"}),
        &prompt,
        &json!({"id":"origin"}),
        "C:/repo",
        "scope-limit",
    )
    .unwrap();
    assert!(result.len() < 4096);
    for expected in [
        "最新の回答: a",
        "元の作業指示: 請求を確認",
        "未処理の回答がある",
        "Stripeの請求を確認する",
        "参照先:",
    ] {
        assert!(result.contains(expected), "missing {expected}");
    }
    for unrelated in [
        "全件にだけある作業",
        "無関係な全引き継ぎ",
        "無関係なCiv6の作業",
        "https://github.com/example/civ6/pull/99",
    ] {
        assert!(!result.contains(unrelated), "unexpected {unrelated}");
    }
}

#[test]
fn long_thread_keeps_opening_question_latest_answer_and_its_choices() {
    let h = Harness::new();
    let thread = json!({"id":"100","name":"請求設定"});
    let mut messages = vec![message("100", "a 米ドル契約を維持 / b 契約を変更", "BOT")];
    messages.extend((101..300).map(|id| {
        message(
            &id.to_string(),
            &format!("古い補足{id} {}", "説明🚀".repeat(500)),
            "BOT",
        )
    }));
    messages.push(message("300", "新しい質問: a 維持する / b 変更する", "BOT"));
    messages.push(message("301", "a（維持する）", "OWN"));
    messages.push(message("302", "受け取りました", "BOT"));
    let result =
        cc_ask::prompt_context::conversation(&h.ctx, &thread, &messages, "OWN", 16000).unwrap();
    assert!(result.len() <= 16000);
    for text in [
        "a 米ドル契約を維持",
        "新しい質問: a 維持する",
        "a（維持する）",
        "受け取りました",
        "全会話の参照先:",
    ] {
        assert!(result.contains(text), "missing {text}");
    }
    let owners: Vec<_> = messages
        .iter()
        .filter(|m| s(&m["author"]["id"]) == "OWN")
        .cloned()
        .collect();
    let _ = cc_ask::prompt_context::conversation(&h.ctx, &thread, &owners, "OWN", 100);
    let sources: Vec<_> = std::fs::read_dir(h.ctx.here.join("prompt-context"))
        .unwrap()
        .map(|e| e.unwrap().path())
        .collect();
    assert!(sources
        .iter()
        .any(|path| read_json(path).ok() == Some(json!(messages))));
}

#[test]
fn bounded_history_keeps_registered_question_with_bare_answer_before_chatter() {
    let h = Harness::new();
    let thread = json!({"id":"100","name":"検査送信"});
    atomic_json(
        &h.ctx.here.join("state.json"),
        &json!({"questions":[
            {"thread_id":"100","question_message":"200","after":"290"},
            {"thread_id":"999","question_message":"295"}
        ]}),
        2,
    )
    .unwrap();
    let mut messages = vec![message("100", "古い質問: a 本番を変更する", "BOT")];
    messages.push(message(
        "200",
        "新しい質問: a/bで教えてください。\na: 架空データだけを指定検査先へ送る\nb: 送らない",
        "BOT",
    ));
    messages
        .extend((201..300).map(|id| message(&id.to_string(), &"無関係な説明".repeat(200), "BOT")));
    let mut answer = message("300", "a", "OWN");
    answer["channel_id"] = json!("100");
    answer["timestamp"] = json!("2026-10-11T09:17:20.599+09:00");
    messages.push(answer);
    let prompt = conversation(&h.ctx, &thread, &messages, "OWN", 4096).unwrap();
    assert!(prompt.len() <= 4096);
    assert!(prompt.contains("架空データだけを指定検査先へ送る"));
    assert!(prompt.contains("投稿 200 / 投稿者 BOT"));
    assert!(prompt.contains("投稿 300 / 投稿者 OWN"));
    assert!(prompt.contains("2026-10-11T09:17:20.599+09:00"));
}

#[test]
fn explicit_reply_does_not_bind_to_a_newer_or_cross_thread_question() {
    let h = Harness::new();
    let thread = json!({"id":"100"});
    atomic_json(
        &h.ctx.here.join("state.json"),
        &json!({"questions":[
            {"thread_id":"100","question_message":"200"}
        ]}),
        2,
    )
    .unwrap();
    let mut messages = vec![message("100", "元の依頼", "BOT")];
    messages.push(message("150", "指定先への検査だけを確認する質問", "BOT"));
    messages.push(message("200", "別件の恒久連携を確認する質問", "BOT"));
    messages.extend((201..300).map(|id| message(&id.to_string(), &"補足".repeat(1000), "BOT")));
    let mut answer = message("300", "a", "OWN");
    answer["message_reference"] = json!({"message_id":"150","channel_id":"100"});
    messages.push(answer);
    let prompt = conversation(&h.ctx, &thread, &messages, "OWN", 4096).unwrap();
    assert!(prompt.contains("指定先への検査だけを確認する質問"));
    assert!(!prompt.contains("別件の恒久連携を確認する質問"));
    assert!(prompt.contains("返信先 150"));
    messages.last_mut().unwrap()["message_reference"]["channel_id"] = json!("999");
    let prompt = conversation(&h.ctx, &thread, &messages, "OWN", 4096).unwrap();
    assert!(!prompt.contains("別件の恒久連携を確認する質問"));
    assert!(!prompt.contains("指定先への検査だけを確認する質問"));
}

#[test]
fn context_failure_retries_with_smaller_prompt_and_keeps_the_owner_answer() {
    let mut h = Harness::new();
    h.tick();
    let original = format!(
        "受入条件: 請求設定を確認する\n共有ToDoの文脈:\n{}\n## 引き継ぎ\n無関係なPR全件\n",
        "古い文脈🚀".repeat(88000)
    );
    let path = h.ctx.jobs.join("jobs/job1/prompt.md");
    std::fs::write(&path, &original).unwrap();
    let answer = h.reply("a（米ドル契約を維持）", "100");
    h.finish(
        "job1",
        "failed",
        None,
        Some("Your input exceeds the context window of this model"),
    );
    h.tick();
    {
        let f = h.fake.borrow();
        assert_eq!(f.launches.len(), 2);
        let retry = &f.launches[1];
        assert_eq!(retry["resume_session"], "");
        let prompt = s(&retry["prompt"]);
        assert!(prompt.len() <= 24 * 1024);
        for text in ["a（米ドル契約を維持）", "受入条件: 請求設定を確認する"]
        {
            assert!(prompt.contains(text), "missing {text}");
        }
        assert!(prompt
            .replace('\\', "/")
            .contains(&path.to_string_lossy().replace('\\', "/")));
        assert!(!prompt.contains("無関係なPR全件"));
    }
    assert_eq!(std::fs::read_to_string(path).unwrap(), original);
    assert_eq!(h.state()["answer_receipts"][&answer]["handled_by"], "job2");
    h.tick();
    assert_eq!(
        h.fake.borrow().launches.len(),
        2,
        "the answer was delivered by recovery already"
    );
    h.finish("job2", "failed", None, Some("context window exceeded"));
    h.fake.borrow_mut().now += 60.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert!(s(&h.fake.borrow().launches[2]["prompt"]).len() <= 12 * 1024);
    assert!(s(&h.fake.borrow().launches[2]["prompt"]).contains("a（米ドル契約を維持）"));
    assert!(truth(&h.state()["answer_receipts"][&answer]["posted"]));
}

#[test]
fn preexisting_oversized_reservation_is_capped_before_the_launcher_reads_it() {
    let mut h = Harness::new();
    h.tick();
    h.finish("job1", "failed", None, Some("context window exceeded"));
    h.fake.borrow_mut().crash_before_launch = true;
    assert!(std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| h.tick())).is_err());
    let mut state = h.state();
    state["requests"]["100"]["restart"]["pending"]["prompt"] = json!(format!(
        "最新の回答: a\n{}\n受入条件を保持する",
        "過去の説明🚀".repeat(88000)
    ));
    h.save(&state);
    h.fake.borrow_mut().now += 60.;
    h.tick();
    let fake = h.fake.borrow();
    assert_eq!(fake.launches.len(), 2);
    let prompt = s(&fake.launches[1]["prompt"]);
    assert!(prompt.len() <= 24 * 1024);
    assert!(prompt.contains("最新の回答: a"));
    assert!(prompt.contains("受入条件を保持する"));
    assert!(prompt.contains("参照先:"));
}

#[test]
fn same_thread_and_session_are_reserved_once_even_after_supervisor_metadata_rewrite() {
    let mut h = Harness::new();
    let selection = json!({"model":"gpt-6.1-sol","effort":"high","task_type":"implementation"});
    let thread = json!({"id":"100","name":"依頼"});
    let launch = |h: &mut Harness, thread: &serde_json::Value, key: &str, session: Option<&str>| {
        launch_job_with_session(
            &mut h.ctx,
            thread,
            "依頼を直す",
            &selection,
            key,
            false,
            None,
            None,
            session,
        )
    };
    assert_eq!(launch(&mut h, &thread, "first", None).unwrap(), "job1");
    assert_eq!(launch(&mut h, &thread, "first", None).unwrap(), "job1");
    let file = h.ctx.jobs.join("jobs/job1/job.json");
    let mut job = read_json(&file).unwrap();
    job.as_object_mut().unwrap().remove("discord_thread_id");
    atomic_json(&file, &job, 2).unwrap();
    assert!(launch_blocked(
        &launch(&mut h, &thread, "second", None).unwrap_err()
    ));
    assert!(launch_blocked(
        &launch(&mut h, &json!({"id":"200"}), "third", Some("session-job1")).unwrap_err()
    ));
    assert_eq!(h.fake.borrow().launches.len(), 1);
    h.finish("job1", "succeeded", None, None);
    assert_eq!(launch(&mut h, &thread, "second", None).unwrap(), "job2");
    assert_eq!(h.fake.borrow().launches.len(), 2);
}

#[test]
fn aggregate_headings_in_a_message_never_drop_that_message_or_later_answers() {
    let mut h = Harness::new();
    h.fake
        .borrow_mut()
        .messages
        .get_mut("100")
        .unwrap()
        .push(message(
            "200",
            "## 引き継ぎ\n前回の報告\n未完了のPR\n保留の説明",
            "BOT",
        ));
    let answer = h.reply("a（請求の確認を進める）\n## 引き継ぎ\nこの行もオーナーの指示\n共有ToDoの文脈:\nここから後も実行する", "100");
    h.tick();
    let first = s(&h.fake.borrow().launches[0]["prompt"]).to_owned();
    for expected in [
        "a（請求の確認を進める）",
        "この行もオーナーの指示",
        "ここから後も実行する",
    ] {
        assert!(first.contains(expected), "missing {expected}");
    }
    h.finish("job1", "failed", None, Some("context window exceeded"));
    h.tick();
    let retry = s(&h.fake.borrow().launches[1]["prompt"]).to_owned();
    for expected in [
        "a（請求の確認を進める）",
        "この行もオーナーの指示",
        "ここから後も実行する",
    ] {
        assert!(retry.contains(expected), "missing {expected}");
    }
    assert_eq!(h.state()["answer_receipts"][answer]["handled_by"], "job2");
}
