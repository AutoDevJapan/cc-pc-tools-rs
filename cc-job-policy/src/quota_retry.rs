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
            source["execution_observed"]["model"]
                .as_str()
                .and_then(|model| model.split_once('/'))
                .filter(|(selector, _)| public_selector(selector))
                .map(|(selector, _)| selector),
        )
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
    if same >= MAX || !fresh(&health["openai"], now) || health["openai"]["available"] != true {
        return None;
    }
    available_selector(source, health, now)
}
pub fn available_selector<'a>(source: &Value, health: &'a Value, now: f64) -> Option<&'a str> {
    if !fresh(&health["openai"], now) || health["openai"]["available"] != true {
        return None;
    }
    let used = used(source);
    health["openai"]["safe_routes"]
        .as_array()?
        .iter()
        .find_map(|route| {
            let selected = text(&route["selector"]);
            let unused = !used.contains(&selected)
                && !source["capacity_tried_labels"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .any(|label| {
                        label
                            .as_str()
                            .is_some_and(|label| label == text(&route["label"]))
                    })
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
    if [
        source["retry_not_before"].as_f64(),
        source["rate_limit_reset_at"].as_f64(),
        source["resource_wait"]["until"].as_f64(),
    ]
    .into_iter()
    .flatten()
    .any(|at| !at.is_finite() || at > now)
    {
        return None;
    }
    let (same, switches) = budget(source)?;
    if provider(text(&source["model"])) != Some("openai") {
        if switches >= MAX
            || !crate::quota_recovery::healthy_transition(
                health,
                text(&source["model"]),
                text(&target["model"]),
                now,
            )
        {
            return None;
        }
        let selector = if provider(text(&target["model"])) == Some("openai") {
            let selector = available_selector(source, health, now)?;
            if target["capacity_account_selector"] != selector {
                return None;
            }
            Some(selector)
        } else {
            None
        };
        return Some(
            json!({"kind":"provider_switch","same_model_count":same,"switch_count":switches+1,"selector":selector}),
        );
    }
    if !fresh(&health["openai"], now) && same < MAX {
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
        && (same >= MAX
            || health["openai"]["exhausted"] == true
            || (health["openai"]["safe_routes"].is_array() && !used(source).is_empty()))
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
    if ![Some(1), Some(2)].contains(&proof["version"].as_u64()) {
        return;
    }
    let legacy = if proof["version"] == 1 {
        let Some((same, switches)) = budget(source) else {
            return;
        };
        json!({"kind":"provider_switch","same_model_count":same,"switch_count":switches+1})
    } else {
        Value::Null
    };
    for k in FIELDS {
        job[k] = source[k].clone();
    }
    if !job["requested_model"].is_string() {
        job["requested_model"] = source["model"].clone();
    }
    let retry = if proof["version"] == 1 {
        &legacy
    } else {
        &proof["retry"]
    };
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
    job["quota_retry_seeded_version"] = proof["version"].clone();
    if let Some(selected) = retry["selector"].as_str() {
        let mut tried = source["capacity_tried_accounts"]
            .as_array()
            .cloned()
            .unwrap_or_default();
        tried.push(json!(selected));
        tried.sort_by(|a, b| text(a).cmp(text(b)));
        tried.dedup();
        job["capacity_tried_accounts"] = json!(tried);
        if let Some(label) = proof["health"]["openai"]["safe_routes"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|r| r["selector"] == selected)
            .and_then(|r| r["label"].as_str())
            .filter(|s| public_selector(s))
        {
            let mut labels = source["capacity_tried_labels"]
                .as_array()
                .cloned()
                .unwrap_or_default();
            labels.push(json!(label));
            labels.sort_by(|a, b| text(a).cmp(text(b)));
            labels.dedup();
            job["capacity_tried_labels"] = json!(labels);
        }
    }
}
pub fn inherited(job: &Value, source: &Value, proof: &Value) -> bool {
    let mut expected = job.clone();
    expected["model"] = proof["model"].clone();
    inherit(&mut expected, source, proof);
    let Some((mut same, mut switches)) = budget(&expected) else {
        return false;
    };
    let (Some(prefix), Some(history)) = (
        expected["capacity_history"].as_array(),
        job["capacity_history"].as_array(),
    ) else {
        return false;
    };
    if !history.starts_with(prefix)
        || job["capacity_exhausted"] == true
        || job["effort"] != "max"
        || job["model_origin"] != "policy"
        || count(job, "capacity_wait_count")
            .is_none_or(|n| n < count(&expected, "capacity_wait_count").unwrap_or(MAX))
        || job["requested_model"] != expected["requested_model"]
    {
        return false;
    }
    let mut model = text(&proof["model"]);
    let mut selector = expected["capacity_account_selector"].clone();
    for entry in &history[prefix.len()..] {
        if !crate::same_model(text(&entry["from_model"]), model) {
            return false;
        }
        if entry["kind"] == "same_model_account_retry" {
            if !crate::same_model(text(&entry["to_model"]), model)
                || !public_selector(text(&entry["account_selector"]))
            {
                return false;
            }
            same += 1;
            selector = entry["account_selector"].clone();
        } else {
            if crate::quota_recovery::provider(model)
                == crate::quota_recovery::provider(text(&entry["to_model"]))
            {
                return false;
            }
            switches += 1;
            selector = Value::Null;
        }
        model = text(&entry["to_model"]);
    }
    same <= MAX
        && switches <= MAX
        && job["capacity_same_model_retry_count"].as_u64() == Some(same)
        && job["capacity_retry_count"].as_u64() == Some(switches)
        && crate::same_model(text(&job["model"]), model)
        && job["capacity_account_selector"] == selector
        && ["capacity_tried_accounts", "capacity_tried_labels"]
            .iter()
            .all(|key| {
                expected[*key]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .all(|v| job[*key].as_array().is_some_and(|rows| rows.contains(v)))
            })
}
/// Seed an already registered v1 worker once, preserving its actual later
/// capacity history as a suffix. The historical source/capsule stay unchanged.
pub fn adopt_legacy(job: &mut Value, source: &Value, proof: &Value) -> bool {
    if proof["version"] != 1 || job["quota_retry_seeded_version"] == 1 {
        return true;
    }
    let old = job.clone();
    let Some((same, switches)) = budget(&old) else {
        return false;
    };
    let Some(waits) = count(&old, "capacity_wait_count") else {
        return false;
    };
    let mut seeded = old.clone();
    seeded["model"] = proof["model"].clone();
    inherit(&mut seeded, source, proof);
    if seeded["quota_retry_seeded_version"] != 1 {
        return false;
    }
    let extra = old["capacity_history"]
        .as_array()
        .cloned()
        .unwrap_or_default();
    seeded["capacity_history"]
        .as_array_mut()
        .unwrap()
        .extend(extra);
    seeded["capacity_same_model_retry_count"] = json!(
        seeded["capacity_same_model_retry_count"]
            .as_u64()
            .unwrap_or(MAX)
            + same
    );
    seeded["capacity_retry_count"] =
        json!(seeded["capacity_retry_count"].as_u64().unwrap_or(MAX) + switches);
    seeded["capacity_wait_count"] =
        json!(seeded["capacity_wait_count"].as_u64().unwrap_or(MAX) + waits);
    for key in ["capacity_tried_accounts", "capacity_tried_labels"] {
        let mut values = seeded[key].as_array().cloned().unwrap_or_default();
        values.extend(old[key].as_array().into_iter().flatten().cloned());
        values.sort_by(|a, b| text(a).cmp(text(b)));
        values.dedup();
        seeded[key] = json!(values);
    }
    seeded["model"] = old["model"].clone();
    if !old["capacity_history"].as_array().is_none_or(Vec::is_empty) {
        seeded["capacity_account_selector"] = old["capacity_account_selector"].clone();
    }
    if !inherited(&seeded, source, proof) {
        return false;
    }
    *job = seeded;
    true
}
pub fn current_available(job: &Value, health: &Value, now: f64) -> bool {
    let Some(provider) = crate::quota_recovery::provider(text(&job["model"])) else {
        return false;
    };
    fresh(&health[provider], now)
        && health[provider]["available"] == true
        && (provider != "openai"
            || crate::quota_recovery::safe_openai_selector(
                health,
                Some(text(&job["capacity_account_selector"])),
                now,
            )
            .is_some())
}
pub fn terminal_reason(source: &Value) -> Option<&'static str> {
    let Some((same, switches)) = budget(source) else {
        return Some("invalid_or_exhausted_history");
    };
    (same >= MAX && switches >= MAX).then_some("finite_retry_budget_exhausted")
}
