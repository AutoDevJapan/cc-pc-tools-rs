//! One prepared review transition after verified OAuth exhaustion. No user flag
//! grants this route; launchers must reload the source and prove process exit.
use serde_json::{Value, json};
// The existing adaptive quota sampler accepts its 300s interval plus 60s.
// A freshly collected launch capsule below has a separate 60s handoff lifetime.
const QUOTA_METADATA_MAX_AGE: f64 = 360.;

fn s<'a>(v: &'a Value, k: &str) -> &'a str {
    v[k].as_str().unwrap_or("")
}
fn generation(v: &Value, k: &str) -> u64 {
    v[k].as_u64().unwrap_or(0)
}
fn digest(v: &Value) -> String {
    crate::product_retry::digest(&serde_json::to_vec(v).unwrap())
}
fn timestamp(v: &Value) -> Option<f64> {
    v.as_f64()
        .map(|n| if n > 1e12 { n / 1000. } else { n })
        .or_else(|| {
            chrono::DateTime::parse_from_rfc3339(v.as_str()?)
                .ok()
                .map(|t| t.timestamp_millis() as f64 / 1000.)
        })
        .filter(|n| n.is_finite())
}
pub fn provider(model: &str) -> Option<&'static str> {
    if ["gpt-6.1-sol", "gpt-6-luna", "gpt-6-astra"]
        .iter()
        .any(|m| crate::same_model(model, m))
    {
        Some("openai")
    } else if ["opus", "sonnet", "haiku"]
        .iter()
        .any(|m| crate::same_model(model, m))
    {
        Some("anthropic")
    } else if crate::same_model(model, "grok-4.7") {
        Some("xai")
    } else {
        None
    }
}

/// This exact provider error also appears inside remote-compaction HTTP402 errors.
/// A status code alone is not evidence of exhausted subscription allowance.
pub fn grok_balance_exhausted(message: &str) -> bool {
    message
        .to_ascii_lowercase()
        .contains("grok build usage balance exhausted")
}

pub fn quota_source(source: &Value) -> bool {
    source["last_failure_kind"] == "quota"
        || (provider(s(source, "model")) == Some("xai")
            && source["last_failure_kind"] == "transient"
            && grok_balance_exhausted(s(source, "last_error")))
}

