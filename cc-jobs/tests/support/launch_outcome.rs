use super::*;
use cc_job_policy::launch_outcome::{JobState, Kind, Outcome};

fn typed(t: &Test, key: &str) -> Outcome {
    let prompt = t.ctx.root.join("typed-prompt.md");
    if !prompt.exists() {
        fs::write(&prompt, b"finite fixture").unwrap();
    }
    let (result, out, _) = t.cli(&[
        "run",
        "--name",
        "typed",
        "--launch-key",
        key,
        "--launch-outcome",
        "--cwd",
        t.ctx.root.to_str().unwrap(),
        "--prompt-file",
        prompt.to_str().unwrap(),
    ]);
    assert_eq!(result.unwrap(), 0);
    serde_json::from_str(&out).unwrap()
}

#[test]
fn typed_launch_returns_queued_then_terminal_no_action_with_exact_id() {
    let t = Test::new();
    let key = "ac".repeat(16);
    let first = typed(&t, &key);
    assert_eq!(first.kind, Kind::Queued);
    assert_eq!(first.job_state, JobState::Queued);
    assert_eq!(first.launch_key.as_deref(), Some(key.as_str()));
    assert_eq!(
        first.dispatch_sha256,
        Some(cc_job_policy::product_retry::digest(b"finite fixture"))
    );
    assert_eq!(
        first.received_sha256,
        Some(cc_job_policy::product_retry::digest(
            &fs::read(t.ctx.job_dir(&first.job_id).join("prompt.md")).unwrap()
        ))
    );
    let mut job = t.ctx.load(&first.job_id).unwrap();
    job["status"] = json!("failed");
    job["queue_restart_pending"] = json!(false);
    job["reboot_launch_pending"] = json!(false);
    job["todo_resume_generation"] = json!(7);
    t.ctx.save(&job).unwrap();
    let before = fs::read(t.ctx.job_dir(&first.job_id).join("job.json")).unwrap();
    *t.rt.load_hook.lock().unwrap() = Some(Box::new(|path, _| {
        assert!(
            !path.ends_with("typed-prompt.md"),
            "replay must not inspect input bodies"
        );
    }));
    let again = typed(&t, &key);
    assert_eq!(again.kind, Kind::ExistingJobNoAction);
    assert!(again.terminal_no_action());
    assert_eq!(again.job_id, first.job_id);
    assert_eq!(again.job_resume_generation, Some(7));
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert_eq!(
        fs::read(t.ctx.job_dir(&first.job_id).join("job.json")).unwrap(),
        before
    );
    assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
}

#[test]
fn typed_live_queued_and_restart_pending_replays_do_not_issue_another_supervisor() {
    for (status, restart, reboot) in [
        ("running", false, false),
        ("queued", false, false),
        ("queued", true, false),
        ("queued", true, true),
    ] {
        let t = Test::new();
        let key = "bc".repeat(16);
        let first = typed(&t, &key);
        let mut job = t.ctx.load(&first.job_id).unwrap();
        job["status"] = json!(status);
        job["queue_restart_pending"] = json!(restart);
        job["reboot_launch_pending"] = json!(reboot);
        if status == "running" {
            job["pid"] = json!(std::process::id());
            job["pid_created"] = json!(123);
            assert!(
                t.ctx.running_job(&job),
                "the replay fixture must be a healthy live owner"
            );
        }
        t.ctx.save(&job).unwrap();
        let again = typed(&t, &key);
        assert_eq!(again.kind, Kind::ExistingJobNoAction);
        assert_eq!(again.queue_restart_pending, Some(restart));
        assert_eq!(again.reboot_launch_pending, Some(reboot));
        assert!(!again.terminal_no_action());
        assert!(again.reserved());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    }
}

#[test]
fn typed_response_never_claims_registration_when_canonical_publication_fails() {
    let t = Test::new();
    *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(|path, _| {
        if path.file_name().is_some_and(|p| p == "job.json") {
            return Err(io::Error::other("fixture failure"));
        }
        Ok(())
    }));
    let prompt = t.ctx.root.join("prompt.md");
    fs::write(&prompt, "fixture").unwrap();
    let (result, out, _) = t.cli(&[
        "run",
        "--name",
        "typed",
        "--launch-key",
        &"cd".repeat(16),
        "--launch-outcome",
        "--prompt-file",
        prompt.to_str().unwrap(),
    ]);
    assert!(result.is_err());
    assert!(out.is_empty());
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
}

