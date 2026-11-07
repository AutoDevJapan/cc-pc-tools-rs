#[test]
fn ordinary_delegation_roles_keep_parent_sandbox_and_tools() {
    for role in ["implementation", "research", "review"] {
        let f = Fixture::new();
        f.on();
        f.console
            .api(
                &Method::POST,
                "delegate",
                json!({"id":1,"request_id":"ordinary-role",
            "prompt":"Chromeで資料を確認して作業を進める","task_type":role}),
            )
            .unwrap();
        f.console.tick().unwrap();
        let args = model_run(&f);
        for (flag, expected) in [
            ("--sandbox", "inherit"),
            ("--mcp-profile", "inherit"),
            ("--scope-source", "launcher"),
            ("--task-type", role),
        ] {
            assert_eq!(
                model_arg(&args, flag).as_deref(),
                Some(expected),
                "{role}/{flag}"
            );
        }
    }
}

#[test]
fn explicit_tools_and_scope_survive_reload_and_reach_cli_with_provenance() {
    for (sandbox, mcp) in [
        ("workspace-write", "minimal"),
        ("read-only", "browser"),
        ("inherit", "full"),
        ("inherit", "inherit"),
    ] {
        let f = Fixture::new();
        f.on();
        let request = json!({"id":1,"request_id":"explicit-tools","prompt":"Chromeで確認する",
            "task_type":"review","sandbox":sandbox,"mcp_profile":mcp});
        f.console
            .api(&Method::POST, "delegate", request.clone())
            .unwrap();
        let before = f.console.read_state().unwrap();
        assert_eq!(
            before.request_options["manual:explicit-tools"]["mcp_profile"],
            mcp
        );
        let restarted = Console {
            paths: f.console.paths.clone(),
            runner: f.fake.clone(),
            lock: Mutex::new(()),
        };
        restarted.tick().unwrap();
        let args = model_run(&f);
        assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some(sandbox));
        assert_eq!(model_arg(&args, "--mcp-profile").as_deref(), Some(mcp));
        assert_eq!(
            model_arg(&args, "--scope-source").as_deref(),
            Some("caller_constraint")
        );
        restarted.api(&Method::POST, "delegate", request).unwrap();
        assert_eq!(
            restarted.read_state().unwrap().request_options,
            before.request_options
        );
        assert_eq!(f.launches(), 1);
    }
}

#[test]
fn delegation_invalid_capabilities_are_rejected_before_runner_or_queue() {
    for (field, value) in [
        ("sandbox", json!("unknown")),
        ("sandbox", json!(true)),
        ("mcp_profile", json!("unknown")),
        ("mcp_profile", json!(3)),
    ] {
        let f = Fixture::new();
        f.on();
        let mut request = json!({"id":1,"request_id":"invalid-capability","prompt":"確認を進める"});
        request[field] = value;
        assert_eq!(
            f.console
                .api(&Method::POST, "delegate", request)
                .unwrap_err()
                .code,
            "invalid_request"
        );
        assert!(f.console.read_state().unwrap().queue.is_empty());
        assert!(f.fake.calls.lock().unwrap().is_empty());
    }
}

fn capability_source(f: &Fixture, patch: Value) {
    let mut source = model_source(f);
    for (field, value) in patch.as_object().unwrap() {
        source[field] = value.clone();
    }
    fs::write(
        f.console.paths.jobs.join("source/job.json"),
        source.to_string(),
    )
    .unwrap();
}

#[test]
fn delegated_source_hints_do_not_become_constraints_but_explicit_inputs_do() {
    for (origin, flags, expected_sandbox, expected_mcp) in [
        (json!("launcher"), Value::Null, "inherit", "inherit"),
        (json!("policy"), Value::Null, "inherit", "inherit"),
        (Value::Null, Value::Null, "inherit", "inherit"),
        (
            json!("caller_constraint"),
            Value::Null,
            "read-only",
            "minimal",
        ),
        (json!("owner_request"), Value::Null, "read-only", "minimal"),
        (json!("owner_marker"), Value::Null, "read-only", "minimal"),
        (
            json!("launcher"),
            json!({"sandbox":true,"mcp_profile":false}),
            "read-only",
            "inherit",
        ),
        (
            json!("launcher"),
            json!({"sandbox":false,"mcp_profile":true}),
            "inherit",
            "minimal",
        ),
    ] {
        let f = Fixture::new();
        f.on();
        capability_source(
            &f,
            json!({"task_type":"review","sandbox":"read-only","mcp_profile":"minimal",
            "scope_source":origin,"explicit_capability_constraints":flags}),
        );
        f.console
            .api(
                &Method::POST,
                "delegate",
                json!({"id":1,"request_id":"continue-capabilities",
            "source_job_id":"source","prompt":"同じ作業を続ける"}),
            )
            .unwrap();
        f.console.tick().unwrap();
        let args = model_run(&f);
        assert_eq!(
            model_arg(&args, "--sandbox").as_deref(),
            Some(expected_sandbox)
        );
        assert_eq!(
            model_arg(&args, "--mcp-profile").as_deref(),
            Some(expected_mcp)
        );
        let constrained = expected_sandbox != "inherit" || expected_mcp != "inherit";
        assert_eq!(
            model_arg(&args, "--scope-source").as_deref(),
            Some(match origin.as_str() {
                Some("owner_request") => "owner_request",
                Some("owner_marker") => "owner_marker",
                _ if constrained => "caller_constraint",
                _ => "launcher",
            })
        );
    }
}

