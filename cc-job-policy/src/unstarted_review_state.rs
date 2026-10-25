//! Decode the existing scheduler row and construct one compare-and-swap update.
//! No filesystem, HTTP, process or shared-store access occurs in this module.
use crate::unstarted_review::{self as rule, OAuthInventory, Origin, Pending, Registration};
use serde_json::{Value, json};

pub const SOURCE_HEAD: &str = "173c3a4aa925bb03c8e1c109ea598e05f7d0dc9a";
pub const SPEC_SHA: &str = "0699a3aa47d93e2cc1014964f4f913bbb6f1d986bb9fb3fa52159453bebd441e";
pub const WRITERS: [&str; 2] = ["20261024-201241-todo-3387", "20261024-220456-todo-3648"];
pub const PRODUCER: &str =
    "D:/CC/Work/unattended-operations-20261013-wt/todo-3255-godd/record-recovery-3255.exe";
pub const REVIEW_TREE: &str = "cad96a1891817f8c0a0b08d82c89dba2567ee2d4";
pub const CHECKOUT_HEAD: &str = "1497a9326e4c579699f9eb55e5a74b86f530c993";

pub fn spec_path_matches(value: &str) -> bool {
    let normalized = value.replace('\\', "/").to_ascii_lowercase();
    let Some(name) = normalized
        .strip_prefix("d:/cc/tools/todo/specs/3649-")
        .and_then(|p| p.strip_suffix(".md"))
    else {
        return false;
    };
    name.len() == 32 && name.bytes().all(|b| b.is_ascii_hexdigit())
}

pub fn observed_authors(jobs: &[Value]) -> Option<Vec<String>> {
    let mut authors = Vec::new();
    for (id, todo) in WRITERS.iter().zip([3387, 3648]) {
        let rows: Vec<_> = jobs.iter().filter(|j| j["id"] == *id).collect();
        if rows.len() != 1
            || rows[0]["todo_id"] != todo
            || rows[0]["status"] != "succeeded"
            || rows[0]["execution_observed"]["source"] != "native_turn_context"
            || crate::quota_recovery::provider(text(&rows[0]["execution_observed"]["model"]))
                .is_none()
        {
            return None;
        }
        authors.extend(crate::author_models(rows[0]));
    }
    authors.sort();
    authors.dedup();
    Some(authors)
}

fn text(value: &Value) -> &str {
    value.as_str().unwrap_or("")
}
fn path(value: &Value) -> String {
    // Saved spellings only; the runtime adapter checks physical source identity.
    text(value)
        .replace('\\', "/")
        .trim_end_matches('/')
        .to_ascii_lowercase()
}
fn hash(value: &Value) -> String {
    crate::product_retry::digest(&serde_json::to_vec(value).unwrap())
}
fn inactive(value: &Value) -> bool {
    value.is_null() || value == false
}
fn empty_array(value: &Value) -> bool {
    value.is_null() || value.as_array().is_some_and(Vec::is_empty)
}
fn origin(row: &Value, field: &str) -> Origin {
    match row.get(field) {
        None => Origin::Absent,
        Some(Value::Null) => Origin::Null,
        Some(v) if v == "policy" => Origin::Policy,
        Some(v) if v == "owner_request" => Origin::OwnerRequest,
        _ => Origin::Unknown,
    }
}
fn sha_bytes(value: &Value) -> [u8; 32] {
    let digest = hash(value);
    let mut bytes = [0; 32];
    for (index, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&digest[index * 2..index * 2 + 2], 16).unwrap();
    }
    bytes
}

