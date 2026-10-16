//! Queue-side acceptance of an already reserved intent. The reserve API's
//! production-only source discovery is tested by cc-todo; all files here stay
//! in the fixture, including the source checkout recorded in that intent.
use super::*;
use cc_job_policy::product_retry as policy;

const SOURCE_SESSION: &str = "12345678-1234-4321-9234-123456789abc";
const BODY: &[u8] = b"Continue only the saved original task and session.\n";

fn fixture() -> (Test, Value, String) {
    let t = Test::new();
    let at = parse_iso("2026-10-16T03:00:00Z").unwrap() as u64;
    *t.rt.epoch_base.lock().unwrap() = Some(at as f64);
    fs::write(
        t.ctx.root.join("queue-config.json"),
        r#"{"max_running":10}"#,
    )
    .unwrap();
    fs::write(t.ctx.root.join("prompt.md"), BODY).unwrap();
    let mut row = json!({"id":1626,"project":"PhosGaea-Vol1","spec":"saved-original-spec",
        "status":"wait","dispatchable":false,"needs":[],"jobs":["source-old"],
        "resume_generation":7,"worker_receipt":{"job":"source-old","generation":7},
        "scheduler":{"job":"source-old","worktree":policy::TREE,"branch":"original"},
        "wait_ready":{"condition":"retry-time","since":at-120,"not_before":at-60}});
    let key = policy::key(&row["wait_ready"]).unwrap();
    let snapshot = policy::snapshot(&row);
    row["product_retry_intent"] = json!({"version":1,"key":key,"reserved_at":at,
        "snapshot":snapshot,"proof":{"id":1626,"verified":true,"session_receipt_verified":true,
            "session":policy::QUALIFICATION_SESSION,"job":policy::QUALIFICATION_JOB,
            "request_sha256":policy::REQUEST_HASH,"worktree":policy::TREE,
            "expires_at":"2026-10-21T00:00:00Z","snapshot":snapshot},
        "source":{"id":"source-old","session":SOURCE_SESSION,"cwd":t.ctx.root,
            "model":"m","effort":"e","sandbox":"read-only","mcp_profile":"minimal",
            "scope_source":"launcher","network":false,"add_dirs":[]},
        "prompt_file":t.ctx.root.join("prompt.md"),"prompt_sha256":policy::digest(BODY),"bound_job":null});
    let db = json!({"items":[row]});
    save_db(&t, &db);
    (t, db, key)
}

fn save_db(t: &Test, db: &Value) {
    fs::write(
        t.ctx.root.join("todo.json"),
        serde_json::to_vec(db).unwrap(),
    )
    .unwrap();
}

fn submit(t: &Test, key: &str, extra: &[&str], body: &[u8]) -> Result<i32> {
    let mut args = vec![
        "--todo-id",
        "1626",
        "--launch-key",
        key,
        "--original-product-retry-key",
        key,
        "--resume-session",
        SOURCE_SESSION,
    ];
    args.extend(extra);
    t.run(&args, body)
}

fn accepted(t: &Test) -> Value {
    let jobs = t.ctx.list_jobs(&HashSet::new());
    let jobs: Vec<_> = jobs
        .into_iter()
        .filter(|j| j.get("original_product_retry").is_some())
        .collect();
    assert_eq!(jobs.len(), 1);
    jobs[0].clone()
}

fn bind(t: &Test, db: &mut Value, key: &str) -> Value {
    let job = accepted(t);
    cc_todo::product_retry::bind(db, &job, key, t.rt.epoch() as u64).unwrap();
    save_db(t, db);
    job
}

fn report_reason(t: &Test, job: &Value) -> Value {
    let report = t
        .ctx
        .queue_report(&t.ctx.list_jobs(&HashSet::new()))
        .unwrap();
    report["queued"]
        .as_array()
        .unwrap()
        .iter()
        .find(|entry| entry["id"] == job["id"])
        .unwrap()["reason"]
        .clone()
}

