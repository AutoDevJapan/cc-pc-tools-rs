//! Finite qualification of the two original products, not automatic dispatch.
use crate::{Lock, Paths, Result, load, now, save};
use cc_scheduler::{
    backend::{Backend, RealBackend},
    policy,
};
use serde_json::{Value, json};
use std::{path::PathBuf, process::Command, time::Duration};

pub fn snapshot(db: &Value, item: &Value) -> Value {
    json!({"id":item["id"],"project":item["project"],
        "registered_path":db["projects"][item["project"].as_str().unwrap_or("")],
        "status":item["status"],"dispatchable":item["dispatchable"],"spec":item["spec"],
        "jobs":item["jobs"],"wait_on":item["wait_on"],"scheduler_job":item["scheduler"]["job"],
        "worktree":item["scheduler"]["worktree"],"resume_session":item["scheduler"]["resume_session"],
        "verified_status":item["recovery"]["verified_status"],
        "hold_reason":item["recovery"]["automation_hold_reason"],
        "request_hash":item["recovery"]["preserved_worker_request_sha256"]})
}

/// The verified receipt is bound to current original values under todo.lock.
/// Preserve every original field, including stops, dependencies, history and spec.
pub fn qualify(db: &mut Value, id: i64, receipt: &Value, apply: bool) -> Result<Value> {
    if ![1626, 2407, 2475].contains(&id) || receipt["id"] != id || receipt["verified"] != true {
        return Err("元の対象と保護判定を確認できません".into());
    }
    let item = db["items"]
        .as_array()
        .and_then(|rows| rows.iter().find(|i| i["id"] == id))
        .ok_or("元のToDoがありません")?;
    if snapshot(db, item) != receipt["snapshot"] {
        return Err("照合中に元記録が変わりました。再検証が必要です".into());
    }
    if item["status"] == "done"
        || cc_job_policy::cancellation::owner_stopped(item, &Value::Null)
        || !item["scheduler"]["owner_stop_job"].is_null()
        || item["status"] == "block"
        || !item["decision_wait"].is_null()
        || item["owner_postponement_recorded"] == true
        || item["stop_kind"].as_str().is_some_and(|s| !s.is_empty())
    {
        return Err("完了または停止の記録を維持します".into());
    }
    let pending = cc_job_policy::todo_wait::pending_on(db, item);
    let target_status = if item["status"] == "recovery_unverified" {
        match item["recovery"]["verified_status"].as_str() {
            Some("wait") if !pending.is_empty() => "wait",
            Some("todo") if pending.is_empty() => "todo",
            _ => return Err("元の停止・依存の状態が未確認です".into()),
        }
    } else {
        item["status"]
            .as_str()
            .filter(|s| ["todo", "doing", "wait"].contains(s))
            .ok_or("元の状態が未確認です")?
    }
    .to_owned();
    let evidence = json!({"receipt":receipt,"qualified_at":now(),"status_after":target_status,
        "dispatchable_after":item["dispatchable"],"pending_on":pending});
    let repeated = item["product_resume_qualification"]["receipt"]["request_sha256"]
        == receipt["request_sha256"]
        && item["product_resume_qualification"]["receipt"]["session"] == receipt["session"]
        && item["product_resume_qualification"]["receipt"]["source_session"]
            == receipt["source_session"]
        && item["product_resume_qualification"]["receipt"]["worktree"] == receipt["worktree"]
        && item["product_resume_qualification"]["receipt"]["job"] == receipt["job"]
        && item["product_resume_qualification"]["status_after"] == target_status
        && item["product_resume_qualification"]["pending_on"] == json!(pending);
    if apply && !repeated {
        let mut before = item.clone();
        // Keep the complete original values without recursively copying history.
        before
            .as_object_mut()
            .unwrap()
            .remove("product_resume_history");
        let row = crate::find(db, id)?;
        if !row["product_resume_history"].is_array() {
            row["product_resume_history"] = json!([]);
        }
        row["product_resume_history"]
            .as_array_mut()
            .unwrap()
            .push(json!({"before":before,"verification":evidence}));
        row["product_resume_qualification"] = evidence;
        row["status"] = json!(target_status);
        row["updated"] = json!(now());
    }
    Ok(
        json!({"id":id,"qualified":true,"saved":apply && !repeated,"launched":0,
        "status":target_status,"pending_on":pending,"worktree":receipt["worktree"],
        "session":receipt["session"],"job":receipt["job"],"request":receipt["request"],
        "request_sha256":receipt["request_sha256"],"automatic_dispatch_changed":false}),
    )
}

pub fn run(paths: &Paths, id: i64, apply: bool) -> Result<Value> {
    if ![1626, 2407, 2475].contains(&id)
        || paths.store != Paths::production().store
        || paths.jobs != Paths::production().jobs
    {
        return Err("この検証は指定された元の記録だけを扱います".into());
    }
    let mut command = Command::new("D:/CC/Tools/client-records/scrub391.exe");
    command.args(["--verify-product-resume", &id.to_string()]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let output = command
        .output()
        .map_err(|_| "正式な保護判定CLIを実行できません")?;
    let receipt: Value =
        serde_json::from_slice(&output.stdout).map_err(|_| "保護判定の結果が不正です")?;
    if !output.status.success() || receipt["verified"] != true {
        return Ok(json!({"id":id,"qualified":false,"saved":false,"launched":0,
            "reason":receipt["reason"].as_str().unwrap_or("guard_unavailable")}));
    }
    let _lock = Lock::acquire(&paths.lock, Duration::from_secs(30))?;
    let mut backend = RealBackend::new(PathBuf::from("D:/CC/Tools/scheduler"));
    let ps = backend
        .snapshot()
        .map_err(|_| "プロセスの識別を確認できません")?;
    let mut db = load(&paths.store)?;
    let row = db["items"]
        .as_array()
        .and_then(|rows| rows.iter().find(|i| i["id"] == id))
        .ok_or("元のToDoがありません")?;
    for jid in crate::job_refs::ids(&row["jobs"])
        .chain(row["scheduler"]["job"].as_str())
        .chain(receipt["job"].as_str())
    {
        if !crate::job_refs::valid_id(jid) {
            return Err("担当記録の識別子が不正です".into());
        }
        let file = paths.jobs.join(jid).join("job.json");
        if !file.try_exists().map_err(|_| "担当記録を確認できません")? {
            continue;
        }
        let job = load(&file)?;
        if job["id"] != jid
            || policy::alive(&job, &ps, crate::time_s())
            || (receipt["job"] == jid && cc_job_policy::cancellation::owner_stopped(row, &job))
        {
            return Err("稼働中または停止済みの元担当を維持します".into());
        }
    }
    let report = qualify(&mut db, id, &receipt, apply)?;
    if report["saved"] == true {
        save(&paths.store, &db)?;
    }
    Ok(report)
}
