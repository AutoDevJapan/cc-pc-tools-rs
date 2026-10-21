//! A bounded measurement feeds one ordinary scheduler row, under todo.lock.
//! This is not a cleanup authority, an original registry, or a worker launcher.
use crate::{Command, Paths, Result, SubworkOptions, add_item, edit_item, edit_text, find, now};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{fs, io::Read, path::Path};

const KEY: &str = "capacity-decision-v1";

pub fn scheduler_only(row: &Value) -> bool {
    row["capacity_decision"]["key"] == KEY
}
const CATEGORIES: &[&str] = &[
    "windows",
    "installed_apps",
    "legacy_installed_apps",
    "machine_data",
    "owner_data",
    "operations",
    "deletion_requests",
];
const STATUSES: &[&str] = &[
    "partial",
    "complete_metadata_scan",
    "partial_with_errors",
    "protected_unclassified",
    "not_scanned_deadline",
    "missing_linked_or_unavailable",
];

pub fn validate_completion(row: &Value) -> Result<()> {
    if row["capacity_decision"]["key"] == KEY
        && (row["capacity_decision"]["result"].is_null()
            || row["capacity_decision"]["result"]["generation"]
                != row["capacity_decision"]["active_generation"])
    {
        return Err("capacity decision needs a matching aggregate result before completion".into());
    }
    Ok(())
}

pub fn status(db: &Value) -> Result<Value> {
    let rows: Vec<_> = db["items"]
        .as_array()
        .ok_or("invalid items")?
        .iter()
        .filter(|row| row["capacity_decision"]["key"] == KEY)
        .collect();
    if rows.len() > 1 {
        return Err("duplicate capacity decision rows".into());
    }
    let Some(row) = rows.first() else {
        return Ok(json!({"schema":1,"registered":false,"launch_policy":"ordinary_scheduler"}));
    };
    Ok(
        json!({"schema":1,"registered":true,"todo_id":row["id"],"todo_status":row["status"],"model":row["model"],"effort":row["effort"],"jobs":row["jobs"],"active_generation":row["capacity_decision"]["active_generation"],"active_measurement":row["capacity_decision"]["active"],"latest_generation":row["capacity_decision"]["latest_generation"],"latest_measurement":row["capacity_decision"]["latest"],"result":row["capacity_decision"]["result"],"evaluation":row["capacity_decision"]["last_evaluation"],"needs_result_repair":row["capacity_decision"]["missing_result_observed"]==true&&validate_completion(row).is_err(),"has_owner_wait":!row["owner_question_wait"].is_null()||!row["decision_wait"].is_null(),"has_dependency_wait":!row["wait_on"].is_null()&&!row["wait_on"].as_array().is_some_and(Vec::is_empty),"launch_policy":"ordinary_scheduler"}),
    )
}

fn read_json(path: &Path) -> Result<Value> {
    let mut bytes = Vec::new();
    fs::File::open(path)
        .map_err(|_| "capacity input unavailable")?
        .take(65537)
        .read_to_end(&mut bytes)
        .map_err(|_| "capacity input unreadable")?;
    if bytes.len() > 65536 {
        return Err("capacity input too large".into());
    }
    serde_json::from_slice(&bytes).map_err(|_| "invalid capacity input".into())
}

