//! Durable stall recovery. Never hold todo.lock across tools that also edit ToDo.
use crate::{Lock, Notifier, Paths, Result, STALL_S, find, load, normalized, now, save, time_s};
use cc_scheduler::recovery_failure::{self as failure, Failure, SLOW_RETRY_S};
use cc_scheduler::{
    backend::{Backend, RealBackend},
    common::{self, arr, epoch, n, s},
    engine::Scheduler,
    policy::{self, Processes},
};
use serde_json::{Value, json};
use std::{fs, path::PathBuf, time::Duration};

pub const HANG_S: f64 = 1800.;
pub const RETRY_WINDOW_S: f64 = 21600.;
const CLAIM_S: f64 = 300.;

fn discord_wait(item: &Value, t: f64) -> bool {
    let reserved = n(&item["stall_recovery"]["pending"]["discord_reserved_at"]);
    let history = arr(&item["discord_restart_history"]);
    failure::retry_at(&history, t) > t && !(reserved > 0. && t - reserved < RETRY_WINDOW_S)
}

pub fn under(cwd: &str, path: &str) -> bool {
    if cwd.is_empty() || path.is_empty() {
        return false;
    }
    let cwd = normalized(cwd);
    let base = normalized(path);
    cwd == base || cwd.starts_with(&(base.clone() + "\\")) || cwd.starts_with(&(base + "-wt\\"))
}
pub fn excluded(project: &str, path: &str, clients: &str) -> bool {
    project.eq_ignore_ascii_case("owner")
        || clients
            .trim_start_matches('\u{feff}')
            .lines()
            .map(str::trim)
            .filter(|n| !n.is_empty() && !n.starts_with('#'))
            .any(|name| {
                project.to_lowercase().contains(&name.to_lowercase())
                    || path.to_lowercase().contains(&name.to_lowercase())
            })
}
pub fn last_activity(j: &Value) -> f64 {
    epoch(&j["last_event_at"])
        .max(epoch(&j["started_at"]))
        .max(epoch(&j["created_at"]))
}
fn healthy(j: &Value, ps: &Processes, t: f64) -> bool {
    policy::live(j) && policy::alive(j, ps, t) && t - last_activity(j) < HANG_S
}
fn linked(item: &Value, j: &Value) -> bool {
    // A working directory identifies a checkout, not the work item that owns it.
    (item["id"].is_number()
        && item["id"] == j["todo_id"]
        && (s(&j["project"]).is_empty() || item["project"] == j["project"]))
        || item["jobs"]
            .as_array()
            .into_iter()
            .flatten()
            .any(|id| id == &j["id"] || (j["thread_id"].is_string() && id == &j["thread_id"]))
        || (item["scheduler"]["job"].is_string() && item["scheduler"]["job"] == j["id"])
        || (item["stall_recovery"]["pending"]["key"].is_string()
            && item["stall_recovery"]["pending"]["key"] == j["launch_key"])
}

fn stopped(item: &Value, job: &Value) -> bool {
    item["cancel_requested"] == true
        || item["stopped"] == true
        || job["cancel_requested"] == true
        || (matches!(s(&job["status"]), "cancelled" | "canceled")
            && !(job["id"].is_string()
                && item["stall_recovery"]["pending"]["recovery_cancel"] == job["id"]))
        || (common::truth(&item["scheduler"]["question"])
            && !common::truth(&item["scheduler"]["answer"]))
        || (common::truth(&item["scheduler"]["answer"])
            && ["停止したまま", "再開しない", "止めておく"]
                .iter()
                .any(|text| s(&item["note"]).contains(text)))
        || cc_scheduler::backend::result_outcome(s(&job["last_message"])) == "block"
}

fn latest_linked(item: &Value, jobs: &[Value]) -> Value {
    jobs.iter()
        .filter(|job| linked(item, job))
        .max_by(|a, b| {
            epoch(&a["created_at"])
                .total_cmp(&epoch(&b["created_at"]))
                .then_with(|| s(&a["id"]).cmp(s(&b["id"])))
        })
        .cloned()
        .unwrap_or(Value::Null)
}

fn overlap(left: &str, right: &str) -> bool {
    if left.is_empty() || right.is_empty() {
        return false;
    }
    let left = normalized(left);
    let right = normalized(right);
    left == right || left.starts_with(&(right.clone() + "\\")) || right.starts_with(&(left + "\\"))
}

/// A reservation may share a project, but may never share a writer's checkout.
pub fn workspace(item: &Value, job: &Value, db: &Value, jobs: &[Value], path: &str) -> Value {
    let own_latest = item["id"].is_number() && job["todo_id"] == item["id"];
    let candidates = if own_latest {
        [s(&job["cwd"]), s(&item["scheduler"]["worktree"]), path]
    } else {
        [s(&item["scheduler"]["worktree"]), s(&job["cwd"]), path]
    };
    let cwd = candidates
        .into_iter()
        .find(|cwd| !cwd.is_empty())
        .unwrap_or(path);
    let dedicated = !s(&item["scheduler"]["worktree"]).is_empty()
        || normalized(cwd).contains("-wt\\")
        || (!s(&job["cwd"]).is_empty() && normalized(cwd) == normalized(s(&job["cwd"])));
    let busy = jobs.iter().any(|other| {
        policy::live(other) && other["id"] != job["id"] && overlap(cwd, s(&other["cwd"]))
    }) || db["items"].as_array().into_iter().flatten().any(|other| {
        other["id"] != item["id"]
            && matches!(s(&other["status"]), "todo" | "doing")
            && other["stall_recovery"]["pending"].is_object()
            && other["stall_recovery"]["pending"]["workspace"]["isolate_worktree"] != true
            && overlap(
                cwd,
                s(&other["stall_recovery"]["pending"]["workspace"]["cwd"]),
            )
    });
    json!({"cwd":cwd,"isolate_worktree":!dedicated || busy})
}

