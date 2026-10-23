use super::*;
use cc_job_policy::{finished_review::Request, product_retry::digest, work_owner};

fn setup() -> (Test, subwork_support::Fixture, Request) {
    let (t, mut f, _) = finite_owner_wait_repair::setup();
    let tree = subwork_support::tree(&f.repo, "todo-3-writer", "scheduler/todo-3-writer");
    let mut w = f.jobs[0].clone();
    w["id"] = json!("writer");
    w["todo_id"] = json!(3);
    w["cwd"] = json!(tree);
    w["work_owner"] = work_owner::checkout(tree.to_str().unwrap());
    w["work_owner"]["pr"] = json!(86);
    t.put(w.clone());
    f.db["items"].as_array_mut().unwrap().extend([
        json!({"id":3,"status":"wait","wait_on":[77],"jobs":["writer"],"scheduler":{"job":"writer","worktree":tree,"branch":"scheduler/todo-3-writer"}}),
        json!({"id":77,"status":"block","note":"unfinished cleanup"})]);
    fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
    let mut j = t.ctx.load("child").unwrap();
    j["steps"] = json!(0);
    j["network"] = json!(false);
    j["sandbox"] = json!("danger-full-access");
    j["scope_source"] = json!("launcher");
    j["add_dirs"] = json!(["original-launch-write-hint"]);
    j["fixed_review_protocol"] = json!({"version":cc_job_policy::finished_review::EXECUTION_PROTOCOL,"pid":j["pid"],"created":j["pid_created"]});
    j["work_wait_on"] = json!([3]);
    j["work_wait_evidence"] = json!({"owner_job_id":"writer","owner":work_owner::owner_evidence(&w),"candidate_identity":j["work_owner"]});
    t.put(j);
    let spec = fs::read(f.db["items"][1]["spec"].as_str().unwrap()).unwrap();
    let r = Request {
        job_id: "child".into(),
        todo_id: 2,
        parent_job_id: "parent".into(),
        parent_todo_id: 1,
        owner_job_id: "writer".into(),
        owner_todo_id: 3,
        read_only: true,
        child_spec_sha256: digest(&spec),
        parent_spec_sha256: digest(&spec),
        prompt_sha256: digest(b"fixture task"),
        fixed_head: "a".repeat(40),
        fixed_base: "b".repeat(40),
    };
    t.rt.handler.lock().unwrap().replace(Box::new(|c| {
        Ok(Reply {
            code: 0,
            stderr: vec![],
            stdout: if c.argv.iter().any(|a| a == "rev-parse") {
                format!("{}\n", "a".repeat(40)).into_bytes()
            } else {
                vec![]
            },
        })
    }));
    (t, f, r)
}

#[test]
fn native_session_history_does_not_block_normal_registration_or_change_the_store() {
    let t = Test::new();
    let home = tempfile::tempdir().unwrap();
    let session = "00000000-0000-7000-8000-000000000001";
    let db = json!({"items":[{"id":1,"status":"todo","jobs":[session]}]});
    let store = home.path().join("todo.json");
    fs::write(&store, db.to_string()).unwrap();
    t.rt.env
        .lock()
        .unwrap()
        .insert("CC_TODO_HOME".into(), home.path().to_string_lossy().into());
    let before = fs::read(&store).unwrap();
    t.run(&["--todo-id", "1"], b"fixture ordinary task")
        .unwrap();
    let jobs = t.ctx.list_jobs(&HashSet::new());
    assert_eq!(jobs.len(), 1);
    assert!(jobs[0]["fixed_review_requalification"].is_null());
    assert_eq!(fs::read(&store).unwrap(), before);
    assert!(!t.ctx.job_dir(session).exists());
}

