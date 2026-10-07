use cc_jobs::notify::*;
use cc_jobs::*;

#[test]
fn cargo_gate_windows_sys_exit_matches_python_long_conversion() {
    for (code, expected) in [
        (0, 0),
        (1, 1),
        (7, 7),
        (127, 127),
        (i32::MAX as i64, i32::MAX),
        (0xc0000005, -1),
        (0xffffffff, -1),
    ] {
        assert_eq!(cc_jobs::gate::python_cli_exit(code), expected);
    }
}

#[test]
fn cargo_cmd_preserves_a_failing_child_exit_code() {
    let temp = Temp::new();
    let bin = temp.0.join("bin");
    fs::create_dir(&bin).unwrap();
    fs::copy(
        env!("CARGO_BIN_EXE_cargo_gate"),
        temp.0.join("cargo_gate.exe"),
    )
    .unwrap();
    let launcher = bin.join("cargo.cmd");
    fs::write(
        &launcher,
        include_str!("../bin/cargo.cmd")
            .replace("\r\n", "\n")
            .replace('\n', "\r\n"),
    )
    .unwrap();
    let fixture = temp.0.join("fixture.exe");
    fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), &fixture).unwrap();
    let result = hidden_command("cmd.exe")
        .current_dir(&bin)
        .args(["/d", "/c", ".\\cargo.cmd", "--exit-seven"])
        .env("NoDefaultCurrentDirectoryInExePath", "1")
        .env("CODEX_JOBS_REAL_CARGO", &fixture)
        .env("CODEX_JOBS_CARGO_LOCK_DIR", &temp.0)
        .env("CARGO_TARGET_DIR", &temp.0)
        .env("CODEX_JOBS_CARGO_SLOTS", "1")
        .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
        .output()
        .unwrap();
    assert_eq!(
        result.status.code(),
        Some(7),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
    assert_eq!(result.stdout, b"");
    assert_eq!(result.stderr, b"");
    assert!(temp.0.join("cargo-build.lock.0").exists());
}
use std::{
    collections::{BTreeMap, HashMap, HashSet, VecDeque},
    fs,
    io::{self, Cursor, Read},
    path::{Path, PathBuf},
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, AtomicI32, AtomicU32, AtomicUsize, Ordering},
    },
    time::{Duration, Instant},
};
const OK: &str = include_str!("fixtures/OK_EVENTS.jsonl");
const FAIL: &str = include_str!("fixtures/FAIL_EVENTS.jsonl");
const SESSION: &str = include_str!("fixtures/SESSION_LINES.jsonl");
mod queue_regressions {
    use super::*;
    use cc_jobs::queue::*;
    const SESSION_ID: &str = "01a11052-a537-74f3-9611-deb742778fc5";
    fn long_parent(t: &Test, id: &str) -> Value {
        let mut j = waiting(t, id, "orch-long");
        j["status"] = json!("running");
        j["started_at"] = json!("2026-10-01T10:00:00+09:00");
        j["thread_id"] = json!(SESSION_ID);
        j["todo_id"] = json!(1956);
        t.ctx.save(&j).unwrap();
        j
    }
    fn live_tool_parent(t: &Test) -> Value {
        let mut parent = long_parent(t, "long");
        parent["queue_fairness_version"] = json!(1);
        parent["pid"] = json!(445);
        parent["pid_created"] = json!(45);
        parent["codex_pid"] = json!(444);
        parent["codex_pid_created"] = json!(44);
        t.rt.procs.lock().unwrap().extend([(444, 44), (445, 45)]);
        t.rt.pause_allowed.store(true, Ordering::Relaxed);
        t.ctx.save(&parent).unwrap();
        waiting(t, "other", "orch-other");
        parent
    }
    fn tool_events(t: &Test, events: &[(&str, &str)]) {
        let text: String = events
            .iter()
            .map(|(ty, id)| {
                format!(
                    "{}\n",
                    json!({"_ts":t.rt.now(),"type":ty,"item":{"id":id,"type":"command_execution"}})
                )
            })
            .collect();
        fs::write(t.ctx.job_dir("long").join("events.jsonl"), text).unwrap();
    }
    #[test]
    fn context_limit_rotates_a_short_job_only_at_the_existing_idle_boundary() {
        for (pct, limit, phase, expected) in [
            (50, 50, "", false),
            (51, 50, "", true),
            (60, 70, "", false),
            (90, 50, "summarize", false),
        ] {
            let t = Test::new();
            config(&t, json!({"max_running":1,"context_limit_percent":limit}));
            let mut parent = live_tool_parent(&t);
            parent["started_at"] = json!("2026-10-01T11:59:00+09:00");
            parent["context_pct"] = json!(pct);
            parent["context_phase"] = json!(phase);
            t.ctx.save(&parent).unwrap();
            tool_events(&t, &[("item.started", "cargo")]);
            assert!(t.ctx.admit("other").unwrap().is_none());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            tool_events(
                &t,
                &[("item.started", "cargo"), ("item.completed", "cargo")],
            );
            assert_eq!(t.ctx.admit("other").unwrap().is_some(), expected);
            if expected {
                let saved = t.ctx.load("long").unwrap();
                assert_eq!(saved["context_phase"], "summarize");
                assert_eq!(saved["resume_session"], SESSION_ID);
                assert_eq!(saved["todo_id"], 1956);
                assert!(
                    fs::read_to_string(t.ctx.job_dir("long").join("context-summary-prompt.md"))
                        .unwrap()
                        .contains("関係ファイルとPR")
                );
            }
        }
    }
    #[test]
    fn failed_yield_backoff_applies_to_its_attempt_but_not_mid_item() {
        let t = Test::new();
        config(&t, json!({"max_running":1,"context_limit_percent":50}));
        let mut parent = live_tool_parent(&t);
        parent["started_at"] = json!("2026-10-01T11:59:00+09:00");
        parent["context_pct"] = json!(51);
        t.ctx.save(&parent).unwrap();
        let retry = t.ctx.job_dir("long").join("yield-retry.json");
        // Mid-item checks are cheap and must not delay the next boundary.
        tool_events(&t, &[("item.started", "cargo")]);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert!(!retry.exists());
        tool_events(
            &t,
            &[("item.started", "cargo"), ("item.completed", "cargo")],
        );
        let epoch = parse_iso(&t.rt.now()).unwrap();
        let attempt = format!(
            "{}:{}",
            s(&parent, "started_at"),
            parent["codex_pid_created"]
        );
        fs::write(
            &retry,
            json!({"not_before": epoch + 20., "attempt": attempt}).to_string(),
        )
        .unwrap();
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.ctx.load("long").unwrap()["status"], "running");
        // A backoff left by an earlier Codex attempt of the same job is ignored.
        fs::write(
            &retry,
            json!({"not_before": epoch + 20., "attempt": "earlier:1"}).to_string(),
        )
        .unwrap();
        assert!(t.ctx.admit("other").unwrap().is_some());
        assert_eq!(t.ctx.load("long").unwrap()["context_phase"], "summarize");
    }
    #[test]
    fn context_handoff_resumes_for_summary_then_starts_fresh_with_the_same_todo_and_scope() {
        let t = Test::new();
        let mut j = waiting(&t, "handoff", "req-handoff");
        j["context_phase"] = json!("summarize");
        j["context_source_session"] = json!(SESSION_ID);
        j["resume_session"] = json!(SESSION_ID);
        j["thread_id"] = json!(SESSION_ID);
        j["context_pct"] = json!(90);
        j["todo_id"] = json!(1972);
        j["sandbox"] = json!("danger-full-access");
        t.ctx.save(&j).unwrap();
        let dir = t.ctx.job_dir("handoff");
        fs::write(dir.join("prompt.md"), "元の依頼全文と受入条件").unwrap();
        fs::write(
            dir.join("context-summary-prompt.md"),
            "引き継ぎだけを書いてください",
        )
        .unwrap();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let text = if seq.fetch_add(1, Ordering::Relaxed) == 0 {
                let summary = "## 目的\n消費を減らす\n## 終わったこと\nPRのpush済み\n## 残り\nテスト\n## 次の一手\ncargo test\n## 関係ファイルとPR\nD:/work/src.rs\n## 最新の指示と注意点\nRustのみ、送信を繰り返さない\n";
                format!(
                    "{}\n{}\n{}\n",
                    json!({"type":"thread.started","thread_id":SESSION_ID}),
                    json!({"type":"item.completed","item":{"type":"agent_message","text":summary}}),
                    json!({"type":"turn.completed","usage":{}})
                )
            } else {
                OK.into()
            };
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        t.ctx.supervise("handoff").unwrap();
        let saved = t.ctx.load("handoff").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["context_rotation_count"], 1);
        assert_eq!(saved["todo_id"], 1972);
        assert_eq!(saved["sandbox"], "danger-full-access");
        assert!(saved["context_pct"].is_null());
        assert!(
            fs::read_to_string(dir.join("context-continue.md"))
                .unwrap()
                .contains("PRのpush済み")
        );
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 2);
        assert!(args[0].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert!(args[0].windows(2).any(|w| w == ["-s", "read-only"]));
        assert!(!args[1].iter().any(|a| a == "resume"));
        assert!(
            args[1]
                .windows(2)
                .any(|w| w == ["-s", "danger-full-access"])
        );
    }
    #[test]
    fn context_rotation_waits_for_descendants_and_does_not_resurrect_terminal_work() {
        for case in ["child", "cancel", "turn.completed", "turn.failed"] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = live_tool_parent(&t);
            parent["context_pct"] = json!(90);
            if case == "cancel" {
                parent["cancel_requested"] = json!(true);
            }
            t.ctx.save(&parent).unwrap();
            tool_events(
                &t,
                &[("item.started", "cargo"), ("item.completed", "cargo")],
            );
            if case == "child" {
                let mut child = waiting(&t, "child", "rev-child");
                child["parent_job_id"] = json!("long");
                t.ctx.save(&child).unwrap();
            } else if case.starts_with("turn.") {
                use std::io::Write;
                writeln!(
                    fs::OpenOptions::new()
                        .append(true)
                        .open(t.ctx.job_dir("long").join("events.jsonl"))
                        .unwrap(),
                    "{}",
                    json!({"type":case,"_ts":t.rt.now()})
                )
                .unwrap();
            }
            let admission = t.ctx.admit("other").unwrap();
            if ["child", "cancel"].contains(&case) {
                assert!(admission.is_none(), "{case}");
            }
            assert!(t.rt.kills.lock().unwrap().is_empty(), "{case}");
            assert_ne!(
                t.ctx.load("long").unwrap()["context_phase"],
                "summarize",
                "{case}"
            );
        }
    }
    #[test]
    fn context_uses_unrounded_usage_and_disables_discovered_tools_for_the_summary() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut parent = live_tool_parent(&t);
        parent["started_at"] = json!("2026-10-01T11:59:00+09:00");
        parent["mcp_profile"] = json!("full");
        apply_token_count(
            &mut parent,
            &json!({"info":{"model_context_window":1000,"last_token_usage":{"total_tokens":501}}}),
        );
        assert_eq!(parent["context_pct"], 50);
        t.ctx.save(&parent).unwrap();
        tool_events(
            &t,
            &[("item.started", "cargo"), ("item.completed", "cargo")],
        );
        assert!(t.ctx.admit("other").unwrap().is_some());
        let summary = t.ctx.load("long").unwrap();
        let args = codex_args(&summary, &t.ctx.job_dir("long"));
        for required in [
            "features.plugins=false",
            "features.apps=false",
            "features.shell_tool=false",
            "web_search=disabled",
            "mcp_servers.stripe.enabled=false",
            "mcp_servers.unseen-server.enabled=false",
        ] {
            assert!(args.iter().any(|a| a == required), "{required}");
        }
        assert!(!args.iter().any(|a| a.contains("enabled=true")));
    }
    #[test]
    fn invalid_or_unwritable_context_summary_retains_the_session_and_never_reports_success() {
        for broken in ["content", "context-continue.md", "context-handoff.md"] {
            let t = Test::new();
            let mut j = waiting(&t, "handoff", "req-handoff");
            j["context_phase"] = json!("summarize");
            j["context_source_session"] = json!(SESSION_ID);
            j["resume_session"] = json!(SESSION_ID);
            j["thread_id"] = json!(SESSION_ID);
            t.ctx.save(&j).unwrap();
            let dir = t.ctx.job_dir("handoff");
            fs::write(dir.join("context-summary-prompt.md"), "summary").unwrap();
            if broken != "content" {
                fs::create_dir(dir.join(broken)).unwrap();
            }
            let summary = if broken == "content" {
                "不足した引き継ぎ"
            } else {
                "## 目的\n1\n## 終わったこと\n2\n## 残り\n3\n## 次の一手\n4\n## 関係ファイルとPR\n5\n## 最新の指示と注意点\n6"
            };
            *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
                Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(format!("{}\n{}\n{}\n", json!({"type":"thread.started","thread_id":SESSION_ID}), json!({"type":"item.completed","item":{"type":"agent_message","text":summary}}), json!({"type":"turn.completed"})).into_bytes())))
            })
            }));
            t.ctx.supervise("handoff").unwrap();
            let saved = t.ctx.load("handoff").unwrap();
            assert_eq!(saved["status"], "failed", "{broken}");
            assert_eq!(saved["resume_session"], SESSION_ID);
            assert_eq!(saved["thread_id"], SESSION_ID);
            assert_eq!(saved["context_phase"], "summarize");
            assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        }
    }
    fn collab(t: &Test, id: &str, tool: &str, child: &str, status: &str) {
        use std::io::Write;
        let path = t.ctx.job_dir("long").join("events.jsonl");
        let mut file = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(path)
            .unwrap();
        writeln!(file,"{}",json!({"_ts":t.rt.now(),"type":"item.started","item":{"id":id,"type":"collab_tool_call","tool":tool}})).unwrap();
        writeln!(file,"{}",json!({"_ts":t.rt.now(),"type":"item.completed","item":{"id":id,"type":"collab_tool_call","tool":tool,"receiver_thread_ids":[child],"status":"completed","agents_states":{child:{"status":status}}}})).unwrap();
    }
    #[test]
    fn native_subagents_block_yield_until_wait_or_close_reports_terminal_state() {
        for close in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            live_tool_parent(&t);
            let child = "01a111c8-1034-7ac1-9999-aaaaaaaaaaaa";
            collab(&t, "spawn", "spawn_agent", child, "running");
            collab(&t, "timeout", "wait", child, "running");
            assert!(t.ctx.admit("other").unwrap().is_none());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            collab(
                &t,
                "done",
                if close { "close_agent" } else { "wait" },
                child,
                "completed",
            );
            assert!(t.ctx.admit("other").unwrap().is_some());
            assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
        }
    }
    #[test]
    fn subagent_rollout_requires_its_own_completed_turn_and_never_reuses_an_old_output_after_send_input()
     {
        for send_input in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            live_tool_parent(&t);
            let child = "01a111c8-1034-7ac1-9999-aaaaaaaaaaaa";
            collab(&t, "spawn", "spawn_agent", child, "running");
            let dir = t.ctx.codex_home.join("sessions/2026/10/01");
            fs::create_dir_all(&dir).unwrap();
            let path = dir.join(format!("rollout-test-{child}.jsonl"));
            let events = [
                json!({"type":"session_meta","payload":{"id":child,"timestamp":t.rt.now()}}),
                json!({"timestamp":"2026-10-01T11:59:00+09:00","type":"event_msg","payload":{"type":"task_complete","turn_id":"old-parent"}}),
                json!({"timestamp":t.rt.now(),"type":"event_msg","payload":{"type":"task_started","turn_id":"child-turn"}}),
            ];
            let prefix = events.iter().map(|v| format!("{v}\n")).collect::<String>();
            fs::write(&path, &prefix).unwrap();
            assert!(t.ctx.admit("other").unwrap().is_none());
            fs::write(&path, format!("{prefix}{{")).unwrap();
            assert!(t.ctx.admit("other").unwrap().is_none());
            fs::write(&path,format!("{prefix}{}\n",json!({"timestamp":t.rt.now(),"type":"event_msg","payload":{"type":"task_complete","turn_id":"child-turn","last_agent_message":"結果: 送信は完了済み"}}))).unwrap();
            if send_input {
                collab(&t, "more", "send_input", child, "running");
                assert!(t.ctx.admit("other").unwrap().is_none());
                collab(&t, "joined", "wait", child, "completed");
            }
            assert!(t.ctx.admit("other").unwrap().is_some());
            let resume = fs::read_to_string(t.ctx.job_dir("long").join("yield-resume.md")).unwrap();
            if send_input {
                assert!(!resume.contains("結果: 送信は完了済み"));
            } else {
                assert!(resume.contains(child));
                assert!(resume.contains("結果: 送信は完了済み"));
            }
        }
    }
    #[test]
    fn legacy_running_turn_stops_only_at_tool_completion_and_gives_waiter_the_slot() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        live_tool_parent(&t);
        tool_events(&t, &[("item.started", "cargo")]);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert!(t.rt.kills.lock().unwrap().is_empty());
        tool_events(
            &t,
            &[("item.started", "cargo"), ("item.completed", "cargo")],
        );
        assert!(t.ctx.admit("other").unwrap().is_some());
        let j = t.ctx.load("long").unwrap();
        assert_eq!(j["status"], "queued");
        assert_eq!(j["resume_session"], SESSION_ID);
        assert_eq!(j["todo_id"], 1956);
        assert_eq!(j["yield_boundary_item"], "cargo");
        assert_eq!(j["yield_count"], 1);
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![445, 444]);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert!(j["turn_completed"].is_null());
    }
    #[test]
    fn new_item_racing_the_pause_prevents_stop_and_background_workers_delay_yield() {
        for background in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            live_tool_parent(&t);
            tool_events(
                &t,
                &[("item.started", "first"), ("item.completed", "first")],
            );
            if background {
                t.rt.pause_allowed.store(false, Ordering::Relaxed);
            } else {
                let path = t.ctx.job_dir("long").join("events.jsonl");
                let now = t.rt.now();
                *t.rt.pause_hook.lock().unwrap() = Some(Box::new(move || {
                    use std::io::Write;
                    writeln!(fs::OpenOptions::new().append(true).open(&path).unwrap(),"{}",
                        json!({"_ts":now,"type":"item.started","item":{"id":"next","type":"mcp_tool_call"}})).unwrap();
                }));
            }
            assert!(t.ctx.admit("other").unwrap().is_none());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            assert_eq!(t.ctx.load("long").unwrap()["status"], "running");
        }
    }
    #[test]
    fn failed_spawn_after_stop_retries_the_same_durable_session_without_double_yield() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        live_tool_parent(&t);
        tool_events(&t, &[("item.started", "tool"), ("item.completed", "tool")]);
        t.rt.supervisor_fail.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_some());
        assert_eq!(t.ctx.load("long").unwrap()["status"], "queued");
        assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
        t.rt.supervisor_fail.store(false, Ordering::Relaxed);
        assert!(t.ctx.admit("long").unwrap().is_none());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        assert_eq!(t.ctx.load("long").unwrap()["resume_session"], SESSION_ID);
        assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
    }
    #[test]
    fn legacy_stop_failure_retains_the_family_until_all_recorded_controllers_are_dead() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let original = live_tool_parent(&t);
        tool_events(&t, &[("item.started", "tool"), ("item.completed", "tool")]);
        t.rt.paused_stop_failure_pid.store(444, Ordering::Relaxed);
        t.rt.paused_stop_failure.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_none());
        t.rt.pause_allowed.store(false, Ordering::Relaxed);
        assert_eq!(t.ctx.refresh(original, false).unwrap()["status"], "running");
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![445]);
        t.rt.pause_allowed.store(false, Ordering::Relaxed);
        t.rt.pause_recoveries.lock().unwrap().clear();
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert!(
            !t.rt
                .pause_recoveries
                .lock()
                .unwrap()
                .iter()
                .any(|id| id == "long")
        );
        t.rt.pause_allowed.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_some());
        let queued = t.ctx.load("long").unwrap();
        assert_eq!(queued["status"], "queued");
        assert_eq!(queued["yield_count"], 1);
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![445, 444]);
    }
    #[test]
    fn new_supervisor_stops_a_live_turn_and_resumes_after_another_job() {
        live_supervisor_handoff(false, false);
    }
    #[test]
    fn refresh_commits_pending_handoff_and_returns_the_new_queued_state() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let original = live_tool_parent(&t);
        tool_events(&t, &[("item.started", "tool"), ("item.completed", "tool")]);
        t.rt.paused_stop_failure_pid.store(444, Ordering::Relaxed);
        t.rt.paused_stop_failure.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_none());
        let refreshed = t.ctx.refresh(original, false).unwrap();
        assert_eq!(refreshed["status"], "queued");
        assert_eq!(t.ctx.load("long").unwrap()["status"], "queued");
        assert!(refreshed["queue_sequence"].as_u64().unwrap() > 0);
    }
    #[test]
    fn supervisor_keeps_monitoring_after_stop_failure_and_recovers_after_save_failure() {
        live_supervisor_handoff(true, false);
        live_supervisor_handoff(false, true);
    }
    fn live_supervisor_handoff(stop_failure: bool, save_failure: bool) {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.rt.pause_allowed.store(true, Ordering::Relaxed);
        t.rt.paused_stop_failure
            .store(stop_failure, Ordering::Relaxed);
        t.rt.fail_queued_save.store(save_failure, Ordering::Relaxed);
        waiting(&t, "long", "orch-long");
        fs::write(t.ctx.job_dir("long").join("prompt.md"), "original").unwrap();
        let ctx = t.ctx.clone();
        let rt = t.rt.clone();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let first = seq.fetch_add(1, Ordering::Relaxed) == 0;
            let text = if first {
                rt.tick(7201.);
                rt.procs.lock().unwrap().insert(321, 99);
                rt.child_alive.store(true, Ordering::Relaxed);
                fs::create_dir_all(ctx.job_dir("other")).unwrap();
                ctx.save(&job(json!({"id":"other","name":"orch-other","pid":std::process::id(),"pid_created":123,"queued_at":"2026-10-01T11:59:59+09:00"}))).unwrap();
                {
                    let _gate = ctx.queue_lock().unwrap();
                    ctx.track_queued_job("other").unwrap();
                }
                format!(
                    "{}\n{}\n{}\n{}\n",
                    json!({"type":"thread.started","thread_id":SESSION_ID}),
                    json!({"type":"turn.started"}),
                    json!({"type":"item.started","item":{"type":"command_execution","id":"build"}}),
                    json!({"type":"item.completed","item":{"type":"command_execution","id":"build"}})
                )
            } else {
                format!(
                    "{}\n{}\n",
                    json!({"type":"item.completed","item":{"type":"agent_message","text":"状態: 完了"}}),
                    json!({"type":"turn.completed"})
                )
            };
            Box::new(FakeChild {
                alive: if first {
                    rt.child_alive.clone()
                } else {
                    Arc::new(AtomicBool::new(false))
                },
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        let ctx = t.ctx.clone();
        let observed = Arc::new(AtomicBool::new(false));
        let seen = observed.clone();
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds == 3. && !seen.swap(true, Ordering::Relaxed) {
                assert_eq!(ctx.load("long").unwrap()["yield_boundary_item"], "build");
                assert!(ctx.admit("other").unwrap().is_some());
                let mut j = ctx.load("other").unwrap();
                j["status"] = json!("succeeded");
                ctx.save(&j).unwrap();
            }
        }));
        t.ctx.supervise("long").unwrap();
        assert!(observed.load(Ordering::Relaxed));
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 2);
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![321]);
    }
    #[test]
    fn long_turn_yields_then_supervisor_resumes_same_session_after_another_job() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        let mut j = waiting(&t, "long", "orch-long");
        j["todo_id"] = json!(1956);
        t.ctx.save(&j).unwrap();
        fs::write(t.ctx.job_dir("long").join("prompt.md"), "original task").unwrap();
        let ctx = t.ctx.clone();
        let rt = t.rt.clone();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            if seq.fetch_add(1, Ordering::Relaxed) == 0 {
                rt.tick(7201.);
                fs::create_dir_all(ctx.job_dir("other")).unwrap();
                ctx.save(&job(json!({"id":"other","name":"orch-other","pid":std::process::id(),"pid_created":123,"queued_at":"2026-10-01T11:59:59+09:00"}))).unwrap();
                let _gate = ctx.queue_lock().unwrap();
                ctx.track_queued_job("other").unwrap();
            }
            let text = format!(
                "{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"item.completed\",\"item\":{{\"type\":\"agent_message\",\"text\":\"状態: 継続\"}}}}\n{{\"type\":\"turn.completed\"}}\n"
            );
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        let ctx = t.ctx.clone();
        let seen = Arc::new(AtomicBool::new(false));
        let observed = seen.clone();
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds == 3. && !observed.swap(true, Ordering::Relaxed) {
                let yielded = ctx.load("long").unwrap();
                assert_eq!(yielded["status"], "queued");
                assert_eq!(yielded["resume_session"], SESSION_ID);
                assert_eq!(yielded["todo_id"], 1956);
                assert_eq!(yielded["yield_last_message"], "状態: 継続");
                assert!(ctx.admit("other").unwrap().is_some());
                let mut other = ctx.load("other").unwrap();
                other["status"] = json!("succeeded");
                ctx.save(&other).unwrap();
            }
        }));
        t.ctx.supervise("long").unwrap();
        assert!(seen.load(Ordering::Relaxed));
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 2);
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        let saved = t.ctx.load("long").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["yield_count"], 1);
        assert_eq!(saved["todo_id"], 1956);
        assert_eq!(
            fs::read_to_string(t.ctx.job_dir("long").join("prompt.md")).unwrap(),
            "original task"
        );
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn legacy_supervisor_is_not_stopped_and_finished_turn_is_requeued_once() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut parent = long_parent(&t, "long");
        waiting(&t, "other", "orch-other");
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.ctx.load("long").unwrap()["status"], "running");
        assert!(t.rt.kills.lock().unwrap().is_empty());
        let owner = t.ctx.job_lock("long", "supervisor.lock").unwrap();
        let mut blocker = waiting(&t, "blocker", "orch-blocker");
        blocker["status"] = json!("running");
        t.ctx.save(&blocker).unwrap();
        finish(&mut parent, Some(0), 7200000, &t.rt.now());
        parent["turn_completed"] = json!(true);
        parent["status"] = json!("succeeded");
        parent["last_message"] = json!("状態: 継続中");
        t.ctx.save(&parent).unwrap();
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        drop(owner);
        assert!(t.ctx.admit("other").unwrap().is_none());
        let yielded = t.ctx.load("long").unwrap();
        assert_eq!(yielded["status"], "queued");
        assert_eq!(yielded["resume_session"], SESSION_ID);
        assert_eq!(yielded["todo_id"], 1956);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn parent_waiting_for_children_or_grandchildren_never_yields() {
        for state in ["queued", "running"] {
            for grandchild in [false, true] {
                let t = Test::new();
                config(&t, json!({"max_running":1}));
                long_parent(&t, "long");
                waiting(&t, "other", "req-other");
                child(&t, "child", "long");
                let id = if grandchild {
                    let mut ended = t.ctx.load("child").unwrap();
                    ended["status"] = json!("succeeded");
                    t.ctx.save(&ended).unwrap();
                    child(&t, "grandchild", "child");
                    "grandchild"
                } else {
                    "child"
                };
                let mut pending = t.ctx.load(id).unwrap();
                pending["status"] = json!(state);
                t.ctx.save(&pending).unwrap();
                assert!(t.ctx.admit("other").unwrap().is_none());
                assert!(!t.ctx.jobs.join(".queue-yields.json").exists());
                assert!(t.rt.kills.lock().unwrap().is_empty());
                pending["status"] = json!("succeeded");
                t.ctx.save(&pending).unwrap();
                assert!(t.ctx.admit("other").unwrap().is_none());
                assert!(t.ctx.jobs.join(".queue-yields.json").exists());
            }
        }
    }
    #[test]
    fn yield_reservation_is_withdrawn_when_a_child_starts_or_waiters_disappear() {
        for child_starts in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = long_parent(&t, "long");
            waiting(&t, "other", "orch-other");
            assert!(t.ctx.admit("other").unwrap().is_none());
            if child_starts {
                child(&t, "child", "long");
            } else {
                let mut other = t.ctx.load("other").unwrap();
                other["cancel_requested"] = json!(true);
                t.ctx.save(&other).unwrap();
            }
            parent["status"] = json!("succeeded");
            parent["turn_completed"] = json!(true);
            t.ctx.save(&parent).unwrap();
            t.ctx.admit("other").unwrap();
            assert_eq!(t.ctx.load("long").unwrap()["status"], "succeeded");
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        }
    }
    #[test]
    fn yield_requires_two_hours_a_waiter_a_session_and_unfinished_success() {
        for case in [
            "young",
            "no-waiter",
            "no-session",
            "complete",
            "failed",
            "cancelled",
            "cooldown",
            "child",
        ] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = long_parent(&t, "long");
            if case == "young" {
                parent["started_at"] = json!("2026-10-01T10:00:01+09:00");
            }
            if case == "child" {
                parent["parent_job_id"] = json!("ancestor");
            }
            t.ctx.save(&parent).unwrap();
            if case != "no-waiter" {
                let mut other = waiting(&t, "other", "orch-other");
                if case == "cooldown" {
                    other["retry_not_before"] = json!(t.rt.epoch() + 60.);
                    t.ctx.save(&other).unwrap();
                }
                t.ctx.admit("other").unwrap();
            }
            parent["status"] = json!("succeeded");
            parent["turn_completed"] = json!(true);
            match case {
                "no-session" => parent["thread_id"] = Value::Null,
                "complete" => parent["last_message"] = json!("状態: 完了\n依頼を完了しました。"),
                "failed" => parent["status"] = json!("failed"),
                "cancelled" => parent["cancel_requested"] = json!(true),
                _ => (),
            }
            t.ctx.save(&parent).unwrap();
            if case != "no-waiter" {
                t.ctx.admit("other").unwrap();
            }
            assert_ne!(t.ctx.load("long").unwrap()["status"], "queued", "{case}");
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{case}");
        }
    }
    #[test]
    fn owner_then_shepherd_then_other_jobs_use_fifo_within_each_class() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        for (id, name, time) in [
            ("other", "orch-other", "08:00:00"),
            ("shep-new", "shep-new", "11:00:00"),
            ("req-new", "req-new", "11:30:00"),
            ("shep-old", "shep-old", "09:00:00"),
            ("req-old", "req-old", "10:00:00"),
        ] {
            let mut j = waiting(&t, id, name);
            j["queued_at"] = json!(format!("2026-10-01T{time}+09:00"));
            t.ctx.save(&j).unwrap();
        }
        for id in ["req-old", "req-new", "shep-old", "shep-new", "other"] {
            assert!(t.ctx.admit(id).unwrap().is_some(), "{id}");
            let mut j = t.ctx.load(id).unwrap();
            j["status"] = json!("succeeded");
            t.ctx.save(&j).unwrap();
        }
    }
    #[test]
    fn stale_reservations_never_revive_finished_or_superseded_work() {
        for case in ["future-waiter", "modern", "successor", "decision"] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = long_parent(&t, "long");
            waiting(&t, "old-waiter", "orch-old");
            assert!(t.ctx.admit("old-waiter").unwrap().is_none());
            parent["status"] = json!("succeeded");
            parent["turn_completed"] = json!(true);
            parent["finished_at"] = json!(t.rt.now());
            parent["gto"] = json!({"quality":4});
            if case == "modern" {
                parent["queue_fairness_version"] = json!(1);
            }
            if case == "decision" {
                parent["last_message"] = json!("状態: 要判断\nオーナーの回答を待ちます。");
            }
            t.ctx.save(&parent).unwrap();
            if case == "successor" {
                let mut next = waiting(&t, "next", "req-reply");
                next["resume_session"] = json!(SESSION_ID);
                t.ctx.save(&next).unwrap();
            }
            if case == "future-waiter" {
                let mut old = t.ctx.load("old-waiter").unwrap();
                old["status"] = json!("cancelled");
                t.ctx.save(&old).unwrap();
                let mut future = waiting(&t, "future", "req-future");
                future["queued_at"] = json!("2026-10-01T14:00:00+09:00");
                t.ctx.save(&future).unwrap();
                t.ctx.admit("future").unwrap();
            } else {
                t.ctx.admit("old-waiter").unwrap();
            }
            assert_eq!(t.ctx.load("long").unwrap(), parent, "{case}");
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{case}");
        }
    }
    #[test]
    fn fairness_io_failures_preserve_completed_turn_results() {
        for broken in [".queue.lock", ".queue-index.json", "yield-resume.md"] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            t.rt.advance_epoch.store(true, Ordering::Relaxed);
            waiting(&t, "long", "orch-long");
            fs::write(t.ctx.job_dir("long").join("prompt.md"), "work").unwrap();
            let ctx = t.ctx.clone();
            let rt = t.rt.clone();
            *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
                rt.tick(7201.);
                fs::create_dir_all(ctx.job_dir("other")).unwrap();
                ctx.save(&job(json!({"id":"other","name":"orch-other","pid":std::process::id(),"pid_created":123}))).unwrap();
                {
                    let _gate = ctx.queue_lock().unwrap();
                    ctx.track_queued_job("other").unwrap();
                }
                if broken == "yield-resume.md" {
                    fs::create_dir(ctx.job_dir("long").join(broken)).unwrap();
                } else {
                    fs::rename(
                        ctx.jobs.join(broken),
                        ctx.jobs.join(format!("{broken}.saved")),
                    )
                    .unwrap();
                    fs::create_dir(ctx.jobs.join(broken)).unwrap();
                }
                Box::new(FakeChild {
                    alive: Arc::new(AtomicBool::new(false)),
                    out: Some(Box::new(Cursor::new(OK.as_bytes()))),
                })
            }));
            t.ctx.supervise("long").unwrap();
            let saved = t.ctx.load("long").unwrap();
            assert_eq!(saved["status"], "succeeded", "{broken}");
            assert_eq!(saved["last_message"], "DONE", "{broken}");
            assert_eq!(saved["exit_code"], 0, "{broken}");
            assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1, "{broken}");
            assert!(
                t.rt.diagnostics
                    .lock()
                    .unwrap()
                    .iter()
                    .any(|line| line.contains("枠を譲る処理を延期")),
                "{broken}"
            );
        }
    }
    #[test]
    fn one_broken_yield_reservation_never_stops_unrelated_admission() {
        for broken in ["missing", "unreadable", "spawn", "ledger"] {
            let t = Test::new();
            let mut parent = long_parent(&t, "long");
            let mut blocker = waiting(&t, "blocker", "orch-blocker");
            blocker["status"] = json!("running");
            t.ctx.save(&blocker).unwrap();
            config(&t, json!({"max_running":1}));
            waiting(&t, "other", "req-other");
            t.ctx.admit("other").unwrap();
            match broken {
                "missing" => {
                    fs::rename(t.ctx.job_dir("long"), t.ctx.root.join("moved-long")).unwrap();
                }
                "unreadable" => {
                    fs::write(t.ctx.job_dir("long").join("job.json"), "{").unwrap();
                }
                "spawn" => {
                    parent["status"] = json!("succeeded");
                    parent["turn_completed"] = json!(true);
                    parent["finished_at"] = json!(t.rt.now());
                    t.ctx.save(&parent).unwrap();
                    t.rt.supervisor_fail.store(true, Ordering::Relaxed);
                }
                "ledger" => {
                    fs::write(t.ctx.jobs.join(".queue-yields.json"), "{").unwrap();
                }
                _ => unreachable!(),
            }
            config(&t, json!({"max_running":6}));
            assert!(t.ctx.admit("other").unwrap().is_some(), "{broken}");
            if broken == "spawn" {
                assert_eq!(t.ctx.load("long").unwrap(), parent);
            }
        }
    }
    #[test]
    fn queued_supervisor_refreshes_activity_even_during_cooldown() {
        let t = Test::new();
        let mut j = waiting(&t, "heartbeat", "wait");
        j["retry_not_before"] = json!(t.rt.epoch() + 900.);
        t.ctx.save(&j).unwrap();
        let path = t.ctx.job_dir("heartbeat").join("job.json");
        let now = t.rt.now();
        let count = AtomicUsize::new(0);
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds != 3. {
                return;
            }
            let n = count.fetch_add(1, Ordering::Relaxed);
            let mut j: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
            if n == 0 {
                j["last_event_at"] = json!("2000-01-01T00:00:00+09:00");
                fs::write(&path, pretty(&j)).unwrap();
            }
            if n == 20 {
                assert_eq!(j["last_event_at"], now);
                j["status"] = json!("cancelled");
                fs::write(&path, pretty(&j)).unwrap();
            }
        }));
        t.ctx.supervise("heartbeat").unwrap();
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn reload_queue_only_restarts_waiters_and_preserves_session() {
        let t = Test::new();
        let mut j = waiting(&t, "waiting", "wait");
        j["pid"] = json!(444);
        j["pid_created"] = json!(55);
        j["resume_session"] = json!(SESSION_ID);
        t.ctx.save(&j).unwrap();
        t.rt.procs.lock().unwrap().insert(444, 55);
        t.put(job(
            json!({"id":"running","status":"running","pid":666,"pid_created":77}),
        ));
        let report = t.ctx.reload_queue().unwrap();
        assert_eq!(report, json!(["waiting"]));
        assert_eq!(*t.rt.kills.lock().unwrap(), [444]);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.load("waiting").unwrap()["resume_session"], SESSION_ID);
        assert_eq!(t.ctx.load("running").unwrap()["status"], "running");
        assert!(
            !t.rt
                .calls
                .lock()
                .unwrap()
                .iter()
                .any(|call| call.argv.first().is_some_and(|a| a == "taskkill"))
        );
    }
    #[test]
    fn queue_summary_matches_real_admission_and_fills_eight_slots() {
        let t = Test::new();
        config(&t, json!({"max_running":8}));
        for i in 0..10 {
            waiting(&t, &format!("slot-{i:02}"), "ordinary name with spaces");
        }
        for i in 0..6 {
            assert!(t.ctx.admit(&format!("slot-{i:02}")).unwrap().is_some());
        }
        let report = t
            .ctx
            .queue_report(&t.ctx.list_jobs(&HashSet::new()))
            .unwrap();
        assert_eq!(report["occupied_slots"], 6);
        assert_eq!(report["queued_count"], 4);
        assert_eq!(report["queued"][0]["reason"]["code"], "ready");
        assert_eq!(report["queued"][1]["reason"]["code"], "ready");
        assert_eq!(report["queued"][2]["reason"]["code"], "running_limit");
        assert!(t.ctx.admit("slot-06").unwrap().is_some());
        assert!(t.ctx.admit("slot-07").unwrap().is_some());
        assert!(t.ctx.admit("slot-08").unwrap().is_none());
        let (result, out, _) = t.cli(&["status", "--summary", "--json"]);
        assert_eq!(result.unwrap(), 0);
        let report: Value = serde_json::from_str(&out).unwrap();
        assert_eq!(report["summary"]["occupied_slots"], 8);
        assert_eq!(report["summary"]["queued_count"], 2);
        assert_eq!(report["jobs"][0]["name"], "ordinary name with spaces");
        let (_, line, _) = t.cli(&["status", "--summary"]);
        assert_eq!(line.lines().count(), 1);
        assert!(line.contains("実行中 8/8枠"));
    }
    #[test]
    fn wait_reasons_cover_low_priority_cooldown_children_and_yielded_jobs() {
        let t = Test::new();
        config(&t, json!({"max_running":8,"max_children_per_parent":1}));
        let mut normal = waiting(&t, "normal", "normal");
        normal["retry_not_before"] = json!(t.rt.epoch() + 60.);
        t.ctx.save(&normal).unwrap();
        let mut low = waiting(&t, "low", "normal");
        low["todo_id"] = json!(18);
        t.ctx.save(&low).unwrap();
        let mut parent = waiting(&t, "parent", "parent");
        parent["status"] = json!("running");
        t.ctx.save(&parent).unwrap();
        let mut one = child(&t, "child-one", "parent");
        one["status"] = json!("running");
        t.ctx.save(&one).unwrap();
        child(&t, "child-two", "parent");
        let mut yielded = waiting(&t, "yielded", "normal");
        yielded["yield_resume"] = json!(true);
        t.ctx.save(&yielded).unwrap();
        let report = t
            .ctx
            .queue_report(&t.ctx.list_jobs(&HashSet::new()))
            .unwrap();
        let reason = |id: &str| {
            report["queued"]
                .as_array()
                .unwrap()
                .iter()
                .find(|j| s(j, "id") == id)
                .unwrap()["reason"]["code"]
                .clone()
        };
        assert_eq!(reason("normal"), "retry_cooldown");
        assert_eq!(reason("low"), "lowest_priority");
        assert_eq!(reason("child-two"), "child_limit");
        assert_eq!(reason("yielded"), "ready");
        assert_eq!(report["occupied_slots"], 1);
        assert_eq!(report["running"], 2);
        assert!(t.ctx.admit("low").unwrap().is_none());
        assert!(t.ctx.admit("child-two").unwrap().is_none());
        assert!(t.ctx.admit("yielded").unwrap().is_some());
    }
    #[test]
    fn starting_and_unreadable_reservations_cannot_be_hidden_by_status() {
        let t = Test::new();
        config(&t, json!({"max_running":2}));
        let mut pending = waiting(&t, "starting", "starting");
        pending["status"] = json!("starting");
        t.ctx.save(&pending).unwrap();
        waiting(&t, "unreadable", "unreadable");
        fs::write(t.ctx.job_dir("unreadable").join("job.json"), "{").unwrap();
        waiting(&t, "next", "next");
        assert!(t.ctx.admit("next").unwrap().is_none());
        let (code, out, _) = t.cli(&["status", "--summary", "--json"]);
        assert_eq!(code.unwrap(), 0);
        let report: Value = serde_json::from_str(&out).unwrap();
        assert_eq!(report["summary"]["occupied_slots"], 2);
        assert_eq!(report["summary"]["queued_count"], 1);
    }
    #[test]
    fn cleanup_only_stops_terminal_identity_without_live_child_or_handoff() {
        let t = Test::new();
        for (i, id) in [
            "done",
            "live-child",
            "reused",
            "handoff",
            "running",
            "queued",
            "recent",
        ]
        .iter()
        .enumerate()
        {
            let pid = 500 + i as u32;
            let mut j = waiting(&t, id, id);
            j["pid"] = json!(pid);
            j["pid_created"] = json!(55);
            j["status"] = json!("succeeded");
            j["finished_at"] = json!(if *id == "recent" {
                t.rt.now()
            } else {
                "2026-10-01T11:57:00+09:00".into()
            });
            t.rt.procs
                .lock()
                .unwrap()
                .insert(pid, if *id == "reused" { 56 } else { 55 });
            if *id == "live-child" {
                j["codex_pid"] = json!(600);
                j["codex_pid_created"] = json!(66);
                t.rt.procs.lock().unwrap().insert(600, 66);
            }
            if ["running", "queued"].contains(id) {
                j["status"] = json!(id);
            }
            t.ctx.save(&j).unwrap();
            if *id == "handoff" {
                fs::write(
                    t.ctx.job_dir(id).join("yield-transition.json"),
                    json!({"complete":false,"source":{"id":id}}).to_string(),
                )
                .unwrap();
            }
        }
        let original = t.ctx.load("done").unwrap();
        let preview = t.ctx.cleanup_supervisors(false).unwrap();
        assert_eq!(preview.as_array().unwrap().len(), 1);
        assert!(t.rt.kills.lock().unwrap().is_empty());
        assert_eq!(t.ctx.load("done").unwrap(), original);
        assert_eq!(t.ctx.cleanup_supervisors(true).unwrap()[0]["id"], "done");
        assert_eq!(*t.rt.kills.lock().unwrap(), [500]);
        let done = t.ctx.load("done").unwrap();
        assert_eq!(done["finished_at"], original["finished_at"]);
        assert_eq!(done["status"], "succeeded");
        assert!(
            t.ctx
                .cleanup_supervisors(true)
                .unwrap()
                .as_array()
                .unwrap()
                .is_empty()
        );
    }
    #[test]
    fn invalid_queue_config_keeps_status_available_with_explicit_error() {
        let t = Test::new();
        waiting(&t, "pending", "name with spaces");
        fs::write(t.ctx.root.join("queue-config.json"), "{").unwrap();
        for args in [
            vec!["status"],
            vec!["status", "--json"],
            vec!["status", "--summary", "--json"],
        ] {
            let (result, out, _) = t.cli(&args);
            assert_eq!(result.unwrap(), 0);
            if args.contains(&"--json") {
                let data: Value = serde_json::from_str(&out).unwrap();
                if args.contains(&"--summary") {
                    assert!(!data["summary"]["config_error"].is_null());
                    assert!(data["summary"]["max_running"].is_null());
                } else {
                    assert!(data.is_array());
                    assert_eq!(data[0]["queue_wait_reason"]["code"], "config_error");
                }
            } else {
                assert!(out.contains("不明（設定エラー）"));
                assert!(out.contains("name with spaces"));
            }
        }
    }
    #[test]
    fn failed_supervisor_reload_does_not_prevent_other_waiters_upgrading() {
        let t = Test::new();
        for (id, pid) in [("a-failing", 444), ("b-good", 445)] {
            let mut j = waiting(&t, id, id);
            j["pid"] = json!(pid);
            j["pid_created"] = json!(55);
            t.ctx.save(&j).unwrap();
            t.rt.procs.lock().unwrap().insert(pid, 55);
        }
        t.rt.paused_stop_failure.store(true, Ordering::Relaxed);
        t.rt.paused_stop_failure_pid.store(444, Ordering::Relaxed);
        let report = t.ctx.reload_queue().unwrap();
        assert_eq!(report[0]["action"], "deferred");
        assert_eq!(report[1], "b-good");
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.load("a-failing").unwrap()["pid"], 444);
    }
    #[test]
    fn reload_does_not_wait_for_a_busy_registration_or_hold_the_queue_between_jobs() {
        let t = Test::new();
        let busy = waiting(&t, "busy", "busy");
        waiting(&t, "available", "available");
        let held = t.ctx.job_lock("busy", "state.lock").unwrap();
        let begin = Instant::now();
        assert_eq!(t.ctx.reload_queue().unwrap(), json!(["available"]));
        assert!(begin.elapsed() < Duration::from_secs(2));
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.load("busy").unwrap(), busy);
        drop(t.ctx.queue_lock().unwrap());
        drop(held);
        assert!(
            t.ctx
                .reload_queue()
                .unwrap()
                .as_array()
                .unwrap()
                .contains(&json!("busy"))
        );
    }
    #[test]
    fn transient_waiting_record_read_failure_does_not_fail_the_job() {
        let t = Test::new();
        waiting(&t, "wait", "wait");
        fs::write(t.ctx.job_dir("wait").join("prompt.md"), "work").unwrap();
        let path = t.ctx.job_dir("wait").join("job.json");
        let injected = Arc::new(AtomicBool::new(false));
        let seen = injected.clone();
        let runtime = Arc::downgrade(&t.rt);
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |loaded, _| {
            if loaded == path && !seen.swap(true, Ordering::Relaxed) {
                runtime
                    .upgrade()
                    .unwrap()
                    .load_failures
                    .store(50, Ordering::Relaxed);
            }
        }));
        t.ctx.supervise("wait").unwrap();
        assert!(injected.load(Ordering::Relaxed));
        assert_eq!(t.ctx.load("wait").unwrap()["status"], "succeeded");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        assert!(
            t.rt.diagnostics
                .lock()
                .unwrap()
                .iter()
                .any(|line| line.starts_with("順番待ちの状態読取りを再試行"))
        );
    }
    #[test]
    fn recovery_requeues_watchdog_cancelled_recovery_but_never_owner_cancelled() {
        for explicit in [false, true] {
            let t = Test::new();
            let mut j = failed(&t, "recover", 1, "2026-10-01T12:00:00+09:00");
            j["status"] = json!("cancelled");
            j["recovered_429_at"] = json!(t.rt.now());
            j["cancel_requested"] = json!(explicit);
            t.ctx.save(&j).unwrap();
            let todo = t.ctx.root.join("todo.json");
            fs::write(
                &todo,
                json!({"items":[{"id":1,"status":"doing","jobs":["recover"]}]}).to_string(),
            )
            .unwrap();
            let report = t
                .ctx
                .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
                .unwrap();
            if explicit {
                assert_eq!(report, json!([]));
                assert_eq!(t.ctx.load("recover").unwrap()["status"], "cancelled");
            } else {
                assert_eq!(report[0]["action"], "queued");
            }
            assert_eq!(
                t.rt.supervisors.load(Ordering::Relaxed),
                usize::from(!explicit)
            );
        }
    }
    #[test]
    fn recovered_supervisor_identity_survives_recovery_command_exit() {
        let t = Test::new();
        failed(&t, "revive", 1, "2026-10-01T11:00:00+09:00");
        t.rt.supervisor_pid.store(888, Ordering::Relaxed);
        t.rt.procs.lock().unwrap().insert(888, 777);
        let todo = t.ctx.root.join("todo.json");
        fs::write(
            &todo,
            json!({"items":[{"id":1,"status":"doing","jobs":["revive"]}]}).to_string(),
        )
        .unwrap();
        t.ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
            .unwrap();
        // The detached supervisor has not executed any code yet, and the
        // recovery process has exited. Its recorded child identity is enough.
        t.rt.procs.lock().unwrap().remove(&std::process::id());
        let j = t.ctx.refresh(t.ctx.load("revive").unwrap(), false).unwrap();
        assert_eq!(j["pid"], 888);
        assert_eq!(j["pid_created"], 777);
        assert_eq!(j["status"], "queued");
        assert!(t.ctx.supervisor_alive(&j));
    }
    #[test]
    fn damaged_index_rebuilds_and_unreadable_active_record_blocks_admission() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "one");
        assert!(t.ctx.admit("a").unwrap().is_some());
        waiting(&t, "b", "two");
        fs::write(t.ctx.jobs.join(".queue-index.json"), "{").unwrap();
        {
            let _gate = t.ctx.queue_lock().unwrap();
            t.ctx.track_queued_job("b").unwrap();
        }
        assert!(t.ctx.admit("b").unwrap().is_none());
        fs::write(t.ctx.job_dir("a").join("job.json"), "{").unwrap();
        assert!(t.ctx.admit("b").unwrap().is_none());
        assert_eq!(t.ctx.load("b").unwrap()["status"], "queued");
        let j = job(json!({"id":"a","status":"succeeded"}));
        t.ctx.save(&j).unwrap();
        assert!(t.ctx.admit("b").unwrap().is_some());
    }
    #[test]
    fn temporarily_invalid_settings_keep_waiting_job_then_admit_it() {
        let t = Test::new();
        waiting(&t, "wait", "wait");
        fs::write(t.ctx.job_dir("wait").join("prompt.md"), "work").unwrap();
        let cfg = t.ctx.root.join("queue-config.json");
        fs::write(&cfg, "{").unwrap();
        let saved = t.ctx.job_dir("wait").join("job.json");
        let seen = Arc::new(AtomicBool::new(false));
        let observed = seen.clone();
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds == 3. && !observed.swap(true, Ordering::Relaxed) {
                let j: Value = serde_json::from_slice(&fs::read(&saved).unwrap()).unwrap();
                assert_eq!(j["status"], "queued");
                fs::write(&cfg, "{\"max_running\":1}").unwrap();
            }
        }));
        t.ctx.supervise("wait").unwrap();
        assert!(seen.load(Ordering::Relaxed));
        assert_eq!(t.ctx.load("wait").unwrap()["status"], "succeeded");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        assert!(
            t.rt.diagnostics
                .lock()
                .unwrap()
                .iter()
                .any(|s| s.starts_with("順番待ちを継続"))
        );
    }
    #[test]
    fn waiting_supervisor_skips_global_gate_while_its_wait_reason_is_unchanged() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut busy = waiting(&t, "busy", "busy");
        busy["status"] = json!("running");
        t.ctx.save(&busy).unwrap();
        waiting(&t, "wait", "wait");
        let gate_path = t.ctx.jobs.join(".queue.lock");
        let saved = t.ctx.job_dir("wait").join("job.json");
        let polls = Arc::new(AtomicUsize::new(0));
        let counted = polls.clone();
        let held = Mutex::new(None);
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds != 3. {
                return;
            }
            match counted.fetch_add(1, Ordering::Relaxed) {
                // The first gated pass recorded the reason; hold the gate as a
                // stuck registrar would. Later polls must not wait for it.
                0 => *held.lock().unwrap() = Some(FileLock::acquire_queue(&gate_path).unwrap()),
                5 => {
                    let mut j: Value = serde_json::from_slice(&fs::read(&saved).unwrap()).unwrap();
                    assert_eq!(j["queue_wait_reason"]["code"], "running_limit");
                    j["cancel_requested"] = json!(true);
                    fs::write(&saved, j.to_string()).unwrap();
                    held.lock().unwrap().take();
                }
                _ => {}
            }
        }));
        let started = std::time::Instant::now();
        t.ctx.supervise("wait").unwrap();
        assert_eq!(polls.load(Ordering::Relaxed), 6);
        assert!(
            started.elapsed() < Duration::from_secs(20),
            "{:?}",
            started.elapsed()
        );
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn broken_settings_after_spawn_do_not_erase_final_result() {
        let t = Test::new();
        waiting(&t, "finish", "finish");
        fs::write(t.ctx.job_dir("finish").join("prompt.md"), "work").unwrap();
        let cfg = t.ctx.root.join("queue-config.json");
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            fs::write(&cfg, "{").unwrap();
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(OK.as_bytes()))),
            })
        }));
        t.ctx.supervise("finish").unwrap();
        assert_eq!(t.ctx.load("finish").unwrap()["status"], "succeeded");
    }
    #[test]
    fn supervisor_exhausts_bounded_retries_without_creating_fresh_session() {
        let t = Test::new();
        config(&t, json!({"retry_delay_seconds":1,"max_429_retries":2}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        waiting(&t, "exhaust", "req-exhaust");
        fs::write(t.ctx.job_dir("exhaust").join("prompt.md"), "continue").unwrap();
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(|| {
            let text = format!(
                "{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"turn.failed\",\"error\":{{\"message\":\"429 Too Many Requests\"}}}}\n"
            );
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        t.ctx.supervise("exhaust").unwrap();
        let saved = t.ctx.load("exhaust").unwrap();
        assert_eq!(saved["status"], "failed");
        assert_eq!(saved["retry_count"], 2);
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 3);
        assert!(
            args[1..]
                .iter()
                .all(|a| a.windows(2).any(|w| w == ["resume", SESSION_ID]))
        );
    }
    #[test]
    fn active_index_includes_new_registrations_and_reconciles_legacy_jobs() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "ordinary");
        assert!(t.ctx.admit("a").unwrap().is_some());
        let mut j = t.ctx.load("a").unwrap();
        j["status"] = json!("succeeded");
        t.ctx.save(&j).unwrap();
        waiting(&t, "b", "ordinary");
        {
            let _gate = t.ctx.queue_lock().unwrap();
            t.ctx.track_queued_job("b").unwrap();
        }
        assert!(t.ctx.admit("b").unwrap().is_some());
        j = t.ctx.load("b").unwrap();
        j["status"] = json!("succeeded");
        t.ctx.save(&j).unwrap();
        waiting(&t, "c", "legacy");
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.rt.tick(31.);
        assert!(t.ctx.admit("c").unwrap().is_some());
        let index: Value =
            serde_json::from_slice(&fs::read(t.ctx.jobs.join(".queue-index.json")).unwrap())
                .unwrap();
        assert_eq!(index["ids"], json!(["c"]));
    }
    #[test]
    fn cancelling_during_retry_cooldown_closes_existing_card() {
        let t = Test::new();
        let mut j = waiting(&t, "cancel-retry", "req-cancel");
        j["first_started_at"] = json!(t.rt.now());
        j["retry_count"] = json!(1);
        j["started_at"] = Value::Null;
        j["retry_not_before"] = json!(t.rt.epoch() + 60.);
        t.ctx.save(&j).unwrap();
        fs::write(
            t.ctx.job_dir("cancel-retry").join("prompt.md"),
            "作業: 再開待ちの検査",
        )
        .unwrap();
        task_cancel(&j, Some("ao.exe".into()), t.ctx.jobs.clone(), t.rt.clone());
        let calls = t.rt.calls.lock().unwrap();
        assert!(
            calls
                .iter()
                .any(|call| call.argv.get(3).is_some_and(|a| a == "finish"))
        );
    }

    fn waiting(t: &Test, id: &str, name: &str) -> Value {
        let j = t.put(job(json!({"id":id,"name":name,"pid":std::process::id(),"pid_created":123,"codex":"codex.CMD","cwd":t.ctx.root})));
        let _gate = t.ctx.queue_lock().unwrap();
        t.ctx.track_queued_job(id).unwrap();
        j
    }
    fn config(t: &Test, value: Value) {
        fs::write(t.ctx.root.join("queue-config.json"), value.to_string()).unwrap();
    }
    fn loop_job(t: &Test, id: &str, todo: u64) -> Value {
        let mut j = waiting(t, id, "req-loop");
        j["todo_id"] = json!(todo);
        t.ctx.save(&j).unwrap();
        j
    }
    #[test]
    fn loops_wait_for_normal_work_even_with_spare_capacity_and_cooldown() {
        for todo in [18, 23] {
            let t = Test::new();
            loop_job(&t, "loop", todo);
            let mut other = waiting(&t, "ordinary", "ordinary");
            other["retry_not_before"] = json!(t.rt.epoch() + 3600.);
            t.ctx.save(&other).unwrap();
            assert!(t.ctx.admit("loop").unwrap().is_none());
            other["retry_not_before"] = json!(0);
            t.ctx.save(&other).unwrap();
            assert!(t.ctx.admit("loop").unwrap().is_none());
            assert!(t.ctx.admit("ordinary").unwrap().is_some());
            assert!(t.ctx.admit("loop").unwrap().is_some());
        }
    }
    #[test]
    fn lowest_tier_remains_fifo_and_ignores_dead_or_cancelled_waiters() {
        let t = Test::new();
        loop_job(&t, "a", 18);
        loop_job(&t, "b", 23);
        let mut dead = waiting(&t, "dead", "ordinary");
        dead["pid"] = json!(999999);
        t.ctx.save(&dead).unwrap();
        let mut cancelled = waiting(&t, "cancelled", "req-owner");
        cancelled["cancel_requested"] = json!(true);
        t.ctx.save(&cancelled).unwrap();
        config(&t, json!({"max_running":1}));
        assert!(t.ctx.admit("b").unwrap().is_none());
        assert!(t.ctx.admit("a").unwrap().is_some());
        assert!(t.ctx.admit("b").unwrap().is_none());
    }
    #[test]
    fn text_matching_and_config_changes_apply_to_existing_waiters() {
        for word in ["くり返す", "回し続ける", "agents.prompt", "繰り返す"] {
            for field in ["task_title", "prompt"] {
                let t = Test::new();
                let mut j = loop_job(&t, "loop", 9000);
                if field == "prompt" {
                    fs::write(t.ctx.job_dir("loop").join("prompt.md"), word).unwrap();
                } else {
                    j[field] = json!(word);
                    t.ctx.save(&j).unwrap();
                }
                waiting(&t, "ordinary", "ordinary");
                assert!(t.ctx.admit("loop").unwrap().is_none(), "{word}/{field}");
                config(&t, json!({"normal_priority_todo_ids":[9000]}));
                assert!(t.ctx.admit("loop").unwrap().is_some());
            }
        }
        let t = Test::new();
        loop_job(&t, "loop", 9000);
        waiting(&t, "ordinary", "ordinary");
        config(&t, json!({"lowest_priority_todo_ids":[9000]}));
        assert!(t.ctx.admit("loop").unwrap().is_none());
        config(&t, json!({"lowest_priority_todo_ids":[]}));
        assert!(t.ctx.admit("loop").unwrap().is_some());
    }
    #[test]
    fn blocked_normal_children_block_loops_but_running_loop_dependencies_can_finish() {
        let t = Test::new();
        loop_job(&t, "loop", 18);
        assert!(t.ctx.admit("loop").unwrap().is_some());
        child(&t, "dependency", "loop");
        waiting(&t, "ordinary", "ordinary");
        assert!(t.ctx.admit("dependency").unwrap().is_some());
        child(&t, "second", "loop");
        assert!(t.ctx.admit("second").unwrap().is_some());
        child(&t, "third", "loop");
        let mut other = t.ctx.load("ordinary").unwrap();
        other["status"] = json!("succeeded");
        t.ctx.save(&other).unwrap();
        loop_job(&t, "another-loop", 23);
        assert!(t.ctx.admit("another-loop").unwrap().is_none());
    }
    #[test]
    fn shared_todo_title_sets_lowest_priority_without_a_copied_job_title() {
        let mut t = Test::new();
        let tools = t.ctx.root.clone();
        t.ctx.root = tools.join("codex-jobs");
        t.ctx.jobs = t.ctx.root.join("jobs");
        fs::create_dir_all(tools.join("todo")).unwrap();
        fs::write(
            tools.join("todo/todo.json"),
            json!({"items":[{"id":9000,"title":"agents.promptを回し続ける"}]}).to_string(),
        )
        .unwrap();
        loop_job(&t, "loop", 9000);
        waiting(&t, "ordinary", "ordinary");
        assert!(t.ctx.admit("loop").unwrap().is_none());
    }
    #[test]
    fn fairness_uses_the_new_tier_in_both_directions() {
        for low_parent in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = live_tool_parent(&t);
            if low_parent {
                parent["todo_id"] = json!(18);
                t.ctx.save(&parent).unwrap();
            } else {
                let mut other = t.ctx.load("other").unwrap();
                other["todo_id"] = json!(18);
                t.ctx.save(&other).unwrap();
            }
            tool_events(&t, &[("item.started", "work"), ("item.completed", "work")]);
            assert_eq!(t.ctx.admit("other").unwrap().is_some(), low_parent);
            assert_eq!(
                t.ctx.load("long").unwrap()["status"] == "queued",
                low_parent
            );
        }
    }
    #[test]
    fn forty_concurrent_admissions_never_exceed_six() {
        let t = Test::new();
        for i in 0..40 {
            waiting(&t, &format!("job-{i:02}"), "ordinary");
        }
        let handles: Vec<_> = (0..40)
            .map(|i| {
                let ctx = t.ctx.clone();
                std::thread::spawn(move || ctx.admit(&format!("job-{i:02}")).unwrap().is_some())
            })
            .collect();
        let admitted = handles
            .into_iter()
            .map(|h| usize::from(h.join().unwrap()))
            .sum::<usize>();
        assert_eq!(admitted, 6);
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(
            jobs.iter().filter(|j| s(j, "status") == "running").count(),
            6
        );
        assert_eq!(
            jobs.iter().filter(|j| s(j, "status") == "queued").count(),
            34
        );
        let mut ended = t.ctx.load("job-00").unwrap();
        ended["status"] = json!("succeeded");
        t.ctx.save(&ended).unwrap();
        assert!(t.ctx.admit("job-06").unwrap().is_some());
        assert!(t.ctx.admit("job-07").unwrap().is_none());
    }
    fn child(t: &Test, id: &str, parent: &str) -> Value {
        let mut j = waiting(t, id, id);
        j["parent_job_id"] = json!(parent);
        t.ctx.save(&j).unwrap();
        j
    }
    #[test]
    fn six_waiting_parents_can_all_start_dependencies_and_finish() {
        let t = Test::new();
        for i in 0..6 {
            let id = format!("parent-{i}");
            waiting(&t, &id, &id);
            assert!(t.ctx.admit(&id).unwrap().is_some());
        }
        waiting(&t, "root-extra", "req-owner");
        for i in 0..6 {
            child(&t, &format!("child-{i}"), &format!("parent-{i}"));
        }
        assert!(t.ctx.admit("root-extra").unwrap().is_none());
        for i in 0..6 {
            let id = format!("child-{i}");
            assert!(t.ctx.admit(&id).unwrap().is_some());
            let mut ended = t.ctx.load(&id).unwrap();
            ended["status"] = json!("succeeded");
            t.ctx.save(&ended).unwrap();
            let mut parent = t.ctx.load(&format!("parent-{i}")).unwrap();
            parent["status"] = json!("succeeded");
            t.ctx.save(&parent).unwrap();
        }
        assert!(t.ctx.admit("root-extra").unwrap().is_some());
    }
    #[test]
    fn children_win_priority_and_concurrent_siblings_are_limited_to_two() {
        let t = Test::new();
        waiting(&t, "parent", "parent");
        assert!(t.ctx.admit("parent").unwrap().is_some());
        waiting(&t, "root-extra", "req-owner");
        for i in 0..12 {
            child(&t, &format!("child-{i:02}"), "parent");
        }
        assert!(t.ctx.admit("root-extra").unwrap().is_none());
        let handles: Vec<_> = (0..12)
            .map(|i| {
                let ctx = t.ctx.clone();
                std::thread::spawn(move || ctx.admit(&format!("child-{i:02}")).unwrap().is_some())
            })
            .collect();
        assert_eq!(
            handles
                .into_iter()
                .map(|h| usize::from(h.join().unwrap()))
                .sum::<usize>(),
            2
        );
        assert!(t.ctx.admit("root-extra").unwrap().is_some());
        let mut ended = t.ctx.load("child-00").unwrap();
        ended["status"] = json!("succeeded");
        t.ctx.save(&ended).unwrap();
        assert!(t.ctx.admit("child-02").unwrap().is_some());
    }
    #[test]
    fn descendants_keep_one_root_slot_after_ancestors_exit() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "parent", "parent");
        assert!(t.ctx.admit("parent").unwrap().is_some());
        child(&t, "child", "parent");
        assert!(t.ctx.admit("child").unwrap().is_some());
        child(&t, "grandchild", "child");
        assert!(t.ctx.admit("grandchild").unwrap().is_some());
        for id in ["parent", "child"] {
            let mut j = t.ctx.load(id).unwrap();
            j["status"] = json!("succeeded");
            t.ctx.save(&j).unwrap();
        }
        waiting(&t, "other", "req-owner");
        assert!(t.ctx.admit("other").unwrap().is_none());
        let mut j = t.ctx.load("grandchild").unwrap();
        j["status"] = json!("succeeded");
        t.ctx.save(&j).unwrap();
        assert!(t.ctx.admit("other").unwrap().is_some());
    }
    #[test]
    fn inherited_parent_is_saved_and_invalid_or_dead_parents_are_rejected() {
        let t = Test::new();
        t.put(patch(t.running(), json!({"id":"parent"})));
        t.rt.env
            .lock()
            .unwrap()
            .insert("CC_JOB_ID".into(), "parent".into());
        assert_eq!(t.run(&[], b"review").unwrap(), 0);
        let children = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(
            children.iter().find(|j| s(j, "id") != "parent").unwrap()["parent_job_id"],
            "parent"
        );
        for parent in ["../parent", "missing"] {
            assert!(t.run(&["--parent-job-id", parent], b"review").is_err());
        }
        let mut parent = t.ctx.load("parent").unwrap();
        parent["status"] = json!("succeeded");
        t.ctx.save(&parent).unwrap();
        assert!(t.run(&["--parent-job-id", "parent"], b"review").is_err());
        assert_eq!(t.run(&[], b"background job").unwrap(), 0);
        assert_eq!(
            t.ctx
                .list_jobs(&HashSet::new())
                .iter()
                .filter(|j| s(j, "id") != "parent" && j["parent_job_id"].is_null())
                .count(),
            1
        );
    }
    #[test]
    fn recovery_requires_launch_evidence_and_is_idempotent() {
        let t = Test::new();
        t.put(patch(t.running(), json!({"id":"parent"})));
        let mut cancelled = waiting(&t, "cancelled-child", "review");
        cancelled["status"] = json!("cancelled");
        cancelled["pid"] = Value::Null;
        cancelled["cancel_requested"] = json!(true);
        cancelled["cancel_requested_at"] = json!(t.rt.now());
        cancelled["started_at"] = Value::Null;
        t.ctx.save(&cancelled).unwrap();
        fs::write(t.ctx.job_dir("cancelled-child").join("prompt.md"), "review").unwrap();
        let mut queued = waiting(&t, "queued-child", "review-other");
        queued["pid"] = json!(444);
        queued["pid_created"] = json!(55);
        t.ctx.save(&queued).unwrap();
        t.rt.procs.lock().unwrap().insert(444, 55);
        let event = json!({"type":"item.completed","item":{"type":"command_execution","command":"codex_jobs.exe run --name review","exit_code":0,"aggregated_output":"cancelled-child\nqueued-child\n"}});
        fs::write(
            t.ctx.job_dir("parent").join("events.jsonl"),
            event.to_string(),
        )
        .unwrap();
        let since = "2026-10-01T00:00:00+09:00";
        assert_eq!(
            t.ctx
                .recover_children(since, false)
                .unwrap()
                .as_array()
                .unwrap()
                .len(),
            2
        );
        assert_eq!(
            t.ctx.load("cancelled-child").unwrap()["status"],
            "cancelled"
        );
        t.ctx.recover_children(since, true).unwrap();
        assert_eq!(
            t.ctx.load("cancelled-child").unwrap()["parent_job_id"],
            "parent"
        );
        assert_eq!(
            t.ctx.load("cancelled-child").unwrap()["cancel_requested"],
            false
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        assert_eq!(*t.rt.kills.lock().unwrap(), [444]);
        t.ctx.recover_children(since, true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
    }
    #[test]
    fn broken_parent_records_and_cycles_do_not_block_unrelated_families() {
        for damage in ["missing", "corrupt", "cycle"] {
            let t = Test::new();
            config(&t, json!({"max_running":2}));
            waiting(&t, "parent", "parent");
            assert!(t.ctx.admit("parent").unwrap().is_some());
            child(&t, "child", "parent");
            assert!(t.ctx.admit("child").unwrap().is_some());
            if damage == "missing" {
                fs::remove_file(t.ctx.job_dir("parent").join("job.json")).unwrap();
            } else if damage == "corrupt" {
                fs::write(t.ctx.job_dir("parent").join("job.json"), "broken").unwrap();
            } else {
                let mut parent = t.ctx.load("parent").unwrap();
                parent["parent_job_id"] = json!("child");
                t.ctx.save(&parent).unwrap();
            }
            waiting(&t, "other", "req-owner");
            assert!(t.ctx.admit("other").unwrap().is_some(), "{damage}");
            waiting(&t, "third", "req-third");
            assert!(t.ctx.admit("third").unwrap().is_none(), "{damage}");
        }
    }
    #[test]
    fn owner_requests_precede_fifo_and_cooldown_does_not_block_ready_jobs() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "ordinary");
        waiting(&t, "z", "req-owner");
        assert!(t.ctx.admit("a").unwrap().is_none());
        assert!(t.ctx.admit("z").unwrap().is_some());
        let mut request = t.ctx.load("z").unwrap();
        request["status"] = json!("queued");
        request["retry_not_before"] = json!(t.rt.epoch() + 60.);
        t.ctx.save(&request).unwrap();
        assert!(t.ctx.admit("a").unwrap().is_some());
        assert!(t.ctx.admit("z").unwrap().is_none());
    }
    #[test]
    fn orphan_child_consumes_slot_and_reused_pid_does_not() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "waiting", "ordinary");
        t.rt.procs.lock().unwrap().insert(888, 77);
        t.put(job(json!({"id":"orphan","status":"running","pid":999,"pid_created":22,"codex_pid":888,"codex_pid_created":77})));
        assert!(t.ctx.admit("waiting").unwrap().is_none());
        t.rt.procs.lock().unwrap().insert(888, 78);
        assert!(t.ctx.admit("waiting").unwrap().is_some());
    }
    #[test]
    fn configuration_is_loaded_again_and_invalid_limits_are_refused() {
        let t = Test::new();
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 6);
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "one");
        waiting(&t, "b", "two");
        assert!(t.ctx.admit("a").unwrap().is_some());
        assert!(t.ctx.admit("b").unwrap().is_none());
        config(&t, json!({"max_running":2}));
        assert!(t.ctx.admit("b").unwrap().is_some());
        for value in [
            json!({"max_running":0}),
            json!({"max_children_per_parent":0}),
            json!({"retry_delay_seconds":0}),
            json!({"retry_delay_seconds":1000}),
            json!({"max_running":"six"}),
            json!({"max_runing":2}),
        ] {
            config(&t, value);
            assert!(t.ctx.queue_config().is_err());
        }
    }
    #[test]
    fn retry_preserves_session_clears_completion_and_stops_at_limit() {
        let t = Test::new();
        let cfg = QueueConfig::default();
        let mut j = job(
            json!({"status":"failed","last_error":"429 Too Many Requests","thread_id":SESSION_ID,"turn_completed":true}),
        );
        for (count, delay) in [(1, 60.), (2, 120.), (3, 240.), (4, 480.), (5, 900.)] {
            j["status"] = json!("failed");
            j["last_error"] = json!("429 Too Many Requests");
            assert!(t.ctx.retry_429(&mut j, &cfg));
            assert_eq!(j["retry_count"], count);
            assert_eq!(j["retry_not_before"], t.rt.epoch() + delay);
            assert_eq!(j["resume_session"], SESSION_ID);
            assert!(j["turn_completed"].is_null());
            assert_eq!(j["status"], "queued");
            assert_eq!(display_state(&j), "順番待ち");
        }
        j["status"] = json!("failed");
        j["last_error"] = json!("429 Too Many Requests");
        assert!(!t.ctx.retry_429(&mut j, &cfg));
        for p in [
            json!({"status":"succeeded"}),
            json!({"cancel_requested":true}),
            json!({"last_error":"502 Bad Gateway"}),
            json!({"thread_id":null,"resume_session":null}),
        ] {
            let mut j = patch(
                job(
                    json!({"status":"failed","last_error":"429 Too Many Requests","thread_id":SESSION_ID}),
                ),
                p,
            );
            assert!(!t.ctx.retry_429(&mut j, &cfg));
        }
    }
    #[test]
    fn supervisor_retries_same_session_then_succeeds() {
        let t = Test::new();
        config(&t, json!({"retry_delay_seconds":1}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        let j = waiting(&t, "retry", "req-retry");
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "continue").unwrap();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let text = if seq.fetch_add(1, Ordering::Relaxed) == 0 {
                format!(
                    "{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"turn.failed\",\"error\":{{\"message\":\"429 Too Many Requests\"}}}}\n"
                )
            } else {
                OK.into()
            };
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        t.ctx.supervise("retry").unwrap();
        let saved = t.ctx.load("retry").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["retry_count"], 1);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 2);
        let args = t.rt.spawn_args.lock().unwrap();
        assert!(!args[0].iter().any(|a| a == "resume"));
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert!(t.rt.sleeps.lock().unwrap().contains(&3.));
    }
    fn failed(t: &Test, id: &str, todo: u64, created: &str) -> Value {
        let j=t.put(job(json!({"id":id,"name":id,"todo_id":todo,"created_at":created,"status":"failed","finished_at":"2026-10-01T12:01:00+09:00","thread_id":SESSION_ID,"cwd":t.ctx.root,"codex":"codex.CMD"})));
        fs::write(t.ctx.job_dir(id).join("prompt.md"), "resume work").unwrap();
        fs::write(t.ctx.job_dir(id).join("events.jsonl"),"{\"type\":\"turn.failed\",\"error\":{\"message\":\"429 Too Many Requests\"},\"_ts\":\"2026-10-01T12:01:00+09:00\"}\n").unwrap();
        j
    }
    #[test]
    fn recovery_requeues_only_latest_unowned_failure_and_is_idempotent() {
        let t = Test::new();
        let a = failed(&t, "a", 1, "2026-10-01T11:59:00+09:00");
        failed(&t, "b", 1, "2026-10-01T12:00:00+09:00");
        let todo = t.ctx.root.join("todo.json");
        fs::write(
            &todo,
            json!({"items":[{"id":1,"status":"doing","jobs":["a","b"]}]}).to_string(),
        )
        .unwrap();
        let before = fs::read(t.ctx.job_dir("b").join("job.json")).unwrap();
        let plan = t
            .ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, false)
            .unwrap();
        assert_eq!(plan[0]["id"], "b");
        assert_eq!(plan[0]["action"], "would_queue");
        assert_eq!(plan[1]["action"], "skip");
        assert_eq!(
            fs::read(t.ctx.job_dir("b").join("job.json")).unwrap(),
            before
        );
        let report = t
            .ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
            .unwrap();
        assert_eq!(report[0]["action"], "queued");
        assert_eq!(t.ctx.load("b").unwrap()["resume_session"], SESSION_ID);
        assert_eq!(t.ctx.load("a").unwrap(), a);
        assert!(t.ctx.supervisor_alive(&t.ctx.load("b").unwrap()));
        t.ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
            .unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn recovery_excludes_completed_todos_successors_and_non_final_429() {
        for case in [
            "done",
            "block",
            "successor",
            "non_final",
            "before_cutoff",
            "cancelled",
        ] {
            let t = Test::new();
            let mut j = failed(&t, "failure", 1, "2026-10-01T12:00:00+09:00");
            let todo = t.ctx.root.join("todo.json");
            fs::write(&todo,json!({"items":[{"id":1,"status":if ["done","block"].contains(&case){case}else{"doing"},"jobs":["failure"]}]}).to_string()).unwrap();
            if case == "successor" {
                t.put(job(json!({"id":"successor","todo_id":1,"status":"succeeded","created_at":"2026-10-01T12:00:01+09:00"})));
            }
            if case == "non_final" {
                fs::write(
                    t.ctx.job_dir("failure").join("events.jsonl"),
                    "{\"type\":\"turn.completed\"}\n",
                )
                .unwrap();
            }
            if case == "cancelled" {
                j["cancel_requested"] = json!(true);
                t.ctx.save(&j).unwrap();
            }
            let cutoff = if case == "before_cutoff" {
                "2026-10-01T12:02:00+09:00"
            } else {
                "2026-10-01T12:00:00+09:00"
            };
            let report = t.ctx.recover_429(cutoff, &todo, true).unwrap();
            assert!(
                !report
                    .as_array()
                    .unwrap()
                    .iter()
                    .any(|row| row["action"] == "queued"),
                "{case}"
            );
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        }
    }
    #[test]
    fn different_sessions_cannot_duplicate_todo_owner() {
        let t = Test::new();
        waiting(&t, "existing", "owner");
        let mut j = t.ctx.load("existing").unwrap();
        j["todo_id"] = json!(77);
        t.ctx.save(&j).unwrap();
        fs::write(t.ctx.root.join("p.md"), "work").unwrap();
        let p = t.ctx.root.join("p.md").to_string_lossy().into_owned();
        let (result, _, _) = t.cli(&[
            "run",
            "--name",
            "other",
            "--model",
            "gpt-6.1-sol",
            "--effort",
            "high",
            "--cwd",
            t.ctx.root.to_str().unwrap(),
            "--prompt-file",
            &p,
            "--todo-id",
            "77",
        ]);
        assert!(
            result
                .unwrap_err()
                .message
                .contains("ToDo 77 already has active job")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
}
fn events(text: &str) -> Vec<Value> {
    text.lines()
        .map(|l| serde_json::from_str(l).unwrap())
        .collect()
}
fn new_job() -> Value {
    json!({"id":"20260929-000000-t","name":"t","model":"gpt-6-luna","effort":"max","sandbox":"read-only","cwd":"D:/work","created_at":"2026-10-01T12:00:00+09:00","status":"queued","pid":null,"steps":0,"tokens":{"total":0},"activity":"queued","last_error":null,"duration_ms":null})
}
#[test]
fn job_cli_defaults_preserve_each_explicit_model_and_effort_independently() {
    for (name, kind, expected) in [
        ("rev-19", "review", "gpt-6-luna"),
        ("task-review", "", "gpt-6-luna"),
        ("req-19", "implementation", "gpt-6.1-sol"),
        ("req-19", "research", "gpt-6-luna"),
    ] {
        let base = [
            "run",
            "--name",
            name,
            "--prompt-file",
            "prompt.md",
            "--task-type",
            kind,
        ];
        let defaults =
            cc_jobs::cli::parse(&base.iter().map(|s| s.to_string()).collect::<Vec<_>>()).unwrap();
        assert_eq!(defaults.required("model").unwrap(), expected);
        assert_eq!(defaults.required("effort").unwrap(), "high");
        for explicit in [
            vec!["--model", "gpt-6-astra"],
            vec!["--effort", "max"],
            vec!["--model", "gpt-6-astra", "--effort", "max"],
        ] {
            let args = base
                .iter()
                .chain(explicit.iter())
                .map(|s| s.to_string())
                .collect::<Vec<_>>();
            let parsed = cc_jobs::cli::parse(&args).unwrap();
            assert_eq!(
                parsed.required("model").unwrap(),
                if explicit.contains(&"--model") {
                    "gpt-6-astra"
                } else {
                    expected
                }
            );
            assert_eq!(
                parsed.required("effort").unwrap(),
                if explicit.contains(&"--effort") {
                    "max"
                } else {
                    "high"
                }
            );
        }
    }
}
fn patch(mut j: Value, p: Value) -> Value {
    j.as_object_mut()
        .unwrap()
        .extend(p.as_object().unwrap().clone());
    j
}
fn job(p: Value) -> Value {
    patch(new_job(), p)
}
struct Temp(PathBuf);
impl Temp {
    fn new() -> Self {
        static SEQ: AtomicUsize = AtomicUsize::new(0);
        let p = std::env::temp_dir().join(format!(
            "cc-jobs-rust-{}-{}",
            std::process::id(),
            SEQ.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir_all(&p).unwrap();
        Self(p)
    }
}
impl Drop for Temp {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
type CallFn = Box<dyn Fn(&Call) -> io::Result<Reply> + Send + Sync>;
type LoadFn = Box<dyn Fn(&Path, usize) + Send + Sync>;
type SpawnHook = Box<dyn Fn() -> Box<dyn Running> + Send + Sync>;
type SleepHook = Box<dyn Fn(f64) + Send + Sync>;

#[test]
fn saved_discord_settings_are_given_only_to_the_notifier_and_inherited_values_win() {
    let saved = BTreeMap::from([
        ("GODD_DISCORD_BOT_TOKEN".into(), "fake-saved-token".into()),
        ("GODD_DISCORD_TASK_CHANNEL_ID".into(), "forum".into()),
        ("OPENAI_API_KEY".into(), "excluded".into()),
    ]);
    let args = ["godd-ao.exe", "notify", "task", "start"].map(str::to_string);
    let injected = notifier_overrides(&args, &BTreeMap::new(), &saved);
    assert_eq!(injected.len(), 2);
    assert!(!injected.contains_key("OPENAI_API_KEY"));
    let inherited = BTreeMap::from([(
        "godd_discord_bot_token".into(),
        "fake-inherited-token".into(),
    )]);
    assert!(!notifier_overrides(&args, &inherited, &saved).contains_key("GODD_DISCORD_BOT_TOKEN"));
    assert!(
        notifier_overrides(
            &["codex.exe".into(), "exec".into()],
            &BTreeMap::new(),
            &saved
        )
        .is_empty()
    );
    assert!(
        notifier_overrides(
            &["codex.exe".into(), "notify".into(), "task".into()],
            &BTreeMap::new(),
            &saved
        )
        .is_empty()
    );
}
struct Fake {
    env: Mutex<BTreeMap<String, String>>,
    time: Mutex<f64>,
    calls: Mutex<Vec<Call>>,
    replies: Mutex<VecDeque<io::Result<Reply>>>,
    handler: Mutex<Option<CallFn>>,
    sleeps: Mutex<Vec<f64>>,
    procs: Arc<Mutex<HashMap<u32, u64>>>,
    kill_works: AtomicBool,
    kills: Arc<Mutex<Vec<u32>>>,
    pause_allowed: AtomicBool,
    pause_calls: AtomicUsize,
    paused_stop_failure: Arc<AtomicBool>,
    paused_stop_failure_pid: Arc<AtomicU32>,
    pause_recoveries: Mutex<Vec<String>>,
    fail_queued_save: AtomicBool,
    pause_hook: Mutex<Option<Box<dyn Fn() + Send + Sync>>>,
    spawns: AtomicUsize,
    supervisors: AtomicUsize,
    spawn_live: AtomicBool,
    child_alive: Arc<AtomicBool>,
    load_count: AtomicUsize,
    load_hook: Mutex<Option<LoadFn>>,
    save_failure: AtomicBool,
    replace_failures: AtomicUsize,
    replace_count: AtomicUsize,
    fail_stdout: AtomicBool,
    diagnostics: Mutex<Vec<String>>,
    load_failures: AtomicUsize,
    replace_os: AtomicI32,
    spawn_hook: Mutex<Option<SpawnHook>>,
    spawn_args: Mutex<Vec<Vec<String>>>,
    advance_epoch: AtomicBool,
    sleep_hook: Mutex<Option<SleepHook>>,
    supervisor_pid: AtomicU32,
    supervisor_fail: AtomicBool,
}
impl Fake {
    fn new() -> Self {
        Self {
            env: Mutex::new(BTreeMap::from([(
                "GODD_AO_BIN".into(),
                "Z:/missing-ao.exe".into(),
            )])),
            time: Mutex::new(100.),
            calls: Mutex::new(vec![]),
            replies: Mutex::new(VecDeque::new()),
            handler: Mutex::new(None),
            sleeps: Mutex::new(vec![]),
            procs: Arc::new(Mutex::new(HashMap::from([(std::process::id(), 123)]))),
            kill_works: AtomicBool::new(true),
            kills: Arc::new(Mutex::new(vec![])),
            pause_allowed: AtomicBool::new(false),
            pause_calls: AtomicUsize::new(0),
            paused_stop_failure: Arc::new(AtomicBool::new(false)),
            paused_stop_failure_pid: Arc::new(AtomicU32::new(0)),
            pause_recoveries: Mutex::new(Vec::new()),
            fail_queued_save: AtomicBool::new(false),
            pause_hook: Mutex::new(None),
            spawns: AtomicUsize::new(0),
            supervisors: AtomicUsize::new(0),
            spawn_live: AtomicBool::new(false),
            child_alive: Arc::new(AtomicBool::new(false)),
            load_count: AtomicUsize::new(0),
            load_hook: Mutex::new(None),
            save_failure: AtomicBool::new(false),
            replace_failures: AtomicUsize::new(0),
            replace_count: AtomicUsize::new(0),
            fail_stdout: AtomicBool::new(false),
            diagnostics: Mutex::new(vec![]),
            load_failures: AtomicUsize::new(0),
            replace_os: AtomicI32::new(0),
            spawn_hook: Mutex::new(None),
            spawn_args: Mutex::new(vec![]),
            advance_epoch: AtomicBool::new(false),
            sleep_hook: Mutex::new(None),
            supervisor_pid: AtomicU32::new(0),
            supervisor_fail: AtomicBool::new(false),
        }
    }
    fn tick(&self, n: f64) {
        *self.time.lock().unwrap() += n;
    }
    fn reply(&self, ok: bool) {
        self.replies.lock().unwrap().push_back(Ok(Reply {
            code: 0,
            stdout: json!({"ok":ok}).to_string().into_bytes(),
            stderr: vec![],
        }));
    }
    fn with_handler(&self, f: impl Fn(&Call) -> io::Result<Reply> + Send + Sync + 'static) {
        *self.handler.lock().unwrap() = Some(Box::new(f));
    }
}
struct FakeChild {
    alive: Arc<AtomicBool>,
    out: Option<Box<dyn Read + Send>>,
}
impl Running for FakeChild {
    fn pid(&self) -> u32 {
        321
    }
    fn stdout(&mut self) -> Option<Box<dyn Read + Send>> {
        self.out.take()
    }
    fn stderr(&mut self) -> Option<Box<dyn Read + Send>> {
        None
    }
    fn poll(&mut self) -> io::Result<Option<i64>> {
        Ok(if self.alive.load(Ordering::Relaxed) {
            None
        } else {
            Some(0)
        })
    }
    fn kill(&mut self) -> io::Result<()> {
        self.alive.store(false, Ordering::Relaxed);
        Ok(())
    }
}
struct BadReader;
impl Read for BadReader {
    fn read(&mut self, _b: &mut [u8]) -> io::Result<usize> {
        Err(io::Error::other("disk full"))
    }
}
impl Runtime for Fake {
    fn summary_mcp_servers(&self, _codex: &str, _cwd: &Path) -> io::Result<Vec<String>> {
        Ok(vec!["stripe".into(), "unseen-server".into()])
    }
    fn recover_paused(&self, path: &Path) -> io::Result<()> {
        self.pause_recoveries.lock().unwrap().push(
            path.parent()
                .unwrap()
                .file_name()
                .unwrap()
                .to_string_lossy()
                .into(),
        );
        System::default().recover_paused(path)
    }
    fn pause_idle_agent(
        &self,
        pid: u32,
        _: u64,
        _: Option<u64>,
        _: Option<&Path>,
    ) -> io::Result<Option<Box<dyn Paused>>> {
        self.pause_calls.fetch_add(1, Ordering::Relaxed);
        if !self.pause_allowed.load(Ordering::Relaxed) {
            return Ok(None);
        }
        if let Some(hook) = self.pause_hook.lock().unwrap().as_ref() {
            hook();
        }
        self.pause_supervisor(pid, 0).map(Some)
    }
    fn pause_supervisor(&self, pid: u32, _: u64) -> io::Result<Box<dyn Paused>> {
        struct Guard {
            pid: u32,
            procs: Arc<Mutex<HashMap<u32, u64>>>,
            kills: Arc<Mutex<Vec<u32>>>,
            alive: Arc<AtomicBool>,
            fail: Arc<AtomicBool>,
            fail_pid: Arc<AtomicU32>,
        }
        impl Paused for Guard {
            fn keep_paused(&mut self) {}
            fn arm_process(&mut self, _: u32, _: u64) -> io::Result<()> {
                Ok(())
            }
            fn processes(&self) -> Vec<(u32, u64)> {
                vec![(self.pid, self.procs.lock().unwrap()[&self.pid])]
            }
            fn stop(&mut self) -> io::Result<()> {
                if (self.fail_pid.load(Ordering::Relaxed) == 0
                    || self.fail_pid.load(Ordering::Relaxed) == self.pid)
                    && self.fail.swap(false, Ordering::Relaxed)
                {
                    return Err(io::Error::other("fixture stop failed"));
                }
                self.kills.lock().unwrap().push(self.pid);
                self.procs.lock().unwrap().remove(&self.pid);
                if self.pid == 321 {
                    self.alive.store(false, Ordering::Relaxed);
                }
                Ok(())
            }
        }
        Ok(Box::new(Guard {
            pid,
            procs: self.procs.clone(),
            kills: self.kills.clone(),
            alive: self.child_alive.clone(),
            fail: self.paused_stop_failure.clone(),
            fail_pid: self.paused_stop_failure_pid.clone(),
        }))
    }
    fn diagnostic(&self, message: &str) {
        self.diagnostics.lock().unwrap().push(message.into());
    }
    fn env(&self) -> BTreeMap<String, String> {
        self.env.lock().unwrap().clone()
    }
    fn now(&self) -> String {
        "2026-10-01T12:00:00+09:00".into()
    }
    fn mono(&self) -> f64 {
        *self.time.lock().unwrap()
    }
    fn epoch(&self) -> f64 {
        parse_iso(&self.now()).unwrap()
            + if self.advance_epoch.load(Ordering::Relaxed) {
                self.mono() - 100.
            } else {
                0.
            }
    }
    fn sleep(&self, n: f64) {
        self.sleeps.lock().unwrap().push(n);
        self.tick(n);
        if let Some(hook) = self.sleep_hook.lock().unwrap().as_ref() {
            hook(n);
        }
    }
    fn cores(&self) -> usize {
        32
    }
    fn proc_created(&self, pid: u32) -> Option<u64> {
        self.procs.lock().unwrap().get(&pid).copied()
    }
    fn which(&self, name: &str) -> Option<String> {
        match name {
            "codex.cmd" | "codex" => Some("C:/npm/codex.CMD".into()),
            "cargo.exe" => Some("C:/Users/test/.cargo/bin/cargo.exe".into()),
            _ => None,
        }
    }
    fn call(&self, c: Call) -> io::Result<Reply> {
        assert_eq!(c.flags, NO_WINDOW);
        self.calls.lock().unwrap().push(c.clone());
        if let Some(h) = self.handler.lock().unwrap().as_ref() {
            return h(&c);
        }
        if let Some(r) = self.replies.lock().unwrap().pop_front() {
            return r;
        }
        Ok(Reply {
            code: 0,
            stdout: if c.argv[0] == "docker" {
                b"[]".to_vec()
            } else {
                b"{\"ok\":true}".to_vec()
            },
            stderr: vec![],
        })
    }
    fn spawn(&self, spec: &Spawn) -> io::Result<Box<dyn Running>> {
        self.spawn_args.lock().unwrap().push(spec.argv.clone());
        assert_eq!(spec.flags, NO_WINDOW);
        self.spawns.fetch_add(1, Ordering::Relaxed);
        if let Some(h) = &*self.spawn_hook.lock().unwrap() {
            return Ok(h());
        }
        assert!(!spec.env.contains_key("GODD_DISCORD_BOT_TOKEN"));
        self.child_alive
            .store(self.spawn_live.load(Ordering::Relaxed), Ordering::Relaxed);
        Ok(Box::new(FakeChild {
            alive: self.child_alive.clone(),
            out: Some(if self.fail_stdout.load(Ordering::Relaxed) {
                Box::new(BadReader)
            } else {
                Box::new(Cursor::new(format!("{OK}\n").into_bytes()))
            }),
        }))
    }
    fn spawn_supervisor(
        &self,
        _exe: &Path,
        _root: &Path,
        _dir: &Path,
        _jid: &str,
    ) -> io::Result<bool> {
        self.supervisors.fetch_add(1, Ordering::Relaxed);
        if self.supervisor_fail.load(Ordering::Relaxed) {
            return Err(io::Error::other("fixture supervisor spawn failure"));
        }
        Ok(true)
    }
    fn spawn_supervisor_identified(
        &self,
        exe: &Path,
        root: &Path,
        dir: &Path,
        jid: &str,
    ) -> io::Result<(bool, Option<u32>)> {
        let detached = self.spawn_supervisor(exe, root, dir, jid)?;
        let pid = self.supervisor_pid.load(Ordering::Relaxed);
        Ok((detached, (pid != 0).then_some(pid)))
    }
    fn kill_tree(&self, pid: u32) -> io::Result<()> {
        self.kills.lock().unwrap().push(pid);
        if self.kill_works.load(Ordering::Relaxed) {
            self.procs.lock().unwrap().remove(&pid);
            if pid == 321 {
                self.child_alive.store(false, Ordering::Relaxed);
            }
        }
        Ok(())
    }
    fn before_load(&self, path: &Path) -> io::Result<()> {
        let n = self.load_count.fetch_add(1, Ordering::Relaxed);
        if self.load_failures.load(Ordering::Relaxed) > 0 {
            self.load_failures.fetch_sub(1, Ordering::Relaxed);
            return Err(io::Error::from_raw_os_error(32));
        }
        if let Some(h) = self.load_hook.lock().unwrap().as_ref() {
            h(path, n);
        }
        Ok(())
    }
    fn before_save(&self, _path: &Path, j: &Value) -> io::Result<()> {
        if s(j, "status") == "queued"
            && truth(&j["yield_count"])
            && self.fail_queued_save.swap(false, Ordering::Relaxed)
        {
            return Err(io::Error::other("fixture queued write failure"));
        }
        if truth(&j["codex_pid"]) && self.save_failure.swap(false, Ordering::Relaxed) {
            Err(io::Error::other("disk full"))
        } else {
            Ok(())
        }
    }
    fn replace(&self, a: &Path, b: &Path) -> io::Result<()> {
        self.replace_count.fetch_add(1, Ordering::Relaxed);
        let count = self.replace_failures.load(Ordering::Relaxed);
        if count > 0 {
            self.replace_failures.fetch_sub(1, Ordering::Relaxed);
            if self.replace_os.load(Ordering::Relaxed) != 0 {
                return Err(io::Error::from_raw_os_error(
                    self.replace_os.load(Ordering::Relaxed),
                ));
            }
            Err(io::Error::new(
                io::ErrorKind::PermissionDenied,
                "locked by a reader",
            ))
        } else {
            fs::rename(a, b)
        }
    }
}
struct Test {
    _tmp: Temp,
    ctx: Context,
    rt: Arc<Fake>,
}
impl Test {
    fn new() -> Self {
        let tmp = Temp::new();
        let rt = Arc::new(Fake::new());
        let root = tmp.0.clone();
        let ctx = Context {
            jobs: root.join("jobs"),
            home: root.join("home"),
            codex_home: root.join("codexhome"),
            gto: root.join("gto.exe"),
            executable: root.join("codex_jobs.exe"),
            root,
            rt: rt.clone(),
        };
        fs::write(&ctx.gto, b"").unwrap();
        Self { _tmp: tmp, ctx, rt }
    }
    fn put(&self, j: Value) -> Value {
        fs::create_dir_all(self.ctx.job_dir(s(&j, "id"))).unwrap();
        self.ctx.save(&j).unwrap();
        j
    }
    fn cli(&self, args: &[&str]) -> (Result<i32>, String, String) {
        let diagnostic_start = self.rt.diagnostics.lock().unwrap().len();
        let mut out = vec![];
        let mut err = vec![];
        let result = self.ctx.dispatch(
            &args.iter().map(|s| s.to_string()).collect::<Vec<_>>(),
            &mut out,
            &mut err,
        );
        for line in &self.rt.diagnostics.lock().unwrap()[diagnostic_start..] {
            err.extend(line.as_bytes());
            err.push(b'\n');
        }
        (
            result,
            String::from_utf8(out).unwrap(),
            String::from_utf8(err).unwrap(),
        )
    }
    fn running(&self) -> Value {
        job(json!({"status":"running","pid":std::process::id(),"pid_created":123}))
    }
    fn run(&self, extra: &[&str], bytes: &[u8]) -> Result<i32> {
        let p = self.ctx.root.join("prompt.md");
        fs::write(&p, bytes).unwrap();
        let mut args = vec![
            "run",
            "--name",
            "x",
            "--model",
            "m",
            "--effort",
            "e",
            "--cwd",
            self.ctx.root.to_str().unwrap(),
            "--prompt-file",
            p.to_str().unwrap(),
        ];
        args.extend(extra);
        self.cli(&args).0
    }
    fn rate(&self, extra: &[&str]) -> Result<i32> {
        let mut args = vec!["rate", "20260929-000000-t", "--quality", "3"];
        args.extend(extra);
        self.cli(&args).0
    }
}
mod event_parsing {
    use super::*;
    #[test]
    fn test_success_run() {
        let mut j = job(json!({"status":"running"}));
        for e in events(OK) {
            apply_event(&mut j, &e);
        }
        assert_eq!(j["thread_id"], "01a0e883-7628-7121-9540-6780fd4cd6f4");
        assert_eq!(j["steps"], 2);
        assert_eq!(j["last_message"], "DONE");
        assert_eq!(j["turn_completed"], true);
        finish(&mut j, Some(0), 23000, "now");
        assert_eq!(j["status"], "succeeded");
        assert_eq!(j["activity"], "done: DONE");
    }
    #[test]
    fn test_activity_follows_commands() {
        let mut j = new_job();
        let ev = events(OK);
        apply_event(&mut j, &ev[3]);
        assert_eq!(
            j["activity"],
            "running: Get-ChildItem -File | Select-Object -ExpandProperty Name"
        );
        assert_eq!(
            j["activity_detail"],
            "Get-ChildItem -File | Select-Object -ExpandProperty Name"
        );
        apply_event(&mut j, &ev[4]);
        assert_eq!(j["activity"], "thinking");
        assert!(j["activity_detail"].is_null());
    }
    #[test]
    fn test_completed_tool_clears_detail_for_every_tool_kind() {
        for kind in TOOL_KINDS {
            let mut j = job(json!({"activity":"mcp: old","activity_detail":"same detail"}));
            apply_event(
                &mut j,
                &json!({"type":"item.completed","item":{"type":kind,"server":"private","tool":"lookup","command":"same detail","query":"same detail","changes":[{"path":"same detail"}]}}),
            );
            assert_eq!(j["activity"], "thinking", "{kind}");
            assert!(j["activity_detail"].is_null());
        }
    }
    #[test]
    fn test_failed_run() {
        let mut j = new_job();
        for ev in events(FAIL) {
            apply_event(&mut j, &ev);
        }
        assert!(s(&j, "last_error").contains("http://127.0.0.1:10101/v1/responses"));
        assert!(!s(&j, "last_error").contains("Reconnecting"));
        finish(&mut j, Some(1), 5000, "now");
        assert_eq!(j["status"], "failed");
        assert!(s(&j, "activity").starts_with("failed: "));
    }
    #[test]
    fn test_exit_zero_without_turn_completed_is_failure() {
        let mut j = new_job();
        finish(&mut j, Some(0), 10, "now");
        assert_eq!(j["status"], "failed");
    }
    #[test]
    fn test_describe_lines() {
        let l = events(OK).iter().map(describe).collect::<Vec<_>>();
        assert_eq!(
            l[0].as_deref(),
            Some("session 01a0e883-7628-7121-9540-6780fd4cd6f4")
        );
        assert!(l[1].is_none());
        assert_eq!(
            l[3].as_deref(),
            Some("$ Get-ChildItem -File | Select-Object -ExpandProperty Name")
        );
        assert_eq!(
            l[4].as_deref(),
            Some("  -> exit 0, 2 lines: Get-ChildItem -File | Select-Object -ExpandProperty Name")
        );
        assert_eq!(l[7].as_deref(), Some("> DONE"));
        assert_eq!(
            l[8].as_deref(),
            Some("turn completed: 105,710 tokens (in 105,429, cached 76,032, out 281)")
        );
        let l = events(FAIL).iter().map(describe).collect::<Vec<_>>();
        assert!(
            l[1].as_ref()
                .unwrap()
                .starts_with("warning: Model metadata")
        );
        assert!(
            l[3].as_ref()
                .unwrap()
                .starts_with("error: Reconnecting... 1/5")
        );
        assert!(
            l[4].as_ref()
                .unwrap()
                .starts_with("FAILED: unexpected status 404")
        );
    }
    #[test]
    fn test_describe_other_item_kinds() {
        let fc = json!({"type":"item.completed","item":{"type":"file_change","changes":[{"path":"a.py","kind":"update"}]}});
        let todo = json!({"type":"item.updated","item":{"type":"todo_list","items":[{"text":"x","completed":true},{"text":"y","completed":false}]}});
        assert_eq!(describe(&fc).unwrap(), "edit: update a.py");
        assert_eq!(describe(&todo).unwrap(), "plan: [x] x | [ ] y");
        assert_eq!(activity(s(&fc, "type"), &fc["item"]).unwrap(), "thinking");
        assert_eq!(
            activity(s(&todo, "type"), &todo["item"]).unwrap(),
            "plan 1/2"
        );
        assert!(describe(&json!({"type":"item.updated","item":{"type":"unknown"}})).is_none());
    }
    #[test]
    fn test_odd_event_shapes_do_not_break_tracking() {
        let ev = json!({"type":"turn.failed","error":"plain string"});
        let mut j = new_job();
        apply_event(&mut j, &ev);
        assert_eq!(j["last_error"], "plain string");
        assert_eq!(describe(&ev).unwrap(), "FAILED: plain string");
        let raws = [
            br#"{"type":"item.completed","item":"not a dict"}"#.as_slice(),
            b"{broken",
            b"[]",
            b"",
            br#"{"type":"thread.started","thread_id":"x"}"#,
        ];
        assert_eq!(event_lines(raws), ["session x"]);
    }
    #[test]
    fn test_event_lines_show_arrival_time() {
        assert_eq!(
            event_lines([
                br#"{"_ts":"2026-09-29T00:08:19+09:00","type":"thread.started","thread_id":"x"}"#
                    .as_slice()
            ]),
            ["00:08:19 session x"]
        );
    }
    #[test]
    fn test_clean_cmd_undoes_posix_quoting() {
        let p = r#""C:\\Program Files\\PowerShell\\7\\pwsh.exe" -NoProfile -Command "#;
        for (src, want) in [
            (
                r#"'$i=0; gc a.py | % { '"'{0,4}: {1}' -f (++"'$i), $_ }'"#,
                "$i=0; gc a.py | % { '{0,4}: {1}' -f (++$i), $_ }",
            ),
            (r#""rg --files -g '*.py'""#, "rg --files -g '*.py'"),
            (
                r#""python -B -c \"print(1)\"""#,
                r#"python -B -c "print(1)""#,
            ),
            ("'unbalanced", "'unbalanced"),
        ] {
            assert_eq!(clean_cmd(&format!("{p}{src}")), want);
        }
        assert_eq!(clean_cmd("ls -la"), "ls -la");
    }
}
mod token_accounting {
    use super::*;
    #[test]
    fn test_total_is_input_plus_output_with_breakdown() {
        let mut j = new_job();
        apply_event(&mut j, events(OK).last().unwrap());
        assert_eq!(
            j["tokens"],
            json!({"input_tokens":105429,"cached_input_tokens":76032,"cache_write_input_tokens":0,"output_tokens":281,"reasoning_output_tokens":159,"total":105710})
        );
    }
    #[test]
    fn test_usage_is_cumulative_last_wins() {
        let mut j = new_job();
        for n in [100, 250] {
            apply_event(
                &mut j,
                &json!({"type":"turn.completed","usage":{"input_tokens":n,"output_tokens":10}}),
            );
        }
        assert_eq!(j["tokens"]["total"], 260);
    }
    #[test]
    fn test_missing_usage_fields_default_to_zero() {
        let mut j = new_job();
        apply_event(
            &mut j,
            &json!({"type":"turn.completed","usage":{"input_tokens":5}}),
        );
        assert_eq!(j["tokens"]["total"], 5);
        assert_eq!(j["tokens"]["output_tokens"], 0);
    }
}
mod live_session_tokens {
    use super::*;
    #[test]
    fn test_tokens_context_and_rate_limit_come_from_the_session_log() {
        let t = Test::new();
        let mut j = job(json!({"status":"running","thread_id":"tid-1","started_at":t.rt.now()}));
        let dir = t.ctx.codex_home.join("sessions/2026/10/01");
        fs::create_dir_all(&dir).unwrap();
        let p = dir.join("rollout-time-tid-1.jsonl");
        fs::write(&p, SESSION).unwrap();
        let mut sess = Session::default();
        assert!(t.ctx.poll_session(&mut j, &mut sess).unwrap());
        assert_eq!(j["tokens"]["total"], 1042782);
        assert_eq!(j["tokens"]["cached_input_tokens"], 950784);
        assert_eq!(j["context_pct"], 14);
        assert_eq!(j["rate_limit_pct"], 37.0);
        assert!(truth(&j["last_event_at"]));
        assert!(!t.ctx.poll_session(&mut j, &mut sess).unwrap());
        use std::io::Write;
        let mut f = fs::OpenOptions::new().append(true).open(&p).unwrap();
        f.write_all(br#"{"type":"event_msg","payload":{"type":"token_co"#)
            .unwrap();
        assert!(!t.ctx.poll_session(&mut j, &mut sess).unwrap());
        f.write_all(b"unt\",\"info\":{\"total_token_usage\":{\"input_tokens\":2000000,\"output_tokens\":5}}}}\n").unwrap();
        assert!(t.ctx.poll_session(&mut j, &mut sess).unwrap());
        assert_eq!(j["tokens"]["total"], 2000005);
    }
    #[test]
    fn test_no_thread_or_no_log_yet() {
        let t = Test::new();
        assert!(
            !t.ctx
                .poll_session(&mut new_job(), &mut Session::default())
                .unwrap()
        );
        assert!(
            !t.ctx
                .poll_session(
                    &mut job(json!({"thread_id":"nope","started_at":t.rt.now()})),
                    &mut Session::default()
                )
                .unwrap()
        );
    }
}
mod atomic_updates {
    use super::*;
    #[test]
    fn test_file_lock_waits_for_another_process_without_reading_its_locked_byte() {
        use std::io::BufRead;
        let tmp = Temp::new();
        let mut c = hidden_command(env!("CARGO_BIN_EXE_cc_jobs_fixture"));
        c.env("CC_JOBS_FIXTURE_MODE", "lock")
            .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped());
        let mut child = c.spawn().unwrap();
        let mut line = String::new();
        std::io::BufReader::new(child.stdout.take().unwrap())
            .read_line(&mut line)
            .unwrap();
        assert_eq!(line.trim(), "held");
        let start = Instant::now();
        let l = FileLock::acquire(&tmp.0.join("probe.lock")).unwrap();
        assert!(start.elapsed().as_secs_f64() >= 0.5);
        drop(l);
        assert!(child.wait().unwrap().success());
        assert_eq!(fs::metadata(tmp.0.join("probe.lock")).unwrap().len(), 0);
    }
    #[test]
    fn test_roundtrip_leaves_no_temp_files() {
        let t = Test::new();
        let mut j = t.put(new_job());
        j["status"] = json!("running");
        t.ctx.save(&j).unwrap();
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "running");
        let names = fs::read_dir(t.ctx.job_dir(s(&j, "id")))
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .collect::<Vec<_>>();
        assert_eq!(names, ["job.json"]);
    }
    #[test]
    fn test_retries_when_target_is_locked() {
        let t = Test::new();
        let mut j = t.put(new_job());
        let before = t.rt.replace_count.load(Ordering::Relaxed);
        t.rt.replace_failures.store(2, Ordering::Relaxed);
        j["steps"] = json!(7);
        t.ctx.save(&j).unwrap();
        assert_eq!(t.rt.replace_count.load(Ordering::Relaxed) - before, 3);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["steps"], 7);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [0.02, 0.02]);
    }
    #[test]
    fn test_broken_or_vanished_job_json_is_skipped() {
        let t = Test::new();
        t.put(job(
            json!({"id":"good","status":"succeeded","gto":{"automatic":false}}),
        ));
        for (id, text) in [("broken", "{broken"), ("list", "[]")] {
            fs::create_dir_all(t.ctx.job_dir(id)).unwrap();
            fs::write(t.ctx.job_dir(id).join("job.json"), text).unwrap();
        }
        assert_eq!(
            t.ctx
                .list_jobs(&HashSet::new())
                .iter()
                .map(|j| s(j, "id"))
                .collect::<Vec<_>>(),
            ["good"]
        );
        assert_eq!(
            t.ctx.watch_step(&mut HashMap::new()).unwrap(),
            [t.ctx.watch_line(&t.ctx.load("good").unwrap())]
        );
        let gone = job(json!({"id":"gone","status":"running","pid":4000000,"pid_created":1}));
        assert_eq!(t.ctx.refresh(gone, false).unwrap()["status"], "running");
    }
}
mod status_formatting {
    use super::*;
    #[test]
    fn test_helpers() {
        assert_eq!(fmt_elapsed(Some(23.4)), "23s");
        assert_eq!(fmt_elapsed(Some(245.)), "4m05s");
        assert_eq!(fmt_elapsed(Some(3720.)), "1h02m");
        assert_eq!(fmt_tokens(105710), "105.7k");
        assert_eq!(fmt_tokens(2500000), "2.5M");
        assert_eq!(fmt_tokens(0), "0");
    }
    #[test]
    fn test_table() {
        let t = Test::new();
        let j = job(
            json!({"status":"succeeded","duration_ms":23000,"steps":2,"tokens":{"total":105710},"activity":"done: DONE","cwd":"C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki/very/deep/path"}),
        );
        let output = t.ctx.fmt_table(&[j]);
        let lines = output.lines().collect::<Vec<_>>();
        assert_eq!(
            lines[0].split_whitespace().collect::<Vec<_>>(),
            [
                "ID",
                "NAME",
                "MODEL/EFFORT",
                "STATE",
                "ELAPSED",
                "IDLE",
                "STEPS",
                "TOKENS",
                "ACTIVITY",
                "CWD"
            ]
        );
        for p in [
            "20260929-000000-t",
            "gpt-6-luna/max",
            "succeeded",
            "23s",
            "105.7k",
            "done: DONE",
            "...",
            "deep/path",
        ] {
            assert!(lines[1].contains(p), "{p}");
        }
    }
    #[test]
    fn test_table_aligns_wide_characters() {
        let t = Test::new();
        let output = t.ctx.fmt_table(&[
            job(json!({"name":"detach 日本","status":"succeeded"})),
            job(json!({"name":"ab","status":"failed"})),
        ]);
        let lines = output.lines().skip(1).collect::<Vec<_>>();
        assert_eq!(
            width(&lines[0][..lines[0].find("succeeded").unwrap()]),
            width(&lines[1][..lines[1].find("failed").unwrap()])
        );
        assert_eq!(width("a日本🚀\u{301}"), 8);
    }
    #[test]
    fn test_idle_is_time_since_last_event_of_a_running_job() {
        let t = Test::new();
        let j = job(
            json!({"status":"running","last_event_at":"2026-10-01T11:58:30+09:00","context_pct":14}),
        );
        assert_eq!(t.ctx.idle_s(&j), Some(90.));
        assert!(
            t.ctx
                .idle_s(&patch(j.clone(), json!({"status":"succeeded"})))
                .is_none()
        );
        assert!(t.ctx.summary(&j).contains("idle 1m30s"));
    }
    #[test]
    fn test_status_command_filters_old_jobs() {
        let t = Test::new();
        for (id, fin) in [
            ("old", "2020-01-01T00:00:00+09:00"),
            ("new", "2026-10-01T12:00:00+09:00"),
        ] {
            t.put(job(json!({"id":id,"status":"succeeded","finished_at":fin,"duration_ms":1000,"gto":{"automatic":false}})));
        }
        let (r, out, _) = t.cli(&["status", "--json"]);
        assert_eq!(r.unwrap(), 0);
        let got: Vec<Value> = serde_json::from_str(&out).unwrap();
        assert_eq!(got.iter().map(|j| s(j, "id")).collect::<Vec<_>>(), ["new"]);
    }
}
mod lost_detection {
    use super::*;
    #[test]
    fn test_dead_supervisor_marks_lost_and_persists() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"running","pid":424242,"pid_created":1}),
        ));
        let got = t.ctx.refresh(j, false).unwrap();
        assert_eq!(got["status"], "lost");
        assert!(truth(&got["finished_at"]));
        t.rt.procs.lock().unwrap().insert(424242, 1);
        assert_eq!(
            t.ctx
                .refresh(t.ctx.load(s(&got, "id")).unwrap(), false)
                .unwrap()["status"],
            "lost"
        );
    }
    #[test]
    fn test_reused_pid_is_not_our_supervisor() {
        let t = Test::new();
        for p in [
            json!({"status":"running","pid":std::process::id(),"pid_created":12345}),
            json!({"id":"legacy","status":"running","pid":std::process::id()}),
        ] {
            let j = t.put(job(p));
            assert_eq!(t.ctx.refresh(j, false).unwrap()["status"], "lost");
        }
    }
    #[test]
    fn test_alive_supervisor_stays_running() {
        let t = Test::new();
        let j = t.put(t.running());
        assert_eq!(t.ctx.refresh(j, false).unwrap()["status"], "running");
    }
    #[test]
    fn test_final_state_written_just_before_exit_wins() {
        let t = Test::new();
        let stale = t.put(job(
            json!({"status":"running","pid":424242,"pid_created":1}),
        ));
        t.ctx
            .save(&patch(stale.clone(), json!({"status":"succeeded"})))
            .unwrap();
        assert_eq!(t.ctx.refresh(stale, false).unwrap()["status"], "succeeded");
    }
    #[test]
    fn test_queued_without_pid_gets_grace_period() {
        let t = Test::new();
        let fresh = t.put(job(json!({"id":"fresh"})));
        let old = t.put(job(
            json!({"id":"old","created_at":"2020-01-01T00:00:00+09:00"}),
        ));
        assert_eq!(t.ctx.refresh(fresh, false).unwrap()["status"], "queued");
        assert_eq!(t.ctx.refresh(old, false).unwrap()["status"], "lost");
    }
    #[test]
    fn test_slow_start_after_grace_is_not_marked_lost() {
        let t = Test::new();
        let stale = t.put(job(json!({"created_at":"2020-01-01T00:00:00+09:00"})));
        t.ctx
            .save(&patch(
                stale.clone(),
                json!({"status":"running","pid":std::process::id(),"pid_created":123}),
            ))
            .unwrap();
        assert_eq!(t.ctx.refresh(stale, false).unwrap()["status"], "running");
    }
    #[test]
    fn test_real_proc_created() {
        assert!(proc_created(std::process::id()).is_some());
        assert!(proc_created(4000000).is_none());
        assert!(proc_created(0).is_none());
    }
    #[test]
    fn test_watch_reports_changes_once_and_skips_finished_jobs() {
        let t = Test::new();
        let j = t.put(t.running());
        let mut seen = HashMap::new();
        assert_eq!(t.ctx.watch_step(&mut seen).unwrap().len(), 1);
        assert!(t.ctx.watch_step(&mut seen).unwrap().is_empty());
        t.ctx
            .save(&patch(
                j,
                json!({"status":"failed","last_error":"boom","gto":{"automatic":false}}),
            ))
            .unwrap();
        let lines = t.ctx.watch_step(&mut seen).unwrap();
        assert_eq!(lines.len(), 1);
        assert!(lines[0].contains("failed"));
        assert!(lines[0].contains("boom"));
        let before = t.rt.load_count.load(Ordering::Relaxed);
        assert!(t.ctx.watch_step(&mut seen).unwrap().is_empty());
        assert_eq!(t.rt.load_count.load(Ordering::Relaxed), before);
    }
}
mod cancel {
    use super::*;
    #[test]
    fn recovery_cleanup_does_not_record_a_deliberate_cancel() {
        let t = Test::new();
        let j = t.put(t.running());
        assert_eq!(t.cli(&["cancel", s(&j, "id"), "--recovery"]).0.unwrap(), 0);
        assert_ne!(t.ctx.load(s(&j, "id")).unwrap()["cancel_requested"], true);
    }
    #[test]
    fn test_cancel_queued_before_supervisor_starts_never_launches_codex() {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "hello").unwrap();
        assert_eq!(t.cli(&["cancel", s(&j, "id")]).0.unwrap(), 0);
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "cancelled");
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["cancel_requested"], true);
    }
    #[test]
    fn test_supervisor_reloads_state_under_lock() {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(|path, n| {
            if n == 1 {
                let mut j: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
                j["status"] = json!("cancelled");
                fs::write(path, pretty(&j)).unwrap();
            }
        }));
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "cancelled");
    }
    #[test]
    fn test_spawned_child_is_stopped_if_supervisor_setup_fails() {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "hello").unwrap();
        t.rt.spawn_live.store(true, Ordering::Relaxed);
        t.rt.save_failure.store(true, Ordering::Relaxed);
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(*t.rt.kills.lock().unwrap(), [321]);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "failed");
    }
    #[test]
    fn test_never_kills_a_process_that_reused_the_pid() {
        let t = Test::new();
        t.rt.procs.lock().unwrap().insert(555, 99);
        let j = t.put(job(json!({"status":"running","pid":555,"codex_pid":555})));
        let (r, out, _) = t.cli(&["cancel", s(&j, "id")]);
        assert_eq!(r.unwrap(), 0);
        assert_eq!(t.rt.proc_created(555), Some(99));
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "lost");
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["cancel_requested"], true);
        assert!(out.contains("no leftover process"));
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn test_refuses_finished_job() {
        let t = Test::new();
        let j = t.put(job(json!({"status":"succeeded"})));
        assert!(t.cli(&["cancel", s(&j, "id")]).0.is_err());
    }
    #[test]
    fn test_reports_failure_and_keeps_state_when_kill_does_not_work() {
        let t = Test::new();
        let j = t.put(t.running());
        t.rt.kill_works.store(false, Ordering::Relaxed);
        let (r, _, err) = t.cli(&["cancel", s(&j, "id")]);
        assert_eq!(r.unwrap(), 1);
        assert!(err.contains("could not stop"));
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "running");
    }
}
mod tail_and_wait {
    use super::*;
    #[test]
    fn test_n0_shows_only_the_summary_and_unterminated_last_line_is_shown() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"succeeded","duration_ms":1000,"gto":{"automatic":false}}),
        ));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("events.jsonl"), OK).unwrap();
        assert_eq!(
            t.cli(&["tail", s(&j, "id"), "-n", "0"]).1.lines().count(),
            1
        );
        let out = t.cli(&["tail", s(&j, "id"), "-n", "2"]).1;
        let lines = out.lines().collect::<Vec<_>>();
        assert_eq!(lines[0], "> DONE");
        assert!(lines[1].starts_with("turn completed: 105,710"));
    }
    #[test]
    fn test_reads_only_the_end_of_a_big_file() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"succeeded","duration_ms":1000,"gto":{"automatic":false}}),
        ));
        let text = (0..20000)
            .map(|i| {
                format!(
                    "{}\n",
                    json!({"type":"thread.started","thread_id":format!("t{i}")})
                )
            })
            .collect::<String>();
        let p = t.ctx.job_dir(s(&j, "id")).join("events.jsonl");
        fs::write(&p, &text).unwrap();
        let out = t.cli(&["tail", s(&j, "id"), "-n", "3"]).1;
        assert_eq!(
            out.lines().take(3).collect::<Vec<_>>(),
            ["session t19997", "session t19998", "session t19999"]
        );
        assert_eq!(
            t.cli(&["tail", s(&j, "id"), "-n", "30000"])
                .1
                .lines()
                .count(),
            20001
        );
        let (_, pos, rest) = read_tail(&p, 3, false).unwrap();
        assert_eq!(pos, text.len() as u64);
        assert!(rest.is_empty());
    }
    #[test]
    fn test_wait_timeout_zero_returns_at_once() {
        let t = Test::new();
        let j = t.put(t.running());
        let (r, out, _) = t.cli(&["wait", s(&j, "id"), "--timeout", "0"]);
        assert_eq!(r.unwrap(), 2);
        assert!(out.contains("timeout after 0.0s:"));
    }
}
mod gto_rating {
    use super::*;
    fn rated(t: &Test) -> Value {
        t.put(job(json!({"status":"succeeded","duration_ms":23000,"task_type":"review","notes_hint":"hint","tokens":{"total":105710}})))
    }
    #[test]
    fn test_payload() {
        let t = Test::new();
        let j = rated(&t);
        assert_eq!(
            t.ctx.gto_payload(&j, 4, None, None),
            json!({"kind":"model_fit_observation","model":"gpt-6-luna","reasoning_effort":"max","task_type":"review","duration_ms":23000,"tokens":105710,"result_quality":4,"notes":"hint","source":"codex-jobs"})
        );
        let p = t.ctx.gto_payload(&j, 1, Some("impl"), Some("n"));
        assert_eq!((s(&p, "task_type"), s(&p, "notes")), ("impl", "n"));
        let lost = job(
            json!({"status":"lost","started_at":"2026-09-29T00:00:00+09:00","finished_at":"2026-09-29T00:01:30+09:00"}),
        );
        assert_eq!(
            t.ctx.gto_payload(&lost, 1, None, None)["duration_ms"],
            90000
        );
    }
    #[test]
    fn test_rate_sends_payload_once() {
        let t = Test::new();
        rated(&t);
        assert_eq!(t.rate(&["--quality", "5"]).unwrap(), 0);
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(
            calls[0].argv,
            [t.ctx.gto.to_str().unwrap(), "internal", "ingest-turn"]
        );
        let payload: Value = serde_json::from_slice(&calls[0].input).unwrap();
        assert_eq!(payload["result_quality"], 5);
        drop(calls);
        assert_eq!(
            t.ctx.load("20260929-000000-t").unwrap()["gto"]["payload"]["tokens"],
            105710
        );
        assert!(t.rate(&[]).is_err());
        assert_eq!(t.rate(&["--force"]).unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 2);
    }
    #[test]
    fn test_rate_refuses_partial_jobs_without_force() {
        let t = Test::new();
        for st in ["cancelled", "lost"] {
            t.put(job(json!({"status":st})));
            assert!(t.rate(&[]).is_err());
            assert!(t.rt.calls.lock().unwrap().is_empty());
        }
    }
    #[test]
    fn test_rate_survives_gto_timeout() {
        let t = Test::new();
        rated(&t);
        t.rt.replies
            .lock()
            .unwrap()
            .push_back(Err(io::Error::new(io::ErrorKind::TimedOut, "gto")));
        assert_eq!(t.rate(&[]).unwrap(), 1);
        assert_eq!(
            *t.rt.diagnostics.lock().unwrap(),
            ["gto ingest-turn failed: TimeoutExpired"]
        );
        assert!(
            t.ctx
                .load("20260929-000000-t")
                .unwrap()
                .get("gto")
                .is_none()
        );
    }
    #[test]
    fn test_rate_skips_without_gto() {
        let t = Test::new();
        rated(&t);
        fs::remove_file(&t.ctx.gto).unwrap();
        let (r, out, _) = t.cli(&["rate", "20260929-000000-t", "--quality", "3"]);
        assert_eq!(r.unwrap(), 0);
        assert!(t.rt.calls.lock().unwrap().is_empty());
        assert!(out.contains("skipped"));
        assert!(
            t.ctx
                .load("20260929-000000-t")
                .unwrap()
                .get("gto")
                .is_none()
        );
    }
    #[test]
    fn test_rate_refuses_running_job() {
        let t = Test::new();
        t.put(t.running());
        assert!(t.rate(&[]).is_err());
    }
}
mod auto_gto_rating {
    use super::*;
    #[test]
    fn test_quality_mapping_for_both_verdict_languages() {
        for (v, q) in [
            (Some("completed"), 4),
            (Some("完了"), 4),
            (None, 3),
            (Some("needs_input"), 3),
            (Some("判断待ち"), 3),
            (Some("partial"), 2),
            (Some("途中で止まった"), 2),
            (Some("not_done"), 2),
            (Some("未完了"), 2),
        ] {
            let (got, notes) = auto_quality(&job(json!({"status":"succeeded"})), v);
            assert_eq!(got, q);
            assert!(notes.starts_with("auto:"));
            if v.is_none() {
                assert_eq!(notes, "auto: no evaluation");
            }
        }
    }
    #[test]
    fn test_infra_failures_are_one_and_override_completion_evidence() {
        for (st, err) in [
            ("failed", Some("proxy timeout")),
            ("failed", Some("Provider error 429")),
            ("failed", Some("supervisor error: FileNotFoundError")),
            ("cancelled", Some("connection reset")),
            ("lost", None),
        ] {
            let (q, n) = auto_quality(
                &job(json!({"status":st,"last_error":err})),
                Some("completed"),
            );
            assert_eq!(q, 1);
            assert!(n.contains("infra"));
        }
    }
    #[test]
    fn test_unknown_failed_or_manual_cancelled_cause_is_not_labelled_infra() {
        for st in ["failed", "cancelled"] {
            let (q, n) = auto_quality(
                &job(json!({"status":st,"last_error":"assertion mismatch"})),
                None,
            );
            assert_eq!(q, 2);
            assert!(!n.contains("infra"));
        }
    }
    #[test]
    fn test_auto_payload_and_once_only_checkpoint() {
        let t = Test::new();
        let mut j=t.put(job(json!({"status":"succeeded","task_type":"implementation","duration_ms":9000,"tokens":{"total":1200}})));
        assert!(t.ctx.auto_rate(&mut j, Some(Some("completed"))));
        assert!(!t.ctx.auto_rate(&mut j.clone(), Some(Some("completed"))));
        let mut stale = patch(j.clone(), json!({"gto":null}));
        assert!(!t.ctx.auto_rate(&mut stale, Some(Some("completed"))));
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls.len(), 1);
        assert_eq!(
            serde_json::from_slice::<Value>(&calls[0].input).unwrap(),
            json!({"kind":"model_fit_observation","model":"gpt-6-luna","reasoning_effort":"max","task_type":"implementation","duration_ms":9000,"tokens":1200,"result_quality":4,"notes":"auto: Jev 完了","source":"codex-jobs"})
        );
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["gto"]["automatic"], true);
    }
    #[test]
    fn test_terminal_hook_on_lost_refresh() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"running","created_at":"2026-09-20T00:00:00+09:00"}),
        ));
        let got = t.ctx.refresh(j, true).unwrap();
        assert_eq!(got["status"], "lost");
        assert_eq!(
            serde_json::from_slice::<Value>(&t.rt.calls.lock().unwrap()[0].input).unwrap()["result_quality"],
            1
        );
        assert!(truth(&t.ctx.load(s(&got, "id")).unwrap()["gto"]));
    }
    #[test]
    fn test_terminal_refresh_covers_a_supervisor_that_exited_before_sending() {
        let t = Test::new();
        let j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        t.ctx.refresh(j.clone(), true).unwrap();
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["gto"]["automatic"], true);
    }
    #[test]
    fn test_terminal_hook_on_cancel() {
        let t = Test::new();
        let j = t.put(new_job());
        assert_eq!(t.cli(&["cancel", s(&j, "id")]).0.unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "cancelled");
    }
    #[test]
    fn test_cancel_lost_job_sends_outside_state_lock() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"running","created_at":"2026-09-20T00:00:00+09:00"}),
        ));
        let root = t.ctx.job_dir(s(&j, "id"));
        t.rt.with_handler(move |_| {
            let _l = FileLock::try_acquire(&root.join("state.lock"))?;
            Ok(Reply {
                code: 0,
                stdout: vec![],
                stderr: vec![],
            })
        });
        let (r, _, err) = t.cli(&["cancel", s(&j, "id")]);
        assert_eq!(r.unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        assert_eq!(
            t.ctx.load(s(&j, "id")).unwrap()["gto"]["payload"]["result_quality"],
            1
        );
        assert!(!err.contains("rating failed"));
    }
    #[test]
    fn test_automatic_ingest_failure_remains_pending_and_retries_later() {
        let t = Test::new();
        let mut j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 1,
            stdout: vec![],
            stderr: b"sensitive-stderr".to_vec(),
        }));
        assert!(!t.ctx.auto_rate(&mut j, Some(None)));
        let mut saved = t.ctx.load(s(&j, "id")).unwrap();
        assert!(saved.get("gto").is_none());
        assert_eq!(saved["gto_auto_pending"], true);
        let before = t.rt.calls.lock().unwrap().len();
        t.ctx.refresh(saved.clone(), true).unwrap();
        assert_eq!(t.rt.calls.lock().unwrap().len(), before);
        saved["gto_auto_attempt_at"] = json!("2026-09-20T00:00:00+09:00");
        t.ctx.save(&saved).unwrap();
        t.ctx.refresh(saved, true).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["gto"]["automatic"], true);
        assert!(saved.get("gto_auto_pending").is_none());
    }
    #[test]
    fn test_live_or_manually_rated_jobs_are_not_automatically_sent() {
        let t = Test::new();
        let mut live = t.put(job(json!({"status":"running"})));
        assert!(!t.ctx.auto_rate(&mut live, None));
        assert!(!t.ctx.auto_rate(
            &mut patch(
                live,
                json!({"status":"succeeded","gto":{"automatic":false}})
            ),
            None
        ));
        assert!(t.rt.calls.lock().unwrap().is_empty());
    }
    #[test]
    fn test_manual_rate_can_override_auto_and_force_still_controls_repeat_manual_rate() {
        let t = Test::new();
        let mut j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        assert!(t.ctx.auto_rate(&mut j, Some(None)));
        assert_eq!(t.rate(&["--quality", "5", "--notes", "manual"]).unwrap(), 0);
        assert!(t.rate(&[]).is_err());
        assert_eq!(t.rate(&["--quality", "5", "--force"]).unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 3);
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["gto"]["automatic"], false);
        assert_eq!(saved["gto"]["payload"]["result_quality"], 5);
        assert_eq!(saved["gto_history"].as_array().unwrap().len(), 2);
        assert_eq!(saved["gto_history"][0]["automatic"], true);
    }
    #[test]
    fn test_jev_lookup_matches_session_filename_and_reads_no_excerpts() {
        let t = Test::new();
        let sid = "01a0f188-0c24-7860-ab46-6c424b1def08";
        t.rt.replies.lock().unwrap().push_back(Ok(Reply{code:0,stdout:json!([{"session_path":format!("2026/09/30/rollout-time-{sid}.jsonl"),"verdict":"completed"}]).to_string().into_bytes(),stderr:vec![]}));
        assert_eq!(
            t.ctx.jev_evaluations(Some(sid), None),
            HashMap::from([(sid.into(), "completed".into())])
        );
        let calls = t.rt.calls.lock().unwrap();
        let args = &calls[0].argv;
        assert_eq!(&args[..4], ["docker", "exec", "godd-ao-postgres", "sh"]);
        assert!(args[5].contains("$POSTGRES_USER"));
        assert!(args.last().unwrap().contains(&format!("{sid}.jsonl")));
        assert!(args.last().unwrap().contains("completed_at DESC"));
        assert!(!args.last().unwrap().contains("excerpt"));
        assert_eq!(calls[0].flags, NO_WINDOW);
    }
    #[test]
    fn test_invalid_session_id_cannot_enter_sql() {
        let t = Test::new();
        assert!(
            t.ctx
                .jev_evaluations(Some("'; DROP TABLE x; --"), None)
                .is_empty()
        );
        assert!(t.rt.calls.lock().unwrap().is_empty());
    }
    #[test]
    fn test_evaluation_query_failure_uses_no_evaluation_and_never_echoes_stderr() {
        let t = Test::new();
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 1,
            stdout: vec![],
            stderr: b"mock-sensitive-stderr".to_vec(),
        }));
        assert!(
            t.ctx
                .jev_evaluations(Some("01a0f188-0c24-7860-ab46-6c424b1def08"), None)
                .is_empty()
        );
        assert_eq!(auto_quality(&job(json!({"status":"succeeded"})), None).0, 3);
        let diagnostic = t.rt.diagnostics.lock().unwrap().join("\n");
        assert_eq!(diagnostic, "auto evaluation unavailable: RuntimeError");
        assert!(!diagnostic.contains("mock-sensitive-stderr"));
    }
    #[test]
    fn test_backfill_filters_days_rated_and_live_jobs_and_reports_actual_sent_count() {
        let t = Test::new();
        let today = t.rt.now();
        for p in [
            json!({"id":"done","status":"succeeded","finished_at":today,"thread_id":"01a0f188-0c24-7860-ab46-6c424b1def08","duration_ms":2000}),
            json!({"id":"failed","status":"failed","finished_at":today,"last_error":"proxy timeout","duration_ms":1000}),
            json!({"id":"rated","status":"succeeded","finished_at":today,"gto":{"automatic":false}}),
            json!({"id":"live","status":"running"}),
            json!({"id":"old","status":"succeeded","created_at":"2026-09-23T12:00:00+09:00","finished_at":"2026-09-23T12:00:00+09:00"}),
        ] {
            t.put(job(p));
        }
        t.rt.replies.lock().unwrap().push_back(Ok(Reply{code:0,stdout:json!([{"session_path":"rollout-01a0f188-0c24-7860-ab46-6c424b1def08.jsonl","verdict":"completed"}]).to_string().into_bytes(),stderr:vec![]}));
        let (r, out, _) = t.cli(&["backfill", "--days", "7"]);
        assert_eq!(r.unwrap(), 0);
        assert_eq!(
            serde_json::from_str::<Value>(&out).unwrap(),
            json!({"days":7,"considered":3,"sent":2,"already_rated":1,"failed":0,"skipped":2})
        );
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls.iter().filter(|c| c.argv[0] == "docker").count(), 1);
        let mut qualities = calls
            .iter()
            .filter(|c| c.argv[0] != "docker")
            .map(|c| {
                serde_json::from_slice::<Value>(&c.input).unwrap()["result_quality"]
                    .as_i64()
                    .unwrap()
            })
            .collect::<Vec<_>>();
        qualities.sort();
        assert_eq!(qualities, [1, 4]);
        assert!(t.ctx.load("old").unwrap().get("gto").is_none());
        assert!(t.ctx.load("live").unwrap().get("gto").is_none());
    }
    #[test]
    fn test_backfill_ingest_failure_is_reported_and_can_be_retried() {
        let t = Test::new();
        t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        t.rt.replies.lock().unwrap().extend([
            Ok(Reply {
                code: 0,
                stdout: b"[]".to_vec(),
                stderr: vec![],
            }),
            Ok(Reply {
                code: 1,
                stdout: vec![],
                stderr: vec![],
            }),
        ]);
        let (r, out, _) = t.cli(&["backfill", "--days", "7"]);
        assert_eq!(r.unwrap(), 1);
        assert_eq!(serde_json::from_str::<Value>(&out).unwrap()["failed"], 1);
        assert!(
            t.ctx
                .load("20260929-000000-t")
                .unwrap()
                .get("gto")
                .is_none()
        );
    }
    #[test]
    fn test_auto_lookup_is_used_for_the_finished_job_session() {
        let t = Test::new();
        let sid = "01a0f188-0c24-7860-ab46-6c424b1def08";
        let mut j = t.put(job(
            json!({"status":"succeeded","thread_id":sid,"duration_ms":1000}),
        ));
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 0,
            stdout:
                json!([{"session_path":format!("rollout-time-{sid}.jsonl"),"verdict":"partial"}])
                    .to_string()
                    .into_bytes(),
            stderr: vec![],
        }));
        assert!(t.ctx.auto_rate(&mut j, None));
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls[0].argv[0], "docker");
        assert_eq!(
            serde_json::from_slice::<Value>(&calls[1].input).unwrap()["result_quality"],
            2
        );
    }
}
mod run_validation {
    use super::*;
    #[test]
    fn unattended_implementation_accepts_approval_and_readonly_cannot_escalate() {
        for (sandbox, approval, reviewer) in [
            ("danger-full-access", "on-request", "auto_review"),
            ("workspace-write", "on-request", "auto_review"),
            ("read-only", "never", "user"),
        ] {
            let args = codex_args(
                &job(json!({"codex":"codex.cmd","sandbox":sandbox,
                "resume_session":"12345678-1234-4321-9234-123456789abc"})),
                Path::new("D:/jobs/j"),
            );
            for value in [
                format!("approval_policy={approval}"),
                format!("approvals_reviewer={reviewer}"),
            ] {
                assert!(
                    args.windows(2)
                        .any(|pair| pair[0] == "-c" && pair[1] == value)
                );
            }
            let expected = if sandbox == "workspace-write" {
                "danger-full-access"
            } else {
                sandbox
            };
            assert!(args.windows(2).any(|pair| pair == ["-s", expected]));
            assert!(
                args.windows(2)
                    .any(|pair| pair == ["resume", "12345678-1234-4321-9234-123456789abc"])
            );
        }
    }
    #[test]
    fn write_jobs_store_permissions_and_owned_cleanup_instructions() {
        let t = Test::new();
        t.run(&["--sandbox", "danger-full-access"], b"implement feature")
            .unwrap();
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs.len(), 1);
        let j = &jobs[0];
        assert_eq!(j["approval_policy"], "on-request");
        assert_eq!(j["approvals_reviewer"], "auto_review");
        let prompt = fs::read_to_string(t.ctx.job_dir(s(j, "id")).join("prompt.md")).unwrap();
        assert!(prompt.starts_with("implement feature"));
        assert!(prompt.contains("git push"));
        assert!(prompt.contains("quarantine --id <所有ID> --reason policy"));
        assert!(prompt.contains("削除拒否だけをオーナー判断待ちにしない"));
    }
    #[test]
    fn test_args_have_no_cwd_flag() {
        let j = job(json!({"codex":"codex.CMD","add_dirs":["D:/x"],"network":true}));
        let args = codex_args(&j, Path::new("D:/jobs/j"));
        assert_eq!(
            &args[..4],
            ["codex.CMD", "exec", "--json", "--skip-git-repo-check"]
        );
        assert_eq!(args.last().unwrap(), "-");
        assert!(!args.iter().any(|s| s == "-C"));
        for (k, v) in [
            ("-m", "gpt-6-luna"),
            ("-c", "model_reasoning_effort=max"),
            ("-s", "read-only"),
            ("--add-dir", "D:/x"),
        ] {
            let i = args.iter().position(|s| s == v).unwrap();
            assert_eq!(args[i - 1], k);
        }
    }
    #[test]
    fn test_mcp_profiles() {
        for (profile, disabled) in [
            ("minimal", vec!["godd-a", "headroom", "pencil", "node_repl"]),
            ("browser", vec!["godd-a", "headroom", "pencil"]),
            ("full", vec![]),
        ] {
            let args = codex_args(
                &job(json!({"codex":"codex.CMD","mcp_profile":profile})),
                Path::new("D:/jobs/j"),
            );
            let overrides = args
                .windows(2)
                .filter(|p| {
                    p[0] == "-c"
                        && p[1].starts_with("mcp_servers.")
                        && p[1].ends_with("enabled=false")
                })
                .map(|p| p[1].clone())
                .collect::<Vec<_>>();
            assert_eq!(
                overrides,
                disabled
                    .iter()
                    .map(|n| format!("mcp_servers.{n}.enabled=false"))
                    .collect::<Vec<_>>()
            );
        }
    }
    #[test]
    fn test_cargo_limit_is_scoped_to_job_environment() {
        let t = Test::new();
        let env = t.ctx.job_environment();
        assert_eq!(env["CARGO_BUILD_JOBS"], "16");
        assert_eq!(
            env["CODEX_JOBS_REAL_CARGO"],
            "C:/Users/test/.cargo/bin/cargo.exe"
        );
        assert_eq!(
            env["PATH"].split(';').next().unwrap(),
            t.ctx.root.join("bin").to_str().unwrap()
        );
        assert_eq!(
            env["CODEX_JOBS_CARGO_GATE"],
            t.ctx.root.join("cargo_gate.exe").to_str().unwrap()
        );
        assert!(!t.rt.env().contains_key("CARGO_BUILD_JOBS"));
    }
    #[test]
    fn test_rejects_cmd_metacharacters() {
        let t = Test::new();
        for extra in [
            vec!["--add-dir", "D:/R&D"],
            vec!["--add-dir", "D:/p%OS%q"],
            vec!["--model", "a\"b"],
        ] {
            assert!(
                t.run(&extra, b"hi")
                    .unwrap_err()
                    .message
                    .contains("cmd.exe would misparse")
            );
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        }
        assert!(t.ctx.list_jobs(&HashSet::new()).is_empty());
    }
    #[test]
    fn test_rejects_network_outside_workspace_write() {
        let t = Test::new();
        assert!(
            t.run(&["--network"], b"hi")
                .unwrap_err()
                .message
                .contains("--network")
        );
        assert_eq!(
            t.run(&["--network", "--sandbox", "workspace-write"], b"hi")
                .unwrap(),
            0
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn test_rejects_non_utf8_prompt() {
        let t = Test::new();
        assert!(
            t.run(&[], &[0xff, 0xfe, 0x65, 0x65])
                .unwrap_err()
                .message
                .contains("not UTF-8")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn test_launch_key_returns_existing_job_instead_of_spawning_again() {
        let t = Test::new();
        let key = "ab".repeat(16);
        assert_eq!(t.run(&["--launch-key", &key], b"hi").unwrap(), 0);
        let first = t.ctx.list_jobs(&HashSet::new())[0].clone();
        assert_eq!(t.run(&["--launch-key", &key], b"hi").unwrap(), 0);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs.len(), 1);
        assert_eq!(jobs[0]["id"], first["id"]);
        assert_eq!(first["launch_key"], key);
    }
    #[test]
    fn test_launch_key_stays_idempotent_after_the_original_job_finishes() {
        let t = Test::new();
        let key = "cd".repeat(16);
        t.run(&["--launch-key", &key], b"hi").unwrap();
        let j = t.ctx.list_jobs(&HashSet::new())[0].clone();
        t.ctx
            .save(&patch(
                j,
                json!({"status":"succeeded","duration_ms":1000,"finished_at":t.rt.now()}),
            ))
            .unwrap();
        t.run(&["--launch-key", &key], b"hi").unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
    }
    #[test]
    fn test_invalid_launch_key_is_rejected() {
        let t = Test::new();
        assert!(
            t.run(&["--launch-key", "../invalid"], b"hi")
                .unwrap_err()
                .message
                .contains("--launch-key")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }

    #[test]
    fn resume_refuses_an_active_writer_even_with_different_or_missing_launch_keys() {
        for field in ["thread_id", "resume_session"] {
            for status in ["queued", "starting", "running"] {
                let t = Test::new();
                let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
                let mut active = json!({"id":"active-writer","status":status});
                active[field] = json!(session);
                t.put(active);
                for key in [None, Some("abcdabcdabcdabcdabcdabcdabcdabcd")] {
                    let mut args = vec!["--resume-session", session];
                    if let Some(key) = key {
                        args.extend(["--launch-key", key]);
                    }
                    assert!(
                        t.run(&args, b"resume")
                            .unwrap_err()
                            .message
                            .contains("already has active job active-writer")
                    );
                }
                assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
                assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
            }
        }
    }

    #[test]
    fn resume_waits_for_queued_predecessor_and_same_key_adopts_without_relaunching() {
        let t = Test::new();
        let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
        let key = "abcdabcdabcdabcdabcdabcdabcdabcd";
        t.run(
            &["--resume-session", session, "--launch-key", key],
            b"resume",
        )
        .unwrap();
        t.run(
            &["--resume-session", session, "--launch-key", key],
            b"resume",
        )
        .unwrap();
        assert!(
            t.run(&["--resume-session", session], b"duplicate")
                .unwrap_err()
                .message
                .contains("already has active job")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        let job = t.ctx.list_jobs(&HashSet::new())[0].clone();
        t.ctx
            .save(&patch(job, json!({"status":"succeeded"})))
            .unwrap();
        t.run(&["--resume-session", session], b"next turn").unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
    }

    #[test]
    fn concurrent_resume_launches_register_only_one_writer() {
        let t = Test::new();
        let barrier = std::sync::Barrier::new(2);
        std::thread::scope(|scope| {
            let launch = || {
                barrier.wait();
                t.run(
                    &["--resume-session", "01a0f188-0c24-7860-ab46-6c424b1def08"],
                    b"resume",
                )
            };
            let first = scope.spawn(launch);
            let second = scope.spawn(launch);
            let results = [first.join().unwrap(), second.join().unwrap()];
            assert_eq!(results.iter().filter(|r| r.is_ok()).count(), 1);
            assert!(
                results
                    .iter()
                    .find_map(|r| r.as_ref().err())
                    .unwrap()
                    .message
                    .contains("already has active job")
            );
        });
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
    }
}
mod cargo_gate_tests {
    use super::*;
    fn intervals(slots: usize, count: usize) -> Vec<(f64, f64)> {
        let tmp = Temp::new();
        let mut procs = vec![];
        for _ in 0..count {
            let mut c = hidden_command(env!("CARGO_BIN_EXE_cargo_gate"));
            c.args(["build"])
                .env(
                    "CODEX_JOBS_REAL_CARGO",
                    env!("CARGO_BIN_EXE_cc_jobs_fixture"),
                )
                .env("CODEX_JOBS_CARGO_SLOTS", slots.to_string())
                .env("CODEX_JOBS_CARGO_LOCK_DIR", &tmp.0)
                .env("CARGO_TARGET_DIR", &tmp.0)
                .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
                .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
                .env("CC_JOBS_FIXTURE_MODE", "cargo")
                .stdout(std::process::Stdio::piped())
                .stderr(std::process::Stdio::piped());
            procs.push(c.spawn().unwrap());
        }
        let mut result: Vec<(f64, f64)> = vec![];
        for p in procs {
            let out = p.wait_with_output().unwrap();
            assert!(
                out.status.success(),
                "{}",
                String::from_utf8_lossy(&out.stderr)
            );
            let text = String::from_utf8(out.stdout).unwrap();
            let lines = text.lines().collect::<Vec<_>>();
            assert_eq!(lines.len(), 2);
            assert!(lines[0].starts_with("START "));
            assert!(lines[1].starts_with("END "));
            result.push((
                lines[0][6..].parse().unwrap(),
                lines[1][4..].parse().unwrap(),
            ));
        }
        result.sort_by(|a, b| a.0.total_cmp(&b.0));
        result
    }
    fn max_overlap(intervals: &[(f64, f64)]) -> usize {
        let mut ev = intervals
            .iter()
            .flat_map(|(s, e)| [(*s, 1_i32), (*e, -1)])
            .collect::<Vec<_>>();
        ev.sort_by(|a, b| a.0.total_cmp(&b.0).then(a.1.cmp(&b.1)));
        let (mut cur, mut best) = (0, 0);
        for (_, d) in ev {
            cur += d;
            best = best.max(cur);
        }
        best as usize
    }
    #[test]
    fn test_one_slot_serializes_build_like_commands() {
        let v = intervals(1, 2);
        assert!(v[1].0 >= v[0].1 - 0.02);
    }
    #[test]
    fn test_slots_limit_concurrent_build_like_commands() {
        let v = intervals(2, 3);
        assert!(max_overlap(&v) <= 2);
        assert_eq!(max_overlap(&v[..2]), 2);
    }
}
#[cfg(windows)]
mod process_boundary_tests {
    use super::*;
    fn wait_file(path: &Path) {
        let until = Instant::now() + Duration::from_secs(5);
        while !path.exists() {
            assert!(Instant::now() < until, "{}", path.display());
            std::thread::sleep(Duration::from_millis(5));
        }
    }
    #[test]
    fn idle_startup_mcp_is_paused_but_its_later_worker_must_finish_first() {
        let tmp = Temp::new();
        for name in ["codex.exe", "worker.exe", "mcp.exe"] {
            fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), tmp.0.join(name)).unwrap();
        }
        let mut child = hidden_command(&tmp.0.join("codex.exe").to_string_lossy())
            .arg("--controller-mcp")
            .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
            .spawn()
            .unwrap();
        wait_file(&tmp.0.join("worker.ready"));
        let baseline = fs::read_to_string(tmp.0.join("first-tool.filetime"))
            .unwrap()
            .parse()
            .unwrap();
        let rt = System::default();
        let created = proc_created(child.id()).unwrap();
        assert!(
            rt.pause_idle_agent(child.id(), created, Some(baseline), None)
                .unwrap()
                .is_none()
        );
        wait_file(&tmp.0.join("worker.done"));
        wait_file(&tmp.0.join("heartbeat"));
        assert!(
            rt.pause_idle_agent(child.id(), created, None, None)
                .unwrap()
                .is_none()
        );
        let mut paused = rt
            .pause_idle_agent(
                child.id(),
                created,
                Some(baseline),
                Some(&tmp.0.join("yield-paused.json")),
            )
            .unwrap()
            .unwrap();
        let ids = paused.processes();
        assert!(ids.len() >= 2);
        paused.stop().unwrap();
        assert!(child.wait().unwrap().success());
        assert!(ids.iter().all(|(pid, _)| proc_created(*pid).is_none()));
    }
    #[test]
    fn a_dead_pausing_supervisor_is_recovered_without_abandoning_the_agent() {
        let tmp = Temp::new();
        for name in ["codex.exe", "worker.exe"] {
            fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), tmp.0.join(name)).unwrap();
        }
        let mut child = hidden_command(&tmp.0.join("codex.exe").to_string_lossy())
            .arg("--controller")
            .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
            .spawn()
            .unwrap();
        wait_file(&tmp.0.join("worker.done"));
        wait_file(&tmp.0.join("heartbeat"));
        let created = proc_created(child.id()).unwrap();
        let mut holder = hidden_command(env!("CARGO_BIN_EXE_cc_jobs_fixture"))
            .args([
                "--freeze-controller",
                &child.id().to_string(),
                &created.to_string(),
            ])
            .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
            .spawn()
            .unwrap();
        wait_file(&tmp.0.join("freeze.ready"));
        let heartbeat = fs::read(tmp.0.join("heartbeat")).unwrap();
        holder.kill().unwrap();
        holder.wait().unwrap();
        std::thread::sleep(Duration::from_millis(70));
        assert_eq!(fs::read(tmp.0.join("heartbeat")).unwrap(), heartbeat);
        let rt = System::default();
        rt.recover_paused(&tmp.0.join("yield-paused.json")).unwrap();
        std::thread::sleep(Duration::from_millis(70));
        assert_ne!(fs::read(tmp.0.join("heartbeat")).unwrap(), heartbeat);
        let mut paused = rt
            .pause_idle_agent(child.id(), created, None, None)
            .unwrap()
            .unwrap();
        paused.stop().unwrap();
        child.wait().unwrap();
    }
    #[test]
    fn real_workers_finish_and_return_cargo_and_civ6_before_controllers_can_stop() {
        let tmp = Temp::new();
        for name in ["codex.exe", "worker.exe"] {
            fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), tmp.0.join(name)).unwrap();
        }
        let mut child = hidden_command(&tmp.0.join("codex.exe").to_string_lossy())
            .arg("--controller")
            .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
            .spawn()
            .unwrap();
        let wait_for = |path: &Path| {
            let deadline = Instant::now() + Duration::from_secs(5);
            while !path.exists() {
                assert!(Instant::now() < deadline);
                std::thread::sleep(Duration::from_millis(5));
            }
        };
        wait_for(&tmp.0.join("worker.ready"));
        let rt = System::default();
        let created = proc_created(child.id()).unwrap();
        assert!(
            rt.pause_idle_agent(child.id(), created, None, None)
                .unwrap()
                .is_none()
        );
        assert!(FileLock::try_acquire(&tmp.0.join("cargo-build.lock.0")).is_err());
        assert!(tmp.0.join("civ6.lock").exists());
        wait_for(&tmp.0.join("worker.done"));
        wait_for(&tmp.0.join("heartbeat"));
        let paused = rt
            .pause_idle_agent(child.id(), created, None, None)
            .unwrap()
            .unwrap();
        let heartbeat = fs::read(tmp.0.join("heartbeat")).unwrap();
        std::thread::sleep(Duration::from_millis(100));
        assert_eq!(fs::read(tmp.0.join("heartbeat")).unwrap(), heartbeat);
        assert!(FileLock::try_acquire(&tmp.0.join("cargo-build.lock.0")).is_ok());
        assert!(!tmp.0.join("civ6.lock").exists());
        drop(paused);
        std::thread::sleep(Duration::from_millis(100));
        assert_ne!(fs::read(tmp.0.join("heartbeat")).unwrap(), heartbeat);
        assert!(
            rt.pause_idle_agent(child.id(), created + 1, None, None)
                .is_err()
        );
        let mut paused = rt
            .pause_idle_agent(child.id(), created, None, None)
            .unwrap()
            .unwrap();
        paused.stop().unwrap();
        assert!(child.wait().unwrap().success());
        assert!(proc_created(child.id()).is_none());
    }
}
mod task_notifier {
    use super::*;
    const WEBHOOK: &str = "https://discord.com/api/webhooks/123456/sentinel-secret-token";
    fn running(t: &Test) -> Value {
        t.put(job(json!({"status":"running","activity":"said: PROMPT_SECRET https://site.test/x?token=private","started_at":t.rt.now()})))
    }
    fn flag(c: &Call, name: &str) -> String {
        let prefix = format!("--{name}=");
        let v = c
            .argv
            .iter()
            .filter_map(|s| s.strip_prefix(&prefix))
            .collect::<Vec<_>>();
        assert_eq!(v.len(), 1);
        v[0].into()
    }
    fn notifier(t: &Test) -> TaskProgress {
        TaskProgress::new(
            Some("D:/ao/godd-ao.exe".into()),
            t.ctx.jobs.clone(),
            t.rt.clone(),
        )
    }
    fn wait_calls(t: &Test, n: usize) {
        let until = Instant::now() + Duration::from_secs(2);
        while t.rt.calls.lock().unwrap().len() < n {
            assert!(Instant::now() < until, "notification timeout");
            std::thread::sleep(Duration::from_millis(1));
        }
    }
    #[test]
    fn real_notifier_receipt_syncs_the_job_card_with_remaining_deadline() {
        let t = Test::new();
        let j = running(&t);
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 0,
            stdout: br#"{"ok":true,"thread_id":"123","starter_message_id":"123"}"#.to_vec(),
            stderr: vec![],
        }));
        assert_eq!(
            ao_notify("godd-ao.exe", "start", &j, &t.ctx.jobs, t.rt.as_ref(), 135.)["ok"],
            true
        );
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls.len(), 2);
        assert!(calls[0].input.is_empty());
        assert_eq!(&calls[1].argv[1..], ["sync-job-card", "--job", s(&j, "id")]);
        assert!(calls[1].timeout <= calls[0].timeout);
    }
    #[test]
    fn test_source_detail_is_bounded_for_start_update_and_finish() {
        let t = Test::new();
        let mut j = running(&t);
        fs::write(
            t.ctx.job_dir(s(&j, "id")).join("prompt.md"),
            "地図の表示を実装する。".repeat(100),
        )
        .unwrap();
        j = patch(
            j,
            json!({"name":"Map proof","activity":"running: build","activity_detail":"find relevant code ".repeat(50),"last_message":"地図の表示と検証を完了。".repeat(150),"status":"succeeded"}),
        );
        for action in ["start", "update", "finish"] {
            assert_eq!(
                ao_notify(
                    "godd-ao.exe",
                    action,
                    &j,
                    &t.ctx.jobs,
                    t.rt.as_ref(),
                    t.rt.mono() + 35.
                )["ok"],
                true
            );
        }
        let calls = t.rt.calls.lock().unwrap();
        let d = calls
            .iter()
            .map(|c| String::from_utf8(c.input.clone()).unwrap())
            .collect::<Vec<_>>();
        assert!(d[0].is_empty()); // A nonempty detail lets AO replace the supplied title.
        assert_eq!(flag(&calls[0], "title"), "【work】地図の表示を実装する");
        assert!(d[1].starts_with("find relevant code"));
        assert!(d[1].chars().count() > 500);
        assert!(d[2].starts_with("地図の表示と検証を完了。"));
        assert!(d[2].chars().count() > 1500);
        assert!(
            calls
                .iter()
                .all(|c| c.argv.iter().any(|s| s == "--detail-stdin")
                    && !c.argv.iter().any(|s| s.starts_with("--detail=")))
        );
        assert_eq!(flag(&calls[1], "activity"), "running command");
        assert_eq!(flag(&calls[2], "result"), "完了");
    }
    #[test]
    fn test_free_text_flags_preserve_leading_hyphens_and_original_timeout() {
        let t = Test::new();
        let j = patch(
            running(&t),
            json!({"name":"--x","activity":"thinking","activity_detail":"--x","last_message":"- 修正完了","status":"succeeded"}),
        );
        for a in ["start", "update", "finish"] {
            assert_eq!(
                ao_notify("godd-ao.exe", a, &j, &t.ctx.jobs, t.rt.as_ref(), 135.)["ok"],
                true
            );
        }
        let c = t.rt.calls.lock().unwrap();
        assert!(flag(&c[0], "title").starts_with("【work】"));
        assert_eq!(c[1].input, b"--x");
        assert_eq!(String::from_utf8(c[2].input.clone()).unwrap(), "- 修正完了");
        assert!(c.iter().all(|c| c.timeout > 0.
            && c.timeout <= 35.
            && c.argv.iter().any(|s| s.starts_with("--deadline-ms="))));
        assert_eq!(NOTIFY_SHUTDOWN_CAP_S, 300.);
    }
    #[test]
    fn test_paths_urls_and_boundary_spanning_secret_reach_only_rust_sanitizer() {
        let t = Test::new();
        let mut j = running(&t);
        let token = "sentinel-credential-tail";
        t.rt.env
            .lock()
            .unwrap()
            .insert("GODD_DISCORD_BOT_TOKEN".into(), token.into());
        for text in [
            r"C:\Users\Alice\秘密\設計.md",
            r"\\office\share\plan.txt",
            "/home/alice/private.txt",
            "/Users/alice/private.txt",
            "~/private.txt",
            "private.test",
            "alice@private.test",
            "alice@intranet",
            "192.168.1.10",
            "README.md",
            "https://example.test/docs/setup",
            "作業 [link](relative/path)",
            "確認 se\u{200b}ntinel-credential-tail 完了",
            "確認 ｓｅｎｔｉｎｅｌ－ｃｒｅｄｅｎｔｉａｌ－ｔａｉｌ 完了",
        ] {
            j["activity_detail"] = json!(text);
            ao_notify(
                "godd-ao.exe",
                "update",
                &j,
                &t.ctx.jobs,
                t.rt.as_ref(),
                135.,
            );
            assert_eq!(
                t.rt.calls.lock().unwrap().last().unwrap().input,
                text.as_bytes()
            );
        }
        fs::write(
            t.ctx.job_dir(s(&j, "id")).join("prompt.md"),
            format!("{}{token}", "a".repeat(795)),
        )
        .unwrap();
        j = patch(
            j,
            json!({"name":"作業","activity_detail":format!("{}{token}","b".repeat(497)),"last_message":format!("{}{token}","c".repeat(1497)),"status":"succeeded"}),
        );
        let before = t.rt.calls.lock().unwrap().len();
        for a in ["start", "update", "finish"] {
            ao_notify("godd-ao.exe", a, &j, &t.ctx.jobs, t.rt.as_ref(), 135.);
        }
        for c in &t.rt.calls.lock().unwrap()[before..] {
            if c.argv[3] == "start" {
                assert!(c.input.is_empty());
            } else {
                assert!(String::from_utf8_lossy(&c.input).contains(token));
            }
            assert!(!format!("{:?}", c.argv).contains(token));
        }
        apply_event(
            &mut j,
            &json!({"type":"item.completed","item":{"type":"agent_message","text":format!("{}{token}","z".repeat(1497))}}),
        );
        assert!(s(&j, "last_message").contains(token));
        apply_event(
            &mut j,
            &json!({"type":"turn.failed","error":format!("{}{token}","z".repeat(497))}),
        );
        assert!(s(&j, "last_error").contains(token));
    }
    #[test]
    fn test_cancelled_finish_uses_japanese_fallback_without_stale_message() {
        let t = Test::new();
        let j = patch(
            running(&t),
            json!({"status":"cancelled","last_message":"Old successful result"}),
        );
        assert_eq!(
            ao_notify(
                "godd-ao.exe",
                "finish",
                &j,
                &t.ctx.jobs,
                t.rt.as_ref(),
                135.
            )["ok"],
            true
        );
        let c = &t.rt.calls.lock().unwrap()[0];
        assert_eq!(flag(c, "result"), "取り消し");
        assert!(c.input.is_empty());
        assert!(c.argv.iter().any(|s| s == "--failed"));
    }
    #[test]
    fn test_ao_cli_is_only_notifier_and_updates_are_throttled() {
        let t = Test::new();
        let j = running(&t);
        let mut n = notifier(&t);
        n.start(&j);
        wait_calls(&t, 1);
        t.rt.tick(10.);
        n.update(&j, false);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        t.rt.tick(5.);
        n.update(&j, false);
        wait_calls(&t, 2);
        t.rt.tick(15.);
        n.update(&patch(j.clone(), json!({"steps":3})), false);
        wait_calls(&t, 3);
        t.rt.tick(1.);
        n.update(
            &patch(
                j.clone(),
                json!({"status":"succeeded","activity":"done: PROMPT_SECRET"}),
            ),
            true,
        );
        assert!(n.close());
        let c = t.rt.calls.lock().unwrap();
        assert_eq!(
            c.iter().map(|c| c.argv[3].as_str()).collect::<Vec<_>>(),
            ["start", "update", "update", "finish"]
        );
        assert!(c.iter().all(|c| c.argv[1..3] == ["notify", "task"]));
        assert_eq!(flag(&c[0], "phase"), "作業場所 D:/work");
        assert_eq!(flag(&c[2], "percent"), "20");
        assert!(
            c.iter()
                .all(|c| !format!("{:?}", c.argv).contains("PROMPT_SECRET"))
        );
        assert!(
            !t.ctx
                .job_dir(s(&j, "id"))
                .join("discord_message_id")
                .exists()
        );
    }
    #[test]
    fn test_ao_failure_does_not_fail_job() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"queued","codex":"codex.CMD","cwd":t.ctx.root}),
        ));
        fs::write(
            t.ctx.job_dir(s(&j, "id")).join("prompt.md"),
            "read-only task",
        )
        .unwrap();
        let exe = t.ctx.root.join("fake-ao.exe");
        fs::write(&exe, "").unwrap();
        t.rt.env
            .lock()
            .unwrap()
            .insert("GODD_AO_BIN".into(), exe.to_string_lossy().into());
        t.rt.with_handler(|c| {
            if c.argv[1] == "notify" {
                Err(io::Error::other("unavailable"))
            } else {
                Ok(Reply {
                    code: 0,
                    stdout: vec![],
                    stderr: vec![],
                })
            }
        });
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "succeeded");
    }
    #[test]
    fn test_start_failures_back_off_and_hold_queued_updates_and_finish() {
        let t = Test::new();
        let j = running(&t);
        for ok in [false, false, true, true, true] {
            t.rt.reply(ok);
        }
        let mut n = notifier(&t);
        n.start(&j);
        n.last_edit = 0.;
        n.update(&patch(j.clone(), json!({"steps":1})), false);
        n.update(&patch(j.clone(), json!({"steps":3})), false);
        n.update(&patch(j, json!({"status":"succeeded"})), true);
        assert!(n.close());
        let actions =
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>();
        assert_eq!(actions, ["start", "start", "start", "update", "finish"]);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1., 2.]);
    }
    #[test]
    fn test_lost_finish_response_retries_the_same_cli_operation() {
        let t = Test::new();
        let j = running(&t);
        for ok in [true, false, true] {
            t.rt.reply(ok);
        }
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&patch(j, json!({"status":"succeeded"})), true);
        assert!(n.close());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "finish", "finish"]
        );
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1.]);
    }
    #[test]
    fn test_lost_start_response_retries_the_same_cli_task_id() {
        let t = Test::new();
        let j = running(&t);
        t.rt.replies
            .lock()
            .unwrap()
            .push_back(Err(io::Error::new(io::ErrorKind::TimedOut, "timeout")));
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&patch(j.clone(), json!({"status":"succeeded"})), true);
        assert!(n.close());
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(
            calls.iter().map(|c| c.argv[3].clone()).collect::<Vec<_>>(),
            ["start", "start", "finish"]
        );
        assert_eq!(
            calls[0].argv[calls[0].argv.iter().position(|s| s == "--id").unwrap() + 1],
            s(&j, "id")
        );
        let strip_deadline = |c: &Call| {
            c.argv
                .iter()
                .filter(|s| !s.starts_with("--deadline-ms="))
                .cloned()
                .collect::<Vec<_>>()
        };
        assert_eq!(strip_deadline(&calls[0]), strip_deadline(&calls[1]));
        assert_eq!(calls[0].input, calls[1].input);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1.]);
    }
    #[test]
    fn test_exhausted_start_never_sends_finish_without_thread() {
        let t = Test::new();
        let j = running(&t);
        for _ in 0..4 {
            t.rt.reply(false);
        }
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&j, true);
        assert!(n.close());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            vec!["start"; 4]
        );
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1., 2., 4.]);
    }
    #[test]
    fn test_one_35_second_deadline_covers_slow_llm_discord_failures_and_backoff() {
        let t = Test::new();
        let j = running(&t);
        let rt = t.rt.clone();
        t.rt.with_handler(move |_| {
            rt.tick(16.);
            Ok(Reply {
                code: 0,
                stdout: b"{\"ok\":false}".to_vec(),
                stderr: vec![],
            })
        });
        let n = notifier(&t);
        assert!(!n.notify_with_retry("start", &j, 135.));
        let (tx, rx) = std::sync::mpsc::channel();
        tx.send(Notice {
            kind: "start".into(),
            job: j.clone(),
            deadline: 135.,
        })
        .unwrap();
        tx.send(Notice {
            kind: "finish".into(),
            job: j.clone(),
            deadline: 170.,
        })
        .unwrap();
        cc_jobs::notify::work_notices(
            "godd-ao.exe",
            &t.ctx.jobs,
            t.rt.as_ref(),
            &rx,
            &Mutex::new(None),
            1.,
        );
        assert_eq!(t.rt.calls.lock().unwrap().len(), 2);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1., 2.]);
        assert_eq!(t.rt.mono(), 135.);
        let calls = t.rt.calls.lock().unwrap();
        for c in calls.iter() {
            assert_eq!(
                flag(c, "deadline-ms").parse::<i64>().unwrap(),
                (c.timeout * 1000.) as i64
            );
        }
    }
    fn blocking(
        t: &Test,
        block_action: &str,
        first_failure: bool,
    ) -> (Arc<AtomicBool>, Arc<AtomicBool>) {
        let entered = Arc::new(AtomicBool::new(false));
        let release = Arc::new(AtomicBool::new(false));
        let e = entered.clone();
        let r = release.clone();
        let action = block_action.to_string();
        let calls = Arc::new(AtomicUsize::new(0));
        t.rt.with_handler(move |c| {
            let n = calls.fetch_add(1, Ordering::Relaxed);
            if c.argv[3] == action && (action == "finish" || n == 0) {
                e.store(true, Ordering::Release);
                let deadline = Instant::now() + Duration::from_secs(2);
                while !r.load(Ordering::Acquire) {
                    assert!(Instant::now() < deadline);
                    std::thread::sleep(Duration::from_millis(1));
                }
            }
            Ok(Reply {
                code: 0,
                stdout: json!({"ok":!(first_failure&&n==0)})
                    .to_string()
                    .into_bytes(),
                stderr: vec![],
            })
        });
        (entered, release)
    }
    fn wait_entered(e: &AtomicBool) {
        let end = Instant::now() + Duration::from_secs(2);
        while !e.load(Ordering::Acquire) {
            assert!(Instant::now() < end);
            std::thread::sleep(Duration::from_millis(1));
        }
    }
    #[test]
    fn test_slow_429_keeps_supervisor_waiting_and_finish_queued() {
        let t = Test::new();
        let j = running(&t);
        let (e, r) = blocking(&t, "start", true);
        let mut n = notifier(&t);
        n.backoff = 0.;
        n.start(&j);
        wait_entered(&e);
        n.update(&j, true);
        let close = std::thread::spawn(move || n.close());
        assert!(!close.is_finished());
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        r.store(true, Ordering::Release);
        assert!(close.join().unwrap());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "start", "finish"]
        );
    }
    #[test]
    fn test_close_waits_for_final_notification_and_has_hard_cap() {
        let t = Test::new();
        let j = running(&t);
        let (e, r) = blocking(&t, "finish", false);
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&j, true);
        let close = std::thread::spawn(move || n.close());
        wait_entered(&e);
        assert!(!close.is_finished());
        r.store(true, Ordering::Release);
        assert!(close.join().unwrap());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "finish"]
        );
        let (e, r) = blocking(&t, "finish", false);
        let mut n = notifier(&t);
        n.shutdown_cap = 0.02;
        n.start(&j);
        n.update(&j, true);
        wait_entered(&e);
        assert!(!n.close());
        r.store(true, Ordering::Release);
        assert!(n.close());
        assert_eq!(NOTIFY_SHUTDOWN_CAP_S, 300.);
    }
    #[test]
    fn test_cancel_uses_the_same_start_gate() {
        let t = Test::new();
        let j = patch(running(&t), json!({"status":"cancelled"}));
        t.rt.reply(false);
        task_cancel(
            &j,
            Some("godd-ao.exe".into()),
            t.ctx.jobs.clone(),
            t.rt.clone(),
        );
        task_cancel(
            &patch(j, json!({"started_at":null})),
            Some("godd-ao.exe".into()),
            t.ctx.jobs.clone(),
            t.rt.clone(),
        );
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "start", "finish"]
        );
    }
    #[test]
    fn test_credentials_are_redacted_and_not_inherited_by_codex() {
        let t = Test::new();
        let mut j = running(&t);
        t.rt.env.lock().unwrap().extend(BTreeMap::from([
            ("GODD_DISCORD_BOT_TOKEN".into(), "sentinel-bot-token".into()),
            ("GODD_DISCORD_WEBHOOK_URL".into(), WEBHOOK.into()),
        ]));
        let text = format!(
            "{}\n",
            json!({"type":"error","message":format!("{} HTTP://private.test/path SENTINEL-BOT-TOKEN",WEBHOOK.to_uppercase())})
        );
        let path = t.ctx.job_dir(s(&j, "id")).join("events.jsonl");
        let mut pending = vec![];
        pump(text.as_bytes(), &path, t.rt.as_ref(), |ev| pending.push(ev)).unwrap();
        let log = fs::read_to_string(&path).unwrap();
        for secret in [
            WEBHOOK,
            WEBHOOK.to_uppercase().as_str(),
            "SENTINEL-BOT-TOKEN",
        ] {
            assert!(!log.contains(secret));
        }
        assert!(log.contains("private.test"));
        assert!(s(&pending[0], "message").contains("private.test"));
        let stderr = t.ctx.job_dir(s(&j, "id")).join("stderr.log");
        pump_stderr(
            b"HTTPS://private.test/x SENTINEL-BOT-TOKEN\n".as_slice(),
            &stderr,
            t.rt.as_ref(),
        )
        .unwrap();
        let log = fs::read_to_string(&stderr).unwrap();
        assert!(log.contains("private.test"));
        assert!(!log.contains("SENTINEL-BOT-TOKEN"));
        j = patch(
            j,
            json!({"name":"HTTPS://private.test/x","activity":"SENTINEL-BOT-TOKEN","last_error":"HTTP://private.test/y","gto":{"payload":{"notes":"HTTPS://private.test/z"}}}),
        );
        t.ctx.save(&j).unwrap();
        let log = fs::read_to_string(t.ctx.job_dir(s(&j, "id")).join("job.json")).unwrap();
        assert!(log.contains("private.test"));
        assert!(log.contains("SENTINEL-BOT-TOKEN"));
        pump(
            br#"{"type":"error","message":"\u0048TTPS://private.test/hidden"}
"#
            .as_slice(),
            &path,
            t.rt.as_ref(),
            |_| (),
        )
        .unwrap();
        assert!(fs::read_to_string(&path).unwrap().contains("private.test"));
        assert!(
            !t.ctx
                .job_environment()
                .contains_key("GODD_DISCORD_BOT_TOKEN")
        );
        assert!(
            !t.ctx
                .job_environment()
                .contains_key("GODD_DISCORD_WEBHOOK_URL")
        );
        assert_eq!(
            redact_local_log_secrets("sentinel-bot-token", &t.rt.env()),
            "[秘密]"
        );
        t.rt.replies
            .lock()
            .unwrap()
            .push_back(Err(io::Error::new(io::ErrorKind::NotFound, "missing.exe")));
        assert_eq!(
            ao_notify("missing.exe", "start", &j, &t.ctx.jobs, t.rt.as_ref(), 135.),
            json!({"ok":false})
        );
    }
    #[test]
    fn test_non_http_uri_and_scp_addresses_pass_through_state_and_logs() {
        let t = Test::new();
        let mut j = running(&t);
        for address in [
            "SSH://git@private.test/org/repo",
            "GiT://private.test/org/repo",
            "git@private.test:org/repo",
            "USER@PRIVATE.TEST:org/repo",
        ] {
            assert_eq!(
                redact_local_log_secrets(&format!("task {address}"), &t.rt.env()),
                format!("task {address}")
            );
            j = patch(
                j,
                json!({"name":address,"activity":address,"last_error":address,"gto":{"payload":{"notes":address}}}),
            );
            t.ctx.save(&j).unwrap();
            assert!(
                fs::read_to_string(t.ctx.job_dir(s(&j, "id")).join("job.json"))
                    .unwrap()
                    .contains(address)
            );
            let p = t.ctx.job_dir(s(&j, "id")).join("events.jsonl");
            let raw = format!("{}\n", json!({"type":"error","message":address}));
            pump(raw.as_bytes(), &p, t.rt.as_ref(), |_| ()).unwrap();
            assert!(fs::read_to_string(p).unwrap().contains(address));
            let p = t.ctx.job_dir(s(&j, "id")).join("stderr.log");
            pump_stderr(format!("{address}\n").as_bytes(), &p, t.rt.as_ref()).unwrap();
            assert!(fs::read_to_string(p).unwrap().contains(address));
        }
    }
}
mod end_to_end {
    use super::*;
    #[test]
    fn terminal_supervisor_cleanup_preserves_its_independent_descendant() {
        let mut t = Test::new();
        t.ctx.rt = Arc::new(System::default());
        let fixture = env!("CARGO_BIN_EXE_cc_jobs_fixture");
        let mut parent = hidden_command(fixture)
            .arg("--supervisor-with-independent-job")
            .env("CC_JOBS_FIXTURE_DIR", &t.ctx.root)
            .spawn()
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(5);
        while !t.ctx.root.join("hold.pid").exists() {
            if Instant::now() >= deadline {
                let _ = parent.kill();
                let _ = parent.wait();
                panic!("fixture descendant did not start");
            }
            std::thread::sleep(Duration::from_millis(10));
        }
        let child: u32 = fs::read_to_string(t.ctx.root.join("hold.pid"))
            .unwrap()
            .parse()
            .unwrap();
        let child_created = proc_created(child).unwrap();
        let j=t.put(job(json!({"id":"finished","status":"succeeded","pid":parent.id(),"pid_created":proc_created(parent.id()),"finished_at":(chrono::Local::now()-chrono::Duration::seconds(180)).to_rfc3339(),"codex_pid":null})));
        let result = t.ctx.cleanup_supervisors(true);
        let survived = proc_created(child) == Some(child_created);
        // Release both owned processes before asserting, including failure paths.
        if proc_created(child) == Some(child_created) {
            t.ctx
                .rt
                .pause_supervisor(child, child_created)
                .unwrap()
                .stop()
                .unwrap();
        }
        if parent.try_wait().unwrap().is_none() {
            parent.kill().unwrap();
        }
        parent.wait().unwrap();
        assert!(
            survived,
            "cleanup followed parent PID links into an independent job"
        );
        assert_eq!(result.unwrap()[0]["id"], "finished");
        assert_eq!(
            t.ctx.load("finished").unwrap()["finished_at"],
            j["finished_at"]
        );
    }
    #[test]
    fn cargo_slot_wait_marker_is_visible_then_cleared_before_execution() {
        let mut t = Test::new();
        t.ctx.rt = Arc::new(System::default());
        let job=t.put(job(json!({"id":"build","status":"running","pid":std::process::id(),"pid_created":proc_created(std::process::id())})));
        let held = FileLock::try_acquire(&t.ctx.root.join("cargo-build.lock.0")).unwrap();
        let mut gate = hidden_command(env!("CARGO_BIN_EXE_cargo_gate"))
            .arg("--exit-seven")
            .env("CC_JOB_ID", "build")
            .env("CODEX_JOBS_CARGO_GATE", t.ctx.root.join("cargo_gate.exe"))
            .env("CODEX_JOBS_CARGO_LOCK_DIR", &t.ctx.root)
            .env("CODEX_JOBS_CARGO_SLOTS", "1")
            .env("CARGO_TARGET_DIR", &t.ctx.root)
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
            .env_remove("CODEX_JOBS_CACHE_LOCK_HELD")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(5);
        let mut visible = false;
        while Instant::now() < deadline {
            if t.ctx.resource_waits(&job) == ["cargoの共有ビルド枠待ち"] {
                visible = true;
                break;
            }
            std::thread::sleep(Duration::from_millis(10));
        }
        drop(held);
        let exit = gate.wait().unwrap();
        assert!(
            visible,
            "running build did not expose its exact resource wait"
        );
        assert_eq!(exit.code(), Some(7));
        assert!(t.ctx.resource_waits(&job).is_empty());
        fs::write(
            t.ctx.job_dir("build").join("resource-waits/stale.json"),
            json!({"job_id":"build","pid":std::process::id(),"pid_created":0,"reason":"stale"})
                .to_string(),
        )
        .unwrap();
        assert!(t.ctx.resource_waits(&job).is_empty());
    }
    #[test]
    fn state_lock_does_not_block_snapshot_readers() {
        let t = Test::new();
        let job = t.put(new_job());
        let held = t.ctx.job_lock(s(&job, "id"), "state.lock").unwrap();
        let begin = Instant::now();
        assert_eq!(t.ctx.load(s(&job, "id")).unwrap(), job);
        assert!(begin.elapsed() < Duration::from_secs(1));
        drop(held);
    }
    #[test]
    fn cancelled_kernel_lock_wait_is_drained_and_reports_path_and_exit_75() {
        let tmp = Temp::new();
        let path = tmp.0.join(".queue.lock");
        let held = FileLock::try_acquire(&path).unwrap();
        let start = Instant::now();
        let error = match FileLock::acquire_queue_timeout(&path, Duration::from_millis(80)) {
            Ok(_) => panic!("contended byte lock unexpectedly acquired"),
            Err(error) => error,
        };
        assert_eq!(error.kind(), io::ErrorKind::TimedOut);
        assert!(start.elapsed() < Duration::from_secs(2));
        let error = Error::file_io("acquire exclusive lock byte range [0, 1)", &path, error);
        assert_eq!(error.code, 75);
        assert!(error.message.contains(path.to_str().unwrap()));
        assert!(error.message.contains("[0, 1)"));
        drop(held);
        // A leaked pending request would acquire this byte after the holder exits.
        let _next = FileLock::try_acquire(&path).unwrap();
    }
    #[test]
    fn queue_config_read_retries_real_byte_lock_and_cli_reports_exhaustion() {
        let t = E2e::new();
        let path = t.exe.parent().unwrap().join("queue-config.json");
        fs::write(&path, r#"{"max_running":3}"#).unwrap();
        let held = FileLock::try_acquire(&path).unwrap();
        let output = t.cli(
            &[
                "run",
                "--name",
                "locked-config",
                "--prompt-file",
                "unused.md",
            ],
            "ok",
        );
        assert_eq!(output.status.code(), Some(75));
        assert!(output.stdout.is_empty());
        assert!(String::from_utf8_lossy(&output.stderr).contains(path.to_str().unwrap()));
        assert!(String::from_utf8_lossy(&output.stderr).contains("os error 33"));
        drop(held);
        let held = FileLock::try_acquire(&path).unwrap();
        let context = Context {
            root: t.exe.parent().unwrap().to_path_buf(),
            ..Context::installed().unwrap()
        };
        std::thread::scope(|scope| {
            scope.spawn(move || {
                std::thread::sleep(Duration::from_millis(100));
                drop(held);
            });
            assert_eq!(context.queue_config().unwrap().max_running, 3);
        });
    }
    #[test]
    fn thirty_parallel_run_processes_share_the_real_queue_files() {
        let t = E2e::new();
        let prompt = t.tmp.0.join("parallel-prompt.md");
        fs::write(&prompt, "fixture parallel launch").unwrap();
        fs::write(
            t.exe.parent().unwrap().join("queue-config.json"),
            r#"{"max_running":1,"max_children_per_parent":1}"#,
        )
        .unwrap();
        // Reserve the single running slot so supervisors keep polling admission.
        let jobs = t.exe.parent().unwrap().join("jobs");
        let dir = jobs.join("parallel-parent");
        fs::create_dir_all(&dir).unwrap();
        fs::write(
            dir.join("job.json"),
            pretty(
                &json!({"id":"parallel-parent","name":"parent","status":"running",
            "created_at":"2026-10-07T00:00:00+09:00","queue_unreadable":true}),
            ),
        )
        .unwrap();
        for n in 0..3132 {
            let dir = jobs.join(format!("history-{n}"));
            fs::create_dir(&dir).unwrap();
            fs::write(dir.join("job.json"), pretty(&json!({"id":format!("history-{n}"),"status":"succeeded","created_at":"2026-10-06T00:00:00+09:00"}))).unwrap();
        }
        let barrier = Arc::new(std::sync::Barrier::new(30));
        let outputs = std::thread::scope(|scope| {
            let handles: Vec<_> = (0..30)
                .map(|n| {
                    let t = &t;
                    let prompt = &prompt;
                    let barrier = barrier.clone();
                    scope.spawn(move || {
                        barrier.wait();
                        t.cli(
                            &[
                                "run",
                                "--name",
                                &format!("parallel-{n}"),
                                "--model",
                                "fixture",
                                "--todo-id",
                                &(10000 + n).to_string(),
                                "--cwd",
                                t.tmp.0.to_str().unwrap(),
                                "--prompt-file",
                                prompt.to_str().unwrap(),
                                "--mcp-profile",
                                "minimal",
                            ],
                            "ok",
                        )
                    })
                })
                .collect();
            handles
                .into_iter()
                .map(|h| h.join().unwrap())
                .collect::<Vec<_>>()
        });
        let mut ids = HashSet::new();
        let mut failures = Vec::new();
        for output in &outputs {
            assert!(!String::from_utf8_lossy(&output.stderr).contains("os error 33"));
            if output.status.success() {
                ids.insert(String::from_utf8_lossy(&output.stdout).trim().to_string());
            } else {
                failures.push(format!(
                    "{:?}: {}",
                    output.status.code(),
                    String::from_utf8_lossy(&output.stderr)
                ));
            }
        }
        eprintln!(
            "30 launch results: {} accepted, {} failed",
            ids.len(),
            failures.len()
        );
        // These jobs are queued and have no child processes. Retain an OS handle
        // while stopping each supervisor; taskkill /T can follow reused parent IDs.
        for id in &ids {
            let job = t.job(id);
            assert_eq!(job["status"], "queued");
            let pid = job["pid"].as_u64().unwrap() as u32;
            assert_ne!(pid, std::process::id());
            assert_eq!(proc_created(pid), job["pid_created"].as_u64());
            unsafe {
                use windows_sys::Win32::{Foundation::CloseHandle, System::Threading::*};
                let handle = OpenProcess(PROCESS_TERMINATE | PROCESS_SYNCHRONIZE, 0, pid);
                assert!(!handle.is_null());
                assert_ne!(TerminateProcess(handle, 0), 0);
                assert_eq!(WaitForSingleObject(handle, 5000), 0);
                CloseHandle(handle);
            }
        }
        assert!(failures.is_empty(), "parallel run failures: {failures:#?}");
        assert_eq!(ids.len(), 30);
        let sequence: Value =
            serde_json::from_slice(&fs::read(jobs.join(".queue-sequence.json")).unwrap()).unwrap();
        assert_eq!(sequence["sequence"], 30);
        let index: Value =
            serde_json::from_slice(&fs::read(jobs.join(".queue-index.json")).unwrap()).unwrap();
        for id in &ids {
            assert!(index["ids"].as_array().unwrap().contains(&json!(id)));
        }
    }
    struct E2e {
        tmp: Temp,
        exe: PathBuf,
        rec: PathBuf,
    }
    impl E2e {
        fn new() -> Self {
            let tmp = Temp::new();
            let tool = tmp.0.join("tool");
            let bin = tmp.0.join("bin");
            let rec = tmp.0.join("rec");
            for p in [&tool, &bin, &rec] {
                fs::create_dir_all(p).unwrap();
            }
            let exe = tool.join("codex_jobs.exe");
            fs::copy(env!("CARGO_BIN_EXE_codex_jobs"), &exe).unwrap();
            fs::copy(
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
                bin.join("fixture.exe"),
            )
            .unwrap();
            fs::write(
                bin.join("codex.cmd"),
                b"@echo off\r\n\"%~dp0fixture.exe\" %*\r\n",
            )
            .unwrap();
            Self { tmp, exe, rec }
        }
        fn cli(&self, args: &[&str], mode: &str) -> std::process::Output {
            let mut c = hidden_command(self.exe.to_str().unwrap());
            c.args(args)
                .env(
                    "PATH",
                    format!(
                        "{};{}",
                        self.tmp.0.join("bin").display(),
                        std::env::var("PATH").unwrap_or_default()
                    ),
                )
                .env("CC_JOBS_FIXTURE_MODE", mode)
                .env("CC_JOBS_FIXTURE_DIR", &self.rec)
                .env("CODEX_HOME", self.tmp.0.join("codexhome"))
                .env("GODD_AO_BIN", self.tmp.0.join("missing-ao.exe"))
                .env("CODEX_JOBS_GTO_BIN", self.tmp.0.join("missing-gto.exe"))
                .env_remove("GODD_DISCORD_BOT_TOKEN")
                .env_remove("CC_JOB_ID")
                .env_remove("CODEX_JOBS_DISCORD_WEBHOOK");
            c.output().unwrap()
        }
        fn start(&self, mode: &str, cwd: &Path, prompt: &[u8]) -> String {
            let p = self.tmp.0.join("prompt.md");
            fs::write(&p, prompt).unwrap();
            let r = self.cli(
                &[
                    "run",
                    "--name",
                    "e2e",
                    "--model",
                    "gpt-6-luna",
                    "--effort",
                    "max",
                    "--cwd",
                    cwd.to_str().unwrap(),
                    "--prompt-file",
                    p.to_str().unwrap(),
                ],
                mode,
            );
            assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stderr));
            String::from_utf8(r.stdout).unwrap().trim().into()
        }
        fn job(&self, jid: &str) -> Value {
            serde_json::from_slice(
                &fs::read(self.tmp.0.join("tool/jobs").join(jid).join("job.json")).unwrap(),
            )
            .unwrap()
        }
        fn until(&self, mut condition: impl FnMut() -> bool) {
            let end = Instant::now() + Duration::from_secs(20);
            while !condition() {
                assert!(Instant::now() < end, "timed out");
                std::thread::sleep(Duration::from_millis(100));
            }
        }
        fn slow(&self) -> (String, Vec<u32>) {
            let jid = self.start("slow", &self.tmp.0, b"hi");
            self.until(|| {
                self.rec.join("sleep.pid").exists() && truth(&self.job(&jid)["codex_pid"])
            });
            let j = self.job(&jid);
            let child: u32 = fs::read_to_string(self.rec.join("sleep.pid"))
                .unwrap()
                .parse()
                .unwrap();
            (
                jid,
                vec![
                    j["pid"].as_u64().unwrap() as u32,
                    j["codex_pid"].as_u64().unwrap() as u32,
                    child,
                ],
            )
        }
    }
    impl Drop for E2e {
        fn drop(&mut self) {
            let rt = System::default();
            for f in ["hold.pid", "sleep.pid"] {
                if let Ok(s) = fs::read_to_string(self.rec.join(f))
                    && let Ok(pid) = s.parse::<u32>()
                    && proc_created(pid).is_some()
                {
                    let _ = rt.kill_tree(pid);
                }
            }
            if let Ok(entries) = fs::read_dir(self.tmp.0.join("tool/jobs")) {
                for e in entries.flatten() {
                    if let Ok(raw) = fs::read(e.path().join("job.json"))
                        && let Ok(j) = serde_json::from_slice::<Value>(&raw)
                    {
                        for k in ["pid", "codex_pid"] {
                            if let Some(pid) = j[k].as_u64()
                                && proc_created(pid as u32).is_some_and(|created| {
                                    j[format!("{k}_created")].as_u64() == Some(created)
                                })
                            {
                                let _ = rt.kill_tree(pid as u32);
                            }
                        }
                    }
                }
            }
            std::thread::sleep(Duration::from_millis(100));
        }
    }
    #[test]
    fn real_supervisors_start_a_dependency_while_the_root_limit_is_full() {
        let e = E2e::new();
        fs::write(
            e.tmp.0.join("tool/queue-config.json"),
            r#"{"max_running":1}"#,
        )
        .unwrap();
        let (parent, _) = e.slow();
        let other = e.start("", &e.tmp.0, b"other root");
        assert_eq!(e.job(&other)["status"], "queued");
        let prompt = e.tmp.0.join("child-prompt.md");
        fs::write(&prompt, "dependency").unwrap();
        let result = e.cli(
            &[
                "run",
                "--name",
                "dependency",
                "--model",
                "gpt-6-luna",
                "--effort",
                "max",
                "--cwd",
                e.tmp.0.to_str().unwrap(),
                "--prompt-file",
                prompt.to_str().unwrap(),
                "--parent-job-id",
                &parent,
            ],
            "",
        );
        assert!(
            result.status.success(),
            "{}",
            String::from_utf8_lossy(&result.stderr)
        );
        let child = String::from_utf8(result.stdout).unwrap().trim().to_string();
        e.until(|| e.job(&child)["status"] == "succeeded");
        assert_eq!(e.job(&child)["parent_job_id"], parent);
        assert_eq!(e.job(&parent)["status"], "running");
        assert_eq!(e.job(&other)["status"], "queued");
        assert!(e.cli(&["cancel", &parent], "").status.success());
        e.until(|| e.job(&other)["status"] == "succeeded");
    }
    #[test]
    fn test_run_survives_its_caller_and_passes_prompt_and_cwd_verbatim() {
        let e = E2e::new();
        let cwd = e.tmp.0.join("日本語 R&D %OS% x");
        fs::create_dir(&cwd).unwrap();
        let prompt = "日本語のプロンプト 🚀 & ^ % \" ! <x>\r\n2行目\n".as_bytes();
        let jid = e.start("", &cwd, prompt);
        let r = e.cli(&["wait", &jid, "--timeout", "30"], "");
        assert!(
            r.status.success(),
            "{} {}",
            String::from_utf8_lossy(&r.stdout),
            String::from_utf8_lossy(&r.stderr)
        );
        assert!(String::from_utf8_lossy(&r.stdout).contains("DONE"));
        let j = e.job(&jid);
        assert_eq!(j["status"], "succeeded");
        assert_eq!(j["steps"], 2);
        assert_eq!(j["tokens"]["total"], 105710);
        let mut expected_prompt = prompt.to_vec();
        expected_prompt.extend_from_slice(cc_job_policy::READ_GUIDANCE.as_bytes());
        assert_eq!(fs::read(e.rec.join("stdin.bin")).unwrap(), expected_prompt);
        let call: Value =
            serde_json::from_slice(&fs::read(e.rec.join("call.json")).unwrap()).unwrap();
        assert_eq!(
            s(&call, "cwd").to_lowercase(),
            cwd.to_str().unwrap().to_lowercase()
        );
        let args = call["argv"].as_array().unwrap();
        assert!(!args.iter().any(|v| v == "-C"));
        assert_eq!(args.first().unwrap(), "exec");
        assert_eq!(args.last().unwrap(), "-");
        let tail = String::from_utf8(e.cli(&["tail", &jid], "").stdout).unwrap();
        assert!(
            regex::Regex::new(
                r"\d\d:\d\d:\d\d   -> exit 0, 2 lines: Get-ChildItem -File \| Select-Object"
            )
            .unwrap()
            .is_match(&tail)
        );
        let raw = fs::read(e.tmp.0.join("tool/jobs").join(jid).join("job.json")).unwrap();
        assert!(raw.windows(2).any(|s| s == b"\r\n"));
    }
    #[test]
    fn test_finishes_even_if_a_grandchild_holds_stdout_open() {
        let e = E2e::new();
        let jid = e.start("hold", &e.tmp.0, b"hi");
        let start = Instant::now();
        let r = e.cli(&["wait", &jid, "--timeout", "25"], "");
        assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stdout));
        assert!(start.elapsed() < Duration::from_secs(15));
        assert!(e.rec.join("hold.pid").exists());
        assert!(
            proc_created(
                fs::read_to_string(e.rec.join("hold.pid"))
                    .unwrap()
                    .parse()
                    .unwrap()
            )
            .is_some()
        );
    }
    #[test]
    fn test_cancel_kills_the_whole_tree() {
        let e = E2e::new();
        let (jid, pids) = e.slow();
        let r = e.cli(&["cancel", &jid], "");
        assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stderr));
        assert!(pids.iter().all(|p| proc_created(*p).is_none()));
        std::thread::sleep(Duration::from_millis(100));
        assert_eq!(e.job(&jid)["status"], "cancelled");
    }
    #[test]
    fn test_cancel_of_a_lost_job_kills_the_leftover_codex() {
        let e = E2e::new();
        let (jid, pids) = e.slow();
        let mut c = hidden_command("taskkill");
        let r = c
            .args(["/F", "/PID", &pids[0].to_string()])
            .output()
            .unwrap();
        assert!(r.status.success());
        e.until(|| {
            serde_json::from_slice::<Vec<Value>>(&e.cli(&["status", "--json"], "").stdout).unwrap()
                [0]["status"]
                == "lost"
        });
        assert!(proc_created(pids[2]).is_some());
        let r = e.cli(&["cancel", &jid], "");
        assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stderr));
        assert!(String::from_utf8_lossy(&r.stdout).contains("killed the leftover"));
        assert!(pids[1..].iter().all(|p| proc_created(*p).is_none()));
    }
}
mod additional_compatibility {
    use super::*;
    #[test]
    fn cargo_path_wrapper_runs_the_rust_gate_and_preserves_streaming() {
        let t = Test::new();
        let bin = t.ctx.root.join("bin");
        fs::create_dir_all(&bin).unwrap();
        fs::copy(
            env!("CARGO_BIN_EXE_cargo_gate"),
            t.ctx.root.join("cargo_gate.exe"),
        )
        .unwrap();
        fs::write(
            bin.join("cargo.cmd"),
            include_str!("../bin/cargo.cmd").replace("\n", "\r\n"),
        )
        .unwrap();
        let mut env = t.ctx.job_environment();
        // This fixture must not contend with the production shared-cache gate.
        env.insert(
            "CARGO_TARGET_DIR".into(),
            t.ctx.root.join("target").to_string_lossy().into(),
        );
        env.insert(
            "CODEX_JOBS_REAL_CARGO".into(),
            env!("CARGO_BIN_EXE_cc_jobs_fixture").into(),
        );
        env.insert(
            "CODEX_JOBS_CARGO_LOCK_DIR".into(),
            t.ctx.root.to_string_lossy().into(),
        );
        assert_eq!(
            cc_jobs::platform::which("cargo", &env),
            Some(bin.join("cargo.cmd").to_string_lossy().into())
        );
        let mut c = hidden_command("cmd.exe");
        c.args(["/d", "/s", "/c", "cargo --echo-env"])
            .envs(env)
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD");
        let out = c.output().unwrap();
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert_eq!(String::from_utf8(out.stdout).unwrap().trim(), "1");
        assert!(t.ctx.root.join("cargo-build.lock.0").exists());
    }
    #[test]
    fn win32_sharing_and_lock_violations_retry_reads_and_replacement_fifty_times() {
        let t = Test::new();
        let j = t.put(new_job());
        let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
        for code in [5, 32, 33] {
            t.rt.replace_os.store(code, Ordering::Relaxed);
            t.rt.replace_failures.store(2, Ordering::Relaxed);
            let before = t.rt.replace_count.load(Ordering::Relaxed);
            t.ctx.save(&j).unwrap();
            assert_eq!(t.rt.replace_count.load(Ordering::Relaxed) - before, 3);
        }
        t.rt.load_failures.store(2, Ordering::Relaxed);
        let before = t.rt.load_count.load(Ordering::Relaxed);
        assert_eq!(t.ctx.read_json(&path).unwrap(), j);
        assert_eq!(t.rt.load_count.load(Ordering::Relaxed) - before, 3);
        t.rt.replace_failures.store(50, Ordering::Relaxed);
        let before = t.rt.replace_count.load(Ordering::Relaxed);
        assert!(t.ctx.save(&j).is_err());
        assert_eq!(t.rt.replace_count.load(Ordering::Relaxed) - before, 50);
        t.rt.load_failures.store(50, Ordering::Relaxed);
        let before = t.rt.load_count.load(Ordering::Relaxed);
        assert!(t.ctx.read_json(&path).is_err());
        assert_eq!(t.rt.load_count.load(Ordering::Relaxed) - before, 50);
    }
    #[test]
    fn unc_paths_keep_their_network_root_when_normalized() {
        assert_eq!(
            cc_jobs::cli::normalize_windows_path(Path::new(r"\\?\UNC\server\share\日本語")),
            PathBuf::from(r"\\server\share\日本語")
        );
        assert_eq!(
            cc_jobs::cli::normalize_windows_path(Path::new(r"\\?\D:\work")),
            PathBuf::from(r"D:\work")
        );
    }
    #[test]
    fn secret_keys_are_case_insensitive_and_pumps_cache_redaction_patterns() {
        let t = Test::new();
        t.rt.env
            .lock()
            .unwrap()
            .insert("godd_discord_bot_token".into(), "fake-case-token".into());
        let env = t.ctx.job_environment();
        assert!(
            !env.keys()
                .any(|k| k.eq_ignore_ascii_case("GODD_DISCORD_BOT_TOKEN"))
        );
        assert_eq!(
            redact_local_log_secrets("FAKE-CASE-TOKEN", &t.rt.env()),
            "[秘密]"
        );
        let p = t.ctx.root.join("case.log");
        pump_stderr(
            b"FAKE-CASE-TOKEN\nfake-case-token\n".as_slice(),
            &p,
            t.rt.as_ref(),
        )
        .unwrap();
        assert_eq!(fs::read_to_string(p).unwrap(), "[秘密]\n[秘密]\n");
    }
    #[test]
    fn queued_supervisor_setup_failure_is_recorded_as_failed() {
        let t = Test::new();
        let j = t.put(new_job());
        fs::create_dir(t.ctx.job_dir(s(&j, "id")).join("state.lock")).unwrap();
        t.ctx.supervise(s(&j, "id")).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], "failed");
        assert!(s(&saved, "last_error").starts_with("supervisor error:"));
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn real_live_process_with_reused_pid_is_never_killed() {
        let t = Test::new();
        let mut c = hidden_command(env!("CARGO_BIN_EXE_cc_jobs_fixture"));
        c.arg("--sleep").env("CC_JOBS_FIXTURE_DIR", &t.ctx.root);
        let mut child = c.spawn().unwrap();
        let pid = child.id();
        let created = proc_created(pid).unwrap();
        let mut ctx = t.ctx.clone();
        ctx.rt = Arc::new(System::default());
        ctx.gto = t.ctx.root.join("missing-gto.exe");
        let j=t.put(job(json!({"status":"running","pid":pid,"pid_created":created+1,"codex_pid":pid,"codex_pid_created":created+1})));
        let mut out = vec![];
        let mut err = vec![];
        let result = ctx.cmd_cancel(s(&j, "id"), &mut out, &mut err);
        let alive = child.try_wait().unwrap().is_none();
        child.kill().unwrap();
        child.wait().unwrap();
        assert_eq!(result.unwrap(), 0);
        assert!(alive);
        assert!(
            String::from_utf8(out)
                .unwrap()
                .contains("no leftover process to kill")
        );
        assert_eq!(ctx.load(s(&j, "id")).unwrap()["status"], "lost");
    }
    #[test]
    fn supervisor_drains_events_arriving_during_the_final_process_poll() {
        struct LateReader {
            release: Arc<AtomicBool>,
            eof: Arc<AtomicBool>,
            bytes: Cursor<Vec<u8>>,
        }
        impl Read for LateReader {
            fn read(&mut self, b: &mut [u8]) -> io::Result<usize> {
                while !self.release.load(Ordering::Acquire) {
                    std::thread::sleep(Duration::from_millis(1));
                }
                let n = self.bytes.read(b)?;
                if n == 0 {
                    self.eof.store(true, Ordering::Release);
                }
                Ok(n)
            }
        }
        struct LateChild {
            release: Arc<AtomicBool>,
            eof: Arc<AtomicBool>,
            reader: Option<Box<dyn Read + Send>>,
        }
        impl Running for LateChild {
            fn pid(&self) -> u32 {
                321
            }
            fn stdout(&mut self) -> Option<Box<dyn Read + Send>> {
                self.reader.take()
            }
            fn stderr(&mut self) -> Option<Box<dyn Read + Send>> {
                None
            }
            fn kill(&mut self) -> io::Result<()> {
                Ok(())
            }
            fn poll(&mut self) -> io::Result<Option<i64>> {
                self.release.store(true, Ordering::Release);
                while !self.eof.load(Ordering::Acquire) {
                    std::thread::sleep(Duration::from_millis(1));
                }
                std::thread::sleep(Duration::from_millis(20));
                Ok(Some(0))
            }
        }
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), b"hi").unwrap();
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(|| {
            let release = Arc::new(AtomicBool::new(false));
            let eof = Arc::new(AtomicBool::new(false));
            let bytes=b"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"late result\"}}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}\n".to_vec();
            Box::new(LateChild {
                release: release.clone(),
                eof: eof.clone(),
                reader: Some(Box::new(LateReader {
                    release,
                    eof,
                    bytes: Cursor::new(bytes),
                })),
            })
        }));
        t.ctx.supervise(s(&j, "id")).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["last_message"], "late result");
        assert_eq!(saved["tokens"]["total"], 3);
    }
    #[test]
    fn help_and_parser_errors_match_original_python_bytes_and_codes() {
        let t = Test::new();
        let cases: Vec<Value> =
            serde_json::from_str(include_str!("fixtures/cli_parity.json")).unwrap();
        assert_eq!(cases.len(), 29);
        for case in cases {
            let args = case["args"]
                .as_array()
                .unwrap()
                .iter()
                .map(|v| v.as_str().unwrap())
                .collect::<Vec<_>>();
            let (r, out, mut err) = t.cli(&args);
            let code = match r {
                Ok(code) => code,
                Err(e) => {
                    err.push_str(&e.message);
                    err.push('\n');
                    e.code
                }
            };
            assert_eq!(code, case["code"].as_i64().unwrap() as i32, "{args:?}");
            assert_eq!(out, s(&case, "stdout"), "{args:?} stdout");
            assert_eq!(err, s(&case, "stderr"), "{args:?} stderr");
        }
        assert!(t.rt.calls.lock().unwrap().is_empty());
        assert!(t.ctx.list_jobs(&HashSet::new()).is_empty());
    }
    #[test]
    fn windows_fault_exit_is_unsigned_in_process_result_and_job_json() {
        let rt = System::default();
        let reply = rt
            .call(Call::new(
                vec![
                    env!("CARGO_BIN_EXE_cc_jobs_fixture").into(),
                    "--fault-exit".into(),
                ],
                vec![],
                5.,
            ))
            .unwrap();
        assert_eq!(reply.code, 3221225477);
        let t = Test::new();
        let mut j = t.put(new_job());
        finish(&mut j, Some(reply.code), 10, &t.rt.now());
        t.ctx.save(&j).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["exit_code"], 3221225477_i64);
        assert_eq!(
            saved["last_error"],
            "codex exited with 3221225477 without turn.completed"
        );
    }
    #[test]
    fn state_json_uses_python_indentation_order_crlf_and_unicode() {
        let t = Test::new();
        let j = json!({"id":"format","status":"queued","name":"日本語🚀","tokens":{"input_tokens":1,"total":2}});
        t.put(j);
        assert_eq!(fs::read(t.ctx.job_dir("format").join("job.json")).unwrap(),"{\r\n \"id\": \"format\",\r\n \"status\": \"queued\",\r\n \"name\": \"日本語🚀\",\r\n \"tokens\": {\r\n  \"input_tokens\": 1,\r\n  \"total\": 2\r\n }\r\n}".as_bytes());
    }
    #[test]
    fn resume_uuid_and_stdin_global_options_preserve_original_contract() {
        let t = Test::new();
        assert!(
            t.run(&["--resume-session", "not-a-session"], b"hi")
                .unwrap_err()
                .message
                .contains("session UUID")
        );
        let id = "01a0f188-0c24-7860-ab46-6c424b1def08";
        t.run(&["--resume-session", &id.to_uppercase()], b"hi")
            .unwrap();
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs[0]["resume_session"], id);
        let args = codex_args(&jobs[0], &t.ctx.job_dir(s(&jobs[0], "id")));
        assert_eq!(&args[args.len() - 3..], ["resume", id, "-"]);
        let date = t.ctx.codex_home.join("sessions/2020/01/01");
        fs::create_dir_all(&date).unwrap();
        let path = date.join(format!("rollout-old-{id}.jsonl"));
        fs::write(&path, b"").unwrap();
        let j = patch(
            jobs[0].clone(),
            json!({"thread_id":"new","started_at":t.rt.now()}),
        );
        assert_eq!(t.ctx.session_file(&j), Some(path));
    }
    #[test]
    fn msvcrt_byte_lock_and_rust_lock_exclude_each_other_without_changing_file() {
        #[link(name = "msvcrt")]
        unsafe extern "C" {
            fn _open(path: *const std::ffi::c_char, flags: i32, ...) -> i32;
            fn _locking(fd: i32, mode: i32, n: i64) -> i32;
            fn _lseek(fd: i32, offset: i32, origin: i32) -> i32;
            fn _close(fd: i32) -> i32;
        }
        let tmp = Temp::new();
        let path = tmp.0.join("crt.lock");
        fs::write(&path, b"sentinel").unwrap();
        let name = std::ffi::CString::new(path.to_str().unwrap()).unwrap();
        unsafe {
            let fd = _open(name.as_ptr(), 0x0002 | 0x8000 | 0x0080);
            assert!(fd >= 0);
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 2, 1), 0);
            assert!(FileLock::try_acquire(&path).is_err());
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 0, 1), 0);
            let lock = FileLock::try_acquire(&path).unwrap();
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_ne!(_locking(fd, 2, 1), 0);
            drop(lock);
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 2, 1), 0);
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 0, 1), 0);
            assert_eq!(_close(fd), 0);
        }
        assert_eq!(fs::read(path).unwrap(), b"sentinel");
    }
    #[test]
    fn cargo_unlocked_commands_and_nested_gate_do_not_consume_a_slot() {
        let env = BTreeMap::new();
        for command in cc_jobs::gate::UNLOCKED {
            assert!(!cc_jobs::gate::needs_lock(&[command.to_string()], &env));
        }
        assert!(cc_jobs::gate::needs_lock(
            &["--release".into(), "build".into()],
            &env
        ));
        assert!(!cc_jobs::gate::needs_lock(
            &["build".into()],
            &BTreeMap::from([("CODEX_JOBS_CARGO_LOCK_HELD".into(), "1".into())])
        ));
        let tmp = Temp::new();
        let mut c = hidden_command(env!("CARGO_BIN_EXE_cargo_gate"));
        c.args(["--echo-env"])
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env("CODEX_JOBS_CARGO_LOCK_DIR", &tmp.0)
            .env("CARGO_TARGET_DIR", &tmp.0)
            .env("CODEX_JOBS_CARGO_LOCK_HELD", "1");
        let out = c.output().unwrap();
        assert!(out.status.success());
        assert_eq!(String::from_utf8(out.stdout).unwrap().trim(), "1");
        assert_eq!(fs::read_dir(&tmp.0).unwrap().count(), 0);
    }
    #[test]
    fn external_failure_stderr_never_leaks_credentials() {
        let t = Test::new();
        fs::copy(env!("CARGO_BIN_EXE_codex_jobs"), &t.ctx.executable).unwrap();
        let fake = t.ctx.root.join("fake-gto.exe");
        fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), &fake).unwrap();
        let j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        let mut c = hidden_command(t.ctx.executable.to_str().unwrap());
        c.args(["rate", s(&j, "id"), "--quality", "4"])
            .env("CODEX_JOBS_GTO_BIN", fake)
            .env("CC_JOBS_FIXTURE_MODE", "external-error")
            .env("CC_JOBS_FIXTURE_DIR", &t.ctx.root)
            .env("GODD_DISCORD_BOT_TOKEN", "SENTINEL-TOKEN");
        let out = c.output().unwrap();
        assert_eq!(out.status.code(), Some(1));
        assert_eq!(
            String::from_utf8(out.stderr).unwrap().replace("\r\n", "\n"),
            "gto ingest-turn failed (1)\n"
        );
        assert!(out.stdout.is_empty());
        assert!(t.ctx.load(s(&j, "id")).unwrap().get("gto").is_none());
    }
}
