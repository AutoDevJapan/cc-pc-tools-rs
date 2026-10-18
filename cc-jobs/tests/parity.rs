use cc_jobs::notify::*;
use cc_jobs::*;
#[path = "../../cc-job-policy/tests/support/mod.rs"]
mod subwork_support;
#[path = "support/product_retry.rs"]
mod original_product_retry;
#[path = "support/owner_wait_repair.rs"]
mod finite_owner_wait_repair;
#[path = "support/finished_review.rs"]
mod finished_review;
#[path = "support/finished_work_handoff.rs"]
mod finished_work_handoff;
#[path = "support/no_action.rs"]
mod no_action;

mod independent_subwork {
    use super::*;
    fn setup(cap: usize) -> (Test, subwork_support::Fixture) {
        let t = Test::new();
        let mut f = subwork_support::Fixture::new();
        f.register();
        fs::write(
            t.ctx.root.join("queue-config.json"),
            json!({"max_running":cap}).to_string(),
        )
        .unwrap();
        t.rt.env
            .lock()
            .unwrap()
            .insert("CC_TODO_HOME".into(), f.dir.path().to_string_lossy().into());
        fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
        f.jobs[0]["pid"] = json!(std::process::id());
        f.jobs[0]["pid_created"] = json!(123);
        t.put(f.jobs[0].clone());
        (t, f)
    }
    fn register(t: &Test, f: &subwork_support::Fixture, extra: &[&str]) -> Result<i32> {
        let row = &f.db["items"][1];
        let mut args = vec![
            "--todo-id",
            "2",
            "--subwork-of",
            "1",
            "--parent-job-id",
            "parent",
            "--assigned-needs",
            "",
            "--launch-key",
            s(&row["scheduler"], "key"),
            "--cwd",
            s(&row["scheduler"], "worktree"),
            "--sandbox",
            "danger-full-access",
        ];
        args.extend(extra);
        t.run(&args, b"finite check")
    }
    fn child(t: &Test) -> Value {
        t.ctx
            .list_jobs(&HashSet::new())
            .into_iter()
            .find(|j| j["todo_id"] == 2)
            .unwrap()
    }
    #[test]
    fn queued_subwork_admits_after_todo_start_releases_its_reservation() {
        let (t, f) = setup(1);
        register(&t, &f, &[]).unwrap();
        let registered = child(&t);
        let paths = cc_todo::Paths::at(f.dir.path(), &t.ctx.jobs);
        let mut db = cc_todo::load(&paths.store).unwrap();
        cc_todo::execute(
            &mut db,
            &cc_todo::Command::Start {
                id: 2,
                job: s(&registered, "id").into(),
                progress: None,
            },
            &paths,
        )
        .unwrap();
        fs::write(&paths.store, db.to_string()).unwrap();
        assert!(db["items"][1]["scheduler"]["key"].is_null());
        assert_eq!(db["items"][1]["scheduler"]["job"], registered["id"]);
        assert!(t.ctx.admit(s(&registered, "id")).unwrap().is_none());
        assert_eq!(
            t.ctx.load(s(&registered, "id")).unwrap()["queue_wait_reason"]["code"],
            "running_limit"
        );
        fs::write(
            t.ctx.root.join("queue-config.json"),
            json!({"max_running":2}).to_string(),
        )
        .unwrap();
        assert!(t.ctx.admit(s(&registered, "id")).unwrap().is_some());
        let started = t.ctx.load(s(&registered, "id")).unwrap();
        assert_eq!(started["status"], "running");
        assert!(started["started_at"].is_string());
        assert!(started["queue_wait_reason"].is_null());
        assert_eq!(started["launch_key"], registered["launch_key"]);
        assert_eq!(started["parent_job_id"], "parent");
    }
    #[test]
    fn registration_keeps_cross_todo_parent_but_does_not_exempt_hardware_capacity() {
        let (t, f) = setup(1);
        register(&t, &f, &[]).unwrap();
        let child = child(&t);
        assert_eq!(child["parent_job_id"], "parent");
        assert!(t.ctx.admit(s(&child, "id")).unwrap().is_none());
        assert_eq!(
            t.ctx.load(s(&child, "id")).unwrap()["queue_wait_reason"]["code"],
            "running_limit"
        );
        fs::write(
            t.ctx.root.join("queue-config.json"),
            json!({"max_running":2}).to_string(),
        )
        .unwrap();
        assert!(t.ctx.admit(s(&child, "id")).unwrap().is_some());
    }
    #[test]
    fn registration_validates_allocation_parent_and_queued_duplicates() {
        let (t, f) = setup(8);
        assert!(register(&t, &f, &["--assigned-needs", "cargo"]).is_err());
        assert!(register(&t, &f, &["--parent-job-id", "fake"]).is_err());
        assert!(register(&t, &f, &["--subwork-of", "9"]).is_err());
        register(&t, &f, &[]).unwrap();
        assert!(
            register(
                &t,
                &f,
                &["--launch-key", "33333333333333333333333333333333"]
            )
            .is_err()
        );
        assert_eq!(
            t.ctx
                .list_jobs(&HashSet::new())
                .iter()
                .filter(|j| j["todo_id"] == 2)
                .count(),
            1
        );
    }
    #[test]
    fn queued_job_revalidates_parent_exit_and_changed_todo() {
        for changed in [false, true] {
            let (t, mut f) = setup(8);
            register(&t, &f, &[]).unwrap();
            let child = child(&t);
            let mut parent = t.ctx.load("parent").unwrap();
            parent["status"] = json!("succeeded");
            parent["pid"] = Value::Null;
            t.ctx.save(&parent).unwrap();
            if changed {
                f.db["items"][1]["needs"] = json!(["cargo"]);
                fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
            }
            assert_eq!(t.ctx.admit(s(&child, "id")).unwrap().is_some(), !changed);
            let saved = t.ctx.load(s(&child, "id")).unwrap();
            if changed {
                assert_eq!(saved["queue_wait_reason"]["code"], "subwork_invalid");
            } else {
                assert!(saved["parent_job_id"].is_null());
            }
        }
    }
    #[test]
    fn invalid_queued_subwork_does_not_block_a_normal_root_but_running_subwork_does() {
        for running in [false, true] {
            let (t, mut f) = setup(8);
            register(&t, &f, &[]).unwrap();
            let mut child = child(&t);
            let mut parent = t.ctx.load("parent").unwrap();
            parent["status"] = json!("succeeded");
            parent["pid"] = Value::Null;
            t.ctx.save(&parent).unwrap();
            f.db["items"][1]["needs"] = json!(["cargo"]);
            fs::write(f.dir.path().join("todo.json"), f.db.to_string()).unwrap();
            if running {
                child["status"] = json!("running");
                child["pid"] = json!(std::process::id());
                child["pid_created"] = json!(123);
                t.ctx.save(&child).unwrap();
            }
            t.put(job(
                json!({"id":"normal","name":"normal","status":"queued","todo_id":3,
                "cwd":f.repo,"pid":std::process::id(),"pid_created":123,"queue_sequence":500,
                "created_at":"2026-10-10T03:00:00+09:00","work_owner":child["work_owner"]}),
            ));
            {
                let _lock = t.ctx.queue_lock().unwrap();
                t.ctx.track_queued_job("normal").unwrap();
            }
            assert_eq!(t.ctx.admit("normal").unwrap().is_some(), !running);
            if running {
                assert_eq!(
                    t.ctx.load("normal").unwrap()["queue_wait_reason"]["code"],
                    "work_owner"
                );
            }
        }
    }
}

mod reboot_recovery {
    use super::*;
    const SESSION_ID: &str = "01a11052-a537-74f3-9611-deb742778fc5";
    fn interrupted(t: &Test, id: &str, status: &str) -> Value {
        *t.rt.boot.lock().unwrap() = Some(parse_iso("2026-10-01T11:55:00+09:00").unwrap());
        let j = t.put(job(json!({"id":id,"name":id,"status":status,
            "created_at":"2026-10-01T11:00:00+09:00",
            "started_at":"2026-10-01T11:01:00+09:00",
            "last_event_at":"2026-10-01T11:54:00+09:00",
            "finished_at":"2026-10-01T11:56:00+09:00",
            "last_error":"scheduler: both job roots are gone",
            "pid":12345,"pid_created":12,"codex_pid":23456,"codex_pid_created":23,
            "thread_id":SESSION_ID,"cwd":t.ctx.root,"sandbox":"danger-full-access",
            "mcp_profile":"browser","codex":"codex.CMD"})));
        fs::write(t.ctx.job_dir(id).join("prompt.md"), "元の依頼文").unwrap();
        j
    }
    #[test]
    fn preview_preserves_state_then_resume_preserves_identity_and_logs_one_batch() {
        let t = Test::new();
        let a = interrupted(&t, "reboot-a", "lost");
        let mut b = interrupted(&t, "reboot-b", "queued");
        b["thread_id"] = Value::Null;
        b["started_at"] = Value::Null;
        t.ctx.save(&b).unwrap();
        let preview = t.cli(&["recover-reboot"]);
        assert_eq!(preview.0.unwrap(), 0);
        assert!(preview.1.contains("would_queue"));
        assert_eq!(t.ctx.load("reboot-a").unwrap(), a);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        assert!(t.rt.calls.lock().unwrap().is_empty());
        assert_eq!(
            t.ctx
                .recover_reboot(true)
                .unwrap()
                .as_array()
                .unwrap()
                .len(),
            2
        );
        let resumed = t.ctx.load("reboot-a").unwrap();
        for field in ["id", "name", "cwd", "sandbox", "mcp_profile", "thread_id"] {
            assert_eq!(resumed[field], a[field], "{field}");
        }
        assert_eq!(resumed["status"], "queued");
        assert_eq!(resumed["resume_session"], SESSION_ID);
        assert!(
            codex_args(&resumed)
                .windows(2)
                .any(|w| w == ["resume", SESSION_ID])
        );
        assert_eq!(t.ctx.load("reboot-b").unwrap()["resume_session"], "");
        assert_eq!(
            fs::read_to_string(t.ctx.job_dir("reboot-b").join("prompt.md")).unwrap(),
            "元の依頼文"
        );
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls.len(), 1);
        assert!(calls[0].argv.iter().any(|a| a == "後片付けの記録"));
        assert!(!calls[0].argv.iter().any(|a| a == "--notify"));
        assert!(calls[0].argv.last().unwrap().contains("2件"));
        assert!(calls[0].argv.last().unwrap().contains("reboot-a"));
        assert!(calls[0].argv.last().unwrap().contains("reboot-b"));
    }
    #[test]
    fn only_current_boot_interruption_is_eligible_even_with_502() {
        for case in [
            "failed",
            "cancelled",
            "cancel_requested",
            "old_lost",
            "new_activity",
            "created_after_boot",
            "normal_lost",
            "completed",
            "final_failure",
            "live_root",
        ] {
            let t = Test::new();
            let mut j = interrupted(&t, "excluded", "lost");
            match case {
                "failed" | "cancelled" => {
                    j["status"] = json!(case);
                    j["last_error"] = json!("502 Bad Gateway");
                }
                "cancel_requested" => j["cancel_requested"] = json!(true),
                "old_lost" => j["finished_at"] = json!("2026-10-01T11:54:00+09:00"),
                "new_activity" => j["last_event_at"] = json!("2026-10-01T11:56:00+09:00"),
                "created_after_boot" => j["created_at"] = json!("2026-10-01T11:56:00+09:00"),
                "normal_lost" => j["last_error"] = json!("child process could not be stopped"),
                "completed" => j["turn_completed"] = json!(true),
                "final_failure" => fs::write(
                    t.ctx.job_dir("excluded").join("events.jsonl"),
                    "{\"type\":\"turn.failed\"}\n{partial",
                )
                .unwrap(),
                "live_root" => {
                    t.rt.procs.lock().unwrap().insert(23456, 23);
                }
                _ => unreachable!(),
            }
            t.ctx.save(&j).unwrap();
            t.ctx.recover_reboot(true).unwrap();
            assert_eq!(t.ctx.load("excluded").unwrap(), j, "{case}");
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{case}");
        }
        let t = Test::new();
        let mut j = interrupted(&t, "reconnect", "lost");
        j["last_error"] = json!("502 Bad Gateway");
        t.ctx.save(&j).unwrap();
        fs::write(
            t.ctx.job_dir("reconnect").join("events.jsonl"),
            "{\"type\":\"error\",\"message\":\"502 Bad Gateway\"}\n{partial",
        )
        .unwrap();
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn spawn_failure_keeps_pending_claim_and_retry_never_requeues_again() {
        let t = Test::new();
        interrupted(&t, "pending", "running");
        t.rt.supervisor_fail.store(true, Ordering::Relaxed);
        t.ctx.recover_reboot(true).unwrap();
        let claimed = t.ctx.load("pending").unwrap();
        assert_eq!(claimed["reboot_launch_pending"], true);
        assert_eq!(claimed["queue_restart_pending"], true);
        assert_eq!(claimed["status"], "queued");
        assert!(t.ctx.supervisor_alive(&claimed));
        assert!(t.rt.calls.lock().unwrap().is_empty());
        t.rt.supervisor_fail.store(false, Ordering::Relaxed);
        t.ctx.recover_reboot(true).unwrap();
        let resumed = t.ctx.load("pending").unwrap();
        assert_eq!(
            resumed["reboot_recovered_at"],
            claimed["reboot_recovered_at"]
        );
        assert_eq!(resumed["reboot_launch_pending"], false);
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2); // failed spawn + success
    }
    #[test]
    fn deliberate_cancel_of_pending_claim_is_never_restarted() {
        let t = Test::new();
        let mut j = interrupted(&t, "pending-cancel", "cancelled");
        j["reboot_launch_pending"] = json!(true);
        j["cancel_requested"] = json!(true);
        t.ctx.save(&j).unwrap();
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.ctx.load("pending-cancel").unwrap(), j);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn concurrent_recoveries_launch_and_report_only_once() {
        let t = Test::new();
        interrupted(&t, "race", "lost");
        std::thread::scope(|scope| {
            let barrier = Arc::new(std::sync::Barrier::new(8));
            for _ in 0..8 {
                let barrier = barrier.clone();
                let ctx = &t.ctx;
                scope.spawn(move || {
                    barrier.wait();
                    ctx.recover_reboot(true).unwrap();
                });
            }
        });
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
    }
    #[test]
    fn successor_and_finished_todo_prevent_recovery() {
        for status in ["done", "block", "wait"] {
            let t = Test::new();
            let mut j = interrupted(&t, "todo-stop", "lost");
            j["todo_id"] = json!(77);
            t.ctx.save(&j).unwrap();
            // Unique test roots share Temp's parent; use a tools-shaped context.
            let mut ctx = t.ctx.clone();
            ctx.root = t.ctx.root.join("tools/codex-jobs");
            fs::create_dir_all(ctx.root.parent().unwrap().join("todo")).unwrap();
            fs::write(
                ctx.root.parent().unwrap().join("todo/todo.json"),
                json!({"items":[{"id":77,"status":status,"jobs":["todo-stop"]}]}).to_string(),
            )
            .unwrap();
            ctx.recover_reboot(true).unwrap();
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{status}");
        }
        let t = Test::new();
        let j = interrupted(&t, "superseded", "lost");
        t.put(job(
            json!({"id":"new-attempt","name":"different","thread_id":SESSION_ID,
            "created_at":"2026-10-01T11:57:00+09:00","status":"succeeded"}),
        ));
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.ctx.load("superseded").unwrap(), j);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn log_failure_retries_log_without_relaunching() {
        let t = Test::new();
        interrupted(&t, "log-retry", "lost");
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 1,
            stdout: vec![],
            stderr: vec![],
        }));
        let (result, output, _) = t.cli(&["recover-reboot", "--apply"]);
        assert_eq!(result.unwrap(), 0);
        assert!(output.contains("log_pending"));
        assert_eq!(t.ctx.load("log-retry").unwrap()["reboot_log_pending"], true);
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(
            t.ctx.load("log-retry").unwrap()["reboot_log_pending"],
            false
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 2);
    }
    #[test]
    fn completed_boot_receipt_skips_history_and_missing_boot_cannot_recover() {
        let t = Test::new();
        let lost = interrupted(&t, "cached", "lost");
        *t.rt.boot.lock().unwrap() = None;
        assert_eq!(t.ctx.recover_reboot(true).unwrap(), json!([]));
        assert_eq!(t.ctx.load("cached").unwrap(), lost);
        *t.rt.boot.lock().unwrap() = Some(parse_iso("2026-10-01T11:55:00+09:00").unwrap());
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(|path, _| {
            assert_ne!(
                path.file_name().unwrap(),
                "job.json",
                "completed boot must not rescan history"
            );
        }));
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn queued_retry_does_not_mistake_previous_failure_for_this_attempt() {
        let t = Test::new();
        let mut queued = interrupted(&t, "retry", "lost");
        queued["started_at"] = Value::Null;
        queued["queued_at"] = json!("2026-10-01T11:54:00+09:00");
        t.ctx.save(&queued).unwrap();
        fs::write(
            t.ctx.job_dir("retry").join("events.jsonl"),
            "{\"type\":\"turn.failed\",\"_ts\":\"2026-10-01T11:53:00+09:00\"}\n",
        )
        .unwrap();
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn running_job_without_session_uses_original_prompt() {
        let t = Test::new();
        let mut running = interrupted(&t, "no-thread", "running");
        running["thread_id"] = Value::Null;
        t.ctx.save(&running).unwrap();
        t.ctx.recover_reboot(true).unwrap();
        let resumed = t.ctx.load("no-thread").unwrap();
        assert_eq!(resumed["resume_session"], "");
        assert!(!codex_args(&resumed).iter().any(|a| a == "resume"));
        assert_eq!(
            fs::read_to_string(t.ctx.job_dir("no-thread").join("prompt.md")).unwrap(),
            "元の依頼文"
        );
    }
    #[test]
    fn unreadable_inventory_is_not_recorded_as_an_empty_completed_boot() {
        let t = Test::new();
        let original = interrupted(&t, "valid", "lost");
        fs::create_dir(t.ctx.job_dir("broken")).unwrap();
        fs::write(t.ctx.job_dir("broken").join("job.json"), b"{bad").unwrap();
        let report = t.ctx.recover_reboot(true).unwrap();
        assert!(
            report
                .as_array()
                .unwrap()
                .iter()
                .any(|entry| entry["action"] == "deferred")
        );
        assert_eq!(
            t.ctx
                .read_json(&t.ctx.root.join("reboot-recovery.json"))
                .unwrap()["complete"],
            false
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(
            t.ctx.load("valid").unwrap()["thread_id"],
            original["thread_id"]
        );
        assert_eq!(
            fs::read(t.ctx.job_dir("broken").join("job.json")).unwrap(),
            b"{bad"
        );
        t.put(job(json!({"id":"broken","status":"failed"})));
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn pending_reservation_is_released_after_todo_finishes() {
        let t = Test::new();
        let mut ctx = t.ctx.clone();
        ctx.root = t.ctx.root.join("tools/codex-jobs");
        fs::create_dir_all(ctx.root.parent().unwrap().join("todo")).unwrap();
        let mut j = interrupted(&t, "no-longer-needed", "lost");
        j["todo_id"] = json!(77);
        t.ctx.save(&j).unwrap();
        t.rt.supervisor_fail.store(true, Ordering::Relaxed);
        ctx.recover_reboot(true).unwrap();
        assert_eq!(
            t.ctx.load("no-longer-needed").unwrap()["reboot_launch_pending"],
            true
        );
        fs::write(
            ctx.root.parent().unwrap().join("todo/todo.json"),
            json!({"items":[{"id":77,"status":"done"}]}).to_string(),
        )
        .unwrap();
        ctx.recover_reboot(true).unwrap();
        let stopped = t.ctx.load("no-longer-needed").unwrap();
        assert_eq!(stopped["status"], "lost");
        assert_eq!(stopped["queue_restart_pending"], false);
        assert_eq!(stopped["reboot_launch_pending"], false);
        assert!(!t.ctx.running_job(&stopped));
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1); // original failed spawn only
        assert!(t.rt.calls.lock().unwrap().is_empty());
    }
    #[test]
    fn running_resume_ignores_prior_attempt_terminal_event() {
        let t = Test::new();
        let mut running = interrupted(&t, "old-terminal", "lost");
        running["started_at"] = json!("2026-10-01T11:53:00+09:00");
        t.ctx.save(&running).unwrap();
        fs::write(
            t.ctx.job_dir("old-terminal").join("events.jsonl"),
            "{\"type\":\"turn.failed\",\"_ts\":\"2026-10-01T11:52:00+09:00\"}\n",
        )
        .unwrap();
        t.ctx.recover_reboot(true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn guard_failure_defers_only_its_job_and_retry_reports_one_batch() {
        let t = Test::new();
        let mut denied = interrupted(&t, "guard-late", "lost");
        denied["thread_id"] = Value::Null;
        denied["cwd"] = json!(t.ctx.root.join("guard-late"));
        fs::create_dir(t.ctx.root.join("guard-late")).unwrap();
        t.ctx.save(&denied).unwrap();
        interrupted(&t, "ready", "lost");
        t.rt.recovery_guards
            .lock()
            .unwrap()
            .insert(s(&denied, "cwd").into(), None);
        let (result, _, _) = t.cli(&["recover-reboot", "--apply"]);
        assert_eq!(result.unwrap(), 75);
        assert_eq!(t.ctx.load("guard-late").unwrap(), denied);
        assert_eq!(t.ctx.load("ready").unwrap()["status"], "queued");
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert!(t.rt.calls.lock().unwrap().is_empty());
        t.rt.recovery_guards.lock().unwrap().clear();
        assert_eq!(t.cli(&["recover-reboot", "--apply"]).0.unwrap(), 0);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        assert!(
            t.rt.calls.lock().unwrap()[0]
                .argv
                .last()
                .unwrap()
                .contains("2件")
        );
    }
    #[test]
    fn protected_work_is_never_mutated_or_reported() {
        let t = Test::new();
        let original = interrupted(&t, "protected", "lost");
        t.rt.recovery_guards
            .lock()
            .unwrap()
            .insert(s(&original, "cwd").into(), Some(true));
        assert_eq!(t.ctx.recover_reboot(true).unwrap(), json!([]));
        assert_eq!(t.ctx.load("protected").unwrap(), original);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        assert!(t.rt.calls.lock().unwrap().is_empty());
    }
    #[test]
    fn missing_unreserved_prompt_is_a_permanent_boot_skip_not_a_dispatch_blocker() {
        let t = Test::new();
        let original = interrupted(&t, "missing-prompt", "lost");
        fs::rename(
            t.ctx.job_dir("missing-prompt").join("prompt.md"),
            t.ctx.root.join("held-prompt.md"),
        )
        .unwrap();
        let (result, output, _) = t.cli(&["recover-reboot", "--apply"]);
        assert_eq!(result.unwrap(), 0);
        assert!(output.contains("skip"));
        assert_eq!(t.ctx.load("missing-prompt").unwrap(), original);
        assert_eq!(
            t.ctx
                .read_json(&t.ctx.root.join("reboot-recovery.json"))
                .unwrap()["complete"],
            true
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn protected_skip_is_cached_while_another_job_is_deferred() {
        let t = Test::new();
        let protected = interrupted(&t, "protected", "lost");
        let mut later = interrupted(&t, "later", "lost");
        later["thread_id"] = Value::Null;
        later["cwd"] = json!(t.ctx.root.join("later"));
        fs::create_dir(t.ctx.root.join("later")).unwrap();
        t.ctx.save(&later).unwrap();
        {
            let mut guards = t.rt.recovery_guards.lock().unwrap();
            guards.insert(s(&protected, "cwd").into(), Some(true));
            guards.insert(s(&later, "cwd").into(), None);
        }
        assert_eq!(t.cli(&["recover-reboot", "--apply"]).0.unwrap(), 75);
        assert_eq!(
            t.ctx
                .read_json(&t.ctx.root.join("reboot-recovery.json"))
                .unwrap()["skipped"]["protected"],
            "protected"
        );
        {
            let mut guards = t.rt.recovery_guards.lock().unwrap();
            // Rechecking the protected entry now fails. A cached skip must
            // permit the remaining ordinary job to progress anyway.
            guards.insert(s(&protected, "cwd").into(), None);
            guards.insert(s(&later, "cwd").into(), Some(false));
        }
        assert_eq!(t.cli(&["recover-reboot", "--apply"]).0.unwrap(), 0);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.load("protected").unwrap(), protected);
    }
}

mod adaptive_config_updates {
    use super::*;
    #[test]
    fn partial_update_and_deployment_defaults_preserve_owner_fixed_limit() {
        let t = Test::new();
        let path = t.ctx.root.join("queue-config.json");
        t.ctx
            .write_json(
                &path,
                &json!({"max_running":16,"fixed_max_running":32,
            "adaptive":{"min_running":16},"retry_delay_seconds":77}),
            )
            .unwrap();
        let patch_path = t.ctx.root.join("config-patch.json");
        t.ctx
            .write_json(
                &patch_path,
                &json!({"max_running":20,"fixed_max_running":null,
            "adaptive":{"increase_step":3}}),
            )
            .unwrap();
        let patch_file = patch_path.to_string_lossy();
        let (result, _, _) = t.cli(&["update-queue-config", "--patch-file", &patch_file]);
        assert_eq!(result.unwrap(), 0);
        let saved = t.ctx.read_json(&path).unwrap();
        assert_eq!(saved["fixed_max_running"], 32);
        assert_eq!(saved["adaptive"]["min_running"], 16);
        assert_eq!(saved["adaptive"]["increase_step"], 3);
        assert_eq!(saved["retry_delay_seconds"], 77);
        assert_eq!(saved["max_running"], 20);
        assert!(
            t.ctx
                .update_queue_config(&json!({"fixed_max_running":24}), false)
                .is_err()
        );
        assert!(
            t.ctx
                .update_queue_config(&json!({"max_running":0}), false)
                .is_err()
        );
        assert_eq!(t.ctx.read_json(&path).unwrap(), saved);
    }
    #[test]
    fn explicit_clear_requires_measured_healthy_samples_and_logs_once() {
        let t = Test::new();
        let path = t.ctx.root.join("queue-config.json");
        let config = json!({"max_running":16,"fixed_max_running":32,"adaptive":{}});
        t.ctx.write_json(&path, &config).unwrap();
        for (samples, age, hardware) in [(2, 0., 32), (3, 1000., 32), (3, 0., 15)] {
            t.ctx.write_json(&t.ctx.root.join("queue-limit-state.json"),&json!({
                "checked_at":t.rt.epoch()-age,"hardware_max_running":hardware,"auto_ready_samples":samples,
                "settings":cc_jobs::adaptive::AdaptiveConfig::default()})).unwrap();
            assert!(t.ctx.update_queue_config(&json!({}), true).is_err());
            assert_eq!(t.ctx.read_json(&path).unwrap(), config);
        }
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-limit-state.json"),
                &json!({
            "checked_at":t.rt.epoch(),"hardware_max_running":32,"auto_ready_samples":3,
            "settings":cc_jobs::adaptive::AdaptiveConfig::default()}),
            )
            .unwrap();
        let (result, _, _) = t.cli(&["update-queue-config", "--clear-fixed"]);
        assert_eq!(result.unwrap(), 0);
        assert!(
            t.ctx
                .read_json(&path)
                .unwrap()
                .get("fixed_max_running")
                .is_none()
        );
        let log = t.ctx.root.join("queue-config.CHANGES.txt");
        assert_eq!(fs::read_to_string(&log).unwrap().lines().count(), 1);
        t.ctx.update_queue_config(&json!({}), true).unwrap();
        assert_eq!(fs::read_to_string(log).unwrap().lines().count(), 1);
    }
    #[test]
    fn simultaneous_policy_change_cannot_clear_using_old_measurements() {
        let t = Test::new();
        let path = t.ctx.root.join("queue-config.json");
        let config = json!({"max_running":16,"fixed_max_running":32,"adaptive":{}});
        t.ctx.write_json(&path, &config).unwrap();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-limit-state.json"),
                &json!({
            "checked_at":t.rt.epoch(),"hardware_max_running":32,"auto_ready_samples":3,
            "settings":cc_jobs::adaptive::AdaptiveConfig::default()}),
            )
            .unwrap();
        assert!(
            t.ctx
                .update_queue_config(&json!({"adaptive":{"cpu_percent":60.}}), true)
                .is_err()
        );
        assert_eq!(t.ctx.read_json(&path).unwrap(), config);
    }
    #[test]
    fn removing_fixed_override_uses_shadow_provider_brakes_immediately() {
        let t = Test::new();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({
            "max_running":16,"fixed_max_running":32,"adaptive":{}}),
            )
            .unwrap();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-limit-state.json"),
                &json!({
            "mode":"fixed","checked_at":t.rt.epoch(),"hardware_max_running":40,
            "max_running":0,"claude_max_running":40,"preferred_provider":"anthropic",
            "auto_ready_samples":3,"settings":cc_jobs::adaptive::AdaptiveConfig::default()}),
            )
            .unwrap();
        let before = t.ctx.limit_status(&t.ctx.queue_config().unwrap()).unwrap();
        assert_eq!(
            (
                before["max_running"].clone(),
                before["codex_max_running"].clone()
            ),
            (json!(32), json!(32))
        );
        t.ctx.update_queue_config(&json!({}), true).unwrap();
        let after = t.ctx.limit_status(&t.ctx.queue_config().unwrap()).unwrap();
        assert_eq!(
            (
                after["max_running"].clone(),
                after["codex_max_running"].clone()
            ),
            (json!(40), json!(0))
        );
        assert_eq!(after["preferred_provider"], "anthropic");
    }
    #[test]
    fn provider_only_waits_do_not_grow_the_hardware_ceiling() {
        let t = Test::new();
        let now = t.rt.epoch();
        let cfg = cc_jobs::adaptive::AdaptiveConfig::default();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":32,"adaptive":{}}),
            )
            .unwrap();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-limit-state.json"),
                &json!({
            "mode":"auto","checked_at":now-60.,"hardware_max_running":32,
            "max_running":0,"claude_max_running":32,"settings":cfg,
            "machine":{"cpu_times":[100,200,100]}}),
            )
            .unwrap();
        t.put(job(json!({"id":"codex-demand","status":"queued","model":"gpt-6.1-sol",
            "model_explicit":true,"pid":std::process::id(),"pid_created":123,"queued_at":t.rt.now()})));
        let report = t
            .ctx
            .queue_report(&t.ctx.list_jobs(&HashSet::new()))
            .unwrap();
        assert_eq!(report["queued"][0]["reason"]["hardware_limited"], false);
        t.rt.queue_sample.lock().unwrap().cpu_times = Some([180, 300, 100]);
        t.ctx.adjust_limit().unwrap();
        let state = t
            .ctx
            .read_json(&t.ctx.root.join("queue-limit-state.json"))
            .unwrap();
        assert_eq!(state["queued"], 0);
        assert_eq!(state["hardware_max_running"], 32);
    }
    #[test]
    fn fixed_mode_keeps_admission_fixed_while_sampling_shadow_automatic_limit() {
        let t = Test::new();
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({
            "max_running":16,"fixed_max_running":32,"adaptive":{}}),
            )
            .unwrap();
        t.ctx.adjust_limit().unwrap();
        t.rt.tick(61.);
        t.rt.queue_sample.lock().unwrap().cpu_times = Some([180, 300, 100]);
        t.ctx.adjust_limit().unwrap();
        assert_eq!(t.rt.queue_samples.load(Ordering::Relaxed), 2);
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 32);
        let state = t
            .ctx
            .read_json(&t.ctx.root.join("queue-limit-state.json"))
            .unwrap();
        assert_eq!(state["mode"], "fixed");
        assert!(state["hardware_max_running"].as_u64().unwrap() >= 16);
        assert!(state["cpu_percent"].is_number());
    }
}

mod claude_routing {
    use super::*;
    fn setup(t: &Test) {
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":4,"adaptive":{}}),
            )
            .unwrap();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-limit-state.json"),
                &json!({
                    "checked_at":t.rt.epoch(),"mode":"auto","max_running":4,
                    "claude_max_running":8,"preferred_provider":"anthropic"
                }),
            )
            .unwrap();
    }
    fn waiting(t: &Test, id: &str, extra: Value) {
        t.put(job(patch(
            json!({"id":id,"name":id,"status":"queued","model":"gpt-6.1-sol",
            "model_explicit":false,"pid":std::process::id(),"pid_created":123,"cwd":t.ctx.root}),
            extra,
        )));
    }
    #[test]
    fn automatic_models_route_at_admission_and_explicit_or_session_models_stay() {
        for (extra, expected) in [
            (json!({}), "anthropic/claude-sonnet-5-5"),
            (
                json!({"task_type":"single_file_summary","sandbox":"read-only"}),
                "anthropic/claude-haiku-5-5",
            ),
            (
                json!({"task_type":"review","sandbox":"read-only"}),
                "anthropic/claude-sonnet-5-5",
            ),
            (
                json!({"task_type":"single_file_summary","sandbox":"danger-full-access"}),
                "anthropic/claude-sonnet-5-5",
            ),
            (json!({"model_explicit":true}), "gpt-6.1-sol"),
            (json!({"model_explicit":null}), "gpt-6.1-sol"),
            (
                json!({"resume_session":"01a0e883-7628-7121-9540-6780fd4cd6f4"}),
                "gpt-6.1-sol",
            ),
            (json!({"first_started_at":"old"}), "gpt-6.1-sol"),
        ] {
            let t = Test::new();
            setup(&t);
            waiting(&t, "candidate", extra);
            assert!(t.ctx.admit("candidate").unwrap().is_some());
            let j = t.ctx.load("candidate").unwrap();
            assert_eq!(j["model"], expected);
            let args = codex_args(&j);
            assert_eq!(
                args[args.iter().position(|a| a == "-m").unwrap() + 1],
                expected
            );
        }
    }
    #[test]
    fn codex_full_lane_does_not_block_claude_but_shared_hardware_still_caps_total() {
        let t = Test::new();
        setup(&t);
        for n in 0..4 {
            waiting(
                &t,
                &format!("codex{n}"),
                json!({"status":"running","model_explicit":true}),
            );
        }
        waiting(&t, "codex-extra", json!({"model_explicit":true}));
        for n in 0..5 {
            waiting(&t, &format!("claude{n}"), json!({}));
        }
        let report = t
            .ctx
            .queue_report(&t.ctx.list_jobs(&HashSet::new()))
            .unwrap();
        assert_eq!(report["max_running"], 8);
        assert_eq!(report["provider_limits"]["openai"], 4);
        assert!(t.ctx.admit("codex-extra").unwrap().is_none());
        for n in 0..4 {
            assert!(t.ctx.admit(&format!("claude{n}")).unwrap().is_some());
        }
        assert!(t.ctx.admit("claude4").unwrap().is_none());
        let report = t
            .ctx
            .queue_report(&t.ctx.list_jobs(&HashSet::new()))
            .unwrap();
        assert_eq!(report["provider_slots"], json!({"openai":4,"anthropic":4}));
    }
    #[test]
    fn stale_future_and_fixed_measurements_do_not_route() {
        for delta in [-1000., 1000.] {
            let t = Test::new();
            setup(&t);
            let p = t.ctx.root.join("queue-limit-state.json");
            let mut state = t.ctx.read_json(&p).unwrap();
            state["checked_at"] = json!(t.rt.epoch() + delta);
            t.ctx.write_json(&p, &state).unwrap();
            waiting(&t, "candidate", json!({}));
            assert!(t.ctx.admit("candidate").unwrap().is_some());
            assert_eq!(t.ctx.load("candidate").unwrap()["model"], "gpt-6.1-sol");
        }
        let t = Test::new();
        setup(&t);
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"adaptive":{},"fixed_max_running":4}),
            )
            .unwrap();
        waiting(&t, "candidate", json!({}));
        assert!(t.ctx.admit("candidate").unwrap().is_some());
        assert_eq!(t.ctx.load("candidate").unwrap()["model"], "gpt-6.1-sol");
    }
    #[test]
    fn mixed_provider_child_keeps_parent_slot_and_unknown_provider_reserves_both_lanes() {
        let t = Test::new();
        setup(&t);
        for n in 0..4 {
            waiting(
                &t,
                &format!("codex{n}"),
                json!({"status":"running","model_explicit":true}),
            );
        }
        waiting(
            &t,
            "parent",
            json!({"status":"running","model":"anthropic/claude-sonnet-5-5"}),
        );
        waiting(
            &t,
            "child",
            json!({"parent_job_id":"parent","model_explicit":true}),
        );
        assert!(t.ctx.admit("child").unwrap().is_some());
        assert_eq!(t.ctx.load("child").unwrap()["model"], "gpt-6.1-sol");
        let unknown = json!({"id":"unknown","queue_unreadable":true,"status":"running"});
        let report = t.ctx.queue_report(&[unknown]).unwrap();
        assert_eq!(report["provider_slots"], json!({"openai":1,"anthropic":1}));
    }
    #[test]
    fn minute_update_uses_one_snapshot_for_both_lanes_at_admission() {
        let t = Test::new();
        setup(&t);
        let state_path = t.ctx.root.join("queue-limit-state.json");
        let mut state = t.ctx.read_json(&state_path).unwrap();
        state["max_running"] = json!(8);
        t.ctx.write_json(&state_path, &state).unwrap();
        let cfg = t.ctx.queue_config().unwrap();
        assert_eq!(cfg.max_running, 8);
        for n in 0..4 {
            waiting(
                &t,
                &format!("codex{n}"),
                json!({"status":"running","model_explicit":true,"queue_fairness_version":3}),
            );
        }
        waiting(&t, "candidate", json!({"model_explicit":true}));
        let previous = Arc::new(Mutex::new(None::<PathBuf>));
        let updated = Arc::new(AtomicBool::new(false));
        let changed = updated.clone();
        state["max_running"] = json!(4);
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |path, _| {
            let mut last = previous.lock().unwrap();
            // Publish between the effective-limit read and the provider read.
            if path == state_path
                && last.as_deref() == Some(path)
                && !changed.swap(true, Ordering::Relaxed)
            {
                fs::write(&state_path, state.to_string()).unwrap();
            }
            *last = Some(path.to_path_buf());
        }));
        assert!(t.ctx.admit_configured("candidate", &cfg).unwrap().is_none());
        assert!(
            updated.load(Ordering::Relaxed),
            "sampler publication was not exercised"
        );
        assert_eq!(
            t.ctx.load("candidate").unwrap()["queue_wait_reason"]["provider_limit"],
            4
        );
    }
    #[test]
    fn run_records_model_provenance_before_applying_defaults() {
        for option in [None, Some("--model"), Some("--auto-model")] {
            let t = Test::new();
            let prompt = t.ctx.root.join("prompt.md");
            fs::write(&prompt, "work").unwrap();
            let mut args = vec![
                "run",
                "--name",
                "routing",
                "--cwd",
                t.ctx.root.to_str().unwrap(),
                "--prompt-file",
                prompt.to_str().unwrap(),
            ];
            if let Some(option) = option {
                args.extend([option, "gpt-6.1-sol"]);
            }
            assert_eq!(t.cli(&args).0.unwrap(), 0);
            let jobs = t.ctx.list_jobs(&HashSet::new());
            assert_eq!(jobs.len(), 1);
            assert_eq!(jobs[0]["model_explicit"], option == Some("--model"));
        }
        let t = Test::new();
        assert!(
            t.cli(&[
                "run",
                "--name",
                "routing",
                "--prompt-file",
                "p",
                "--model",
                "m",
                "--auto-model",
                "m"
            ])
            .0
            .is_err()
        );
    }
}

#[test]
fn display_project_rename_keeps_execution_data_and_survives_stale_supervisor_snapshots() {
    let t = Test::new();
    let former = concat!("x", "LIFE");
    let original = t.put(job(json!({"id":"rename-target","project":former,"name":format!("{former}の改修"),"task_title":"Re-Xの画面を直す","cwd":format!("C:/{former}-wt/agent-1"),"status":"running","pid":123,"todo_id":2053,"unknown":{"old":"keep"},"progress":70})));
    let unrelated = t.put(job(
        json!({"id":"rename-other","project":"other","name":former}),
    ));
    let (result, output, _) = t.cli(&[
        "rename-project",
        "rename-target",
        "--from",
        former,
        "--to",
        "Re:X",
    ]);
    assert_eq!(result.unwrap(), 0);
    assert_eq!(
        serde_json::from_str::<Value>(&output).unwrap()["jobs_changed"],
        1
    );
    let mut expected = original.clone();
    expected["project"] = json!("Re:X");
    expected["name"] = json!("Re:Xの改修");
    expected["task_title"] = json!("Re:Xの画面を直す");
    assert_eq!(t.ctx.load("rename-target").unwrap(), expected);
    assert_eq!(t.ctx.load("rename-other").unwrap(), unrelated);
    assert_eq!(
        t.ctx
            .read_json(&t.ctx.job_dir("rename-target").join("job.json"))
            .unwrap(),
        original,
        "live execution snapshots must not be overwritten by metadata migration"
    );
    let mut stale = original;
    stale["progress"] = json!(80);
    // Simulate an already-running old binary writing its original display names.
    t.ctx
        .write_json(&t.ctx.job_dir("rename-target").join("job.json"), &stale)
        .unwrap();
    expected["progress"] = json!(80);
    assert_eq!(t.ctx.load("rename-target").unwrap(), expected);
    assert_eq!(t.ctx.try_load("rename-target").unwrap(), expected);
    // An updated supervisor carries the same durable rule into its next write.
    t.ctx.save(&stale).unwrap();
    assert_eq!(
        t.ctx
            .read_json(&t.ctx.job_dir("rename-target").join("job.json"))
            .unwrap(),
        expected
    );
    assert!(
        t.cli(&[
            "rename-project",
            "rename-target",
            "--from",
            former,
            "--to",
            "Re:X"
        ])
        .0
        .is_ok()
    );
    assert_eq!(t.ctx.load("rename-target").unwrap(), expected);
}
#[test]
fn display_project_rename_rejects_wrong_project_without_writing() {
    let t = Test::new();
    let original = t.put(job(json!({"id":"rename-target","project":"other"})));
    assert!(
        t.cli(&[
            "rename-project",
            "rename-target",
            "--from",
            "source",
            "--to",
            "Re:X"
        ])
        .0
        .is_err()
    );
    assert_eq!(t.ctx.load("rename-target").unwrap(), original);
    assert!(
        !t.ctx
            .job_dir("rename-target")
            .join("display-name.json")
            .exists()
    );
    assert!(
        t.cli(&[
            "rename-project",
            "../rename-target",
            "--from",
            "other",
            "--to",
            "Re:X"
        ])
        .0
        .is_err()
    );
}
#[test]
fn corrupt_display_rule_never_hides_a_running_job_or_blocks_state_saving() {
    let t = Test::new();
    let original = t.put(job(
        json!({"id":"rename-target","project":"Re:X","status":"running","name":"Re:Xの作業"}),
    ));
    fs::write(
        t.ctx.job_dir("rename-target").join("display-name.json"),
        "{broken",
    )
    .unwrap();
    assert_eq!(t.ctx.load("rename-target").unwrap(), original);
    assert_eq!(t.ctx.try_load("rename-target").unwrap(), original);
    assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
    let mut updated = original;
    updated["progress"] = json!(90);
    t.ctx.save(&updated).unwrap();
    assert_eq!(t.ctx.load("rename-target").unwrap(), updated);
    assert!(!t.rt.diagnostics.lock().unwrap().is_empty());
}
#[test]
fn new_jobs_normalize_explicit_and_path_inferred_product_display_metadata() {
    for explicit in [false, true] {
        let t = Test::new();
        let former = concat!("x", "LIFE");
        let cwd = t.ctx.root.join(format!("{former}-wt")).join("agent-1");
        fs::create_dir_all(&cwd).unwrap();
        let cwd = cwd.to_string_lossy();
        let mut args = vec!["--cwd", &cwd, "--task-title", "Re-Xの作業"];
        if explicit {
            args.extend(["--project", former]);
        }
        assert_eq!(t.run(&args, b"work").unwrap(), 0);
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs.len(), 1);
        assert_eq!(jobs[0]["project"], "Re:X");
        assert_eq!(jobs[0]["task_title"], "Re:Xの作業");
        assert_eq!(jobs[0]["cwd"], cwd.as_ref());
    }
}
#[test]
fn display_project_rename_reads_the_latest_snapshot_after_state_lock() {
    let t = Test::new();
    t.put(job(
        json!({"id":"rename-target","project":"source","name":"sourceの作業","progress":20}),
    ));
    let lock = t.ctx.job_lock("rename-target", "state.lock").unwrap();
    let ctx = t.ctx.clone();
    let worker =
        std::thread::spawn(move || ctx.rename_display_project("rename-target", "source", "Re:X"));
    std::thread::sleep(Duration::from_millis(250));
    assert!(!worker.is_finished(), "rename ignored the job state lock");
    let mut updated = t.ctx.load("rename-target").unwrap();
    updated["progress"] = json!(90);
    updated["activity"] = json!("concurrent update");
    t.ctx.save(&updated).unwrap();
    drop(lock);
    worker.join().unwrap().unwrap();
    let renamed = t.ctx.load("rename-target").unwrap();
    assert_eq!(renamed["project"], "Re:X");
    assert_eq!(renamed["progress"], 90);
    assert_eq!(renamed["activity"], "concurrent update");
}

#[test]
fn cargo_gate_windows_sys_exit_matches_python_long_conversion() {
    for (code, expected) in [
        (0, 0),
        (1, 1),
        (7, 7),
        (127, 127),
        (i32::MAX as i64, i32::MAX),
        (0xc0000005, -1),
        (0xffffffff, -1),
    ] {
        assert_eq!(cc_jobs::gate::python_cli_exit(code), expected);
    }
}

#[test]
fn cargo_cmd_preserves_a_failing_child_exit_code() {
    let temp = Temp::new();
    cargo_test_policy(&temp.0, 1);
    let bin = temp.0.join("bin");
    fs::create_dir(&bin).unwrap();
    fs::copy(
        env!("CARGO_BIN_EXE_cargo_gate"),
        temp.0.join("cargo_gate.exe"),
    )
    .unwrap();
    let launcher = bin.join("cargo.cmd");
    fs::write(
        &launcher,
        include_str!("../bin/cargo.cmd")
            .replace("\r\n", "\n")
            .replace('\n', "\r\n"),
    )
    .unwrap();
    let fixture = temp.0.join("fixture.exe");
    fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), &fixture).unwrap();
    let result = hidden_command("cmd.exe")
        .current_dir(&bin)
        .args(["/d", "/c", ".\\cargo.cmd", "--exit-seven"])
        .env("NoDefaultCurrentDirectoryInExePath", "1")
        .env("CODEX_JOBS_REAL_CARGO", &fixture)
        .env("CODEX_JOBS_CARGO_LOCK_DIR", &temp.0)
        .env("CARGO_TARGET_DIR", &temp.0)
        .env("CODEX_JOBS_CARGO_SLOTS", "1")
        .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
        .output()
        .unwrap();
    assert_eq!(
        result.status.code(),
        Some(7),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
    assert_eq!(result.stdout, b"");
    assert_eq!(result.stderr, b"");
    assert!(temp.0.join("cargo-build.lock.0").exists());
}
// Isolated gates exercise native counters even on memory-small CI runners.
// Production defaults are covered by deterministic admission policy tests.
fn cargo_test_policy(dir: &Path, slots: usize) {
    fs::write(
        dir.join(cc_jobs::gate::admission::CONFIG_FILE),
        serde_json::to_vec(&cc_jobs::gate::admission::Config {
            max_slots: Some(slots),
            min_free_memory_gib: 0.,
            grow_below_cpu_percent: 100.,
            pause_at_cpu_percent: 100.,
            settle_seconds: 0.,
            ..Default::default()
        })
        .unwrap(),
    )
    .unwrap();
}
use std::{
    collections::{BTreeMap, HashMap, HashSet, VecDeque},
    fs,
    io::{self, Cursor, Read},
    path::{Path, PathBuf},
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, AtomicI32, AtomicU32, AtomicUsize, Ordering},
    },
    time::{Duration, Instant},
};
const OK: &str = include_str!("fixtures/OK_EVENTS.jsonl");
const FAIL: &str = include_str!("fixtures/FAIL_EVENTS.jsonl");
const SESSION: &str = include_str!("fixtures/SESSION_LINES.jsonl");
mod capacity_regressions {
    use super::*;
    use cc_jobs::capacity::is_capacity;
    const SESSION_ID: &str = "01a0e883-7628-7121-9540-6780fd4cd6f4";

    fn supervise(failures: usize, patch_value: Value) -> (Test, Value) {
        let t = Test::new();
        let j = t.put(patch(
            job(json!({"id":"capacity","name":"req-capacity","model":"gpt-6.1-sol","cwd":t.ctx.root,"codex":"codex.CMD"})),
            patch_value,
        ));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "continue").unwrap();
        let count = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let text = if count.fetch_add(1, Ordering::Relaxed) < failures {
                format!(
                    "{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"turn.failed\",\"error\":{{\"message\":\"Selected model is at capacity\"}}}}\n"
                )
            } else {
                OK.into()
            };
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        t.ctx.supervise("capacity").unwrap();
        let saved = t.ctx.load("capacity").unwrap();
        (t, saved)
    }

    #[test]
    fn capacity_supervisor_resumes_same_session_on_next_model() {
        let (t, saved) = supervise(
            1,
            json!({"sandbox":"read-only","effort":"high","todo_id":1793}),
        );
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["model"], "anthropic/claude-sonnet-5-5");
        assert_eq!(saved["capacity_retry_count"], 1);
        assert_eq!(saved["capacity_history"][0]["session"], SESSION_ID);
        assert_eq!(saved["capacity_history"][0]["from_model"], "gpt-6.1-sol");
        assert_eq!(
            saved["capacity_history"][0]["to_model"],
            "anthropic/claude-sonnet-5-5"
        );
        assert_eq!(
            saved["capacity_history"][0]["error"],
            "Selected model is at capacity"
        );
        assert_eq!(saved["todo_id"], 1793);
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 2);
        assert!(!args[0].iter().any(|a| a == "resume"));
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert!(
            args[1]
                .windows(2)
                .any(|w| w == ["-m", "anthropic/claude-sonnet-5-5"])
        );
        assert!(args[1].windows(2).any(|w| w == ["-s", "read-only"]));
        assert!(args[1].iter().any(|a| a == "model_reasoning_effort=high"));
        assert!(t.rt.kills.lock().unwrap().is_empty());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        assert!(
            t.rt.diagnostics
                .lock()
                .unwrap()
                .iter()
                .any(|line| line.contains("anthropic/claude-sonnet-5-5で再開"))
        );
    }

    #[test]
    fn capacity_supervisor_stops_after_three_switches() {
        let (t, saved) = supervise(usize::MAX, json!({}));
        assert_eq!(saved["status"], "failed");
        assert_eq!(saved["capacity_retry_count"], 3);
        assert_eq!(saved["capacity_history"].as_array().unwrap().len(), 3);
        assert_eq!(saved["last_error"], "Selected model is at capacity");
        assert!(
            t.rt.diagnostics
                .lock()
                .unwrap()
                .iter()
                .any(|line| line.contains("最大3回に達しました"))
        );
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 4);
        for (arg, model) in args.iter().zip([
            "gpt-6.1-sol",
            "anthropic/claude-sonnet-5-5",
            "gpt-6-astra",
            "gpt-6-luna",
        ]) {
            assert!(arg.windows(2).any(|w| w == ["-m", model]));
        }
    }

    #[test]
    fn capacity_switch_order_starts_from_each_selected_model() {
        let t = Test::new();
        for (from, to) in [
            ("gpt-6.1-sol", "anthropic/claude-sonnet-5-5"),
            ("anthropic/claude-sonnet-5-5", "gpt-6.1-sol"),
            ("gpt-6-luna", "anthropic/claude-sonnet-5-5"),
        ] {
            let mut j = job(
                json!({"status":"failed","last_failure_kind":"capacity","model":from,"last_error":"Selected model is at capacity","thread_id":SESSION_ID,"retry_count":4,"retry_not_before":9999999999.,"context_phase":"continue","yield_resume":true,"turn_completed":true}),
            );
            assert!(t.ctx.retry_capacity(&mut j));
            assert_eq!(j["model"], to);
            assert_eq!(j["resume_session"], SESSION_ID);
            assert_eq!(j["retry_count"], 4);
            assert!(j["retry_not_before"].is_null());
            assert_eq!(j["context_phase"], "continue");
            assert_eq!(j["yield_resume"], true);
            assert!(j["turn_completed"].is_null());
            assert!(j["last_error"].is_null());
        }
    }

    #[test]
    fn capacity_does_not_restart_cancelled_completed_invalid_or_other_models() {
        let t = Test::new();
        for p in [
            json!({"status":"succeeded"}),
            json!({"status":"cancelled"}),
            json!({"cancel_requested":true}),
            json!({"model":"unknown-model"}),
            json!({"thread_id":"invalid","resume_session":"invalid"}),
            json!({"last_error":"429 Too Many Requests"}),
            json!({"last_error":"model not found"}),
            json!({"capacity_history":{}}),
        ] {
            let mut j = patch(
                job(
                    json!({"status":"failed","last_failure_kind":"capacity","model":"gpt-6.1-sol","last_error":"Selected model is at capacity","thread_id":SESSION_ID}),
                ),
                p,
            );
            let before = j.clone();
            assert!(!t.ctx.retry_capacity(&mut j));
            assert_eq!(j, before);
        }
    }

    #[test]
    fn capacity_falls_back_to_existing_resume_id_and_masks_history() {
        let t = Test::new();
        let secret = ["secret", "-synthetic-1793"].concat();
        t.rt.env
            .lock()
            .unwrap()
            .insert("OPENAI_API_KEY".into(), secret.clone());
        let mut j = job(
            json!({"status":"failed","last_failure_kind":"capacity","model":"anthropic/claude-sonnet-5-5","last_error":format!("Selected model is at capacity; key={secret}"),"thread_id":null,"resume_session":SESSION_ID}),
        );
        assert!(t.ctx.retry_capacity(&mut j));
        assert_eq!(j["resume_session"], SESSION_ID);
        assert!(!j.to_string().contains(&secret));
    }

    #[test]
    fn capacity_is_case_insensitive_and_is_infrastructure_failure() {
        assert!(is_capacity(
            "upstream: Selected Model Is At Capacity. Please try again."
        ));
        assert!(!is_capacity("storage is at capacity"));
        assert_eq!(
            auto_quality(
                &job(json!({"status":"failed","last_error":"Selected model is at capacity"})),
                None
            )
            .0,
            1
        );
    }

    #[test]
    fn capacity_switch_budget_survives_supervisor_restart() {
        let (t, saved) = supervise(
            usize::MAX,
            json!({"capacity_retry_count":2,"capacity_history":[{"attempt":1},{"attempt":2}],"model":"gpt-6-luna"}),
        );
        assert_eq!(saved["capacity_retry_count"], 3);
        assert_eq!(saved["capacity_history"].as_array().unwrap().len(), 3);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 2);
    }

    #[test]
    fn capacity_with_429_does_not_escape_the_switch_limit() {
        let t = Test::new();
        let j = t.put(job(json!({"id":"limit","name":"req-capacity-limit","model":"gpt-6.1-sol","cwd":t.ctx.root,"codex":"codex.CMD","capacity_retry_count":3})));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "continue").unwrap();
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(|| {
            Box::new(FakeChild {
            alive: Arc::new(AtomicBool::new(false)),
            out: Some(Box::new(Cursor::new(format!("{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"turn.failed\",\"error\":{{\"message\":\"429 Too Many Requests: Selected model is at capacity\"}}}}\n").into_bytes()))),
        })
        }));
        t.ctx.supervise("limit").unwrap();
        assert_eq!(t.ctx.load("limit").unwrap()["status"], "failed");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    }

    fn failed() -> Value {
        job(
            json!({"status":"failed","model":"gpt-6.1-sol","requested_model":"gpt-6.1-sol","last_error":"Selected model is at capacity","last_failure_kind":"capacity","thread_id":SESSION_ID}),
        )
    }

    #[test]
    fn quoted_capacity_text_never_establishes_failure_evidence() {
        let t = Test::new();
        for kind in [
            "agent_message",
            "reasoning",
            "command_execution",
            "mcp_tool_call",
            "web_search",
            "error",
        ] {
            let mut j = failed();
            j["last_failure_kind"] = Value::Null;
            apply_event(
                &mut j,
                &json!({"type":"item.completed","item":{"type":kind,"text":"Selected model is at capacity","message":"Selected model is at capacity","aggregated_output":"Selected model is at capacity"}}),
            );
            assert!(!t.ctx.retry_capacity(&mut j));
        }
        for (ev, kind) in [
            (
                json!({"type":"turn.failed","error":{"message":"429 Too Many Requests: Selected model is at capacity"}}),
                "capacity",
            ),
            (
                json!({"type":"error","message":"429 Too Many Requests"}),
                "quota",
            ),
            (
                json!({"type":"turn.failed","error":{"code":"request_failed","message":"connection closed"}}),
                "request_failed",
            ),
            (
                json!({"type":"turn.failed","error":{"code":"auth_failed","message":"Selected model is at capacity"}}),
                "auth",
            ),
            (
                json!({"type":"turn.failed","error":{"code":"cancelled","message":"Selected model is at capacity"}}),
                "cancel",
            ),
        ] {
            assert_eq!(cc_jobs::capacity::failure_kind(&ev), Some(kind));
        }
    }

    #[test]
    fn capacity_preserves_scope_wait_and_review_constraints() {
        let t = Test::new();
        let mut j = patch(
            failed(),
            json!({"todo_id":2528,"depends_on":[2510],"scope_source":"owner_request","read_only":true,"parent_job_id":"original","chrome_tabs":[{"id":"owned"}],"context_phase":"continue","yield_resume":true}),
        );
        let before = j.clone();
        assert!(t.ctx.retry_capacity(&mut j));
        for key in [
            "todo_id",
            "depends_on",
            "scope_source",
            "read_only",
            "parent_job_id",
            "chrome_tabs",
            "context_phase",
            "yield_resume",
        ] {
            assert_eq!(j[key], before[key], "{key}");
        }
        assert_eq!(j["model"], "anthropic/claude-sonnet-5-5");
        let again = j.clone();
        assert!(!t.ctx.retry_capacity(&mut j));
        assert_eq!(j, again);
        t.put(job(
            json!({"id":"single-file-author","model":"gpt-6.1-sol"}),
        ));
        let mut haiku = patch(
            failed(),
            json!({"requested_model":"anthropic/claude-haiku-5-5","model":"anthropic/claude-haiku-5-5","task_type":"single_file_review","parent_job_id":"single-file-author"}),
        );
        assert!(t.ctx.retry_capacity(&mut haiku));
        assert_eq!(haiku["model"], "gpt-6-luna");
        assert_eq!(haiku["task_type"], "single_file_review");
        assert_eq!(haiku["requested_model"], "anthropic/claude-haiku-5-5");
        let review = patch(
            failed(),
            json!({"model":"anthropic/claude-sonnet-5-5","task_type":"review"}),
        );
        assert!(cc_jobs::capacity::candidates(&review).is_empty());
    }

    #[test]
    fn capacity_cooldown_is_finite_and_does_not_create_owner_block() {
        let t = Test::new();
        for provider in ["openai", "anthropic"] {
            t.rt.quota_samples
                .lock()
                .unwrap()
                .insert(provider.into(), Default::default());
        }
        let mut j = failed();
        assert!(t.ctx.retry_capacity(&mut j));
        assert_eq!(j["status"], "queued");
        assert_eq!(j["capacity_wait_pending"], true);
        assert_eq!(j["retry_not_before"], t.rt.epoch() + 900.);
        assert!(j["capacity_retry_count"].is_null());
        assert!(t.rt.spawn_args.lock().unwrap().is_empty());
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        for expected in [2, 3] {
            t.rt.tick(900.);
            t.put(j.clone());
            j = t.ctx.load(s(&j, "id")).unwrap();
            assert!(t.ctx.recheck_capacity_wait(&mut j));
            assert_eq!(j["capacity_wait_count"], expected);
            assert_eq!(j["resume_session"], SESSION_ID);
        }
        t.rt.tick(900.);
        assert!(!t.ctx.recheck_capacity_wait(&mut j));
        assert_eq!(j["status"], "failed");
        assert_eq!(j["capacity_exhausted"], true);
        assert_eq!(j["capacity_wait_count"], 3);
        assert!(t.rt.spawn_args.lock().unwrap().is_empty());
    }

    #[test]
    fn capacity_access_denied_or_live_worker_never_restarts() {
        let t = Test::new();
        for denied in [true, false] {
            let mut j = patch(failed(), json!({"codex_pid":123,"codex_pid_created":45}));
            if denied {
                t.rt.proc_probe_errors.lock().unwrap().insert(123, 5);
            } else {
                t.rt.proc_probe_errors.lock().unwrap().clear();
                t.rt.procs.lock().unwrap().insert(123, 45);
            }
            let before = j.clone();
            assert!(!t.ctx.retry_capacity(&mut j));
            assert_eq!(j, before);
        }
    }

    #[test]
    fn generic_terminal_failure_preserves_protocol_capacity_but_auth_and_cancel_win() {
        for terminal in ["request_failed", "auth_failed", "cancelled"] {
            let mut j = failed();
            apply_event(
                &mut j,
                &json!({"type":"error","message":"Selected model is at capacity"}),
            );
            apply_event(
                &mut j,
                &json!({"type":"turn.failed","error":{"code":terminal,"message":"Request failed"}}),
            );
            let t = Test::new();
            assert_eq!(t.ctx.retry_capacity(&mut j), terminal == "request_failed");
        }
    }

    #[test]
    fn explicit_review_never_returns_to_the_source_authors_model() {
        let t = Test::new();
        t.put(job(
            json!({"id":"author","model":"gpt-6-luna","requested_model":"gpt-6.1-sol"}),
        ));
        let mut review = patch(
            failed(),
            json!({"model":"anthropic/claude-sonnet-5-5","requested_model":"anthropic/claude-sonnet-5-5","model_explicit":true,"parent_job_id":"author","task_type":"code_review"}),
        );
        assert!(!t.ctx.retry_capacity(&mut review));
        assert_eq!(review["model"], "anthropic/claude-sonnet-5-5");
        assert_eq!(review["capacity_exhausted"], true);
    }

    #[test]
    fn depleted_claude_switches_to_verified_codex_oauth_and_single_file_summary_stays_light() {
        let t = Test::new();
        let mut j = patch(
            failed(),
            json!({"model":"anthropic/claude-sonnet-5-5","requested_model":"anthropic/claude-sonnet-5-5"}),
        );
        t.rt.quota_samples
            .lock()
            .unwrap()
            .insert("anthropic".into(), Default::default());
        assert!(t.ctx.retry_capacity(&mut j));
        assert_eq!(j["model"], "gpt-6.1-sol");
        assert_eq!(j["capacity_history"][0]["to_provider"], "openai");
        let mut summary = patch(
            failed(),
            json!({"model":"anthropic/claude-haiku-5-5","requested_model":"anthropic/claude-haiku-5-5","task_type":"single_file_summary"}),
        );
        assert!(t.ctx.retry_capacity(&mut summary));
        assert_eq!(summary["model"], "gpt-6-luna");
        assert_eq!(summary["task_type"], "single_file_summary");
    }

    #[test]
    fn due_capacity_wait_rechecks_quota_and_preserves_the_same_job_after_restart() {
        let t = Test::new();
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        for provider in ["openai", "anthropic"] {
            t.rt.quota_samples
                .lock()
                .unwrap()
                .insert(provider.into(), Default::default());
        }
        let mut j = failed();
        assert!(t.ctx.retry_capacity(&mut j));
        let id = s(&j, "id").to_owned();
        t.put(j);
        let mut restarted = t.ctx.load(&id).unwrap();
        assert!(!t.ctx.recheck_capacity_wait(&mut restarted));
        t.rt.tick(900.);
        t.rt.quota_samples.lock().unwrap().clear();
        assert!(t.ctx.recheck_capacity_wait(&mut restarted));
        assert_eq!(restarted["id"], id);
        assert_eq!(restarted["resume_session"], SESSION_ID);
        assert_eq!(restarted["capacity_retry_count"], 1);
        assert_eq!(restarted["capacity_wait_count"], 1);
        assert_eq!(restarted["capacity_wait_pending"], false);
    }

    #[test]
    fn capacity_before_session_creation_preserves_job_and_original_prompt_scope() {
        let t = Test::new();
        let mut j = patch(
            failed(),
            json!({"thread_id":null,"resume_session":null,"todo_id":2528,"prompt":"original.md"}),
        );
        let id = j["id"].clone();
        assert!(t.ctx.retry_capacity(&mut j));
        assert_eq!(j["id"], id);
        assert_eq!(j["prompt"], "original.md");
        assert_eq!(j["todo_id"], 2528);
        assert_eq!(j["resume_session"], "");
        assert!(!codex_args(&j).iter().any(|a| a == "resume"));
    }
}
mod queue_regressions {
    use super::*;
    use cc_jobs::queue::*;
    #[test]
    fn unreconciled_legacy_identity_is_checked_before_execution_and_on_pid_reuse() {
        for identity in ["todo", "session", "related-todo"] {
            let t = Test::new();
            config(&t, json!({"max_running":10}));
            let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
            let mut old = job(
                json!({"id":"legacy","name":"old","status":"succeeded","codex_pid":888,"codex_pid_created":77}),
            );
            let args = match identity {
                "todo" => {
                    old["todo_id"] = json!(77);
                    vec!["--todo-id", "77"]
                }
                "related-todo" => {
                    old["related_todo_ids"] = json!([77]);
                    vec!["--todo-id", "77"]
                }
                _ => {
                    old["thread_id"] = json!(session);
                    vec!["--resume-session", session]
                }
            };
            t.put(old);
            t.rt.procs.lock().unwrap().insert(888, 77);
            // Intake must not scan this terminal-looking legacy record. Its
            // surviving writer is discovered by reconciliation before admit.
            assert_eq!(t.run(&args, b"new receipt").unwrap(), 0);
            let new = t
                .ctx
                .list_jobs(&HashSet::new())
                .into_iter()
                .find(|job| job["id"] != "legacy")
                .unwrap();
            let id = s(&new, "id");
            assert!(t.ctx.admit(id).unwrap().is_none(), "{identity}");
            assert_eq!(
                t.ctx.load(id).unwrap()["queue_wait_reason"]["code"],
                "intake_owner"
            );
            // A PID with another creation time must not keep the old claim.
            t.rt.procs.lock().unwrap().insert(888, 78);
            assert!(t.ctx.admit(id).unwrap().is_some(), "{identity}");
        }
    }
    #[test]
    fn unreadable_indexed_session_keeps_its_identity_with_spare_capacity() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
        t.put(job(
            json!({"id":"legacy","status":"running","queue_unreadable":true,"thread_id":session}),
        ));
        drop(t.ctx.queue_lock().unwrap());
        fs::write(t.ctx.job_dir("legacy").join("job.json"), "broken").unwrap();
        t.put(job(
            json!({"id":"next","name":"different","resume_session":session}),
        ));
        {
            let _gate = t.ctx.queue_lock().unwrap();
            t.ctx.track_queued_job("next").unwrap();
        }
        assert!(t.ctx.admit("next").unwrap().is_none());
        assert_eq!(
            t.ctx.load("next").unwrap()["queue_wait_reason"]["code"],
            "intake_owner"
        );
    }
    #[test]
    fn same_todo_children_share_family_but_a_shared_session_still_waits() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
        t.rt.procs.lock().unwrap().insert(888, 77);
        t.put(job(json!({"id":"parent","name":"parent","todo_id":77,"status":"running","codex_pid":888,"codex_pid_created":77,"thread_id":session})));
        t.put(job(
            json!({"id":"child","name":"review","todo_id":77,"parent_job_id":"parent"}),
        ));
        assert!(t.ctx.admit("child").unwrap().is_some());
        t.put(job(json!({"id":"same-session-child","name":"resume","todo_id":77,"parent_job_id":"parent","resume_session":session})));
        assert!(t.ctx.admit("same-session-child").unwrap().is_none());
        assert_eq!(
            t.ctx.load("same-session-child").unwrap()["queue_wait_reason"]["code"],
            "intake_owner"
        );
        t.put(job(
            json!({"id":"other-root","name":"unrelated","todo_id":77}),
        ));
        assert!(t.ctx.admit("other-root").unwrap().is_none());
        assert_eq!(
            t.ctx.load("other-root").unwrap()["queue_wait_reason"]["code"],
            "intake_owner"
        );
    }
    #[test]
    fn registration_owns_state_before_publishing_a_queued_job() {
        let t = Test::new();
        let reads = Arc::new(AtomicUsize::new(0));
        let observed = reads.clone();
        *t.rt.save_hook.lock().unwrap() = Some(Box::new(move |file, _| {
            if file.file_name().is_some_and(|name| name == "job.json") {
                assert!(
                    FileLock::try_acquire(&file.with_file_name("state.lock")).is_err(),
                    "registration must reserve state before first publication"
                );
                reads.fetch_add(1, Ordering::Relaxed);
            }
        }));
        assert_eq!(t.run(&[], b"fixture registration").unwrap(), 0);
        assert!(observed.load(Ordering::Relaxed) > 0);
    }
    #[test]
    fn history_bootstrap_and_refresh_do_not_hold_the_queue_gate() {
        let t = Test::new();
        for n in 0..32 {
            t.put(job(
                json!({"id":format!("history-{n}"),"status":"succeeded"}),
            ));
        }
        let path = t.ctx.jobs.join(".queue.lock");
        let reads = Arc::new(AtomicUsize::new(0));
        let count = reads.clone();
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |file, _| {
            if file.file_name().is_some_and(|name| name == "job.json") {
                let gate = FileLock::try_acquire(&path)
                    .expect("terminal history must be read outside the queue gate");
                drop(gate);
                count.fetch_add(1, Ordering::Relaxed);
            }
        }));
        drop(t.ctx.queue_lock().unwrap());
        assert_eq!(reads.load(Ordering::Relaxed), 32);
        drop(t.ctx.queue_lock().unwrap());
        assert_eq!(
            reads.load(Ordering::Relaxed),
            32,
            "bootstrap must be shared"
        );
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.rt.tick(31.);
        drop(t.ctx.queue_lock().unwrap());
        assert_eq!(reads.load(Ordering::Relaxed), 64);
    }
    #[test]
    fn scan_commit_merges_a_registration_published_after_history_was_read() {
        let t = Test::new();
        t.put(job(json!({"id":"history-first","status":"succeeded"})));
        let jobs = t.ctx.jobs.clone();
        let writer = Context {
            rt: Arc::new(Fake::new()),
            ..t.ctx.clone()
        };
        let once = Arc::new(AtomicBool::new(false));
        let observed = once.clone();
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |file, _| {
            if file.file_name().is_some_and(|name| name == "job.json")
                && !once.swap(true, Ordering::Relaxed)
            {
                let _gate = FileLock::try_acquire(&jobs.join(".queue.lock")).unwrap();
                let dir = jobs.join("late-registration");
                fs::create_dir(&dir).unwrap();
                fs::write(
                    dir.join("job.json"),
                    pretty(&job(json!({"id":"late-registration"}))),
                )
                .unwrap();
                writer.track_queued_job("late-registration").unwrap();
            }
        }));
        drop(t.ctx.queue_lock().unwrap());
        assert!(observed.load(Ordering::Relaxed));
        let index: Value =
            serde_json::from_slice(&fs::read(t.ctx.jobs.join(".queue-index.json")).unwrap())
                .unwrap();
        assert_eq!(index["ids"], json!(["late-registration"]));
    }
    #[test]
    fn partial_index_replaced_after_preparation_cannot_admit_a_job() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        t.rt.procs.lock().unwrap().insert(888, 77);
        t.put(job(
            json!({"id":"orphan","status":"running","codex_pid":888,"codex_pid_created":77}),
        ));
        waiting(&t, "waiting", "ordinary");
        let gate = t.ctx.jobs.join(".queue.lock");
        let once = Arc::new(AtomicBool::new(false));
        let changed = once.clone();
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |file, _| {
            if file
                .file_name()
                .is_some_and(|name| name == ".queue-index.json")
                && FileLock::try_acquire(&gate).is_err()
                && !once.swap(true, Ordering::Relaxed)
            {
                fs::write(
                    file,
                    r#"{"scan_complete":false,"scanned_at":0,"ids":["waiting"]}"#,
                )
                .unwrap();
            }
        }));
        let error = t
            .ctx
            .admit("waiting")
            .err()
            .expect("partial index must defer admission");
        assert_eq!(error.code, 75);
        assert!(changed.load(Ordering::Relaxed));
        *t.rt.load_hook.lock().unwrap() = None;
        assert_eq!(t.ctx.load("waiting").unwrap()["status"], "queued");
        assert!(t.ctx.admit("waiting").unwrap().is_none());
    }
    #[test]
    fn scan_commit_read_failure_preserves_concurrent_registration() {
        let t = Test::new();
        t.put(job(json!({"id":"history-first","status":"succeeded"})));
        let writer = Context {
            rt: Arc::new(Fake::new()),
            ..t.ctx.clone()
        };
        let jobs = t.ctx.jobs.clone();
        let failed_path = t.rt.load_error_path.clone();
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |file, _| {
            if file.file_name().is_some_and(|name| name == "job.json") {
                let _gate = FileLock::try_acquire(&jobs.join(".queue.lock")).unwrap();
                let dir = jobs.join("late-registration");
                fs::create_dir(&dir).unwrap();
                fs::write(
                    dir.join("job.json"),
                    pretty(&job(json!({"id":"late-registration"}))),
                )
                .unwrap();
                writer.track_queued_job("late-registration").unwrap();
                *failed_path.lock().unwrap() = Some(jobs.join(".queue-index.json"));
            }
        }));
        assert!(t.ctx.queue_lock().is_err());
        *t.rt.load_hook.lock().unwrap() = None;
        *t.rt.load_error_path.lock().unwrap() = None;
        let index: Value =
            serde_json::from_slice(&fs::read(t.ctx.jobs.join(".queue-index.json")).unwrap())
                .unwrap();
        assert_eq!(index["scan_complete"], false);
        assert_eq!(index["ids"], json!(["late-registration"]));
        drop(t.ctx.queue_lock().unwrap());
        let index: Value =
            serde_json::from_slice(&fs::read(t.ctx.jobs.join(".queue-index.json")).unwrap())
                .unwrap();
        assert_eq!(index["scan_complete"], true);
        assert_eq!(index["ids"], json!(["late-registration"]));
    }
    #[test]
    fn unreadable_owner_record_keeps_cached_pr_claim_without_blocking_other_prs() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        let mut owner = waiting(&t, "owner", "orch-pr86");
        owner["work_owner"] = json!({"repository":"o/r","pr":86});
        t.ctx.save(&owner).unwrap();
        assert!(t.ctx.admit("owner").unwrap().is_some());
        let mut duplicate = waiting(&t, "duplicate", "req-pr86");
        duplicate["work_owner"] = owner["work_owner"].clone();
        t.ctx.save(&duplicate).unwrap();
        assert!(t.ctx.admit("duplicate").unwrap().is_none());
        fs::write(t.ctx.job_dir("owner").join("job.json"), "broken").unwrap();
        assert!(t.ctx.admit("duplicate").unwrap().is_none());
        assert_eq!(
            t.ctx.load("duplicate").unwrap()["queue_wait_reason"]["owner_job_id"],
            "owner"
        );
        let mut other = waiting(&t, "other", "req-pr87");
        other["work_owner"] = json!({"repository":"o/r","pr":87});
        t.ctx.save(&other).unwrap();
        assert!(t.ctx.admit("other").unwrap().is_some());
    }
    #[test]
    fn later_normal_priority_does_not_deadlock_earlier_low_priority_owner() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        let mut first = waiting(&t, "first", "orch-todo-18");
        first["todo_id"] = json!(18);
        first["queue_sequence"] = json!(1);
        first["work_owner"] = json!({"repository":"adj/game","pr":86});
        t.ctx.save(&first).unwrap();
        let mut later = waiting(&t, "later", "req-pr86");
        later["queue_sequence"] = json!(2);
        later["work_owner"] = first["work_owner"].clone();
        t.ctx.save(&later).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_none());
        assert!(t.ctx.admit("first").unwrap().is_some());
    }
    #[test]
    fn readiness_wait_added_after_registration_blocks_admission_until_released() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        let mut candidate = waiting(&t, "original", "orch-todo-1139");
        candidate["todo_id"] = json!(1139);
        t.ctx.save(&candidate).unwrap();
        let mut db = json!({"items":[{"id":1139,"status":"wait","wait_ready":{"condition":"retry-time","since":100,"not_before":3700}}]});
        fs::write(&store, db.to_string()).unwrap();
        assert!(t.ctx.admit("original").unwrap().is_none());
        assert_eq!(
            t.ctx.load("original").unwrap()["queue_wait_reason"]["code"],
            "todo_readiness"
        );
        assert_eq!(t.ctx.load("original").unwrap()["status"], "queued");
        // The queue cannot claim recovery merely because wall time passed. The watchdog releases it.
        db["items"][0]["status"] = json!("todo");
        db["items"][0].as_object_mut().unwrap().remove("wait_ready");
        fs::write(&store, db.to_string()).unwrap();
        assert!(t.ctx.admit("original").unwrap().is_some());
    }
    #[test]
    fn timed_retry_rejects_new_supervisor_registration_without_writing_a_job() {
        let t = Test::new();
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        let db = json!({"items":[{"id":1139,"status":"wait","wait_ready":{"condition":"retry-time","since":100,"not_before":3700}}]});
        fs::write(&store, db.to_string()).unwrap();
        let before = t.ctx.list_jobs(&HashSet::new());
        let error = t
            .run(&["--todo-id", "1139"], b"existing task check")
            .unwrap_err();
        assert!(error.message.contains("次の再確認"));
        assert_eq!(t.ctx.list_jobs(&HashSet::new()), before);
        assert_eq!(
            serde_json::from_slice::<Value>(&fs::read(&store).unwrap()).unwrap(),
            db
        );
        assert!(t.run(&[], b"unrelated finite work").is_ok());
    }
    #[test]
    fn initial_todo_creation_cannot_race_running_publication() {
        for directory_exists in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":10}));
            let home = t.ctx.root.join("initial-todo");
            if directory_exists {
                fs::create_dir(&home).unwrap();
            }
            t.rt.env
                .lock()
                .unwrap()
                .insert("CC_TODO_HOME".into(), home.to_string_lossy().into_owned());
            let store = home.join("todo.json");
            let mut candidate = waiting(&t, "initial", "orch-todo-1139");
            candidate["todo_id"] = json!(1139);
            t.ctx.save(&candidate).unwrap();
            let attempts = Arc::new(Mutex::new(Vec::new()));
            let observed = attempts.clone();
            let path = t.ctx.job_dir("initial").join("job.json");
            let lock = home.join("todo.lock");
            let store_at_save = store.clone();
            *t.rt.save_hook.lock().unwrap() = Some(Box::new(move |file, _| {
                if file == path {
                    fs::create_dir_all(lock.parent().unwrap()).unwrap();
                    match FileLock::try_acquire(&lock) {
                        Ok(_writer) => {
                            observed.lock().unwrap().push(false);
                            fs::write(&store_at_save, json!({"items":[{"id":1139,"status":"wait","wait_on":[2]},{"id":2,"status":"doing"}]}).to_string()).unwrap();
                        }
                        Err(_) => observed.lock().unwrap().push(true),
                    }
                }
            }));
            assert!(t.ctx.admit("initial").unwrap().is_some());
            *t.rt.save_hook.lock().unwrap() = None;
            assert_eq!(
                *attempts.lock().unwrap(),
                vec![true],
                "directory exists: {directory_exists}"
            );
            assert!(!store.exists());
            // The writer can register a wait once publication has released the same lock.
            let _writer = FileLock::try_acquire(&home.join("todo.lock")).unwrap();
            fs::write(&store, json!({"items":[{"id":1139,"status":"wait","wait_on":[2]},{"id":2,"status":"doing"}]}).to_string()).unwrap();
        }
    }
    #[test]
    fn wait_registration_cannot_race_running_publication_after_queue_snapshot() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        let db = json!({"items":[{"id":1139,"status":"todo"}]});
        fs::write(&store, db.to_string()).unwrap();
        let mut candidate = waiting(&t, "original", "orch-todo-1139");
        candidate["todo_id"] = json!(1139);
        t.ctx.save(&candidate).unwrap();
        let attempts = Arc::new(Mutex::new(Vec::new()));
        let observed = attempts.clone();
        let path = t.ctx.job_dir("original").join("job.json");
        let todo_lock = store.with_file_name("todo.lock");
        let store_at_save = store.clone();
        *t.rt.save_hook.lock().unwrap() = Some(Box::new(move |file, _| {
            if file == path {
                match FileLock::try_acquire(&todo_lock) {
                    Ok(_guard) => {
                        observed.lock().unwrap().push(false);
                        // A writer admitted here would install a wait between the snapshot and publication.
                        let wait = json!({"items":[{"id":1139,"status":"wait","wait_ready":{"condition":"retry-time","since":100,"not_before":3700}}]});
                        let _ = fs::write(&store_at_save, wait.to_string());
                    }
                    Err(_) => observed.lock().unwrap().push(true),
                }
            }
        }));
        let admission = t.ctx.admit("original").unwrap();
        *t.rt.save_hook.lock().unwrap() = None;
        assert!(admission.is_some());
        assert_eq!(t.ctx.load("original").unwrap()["status"], "running");
        assert_eq!(*attempts.lock().unwrap(), vec![true]);
        assert_eq!(
            serde_json::from_slice::<Value>(&fs::read(&store).unwrap()).unwrap(),
            db
        );
    }
    #[test]
    fn duplicate_wait_is_durable_across_owner_worker_exit_until_todo_done() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        let mut db = json!({"items":[{"id":880,"status":"doing"},{"id":1595,"status":"todo"}]});
        fs::write(&store, db.to_string()).unwrap();
        let mut first = waiting(&t, "first", "orch-todo-880");
        first["todo_id"] = json!(880);
        first["queue_sequence"] = json!(1);
        first["work_owner"] = json!({"repository":"adj/game","pr":86});
        t.ctx.save(&first).unwrap();
        let mut later = waiting(&t, "later", "orch-todo-1595");
        later["todo_id"] = json!(1595);
        later["queue_sequence"] = json!(2);
        later["work_owner"] = first["work_owner"].clone();
        t.ctx.save(&later).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_none());
        assert_eq!(t.ctx.load("later").unwrap()["work_wait_on"], json!([880]));
        first["status"] = json!("succeeded");
        t.ctx.save(&first).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_none());
        assert_eq!(
            t.ctx.load("later").unwrap()["queue_wait_reason"]["code"],
            "todo_dependency"
        );
        db["items"][0]["status"] = json!("done");
        fs::write(&store, db.to_string()).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_some());
    }
    #[test]
    fn legacy_home_git_wait_is_reevaluated_and_archived_without_rewaiting() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        t.rt.env
            .lock()
            .unwrap()
            .insert("CC_TODO_HOME".into(), t.ctx.root.to_string_lossy().into());
        assert!(
            std::process::Command::new("git")
                .arg("init")
                .arg(&t.ctx.root)
                .output()
                .unwrap()
                .status
                .success()
        );
        let a = t.ctx.root.join("a");
        let b = t.ctx.root.join("b");
        fs::create_dir(&a).unwrap();
        fs::create_dir(&b).unwrap();
        let db = json!({"projects":{"a":a,"b":b},"items":[{"id":880,"status":"todo"},{"id":1595,"status":"wait","wait_on":[880],"work_owner_job":"first","work_owner_wait_on":880,"jobs":["later"]}]});
        fs::write(t.ctx.root.join("todo.json"), db.to_string()).unwrap();
        let identity = json!({"worktree":cc_job_policy::work_owner::norm(&t.ctx.root),"git_common":cc_job_policy::work_owner::norm(&t.ctx.root.join(".git")),"root_kind":"user_home","branch":"fix/other"});
        let mut first = waiting(&t, "first", "orch-first");
        first["todo_id"] = json!(880);
        first["cwd"] = json!(a);
        first["project"] = json!("a");
        first["work_owner"] = identity.clone();
        first["status"] = json!("succeeded");
        t.ctx.save(&first).unwrap();
        let mut later = waiting(&t, "later", "orch-later");
        later["todo_id"] = json!(1595);
        later["cwd"] = json!(b);
        later["project"] = json!("b");
        later["work_owner"] = identity;
        later["work_wait_on"] = json!([880]);
        later["queue_wait_reason"] = json!({"code":"todo_dependency"});
        t.ctx.save(&later).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_some());
        let saved = t.ctx.load("later").unwrap();
        assert_eq!(saved["work_wait_on"], json!([]));
        assert_eq!(saved["work_wait_history"][0]["wait_on"], json!([880]));
        assert_eq!(saved["work_owner"]["scope"], "registered_folder");
        assert!(saved["queue_wait_reason"].is_null());
        // Admission reads the current ToDo store but never rewrites it outside its lock.
        assert_eq!(
            serde_json::from_slice::<Value>(&fs::read(t.ctx.root.join("todo.json")).unwrap())
                .unwrap(),
            db
        );
    }
    #[test]
    fn manual_db_wait_survives_disproved_automatic_queue_wait() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        t.rt.env
            .lock()
            .unwrap()
            .insert("CC_TODO_HOME".into(), t.ctx.root.to_string_lossy().into());
        let db = json!({"items":[{"id":880,"status":"todo"},{"id":1595,"status":"wait","wait_on":[880],"work_owner_job":"first","jobs":["later"]}]});
        fs::write(t.ctx.root.join("todo.json"), db.to_string()).unwrap();
        let mut first = waiting(&t, "first", "orch-first");
        first["todo_id"] = json!(880);
        first["work_owner"] = json!({"worktree":"/a"});
        first["status"] = json!("succeeded");
        t.ctx.save(&first).unwrap();
        let mut later = waiting(&t, "later", "orch-later");
        later["todo_id"] = json!(1595);
        later["work_owner"] = json!({"worktree":"/b"});
        later["work_wait_on"] = json!([880]);
        later["queue_wait_reason"] = json!({"code":"work_owner","owner_job_id":"first"});
        t.ctx.save(&later).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_none());
        let saved = t.ctx.load("later").unwrap();
        assert_eq!(saved["work_wait_on"], json!([]));
        assert_eq!(saved["queue_wait_reason"]["code"], "todo_dependency");
        assert_eq!(saved["queue_wait_reason"]["wait_on"], json!([880]));
        assert_eq!(
            serde_json::from_slice::<Value>(&fs::read(t.ctx.root.join("todo.json")).unwrap())
                .unwrap(),
            db
        );
    }
    #[test]
    fn git_metadata_recognizes_subdirectories_and_linked_worktrees_without_shells() {
        let t = Test::new();
        let git = t.ctx.root.join(".git");
        fs::create_dir(&git).unwrap();
        fs::write(
            git.join("config"),
            "[remote \"origin\"]\n url = git@github.com:AutoDevJapan/game.git\n",
        )
        .unwrap();
        fs::write(git.join("HEAD"), "ref: refs/heads/main\n").unwrap();
        let sub = t.ctx.root.join("src");
        fs::create_dir(&sub).unwrap();
        let a = cc_job_policy::work_owner::checkout(sub.to_str().unwrap());
        assert_eq!(a["repository"], "autodevjapan/game");
        assert_eq!(a["branch"], "main");
        let wt = t.ctx.root.join("linked");
        fs::create_dir(&wt).unwrap();
        let meta = git.join("worktrees").join("linked");
        fs::create_dir_all(&meta).unwrap();
        fs::write(wt.join(".git"), format!("gitdir: {}\n", meta.display())).unwrap();
        fs::write(meta.join("commondir"), "../..\n").unwrap();
        fs::write(meta.join("HEAD"), "ref: refs/heads/main\n").unwrap();
        let b = cc_job_policy::work_owner::checkout(wt.to_str().unwrap());
        assert_ne!(a["worktree"], b["worktree"]);
        assert_eq!(a["git_common"], b["git_common"]);
        assert!(cc_job_policy::work_owner::same(&a, &b));
        let c = cc_job_policy::work_owner::checkout(t.ctx.root.to_str().unwrap());
        assert_eq!(a["worktree"], c["worktree"]);
    }
    #[test]
    fn same_pr_different_todos_wait_at_admission_but_child_review_can_run() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        let mut a = waiting(&t, "owner", "orch-todo-880");
        a["todo_id"] = json!(880);
        a["created_at"] = json!("2026-10-01T10:00:00+09:00");
        a["work_owner"] = json!({"repository":"adj/game","pr":86,"worktree":"/owner"});
        t.ctx.save(&a).unwrap();
        let mut b = waiting(&t, "later", "req-todo-1595");
        b["todo_id"] = json!(1595);
        b["work_owner"] = json!({"repository":"adj/game","pr":86,"worktree":"/later"});
        t.ctx.save(&b).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_none());
        assert_eq!(
            t.ctx.load("later").unwrap()["queue_wait_reason"]["owner_job_id"],
            "owner"
        );
        assert!(t.ctx.admit("owner").unwrap().is_some());
        let mut review = waiting(&t, "review", "rev-pr86");
        review["parent_job_id"] = json!("owner");
        review["work_owner"] = a["work_owner"].clone();
        t.ctx.save(&review).unwrap();
        assert!(t.ctx.admit("review").unwrap().is_some());
        let mut owner = t.ctx.load("owner").unwrap();
        owner["status"] = json!("succeeded");
        t.ctx.save(&owner).unwrap();
        assert!(t.ctx.admit("later").unwrap().is_some());
    }
    #[test]
    fn concurrent_same_pr_admission_reserves_one_root() {
        let t = Test::new();
        config(&t, json!({"max_running":10}));
        for id in ["first", "second"] {
            let mut j = waiting(&t, id, "req-pr86");
            j["work_owner"] = json!({"repository":"adj/game","pr":86,"worktree":id});
            t.ctx.save(&j).unwrap();
        }
        std::thread::scope(|scope| {
            let one = scope.spawn(|| t.ctx.admit("first").unwrap().is_some());
            let two = scope.spawn(|| t.ctx.admit("second").unwrap().is_some());
            assert_ne!(one.join().unwrap(), two.join().unwrap());
        });
        assert_eq!(
            ["first", "second"]
                .iter()
                .filter(|id| t.ctx.load(id).unwrap()["status"] == "running")
                .count(),
            1
        );
    }
    #[test]
    fn failed_supervisor_upgrade_survives_refresh_and_is_retried_next_minute() {
        let t = Test::new();
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        config(&t, json!({"max_running":4,"adaptive":{}}));
        t.rt.procs.lock().unwrap().insert(601, 123);
        t.put(json!({"id":"old-wait","status":"queued","queued_at":"2020-01-01T00:00:00+09:00","pid":601,"pid_created":123,"queue_supervisor_version":4}));
        t.rt.supervisor_fail.store(true, Ordering::Relaxed);
        t.ctx.adjust_limit().unwrap();
        let failed = t.ctx.load("old-wait").unwrap();
        assert_eq!(failed["queue_restart_pending"], true);
        assert_eq!(t.ctx.refresh(failed, false).unwrap()["status"], "queued");
        t.rt.tick(180.);
        assert_eq!(
            t.ctx
                .refresh(t.ctx.load("old-wait").unwrap(), false)
                .unwrap()["status"],
            "queued"
        );
        t.rt.supervisor_fail.store(false, Ordering::Relaxed);
        t.ctx.adjust_limit().unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        assert_eq!(&*t.rt.kills.lock().unwrap(), &[601]);
    }
    #[test]
    fn minute_controller_upgrades_only_legacy_queued_supervisors_and_rechecks_later_requeues() {
        let t = Test::new();
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        config(&t, json!({"max_running":4,"adaptive":{}}));
        for (id, pid, status, version, child) in [
            ("old-wait", 601, "queued", 4, None),
            ("old-run", 602, "running", 4, None),
            ("new-wait", 603, "queued", 5, None),
            ("live-child", 604, "queued", 4, Some(605)),
        ] {
            t.rt.procs.lock().unwrap().insert(pid, 123);
            if let Some(child) = child {
                t.rt.procs.lock().unwrap().insert(child, 123);
            }
            t.put(json!({"id":id,"status":status,"pid":pid,"pid_created":123,"queue_supervisor_version":version,"codex_pid":child,"codex_pid_created":123}));
        }
        t.ctx.adjust_limit().unwrap();
        assert_eq!(&*t.rt.kills.lock().unwrap(), &[601]);
        let mut ack = t.ctx.load("old-wait").unwrap();
        assert_eq!(ack["queue_supervisor_version"], 4);
        ack["queue_supervisor_version"] = json!(5);
        ack["queue_restart_pending"] = json!(false);
        t.ctx.save(&ack).unwrap();
        assert_eq!(t.ctx.load("old-run").unwrap()["status"], "running");
        let mut job = t.ctx.load("old-run").unwrap();
        job["status"] = json!("queued");
        t.ctx.save(&job).unwrap();
        t.rt.tick(60.);
        t.ctx.adjust_limit().unwrap();
        assert_eq!(&*t.rt.kills.lock().unwrap(), &[601, 602]);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
    }
    #[test]
    fn only_global_slot_waiters_justify_growth_not_cooldown_or_dependency_waits() {
        for ready in [false, true] {
            let t = Test::new();
            let now = t.rt.epoch();
            t.rt.env.lock().unwrap().insert(
                "CC_TODO_HOME".into(),
                t.ctx.root.to_string_lossy().into_owned(),
            );
            fs::write(
                t.ctx.root.join("todo.json"),
                json!({"items":[{"id":1,"status":"wait","wait_on":[2]},{"id":2,"status":"doing"}]})
                    .to_string(),
            )
            .unwrap();
            config(&t, json!({"max_running":4,"adaptive":{}}));
            for n in 0..4 {
                let mut j = waiting(&t, &format!("busy{n}"), &format!("busy{n}"));
                j["status"] = json!("running");
                t.ctx.save(&j).unwrap();
            }
            let mut cooldown = waiting(&t, "cooldown", "cooldown");
            cooldown["retry_not_before"] = json!(now + 900.);
            t.ctx.save(&cooldown).unwrap();
            let mut dependency = waiting(&t, "dependency", "dependency");
            dependency["todo_id"] = json!(1);
            t.ctx.save(&dependency).unwrap();
            if ready {
                waiting(&t, "ready", "ready");
            }
            let settings = cc_jobs::adaptive::AdaptiveConfig::default();
            let quota = cc_jobs::adaptive::parse_quotas(
                &json!({"accounts":[{"id":"a","quota":{"weeklyPercent":20.,"weeklyResetAt":now+86400.}}]}),
                now,
            );
            let state = cc_jobs::adaptive::LimitState {
                checked_at: now - 60.,
                first_at: now - 600.,
                stable_since: now - 600.,
                max_running: 4,
                mode: "auto".into(),
                settings: Some(settings),
                quota_checked_at: now,
                openai: vec![quota],
                machine: cc_jobs::adaptive::MachineSample {
                    cpu_times: Some([100, 200, 100]),
                    ..Default::default()
                },
                ..Default::default()
            };
            t.ctx
                .write_json(&t.ctx.root.join("queue-limit-state.json"), &json!(state))
                .unwrap();
            t.rt.queue_sample.lock().unwrap().cpu_times = Some([180, 300, 100]);
            t.ctx.adjust_limit().unwrap();
            assert_eq!(
                t.ctx.queue_config().unwrap().max_running,
                if ready { 6 } else { 4 }
            );
        }
    }
    #[test]
    fn minute_schedule_jitter_does_not_skip_next_bucket_or_reset_state() {
        let t = Test::new();
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.rt.tick(40.);
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":25,"adaptive":{}}),
            )
            .unwrap();
        t.ctx.adjust_limit().unwrap();
        t.rt.tick(59.);
        t.ctx.adjust_limit().unwrap();
        assert_eq!(t.rt.queue_samples.load(Ordering::Relaxed), 2);
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 25);
        t.rt.tick(121.);
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 25);
    }
    #[test]
    fn journal_io_error_does_not_publish_new_state_or_discard_history() {
        let t = Test::new();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":25,"adaptive":{}}),
            )
            .unwrap();
        let path = t.ctx.root.join("queue-limit-state.json");
        t.ctx
            .write_json(
                &path,
                &json!({"mode":"auto","max_running":12,"checked_at":t.rt.epoch()}),
            )
            .unwrap();
        let before = fs::read(&path).unwrap();
        fs::create_dir(t.ctx.root.join("queue-limit-history.json")).unwrap();
        assert!(t.ctx.adjust_limit().is_err());
        assert_eq!(fs::read(&path).unwrap(), before);
        assert!(t.ctx.root.join("queue-limit-history.json").is_dir());
    }
    #[test]
    fn automatic_limit_is_shared_once_per_minute_and_fixed_override_is_immediate() {
        let t = Test::new();
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":25,"adaptive":{}}),
            )
            .unwrap();
        t.ctx.adjust_limit().unwrap();
        t.ctx.adjust_limit().unwrap();
        assert_eq!(t.rt.queue_samples.load(Ordering::Relaxed), 1);
        let before = t
            .ctx
            .read_json(&t.ctx.root.join("queue-limit-state.json"))
            .unwrap();
        let (r, out, _) = t.cli(&["status", "--json", "--summary"]);
        assert_eq!(r.unwrap(), 0);
        let report: Value = serde_json::from_str(&out).unwrap();
        assert!(s(&report["summary"], "limit_label").contains("上限 25（自動"));
        assert_eq!(
            before,
            t.ctx
                .read_json(&t.ctx.root.join("queue-limit-state.json"))
                .unwrap()
        );
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":25,"adaptive":{},"fixed_max_running":7}),
            )
            .unwrap();
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 7);
        t.ctx.adjust_limit().unwrap();
        assert_eq!(t.rt.queue_samples.load(Ordering::Relaxed), 2);
        let s = t.ctx.limit_status(&t.ctx.queue_config().unwrap()).unwrap();
        assert_eq!(s["mode"], "fixed");
    }
    #[test]
    fn lowering_limit_preserves_live_jobs_and_blocks_new_admission() {
        let t = Test::new();
        for n in 0..5 {
            t.put(json!({"id":format!("running{n}"),"status":"running","pid":std::process::id(),"pid_created":123}));
        }
        t.put(json!({"id":"waiting","status":"queued","queued_at":t.rt.now()}));
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":25,"adaptive":{}}),
            )
            .unwrap();
        let now = t.rt.epoch();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-limit-state.json"),
                &json!({"mode":"auto","max_running":4,"checked_at":now}),
            )
            .unwrap();
        assert!(t.ctx.admit("waiting").unwrap().is_none());
        assert!(t.rt.kills.lock().unwrap().is_empty());
        assert_eq!(t.rt.queue_samples.load(Ordering::Relaxed), 0);
        for n in 0..5 {
            assert_eq!(
                t.ctx.load(&format!("running{n}")).unwrap()["status"],
                "running"
            );
        }
    }
    #[test]
    fn stale_or_corrupt_controller_recovers_at_floor_and_does_not_break_status() {
        let t = Test::new();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-config.json"),
                &json!({"max_running":25,"adaptive":{}}),
            )
            .unwrap();
        t.ctx
            .write_json(
                &t.ctx.root.join("queue-limit-state.json"),
                &json!({"mode":"auto","max_running":60,"checked_at":t.rt.epoch()-cc_jobs::adaptive::AdaptiveConfig::default().stale_after_seconds()-1.}),
            )
            .unwrap();
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 4);
        fs::write(t.ctx.root.join("queue-limit-state.json"), b"broken").unwrap();
        fs::write(t.ctx.root.join("queue-limit-history.json"), b"broken").unwrap();
        let (r, _, _) = t.cli(&["status", "--json", "--summary"]);
        assert_eq!(r.unwrap(), 0);
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 4);
        t.ctx.adjust_limit().unwrap();
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 4);
    }
    #[test]
    fn todo_completion_wait_reserves_no_capacity_and_releases_on_done() {
        let t = Test::new();
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        let mut db =
            json!({"items":[{"id":1,"status":"wait","wait_on":[2]},{"id":2,"status":"doing"}]});
        fs::write(&store, db.to_string()).unwrap();
        let mut dependent = waiting(&t, "dependent", "dependent");
        dependent["todo_id"] = json!(1);
        t.ctx.save(&dependent).unwrap();
        let mut normal = waiting(&t, "ordinary", "ordinary");
        normal["todo_id"] = json!(2);
        t.ctx.save(&normal).unwrap();
        assert!(t.ctx.admit("dependent").unwrap().is_none());
        let reason = t.ctx.load("dependent").unwrap()["queue_wait_reason"].clone();
        assert_eq!(reason["code"], "todo_dependency");
        assert_eq!(reason["message"], "ToDo 2 待ち");
        assert!(t.ctx.admit("ordinary").unwrap().is_some());
        normal = t.ctx.load("ordinary").unwrap();
        normal["status"] = json!("succeeded");
        t.ctx.save(&normal).unwrap();
        db["items"][1]["status"] = json!("done");
        fs::write(&store, db.to_string()).unwrap();
        assert!(t.ctx.admit("dependent").unwrap().is_some());
    }
    #[test]
    fn missing_prerequisite_and_linked_legacy_jobs_remain_waiting() {
        let t = Test::new();
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        fs::write(
            t.ctx.root.join("todo.json"),
            json!({"items":[{"id":1,"status":"doing","wait_on":[99],"jobs":["legacy"]}]})
                .to_string(),
        )
        .unwrap();
        waiting(&t, "legacy", "shep-116");
        assert!(t.ctx.admit("legacy").unwrap().is_none());
        assert_eq!(
            t.ctx.load("legacy").unwrap()["queue_wait_reason"]["wait_on"],
            json!([99])
        );
    }
    #[test]
    fn unreadable_todo_store_defers_legacy_admission_until_next_check() {
        let t = Test::new();
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        fs::write(&store, "{broken").unwrap();
        waiting(&t, "legacy", "shep-116");
        assert!(t.ctx.admit("legacy").unwrap().is_none());
        assert_eq!(
            t.ctx.load("legacy").unwrap()["queue_wait_reason"]["code"],
            "todo_store_unavailable"
        );
        fs::write(store, json!({"items":[]}).to_string()).unwrap();
        assert!(t.ctx.admit("legacy").unwrap().is_some());
    }
    #[test]
    fn waiting_todo_registration_does_not_spawn_a_supervisor() {
        let t = Test::new();
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        fs::write(
            t.ctx.root.join("todo.json"),
            json!({"items":[{"id":1,"status":"wait","wait_on":[2]},{"id":2,"status":"todo"}]})
                .to_string(),
        )
        .unwrap();
        let before = t.rt.supervisors.load(Ordering::SeqCst);
        assert!(t.run(&["--todo-id", "1"], b"complete task").is_err());
        assert_eq!(t.rt.supervisors.load(Ordering::SeqCst), before);
        assert!(
            !t.ctx.jobs.exists()
                || fs::read_dir(&t.ctx.jobs)
                    .unwrap()
                    .flatten()
                    .all(|e| !e.path().join("job.json").exists())
        );
    }
    #[test]
    fn shared_pr_worker_tracks_every_related_todo_at_registration_and_admission() {
        let t = Test::new();
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        let mut db = json!({"items":[{"id":1,"status":"todo"},{"id":2,"status":"wait","wait_on":[3]},{"id":3,"status":"doing"}]});
        fs::write(&store, db.to_string()).unwrap();
        assert!(
            t.run(&["--related-todo-ids", "1,2"], b"finish shared PR")
                .is_err()
        );
        assert!(
            t.run(&["--related-todo-ids", "1,bad"], b"finish shared PR")
                .is_err()
        );
        let mut j = waiting(&t, "shared", "shep-116");
        j["related_todo_ids"] = json!([1, 2]);
        t.ctx.save(&j).unwrap();
        assert!(t.ctx.admit("shared").unwrap().is_none());
        assert_eq!(
            t.ctx.load("shared").unwrap()["queue_wait_reason"]["message"],
            "ToDo 3 待ち"
        );
        db["items"][2]["status"] = json!("done");
        fs::write(&store, db.to_string()).unwrap();
        assert!(t.ctx.admit("shared").unwrap().is_some());
        assert!(t.run(&["--todo-id", "2"], b"duplicate").is_err());
    }
    #[test]
    fn reply_metadata_rechecks_dependencies_added_after_registration() {
        let t = Test::new();
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        let store = t.ctx.root.join("todo.json");
        let mut db = json!({"items":[{"id":2073,"status":"todo"},
            {"id":2074,"status":"todo"},{"id":32,"status":"todo"}]});
        fs::write(&store, db.to_string()).unwrap();
        assert_eq!(
            t.run(
                &["--todo-id", "2073", "--related-todo-ids", "2073,2074"],
                b"reply continuation"
            )
            .unwrap(),
            0
        );
        let path = fs::read_dir(&t.ctx.jobs)
            .unwrap()
            .flatten()
            .map(|e| e.path())
            .find(|p| p.join("job.json").is_file())
            .unwrap();
        let id = path.file_name().unwrap().to_str().unwrap();
        assert_eq!(
            t.ctx.load(id).unwrap()["related_todo_ids"],
            json!([2073, 2074])
        );
        db["items"][1]["status"] = json!("wait");
        db["items"][1]["wait_on"] = json!([32]);
        fs::write(&store, db.to_string()).unwrap();
        assert!(t.ctx.admit(id).unwrap().is_none());
        assert_eq!(
            t.ctx.load(id).unwrap()["queue_wait_reason"]["wait_on"],
            json!([32])
        );
        db["items"][2]["status"] = json!("done");
        fs::write(&store, db.to_string()).unwrap();
        assert!(t.ctx.admit(id).unwrap().is_some());
    }
    const SESSION_ID: &str = "01a11052-a537-74f3-9611-deb742778fc5";
    fn long_parent(t: &Test, id: &str) -> Value {
        let mut j = waiting(t, id, "orch-long");
        j["status"] = json!("running");
        j["started_at"] = json!("2026-10-01T10:00:00+09:00");
        j["thread_id"] = json!(SESSION_ID);
        j["todo_id"] = json!(1956);
        t.ctx.save(&j).unwrap();
        j
    }
    fn live_tool_parent(t: &Test) -> Value {
        let mut parent = long_parent(t, "long");
        parent["queue_fairness_version"] = json!(1);
        parent["pid"] = json!(445);
        parent["pid_created"] = json!(45);
        parent["codex_pid"] = json!(444);
        parent["codex_pid_created"] = json!(44);
        t.rt.procs.lock().unwrap().extend([(444, 44), (445, 45)]);
        t.rt.pause_allowed.store(true, Ordering::Relaxed);
        t.ctx.save(&parent).unwrap();
        waiting(t, "other", "orch-other");
        parent
    }
    fn tool_events(t: &Test, events: &[(&str, &str)]) {
        let text: String = events
            .iter()
            .map(|(ty, id)| {
                format!(
                    "{}\n",
                    json!({"_ts":t.rt.now(),"type":ty,"item":{"id":id,"type":"command_execution"}})
                )
            })
            .collect();
        fs::write(t.ctx.job_dir("long").join("events.jsonl"), text).unwrap();
    }
    #[test]
    fn context_limit_rotates_a_short_job_only_at_the_existing_idle_boundary() {
        for (pct, limit, phase, expected) in [
            (50, 50, "", false),
            (51, 50, "", true),
            (60, 70, "", false),
            (90, 50, "summarize", false),
        ] {
            let t = Test::new();
            config(&t, json!({"max_running":1,"context_limit_percent":limit}));
            let mut parent = live_tool_parent(&t);
            parent["started_at"] = json!("2026-10-01T11:59:00+09:00");
            parent["context_pct"] = json!(pct);
            parent["context_phase"] = json!(phase);
            t.ctx.save(&parent).unwrap();
            tool_events(&t, &[("item.started", "cargo")]);
            assert!(t.ctx.admit("other").unwrap().is_none());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            tool_events(
                &t,
                &[("item.started", "cargo"), ("item.completed", "cargo")],
            );
            assert_eq!(t.ctx.admit("other").unwrap().is_some(), expected);
            if expected {
                let saved = t.ctx.load("long").unwrap();
                assert_eq!(saved["context_phase"], "summarize");
                assert_eq!(saved["resume_session"], SESSION_ID);
                assert_eq!(saved["todo_id"], 1956);
                assert!(
                    fs::read_to_string(t.ctx.job_dir("long").join("context-summary-prompt.md"))
                        .unwrap()
                        .contains("関係ファイルとPR")
                );
            }
        }
    }
    #[test]
    fn resumed_old_session_rotates_after_completed_command_and_keeps_todo() {
        for resumed in [true, false] {
            let t = Test::new();
            config(&t, json!({"max_running":1,"context_limit_percent":50}));
            let mut parent = live_tool_parent(&t);
            parent["started_at"] = json!("2026-10-01T11:59:00+09:00");
            if resumed {
                parent["resume_session"] = json!(SESSION_ID);
            }
            // Stored percentage is stale until the resumed rollout is read.
            parent["context_pct"] = json!(20);
            t.ctx.save(&parent).unwrap();
            let old_dir = t.ctx.codex_home.join("sessions/2026/09/28");
            fs::create_dir_all(&old_dir).unwrap();
            fs::write(old_dir.join(format!("rollout-old-{SESSION_ID}.jsonl")),
                format!("{}\n", json!({"type":"event_msg","payload":{"type":"token_count","info":{"model_context_window":1000,"last_token_usage":{"total_tokens":640}}}}))).unwrap();
            assert!(
                t.ctx
                    .poll_session(&mut parent, &mut Session::default())
                    .unwrap()
            );
            assert_eq!(parent["context_pct_exact"], 64.0);
            t.ctx.save(&parent).unwrap();
            tool_events(&t, &[("item.started", "long-command")]);
            assert!(t.ctx.admit("other").unwrap().is_none());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            tool_events(
                &t,
                &[
                    ("item.started", "long-command"),
                    ("item.completed", "long-command"),
                ],
            );
            assert!(t.ctx.admit("other").unwrap().is_some());
            let saved = t.ctx.load("long").unwrap();
            assert_eq!(saved["context_phase"], "summarize");
            assert_eq!(saved["resume_session"], SESSION_ID);
            assert_eq!(saved["todo_id"], 1956);
            assert_eq!(saved["yield_boundary_item"], "long-command");
        }
    }
    #[test]
    fn resumed_supervisor_refreshes_usage_before_its_first_completed_command() {
        struct CompletingCommand {
            first: bool,
            path: PathBuf,
            rollout: PathBuf,
            rt: Arc<Fake>,
        }
        impl Read for CompletingCommand {
            fn read(&mut self, out: &mut [u8]) -> io::Result<usize> {
                let text = if self.first {
                    self.first = false;
                    format!(
                        "{}\n{}\n",
                        json!({"type":"thread.started","thread_id":SESSION_ID}),
                        json!({"type":"item.started","item":{"id":"command","type":"command_execution"}})
                    )
                } else {
                    // Wait until the supervisor saved the started command. Its
                    // initial poll has consumed the low usage, and mono remains
                    // below the three-second poll interval.
                    let until = Instant::now() + Duration::from_secs(10);
                    loop {
                        let job: Value =
                            serde_json::from_str(&fs::read_to_string(&self.path)?).unwrap();
                        if s(&job, "activity").starts_with("running:") {
                            break;
                        }
                        assert!(Instant::now() < until);
                        std::thread::sleep(Duration::from_millis(1));
                    }
                    use std::io::Write;
                    writeln!(
                        fs::OpenOptions::new().append(true).open(&self.rollout)?,
                        "{}",
                        json!({"type":"event_msg","payload":{"type":"token_count","info":{"model_context_window":1000,"last_token_usage":{"total_tokens":640}}}})
                    )?;
                    self.rt.child_alive.store(true, Ordering::Relaxed);
                    self.first = true;
                    return Cursor::new(format!("{}\n", json!({"type":"item.completed","item":{"id":"command","type":"command_execution","exit_code":0,"aggregated_output":"done once"}})).into_bytes()).read(out);
                };
                Cursor::new(text.into_bytes()).read(out)
            }
        }
        // Use a bounded reader so EOF after the completed item is deterministic.
        struct OnceCommand {
            inner: CompletingCommand,
            reads: usize,
        }
        impl Read for OnceCommand {
            fn read(&mut self, out: &mut [u8]) -> io::Result<usize> {
                if self.reads == 2 {
                    self.inner.rt.child_alive.store(false, Ordering::Relaxed);
                    return Ok(0);
                }
                self.reads += 1;
                self.inner.read(out)
            }
        }
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut j = waiting(&t, "resumed", "req-resumed");
        j["resume_session"] = json!(SESSION_ID);
        j["thread_id"] = json!(SESSION_ID);
        j["todo_id"] = json!(2006);
        t.ctx.save(&j).unwrap();
        let dir = t.ctx.job_dir("resumed");
        fs::write(dir.join("prompt.md"), "元の作業").unwrap();
        let date = t.ctx.codex_home.join("sessions/2026/09/28");
        fs::create_dir_all(&date).unwrap();
        let rollout = date.join(format!("rollout-old-{SESSION_ID}.jsonl"));
        fs::write(&rollout, format!("{}\n", json!({"type":"event_msg","payload":{"type":"token_count","info":{"model_context_window":1000,"last_token_usage":{"total_tokens":200}}}}))).unwrap();
        let seq = AtomicUsize::new(0);
        let rt = t.rt.clone();
        rt.pause_allowed.store(true, Ordering::Relaxed);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            rt.procs.lock().unwrap().insert(321, 32);
            let n = seq.fetch_add(1, Ordering::Relaxed);
            let out: Box<dyn Read + Send> = if n == 0 {
                rt.child_alive.store(true, Ordering::Relaxed);
                Box::new(OnceCommand {
                    inner: CompletingCommand {
                        first: true,
                        path: dir.join("job.json"),
                        rollout: rollout.clone(),
                        rt: rt.clone(),
                    },
                    reads: 0,
                })
            } else {
                rt.child_alive.store(false, Ordering::Relaxed);
                let summary = "## 目的\n元の作業\n## 終わったこと\nコマンド完了\n## 残り\n確認\n## 次の一手\n結果を読む\n## 関係ファイルとPR\nなし\n## 最新の指示と注意点\nRustのみ\n";
                let text = if n == 1 {
                    format!(
                        "{}\n{}\n{}\n",
                        json!({"type":"thread.started","thread_id":SESSION_ID}),
                        json!({"type":"item.completed","item":{"type":"agent_message","text":summary}}),
                        json!({"type":"turn.completed","usage":{}})
                    )
                } else {
                    OK.into()
                };
                Box::new(Cursor::new(text.into_bytes()))
            };
            Box::new(FakeChild {
                alive: rt.child_alive.clone(),
                out: Some(out),
            })
        }));
        t.ctx.supervise("resumed").unwrap();
        let saved = t.ctx.load("resumed").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["context_rotation_count"], 1);
        assert_eq!(saved["context_source_pct"], 64);
        assert_eq!(saved["todo_id"], 2006);
        assert_eq!(saved["yield_boundary_item"], "command");
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 3);
        assert!(args[0].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert!(!args[2].iter().any(|a| a == "resume"));
    }
    #[test]
    fn failed_yield_backoff_applies_to_its_attempt_but_not_mid_item() {
        let t = Test::new();
        config(&t, json!({"max_running":1,"context_limit_percent":50}));
        let mut parent = live_tool_parent(&t);
        parent["started_at"] = json!("2026-10-01T11:59:00+09:00");
        parent["context_pct"] = json!(51);
        t.ctx.save(&parent).unwrap();
        let retry = t.ctx.job_dir("long").join("yield-retry.json");
        // Mid-item checks are cheap and must not delay the next boundary.
        tool_events(&t, &[("item.started", "cargo")]);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert!(!retry.exists());
        tool_events(
            &t,
            &[("item.started", "cargo"), ("item.completed", "cargo")],
        );
        let epoch = parse_iso(&t.rt.now()).unwrap();
        let attempt = format!(
            "{}:{}",
            s(&parent, "started_at"),
            parent["codex_pid_created"]
        );
        fs::write(
            &retry,
            json!({"not_before": epoch + 20., "attempt": attempt}).to_string(),
        )
        .unwrap();
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.ctx.load("long").unwrap()["status"], "running");
        // A backoff left by an earlier Codex attempt of the same job is ignored.
        fs::write(
            &retry,
            json!({"not_before": epoch + 20., "attempt": "earlier:1"}).to_string(),
        )
        .unwrap();
        assert!(t.ctx.admit("other").unwrap().is_some());
        assert_eq!(t.ctx.load("long").unwrap()["context_phase"], "summarize");
    }
    #[test]
    fn context_handoff_resumes_for_summary_then_starts_fresh_with_the_same_todo_and_scope() {
        let t = Test::new();
        let mut j = waiting(&t, "handoff", "req-handoff");
        j["context_phase"] = json!("summarize");
        j["context_source_session"] = json!(SESSION_ID);
        j["resume_session"] = json!(SESSION_ID);
        j["thread_id"] = json!(SESSION_ID);
        j["context_pct"] = json!(90);
        j["todo_id"] = json!(1972);
        j["sandbox"] = json!("danger-full-access");
        t.ctx.save(&j).unwrap();
        let dir = t.ctx.job_dir("handoff");
        fs::write(dir.join("prompt.md"), "元の依頼全文と受入条件").unwrap();
        fs::write(
            dir.join("context-summary-prompt.md"),
            "引き継ぎだけを書いてください",
        )
        .unwrap();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let text = if seq.fetch_add(1, Ordering::Relaxed) == 0 {
                let summary = "## 目的\n消費を減らす\n## 終わったこと\nPRのpush済み\n## 残り\nテスト\n## 次の一手\ncargo test\n## 関係ファイルとPR\nD:/work/src.rs\n## 最新の指示と注意点\nRustのみ、送信を繰り返さない\n";
                format!(
                    "{}\n{}\n{}\n",
                    json!({"type":"thread.started","thread_id":SESSION_ID}),
                    json!({"type":"item.completed","item":{"type":"agent_message","text":summary}}),
                    json!({"type":"turn.completed","usage":{}})
                )
            } else {
                OK.into()
            };
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        t.ctx.supervise("handoff").unwrap();
        let saved = t.ctx.load("handoff").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["context_rotation_count"], 1);
        assert_eq!(saved["todo_id"], 1972);
        assert_eq!(saved["sandbox"], "danger-full-access");
        assert!(saved["context_pct"].is_null());
        assert!(
            fs::read_to_string(dir.join("context-continue.md"))
                .unwrap()
                .contains("PRのpush済み")
        );
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 2);
        assert!(args[0].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert!(args[0].windows(2).any(|w| w == ["-s", "read-only"]));
        assert!(!args[1].iter().any(|a| a == "resume"));
        assert!(
            args[1]
                .windows(2)
                .any(|w| w == ["-s", "danger-full-access"])
        );
    }
    #[test]
    fn context_rotation_waits_for_descendants_and_does_not_resurrect_terminal_work() {
        for case in ["child", "cancel", "turn.completed", "turn.failed"] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = live_tool_parent(&t);
            parent["context_pct"] = json!(90);
            if case == "cancel" {
                parent["cancel_requested"] = json!(true);
            }
            t.ctx.save(&parent).unwrap();
            tool_events(
                &t,
                &[("item.started", "cargo"), ("item.completed", "cargo")],
            );
            if case == "child" {
                let mut child = waiting(&t, "child", "rev-child");
                child["parent_job_id"] = json!("long");
                t.ctx.save(&child).unwrap();
            } else if case.starts_with("turn.") {
                use std::io::Write;
                writeln!(
                    fs::OpenOptions::new()
                        .append(true)
                        .open(t.ctx.job_dir("long").join("events.jsonl"))
                        .unwrap(),
                    "{}",
                    json!({"type":case,"_ts":t.rt.now()})
                )
                .unwrap();
            }
            let admission = t.ctx.admit("other").unwrap();
            if ["child", "cancel"].contains(&case) {
                assert!(admission.is_none(), "{case}");
            }
            assert!(t.rt.kills.lock().unwrap().is_empty(), "{case}");
            assert_ne!(
                t.ctx.load("long").unwrap()["context_phase"],
                "summarize",
                "{case}"
            );
        }
    }
    #[test]
    fn context_uses_unrounded_usage_and_disables_discovered_tools_for_the_summary() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut parent = live_tool_parent(&t);
        parent["started_at"] = json!("2026-10-01T11:59:00+09:00");
        parent["mcp_profile"] = json!("full");
        apply_token_count(
            &mut parent,
            &json!({"info":{"model_context_window":1000,"last_token_usage":{"total_tokens":501}}}),
        );
        assert_eq!(parent["context_pct"], 50);
        t.ctx.save(&parent).unwrap();
        tool_events(
            &t,
            &[("item.started", "cargo"), ("item.completed", "cargo")],
        );
        assert!(
            t.ctx.admit("other").unwrap().is_some(),
            "{:?}",
            t.rt.diagnostics.lock().unwrap()
        );
        let summary = t.ctx.load("long").unwrap();
        let args = codex_args(&summary);
        for required in [
            "features.plugins=false",
            "features.apps=false",
            "features.shell_tool=false",
            "web_search=disabled",
            "mcp_servers.stripe.enabled=false",
            "mcp_servers.unseen-server.enabled=false",
        ] {
            assert!(args.iter().any(|a| a == required), "{required}");
        }
        assert!(!args.iter().any(|a| a.contains("enabled=true")));
    }
    #[test]
    fn failed_inventory_keeps_agent_running_and_backs_off_before_retrying() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        let mut parent = live_tool_parent(&t);
        parent["context_pct"] = json!(51);
        t.ctx.save(&parent).unwrap();
        tool_events(&t, &[("item.started", "tool"), ("item.completed", "tool")]);
        t.rt.summary_fail.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.rt.summary_calls.load(Ordering::Relaxed), 1);
        assert!(t.rt.kills.lock().unwrap().is_empty());
        assert!(!t.ctx.job_dir("long").join("yield-transition.json").exists());
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.rt.summary_calls.load(Ordering::Relaxed), 1);
        t.rt.summary_fail.store(false, Ordering::Relaxed);
        t.rt.tick(31.);
        assert!(t.ctx.admit("other").unwrap().is_some());
        assert_eq!(t.rt.summary_calls.load(Ordering::Relaxed), 2);
        assert_eq!(t.ctx.load("long").unwrap()["context_phase"], "summarize");
    }
    #[test]
    fn invalid_or_unwritable_context_summary_retains_the_session_and_never_reports_success() {
        for broken in ["content", "context-continue.md", "context-handoff.md"] {
            let t = Test::new();
            let mut j = waiting(&t, "handoff", "req-handoff");
            j["context_phase"] = json!("summarize");
            j["context_source_session"] = json!(SESSION_ID);
            j["resume_session"] = json!(SESSION_ID);
            j["thread_id"] = json!(SESSION_ID);
            t.ctx.save(&j).unwrap();
            let dir = t.ctx.job_dir("handoff");
            fs::write(dir.join("context-summary-prompt.md"), "summary").unwrap();
            if broken != "content" {
                fs::create_dir(dir.join(broken)).unwrap();
            }
            let summary = if broken == "content" {
                "不足した引き継ぎ"
            } else {
                "## 目的\n1\n## 終わったこと\n2\n## 残り\n3\n## 次の一手\n4\n## 関係ファイルとPR\n5\n## 最新の指示と注意点\n6"
            };
            *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
                Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(format!("{}\n{}\n{}\n", json!({"type":"thread.started","thread_id":SESSION_ID}), json!({"type":"item.completed","item":{"type":"agent_message","text":summary}}), json!({"type":"turn.completed"})).into_bytes())))
            })
            }));
            t.ctx.supervise("handoff").unwrap();
            let saved = t.ctx.load("handoff").unwrap();
            assert_eq!(saved["status"], "failed", "{broken}");
            assert_eq!(saved["resume_session"], SESSION_ID);
            assert_eq!(saved["thread_id"], SESSION_ID);
            assert_eq!(saved["context_phase"], "summarize");
            assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        }
    }
    fn oversized_summary_job(t: &Test, outputs: Vec<String>) {
        let mut j = waiting(t, "handoff", "req-handoff");
        j["context_phase"] = json!("summarize");
        j["context_source_session"] = json!(SESSION_ID);
        j["resume_session"] = json!(SESSION_ID);
        j["thread_id"] = json!(SESSION_ID);
        j["sandbox"] = json!("danger-full-access");
        t.ctx.save(&j).unwrap();
        let dir = t.ctx.job_dir("handoff");
        fs::write(dir.join("prompt.md"), "元の依頼全文").unwrap();
        fs::write(dir.join("context-summary-prompt.md"), "summary").unwrap();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let i = seq.fetch_add(1, Ordering::Relaxed);
            let text = outputs.get(i).cloned().unwrap_or_else(|| OK.into());
            let events = if i < outputs.len() {
                format!(
                    "{}\n{}\n{}\n",
                    json!({"type":"thread.started","thread_id":SESSION_ID}),
                    json!({"type":"item.completed","item":{"type":"agent_message","text":text}}),
                    json!({"type":"turn.completed","usage":{}})
                )
            } else {
                text
            };
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(events.into_bytes()))),
            })
        }));
    }
    fn summary_of(bytes: usize) -> String {
        let head = "## 目的\nx\n## 終わったこと\nx\n## 残り\nx\n## 次の一手\nx\n## 関係ファイルとPR\nx\n## 最新の指示と注意点\n";
        // Multibyte filler so the limit is checked in UTF-8 bytes, not chars.
        let n = bytes.saturating_sub(head.len()) / 3;
        format!("{head}{}", "あ".repeat(n))
    }
    #[test]
    fn oversized_summary_is_shortened_in_the_same_read_only_session_then_rotates() {
        let t = Test::new();
        oversized_summary_job(
            &t,
            vec![summary_of(27_000), summary_of(25_000), summary_of(3_000)],
        );
        t.ctx.supervise("handoff").unwrap();
        let saved = t.ctx.load("handoff").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["context_rotation_count"], 1);
        assert!(saved["context_summary_shorten_attempts"].is_null());
        let dir = t.ctx.job_dir("handoff");
        assert!(
            fs::read_to_string(dir.join("context-handoff.md"))
                .unwrap()
                .len()
                < 24 * 1024
        );
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 4);
        for a in &args[..3] {
            assert!(a.windows(2).any(|w| w == ["resume", SESSION_ID]));
            assert!(a.windows(2).any(|w| w == ["-s", "read-only"]));
            assert!(a.iter().any(|x| x == "features.shell_tool=false"));
        }
        assert!(!args[3].iter().any(|a| a == "resume"));
        assert!(
            args[3]
                .windows(2)
                .any(|w| w == ["-s", "danger-full-access"])
        );
    }
    #[test]
    fn summary_still_oversized_after_two_shortenings_fails_and_keeps_the_session() {
        let t = Test::new();
        oversized_summary_job(&t, vec![summary_of(30_000); 4]);
        t.ctx.supervise("handoff").unwrap();
        let saved = t.ctx.load("handoff").unwrap();
        assert_eq!(saved["status"], "failed");
        assert!(s(&saved, "last_error").contains("exceeds 24KB"));
        assert_eq!(saved["resume_session"], SESSION_ID);
        assert_eq!(saved["thread_id"], SESSION_ID);
        assert_eq!(saved["context_phase"], "summarize");
        assert_eq!(saved["context_summary_shorten_attempts"], 2);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 3);
        assert!(
            !t.ctx
                .job_dir("handoff")
                .join("context-continue.md")
                .exists()
        );
    }
    #[test]
    fn incomplete_summary_is_not_retried_as_a_shortening() {
        let t = Test::new();
        oversized_summary_job(&t, vec![format!("## 目的\nx\n{}", "あ".repeat(9000))]);
        t.ctx.supervise("handoff").unwrap();
        let saved = t.ctx.load("handoff").unwrap();
        assert_eq!(saved["status"], "failed");
        assert!(saved["context_summary_shorten_attempts"].is_null());
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    }
    fn collab(t: &Test, id: &str, tool: &str, child: &str, status: &str) {
        use std::io::Write;
        let path = t.ctx.job_dir("long").join("events.jsonl");
        let mut file = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(path)
            .unwrap();
        writeln!(file,"{}",json!({"_ts":t.rt.now(),"type":"item.started","item":{"id":id,"type":"collab_tool_call","tool":tool}})).unwrap();
        writeln!(file,"{}",json!({"_ts":t.rt.now(),"type":"item.completed","item":{"id":id,"type":"collab_tool_call","tool":tool,"receiver_thread_ids":[child],"status":"completed","agents_states":{child:{"status":status}}}})).unwrap();
    }
    #[test]
    fn native_subagents_block_yield_until_wait_or_close_reports_terminal_state() {
        for close in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            live_tool_parent(&t);
            let child = "01a111c8-1034-7ac1-9999-aaaaaaaaaaaa";
            collab(&t, "spawn", "spawn_agent", child, "running");
            collab(&t, "timeout", "wait", child, "running");
            assert!(t.ctx.admit("other").unwrap().is_none());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            collab(
                &t,
                "done",
                if close { "close_agent" } else { "wait" },
                child,
                "completed",
            );
            assert!(t.ctx.admit("other").unwrap().is_some());
            assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
        }
    }
    #[test]
    fn subagent_rollout_requires_its_own_completed_turn_and_never_reuses_an_old_output_after_send_input()
     {
        for send_input in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            live_tool_parent(&t);
            let child = "01a111c8-1034-7ac1-9999-aaaaaaaaaaaa";
            collab(&t, "spawn", "spawn_agent", child, "running");
            let dir = t.ctx.codex_home.join("sessions/2026/10/01");
            fs::create_dir_all(&dir).unwrap();
            let path = dir.join(format!("rollout-test-{child}.jsonl"));
            let events = [
                json!({"type":"session_meta","payload":{"id":child,"timestamp":t.rt.now()}}),
                json!({"timestamp":"2026-10-01T11:59:00+09:00","type":"event_msg","payload":{"type":"task_complete","turn_id":"old-parent"}}),
                json!({"timestamp":t.rt.now(),"type":"event_msg","payload":{"type":"task_started","turn_id":"child-turn"}}),
            ];
            let prefix = events.iter().map(|v| format!("{v}\n")).collect::<String>();
            fs::write(&path, &prefix).unwrap();
            assert!(t.ctx.admit("other").unwrap().is_none());
            fs::write(&path, format!("{prefix}{{")).unwrap();
            assert!(t.ctx.admit("other").unwrap().is_none());
            fs::write(&path,format!("{prefix}{}\n",json!({"timestamp":t.rt.now(),"type":"event_msg","payload":{"type":"task_complete","turn_id":"child-turn","last_agent_message":"結果: 送信は完了済み"}}))).unwrap();
            if send_input {
                collab(&t, "more", "send_input", child, "running");
                assert!(t.ctx.admit("other").unwrap().is_none());
                collab(&t, "joined", "wait", child, "completed");
            }
            assert!(t.ctx.admit("other").unwrap().is_some());
            let resume = fs::read_to_string(t.ctx.job_dir("long").join("yield-resume.md")).unwrap();
            if send_input {
                assert!(!resume.contains("結果: 送信は完了済み"));
            } else {
                assert!(resume.contains(child));
                assert!(resume.contains("結果: 送信は完了済み"));
            }
        }
    }
    #[test]
    fn legacy_running_turn_stops_only_at_tool_completion_and_gives_waiter_the_slot() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        live_tool_parent(&t);
        let gate_path = t.ctx.jobs.join(".queue.lock");
        let probes = Arc::new(AtomicUsize::new(0));
        let seen = probes.clone();
        *t.rt.pause_hook.lock().unwrap() = Some(Box::new(move || {
            // This callback used to run with the global gate held. A separate
            // acquisition proves registration can proceed during slow freezing.
            drop(FileLock::try_acquire(&gate_path).expect("pause monopolized queue gate"));
            seen.fetch_add(1, Ordering::Relaxed);
        }));
        tool_events(&t, &[("item.started", "cargo")]);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert!(t.rt.kills.lock().unwrap().is_empty());
        tool_events(
            &t,
            &[("item.started", "cargo"), ("item.completed", "cargo")],
        );
        assert!(t.ctx.admit("other").unwrap().is_some());
        let j = t.ctx.load("long").unwrap();
        assert_eq!(j["status"], "queued");
        assert_eq!(j["resume_session"], SESSION_ID);
        assert_eq!(j["todo_id"], 1956);
        assert_eq!(j["yield_boundary_item"], "cargo");
        assert_eq!(j["yield_count"], 1);
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![445, 444]);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert!(probes.load(Ordering::Relaxed) > 0);
        assert!(j["turn_completed"].is_null());
    }
    #[test]
    fn new_item_racing_the_pause_prevents_stop_and_background_workers_delay_yield() {
        for background in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            live_tool_parent(&t);
            tool_events(
                &t,
                &[("item.started", "first"), ("item.completed", "first")],
            );
            if background {
                t.rt.pause_allowed.store(false, Ordering::Relaxed);
            } else {
                let path = t.ctx.job_dir("long").join("events.jsonl");
                let now = t.rt.now();
                *t.rt.pause_hook.lock().unwrap() = Some(Box::new(move || {
                    use std::io::Write;
                    writeln!(fs::OpenOptions::new().append(true).open(&path).unwrap(),"{}",
                        json!({"_ts":now,"type":"item.started","item":{"id":"next","type":"mcp_tool_call"}})).unwrap();
                }));
            }
            assert!(t.ctx.admit("other").unwrap().is_none());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            assert_eq!(t.ctx.load("long").unwrap()["status"], "running");
        }
    }
    #[test]
    fn failed_spawn_after_stop_retries_the_same_durable_session_without_double_yield() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        live_tool_parent(&t);
        tool_events(&t, &[("item.started", "tool"), ("item.completed", "tool")]);
        t.rt.supervisor_fail.store(true, Ordering::Relaxed);
        assert!(
            t.ctx.admit("other").unwrap().is_some(),
            "job: {}; diagnostics: {:?}",
            t.ctx.load("long").unwrap(),
            t.rt.diagnostics.lock().unwrap()
        );
        assert_eq!(t.ctx.load("long").unwrap()["status"], "queued");
        assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
        t.rt.supervisor_fail.store(false, Ordering::Relaxed);
        assert!(t.ctx.admit("long").unwrap().is_none());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        assert_eq!(t.ctx.load("long").unwrap()["resume_session"], SESSION_ID);
        assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
    }
    #[test]
    fn legacy_stop_failure_retains_the_family_until_all_recorded_controllers_are_dead() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let original = live_tool_parent(&t);
        tool_events(&t, &[("item.started", "tool"), ("item.completed", "tool")]);
        t.rt.paused_stop_failure_pid.store(444, Ordering::Relaxed);
        t.rt.paused_stop_failure.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_none());
        t.rt.pause_allowed.store(false, Ordering::Relaxed);
        assert_eq!(t.ctx.refresh(original, false).unwrap()["status"], "running");
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![445]);
        t.rt.pause_allowed.store(false, Ordering::Relaxed);
        t.rt.pause_recoveries.lock().unwrap().clear();
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert!(
            !t.rt
                .pause_recoveries
                .lock()
                .unwrap()
                .iter()
                .any(|id| id == "long")
        );
        t.rt.pause_allowed.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_some());
        let queued = t.ctx.load("long").unwrap();
        assert_eq!(queued["status"], "queued");
        assert_eq!(queued["yield_count"], 1);
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![445, 444]);
    }
    #[test]
    fn new_supervisor_stops_a_live_turn_and_resumes_after_another_job() {
        live_supervisor_handoff(false, false);
    }
    #[test]
    fn refresh_commits_pending_handoff_and_returns_the_new_queued_state() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let original = live_tool_parent(&t);
        tool_events(&t, &[("item.started", "tool"), ("item.completed", "tool")]);
        t.rt.paused_stop_failure_pid.store(444, Ordering::Relaxed);
        t.rt.paused_stop_failure.store(true, Ordering::Relaxed);
        assert!(t.ctx.admit("other").unwrap().is_none());
        let refreshed = t.ctx.refresh(original, false).unwrap();
        assert_eq!(refreshed["status"], "queued");
        assert_eq!(t.ctx.load("long").unwrap()["status"], "queued");
        assert!(refreshed["queue_sequence"].as_u64().unwrap() > 0);
    }
    #[test]
    fn supervisor_keeps_monitoring_after_stop_failure_and_recovers_after_save_failure() {
        live_supervisor_handoff(true, false);
        live_supervisor_handoff(false, true);
    }
    fn live_supervisor_handoff(stop_failure: bool, save_failure: bool) {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.rt.pause_allowed.store(true, Ordering::Relaxed);
        t.rt.paused_stop_failure
            .store(stop_failure, Ordering::Relaxed);
        t.rt.fail_queued_save.store(save_failure, Ordering::Relaxed);
        waiting(&t, "long", "orch-long");
        fs::write(t.ctx.job_dir("long").join("prompt.md"), "original").unwrap();
        let ctx = t.ctx.clone();
        let rt = t.rt.clone();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let first = seq.fetch_add(1, Ordering::Relaxed) == 0;
            let text = if first {
                rt.tick(7201.);
                rt.procs.lock().unwrap().insert(321, 99);
                rt.child_alive.store(true, Ordering::Relaxed);
                fs::create_dir_all(ctx.job_dir("other")).unwrap();
                ctx.save(&job(json!({"id":"other","name":"orch-other","pid":std::process::id(),"pid_created":123,"queued_at":"2026-10-01T11:59:59+09:00"}))).unwrap();
                {
                    let _gate = ctx.queue_lock().unwrap();
                    ctx.track_queued_job("other").unwrap();
                }
                format!(
                    "{}\n{}\n{}\n{}\n",
                    json!({"type":"thread.started","thread_id":SESSION_ID}),
                    json!({"type":"turn.started"}),
                    json!({"type":"item.started","item":{"type":"command_execution","id":"build"}}),
                    json!({"type":"item.completed","item":{"type":"command_execution","id":"build"}})
                )
            } else {
                format!(
                    "{}\n{}\n",
                    json!({"type":"item.completed","item":{"type":"agent_message","text":"状態: 完了"}}),
                    json!({"type":"turn.completed"})
                )
            };
            Box::new(FakeChild {
                alive: if first {
                    rt.child_alive.clone()
                } else {
                    Arc::new(AtomicBool::new(false))
                },
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        let ctx = t.ctx.clone();
        let observed = Arc::new(AtomicBool::new(false));
        let seen = observed.clone();
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds == 3. && !seen.swap(true, Ordering::Relaxed) {
                assert_eq!(ctx.load("long").unwrap()["yield_boundary_item"], "build");
                assert!(ctx.admit("other").unwrap().is_some());
                let mut j = ctx.load("other").unwrap();
                j["status"] = json!("succeeded");
                ctx.save(&j).unwrap();
            }
        }));
        t.ctx.supervise("long").unwrap();
        assert!(observed.load(Ordering::Relaxed));
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 2);
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert_eq!(t.ctx.load("long").unwrap()["yield_count"], 1);
        assert_eq!(*t.rt.kills.lock().unwrap(), vec![321]);
    }
    #[test]
    fn long_turn_yields_then_supervisor_resumes_same_session_after_another_job() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        let mut j = waiting(&t, "long", "orch-long");
        j["todo_id"] = json!(1956);
        t.ctx.save(&j).unwrap();
        fs::write(t.ctx.job_dir("long").join("prompt.md"), "original task").unwrap();
        let ctx = t.ctx.clone();
        let rt = t.rt.clone();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            if seq.fetch_add(1, Ordering::Relaxed) == 0 {
                rt.tick(7201.);
                fs::create_dir_all(ctx.job_dir("other")).unwrap();
                ctx.save(&job(json!({"id":"other","name":"orch-other","pid":std::process::id(),"pid_created":123,"queued_at":"2026-10-01T11:59:59+09:00"}))).unwrap();
                let _gate = ctx.queue_lock().unwrap();
                ctx.track_queued_job("other").unwrap();
            }
            let text = format!(
                "{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"item.completed\",\"item\":{{\"type\":\"agent_message\",\"text\":\"状態: 継続\"}}}}\n{{\"type\":\"turn.completed\"}}\n"
            );
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        let ctx = t.ctx.clone();
        let seen = Arc::new(AtomicBool::new(false));
        let observed = seen.clone();
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds == 3. && !observed.swap(true, Ordering::Relaxed) {
                let yielded = ctx.load("long").unwrap();
                assert_eq!(yielded["status"], "queued");
                assert_eq!(yielded["resume_session"], SESSION_ID);
                assert_eq!(yielded["todo_id"], 1956);
                assert_eq!(yielded["yield_last_message"], "状態: 継続");
                assert!(ctx.admit("other").unwrap().is_some());
                let mut other = ctx.load("other").unwrap();
                other["status"] = json!("succeeded");
                ctx.save(&other).unwrap();
            }
        }));
        t.ctx.supervise("long").unwrap();
        assert!(seen.load(Ordering::Relaxed));
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 2);
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        let saved = t.ctx.load("long").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["yield_count"], 1);
        assert_eq!(saved["todo_id"], 1956);
        assert_eq!(
            fs::read_to_string(t.ctx.job_dir("long").join("prompt.md")).unwrap(),
            "original task"
        );
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn legacy_supervisor_is_not_stopped_and_finished_turn_is_requeued_once() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut parent = long_parent(&t, "long");
        waiting(&t, "other", "orch-other");
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.ctx.load("long").unwrap()["status"], "running");
        assert!(t.rt.kills.lock().unwrap().is_empty());
        let owner = t.ctx.job_lock("long", "supervisor.lock").unwrap();
        let mut blocker = waiting(&t, "blocker", "orch-blocker");
        blocker["status"] = json!("running");
        t.ctx.save(&blocker).unwrap();
        finish(&mut parent, Some(0), 7200000, &t.rt.now());
        parent["turn_completed"] = json!(true);
        parent["status"] = json!("succeeded");
        parent["last_message"] = json!("状態: 継続中");
        t.ctx.save(&parent).unwrap();
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        drop(owner);
        assert!(t.ctx.admit("other").unwrap().is_none());
        let yielded = t.ctx.load("long").unwrap();
        assert_eq!(yielded["status"], "queued");
        assert_eq!(yielded["resume_session"], SESSION_ID);
        assert_eq!(yielded["todo_id"], 1956);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn parent_waiting_for_children_or_grandchildren_never_yields() {
        for state in ["queued", "running"] {
            for grandchild in [false, true] {
                let t = Test::new();
                config(&t, json!({"max_running":1}));
                long_parent(&t, "long");
                waiting(&t, "other", "req-other");
                child(&t, "child", "long");
                let id = if grandchild {
                    let mut ended = t.ctx.load("child").unwrap();
                    ended["status"] = json!("succeeded");
                    t.ctx.save(&ended).unwrap();
                    child(&t, "grandchild", "child");
                    "grandchild"
                } else {
                    "child"
                };
                let mut pending = t.ctx.load(id).unwrap();
                pending["status"] = json!(state);
                t.ctx.save(&pending).unwrap();
                assert!(t.ctx.admit("other").unwrap().is_none());
                assert!(!t.ctx.jobs.join(".queue-yields.json").exists());
                assert!(t.rt.kills.lock().unwrap().is_empty());
                pending["status"] = json!("succeeded");
                t.ctx.save(&pending).unwrap();
                assert!(t.ctx.admit("other").unwrap().is_none());
                assert!(t.ctx.jobs.join(".queue-yields.json").exists());
            }
        }
    }
    #[test]
    fn yield_reservation_is_withdrawn_when_a_child_starts_or_waiters_disappear() {
        for child_starts in [false, true] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = long_parent(&t, "long");
            waiting(&t, "other", "orch-other");
            assert!(t.ctx.admit("other").unwrap().is_none());
            if child_starts {
                child(&t, "child", "long");
            } else {
                let mut other = t.ctx.load("other").unwrap();
                other["cancel_requested"] = json!(true);
                t.ctx.save(&other).unwrap();
            }
            parent["status"] = json!("succeeded");
            parent["turn_completed"] = json!(true);
            t.ctx.save(&parent).unwrap();
            t.ctx.admit("other").unwrap();
            assert_eq!(t.ctx.load("long").unwrap()["status"], "succeeded");
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        }
    }
    #[test]
    fn yield_requires_two_hours_a_waiter_a_session_and_unfinished_success() {
        for case in [
            "young",
            "no-waiter",
            "no-session",
            "complete",
            "failed",
            "cancelled",
            "cooldown",
            "dependency",
            "child",
        ] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = long_parent(&t, "long");
            if case == "young" {
                parent["started_at"] = json!("2026-10-01T10:00:01+09:00");
            }
            if case == "child" {
                parent["parent_job_id"] = json!("ancestor");
            }
            t.ctx.save(&parent).unwrap();
            if case != "no-waiter" {
                let mut other = waiting(&t, "other", "orch-other");
                if case == "cooldown" {
                    other["retry_not_before"] = json!(t.rt.epoch() + 60.);
                    t.ctx.save(&other).unwrap();
                }
                if case == "dependency" {
                    t.rt.env.lock().unwrap().insert(
                        "CC_TODO_HOME".into(),
                        t.ctx.root.to_string_lossy().into_owned(),
                    );
                    fs::write(t.ctx.root.join("todo.json"),json!({"items":[{"id":2,"status":"wait","wait_on":[3]},{"id":3,"status":"doing"}]}).to_string()).unwrap();
                    other["todo_id"] = json!(2);
                    t.ctx.save(&other).unwrap();
                }
                t.ctx.admit("other").unwrap();
            }
            parent["status"] = json!("succeeded");
            parent["turn_completed"] = json!(true);
            match case {
                "no-session" => parent["thread_id"] = Value::Null,
                "complete" => parent["last_message"] = json!("状態: 完了\n依頼を完了しました。"),
                "failed" => parent["status"] = json!("failed"),
                "cancelled" => parent["cancel_requested"] = json!(true),
                _ => (),
            }
            t.ctx.save(&parent).unwrap();
            if case != "no-waiter" {
                t.ctx.admit("other").unwrap();
            }
            assert_ne!(t.ctx.load("long").unwrap()["status"], "queued", "{case}");
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{case}");
        }
    }
    #[test]
    fn owner_then_shepherd_then_other_jobs_use_fifo_within_each_class() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        for (id, name, time) in [
            ("other", "orch-other", "08:00:00"),
            ("shep-new", "shep-new", "11:00:00"),
            ("req-new", "req-new", "11:30:00"),
            ("shep-old", "shep-old", "09:00:00"),
            ("req-old", "req-old", "10:00:00"),
        ] {
            let mut j = waiting(&t, id, name);
            j["queued_at"] = json!(format!("2026-10-01T{time}+09:00"));
            t.ctx.save(&j).unwrap();
        }
        for id in ["req-old", "req-new", "shep-old", "shep-new", "other"] {
            assert!(t.ctx.admit(id).unwrap().is_some(), "{id}");
            let mut j = t.ctx.load(id).unwrap();
            j["status"] = json!("succeeded");
            t.ctx.save(&j).unwrap();
        }
    }
    #[test]
    fn stale_reservations_never_revive_finished_or_superseded_work() {
        for case in ["future-waiter", "modern", "successor", "decision"] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            let mut parent = long_parent(&t, "long");
            waiting(&t, "old-waiter", "orch-old");
            assert!(t.ctx.admit("old-waiter").unwrap().is_none());
            parent["status"] = json!("succeeded");
            parent["turn_completed"] = json!(true);
            parent["finished_at"] = json!(t.rt.now());
            parent["gto"] = json!({"quality":4});
            if case == "modern" {
                parent["queue_fairness_version"] = json!(1);
            }
            if case == "decision" {
                parent["last_message"] = json!("状態: 要判断\nオーナーの回答を待ちます。");
            }
            t.ctx.save(&parent).unwrap();
            if case == "successor" {
                let mut next = waiting(&t, "next", "req-reply");
                next["resume_session"] = json!(SESSION_ID);
                t.ctx.save(&next).unwrap();
            }
            if case == "future-waiter" {
                let mut old = t.ctx.load("old-waiter").unwrap();
                old["status"] = json!("cancelled");
                t.ctx.save(&old).unwrap();
                let mut future = waiting(&t, "future", "req-future");
                future["queued_at"] = json!("2026-10-01T14:00:00+09:00");
                t.ctx.save(&future).unwrap();
                t.ctx.admit("future").unwrap();
            } else {
                t.ctx.admit("old-waiter").unwrap();
            }
            assert_eq!(t.ctx.load("long").unwrap(), parent, "{case}");
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0, "{case}");
        }
    }
    #[test]
    fn fairness_io_failures_preserve_completed_turn_results() {
        for broken in [".queue.lock", ".queue-index.json", "yield-resume.md"] {
            let t = Test::new();
            config(&t, json!({"max_running":1}));
            t.rt.advance_epoch.store(true, Ordering::Relaxed);
            waiting(&t, "long", "orch-long");
            fs::write(t.ctx.job_dir("long").join("prompt.md"), "work").unwrap();
            let ctx = t.ctx.clone();
            let rt = t.rt.clone();
            *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
                rt.tick(7201.);
                fs::create_dir_all(ctx.job_dir("other")).unwrap();
                ctx.save(&job(json!({"id":"other","name":"orch-other","pid":std::process::id(),"pid_created":123}))).unwrap();
                {
                    let _gate = ctx.queue_lock().unwrap();
                    ctx.track_queued_job("other").unwrap();
                }
                if broken == "yield-resume.md" {
                    fs::create_dir(ctx.job_dir("long").join(broken)).unwrap();
                } else {
                    fs::rename(
                        ctx.jobs.join(broken),
                        ctx.jobs.join(format!("{broken}.saved")),
                    )
                    .unwrap();
                    fs::create_dir(ctx.jobs.join(broken)).unwrap();
                }
                Box::new(FakeChild {
                    alive: Arc::new(AtomicBool::new(false)),
                    out: Some(Box::new(Cursor::new(OK.as_bytes()))),
                })
            }));
            t.ctx.supervise("long").unwrap();
            let saved = t.ctx.load("long").unwrap();
            assert_eq!(saved["status"], "succeeded", "{broken}");
            assert_eq!(saved["last_message"], "DONE", "{broken}");
            assert_eq!(saved["exit_code"], 0, "{broken}");
            assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1, "{broken}");
            assert!(
                t.rt.diagnostics
                    .lock()
                    .unwrap()
                    .iter()
                    .any(|line| line.contains("枠を譲る処理を延期")),
                "{broken}"
            );
        }
    }
    #[test]
    fn one_broken_yield_reservation_never_stops_unrelated_admission() {
        for broken in ["missing", "unreadable", "spawn", "ledger"] {
            let t = Test::new();
            let mut parent = long_parent(&t, "long");
            let mut blocker = waiting(&t, "blocker", "orch-blocker");
            blocker["status"] = json!("running");
            t.ctx.save(&blocker).unwrap();
            config(&t, json!({"max_running":1}));
            waiting(&t, "other", "req-other");
            t.ctx.admit("other").unwrap();
            match broken {
                "missing" => {
                    fs::rename(t.ctx.job_dir("long"), t.ctx.root.join("moved-long")).unwrap();
                }
                "unreadable" => {
                    fs::write(t.ctx.job_dir("long").join("job.json"), "{").unwrap();
                }
                "spawn" => {
                    parent["status"] = json!("succeeded");
                    parent["turn_completed"] = json!(true);
                    parent["finished_at"] = json!(t.rt.now());
                    t.ctx.save(&parent).unwrap();
                    t.rt.supervisor_fail.store(true, Ordering::Relaxed);
                }
                "ledger" => {
                    fs::write(t.ctx.jobs.join(".queue-yields.json"), "{").unwrap();
                }
                _ => unreachable!(),
            }
            config(&t, json!({"max_running":6}));
            assert!(t.ctx.admit("other").unwrap().is_some(), "{broken}");
            if broken == "spawn" {
                assert_eq!(t.ctx.load("long").unwrap(), parent);
            }
        }
    }
    #[test]
    fn queued_supervisor_refreshes_activity_even_during_cooldown() {
        let t = Test::new();
        let mut j = waiting(&t, "heartbeat", "wait");
        j["retry_not_before"] = json!(t.rt.epoch() + 900.);
        t.ctx.save(&j).unwrap();
        let path = t.ctx.job_dir("heartbeat").join("job.json");
        let now = t.rt.now();
        let count = AtomicUsize::new(0);
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds != 3. {
                return;
            }
            let n = count.fetch_add(1, Ordering::Relaxed);
            let mut j: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
            if n == 0 {
                j["last_event_at"] = json!("2000-01-01T00:00:00+09:00");
                fs::write(&path, pretty(&j)).unwrap();
            }
            if n == 20 {
                assert_eq!(j["last_event_at"], now);
                j["status"] = json!("cancelled");
                fs::write(&path, pretty(&j)).unwrap();
            }
        }));
        t.ctx.supervise("heartbeat").unwrap();
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn supervisor_refresh_is_not_execution_admission_during_history_reconciliation() {
        let t = Test::new();
        waiting(&t, "target", "target");
        let index_path = t.ctx.jobs.join(".queue-index.json");
        let mut index = t.ctx.read_json(&index_path).unwrap();
        index["scan_complete"] = json!(false);
        t.ctx.write_json(&index_path, &index).unwrap();
        let scan = FileLock::try_acquire(&t.ctx.jobs.join(".queue-scan.lock")).unwrap();
        assert_eq!(t.ctx.reload_queue().unwrap(), json!(["target"]));
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        let saved = t.ctx.load("target").unwrap();
        assert_eq!(saved["status"], "queued");
        assert!(saved["started_at"].is_null());
        assert!(saved["queue_restart_pending"] == true);
        assert_eq!(t.ctx.admit("target").err().unwrap().code, 75);
        drop(scan);
    }
    #[test]
    fn targeted_supervisor_refresh_preserves_other_workers_and_rejects_unusable_targets() {
        for case in [
            "queued",
            "running",
            "starting",
            "surviving",
            "cancelled",
            "wrong_id",
            "bad_id",
            "missing",
            "busy",
            "missing_index",
            "broken_index",
            "missing_scan_flag",
            "wrong_scan_flag",
            "wrong_scan_time",
        ] {
            let t = Test::new();
            let mut target = waiting(&t, "target", "target");
            target["pid"] = json!(444);
            target["pid_created"] = json!(55);
            target["resume_session"] = json!(SESSION_ID);
            t.ctx.save(&target).unwrap();
            t.rt.procs.lock().unwrap().insert(444, 55);
            let mut other = waiting(&t, "other", "other");
            other["pid"] = json!(445);
            other["pid_created"] = json!(56);
            t.ctx.save(&other).unwrap();
            t.rt.procs.lock().unwrap().insert(445, 56);
            let mut requested = "target";
            match case {
                "running" | "starting" => target["status"] = json!(case),
                "surviving" => {
                    target["codex_pid"] = json!(446);
                    target["codex_pid_created"] = json!(57);
                    t.rt.procs.lock().unwrap().insert(446, 57);
                }
                "cancelled" => target["cancel_requested"] = json!(true),
                "wrong_id" => target["id"] = json!("other"),
                "bad_id" => requested = "../other",
                "missing" => requested = "absent",
                "missing_index" => fs::remove_file(t.ctx.jobs.join(".queue-index.json")).unwrap(),
                "broken_index" => {
                    fs::write(t.ctx.jobs.join(".queue-index.json"), "{broken").unwrap()
                }
                "missing_scan_flag" | "wrong_scan_flag" | "wrong_scan_time" => {
                    let path = t.ctx.jobs.join(".queue-index.json");
                    let mut index = t.ctx.read_json(&path).unwrap();
                    index["scanned_at"] = json!(t.rt.epoch());
                    index["scan_complete"] = json!(true);
                    match case {
                        "missing_scan_flag" => {
                            index.as_object_mut().unwrap().remove("scan_complete");
                        }
                        "wrong_scan_flag" => index["scan_complete"] = json!("complete"),
                        _ => index["scanned_at"] = json!("recent"),
                    }
                    t.ctx.write_json(&path, &index).unwrap();
                }
                _ => {}
            }
            let target_path = t.ctx.job_dir("target").join("job.json");
            fs::write(&target_path, target.to_string()).unwrap();
            let other_path = t.ctx.job_dir("other").join("job.json");
            let untouched = fs::read(&other_path).unwrap();
            let held = (case == "busy").then(|| t.ctx.job_lock("target", "state.lock").unwrap());
            let updates = [
                "queued",
                "missing_index",
                "broken_index",
                "missing_scan_flag",
                "wrong_scan_flag",
                "wrong_scan_time",
            ]
            .contains(&case);
            let (result, out, _) = t.cli(&["reload-queue", "--job-id", requested]);
            if ["wrong_id", "bad_id", "missing"].contains(&case) {
                assert!(result.is_err(), "{case}");
            } else {
                assert_eq!(result.unwrap(), 0, "{case}");
                let report: Value = serde_json::from_str(&out).unwrap();
                assert_eq!(
                    report,
                    if updates {
                        json!(["target"])
                    } else {
                        json!([])
                    },
                    "{case}"
                );
            }
            assert_eq!(fs::read(&other_path).unwrap(), untouched, "{case}");
            assert_eq!(
                t.rt.supervisors.load(Ordering::Relaxed),
                usize::from(updates),
                "{case}"
            );
            if updates {
                assert_eq!(*t.rt.kills.lock().unwrap(), [444]);
                assert_eq!(t.ctx.load("target").unwrap()["resume_session"], SESSION_ID);
                assert!(t.ctx.load("target").unwrap()["started_at"].is_null());
                if case.contains("index") || case.contains("scan_") {
                    let index = t
                        .ctx
                        .read_json(&t.ctx.jobs.join(".queue-index.json"))
                        .unwrap();
                    assert_eq!(
                        index["ids"],
                        if case.contains("scan_") {
                            json!(["target", "other"])
                        } else {
                            json!(["target"])
                        }
                    );
                    assert_eq!(index["scan_complete"], false);
                    assert_eq!(index["scanned_at"], 0.);
                    let scan = FileLock::try_acquire(&t.ctx.jobs.join(".queue-scan.lock")).unwrap();
                    assert_eq!(t.ctx.admit("target").err().unwrap().code, 75);
                    drop(scan);
                }
            } else {
                assert!(t.rt.kills.lock().unwrap().is_empty(), "{case}");
                assert_eq!(
                    fs::read(&target_path).unwrap(),
                    target.to_string().as_bytes(),
                    "{case}"
                );
            }
            drop(held);
        }
    }
    #[test]
    fn reload_queue_only_restarts_waiters_and_preserves_session() {
        let t = Test::new();
        let mut j = waiting(&t, "waiting", "wait");
        j["pid"] = json!(444);
        j["pid_created"] = json!(55);
        j["resume_session"] = json!(SESSION_ID);
        t.ctx.save(&j).unwrap();
        t.rt.procs.lock().unwrap().insert(444, 55);
        t.put(job(
            json!({"id":"running","status":"running","pid":666,"pid_created":77}),
        ));
        let report = t.ctx.reload_queue().unwrap();
        assert_eq!(report, json!(["waiting"]));
        assert_eq!(*t.rt.kills.lock().unwrap(), [444]);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.load("waiting").unwrap()["resume_session"], SESSION_ID);
        assert_eq!(t.ctx.load("running").unwrap()["status"], "running");
        assert!(
            !t.rt
                .calls
                .lock()
                .unwrap()
                .iter()
                .any(|call| call.argv.first().is_some_and(|a| a == "taskkill"))
        );
    }
    #[test]
    fn queue_summary_matches_real_admission_and_fills_eight_slots() {
        let t = Test::new();
        config(&t, json!({"max_running":8}));
        for i in 0..10 {
            waiting(&t, &format!("slot-{i:02}"), "ordinary name with spaces");
        }
        for i in 0..6 {
            assert!(t.ctx.admit(&format!("slot-{i:02}")).unwrap().is_some());
        }
        let report = t
            .ctx
            .queue_report(&t.ctx.list_jobs(&HashSet::new()))
            .unwrap();
        assert_eq!(report["occupied_slots"], 6);
        assert_eq!(report["queued_count"], 4);
        assert_eq!(report["queued"][0]["reason"]["code"], "ready");
        assert_eq!(report["queued"][1]["reason"]["code"], "ready");
        assert_eq!(report["queued"][2]["reason"]["code"], "running_limit");
        assert!(t.ctx.admit("slot-06").unwrap().is_some());
        assert!(t.ctx.admit("slot-07").unwrap().is_some());
        assert!(t.ctx.admit("slot-08").unwrap().is_none());
        let (result, out, _) = t.cli(&["status", "--summary", "--json"]);
        assert_eq!(result.unwrap(), 0);
        let report: Value = serde_json::from_str(&out).unwrap();
        assert_eq!(report["summary"]["occupied_slots"], 8);
        assert_eq!(report["summary"]["queued_count"], 2);
        assert_eq!(report["jobs"][0]["name"], "ordinary name with spaces");
        let (_, line, _) = t.cli(&["status", "--summary"]);
        assert_eq!(line.lines().count(), 1);
        assert!(line.contains("実行中 8/8枠"));
    }
    #[test]
    fn wait_reasons_cover_low_priority_cooldown_children_and_yielded_jobs() {
        let t = Test::new();
        config(
            &t,
            json!({"max_running":8,"max_children_per_parent":1,"lowest_priority_todo_ids":[18,23]}),
        );
        let mut normal = waiting(&t, "normal", "normal");
        normal["retry_not_before"] = json!(t.rt.epoch() + 60.);
        t.ctx.save(&normal).unwrap();
        let mut low = waiting(&t, "low", "normal");
        low["todo_id"] = json!(18);
        t.ctx.save(&low).unwrap();
        let mut parent = waiting(&t, "parent", "parent");
        parent["status"] = json!("running");
        t.ctx.save(&parent).unwrap();
        let mut one = child(&t, "child-one", "parent");
        one["status"] = json!("running");
        t.ctx.save(&one).unwrap();
        child(&t, "child-two", "parent");
        let mut yielded = waiting(&t, "yielded", "normal");
        yielded["yield_resume"] = json!(true);
        t.ctx.save(&yielded).unwrap();
        let report = t
            .ctx
            .queue_report(&t.ctx.list_jobs(&HashSet::new()))
            .unwrap();
        let reason = |id: &str| {
            report["queued"]
                .as_array()
                .unwrap()
                .iter()
                .find(|j| s(j, "id") == id)
                .unwrap()["reason"]["code"]
                .clone()
        };
        assert_eq!(reason("normal"), "retry_cooldown");
        assert_eq!(reason("low"), "lowest_priority");
        assert_eq!(reason("child-two"), "child_limit");
        assert_eq!(reason("yielded"), "ready");
        assert_eq!(report["occupied_slots"], 1);
        assert_eq!(report["running"], 2);
        assert!(t.ctx.admit("low").unwrap().is_none());
        assert!(t.ctx.admit("child-two").unwrap().is_none());
        assert!(t.ctx.admit("yielded").unwrap().is_some());
    }
    #[test]
    fn starting_and_unreadable_reservations_cannot_be_hidden_by_status() {
        let t = Test::new();
        config(&t, json!({"max_running":2}));
        let mut pending = waiting(&t, "starting", "starting");
        pending["status"] = json!("starting");
        t.ctx.save(&pending).unwrap();
        waiting(&t, "unreadable", "unreadable");
        fs::write(t.ctx.job_dir("unreadable").join("job.json"), "{").unwrap();
        waiting(&t, "next", "next");
        assert!(t.ctx.admit("next").unwrap().is_none());
        let (code, out, _) = t.cli(&["status", "--summary", "--json"]);
        assert_eq!(code.unwrap(), 0);
        let report: Value = serde_json::from_str(&out).unwrap();
        assert_eq!(report["summary"]["occupied_slots"], 2);
        assert_eq!(report["summary"]["queued_count"], 1);
    }
    #[test]
    fn cleanup_only_stops_terminal_identity_without_live_child_or_handoff() {
        let t = Test::new();
        for (i, id) in [
            "done",
            "live-child",
            "reused",
            "handoff",
            "running",
            "queued",
            "recent",
        ]
        .iter()
        .enumerate()
        {
            let pid = 500 + i as u32;
            let mut j = waiting(&t, id, id);
            j["pid"] = json!(pid);
            j["pid_created"] = json!(55);
            j["status"] = json!("succeeded");
            j["finished_at"] = json!(if *id == "recent" {
                t.rt.now()
            } else {
                "2026-10-01T11:57:00+09:00".into()
            });
            t.rt.procs
                .lock()
                .unwrap()
                .insert(pid, if *id == "reused" { 56 } else { 55 });
            if *id == "live-child" {
                j["codex_pid"] = json!(600);
                j["codex_pid_created"] = json!(66);
                t.rt.procs.lock().unwrap().insert(600, 66);
            }
            if ["running", "queued"].contains(id) {
                j["status"] = json!(id);
            }
            t.ctx.save(&j).unwrap();
            if *id == "handoff" {
                fs::write(
                    t.ctx.job_dir(id).join("yield-transition.json"),
                    json!({"complete":false,"source":{"id":id}}).to_string(),
                )
                .unwrap();
            }
        }
        let original = t.ctx.load("done").unwrap();
        let preview = t.ctx.cleanup_supervisors(false).unwrap();
        assert_eq!(preview.as_array().unwrap().len(), 1);
        assert!(t.rt.kills.lock().unwrap().is_empty());
        assert_eq!(t.ctx.load("done").unwrap(), original);
        assert_eq!(t.ctx.cleanup_supervisors(true).unwrap()[0]["id"], "done");
        assert_eq!(*t.rt.kills.lock().unwrap(), [500]);
        let done = t.ctx.load("done").unwrap();
        assert_eq!(done["finished_at"], original["finished_at"]);
        assert_eq!(done["status"], "succeeded");
        assert!(
            t.ctx
                .cleanup_supervisors(true)
                .unwrap()
                .as_array()
                .unwrap()
                .is_empty()
        );
    }
    #[test]
    fn invalid_queue_config_keeps_status_available_with_explicit_error() {
        let t = Test::new();
        waiting(&t, "pending", "name with spaces");
        fs::write(t.ctx.root.join("queue-config.json"), "{").unwrap();
        for args in [
            vec!["status"],
            vec!["status", "--json"],
            vec!["status", "--summary", "--json"],
        ] {
            let (result, out, _) = t.cli(&args);
            assert_eq!(result.unwrap(), 0);
            if args.contains(&"--json") {
                let data: Value = serde_json::from_str(&out).unwrap();
                if args.contains(&"--summary") {
                    assert!(!data["summary"]["config_error"].is_null());
                    assert!(data["summary"]["max_running"].is_null());
                } else {
                    assert!(data.is_array());
                    assert_eq!(data[0]["queue_wait_reason"]["code"], "config_error");
                }
            } else {
                assert!(out.contains("不明（設定エラー）"));
                assert!(out.contains("name with spaces"));
            }
        }
    }
    #[test]
    fn failed_supervisor_reload_does_not_prevent_other_waiters_upgrading() {
        let t = Test::new();
        for (id, pid) in [("a-failing", 444), ("b-good", 445)] {
            let mut j = waiting(&t, id, id);
            j["pid"] = json!(pid);
            j["pid_created"] = json!(55);
            j["resume_session"] = json!(SESSION_ID);
            t.ctx.save(&j).unwrap();
            t.rt.procs.lock().unwrap().insert(pid, 55);
        }
        t.rt.paused_stop_failure.store(true, Ordering::Relaxed);
        t.rt.paused_stop_failure_pid.store(444, Ordering::Relaxed);
        let report = t.ctx.reload_queue().unwrap();
        assert_eq!(report[0]["action"], "deferred");
        assert_eq!(report[1], "b-good");
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.load("a-failing").unwrap()["pid"], 444);
        assert_eq!(
            t.ctx.load("a-failing").unwrap()["queue_restart_pending"],
            true
        );
        t.rt.paused_stop_failure.store(false, Ordering::Relaxed);
        assert_eq!(
            t.ctx.reload_queue_job("a-failing").unwrap(),
            json!(["a-failing"])
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        assert_eq!(
            t.ctx.load("a-failing").unwrap()["resume_session"],
            SESSION_ID
        );
        assert_eq!(t.ctx.load("a-failing").unwrap()["status"], "queued");
    }
    #[test]
    fn reload_does_not_wait_for_a_busy_registration_or_hold_the_queue_between_jobs() {
        let t = Test::new();
        let busy = waiting(&t, "busy", "busy");
        waiting(&t, "available", "available");
        let held = t.ctx.job_lock("busy", "state.lock").unwrap();
        let begin = Instant::now();
        assert_eq!(t.ctx.reload_queue().unwrap(), json!(["available"]));
        assert!(begin.elapsed() < Duration::from_secs(2));
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.load("busy").unwrap(), busy);
        drop(t.ctx.queue_lock().unwrap());
        drop(held);
        assert!(
            t.ctx
                .reload_queue()
                .unwrap()
                .as_array()
                .unwrap()
                .contains(&json!("busy"))
        );
    }
    #[test]
    fn transient_waiting_record_read_failure_does_not_fail_the_job() {
        let t = Test::new();
        waiting(&t, "wait", "wait");
        fs::write(t.ctx.job_dir("wait").join("prompt.md"), "work").unwrap();
        let path = t.ctx.job_dir("wait").join("job.json");
        let injected = Arc::new(AtomicBool::new(false));
        let seen = injected.clone();
        let runtime = Arc::downgrade(&t.rt);
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |loaded, _| {
            if loaded == path && !seen.swap(true, Ordering::Relaxed) {
                runtime
                    .upgrade()
                    .unwrap()
                    .load_failures
                    .store(50, Ordering::Relaxed);
            }
        }));
        t.ctx.supervise("wait").unwrap();
        assert!(injected.load(Ordering::Relaxed));
        assert_eq!(t.ctx.load("wait").unwrap()["status"], "succeeded");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        assert!(
            t.rt.diagnostics
                .lock()
                .unwrap()
                .iter()
                .any(|line| line.starts_with("順番待ちの状態読取りを再試行"))
        );
    }
    #[test]
    fn recovery_requeues_watchdog_cancelled_recovery_but_never_owner_cancelled() {
        for explicit in [false, true] {
            let t = Test::new();
            let mut j = failed(&t, "recover", 1, "2026-10-01T12:00:00+09:00");
            j["status"] = json!("cancelled");
            j["recovered_429_at"] = json!(t.rt.now());
            j["cancel_requested"] = json!(explicit);
            t.ctx.save(&j).unwrap();
            let todo = t.ctx.root.join("todo.json");
            fs::write(
                &todo,
                json!({"items":[{"id":1,"status":"doing","jobs":["recover"]}]}).to_string(),
            )
            .unwrap();
            let report = t
                .ctx
                .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
                .unwrap();
            if explicit {
                assert_eq!(report, json!([]));
                assert_eq!(t.ctx.load("recover").unwrap()["status"], "cancelled");
            } else {
                assert_eq!(report[0]["action"], "queued");
            }
            assert_eq!(
                t.rt.supervisors.load(Ordering::Relaxed),
                usize::from(!explicit)
            );
        }
    }
    #[test]
    fn recovered_supervisor_identity_survives_recovery_command_exit() {
        let t = Test::new();
        failed(&t, "revive", 1, "2026-10-01T11:00:00+09:00");
        t.rt.supervisor_pid.store(888, Ordering::Relaxed);
        t.rt.procs.lock().unwrap().insert(888, 777);
        let todo = t.ctx.root.join("todo.json");
        fs::write(
            &todo,
            json!({"items":[{"id":1,"status":"doing","jobs":["revive"]}]}).to_string(),
        )
        .unwrap();
        t.ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
            .unwrap();
        // The detached supervisor has not executed any code yet, and the
        // recovery process has exited. Its recorded child identity is enough.
        t.rt.procs.lock().unwrap().remove(&std::process::id());
        let j = t.ctx.refresh(t.ctx.load("revive").unwrap(), false).unwrap();
        assert_eq!(j["pid"], 888);
        assert_eq!(j["pid_created"], 777);
        assert_eq!(j["status"], "queued");
        assert!(t.ctx.supervisor_alive(&j));
    }
    #[test]
    fn damaged_index_rebuilds_and_unreadable_active_record_blocks_admission() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "one");
        assert!(t.ctx.admit("a").unwrap().is_some());
        waiting(&t, "b", "two");
        fs::write(t.ctx.jobs.join(".queue-index.json"), "{").unwrap();
        {
            let _gate = t.ctx.queue_lock().unwrap();
            t.ctx.track_queued_job("b").unwrap();
        }
        assert!(t.ctx.admit("b").unwrap().is_none());
        fs::write(t.ctx.job_dir("a").join("job.json"), "{").unwrap();
        assert!(t.ctx.admit("b").unwrap().is_none());
        assert_eq!(t.ctx.load("b").unwrap()["status"], "queued");
        let j = job(json!({"id":"a","status":"succeeded"}));
        t.ctx.save(&j).unwrap();
        assert!(t.ctx.admit("b").unwrap().is_some());
    }
    #[test]
    fn temporarily_invalid_settings_keep_waiting_job_then_admit_it() {
        let t = Test::new();
        waiting(&t, "wait", "wait");
        fs::write(t.ctx.job_dir("wait").join("prompt.md"), "work").unwrap();
        let cfg = t.ctx.root.join("queue-config.json");
        fs::write(&cfg, "{").unwrap();
        let saved = t.ctx.job_dir("wait").join("job.json");
        let seen = Arc::new(AtomicBool::new(false));
        let observed = seen.clone();
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds == 3. && !observed.swap(true, Ordering::Relaxed) {
                let j: Value = serde_json::from_slice(&fs::read(&saved).unwrap()).unwrap();
                assert_eq!(j["status"], "queued");
                fs::write(&cfg, "{\"max_running\":1}").unwrap();
            }
        }));
        t.ctx.supervise("wait").unwrap();
        assert!(seen.load(Ordering::Relaxed));
        assert_eq!(t.ctx.load("wait").unwrap()["status"], "succeeded");
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        assert!(
            t.rt.diagnostics
                .lock()
                .unwrap()
                .iter()
                .any(|s| s.starts_with("順番待ちを継続"))
        );
    }
    #[test]
    fn waiting_supervisor_skips_global_gate_while_its_wait_reason_is_unchanged() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut busy = waiting(&t, "busy", "busy");
        busy["status"] = json!("running");
        t.ctx.save(&busy).unwrap();
        waiting(&t, "wait", "wait");
        let gate_path = t.ctx.jobs.join(".queue.lock");
        let saved = t.ctx.job_dir("wait").join("job.json");
        let polls = Arc::new(AtomicUsize::new(0));
        let counted = polls.clone();
        let held = Mutex::new(None);
        *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |seconds| {
            if seconds != 3. {
                return;
            }
            match counted.fetch_add(1, Ordering::Relaxed) {
                // The first gated pass recorded the reason; hold the gate as a
                // stuck registrar would. Later polls must not wait for it.
                0 => *held.lock().unwrap() = Some(FileLock::acquire_queue(&gate_path).unwrap()),
                5 => {
                    let mut j: Value = serde_json::from_slice(&fs::read(&saved).unwrap()).unwrap();
                    assert_eq!(j["queue_wait_reason"]["code"], "running_limit");
                    j["cancel_requested"] = json!(true);
                    fs::write(&saved, j.to_string()).unwrap();
                    held.lock().unwrap().take();
                }
                _ => {}
            }
        }));
        let started = std::time::Instant::now();
        t.ctx.supervise("wait").unwrap();
        assert_eq!(polls.load(Ordering::Relaxed), 6);
        assert!(
            started.elapsed() < Duration::from_secs(20),
            "{:?}",
            started.elapsed()
        );
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn broken_settings_after_spawn_do_not_erase_final_result() {
        let t = Test::new();
        waiting(&t, "finish", "finish");
        fs::write(t.ctx.job_dir("finish").join("prompt.md"), "work").unwrap();
        let cfg = t.ctx.root.join("queue-config.json");
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            fs::write(&cfg, "{").unwrap();
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(OK.as_bytes()))),
            })
        }));
        t.ctx.supervise("finish").unwrap();
        assert_eq!(t.ctx.load("finish").unwrap()["status"], "succeeded");
    }
    #[test]
    fn supervisor_exhausts_bounded_retries_without_creating_fresh_session() {
        let t = Test::new();
        config(&t, json!({"retry_delay_seconds":1,"max_429_retries":2}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        waiting(&t, "exhaust", "req-exhaust");
        fs::write(t.ctx.job_dir("exhaust").join("prompt.md"), "continue").unwrap();
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(|| {
            let text = format!(
                "{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"turn.failed\",\"error\":{{\"message\":\"429 Too Many Requests\"}}}}\n"
            );
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        t.ctx.supervise("exhaust").unwrap();
        let saved = t.ctx.load("exhaust").unwrap();
        assert_eq!(saved["status"], "failed");
        assert_eq!(saved["retry_count"], 2);
        let args = t.rt.spawn_args.lock().unwrap();
        assert_eq!(args.len(), 3);
        assert!(
            args[1..]
                .iter()
                .all(|a| a.windows(2).any(|w| w == ["resume", SESSION_ID]))
        );
    }
    #[test]
    fn active_index_includes_new_registrations_and_reconciles_legacy_jobs() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "ordinary");
        assert!(t.ctx.admit("a").unwrap().is_some());
        let mut j = t.ctx.load("a").unwrap();
        j["status"] = json!("succeeded");
        t.ctx.save(&j).unwrap();
        waiting(&t, "b", "ordinary");
        {
            let _gate = t.ctx.queue_lock().unwrap();
            t.ctx.track_queued_job("b").unwrap();
        }
        assert!(t.ctx.admit("b").unwrap().is_some());
        j = t.ctx.load("b").unwrap();
        j["status"] = json!("succeeded");
        t.ctx.save(&j).unwrap();
        waiting(&t, "c", "legacy");
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        t.rt.tick(31.);
        assert!(t.ctx.admit("c").unwrap().is_some());
        let index: Value =
            serde_json::from_slice(&fs::read(t.ctx.jobs.join(".queue-index.json")).unwrap())
                .unwrap();
        assert_eq!(index["ids"], json!(["c"]));
    }
    #[test]
    fn cancelling_during_retry_cooldown_closes_existing_card() {
        let t = Test::new();
        let mut j = waiting(&t, "cancel-retry", "req-cancel");
        j["first_started_at"] = json!(t.rt.now());
        j["retry_count"] = json!(1);
        j["started_at"] = Value::Null;
        j["retry_not_before"] = json!(t.rt.epoch() + 60.);
        t.ctx.save(&j).unwrap();
        fs::write(
            t.ctx.job_dir("cancel-retry").join("prompt.md"),
            "作業: 再開待ちの検査",
        )
        .unwrap();
        task_cancel(&j, Some("ao.exe".into()), t.ctx.jobs.clone(), t.rt.clone());
        let calls = t.rt.calls.lock().unwrap();
        assert!(
            calls
                .iter()
                .any(|call| call.argv.get(3).is_some_and(|a| a == "finish"))
        );
    }

    fn waiting(t: &Test, id: &str, name: &str) -> Value {
        let j = t.put(job(json!({"id":id,"name":name,"pid":std::process::id(),"pid_created":123,"codex":"codex.CMD","cwd":t.ctx.root})));
        let _gate = t.ctx.queue_lock().unwrap();
        t.ctx.track_queued_job(id).unwrap();
        j
    }
    fn config(t: &Test, value: Value) {
        fs::write(t.ctx.root.join("queue-config.json"), value.to_string()).unwrap();
    }
    fn loop_job(t: &Test, id: &str, todo: u64) -> Value {
        fs::create_dir_all(&t.ctx.root).unwrap();
        // These regression cases exercise an explicitly configured legacy tier.
        if !t.ctx.root.join("queue-config.json").exists() {
            config(t, json!({"lowest_priority_todo_ids":[18,23]}));
        }
        let mut j = waiting(t, id, "req-loop");
        j["todo_id"] = json!(todo);
        t.ctx.save(&j).unwrap();
        j
    }
    #[test]
    fn department_order_wins_over_request_prefix_and_children() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut low = waiting(&t, "low", "req-low");
        low["project"] = json!("unknown");
        t.ctx.save(&low).unwrap();
        let mut high = waiting(&t, "high", "orch-client");
        high["project"] = json!("SocialPilot");
        t.ctx.save(&high).unwrap();
        assert!(t.ctx.admit("low").unwrap().is_none());
        assert!(t.ctx.admit("high").unwrap().is_some());
        assert!(t.ctx.admit("low").unwrap().is_none());
    }
    #[test]
    fn configured_legacy_lowest_cannot_demote_contract_below_own_products() {
        let t = Test::new();
        config(&t, json!({"max_running":1,"lowest_priority_todo_ids":[18]}));
        let mut high = waiting(&t, "high", "orch-client");
        high["project"] = json!("SocialPilot");
        high["todo_id"] = json!(18);
        t.ctx.save(&high).unwrap();
        let mut low = waiting(&t, "low", "req-own");
        low["project"] = json!("unknown");
        t.ctx.save(&low).unwrap();
        assert!(t.ctx.admit("high").unwrap().is_some());
    }
    #[test]
    fn running_own_product_yields_to_a_ready_higher_department_at_tool_boundary() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut parent = live_tool_parent(&t);
        parent["project"] = json!("unknown");
        parent["started_at"] = json!(t.rt.now());
        t.ctx.save(&parent).unwrap();
        let mut other = t.ctx.load("other").unwrap();
        other["project"] = json!("SocialPilot");
        t.ctx.save(&other).unwrap();
        tool_events(&t, &[("item.started", "work"), ("item.completed", "work")]);
        assert!(t.ctx.admit("other").unwrap().is_some());
        let saved = t.ctx.load("long").unwrap();
        assert_eq!(saved["status"], "queued");
        assert_eq!(saved["resume_session"], SESSION_ID);
    }
    #[test]
    fn loops_wait_for_normal_work_even_with_spare_capacity_and_cooldown() {
        for todo in [18, 23] {
            let t = Test::new();
            loop_job(&t, "loop", todo);
            let mut other = waiting(&t, "ordinary", "ordinary");
            other["retry_not_before"] = json!(t.rt.epoch() + 3600.);
            t.ctx.save(&other).unwrap();
            assert!(t.ctx.admit("loop").unwrap().is_none());
            other["retry_not_before"] = json!(0);
            t.ctx.save(&other).unwrap();
            assert!(t.ctx.admit("loop").unwrap().is_none());
            assert!(t.ctx.admit("ordinary").unwrap().is_some());
            assert!(t.ctx.admit("loop").unwrap().is_some());
        }
    }
    #[test]
    fn lowest_tier_remains_fifo_and_ignores_dead_or_cancelled_waiters() {
        let t = Test::new();
        loop_job(&t, "a", 18);
        loop_job(&t, "b", 23);
        let mut dead = waiting(&t, "dead", "ordinary");
        dead["pid"] = json!(999999);
        t.ctx.save(&dead).unwrap();
        let mut cancelled = waiting(&t, "cancelled", "req-owner");
        cancelled["cancel_requested"] = json!(true);
        t.ctx.save(&cancelled).unwrap();
        config(&t, json!({"max_running":1}));
        assert!(t.ctx.admit("b").unwrap().is_none());
        assert!(t.ctx.admit("a").unwrap().is_some());
        assert!(t.ctx.admit("b").unwrap().is_none());
    }
    #[test]
    fn only_configured_ids_set_the_lowest_tier_and_config_changes_apply() {
        // Prompt bodies, job titles and ToDo titles quote shared guidance and
        // ordinary wording ("繰り返し起動しない"); none of them is a tier.
        for word in [
            "くり返す",
            "回し続ける",
            "agents.prompt",
            "繰り返し起動しない",
        ] {
            let mut t = Test::new();
            let tools = t.ctx.root.clone();
            t.ctx.root = tools.join("codex-jobs");
            t.ctx.jobs = t.ctx.root.join("jobs");
            fs::create_dir_all(tools.join("todo")).unwrap();
            fs::write(
                tools.join("todo/todo.json"),
                json!({"items":[{"id":9000,"title":word}]}).to_string(),
            )
            .unwrap();
            let mut j = loop_job(&t, "loop", 9000);
            j["task_title"] = json!(word);
            t.ctx.save(&j).unwrap();
            fs::write(t.ctx.job_dir("loop").join("prompt.md"), word).unwrap();
            waiting(&t, "ordinary", "ordinary");
            assert!(t.ctx.admit("loop").unwrap().is_some(), "{word}");
        }
        let t = Test::new();
        loop_job(&t, "loop", 9000);
        waiting(&t, "ordinary", "ordinary");
        config(&t, json!({"lowest_priority_todo_ids":[9000]}));
        assert!(t.ctx.admit("loop").unwrap().is_none());
        config(
            &t,
            json!({"lowest_priority_todo_ids":[9000],"normal_priority_todo_ids":[9000]}),
        );
        assert!(t.ctx.admit("loop").unwrap().is_some());
    }
    #[test]
    fn blocked_normal_children_block_loops_but_running_loop_dependencies_can_finish() {
        let t = Test::new();
        loop_job(&t, "loop", 18);
        assert!(t.ctx.admit("loop").unwrap().is_some());
        child(&t, "dependency", "loop");
        waiting(&t, "ordinary", "ordinary");
        assert!(t.ctx.admit("dependency").unwrap().is_some());
        child(&t, "second", "loop");
        assert!(t.ctx.admit("second").unwrap().is_some());
        child(&t, "third", "loop");
        let mut other = t.ctx.load("ordinary").unwrap();
        other["status"] = json!("succeeded");
        t.ctx.save(&other).unwrap();
        loop_job(&t, "another-loop", 23);
        assert!(t.ctx.admit("another-loop").unwrap().is_none());
    }
    /// 2026-10-08: shared write guidance ("複数なら--onを繰り返す") was in every
    /// prompt, so prompt scanning marked all jobs lowest and admission became
    /// plain FIFO: older orch- work and ToDo 18 started ahead of req- jobs.
    #[test]
    fn shared_prompt_guidance_keeps_req_first_and_loops_last() {
        let t = Test::new();
        config(
            &t,
            json!({"max_running":1,"lowest_priority_todo_ids":[18,23]}),
        );
        let guidance = cc_job_policy::WRITE_GUIDANCE;
        assert!(guidance.contains("繰り返す"));
        let mut lowest = loop_job(&t, "loop-18", 18);
        lowest["name"] = json!("orch-todo-18");
        lowest["created_at"] = json!("2026-10-08T08:00:00+09:00");
        t.ctx.save(&lowest).unwrap();
        for (id, name, at) in [
            ("orch-old", "orch-todo-1161", "2026-10-08T09:16:00+09:00"),
            ("req-new", "req-x-skill", "2026-10-08T09:23:00+09:00"),
        ] {
            let mut j = waiting(&t, id, name);
            j["created_at"] = json!(at);
            t.ctx.save(&j).unwrap();
        }
        for id in ["loop-18", "orch-old", "req-new"] {
            fs::write(
                t.ctx.job_dir(id).join("prompt.md"),
                format!("work{guidance}"),
            )
            .unwrap();
        }
        assert!(t.ctx.admit("loop-18").unwrap().is_none());
        assert_eq!(
            t.ctx.load("loop-18").unwrap()["queue_wait_reason"]["code"],
            "lowest_priority"
        );
        assert!(t.ctx.admit("orch-old").unwrap().is_none());
        assert!(t.ctx.admit("req-new").unwrap().is_some());
    }
    #[test]
    fn running_loop_yields_to_normal_waiter_before_two_hours() {
        let t = Test::new();
        config(
            &t,
            json!({"max_running":1,"lowest_priority_todo_ids":[18,23]}),
        );
        let mut parent = live_tool_parent(&t);
        parent["todo_id"] = json!(18);
        parent["started_at"] = json!(t.rt.now());
        t.ctx.save(&parent).unwrap();
        tool_events(&t, &[("item.started", "work"), ("item.completed", "work")]);
        assert!(t.ctx.admit("other").unwrap().is_some());
        assert_eq!(t.ctx.load("long").unwrap()["status"], "queued");
    }
    #[test]
    fn short_normal_job_does_not_yield_before_two_hours() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        let mut parent = live_tool_parent(&t);
        parent["started_at"] = json!(t.rt.now());
        t.ctx.save(&parent).unwrap();
        tool_events(&t, &[("item.started", "work"), ("item.completed", "work")]);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(t.ctx.load("long").unwrap()["status"], "running");
    }
    #[test]
    fn fairness_uses_the_new_tier_in_both_directions() {
        for low_parent in [false, true] {
            let t = Test::new();
            config(
                &t,
                json!({"max_running":1,"lowest_priority_todo_ids":[18,23]}),
            );
            let mut parent = live_tool_parent(&t);
            if low_parent {
                parent["todo_id"] = json!(18);
                t.ctx.save(&parent).unwrap();
            } else {
                let mut other = t.ctx.load("other").unwrap();
                other["todo_id"] = json!(18);
                t.ctx.save(&other).unwrap();
            }
            tool_events(&t, &[("item.started", "work"), ("item.completed", "work")]);
            assert_eq!(t.ctx.admit("other").unwrap().is_some(), low_parent);
            assert_eq!(
                t.ctx.load("long").unwrap()["status"] == "queued",
                low_parent
            );
        }
    }
    #[test]
    fn forty_concurrent_admissions_never_exceed_six() {
        let t = Test::new();
        for i in 0..40 {
            waiting(&t, &format!("job-{i:02}"), "ordinary");
        }
        let handles: Vec<_> = (0..40)
            .map(|i| {
                let ctx = t.ctx.clone();
                std::thread::spawn(move || ctx.admit(&format!("job-{i:02}")).unwrap().is_some())
            })
            .collect();
        let admitted = handles
            .into_iter()
            .map(|h| usize::from(h.join().unwrap()))
            .sum::<usize>();
        assert_eq!(admitted, 6);
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(
            jobs.iter().filter(|j| s(j, "status") == "running").count(),
            6
        );
        assert_eq!(
            jobs.iter().filter(|j| s(j, "status") == "queued").count(),
            34
        );
        let mut ended = t.ctx.load("job-00").unwrap();
        ended["status"] = json!("succeeded");
        t.ctx.save(&ended).unwrap();
        assert!(t.ctx.admit("job-06").unwrap().is_some());
        assert!(t.ctx.admit("job-07").unwrap().is_none());
    }
    fn child(t: &Test, id: &str, parent: &str) -> Value {
        let mut j = waiting(t, id, id);
        j["parent_job_id"] = json!(parent);
        t.ctx.save(&j).unwrap();
        j
    }
    fn cargo_wait(t: &Test, id: &str, created: u64) -> PathBuf {
        let dir = t.ctx.job_dir(id).join("resource-waits");
        fs::create_dir_all(&dir).unwrap();
        let path = dir.join(format!("cargo-{created}.json"));
        fs::write(
            &path,
            json!({"job_id":id,"kind":"cargo","pid":std::process::id(),"pid_created":created,"reason":"cargoの共有ビルド枠待ち"})
                .to_string(),
        )
        .unwrap();
        path
    }
    #[test]
    fn cargo_waiting_job_frees_its_slot_until_cargo_is_admitted() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "builder", "builder");
        assert!(t.ctx.admit("builder").unwrap().is_some());
        waiting(&t, "next", "next");
        assert!(t.ctx.admit("next").unwrap().is_none());
        let report = || {
            t.ctx
                .queue_report(&t.ctx.list_jobs(&HashSet::new()))
                .unwrap()
        };
        // A marker whose PID was reused by another process never frees the slot.
        let stale = cargo_wait(&t, "builder", 0);
        assert!(t.ctx.admit("next").unwrap().is_none());
        assert_eq!(report()["occupied_slots"], 1);
        assert_eq!(report()["queued"][0]["reason"]["code"], "running_limit");
        fs::remove_file(stale).unwrap();
        let live = cargo_wait(&t, "builder", 123);
        t.rt.proc_probe_errors
            .lock()
            .unwrap()
            .insert(std::process::id(), 5);
        assert_eq!(report()["occupied_slots"], 1);
        assert!(t.ctx.admit("next").unwrap().is_none());
        t.rt.proc_probe_errors
            .lock()
            .unwrap()
            .remove(&std::process::id());
        // Existing gate processes publish the legacy format without `kind`.
        let mut marker: Value = serde_json::from_slice(&fs::read(&live).unwrap()).unwrap();
        marker.as_object_mut().unwrap().remove("kind");
        fs::write(&live, marker.to_string()).unwrap();
        let r = report();
        assert_eq!(r["occupied_slots"], 0);
        assert_eq!(r["running"], 1);
        assert_eq!(r["queued"][0]["reason"]["code"], "ready");
        let active = live.with_file_name("active.json");
        marker["kind"] = json!("cargo-active");
        fs::write(&active, marker.to_string()).unwrap();
        assert_eq!(report()["occupied_slots"], 1);
        assert!(t.ctx.admit("next").unwrap().is_none());
        marker["pid"] = json!(321);
        marker["pid_created"] = json!(456);
        t.rt.procs.lock().unwrap().insert(321, 456);
        t.rt.proc_probe_errors.lock().unwrap().insert(321, 5);
        fs::write(&active, marker.to_string()).unwrap();
        assert_eq!(report()["occupied_slots"], 1);
        assert!(t.ctx.admit("next").unwrap().is_none());
        t.rt.proc_probe_errors.lock().unwrap().remove(&321);
        marker["pid_created"] = Value::Null;
        fs::write(&active, marker.to_string()).unwrap();
        assert_eq!(report()["occupied_slots"], 1);
        assert!(t.ctx.admit("next").unwrap().is_none());
        marker["pid_created"] = json!(456);
        for pid in [Value::Null, json!("321"), json!(0), json!(u64::MAX)] {
            marker["pid"] = pid;
            fs::write(&active, marker.to_string()).unwrap();
            assert_eq!(report()["occupied_slots"], 1);
            assert!(t.ctx.admit("next").unwrap().is_none());
        }
        marker["pid"] = json!(321);
        marker["kind"] = json!("unknown");
        fs::write(&active, marker.to_string()).unwrap();
        assert_eq!(report()["occupied_slots"], 1);
        assert!(t.ctx.admit("next").unwrap().is_none());
        marker["kind"] = json!("cargo-active");
        // A positively mismatched or absent sibling cannot occupy this job.
        marker["pid_created"] = json!(455);
        fs::write(&active, marker.to_string()).unwrap();
        assert_eq!(report()["occupied_slots"], 0);
        t.rt.procs.lock().unwrap().remove(&321);
        marker["pid_created"] = json!(456);
        fs::write(&active, marker.to_string()).unwrap();
        assert_eq!(report()["occupied_slots"], 0);
        t.rt.procs.lock().unwrap().insert(321, 456);
        marker["job_id"] = json!("different-job");
        fs::write(&active, marker.to_string()).unwrap();
        assert_eq!(report()["occupied_slots"], 0);
        fs::write(&active, "{incomplete").unwrap();
        assert_eq!(report()["occupied_slots"], 1);
        assert!(t.ctx.admit("next").unwrap().is_none());
        fs::remove_file(active).unwrap();
        // Cargo admitted: the marker is gone and the job occupies its slot again.
        fs::remove_file(&live).unwrap();
        assert!(t.ctx.admit("next").unwrap().is_none());
        assert_eq!(report()["occupied_slots"], 1);
        cargo_wait(&t, "builder", 123);
        assert!(t.ctx.admit("next").unwrap().is_some());
    }
    #[test]
    fn cargo_waiting_root_keeps_its_slot_while_a_descendant_runs() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "root", "root");
        assert!(t.ctx.admit("root").unwrap().is_some());
        child(&t, "kid", "root");
        assert!(t.ctx.admit("kid").unwrap().is_some());
        waiting(&t, "other", "other");
        cargo_wait(&t, "root", 123);
        assert!(t.ctx.admit("other").unwrap().is_none());
        assert_eq!(
            t.ctx
                .queue_report(&t.ctx.list_jobs(&HashSet::new()))
                .unwrap()["occupied_slots"],
            1
        );
        cargo_wait(&t, "kid", 123);
        assert!(t.ctx.admit("other").unwrap().is_some());
    }
    #[test]
    fn six_waiting_parents_can_all_start_dependencies_and_finish() {
        let t = Test::new();
        for i in 0..6 {
            let id = format!("parent-{i}");
            waiting(&t, &id, &id);
            assert!(t.ctx.admit(&id).unwrap().is_some());
        }
        waiting(&t, "root-extra", "req-owner");
        for i in 0..6 {
            child(&t, &format!("child-{i}"), &format!("parent-{i}"));
        }
        assert!(t.ctx.admit("root-extra").unwrap().is_none());
        for i in 0..6 {
            let id = format!("child-{i}");
            assert!(t.ctx.admit(&id).unwrap().is_some());
            let mut ended = t.ctx.load(&id).unwrap();
            ended["status"] = json!("succeeded");
            t.ctx.save(&ended).unwrap();
            let mut parent = t.ctx.load(&format!("parent-{i}")).unwrap();
            parent["status"] = json!("succeeded");
            t.ctx.save(&parent).unwrap();
        }
        assert!(t.ctx.admit("root-extra").unwrap().is_some());
    }
    #[test]
    fn children_win_priority_and_concurrent_siblings_are_limited_to_two() {
        let t = Test::new();
        waiting(&t, "parent", "parent");
        assert!(t.ctx.admit("parent").unwrap().is_some());
        waiting(&t, "root-extra", "req-owner");
        for i in 0..12 {
            child(&t, &format!("child-{i:02}"), "parent");
        }
        assert!(t.ctx.admit("root-extra").unwrap().is_none());
        let handles: Vec<_> = (0..12)
            .map(|i| {
                let ctx = t.ctx.clone();
                std::thread::spawn(move || ctx.admit(&format!("child-{i:02}")).unwrap().is_some())
            })
            .collect();
        assert_eq!(
            handles
                .into_iter()
                .map(|h| usize::from(h.join().unwrap()))
                .sum::<usize>(),
            2
        );
        assert!(t.ctx.admit("root-extra").unwrap().is_some());
        let mut ended = t.ctx.load("child-00").unwrap();
        ended["status"] = json!("succeeded");
        t.ctx.save(&ended).unwrap();
        assert!(t.ctx.admit("child-02").unwrap().is_some());
    }
    #[test]
    fn descendants_keep_one_root_slot_after_ancestors_exit() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "parent", "parent");
        assert!(t.ctx.admit("parent").unwrap().is_some());
        child(&t, "child", "parent");
        assert!(t.ctx.admit("child").unwrap().is_some());
        child(&t, "grandchild", "child");
        assert!(t.ctx.admit("grandchild").unwrap().is_some());
        for id in ["parent", "child"] {
            let mut j = t.ctx.load(id).unwrap();
            j["status"] = json!("succeeded");
            t.ctx.save(&j).unwrap();
        }
        waiting(&t, "other", "req-owner");
        assert!(t.ctx.admit("other").unwrap().is_none());
        let mut j = t.ctx.load("grandchild").unwrap();
        j["status"] = json!("succeeded");
        t.ctx.save(&j).unwrap();
        assert!(t.ctx.admit("other").unwrap().is_some());
    }
    #[test]
    fn inherited_parent_is_saved_and_invalid_or_dead_parents_are_rejected() {
        let t = Test::new();
        t.put(patch(t.running(), json!({"id":"parent"})));
        t.rt.env
            .lock()
            .unwrap()
            .insert("CC_JOB_ID".into(), "parent".into());
        assert_eq!(t.run(&[], b"review").unwrap(), 0);
        let children = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(
            children.iter().find(|j| s(j, "id") != "parent").unwrap()["parent_job_id"],
            "parent"
        );
        for parent in ["../parent", "missing"] {
            assert!(t.run(&["--parent-job-id", parent], b"review").is_err());
        }
        // A running job starting another ToDo's work is a dispatcher, not a
        // parent: that job queues as a root under the shared priority order.
        let mut parent = t.ctx.load("parent").unwrap();
        parent["todo_id"] = json!(7);
        t.ctx.save(&parent).unwrap();
        assert!(t.run(&["--todo-id", "7"], b"same ToDo").is_err());
        assert_eq!(t.run(&["--todo-id", "8"], b"other ToDo").unwrap(), 0);
        let other = t
            .ctx
            .list_jobs(&HashSet::new())
            .into_iter()
            .find(|j| j["todo_id"] == 8)
            .unwrap();
        assert!(other["parent_job_id"].is_null());
        fs::remove_dir_all(t.ctx.job_dir(s(&other, "id"))).unwrap();
        let mut parent = t.ctx.load("parent").unwrap();
        parent["status"] = json!("succeeded");
        t.ctx.save(&parent).unwrap();
        assert!(t.run(&["--parent-job-id", "parent"], b"review").is_err());
        assert_eq!(t.run(&[], b"background job").unwrap(), 0);
        assert_eq!(
            t.ctx
                .list_jobs(&HashSet::new())
                .iter()
                .filter(|j| s(j, "id") != "parent" && j["parent_job_id"].is_null())
                .count(),
            1
        );
    }
    #[test]
    fn recovery_requires_launch_evidence_and_is_idempotent() {
        let t = Test::new();
        t.put(patch(t.running(), json!({"id":"parent"})));
        let mut cancelled = waiting(&t, "cancelled-child", "review");
        cancelled["status"] = json!("cancelled");
        cancelled["pid"] = Value::Null;
        cancelled["cancel_requested"] = json!(true);
        cancelled["cancel_requested_at"] = json!(t.rt.now());
        cancelled["started_at"] = Value::Null;
        t.ctx.save(&cancelled).unwrap();
        fs::write(t.ctx.job_dir("cancelled-child").join("prompt.md"), "review").unwrap();
        let mut queued = waiting(&t, "queued-child", "review-other");
        queued["pid"] = json!(444);
        queued["pid_created"] = json!(55);
        t.ctx.save(&queued).unwrap();
        t.rt.procs.lock().unwrap().insert(444, 55);
        let event = json!({"type":"item.completed","item":{"type":"command_execution","command":"codex_jobs.exe run --name review","exit_code":0,"aggregated_output":"cancelled-child\nqueued-child\n"}});
        fs::write(
            t.ctx.job_dir("parent").join("events.jsonl"),
            event.to_string(),
        )
        .unwrap();
        let since = "2026-10-01T00:00:00+09:00";
        assert_eq!(
            t.ctx
                .recover_children(since, false)
                .unwrap()
                .as_array()
                .unwrap()
                .len(),
            2
        );
        assert_eq!(
            t.ctx.load("cancelled-child").unwrap()["status"],
            "cancelled"
        );
        t.ctx.recover_children(since, true).unwrap();
        assert_eq!(
            t.ctx.load("cancelled-child").unwrap()["parent_job_id"],
            "parent"
        );
        assert_eq!(
            t.ctx.load("cancelled-child").unwrap()["cancel_requested"],
            false
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
        assert_eq!(*t.rt.kills.lock().unwrap(), [444]);
        t.ctx.recover_children(since, true).unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
    }
    #[test]
    fn broken_parent_records_and_cycles_do_not_block_unrelated_families() {
        for damage in ["missing", "corrupt", "cycle"] {
            let t = Test::new();
            config(&t, json!({"max_running":2}));
            waiting(&t, "parent", "parent");
            assert!(t.ctx.admit("parent").unwrap().is_some());
            child(&t, "child", "parent");
            assert!(t.ctx.admit("child").unwrap().is_some());
            if damage == "missing" {
                fs::remove_file(t.ctx.job_dir("parent").join("job.json")).unwrap();
            } else if damage == "corrupt" {
                fs::write(t.ctx.job_dir("parent").join("job.json"), "broken").unwrap();
            } else {
                let mut parent = t.ctx.load("parent").unwrap();
                parent["parent_job_id"] = json!("child");
                t.ctx.save(&parent).unwrap();
            }
            waiting(&t, "other", "req-owner");
            assert!(t.ctx.admit("other").unwrap().is_some(), "{damage}");
            waiting(&t, "third", "req-third");
            assert!(t.ctx.admit("third").unwrap().is_none(), "{damage}");
        }
    }
    #[test]
    fn owner_requests_precede_fifo_and_cooldown_does_not_block_ready_jobs() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "ordinary");
        waiting(&t, "z", "req-owner");
        assert!(t.ctx.admit("a").unwrap().is_none());
        assert!(t.ctx.admit("z").unwrap().is_some());
        let mut request = t.ctx.load("z").unwrap();
        request["status"] = json!("queued");
        request["retry_not_before"] = json!(t.rt.epoch() + 60.);
        t.ctx.save(&request).unwrap();
        assert!(t.ctx.admit("a").unwrap().is_some());
        assert!(t.ctx.admit("z").unwrap().is_none());
    }
    #[test]
    fn orphan_child_consumes_slot_and_reused_pid_does_not() {
        let t = Test::new();
        config(&t, json!({"max_running":1}));
        t.rt.procs.lock().unwrap().insert(888, 77);
        t.put(job(json!({"id":"orphan","status":"running","pid":999,"pid_created":22,"codex_pid":888,"codex_pid_created":77})));
        // This is pre-existing history, present when the queue bootstraps.
        // Late legacy writes are covered by the 30-second reconciliation test.
        waiting(&t, "waiting", "ordinary");
        assert!(t.ctx.admit("waiting").unwrap().is_none());
        t.rt.procs.lock().unwrap().insert(888, 78);
        assert!(t.ctx.admit("waiting").unwrap().is_some());
    }
    #[test]
    fn configuration_is_loaded_again_and_invalid_limits_are_refused() {
        let t = Test::new();
        assert_eq!(t.ctx.queue_config().unwrap().max_running, 6);
        config(&t, json!({"max_running":1}));
        waiting(&t, "a", "one");
        waiting(&t, "b", "two");
        assert!(t.ctx.admit("a").unwrap().is_some());
        assert!(t.ctx.admit("b").unwrap().is_none());
        config(&t, json!({"max_running":2}));
        assert!(t.ctx.admit("b").unwrap().is_some());
        for value in [
            json!({"max_running":0}),
            json!({"max_children_per_parent":0}),
            json!({"retry_delay_seconds":0}),
            json!({"retry_delay_seconds":1000}),
            json!({"max_running":"six"}),
            json!({"max_runing":2}),
        ] {
            config(&t, value);
            assert!(t.ctx.queue_config().is_err());
        }
    }
    #[test]
    fn retry_preserves_session_clears_completion_and_stops_at_limit() {
        let t = Test::new();
        let cfg = QueueConfig::default();
        let mut j = job(
            json!({"status":"failed","last_error":"429 Too Many Requests","thread_id":SESSION_ID,"turn_completed":true}),
        );
        for (count, delay) in [(1, 60.), (2, 120.), (3, 240.), (4, 480.), (5, 900.)] {
            j["status"] = json!("failed");
            j["last_error"] = json!("429 Too Many Requests");
            assert!(t.ctx.retry_429(&mut j, &cfg));
            assert_eq!(j["retry_count"], count);
            assert_eq!(j["retry_not_before"], t.rt.epoch() + delay);
            assert_eq!(j["resume_session"], SESSION_ID);
            assert!(j["turn_completed"].is_null());
            assert_eq!(j["status"], "queued");
            assert_eq!(display_state(&j), "順番待ち");
        }
        j["status"] = json!("failed");
        j["last_error"] = json!("429 Too Many Requests");
        assert!(!t.ctx.retry_429(&mut j, &cfg));
        for p in [
            json!({"status":"succeeded"}),
            json!({"cancel_requested":true}),
            json!({"last_error":"502 Bad Gateway"}),
            json!({"thread_id":null,"resume_session":null}),
        ] {
            let mut j = patch(
                job(
                    json!({"status":"failed","last_error":"429 Too Many Requests","thread_id":SESSION_ID}),
                ),
                p,
            );
            assert!(!t.ctx.retry_429(&mut j, &cfg));
        }
    }
    #[test]
    fn supervisor_retries_same_session_then_succeeds() {
        let t = Test::new();
        config(&t, json!({"retry_delay_seconds":1}));
        t.rt.advance_epoch.store(true, Ordering::Relaxed);
        let j = waiting(&t, "retry", "req-retry");
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "continue").unwrap();
        let seq = AtomicUsize::new(0);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
            let text = if seq.fetch_add(1, Ordering::Relaxed) == 0 {
                format!(
                    "{{\"type\":\"thread.started\",\"thread_id\":\"{SESSION_ID}\"}}\n{{\"type\":\"turn.failed\",\"error\":{{\"message\":\"429 Too Many Requests\"}}}}\n"
                )
            } else {
                OK.into()
            };
            Box::new(FakeChild {
                alive: Arc::new(AtomicBool::new(false)),
                out: Some(Box::new(Cursor::new(text.into_bytes()))),
            })
        }));
        t.ctx.supervise("retry").unwrap();
        let saved = t.ctx.load("retry").unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["retry_count"], 1);
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 2);
        let args = t.rt.spawn_args.lock().unwrap();
        assert!(!args[0].iter().any(|a| a == "resume"));
        assert!(args[1].windows(2).any(|w| w == ["resume", SESSION_ID]));
        assert!(t.rt.sleeps.lock().unwrap().contains(&3.));
    }
    fn failed(t: &Test, id: &str, todo: u64, created: &str) -> Value {
        let j=t.put(job(json!({"id":id,"name":id,"todo_id":todo,"created_at":created,"status":"failed","finished_at":"2026-10-01T12:01:00+09:00","thread_id":SESSION_ID,"cwd":t.ctx.root,"codex":"codex.CMD"})));
        fs::write(t.ctx.job_dir(id).join("prompt.md"), "resume work").unwrap();
        fs::write(t.ctx.job_dir(id).join("events.jsonl"),"{\"type\":\"turn.failed\",\"error\":{\"message\":\"429 Too Many Requests\"},\"_ts\":\"2026-10-01T12:01:00+09:00\"}\n").unwrap();
        j
    }
    #[test]
    fn recovery_requeues_only_latest_unowned_failure_and_is_idempotent() {
        let t = Test::new();
        let a = failed(&t, "a", 1, "2026-10-01T11:59:00+09:00");
        failed(&t, "b", 1, "2026-10-01T12:00:00+09:00");
        let todo = t.ctx.root.join("todo.json");
        fs::write(
            &todo,
            json!({"items":[{"id":1,"status":"doing","jobs":["a","b"]}]}).to_string(),
        )
        .unwrap();
        let before = fs::read(t.ctx.job_dir("b").join("job.json")).unwrap();
        let plan = t
            .ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, false)
            .unwrap();
        assert_eq!(plan[0]["id"], "b");
        assert_eq!(plan[0]["action"], "would_queue");
        assert_eq!(plan[1]["action"], "skip");
        assert_eq!(
            fs::read(t.ctx.job_dir("b").join("job.json")).unwrap(),
            before
        );
        let report = t
            .ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
            .unwrap();
        assert_eq!(report[0]["action"], "queued");
        assert_eq!(t.ctx.load("b").unwrap()["resume_session"], SESSION_ID);
        assert_eq!(t.ctx.load("a").unwrap(), a);
        assert!(t.ctx.supervisor_alive(&t.ctx.load("b").unwrap()));
        t.ctx
            .recover_429("2026-10-01T12:00:00+09:00", &todo, true)
            .unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn recovery_excludes_completed_todos_successors_and_non_final_429() {
        for case in [
            "done",
            "block",
            "successor",
            "non_final",
            "before_cutoff",
            "cancelled",
        ] {
            let t = Test::new();
            let mut j = failed(&t, "failure", 1, "2026-10-01T12:00:00+09:00");
            let todo = t.ctx.root.join("todo.json");
            fs::write(&todo,json!({"items":[{"id":1,"status":if ["done","block"].contains(&case){case}else{"doing"},"jobs":["failure"]}]}).to_string()).unwrap();
            if case == "successor" {
                t.put(job(json!({"id":"successor","todo_id":1,"status":"succeeded","created_at":"2026-10-01T12:00:01+09:00"})));
            }
            if case == "non_final" {
                fs::write(
                    t.ctx.job_dir("failure").join("events.jsonl"),
                    "{\"type\":\"turn.completed\"}\n",
                )
                .unwrap();
            }
            if case == "cancelled" {
                j["cancel_requested"] = json!(true);
                t.ctx.save(&j).unwrap();
            }
            let cutoff = if case == "before_cutoff" {
                "2026-10-01T12:02:00+09:00"
            } else {
                "2026-10-01T12:00:00+09:00"
            };
            let report = t.ctx.recover_429(cutoff, &todo, true).unwrap();
            assert!(
                !report
                    .as_array()
                    .unwrap()
                    .iter()
                    .any(|row| row["action"] == "queued"),
                "{case}"
            );
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        }
    }
    #[test]
    fn different_sessions_cannot_duplicate_todo_owner() {
        let t = Test::new();
        waiting(&t, "existing", "owner");
        let mut j = t.ctx.load("existing").unwrap();
        j["todo_id"] = json!(77);
        t.ctx.save(&j).unwrap();
        // Reconciled legacy owners are rejected during intake. Owners not yet
        // discovered are checked again before any actual execution admission.
        drop(t.ctx.queue_lock().unwrap());
        fs::write(t.ctx.root.join("p.md"), "work").unwrap();
        let p = t.ctx.root.join("p.md").to_string_lossy().into_owned();
        let (result, _, _) = t.cli(&[
            "run",
            "--name",
            "other",
            "--model",
            "gpt-6.1-sol",
            "--effort",
            "high",
            "--cwd",
            t.ctx.root.to_str().unwrap(),
            "--prompt-file",
            &p,
            "--todo-id",
            "77",
        ]);
        assert!(
            result
                .unwrap_err()
                .message
                .contains("ToDo 77 already has active job")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
}
fn events(text: &str) -> Vec<Value> {
    text.lines()
        .map(|l| serde_json::from_str(l).unwrap())
        .collect()
}
fn new_job() -> Value {
    // These jobs model independent virtual checkouts. Do not discover the
    // host's personal dotfiles repository above Windows Temp fixture paths.
    json!({"id":"20260929-000000-t","name":"t","model":"gpt-6-luna","effort":"max","sandbox":"read-only","cwd":"D:/work","work_owner":{},"created_at":"2026-10-01T12:00:00+09:00","status":"queued","pid":null,"steps":0,"tokens":{"total":0},"activity":"queued","last_error":null,"duration_ms":null})
}
#[test]
fn job_cli_defaults_preserve_each_explicit_model_and_effort_independently() {
    for (name, kind, expected) in [
        ("rev-19", "review", "gpt-6-luna"),
        ("task-review", "", "gpt-6-luna"),
        ("req-19", "implementation", "gpt-6.1-sol"),
        ("req-19", "research", "gpt-6-luna"),
    ] {
        let base = [
            "run",
            "--name",
            name,
            "--prompt-file",
            "prompt.md",
            "--task-type",
            kind,
        ];
        let defaults =
            cc_jobs::cli::parse(&base.iter().map(|s| s.to_string()).collect::<Vec<_>>()).unwrap();
        assert_eq!(defaults.required("model").unwrap(), expected);
        assert_eq!(defaults.required("effort").unwrap(), "high");
        for explicit in [
            vec!["--model", "gpt-6-astra"],
            vec!["--effort", "max"],
            vec!["--model", "gpt-6-astra", "--effort", "max"],
        ] {
            let args = base
                .iter()
                .chain(explicit.iter())
                .map(|s| s.to_string())
                .collect::<Vec<_>>();
            let parsed = cc_jobs::cli::parse(&args).unwrap();
            assert_eq!(
                parsed.required("model").unwrap(),
                if explicit.contains(&"--model") {
                    "gpt-6-astra"
                } else {
                    expected
                }
            );
            assert_eq!(
                parsed.required("effort").unwrap(),
                if explicit.contains(&"--effort") {
                    "max"
                } else {
                    "high"
                }
            );
        }
    }
}
fn patch(mut j: Value, p: Value) -> Value {
    j.as_object_mut()
        .unwrap()
        .extend(p.as_object().unwrap().clone());
    j
}
fn job(p: Value) -> Value {
    patch(new_job(), p)
}
// The second field owns the directory even though callers only use the normalized path.
struct Temp(PathBuf, #[allow(dead_code)] tempfile::TempDir);
impl Temp {
    fn new() -> Self {
        Self::in_dir(&std::env::temp_dir())
    }
    fn in_dir(parent: &Path) -> Self {
        // PIDs are reused, and an interrupted test can leave its old directory.
        // Atomically create a fresh random directory instead of reusing that fixture.
        let owner = tempfile::Builder::new()
            .prefix("cc-jobs-rust-")
            .tempdir_in(parent)
            .unwrap();
        // Windows temp_dir may contain an 8.3 username. Child processes report
        // the long spelling; normalize the shared fixture before making paths.
        let path = cc_jobs::cli::normalize_windows_path(&owner.path().canonicalize().unwrap());
        Self(path, owner)
    }
}

#[test]
fn temporary_fixture_never_reuses_a_stale_pid_directory() {
    let root = tempfile::tempdir().unwrap();
    let stale = root
        .path()
        .join(format!("cc-jobs-rust-{}-0", std::process::id()));
    fs::create_dir_all(stale.join("title-tools")).unwrap();
    fs::write(stale.join("sentinel"), b"previous fixture").unwrap();
    let first = Temp::in_dir(root.path());
    let second = Temp::in_dir(root.path());
    assert_ne!(first.0, second.0);
    assert_ne!(
        first.0,
        cc_jobs::cli::normalize_windows_path(&stale.canonicalize().unwrap())
    );
    assert_eq!(fs::read_dir(&first.0).unwrap().count(), 0);
    let first_path = first.0.clone();
    drop(first);
    assert!(!first_path.exists());
    assert!(second.0.is_dir());
    assert_eq!(
        fs::read(stale.join("sentinel")).unwrap(),
        b"previous fixture"
    );
}
type CallFn = Box<dyn Fn(&Call) -> io::Result<Reply> + Send + Sync>;
type LoadFn = Box<dyn Fn(&Path, usize) + Send + Sync>;
type SpawnHook = Box<dyn Fn() -> Box<dyn Running> + Send + Sync>;
type SleepHook = Box<dyn Fn(f64) + Send + Sync>;
type KillHook = Box<dyn Fn(u32) + Send + Sync>;

#[test]
fn saved_discord_settings_are_given_only_to_the_notifier_and_inherited_values_win() {
    let saved = BTreeMap::from([
        ("GODD_DISCORD_BOT_TOKEN".into(), "fake-saved-token".into()),
        ("GODD_DISCORD_TASK_CHANNEL_ID".into(), "forum".into()),
        ("OPENAI_API_KEY".into(), "excluded".into()),
    ]);
    let args = ["godd-ao.exe", "notify", "task", "start"].map(str::to_string);
    let injected = notifier_overrides(&args, &BTreeMap::new(), &saved);
    assert_eq!(injected.len(), 2);
    assert!(!injected.contains_key("OPENAI_API_KEY"));
    let inherited = BTreeMap::from([(
        "godd_discord_bot_token".into(),
        "fake-inherited-token".into(),
    )]);
    assert!(!notifier_overrides(&args, &inherited, &saved).contains_key("GODD_DISCORD_BOT_TOKEN"));
    assert!(
        notifier_overrides(
            &["codex.exe".into(), "exec".into()],
            &BTreeMap::new(),
            &saved
        )
        .is_empty()
    );
    assert!(
        notifier_overrides(
            &["codex.exe".into(), "notify".into(), "task".into()],
            &BTreeMap::new(),
            &saved
        )
        .is_empty()
    );
}
struct Fake {
    epoch_base: Mutex<Option<f64>>,
    quota_samples: Mutex<HashMap<String, cc_jobs::adaptive::QuotaSnapshot>>,
    boot: Mutex<Option<f64>>,
    recovery_guards: Mutex<HashMap<String, Option<bool>>>,
    summary_calls: AtomicUsize,
    summary_fail: AtomicBool,
    queue_sample: Mutex<cc_jobs::adaptive::MachineSample>,
    queue_samples: AtomicUsize,
    title_tools: Mutex<Option<PathBuf>>,
    env: Mutex<BTreeMap<String, String>>,
    time: Mutex<f64>,
    calls: Mutex<Vec<Call>>,
    replies: Mutex<VecDeque<io::Result<Reply>>>,
    handler: Mutex<Option<CallFn>>,
    sleeps: Mutex<Vec<f64>>,
    procs: Arc<Mutex<HashMap<u32, u64>>>,
    proc_probe_errors: Mutex<HashMap<u32, i32>>,
    kill_works: AtomicBool,
    kill_hook: Mutex<Option<KillHook>>,
    kills: Arc<Mutex<Vec<u32>>>,
    pause_allowed: AtomicBool,
    pause_calls: AtomicUsize,
    paused_stop_failure: Arc<AtomicBool>,
    paused_stop_failure_pid: Arc<AtomicU32>,
    pause_recoveries: Mutex<Vec<String>>,
    fail_queued_save: AtomicBool,
    pause_hook: Mutex<Option<Box<dyn Fn() + Send + Sync>>>,
    spawns: AtomicUsize,
    supervisors: AtomicUsize,
    spawn_live: AtomicBool,
    child_alive: Arc<AtomicBool>,
    load_count: AtomicUsize,
    load_hook: Mutex<Option<LoadFn>>,
    save_hook: Mutex<Option<LoadFn>>,
    load_error_path: Arc<Mutex<Option<PathBuf>>>,
    save_failure: AtomicBool,
    replace_failures: AtomicUsize,
    replace_count: AtomicUsize,
    fail_stdout: AtomicBool,
    diagnostics: Mutex<Vec<String>>,
    load_failures: AtomicUsize,
    replace_os: AtomicI32,
    spawn_hook: Mutex<Option<SpawnHook>>,
    spawn_args: Mutex<Vec<Vec<String>>>,
    advance_epoch: AtomicBool,
    sleep_hook: Mutex<Option<SleepHook>>,
    supervisor_pid: AtomicU32,
    supervisor_fail: AtomicBool,
    process_tree_job_supported: AtomicBool,
}
impl Fake {
    fn new() -> Self {
        Self {
            epoch_base: Mutex::new(None),
            quota_samples: Mutex::new(HashMap::new()),
            boot: Mutex::new(None),
            recovery_guards: Mutex::new(HashMap::new()),
            summary_calls: AtomicUsize::new(0),
            summary_fail: AtomicBool::new(false),
            queue_sample: Mutex::new(cc_jobs::adaptive::MachineSample {
                free_memory_gib: Some(20.),
                cpu_times: Some([100, 200, 100]),
                health_ms: Some(10.),
            }),
            queue_samples: AtomicUsize::new(0),
            title_tools: Mutex::new(None),
            env: Mutex::new(BTreeMap::from([(
                "GODD_AO_BIN".into(),
                "Z:/missing-ao.exe".into(),
            )])),
            time: Mutex::new(100.),
            calls: Mutex::new(vec![]),
            replies: Mutex::new(VecDeque::new()),
            handler: Mutex::new(None),
            sleeps: Mutex::new(vec![]),
            procs: Arc::new(Mutex::new(HashMap::from([(std::process::id(), 123)]))),
            proc_probe_errors: Mutex::new(HashMap::new()),
            kill_works: AtomicBool::new(true),
            kill_hook: Mutex::new(None),
            kills: Arc::new(Mutex::new(vec![])),
            pause_allowed: AtomicBool::new(false),
            pause_calls: AtomicUsize::new(0),
            paused_stop_failure: Arc::new(AtomicBool::new(false)),
            paused_stop_failure_pid: Arc::new(AtomicU32::new(0)),
            pause_recoveries: Mutex::new(Vec::new()),
            fail_queued_save: AtomicBool::new(false),
            pause_hook: Mutex::new(None),
            spawns: AtomicUsize::new(0),
            supervisors: AtomicUsize::new(0),
            spawn_live: AtomicBool::new(false),
            child_alive: Arc::new(AtomicBool::new(false)),
            load_count: AtomicUsize::new(0),
            load_hook: Mutex::new(None),
            save_hook: Mutex::new(None),
            load_error_path: Arc::new(Mutex::new(None)),
            save_failure: AtomicBool::new(false),
            replace_failures: AtomicUsize::new(0),
            replace_count: AtomicUsize::new(0),
            fail_stdout: AtomicBool::new(false),
            diagnostics: Mutex::new(vec![]),
            load_failures: AtomicUsize::new(0),
            replace_os: AtomicI32::new(0),
            spawn_hook: Mutex::new(None),
            spawn_args: Mutex::new(vec![]),
            advance_epoch: AtomicBool::new(false),
            sleep_hook: Mutex::new(None),
            supervisor_pid: AtomicU32::new(0),
            supervisor_fail: AtomicBool::new(false),
            process_tree_job_supported: AtomicBool::new(false),
        }
    }
    fn tick(&self, n: f64) {
        *self.time.lock().unwrap() += n;
    }
    fn reply(&self, ok: bool) {
        self.replies.lock().unwrap().push_back(Ok(Reply {
            code: 0,
            stdout: json!({"ok":ok}).to_string().into_bytes(),
            stderr: vec![],
        }));
    }
    fn with_handler(&self, f: impl Fn(&Call) -> io::Result<Reply> + Send + Sync + 'static) {
        *self.handler.lock().unwrap() = Some(Box::new(f));
    }
}
struct FakeChild {
    alive: Arc<AtomicBool>,
    out: Option<Box<dyn Read + Send>>,
}
impl Running for FakeChild {
    fn pid(&self) -> u32 {
        321
    }
    fn stdout(&mut self) -> Option<Box<dyn Read + Send>> {
        self.out.take()
    }
    fn stderr(&mut self) -> Option<Box<dyn Read + Send>> {
        None
    }
    fn poll(&mut self) -> io::Result<Option<i64>> {
        Ok(if self.alive.load(Ordering::Relaxed) {
            None
        } else {
            Some(0)
        })
    }
    fn kill(&mut self) -> io::Result<()> {
        self.alive.store(false, Ordering::Relaxed);
        Ok(())
    }
}
struct BadReader;
impl Read for BadReader {
    fn read(&mut self, _b: &mut [u8]) -> io::Result<usize> {
        Err(io::Error::other("disk full"))
    }
}
impl Runtime for Fake {
    fn supports_process_tree_job(&self) -> bool {
        self.process_tree_job_supported.load(Ordering::Relaxed)
    }
    fn kill_process_tree_job(&self, pid: u32, _: u64, _: Option<(u32, u64)>) -> io::Result<bool> {
        if !self.supports_process_tree_job() {
            return Ok(false);
        }
        if let Some(hook) = self.kill_hook.lock().unwrap().as_ref() {
            hook(pid);
        }
        self.kills.lock().unwrap().push(pid);
        Ok(true)
    }
    fn boot_epoch(&self) -> Option<f64> {
        *self.boot.lock().unwrap()
    }
    fn recovery_protected(&self, cwd: &str) -> io::Result<bool> {
        match self.recovery_guards.lock().unwrap().get(cwd) {
            Some(None) => Err(io::Error::new(
                io::ErrorKind::TimedOut,
                "fixture guard timeout",
            )),
            Some(Some(protected)) => Ok(*protected),
            None => Ok(false),
        }
    }
    fn work_checkout(&self, cwd: &str) -> Value {
        cc_job_policy::work_owner::checkout(cwd)
    }
    fn queue_machine_sample(
        &self,
        _: &cc_jobs::adaptive::AdaptiveConfig,
    ) -> cc_jobs::adaptive::MachineSample {
        self.queue_samples.fetch_add(1, Ordering::Relaxed);
        self.queue_sample.lock().unwrap().clone()
    }
    fn queue_quota_sample(
        &self,
        provider: &str,
        at: f64,
        _: u64,
    ) -> cc_jobs::adaptive::QuotaSnapshot {
        if let Some(q) = self.quota_samples.lock().unwrap().get(provider) {
            return q.clone();
        }
        cc_jobs::adaptive::parse_quotas(
            &json!({"accounts":[{"id":"a","quota":{"weeklyPercent":20.,"weeklyResetAt":at+86400.}}]}),
            at,
        )
    }
    fn task_title_tools(&self) -> PathBuf {
        self.title_tools
            .lock()
            .unwrap()
            .clone()
            .unwrap_or_else(|| PathBuf::from(cc_task_title::TOOLS))
    }
    fn task_job_title(&self, job: &Value, prompt: &str) -> io::Result<String> {
        let names =
            cc_task_title::Names::load(&self.task_title_tools().join("client-projects.txt"))?;
        // No installed cache or loopback model is used by offline tests.
        Ok(cc_task_title::job_title(job, prompt, &Value::Null, &names))
    }
    fn summary_mcp_servers(&self, _codex: &str, _cwd: &Path) -> io::Result<Vec<String>> {
        self.summary_calls.fetch_add(1, Ordering::Relaxed);
        if let Some(tools) = self.title_tools.lock().unwrap().as_ref() {
            let gate = tools.parent().unwrap().join("jobs/.queue.lock");
            if gate.parent().unwrap().is_dir() {
                drop(FileLock::try_acquire(&gate).expect("MCP inventory monopolized queue gate"));
            }
        }
        if self.summary_fail.load(Ordering::Relaxed) {
            return Err(io::Error::other("inventory unavailable"));
        }
        Ok(vec!["stripe".into(), "unseen-server".into()])
    }
    fn recover_paused(&self, path: &Path) -> io::Result<()> {
        self.pause_recoveries.lock().unwrap().push(
            path.parent()
                .unwrap()
                .file_name()
                .unwrap()
                .to_string_lossy()
                .into(),
        );
        System::default().recover_paused(path)
    }
    fn pause_idle_agent(
        &self,
        pid: u32,
        _: u64,
        _: Option<u64>,
        _: Option<&Path>,
    ) -> io::Result<Option<Box<dyn Paused>>> {
        self.pause_calls.fetch_add(1, Ordering::Relaxed);
        if !self.pause_allowed.load(Ordering::Relaxed) {
            return Ok(None);
        }
        if let Some(hook) = self.pause_hook.lock().unwrap().as_ref() {
            hook();
        }
        self.pause_supervisor(pid, 0).map(Some)
    }
    fn pause_supervisor(&self, pid: u32, _: u64) -> io::Result<Box<dyn Paused>> {
        struct Guard {
            pid: u32,
            procs: Arc<Mutex<HashMap<u32, u64>>>,
            kills: Arc<Mutex<Vec<u32>>>,
            alive: Arc<AtomicBool>,
            fail: Arc<AtomicBool>,
            fail_pid: Arc<AtomicU32>,
        }
        impl Paused for Guard {
            fn keep_paused(&mut self) {}
            fn arm_process(&mut self, _: u32, _: u64) -> io::Result<()> {
                Ok(())
            }
            fn processes(&self) -> Vec<(u32, u64)> {
                vec![(self.pid, self.procs.lock().unwrap()[&self.pid])]
            }
            fn stop(&mut self) -> io::Result<()> {
                if (self.fail_pid.load(Ordering::Relaxed) == 0
                    || self.fail_pid.load(Ordering::Relaxed) == self.pid)
                    && self.fail.swap(false, Ordering::Relaxed)
                {
                    return Err(io::Error::other("fixture stop failed"));
                }
                self.kills.lock().unwrap().push(self.pid);
                self.procs.lock().unwrap().remove(&self.pid);
                if self.pid == 321 {
                    self.alive.store(false, Ordering::Relaxed);
                }
                Ok(())
            }
        }
        Ok(Box::new(Guard {
            pid,
            procs: self.procs.clone(),
            kills: self.kills.clone(),
            alive: self.child_alive.clone(),
            fail: self.paused_stop_failure.clone(),
            fail_pid: self.paused_stop_failure_pid.clone(),
        }))
    }
    fn diagnostic(&self, message: &str) {
        self.diagnostics.lock().unwrap().push(message.into());
    }
    fn env(&self) -> BTreeMap<String, String> {
        self.env.lock().unwrap().clone()
    }
    fn now(&self) -> String {
        if let Some(at) = *self.epoch_base.lock().unwrap() {
            return chrono::DateTime::from_timestamp(at as i64, 0)
                .unwrap()
                .to_rfc3339();
        }
        "2026-10-01T12:00:00+09:00".into()
    }
    fn mono(&self) -> f64 {
        *self.time.lock().unwrap()
    }
    fn epoch(&self) -> f64 {
        let base = *self.epoch_base.lock().unwrap();
        base.unwrap_or_else(|| parse_iso(&self.now()).unwrap())
            + if self.advance_epoch.load(Ordering::Relaxed) {
                self.mono() - 100.
            } else {
                0.
            }
    }
    fn sleep(&self, n: f64) {
        self.sleeps.lock().unwrap().push(n);
        self.tick(n);
        if let Some(hook) = self.sleep_hook.lock().unwrap().as_ref() {
            hook(n);
        }
    }
    fn cores(&self) -> usize {
        32
    }
    fn proc_created(&self, pid: u32) -> Option<u64> {
        self.procs.lock().unwrap().get(&pid).copied()
    }
    fn proc_created_checked(&self, pid: u32) -> io::Result<Option<u64>> {
        if let Some(code) = self.proc_probe_errors.lock().unwrap().get(&pid) {
            return Err(io::Error::from_raw_os_error(*code));
        }
        Ok(self.proc_created(pid))
    }
    fn which(&self, name: &str) -> Option<String> {
        match name {
            "codex.cmd" | "codex" => Some("C:/npm/codex.CMD".into()),
            "cargo.exe" => Some("C:/Users/test/.cargo/bin/cargo.exe".into()),
            _ => None,
        }
    }
    fn call(&self, c: Call) -> io::Result<Reply> {
        assert_eq!(c.flags, NO_WINDOW);
        self.calls.lock().unwrap().push(c.clone());
        if let Some(h) = self.handler.lock().unwrap().as_ref() {
            return h(&c);
        }
        if let Some(r) = self.replies.lock().unwrap().pop_front() {
            return r;
        }
        Ok(Reply {
            code: 0,
            stdout: if c.argv[0] == "docker" {
                b"[]".to_vec()
            } else {
                b"{\"ok\":true}".to_vec()
            },
            stderr: vec![],
        })
    }
    fn spawn(&self, spec: &Spawn) -> io::Result<Box<dyn Running>> {
        self.spawn_args.lock().unwrap().push(spec.argv.clone());
        assert_eq!(spec.flags, NO_WINDOW);
        self.spawns.fetch_add(1, Ordering::Relaxed);
        if let Some(h) = &*self.spawn_hook.lock().unwrap() {
            return Ok(h());
        }
        assert!(!spec.env.contains_key("GODD_DISCORD_BOT_TOKEN"));
        self.child_alive
            .store(self.spawn_live.load(Ordering::Relaxed), Ordering::Relaxed);
        Ok(Box::new(FakeChild {
            alive: self.child_alive.clone(),
            out: Some(if self.fail_stdout.load(Ordering::Relaxed) {
                Box::new(BadReader)
            } else {
                Box::new(Cursor::new(format!("{OK}\n").into_bytes()))
            }),
        }))
    }
    fn spawn_supervisor(
        &self,
        _exe: &Path,
        _root: &Path,
        _dir: &Path,
        _jid: &str,
    ) -> io::Result<bool> {
        self.supervisors.fetch_add(1, Ordering::Relaxed);
        if self.supervisor_fail.load(Ordering::Relaxed) {
            return Err(io::Error::other("fixture supervisor spawn failure"));
        }
        Ok(true)
    }
    fn spawn_supervisor_identified(
        &self,
        exe: &Path,
        root: &Path,
        dir: &Path,
        jid: &str,
    ) -> io::Result<(bool, Option<u32>)> {
        let detached = self.spawn_supervisor(exe, root, dir, jid)?;
        let pid = self.supervisor_pid.load(Ordering::Relaxed);
        Ok((detached, (pid != 0).then_some(pid)))
    }
    fn kill_tree(&self, pid: u32) -> io::Result<()> {
        if let Some(hook) = self.kill_hook.lock().unwrap().as_ref() {
            hook(pid);
        }
        self.kills.lock().unwrap().push(pid);
        if self.kill_works.load(Ordering::Relaxed) {
            self.procs.lock().unwrap().remove(&pid);
            if pid == 321 {
                self.child_alive.store(false, Ordering::Relaxed);
            }
        }
        Ok(())
    }
    fn before_load(&self, path: &Path) -> io::Result<()> {
        if self.load_error_path.lock().unwrap().as_deref() == Some(path) {
            return Err(io::Error::from_raw_os_error(32));
        }
        let n = self.load_count.fetch_add(1, Ordering::Relaxed);
        if self.load_failures.load(Ordering::Relaxed) > 0 {
            self.load_failures.fetch_sub(1, Ordering::Relaxed);
            return Err(io::Error::from_raw_os_error(32));
        }
        if let Some(h) = self.load_hook.lock().unwrap().as_ref() {
            h(path, n);
        }
        Ok(())
    }
    fn before_save(&self, _path: &Path, j: &Value) -> io::Result<()> {
        if let Some(h) = self.save_hook.lock().unwrap().as_ref() {
            h(_path, 0);
        }
        if s(j, "status") == "queued"
            && truth(&j["yield_count"])
            && self.fail_queued_save.swap(false, Ordering::Relaxed)
        {
            return Err(io::Error::other("fixture queued write failure"));
        }
        if truth(&j["codex_pid"]) && self.save_failure.swap(false, Ordering::Relaxed) {
            Err(io::Error::other("disk full"))
        } else {
            Ok(())
        }
    }
    fn replace(&self, a: &Path, b: &Path) -> io::Result<()> {
        self.replace_count.fetch_add(1, Ordering::Relaxed);
        let count = self.replace_failures.load(Ordering::Relaxed);
        if count > 0 {
            self.replace_failures.fetch_sub(1, Ordering::Relaxed);
            if self.replace_os.load(Ordering::Relaxed) != 0 {
                return Err(io::Error::from_raw_os_error(
                    self.replace_os.load(Ordering::Relaxed),
                ));
            }
            Err(io::Error::new(
                io::ErrorKind::PermissionDenied,
                "locked by a reader",
            ))
        } else {
            fs::rename(a, b)
        }
    }
}
struct Test {
    _tmp: Temp,
    ctx: Context,
    rt: Arc<Fake>,
}
impl Test {
    fn new() -> Self {
        let tmp = Temp::new();
        let rt = Arc::new(Fake::new());
        let root = tmp.0.clone();
        let title_tools = root.join("title-tools");
        fs::create_dir(&title_tools).unwrap();
        fs::write(title_tools.join("client-projects.txt"), "ExampleClient\n").unwrap();
        *rt.title_tools.lock().unwrap() = Some(title_tools);
        let ctx = Context {
            jobs: root.join("jobs"),
            home: root.join("home"),
            codex_home: root.join("codexhome"),
            gto: root.join("gto.exe"),
            executable: root.join("codex_jobs.exe"),
            root,
            rt: rt.clone(),
        };
        // Every fixture owns its future ToDo lock, even before its store exists.
        rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            ctx.root.to_string_lossy().into_owned(),
        );
        fs::write(&ctx.gto, b"").unwrap();
        fs::write(&ctx.executable, b"fake supervisor executable").unwrap();
        let policy = ctx.root.join("owner-instructions.md");
        fs::write(&policy, "fixture owner instructions v1").unwrap();
        rt.env.lock().unwrap().insert(
            "CC_OWNER_INSTRUCTIONS_PATH".into(),
            policy.to_string_lossy().into_owned(),
        );
        Self { _tmp: tmp, ctx, rt }
    }
    fn put(&self, j: Value) -> Value {
        fs::create_dir_all(self.ctx.job_dir(s(&j, "id"))).unwrap();
        let prompt = self.ctx.job_dir(s(&j, "id")).join("prompt.md");
        if !prompt.exists() {
            fs::write(prompt, "fixture task").unwrap();
        }
        self.ctx.save(&j).unwrap();
        j
    }
    fn cli(&self, args: &[&str]) -> (Result<i32>, String, String) {
        let diagnostic_start = self.rt.diagnostics.lock().unwrap().len();
        let mut out = vec![];
        let mut err = vec![];
        let result = self.ctx.dispatch(
            &args.iter().map(|s| s.to_string()).collect::<Vec<_>>(),
            &mut out,
            &mut err,
        );
        for line in &self.rt.diagnostics.lock().unwrap()[diagnostic_start..] {
            err.extend(line.as_bytes());
            err.push(b'\n');
        }
        (
            result,
            String::from_utf8(out).unwrap(),
            String::from_utf8(err).unwrap(),
        )
    }
    fn running(&self) -> Value {
        job(json!({"status":"running","pid":std::process::id(),"pid_created":123}))
    }
    fn run(&self, extra: &[&str], bytes: &[u8]) -> Result<i32> {
        let p = self.ctx.root.join("prompt.md");
        fs::write(&p, bytes).unwrap();
        let mut args = vec![
            "run",
            "--name",
            "x",
            "--model",
            "m",
            "--effort",
            "e",
            "--cwd",
            self.ctx.root.to_str().unwrap(),
            "--prompt-file",
            p.to_str().unwrap(),
        ];
        args.extend(extra);
        self.cli(&args).0
    }
    fn rate(&self, extra: &[&str]) -> Result<i32> {
        let mut args = vec!["rate", "20260929-000000-t", "--quality", "3"];
        args.extend(extra);
        self.cli(&args).0
    }
}
mod event_parsing {
    use super::*;
    #[test]
    fn test_success_run() {
        let mut j = job(json!({"status":"running"}));
        for e in events(OK) {
            apply_event(&mut j, &e);
        }
        assert_eq!(j["thread_id"], "01a0e883-7628-7121-9540-6780fd4cd6f4");
        assert_eq!(j["steps"], 2);
        assert_eq!(j["last_message"], "DONE");
        assert_eq!(j["turn_completed"], true);
        finish(&mut j, Some(0), 23000, "now");
        assert_eq!(j["status"], "succeeded");
        assert_eq!(j["activity"], "done: DONE");
    }
    #[test]
    fn test_activity_follows_commands() {
        let mut j = new_job();
        let ev = events(OK);
        apply_event(&mut j, &ev[3]);
        assert_eq!(
            j["activity"],
            "running: Get-ChildItem -File | Select-Object -ExpandProperty Name"
        );
        assert_eq!(
            j["activity_detail"],
            "Get-ChildItem -File | Select-Object -ExpandProperty Name"
        );
        apply_event(&mut j, &ev[4]);
        assert_eq!(j["activity"], "thinking");
        assert!(j["activity_detail"].is_null());
    }
    #[test]
    fn test_completed_tool_clears_detail_for_every_tool_kind() {
        for kind in TOOL_KINDS {
            let mut j = job(json!({"activity":"mcp: old","activity_detail":"same detail"}));
            apply_event(
                &mut j,
                &json!({"type":"item.completed","item":{"type":kind,"server":"private","tool":"lookup","command":"same detail","query":"same detail","changes":[{"path":"same detail"}]}}),
            );
            assert_eq!(j["activity"], "thinking", "{kind}");
            assert!(j["activity_detail"].is_null());
        }
    }
    #[test]
    fn test_failed_run() {
        let mut j = new_job();
        for ev in events(FAIL) {
            apply_event(&mut j, &ev);
        }
        assert!(s(&j, "last_error").contains("http://127.0.0.1:10101/v1/responses"));
        assert!(!s(&j, "last_error").contains("Reconnecting"));
        finish(&mut j, Some(1), 5000, "now");
        assert_eq!(j["status"], "failed");
        assert!(s(&j, "activity").starts_with("failed: "));
    }
    #[test]
    fn test_exit_zero_without_turn_completed_is_failure() {
        let mut j = new_job();
        finish(&mut j, Some(0), 10, "now");
        assert_eq!(j["status"], "failed");
    }
    #[test]
    fn test_describe_lines() {
        let l = events(OK).iter().map(describe).collect::<Vec<_>>();
        assert_eq!(
            l[0].as_deref(),
            Some("session 01a0e883-7628-7121-9540-6780fd4cd6f4")
        );
        assert!(l[1].is_none());
        assert_eq!(
            l[3].as_deref(),
            Some("$ Get-ChildItem -File | Select-Object -ExpandProperty Name")
        );
        assert_eq!(
            l[4].as_deref(),
            Some("  -> exit 0, 2 lines: Get-ChildItem -File | Select-Object -ExpandProperty Name")
        );
        assert_eq!(l[7].as_deref(), Some("> DONE"));
        assert_eq!(
            l[8].as_deref(),
            Some("turn completed: 105,710 tokens (in 105,429, cached 76,032, out 281)")
        );
        let l = events(FAIL).iter().map(describe).collect::<Vec<_>>();
        assert!(
            l[1].as_ref()
                .unwrap()
                .starts_with("warning: Model metadata")
        );
        assert!(
            l[3].as_ref()
                .unwrap()
                .starts_with("error: Reconnecting... 1/5")
        );
        assert!(
            l[4].as_ref()
                .unwrap()
                .starts_with("FAILED: unexpected status 404")
        );
    }
    #[test]
    fn test_describe_other_item_kinds() {
        let fc = json!({"type":"item.completed","item":{"type":"file_change","changes":[{"path":"a.py","kind":"update"}]}});
        let todo = json!({"type":"item.updated","item":{"type":"todo_list","items":[{"text":"x","completed":true},{"text":"y","completed":false}]}});
        assert_eq!(describe(&fc).unwrap(), "edit: update a.py");
        assert_eq!(describe(&todo).unwrap(), "plan: [x] x | [ ] y");
        assert_eq!(activity(s(&fc, "type"), &fc["item"]).unwrap(), "thinking");
        assert_eq!(
            activity(s(&todo, "type"), &todo["item"]).unwrap(),
            "plan 1/2"
        );
        assert!(describe(&json!({"type":"item.updated","item":{"type":"unknown"}})).is_none());
    }
    #[test]
    fn test_odd_event_shapes_do_not_break_tracking() {
        let ev = json!({"type":"turn.failed","error":"plain string"});
        let mut j = new_job();
        apply_event(&mut j, &ev);
        assert_eq!(j["last_error"], "plain string");
        assert_eq!(describe(&ev).unwrap(), "FAILED: plain string");
        let raws = [
            br#"{"type":"item.completed","item":"not a dict"}"#.as_slice(),
            b"{broken",
            b"[]",
            b"",
            br#"{"type":"thread.started","thread_id":"x"}"#,
        ];
        assert_eq!(event_lines(raws), ["session x"]);
    }
    #[test]
    fn test_event_lines_show_arrival_time() {
        assert_eq!(
            event_lines([
                br#"{"_ts":"2026-09-29T00:08:19+09:00","type":"thread.started","thread_id":"x"}"#
                    .as_slice()
            ]),
            ["00:08:19 session x"]
        );
    }
    #[test]
    fn test_clean_cmd_undoes_posix_quoting() {
        let p = r#""C:\\Program Files\\PowerShell\\7\\pwsh.exe" -NoProfile -Command "#;
        for (src, want) in [
            (
                r#"'$i=0; gc a.py | % { '"'{0,4}: {1}' -f (++"'$i), $_ }'"#,
                "$i=0; gc a.py | % { '{0,4}: {1}' -f (++$i), $_ }",
            ),
            (r#""rg --files -g '*.py'""#, "rg --files -g '*.py'"),
            (
                r#""python -B -c \"print(1)\"""#,
                r#"python -B -c "print(1)""#,
            ),
            ("'unbalanced", "'unbalanced"),
        ] {
            assert_eq!(clean_cmd(&format!("{p}{src}")), want);
        }
        assert_eq!(clean_cmd("ls -la"), "ls -la");
    }
}

mod registrar_launch_policy {
    use super::*;
    #[test]
    fn old_saved_and_continuation_inputs_receive_current_rule_without_changing_originals_or_scope()
    {
        for sandbox in ["read-only", "danger-full-access"] {
            for (phase, yielded, source) in [
                ("", false, "prompt.md"),
                ("continue", false, "context-continue.md"),
                ("continue", true, "yield-resume.md"),
                ("summarize", true, "context-summary-prompt.md"),
            ] {
                let t = Test::new();
                let j = t.put(job(json!({"id":"old-job","sandbox":sandbox,"context_phase":phase,"yield_resume":yielded})));
                let original = t.ctx.job_dir("old-job").join(source);
                fs::write(&original, "old task\r\noriginal scope").unwrap();
                let path = t.ctx.prompt_for_attempt(&j).unwrap();
                let fed = fs::read_to_string(&path).unwrap();
                assert!(fed.starts_with("old task\r\noriginal scope"));
                assert!(fed.contains(cc_job_policy::REGISTRAR_GUIDANCE));
                assert_eq!(fed.matches(cc_job_policy::REGISTRAR_GUIDANCE).count(), 1);
                assert_eq!(
                    fs::read_to_string(&original).unwrap(),
                    "old task\r\noriginal scope"
                );
                assert_eq!(j["sandbox"], sandbox);
                assert_eq!(t.ctx.prompt_for_attempt(&j).unwrap(), path);
                assert_eq!(fs::read_to_string(path).unwrap(), fed);
                fs::write(&original, &fed).unwrap();
                let appended = fs::read_to_string(t.ctx.prompt_for_attempt(&j).unwrap()).unwrap();
                assert!(appended.starts_with(&fed));
                assert_eq!(
                    appended.matches(cc_job_policy::REGISTRAR_GUIDANCE).count(),
                    2
                );
                assert_eq!(fs::read_to_string(original).unwrap(), fed);
            }
        }
    }
    #[test]
    fn missing_or_invalid_input_cannot_be_replaced_with_a_policy_only_launch() {
        let t = Test::new();
        let j = t.put(job(json!({"id":"old-job","context_phase":"continue"})));
        assert!(t.ctx.prompt_for_attempt(&j).is_err());
        fs::write(t.ctx.job_dir("old-job").join("context-continue.md"), [0xff]).unwrap();
        assert!(t.ctx.prompt_for_attempt(&j).is_err());
        assert!(
            !t.ctx
                .job_dir("old-job")
                .join("execution-prompt.md")
                .exists()
        );
    }
    #[test]
    fn a_quoted_rule_in_the_middle_cannot_suppress_the_final_restriction() {
        let t = Test::new();
        let j = t.put(job(json!({"id":"quoted-job"})));
        let path = t.ctx.job_dir("quoted-job").join("prompt.md");
        let original = format!(
            "quoted log:\n{}\nThis quote is not a site authorization.",
            cc_job_policy::REGISTRAR_GUIDANCE
        );
        fs::write(&path, &original).unwrap();
        let fed = fs::read_to_string(t.ctx.prompt_for_attempt(&j).unwrap()).unwrap();
        assert_eq!(fs::read_to_string(path).unwrap(), original);
        assert!(fed.trim_end().ends_with(cc_job_policy::REGISTRAR_GUIDANCE));
        assert_eq!(fed.matches(cc_job_policy::REGISTRAR_GUIDANCE).count(), 2);
    }
    #[test]
    fn even_a_rule_quoted_at_the_end_receives_a_separate_execution_rule() {
        let t = Test::new();
        let j = t.put(job(json!({"id":"quoted-end-job"})));
        let path = t.ctx.job_dir("quoted-end-job").join("prompt.md");
        let original = format!("quoted log:\n{}", cc_job_policy::REGISTRAR_GUIDANCE);
        fs::write(&path, &original).unwrap();
        let fed = fs::read_to_string(t.ctx.prompt_for_attempt(&j).unwrap()).unwrap();
        assert_eq!(fs::read_to_string(path).unwrap(), original);
        assert!(fed.starts_with(&original));
        assert!(fed.contains("\n\n## 実行時のサイト制限\n"));
        assert_eq!(fed.matches(cc_job_policy::REGISTRAR_GUIDANCE).count(), 2);
    }
}
mod saved_owner_launch_policy {
    use super::*;
    #[test]
    fn every_attempt_loads_latest_policy_and_keeps_source_and_read_only_scope() {
        for (phase, yielded, source) in [
            ("", false, "prompt.md"),
            ("continue", false, "context-continue.md"),
            ("continue", true, "yield-resume.md"),
            ("summarize", true, "context-summary-prompt.md"),
        ] {
            let t = Test::new();
            let j = t.put(job(json!({"id":"saved-owner", "sandbox":"read-only", "context_phase":phase,"yield_resume":yielded})));
            let original = t.ctx.job_dir("saved-owner").join(source);
            fs::write(&original, "original limited task").unwrap();
            let policy = t.ctx.root.join("owner-instructions.md");
            let first = t.ctx.prompt_for_attempt(&j).unwrap();
            assert!(
                fs::read_to_string(&first)
                    .unwrap()
                    .contains("fixture owner instructions v1")
            );
            fs::write(&policy, "updated owner instructions v2").unwrap();
            let next = fs::read_to_string(t.ctx.prompt_for_attempt(&j).unwrap()).unwrap();
            assert!(next.contains("updated owner instructions v2"));
            assert!(!next.contains("fixture owner instructions v1"));
            assert!(next.starts_with("original limited task"));
            assert!(next.trim_end().ends_with(cc_job_policy::REGISTRAR_GUIDANCE));
            assert_eq!(
                fs::read_to_string(original).unwrap(),
                "original limited task"
            );
            assert_eq!(j["sandbox"], "read-only");
        }
    }
    #[test]
    fn missing_invalid_or_empty_policy_refuses_attempt_without_overwriting_previous_input() {
        let t = Test::new();
        let j = t.put(job(json!({"id":"saved-owner"})));
        fs::write(
            t.ctx.job_dir("saved-owner").join("prompt.md"),
            "original task",
        )
        .unwrap();
        let selected = t.ctx.prompt_for_attempt(&j).unwrap();
        let before = fs::read(&selected).unwrap();
        let policy = t.ctx.root.join("owner-instructions.md");
        fs::remove_file(&policy).unwrap();
        assert!(t.ctx.prompt_for_attempt(&j).is_err());
        for invalid in [b" \n".as_slice(), &[0xff]] {
            fs::write(&policy, invalid).unwrap();
            assert!(t.ctx.prompt_for_attempt(&j).is_err());
        }
        assert_eq!(fs::read(selected).unwrap(), before);
    }
}
mod token_accounting {
    use super::*;
    #[test]
    fn test_total_is_input_plus_output_with_breakdown() {
        let mut j = new_job();
        apply_event(&mut j, events(OK).last().unwrap());
        assert_eq!(
            j["tokens"],
            json!({"input_tokens":105429,"cached_input_tokens":76032,"cache_write_input_tokens":0,"output_tokens":281,"reasoning_output_tokens":159,"total":105710})
        );
    }
    #[test]
    fn test_usage_is_cumulative_last_wins() {
        let mut j = new_job();
        for n in [100, 250] {
            apply_event(
                &mut j,
                &json!({"type":"turn.completed","usage":{"input_tokens":n,"output_tokens":10}}),
            );
        }
        assert_eq!(j["tokens"]["total"], 260);
    }
    #[test]
    fn test_missing_usage_fields_default_to_zero() {
        let mut j = new_job();
        apply_event(
            &mut j,
            &json!({"type":"turn.completed","usage":{"input_tokens":5}}),
        );
        assert_eq!(j["tokens"]["total"], 5);
        assert_eq!(j["tokens"]["output_tokens"], 0);
    }
}
mod live_session_tokens {
    use super::*;
    #[test]
    fn test_tokens_context_and_rate_limit_come_from_the_session_log() {
        let t = Test::new();
        let mut j = job(json!({"status":"running","thread_id":"tid-1","started_at":t.rt.now()}));
        let dir = t.ctx.codex_home.join("sessions/2026/10/01");
        fs::create_dir_all(&dir).unwrap();
        let p = dir.join("rollout-time-tid-1.jsonl");
        fs::write(&p, SESSION).unwrap();
        let mut sess = Session::default();
        assert!(t.ctx.poll_session(&mut j, &mut sess).unwrap());
        assert_eq!(j["tokens"]["total"], 1042782);
        assert_eq!(j["tokens"]["cached_input_tokens"], 950784);
        assert_eq!(j["context_pct"], 14);
        assert_eq!(j["rate_limit_pct"], 37.0);
        assert!(truth(&j["last_event_at"]));
        assert!(!t.ctx.poll_session(&mut j, &mut sess).unwrap());
        use std::io::Write;
        let mut f = fs::OpenOptions::new().append(true).open(&p).unwrap();
        f.write_all(br#"{"type":"event_msg","payload":{"type":"token_co"#)
            .unwrap();
        assert!(!t.ctx.poll_session(&mut j, &mut sess).unwrap());
        f.write_all(b"unt\",\"info\":{\"total_token_usage\":{\"input_tokens\":2000000,\"output_tokens\":5}}}}\n").unwrap();
        assert!(t.ctx.poll_session(&mut j, &mut sess).unwrap());
        assert_eq!(j["tokens"]["total"], 2000005);
    }
    #[test]
    fn test_no_thread_or_no_log_yet() {
        let t = Test::new();
        assert!(
            !t.ctx
                .poll_session(&mut new_job(), &mut Session::default())
                .unwrap()
        );
        assert!(
            !t.ctx
                .poll_session(
                    &mut job(json!({"thread_id":"nope","started_at":t.rt.now()})),
                    &mut Session::default()
                )
                .unwrap()
        );
    }
}
mod atomic_updates {
    use super::*;
    #[test]
    fn test_file_lock_waits_for_another_process_without_reading_its_locked_byte() {
        use std::io::BufRead;
        let tmp = Temp::new();
        let mut c = hidden_command(env!("CARGO_BIN_EXE_cc_jobs_fixture"));
        c.env("CC_JOBS_FIXTURE_MODE", "lock")
            .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped());
        let mut child = c.spawn().unwrap();
        let mut line = String::new();
        std::io::BufReader::new(child.stdout.take().unwrap())
            .read_line(&mut line)
            .unwrap();
        assert_eq!(line.trim(), "held");
        let start = Instant::now();
        let l = FileLock::acquire(&tmp.0.join("probe.lock")).unwrap();
        assert!(start.elapsed().as_secs_f64() >= 0.5);
        drop(l);
        assert!(child.wait().unwrap().success());
        assert_eq!(fs::metadata(tmp.0.join("probe.lock")).unwrap().len(), 0);
    }
    #[test]
    fn test_roundtrip_leaves_no_temp_files() {
        let t = Test::new();
        // This checks only atomic state storage, not a runnable job fixture.
        let mut j = new_job();
        fs::create_dir_all(t.ctx.job_dir(s(&j, "id"))).unwrap();
        t.ctx.save(&j).unwrap();
        j["status"] = json!("running");
        t.ctx.save(&j).unwrap();
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "running");
        let names = fs::read_dir(t.ctx.job_dir(s(&j, "id")))
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .collect::<Vec<_>>();
        assert_eq!(names, ["job.json"]);
    }
    #[test]
    fn test_retries_when_target_is_locked() {
        let t = Test::new();
        let mut j = t.put(new_job());
        let before = t.rt.replace_count.load(Ordering::Relaxed);
        t.rt.replace_failures.store(2, Ordering::Relaxed);
        j["steps"] = json!(7);
        t.ctx.save(&j).unwrap();
        assert_eq!(t.rt.replace_count.load(Ordering::Relaxed) - before, 3);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["steps"], 7);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [0.02, 0.02]);
    }
    #[test]
    fn test_broken_or_vanished_job_json_is_skipped() {
        let t = Test::new();
        t.put(job(
            json!({"id":"good","status":"succeeded","gto":{"automatic":false}}),
        ));
        for (id, text) in [("broken", "{broken"), ("list", "[]")] {
            fs::create_dir_all(t.ctx.job_dir(id)).unwrap();
            fs::write(t.ctx.job_dir(id).join("job.json"), text).unwrap();
        }
        assert_eq!(
            t.ctx
                .list_jobs(&HashSet::new())
                .iter()
                .map(|j| s(j, "id"))
                .collect::<Vec<_>>(),
            ["good"]
        );
        assert_eq!(
            t.ctx.watch_step(&mut HashMap::new()).unwrap(),
            [t.ctx.watch_line(&t.ctx.load("good").unwrap())]
        );
        let gone = job(json!({"id":"gone","status":"running","pid":4000000,"pid_created":1}));
        assert_eq!(t.ctx.refresh(gone, false).unwrap()["status"], "running");
    }
}
mod status_formatting {
    use super::*;
    #[test]
    fn test_helpers() {
        assert_eq!(fmt_elapsed(Some(23.4)), "23s");
        assert_eq!(fmt_elapsed(Some(245.)), "4m05s");
        assert_eq!(fmt_elapsed(Some(3720.)), "1h02m");
        assert_eq!(fmt_tokens(105710), "105.7k");
        assert_eq!(fmt_tokens(2500000), "2.5M");
        assert_eq!(fmt_tokens(0), "0");
    }
    #[test]
    fn test_table() {
        let t = Test::new();
        let j = job(
            json!({"status":"succeeded","duration_ms":23000,"steps":2,"tokens":{"total":105710},"activity":"done: DONE","cwd":"C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki/very/deep/path"}),
        );
        let output = t.ctx.fmt_table(&[j]);
        let lines = output.lines().collect::<Vec<_>>();
        assert_eq!(
            lines[0].split_whitespace().collect::<Vec<_>>(),
            [
                "ID",
                "NAME",
                "MODEL/EFFORT",
                "STATE",
                "ELAPSED",
                "IDLE",
                "STEPS",
                "TOKENS",
                "ACTIVITY",
                "CWD"
            ]
        );
        for p in [
            "20260929-000000-t",
            "gpt-6-luna/max",
            "succeeded",
            "23s",
            "105.7k",
            "done: DONE",
            "...",
            "deep/path",
        ] {
            assert!(lines[1].contains(p), "{p}");
        }
    }
    #[test]
    fn test_table_aligns_wide_characters() {
        let t = Test::new();
        let output = t.ctx.fmt_table(&[
            job(json!({"name":"detach 日本","status":"succeeded"})),
            job(json!({"name":"ab","status":"failed"})),
        ]);
        let lines = output.lines().skip(1).collect::<Vec<_>>();
        assert_eq!(
            width(&lines[0][..lines[0].find("succeeded").unwrap()]),
            width(&lines[1][..lines[1].find("failed").unwrap()])
        );
        assert_eq!(width("a日本🚀\u{301}"), 8);
    }
    #[test]
    fn test_idle_is_time_since_last_event_of_a_running_job() {
        let t = Test::new();
        let j = job(
            json!({"status":"running","last_event_at":"2026-10-01T11:58:30+09:00","context_pct":14}),
        );
        assert_eq!(t.ctx.idle_s(&j), Some(90.));
        assert!(
            t.ctx
                .idle_s(&patch(j.clone(), json!({"status":"succeeded"})))
                .is_none()
        );
        assert!(t.ctx.summary(&j).contains("idle 1m30s"));
    }
    #[test]
    fn test_status_command_filters_old_jobs() {
        let t = Test::new();
        for (id, fin) in [
            ("old", "2020-01-01T00:00:00+09:00"),
            ("new", "2026-10-01T12:00:00+09:00"),
        ] {
            t.put(job(json!({"id":id,"status":"succeeded","finished_at":fin,"duration_ms":1000,"gto":{"automatic":false}})));
        }
        let (r, out, _) = t.cli(&["status", "--json"]);
        assert_eq!(r.unwrap(), 0);
        let got: Vec<Value> = serde_json::from_str(&out).unwrap();
        assert_eq!(got.iter().map(|j| s(j, "id")).collect::<Vec<_>>(), ["new"]);
    }
}
mod lost_detection {
    use super::*;
    #[test]
    fn test_dead_supervisor_marks_lost_and_persists() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"running","pid":424242,"pid_created":1}),
        ));
        let got = t.ctx.refresh(j, false).unwrap();
        assert_eq!(got["status"], "lost");
        assert!(truth(&got["finished_at"]));
        t.rt.procs.lock().unwrap().insert(424242, 1);
        assert_eq!(
            t.ctx
                .refresh(t.ctx.load(s(&got, "id")).unwrap(), false)
                .unwrap()["status"],
            "lost"
        );
    }
    #[test]
    fn test_reused_pid_is_not_our_supervisor() {
        let t = Test::new();
        for (p, expected) in [
            (
                json!({"status":"running","pid":std::process::id(),"pid_created":12345}),
                "lost",
            ),
            (
                json!({"id":"legacy","status":"running","pid":std::process::id()}),
                "running",
            ),
        ] {
            let j = t.put(job(p));
            let refreshed = t.ctx.refresh(j.clone(), false).unwrap();
            assert_eq!(refreshed["status"], expected);
            if expected == "running" {
                // Unknown creation time is occupied, not proof of PID reuse.
                assert_eq!(refreshed["pid"], j["pid"]);
                assert_eq!(t.ctx.load(s(&j, "id")).unwrap(), j);
            }
        }
    }
    #[test]
    fn test_alive_supervisor_stays_running() {
        let t = Test::new();
        let j = t.put(t.running());
        assert_eq!(t.ctx.refresh(j, false).unwrap()["status"], "running");
    }
    #[test]
    fn test_final_state_written_just_before_exit_wins() {
        let t = Test::new();
        let stale = t.put(job(
            json!({"status":"running","pid":424242,"pid_created":1}),
        ));
        t.ctx
            .save(&patch(stale.clone(), json!({"status":"succeeded"})))
            .unwrap();
        assert_eq!(t.ctx.refresh(stale, false).unwrap()["status"], "succeeded");
    }
    #[test]
    fn test_queued_without_pid_gets_grace_period() {
        let t = Test::new();
        let fresh = t.put(job(json!({"id":"fresh"})));
        let old = t.put(job(
            json!({"id":"old","created_at":"2020-01-01T00:00:00+09:00"}),
        ));
        assert_eq!(t.ctx.refresh(fresh, false).unwrap()["status"], "queued");
        assert_eq!(t.ctx.refresh(old, false).unwrap()["status"], "lost");
    }
    #[test]
    fn test_slow_start_after_grace_is_not_marked_lost() {
        let t = Test::new();
        let stale = t.put(job(json!({"created_at":"2020-01-01T00:00:00+09:00"})));
        t.ctx
            .save(&patch(
                stale.clone(),
                json!({"status":"running","pid":std::process::id(),"pid_created":123}),
            ))
            .unwrap();
        assert_eq!(t.ctx.refresh(stale, false).unwrap()["status"], "running");
    }
    #[test]
    fn test_real_proc_created() {
        assert!(proc_created(std::process::id()).is_some());
        assert!(proc_created(4000000).is_none());
        assert!(proc_created(0).is_none());
    }
    #[test]
    fn unavailable_supervisor_identity_keeps_execution_and_does_not_close_tabs() {
        for code in [5, 6, 299] {
            let t = Test::new();
            let j = t.put(t.running());
            let pid = j["pid"].as_u64().unwrap() as u32;
            t.rt.proc_probe_errors.lock().unwrap().insert(pid, code);
            assert!(t.ctx.supervisor_alive(&j));
            assert_eq!(t.ctx.refresh(j.clone(), false).unwrap(), j);
            assert_eq!(t.ctx.load(s(&j, "id")).unwrap(), j);
            assert!(t.rt.calls.lock().unwrap().is_empty());
            assert!(t.rt.kills.lock().unwrap().is_empty());
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        }
    }
    #[test]
    fn known_absence_and_reused_pid_still_mark_the_job_lost() {
        for reused in [false, true] {
            let t = Test::new();
            let j = t.put(t.running());
            let pid = j["pid"].as_u64().unwrap() as u32;
            if reused {
                t.rt.procs.lock().unwrap().insert(pid, 124);
            } else {
                t.rt.procs.lock().unwrap().remove(&pid);
            }
            assert!(!t.ctx.supervisor_alive(&j));
            assert_eq!(t.ctx.refresh(j, false).unwrap()["status"], "lost");
        }
    }
    #[cfg(windows)]
    #[test]
    fn exited_process_with_still_active_exit_code_is_not_live() {
        use std::os::windows::process::CommandExt;
        let mut child = std::process::Command::new("cmd.exe")
            .args(["/d", "/c", "exit", "259"])
            .creation_flags(NO_WINDOW)
            .spawn()
            .unwrap();
        let pid = child.id();
        assert_eq!(child.wait().unwrap().code(), Some(259));
        assert_eq!(proc_created_checked(pid).unwrap(), None);
    }
    #[test]
    fn test_watch_reports_changes_once_and_skips_finished_jobs() {
        let t = Test::new();
        let j = t.put(t.running());
        let mut seen = HashMap::new();
        assert_eq!(t.ctx.watch_step(&mut seen).unwrap().len(), 1);
        assert!(t.ctx.watch_step(&mut seen).unwrap().is_empty());
        t.ctx
            .save(&patch(
                j,
                json!({"status":"failed","last_error":"boom","gto":{"automatic":false}}),
            ))
            .unwrap();
        let lines = t.ctx.watch_step(&mut seen).unwrap();
        assert_eq!(lines.len(), 1);
        assert!(lines[0].contains("failed"));
        assert!(lines[0].contains("boom"));
        let before = t.rt.load_count.load(Ordering::Relaxed);
        assert!(t.ctx.watch_step(&mut seen).unwrap().is_empty());
        assert_eq!(t.rt.load_count.load(Ordering::Relaxed), before);
    }
}
mod cancel {
    use super::*;
    #[test]
    fn stale_managed_tree_marker_with_no_recorded_process_uses_empty_legacy_targets() {
        let t = Test::new();
        let j = t.put(patch(
            t.running(),
            json!({"pid":null,"pid_created":null,"codex_pid":null,
                "codex_pid_created":null,"process_tree_version":1}),
        ));
        assert_eq!(t.cli(&["cancel", s(&j, "id")]).0.unwrap(), 0);
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], "cancelled");
        assert_eq!(saved["process_tree_cancel_method"], "retained_handle_walk");
    }
    #[test]
    fn managed_job_completion_does_not_reopen_recorded_pids() {
        let t = Test::new();
        let pid = std::process::id();
        t.rt.procs.lock().unwrap().insert(321, 456);
        t.rt.process_tree_job_supported
            .store(true, Ordering::Relaxed);
        let j = t.put(patch(
            t.running(),
            json!({"process_tree_version":1,"codex_pid":321,"codex_pid_created":456}),
        ));
        let rt = t.rt.clone();
        *t.rt.kill_hook.lock().unwrap() = Some(Box::new(move |_| {
            let mut errors = rt.proc_probe_errors.lock().unwrap();
            errors.insert(pid, 5);
            errors.insert(321, 5);
        }));
        assert_eq!(t.cli(&["cancel", s(&j, "id")]).0.unwrap(), 0);
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], "cancelled");
        assert_eq!(saved["process_tree_cancel_method"], "job_object");
        assert_eq!(*t.rt.kills.lock().unwrap(), [pid]);
    }
    #[test]
    fn missing_supervisor_creation_time_preserves_reservation_and_skips_cleanup() {
        let t = Test::new();
        t.rt.procs.lock().unwrap().insert(500, 777);
        let j = t.put(patch(t.running(), json!({"pid":500,"pid_created":null})));
        assert!(t.ctx.supervisor_alive(&j));
        assert_eq!(
            t.ctx.refresh(j.clone(), false).unwrap()["status"],
            "running"
        );
        let terminal = t.put(patch(
            j,
            json!({"status":"succeeded", "codex_pid":null,
            "finished_at":"2026-01-01T00:00:00+00:00"}),
        ));
        assert!(
            t.ctx
                .cleanup_supervisors(true)
                .unwrap()
                .as_array()
                .unwrap()
                .is_empty()
        );
        assert_eq!(t.ctx.load(s(&terminal, "id")).unwrap(), terminal);
        assert_eq!(t.rt.pause_calls.load(Ordering::Relaxed), 0);
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn inaccessible_identity_never_certifies_cancellation_or_releases_ownership() {
        for key in ["pid", "codex_pid"] {
            let t = Test::new();
            t.rt.procs.lock().unwrap().insert(321, 456);
            let j = t.put(patch(
                t.running(),
                json!({"codex_pid":321,"codex_pid_created":456}),
            ));
            let pid = j[key].as_u64().unwrap() as u32;
            t.rt.proc_probe_errors.lock().unwrap().insert(pid, 5);
            assert!(t.ctx.is_ours_checked(&j, key).is_err());
            assert!(t.ctx.is_ours(&j, key));
            assert!(t.cli(&["cancel", s(&j, "id")]).0.is_err());
            let saved = t.ctx.load(s(&j, "id")).unwrap();
            assert_eq!(saved["status"], "running");
            assert_eq!(saved["cancel_requested"], true);
            assert!(saved["finished_at"].is_null());
            assert!(t.rt.kills.lock().unwrap().is_empty());
        }
    }
    #[test]
    fn cancellation_cannot_certify_exit_when_post_stop_inspection_fails() {
        let t = Test::new();
        let j = t.put(t.running());
        let rt = t.rt.clone();
        *t.rt.kill_hook.lock().unwrap() = Some(Box::new(move |pid| {
            rt.proc_probe_errors.lock().unwrap().insert(pid, 5);
        }));
        assert!(t.cli(&["cancel", s(&j, "id")]).0.is_err());
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "running");
    }
    fn guard_store(t: &Test) -> PathBuf {
        let store = t.ctx.root.join("todo.json");
        t.rt.env.lock().unwrap().insert(
            "CC_TODO_HOME".into(),
            t.ctx.root.to_string_lossy().into_owned(),
        );
        fs::write(&store,json!({"items":[{"id":2073,"status":"todo","jobs":[]},{"id":2074,"status":"todo"},{"id":32,"status":"todo"}]}).to_string()).unwrap();
        store
    }
    fn add_wait(store: &Path) {
        let mut db: Value = serde_json::from_slice(&fs::read(store).unwrap()).unwrap();
        db["items"][1]["wait_on"] = json!([32]);
        fs::write(store, db.to_string()).unwrap();
    }
    fn guarded(t: &Test, jid: &str, store: &Path, key: &str) -> (Result<i32>, String, String) {
        t.cli(&[
            "cancel",
            jid,
            "--recovery",
            "--if-todo-ready",
            "--cancel-key",
            key,
            "--todo-file",
            store.to_str().unwrap(),
            "--todo-id",
            "2073",
            "--related-todo-ids",
            "2073,2074",
        ])
    }
    #[test]
    #[cfg(windows)]
    fn guarded_wait_registration_wins_before_cancellation_and_refresh() {
        for status in ["queued", "running", "succeeded"] {
            let t = Test::new();
            let store = guard_store(&t);
            let j = t.put(job(
                json!({"id":"source","status":status,"pid":std::process::id(),"pid_created":123}),
            ));
            let original = fs::read(t.ctx.job_dir("source").join("job.json")).unwrap();
            let todo = FileLock::acquire(&store.with_file_name("todo.lock")).unwrap();
            let (ready, arrived) = std::sync::mpsc::channel();
            let job_path = t.ctx.job_dir("source").join("job.json");
            let queue_path = t.ctx.jobs.join(".queue.lock");
            let state_path = t.ctx.job_dir("source").join("state.lock");
            t.rt.load_count.store(0, Ordering::Relaxed);
            *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |path, n| {
                if path == job_path && n == 1 {
                    assert!(FileLock::try_acquire(&queue_path).is_err());
                    assert!(FileLock::try_acquire(&state_path).is_err());
                    ready.send(()).unwrap();
                }
            }));
            let ctx = t.ctx.clone();
            let path = store.clone();
            let cancel = std::thread::spawn(move || {
                let mut out = vec![];
                let mut err = vec![];
                let result = ctx.dispatch(
                    &[
                        "cancel",
                        "source",
                        "--recovery",
                        "--if-todo-ready",
                        "--cancel-key",
                        "attempt",
                        "--todo-file",
                        path.to_str().unwrap(),
                        "--todo-id",
                        "2073",
                        "--related-todo-ids",
                        "2074",
                    ]
                    .map(String::from),
                    &mut out,
                    &mut err,
                );
                (result, out, err)
            });
            arrived
                .recv_timeout(std::time::Duration::from_secs(5))
                .unwrap();
            add_wait(&store);
            drop(todo);
            let (result, out, err) = cancel.join().unwrap();
            assert_eq!(result.unwrap(), 0, "{}", String::from_utf8_lossy(&err));
            let response: Value = serde_json::from_slice(&out).unwrap();
            assert_eq!(response["outcome"], "deferred");
            assert_eq!(response["wait_on"], json!([32]));
            assert_eq!(
                fs::read(t.ctx.job_dir("source").join("job.json")).unwrap(),
                original
            );
            assert_eq!(t.ctx.load("source").unwrap(), j);
            assert!(t.rt.kills.lock().unwrap().is_empty());
            assert!(t.rt.calls.lock().unwrap().is_empty());
        }
    }
    #[test]
    fn guarded_cancel_commits_before_later_wait_and_releases_locks_before_killing() {
        let t = Test::new();
        let store = guard_store(&t);
        t.put(job(
            json!({"id":"source","status":"running","pid":std::process::id(),"pid_created":123}),
        ));
        let todo_path = store.with_file_name("todo.lock");
        let queue_path = t.ctx.jobs.join(".queue.lock");
        let state_path = t.ctx.job_dir("source").join("state.lock");
        let job_path = t.ctx.job_dir("source").join("job.json");
        let (ready, arrived) = std::sync::mpsc::channel();
        let (release, resume) = std::sync::mpsc::channel();
        let resume = Mutex::new(resume);
        *t.rt.kill_hook.lock().unwrap() = Some(Box::new(move |_| {
            let todo = FileLock::try_acquire(&todo_path).expect("kill retained todo.lock");
            drop(FileLock::try_acquire(&queue_path).expect("kill retained queue lock"));
            assert!(FileLock::try_acquire(&state_path).is_err());
            let j: Value = serde_json::from_slice(&fs::read(&job_path).unwrap()).unwrap();
            assert_eq!(j["recovery_cancel"]["key"], "attempt");
            ready.send(()).unwrap();
            resume
                .lock()
                .unwrap()
                .recv_timeout(std::time::Duration::from_secs(5))
                .unwrap();
            drop(todo);
        }));
        let ctx = t.ctx.clone();
        let path = store.clone();
        let cancel = std::thread::spawn(move || {
            let mut out = vec![];
            let mut err = vec![];
            let result = ctx.dispatch(
                &[
                    "cancel",
                    "source",
                    "--recovery",
                    "--if-todo-ready",
                    "--cancel-key",
                    "attempt",
                    "--todo-file",
                    path.to_str().unwrap(),
                    "--related-todo-ids",
                    "2073,2074",
                ]
                .map(String::from),
                &mut out,
                &mut err,
            );
            (result, out)
        });
        arrived
            .recv_timeout(std::time::Duration::from_secs(5))
            .unwrap();
        // The hook owns the real shared lock on behalf of this wait writer.
        add_wait(&store);
        release.send(()).unwrap();
        let (result, out) = cancel.join().unwrap();
        assert_eq!(result.unwrap(), 0);
        assert_eq!(
            serde_json::from_slice::<Value>(&out).unwrap()["outcome"],
            "cancelled"
        );
        assert_eq!(t.ctx.load("source").unwrap()["status"], "cancelled");
        assert_eq!(
            t.ctx.load("source").unwrap()["related_todo_ids"],
            json!([2073, 2074])
        );
        assert_ne!(t.ctx.load("source").unwrap()["cancel_requested"], true);
        assert_eq!(t.rt.kills.lock().unwrap().len(), 1);
        t.rt.procs.lock().unwrap().insert(777, 777);
        t.put(job(
            json!({"id":"successor","status":"queued","pid":777,"pid_created":777,
            "related_todo_ids":[2073,2074],"cwd":t.ctx.root}),
        ));
        assert!(t.ctx.admit("successor").unwrap().is_none());
        assert_eq!(
            t.ctx.load("successor").unwrap()["queue_wait_reason"]["wait_on"],
            json!([32])
        );
    }
    #[test]
    fn guarded_retry_wait_defers_cancellation_without_changing_source() {
        let t = Test::new();
        let store = guard_store(&t);
        let mut db: Value = serde_json::from_slice(&fs::read(&store).unwrap()).unwrap();
        db["items"][0]["status"] = json!("wait");
        db["items"][0]["wait_ready"] =
            json!({"condition":"retry-time","since":100,"not_before":3700});
        fs::write(&store, db.to_string()).unwrap();
        let source = t.put(job(
            json!({"id":"source","status":"running","pid":std::process::id(),"pid_created":123}),
        ));
        let (result, out, _) = guarded(&t, "source", &store, "attempt");
        assert_eq!(result.unwrap(), 0);
        let response: Value = serde_json::from_str(&out).unwrap();
        assert_eq!(response["outcome"], "deferred");
        assert!(!response["wait_ready"].as_array().unwrap().is_empty());
        assert_eq!(t.ctx.load("source").unwrap(), source);
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn guarded_commit_is_attempt_specific_and_owner_stop_always_wins() {
        for key in ["old", "attempt"] {
            let t = Test::new();
            let store = guard_store(&t);
            add_wait(&store);
            t.put(job(
                json!({"id":"source","status":"queued","pid":std::process::id(),"pid_created":123,
                "recovery_cancel":{"key":key,"committed_at":"2026-10-11T02:01:36+09:00"}}),
            ));
            let (result, out, _) = guarded(&t, "source", &store, "attempt");
            assert_eq!(result.unwrap(), 0);
            assert_eq!(
                serde_json::from_str::<Value>(&out).unwrap()["outcome"],
                if key == "old" {
                    "deferred"
                } else {
                    "cancelled"
                }
            );
        }
        for stop in [
            json!({"status":"running","cancel_requested":true}),
            json!({"status":"cancelled","cancel_reason":"manual"}),
            json!({"status":"running","cancel_reason":"person"}),
            json!({"status":"running","stopped":true}),
        ] {
            let t = Test::new();
            let store = guard_store(&t);
            let j = t.put(patch(
                job(json!({"id":"source","recovery_cancel":{"key":"attempt"}})),
                stop,
            ));
            let (result, out, _) = guarded(&t, "source", &store, "attempt");
            assert_eq!(result.unwrap(), 0);
            assert_eq!(
                serde_json::from_str::<Value>(&out).unwrap()["outcome"],
                "owner_stopped"
            );
            assert_eq!(t.ctx.load("source").unwrap(), j);
            assert!(t.rt.kills.lock().unwrap().is_empty());
        }
    }
    #[test]
    fn guarded_terminal_scope_validation_and_missing_dependency_remain_explicit() {
        let t = Test::new();
        let store = guard_store(&t);
        let j = t.put(job(json!({"id":"source","status":"succeeded"})));
        let (result, out, _) = guarded(&t, "source", &store, "attempt");
        assert_eq!(result.unwrap(), 0);
        assert_eq!(
            serde_json::from_str::<Value>(&out).unwrap()["outcome"],
            "already_terminal"
        );
        assert_eq!(t.ctx.load("source").unwrap(), j);
        for args in [
            vec!["cancel", "source", "--if-todo-ready"],
            vec!["cancel", "source", "--related-todo-ids", "2073"],
            vec![
                "cancel",
                "source",
                "--recovery",
                "--if-todo-ready",
                "--cancel-key",
                "attempt",
                "--related-todo-ids",
                "bad",
            ],
        ] {
            assert!(t.cli(&args).0.is_err());
            assert_eq!(t.ctx.load("source").unwrap(), j);
        }
        let mut db: Value = serde_json::from_slice(&fs::read(&store).unwrap()).unwrap();
        db["items"][1]["wait_on"] = json!([99]);
        fs::write(&store, db.to_string()).unwrap();
        let (_, out, _) = guarded(&t, "source", &store, "attempt");
        assert_eq!(
            serde_json::from_str::<Value>(&out).unwrap()["wait_on"],
            json!([99])
        );
        fs::write(&store, "{broken").unwrap();
        assert!(guarded(&t, "source", &store, "attempt").0.is_err());
        assert_eq!(t.ctx.load("source").unwrap(), j);
    }
    #[test]
    fn guarded_missing_todo_store_never_commits_cancellation() {
        for status in ["queued", "running", "succeeded"] {
            let t = Test::new();
            let store = guard_store(&t);
            fs::remove_file(&store).unwrap();
            t.put(job(json!({"id":"source","status":status,"todo_id":2073})));
            let file = t.ctx.job_dir("source").join("job.json");
            let before = fs::read(&file).unwrap();
            assert!(guarded(&t, "source", &store, "attempt").0.is_err());
            assert_eq!(fs::read(&file).unwrap(), before);
            assert!(t.ctx.load("source").unwrap()["recovery_cancel"].is_null());
            assert!(!store.exists());
        }
    }
    #[test]
    fn native_guarded_cancellation_defers_without_refreshing_or_rewriting_source() {
        let tmp = Temp::new();
        let exe = tmp
            .0
            .join(format!("codex_jobs{}", std::env::consts::EXE_SUFFIX));
        fs::copy(env!("CARGO_BIN_EXE_codex_jobs"), &exe).unwrap();
        let dir = tmp.0.join("jobs/source");
        fs::create_dir_all(&dir).unwrap();
        let store = tmp.0.join("todo.json");
        fs::write(
            &store,
            json!({"items":[{"id":2074,"status":"wait","wait_on":[32]},{"id":32,"status":"todo"}]})
                .to_string(),
        )
        .unwrap();
        for status in ["queued", "succeeded"] {
            let original=json!({"id":"source","status":status,"steps":0,"model":"gpt-6-luna","effort":"max","pid":null}).to_string();
            fs::write(dir.join("job.json"), &original).unwrap();
            let mut cmd = hidden_command(&exe.to_string_lossy());
            cmd.env_clear()
                .env("USERPROFILE", &tmp.0)
                .env("HOME", &tmp.0)
                .env("TMP", &tmp.0)
                .env("TEMP", &tmp.0);
            if let Some(root) = std::env::var_os("SystemRoot") {
                cmd.env("SystemRoot", root);
            }
            let output = cmd
                .args([
                    "cancel",
                    "source",
                    "--recovery",
                    "--if-todo-ready",
                    "--cancel-key",
                    "attempt",
                    "--related-todo-ids",
                    "2074",
                    "--todo-file",
                    store.to_str().unwrap(),
                ])
                .output()
                .unwrap();
            assert!(
                output.status.success(),
                "{}",
                String::from_utf8_lossy(&output.stderr)
            );
            let result: Value = serde_json::from_slice(&output.stdout).unwrap();
            assert_eq!(result["outcome"], "deferred");
            assert_eq!(result["wait_on"], json!([32]));
            assert_eq!(fs::read_to_string(dir.join("job.json")).unwrap(), original);
        }
    }
    #[test]
    fn recovery_cleanup_does_not_record_a_deliberate_cancel() {
        let t = Test::new();
        let j = t.put(t.running());
        assert_eq!(t.cli(&["cancel", s(&j, "id"), "--recovery"]).0.unwrap(), 0);
        assert_ne!(t.ctx.load(s(&j, "id")).unwrap()["cancel_requested"], true);
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert!(cc_job_policy::cancellation::recovery_cancel(
            &Value::Null,
            &saved
        ));
        assert!(!cc_job_policy::cancellation::blocks_recovery(
            &json!({}),
            &saved
        ));
    }
    #[test]
    fn plain_recovery_preserves_guarded_intent_and_never_overrides_explicit_stops() {
        let t = Test::new();
        let mut j = t.running();
        j["recovery_cancel"] =
            json!({"key":"guarded-original","committed_at":"2026-10-11T02:01:36+09:00"});
        t.put(j.clone());
        assert_eq!(t.cli(&["cancel", s(&j, "id"), "--recovery"]).0.unwrap(), 0);
        assert_eq!(
            t.ctx.load(s(&j, "id")).unwrap()["recovery_cancel"],
            j["recovery_cancel"]
        );
        for stop in [
            json!({"cancel_requested":true}),
            json!({"stopped":true}),
            json!({"cancel_reason":"manual"}),
            json!({"cancel_reason":"person"}),
        ] {
            let t = Test::new();
            let mut j = t.running();
            j["recovery_cancel"] = json!({"key":"old","committed_at":"2026-10-11T02:01:36+09:00"});
            let j = t.put(patch(j, stop));
            assert!(t.cli(&["cancel", s(&j, "id"), "--recovery"]).0.is_err());
            assert_eq!(
                t.ctx.load(s(&j, "id")).unwrap()["recovery_cancel"],
                j["recovery_cancel"]
            );
            assert!(t.rt.kills.lock().unwrap().is_empty());
        }
    }
    #[test]
    fn test_cancel_queued_before_supervisor_starts_never_launches_codex() {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "hello").unwrap();
        assert_eq!(t.cli(&["cancel", s(&j, "id")]).0.unwrap(), 0);
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "cancelled");
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["cancel_requested"], true);
    }
    #[test]
    fn test_supervisor_reloads_state_under_lock() {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(|path, n| {
            if n == 1 {
                let mut j: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
                j["status"] = json!("cancelled");
                fs::write(path, pretty(&j)).unwrap();
            }
        }));
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "cancelled");
    }
    #[test]
    fn test_spawned_child_is_stopped_if_supervisor_setup_fails() {
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), "hello").unwrap();
        t.rt.spawn_live.store(true, Ordering::Relaxed);
        // A live fake child must expose its identity so the injected failure
        // occurs on the first post-spawn save, rather than during cleanup.
        t.rt.procs.lock().unwrap().insert(321, 456);
        t.rt.save_failure.store(true, Ordering::Relaxed);
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
        assert_eq!(*t.rt.kills.lock().unwrap(), [321]);
        assert!(!t.rt.child_alive.load(Ordering::Relaxed));
        assert_eq!(t.rt.proc_created(321), None);
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], "failed");
        assert!(s(&saved, "last_error").contains("disk full"));
    }
    #[test]
    fn test_never_kills_a_process_that_reused_the_pid() {
        let t = Test::new();
        t.rt.procs.lock().unwrap().insert(555, 99);
        let j = t.put(job(json!({"status":"running","pid":555,"pid_created":98,
            "codex_pid":555,"codex_pid_created":98})));
        let (r, out, _) = t.cli(&["cancel", s(&j, "id")]);
        assert_eq!(r.unwrap(), 0);
        assert_eq!(t.rt.proc_created(555), Some(99));
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "lost");
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["cancel_requested"], true);
        assert!(out.contains("no leftover process"));
        assert!(t.rt.kills.lock().unwrap().is_empty());
    }
    #[test]
    fn test_refuses_finished_job() {
        let t = Test::new();
        let j = t.put(job(json!({"status":"succeeded"})));
        assert!(t.cli(&["cancel", s(&j, "id")]).0.is_err());
    }
    #[test]
    fn test_reports_failure_and_keeps_state_when_kill_does_not_work() {
        let t = Test::new();
        let j = t.put(t.running());
        t.rt.kill_works.store(false, Ordering::Relaxed);
        let (r, _, err) = t.cli(&["cancel", s(&j, "id")]);
        assert_eq!(r.unwrap(), 1);
        assert!(err.contains("could not stop"));
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "running");
    }
}
mod tail_and_wait {
    use super::*;
    #[test]
    fn test_n0_shows_only_the_summary_and_unterminated_last_line_is_shown() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"succeeded","duration_ms":1000,"gto":{"automatic":false}}),
        ));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("events.jsonl"), OK).unwrap();
        assert_eq!(
            t.cli(&["tail", s(&j, "id"), "-n", "0"]).1.lines().count(),
            1
        );
        let out = t.cli(&["tail", s(&j, "id"), "-n", "2"]).1;
        let lines = out.lines().collect::<Vec<_>>();
        assert_eq!(lines[0], "> DONE");
        assert!(lines[1].starts_with("turn completed: 105,710"));
    }
    #[test]
    fn test_reads_only_the_end_of_a_big_file() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"succeeded","duration_ms":1000,"gto":{"automatic":false}}),
        ));
        let text = (0..20000)
            .map(|i| {
                format!(
                    "{}\n",
                    json!({"type":"thread.started","thread_id":format!("t{i}")})
                )
            })
            .collect::<String>();
        let p = t.ctx.job_dir(s(&j, "id")).join("events.jsonl");
        fs::write(&p, &text).unwrap();
        let out = t.cli(&["tail", s(&j, "id"), "-n", "3"]).1;
        assert_eq!(
            out.lines().take(3).collect::<Vec<_>>(),
            ["session t19997", "session t19998", "session t19999"]
        );
        assert_eq!(
            t.cli(&["tail", s(&j, "id"), "-n", "30000"])
                .1
                .lines()
                .count(),
            20001
        );
        let (_, pos, rest) = read_tail(&p, 3, false).unwrap();
        assert_eq!(pos, text.len() as u64);
        assert!(rest.is_empty());
    }
    #[test]
    fn test_wait_timeout_zero_returns_at_once() {
        let t = Test::new();
        let j = t.put(t.running());
        let (r, out, _) = t.cli(&["wait", s(&j, "id"), "--timeout", "0"]);
        assert_eq!(r.unwrap(), 2);
        assert!(out.contains("timeout after 0.0s:"));
    }
}
mod gto_rating {
    use super::*;
    fn rated(t: &Test) -> Value {
        t.put(job(json!({"status":"succeeded","duration_ms":23000,"task_type":"review","notes_hint":"hint","tokens":{"total":105710}})))
    }
    #[test]
    fn test_payload() {
        let t = Test::new();
        let j = rated(&t);
        assert_eq!(
            t.ctx.gto_payload(&j, 4, None, None),
            json!({"kind":"model_fit_observation","model":"gpt-6-luna","reasoning_effort":"max","task_type":"review","duration_ms":23000,"tokens":105710,"result_quality":4,"notes":"hint","source":"codex-jobs"})
        );
        let p = t.ctx.gto_payload(&j, 1, Some("impl"), Some("n"));
        assert_eq!((s(&p, "task_type"), s(&p, "notes")), ("impl", "n"));
        let lost = job(
            json!({"status":"lost","started_at":"2026-09-29T00:00:00+09:00","finished_at":"2026-09-29T00:01:30+09:00"}),
        );
        assert_eq!(
            t.ctx.gto_payload(&lost, 1, None, None)["duration_ms"],
            90000
        );
    }
    #[test]
    fn test_rate_sends_payload_once() {
        let t = Test::new();
        rated(&t);
        assert_eq!(t.rate(&["--quality", "5"]).unwrap(), 0);
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(
            calls[0].argv,
            [t.ctx.gto.to_str().unwrap(), "internal", "ingest-turn"]
        );
        let payload: Value = serde_json::from_slice(&calls[0].input).unwrap();
        assert_eq!(payload["result_quality"], 5);
        drop(calls);
        assert_eq!(
            t.ctx.load("20260929-000000-t").unwrap()["gto"]["payload"]["tokens"],
            105710
        );
        assert!(t.rate(&[]).is_err());
        assert_eq!(t.rate(&["--force"]).unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 2);
    }
    #[test]
    fn test_rate_refuses_partial_jobs_without_force() {
        let t = Test::new();
        for st in ["cancelled", "lost"] {
            t.put(job(json!({"status":st})));
            assert!(t.rate(&[]).is_err());
            assert!(t.rt.calls.lock().unwrap().is_empty());
        }
    }
    #[test]
    fn test_rate_survives_gto_timeout() {
        let t = Test::new();
        rated(&t);
        t.rt.replies
            .lock()
            .unwrap()
            .push_back(Err(io::Error::new(io::ErrorKind::TimedOut, "gto")));
        assert_eq!(t.rate(&[]).unwrap(), 1);
        assert_eq!(
            *t.rt.diagnostics.lock().unwrap(),
            ["gto ingest-turn failed: TimeoutExpired"]
        );
        assert!(
            t.ctx
                .load("20260929-000000-t")
                .unwrap()
                .get("gto")
                .is_none()
        );
    }
    #[test]
    fn test_rate_skips_without_gto() {
        let t = Test::new();
        rated(&t);
        fs::remove_file(&t.ctx.gto).unwrap();
        let (r, out, _) = t.cli(&["rate", "20260929-000000-t", "--quality", "3"]);
        assert_eq!(r.unwrap(), 0);
        assert!(t.rt.calls.lock().unwrap().is_empty());
        assert!(out.contains("skipped"));
        assert!(
            t.ctx
                .load("20260929-000000-t")
                .unwrap()
                .get("gto")
                .is_none()
        );
    }
    #[test]
    fn test_rate_refuses_running_job() {
        let t = Test::new();
        t.put(t.running());
        assert!(t.rate(&[]).is_err());
    }
}
mod auto_gto_rating {
    use super::*;
    #[test]
    fn test_quality_mapping_for_both_verdict_languages() {
        for (v, q) in [
            (Some("completed"), 4),
            (Some("完了"), 4),
            (None, 3),
            (Some("needs_input"), 3),
            (Some("判断待ち"), 3),
            (Some("partial"), 2),
            (Some("途中で止まった"), 2),
            (Some("not_done"), 2),
            (Some("未完了"), 2),
        ] {
            let (got, notes) = auto_quality(&job(json!({"status":"succeeded"})), v);
            assert_eq!(got, q);
            assert!(notes.starts_with("auto:"));
            if v.is_none() {
                assert_eq!(notes, "auto: no evaluation");
            }
        }
    }
    #[test]
    fn test_infra_failures_are_one_and_override_completion_evidence() {
        for (st, err) in [
            ("failed", Some("proxy timeout")),
            ("failed", Some("Provider error 429")),
            ("failed", Some("supervisor error: FileNotFoundError")),
            ("cancelled", Some("connection reset")),
            ("lost", None),
        ] {
            let (q, n) = auto_quality(
                &job(json!({"status":st,"last_error":err})),
                Some("completed"),
            );
            assert_eq!(q, 1);
            assert!(n.contains("infra"));
        }
    }
    #[test]
    fn test_unknown_failed_or_manual_cancelled_cause_is_not_labelled_infra() {
        for st in ["failed", "cancelled"] {
            let (q, n) = auto_quality(
                &job(json!({"status":st,"last_error":"assertion mismatch"})),
                None,
            );
            assert_eq!(q, 2);
            assert!(!n.contains("infra"));
        }
    }
    #[test]
    fn test_auto_payload_and_once_only_checkpoint() {
        let t = Test::new();
        let mut j=t.put(job(json!({"status":"succeeded","task_type":"implementation","duration_ms":9000,"tokens":{"total":1200}})));
        assert!(t.ctx.auto_rate(&mut j, Some(Some("completed"))));
        assert!(!t.ctx.auto_rate(&mut j.clone(), Some(Some("completed"))));
        let mut stale = patch(j.clone(), json!({"gto":null}));
        assert!(!t.ctx.auto_rate(&mut stale, Some(Some("completed"))));
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls.len(), 1);
        assert_eq!(
            serde_json::from_slice::<Value>(&calls[0].input).unwrap(),
            json!({"kind":"model_fit_observation","model":"gpt-6-luna","reasoning_effort":"max","task_type":"implementation","duration_ms":9000,"tokens":1200,"result_quality":4,"notes":"auto: Jev 完了","source":"codex-jobs"})
        );
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["gto"]["automatic"], true);
    }
    #[test]
    fn test_terminal_hook_on_lost_refresh() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"running","created_at":"2026-09-20T00:00:00+09:00"}),
        ));
        let got = t.ctx.refresh(j, true).unwrap();
        assert_eq!(got["status"], "lost");
        assert_eq!(
            serde_json::from_slice::<Value>(&t.rt.calls.lock().unwrap()[0].input).unwrap()["result_quality"],
            1
        );
        assert!(truth(&t.ctx.load(s(&got, "id")).unwrap()["gto"]));
    }
    #[test]
    fn test_terminal_refresh_covers_a_supervisor_that_exited_before_sending() {
        let t = Test::new();
        let j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        t.ctx.refresh(j.clone(), true).unwrap();
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["gto"]["automatic"], true);
    }
    #[test]
    fn test_terminal_hook_on_cancel() {
        let t = Test::new();
        let j = t.put(new_job());
        assert_eq!(t.cli(&["cancel", s(&j, "id")]).0.unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "cancelled");
    }
    #[test]
    fn test_cancel_lost_job_sends_outside_state_lock() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"running","created_at":"2026-09-20T00:00:00+09:00"}),
        ));
        let root = t.ctx.job_dir(s(&j, "id"));
        t.rt.with_handler(move |_| {
            let _l = FileLock::try_acquire(&root.join("state.lock"))?;
            Ok(Reply {
                code: 0,
                stdout: vec![],
                stderr: vec![],
            })
        });
        let (r, _, err) = t.cli(&["cancel", s(&j, "id")]);
        assert_eq!(r.unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        assert_eq!(
            t.ctx.load(s(&j, "id")).unwrap()["gto"]["payload"]["result_quality"],
            1
        );
        assert!(!err.contains("rating failed"));
    }
    #[test]
    fn test_automatic_ingest_failure_remains_pending_and_retries_later() {
        let t = Test::new();
        let mut j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 1,
            stdout: vec![],
            stderr: b"sensitive-stderr".to_vec(),
        }));
        assert!(!t.ctx.auto_rate(&mut j, Some(None)));
        let mut saved = t.ctx.load(s(&j, "id")).unwrap();
        assert!(saved.get("gto").is_none());
        assert_eq!(saved["gto_auto_pending"], true);
        let before = t.rt.calls.lock().unwrap().len();
        t.ctx.refresh(saved.clone(), true).unwrap();
        assert_eq!(t.rt.calls.lock().unwrap().len(), before);
        saved["gto_auto_attempt_at"] = json!("2026-09-20T00:00:00+09:00");
        t.ctx.save(&saved).unwrap();
        t.ctx.refresh(saved, true).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["gto"]["automatic"], true);
        assert!(saved.get("gto_auto_pending").is_none());
    }
    #[test]
    fn test_live_or_manually_rated_jobs_are_not_automatically_sent() {
        let t = Test::new();
        let mut live = t.put(job(json!({"status":"running"})));
        assert!(!t.ctx.auto_rate(&mut live, None));
        assert!(!t.ctx.auto_rate(
            &mut patch(
                live,
                json!({"status":"succeeded","gto":{"automatic":false}})
            ),
            None
        ));
        assert!(t.rt.calls.lock().unwrap().is_empty());
    }
    #[test]
    fn test_manual_rate_can_override_auto_and_force_still_controls_repeat_manual_rate() {
        let t = Test::new();
        let mut j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        assert!(t.ctx.auto_rate(&mut j, Some(None)));
        assert_eq!(t.rate(&["--quality", "5", "--notes", "manual"]).unwrap(), 0);
        assert!(t.rate(&[]).is_err());
        assert_eq!(t.rate(&["--quality", "5", "--force"]).unwrap(), 0);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 3);
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["gto"]["automatic"], false);
        assert_eq!(saved["gto"]["payload"]["result_quality"], 5);
        assert_eq!(saved["gto_history"].as_array().unwrap().len(), 2);
        assert_eq!(saved["gto_history"][0]["automatic"], true);
    }
    #[test]
    fn test_jev_lookup_matches_session_filename_and_reads_no_excerpts() {
        let t = Test::new();
        let sid = "01a0f188-0c24-7860-ab46-6c424b1def08";
        t.rt.replies.lock().unwrap().push_back(Ok(Reply{code:0,stdout:json!([{"session_path":format!("2026/09/30/rollout-time-{sid}.jsonl"),"verdict":"completed"}]).to_string().into_bytes(),stderr:vec![]}));
        assert_eq!(
            t.ctx.jev_evaluations(Some(sid), None),
            HashMap::from([(sid.into(), "completed".into())])
        );
        let calls = t.rt.calls.lock().unwrap();
        let args = &calls[0].argv;
        assert_eq!(&args[..4], ["docker", "exec", "godd-ao-postgres", "sh"]);
        assert!(args[5].contains("$POSTGRES_USER"));
        assert!(args.last().unwrap().contains(&format!("{sid}.jsonl")));
        assert!(args.last().unwrap().contains("completed_at DESC"));
        assert!(!args.last().unwrap().contains("excerpt"));
        assert_eq!(calls[0].flags, NO_WINDOW);
    }
    #[test]
    fn test_invalid_session_id_cannot_enter_sql() {
        let t = Test::new();
        assert!(
            t.ctx
                .jev_evaluations(Some("'; DROP TABLE x; --"), None)
                .is_empty()
        );
        assert!(t.rt.calls.lock().unwrap().is_empty());
    }
    #[test]
    fn test_evaluation_query_failure_uses_no_evaluation_and_never_echoes_stderr() {
        let t = Test::new();
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 1,
            stdout: vec![],
            stderr: b"mock-sensitive-stderr".to_vec(),
        }));
        assert!(
            t.ctx
                .jev_evaluations(Some("01a0f188-0c24-7860-ab46-6c424b1def08"), None)
                .is_empty()
        );
        assert_eq!(auto_quality(&job(json!({"status":"succeeded"})), None).0, 3);
        let diagnostic = t.rt.diagnostics.lock().unwrap().join("\n");
        assert_eq!(diagnostic, "auto evaluation unavailable: RuntimeError");
        assert!(!diagnostic.contains("mock-sensitive-stderr"));
    }
    #[test]
    fn test_backfill_filters_days_rated_and_live_jobs_and_reports_actual_sent_count() {
        let t = Test::new();
        let today = t.rt.now();
        for p in [
            json!({"id":"done","status":"succeeded","finished_at":today,"thread_id":"01a0f188-0c24-7860-ab46-6c424b1def08","duration_ms":2000}),
            json!({"id":"failed","status":"failed","finished_at":today,"last_error":"proxy timeout","duration_ms":1000}),
            json!({"id":"rated","status":"succeeded","finished_at":today,"gto":{"automatic":false}}),
            json!({"id":"live","status":"running"}),
            json!({"id":"old","status":"succeeded","created_at":"2026-09-23T12:00:00+09:00","finished_at":"2026-09-23T12:00:00+09:00"}),
        ] {
            t.put(job(p));
        }
        t.rt.replies.lock().unwrap().push_back(Ok(Reply{code:0,stdout:json!([{"session_path":"rollout-01a0f188-0c24-7860-ab46-6c424b1def08.jsonl","verdict":"completed"}]).to_string().into_bytes(),stderr:vec![]}));
        let (r, out, _) = t.cli(&["backfill", "--days", "7"]);
        assert_eq!(r.unwrap(), 0);
        assert_eq!(
            serde_json::from_str::<Value>(&out).unwrap(),
            json!({"days":7,"considered":3,"sent":2,"already_rated":1,"failed":0,"skipped":2})
        );
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls.iter().filter(|c| c.argv[0] == "docker").count(), 1);
        let mut qualities = calls
            .iter()
            .filter(|c| c.argv[0] != "docker")
            .map(|c| {
                serde_json::from_slice::<Value>(&c.input).unwrap()["result_quality"]
                    .as_i64()
                    .unwrap()
            })
            .collect::<Vec<_>>();
        qualities.sort();
        assert_eq!(qualities, [1, 4]);
        assert!(t.ctx.load("old").unwrap().get("gto").is_none());
        assert!(t.ctx.load("live").unwrap().get("gto").is_none());
    }
    #[test]
    fn test_backfill_ingest_failure_is_reported_and_can_be_retried() {
        let t = Test::new();
        t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        t.rt.replies.lock().unwrap().extend([
            Ok(Reply {
                code: 0,
                stdout: b"[]".to_vec(),
                stderr: vec![],
            }),
            Ok(Reply {
                code: 1,
                stdout: vec![],
                stderr: vec![],
            }),
        ]);
        let (r, out, _) = t.cli(&["backfill", "--days", "7"]);
        assert_eq!(r.unwrap(), 1);
        assert_eq!(serde_json::from_str::<Value>(&out).unwrap()["failed"], 1);
        assert!(
            t.ctx
                .load("20260929-000000-t")
                .unwrap()
                .get("gto")
                .is_none()
        );
    }
    #[test]
    fn test_auto_lookup_is_used_for_the_finished_job_session() {
        let t = Test::new();
        let sid = "01a0f188-0c24-7860-ab46-6c424b1def08";
        let mut j = t.put(job(
            json!({"status":"succeeded","thread_id":sid,"duration_ms":1000}),
        ));
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 0,
            stdout:
                json!([{"session_path":format!("rollout-time-{sid}.jsonl"),"verdict":"partial"}])
                    .to_string()
                    .into_bytes(),
            stderr: vec![],
        }));
        assert!(t.ctx.auto_rate(&mut j, None));
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls[0].argv[0], "docker");
        assert_eq!(
            serde_json::from_slice::<Value>(&calls[1].input).unwrap()["result_quality"],
            2
        );
    }
}
mod run_validation {
    use super::*;
    #[test]
    fn unattended_implementation_accepts_approval_and_readonly_cannot_escalate() {
        for (sandbox, approval, reviewer) in [
            ("danger-full-access", "on-request", "auto_review"),
            ("workspace-write", "on-request", "auto_review"),
            ("read-only", "never", "user"),
        ] {
            let args = codex_args(&job(json!({"codex":"codex.cmd","sandbox":sandbox,
            "resume_session":"12345678-1234-4321-9234-123456789abc"})));
            for value in [
                format!("approval_policy={approval}"),
                format!("approvals_reviewer={reviewer}"),
            ] {
                assert!(
                    args.windows(2)
                        .any(|pair| pair[0] == "-c" && pair[1] == value)
                );
            }
            let expected = if sandbox == "workspace-write" {
                "danger-full-access"
            } else {
                sandbox
            };
            assert!(args.windows(2).any(|pair| pair == ["-s", expected]));
            assert!(
                args.windows(2)
                    .any(|pair| pair == ["resume", "12345678-1234-4321-9234-123456789abc"])
            );
        }
    }
    #[test]
    fn write_jobs_store_permissions_and_owned_cleanup_instructions() {
        let t = Test::new();
        t.run(&["--sandbox", "danger-full-access"], b"implement feature")
            .unwrap();
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs.len(), 1);
        let j = &jobs[0];
        assert_eq!(j["approval_policy"], "on-request");
        assert_eq!(j["approvals_reviewer"], "auto_review");
        let prompt = fs::read_to_string(t.ctx.job_dir(s(j, "id")).join("prompt.md")).unwrap();
        assert!(prompt.starts_with("implement feature"));
        assert!(prompt.contains("git push"));
        assert!(prompt.contains("quarantine --id <所有ID> --reason policy"));
        assert!(prompt.contains("削除拒否だけをオーナー判断待ちにしない"));
    }
    #[test]
    fn test_args_have_no_cwd_flag() {
        let j = job(json!({"codex":"codex.CMD","add_dirs":["D:/x"],"network":true}));
        let args = codex_args(&j);
        // The final message is written by the supervisor from masked events.
        assert!(!args.iter().any(|a| a == "-o" || a.ends_with(".md")));
        assert_eq!(
            &args[..4],
            ["codex.CMD", "exec", "--json", "--skip-git-repo-check"]
        );
        assert_eq!(args.last().unwrap(), "-");
        assert!(!args.iter().any(|s| s == "-C"));
        for (k, v) in [
            ("-m", "gpt-6-luna"),
            ("-c", "model_reasoning_effort=max"),
            ("-s", "read-only"),
            ("--add-dir", "D:/x"),
        ] {
            let i = args.iter().position(|s| s == v).unwrap();
            assert_eq!(args[i - 1], k);
        }
    }
    #[test]
    fn test_mcp_profiles() {
        for (profile, disabled) in [
            ("minimal", vec!["godd-a", "headroom", "pencil", "node_repl"]),
            ("browser", vec!["godd-a", "headroom", "pencil"]),
            ("full", vec![]),
        ] {
            let args = codex_args(&job(json!({"codex":"codex.CMD","mcp_profile":profile})));
            let overrides = args
                .windows(2)
                .filter(|p| {
                    p[0] == "-c"
                        && p[1].starts_with("mcp_servers.")
                        && p[1].ends_with("enabled=false")
                })
                .map(|p| p[1].clone())
                .collect::<Vec<_>>();
            assert_eq!(
                overrides,
                disabled
                    .iter()
                    .map(|n| format!("mcp_servers.{n}.enabled=false"))
                    .collect::<Vec<_>>()
            );
        }
    }
    #[test]
    fn test_cargo_limit_is_scoped_to_job_environment() {
        let t = Test::new();
        // An inherited legacy shared target is migrated to the gate's per-workspace one.
        t.rt.env
            .lock()
            .unwrap()
            .insert("CARGO_TARGET_DIR".into(), cc_jobs::cache::SHARED.into());
        let env = t.ctx.job_environment();
        // CPU budget 32 split across the default 3 build slots.
        assert_eq!(env["CARGO_BUILD_JOBS"], "10");
        assert!(!env.contains_key("CARGO_INCREMENTAL"));
        assert_eq!(env["CARGO_PROFILE_DEV_INCREMENTAL"], "true");
        assert_eq!(env["CARGO_PROFILE_TEST_INCREMENTAL"], "false");
        assert!(!env.contains_key("CARGO_TARGET_DIR"));
        let installed = Path::new(cc_jobs::gate::SCCACHE).is_file();
        assert_eq!(
            env.get("RUSTC_WRAPPER").map(String::as_str),
            installed.then_some(cc_jobs::gate::SCCACHE)
        );
        assert_eq!(
            env.get("SCCACHE_SERVER_PORT").map(String::as_str),
            installed.then_some("42259")
        );
        t.rt.env
            .lock()
            .unwrap()
            .insert("CODEX_JOBS_CARGO_SLOTS".into(), "4".into());
        assert_eq!(t.ctx.job_environment()["CARGO_BUILD_JOBS"], "10");
        let env = t.ctx.job_environment();
        assert_eq!(
            env["CODEX_JOBS_REAL_CARGO"],
            "C:/Users/test/.cargo/bin/cargo.exe"
        );
        assert_eq!(
            env["PATH"].split(';').next().unwrap(),
            t.ctx.root.join("bin").to_str().unwrap()
        );
        assert_eq!(
            env["CODEX_JOBS_CARGO_GATE"],
            t.ctx.root.join("cargo_gate.exe").to_str().unwrap()
        );
        assert!(!t.rt.env().contains_key("CARGO_BUILD_JOBS"));
    }
    #[test]
    fn test_rejects_cmd_metacharacters() {
        let t = Test::new();
        for extra in [
            vec!["--add-dir", "D:/R&D"],
            vec!["--add-dir", "D:/p%OS%q"],
            vec!["--model", "a\"b"],
        ] {
            assert!(
                t.run(&extra, b"hi")
                    .unwrap_err()
                    .message
                    .contains("cmd.exe would misparse")
            );
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        }
        assert!(t.ctx.list_jobs(&HashSet::new()).is_empty());
    }
    #[test]
    fn test_rejects_network_outside_workspace_write() {
        let t = Test::new();
        assert!(
            t.run(&["--network"], b"hi")
                .unwrap_err()
                .message
                .contains("--network")
        );
        assert_eq!(
            t.run(&["--network", "--sandbox", "workspace-write"], b"hi")
                .unwrap(),
            0
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn test_rejects_non_utf8_prompt() {
        let t = Test::new();
        assert!(
            t.run(&[], &[0xff, 0xfe, 0x65, 0x65])
                .unwrap_err()
                .message
                .contains("not UTF-8")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn test_launch_key_returns_existing_job_instead_of_spawning_again() {
        let t = Test::new();
        let key = "ab".repeat(16);
        assert_eq!(t.run(&["--launch-key", &key], b"hi").unwrap(), 0);
        let first = t.ctx.list_jobs(&HashSet::new())[0].clone();
        assert_eq!(t.run(&["--launch-key", &key], b"hi").unwrap(), 0);
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs.len(), 1);
        assert_eq!(jobs[0]["id"], first["id"]);
        assert_eq!(first["launch_key"], key);
    }
    #[test]
    fn new_launch_key_and_terminal_receipt_never_read_terminal_history() {
        let t = Test::new();
        t.put(job(json!({"id":"history","status":"succeeded"})));
        *t.rt.load_hook.lock().unwrap() = Some(Box::new(|path, _| {
            assert!(
                !path.ends_with("history/job.json"),
                "receipt must not scan history"
            );
        }));
        let key = "ab".repeat(16);
        t.run(&["--launch-key", &key], b"hi").unwrap();
        let receipt = t
            .ctx
            .read_json(&t.ctx.jobs.join(format!(".launch-{key}.json")))
            .unwrap();
        let id = s(&receipt, "id");
        let mut existing = t.ctx.load(id).unwrap();
        existing["status"] = json!("succeeded");
        t.ctx.save(&existing).unwrap();
        t.run(&["--launch-key", &key], b"hi").unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
    #[test]
    fn legacy_launch_lock_without_receipt_adopts_completed_job() {
        let t = Test::new();
        let key = "ab".repeat(16);
        t.put(job(
            json!({"id":"legacy-key","status":"succeeded","launch_key":key}),
        ));
        fs::write(t.ctx.jobs.join(format!(".launch-{key}.lock")), "").unwrap();
        fs::write(
            t.ctx.jobs.join(format!(".launch-{key}.pending")),
            "interrupted receipt",
        )
        .unwrap();
        t.run(&["--launch-key", &key], b"retry").unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
        assert_eq!(
            t.ctx
                .read_json(&t.ctx.jobs.join(format!(".launch-{key}.json")))
                .unwrap()["id"],
            "legacy-key"
        );
    }
    #[test]
    fn concurrent_same_launch_key_returns_one_receipt_and_one_supervisor() {
        let t = Test::new();
        let key = "ab".repeat(16);
        let barrier = std::sync::Barrier::new(8);
        std::thread::scope(|scope| {
            for _ in 0..8 {
                scope.spawn(|| {
                    barrier.wait();
                    assert_eq!(t.run(&["--launch-key", &key], b"once").unwrap(), 0);
                });
            }
        });
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs.len(), 1);
        assert_eq!(
            t.ctx
                .read_json(&t.ctx.jobs.join(format!(".launch-{key}.json")))
                .unwrap()["id"],
            jobs[0]["id"]
        );
    }
    #[test]
    fn test_launch_key_stays_idempotent_after_the_original_job_finishes() {
        let t = Test::new();
        let key = "cd".repeat(16);
        t.run(&["--launch-key", &key], b"hi").unwrap();
        let j = t.ctx.list_jobs(&HashSet::new())[0].clone();
        t.ctx
            .save(&patch(
                j,
                json!({"status":"succeeded","duration_ms":1000,"finished_at":t.rt.now()}),
            ))
            .unwrap();
        t.run(&["--launch-key", &key], b"hi").unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
    }
    #[test]
    fn test_invalid_launch_key_is_rejected() {
        let t = Test::new();
        assert!(
            t.run(&["--launch-key", "../invalid"], b"hi")
                .unwrap_err()
                .message
                .contains("--launch-key")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }

    #[test]
    fn resume_refuses_an_active_writer_even_with_different_or_missing_launch_keys() {
        for field in ["thread_id", "resume_session"] {
            for status in ["queued", "starting", "running"] {
                let t = Test::new();
                let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
                let mut active = json!({"id":"active-writer","status":status});
                active[field] = json!(session);
                t.put(active);
                drop(t.ctx.queue_lock().unwrap());
                for key in [None, Some("abcdabcdabcdabcdabcdabcdabcdabcd")] {
                    let mut args = vec!["--resume-session", session];
                    if let Some(key) = key {
                        args.extend(["--launch-key", key]);
                    }
                    assert!(
                        t.run(&args, b"resume")
                            .unwrap_err()
                            .message
                            .contains("already has active job active-writer")
                    );
                }
                assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
                assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
            }
        }
    }

    #[test]
    fn resume_refuses_a_lost_writer_when_its_identity_is_inaccessible_or_missing() {
        for missing_created in [false, true] {
            let t = Test::new();
            let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
            let active = json!({"id":"active-writer","status":"lost","thread_id":session,
                "codex_pid":321,"codex_pid_created":if missing_created {None} else {Some(456)}});
            t.rt.procs.lock().unwrap().insert(321, 456);
            if !missing_created {
                t.rt.proc_probe_errors.lock().unwrap().insert(321, 5);
            }
            t.put(active.clone());
            {
                let _gate = t.ctx.queue_lock().unwrap();
                t.ctx.track_queued_job("active-writer").unwrap();
            }
            assert!(
                t.run(&["--resume-session", session], b"duplicate")
                    .unwrap_err()
                    .message
                    .contains("already has active job active-writer")
            );
            assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
            assert_eq!(t.ctx.load("active-writer").unwrap(), active);
        }
    }

    #[test]
    fn resume_waits_for_queued_predecessor_and_same_key_adopts_without_relaunching() {
        let t = Test::new();
        let session = "01a0f188-0c24-7860-ab46-6c424b1def08";
        let key = "abcdabcdabcdabcdabcdabcdabcdabcd";
        t.run(
            &["--resume-session", session, "--launch-key", key],
            b"resume",
        )
        .unwrap();
        t.run(
            &["--resume-session", session, "--launch-key", key],
            b"resume",
        )
        .unwrap();
        assert!(
            t.run(&["--resume-session", session], b"duplicate")
                .unwrap_err()
                .message
                .contains("already has active job")
        );
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        let job = t.ctx.list_jobs(&HashSet::new())[0].clone();
        t.ctx
            .save(&patch(job, json!({"status":"succeeded"})))
            .unwrap();
        t.run(&["--resume-session", session], b"next turn").unwrap();
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 2);
    }

    #[test]
    fn concurrent_resume_launches_register_only_one_writer() {
        let t = Test::new();
        let barrier = std::sync::Barrier::new(2);
        std::thread::scope(|scope| {
            let launch = || {
                barrier.wait();
                t.run(
                    &["--resume-session", "01a0f188-0c24-7860-ab46-6c424b1def08"],
                    b"resume",
                )
            };
            let first = scope.spawn(launch);
            let second = scope.spawn(launch);
            let results = [first.join().unwrap(), second.join().unwrap()];
            assert_eq!(results.iter().filter(|r| r.is_ok()).count(), 1);
            assert!(
                results
                    .iter()
                    .find_map(|r| r.as_ref().err())
                    .unwrap()
                    .message
                    .contains("already has active job")
            );
        });
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
        assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
    }
}
mod cargo_gate_tests {
    use super::*;
    use cc_jobs::gate::{
        HELD_TARGET, INJECTED_TARGET, clamp_jobs, default_target, jobs_limit, prepare_target,
        target_lock,
    };
    use std::process::{Child, Stdio};
    #[test]
    fn resolved_target_is_passed_to_cargo_without_poisoning_the_rust_cache() {
        let a = |v: &[&str]| v.iter().map(|s| s.to_string()).collect::<Vec<_>>();
        let mut env = BTreeMap::from([
            ("cargo_target_dir".into(), r"D:\target-a".into()),
            ("CARGO_BUILD_TARGET_DIR".into(), r"D:\target-b".into()),
            (INJECTED_TARGET.into(), r"D:\target-a".into()),
            ("CARGO_FEATURE_FAST".into(), "1".into()),
        ]);
        let args = cc_jobs::gate::target_arguments(
            &a(&["test", "--locked", "--", "--nocapture"]),
            &mut env,
            Path::new(r"D:\target-a"),
        );
        assert_eq!(
            args,
            a(&[
                "test",
                "--locked",
                "--target-dir",
                r"D:\target-a",
                "--",
                "--nocapture"
            ])
        );
        assert_eq!(
            env,
            BTreeMap::from([
                ("CARGO_FEATURE_FAST".into(), "1".into()),
                (
                    cc_jobs::gate::FORWARDED_TARGET.into(),
                    r"D:\target-a".into()
                ),
            ])
        );
        let explicit = a(&["nextest", "run", r"--target-dir=D:\chosen"]);
        assert_eq!(
            cc_jobs::gate::target_arguments(&explicit, &mut env, Path::new(r"D:\chosen")),
            explicit
        );
        env.insert("CARGO_TARGET_DIR".into(), r"D:\custom".into());
        let custom = a(&["custom", "run"]);
        assert_eq!(
            cc_jobs::gate::target_arguments(&custom, &mut env, Path::new(r"D:\custom")),
            custom
        );
        assert_eq!(env["CARGO_TARGET_DIR"], r"D:\custom");
        for command in ["show-config", "self", "store", "help"] {
            let args = a(&["nextest", command]);
            let before = env.clone();
            assert!(!cc_jobs::gate::build_like(&args));
            assert_eq!(
                cc_jobs::gate::target_arguments(&args, &mut env, Path::new(r"D:\custom")),
                args
            );
            assert_eq!(env, before);
        }
        let args =
            cc_jobs::gate::target_arguments(&a(&["build"]), &mut env, Path::new(r"D:\custom"));
        assert_eq!(args, a(&["build", "--target-dir", r"D:\custom"]));
        assert!(!env.contains_key("CARGO_TARGET_DIR"));
        env.insert("CODEX_JOBS_CARGO_LOCK_HELD".into(), "1".into());
        assert_eq!(
            prepare_target(&a(&["build"]), &mut env, |_| panic!(
                "explicit nested target needs no metadata"
            )),
            Some(PathBuf::from(r"D:\custom"))
        );
    }
    /// A gated build of `target` (under `tmp`) that runs 600 ms once `ready`
    /// builds have been admitted.
    fn build(tmp: &Path, slots: Option<usize>, target: &str, ready: usize) -> Child {
        let mut c = hidden_command(env!("CARGO_BIN_EXE_cargo_gate"));
        c.args(["build"])
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env("CODEX_JOBS_CARGO_LOCK_DIR", tmp)
            .env("CARGO_TARGET_DIR", tmp.join(target))
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
            .env("CC_JOBS_FIXTURE_DIR", tmp)
            .env("CC_JOBS_FIXTURE_MODE", "cargo")
            .env("CC_JOBS_FIXTURE_READY", ready.to_string())
            // Never publish wait markers into a real job that runs these tests.
            .env_remove("CC_JOB_ID")
            .env_remove("CODEX_JOB_ID")
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        match slots {
            Some(n) => c.env("CODEX_JOBS_CARGO_SLOTS", n.to_string()),
            None => c.env_remove("CODEX_JOBS_CARGO_SLOTS"),
        };
        c.spawn().unwrap()
    }
    fn interval(p: Child) -> (f64, f64) {
        let out = p.wait_with_output().unwrap();
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        let text = String::from_utf8(out.stdout).unwrap();
        let lines = text.lines().collect::<Vec<_>>();
        assert_eq!(lines.len(), 2);
        assert!(lines[0].starts_with("START "));
        assert!(lines[1].starts_with("END "));
        (
            lines[0][6..].parse().unwrap(),
            lines[1][4..].parse().unwrap(),
        )
    }
    /// Intervals in spawn order for builds of the given targets.
    fn timed(slots: Option<usize>, targets: &[&str], ready: usize) -> Vec<(f64, f64)> {
        let tmp = Temp::new();
        cargo_test_policy(&tmp.0, slots.unwrap_or(4));
        let procs: Vec<_> = targets
            .iter()
            .map(|t| build(&tmp.0, slots, t, ready))
            .collect();
        procs.into_iter().map(interval).collect()
    }
    fn intervals(slots: usize, count: usize) -> Vec<(f64, f64)> {
        let targets: Vec<_> = (0..count).map(|i| format!("target-{i}")).collect();
        let targets: Vec<_> = targets.iter().map(String::as_str).collect();
        let mut result = timed(Some(slots), &targets, slots);
        result.sort_by(|a, b| a.0.total_cmp(&b.0));
        result
    }
    fn overlap(a: (f64, f64), b: (f64, f64)) -> bool {
        a.0 < b.1 - 0.02 && b.0 < a.1 - 0.02
    }
    fn max_overlap(intervals: &[(f64, f64)]) -> usize {
        let mut ev = intervals
            .iter()
            .flat_map(|(s, e)| [(*s, 1_i32), (*e, -1)])
            .collect::<Vec<_>>();
        ev.sort_by(|a, b| a.0.total_cmp(&b.0).then(a.1.cmp(&b.1)));
        let (mut cur, mut best) = (0, 0);
        for (_, d) in ev {
            cur += d;
            best = best.max(cur);
        }
        best as usize
    }
    #[test]
    fn test_one_slot_serializes_build_like_commands() {
        let v = intervals(1, 2);
        assert!(v[1].0 >= v[0].1 - 0.02);
    }
    #[test]
    fn test_slots_limit_concurrent_build_like_commands() {
        let v = intervals(2, 3);
        assert!(max_overlap(&v) <= 2);
        assert_eq!(max_overlap(&v[..2]), 2);
    }
    #[test]
    fn same_target_builds_never_overlap_even_with_free_slots() {
        let v = timed(Some(3), &["shared", "shared", "shared"], 1);
        assert_eq!(max_overlap(&v), 1, "{v:?}");
    }
    #[test]
    fn build_waiting_for_its_target_does_not_hold_a_slot() {
        // Two slots: one build of `a` waits for the other, so the `b` build must
        // get the second slot and run alongside the first `a` build. If the
        // waiter held a slot, the `b` build could not start and the fixture
        // would report stalled admission.
        let v = timed(Some(2), &["a", "a", "b"], 2);
        assert!(!overlap(v[0], v[1]), "{v:?}");
        assert!(overlap(v[2], v[0]) || overlap(v[2], v[1]), "{v:?}");
    }
    #[test]
    fn live_policy_can_admit_more_than_three_distinct_targets() {
        let capacity = std::thread::available_parallelism().unwrap().get().min(4);
        let v = timed(None, &["t0", "t1", "t2", "t3", "t4"], capacity);
        assert_eq!(max_overlap(&v), capacity, "{v:?}");
    }
    #[test]
    fn cpu_budget_is_split_across_slots_and_explicit_jobs_are_clamped() {
        assert_eq!(jobs_limit(32, 3), 10);
        assert_eq!(jobs_limit(32, 4), 8);
        assert_eq!(jobs_limit(2, 3), 1);
        assert_eq!(jobs_limit(32, 1), 10);
        let a = |v: &[&str]| v.iter().map(|s| s.to_string()).collect::<Vec<_>>();
        assert_eq!(
            clamp_jobs(
                &a(&[
                    "build", "-j", "64", "--jobs=4", "-j32", "-j=0", "--jobs", "x"
                ]),
                10
            ),
            a(&[
                "build", "-j", "10", "--jobs=4", "-j10", "-j10", "--jobs", "10"
            ])
        );
        assert_eq!(
            clamp_jobs(&a(&["test", "--", "-j", "64", "--jobs=64"]), 10),
            a(&["test", "--", "-j", "64", "--jobs=64"])
        );
        // nextest: -j is the test thread count; --build-jobs is Cargo's.
        assert_eq!(
            clamp_jobs(
                &a(&[
                    "+nightly",
                    "nextest",
                    "run",
                    "-j",
                    "64",
                    "--build-jobs",
                    "64",
                    "--build-jobs=2"
                ]),
                10
            ),
            a(&[
                "+nightly",
                "nextest",
                "run",
                "-j",
                "10",
                "--build-jobs",
                "10",
                "--build-jobs=2"
            ])
        );
        let tmp = Temp::new();
        cargo_test_policy(&tmp.0, 1);
        let target = tmp.0.join("t");
        let out = hidden_command(env!("CARGO_BIN_EXE_cargo_gate"))
            .args(["--echo-build", "build", "-j", "4096", "--", "-j", "4096"])
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env("CODEX_JOBS_CARGO_LOCK_DIR", &tmp.0)
            .env("CODEX_JOBS_CARGO_SLOTS", "1")
            .env("CARGO_TARGET_DIR", &target)
            .env("CARGO_BUILD_JOBS", "4096")
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
            .output()
            .unwrap();
        assert!(out.status.success());
        let limit = jobs_limit(std::thread::available_parallelism().unwrap().get(), 1);
        assert_eq!(
            String::from_utf8(out.stdout).unwrap().trim(),
            format!(
                "{limit}|{}|build -j {limit} -- -j 4096",
                cc_jobs::cache::resolved(&target).unwrap()
            )
        );
    }
    #[test]
    fn explicit_targets_are_kept_and_legacy_or_injected_targets_migrate() {
        let a = |v: &[&str]| v.iter().map(|s| s.to_string()).collect::<Vec<_>>();
        let root = PathBuf::from(r"D:\work\repo-wt");
        let meta = |root: &Path| {
            let root = root.to_path_buf();
            move |_: &BTreeMap<String, String>| Some((root.clone(), root.join("target")))
        };
        let never = |_: &BTreeMap<String, String>| -> Option<(PathBuf, PathBuf)> {
            panic!("explicit target must not query Cargo")
        };
        let legacy = BTreeMap::from([(
            "CARGO_TARGET_DIR".to_string(),
            cc_jobs::cache::SHARED.to_string(),
        )]);
        // Explicit argument and user environment are preserved untouched.
        let mut env = legacy.clone();
        assert_eq!(
            prepare_target(&a(&["build", r"--target-dir=D:\mine"]), &mut env, never),
            Some(PathBuf::from(r"D:\mine"))
        );
        assert_eq!(env, legacy);
        let mut env = BTreeMap::from([("cargo_target_dir".into(), r"D:\user".into())]);
        assert_eq!(
            prepare_target(&a(&["build"]), &mut env, never),
            Some(PathBuf::from(r"D:\user"))
        );
        assert_eq!(env["cargo_target_dir"], r"D:\user");
        // The inherited legacy cache migrates to this workspace's own target.
        let mut env = legacy.clone();
        let dir = prepare_target(&a(&["build"]), &mut env, meta(&root)).unwrap();
        assert_eq!(dir, default_target(&root));
        assert_eq!(dir.file_name().unwrap(), "target");
        assert_eq!(env["CARGO_TARGET_DIR"], dir.to_string_lossy());
        assert_eq!(env[INJECTED_TARGET], dir.to_string_lossy());
        assert!(!cc_jobs::cache::guard_needed(&a(&["build"]), &env));
        // A nested gate recomputes a target injected by its outer gate.
        let other = PathBuf::from(r"D:\work\other");
        assert_eq!(
            prepare_target(&a(&["build"]), &mut env, meta(&other)),
            Some(default_target(&other))
        );
        // Cargo configuration (build.target-dir) is kept; the legacy variable is dropped.
        let mut env = legacy.clone();
        let configured = PathBuf::from(r"D:\configured");
        assert_eq!(
            prepare_target(&a(&["build"]), &mut env, |_| Some((
                root.clone(),
                configured.clone()
            ))),
            Some(PathBuf::from(r"D:\configured"))
        );
        assert!(!env.contains_key("CARGO_TARGET_DIR"));
        let mut env = legacy.clone();
        env.insert("CARGO_BUILD_TARGET_DIR".into(), r"D:\build-env".into());
        assert_eq!(
            prepare_target(&a(&["build"]), &mut env, never),
            Some(PathBuf::from(r"D:\build-env"))
        );
        assert!(!env.contains_key("CARGO_TARGET_DIR"));
        // Unknown layout keeps the legacy cache, which stays budget-guarded.
        let mut env = legacy.clone();
        assert_eq!(
            prepare_target(&a(&["build"]), &mut env, |_| None),
            Some(PathBuf::from(cc_jobs::cache::SHARED))
        );
        assert!(cc_jobs::cache::guard_needed(&a(&["build"]), &env));
        // An explicit legacy target keeps its ownership-pruning guard.
        assert!(cc_jobs::cache::guard_needed(
            &a(&["build", "--target-dir", cc_jobs::cache::SHARED]),
            &BTreeMap::new()
        ));
        // Stable per workspace, independent of spelling; distinct per worktree.
        assert_eq!(
            default_target(&root),
            default_target(Path::new(r"d:\WORK\.\repo-wt\"))
        );
        assert_ne!(default_target(&root), default_target(&other));
        assert!(
            default_target(&root)
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("target")
        );
    }
    fn nested(tmp: &Path, held: &str, target: &Path) -> Child {
        hidden_command(env!("CARGO_BIN_EXE_cargo_gate"))
            .args(["--echo-build", "build"])
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env("CODEX_JOBS_CARGO_LOCK_DIR", tmp)
            .env("CODEX_JOBS_CARGO_SLOTS", "1")
            .env("CODEX_JOBS_CARGO_LOCK_HELD", "1")
            .env(HELD_TARGET, held)
            .env("CARGO_TARGET_DIR", target)
            .env_remove("CC_JOB_ID")
            .env_remove("CODEX_JOB_ID")
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .unwrap()
    }
    fn finished_within(child: &mut Child, limit: Duration) -> bool {
        let deadline = Instant::now() + limit;
        while Instant::now() < deadline {
            if child.try_wait().unwrap().is_some() {
                return true;
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        false
    }
    #[test]
    fn nested_gate_reuses_outer_slot_and_target_but_locks_an_independent_target() {
        let tmp = Temp::new();
        let (outer, other) = (tmp.0.join("outer"), tmp.0.join("other"));
        fs::create_dir_all(&outer).unwrap();
        fs::create_dir_all(&other).unwrap();
        let outer_key = cc_jobs::cache::resolved(&outer).unwrap();
        let other_key = cc_jobs::cache::resolved(&other).unwrap();
        // The outer gate holds the only slot and its target.
        let _slot = FileLock::try_acquire(&tmp.0.join("cargo-build.lock.0")).unwrap();
        let _outer = FileLock::try_acquire(&target_lock(&tmp.0, &outer_key)).unwrap();
        let mut same = nested(&tmp.0, &outer_key, &outer);
        assert!(finished_within(&mut same, Duration::from_secs(30)));
        assert!(same.wait_with_output().unwrap().status.success());
        // An independent target inherited by nested Cargo must not bypass its lock.
        let busy = FileLock::try_acquire(&target_lock(&tmp.0, &other_key)).unwrap();
        let mut independent = nested(&tmp.0, &outer_key, &other);
        assert!(finished_within(&mut independent, Duration::from_secs(30)));
        assert!(!independent.wait_with_output().unwrap().status.success());
        drop(busy);
        let mut independent = nested(&tmp.0, &outer_key, &other);
        assert!(finished_within(&mut independent, Duration::from_secs(30)));
        let out = independent.wait_with_output().unwrap();
        assert!(out.status.success());
        let line = String::from_utf8(out.stdout).unwrap();
        assert_eq!(line.trim().split('|').nth(1), Some(other_key.as_str()));
    }
}
#[cfg(windows)]
mod process_boundary_tests {
    use super::*;
    use std::process::Child;
    struct FixtureChild {
        child: Child,
        created: u64,
    }
    impl FixtureChild {
        fn new(child: Child) -> Self {
            let created = proc_created(child.id()).unwrap();
            Self { child, created }
        }
    }
    impl std::ops::Deref for FixtureChild {
        type Target = Child;
        fn deref(&self) -> &Child {
            &self.child
        }
    }
    impl std::ops::DerefMut for FixtureChild {
        fn deref_mut(&mut self) -> &mut Child {
            &mut self.child
        }
    }
    impl Drop for FixtureChild {
        fn drop(&mut self) {
            if self.child.try_wait().ok().flatten().is_none()
                && proc_created(self.child.id()) == Some(self.created)
            {
                let _ = System::default().kill_tree(self.child.id());
            }
            let _ = self.child.wait();
        }
    }
    fn wait_file(path: &Path) {
        let until = Instant::now() + Duration::from_secs(30);
        while !path.exists() {
            assert!(Instant::now() < until, "{}", path.display());
            std::thread::sleep(Duration::from_millis(5));
        }
    }
    #[test]
    fn idle_startup_mcp_is_paused_but_its_later_worker_must_finish_first() {
        let tmp = Temp::new();
        for name in ["codex.exe", "worker.exe", "mcp.exe"] {
            fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), tmp.0.join(name)).unwrap();
        }
        let mut child = FixtureChild::new(
            hidden_command(&tmp.0.join("codex.exe").to_string_lossy())
                .arg("--controller-mcp")
                .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
                .env("CC_JOBS_FIXTURE_RESOURCE_BARRIER", "1")
                .spawn()
                .unwrap(),
        );
        wait_file(&tmp.0.join("worker.ready"));
        let baseline = fs::read_to_string(tmp.0.join("first-tool.filetime"))
            .unwrap()
            .parse()
            .unwrap();
        let rt = System::default();
        let created = proc_created(child.id()).unwrap();
        assert!(
            rt.pause_idle_agent(child.id(), created, Some(baseline), None)
                .unwrap()
                .is_none()
        );
        fs::write(tmp.0.join("worker.release"), "release").unwrap();
        wait_file(&tmp.0.join("worker.done"));
        wait_file(&tmp.0.join("heartbeat"));
        assert!(
            rt.pause_idle_agent(child.id(), created, None, None)
                .unwrap()
                .is_none()
        );
        let mut paused = rt
            .pause_idle_agent(
                child.id(),
                created,
                Some(baseline),
                Some(&tmp.0.join("yield-paused.json")),
            )
            .unwrap()
            .unwrap();
        let ids = paused.processes();
        assert!(ids.len() >= 2);
        paused.stop().unwrap();
        assert!(child.wait().unwrap().success());
        assert!(ids.iter().all(|(pid, _)| proc_created(*pid).is_none()));
    }
    #[test]
    fn a_dead_pausing_supervisor_is_recovered_without_abandoning_the_agent() {
        let tmp = Temp::new();
        for name in ["codex.exe", "worker.exe"] {
            fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), tmp.0.join(name)).unwrap();
        }
        let mut child = FixtureChild::new(
            hidden_command(&tmp.0.join("codex.exe").to_string_lossy())
                .arg("--controller")
                .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
                .spawn()
                .unwrap(),
        );
        wait_file(&tmp.0.join("worker.done"));
        wait_file(&tmp.0.join("heartbeat"));
        let created = proc_created(child.id()).unwrap();
        let mut holder = FixtureChild::new(
            hidden_command(env!("CARGO_BIN_EXE_cc_jobs_fixture"))
                .args([
                    "--freeze-controller",
                    &child.id().to_string(),
                    &created.to_string(),
                ])
                .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
                .spawn()
                .unwrap(),
        );
        wait_file(&tmp.0.join("freeze.ready"));
        let heartbeat = fs::read(tmp.0.join("heartbeat")).unwrap();
        holder.kill().unwrap();
        holder.wait().unwrap();
        std::thread::sleep(Duration::from_millis(70));
        assert_eq!(fs::read(tmp.0.join("heartbeat")).unwrap(), heartbeat);
        let rt = System::default();
        rt.recover_paused(&tmp.0.join("yield-paused.json")).unwrap();
        std::thread::sleep(Duration::from_millis(70));
        assert_ne!(fs::read(tmp.0.join("heartbeat")).unwrap(), heartbeat);
        let mut paused = rt
            .pause_idle_agent(child.id(), created, None, None)
            .unwrap()
            .unwrap();
        paused.stop().unwrap();
        child.wait().unwrap();
    }
    #[test]
    fn real_workers_finish_and_return_cargo_and_civ6_before_controllers_can_stop() {
        let tmp = Temp::new();
        for name in ["codex.exe", "worker.exe"] {
            fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), tmp.0.join(name)).unwrap();
        }
        let mut child = FixtureChild::new(
            hidden_command(&tmp.0.join("codex.exe").to_string_lossy())
                .arg("--controller")
                .env("CC_JOBS_FIXTURE_DIR", &tmp.0)
                .env("CC_JOBS_FIXTURE_RESOURCE_BARRIER", "1")
                .spawn()
                .unwrap(),
        );
        let wait_for = |path: &Path| {
            let deadline = Instant::now() + Duration::from_secs(30);
            while !path.exists() {
                assert!(Instant::now() < deadline);
                std::thread::sleep(Duration::from_millis(5));
            }
        };
        wait_for(&tmp.0.join("worker.ready"));
        let rt = System::default();
        let created = proc_created(child.id()).unwrap();
        assert!(
            rt.pause_idle_agent(child.id(), created, None, None)
                .unwrap()
                .is_none()
        );
        assert!(FileLock::try_acquire(&tmp.0.join("cargo-build.lock.0")).is_err());
        assert!(tmp.0.join("civ6.lock").exists());
        fs::write(tmp.0.join("worker.release"), "release").unwrap();
        wait_for(&tmp.0.join("worker.done"));
        wait_for(&tmp.0.join("heartbeat"));
        let paused = rt
            .pause_idle_agent(child.id(), created, None, None)
            .unwrap()
            .unwrap();
        let heartbeat = fs::read(tmp.0.join("heartbeat")).unwrap();
        std::thread::sleep(Duration::from_millis(100));
        assert_eq!(fs::read(tmp.0.join("heartbeat")).unwrap(), heartbeat);
        assert!(FileLock::try_acquire(&tmp.0.join("cargo-build.lock.0")).is_ok());
        assert!(!tmp.0.join("civ6.lock").exists());
        drop(paused);
        let resume_deadline = Instant::now() + Duration::from_secs(5);
        loop {
            let current = fs::read(tmp.0.join("heartbeat")).unwrap();
            if !current.is_empty() && current != heartbeat {
                break;
            }
            assert!(
                Instant::now() < resume_deadline,
                "resumed controller did not advance its heartbeat"
            );
            std::thread::sleep(Duration::from_millis(10));
        }
        assert!(
            rt.pause_idle_agent(child.id(), created + 1, None, None)
                .is_err()
        );
        let mut paused = rt
            .pause_idle_agent(child.id(), created, None, None)
            .unwrap()
            .unwrap();
        paused.stop().unwrap();
        assert!(child.wait().unwrap().success());
        assert!(proc_created(child.id()).is_none());
    }
}
mod task_notifier {
    use super::*;
    const WEBHOOK: &str = "https://discord.com/api/webhooks/123456/sentinel-secret-token";
    fn running(t: &Test) -> Value {
        t.put(job(json!({"status":"running","activity":"said: PROMPT_SECRET https://site.test/x?token=private","started_at":t.rt.now()})))
    }
    fn flag(c: &Call, name: &str) -> String {
        let prefix = format!("--{name}=");
        let v = c
            .argv
            .iter()
            .filter_map(|s| s.strip_prefix(&prefix))
            .collect::<Vec<_>>();
        assert_eq!(v.len(), 1);
        v[0].into()
    }
    fn notifier(t: &Test) -> TaskProgress {
        TaskProgress::new(
            Some("D:/ao/godd-ao.exe".into()),
            t.ctx.jobs.clone(),
            t.rt.clone(),
        )
    }
    fn wait_calls(t: &Test, n: usize) {
        // The fake clock verifies throttling; this bound only covers worker scheduling
        // and fixture file IO on the two-core Windows CI runner.
        let until = Instant::now() + Duration::from_secs(15);
        while t.rt.calls.lock().unwrap().len() < n {
            assert!(
                Instant::now() < until,
                "notification timeout: expected {n}, got {}",
                t.rt.calls.lock().unwrap().len()
            );
            std::thread::sleep(Duration::from_millis(1));
        }
    }
    #[test]
    fn real_notifier_receipt_syncs_the_job_card_with_remaining_deadline() {
        let t = Test::new();
        let j = running(&t);
        t.rt.replies.lock().unwrap().push_back(Ok(Reply {
            code: 0,
            stdout: br#"{"ok":true,"thread_id":"123","starter_message_id":"123"}"#.to_vec(),
            stderr: vec![],
        }));
        assert_eq!(
            ao_notify("godd-ao.exe", "start", &j, &t.ctx.jobs, t.rt.as_ref(), 135.)["ok"],
            true
        );
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(calls.len(), 2);
        assert!(calls[0].input.is_empty());
        assert_eq!(&calls[1].argv[1..], ["sync-job-card", "--job", s(&j, "id")]);
        assert!(calls[1].timeout <= calls[0].timeout);
    }
    #[test]
    fn test_source_detail_is_bounded_for_start_update_and_finish() {
        let t = Test::new();
        let mut j = running(&t);
        fs::write(
            t.ctx.job_dir(s(&j, "id")).join("prompt.md"),
            "地図の表示を実装する。".repeat(100),
        )
        .unwrap();
        j = patch(
            j,
            json!({"name":"Map proof","activity":"running: build","activity_detail":"find relevant code ".repeat(50),"last_message":"地図の表示と検証を完了。".repeat(150),"status":"succeeded"}),
        );
        for action in ["start", "update", "finish"] {
            assert_eq!(
                ao_notify(
                    "godd-ao.exe",
                    action,
                    &j,
                    &t.ctx.jobs,
                    t.rt.as_ref(),
                    t.rt.mono() + 35.
                )["ok"],
                true
            );
        }
        let calls = t.rt.calls.lock().unwrap();
        let d = calls
            .iter()
            .map(|c| String::from_utf8(c.input.clone()).unwrap())
            .collect::<Vec<_>>();
        assert!(d[0].is_empty()); // A nonempty detail lets AO replace the supplied title.
        assert_eq!(flag(&calls[0], "title"), "【work】地図の表示を実装する");
        assert!(d[1].starts_with("find relevant code"));
        assert!(d[1].chars().count() > 500);
        assert!(d[2].starts_with("地図の表示と検証を完了。"));
        assert!(d[2].chars().count() > 1500);
        assert!(
            calls
                .iter()
                .all(|c| c.argv.iter().any(|s| s == "--detail-stdin")
                    && !c.argv.iter().any(|s| s.starts_with("--detail=")))
        );
        assert_eq!(flag(&calls[1], "activity"), "running command");
        assert_eq!(flag(&calls[2], "result"), "完了");
    }
    #[test]
    fn test_free_text_flags_preserve_leading_hyphens_and_original_timeout() {
        let t = Test::new();
        let j = patch(
            running(&t),
            json!({"name":"--x","activity":"thinking","activity_detail":"--x","last_message":"- 修正完了","status":"succeeded"}),
        );
        for a in ["start", "update", "finish"] {
            assert_eq!(
                ao_notify("godd-ao.exe", a, &j, &t.ctx.jobs, t.rt.as_ref(), 135.)["ok"],
                true
            );
        }
        let c = t.rt.calls.lock().unwrap();
        assert!(flag(&c[0], "title").starts_with("【work】"));
        assert_eq!(c[1].input, b"--x");
        assert_eq!(String::from_utf8(c[2].input.clone()).unwrap(), "- 修正完了");
        assert!(c.iter().all(|c| c.timeout > 0.
            && c.timeout <= 35.
            && c.argv.iter().any(|s| s.starts_with("--deadline-ms="))));
        assert_eq!(NOTIFY_SHUTDOWN_CAP_S, 300.);
    }
    #[test]
    fn test_paths_urls_and_boundary_spanning_secret_reach_only_rust_sanitizer() {
        let t = Test::new();
        let mut j = running(&t);
        let token = "sentinel-credential-tail";
        t.rt.env
            .lock()
            .unwrap()
            .insert("GODD_DISCORD_BOT_TOKEN".into(), token.into());
        for text in [
            r"C:\Users\Alice\秘密\設計.md",
            r"\\office\share\plan.txt",
            "/home/alice/private.txt",
            "/Users/alice/private.txt",
            "~/private.txt",
            "private.test",
            "alice@private.test",
            "alice@intranet",
            "192.168.1.10",
            "README.md",
            "https://example.test/docs/setup",
            "作業 [link](relative/path)",
            "確認 se\u{200b}ntinel-credential-tail 完了",
            "確認 ｓｅｎｔｉｎｅｌ－ｃｒｅｄｅｎｔｉａｌ－ｔａｉｌ 完了",
        ] {
            j["activity_detail"] = json!(text);
            ao_notify(
                "godd-ao.exe",
                "update",
                &j,
                &t.ctx.jobs,
                t.rt.as_ref(),
                135.,
            );
            assert_eq!(
                t.rt.calls.lock().unwrap().last().unwrap().input,
                text.as_bytes()
            );
        }
        fs::write(
            t.ctx.job_dir(s(&j, "id")).join("prompt.md"),
            format!("{}{token}", "a".repeat(795)),
        )
        .unwrap();
        j = patch(
            j,
            json!({"name":"作業","activity_detail":format!("{}{token}","b".repeat(497)),"last_message":format!("{}{token}","c".repeat(1497)),"status":"succeeded"}),
        );
        let before = t.rt.calls.lock().unwrap().len();
        for a in ["start", "update", "finish"] {
            ao_notify("godd-ao.exe", a, &j, &t.ctx.jobs, t.rt.as_ref(), 135.);
        }
        for c in &t.rt.calls.lock().unwrap()[before..] {
            if c.argv[3] == "start" {
                assert!(c.input.is_empty());
            } else {
                assert!(String::from_utf8_lossy(&c.input).contains(token));
            }
            assert!(!format!("{:?}", c.argv).contains(token));
        }
        apply_event(
            &mut j,
            &json!({"type":"item.completed","item":{"type":"agent_message","text":format!("{}{token}","z".repeat(1497))}}),
        );
        assert!(s(&j, "last_message").contains(token));
        apply_event(
            &mut j,
            &json!({"type":"turn.failed","error":format!("{}{token}","z".repeat(497))}),
        );
        assert!(s(&j, "last_error").contains(token));
    }
    #[test]
    fn test_cancelled_finish_uses_japanese_fallback_without_stale_message() {
        let t = Test::new();
        let j = patch(
            running(&t),
            json!({"status":"cancelled","last_message":"Old successful result"}),
        );
        assert_eq!(
            ao_notify(
                "godd-ao.exe",
                "finish",
                &j,
                &t.ctx.jobs,
                t.rt.as_ref(),
                135.
            )["ok"],
            true
        );
        let c = &t.rt.calls.lock().unwrap()[0];
        assert_eq!(flag(c, "result"), "取り消し");
        assert!(c.input.is_empty());
        assert!(c.argv.iter().any(|s| s == "--failed"));
    }
    #[test]
    fn test_ao_cli_is_only_notifier_and_updates_are_throttled() {
        let t = Test::new();
        let j = running(&t);
        let mut n = notifier(&t);
        n.start(&j);
        wait_calls(&t, 1);
        t.rt.tick(10.);
        n.update(&j, false);
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        t.rt.tick(5.);
        n.update(&j, false);
        wait_calls(&t, 2);
        t.rt.tick(15.);
        n.update(&patch(j.clone(), json!({"steps":3})), false);
        wait_calls(&t, 3);
        t.rt.tick(1.);
        n.update(
            &patch(
                j.clone(),
                json!({"status":"succeeded","activity":"done: PROMPT_SECRET"}),
            ),
            true,
        );
        assert!(n.close());
        let c = t.rt.calls.lock().unwrap();
        assert_eq!(
            c.iter().map(|c| c.argv[3].as_str()).collect::<Vec<_>>(),
            ["start", "update", "update", "finish"]
        );
        assert!(c.iter().all(|c| c.argv[1..3] == ["notify", "task"]));
        assert_eq!(flag(&c[0], "phase"), "作業場所 D:/work");
        assert_eq!(flag(&c[2], "percent"), "20");
        assert!(
            c.iter()
                .all(|c| !format!("{:?}", c.argv).contains("PROMPT_SECRET"))
        );
        assert!(
            !t.ctx
                .job_dir(s(&j, "id"))
                .join("discord_message_id")
                .exists()
        );
    }
    #[test]
    fn test_ao_failure_does_not_fail_job() {
        let t = Test::new();
        let j = t.put(job(
            json!({"status":"queued","codex":"codex.CMD","cwd":t.ctx.root}),
        ));
        fs::write(
            t.ctx.job_dir(s(&j, "id")).join("prompt.md"),
            "read-only task",
        )
        .unwrap();
        let exe = t.ctx.root.join("fake-ao.exe");
        fs::write(&exe, "").unwrap();
        t.rt.env
            .lock()
            .unwrap()
            .insert("GODD_AO_BIN".into(), exe.to_string_lossy().into());
        t.rt.with_handler(|c| {
            if c.argv[1] == "notify" {
                Err(io::Error::other("unavailable"))
            } else {
                Ok(Reply {
                    code: 0,
                    stdout: vec![],
                    stderr: vec![],
                })
            }
        });
        t.ctx.supervise(s(&j, "id")).unwrap();
        assert_eq!(t.ctx.load(s(&j, "id")).unwrap()["status"], "succeeded");
    }
    #[test]
    fn test_start_failures_back_off_and_hold_queued_updates_and_finish() {
        let t = Test::new();
        let j = running(&t);
        for ok in [false, false, true, true, true] {
            t.rt.reply(ok);
        }
        let mut n = notifier(&t);
        n.start(&j);
        n.last_edit = 0.;
        n.update(&patch(j.clone(), json!({"steps":1})), false);
        n.update(&patch(j.clone(), json!({"steps":3})), false);
        n.update(&patch(j, json!({"status":"succeeded"})), true);
        assert!(n.close());
        let actions =
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>();
        assert_eq!(actions, ["start", "start", "start", "update", "finish"]);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1., 2.]);
    }
    #[test]
    fn test_lost_finish_response_retries_the_same_cli_operation() {
        let t = Test::new();
        let j = running(&t);
        for ok in [true, false, true] {
            t.rt.reply(ok);
        }
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&patch(j, json!({"status":"succeeded"})), true);
        assert!(n.close());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "finish", "finish"]
        );
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1.]);
    }
    #[test]
    fn test_lost_start_response_retries_the_same_cli_task_id() {
        let t = Test::new();
        let j = running(&t);
        t.rt.replies
            .lock()
            .unwrap()
            .push_back(Err(io::Error::new(io::ErrorKind::TimedOut, "timeout")));
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&patch(j.clone(), json!({"status":"succeeded"})), true);
        assert!(n.close());
        let calls = t.rt.calls.lock().unwrap();
        assert_eq!(
            calls.iter().map(|c| c.argv[3].clone()).collect::<Vec<_>>(),
            ["start", "start", "finish"]
        );
        assert_eq!(
            calls[0].argv[calls[0].argv.iter().position(|s| s == "--id").unwrap() + 1],
            s(&j, "id")
        );
        let strip_deadline = |c: &Call| {
            c.argv
                .iter()
                .filter(|s| !s.starts_with("--deadline-ms="))
                .cloned()
                .collect::<Vec<_>>()
        };
        assert_eq!(strip_deadline(&calls[0]), strip_deadline(&calls[1]));
        assert_eq!(calls[0].input, calls[1].input);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1.]);
    }
    #[test]
    fn test_exhausted_start_never_sends_finish_without_thread() {
        let t = Test::new();
        let j = running(&t);
        for _ in 0..4 {
            t.rt.reply(false);
        }
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&j, true);
        assert!(n.close());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            vec!["start"; 4]
        );
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1., 2., 4.]);
    }
    #[test]
    fn test_one_35_second_deadline_covers_slow_llm_discord_failures_and_backoff() {
        let t = Test::new();
        let j = running(&t);
        let rt = t.rt.clone();
        t.rt.with_handler(move |_| {
            rt.tick(16.);
            Ok(Reply {
                code: 0,
                stdout: b"{\"ok\":false}".to_vec(),
                stderr: vec![],
            })
        });
        let n = notifier(&t);
        assert!(!n.notify_with_retry("start", &j, 135.));
        let (tx, rx) = std::sync::mpsc::channel();
        tx.send(Notice {
            kind: "start".into(),
            job: j.clone(),
            deadline: 135.,
        })
        .unwrap();
        tx.send(Notice {
            kind: "finish".into(),
            job: j.clone(),
            deadline: 170.,
        })
        .unwrap();
        cc_jobs::notify::work_notices(
            "godd-ao.exe",
            &t.ctx.jobs,
            t.rt.as_ref(),
            &rx,
            &Mutex::new(None),
            1.,
        );
        assert_eq!(t.rt.calls.lock().unwrap().len(), 2);
        assert_eq!(*t.rt.sleeps.lock().unwrap(), [1., 2.]);
        assert_eq!(t.rt.mono(), 135.);
        let calls = t.rt.calls.lock().unwrap();
        for c in calls.iter() {
            assert_eq!(
                flag(c, "deadline-ms").parse::<i64>().unwrap(),
                (c.timeout * 1000.) as i64
            );
        }
    }
    fn blocking(
        t: &Test,
        block_action: &str,
        first_failure: bool,
    ) -> (Arc<AtomicBool>, Arc<AtomicBool>) {
        let entered = Arc::new(AtomicBool::new(false));
        let release = Arc::new(AtomicBool::new(false));
        let e = entered.clone();
        let r = release.clone();
        let action = block_action.to_string();
        let calls = Arc::new(AtomicUsize::new(0));
        t.rt.with_handler(move |c| {
            let n = calls.fetch_add(1, Ordering::Relaxed);
            if c.argv[3] == action && (action == "finish" || n == 0) {
                e.store(true, Ordering::Release);
                // Liveness bound only; CI runners stall threads for seconds.
                let deadline = Instant::now() + Duration::from_secs(30);
                while !r.load(Ordering::Acquire) {
                    assert!(Instant::now() < deadline);
                    std::thread::sleep(Duration::from_millis(1));
                }
            }
            Ok(Reply {
                code: 0,
                stdout: json!({"ok":!(first_failure&&n==0)})
                    .to_string()
                    .into_bytes(),
                stderr: vec![],
            })
        });
        (entered, release)
    }
    fn wait_entered(e: &AtomicBool) {
        let end = Instant::now() + Duration::from_secs(30);
        while !e.load(Ordering::Acquire) {
            assert!(Instant::now() < end);
            std::thread::sleep(Duration::from_millis(1));
        }
    }
    #[test]
    fn test_slow_429_keeps_supervisor_waiting_and_finish_queued() {
        let t = Test::new();
        let j = running(&t);
        let (e, r) = blocking(&t, "start", true);
        let mut n = notifier(&t);
        n.backoff = 0.;
        n.start(&j);
        wait_entered(&e);
        n.update(&j, true);
        let close = std::thread::spawn(move || n.close());
        assert!(!close.is_finished());
        assert_eq!(t.rt.calls.lock().unwrap().len(), 1);
        r.store(true, Ordering::Release);
        assert!(close.join().unwrap());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "start", "finish"]
        );
    }
    #[test]
    fn test_close_waits_for_final_notification_and_has_hard_cap() {
        let t = Test::new();
        let j = running(&t);
        let (e, r) = blocking(&t, "finish", false);
        let mut n = notifier(&t);
        n.start(&j);
        n.update(&j, true);
        let close = std::thread::spawn(move || n.close());
        wait_entered(&e);
        assert!(!close.is_finished());
        r.store(true, Ordering::Release);
        assert!(close.join().unwrap());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "finish"]
        );
        let (e, r) = blocking(&t, "finish", false);
        let mut n = notifier(&t);
        n.shutdown_cap = 0.02;
        n.start(&j);
        n.update(&j, true);
        wait_entered(&e);
        assert!(!n.close());
        r.store(true, Ordering::Release);
        // The 20ms cap was tested while finish was held. Draining after release
        // is an eventual-completion assertion, not a 20ms CI scheduling promise.
        n.shutdown_cap = 30.;
        assert!(n.close());
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "finish", "start", "finish"]
        );
        assert_eq!(NOTIFY_SHUTDOWN_CAP_S, 300.);
    }
    #[test]
    fn test_cancel_uses_the_same_start_gate() {
        let t = Test::new();
        let j = patch(running(&t), json!({"status":"cancelled"}));
        t.rt.reply(false);
        task_cancel(
            &j,
            Some("godd-ao.exe".into()),
            t.ctx.jobs.clone(),
            t.rt.clone(),
        );
        task_cancel(
            &patch(j, json!({"started_at":null})),
            Some("godd-ao.exe".into()),
            t.ctx.jobs.clone(),
            t.rt.clone(),
        );
        assert_eq!(
            t.rt.calls
                .lock()
                .unwrap()
                .iter()
                .map(|c| c.argv[3].clone())
                .collect::<Vec<_>>(),
            ["start", "start", "finish"]
        );
    }
    #[test]
    fn test_credentials_are_redacted_and_not_inherited_by_codex() {
        let t = Test::new();
        let mut j = running(&t);
        t.rt.env.lock().unwrap().extend(BTreeMap::from([
            ("GODD_DISCORD_BOT_TOKEN".into(), "sentinel-bot-token".into()),
            ("GODD_DISCORD_WEBHOOK_URL".into(), WEBHOOK.into()),
        ]));
        let text = format!(
            "{}\n",
            json!({"type":"error","message":format!("{} HTTP://private.test/path SENTINEL-BOT-TOKEN",WEBHOOK.to_uppercase())})
        );
        let path = t.ctx.job_dir(s(&j, "id")).join("events.jsonl");
        let mut pending = vec![];
        pump(text.as_bytes(), &path, t.rt.as_ref(), |ev| pending.push(ev)).unwrap();
        let log = fs::read_to_string(&path).unwrap();
        for secret in [
            WEBHOOK,
            WEBHOOK.to_uppercase().as_str(),
            "SENTINEL-BOT-TOKEN",
        ] {
            assert!(!log.contains(secret));
        }
        assert!(log.contains("private.test"));
        assert!(s(&pending[0], "message").contains("private.test"));
        let stderr = t.ctx.job_dir(s(&j, "id")).join("stderr.log");
        pump_stderr(
            b"HTTPS://private.test/x SENTINEL-BOT-TOKEN\n".as_slice(),
            &stderr,
            t.rt.as_ref(),
        )
        .unwrap();
        let log = fs::read_to_string(&stderr).unwrap();
        assert!(log.contains("private.test"));
        assert!(!log.contains("SENTINEL-BOT-TOKEN"));
        j = patch(
            j,
            json!({"name":"HTTPS://private.test/x","activity":"SENTINEL-BOT-TOKEN","last_error":"HTTP://private.test/y","gto":{"payload":{"notes":"HTTPS://private.test/z"}}}),
        );
        t.ctx.save(&j).unwrap();
        let log = fs::read_to_string(t.ctx.job_dir(s(&j, "id")).join("job.json")).unwrap();
        assert!(log.contains("private.test"));
        // job.json is persisted with the same masking as the logs (ToDo 938).
        assert!(!log.contains("SENTINEL-BOT-TOKEN"));
        pump(
            br#"{"type":"error","message":"\u0048TTPS://private.test/hidden"}
"#
            .as_slice(),
            &path,
            t.rt.as_ref(),
            |_| (),
        )
        .unwrap();
        assert!(fs::read_to_string(&path).unwrap().contains("private.test"));
        assert!(
            !t.ctx
                .job_environment()
                .contains_key("GODD_DISCORD_BOT_TOKEN")
        );
        assert!(
            !t.ctx
                .job_environment()
                .contains_key("GODD_DISCORD_WEBHOOK_URL")
        );
        assert_eq!(
            redact_local_log_secrets("sentinel-bot-token", &t.rt.env()),
            "[秘密]"
        );
        t.rt.replies
            .lock()
            .unwrap()
            .push_back(Err(io::Error::new(io::ErrorKind::NotFound, "missing.exe")));
        assert_eq!(
            ao_notify("missing.exe", "start", &j, &t.ctx.jobs, t.rt.as_ref(), 135.),
            json!({"ok":false})
        );
    }
    #[test]
    fn test_non_http_uri_and_scp_addresses_pass_through_state_and_logs() {
        let t = Test::new();
        let mut j = running(&t);
        for address in [
            "SSH://git@private.test/org/repo",
            "GiT://private.test/org/repo",
            "git@private.test:org/repo",
            "USER@PRIVATE.TEST:org/repo",
        ] {
            assert_eq!(
                redact_local_log_secrets(&format!("task {address}"), &t.rt.env()),
                format!("task {address}")
            );
            j = patch(
                j,
                json!({"name":address,"activity":address,"last_error":address,"gto":{"payload":{"notes":address}}}),
            );
            t.ctx.save(&j).unwrap();
            assert!(
                fs::read_to_string(t.ctx.job_dir(s(&j, "id")).join("job.json"))
                    .unwrap()
                    .contains(address)
            );
            let p = t.ctx.job_dir(s(&j, "id")).join("events.jsonl");
            let raw = format!("{}\n", json!({"type":"error","message":address}));
            pump(raw.as_bytes(), &p, t.rt.as_ref(), |_| ()).unwrap();
            assert!(fs::read_to_string(p).unwrap().contains(address));
            let p = t.ctx.job_dir(s(&j, "id")).join("stderr.log");
            pump_stderr(format!("{address}\n").as_bytes(), &p, t.rt.as_ref()).unwrap();
            assert!(fs::read_to_string(p).unwrap().contains(address));
        }
    }
}
mod end_to_end {
    use super::*;
    #[test]
    fn terminal_supervisor_cleanup_preserves_its_independent_descendant() {
        let mut t = Test::new();
        t.ctx.rt = Arc::new(System::default());
        let fixture = env!("CARGO_BIN_EXE_cc_jobs_fixture");
        let mut parent = hidden_command(fixture)
            .arg("--supervisor-with-independent-job")
            .env("CC_JOBS_FIXTURE_DIR", &t.ctx.root)
            .spawn()
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(5);
        while !t.ctx.root.join("hold.pid").exists() {
            if Instant::now() >= deadline {
                let _ = parent.kill();
                let _ = parent.wait();
                panic!("fixture descendant did not start");
            }
            std::thread::sleep(Duration::from_millis(10));
        }
        let child: u32 = fs::read_to_string(t.ctx.root.join("hold.pid"))
            .unwrap()
            .parse()
            .unwrap();
        let child_created = proc_created(child).unwrap();
        let j=t.put(job(json!({"id":"finished","status":"succeeded","pid":parent.id(),"pid_created":proc_created(parent.id()),"finished_at":(chrono::Local::now()-chrono::Duration::seconds(180)).to_rfc3339(),"codex_pid":null})));
        let result = t.ctx.cleanup_supervisors(true);
        let survived = proc_created(child) == Some(child_created);
        // Release both owned processes before asserting, including failure paths.
        if proc_created(child) == Some(child_created) {
            t.ctx
                .rt
                .pause_supervisor(child, child_created)
                .unwrap()
                .stop()
                .unwrap();
        }
        if parent.try_wait().unwrap().is_none() {
            parent.kill().unwrap();
        }
        parent.wait().unwrap();
        assert!(
            survived,
            "cleanup followed parent PID links into an independent job"
        );
        assert_eq!(result.unwrap()[0]["id"], "finished");
        assert_eq!(
            t.ctx.load("finished").unwrap()["finished_at"],
            j["finished_at"]
        );
    }
    #[test]
    fn cargo_slot_wait_marker_is_visible_then_cleared_before_execution() {
        let mut t = Test::new();
        cargo_test_policy(&t.ctx.root, 1);
        t.ctx.rt = Arc::new(System::default());
        let job=t.put(job(json!({"id":"build","status":"running","pid":std::process::id(),"pid_created":proc_created(std::process::id())})));
        let held = FileLock::try_acquire(&t.ctx.root.join("cargo-build.lock.0")).unwrap();
        let mut gate = hidden_command(env!("CARGO_BIN_EXE_cargo_gate"))
            .arg("--exit-seven")
            .env("CC_JOB_ID", "build")
            .env("CODEX_JOBS_CARGO_GATE", t.ctx.root.join("cargo_gate.exe"))
            .env("CODEX_JOBS_CARGO_LOCK_DIR", &t.ctx.root)
            .env("CODEX_JOBS_CARGO_SLOTS", "1")
            .env("CARGO_TARGET_DIR", &t.ctx.root)
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
            .env_remove("CODEX_JOBS_CACHE_LOCK_HELD")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(30);
        let mut visible = false;
        while Instant::now() < deadline {
            if t.ctx.resource_waits(&job) == ["cargoの共有ビルド枠待ち"] {
                visible = true;
                break;
            }
            std::thread::sleep(Duration::from_millis(10));
        }
        // While Cargo waits for a slot the job is idle and frees its queue slot.
        let occupied =
            t.ctx.queue_report(std::slice::from_ref(&job)).unwrap()["occupied_slots"].clone();
        drop(held);
        let exit = gate.wait().unwrap();
        assert!(
            visible,
            "running build did not expose its exact resource wait"
        );
        assert_eq!(occupied, 0);
        assert_eq!(exit.code(), Some(7));
        assert!(t.ctx.resource_waits(&job).is_empty());
        assert_eq!(
            t.ctx.queue_report(std::slice::from_ref(&job)).unwrap()["occupied_slots"],
            1
        );
        fs::write(
            t.ctx.job_dir("build").join("resource-waits/stale.json"),
            json!({"job_id":"build","pid":std::process::id(),"pid_created":0,"reason":"stale"})
                .to_string(),
        )
        .unwrap();
        assert!(t.ctx.resource_waits(&job).is_empty());
    }
    #[test]
    fn state_lock_does_not_block_snapshot_readers() {
        let t = Test::new();
        let job = t.put(new_job());
        let held = t.ctx.job_lock(s(&job, "id"), "state.lock").unwrap();
        assert_eq!(t.ctx.load(s(&job, "id")).unwrap(), job);
        assert!(FileLock::try_acquire(&t.ctx.job_dir(s(&job, "id")).join("state.lock")).is_err());
        drop(held);
    }
    #[test]
    fn cancelled_kernel_lock_wait_is_drained_and_reports_path_and_exit_75() {
        let tmp = Temp::new();
        let path = tmp.0.join(".queue.lock");
        let held = FileLock::try_acquire(&path).unwrap();
        let start = Instant::now();
        let error = match FileLock::acquire_queue_timeout(&path, Duration::from_millis(80)) {
            Ok(_) => panic!("contended byte lock unexpectedly acquired"),
            Err(error) => error,
        };
        assert_eq!(error.kind(), io::ErrorKind::TimedOut);
        assert!(start.elapsed() < Duration::from_secs(2));
        let error = Error::file_io("acquire exclusive lock byte range [0, 1)", &path, error);
        assert_eq!(error.code, 75);
        assert!(error.message.contains(path.to_str().unwrap()));
        assert!(error.message.contains("[0, 1)"));
        drop(held);
        // A leaked pending request would acquire this byte after the holder exits.
        let _next = FileLock::try_acquire(&path).unwrap();
    }
    #[test]
    fn queue_config_read_retries_real_byte_lock_and_cli_reports_exhaustion() {
        let t = E2e::new();
        let path = t.exe.parent().unwrap().join("queue-config.json");
        fs::write(&path, r#"{"max_running":3}"#).unwrap();
        let held = FileLock::try_acquire(&path).unwrap();
        let output = t.cli(
            &[
                "run",
                "--name",
                "locked-config",
                "--prompt-file",
                "unused.md",
            ],
            "ok",
        );
        assert_eq!(output.status.code(), Some(75));
        assert!(output.stdout.is_empty());
        assert!(String::from_utf8_lossy(&output.stderr).contains(path.to_str().unwrap()));
        assert!(String::from_utf8_lossy(&output.stderr).contains("os error 33"));
        drop(held);
        let held = FileLock::try_acquire(&path).unwrap();
        let context = Context {
            root: t.exe.parent().unwrap().to_path_buf(),
            ..Context::installed().unwrap()
        };
        std::thread::scope(|scope| {
            scope.spawn(move || {
                std::thread::sleep(Duration::from_millis(100));
                drop(held);
            });
            assert_eq!(context.queue_config().unwrap().max_running, 3);
        });
    }
    #[test]
    fn real_capacity_retry_preserves_prompt_cwd_and_session_and_stops_at_limit() {
        for (mode, expected, count) in [
            ("capacity-once", "succeeded", 1),
            ("capacity-always", "failed", 3),
        ] {
            let t = E2e::new();
            let jid = t.start(mode, &t.tmp.0, "再開する内容\nsecond line".as_bytes());
            t.until(|| TERMINAL.contains(&s(&t.job(&jid), "status")));
            let job = t.job(&jid);
            assert_eq!(job["status"], expected);
            assert_eq!(job["capacity_retry_count"], count);
            let calls: Vec<Value> = fs::read_to_string(t.rec.join("capacity-calls.jsonl"))
                .unwrap()
                .lines()
                .map(|line| serde_json::from_str(line).unwrap())
                .collect();
            assert_eq!(calls.len(), count as usize + 1);
            for call in &calls {
                assert_eq!(call["cwd"], t.tmp.0.to_string_lossy().as_ref());
                assert_eq!(call["prompt"], calls[0]["prompt"]);
            }
            assert!(
                calls[0]["prompt"]
                    .as_str()
                    .unwrap()
                    .contains("再開する内容\nsecond line")
            );
            for (index, call) in calls.iter().enumerate() {
                let args: Vec<_> = call["argv"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .map(|v| v.as_str().unwrap())
                    .collect();
                assert!(args.windows(2).any(|w| w
                    == [
                        "-m",
                        [
                            "gpt-6-luna",
                            "anthropic/claude-sonnet-5-5",
                            "gpt-6.1-sol",
                            "gpt-6-astra"
                        ][index]
                    ]));
                if index > 0 {
                    assert!(
                        args.windows(2)
                            .any(|w| w == ["resume", "01a0e883-7628-7121-9540-6780fd4cd6f4"])
                    );
                }
            }
        }
    }
    #[test]
    fn thirty_parallel_run_processes_share_the_real_queue_files() {
        parallel_intake(false);
    }
    #[test]
    fn thirty_parallel_run_processes_are_durable_while_history_scan_is_held() {
        parallel_intake(true);
    }
    fn parallel_intake(hold_scan: bool) {
        let t = E2e::new();
        let prompt = t.tmp.0.join("parallel-prompt.md");
        fs::write(&prompt, "fixture parallel launch").unwrap();
        fs::write(
            t.exe.parent().unwrap().join("queue-config.json"),
            r#"{"max_running":1,"max_children_per_parent":1}"#,
        )
        .unwrap();
        // Reserve the single running slot so supervisors keep polling admission.
        let jobs = t.exe.parent().unwrap().join("jobs");
        let dir = jobs.join("parallel-parent");
        fs::create_dir_all(&dir).unwrap();
        fs::write(
            dir.join("job.json"),
            pretty(
                &json!({"id":"parallel-parent","name":"parent","status":"running",
            "created_at":"2026-10-07T00:00:00+09:00","queue_unreadable":true}),
            ),
        )
        .unwrap();
        for n in 0..3132 {
            let dir = jobs.join(format!("history-{n}"));
            fs::create_dir(&dir).unwrap();
            fs::write(dir.join("job.json"), pretty(&json!({"id":format!("history-{n}"),"status":"succeeded","created_at":"2026-10-06T00:00:00+09:00"}))).unwrap();
        }
        let scan_path = jobs.join(".queue-scan.lock");
        let mut held_scan = hold_scan.then(|| FileLock::try_acquire(&scan_path).unwrap());
        if let Some(lock) = &mut held_scan {
            lock.set_owner(&scan_path, "fixture:history_scan:blocked");
        }
        let started = Instant::now();
        let barrier = Arc::new(std::sync::Barrier::new(30));
        let outputs = std::thread::scope(|scope| {
            let handles: Vec<_> = (0..30)
                .map(|n| {
                    let t = &t;
                    let prompt = &prompt;
                    let barrier = barrier.clone();
                    scope.spawn(move || {
                        barrier.wait();
                        let name = format!("parallel-{n}");
                        let todo = (10000 + n).to_string();
                        let key = format!("{n:032x}");
                        let mut args = vec![
                            "run",
                            "--name",
                            &name,
                            "--model",
                            "fixture",
                            "--todo-id",
                            &todo,
                            "--cwd",
                            t.tmp.0.to_str().unwrap(),
                            "--prompt-file",
                            prompt.to_str().unwrap(),
                            "--mcp-profile",
                            "minimal",
                        ];
                        if hold_scan {
                            args.extend(["--launch-key", &key]);
                        }
                        t.cli(&args, "ok")
                    })
                })
                .collect();
            handles
                .into_iter()
                .map(|h| h.join().unwrap())
                .collect::<Vec<_>>()
        });
        let mut ids = HashSet::new();
        let mut failures = Vec::new();
        for output in &outputs {
            assert!(!String::from_utf8_lossy(&output.stderr).contains("os error 33"));
            if output.status.success() {
                ids.insert(String::from_utf8_lossy(&output.stdout).trim().to_string());
            } else {
                failures.push(format!(
                    "{:?}: {}",
                    output.status.code(),
                    String::from_utf8_lossy(&output.stderr)
                ));
            }
        }
        eprintln!(
            "30 launch results: {} accepted, {} failed",
            ids.len(),
            failures.len()
        );
        eprintln!(
            "intake elapsed {:?}; held history scan={hold_scan}",
            started.elapsed()
        );
        if hold_scan && !ids.is_empty() {
            // The test-owned scan handle has remained held throughout intake.
            // No supervisor can have established a complete admission index.
            let index: Value =
                serde_json::from_slice(&fs::read(jobs.join(".queue-index.json")).unwrap()).unwrap();
            assert_eq!(index["scan_complete"], false);
            assert!(FileLock::try_acquire(&scan_path).is_err());
            assert!(
                !t.rec.join("call.json").exists(),
                "no fixture agent may start before reconciliation"
            );
        }
        drop(held_scan);
        // Observe reconciliation separately from receipt. The running legacy
        // parent must be discovered before execution, while all new IDs survive.
        let reconciled = if failures.is_empty() {
            let until = Instant::now() + Duration::from_secs(120);
            loop {
                let complete = fs::read(jobs.join(".queue-index.json"))
                    .ok()
                    .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok())
                    .is_some_and(|index| {
                        index["scan_complete"] == true
                            && index["ids"]
                                .as_array()
                                .is_some_and(|ids| ids.contains(&json!("parallel-parent")))
                    });
                if complete || Instant::now() >= until {
                    break complete;
                }
                std::thread::sleep(Duration::from_millis(50));
            }
        } else {
            false
        };
        if let Ok(metrics) = fs::read_to_string(jobs.join(".queue-scan-metrics.json")) {
            eprintln!("history scan phase metrics: {metrics}");
        }
        // These jobs are queued and have no child processes. Retain an OS handle
        // while stopping each supervisor; taskkill /T can follow reused parent IDs.
        let mut owners = FixtureOwners {
            handles: Vec::new(),
            shutdown_started: false,
        };
        for id in &ids {
            let job = t.job(id);
            assert_eq!(job["status"], "queued");
            let pid = job["pid"].as_u64().unwrap() as u32;
            assert_ne!(pid, std::process::id());
            assert_eq!(proc_created(pid), job["pid_created"].as_u64());
            owners
                .handles
                .push(FixtureProcess::retain(pid, job["pid_created"].as_u64().unwrap()).unwrap());
        }
        // Cancel every owner first: waiting for one scanner while the other
        // 29 owners keep issuing I/O delays the scanner's kernel cancellation.
        let all_exited = owners.shutdown();
        assert!(all_exited, "parallel fixture shutdown did not confirm exit");
        assert!(failures.is_empty(), "parallel run failures: {failures:#?}");
        assert_eq!(ids.len(), 30);
        assert!(
            reconciled,
            "legacy running owner must be discovered after intake"
        );
        assert!(
            !t.rec.join("call.json").exists(),
            "the discovered legacy owner still occupies the slot"
        );
        let sequence: Value =
            serde_json::from_slice(&fs::read(jobs.join(".queue-sequence.json")).unwrap()).unwrap();
        assert_eq!(sequence["sequence"], 30);
        let index: Value =
            serde_json::from_slice(&fs::read(jobs.join(".queue-index.json")).unwrap()).unwrap();
        for id in &ids {
            assert!(index["ids"].as_array().unwrap().contains(&json!(id)));
        }
    }
    struct E2e {
        tmp: Temp,
        exe: PathBuf,
        rec: PathBuf,
        diagnostic: std::sync::atomic::AtomicBool,
        handles: Mutex<Vec<FixtureProcess>>,
        inherited_home: Option<PathBuf>,
    }
    struct FixtureProcess {
        handle: windows_sys::Win32::Foundation::HANDLE,
        pid: u32,
        created: u64,
    }
    struct FixtureOwners {
        handles: Vec<FixtureProcess>,
        shutdown_started: bool,
    }
    impl FixtureOwners {
        fn shutdown(&mut self) -> bool {
            self.shutdown_started = true;
            for owner in &self.handles {
                owner.request_stop();
            }
            let mut all = true;
            for owner in &self.handles {
                all &= owner.wait_exit();
            }
            all
        }
    }
    impl Drop for FixtureOwners {
        fn drop(&mut self) {
            // A retention/identity assertion can fail before normal shutdown.
            if !self.shutdown_started {
                let _ = self.shutdown();
            }
        }
    }
    // The test owns these handles; the mutex serializes retention and cleanup.
    unsafe impl Send for FixtureProcess {}
    impl FixtureProcess {
        fn retain(pid: u32, created: u64) -> Option<Self> {
            use windows_sys::Win32::{Foundation::*, System::Threading::*};
            unsafe {
                let handle = OpenProcess(
                    PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_TERMINATE | PROCESS_SYNCHRONIZE,
                    0,
                    pid,
                );
                if handle.is_null() {
                    return None;
                }
                let mut times = [std::mem::zeroed::<FILETIME>(); 4];
                let valid = GetProcessTimes(
                    handle,
                    &mut times[0],
                    &mut times[1],
                    &mut times[2],
                    &mut times[3],
                ) != 0
                    && (((times[0].dwHighDateTime as u64) << 32) | times[0].dwLowDateTime as u64)
                        == created;
                if !valid {
                    CloseHandle(handle);
                    return None;
                }
                Some(Self {
                    handle,
                    pid,
                    created,
                })
            }
        }
        fn live(&self) -> bool {
            unsafe {
                windows_sys::Win32::System::Threading::WaitForSingleObject(self.handle, 0)
                    == windows_sys::Win32::Foundation::WAIT_TIMEOUT
            }
        }
        fn request_stop(&self) {
            use windows_sys::Win32::System::Threading::*;
            unsafe {
                if !self.live() || TerminateProcess(self.handle, 0) != 0 {
                    return;
                }
                let error = io::Error::last_os_error();
                if self.live() {
                    eprintln!(
                        "fixture termination request failed: pid={} created={} error={error}",
                        self.pid, self.created
                    );
                }
            }
        }
        fn wait_exit(&self) -> bool {
            use windows_sys::Win32::{Foundation::*, System::Threading::*};
            unsafe {
                let result = WaitForSingleObject(self.handle, 5000);
                if result != WAIT_OBJECT_0 {
                    eprintln!(
                        "fixture cleanup failed: pid={} created={} wait={result}",
                        self.pid, self.created
                    );
                }
                result == WAIT_OBJECT_0
            }
        }
        fn stop(&self) -> bool {
            self.request_stop();
            self.wait_exit()
        }
    }
    impl Drop for FixtureProcess {
        fn drop(&mut self) {
            unsafe {
                windows_sys::Win32::Foundation::CloseHandle(self.handle);
            }
        }
    }
    impl E2e {
        fn new() -> Self {
            let tmp = Temp::new();
            let tool = tmp.0.join("tool");
            let bin = tmp.0.join("bin");
            let rec = tmp.0.join("rec");
            for p in [&tool, &bin, &rec] {
                fs::create_dir_all(p).unwrap();
            }
            let exe = tool.join("codex_jobs.exe");
            fs::copy(env!("CARGO_BIN_EXE_codex_jobs"), &exe).unwrap();
            fs::copy(
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
                bin.join("fixture.exe"),
            )
            .unwrap();
            let bun = tmp
                .0
                .join("appdata/npm/node_modules/@bitkyc08/opencodex/node_modules/bun/bin");
            fs::create_dir_all(&bun).unwrap();
            fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), bun.join("bun.exe")).unwrap();
            fs::write(
                bin.join("codex.cmd"),
                b"@echo off\r\n\"%~dp0fixture.exe\" %*\r\n",
            )
            .unwrap();
            fs::write(
                tmp.0.join("owner-instructions.md"),
                "fixture owner instructions v1",
            )
            .unwrap();
            Self {
                tmp,
                exe,
                rec,
                diagnostic: std::sync::atomic::AtomicBool::new(false),
                handles: Mutex::new(Vec::new()),
                inherited_home: None,
            }
        }
        fn cli(&self, args: &[&str], mode: &str) -> std::process::Output {
            let mut c = hidden_command(self.exe.to_str().unwrap());
            if mode == "report-only" { c.env("CC_TODO_HOME", self.tmp.0.join("todo")); }
            c.args(args)
                .env(
                    "PATH",
                    format!(
                        "{};{}",
                        self.tmp.0.join("bin").display(),
                        std::env::var("PATH").unwrap_or_default()
                    ),
                )
                .env("CC_JOBS_FIXTURE_MODE", mode)
                .env("APPDATA", self.tmp.0.join("appdata"))
                .env("CC_JOBS_FIXTURE_DIR", &self.rec)
                .env(
                    "CC_OWNER_INSTRUCTIONS_PATH",
                    self.tmp.0.join("owner-instructions.md"),
                )
                .env("CODEX_HOME", self.tmp.0.join("codexhome"))
                .env("GODD_AO_BIN", self.tmp.0.join("missing-ao.exe"))
                .env("CODEX_JOBS_GTO_BIN", self.tmp.0.join("missing-gto.exe"))
                .env_remove("GODD_DISCORD_BOT_TOKEN")
                .env_remove("CC_JOB_ID")
                .env_remove("CODEX_JOBS_DISCORD_WEBHOOK");
            if let Some(home) = &self.inherited_home {
                c.env("USERPROFILE", home).env("HOME", home);
            }
            // CODEX_HOME alone does not isolate native plugin initialization:
            // Context::installed also resolves USERPROFILE/HOME independently.
            c.env("USERPROFILE", self.tmp.0.join("home"))
                .env("HOME", self.tmp.0.join("home"));
            c.output().unwrap()
        }
        fn start(&self, mode: &str, cwd: &Path, prompt: &[u8]) -> String {
            let p = self.tmp.0.join("prompt.md");
            fs::write(&p, prompt).unwrap();
            let r = self.cli(
                &[
                    "run",
                    "--name",
                    "e2e",
                    "--model",
                    "gpt-6-luna",
                    "--effort",
                    "max",
                    "--cwd",
                    cwd.to_str().unwrap(),
                    "--prompt-file",
                    p.to_str().unwrap(),
                ],
                mode,
            );
            assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stderr));
            String::from_utf8(r.stdout).unwrap().trim().into()
        }
        fn job(&self, jid: &str) -> Value {
            let root = self.tmp.0.join("tool");
            let reader = Context {
                jobs: root.join("jobs"),
                root,
                home: self.tmp.0.join("home"),
                codex_home: self.tmp.0.join("codexhome"),
                gto: self.tmp.0.join("missing-gto.exe"),
                executable: self.exe.clone(),
                rt: Arc::new(System::default()),
            };
            reader
                .read_json(&reader.job_dir(jid).join("job.json"))
                .unwrap()
        }
        fn until(&self, mut condition: impl FnMut() -> bool) {
            self.until_phase("unspecified", &mut condition);
        }
        fn retain_fixture_processes(&self) {
            let mut identities = Vec::new();
            if let Ok(entries) = fs::read_dir(self.tmp.0.join("tool/jobs")) {
                for entry in entries.flatten() {
                    if let Ok(bytes) = fs::read(entry.path().join("job.json"))
                        && let Ok(job) = serde_json::from_slice::<Value>(&bytes)
                    {
                        for key in ["pid", "codex_pid"] {
                            if let (Some(pid), Some(created)) =
                                (job[key].as_u64(), job[format!("{key}_created")].as_u64())
                            {
                                identities.push((pid as u32, created));
                            }
                        }
                    }
                }
            }
            // The marker belongs to this unique fixture directory; verify its
            // process image before retaining the creation-time-bound OS handle.
            for name in ["sleep.pid", "hold.pid"] {
                if let Ok(bytes) = fs::read_to_string(self.rec.join(name))
                    && let Ok(pid) = bytes.trim().parse::<u32>()
                    && let Ok(bytes) =
                        fs::read(self.rec.join(name.replace(".pid", ".identity.json")))
                    && let Ok(identity) = serde_json::from_slice::<Value>(&bytes)
                    && identity["pid"].as_u64() == Some(pid as u64)
                    && let Some(created) = identity["created"].as_u64()
                {
                    use windows_sys::Win32::{Foundation::*, System::Threading::*};
                    unsafe {
                        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
                        if !handle.is_null() {
                            let mut path = [0u16; 32768];
                            let mut size = path.len() as u32;
                            let own =
                                QueryFullProcessImageNameW(handle, 0, path.as_mut_ptr(), &mut size)
                                    != 0
                                    && String::from_utf16_lossy(&path[..size as usize])
                                        .replace('\\', "/")
                                        .eq_ignore_ascii_case(
                                            &self
                                                .tmp
                                                .0
                                                .join("bin/fixture.exe")
                                                .to_string_lossy()
                                                .replace('\\', "/"),
                                        );
                            CloseHandle(handle);
                            if own {
                                identities.push((pid, created));
                            }
                        }
                    }
                }
            }
            let mut handles = self.handles.lock().unwrap();
            for (pid, created) in identities {
                if !handles.iter().any(|h| h.pid == pid && h.created == created)
                    && let Some(handle) = FixtureProcess::retain(pid, created)
                {
                    handles.push(handle);
                }
            }
        }
        fn phase_snapshot(&self, phase: &str, elapsed: Duration) -> Value {
            let mut jobs = Vec::new();
            let mut names = HashMap::new();
            if let Ok(entries) = fs::read_dir(self.tmp.0.join("tool/jobs")) {
                for entry in entries.flatten() {
                    if let Ok(bytes) = fs::read(entry.path().join("job.json"))
                        && let Ok(job) = serde_json::from_slice::<Value>(&bytes)
                    {
                        let role = if s(&job, "name") == "dependency"
                            || !s(&job, "parent_job_id").is_empty()
                        {
                            "child"
                        } else if jobs.is_empty() {
                            "parent"
                        } else {
                            "other"
                        };
                        names.insert(s(&job, "id").to_owned(), role);
                        jobs.push(job);
                    }
                }
            }
            jobs.sort_by(|a, b| {
                s(a, "created_at")
                    .cmp(s(b, "created_at"))
                    .then_with(|| s(a, "id").cmp(s(b, "id")))
            });
            for (n, job) in jobs
                .iter()
                .filter(|j| s(j, "parent_job_id").is_empty())
                .enumerate()
            {
                names.insert(
                    s(job, "id").to_owned(),
                    if n == 0 { "parent" } else { "other" },
                );
            }
            let alias = |id: &str| names.get(id).copied().unwrap_or("unknown");
            let rows: Vec<_> = jobs.iter().map(|job| {
                let process = |key| { let pid = job[key].as_u64().unwrap_or(0) as u32; let current = proc_created(pid);
                    let handles = self.handles.lock().unwrap();
                    let retained_live = handles.iter().find(|handle| handle.pid == pid && Some(handle.created) == job[format!("{key}_created")].as_u64()).map(FixtureProcess::live);
                    json!({"pid":pid,"recorded_created":job[format!("{key}_created")],"current_created":current,"retained_handle_live":retained_live,"identity_matches":current.is_some_and(|value| job[format!("{key}_created")].as_u64() == Some(value))}) };
                let error_kind = if s(job,"last_error").contains("plugin env_vars") { "plugin_metadata" } else if job["last_error"].is_null() { "none" } else { "other" };
                json!({"role":alias(s(job,"id")),"status":job["status"],"error_kind":error_kind,"parent":alias(s(job,"parent_job_id")),"queue_sequence":job["queue_sequence"],
                    "wait_code":job["queue_wait_reason"]["code"],"wait_owner":alias(s(&job["queue_wait_reason"],"owner_job_id")),"max_running":job["queue_wait_reason"]["max_running"],
                    "admission_error":!job["queue_admission_error"].is_null(),"owner_hint":job["work_owner"].is_object(),"supervisor":process("pid"),"agent":process("codex_pid")})
            }).collect();
            let index: Value = fs::read(self.tmp.0.join("tool/jobs/.queue-index.json"))
                .ok()
                .and_then(|bytes| serde_json::from_slice(&bytes).ok())
                .unwrap_or(Value::Null);
            json!({"phase":phase,"elapsed_ms":elapsed.as_millis() as u64,"jobs":rows,"sleep_marker":self.rec.join("sleep.pid").exists(),
                "index_complete":index["scan_complete"],"scanned_at":index["scanned_at"],"index_ids":index["ids"].as_array().into_iter().flatten().map(|id|alias(id.as_str().unwrap_or(""))).collect::<Vec<_>>()})
        }
        fn until_phase(&self, phase: &str, mut condition: impl FnMut() -> bool) {
            let started = Instant::now();
            let end = Instant::now() + Duration::from_secs(20);
            while !condition() {
                if self.diagnostic.load(Ordering::Relaxed) {
                    self.retain_fixture_processes();
                }
                if Instant::now() >= end {
                    let snapshot = self.phase_snapshot(phase, started.elapsed());
                    eprintln!("dependency fixture timeout: {snapshot}");
                    panic!("timed out in {phase}");
                }
                std::thread::sleep(Duration::from_millis(100));
            }
            if self.diagnostic.load(Ordering::Relaxed) {
                self.retain_fixture_processes();
                eprintln!(
                    "dependency fixture completed: {}",
                    self.phase_snapshot(phase, started.elapsed())
                );
            }
        }
        fn slow(&self) -> (String, Vec<u32>) {
            self.slow_at("slow", &self.tmp.0)
        }
        fn slow_at(&self, mode: &str, cwd: &Path) -> (String, Vec<u32>) {
            let jid = self.start(mode, cwd, b"hi");
            self.until_phase("parent_fixture_start", || {
                self.rec.join("sleep.pid").exists() && truth(&self.job(&jid)["codex_pid"])
            });
            let j = self.job(&jid);
            let child: u32 = fs::read_to_string(self.rec.join("sleep.pid"))
                .unwrap()
                .parse()
                .unwrap();
            (
                jid,
                vec![
                    j["pid"].as_u64().unwrap() as u32,
                    j["codex_pid"].as_u64().unwrap() as u32,
                    child,
                ],
            )
        }
    }
    impl Drop for E2e {
        fn drop(&mut self) {
            if self.diagnostic.load(Ordering::Relaxed) {
                self.retain_fixture_processes();
                let mut handles = self.handles.lock().unwrap();
                for handle in handles.iter() {
                    handle.request_stop();
                }
                let mut stopped = true;
                for handle in handles.iter() {
                    stopped &= handle.wait_exit();
                }
                handles.clear();
                if !std::thread::panicking() {
                    assert!(stopped, "fixture cleanup did not confirm exit");
                }
                return;
            }
            let rt = System::default();
            for f in ["hold.pid", "sleep.pid"] {
                if let Ok(s) = fs::read_to_string(self.rec.join(f))
                    && let Ok(pid) = s.parse::<u32>()
                    && proc_created(pid).is_some()
                {
                    let _ = rt.kill_tree(pid);
                }
            }
            if let Ok(entries) = fs::read_dir(self.tmp.0.join("tool/jobs")) {
                for e in entries.flatten() {
                    if let Ok(raw) = fs::read(e.path().join("job.json"))
                        && let Ok(j) = serde_json::from_slice::<Value>(&raw)
                    {
                        for k in ["pid", "codex_pid"] {
                            if let Some(pid) = j[k].as_u64()
                                && proc_created(pid as u32).is_some_and(|created| {
                                    j[format!("{k}_created")].as_u64() == Some(created)
                                })
                            {
                                let _ = rt.kill_tree(pid as u32);
                            }
                        }
                    }
                }
            }
            std::thread::sleep(Duration::from_millis(100));
        }
    }
    /// Parts of the synthetic secrets the fixture prints.
    const SECRET_PARTS: [&str; 8] = [
        "Ab3De5Gh7J",
        "sv7Hq2Xw",
        "Zx9Kq2Lm",
        "abcd1234efgh",
        "Pk4Vb8Nm",
        "Rt6Yu1Io",
        "Lq3Wz5Ex",
        "Mj2Kn4Bh",
    ];
    /// Every record file in the job directory, including temporaries, is masked.
    fn assert_records_masked(dir: &Path) {
        for entry in fs::read_dir(dir).unwrap().flatten() {
            let name = entry.file_name().to_string_lossy().into_owned();
            // Lock files are held open by the supervisor and carry no text.
            if name.starts_with("prompt") || name.ends_with(".lock") || !entry.path().is_file() {
                continue;
            }
            let text = String::from_utf8_lossy(&fs::read(entry.path()).unwrap()).into_owned();
            let leak = SECRET_PARTS.iter().find(|part| text.contains(*part));
            assert_eq!(leak, None, "{name}: {text}");
        }
        for name in ["events.jsonl", "job.json", "stderr.log", "last_message.md"] {
            let text = fs::read_to_string(dir.join(name)).unwrap();
            assert!(text.contains("[REDACTED]"), "{name} was not masked: {text}");
        }
    }
    #[test]
    fn records_are_masked_while_codex_runs_and_after_it_is_killed() {
        let e = E2e::new();
        let jid = e.start("secret-hold", &e.tmp.0, b"hold");
        let dir = e.tmp.0.join("tool/jobs").join(&jid);
        let read = |name: &str| fs::read_to_string(dir.join(name)).unwrap_or_default();
        e.until(|| {
            read("last_message.md").starts_with("PARTIAL")
                && read("stderr.log").contains("plain stderr line")
                && read("events.jsonl").contains("not json")
        });
        // Mid-run: codex is still alive and nothing raw has reached the disk.
        let pid = e.job(&jid)["codex_pid"].as_u64().unwrap() as u32;
        assert!(proc_created(pid).is_some(), "codex exited early");
        assert_records_masked(&dir);
        assert_eq!(read("last_message.md"), "PARTIAL [REDACTED]");
        // A crash leaves the same masked files behind.
        System::default().kill_tree(pid).unwrap();
        e.until(|| s(&e.job(&jid), "status") == "failed");
        assert_records_masked(&dir);
        assert_eq!(read("last_message.md"), "PARTIAL [REDACTED]");
    }
    #[test]
    fn supervisor_persists_masked_records_but_feeds_the_prompt_unchanged() {
        let e = E2e::new();
        let prompt = format!(
            "use serviceSecret=\"{}\" as given",
            "pr0mptSecret9".repeat(2)
        );
        let jid = e.start("secret-events", &e.tmp.0, prompt.as_bytes());
        e.until(|| s(&e.job(&jid), "status") == "succeeded");
        let saved = e.job(&jid);
        let supervisor = saved["pid"].as_u64().unwrap() as u32;
        let supervisor_created = saved["pid_created"].as_u64().unwrap();
        e.until(|| proc_created(supervisor) != Some(supervisor_created));
        let dir = e.tmp.0.join("tool/jobs").join(&jid);
        assert_records_masked(&dir);
        let stderr = fs::read_to_string(dir.join("stderr.log")).unwrap();
        for kept in ["key: ", " after key", " after value", "plain stderr line"] {
            assert!(stderr.contains(kept), "{kept:?} missing: {stderr}");
        }
        assert_eq!(
            fs::read_to_string(dir.join("last_message.md")).unwrap(),
            "DONE [REDACTED]"
        );
        let call: Value =
            serde_json::from_str(&fs::read_to_string(e.rec.join("call.json")).unwrap()).unwrap();
        let argv = call["argv"].as_array().unwrap();
        assert!(
            !argv.iter().any(|a| a == "-o"),
            "codex must not write records itself: {call}"
        );
        let events = fs::read_to_string(dir.join("events.jsonl")).unwrap();
        let lines = events.lines().collect::<Vec<_>>();
        assert_eq!(lines.last(), Some(&"not json [REDACTED]"));
        for line in &lines[..lines.len() - 1] {
            let event: Value = serde_json::from_str(line).expect("masked event is valid JSON");
            assert!(event["_ts"].is_string());
        }
        let job = e.job(&jid);
        assert_eq!(job["last_message"], "DONE [REDACTED]");
        assert_eq!(job["thread_id"], "01a0e883-7628-7121-9540-6780fd4cd6f4");
        // The model still receives the prompt exactly as written.
        let fed = fs::read(e.rec.join("stdin.bin")).unwrap();
        assert!(fed.starts_with(prompt.as_bytes()));
        assert!(
            fs::read(dir.join("prompt.md"))
                .unwrap()
                .starts_with(prompt.as_bytes())
        );
    }
    #[test]
    fn real_supervisors_start_a_dependency_while_the_root_limit_is_full() {
        let e = E2e::new();
        dependency_contract(&e);
    }
    #[test]
    fn dependency_fixture_does_not_read_its_callers_plugin_configuration() {
        let mut e = E2e::new();
        let home = e.tmp.0.join("caller-home");
        let plugin = home.join(".codex/plugins/cache/openai-bundled/unified-computer-use/v1");
        fs::create_dir_all(&plugin).unwrap();
        let file = plugin.join(".mcp.json");
        let sentinel = r#"{"mcpServers":{"cua_repl":{"env_vars":"caller-owned-invalid-type"}}}"#;
        fs::write(&file, sentinel).unwrap();
        e.inherited_home = Some(home);
        dependency_contract(&e);
        assert_eq!(fs::read_to_string(file).unwrap(), sentinel);
    }
    #[test]
    fn fixture_process_handle_rejects_a_mismatched_creation_time() {
        let pid = std::process::id();
        let created = proc_created(pid).unwrap();
        assert!(FixtureProcess::retain(pid, created + 1).is_none());
        let retained = FixtureProcess::retain(pid, created).unwrap();
        assert!(retained.live());
        // Dropping the reference closes the handle; it never terminates a PID.
        drop(retained);
        assert_eq!(proc_created(pid), Some(created));
    }
    #[test]
    fn fixture_marker_must_match_its_saved_process_creation_time() {
        struct ChildGuard {
            child: std::process::Child,
            process: FixtureProcess,
        }
        impl Drop for ChildGuard {
            fn drop(&mut self) {
                let _ = self.process.stop();
                let _ = self.child.wait();
            }
        }
        let e = E2e::new();
        fs::write(e.rec.join("fixture-owner.json"), pretty(&json!({"pid":std::process::id(),"created":proc_created(std::process::id()).unwrap(),"receipt_done":true}))).unwrap();
        let child = hidden_command(&e.tmp.0.join("bin/fixture.exe").to_string_lossy())
            .env("CC_JOBS_FIXTURE_MODE", "dependency-parent")
            .env("CC_JOBS_FIXTURE_DIR", &e.rec)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .unwrap();
        let pid = child.id();
        let created = proc_created(pid).unwrap();
        let _guard = ChildGuard {
            child,
            process: FixtureProcess::retain(pid, created).unwrap(),
        };
        e.until_phase("fixture_identity_published", || {
            e.rec.join("sleep.pid").exists()
        });
        fs::write(
            e.rec.join("sleep.identity.json"),
            pretty(&json!({"pid":pid,"created":created + 1})),
        )
        .unwrap();
        e.retain_fixture_processes();
        assert!(
            e.handles.lock().unwrap().is_empty(),
            "a reused marker must not bind to the current PID"
        );
        fs::write(
            e.rec.join("sleep.identity.json"),
            pretty(&json!({"pid":pid,"created":created})),
        )
        .unwrap();
        e.retain_fixture_processes();
        assert!(
            e.handles
                .lock()
                .unwrap()
                .iter()
                .any(|handle| handle.pid == pid && handle.created == created && handle.live())
        );
        e.diagnostic.store(true, Ordering::Relaxed);
    }
    fn dependency_contract(e: &E2e) {
        e.diagnostic.store(true, Ordering::Relaxed);
        fs::write(
            e.tmp.0.join("tool/queue-config.json"),
            r#"{"max_running":1}"#,
        )
        .unwrap();
        // Distinct worktrees prevent an inherited home Git checkout from
        // making work_owner, rather than capacity 1, block the other root.
        let workspace = |name| {
            let path = e.tmp.0.join(name);
            fs::create_dir_all(path.join(".git")).unwrap();
            fs::write(path.join(".git/HEAD"), "ref: refs/heads/fixture\n").unwrap();
            fs::write(
                path.join(".git/config"),
                "[core]\nrepositoryformatversion = 0\n",
            )
            .unwrap();
            path
        };
        let parent_work = workspace("parent-work");
        let other_work = workspace("other-work");
        fs::write(e.rec.join("fixture-owner.json"), pretty(&json!({"pid":std::process::id(),"created":proc_created(std::process::id()).unwrap(),"receipt_done":true}))).unwrap();
        let (parent, parent_pids) = e.slow_at("dependency-parent", &parent_work);
        let other = e.start("", &other_work, b"other root");
        assert_eq!(e.job(&other)["status"], "queued");
        e.until_phase("other_root_waits_for_capacity", || {
            e.job(&other)["queue_wait_reason"]["code"] == "running_limit"
        });
        let prompt = e.tmp.0.join("child-prompt.md");
        fs::write(&prompt, "dependency").unwrap();
        let result = e.cli(
            &[
                "run",
                "--name",
                "dependency",
                "--model",
                "gpt-6-luna",
                "--effort",
                "max",
                "--cwd",
                parent_work.to_str().unwrap(),
                "--prompt-file",
                prompt.to_str().unwrap(),
                "--parent-job-id",
                &parent,
            ],
            "",
        );
        assert!(
            result.status.success(),
            "{}",
            String::from_utf8_lossy(&result.stderr)
        );
        let child = String::from_utf8(result.stdout).unwrap().trim().to_string();
        e.until_phase("child_success_with_full_root", || {
            e.job(&child)["status"] == "succeeded"
        });
        assert_eq!(e.job(&child)["parent_job_id"], parent);
        assert_eq!(e.job(&parent)["status"], "running");
        assert_eq!(e.job(&other)["status"], "queued");
        let cancelled = e.cli(&["cancel", &parent], "");
        assert!(
            cancelled.status.success(),
            "parent cancel {:?}: stdout={} stderr={}",
            cancelled.status.code(),
            String::from_utf8_lossy(&cancelled.stdout),
            String::from_utf8_lossy(&cancelled.stderr)
        );
        e.until_phase("parent_processes_exited", || {
            let handles = e.handles.lock().unwrap();
            parent_pids.iter().all(|pid| {
                handles
                    .iter()
                    .any(|handle| handle.pid == *pid && !handle.live())
            })
        });
        e.until_phase("other_success_after_parent_cancel", || {
            e.job(&other)["status"] == "succeeded"
        });
    }
    #[test]
    fn test_run_survives_its_caller_and_passes_prompt_and_cwd_verbatim() {
        let e = E2e::new();
        let cwd = e.tmp.0.join("日本語 R&D %OS% x");
        fs::create_dir(&cwd).unwrap();
        let prompt = "日本語のプロンプト 🚀 & ^ % \" ! <x>\r\n2行目\n".as_bytes();
        let jid = e.start("", &cwd, prompt);
        let r = e.cli(&["wait", &jid, "--timeout", "30"], "");
        assert!(
            r.status.success(),
            "{} {}",
            String::from_utf8_lossy(&r.stdout),
            String::from_utf8_lossy(&r.stderr)
        );
        assert!(String::from_utf8_lossy(&r.stdout).contains("DONE"));
        let j = e.job(&jid);
        assert_eq!(j["status"], "succeeded");
        assert_eq!(j["steps"], 2);
        assert_eq!(j["tokens"]["total"], 105710);
        let mut expected_prompt = prompt.to_vec();
        expected_prompt.extend_from_slice(cc_job_policy::READ_GUIDANCE.as_bytes());
        expected_prompt.extend_from_slice("\n\n## 最新の保存済みオーナー指示\n以下はPCの保存済み運用指示です。元の作業対象・閲覧専用指定・承認審査の範囲を広げません。GitHub所有者は実際の所有者を確認し、保護対象はscrub391 --guard-pathで除外してください。稼働中の担当は途中成果を保存し、次の区切りで反映してください。\nfixture owner instructions v1".as_bytes());
        expected_prompt.extend_from_slice("\n\n## 実行時のサイト制限\n".as_bytes());
        expected_prompt.extend_from_slice(cc_job_policy::REGISTRAR_GUIDANCE.as_bytes());
        expected_prompt.extend_from_slice(b"\n");
        assert_eq!(fs::read(e.rec.join("stdin.bin")).unwrap(), expected_prompt);
        let call: Value =
            serde_json::from_slice(&fs::read(e.rec.join("call.json")).unwrap()).unwrap();
        assert_eq!(
            s(&call, "cwd").to_lowercase(),
            cwd.to_str().unwrap().to_lowercase()
        );
        let args = call["argv"].as_array().unwrap();
        assert!(!args.iter().any(|v| v == "-C"));
        assert_eq!(args.first().unwrap(), "exec");
        assert_eq!(args.last().unwrap(), "-");
        let tail = String::from_utf8(e.cli(&["tail", &jid], "").stdout).unwrap();
        assert!(
            regex::Regex::new(
                r"\d\d:\d\d:\d\d   -> exit 0, 2 lines: Get-ChildItem -File \| Select-Object"
            )
            .unwrap()
            .is_match(&tail)
        );
        let raw = fs::read(e.tmp.0.join("tool/jobs").join(jid).join("job.json")).unwrap();
        assert!(raw.windows(2).any(|s| s == b"\r\n"));
    }
    #[test]
    fn test_finishes_even_if_a_grandchild_holds_stdout_open() {
        let e = E2e::new();
        let jid = e.start("hold", &e.tmp.0, b"hi");
        let r = e.cli(&["wait", &jid, "--timeout", "25"], "");
        assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stdout));
        // The open-pipe grandchild is still alive: completion did not wait for
        // EOF. This proves the behavior without including process startup time.
        assert!(e.rec.join("hold.pid").exists());
        assert!(
            proc_created(
                fs::read_to_string(e.rec.join("hold.pid"))
                    .unwrap()
                    .parse()
                    .unwrap()
            )
            .is_some()
        );
    }
    #[test]
    fn test_cancel_kills_the_whole_tree() {
        let e = E2e::new();
        let (jid, pids) = e.slow();
        let identities: Vec<_> = pids
            .iter()
            .map(|pid| (*pid, proc_created(*pid).expect("fixture process is live")))
            .collect();
        let r = e.cli(&["cancel", &jid], "");
        assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stderr));
        // TerminateProcess is asynchronous; a reused PID must not look like a survivor.
        e.until(|| {
            identities
                .iter()
                .all(|(pid, born)| proc_created(*pid) != Some(*born))
                && e.job(&jid)["status"] == "cancelled"
        });
        assert_eq!(e.job(&jid)["status"], "cancelled");
    }
    #[test]
    fn no_action_real_fixture_cancels_exits_and_restarts_once_in_same_workspace() {
        use cc_job_policy::action_progress::{self, Progress};
        let t=E2e::new();let repo=t.tmp.0.join("repo");
        fs::create_dir_all(repo.join(".git")).unwrap();
        fs::write(repo.join(".git/config"),"[remote \"origin\"]\nurl = https://github.com/AutoDevJapan/cc-pc-tools-rs.git\n").unwrap();
        fs::write(repo.join(".git/HEAD"),"ref: refs/heads/scheduler/todo-1-fixture\n").unwrap();
        fs::write(repo.join("unfinished.txt"),"preserve this unfinished work").unwrap();
        fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"),t.tmp.0.join("bin/codex.exe")).unwrap();
        fs::write(t.tmp.0.join("bin/codex.cmd"),b"@echo off\r\n\"%~dp0codex.exe\" %*\r\n").unwrap();
        let first_prompt=t.tmp.0.join("first.md");fs::write(&first_prompt,"harmless report-only fixture").unwrap();
        let launched=t.cli(&["run","--name","fixture-first","--model","gpt-6.1-sol","--task-type","implementation","--sandbox","danger-full-access","--cwd",repo.to_str().unwrap(),"--prompt-file",first_prompt.to_str().unwrap()],"report-only");
        assert!(launched.status.success(),"{}",String::from_utf8_lossy(&launched.stderr));
        let first=String::from_utf8(launched.stdout).unwrap().trim().to_owned();
        let events=t.tmp.0.join("tool/jobs").join(&first).join("events.jsonl");
        t.until(|| fs::read_to_string(&events).is_ok_and(|s|s.lines().count()>=4));
        let source=t.job(&first);
        let row=json!({"id":1,"project":"cc-pc-tools-rs","status":"doing","jobs":[first],"resume_generation":1,
            "worker_receipt":{"job":first,"generation":1},"scheduler":{"job":first,"worktree":repo,"branch":"scheduler/todo-1-fixture"}});
        let mut db=json!({"projects":{"cc-pc-tools-rs":repo},"items":[row]});
        let diagnostic=json!({"id_match":db["items"][0]["scheduler"]["job"]==source["id"],"owner_wait":source["waiting_for_work_owner"],"resource_wait":source["resource_wait"],"capacity_wait":source["capacity_wait_pending"],"sandbox":source["sandbox"],"pid_birth":source["pid_created"],"child_birth":source["codex_pid_created"],"started":source["started_at"],"stop":source["stopped"],"cancel":source["cancel_requested"],"work_on":source["work_wait_on"]});
        assert!(action_progress::eligible(&db,&db["items"][0],&source),"fixture is not eligible: {diagnostic}");
        assert_eq!(db["items"][0]["scheduler"]["worktree"],source["cwd"]);
        let store=t.tmp.0.join("todo/todo.json");fs::create_dir_all(store.parent().unwrap()).unwrap();fs::write(&store,db.to_string()).unwrap();
        // Virtualize only observation time, never production thresholds or OS identities.
        let p=action_progress::scan(&events,&source,&Progress::default(),System::default().epoch()-1801.).unwrap();
        let proof=t.tmp.0.join("proof.json");fs::write(&proof,json!({"ticket":action_progress::ticket(&db,&db["items"][0],&source),"progress":p}).to_string()).unwrap();
        let cancel=["cancel",&first,"--recovery","--if-todo-ready","--todo-id","1","--todo-file",store.to_str().unwrap(),"--cancel-key","no-action-fixture","--no-action-check",proof.to_str().unwrap()];
        let out=t.cli(&cancel,"report-only");assert!(out.status.success(),"{}",String::from_utf8_lossy(&out.stderr));
        assert_eq!(serde_json::from_slice::<Value>(&out.stdout).unwrap()["outcome"],"cancelled", "{}", String::from_utf8_lossy(&out.stdout));
        t.until(||proc_created(source["pid"].as_u64().unwrap() as u32)!=source["pid_created"].as_u64()
            && proc_created(source["codex_pid"].as_u64().unwrap() as u32)!=source["codex_pid_created"].as_u64());
        assert_eq!(t.job(&first)["status"],"cancelled");
        let out=t.cli(&cancel,"report-only");assert!(out.status.success());assert_eq!(serde_json::from_slice::<Value>(&out.stdout).unwrap()["outcome"],"deferred");
        db["items"][0]["status"]=json!("todo");db["items"][0]["scheduler"]["job"]=Value::Null;fs::write(&store,db.to_string()).unwrap();
        let prompt=t.tmp.0.join("resume.md");fs::write(&prompt,"same fixture continuation").unwrap();
        let args=["run","--name","fixture-resume","--model","gpt-6.1-sol","--task-type","implementation","--sandbox","danger-full-access","--todo-id","1","--cwd",repo.to_str().unwrap(),"--prompt-file",prompt.to_str().unwrap(),"--launch-key","28272827282728272827282728272827"];
        let a=t.cli(&args,"report-only");let b=t.cli(&args,"report-only");assert!(a.status.success());assert!(b.status.success());assert_eq!(a.stdout,b.stdout);
        let next=String::from_utf8(a.stdout).unwrap().trim().to_owned();assert_ne!(next,first);
        t.until(|| t.job(&next)["status"]=="running" && t.job(&next)["codex_pid"].as_u64().is_some());
        assert_eq!(t.job(&next)["cwd"],source["cwd"]);assert_eq!(t.job(&next)["todo_id"],1);
        assert_eq!(fs::read_to_string(repo.join("unfinished.txt")).unwrap(),"preserve this unfinished work");
        assert_eq!(fs::read_dir(t.tmp.0.join("tool/jobs")).unwrap().flatten().filter(|e|e.path().join("job.json").is_file()).count(),2);
        let stop=t.cli(&["cancel",&next,"--recovery"],"report-only");assert!(stop.status.success());
    }
    #[test]
    fn test_native_recovery_cancel_preserves_intent_after_process_tree_stops() {
        let e = E2e::new();
        let key = "fa8563e2a26943a8aa8e9c1af8b8d284";
        let prompt = e.tmp.0.join("prompt.md");
        fs::write(&prompt, "saved acceptance").unwrap();
        let started = e.cli(
            &[
                "run",
                "--name",
                "native-recovery",
                "--model",
                "gpt-6-luna",
                "--effort",
                "max",
                "--cwd",
                e.tmp.0.to_str().unwrap(),
                "--prompt-file",
                prompt.to_str().unwrap(),
                "--launch-key",
                key,
            ],
            "slow",
        );
        assert!(
            started.status.success(),
            "{}",
            String::from_utf8_lossy(&started.stderr)
        );
        let jid = String::from_utf8(started.stdout)
            .unwrap()
            .trim()
            .to_string();
        e.until_phase("recovery_fixture_start", || {
            e.rec.join("sleep.pid").exists() && truth(&e.job(&jid)["codex_pid"])
        });
        let before = e.job(&jid);
        assert_eq!(before["launch_key"], key);
        assert_eq!(before["process_tree_version"], 1);
        let child: u32 = fs::read_to_string(e.rec.join("sleep.pid"))
            .unwrap()
            .parse()
            .unwrap();
        let pids = [
            before["pid"].as_u64().unwrap() as u32,
            before["codex_pid"].as_u64().unwrap() as u32,
            child,
        ];
        let identities: Vec<_> = pids
            .iter()
            .map(|pid| (*pid, proc_created(*pid).expect("fixture process is live")))
            .collect();
        let result = e.cli(&["cancel", &jid, "--recovery"], "");
        if !result.status.success() {
            e.diagnostic.store(true, Ordering::Relaxed);
            e.retain_fixture_processes();
            let retained: Vec<_> = e
                .handles
                .lock()
                .unwrap()
                .iter()
                .map(|handle| (handle.pid, handle.created, handle.live()))
                .collect();
            eprintln!(
                "recovery cancellation fixture identities: expected={identities:?} retained={retained:?}"
            );
        }
        assert!(
            result.status.success(),
            "{}",
            String::from_utf8_lossy(&result.stderr)
        );
        e.until(|| {
            identities
                .iter()
                .all(|(pid, born)| proc_created(*pid) != Some(*born))
        });
        let saved = e.job(&jid);
        assert_eq!(saved["status"], "cancelled");
        assert_eq!(saved["process_tree_cancel_method"], "job_object");
        assert_ne!(saved["cancel_requested"], true);
        assert!(cc_job_policy::cancellation::recovery_cancel(
            &Value::Null,
            &saved
        ));
        for field in [
            "cwd",
            "model",
            "effort",
            "sandbox",
            "thread_id",
            "launch_key",
        ] {
            assert_eq!(saved[field], before[field], "{field}");
        }
        assert!(!cc_job_policy::cancellation::blocks_recovery(
            &json!({}),
            &saved
        ));
    }
    #[test]
    fn test_cancel_of_a_lost_job_kills_the_leftover_codex() {
        let e = E2e::new();
        let (jid, pids) = e.slow();
        assert_eq!(e.job(&jid)["process_tree_version"], 1);
        let mut owners = FixtureOwners {
            handles: pids
                .iter()
                .map(|pid| {
                    FixtureProcess::retain(
                        *pid,
                        proc_created(*pid).expect("fixture process is live"),
                    )
                    .expect("retain the owned fixture identity before cancellation")
                })
                .collect(),
            shutdown_started: false,
        };
        // Stop only the retained supervisor. Its children deliberately stay
        // alive, so this still exercises cancellation of a genuinely lost job.
        assert!(owners.handles[0].stop());
        e.until(|| {
            serde_json::from_slice::<Vec<Value>>(&e.cli(&["status", "--json"], "").stdout).unwrap()
                [0]["status"]
                == "lost"
        });
        assert!(owners.handles[1..].iter().all(FixtureProcess::live));
        let r = e.cli(&["cancel", &jid], "");
        assert!(r.status.success(), "{}", String::from_utf8_lossy(&r.stderr));
        assert!(String::from_utf8_lossy(&r.stdout).contains("killed the leftover"));
        assert_eq!(
            e.job(&jid)["process_tree_cancel_method"],
            "retained_handle_walk"
        );
        // Wait on the very same creation-checked objects retained before the
        // lost-supervisor transition; a failed reopen or reused PID proves nothing.
        assert!(owners.handles.iter().all(FixtureProcess::wait_exit));
        owners.shutdown_started = true;
    }
}
mod additional_compatibility {
    use super::*;
    #[test]
    fn cargo_path_wrapper_runs_the_rust_gate_and_preserves_streaming() {
        let t = Test::new();
        cargo_test_policy(&t.ctx.root, 1);
        let bin = t.ctx.root.join("bin");
        fs::create_dir_all(&bin).unwrap();
        fs::copy(
            env!("CARGO_BIN_EXE_cargo_gate"),
            t.ctx.root.join("cargo_gate.exe"),
        )
        .unwrap();
        fs::write(
            bin.join("cargo.cmd"),
            include_str!("../bin/cargo.cmd").replace("\n", "\r\n"),
        )
        .unwrap();
        let mut env = t.ctx.job_environment();
        // This fixture must not contend with the production shared-cache gate.
        env.insert(
            "CARGO_TARGET_DIR".into(),
            t.ctx.root.join("target").to_string_lossy().into(),
        );
        env.insert(
            "CODEX_JOBS_REAL_CARGO".into(),
            env!("CARGO_BIN_EXE_cc_jobs_fixture").into(),
        );
        env.insert(
            "CODEX_JOBS_CARGO_LOCK_DIR".into(),
            t.ctx.root.to_string_lossy().into(),
        );
        assert_eq!(
            cc_jobs::platform::which("cargo", &env),
            Some(bin.join("cargo.cmd").to_string_lossy().into())
        );
        let mut c = hidden_command("cmd.exe");
        c.args(["/d", "/s", "/c", "cargo --echo-env"])
            .envs(env)
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD");
        let out = c.output().unwrap();
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert_eq!(String::from_utf8(out.stdout).unwrap().trim(), "1");
        assert!(t.ctx.root.join("cargo-build.lock.0").exists());
    }
    #[test]
    fn win32_sharing_and_lock_violations_retry_reads_and_replacement_fifty_times() {
        let t = Test::new();
        let j = t.put(new_job());
        let path = t.ctx.job_dir(s(&j, "id")).join("job.json");
        for code in [5, 32, 33] {
            t.rt.replace_os.store(code, Ordering::Relaxed);
            t.rt.replace_failures.store(2, Ordering::Relaxed);
            let before = t.rt.replace_count.load(Ordering::Relaxed);
            t.ctx.save(&j).unwrap();
            assert_eq!(t.rt.replace_count.load(Ordering::Relaxed) - before, 3);
        }
        t.rt.load_failures.store(2, Ordering::Relaxed);
        let before = t.rt.load_count.load(Ordering::Relaxed);
        assert_eq!(t.ctx.read_json(&path).unwrap(), j);
        assert_eq!(t.rt.load_count.load(Ordering::Relaxed) - before, 3);
        t.rt.replace_failures.store(50, Ordering::Relaxed);
        let before = t.rt.replace_count.load(Ordering::Relaxed);
        assert!(t.ctx.save(&j).is_err());
        assert_eq!(t.rt.replace_count.load(Ordering::Relaxed) - before, 50);
        t.rt.load_failures.store(50, Ordering::Relaxed);
        let before = t.rt.load_count.load(Ordering::Relaxed);
        assert!(t.ctx.read_json(&path).is_err());
        assert_eq!(t.rt.load_count.load(Ordering::Relaxed) - before, 50);
    }
    #[test]
    fn unc_paths_keep_their_network_root_when_normalized() {
        assert_eq!(
            cc_jobs::cli::normalize_windows_path(Path::new(r"\\?\UNC\server\share\日本語")),
            PathBuf::from(r"\\server\share\日本語")
        );
        assert_eq!(
            cc_jobs::cli::normalize_windows_path(Path::new(r"\\?\D:\work")),
            PathBuf::from(r"D:\work")
        );
    }
    #[test]
    fn secret_keys_are_case_insensitive_and_pumps_cache_redaction_patterns() {
        let t = Test::new();
        t.rt.env
            .lock()
            .unwrap()
            .insert("godd_discord_bot_token".into(), "fake-case-token".into());
        let env = t.ctx.job_environment();
        assert!(
            !env.keys()
                .any(|k| k.eq_ignore_ascii_case("GODD_DISCORD_BOT_TOKEN"))
        );
        assert_eq!(
            redact_local_log_secrets("FAKE-CASE-TOKEN", &t.rt.env()),
            "[秘密]"
        );
        let p = t.ctx.root.join("case.log");
        pump_stderr(
            b"FAKE-CASE-TOKEN\nfake-case-token\n".as_slice(),
            &p,
            t.rt.as_ref(),
        )
        .unwrap();
        assert_eq!(fs::read_to_string(p).unwrap(), "[秘密]\n[秘密]\n");
    }
    #[test]
    fn queued_supervisor_setup_failure_is_recorded_as_failed() {
        let t = Test::new();
        let j = t.put(new_job());
        fs::create_dir(t.ctx.job_dir(s(&j, "id")).join("state.lock")).unwrap();
        t.ctx.supervise(s(&j, "id")).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], "failed");
        assert!(s(&saved, "last_error").starts_with("supervisor error:"));
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
    }
    #[test]
    fn real_live_process_with_reused_pid_is_never_killed() {
        let t = Test::new();
        let mut c = hidden_command(env!("CARGO_BIN_EXE_cc_jobs_fixture"));
        c.arg("--sleep").env("CC_JOBS_FIXTURE_DIR", &t.ctx.root);
        let mut child = c.spawn().unwrap();
        let pid = child.id();
        let created = proc_created(pid).unwrap();
        let mut ctx = t.ctx.clone();
        ctx.rt = Arc::new(System::default());
        ctx.gto = t.ctx.root.join("missing-gto.exe");
        let j=t.put(job(json!({"status":"running","pid":pid,"pid_created":created+1,"codex_pid":pid,"codex_pid_created":created+1})));
        let mut out = vec![];
        let mut err = vec![];
        let result = ctx.cmd_cancel(s(&j, "id"), &mut out, &mut err);
        let alive = child.try_wait().unwrap().is_none();
        child.kill().unwrap();
        child.wait().unwrap();
        assert_eq!(result.unwrap(), 0);
        assert!(alive);
        assert!(
            String::from_utf8(out)
                .unwrap()
                .contains("no leftover process to kill")
        );
        assert_eq!(ctx.load(s(&j, "id")).unwrap()["status"], "lost");
    }
    #[test]
    fn supervisor_drains_events_arriving_during_the_final_process_poll() {
        struct LateReader {
            release: Arc<AtomicBool>,
            eof: Arc<AtomicBool>,
            bytes: Cursor<Vec<u8>>,
        }
        impl Read for LateReader {
            fn read(&mut self, b: &mut [u8]) -> io::Result<usize> {
                let deadline = std::time::Instant::now() + Duration::from_secs(5);
                while !self.release.load(Ordering::Acquire) {
                    if std::time::Instant::now() >= deadline {
                        return Err(io::Error::new(
                            io::ErrorKind::TimedOut,
                            "late reader was not released",
                        ));
                    }
                    std::thread::sleep(Duration::from_millis(1));
                }
                let n = self.bytes.read(b)?;
                if n == 0 {
                    self.eof.store(true, Ordering::Release);
                }
                Ok(n)
            }
        }
        struct LateChild {
            release: Arc<AtomicBool>,
            eof: Arc<AtomicBool>,
            reader: Option<Box<dyn Read + Send>>,
        }
        impl Running for LateChild {
            fn pid(&self) -> u32 {
                321
            }
            fn stdout(&mut self) -> Option<Box<dyn Read + Send>> {
                self.reader.take()
            }
            fn stderr(&mut self) -> Option<Box<dyn Read + Send>> {
                None
            }
            fn kill(&mut self) -> io::Result<()> {
                Ok(())
            }
            fn poll(&mut self) -> io::Result<Option<i64>> {
                self.release.store(true, Ordering::Release);
                let deadline = std::time::Instant::now() + Duration::from_secs(5);
                while !self.eof.load(Ordering::Acquire) {
                    if std::time::Instant::now() >= deadline {
                        return Err(io::Error::new(
                            io::ErrorKind::TimedOut,
                            "late reader did not reach EOF",
                        ));
                    }
                    std::thread::sleep(Duration::from_millis(1));
                }
                Ok(Some(0))
            }
        }
        let t = Test::new();
        let j = t.put(job(json!({"codex":"codex.CMD","cwd":t.ctx.root})));
        fs::write(t.ctx.job_dir(s(&j, "id")).join("prompt.md"), b"hi").unwrap();
        // Startup can probe a child without an identity before starting its
        // reader. This fixture represents a known child, so defer poll to the
        // final drain boundary it is intended to exercise.
        t.rt.procs.lock().unwrap().insert(321, 456);
        *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(|| {
            let release = Arc::new(AtomicBool::new(false));
            let eof = Arc::new(AtomicBool::new(false));
            let bytes=b"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"late result\"}}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}\n".to_vec();
            Box::new(LateChild {
                release: release.clone(),
                eof: eof.clone(),
                reader: Some(Box::new(LateReader {
                    release,
                    eof,
                    bytes: Cursor::new(bytes),
                })),
            })
        }));
        t.ctx.supervise(s(&j, "id")).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["status"], "succeeded");
        assert_eq!(saved["last_message"], "late result");
        assert_eq!(saved["tokens"]["total"], 3);
    }
    #[test]
    fn help_and_parser_errors_match_original_python_bytes_and_codes() {
        let t = Test::new();
        let cases: Vec<Value> =
            serde_json::from_str(include_str!("fixtures/cli_parity.json")).unwrap();
        assert_eq!(cases.len(), 29);
        for case in cases {
            let args = case["args"]
                .as_array()
                .unwrap()
                .iter()
                .map(|v| v.as_str().unwrap())
                .collect::<Vec<_>>();
            let (r, out, mut err) = t.cli(&args);
            let code = match r {
                Ok(code) => code,
                Err(e) => {
                    err.push_str(&e.message);
                    err.push('\n');
                    e.code
                }
            };
            assert_eq!(code, case["code"].as_i64().unwrap() as i32, "{args:?}");
            // Preserve the historical Python baseline, allowing only the new
            // explicitly documented finite retry option in run's help.
            let legacy_out = if args.first() == Some(&"run") && args.iter().any(|arg| ["-h","--help"].contains(arg)) {
                let usage="                         [--original-product-retry-key ORIGINAL_PRODUCT_RETRY_KEY]\n";
                let option="  --original-product-retry-key ORIGINAL_PRODUCT_RETRY_KEY\n                        reserved retry key for original ToDo 1626 only (32\n                        lowercase hex); requires the same --launch-key and\n                        waits for product-retry-bind before execution\n";
                assert_eq!(out.matches(usage).count(),1);
                assert_eq!(out.matches(option).count(),1);
                out.replace(usage,"").replace(option,"")
            }else{out};
            assert_eq!(legacy_out, s(&case, "stdout"), "{args:?} stdout");
            let legacy_err=if args.first()==Some(&"run"){
                err.replace("                         [--original-product-retry-key ORIGINAL_PRODUCT_RETRY_KEY]\n","")
            }else{err};
            assert_eq!(legacy_err, s(&case, "stderr"), "{args:?} stderr");
        }
        assert!(t.rt.calls.lock().unwrap().is_empty());
        assert!(t.ctx.list_jobs(&HashSet::new()).is_empty());
    }
    #[test]
    fn windows_fault_exit_is_unsigned_in_process_result_and_job_json() {
        let rt = System::default();
        let reply = rt
            .call(Call::new(
                vec![
                    env!("CARGO_BIN_EXE_cc_jobs_fixture").into(),
                    "--fault-exit".into(),
                ],
                vec![],
                5.,
            ))
            .unwrap();
        assert_eq!(reply.code, 3221225477);
        let t = Test::new();
        let mut j = t.put(new_job());
        finish(&mut j, Some(reply.code), 10, &t.rt.now());
        t.ctx.save(&j).unwrap();
        let saved = t.ctx.load(s(&j, "id")).unwrap();
        assert_eq!(saved["exit_code"], 3221225477_i64);
        assert_eq!(
            saved["last_error"],
            "codex exited with 3221225477 without turn.completed"
        );
    }
    #[test]
    fn state_json_uses_python_indentation_order_crlf_and_unicode() {
        let t = Test::new();
        let j = json!({"id":"format","status":"queued","name":"日本語🚀","tokens":{"input_tokens":1,"total":2}});
        t.put(j);
        assert_eq!(fs::read(t.ctx.job_dir("format").join("job.json")).unwrap(),"{\r\n \"id\": \"format\",\r\n \"status\": \"queued\",\r\n \"name\": \"日本語🚀\",\r\n \"tokens\": {\r\n  \"input_tokens\": 1,\r\n  \"total\": 2\r\n }\r\n}".as_bytes());
    }
    #[test]
    fn resume_uuid_and_stdin_global_options_preserve_original_contract() {
        let t = Test::new();
        assert!(
            t.run(&["--resume-session", "not-a-session"], b"hi")
                .unwrap_err()
                .message
                .contains("session UUID")
        );
        let id = "01a0f188-0c24-7860-ab46-6c424b1def08";
        t.run(&["--resume-session", &id.to_uppercase()], b"hi")
            .unwrap();
        let jobs = t.ctx.list_jobs(&HashSet::new());
        assert_eq!(jobs[0]["resume_session"], id);
        let args = codex_args(&jobs[0]);
        assert_eq!(&args[args.len() - 3..], ["resume", id, "-"]);
        let date = t.ctx.codex_home.join("sessions/2020/01/01");
        fs::create_dir_all(&date).unwrap();
        let path = date.join(format!("rollout-old-{id}.jsonl"));
        fs::write(&path, b"").unwrap();
        let j = patch(
            jobs[0].clone(),
            json!({"thread_id":"new","started_at":t.rt.now()}),
        );
        assert_eq!(t.ctx.session_file(&j), Some(path));
    }
    #[test]
    fn msvcrt_byte_lock_and_rust_lock_exclude_each_other_without_changing_file() {
        #[link(name = "msvcrt")]
        unsafe extern "C" {
            fn _open(path: *const std::ffi::c_char, flags: i32, ...) -> i32;
            fn _locking(fd: i32, mode: i32, n: i64) -> i32;
            fn _lseek(fd: i32, offset: i32, origin: i32) -> i32;
            fn _close(fd: i32) -> i32;
        }
        let tmp = Temp::new();
        let path = tmp.0.join("crt.lock");
        fs::write(&path, b"sentinel").unwrap();
        let name = std::ffi::CString::new(path.to_str().unwrap()).unwrap();
        unsafe {
            let fd = _open(name.as_ptr(), 0x0002 | 0x8000 | 0x0080);
            assert!(fd >= 0);
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 2, 1), 0);
            assert!(FileLock::try_acquire(&path).is_err());
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 0, 1), 0);
            let lock = FileLock::try_acquire(&path).unwrap();
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_ne!(_locking(fd, 2, 1), 0);
            drop(lock);
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 2, 1), 0);
            assert_eq!(_lseek(fd, 0, 0), 0);
            assert_eq!(_locking(fd, 0, 1), 0);
            assert_eq!(_close(fd), 0);
        }
        assert_eq!(fs::read(path).unwrap(), b"sentinel");
    }
    #[test]
    fn cargo_unlocked_commands_and_nested_gate_do_not_consume_a_slot() {
        let env = BTreeMap::new();
        for command in cc_jobs::gate::UNLOCKED {
            assert!(!cc_jobs::gate::needs_lock(&[command.to_string()], &env));
        }
        assert!(cc_jobs::gate::needs_lock(
            &["--release".into(), "build".into()],
            &env
        ));
        for args in [
            vec!["+stable", "fmt"],
            vec!["--config", "x.toml", "metadata"],
            vec!["nextest", "--version"],
            vec!["+stable", "nextest", "--help"],
        ] {
            assert!(!cc_jobs::gate::build_like(
                &args.into_iter().map(String::from).collect::<Vec<_>>()
            ));
        }
        assert!(!cc_jobs::gate::needs_lock(
            &["build".into()],
            &BTreeMap::from([("CODEX_JOBS_CARGO_LOCK_HELD".into(), "1".into())])
        ));
        let tmp = Temp::new();
        let mut c = hidden_command(env!("CARGO_BIN_EXE_cargo_gate"));
        c.args(["--echo-env"])
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env("CODEX_JOBS_CARGO_LOCK_DIR", &tmp.0)
            .env("CARGO_TARGET_DIR", &tmp.0)
            .env("CODEX_JOBS_CARGO_LOCK_HELD", "1")
            // An outer gate from before per-target locks records no held target.
            .env_remove(cc_jobs::gate::HELD_TARGET);
        let out = c.output().unwrap();
        assert!(out.status.success());
        assert_eq!(String::from_utf8(out.stdout).unwrap().trim(), "1");
        assert_eq!(fs::read_dir(&tmp.0).unwrap().count(), 0);
    }
    #[test]
    fn external_failure_stderr_never_leaks_credentials() {
        let t = Test::new();
        fs::copy(env!("CARGO_BIN_EXE_codex_jobs"), &t.ctx.executable).unwrap();
        let fake = t.ctx.root.join("fake-gto.exe");
        fs::copy(env!("CARGO_BIN_EXE_cc_jobs_fixture"), &fake).unwrap();
        let j = t.put(job(json!({"status":"succeeded","duration_ms":1000})));
        let mut c = hidden_command(t.ctx.executable.to_str().unwrap());
        c.args(["rate", s(&j, "id"), "--quality", "4"])
            .env("CODEX_JOBS_GTO_BIN", fake)
            .env("CC_JOBS_FIXTURE_MODE", "external-error")
            .env("CC_JOBS_FIXTURE_DIR", &t.ctx.root)
            .env("GODD_DISCORD_BOT_TOKEN", "SENTINEL-TOKEN");
        let out = c.output().unwrap();
        assert_eq!(out.status.code(), Some(1));
        assert_eq!(
            String::from_utf8(out.stderr).unwrap().replace("\r\n", "\n"),
            "gto ingest-turn failed (1)\n"
        );
        assert!(out.stdout.is_empty());
        assert!(t.ctx.load(s(&j, "id")).unwrap().get("gto").is_none());
    }
}
