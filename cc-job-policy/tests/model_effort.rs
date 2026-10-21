use cc_job_policy::model_profile;

#[test]
fn review_aliases_are_author_identities_and_preview_is_not_review() {
    assert!(cc_job_policy::same_model(
        "opus",
        "anthropic/claude-opus-5-5"
    ));
    assert!(cc_job_policy::same_model(
        "Anthropic/Claude-Sonnet-5-5",
        "sonnet"
    ));
    assert!(!cc_job_policy::review_task("preview-3081", "preview"));
}

#[test]
fn unknown_provider_combinations_are_never_treated_as_automatic_gpt_defaults() {
    let mut job = serde_json::json!({"todo_id":3081,"model":"anthropic/gpt-6-astra",
        "model_origin":"policy","model_explicit":false,"effort":"high"});
    cc_job_policy::apply_managed_launch_policy(&mut job);
    assert_eq!(job["model"], "anthropic/gpt-6-astra");
    assert_eq!(job["effort"], "high");
}

#[test]
fn unattended_default_keeps_sol_max_for_implementation_and_readers() {
    for (name, kind) in [
        ("req-3081", "implementation"),
        ("research-3081", "research"),
        ("check-3081", "verification"),
    ] {
        let profile = model_profile(name, kind);
        assert_eq!(profile.model, "gpt-6.1-sol", "{name}/{kind}");
        assert_eq!(profile.effort, "max", "{name}/{kind}");
    }
}

#[test]
fn review_default_and_source_history_never_choose_an_authors_model() {
    use cc_job_policy::{author_models, review_profile};
    use serde_json::json;
    assert_eq!(
        model_profile("rev-3081", "review").model,
        "anthropic/claude-opus-5-5"
    );
    let source = json!({"model":"anthropic/claude-opus-5-5","requested_model":"openai/gpt-6.1-sol",
        "capacity_history":[{"from_model":"gpt-6.1-sol","to_model":"anthropic/claude-opus-5-5"}]});
    let authors = author_models(&source);
    assert_eq!(
        review_profile(&authors).unwrap().model,
        "anthropic/claude-sonnet-5-5"
    );
    assert_eq!(review_profile(&authors).unwrap().effort, "max");
}