// Project only bounded, known anonymous fields. Ignore no private names into a spec.
pub fn measurement(value: &Value, at: u64) -> Result<Value> {
    let stamp = value["time"].as_str().ok_or("missing measurement time")?;
    let epoch = chrono::DateTime::parse_from_rfc3339(stamp)
        .map_err(|_| "invalid measurement time")?
        .timestamp();
    if epoch < 0
        || epoch as u64 > at
        || at - epoch as u64 > 4 * 3600
        || value["schema"] != 1
        || value["whole_drive_scan"] != false
        || value["bytes_reclaimed"] != 0
    {
        return Err("stale or invalid measurement".into());
    }
    let input = value["categories"]
        .as_array()
        .ok_or("missing measurement categories")?;
    if input.is_empty() || input.len() > CATEGORIES.len() {
        return Err("invalid measurement categories".into());
    }
    let mut categories = Vec::new();
    let mut seen = std::collections::HashSet::new();
    for row in input {
        let category = row["category"].as_str().ok_or("invalid category")?;
        let status = row["status"].as_str().ok_or("invalid scan status")?;
        if !CATEGORIES.contains(&category)
            || !STATUSES.contains(&status)
            || !seen.insert(category)
            || !(row["logical_bytes"].is_null() || row["logical_bytes"].as_u64().is_some())
            || (status == "protected_unclassified" && !row["logical_bytes"].is_null())
        {
            return Err("invalid measurement category".into());
        }
        let mut observed =
            json!({"category":category,"status":status,"logical_bytes":row["logical_bytes"]});
        for field in ["files", "directories", "reparse_points_skipped", "errors"] {
            if let Some(value) = row.get(field) {
                if value.as_u64().is_none() {
                    return Err("invalid measurement count".into());
                }
                observed[field] = value.clone();
            }
        }
        for field in ["scan_started", "scan_completed"] {
            if let Some(value) = row.get(field) {
                let stamp = value
                    .as_str()
                    .and_then(|s| chrono::DateTime::parse_from_rfc3339(s).ok())
                    .ok_or("invalid scan time")?;
                observed[field] = json!(stamp.to_rfc3339_opts(chrono::SecondsFormat::Secs, true));
            }
        }
        categories.push(observed);
    }
    categories.sort_by_key(|v| v["category"].as_str().unwrap().to_owned());
    let mut normalized = json!({"schema":1,"time":chrono::DateTime::from_timestamp(epoch,0).ok_or("invalid timestamp")?.to_rfc3339_opts(chrono::SecondsFormat::Secs,true),"whole_drive_scan":false,"bytes_reclaimed":0,"categories":categories});
    for (field, keys, signed) in [
        ("free_bytes", &["C", "D", "E"][..], false),
        ("used_bytes", &["C", "D", "E"][..], false),
        ("volume_used_change_bytes", &["C", "D", "E"][..], true),
        (
            "known_root_file_bytes",
            &["pagefile", "hibernation", "swapfile", "docker_virtual_disk"][..],
            false,
        ),
    ] {
        if let Some(map) = value.get(field) {
            let map = map.as_object().ok_or("invalid anonymous measurement map")?;
            let mut projected = serde_json::Map::new();
            for key in keys {
                if let Some(value) = map.get(*key) {
                    if !value.is_null()
                        && (if signed {
                            value.as_i64().is_none()
                        } else {
                            value.as_u64().is_none()
                        })
                    {
                        return Err("invalid anonymous capacity value".into());
                    }
                    projected.insert((*key).into(), value.clone());
                }
            }
            normalized[field] = json!(projected);
        }
    }
    Ok(normalized)
}

fn generation(report: &Value) -> String {
    format!("{:x}", Sha256::digest(report.to_string().as_bytes()))
}

fn archive_cycle(paths: &Paths, old: &Value, row: &Value) -> Result<()> {
    let generation = old["active_generation"]
        .as_str()
        .ok_or("invalid archived generation")?;
    if generation.len() != 64
        || !generation
            .bytes()
            .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
    {
        return Err("invalid archived generation".into());
    }
    let folder = paths
        .store
        .parent()
        .ok_or("invalid store parent")?
        .join("capacity-decisions");
    fs::create_dir_all(&folder).map_err(crate::err)?;
    if !folder.is_absolute() {
        return Err("capacity archive needs an absolute store".into());
    }
    let path = folder.join(format!("{generation}.json"));
    let value = json!({"schema":1,"generation":generation,"measurement":old["active"],"result":old["result"],"jobs":row["jobs"],"dispatch":row["scheduler"],"recovery":row["stall_recovery"]});
    let data = value.to_string();
    use std::io::Write;
    let stage = folder.join(format!("cycle-{}.tmp", uuid::Uuid::new_v4().simple()));
    let mut file = fs::OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(&stage)
        .map_err(crate::err)?;
    let write = file
        .write_all(data.as_bytes())
        .and_then(|_| file.sync_all())
        .map_err(crate::err);
    drop(file);
    let result = write.and_then(|_| match fs::hard_link(&stage, &path) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
            let mut bytes = Vec::new();
            fs::File::open(&path)
                .map_err(crate::err)?
                .take((4 << 20) + 1)
                .read_to_end(&mut bytes)
                .map_err(crate::err)?;
            let published: Value =
                serde_json::from_slice(&bytes).map_err(|_| "capacity archive invalid")?;
            // The first fully published cycle is authoritative after a lost DB
            // commit; later mutable dispatch bookkeeping must not deadlock it.
            if bytes.len() <= 4 << 20
                && published["schema"] == 1
                && published["generation"] == generation
                && published["measurement"] == old["active"]
            {
                Ok(())
            } else {
                Err("capacity archive conflict".into())
            }
        }
        Err(e) => Err(crate::err(e)),
    });
    // Only this call's create_new staging leaf is removed. Publishing a full
    // synced file through a no-overwrite hard link cannot leave a partial receipt.
    if stage.is_absolute() && stage.parent() == Some(folder.as_path()) {
        let _ = fs::remove_file(stage);
    }
    result
}

