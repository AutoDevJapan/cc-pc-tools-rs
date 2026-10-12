mod common;
use cc_ask::{commands::*, questions, steering};
use common::*;
use serde_json::{json, Value};

fn job(h: &Harness, id: &str, status: &str, session: &str, extra: Value) {
    let dir = h.ctx.jobs.join("jobs").join(id);
    std::fs::create_dir_all(&dir).unwrap();
    let mut info = json!({"status":status,"resume_session":session,"started_at":"2026-09-30T10:00:00+00:00","created_at":"2026-09-30T10:00:00+00:00"});
    for (k, v) in extra.as_object().into_iter().flatten() {
        info[k] = v.clone();
    }
    std::fs::write(dir.join("job.json"), info.to_string()).unwrap();
}

fn bot(id: &str, text: &str) -> Value {
    let mut m = message(id, text, "BOT");
    m["author"]["bot"] = json!(true);
    m
}

fn question(extra: Value) -> Value {
    let mut q = json!({"thread_id":"200","after":"300","question_message":"300","open":true,"source":"posted"});
    for (k, v) in extra.as_object().into_iter().flatten() {
        q[k] = v.clone();
    }
    q
}

fn history(h: &Harness, msgs: Vec<Value>) {
    h.fake.borrow_mut().messages.insert("200".into(), msgs);
}

#[test]
fn last_successful_post_event_resolves_the_author_without_a_footer() {
    let mut h = Harness::new();
    for (id, mid, destination, exit) in [
        ("older", "200", "200", 0),
        ("author", "300", "200", 0),
        ("wrong", "350", "999", 0),
        ("failed", "400", "200", 1),
    ] {
        job(&h, id, "failed", &format!("session-{id}"), json!({}));
        let event = json!({"item":{"command":format!("discord_ask.exe post --thread {destination} --body-file q.txt"),
            "exit_code":exit,"aggregated_output":json!({"thread_id":destination,"message_ids":[mid]}).to_string()}});
        std::fs::write(
            h.ctx.jobs.join("jobs").join(id).join("events.jsonl"),
            event.to_string(),
        )
        .unwrap();
    }
    history(&h, vec![bot("300", "a/bで返信してください")]);
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({})))
            .unwrap()
            .as_deref(),
        Some("author")
    );
    let after_all = question(json!({"after":"500","question_message":"500"}));
    assert_eq!(
        steering::associated_job(&mut h.ctx, &after_all)
            .unwrap()
            .as_deref(),
        Some("author")
    );
    job(
        &h,
        "generic",
        "running",
        "session-generic",
        json!({"discord_thread_id":"200"}),
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({})))
            .unwrap()
            .as_deref(),
        Some("author")
    );
}

#[test]
fn footer_on_the_question_beats_a_later_footer_and_pasted_ids() {
    let mut h = Harness::new();
    job(&h, "job_a", "failed", "sess-a", json!({}));
    job(&h, "job_b", "succeeded", "sess-b", json!({}));
    job(&h, "job_c", "failed", "sess-c", json!({}));
    history(
        &h,
        vec![
            bot(
                "300",
                "どちらにしますか？\n-# ジョブ job_a。続きはこのスレッドに返信してください。",
            ),
            message("301", "-# ジョブ job_c。", "OWN"),
            bot(
                "302",
                "途中経過です。\n-# ジョブ job_b。続きは自動で再開します。",
            ),
        ],
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({})))
            .unwrap()
            .as_deref(),
        Some("job_a")
    );
}

#[test]
fn without_a_footer_on_the_question_the_latest_earlier_footer_wins_then_the_last_one() {
    let mut h = Harness::new();
    job(&h, "job_a", "failed", "sess-a", json!({}));
    job(&h, "job_b", "failed", "sess-b", json!({}));
    history(
        &h,
        vec![
            bot("100", "報告\n-# ジョブ job_a。"),
            bot("200", "報告\n-# ジョブ job_b。"),
            bot("300", "どちらにしますか？"),
        ],
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({})))
            .unwrap()
            .as_deref(),
        Some("job_b")
    );
    // A question registered before every footer falls back to the latest poster.
    let early = question(json!({"after":"50","question_message":"50"}));
    assert_eq!(
        steering::associated_job(&mut h.ctx, &early)
            .unwrap()
            .as_deref(),
        Some("job_b")
    );
}

