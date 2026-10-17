mod support;
use cc_job_policy::{owner_wait_repair, product_retry::digest, todo_wait, work_owner};
use serde_json::{Value, json};
use support::Fixture;

fn fixture() -> (Fixture, Value, Value) {
    let mut f = Fixture::new();
    let parent_tree = support::tree(&f.repo, "todo-1-parent", "scheduler/todo-1-parent");
    f.jobs[0]["cwd"] = json!(parent_tree);
    f.jobs[0]["work_owner"] = work_owner::checkout(parent_tree.to_str().unwrap());
    f.jobs[0]["work_owner"]["pr"] = json!(86);
    f.jobs[0]["status"] = json!("succeeded");
    f.jobs[0]["exit_code"] = json!(0);
    f.jobs[0]["finished_at"] = json!("2026-10-10T01:02:00+09:00");
    f.jobs[0]["subwork_live"] = json!(false);
    f.db["items"][0]["scheduler"]["worktree"] = json!(parent_tree);
    f.db["items"][0]["scheduler"]["branch"] = json!("scheduler/todo-1-parent");
    f.db["items"][0]["spec"] = f.db["items"][1]["spec"].clone();
    f.db["items"][0]["status"] = json!("wait");
    f.db["items"][0]["wait_on"] = json!([2]);
    f.db["items"][1]["status"] = json!("doing");
    f.db["items"][1]["jobs"] = json!(["child"]);
    f.db["items"][1]["scheduler"]["job"] = json!("child");
    let mut j = f.child_job();
    j["work_wait_on"] = json!([1]);
    j["work_wait_evidence"] = json!({"owner_job_id":"parent", "owner":work_owner::owner_evidence(&f.jobs[0]),"candidate_identity":j["work_owner"]});
    let request = json!({"todo_id":2,"parent_todo_id":1,"job_id":"child","owner_job_id":"parent","kind":"review",
        "child_spec_sha256":digest(b"child spec"),"parent_spec_sha256":digest(b"parent spec"),"prompt_sha256":digest(b"prompt")});
    (f, j, request)
}
fn plan(f: &Fixture, j: &Value, r: &Value) -> Result<Value, String> {
    owner_wait_repair::plan(
        &f.db,
        j,
        &f.jobs[0],
        r,
        b"child spec",
        b"parent spec",
        b"prompt",
    )
}
#[test]
fn two_layer_cycle_is_repaired_without_changing_manual_dependencies_or_history() {
    let (mut f, mut j, r) = fixture();
    assert!(todo_wait::validate(&f.db, 2, &[1]).is_err());
    assert_eq!(work_owner::remembered_pending(&f.db, &j, &f.jobs), vec![1]);
    f.db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":77,"status":"doing"}));
    f.db["items"][1]["wait_on"] = json!([77]);
    j["work_wait_on"] = json!([1, 77]);
    let original_db = f.db.clone();
    let repaired = plan(&f, &j, &r).unwrap();
    assert_eq!(repaired["work_wait_on"], json!([77]));
    assert_eq!(
        work_owner::remembered_pending(&f.db, &repaired, &f.jobs),
        vec![77]
    );
    assert_eq!(f.db, original_db);
    let mut retained = repaired.clone();
    for key in [
        "work_wait_on",
        "work_wait_history",
        "work_wait_evidence",
        "queue_wait_reason",
    ] {
        retained.as_object_mut().unwrap().remove(key);
    }
    let mut before = j.clone();
    for key in [
        "work_wait_on",
        "work_wait_history",
        "work_wait_evidence",
        "queue_wait_reason",
    ] {
        before.as_object_mut().unwrap().remove(key);
    }
    assert_eq!(retained, before);
    assert_eq!(
        repaired["work_wait_history"][0]["evidence"],
        j["work_wait_evidence"]
    );
}
#[test]
fn exact_provenance_and_scope_are_required_not_just_a_shared_pr() {
    for case in [
        "running",
        "started",
        "thread",
        "steps",
        "cancel",
        "owner_stop",
        "owner_failed",
        "parent_running",
        "missing_child_wait",
        "manual",
        "manual_indirect",
        "foreign_repo",
        "stale_branch",
        "same_tree",
        "unlinked",
        "changed_spec",
        "changed_prompt",
        "writer",
        "false_evidence",
        "candidate_evidence",
        "foreign_launch",
    ] {
        let (mut f, mut j, mut r) = fixture();
        match case {
            "running" => j["status"] = json!("running"),
            "started" => j["started_at"] = json!("started"),
            "thread" => j["thread_id"] = json!("thread"),
            "steps" => j["steps"] = json!(1),
            "cancel" => j["cancel_requested"] = json!(true),
            "owner_stop" => f.db["items"][1]["status"] = json!("block"),
            "owner_failed" => f.jobs[0]["exit_code"] = json!(1),
            "parent_running" => f.db["items"][0]["status"] = json!("doing"),
            "missing_child_wait" => f.db["items"][0]["wait_on"] = json!([]),
            "manual" => f.db["items"][1]["wait_on"] = json!([1]),
            "manual_indirect" => {
                f.db["items"][1]["wait_on"] = json!([77]);
                f.db["items"]
                    .as_array_mut()
                    .unwrap()
                    .push(json!({"id":77,"status":"wait","wait_on":[1]}));
            }
            "foreign_repo" => j["work_owner"]["repository"] = json!("other/repo"),
            "stale_branch" => f.db["items"][1]["scheduler"]["branch"] = json!("other"),
            "same_tree" => j["cwd"] = f.jobs[0]["cwd"].clone(),
            "unlinked" => f.db["items"][1]["jobs"] = json!([]),
            "changed_spec" => r["child_spec_sha256"] = json!(digest(b"other")),
            "changed_prompt" => r["prompt_sha256"] = json!(digest(b"other")),
            "writer" => r["kind"] = json!("implementation"),
            "false_evidence" => j["work_wait_evidence"]["owner"]["todo_id"] = json!(99),
            "candidate_evidence" => {
                j["work_wait_evidence"]["candidate_identity"]["branch"] = json!("other")
            }
            "foreign_launch" => j["launch_key"] = json!("other"),
            _ => unreachable!(),
        }
        assert!(plan(&f, &j, &r).is_err(), "{case}");
    }
}
#[test]
fn repair_gives_no_exemption_from_other_live_owners() {
    let (mut f, j, r) = fixture();
    let repaired = plan(&f, &j, &r).unwrap();
    assert_eq!(repaired["work_wait_on"], json!([]));
    assert!(work_owner::remembered_pending(&f.db, &repaired, &f.jobs).is_empty());
    let mut live = f.jobs[0].clone();
    live["id"] = json!("other-author");
    live["todo_id"] = json!(77);
    live["status"] = json!("running");
    live["work_owner_live"] = json!(true);
    f.jobs.push(live);
    assert_eq!(
        work_owner::blocker(&repaired, &f.jobs).unwrap()["id"],
        "other-author"
    );
    assert!(repaired["subwork"].is_null());
    assert!(repaired["parent_job_id"].is_null());
}
