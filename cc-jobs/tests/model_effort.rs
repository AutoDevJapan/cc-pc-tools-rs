use cc_jobs::{codex_args, json};

#[test]
fn gpt_launch_explicitly_requests_fast_and_keeps_owner_effort() {
    let args = codex_args(&json!({
        "codex":"codex.cmd", "model":"gpt-6.1-sol", "effort":"max",
        "sandbox":"read-only"
    }));
    assert!(args.iter().any(|v| v == "service_tier=fast"));
    assert!(args.iter().any(|v| v == "features.fast_mode=true"));
    assert!(args.iter().any(|v| v == "model_reasoning_effort=max"));
    assert!(!args.iter().any(|v| v.contains("ultrafast")));
}

#[test]
fn explicit_model_and_effort_survive_launch() {
    for (model, effort) in [
        ("anthropic/claude-opus-5-5", "max"),
        ("gpt-6-luna", "high"),
        ("owner-special-model", "medium"),
        ("gpt-owner-future", "high"),
        ("anthropic/claude-owner-future", "max"),
    ] {
        let args = codex_args(&json!({
            "codex":"codex.cmd", "model":model, "effort":effort,
            "sandbox":"read-only"
        }));
        assert!(args.windows(2).any(|v| v == ["-m", model]));
        assert!(
            args.iter()
                .any(|v| v == &format!("model_reasoning_effort={effort}"))
        );
        if model.contains("owner-") && !model.starts_with("anthropic/") {
            assert!(!args.iter().any(|v| v.starts_with("service_tier=")));
            assert!(!args.iter().any(|v| v.starts_with("features.fast_mode=")));
        }
    }
}

#[test]
fn managed_legacy_hints_use_max_while_a_recorded_owner_override_survives() {
    for model in ["gpt-6.1-sol", "anthropic/claude-sonnet-5-5"] {
        let mut job = json!({"codex":"codex.cmd","model":model,"effort":"high",
            "sandbox":"read-only","todo_id":2962,"launch_key":"0123456789abcdef0123456789abcdef"});
        assert!(
            codex_args(&job)
                .iter()
                .any(|arg| arg == "model_reasoning_effort=max")
        );
        job["effort_origin"] = json!("owner_request");
        assert!(
            codex_args(&job)
                .iter()
                .any(|arg| arg == "model_reasoning_effort=high")
        );
    }
}

#[test]
fn unknown_claude_keeps_its_identity_and_disables_inherited_gpt_fast() {
    let args = codex_args(
        &json!({"codex":"codex.cmd","model":"anthropic/claude-owner-future",
        "effort":"high","sandbox":"read-only"}),
    );
    assert!(
        args.windows(2)
            .any(|w| w == ["-m", "anthropic/claude-owner-future"])
    );
    assert!(args.iter().any(|arg| arg == "service_tier=default"));
    assert!(args.iter().any(|arg| arg == "features.fast_mode=false"));
}
