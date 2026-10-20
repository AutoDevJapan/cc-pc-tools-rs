use super::*;

fn ordinary_dispatch(waiting: bool) -> Fixture {
    let mut f = Fixture::new();
    let mut runner = SystemRunner;
    run_git(
        &mut runner,
        &f.repo,
        &["init", "-b", "scheduler/todo-1-test"],
    )
    .unwrap();
    f.e.backend.repair_allowed = true;
    let mut db = f.e.todo().unwrap();
    db["projects"]["cc-pc-tools-rs"] = json!(f.repo);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    f.put(item(
        1,
        "cc-pc-tools-rs",
        if waiting { "wait" } else { "todo" },
        &[],
    ));
    f.put(item(2, "cc-pc-tools-rs", "todo", &[]));
    let key = "a".repeat(32);
    let folder = f.e.root.join("prompts");
    fs::create_dir_all(&folder).unwrap();
    let source = folder.join(format!("1-{key}.md"));
    let text = format!(
        "{}\n割り当てた資源: なし\n\n元の登録作業場所: {}\n\n依頼:\n{}",
        COMMON_INSTRUCTIONS,
        f.repo.display(),
        fs::read_to_string(s(&f.get(1)["spec"])).unwrap()
    );
    fs::write(&source, &text).unwrap();
    let stored =
        f.e.root
            .parent()
            .unwrap()
            .join("codex-jobs/jobs/registered/prompt.md");
    fs::create_dir_all(stored.parent().unwrap()).unwrap();
    fs::write(&stored, format!("{text}{}", cc_job_policy::WRITE_GUIDANCE)).unwrap();
    let tree = f.repo.clone();
    f.e.edit(&json!(1), |row| {
        row["model"] = json!("gpt-6.1-sol");
        row["effort"] = json!("max");
        row["scheduler"] = json!({"key":key,"job":null,"phase":"prepared","worktree":tree,
            "branch":"scheduler/todo-1-test","prompt":source,
            "selection":{"model":"gpt-6.1-sol","effort":"max"},
            "selection_input":{"model":"gpt-6.1-sol","effort":"max"}});
        if waiting {
            row["wait_on"] = json!([2]);
        }
    })
    .unwrap();
    let mut receipt = job(
        "registered",
        0,
        if waiting { "succeeded" } else { "queued" },
    );
    receipt["todo_id"] = json!(1);
    receipt["project"] = json!("cc-pc-tools-rs");
    receipt["scope_source"] = json!("launcher");
    receipt["launch_key"] = json!(key);
    receipt["cwd"] = json!(tree);
    receipt["model"] = json!("gpt-6.1-sol");
    receipt["effort"] = json!("max");
    receipt["prompt_source"] = json!(source);
    receipt["prompt"] = json!(stored);
    receipt["sandbox"] = json!("danger-full-access");
    receipt["exit_code"] = if waiting { json!(0) } else { Value::Null };
    receipt["finished_at"] = if waiting { json!(T) } else { Value::Null };
    receipt["last_message"] = json!("状態: 継続中\n依存作業の完了を待っています。");
    f.e.backend.records = vec![receipt];
    f.e.backend.ps.clear();
    f
}

#[test]
fn ordinary_dispatch_receipt_inspection_is_read_only_and_contains_no_source_text() {
    let mut f = ordinary_dispatch(true);
    let bytes = fs::read(&f.e.todo_path).unwrap();
    let report = cc_scheduler::dispatch_receipt::inspect(&mut f.e, 1).unwrap();
    assert_eq!(report["eligible"], true, "{report}");
    assert_eq!(bytes, fs::read(&f.e.todo_path).unwrap());
    let text = report.to_string();
    assert!(!text.contains(&"a".repeat(32)));
    assert!(!text.contains("指定された機能"));
    assert!(f.e.backend.launched.is_empty());
    assert!(f.e.backend.cancelled.is_empty());
}

#[test]
fn repair_tick_recovers_ordinary_dispatch_and_keeps_dependency_wait() {
    for waiting in [false, true] {
        let mut f = ordinary_dispatch(waiting);
        let before = f.get(1);
        let mut records = f.e.backend.records.clone();
        f.e.reconcile_repairs(&mut records, &Processes::new(), &f.cfg.clone())
            .unwrap();
        let after = f.get(1);
        assert!(
            arr(&after["jobs"]).contains(&json!("registered")),
            "waiting={waiting}"
        );
        for field in ["spec", "note", "wait_on"] {
            assert_eq!(after[field], before[field], "{field}");
        }
        for field in ["branch", "worktree"] {
            assert_eq!(after["scheduler"][field], before["scheduler"][field]);
        }
        if waiting {
            assert_eq!(after["status"], "wait");
            assert!(after["scheduler"]["job"].is_null());
            assert_ne!(after["scheduler"]["phase"], "running");
            assert_eq!(after["scheduler"]["last_job"], "registered");
            assert!(result_received(&after, &json!("registered")));
        } else {
            assert_eq!(after["status"], "doing");
            assert_eq!(after["scheduler"]["job"], "registered");
        }
        let bytes = fs::read(&f.e.todo_path).unwrap();
        f.e.reconcile_repairs(&mut records, &Processes::new(), &f.cfg.clone())
            .unwrap();
        assert_eq!(bytes, fs::read(&f.e.todo_path).unwrap());
        assert!(f.e.backend.launched.is_empty());
        assert!(f.e.backend.cancelled.is_empty());
        assert!(f.e.backend.notices.is_empty());
    }
}