/// The transport adapter must have called the fixed normal producer itself.
/// An arbitrary JSON request or a saved investigation file is not that source.
pub fn registration(normal: &Value) -> Option<Registration<'_>> {
    let p = &normal["producer"];
    if normal["ok"] != true
        || normal["kind"] != "original-review-registration-3649-v1"
        || normal["todo_id"] != 3649
        || normal["state"] != "observed"
        || p["state"] != "recorded"
        || p["compiled_source_modified"] != false
        || text(&p["compiled_revision"]).len() != 40
        || !text(&p["compiled_revision"])
            .bytes()
            .all(|b| b.is_ascii_hexdigit())
        || normal["owner_origin_proved"] != false
        || normal["fallback_authorized"] != false
        || normal["live_state_changed"] != false
        || normal["data_access_granted"] != false
    {
        return None;
    }
    let r = &normal["registration"];
    Some(Registration {
        source_job: text(&r["source_job"]),
        thread: text(&r["thread"]),
        turn: text(&r["turn"]),
        item: text(&r["item"]),
        command_sha256: text(&r["command_sha256"]),
        output_sha256: text(&r["output_sha256"]),
        normal_api_bound: true,
        completed_successfully: r["completed_successfully"] == true,
        added_3649: r["added_3649"] == true,
        omitted_origin_flags: r["omitted_origin_flags"] == true,
        // The parent supplied this finite continuation. It is NOT a claim
        // about the owner's historical model choices or a new owner message.
        continuation_bound: r["continuation_thread"] == "7c5faa87-6f1e-49c7-92d6-08d6448bfbe6"
            && r["continuation_message"] == "360df070-ccac-48a2-bc86-a6a18851164a"
            && r["continuation_sha256"]
                == "6de3b1080e0d3b780517fffa5db3c5dc928727071656316534b75a4a2ba67e36"
            && r["continuation_source"] == "parent_bot",
    })
}

fn pending<'a>(row: &'a Value, spec_sha: &'a str, source_verified: bool) -> Pending<'a> {
    let scheduler = &row["scheduler"];
    let stops_absent = !crate::cancellation::owner_stopped(row, &Value::Null)
        && ["owner_stopped", "stopped", "owner_postponement_recorded"]
            .iter()
            .all(|k| inactive(&row[*k]))
        && [
            "stop_kind",
            "owner_question_wait",
            "decision_wait",
            "wait_ready",
        ]
        .iter()
        .all(|k| row[*k].is_null())
        && (scheduler["owner_instruction"].is_null() || scheduler["owner_instruction"] == "")
        && [
            "job",
            "last_job",
            "owner_stop_job",
            "pending_cancel",
            "question",
            "console_request",
        ]
        .iter()
        .all(|k| scheduler[*k].is_null());
    Pending {
        todo_id: row["id"].as_u64().unwrap_or(0),
        project: text(&row["project"]),
        todo: row["status"] == "todo",
        dispatchable: row["dispatchable"] == true,
        jobs_empty: row["jobs"].as_array().is_some_and(Vec::is_empty),
        live_worker_absent: true, // The runtime adapter separately checks jobs/processes.
        stops_absent,
        dependencies_ready: empty_array(&row["wait_on"]),
        other_waits_absent: scheduler["not_before"].is_number(),
        reservation_unchanged: source_verified
            && row["worker_receipt"].is_null()
            && row.get("resume_generation").is_none_or(|g| g.is_u64())
            && scheduler["phase"] == "preparing"
            && text(&scheduler["branch"]) == "scheduler/todo-3649-work"
            && !text(&scheduler["worktree"]).is_empty()
            && !text(&scheduler["key"]).is_empty()
            && scheduler["selection"].is_null()
            && scheduler["selection_input"].is_null()
            && scheduler["unstarted_review"].is_null(),
        source_head: SOURCE_HEAD,
        spec_sha256: spec_sha,
        requested_model: text(&row["model"]),
        requested_effort: text(&row["effort"]),
        model_origin: origin(row, "model_origin"),
        effort_origin: origin(row, "effort_origin"),
        quota_exhausted_recorded: scheduler["resource_wait"]["reason"] == "quota_exhausted",
        quota_until: scheduler["resource_wait"]["until"]
            .as_f64()
            .unwrap_or(f64::NAN),
        not_before: scheduler["not_before"].as_f64().unwrap_or(f64::NAN),
        state_sha256: sha_bytes(row),
    }
}

/// The caller supplies only already refreshed, safe provider aggregates. Never
/// raw account replies, identifiers, credentials or paid-overage configuration.
fn inventory(health: &Value, now: f64) -> OAuthInventory {
    OAuthInventory {
        observed_at: health["at"].as_f64().unwrap_or(f64::NAN),
        anthropic_verified_exhausted: crate::quota_recovery::healthy_transition(
            health,
            "claude-opus-5-5",
            "gpt-6-luna",
            now,
        ),
        openai_verified_available: health["openai"]["available"] == true,
        subscription_only_no_additional_spend: crate::quota_recovery::safe_openai_selector(
            health, None, now,
        )
        .is_some(),
    }
}

