use super::*;
use cc_job_policy::action_progress::{self, Progress};
fn setup() -> (Test, Value, Value, PathBuf) {
    let t = Test::new();
    let repo = t.ctx.root.join("repo");
    fs::create_dir_all(repo.join(".git")).unwrap();
    fs::write(
        repo.join(".git/config"),
        "[remote \"origin\"]\nurl = https://github.com/AutoDevJapan/cc-pc-tools-rs.git\n",
    )
    .unwrap();
    fs::write(
        repo.join(".git/HEAD"),
        "ref: refs/heads/scheduler/todo-1-fixture\n",
    )
    .unwrap();
    let j=t.put(job(json!({"id":"source","todo_id":1,"status":"running","pid":41,"pid_created":11,"codex_pid":42,"codex_pid_created":22,
        "started_at":"2026-10-01T11:00:00+09:00","task_type":"implementation","sandbox":"danger-full-access","cwd":repo,"attempt_events_offset":0})));
    t.rt.procs.lock().unwrap().extend([(41, 11), (42, 22)]);
    t.rt.pause_allowed.store(true, Ordering::Relaxed);
    let db = json!({"projects":{"cc-pc-tools-rs":repo},"items":[{"id":1,"project":"cc-pc-tools-rs","status":"doing","resume_generation":3,
        "worker_receipt":{"job":"source","generation":3},"scheduler":{"job":"source","worktree":repo,"branch":"scheduler/todo-1-fixture"}},{"id":2,"status":"todo"}]});
    let store = t.ctx.root.join("todo.json");
    fs::write(&store, db.to_string()).unwrap();
    let events = t.ctx.job_dir("source").join("events.jsonl");
    fs::write(
        &events,
        "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\"}}\n".repeat(3),
    )
    .unwrap();
    let p = action_progress::scan(&events, &j, &Progress::default(), t.rt.epoch() - 1801.).unwrap();
    let proof = json!({"ticket":action_progress::ticket(&db,&db["items"][0],&j),"progress":p});
    let path = t.ctx.root.join("proof.json");
    fs::write(&path, proof.to_string()).unwrap();
    (t, db, j, path)
}
fn cancel(t: &Test, path: &Path) -> (Result<i32>, String, String) {
    t.cli(&[
        "cancel",
        "source",
        "--recovery",
        "--if-todo-ready",
        "--todo-id",
        "1",
        "--cancel-key",
        "no-action-source",
        "--no-action-check",
        path.to_str().unwrap(),
    ])
}
#[test]
fn no_action_guard_cancels_once_and_retains_safe_proof() {
    let (t, _, _, path) = setup();
    let (r, out, err) = cancel(&t, &path);
    assert_eq!(r.unwrap(), 0, "{err}");
    assert_eq!(
        serde_json::from_str::<Value>(&out).unwrap()["outcome"],
        "cancelled"
    );
    let saved = t.ctx.load("source").unwrap();
    assert_eq!(saved["status"], "cancelled");
    assert!(saved["no_action_recovery"].is_object());
    assert_eq!(saved["gto_auto_pending"], true);
    assert!(
        t.rt.calls.lock().unwrap().is_empty(),
        "network/rating entered the bounded cancellation path"
    );
    let n = t.rt.kills.lock().unwrap().len();
    let (r, out, err) = cancel(&t, &path);
    assert_eq!(r.unwrap(), 0, "{err}");
    assert_eq!(
        serde_json::from_str::<Value>(&out).unwrap()["outcome"],
        "deferred"
    );
    assert_eq!(t.rt.kills.lock().unwrap().len(), n);
}
#[test]
fn no_action_guard_refreshes_generation_owner_wait_worker_and_tools_at_commit() {
    for case in [
        "generation",
        "owner",
        "dependency",
        "worker",
        "tool",
        "partial",
        "child",
        "attempt",
        "readonly",
    ] {
        let (t, mut db, mut j, path) = setup();
        match case {
            "generation" => db["items"][0]["resume_generation"] = json!(4),
            "owner" => db["items"][0]["stopped"] = json!(true),
            "dependency" => db["items"][0]["wait_on"] = json!([2]),
            "worker" => db["items"][0]["scheduler"]["job"] = json!("new"),
            "readonly" => j["sandbox"] = json!("read-only"),
            "attempt" => j["codex_pid_created"] = json!(23),
            "child" => t.rt.pause_allowed.store(false, Ordering::Relaxed),
            "tool" | "partial" => {
                let events = t.ctx.job_dir("source").join("events.jsonl");
                *t.rt.pause_hook.lock().unwrap() = Some(Box::new(move || {
                    use std::io::Write;
                    fs::OpenOptions::new().append(true).open(&events).unwrap().write_all(if case=="tool"{b"{\"type\":\"item.started\",\"item\":{\"id\":\"cmd\",\"type\":\"command_execution\"}}\n"}else{b"{"}).unwrap();
                }));
            }
            _ => unreachable!(),
        }
        fs::write(t.ctx.root.join("todo.json"), db.to_string()).unwrap();
        t.ctx.save(&j).unwrap();
        let (r, out, err) = cancel(&t, &path);
        assert_eq!(r.unwrap(), 0, "{case}: {err}");
        assert_eq!(
            serde_json::from_str::<Value>(&out).unwrap()["outcome"],
            "deferred",
            "{case}"
        );
        assert!(t.rt.kills.lock().unwrap().is_empty(), "{case}");
        assert_eq!(t.ctx.load("source").unwrap(), j);
    }
}
