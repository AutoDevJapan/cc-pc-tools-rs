use super::*;

fn setup() -> (Test, Value, Vec<u8>) {
    let t = Test::new();
    let prompt = b"fixed prepared task with ORIGINAL_REPLY".to_vec();
    let root = json!(t.ctx.root);
    let mut row = json!({"id":1,"project":"p","title":"owned task","created_s":1,"spec":"spec.md",
        "status":"todo","jobs":[],"resume_generation":2,"worker_receipt":{"job":null,"generation":2},
        "scheduler":{"key":"a".repeat(32),"phase":"prepared","owner_instruction":"ORIGINAL_REPLY"}});
    row["scheduler"]["prepared_input"] =
        cc_job_policy::prepared_input::receipt(&row, &root, &prompt);
    let db = json!({"projects":{"p":root},"items":[row]});
    fs::write(t.ctx.root.join("todo.json"), db.to_string()).unwrap();
    (t, db, prompt)
}
fn register(t: &Test, db: &Value, prompt: &[u8]) -> Result<i32> {
    let proof = &db["items"][0]["scheduler"]["prepared_input"];
    t.run(
        &[
            "--todo-id",
            "1",
            "--project",
            "p",
            "--launch-key",
            s(proof, "launch_key"),
            "--scheduler-input-sha256",
            s(proof, "input_sha256"),
            "--scheduler-prompt-sha256",
            s(proof, "source_prompt_sha256"),
        ],
        prompt,
    )
}
#[test]
fn prepared_registration_rejects_changed_input_scope_and_prompt_without_supervisor() {
    for change in ["input", "scope", "prompt"] {
        let (t, mut db, mut prompt) = setup();
        match change {
            "input" => db["items"][0]["scheduler"]["owner_instruction"] = json!("CORRECTED_REPLY"),
            "scope" => db["projects"]["p"] = json!("D:/different"),
            _ => prompt.push(b'!'),
        }
        fs::write(t.ctx.root.join("todo.json"), db.to_string()).unwrap();
        assert!(register(&t, &db, &prompt).is_err(), "{change}");
        assert!(t.ctx.list_jobs(&HashSet::new()).is_empty());
        assert_eq!(t.rt.supervisors.load(Ordering::Relaxed), 0);
    }
}
#[test]
fn prepared_queue_rechecks_latest_input_prompt_hold_and_replay() {
    for change in ["unchanged", "input", "hold", "prompt", "receipt"] {
        let (t, mut db, prompt) = setup();
        register(&t, &db, &prompt).unwrap();
        let mut job = t.ctx.list_jobs(&HashSet::new()).remove(0);
        let id = s(&job, "id").to_owned();
        match change {
            "input" => db["items"][0]["scheduler"]["owner_instruction"] = json!("CORRECTED_REPLY"),
            "hold" => db["items"][0]["owner_stopped"] = json!(true),
            "prompt" => fs::write(s(&job, "prompt"), b"wrong stored prompt").unwrap(),
            "receipt" => {
                job.as_object_mut().unwrap().remove("prepared_input");
                t.ctx.save(&job).unwrap();
            }
            _ => (),
        }
        fs::write(t.ctx.root.join("todo.json"), db.to_string()).unwrap();
        let admitted = t.ctx.admit(&id).unwrap();
        let saved = t.ctx.load(&id).unwrap();
        assert_eq!(admitted.is_some(), change == "unchanged", "{change}");
        if change == "unchanged" {
            assert_eq!(saved["prepared_input_admitted"], job["prepared_input"]);
            drop(admitted);
            assert!(t.ctx.admit(&id).unwrap().is_none());
        }
        if change == "input" {
            assert_eq!(saved["status"], "failed");
            assert_eq!(saved["last_failure_kind"], "stale_prepared_input");
            assert!(saved["started_at"].is_null());
        }
        assert_eq!(
            t.rt.spawns.load(Ordering::Relaxed),
            0,
            "no model process in this acceptance test"
        );
    }
}
#[test]
fn prepared_launch_key_replay_never_relabels_the_old_prompt() {
    let (t, db, prompt) = setup();
    register(&t, &db, &prompt).unwrap();
    register(&t, &db, &prompt).unwrap();
    let old = t.ctx.list_jobs(&HashSet::new());
    assert_eq!(old.len(), 1);
    let proof = &db["items"][0]["scheduler"]["prepared_input"];
    assert!(
        t.run(
            &[
                "--todo-id",
                "1",
                "--project",
                "p",
                "--launch-key",
                s(proof, "launch_key"),
                "--scheduler-input-sha256",
                &"b".repeat(64),
                "--scheduler-prompt-sha256",
                s(proof, "source_prompt_sha256")
            ],
            &prompt
        )
        .is_err()
    );
    assert_eq!(t.ctx.list_jobs(&HashSet::new()).len(), 1);
    assert!(
        t.run(
            &[
                "--todo-id",
                "1",
                "--project",
                "p",
                "--launch-key",
                s(proof, "launch_key"),
                "--scheduler-prompt-sha256",
                s(proof, "source_prompt_sha256")
            ],
            &prompt
        )
        .is_err()
    );
    let mut corrupted = old[0].clone();
    corrupted.as_object_mut().unwrap().remove("prepared_input");
    t.ctx.save(&corrupted).unwrap();
    assert!(
        t.run(
            &[
                "--todo-id",
                "1",
                "--project",
                "p",
                "--launch-key",
                s(proof, "launch_key")
            ],
            &prompt
        )
        .is_err()
    );
}

