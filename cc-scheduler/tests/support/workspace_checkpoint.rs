use super::*;
use cc_scheduler::workspace_checkpoint::checkpoint;

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
        "created_at":T-1200.,"work_owner":identity,
        "queue_wait_reason":{"code":"work_owner","owner_job_id":"owned","owner_todo_id":1}}));
    (f, request, target)
}

#[test]
fn workspace_checkpoint_saves_with_exact_inactive_owner_waiter() {
    for todo_id in [Value::Null, json!(1)] {
        let (mut f, request, target) = waiting_fixture();
        f.e.backend.records[1]["todo_id"] = todo_id;
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

#[test]
fn workspace_checkpoint_rejects_ambiguous_or_active_waiters() {
    for gate in [
        "missing_reason",
        "wrong_owner",
        "wrong_owner_todo",
        "other_todo",
        "other_cwd",
        "missing_identity",
        "wrong_repository",
        "wrong_common",
        "wrong_tree",
        "starting",
        "running",
        "surviving_process",
        "unknown_birth",
        "owner_live",
        "restart",
        "reboot",
        "cancel",
        "unreadable",
        "recent",
        "conflicting_evidence",
        "source_not_current",
        "source_dead",
    ] {
        let (mut f, request, _) = waiting_fixture();
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
            "surviving_process" | "unknown_birth" => {
                waiter["pid"] = json!(10);
                waiter["pid_created"] = proc(10, 0, "codex.exe")["created"].clone();
                let mut process = proc(10, 0, "codex.exe");
                if gate == "unknown_birth" {
                    process["created"] = Value::Null;
                }
                f.e.backend.ps.insert(10, process);
            }
            "owner_live" => waiter["work_owner_live"] = json!(true),
            "restart" => waiter["queue_restart_pending"] = json!(true),
            "reboot" => waiter["reboot_launch_pending"] = json!(true),
            "cancel" => waiter["cancel_requested"] = json!(true),
            "unreadable" => waiter["queue_unreadable"] = json!(true),
            "recent" => waiter["created_at"] = json!(T - 5.),
            "conflicting_evidence" => {
                waiter["work_wait_evidence"] = json!({"owner_job_id":"another"})
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
        assert!(
            checkpoint(&mut f.e, &mut SystemRunner, &request).is_err(),
            "{gate}"
        );
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
}
impl Runner for GateProbe {
    fn run(&mut self, invocation: &Invocation) -> Result<Output> {
        assert!(ByteLock::acquire(&self.path, Duration::ZERO)?.is_none());
        self.observed = true;
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
    };
    assert_eq!(
        checkpoint(&mut f.e, &mut runner, &request).unwrap()["saved"],
        true
    );
    assert!(runner.observed);
    assert!(ByteLock::acquire(&path, Duration::ZERO).unwrap().is_some());
}

#[test]
fn workspace_checkpoint_rejects_intervening_notes_waits_and_stops_even_on_repeat() {
    for repeated in [false, true] {
        for field in ["note", "wait_on", "status", "job"] {
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
                _ => unreachable!(),
            }
            f.e.backend.repair_edit = Some((f.e.todo_path.clone(), changed.clone()));
            assert!(
                checkpoint(&mut f.e, &mut SystemRunner, &request).is_err(),
                "{repeated}/{field}"
            );
            assert_eq!(f.e.todo().unwrap(), changed, "{repeated}/{field}");
            assert!(f.e.backend.launched.is_empty());
        }
    }
}
