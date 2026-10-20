//! Content-free, request-scoped selection using OpenCodex's existing public
//! account namespaces. Never changes the active account, pool policy or picker.
use crate::*;

pub const MAX_SAME_MODEL_RETRIES: u64 = 3;
pub const RETRY_DELAYS: [f64; 3] = [5., 15., 30.];

pub fn public_selector(value: &str) -> bool {
    value == "main"
        || (value.len() == 7
            && value.starts_with('p')
            && value[1..]
                .bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)))
}

/// Only known GPT identities participate. Unknown owner models stay untouched.
pub fn native_model(model: &str) -> Option<&str> {
    let model = model.strip_prefix("openai/").unwrap_or(model);
    ["gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna"]
        .contains(&model)
        .then_some(model)
}

pub fn requested_tier(model: &str) -> Option<&'static str> {
    let lower = model.to_ascii_lowercase();
    if native_model(model).is_some() {
        Some("fast")
    } else if lower
        .strip_prefix("anthropic/")
        .unwrap_or(&lower)
        .starts_with("claude-")
        || ["opus", "sonnet", "haiku"].contains(&lower.as_str())
    {
        Some("default")
    } else {
        None
    }
}

/// Preserve a drained failure without overwriting a newer worker or scope.
pub fn stale_completion(source: &Value, fresh: &Value, now: &str) -> Value {
    let mut result = fresh.clone();
    result["stale_capacity_completion"] = json!({"at":now,"failure_kind":"capacity",
        "started_at":source["started_at"],"child_birth":source["codex_pid_created"]});
    if s(fresh, "status") == "running"
        && fresh["cancel_requested"] != true
        && source["codex_pid"].as_u64().is_some_and(|pid| pid > 0)
        && source["codex_pid_created"].as_u64().is_some()
        && [
            "id",
            "pid",
            "pid_created",
            "codex_pid",
            "codex_pid_created",
            "started_at",
            "attempt_events_offset",
        ]
        .iter()
        .all(|field| source[field] == fresh[field])
    {
        finish(
            &mut result,
            source["exit_code"].as_i64(),
            source["duration_ms"].as_i64().unwrap_or(0),
            now,
        );
        result["last_failure_kind"] = json!("capacity");
        result["last_error"] =
            json!("Selected model is at capacity; completed attempt scope changed");
    }
    result
}

pub fn launch_model(job: &Value) -> String {
    match (
        native_model(s(job, "model")),
        s(job, "capacity_account_selector"),
    ) {
        (Some(model), selector) if public_selector(selector) => format!("{selector}/{model}"),
        _ => s(job, "model").into(),
    }
}

/// Native session metadata is separate from launch requests and provider facts.
pub fn observe_turn_context(job: &mut Value, payload: &Value) {
    let model = s(payload, "model");
    if model != s(job, "model") && model != launch_model(job) {
        return;
    }
    let effort = payload
        .get("effort")
        .or_else(|| payload.get("reasoning_effort"))
        .and_then(Value::as_str);
    let mut observed = json!({"source":"native_turn_context","model":model});
    if effort
        .is_some_and(|v| ["none", "minimal", "low", "medium", "high", "xhigh", "max"].contains(&v))
    {
        observed["effort"] = json!(effort);
    }
    if [
        "fast",
        "priority",
        "default",
        "standard",
        "flex",
        "ultrafast",
    ]
    .contains(&s(payload, "service_tier"))
    {
        observed["service_tier"] = payload["service_tier"].clone();
    }
    job["execution_observed"] = observed;
}

pub fn same_attempt(source: &Value, fresh: &Value) -> bool {
    s(fresh, "status") == "running"
        && fresh["cancel_requested"] != true
        && [
            "id",
            "todo_id",
            "related_todo_ids",
            "launch_key",
            "pid",
            "pid_created",
            "codex_pid",
            "codex_pid_created",
            "started_at",
            "attempt_events_offset",
            "model",
            "requested_model",
            "model_explicit",
            "model_origin",
            "effort",
            "effort_origin",
            "cwd",
            "sandbox",
            "scope_source",
            "add_dirs",
            "network",
            "mcp_profile",
            "approval_policy",
            "approvals_reviewer",
            "fixed_read_only_review",
            "fixed_review_requalification",
            "review_source_jobs",
            "review_excluded_models",
            "review_author_models",
            "review_source_verified",
            "prompt",
            "prompt_source",
            "task_type",
            "capacity_account_selector",
            "capacity_same_model_retry_count",
            "capacity_retry_count",
            "capacity_tried_accounts",
            "capacity_tried_labels",
            "capacity_wait_count",
            "capacity_wait_pending",
            "retry_not_before",
            "finite_review",
            "todo_dependency_generation",
        ]
        .iter()
        .all(|field| source[field] == fresh[field])
}

