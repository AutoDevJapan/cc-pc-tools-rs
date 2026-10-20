//! Business attribution shared by the normal and privacy-preserving projections.
use super::*;
use cc_scheduler::product_identity::{self as identity, Resolution};

pub(super) fn resolve(row: &Value, policy: &Value) -> Resolution {
    identity::resolve(row, policy)
}

pub(super) fn target(row: &Value, policy: &Value, fallback: String) -> String {
    match resolve(row, policy) {
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

/// A direct numeric ToDo link wins over legacy session/history links. A conflicting
/// direct owner is never replaced by a secondary link to a different product.
pub(super) fn assignments(
    items: &[Value],
    jobs: &[Value],
    policy: &Value,
) -> BTreeMap<String, u64> {
    jobs.iter()
        .filter_map(|job| {
            let id = job["id"].as_str().filter(|id| !id.is_empty())?;
            let direct = job["todo_id"].as_u64();
            let target = resolve(job, policy);
            let item = items
                .iter()
                .filter(|item| linked(item, job))
                .filter(|item| direct.is_none_or(|id| item["id"].as_u64() == Some(id)))
                .filter(|item| identity::compatible(resolve(item, policy), target))
                .min_by_key(|item| {
                    (
                        item["scheduler"]["job"] != job["id"],
                        item["id"].as_u64().unwrap_or(u64::MAX),
                    )
                })?;
            Some((id.to_owned(), item["id"].as_u64()?))
        })
        .collect()
}

pub(super) fn catalogue(product: &catalog::Product, mut value: Value) -> Value {
    if let Some(identity) = identity::by_name(product.name) {
        value["productIdentity"] = Resolution::Verified(identity).metadata();
        value["priority"] = json!(identity.rank);
        value["activityState"] =
            json!(if value["workers"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|worker| worker["primaryJob"].is_string()
                    && matches!(
                        worker["jobStatus"].as_str(),
                        Some("running" | "starting" | "pending")
                    ))
            {
                "recorded_active"
            } else {
                "unobserved"
            });
        value["observationScope"] = json!("cc_jobs");
        // Per-ToDo progress is retained below workers; it is never borrowed or averaged.
        value["progress"] = Value::Null;
    }
    value
}