#[test]
fn reserve_receipt_arguments_pass_the_real_jobs_parser() {
    let (t, mut db, base) = fixture();
    db["items"][0].as_object_mut().unwrap().remove("product_retry_intent");
    let source = json!({"id":"source-old","todo_id":1626,"status":"succeeded",
        "retry_live":false,"cwd":policy::TREE,"thread_id":SOURCE_SESSION,
        "model":"m","effort":"e","sandbox":"read-only","mcp_profile":"minimal",
        "network":false,"add_dirs":[],"scope_source":"launcher"});
    let proof = json!({"id":1626,"verified":true,"session_receipt_verified":true,
        "session":policy::QUALIFICATION_SESSION,"job":policy::QUALIFICATION_JOB,
        "request_sha256":policy::REQUEST_HASH,"worktree":policy::TREE,
        "expires_at":"2026-10-21T00:00:00Z",
        "snapshot":cc_todo::product_resume::snapshot(&db,&db["items"][0])});
    let receipt = cc_todo::product_retry::stage(
        &mut db, &proof, &source, &base, t.ctx.root.join("prompt.md").to_str().unwrap(),
        &policy::digest(BODY), t.rt.epoch() as u64,
    ).unwrap();
    let args: Vec<String> = receipt["run_args"].as_array().unwrap().iter()
        .map(|value| value.as_str().unwrap().to_owned()).collect();
    let parsed = cc_jobs::cli::parse(&args).unwrap();
    assert_eq!(parsed.cmd, "run");
    assert_eq!(parsed.value("todo-id"), Some("1626"));
    assert_eq!(parsed.value("launch-key"), receipt["key"].as_str());
    assert_eq!(parsed.value("original-product-retry-key"), receipt["key"].as_str());
    assert_eq!(parsed.value("resume-session"), Some(SOURCE_SESSION));
    assert_eq!(parsed.value("cwd"), Some(policy::TREE));
    assert_eq!(parsed.value("sandbox"), Some("read-only"));
    assert_eq!(parsed.value("scope-source"), Some("launcher"));
    assert_eq!(receipt["launched"], 0);
    assert!(db["items"][0]["wait_ready"].is_object());
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
}

#[test]
fn markerless_jobs_keep_the_ordinary_wait_and_admission_behavior() {
    let (t, original, _) = fixture();
    assert!(t.run(&["--todo-id", "1626"], BODY).is_err());
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    assert_eq!(
        t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(),
        original
    );
    t.run(&[], b"unrelated ordinary work").unwrap();
    let job = t.ctx.list_jobs(&HashSet::new())[0].clone();
    assert!(job.get("original_product_retry").is_none());
    assert!(t.ctx.admit(s(&job, "id")).unwrap().is_some());
}

#[test]
fn intake_preserves_wait_and_exact_prompt_until_canonical_bind() {
    let (t, mut db, key) = fixture();
    submit(&t, &key, &[], BODY).unwrap();
    let job = accepted(&t);
    assert_eq!(job["requested_model"], "m");
    assert_eq!(job["resume_session"], SOURCE_SESSION);
    assert_ne!(job["resume_session"], policy::QUALIFICATION_SESSION);
    assert_eq!(
        fs::read(t.ctx.job_dir(s(&job, "id")).join("prompt.md")).unwrap(),
        BODY
    );
    assert_eq!(t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(), db);
    assert_eq!(report_reason(&t, &job)["code"], "product_retry_unbound");
    assert!(t.ctx.admit(s(&job, "id")).unwrap().is_none());
    assert_eq!(t.ctx.load(s(&job, "id")).unwrap()["status"], "queued");
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    bind(&t, &mut db, &key);
    assert!(db["items"][0]["wait_ready"].is_null());
    assert_eq!(db["items"][0]["dispatchable"], false);
    submit(&t, &key, &[], BODY).unwrap();
    assert_eq!(accepted(&t)["id"], job["id"]);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert!(t.ctx.admit(s(&job, "id")).unwrap().is_some());
    assert_eq!(t.ctx.load(s(&job, "id")).unwrap()["status"], "running");
}