pub fn same_queued_attempt(source: &Value, fresh: &Value) -> bool {
    if s(fresh, "status") != "queued" {
        return false;
    }
    let mut running = fresh.clone();
    running["status"] = json!("running");
    same_attempt(source, &running)
}

/// Called with queue -> state -> ToDo locks held. A saved job cannot resurrect
/// an owner's stop, completed task, replaced worker or new dependency generation.
pub fn todo_owns_attempt(db: &Value, job: &Value) -> bool {
    let Some(id) = job["todo_id"].as_u64() else {
        return true;
    };
    let Some(row) = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|r| r["id"] == id)
    else {
        return false;
    };
    if !["todo", "doing"].contains(&s(row, "status"))
        || cc_job_policy::cancellation::owner_stopped(row, job)
        || row["owner_stopped"] == true
        || !s(&row["scheduler"], "owner_stop_job").is_empty()
        || row["dependency_generation"].as_u64().unwrap_or(0)
            != job["todo_dependency_generation"].as_u64().unwrap_or(0)
    {
        return false;
    }
    if !s(&row["scheduler"], "job").is_empty() {
        return row["scheduler"]["job"] == job["id"];
    }
    row["jobs"]
        .as_array()
        .is_some_and(|jobs| jobs.contains(&job["id"]))
}

fn observed_model(model: &str) -> &str {
    model
        .split_once('/')
        .filter(|(p, _)| public_selector(p) || *p == "openai")
        .map(|(_, model)| model)
        .unwrap_or(model)
}

fn labels(value: &Value) -> Vec<String> {
    value
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .filter(|v| public_selector(v))
        .map(str::to_owned)
        .collect()
}

