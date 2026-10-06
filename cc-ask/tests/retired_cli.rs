use std::process::Command;

#[test]
fn legacy_launch_commands_reject_before_loading_config_or_creating_state() {
    let root = tempfile::tempdir().unwrap();
    for args in [vec!["automation-start"], vec!["patrol"], vec!["routing"]] {
        let output = Command::new(env!("CARGO_BIN_EXE_discord_ask"))
            .args(args)
            .env("CC_ASK_HOME", root.path().join("absent-state"))
            .env("CC_ASK_CONFIG", root.path().join("absent-config.json"))
            .output()
            .unwrap();
        assert!(!output.status.success());
        assert!(String::from_utf8_lossy(&output.stderr).contains("共有ToDo"));
        assert_eq!(std::fs::read_dir(root.path()).unwrap().count(), 0);
    }
}
