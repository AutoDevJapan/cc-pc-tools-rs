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

/// Authorize one original receipt, then let the scheduler prepare and admit it.
pub fn bind_review(db: &mut Value, receipt: &Value, owner: &Value) -> Result<bool> {
    bind_review_at(db, receipt, owner, crate::time_s())
}

/// Check an existing allocation against the supplied observation time.
pub fn bind_review_at(
    db: &mut Value,
    receipt: &Value,
    owner: &Value,
    at: f64,
) -> Result<bool> {
    use cc_scheduler::product_review as scope;
    let row = db["items"]
        .as_array()
        .and_then(|a| a.iter().find(|i| i["id"] == 2407))
        .ok_or("元のレビューがありません")?;
    let parent = db["items"]
        .as_array()
        .and_then(|a| a.iter().find(|i| i["id"] == 1626))
        .ok_or("元の主担当がありません")?;
    if receipt["verified"] != true
        || receipt["id"] != 2407
        || receipt["snapshot"] != snapshot(db, row)
        || receipt["request_sha256"] != scope::HASH
        || receipt["job"] != scope::ORIGIN
        || row["status"] != "todo"
        || row["subwork"]["parent_todo_id"] != 1626
        || row["subwork"]["origin_job_id"] != scope::ORIGIN
        || row["subwork"]["kind"] != "review"
        || row["project"] != "PhosGaea-Vol1"
        || parent["status"] != "wait"
        || !parent["wait_on"]
            .as_array()
            .is_some_and(|a| a.contains(&json!(2407)))
        || !parent["jobs"]
            .as_array()
            .is_some_and(|a| a.contains(&json!(scope::ORIGIN)))
        || cc_job_policy::todo_wait::waiting(db, row)
        || row["needs"].as_array().is_some_and(|a| !a.is_empty())
        || (!row["needs"].is_null() && !row["needs"].is_array())
        || row["jobs"].as_array().is_some_and(|a| !a.is_empty())
        || !row["scheduler"]["job"].is_null()
        || row["scheduler"]["key"] != scope::KEY
        || cc_job_policy::cancellation::owner_stopped(row, &Value::Null)
        || !row["scheduler"]["owner_stop_job"].is_null()
        || !row["decision_wait"].is_null()
        || row["owner_postponement_recorded"] == true
        || row["stop_kind"].as_str().is_some_and(|s| !s.is_empty())
        || owner["repository"] != "autodevjapan/phosgaea-vol1"
        || owner["worktree"] != row["subwork"]["owner"]["worktree"]
        || owner["branch"].as_str().is_none_or(str::is_empty)
        || owner["git_common"].as_str().is_none_or(str::is_empty)
    {
        return Err("元の資格・停止・依存・割当の照合に失敗しました".into());
    }
    if row["product_review_allocation"].is_object() {
        if scope::eligible(row, at) {
            return Ok(false);
        }
        return Err("保存済みの割当が変わっています".into());
    }
    if !row["scheduler"]["worktree"].is_null() || !row["scheduler"]["subwork_admission"].is_null() {
        return Err("既存の準備を上書きできません".into());
    }
    let before = row.clone();
    let row = crate::find(db, 2407)?;
    row["dispatchable"] = json!(true);
    row["needs"] = json!([]);
    row["subwork"]["owner"] = owner.clone();
    row["subwork"]["owner"]["pr"] = json!(24);
    row["scheduler"]["worktree"] = json!(scope::TREE);
    row["scheduler"]["branch"] = json!(scope::BRANCH);
    let binding = scope::binding(row);
    row["product_review_allocation"] = json!({"before":before,"verification":receipt,
        "binding":binding,"authorized_by":"owner-direct-20261013-todo2539", "at":now()});
    row["updated"] = json!(now());
    Ok(true)
}

