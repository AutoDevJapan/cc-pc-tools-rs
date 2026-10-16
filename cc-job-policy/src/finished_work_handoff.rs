//! Release one exact, completed writer edge after its result was delivered.
//! This is a handoff receipt, not an owner, resource, or permission exemption.
use crate::{product_retry::digest, todo_wait, work_owner};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub job_id: String,
    pub todo_id: i64,
    pub parent_job_id: String,
    pub parent_todo_id: i64,
    pub owner_job_id: String,
    pub owner_todo_id: i64,
    pub child_spec_sha256: String,
    pub parent_spec_sha256: String,
    pub owner_spec_sha256: String,
    pub prompt_sha256: String,
    pub writer_code_head: String,
    pub delivered_source_head: String,
    pub writer_scope: String,
    pub candidate_scope: String,
    pub delivery_marker: String,
}

#[derive(Clone, Copy)]
pub struct Inputs<'a> {
    pub candidate_spec: &'a [u8],
    pub parent_spec: &'a [u8],
    pub owner_spec: &'a [u8],
    pub prompt: &'a [u8],
}

fn s<'a>(v: &'a Value, key: &str) -> &'a str {
    v.get(key).and_then(Value::as_str).unwrap_or("")
}
fn row(db: &Value, id: i64) -> Result<&Value, String> {
    db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|item| item["id"].as_i64() == Some(id))
        .ok_or_else(|| "handoff ToDo record missing".into())
}
fn linked(row: &Value, job: &Value) -> bool {
    row["id"] == job["todo_id"]
        && row["jobs"]
            .as_array()
            .is_some_and(|jobs| jobs.iter().any(|id| id == &job["id"]))
}
fn scope_ok(scope: &str) -> bool {
    !scope.is_empty()
        && !scope.starts_with('/')
        && !scope.contains('\\')
        && scope
            .split('/')
            .all(|part| !part.is_empty() && part != "." && part != "..")
}
fn scopes_overlap(a: &str, b: &str) -> bool {
    a == b || a.starts_with(&format!("{b}/")) || b.starts_with(&format!("{a}/"))
}
fn sha(s: &str) -> bool {
    s.len() == 40 && s.bytes().all(|b| b.is_ascii_hexdigit())
}