fn jobs_finished(row: &Value, paths: &Paths) -> bool {
    if crate::job_refs::unverified_count(&row["jobs"]) > 0 {
        return false;
    }
    let Ok(processes) = cc_scheduler::native::process_snapshot() else {
        return false;
    };
    crate::job_refs::ids(&row["jobs"]).all(|id| {
        crate::load_job(&paths.jobs.join(id).join("job.json"))
            .ok()
            .is_some_and(|job| {
                matches!(
                    job["status"].as_str(),
                    Some("succeeded" | "finished" | "failed" | "lost" | "cancelled" | "canceled")
                ) && !cc_job_policy::cancellation::blocks_recovery(row, &job)
                    && job["owner_stopped"] != true
                    && !cc_scheduler::policy::alive(&job, &processes, crate::time_s())
                    && cc_scheduler::policy::process_tree(&job, &processes).is_empty()
            })
    })
}

fn hold_reason(row: &Value) -> Option<&'static str> {
    let present =
        |value: &Value| !(value.is_null() || value == false || value == "" || value == &json!([]));
    if row["dispatchable"] != true {
        return Some("automatic_dispatch_disabled");
    }
    if cc_job_policy::cancellation::owner_stopped(row, &Value::Null)
        || row["owner_stopped"] == true
        || !row["scheduler"]["owner_stop_job"].is_null()
    {
        return Some("owner_stop");
    }
    if present(&row["wait_on"]) {
        return Some("dependency_wait");
    }
    if cc_scheduler::common::truth(&row["scheduler"]["question"])
        && !cc_scheduler::common::truth(&row["scheduler"]["answer"])
    {
        return Some("owner_question_unanswered");
    }
    if row["scheduler"]["question_close_pending"] == true {
        return Some("question_close_pending");
    }
    if [
        "conversation_wait",
        "owner_question_wait",
        "decision_wait",
        "owner_postponement_recorded",
        "stop_kind",
    ]
    .iter()
    .any(|key| present(&row[*key]))
    {
        return Some("recorded_hold");
    }
    if present(&row["wait_ready"]) {
        return Some("technical_wait");
    }
    None
}

fn bind_spec(
    paths: &Paths,
    text: &str,
    report: &Value,
    id: Option<i64>,
    db: &mut Value,
    project: &str,
) -> Result<i64> {
    let folder = paths
        .store
        .parent()
        .ok_or("invalid store parent")?
        .join("specs");
    fs::create_dir_all(&folder).map_err(crate::err)?;
    let stage = folder.join(format!(
        "capacity-input-{}.md",
        uuid::Uuid::new_v4().simple()
    ));
    let body = format!(
        "{text}\n\n今回の匿名計測（旧保護ログの転載ではありません）:\n```json\n{report}\n```\n計測世代: {}\n",
        generation(report)
    );
    let mut file = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&stage)
        .map_err(crate::err)?;
    use std::io::Write;
    file.write_all(body.as_bytes())
        .and_then(|_| file.sync_all())
        .map_err(crate::err)?;
    drop(file);
    let result = if let Some(id) = id {
        edit_item(db, id, None, Some(&stage), paths).map(|_| id)
    } else {
        add_item(
            db,
            &Command::Add {
                project: project.into(),
                title: "容量計測から不要物を有限に判定する".into(),
                note: "未調査・候補・保持・回収・必要な本人判断を区別して記録する。".into(),
                spec: Some(stage.clone()),
                model: Some("gpt-6.1-sol".into()),
                model_origin: Some("policy".into()),
                effort: Some("max".into()),
                effort_origin: Some("policy".into()),
                needs: vec![],
                progress: None,
                subwork: SubworkOptions::default(),
            },
            paths,
        )
    };
    // Only the exact newly created file is removed; canonical spec copies survive.
    let _ = fs::remove_file(stage);
    result
}