#[test]
fn supervisor_waits_for_bind_then_launches_exactly_one_model() {
    let (t, original, key) = fixture();
    submit(&t, &key, &[], BODY).unwrap();
    let job = accepted(&t);
    let store = t.ctx.root.join("todo.json");
    let job_file = t.ctx.job_dir(s(&job, "id")).join("job.json");
    let queue_lock = t.ctx.jobs.join(".queue.lock");
    let seen = Arc::new(Mutex::new(None));
    let observed = seen.clone();
    let rt = Arc::downgrade(&t.rt);
    *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
        if seconds != 3. || observed.lock().unwrap().is_some() {
            return;
        }
        let rt = rt.upgrade().unwrap();
        let _queue = FileLock::acquire(&queue_lock).unwrap();
        let _state = FileLock::acquire(&job_file.with_file_name("state.lock")).unwrap();
        let _todo = FileLock::acquire(&store.with_file_name("todo.lock")).unwrap();
        let mut db: Value = serde_json::from_slice(&fs::read(&store).unwrap()).unwrap();
        let job: Value = serde_json::from_slice(&fs::read(&job_file).unwrap()).unwrap();
        *observed.lock().unwrap() = Some((
            rt.spawns.load(Ordering::Relaxed),
            db == original,
            job["queue_wait_reason"].clone(),
        ));
        cc_todo::product_retry::bind(&mut db, &job, &key, rt.epoch() as u64).unwrap();
        fs::write(&store, serde_json::to_vec(&db).unwrap()).unwrap();
    }));
    t.ctx.supervise(s(&job, "id")).unwrap();
    *t.rt.sleep_hook.lock().unwrap() = None;
    let observed = seen
        .lock()
        .unwrap()
        .clone()
        .expect("supervisor actually polled the unbound job");
    assert_eq!(observed.0, 0);
    assert!(observed.1);
    assert_eq!(observed.2["code"], "product_retry_unbound");
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    assert_eq!(t.ctx.load(s(&job, "id")).unwrap()["status"], "succeeded");
}

#[test]
fn running_retry_preserves_other_todo_values_and_canonical_snapshot_bytes() {
    let (t, mut db, key) = fixture();
    let other: Value = serde_json::from_str(
        r#"{"id":41,"status":"todo","note":"token: fixture-value","licenseKey":"fixture-value","Key":"first","key":"second","未知":{"z":"日本語🦀","a":true}}"#,
    )
    .unwrap();
    db["items"].as_array_mut().unwrap().push(other.clone());
    db["unknownRoot"] = json!({"note":"token: fixture-value","licenseKey":"fixture-value"});
    save_db(&t, &db);
    submit(&t, &key, &[], BODY).unwrap();
    let job = bind(&t, &mut db, &key);
    // Bind already marks the row doing. A completed dependency must cause
    // admission to save the ToDo again, exercising the actual writer branch.
    db["items"][0]["status"] = json!("wait");
    db["items"][0]["wait_on"] = json!([42]);
    db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":42,"status":"done"}));
    save_db(&t, &db);
    let store = t.ctx.root.join("todo.json");
    let before = fs::read(&store).unwrap();
    let backup = store.with_file_name("todo.json.bak-20260930d");
    assert!(!backup.exists());

    let lease = t.ctx.admit(s(&job, "id")).unwrap();
    assert!(lease.is_some());
    drop(lease);
    assert_eq!(t.ctx.load(s(&job, "id")).unwrap()["status"], "running");
    let bytes = fs::read(&store).unwrap();
    let after = cc_job_policy::todo_store::read(&store).unwrap();
    assert_eq!(after["items"][0]["status"], "doing");
    assert!(after["items"][0]["wait_on"].is_null());
    assert_eq!(after["items"][1], other);
    assert_eq!(after["unknownRoot"], db["unknownRoot"]);
    let unchanged_row = r#"  {
   "id": 41,
   "status": "todo",
   "note": "token: fixture-value",
   "licenseKey": "fixture-value",
   "Key": "first",
   "key": "second",
   "未知": {
    "z": "日本語🦀",
    "a": true
   }
  }"#;
    assert!(
        bytes
            .windows(unchanged_row.len())
            .any(|window| window == unchanged_row.as_bytes()),
        "the unrelated row keeps its keys, order, Unicode and literal values"
    );
    assert!(
        std::str::from_utf8(&bytes)
            .unwrap()
            .starts_with("{\n \"items\": [\n")
    );
    assert!(!bytes.contains(&b'\r'));
    assert_eq!(fs::read(&backup).unwrap(), before);
    assert!(
        !store
            .with_file_name(format!("todo.json.{}.tmp", std::process::id()))
            .exists()
    );
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
}

