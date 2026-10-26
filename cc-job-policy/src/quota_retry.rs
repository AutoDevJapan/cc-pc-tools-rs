//! Continue a drained GPT quota failure without creating a new retry budget.
use serde_json::{Value, json};

const MAX: u64 = 3;
const FIELDS: [&str; 9] = [
    "capacity_same_model_retry_count",
    "capacity_retry_count",
    "capacity_wait_count",
    "capacity_history",
    "capacity_account_selector",
    "capacity_tried_accounts",
    "capacity_tried_labels",
    "capacity_exhausted",
    "requested_model",
];
pub fn tracked(source: &Value) -> bool {
    crate::quota_recovery::provider(text(&source["model"])) == Some("openai")
        || source["quota_recovery"]["version"] == 2
        || FIELDS[..3].iter().any(|key| !source[*key].is_null())
        || !source["capacity_history"].is_null()
        || source["capacity_exhausted"] == true
}
fn text(v: &Value) -> &str {
    v.as_str().unwrap_or("")
}
fn count(source: &Value, key: &str) -> Option<u64> {
    if source[key].is_null() {
        Some(0)
    } else {
        source[key].as_u64().filter(|n| *n <= MAX)
    }
}
fn budget(source: &Value) -> Option<(u64, u64)> {
    let same = count(source, FIELDS[0])?;
    let switches = count(source, FIELDS[1])?;
    count(source, FIELDS[2])?;
    let history = match &source["capacity_history"] {
        Value::Null => &[][..],
        Value::Array(rows) => rows.as_slice(),
        _ => return None,
    };
    if source["capacity_exhausted"] == true
        || history
            .iter()
            .filter(|h| h["kind"] == "same_model_account_retry")
            .count() as u64
            > same
        || history
            .iter()
            .filter(|h| {
                h["from_model"].is_string()
                    && h["to_model"].is_string()
                    && !crate::same_model(text(&h["from_model"]), text(&h["to_model"]))
            })
            .count() as u64
            > switches
        || source["quota_recovery"]["retry"]["same_model_count"]
            .as_u64()
            .is_some_and(|n| n > same)
        || source["quota_recovery"]["retry"]["switch_count"]
            .as_u64()
            .is_some_and(|n| n > switches)
    {
        return None;
    }
    Some((same, switches))
}
fn fresh(q: &Value, now: f64) -> bool {
    now.is_finite()
        && q["verified"] == true
        && q["at"]
            .as_f64()
            .is_some_and(|at| at <= now + 1. && now - at <= 60.)
}
fn used(source: &Value) -> Vec<&str> {
    source["capacity_tried_accounts"]
        .as_array()
        .into_iter()
        .flatten()
        .map(text)
        .chain(std::iter::once(text(&source["capacity_account_selector"])))
        .chain(
            source["capacity_history"]
                .as_array()
                .into_iter()
                .flatten()
                .map(|h| text(&h["account_selector"])),
        )
        .filter(|s| !s.is_empty())
        .collect()
}
pub fn selector<'a>(source: &Value, health: &'a Value, now: f64) -> Option<&'a str> {
    let (same, _) = budget(source)?;
    if same >= MAX
        || !fresh(&health["openai"], now)
        || health["openai"]["available"] != true
        || !public_selector(text(&source["capacity_account_selector"]))
    {
        return None;
    }
    let used = used(source);
    health["openai"]["safe_routes"]
        .as_array()?
        .iter()
        .find_map(|route| {
            let selected = text(&route["selector"]);
            let unused = !used.contains(&selected)
                && !route["aliases"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .any(|alias| used.contains(&text(alias)));
            (unused
                && crate::quota_recovery::safe_openai_selector(health, Some(selected), now)
                    .is_some())
            .then_some(selected)
        })
}
pub fn public_selector(s: &str) -> bool {
    s == "main"
        || (s.len() == 7
            && s.starts_with('p')
            && s[1..]
                .bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)))
}
/// The selector names the exact healthy subscription, never just its provider.
pub fn plan(source: &Value, target: &Value, health: &Value, now: f64) -> Option<Value> {
    use crate::quota_recovery::provider;
    if [source["retry_not_before"].as_f64(), source["rate_limit_reset_at"].as_f64(), source["resource_wait"]["until"].as_f64()]
        .into_iter().flatten().any(|at| !at.is_finite() || at > now) { return None; }
    let (same, switches) = budget(source)?;
    if provider(text(&source["model"])) != Some("openai") {
        if switches >= MAX
            || !crate::quota_recovery::healthy_transition(
                health,
                text(&source["model"]),
                text(&target["model"]),
                now,
            )
            || source["capacity_history"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|h| {
                    ["from_model", "to_model"]
                        .iter()
                        .any(|k| crate::same_model(text(&h[*k]), text(&target["model"])))
                })
        {
            return None;
        }
        let selector = if provider(text(&target["model"])) == Some("openai") {
            Some(crate::quota_recovery::safe_openai_selector(
                health,
                Some(text(&target["capacity_account_selector"])),
                now,
            )?)
        } else {
            None
        };
        return Some(
            json!({"kind":"provider_switch","same_model_count":same,"switch_count":switches+1,"selector":selector}),
        );
    }
    if !fresh(&health["openai"], now) {
        return None;
    }
    if provider(text(&target["model"])) == Some("openai") {
        let selected = selector(source, health, now)?;
        if !crate::same_model(text(&source["model"]), text(&target["model"]))
            || target["capacity_account_selector"] != selected
        {
            return None;
        }
        Some(
            json!({"kind":"same_model_account_retry","selector":selected,
            "same_model_count":same+1,"switch_count":switches}),
        )
    } else if provider(text(&target["model"])) == Some("anthropic")
        && switches < MAX
        && fresh(&health["anthropic"], now)
        && health["anthropic"]["available"] == true
        && (same >= MAX || health["openai"]["exhausted"] == true)
        && selector(source, health, now).is_none()
    {
        Some(json!({"kind":"provider_switch","same_model_count":same,"switch_count":switches+1}))
    } else {
        None
    }
}
pub fn binding(source: &Value) -> String {
    let mut v = json!({});
    for k in FIELDS {
        v[k] = source[k].clone();
    }
    v["quota_recovery"] = source["quota_recovery"].clone();
    crate::product_retry::digest(&serde_json::to_vec(&v).unwrap())
}
/// Called only after source/spec/scope/drain validation, never on the old job.
pub fn inherit(job: &mut Value, source: &Value, proof: &Value) {
    if proof["version"] != 2 {
        return;
    }
    for k in FIELDS {
        job[k] = source[k].clone();
    }
    if !job["requested_model"].is_string() {
        job["requested_model"] = source["model"].clone();
    }
    let retry = &proof["retry"];
    job["capacity_same_model_retry_count"] = retry["same_model_count"].clone();
    job["capacity_retry_count"] = retry["switch_count"].clone();
    job["capacity_account_selector"] = retry["selector"].clone();
    let mut history = source["capacity_history"]
        .as_array()
        .cloned()
        .unwrap_or_default();
    history.push(json!({"kind":retry["kind"],"from_model":source["model"],"to_model":job["model"],
        "account_selector":retry["selector"],"failure_kind":"quota","source_job":source["id"],
        "at":proof["health"]["at"],"requested_effort":"max","requested_tier":if retry["selector"].is_string(){"fast"}else{"default"}}));
    job["capacity_history"] = json!(history);
    if let Some(selected) = retry["selector"].as_str() {
        let mut tried = source["capacity_tried_accounts"]
            .as_array()
            .cloned()
            .unwrap_or_default();
        tried.push(json!(selected));
        tried.sort_by(|a, b| text(a).cmp(text(b)));
        tried.dedup();
        job["capacity_tried_accounts"] = json!(tried);
    }
}
pub fn inherited(job: &Value, source: &Value, proof: &Value) -> bool {
    let mut expected = job.clone();
    inherit(&mut expected, source, proof);
    FIELDS.iter().all(|k| expected[*k] == job[*k])
}