/// Bind the scope that normal preparation and the final launcher must retain.
/// Field presence is preserved: absent origins are not encoded as null/policy.
pub fn request_binding(row: &Value) -> String {
    let mut fields = serde_json::Map::new();
    for name in [
        "id",
        "project",
        "title",
        "spec",
        "model",
        "effort",
        "model_origin",
        "effort_origin",
        "needs",
        "role",
        "task_type",
        "sandbox",
        "network",
        "mcp_profile",
        "add_dirs",
        "review_source_jobs",
        "subwork",
        "dependency_generation",
        "resume_generation",
    ] {
        if let Some(value) = row.get(name) {
            fields.insert(name.into(), value.clone());
        }
    }
    let mut reserved = serde_json::Map::new();
    for name in [
        "worktree",
        "branch",
        "key",
        "attempt",
        "owner_instruction",
        "console_request",
    ] {
        if let Some(value) = row["scheduler"].get(name) {
            reserved.insert(name.into(), value.clone());
        }
    }
    fields.insert("reservation".into(), Value::Object(reserved));
    hash(&Value::Object(fields))
}

/// Prepare a CAS proposal only. The row, original request and quota hold are not
/// changed until the caller applies exactly this original row under todo.lock.
pub struct Proposed {
    original: Value,
    capsule: Value,
}
#[derive(Clone, Copy)]
pub struct Checked {
    pub source_verified: bool,
    pub live_worker_absent: bool,
}

impl Proposed {
    pub fn state_sha256(&self) -> &str {
        text(&self.capsule["expected_state_sha256"])
    }
}

pub fn proposal(
    row: &Value,
    normal: &Value,
    health: &Value,
    authors: &[String],
    spec_sha: &str,
    checked: Checked,
    now: f64,
) -> Result<Proposed, rule::Reason> {
    let registration = registration(normal).ok_or(rule::Reason::RegistrationUnverified)?;
    let author_refs: Vec<&str> = authors.iter().map(String::as_str).collect();
    let mut pending = pending(row, spec_sha, checked.source_verified);
    pending.live_worker_absent = checked.live_worker_absent;
    let plan = rule::plan(
        pending,
        registration,
        inventory(health, now),
        now,
        Some(&author_refs),
    )?;
    let capsule = json!({"version":1,"todo_id":3649,"created_at":now,
        "expected_state_sha256":hash(row),"request_sha256":request_binding(row),
        "launch_key":row["scheduler"]["key"],"source_head":SOURCE_HEAD,"spec_sha256":spec_sha,
        "review_tree":REVIEW_TREE,"checkout_head":CHECKOUT_HEAD,
        "generation_present":row.get("resume_generation").is_some(),"generation_before":row["resume_generation"].as_u64().unwrap_or(0),
        "selected_selector":crate::quota_recovery::safe_openai_selector(health,None,now),
        "model":plan.selected_model,"effort":plan.selected_effort,
        "prior_not_before":row["scheduler"]["not_before"],
        "prior_resource_wait":row["scheduler"]["resource_wait"],
        "registration":normal["registration"],"producer":normal["producer"],
        "author_jobs":WRITERS,"author_models":authors,
        "health":{"at":health["at"],"openai":health["openai"],"anthropic":health["anthropic"]}});
    Ok(Proposed {
        original: row.clone(),
        capsule,
    })
}

/// Apply only a proposal just recomputed from normal inputs. This is not a
/// general JSON patch API. Duplicate application and stale snapshots fail.
pub fn apply(row: &mut Value, proposed: &Proposed) -> bool {
    let proposal = &proposed.capsule;
    if *row != proposed.original
        || proposal["expected_state_sha256"] != hash(row)
        || proposal["request_sha256"] != request_binding(row)
        || !row["scheduler"]["unstarted_review"].is_null()
        || proposal["version"] != 1
        || proposal["todo_id"] != 3649
    {
        return false;
    }
    row["scheduler"]["unstarted_review"] = proposal.clone();
    let fields = row["scheduler"].as_object_mut().unwrap();
    fields.remove("not_before");
    fields.remove("resource_wait");
    true
}

/// Revalidate the selected route before normal preparation. It does not release
/// a newly recorded hold, accept an owner override or create another launch key.
pub fn current(
    row: &Value,
    normal: &Value,
    health: &Value,
    authors: &[String],
    spec_sha: &str,
    now: f64,
) -> bool {
    current_for(row, normal, health, authors, spec_sha, now, None)
}