#[test]
fn running_retry_save_failure_keeps_canonical_todo_and_does_not_publish_running() {
    let (t, mut db, key) = fixture();
    submit(&t, &key, &[], BODY).unwrap();
    let job = bind(&t, &mut db, &key);
    db["items"][0]["status"] = json!("wait");
    db["items"][0]["wait_on"] = json!([42]);
    db["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":42,"status":"done"}));
    save_db(&t, &db);
    let store = t.ctx.root.join("todo.json");
    let original = fs::read(&store).unwrap();
    let backup = store.with_file_name("todo.json.bak-20260930d");
    let tmp = store.with_file_name(format!("todo.json.{}.tmp", std::process::id()));
    fs::create_dir(&tmp).unwrap();
    fs::write(tmp.join("pending"), b"pending fixture bytes").unwrap();

    let error = t.ctx.admit(s(&job, "id")).err().expect("save must fail");
    assert!(error.message.contains("todo_store_io:"), "{error:?}");
    assert_eq!(fs::read(&store).unwrap(), original);
    assert_eq!(fs::read(&backup).unwrap(), original);
    assert_eq!(
        fs::read(tmp.join("pending")).unwrap(),
        b"pending fixture bytes"
    );
    let saved = t.ctx.load(s(&job, "id")).unwrap();
    assert_eq!(saved["status"], "queued");
    assert!(saved["started_at"].is_null());
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
}

#[test]
fn parser_rejects_other_todos_invalid_keys_and_relation_spoofing() {
    let (t, original, key) = fixture();
    for extra in [
        vec!["--todo-id", "99"],
        vec!["--original-product-retry-key", "ABC"],
        vec!["--launch-key", "00000000000000000000000000000000"],
        vec!["--related-todo-ids", "99"],
        vec!["--related-todo-ids", "1626"],
        vec!["--parent-job-id", "owner"],
        vec!["--subwork-of", "99"],
        vec!["--assigned-needs", "cargo"],
        vec!["--project", "another-product"],
        vec!["--pr-url", "https://github.com/AutoDevJapan/example/pull/1"],
    ] {
        assert!(submit(&t, &key, &extra, BODY).is_err(), "{extra:?}");
    }
    assert!(
        t.run(&["--original-product-retry-key", &key], BODY)
            .is_err()
    );
    assert!(t.ctx.list_jobs(&HashSet::new()).is_empty());
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    assert_eq!(
        t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(),
        original
    );
}

#[test]
fn registration_rejects_changed_source_session_cwd_key_prompt_and_scope() {
    for case in [
        "key", "prompt", "session", "cwd", "model", "effort", "sandbox", "dirs", "scope",
    ] {
        let (t, original, key) = fixture();
        let other_dir = t.ctx.root.join("other");
        fs::create_dir(&other_dir).unwrap();
        let extra = match case {
            "key" => vec![
                "--launch-key",
                "00000000000000000000000000000000",
                "--original-product-retry-key",
                "00000000000000000000000000000000",
            ],
            "session" => vec!["--resume-session", policy::QUALIFICATION_SESSION],
            "cwd" => vec!["--cwd", other_dir.to_str().unwrap()],
            "model" => vec!["--model", "different"],
            "effort" => vec!["--effort", "different"],
            "sandbox" => vec!["--sandbox", "danger-full-access"],
            "dirs" => vec!["--add-dir", other_dir.to_str().unwrap()],
            "scope" => vec!["--scope-source", "owner_request"],
            _ => vec![],
        };
        assert!(
            submit(
                &t,
                &key,
                &extra,
                if case == "prompt" { b"changed" } else { BODY }
            )
            .is_err(),
            "{case}"
        );
        assert!(t.ctx.list_jobs(&HashSet::new()).is_empty(), "{case}");
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{case}");
        assert_eq!(
            t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(),
            original,
            "{case}"
        );
    }
}

#[test]
fn registration_rejects_owner_stop_new_wait_dependency_and_expiry() {
    for case in ["stop", "block", "wait", "dependency", "expiry"] {
        let (t, mut db, key) = fixture();
        match case {
            "stop" => db["items"][0]["stopped"] = json!(true),
            "block" => db["items"][0]["status"] = json!("block"),
            "wait" => {
                db["items"][0]["wait_ready"]["not_before"] = json!(t.rt.epoch() as u64 + 3600)
            }
            "dependency" => {
                db["items"][0]["wait_on"] = json!([999]);
            }
            "expiry" => *t.rt.epoch_base.lock().unwrap() = Some(policy::LIMIT as f64),
            _ => unreachable!(),
        }
        save_db(&t, &db);
        assert!(submit(&t, &key, &[], BODY).is_err(), "{case}");
        assert!(t.ctx.list_jobs(&HashSet::new()).is_empty(), "{case}");
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{case}");
        assert_eq!(
            t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(),
            db,
            "{case}"
        );
    }
}