pub fn ingest(
    db: &mut Value,
    paths: &Paths,
    project: &str,
    report_file: &Path,
    spec: &Path,
    at: u64,
) -> Result<Value> {
    let report = measurement(&read_json(report_file)?, at)?;
    let text = edit_text(spec)?;
    if text.len() > 32768 {
        return Err("capacity spec too large".into());
    }
    if !db["projects"][project].is_string() {
        return Err("capacity project is not registered".into());
    }
    let rows = db["items"].as_array().ok_or("invalid items")?;
    let matches: Vec<_> = rows
        .iter()
        .filter(|r| r["capacity_decision"]["key"] == KEY)
        .collect();
    if matches.len() > 1 {
        return Err("duplicate capacity decision rows".into());
    }
    let gen_id = generation(&report);
    let mut copy = db.clone();
    let (id, action, reason, missing) = if let Some(existing) = matches.first() {
        if existing["project"] != project {
            return Err("capacity project changed".into());
        }
        let id = existing["id"].as_i64().ok_or("invalid decision id")?;
        let old = &existing["capacity_decision"];
        if report["time"].as_str() < old["latest"]["time"].as_str() {
            return Err("older capacity generation".into());
        }
        if report["time"] == old["latest"]["time"] && gen_id != old["latest_generation"] {
            return Err("conflicting capacity generation".into());
        }
        let missing = existing["status"] == "done" && validate_completion(existing).is_err();
        let hold = hold_reason(existing);
        let finished =
            existing["status"] == "done" && hold.is_none() && jobs_finished(existing, paths);
        if missing && finished && old["last_unrecorded_measurement"] != gen_id {
            let instruction = format!(
                "{text}\n技術的な再開: この元の計測世代の集計が保存されていません。元の世代をcapacity-resultへ記録するまで未完です。再開を促した最新の匿名計測（元の世代は末尾のまま保持）: {report}\n"
            );
            bind_spec(
                paths,
                &instruction,
                &old["active"],
                Some(id),
                &mut copy,
                project,
            )?;
            let row = find(&mut copy, id)?;
            row["capacity_decision"]["last_unrecorded_measurement"] = json!(gen_id);
            row["capacity_decision"]["missing_result_observed"] = json!(true);
            row["status"] = json!("wait");
            // This is still the SAME unfinished generation: keep its owner
            // answers/jobs and use ordinary scheduler recovery, not a new cycle.
            row.as_object_mut().unwrap().remove("work_owner_job");
            cc_job_policy::todo_wait::release_item(row);
            (
                id,
                "requeued_missing_result",
                "decision_result_missing",
                true,
            )
        } else if existing["status"] == "done"
            && !missing
            && gen_id != old["active_generation"]
            && finished
        {
            archive_cycle(paths, old, existing)?;
            bind_spec(paths, &text, &report, Some(id), &mut copy, project)?;
            let row = find(&mut copy, id)?;
            let history = row["capacity_decision"]
                .as_object_mut()
                .ok_or("invalid decision marker")?;
            let mut past = history
                .get("history")
                .and_then(Value::as_array)
                .cloned()
                .unwrap_or_default();
            past.push(json!({"generation":old["active_generation"],"measurement":old["active"],"result":old["result"],"jobs":existing["jobs"]}));
            // Every completed generation has an immutable lifecycle receipt.
            // Keep only sixteen summaries in the shared scheduling row.
            if past.len() > 16 {
                past.remove(0);
            }
            history.insert("history".into(), json!(past));
            history.insert("active_generation".into(), json!(gen_id));
            history.insert("active".into(), report.clone());
            history.remove("result");
            history.remove("missing_result_observed");
            history.remove("last_unrecorded_measurement");
            // A completed cycle is a fresh finite dispatch, never a retry of
            // its old conversation. Preserve the workspace and archived history.
            let mut workspace = json!({});
            for key in ["worktree", "branch"] {
                if let Some(value) = existing["scheduler"].get(key) {
                    workspace[key] = value.clone();
                }
            }
            let fields = row.as_object_mut().unwrap();
            for key in [
                "work_owner_job",
                "work_owner_wait_on",
                "progress",
                "progress_updated",
                "worker_receipt",
                "dependency_ready",
                "stall_recovery",
            ] {
                fields.remove(key);
            }
            row["jobs"] = json!([]);
            row["scheduler"] = workspace;
            row["status"] = json!("todo");
            (id, "reused_completed", "new_measurement", false)
        } else {
            let reason = hold.unwrap_or(if missing {
                "decision_result_missing"
            } else if existing["status"] == "done" && gen_id == old["active_generation"] {
                "same_generation"
            } else if existing["status"] == "done" && !finished {
                "job_not_finished_or_unverified"
            } else {
                "unfinished"
            });
            (
                id,
                "retained_unfinished_or_same_generation",
                reason,
                missing,
            )
        }
    } else {
        let id = bind_spec(paths, &text, &report, None, &mut copy, project)?;
        find(&mut copy, id)?["capacity_decision"] = json!({"key":KEY,"origin_todo_id":3085,"active_generation":gen_id,"active":report,"history":[]});
        (id, "queued", "first_measurement", false)
    };
    let row = find(&mut copy, id)?;
    let retained = action == "retained_unfinished_or_same_generation";
    let previous = &row["capacity_decision"]["last_evaluation"];
    let counter = if retained {
        previous["consecutive_retained"]
            .as_u64()
            .unwrap_or(0)
            .saturating_add(u64::from(
                previous["generation"] != gen_id || previous["action"] != action,
            ))
    } else {
        0
    };
    row["capacity_decision"]["last_evaluation"] = json!({"generation":gen_id,"action":action,"reason":reason,"missing_decision_result":missing,"consecutive_retained":counter,"recorded_at":now()});
    if missing {
        row["capacity_decision"]["missing_result_observed"] = json!(true);
    }
    row["capacity_decision"]["latest_generation"] = json!(gen_id);
    row["capacity_decision"]["latest"] = report;
    row["updated"] = json!(now());
    *db = copy;
    Ok(json!({"schema":1,"todo_id":id,"generation":gen_id,"action":action,"worker_launched":false}))
}

