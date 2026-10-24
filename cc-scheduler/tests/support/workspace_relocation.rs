use super::*;
use cc_scheduler::workspace_relocation::{relocate, Request};

fn fixture() -> (Fixture, Request, PathBuf) {
    let (mut f, workspace, _) = reserved_branch_fixture();
    let target = PathBuf::from(s(&workspace["scheduler"]["worktree"]));
    let mut runner = SystemRunner;
    cc_scheduler::preparation::ensure_workspace(
        &mut runner,
        f.repo.to_str().unwrap(),
        &workspace,
        &[],
    )
    .unwrap();
    run_git(
        &mut runner,
        &f.repo,
        &[
            "remote",
            "add",
            "origin",
            "https://github.com/AutoDevJapan/cc-pc-tools-rs.git",
        ],
    )
    .unwrap();
    let shared = target.parent().unwrap().join("todo-2-shared");
    run_git(
        &mut runner,
        &f.repo,
        &[
            "worktree",
            "add",
            "-b",
            "scheduler/todo-2-current",
            shared.to_str().unwrap(),
        ],
    )
    .unwrap();
    let source_owner = cc_job_policy::work_owner::checkout(shared.to_str().unwrap());
    let mut old_owner = source_owner.clone();
    old_owner["branch"] = json!("scheduler/todo-2-original");
    let reference = shared.join("continuation.md");
    let spec = format!(
        "ToDo1\nContinue original work.\n{}\n{}\n",
        target.to_str().unwrap().replace('\\', "/"),
        s(&workspace["scheduler"]["branch"])
    );
    fs::write(&reference, &spec).unwrap();
    let spec_root = f.e.todo_path.parent().unwrap().join("specs");
    fs::create_dir_all(&spec_root).unwrap();
    let saved = spec_root.join(format!("1-{}.md", "a".repeat(32)));
    fs::write(&saved, &spec).unwrap();
    let mut row = item(1, "cc-pc-tools-rs", "todo", &["cargo"]);
    row["scheduler"] = json!({"worktree":shared,"branch":"scheduler/todo-2-original","last_job":"finished","phase":"retry","resume":true});
    row["jobs"] = json!(["finished"]);
    row["spec_history"] = json!([{"spec":"original-preserved"}]);
    row["note"] = json!("keep original answers and remaining work");
    row["processed_results"] = json!({"finished":{"reason":"succeeded","generation":1}});
    f.put(row);
    f.e.edit(&json!(1), |row| row["spec"] = json!(saved))
        .unwrap();
    let mut owner_row = item(2, "cc-pc-tools-rs", "doing", &[]);
    owner_row["scheduler"] = json!({"worktree":shared,"branch":"scheduler/todo-2-current","job":"parent","phase":"running"});
    owner_row["jobs"] = json!(["parent"]);
    f.put(owner_row);
    let mut db = f.e.todo().unwrap();
    db["projects"]["cc-pc-tools-rs"] = json!(f.repo);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    f.e.backend.repair_allowed = true;
    let mut parent = job("parent", 2, "running");
    parent["todo_id"] = json!(2);
    parent["project"] = json!("cc-pc-tools-rs");
    parent["cwd"] = json!(shared);
    parent["work_owner"] = source_owner;
    f.e.backend.records = vec![
        json!({"id":"finished","status":"succeeded","exit_code":0,"todo_id":1,"project":"cc-pc-tools-rs","cwd":shared,"work_owner":old_owner}),
        parent,
    ];
    f.e.backend.ps.insert(2, proc(2, 0, "codex.exe"));
    fs::write(target.join("pending.txt"), "preserve unfinished work").unwrap();
    let request = Request {
        id: 1,
        source_job: "finished".into(),
        shared_owner_job: "parent".into(),
        source_branch: "scheduler/todo-2-current".into(),
        source_head: run_git(&mut runner, &shared, &["rev-parse", "HEAD"]).unwrap(),
        worktree: target.to_str().unwrap().into(),
        branch: s(&workspace["scheduler"]["branch"]).into(),
        head: run_git(&mut runner, &target, &["rev-parse", "HEAD"]).unwrap(),
        spec_reference: reference.to_str().unwrap().into(),
        spec_sha256: cc_job_policy::product_retry::digest(spec.as_bytes()),
    };
    (f, request, shared)
}

