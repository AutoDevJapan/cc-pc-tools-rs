use super::*;
use cc_scheduler::targeted_dispatch;

fn save_settings(f: &Fixture) {
    write_json(&f.e.root.join("config.json"), &f.cfg, 1).unwrap();
    write_json(&f.e.root.join("priorities.json"), &f.pri, 1).unwrap();
}

#[test]
fn targeted_dispatch_resumes_only_the_selected_session_and_reads_the_current_spec() {
    let mut f = Fixture::new();
    f.managed(2, 1, "other", 0);
    let cwd = f.get(2)["scheduler"]["worktree"].clone();
    let session = "12345678-1234-4321-9234-123456789abc";
    let source = &mut f.e.backend.records[0];
    source["status"] = json!("succeeded");
    source["last_message"] = json!("状態: 継続中\n過去の記録にはblocked by policyとあります。");
    source["thread_id"] = json!(session);
    source["cwd"] = cwd.clone();
    source["sandbox"] = json!("read-only");
    source["mcp_profile"] = json!("browser");
    source["network"] = json!(false);
    source["add_dirs"] = json!([]);
    f.e.backend.ps.clear();
    f.reconcile();
    f.e.backend.clock += 61.;
    f.e.edit(&json!(2), |row| {
        row["sandbox"] = json!("read-only");
        row["task_type"] = json!("research");
    })
    .unwrap();
    let operation = "元の対象だけをcc-console.exe todo list --id 2で確認する。";
    fs::write(s(&f.get(2)["spec"]), operation).unwrap();
    f.put(item(1, "other", "todo", &[]));
    let other = f.get(1);
    save_settings(&f);

    let result = targeted_dispatch::run(&mut f.e, 2).unwrap();
    assert_eq!(result["launched"], "launched1");
    assert_eq!(f.e.backend.launched.len(), 1);
    let launched = &f.e.backend.launched[0];
    assert_eq!(launched["item"]["id"], 2);
    assert_eq!(launched["item"]["scheduler"]["worktree"], cwd);
    assert_eq!(launched["item"]["scheduler"]["resume_session"], session);
    assert_eq!(launched["item"]["needs"], json!([]));
    let command = RealBackend::new(f.e.root.clone()).launch_command(
        &launched["item"],
        Path::new("prompt.md"),
        &launched["item"]["scheduler"]["selection"],
    );
    assert!(command
        .args
        .windows(2)
        .any(|pair| pair == ["--sandbox", "read-only"]));
    assert!(command
        .args
        .windows(2)
        .any(|pair| pair == ["--mcp-profile", "browser"]));
    assert!(!command
        .args
        .iter()
        .any(|arg| arg == "--network" || arg == "--add-dir"));
    assert!(s(&launched["text"]).contains(operation));
    assert_eq!(f.get(1), other);
    assert!(targeted_dispatch::run(&mut f.e, 2).unwrap()["launched"].is_null());
    assert_eq!(f.e.backend.launched.len(), 1);
    assert!(f.e.backend.cancelled.is_empty());
    assert!(f.e.backend.killed.is_empty());
    assert!(f.e.backend.notices.is_empty());
}

#[test]
fn targeted_dispatch_keeps_stops_dependencies_resources_and_priority() {
    for gate in [
        "disabled",
        "owner",
        "question",
        "wait",
        "dependency",
        "cargo",
        "cpu",
        "ram",
        "priority",
        "recovery",
    ] {
        let mut f = Fixture::new();
        f.put(item(2, "other", "todo", &["cargo"]));
        match gate {
            "disabled" => f.cfg["dispatch_enabled"] = json!(false),
            "owner" => {
                f.e.edit(&json!(2), |row| row["stopped"] = json!(true))
                    .unwrap();
            }
            "question" => {
                f.e.edit(&json!(2), |row| {
                    row["owner_question_wait"] = json!({"question":"existing"})
                })
                .unwrap();
            }
            "wait" => {
                f.e.edit(&json!(2), |row| row["status"] = json!("block"))
                    .unwrap();
            }
            "dependency" => {
                f.put(item(1, "other", "block", &[]));
                f.e.edit(&json!(2), |row| row["wait_on"] = json!([1]))
                    .unwrap();
            }
            "cargo" => {
                f.e.backend.ps.insert(3, proc(3, 0, "cargo.exe"));
            }
            "cpu" => f.e.backend.m["cpu_percent"] = json!(99),
            "ram" => f.e.backend.m["available_gb"] = json!(3),
            "priority" => {
                f.put(item(1, "civ6", "todo", &[]));
            }
            "recovery" => {
                f.e.edit(&json!(2), |row| {
                    row["stall_recovery"] = json!({"pending":{"request":"existing"}})
                })
                .unwrap();
            }
            _ => unreachable!(),
        }
        save_settings(&f);
        let before = f.get(2);
        let report = targeted_dispatch::run(&mut f.e, 2).unwrap();
        assert!(report["launched"].is_null(), "{gate}: {report}");
        assert!(f.e.backend.launched.is_empty(), "{gate}");
        assert_eq!(f.get(2), before, "{gate}");
        assert!(f.e.backend.cancelled.is_empty(), "{gate}");
        assert!(f.e.backend.notices.is_empty(), "{gate}");
    }
}

