use crate::preparation::{failure_reason, owns_workspace};
use crate::{
    backend::{Backend, safe_text, valid_effort, valid_model},
    common::*,
    policy::*,
};
use cc_job_policy::{cancellation, todo_wait};
use crate::admission::{Observation, Reason as AdmissionReason, Stage as AdmissionStage};
fn todo_store_error(error: cc_job_policy::todo_store::Error) -> Error {
    match error {
        cc_job_policy::todo_store::Error::TooLarge => Error("todo_store_too_large: limit 64 MiB"),
        cc_job_policy::todo_store::Error::InvalidJson(_) => Error("todo_store_invalid_json"),
        cc_job_policy::todo_store::Error::Io(e) => e.into(),
    }
}
fn invalid_subwork(error: &str, stage: &'static str) -> Error {
    // Emit only fixed classifications. Never print a store value or an
    // unrecognized error, and preserve the caller's existing failure code.
    let reason = match error {
        "subwork ToDo is missing" => "parent_missing",
        "invalid subwork relation" => "relation_invalid",
        "invalid or cyclic subwork parent" => "parent_invalid",
        "subwork and parent must belong to the same repository and Git common directory" => {
            "repository_mismatch"
        }
        "subwork names a different PR" => "pr_mismatch",
        "independent subwork cannot depend on its parent completion" => "dependency_cycle",
        "subwork requires its own scheduler-owned worktree" => "checkout_not_independent",
        "subwork checkout is missing" => "checkout_missing",
        "parent has more than one active root worker" => "multiple_live_parents",
        _ => "validation_failed",
    };
    eprintln!("subwork validation failed stage={stage} reason={reason}");
    Error("InvalidSubwork")
}
use serde_json::{Value, json};
use std::{
    fs,
    path::{Path, PathBuf},
    time::Duration,
};
fn launch_inputs(item: &Value) -> Value {
    crate::admission::inputs::launch(item)
}

fn review_request_inputs(item: &Value) -> Value {
    crate::admission::inputs::review(item)
}

