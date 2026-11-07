#[test]
fn console_capability_lifetime_cannot_merge_away_a_new_durable_constraint() {
    let f = Fixture::new();
    capability_source(
        &f,
        json!({"sandbox":"read-only","mcp_profile":"minimal",
        "scope_source":"launcher","fixed_read_only_review":{"binding":"fixture"}}),
    );
    let jobs = f.console.jobs().unwrap();
    let db = f.console.todos().unwrap();
    let automatic = Trigger {
        key: "decision:source".into(),
        name: "finite-automatic".into(),
        cwd: f.root.to_string_lossy().into_owned(),
        prompt: "同じ作業を続ける".into(),
        todo_id: Some(1),
        options: json!({}),
    };
    let requested = Trigger {
        key: "manual:explicit-supplement".into(),
        name: "finite-explicit".into(),
        options: json!({"source_job_id":"source","sandbox":"read-only"}),
        ..automatic.clone()
    };
    assert!(!compatible_recovery_scope(
        &automatic, &requested, &jobs, &db
    ));
    assert!(compatible_recovery_scope(
        &automatic, &automatic, &jobs, &db
    ));
}

#[test]
fn console_capability_lifetime_marker_keeps_the_other_fixed_field_temporary_after_recheck() {
    let source = json!({"id":"finite-source","todo_id":1,"launch_key":"finite-key",
        "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher",
        "fixed_read_only_review":{"binding":"fixture"}});
    let trigger = Trigger {
        key: "decision:finite-source".into(),
        name: "finite-marker".into(),
        cwd: "D:/fixture".into(),
        prompt: "読むだけ: 保存した作業を確認する".into(),
        todo_id: Some(1),
        options: json!({}),
    };
    let initial = trigger_launch_options(&trigger, None, &trigger.options, Some(&source)).unwrap();
    let rechecked = trigger_launch_options(&trigger, None, &initial, Some(&source)).unwrap();
    assert_eq!(rechecked["sandbox"], "read-only");
    assert_eq!(rechecked["mcp_profile"], "minimal");
    assert_eq!(rechecked["scope_source"], "owner_marker");
    assert_eq!(
        rechecked["explicit_capability_constraints"],
        json!({"sandbox":true,"mcp_profile":false})
    );
    let ordinary = launch_options(None, &json!({}), Some(&rechecked), true).unwrap();
    assert_eq!(ordinary["sandbox"], "read-only");
    assert_eq!(ordinary["mcp_profile"], "inherit");
}

#[test]
fn console_capability_lifetime_subwork_dispatch_sends_fixed_source_with_field_mask() {
    let f = Fixture::new();
    f.on();
    capability_source(
        &f,
        json!({"sandbox":"read-only","mcp_profile":"minimal",
        "scope_source":"launcher","fixed_read_only_review":{"binding":"fixture"}}),
    );
    let mut db = f.console.todos().unwrap();
    db["items"][0]["subwork"] = json!({"kind":"check"});
    db["items"][0]["jobs"] = json!(["source"]);
    fs::write(f.console.paths.todo.join("todo.json"), db.to_string()).unwrap();
    let mut state = QueueState {
        initialized: true,
        ..Default::default()
    };
    f.console
        .enqueue(
            &mut state,
            Trigger {
                key: "decision:source".into(),
                name: "finite-adapter".into(),
                cwd: f.root.to_string_lossy().into_owned(),
                prompt: "同じ有限作業を続ける".into(),
                todo_id: Some(1),
                options: json!({"sandbox":"read-only"}),
            },
        )
        .unwrap();
    f.console
        .dispatch(&mut state, &f.console.jobs().unwrap())
        .unwrap();
    let calls = f.fake.calls.lock().unwrap();
    let adapter = calls
        .iter()
        .find(|call| call[0] == "dispatch_subwork")
        .unwrap();
    let request: Value = serde_json::from_str(&adapter[1]).unwrap();
    assert_eq!(request["options"]["scope_contract_source_job"], "source");
    assert_eq!(request["options"]["sandbox"], "read-only");
    assert_eq!(request["options"]["mcp_profile"], "minimal");
    assert_eq!(request["options"]["scope_source"], "caller_constraint");
    assert_eq!(request["options"]["capability_provenance_version"], 1);
    assert_eq!(
        request["options"]["explicit_capability_constraints"],
        json!({"sandbox":true,"mcp_profile":false})
    );
    assert_eq!(request["read_only"], false);
}

#[test]
fn console_capability_lifetime_keeps_only_the_current_explicit_field() {
    for (input, expected_origin, sandbox_explicit, mcp_explicit) in [
        (
            json!({"sandbox":"read-only"}),
            "caller_constraint",
            true,
            false,
        ),
        (
            json!({"mcp_profile":"minimal"}),
            "caller_constraint",
            false,
            true,
        ),
        (
            json!({"sandbox":"read-only","scope_source":"owner_request"}),
            "owner_request",
            true,
            false,
        ),
        (
            json!({"mcp_profile":"minimal","scope_source":"owner_marker"}),
            "owner_marker",
            false,
            true,
        ),
        (json!({}), "launcher", false, false),
    ] {
        let source = json!({"id":"finite-source","todo_id":1,"launch_key":"finite-key",
            "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher",
            "fixed_read_only_review":{"binding":"fixture"}});
        let selected = launch_options(None, &input, Some(&source), true).unwrap();
        assert_eq!(selected["sandbox"], "read-only");
        assert_eq!(selected["mcp_profile"], "minimal");
        assert_eq!(selected["scope_source"], expected_origin);
        assert_eq!(selected["capability_provenance_version"], 1);
        assert_eq!(
            selected["explicit_capability_constraints"],
            json!({"sandbox":sandbox_explicit,"mcp_profile":mcp_explicit})
        );
        let mut successor = selected.clone();
        successor["id"] = json!("ordinary");
        successor["todo_id"] = json!(1);
        successor["launch_key"] = json!("ordinary-key");
        let next = launch_options(None, &json!({}), Some(&successor), true).unwrap();
        assert_eq!(
            next["sandbox"],
            if sandbox_explicit {
                "read-only"
            } else {
                "inherit"
            }
        );
        assert_eq!(
            next["mcp_profile"],
            if mcp_explicit { "minimal" } else { "inherit" }
        );
        assert_eq!(
            next["explicit_capability_constraints"],
            selected["explicit_capability_constraints"]
        );
    }
}

