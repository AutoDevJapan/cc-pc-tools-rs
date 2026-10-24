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
    } else {
        None
    }
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
            .or_else(|| q["updatedAt"].is_null().then_some(now))
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
        let exhausted = used.unwrap() >= 100. || short.is_some_and(|v| v >= 100.);
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
        && [from, to].iter().all(|p| {
            let q = &inventory[*p];
            q["verified"] == true
                && q["at"]
                    .as_f64()
                    .is_some_and(|at| at <= now + 1. && now - at <= 60.)
        })
        && inventory[from]["exhausted"] == true
        && inventory[to]["available"] == true
}

fn review(job: &Value) -> bool {
    crate::review_task(s(job, "name"), s(job, "task_type")) || job["subwork"]["kind"] == "review"
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
    v
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
        && s(&item["scheduler"], "owner_stop_job").is_empty()
        && source["last_failure_kind"] == "quota"
        && source["model_origin"] != "owner_request"
        && review(source)
        && source["sandbox"] == "read-only"
        && !crate::launch_context::fixed_execution_contract(source)
        && source["finite_review"]["todo_id"] == item["id"]
        && source["finite_review"]["spec"] == item["spec"]
        && !s(source, "launch_key").is_empty()
        && source["finite_review"]["launch_key"] == source["launch_key"]
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
        || !review(target)
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
    if source["finite_review"]["spec_sha256"]
        .as_str()
        .is_some_and(|old| old != spec_sha256)
    {
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
