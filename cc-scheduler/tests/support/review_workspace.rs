use super::*;

fn legacy_review() -> Fixture {
    let mut f = Fixture::new();
    let mut runner = SystemRunner;
    run_git(&mut runner, &f.repo, &["init", "--quiet"]).unwrap();
    run_git(
        &mut runner,
        &f.repo,
        &[
            "remote",
            "add",
            "origin",
            "https://github.com/adj/fixture.git",
        ],
    )
    .unwrap();
    fs::write(f.repo.join("source.txt"), "fixed source").unwrap();
    run_git(&mut runner, &f.repo, &["add", "source.txt"]).unwrap();
    run_git(
        &mut runner,
        &f.repo,
        &[
            "-c",
            "user.name=fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--quiet",
            "-m",
            "source",
        ],
    )
    .unwrap();
    let parent = PathBuf::from(format!("{}-wt", f.repo.display())).join("todo-1-writer");
    run_git(
        &mut runner,
        &f.repo,
        &[
            "worktree",
            "add",
            "-b",
            "scheduler/todo-1-writer",
            parent.to_str().unwrap(),
        ],
    )
    .unwrap();
    // Reuse an existing reserved ref so this offline acceptance test cannot fetch.
    run_git(&mut runner, &f.repo, &["branch", "scheduler/todo-2-review"]).unwrap();
    fs::write(parent.join("unfinished.txt"), "keep writer work").unwrap();
    let mut owner = cc_job_policy::work_owner::checkout(parent.to_str().unwrap());
    owner["pr"] = json!(282);
    let mut writer = item(1, "other", "doing", &[]);
    writer["title"] = json!("PR282 writer");
    writer["jobs"] = json!(["parent"]);
    writer["scheduler"] =
        json!({"job":"parent","worktree":parent,"branch":"scheduler/todo-1-writer"});
    f.put(writer);
    let mut review = item(2, "other", "todo", &[]);
    review["title"] = json!("PR282 fixed review");
    review["jobs"] = json!(["legacy"]);
    review["model"] = json!("anthropic/claude-opus-5-5");
    review["effort"] = json!("max");
    review["subwork"] =
        json!({"parent_todo_id":1,"origin_job_id":"parent","kind":"review","owner":owner});
    review["scheduler"] = json!({"last_job":"legacy","phase":"preparing","worktree":parent,
        "branch":"scheduler/todo-1-writer","workspace_receipt":{"job":"legacy","todo_id":2,"cwd":parent}});
    f.put(review);
    f.e.backend.records = vec![
        json!({"id":"parent","todo_id":1,"status":"succeeded","cwd":parent,"work_owner":owner}),
        json!({"id":"legacy","todo_id":2,"status":"succeeded","cwd":parent,"exit_code":0}),
    ];
    f.e.backend.ps.clear();
    f.e.backend.real_workspaces = true;
    f
}

