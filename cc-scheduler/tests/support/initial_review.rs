use super::*;
use cc_job_policy::initial_review as rule;
fn fixture(planned: bool) -> (Fixture, subwork_support::Fixture) {
    let (sub, _, workspace) = if planned {
        subwork_support::initial_review::planned()
    } else {
        subwork_support::initial_review::fixture()
    };
    let mut f = Fixture::new();
    f.e.backend.records = sub.jobs.clone();
    f.e.backend.ps.clear();
    f.e.backend.quota_inventory = subwork_support::initial_review::health();
    f.e.backend.m["initial_workspace"] = workspace;
    f.e.backend.m["quota_block_model"] = json!("anthropic/claude-opus-5-5");
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    (f, sub)
}
#[test]
fn unstarted_policy_review_does_not_wait_for_a_depleted_default_when_oauth_is_available() {
    let (mut f, sub) = fixture(true);
    let prepared =
        f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let s = &prepared["scheduler"]["selection"];
    assert_eq!(s["model"], "gpt-6-luna");
    assert_eq!(s["effort"], "max");
    assert_eq!(s["model_origin"], "policy");
    assert_eq!(s["sandbox"], "read-only");
    assert_eq!(s["add_dirs"], json!([]));
    assert_eq!(s["mcp_profile"], "minimal");
    assert_eq!(s["review_source_jobs"], json!([rule::AUTHOR]));
    assert!(s["initial_review"].is_object());
    let command =
        RealBackend::new(f.e.root.clone()).launch_command(&prepared, Path::new("prompt"), s);
    for (flag, value) in [
        ("--model", "gpt-6-luna"),
        ("--model-origin", "policy"),
        ("--effort", "max"),
        ("--sandbox", "read-only"),
        ("--mcp-profile", "minimal"),
    ] {
        assert!(
            command.args.windows(2).any(|a| a == [flag, value]),
            "{flag}"
        );
    }
    assert!(!command.args.iter().any(|a| a == "--add-dir"));
}
#[test]
fn ordinary_cli_preview_and_cas_preserve_deadline_history_without_launching() {
    let (mut f, _sub) = fixture(false);
    let preview = cc_scheduler::initial_review::run(&mut f.e, None).unwrap();
    assert_eq!(preview["eligible"], true);
    assert_eq!(preview["saved"], false);
    assert_eq!(f.get(3900)["scheduler"]["not_before"], T + 900.);
    let applied =
        cc_scheduler::initial_review::run(&mut f.e, preview["state_sha256"].as_str()).unwrap();
    assert_eq!(applied["saved"], true);
    assert_eq!(applied["launched"], 0);
    assert!(f.e.backend.launched.is_empty());
    assert!(f.get(3900)["scheduler"]["not_before"].is_null());
    assert!(
        cc_scheduler::initial_review::run(&mut f.e, preview["state_sha256"].as_str()).unwrap()["eligible"]
            == false
    );
}
#[test]
fn source_spec_job_stop_and_generation_changes_invalidate_preparation() {
    for case in [
        "spec",
        "source",
        "job",
        "stop",
        "generation",
        "quota",
        "workspace",
    ] {
        let (mut f, mut sub) = fixture(true);
        match case {
            "spec" => fs::write(
                sub.db["items"][1]["spec"].as_str().unwrap(),
                "different fixed head",
            )
            .unwrap(),
            "source" => f.e.backend.records[0]["execution_observed"]["model"] = json!("gpt-6-luna"),
            "job" => {
                f.e.backend
                    .records
                    .push(json!({"id":"already","todo_id":3900,"status":"queued"}))
            }
            "stop" => sub.db["items"][1]["stopped"] = json!(true),
            "generation" => sub.db["items"][1]["resume_generation"] = json!(1),
            "quota" => f.e.backend.quota_inventory["openai"]["available"] = json!(false),
            "workspace" => f.e.backend.m["initial_workspace"]["head"] = json!("changed"),
            _ => unreachable!(),
        }
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        assert!(
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .is_err(),
            "{case}"
        );
        assert!(f.e.backend.launched.is_empty());
    }
}
