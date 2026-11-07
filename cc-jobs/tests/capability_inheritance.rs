use cc_jobs::{cli, codex_args, json};

#[test]
fn ordinary_roles_inherit_configuration_without_capability_overrides() {
    for role in ["implementation", "review", "research"] {
        let parsed = cli::parse(
            &[
                "run",
                "--name",
                "capabilities",
                "--prompt-file",
                "request.md",
                "--task-type",
                role,
            ]
            .map(String::from),
        )
        .unwrap();
        assert_eq!(parsed.value("sandbox"), Some("inherit"));
        assert_eq!(parsed.value("mcp-profile"), Some("inherit"));
        for model in ["gpt-6.1-sol", "anthropic/claude-sonnet-5-5"] {
            let args = codex_args(&json!({
                "codex":"codex.cmd", "model":model, "effort":"max",
                "task_type":role, "sandbox":"inherit", "mcp_profile":"inherit"
            }));
            assert!(!args.iter().any(|arg| arg == "-s"));
            assert!(!args.iter().any(|arg| arg.starts_with("approval_policy=")
                || arg.starts_with("approvals_reviewer=")
                || arg.starts_with("plugins.")
                || arg.starts_with("mcp_servers.")
                || arg.contains("bypass")));
        }
    }
}

#[test]
fn explicit_workspace_restriction_survives_role_selection() {
    for role in ["implementation", "review", "research"] {
        let args = codex_args(&json!({"codex":"codex.cmd", "model":"gpt-6.1-sol",
            "task_type":role, "sandbox":"workspace-write", "mcp_profile":"inherit"}));
        assert!(
            args.windows(2)
                .any(|args| args == ["-s", "workspace-write"])
        );
        assert!(!args.iter().any(|arg| arg == "danger-full-access"));
    }
}

#[test]
fn browser_keywords_do_not_replace_inherited_or_explicit_tool_configuration() {
    assert_eq!(cc_chrome::profile("inherit", "Chromeで画面確認"), "inherit");
    assert_eq!(cc_chrome::profile("minimal", "Chromeで画面確認"), "minimal");
}

#[test]
fn explicit_lightweight_model_recovery_does_not_depend_on_readonly_permissions() {
    for sandbox in [
        "inherit",
        "read-only",
        "workspace-write",
        "danger-full-access",
    ] {
        let candidates = cc_jobs::capacity::candidates(&json!({
            "model":"anthropic/claude-haiku-5-5","requested_model":"anthropic/claude-haiku-5-5",
            "task_type":"single_file_summary","sandbox":sandbox
        }));
        assert!(candidates.contains(&"gpt-6-luna"), "{sandbox}");
    }
}
