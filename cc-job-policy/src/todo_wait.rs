//! Durable task prerequisites, shared by every automatic launcher.
use serde_json::{Value, json};
use std::collections::BTreeSet;

pub fn pending_on(db: &Value, item: &Value) -> Vec<i64> {
    item["wait_on"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_i64)
        .filter(|id| {
            !db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|target| target["id"].as_i64() == Some(*id) && target["status"] == "done")
        })
        .collect()
}

pub fn waiting(db: &Value, item: &Value) -> bool {
    !pending_on(db, item).is_empty()
}

/// A retry deadline permits another check, not a claim that a capability recovered.
pub fn retry_due(wait: &Value, at: u64) -> bool {
    let (Some(since), Some(until)) = (wait["since"].as_u64(), wait["not_before"].as_u64()) else {
        return false;
    };
    wait["condition"] == "retry-time"
        && until
            .checked_sub(since)
            .is_some_and(|seconds| (60..=86400).contains(&seconds))
        && since <= at
        && until <= at
}

pub fn ready_message(wait: &Value) -> String {
    match wait["condition"].as_str() {
        Some("chatgpt-plus") => "ChatGPT Plusの復旧確認待ち".into(),
        Some("retry-time") => {
            let deadline = wait["not_before"]
                .as_i64()
                .and_then(|at| chrono::DateTime::from_timestamp(at, 0))
                .map(|at| {
                    at.with_timezone(&chrono::FixedOffset::east_opt(9 * 3600).unwrap())
                        .format("%m/%d %H:%M JST")
                        .to_string()
                });
            match deadline {
                Some(at) => format!("次の再確認まで待機（{at}）"),
                None => "次の再確認まで待機（時刻未確認）".into(),
            }
        }
        _ => "再開条件の確認待ち".into(),
    }
}

/// Use saved numeric/job links only. Keep readiness waits distinct from ToDo edges.
pub fn job_ready_waits(db: &Value, job: &Value) -> Vec<Value> {
    db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|item| crate::priority::linked(item, job))
        .filter(|item| !item["wait_ready"].is_null() && item["status"] != "done")
        .map(|item| {
            json!({"item":item["id"],"wait":item["wait_ready"],
            "message":ready_message(&item["wait_ready"])})
        })
        .collect()
}

