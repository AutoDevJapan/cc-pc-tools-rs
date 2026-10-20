use super::*;

fn ordinary_dispatch(waiting: bool) -> Fixture {
    let mut f = Fixture::new();
    f.e.backend.repair_allowed = true;
    let mut db = f.e.todo().unwrap();
    db["projects"]["cc-pc-tools-rs"] = json!(f.repo);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    f.put(item(1, "cc-pc-tools-rs", if waiting { "wait" } else { "todo" }, &[]));
    f.put(item(2, "cc-pc-tools-rs", "todo", &[]));
    let key = "a".repeat(32);
    let folder = f.e.root.join("prompts");
    fs::create_dir_all(&folder).unwrap();
    let source = folder.join(format!("1-{key}.md"));
    let text = format!("{}\n割り当てた資源: なし\n\n元の登録作業場所: {}\n\n依頼:\n{}",
        COMMON_INSTRUCTIONS, f.repo.display(), fs::read_to_string(s(&f.get(1)["spec"])).unwrap());
    fs::write(&source, &text).unwrap();
    let stored = f.e.root.parent().unwrap().join("codex-jobs/jobs/registered/prompt.md");
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
        if waiting { row["wait_on"] = json!([2]); }
    }).unwrap();
    let mut receipt = job("registered", 0, if waiting { "succeeded" } else { "queued" });
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
        f.e.reconcile_repairs(&mut records, &Processes::new(), &f.cfg.clone()).unwrap();
        let after = f.get(1);
        assert!(arr(&after["jobs"]).contains(&json!("registered")), "waiting={waiting}");
        for field in ["spec", "note", "wait_on"] { assert_eq!(after[field], before[field], "{field}"); }
        for field in ["branch", "worktree"] { assert_eq!(after["scheduler"][field], before["scheduler"][field]); }
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
        f.e.reconcile_repairs(&mut records, &Processes::new(), &f.cfg.clone()).unwrap();
        assert_eq!(bytes, fs::read(&f.e.todo_path).unwrap());
        assert!(f.e.backend.launched.is_empty());
        assert!(f.e.backend.cancelled.is_empty());
        assert!(f.e.backend.notices.is_empty());
    }
}