fn current_for(
    row: &Value,
    normal: &Value,
    health: &Value,
    authors: &[String],
    spec_sha: &str,
    now: f64,
    job: Option<&Value>,
) -> bool {
    if !["preparing", "prepared", "running"].contains(&text(&row["scheduler"]["phase"]))
        || (row["scheduler"]["phase"] == "running" && job.is_none())
    {
        return false;
    }
    if row["scheduler"]["phase"] == "running" {
        let Some(job) = job else {
            return false;
        };
        if row["status"] != "doing"
            || row["scheduler"]["job"] != job["id"]
            || !row["jobs"]
                .as_array()
                .is_some_and(|ids| ids.as_slice() == [job["id"].clone()])
        {
            return false;
        }
    }
    let proof = &row["scheduler"]["unstarted_review"];
    let Some(registration) = registration(normal) else {
        return false;
    };
    let mut original = row.clone();
    if let Some(job) = job
        && row["status"] == "doing"
        && row["scheduler"]["job"] == job["id"]
        && row["jobs"]
            .as_array()
            .is_some_and(|ids| ids.as_slice() == [job["id"].clone()])
    {
        if !row["worker_receipt"].is_null() {
            let before = proof["generation_before"].as_u64();
            if row["worker_receipt"]["job"] != job["id"]
                || row["worker_receipt"]["generation"] != row["resume_generation"]
                || before.and_then(|g| g.checked_add(1)) != row["resume_generation"].as_u64()
                || job["todo_resume_generation"] != row["resume_generation"]
            {
                return false;
            }
            original.as_object_mut().unwrap().remove("worker_receipt");
            if proof["generation_present"] == true {
                original["resume_generation"] = proof["generation_before"].clone();
            } else {
                original
                    .as_object_mut()
                    .unwrap()
                    .remove("resume_generation");
            }
            if row["scheduler"]["key"].is_null() {
                original["scheduler"]["key"] = proof["launch_key"].clone();
            }
        }
        original["status"] = json!("todo");
        original["jobs"] = json!([]);
        original["scheduler"].as_object_mut().unwrap().remove("job");
    }
    let Some(fields) = original["scheduler"].as_object_mut() else {
        return false;
    };
    if fields
        .get("phase")
        .is_some_and(|p| p == "prepared" || (p == "running" && job.is_some()))
    {
        fields.insert("phase".into(), json!("preparing"));
        fields.remove("selection");
        fields.remove("selection_input");
    }
    fields.remove("unstarted_review");
    fields.insert("not_before".into(), proof["prior_not_before"].clone());
    fields.insert("resource_wait".into(), proof["prior_resource_wait"].clone());
    let mut pending = pending(&original, spec_sha, true);
    pending.reservation_unchanged = pending.reservation_unchanged
        && proof["request_sha256"] == request_binding(&original)
        && proof["launch_key"] == original["scheduler"]["key"]
        && proof["source_head"] == SOURCE_HEAD
        && proof["review_tree"] == REVIEW_TREE
        && proof["checkout_head"] == CHECKOUT_HEAD
        && proof["spec_sha256"] == spec_sha;
    let refs: Vec<&str> = authors.iter().map(String::as_str).collect();
    proof["version"] == 1
        && proof["model"] == "gpt-6-luna"
        && proof["effort"] == "max"
        && proof["registration"] == normal["registration"]
        && crate::quota_recovery::safe_openai_selector(
            health,
            Some(
                job.map(|j| text(&j["capacity_account_selector"]))
                    .unwrap_or_else(|| text(&proof["selected_selector"])),
            ),
            now,
        )
        .is_some()
        && row["scheduler"]["not_before"].is_null()
        && row["scheduler"]["resource_wait"].is_null()
        && rule::plan(
            pending,
            registration,
            inventory(health, now),
            now,
            Some(&refs),
        )
        .is_ok()
}