fn recent_continuations(item: &Value, jobs: &[Value], t: f64) -> usize {
    jobs.iter()
        .filter(|job| {
            let at = epoch(&job["finished_at"]).max(last_activity(job));
            linked(item, job)
                && matches!(s(&job["status"]), "succeeded" | "finished")
                && cc_scheduler::backend::result_outcome(s(&job["last_message"])) == "todo"
                && at > 0.
                && at <= t
                && t - at < RETRY_WINDOW_S
        })
        .count()
}
pub fn hash(text: &str) -> String {
    let h = |seed| {
        text.bytes().fold(seed, |h: u64, c| {
            (h ^ u64::from(c)).wrapping_mul(0x100000001b3)
        })
    };
    format!(
        "{:016x}{:016x}",
        h(0xcbf29ce484222325),
        h(0x84222325cbf29ce4)
    )
}
pub fn continuation(item: &Value, job: &Value) -> String {
    let id = &item["id"];
    let mut prompt = format!(
        "前回のジョブが止まったので再開してください。共有ToDo #{id} の続きから。\n作業: {}\n補足: {}\nAPIキーを使わずOAuthのサブスク枠だけを使う。新しいコードはRustで書き、Pythonは使わない。\n",
        s(&item["title"]),
        s(&item["note"])
    );
    let repeats = item["stall_recovery"]["recent_continuations"]
        .as_u64()
        .unwrap_or(0);
    if repeats >= 2 {
        prompt.push_str(&format!(
            "直近6時間に同じ作業の未完了の引き継ぎが{repeats}回続いています。進め方を設計から見直してください。元のToDoを維持し、細分化したToDoを増やすだけで終えず、完了条件・残作業・実際の阻害原因を整理し、元の依頼を完了できる実装と検証まで進めてください。不足の説明や反復回数だけでオーナーの判断待ちにしないでください。\n"
        ));
    }
    prompt.push_str("別の担当と同じ作業フォルダを共有せず、この項目の既存の担当用worktreeか隔離したworktreeで作業してください。\n");
    for k in ["last_message", "last_error"] {
        let text = cc_scheduler::backend::safe_text(s(&job[k]));
        if !text.is_empty() {
            prompt.push_str(&format!(
                "\n前回の{k}（引き継ぎ資料）:\n{}\n",
                text.chars().take(6000).collect::<String>()
            ));
        }
    }
    if !s(&job["recovery_prompt"]).is_empty() {
        prompt.push_str(&format!(
            "\n元の作業指示:\n{}\n",
            cc_scheduler::backend::safe_text(s(&job["recovery_prompt"]))
        ));
    }
    if item["dispatchable"] == true && !s(&item["spec"]).is_empty() {
        prompt.push_str(&format!(
            "\n元の受入条件は次の保存済み仕様を読んで満たしてください: {}\n",
            s(&item["spec"])
        ));
    }
    let mut end = prompt.len().min(60 * 1024);
    while !prompt.is_char_boundary(end) {
        end -= 1;
    }
    prompt.truncate(end);
    prompt
}

