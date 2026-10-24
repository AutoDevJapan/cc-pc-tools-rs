//! A real protocol error followed by a child that never exits. The clock and
//! escape hatch are controlled by the fixture, not by wall-clock sleeps.
use super::*;

const QUOTA: &str =
    "Reconnecting... 1/5 (exceeded retry limit, last status: 429 Too Many Requests)";

fn fixture(events: Vec<Value>, extra: Value) -> (Test, Value, Arc<AtomicBool>) {
    let t = Test::new();
    let j = t.put(patch(
        job(
            json!({"id":"pretool", "name":"pretool", "model":"anthropic/claude-opus-5-5",
        "cwd":t.ctx.root, "codex":"codex.CMD", "sandbox":"read-only"}),
        ),
        extra,
    ));
    t.rt.pause_allowed.store(true, Ordering::Relaxed);
    t.rt.procs.lock().unwrap().insert(321, 456);
    t.rt.child_alive.store(true, Ordering::Relaxed);
    let alive = t.rt.child_alive.clone();
    let text = events
        .into_iter()
        .map(|ev| format!("{ev}\n"))
        .collect::<String>();
    *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
        Box::new(FakeChild {
            alive: alive.clone(),
            out: Some(Box::new(Cursor::new(text.clone().into_bytes()))),
        })
    }));
    let clock = Arc::downgrade(&t.rt);
    let alive = t.rt.child_alive.clone();
    let escaped = Arc::new(AtomicBool::new(false));
    let escape = escaped.clone();
    *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |_| {
        if clock.upgrade().unwrap().mono() >= 220. {
            escape.store(true, Ordering::Relaxed);
            alive.store(false, Ordering::Relaxed);
        }
    }));
    (t, j, escaped)
}

#[test]
fn protocol_429_before_first_tool_reaps_the_idle_child_before_the_escape_hatch() {
    let (t, j, escaped) = fixture(vec![json!({"type":"error","message":QUOTA})], json!({}));
    t.ctx.supervise(s(&j, "id")).unwrap();
    let saved = t.ctx.load(s(&j, "id")).unwrap();
    assert!(
        !escaped.load(Ordering::Relaxed),
        "the protocol error never reached bounded collection"
    );
    assert_eq!(saved["status"], "failed");
    assert_eq!(saved["last_failure_kind"], "quota");
    assert_eq!(saved["last_error"], QUOTA);
    assert_eq!(saved["protocol_stall"]["stage"], "drained");
    assert_eq!(saved["steps"], 0);
    assert_eq!(*t.rt.kills.lock().unwrap(), [321]);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    assert!(saved["cancel_requested"] != true);
    for key in ["id", "cwd", "sandbox", "model", "prompt", "todo_id"] {
        assert_eq!(saved[key], j[key], "{key}");
    }
}

#[test]
fn zero_completed_steps_does_not_mean_a_tool_or_external_operation_is_idle() {
    for kind in [
        "command_execution",
        "mcp_tool_call",
        "collab_tool_call",
        "future_external_operation",
    ] {
        let (t, j, escaped) = fixture(
            vec![
                json!({"type":"error","message":QUOTA}),
                json!({"type":"item.started","item":{"id":"active","type":kind}}),
            ],
            json!({}),
        );
        t.ctx.supervise(s(&j, "id")).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert!(escaped.load(Ordering::Relaxed), "{kind}");
        assert_eq!(saved["steps"], 0);
        assert!(t.rt.kills.lock().unwrap().is_empty(), "{kind}");
        assert!(saved["protocol_stall"].is_null());
    }
}

#[test]
fn a_tool_arriving_during_the_pause_is_rechecked_before_stopping() {
    let (t, j, escaped) = fixture(vec![json!({"type":"error","message":QUOTA})], json!({}));
    let events = t.ctx.job_dir(s(&j, "id")).join("events.jsonl");
    *t.rt.pause_hook.lock().unwrap() = Some(Box::new(move || {
        use std::io::Write;
        let mut file = fs::OpenOptions::new().append(true).open(&events).unwrap();
        writeln!(
            file,
            "{}",
            json!({"type":"item.started","item":{"id":"late","type":"mcp_tool_call"}})
        )
        .unwrap();
    }));
    t.ctx.supervise(s(&j, "id")).unwrap();
    assert!(escaped.load(Ordering::Relaxed));
    assert!(t.rt.kills.lock().unwrap().is_empty());
    assert!(t.ctx.load(s(&j, "id")).unwrap()["protocol_stall"].is_null());
}

