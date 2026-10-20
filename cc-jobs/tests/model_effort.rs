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
        if model.contains("owner-") {
            assert!(!args.iter().any(|v| v.starts_with("service_tier=")));
            assert!(!args.iter().any(|v| v.starts_with("features.fast_mode=")));
        }
    }
}
