//! Acceptance regressions for ToDo3103, through both real projection paths.
use super::*;

fn identity(alpha: bool) -> Value {
    if alpha {
        json!({"product_id":"godd-mcp-alpha","repository_id":61002u64,"owner_id":71002})
    } else {
        json!({"product_id":"godd-mcp-contract","repository_id":61001u64,"owner_id":71001})
    }
}

fn board(items: Value, jobs: Vec<Value>, masked: bool) -> Value {
    let temp = tempfile::tempdir().unwrap();
    let root = temp.path();
    fs::create_dir_all(root.join("todo")).unwrap();
    fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
    fs::create_dir_all(root.join("scheduler")).unwrap();
    fs::write(
        root.join("scheduler/priorities.json"),
        json!({"product_identities":{
            "godd-mcp-contract":{"repository_id":61001,"owner_id":71001},
            "godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}
        }})
        .to_string(),
    )
    .unwrap();
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
            json!({"repository_id":61001u64}),
            json!({"repository_id":61001u64,"owner_id":71002}),
            json!({"product_id":"godd-mcp-alpha","repository_id":61001u64,"owner_id":71001}),
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

fn row<'a>(value: &'a Value, id: &str) -> &'a Value {
    value["departments"]
        .as_array()
        .unwrap()
        .iter()
        .flat_map(|d| d["products"].as_array().unwrap())
        .chain(value["unassignedProducts"].as_array().unwrap())
        .flat_map(|p| p["workers"].as_array().unwrap())
        .find(|w| w["id"] == id)
        .unwrap()
}

#[test]
fn mcp_private_history_edges_keep_titles_and_notes_masked_after_primary_deduplication() {
    let value = board(
        json!([
            {"id":5,"project":"tools","status":"doing","title":"private-title-marker","note":"private-note-marker","jobs":["private-related"]},
            {"id":6,"project":"tools","status":"doing"}
        ]),
        vec![
            json!({"id":"private-related","todo_id":6,"status":"running","cwd":"D:/synthetic/SyntheticPrivate"}),
        ],
        false,
    );
    assert!(row(&value, "todo-5")["primaryJob"].is_null());
    assert!(!value.to_string().contains("private-title-marker"));
    assert!(!value.to_string().contains("private-note-marker"));
}

#[test]
fn mcp_shared_failure_history_is_retained_for_both_todos() {
    for masked in [true, false] {
        let value = board(
            json!([
                {"id":1,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["failed"]},
                {"id":2,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["failed"]}
            ]),
            vec![
                json!({"id":"failed","todo_id":1,"status":"failed","finished_at":"1970-01-01T00:16:40Z"}),
            ],
            masked,
        );
        assert_eq!(row(&value, "todo-1")["historyFailures"], 1);
        assert_eq!(row(&value, "todo-2")["historyFailures"], 1);
    }
}

#[test]
fn mcp_job_name_cannot_override_a_conflicting_owner_declared_primary_todo() {
    for masked in [true, false] {
        let value = board(
            json!([{"id":2641,"project":"cc-pc-tools-rs","status":"doing"}]),
            vec![
                json!({"id":"conflicting-name","todo_id":2641,"project":"GoDD-MCP","status":"running"}),
            ],
            masked,
        );
        assert!(row(&value, "todo-2641")["primaryJob"].is_null());
        assert!(product(&value, "GoDD MCP")["workers"]
            .as_array()
            .unwrap()
            .is_empty());
        assert_eq!(
            value["unassignedProducts"][0]["workers"][0]["productIdentity"]["state"],
            "unknown"
        );
    }
}

#[test]
fn mcp_detail_api_keeps_private_and_unknown_business_targets_closed() {
    let temp = tempfile::tempdir().unwrap();
    let root = temp.path();
    fs::create_dir_all(root.join("todo")).unwrap();
    fs::create_dir_all(root.join("codex-jobs/jobs/a")).unwrap();
    fs::write(root.join("client-projects.txt"), "SyntheticPrivate\n").unwrap();
    fs::write(root.join("todo/todo.json"),json!({"projects":{"GoDD MCP":"D:/synthetic/contract","tools":"D:/synthetic/tools"},"items":[
        {"id":1,"project":"GoDD MCP","note":"private-body"},
        {"id":2,"project":"tools","product_identity":{},"note":"unknown-body"}
    ]}).to_string()).unwrap();
    fs::write(root.join("codex-jobs/jobs/a/job.json"),json!({"id":"private","project":"GoDD MCP","status":"running","last_message":"private-report"}).to_string()).unwrap();
    let operations = Operations::new(root.into(), root.join("Skills"));
    assert_eq!(operations.worker_detail("todo-1", 0), Err(404));
    assert_eq!(operations.worker_detail("todo-2", 0), Err(404));
    assert_eq!(
        operations.worker_detail(&safe_id("job", "private"), 0),
        Err(404)
    );
}