pub fn allocate_review(paths: &Paths, apply: bool) -> Result<Value> {
    use cc_scheduler::{common::s, engine::Scheduler, product_review as scope};
    if paths.store != Paths::production().store
        || paths.jobs != Paths::production().jobs
        || !scope::in_scope(crate::time_s())
    {
        return Err("指定した期限内の元記録だけが対象です".into());
    }
    let mut command = Command::new("D:/CC/Tools/client-records/scrub391.exe");
    command.args(["--verify-product-resume", "2407"]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let output = command
        .output()
        .map_err(|_| "正式な保護検証を実行できません")?;
    let receipt: Value =
        serde_json::from_slice(&output.stdout).map_err(|_| "正式な検証結果が不正です")?;
    if !output.status.success() || receipt["verified"] != true {
        return Err("元のレビュー資格を確認できません".into());
    }
    let root = PathBuf::from("D:/CC/Tools/scheduler");
    // Same lock order as the resident scheduler. A legacy scheduler cannot
    // observe dispatchable=true before the fixed checkout exists.
    let _scheduler_lock = cc_scheduler::common::ByteLock::acquire(
        &root.join("scheduler.lock"),
        Duration::from_secs(30),
    )
    .map_err(|e| e.0.to_owned())?
    .ok_or("schedulerの実行中処理を待っています")?;
    let _lock = Lock::acquire(&paths.lock, Duration::from_secs(30))?;
    let mut backend = RealBackend::new(root.clone());
    let jobs = backend.jobs().map_err(|_| "担当記録を確認できません")?;
    if jobs.iter().any(|j| {
        j["todo_id"] == 2407
            || j["launch_key"] == scope::KEY
            || j["related_todo_ids"]
                .as_array()
                .is_some_and(|a| a.contains(&json!(2407)))
    }) {
        return Err("元レビューには既に担当登録があるため追加起動しません".into());
    }
    let owner = cc_job_policy::work_owner::checkout(scope::SOURCE);
    let registered = cc_job_policy::work_owner::checkout(scope::ROOT);
    if owner["repository"] != registered["repository"]
        || owner["git_common"] != registered["git_common"]
    {
        return Err("実際のGit管理先が一致しません".into());
    }
    let mut git = Command::new("git");
    git.args(["-C", scope::SOURCE, "rev-parse", "HEAD"]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        git.creation_flags(0x08000000);
    }
    let head = git.output().map_err(|_| "元のHEADを確認できません")?;
    if !head.status.success() || String::from_utf8_lossy(&head.stdout).trim() != scope::HEAD {
        return Err("元の固定HEADが変わっています".into());
    }
    let mut db = load(&paths.store)?;
    let row = db["items"]
        .as_array()
        .unwrap()
        .iter()
        .find(|i| i["id"] == 2407)
        .ok_or("元レビューがありません")?;
    // The current saved copy must still have the exact original bytes.
    let mut spec_guard = Command::new("D:/CC/Tools/client-records/scrub391.exe");
    spec_guard.args(["--guard-path", s(&row["spec"])]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        spec_guard.creation_flags(0x08000000);
    }
    let out = spec_guard
        .output()
        .map_err(|_| "保存仕様の検証を実行できません")?;
    let result: Value =
        serde_json::from_slice(&out.stdout).map_err(|_| "保存仕様の検証が不正です")?;
    if !out.status.success()
        || result["ok"] != true
        || result["client_resource"] != false
        || result["classification_unverified"] != false
    {
        return Err("保存仕様を確認できません".into());
    }
    let changed = bind_review(&mut db, &receipt, &owner)?;
    if !apply {
        return Ok(json!({"id":2407,"qualified":true,"saved":false,"launched":0}));
    }
    if changed {
        crate::find(&mut db, 2407)?["dispatchable"] = json!(false);
        save(&paths.store, &db)?;
    }
    drop(_lock);
    let item = db["items"]
        .as_array()
        .unwrap()
        .iter()
        .find(|i| i["id"] == 2407)
        .unwrap();
    if let Err(e) = backend.worktree(scope::ROOT, item) {
        return Ok(json!({"id":2407,"saved":changed,"launched":null,"failure":e.0}));
    }
    let mut engine =
        Scheduler::new(root, paths.store.clone(), backend).map_err(|e| e.0.to_owned())?;
    engine.state = cc_scheduler::common::read_json(&engine.root.join("state.json"), json!({}))
        .map_err(|e| e.0.to_owned())?;
    let (cfg, pri) = engine.settings().map_err(|e| e.0.to_owned())?;
    let expected = item.clone();
    let enabled = engine
        .edit(&json!(2407), |row| {
            if *row == expected {
                row["dispatchable"] = json!(true);
            }
        })
        .map_err(|e| e.0.to_owned())?;
    if enabled["dispatchable"] != true {
        return Err("照合中に元記録が変わりました".into());
    }
    let dispatch = engine.dispatch_for(&cfg, &pri, Some(2407));
    if !engine.report["launched"].is_object() {
        // Only this CLI retries it; old resident binaries never receive an
        // unlaunched dispatchable product item, even if the registry returns.
        engine
            .edit(&json!(2407), |row| {
                if row["status"] == "todo"
                    && row["product_review_allocation"] == enabled["product_review_allocation"]
                {
                    row["dispatchable"] = json!(false);
                }
            })
            .map_err(|e| e.0.to_owned())?;
    }
    engine.persist().map_err(|e| e.0.to_owned())?;
    Ok(
        json!({"id":2407,"saved":changed,"launched":engine.report["launched"],
        "allowance_now":engine.report["allowance_now"],"priority_wait":engine.report["priority_wait"],
        "capacity":engine.report["reasons"],"failure":dispatch.err().map(|e|e.0)}),
    )
}
