mod support;
use cc_job_policy::{
    finished_review::{self, Request},
    product_retry::digest,
    work_owner,
};
use serde_json::{Value, json};
use support::Fixture;

fn setup() -> (Fixture, Value, Value, Request) {
    let mut f = Fixture::new();
    let p = support::tree(&f.repo, "todo-1-source", "scheduler/todo-1-source");
    f.jobs[0]["cwd"] = json!(p);
    f.jobs[0]["work_owner"] = work_owner::checkout(p.to_str().unwrap());
    f.jobs[0]["work_owner"]["pr"] = json!(86);
    f.jobs[0]["status"] = json!("succeeded");
    f.jobs[0]["exit_code"] = json!(0);
    f.jobs[0]["finished_at"] = json!("2026-10-10T01:03:00+09:00");
    f.db["items"][0]["scheduler"]["worktree"] = json!(p);
    f.db["items"][0]["scheduler"]["branch"] = json!("scheduler/todo-1-source");
    f.db["items"][0]["status"] = json!("wait");
    f.db["items"][0]["wait_on"] = json!([2, 3]);
    f.db["items"][1]["status"] = json!("doing");
    f.db["items"][1]["jobs"] = json!(["child"]);
    f.db["items"][1]["scheduler"]["job"] = json!("child");
    let tree = support::tree(&f.repo, "todo-3-writer", "scheduler/todo-3-writer");
    let mut writer = f.jobs[0].clone();
    writer["id"] = json!("writer");
    writer["todo_id"] = json!(3);
    writer["cwd"] = json!(tree);
    writer["work_owner"] = work_owner::checkout(tree.to_str().unwrap());
    writer["work_owner"]["pr"] = json!(86);
    f.db["items"].as_array_mut().unwrap().extend([
        json!({"id":3,"status":"wait","wait_on":[77],"jobs":["writer"],"scheduler":{"job":"writer","worktree":tree,"branch":"scheduler/todo-3-writer"}}),
        json!({"id":77,"status":"block","note":"unrelated cleanup remains unfinished"})]);
    let mut j = f.child_job();
    j["sandbox"] = json!("danger-full-access");
    j["scope_source"] = json!("launcher");
    j["network"] = json!(false);
    j["steps"] = json!(0);
    j["work_wait_on"] = json!([3]);
    j["work_wait_evidence"] = json!({"owner_job_id":"writer","owner":work_owner::owner_evidence(&writer),"candidate_identity":j["work_owner"]});
    j["work_wait_history"] = json!([{"reason":"original_cycle_receipt"}]);
    let r = Request {
        job_id: "child".into(),
        todo_id: 2,
        parent_job_id: "parent".into(),
        parent_todo_id: 1,
        owner_job_id: "writer".into(),
        owner_todo_id: 3,
        read_only: true,
        child_spec_sha256: digest(b"child"),
        parent_spec_sha256: digest(b"parent"),
        prompt_sha256: digest(b"prompt"),
        fixed_head: "a".repeat(40),
        fixed_base: "b".repeat(40),
    };
    (f, j, writer, r)
}
fn plan(f: &Fixture, j: &Value, w: &Value, r: &Request) -> Result<Value, String> {
    finished_review::plan(&f.db, j, &f.jobs[0], w, r, [b"child", b"parent", b"prompt"])
}

#[test]
fn continuation_needs_the_same_finished_qualification_and_no_other_wait() {
    for case in [
        "valid",
        "other_wait",
        "manual_dependency",
        "model",
        "fixed_head",
        "identity",
        "live_source",
    ] {
        let (mut f, j, w, mut r) = setup();
        let mut source = plan(&f, &j, &w, &r).unwrap();
        source["status"] = json!("succeeded");
        source["exit_code"] = json!(0);
        source["finished_at"] = json!("2026-10-17T01:00:00+09:00");
        let mut next = j.clone();
        next["id"] = json!("continuation");
        next["work_wait_on"] = json!([]);
        next["work_wait_evidence"] = Value::Null;
        next["fixed_review_requalification"] = json!({"source_job_id":"child"});
        f.db["items"][1]["jobs"] = json!(["child", "continuation"]);
        f.db["items"][1]["scheduler"]["job"] = json!("continuation");
        r.job_id = "continuation".into();
        match case {
            "other_wait" => next["work_wait_on"] = json!([77]),
            "manual_dependency" => f.db["items"][1]["wait_on"] = json!([77]),
            "model" => next["model"] = json!("changed"),
            "fixed_head" => r.fixed_head = "c".repeat(40),
            "identity" => source["fixed_read_only_review"]["identity"] = json!({}),
            "live_source" => source["status"] = json!("running"),
            _ => (),
        }
        let before = next.clone();
        let result = finished_review::plan_continuation(
            &f.db,
            &next,
            &f.jobs[0],
            &w,
            &r,
            [b"child", b"parent", b"prompt"],
            Some(&source),
        );
        assert_eq!(result.is_ok(), case == "valid", "{case}: {result:?}");
        assert_eq!(next, before);
    }
}