#[test]
fn ambiguous_legacy_standalone_scope_is_retained_without_owner_attribution() {
    let f = Fixture::new();
    f.on();
    capability_source(
        &f,
        json!({"todo_id":null,"launch_key":null,"scope_source":"launcher",
        "sandbox":"read-only","mcp_profile":"minimal"}),
    );
    f.console
        .api(
            &Method::POST,
            "delegate",
            json!({"id":1,"request_id":"legacy-capabilities",
        "source_job_id":"source","prompt":"同じ作業を続ける"}),
        )
        .unwrap();
    f.console.tick().unwrap();
    let args = model_run(&f);
    assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("read-only"));
    assert_eq!(
        model_arg(&args, "--mcp-profile").as_deref(),
        Some("minimal")
    );
    assert_eq!(
        model_arg(&args, "--scope-source").as_deref(),
        Some("caller_constraint")
    );
    let state = f.console.read_state().unwrap();
    assert_eq!(
        state.launch_options["manual:legacy-capabilities"]["legacy_capability_constraints"],
        json!({"sandbox":true,"mcp_profile":true})
    );
}

#[test]
fn fixed_source_scope_is_retained_without_copying_private_receipts() {
    for marker in [
        "fixed_read_only_review",
        "original_product_retry",
        "initial_review",
        "finite_research_approval",
    ] {
        let f = Fixture::new();
        f.on();
        let mut patch =
            json!({"sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher"});
        patch[marker] = json!({"private_receipt":"DO_NOT_DISCLOSE_CONTRACT_CONTENT"});
        capability_source(&f, patch);
        let source = f
            .console
            .jobs()
            .unwrap()
            .into_iter()
            .find(|job| job["id"] == "source")
            .unwrap();
        assert_eq!(source["capability_contract"], true);
        assert!(!source
            .to_string()
            .contains("DO_NOT_DISCLOSE_CONTRACT_CONTENT"));
        f.console
            .api(
                &Method::POST,
                "delegate",
                json!({"id":1,"request_id":"finite-capabilities",
            "source_job_id":"source","prompt":"同じ有限作業を続ける"}),
            )
            .unwrap();
        f.console.tick().unwrap();
        let args = model_run(&f);
        assert_eq!(model_arg(&args, "--sandbox").as_deref(), Some("read-only"));
        assert_eq!(
            model_arg(&args, "--mcp-profile").as_deref(),
            Some("minimal")
        );
    }
}

#[test]
fn inherited_fixed_source_cannot_be_widened_by_delegate_options() {
    let source = json!({"sandbox":"read-only","mcp_profile":"minimal","fixed_read_only_review":{}});
    for requested in [json!({"sandbox":"inherit"}), json!({"mcp_profile":"full"})] {
        assert_eq!(
            launch_options(None, &requested, Some(&source), true)
                .unwrap_err()
                .code,
            "invalid_request"
        );
    }
}

#[test]
fn tool_restricted_supplement_cannot_consume_the_ordinary_recovery() {
    let f = todo1597_fixture("todo");
    f.console.api(&Method::POST, "delegate", json!({"id":1,"request_id":"tool-supplement",
        "cwd":f.root,"isolate_worktree":false,"prompt":"追加の確認を進める","mcp_profile":"minimal"})).unwrap();
    let mut state = f.console.read_state().unwrap();
    f.console
        .dispatch(&mut state, &f.console.jobs().unwrap())
        .unwrap();
    assert!(!state.merged_requests.contains_key("decision:source"));
    assert!(state
        .queue
        .iter()
        .any(|trigger| trigger.key == "manual:tool-supplement"));
    assert_eq!(f.launches(), 1);
    assert_eq!(
        model_arg(&model_run(&f), "--mcp-profile").as_deref(),
        Some("inherit")
    );
}

#[test]
fn changed_tool_profile_starts_a_fresh_conversation() {
    let session = "12345678-1234-4321-9234-123456789abc";
    let source = json!({"model":"gpt-6.1-sol","effort":"high","sandbox":"inherit",
        "mcp_profile":"minimal","scope_source":"caller_constraint","cwd":"D:/fixture"});
    let options = launch_options(
        None,
        &json!({"model":"gpt-6.1-sol","effort":"high",
        "mcp_profile":"inherit","resume_session":session}),
        Some(&source),
        true,
    )
    .unwrap();
    assert_eq!(options["resume_session"], Value::Null);
    assert_eq!(options["fresh_conversation"], true);
    assert_eq!(options["source_workspace"], source["cwd"]);
}