#[test]
fn missing_managed_or_existing_broken_native_record_still_rejects_registration() {
    for existing_native in [false, true] {
        let t = Test::new();
        let home = tempfile::tempdir().unwrap();
        let id = if existing_native {
            "00000000-0000-7000-8000-000000000001"
        } else {
            "20261024-000000-missing-managed"
        };
        if existing_native {
            fs::create_dir_all(t.ctx.job_dir(id)).unwrap();
            fs::write(t.ctx.job_dir(id).join("job.json"), b"broken").unwrap();
        }
        let store = home.path().join("todo.json");
        fs::write(
            &store,
            json!({"items":[{"id":1,"status":"todo","jobs":[id]}]}).to_string(),
        )
        .unwrap();
        t.rt.env
            .lock()
            .unwrap()
            .insert("CC_TODO_HOME".into(), home.path().to_string_lossy().into());
        let before = fs::read(&store).unwrap();
        let error = t
            .run(&["--todo-id", "1"], b"fixture ordinary task")
            .unwrap_err();
        assert!(
            error
                .message
                .contains("no such job (or unreadable job.json)")
        );
        assert_eq!(fs::read(&store).unwrap(), before);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
}

#[test]
fn original_reader_admits_and_receives_fixed_read_only_source_while_cleanup_stays_unfinished() {
    let (t, f, r) = setup();
    let request = t.ctx.root.join("request.json");
    fs::write(&request, serde_json::to_vec(&r).unwrap()).unwrap();
    let before = t
        .ctx
        .read_json(&t.ctx.job_dir("child").join("job.json"))
        .unwrap();
    let todo_before = fs::read(f.dir.path().join("todo.json")).unwrap();
    let prompt_before = fs::read(t.ctx.job_dir("child").join("prompt.md")).unwrap();
    let args = [
        "qualify-fixed-review",
        "--request-file",
        request.to_str().unwrap(),
    ];
    assert_eq!(t.cli(&args).0.unwrap(), 0);
    assert_eq!(
        t.ctx
            .read_json(&t.ctx.job_dir("child").join("job.json"))
            .unwrap(),
        before
    );
    let mut args = args.to_vec();
    args.push("--apply");
    assert_eq!(t.cli(&args).0.unwrap(), 0);
    let after = t
        .ctx
        .read_json(&t.ctx.job_dir("child").join("job.json"))
        .unwrap();
    let allowed = [
        "work_wait_on",
        "work_wait_evidence",
        "work_wait_history",
        "queue_wait_reason",
        "fixed_read_only_review",
        "sandbox",
        "approval_policy",
        "approvals_reviewer",
    ];
    for (k, v) in before.as_object().unwrap() {
        if !allowed.contains(&k.as_str()) {
            assert_eq!(&after[k], v, "{k}");
        }
    }
    assert_eq!(
        fs::read(f.dir.path().join("todo.json")).unwrap(),
        todo_before
    );
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    // No new job, duplicate review or synthetic writer->reader wait was needed.
    assert!(t.ctx.admit("child").unwrap().is_some());
    let running = t.ctx.load("child").unwrap();
    assert_eq!(running["status"], "running");
    assert_eq!(running["work_wait_on"], json!([]));
    assert_eq!(running["sandbox"], "read-only");
    let args = codex_args(&running);
    assert!(args.windows(2).any(|a| a == ["-s", "read-only"]));
    assert!(args.iter().any(|a| a == "approval_policy=never"));
    assert!(args.iter().any(|a| a == "approvals_reviewer=user"));
    assert!(
        !args
            .iter()
            .any(|a| a == "--add-dir" || a.contains("network_access=true"))
    );
    let path = t.ctx.prompt_for_attempt(&running).unwrap();
    let text = fs::read_to_string(path).unwrap();
    assert!(text.contains(&r.fixed_head));
    assert!(text.contains(&r.fixed_base));
    assert!(text.ends_with(cc_job_policy::READ_GUIDANCE));
    assert_eq!(
        fs::read(t.ctx.job_dir("child").join("prompt.md")).unwrap(),
        prompt_before
    );
    let db = t.ctx.read_json(&f.dir.path().join("todo.json")).unwrap();
    assert_eq!(db["items"][2]["status"], "wait");
    assert_eq!(db["items"][2]["wait_on"], json!([77]));
    assert_eq!(db["items"][3]["status"], "block");
}

#[test]
fn execution_checks_the_consumed_prompt_even_if_the_original_path_is_replaced() {
    let (t, f, r) = setup();
    t.ctx.qualify_finished_review(&r, true).unwrap();
    let prompt = t.ctx.job_dir("child").join("prompt.md");
    fs::write(&prompt, b"unverified input that must never be consumed").unwrap();
    let spec = std::path::PathBuf::from(f.db["items"][1]["spec"].as_str().unwrap());
    *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |path, _| {
        if path == spec {
            fs::write(&prompt, b"fixture task").unwrap();
        }
    }));
    // The second read would match the saved hash, but the input already read
    // for execution did not. The old two-read implementation accepted it.
    let job = t.ctx.load("child").unwrap();
    let error = t.ctx.prompt_for_attempt(&job).unwrap_err();
    assert!(
        error.message.contains("changed before execution"),
        "{}",
        error.message
    );
    assert_eq!(
        fs::read(t.ctx.job_dir("child").join("prompt.md")).unwrap(),
        b"fixture task"
    );
    assert!(!t.ctx.job_dir("child").join("execution-prompt.md").exists());
}

