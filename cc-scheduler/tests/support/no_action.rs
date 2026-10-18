// Included in parity.rs to reuse its isolated store/backend.
use super::*;
use std::io::Write;
fn setup() -> Fixture {
    let mut f = Fixture::new();
    f.managed(1, 1, "cc-pc-tools-rs", 0);
    let mut db = f.e.todo().unwrap();
    db["projects"]["cc-pc-tools-rs"] = json!(f.repo);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    let path = f.temp.path().join("events");
    fs::write(
        &path,
        "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\"}}\n".repeat(3),
    )
    .unwrap();
    f.e.backend.action_path = Some(path);
    f.e.backend.repair_allowed = true;
    f.e.backend.ps.insert(2, proc(2, 1, "codex.exe"));
    let cwd = f.get(1)["scheduler"]["worktree"].clone();
    let j = &mut f.e.backend.records[0];
    j["codex_pid"] = json!(2);
    j["codex_pid_created"] = proc(2, 1, "codex.exe")["created"].clone();
    j["started_at"] = json!("2026-10-18T00:00:00Z");
    j["task_type"] = json!("implementation");
    j["todo_id"] = json!(1);
    j["cwd"] = cwd;
    f.cfg["kill_enabled"] = json!(false);
    f
}
fn pass(f: &mut Fixture) {
    let mut jobs = f.e.backend.records.clone();
    let ps = f.e.backend.ps.clone();
    f.e.recover_no_action(&mut jobs, &ps).unwrap();
    f.e.backend.records = jobs;
}
#[test]
fn report_only_recovery_requeues_once_with_preserved_workspace_and_backoff() {
    let mut f = setup();
    let original = f.get(1);
    pass(&mut f);
    f.e.backend.clock += 1801.;
    fs::OpenOptions::new()
        .append(true)
        .open(f.e.backend.action_path.as_ref().unwrap())
        .unwrap()
        .write_all(b"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\"}}\n")
        .unwrap();
    pass(&mut f);
    pass(&mut f);
    assert_eq!(f.e.backend.cancelled, vec![json!("j1")]);
    let row = f.get(1);
    assert_eq!(row["status"], "todo");
    assert_eq!(
        row["scheduler"]["worktree"],
        original["scheduler"]["worktree"]
    );
    assert_eq!(row["scheduler"]["last_stop"], "no_action");
    assert_eq!(row["scheduler"]["retry_count"], 1);
    assert!(n(&row["scheduler"]["not_before"]) > f.e.backend.clock);
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
    f.e.backend.clock = n(&row["scheduler"]["not_before"]) + 1.;
    f.dispatch();
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
    assert_eq!(
        f.get(1)["scheduler"]["worktree"],
        original["scheduler"]["worktree"]
    );
}
#[test]
fn no_action_rechecks_stop_dependency_generation_and_worker_after_scope_check() {
    for case in ["stop", "dependency", "generation", "worker"] {
        let mut f = setup();
        pass(&mut f);
        f.e.backend.clock += 1801.;
        let mut db = f.e.todo().unwrap();
        let row = &mut db["items"][0];
        match case {
            "stop" => row["stopped"] = json!(true),
            "dependency" => row["wait_on"] = json!([999]),
            "generation" => row["resume_generation"] = json!(99),
            "worker" => row["scheduler"]["job"] = json!("new"),
            _ => unreachable!(),
        }
        f.e.backend.repair_edit = Some((f.e.todo_path.clone(), db.clone()));
        pass(&mut f);
        assert!(f.e.backend.cancelled.is_empty(), "{case}");
        assert_eq!(f.e.todo().unwrap(), db);
    }
}
#[test]
fn review_children_unknown_scope_and_unread_events_never_cancel() {
    for case in ["review", "child", "scope", "unread"] {
        let mut f = setup();
        pass(&mut f);
        f.e.backend.clock += 1801.;
        match case {
            "review" => f.e.backend.records[0]["task_type"] = json!("review"),
            "child" => {
                f.e.backend.ps.insert(3, proc(3, 2, "cargo.exe"));
            }
            "scope" => f.e.backend.repair_allowed = false,
            "unread" => f.e.backend.action_path = Some(f.temp.path().join("absent")),
            _ => unreachable!(),
        }
        pass(&mut f);
        assert!(f.e.backend.cancelled.is_empty(), "{case}");
    }
}
#[test]
fn repeated_no_action_failure_uses_technical_backoff_without_owner_question() {
    let mut f = setup();
    f.e.edit(&json!(1), |r| r["scheduler"]["retry_count"] = json!(2))
        .unwrap();
    pass(&mut f);
    f.e.backend.clock += 1801.;
    pass(&mut f);
    let row = f.get(1);
    assert_eq!(row["status"], "todo");
    assert!(row["scheduler"]["question"].is_null());
    assert_eq!(
        n(&row["scheduler"]["not_before"]),
        f.e.backend.clock + cc_scheduler::recovery_failure::SLOW_RETRY_S
    );
}

