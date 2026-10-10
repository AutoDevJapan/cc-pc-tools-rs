//! A session-independent, read-only projection of the shared operational records.
//! Never read job prompts, credentials, client workspaces or session transcripts.
use chrono::DateTime;
#[path = "commander_health.rs"]
mod commander_health;
#[path = "waiting_state.rs"]
mod waiting_state;
use serde_json::{Value, json};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    hash::{Hash, Hasher},
    io::Read,
    path::{Path, PathBuf},
    sync::{Arc, RwLock},
    time::{Duration, SystemTime, UNIX_EPOCH},
};

const DEPARTMENTS: [(&str, &str); 6] = [
    ("ゲーム部", "#1E8C93"),
    ("GoDD事業部", "#8E5BB5"),
    ("自社サービス部", "#3B73C4"),
    ("発信部", "#C2508F"),
    ("受託部", "#4E9A4E"),
    ("情報システム部", "#5C7080"),
];
const RECENT_MS: u64 = 3 * 60 * 60 * 1000;

pub struct Operations {
    root: PathBuf,
    snapshot: RwLock<Value>,
}
impl Operations {
    pub fn production() -> Self {
        Self {
            root: "D:/CC/Tools".into(),
            snapshot: RwLock::new(empty()),
        }
    }
    pub fn snapshot(&self) -> Value {
        self.snapshot
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }
    pub fn refresh(&self) {
        let value = collect(&self.root, now_ms());
        *self.snapshot.write().unwrap_or_else(|e| e.into_inner()) = value;
    }
}
pub async fn resident(board: Arc<Operations>) {
    let mut tick = tokio::time::interval(Duration::from_secs(15));
    tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    loop {
        tick.tick().await;
        let board = board.clone();
        let _ = tokio::task::spawn_blocking(move || board.refresh()).await;
    }
}
fn empty() -> Value {
    json!({"fetchedAt":null,"sourceOk":false,"sources":[],
        "commander":{"id":"commander","name":"司令塔","status":"unknown","detail":"共有データを取得中"},
        "metrics":{},"departments":departments(BTreeMap::new())})
}
fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}
fn stamp(v: &Value) -> Option<u64> {
    v.as_str()
        .and_then(|s| DateTime::parse_from_rfc3339(s).ok())
        .and_then(|t| u64::try_from(t.timestamp_millis()).ok())
}
fn todo_stamp(item: &Value) -> Option<u64> {
    stamp(&item["updated_at"]).or_else(|| {
        let raw = item["updated"].as_str()?;
        chrono::NaiveDateTime::parse_from_str(raw, "%Y-%m-%d %H:%M")
            .ok()?
            .and_local_timezone(chrono::FixedOffset::east_opt(9 * 3600)?)
            .single()
            .and_then(|t| u64::try_from(t.timestamp_millis()).ok())
    })
}
fn modified(path: &Path) -> Option<u64> {
    fs::metadata(path)
        .ok()?
        .modified()
        .ok()?
        .duration_since(UNIX_EPOCH)
        .ok()
        .map(|d| d.as_millis() as u64)
}
fn fresh(at: Option<u64>, now: u64, max_age: u64) -> bool {
    at.is_some_and(|at| at <= now + 60_000 && now.saturating_sub(at) <= max_age)
}
fn read_json(path: &Path) -> Result<Value, ()> {
    let mut bytes = Vec::new();
    fs::File::open(path)
        .map_err(|_| ())?
        .take(16 * 1024 * 1024 + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| ())?;
    if bytes.len() > 16 * 1024 * 1024 {
        return Err(());
    }
    let bytes = bytes.strip_prefix(&[0xef, 0xbb, 0xbf]).unwrap_or(&bytes);
    serde_json::from_slice(bytes).map_err(|_| ())
}
fn text<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
fn active(v: &Value) -> bool {
    matches!(
        text(v, "status"),
        "running" | "starting" | "pending" | "queued"
    )
}
fn failed(v: &Value) -> bool {
    matches!(text(v, "status"), "failed" | "lost" | "cancelled")
}
fn linked(item: &Value, job: &Value) -> bool {
    job["todo_id"]
        .as_u64()
        .is_some_and(|id| item["id"].as_u64() == Some(id))
        || item["job"] == job["id"] && job["id"].is_string()
        || item["scheduler"]["job"] == job["id"] && job["id"].is_string()
        || item["jobs"]
            .as_array()
            .is_some_and(|ids| ids.contains(&job["id"]))
}
fn safe_id(prefix: &str, value: &str) -> String {
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    value.hash(&mut hash);
    format!("{prefix}-{:016x}", hash.finish())
}
fn clean(names: &cc_ask::task_title::Names, value: &str) -> String {
    // No raw activity, errors or transcripts are ever projected. Mask embedded secrets
    // in user-entered task titles as well as registered client names.
    use std::sync::LazyLock;
    static SECRET: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(
        r#"(?i)(?:(?:api[_ -]?key|(?:access|refresh|id|discord|client)[_ -]?(?:token|secret)|token|password|passwd|secret|authorization|パスワード|認証コード)["']?\s*[:=]\s*["']?\S+|\bBearer\s+\S+|\b(?:sk-|ghp_|gho_|github_pat_)[A-Za-z0-9_-]+|https?://\S+|[A-Za-z]:[/\\]\S+)"#).unwrap()
    });
    let value = names.redact(value);
    let value = cc_ask::task_title::canonical_text(&value);
    SECRET
        .replace_all(&value, "[非表示]")
        .chars()
        .take(300)
        .collect()
}
fn project_name(names: &cc_ask::task_title::Names, project: &str) -> String {
    let masked = names.project(project);
    if masked == "クライアント案件" {
        return masked;
    }
    match project {
        "civ6" | "civ6-banseiki" => "万世紀".into(),
        "tools" | "PCツール" => "PCの道具".into(),
        "" => "その他".into(),
        _ => clean(names, &cc_ask::task_title::canonical_project(project)),
    }
}
fn department(project: &str) -> usize {
    let p = project.to_lowercase();
    if project == "クライアント案件" {
        4
    } else if ["civ6", "banseiki", "万世紀", "phosgaea", "colonization"]
        .iter()
        .any(|s| p.contains(s))
    {
        0
    } else if p.starts_with("godd") {
        1
    } else if [
        "youtube",
        "health-channel",
        "x-skill",
        "xブックマーク",
        "adj-daily-shorts",
    ]
    .iter()
    .any(|s| p.contains(s))
    {
        3
    } else if p.is_empty()
        || matches!(
            p.as_str(),
            "tools" | "pcツール" | "pcの道具" | "makhmeto" | "常駐"
        )
        || p.starts_with("opencodex")
    {
        5
    } else {
        2
    }
}
type Tree = BTreeMap<(usize, String), Vec<Value>>;
fn add(tree: &mut Tree, dept: usize, product: String, worker: Value) {
    tree.entry((dept, product)).or_default().push(worker);
}
fn departments(tree: Tree) -> Vec<Value> {
    DEPARTMENTS
        .iter()
        .enumerate()
        .map(|(i, (name, color))| {
            let products: Vec<_> = tree
                .iter()
                .filter(|((d, _), _)| *d == i)
                .map(|((_, p), w)| json!({"id":safe_id("product",p),"name":p,"workers":w}))
                .collect();
            json!({"id":format!("department-{i}"),"name":name,"color":color,"products":products})
        })
        .collect()
}
fn source(key: &str, label: &str, ok: bool, at: Option<u64>, message: &str) -> Value {
    json!({"key":key,"label":label,"ok":ok,"updatedAt":at,"message":message})
}
fn jobs(root: &Path) -> Result<Vec<Value>, ()> {
    let mut out = Vec::new();
    for entry in fs::read_dir(root.join("codex-jobs/jobs")).map_err(|_| ())? {
        let entry = entry.map_err(|_| ())?;
        if !entry.file_type().map_err(|_| ())?.is_dir() {
            continue;
        }
        let path = entry.path().join("job.json");
        if !path.exists() {
            continue;
        }
        let v = read_json(&path)?;
        if !v["status"].is_string() || !v["id"].is_string() {
            return Err(());
        }
        out.push(v);
    }
    out.sort_by(|a, b| text(b, "created_at").cmp(text(a, "created_at")));
    Ok(out)
}
fn collect(root: &Path, now: u64) -> Value {
    static QUESTION_URL: std::sync::OnceLock<regex::Regex> = std::sync::OnceLock::new();
    let question_pattern = QUESTION_URL.get_or_init(|| {
        regex::Regex::new(r"https://discord\.com/channels/[0-9]+/[0-9]+(?:/[0-9]+)?")
            .expect("fixed Discord URL pattern")
    });
    let mut sources = Vec::new();
    let privacy = cc_ask::task_title::Names::load(&root.join("client-projects.txt"));
    if privacy.is_err() {
        let mut out = empty();
        out["fetchedAt"] = json!(now);
        out["sources"] = json!([source(
            "privacy",
            "表示の保護",
            false,
            None,
            "保護対象を確認できないため作業名を表示できません"
        )]);
        return out;
    }
    let names = privacy.unwrap();
    let todo_path = root.join("todo/todo.json");
    let todo = read_json(&todo_path).and_then(|v| {
        if v["items"].is_array() && v["projects"].is_object() {
            Ok(v)
        } else {
            Err(())
        }
    });
    sources.push(source(
        "todos",
        "共有ToDo",
        todo.is_ok(),
        modified(&todo_path),
        if todo.is_ok() {
            "共有記録から取得"
        } else {
            "共有記録を取得できません"
        },
    ));
    let jobs = jobs(root);
    sources.push(source(
        "jobs",
        "ジョブ",
        jobs.is_ok(),
        Some(now),
        if jobs.is_ok() {
            "保存されたジョブから取得"
        } else {
            "ジョブ記録を取得できません"
        },
    ));
    let scheduler_path = root.join("scheduler/status.json");
    let scheduler = read_json(&scheduler_path).unwrap_or(Value::Null);
    let scheduler_at = stamp(&scheduler["measured_at"]);
    let scheduler_ok = fresh(scheduler_at, now, 180_000);
    sources.push(source(
        "scheduler",
        "CPU・メモリ・ジョブ枠",
        scheduler_ok,
        scheduler_at,
        if scheduler_ok {
            "常駐監視から取得"
        } else {
            "常駐監視の記録が古いか取得できません"
        },
    ));
    let queue_path = root.join("localai-web-rs/target/release/console-state.json");
    let queue_read = read_json(&queue_path);
    let queue = queue_read.as_ref().cloned().unwrap_or(Value::Null);
    let health_path = queue_path.with_file_name("commander-health.json");
    let health = read_json(&health_path);
    let commander = commander_health::project(
        health.as_ref().ok(),
        &queue,
        now,
        (queue_read.is_err() && queue_path.try_exists().unwrap_or(true))
            || (health.is_err() && health_path.try_exists().unwrap_or(true)),
    );
    let commander_ok = commander["status"] == "working";
    let tick_at = commander["completedAt"]
        .as_u64()
        .or(commander["startedAt"].as_u64());
    let mut commander_source = source(
        "console",
        "司令塔の巡回",
        commander_ok,
        tick_at,
        text(&commander, "detail"),
    );
    commander_source["state"] = commander["state"].clone();
    sources.push(commander_source);
    let mut tree = Tree::new();
    let mut used = BTreeSet::new();
    let js = jobs.as_ref().map(Vec::as_slice).unwrap_or(&[]);
    let db = todo.as_ref().ok();
    if let Some(items) = db.and_then(|v| v["items"].as_array()) {
        for item in items {
            let is_done = text(item, "status") == "done";
            if is_done && !fresh(todo_stamp(item), now, RECENT_MS) {
                continue;
            }
            let associated: Vec<_> = js.iter().filter(|j| linked(item, j)).collect();
            for j in &associated {
                used.insert(text(j, "id").to_owned());
            }
            let raw_project = text(item, "project");
            let project = project_name(&names, raw_project);
            let client = project == "クライアント案件"
                || names.project(text(item, "cwd")) == "クライアント案件"
                || db.is_some_and(|db| {
                    names.project(db["projects"][raw_project].as_str().unwrap_or(""))
                        == "クライアント案件"
                });
            let project = if client {
                "クライアント案件".into()
            } else {
                project
            };
            let current = associated.iter().find(|j| active(j));
            let status = if is_done {
                "done"
            } else if matches!(text(item, "status"), "block" | "wait") {
                "waiting"
            } else if let Some(j) = current {
                if text(j, "status") == "queued" {
                    "waiting"
                } else {
                    "working"
                }
            } else if text(item, "status") == "doing" {
                if jobs.is_ok()
                    && associated.first().is_some_and(|j| {
                        failed(j) && fresh(stamp(&j["finished_at"]), now, RECENT_MS)
                    })
                {
                    "attention"
                } else {
                    "waiting"
                }
            } else {
                "idle"
            };
            let task = if client {
                "クライアント案件の作業（内容は非表示）".into()
            } else {
                clean(&names, text(item, "title"))
            };
            let progress = item["progress"].as_u64().filter(|p| *p <= 100);
            let summary = if client {
                String::new()
            } else {
                clean(&names, text(item, "note"))
                    .chars()
                    .take(180)
                    .collect::<String>()
            };
            let wait_kind = match text(item, "status") {
                "block" => "owner",
                "wait" => "dependency",
                _ if status == "waiting" => waiting_state::kind(
                    item,
                    current.copied(),
                    &queue,
                    scheduler_ok
                        && scheduler["reasons"]
                            .as_array()
                            .is_some_and(|reasons| !reasons.is_empty()),
                ),
                _ => "none",
            };
            let question_url = if client {
                None
            } else {
                question_pattern
                    .find(text(item, "note"))
                    .map(|value| value.as_str().to_owned())
            };
            let history = associated.iter().filter(|j| failed(j)).count();
            let detail = match status {
                "working" => "共有ToDoに紐づく担当が稼働中です。",
                "waiting" if text(item, "status") == "block" => "オーナーの判断待ちです。",
                "waiting" if text(item, "status") == "wait" => "別のToDoの完了待ちです。",
                "waiting" if wait_kind == "job_queue" => "ジョブの起動順を待っています。",
                "waiting" if wait_kind == "startup_error" => {
                    "起動準備が失敗しています。既存の復旧処理が再確認します。"
                }
                "waiting" if wait_kind == "resource" => {
                    "実測したCPU・メモリなどの起動条件を待っています。"
                }
                "waiting" => {
                    "担当の開始または再開を待っています。CPU枠の待ちとは確定していません。"
                }
                "attention" => {
                    "未完了の作業で直近の担当が失敗または停止しました。再開状況を確認してください。"
                }
                "done" => "共有ToDoで完了が記録されています。",
                _ => "未着手です。",
            };
            add(
                &mut tree,
                department(if client { &project } else { raw_project }),
                project,
                json!({"id":format!("todo-{}",item["id"].as_u64().unwrap_or(0)),"name":if client {"担当".into()}else{task.chars().take(24).collect::<String>()},
                    "task":task,"summary":summary,"waitKind":wait_kind,"questionUrl":question_url,"status":status,"progress":progress,"updatedAt":todo_stamp(item).and_then(|n|chrono::DateTime::from_timestamp_millis(n as i64)).map(|t|t.to_rfc3339()),
                    "detail":detail,"historyFailures":history}),
            );
        }
    }
    // Standalone active jobs and recent failures are kept visible. Terminal failures
    // are history, never an unresolved incident without an open task.
    for j in js {
        if used.contains(text(j, "id")) {
            continue;
        }
        if !(active(j) || failed(j) && fresh(stamp(&j["finished_at"]), now, RECENT_MS)) {
            continue;
        }
        let inferred = infer_project(j, db);
        let project = project_name(&names, &inferred);
        let client =
            project == "クライアント案件" || names.project(text(j, "cwd")) == "クライアント案件";
        let project = if client {
            "クライアント案件".into()
        } else {
            project
        };
        let raw_title = if text(j, "task_title").is_empty() {
            text(j, "name")
        } else {
            text(j, "task_title")
        };
        let task = if client {
            "クライアント案件の作業（内容は非表示）".into()
        } else {
            clean(&names, raw_title)
        };
        let status = if text(j, "status") == "queued" {
            "waiting"
        } else if active(j) {
            "working"
        } else {
            "idle"
        };
        add(
            &mut tree,
            department(if client { &project } else { &inferred }),
            project,
            json!({"id":safe_id("job",text(j,"id")),"name":if client {"担当".into()}else{task.chars().take(24).collect::<String>()},
                "task":task,"status":status,"progress":null,"updatedAt":j["finished_at"].as_str().or(j["started_at"].as_str()),
                "detail":if active(j){"共有ジョブの担当です。"}else{"過去の失敗記録です。現在の障害を示すものではありません。"},"historyFailures":usize::from(failed(j))}),
        );
    }
    let tools = read_json(&root.join("tool-names.json")).unwrap_or(Value::Null);
    for tool in tools["tools"].as_array().into_iter().flatten() {
        let key = text(tool, "resident");
        if key.is_empty() {
            continue;
        }
        let (ok, at) = match key {
            "relay" => {
                let p = root.join("ocx-health-watchdog/status.json");
                let v = read_json(&p).unwrap_or(Value::Null);
                let at = stamp(&v["at"]);
                (v["healthy"] == true && fresh(at, now, 180_000), at)
            }
            "discord" => {
                let path = root.join("discord-ask/tick-health.json");
                let v = read_json(&path).unwrap_or(Value::Null);
                let at = v["observed_at"]
                    .as_u64()
                    .filter(|v| *v > 0)
                    .and_then(|v| v.checked_mul(1000));
                (
                    v["error"].is_null()
                        && v["stages"]["startup_recovery"] == true
                        && v["stages"]["cmd_todo_watch"] == true
                        && v["stages"]["cmd_scheduler_step"] == true
                        && fresh(at, now, 180_000),
                    at,
                )
            }
            // The guard's log is event-driven. Its absence or age does not prove
            // failure. Report an unknown state rather than invent an incident.
            "guard" => (false, None),
            _ => (false, None),
        };
        let unknown = key == "guard"
            || !matches!(key, "relay" | "discord")
            || (key == "discord"
                && root.join("discord-ask/tick-health.json").try_exists().is_ok_and(|exists| !exists));
        let label = clean(&names, text(tool, "name"));
        let mut resident_source = source(
            key,
            &label,
            ok,
            at,
            if unknown {
                "稼働状態の記録なし"
            } else if key == "discord" && ok {
                "停滞監視・容量確認・起動時復旧を確認（Discord取得は別工程）"
            } else if ok {
                "監視中"
            } else {
                "監視の応答または新しい記録を確認できません"
            },
        );
        resident_source["state"] = json!(if unknown {
            "unknown"
        } else if ok {
            "connected"
        } else {
            "unavailable"
        });
        sources.push(resident_source);
        add(
            &mut tree,
            5,
            "常駐".into(),
            json!({"id":safe_id("resident",key),"name":label,"task":clean(&names,text(tool,"role")),
            "status":if unknown{"unknown"}else if ok{"working"}else{"attention"},"progress":null,
            "updatedAt":at.and_then(|n|chrono::DateTime::from_timestamp_millis(n as i64)).map(|t|t.to_rfc3339()),
            "detail":if unknown{"常駐の稼働状態を確認できません。"}else if ok{"常駐監視の記録を確認しました。"}else{"常駐監視の応答または新しい記録を確認できません。"},"historyFailures":0}),
        );
    }
    let mut metrics = if scheduler_ok {
        json!({"cpuPercent":scheduler["cpu_percent"],
        "memoryUsedGb":scheduler["ram_total_gb"].as_f64().zip(scheduler["available_gb"].as_f64()).map(|(t,a)|t-a),
        "memoryTotalGb":scheduler["ram_total_gb"],"activeJobs":scheduler["running_jobs"],
        "jobLimit":scheduler["hard_cap"],"waitingJobs":scheduler["queue_length"],"availableSlots":scheduler["allowance_now"]})
    } else {
        json!({"cpuPercent":null,"memoryUsedGb":null,"memoryTotalGb":null,"activeJobs":js.iter().filter(|j|active(j)).count(),"jobLimit":null,"waitingJobs":null})
    };
    metrics["ownerDecisions"] = db
        .and_then(|db| db["items"].as_array())
        .map(|items| {
            json!(
                items
                    .iter()
                    .filter(|item| text(item, "status") == "block")
                    .count()
            )
        })
        .unwrap_or(Value::Null);
    // These are distinct inventories, not a total of waits for CPU capacity.
    let todo_count = |status| {
        db.and_then(|v| v["items"].as_array())
            .map(|items| items.iter().filter(|i| text(i, "status") == status).count())
    };
    metrics["todoDependencyWaits"] = json!(todo_count("wait"));
    metrics["todoPendingStarts"] = json!(todo_count("todo"));
    metrics["todoBlocked"] = json!(todo_count("block"));
    metrics["jobQueued"] = if jobs.is_ok() {
        json!(js.iter().filter(|j| text(j, "status") == "queued").count())
    } else {
        Value::Null
    };
    metrics["consoleQueued"] = json!(queue["queue"].as_array().map(Vec::len));
    metrics["consolePreparationFailures"] = json!(queue["queue"].as_array().map(|items| {
        items
            .iter()
            .filter(|t| queue["preparation_failures"][text(t, "key")].is_array())
            .count()
    }));
    metrics["capacityReasons"] = if scheduler_ok {
        scheduler["reasons"].clone()
    } else {
        Value::Null
    };
    json!({"fetchedAt":now,"sourceOk":sources.iter().all(|s|s["ok"]==true || s["state"]=="unknown"),"sources":sources,
        "commander":commander,
        "metrics":metrics,"departments":departments(tree)})
}
fn infer_project(job: &Value, db: Option<&Value>) -> String {
    let cwd = text(job, "cwd").replace('\\', "/").to_lowercase();
    if let Some(projects) = db.and_then(|v| v["projects"].as_object()) {
        let mut matched: Vec<_> = projects
            .iter()
            .filter_map(|(name, p)| {
                let p = p
                    .as_str()?
                    .replace('\\', "/")
                    .trim_end_matches('/')
                    .to_lowercase();
                let rest = cwd.strip_prefix(&p)?;
                (rest.is_empty() || rest.starts_with('/') || rest.starts_with("-wt/"))
                    .then_some((p.len(), name))
            })
            .collect();
        matched.sort_by_key(|(len, _)| std::cmp::Reverse(*len));
        if let Some((_, name)) = matched.first() {
            return (*name).clone();
        }
    }
    if !text(job, "project").is_empty() {
        text(job, "project").into()
    } else {
        cc_ask::task_title::project_from_folder(text(job, "cwd"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn shared_records_project_recovery_current_incidents_history_and_client_paths() {
        let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/operations");
        let now = stamp(&json!("2026-10-09T13:00:00Z")).unwrap();
        let output = collect(&root, now);
        let workers: Vec<_> = output["departments"]
            .as_array()
            .unwrap()
            .iter()
            .flat_map(|d| d["products"].as_array().unwrap())
            .flat_map(|p| p["workers"].as_array().unwrap())
            .collect();
        let find = |id: &str| *workers.iter().find(|w| w["id"] == id).unwrap();
        assert_eq!(find("todo-1")["status"], "working");
        assert_eq!(find("todo-1")["progress"], 40);
        assert_eq!(find("todo-1")["historyFailures"], 1);
        assert_eq!(find("todo-2")["status"], "waiting");
        assert_eq!(find("todo-2")["waitKind"], "owner");
        assert_eq!(find("todo-6")["waitKind"], "start");
        assert_eq!(find("todo-7")["waitKind"], "dependency");
        assert_eq!(find("todo-5")["summary"], "");
        assert!(find("todo-5")["questionUrl"].is_null());
        assert_eq!(output["metrics"]["ownerDecisions"], 1);
        assert!(
            find("todo-2")["summary"]
                .as_str()
                .unwrap()
                .contains("画像案への回答を待っています")
        );
        assert!(
            !find("todo-2")["summary"]
                .as_str()
                .unwrap()
                .contains("hidden-value")
        );
        assert_eq!(
            find("todo-2")["questionUrl"],
            "https://discord.com/channels/100/200"
        );
        assert!(find("todo-2")["progress"].is_null());
        assert_eq!(find("todo-3")["status"], "attention");
        assert_eq!(find("todo-4")["status"], "done");
        assert_eq!(find("todo-6")["status"], "waiting");
        let history = workers.iter().find(|w| w["task"] == "以前の失敗").unwrap();
        assert_eq!(history["status"], "idle");
        assert_eq!(history["historyFailures"], 1);
        assert_ne!(output["commander"]["status"], "attention");
        assert_eq!(output["commander"]["status"], "working");
        assert_eq!(output["sourceOk"], true);
        assert!(
            output["sources"]
                .as_array()
                .unwrap()
                .iter()
                .any(|s| s["state"] == "unknown")
        );
        assert_eq!(output["metrics"]["memoryUsedGb"], 56.0);
        assert!(find("todo-1")["updatedAt"].is_string());
        let serialized = output.to_string();
        assert!(!serialized.contains("SecretClient") && !serialized.contains("hidden-value"));
        assert!(serialized.contains("クライアント案件"));
        let stale = collect(&root, now + 300_000);
        assert!(stale["metrics"]["cpuPercent"].is_null());
        assert_eq!(stale["sourceOk"], false);
    }
    #[test]
    fn current_worker_wins_over_historical_failure_and_progress_is_only_measured() {
        let item = json!({"id":12,"jobs":["old","new"],"status":"doing"});
        let old = json!({"id":"old","status":"failed"});
        let new = json!({"id":"new","status":"running"});
        assert!(linked(&item, &old) && linked(&item, &new));
        assert!(!active(&old) && active(&new));
        assert!(item["progress"].as_u64().is_none());
    }
    #[test]
    fn client_names_and_embedded_credentials_are_masked_before_projection() {
        let names = cc_ask::task_title::Names::parse("SecretClient\n");
        let output = clean(
            &names,
            "SecretClient API_KEY=sk-do-not-display https://user:pass@host",
        );
        assert!(!output.contains("SecretClient"));
        assert!(!output.contains("do-not-display"));
        assert!(!output.contains("user:pass"));
        for input in [
            "client_secret=hidden-value",
            "access_token=hidden-value",
            r#""refresh_token":"hidden-value""#,
            "パスワード=hidden-value",
        ] {
            assert!(!clean(&names, input).contains("hidden-value"));
        }
        assert_eq!(project_name(&names, "secretclient"), "クライアント案件");
    }
    #[test]
    fn project_inference_observes_path_boundaries_and_longest_match() {
        let db = json!({"projects":{"a":"D:/dev/a","ab":"D:/dev/ab","tool":"D:/dev/a/tools"}});
        assert_eq!(
            infer_project(&json!({"cwd":"D:/dev/ab-wt/branch"}), Some(&db)),
            "ab"
        );
        assert_eq!(
            infer_project(&json!({"cwd":"D:/dev/a/tools"}), Some(&db)),
            "tool"
        );
        assert_ne!(
            infer_project(&json!({"cwd":"D:/dev/abcd"}), Some(&db)),
            "ab"
        );
    }
    #[test]
    fn stale_and_future_monitor_records_cannot_report_current_capacity() {
        assert!(!fresh(Some(1), 300_000, 180_000));
        assert!(!fresh(Some(400_001), 300_000, 180_000));
        assert!(fresh(Some(290_000), 300_000, 180_000));
        assert_eq!(departments(Tree::new()).len(), 6);
    }
}
