//! Acceptance regressions for ToDo3103, through both real projection paths.
use super::*;

fn identity(alpha: bool) -> Value {
    if alpha {
        json!({"product_id":"godd-mcp-alpha","repository_id":1154066087u64,"owner_id":246834983})
    } else {
        json!({"product_id":"godd-mcp-contract","repository_id":1156778979u64,"owner_id":133784601})
    }
}

fn board(items: Value, jobs: Vec<Value>, masked: bool) -> Value {
    let temp = tempfile::tempdir().unwrap();
    let root = temp.path();
    fs::create_dir_all(root.join("todo")).unwrap();
    fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
    if !masked {
        fs::write(root.join("client-projects.txt"), "SyntheticPrivate\n").unwrap();
    }
    fs::write(root.join("todo/todo.json"), json!({
        "projects":{"tools":"D:/synthetic/tools","GoDD-MCP-Alpha":"D:/synthetic/alpha","GoDD-MCP":"D:/synthetic/contract"},
        "items":items
    }).to_string()).unwrap();
    for (i, job) in jobs.into_iter().enumerate() {
        let path = root.join(format!("codex-jobs/jobs/{i}"));
        fs::create_dir_all(&path).unwrap();
        fs::write(path.join("job.json"), job.to_string()).unwrap();
    }
    collect(root, 1_000_000)
}

fn product<'a>(board: &'a Value, name: &str) -> &'a Value {
    board["departments"]
        .as_array()
        .unwrap()
        .iter()
        .flat_map(|d| d["products"].as_array().unwrap())
        .find(|p| p["name"] == name)
        .unwrap_or_else(|| panic!("missing product {name}"))
}

#[test]
fn mcp_repository_ids_separate_business_target_from_code_project() {
    for masked in [true, false] {
        let value = board(
            json!([
                {"id":1,"project":"tools","product_identity":identity(false),"status":"todo","progress":12},
                {"id":2,"project":"tools","product_identity":identity(true),"status":"todo","progress":45}
            ]),
            vec![],
            masked,
        );
        let contract = product(&value, "GoDD MCP");
        let alpha = product(&value, "GoDD MCP α");
        assert_eq!(
            contract["productIdentity"]["productId"],
            "godd-mcp-contract"
        );
        assert_eq!(alpha["productIdentity"]["productId"], "godd-mcp-alpha");
        assert_eq!(contract["workers"][0]["id"], "todo-1");
        assert_eq!(alpha["workers"][0]["id"], "todo-2");
        assert_eq!(contract["workers"][0]["priority"], 10);
        assert_eq!(alpha["workers"][0]["priority"], 60);
        assert_ne!(contract["id"], alpha["id"]);
    }
}

#[test]
fn mcp_alpha_omission_never_aliases_to_alpha() {
    assert_eq!(catalog::find("GoDD-MCP").unwrap().name, "GoDD MCP");
    assert_eq!(catalog::find("GoDD-MCP-α").unwrap().name, "GoDD MCP α");
    for ambiguous in [
        "GoDD",
        "MCP",
        "GoDD-MCP-a",
        "GoDD-MCP-Alpha-ci",
        "GoDD-MCP?",
    ] {
        assert!(catalog::find(ambiguous).is_none());
    }
}

#[test]
fn mcp_other_products_ci_never_supplies_contract_execution() {
    for masked in [true, false] {
        let value = board(
            json!([
                {"id":1,"project":"GoDD-MCP","status":"doing","jobs":["alpha-ci"]},
                {"id":2641,"project":"GoDD-MCP-Alpha","status":"doing"}
            ]),
            vec![
                json!({"id":"alpha-ci","todo_id":2641,"status":"running","product_identity":identity(true)}),
            ],
            masked,
        );
        let contract = product(&value, "GoDD MCP");
        assert!(contract["workers"][0]["primaryJob"].is_null());
        assert_ne!(contract["workers"][0]["status"], "working");
        assert_eq!(
            product(&value, "GoDD MCP α")["workers"][0]["jobStatus"],
            "running"
        );
    }
}

#[test]
fn mcp_same_job_has_one_primary_assignment_without_removing_tasks() {
    for masked in [true, false] {
        let value = board(
            json!([
                {"id":1,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["one-job"],"progress":20},
                {"id":2,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["one-job"],"progress":30}
            ]),
            vec![json!({"id":"one-job","todo_id":1,"status":"running"})],
            masked,
        );
        let workers = product(&value, "GoDD MCP α")["workers"].as_array().unwrap();
        assert_eq!(workers.len(), 2);
        assert_eq!(
            workers
                .iter()
                .filter(|w| w["primaryJob"].is_string())
                .count(),
            1
        );
        assert_eq!(
            workers.iter().find(|w| w["id"] == "todo-2").unwrap()["progress"],
            30
        );
    }
}

