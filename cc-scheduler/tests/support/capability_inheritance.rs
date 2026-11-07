use super::*;

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
            "cwd":cwd, "model":"gpt-6.1-sol", "effort":"max",
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
        assert_eq!(prepared["scheduler"]["resume_session"].is_string(), owner);
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
