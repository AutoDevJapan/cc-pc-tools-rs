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
