mod support;
use cc_job_policy::{subwork, todo_wait, work_owner};
use serde_json::json;
use support::Fixture;

#[test]
fn same_pr_subwork_is_independent_and_other_roots_still_collide() {
    let mut f = Fixture::new();
    let mut plain = f.child_job();
    plain.as_object_mut().unwrap().remove("subwork");
    assert!(work_owner::blocker(&plain, &f.jobs).is_some());
    f.register();
    assert!(work_owner::reconcile_todos(&mut f.db, &f.jobs).is_empty());
    assert_eq!(f.db["items"][1]["status"], "todo");
    let mut child = f.child_job();
    subwork::refresh_job(&f.db, &f.jobs, &mut child).unwrap();
    assert_eq!(child["parent_job_id"], "parent");
    assert!(work_owner::blocker(&child, &f.jobs).is_none());
    let mut other = f.jobs[0].clone();
    other["id"] = json!("other");
    other["todo_id"] = json!(99);
    f.jobs.push(other);
    assert_eq!(work_owner::blocker(&child, &f.jobs).unwrap()["id"], "other");
}

#[test]
fn live_parent_is_required_for_new_relations_and_migration_is_exact() {
    for status in ["queued", "succeeded", "lost"] {
        let mut f = Fixture::new();
        f.jobs[0]["status"] = json!(status);
        subwork::mark_live(&mut f.jobs, |_| false);
        assert!(
            subwork::register(
                &f.db,
                &f.db["items"][1],
                1,
                "parent",
                "check",
                &f.jobs,
                false
            )
            .is_err()
        );
        assert!(
            subwork::register(
                &f.db,
                &f.db["items"][1],
                1,
                "parent",
                "check",
                &f.jobs,
                true
            )
            .is_err()
        );
        f.db["items"][1]["work_owner_job"] = json!("parent");
        f.db["items"][1]["wait_on"] = json!([1, 77]);
        let rel = subwork::register(
            &f.db,
            &f.db["items"][1],
            1,
            "parent",
            "check",
            &f.jobs,
            true,
        )
        .unwrap();
        assert_eq!(rel["origin_job_id"], "parent");
    }
}

#[test]
fn false_parents_nested_relations_wrong_repos_and_writers_are_rejected() {
    for case in [
        "self",
        "unlinked",
        "wrong_todo",
        "nested",
        "writer",
        "different_repo",
        "same_tree",
        "duplicate_queued",
    ] {
        let mut f = Fixture::new();
        let mut kind = "check";
        match case {
            "self" => f.db["items"][1]["id"] = json!(1),
            "unlinked" => f.db["items"][0]["jobs"] = json!([]),
            "wrong_todo" => f.jobs[0]["todo_id"] = json!(9),
            "nested" => f.db["items"][0]["subwork"] = json!({"parent_todo_id":2}),
            "writer" => kind = "implementation",
            "different_repo" => f.db["projects"]["alias"] = json!(f.dir.path().join("elsewhere")),
            "same_tree" => f.db["items"][1]["scheduler"]["worktree"] = f.jobs[0]["cwd"].clone(),
            "duplicate_queued" => f
                .jobs
                .push(json!({"id":"queued","todo_id":2,"status":"queued"})),
            _ => unreachable!(),
        }
        assert!(
            subwork::register(&f.db, &f.db["items"][1], 1, "parent", kind, &f.jobs, false).is_err(),
            "{case}"
        );
    }
}

#[test]
fn parent_exit_disappearance_and_resumption_recompute_the_actual_parent() {
    let mut f = Fixture::new();
    f.register();
    let mut child = f.child_job();
    subwork::refresh_job(&f.db, &f.jobs, &mut child).unwrap();
    f.jobs[0]["subwork_live"] = json!(false);
    f.jobs[0]["status"] = json!("succeeded");
    subwork::refresh_job(&f.db, &f.jobs, &mut child).unwrap();
    assert!(child["parent_job_id"].is_null());
    subwork::refresh_job(&f.db, &[], &mut child).unwrap();
    let mut resumed = f.jobs[0].clone();
    resumed["id"] = json!("resumed");
    resumed["status"] = json!("running");
    resumed["subwork_live"] = json!(true);
    f.db["items"][0]["jobs"] = json!(["parent", "resumed"]);
    subwork::refresh_job(&f.db, &[resumed], &mut child).unwrap();
    assert_eq!(child["parent_job_id"], "resumed");
}