#[test]
fn a_new_job_generation_or_owner_cancel_is_not_overwritten_or_stopped() {
    for key in [
        "codex_pid_created",
        "started_at",
        "todo_resume_generation",
        "cwd",
        "cancel_requested",
    ] {
        let (t, j, escaped) = fixture(vec![json!({"type":"error","message":QUOTA})], json!({}));
        let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
        let value = if key == "cancel_requested" {
            json!(true)
        } else {
            json!("replacement")
        };
        let expected = value.clone();
        *t.rt.pause_hook.lock().unwrap() = Some(Box::new(move || {
            let mut fresh: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
            fresh[key] = value.clone();
            if key == "cancel_requested" {
                fresh["status"] = json!("cancelled");
            }
            fs::write(&path, fresh.to_string()).unwrap();
        }));
        t.ctx.supervise(s(&j, "id")).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert!(escaped.load(Ordering::Relaxed), "{key}");
        assert!(t.rt.kills.lock().unwrap().is_empty(), "{key}");
        assert_eq!(saved[key], expected, "{key}");
        if key == "cancel_requested" {
            assert_eq!(saved["status"], "cancelled");
        }
    }
}

#[test]
fn a_new_dependency_or_question_wait_keeps_the_original_todo_unchanged() {
    for wait in [
        json!({"status":"waiting","wait_on":[9],"dependency_generation":2}),
        json!({"status":"blocked","question_wait":{"id":"pending-question"}}),
        json!({"status":"doing","resume_generation":2}),
        json!({"status":"doing","wait_on":[9]}),
    ] {
        let (t, j, escaped) = fixture(
            vec![json!({"type":"error","message":QUOTA})],
            json!({"todo_id":2,"todo_resume_generation":1,"todo_dependency_generation":1}),
        );
        let db = t.ctx.root.join("todo.json");
        let row = json!({"id":2,"status":"doing","resume_generation":1,"dependency_generation":1,
            "jobs":["pretool"],"scheduler":{"job":"pretool"}});
        fs::write(&db, json!({"items":[row.clone()]}).to_string()).unwrap();
        let changed = patch(row, wait);
        let expected = changed.clone();
        let path = db.clone();
        *t.rt.pause_hook.lock().unwrap() = Some(Box::new(move || {
            fs::write(&path, json!({"items":[changed]}).to_string()).unwrap();
        }));
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert!(escaped.load(Ordering::Relaxed));
        assert!(t.rt.kills.lock().unwrap().is_empty());
        let current: Value = serde_json::from_slice(&fs::read(db).unwrap()).unwrap();
        assert_eq!(current["items"][0], expected);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    }
}

#[test]
fn protocol_auth_cancel_transient_and_normal_completion_do_not_trigger_collection() {
    for (event, kind, status) in [
        (
            json!({"type":"turn.failed","error":{"code":"auth_failed","message":"Selected model is at capacity"}}),
            "auth",
            "failed",
        ),
        (
            json!({"type":"turn.failed","error":{"code":"cancelled","message":QUOTA}}),
            "cancel",
            "failed",
        ),
        (json!({"type":"turn.started"}), "", "failed"),
        (
            json!({"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}),
            "",
            "succeeded",
        ),
    ] {
        let (t, j, escaped) = fixture(
            vec![json!({"type":"error","message":QUOTA}), event],
            json!({}),
        );
        t.ctx.supervise(s(&j, "id")).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert!(escaped.load(Ordering::Relaxed));
        assert!(t.rt.kills.lock().unwrap().is_empty());
        assert_eq!(s(&saved, "last_failure_kind"), kind);
        assert_eq!(saved["status"], status);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    }
}

#[test]
fn a_stalled_capacity_attempt_resumes_the_same_job_only_after_the_tree_is_drained() {
    let (t, j, escaped) = fixture(
        vec![json!({"type":"error","message":"Selected model is at capacity"})],
        json!({}),
    );
    let alive = t.rt.child_alive.clone();
    let count = AtomicUsize::new(0);
    *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
        let first = count.fetch_add(1, Ordering::Relaxed) == 0;
        if !first {
            assert!(
                !alive.load(Ordering::Relaxed),
                "old child must be drained before respawn"
            );
        }
        Box::new(FakeChild {
            alive: if first {
                alive.clone()
            } else {
                Arc::new(AtomicBool::new(false))
            },
            out: Some(Box::new(Cursor::new(if first {
                format!(
                    "{}\n",
                    json!({"type":"error","message":"Selected model is at capacity"})
                )
                .into_bytes()
            } else {
                OK.as_bytes().to_vec()
            }))),
        })
    }));
    t.ctx.supervise(s(&j, "id")).unwrap();
    let saved = t.ctx.load(s(&j, "id")).unwrap();
    assert!(!escaped.load(Ordering::Relaxed));
    assert_eq!(saved["id"], j["id"]);
    assert_eq!(saved["status"], "succeeded");
    assert_eq!(saved["model"], "gpt-6.1-sol");
    assert_eq!(saved["capacity_retry_count"], 1);
    assert_eq!(*t.rt.kills.lock().unwrap(), [321]);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 2);
}