#[test]
fn bound_jobs_recheck_owner_stop_new_wait_dependency_expiry_and_identity() {
    for case in [
        "stop",
        "block",
        "wait",
        "dependency",
        "expiry",
        "key",
        "source",
        "cwd",
        "allocation",
        "prompt",
    ] {
        let (t, mut db, key) = fixture();
        submit(&t, &key, &[], BODY).unwrap();
        let job = bind(&t, &mut db, &key);
        match case {
            "stop" => db["items"][0]["stopped"] = json!(true),
            "block" => db["items"][0]["status"] = json!("block"),
            "wait" => {
                db["items"][0]["wait_ready"] =
                    json!({"condition":"retry-time","since":1,"not_before":61})
            }
            "dependency" => db["items"][0]["wait_on"] = json!([999]),
            "expiry" => *t.rt.epoch_base.lock().unwrap() = Some(policy::LIMIT as f64),
            "key" => {
                db["items"][0]["product_retry_intent"]["key"] =
                    json!("00000000000000000000000000000000")
            }
            "source" => {
                db["items"][0]["product_retry_intent"]["source"]["session"] =
                    json!(policy::QUALIFICATION_SESSION)
            }
            "cwd" => {
                db["items"][0]["product_retry_intent"]["source"]["cwd"] = json!("D:/changed-scope")
            }
            "allocation" => db["items"][0]["worker_receipt"]["job"] = json!("another-worker"),
            "prompt" => {
                fs::write(t.ctx.job_dir(s(&job, "id")).join("prompt.md"), b"changed").unwrap()
            }
            _ => unreachable!(),
        }
        save_db(&t, &db);
        let reason = report_reason(&t, &job);
        let terminal= !["wait","dependency"].contains(&case);
        assert_eq!(reason["terminal"], terminal, "{case}: {reason}");
        // Read-only status is not allowed to finalize a queued job.
        assert_eq!(
            t.ctx.load(s(&job, "id")).unwrap()["status"],
            "queued",
            "{case}"
        );
        assert!(t.ctx.admit(s(&job, "id")).unwrap().is_none(), "{case}");
        let saved = t.ctx.load(s(&job, "id")).unwrap();
        assert_eq!(saved["status"], if terminal {"failed"}else{"queued"}, "{case}");
        assert_eq!(saved["queue_wait_reason"], reason, "{case}");
        assert!(saved["started_at"].is_null(), "{case}");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0, "{case}");
        assert!(t.rt.kills.lock().unwrap().is_empty(), "{case}");
        assert_eq!(
            t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(),
            db,
            "{case}"
        );
    }
}

#[test]
fn completed_dependency_reuses_the_bound_queue_entry_and_marks_it_doing(){
    let(t,mut db,key)=fixture();submit(&t,&key,&[],BODY).unwrap();let job=bind(&t,&mut db,&key);
    db["items"][0]["status"]=json!("wait");db["items"][0]["wait_on"]=json!([999]);
    db["items"].as_array_mut().unwrap().push(json!({"id":999,"status":"todo"}));save_db(&t,&db);
    assert!(t.ctx.admit(s(&job,"id")).unwrap().is_none());
    assert_eq!(t.ctx.load(s(&job,"id")).unwrap()["status"],"queued");
    db["items"][1]["status"]=json!("done");save_db(&t,&db);
    let lease=t.ctx.admit(s(&job,"id")).unwrap();assert!(lease.is_some());drop(lease);
    let after=t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap();
    assert_eq!(after["items"][0]["status"],"doing");assert_eq!(after["items"][0]["worker_receipt"],db["items"][0]["worker_receipt"]);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed),1);assert_eq!(t.rt.spawns.load(Ordering::Relaxed),0);
}

#[test]
fn replay_recovers_the_existing_unbound_job_after_a_lost_receipt() {
    let (t, db, key) = fixture();
    submit(&t, &key, &[], BODY).unwrap();
    let job = accepted(&t);
    fs::remove_file(t.ctx.jobs.join(format!(".launch-{key}.json"))).unwrap();
    submit(&t, &key, &[], BODY).unwrap();
    assert_eq!(accepted(&t)["id"], job["id"]);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    assert_eq!(t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(), db);
}

