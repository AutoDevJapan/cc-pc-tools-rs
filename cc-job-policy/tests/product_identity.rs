use cc_job_policy::priority::{item_rank, project_rank};
use cc_job_policy::product_identity::{ALPHA, CONTRACT, Resolution, resolve};
use serde_json::json;

#[test]
fn mcp_department_priority_uses_verified_business_identity() {
    assert_eq!(project_rank("GoDD-MCP"), 10);
    assert_eq!(project_rank("GoDD MCP α"), 60);
    let contract = json!({"id":1,"project":"tools","product_identity":{
        "product_id":"godd-mcp-contract","repository_id":1156778979u64,"owner_id":133784601}});
    let alpha = json!({"id":2,"project":"tools","product_identity":{
        "product_id":"godd-mcp-alpha","repository_id":1154066087u64,"owner_id":246834983}});
    assert_eq!(item_rank(&contract, &json!({})), 10);
    assert_eq!(item_rank(&alpha, &json!({})), 60);
    let unverified =
        json!({"id":3,"project":"GoDD MCP α","product_identity":{"repository_id":1156778979u64}});
    assert_eq!(item_rank(&unverified, &json!({})), 100);
}

#[test]
fn explicit_business_identity_overrides_the_code_repository_and_stale_ranks() {
    let row = json!({"id":1,"project":"GoDD MCP α","repository_id":1154066087u64,"repository_owner_id":246834983,
        "product_identity":{"repository_id":1156778979u64,"owner_id":133784601}});
    assert_eq!(resolve(&row, &json!({})), Resolution::Verified(&CONTRACT));
    assert_eq!(item_rank(&row, &json!({"items":{"1":60}})), 10);
    let alpha = json!({"id":2,"project":"tools","repository_id":1154066087u64,"repository_owner_id":246834983});
    assert_eq!(resolve(&alpha, &json!({})), Resolution::Verified(&ALPHA));
}

#[test]
fn malformed_or_conflicting_explicit_identity_stays_unknown() {
    for bad in [
        json!(null),
        json!({}),
        json!({"product_id":"godd-mcp-alpha"}),
        json!({"repository_id":1154066087u64,"owner_id":"246834983"}),
        json!({"repository_id":1154066087u64,"owner_id":246834983,"product_id":null}),
        json!({"repository_id":1154066087u64,"owner_id":133784601}),
    ] {
        let row = json!({"id":1,"project":"GoDD MCP α","product_identity":bad});
        assert_eq!(resolve(&row, &json!({})), Resolution::Unknown);
        assert_eq!(item_rank(&row, &json!({"items":{"1":10}})), 100);
    }
    let row = json!({"id":1,"project":"tools","product_identity":{"repository_id":1156778979u64,"owner_id":133784601}});
    let policy = json!({"items":{"1":{"product_identity":{"repository_id":1154066087u64,"owner_id":246834983}}}});
    assert_eq!(resolve(&row, &policy), Resolution::Unknown);
    assert_eq!(
        resolve(&json!({"project":"GoDD MCP α"}), &json!({})),
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
        &json!({}),
    );
    assert_eq!(ranks["alpha"], 60);
}
