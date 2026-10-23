use cc_todo::{Cli, Paths, add_item};
use clap::Parser;
use serde_json::json;

#[test]
fn owner_selection_origins_are_saved_atomically_with_the_dispatchable_spec() {
    for (origin, model, effort) in [
        ("owner_request", "gpt-6-luna", "high"),
        ("policy", "gpt-6.1-sol", "max"),
    ] {
        let dir = tempfile::tempdir().unwrap();
        let source = dir.path().join("instruction.md");
        let text = if origin == "owner_request" {
            "本人がこの作業だけLuna/highを指定した記録"
        } else {
            "AIの旧Luna/highヒント"
        };
        std::fs::write(&source, text).unwrap();
        let mut db = json!({"projects":{"fixture":dir.path()},"items":[],"next_id":1});
        let paths = Paths::at(dir.path(), &dir.path().join("jobs"));
        let command = Cli::try_parse_from([
            "todo",
            "add",
            "fixture",
            "個別指定を保つ",
            "--spec",
            source.to_str().unwrap(),
            "--model",
            "gpt-6-luna",
            "--model-origin",
            origin,
            "--effort",
            "high",
            "--effort-origin",
            origin,
        ])
        .unwrap()
        .command;
        let id = add_item(&mut db, &command, &paths).unwrap();
        assert_eq!(id, 1);
        let row = &db["items"][0];
        assert_eq!(row["dispatchable"], true);
        assert_eq!(row["model_origin"], origin);
        assert_eq!(row["effort_origin"], origin);
        let mut launch = row.clone();
        launch["todo_id"] = row["id"].clone();
        cc_job_policy::apply_managed_launch_policy(&mut launch);
        assert_eq!(launch["model"], model);
        assert_eq!(launch["effort"], effort);
        assert_eq!(
            std::fs::read_to_string(row["spec"].as_str().unwrap()).unwrap(),
            text
        );
    }
}

#[test]
fn an_owner_origin_without_a_selection_or_an_unknown_origin_is_rejected() {
    for args in [
        vec![
            "todo",
            "add",
            "fixture",
            "x",
            "--effort-origin",
            "owner_request",
        ],
        vec![
            "todo",
            "add",
            "fixture",
            "x",
            "--model",
            "gpt-6-luna",
            "--model-origin",
            "guessed",
        ],
    ] {
        assert!(Cli::try_parse_from(args).is_err());
    }
}
