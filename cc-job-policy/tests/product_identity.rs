use cc_job_policy::priority::{item_rank, project_rank};
use cc_job_policy::product_identity::{ALPHA, CONTRACT, Resolution, resolve};
use serde_json::json;
fn bindings() -> serde_json::Value {
    json!({"product_identities":{
        "godd-mcp-contract":{"repository_id":61001,"owner_id":71001},
        "godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}
    }})
}
fn with_bindings(mut policy: serde_json::Value) -> serde_json::Value {
    policy["product_identities"] = bindings()["product_identities"].clone();
    policy
}

#[test]
fn mcp_department_priority_uses_verified_business_identity() {
    assert_eq!(project_rank("GoDD-MCP"), 10);
    assert_eq!(project_rank("GoDD　MCP"), 10);
    assert_eq!(project_rank("GoDD MCP α"), 60);
    let contract = json!({"id":1,"project":"tools","product_identity":{
        "product_id":"godd-mcp-contract","repository_id":61001u64,"owner_id":71001}});
    let alpha = json!({"id":2,"project":"tools","product_identity":{
        "product_id":"godd-mcp-alpha","repository_id":61002u64,"owner_id":71002}});
    assert_eq!(item_rank(&contract, &bindings()), 10);
    assert_eq!(item_rank(&alpha, &bindings()), 60);
    let unverified =
        json!({"id":3,"project":"GoDD MCP α","product_identity":{"repository_id":61001u64}});
    assert_eq!(item_rank(&unverified, &bindings()), 100);
}

#[test]
fn explicit_business_identity_overrides_the_code_repository_and_stale_ranks() {
    let row = json!({"id":1,"project":"GoDD MCP α","repository_id":61002u64,"repository_owner_id":71002,
        "product_identity":{"repository_id":61001u64,"owner_id":71001}});
    assert_eq!(resolve(&row, &bindings()), Resolution::Verified(&CONTRACT));
    assert_eq!(
        item_rank(&row, &with_bindings(json!({"items":{"1":60}}))),
        10
    );
    let alpha =
        json!({"id":2,"project":"tools","repository_id":61002u64,"repository_owner_id":71002});
    assert_eq!(resolve(&alpha, &bindings()), Resolution::Verified(&ALPHA));
}

#[test]
fn malformed_or_conflicting_explicit_identity_stays_unknown() {
    for bad in [
        json!(null),
        json!({}),
        json!({"product_id":"godd-mcp-alpha"}),
        json!({"repository_id":61002u64,"owner_id":"71002"}),
        json!({"repository_id":61002u64,"owner_id":71002,"product_id":null}),
        json!({"repository_id":61002u64,"owner_id":71001}),
    ] {
        let row = json!({"id":1,"project":"GoDD MCP α","product_identity":bad});
        assert_eq!(resolve(&row, &bindings()), Resolution::Unknown);
        assert_eq!(
            item_rank(&row, &with_bindings(json!({"items":{"1":10}}))),
            100
        );
    }
    let row = json!({"id":1,"project":"tools","product_identity":{"repository_id":61001u64,"owner_id":71001}});
    let policy =
        json!({"items":{"1":{"product_identity":{"repository_id":61002u64,"owner_id":71002}}}});
    let policy = with_bindings(policy);
    assert_eq!(resolve(&row, &policy), Resolution::Unknown);
    assert_eq!(
        resolve(&json!({"project":"GoDD MCP α"}), &bindings()),
        Resolution::NameOnly(&ALPHA)
    );
}

#[test]
fn alpha_job_rank_does_not_inherit_a_conflicting_contract_history_link() {
    let db = json!({"items":[
        {"id":1,"project":"GoDD MCP","jobs":["alpha"]},
        {"id":2641,"project":"cc-pc-tools-rs"}
    ]});
    let ranks = cc_job_policy::priority::job_ranks(
        &[json!({"id":"alpha","todo_id":2641,"project":"tools"})],
        &db,
        &bindings(),
    );
    assert_eq!(ranks["alpha"], 60);
}

#[test]
fn ambiguous_legacy_edges_do_not_choose_a_business_target_by_rank() {
    let db = json!({"items":[
        {"id":1,"project":"GoDD MCP","jobs":["ambiguous"]},
        {"id":2,"project":"GoDD MCP α","jobs":["ambiguous"]}
    ]});
    let ranks = cc_job_policy::priority::job_ranks(
        &[json!({"id":"ambiguous","project":"tools"})],
        &db,
        &bindings(),
    );
    assert_eq!(ranks["ambiguous"], 100);
}

#[test]
fn a_normal_primary_review_job_keeps_its_existing_priority() {
    let db = json!({"items":[{"id":3200,"project":"cc-pc-tools-rs"},{"id":3093,"project":"cc-pc-tools-rs"}]});
    let ranks = cc_job_policy::priority::job_ranks(
        &[json!({"id":"r","todo_id":3200,"project":"cc-pc-tools-rs","related_todo_ids":[3093]})],
        &db,
        &bindings(),
    );
    assert_eq!(ranks["r"], 30);
}

