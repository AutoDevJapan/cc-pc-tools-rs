use localai_web_rs::workforce::{generation, project};
use serde_json::json;

#[test]
fn failed_acquisition_keeps_previous_generation_as_explicitly_old() {
    let good = json!({"fetchedAt":1000,"metadataOk":true,"unassignedProducts":[],"departments":[{"products":[{"workers":[{"id":"todo-12","recordedStatus":"doing","progress":0}]}]}]});
    let previous = generation(&good, None, None, 900, 1000, None);
    let failed = generation(
        &json!({"fetchedAt":2000,"metadataOk":false}),
        None,
        Some(&previous),
        1900,
        2000,
        Some("collector_timeout"),
    );
    assert_eq!(failed["state"], "unavailable");
    assert!(failed["inventory"]["taskRecords"].is_null());
    assert_eq!(failed["previous"]["observedAt"], 1000);
    assert_eq!(failed["previous"]["tasks"][0]["todoId"], 12);
    assert_eq!(failed["previous"]["tasks"][0]["progress"], 0);
    assert_eq!(failed["acquisition"]["startedAt"], 1900);
    assert_eq!(failed["acquisition"]["completedAt"], 2000);
    assert_eq!(failed["acquisition"]["error"], "collector_timeout");
    let again = generation(
        &json!({}),
        None,
        Some(&failed),
        2900,
        3000,
        Some("collector_failed"),
    );
    assert_eq!(again["previous"]["observedAt"], 1000);
    assert!(again["previous"].get("previous").is_none());
}

#[test]
fn responsibility_role_is_registered_for_each_existing_product_without_processes() {
    let board = json!({"fetchedAt":1000,"metadataOk":true,"unassignedProducts":[],"departments":[{"id":"department-5","products":[{"id":"product-0123456789abcdef","workers":[]}]}]});
    let data = project(&board, None, 1000);
    assert_eq!(data["responsibility"]["model"], "per_product");
    assert_eq!(data["responsibility"]["productRoles"], 1);
    assert_eq!(
        data["responsibility"]["roles"][0]["productId"],
        "product-0123456789abcdef"
    );
    assert_eq!(data["responsibility"]["roles"][0]["state"], "registered");
    assert!(data["responsibility"]["roles"][0]["currentSession"].is_null());
    assert_eq!(data["responsibility"]["residentProcesses"], 0);
}
