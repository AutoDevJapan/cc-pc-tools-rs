use cc_job_policy::priority::{item_rank, project_rank};
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