#[test]
fn collected_quota_review_reenters_the_existing_verified_oauth_route_with_the_same_spec() {
    let (t, mut j, escaped) = fixture(
        vec![json!({"type":"error","message":QUOTA})],
        json!({"todo_id":2,"todo_resume_generation":1,"todo_dependency_generation":0,
            "task_type":"review","model_origin":"policy","effort":"max",
            "launch_key":"11111111111111111111111111111111","mcp_profile":"minimal",
            "approval_policy":"never","approvals_reviewer":"user","add_dirs":[],"network":false}),
    );
    let spec = t.ctx.root.join("fixed-review.md");
    fs::write(&spec, "Review the exact fixed source. Preserve this body.").unwrap();
    let spec_hash = cc_job_policy::product_retry::digest(&fs::read(&spec).unwrap());
    j["finite_review"] =
        json!({"todo_id":2,"spec":spec,"launch_key":j["launch_key"],"spec_sha256":spec_hash});
    j["review_source_jobs"] = json!(["author"]);
    t.ctx.save(&j).unwrap();
    t.put(json!({"id":"author","status":"succeeded","model":"gpt-6.1-sol","cwd":t.ctx.root.join("author")}));
    let mut row = json!({"id":2,"project":"p","status":"doing","dispatchable":true,"needs":[],
        "spec":spec,"resume_generation":1,"dependency_generation":0,"jobs":["pretool"],
        "review_source_jobs":["author"],"scheduler":{"job":"pretool","worktree":t.ctx.root,"branch":"scheduler/todo-2-review"}});
    let db = t.ctx.root.join("todo.json");
    fs::write(&db, json!({"items":[row]}).to_string()).unwrap();
    t.ctx.supervise("pretool").unwrap();
    assert!(!escaped.load(Ordering::Relaxed));
    let source = t.ctx.load("pretool").unwrap();
    assert_eq!(source["last_failure_kind"], "quota");
    assert_eq!(source["protocol_stall"]["stage"], "drained");
    assert_eq!(
        t.rt.spawns.load(Ordering::Relaxed),
        1,
        "no blind same-provider retry"
    );
    assert_eq!(source["finite_review"], j["finite_review"]);
    // supervise() has returned. Model the old supervisor's exit and a new
    // creation time at the same PID before normal scheduler registration.
    t.rt.procs.lock().unwrap().insert(std::process::id(), 124);
    // Ordinary result handling prepares another generation of the SAME ToDo.
    // Use the existing proof/CLI/admission implementation, not a new requeue.
    row["status"] = json!("todo");
    row["scheduler"]["job"] = Value::Null;
    row["scheduler"]["last_job"] = json!("pretool");
    row["scheduler"]["key"] = json!("22222222222222222222222222222222");
    row["scheduler"]["phase"] = json!("prepared");
    let at = t.rt.epoch();
    let health = json!({"at":at,"anthropic":{"at":at,"verified":true,"exhausted":true,"available":false},
        "openai":{"at":at,"verified":true,"exhausted":false,"available":true}});
    *t.rt.quota_recovery_inventory.lock().unwrap() = health.clone();
    let mut target = json!({"model":"gpt-6-luna","model_origin":"policy","effort":"max","cwd":t.ctx.root,
        "launch_key":row["scheduler"]["key"],"task_type":"review","sandbox":"read-only","network":false,
        "mcp_profile":"minimal","approval_policy":"never","approvals_reviewer":"user","add_dirs":[]});
    target["quota_recovery"] =
        cc_job_policy::quota_recovery::receipt(&row, &source, &target, &health, at).unwrap();
    target["finite_review_spec_sha256"] = json!(spec_hash);
    row["scheduler"]["selection"] = target;
    fs::write(
        &db,
        json!({"projects":{"p":t.ctx.root},"items":[row],"next_id":3}).to_string(),
    )
    .unwrap();
    let prompt = format!(
        "Scheduler prompt\n依頼:\n{}",
        fs::read_to_string(&spec).unwrap()
    );
    t.run(
        &[
            "--todo-id",
            "2",
            "--launch-key",
            "22222222222222222222222222222222",
            "--model",
            "gpt-6-luna",
            "--model-origin",
            "policy",
            "--effort",
            "max",
            "--task-type",
            "review",
            "--review-source-jobs",
            "author",
            "--sandbox",
            "read-only",
            "--mcp-profile",
            "minimal",
        ],
        prompt.as_bytes(),
    )
    .unwrap();
    let target = t
        .ctx
        .list_jobs(&HashSet::new())
        .into_iter()
        .find(|j| j["todo_id"] == 2 && j["id"] != "pretool")
        .unwrap();
    let paths = cc_todo::Paths::at(&t.ctx.root, &t.ctx.jobs);
    let mut state = cc_todo::load(&db).unwrap();
    cc_todo::execute(
        &mut state,
        &cc_todo::Command::Start {
            id: 2,
            job: s(&target, "id").into(),
            progress: None,
        },
        &paths,
    )
    .unwrap();
    fs::write(&db, state.to_string()).unwrap();
    *t.rt.spawn_hook.lock().unwrap() = None;
    t.ctx.supervise(s(&target, "id")).unwrap();
    let result = t.ctx.load(s(&target, "id")).unwrap();
    assert_eq!(result["status"], "succeeded");
    assert_eq!(result["model"], "gpt-6-luna");
    assert_eq!(result["todo_id"], source["todo_id"]);
    assert_eq!(
        result["finite_review"]["spec_sha256"],
        source["finite_review"]["spec_sha256"]
    );
    for k in ["cwd", "sandbox", "network", "mcp_profile", "add_dirs"] {
        assert_eq!(result[k], source[k], "{k}");
    }
    assert_eq!(result["review_author_models"], json!(["gpt-6.1-sol"]));
    assert_eq!(
        t.ctx.load("pretool").unwrap(),
        source,
        "original error remains intact"
    );
}