#[test]
fn recorded_posting_job_comes_first() {
    let mut h = Harness::new();
    job(&h, "job_a", "failed", "sess-a", json!({}));
    job(&h, "job_b", "failed", "sess-b", json!({}));
    history(
        &h,
        vec![bot("300", "どちらにしますか？\n-# ジョブ job_a。")],
    );
    let q = question(json!({"posting_job":"job_b"}));
    assert_eq!(
        steering::associated_job(&mut h.ctx, &q).unwrap().as_deref(),
        Some("job_b")
    );
}

#[test]
fn unrelated_jobs_are_never_owners() {
    let mut h = Harness::new();
    job(&h, "no_session", "failed", "", json!({}));
    job(
        &h,
        "other_thread",
        "failed",
        "sess-x",
        json!({"discord_thread_id":"999"}),
    );
    history(
        &h,
        vec![
            bot("300", "どちらにしますか？\n-# ジョブ no_session。"),
            bot("301", "報告\n-# ジョブ other_thread。"),
            bot("302", "報告\n-# ジョブ missing_job。"),
            message(
                "303",
                "-# ジョブ no_session。\n-# ジョブ other_thread。",
                "OWN",
            ),
        ],
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({}))).unwrap(),
        None
    );
    job(&h, "pasted", "failed", "sess-p", json!({}));
    history(
        &h,
        vec![
            bot("300", "どちらにしますか？"),
            message("301", "-# ジョブ pasted。", "OWN"),
        ],
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({}))).unwrap(),
        None
    );
}

#[test]
fn choice_requests_are_recognized_but_reports_quotes_and_code_are_not() {
    for (text, expected) in [
        (
            "a. 公開する\nb. 準備を続ける\nどちらかを選んでください",
            true,
        ),
        (
            "方針を決めたいです。\na) 先に直す\nb) 先に公開\n返信してください",
            true,
        ),
        ("確認結果：\na. 料金一致\nb. 期間一致", false),
        (
            "報告です。\n> a. 公開する\n> b. 続ける\n> 選んでください",
            false,
        ),
        (
            "ログです。\n```\na. 公開する\nb. 続ける\n選んでください\n```",
            false,
        ),
        ("状態: 完了\na. x\nb. y\n選んでください", false),
        ("a/bで返信してください", true),
        ("方針です。a/bを選択してください。", true),
        ("A/Bのどちらかを選んでください", true),
        ("a/bの比較結果を報告します。", false),
        ("ログです。`a/bで返信してください`", false),
        ("前の依頼は「a/bで返信してください」でした。", false),
        ("> a/bで返信してください", false),
        ("状態: 完了\na/bで返信してください", false),
    ] {
        assert_eq!(questions::is_question(text), expected, "{text}");
    }
}

#[test]
fn posted_choice_question_has_isolated_context_and_preserves_previous_caller() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["guild_id"] = json!("G");
    h.save(&json!({"questions":[{"thread_id":"200","after":"10","question_message":"10","open":false,"session_id":"stale","project":"P"}]}));
    let path = h.root.path().join("post.txt");
    std::fs::write(&path, "a. 公開する\nb. 続ける\nどちらかを選んでください").unwrap();
    let out = cmd_post(&mut h.ctx, "200", &path, &[], true).unwrap();
    let state = h.state();
    let q = state["questions"].as_array().unwrap().last().unwrap();
    assert_eq!(q["question_message"], out["after"]);
    assert_ne!(q["thread_id"], "200");
    assert_eq!(q["work_thread_id"], "200");
    assert_eq!(q["progress_thread_id"], out["progress_thread_id"]);
    assert_ne!(q["progress_thread_id"], q["thread_id"]);
    assert_ne!(q["project"], "P");
    assert_eq!(state["questions"][0], json!({"thread_id":"200","after":"10",
        "question_message":"10","open":false,"session_id":"stale","project":"P"}));
    assert!(h.fake.borrow().posts.is_empty());
    assert!(cc_ask::core::s(&q["question_body"]).contains("a. 公開する"));
    assert_ne!(q["session_id"], "stale");
}