/// Plan reservations only; no job launch, cancellation or Discord in this function.
pub fn plan(
    db: &mut Value,
    jobs: &[Value],
    ps: &Processes,
    t: f64,
    clients: &str,
) -> Result<Vec<i64>> {
    if !db["stalls"].is_object() {
        db["stalls"] = json!({});
    }
    let projects = db["projects"]
        .as_object()
        .ok_or("invalid projects")?
        .clone();
    // Migrate only the exact hold created by the old watchdog; preserve owner decisions.
    for row in db["items"].as_array_mut().ok_or("invalid items")? {
        if row["status"] == "block"
            && s(&row["note"])
                == "6時間以内に自動再開が2回止まったため、3回目の起動を止めました。原因を調べてから再開してください。"
            && !excluded(
                s(&row["project"]),
                projects.get(s(&row["project"])).map(s).unwrap_or(""),
                clients,
            )
            && !excluded(
                s(&row["project"]),
                s(&latest_linked(row, jobs)["cwd"]),
                clients,
            )
            && !excluded(
                s(&row["project"]),
                s(&row["scheduler"]["worktree"]),
                clients,
            )
            && !stopped(row, &latest_linked(row, jobs))
        {
            row["status"] = json!("doing");
            row["note"] = json!(
                "停滞監視の回数制限による保留を解除しました。原因に応じて復旧し、再試行を続けます。"
            );
            row["stall_recovery"]["report"] = Value::Null;
        }
    }
    let mut selected = vec![];
    for (project, path) in projects {
        let path = s(&path);
        if excluded(&project, path, clients) {
            continue;
        }
        let mut items: Vec<Value> = db["items"]
            .as_array()
            .ok_or("invalid items")?
            .iter()
            .filter(|i| i["project"] == project && matches!(s(&i["status"]), "todo" | "doing"))
            .cloned()
            .collect();
        if items.is_empty() {
            db["stalls"].as_object_mut().unwrap().remove(&project);
            continue;
        }
        items.sort_by_key(|i| (s(&i["status"]) != "doing", i["id"].as_i64().unwrap_or(0)));
        let mut unattended = false;
        for mut item in items {
            if excluded(&project, s(&item["scheduler"]["worktree"]), clients) {
                continue;
            }
            // Each item owns only its own durable reservation.
            if item["stall_recovery"]["pending"].is_object() {
                unattended = true;
                continue;
            }
            if discord_wait(&item, t) || n(&item["stall_recovery"]["next_at"]) > t {
                unattended = true;
                continue;
            }
            let id = item["id"].as_i64().ok_or("invalid item id")?;
            if jobs.iter().any(|j| linked(&item, j) && healthy(j, ps, t)) {
                if item["stall_recovery"].is_object() {
                    find(db, id)?["stall_recovery"]["since"] = Value::Null;
                }
                continue;
            }
            let job = latest_linked(&item, jobs);
            // An explicit cancellation is not a crash. Do not undo a person's or
            // another agent's deliberate stop/duplicate cleanup.
            if stopped(&item, &job) {
                continue;
            }
            if excluded(&project, s(&job["cwd"]), clients) {
                continue;
            }
            unattended = true;
            // Unrelated jobs cannot reset this item's absence timer.
            let latest = if policy::live(&job) {
                last_activity(&job)
            } else {
                epoch(&job["finished_at"])
            };
            let since = if latest > 0. {
                latest.min(t)
            } else {
                item["stall_recovery"]["since"].as_f64().unwrap_or_else(|| {
                    let created = epoch(&item["created_s"]);
                    if created > 0. {
                        created.min(t)
                    } else {
                        db["stalls"][&project]["since"].as_f64().unwrap_or(t)
                    }
                })
            };
            let row = find(db, id)?;
            if !row["stall_recovery"].is_object() {
                row["stall_recovery"] = json!({});
            }
            row["stall_recovery"]["since"] = json!(since);
            let failed_recovery = (!arr(&item["stall_recovery"]["history"]).is_empty()
                || !arr(&item["discord_restart_history"]).is_empty())
                && matches!(s(&job["status"]), "failed" | "lost");
            let continuing = matches!(s(&job["status"]), "succeeded" | "finished")
                && cc_scheduler::backend::result_outcome(s(&job["last_message"])) == "todo";
            if t - since < STALL_S && !policy::live(&job) && !failed_recovery && !continuing {
                continue;
            }
            let (kind, reason) = if policy::live(&job) {
                if policy::alive(&job, ps, t) && t - last_activity(&job) < HANG_S {
                    continue;
                }
                (
                    "restart",
                    if policy::alive(&job, ps, t) {
                        "30分以上進捗がなかった"
                    } else {
                        "ジョブのプロセスが消えた"
                    },
                )
            } else if s(&item["status"]) == "doing" || !arr(&item["jobs"]).is_empty() {
                (
                    "delegate",
                    match s(&job["status"]) {
                        "failed" => "担当ジョブが失敗した",
                        "lost" => "担当ジョブの記録が途切れた",
                        "succeeded" | "finished" => "担当ジョブが終了した",
                        _ => "担当ジョブが動いていなかった",
                    },
                )
            } else if arr(&item["jobs"]).is_empty()
                && item["dispatchable"] == true
                && !s(&item["spec"]).is_empty()
            {
                ("scheduler", "仕様のある未着手の作業が20分以上止まっていた")
            } else if arr(&item["jobs"]).is_empty() {
                ("delegate", "未着手の作業が20分以上止まっていた")
            } else {
                continue;
            };
            let history: Vec<Value> = arr(if item["discord_restart_thread"].is_string() {
                &item["discord_restart_history"]
            } else {
                &item["stall_recovery"]["history"]
            })
            .into_iter()
            .filter(|at| t - n(at) < RETRY_WINDOW_S)
            .collect();
            let repeats = recent_continuations(&item, jobs, t);
            if !item["stall_recovery"].is_object() {
                item["stall_recovery"] = json!({});
            }
            item["stall_recovery"]["recent_continuations"] = json!(repeats);
            let work = workspace(&item, &job, db, jobs, path);
            let row = find(db, id)?;
            if !row["stall_recovery"].is_object() {
                row["stall_recovery"] = json!({});
            }
            row["stall_recovery"]["history"] = json!(history);
            row["stall_recovery"]["recent_continuations"] = json!(repeats);
            let evidence = s(&job["recovery_evidence"]);
            let evidence = if evidence.is_empty() {
                s(&job["last_error"])
            } else {
                evidence
            };
            let class = failure::classify(evidence);
            row["stall_recovery"]["failure"] = json!(class.name());
            row["stall_recovery"]["cause"] = json!(failure::summary(evidence));
            if history.len() >= 2 && row["stall_recovery"]["notice_sent"] != true {
                row["stall_recovery"]["report"] = json!(format!(
                    "【{project}】#{id} {}：原因: {}。試した処置: {}。復旧を続け、失敗時は1時間ごとに再試行します。",
                    s(&row["title"]),
                    failure::summary(if evidence.is_empty() {
                        reason
                    } else {
                        evidence
                    }),
                    (if s(&row["stall_recovery"]["tried"]).is_empty() {
                        "同じ作業を2回再開"
                    } else {
                        s(&row["stall_recovery"]["tried"])
                    })
                    .to_owned()
                    .chars()
                    .take(300)
                    .collect::<String>()
                ));
            }
            let due = failure::retry_at(&history, t);
            if due > t {
                row["stall_recovery"]["next_at"] = json!(due);
                if row["discord_restart_thread"].is_string() {
                    row["discord_restart_not_before"] = json!(due);
                }
                continue;
            }
            {
                let attempt = row["stall_recovery"]["attempt"].as_u64().unwrap_or(0) + 1;
                let request = format!("stall-{id}-{attempt}");
                let key = hash(&format!("manual:{request}"));
                row["stall_recovery"]["attempt"] = json!(attempt);
                let repaired = failure::repaired_job(&job, class);
                let resume = if work["isolate_worktree"] == true {
                    None
                } else {
                    session(&repaired)
                };
                let prompt = continuation(&item, &job);
                let kind = if class != Failure::Task && job.is_object() {
                    "restart"
                } else {
                    kind
                };
                row["stall_recovery"]["pending"] = json!({"request":request,"key":key,"kind":kind,"reason":reason,"job":repaired,"resume_session":resume,"workspace":work,"recent_continuations":repeats,"prompt":prompt,"reserved_at":t,"failure":class.name()});
            }
            selected.push(id);
        }
        if !unattended {
            db["stalls"].as_object_mut().unwrap().remove(&project);
        }
    }
    Ok(selected)
}