#[test]
fn unknown_product_jobs_inherit_a_recorded_dependency_priority_without_claiming_identity() {
    let bad = json!({"repository_id":61001,"owner_id":0});
    let db = json!({"items":[{"id":1,"project":"GoDD MCP","status":"wait","wait_on":[7]},
        {"id":7,"project":"tools","status":"doing","product_identity":bad}]});
    let job = json!({"id":"prerequisite","todo_id":7,"product_identity":bad});
    assert_eq!(resolve(&job, &bindings()), Resolution::Unknown);
    let ranks = cc_job_policy::priority::job_ranks(&[job], &db, &bindings());
    assert_eq!(ranks["prerequisite"], 10);
}

#[test]
fn mcp_business_jobs_preserve_dependency_urgency_without_borrowing_history_priority() {
    for declared in [false, true] {
        for dependency in ["wait_on", "work_wait_on", "none"] {
            for parent_rank in [10u32, 20, 30, 40, 50, 61, 100] {
                let mut parent =
                    json!({"id":1,"project":"tools","status":"wait","jobs":["prerequisite"]});
                if dependency != "none" {
                    parent[dependency] = json!([7]);
                }
                let db = json!({"items":[parent,{"id":7,"project":"tools","status":"doing"}]});
                let mut job = json!({"id":"prerequisite","todo_id":7,"project":"GoDD MCP α"});
                if declared {
                    job["product_identity"] = json!({"repository_id":61002,"owner_id":71002});
                }
                let policy = with_bindings(json!({"items":{"1":parent_rank}}));
                let ranks = cc_job_policy::priority::job_ranks(&[job], &db, &policy);
                let expected = if dependency == "none" {
                    60
                } else {
                    parent_rank.min(60)
                };
                assert_eq!(
                    ranks["prerequisite"], expected,
                    "{declared}/{dependency}/{parent_rank}"
                );
            }
        }
    }
}

#[test]
fn bindings_are_required_and_never_returned_by_metadata() {
    let row = json!({"product_identity":{"repository_id":61001,"owner_id":71001}});
    assert_eq!(
        resolve(&row, &json!({"product_identities":{}})),
        Resolution::Unknown
    );
    let metadata = resolve(&row, &bindings()).metadata().to_string();
    assert!(!metadata.contains("61001"));
    assert!(!metadata.contains("71001"));
}

#[test]
fn explicit_identity_does_not_override_an_opposing_owner_declaration() {
    let row = json!({"id":2641,"project":"cc-pc-tools-rs","product_identity":{"repository_id":61001,"owner_id":71001}});
    assert_eq!(resolve(&row, &bindings()), Resolution::Unknown);
}

#[test]
fn mcp_live_requester_identity_drives_formal_dependency_priority() {
    for alpha in [false, true] {
        for edge in ["wait_on", "work_wait_on"] {
            for chain in [false, true] {
                let mut parent = json!({"id":1,"project":"tools","status":"doing"});
                parent[edge] = json!([if chain { 5 } else { 7 }]);
                let db = json!({"items":[parent,
                    {"id":5,"project":"tools","status":"doing","work_wait_on":[7]},
                    {"id":7,"project":"tools","status":"doing"}]});
                let jobs = vec![
                    json!({"id":"requester","todo_id":1,"status":"queued","product_identity":{"repository_id":if alpha {61002} else {61001},"owner_id":if alpha {71002} else {71001}}}),
                    json!({"id":"middle","todo_id":5,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}),
                    json!({"id":"prerequisite","todo_id":7,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}),
                ];
                let ranks = cc_job_policy::priority::job_ranks(&jobs, &db, &bindings());
                assert_eq!(ranks["requester"], if alpha { 60 } else { 10 });
                assert_eq!(
                    ranks["prerequisite"],
                    if alpha { 60 } else { 10 },
                    "alpha={alpha}/edge={edge}/chain={chain}"
                );
            }
        }
    }
}

#[test]
fn mcp_live_requester_dependency_exclusion_controls() {
    for control in [
        "parent_done",
        "parent_block",
        "wait_ready",
        "child_done",
        "no_product_job",
        "terminal_requester",
    ] {
        let mut parent = json!({"id":1,"project":"tools","status":"doing","wait_on":[7]});
        let mut child = json!({"id":7,"project":"tools","status":"doing"});
        match control {
            "parent_done" => parent["status"] = json!("done"),
            "parent_block" => parent["status"] = json!("block"),
            "wait_ready" => parent["wait_ready"] = json!({"condition":"synthetic"}),
            "child_done" => child["status"] = json!("done"),
            _ => {}
        }
        let mut requester = json!({"id":"requester","todo_id":1,"status":"queued","product_identity":{"repository_id":61001,"owner_id":71001}});
        if control == "no_product_job" {
            requester
                .as_object_mut()
                .unwrap()
                .remove("product_identity");
        }
        if control == "terminal_requester" {
            requester["status"] = json!("failed");
        }
        let jobs = [
            requester,
            json!({"id":"prerequisite","todo_id":7,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}),
        ];
        let ranks = cc_job_policy::priority::job_ranks(
            &jobs,
            &json!({"items":[parent,child]}),
            &bindings(),
        );
        assert_eq!(
            ranks["prerequisite"],
            if matches!(control, "no_product_job" | "terminal_requester") {
                30
            } else {
                60
            },
            "{control}"
        );
    }
}