#[test]
fn stopping_intent_must_be_durable_and_a_failed_stop_never_counts_as_drain() {
    for fault in ["intent", "stop"] {
        let (t, j, escaped) = fixture(vec![json!({"type":"error","message":QUOTA})], json!({}));
        let denied = Arc::new(AtomicBool::new(false));
        if fault == "intent" {
            let denied = denied.clone();
            let kills = t.rt.kills.clone();
            *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |_, job| {
                if job["protocol_stall"]["stage"] == "stopping"
                    && !denied.swap(true, Ordering::Relaxed)
                {
                    assert!(
                        kills.lock().unwrap().is_empty(),
                        "nothing stops before durable intent"
                    );
                    return Err(io::Error::other("fixture intent unavailable"));
                }
                Ok(())
            }));
        } else {
            t.rt.paused_stop_failure.store(true, Ordering::Relaxed);
        }
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert!(!escaped.load(Ordering::Relaxed));
        let result = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(result["status"], "failed");
        assert_eq!(result["last_failure_kind"], "quota");
        assert_eq!(result["protocol_stall"]["stage"], "drained");
        assert_eq!(*t.rt.kills.lock().unwrap(), [321]);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        if fault == "intent" {
            assert!(denied.load(Ordering::Relaxed));
        }
    }
}

#[test]
fn a_terminal_save_failure_retains_intent_and_cannot_retry_or_publish_success() {
    let (t, j, escaped) = fixture(vec![json!({"type":"error","message":QUOTA})], json!({}));
    let denied = Arc::new(AtomicBool::new(false));
    let seen = denied.clone();
    *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |_, job| {
        if job["protocol_stall"]["stage"] == "drained" {
            seen.store(true, Ordering::Relaxed);
            return Err(io::Error::other("fixture final save unavailable"));
        }
        Ok(())
    }));
    assert!(t.ctx.supervise(s(&j, "id")).is_err());
    assert!(denied.load(Ordering::Relaxed));
    assert!(!escaped.load(Ordering::Relaxed));
    let durable = t.ctx.load(s(&j, "id")).unwrap();
    assert_eq!(durable["protocol_stall"]["stage"], "stopping");
    assert_eq!(durable["last_failure_kind"], "quota");
    assert_eq!(durable["last_error"], QUOTA);
    assert_ne!(durable["status"], "succeeded");
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    assert_eq!(*t.rt.kills.lock().unwrap(), [321]);
}