/// The launcher reloads both the saved row and normal evidence before using this
/// route. A caller flag cannot enable it. Model routing still checks all authors.
pub fn admission(
    row: &Value,
    job: &Value,
    normal: &Value,
    health: &Value,
    authors: &[String],
    spec_sha: &str,
    now: f64,
) -> bool {
    let selection = &row["scheduler"]["selection"];
    if row["scheduler"]["unstarted_review"] != job["unstarted_review"]
        || selection["unstarted_review"] != job["unstarted_review"]
        || job["todo_id"] != 3649
        || job["launch_key"] != row["scheduler"]["unstarted_review"]["launch_key"]
        || path(&job["cwd"]) != path(&row["scheduler"]["worktree"])
        || job["model"] != "gpt-6-luna"
        || job["effort"] != "max"
        || job["model_origin"] != "policy"
        || job["effort_origin"] != "policy"
        || job["sandbox"] != "read-only"
        || job["task_type"] != "review"
        || job["review_source_jobs"] != json!(WRITERS)
    {
        return false;
    }
    for field in ["sandbox", "network", "mcp_profile"] {
        if job[field] != selection[field] {
            return false;
        }
    }
    let dirs = |v: &Value| -> Option<Vec<String>> {
        v.as_array()?
            .iter()
            .map(|p| {
                p.as_str()?;
                Some(path(p))
            })
            .collect()
    };
    if dirs(&job["add_dirs"]).is_none() || dirs(&job["add_dirs"]) != dirs(&selection["add_dirs"]) {
        return false;
    }
    current_for(row, normal, health, authors, spec_sha, now, Some(job))
}

#[cfg(test)]
mod tests {
    use super::*;
    fn checked(source_verified: bool, live_worker_absent: bool) -> Checked {
        Checked {
            source_verified,
            live_worker_absent,
        }
    }

    fn fixture() -> (Value, Value, Value, Vec<String>) {
        let row = json!({"id":3649,"project":"localai-web-rs","status":"todo","dispatchable":true,
            "title":"same original review","spec":"synthetic.md","model":"claude-opus-5-5","effort":"max",
            "jobs":[],"note":"retained note","needs":[],"scheduler":{"key":"same-reservation","phase":"preparing",
                "worktree":"D:/CC/example/todo-3649-work","branch":"scheduler/todo-3649-work","attempt":1,
                "not_before":1793030399.891,"resource_wait":{"reason":"quota_exhausted","until":1793030399.891}}});
        let normal = json!({"ok":true,"kind":"original-review-registration-3649-v1","todo_id":3649,"state":"observed",
            "owner_origin_proved":false,"fallback_authorized":false,"live_state_changed":false,"data_access_granted":false,
            "producer":{"state":"recorded","compiled_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","compiled_source_modified":false},
            "registration":{"source_job":"20261024-201241-todo-3387","thread":"01a169de-626d-7270-9de7-362e2cc8af25",
                "turn":"01a16d9d-54fe-77c0-9ce3-1a46594978fd","item":"exec-27beecc5-8f7a-47c3-927f-9f0a7208a390",
                "command_sha256":"31ad65c65a2f203326add7cd2b1feb438034671fa12255603abdd40312976deb",
                "output_sha256":"9eec3fe88e851eaaf196a27033696cee8a5e34bbae59d222c7fa3778c0a0d992",
                "completed_successfully":true,"added_3649":true,"omitted_origin_flags":true,
                "continuation_thread":"7c5faa87-6f1e-49c7-92d6-08d6448bfbe6","continuation_message":"360df070-ccac-48a2-bc86-a6a18851164a",
                "continuation_sha256":"6de3b1080e0d3b780517fffa5db3c5dc928727071656316534b75a4a2ba67e36","continuation_source":"parent_bot"}});
        let health = json!({"at":1000.,"anthropic":{"at":1000.,"verified":true,"exhausted":true,"available":false},
            "openai":{"at":1000.,"verified":true,"exhausted":false,"available":true,"safe_routes":[{"selector":"pabcdef","at":1000.,"verified":true}]}});
        (row, normal, health, vec!["gpt-6.1-sol".into()])
    }