/// The caller holds the queue, candidate/parent/writer-history state locks, and
/// ToDo lock, and has verified process identities and the Git receipt.
/// Exactly one remembered automatic edge is removed; normal admission reruns.
pub fn plan(
    db: &Value,
    candidate: &Value,
    parent: &Value,
    owner: &Value,
    request: &Request,
    inputs: Inputs<'_>,
) -> Result<Value, String> {
    let child = row(db, request.todo_id)?;
    let root = row(db, request.parent_todo_id)?;
    let writer = row(db, request.owner_todo_id)?;
    let owner_id = json!(request.owner_todo_id);
    if request.todo_id == request.parent_todo_id
        || request.todo_id == request.owner_todo_id
        || request.parent_todo_id == request.owner_todo_id
        || request.job_id == request.parent_job_id
        || request.job_id == request.owner_job_id
        || request.parent_job_id == request.owner_job_id
        || candidate["id"] != request.job_id
        || parent["id"] != request.parent_job_id
        || owner["id"] != request.owner_job_id
        || !linked(child, candidate)
        || !linked(root, parent)
        || !linked(writer, owner)
        || !["todo", "doing"].contains(&s(child, "status"))
        || child["dispatchable"] != true
        || child["owner_stopped"] == true
        || !child["wait_ready"].is_null()
        || !todo_wait::pending_on(db, child).is_empty()
        || !child["work_owner_job"].is_null()
        || child["subwork"].is_object()
        || root["status"] != "wait"
        || root["owner_stopped"] == true
        || !root["wait_ready"].is_null()
        || !todo_wait::pending_on(db, root).contains(&request.todo_id)
        || writer["status"] != "wait"
        || writer["owner_stopped"] == true
        || todo_wait::pending_on(db, writer).is_empty()
        || todo_wait::pending_on(db, writer).contains(&request.todo_id)
        || parent["status"] != "succeeded"
        || parent["exit_code"] != 0
        || s(parent, "finished_at").is_empty()
        || owner["status"] != "succeeded"
        || owner["exit_code"] != 0
        || s(owner, "finished_at").is_empty()
        || candidate["status"] != "queued"
        || candidate["cancel_requested"] == true
        || !candidate["started_at"].is_null()
        || !candidate["thread_id"].is_null()
        || !candidate["codex_pid"].is_null()
        || candidate["steps"].as_u64() != Some(0)
        || !s(candidate, "parent_job_id").is_empty()
        || candidate["subwork"].is_object()
        || candidate["related_todo_ids"]
            .as_array()
            .is_some_and(|a| !a.is_empty())
        || s(candidate, "launch_key").is_empty()
        || child["scheduler"]["key"] != candidate["launch_key"]
        || candidate["work_wait_on"] != json!([request.owner_todo_id])
        || candidate["queue_wait_reason"]["code"] != "todo_dependency"
        || !candidate["queue_wait_reason"]["wait_on"]
            .as_array()
            .is_some_and(|a| a.contains(&owner_id))
        || candidate["work_wait_evidence"]["owner_job_id"] != owner["id"]
        || candidate["work_wait_evidence"]["owner"] != work_owner::owner_evidence(owner)
        || candidate["work_wait_evidence"]["candidate_identity"] != candidate["work_owner"]
        || inputs.candidate_spec.is_empty()
        || inputs.parent_spec.is_empty()
        || inputs.owner_spec.is_empty()
        || inputs.prompt.is_empty()
        || request.child_spec_sha256 != digest(inputs.candidate_spec)
        || request.parent_spec_sha256 != digest(inputs.parent_spec)
        || request.owner_spec_sha256 != digest(inputs.owner_spec)
        || request.prompt_sha256 != digest(inputs.prompt)
        || !sha(&request.writer_code_head)
        || !sha(&request.delivered_source_head)
        || !scope_ok(&request.writer_scope)
        || !scope_ok(&request.candidate_scope)
        || scopes_overlap(&request.writer_scope, &request.candidate_scope)
        || !String::from_utf8_lossy(inputs.owner_spec).contains(&request.writer_scope)
        || !String::from_utf8_lossy(inputs.candidate_spec).contains(&request.candidate_scope)
        || request.delivery_marker.is_empty()
        || !s(root, "note").contains(&request.delivery_marker)
        || !s(root, "note").contains(&request.writer_code_head)
        || !s(root, "note").contains(&request.delivered_source_head)
    {
        return Err("handoff requires the exact completed writer, delivered result, and unstarted candidate".into());
    }
    let child_identity = work_owner::from_job(candidate);
    let parent_identity = work_owner::from_job(parent);
    let writer_identity = work_owner::from_job(owner);
    if !["repository", "git_common"].iter().all(|key| {
        !s(&child_identity, key).is_empty()
            && child_identity[*key] == parent_identity[*key]
            && child_identity[*key] == writer_identity[*key]
    }) || !child_identity["pr"].is_u64()
        || s(&child_identity, "worktree").is_empty()
        || s(&parent_identity, "worktree").is_empty()
        || s(&writer_identity, "worktree").is_empty()
        || child_identity["worktree"] == parent_identity["worktree"]
        || child_identity["worktree"] == writer_identity["worktree"]
        || parent_identity["worktree"] == writer_identity["worktree"]
        || child_identity["branch"] == parent_identity["branch"]
        || child_identity["branch"] == writer_identity["branch"]
    {
        return Err(
            "handoff needs matching registered repository ownership and separate worktrees".into(),
        );
    }
    let mut next = candidate.clone();
    if !next["work_wait_history"].is_array() {
        next["work_wait_history"] = json!([]);
    }
    next["work_wait_history"]
        .as_array_mut()
        .unwrap()
        .push(json!({
            "reason":"finished_work_handoff",
            "request":request,
            "wait_on":candidate["work_wait_on"],
            "evidence":candidate["work_wait_evidence"],
            "queue_wait_reason":candidate["queue_wait_reason"],
            "owner_finished_at":owner["finished_at"],
            "delivery_marker":request.delivery_marker
        }));
    next["work_wait_on"] = json!([]);
    next["work_wait_evidence"] = Value::Null;
    next["queue_wait_reason"] = Value::Null;
    Ok(next)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    struct Fixture {
        db: Value,
        candidate: Value,
        parent: Value,
        owner: Value,
        request: Request,
        inputs: Inputs<'static>,
    }

    #[test]
    fn scope_boundaries_are_segment_aware() {
        assert!(scope_ok("tools/product-ci"));
        assert!(!scope_ok("../tools/product-ci"));
        assert!(!scope_ok("tools\\product-ci"));
        assert!(scopes_overlap("tools/x", "tools/x/sub"));
        assert!(!scopes_overlap("tools/x", "tools/xy"));
    }

    fn fixture() -> Fixture {
        let child_spec = b"Implement tools/product-ci module";
        let parent_spec = b"Parent integrates child result";
        let owner_spec = b"Implement tools/pr148-windows-runner module";
        let prompt = b"Original finite implementation task";
        let child_identity = json!({"repository":"org/repo","git_common":"common","worktree":"child","branch":"child-branch","pr":148});
        let parent_identity = json!({"repository":"org/repo","git_common":"common","worktree":"parent","branch":"parent-branch","pr":148});
        let owner_identity = json!({"repository":"org/repo","git_common":"common","worktree":"writer","branch":"writer-branch","pr":148});
        let mut candidate = json!({"id":"candidate","todo_id":3,"status":"queued","started_at":null,"thread_id":null,"codex_pid":null,"steps":0,"parent_job_id":"","launch_key":"key","work_owner":child_identity,"work_wait_on":[2],"queue_wait_reason":{"code":"todo_dependency","wait_on":[2]},"subwork":null,"related_todo_ids":[]});
        let owner = json!({"id":"writer","todo_id":2,"status":"succeeded","exit_code":0,"finished_at":"now","work_owner":owner_identity,"cwd":"writer"});
        candidate["work_wait_evidence"] = json!({"owner_job_id":"writer","owner":work_owner::owner_evidence(&owner),"candidate_identity":child_identity});
        let parent = json!({"id":"parent","todo_id":1,"status":"succeeded","exit_code":0,"finished_at":"now","work_owner":parent_identity,"cwd":"parent"});
        let db = json!({"items":[
            {"id":1,"status":"wait","wait_on":[3],"jobs":["parent"],"note":"delivery marker 1234567890abcdef1234567890abcdef12345678 abcdef1234567890abcdef1234567890abcdef12","spec":"parent-spec"},
            {"id":2,"status":"wait","wait_on":[9],"jobs":["writer"],"spec":"owner-spec"},
            {"id":3,"status":"doing","dispatchable":true,"wait_on":[],"jobs":["candidate"],"scheduler":{"key":"key"},"spec":"child-spec"},
            {"id":9,"status":"wait","wait_on":[],"jobs":[]}
        ]});
        let request = Request {
            job_id: "candidate".into(),
            todo_id: 3,
            parent_job_id: "parent".into(),
            parent_todo_id: 1,
            owner_job_id: "writer".into(),
            owner_todo_id: 2,
            child_spec_sha256: digest(child_spec),
            parent_spec_sha256: digest(parent_spec),
            owner_spec_sha256: digest(owner_spec),
            prompt_sha256: digest(prompt),
            writer_code_head: "1234567890abcdef1234567890abcdef12345678".into(),
            delivered_source_head: "abcdef1234567890abcdef1234567890abcdef12".into(),
            writer_scope: "tools/pr148-windows-runner".into(),
            candidate_scope: "tools/product-ci".into(),
            delivery_marker: "delivery marker".into(),
        };
        Fixture {
            db,
            candidate,
            parent,
            owner,
            request,
            inputs: Inputs {
                candidate_spec: child_spec,
                parent_spec,
                owner_spec,
                prompt,
            },
        }
    }

    #[test]
    fn removes_only_a_delivered_completed_writer_edge() {
        let fixture = fixture();
        let next = plan(
            &fixture.db,
            &fixture.candidate,
            &fixture.parent,
            &fixture.owner,
            &fixture.request,
            fixture.inputs,
        )
        .unwrap();
        assert_eq!(next["work_wait_on"], json!([]));
        assert_eq!(
            next["work_wait_history"][0]["reason"],
            "finished_work_handoff"
        );
        assert_eq!(
            next["work_wait_history"][0]["evidence"],
            fixture.candidate["work_wait_evidence"]
        );
        assert_eq!(fixture.candidate["work_wait_on"], json!([2]));
    }

    #[test]
    fn refuses_missing_delivery_and_manual_or_active_edges() {
        for case in [
            "missing_delivery",
            "writer_live",
            "manual_child_wait",
            "wrong_edge",
            "overlap",
        ] {
            let mut fixture = fixture();
            match case {
                "missing_delivery" => fixture.db["items"][0]["note"] = json!("no delivery"),
                "writer_live" => fixture.owner["status"] = json!("running"),
                "manual_child_wait" => fixture.db["items"][2]["wait_on"] = json!([2]),
                "wrong_edge" => {
                    fixture.candidate["work_wait_evidence"]["owner_job_id"] = json!("other")
                }
                "overlap" => fixture.request.candidate_scope = fixture.request.writer_scope.clone(),
                _ => unreachable!(),
            }
            assert!(
                plan(
                    &fixture.db,
                    &fixture.candidate,
                    &fixture.parent,
                    &fixture.owner,
                    &fixture.request,
                    fixture.inputs
                )
                .is_err(),
                "{case}"
            );
        }
    }
}