#[test]
fn review_workspace_separates_execution_without_replacing_source_or_history() {
    let mut f = legacy_review();
    let before = f.get(2);
    let writer = f.get(1);
    let parent = Path::new(s(&writer["scheduler"]["worktree"]));
    let head = run_git(&mut SystemRunner, parent, &["rev-parse", "HEAD"]).unwrap();
    let status = run_git(&mut SystemRunner, parent, &["status", "--porcelain"]).unwrap();
    assert!(cc_job_policy::subwork::resolve(&f.e.todo().unwrap(), &before, &[], true).is_err());
    let repo = f.repo.to_str().unwrap().to_owned();
    let prepared = f.e.prepare(&before, &repo).unwrap().unwrap();
    assert_ne!(
        prepared["scheduler"]["worktree"],
        before["scheduler"]["worktree"]
    );
    assert_eq!(prepared["scheduler"]["branch"], "scheduler/todo-2-review");
    cc_job_policy::subwork::resolve(&f.e.todo().unwrap(), &prepared, &[], true).unwrap();
    for field in [
        "subwork", "jobs", "spec", "note", "model", "effort", "status", "needs", "wait_on",
    ] {
        assert_eq!(prepared[field], before[field], "{field}");
    }
    assert_eq!(
        prepared["scheduler"]["workspace_receipt"],
        before["scheduler"]["workspace_receipt"]
    );
    let receipt = prepared["scheduler"]["subwork_workspace_migration"].clone();
    assert_eq!(receipt["source_job"], "legacy");
    assert_eq!(
        receipt["previous"]["worktree"],
        before["scheduler"]["worktree"]
    );
    assert_eq!(
        receipt["previous"]["workspace_receipt"],
        before["scheduler"]["workspace_receipt"]
    );
    let repeated = f.e.prepare(&prepared, &repo).unwrap().unwrap();
    assert_eq!(
        repeated["scheduler"]["worktree"],
        prepared["scheduler"]["worktree"]
    );
    assert_eq!(
        repeated["scheduler"]["subwork_workspace_migration"],
        receipt
    );
    assert_eq!(
        run_git(&mut SystemRunner, parent, &["rev-parse", "HEAD"]).unwrap(),
        head
    );
    assert_eq!(
        run_git(&mut SystemRunner, parent, &["status", "--porcelain"]).unwrap(),
        status
    );
    assert_eq!(
        fs::read_to_string(parent.join("unfinished.txt")).unwrap(),
        "keep writer work"
    );
    assert_eq!(f.get(1), writer);
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn review_workspace_refuses_unverified_provenance_and_running_or_queued_review() {
    for change in [
        "parent",
        "kind",
        "owner",
        "unlinked",
        "live",
        "queued",
        "still_alive",
        "unknown",
        "allocation",
    ] {
        let mut f = legacy_review();
        let mut row = f.get(2);
        match change {
            "parent" => row["subwork"]["origin_job_id"] = json!("unknown"),
            "kind" => row["subwork"]["kind"] = json!("implementation"),
            "owner" => row["subwork"]["owner"]["git_common"] = json!("unknown"),
            "unlinked" => row["jobs"] = json!([]),
            "live" => f.e.backend.records[1]["status"] = json!("running"),
            "queued" => {
                f.e.backend
                    .records
                    .push(json!({"id":"new","todo_id":2,"status":"queued"}))
            }
            "still_alive" => {
                let process = proc(7, 0, "codex.exe");
                f.e.backend.records[1]["pid"] = json!(7);
                f.e.backend.records[1]["pid_created"] = process["created"].clone();
                f.e.backend.ps.insert(7, process);
            }
            "unknown" => f.e.backend.records[1]["status"] = Value::Null,
            "allocation" => row["scheduler"]["worktree"] = json!(f.temp.path().join("unrelated")),
            _ => unreachable!(),
        }
        f.e.edit(&json!(2), |saved| *saved = row.clone()).unwrap();
        let bytes = fs::read(&f.e.todo_path).unwrap();
        assert!(
            f.e.prepare(&row, f.repo.to_str().unwrap()).is_err(),
            "{change}"
        );
        assert_eq!(fs::read(&f.e.todo_path).unwrap(), bytes, "{change}");
        assert!(!PathBuf::from(format!("{}-wt/todo-2-review", f.repo.display())).exists());
        assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn review_workspace_dispatch_keeps_normal_capacity_model_and_admission_checks() {
    for gate in [
        "open", "cpu", "memory", "cargo", "deadline", "wait", "stop", "active",
    ] {
        let mut f = legacy_review();
        match gate {
            "cpu" => f.e.backend.m["cpu_percent"] = json!(99),
            "memory" => f.e.backend.m["available_gb"] = json!(3),
            "cargo" => {
                f.e.edit(&json!(2), |row| row["needs"] = json!(["cargo"]))
                    .unwrap();
                f.e.backend.ps.insert(8, proc(8, 0, "cargo.exe"));
            }
            "deadline" => {
                f.e.edit(&json!(2), |row| {
                    row["scheduler"]["not_before"] = json!(T + 3600.)
                })
                .unwrap();
            }
            "wait" => {
                f.e.edit(&json!(2), |row| row["wait_on"] = json!([1]))
                    .unwrap();
            }
            "stop" => {
                f.e.edit(&json!(2), |row| row["status"] = json!("block"))
                    .unwrap();
            }
            "active" => {
                f.e.backend
                    .records
                    .push(json!({"id":"other","todo_id":2,"status":"queued"}))
            }
            _ => (),
        }
        let report = f.e.dispatch_existing_subwork(2).unwrap();
        assert_eq!(
            report["launched"].is_object(),
            gate == "open",
            "{gate}: {report}"
        );
        assert_eq!(
            f.e.backend.launched.len(),
            usize::from(gate == "open"),
            "{gate}"
        );
        if gate == "open" {
            let row = f.get(2);
            assert_eq!(row["scheduler"]["branch"], "scheduler/todo-2-review");
            assert_eq!(
                f.e.backend.launched[0]["item"]["scheduler"]["selection"]["model"],
                "anthropic/claude-opus-5-5"
            );
            assert_eq!(
                f.e.backend.launched[0]["item"]["scheduler"]["selection"]["effort"],
                "max"
            );
            assert_eq!(
                row["scheduler"]["subwork_admission"]["worktree"],
                row["scheduler"]["worktree"]
            );
            let repeated = f.e.dispatch_existing_subwork(2).unwrap();
            assert_eq!(repeated["active"], true);
            assert!(repeated["launched"].is_null());
            assert_eq!(f.e.backend.launched.len(), 1);
        }
    }
}