#[test]
fn replay_recovers_intake_even_if_the_first_supervisor_spawn_failed() {
    let (t, db, key) = fixture();
    t.rt.supervisor_fail.store(true, Ordering::Relaxed);
    assert!(submit(&t, &key, &[], BODY).is_err());
    let job = accepted(&t);
    t.rt.supervisor_fail.store(false, Ordering::Relaxed);
    submit(&t, &key, &[], BODY).unwrap();
    assert_eq!(accepted(&t), job);
    // Normal reload owns controller recovery; a receipt replay never spawns.
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    assert_eq!(t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(), db);
    assert!(t.ctx.admit(s(&job, "id")).unwrap().is_none());
}

#[test]
fn owner_stop_after_a_ready_status_snapshot_wins_final_admission() {
    let (t, mut db, key) = fixture();
    t.rt.supervisor_pid
        .store(std::process::id(), Ordering::Relaxed);
    submit(&t, &key, &[], BODY).unwrap();
    let job = bind(&t, &mut db, &key);
    assert_eq!(report_reason(&t, &job)["code"], "ready");
    db["items"][0]["stopped"] = json!(true);
    {
        let _writer = FileLock::acquire(&t.ctx.root.join("todo.lock")).unwrap();
        save_db(&t, &db);
    }
    assert!(t.ctx.admit(s(&job, "id")).unwrap().is_none());
    let saved = t.ctx.load(s(&job, "id")).unwrap();
    assert_eq!(saved["status"], "failed");
    assert_eq!(
        saved["queue_wait_reason"]["code"],
        "product_retry_owner_stopped"
    );
    assert!(saved["started_at"].is_null());
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    assert!(t.rt.kills.lock().unwrap().is_empty());
}

#[test]
fn same_key_replay_rejects_body_and_scope_changes_without_another_supervisor() {
    let (t, db, key) = fixture();
    submit(&t, &key, &[], BODY).unwrap();
    let job = accepted(&t);
    submit(&t, &key, &[], BODY).unwrap();
    for extra in [
        vec!["--model", "different"],
        vec!["--sandbox", "danger-full-access"],
        vec!["--cwd", t.ctx.jobs.to_str().unwrap()],
        vec!["--resume-session", policy::QUALIFICATION_SESSION],
        vec!["--add-dir", t.ctx.root.to_str().unwrap()],
    ] {
        assert!(submit(&t, &key, &extra, BODY).is_err(), "{extra:?}");
    }
    assert!(submit(&t, &key, &[], b"different body").is_err());
    assert!(t.run(&["--launch-key", &key], BODY).is_err());
    assert_eq!(accepted(&t), job);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert_eq!(t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(), db);
}

#[test]
fn concurrent_replays_create_only_one_queued_job_and_one_supervisor() {
    let (t, _, key) = fixture();
    // Do not use Test::run here: every caller reads the same immutable prompt.
    let prompt = t.ctx.root.join("prompt.md");
    let args: Vec<String> = [
        "run",
        "--name",
        "x",
        "--model",
        "m",
        "--effort",
        "e",
        "--cwd",
        t.ctx.root.to_str().unwrap(),
        "--prompt-file",
        prompt.to_str().unwrap(),
        "--todo-id",
        "1626",
        "--launch-key",
        &key,
        "--original-product-retry-key",
        &key,
        "--resume-session",
        SOURCE_SESSION,
    ]
    .iter()
    .map(|s| (*s).to_owned())
    .collect();
    let barrier = std::sync::Barrier::new(4);
    std::thread::scope(|scope| {
        let mut threads = Vec::new();
        for _ in 0..4 {
            threads.push(scope.spawn(|| {
                barrier.wait();
                t.ctx.dispatch(&args, &mut Vec::new(), &mut Vec::new())
            }));
        }
        for thread in threads {
            assert_eq!(thread.join().unwrap().unwrap(), 0);
        }
    });
    accepted(&t);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
}

