//! Counts jobs, task records and verified processing separately.
//! Inputs must already be privacy-restricted projections; no source prose is read here.
use chrono::DateTime;
use serde_json::{json, Map, Value};
use std::collections::{BTreeMap, BTreeSet};

const STATES: [&str; 10] = [
    "processing",
    "execution_unverified",
    "job_queued",
    "resource_wait",
    "dependency_wait",
    "decision_wait",
    "recovery_unverified",
    "blocked_unknown",
    "not_started",
    "done",
];

fn at(value: &Value) -> Option<u64> {
    DateTime::parse_from_rfc3339(value.as_str()?)
        .ok()?
        .timestamp_millis()
        .try_into()
        .ok()
}

fn recent(time: Option<u64>, now: u64) -> bool {
    time.is_some_and(|time| time <= now && now - time <= 30_000)
}

fn processing(job: &Value, now: u64) -> bool {
    job["recorded_status"] == "running"
        && job["execution"] == "running"
        && job["worker_presence"] == "verified_alive"
        && job["source"]["status"] == "ok"
        && job["source"]["complete"] == true
        && job["wait_reasons"].as_array().is_some_and(Vec::is_empty)
        && job["last_activity"]["state"] == "fresh"
        && recent(at(&job["last_activity"]["at"]), now)
        && matches!(
            job["phase"].as_str(),
            Some("thinking" | "tool" | "editing" | "research" | "delegating")
        )
}