pub enum Outcome {
    Started(String),
    Queued,
    Deferred,
    Obsolete,
}
pub trait RecoveryBackend {
    fn remedy(&mut self, class: Failure) -> Result<String> {
        Ok(class.remedy().into())
    }
    fn handoff(&mut self, job: &Value) -> Result<String> {
        Ok(s(&job["recovery_prompt"]).into())
    }
    fn observed(&self) -> Value {
        json!({})
    }
    fn clock(&self) -> f64;
    fn clients(&mut self) -> Result<String>;
    fn snapshot(&mut self) -> Result<(Vec<Value>, Processes)>;
    fn perform(&mut self, item: &Value, pending: &Value, db: &Value) -> Result<Outcome>;
}
fn edit<T>(paths: &Paths, f: impl FnOnce(&mut Value) -> Result<T>) -> Result<T> {
    let _guard = Lock::acquire(&paths.lock, Duration::from_secs(2))?;
    let mut db = load(&paths.store)?;
    let v = f(&mut db)?;
    save(&paths.store, &db)?;
    Ok(v)
}
fn release_claim(paths: &Paths, id: i64, token: &str) -> Result<()> {
    edit(paths, |db| {
        let row = find(db, id)?;
        if row["stall_recovery"]["pending"]["claim"] == token {
            common::remove(&mut row["stall_recovery"]["pending"], "claim");
            common::remove(&mut row["stall_recovery"]["pending"], "claim_until");
        }
        Ok(())
    })
}
pub fn run_watch(
    paths: &Paths,
    notifier: &mut dyn Notifier,
    backend: &mut dyn RecoveryBackend,
) -> Result<()> {
    let lock = paths.store.with_file_name("watch.lock");
    let Ok(_watch) = Lock::acquire(&lock, Duration::ZERO) else {
        return Ok(());
    };
    let clients = backend.clients()?; // Missing exclusions must never authorize a launch.
    let t = backend.clock();
    let (jobs, ps) = backend.snapshot()?;
    edit(paths, |db| {
        let mut previous = backend.observed();
        if let Some(own) = db["stall_observed"].as_object() {
            for (key, value) in own {
                previous[key] = value.clone();
            }
        }
        db["stall_observed"] = policy::ownership(&jobs, &ps, &previous);
        plan(db, &jobs, &ps, t, &clients)?;
        Ok(())
    })?;
    // Reservations and claims serialize effects; tools must never inherit a state lock.
    drop(_watch);
    let ids: Vec<i64> = arr(&load(&paths.store)?["items"])
        .iter()
        .filter(|i| {
            i["stall_recovery"]["pending"].is_object() || i["stall_recovery"]["report"].is_string()
        })
        .filter_map(|i| i["id"].as_i64())
        .collect();
    let mut notice_failed = false;
    for id in ids {
        let db = load(&paths.store)?;
        let Some(item) = arr(&db["items"]).into_iter().find(|i| i["id"] == id) else {
            continue;
        };
        let project = s(&item["project"]);
        let path = s(&db["projects"][project]);
        if excluded(project, path, &clients) {
            continue;
        }
        let p = &item["stall_recovery"]["pending"];
        if p.is_object() {
            if excluded(project, s(&p["job"]["cwd"]), &clients)
                || excluded(project, s(&p["workspace"]["cwd"]), &clients)
            {
                continue;
            }
            if !matches!(s(&item["status"]), "todo" | "doing") {
                edit(paths, |db| {
                    let row = find(db, id)?;
                    if row["stall_recovery"]["pending"]["request"] == p["request"] {
                        row["stall_recovery"]["pending"] = Value::Null;
                    }
                    Ok(())
                })?;
                continue;
            }
            let (fresh_jobs, fresh_ps) = backend.snapshot()?;
            let launched =
                p["key"].is_string() && fresh_jobs.iter().any(|j| j["launch_key"] == p["key"]);
            if !launched && stopped(&item, &latest_linked(&item, &fresh_jobs)) {
                edit(paths, |db| {
                    let row = find(db, id)?;
                    if row["stall_recovery"]["pending"]["request"] == p["request"] {
                        row["stall_recovery"]["pending"] = Value::Null;
                    }
                    Ok(())
                })?;
                continue;
            }
            if !launched
                && fresh_jobs
                    .iter()
                    .any(|j| linked(&item, j) && healthy(j, &fresh_ps, backend.clock()))
            {
                edit(paths, |db| {
                    let row = find(db, id)?;
                    if row["stall_recovery"]["pending"]["request"] == p["request"] {
                        row["stall_recovery"]["pending"] = Value::Null;
                    }
                    Ok(())
                })?;
                continue;
            }
            if !launched && discord_wait(&item, backend.clock()) {
                continue;
            }
            if !launched && n(&item["stall_recovery"]["next_at"]) > backend.clock() {
                continue;
            }
            if fresh_jobs.iter().any(|job| {
                p["job"]["id"].is_string()
                    && job["id"] == p["job"]["id"]
                    && job["cancel_requested"] == true
            }) {
                edit(paths, |db| {
                    let row = find(db, id)?;
                    if row["stall_recovery"]["pending"]["request"] == p["request"] {
                        row["stall_recovery"]["pending"] = Value::Null;
                    }
                    Ok(())
                })?;
                continue;
            }
            // Reserve the shared attempt before launching, so a lost commit/receipt
            // cannot let intake start a third job in the same window.
            if item["discord_restart_thread"].is_string() {
                let t = backend.clock();
                let old = n(&p["discord_reserved_at"]);
                if old == 0. || t - old >= RETRY_WINDOW_S {
                    let mut allowed = false;
                    edit(paths, |db| {
                        let row = find(db, id)?;
                        if row["stall_recovery"]["pending"]["request"] != p["request"] {
                            return Ok(());
                        }
                        let mut history: Vec<Value> = arr(&row["discord_restart_history"])
                            .into_iter()
                            .filter(|at| t - n(at) < RETRY_WINDOW_S)
                            .collect();
                        if failure::retry_at(&history, t) > t {
                            row["discord_restart_not_before"] =
                                json!(failure::retry_at(&history, t));
                            return Ok(());
                        }
                        history.push(json!(t));
                        row["discord_restart_history"] = json!(history);
                        row["stall_recovery"]["pending"]["discord_reserved_at"] = json!(t);
                        row["discord_restart_not_before"] = json!(failure::retry_at(&history, t));
                        allowed = true;
                        Ok(())
                    })?;
                    if !allowed {
                        continue;
                    }
                }
            }
            let token = uuid::Uuid::new_v4().to_string();
            let claimed = edit(paths, |db| {
                let row = find(db, id)?;
                if row["stall_recovery"]["pending"]["request"] != p["request"]
                    || !matches!(s(&row["status"]), "todo" | "doing")
                    || (!launched
                        && n(&row["stall_recovery"]["pending"]["claim_until"]) > backend.clock())
                {
                    return Ok(false);
                }
                row["stall_recovery"]["pending"]["claim"] = json!(token);
                row["stall_recovery"]["pending"]["claim_until"] = json!(backend.clock() + CLAIM_S);
                Ok(true)
            })?;
            if !claimed {
                continue;
            }
            // Capture the original instructions once before the first enqueue.
            // Subsequent retries must preserve the accepted request's exact body.
            let mut effective = p.clone();
            if !launched && p["handoff_saved"] != true && p["job"].is_object() {
                if let Ok(handoff) = backend.handoff(&p["job"]) {
                    if !handoff.is_empty() {
                        effective["job"]["recovery_prompt"] = json!(handoff);
                        effective["prompt"] = json!(continuation(&item, &effective["job"]));
                    }
                }
                effective["handoff_saved"] = json!(true);
                edit(paths, |db| {
                    let row = find(db, id)?;
                    if row["stall_recovery"]["pending"]["request"] == p["request"]
                        && row["stall_recovery"]["pending"]["claim"] == token
                    {
                        for key in ["job", "prompt", "handoff_saved"] {
                            row["stall_recovery"]["pending"][key] = effective[key].clone();
                        }
                    }
                    Ok(())
                })?;
            }
            let p = &effective;
            let class = failure::classify(s(&p["job"]["recovery_evidence"]));
            let class = match s(&p["failure"]) {
                "console" => Failure::Console,
                "proxy" => Failure::Proxy,
                "model" => Failure::Model,
                "context" => Failure::Context,
                "writer" => Failure::Writer,
                _ => class,
            };
            let mut remedy_result = Ok(());
            if !launched
                && matches!(class, Failure::Console | Failure::Proxy)
                && p["remedy_done"] != true
            {
                let at = backend.clock();
                if n(&db["stall_console_retry_at"]) > at {
                    release_claim(paths, id, &token)?;
                    continue;
                }
                // Global reservation prevents every failed project from restarting the console.
                let reserved = edit(paths, |db| {
                    if n(&db["stall_console_retry_at"]) > at {
                        return Ok(false);
                    }
                    db["stall_console_retry_at"] = json!(at + SLOW_RETRY_S);
                    Ok(true)
                })?;
                if !reserved {
                    release_claim(paths, id, &token)?;
                    continue;
                }
                let remedy = backend.remedy(class);
                edit(paths, |db| {
                    let row = find(db, id)?;
                    if row["stall_recovery"]["pending"]["request"] != p["request"] {
                        return Ok(());
                    }
                    row["stall_recovery"]["tried"] = json!(match &remedy {
                        Ok(detail) => detail.clone(),
                        Err(error) =>
                            format!("{}（失敗: {}）", class.remedy(), failure::summary(error)),
                    });
                    if remedy.is_ok() {
                        row["stall_recovery"]["pending"]["remedy_done"] = json!(true);
                    }
                    Ok(())
                })?;
                remedy_result = remedy.map(|_| ());
                if remedy_result.is_ok() {
                    edit(paths, |db| {
                        find(db, id)?["stall_recovery"]["next_at"] = json!(at + 60.);
                        Ok(())
                    })?;
                    release_claim(paths, id, &token)?;
                    continue; // Give the scheduled launcher time to replace the resident.
                }
            }
            match remedy_result.and_then(|_| backend.perform(&item, p, &db)) {
                Ok(Outcome::Started(job)) => {
                    edit(paths, |db| {
                        let row = find(db, id)?;
                        if row["stall_recovery"]["pending"]["request"] != p["request"] {
                            return Ok(());
                        }
                        if matches!(s(&row["status"]), "todo" | "doing") {
                            row["status"] = json!("doing");
                        }
                        if !arr(&row["jobs"]).contains(&json!(job)) {
                            common::push(&mut row["jobs"], json!(job));
                        }
                        // The old scheduler owner must not reconcile the cancelled job over this continuation.
                        if p["kind"] != "scheduler"
                            && row["scheduler"].is_object()
                            && row["scheduler"]["job"] != job
                        {
                            row["scheduler"]["last_job"] = row["scheduler"]["job"].clone();
                            row["scheduler"]["job"] = Value::Null;
                            row["scheduler"]["key"] = Value::Null;
                            row["scheduler"]["phase"] = json!("watchdog");
                            common::remove(&mut row["scheduler"], "pending_cancel");
                            common::remove(&mut row["scheduler"], "notice_pending");
                        }
                        row["updated"] = json!(now());
                        let at = backend.clock();
                        common::push(&mut row["stall_recovery"]["history"], json!(at));
                        if row["discord_restart_thread"].is_string() {
                            let mut history: Vec<Value> = arr(&row["discord_restart_history"])
                                .into_iter()
                                .filter(|at| backend.clock() - n(at) < RETRY_WINDOW_S)
                                .collect();
                            if !row["stall_recovery"]["pending"]["discord_reserved_at"].is_number()
                            {
                                history.push(json!(at));
                            }
                            row["discord_restart_history"] = json!(history);
                            row["discord_restart_not_before"] =
                                json!(failure::retry_at(&history, at));
                        }
                        let verb = if p["kind"] == "restart" {
                            if session(&p["job"]).is_some() {
                                "終了・再開"
                            } else {
                                "終了・再起動"
                            }
                        } else {
                            "再起動"
                        };
                        row["stall_recovery"]["tried"] = json!(class.remedy());
                        row["stall_recovery"]["next_at"] = json!(failure::retry_at(
                            &arr(&row["stall_recovery"]["history"]),
                            at
                        ));
                        if row["stall_recovery"]["notice_sent"] != true
                            && row["stall_recovery"]["report"].is_null()
                        {
                            row["stall_recovery"]["report"] = json!(format!(
                                "【{project}】#{id} {}：{}ため、{verb}しました（ジョブ {job}）",
                                s(&row["title"]),
                                s(&p["reason"])
                            ));
                        }
                        row["stall_recovery"]["pending"] = Value::Null;
                        Ok(())
                    })?;
                }
                Ok(Outcome::Obsolete) => {
                    edit(paths, |db| {
                        let row = find(db, id)?;
                        if row["stall_recovery"]["pending"]["request"] == p["request"] {
                            row["stall_recovery"]["pending"] = Value::Null;
                        }
                        Ok(())
                    })?;
                }
                Ok(Outcome::Queued | Outcome::Deferred) => {}
                Err(error) => {
                    let cause = failure::summary(&error);
                    let actual = failure::classify(&error);
                    let mut repaired = failure::repaired_job(&p["job"], actual);
                    if matches!(actual, Failure::Context | Failure::Model) {
                        repaired["recovery_prompt"] = json!(backend.handoff(&p["job"])?);
                        repaired["last_error"] = json!(cause);
                    }
                    edit(paths, |db| {
                        let row = find(db, id)?;
                        if row["stall_recovery"]["pending"]["request"] != p["request"] {
                            return Ok(());
                        }
                        row["stall_recovery"]["last_error"] = json!(cause);
                        if actual != Failure::Task {
                            row["stall_recovery"]["pending"]["failure"] = json!(actual.name());
                            row["stall_recovery"]["pending"]["job"] = repaired.clone();
                            row["stall_recovery"]["pending"]["resume_session"] =
                                if p["workspace"]["isolate_worktree"] == true {
                                    Value::Null
                                } else {
                                    json!(session(&repaired))
                                };
                            row["stall_recovery"]["pending"]["kind"] = json!("restart");
                            row["stall_recovery"]["pending"]["remedy_done"] = Value::Null;
                            if matches!(actual, Failure::Context | Failure::Model) {
                                row["stall_recovery"]["pending"]["prompt"] =
                                    json!(continuation(&item, &repaired));
                            }
                        }
                        row["stall_recovery"]["next_at"] = json!(backend.clock() + SLOW_RETRY_S);
                        if row["stall_recovery"]["notice_sent"] != true {
                            row["stall_recovery"]["report"] = json!(format!(
                                "【{project}】#{id} {}：原因: {cause}。試した処置: {}。1時間後に同じ依頼で再試行します。",
                                s(&row["title"]),
                                class.remedy()
                            ));
                        }
                        Ok(())
                    })?;
                    eprintln!("stall recovery #{id}: {cause}; retry in one hour");
                }
            }
            release_claim(paths, id, &token)?;
        }
        let fresh = load(&paths.store)?;
        let report = arr(&fresh["items"])
            .into_iter()
            .find(|i| i["id"] == id)
            .and_then(|i| i["stall_recovery"]["report"].as_str().map(str::to_owned));
        if let Some(report) = report {
            if let Err(error) = notifier.notify(paths, &report, false) {
                eprintln!(
                    "stall recovery #{id}: 原因の通知を再試行します: {}",
                    failure::summary(&error)
                );
                notice_failed = true;
                continue;
            }
            println!("{report}");
            edit(paths, |db| {
                let row = find(db, id)?;
                if row["stall_recovery"]["report"] != report {
                    return Ok(());
                }
                if report.contains("原因:") {
                    row["stall_recovery"]["notice_sent"] = json!(true);
                }
                row["stall_recovery"]["report"] = Value::Null;
                Ok(())
            })?;
        }
    }
    if notice_failed {
        Err("停滞の通知に失敗しました。作業の復旧は継続し、本文を保持しました".into())
    } else {
        Ok(())
    }
}
pub fn session(job: &Value) -> Option<String> {
    ["thread_id", "resume_session"].iter().find_map(|k| {
        uuid::Uuid::parse_str(s(&job[*k]))
            .ok()
            .map(|id| id.to_string())
    })
}
/// Durable queue input: console owns capacity, session exclusivity and checkout creation.
pub fn delegation_request(item: &Value, pending: &Value) -> Value {
    json!({
        "id":item["id"],
        "request_id":pending["request"],
        "prompt":pending["prompt"],
        "source_job_id":pending["job"]["id"],
        "resume_session":if pending["workspace"]["isolate_worktree"] == true { None } else { session(&pending["job"]) },
        "cwd":pending["workspace"]["cwd"],
        "isolate_worktree":pending["workspace"]["isolate_worktree"] != false,
        "model":pending["job"]["model"],
        "effort":pending["job"]["effort"]
    })
}
/// A dead parent doesn't erase ownership; a reused PID or another live owner does.
pub fn owned_helpers(job: &Value, jobs: &[Value], ps: &Processes, observed: &Value) -> Vec<Value> {
    if !job["id"].is_string() {
        return vec![];
    }
    ps.values()
        .filter(|p| {
            policy::identity(p).is_some_and(|key| observed[&key]["job"] == job["id"])
                && !jobs.iter().any(|other| {
                    other["id"] != job["id"]
                        && policy::live(other)
                        && policy::process_tree(other, ps).contains(&(n(&p["pid"]) as u32))
                })
        })
        .cloned()
        .collect()
}