#[test]
fn queued_state_allocation_and_workspace_changes_invalidate_admission() {
    for case in [
        "block",
        "done",
        "needs",
        "spec",
        "key",
        "worktree",
        "relation",
        "wait",
        "owner_repo",
        "owner_pr",
        "no_admission",
    ] {
        let mut f = Fixture::new();
        f.register();
        let mut job = f.child_job();
        match case {
            "block" | "done" => f.db["items"][1]["status"] = json!(case),
            "needs" => f.db["items"][1]["needs"] = json!(["cargo"]),
            "spec" => f.db["items"][1]["spec"] = json!("changed"),
            "key" => f.db["items"][1]["scheduler"]["key"] = json!("changed"),
            "worktree" => job["cwd"] = f.jobs[0]["cwd"].clone(),
            "relation" => f.db["items"][1]["subwork"]["kind"] = json!("research"),
            "wait" => f.db["items"][1]["wait_on"] = json!([99]),
            "owner_repo" => job["work_owner"]["repository"] = json!("elsewhere/project"),
            "owner_pr" => job["work_owner"]["pr"] = json!(87),
            "no_admission" => {
                f.db["items"][1]["scheduler"]["subwork_admission"] = serde_json::Value::Null
            }
            _ => unreachable!(),
        }
        assert!(
            subwork::refresh_job(&f.db, &f.jobs, &mut job).is_err(),
            "{case}"
        );
    }
}

#[test]
fn parent_waits_for_results_but_subwork_cannot_wait_for_parent_even_indirectly() {
    let mut f = Fixture::new();
    f.register();
    assert!(todo_wait::validate(&f.db, 1, &[2]).is_ok());
    assert!(todo_wait::validate(&f.db, 2, &[1]).is_err());
    f.db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":3,"status":"todo"}));
    f.db["items"][1]["wait_on"] = json!([3]);
    assert!(todo_wait::validate(&f.db, 3, &[1]).is_err());
}

#[test]
fn generic_titles_and_parallel_siblings_keep_the_parent_pr_scope() {
    let mut f = Fixture::new();
    f.db["items"][1]["title"] = json!("有限の検査");
    f.register();
    let mut child = f.child_job();
    child["work_owner"].as_object_mut().unwrap().remove("pr");
    subwork::refresh_job(&f.db, &f.jobs, &mut child).unwrap();
    assert_eq!(child["work_owner"]["pr"], 86);
    let mut sibling = child.clone();
    sibling["id"] = json!("sibling");
    sibling["todo_id"] = json!(3);
    sibling["status"] = json!("running");
    assert!(work_owner::blocker(&child, &[sibling.clone()]).is_none());
    sibling["subwork"]["parent_todo_id"] = json!(99);
    assert!(work_owner::blocker(&child, &[sibling]).is_some());
}

#[test]
fn invalid_queued_subwork_releases_ownership_but_running_work_still_protects_it() {
    let mut f = Fixture::new();
    f.register();
    let mut child = f.child_job();
    let mut normal = child.clone();
    normal["id"] = json!("normal");
    normal["todo_id"] = json!(3);
    normal["created_at"] = json!("2026-10-10T02:00:00+09:00");
    normal.as_object_mut().unwrap().remove("subwork");
    f.db["items"][1]["needs"] = json!(["cargo"]);
    let observed = work_owner::observed_jobs(&f.db, &[child.clone()]);
    assert!(work_owner::blocker(&normal, &observed).is_none());
    for state in ["running", "starting", "surviving"] {
        child["status"] = json!(if state == "surviving" {
            "queued"
        } else {
            state
        });
        child["work_owner_live"] = json!(state == "surviving");
        let observed = work_owner::observed_jobs(&f.db, &[child.clone()]);
        assert!(work_owner::blocker(&normal, &observed).is_some(), "{state}");
    }
}