/// CLI replies live only in memory. The return value contains public random
/// selectors, bounded model/effort/tier observations and time, never account IDs.
pub fn snapshot(accounts: &Value, namespaces: &Value, logs: &Value, job: &Value, at: f64) -> Value {
    let mut result = json!({"at":at,"verified":false,"eligible":[],"failed":[]});
    let (Some(rows), Some(map), Some(model)) = (
        accounts["accounts"].as_array(),
        namespaces.as_object(),
        native_model(s(job, "model")),
    ) else {
        return result;
    };
    let quotas = adaptive::parse_quotas(accounts, at);
    let mut eligible = Vec::new();
    for (selector, target) in map {
        if !public_selector(selector) {
            continue;
        }
        let Some(target) = target.as_str() else {
            continue;
        };
        let id = if target == "@main" {
            "__main__"
        } else {
            target
        };
        // Pool metadata must explicitly confirm OAuth and spending disabled.
        // Missing, stale, paused or unverified rows never become an account.
        let Some(row) = rows.iter().find(|row| {
            s(row, "id") == id && s(row, "type") == "codex" && row["creditsAfterLimit"] == false
        }) else {
            continue;
        };
        let healthy = s(row, "health").eq_ignore_ascii_case("healthy")
            || s(row, "health").eq_ignore_ascii_case("ok")
            || s(&row["health"], "status").eq_ignore_ascii_case("healthy");
        if !healthy || row["hasCredential"] == false {
            continue;
        }
        if row["quota"]["updatedAt"].as_f64().is_some_and(|checked| {
            let checked = if checked > 1e12 {
                checked / 1000.
            } else {
                checked
            };
            checked > at + 1. || at - checked > 60.
        }) {
            continue;
        }
        if quotas
            .accounts
            .iter()
            .any(|q| q.id == id && q.reset_at > at && q.used_percent < 100. && !q.short_exhausted)
        {
            eligible.push(selector.clone());
        }
    }
    eligible.sort();
    eligible.dedup();
    result["eligible"] = json!(eligible);

    let mut failed = labels(&job["capacity_tried_accounts"]);
    let mut log_labels = Vec::<String>::new();
    if public_selector(s(job, "capacity_account_selector")) {
        failed.push(s(job, "capacity_account_selector").into());
    }
    // The CLI conversation filter is authoritative; do not scan other threads.
    // A failed row must also match this model and the current attempt's lifetime.
    let started = parse_iso(s(job, "started_at"))
        .or_else(|| parse_iso(s(job, "capacity_failed_started_at")))
        .unwrap_or(at - 60.);
    let last = logs["logs"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|row| {
            row["timestamp"]
                .as_f64()
                .is_some_and(|ms| ms / 1000. >= started - 1. && ms / 1000. <= at + 1.)
                && ["model", "requestedModel", "wireModel"]
                    .iter()
                    .any(|field| observed_model(s(row, field)) == model)
                && (row["status"].as_u64().is_some_and(|s| s >= 400)
                    || ["failed", "incomplete"].contains(&s(row, "terminalStatus")))
        })
        .max_by(|a, b| {
            a["timestamp"]
                .as_f64()
                .partial_cmp(&b["timestamp"].as_f64())
                .unwrap()
        });
    if let Some(last) = last {
        if public_selector(s(last, "accountLogLabel")) {
            log_labels.push(s(last, "accountLogLabel").into());
        }
        for attempt in last["attempts"].as_array().into_iter().flatten() {
            if public_selector(s(attempt, "accountLogLabel")) {
                log_labels.push(s(attempt, "accountLogLabel").into());
            }
        }
        let mut observation = json!({});
        for field in ["requestedEffort", "effectiveEffort", "reasoningWireValue"] {
            if ["low", "medium", "high", "xhigh", "max"].contains(&s(last, field)) {
                observation[field] = last[field].clone();
            }
        }
        for field in [
            "callerServiceTier",
            "requestedServiceTier",
            "responseServiceTier",
        ] {
            if [
                "default",
                "standard",
                "fast",
                "priority",
                "ultrafast",
                "auto",
                "flex",
            ]
            .contains(&s(last, field))
            {
                observation[field] = last[field].clone();
            }
        }
        result["observation"] = observation;
    }
    let mut failed_labels = labels(&job["capacity_tried_labels"]);
    failed_labels.extend(log_labels.iter().cloned());
    failed_labels.sort();
    failed_labels.dedup();
    let mut public_labels = json!({});
    let mut current_attributed = false;
    for (selector, target) in map {
        if !public_selector(selector) {
            continue;
        }
        let Some(id) = target.as_str() else {
            continue;
        };
        let id = if id == "@main" { "__main__" } else { id };
        if let Some(label) = accounts["_account_labels"][id]
            .as_str()
            .filter(|label| public_selector(label))
        {
            public_labels[selector] = json!(label);
            if failed_labels.iter().any(|observed| observed == label) {
                failed.push(selector.clone());
            }
            if log_labels.iter().any(|observed| observed == label) {
                current_attributed = true;
            }
        }
    }
    failed.sort();
    failed.dedup();
    // Multiple public aliases of one private account are still a single slot.
    // Discard the private relation before returning this snapshot.
    let failed_targets: Vec<_> = failed
        .iter()
        .filter_map(|label| map.get(label).and_then(Value::as_str))
        .collect();
    let mut seen_targets = std::collections::HashSet::new();
    eligible.retain(|label| {
        map.get(label)
            .and_then(Value::as_str)
            .is_some_and(|id| !failed_targets.contains(&id) && seen_targets.insert(id))
    });
    result["eligible"] = json!(eligible);
    result["verified"] = json!(current_attributed);
    result["public_labels"] = public_labels;
    result["failed_labels"] = json!(failed_labels);
    result["failed"] = json!(failed);
    result
}

fn query(rt: &(impl Runtime + ?Sized), args: &[&str]) -> Option<Value> {
    let env = rt.env();
    let appdata = env.get("APPDATA")?;
    let package = std::path::Path::new(appdata).join("npm/node_modules/@bitkyc08/opencodex");
    let mut argv = vec![
        package
            .join("node_modules/bun/bin/bun.exe")
            .to_string_lossy()
            .into_owned(),
        package.join("bin/ocx.mjs").to_string_lossy().into_owned(),
    ];
    argv.extend(args.iter().map(|s| s.to_string()));
    let reply = rt.call(Call::new(argv, vec![], 15.)).ok()?;
    (reply.code == 0 && reply.stdout.len() <= 2_000_000)
        .then(|| serde_json::from_slice(&reply.stdout).ok())
        .flatten()
}