#[test]
fn fixed_review_spawn_disables_every_external_tool_for_browser_full_and_legacy_pipe() {
    for profile in ["minimal", "browser", "full"] {
        for summary in [false, true] {
            let (t, _f, r) = setup();
            t.ctx.qualify_finished_review(&r, true).unwrap();
            let mut job = t.ctx.load("child").unwrap();
            job["codex"] = json!("codex.cmd");
            job["mcp_profile"] = json!(profile);
            job["chrome_pipe"] = json!("legacy-host-browser-pipe");
            if summary {
                job["context_phase"] = json!("summarize");
                fs::write(
                    t.ctx.job_dir("child").join("context-summary-prompt.md"),
                    "summarize original read-only work",
                )
                .unwrap();
            }
            {
                let mut env = t.rt.env.lock().unwrap();
                for name in [
                    "cdp_browser_backend_pipe_path",
                    "BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET",
                    "BROWSER_USE_AVAILABLE_BACKENDS",
                ] {
                    env.insert(name.into(), "inherited-host-browser".into());
                }
            }
            let before = job.clone();
            let spawn = t.ctx.execution_spawn(&job).unwrap();
            let configs: Vec<_> = spawn
                .argv
                .windows(2)
                .filter(|a| a[0] == "-c")
                .map(|a| a[1].as_str())
                .collect();
            assert!(spawn.argv.windows(2).any(|a| a == ["-s", "read-only"]));
            for flag in [
                "features.plugins=false",
                "features.apps=false",
                "web_search=disabled",
                "approval_policy=never",
                "approvals_reviewer=user",
                "mcp_servers.stripe.enabled=false",
                "mcp_servers.unseen-server.enabled=false",
                "mcp_servers.node_repl.enabled=false",
            ] {
                assert!(
                    configs.contains(&flag),
                    "{profile}/{summary}: missing {flag}"
                );
            }
            assert!(!configs.iter().any(|c| (c.starts_with("mcp_servers.")
                || c.starts_with("plugins.")
                || c.starts_with("features.apps")
                || c.starts_with("features.plugins"))
                && c.ends_with("=true")));
            assert!(!spawn.argv.iter().any(|a| a == "--add-dir"
                || a.contains("legacy-host-browser-pipe")
                || a.contains("CDP_BROWSER_BACKEND")
                || a.contains("network_access=true")));
            assert!(!spawn.env.keys().any(|key| {
                [
                    "CDP_BROWSER_BACKEND_PIPE_PATH",
                    "BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET",
                    "BROWSER_USE_AVAILABLE_BACKENDS",
                ]
                .iter()
                .any(|name| key.eq_ignore_ascii_case(name))
            }));
            assert_eq!(t.rt.summary_calls.load(Ordering::Relaxed), 1);
            assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
            assert_eq!(spawn.flags, NO_WINDOW);
            assert_eq!(job, before); // Saved profile, source, legacy fields and history stay intact.
        }
    }
}

