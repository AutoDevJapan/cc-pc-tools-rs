use std::os::windows::process::CommandExt;
use std::process::Command;

#[test]
fn admission_cli_has_a_read_only_entry_without_dispatch_initialization() {
    let output = Command::new(env!("CARGO_BIN_EXE_scheduler"))
        .creation_flags(0x08000000)
        .args(["explain-admission", "--help"])
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    let help = String::from_utf8(output.stdout).unwrap();
    assert!(help.contains("--id <ID>"));
    assert!(help.contains("read-only"));
    for args in [
        vec!["--id", "0"],
        vec!["--id", "4091", "--apply"],
        vec!["--all"],
    ] {
        let output = Command::new(env!("CARGO_BIN_EXE_scheduler"))
            .creation_flags(0x08000000)
            .arg("explain-admission")
            .args(args)
            .output()
            .unwrap();
        assert!(!output.status.success());
        assert!(output.stdout.is_empty());
    }
}

#[test]
fn launch_outcome_cli_accepts_only_one_read_only_target() {
    let output=Command::new(env!("CARGO_BIN_EXE_scheduler")).creation_flags(0x08000000)
        .args(["explain-launch-outcome","--help"]).output().unwrap();
    assert!(output.status.success());
    assert!(String::from_utf8(output.stdout).unwrap().contains("Read only saved typed launch evidence"));
    for args in [vec!["--id","0"],vec!["--id","4086","--apply"],vec!["--all"],vec!["--id","4086","--id","4091"]] {
        let output=Command::new(env!("CARGO_BIN_EXE_scheduler")).creation_flags(0x08000000)
            .arg("explain-launch-outcome").args(args).output().unwrap();
        assert!(!output.status.success());assert!(output.stdout.is_empty());
    }
}