/// Consume the normal refreshed account-list reply in memory. Never return an
/// account identifier, label, raw reply or an error string from that reply.
pub fn aggregate(raw: &Value, provider: &str, now: f64) -> Value {
    let mut known = true;
    let mut count = 0;
    let mut available = false;
    let mut reason = "no_eligible_oauth";
    let Some(rows) = raw["accounts"].as_array() else {
        return json!({"at":now,"verified":false,"exhausted":false,"available":false,"reason":"metadata_missing"});
    };
    for row in rows {
        if !["oauth", "codex"].contains(&s(row, "type")) {
            continue;
        }
        if row["paused"] == true || row["needsReauth"] == true || row["validationPending"] == true {
            continue;
        }
        count += 1;
        let q = &row["quota"];
        let health = row["health"]
            .as_str()
            .or(row["health"]["status"].as_str())
            .unwrap_or("")
            .to_ascii_lowercase();
        let healthy = ["healthy", "ok"].contains(&health.as_str());
        let limited = ["rate limited", "rate_limited", "ratelimited"].contains(&health.as_str());
        let used = q["weeklyPercent"]
            .as_f64()
            .filter(|v| (0. ..=100.).contains(v));
        let reset = timestamp(&q["weeklyResetAt"]).filter(|v| *v > now);
        let updated = timestamp(&q["updatedAt"])
            // OpenAI's successful --quota --refresh reply omits updatedAt.
            // Its collection time is an observation; a supplied stale time or
            // explicit refresh failure is never replaced with the current time.
            .or_else(|| (provider == "openai" && q["updatedAt"].is_null()).then_some(now))
            .filter(|v| *v <= now + 1. && now - *v <= QUOTA_METADATA_MAX_AGE);
        let refreshed = row["quotaRefresh"]["status"]
            .as_str()
            .is_none_or(|v| v == "ok");
        if used.is_none()
            || reset.is_none()
            || updated.is_none()
            || !refreshed
            || row["quotaUnavailable"] == true
            || !row["quotaFailure"].is_null()
            || (!healthy && !limited)
        {
            known = false;
            reason = if used.is_none() || reset.is_none() {
                "quota_window_unknown"
            } else if updated.is_none() {
                "quota_time_stale"
            } else if !refreshed
                || row["quotaUnavailable"] == true
                || !row["quotaFailure"].is_null()
            {
                "quota_refresh_failed"
            } else {
                "health_unknown"
            };
            continue;
        }
        let short = q["fiveHourPercent"].as_f64().or(q["shortPercent"].as_f64());
        if short.is_some_and(|v| !(0. ..=100.).contains(&v)) {
            known = false;
            reason = "short_window_invalid";
            continue;
        }
        let short_reset =
            timestamp(&q["fiveHourResetAt"]).or_else(|| timestamp(&q["shortResetAt"]));
        let short_exhausted =
            short.is_some_and(|v| v >= 100.) && short_reset.is_none_or(|at| at > now);
        let exhausted = used.unwrap() >= 100. || short_exhausted;
        // Ineligibility for paid overage cannot establish an available free
        // route. It may still prove that the subscription itself is exhausted.
        if !exhausted {
            if !healthy
                || row["creditsAfterLimit"] != false
                || row["hasCredential"] == false
                || row["autoSelectable"] == false
                || row["selectionExcludedReason"].is_string()
                || (provider == "openai" && row["type"] != "codex")
            {
                known = false;
                reason = "oauth_selection_unverified";
                continue;
            }
            available = true;
        }
    }
    if available {
        reason = "available_oauth";
    } else if known && count > 0 {
        reason = "exhausted_oauth";
    }
    json!({"at":now,"verified":available || (known && count > 0),"exhausted":known && count > 0 && !available,"available":available,"reason":reason})
}

pub fn healthy_transition(inventory: &Value, from: &str, to: &str, now: f64) -> bool {
    if !now.is_finite() {
        return false;
    }
    let (Some(from), Some(to)) = (provider(from), provider(to)) else {
        return false;
    };
    from != to
        && [from, to].iter().filter(|p| **p != "xai").all(|p| {
            let q = &inventory[*p];
            q["verified"] == true
                && q["at"]
                    .as_f64()
                    .is_some_and(|at| at <= now + 1. && now - at <= 60.)
        })
        // Grok's explicit balance error is checked on the bound source by the
        // caller. It has no refreshable quota window in the existing sampler.
        && (from == "xai" || inventory[from]["exhausted"] == true)
        && to != "xai"
        && inventory[to]["available"] == true
}

/// Public request-scoped namespaces only. Private account IDs and the namespace
/// mapping stay in memory. Each returned route itself is fresh OAuth/no-overage;
/// an unrelated available account can never qualify an unknown selected route.
pub fn safe_openai_routes(raw: &Value, namespaces: &Value, now: f64) -> Value {
    let (Some(rows), Some(namespaces)) = (raw["accounts"].as_array(), namespaces.as_object())
    else {
        return json!([]);
    };
    let mut routes = Vec::new();
    for (selector, target) in namespaces {
        let public = selector == "main"
            || (selector.len() == 7
                && selector.starts_with('p')
                && selector[1..]
                    .bytes()
                    .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)));
        let Some(target) = target.as_str().filter(|_| public) else {
            continue;
        };
        let target = if target == "@main" {
            "__main__"
        } else {
            target
        };
        let rows: Vec<_> = rows.iter().filter(|r| r["id"] == target).collect();
        if rows.len() != 1
            || rows[0]["type"] != "codex"
            || rows[0]["creditsAfterLimit"] != false
            || !match rows[0].get("hasCredential") {
                Some(Value::Bool(true)) => true,
                // The normal refreshed OpenCodex account-list schema omits
                // hasCredential. Its explicit needsReauth=false, Healthy and
                // successfully refreshed quota together establish the route.
                None => rows[0]["needsReauth"] == false,
                _ => false,
            }
        {
            continue;
        }
        let Some(checked) = timestamp(&rows[0]["quota"]["updatedAt"])
            .or_else(|| rows[0]["quota"]["updatedAt"].is_null().then_some(now))
            .filter(|at| *at <= now + 1. && now - *at <= 60.)
        else {
            continue;
        };
        let single = json!({"accounts":[rows[0]]});
        let q = aggregate(&single, "openai", now);
        if q["verified"] == true && q["available"] == true {
            routes.push(json!({"selector":selector,"at":checked,"verified":true}));
        }
    }
    routes.sort_by(|a, b| s(a, "selector").cmp(s(b, "selector")));
    json!(routes)
}