pub struct RealRecovery {
    scheduler: Scheduler<RealBackend>,
    paths: Paths,
}
impl RealRecovery {
    pub fn new(paths: &Paths) -> Self {
        let root = PathBuf::from("D:/CC/Tools/scheduler");
        Self {
            scheduler: Scheduler::new(root.clone(), paths.store.clone(), RealBackend::new(root))
                .expect("scheduler directory"),
            paths: paths.clone(),
        }
    }
    fn tool(&mut self, program: &str, args: &[String]) -> Result<String> {
        let c = common::Invocation {
            program: program.into(),
            args: args.to_vec(),
            cwd: None,
            timeout: Some(Duration::from_secs(45)),
            git_prompt_off: false,
        };
        let out = self
            .scheduler
            .backend
            .runner
            .run(&c)
            .map_err(|e| e.0.to_owned())?;
        if out.code != 0 {
            return Err(format!(
                "{program} 終了コード {}: {}",
                out.code,
                failure::summary(&format!("{}\n{}", out.stderr, out.stdout))
            ));
        }
        Ok(out.stdout)
    }
    fn cleanup(&mut self, job: &Value, observed: &Value) -> Result<()> {
        let ps = self
            .scheduler
            .backend
            .snapshot()
            .map_err(|e| e.0.to_owned())?;
        let jobs = self.scheduler.backend.jobs().map_err(|e| e.0.to_owned())?;
        for p in owned_helpers(job, &jobs, &ps, observed) {
            if !self
                .scheduler
                .backend
                .kill_orphan(&p)
                .map_err(|e| e.0.to_owned())?
                && cc_scheduler::native::process_birth(n(&p["pid"]) as u32) == p["created"].as_u64()
            {
                return Err("補助プロセスの終了を確認できません".into());
            }
        }
        Ok(())
    }
}
impl RecoveryBackend for RealRecovery {
    fn handoff(&mut self, job: &Value) -> Result<String> {
        Ok(
            fs::read_to_string(self.paths.jobs.join(s(&job["id"])).join("prompt.md"))
                .unwrap_or_default()
                .chars()
                .take(24000)
                .collect(),
        )
    }
    fn remedy(&mut self, class: Failure) -> Result<String> {
        if matches!(class, Failure::Console | Failure::Proxy) {
            if !failure::reserve_console(&self.scheduler.root, time_s())? {
                return Err("コンソールの再起動は1時間に1回です。前の復旧を待ちます".into());
            }
            self.tool(
                "schtasks.exe",
                &["/Run".into(), "/TN".into(), failure::CONSOLE_TASK.into()],
            )?;
            // A running scheduled watcher ignores /Run while HTTP health looks good.
            // Ask the resident to exit; only the scheduled watcher creates its replacement.
            let exit = self.scheduler.backend.http.request(
                "POST",
                "http://127.0.0.1:10012/api/resident/restart",
                Some(&json!({})),
                false,
                10,
            );
            if let Err(error) = exit {
                return Ok(format!(
                    "定期タスクを起動。終了APIに接続できず: {}。60秒後にジョブを再試行",
                    error.0
                ));
            }
        }
        Ok(class.remedy().into())
    }
    fn observed(&self) -> Value {
        common::read_json(&self.scheduler.root.join("state.json"),json!({})).unwrap_or(json!({}))["observed"].as_object().map(|o|Value::Object(o.clone())).unwrap_or(json!({}))
    }
    fn clock(&self) -> f64 {
        time_s()
    }
    fn clients(&mut self) -> Result<String> {
        fs::read_to_string("D:/CC/Tools/client-projects.txt").map_err(|e| e.to_string())
    }
    fn snapshot(&mut self) -> Result<(Vec<Value>, Processes)> {
        let mut jobs = vec![];
        for entry in fs::read_dir(&self.paths.jobs).map_err(|e| e.to_string())? {
            let file = entry.map_err(|e| e.to_string())?.path().join("job.json");
            if !file.exists() {
                continue;
            }
            let mut job: Value = serde_json::from_str(
                &fs::read_to_string(&file).map_err(|_| "ジョブ記録を読めません")?,
            )
            .map_err(|_| "ジョブ記録が壊れています")?;
            if !job["id"].is_string() || !job["status"].is_string() {
                return Err("ジョブ記録に識別子か状態がありません".into());
            }
            if !policy::live(&job) {
                job["recovery_evidence"] = json!(failure::evidence(&job, file.parent().unwrap()));
                if matches!(
                    failure::classify(s(&job["recovery_evidence"])),
                    Failure::Context | Failure::Model
                ) {
                    job["recovery_prompt"] = json!(
                        fs::read_to_string(file.parent().unwrap().join("prompt.md"))
                            .unwrap_or_default()
                            .chars()
                            .take(24000)
                            .collect::<String>()
                    );
                }
            }
            jobs.push(job);
        }
        let ps = self
            .scheduler
            .backend
            .snapshot()
            .map_err(|e| e.0.to_owned())?;
        Ok((jobs, ps))
    }
    fn perform(&mut self, item: &Value, p: &Value, db: &Value) -> Result<Outcome> {
        let (mut jobs, ps) = self.snapshot()?;
        if let Some(job) = jobs
            .iter()
            .find(|job| p["key"].is_string() && job["launch_key"] == p["key"])
        {
            return Ok(Outcome::Started(s(&job["id"]).into()));
        }
        let fresh = load(&self.paths.store)?;
        let Some(row) = arr(&fresh["items"])
            .into_iter()
            .find(|row| row["id"] == item["id"])
        else {
            return Ok(Outcome::Obsolete);
        };
        if row["stall_recovery"]["pending"]["request"] != p["request"]
            || !matches!(s(&row["status"]), "todo" | "doing")
            || stopped(&row, &latest_linked(&row, &jobs))
        {
            return Ok(Outcome::Obsolete);
        }
        if discord_wait(&row, time_s()) {
            return Ok(Outcome::Deferred);
        }
        let project = s(&row["project"]);
        let path = s(&fresh["projects"][project]);
        let clients = self.clients()?;
        if excluded(project, path, &clients)
            || excluded(project, s(&p["job"]["cwd"]), &clients)
            || excluded(project, s(&p["workspace"]["cwd"]), &clients)
        {
            return Ok(Outcome::Deferred);
        }
        if jobs
            .iter()
            .any(|job| linked(&row, job) && healthy(job, &ps, time_s()))
        {
            return Ok(Outcome::Obsolete);
        }
        let mut job = jobs
            .iter()
            .find(|job| p["job"]["id"].is_string() && job["id"] == p["job"]["id"])
            .cloned()
            .unwrap_or(p["job"].clone());
        if job["cancel_requested"] == true {
            return Ok(Outcome::Obsolete);
        }
        let observed = policy::ownership(&jobs, &ps, &db["stall_observed"]);
        if policy::live(&job) || job["status"] == "lost" {
            // Record our stop intent before the tool; a crash must not confuse it
            // with an explicit owner cancellation or lose the existing request.
            let reserved = edit(&self.paths, |db| {
                let row = find(db, item["id"].as_i64().ok_or("invalid item id")?)?;
                if row["stall_recovery"]["pending"]["request"] != p["request"]
                    || !matches!(s(&row["status"]), "todo" | "doing")
                {
                    return Ok(false);
                }
                row["stall_recovery"]["pending"]["recovery_cancel"] = job["id"].clone();
                Ok(true)
            })?;
            if !reserved {
                return Ok(Outcome::Obsolete);
            }
            if !self
                .scheduler
                .backend
                .cancel_for_recovery(&mut job)
                .map_err(|e| e.0.to_owned())?
            {
                return Err("ジョブを終了できません".into());
            }
        }
        if job.is_object() {
            self.cleanup(&job, &observed)?;
        }
        // Console queues every item even when capacity is exhausted. Its common
        // admission policy rechecks capacity, ownership and worktrees at launch.
        jobs = self.scheduler.backend.jobs().map_err(|e| e.0.to_owned())?;
        let ps = self
            .scheduler
            .backend
            .snapshot()
            .map_err(|e| e.0.to_owned())?;
        if policy::alive(&job, &ps, time_s()) {
            return Err("終了後もプロセスが残っています".into());
        }
        if jobs
            .iter()
            .any(|other| linked(&row, other) && healthy(other, &ps, time_s()))
        {
            return Ok(Outcome::Obsolete);
        }
        let latest = load(&self.paths.store)?;
        let Some(row) = arr(&latest["items"])
            .into_iter()
            .find(|row| row["id"] == item["id"])
        else {
            return Ok(Outcome::Obsolete);
        };
        if row["stall_recovery"]["pending"]["request"] != p["request"]
            || !matches!(s(&row["status"]), "todo" | "doing")
            || stopped(&row, &latest_linked(&row, &jobs))
        {
            return Ok(Outcome::Obsolete);
        }
        let payload = delegation_request(&row, p);
        let result = self
            .scheduler
            .backend
            .http
            .request(
                "POST",
                "http://127.0.0.1:10012/api/agent/delegate",
                Some(&payload),
                false,
                45,
            )
            .map_err(|e| format!("app-server client: コンソールへの委譲に失敗: {}", e.0))?;
        let result = result.get("data").unwrap_or(&result);
        if let Some(id) = result["job_id"].as_str() {
            return Ok(Outcome::Started(id.into()));
        }
        if result["accepted"] == true && result["suppressed_reason"].is_null() {
            return Ok(Outcome::Queued);
        }
        if result["suppressed_reason"].is_string() {
            return Ok(Outcome::Obsolete);
        }
        Err("委譲を受け付けられません".into())
    }
}
pub fn restart_args(
    job: &Value,
    item: &Value,
    project_path: &str,
    key: &str,
    prompt: &std::path::Path,
) -> Vec<String> {
    let val = |k, default: &str| {
        let v = s(&job[k]);
        if v.is_empty() {
            default.to_owned()
        } else {
            v.to_owned()
        }
    };
    let profile = cc_job_policy::model_profile(s(&job["name"]), s(&job["task_type"]));
    let mut args = vec![
        "run".into(),
        "--name".into(),
        format!("stall-todo-{}", item["id"]),
        "--task-title".into(),
        s(&item["title"]).into(),
        "--project".into(),
        s(&item["project"]).into(),
        "--todo-id".into(),
        item["id"].to_string(),
        "--model".into(),
        val("model", profile.model),
        "--effort".into(),
        val("effort", profile.effort),
        "--cwd".into(),
        val("cwd", project_path),
        "--sandbox".into(),
        val("sandbox", "danger-full-access"),
        "--mcp-profile".into(),
        val("mcp_profile", "minimal"),
        "--prompt-file".into(),
        prompt.to_string_lossy().into(),
        "--launch-key".into(),
        key.into(),
    ];
    if let Some(id) = session(job) {
        args.extend(["--resume-session".into(), id]);
    }
    for dir in arr(&job["add_dirs"]) {
        args.extend(["--add-dir".into(), s(&dir).into()]);
    }
    if job["network"] == true && job["sandbox"] == "workspace-write" {
        args.push("--network".into());
    }
    args
}