fn evidence(active: Option<&Value>, now: u64, stopped: &BTreeSet<u64>) -> (Value, BTreeSet<u64>) {
    let Some(input) = active else {
        return (
            json!({"state":"unavailable","verifiedCount":null,"total":null,"observedAt":null}),
            BTreeSet::new(),
        );
    };
    let observed = at(&input["heartbeat_at"]);
    let captured = at(&input["captured_at"]);
    let valid = input["schema_version"] == 1
        && input["jobs"]
            .as_array()
            .is_some_and(|jobs| jobs.len() <= 10_000)
        && matches!(input["status"].as_str(), Some("ok" | "degraded"))
        && captured
            .zip(observed)
            .is_some_and(|(start, end)| start <= end);
    if !valid || !recent(observed, now) {
        return (
            json!({"state":if valid && observed.is_some_and(|at|at<=now) {"stale"} else {"unavailable"},
            "verifiedCount":null,"total":null,"observedAt":observed}),
            BTreeSet::new(),
        );
    }
    let mut seen = BTreeSet::new();
    let mut ids = BTreeSet::new();
    let mut count = 0usize;
    let mut invalid = false;
    for job in input["jobs"].as_array().unwrap() {
        // Opaque collector keys only. Never return source paths, arbitrary labels or prose.
        let Some(key) = job["job_key"]
            .as_str()
            .and_then(|key| key.strip_prefix("job-"))
            .filter(|key| key.len() == 64 && key.bytes().all(|b| b.is_ascii_hexdigit()))
        else {
            invalid = true;
            continue;
        };
        if !processing(job, now)
            || job["todo_ids"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(Value::as_u64)
                .any(|id| stopped.contains(&id))
            || !seen.insert(key)
        {
            continue;
        }
        count += 1;
        ids.extend(
            job["todo_ids"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(Value::as_u64)
                .filter(|id| *id > 0),
        );
    }
    let complete = !invalid
        && input["status"] == "ok"
        && input["truncated"] == false
        && input["sources"]["jobs"]["status"] == "ok"
        && input["sources"]["jobs"]["complete"] == true;
    (
        json!({"state":if complete {"complete"} else {"partial"},"verifiedCount":count,
        "total":if complete {Some(count)} else {None},"observedAt":observed}),
        ids,
    )
}

fn records(board: &Value) -> impl Iterator<Item = &Value> {
    board["departments"]
        .as_array()
        .into_iter()
        .flatten()
        .flat_map(|department| department["products"].as_array().into_iter().flatten())
        .chain(board["unassignedProducts"].as_array().into_iter().flatten())
        .flat_map(|product| product["workers"].as_array().into_iter().flatten())
}

fn complete_collections(board: &Value) -> bool {
    let product = |product: &Value| product["workers"].is_array();
    board["departments"].as_array().is_some_and(|departments| {
        departments.iter().all(|department| {
            department["products"]
                .as_array()
                .is_some_and(|products| products.iter().all(product))
        })
    }) && board["unassignedProducts"]
        .as_array()
        .is_some_and(|products| products.iter().all(product))
}

fn state(worker: &Value, id: u64, processing: &BTreeSet<u64>) -> &'static str {
    // Canonical work waits take precedence over a collector captured before the wait.
    match worker["recordedStatus"].as_str() {
        Some("done") => "done",
        Some("recovery_unverified") => "recovery_unverified",
        Some("wait")
            if worker["dependencies"]
                .as_array()
                .is_some_and(|ids| ids.iter().any(|id| id.as_u64().is_some_and(|id| id > 0))) =>
        {
            "dependency_wait"
        }
        Some("wait") => "execution_unverified",
        Some("block") => match worker["waitingKind"].as_str() {
            Some("decision") => "decision_wait",
            Some("resource") => "resource_wait",
            _ => "blocked_unknown",
        },
        _ if worker["waitingKind"] == "resource" => "resource_wait",
        _ if worker["jobStatus"] == "queued" => "job_queued",
        _ if processing.contains(&id) => "processing",
        Some("todo") => "not_started",
        _ => "execution_unverified",
    }
}

/// Pure projection of the resident board and the existing active-only collector cache.
/// A partial acquisition can prove individual workers; it cannot certify a total.
pub fn project(board: &Value, active: Option<&Value>, now: u64) -> Value {
    let fetched = board["fetchedAt"].as_u64();
    let ready = (board["metadataOk"] == true || board["sourceOk"] == true)
        && complete_collections(board)
        && fetched.is_some_and(|at| at <= now);
    let stale = ready && !recent(fetched, now);
    let mut counts: BTreeMap<&str, usize> = STATES.into_iter().map(|state| (state, 0)).collect();
    let stopped = if ready && !stale {
        records(board)
            .filter(|row| {
                matches!(
                    row["recordedStatus"].as_str(),
                    Some("wait" | "block" | "done" | "recovery_unverified")
                )
            })
            .filter_map(|row| {
                row["id"]
                    .as_str()?
                    .strip_prefix("todo-")?
                    .parse::<u64>()
                    .ok()
            })
            .collect()
    } else {
        BTreeSet::new()
    };
    let (processing, processing_ids) = evidence(active, now, &stopped);
    let current_ids = if stale {
        BTreeSet::new()
    } else {
        processing_ids
    };
    let mut seen = BTreeSet::new();
    let mut tasks = Vec::new();
    let mut finished = 0;
    let mut measured = 0;
    let mut results = 0;
    let mut job_rows = 0;
    let mut unknown_rows = 0;
    let mut seen_jobs = BTreeSet::new();
    if ready {
        for worker in records(board) {
            let Some(id) = worker["id"]
                .as_str()
                .and_then(|id| id.strip_prefix("todo-"))
                .and_then(|id| id.parse::<u64>().ok())
                .filter(|id| *id > 0)
            else {
                if let Some(key) = worker["id"]
                    .as_str()
                    .and_then(|id| id.strip_prefix("job-"))
                    .filter(|key| key.len() == 16 && key.bytes().all(|b| b.is_ascii_hexdigit()))
                {
                    job_rows += usize::from(seen_jobs.insert(key));
                } else {
                    unknown_rows += 1;
                }
                continue;
            };
            if !seen.insert(id) {
                continue;
            }
            let state = state(worker, id, &current_ids);
            *counts.get_mut(state).unwrap() += 1;
            finished += usize::from(state == "done");
            let progress = worker["progress"]
                .as_u64()
                .filter(|progress| *progress <= 100);
            measured += usize::from(progress.is_some());
            let result_at = worker["lastResultAt"]
                .as_u64()
                .or_else(|| at(&worker["lastResultAt"]))
                .filter(|at| *at <= now);
            results += usize::from(result_at.is_some());
            tasks.push(json!({"todoId":id,"state":state,"progress":progress,"lastResultAt":result_at,
                "references":{"todoId":id,"session":null,"issue":null,"pr":null,"test":null,"deployment":null}}));
        }
    }
    let states: Map<String, Value> = counts
        .into_iter()
        .map(|(key, value)| (key.into(), if ready { json!(value) } else { Value::Null }))
        .collect();
    let mut recorded = Map::new();
    for key in [
        "activeJobs",
        "jobQueued",
        "consoleQueued",
        "consolePreparationFailures",
        "todoDependencyWaits",
        "todoPendingStarts",
        "todoBlocked",
        "recoveryUnverified",
    ] {
        recorded.insert(
            key.into(),
            board["metrics"][key]
                .as_u64()
                .map(Value::from)
                .unwrap_or(Value::Null),
        );
    }
    json!({"schemaVersion":1,"state":if stale {"stale"} else if ready {"available"} else {"unavailable"},"observedAt":fetched,
        "inventory":{"taskRecords":if ready {Some(tasks.len())} else {None},
            "unfinishedTaskRecords":if ready {Some(tasks.len()-finished)} else {None},
            "jobOnlyRecords":if ready {Some(job_rows)} else {None},
            "unknownRecords":if ready {Some(unknown_rows)} else {None},
            "measuredProgressRecords":if ready {Some(measured)} else {None},
            "resultTimeRecords":if ready {Some(results)} else {None}},
        "states":states,"tasks":tasks,"recorded":recorded,"processing":processing,
        "coverage":[{"kind":"cc_jobs","state":processing["state"]},
            {"kind":"native_codex","state":"unobserved"},{"kind":"claude","state":"unobserved"}],
        "responsibility":{"state":"not_assigned","productRoles":null,"departmentRoles":null}})
}