#[test]
fn relocation_preserves_shared_writer_history_and_resumes_original_task_once() {
    let (mut f, request, shared) = fixture();
    let before = f.get(1);
    let parent = f.get(2);
    let shared_status = run_git(&mut SystemRunner, &shared, &["status", "--porcelain"]).unwrap();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(checked["eligible"], true, "{checked}");
    assert_eq!(f.get(1), before);
    let expected = s(&checked["state_sha256"]).to_owned();
    let saved = relocate(&mut f.e, &mut SystemRunner, &request, Some(&expected)).unwrap();
    assert_eq!(saved["saved"], true);
    assert_eq!(saved["launched"], 0);
    for key in [
        "id",
        "spec",
        "spec_history",
        "note",
        "needs",
        "jobs",
        "processed_results",
        "status",
    ] {
        assert_eq!(f.get(1)[key], before[key], "{key}");
    }
    assert_eq!(f.get(2), parent);
    assert_eq!(
        run_git(&mut SystemRunner, &shared, &["status", "--porcelain"]).unwrap(),
        shared_status
    );
    assert_eq!(
        fs::read_to_string(Path::new(&request.worktree).join("pending.txt")).unwrap(),
        "preserve unfinished work"
    );
    let repeated = relocate(&mut f.e, &mut SystemRunner, &request, Some(&expected)).unwrap();
    assert_eq!(repeated["already_recorded"], true);
    assert_eq!(repeated["saved"], false);
    let row = f.get(1);
    let recovered = cc_scheduler::preparation::recover_workspace(
        &mut SystemRunner,
        f.repo.to_str().unwrap(),
        &row,
        &f.e.backend.records,
    )
    .unwrap()
    .unwrap();
    assert_eq!(recovered["worktree"], row["scheduler"]["worktree"]);
    assert_eq!(recovered["branch"], request.branch);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn relocation_rejects_stops_dependencies_unknown_workers_and_changed_provenance() {
    let (mut f, request, _) = fixture();
    let original = f.e.todo().unwrap();
    let jobs = f.e.backend.records.clone();
    let valid = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(valid["eligible"], true);
    let expected = s(&valid["state_sha256"]).to_owned();
    for gate in [
        "stop",
        "dependency",
        "block",
        "cancel",
        "project",
        "job",
        "source_branch",
        "source_owner",
        "source_common",
        "source_result",
        "parent",
        "parent_branch",
        "missing_history",
        "unknown_task_worker",
        "unknown_target_worker",
        "alive_terminal",
        "scope",
        "reference_scope",
        "branch",
        "head",
        "shared_head",
        "spec_path",
        "spec_hash",
        "phase",
        "shared_worker",
        "target_claim",
        "reservation",
        "empty_old_branch",
        "console",
    ] {
        write_json(&f.e.todo_path, &original, 1).unwrap();
        f.e.backend.records = jobs.clone();
        f.e.backend.repair_allowed = true;
        f.e.backend.repair_denied = None;
        let mut req = request.clone();
        match gate {
            "stop" => {
                f.e.edit(&json!(1), |r| r["owner_stopped"] = json!(true))
                    .unwrap();
            }
            "dependency" => {
                f.e.edit(&json!(1), |r| r["wait_on"] = json!([99])).unwrap();
            }
            "block" => {
                f.e.edit(&json!(1), |r| r["status"] = json!("block"))
                    .unwrap();
            }
            "cancel" => {
                f.e.edit(&json!(1), |r| {
                    r["scheduler"]["pending_cancel"] = json!(true)
                })
                .unwrap();
            }
            "project" => {
                f.e.edit(&json!(1), |r| r["project"] = json!("other"))
                    .unwrap();
            }
            "job" => req.source_job = "unknown".into(),
            "source_branch" => {
                f.e.backend.records[0]["work_owner"]["branch"] = json!("scheduler/todo-3-foreign")
            }
            "source_owner" => f.e.backend.records[0]["todo_id"] = json!(3),
            "source_common" => {
                f.e.backend.records[0]["work_owner"]["git_common"] = json!("unknown")
            }
            "source_result" => f.e.backend.records[0]["exit_code"] = json!(1),
            "parent" => req.shared_owner_job = "unknown".into(),
            "parent_branch" => {
                f.e.backend.records[1]["work_owner"]["branch"] = json!("scheduler/todo-2-stale")
            }
            "missing_history" => {
                f.e.edit(&json!(1), |r| r["jobs"] = json!(["finished", "missing"]))
                    .unwrap();
            }
            "unknown_task_worker" | "unknown_target_worker" => {
                f.e.backend.records.push(json!({"id":"unverified","status":"unknown","todo_id":if gate == "unknown_task_worker" {1} else {3},"cwd":if gate == "unknown_task_worker" {f.repo.to_str().unwrap()} else {&request.worktree}}));
            }
            "alive_terminal" => {
                f.e.backend.records[0]["pid"] = json!(2);
                f.e.backend.records[0]["pid_created"] = f.e.backend.ps[&2]["created"].clone();
            }
            "scope" => f.e.backend.repair_allowed = false,
            "reference_scope" => f.e.backend.repair_denied = Some(request.spec_reference.clone()),
            "branch" => req.branch = "scheduler/todo-1-other".into(),
            "head" => req.head = "a".repeat(40),
            "shared_head" => req.source_head = "a".repeat(40),
            "spec_path" => {
                f.e.edit(&json!(1), |r| r["spec"] = json!(request.spec_reference))
                    .unwrap();
            }
            "spec_hash" => req.spec_sha256 = "a".repeat(64),
            "phase" => {
                f.e.edit(&json!(1), |r| r["scheduler"]["phase"] = json!("prepared"))
                    .unwrap();
            }
            "shared_worker" => {
                f.e.backend.records.push(json!({"id":"unknown-shared","status":"unknown","todo_id":3,"cwd":jobs[0]["cwd"]}));
            }
            "target_claim" => {
                f.e.backend.records.push(json!({"id":"unknown-target","status":"unknown","todo_id":3,"cwd":f.repo,"work_owner":cc_job_policy::work_owner::checkout(&request.worktree)}));
            }
            "reservation" => {
                f.e.edit(&json!(1), |r| r["scheduler"]["key"] = json!("a".repeat(32)))
                    .unwrap();
                f.e.backend.records[0]["launch_key"] = json!("a".repeat(32));
            }
            "empty_old_branch" => {
                f.e.edit(&json!(1), |r| r["scheduler"]["branch"] = json!(""))
                    .unwrap();
                f.e.backend.records[0]["work_owner"]["branch"] = json!("");
            }
            "console" => {
                f.e.edit(&json!(1), |r| {
                    r["scheduler"]["key"] = json!("a".repeat(32));
                    r["scheduler"]["console_request"] =
                        json!({"key":"intake","prompt":"preserve original request"});
                    r["scheduler"]["console_receipts"] =
                        json!({"intake":{"launch_key":"a".repeat(32)}});
                })
                .unwrap();
            }
            _ => unreachable!(),
        }
        let before = fs::read(&f.e.todo_path).unwrap();
        let checked = relocate(&mut f.e, &mut SystemRunner, &req, None).unwrap();
        assert_eq!(checked["eligible"], false, "{gate}: {checked}");
        let reason = match gate {
            "stop" | "dependency" | "block" | "cancel" | "project" | "phase" => {
                "RelocationTaskHeld"
            }
            "job" => "RelocationSourceMissing",
            "source_owner" | "source_result" | "alive_terminal" => "RelocationSourceOwnership",
            "source_branch" | "empty_old_branch" => "RelocationWorkspaceIdentity",
            "source_common" => "RelocationRepositoryIdentity",
            "parent" => "RelocationSharedOwnerMissing",
            "parent_branch" => "RelocationSharedOwnerIdentity",
            "missing_history" => "RelocationWorkerUnknown",
            "unknown_task_worker" | "unknown_target_worker" | "target_claim" => {
                "RelocationCompetingWorker"
            }
            "shared_worker" => "RelocationSharedOwnerUnknown",
            "scope" | "reference_scope" => "ClientResource",
            "branch" | "spec_hash" => "RelocationSpecMismatch",
            "head" | "shared_head" => "RelocationGitChanged",
            "spec_path" => "RelocationSpecPath",
            "reservation" => "RelocationReservationOwned",
            "console" => "RelocationConsoleRequest",
            _ => unreachable!(),
        };
        assert_eq!(checked["reason"], reason, "{gate}");
        assert_eq!(
            relocate(&mut f.e, &mut SystemRunner, &req, Some(&expected))
                .unwrap_err()
                .0,
            reason,
            "{gate}"
        );
        assert_eq!(fs::read(&f.e.todo_path).unwrap(), before, "{gate}");
        assert!(f.e.backend.launched.is_empty(), "{gate}");
        for private in [
            request.worktree.as_str(),
            request.source_job.as_str(),
            s(&f.get(1)["note"]),
        ] {
            assert!(
                !checked.to_string().contains(private),
                "private output: {gate}"
            );
        }
    }
}

#[test]
fn relocation_scope_checks_leave_admission_free_and_final_cas_keeps_intervening_edits() {
    struct LastReadEdit {
        path: PathBuf,
        changed: Value,
        status_reads: usize,
        fired: bool,
    }
    impl Runner for LastReadEdit {
        fn run(&mut self, call: &Invocation) -> Result<Output> {
            let output = SystemRunner.run(call)?;
            if call.args.iter().any(|arg| arg == "--porcelain") {
                self.status_reads += 1;
                if self.status_reads == 2 {
                    write_json(&self.path, &self.changed, 1)?;
                    self.fired = true;
                }
            }
            Ok(output)
        }
    }
    let (mut f, request, _) = fixture();
    let gate =
        f.e.root
            .parent()
            .unwrap()
            .join("codex-jobs/jobs/.queue.lock");
    f.e.backend.repair_probe_queue = Some(gate);
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(checked["eligible"], true, "{checked}");
    assert_eq!(f.e.backend.repair_queue_available, vec![true; 4]);
    for field in ["note", "needs", "status", "job", "project"] {
        let mut changed = f.e.todo().unwrap();
        match field {
            "note" => changed["items"][0]["note"] = json!("new answer after the final read"),
            "needs" => changed["items"][0]["needs"] = json!([]),
            "status" => changed["items"][0]["status"] = json!("block"),
            "job" => changed["items"][0]["scheduler"]["job"] = json!("new-assignment"),
            "project" => changed["projects"]["cc-pc-tools-rs"] = json!("changed-registration"),
            _ => unreachable!(),
        }
        let original = f.e.todo().unwrap();
        let mut runner = LastReadEdit {
            path: f.e.todo_path.clone(),
            changed: changed.clone(),
            status_reads: 0,
            fired: false,
        };
        assert_eq!(
            relocate(
                &mut f.e,
                &mut runner,
                &request,
                Some(s(&checked["state_sha256"]))
            )
            .unwrap_err()
            .0,
            "RelocationStateChanged",
            "{field}"
        );
        assert!(runner.fired, "{field}");
        assert_eq!(f.e.todo().unwrap(), changed, "{field}");
        write_json(&f.e.todo_path, &original, 1).unwrap();
    }
}

#[test]
fn relocation_apply_rejects_changes_after_inspection_and_during_scope_check() {
    let (mut f, request, _) = fixture();
    let original = f.e.todo().unwrap();
    for gate in ["note", "needs", "spec", "stop", "job", "project"] {
        write_json(&f.e.todo_path, &original, 1).unwrap();
        let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
        assert_eq!(checked["eligible"], true, "{checked}");
        let mut changed = original.clone();
        match gate {
            "note" => changed["items"][0]["note"] = json!("new owner answer"),
            "needs" => changed["items"][0]["needs"] = json!([]),
            "spec" => changed["items"][0]["spec"] = json!("changed"),
            "stop" => changed["items"][0]["owner_stopped"] = json!(true),
            "job" => changed["items"][0]["scheduler"]["job"] = json!("new-owner"),
            "project" => changed["projects"]["cc-pc-tools-rs"] = json!("changed"),
            _ => unreachable!(),
        }
        // The fake changes the real synthetic store inside the guard callback.
        f.e.backend.repair_edit = Some((f.e.todo_path.clone(), changed.clone()));
        assert!(
            relocate(
                &mut f.e,
                &mut SystemRunner,
                &request,
                Some(s(&checked["state_sha256"]))
            )
            .is_err(),
            "{gate}"
        );
        assert_eq!(f.e.todo().unwrap(), changed, "{gate}");
    }
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn relocation_rejects_changed_saved_spec_bytes_and_same_namespace_other_repository() {
    let (mut f, request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    let saved = PathBuf::from(s(&f.get(1)["spec"]));
    let original_spec = fs::read(&saved).unwrap();
    fs::write(&saved, "different instructions").unwrap();
    assert_eq!(
        relocate(
            &mut f.e,
            &mut SystemRunner,
            &request,
            Some(s(&checked["state_sha256"]))
        )
        .unwrap_err()
        .0,
        "RelocationSpecMismatch"
    );
    fs::write(&saved, original_spec).unwrap();
    let before = fs::read(&f.e.todo_path).unwrap();
    run_git(
        &mut SystemRunner,
        &f.repo,
        &[
            "remote",
            "set-url",
            "origin",
            "https://github.com/OtherOrg/cc-pc-tools-rs.git",
        ],
    )
    .unwrap();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(checked["eligible"], false);
    assert_eq!(checked["reason"], "RelocationRepositoryIdentity");
    assert_eq!(fs::read(&f.e.todo_path).unwrap(), before);
}

#[test]
fn relocation_ordinary_dispatch_uses_the_continuation_and_never_relaunches_twice() {
    let (mut f, request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    f.e.backend.real_workspaces = true;
    f.dispatch();
    assert_eq!(
        f.e.backend.launched.len(),
        1,
        "{} {:?}",
        f.e.report,
        f.e.failures
    );
    let launch = &f.e.backend.launched[0];
    assert_eq!(launch["item"]["id"], 1);
    assert_eq!(launch["item"]["scheduler"]["worktree"], request.worktree);
    assert!(s(&launch["text"]).contains("Continue original work."));
    assert!(s(&launch["text"]).contains("todo.exe history 1"));
    assert!(!s(&launch["text"]).contains("前回のジョブが止まった"));
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
}

#[test]
fn relocation_later_failed_worker_keeps_its_real_failure_and_new_workspace() {
    let (mut f, request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    f.e.edit(&json!(1), |row| {
        push(&mut row["jobs"], json!("later-failed"));
        row["scheduler"]["last_job"] = json!("later-failed");
    })
    .unwrap();
    f.e.backend.records.push(json!({"id":"later-failed","todo_id":1,"project":"cc-pc-tools-rs","status":"failed","cwd":request.worktree}));
    f.e.backend.real_workspaces = true;
    f.dispatch();
    assert_eq!(
        f.e.backend.launched.len(),
        1,
        "{} {:?}",
        f.e.report,
        f.e.failures
    );
    let launch = &f.e.backend.launched[0];
    assert_eq!(launch["item"]["scheduler"]["worktree"], request.worktree);
    assert!(s(&launch["text"]).contains("前回のジョブが止まった"));
    assert_eq!(
        launch["item"]["scheduler"]["workspace_relocation"]["request"]["source_job"],
        "finished"
    );
    assert!(!s(&launch["text"]).contains("前回の担当は正常終了しました"));
}

#[test]
fn relocation_missing_latest_source_never_falls_back_to_the_shared_writer() {
    for fallback in [false, true] {
        let (mut f, request, _) = fixture();
        let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
        relocate(
            &mut f.e,
            &mut SystemRunner,
            &request,
            Some(s(&checked["state_sha256"])),
        )
        .unwrap();
        let mut old = f.e.backend.records.remove(0);
        if fallback {
            old["id"] = json!("older");
            f.e.backend.records.push(old);
            f.e.edit(&json!(1), |row| push(&mut row["jobs"], json!("older")))
                .unwrap();
        }
        let row = f.get(1);
        let result = cc_scheduler::preparation::recover_workspace(
            &mut SystemRunner,
            f.repo.to_str().unwrap(),
            &row,
            &f.e.backend.records,
        );
        assert_eq!(
            result.unwrap_err().0,
            "RelocationSourceMissing",
            "fallback={fallback}"
        );
        assert_eq!(f.get(1), row);
    }
}

#[test]
fn relocation_unused_receipt_can_be_requalified_without_changing_its_destination() {
    let (mut f, mut request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    let original = f.get(1);
    run_git(
        &mut SystemRunner,
        Path::new(&request.worktree),
        &[
            "-c",
            "user.name=fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--allow-empty",
            "-m",
            "continue preserved work",
        ],
    )
    .unwrap();
    request.head = run_git(
        &mut SystemRunner,
        Path::new(&request.worktree),
        &["rev-parse", "HEAD"],
    )
    .unwrap();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(checked["eligible"], true, "{checked}");
    assert_eq!(checked["already_recorded"], false);
    assert_eq!(
        relocate(
            &mut f.e,
            &mut SystemRunner,
            &request,
            Some(s(&checked["state_sha256"]))
        )
        .unwrap()["saved"],
        true
    );
    let after = f.get(1);
    assert_eq!(
        after["scheduler"]["workspace_relocation_history"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert_eq!(
        after["scheduler"]["workspace_relocation"]["previous_scheduler"],
        original["scheduler"]["workspace_relocation"]["previous_scheduler"]
    );
    assert_eq!(after["note"], original["note"]);
    let recovered = cc_scheduler::preparation::recover_workspace(
        &mut SystemRunner,
        f.repo.to_str().unwrap(),
        &after,
        &f.e.backend.records,
    )
    .unwrap()
    .unwrap();
    assert_eq!(recovered["branch"], request.branch);
    f.e.edit(&json!(1), |row| {
        row["resume_generation"] = json!(99);
        row["worker_receipt"] = json!({"job":"unknown-assigned","generation":99});
    })
    .unwrap();
    let consumed = f.get(1);
    assert_eq!(
        relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap()["eligible"],
        false
    );
    assert_eq!(f.get(1), consumed);
}

#[test]
fn relocation_normal_owner_reconciliation_keeps_the_dedicated_reservation() {
    let (mut f, request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    let mut db = f.e.todo().unwrap();
    let before = db.clone();
    let changed = cc_job_policy::work_owner::reconcile_todos(&mut db, &f.e.backend.records);
    assert!(!changed.contains(&1), "{changed:?} {db}");
    assert_eq!(db, before);
    let mut contender = job("dedicated-owner", 3, "running");
    contender["todo_id"] = json!(3);
    contender["project"] = json!("cc-pc-tools-rs");
    contender["cwd"] = json!(request.worktree);
    contender["work_owner"] = cc_job_policy::work_owner::checkout(&request.worktree);
    let mut owner_row = item(3, "cc-pc-tools-rs", "doing", &[]);
    owner_row["jobs"] = json!(["dedicated-owner"]);
    push(&mut db["items"], owner_row);
    f.e.backend.records.push(contender);
    assert!(cc_job_policy::work_owner::reconcile_todos(&mut db, &f.e.backend.records).contains(&1));
    assert_eq!(db["items"][0]["wait_on"], json!([3]));
}

#[test]
fn relocation_git_checks_do_not_refresh_the_shared_index() {
    struct ReadOnlyGit {
        shared: String,
        status_calls: Vec<(String, bool)>,
    }
    impl Runner for ReadOnlyGit {
        fn run(&mut self, call: &Invocation) -> Result<Output> {
            if call.args.iter().any(|arg| arg == "status") {
                let at = call.args.iter().position(|arg| arg == "-C").unwrap();
                self.status_calls.push((
                    call.args[at + 1].clone(),
                    call.args.iter().any(|arg| arg == "--no-optional-locks"),
                ));
            }
            SystemRunner.run(call)
        }
    }
    let (mut f, request, shared) = fixture();
    let mut runner = ReadOnlyGit {
        shared: shared.to_str().unwrap().into(),
        status_calls: Vec::new(),
    };
    assert_eq!(
        relocate(&mut f.e, &mut runner, &request, None).unwrap()["eligible"],
        true
    );
    assert!(!runner.status_calls.is_empty());
    assert!(
        runner
            .status_calls
            .iter()
            .all(|(path, no_locks)| path != &runner.shared && *no_locks),
        "{:?}",
        runner.status_calls
    );
}

#[test]
fn relocation_requalifies_a_new_saved_spec_but_never_a_different_destination() {
    let (mut f, mut request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    let old_path = PathBuf::from(s(&f.get(1)["spec"]));
    let old_bytes = fs::read(&old_path).unwrap();
    let new_text = format!(
        "{}\nAdditional preserved continuation requirement.\n",
        String::from_utf8(old_bytes.clone()).unwrap()
    );
    let new_path = old_path
        .parent()
        .unwrap()
        .join(format!("1-{}.md", "b".repeat(32)));
    fs::write(&new_path, &new_text).unwrap();
    fs::write(&request.spec_reference, &new_text).unwrap();
    f.e.edit(&json!(1), |row| {
        push(&mut row["spec_history"], json!({"spec":old_path}));
        row["spec"] = json!(new_path);
    })
    .unwrap();
    request.spec_sha256 = cc_job_policy::product_retry::digest(new_text.as_bytes());
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(checked["eligible"], true, "{checked}");
    assert_eq!(
        relocate(
            &mut f.e,
            &mut SystemRunner,
            &request,
            Some(s(&checked["state_sha256"]))
        )
        .unwrap()["saved"],
        true
    );
    assert_eq!(fs::read(&old_path).unwrap(), old_bytes);
    assert_eq!(f.get(1)["spec"], json!(new_path));
    assert_eq!(
        f.get(1)["scheduler"]["workspace_relocation_history"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    let before = f.get(1);
    request.branch = "scheduler/todo-1-changed".into();
    let denied = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(denied["reason"], "RelocationWorkspaceIdentity");
    assert_eq!(f.get(1), before);
}

#[test]
fn relocation_keeps_working_when_the_shared_owner_writes_its_own_files() {
    let (mut f, request, shared) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    fs::write(
        shared.join("owner-unfinished.txt"),
        "the shared writer keeps working",
    )
    .unwrap();
    assert_eq!(
        relocate(
            &mut f.e,
            &mut SystemRunner,
            &request,
            Some(s(&checked["state_sha256"]))
        )
        .unwrap()["saved"],
        true
    );
    assert_eq!(
        fs::read_to_string(shared.join("owner-unfinished.txt")).unwrap(),
        "the shared writer keeps working"
    );
}

#[test]
fn relocation_explicitly_rechecks_terminal_metadata_without_replacing_original_ownership() {
    let (mut f, request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    f.e.backend.records[0]["queue_unreadable"] = json!(false);
    let row = f.get(1);
    assert_eq!(
        cc_scheduler::preparation::recover_workspace(
            &mut SystemRunner,
            f.repo.to_str().unwrap(),
            &row,
            &f.e.backend.records
        )
        .unwrap_err()
        .0,
        "RelocationReceiptMismatch"
    );
    let refreshed = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(refreshed["eligible"], true, "{refreshed}");
    assert_eq!(refreshed["already_recorded"], false);
    assert_eq!(
        relocate(
            &mut f.e,
            &mut SystemRunner,
            &request,
            Some(s(&refreshed["state_sha256"]))
        )
        .unwrap()["saved"],
        true
    );
    let row = f.get(1);
    assert!(cc_scheduler::preparation::recover_workspace(
        &mut SystemRunner,
        f.repo.to_str().unwrap(),
        &row,
        &f.e.backend.records
    )
    .unwrap()
    .is_some());
    f.e.backend.records[0]["work_owner"]["branch"] = json!("scheduler/todo-3-unknown");
    assert_eq!(
        relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap()["reason"],
        "RelocationSourceOwnership"
    );
}

#[test]
fn relocation_successor_checkpoint_and_retry_use_the_successors_branch() {
    let (mut f, request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    f.e.backend.real_workspaces = true;
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
    let worker = s(&f.get(1)["scheduler"]["job"]).to_owned();
    let target = Path::new(&request.worktree);
    let assigned_owner = cc_job_policy::work_owner::checkout(&request.worktree);
    let record =
        f.e.backend
            .records
            .iter_mut()
            .find(|job| job["id"] == worker)
            .unwrap();
    record["cwd"] = json!(request.worktree);
    record["project"] = json!("cc-pc-tools-rs");
    record["work_owner"] = assigned_owner;
    let next = "scheduler/todo-1-successor-followup";
    run_git(&mut SystemRunner, target, &["switch", "-c", next]).unwrap();
    let checkpoint = cc_scheduler::workspace_checkpoint::Request {
        id: 1,
        job: worker.clone(),
        expected_branch: request.branch.clone(),
        branch: next.into(),
        head: request.head.clone(),
    };
    assert_eq!(
        cc_scheduler::workspace_checkpoint::checkpoint(&mut f.e, &mut SystemRunner, &checkpoint)
            .unwrap()["saved"],
        true
    );
    f.e.backend
        .records
        .iter_mut()
        .find(|job| job["id"] == worker)
        .unwrap()["status"] = json!("failed");
    f.reconcile();
    let row = f.get(1);
    let recovered = cc_scheduler::preparation::recover_workspace(
        &mut SystemRunner,
        f.repo.to_str().unwrap(),
        &row,
        &f.e.backend.records,
    )
    .unwrap()
    .unwrap();
    assert_eq!(recovered["branch"], next);
    f.e.backend.clock = T + 601.;
    f.dispatch();
    assert_eq!(
        f.e.backend.launched.len(),
        2,
        "{} {:?}",
        f.e.report,
        f.e.failures
    );
    assert_eq!(f.e.backend.launched[1]["item"]["scheduler"]["branch"], next);
}

#[test]
fn relocation_requalification_no_longer_needs_the_finished_parent_or_shared_checkout() {
    let (mut f, mut request, shared) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    let old_spec = PathBuf::from(s(&f.get(1)["spec"]));
    let text = format!(
        "{}\nContinue after the shared owner finished.\n",
        fs::read_to_string(&old_spec).unwrap()
    );
    let reference = f.repo.join("retained-continuation.md");
    fs::write(&reference, &text).unwrap();
    let new_spec = old_spec
        .parent()
        .unwrap()
        .join(format!("1-{}.md", "c".repeat(32)));
    fs::write(&new_spec, &text).unwrap();
    f.e.edit(&json!(1), |row| {
        push(&mut row["spec_history"], json!({"spec":old_spec}));
        row["spec"] = json!(new_spec);
    })
    .unwrap();
    f.e.edit(&json!(2), |row| {
        row["status"] = json!("done");
        row["scheduler"]["job"] = Value::Null;
        row["scheduler"]["last_job"] = json!("parent");
    })
    .unwrap();
    f.e.backend.records.retain(|job| job["id"] != "parent");
    f.e.backend.ps.remove(&2);
    // This is an isolated synthetic checkout, never a production task's files.
    run_git(
        &mut SystemRunner,
        &f.repo,
        &["worktree", "remove", "--force", shared.to_str().unwrap()],
    )
    .unwrap();
    request.spec_reference = reference.to_str().unwrap().into();
    request.spec_sha256 = cc_job_policy::product_retry::digest(text.as_bytes());
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    assert_eq!(checked["eligible"], true, "{checked}");
    assert_eq!(
        relocate(
            &mut f.e,
            &mut SystemRunner,
            &request,
            Some(s(&checked["state_sha256"]))
        )
        .unwrap()["saved"],
        true
    );
    let row = f.get(1);
    let recovered = cc_scheduler::preparation::recover_workspace(
        &mut SystemRunner,
        f.repo.to_str().unwrap(),
        &row,
        &f.e.backend.records,
    )
    .unwrap()
    .unwrap();
    assert_eq!(recovered["branch"], request.branch);
    assert_eq!(row["spec"], json!(new_spec));
    struct ScopeProbe {
        blocked: String,
        seen: std::sync::Arc<std::sync::Mutex<Vec<String>>>,
    }
    impl Runner for ScopeProbe {
        fn run(&mut self, call: &Invocation) -> Result<Output> {
            if let Some(at) = call.args.iter().position(|arg| arg == "--guard-path") {
                let path = call.args[at + 1].clone();
                self.seen.lock().unwrap().push(path.clone());
                return Ok(Output{code:0,stdout:json!({"client_resource":path==self.blocked,"classification_unverified":false,"access_allowed":path!=self.blocked}).to_string(),stderr:String::new()});
            }
            SystemRunner.run(call)
        }
    }
    let seen = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
    let mut backend = RealBackend::new(f.e.root.clone());
    backend.runner = Box::new(ScopeProbe {
        blocked: shared.to_str().unwrap().into(),
        seen: seen.clone(),
    });
    let recovered = backend
        .workspace_recovery(f.repo.to_str().unwrap(), &row, &f.e.backend.records)
        .unwrap()
        .unwrap();
    assert_eq!(recovered["branch"], request.branch);
    let seen = seen.lock().unwrap();
    assert!(seen
        .iter()
        .any(|path| Path::new(path) == Path::new(&request.worktree)));
    assert!(!seen.iter().any(|path| Path::new(path) == shared));
}

#[test]
fn relocation_rejects_a_changed_request_target_before_any_git_read() {
    struct GitProbe {
        calls: usize,
    }
    impl Runner for GitProbe {
        fn run(&mut self, call: &Invocation) -> Result<Output> {
            self.calls += 1;
            SystemRunner.run(call)
        }
    }
    let (mut f, request, _) = fixture();
    let checked = relocate(&mut f.e, &mut SystemRunner, &request, None).unwrap();
    relocate(
        &mut f.e,
        &mut SystemRunner,
        &request,
        Some(s(&checked["state_sha256"])),
    )
    .unwrap();
    let mut row = f.get(1);
    row["scheduler"]["workspace_relocation"]["request"]["worktree"] = json!(f.repo);
    let mut probe = GitProbe { calls: 0 };
    let error = cc_scheduler::preparation::recover_workspace(
        &mut probe,
        f.repo.to_str().unwrap(),
        &row,
        &f.e.backend.records,
    )
    .unwrap_err();
    assert_eq!(error.0, "RelocationReceiptMismatch");
    assert_eq!(probe.calls, 0, "an unqualified request path was inspected");
}
