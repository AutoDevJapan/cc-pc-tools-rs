//! The existing, finite authorization for original task 1626. Not general dispatch permission.
use serde_json::{Value, json};
use sha2::{Digest, Sha256};

pub const ID: u64 = 1626;
pub const TREE: &str = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1-wt/todo-1626-1";
pub const REQUEST_HASH: &str = "a600dda0a9dc0f26a3507e081ef5b938cd7de40bad92e500068f3a191b99ecb8";
pub const QUALIFICATION_SESSION: &str = "01a12690-d332-7140-bc8c-47974cdcd2cb";
pub const QUALIFICATION_JOB: &str = "20261011-234558-orch-9a5f7b7c6fb45eb2815aa60ea8a5df95";
pub const LIMIT: u64 = 1792540800; // 2026-10-21 00:00 UTC
pub fn digest(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
pub fn same_path(a: &Value, b: &Value) -> bool {
    a.as_str().zip(b.as_str()).is_some_and(|(a, b)| {
        !a.is_empty()
            && a.replace('\\', "/")
                .eq_ignore_ascii_case(&b.replace('\\', "/"))
    })
}
fn finite(at: u64) -> bool {
    (1791849600..LIMIT).contains(&at)
}
fn expiry(proof: &Value) -> Option<u64> {
    chrono::DateTime::parse_from_rfc3339(proof["expires_at"].as_str()?)
        .ok()?
        .timestamp()
        .try_into()
        .ok()
}
pub fn valid_proof(proof: &Value, at: u64) -> bool {
    finite(at)
        && expiry(proof).is_some_and(|until| at < until && until <= LIMIT)
        && proof["id"] == ID
        && proof["verified"] == true
        && proof["session_receipt_verified"] == true
        && proof["session"] == QUALIFICATION_SESSION
        && proof["job"] == QUALIFICATION_JOB
        && proof["request_sha256"] == REQUEST_HASH
        && same_path(&proof["worktree"], &json!(TREE))
        && proof["snapshot"].is_object()
}
pub fn key(wait: &Value) -> Option<String> {
    let since = wait["since"].as_u64()?;
    let until = wait["not_before"].as_u64()?;
    Some(
        digest(
            format!("product-retry-2709\0{ID}\0{since}\0{until}\0{REQUEST_HASH}\0{TREE}")
                .as_bytes(),
        )[..32]
            .to_owned(),
    )
}
pub fn attempt_key(base:&str,attempt:u64)->Option<String>{
    if attempt>2 || base.len()!=32 || !base.bytes().all(|b|b.is_ascii_digit() || (b'a'..=b'f').contains(&b)){return None;}
    Some(if attempt==0 {base.to_owned()}else{digest(format!("product-retry-attempt\0{base}\0{attempt}").as_bytes())[..32].to_owned()})
}
pub fn base_key(intent:&Value)->&Value{intent.get("base_key").unwrap_or(&intent["key"])}
pub fn snapshot(row: &Value) -> Value {
    json!({"id":row["id"],"project":row["project"],"spec":row["spec"],"status":row["status"],
        "dispatchable":row["dispatchable"],"wait_ready":row["wait_ready"],"wait_on":row["wait_on"],"needs":row["needs"],"model":row["model"],"effort":row["effort"],
        "stopped":row["stopped"],"cancel_requested":row["cancel_requested"],"stop_kind":row["stop_kind"],
        "decision_wait":row["decision_wait"],"owner_postponement_recorded":row["owner_postponement_recorded"],
        "resume_generation":row["resume_generation"],"worker_receipt":row["worker_receipt"],"jobs":row["jobs"],
        "scheduler":{"job":row["scheduler"]["job"],"last_job":row["scheduler"]["last_job"],
            "worktree":row["scheduler"]["worktree"],"branch":row["scheduler"]["branch"],
            "owner_stop_job":row["scheduler"]["owner_stop_job"],"owner_instruction":row["scheduler"]["owner_instruction"],
            "question":row["scheduler"]["question"],"console_request":row["scheduler"]["console_request"]}})
}
pub fn stopped(row: &Value) -> bool {
    crate::cancellation::owner_stopped(row, &Value::Null)
        || row["status"] == "block"
        || row["status"] == "done"
        || row["status"] == "recovery_unverified"
        || !row["decision_wait"].is_null()
        || row["owner_postponement_recorded"] == true
        || row["stop_kind"].as_str().is_some_and(|v| !v.is_empty())
        || !row["scheduler"]["owner_stop_job"].is_null()
}
fn dependencies_clear(db: &Value, row: &Value) -> bool {
    (row["wait_on"].is_null()
        || row["wait_on"].as_array().is_some_and(|on| {
            on.iter()
                .all(|id| id.as_u64().is_some_and(|id| id > 0 && id != ID))
        }))
        && !crate::todo_wait::waiting(db, row)
        && row["wait_on"].as_array().into_iter().flatten().all(|id| {
            let matches: Vec<_> = db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .filter(|item| item["id"] == *id)
                .collect();
            matches.len() == 1 && matches[0]["status"] == "done"
        })
}
pub fn row(db: &Value) -> Option<&Value> {
    let mut rows = db["items"].as_array()?.iter().filter(|row| row["id"] == ID);
    let row = rows.next()?;
    rows.next().is_none().then_some(row)
}
fn generation(value: &Value) -> Option<u64> {
    value
        .as_f64()
        .filter(|v| v.is_finite() && *v >= 0. && *v < 9_007_199_254_740_991. && v.fract() == 0.)
        .map(|v| v as u64)
}
pub fn candidate(db: &Value, at: u64) -> Option<&Value> {
    let row = row(db)?;
    (finite(at)
        && row["status"] == "wait"
        && row["dispatchable"] == false
        && !stopped(row)
        && crate::todo_wait::retry_due(&row["wait_ready"], at)
        && dependencies_clear(db, row)
        && same_path(&row["scheduler"]["worktree"], &json!(TREE))
        && row["jobs"].is_array()
        && generation(&row["resume_generation"]).is_some()
        && generation(&row["resume_generation"])
            == generation(&row["worker_receipt"]["generation"])
        && row["worker_receipt"]["job"].is_string()
        && row["worker_receipt"]["job"] == row["scheduler"]["job"])
        .then_some(row)
}
fn matches_source(job: &Value, intent: &Value, continuing: bool) -> bool {
    let source = &intent["source"];
    job["todo_id"] == ID
        && (job["resume_session"] == source["session"]
            || (continuing
                && (job["resume_session"].is_null() || job["resume_session"].is_string())))
        && (job["related_todo_ids"].is_null()
            || job["related_todo_ids"]
                .as_array()
                .is_some_and(Vec::is_empty))
        && job["parent_job_id"].is_null()
        && job["subwork"].is_null()
        && same_path(&job["cwd"], &source["cwd"])
        && same_path(&job["prompt_source"], &intent["prompt_file"])
        && [
            "effort",
            "sandbox",
            "mcp_profile",
            "scope_source",
            "network",
            "add_dirs",
        ]
        .iter()
        .all(|field| job[*field] == source[*field])
        && job["requested_model"] == source["model"]
}
pub fn marker(intent: &Value) -> Value {
    json!({"version":1,"key":intent["key"],"source_job":intent["source"]["id"],
        "source_session":intent["source"]["session"],"request_sha256":REQUEST_HASH,
        "expires_at":intent["proof"]["expires_at"],"prompt_sha256":intent["prompt_sha256"]})
}
pub fn job_matches(job: &Value, intent: &Value) -> bool {
    job["original_product_retry"] == marker(intent)
        && job["launch_key"] == intent["key"]
        && matches_source(job, intent, false)
}
pub fn continuing(job: &Value, at: u64) -> bool {
    job["first_started_at"]
        .as_str()
        .and_then(|v| chrono::DateTime::parse_from_rfc3339(v).ok())
        .is_some_and(|started| started.timestamp() >= 0 && (started.timestamp() as u64) <= at)
}
/// Called under queue -> ToDo locks; the API's reservation still holds the original wait.
pub fn registration(
    db: &Value,
    job: &Value,
    requested_key: &str,
    prompt_digest: &str,
    at: u64,
) -> Result<Value, String> {
    let row = candidate(db, at).ok_or("product_retry_not_due")?;
    let intent = &row["product_retry_intent"];
    if !valid_proof(&intent["proof"], at)
        || intent["key"] != requested_key
        || key(&row["wait_ready"]).as_deref() != base_key(intent).as_str()
        || attempt_key(base_key(intent).as_str().unwrap_or(""),intent["attempt"].as_u64().unwrap_or(0)).as_deref()!=Some(requested_key)
        || intent["snapshot"] != snapshot(row)
        || intent["prompt_sha256"] != prompt_digest
        || !intent["bound_job"].is_null()
        || !matches_source(job, intent, false)
    {
        return Err("product_retry_intent_changed".into());
    }
    Ok(marker(intent))
}
/// The normal queue consults this again immediately before starting a supervisor's model process.
pub fn admission_reason(db: &Value, job: &Value, at: u64) -> Option<Value> {
    job.get("original_product_retry")?;
    let fail = |code: &str, terminal: bool| {
        Some(
            json!({"code":code,"message":"元の作業の再開条件を確認しています","terminal":terminal}),
        )
    };
    let Some(row) = row(db) else {
        return fail("product_retry_missing", true);
    };
    let intent = &row["product_retry_intent"];
    if stopped(row) || crate::cancellation::owner_stopped(&Value::Null, job) {
        return fail("product_retry_owner_stopped", true);
    }
    if !valid_proof(&intent["proof"], at) {
        return fail("product_retry_expired", true);
    }
    // First entry is pinned to the actual original session. Once this exact
    // bound job has started, ordinary context rotation/429 retries own its
    // continuation sessions; the immutable workspace and permissions stay pinned.
    let continuing = continuing(job, at);
    if !job["original_product_retry"].is_object()
        || job["original_product_retry"] != marker(intent)
        || job["launch_key"] != intent["key"]
        || !matches_source(job, intent, continuing)
        || row["dispatchable"] != false
    {
        return fail("product_retry_scope_changed", true);
    }
    if intent["bound_job"].is_null() {
        if intent["snapshot"] != snapshot(row) {
            return fail("product_retry_original_wait_changed", true);
        }
        return fail("product_retry_unbound", false);
    }
    if intent["bound_job"] != job["id"]
        || row["worker_receipt"]["job"] != job["id"]
        || row["scheduler"]["job"] != job["id"]
        || intent["bound_generation"] != row["resume_generation"]
        || row["spec"] != intent["snapshot"]["spec"]
        || row["project"] != intent["snapshot"]["project"]
        || row["needs"] != intent["snapshot"]["needs"]
        || row["model"] != intent["snapshot"]["model"]
        || row["effort"] != intent["snapshot"]["effort"]
    {
        return fail("product_retry_binding_changed", true);
    }
    if !row["wait_ready"].is_null() || !dependencies_clear(db, row) {
        return fail("product_retry_new_wait", false);
    }
    if !matches!(row["status"].as_str(), Some("doing" | "wait" | "todo")) {
        return fail("product_retry_binding_changed", true);
    }
    None
}

/// The queue caller owns queue -> job -> ToDo locks. Never creates a worker.
pub fn mark_continuation_running(db: &mut Value, job: &Value, at: u64) -> Result<bool, String> {
    if job.get("original_product_retry").is_none() {
        return Ok(false);
    }
    if admission_reason(db, job, at).is_some() {
        return Err("product_retry_not_admitted".into());
    }
    let row = db["items"]
        .as_array_mut()
        .and_then(|items| items.iter_mut().find(|row| row["id"] == ID))
        .ok_or("product_retry_missing")?;
    if row["status"] == "doing" {
        return Ok(false);
    }
    row["status"] = json!("doing");
    row.as_object_mut().unwrap().remove("wait_on");
    Ok(true)
}
