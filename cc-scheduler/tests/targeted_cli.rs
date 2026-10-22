use std::process::Command;

#[test]
fn targeted_dispatch_has_an_explicit_cli_entry_without_running_a_global_tick() {
    let output = Command::new(env!("CARGO_BIN_EXE_scheduler"))
        .args(["dispatch-one", "--help"])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    assert!(String::from_utf8(output.stdout)
        .unwrap()
        .contains("--id <ID>"));
}