#[test]
fn prepared_running_publication_keeps_the_canonical_writer_lock() {
    let (t, db, prompt) = setup();
    register(&t, &db, &prompt).unwrap();
    let id = s(&t.ctx.list_jobs(&HashSet::new())[0], "id").to_owned();
    let lock = t.ctx.root.join("todo.lock");
    let observed = Arc::new(AtomicBool::new(false));
    let seen = observed.clone();
    *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |_, job| {
        if job["status"] == "running" {
            assert!(
                FileLock::try_acquire(&lock).is_err(),
                "canonical input/stop writer could race running publication"
            );
            seen.store(true, Ordering::Relaxed);
        }
        Ok(())
    }));
    let admission = t.ctx.admit(&id).unwrap();
    assert!(admission.is_some());
    assert!(observed.load(Ordering::Relaxed));
    assert_eq!(
        t.ctx.load(&id).unwrap()["prepared_input_admitted"],
        db["items"][0]["scheduler"]["prepared_input"]
    );
}

#[test]
fn prepared_admission_requires_the_store_and_recovers_after_restore() {
    for change in [
        "missing",
        "corrupt",
        "missing_task",
        "stopped",
        "other_owner",
    ] {
        let (t, db, prompt) = setup();
        register(&t, &db, &prompt).unwrap();
        let id = s(&t.ctx.list_jobs(&HashSet::new())[0], "id").to_owned();
        let path = t.ctx.root.join("todo.json");
        let mut changed = db.clone();
        match change {
            "missing" => fs::remove_file(&path).unwrap(),
            "corrupt" => fs::write(&path, b"{").unwrap(),
            _ => {
                match change {
                    "missing_task" => changed["items"] = json!([]),
                    "stopped" => changed["items"][0]["owner_stopped"] = json!(true),
                    _ => changed["items"][0]["scheduler"]["job"] = json!("unrelated"),
                };
                fs::write(&path, changed.to_string()).unwrap();
            }
        }
        let admitted = t.ctx.admit(&id);
        assert!(
            !matches!(admitted, Ok(Some(_))),
            "prepared admitted without current proof: {change}"
        );
        let saved = t.ctx.load(&id).unwrap();
        assert_eq!(saved["status"], "queued");
        assert!(saved.get("prepared_input_admitted").is_none());
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 0);
        fs::write(&path, db.to_string()).unwrap();
        assert!(
            t.ctx.admit(&id).unwrap().is_some(),
            "restored canonical proof cannot resume: {change}"
        );
    }
}

#[test]
fn ordinary_manual_admission_keeps_its_store_independent_path() {
    let t = Test::new();
    t.run(&[], b"ordinary manual task").unwrap();
    assert!(!t.ctx.root.join("todo.json").exists());
    let id = s(&t.ctx.list_jobs(&HashSet::new())[0], "id").to_owned();
    assert!(t.ctx.admit(&id).unwrap().is_some());
}