#[test]
fn mcp_related_review_preserves_current_primary_and_dependency_priority() {
    let policy = bindings();
    let items = vec![
        json!({"id":1,"project":"tools","status":"doing","wait_on":[7],"jobs":["review"]}),
        json!({"id":7,"project":"tools","status":"doing"}),
        json!({"id":9,"project":"tools","status":"doing"}),
    ];
    let jobs = vec![
        json!({"id":"requester","todo_id":1,"status":"running","created_at":"2026-10-20T00:00:00Z","product_identity":{"repository_id":61001,"owner_id":71001}}),
        json!({"id":"review","todo_id":9,"related_todo_ids":[1],"status":"queued","created_at":"2026-10-20T01:00:00Z"}),
        json!({"id":"prerequisite","todo_id":7,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}),
    ];
    let associations = cc_job_policy::product_identity::assignments(&items, &jobs, &policy);
    assert_eq!(associations.primary(&jobs[1]), Some(9));
    assert_eq!(
        associations.record(&items[0], &policy),
        Resolution::Verified(&CONTRACT)
    );
    assert_eq!(
        cc_job_policy::priority::job_ranks(&jobs, &json!({"items":items}), &policy)["prerequisite"],
        10
    );
}

#[test]
fn mcp_effective_private_task_masks_every_raw_job_edge() {
    let policy = bindings();
    let items = vec![
        json!({"id":1,"project":"tools","status":"doing","jobs":["review"]}),
        json!({"id":9,"project":"tools","status":"doing"}),
    ];
    let jobs = vec![
        json!({"id":"requester","todo_id":1,"status":"running","product_identity":{"repository_id":61001,"owner_id":71001}}),
        json!({"id":"review","todo_id":9,"related_todo_ids":[1],"status":"queued"}),
    ];
    let associations = cc_job_policy::product_identity::assignments(&items, &jobs, &policy);
    assert_eq!(
        associations.record(&items[0], &policy),
        Resolution::Verified(&CONTRACT)
    );
    assert!(
        associations.sensitive(&jobs[1]),
        "a business-private task's related report must remain private"
    );
}

#[test]
fn mcp_history_only_product_job_does_not_borrow_code_priority() {
    let db = json!({"items":[{"id":7,"project":"tools","status":"wait","jobs":["alpha"]}]});
    let job = json!({"id":"alpha","related_todo_ids":[7],"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}});
    assert_eq!(
        cc_job_policy::priority::job_ranks(&[job], &db, &bindings())["alpha"],
        60
    );
}

#[test]
fn mcp_private_live_sibling_keeps_all_raw_edges_private_without_changing_current() {
    let items = vec![
        json!({"id":1,"project":"tools","status":"doing"}),
        json!({"id":9,"project":"tools","status":"doing"}),
    ];
    let mut jobs = vec![
        json!({"id":"requester","todo_id":1,"status":"running","created_at":"2026-10-20T00:00:00Z","product_identity":{"repository_id":61001,"owner_id":71001}}),
        json!({"id":"sibling","todo_id":1,"status":"running","created_at":"2026-10-20T02:00:00Z"}),
        json!({"id":"review","todo_id":9,"related_todo_ids":[1],"status":"queued"}),
    ];
    let policy = bindings();
    let active = cc_job_policy::product_identity::assignments(&items, &jobs, &policy);
    assert_eq!(active.current(1).unwrap()["id"], "sibling");
    assert!(
        active.sensitive(&jobs[2]),
        "current choice must not discard a live private sibling's privacy"
    );
    jobs[0]["status"] = json!("succeeded");
    let ended = cc_job_policy::product_identity::assignments(&items, &jobs, &policy);
    assert!(
        !ended.sensitive(&jobs[2]),
        "completed job-only ownership must not become a permanent private binding"
    );
    jobs[0]["status"] = json!("running");
    jobs[0]["product_identity"] = json!({"repository_id":61002,"owner_id":71002});
    let alpha = cc_job_policy::product_identity::assignments(&items, &jobs, &policy);
    assert!(
        !alpha.sensitive(&jobs[2]),
        "the public Alpha sibling must not be treated as the distinct contract product"
    );
}
