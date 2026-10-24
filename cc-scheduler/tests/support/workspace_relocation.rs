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
                f.e.backend.records.push(json!({"id":"unverified","status":"unknown","todo_id":if gate == "unknown_task_worker" {1} else {3},"cwd":request.worktree}));
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
            _ => unreachable!(),
        }
        let before = fs::read(&f.e.todo_path).unwrap();
        let checked = relocate(&mut f.e, &mut SystemRunner, &req, None).unwrap();
        assert_eq!(checked["eligible"], false, "{gate}: {checked}");
        assert!(
            relocate(&mut f.e, &mut SystemRunner, &req, Some(&"a".repeat(64))).is_err(),
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
    assert!(s(&launch["text"]).contains("Keep") || s(&launch["text"]).contains("元の"));
    f.dispatch();
    assert_eq!(f.e.backend.launched.len(), 1);
}
