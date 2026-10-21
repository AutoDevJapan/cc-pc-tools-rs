//! Counts jobs, task records and verified processing separately.
//! Inputs must already be privacy-restricted projections; no source prose is read here.
use chrono::DateTime;
use serde_json::{json, Map, Value};
use std::collections::{BTreeMap, BTreeSet};

const STATES: [&str; 12] = [
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
    "intake_wait",
    "startup_failure",
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
    // The Go collector owns the canonical predicate, including cancellation,
    // process identity and resource markers. Expire its certificate here only;
    // never manufacture one from the diagnostic fields alone.
    job["processing_verified"] == true
        && job["recorded_status"] == "running"
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
        && input["processing_contract"] == "agent-progress-active-v1"
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
    let mut sessions = Vec::new();
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
        if !job["processing_verified"].is_boolean() {
            invalid = true;
        }
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
        sessions.push(json!({"key":format!("job-{key}"),
            "validUntil":at(&job["last_activity"]["at"]).unwrap().min(observed.unwrap()).saturating_add(30_000)}));
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
        && input["sources"]["todo"]["status"] == "ok"
        && input["sources"]["todo"]["complete"] == true
        && input["sources"]["jobs"]["status"] == "ok"
        && input["sources"]["jobs"]["complete"] == true;
    let expires = input["jobs"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|job| processing(job, now))
        .filter_map(|job| at(&job["last_activity"]["at"]))
        .max()
        .unwrap_or(observed.unwrap());
    (
        json!({"state":if complete {"complete"} else {"partial"},"verifiedCount":count,
        "total":if complete {Some(count)} else {None},"observedAt":observed,
        "startedAt":captured,"validUntil":expires.min(observed.unwrap()).saturating_add(30_000),"sessions":sessions}),
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
        departments.iter().all(|d| {
            d["products"]
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
            Some("decision" | "owner") => "decision_wait",
            Some("resource") => "resource_wait",
            _ => "blocked_unknown",
        },
        _ if worker["waitingKind"] == "resource" => "resource_wait",
        _ if worker["waitingKind"] == "intake" => "intake_wait",
        _ if worker["waitingKind"] == "startup_error" => "startup_failure",
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
            let sessions: Vec<_> = active
                .into_iter()
                .flat_map(|data| data["jobs"].as_array().into_iter().flatten())
                .filter(|job| {
                    state == "processing"
                        && self::processing(job, now)
                        && job["todo_ids"]
                            .as_array()
                            .is_some_and(|ids| ids.iter().any(|value| value.as_u64() == Some(id)))
                })
                .filter_map(|job| job["job_key"].as_str())
                .filter(|key| {
                    key.strip_prefix("job-")
                        .is_some_and(|k| k.len() == 64 && k.bytes().all(|b| b.is_ascii_hexdigit()))
                })
                .collect::<BTreeSet<_>>()
                .into_iter()
                .collect();
            tasks.push(json!({"todoId":id,"state":state,"progress":progress,"lastResultAt":result_at,
                "priority":worker["priority"].as_u64(),"recordUpdatedAt":worker["updatedAt"].as_u64().or_else(||at(&worker["updatedAt"])),
                "dependencies":worker["dependencies"].as_array().into_iter().flatten().filter_map(Value::as_u64).filter(|id|*id>0).collect::<Vec<_>>(),
                "references":{"todoId":id,"sessions":sessions,"session":sessions.first(),"issue":null,"pr":null,"test":null,"deployment":null},
                "availability":{"session":if sessions.is_empty(){"unobserved"}else{"verified_processing"},"progress":if progress.is_some(){"measured"}else{"unrecorded"},
                    "result":if board["protectionMode"]=="masked_metadata"{"protected"}else{"unrecorded"},"links":if board["protectionMode"]=="masked_metadata"{"protected"}else{"unrecorded"}}}));
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
    let mut roles = Vec::new();
    let mut product_ids = BTreeSet::new();
    for product in board["departments"]
        .as_array()
        .into_iter()
        .flatten()
        .flat_map(|d| d["products"].as_array().into_iter().flatten())
    {
        let Some(id) = product["id"].as_str().filter(|id| {
            id.strip_prefix("product-")
                .is_some_and(|key| key.len() == 16 && key.bytes().all(|b| b.is_ascii_hexdigit()))
        }) else {
            continue;
        };
        if !product_ids.insert(id) {
            continue;
        }
        let task_ids: Vec<_> = product["workers"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|row| {
                row["id"]
                    .as_str()?
                    .strip_prefix("todo-")?
                    .parse::<u64>()
                    .ok()
            })
            .filter(|id| *id > 0)
            .collect();
        roles.push(
            json!({"productId":id,"state":"registered","taskIds":task_ids,
            "currentSession":null,"sessionReason":"個別の担当は作業ごとの観測で確認します。"}),
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
        "responsibility":{"state":"registered","model":"per_product","productRoles":if ready{Some(roles.len())}else{None},
            "departmentRoles":null,"roles":roles,"residentProcesses":0,
            "decisionUrl":"https://discord.com/channels/1552944707464597524/1561946066696278066/1561946270082404463"}})
}

pub fn generation(
    board: &Value,
    active: Option<&Value>,
    previous: Option<&Value>,
    started: u64,
    completed: u64,
    error: Option<&str>,
) -> Value {
    let mut value = project(board, active, completed);
    value["acquisition"] = json!({"startedAt":started,"completedAt":completed,"error":error});
    if value["state"] == "unavailable" {
        let previous = previous.and_then(|old| {
            if old["state"] == "unavailable" {
                old.get("previous")
            } else {
                Some(old)
            }
        });
        if let Some(previous) = previous {
            let mut old = previous.clone();
            old.as_object_mut().unwrap().remove("previous");
            expire(&mut old, completed);
            value["previous"] = old;
        }
    }
    value
}

/// A resident can stop refreshing; read-only HTTP consumers must still expire proof.
pub fn expire(value: &mut Value, now: u64) {
    if value["observedAt"]
        .as_u64()
        .is_some_and(|at| !recent(Some(at), now))
        && value["state"] == "available"
    {
        value["state"] = json!("stale");
    }
    let expired = value["processing"]["validUntil"]
        .as_u64()
        .is_some_and(|until| now > until);
    if let Some(sessions) = value["processing"]["sessions"].as_array().cloned() {
        let retained: Vec<_> = sessions
            .iter()
            .filter(|session| {
                session["validUntil"]
                    .as_u64()
                    .is_some_and(|until| now <= until)
            })
            .cloned()
            .collect();
        if retained.len() != sessions.len() || expired {
            let keys: BTreeSet<_> = retained
                .iter()
                .filter_map(|session| session["key"].as_str())
                .map(str::to_owned)
                .collect();
            value["processing"]["state"] = json!(if retained.is_empty() {
                "stale"
            } else {
                "partial"
            });
            value["processing"]["verifiedCount"] = if retained.is_empty() {
                Value::Null
            } else {
                json!(retained.len())
            };
            value["processing"]["total"] = Value::Null;
            let mut moved = 0u64;
            for task in value["tasks"].as_array_mut().into_iter().flatten() {
                if task["state"] != "processing" {
                    continue;
                }
                let sessions: Vec<_> = task["references"]["sessions"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter(|key| key.as_str().is_some_and(|key| keys.contains(key)))
                    .cloned()
                    .collect();
                task["references"]["session"] = sessions.first().cloned().unwrap_or(Value::Null);
                task["references"]["sessions"] = json!(sessions);
                if sessions.is_empty() {
                    task["state"] = json!("execution_unverified");
                    task["availability"]["session"] = json!("stale");
                    moved += 1;
                }
            }
            if moved > 0 {
                value["states"]["processing"] = json!(value["states"]["processing"]
                    .as_u64()
                    .unwrap_or(0)
                    .saturating_sub(moved));
                value["states"]["execution_unverified"] = json!(
                    value["states"]["execution_unverified"]
                        .as_u64()
                        .unwrap_or(0)
                        + moved
                );
            }
            value["processing"]["sessions"] = json!(retained);
        }
        return;
    }
    if expired {
        value["processing"]["state"] = json!("stale");
        value["processing"]["verifiedCount"] = Value::Null;
        value["processing"]["total"] = Value::Null;
        let mut moved = 0u64;
        for task in value["tasks"].as_array_mut().into_iter().flatten() {
            if task["state"] == "processing" {
                task["state"] = json!("execution_unverified");
                task["references"]["session"] = Value::Null;
                task["references"]["sessions"] = json!([]);
                task["availability"]["session"] = json!("stale");
                moved += 1;
            }
        }
        if moved > 0 {
            value["states"]["processing"] = json!(0);
            value["states"]["execution_unverified"] = json!(
                value["states"]["execution_unverified"]
                    .as_u64()
                    .unwrap_or(0)
                    + moved
            );
        }
    }
}

/// Update compatible display rows from the strict shared projection; keep the
/// original job/ToDo state alongside it. No raw source is revisited.
pub fn apply_to_board(board: &mut Value) {
    let tasks: BTreeMap<_, _> = board["workforce"]["tasks"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|task| Some((task["todoId"].as_u64()?, task.clone())))
        .collect();
    let protected = board["protectionMode"] == "masked_metadata";
    let groups = board["departments"]
        .as_array_mut()
        .into_iter()
        .flatten()
        .flat_map(|d| d["products"].as_array_mut().into_iter().flatten());
    for product in groups {
        apply_product(product, &tasks, protected);
    }
    for product in board["unassignedProducts"]
        .as_array_mut()
        .into_iter()
        .flatten()
    {
        apply_product(product, &tasks, protected);
    }
}

/// Join instrumented native receipts, without turning native UUIDs into CC jobs.
pub fn merge_native(board: &mut Value, native: &Value, now: u64) {
    board["nativeActivity"] = native.clone();
    let Some(workforce) = board.get_mut("workforce").filter(|value| value.is_object()) else {
        return;
    };
    let mut proofs = workforce["processing"]["sessions"]
        .as_array()
        .cloned()
        .unwrap_or_default();
    let cc_keys: BTreeSet<String> = proofs
        .iter()
        .filter_map(|proof| proof["key"].as_str().map(str::to_owned))
        .collect();
    let cc_count = workforce["processing"]["verifiedCount"].as_u64();
    let mut added = BTreeSet::new();
    for session in native["sessions"].as_array().into_iter().flatten() {
        let (Some(id), Some(key), Some(until)) = (
            session["todoId"].as_u64(),
            session["key"].as_str(),
            session["validUntil"].as_u64(),
        ) else {
            continue;
        };
        if until < now || !key.starts_with("native-") || added.contains(key) {
            continue;
        }
        let Some(task) = workforce["tasks"]
            .as_array_mut()
            .into_iter()
            .flatten()
            .find(|task| task["todoId"] == id)
        else {
            continue;
        };
        if task["state"] == "done"
            || task["references"]["sessions"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|key| key.as_str().is_some_and(|key| cc_keys.contains(key)))
        {
            continue;
        }
        task["recordedWorkState"] = task["state"].clone();
        task["state"] = json!("processing");
        let mut refs = task["references"]["sessions"]
            .as_array()
            .cloned()
            .unwrap_or_default();
        refs.push(json!(key));
        refs.sort_by(|a, b| a.as_str().cmp(&b.as_str()));
        refs.dedup();
        task["references"]["session"] = refs.first().cloned().unwrap_or(Value::Null);
        task["references"]["sessions"] = json!(refs);
        task["availability"]["session"] = json!("native_tool_receipt");
        task["nativeExecution"] = session.clone();
        added.insert(key.to_owned());
        proofs.push(session.clone());
    }
    let total = cc_count.unwrap_or(0) + added.len() as u64;
    workforce["processing"]["verifiedCount"] = if cc_count.is_some() || !added.is_empty() {
        json!(total)
    } else {
        Value::Null
    };
    workforce["processing"]["total"] = Value::Null;
    if workforce["processing"]["state"] != "unavailable" || !added.is_empty() {
        workforce["processing"]["state"] = json!("partial")
    }
    if !added.is_empty() {
        workforce["processing"]["validUntil"] = proofs
            .iter()
            .filter_map(|proof| proof["validUntil"].as_u64())
            .max()
            .map(Value::from)
            .unwrap_or(Value::Null);
        workforce["processing"]["observedAt"] = proofs
            .iter()
            .filter_map(|proof| proof["actualAt"].as_u64())
            .chain(workforce["processing"]["observedAt"].as_u64())
            .max()
            .map(Value::from)
            .unwrap_or(Value::Null);
    }
    workforce["processing"]["sessions"] = json!(proofs);
    workforce["processing"]["bySource"] =
        json!({"ccJobs":cc_count,"nativeToolReceipts":added.len()});
    let mut counts = BTreeMap::<String, u64>::new();
    for task in workforce["tasks"].as_array().into_iter().flatten() {
        if let Some(state) = task["state"].as_str() {
            *counts.entry(state.to_owned()).or_default() += 1;
        }
    }
    if !added.is_empty() {
        if let Some(states) = workforce["states"].as_object_mut() {
            for (state, count) in states.iter_mut() {
                *count = json!(counts.get(state).copied().unwrap_or(0));
            }
        }
    }
    if let Some(coverage) = workforce["coverage"].as_array_mut() {
        for source in coverage {
            if source["kind"] == "native_codex" {
                source["state"] = native["state"].clone();
                source["complete"] = json!(false);
            }
        }
    }
}

fn apply_product(product: &mut Value, tasks: &BTreeMap<u64, Value>, protected: bool) {
    for worker in product["workers"].as_array_mut().into_iter().flatten() {
        let task = worker["id"]
            .as_str()
            .and_then(|id| id.strip_prefix("todo-"))
            .and_then(|id| id.parse::<u64>().ok())
            .and_then(|id| tasks.get(&id));
        let state = task
            .and_then(|task| task["state"].as_str())
            .unwrap_or("execution_unverified");
        worker["workState"] = json!(state);
        worker["status"] = json!(match state {
            "processing" => "working",
            "done" => "done",
            "not_started" => "idle",
            "startup_failure" => "attention",
            "execution_unverified" =>
                if worker["status"] == "attention" {
                    "attention"
                } else {
                    "unknown"
                },
            _ => "waiting",
        });
        worker["waitKind"] = json!(match state {
            "decision_wait" => "decision",
            "resource_wait" => "resource",
            "dependency_wait" => "dependency",
            _ => "",
        });
        worker["priority"] = task
            .map(|task| task["priority"].clone())
            .unwrap_or(Value::Null);
        worker["references"] = task
            .map(|task| task["references"].clone())
            .unwrap_or(json!({"sessions":[],"session":null}));
        if let Some(native) = task.and_then(|task| task.get("nativeExecution")) {
            worker["executionSource"] = native.clone();
            worker["nativeExecutionKey"] = native["key"].clone();
            worker["primaryJob"] = Value::Null;
            worker["jobStatus"] = Value::Null;
            worker["processingJobKey"] = Value::Null;
            worker["currentStage"] = json!("native実行のTool結果を確認しています。");
            worker["lastActivityAt"] = native["actualAt"]
                .as_u64()
                .and_then(|at| DateTime::from_timestamp_millis(at as i64))
                .map(|at| json!(at.to_rfc3339()))
                .unwrap_or(Value::Null);
            worker["factsSource"] =
                json!("元ToDoへ結び付いたnative実行の操作結果。CCjobと別のsourceです。");
        }
        if protected {
            worker["lastResultAt"] = Value::Null;
            worker["lastResult"] = json!("作業本文を保護中のため取得できません。");
        }
        if state != "processing" && worker["jobStatus"] == "running" {
            worker["currentStage"] = json!("記録上は実行中・最近の処理は未確認");
        }
        if matches!(
            state,
            "dependency_wait"
                | "decision_wait"
                | "resource_wait"
                | "intake_wait"
                | "startup_failure"
                | "recovery_unverified"
                | "blocked_unknown"
                | "job_queued"
        ) {
            worker["stopReason"] = json!(match state {
                "dependency_wait" => "記録された別のToDoの完了待ちです。",
                "decision_wait" => "送信を確認した質問への本人回答待ちです。",
                "resource_wait" => "記録された資源の空き待ちです。",
                "intake_wait" => "Consoleの受付から担当の準備へ進むのを待っています。",
                "startup_failure" => "担当の準備に失敗した記録があります。",
                "recovery_unverified" => "失われた元の作業記録と成果を確認中です。",
                "job_queued" => "ジョブの起動待ちです。資源待ちかどうかは未確認です。",
                _ => "停止理由を確認できません。本人判断待ちとは確認できていません。",
            });
            worker["nextAction"] = json!(match state {
                "dependency_wait" => "依存先の完了後に元の仕事を続行します。",
                "decision_wait" => "元質問に届いた本人回答を実装と検査へ反映します。",
                "resource_wait" | "job_queued" | "intake_wait" =>
                    "既存の枠と優先順位で担当を起動します。",
                "startup_failure" => "準備失敗の原因を調べて元の仕事を再開します。",
                _ => "元の依頼・成果・停止記録を照合して続行します。",
            });
        }
    }
}