    #[test]
    fn state_cas_retains_original_request_reservation_and_quota_history() {
        let (original, normal, health, authors) = fixture();
        let proposed = proposal(
            &original,
            &normal,
            &health,
            &authors,
            SPEC_SHA,
            checked(true, true),
            1000.,
        )
        .unwrap();
        let mut row = original.clone();
        assert!(apply(&mut row, &proposed));
        assert!(!apply(&mut row, &proposed));
        assert_eq!(row["model"], "claude-opus-5-5");
        assert!(row.get("model_origin").is_none());
        assert!(row.get("effort_origin").is_none());
        assert_eq!(
            row["scheduler"]["unstarted_review"]["prior_not_before"],
            original["scheduler"]["not_before"]
        );
        assert_eq!(
            row["scheduler"]["unstarted_review"]["prior_resource_wait"],
            original["scheduler"]["resource_wait"]
        );
        assert!(current(&row, &normal, &health, &authors, SPEC_SHA, 1000.));
        let mut restored = row.clone();
        restored["scheduler"]
            .as_object_mut()
            .unwrap()
            .remove("unstarted_review");
        for field in ["not_before", "resource_wait"] {
            restored["scheduler"][field] = original["scheduler"][field].clone();
        }
        assert_eq!(
            restored, original,
            "the transaction changed another original field"
        );
        let mut changed = original.clone();
        changed["note"] = json!("new concurrent owner note");
        assert!(!apply(&mut changed, &proposed));
        assert!(changed["scheduler"]["not_before"].is_number());
    }

    #[test]
    fn state_decoder_does_not_convert_null_origins_or_unknown_containers() {
        let (original, normal, health, authors) = fixture();
        for (field, value) in [
            ("model_origin", Value::Null),
            ("effort_origin", json!("policy")),
            ("wait_on", json!([2174])),
            ("jobs", Value::Null),
            ("wait_ready", json!({})),
            ("stopped", json!(true)),
        ] {
            let mut row = original.clone();
            row[field] = value;
            assert!(
                proposal(
                    &row,
                    &normal,
                    &health,
                    &authors,
                    SPEC_SHA,
                    checked(true, true),
                    1000.
                )
                .is_err(),
                "{field}"
            );
        }
        for field in ["owner_instruction", "pending_cancel", "question"] {
            let mut row = original.clone();
            row["scheduler"][field] = json!("new request");
            assert!(
                proposal(
                    &row,
                    &normal,
                    &health,
                    &authors,
                    SPEC_SHA,
                    checked(true, true),
                    1000.
                )
                .is_err(),
                "{field}"
            );
        }
        assert!(
            proposal(
                &original,
                &normal,
                &health,
                &authors,
                SPEC_SHA,
                checked(false, true),
                1000.
            )
            .is_err()
        );
        assert!(
            proposal(
                &original,
                &normal,
                &health,
                &authors,
                SPEC_SHA,
                checked(true, false),
                1000.
            )
            .is_err()
        );
        assert!(
            proposal(
                &original,
                &normal,
                &health,
                &authors,
                "changed",
                checked(true, true),
                1000.
            )
            .is_err()
        );
    }

    #[test]
    fn state_normal_registration_and_fresh_no_overage_health_are_required() {
        let (row, normal, health, authors) = fixture();
        for field in [
            "command_sha256",
            "output_sha256",
            "item",
            "source_job",
            "continuation_message",
            "continuation_sha256",
            "continuation_source",
        ] {
            let mut changed = normal.clone();
            changed["registration"][field] = json!("other");
            assert!(
                proposal(
                    &row,
                    &changed,
                    &health,
                    &authors,
                    SPEC_SHA,
                    checked(true, true),
                    1000.
                )
                .is_err(),
                "{field}"
            );
        }
        let mut absent = normal.clone();
        absent["state"] = json!("unavailable");
        assert!(
            proposal(
                &row,
                &absent,
                &health,
                &authors,
                SPEC_SHA,
                checked(true, true),
                1000.
            )
            .is_err()
        );
        let mut stale = health.clone();
        stale["openai"]["at"] = json!(939.);
        assert!(
            proposal(
                &row,
                &normal,
                &stale,
                &authors,
                SPEC_SHA,
                checked(true, true),
                1000.
            )
            .is_err()
        );
        let mut unavailable = health.clone();
        unavailable["anthropic"]["verified"] = json!(false);
        assert!(
            proposal(
                &row,
                &normal,
                &unavailable,
                &authors,
                SPEC_SHA,
                checked(true, true),
                1000.
            )
            .is_err()
        );
        assert!(
            proposal(
                &row,
                &normal,
                &health,
                &["openai/gpt-6-luna".into()],
                SPEC_SHA,
                checked(true, true),
                1000.
            )
            .is_err()
        );
    }