#[test]
fn ordinary_dispatch_receipt_rejects_mismatches_stops_and_conflicting_owners_without_writes() {
    for changed in [
        "spec",
        "shorter_spec",
        "resources",
        "stored_prompt",
        "source_path",
        "generation",
        "model",
        "effort",
        "todo",
        "project",
        "cwd",
        "branch",
        "parent",
        "assigned_needs",
        "scope",
        "stop",
        "cancel",
        "job_stop",
        "question",
        "ready",
        "owner_stop",
        "duplicate_key",
        "other_owner",
        "processed",
        "unknown_state",
        "still_alive",
        "live_wait",
        "owner_question_wait",
        "decision_wait",
        "postponement",
        "stop_kind",
        "todo_dependency",
    ] {
        let mut f = ordinary_dispatch(true);
        match changed {
            "spec" => fs::write(s(&f.get(1)["spec"]), "違う仕様").unwrap(),
            "shorter_spec" => fs::write(s(&f.get(1)["spec"]), "指定された").unwrap(),
            "resources" => {
                f.e.edit(&json!(1), |r| r["needs"] = json!(["cargo"]))
                    .unwrap();
            }
            "stored_prompt" => fs::write(s(&f.e.backend.records[0]["prompt"]), "tampered").unwrap(),
            "source_path" => f.e.backend.records[0]["prompt_source"] = json!("unrelated.md"),
            "generation" => f.e.backend.records[0]["launch_key"] = json!("b".repeat(32)),
            "model" => f.e.backend.records[0]["model"] = json!("another-model"),
            "effort" => f.e.backend.records[0]["effort"] = json!("high"),
            "todo" => f.e.backend.records[0]["todo_id"] = json!(2),
            "project" => f.e.backend.records[0]["project"] = json!("other"),
            "cwd" => f.e.backend.records[0]["cwd"] = json!(f.temp.path()),
            "branch" => {
                f.e.edit(&json!(1), |r| {
                    r["scheduler"]["branch"] = json!("scheduler/todo-1-other")
                })
                .unwrap();
            }
            "parent" => f.e.backend.records[0]["parent_job_id"] = json!("parent"),
            "assigned_needs" => f.e.backend.records[0]["assigned_needs"] = json!([]),
            "scope" => f.e.backend.repair_allowed = false,
            "stop" => {
                f.e.edit(&json!(1), |r| r["status"] = json!("block"))
                    .unwrap();
            }
            "cancel" => {
                f.e.edit(&json!(1), |r| r["cancel_requested"] = json!(true))
                    .unwrap();
            }
            "job_stop" => f.e.backend.records[0]["cancel_requested"] = json!(true),
            "question" => {
                f.e.edit(&json!(1), |r| {
                    r["scheduler"]["question"] = json!("new-question")
                })
                .unwrap();
            }
            "ready" => {
                f.e.edit(&json!(1), |r| {
                    r["wait_ready"] = json!({"condition":"retry-time"})
                })
                .unwrap();
            }
            "owner_stop" => {
                f.e.edit(&json!(1), |r| r["owner_stopped"] = json!(true))
                    .unwrap();
            }
            "duplicate_key" => {
                let mut another = f.e.backend.records[0].clone();
                another["id"] = json!("duplicate");
                f.e.backend.records.push(another);
            }
            "other_owner" => {
                let mut another = f.e.backend.records[0].clone();
                another["id"] = json!("another");
                another["launch_key"] = json!("b".repeat(32));
                another["status"] = json!("running");
                f.e.backend.records.push(another);
            }
            "processed" => {
                f.e.edit(&json!(1), |r| {
                    record_result(r, &json!("registered"), "received")
                })
                .unwrap();
            }
            "unknown_state" => f.e.backend.records[0]["status"] = json!("unknown"),
            "still_alive" => {
                f.e.backend.records[0]["pid"] = json!(7);
                f.e.backend.ps = ps(vec![proc(7, 0, "codex.exe")]);
            }
            "live_wait" => f.e.backend.records[0]["status"] = json!("running"),
            "owner_question_wait" | "decision_wait" => {
                f.e.edit(&json!(1), |r| r[changed] = json!({"existing":"hold"}))
                    .unwrap();
            }
            "postponement" => {
                f.e.edit(&json!(1), |r| {
                    r["owner_postponement_recorded"] = json!(true)
                })
                .unwrap();
            }
            "stop_kind" => {
                f.e.edit(&json!(1), |r| r["stop_kind"] = json!("owner"))
                    .unwrap();
            }
            "todo_dependency" => {
                f.e.edit(&json!(1), |r| r["status"] = json!("todo"))
                    .unwrap();
            }
            _ => unreachable!(),
        }
        let bytes = fs::read(&f.e.todo_path).unwrap();
        let report = cc_scheduler::dispatch_receipt::inspect(&mut f.e, 1).unwrap();
        assert_eq!(report["eligible"], false, "{changed}: {report}");
        let mut records = f.e.backend.records.clone();
        f.e.reconcile_repairs(&mut records, &f.e.backend.ps.clone(), &f.cfg.clone())
            .unwrap();
        assert_eq!(bytes, fs::read(&f.e.todo_path).unwrap(), "{changed}");
        assert!(f.e.backend.launched.is_empty());
        assert!(f.e.backend.cancelled.is_empty());
    }
}

