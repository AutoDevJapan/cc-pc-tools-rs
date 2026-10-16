//! Offline transaction checks: never accesses the real original product or launches a model.
use cc_job_policy::product_retry as p;
use cc_todo::product_retry::{bind, stage};
use serde_json::{Value, json};
const T: u64 = 1792108800;
fn fixture() -> (Value, Value, Value, String) {
    let db = json!({"projects":{"PhosGaea-Vol1":"fixture-repo"},"items":[{"id":1626,"project":"PhosGaea-Vol1","status":"wait","dispatchable":false,
        "spec":"fixture-spec.md","jobs":["source"],"needs":[],"resume_generation":9,"worker_receipt":{"job":"source","generation":9},
        "scheduler":{"job":"source","worktree":p::TREE,"branch":"scheduler/todo-1626-1"},
        "wait_ready":{"condition":"retry-time","since":T-120,"not_before":T-60},"progress":40,"note":"Keep previous work."}]});
    let proof = json!({"id":1626,"verified":true,"session_receipt_verified":true,"session":p::QUALIFICATION_SESSION,
        "job":p::QUALIFICATION_JOB,"request_sha256":p::REQUEST_HASH,"request":"fixture-original.md","worktree":p::TREE,
        "expires_at":"2026-10-21T00:00:00Z","snapshot":cc_todo::product_resume::snapshot(&db,&db["items"][0])});
    let source = json!({"id":"source","todo_id":1626,"status":"succeeded","retry_live":false,"cwd":p::TREE,
        "thread_id":"22222222-2222-4222-8222-222222222222","model":"gpt-6.1-sol","effort":"high","sandbox":"danger-full-access",
        "mcp_profile":"browser","network":false,"add_dirs":[],"scope_source":"launcher"});
    let key = p::key(&db["items"][0]["wait_ready"]).unwrap();
    (db, proof, source, key)
}
fn reserve(db: &mut Value, proof: &Value, source: &Value, key: &str) -> Result<Value, String> {
    stage(
        db,
        proof,
        source,
        key,
        "fixture-prompt.md",
        &p::digest(b"continue"),
        T,
    )
}
fn queued(db: &Value, key: &str) -> Value {
    let mut job = json!({"id":"accepted","todo_id":1626,"status":"queued","started_at":null,"codex_pid":null,
        "launch_key":key,"resume_session":"22222222-2222-4222-8222-222222222222","cwd":p::TREE,"prompt_source":"fixture-prompt.md",
        "model":"gpt-6.1-sol","requested_model":"gpt-6.1-sol","effort":"high","sandbox":"danger-full-access","mcp_profile":"browser",
        "network":false,"add_dirs":[],"scope_source":"launcher"});
    job["original_product_retry"] =
        p::registration(db, &job, key, &p::digest(b"continue"), T).unwrap();
    job
}
#[test]
fn reservation_keeps_wait_and_latest_session_until_exact_queue_binding() {
    let (mut db, proof, source, key) = fixture();
    let before = db.clone();
    let receipt = reserve(&mut db, &proof, &source, &key).unwrap();
    assert_eq!(receipt["launched"], 0);
    assert_eq!(receipt["wait_preserved"], true);
    assert_eq!(
        db["items"][0]["wait_ready"],
        before["items"][0]["wait_ready"]
    );
    assert_eq!(db["items"][0]["status"], "wait");
    assert!(
        receipt["run_args"]
            .as_array()
            .unwrap()
            .contains(&source["thread_id"])
    );
    assert!(
        !receipt["run_args"]
            .as_array()
            .unwrap()
            .contains(&json!(p::QUALIFICATION_SESSION))
    );
    let reserved = db.clone();
    assert_eq!(
        reserve(&mut db, &proof, &source, &key).unwrap()["saved"],
        false
    );
    assert_eq!(db, reserved);
    let job = queued(&db, &key);
    assert_eq!(
        p::admission_reason(&db, &job, T).unwrap()["code"],
        "product_retry_unbound"
    );
    assert!(bind(&mut db, &job, &key, T).is_ok());
    assert!(db["items"][0]["wait_ready"].is_null());
    assert_eq!(db["items"][0]["status"], "doing");
    assert_eq!(db["items"][0]["dispatchable"], false);
    assert_eq!(db["items"][0]["progress"], 40);
    assert_eq!(db["items"][0]["note"], before["items"][0]["note"]);
    assert_eq!(
        db["items"][0]["product_retry_intent"]["snapshot"]["wait_ready"],
        before["items"][0]["wait_ready"]
    );
    assert!(p::admission_reason(&db, &job, T).is_none());
    let bound = db.clone();
    assert_eq!(bind(&mut db, &job, &key, T).unwrap()["reused"], true);
    assert_eq!(db, bound);
}
#[test]
fn new_owner_stops_waits_or_spec_changes_survive_a_lost_queue_response() {
    for gate in [
        "block",
        "stop",
        "question",
        "dependency",
        "new_wait",
        "spec",
        "generation",
    ] {
        let (mut db, proof, source, key) = fixture();
        reserve(&mut db, &proof, &source, &key).unwrap();
        let job = queued(&db, &key);
        match gate {
            "block" => db["items"][0]["status"] = json!("block"),
            "stop" => db["items"][0]["stopped"] = json!(true),
            "question" => db["items"][0]["decision_wait"] = json!({"question":"pending"}),
            "dependency" => {
                db["items"][0]["wait_on"] = json!([99]);
                db["items"]
                    .as_array_mut()
                    .unwrap()
                    .push(json!({"id":99,"status":"todo"}));
            }
            "new_wait" => db["items"][0]["wait_ready"]["not_before"] = json!(T + 300),
            "spec" => db["items"][0]["spec"] = json!("different.md"),
            "generation" => db["items"][0]["resume_generation"] = json!(10),
            _ => unreachable!(),
        }
        let before = db.clone();
        assert!(bind(&mut db, &job, &key, T).is_err(), "{gate}");
        assert_eq!(db, before, "{gate}");
        assert!(p::admission_reason(&db, &job, T).is_some(), "{gate}");
    }
}
#[test]
fn changed_source_qualification_or_receipt_never_creates_an_intent() {
    for gate in [
        "expired",
        "qualification",
        "snapshot",
        "worker",
        "live",
        "session",
        "sandbox",
        "model",
        "generation",
        "history",
    ] {
        let (mut db, mut proof, mut source, key) = fixture();
        match gate {
            "expired" => proof["expires_at"] = json!("2026-10-15T00:00:00Z"),
            "qualification" => proof["session"] = source["thread_id"].clone(),
            "snapshot" => proof["snapshot"]["status"] = json!("todo"),
            "worker" => source["todo_id"] = json!(99),
            "live" => source["retry_live"] = json!(true),
            "session" => source["thread_id"] = json!("not-a-session"),
            "sandbox" => source["sandbox"] = json!("unknown"),
            "model" => db["items"][0]["model"] = json!("explicit-other-model"),
            "generation" => db["items"][0]["worker_receipt"]["generation"] = json!(8),
            "history" => db["items"][0]["product_retry_history"] = json!("bad-history"),
            _ => unreachable!(),
        }
        let before = db.clone();
        assert!(reserve(&mut db, &proof, &source, &key).is_err(), "{gate}");
        assert_eq!(db, before, "{gate}");
    }
}
#[test]
fn queued_scope_prompt_key_and_owner_cancellation_are_not_trusted() {
    for gate in [
        "todo",
        "related",
        "parent",
        "session",
        "cwd",
        "sandbox",
        "key",
        "marker",
        "cancelled",
    ] {
        let (mut db, proof, source, key) = fixture();
        reserve(&mut db, &proof, &source, &key).unwrap();
        let mut job = queued(&db, &key);
        match gate {
            "todo" => job["todo_id"] = json!(99),
            "related" => job["related_todo_ids"] = json!([99]),
            "parent" => job["parent_job_id"] = json!("other"),
            "session" => job["resume_session"] = json!(p::QUALIFICATION_SESSION),
            "cwd" => job["cwd"] = json!("elsewhere"),
            "sandbox" => job["sandbox"] = json!("workspace-write"),
            "key" => job["launch_key"] = json!("b".repeat(32)),
            "marker" => job["original_product_retry"]["request_sha256"] = json!("other"),
            "cancelled" => job["cancel_requested"] = json!(true),
            _ => unreachable!(),
        }
        let before = db.clone();
        assert!(bind(&mut db, &job, &key, T).is_err(), "{gate}");
        assert_eq!(db, before, "{gate}");
    }
    let (db, proof, source, key) = fixture();
    let mut db2 = db.clone();
    reserve(&mut db2, &proof, &source, &key).unwrap();
    let mut job = queued(&db2, &key);
    job.as_object_mut()
        .unwrap()
        .remove("original_product_retry");
    assert!(p::admission_reason(&db, &job, T).is_none());
}
