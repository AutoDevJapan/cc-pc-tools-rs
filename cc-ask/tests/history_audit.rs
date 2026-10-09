mod common;
use cc_ask::{
    core::*,
    history_audit::{self, Args},
};
use common::*;
use serde_json::{json, Value};

#[test]
fn archive_timestamp_query_preserves_plus_timezone() {
    assert_eq!(
        history_audit::query_component("2026-10-09T12:34:56+00:00"),
        "2026-10-09T12%3A34%3A56%2B00%3A00"
    );
}

#[test]
fn partitions_are_disjoint_and_exhaustive() {
    for id in 100..300 {
        assert_eq!(
            (0..4)
                .filter(|p| history_audit::in_partition(&id.to_string(), *p, 4))
                .count(),
            1
        );
    }
    assert!(!history_audit::in_partition("100", 4, 4));
    assert!(!history_audit::in_partition("invalid", 0, 4));
}

#[test]
fn recovery_measurement_survives_new_message_and_repeat_audit() {
    let mut h = harness();
    let mut args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: true,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let first = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(first["unacquired"], 1);
    assert_eq!(first["recovered"], 1);
    assert_eq!(
        h.state()["answer_receipts"]["100"]["received_via"],
        "history-audit"
    );
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["last_message_id"] = json!("101");
    h.fake
        .borrow_mut()
        .messages
        .get_mut("100")
        .unwrap()
        .push(message("101", "followup result", "BOT"));
    args.refresh = true;
    let second = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(second["unacquired"], 1);
    assert_eq!(second["recovered"], 1);
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn bot_only_history_does_not_rewrite_shared_state() {
    let mut h = harness();
    h.fake
        .borrow_mut()
        .messages
        .insert("100".into(), vec![message("100", "agent result", "BOT")]);
    let path = h.ctx.here.join("state.json");
    let before = std::fs::metadata(&path).unwrap().modified().unwrap();
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: true,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let result = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(result["owner_messages"], 0);
    assert_eq!(
        before,
        std::fs::metadata(&path).unwrap().modified().unwrap()
    );
}
struct Guarded(Box<dyn Effects>);
impl Effects for Guarded {
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        self.0.http(m, u, b, d)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        if a.iter().any(|v| v == "--guard-path") {
            return Ok(Output {
                stdout: "{\"client_resource\":false}".into(),
                ..Default::default()
            });
        }
        if a.iter().any(|v| v == "status") && a.iter().any(|v| v == "--all") {
            let path = std::path::Path::new(&a[0])
                .parent()
                .unwrap()
                .join("reader-index.json");
            return Ok(Output {
                stdout: std::fs::read_to_string(path).unwrap_or_else(|_| "[]".into()),
                ..Default::default()
            });
        }
        self.0.run(a, t)
    }
    fn now(&self) -> f64 {
        self.0.now()
    }
}

#[test]
fn reader_session_resolves_to_own_job_with_successful_receipt_event() {
    let mut h = harness();
    let job = h.ctx.jobs.join("jobs").join("reader");
    std::fs::create_dir_all(&job).unwrap();
    atomic_json(
        &job.join("job.json"),
        &json!({"id":"reader","status":"running","discord_thread_id":"100"}),
        2,
    )
    .unwrap();
    atomic_json(
        &h.ctx.jobs.join("reader-index.json"),
        &json!([{"id":"reader","thread_id":"reader-session","status":"running"}]),
        2,
    )
    .unwrap();
    let event = json!({"type":"item.completed","item":{"type":"command_execution","command":"discord_ask.exe replies --thread 100","exit_code":0,"aggregated_output":json!([{"thread_id":"100","replies":[{"id":"100"}]}]).to_string()}});
    std::fs::write(job.join("events.jsonl"), format!("{event}\n")).unwrap();
    h.save(&json!({"questions":[],"requests":{},"answer_receipts":{"100":{"thread":"100","read_by":"reader-session"}}}));
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: false,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let result = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(result["unproven_delivery"], 0);
    let checkpoint = read_json(&args.output).unwrap();
    assert!(
        checkpoint["rows"]["100"]["owner_messages"][0]["evidence"]["workers"]
            .as_array()
            .unwrap()
            .iter()
            .any(|w| w["job"] == "reader" && w["successful_reply_read"] == true)
    );
    assert!(h.fake.borrow().launches.is_empty());
}

