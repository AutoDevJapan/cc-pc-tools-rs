use super::*;
use cc_scheduler::resume_checkpoint;

fn setup() -> (Fixture, Value) {
    let mut f = Fixture::new();
    let mut db = f.e.todo().unwrap();
    db["projects"]["original"] = json!(f.repo);
    write_json(&f.e.todo_path, &db, 1).unwrap();
    let mut row = item(1626, "original", "todo", &[]);
    row["dispatchable"] = json!(false);
    row["scheduler"] = json!({"branch":"old","worktree":"D:/original","phase":"retry"});
    f.put(row);
    let mut actor = item(3650, "cc-pc-tools-rs", "doing", &[]);
    actor["scheduler"] = json!({"job":"parent-job","phase":"running"});
    f.put(actor);
    f.e.backend.current_checkpoint = Some(json!({"after_branch":"current","head":"a".repeat(40)}));
    (
        f,
        json!({"todo_id":1626,"actor_todo_id":3650,"actor_job":"parent-job"}),
    )
}

#[test]
fn checkpoint_restores_only_verified_branch_record_and_keeps_original_work() {
    let (mut f, request) = setup();
    let before = f.get(1626);
    let preview = resume_checkpoint::run(&mut f.e, &request, None).unwrap();
    assert_eq!(preview["eligible"], true);
    assert_eq!(f.get(1626), before);
    let applied =
        resume_checkpoint::run(&mut f.e, &request, preview["state_sha256"].as_str()).unwrap();
    assert_eq!(applied["saved"], true);
    assert_eq!(applied["launched"], 0);
    let mut after = f.get(1626);
    assert_eq!(after["scheduler"]["branch"], "current");
    assert_eq!(
        after["workspace_checkpoint_history"][0]["before_branch"],
        "old"
    );
    after["scheduler"]["branch"] = json!("old");
    after
        .as_object_mut()
        .unwrap()
        .remove("workspace_checkpoint_history");
    after.as_object_mut().unwrap().remove("updated");
    let mut original = before;
    original.as_object_mut().unwrap().remove("updated");
    assert_eq!(after, original);
    assert!(f.e.backend.launched.is_empty());
    assert!(f.e.backend.cancelled.is_empty());
}

#[test]
fn checkpoint_keeps_stops_dependencies_active_owners_and_missing_proof() {
    for gate in [
        "stop",
        "wait",
        "question",
        "active",
        "actor",
        "actor_stop",
        "actor_question",
        "proof",
        "history",
    ] {
        let (mut f, mut request) = setup();
        match gate {
            "stop" => {
                f.e.edit(&json!(1626), |r| r["owner_stopped"] = json!(true))
                    .unwrap();
            }
            "wait" => {
                f.put(item(99, "other", "todo", &[]));
                f.e.edit(&json!(1626), |r| r["wait_on"] = json!([99]))
                    .unwrap();
            }
            "question" => {
                f.e.edit(&json!(1626), |r| r["owner_question_wait"] = json!({"id":1}))
                    .unwrap();
            }
            "active" => {
                f.e.backend
                    .records
                    .push(json!({"id":"active","todo_id":1626,"status":"queued"}))
            }
            "actor" => request["actor_job"] = json!("other-job"),
            "actor_stop" => {
                f.e.edit(&json!(3650), |r| r["owner_stopped"] = json!(true))
                    .unwrap();
            }
            "actor_question" => {
                f.e.edit(&json!(3650), |r| r["owner_question_wait"] = json!({"id":1}))
                    .unwrap();
            }
            "proof" => f.e.backend.current_checkpoint = None,
            "history" => {
                f.e.edit(&json!(1626), |r| {
                    r["workspace_checkpoint_history"] = json!("bad")
                })
                .unwrap();
            }
            _ => (),
        }
        let before = f.get(1626);
        let preview = resume_checkpoint::run(&mut f.e, &request, None).unwrap();
        assert_eq!(preview["eligible"], false, "{gate}");
        let applied =
            resume_checkpoint::run(&mut f.e, &request, preview["state_sha256"].as_str()).unwrap();
        assert_eq!(applied["saved"], false, "{gate}");
        assert_eq!(f.get(1626), before);
    }
}

#[test]
fn checkpoint_preserves_an_actor_stop_or_replacement_during_proof_recheck() {
    for mutation in ["stop", "replacement", "duplicate_actor", "duplicate_target"] {
        let (mut f, request) = setup();
        let preview = resume_checkpoint::run(&mut f.e, &request, None).unwrap();
        let before = f.get(1626);
        let mut changed = f.e.todo().unwrap();
        let actor = changed["items"]
            .as_array_mut()
            .unwrap()
            .iter_mut()
            .find(|row| row["id"] == 3650)
            .unwrap();
        if mutation == "replacement" {
            actor["scheduler"]["job"] = json!("new-job");
        } else if mutation == "stop" {
            actor["owner_stopped"] = json!(true);
        }
        let stopped_actor = actor.clone();
        if mutation == "duplicate_actor" {
            changed["items"]
                .as_array_mut()
                .unwrap()
                .push(stopped_actor.clone());
        }
        if mutation == "duplicate_target" {
            changed["items"]
                .as_array_mut()
                .unwrap()
                .push(before.clone());
        }
        f.e.backend.checkpoint_edit = Some((f.e.todo_path.clone(), changed));
        f.e.backend.checkpoint_edit_after = 1;
        assert_eq!(
            resume_checkpoint::run(&mut f.e, &request, preview["state_sha256"].as_str())
                .unwrap_err()
                .0,
            "InputChanged"
        );
        assert_eq!(f.get(1626), before);
        assert_eq!(f.get(3650), stopped_actor);
    }
}

#[test]
fn checkpoint_will_not_replace_a_changed_record_or_new_proof_generation() {
    for source_change in [false, true] {
        let (mut f, request) = setup();
        let preview = resume_checkpoint::run(&mut f.e, &request, None).unwrap();
        if source_change {
            f.e.backend.current_checkpoint.as_mut().unwrap()["head"] = json!("b".repeat(40));
        } else {
            f.e.edit(&json!(1626), |r| r["owner_stopped"] = json!(true))
                .unwrap();
        }
        let before = f.get(1626);
        assert_eq!(
            resume_checkpoint::run(&mut f.e, &request, preview["state_sha256"].as_str())
                .unwrap_err()
                .0,
            "InputChanged"
        );
        assert_eq!(f.get(1626), before);
    }
}