#[test]
fn console_capability_lifetime_projection_survives_queue_reload_and_cli() {
    for (sandbox_explicit, mcp_explicit, flag) in [
        (true, false, "sandbox"),
        (false, true, "mcp_profile"),
        (true, true, "both"),
        (false, false, "none"),
    ] {
        let f = Fixture::new();
        f.on();
        capability_source(
            &f,
            json!({"sandbox":"read-only","mcp_profile":"minimal",
            "scope_source":"owner_request","capability_provenance_version":1,
            "explicit_capability_constraints":{"sandbox":sandbox_explicit,"mcp_profile":mcp_explicit}}),
        );
        let source = f
            .console
            .jobs()
            .unwrap()
            .into_iter()
            .find(|job| job["id"] == "source")
            .unwrap();
        assert_eq!(source["capability_provenance_version"], 1);
        f.console
            .api(
                &Method::POST,
                "delegate",
                json!({"id":1,
            "request_id":"lifetime-reload","source_job_id":"source","prompt":"同じ作業を続ける"}),
            )
            .unwrap();
        let restarted = Console {
            paths: f.console.paths.clone(),
            runner: f.fake.clone(),
            lock: Mutex::new(()),
        };
        restarted.tick().unwrap();
        let args = model_run(&f);
        assert_eq!(
            model_arg(&args, "--sandbox").as_deref(),
            Some(if sandbox_explicit {
                "read-only"
            } else {
                "inherit"
            })
        );
        assert_eq!(
            model_arg(&args, "--mcp-profile").as_deref(),
            Some(if mcp_explicit { "minimal" } else { "inherit" })
        );
        assert_eq!(
            model_arg(&args, "--capability-constraints").as_deref(),
            Some(flag)
        );
        let state = restarted.read_state().unwrap();
        let saved = &state.launch_options["manual:lifetime-reload"];
        assert_eq!(saved["capability_provenance_version"], 1);
        assert_eq!(
            saved["explicit_capability_constraints"],
            json!({"sandbox":sandbox_explicit,"mcp_profile":mcp_explicit})
        );
    }
}

#[test]
fn console_capability_lifetime_adapter_regenerates_internal_source_and_preserves_mask() {
    let mut source = json!({"id":"finite-source","todo_id":1,"launch_key":"finite-key",
        "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher",
        "fixed_read_only_review":{"binding":"fixture"}});
    let trigger = Trigger {
        key: "manual:finite-lifetime".into(),
        name: "finite-lifetime".into(),
        cwd: "D:/fixture".into(),
        prompt: "同じ有限作業を続ける".into(),
        todo_id: Some(1),
        options: json!({"source_job_id":"finite-source","scope_contract_source_job":"forged",
            "sandbox":"read-only"}),
    };
    let selected = trigger_launch_options(
        &trigger,
        Some(&json!({"id":1})),
        &trigger.options,
        Some(&source),
    )
    .unwrap();
    assert_eq!(selected["scope_contract_source_job"], "finite-source");
    assert_eq!(selected["capability_provenance_version"], 1);
    assert_eq!(
        selected["explicit_capability_constraints"],
        json!({"sandbox":true,"mcp_profile":false})
    );
    source
        .as_object_mut()
        .unwrap()
        .remove("fixed_read_only_review");
    let ordinary =
        trigger_launch_options(&trigger, Some(&json!({"id":1})), &selected, Some(&source)).unwrap();
    assert!(ordinary["scope_contract_source_job"].is_null());
    source["fixed_read_only_review"] = json!({"binding":"fixture"});
    source["id"] = json!("unrelated");
    let unrelated = trigger_launch_options(
        &trigger,
        Some(&json!({"id":1})),
        &trigger.options,
        Some(&source),
    )
    .unwrap();
    assert!(unrelated["scope_contract_source_job"].is_null());
}

#[test]
fn console_capability_lifetime_raw_api_cannot_forge_internal_provenance() {
    let f = Fixture::new();
    f.on();
    f.console
        .api(
            &Method::POST,
            "delegate",
            json!({"id":1,"request_id":"forged-lifetime",
        "prompt":"通常の作業を続ける","capability_provenance_version":1,
        "explicit_capability_constraints":{"sandbox":true,"mcp_profile":true},
        "scope_contract_source_job":"forged","scope_source":"owner_request"}),
        )
        .unwrap();
    let queued = f.console.read_state().unwrap();
    assert!(queued.queue[0].options["scope_contract_source_job"].is_null());
    assert!(queued.queue[0].options["capability_provenance_version"].is_null());
    assert!(queued.queue[0].options["explicit_capability_constraints"].is_null());
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("inherit"));
    assert_eq!(
        model_arg(&args, "--mcp-profile").as_deref(),
        Some("inherit")
    );
    assert_eq!(
        model_arg(&args, "--scope-source").as_deref(),
        Some("launcher")
    );
    assert_eq!(
        model_arg(&args, "--capability-constraints").as_deref(),
        Some("none")
    );
}