#[test]
fn typed_concurrent_same_key_has_one_queued_publication_and_no_duplicate_effect() {
    let t = Test::new();
    let key = "de".repeat(16);
    let prompt = t.ctx.root.join("concurrent.md");
    fs::write(&prompt, "fixture").unwrap();
    let args = cc_jobs::cli::parse(
        &[
            "run",
            "--name",
            "typed",
            "--launch-key",
            &key,
            "--launch-outcome",
            "--cwd",
            t.ctx.root.to_str().unwrap(),
            "--prompt-file",
            prompt.to_str().unwrap(),
        ]
        .iter()
        .map(|v| v.to_string())
        .collect::<Vec<_>>(),
    )
    .unwrap();
    let barrier = std::sync::Barrier::new(4);
    let outcomes = std::thread::scope(|scope| {
        let handles = (0..4)
            .map(|_| {
                scope.spawn(|| {
                    barrier.wait();
                    let mut out = vec![];
                    t.ctx.cmd_run(&args, &mut out, &mut vec![]).unwrap();
                    serde_json::from_slice::<Outcome>(&out).unwrap()
                })
            })
            .collect::<Vec<_>>();
        handles
            .into_iter()
            .map(|h| h.join().unwrap())
            .collect::<Vec<_>>()
    });
    assert_eq!(
        outcomes.iter().filter(|o| o.kind == Kind::Queued).count(),
        1
    );
    assert_eq!(
        outcomes
            .iter()
            .filter(|o| o.kind == Kind::ExistingJobNoAction)
            .count(),
        3
    );
    assert!(outcomes.iter().all(|o| o.job_id == outcomes[0].job_id));
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
}

#[test]
fn failed_spawn_preserves_canonical_receipt_and_replay_has_no_second_effect() {
    let t = Test::new();
    let key = "ef".repeat(16);
    t.rt.supervisor_fail.store(true, Ordering::Relaxed);
    assert!(
        t.run(&["--launch-key", &key, "--launch-outcome"], b"fixture")
            .is_err()
    );
    let canonical = t
        .ctx
        .read_json(&t.ctx.jobs.join(format!(".launch-{key}.json")))
        .unwrap();
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    t.rt.supervisor_fail.store(false, Ordering::Relaxed);
    let outcome = typed(&t, &key);
    assert_eq!(outcome.job_id, canonical["id"]);
    assert_eq!(outcome.kind, Kind::ExistingJobNoAction);
    assert_eq!(outcome.job_state, JobState::Queued);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 1);
    assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
}

#[test]
fn normal_registration_preserves_request_generation_attempt_and_reserved_worker_generation() {
    let t = Test::new();
    fs::write(
        t.ctx.root.join("todo.json"),
        json!({"projects":{"fixture":t.ctx.root},"items":[
        {"id":77,"project":"fixture","status":"todo","dispatchable":true,"resume_generation":6,
        "jobs":[],"wait_on":[],"scheduler":{"attempt":4}}]})
        .to_string(),
    )
    .unwrap();
    t.run(
        &[
            "--todo-id",
            "77",
            "--launch-key",
            &"fa".repeat(16),
            "--launch-outcome",
        ],
        b"fixture",
    )
    .unwrap();
    let job = t
        .ctx
        .list_jobs(&HashSet::new())
        .into_iter()
        .find(|j| j["todo_id"] == 77)
        .unwrap();
    let outcome = Outcome::from_job(Kind::Queued, &job);
    assert_eq!(outcome.registration_generation, Some(6));
    assert_eq!(outcome.registration_attempt, Some(4));
    assert_eq!(outcome.job_resume_generation, Some(7));
}

#[test]
fn registration_cancelled_before_supervisor_issue_returns_accepted() {
    let t = Test::new();
    *t.rt.load_hook.lock().unwrap() = Some(Box::new(|path, _| {
        if path.file_name().is_some_and(|name| name == "job.json") && path.is_file() {
            let mut value: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
            value["status"] = json!("cancelled");
            value["cancel_requested"] = json!(true);
            fs::write(path, serde_json::to_vec(&value).unwrap()).unwrap();
        }
    }));
    let outcome = typed(&t, &"fb".repeat(16));
    assert_eq!(outcome.kind, Kind::Accepted);
    assert_eq!(outcome.job_state, JobState::Terminal);
    assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
}

#[cfg(windows)]
#[test]
fn actual_cmd_run_canonicalizes_junction_cwd_before_fingerprinting() {
    use std::os::windows::process::CommandExt;
    let t = Test::new();
    let real = t.ctx.root.join("real-cwd");
    let alias = t.ctx.root.join("alias-cwd");
    fs::create_dir(&real).unwrap();
    let output = std::process::Command::new("cmd.exe")
        .creation_flags(0x08000000)
        .args(["/d", "/c", "mklink", "/J"])
        .arg(&alias)
        .arg(&real)
        .output()
        .unwrap();
    assert!(output.status.success(), "{output:?}");
    t.run(
        &[
            "--cwd",
            alias.to_str().unwrap(),
            "--launch-key",
            &"fc".repeat(16),
            "--launch-outcome",
        ],
        b"fixture",
    )
    .unwrap();
    let job = t.ctx.list_jobs(&HashSet::new()).into_iter().next().unwrap();
    assert_eq!(
        Outcome::from_job(Kind::Queued, &job).cwd_sha256,
        cc_job_policy::launch_outcome::path_fingerprint(
            &fs::canonicalize(&real).unwrap().to_string_lossy()
        )
    );
}