#[test]
fn missing_fixed_review_tool_inventory_never_falls_back_to_the_saved_browser_profile() {
    let (t, _f, r) = setup();
    t.ctx.qualify_finished_review(&r, true).unwrap();
    let mut job = t.ctx.load("child").unwrap();
    job["mcp_profile"] = json!("browser");
    job["codex"] = json!("codex.cmd");
    t.rt.summary_fail.store(true, Ordering::Relaxed);
    assert!(t.ctx.execution_spawn(&job).is_err());
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
}

#[test]
fn supervisor_does_not_install_a_browser_bridge_when_fixed_inventory_is_unavailable() {
    let (t, _f, r) = setup();
    t.ctx.qualify_finished_review(&r, true).unwrap();
    let mut job = t.ctx.load("child").unwrap();
    job["mcp_profile"] = json!("browser");
    job["codex"] = json!("codex.cmd");
    t.put(job);
    assert!(!t.ctx.home.exists());
    t.rt.summary_fail.store(true, Ordering::Relaxed);
    t.ctx.supervise("child").unwrap();
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    assert!(
        !t.ctx.home.exists(),
        "fixed review must not install the host-browser hook before a failed launch"
    );
    assert!(t.ctx.load("child").unwrap()["chrome_pipe"].is_null());
}

#[test]
fn ordinary_browser_and_full_launches_keep_their_existing_capabilities() {
    let t = Test::new();
    for profile in ["browser", "full"] {
        let j=t.put(job(json!({"id":format!("normal-{profile}"),"mcp_profile":profile,"chrome_pipe":"normal-pipe","sandbox":"read-only","add_dirs":["normal-directory"]})));
        let spawn = t.ctx.execution_spawn(&j).unwrap();
        assert!(
            spawn
                .argv
                .iter()
                .any(|a| a == "mcp_servers.node_repl.enabled=true")
        );
        assert!(
            spawn
                .argv
                .iter()
                .any(|a| a == "plugins.'chrome@openai-bundled'.enabled=true")
        );
        assert!(spawn.argv.iter().any(|a| a == "--add-dir"));
        assert_eq!(spawn.env["CDP_BROWSER_BACKEND_PIPE_PATH"], "normal-pipe");
    }
    assert_eq!(t.rt.summary_calls.load(Ordering::Relaxed), 0);
}