#[test]
fn mcp_unverified_owner_does_not_fall_back_to_a_name() {
    for masked in [true, false] {
        for bad in [
            json!({"repository_id":1156778979u64}),
            json!({"repository_id":1156778979u64,"owner_id":246834983}),
            json!({"product_id":"godd-mcp-alpha","repository_id":1156778979u64,"owner_id":133784601}),
        ] {
            let value = board(
                json!([{"id":7,"project":"GoDD-MCP-Alpha","product_identity":bad,"status":"doing"}]),
                vec![],
                masked,
            );
            assert!(product(&value, "GoDD MCP α")["workers"]
                .as_array()
                .unwrap()
                .is_empty());
            let unknown = &value["unassignedProducts"][0]["workers"][0];
            assert_eq!(unknown["productIdentity"]["state"], "unknown");
            assert_eq!(unknown["priority"], 100);
        }
    }
}

#[test]
fn mcp_no_observed_job_does_not_claim_native_sessions_stopped() {
    let value = board(json!([]), vec![], true);
    let contract = product(&value, "GoDD MCP");
    assert_eq!(contract["activityState"], "unobserved");
    assert!(contract["progress"].is_null());
    assert_eq!(contract["observationScope"], "cc_jobs");
}

#[test]
fn mcp_owner_declared_ci_todos_remain_alpha_in_the_maintenance_repository() {
    for masked in [true, false] {
        let value = board(
            json!([
                {"id":2641,"project":"cc-pc-tools-rs","status":"todo"},
                {"id":2737,"project":"cc-pc-tools-rs","status":"doing"},
                {"id":3093,"project":"cc-pc-tools-rs","status":"done","updated_at":"1970-01-01T00:16:40Z"}
            ]),
            vec![],
            masked,
        );
        assert_eq!(
            product(&value, "GoDD MCP α")["workers"]
                .as_array()
                .unwrap()
                .len(),
            3
        );
        assert!(product(&value, "GoDD MCP")["workers"]
            .as_array()
            .unwrap()
            .is_empty());
        for row in product(&value, "GoDD MCP α")["workers"].as_array().unwrap() {
            assert_eq!(row["productIdentity"]["state"], "owner_declared");
            assert_eq!(row["codeProduct"], "PCの道具");
        }
    }
}

#[test]
fn mcp_metadata_panel_uses_task_identity_and_one_job_without_borrowing_progress() {
    let value = board(
        json!([
            {"id":1,"project":"tools","product_identity":identity(false),"status":"doing","jobs":["one"],"progress":20},
            {"id":2,"project":"GoDD-MCP-Alpha","status":"todo","progress":80}
        ]),
        vec![json!({"id":"one","todo_id":1,"status":"running"})],
        true,
    );
    let panel = metadata_display_snapshot(&value, "jobs").unwrap();
    assert_eq!(panel["jobs"].as_array().unwrap().len(), 1);
    assert_eq!(
        panel["jobs"][0]["productIdentity"]["productId"],
        "godd-mcp-contract"
    );
    assert_eq!(panel["jobs"][0]["progress"], 20);
    assert_eq!(panel["jobs"][0]["codeProduct"], "PCの道具");
    assert!(product(&value, "GoDD MCP")["progress"].is_null());
}

#[test]
fn mcp_conflicting_legacy_links_without_a_primary_todo_remain_unattributed() {
    for masked in [true, false] {
        let value = board(
            json!([
                {"id":1,"project":"GoDD-MCP","status":"doing","jobs":["uncertain"]},
                {"id":2,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["uncertain"]}
            ]),
            vec![json!({"id":"uncertain","project":"tools","status":"running"})],
            masked,
        );
        for name in ["GoDD MCP", "GoDD MCP α"] {
            assert!(product(&value, name)["workers"][0]["primaryJob"].is_null());
            assert_eq!(product(&value, name)["activityState"], "unobserved");
        }
        let unknown = &value["unassignedProducts"][0]["workers"][0];
        assert_eq!(unknown["productIdentity"]["state"], "unknown");
        assert!(unknown["primaryJob"].is_string());
    }
}