#[test]
fn finished_writer_does_not_make_its_cleanup_a_fixed_readers_prerequisite() {
    let (f, j, w, r) = setup();
    let before = f.db.clone();
    let next = plan(&f, &j, &w, &r).unwrap();
    assert_eq!(next["work_wait_on"], json!([]));
    assert_eq!(next["sandbox"], "read-only");
    assert_eq!(next["work_wait_history"][0], j["work_wait_history"][0]);
    assert_eq!(
        next["work_wait_history"][1]["evidence"],
        j["work_wait_evidence"]
    );
    assert_eq!(f.db, before);
    assert_eq!(f.db["items"][2]["wait_on"], json!([77]));
    assert!(finished_review::binding(&f.db, &next, b"child", b"prompt").is_ok());
    for key in ["id", "cwd", "model", "launch_key", "todo_id"] {
        assert_eq!(next[key], j[key], "{key}");
    }
    // This is no subwork/admission exemption. A different actual writer still blocks.
    let mut live = w.clone();
    live["id"] = json!("another");
    live["status"] = json!("running");
    assert_eq!(
        work_owner::blocker(&next, &[live]).unwrap()["id"],
        "another"
    );
}
#[test]
fn qualification_never_releases_manual_edges_started_work_or_different_scope() {
    for case in [
        "manual",
        "owner_stop",
        "root_stop",
        "started",
        "steps_missing",
        "related_parent",
        "writer_running",
        "writer_failed",
        "writer_history",
        "parent_not_waiting",
        "wrong_evidence",
        "multiple_waits",
        "foreign_repo",
        "changed_spec",
        "owner_scope",
        "network",
        "invalid_head",
    ] {
        let (mut f, mut j, mut w, mut r) = setup();
        match case {
            "manual" => f.db["items"][1]["wait_on"] = json!([77]),
            "owner_stop" => f.db["items"][1]["status"] = json!("block"),
            "root_stop" => f.db["items"][0]["owner_stopped"] = json!(true),
            "started" => j["started_at"] = json!("started"),
            "steps_missing" => {
                j.as_object_mut().unwrap().remove("steps");
            }
            "related_parent" => j["related_todo_ids"] = json!([1]),
            "writer_running" => w["status"] = json!("running"),
            "writer_failed" => w["exit_code"] = json!(1),
            "writer_history" => f.db["items"][2]["jobs"] = json!([]),
            "parent_not_waiting" => f.db["items"][0]["wait_on"] = json!([3]),
            "wrong_evidence" => j["work_wait_evidence"]["owner_job_id"] = json!("parent"),
            "multiple_waits" => j["work_wait_on"] = json!([3, 77]),
            "foreign_repo" => w["work_owner"]["repository"] = json!("different/repo"),
            "changed_spec" => r.child_spec_sha256 = digest(b"changed"),
            "owner_scope" => j["scope_source"] = json!("owner_request"),
            "network" => j["network"] = json!(true),
            "invalid_head" => r.fixed_head = "--other-option".into(),
            _ => unreachable!(),
        }
        assert!(plan(&f, &j, &w, &r).is_err(), "{case}");
    }
}
#[test]
fn binding_rechecks_actual_read_only_rights_and_original_instructions_before_execution() {
    for case in [
        "write",
        "approval",
        "network",
        "changed_spec",
        "changed_prompt",
        "new_wait",
        "stop",
        "source_path",
        "related",
    ] {
        let (mut f, j, w, r) = setup();
        let mut next = plan(&f, &j, &w, &r).unwrap();
        let mut spec = &b"child"[..];
        let mut prompt = &b"prompt"[..];
        match case {
            "write" => next["sandbox"] = json!("danger-full-access"),
            "approval" => next["approval_policy"] = json!("on-request"),
            "network" => next["network"] = json!(true),
            "changed_spec" => spec = b"changed",
            "changed_prompt" => prompt = b"changed",
            "new_wait" => f.db["items"][1]["wait_on"] = json!([77]),
            "stop" => f.db["items"][1]["status"] = json!("block"),
            "source_path" => next["fixed_read_only_review"]["source_cwd"] = json!("other"),
            "related" => next["related_todo_ids"] = json!([1]),
            _ => unreachable!(),
        }
        assert!(
            finished_review::binding(&f.db, &next, spec, prompt).is_err(),
            "{case}"
        );
    }
}
