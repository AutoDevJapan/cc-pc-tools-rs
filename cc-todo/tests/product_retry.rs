//! Offline transaction checks: never accesses the real original product or launches a model.
use cc_job_policy::product_retry as p;
use cc_todo::product_retry::{attempt_plan, bind, continue_queued, stage, stage_attempt};
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
fn terminal_unbound_attempt_can_be_replaced_without_changing_the_wait() {
    let (mut db, mut proof, source, base) = fixture();
    reserve(&mut db, &proof, &source, &base).unwrap();
    let original_wait = db["items"][0]["wait_ready"].clone();
    db["items"][0]["stopped"] = json!(true);
    let stopped = queued_unchecked(&db, &base);
    assert_eq!(
        p::admission_reason(&db, &stopped, T).unwrap()["terminal"],
        true
    );
    db["items"][0]["stopped"] = json!(false);
    for ordinal in 1..=2 {
        proof["snapshot"] = cc_todo::product_resume::snapshot(&db, &db["items"][0]);
        let plan = attempt_plan(&db["items"][0], &base, true).unwrap();
        assert_eq!(plan.attempt, ordinal);
        assert_ne!(plan.key, base);
        let receipt = stage_attempt(
            &mut db,
            &proof,
            &source,
            &plan,
            "fixture-prompt.md",
            &p::digest(b"continue"),
            T,
        )
        .unwrap();
        assert_eq!(receipt["key"], plan.key);
        assert_eq!(receipt["base_key"], base);
        assert_eq!(db["items"][0]["wait_ready"], original_wait);
        let next = queued(&db, &plan.key);
        assert_eq!(next["original_product_retry"]["key"], plan.key);
    }
    let before = db.clone();
    assert!(attempt_plan(&db["items"][0], &base, true).is_err());
    assert_eq!(db, before);
    assert_eq!(
        db["items"][0]["product_retry_history"]
            .as_array()
            .unwrap()
            .len(),
        2
    );
}
fn queued_unchecked(db: &Value, key: &str) -> Value {
    let mut job = json!({"id":"old-accepted","todo_id":1626,"launch_key":key,"resume_session":"22222222-2222-4222-8222-222222222222","cwd":p::TREE,
        "prompt_source":"fixture-prompt.md","requested_model":"gpt-6.1-sol","effort":"high","sandbox":"danger-full-access","mcp_profile":"browser","network":false,"add_dirs":[],"scope_source":"launcher"});
    job["original_product_retry"] = p::marker(&db["items"][0]["product_retry_intent"]);
    job
}

#[test]
fn normal_context_rotation_and_second_readiness_wait_keep_the_bound_job() {
    let (mut db, proof, source, key) = fixture();
    reserve(&mut db, &proof, &source, &key).unwrap();
    let mut job = queued(&db, &key);
    bind(&mut db, &job, &key, T).unwrap();
    job["first_started_at"] = json!("2026-10-16T00:00:00Z");
    job["resume_session"] = Value::Null;
    job["context_phase"] = json!("continue");
    job["model"] = json!("anthropic/claude-sonnet-5-5");
    assert!(p::admission_reason(&db, &job, T).is_none());
    db["items"][0]["status"] = json!("wait");
    db["items"][0]["wait_ready"] = json!({"condition":"retry-time","since":T-60,"not_before":T});
    let next_key = p::key(&db["items"][0]["wait_ready"]).unwrap();
    assert_ne!(key, next_key);
    assert_eq!(
        p::admission_reason(&db, &job, T).unwrap()["terminal"],
        false
    );
    let before = db.clone();
    assert!(continue_queued(&mut db, &job, &next_key, T - 1).is_err());
    assert_eq!(db, before);
    let generation = db["items"][0]["resume_generation"].clone();
    assert_eq!(
        continue_queued(&mut db, &job, &next_key, T).unwrap()["continued_existing"],
        true
    );
    assert_eq!(db["items"][0]["resume_generation"], generation);
    assert_eq!(db["items"][0]["scheduler"]["job"], job["id"]);
    assert_eq!(db["items"][0]["product_retry_intent"]["key"], key);
    assert!(p::admission_reason(&db, &job, T).is_none());
    assert_eq!(
        continue_queued(&mut db, &job, &next_key, T).unwrap()["saved"],
        false
    );
    let mut stopped = db.clone();
    stopped["items"][0]["status"] = json!("block");
    assert_eq!(
        p::admission_reason(&stopped, &job, T).unwrap()["terminal"],
        true
    );
}

#[test]
fn ordinary_dependency_completion_reuses_the_started_worker() {
    let (mut db, proof, source, key) = fixture();
    reserve(&mut db, &proof, &source, &key).unwrap();
    let mut job = queued(&db, &key);
    bind(&mut db, &job, &key, T).unwrap();
    job["first_started_at"] = json!("2026-10-16T00:00:00Z");
    job["resume_session"] = json!("33333333-3333-4333-8333-333333333333");
    db["items"][0]["status"] = json!("wait");
    db["items"][0]["wait_on"] = json!([99]);
    db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":99,"status":"todo"}));
    assert_eq!(
        p::admission_reason(&db, &job, T).unwrap()["terminal"],
        false
    );
    let before = db.clone();
    assert!(p::mark_continuation_running(&mut db, &job, T).is_err());
    assert_eq!(db, before);
    db["items"][1]["status"] = json!("done");
    assert!(p::admission_reason(&db, &job, T).is_none());
    assert!(p::mark_continuation_running(&mut db, &job, T).unwrap());
    assert_eq!(db["items"][0]["status"], "doing");
    assert_eq!(db["items"][0]["scheduler"]["job"], job["id"]);
    assert_eq!(db["items"][0]["resume_generation"], 10);
}

#[test]
fn a_lost_receipt_cannot_reuse_changed_source_settings() {
    for field in [
        "model",
        "effort",
        "sandbox",
        "mcp_profile",
        "thread_id",
        "network",
        "add_dirs",
    ] {
        let (mut db, proof, mut source, key) = fixture();
        reserve(&mut db, &proof, &source, &key).unwrap();
        match field {
            "thread_id" => source[field] = json!("33333333-3333-4333-8333-333333333333"),
            "network" => source[field] = json!(true),
            "add_dirs" => source[field] = json!(["another-location"]),
            "sandbox" => source[field] = json!("read-only"),
            _ => source[field] = json!("different"),
        }
        let before = db.clone();
        assert!(reserve(&mut db, &proof, &source, &key).is_err(), "{field}");
        assert_eq!(db, before, "{field}");
    }
}

#[test]
fn an_ambiguous_dependency_is_not_a_completed_dependency() {
    let (mut db, mut proof, source, key) = fixture();
    db["items"][0]["wait_on"] = json!([99]);
    db["items"].as_array_mut().unwrap().extend([
        json!({"id":99,"status":"done"}),
        json!({"id":99,"status":"todo"}),
    ]);
    proof["snapshot"] = cc_todo::product_resume::snapshot(&db, &db["items"][0]);
    let before = db.clone();
    assert!(reserve(&mut db, &proof, &source, &key).is_err());
    assert_eq!(db, before);
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
