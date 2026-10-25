use super::*;
use cc_job_policy::initial_review as rule;
fn setup() -> (Test, subwork_support::Fixture) {
    let (mut f, _, workspace) = subwork_support::initial_review::planned();
    let t = Test::new();
    *t.rt.epoch_base.lock().unwrap() = Some(subwork_support::initial_review::AT);
    *t.rt.quota_recovery_inventory.lock().unwrap() = subwork_support::initial_review::health();
    *t.rt.initial_review_workspace.lock().unwrap() = workspace;
    t.rt.env
        .lock()
        .unwrap()
        .insert("CC_TODO_HOME".into(), f.dir.path().to_string_lossy().into());
    f.db["items"][1]["scheduler"]["phase"] = json!("prepared");
    let mut selected = json!({});
    rule::select(
        &mut selected,
        &f.db["items"][1]["scheduler"]["initial_review"],
    );
    f.db["items"][1]["scheduler"]["selection"] = selected;
    t.put(f.jobs[0].clone());
    save(&t, &f);
    (t, f)
}
fn save(_t: &Test, f: &subwork_support::Fixture) {
    fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
}
fn register(t: &Test, f: &subwork_support::Fixture) -> Result<i32> {
    let row = &f.db["items"][1];
    let prompt = format!(
        "Unattended instructions mention Chrome\n依頼:\n{}",
        fs::read_to_string(s(row, "spec")).unwrap()
    );
    t.run(
        &[
            "--todo-id",
            "3900",
            "--subwork-of",
            "3894",
            "--parent-job-id",
            rule::AUTHOR,
            "--assigned-needs",
            "",
            "--launch-key",
            s(&row["scheduler"], "key"),
            "--cwd",
            s(&row["scheduler"], "worktree"),
            "--model",
            "gpt-6-luna",
            "--model-origin",
            "policy",
            "--effort",
            "max",
            "--effort-origin",
            "policy",
            "--task-type",
            "review",
            "--review-source-jobs",
            rule::AUTHOR,
            "--sandbox",
            "read-only",
            "--mcp-profile",
            "minimal",
        ],
        prompt.as_bytes(),
    )
}
fn child(t: &Test) -> Value {
    t.ctx
        .list_jobs(&HashSet::new())
        .into_iter()
        .find(|j| j["todo_id"] == 3900)
        .unwrap()
}
#[test]
fn normal_registration_and_final_admission_keep_luna_max_minimal_and_exact_spec() {
    let (t, f) = setup();
    register(&t, &f).unwrap();
    let j = child(&t);
    assert_eq!(j["model"], "gpt-6-luna");
    assert_eq!(j["model_origin"], "policy");
    assert_eq!(j["effort"], "max");
    assert_eq!(j["mcp_profile"], "minimal");
    assert_eq!(j["network"], false);
    assert_eq!(j["add_dirs"], json!([]));
    assert_eq!(j["capacity_account_selector"], "main");
    assert_eq!(
        j["finite_review"]["spec_sha256"],
        j["initial_review"]["spec_sha256"]
    );
    // Ordinary worker receipt increments the generation and removes the key.
    let paths = cc_todo::Paths::at(f.dir.path(), &t.ctx.jobs);
    let mut db = cc_todo::load(&paths.store).unwrap();
    cc_todo::execute(
        &mut db,
        &cc_todo::Command::Start {
            id: 3900,
            job: s(&j, "id").into(),
            progress: None,
        },
        &paths,
    )
    .unwrap();
    fs::write(&paths.store, db.to_string()).unwrap();
    let state = t.ctx.admit(s(&j, "id")).unwrap();
    assert!(state.is_some(), "{}", t.ctx.load(s(&j, "id")).unwrap());
    drop(state);
    let running = t.ctx.load(s(&j, "id")).unwrap();
    assert_eq!(running["status"], "running");
    assert_eq!(running["model"], "gpt-6-luna");
    assert!(running["execution_observed"].is_null());
}
#[test]
fn registration_rejects_changed_spec_scope_source_and_stale_health_without_creating_job() {
    for case in [
        "spec",
        "scope",
        "source",
        "health",
        "namespace",
        "generation",
        "owner",
    ] {
        let (t, mut f) = setup();
        match case {
            "spec" => fs::write(s(&f.db["items"][1], "spec"), "another head").unwrap(),
            "scope" => f.db["items"][1]["scheduler"]["selection"]["network"] = json!(true),
            "source" => {
                let mut j = f.jobs[0].clone();
                j["execution_observed"]["model"] = json!("gpt-6-luna");
                t.put(j);
            }
            "health" => t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["at"] = json!(1.),
            "namespace" => {
                t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["safe_routes"] = json!([])
            }
            "generation" => f.db["items"][1]["resume_generation"] = json!(5),
            "owner" => f.db["items"][1]["model_origin"] = json!("owner_request"),
            _ => unreachable!(),
        }
        save(&t, &f);
        assert!(register(&t, &f).is_err(), "{case}");
        assert!(
            !t.ctx
                .list_jobs(&HashSet::new())
                .iter()
                .any(|j| j["todo_id"] == 3900),
            "{case}"
        );
    }
}
#[test]
fn final_admission_rechecks_current_health_stop_dependencies_and_generation() {
    for case in [
        "quota",
        "stale",
        "stop",
        "dependency",
        "generation",
        "spec",
        "workspace",
    ] {
        let (t, mut f) = setup();
        register(&t, &f).unwrap();
        let j = child(&t);
        match case {
            "quota" => {
                t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["available"] = json!(false)
            }
            "stale" => t.rt.quota_recovery_inventory.lock().unwrap()["openai"]["at"] = json!(1.),
            "stop" => f.db["items"][1]["stopped"] = json!(true),
            "dependency" => f.db["items"][1]["wait_on"] = json!([3894]),
            "generation" => f.db["items"][1]["resume_generation"] = json!(3),
            "spec" => fs::write(s(&f.db["items"][1], "spec"), "changed").unwrap(),
            "workspace" => t.rt.initial_review_workspace.lock().unwrap()["head"] = json!("changed"),
            _ => unreachable!(),
        }
        save(&t, &f);
        assert!(t.ctx.admit(s(&j, "id")).unwrap().is_none(), "{case}");
        assert_ne!(
            t.ctx.load(s(&j, "id")).unwrap()["status"],
            "running",
            "{case}"
        );
    }
}
#[test]
fn same_launch_key_never_publishes_a_second_worker() {
    let (t, f) = setup();
    register(&t, &f).unwrap();
    let first = child(&t);
    let _ = register(&t, &f);
    let jobs = t.ctx.list_jobs(&HashSet::new());
    let jobs: Vec<_> = jobs.iter().filter(|j| j["todo_id"] == 3900).collect();
    assert_eq!(jobs.len(), 1);
    assert_eq!(jobs[0]["id"], first["id"]);
}