/// Validate the entire proposed edge set before mutating the store.
pub fn validate(db: &Value, id: i64, on: &[i64]) -> Result<(), String> {
    let items = db["items"].as_array().ok_or("invalid items")?;
    let item = items
        .iter()
        .find(|i| i["id"].as_i64() == Some(id))
        .ok_or_else(|| format!("no item {id}"))?;
    if item["status"] == "done" {
        return Err("完了したToDoには待機を設定できません。再開してから設定してください".into());
    }
    if on.is_empty() {
        return Err("待つToDoを --on で指定してください".into());
    }
    // Independent work must never (even through another prerequisite) wait for its root.
    for child in items.iter().filter(|row| row["subwork"].is_object()) {
        let mut stack = vec![child["id"].as_i64().ok_or("invalid subwork id")?];
        let mut seen = BTreeSet::new();
        while let Some(next) = stack.pop() {
            if Some(next) == child["subwork"]["parent_todo_id"].as_i64() {
                return Err("独立した下位作業は主担当の完了を待てません".into());
            }
            if !seen.insert(next) {
                continue;
            }
            if next == id {
                stack.extend(on.iter().copied());
            } else if let Some(row) = items.iter().find(|row| row["id"] == next)
                && row["status"] != "done"
            {
                stack.extend(
                    row["wait_on"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .filter_map(Value::as_i64),
                );
            }
        }
    }
    for target in on {
        if !items.iter().any(|i| i["id"].as_i64() == Some(*target)) {
            return Err(format!("no item {target}"));
        }
        let mut stack = vec![*target];
        let mut seen = BTreeSet::new();
        while let Some(next) = stack.pop() {
            if next == id {
                return Err("自分自身や循環するToDoの完了は待てません".into());
            }
            if !seen.insert(next) {
                continue;
            }
            if let Some(row) = items.iter().find(|i| i["id"].as_i64() == Some(next))
                && row["status"] != "done"
            {
                stack.extend(
                    row["wait_on"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .filter_map(Value::as_i64),
                );
            }
        }
    }
    Ok(())
}

/// Restore eligibility without removing owner stops, workspaces, specs or history.
/// The readiness marker lasts until a worker is assigned and bypasses old retry timers.
pub fn release_ready(db: &mut Value) -> Vec<i64> {
    release_ready_if(db, |_| true)
}

/// Reuse normal dependency release within an already verified operational scope.
/// The caller supplies eligibility; dependencies are always checked against this store.
pub fn release_ready_if(db: &mut Value, mut eligible: impl FnMut(&Value) -> bool) -> Vec<i64> {
    let ready: BTreeSet<i64> = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|i| i["wait_on"].as_array().is_some_and(|a| !a.is_empty()) && !waiting(db, i))
        .filter(|i| i["wait_ready"].is_null())
        .filter(|i| eligible(i))
        .filter_map(|i| i["id"].as_i64())
        .collect();
    let mut released = Vec::new();
    for item in db["items"].as_array_mut().into_iter().flatten() {
        let Some(id) = item["id"].as_i64() else {
            continue;
        };
        if !ready.contains(&id) {
            continue;
        }
        item.as_object_mut().unwrap().remove("wait_on");
        if item["status"] != "wait" {
            continue;
        }
        release_item(item);
        released.push(id);
    }
    released
}
/// An exhausted automatic owner edge uses the same normal resumption path.
pub fn release_item(item: &mut Value) {
    if !item["wait_ready"].is_null() {
        return;
    }
    if item["status"] == "wait" {
        item["status"] = json!("todo");
        item["dependency_ready"] = json!(true);
        item["dependency_generation"] = json!(
            item["dependency_generation"]
                .as_u64()
                .unwrap_or(0)
                .saturating_add(1)
        );
        if let Some(scheduler) = item["scheduler"].as_object_mut() {
            for key in ["job", "key", "selection"] {
                scheduler.remove(key);
            }
            scheduler.insert("not_before".into(), json!(0));
            scheduler.insert("phase".into(), json!("retry"));
            scheduler.insert("resume".into(), json!(true));
        }
        if let Some(recovery) = item["stall_recovery"].as_object_mut() {
            for key in ["pending", "report", "since"] {
                recovery.remove(key);
            }
            recovery.insert("next_at".into(), json!(0));
        }
        item.as_object_mut()
            .unwrap()
            .remove("discord_restart_not_before");
    }
}

/// Exact job ownership; never infer a dependency from arbitrary prompt text.
pub fn job_pending_on(db: &Value, job: &Value) -> Vec<i64> {
    let mut on = BTreeSet::new();
    for item in db["items"].as_array().into_iter().flatten() {
        let linked = (item["id"].as_i64().is_some()
            && job["related_todo_ids"]
                .as_array()
                .is_some_and(|ids| ids.contains(&item["id"])))
            || (job["todo_id"].as_i64().is_some() && job["todo_id"] == item["id"])
            || (job["id"].is_string()
                && (item["jobs"]
                    .as_array()
                    .is_some_and(|a| a.contains(&job["id"]))
                    || item["scheduler"]["job"] == job["id"]));
        if linked {
            on.extend(pending_on(db, item));
        }
    }
    on.into_iter().collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn scoped_release_preserves_unverified_rows_and_missing_edges() {
        let mut db = json!({"items":[
            {"id":1,"status":"wait","wait_on":[4],"spec":"saved"},
            {"id":2,"status":"wait","wait_on":[4]},
            {"id":3,"status":"wait","wait_on":[4,99]},
            {"id":4,"status":"done"}
        ]});
        let unverified = db["items"][1].clone();
        let missing = db["items"][2].clone();
        assert_eq!(release_ready_if(&mut db, |i| i["id"] != 2), vec![1]);
        assert_eq!(db["items"][1], unverified);
        assert_eq!(db["items"][2], missing);
        assert_eq!(db["items"][0]["spec"], "saved");
        assert_eq!(db["items"][0]["dependency_generation"], 1);
        assert!(release_ready_if(&mut db, |i| i["id"] != 2).is_empty());
    }
    #[test]
    fn readiness_wait_uses_exact_job_links_and_never_invents_dependency_edges() {
        let db = json!({"items":[{"id":1,"status":"wait","wait_ready":{"condition":"retry-time","since":100,"not_before":3700},"jobs":["linked"]},
            {"id":2,"status":"done","wait_ready":{"condition":"chatgpt-plus"}}]});
        for job in [
            json!({"todo_id":1}),
            json!({"related_todo_ids":[1]}),
            json!({"id":"linked"}),
        ] {
            assert_eq!(job_ready_waits(&db, &job).len(), 1);
            assert!(job_pending_on(&db, &job).is_empty());
        }
        assert!(job_ready_waits(&db, &json!({"todo_id":2})).is_empty());
        assert!(job_ready_waits(&db, &json!({"id":"other","prompt":"ToDo 1"})).is_empty());
        assert!(ready_message(&db["items"][0]["wait_ready"]).contains("再確認"));
    }
    #[test]
    fn cycle_and_missing_ids_are_rejected_without_mutation() {
        let db = json!({"items":[{"id":1,"status":"todo"},{"id":2,"wait_on":[3]},{"id":3,"wait_on":[1]}]});
        assert!(validate(&db, 1, &[2]).is_err());
        assert!(validate(&db, 1, &[1]).is_err());
        assert!(validate(&db, 1, &[99]).is_err());
        assert!(validate(&db, 2, &[3]).is_ok());
    }
    #[test]
    fn all_targets_must_finish_and_owner_block_is_preserved() {
        let mut db = json!({"items":[{"id":1,"status":"wait","wait_on":[2,3],"jobs":["old"],"spec":"saved"},
            {"id":2,"status":"done"},{"id":3,"status":"block"},
            {"id":4,"status":"block","wait_on":[2]}]});
        assert!(waiting(&db, &db["items"][0]));
        assert!(release_ready(&mut db).is_empty());
        assert_eq!(db["items"][3]["status"], "block");
        db["items"][2]["status"] = json!("done");
        assert_eq!(release_ready(&mut db), vec![1]);
        assert_eq!(db["items"][0]["status"], "todo");
        assert_eq!(db["items"][0]["jobs"], json!(["old"]));
        assert_eq!(db["items"][0]["spec"], "saved");
        assert!(release_ready(&mut db).is_empty());
    }
    #[test]
    fn missing_target_stays_waiting_even_if_status_was_overwritten() {
        let db = json!({"items":[{"id":1,"status":"todo","wait_on":[99],"jobs":["j"]}]});
        assert!(waiting(&db, &db["items"][0]));
        assert_eq!(job_pending_on(&db, &json!({"id":"j"})), vec![99]);
        assert_eq!(job_pending_on(&db, &json!({"todo_id":1})), vec![99]);
        assert!(job_pending_on(&db, &json!({"todo_id":2})).is_empty());
    }
    #[test]
    fn every_release_has_a_new_generation_and_preserves_owner_stop() {
        let mut db =
            json!({"items":[{"id":1,"status":"wait","wait_on":[2]},{"id":2,"status":"done"}]});
        assert_eq!(release_ready(&mut db), vec![1]);
        assert_eq!(db["items"][0]["dependency_generation"], 1);
        db["items"][0]["status"] = json!("wait");
        db["items"][0]["wait_on"] = json!([2]);
        assert_eq!(release_ready(&mut db), vec![1]);
        assert_eq!(db["items"][0]["dependency_generation"], 2);
        db["items"][0]["status"] = json!("block");
        db["items"][0]["wait_on"] = json!([2]);
        assert!(release_ready(&mut db).is_empty());
        assert_eq!(db["items"][0]["status"], "block");
    }
}