#[test]
fn ordinary_dispatch_receipt_apply_requires_exact_state_and_preserves_concurrent_edit() {
    let mut f = ordinary_dispatch(true);
    let report = cc_scheduler::dispatch_receipt::inspect(&mut f.e, 1).unwrap();
    let bytes = fs::read(&f.e.todo_path).unwrap();
    assert_eq!(
        cc_scheduler::dispatch_receipt::run(&mut f.e, 1, Some(&"0".repeat(64)))
            .unwrap_err()
            .0,
        "ReceiptMismatch"
    );
    assert_eq!(bytes, fs::read(&f.e.todo_path).unwrap());
    let mut changed = f.e.todo().unwrap();
    changed["items"][0]["note"] = json!("newer note");
    f.e.backend.repair_edit = Some((f.e.todo_path.clone(), changed.clone()));
    assert_eq!(
        cc_scheduler::dispatch_receipt::run(&mut f.e, 1, Some(s(&report["state_sha256"])))
            .unwrap_err()
            .0,
        "ReceiptMismatch"
    );
    assert_eq!(f.e.todo().unwrap(), changed);
}

#[test]
fn ordinary_dispatch_recovery_allows_only_the_next_normal_owner_to_checkpoint() {
    let mut f = ordinary_dispatch(true);
    let report = cc_scheduler::dispatch_receipt::inspect(&mut f.e, 1).unwrap();
    let saved =
        cc_scheduler::dispatch_receipt::run(&mut f.e, 1, Some(s(&report["state_sha256"]))).unwrap();
    assert_eq!(saved["saved"], true);
    let row = f.get(1);
    assert_eq!(row["status"], "wait");
    assert_eq!(row["wait_on"], json!([2]));
    assert_eq!(
        cc_scheduler::dispatch_receipt::run(&mut f.e, 1, Some(s(&report["state_sha256"]))).unwrap()
            ["saved"],
        false
    );
    f.e.edit(&json!(2), |r| r["status"] = json!("done"))
        .unwrap();
    f.e.release_repair_waits().unwrap();
    assert_eq!(f.get(1)["status"], "todo");
    assert!(f.get(1)["wait_on"].is_null());
    let mut next = f.e.backend.records[0].clone();
    next["id"] = json!("next-owner");
    next["status"] = json!("running");
    next["created_at"] = json!(T);
    f.e.backend.records.push(next);
    let mut db = f.e.todo().unwrap();
    let paths = cc_todo::Paths::at(
        f.e.todo_path.parent().unwrap(),
        &f.e.todo_path.with_file_name("jobs"),
    );
    cc_todo::execute(
        &mut db,
        &cc_todo::Command::Start {
            id: 1,
            job: "next-owner".into(),
            progress: None,
        },
        &paths,
    )
    .unwrap();
    write_json(&f.e.todo_path, &db, 1).unwrap();
    let mut runner = SystemRunner;
    run_git(
        &mut runner,
        &f.repo,
        &[
            "-c",
            "user.name=fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--allow-empty",
            "-m",
            "saved work",
        ],
    )
    .unwrap();
    fs::write(f.repo.join("unfinished.txt"), "keep pending changes").unwrap();
    let mut request = cc_scheduler::workspace_checkpoint::Request {
        id: 1,
        job: "next-owner".into(),
        expected_branch: "scheduler/todo-1-test".into(),
        branch: "scheduler/todo-1-test".into(),
        head: run_git(&mut runner, &f.repo, &["rev-parse", "HEAD"]).unwrap(),
    };
    assert_eq!(
        cc_scheduler::workspace_checkpoint::checkpoint(&mut f.e, &mut runner, &request).unwrap()
            ["saved"],
        true
    );
    let bytes = fs::read(&f.e.todo_path).unwrap();
    request.job = "registered".into();
    assert_eq!(
        cc_scheduler::workspace_checkpoint::checkpoint(&mut f.e, &mut runner, &request)
            .unwrap_err()
            .0,
        "InputRejected"
    );
    assert_eq!(bytes, fs::read(&f.e.todo_path).unwrap());
    assert_eq!(
        fs::read_to_string(f.repo.join("unfinished.txt")).unwrap(),
        "keep pending changes"
    );
}