pub fn safe_openai_selector<'a>(
    health: &'a Value,
    expected: Option<&str>,
    now: f64,
) -> Option<&'a str> {
    if !now.is_finite() {
        return None;
    }
    health["openai"]["safe_routes"]
        .as_array()?
        .iter()
        .find_map(|route| {
            let selector = s(route, "selector");
            let public = selector == "main"
                || (selector.len() == 7
                    && selector.starts_with('p')
                    && selector[1..]
                        .bytes()
                        .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)));
            (public
                && expected.is_none_or(|e| e == selector)
                && route["verified"] == true
                && route["at"]
                    .as_f64()
                    .is_some_and(|at| at <= now + 1. && now - at <= 60.))
            .then_some(selector)
        })
}

fn review(job: &Value) -> bool {
    crate::review_task(s(job, "name"), s(job, "task_type")) || job["subwork"]["kind"] == "review"
}
fn finite_spec(source: &Value) -> &Value {
    if review(source) {
        &source["finite_review"]
    } else {
        &source["finite_subwork"]
    }
}
fn scope(job: &Value) -> Value {
    let mut dirs: Vec<String> = job["add_dirs"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .map(|v| crate::work_owner::norm(std::path::Path::new(v)))
        .collect();
    dirs.sort();
    dirs.dedup();
    json!({"cwd":crate::work_owner::norm(std::path::Path::new(s(job,"cwd"))),
        "sandbox":job["sandbox"],"network":job["network"] == true,"add_dirs":dirs,
        "mcp_profile":job["mcp_profile"],"review":review(job),
        "approval_policy":job["approval_policy"],"approvals_reviewer":job["approvals_reviewer"]})
}
fn request(item: &Value) -> Value {
    let mut v = json!({});
    for field in [
        "id",
        "project",
        "spec",
        "model",
        "effort",
        "model_origin",
        "effort_origin",
        "needs",
        "subwork",
        "review_source_jobs",
        "task_type",
        "sandbox",
        "mcp_profile",
        "add_dirs",
        "network",
        "dependency_generation",
        "resume_generation",
    ] {
        v[field] = item[field].clone();
    }
    v["worktree"] = item["scheduler"]["worktree"].clone();
    v["branch"] = item["scheduler"]["branch"].clone();
    v["options"] = item["scheduler"]["console_request"]["options"].clone();
    v["resume_generation"] = json!(generation(item, "resume_generation"));
    v["dependency_generation"] = json!(generation(item, "dependency_generation"));
    v
}
fn source_binding(source: &Value) -> Value {
    let mut v = scope(source);
    for k in [
        "id",
        "todo_id",
        "launch_key",
        "model",
        "model_origin",
        "started_at",
        "finished_at",
        "status",
        "last_failure_kind",
        "recovery_cancel",
        "finite_review",
        "subwork",
        "todo_dependency_generation",
        "todo_resume_generation",
        "review_source_jobs",
        "review_author_models",
    ] {
        v[k] = source[k].clone();
    }
    if provider(s(source, "model")) == Some("xai") || source["finite_subwork"].is_object() {
        v["finite_subwork"] = source["finite_subwork"].clone();
        v["failure_sha256"] = json!(digest(&source["last_error"]));
    }
    // Do not change this v1 digest: a still-running installed scheduler can
    // prepare the capsule with the previous policy. eligible_source rechecks
    // the current collection record before any consumer accepts that capsule.
    v
}

/// A retained stop intent belongs only to its exact native attempt. Legacy
/// sources and receipts from older attempts remain compatible.
pub fn collection_pending(source: &Value) -> bool {
    let intent = &source["protocol_stall"];
    intent["child_pid"].as_u64().is_some_and(|pid| pid > 0)
        && intent["child_created"].is_u64()
        && intent["stage"] != "drained"
        && [
            ("child_pid", "codex_pid"),
            ("child_created", "codex_pid_created"),
            ("started_at", "started_at"),
            ("attempt_events_offset", "attempt_events_offset"),
        ]
        .iter()
        .all(|(saved, current)| intent[*saved] == source[*current])
}

pub fn eligible_source(item: &Value, source: &Value) -> bool {
    let source_generation = source["todo_resume_generation"]
        .as_u64()
        .or_else(|| {
            (item["worker_receipt"]["job"] == source["id"])
                .then(|| item["worker_receipt"]["generation"].as_u64())
                .flatten()
        })
        .or_else(|| item["processed_results"][s(source, "id")]["generation"].as_u64());
    !s(source, "id").is_empty()
        && source["todo_id"] == item["id"]
        && item["scheduler"]["last_job"] == source["id"]
        && item["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&source["id"]))
        && (source["status"] == "failed"
            || (crate::cancellation::cancelled(source)
                && crate::cancellation::recovery_cancel(item, source)))
        && !crate::cancellation::owner_stopped(item, source)
        && item["owner_stopped"] != true
        && item["owner_question_wait"].is_null()
        && item["decision_wait"].is_null()
        && !collection_pending(source)
        && s(&item["scheduler"], "owner_stop_job").is_empty()
        && quota_source(source)
        && source["model_origin"] != "owner_request"
        && (review(source)
            || (source["subwork"] == item["subwork"] && source["subwork"]["kind"] == "research"))
        && source["sandbox"] == "read-only"
        && !crate::launch_context::fixed_execution_contract(source)
        && finite_spec(source)["todo_id"] == item["id"]
        && finite_spec(source)["spec"] == item["spec"]
        && !s(source, "launch_key").is_empty()
        && finite_spec(source)["launch_key"] == source["launch_key"]
        && generation(item, "dependency_generation")
            == generation(source, "todo_dependency_generation")
        && source_generation == Some(generation(item, "resume_generation"))
        && timestamp(&source["started_at"])
            .zip(timestamp(&source["finished_at"]))
            .is_some_and(|(a, b)| b >= a)
}

/// The caller proves drain and author independence separately. A digest binds
/// the exact saved request, source generation and execution scope across APIs.
pub fn receipt(
    item: &Value,
    source: &Value,
    target: &Value,
    health: &Value,
    now: f64,
) -> Option<Value> {
    if !eligible_source(item, source)
        || !["todo", "doing"].contains(&s(item, "status"))
        || item["dispatchable"] != true
        || item["wait_on"].as_array().is_some_and(|v| !v.is_empty())
        || target["model_origin"] != "policy"
        || review(source) != review(target)
        || target["sandbox"] != "read-only"
        || target["effort"] != "max"
        || scope(source) != scope(target)
        || s(target, "launch_key").is_empty()
        || target["launch_key"] == source["launch_key"]
        || timestamp(&source["finished_at"]).is_none_or(|at| at > now + 1.)
        || (source["recovery_cancel"].is_object()
            && timestamp(&source["recovery_cancel"]["committed_at"])
                .is_some_and(|at| at > now + 1.))
        || !healthy_transition(health, s(source, "model"), s(target, "model"), now)
    {
        return None;
    }
    if provider(s(target, "model")) == Some("openai")
        && !["gpt-6.1-sol", "gpt-6-luna", "gpt-6-astra"].contains(
            &s(target, "model")
                .strip_prefix("openai/")
                .unwrap_or(s(target, "model")),
        )
    {
        return None;
    }
    let spec = std::fs::read(s(item, "spec"))
        .ok()
        .filter(|b| b.len() <= 1024 * 1024)?;
    let spec_sha256 = crate::product_retry::digest(&spec);
    // A legacy path alone cannot prove which body the original reader ran.
    // Never manufacture that missing evidence from the current file.
    if finite_spec(source)["spec_sha256"] != spec_sha256 {
        return None;
    }
    let mut safe_health = json!({"at":health["at"]});
    for provider in ["openai", "anthropic"] {
        let q = &health[provider];
        safe_health[provider] = json!({"at":q["at"],"verified":q["verified"],"exhausted":q["exhausted"],"available":q["available"]});
    }
    Some(
        json!({"version":1,"source_job":source["id"],"source_sha256":digest(&source_binding(source)),
        "resume_generation":generation(item,"resume_generation"),
        "request_sha256":digest(&request(item)),"launch_key":target["launch_key"],
        "model":target["model"],"effort":target["effort"],"model_origin":"policy",
        "spec_sha256":spec_sha256,"scope_sha256":digest(&scope(target)),"health":safe_health}),
    )
}

pub fn verify(proof: &Value, item: &Value, source: &Value, target: &Value, now: f64) -> bool {
    let mut prepared = item.clone();
    if item["status"] == "doing" {
        let Some(before) = proof["resume_generation"].as_u64() else {
            return false;
        };
        if item["scheduler"]["job"] != target["id"]
            || item["worker_receipt"]["job"] != target["id"]
            || item["worker_receipt"]["generation"] != item["resume_generation"]
            || item["resume_generation"].as_u64() != before.checked_add(1)
            || item["resume_generation"] != target["todo_resume_generation"]
        {
            return false;
        }
        prepared["resume_generation"] = json!(before);
    }
    receipt(&prepared, source, target, &proof["health"], now).is_some_and(|p| p == *proof)
}

#[cfg(test)]
mod unstarted_routes_tests {
    use super::*;
    #[test]
    fn public_route_is_bound_to_its_own_fresh_subscription_and_disabled_overage() {
        let row = json!({"id":"private-one","type":"codex","creditsAfterLimit":false,"hasCredential":true,"health":"healthy","autoSelectable":true,
            "quota":{"weeklyPercent":10.,"weeklyResetAt":2000.,"updatedAt":1000.}});
        let raw = json!({"accounts":[row]});
        let namespaces = json!({"pabcdef":"private-one","p000000":"unknown"});
        let routes = safe_openai_routes(&raw, &namespaces, 1000.);
        assert_eq!(
            routes,
            json!([{"selector":"pabcdef","at":1000.,"verified":true}])
        );
        assert!(
            !serde_json::to_string(&routes)
                .unwrap()
                .contains("private-one")
        );
        let health = json!({"openai":{"safe_routes":routes}});
        assert_eq!(
            safe_openai_selector(&health, Some("pabcdef"), 1000.),
            Some("pabcdef")
        );
        let mut normal_cli = raw.clone();
        normal_cli["accounts"][0]
            .as_object_mut()
            .unwrap()
            .remove("hasCredential");
        normal_cli["accounts"][0]["needsReauth"] = json!(false);
        assert_eq!(safe_openai_routes(&normal_cli, &namespaces, 1000.), routes);
        normal_cli["accounts"][0]["hasCredential"] = Value::Null;
        assert_eq!(
            safe_openai_routes(&normal_cli, &namespaces, 1000.),
            json!([])
        );
        assert_eq!(
            safe_openai_selector(&health, Some("p000000"), 1000.),
            None,
            "another available route must not qualify the requested unknown route"
        );
        assert_eq!(safe_openai_selector(&health, None, 1061.), None);
        for (key, value) in [
            ("type", json!("api_key")),
            ("creditsAfterLimit", json!(true)),
            ("creditsAfterLimit", Value::Null),
            ("hasCredential", json!(false)),
            ("autoSelectable", json!(false)),
        ] {
            let mut changed = raw.clone();
            changed["accounts"][0][key] = value;
            assert_eq!(
                safe_openai_routes(&changed, &namespaces, 1000.),
                json!([]),
                "{key}"
            );
        }
        let mut stale = raw;
        stale["accounts"][0]["quota"]["updatedAt"] = json!(500.);
        assert_eq!(safe_openai_routes(&stale, &namespaces, 1000.), json!([]));
    }
}
