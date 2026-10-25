mod support;
use cc_job_policy::initial_review as rule;
use serde_json::{Value, json};
use support::initial_review::*;
fn sha(f: &support::Fixture) -> String {
    cc_job_policy::product_retry::digest(
        &std::fs::read(f.db["items"][1]["spec"].as_str().unwrap()).unwrap(),
    )
}

#[test]
fn cli_accepts_only_the_fixed_typed_preview_or_cas() {
    let good = vec![
        "reevaluate-unstarted-review".into(),
        "--id".into(),
        "3900".into(),
    ];
    assert_eq!(rule::arguments(&good), Some(None));
    let mut apply = good.clone();
    apply.extend([
        "--apply".into(),
        "--expected-state-sha256".into(),
        "a".repeat(64),
    ]);
    assert_eq!(rule::arguments(&apply), Some(Some("a".repeat(64).as_str())));
    for values in [
        vec!["reevaluate-unstarted-review", "--id", "3649"],
        vec!["reevaluate-unstarted-review", "--id", "3900", "--apply"],
        vec![
            "reevaluate-unstarted-review",
            "--id",
            "3900",
            "--apply",
            "--expected-state-sha256",
            "null",
        ],
        vec![
            "reevaluate-unstarted-review",
            "--id",
            "3900",
            "--id",
            "3900",
        ],
        vec!["reevaluate-unstarted-review", "--id", "03900"],
    ] {
        assert!(
            rule::arguments(&values.into_iter().map(str::to_owned).collect::<Vec<_>>()).is_none()
        );
    }
}
#[test]
fn exact_policy_registration_preserves_request_history_and_rejects_duplicate_cas() {
    let (f, source, workspace) = fixture();
    let original = f.db["items"][1].clone();
    let p = rule::proposal(
        &original,
        &source,
        &workspace,
        &sha(&f),
        &health(),
        true,
        AT,
    )
    .unwrap();
    let mut row = original.clone();
    assert!(rule::apply(&mut row, &p));
    assert!(!rule::apply(&mut row, &p));
    for k in [
        "spec",
        "model",
        "effort",
        "model_origin",
        "effort_origin",
        "subwork",
        "jobs",
        "spec_history",
    ] {
        assert_eq!(row[k], original[k]);
    }
    assert_eq!(
        row["scheduler"]["initial_review"]["prior_not_before"],
        AT + 900.
    );
    assert_eq!(
        row["scheduler"]["initial_review"]["prior_resource_wait"],
        original["scheduler"]["resource_wait"]
    );
    let mut changed = original.clone();
    changed["note"] = json!("concurrent producer");
    assert!(!rule::apply(&mut changed, &p));
    assert!(rule::current(
        &row,
        &source,
        &workspace,
        &sha(&f),
        &health(),
        AT,
        None
    ));
}
#[test]
fn owner_unknown_legacy_and_changed_inputs_do_not_authorize_initial_selection() {
    for case in [
        "owner",
        "absent",
        "null",
        "unknown",
        "effort_owner",
        "legacy_hint",
        "different_id",
        "jobs",
        "stop",
        "dependency",
        "wait",
        "deadline",
        "reason",
        "selection",
        "relation",
        "origin_job",
        "same_author",
        "unobserved_author",
        "worker",
        "bad_type",
    ] {
        let (mut f, mut source, workspace) = fixture();
        let digest = sha(&f);
        let row = &mut f.db["items"][1];
        let mut absent = true;
        match case {
            "owner" => row["model_origin"] = json!("owner_request"),
            "absent" => {
                row.as_object_mut().unwrap().remove("model_origin");
            }
            "null" => row["model_origin"] = Value::Null,
            "unknown" => row["model_origin"] = json!("legacy"),
            "effort_owner" => row["effort_origin"] = json!("owner_request"),
            "legacy_hint" => {
                row["legacy_model_hint"] = json!("gpt-6-luna");
                row["model"] = json!("gpt-6.1-sol");
            }
            "different_id" => row["id"] = json!(3649),
            "jobs" => row["jobs"] = json!(["existing"]),
            "stop" => row["stopped"] = json!(true),
            "dependency" => row["wait_on"] = json!([123]),
            "wait" => row["wait_ready"] = json!({"kind":"other"}),
            "deadline" => row["scheduler"]["not_before"] = json!(AT + 1.),
            "reason" => row["scheduler"]["resource_wait"]["reason"] = json!("owner"),
            "selection" => row["scheduler"]["selection"] = json!({"model":"other"}),
            "relation" => row["subwork"]["kind"] = json!("check"),
            "origin_job" => row["subwork"]["origin_job_id"] = json!("different"),
            "same_author" => source["models"] = json!(["openai/gpt-6-luna"]),
            "unobserved_author" => source["models"] = json!([]),
            "worker" => absent = false,
            "bad_type" => row["resume_generation"] = json!("0"),
            _ => unreachable!(),
        }
        assert!(
            rule::proposal(row, &source, &workspace, &digest, &health(), absent, AT).is_none(),
            "{case}"
        );
    }
}
#[test]
fn health_all_exhausted_unknown_stale_future_and_namespace_changes_refuse_selection() {
    for case in [
        "all",
        "unknown",
        "stale",
        "future",
        "from_unknown",
        "not_exhausted",
        "namespace",
        "route_stale",
        "route_future",
        "unsafe_selector",
    ] {
        let (f, source, workspace) = fixture();
        let mut h = health();
        match case {
            "all" => h["openai"]["available"] = json!(false),
            "unknown" => h["openai"]["verified"] = json!(false),
            "stale" => h["openai"]["at"] = json!(AT - 61.),
            "future" => h["at"] = json!(AT + 2.),
            "from_unknown" => h["anthropic"]["verified"] = json!(false),
            "not_exhausted" => h["anthropic"]["exhausted"] = json!(false),
            "namespace" => h["openai"]["safe_routes"] = json!([]),
            "route_stale" => h["openai"]["safe_routes"][0]["at"] = json!(AT - 61.),
            "route_future" => h["openai"]["safe_routes"][0]["at"] = json!(AT + 2.),
            "unsafe_selector" => {
                h["openai"]["safe_routes"][0]["selector"] = json!("private account")
            }
            _ => unreachable!(),
        }
        assert!(
            rule::proposal(
                &f.db["items"][1],
                &source,
                &workspace,
                &sha(&f),
                &h,
                true,
                AT
            )
            .is_none(),
            "{case}"
        );
    }
    for raw in [
        Value::Null,
        json!({"accounts":[]}),
        json!({"accounts":[{"type":"api-key"}]}),
        json!({"accounts":[{"type":"codex"},{"type":"api"}]}),
    ] {
        assert!(!rule::oauth_only(&raw));
    }
    assert!(rule::oauth_only(&json!({"accounts":[{"type":"codex"}]})));
}
#[test]
fn admission_binds_scope_spec_route_generation_and_the_single_worker() {
    let (f, source, workspace) = planned();
    let digest = sha(&f);
    let mut row = f.db["items"][1].clone();
    row["scheduler"]["phase"] = json!("prepared");
    let mut selected = json!({});
    rule::select(&mut selected, &row["scheduler"]["initial_review"]);
    row["scheduler"]["selection"] = selected.clone();
    let mut job = selected;
    job["id"] = json!("worker");
    job["todo_id"] = json!(3900);
    job["cwd"] = row["scheduler"]["worktree"].clone();
    job["launch_key"] = row["scheduler"]["key"].clone();
    job["review_source_jobs"] = json!([rule::AUTHOR]);
    job["capacity_account_selector"] = json!("main");
    assert!(rule::current(
        &row,
        &source,
        &workspace,
        &digest,
        &health(),
        AT,
        Some(&job)
    ));
    for case in [
        "scope",
        "model",
        "effort",
        "selector",
        "spec",
        "generation",
        "stop",
        "dependency",
        "job",
        "source",
        "workspace",
    ] {
        let (mut r, mut j, mut a, mut w) =
            (row.clone(), job.clone(), source.clone(), workspace.clone());
        let mut d = digest.clone();
        match case {
            "scope" => j["add_dirs"] = json!(["extra"]),
            "model" => j["model"] = json!("anthropic/claude-opus-5-5"),
            "effort" => j["effort"] = json!("high"),
            "selector" => j["capacity_account_selector"] = json!("p123456"),
            "spec" => d = "f".repeat(64),
            "generation" => r["resume_generation"] = json!(2),
            "stop" => r["stopped"] = json!(true),
            "dependency" => r["wait_on"] = json!([99]),
            "job" => r["jobs"] = json!(["other"]),
            "source" => a["sha256"] = json!("changed"),
            "workspace" => w["head"] = json!("changed"),
            _ => unreachable!(),
        }
        assert!(
            !rule::current(&r, &a, &w, &d, &health(), AT, Some(&j)),
            "{case}"
        );
    }
    row["status"] = json!("doing");
    row["jobs"] = json!(["worker"]);
    row["scheduler"]["job"] = json!("worker");
    row["resume_generation"] = json!(1);
    row["worker_receipt"] = json!({"job":"worker","generation":1});
    row["scheduler"]["key"] = Value::Null;
    row["scheduler"]["phase"] = json!("running");
    job["todo_resume_generation"] = json!(1);
    row["scheduler"]
        .as_object_mut()
        .unwrap()
        .remove("selection");
    assert!(rule::current(
        &row,
        &source,
        &workspace,
        &digest,
        &health(),
        AT,
        Some(&job)
    ));
    row["worker_receipt"]["job"] = json!("other");
    assert!(!rule::current(
        &row,
        &source,
        &workspace,
        &digest,
        &health(),
        AT,
        Some(&job)
    ));
}