#[test]
fn retry_never_inherits_a_parent_or_widens_workspace_write() {
    let (t, mut db, key) = fixture();
    let parent = t.put(job(json!({"id":"invoking-parent","todo_id":99,"status":"running","pid":std::process::id(),"pid_created":123,"cwd":"D:/other-checkout","work_owner":{}})));
    // An unrelated inherited ID must not become a scheduling exemption.
    t.rt.env
        .lock()
        .unwrap()
        .insert("CC_JOB_ID".into(), s(&parent, "id").into());
    db["items"][0]["product_retry_intent"]["source"]["sandbox"] = json!("workspace-write");
    db["items"][0]["product_retry_intent"]["source"]["network"] = json!(true);
    save_db(&t, &db);
    submit(
        &t,
        &key,
        &["--sandbox", "workspace-write", "--network"],
        BODY,
    )
    .unwrap();
    let job = accepted(&t);
    assert!(job["parent_job_id"].is_null());
    assert_eq!(job["sandbox"], "workspace-write");
    let args = codex_args(&job);
    let sandbox = args.iter().position(|arg| arg == "-s").unwrap();
    assert_eq!(args[sandbox + 1], "workspace-write");
    assert!(
        args.iter()
            .any(|arg| arg == "sandbox_workspace_write.network_access=true")
    );
    let execution = t.ctx.prompt_for_attempt(&job).unwrap();
    assert!(
        fs::read_to_string(execution)
            .unwrap()
            .contains(cc_job_policy::WRITE_GUIDANCE.trim())
    );
    assert_eq!(
        fs::read(t.ctx.job_dir(s(&job, "id")).join("prompt.md")).unwrap(),
        BODY
    );
    assert_eq!(t.ctx.load("invoking-parent").unwrap(), parent);
}

#[test]
fn retry_preserves_network_metadata_saved_by_an_unrestricted_source() {
    let (t, mut db, key) = fixture();
    db["items"][0]["product_retry_intent"]["source"]["sandbox"] = json!("danger-full-access");
    db["items"][0]["product_retry_intent"]["source"]["network"] = json!(true);
    save_db(&t, &db);
    submit(
        &t,
        &key,
        &["--sandbox", "danger-full-access", "--network"],
        BODY,
    )
    .unwrap();
    let job = accepted(&t);
    assert_eq!(job["sandbox"], "danger-full-access");
    assert_eq!(job["network"], true);
    assert!(t.ctx.admit(s(&job, "id")).unwrap().is_none());
}

#[test]
fn running_retry_and_unrelated_job_are_never_finalized_or_killed_by_invalidation() {
    let (t, mut db, key) = fixture();
    submit(&t, &key, &[], BODY).unwrap();
    let retry = bind(&t, &mut db, &key);
    assert!(t.ctx.admit(s(&retry, "id")).unwrap().is_some());
    let running = t.ctx.load(s(&retry, "id")).unwrap();
    let other = t.put(job(
        json!({"id":"unrelated-running","todo_id":42,"status":"running"}),
    ));
    db["items"][0]["stopped"] = json!(true);
    save_db(&t, &db);
    assert!(t.ctx.admit(s(&retry, "id")).unwrap().is_none());
    assert_eq!(t.ctx.load(s(&retry, "id")).unwrap(), running);
    assert_eq!(t.ctx.load("unrelated-running").unwrap(), other);
    assert!(t.rt.kills.lock().unwrap().is_empty());
}

#[test]
fn running_publication_holds_the_same_todo_lock_as_bind_and_wait_writers() {
    let (t, mut db, key) = fixture();
    submit(&t, &key, &[], BODY).unwrap();
    let job = bind(&t, &mut db, &key);
    let job_file = t.ctx.job_dir(s(&job, "id")).join("job.json");
    let todo_lock = t.ctx.root.join("todo.lock");
    let attempts = Arc::new(Mutex::new(Vec::new()));
    let observed = attempts.clone();
    *t.rt.save_hook.lock().unwrap() = Some(Box::new(move |path, _| {
        if path == job_file {
            observed
                .lock()
                .unwrap()
                .push(FileLock::try_acquire(&todo_lock).is_err());
        }
    }));
    let admission = t.ctx.admit(s(&job, "id")).unwrap();
    *t.rt.save_hook.lock().unwrap() = None;
    assert!(admission.is_some());
    assert_eq!(*attempts.lock().unwrap(), vec![true]);
    assert_eq!(t.ctx.read_json(&t.ctx.root.join("todo.json")).unwrap(), db);
}
