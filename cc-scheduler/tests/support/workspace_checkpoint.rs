use super::*;
use cc_scheduler::workspace_checkpoint::checkpoint;

fn main_fixture() -> (Fixture, cc_scheduler::workspace_checkpoint::Request, PathBuf) {
    let (mut f, mut request, target) = checkpoint_fixture();
    // All data is synthetic. Establish the same persisted assignment identity
    // as a normal scheduler launch, before the worker switches off main.
    run_git(
        &mut SystemRunner,
        &target,
        &["remote", "set-url", "origin", "https://github.com/example/repair.git"],
    )
    .unwrap();
    let mut assigned = cc_job_policy::work_owner::checkout(target.to_str().unwrap());
    assigned["branch"] = json!("main");
    f.e.backend.records[0]["work_owner"] = assigned;
    f.e.edit(&json!(1), |row| {
        row["scheduler"]["job"] = json!("owned");
        row["scheduler"]["branch"] = json!("main");
    })
    .unwrap();
    request.expected_branch = "main".into();
    (f, request, target)
}

#[test]
fn workspace_checkpoint_main_preserves_assignment_and_supports_normal_resume() {
    use cc_scheduler::{preparation::ensure_workspace, workspace_checkpoint::inspect};
    for running in [false, true] {
        let (mut f, request, target) = main_fixture();
        if running {
            f.e.backend.records[0]["status"] = json!("running");
            f.e.backend.records[0]["pid"] = json!(9);
            f.e.backend.records[0]["pid_created"] = proc(9, 0, "codex.exe")["created"].clone();
            f.e.backend.ps.insert(9, proc(9, 0, "codex.exe"));
        }
        let before = f.get(1);
        let store = fs::read(&f.e.todo_path).unwrap();
        let jobs = f.e.backend.records.clone();
        let status = run_git(&mut SystemRunner, &target, &["status", "--porcelain"]).unwrap();
        let report = inspect(&mut f.e, &mut SystemRunner, &request).unwrap();
        assert_eq!(report["eligible"], true, "{report}");
        assert_eq!(report["saved"], false);
        assert_eq!(fs::read(&f.e.todo_path).unwrap(), store);
        assert_eq!(checkpoint(&mut f.e, &mut SystemRunner, &request).unwrap()["saved"], true);
        let saved = f.get(1);
        ensure_workspace(&mut SystemRunner, f.repo.to_str().unwrap(), &saved, &jobs).unwrap();
        assert_eq!(checkpoint(&mut f.e, &mut SystemRunner, &request).unwrap()["saved"], false);
        assert_eq!(f.get(1), saved);
        let mut after = saved;
        after["scheduler"]["branch"] = before["scheduler"]["branch"].clone();
        remove(&mut after["scheduler"], "workspace_checkpoint");
        after["updated"] = before["updated"].clone();
        assert_eq!(after, before);
        assert_eq!(f.e.backend.records, jobs);
        assert_eq!(run_git(&mut SystemRunner, &target, &["status", "--porcelain"]).unwrap(), status);
        assert_eq!(run_git(&mut SystemRunner, &target, &["rev-parse", "HEAD"]).unwrap(), request.head);
        assert_eq!(fs::read_to_string(target.join("unfinished.txt")).unwrap(), "keep this pending work");
        assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn workspace_checkpoint_main_rejects_unbound_or_changed_assignment() {
    use cc_scheduler::workspace_checkpoint::inspect;
    for (gate, reason) in [
        ("other_todo", "CheckpointSourceOwnership"),
        ("other_job", "CheckpointSourceMissing"),
        ("historical_job", "CheckpointSourceAssignment"),
        ("related_only", "CheckpointSourceAssignment"),
        ("missing_owner", "CheckpointSourceAssignment"),
        ("old_branch", "CheckpointSourceAssignment"),
        ("owner_tree", "CheckpointSourceAssignment"),
        ("owner_common", "CheckpointSourceAssignment"),
        ("owner_repository", "CheckpointSourceAssignment"),
        ("source_cwd", "CheckpointSourceCwd"),
        ("expected", "CheckpointExpectedBranch"),
        ("arbitrary_expected", "InputRejected"),
        ("other_todo_expected", "InputRejected"),
        ("arbitrary_destination", "InputRejected"),
        ("main_destination", "InputRejected"),
        ("other_todo_destination", "InputRejected"),
        ("actual_branch", "BranchMismatch"),
        ("head", "CheckpointHeadMismatch"),
        ("scope", "ClientResource"),
        ("worktree_scope", "ClientResource"),
        ("other_repo", "RepositoryMismatch"),
        ("competing_todo", "CheckpointCompetingWorker"),
        ("competing_cwd", "CheckpointCompetingWorker"),
    ] {
        let (mut f, mut request, target) = main_fixture();
        match gate {
            "other_todo" => f.e.backend.records[0]["todo_id"] = json!(2),
            "other_job" => request.job = "missing".into(),
            "historical_job" => { f.e.edit(&json!(1), |r| r["scheduler"]["job"] = json!("new-owner")).unwrap(); }
            "related_only" => {
                f.e.backend.records[0]["todo_id"] = json!(2);
                f.e.backend.records[0]["related_todo_ids"] = json!([1]);
            }
            "missing_owner" => f.e.backend.records[0]["work_owner"] = Value::Null,
            "old_branch" => f.e.backend.records[0]["work_owner"]["branch"] = json!("develop"),
            "owner_tree" => f.e.backend.records[0]["work_owner"]["worktree"] = json!(f.repo),
            "owner_common" => f.e.backend.records[0]["work_owner"]["git_common"] = json!(f.repo),
            "owner_repository" => f.e.backend.records[0]["work_owner"]["repository"] = json!("example/other"),
            "source_cwd" => f.e.backend.records[0]["cwd"] = json!(f.repo),
            "expected" => { f.e.edit(&json!(1), |r| r["scheduler"]["branch"] = json!("scheduler/todo-1-new")).unwrap(); }
            "arbitrary_expected" => request.expected_branch = "develop".into(),
            "other_todo_expected" => request.expected_branch = "scheduler/todo-2-old".into(),
            "arbitrary_destination" => request.branch = "feature/work".into(),
            "main_destination" => request.branch = "main".into(),
            "other_todo_destination" => request.branch = "scheduler/todo-2-work".into(),
            "actual_branch" => request.branch = "scheduler/todo-1-missing".into(),
            "head" => request.head = "f".repeat(40),
            "scope" => f.e.backend.repair_allowed = false,
            "worktree_scope" => f.e.backend.repair_denied = Some(target.to_str().unwrap().into()),
            "other_repo" => {
                let repo = f.temp.path().join("separate");
                fs::create_dir(&repo).unwrap();
                run_git(&mut SystemRunner, &repo, &["init", "-b", "main"]).unwrap();
                let mut db = f.e.todo().unwrap();
                db["projects"]["other"] = json!(repo);
                write_json(&f.e.todo_path, &db, 2).unwrap();
            }
            "competing_todo" | "competing_cwd" => {
                let mut other = job("other", 9, "running");
                other["todo_id"] = json!(if gate == "competing_todo" { 1 } else { 2 });
                other["cwd"] = json!(if gate == "competing_cwd" { &target } else { &f.repo });
                f.e.backend.records.push(other);
                f.e.backend.ps.insert(9, proc(9, 0, "codex.exe"));
            }
            _ => unreachable!(),
        }
        let before = fs::read(&f.e.todo_path).unwrap();
        let jobs = f.e.backend.records.clone();
        let report = inspect(&mut f.e, &mut SystemRunner, &request).unwrap();
        assert_eq!(report["reason"], reason, "{gate}: {report}");
        assert_eq!(report["eligible"], false, "{gate}");
        assert_eq!(checkpoint(&mut f.e, &mut SystemRunner, &request).unwrap_err().0, reason, "{gate}");
        assert_eq!(fs::read(&f.e.todo_path).unwrap(), before, "{gate}");
        assert_eq!(f.e.backend.records, jobs, "{gate}");
        assert!(f.e.backend.launched.is_empty(), "{gate}");
    }
}

#[test]
fn workspace_checkpoint_main_preserves_concurrent_row_and_project_changes() {
    for repeated in [false, true] {
        for field in ["note", "wait_on", "status", "job", "branch", "project"] {
            let (mut f, request, _) = main_fixture();
            if repeated { checkpoint(&mut f.e, &mut SystemRunner, &request).unwrap(); }
            let mut changed = f.e.todo().unwrap();
            let row = &mut changed["items"][0];
            match field {
                "note" => row["note"] = json!("new owner answer"),
                "wait_on" => row["wait_on"] = json!([2, 3]),
                "status" => row["status"] = json!("block"),
                "job" => row["scheduler"]["job"] = json!("next-owner"),
                "branch" => row["scheduler"]["branch"] = json!("scheduler/todo-1-next"),
                "project" => changed["projects"]["other"] = json!(f.temp.path().join("other-project")),
                _ => unreachable!(),
            }
            f.e.backend.repair_edit = Some((f.e.todo_path.clone(), changed.clone()));
            assert_eq!(checkpoint(&mut f.e, &mut SystemRunner, &request).unwrap_err().0, "CheckpointRowChanged", "{repeated}/{field}");
            assert_eq!(f.e.todo().unwrap(), changed);
            assert!(f.e.backend.launched.is_empty());
        }
    }
}

fn waiting_fixture() -> (
    Fixture,
    cc_scheduler::workspace_checkpoint::Request,
    PathBuf,
) {
    let (mut f, request, target) = checkpoint_fixture();
    f.e.edit(&json!(1), |row| row["scheduler"]["job"] = json!("owned"))
        .unwrap();
    let identity = json!({"repository":"example/repair", "git_common":f.repo.join(".git"),
        "worktree":target,"branch":"scheduler/todo-1-reserved"});
    let source = &mut f.e.backend.records[0];
    source["status"] = json!("running");
    source["pid"] = json!(9);
    source["pid_created"] = proc(9, 0, "codex.exe")["created"].clone();
    source["work_owner"] = identity.clone();
    f.e.backend.ps.insert(9, proc(9, 0, "codex.exe"));
    f.e.backend
        .records
        .push(json!({"id":"waiter","status":"queued","cwd":target,
        "pid":10,"pid_created":proc(10,0,"codex_jobs.exe")["created"],"codex_pid":null,"codex_pid_created":null,
        "created_at":T-1200.,"work_owner":identity,
        "queue_wait_reason":{"code":"work_owner","owner_job_id":"owned","owner_todo_id":1}}));
    f.e.backend.ps.insert(10, proc(10, 0, "codex_jobs.exe"));
    f.e.backend.ps.insert(11, proc(11, 10, "conhost.exe"));
    (f, request, target)
}

#[test]
fn workspace_checkpoint_saves_with_exact_inactive_owner_waiter() {
    for (todo_id, evidence) in [(Value::Null, false), (json!(1), true)] {
        let (mut f, request, target) = waiting_fixture();
        f.e.backend.records[1]["todo_id"] = todo_id;
        // A fresh, birth-verified supervisor is normal, even during registration grace.
        f.e.backend.records[1]["created_at"] = json!(T - 5.);
        if evidence {
            f.e.backend.ps.get_mut(&11).unwrap()["name"] = json!("ConHost.EXE");
            f.e.backend.records[1]["work_wait_evidence"] = json!({
                "owner_job_id":"owned", "owner":cc_job_policy::work_owner::owner_evidence(&f.e.backend.records[0]),
                "candidate_identity":f.e.backend.records[1]["work_owner"]});
        }
        let before = f.get(1);
        let jobs = f.e.backend.records.clone();
        let git_before = run_git(&mut SystemRunner, &target, &["status", "--porcelain"]).unwrap();
        let result = checkpoint(&mut f.e, &mut SystemRunner, &request);
        assert!(result.is_ok(), "exact owner waiter rejected: {result:?}");
        assert_eq!(result.unwrap()["saved"], true);
        let mut after = f.get(1);
        assert_eq!(after["scheduler"]["branch"], request.branch);
        after["scheduler"]["branch"] = before["scheduler"]["branch"].clone();
        remove(&mut after["scheduler"], "workspace_checkpoint");
        after["updated"] = before["updated"].clone();
        assert_eq!(after, before);
        assert_eq!(f.e.backend.records, jobs);
        assert_eq!(
            run_git(&mut SystemRunner, &target, &["status", "--porcelain"]).unwrap(),
            git_before
        );
        assert_eq!(
            fs::read_to_string(target.join("unfinished.txt")).unwrap(),
            "keep this pending work"
        );
        assert!(f.e.backend.launched.is_empty());
    }
}

/// Use the real Windows snapshot and real hidden-console topology, with only
/// the task store and admission backend isolated in the existing fixture.
#[test]
fn workspace_checkpoint_accepts_native_hidden_console_waiter() {
    use std::{
        os::windows::process::CommandExt,
        process::{Child, Command, Stdio},
        time::Instant,
    };
    struct Probe(Child);
    impl Drop for Probe {
        fn drop(&mut self) {
            let _ = self.0.kill();
            let _ = self.0.wait();
        }
    }
    let (mut f, request, target) = waiting_fixture();
    let program = PathBuf::from(std::env::var_os("SystemRoot").unwrap()).join("System32/ping.exe");
    let mut probe = Probe(
        Command::new(program)
            .args(["-n", "30", "127.0.0.1"])
            .current_dir(f.temp.path())
            .creation_flags(
                NO_WINDOW | windows_sys::Win32::System::Threading::CREATE_NEW_PROCESS_GROUP,
            )
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap(),
    );
    let pid = probe.0.id();
    let deadline = Instant::now() + Duration::from_secs(4);
    let (snapshot, console) = loop {
        let ps = cc_scheduler::native::process_snapshot().unwrap();
        if let Some(console) = ps
            .values()
            .find(|p| {
                p["ppid"] == pid
                    && s(&p["name"]).eq_ignore_ascii_case("conhost.exe")
                    && p["created"].is_u64()
            })
            .cloned()
        {
            break (ps, console);
        }
        assert!(
            Instant::now() < deadline,
            "hidden console host was not observed"
        );
        std::thread::sleep(Duration::from_millis(20));
    };
    let birth = snapshot[&pid]["created"].as_u64().unwrap();
    let console_id = console["pid"].as_u64().unwrap() as u32;
    assert!(console["created"].as_u64().unwrap() >= birth);
    f.e.backend.records[1]["pid"] = json!(pid);
    f.e.backend.records[1]["pid_created"] = json!(birth);
    f.e.backend.ps.retain(|id, _| *id == 9);
    f.e.backend
        .ps
        .extend(snapshot.into_iter().filter(|(id, p)| {
            *id == pid || *id == console_id || p["ppid"] == pid || p["ppid"] == console_id
        }));
    let before = f.get(1);
    assert!(
        probe.0.try_wait().unwrap().is_none(),
        "owned console probe exited before inspection"
    );
    let inspected =
        cc_scheduler::workspace_checkpoint::inspect(&mut f.e, &mut SystemRunner, &request).unwrap();
    assert_eq!(inspected["eligible"], true, "{inspected}");
    assert_eq!(inspected["saved"], false);
    assert_eq!(f.get(1), before);
    let saved = checkpoint(&mut f.e, &mut SystemRunner, &request).unwrap();
    assert_eq!(saved["saved"], true);
    assert_eq!(saved["verified_owner_waiters"], 1);
    assert!(
        probe.0.try_wait().unwrap().is_none(),
        "owned console probe exited before checkpoint completed"
    );
    assert_eq!(
        fs::read_to_string(target.join("unfinished.txt")).unwrap(),
        "keep this pending work"
    );
    drop(probe);
    let deadline = Instant::now() + Duration::from_secs(4);
    loop {
        let ps = cc_scheduler::native::process_snapshot().unwrap();
        let remains = ps
            .get(&console_id)
            .is_some_and(|p| p["created"].is_null() || p["created"] == console["created"]);
        if !remains {
            break;
        }
        assert!(Instant::now() < deadline, "owned console host did not exit");
        std::thread::sleep(Duration::from_millis(20));
    }
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn workspace_checkpoint_rejects_ambiguous_or_active_waiters() {
    use cc_scheduler::workspace_checkpoint::inspect;
    for (gate, reason) in [
        ("missing_reason", "CheckpointWaitUnverified"),
        ("wrong_owner", "CheckpointWaitUnverified"),
        ("wrong_owner_todo", "CheckpointWaitUnverified"),
        ("other_todo", "CheckpointWaitUnverified"),
        ("other_cwd", "CheckpointWaitIdentity"),
        ("missing_identity", "CheckpointWaitIdentity"),
        ("wrong_repository", "CheckpointWaitIdentity"),
        ("wrong_common", "CheckpointWaitIdentity"),
        ("wrong_tree", "CheckpointWaitIdentity"),
        ("starting", "CheckpointCompetingWorker"),
        ("running", "CheckpointCompetingWorker"),
        ("surviving_process", "CheckpointCompetingWorker"),
        ("unknown_worker_birth", "CheckpointCompetingWorker"),
        ("unknown_birth", "CheckpointCompetingWorker"),
        ("wrong_birth", "CheckpointCompetingWorker"),
        ("supervisor_child", "CheckpointCompetingWorker"),
        ("unknown_child_birth", "CheckpointCompetingWorker"),
        ("console_descendant", "CheckpointCompetingWorker"),
        ("unknown_console_descendant", "CheckpointCompetingWorker"),
        ("unknown_console_birth", "CheckpointCompetingWorker"),
        ("bad_console_name", "CheckpointCompetingWorker"),
        ("malformed_console_pid", "CheckpointCompetingWorker"),
        ("execution_named_console", "CheckpointCompetingWorker"),
        ("malformed_pid", "CheckpointCompetingWorker"),
        ("malformed_flag", "CheckpointCompetingWorker"),
        ("owner_live", "CheckpointCompetingWorker"),
        ("restart", "CheckpointCompetingWorker"),
        ("reboot", "CheckpointCompetingWorker"),
        ("cancel", "CheckpointCompetingWorker"),
        ("unreadable", "CheckpointCompetingWorker"),
        ("recent_without_monitor", "CheckpointCompetingWorker"),
        ("conflicting_evidence", "CheckpointWaitUnverified"),
        ("changed_candidate", "CheckpointWaitUnverified"),
        ("source_not_current", "CheckpointSourceNotCurrent"),
        ("source_dead", "CheckpointWaitUnverified"),
    ] {
        let (mut f, request, _) = waiting_fixture();
        let owner_evidence = cc_job_policy::work_owner::owner_evidence(&f.e.backend.records[0]);
        let waiter = &mut f.e.backend.records[1];
        match gate {
            "missing_reason" => remove(waiter, "queue_wait_reason"),
            "wrong_owner" => waiter["queue_wait_reason"]["owner_job_id"] = json!("someone-else"),
            "wrong_owner_todo" => waiter["queue_wait_reason"]["owner_todo_id"] = json!(2),
            "other_todo" => waiter["todo_id"] = json!(2),
            "other_cwd" => {
                waiter["cwd"] = json!(f.repo);
                waiter["todo_id"] = json!(1);
            }
            "missing_identity" => remove(waiter, "work_owner"),
            "wrong_repository" => waiter["work_owner"]["repository"] = json!("example/other"),
            "wrong_common" => waiter["work_owner"]["git_common"] = json!(f.temp.path()),
            "wrong_tree" => waiter["work_owner"]["worktree"] = json!(f.repo),
            "starting" | "running" => waiter["status"] = json!(gate),
            "surviving_process" | "unknown_worker_birth" => {
                waiter["codex_pid"] = json!(11);
                waiter["codex_pid_created"] = proc(11, 10, "codex.exe")["created"].clone();
                let mut process = proc(11, 10, "codex.exe");
                if gate == "unknown_worker_birth" {
                    process["created"] = Value::Null;
                }
                f.e.backend.ps.insert(11, process);
            }
            "unknown_birth" => f.e.backend.ps.get_mut(&10).unwrap()["created"] = Value::Null,
            "wrong_birth" => waiter["pid_created"] = json!(1),
            "supervisor_child" | "unknown_child_birth" => {
                let mut child = proc(11, 10, "worker.exe");
                if gate == "unknown_child_birth" {
                    child["created"] = Value::Null;
                }
                f.e.backend.ps.insert(11, child);
            }
            "console_descendant" | "unknown_console_descendant" => {
                let mut descendant = proc(12, 11, "worker.exe");
                if gate == "unknown_console_descendant" {
                    descendant["created"] = Value::Null;
                }
                f.e.backend.ps.insert(12, descendant);
            }
            "unknown_console_birth" => {
                f.e.backend.ps.get_mut(&11).unwrap()["created"] = Value::Null
            }
            "bad_console_name" => {
                f.e.backend.ps.get_mut(&11).unwrap()["name"] = json!("OpenConsole.exe")
            }
            "malformed_console_pid" => f.e.backend.ps.get_mut(&11).unwrap()["pid"] = Value::Null,
            "execution_named_console" => waiter["codex_pid"] = json!(11),
            "malformed_pid" => waiter["pid"] = json!("unknown"),
            "malformed_flag" => waiter["queue_restart_pending"] = json!("unknown"),
            "owner_live" => waiter["work_owner_live"] = json!(true),
            "restart" => waiter["queue_restart_pending"] = json!(true),
            "reboot" => waiter["reboot_launch_pending"] = json!(true),
            "cancel" => waiter["cancel_requested"] = json!(true),
            "unreadable" => waiter["queue_unreadable"] = json!(true),
            "recent_without_monitor" => {
                remove(waiter, "pid");
                f.e.backend.ps.remove(&10);
                waiter["created_at"] = json!(T - 5.);
            }
            "conflicting_evidence" => {
                waiter["work_wait_evidence"] = json!({"owner_job_id":"another"})
            }
            "changed_candidate" => {
                waiter["work_wait_evidence"] = json!({"owner_job_id":"owned",
                    "owner":owner_evidence,
                    "candidate_identity":{"worktree":"another"}});
            }
            "source_not_current" => {
                f.e.edit(&json!(1), |row| row["scheduler"]["job"] = json!("another"))
                    .unwrap();
            }
            "source_dead" => {
                f.e.backend.records[0]["status"] = json!("succeeded");
                f.e.backend.ps.clear();
            }
            _ => unreachable!(),
        }
        let before = f.get(1);
        let report = inspect(&mut f.e, &mut SystemRunner, &request).unwrap();
        assert_eq!(report["eligible"], false, "{gate}");
        assert_eq!(report["reason"], reason, "{gate}");
        assert_eq!(f.get(1), before, "{gate}");
        assert!(f.e.backend.launched.is_empty(), "{gate}");
    }
}

#[test]
fn workspace_checkpoint_inspection_reports_comparisons_without_saving() {
    use cc_scheduler::workspace_checkpoint::inspect;
    let (mut f, mut request, _) = waiting_fixture();
    let before = fs::read(&f.e.todo_path).unwrap();
    let report = inspect(&mut f.e, &mut SystemRunner, &request).unwrap();
    assert_eq!(report["eligible"], true);
    assert_eq!(report["saved"], false);
    assert_eq!(report["verified_owner_waiters"], 1);
    assert_eq!(report["checks"]["queued_wait_owner_matches"], true);
    assert_eq!(fs::read(&f.e.todo_path).unwrap(), before);
    request.expected_branch = "scheduler/todo-1-stale".into();
    let report = inspect(&mut f.e, &mut SystemRunner, &request).unwrap();
    assert_eq!(report["eligible"], false);
    assert_eq!(report["reason"], "CheckpointExpectedBranch");
    assert_eq!(report["checks"]["expected_branch_matches"], false);
    request.expected_branch = "scheduler/todo-1-reserved".into();
    f.e.backend.records[1]["status"] = json!("running");
    let report = inspect(&mut f.e, &mut SystemRunner, &request).unwrap();
    assert_eq!(report["eligible"], false);
    assert_eq!(report["reason"], "CheckpointCompetingWorker");
    assert_eq!(report["checks"]["competing_worker_inactive"], false);
    let serialized = report.to_string();
    for private in [
        s(&f.get(1)["note"]),
        s(&f.get(1)["spec"]),
        "example/repair",
        "\"waiter\"",
        "\"owned\"",
    ] {
        assert!(!serialized.contains(private), "private source escaped");
    }
    assert_eq!(fs::read(&f.e.todo_path).unwrap(), before);
    assert!(f.e.backend.launched.is_empty());
}

struct GateProbe {
    path: PathBuf,
    observed: bool,
    outside: bool,
}
impl Runner for GateProbe {
    fn run(&mut self, invocation: &Invocation) -> Result<Output> {
        let gate = ByteLock::acquire(&self.path, Duration::ZERO)?;
        let final_state = invocation
            .args
            .iter()
            .any(|arg| arg == "branch" || arg == "HEAD");
        assert_eq!(gate.is_none(), final_state);
        self.observed |= final_state;
        self.outside |= !final_state;
        drop(gate);
        SystemRunner.run(invocation)
    }
}

#[test]
fn workspace_checkpoint_holds_the_ordinary_admission_gate() {
    let (mut f, request, _) = waiting_fixture();
    let path =
        f.e.root
            .parent()
            .unwrap()
            .join("codex-jobs/jobs/.queue.lock");
    fs::create_dir_all(path.parent().unwrap()).unwrap();
    let available = ByteLock::acquire(&path, Duration::ZERO).unwrap().unwrap();
    drop(available);
    let mut runner = GateProbe {
        path: path.clone(),
        observed: false,
        outside: false,
    };
    assert_eq!(
        checkpoint(&mut f.e, &mut runner, &request).unwrap()["saved"],
        true
    );
    assert!(runner.observed);
    assert!(runner.outside);
    assert!(ByteLock::acquire(&path, Duration::ZERO).unwrap().is_some());
}

#[test]
fn workspace_checkpoint_rejects_intervening_notes_waits_and_stops_even_on_repeat() {
    for repeated in [false, true] {
        for field in ["note", "wait_on", "status", "job", "project"] {
            let (mut f, request, _) = waiting_fixture();
            if repeated {
                checkpoint(&mut f.e, &mut SystemRunner, &request).unwrap();
            }
            let mut changed = f.e.todo().unwrap();
            let row = &mut changed["items"][0];
            match field {
                "note" => row["note"] = json!("new owner answer"),
                "wait_on" => row["wait_on"] = json!([2, 3]),
                "status" => row["status"] = json!("block"),
                "job" => row["scheduler"]["job"] = json!("next-owner"),
                "project" => {
                    changed["projects"]["other"] = json!(f.temp.path().join("other-project"))
                }
                _ => unreachable!(),
            }
            f.e.backend.repair_edit = Some((f.e.todo_path.clone(), changed.clone()));
            assert_eq!(
                checkpoint(&mut f.e, &mut SystemRunner, &request)
                    .unwrap_err()
                    .0,
                "CheckpointRowChanged",
                "{repeated}/{field}"
            );
            assert_eq!(f.e.todo().unwrap(), changed, "{repeated}/{field}");
            assert!(f.e.backend.launched.is_empty());
        }
    }
}
