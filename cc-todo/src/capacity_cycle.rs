//! A bounded measurement feeds one ordinary scheduler row, under todo.lock.
//! This is not a cleanup authority, an original registry, or a worker launcher.
use crate::{Command, Paths, Result, SubworkOptions, add_item, edit_item, edit_text, find, now};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{fs, io::Read, path::Path};

const KEY: &str = "capacity-decision-v1";
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
        categories.push(
            json!({"category":category,"status":status,"logical_bytes":row["logical_bytes"]}),
        );
    }
    categories.sort_by_key(|v| v["category"].as_str().unwrap().to_owned());
    Ok(
        json!({"schema":1,"time":chrono::DateTime::from_timestamp(epoch,0).ok_or("invalid timestamp")?.to_rfc3339_opts(chrono::SecondsFormat::Secs,true),"whole_drive_scan":false,"bytes_reclaimed":0,"categories":categories}),
    )
}

fn generation(report: &Value) -> String {
    format!("{:x}", Sha256::digest(report.to_string().as_bytes()))
}

fn archive_cycle(paths: &Paths, old: &Value) -> Result<()> {
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
    let path = folder.join(format!("{generation}.json"));
    let value = json!({"schema":1,"generation":generation,"measurement":old["active"],"result":old["result"]});
    let data = value.to_string();
    use std::io::Write;
    match fs::OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(&path)
    {
        Ok(mut file) => file
            .write_all(data.as_bytes())
            .and_then(|_| file.sync_all())
            .map_err(crate::err),
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
            if fs::read(path).map_err(crate::err)? == data.as_bytes() {
                Ok(())
            } else {
                Err("capacity archive conflict".into())
            }
        }
        Err(e) => Err(crate::err(e)),
    }
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
                matches!(job["status"].as_str(), Some("succeeded" | "finished"))
                    && !cc_scheduler::policy::alive(&job, &processes, crate::time_s())
                    && cc_scheduler::policy::process_tree(&job, &processes).is_empty()
            })
    })
}

fn no_hold(row: &Value) -> bool {
    [
        "wait_on",
        "wait_ready",
        "conversation_wait",
        "owner_question_wait",
        "decision_wait",
        "owner_postponement_recorded",
        "stop_kind",
    ]
    .iter()
    .all(|key| {
        row[*key].is_null() || row[*key] == false || row[*key] == "" || row[*key] == json!([])
    }) && row["scheduler"]["owner_stop_job"].is_null()
        && row["scheduler"]["question_close_pending"] != true
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
                effort: Some("max".into()),
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
    let (id, action) = if let Some(existing) = matches.first() {
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
        if existing["status"] == "done"
            && gen_id != old["active_generation"]
            && jobs_finished(existing, paths)
            && no_hold(existing)
        {
            archive_cycle(paths, old)?;
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
            past.push(json!({"generation":old["active_generation"],"measurement":old["active"],"result":old["result"]}));
            // Every completed generation has an immutable anonymous receipt.
            // Keep only sixteen summaries in the shared scheduling row.
            if past.len() > 16 {
                past.remove(0);
            }
            history.insert("history".into(), json!(past));
            history.insert("active_generation".into(), json!(gen_id));
            history.insert("active".into(), report.clone());
            history.remove("result");
            // Use the existing dependency/readiness resumption path. It keeps
            // the workspace and result ledger and bypasses obsolete retry clocks.
            let fields = row.as_object_mut().unwrap();
            for key in [
                "work_owner_job",
                "work_owner_wait_on",
                "progress",
                "progress_updated",
            ] {
                fields.remove(key);
            }
            row["status"] = json!("wait");
            cc_job_policy::todo_wait::release_item(row);
            (id, "reused_completed")
        } else {
            (id, "retained_unfinished_or_same_generation")
        }
    } else {
        let id = bind_spec(paths, &text, &report, None, &mut copy, project)?;
        find(&mut copy, id)?["capacity_decision"] = json!({"key":KEY,"origin_todo_id":3085,"active_generation":gen_id,"active":report,"history":[]});
        (id, "queued")
    };
    let row = find(&mut copy, id)?;
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
        "reclaimed_bytes",
    ];
    let unknown = [
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
    row["updated"] = json!(now());
    Ok(json!({"recorded":true,"todo_id":id,"result":result}))
}
