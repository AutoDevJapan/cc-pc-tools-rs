use super::*;

#[test]
fn grok_quota_selects_a_healthy_independent_model_for_existing_subwork() {
    let (mut f, mut sub) = fixture();
    f.e.backend.records[1]["model"] = json!("grok-4.7");
    sub.db["items"][1]["model"] = json!("grok-4.7");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let prepared = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap()).unwrap().unwrap();
    let selection = &prepared["scheduler"]["selection"];
    assert_eq!(selection["model"], "gpt-6-astra");
    assert_eq!(selection["review_excluded_models"], json!(["gpt-6.1-sol"]));
    assert!(selection["quota_recovery"].is_object());
    assert!(prepared["scheduler"]["resume_session"].is_null());
    assert_eq!(prepared["subwork"], sub.db["items"][1]["subwork"]);
    assert_eq!(prepared["spec"], sub.db["items"][1]["spec"]);
    assert_eq!(f.e.backend.records[1]["model"], "grok-4.7");
}

pub(super) fn inventory(at: f64) -> Value {
    json!({"at":at,"anthropic":{"at":at,"verified":true,"exhausted":true,"available":false},
        "openai":{"at":at,"verified":true,"exhausted":false,"available":true}})
}

fn fixture() -> (Fixture, subwork_support::Fixture) {
    let mut sub = subwork_support::Fixture::new();
    sub.db["items"][1]["task_type"] = json!("review");
    sub.db["items"][1]["sandbox"] = json!("read-only");
    sub.db["items"][1]["model"] = json!("gpt-6-luna");
    sub.db["items"][1]["model_origin"] = json!("policy");
    sub.db["items"][1]["effort"] = json!("max");
    sub.db["items"][1]["review_source_jobs"] = json!(["parent"]);
    sub.db["items"][1]["jobs"] = json!(["quota-reader"]);
    sub.db["items"][1]["scheduler"]["last_job"] = json!("quota-reader");
    let row = &sub.db["items"][1];
    sub.jobs.push(json!({"id":"quota-reader","todo_id":2,"status":"failed",
        "last_failure_kind":"quota","model":"anthropic/claude-opus-5-5","model_origin":"policy",
        "effort":"max","cwd":row["scheduler"]["worktree"],"task_type":"review",
        "sandbox":"read-only","mcp_profile":"minimal","network":false,"add_dirs":[],
        "approval_policy":"never","approvals_reviewer":"user",
        "launch_key":"11111111111111111111111111111111",
        "started_at":iso(T-120.),"finished_at":iso(T-60.),
        "finite_review":{"todo_id":2,"spec":row["spec"],"launch_key":"11111111111111111111111111111111",
            "spec_sha256":cc_job_policy::product_retry::digest(&std::fs::read(s(&row["spec"])).unwrap())},
        "review_source_jobs":["parent"],"review_author_models":["gpt-6.1-sol"],
        "todo_dependency_generation":0,"todo_resume_generation":0}));
    let mut f = Fixture::new();
    f.e.backend.quota_inventory = inventory(T);
    f.e.backend.records = sub.jobs.clone();
    f.e.backend.ps.clear();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    (f, sub)
}

#[test]
fn verified_quota_review_keeps_policy_luna_in_scheduler_preparation() {
    let (mut f, sub) = fixture();
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let selected = &prepared["scheduler"]["selection"];
    assert_eq!(
        selected["model"], "gpt-6-luna",
        "verified quota recovery returned to the depleted review default"
    );
    assert_eq!(selected["model_origin"], "policy");
    assert_eq!(selected["effort"], "max");
    assert_eq!(
        selected["finite_review_spec_sha256"],
        cc_job_policy::product_retry::digest(&std::fs::read(s(&prepared["spec"])).unwrap())
    );
    assert_eq!(selected["review_excluded_models"], json!(["gpt-6.1-sol"]));
    assert!(selected["quota_recovery"].is_object());
    assert!(prepared["scheduler"]["resume_session"].is_null());
    let invocation = RealBackend::new(f.e.root.clone()).launch_command(
        &prepared,
        Path::new("review.md"),
        selected,
    );
    for (flag, value) in [
        ("--model", "gpt-6-luna"),
        ("--model-origin", "policy"),
        ("--effort", "max"),
        ("--task-type", "review"),
        ("--sandbox", "read-only"),
    ] {
        assert!(
            invocation.args.windows(2).any(|args| args == [flag, value]),
            "{flag}"
        );
    }
}

