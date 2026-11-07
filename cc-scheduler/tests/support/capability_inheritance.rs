use super::*;

#[test]
fn retained_constraints_survive_multiple_backend_and_cli_generations() {
    let f = Fixture::new();
    let row = json!({"id":1,"title":"調査","project":"other",
        "scheduler":{"worktree":"D:/fixture","key":"key"}});
    for provenance in [
        "explicit_capability_constraints",
        "legacy_capability_constraints",
    ] {
        let mut selection = json!({"model":"gpt-6.1-sol","effort":"max","task_type":"research",
            "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher"});
        selection[provenance] = json!({"sandbox":true,"mcp_profile":true});
        for _ in 0..3 {
            let call = RealBackend::new(f.e.root.clone()).launch_command(
                &row,
                Path::new("prompt"),
                &selection,
            );
            let value =
                |flag: &str| call.args.windows(2).find(|pair| pair[0] == flag).unwrap()[1].clone();
            assert_eq!(value("--scope-source"), "caller_constraint");
            assert_eq!(value("--sandbox"), "read-only");
            assert_eq!(value("--mcp-profile"), "minimal");
            // Registration stores these argv values on the next managed job;
            // raw standalone provenance is intentionally not fabricated.
            selection["scope_source"] = json!(value("--scope-source"));
            selection["explicit_capability_constraints"] =
                json!({"sandbox":false,"mcp_profile":false});
            selection["legacy_capability_constraints"] = serde_json::Value::Null;
        }
    }
}

fn assert_scope(f: &Fixture, prepared: &Value, sandbox: &str, mcp: &str) {
    let selection = &prepared["scheduler"]["selection"];
    assert_eq!(selection["sandbox"], sandbox);
    assert_eq!(selection["mcp_profile"], mcp);
    let command = RealBackend::new(f.e.root.clone()).launch_command(
        prepared,
        Path::new("prompt.md"),
        selection,
    );
    for (flag, value) in [("--sandbox", sandbox), ("--mcp-profile", mcp)] {
        assert!(
            command.args.windows(2).any(|args| args == [flag, value]),
            "{flag}"
        );
    }
}

#[test]
fn ordinary_roles_ignore_cached_router_capability_hints() {
    for role in ["implementation", "research", "review"] {
        for origin in [Value::Null, json!("policy"), json!("launcher")] {
            let mut f = Fixture::new();
            let mut row = item(1, "other", "todo", &[]);
            row["scheduler"] = json!({"selection":{
                "model":"gpt-6.1-sol", "effort":"max", "task_type":role,
                "sandbox":"read-only", "mcp_profile":"minimal", "scope_source":origin
            }});
            f.put(row);
            let prepared =
                f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                    .unwrap()
                    .unwrap();
            assert_scope(&f, &prepared, "inherit", "inherit");
            assert_eq!(
                prepared["scheduler"]["selection"]["scope_source"],
                "launcher"
            );
        }
    }
}

#[test]
fn direct_backend_calls_normalize_router_hints_and_keep_explicit_scope() {
    let f = Fixture::new();
    for origin in [
        Value::Null,
        json!("policy"),
        json!("launcher"),
        json!("owner_request"),
    ] {
        let row = json!({"id":1,"title":"調査","project":"other",
            "subwork":{"kind":"research","parent_todo_id":2},
            "scheduler":{"worktree":"D:/fixture","key":"key"}});
        let selection = json!({"model":"gpt-6.1-sol","effort":"max","task_type":"research",
            "sandbox":"read-only","mcp_profile":"minimal","scope_source":origin});
        let call = RealBackend::new(f.e.root.clone()).launch_command(
            &row,
            Path::new("prompt"),
            &selection,
        );
        let owner = origin == "owner_request";
        for (flag, value) in [
            ("--sandbox", if owner { "read-only" } else { "inherit" }),
            ("--mcp-profile", if owner { "minimal" } else { "inherit" }),
        ] {
            assert!(call.args.windows(2).any(|args| args == [flag, value]));
        }
    }
}

#[test]
fn research_and_review_subwork_inherit_without_a_role_restriction() {
    for role in ["research", "review"] {
        let mut sub = subwork_support::Fixture::new();
        sub.db["items"][1]["subwork"] = cc_job_policy::subwork::register(
            &sub.db,
            &sub.db["items"][1],
            1,
            "parent",
            role,
            &sub.jobs,
            false,
        )
        .unwrap();
        let mut f = Fixture::new();
        f.e.backend.records = sub.jobs.clone();
        write_json(&f.e.todo_path, &sub.db, 1).unwrap();
        let prepared =
            f.e.prepare(&sub.db["items"][1], sub.repo.to_str().unwrap())
                .unwrap()
                .unwrap();
        assert_scope(&f, &prepared, "inherit", "inherit");
        assert_eq!(prepared["subwork"], sub.db["items"][1]["subwork"]);
        assert_eq!(prepared["needs"], sub.db["items"][1]["needs"]);
        let prompt = fs::read_to_string(s(&prepared["scheduler"]["prompt"])).unwrap();
        assert!(prompt.contains("必要な調査・修正・テスト・連携は自分で判断"));
        assert!(!prompt.contains("主担当のソースを編集したり"));
    }
}