#[test]
fn failed_cancellation_is_bounded_before_hourly_technical_retry() {
    let mut f = setup();
    f.e.backend.cancel_ok = false;
    pass(&mut f);
    f.e.backend.clock += 1801.;
    pass(&mut f);
    pass(&mut f);
    assert_eq!(f.e.backend.cancelled.len(), 1);
    let retry = f.get(1)["scheduler"]["no_action_retry"].clone();
    assert_eq!(retry["count"], 1);
    f.e.backend.clock = n(&retry["not_before"]) + 1.;
    pass(&mut f);
    pass(&mut f);
    assert_eq!(f.e.backend.cancelled.len(), 2);
    let row = f.get(1);
    assert_eq!(row["status"], "doing");
    assert_eq!(
        n(&row["scheduler"]["no_action_retry"]["not_before"]),
        f.e.backend.clock + cc_scheduler::recovery_failure::SLOW_RETRY_S
    );
    assert!(row["scheduler"]["question"].is_null());
}

#[test]
fn no_action_resume_keeps_explicit_model_and_capabilities_and_routes_auto_to_implementation() {
    for explicit in [true, false] {
        let mut f = setup();
        pass(&mut f);
        f.e.backend.clock += 1801.;
        pass(&mut f);
        let source = &mut f.e.backend.records[0];
        source["no_action_recovery"] = json!({"ticket":"fixture"});
        source["model_explicit"] = json!(explicit);
        source["model"] = json!("gpt-6-luna");
        source["effort"] = json!("max");
        source["sandbox"] = json!("danger-full-access");
        source["mcp_profile"] = json!("minimal");
        source["network"] = json!(false);
        source["add_dirs"] = json!(["D:/fixture-scope"]);
        f.e.backend.clock = n(&f.get(1)["scheduler"]["not_before"]) + 1.;
        let prepared =
            f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                .unwrap()
                .unwrap();
        let selection = &prepared["scheduler"]["selection"];
        assert_eq!(
            selection["model"],
            if explicit {
                "gpt-6-luna"
            } else {
                "gpt-6.1-sol"
            }
        );
        assert_eq!(selection["model_explicit"], explicit);
        assert_eq!(selection["effort"], "max");
        assert_eq!(selection["mcp_profile"], "minimal");
        assert_eq!(selection["network"], false);
        assert_eq!(selection["add_dirs"], json!(["D:/fixture-scope"]));
        let backend = RealBackend::new(f.temp.path().join("scheduler"));
        let call = backend.launch_command(&prepared, Path::new("prompt.md"), selection);
        assert!(call
            .args
            .windows(2)
            .any(|p| p == ["--mcp-profile", "minimal"]));
        assert!(call
            .args
            .windows(2)
            .any(|p| p == ["--add-dir", "D:/fixture-scope"]));
        assert!(!call.args.iter().any(|a| a == "--network"));
        assert_eq!(
            call.args
                .iter()
                .filter(|a| a.as_str() == "--add-dir")
                .count(),
            1
        );
        assert!(call
            .args
            .iter()
            .any(|a| a == if explicit { "--model" } else { "--auto-model" }));
    }
}