/// Preserve native client capabilities when the public account selector is
/// hidden from the operator's picker. No global catalog or UI setting changes.
pub fn account_catalog(catalog: &Value, job: &Value) -> Option<Value> {
    let native = native_model(s(job, "model"))?;
    let alias = launch_model(job);
    if alias == s(job, "model") {
        return None;
    }
    let mut rows = catalog["models"]
        .as_array()?
        .iter()
        .filter(|row| s(row, "slug") == native);
    let mut model = rows.next()?.clone();
    if rows.next().is_some()
        || model["context_window"].as_u64().is_none_or(|n| n == 0)
        || !model["supported_reasoning_levels"]
            .as_array()?
            .iter()
            .any(|v| s(v, "effort") == s(job, "effort") || v.as_str() == Some(s(job, "effort")))
    {
        return None;
    }
    model["slug"] = json!(alias);
    model["default_reasoning_level"] = job["effort"].clone();
    model["opencodex_catalog_kind"] = json!("account-selector-v1");
    Some(json!({"models":[model]}))
}

pub fn model_catalog(rt: &(impl Runtime + ?Sized), job: &Value) -> Option<Value> {
    account_catalog(&query(rt, &["inspect", "catalog", "--json"])?, job)
}

pub fn sample(rt: &(impl Runtime + ?Sized), job: &Value, at: f64) -> Value {
    let unknown = || json!({"at":at,"verified":false,"eligible":[],"failed":[]});
    let Some(mut accounts) = query(
        rt,
        &[
            "account",
            "list",
            "openai",
            "--quota",
            "--refresh",
            "--json",
        ],
    ) else {
        return unknown();
    };
    let Some(namespaces) = query(rt, &["config", "get", "codexAccountNamespaces", "--json"]) else {
        return unknown();
    };
    let mut routing_labels = json!({"__main__":"main"});
    if let Some(records) = query(rt, &["config", "get", "codexAccounts", "--json"]) {
        for record in records.as_array().into_iter().flatten() {
            let id = s(record, "id");
            if id.is_empty() {
                continue;
            }
            let label = if public_selector(s(record, "logLabel")) {
                s(record, "logLabel").to_owned()
            } else {
                format!(
                    "p{}",
                    &cc_job_policy::product_retry::digest(id.as_bytes())[..6]
                )
            };
            routing_labels[id] = json!(label);
        }
    }
    accounts["_account_labels"] = routing_labels;
    let session =
        normalize_uuid(s(job, "thread_id")).or_else(|| normalize_uuid(s(job, "resume_session")));
    let logs = if let Some(session) = session {
        query(
            rt,
            &[
                "observe",
                "logs",
                "--conversation",
                &session,
                "--model",
                s(job, "model"),
                "--limit",
                "20",
                "--json",
            ],
        )
        .unwrap_or(Value::Null)
    } else {
        Value::Null
    };
    snapshot(&accounts, &namespaces, &logs, job, at)
}

/// None means attribution or pool health is unverified; it does not authorize
/// an immediate provider switch. Some(None) means all verified alternatives ran out.
pub fn next_selector(job: &Value, pool: &Value, at: f64) -> Option<Option<String>> {
    if pool["verified"] != true
        || (labels(&pool["failed"]).is_empty()
            && !public_selector(s(job, "capacity_account_selector")))
        || !pool["at"]
            .as_f64()
            .is_some_and(|checked| checked <= at && at - checked <= 60.)
    {
        return None;
    }
    let mut used = labels(&job["capacity_tried_accounts"]);
    for entry in job["capacity_history"].as_array().into_iter().flatten() {
        if public_selector(s(entry, "account_selector")) {
            used.push(s(entry, "account_selector").into());
        }
    }
    used.extend(labels(&pool["failed"]));
    if public_selector(s(job, "capacity_account_selector")) {
        used.push(s(job, "capacity_account_selector").into());
    }
    Some(
        labels(&pool["eligible"])
            .into_iter()
            .find(|selector| !used.contains(selector)),
    )
}

pub fn tried_accounts(job: &Value, pool: &Value, next: &str) -> Vec<String> {
    let mut used = labels(&job["capacity_tried_accounts"]);
    used.extend(labels(&pool["failed"]));
    used.push(next.into());
    used.sort();
    used.dedup();
    used
}
