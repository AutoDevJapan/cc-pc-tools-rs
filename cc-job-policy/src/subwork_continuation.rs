//! A current parent request creates a new finite contract, never a valid old quota capsule.
use crate::{cancellation, product_retry::digest, quota_recovery as quota};
use serde_json::{Value, json};

fn text<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
fn gen_(v: &Value, key: &str) -> u64 {
    v[key].as_u64().unwrap_or(0)
}
fn hash(v: &Value) -> String {
    digest(&serde_json::to_vec(v).unwrap())
}
fn actor_binding(actor: &Value) -> Value {
    json!({"id":actor["id"],"todo_id":actor["todo_id"],"cwd":actor["cwd"],"work_owner":actor["work_owner"],
        "todo_resume_generation":actor["todo_resume_generation"]})
}
// All three consumers use the same current-parent check. A historical job in
// parent.jobs is provenance, not authority for a newer parent generation.
pub fn parent_registered(db: &Value, item: &Value, actor: &Value) -> bool {
    db["items"].as_array().into_iter().flatten().any(|parent| {
        parent["id"] == actor["todo_id"]
            && parent["id"] == item["subwork"]["parent_todo_id"]
            && !cancellation::owner_stopped(parent, actor)
            && parent["owner_stopped"] != true
            && text(&parent["scheduler"], "owner_stop_job").is_empty()
            && actor["subwork"].is_null()
            && text(actor, "parent_job_id").is_empty()
            && parent["jobs"]
                .as_array()
                .is_some_and(|ids| ids.contains(&actor["id"]))
            && actor["todo_resume_generation"].is_u64()
            && actor["todo_resume_generation"] == parent["resume_generation"]
            && parent["scheduler"]["job"] == actor["id"]
            && (parent["worker_receipt"].is_null()
                || (parent["worker_receipt"].is_object()
                    && parent["worker_receipt"]["job"] == actor["id"]
                    && parent["worker_receipt"]["generation"] == parent["resume_generation"]))
            && ["repository", "git_common", "worktree"].iter().all(|k| {
                !text(&actor["work_owner"], k).is_empty()
                    && actor["work_owner"][*k] == item["subwork"]["owner"][*k]
            })
    })
}
pub fn unheld(item: &Value, source: &Value) -> bool {
    ["todo", "doing"].contains(&text(item, "status"))
        && item["dispatchable"] == true
        && !cancellation::blocks_recovery(item, source)
        && item["owner_stopped"] != true
        && [
            "owner_question_wait",
            "decision_wait",
            "wait_ready",
            "stop_kind",
            "work_owner_wait_on",
        ]
        .iter()
        .all(|k| item[*k].is_null())
        && item["wait_on"].as_array().is_none_or(Vec::is_empty)
        && [
            "owner_stop_job",
            "pending_cancel",
            "question",
            "question_close_pending",
        ]
        .iter()
        .all(|k| {
            let v = &item["scheduler"][*k];
            v.is_null() || v == false || v == ""
        })
}
fn retry_budget_known(source: &Value) -> bool {
    let proof = &source["quota_recovery"];
    // A v1 capsule predates retry details. Only an already restored snapshot
    // proves its spent switch; this new-generation intake cannot validate the
    // old capsule's predecessor from a caller flag or reconstruct missing data.
    (proof["version"] != 1
        || (source["quota_retry_seeded_version"] == 1
            && source["capacity_retry_count"]
                .as_u64()
                .is_some_and(|n| n > 0)
            && source["capacity_history"]
                .as_array()
                .is_some_and(|history| {
                    history.iter().any(|entry| {
                        entry["kind"] == "provider_switch"
                            && entry["failure_kind"] == "quota"
                            && entry["source_job"].is_string()
                            && entry["source_job"] == proof["source_job"]
                            && entry["to_model"].is_string()
                            && entry["to_model"] == proof["model"]
                    })
                })))
        && crate::quota_retry::terminal_reason(source).is_none()
}
/// Caller additionally checks actual parent ownership, real process drain and dependencies.
pub fn request(item: &Value, source: &Value, actor: &Value) -> Option<Value> {
    let finite = if item["subwork"]["kind"] == "review" {
        &source["finite_review"]
    } else {
        &source["finite_subwork"]
    };
    let generation = gen_(item, "resume_generation");
    let dependency = gen_(item, "dependency_generation");
    let begin = chrono::DateTime::parse_from_rfc3339(text(source, "started_at")).ok()?;
    let end = chrono::DateTime::parse_from_rfc3339(text(source, "finished_at")).ok()?;
    if !unheld(item, source)
        || !["research", "review"].contains(&text(&item["subwork"], "kind"))
        || item["scheduler"]["last_job"] != source["id"]
        || text(source, "id").is_empty()
        || source["todo_id"] != item["id"]
        || source["status"] != "failed"
        || !quota::quota_source(source)
        || source["model_origin"] == "owner_request"
        || source["subwork"] != item["subwork"]
        || !item["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&source["id"]))
        || source["todo_resume_generation"]
            .as_u64()
            .is_none_or(|g| g >= generation)
        || source["todo_dependency_generation"]
            .as_u64()
            .is_none_or(|g| g >= dependency)
        || actor["todo_id"] != item["subwork"]["parent_todo_id"]
        || text(actor, "id").is_empty()
        || !actor["todo_resume_generation"].is_u64()
        || source["sandbox"] != "read-only"
        || crate::launch_context::fixed_execution_contract(source)
        || quota::collection_pending(source)
        || end < begin
        || crate::work_owner::norm(std::path::Path::new(text(source, "cwd")))
            != crate::work_owner::norm(std::path::Path::new(text(&item["scheduler"], "worktree")))
        || finite["todo_id"] != item["id"]
        || finite["spec"] != item["spec"]
        || text(source, "launch_key").is_empty()
        || finite["launch_key"] != source["launch_key"]
        || !retry_budget_known(source)
    {
        return None;
    }
    let bytes = std::fs::read(text(item, "spec"))
        .ok()
        .filter(|b| !b.is_empty() && b.len() <= 1024 * 1024)?;
    let spec_sha256 = digest(&bytes);
    if finite["spec_sha256"] != spec_sha256 {
        return None;
    }
    // Explicit current inputs may narrow neither the source nor its meaning.
    let mut target = source.clone();
    for k in ["sandbox", "mcp_profile", "network", "add_dirs"] {
        if let Some(value) = item.get(k).filter(|v| !v.is_null()).or_else(|| {
            item["scheduler"]["console_request"]["options"]
                .get(k)
                .filter(|v| !v.is_null())
        }) {
            target[k] = value.clone();
        }
    }
    if quota::scope(&target) != quota::scope(source) {
        return None;
    }
    Some(
        json!({"version":1,"source_job":source["id"],"actor_job":actor["id"],
        "actor_resume_generation":actor["todo_resume_generation"],
        "actor_sha256":hash(&actor_binding(actor)),"source_sha256":hash(&quota::source_binding(source)),
        "retry_source_sha256":crate::quota_retry::binding(source),
        "request_sha256":hash(&quota::request(item)),"resume_generation":generation,
        "dependency_generation":dependency,"spec_sha256":spec_sha256}),
    )
}
pub fn current(proof: &Value, item: &Value, source: &Value, actor: &Value) -> bool {
    proof.is_object() && request(item, source, actor).as_ref() == Some(proof)
}
/// Once the exact next worker owns this request, a later ordinary attempt must
/// use that worker's own result. The retained original request grants no new exception.
pub fn consumed(item: &Value, source: &Value) -> bool {
    let request = &item["subwork_continuation"];
    request.is_object()
        && source["subwork_continuation"]["request"] == *request
        && source["subwork_continuation"]["launch_key"] == source["launch_key"]
        && source["todo_resume_generation"].as_u64()
            == request["resume_generation"]
                .as_u64()
                .and_then(|g| g.checked_add(1))
        && source["todo_resume_generation"] == item["resume_generation"]
        && item["worker_receipt"]["generation"] == item["resume_generation"]
        && item["worker_receipt"]["job"] == source["id"]
        && source["todo_id"] == item["id"]
        && source["subwork"] == item["subwork"]
        && item["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&source["id"]))
}
pub fn available(health: &Value, model: &str, now: f64) -> bool {
    if model
        .split_once('/')
        .is_some_and(|(p, _)| !["openai", "anthropic"].contains(&p))
    {
        return false;
    }
    let Some(provider) = quota::provider(model).filter(|p| ["openai", "anthropic"].contains(p))
    else {
        return false;
    };
    let q = &health[provider];
    now.is_finite()
        && q["verified"] == true
        && q["available"] == true
        && q["exhausted"] == false
        && q["at"]
            .as_f64()
            .is_some_and(|at| at <= now + 1. && now - at <= 60.)
}
pub fn receipt(
    request: &Value,
    item: &Value,
    source: &Value,
    actor: &Value,
    target: &Value,
    health: &Value,
    now: f64,
) -> Option<Value> {
    if !current(request, item, source, actor)
        || !available(health, text(target, "model"), now)
        || target["effort"] != "max"
        || quota::scope(target) != quota::scope(source)
        || !["policy", "owner_request"].contains(&text(target, "model_origin"))
        || target["task_type"] != item["subwork"]["kind"]
        || text(target, "launch_key").is_empty()
        || target["launch_key"] == source["launch_key"]
        || (quota::provider(text(target, "model")) == Some("openai")
            && quota::safe_openai_selector(
                health,
                Some(text(target, "capacity_account_selector")),
                now,
            )
            .is_none())
    {
        return None;
    }
    let mut safe_health = json!({});
    for provider in ["openai", "anthropic"] {
        for field in ["at", "verified", "available", "exhausted"] {
            safe_health[provider][field] = health[provider][field].clone();
        }
    }
    safe_health["openai"]["safe_routes"] = health["openai"]["safe_routes"].clone();
    Some(
        json!({"version":1,"request":request,"launch_key":target["launch_key"],"scope_sha256":hash(&quota::scope(target)),
        "model":target["model"],"effort":target["effort"],"model_origin":target["model_origin"],"effort_origin":target["effort_origin"],
        "selector":target["capacity_account_selector"],"health":safe_health,"at":now}),
    )
}
/// Link may transfer exactly once to the next generation. No historical record is edited.
pub fn verify(
    proof: &Value,
    item: &Value,
    source: &Value,
    actor: &Value,
    target: &Value,
    health: &Value,
    now: f64,
) -> bool {
    let mut prepared = item.clone();
    let request = &proof["request"];
    if item["status"] == "doing" {
        if item["scheduler"]["job"] != target["id"]
            || item["worker_receipt"]["job"] != target["id"]
            || item["worker_receipt"]["generation"] != item["resume_generation"]
            || item["resume_generation"].as_u64()
                != request["resume_generation"]
                    .as_u64()
                    .and_then(|g| g.checked_add(1))
            || item["resume_generation"] != target["todo_resume_generation"]
        {
            return false;
        }
        prepared["resume_generation"] = request["resume_generation"].clone();
    }
    let mut initial = target.clone();
    for field in ["model", "effort", "model_origin", "effort_origin"] {
        initial[field] = proof[field].clone();
    }
    initial["capacity_account_selector"] = proof["selector"].clone();
    let original_at = proof["at"].as_f64().unwrap_or(f64::NAN);
    item["subwork_continuation"] == *request
        && target["subwork"] == item["subwork"]
        && available(health, text(target, "model"), now)
        && target["effort"] == proof["effort"]
        && receipt(
            request,
            &prepared,
            source,
            actor,
            &initial,
            &proof["health"],
            original_at,
        )
        .as_ref()
            == Some(proof)
        && target["todo_dependency_generation"] == item["dependency_generation"]
}
pub const RETRY_FIELDS: [&str; 7] = [
    "capacity_same_model_retry_count",
    "capacity_retry_count",
    "capacity_wait_count",
    "capacity_tried_accounts",
    "capacity_tried_labels",
    "capacity_history",
    "capacity_exhausted",
];
pub fn inherited(job: &Value, source: &Value) -> bool {
    RETRY_FIELDS.iter().all(|key| job[*key] == source[*key])
}