#[test]
fn legacy_minute_and_text_do_not_prove_an_exact_reply_id() {
    let mut h = harness();
    let root = h.ctx.jobs.join("jobs").join("worker");
    std::fs::create_dir_all(&root).unwrap();
    atomic_json(&root.join("job.json"),&json!({"id":"worker","status":"succeeded","created_at":"2026-10-01T00:00:00Z","discord_thread_id":"100"}),2).unwrap();
    std::fs::write(
        root.join("prompt.md"),
        "### オーナー（2026-09-30T10:00）\nREADMEを直して",
    )
    .unwrap();
    std::fs::write(
        root.join("events.jsonl"),
        "{\"item\":{\"type\":\"agent_message\"}}\n",
    )
    .unwrap();
    std::fs::write(root.join("last_message.md"), "状態: 完了\n反映しました").unwrap();
    h.save(&json!({"questions":[],"requests":{},"answer_receipts":{"100":{"thread":"100","handled_by":"worker"}}}));
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: false,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let result = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(result["unproven_delivery"], 1);
    assert_eq!(result["worker_finished_after_reply"], 0);
}

#[test]
fn operator_confirmed_read_still_requires_exact_successful_event() {
    let mut h = harness();
    let root = h.ctx.jobs.join("jobs").join("worker");
    std::fs::create_dir_all(&root).unwrap();
    atomic_json(
        &root.join("job.json"),
        &json!({"id":"worker","status":"running","discord_thread_id":"100"}),
        2,
    )
    .unwrap();
    let event = json!({"type":"item.completed","item":{"type":"command_execution","command":"discord_ask.exe replies --thread 100 --read-only","exit_code":0,"aggregated_output":json!([{"thread_id":"100","replies":[{"id":"100"}]}]).to_string()}});
    std::fs::write(root.join("events.jsonl"), format!("{event}\n")).unwrap();
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: false,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    h.save(&json!({"questions":[],"requests":{},"answer_receipts":{"100":{"thread":"100","handled_by":"worker"}}}));
    assert_eq!(
        history_audit::run(&mut h.ctx, &args).unwrap()["unproven_delivery"],
        1
    );
    let mut state = h.state();
    state["answer_receipts"]["100"]["handled_via"] = json!("manual-confirmation");
    h.save(&state);
    let mut report = read_json(&args.output).unwrap();
    report["rows"]["100"]["last_message_id"] = serde_json::Value::Null;
    atomic_json(&args.output, &report, 2).unwrap();
    assert_eq!(
        history_audit::run(&mut h.ctx, &args).unwrap()["unproven_delivery"],
        0
    );
    std::fs::write(
        root.join("events.jsonl"),
        "{\"item\":{\"type\":\"agent_message\"}}\n",
    )
    .unwrap();
    let mut report = read_json(&args.output).unwrap();
    report["rows"]["100"]["last_message_id"] = serde_json::Value::Null;
    atomic_json(&args.output, &report, 2).unwrap();
    assert_eq!(
        history_audit::run(&mut h.ctx, &args).unwrap()["unproven_delivery"],
        1
    );
}
fn harness() -> Harness {
    let mut h = Harness::new();
    h.ctx.effects = Box::new(Guarded(Box::new(SharedFake(h.fake.clone()))));
    for t in h.fake.borrow_mut().threads.values_mut() {
        t["guild_id"] = json!("G");
        t["last_message_id"] = t["id"].clone();
    }
    h.save(&json!({"questions":[],"requests":{},"answer_receipts":{}}));
    h
}
#[test]
fn receipt_and_reply_ids_are_exact_not_monotonic_cursor() {
    let state = json!({"owner_answers":{"100":"999"},"questions":[{"thread_id":"100","owner_replies":[{"id":"102"}]}]});
    assert!(!history_audit::captured(&state, &json!({}), "100", "101"));
    assert!(history_audit::captured(&state, &json!({}), "100", "102"));
}
#[test]
fn audits_more_than_one_hundred_messages_without_consuming_or_posting() {
    let mut h = harness();
    h.fake.borrow_mut().messages.insert(
        "100".into(),
        (100..=304)
            .map(|id| message(&id.to_string(), "private body", "OWN"))
            .collect(),
    );
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: false,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(summary["messages"], 205);
    assert_eq!(summary["owner_messages"], 205);
    assert_eq!(summary["unacquired"], 205);
    assert_eq!(
        before,
        std::fs::read(h.ctx.here.join("state.json")).unwrap()
    );
    let report = std::fs::read_to_string(&args.output).unwrap();
    assert!(!report.contains("private body"));
    let f = h.fake.borrow();
    assert!(f
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).contains("&before=")));
    assert!(f.posts.is_empty() && f.patches.is_empty() && f.launches.is_empty());
}
#[test]
fn captures_archived_closed_question_without_claiming_delivery() {
    let mut h = harness();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["thread_metadata"]["archived"] =
        json!(true);
    h.fake.borrow_mut().messages.insert(
        "100".into(),
        vec![
            message("100", "old question", "BOT"),
            message("101", "answer", "OWN"),
        ],
    );
    h.save(&json!({"questions":[{"thread_id":"100","after":"100","open":false,"title":"Old"}],"requests":{},"answer_receipts":{}}));
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: true,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(summary["recovered"], 1);
    assert_eq!(h.state()["questions"][0]["open"], false);
    assert_eq!(h.state()["questions"][0]["owner_replies"][0]["id"], "101");
    assert!(!reply_delivered(&h.state(), "101"));
    let f = h.fake.borrow();
    assert!(f.posts.is_empty() && f.patches.is_empty() && f.launches.is_empty());
}
#[test]
fn client_thread_is_excluded_before_history_read() {
    let mut h = harness();
    std::fs::write(&h.ctx.client_projects, "Protected").unwrap();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["name"] = json!("Protected");
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: true,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(summary["excluded_clients"], 1);
    assert!(!h
        .fake
        .borrow()
        .http_calls
        .iter()
        .any(|c| s(&c["url"]).contains("/channels/100/messages")));
}
#[test]
fn failure_is_recorded_and_never_counted_as_audited() {
    let mut h = harness();
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/100/messages?limit=100".into(), 1);
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: false,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(summary["errors"], 1);
    assert_eq!(summary["inspected_threads"], 1);
    assert_eq!(summary["messages"], 0);
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(summary["errors"], 0);
    assert_eq!(summary["messages"], 1);
}

