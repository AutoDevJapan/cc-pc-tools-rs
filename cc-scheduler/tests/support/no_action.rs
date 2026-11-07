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
    j["task_type"] = json!("ci_fix");
    j["sandbox"] = json!("danger-full-access");
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
    for case in ["stop", "dependency", "generation", "worker", "recovery"] {
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
            "recovery" => row["stall_recovery"]["pending"] = json!({"reservation":"other"}),
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
    for case in ["review", "child", "scope", "unread", "restricted_scope"] {
        let mut f = setup();
        pass(&mut f);
        f.e.backend.clock += 1801.;
        match case {
            "review" => f.e.backend.records[0]["task_type"] = json!("review"),
            "restricted_scope" => f.e.backend.records[0]["sandbox"] = json!("workspace-write"),
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
        source["model_origin"] = json!(if explicit { "owner_request" } else { "policy" });
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

#[test]
fn recovery_reservation_registered_during_cancel_is_preserved_and_not_requeued() {
    let mut f = setup();
    pass(&mut f);
    f.e.backend.clock += 1801.;
    let mut db = f.e.todo().unwrap();
    db["items"][0]["stall_recovery"]["pending"] = json!({"reservation":"other"});
    f.e.backend.cancel_edit = Some((f.e.todo_path.clone(), db.clone()));
    pass(&mut f);
    assert_eq!(f.e.backend.cancelled, vec![json!("j1")]);
    assert_eq!(f.e.todo().unwrap(), db);
    f.dispatch();
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn unavailable_explicit_source_model_is_retained_without_automatic_rerouting() {
    let mut f = setup();
    pass(&mut f);
    f.e.backend.clock += 1801.;
    pass(&mut f);
    let source = &mut f.e.backend.records[0];
    source["no_action_recovery"] = json!({"ticket":"fixture"});
    source["model_explicit"] = json!(true);
    source["model"] = json!("gpt-6.1-luna");
    source["effort"] = json!("max");
    f.e.backend.clock = n(&f.get(1)["scheduler"]["not_before"]) + 1.;
    assert_eq!(
        f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
            .unwrap_err(),
        Error("ModelUnavailable")
    );
    assert_eq!(f.e.backend.pick_count, 1); // initial ordinary selection only; no reroute
    assert_eq!(f.e.backend.records[0]["model"], "gpt-6.1-luna");
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn workers_outside_the_existing_resume_boundary_are_never_cancelled() {
    for (project, allowed, recover) in [
        ("other", true, false),
        ("other", false, false),
        ("owner", true, false),
    ] {
        let mut f = setup();
        f.e.edit(&json!(1), |row| row["project"] = json!(project))
            .unwrap();
        f.e.backend.repair_allowed = allowed;
        pass(&mut f);
        f.e.backend.clock += 1801.;
        pass(&mut f);
        assert_eq!(
            f.e.backend.cancelled.len(),
            usize::from(recover),
            "{project}/{allowed}"
        );
    }
}

#[test]
fn new_arrivals_cannot_starve_the_next_scan_of_an_existing_worker() {
    let mut f = setup();
    fn add(f: &mut Fixture, id: u64) {
        let root = (id * 10) as u32;
        f.managed(id, root, "cc-pc-tools-rs", 0);
        let child = root + 1;
        f.e.backend.ps.insert(child, proc(child, root, "codex.exe"));
        let j = f.e.backend.records.last_mut().unwrap();
        j["todo_id"] = json!(id);
        j["sandbox"] = json!("danger-full-access");
        j["task_type"] = json!("ci_fix");
        j["started_at"] = json!("2026-10-18T00:00:00Z");
        j["codex_pid"] = json!(child);
        j["codex_pid_created"] = proc(child, root, "codex.exe")["created"].clone();
    }
    for id in 2..=4 {
        add(&mut f, id);
    }
    pass(&mut f);
    assert_eq!(f.e.state["no_action_cursor"], 2);
    assert_eq!(f.e.state["no_action_cycle_max"], 4);
    for id in 100..=101 {
        add(&mut f, id);
    }
    pass(&mut f);
    assert_eq!(f.e.state["no_action_cursor"], 4);
    for id in 102..=103 {
        add(&mut f, id);
    }
    pass(&mut f);
    assert_eq!(f.e.state["no_action_cursor"], 2);
    assert_eq!(f.e.state["no_action"]["j1"]["messages"], 3); // EOF revisit, not a duplicate read
    assert_eq!(f.e.state["no_action_cycle_max"], 103);
    assert!(f.e.backend.cancelled.is_empty());
}

#[test]
fn separate_live_child_job_protects_parent_until_the_child_really_finishes() {
    let mut f = setup();
    pass(&mut f);
    f.e.backend.clock += 1801.;
    f.e.backend.records.push(json!({"id":"child","parent_job_id":"j1","status":"running","pid":9,"pid_created":proc(9,0,"codex.exe")["created"]}));
    f.e.backend.ps.insert(9, proc(9, 0, "codex.exe"));
    pass(&mut f);
    assert!(f.e.backend.cancelled.is_empty());
    f.e.backend.records[1]["status"] = json!("succeeded");
    f.e.backend.ps.remove(&9);
    pass(&mut f);
    assert!(f.e.backend.cancelled.is_empty());
    f.e.backend.clock += 1801.;
    pass(&mut f);
    assert_eq!(f.e.backend.cancelled, vec![json!("j1")]);
}

#[test]
fn automatic_successor_is_recovered_again_without_requiring_a_new_agent_start_command() {
    let mut f = setup();
    f.e.edit(&json!(1), |r| {
        r["resume_generation"] = json!(7);
        r["worker_receipt"] = json!({"job":"j1","generation":7});
    })
    .unwrap();
    pass(&mut f);
    f.e.backend.clock += 1801.;
    pass(&mut f);
    f.e.backend.ps.clear();
    f.e.backend.clock = n(&f.get(1)["scheduler"]["not_before"]) + 1.;
    f.dispatch();
    assert_eq!(f.get(1)["worker_receipt"]["job"], "j1");
    let p = proc(99, 0, "codex.exe");
    let c = proc(100, 99, "codex.exe");
    f.e.backend.ps.insert(99, p.clone());
    f.e.backend.ps.insert(100, c.clone());
    let j = f.e.backend.records.last_mut().unwrap();
    j["pid_created"] = p["created"].clone();
    j["codex_pid"] = json!(100);
    j["codex_pid_created"] = c["created"].clone();
    j["task_type"] = json!("ci_fix");
    j["sandbox"] = json!("danger-full-access");
    j["started_at"] = json!("2026-10-19T00:00:00Z");
    pass(&mut f);
    f.e.backend.clock += 1801.;
    pass(&mut f);
    assert_eq!(f.e.backend.cancelled.len(), 2);
    assert_eq!(f.get(1)["scheduler"]["retry_count"], 2);
    assert!(
        n(&f.get(1)["scheduler"]["not_before"])
            >= f.e.backend.clock + cc_scheduler::recovery_failure::SLOW_RETRY_S
    );
    // A normal root's old agent receipt must not replace its newer result on
    // the third preparation. Normal roots do not advance that agent receipt.
    let cwd = f.get(1)["scheduler"]["worktree"].clone();
    let j = f.e.backend.records.last_mut().unwrap();
    j["cwd"] = cwd;
    j["status"] = json!("succeeded");
    j["last_message"] = json!("状態: 継続中\nLATEST_SECOND_WORKER_RESULT");
    j["thread_id"] = json!("87654321-1234-4321-9234-123456789abc");
    j["model"] = json!("gpt-6.1-sol"); j["effort"] = json!("max");
    j["launch_policy_version"] = json!(cc_job_policy::launch_context::VERSION);
    f.e.backend.records[0]["last_message"] = json!("状態: 継続中\nSTALE_FIRST_WORKER_RESULT");
    f.e.backend.ps.clear();
    f.e.backend.clock = n(&f.get(1)["scheduler"]["not_before"]) + 1.;
    f.e.edit(&json!(1), |r| r["scheduler"]["workspace_receipt"] = Value::Null).unwrap();
    let current = f.get(1);
    let prepared = f.e.prepare(&current, &f.repo.to_string_lossy()).unwrap().unwrap();
    let prompt = fs::read_to_string(s(&prepared["scheduler"]["prompt"])).unwrap();
    assert!(prompt.contains("LATEST_SECOND_WORKER_RESULT"));
    assert!(!prompt.contains("STALE_FIRST_WORKER_RESULT"));
    assert_eq!(prepared["scheduler"]["resume_session"], "87654321-1234-4321-9234-123456789abc");
    assert_eq!(prepared["worker_receipt"]["job"], "j1");
}
