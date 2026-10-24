use super::*;
use cc_scheduler::workspace_relocation::{relocate, Request};

fn fixture() -> (Fixture, Request, PathBuf) {
    let (mut f, workspace, _) = reserved_branch_fixture();
    let target = PathBuf::from(s(&workspace["scheduler"]["worktree"]));
    let mut runner = SystemRunner;
    cc_scheduler::preparation::ensure_workspace(&mut runner, f.repo.to_str().unwrap(), &workspace, &[]).unwrap();
    run_git(&mut runner, &f.repo, &["remote", "add", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git"]).unwrap();
    let shared = target.parent().unwrap().join("todo-2-shared");
    run_git(&mut runner, &f.repo, &["worktree", "add", "-b", "scheduler/todo-2-current", shared.to_str().unwrap()]).unwrap();
    let source_owner = cc_job_policy::work_owner::checkout(shared.to_str().unwrap());
    let mut old_owner = source_owner.clone();
    old_owner["branch"] = json!("scheduler/todo-2-original");
    let reference = shared.join("continuation.md");
    let spec = format!("ToDo1\nContinue original work.\n{}\n{}\n", target.to_str().unwrap().replace('\\', "/"), s(&workspace["scheduler"]["branch"]));
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
    f.e.edit(&json!(1), |row| row["spec"] = json!(saved)).unwrap();
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
    f.e.backend.records = vec![json!({"id":"finished","status":"succeeded","exit_code":0,"todo_id":1,"project":"cc-pc-tools-rs","cwd":shared,"work_owner":old_owner}), parent];
    f.e.backend.ps.insert(2, proc(2, 0, "codex.exe"));
    fs::write(target.join("pending.txt"), "preserve unfinished work").unwrap();
    let request = Request {
        id:1, source_job:"finished".into(), shared_owner_job:"parent".into(),
        source_branch:"scheduler/todo-2-current".into(),
        source_head:run_git(&mut runner, &shared, &["rev-parse", "HEAD"]).unwrap(),
        worktree:target.to_str().unwrap().into(), branch:s(&workspace["scheduler"]["branch"]).into(),
        head:run_git(&mut runner, &target, &["rev-parse", "HEAD"]).unwrap(),
        spec_reference:reference.to_str().unwrap().into(),
        spec_sha256:cc_job_policy::product_retry::digest(spec.as_bytes()),
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
    for key in ["id", "spec", "spec_history", "note", "needs", "jobs", "processed_results", "status"] {
        assert_eq!(f.get(1)[key], before[key], "{key}");
    }
    assert_eq!(f.get(2), parent);
    assert_eq!(run_git(&mut SystemRunner, &shared, &["status", "--porcelain"]).unwrap(), shared_status);
    assert_eq!(fs::read_to_string(Path::new(&request.worktree).join("pending.txt")).unwrap(), "preserve unfinished work");
    let repeated = relocate(&mut f.e, &mut SystemRunner, &request, Some(&expected)).unwrap();
    assert_eq!(repeated["already_recorded"], true);
    assert_eq!(repeated["saved"], false);
    let row = f.get(1);
    let recovered = cc_scheduler::preparation::recover_workspace(&mut SystemRunner, f.repo.to_str().unwrap(), &row, &f.e.backend.records).unwrap().unwrap();
    assert_eq!(recovered["worktree"], row["scheduler"]["worktree"]);
    assert_eq!(recovered["branch"], request.branch);
    assert!(f.e.backend.launched.is_empty());
}
