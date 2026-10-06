mod common;
use cc_ask::{commands::cmd_remind, snooze::*};
use common::{message, Harness};
use serde_json::json;

fn epoch(text: &str) -> f64 {
    chrono::DateTime::parse_from_rfc3339(text)
        .unwrap()
        .timestamp() as f64
}
fn mid(at: f64) -> String {
    (((at * 1000.) as u64 - 1420070400000) << 22).to_string()
}

#[test]
fn supported_japanese_dates_times_and_durations() {
    let base = epoch("2026-10-03T21:00:00+09:00");
    for (text, expected) in [
        ("再通知は明日にして", "2026-10-04T09:00:00+09:00"),
        (
            "再通知は明後日の午後3時半にして欲しい",
            "2026-10-05T15:30:00+09:00",
        ),
        (
            "再通知は２０２６年１０月５日の９時３０分にして。",
            "2026-10-05T09:30:00+09:00",
        ),
        ("再通知を10/5 09:30に変更", "2026-10-05T09:30:00+09:00"),
        (
            "再送は2026-10-05T00:30:00Zにして",
            "2026-10-05T00:30:00+00:00",
        ),
        ("再通知は9時にして", "2026-10-04T09:00:00+09:00"),
        ("再通知は2時間後にして", "2026-10-03T23:00:00+09:00"),
        ("再メンションは30分後にして", "2026-10-03T21:30:00+09:00"),
        ("リマインドは今すぐにして", "2026-10-03T21:00:00+09:00"),
        (
            "明日外出します。\n再通知は2026/10/6にして",
            "2026-10-06T09:00:00+09:00",
        ),
        ("再通知は2027/1/1 0時にして", "2027-01-01T00:00:00+09:00"),
    ] {
        assert_eq!(parse(text, base).unwrap().to_rfc3339(), expected, "{text}");
    }
}

#[test]
fn does_not_guess_invalid_ambiguous_past_or_quoted_instructions() {
    let base = epoch("2026-10-03T21:00:00+09:00");
    for text in [
        "再通知は10/2にして",
        "再通知は今日9時にして",
        "再通知は10/32にして",
        "再通知は明日25時にして",
        "再通知は明日の午後13時にして",
        "再通知は明日9:61にして",
        "再通知は99999999999999999999時間後にして",
        "再通知はそのうちにして",
        "ログ: 再通知は明日にして",
        "『再通知は明日にして』と返信する機能",
        "> 再通知は明日にして",
        "```\n再通知は明日にして\n```",
        "~~~\n再通知は明日にして\n~~~",
        "```text\n~~~\n再通知は明日にして\n```",
        "````text\n```\n再通知は明日にして\n````",
        "    再通知は明日9時にして",
        "\t再通知は明日9時にして",
        "再通知は明日にして\n再通知は明後日にして",
        "明日9時にテストして",
    ] {
        assert!(parse(text, base).is_none(), "{text}");
    }
}

#[test]
fn latest_owner_instruction_is_durable_and_other_authors_cannot_change_it() {
    let base = epoch("2026-10-03T21:00:00+09:00");
    let first = message(&mid(base), "再通知は明日にして", "OWN");
    let newest = message(&mid(base + 1.), "再通知は明後日にして", "OWN");
    let other = message(&mid(base + 2.), "再通知は1時間後にして", "OTHER");
    let mut bot = message(&mid(base + 3.), "再通知は1時間後にして", "OWN");
    bot["author"]["bot"] = json!(true);
    let mut q = json!({"thread_id":"200","open":true,"unknown":"keep"});
    apply(&mut q, &[newest.clone(), first.clone(), other, bot], "OWN");
    assert_eq!(q["remind_reply"], newest["id"]);
    assert_eq!(q["remind_not_before_iso"], "2026-10-05T09:00:00+09:00");
    let saved = q.clone();
    apply(&mut q, &[first, newest], "OWN");
    assert_eq!(q, saved);
    assert_eq!(q["unknown"], "keep");
}

#[test]
fn precise_iso_deadline_keeps_milliseconds_in_saved_pause() {
    let base = epoch("2026-10-03T21:00:00+09:00");
    let m = message(
        &mid(base),
        "再通知は2026-10-04T09:00:00.500+09:00にして",
        "OWN",
    );
    let mut q = json!({});
    apply(&mut q, &[m], "OWN");
    let at = epoch("2026-10-04T09:00:00+09:00") + 0.5;
    assert_eq!(q["remind_not_before"], at);
    assert!(paused(&q, at - 0.001));
    assert!(!paused(&q, at));
}

