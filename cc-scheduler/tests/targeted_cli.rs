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