#[test]
fn incomplete_inventory_is_rediscovered_without_refresh_flag() {
    let mut h = harness();
    h.save(&json!({"questions":[],"requests":{"999":{}},"answer_receipts":{}}));
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: false,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let first = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(first["inventory_complete"], false);
    assert_eq!(first["unavailable"], 1);
    h.fake.borrow_mut().threads.insert("999".into(),json!({"id":"999","guild_id":"G","type":11,"parent_id":REQUEST_CHANNEL_ID,"name":"Other","last_message_id":"999","thread_metadata":{"archived":true}}));
    let second = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(second["inventory_complete"], true);
    assert_eq!(second["unavailable"], 0);
    assert_eq!(second["inspected_threads"], 3);
}

#[test]
fn removed_thread_rows_do_not_contribute_to_current_summary() {
    let r = json!({"inventory":{"complete":true,"threads":{"100":{}},"unavailable":[]},"rows":{"100":{"message_count":1,"owner_messages":[]},"200":{"message_count":999,"owner_messages":[{"id":"201"}]}},"errors":{}});
    let out = history_audit::summary(&r);
    assert_eq!(out["messages"], 1);
    assert_eq!(out["owner_messages"], 0);
    assert_eq!(out["inspected_threads"], 1);
}

struct ProtectDuringHistory {
    inner: Box<dyn Effects>,
    state_path: std::path::PathBuf,
}
impl Effects for ProtectDuringHistory {
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        let out = self.inner.http(m, u, b, d)?;
        if u.contains("/channels/100/messages?") {
            atomic_json(
                &self.state_path,
                &json!({"questions":[{"thread_id":"100","after":"100","cwd":"D:/Protected"}],"requests":{},"answer_receipts":{}}),
                2,
            )?;
        }
        Ok(out)
    }
    fn run(&mut self, a: &[String], t: u64) -> Result<Output> {
        self.inner.run(a, t)
    }
    fn now(&self) -> f64 {
        self.inner.now()
    }
}

#[test]
fn protection_changed_during_network_read_prevents_capture() {
    let mut h = harness();
    std::fs::write(&h.ctx.client_projects, "Protected").unwrap();
    h.fake
        .borrow_mut()
        .messages
        .insert("100".into(), vec![message("101", "answer", "OWN")]);
    h.ctx.effects = Box::new(ProtectDuringHistory {
        inner: Box::new(Guarded(Box::new(SharedFake(h.fake.clone())))),
        state_path: h.ctx.here.join("state.json"),
    });
    let args = Args {
        output: h.root.path().join("audit.json"),
        limit: 10,
        capture: true,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    let out = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(out["errors"], 1);
    assert!(h.state()["questions"][0]["owner_replies"].is_null());
    assert!(h.state()["answer_receipts"]["101"].is_null());
}
