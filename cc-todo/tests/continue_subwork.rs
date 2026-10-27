use clap::Parser;

#[test]
fn current_parent_can_request_a_new_contract_without_rewriting_the_old_source() {
    let parsed = cc_todo::Cli::try_parse_from([
        "todo", "continue-subwork", "3719", "--source-job", "20261026-180903-todo-3719",
        "--parent-job-id", "20261027-175645-todo-3080", "--resume-generation", "3",
        "--dependency-generation", "1", "--spec-sha256",
        "4f6fe32f62db56bed2acb4d378147ff952ad06230243fb45f7674f3158313fc3",
    ]);
    assert!(parsed.is_ok(), "no ordinary continuation intake: {parsed:?}");
}