#[test]
fn prepared_supervisor_waits_for_the_store_before_any_spawn() {
    let (t, db, prompt) = setup();
    register(&t, &db, &prompt).unwrap();
    let id = s(&t.ctx.list_jobs(&HashSet::new())[0], "id").to_owned();
    let store = t.ctx.root.join("todo.json");
    fs::remove_file(&store).unwrap();
    let checked = Arc::new(AtomicBool::new(false));
    let observed = checked.clone();
    let runtime = Arc::downgrade(&t.rt);
    let job_path = t.ctx.job_dir(&id).join("job.json");
    *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |_| {
        if !observed.swap(true, Ordering::Relaxed) {
            assert_eq!(runtime.upgrade().unwrap().spawns.load(Ordering::Relaxed), 0);
            let saved: Value = serde_json::from_slice(&fs::read(&job_path).unwrap()).unwrap();
            assert_eq!(saved["status"], "queued");
            assert_eq!(
                saved["queue_wait_reason"]["code"],
                "prepared_input_store_unavailable"
            );
            assert!(saved["prepared_input_admitted"].is_null());
            fs::write(&store, db.to_string()).unwrap();
        }
    }));
    t.ctx.supervise(&id).unwrap();
    assert!(
        checked.load(Ordering::Relaxed),
        "supervisor must have waited"
    );
    assert_eq!(
        t.rt.spawns.load(Ordering::Relaxed),
        1,
        "restored proof resumes once"
    );
}

#[test]
fn prepared_running_is_the_input_boundary_and_later_edit_stays_pending() {
    let (t, db, prompt) = setup();
    register(&t, &db, &prompt).unwrap();
    let id = s(&t.ctx.list_jobs(&HashSet::new())[0], "id").to_owned();
    let published = Arc::new(AtomicBool::new(false));
    let observed = published.clone();
    *t.rt.save_value_hook.lock().unwrap() = Some(Box::new(move |_, job| {
        if job["status"] == "running" {
            observed.store(true, Ordering::Relaxed);
        }
        Ok(())
    }));
    let edited = Arc::new(AtomicBool::new(false));
    let seen = edited.clone();
    let job_path = t.ctx.job_dir(&id).join("job.json");
    let store = t.ctx.root.join("todo.json");
    let lock = t.ctx.root.join("todo.lock");
    *t.rt.load_hook.lock().unwrap() = Some(Box::new(move |path, _| {
        if path == job_path
            && published.load(Ordering::Relaxed)
            && !seen.load(Ordering::Relaxed)
            && let Ok(_writer) = FileLock::try_acquire(&lock)
        {
            let mut current: Value = serde_json::from_slice(&fs::read(&store).unwrap()).unwrap();
            current["items"][0]["scheduler"]["owner_instruction"] = json!("AFTER_RUNNING_REPLY");
            current["items"][0]["note"] = json!("AFTER_RUNNING_REPLY");
            fs::write(&store, current.to_string()).unwrap();
            seen.store(true, Ordering::Relaxed);
        }
    }));
    let execution = t.ctx.job_dir(&id).join("execution-prompt.md");
    let seen = edited.clone();
    *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || {
        assert!(
            seen.load(Ordering::Relaxed),
            "edit must precede physical spawn"
        );
        let input = fs::read_to_string(&execution).unwrap();
        assert!(input.contains("ORIGINAL_REPLY"));
        assert!(
            !input.contains("AFTER_RUNNING_REPLY"),
            "no live input injection"
        );
        Box::new(FakeChild {
            alive: Arc::new(AtomicBool::new(false)),
            out: Some(Box::new(Cursor::new(format!("{OK}\n").into_bytes()))),
        })
    }));
    t.ctx.supervise(&id).unwrap();
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    let current: Value =
        serde_json::from_slice(&fs::read(t.ctx.root.join("todo.json")).unwrap()).unwrap();
    assert_eq!(
        current["items"][0]["scheduler"]["owner_instruction"],
        "AFTER_RUNNING_REPLY"
    );
    assert_eq!(current["items"][0]["note"], "AFTER_RUNNING_REPLY");
    assert_eq!(
        t.ctx.load(&id).unwrap()["prepared_input_admitted"],
        db["items"][0]["scheduler"]["prepared_input"]
    );
}
