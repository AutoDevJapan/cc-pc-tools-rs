use cc_job_policy::action_progress::*;
use serde_json::{Value, json};
use std::{fs, io::Write};
fn job() -> Value {
    json!({"id":"source","pid":1,"pid_created":11,"codex_pid":2,"codex_pid_created":22,"started_at":"2026-10-18T01:00:00Z","attempt_events_offset":0})
}
fn line(t: &str, id: &str, kind: &str) -> String {
    format!(
        "{}\n",
        json!({"type":t,"item":{"id":id,"type":kind,"status":"completed","text":"SECRET_DO_NOT_COPY","command":"PRIVATE_ARGUMENTS"}})
    )
}
fn messages() -> String {
    (0..3)
        .map(|i| line("item.completed", &format!("m{i}"), "agent_message"))
        .collect()
}
#[test]
fn repeated_messages_never_reset_real_progress_and_receipts_have_no_content() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("events");
    fs::write(&path, messages()).unwrap();
    let mut p = scan(&path, &job(), &Progress::default(), 1.).unwrap();
    for at in [600., 1200., 1801.] {
        fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all(messages().as_bytes())
            .unwrap();
        p = scan(&path, &job(), &p, at).unwrap();
    }
    assert!(p.stalled(1801.));
    let saved = serde_json::to_string(&p).unwrap();
    assert!(!saved.contains("SECRET"));
    assert!(!saved.contains("PRIVATE"));
}
#[test]
fn long_tools_and_real_completions_protect_the_worker() {
    for kind in [
        "command_execution",
        "file_change",
        "mcp_tool_call",
        "web_search",
    ] {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("events");
        fs::write(&path, line("item.started", "tool", kind) + &messages()).unwrap();
        let mut p = scan(&path, &job(), &Progress::default(), 1.).unwrap();
        p = scan(&path, &job(), &p, 10000.).unwrap();
        assert!(!p.stalled(10000.));
        fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all((line("item.completed", "tool", kind) + &messages()).as_bytes())
            .unwrap();
        p = scan(&path, &job(), &p, 10001.).unwrap();
        assert!(!p.stalled(10001.));
        assert!(p.stalled(11801.));
    }
}
#[test]
fn corruption_partial_truncation_replacement_and_new_attempt_are_not_idle() {
    for case in [
        "corrupt",
        "partial",
        "truncated",
        "replaced",
        "attempt",
        "unknown",
        "delegation",
        "oversize",
    ] {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("events");
        fs::write(&path, messages()).unwrap();
        let mut j = job();
        let p = scan(&path, &j, &Progress::default(), 1.).unwrap();
        match case {
            "corrupt" => {
                fs::OpenOptions::new()
                    .append(true)
                    .open(&path)
                    .unwrap()
                    .write_all(b"{bad}\n")
                    .unwrap();
            }
            "partial" => {
                fs::OpenOptions::new()
                    .append(true)
                    .open(&path)
                    .unwrap()
                    .write_all(b"{\"type\":")
                    .unwrap();
            }
            "truncated" => fs::write(&path, "").unwrap(),
            "replaced" => {
                fs::rename(&path, dir.path().join("old")).unwrap();
                fs::write(&path, messages()).unwrap();
            }
            "attempt" => j["codex_pid_created"] = json!(33),
            "unknown" | "delegation" => {
                fs::OpenOptions::new()
                    .append(true)
                    .open(&path)
                    .unwrap()
                    .write_all(
                        line(
                            "item.completed",
                            "x",
                            if case == "unknown" {
                                "future_tool"
                            } else {
                                "collab_tool_call"
                            },
                        )
                        .as_bytes(),
                    )
                    .unwrap();
            }
            "oversize" => {
                fs::OpenOptions::new()
                    .append(true)
                    .open(&path)
                    .unwrap()
                    .write_all(&vec![b'x'; MAX_LINE as usize + 5])
                    .unwrap();
            }
            _ => unreachable!(),
        }
        let next = scan(&path, &j, &p, 2000.).unwrap();
        assert!(!next.stalled(2000.), "{case}");
    }
}
#[test]
fn partial_line_is_retried_at_its_original_cursor_and_unreadable_is_error() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("events");
    let text = messages();
    fs::write(&path, &text[..text.len() - 2]).unwrap();
    let p = scan(&path, &job(), &Progress::default(), 1.).unwrap();
    assert!(!p.caught_up);
    fs::OpenOptions::new()
        .append(true)
        .open(&path)
        .unwrap()
        .write_all(&text.as_bytes()[text.len() - 2..])
        .unwrap();
    let p = scan(&path, &job(), &p, 2.).unwrap();
    assert!(p.caught_up);
    assert_eq!(p.messages, 3);
    assert!(scan(&dir.path().join("absent"), &job(), &p, 9999.).is_err());
}
#[test]
fn backlog_is_incremental_bounded_and_never_proves_idle() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("events");
    fs::write(&path, messages().repeat(400)).unwrap();
    let p = scan(&path, &job(), &Progress::default(), 1.).unwrap();
    assert_eq!(p.messages, MAX_EVENTS as u32);
    assert!(!p.caught_up);
    assert!(p.offset <= MAX_BYTES);
    let q = scan(&path, &job(), &p, 2000.).unwrap();
    assert!(q.offset > p.offset);
    assert!(!q.stalled(2000.));
}
#[test]
fn stop_wait_review_and_new_worker_are_not_candidates() {
    let j = {
        let mut j = job();
        j["status"] = json!("running");
        j["task_type"] = json!("implementation");
        j["todo_id"] = json!(1);
        j
    };
    let row = json!({"id":1,"project":"cc-pc-tools-rs","status":"doing","dispatchable":true,"spec":"saved-spec.md","resume_generation":1,"scheduler":{"job":"source"}});
    let db = json!({"items":[row,{"id":2,"status":"todo"}],"projects":{}});
    assert!(eligible(&db, &row, &j));
    let mut observed = j.clone();
    observed["task_type"] = json!("ci_fix");
    assert!(eligible(&db, &row, &observed));
    for case in [
        "owner",
        "wait",
        "ready",
        "review",
        "research",
        "worker",
        "generation",
        "quota",
        "readonly",
    ] {
        let (mut r, mut k) = (row.clone(), j.clone());
        match case {
            "owner" => r["stopped"] = json!(true),
            "wait" => r["wait_on"] = json!([2]),
            "ready" => r["wait_ready"] = json!({"condition":"x"}),
            "review" | "research" => k["task_type"] = json!(case),
            "worker" => r["scheduler"]["job"] = json!("other"),
            "generation" => r["resume_generation"] = json!(2),
            "quota" => k["capacity_wait_pending"] = json!(true),
            "readonly" => k["sandbox"] = json!("read-only"),
            _ => unreachable!(),
        }
        if case == "generation" {
            assert_ne!(ticket(&db, &r, &k), ticket(&db, &row, &j));
        } else {
            assert!(!eligible(&db, &r, &k), "{case}");
        }
    }
}
