use std::process::Command;
fn invoke(args: &[&str]) -> std::process::Output {
    let mut c = Command::new(env!("CARGO_BIN_EXE_scheduler"));
    c.args(args);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        c.creation_flags(0x08000000);
    }
    c.output().unwrap()
}
#[test]
fn initial_review_cli_rejects_foreign_ids_and_malformed_cas_before_collection() {
    for args in [
        vec!["reevaluate-unstarted-review", "--id", "3649"],
        vec!["reevaluate-unstarted-review", "--id", "3900", "--apply"],
        vec![
            "reevaluate-unstarted-review",
            "--id",
            "3900",
            "--apply",
            "--expected-state-sha256",
            "null",
        ],
        vec!["reevaluate-unstarted-review", "--id", "3900", "--force"],
    ] {
        let out = invoke(&args);
        assert_eq!(out.status.code(), Some(1));
        assert!(out.stdout.is_empty());
        assert_eq!(
            String::from_utf8(out.stderr).unwrap().trim(),
            "review reevaluation failed InputRejected"
        );
    }
    let out = invoke(&["reevaluate-unstarted-review", "--help"]);
    assert!(out.status.success());
    assert!(
        String::from_utf8(out.stdout)
            .unwrap()
            .contains("--expected-state-sha256")
    );
}