fn selection_origin(
    field: &str,
    item: &Value,
    options: &Value,
    source: Option<&Value>,
    selected: &Value,
) -> Value {
    let origin = format!("{field}_origin");
    let matches = |candidate: &Value| {
        if field == "model" {
            cc_ask::job_policy::same_model(s(&candidate[field]), s(&selected[field]))
        } else {
            candidate[field] == selected[field]
        }
    };
    for candidate in [item, options] {
        if matches(candidate) && candidate[&origin] == "owner_request" {
            return json!("owner_request");
        }
    }
    if [item, options]
        .iter()
        .any(|candidate| matches(candidate) && candidate.get(&origin).is_some())
    {
        return json!("policy");
    }
    if source.is_some_and(|source| matches(source) && source[&origin] == "owner_request") {
        json!("owner_request")
    } else {
        json!("policy")
    }
}
/// A running (`doing`) ToDo that was told to wait while its job was finishing must stay waiting: the
/// job's result must not mark it done/block/todo. Callers only reach this for `doing` rows, so owner
/// `block`/`todo` rows are never converted. The existing note is kept (the wait is reported via
/// `wait_on` and the scheduler report); the job stays in the history.
fn park_waiting(row: &mut Value, jid: Option<&Value>) -> bool {
    let ids = arr(&row["wait_on"]);
    if ids.is_empty() {
        return false;
    }
    row["status"] = json!("wait");
    if !row["scheduler"].is_object() {
        row["scheduler"] = json!({});
    }
    let m = &mut row["scheduler"];
    if let Some(jid) = jid {
        m["last_job"] = jid.clone();
    }
    merge(
        m,
        json!({"job":null,"key":null,"phase":"waiting","resume":true}),
    );
    remove(m, "pending_cancel");
    remove(m, "notice_pending");
    true
}
#[path = "instructions.rs"]
mod instructions;
pub use instructions::*;
#[path = "product_result_repair.rs"]
mod product_result_repair;
pub struct Scheduler<B: Backend> {
    pub root: PathBuf,
    pub todo_path: PathBuf,
    pub backend: B,
    pub state: Value,
    pub report: Value,
    pub failures: Vec<String>,
    admission_updates: Option<std::collections::BTreeMap<i64, String>>,
}
impl<B: Backend> Scheduler<B> {
    pub fn civ6_home(&self) -> PathBuf {
        self.root.parent().unwrap_or(&self.root).join("civ6-live")
    }
    pub fn new(root: PathBuf, todo_path: PathBuf, backend: B) -> Result<Self> {
        fs::create_dir_all(&root)?;
        backup(&root.join("scheduler.log"))?;
        Ok(Self {
            root,
            todo_path,
            backend,
            state: json!({}),
            report: json!({}),
            failures: vec![],
            admission_updates: None,
        })
    }
    pub fn log(&self, level: &str, message: &str) -> Result<()> {
        use std::io::Write;
        let p = self.root.join("scheduler.log");
        if p.metadata()
            .map(|m| m.len() >= 2 * 1024 * 1024)
            .unwrap_or(false)
        {
            for i in (1..5).rev() {
                let src = PathBuf::from(format!("{}.{}", p.display(), i));
                if src.exists() {
                    let dst = PathBuf::from(format!("{}.{}", p.display(), i + 1));
                    if dst.exists() {
                        fs::remove_file(&dst)?;
                    }
                    fs::rename(src, dst)?;
                }
            }
            let first = PathBuf::from(format!("{}.1", p.display()));
            if first.exists() {
                fs::remove_file(&first)?;
            }
            fs::rename(&p, first)?;
        }
        let mut f = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(p)?;
        writeln!(
            f,
            "{} {} {}",
            chrono::Local::now().format("%Y-%m-%d %H:%M:%S,%3f"),
            level,
            message
        )?;
        Ok(())
    }
    pub fn guard<T>(
        &mut self,
        name: &str,
        action: impl FnOnce(&mut Self) -> Result<T>,
    ) -> Option<T> {
        match action(self) {
            Ok(v) => Some(v),
            Err(e) => {
                self.failures.push(name.into());
                let _ = self.log("ERROR", &format!("{name} failed ({})", e.0));
                None
            }
        }
    }
    pub fn settings(&self) -> Result<(Value, Value)> {
        let mut cfg = defaults();
        let configured = read_json(&self.root.join("config.json"), json!({}))?;
        if !configured.is_object() {
            return Err(Error("TypeError"));
        }
        merge(&mut cfg, configured);
        for (key, _) in defaults().as_object().unwrap() {
            let v = &cfg[key];
            let valid = match key.as_str() {
                "dispatch_enabled" | "kill_enabled" => v.is_boolean(),
                "hard_cap" => v.is_null() || v.as_u64().is_some_and(|v| v >= 1),
                _ => v.is_number() && n(v).is_finite() && n(v) > 0.,
            };
            if !valid {
                return Err(Error("ValueError"));
            }
        }
        if n(&cfg["launch_interval_s"]) < 10.
            || n(&cfg["orphan_age_s"]) < 600.
            || n(&cfg["commit_limit_percent"]) > 85.
            || n(&cfg["cpu_limit_percent"]) > 80.
            || n(&cfg["reserve_gb"]) < 16.
            || n(&cfg["disk_min_gb"]) < 30.
        {
            return Err(Error("ValueError"));
        }
        let mut pri = json!({});
        let configured = read_json(&self.root.join("priorities.json"), json!({}))?;
        if !configured.is_object() {
            return Err(Error("TypeError"));
        }
        merge(&mut pri, configured);
        if pri
            .as_object()
            .unwrap()
            .values()
            .any(|v| !v.is_object() && v.as_u64().is_none_or(|x| x < 1))
        {
            return Err(Error("ValueError"));
        }
        Ok((cfg, pri))
    }
    pub fn todo(&self) -> Result<Value> {
        match cc_job_policy::todo_store::read(&self.todo_path) {
            Ok(db) => Ok(db),
            Err(cc_job_policy::todo_store::Error::Io(e))
                if e.kind() == std::io::ErrorKind::NotFound =>
            {
                Ok(json!({"projects":{},"items":[],"next_id":1,"stalls":{}}))
            }
            Err(e) => Err(todo_store_error(e)),
        }
    }
    pub fn edit(&mut self, id: &Value, action: impl FnOnce(&mut Value)) -> Result<Value> {
        self.edit_in_project(id, None, action)
    }
    /// Bind an explicit workspace receipt to its registered project under the store lock.
    pub fn edit_in_project(
        &mut self,
        id: &Value,
        expected_path: Option<&str>,
        action: impl FnOnce(&mut Value),
    ) -> Result<Value> {
        let _lock = ByteLock::acquire(
            &self.todo_path.with_file_name("todo.lock"),
            Duration::from_secs(2),
        )?
        .ok_or(Error("TimeoutError"))?;
        let mut db = self.todo()?;
        if let Some(path) = expected_path {
            let current = arr(&db["items"])
                .into_iter()
                .find(|row| row["id"] == *id)
                .ok_or(Error("ValueError"))?;
            if db["projects"][s(&current["project"])] != path {
                return Err(Error("InputRejected"));
            }
        }
        let row = db["items"]
            .as_array_mut()
            .ok_or(Error("ValueError"))?
            .iter_mut()
            .find(|row| row["id"] == *id)
            .ok_or(Error("ValueError"))?;
        let before = row.clone();
        action(row);
        if *row == before {
            return Ok(before);
        }
        row["updated"] = json!(chrono::Local::now().format("%Y-%m-%d %H:%M").to_string());
        let out = row.clone();
        cc_job_policy::todo_store::check_size(encode(&db, 1).len()).map_err(todo_store_error)?;
        write_json(&self.todo_path, &db, 1)?;
        // Capture only the control projection after a successful normal write,
        // while this same todo.lock still pins the written row and root.
        if let Some(updates) = self.admission_updates.as_mut() {
            if let Some(id) = out["id"].as_i64() {
                if updates.len() < crate::admission::MAX_ENTRIES || updates.contains_key(&id) {
                    updates.insert(id, crate::admission::result_fingerprint(&out, &db["projects"][s(&out["project"])]));
                }
            }
        }
        Ok(out)
    }
    pub fn sample(&mut self, cfg: &Value) -> Result<(Value, Processes, Vec<Value>)> {
        let (m, ps) = self.backend.measure(&mut self.state)?;
        if truth(&self.state["boot_s"]) && (n(&m["boot_s"]) - n(&self.state["boot_s"])).abs() > 5. {
            for k in ["cpu", "footprints", "progress", "observed"] {
                remove(&mut self.state, k);
            }
        }
        self.state["boot_s"] = m["boot_s"].clone();
        let mut jobs = self.backend.jobs()?;
        cc_job_policy::subwork::mark_live(&mut jobs, |j| {
            ["pid", "codex_pid"].iter().any(|key| matches(j, key, &ps))
        });
        let now = self.backend.clock();
        self.state["observed"] = ownership(&jobs, &ps, &self.state["observed"]);
        merge(
            &mut self.report,
            capacity(&m, cfg, &mut self.state, &jobs, &ps, now),
        );
        let db = self.todo()?;
        let busy =
            resources_busy_with_lock(&arr(&db["items"]), &jobs, &ps, now, &self.civ6_home())?;
        merge(
            &mut self.report,
            json!({"queue_length":arr(&db["items"]).iter().filter(|i|s(&i["status"])=="todo"&&truth(&i["dispatchable"])&&truth(&i["spec"])&&!todo_wait::waiting(&db,i)).count(),"busy_resources":busy}),
        );
        // Name the prerequisites being waited for ("ToDo 2005 待ち"), with the waiting item separately.
        let waiting: Vec<Value> = arr(&db["items"])
            .iter()
            .filter(|i| {
                s(&i["status"]) != "done"
                    && (todo_wait::waiting(&db, i) || !i["wait_ready"].is_null())
            })
            .map(|i| {
                let pending = todo_wait::pending_on(&db, i);
                let mut message: Vec<String> =
                    pending.iter().map(|id| format!("ToDo {id} 待ち")).collect();
                if !i["wait_ready"].is_null() {
                    message.push(todo_wait::ready_message(&i["wait_ready"]));
                }
                json!({"item":i["id"],"wait_on":pending,"message":message.join(", ")})
            })
            .collect();
        if !waiting.is_empty() {
            self.report["waiting"] = json!(waiting);
        }
        Ok((m, ps, jobs))
    }
    pub fn persist(&mut self) -> Result<()> {
        write_json(&self.root.join("state.json"), &self.state, 2)?;
        self.report["step_failures"] = json!(self.failures);
        write_json(&self.root.join("status.json"), &self.report, 2)
    }
    pub fn link(&mut self, id: &Value, j: &Value, key: &Value) -> Result<()> {
        let start = epoch(if truth(&j["started_at"]) {
            &j["started_at"]
        } else {
            &j["created_at"]
        });
        let start = if start != 0. {
            start
        } else {
            self.backend.clock()
        };
        self.edit(id, |i| {
            if !matches!(s(&i["status"]), "todo" | "doing")
                || i["scheduler"]["key"] != *key
                || truth(&i["scheduler"]["job"])
            {
                return;
            }
            i["status"] = json!("doing");
            if i["subwork"].is_object() {
                assign_worker(i, s(&j["id"]));
            }
            remove(i, "dependency_ready");
            if !arr(&i["jobs"]).contains(&j["id"]) {
                push(&mut i["jobs"], j["id"].clone());
            }
            merge(
                &mut i["scheduler"],
                json!({"job":j["id"],"phase":"running","started_s":start}),
            );
            if i["scheduler"]["review_preparation_failure"].is_object() {
                let previous = i["scheduler"]["review_preparation_failure"].clone();
                push(&mut i["scheduler"]["review_preparation_history"], previous);
                remove(&mut i["scheduler"], "review_preparation_failure");
            }
        })?;
        Ok(())
    }
    pub fn requeue(&mut self, id: &Value, jid: &Value, reason: &str) -> Result<()> {
        self.requeue_if(id, jid, reason, None)
    }
    pub fn requeue_if(
        &mut self,
        id: &Value,
        jid: &Value,
        reason: &str,
        expected: Option<&Value>,
    ) -> Result<()> {
        let now = self.backend.clock();
        let mut applied = false;
        self.edit(id, |i| {
            if expected.is_some_and(|expected| expected != i) || cancellation::owner_stopped(i, &Value::Null) { return; }
            if s(&i["status"]) != "doing" || i["scheduler"]["job"] != *jid {
                return;
            }
            if park_waiting(i, Some(jid)) {
                return;
            }
            let budget = i["scheduler"]["retry_count"].clone();
            let delay = if reason == "stale_review_spec" { 0. } else if reason == "resource_wait" { 3600. } else if reason == "memory" { 180. } else if n(&budget) >= 1. {
                crate::recovery_failure::SLOW_RETRY_S
            } else { crate::recovery_failure::FAST_RETRY_S };
            i["status"] = json!("todo");
            let message = match reason {
                "no_action" => "30分間実操作がなく報告だけが続いたため担当を復旧します",
                "hung" => "30分間進捗がなかったため停止しました",
                "lost" => "担当ジョブのプロセスが消失しました",
                "memory" => "空きメモリ不足で停止しました",
                "failed" => "担当ジョブが失敗しました",
                "resource_wait" => "指定モデルの利用枠を待っています",
                "stale_review_spec" => "レビュー中に仕様が更新されたため、旧結果を保管して新しい仕様から再開します",
                _ => "担当ジョブが停止しました",
            };
            if reason == "stale_review_spec" {
                i["note"] = json!(format!("{}\n{message}。", s(&i["note"])));
                record_result(i, jid, "stale_review_spec");
            } else {
                i["note"] = json!(format!("{message}。原因を引き継ぎ、資源の空きを確認して自動で再開します。"));
            }
            if reason == "resource_wait" { record_result(i, jid, "resource_wait"); }
            let until = n(&i["scheduler"]["not_before"]).max(n(&i["scheduler"]["resource_wait"]["until"])).max(now+delay);
            let m = &mut i["scheduler"];
            merge(m, json!({"last_job":jid,"last_stop":reason,"job":null,"key":null,"phase":"retry","resume":true,"not_before":until}));
            remove(m, "pending_cancel");
            remove(m, "notice_pending");
            if reason != "memory" && reason != "resource_wait" && reason != "stale_review_spec" {
                m["retry_count"] = add_count(&budget, 1);
            }
            applied = true;
        })?;
        if !applied {
            return Ok(());
        }
        remove(&mut self.state["progress"], s(jid));
        self.log("INFO", &format!("item {id} recovery {reason}"))?;
        Ok(())
    }
    pub fn cancel_for(
        &mut self,
        i: &Value,
        j: &mut Value,
        reason: &str,
        cfg: &Value,
    ) -> Result<()> {
        if !truth(&cfg["kill_enabled"]) {
            push(
                &mut self.report["cancel_dry_run"],
                json!({"item":i["id"],"job":j["id"],"reason":reason}),
            );
            self.log(
                "INFO",
                &format!(
                    "would cancel item {} reason={reason} (killing disabled)",
                    i["id"]
                ),
            )?;
            return Ok(());
        }
        let fresh = self.edit(&i["id"], |row| {
            if s(&row["status"]) == "doing" && row["scheduler"]["job"] == j["id"] {
                row["scheduler"]["pending_cancel"] = json!(reason);
            }
        })?;
        if fresh["scheduler"]["job"] != j["id"]
            || s(&fresh["scheduler"]["pending_cancel"]) != reason
        {
            return Ok(());
        }
        if self.backend.cancel_for_recovery(j)? {
            self.requeue(&i["id"], &j["id"], reason)?;
        } else {
            self.log(
                "WARNING",
                &format!(
                    "cancel incomplete item {}; retained resource reservation",
                    i["id"]
                ),
            )?;
        }
        Ok(())
    }
    pub fn reconcile(&mut self, jobs: &mut [Value], ps: &Processes, cfg: &Value) -> Result<()> {
        let now = self.backend.clock();
        // Recover only the exact holds produced by this scheduler's old retry limit.
        let db = self.todo()?;
        let excluded = fs::read_to_string(
            self.root
                .parent()
                .unwrap_or(&self.root)
                .join("client-projects.txt"),
        )?;
        for i in arr(&db["items"]) {
            if todo_wait::waiting(&db, &i) {
                continue;
            }
            if s(&i["project"]) == "owner"
                || excluded
                    .trim_start_matches('\u{feff}')
                    .lines()
                    .map(str::trim)
                    .filter(|name| !name.is_empty() && !name.starts_with('#'))
                    .any(|name| {
                        s(&i["project"])
                            .to_lowercase()
                            .contains(&name.to_lowercase())
                            || s(&db["projects"][s(&i["project"])])
                                .to_lowercase()
                                .contains(&name.to_lowercase())
                    })
            {
                continue;
            }
            let suffix = "。2回止まったため、オーナーに再開方法を確認します。";
            if matches!(s(&i["status"]), "block" | "doing" | "todo")
                && s(&i["scheduler"]["phase"]) == "blocked"
                && [
                    "ジョブが失敗",
                    "ジョブのプロセスが消失",
                    "30分間進捗がなかったため停止",
                    "ジョブが停止",
                ]
                .iter()
                .any(|prefix| s(&i["note"]) == format!("{prefix}{suffix}"))
            {
                let active_owner = jobs
                    .iter()
                    .filter(|j| {
                        live(j)
                            && alive(j, ps, now)
                            && (i["scheduler"]["job"] == j["id"]
                                || arr(&i["jobs"]).contains(&j["id"]))
                    })
                    .max_by(|a, b| epoch(&a["created_at"]).total_cmp(&epoch(&b["created_at"])))
                    .cloned();
                let response = if truth(&i["scheduler"]["question"])
                    && !truth(&i["scheduler"]["answer"])
                {
                    let Some(reply) = self.guard(&format!("answer item {} legacy", i["id"]), |e| {
                        e.backend.reply(&i["scheduler"]["question"])
                    }) else {
                        continue;
                    };
                    reply
                } else {
                    None
                };
                let stopped = response.as_ref().is_some_and(|reply| truth(&reply["stop"]));
                self.edit(&i["id"], |row| {
                    if row["status"] != i["status"] || row["note"] != i["note"] || row["scheduler"] != i["scheduler"] { return; }
                    row["note"] = json!("失敗回数だけによる保留を解除しました。原因を引き継いで自動で再開します。");
                    if let Some(reply)=&response {
                        row["scheduler"]["answer"]=reply["id"].clone();
                        row["scheduler"]["owner_instruction"]=json!(s(&reply["text"]));
                        row["note"]=json!(format!("オーナーの回答: {}",s(&reply["text"])));
                    }
                    if stopped {
                        row["status"]=json!("block");
                        row["note"]=json!("オーナーの回答に従い、停止したままにします。");
                        if let Some(owner)=&active_owner {
                            row["scheduler"]["owner_stop_job"]=owner["id"].clone();
                        }
                    } else if let Some(owner) = &active_owner {
                        row["status"] = json!("doing");
                        merge(&mut row["scheduler"],json!({"job":owner["id"],"phase":"running"}));
                    } else {
                        row["status"] = json!("todo");
                        let until = n(&row["scheduler"]["not_before"]).max(now+60.);
                        merge(&mut row["scheduler"], json!({"job":null,"key":null,"phase":"retry","resume":true,"not_before":until}));
                    }
                    remove(&mut row["scheduler"], "notice_pending");
                    remove(&mut row["scheduler"], "pending_cancel");
                    if truth(&row["scheduler"]["question"]) {
                        row["scheduler"]["question_close_pending"] = json!(true);
                        row["scheduler"]["retire_retry_question"] = json!(true);
                    }
                })?;
            }
        }
        // Explicit owner stop is distinct from automatic resource-pressure termination.
        // Save its intent first and retry a failed cancellation on the next pass.
        for row in arr(&self.todo()?["items"]) {
            if s(&row["status"]) != "block" || !truth(&row["scheduler"]["owner_stop_job"]) {
                continue;
            }
            let jid = &row["scheduler"]["owner_stop_job"];
            if let Some(job) = jobs.iter_mut().find(|j| j["id"] == *jid && live(j)) {
                let owners = arr(&self.todo()?["items"])
                    .iter()
                    .filter(|i| {
                        i["id"] != row["id"]
                            && s(&i["status"]) != "done"
                            && i["scheduler"]["owner_stop_job"] != *jid
                            && (arr(&i["jobs"]).contains(jid) || i["scheduler"]["job"] == *jid)
                    })
                    .count();
                if owners == 0 {
                    self.guard(&format!("answer item {} stop", row["id"]), |e| {
                        e.backend.cancel(job)
                    });
                }
            } else {
                self.edit(&row["id"], |i| {
                    remove(&mut i["scheduler"], "owner_stop_job")
                })?;
            }
        }
        for j in jobs.iter_mut() {
            if live(j) && !alive(j, ps, now) {
                let name = format!("lost {}", s(&j["id"]));
                self.guard(&name, |e| e.backend.mark_lost(j));
            }
        }
        for i in arr(&self.todo()?["items"]) {
            let m = &i["scheduler"];
            if matches!(s(&i["status"]), "todo" | "doing") && truth(&m["key"]) && !truth(&m["job"])
            {
                if let Some(j) = jobs.iter().find(|j| j["launch_key"] == m["key"]) {
                    self.guard(&format!("adopt item {}", i["id"]), |e| {
                        e.link(&i["id"], j, &m["key"])
                    });
                }
            }
        }
        for i in arr(&self.todo()?["items"]) {
            if i["stall_recovery"]["pending"].is_object() {
                continue;
            }
            if s(&i["status"]) == "doing" && truth(&i["scheduler"]["job"]) {
                self.guard(&format!("reconcile item {}", i["id"]), |e| {
                    e.reconcile_item(&i, jobs, ps, cfg)
                });
            } else if s(&i["status"]) == "doing" && truth(&i["jobs"]) {
                let jid = arr(&i["jobs"]).last().cloned().unwrap();
                if jobs
                    .iter()
                    .any(|j| j["id"] == jid && s(&j["status"]) == "lost")
                    && !truth(&i["spec"])
                {
                    self.guard(&format!("manual recovery {}",i["id"]),|e|e.edit(&i["id"],|row|{if s(&row["status"])=="doing"&&row["jobs"]==i["jobs"]{if park_waiting(row,None){return;}merge(row,json!({"status":"todo","note":"担当ジョブのプロセスが消失しました。指示ファイルがないため、担当者が再開してください。"}));}}));
                }
            }
        }
        Ok(())
    }
    fn current_product_without_registry(&mut self, item: &Value, root: &str) -> bool {
        matches!(std::fs::read_to_string(self.root.parent().unwrap().join("client-projects.txt")),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound)
            && self.backend.current_product_allowed(item, root)
    }
    /// Isolate new, verified PC repairs and current successions from the unavailable private registry and
    /// old question generations. Unknown records retain their original state.
    pub fn reconcile_repairs(
        &mut self,
        jobs: &mut [Value],
        ps: &Processes,
        cfg: &Value,
    ) -> Result<()> {
        let deferred = crate::dispatch_receipt::recover_pending(self, jobs, ps)?;
        self.adopt_repair_dispatches(jobs)?;
        let db = self.todo()?;
        for item in arr(&db["items"]) {
            let project = s(&item["project"]);
            if s(&item["status"]) != "doing"
                || item["scheduler"]["job"]
                    .as_str()
                    .is_none_or(|id| id.is_empty())
                || (truth(&item["scheduler"]["question"]) && !truth(&item["scheduler"]["answer"]))
                || !((["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&project)
                    && self.backend.repair_scope_allowed(s(&db["projects"][project])))
                    || self.current_product_without_registry(&item, s(&db["projects"][project])))
            {
                continue;
            }
            if !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&project)
                && !self.backend.repair_scope_allowed(s(&item["scheduler"]["worktree"]))
            {
                continue;
            }
            self.guard(&format!("reconcile repair {}", item["id"]), |e| {
                e.reconcile_item(&item, jobs, ps, cfg)
            });
        }
        if self.reconcile_product_results(jobs, ps).is_err() {
            self.report["product_results_deferred"] = json!([cc_job_policy::product_retry::ID]);
        }
        self.release_repair_waits_excluding(&deferred)
    }
    // Registration may finish after the launch CLI times out. The repair tick
    // must consume that receipt as well as the general watchdog, otherwise it
    // keeps launching the same saved work while the original result is lost.
    fn adopt_repair_dispatches(&mut self, jobs: &[Value]) -> Result<()> {
        let db = self.todo()?;
        for item in arr(&db["items"]) {
            let project = s(&item["project"]);
            let scheduler = &item["scheduler"];
            let key = s(&scheduler["key"]);
            if !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&project)
                || !matches!(s(&item["status"]), "todo" | "doing")
                || item["dispatchable"] != true
                || !item["subwork"].is_object()
                || key.len() != 32 || !key.bytes().all(|c| c.is_ascii_hexdigit())
                || truth(&scheduler["job"])
                || truth(&scheduler["question"])
                || truth(&scheduler["owner_stop_job"])
                || item["stopped"] == true || item["owner_stopped"] == true
                || s(&item["spec"]).is_empty() || s(&scheduler["worktree"]).is_empty()
                || s(&scheduler["selection"]["model"]).is_empty()
                || item["stall_recovery"]["pending"].is_object()
                || todo_wait::waiting(&db, &item)
            {
                continue;
            }
            let matching = |job: &&Value| {
                job["launch_key"] == scheduler["key"]
                    && job["todo_id"] == item["id"]
                    && job["subwork"] == item["subwork"]
                    && job["subwork_spec"] == item["spec"]
                    && job["assigned_needs"] == item["needs"]
                    && !s(&job["cwd"]).is_empty()
                    && norm(s(&job["cwd"])) == norm(s(&scheduler["worktree"]))
                    && job["model"] == scheduler["selection"]["model"]
                    && job["effort"] == scheduler["selection"]["effort"]
            };
            let live_matches = jobs.iter().filter(matching).filter(|job| live(job)).collect::<Vec<_>>();
            if live_matches.len() > 1 {
                continue;
            }
            let Some(job) = live_matches.first().copied().or_else(|| jobs.iter().rev().find(matching)) else { continue; };
            if result_received(&item, &job["id"]) { continue; }
            if !self.backend.repair_scope_allowed(s(&db["projects"][project]))
                || !self.backend.repair_scope_allowed(s(&scheduler["worktree"]))
            {
                continue;
            }
            let saved = self.edit(&item["id"], |row| {
                // A new instruction, dependency, owner stop or handoff wins.
                if *row != item { return; }
                row["status"] = json!("doing");
                if !arr(&row["jobs"]).contains(&job["id"]) {
                    push(&mut row["jobs"], job["id"].clone());
                }
                merge(&mut row["scheduler"], json!({"job":job["id"],"phase":"running",
                    "started_s":epoch(&job["created_at"])}));
            })?;
            if saved["scheduler"]["job"] == job["id"] {
                push(&mut self.report["repair_dispatches_adopted"], item["id"].clone());
            }
        }
        Ok(())
    }
    /// The repair tick does not run the general watchdog (its private registry
    /// may be unavailable). Catch both newly completed and previously stranded
    /// edges here, without admitting unknown records or replaying old questions.
    pub fn release_repair_waits(&mut self) -> Result<()> {
        self.release_repair_waits_excluding(&[])
    }
    fn release_repair_waits_excluding(&mut self, deferred: &[Value]) -> Result<()> {
        let snapshot = self.todo()?;
        let mut candidates = Vec::new();
        for item in arr(&snapshot["items"]) {
            let project = s(&item["project"]);
            if deferred.contains(&item["id"])
                || item["status"] != "wait"
                || item["dispatchable"] != true
                || !truth(&item["spec"])
                || !item["wait_ready"].is_null()
                || item["wait_on"].as_array().is_none_or(Vec::is_empty)
                || item["wait_on"]
                    .as_array()
                    .is_some_and(|ids| ids.iter().any(|id| id.as_i64().is_none()))
                || todo_wait::waiting(&snapshot, &item)
                || truth(&item["scheduler"]["owner_stop_job"])
                || truth(&item["scheduler"]["question"])
                || truth(&item["scheduler"]["question_close_pending"])
                || item["stall_recovery"]["pending"].is_object()
            {
                continue;
            }
            if !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&project)
                && !self.current_product_without_registry(&item, s(&snapshot["projects"][project]))
            {
                continue;
            }
            candidates.push(item);
        }
        if candidates.is_empty() {
            return Ok(());
        }
        let jobs = self.backend.jobs()?;
        let ps = self.backend.snapshot()?;
        let owned = |item: &Value, jobs: &[Value], ps: &Processes| {
            jobs.iter().any(|job| {
                cc_job_policy::priority::linked(item, job)
                    && (live(job) || ["pid", "codex_pid"].iter().any(|key| matches(job, key, ps)))
            })
        };
        // Skip live owners before invoking external guards, and reuse each
        // path's result only within this tick. No original guard policy changes.
        let mut checked = std::collections::BTreeMap::new();
        let mut verified = Vec::new();
        for item in candidates
            .into_iter()
            .filter(|item| !owned(item, &jobs, &ps))
        {
            let repo = s(&snapshot["projects"][s(&item["project"])]);
            let worktree = &item["scheduler"]["worktree"];
            if repo.is_empty() || (!worktree.is_null() && !worktree.is_string()) {
                continue;
            }
            // An unstarted task has no worktree yet. Normal preparation will
            // create and verify it; an existing worktree must still pass guard.
            if [repo, s(worktree)]
                .into_iter()
                .filter(|p| !p.is_empty())
                .all(|path| {
                    *checked
                        .entry(path.to_string())
                        .or_insert_with(|| self.backend.repair_scope_allowed(path))
                })
            {
                verified.push(item);
            }
        }
        if verified.is_empty() {
            return Ok(());
        }
        // Scope checks can be slow. Refresh ownership before the locked commit,
        // then compare the complete row so a concurrent stop/handoff wins.
        let jobs = self.backend.jobs()?;
        let ps = self.backend.snapshot()?;
        verified.retain(|item| !owned(item, &jobs, &ps));
        let _lock = ByteLock::acquire(
            &self.todo_path.with_file_name("todo.lock"),
            Duration::from_secs(2),
        )?
        .ok_or(Error("TimeoutError"))?;
        let mut db = self.todo()?;
        let projects = db["projects"].clone();
        let released = todo_wait::release_ready_if(&mut db, |item| {
            verified.contains(item)
                && projects[s(&item["project"])] == snapshot["projects"][s(&item["project"])]
        });
        if !released.is_empty() {
            cc_job_policy::todo_store::check_size(encode(&db, 1).len())
                .map_err(todo_store_error)?;
            write_json(&self.todo_path, &db, 1)?;
            self.report["dependencies_released"] = json!(released);
        }
        Ok(())
    }
    pub fn reconcile_item(
        &mut self,
        i: &Value,
        jobs: &mut [Value],
        ps: &Processes,
        cfg: &Value,
    ) -> Result<()> {
        // A concurrent explicit handoff invalidates this reconciliation snapshot.
        let current = arr(&self.todo()?["items"])
            .into_iter()
            .find(|row| row["id"] == i["id"]);
        if current.as_ref().is_none_or(|row| {
            row["status"] != i["status"]
                || row["scheduler"] != i["scheduler"]
                || row["resume_generation"] != i["resume_generation"]
        }) {
            return Ok(());
        }
        let m = &i["scheduler"];
        let job = jobs.iter_mut().find(|j| j["id"] == m["job"]);
        if job.as_ref().is_none_or(|j| !live(j))
            && ps.values().any(|p| {
                matches!(s(&p["name"]).to_lowercase().as_str(), "codex" | "codex.exe")
                    && identity(p).is_some_and(|key| self.state["observed"][key]["job"] == m["job"])
            })
        {
            push(
                &mut self.report["recovery_waiting_for_worker"],
                m["job"].clone(),
            );
            return Ok(());
        }
        let Some(j) = job else {
            return self.requeue(&i["id"], &m["job"], "lost");
        };
        if !live(j) && result_received(i, &j["id"]) {
            return Ok(());
        }
        if matches!(
            s(&j["status"]),
            "lost" | "failed" | "cancelled" | "canceled"
        ) && (cancellation::blocks_recovery(i, j)
            || (truth(&m["question"]) && !truth(&m["answer"]))
            || (truth(&m["answer"])
                && ["停止したまま", "再開しない", "止めておく"]
                    .iter()
                    .any(|text| s(&i["note"]).contains(text)))
            || crate::backend::result_outcome(s(&j["last_message"])) == "block")
        {
            self.edit(&i["id"], |row| {
                if row["status"] != i["status"]
                    || row["scheduler"] != i["scheduler"]
                    || row["resume_generation"] != i["resume_generation"]
                {
                    return;
                }
                if park_waiting(row, Some(&j["id"])) {
                    return;
                }
                row["status"] = json!("block");
                if !truth(&m["question"]) {
                    row["note"] = json!(
                        "担当ジョブが取り消されました。再開する場合は担当者が確認してください。"
                    );
                }
                row["scheduler"]["phase"] = json!("cancelled");
            })?;
            return Ok(());
        }
        // Capacity cooldown belongs to the same saved job/controller. Quota
        // pressure must not cancel it and launch a new job with a fresh budget.
        if j["capacity_wait_pending"] == true && live(j) {
            return Ok(());
        }
        if j["capacity_exhausted"] == true && s(&j["status"]) == "failed" {
            self.edit(&i["id"],|row| {
                if row["scheduler"]["job"]==j["id"] && row["status"]=="doing" {
                    row["scheduler"]["phase"]=json!("capacity_exhausted");
                    row["note"]=json!("モデルの混雑による有限の再試行を終了しました。元担当と作業内容を保持しています。");
                }
            })?;
            return Ok(());
        }
        if j["last_failure_kind"]=="quota_recovery" && j["status"]=="failed" {
            self.edit(&i["id"],|row| {if row["scheduler"]["job"]==j["id"] && row["status"]=="doing" {
                row["scheduler"]["phase"]=json!("quota_recovery_ended");
                row["note"]=json!("利用枠の回復確認を有限回で終了しました。元担当・履歴・作業範囲を保持しています。");
            }})?;return Ok(());
        }
        let saved_until = (i["scheduler"]["resource_wait"]["job"] == j["id"])
            .then(|| i["scheduler"]["resource_wait"]["until"].as_f64())
            .flatten();
        let resource_until = match saved_until {
            Some(until) => Some(until),
            None => self.backend.resource_until(j)?,
        };
        if let Some(until) = resource_until {
            let fresh = self.edit(&i["id"], |row| {
                if row["status"] == "doing" && row["scheduler"] == i["scheduler"] {
                    row["scheduler"]["resource_wait"] =
                        json!({"reason":"http_429","until":until,"job":j["id"]});
                    row["scheduler"]["not_before"] =
                        json!(n(&row["scheduler"]["not_before"]).max(until));
                }
            })?;
            if live(j) {
                self.cancel_for(&fresh, j, "resource_wait", cfg)?;
            } else {
                self.requeue(&i["id"], &j["id"], "resource_wait")?;
            }
            return Ok(());
        }
        if live(j) {
            if truth(&m["pending_cancel"]) {
                return self.cancel_for(i, j, s(&m["pending_cancel"]), cfg);
            }
            let tree = process_tree(j, ps);
            let cpu = if !tree.is_empty()
                && tree
                    .iter()
                    .all(|pid| !ps[pid]["cpu"].is_null() && truth(&ps[pid]["metrics_ok"]))
            {
                Some(tree.iter().map(|pid| n(&ps[pid]["cpu"])).sum())
            } else {
                None
            };
            let mut sig: Vec<String> = tree.iter().filter_map(|pid| identity(&ps[pid])).collect();
            sig.sort();
            let event = self.backend.event_mtime(j)?;
            let (record, hung) = hang_progress(
                &self.state["progress"][s(&j["id"])],
                event,
                cpu,
                &sig.join(","),
                self.backend.clock(),
                n(&cfg["hang_s"]),
            );
            if !self.state["progress"].is_object() {
                self.state["progress"] = json!({});
            }
            self.state["progress"][s(&j["id"])] = record;
            if hung {
                self.cancel_for(i, j, "hung", cfg)?;
            }
        } else if matches!(
            s(&j["status"]),
            "lost" | "cancelled" | "canceled" | "failed"
        ) {
            self.requeue(
                &i["id"],
                &j["id"],
                if truth(&m["pending_cancel"]) {
                    s(&m["pending_cancel"])
                } else {
                    s(&j["status"])
                },
            )?;
        } else if s(&j["status"]) == "succeeded" {
            let child_review = cc_job_policy::review_result::is_finite_review(j);
            if child_review
                && !cc_job_policy::review_result::is_review_subwork(j)
                && !cc_job_policy::review_result::matches_item(j, i)
                && i["status"] == "doing"
                && i["scheduler"]["job"] == j["id"]
                && i["scheduler"]["key"] == j["finite_review"]["launch_key"]
                && i["wait_ready"].is_null()
                && i["owner_question_wait"].is_null()
                && i["decision_wait"].is_null()
                && i["owner_postponement_recorded"] != true
                && !truth(&i["stop_kind"])
                && !cancellation::owner_stopped(i, j)
            {
                return self.requeue_if(&i["id"], &j["id"], "stale_review_spec", Some(i));
            }
            if child_review
                && (!cc_job_policy::review_result::matches_item(j, i)
                    || !i["wait_ready"].is_null()
                    || !i["owner_question_wait"].is_null()
                    || !i["decision_wait"].is_null()
                    || i["owner_postponement_recorded"] == true
                    || i["stop_kind"].as_str().is_some_and(|s| !s.is_empty())
                    || cancellation::owner_stopped(i, j))
            {
                return Ok(());
            }
            if result_received(i, &j["id"]) {
                return Ok(());
            }
            // Legacy finished rows have already consumed this result, even without a ledger.
            if m["phase"] == "finished" {
                self.edit(&i["id"], |row| {
                    if row["status"] == i["status"]
                        && row["scheduler"] == i["scheduler"]
                        && row["resume_generation"] == i["resume_generation"]
                    {
                        record_result(row, &j["id"], "legacy_finished");
                    }
                })?;
                return Ok(());
            }
            let mut outcome = self.backend.outcome(j)?;
            // A shared worker can finish one child while its parent still has work.
            // An explicit per-item completion remains authoritative (the edit below
            // only touches doing items). A job bound to this item may report its result.
            let linked_items = arr(&self.todo()?["items"])
                .into_iter()
                .filter(|row| {
                    row["scheduler"]["job"] == j["id"] || arr(&row["jobs"]).contains(&j["id"])
                })
                .count();
            if linked_items > 1 && j["todo_id"] != i["id"] {
                outcome = "todo".into();
            }
            let next_at = self.backend.clock() + 60.;
            self.edit(&i["id"], |row| {
                // Revalidate under the shared OS lock after the potentially slow backend read.
                // Generation prevents doing -> block -> start (same worker) ABA as well.
                if s(&row["status"]) == "doing"
                    && row["scheduler"] == i["scheduler"]
                    && row["resume_generation"] == i["resume_generation"]
                    && row["wait_on"] == i["wait_on"]
                    && (!child_review || *row == *i)
                    && !result_received(row, &j["id"])
                {
                    record_result(row, &j["id"], &outcome);
                    if child_review && outcome == "done" {
                        row["scheduler"]["review_result_receipt"] = json!({"source_job":j["id"],
                            "spec":row["spec"],"source_finished_at":j["finished_at"],
                            "received_at":next_at-60.,"requested_model":j["model"],"requested_effort":j["effort"],
                            "native_observation_available":j["execution_observed"]["source"]=="native_turn_context",
                            "recorded_message_sha256":cc_ask::job_policy::product_retry::digest(s(&j["last_message"]).as_bytes())});
                    }
                    if park_waiting(row, Some(&j["id"])) {
                        return;
                    }
                    row["status"] = json!(outcome);
                    row["note"] = json!(if outcome == "todo" {
                        "作業が残っています。資源の空きを確認して自動で続けます。"
                    } else if outcome == "done" {
                        "割り当てたジョブが完了しました。"
                    } else {
                        "ジョブの結果に判断が必要です。"
                    });
                    row["scheduler"]["phase"] = json!("finished");
                    if outcome == "todo" {
                        row["scheduler"]["last_job"] = j["id"].clone();
                        row["scheduler"]["job"] = Value::Null;
                        row["scheduler"]["key"] = Value::Null;
                        row["scheduler"]["phase"] = json!("queued");
                        row["scheduler"]["resume"] = json!(true);
                        row["scheduler"]["not_before"] =
                            json!(n(&row["scheduler"]["not_before"]).max(next_at));
                        row["scheduler"]["continuation_count"] =
                            add_count(&row["scheduler"]["continuation_count"], 1);
                    }
                }
            })?;
            remove(&mut self.state["progress"], s(&j["id"]));
        }
        Ok(())
    }
    pub fn memory_pressure(
        &mut self,
        m: &Value,
        jobs: &mut [Value],
        ps: &Processes,
        pri: &Value,
        cfg: &Value,
    ) -> Result<()> {
        if n(&m["available_gb"]) >= 4. {
            return Ok(());
        }
        if let Some(i) = pressure_victim(
            &arr(&self.todo()?["items"]),
            jobs,
            ps,
            pri,
            self.backend.clock(),
        ) {
            if let Some(j) = jobs.iter_mut().find(|j| j["id"] == i["scheduler"]["job"]) {
                self.cancel_for(&i, j, "memory", cfg)?;
            }
        }
        Ok(())
    }
    pub fn orphans(
        &mut self,
        jobs: &[Value],
        ps: &Processes,
        cfg: &Value,
        apply: bool,
    ) -> Result<Value> {
        let now = self.backend.clock();
        let ledger = ownership(jobs, ps, &self.state["observed"]);
        self.state["observed"] = ledger.clone();
        let candidates = orphan_candidates(jobs, ps, &ledger, now, n(&cfg["orphan_age_s"]));
        let before = node_count(ps);
        let mut killed = vec![];
        let after = if apply && truth(&cfg["kill_enabled"]) {
            for c in &candidates {
                let fresh = self.backend.snapshot()?;
                let fresh_jobs = self.backend.jobs()?;
                let verified = orphan_candidates(
                    &fresh_jobs,
                    &fresh,
                    &ownership(&fresh_jobs, &fresh, &ledger),
                    self.backend.clock(),
                    n(&cfg["orphan_age_s"]),
                );
                if verified
                    .iter()
                    .any(|p| p["pid"] == c["pid"] && p["created"] == c["created"])
                    && self.guard(&format!("orphan kill {}", c["pid"]), |e| {
                        e.backend.kill_orphan(c)
                    }) == Some(true)
                {
                    killed.push(c["pid"].clone());
                }
            }
            node_count(&self.backend.snapshot()?)
        } else {
            before
        };
        let report = json!({"measured_at":iso(now),"dry_run":!(apply&&truth(&cfg["kill_enabled"])),"node_before":before,"node_after":after,"candidates":candidates,"killed":killed,"unattributed_nodes":ps.values().filter(|p|matches!(s(&p["name"]).to_lowercase().as_str(),"node"|"node.exe")&&identity(p).is_none_or(|key|ledger.get(key).is_none())).count()});
        self.report["orphans"] = report.clone();
        write_json(&self.root.join("orphans.json"), &report, 2)?;
        self.log(
            "INFO",
            &format!(
                "orphans dry_run={} node={before}->{after} candidates={} killed={}",
                report["dry_run"],
                candidates.len(),
                killed.len()
            ),
        )?;
        Ok(report)
    }
    pub fn notifications(&mut self) -> Result<()> {
        for i in arr(&self.todo()?["items"]) {
            if s(&i["status"]) == "block" && truth(&i["scheduler"]["notice_pending"]) {
                self.guard(&format!("notify item {}", i["id"]), |e| {
                    let receipt = e.backend.notify(&i)?;
                    e.edit(&i["id"], |row| {
                        remove(&mut row["scheduler"], "notice_pending");
                        row["scheduler"]["question"] = receipt;
                    })?;
                    Ok(())
                });
            }
        }
        Ok(())
    }
    pub fn answers(&mut self) -> Result<()> {
        for i in arr(&self.todo()?["items"]) {
            let r = i["scheduler"]["question"].clone();
            if !truth(&r) {
                continue;
            }
            self.guard(&format!("answer item {}",i["id"]),|e|{let mut current=i.clone();if s(&i["status"])=="block"&&!truth(&i["scheduler"]["answer"]){let Some(response)=e.backend.reply(&r)?else{return Ok(());};current=e.edit(&i["id"],|row|{if s(&row["status"])!="block"||row["scheduler"]["question"]!=r||truth(&row["scheduler"]["answer"]){return;}merge(&mut row["scheduler"],json!({"answer":response["id"],"question_close_pending":true}));if truth(&response["stop"]){row["note"]=json!("オーナーの回答に従い、停止したままにします。");}else{merge(row,json!({"status":"todo","note":"オーナーの回答を受け、同じ worktree から再開します。"}));merge(&mut row["scheduler"],json!({"job":null,"key":null,"phase":"retry","resume":true,"retry_count":0,"owner_instruction":format!("原因を調べてから元の依頼を再開してください。\nオーナーの回答: {}",s(&response["text"]))}));remove(&mut row["scheduler"],"selection");}})?;}
if truth(&current["scheduler"]["question_close_pending"]){e.backend.close_question(&r)?;e.edit(&i["id"],|row|{
    if row["scheduler"]["question"] != r { return; }
    remove(&mut row["scheduler"],"question_close_pending");
    if truth(&row["scheduler"]["retire_retry_question"]) {
        remove(&mut row["scheduler"],"question");
        remove(&mut row["scheduler"],"retire_retry_question");
    }
})?;}Ok(())});
        }
        Ok(())
    }
    pub fn prepare(&mut self, i: &Value, repo: &str) -> Result<Option<Value>> {
        self.prepare_with_evidence(i, repo, None, &mut Observation::new(self.backend.clock(), false, true))
    }
    fn prepare_with_evidence(
        &mut self,
        i: &Value,
        repo: &str,
        evidence: Option<&Value>,
        observation: &mut Observation,
    ) -> Result<Option<Value>> {
        if i["product_review_allocation"].is_object() && !self.backend.product_review_allowed(i) {
            return Err(Error("InputRejected"));
        }
        let cooldown = n(&i["scheduler"]["not_before"]) > self.backend.clock();
        if n(&i["scheduler"]["quota_recheck_after"]) > self.backend.clock() {
            observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::QuotaRecheckWait);
            return Ok(None);
        }
        // Only a complete fresh capsule can reconsider the automatic delay
        // from this same drained quota source. Explicit source resets and
        // independent owner/resource/technical waits remain authoritative.
        let quota_recheck = cooldown && crate::preparation::quota_recheck_candidate(
            i, &self.backend.jobs()?, self.backend.clock());
        if cooldown && !quota_recheck {
            observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::NotBefore);
            return Ok(None);
        }
        let unstarted = crate::unstarted_review_runtime::prepare(self, i, repo)?;
        let initial = crate::initial_review::prepare(self, i, repo)?;
        let requested_model = if truth(&i["model"]) {
            &i["model"]
        } else {
            &i["scheduler"]["console_request"]["options"]["model"]
        };
        if let Some(until) = if unstarted.is_none() && initial.is_none() { self
            .backend
            .resource_until(&json!({"model":requested_model}))?
        } else { None } {
            self.edit(&i["id"], |row| {
                if row["status"] == "todo" && launch_inputs(row) == launch_inputs(i) {
                    if !row["scheduler"].is_object() {
                        row["scheduler"] = json!({});
                    }
                    row["scheduler"]["not_before"] = json!(until);
                    row["scheduler"]["resource_wait"] =
                        json!({"reason":"quota_exhausted","until":until});
                }
            })?;
            observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::QuotaExhausted);
            return Ok(None);
        }
        let base = resolve(Path::new(repo))?;
        let jobs = self.backend.jobs()?;
        if i["subwork"].is_object() {
            let mut proposed = i.clone();
            if crate::preparation::inherited_review_workspace(i, &jobs) {
                let source =
                    crate::preparation::workspace_source(i, &jobs).ok_or(Error("InputRejected"))?;
                let ps = self.backend.snapshot()?;
                let at = self.backend.clock();
                if !arr(&i["jobs"]).contains(&source["id"])
                    || !matches!(
                        s(&source["status"]),
                        "succeeded" | "failed" | "lost" | "cancelled" | "canceled"
                    )
                    || jobs.iter().any(|job| {
                        (job["todo_id"] == i["id"]
                            || arr(&job["related_todo_ids"]).contains(&i["id"])
                            || arr(&i["jobs"]).contains(&job["id"]))
                            && (live(job) || alive(job, &ps, at))
                    })
                {
                    return Err(Error("InputRejected"));
                }
                // Validate provenance as an unallocated checkout. The stored
                // relation, old receipt and writer's source remain untouched.
                proposed["scheduler"]["worktree"] = Value::Null;
                proposed["scheduler"]["branch"] = Value::Null;
            }
            cc_job_policy::subwork::resolve(&self.todo()?, &proposed, &[], false)
                .map_err(|error| invalid_subwork(&error, "before_workspace"))?;
        }
        let recovered = self.backend.workspace_recovery(repo, i, &jobs)?;
        let expected_inputs = launch_inputs(i);
        let now = self.backend.clock();
        let mut reserved = false;
        let item=self.edit(&i["id"],|row|{if s(&row["status"])!="todo"||!truth(&row["dispatchable"])||!arr(&row["wait_on"]).is_empty(){return;}
if launch_inputs(row)!=expected_inputs || (n(&row["scheduler"]["not_before"])>now && !quota_recheck) {return;}
reserved=true;
if !row["scheduler"].is_object(){row["scheduler"]=json!({});}
if let Some(recovered)=recovered {merge(&mut row["scheduler"], recovered);}
if !truth(&row["scheduler"]["worktree"]){let slug=regex::Regex::new("[^a-z0-9]+").unwrap().replace_all(&s(&row["title"]).to_lowercase(),"-").trim_matches('-').chars().take(32).collect::<String>();let name=format!("todo-{}-{}",row["id"],if slug.is_empty(){"work"}else{&slug});merge(&mut row["scheduler"],json!({"worktree":PathBuf::from(format!("{}-wt",base.display())).join(&name),"branch":format!("scheduler/{name}")}));}
if !truth(&row["scheduler"]["key"]){let attempt=add_count(&row["scheduler"]["attempt"],1);merge(&mut row["scheduler"],json!({"key":uuid::Uuid::new_v4().simple().to_string(),"attempt":attempt}));}row["scheduler"]["phase"]=json!("preparing");})?;
        if !reserved
            || s(&item["status"]) != "todo"
            || !truth(&item["scheduler"]["key"])
            || (n(&item["scheduler"]["not_before"]) > self.backend.clock() && !quota_recheck)
        {
            observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::InputsChanged);
            return Ok(None);
        }
        self.backend.worktree(repo, &item)?;
        if item["subwork"].is_object() {
            cc_job_policy::subwork::resolve(&self.todo()?, &item, &[], true)
                .map_err(|error| invalid_subwork(&error, "after_workspace"))?;
        }
        let spec = Path::new(s(&item["spec"]));
        if spec.metadata().map_err(|_| Error("InputRejected"))?.len() > 1024 * 1024 {
            return Err(Error("ValueError"));
        }
        let text = fs::read_to_string(spec)?;
        if text.trim().is_empty() {
            return Err(Error("ValueError"));
        }
        let resources = arr(&item["needs"])
            .iter()
            .map(|v| s(v).to_string())
            .collect::<Vec<_>>()
            .join(", ");
        let mut prompt = format!(
            "{COMMON_INSTRUCTIONS}\n割り当てた資源: {}\n",
            if resources.is_empty() {
                "なし"
            } else {
                &resources
            }
        );
        prompt += &format!(
            "\n元の登録作業場所: {repo}\n資料は元の登録範囲内で参照し、原資料を保持してください。専用作業場所の作成は依頼の範囲を広げる許可ではありません。\n"
        );
        if truth(&item["scheduler"]["resume"]) {
            let completed_relocation = crate::preparation::workspace_source(&item, &jobs)
                .is_some_and(|source| source["status"] == "succeeded"
                    && item["scheduler"]["workspace_relocation"]["request"]["source_job"] == source["id"]);
            if completed_relocation {
                prompt += &format!("前回の担当は正常終了しました。完了した成果を保持し、保存された継続仕様と正常完了後の追記を同じ元番号で進めてください。専用worktreeのgit statusと途中コミットを確認してください。最初に D:/CC/Tools/todo/todo.exe history {} --limit 100 を正規CLIで読み、未完了の追記・元の許可・停止・依存も引き継いでください。履歴の続きがあれば --offset <next_offset> で読み、旧成果の重複実装や元の依頼全体の完了扱いをしないでください。\n",item["id"]);
            } else {
                prompt += &format!("{RESUME_INSTRUCTION}\n");
            }
        }
        let jobs = self.backend.jobs()?;
        let source = jobs.iter().find(|j| {
            !truth(&item["scheduler"]["console_fresh_request"])
                && (j["id"] == item["scheduler"]["last_job"]
                    || j["id"] == item["scheduler"]["workspace_receipt"]["job"])
                && owns_workspace(&item, j)
        });
        let mut resume_session = source.and_then(|j| {
            crate::preparation::recovery_session(&item, j, s(&item["scheduler"]["worktree"]), &jobs)
        });
        if let Some(source) = source {
            let result = safe_text(s(&source["last_message"]));
            if !result.is_empty() {
                prompt += &format!(
                    "\n前回の作業結果:\n{}\n",
                    result.chars().take(8000).collect::<String>()
                );
            }
        }
        if n(&item["scheduler"]["continuation_count"]) >= 3. {
            prompt += "\n3回以上続けても元の依頼が未完了です。まず進め方を設計から見直し、必要な既存の子作業の結果を統合してください。同じ不足説明や新しい引き継ぎ用ToDoの追加で終了せず、このToDoの完了条件を満たすまで続けてください。資源待ちは同じ担当が待って検証を完了してください。\n";
        }
        if truth(&item["scheduler"]["owner_instruction"]) {
            prompt += &format!(
                "\nオーナーの回答に従って再開してください:\n{}\n",
                s(&item["scheduler"]["owner_instruction"])
            );
        }
        if let Some(receipt) = evidence.and_then(|receipt| {
            crate::backend::bound_resume_evidence(&item, repo, text.as_bytes(), receipt)
        }) {
            let handoff = json!({
                "verification_source":"scrub391.exe --verify-current-succession",
                "receipt":receipt,
                "current_record":{"store":self.todo_path,"todo_id":item["id"]}
            });
            prompt += &format!(
                "\n正規に取得した今回の再開確認（ツール結果の引継ぎであり、このプロンプトへの本人の新しい直接入力やDiscord取得原文ではありません）:\n```json\n{}\n```\n\
                元ToDo {}の現在のメモは上の共有台帳からその番号だけを選んで読み、必要な過去メモは正規のtodo.exe history {}で確認してください。既存の本人回答・停止・依存・途中成果を保持し、同じ判断を再質問しないでください。\n\
                この確認は同じ元作業の再開に関するものです。古いproduct-resumeの期限・範囲や原保護名簿を変更したものではなく、元の業務成果の完成証拠でもありません。\n",
                serde_json::to_string_pretty(&handoff)?, item["id"], item["id"]
            );
        }
        prompt += &format!("\n依頼:\n{text}");
        if let Some(body) = item["scheduler"]["console_request"]["prompt"]
            .as_str()
            .filter(|_| {
                item["scheduler"]["console_receipts"]
                    [s(&item["scheduler"]["console_request"]["key"])]["launch_key"]
                    == item["scheduler"]["key"]
            })
        {
            prompt += &format!(
                "\n受付済みの引き継ぎ本文（受付時の履歴です。固定HEADと対象範囲は上の最新保存仕様を優先してください）:\n{body}\n"
            );
        }
        if item["subwork"].is_object() {
            prompt += &format!(
                "\nこの作業はToDo {}の独立した{}です。保存済み仕様の有限な範囲だけを扱い、主担当の完了を前提にせず結果を返してください。主担当のソースを編集したり、独立したPRの作成やマージを担当したりしないでください。\n",
                item["subwork"]["parent_todo_id"],
                s(&item["subwork"]["kind"])
            );
        }
        if item["product_review_allocation"].is_object() {
            prompt += "\n2026-10-13の元2407の起動修復に伴う最新の出力指示: 固定HEADのレビューは読み取り専用です。結果は通常の最後の応答だけで主担当へ返してください。原仕様にある結果ファイルへの保存と共有ToDo更新は主担当が正規の応答から行います。ファイル保存・コード編集・ビルド・テスト・子ジョブ・Discord投稿は行わず、具体的なMust/Suggestion/Nitと未測定事項を返してこの有限レビューを終了してください。元1626の担当は起動しません。\n";
        }
        let task = format!("{text}\n{}", s(&item["scheduler"]["owner_instruction"]));
        let mut selection = item["scheduler"]["selection"].clone();
        let accepted_options = &item["scheduler"]["console_request"]["options"];
        if !truth(&selection) {
            selection = if truth(&item["model"]) && truth(&item["effort"]) {
                json!({"model":item["model"],"effort":item["effort"],"task_type":"implementation"})
            } else {
                self.backend.pick(&task)?
            };
        }
        // Automatic termination does not authorize routing the saved work to a
        // different model or widening its sandbox. Explicit new inputs still win.
        if let Some(source) = source.filter(|source| cancellation::recovery_cancel(&item, source)) {
            for field in [
                "model",
                "effort",
                "sandbox",
                "task_type",
                "mcp_profile",
                "network",
                "add_dirs",
            ] {
                if !truth(&item[field])
                    && !truth(&accepted_options[field])
                    && !source[field].is_null()
                {
                    selection[field] = source[field].clone();
                }
            }
            if source["no_action_recovery"].is_object() {
                selection["model_explicit"] = source["model_explicit"].clone();
                if source["model_explicit"] == false
                    && source["model_origin"] != "owner_request"
                    && !truth(&item["model"])
                    && !truth(&accepted_options["model"])
                {
                    selection["model"] =
                        json!(cc_ask::job_policy::model_profile("", "implementation").model);
                }
            }
        }
        // A cached selection is only a default, never authority over an explicit ToDo.
        for k in ["model", "effort"] {
            if truth(&accepted_options[k]) {
                selection[k] = accepted_options[k].clone();
            }
            if truth(&item[k]) {
                selection[k] = item[k].clone();
            }
            let origin = format!("{k}_origin");
            if accepted_options[&origin] == "owner_request"
                && item[&origin] != "owner_request"
                && truth(&accepted_options[k])
            {
                selection[k] = accepted_options[k].clone();
            }
            if !truth(&item[k]) && !truth(&accepted_options[k]) {
                if let Some(source) = source.filter(|source| source[&origin] == "owner_request") {
                    selection[k] = source[k].clone();
                }
            }
        }
        // Claude CLI aliases identify the same existing subscription model.
        // Normalize them before checking the served catalog; unknown explicit
        // model names still fail without silently selecting another model.
        let model = s(&selection["model"]);
        selection["model"] = json!(match model {
            "sonnet" => "anthropic/claude-sonnet-5-5".to_owned(),
            "opus" => "anthropic/claude-opus-5-5".to_owned(),
            "haiku" => "anthropic/claude-haiku-5-5".to_owned(),
            _ if model.starts_with("claude-") => format!("anthropic/{model}"),
            _ => model.to_owned(),
        });
        if let Some(kind) = accepted_options["task_type"]
            .as_str()
            .or(item["task_type"].as_str())
        {
            selection["task_type"] = json!(kind);
        } else if ["review-only", "review_only"].contains(&s(&item["role"])) {
            selection["task_type"] = json!("review");
        }
        selection["model_origin"] =
            selection_origin("model", &item, accepted_options, source, &selection);
        selection["effort_origin"] =
            selection_origin("effort", &item, accepted_options, source, &selection);
        let fixed_contract =
            source.is_some_and(cc_ask::job_policy::launch_context::fixed_execution_contract);
        if let Some(source) = source.filter(|_| fixed_contract) {
            for field in ["model", "effort"] {
                if !truth(&item[field]) && !truth(&accepted_options[field]) && truth(&source[field])
                {
                    selection[field] = source[field].clone();
                }
            }
        }
        let mut managed = selection.clone();
        managed["todo_id"] = item["id"].clone();
        if !fixed_contract {
            cc_ask::job_policy::apply_managed_launch_policy(&mut managed);
        }
        for field in [
            "model",
            "model_origin",
            "effort",
            "effort_origin",
            "legacy_model_hint",
            "legacy_effort_hint",
        ] {
            if !managed[field].is_null() {
                selection[field] = managed[field].clone();
            }
        }
        if cc_ask::job_policy::review_task("", s(&selection["task_type"]))
            || s(&item["subwork"]["kind"]) == "review"
            || unstarted.is_some()
        {
            let sources = if unstarted.is_some() {
                cc_job_policy::unstarted_review_state::WRITERS.iter().map(|id| id.to_string()).collect()
            } else { cc_ask::job_policy::review_source_jobs(&item, &self.todo()?) };
            let mut excluded = Vec::<String>::new();
            for id in &sources {
                let author=jobs.iter().find(|job|s(&job["id"])==id)
                    .ok_or(Error("ReviewSourceUnavailable"))?;
                excluded.extend(cc_ask::job_policy::author_models(author));
            }
            excluded.sort();
            excluded.dedup();
            let explicit = truth(&item["model"])
                || truth(&accepted_options["model"])
                || selection["model_explicit"] == true
                || selection["model_origin"] == "owner_request";
            if let Some(evidence) = &unstarted {
                crate::unstarted_review_runtime::select(&item, &mut selection, evidence,
                    &cc_job_policy::product_retry::digest(text.as_bytes()), self.backend.clock())?;
            }
            if let Some(evidence) = &initial {
                crate::initial_review::select(self,&item,&mut selection,evidence,
                    &cc_job_policy::product_retry::digest(text.as_bytes()))?;
            }
            let conflict = excluded
                .iter()
                .any(|author| cc_ask::job_policy::same_model(author, s(&selection["model"])));
            let owner_choice = selection["model_origin"] == "owner_request";
            selection["review_excluded_models"] = json!(excluded);
            let quota_choice= if unstarted.is_some() || initial.is_some() { true } else {
                crate::quota_recovery::prepare(&mut self.backend,&item,source,&jobs,&mut selection,
                    &cc_job_policy::product_retry::digest(text.as_bytes()))?
            };
            if quota_choice && excluded.iter().any(|author| cc_job_policy::same_model(author, s(&selection["model"]))) {
                return Err(Error("ReviewModelConflict"));
            }
            if explicit && owner_choice && conflict {
                return Err(Error("ReviewModelConflict"));
            }
            let ai_writer_default = !owner_choice
                && ["gpt-6.1-sol", "gpt-6-luna", "gpt-6-astra"]
                    .iter()
                    .any(|model| cc_ask::job_policy::same_model(s(&selection["model"]), model));
            if !quota_choice && (!explicit || conflict || ai_writer_default) {
                let profile=cc_ask::job_policy::review_profile(&excluded).ok_or(Error("ReviewModelUnavailable"))?;
                selection["model"]=json!(profile.model);
                selection["model_origin"] = json!("policy");
                if !truth(&item["effort"]) && !truth(&accepted_options["effort"]) {
                    selection["effort"] = json!(profile.effort);
                    selection["effort_origin"] = json!("policy");
                }
            }
            selection["review_source_jobs"] = json!(sources);
            selection["review_excluded_models"] = json!(excluded);
            selection["task_type"] = json!("review");
            // Check the selected review provider too: a legacy GPT hint can
            // route to a depleted default before the CLI ever sees it.
            if let Some(until)=if initial.is_none() {self.backend.resource_until(&selection)?} else {None} {
                self.edit(&item["id"],|row| {
                    if row["status"] == "todo" && row["scheduler"]["key"] == item["scheduler"]["key"]
                        && launch_inputs(row) == launch_inputs(&item) {
                        row["scheduler"]["not_before"]=json!(until);
                        row["scheduler"]["resource_wait"]=json!({"reason":"quota_exhausted","until":until});
                    }
                })?;
                observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::QuotaExhausted);
                return Ok(None);
            }
        }
        if item["subwork"]["kind"] == "research" {
            crate::quota_recovery::prepare(&mut self.backend, &item, source, &jobs, &mut selection,
                &cc_job_policy::product_retry::digest(text.as_bytes()))?;
        }
        if quota_recheck && !selection["quota_recovery"].is_object() {
            observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::QuotaProofMissing);
            return Ok(None);
        }
        if !valid_model(s(&selection["model"])) {
            return Err(Error("ValueError"));
        }
        if !self.backend.model_exists(s(&selection["model"])) {
            if truth(&item["model"])
                || truth(&accepted_options["model"])
                || selection["model_explicit"] == true
                || selection["model_origin"] == "owner_request"
            {
                self.log(
                    "WARNING",
                    "指定モデルを利用できないため、指定を保持して再試行します。",
                )?;
                return Err(Error("ModelUnavailable"));
            }
            // An agent asked for a model OpenCodex does not serve (2026-10-01: "gpt-6.1-luna" failed 4 times and
            // reached the owner as a question). Let Jev pick an existing model instead of failing again.
            self.log(
                "WARNING",
                &format!(
                    "model {} is not served by OpenCodex; asking Jev instead",
                    s(&selection["model"])
                ),
            )?;
            let picked = self.backend.pick(&task)?;
            selection["model"] = picked["model"].clone();
            selection["model_origin"] = json!("policy");
            if !self.backend.model_exists(s(&selection["model"])) {
                selection["model"] = json!("gpt-6.1-sol");
            }
        }
        if !valid_effort(s(&selection["effort"])) {
            return Err(Error("ValueError"));
        }
        let mut managed = selection.clone();
        managed["todo_id"] = item["id"].clone();
        if !fixed_contract {
            cc_ask::job_policy::apply_managed_launch_policy(&mut managed);
        }
        for field in [
            "model",
            "model_origin",
            "effort",
            "effort_origin",
            "legacy_model_hint",
            "legacy_effort_hint",
        ] {
            if !managed[field].is_null() {
                selection[field] = managed[field].clone();
            }
        }
        if !self.backend.model_exists(s(&selection["model"])) {
            return Err(Error("ModelUnavailable"));
        }
        if item["subwork"].is_object() {
            selection["task_type"] = item["subwork"]["kind"].clone();
        }
        // An unfinished successful conversation needs its recorded execution
        // scope too, not only its model/session. Explicit saved inputs still win.
        if let Some(source) = source.filter(|_| resume_session.is_some()) {
            let sandbox = if !item["sandbox"].is_null() {
                &item["sandbox"]
            } else if !accepted_options["sandbox"].is_null() {
                &accepted_options["sandbox"]
            } else {
                &source["sandbox"]
            };
            if !sandbox.is_null() {
                selection["sandbox"] = sandbox.clone();
            }
        }
        if item["scheduler"]["console_request"]["read_only"] == true
            || ["research", "review"].contains(&s(&item["subwork"]["kind"]))
            || cc_ask::job_policy::explicit_read_only(s(&item["title"]))
        {
            selection["sandbox"] = json!("read-only");
        }
        // A resumed conversation may restore the previous model. Reuse it only
        // when the selected settings agree, including partial explicit choices.
        if source.is_some_and(|source| {
            (selection["sandbox"] == "read-only" && source["sandbox"] != "read-only")
                || (cc_ask::job_policy::review_result::is_finite_review(source)
                    && source["finite_review"]["spec"]
                        .as_str()
                        .or(source["subwork_spec"].as_str())
                        != item["spec"].as_str())
                || ["model", "effort"].iter().any(|field| {
                    (truth(&item[*field]) || source[*field].is_string())
                        && source[*field] != selection[*field]
                })
        }) {
            resume_session = None;
        }
        // Do not carry capabilities from a conversation that was just retired.
        if let Some(source) = source.filter(|_| resume_session.is_some()) {
            for field in ["mcp_profile", "network", "add_dirs"] {
                let value = if !item[field].is_null() {
                    &item[field]
                } else if !accepted_options[field].is_null() {
                    &accepted_options[field]
                } else {
                    &source[field]
                };
                if !value.is_null() {
                    selection[field] = value.clone();
                }
            }
        }
        if cc_job_policy::review_task(s(&item["title"]), s(&selection["task_type"]))
            || ["review", "research"].contains(&s(&item["subwork"]["kind"]))
        {
            // Bind the same bytes embedded above, before the launch CLI can
            // observe a later edit at the saved specification's path.
            selection["finite_review_spec_sha256"] =
                json!(cc_job_policy::product_retry::digest(text.as_bytes()));
        }
        let folder = self.root.join("prompts");
        fs::create_dir_all(&folder)?;
        let target = folder.join(format!(
            "{}-{}.md",
            item["id"],
            s(&item["scheduler"]["key"])
        ));
        backup(&target)?;
        fs::write(&target, prompt)?;
        let selection_input = json!({"model":item["model"],"effort":item["effort"]});
        let prepared = self.edit(&item["id"], |row| {
            if s(&row["status"]) == "todo"
                && row["scheduler"]["key"] == item["scheduler"]["key"]
                && launch_inputs(row) == launch_inputs(&item) {
                merge(
                    &mut row["scheduler"],
                    json!({"phase":"prepared","selection":selection,"prompt":target,"resume_session":resume_session,"selection_input":selection_input}),
                );
            }
        })?;
        if s(&prepared["status"]) != "todo"
            || prepared["scheduler"]["key"] != item["scheduler"]["key"]
            || launch_inputs(&prepared) != launch_inputs(&item)
            || prepared["scheduler"]["prompt"] != json!(target)
        {
            observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::InputsChanged);
            return Ok(None);
        }
        Ok(Some(prepared))
    }
    pub fn dispatch(&mut self, cfg: &Value, pri: &Value) -> Result<()> {
        self.dispatch_for(cfg, pri, None)
    }
    /// Try one existing finite subwork using ordinary admission, without registration.
    pub fn dispatch_existing_subwork(&mut self, id: i64) -> Result<Value> {
        if id <= 0 {
            return Err(Error("InputRejected"));
        }
        remove(&mut self.report, "launched");
        let _lock = ByteLock::acquire(&self.root.join("scheduler.lock"), Duration::from_secs(30))?
            .ok_or(Error("TimeoutError"))?;
        self.state = read_json(&self.root.join("state.json"), json!({}))?;
        if !self.state.is_object() {
            return Err(Error("AttributeError"));
        }
        let db = self.todo()?;
        let item = arr(&db["items"])
            .into_iter()
            .find(|row| row["id"] == id)
            .ok_or(Error("InputRejected"))?;
        if !item["subwork"].is_object() || item["project"] == "owner" {
            return Err(Error("InputRejected"));
        }
        let jobs = self.backend.jobs()?;
        let ps = self.backend.snapshot()?;
        let at = self.backend.clock();
        let active = jobs.iter().any(|job| {
            (job["todo_id"] == id
                || arr(&item["jobs"]).contains(&job["id"])
                || arr(&job["related_todo_ids"]).contains(&json!(id)))
                && (live(job) || alive(job, &ps, at))
        });
        if !active {
            let (cfg, pri) = self.settings()?;
            self.dispatch_for(&cfg, &pri, Some(id))?;
            self.persist()?;
        }
        let current = arr(&self.todo()?["items"])
            .into_iter()
            .find(|row| row["id"] == id)
            .ok_or(Error("InputRejected"))?;
        Ok(
            json!({"id":id,"status":current["status"],"phase":current["scheduler"]["phase"],
            "active":active,"launched":self.report["launched"],"priority_wait":self.report["priority_wait"],
            "allowance_now":self.report["allowance_now"],"busy_resources":self.report["busy_resources"],
            "not_before":current["scheduler"]["not_before"],
            "preparation_reason":current["scheduler"]["preparation_failure"]["reason"]}),
        )
    }
    /// Caller holds scheduler.lock. The accepted body is durable before any launch.
    /// Preparation and admission remain the same as ordinary finite work.
    pub fn dispatch_console_subwork(
        &mut self,
        cfg: &Value,
        pri: &Value,
        id: i64,
        request: &Value,
    ) -> Result<Option<String>> {
        if !request["key"].is_string()
            || s(&request["key"]).is_empty()
            || !request["launch_key"].is_string()
            || s(&request["launch_key"]).is_empty()
            || !request["prompt"].is_string()
            || s(&request["prompt"]).len() > 128 * 1024
        {
            return Err(Error("ValueError"));
        }
        let db = self.todo()?;
        let mut item = arr(&db["items"])
            .into_iter()
            .find(|i| i["id"] == id)
            .ok_or(Error("ValueError"))?;
        if !item["subwork"].is_object() {
            return Err(Error("InvalidSubwork"));
        }
        let receipt = &item["scheduler"]["console_receipts"][s(&request["key"])];
        if receipt.is_object() && receipt["request"] != *request {
            return Err(Error("InputRejected"));
        }
        let jobs = self.backend.jobs()?;
        // Recover a response lost after the launcher accepted the exact reservation.
        if receipt.is_object() {
            if let Some(job) = jobs
                .iter()
                .find(|j| j["launch_key"] == receipt["launch_key"] && j["todo_id"] == id)
            {
                return Ok(Some(s(&job["id"]).to_string()));
            }
        }
        // A legacy Start may leave an unowned review doing after its old worker
        // was cancelled for recovery. Reconcile only that proven idle retry,
        // then use the same reservation and every ordinary admission gate.
        let previous = jobs
            .iter()
            .find(|job| job["id"] == item["scheduler"]["last_job"]);
        let idle_retry = item["status"] == "doing"
            && item["scheduler"]["phase"] == "retry"
            && item["scheduler"]["resume"] == true
            && !truth(&item["scheduler"]["job"])
            && !truth(&item["scheduler"]["key"])
            && !truth(&item["worker_receipt"]["job"])
            && previous.is_some_and(|job| {
                cancellation::cancelled(job)
                    && cancellation::recovery_cancel(&item, job)
                    && arr(&item["jobs"]).contains(&job["id"])
            })
            && !jobs.iter().any(|job| {
                live(job) && (job["todo_id"] == id || arr(&item["jobs"]).contains(&job["id"]))
            });
        if idle_retry && !todo_wait::waiting(&db, &item) {
            // Checkout creation and its mandatory validation remain in prepare().
            cc_job_policy::subwork::resolve(&db, &item, &jobs, false)
                .map_err(|_| Error("InvalidSubwork"))?;
            let before = item.clone();
            item = self.edit(&json!(id), |row| {
                if *row == before {
                    row["status"] = json!("todo");
                }
            })?;
        }
        if item["status"] != "todo"
            || todo_wait::waiting(&db, &item)
            || n(&item["scheduler"]["not_before"]) > self.backend.clock()
        {
            return Ok(None);
        }
        let old = &item["scheduler"]["console_request"];
        if old.is_object() && old != request {
            let previous = &item["scheduler"]["console_receipts"][s(&old["key"])];
            // Distinct bodies wait for the previous real job, retaining both receipts.
            // Never overwrite an unlaunched reservation or reuse its launch key.
            if !jobs.iter().any(|j| {
                j["launch_key"] == previous["launch_key"] && j["todo_id"] == id && !live(j)
            }) || (truth(&item["scheduler"]["key"])
                && jobs
                    .iter()
                    .any(|j| j["launch_key"] == item["scheduler"]["key"]))
            {
                return Ok(None);
            }
        }
        let inputs = launch_inputs(&item);
        let saved = self.edit(&json!(id), |row| {
            if row["status"] == "todo" && launch_inputs(row) == inputs {
                if !row["scheduler"].is_object() {
                    row["scheduler"] = json!({});
                }
                row["scheduler"]["console_request"] = request.clone();
                if old.is_object() && old != request {
                    // A new accepted body starts its own conversation. The files
                    // and all earlier receipts remain, but their conversation does not.
                    row["scheduler"]["console_fresh_request"] = json!(true);
                    remove(&mut row["scheduler"], "selection");
                    remove(&mut row["scheduler"], "resume_session");
                }
                if !truth(&row["scheduler"]["key"]) {
                    row["scheduler"]["key"] = request["launch_key"].clone();
                }
                if !row["scheduler"]["console_receipts"].is_object() {
                    row["scheduler"]["console_receipts"] = json!({});
                }
                row["scheduler"]["console_receipts"][s(&request["key"])] =
                    json!({"request":request,"launch_key":row["scheduler"]["key"]});
            }
        })?;
        if saved["scheduler"]["console_request"] != *request {
            return Ok(None);
        }
        self.dispatch_for(cfg, pri, Some(id))?;
        self.persist()?;
        Ok(self.report["launched"]["job"].as_str().map(str::to_string))
    }
    /// A watchdog reservation can dispatch only its own item through normal capacity/resource gates.
    pub fn dispatch_for(&mut self, cfg: &Value, pri: &Value, target: Option<i64>) -> Result<()> {
        let mut observation = Observation::new(self.backend.clock(), target.is_none(), truth(&cfg["dispatch_enabled"]));
        self.admission_updates = target.is_none().then(std::collections::BTreeMap::new);
        let result = self.dispatch_observed(cfg, pri, target, &mut observation);
        if let Some(updates) = self.admission_updates.take() {
            for (id,fingerprint) in updates { observation.result(id,fingerprint); }
        }
        observation.finish(self.backend.clock());
        observation.complete = result.is_ok();
        observation.dispatch_error = result.as_ref().err().map(crate::admission::Failure::from_error);
        if target.is_none() {
            // A failed diagnostic write must not change any dispatch result.
            self.report["admission_recorded"] = json!(observation.publish(&self.root).is_ok());
        }
        result
    }
    fn dispatch_observed(&mut self, cfg: &Value, pri: &Value, target: Option<i64>, observation: &mut Observation) -> Result<()> {
        if !truth(&cfg["dispatch_enabled"]) {
            return Ok(());
        }
        let (_, ps, jobs) = self.sample(cfg)?;
        let db = self.todo()?;
        let busy = resources_busy_with_lock(
            &arr(&db["items"]),
            &jobs,
            &ps,
            self.backend.clock(),
            &self.civ6_home(),
        )?;
        // Read the private exclusions only in memory. A missing original is not
        // an empty list; only this explicitly verified repair repository can run.
        let exclusions =
            std::fs::read_to_string(self.root.parent().unwrap().join("client-projects.txt"));
        // Owned by this admission call only; never a persistent permission cache.
        let mut resume_receipts = std::collections::BTreeMap::new();
        let eligible: Vec<Value> = arr(&db["items"])
            .into_iter()
            .filter(|i| {
                let project = s(&i["project"]);
                let path = s(&db["projects"][project]);
                observation.item(i, &db["projects"][project]);
                macro_rules! reject {
                    ($condition:expr, $reason:ident) => {
                        if $condition {
                            observation.decide(&i["id"], AdmissionStage::Filtering, AdmissionReason::$reason);
                            return false;
                        }
                    }
                }
                reject!(project == "owner", OwnerProject);
                reject!(s(&i["status"]) != "todo", StatusNotTodo);
                reject!(!truth(&i["dispatchable"]), NotDispatchable);
                reject!(!truth(&i["spec"]), MissingSpecMetadata);
                reject!(todo_wait::waiting(&db, i), DependencyWait);
                reject!(target.is_some() && crate::targeted_dispatch::held(i), TargetedHold);
                reject!(i["scheduler"]["quota_retry_terminal"]["source_job"]==i["scheduler"]["last_job"]
                    && i["scheduler"]["quota_retry_terminal"].is_object(), QuotaRetryEnded);
                let review_failure = &i["scheduler"]["review_preparation_failure"];
                if review_failure["exhausted"] == true
                    && review_failure["inputs"] == review_request_inputs(i)
                {
                    let sources = cc_ask::job_policy::review_source_jobs(i, &db);
                    let recovered = review_failure["reason"] == "ReviewSourceUnavailable"
                        && !sources.is_empty()
                        && sources
                            .iter()
                            .all(|id| jobs.iter().any(|job| job["id"] == *id));
                    if !recovered {
                        observation.decide(&i["id"], AdmissionStage::Filtering, AdmissionReason::ReviewPreparationExhausted);
                        return false;
                    }
                }
                let mut guard_allowed = None;
                let excluded = match &exclusions {
                    Ok(names) => names
                        .trim_start_matches('\u{feff}')
                        .lines()
                        .map(str::trim)
                        .filter(|n| !n.is_empty() && !n.starts_with('#'))
                        .any(|n| {
                            project.to_lowercase().contains(&n.to_lowercase())
                                || path.to_lowercase().contains(&n.to_lowercase())
                        }),
                    Err(error)
                        if error.kind() == std::io::ErrorKind::NotFound && i["id"] == 2407 =>
                    {
                        let allowed = self.backend.product_review_allowed(i);
                        guard_allowed = Some(allowed);
                        !allowed
                    }
                    Err(error)
                        if error.kind() == std::io::ErrorKind::NotFound
                            && ["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"]
                                .contains(&project) =>
                    {
                        let allowed = self.backend.repair_scope_allowed(path);
                        guard_allowed = Some(allowed);
                        !allowed
                    }
                    Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                        if let Some(receipt) = self.backend.current_product_admission(i, path) {
                            guard_allowed = Some(true);
                            if let Some(id) = i["id"].as_u64() {
                                resume_receipts.insert(id, receipt);
                            }
                            false
                        } else {
                            guard_allowed = Some(false);
                            true
                        }
                    }
                    Err(_) => true,
                };
                observation.root(&i["id"], guard_allowed, excluded);
                reject!(project == "owner", OwnerProject);
                reject!(todo_wait::waiting(&db, i), DependencyWait);
                reject!(excluded, RootExcluded);
                reject!(target.is_none() && i["stall_recovery"]["pending"].is_object(), RecoveryReservation);
                reject!(jobs.iter().any(|j| {
                        live(j)
                            && (arr(&i["jobs"]).contains(&j["id"])
                                || arr(&j["related_todo_ids"]).contains(&i["id"])
                                || i["scheduler"]["job"] == j["id"])
                    }), RelatedOwner);
                observation.eligible(&i["id"]);
                true
            })
            .collect();
        let mut queue = priority_queue_observed(&eligible, pri, self.backend.clock(), |item, reason| {
            if let Some(reason) = reason { observation.decide(&item["id"], AdmissionStage::Priority, reason); }
        });
        // Reconsider only the same source's automatic quota delay. Do not
        // rewrite its stored deadline or make prepare's proof optional.
        queue.extend(eligible.iter().filter(|item| {
            crate::preparation::quota_recheck_candidate(item, &jobs, self.backend.clock())
        }).cloned());
        let ranks = cc_job_policy::priority::todo_priorities_for_jobs(&db, &jobs, pri).ranks;
        // Rotation is only a tie breaker within the owner's priority class.
        queue.sort_by(|a, b| {
            ranks
                .get(&a["id"].as_u64().unwrap_or(0))
                .copied()
                .unwrap_or(cc_job_policy::priority::DEFAULT)
                .cmp(
                    &ranks
                        .get(&b["id"].as_u64().unwrap_or(0))
                        .copied()
                        .unwrap_or(cc_job_policy::priority::DEFAULT),
                )
                .then_with(|| {
                    n(&self.state["project_last_launch"][s(&a["project"])])
                        .total_cmp(&n(&self.state["project_last_launch"][s(&b["project"])]))
                })
        });
        merge(
            &mut self.report,
            json!({"queue_length":queue.len(),"busy_resources":busy}),
        );
        observation.queue(&queue, &self.report);
        if !truth(&self.report["allowance_now"]) {
            observation.capacity_wait();
            return Ok(());
        }
        if let Some(target) = target {
            let target_rank = queue
                .iter()
                .find(|i| i["id"].as_i64() == Some(target))
                .and_then(|i| ranks.get(&i["id"].as_u64().unwrap_or(0)))
                .copied();
            if target_rank.is_some_and(|rank| {
                queue.iter().any(|i| {
                    ranks
                        .get(&i["id"].as_u64().unwrap_or(0))
                        .is_some_and(|other| *other < rank)
                        && !arr(&i["needs"]).iter().any(|v| busy.contains(s(v)))
                })
            }) {
                self.report["priority_wait"] = json!(target);
                return Ok(());
            }
            queue.retain(|i| i["id"].as_i64() == Some(target));
        }
        let mut first_failure = None;
        for i in &queue {
            if arr(&i["needs"]).iter().any(|v| busy.contains(s(v))) {
                observation.decide(&i["id"], AdmissionStage::Resource, AdmissionReason::ResourceBusy);
                continue;
            }
            observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::PreparationDeferred);
            let result: Result<()> = (|| {
                let evidence = i["id"].as_u64().and_then(|id| resume_receipts.get(&id));
                let Some(mut prepared) = self.prepare_with_evidence(
                    i,
                    s(&db["projects"][s(&i["project"])]),
                    evidence,
                    observation,
                )?
                else {
                    return Ok(());
                };
                if s(&prepared["status"]) != "todo" {
                    observation.decide(&i["id"], AdmissionStage::Preparation, AdmissionReason::PreparedStatusChanged);
                    return Ok(());
                }
                observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::Unknown);
                let (_, ps, jobs) = self.sample(cfg)?;
                let db = self.todo()?;
                let busy = resources_busy_with_lock(
                    &arr(&db["items"]),
                    &jobs,
                    &ps,
                    self.backend.clock(),
                    &self.civ6_home(),
                )?;
                observation.recheck(&i["id"], &self.report);
                if !truth(&self.report["allowance_now"]) {
                    observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::CapacityChanged);
                    return Ok(());
                }
                if arr(&prepared["needs"]).iter().any(|v| busy.contains(s(v))) {
                    observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::ResourceChanged);
                    return Ok(());
                }
                let current = arr(&db["items"])
                    .into_iter()
                    .find(|r| r["id"] == prepared["id"])
                    .ok_or(Error("ValueError"))?;
                if todo_wait::waiting(&db, &current) {
                    observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::DependencyChanged);
                    return Ok(());
                }
                if target.is_some() && crate::targeted_dispatch::held(&current) {
                    observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::HoldChanged);
                    return Ok(());
                }
                if s(&current["status"]) != "todo"
                    || current["scheduler"]["key"] != prepared["scheduler"]["key"]
                    || launch_inputs(&current) != launch_inputs(&prepared)
                {
                    observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::InputsChanged);
                    return Ok(());
                }
                // Recheck the finite receipt after preparation and sampling.
                // A changed spec/root or a newly restored deny wins admission.
                if !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"]
                    .contains(&s(&current["project"])) && current["id"] != 2407
                    && matches!(&exclusions, Err(e) if e.kind() == std::io::ErrorKind::NotFound)
                    && !self.current_product_without_registry(
                        &current, s(&db["projects"][s(&current["project"])]))
                {
                    observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::RootChanged);
                    return Ok(());
                }
                if current["subwork"].is_object() {
                    if current["product_review_allocation"].is_object()
                        && !self.backend.product_review_allowed(&current)
                    {
                        observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::InputsChanged);
                        return Err(Error("InputRejected"));
                    }
                    let parent = cc_job_policy::subwork::resolve(&db, &current, &jobs, true)
                        .map_err(|_| Error("InvalidSubwork"))?;
                    // Publish proof of this resource/capacity check, not just preparation's launch key.
                    let receipt = cc_job_policy::subwork::admission(&prepared);
                    let saved = self.edit(&prepared["id"], |row| {
                        if row["status"] == "todo"
                            && row["scheduler"]["key"] == prepared["scheduler"]["key"]
                            && launch_inputs(row) == launch_inputs(&prepared)
                        {
                            row["scheduler"]["subwork_admission"] = receipt.clone();
                        }
                    })?;
                    if saved["status"] != "todo"
                        || launch_inputs(&saved) != launch_inputs(&prepared)
                        || saved["scheduler"]["subwork_admission"] != receipt
                    {
                        observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::InputsChanged);
                        return Ok(());
                    }
                    prepared["scheduler"]["subwork_parent_job"] = json!(parent);
                }
                // Sampling may yield while the owner changes the requested model.
                // Defer this receipt so prepare recomputes settings and session on
                // the next pass, using the same durable launch key.
                if ["model", "effort"].iter().any(|field| {
                    current[*field] != prepared["scheduler"]["selection_input"][*field]
                }) {
                    observation.decide(&i["id"], AdmissionStage::Recheck, AdmissionReason::ModelSelectionChanged);
                    return Ok(());
                }
                self.state["last_launch"] = json!(self.backend.clock());
                if !self.state["project_last_launch"].is_object() {
                    self.state["project_last_launch"] = json!({});
                }
                self.state["project_last_launch"][s(&prepared["project"])] =
                    json!(self.backend.clock());
                self.persist()?;
                observation.decide(&i["id"], AdmissionStage::Launch, AdmissionReason::Unknown);
                let jid = self.backend.launch(
                    &prepared,
                    Path::new(s(&prepared["scheduler"]["prompt"])),
                    &prepared["scheduler"]["selection"],
                )?;
                observation.decide(&i["id"], AdmissionStage::Launch, AdmissionReason::LaunchedUnlinked);
                let j = self
                    .backend
                    .jobs()?
                    .into_iter()
                    .find(|r| s(&r["id"]) == jid)
                    .unwrap_or(json!({"id":jid,"created_at":self.backend.clock()}));
                self.link(&prepared["id"], &j, &prepared["scheduler"]["key"])?;
                observation.decide(&i["id"], AdmissionStage::Launch, AdmissionReason::Launched);
                merge(
                    &mut self.report,
                    json!({"launched":{"item":i["id"],"job":jid},"allowance_now":0,"launch_wait_s":cfg["launch_interval_s"]}),
                );
                self.log(
                    "INFO",
                    &format!(
                        "launched item {} job {} priority {}",
                        i["id"],
                        jid,
                        priority(i, pri)
                    ),
                )?;
                Ok(())
            })();
            if let Err(e) = &result {
                observation.failed(&i["id"], e);
                if e.0 != "KeyboardInterrupt" {
                    let until =
                        self.backend.clock() + if e.0 == "ResourceWait" { 3600. } else { 300. };
                    let failed_at = self.backend.clock();
                    self.guard(&format!("defer item {}", i["id"]), |eng| {
                    eng.edit(&i["id"], |row| {
                        if s(&row["status"]) == "todo" {
                            if !row["scheduler"].is_object() {
                                row["scheduler"] = json!({});
                            }
                            if e.0=="QuotaRetryEnded" {
                                row["scheduler"]["phase"]=json!("quota_retry_ended");
                                row["scheduler"]["quota_retry_terminal"]=json!({"source_job":i["scheduler"]["last_job"],"at":failed_at,"reason":"finite_or_invalid_retry_history"});
                                row["note"]=json!("同じ担当の利用枠回復を有限予算で終了しました。元の仕様・履歴・停止状態を保持しています。");
                                return;
                            }
                            let until = n(&row["scheduler"]["not_before"]).max(until);
                            let old = &row["scheduler"];
                            let retained_quota = e.0 == "ResourceWait"
                                && old["preparation_failure"]["reason"] == "resource_wait"
                                && old["resource_wait"]["reason"] == "http_429"
                                && old["last_job"] == i["scheduler"]["last_job"]
                                && old["not_before"].is_number();
                            let until = if retained_quota {n(&old["not_before"])} else {until};
                            row["scheduler"]["not_before"] = json!(until);
                            if e.0 == "ResourceWait" {row["scheduler"]["quota_recheck_after"] = json!(failed_at+300.);}
                            let (reason, message) = failure_reason(e);
                            if e.0 == "ResourceWait" { row["scheduler"]["resource_wait"] = json!({"reason":"http_429","until":until}); }
                            row["scheduler"]["preparation_failure"] = json!({"reason":reason,"at":failed_at,"retry_at":until});
                            if ["ReviewModelConflict","ReviewModelUnavailable","ReviewSourceUnavailable"].contains(&e.0) {
                                let inputs = review_request_inputs(row);
                                let previous = &row["scheduler"]["review_preparation_failure"];
                                let count = if previous["inputs"] == inputs {n(&previous["count"]) as u64 + 1} else {1};
                                row["scheduler"]["review_preparation_failure"] = json!({"reason":e.0,
                                    "inputs":inputs,"count":count,"exhausted":count>=3});
                                if count>=3 {
                                    row["note"]=json!("レビューのモデルと作者の照合を3回で終了しました。記録または指定の変更後に再確認します。");
                                    return;
                                }
                            }
                            row["note"] = json!(format!(
                                "起動の準備: {message}。保存済みの作業を保ち、待機期限後に再確認します。"
                            ));
                        }
                    })
                });
                }
            }
            if result.as_ref().is_err_and(|e| e.0 == "KeyboardInterrupt") {
                return result;
            }
            if let Err(e) = result {
                self.log(
                    "WARNING",
                    &format!(
                        "preparation item {} reason={}",
                        i["id"],
                        failure_reason(&e).0
                    ),
                )?;
                if target.is_some() {
                    return Err(e);
                }
                first_failure.get_or_insert(e);
                continue;
            }
            if self.report["launched"].is_object() {
                observation.earlier_launch(&i["id"]);
                break;
            }
        }
        if self.report["launched"].is_object() {
            Ok(())
        } else {
            first_failure.map_or(Ok(()), Err)
        }
    }
    pub fn run(&mut self, mode: &str, apply: bool) -> Result<Value> {
        let Some(_lock) = ByteLock::acquire(&self.root.join("scheduler.lock"), Duration::ZERO)?
        else {
            return Ok(json!({"busy":true}));
        };
        self.failures.clear();
        self.report = json!({});
        self.state = read_json(&self.root.join("state.json"), json!({}))?;
        if !self.state.is_object() {
            return Err(Error("AttributeError"));
        }
        let (cfg, pri) = self.settings()?;
        let (m, ps, mut jobs) = self.sample(&cfg)?;
        match mode {
            "recover" => {
                let mut safe = cfg.clone();
                safe["kill_enabled"] = json!(false);
                self.guard("reconcile", |e| e.reconcile(&mut jobs, &ps, &safe));
            }
            "step" => {
                self.guard("no action recovery", |e| {
                    e.recover_no_action(&mut jobs, &ps)
                });
                self.guard("reconcile", |e| e.reconcile(&mut jobs, &ps, &cfg));
                self.guard("memory pressure", |e| {
                    e.memory_pressure(&m, &mut jobs, &ps, &pri, &cfg)
                });
                self.guard("orphans", |e| e.orphans(&jobs, &ps, &cfg, true));
                self.guard("notifications", |e| e.notifications());
                self.guard("answers", |e| e.answers());
                if !self.failures.iter().any(|name| {
                    !["notify item ", "answer item ", "notifications", "answers"]
                        .iter()
                        .any(|prefix| name.starts_with(prefix))
                }) {
                    self.guard("dispatch", |e| e.dispatch(&cfg, &pri));
                }
            }
            "repair-step" => {
                // Keep lost question generations and their notification/reply
                // processing quarantined. General termination remains disabled;
                // only the separately verified no-action path may cancel.
                let mut safe = cfg.clone();
                safe["kill_enabled"] = json!(false);
                self.report["repair_mode"] = json!(true);
                self.guard("no action recovery", |e| {
                    e.recover_no_action(&mut jobs, &ps)
                });
                self.guard("reconcile repairs", |e| {
                    e.reconcile_repairs(&mut jobs, &ps, &safe)
                });
                if self.failures.is_empty() {
                    self.guard("dispatch", |e| e.dispatch(&safe, &pri));
                }
            }
            "orphans" => {
                self.orphans(&jobs, &ps, &cfg, apply)?;
            }
            _ => {}
        }
        self.persist()?;
        Ok(self.report.clone())
    }
}
pub fn step<B: Backend>(engine: Result<Scheduler<B>>) -> Value {
    match engine {
        Ok(mut e) => match e.run("step", false) {
            Ok(v) => v,
            Err(err) => {
                let _ = e.log("ERROR", &format!("tick failed ({})", err.0));
                json!({"error":err.0})
            }
        },
        Err(err) => {
            eprintln!("scheduler tick failed {}", err.0);
            json!({"error":err.0})
        }
    }
}