#[test]
fn targeted_dispatch_obtains_the_normal_subwork_allocation_instead_of_forging_it() {
    let mut sub = subwork_support::Fixture::new();
    sub.register();
    sub.db["items"][1]["scheduler"]
        .as_object_mut()
        .unwrap()
        .remove("subwork_admission");
    sub.jobs[0]["pid"] = json!(1);
    sub.jobs[0]["created_at"] = json!(T - 1200.);
    sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
    let mut f = Fixture::new();
    f.e.backend.records = sub.jobs.clone();
    f.e.backend.launch_start = Some(f.e.todo_path.clone());
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    save_settings(&f);
    let mut unallocated = sub.child_job();
    assert!(cc_job_policy::subwork::refresh_job(&sub.db, &sub.jobs, &mut unallocated).is_err());
    let report = targeted_dispatch::run(&mut f.e, 2).unwrap();
    assert_eq!(report["launched"], "launched1");
    assert_eq!(f.e.backend.launched.len(), 1);
    let row = f.get(2);
    assert!(row["scheduler"]["subwork_admission"].is_object());
    assert_eq!(row["subwork"], sub.db["items"][1]["subwork"]);
    assert_eq!(row["needs"], sub.db["items"][1]["needs"]);
    assert_eq!(
        row["scheduler"]["worktree"],
        sub.db["items"][1]["scheduler"]["worktree"]
    );
    unallocated["id"] = report["launched"].clone();
    unallocated["launch_key"] = f.e.backend.records.last().unwrap()["launch_key"].clone();
    cc_job_policy::subwork::refresh_job(&f.e.todo().unwrap(), &sub.jobs, &mut unallocated).unwrap();
}

#[test]
fn targeted_dispatch_keeps_the_lock_and_rejects_an_unknown_id() {
    let mut f = Fixture::new();
    f.put(item(2, "other", "todo", &[]));
    save_settings(&f);
    let _lock = ByteLock::acquire(&f.e.root.join("scheduler.lock"), Duration::ZERO)
        .unwrap()
        .unwrap();
    assert_eq!(targeted_dispatch::run(&mut f.e, 2).unwrap()["busy"], true);
    drop(_lock);
    assert!(targeted_dispatch::run(&mut f.e, 999).is_err());
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn targeted_dispatch_does_not_restore_writer_capabilities_into_a_readonly_request() {
    let mut f = Fixture::new();
    f.managed(2, 1, "other", 0);
    let cwd = f.get(2)["scheduler"]["worktree"].clone();
    let source = &mut f.e.backend.records[0];
    source["status"] = json!("succeeded");
    source["last_message"] = json!("状態: 継続中");
    source["thread_id"] = json!("12345678-1234-4321-9234-123456789abc");
    source["cwd"] = cwd;
    source["sandbox"] = json!("danger-full-access");
    source["network"] = json!(true);
    source["add_dirs"] = json!(["D:/old-writer-scope"]);
    f.e.backend.ps.clear();
    f.reconcile();
    f.e.backend.clock += 61.;
    f.e.edit(&json!(2), |row| {
        row["scheduler"]["console_request"] = json!({"read_only":true});
    })
    .unwrap();
    save_settings(&f);
    targeted_dispatch::run(&mut f.e, 2).unwrap();
    let selected = &f.e.backend.launched[0]["item"]["scheduler"];
    assert_eq!(selected["selection"]["sandbox"], "read-only");
    assert!(selected["resume_session"].is_null());
    assert_ne!(selected["selection"]["network"], true);
    assert!(selected["selection"]["add_dirs"].is_null());
}

#[test]
fn targeted_dispatch_preserves_a_stop_recorded_during_preparation() {
    let mut f = Fixture::new();
    f.put(item(2, "other", "todo", &[]));
    save_settings(&f);
    let mut changed = f.e.todo().unwrap();
    changed["items"][0]["stopped"] = json!(true);
    f.e.backend.prepare_edit = Some((f.e.todo_path.clone(), changed));
    targeted_dispatch::run(&mut f.e, 2).unwrap();
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(f.get(2)["stopped"], true);
}

#[test]
fn targeted_dispatch_rejects_a_subwork_workspace_that_no_longer_matches() {
    let mut sub = subwork_support::Fixture::new();
    sub.register();
    sub.db["items"][1]["scheduler"]["branch"] = json!("work/unrelated");
    sub.jobs[0]["pid"] = json!(1);
    sub.jobs[0]["created_at"] = json!(T - 1200.);
    sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
    let mut f = Fixture::new();
    f.e.backend.records = sub.jobs.clone();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    save_settings(&f);
    assert!(targeted_dispatch::run(&mut f.e, 2).is_err());
    assert!(f.e.backend.launched.is_empty());
    assert_eq!(f.get(2)["scheduler"]["branch"], "work/unrelated");
}
