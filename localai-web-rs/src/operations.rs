//! A session-independent, read-only projection of the shared operational records.
//! Never read job prompts, credentials, client workspaces or session transcripts.
use chrono::DateTime;
#[path = "commander_health.rs"]
mod commander_health;
#[path = "waiting_state.rs"]
mod waiting_state;
use serde_json::{json, Value};
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
    questions: RwLock<Value>,
}
impl Operations {
    pub fn production() -> Self {
        Self {
            root: "D:/CC/Tools".into(),
            snapshot: RwLock::new(empty()),
            questions: RwLock::new(Value::Null),
        }
    }
    pub fn snapshot(&self) -> Value {
        self.snapshot
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }
    pub fn refresh(&self) {
        let live = self
            .questions
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone();
        let value = collect_with_questions(&self.root, now_ms(), &live);
        *self.snapshot.write().unwrap_or_else(|e| e.into_inner()) = value;
    }
    pub fn worker_detail(&self, id: &str, offset: usize) -> Result<Value, u16> {
        let names = cc_ask::task_title::Names::load(&self.root.join("client-projects.txt"))
            .map_err(|_| 503u16)?;
        let db = cc_todo::read_store(&self.root.join("todo/todo.json")).map_err(|_| 503u16)?;
        let items = db["items"].as_array().ok_or(503u16)?;
        if !db["projects"].is_object() {
            return Err(503);
        }
        let jobs = jobs(&self.root).map_err(|_| 503u16)?;
        let mut privacy = crate::console_ui::PrivacyIndex::new(&names, &db);
        if id.starts_with("job-") {
            let job = jobs
                .iter()
                .find(|job| safe_id("job", text(job, "id")) == id)
                .ok_or(404u16)?;
            if privacy.is_private_job(job) {
                return Err(404);
            }
            return Ok(
                json!({"ok":true,"id":id,"updatedAt":job["finished_at"].as_str().or(job["started_at"].as_str()),
                "detailVersion":detail_version(job),"report":crate::console_ui::clean(&names,text(job,"last_message")),"note":"","history":[],"offset":0,"total":0,"nextOffset":null}),
            );
        }
        let todo_id = id
            .strip_prefix("todo-")
            .and_then(|id| id.parse::<u64>().ok())
            .ok_or(404u16)?;
        let item = items
            .iter()
            .find(|item| item["id"].as_u64() == Some(todo_id))
            .ok_or(404u16)?;
        let client = [
            text(item, "project"),
            text(item, "cwd"),
            text(item, "spec"),
            db["projects"][text(item, "project")].as_str().unwrap_or(""),
        ]
        .iter()
        .any(|value| names.project(value) == "クライアント案件")
            || jobs
                .iter()
                .filter(|job| linked(item, job))
                .any(|job| privacy.is_private_job(job));
        if client {
            return Err(404);
        }
        let total = crate::console_ui::history_count(item);
        let history = crate::console_ui::history_page(&names, item, offset, 10);
        let next = offset.saturating_add(history.len());
        Ok(
            json!({"ok":true,"id":id,"updatedAt":todo_stamp(item).and_then(|n|chrono::DateTime::from_timestamp_millis(n as i64)).map(|t|t.to_rfc3339()),
            "detailVersion":detail_version(item),"note":crate::console_ui::clean(&names,text(item,"note")),"history":history,
            "offset":offset,"total":total,"nextOffset":(next < total).then_some(next)}),
        )
    }
}
pub async fn resident(board: Arc<Operations>) {
    let inspection = board.clone();
    tokio::spawn(async move {
        let mut tick = tokio::time::interval(Duration::from_secs(120));
        tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        loop {
            tick.tick().await;
            let board = inspection.clone();
            let _ = tokio::task::spawn_blocking(move || {
                let previous = board
                    .questions
                    .read()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone();
                let value = crate::owner_decisions::inspect(&board.root, &previous);
                *board.questions.write().unwrap_or_else(|e| e.into_inner()) = value;
                // The 15-second resident alone collects the board. Inspection
                // publishes its facts for the next tick without a second scan.
            })
            .await;
        }
    });
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
        || ["id", "thread_id", "resume_session"].iter().any(|key| {
            job[*key]
                .as_str()
                .filter(|id| !id.is_empty())
                .is_some_and(|id| {
                    item["job"].as_str() == Some(id)
                        || item["scheduler"]["job"].as_str() == Some(id)
                        || item["jobs"]
                            .as_array()
                            .is_some_and(|ids| ids.iter().any(|v| v.as_str() == Some(id)))
                })
        })
}
fn safe_id(prefix: &str, value: &str) -> String {
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    value.hash(&mut hash);
    format!("{prefix}-{:016x}", hash.finish())
}
fn detail_version(value: &Value) -> String {
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    for key in [
        "note",
        "history",
        "note_history",
        "spec_history",
        "last_message",
        "updated",
        "updated_at",
        "finished_at",
        "started_at",
    ] {
        value[key].to_string().hash(&mut hash);
    }
    format!("{:016x}", hash.finish())
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
#[cfg(test)]
fn department(project: &str) -> usize {
    let p = project.to_lowercase();
    if project == "クライアント案件"
        || matches!(p.as_str(), "arps" | "socialpilot" | "social pilot")
    {
        4
    } else if matches!(p.as_str(), "skills" | "skill") {
        5
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
        || p.starts_with("cc-")
        || matches!(p.as_str(), "localai-web-rs" | "chatgpt-web")
    {
        5
    } else {
        2
    }
}
type Tree = BTreeMap<(usize, String), Vec<Value>>;
fn rank_department(rank: u32) -> usize {
    match rank {
        10 => 4,
        20 => 0,
        30 => 5,
        40 => 3,
        50 => 5,
        60..=67 => 1,
        _ => 2,
    }
}
#[cfg(test)]
#[test]
fn owner_department_order_and_explicit_projects_match_admission() {
    for (p, d) in [
        ("ARPS", 4),
        ("SocialPilot", 4),
        ("localai-web-rs", 5),
        ("Skills", 5),
    ] {
        assert_eq!(department(p), d);
        assert_eq!(rank_department(cc_scheduler::priority::project_rank(p)), d);
    }
    let values = departments(Tree::new());
    assert_eq!(
        values
            .iter()
            .map(|d| d["name"].as_str().unwrap())
            .collect::<Vec<_>>(),
        [
            "受託部",
            "ゲーム部",
            "情報システム部",
            "発信部",
            "GoDD事業部",
            "自社サービス部"
        ]
    );
}
fn add(tree: &mut Tree, dept: usize, product: String, worker: Value) {
    let mut worker = worker;
    let defaults = crate::work_facts::project(&Value::Null, false, str::to_owned);
    for (key, value) in defaults.as_object().unwrap() {
        worker
            .as_object_mut()
            .unwrap()
            .entry(key.clone())
            .or_insert(value.clone());
    }
    tree.entry((dept, product)).or_default().push(worker);
}
fn departments(tree: Tree) -> Vec<Value> {
    [4, 0, 5, 3, 1, 2]
        .into_iter()
        .map(|i| {
            let (name, color) = DEPARTMENTS[i];
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
#[cfg(test)]
fn collect(root: &Path, now: u64) -> Value {
    collect_with_questions(
        root,
        now,
        &read_json(&root.join("discord-ask/operations-history.json")).unwrap_or(Value::Null),
    )
}
// A missing name registry must not expose any free text or erase the available
// activity metadata. This projection does not create or substitute a registry.
fn protected_metadata(root: &Path, now: u64) -> Value {
    let todo_path = root.join("todo/todo.json");
    let todo = cc_todo::read_store(&todo_path)
        .ok()
        .filter(|db| db["items"].is_array() && db["projects"].is_object());
    let job_result = jobs(root);
    let js = job_result.as_ref().map(Vec::as_slice).unwrap_or(&[]);
    let policy = read_json(&root.join("scheduler/priorities.json")).unwrap_or(Value::Null);
    let mut tree = Tree::new();
    if todo.as_ref().is_some_and(|db| {
        db["projects"].as_object().is_some_and(|projects| {
            projects
                .keys()
                .any(|project| matches!(project.to_ascii_lowercase().as_str(), "skills" | "skill"))
        })
    }) {
        tree.entry((5, "Skills".into())).or_default();
    }
    let mut used = BTreeSet::new();
    let mut unnamed = BTreeMap::<String, String>::new();
    let mut project_label = |raw: &str| {
        if matches!(raw.to_ascii_lowercase().as_str(), "skills" | "skill") {
            "Skills".to_owned()
        } else {
            let ordinal = unnamed.len() + 1;
            unnamed
                .entry(raw.to_owned())
                .or_insert_with(|| format!("作業名の確認待ち {ordinal}"))
                .clone()
        }
    };
    if let Some(items) = todo.as_ref().and_then(|db| db["items"].as_array()) {
        for item in items {
            let Some(id) = item["id"].as_u64() else {
                continue;
            };
            let done = text(item, "status") == "done";
            if done && !fresh(todo_stamp(item), now, RECENT_MS) {
                continue;
            }
            let associated: Vec<_> = js.iter().filter(|job| linked(item, job)).collect();
            for job in &associated {
                used.insert(text(job, "id").to_owned());
            }
            let current = associated.iter().copied().find(|job| active(job));
            let status = if done {
                "done"
            } else if current.is_some_and(|job| text(job, "status") != "queued") {
                "working"
            } else if text(item, "status") == "doing"
                && job_result.is_ok()
                && associated.first().is_some_and(|job| {
                    failed(job) && fresh(stamp(&job["finished_at"]), now, RECENT_MS)
                })
            {
                "attention"
            } else if matches!(
                text(item, "status"),
                "doing" | "block" | "wait" | "recovery_unverified"
            ) || current.is_some()
            {
                "waiting"
            } else {
                "idle"
            };
            let raw_project = text(item, "project");
            let skills = matches!(
                raw_project.to_ascii_lowercase().as_str(),
                "skills" | "skill"
            );
            let detail = if text(item, "status") == "recovery_unverified" {
                "復元した作業記録を確認中です。作業名と本文は表示を保留しています。"
            } else {
                "保護設定を復旧中のため、作業名と本文は表示を保留しています。Skills以外の部署所属は確認中です。"
            };
            add(
                &mut tree,
                if skills {
                    5
                } else {
                    rank_department(cc_scheduler::priority::item_rank(item, &policy))
                },
                project_label(raw_project),
                json!({"id":format!("todo-{id}"),"name":format!("作業 #{id}"),"task":"作業内容の表示を保留中", "status":status,
                    "progress":item["progress"].as_u64().filter(|n|*n<=100),
                    "updatedAt":todo_stamp(item).and_then(|at|chrono::DateTime::from_timestamp_millis(at as i64)).map(|at|at.to_rfc3339()),
                    "detail":detail,"detailAvailable":false,"historyFailures":job_result.as_ref().ok().map(|_|associated.iter().filter(|job|failed(job)).count()),
                    "waitingKind":if text(item,"status")=="wait" {"dependency"} else if text(item,"status")=="recovery_unverified" {"technical"} else {"unverified"}}),
            );
        }
    }
    for job in js.iter().filter(|job| {
        (active(job) || (failed(job) && fresh(stamp(&job["finished_at"]), now, RECENT_MS)))
            && !used.contains(text(job, "id"))
    }) {
        let raw_project = infer_project(job, todo.as_ref());
        let skills = matches!(
            raw_project.to_ascii_lowercase().as_str(),
            "skills" | "skill"
        );
        add(
            &mut tree,
            if skills {
                5
            } else {
                rank_department(cc_scheduler::priority::project_rank(&raw_project))
            },
            project_label(&raw_project),
            json!({"id":safe_id("job",text(job,"id")),"name":"名称を確認中の担当","task":"作業内容の表示を保留中",
                "status":if failed(job) {"attention"} else if text(job,"status")=="queued" {"waiting"} else {"working"},"progress":null,
                "updatedAt":stamp(&job["finished_at"]).or_else(||stamp(&job["started_at"])).and_then(|at|chrono::DateTime::from_timestamp_millis(at as i64)).map(|at|at.to_rfc3339()),
                "detail":"保護設定を復旧中です。作業名・本文・部署所属は確認中です。","detailAvailable":false,"historyFailures":if failed(job){Some(1u64)}else{None},"waitingKind":"unverified"}),
        );
    }
    let count = |status| {
        todo.as_ref()
            .and_then(|db| db["items"].as_array())
            .map(|items| {
                items
                    .iter()
                    .filter(|item| text(item, "status") == status)
                    .count()
            })
    };
    let mut out = empty();
    out["fetchedAt"] = json!(now);
    out["protectionMode"] = json!("masked_metadata");
    out["sourceOk"] = json!(false);
    out["metadataOk"] = json!(todo.is_some() && job_result.is_ok());
    out["departments"] = json!(departments(tree));
    out["metrics"] = json!({"activeJobs":job_result.as_ref().ok().map(|jobs|jobs.iter().filter(|job|matches!(text(job,"status"),"running"|"starting"|"pending")).count()),
        "jobQueued":job_result.as_ref().ok().map(|jobs|jobs.iter().filter(|job|text(job,"status")=="queued").count()),
        "todoDependencyWaits":count("wait"),"todoPendingStarts":count("todo"),"todoBlocked":count("block"),"recoveryUnverified":count("recovery_unverified")});
    out["sources"] = json!([
        source("privacy","作業名の保護",false,None,"保護設定の原本を取得できません。作業名・本文は伏せています。Skills以外の部署所属は確認中です。"),
        source("todos","共有ToDo",todo.is_some(),modified(&todo_path),"記録された状態と実測進捗だけを取得"),
        source("jobs","ジョブ",job_result.is_ok(),Some(now),"記録された実行状態だけを取得")]);
    out
}
fn collect_with_questions(root: &Path, now: u64, live: &Value) -> Value {
    static QUESTION_URL: std::sync::OnceLock<regex::Regex> = std::sync::OnceLock::new();
    let question_pattern = QUESTION_URL.get_or_init(|| {
        regex::Regex::new(r"https://discord\.com/channels/[0-9]+/[0-9]+(?:/[0-9]+)?")
            .expect("fixed Discord URL pattern")
    });
    let mut sources = Vec::new();
    let privacy = cc_ask::task_title::Names::load(&root.join("client-projects.txt"));
    if privacy.is_err() {
        return protected_metadata(root, now);
    }
    let names = privacy.unwrap();
    let policy = read_json(&root.join("scheduler/priorities.json")).unwrap_or(Value::Null);
    let todo_path = root.join("todo/todo.json");
    let todo = cc_todo::read_store(&todo_path)
        .map_err(|_| ())
        .and_then(|v| {
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
    let empty_db = Value::Null;
    let mut question_state =
        cc_ask::question_store::read(&root.join("discord-ask/state.json")).unwrap_or(Value::Null);
    if question_state.is_object() {
        question_state["operations_jobs"] = json!(crate::owner_decisions::job_metadata(
            js,
            &names,
            db.unwrap_or(&empty_db)
        ));
        question_state["jobs_source_ok"] = json!(jobs.is_ok());
    }
    let decisions = crate::owner_decisions::project(
        db.unwrap_or(&empty_db),
        &question_state,
        live,
        &names,
        now,
    );
    sources.push(source(
        "questions",
        "Discordの質問・回答・配送",
        decisions["sourceOk"] == true,
        live["checkedAt"].as_u64(),
        if decisions["sourceOk"] == true {
            "質問の投稿ごとに照合"
        } else {
            "質問の記録を取得できません"
        },
    ));
    let mut privacy = crate::console_ui::PrivacyIndex::new(&names, db.unwrap_or(&empty_db));
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
            let owned = &policy["items"][item["id"].as_u64().unwrap_or(0).to_string()];
            let display_project = owned["project"]
                .as_str()
                .or_else(|| item["owner_project"].as_str())
                .unwrap_or(raw_project);
            let project = project_name(&names, display_project);
            let client = project == "クライアント案件"
                || cc_scheduler::priority::item_rank(item, &policy) == 10
                || names.project(text(item, "cwd")) == "クライアント案件"
                || names.project(text(item, "spec")) == "クライアント案件"
                || associated.iter().any(|job| privacy.is_private_job(job))
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
            let facts = crate::work_facts::project(item, client, |value| {
                crate::console_ui::clean(&names, value)
            });
            let decision_kind = decisions["tasks"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|t| t["id"] == item["id"])
                .map(|t| text(t, "kind"))
                .unwrap_or("none");
            let wait_kind = match text(item, "status") {
                "block" => {
                    if decision_kind == "none" {
                        "unverified"
                    } else {
                        decision_kind
                    }
                }
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
                decisions["questions"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter(|q| {
                        q["taskIds"]
                            .as_array()
                            .is_some_and(|ids| ids.contains(&item["id"]))
                    })
                    .rev()
                    .find_map(|q| q["questionUrl"].as_str().map(str::to_owned))
                    .or_else(|| {
                        question_pattern
                            .find(text(item, "note"))
                            .map(|value| value.as_str().to_owned())
                    })
            };
            let history = associated.iter().filter(|j| failed(j)).count();
            let detail = match status {
                "working" => "共有ToDoに紐づく担当が稼働中です。",
                "waiting" if wait_kind == "owner" => "送信済みの質問への回答を待っています。",
                "waiting" if wait_kind == "answer_received" => {
                    "回答を受領しています。担当への配送・反映を確認中です。"
                }
                "waiting" if wait_kind == "deferred" => "オーナーが指定した延期を保持しています。",
                "waiting" if wait_kind == "owner_stopped" => "オーナーの停止指示を保持しています。",
                "waiting" if wait_kind == "unsent" => "具体的な質問をまだ送信できていません。",
                "waiting" if wait_kind == "technical" => "技術上の停止からの再開を待っています。",
                "waiting" if wait_kind == "unverified" => "停止理由と質問の配送を確認中です。",
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
            let mut worker = json!({"id":format!("todo-{}",item["id"].as_u64().unwrap_or(0)),"name":if client {"担当".into()}else{task.chars().take(24).collect::<String>()},
                "task":task,"waitKind":wait_kind,"questionUrl":question_url,"status":status,"progress":progress,"updatedAt":todo_stamp(item).and_then(|n|chrono::DateTime::from_timestamp_millis(n as i64)).map(|t|t.to_rfc3339()),
                "detail":detail,"historyFailures":history});
            if !client {
                worker["factsSource"] =
                    json!(format!("共有ToDo #{}", item["id"].as_u64().unwrap_or(0)));
                worker["note"] = json!(crate::console_ui::clean(&names, text(item, "note"))
                    .chars()
                    .take(360)
                    .collect::<String>());
                worker["historyCount"] = json!(crate::console_ui::history_count(item));
                worker["detailAvailable"] = json!(true);
                worker["detailVersion"] = json!(detail_version(item));
            }
            worker
                .as_object_mut()
                .unwrap()
                .extend(facts.as_object().unwrap().clone());
            add(
                &mut tree,
                if client {
                    4
                } else {
                    rank_department(cc_scheduler::priority::item_rank(item, &policy))
                },
                project,
                worker,
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
        let client = project == "クライアント案件"
            || names.project(text(j, "cwd")) == "クライアント案件"
            || cc_scheduler::priority::item_rank(&json!({"project":inferred}), &policy) == 10;
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
        let mut worker = json!({"id":safe_id("job",text(j,"id")),"name":if client {"担当".into()}else{task.chars().take(24).collect::<String>()},
                "task":task,"status":status,"progress":null,"updatedAt":j["finished_at"].as_str().or(j["started_at"].as_str()),
                "detail":if active(j){"共有ジョブの担当です。"}else{"過去の失敗記録です。現在の障害を示すものではありません。"},"historyFailures":usize::from(failed(j))});
        if !client {
            worker["report"] = json!(crate::console_ui::clean(&names, text(j, "last_message"))
                .chars()
                .take(360)
                .collect::<String>());
            worker["detailAvailable"] = json!(true);
            worker["detailVersion"] = json!(detail_version(j));
        }
        let facts =
            crate::work_facts::project(j, client, |value| crate::console_ui::clean(&names, value));
        worker
            .as_object_mut()
            .unwrap()
            .extend(facts.as_object().unwrap().clone());
        add(
            &mut tree,
            if client {
                4
            } else {
                let mut owned = j.clone();
                owned["project"] = json!(inferred);
                rank_department(cc_scheduler::priority::item_rank(&owned, &policy))
            },
            project,
            worker,
        );
    }
    let tools = read_json(&root.join("tool-names.json")).unwrap_or(Value::Null);
    for tool in tools["tools"].as_array().into_iter().flatten() {
        let key = text(tool, "resident");
        if key.is_empty() {
            continue;
        }
        let mut observation_only = false;
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
                observation_only = v["mode"] == "observe_only"
                    && v["stages"]["cmd_todo_watch"] == true
                    && v["stages"]["cmd_scheduler_step"] == true
                    && fresh(at, now, 180_000);
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
                && root
                    .join("discord-ask/tick-health.json")
                    .try_exists()
                    .is_ok_and(|exists| !exists));
        let label = clean(&names, text(tool, "name"));
        let mut resident_source = source(
            key,
            &label,
            ok,
            at,
            if observation_only {
                "停止監視・容量確認を観測しました。起動時復旧は保留中です。"
            } else if unknown {
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
            "detail":if observation_only{"停止監視・容量確認を観測しました。起動時復旧は保留中です。"}else if unknown{"常駐の稼働状態を確認できません。"}else if ok{"常駐監視の記録を確認しました。"}else{"常駐監視の応答または新しい記録を確認できません。"},"historyFailures":0}),
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
    metrics
        .as_object_mut()
        .unwrap()
        .extend(decisions["metrics"].as_object().unwrap().clone());
    // These are different inventories; none is a count of CPU-bound waits.
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
        "metrics":metrics,"ownerQuestions":decisions["questions"],"decisionTasks":decisions["tasks"],"blockedDecisionTasks":decisions["blockedTasks"],"departments":departments(tree)})
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
    fn board_is_bounded_but_paged_detail_keeps_notes_facts_and_privacy() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();
        fs::create_dir_all(root.join("todo")).unwrap();
        fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
        fs::write(root.join("client-projects.txt"), "SecretClient\n").unwrap();
        let note = format!(
            "要約: 接続復旧\n現在の工程: 検査\n次の行動: 再取得\n{}",
            "詳細な本文".repeat(3000)
        );
        let history: Vec<_> = (0..41).map(|i| json!({"note":format!("記録{i}:{}", "履歴".repeat(5000)),"updated":"2026-10-11 10:00"})).collect();
        let db = json!({"projects":{"demo":"D:/test/demo","SecretClient":"D:/private"},"items":[
            {"id":1,"project":"demo","status":"doing","title":"接続復旧","progress":42,"note":note,"note_history":history,"updated":"2026-10-11 10:00","wait_on":[2]},
            {"id":2,"project":"SecretClient","status":"doing","title":"hidden-value","note":"hidden-value"}]});
        fs::write(root.join("todo/todo.json"), db.to_string()).unwrap();
        let board = Operations {
            root: root.into(),
            snapshot: RwLock::new(empty()),
            questions: RwLock::new(Value::Null),
        };
        board.refresh();
        let snapshot = board.snapshot();
        let workers: Vec<_> = snapshot["departments"]
            .as_array()
            .unwrap()
            .iter()
            .flat_map(|dept| dept["products"].as_array().unwrap())
            .flat_map(|p| p["workers"].as_array().unwrap())
            .collect();
        let worker = workers.iter().find(|w| w["id"] == "todo-1").unwrap();
        assert!(worker["history"].is_null());
        assert_eq!(worker["historyCount"], 41);
        assert!(worker["note"].as_str().unwrap().chars().count() <= 360);
        assert_eq!(worker["progress"], 42);
        assert_eq!(worker["currentStage"], "検査");
        assert_eq!(worker["nextAction"], "再取得");
        assert_eq!(worker["dependencies"], json!([2]));
        assert!(snapshot.to_string().len() < 20_000);
        assert!(!snapshot.to_string().contains("hidden-value"));
        let mut offset = 0;
        let mut pages = Vec::new();
        loop {
            let detail = board.worker_detail("todo-1", offset).unwrap();
            assert_eq!(detail["note"], note);
            assert_eq!(detail["updatedAt"], worker["updatedAt"]);
            let page = detail["history"].as_array().unwrap();
            assert!(page.len() <= 10);
            pages.extend(page.iter().cloned());
            match detail["nextOffset"].as_u64() {
                Some(next) => offset = next as usize,
                None => break,
            }
        }
        assert_eq!(pages.len(), 41);
        assert_eq!(
            pages,
            crate::console_ui::history(
                &cc_ask::task_title::Names::parse("SecretClient\n"),
                &db["items"][0]
            )
        );
        assert_eq!(board.worker_detail("todo-2", 0), Err(404));
        assert_eq!(board.worker_detail("todo-999", 0), Err(404));
        let original = board.worker_detail("todo-1", 0).unwrap();
        let mut changed = db.clone();
        changed["items"][0]["note"] = json!("同じ分の別メモ");
        fs::write(root.join("todo/todo.json"), changed.to_string()).unwrap();
        let changed = board.worker_detail("todo-1", 10).unwrap();
        assert_eq!(original["updatedAt"], changed["updatedAt"]);
        assert_ne!(original["detailVersion"], changed["detailVersion"]);
        fs::write(root.join("todo/todo.json"), "invalid").unwrap();
        assert_eq!(board.worker_detail("todo-1", 0), Err(503));
        fs::write(root.join("todo/todo.json"), db.to_string()).unwrap();
        fs::create_dir(root.join("codex-jobs/jobs/broken")).unwrap();
        fs::write(root.join("codex-jobs/jobs/broken/job.json"), "invalid").unwrap();
        assert_eq!(board.worker_detail("todo-1", 0), Err(503));
    }
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
        assert!(find("todo-2")["summary"]
            .as_str()
            .unwrap()
            .contains("画像案への回答を待っています"));
        assert!(!find("todo-2")["summary"]
            .as_str()
            .unwrap()
            .contains("hidden-value"));
        assert_eq!(
            find("todo-2")["questionUrl"],
            "https://discord.com/channels/100/200/200"
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
        assert!(output["sources"]
            .as_array()
            .unwrap()
            .iter()
            .any(|s| s["state"] == "unknown"));
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
        let alias = json!({"id":"launcher","thread_id":"new","status":"running"});
        assert!(linked(&item, &alias));
        assert!(!linked(
            &json!({"job":""}),
            &json!({"id":"","thread_id":""})
        ));
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
    fn missing_registry_keeps_safe_activity_and_skills_without_free_text() {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path();
        fs::create_dir_all(root.join("todo")).unwrap();
        fs::create_dir_all(root.join("codex-jobs/jobs/demo")).unwrap();
        fs::write(root.join("todo/todo.json"), json!({
            "projects":{"Skills":"D:/CC/Skills","SecretClient":"D:/private"},
            "items":[
                {"id":1,"project":"Skills","title":"SecretClient password=hidden-value","note":"private prose","status":"doing","progress":37,"jobs":["private-job-id"]},
                {"id":2,"project":"SecretClient","title":"do not expose","status":"recovery_unverified","cwd":"D:/private/SecretClient","spec":"D:/private/SecretClient/spec.md"}
            ]
        }).to_string()).unwrap();
        fs::write(root.join("codex-jobs/jobs/demo/job.json"), json!({
            "id":"private-job-id","status":"running","todo_id":1,
            "name":"SecretClient","last_message":"hidden-value private prose","cwd":"D:/private/SecretClient"
        }).to_string()).unwrap();
        fs::create_dir_all(root.join("codex-jobs/jobs/failed")).unwrap();
        fs::write(root.join("codex-jobs/jobs/failed/job.json"),json!({"id":"private-failed-id","status":"failed","finished_at":"1970-01-01T00:16:40Z","name":"SecretClient","last_message":"hidden-value"}).to_string()).unwrap();
        let board = collect_with_questions(
            root,
            1_000_000,
            &json!({"question":"SecretClient hidden-value"}),
        );
        assert_eq!(board["sourceOk"], false);
        assert_eq!(board["metadataOk"], true);
        assert_eq!(board["protectionMode"], "masked_metadata");
        assert_eq!(board["metrics"]["activeJobs"], 1);
        assert_eq!(board["metrics"]["recoveryUnverified"], 1);
        assert_eq!(board["departments"].as_array().unwrap().len(), 6);
        let info = board["departments"]
            .as_array()
            .unwrap()
            .iter()
            .find(|d| d["name"] == "情報システム部")
            .unwrap();
        let skills = info["products"]
            .as_array()
            .unwrap()
            .iter()
            .find(|p| p["name"] == "Skills")
            .unwrap();
        assert_eq!(skills["workers"][0]["progress"], 37);
        assert_eq!(skills["workers"][0]["status"], "working");
        assert_eq!(skills["workers"][0]["detailAvailable"], false);
        assert_eq!(skills["workers"][0]["historyFailures"], 0);
        assert!(board["departments"]
            .as_array()
            .unwrap()
            .iter()
            .flat_map(|d| d["products"].as_array().unwrap())
            .flat_map(|p| p["workers"].as_array().unwrap())
            .any(|w| w["status"] == "attention"));
        let encoded = board.to_string();
        for protected in [
            "SecretClient",
            "hidden-value",
            "private prose",
            "D:/private",
            "private-job-id",
            "do not expose",
        ] {
            assert!(!encoded.contains(protected), "protected value leaked");
        }
        assert!(board["ownerQuestions"].is_null());
        assert!(!root.join("client-projects.txt").exists());
    }
    #[test]
    fn missing_registry_and_unreadable_activity_do_not_invent_zero_counts() {
        let temp = tempfile::tempdir().unwrap();
        let board = collect(temp.path(), 1_000_000);
        assert_eq!(board["sourceOk"], false);
        assert_eq!(board["metadataOk"], false);
        assert!(board["metrics"]["activeJobs"].is_null());
        assert!(board["metrics"]["todoDependencyWaits"].is_null());
        assert_eq!(board["departments"].as_array().unwrap().len(), 6);
        assert!(board["departments"]
            .as_array()
            .unwrap()
            .iter()
            .all(|d| d["products"].as_array().unwrap().is_empty()));
        assert!(!temp.path().join("client-projects.txt").exists());
    }
    #[test]
    fn stale_and_future_monitor_records_cannot_report_current_capacity() {
        assert!(!fresh(Some(1), 300_000, 180_000));
        assert!(!fresh(Some(400_001), 300_000, 180_000));
        assert!(fresh(Some(290_000), 300_000, 180_000));
        assert_eq!(departments(Tree::new()).len(), 6);
    }
}