    #[test]
    fn admission_preserves_the_verified_selection_across_registration_and_link() {
        for before in [None, Some(4u64)] {
            let (mut row, normal, health, authors) = fixture();
            if let Some(generation) = before {
                row["resume_generation"] = json!(generation);
            }
            let proposed = proposal(
                &row,
                &normal,
                &health,
                &authors,
                SPEC_SHA,
                checked(true, true),
                1000.,
            )
            .unwrap();
            assert!(apply(&mut row, &proposed));
            let proof = row["scheduler"]["unstarted_review"].clone();
            row["scheduler"]["phase"] = json!("prepared");
            row["scheduler"]["selection"] = json!({"unstarted_review":proof,"model":"gpt-6-luna","effort":"max",
            "sandbox":"read-only","network":false,"mcp_profile":"minimal","add_dirs":["D:/CC"]});
            let job = json!({"id":"new-review","todo_id":3649,"launch_key":"same-reservation","cwd":"D:/CC/example/todo-3649-work",
            "model":"gpt-6-luna","effort":"max","model_origin":"policy","effort_origin":"policy","task_type":"review","sandbox":"read-only","capacity_account_selector":"pabcdef",
            "network":false,"mcp_profile":"minimal","add_dirs":["D:/CC"],"unstarted_review":proof,"review_source_jobs":WRITERS});
            assert!(admission(
                &row, &job, &normal, &health, &authors, SPEC_SHA, 1000.
            ));
            row["status"] = json!("doing");
            row["jobs"] = json!(["new-review"]);
            row["scheduler"]["job"] = json!("new-review");
            row["scheduler"]["phase"] = json!("running");
            assert!(admission(
                &row, &job, &normal, &health, &authors, SPEC_SHA, 1000.
            ));
            for field in ["unlinked", "wrong_job", "missing_jobs", "wrong_status"] {
                let mut changed = row.clone();
                match field {
                    "unlinked" => changed["scheduler"]["job"] = Value::Null,
                    "wrong_job" => changed["scheduler"]["job"] = json!("other"),
                    "missing_jobs" => changed["jobs"] = json!([]),
                    _ => changed["status"] = json!("todo"),
                }
                assert!(
                    !admission(&changed, &job, &normal, &health, &authors, SPEC_SHA, 1000.),
                    "{field}"
                );
            }
            let mut assigned = row.clone();
            let mut assigned_job = job.clone();
            let next = before.unwrap_or(0) + 1;
            assigned["resume_generation"] = json!(next);
            assigned["worker_receipt"] = json!({"job":"new-review","generation":next});
            assigned["scheduler"]["key"] = Value::Null;
            assigned_job["todo_resume_generation"] = json!(next);
            assert!(admission(
                &assigned,
                &assigned_job,
                &normal,
                &health,
                &authors,
                SPEC_SHA,
                1000.
            ));
            assigned["worker_receipt"]["job"] = json!("other");
            assert!(!admission(
                &assigned,
                &assigned_job,
                &normal,
                &health,
                &authors,
                SPEC_SHA,
                1000.
            ));
            for (field, value) in [
                ("model", json!("gpt-6.1-sol")),
                ("effort_origin", json!("owner_request")),
                ("capacity_account_selector", json!("p000000")),
                ("sandbox", json!("danger-full-access")),
                ("network", json!(true)),
                ("launch_key", json!("other")),
                ("review_source_jobs", json!([])),
                ("add_dirs", json!([])),
            ] {
                let mut changed = job.clone();
                changed[field] = value;
                assert!(
                    !admission(&row, &changed, &normal, &health, &authors, SPEC_SHA, 1000.),
                    "{field}"
                );
            }
            row["scheduler"]["not_before"] = json!(2000.);
            assert!(!admission(
                &row, &job, &normal, &health, &authors, SPEC_SHA, 1000.
            ));
        }
    }

    #[test]
    fn author_requests_alone_never_establish_the_observed_model() {
        let mut jobs = vec![
            json!({"id":WRITERS[0],"todo_id":3387,"status":"succeeded","model":"gpt-6.1-sol"}),
            json!({"id":WRITERS[1],"todo_id":3648,"status":"succeeded","model":"gpt-6.1-sol"}),
        ];
        assert!(observed_authors(&jobs).is_none());
        for job in &mut jobs {
            job["execution_observed"] =
                json!({"source":"native_turn_context","model":"gpt-6.1-sol"});
        }
        assert_eq!(observed_authors(&jobs), Some(vec!["gpt-6.1-sol".into()]));
        jobs[1]["execution_observed"]["source"] = json!("requested_model");
        assert!(observed_authors(&jobs).is_none());
    }
}