pub fn record_result(db: &mut Value, id: i64, path: &Path) -> Result<Value> {
    let value = read_json(path)?;
    let row = find(db, id)?;
    if row["capacity_decision"]["key"] != KEY
        || value["schema"] != 1
        || value["generation"] != row["capacity_decision"]["active_generation"]
    {
        return Err("capacity result generation mismatch".into());
    }
    let numeric = [
        "uninvestigated_categories",
        "investigated_resources",
        "candidates",
        "retained",
    ];
    let unknown = [
        "reclaimed_bytes",
        "archive_verified",
        "questions_created",
        "questions_sent",
        "answers_applied",
    ];
    let mut result = json!({"schema":1,"generation":value["generation"]});
    for field in numeric {
        if value[field].as_u64().is_none() {
            return Err("invalid capacity result count".into());
        }
        result[field] = value[field].clone();
    }
    for field in unknown {
        if !(value[field].is_null() || value[field].as_u64().is_some()) {
            return Err("invalid capacity result count".into());
        }
        result[field] = value[field].clone();
    }
    let reasons = value["retention_reasons"]
        .as_object()
        .ok_or("missing retention reasons")?;
    let allowed = [
        "active_owner",
        "unfinished",
        "protected_unclassified",
        "ownership_unverified",
        "source_or_recovery",
        "needed",
        "unavailable",
        "no_eligible_candidates",
    ];
    if reasons.len() > allowed.len()
        || reasons
            .iter()
            .any(|(k, v)| !allowed.contains(&k.as_str()) || v.as_u64().is_none())
    {
        return Err("invalid retention reasons".into());
    }
    result["retention_reasons"] = json!(reasons);
    let protected = row["capacity_decision"]["active"]["categories"]
        .as_array()
        .ok_or("missing active categories")?
        .iter()
        .filter(|r| r["status"] != "complete_metadata_scan")
        .count() as u64;
    if result["uninvestigated_categories"].as_u64().unwrap() < protected {
        return Err("uninvestigated capacity falsely completed".into());
    }
    row["capacity_decision"]["result"] = result.clone();
    row["capacity_decision"]
        .as_object_mut()
        .unwrap()
        .remove("missing_result_observed");
    row["updated"] = json!(now());
    Ok(json!({"recorded":true,"todo_id":id,"result":result}))
}