#[test]
fn time_only_at_exact_boundary_is_today_but_elapsed_time_is_tomorrow() {
    let base = epoch("2026-10-03T09:00:00+09:00");
    assert_eq!(
        parse("再通知は9時にして", base).unwrap().to_rfc3339(),
        "2026-10-03T09:00:00+09:00"
    );
    assert_eq!(
        parse("再通知は9時にして", base + 0.5).unwrap().to_rfc3339(),
        "2026-10-04T09:00:00+09:00"
    );
}

#[test]
fn manual_deadline_wins_over_preceding_unprocessed_owner_instruction() {
    let mut h = Harness::new();
    let base = epoch("2026-10-03T21:00:00+09:00");
    h.fake.borrow_mut().now = base + 1.;
    h.save(&json!({"questions":[{"thread_id":"200","after":"0","open":true}]}));
    let old = message(&mid(base), "再通知は1時間後にして", "OWN");
    h.fake.borrow_mut().messages.insert("200".into(), vec![old]);
    cc_ask::commands::cmd_snooze(&mut h.ctx, "200", "2026-10-05T09:00:00+09:00").unwrap();
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(
        h.state()["questions"][0]["remind_not_before_iso"],
        "2026-10-05T09:00:00+09:00"
    );
    let newest = message(&mid(base + 2.), "再通知は2時間後にして", "OWN");
    h.fake
        .borrow_mut()
        .messages
        .get_mut("200")
        .unwrap()
        .push(newest);
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.state()["questions"][0]["remind_not_before"], base + 7202.);
}

#[test]
fn remind_alone_parses_reply_before_any_post_and_resumes_hourly_at_boundary() {
    let mut h = Harness::new();
    let base = epoch("2026-10-03T21:00:00+09:00");
    h.fake.borrow_mut().now = base;
    h.fake.borrow_mut().messages.insert(
        "200".into(),
        vec![message(&mid(base), "再通知は2時間後にして", "OWN")],
    );
    h.save(&json!({"questions":[{"thread_id":"200","after":mid(base - 7200.),"open":true,"title":"質問"}]}));
    cmd_remind(&mut h.ctx).unwrap();
    assert!(h.fake.borrow().posts.is_empty());
    assert_eq!(h.state()["questions"][0]["remind_not_before"], base + 7200.);
    h.fake.borrow_mut().now = base + 7200.;
    cmd_remind(&mut h.ctx).unwrap();
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 1);
    h.fake.borrow_mut().now += 3600.;
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 2);
}

#[test]
fn request_result_is_held_and_question_stays_open_during_postponement() {
    let mut h = Harness::new();
    h.tick();
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n月額1000円で公開してよいですか。\na. 公開する\nb. 公開を停止する"),
        None,
    );
    h.tick();
    let base = epoch("2026-10-03T21:00:00+09:00");
    h.fake.borrow_mut().now = base;
    h.fake.borrow_mut().sequence = mid(base).parse().unwrap();
    h.reply("再通知は1時間後にして", "100");
    let posts = h.fake.borrow().posts.len();
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), posts);
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.state()["questions"][0]["open"], true);
    h.fake.borrow_mut().now += 3600.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    cmd_remind(&mut h.ctx).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), posts + 1);
}

#[test]
fn pending_request_worker_result_survives_pause_and_is_delivered_once() {
    let mut h = Harness::new();
    h.tick();
    let base = epoch("2026-10-03T21:00:00+09:00");
    h.fake.borrow_mut().now = base;
    h.fake.borrow_mut().sequence = mid(base).parse().unwrap();
    h.reply("再通知は1時間後にして", "100");
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 要判断\n月額1000円で公開してよいですか。\na. 公開する\nb. 公開を停止する"),
        None,
    );
    let posts = h.fake.borrow().posts.len();
    h.tick();
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), posts);
    assert_eq!(h.state()["requests"]["100"]["job"], "job1");
    h.fake.borrow_mut().now += 3600.;
    h.tick();
    h.tick();
    assert_eq!(h.fake.borrow().posts.len(), posts + 1);
    assert_eq!(h.state()["questions"][0]["remind_not_before"], base + 3600.);
    assert_eq!(
        h.state()["questions"][0]["remind_reply"],
        h.state()["requests"]["100"]["remind_reply"]
    );
    assert_eq!(h.fake.borrow().launches.len(), 1);
}

#[test]
fn initial_request_failure_waits_for_deadline_before_retrying() {
    let mut h = Harness::new();
    h.tick();
    let base = epoch("2026-10-03T21:00:00+09:00");
    h.fake.borrow_mut().now = base;
    h.fake.borrow_mut().sequence = mid(base).parse().unwrap();
    h.reply("再通知は1時間後にして", "100");
    h.finish("job1", "failed", None, None);
    let posts = h.fake.borrow().posts.len();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.fake.borrow().posts.len(), posts);
    h.fake.borrow_mut().now += 3600.;
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.fake.borrow().posts.len(), posts);
}