#[test]
fn legacy_question_resolves_failed_explicit_job_with_session_without_reading_discord() {
    let mut h = Harness::new();
    job(
        &h,
        "failed_ok",
        "failed",
        "sess-f",
        json!({"discord_thread_id":"200"}),
    );
    job(
        &h,
        "lost_nosession",
        "lost",
        "",
        json!({"discord_thread_id":"200","started_at":"2026-10-01T10:00:00+00:00"}),
    );
    let mut q = question(json!({}));
    q.as_object_mut().unwrap().remove("source");
    assert_eq!(
        steering::associated_job(&mut h.ctx, &q).unwrap().as_deref(),
        Some("failed_ok")
    );
    assert_eq!(
        steering::local_associated_job(&h.ctx, &q)
            .unwrap()
            .as_deref(),
        Some("failed_ok")
    );
    assert!(h.fake.borrow().http_calls.is_empty());
    q["source"] = json!("scheduler");
    assert!(steering::associated_job(&mut h.ctx, &q).unwrap().is_none());
}

#[test]
fn posted_question_without_session_ignores_explicit_jobs_that_have_no_session() {
    let mut h = Harness::new();
    job(
        &h,
        "running_nosession",
        "running",
        "",
        json!({"discord_thread_id":"200"}),
    );
    history(&h, vec![bot("300", "どちらにしますか？")]);
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({}))).unwrap(),
        None
    );
}

#[test]
fn question_footer_outranks_generic_local_match() {
    let mut h = Harness::new();
    job(
        &h,
        "local",
        "failed",
        "sess-l",
        json!({"discord_thread_id":"200"}),
    );
    job(&h, "poster", "failed", "sess-p", json!({}));
    history(
        &h,
        vec![bot("300", "どちらにしますか？\n-# ジョブ poster。")],
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({})))
            .unwrap()
            .as_deref(),
        Some("poster")
    );
    // Without a footer the local association is used before any older footer.
    history(
        &h,
        vec![
            bot("100", "報告\n-# ジョブ poster。"),
            bot("300", "どちらにしますか？"),
        ],
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({})))
            .unwrap()
            .as_deref(),
        Some("local")
    );
}

#[test]
fn several_live_workers_stay_ambiguous_even_with_a_footer_fallback() {
    let mut h = Harness::new();
    job(
        &h,
        "live1",
        "running",
        "sess-1",
        json!({"discord_thread_id":"200"}),
    );
    job(
        &h,
        "live2",
        "running",
        "sess-2",
        json!({"discord_thread_id":"200"}),
    );
    job(&h, "poster", "failed", "sess-p", json!({}));
    history(
        &h,
        vec![
            bot("100", "報告\n-# ジョブ poster。"),
            bot("300", "どちらにしますか？"),
        ],
    );
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({}))).unwrap(),
        None
    );
}

#[test]
fn only_the_current_bot_counts_and_identity_failure_is_an_error() {
    let mut h = Harness::new();
    job(&h, "poster", "failed", "sess-p", json!({}));
    let mut other = bot("300", "どちらにしますか？\n-# ジョブ poster。");
    other["author"]["id"] = json!("OTHERBOT");
    history(&h, vec![other]);
    assert_eq!(
        steering::associated_job(&mut h.ctx, &question(json!({}))).unwrap(),
        None
    );
    history(
        &h,
        vec![bot("300", "どちらにしますか？\n-# ジョブ poster。")],
    );
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/users/@me".into(), 1);
    assert!(steering::associated_job(&mut h.ctx, &question(json!({}))).is_err());
}