#[test]
fn quota_selection_requires_current_drained_scope_and_fresh_oauth() {
    for case in [
        "unknown_quota",
        "stale_quota",
        "target_empty",
        "wrong_job",
        "wrong_generation",
        "changed_spec",
        "unbound_spec",
        "rewritten_spec",
        "wrong_worktree",
        "wider_scope",
        "not_drained",
        "owner_stop",
        "cancel_without_intent",
        "same_author",
        "future_finish",
    ] {
        let (mut f, mut sub) = fixture();
        match case {
            "unknown_quota" => f.e.backend.quota_inventory["openai"]["verified"] = json!(false),
            "stale_quota" => f.e.backend.quota_inventory["openai"]["at"] = json!(T - 61.),
            "target_empty" => f.e.backend.quota_inventory["openai"]["available"] = json!(false),
            "wrong_job" => f.e.backend.records[1]["todo_id"] = json!(99),
            "wrong_generation" => sub.db["items"][1]["resume_generation"] = json!(99),
            "changed_spec" => {
                f.e.backend.records[1]["finite_review"]["spec"] = json!("different.md")
            }
            "unbound_spec" => {
                f.e.backend.records[1]["finite_review"]
                    .as_object_mut()
                    .unwrap()
                    .remove("spec_sha256");
            }
            "rewritten_spec" => std::fs::write(
                s(&sub.db["items"][1]["spec"]),
                "Different instructions before recovery",
            )
            .unwrap(),
            "wrong_worktree" => f.e.backend.records[1]["cwd"] = json!(sub.repo),
            "wider_scope" => sub.db["items"][1]["add_dirs"] = json!([sub.repo]),
            "not_drained" => {
                f.e.backend.records[1]["pid"] = json!(444);
                f.e.backend.records[1]["pid_created"] = json!(100);
                let mut p = proc(444, 0, "codex.exe");
                p["created"] = json!(100);
                f.e.backend.ps.insert(444, p);
            }
            "owner_stop" => sub.db["items"][1]["stopped"] = json!(true),
            "cancel_without_intent" => f.e.backend.records[1]["status"] = json!("cancelled"),
            "same_author" => f.e.backend.records[0]["model"] = json!("gpt-6-luna"),
            "future_finish" => f.e.backend.records[1]["finished_at"] = json!(iso(T + 60.)),
            _ => unreachable!(),
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let result = f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap());
        assert!(result.is_err(), "{case} unexpectedly prepared {result:?}");
        assert!(f.e.backend.launched.is_empty());
    }
}

#[test]
fn ordinary_policy_review_keeps_its_default_and_owner_choice_remains_distinct() {
    for owner in [false, true] {
        let (mut f, mut sub) = fixture();
        sub.db["items"][1]["scheduler"]["last_job"] = Value::Null;
        sub.db["items"][1]["model_origin"] = json!(if owner { "owner_request" } else { "policy" });
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let p =
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .unwrap()
                .unwrap();
        assert_eq!(
            p["scheduler"]["selection"]["model"],
            if owner {
                "gpt-6-luna"
            } else {
                "anthropic/claude-opus-5-5"
            }
        );
        assert!(p["scheduler"]["selection"]["quota_recovery"].is_null());
    }
}

#[test]
fn selected_review_default_waits_on_verified_exhaustion_before_registration() {
    let (mut f, mut sub) = fixture();
    sub.db["items"][1]["scheduler"]["last_job"] = Value::Null;
    f.e.backend.m["quota_block_model"] = json!("anthropic/claude-opus-5-5");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    assert!(
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .is_none()
    );
    assert_eq!(
        f.get(2)["scheduler"]["resource_wait"]["reason"],
        "quota_exhausted"
    );
    assert!(f.e.backend.launched.is_empty());
}

#[test]
fn recovery_cancel_keeps_the_quota_classification_and_policy_choice() {
    let (mut f, sub) = fixture();
    f.e.backend.records[1]["status"] = json!("cancelled");
    f.e.backend.records[1]["recovery_cancel"] = json!({"key":"drain","committed_at":iso(T-50.)});
    let p =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    assert_eq!(p["scheduler"]["selection"]["model"], "gpt-6-luna");
    assert_eq!(f.e.backend.records[1]["last_failure_kind"], "quota");
}

#[test]
fn quota_preparation_rejects_a_spec_changed_during_metadata_collection() {
    let (mut f, sub) = fixture();
    f.e.backend.quota_spec_edit = Some((
        PathBuf::from(s(&sub.db["items"][1]["spec"])),
        "Another fixed head".into(),
    ));
    assert!(
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .is_err()
    );
    assert!(f.e.backend.launched.is_empty());
}
