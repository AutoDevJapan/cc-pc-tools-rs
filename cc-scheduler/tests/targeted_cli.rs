use std::process::Command;

#[test]
fn targeted_dispatch_has_an_explicit_cli_entry_without_running_a_global_tick() {
    for command in ["dispatch-one", "dispatch-subwork", "resume-registration"] {
        let output = Command::new(env!("CARGO_BIN_EXE_scheduler"))
            .args([command, "--help"])
            .output()
            .unwrap();
        assert!(output.status.success(), "{command}: {output:?}");
        assert!(String::from_utf8(output.stdout)
            .unwrap()
            .contains("--id <ID>"));
    }
}

#[test]
fn workspace_relocation_exposes_only_explicit_check_and_apply_cli() {
    use std::os::windows::process::CommandExt;
    let output = Command::new(env!("CARGO_BIN_EXE_scheduler"))
        .creation_flags(0x08000000)
        .args(["relocate-workspace", "--help"]).output().unwrap();
    assert!(output.status.success());
    let text = String::from_utf8(output.stdout).unwrap();
    assert!(text.contains("--expected-state-sha256"));
    assert!(text.contains("Never launches"));
}