#[test]
fn explicit_current_scope_overrides_cached_router_hints() {
    for location in ["item", "options", "selection"] {
        for sandbox in ["read-only", "workspace-write", "inherit"] {
            let mut f = Fixture::new();
            let mut row = item(1, "other", "todo", &[]);
            row["scheduler"] = json!({"selection":{
                "model":"gpt-6.1-sol", "effort":"max", "task_type":"research",
                "sandbox":"read-only", "mcp_profile":"minimal"
            }});
            let scope =
                json!({"sandbox":sandbox,"mcp_profile":"browser","scope_source":"owner_request"});
            match location {
                "item" => merge(&mut row, scope),
                "options" => row["scheduler"]["console_request"]["options"] = scope,
                _ => merge(&mut row["scheduler"]["selection"], scope),
            }
            f.put(row);
            let prepared =
                f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                    .unwrap()
                    .unwrap();
            assert_scope(&f, &prepared, sandbox, "browser");
            assert_eq!(
                prepared["scheduler"]["selection"]["scope_source"],
                "owner_request"
            );
        }
    }
}

#[test]
fn explicit_read_only_request_keeps_inherited_tools() {
    for marker in ["title", "console"] {
        let mut f = Fixture::new();
        let mut row = item(1, "other", "todo", &[]);
        if marker == "title" {
            row["title"] = json!("読むだけ: 調査する");
        } else {
            row["scheduler"] = json!({"console_request":{"read_only":true}});
        }
        f.put(row);
        let prepared =
            f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                .unwrap()
                .unwrap();
        assert_scope(&f, &prepared, "read-only", "inherit");
        assert_eq!(
            prepared["scheduler"]["selection"]["scope_source"],
            if marker == "title" {
                "owner_marker"
            } else {
                "owner_request"
            }
        );
    }
}

#[test]
fn retry_retires_legacy_capability_context_and_preserves_owner_scope() {
    for origin in [
        Value::Null,
        json!("launcher"),
        json!("policy"),
        json!("owner_request"),
        json!("owner_marker"),
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        let cwd = f.get(1)["scheduler"]["worktree"].clone();
        let source = &mut f.e.backend.records[0];
        merge(
            source,
            json!({"status":"succeeded", "last_message":"状態: 継続中",
            "cwd":cwd, "model":"mock-model", "effort":"high",
            "thread_id":"12345678-1234-4321-9234-123456789abc",
                "sandbox":"read-only", "mcp_profile":"minimal", "scope_source":origin,
                "todo_id":1,"launch_key":"managed"}),
        );
        let original = source.clone();
        f.e.backend.ps.clear();
        f.reconcile();
        f.e.backend.clock += 61.;
        let prepared =
            f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                .unwrap()
                .unwrap();
        let owner = origin == "owner_request" || origin == "owner_marker";
        assert_scope(
            &f,
            &prepared,
            if owner { "read-only" } else { "inherit" },
            if owner { "minimal" } else { "inherit" },
        );
        assert_eq!(
            prepared["scheduler"]["resume_session"].is_string(),
            owner,
            "origin={origin}, prepared={}",
            prepared["scheduler"]
        );
        assert_eq!(f.e.backend.records[0], original);
    }
}

#[test]
fn actual_fixed_contract_keeps_the_source_scope_over_router_hints() {
    for binding in [
        "fixed_read_only_review",
        "fixed_review_requalification",
        "original_product_retry",
        "finite_research_approval",
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        let cwd = f.get(1)["scheduler"]["worktree"].clone();
        let source = &mut f.e.backend.records[0];
        merge(
            source,
            json!({"status":"succeeded", "last_message":"状態: 継続中",
            "cwd":cwd, "model":"gpt-6.1-sol", "effort":"max",
            "sandbox":"read-only", "mcp_profile":"minimal", "scope_source":"launcher"}),
        );
        source[binding] = json!({"binding":"fixture"});
        f.e.backend.ps.clear();
        f.reconcile();
        f.e.backend.clock += 61.;
        f.e.edit(&json!(1), |row| {
            row["scheduler"]["selection"] = json!({
                "model":"gpt-6.1-sol", "effort":"max", "task_type":"implementation",
                "sandbox":"workspace-write", "mcp_profile":"browser"
            })
        })
        .unwrap();
        let prepared =
            f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                .unwrap()
                .unwrap();
        assert_scope(&f, &prepared, "read-only", "minimal");
    }
}

