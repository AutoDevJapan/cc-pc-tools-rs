//! One indexed observation supplies execution, history and privacy separately.
use super::*;
use cc_scheduler::product_identity::{self as identity, Resolution};

pub(super) fn resolve(row: &Value, policy: &Value) -> Resolution {
    identity::resolve(row, policy)
}
pub(super) fn target_for(resolution: Resolution, row: &Value, fallback: String) -> String {
    match resolution {
        Resolution::Unknown => format!(
            "製品未確認（登録 {}）",
            safe_id("record", &row["id"].to_string())
        ),
        resolution => resolution
            .product()
            .map(|p| p.name.to_owned())
            .unwrap_or(fallback),
    }
}
pub(super) fn code_product(row: &Value) -> Option<&'static str> {
    catalog::find(text(row, "project")).map(|p| p.name)
}

pub(super) use cc_scheduler::product_identity::{Associations, assignments};

pub(super) fn catalogue(product: &catalog::Product, mut value: Value) -> Value {
    if let Some(identity) = identity::by_name(product.name) {
        value["productIdentity"] = json!({"state":"catalogue","productId":identity.id,"ownerPolicy":identity.owner_policy});
        value["priority"] = json!(identity.rank);
        value["activityState"] =
            json!(if value["workers"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|w| w["primaryJob"].is_string()
                    && matches!(
                        w["jobStatus"].as_str(),
                        Some("running" | "starting" | "pending")
                    ))
            {
                "recorded_active"
            } else {
                "unobserved"
            });
        value["observationScope"] = json!("cc_jobs");
        value["progress"] = Value::Null;
    }
    value
}