#[test]
fn qualifying_refuses_old_or_reused_controller_live_writer_protected_source_and_git_changes() {
    for case in [
        "old_protocol",
        "previous_protocol",
        "reused_controller",
        "writer_alive",
        "writer_probe_denied",
        "protected",
        "wrong_head",
        "dirty",
        "unknown_request",
    ] {
        let (t, f, r) = setup();
        match case {
            "old_protocol" => {
                let mut j = t.ctx.load("child").unwrap();
                j["fixed_review_protocol"] = Value::Null;
                t.put(j);
            }
            "previous_protocol" => {
                let mut j = t.ctx.load("child").unwrap();
                j["fixed_review_protocol"]["version"] = json!(1);
                t.put(j);
            }
            "reused_controller" => {
                let mut j = t.ctx.load("child").unwrap();
                j["pid_created"] = json!(999);
                t.put(j);
            }
            "writer_alive" => {
                let mut w = t.ctx.load("writer").unwrap();
                w["pid"] = json!(std::process::id());
                w["pid_created"] = json!(123);
                t.put(w);
            }
            "writer_probe_denied" => {
                let mut w = t.ctx.load("writer").unwrap();
                w["pid"] = json!(123456);
                w["pid_created"] = json!(123);
                t.put(w);
                t.rt.proc_probe_errors.lock().unwrap().insert(123456, 5);
            }
            "protected" => {
                t.rt.recovery_guards
                    .lock()
                    .unwrap()
                    .insert(s(&f.jobs[0], "cwd").into(), Some(true));
            }
            "wrong_head" => {
                t.rt.handler.lock().unwrap().replace(Box::new(|_| {
                    Ok(Reply {
                        code: 0,
                        stdout: b"different\n".to_vec(),
                        stderr: vec![],
                    })
                }));
            }
            "dirty" => {
                t.rt.handler.lock().unwrap().replace(Box::new(|c| {
                    Ok(Reply {
                        code: if c.argv.iter().any(|a| a == "diff") {
                            1
                        } else {
                            0
                        },
                        stdout: format!("{}\n", "a".repeat(40)).into_bytes(),
                        stderr: vec![],
                    })
                }));
            }
            "unknown_request" => {
                let mut raw = serde_json::to_value(&r).unwrap();
                raw["grant_write"] = json!(true);
                assert!(serde_json::from_value::<Request>(raw).is_err());
                continue;
            }
            _ => unreachable!(),
        }
        let before = t.ctx.load("child").unwrap();
        assert!(t.ctx.qualify_finished_review(&r, true).is_err(), "{case}");
        assert_eq!(t.ctx.load("child").unwrap(), before, "{case}");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
}

#[test]
fn normal_continuations_cannot_regain_launcher_capabilities_before_fresh_qualification() {
    for profile in ["browser", "full"] {
        for resumed in [false, true] {
            let (t, f, r) = setup();
            t.ctx.qualify_finished_review(&r, true).unwrap();
            let mut original = t.ctx.load("child").unwrap();
            original["status"] = json!("succeeded");
            original["exit_code"] = json!(0);
            original["finished_at"] = json!("2026-10-17T01:00:00+09:00");
            original["pid"] = Value::Null;
            original["codex_pid"] = Value::Null;
            original["model"] = json!("m");
            original["effort"] = json!("e");
            let session = "12345678-1234-4321-9234-123456789abc";
            original["thread_id"] = json!(session);
            t.put(original.clone());
            let cwd = s(&original, "cwd");
            let mut args = vec![
                "--todo-id",
                "2",
                "--cwd",
                cwd,
                "--sandbox",
                "danger-full-access",
                "--mcp-profile",
                profile,
                "--launch-key",
                "abcdabcdabcdabcdabcdabcdabcdabcd",
            ];
            if resumed {
                args.extend(["--resume-session", session]);
            }
            t.run(&args, b"PR86 Continue the same finite review")
                .unwrap();
            let mut next = t
                .ctx
                .list_jobs(&HashSet::new())
                .into_iter()
                .find(|j| j["launch_key"] == "abcdabcdabcdabcdabcdabcdabcdabcd")
                .unwrap();
            assert_eq!(
                next["fixed_review_requalification"]["source_job_id"],
                "child"
            );
            assert!(next["fixed_read_only_review"].is_null());
            assert!(t.ctx.admit(s(&next, "id")).unwrap().is_none());
            assert_eq!(
                t.ctx.load(s(&next, "id")).unwrap()["queue_wait_reason"]["code"],
                "fixed_review_requalification"
            );
            assert!(
                t.ctx
                    .execution_spawn(&next)
                    .err()
                    .unwrap()
                    .message
                    .contains("fresh qualification")
            );
            let argv = codex_args(&next);
            assert!(argv.windows(2).any(|a| a == ["-s", "read-only"]));
            assert!(argv.iter().any(|a| a == "features.plugins=false"));
            assert!(
                !argv
                    .iter()
                    .any(|a| a.ends_with(".enabled=true") || a == "--add-dir")
            );
            let prompt = fs::read(t.ctx.job_dir(s(&next, "id")).join("prompt.md")).unwrap();
            assert!(String::from_utf8_lossy(&prompt).ends_with(cc_job_policy::READ_GUIDANCE));
            let mut db = f.db.clone();
            db["items"][1]["jobs"] = json!([
                "00000000-0000-7000-8000-000000000001",
                "child",
                next["id"],
                "00000000-0000-7000-8000-000000000002"
            ]);
            db["items"][1]["scheduler"]["job"] = next["id"].clone();
            db["items"][1]["scheduler"]["key"] = json!("abcdabcdabcdabcdabcdabcdabcdabcd");
            fs::write(f.dir.path().join("todo.json"), db.to_string()).unwrap();
            next["pid"] = json!(std::process::id());
            next["pid_created"] = json!(123);
            next["fixed_review_protocol"] = json!({"version":cc_job_policy::finished_review::EXECUTION_PROTOCOL,
                "pid":next["pid"],"created":next["pid_created"]});
            t.put(next.clone());
            let mut request = r.clone();
            request.job_id = s(&next, "id").into();
            request.prompt_sha256 = digest(&prompt);
            t.ctx.qualify_finished_review(&request, true).unwrap();
            let qualified = t.ctx.load(s(&next, "id")).unwrap();
            let spawn = t.ctx.execution_spawn(&qualified).unwrap();
            assert!(spawn.argv.iter().any(|a| a == "features.plugins=false"));
            assert_eq!(t.ctx.load("child").unwrap(), original);
            assert_eq!(
                t.ctx.read_json(&f.dir.path().join("todo.json")).unwrap(),
                db
            );
            if resumed {
                let mut failed = qualified.clone();
                failed["status"] = json!("failed");
                failed["exit_code"] = json!(1);
                failed["finished_at"] = json!("2026-10-17T02:00:00+09:00");
                failed["pid"] = Value::Null;
                failed["codex_pid"] = Value::Null;
                t.put(failed.clone());
                let key = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
                t.run(
                    &[
                        "--todo-id",
                        "2",
                        "--cwd",
                        cwd,
                        "--mcp-profile",
                        profile,
                        "--launch-key",
                        key,
                    ],
                    b"PR86 Retry the same finite review",
                )
                .unwrap();
                let mut retry = t
                    .ctx
                    .list_jobs(&HashSet::new())
                    .into_iter()
                    .find(|j| j["launch_key"] == key)
                    .unwrap();
                assert_eq!(
                    retry["fixed_review_requalification"]["source_job_id"],
                    "child"
                );
                assert!(t.ctx.admit(s(&retry, "id")).unwrap().is_none());
                db["items"][1]["jobs"]
                    .as_array_mut()
                    .unwrap()
                    .push(retry["id"].clone());
                db["items"][1]["scheduler"]["job"] = retry["id"].clone();
                db["items"][1]["scheduler"]["key"] = json!(key);
                fs::write(f.dir.path().join("todo.json"), db.to_string()).unwrap();
                retry["pid"] = json!(std::process::id());
                retry["pid_created"] = json!(123);
                retry["fixed_review_protocol"] = json!({"version":cc_job_policy::finished_review::EXECUTION_PROTOCOL,
                    "pid":retry["pid"],"created":retry["pid_created"]});
                t.put(retry.clone());
                request.job_id = s(&retry, "id").into();
                request.prompt_sha256 =
                    digest(&fs::read(t.ctx.job_dir(s(&retry, "id")).join("prompt.md")).unwrap());
                t.ctx.qualify_finished_review(&request, true).unwrap();
                assert_eq!(t.ctx.load(s(&failed, "id")).unwrap(), failed);
                assert_eq!(t.ctx.load("child").unwrap(), original);
            }
        }
    }
}