#[test]
fn matching_fixed_input_retains_only_the_explicit_field_for_the_next_generation() {
    for field in ["sandbox", "mcp_profile"] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        let cwd = f.get(1)["scheduler"]["worktree"].clone();
        merge(
            &mut f.e.backend.records[0],
            json!({"status":"succeeded",
            "last_message":"状態: 継続中","cwd":cwd,"model":"gpt-6.1-sol","effort":"max",
            "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher",
            "fixed_read_only_review":{"binding":"fixture"}}),
        );
        f.e.backend.ps.clear();
        f.reconcile();
        f.e.backend.clock += 61.;
        f.e.edit(&json!(1), |row| {
            row["scheduler"]["console_request"]["options"] = json!({"scope_source":"owner_marker"});
            row["scheduler"]["console_request"]["options"][field] = json!(if field == "sandbox" {
                "read-only"
            } else {
                "minimal"
            });
        })
        .unwrap();
        let prepared =
            f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
                .unwrap()
                .unwrap();
        assert_scope(&f, &prepared, "read-only", "minimal");
        let selected = &prepared["scheduler"]["selection"];
        assert_eq!(selected["scope_source"], "owner_marker");
        assert_eq!(selected["capability_provenance_version"], 1);
        assert_eq!(selected["explicit_capability_constraints"][field], true);
        let mut next = selected.clone();
        next.as_object_mut().unwrap().remove("scope_contract");
        let ordinary = cc_job_policy::capability_scope::retained(&next);
        assert_eq!(
            ordinary["sandbox"],
            if field == "sandbox" {
                "read-only"
            } else {
                "inherit"
            }
        );
        assert_eq!(
            ordinary["mcp_profile"],
            if field == "mcp_profile" {
                "minimal"
            } else {
                "inherit"
            }
        );
    }
}

#[test]
fn fresh_console_uses_a_verified_scope_source_without_restoring_its_conversation() {
    let mut f = Fixture::new();
    f.managed(1, 1, "other", 0);
    let cwd = f.get(1)["scheduler"]["worktree"].clone();
    merge(
        &mut f.e.backend.records[0],
        json!({"status":"succeeded",
        "todo_id":1,"last_message":"OLD_SCOPE_SOURCE_BODY","cwd":cwd,"model":"gpt-6-luna","effort":"high",
        "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher",
        "thread_id":"12345678-1234-4321-9234-123456789abc",
        "fixed_read_only_review":{"binding":"fixture"}}),
    );
    f.e.backend.ps.clear();
    f.reconcile();
    f.e.backend.clock += 61.;
    let source = f.e.backend.records[0]["id"].clone();
    f.e.edit(&json!(1), |row| {
        row["scheduler"]["console_fresh_request"] = json!(true);
        row["scheduler"]["console_request"]["options"] = json!({
            "scope_contract_source_job":source,"sandbox":"read-only","mcp_profile":"minimal",
            "scope_source":"launcher","capability_provenance_version":1,
            "explicit_capability_constraints":{"sandbox":false,"mcp_profile":false}});
    })
    .unwrap();
    let prepared =
        f.e.prepare(&f.get(1), &f.repo.to_string_lossy())
            .unwrap_or_else(|error| {
                panic!(
                    "{error:?}; item={}; source={}",
                    f.get(1),
                    f.e.backend.records[0]
                )
            })
            .unwrap();
    assert_scope(&f, &prepared, "read-only", "minimal");
    assert_eq!(
        prepared["scheduler"]["selection"]["scope_contract"]["source_job"],
        source
    );
    assert!(prepared["scheduler"]["resume_session"].is_null());
    let prompt = fs::read_to_string(s(&prepared["scheduler"]["prompt"])).unwrap();
    assert!(!prompt.contains("OLD_SCOPE_SOURCE_BODY"));
}

#[test]
fn console_scope_source_rejects_unrelated_stale_live_and_unowned_records() {
    for case in [
        "missing",
        "not_fixed",
        "other_todo",
        "not_linked",
        "other_workspace",
        "live",
        "generation",
        "stopped",
        "dependency",
        "question",
        "newer_worker",
    ] {
        let mut f = Fixture::new();
        f.managed(1, 1, "other", 0);
        let cwd = f.get(1)["scheduler"]["worktree"].clone();
        merge(
            &mut f.e.backend.records[0],
            json!({"todo_id":1,"status":"succeeded",
            "last_message":"状態: 継続中","cwd":cwd,"model":"gpt-6.1-sol","effort":"max",
            "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher",
            "fixed_read_only_review":{"binding":"fixture"}}),
        );
        f.e.backend.ps.clear();
        f.reconcile();
        f.e.backend.clock += 61.;
        let source = f.e.backend.records[0]["id"].clone();
        f.e.edit(&json!(1), |row| {
            row["scheduler"]["console_fresh_request"] = json!(true);
            row["scheduler"]["console_request"]["options"]["scope_contract_source_job"] =
                source.clone();
            match case {
                "missing" => {
                    row["scheduler"]["console_request"]["options"]["scope_contract_source_job"] =
                        json!("missing")
                }
                "not_linked" => row["jobs"] = json!([]),
                "generation" => {
                    row["resume_generation"] = json!(2);
                    row["worker_receipt"] = json!({"job":source,"generation":1});
                }
                "stopped" => row["owner_stopped"] = json!(true),
                "dependency" => row["wait_on"] = json!([99]),
                "question" => row["owner_question_wait"] = json!({"request":"current"}),
                "newer_worker" => row["scheduler"]["last_job"] = json!("newer"),
                _ => {}
            }
        })
        .unwrap();
        match case {
            "not_fixed" => {
                f.e.backend.records[0]
                    .as_object_mut()
                    .unwrap()
                    .remove("fixed_read_only_review");
            }
            "other_todo" => f.e.backend.records[0]["todo_id"] = json!(99),
            "other_workspace" => f.e.backend.records[0]["cwd"] = json!("D:/other-workspace"),
            "live" => f.e.backend.records[0]["status"] = json!("running"),
            "newer_worker" => {
                f.e.backend
                    .records
                    .push(json!({"id":"newer","todo_id":1,"cwd":cwd,"status":"succeeded"}))
            }
            _ => {}
        }
        let result = f.e.prepare(&f.get(1), &f.repo.to_string_lossy());
        assert!(
            matches!(result, Ok(None) | Err(Error("InputRejected"))),
            "{case}: {result:?}"
        );
        assert!(f.e.backend.launched.is_empty(), "{case}");
    }
}

#[test]
fn finite_scope_console_subwork_dispatch_keeps_fresh_settings_and_parent_admission() {
    let mut sub = subwork_support::Fixture::new();
    sub.register();
    sub.jobs[0]["pid"] = json!(1);
    sub.jobs[0]["created_at"] = json!(T - 1200.);
    sub.jobs[0]["pid_created"] = proc(1, 0, "codex.exe")["created"].clone();
    let mut child = sub.child_job();
    merge(
        &mut child,
        json!({"status":"succeeded","todo_resume_generation":1,
        "created_at":T-1200.,"started_at":T-1200.,"finished_at":T-600.,
        "model":"gpt-6-luna","effort":"high","sandbox":"read-only","mcp_profile":"minimal",
        "scope_source":"launcher","fixed_read_only_review":{"binding":"fixture"},
        "last_message":"OLD_FIXED_SUBWORK_BODY","thread_id":"12345678-1234-4321-9234-123456789abc"}),
    );
    sub.jobs.push(child);
    let row = &mut sub.db["items"][1];
    row["jobs"] = json!(["child"]);
    row["resume_generation"] = json!(1);
    row["worker_receipt"] = json!({"job":"child","generation":1});
    row["scheduler"]["last_job"] = json!("child");
    row["scheduler"]["key"] = Value::Null;
    row["scheduler"]["console_fresh_request"] = json!(true);
    let mut f = Fixture::new();
    f.e.backend.records = sub.jobs.clone();
    write_json(&f.e.todo_path, &sub.db, 1).unwrap();
    let request = json!({"key":"manual:fresh-fixed","launch_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "prompt":"Continue the same finite task with the newly chosen model.","read_only":false,
        "options":{"model":"gpt-6.1-sol","effort":"max","model_origin":"owner_request",
            "effort_origin":"owner_request","scope_contract_source_job":"child",
            "sandbox":"read-only","mcp_profile":"minimal","scope_source":"launcher",
            "capability_provenance_version":1,"explicit_capability_constraints":{"sandbox":false,"mcp_profile":false}}});
    assert!(
        f.e.dispatch_console_subwork(&f.cfg, &f.pri, 2, &request)
            .unwrap()
            .is_some(),
        "report={}; item={}",
        f.e.report,
        f.get(2)
    );
    let launched = &f.e.backend.launched[0];
    let prepared = &launched["item"];
    assert_scope(&f, prepared, "read-only", "minimal");
    assert_eq!(prepared["scheduler"]["selection"]["model"], "gpt-6.1-sol");
    assert_eq!(
        prepared["scheduler"]["selection"]["explicit_capability_constraints"],
        json!({"sandbox":false,"mcp_profile":false})
    );
    assert!(prepared["scheduler"]["resume_session"].is_null());
    assert!(!s(&launched["text"]).contains("OLD_FIXED_SUBWORK_BODY"));
    assert_eq!(prepared["subwork"], sub.db["items"][1]["subwork"]);
    assert_eq!(prepared["scheduler"]["subwork_parent_job"], "parent");
    assert!(prepared["scheduler"]["subwork_admission"].is_object());
}
