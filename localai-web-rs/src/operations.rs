//! A session-independent, read-only projection of the shared operational records.
//! Never read job prompts, credentials, client workspaces or session transcripts.
use chrono::DateTime;
#[path = "operations_identity.rs"]
mod attribution;
#[path = "operations_catalog.rs"]
mod catalog;
#[path = "commander_health.rs"]
mod commander_health;
#[cfg(test)]
#[path = "operations_product_identity_tests.rs"]
mod product_identity_tests;
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
    questions: RwLock<Value>,
    skills: crate::skills_inventory::Inventory,
    native: crate::native_activity::Registry,
}
impl Operations {
    pub fn production() -> Self {
        Self::new("D:/CC/Tools".into(), "D:/CC/Skills".into())
    }
    /// Use one record root for both the resident snapshot and its HTTP consumer.
    pub fn new(root: PathBuf, skills_root: PathBuf) -> Self {
        let native = crate::native_activity::Registry::new(&root);
        Self {
            root,
            snapshot: RwLock::new(empty()),
            questions: RwLock::new(Value::Null),
            skills: crate::skills_inventory::Inventory::new(skills_root),
            native,
        }
    }
    pub fn snapshot(&self) -> Value {
        let now = now_ms();
        let mut value = self.cc_snapshot();
        let native = self.native.view(now);
        native_business_scopes(&mut value, &native);
        crate::workforce::merge_native(&mut value, &native, now);
        crate::workforce::apply_to_board(&mut value);
        value
    }
    fn cc_snapshot(&self) -> Value {
        let mut value = self
            .snapshot
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone();
        if let Some(workforce) = value.get_mut("workforce") {
            crate::workforce::expire(workforce, now_ms());
        }
        if value["workforce"].is_object() {
            crate::workforce::apply_to_board(&mut value);
        }
        value
    }
    pub fn native_snapshot(&self) -> Value {
        self.native.view(now_ms())
    }
    pub fn register_native(
        &self,
        mut request: crate::native_activity::Register,
    ) -> Result<Value, &'static str> {
        if let Some(project) = request.business_project.as_deref() {
            request.business_project = Some(
                catalog::find(project)
                    .ok_or("invalid_native_business_project")?
                    .name
                    .to_owned(),
            );
        }
        self.native.register(request, now_ms())
    }
    pub fn record_native(
        &self,
        request: crate::native_activity::Activity,
    ) -> Result<Value, &'static str> {
        self.native.activity(request, now_ms())
    }
    pub fn close_native(
        &self,
        request: crate::native_activity::Close,
    ) -> Result<Value, &'static str> {
        self.native.close(request, now_ms())
    }
    pub fn refresh(&self) {
        let live = self
            .questions
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone();
        let now = now_ms();
        let mut value = collect_with_questions(&self.root, now, &live);
        value["skillsInventory"] = self.skills.snapshot(now);
        let active = crate::workforce_observation::acquire(&self.root);
        let completed = now_ms();
        self.publish(value, active, now, completed);
    }
    fn publish(
        &self,
        mut value: Value,
        active: Result<Value, &'static str>,
        now: u64,
        completed: u64,
    ) {
        // Native overlays are per-request facts, never cached as CC observations.
        let previous = self.cc_snapshot();
        value["workforce"] = crate::workforce::generation(
            &value,
            active.as_ref().ok(),
            previous.get("workforce"),
            now,
            completed,
            active.as_ref().err().copied(),
        );
        if value["workforce"]["state"] == "unavailable"
            && value["protectionMode"] == "masked_metadata"
            && previous["protectionMode"] == "masked_metadata"
            && previous["departments"].is_array()
        {
            // Retain only the already masked display, with its original data time.
            // The failed acquisition and unknown current counts remain explicit.
            value["departments"] = previous["departments"].clone();
            value["unassignedProducts"] = previous["unassignedProducts"].clone();
            value["previousDataAt"] = previous
                .get("previousDataAt")
                .unwrap_or(&previous["fetchedAt"])
                .clone();
        }
        crate::workforce::apply_to_board(&mut value);
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
        let policy = priority_policy(&self.root);
        let associations = attribution::assignments(items, &jobs, &policy);
        let mut privacy = crate::console_ui::PrivacyIndex::new(&names, &db);
        if id.starts_with("job-") {
            let job = jobs
                .iter()
                .find(|job| safe_id("job", text(job, "id")) == id)
                .ok_or(404u16)?;
            if privacy.is_private_job(job)
                || associations.sensitive(job)
                || private_product(job, &policy)
                || items
                    .iter()
                    .filter(|item| linked(item, job))
                    .any(|item| private_product(item, &policy))
            {
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
        let client = private_todo(
            item,
            Some(&db),
            &policy,
            &names,
            &mut privacy,
            &associations,
        );
        if client {
            return Err(404);
        }
        // Archives are opened only for this explicit, privacy-checked detail
        // request; ordinary board refreshes use reference counts alone.
        let materialized =
            cc_todo::todo_history::materialized(&self.root.join("todo/todo.json"), item)
                .map_err(|_| 503u16)?;
        let item = &materialized;
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
fn native_business_scopes(board: &mut Value, native: &Value) {
    let mut declared: BTreeMap<u64, BTreeSet<String>> = BTreeMap::new();
    for binding in native["bindings"].as_array().into_iter().flatten() {
        if let (Some(id), Some(project)) = (
            binding["todoId"].as_u64(),
            binding["businessProject"].as_str(),
        ) {
            if let Some(product) = catalog::find(project) {
                declared
                    .entry(id)
                    .or_default()
                    .insert(product.name.to_owned());
            }
        }
    }
    if declared.is_empty() || !board["departments"].is_array() {
        return;
    }
    let mut tree = Tree::new();
    for department in board["departments"].as_array().into_iter().flatten() {
        let index = department["id"]
            .as_str()
            .and_then(|id| id.strip_prefix("department-"))
            .and_then(|id| id.parse::<usize>().ok())
            .unwrap_or(usize::MAX);
        for product in department["products"].as_array().into_iter().flatten() {
            let name = text(product, "name").to_owned();
            tree.entry((index, name.clone())).or_default();
            for worker in product["workers"].as_array().into_iter().flatten() {
                add_native_scoped_worker(&mut tree, index, &name, worker, &declared);
            }
        }
    }
    for product in board["unassignedProducts"].as_array().into_iter().flatten() {
        let name = text(product, "name").to_owned();
        tree.entry((usize::MAX, name.clone())).or_default();
        for worker in product["workers"].as_array().into_iter().flatten() {
            add_native_scoped_worker(&mut tree, usize::MAX, &name, worker, &declared);
        }
    }
    board["unassignedProducts"] = json!(unassigned_products(&tree));
    board["departments"] = json!(departments(tree));
}
fn add_native_scoped_worker(
    tree: &mut Tree,
    department: usize,
    name: &str,
    worker: &Value,
    declared: &BTreeMap<u64, BTreeSet<String>>,
) {
    let target = worker["id"]
        .as_str()
        .and_then(|id| id.strip_prefix("todo-"))
        .and_then(|id| id.parse::<u64>().ok())
        .and_then(|id| declared.get(&id))
        .filter(|targets| targets.len() == 1)
        .and_then(|targets| targets.first())
        .and_then(|name| catalog::find(name));
    let mut worker = worker.clone();
    if let Some(product) = target {
        // A declared business target changes placement, never privacy or code identity.
        worker["nativeBusinessScope"] = json!({"source":"native_binding","project":product.name});
        add(tree, product.department, product.name.to_owned(), worker);
    } else {
        add(tree, department, name.to_owned(), worker)
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
                crate::owner_followup::poll(&board.root);
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
/// Restricted read-only display for the existing shared records panel.
/// Missing privacy records never grant access to text, paths or job actions.
pub fn metadata_display(
    root: &Path,
    todo_path: &Path,
    jobs_path: &Path,
    kind: &str,
) -> Result<Value, &'static str> {
    if !matches!(kind, "todo" | "jobs" | "handoff") {
        return Err("invalid display");
    }
    let board = protected_metadata_at(root, todo_path, jobs_path, now_ms());
    metadata_display_snapshot(&board, kind)
}

/// Reuse the resident's already protected projection instead of scanning the
/// complete ToDo and job history once for each open panel on every refresh.
pub fn metadata_display_snapshot(board: &Value, kind: &str) -> Result<Value, &'static str> {
    if !matches!(kind, "todo" | "jobs" | "handoff") {
        return Err("invalid display");
    }
    if board["protectionMode"] != "masked_metadata" {
        return Err("protected projection required");
    }
    if board["metadataOk"] != true {
        return Err("metadata unavailable");
    }
    let mut rows = Vec::new();
    for department in board["departments"].as_array().into_iter().flatten() {
        for product in department["products"].as_array().into_iter().flatten() {
            for worker in product["workers"].as_array().into_iter().flatten() {
                rows.push((product, worker));
            }
        }
    }
    for product in board["unassignedProducts"].as_array().into_iter().flatten() {
        for worker in product["workers"].as_array().into_iter().flatten() {
            rows.push((product, worker));
        }
    }
    let items:Vec<_>=rows.iter().filter_map(|(product,worker)| {
        let id=worker["id"].as_str()?.strip_prefix("todo-")?.parse::<u64>().ok()?;
        Some(json!({"id":id,"project":product["name"],"productIdentity":worker["productIdentity"],"codeProduct":worker["codeProduct"],"title":worker["task"],"status":worker["recordedStatus"],
            "progress":worker["progress"],"updated_at":worker["updatedAt"],"note":"作業本文は保護設定の復旧まで非表示です。",
            "summary":worker["summary"],"currentStage":worker["currentStage"],
            "lastActivityAt":worker["lastActivityAt"],"stopReason":worker["stopReason"],"nextAction":worker["nextAction"],
            "dependencies":worker["dependencies"],"history":[],"historyAvailable":false,"actionsAvailable":false}))
    }).collect();
    let mut used = BTreeSet::new();
    let jobs:Vec<_>=rows.iter().filter_map(|(product,worker)| {
        let id=worker["primaryJob"].as_str()?;
        if !used.insert(id.to_owned()) { return None; }
        Some(json!({"id":id,"project":product["name"],"productIdentity":worker["productIdentity"],"codeProduct":worker["codeProduct"],"task_title":format!("{} · {}",text(product,"name"),text(worker,"task")),"status":worker["jobStatus"],
            "summary":worker["summary"],"currentStage":worker["currentStage"],"progress":worker["progress"],
            "lastActivityAt":worker["lastActivityAt"],"stopReason":worker["stopReason"],"nextAction":worker["nextAction"],
            "updated_at":worker["updatedAt"],"started_at":null,"actionsAvailable":false,"contentAvailable":false}))
    }).collect();
    let mut result = match kind {
        "todo" => json!({"items":items}),
        "jobs" => json!({"jobs":jobs}),
        _ => {
            json!({"text":"製品名・状態・作業の種類を共有記録から取得しました。本文、履歴、質問と回答は保護設定と原記録を復旧中です。空欄は未確認であり、未回答の質問が0件とは確認できていません。",
            "todos":items,"running_jobs":jobs,"decisions":[],"decisionsAvailable":false})
        }
    };
    result["protectionMode"] = json!("masked_metadata");
    result["metadataOnly"] = json!(true);
    result["fetchedAt"] = board["fetchedAt"].clone();
    result["workforce"] = board["workforce"].clone();
    Ok(result)
}
fn empty() -> Value {
    json!({"fetchedAt":null,"sourceOk":false,"sources":[],
        "commander":{"id":"commander","name":"司令塔","status":"unknown","detail":"共有データを取得中"},
        "productBindingsConfigured":cc_scheduler::product_identity::bindings_configured(),"productBindingsOverridden":null,"metrics":{},"departments":departments(BTreeMap::new())})
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
fn priority_policy(root: &Path) -> Value {
    let path = root.join("scheduler/priorities.json");
    match fs::metadata(&path) {
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => json!({}),
        _ => read_json(&path).unwrap_or(Value::Null),
    }
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
// Only fixed vocabulary is derived from activity metadata; never copy a command,
// title, message or path while the original privacy records are unavailable.
fn protected_stage(job: Option<&Value>) -> &'static str {
    let Some(job) = job else {
        return "担当の起動待ち";
    };
    match text(job, "status") {
        "queued" | "pending" => return "起動待ち",
        "starting" => return "担当を起動中",
        "running" => {}
        _ => return "工程は未記録",
    }
    let activity = job["activity"]
        .as_str()
        .or_else(|| job["activity"]["type"].as_str())
        .or_else(|| job["activity"]["kind"].as_str())
        .unwrap_or("");
    let kind = activity
        .split_once(':')
        .map_or(activity, |(kind, _)| kind)
        .trim();
    match kind {
        "command" | "command_execution" | "exec_command" | "running" => "コマンドを実行中",
        "mcp" => "連携ツールを実行中",
        "file_change" | "edit" | "apply_patch" | "editing" => "ファイルを修正中",
        "searching" => "資料を検索中",
        "agent" => "並列担当と連携中",
        "said" => "作業状況を記録中",
        "thinking" | "reasoning" => "調査・検討中",
        "review" => "レビュー中",
        "test" | "testing" => "検査中",
        _ => match text(job, "status") {
            "queued" | "pending" => "起動待ち",
            "starting" => "担当を起動中",
            "running" => "実行中（工程は未記録）",
            _ => "工程は未記録",
        },
    }
}
fn protected_activity(job: Option<&Value>) -> Option<String> {
    let job = job?;
    if text(job, "status") != "running" {
        return None;
    }
    ["last_event_at", "last_heartbeat_at", "heartbeat_at"]
        .iter()
        .filter_map(|key| stamp(&job[*key]))
        .chain(
            ["at", "timestamp"]
                .iter()
                .filter_map(|key| stamp(&job["activity"][*key])),
        )
        .max()
        .and_then(|at| chrono::DateTime::from_timestamp_millis(at as i64))
        .map(|at| at.to_rfc3339())
}
fn failed(v: &Value) -> bool {
    matches!(text(v, "status"), "failed" | "lost" | "cancelled")
}
fn linked(item: &Value, job: &Value) -> bool {
    job["todo_id"]
        .as_u64()
        .is_some_and(|id| item["id"].as_u64() == Some(id))
        || (item["id"].is_u64()
            && job["related_todo_ids"]
                .as_array()
                .is_some_and(|ids| ids.contains(&item["id"])))
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
// The media placement repair must not turn a shared history/reference into
// another current worker. Keep the full associated list for privacy/history;
// narrow only the current observation of the explicitly assigned product.
fn media_current<'a>(
    item: &Value,
    associated: &[&'a Value],
    db: Option<&Value>,
    media: bool,
) -> Option<&'a Value> {
    associated.iter().copied().find(|job| {
        if !active(job) {
            return false;
        }
        if !media {
            return true;
        }
        let items = db
            .and_then(|db| db["items"].as_array())
            .map(Vec::as_slice)
            .unwrap_or(&[]);
        let direct = job["todo_id"]
            .as_u64()
            .filter(|id| *id > 0 && items.iter().any(|row| row["id"].as_u64() == Some(*id)));
        let primary = direct.or_else(|| {
            items
                .iter()
                .filter(|row| linked(row, job))
                .min_by_key(|row| {
                    (
                        if row["scheduler"]["job"].as_str() == job["id"].as_str() {
                            0
                        } else if matches!(text(row, "status"), "doing" | "todo") {
                            1
                        } else {
                            2
                        },
                        row["id"].as_u64().unwrap_or(u64::MAX),
                    )
                })
                .and_then(|row| row["id"].as_u64())
        });
        primary.is_some() && primary == item["id"].as_u64()
    })
}
fn media_processing_key(job: Option<&Value>) -> Value {
    job.map(|job| {
        let hash = ring::digest::digest(&ring::digest::SHA256, text(job, "id").as_bytes());
        json!(format!(
            "job-{}",
            hash.as_ref()
                .iter()
                .map(|b| format!("{b:02x}"))
                .collect::<String>()
        ))
    })
    .unwrap_or(Value::Null)
}
fn private_product(row: &Value, policy: &Value) -> bool {
    attribution::resolve(row, policy) == cc_scheduler::product_identity::Resolution::Unknown
        || cc_scheduler::priority::item_rank(row, policy) == 10
}

fn private_todo(
    item: &Value,
    db: Option<&Value>,
    policy: &Value,
    names: &cc_ask::task_title::Names,
    privacy: &mut crate::console_ui::PrivacyIndex<'_>,
    associations: &attribution::Associations<'_>,
) -> bool {
    let raw_project = text(item, "project");
    let display_project = policy["items"][item["id"].as_u64().unwrap_or(0).to_string()]["project"]
        .as_str()
        .or_else(|| item["owner_project"].as_str())
        .unwrap_or(raw_project);
    private_product(item, policy)
        || names.project(raw_project) == "クライアント案件"
        || project_name(names, display_project) == "クライアント案件"
        || names.project(text(item, "cwd")) == "クライアント案件"
        || names.project(text(item, "spec")) == "クライアント案件"
        || associations
            .raw(item["id"].as_u64().unwrap_or(0))
            .iter()
            .any(|job| {
                privacy.is_private_job(job)
                    || associations.sensitive(job)
                    || private_product(job, policy)
            })
        || db.is_some_and(|db| {
            names.project(db["projects"][raw_project].as_str().unwrap_or("")) == "クライアント案件"
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
        "note_history_archives",
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
    if let Some(product) = catalog::find(project) {
        return product.name.into();
    }
    let masked = names.project(project);
    if masked == "クライアント案件" {
        return masked;
    }
    match project {
        "civ6" | "civ6-banseiki" => "万世記".into(),
        "tools" | "PCツール" => "PCの道具".into(),
        "" => "その他".into(),
        _ => clean(names, &cc_ask::task_title::canonical_project(project)),
    }
}
#[cfg(test)]
fn department(project: &str) -> usize {
    if let Some(product) = cc_scheduler::product_identity::by_name(project) {
        return product.department;
    }
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
#[cfg(test)]
#[path = "operations_media_tests.rs"]
mod media_tests;
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
        ("GoDD-MCP", 4),
        ("GoDD MCP α", 1),
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
    let (dept, product) = catalog::find(&product)
        .map(|p| (p.department, p.name.to_owned()))
        .unwrap_or((dept, product));
    tree.entry((dept, product)).or_default().push(worker);
}
fn departments(tree: Tree) -> Vec<Value> {
    [4, 0, 5, 3, 1, 2]
        .into_iter()
        .map(|i| {
            let (name, color) = DEPARTMENTS[i];
            let products: Vec<_> = catalog::PRODUCTS
                .iter()
                .filter(|p| p.department == i)
                .map(|p| {
                    attribution::catalogue(
                        p,
                        json!({"id":safe_id("product",p.name),"name":p.name,
                    "workers":tree.get(&(i,p.name.to_owned())).cloned().unwrap_or_default()}),
                    )
                })
                .collect();
            json!({"id":format!("department-{i}"),"name":name,"color":color,"products":products})
        })
        .collect()
}
fn unassigned_products(tree: &Tree) -> Vec<Value> {
    let mut entries = BTreeMap::<String, Vec<Value>>::new();
    for ((_, name), workers) in tree {
        if catalog::find(name).is_none() {
            entries
                .entry(name.clone())
                .or_default()
                .extend(workers.clone());
        }
    }
    entries
        .into_iter()
        .map(|(name, workers)| json!({"id":safe_id("product",&name),"name":name,"workers":workers}))
        .collect()
}

/// A finite vocabulary describes observed work without releasing source prose.
fn registered_work_kinds(items: &[&Value]) -> Vec<&'static str> {
    let kinds = [
        (
            "Discordの受付・回答・通知",
            &["discord", "質問", "回答", "通知"][..],
        ),
        (
            "開発・修正・検査",
            &["実装", "修正", "検証", "テスト", "ci", "レビュー"][..],
        ),
        (
            "サイト・画面・表示",
            &["サイト", "画面", "表示", "ui", "ux"][..],
        ),
        (
            "PCの容量・メモリ・後片付け",
            &["容量", "メモリ", "削除", "清掃", "ドライブ", "キャッシュ"][..],
        ),
        (
            "AIの実行・接続・利用枠",
            &["モデル", "llm", "codex", "claude", "利用枠", "oauth"][..],
        ),
        (
            "監視・担当の再開・定期実行",
            &["監視", "再開", "常駐", "定期", "スケジュール", "担当"][..],
        ),
        (
            "動画・音楽・音声の制作",
            &["動画", "音楽", "音声", "楽曲", "tts", "suno", "youtube"][..],
        ),
        (
            "サービスの設定・配備・料金",
            &[
                "配備",
                "デプロイ",
                "設定",
                "料金",
                "課金",
                "railway",
                "vercel",
            ][..],
        ),
        (
            "ゲームの制作・検査",
            &["ゲーム", "civ6", "godot", "unity", "戦闘", "武器"][..],
        ),
    ];
    kinds
        .into_iter()
        .filter_map(|(label, words)| {
            items
                .iter()
                .any(|item| {
                    let title = text(item, "title").to_lowercase();
                    words.iter().any(|word| title.contains(word))
                })
                .then_some(label)
        })
        .collect()
}

// A recorded owner choice changes presentation only, never privacy or admission.
// Match the independent, verified question generation and owner before using it.
fn membership_choice(root: &Path, now: u64) -> Option<(BTreeSet<String>, Value)> {
    let choice =
        read_json(&root.join("localai-web-rs/target/release/operations-membership-choice.json"))
            .ok()?;
    if choice["selection"] != "separate" || choice["source"] != "discord-owner-reply" {
        return None;
    }
    let answered = stamp(&choice["answered_at"])?;
    if answered > now + 60_000 {
        return None;
    }
    let ledger = read_json(&root.join("discord-ask/recovery-messages.json")).ok()?;
    if ledger["domain"] != "new-owner-recovery-messages" {
        return None;
    }
    let events = ledger["events"].as_array()?;
    for event in events.iter().filter(|e| e["event"] == "intent") {
        let request = &event["request"];
        if request["operation"]["kind"] != "ask"
            || request["operation"]["todo_id"] != choice["todo_id"]
            || request["destination"]["owner"] != choice["author_id"]
        {
            continue;
        }
        let verified = events.iter().find(|e| {
            e["event"] == "verified"
                && e["request_id"] == request["request_id"]
                && e["phase"] == "question:0"
                && e["receipt"]["thread_id"] == choice["question_thread_id"]
                && e["receipt"]["message_id"] == choice["question_message_id"]
        })?;
        let question = choice["question_message_id"]
            .as_str()?
            .parse::<u64>()
            .ok()?;
        if choice["message_id"].as_str()?.parse::<u64>().ok()? <= question {
            return None;
        }
        let ids: BTreeSet<_> = choice["registration_ids"]
            .as_array()?
            .iter()
            .filter_map(Value::as_str)
            .filter(|id| {
                id.strip_prefix("registration-").is_some_and(|tail| {
                    tail.len() == 16 && tail.bytes().all(|b| b.is_ascii_hexdigit())
                })
            })
            .map(str::to_owned)
            .collect();
        let resources: BTreeSet<_> = request["operation"]["decision_original"]["resources"]
            .as_array()?
            .iter()
            .filter_map(|r| r["id"].as_str())
            .map(str::to_owned)
            .collect();
        if ids.is_empty() || ids != resources {
            return None;
        }
        let guild = request["destination"]["guild"].as_str()?;
        if !guild.bytes().all(|b| b.is_ascii_digit()) {
            return None;
        }
        let url = format!(
            "https://discord.com/channels/{guild}/{}",
            text(&verified["receipt"], "thread_id")
        );
        let info = json!({"selection":"separate","answeredAt":answered,"acceptedRegistrations":ids.len(),"questionUrl":url});
        return Some((ids, info));
    }
    None
}
fn source(key: &str, label: &str, ok: bool, at: Option<u64>, message: &str) -> Value {
    json!({"key":key,"label":label,"ok":ok,"updatedAt":at,"message":message})
}
fn jobs(root: &Path) -> Result<Vec<Value>, ()> {
    jobs_at(&root.join("codex-jobs/jobs"))
}
fn jobs_at(path: &Path) -> Result<Vec<Value>, ()> {
    let mut out = Vec::new();
    for entry in fs::read_dir(path).map_err(|_| ())? {
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
fn protected_wait(item: &Value, current: Option<&Value>, queue: &Value, now: u64) -> &'static str {
    if item["status"] == "wait" {
        return "dependency";
    }
    if item["status"] == "recovery_unverified" {
        return "technical";
    }
    let wait = &item["owner_question_wait"];
    let question = &wait["question"];
    if item["status"] == "block"
        && wait["domain"] == "todo-owner-question-wait-v1"
        && question["domain"] == "discord-conversation-question-v1"
        && question["todo_id"]
            .as_str()
            .and_then(|id| id.parse::<u64>().ok())
            == item["id"].as_u64()
        && wait["block_note"].is_string()
        && wait["block_note"] == item["note"]
        && stamp(&wait["blocked_at"]).is_some_and(|at| at <= now)
        && wait["question_receipt_sha256"]
            .as_str()
            .is_some_and(|key| key.len() == 64 && key.bytes().all(|b| b.is_ascii_hexdigit()))
    {
        return "decision";
    }
    if item["status"] == "block" {
        return "unverified";
    }
    if current.is_some_and(|job| job["status"] == "queued") {
        return "job_queue";
    }
    if queue["queue"].as_array().is_some_and(|entries| {
        entries.iter().any(|entry| {
            entry["todo_id"] == item["id"]
                && queue["preparation_failures"][text(entry, "key")].is_array()
        })
    }) {
        return "startup_error";
    }
    if current.is_none()
        && queue["queue"]
            .as_array()
            .is_some_and(|entries| entries.iter().any(|entry| entry["todo_id"] == item["id"]))
    {
        return "intake";
    }
    "unverified"
}

fn protected_metadata(root: &Path, now: u64) -> Value {
    protected_metadata_at(
        root,
        &root.join("todo/todo.json"),
        &root.join("codex-jobs/jobs"),
        now,
    )
}
fn protected_metadata_at(root: &Path, todo_path: &Path, jobs_path: &Path, now: u64) -> Value {
    let todo = cc_todo::read_store(todo_path)
        .ok()
        .filter(|db| db["items"].is_array() && db["projects"].is_object());
    let job_result = jobs_at(jobs_path);
    let js = job_result.as_ref().map(Vec::as_slice).unwrap_or(&[]);
    let policy = priority_policy(root);
    let queue = read_json(&root.join("localai-web-rs/target/release/console-state.json"))
        .unwrap_or(Value::Null);
    let mut tree = Tree::new();
    let mut used = BTreeSet::new();
    let assignments = attribution::assignments(
        todo.as_ref()
            .and_then(|db| db["items"].as_array())
            .map(Vec::as_slice)
            .unwrap_or(&[]),
        js,
        &policy,
    );
    let priorities = todo
        .as_ref()
        .map(|db| {
            cc_scheduler::priority::todo_priorities_with_assignments(db, &policy, &assignments)
        })
        .unwrap_or_default();
    let ranks = &priorities.ranks;
    let public_product = |raw: &str| {
        catalog::find(raw).or_else(|| {
            todo.as_ref().and_then(|db| {
                db["projects"][raw]
                    .as_str()
                    .and_then(catalog::registered_root)
            })
        })
    };
    let project_label = |raw: &str| {
        public_product(raw)
            .map(|p| p.name.to_owned())
            .or_else(|| catalog::previously_named(raw).map(str::to_owned))
            .or_else(|| {
                todo.as_ref()
                    .and_then(|db| db["items"].as_array())
                    .and_then(|items| {
                        items
                            .iter()
                            .find(|item| text(item, "project") == raw)
                            .and_then(|item| item["id"].as_u64())
                    })
                    .map(|id| format!("未指定の登録（作業 #{id}）"))
            })
            .unwrap_or_else(|| {
                format!(
                    "登録 {}",
                    safe_id("project", raw).trim_start_matches("project-")
                )
            })
    };
    let mut registrations = Vec::new();
    if let Some(db) = &todo {
        let mut projects = BTreeSet::new();
        projects.extend(
            db["projects"]
                .as_object()
                .into_iter()
                .flat_map(|p| p.keys())
                .cloned(),
        );
        projects.extend(
            db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .map(|i| text(i, "project").to_owned()),
        );
        projects.extend(
            js.iter()
                .filter(|job| active(job))
                .map(|job| infer_project(job, Some(db))),
        );
        for raw in projects {
            if raw.eq_ignore_ascii_case("owner") {
                continue;
            }
            let items: Vec<_> = db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .filter(|item| text(item, "project") == raw)
                .collect();
            let public = public_product(&raw);
            let label = project_label(&raw);
            tree.entry((public.map_or(usize::MAX, |p| p.department), label.clone()))
                .or_default();
            if public.is_none() || public.is_some_and(|p| p.name == "Skills") {
                registrations.push(json!({"id":safe_id("registration",&raw),"name":label,
                    "nameConfirmed":public.is_some()||catalog::previously_named(&raw).is_some(),
                    "previouslyAssignedDepartment":if public.is_some_and(|p|p.name=="Skills") {Some("情報システム部")} else {None},
                    "registeredTasks":items.len(),"unfinishedTasks":items.iter().filter(|i|i["status"]!="done").count(),
                    "taskIds":items.iter().filter_map(|i|i["id"].as_u64()).collect::<Vec<_>>(),
                    "workKinds":registered_work_kinds(&items),"decision":if public.is_some_and(|p|p.name=="Skills") {"以前の指示に従い情報システム部内に配置済み"} else {"所属と一覧への残し方の判断待ち"}}));
            }
        }
    }
    let membership = membership_choice(root, now);
    if let Some((ids, _)) = &membership {
        for row in &mut registrations {
            if row["id"].as_str().is_some_and(|id| ids.contains(id)) {
                row["decision"] =
                    json!("本人の回答に従い、製品一覧とは別欄の作業として保持しています。");
                row["decisionResolved"] = json!(true);
            }
        }
    }
    if let Some(items) = todo.as_ref().and_then(|db| db["items"].as_array()) {
        for item in items {
            let Some(id) = item["id"].as_u64() else {
                continue;
            };
            let done = text(item, "status") == "done";
            if done && !fresh(todo_stamp(item), now, RECENT_MS) {
                continue;
            }
            let associated = assignments.assigned(id);
            let history_jobs = assignments.history(item["id"].as_u64().unwrap_or(0));
            for job in associated {
                used.insert(text(job, "id").to_owned());
            }
            let media =
                public_product(text(item, "project")).is_some_and(|p| p.name == "youtube-daily");
            let current = if media {
                media_current(item, associated, todo.as_ref(), true)
            } else {
                assignments.current(id)
            };
            let status = if done {
                "done"
            } else if current.is_some_and(|job| text(job, "status") != "queued") {
                "working"
            } else if text(item, "status") == "doing"
                && job_result.is_ok()
                && history_jobs.first().is_some_and(|job| {
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
            if raw_project.eq_ignore_ascii_case("owner") {
                continue;
            }
            let display_project = cc_scheduler::product_identity::display_project(item, &policy);
            let identity = assignments.record(item, &policy);
            let label = attribution::target_for(identity, item, project_label(display_project));
            let kinds = registered_work_kinds(&[item]);
            let task = if kinds.is_empty() {
                "作業の種類を確認中".to_owned()
            } else {
                kinds.join("、")
            };
            let dependencies: Vec<_> = item["wait_on"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(Value::as_u64)
                .filter(|id| *id > 0)
                .collect();
            let stop_reason = if text(item, "status") == "recovery_unverified" {
                "元の作業記録が消失したため、成果の確認と再開準備を行っています。"
            } else if text(item, "status") == "wait" {
                "記録された別のToDoの完了を待っています。"
            } else if status == "attention" {
                "前回の担当が失敗または停止しました。再開状況を確認しています。"
            } else if status == "waiting" {
                "実行中の担当と停止理由を確認しています。"
            } else {
                ""
            };
            let detail = if text(item, "status") == "recovery_unverified" {
                "復元した作業記録を確認中です。製品名は本人の指定に合わせ、作業本文は表示を保留しています。"
            } else {
                "作業本文の保護設定を復旧中です。部署と製品名は本人が指定した一覧から表示しています。"
            };
            add(
                &mut tree,
                catalog::find(&label).map_or(usize::MAX, |p| p.department),
                label,
                json!({"id":format!("todo-{id}"),"name":format!("作業 #{id}"),"task":format!("#{id} {task}"), "status":status,
                    "productIdentity":identity.metadata(),"jobProductIdentity":current.map(|job|assignments.target(job).metadata()),"codeProduct":attribution::code_product(item),
                    "priority":ranks.get(&id).copied(),
                    "recordedStatus":match text(item,"status") {"todo"|"doing"|"wait"|"block"|"done"|"recovery_unverified"=>item["status"].clone(),_=>json!("unknown")},
                    "processingJobKey":current.map(|job|cc_scheduler::product_identity::job_key(text(job,"id"))),"primaryJob":current.map(|job|safe_id("job",text(job,"id"))),"jobStatus":current.map(|job|text(job,"status")),
                    "summary":task,"currentStage":protected_stage(current),"lastActivityAt":protected_activity(current),
                    "stopReason":stop_reason,"dependencies":dependencies,
                    "nextAction":if status=="waiting"||status=="attention" {"元の依頼・成果・担当の状態を照合して続行します。"} else {""},
                    "factsSource":format!("共有ToDo #{id} の状態と作業種別（本文は非表示）"),
                    "progress":item["progress"].as_u64().filter(|n|*n<=100),
                    "updatedAt":todo_stamp(item).and_then(|at|chrono::DateTime::from_timestamp_millis(at as i64)).map(|at|at.to_rfc3339()),
                    "detail":detail,"detailAvailable":false,"historyFailures":job_result.as_ref().ok().map(|_|history_jobs.iter().filter(|job|failed(job)).count()),
                    "waitingKind":protected_wait(item,current,&queue,now)}),
            );
        }
    }
    for job in js.iter().filter(|job| {
        (active(job) || (failed(job) && fresh(stamp(&job["finished_at"]), now, RECENT_MS)))
            && !used.contains(text(job, "id"))
    }) {
        let raw_project = infer_project(job, todo.as_ref());
        if raw_project.eq_ignore_ascii_case("owner") {
            continue;
        }
        let identity = assignments.target(job);
        let label = attribution::target_for(identity, job, project_label(&raw_project));
        add(
            &mut tree,
            catalog::find(&label).map_or(usize::MAX, |p| p.department),
            label,
            json!({"id":safe_id("job",text(job,"id")),"name":"名称を確認中の担当","task":"作業内容の表示を保留中",
                "productIdentity":identity.metadata(),"codeProduct":attribution::code_product(job),
                "processingJobKey":cc_scheduler::product_identity::job_key(text(job,"id")),"primaryJob":safe_id("job",text(job,"id")),"jobStatus":text(job,"status"),
                "status":if failed(job) {"attention"} else if text(job,"status")=="queued" {"waiting"} else {"working"},"progress":null,
                "updatedAt":stamp(&job["finished_at"]).or_else(||stamp(&job["started_at"])).and_then(|at|chrono::DateTime::from_timestamp_millis(at as i64)).map(|at|at.to_rfc3339()),
                "detail":"作業本文の保護設定を復旧中です。指定外の登録は所属の判断待ちに表示しています。","detailAvailable":false,"historyFailures":if failed(job){Some(1u64)}else{None},"waitingKind":"unverified"}),
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
    out["productBindingsOverridden"] = json!(
        policy
            .as_object()
            .map(|p| p.contains_key("product_identities"))
    );
    out["protectionMode"] = json!("masked_metadata");
    out["sourceOk"] = json!(false);
    out["metadataOk"] = json!(todo.is_some() && job_result.is_ok());
    out["unassignedProducts"] = json!(unassigned_products(&tree));
    out["unlistedRegistrations"] = json!(registrations);
    out["membershipDecision"] = membership.map(|(_, info)| info).unwrap_or(Value::Null);
    out["departments"] = json!(departments(tree));
    out["metrics"] = json!({"activeJobs":job_result.as_ref().ok().map(|jobs|jobs.iter().filter(|job|matches!(text(job,"status"),"running"|"starting"|"pending")).count()),
        "jobQueued":job_result.as_ref().ok().map(|jobs|jobs.iter().filter(|job|text(job,"status")=="queued").count()),
        "todoDependencyWaits":count("wait"),"todoPendingStarts":count("todo"),"todoBlocked":count("block"),"recoveryUnverified":count("recovery_unverified")});
    let scheduler_path = root.join("scheduler/status.json");
    let scheduler = read_json(&scheduler_path).unwrap_or(Value::Null);
    let scheduler_at = stamp(&scheduler["measured_at"]);
    let scheduler_ok = fresh(scheduler_at, now, 180_000);
    for (key, value) in [
        ("cpuPercent", scheduler["cpu_percent"].clone()),
        (
            "memoryUsedGb",
            json!(
                scheduler["ram_total_gb"]
                    .as_f64()
                    .zip(scheduler["available_gb"].as_f64())
                    .map(|(total, free)| total - free)
            ),
        ),
        ("memoryTotalGb", scheduler["ram_total_gb"].clone()),
        ("jobLimit", scheduler["hard_cap"].clone()),
        ("waitingJobs", scheduler["queue_length"].clone()),
    ] {
        out["metrics"][key] = if scheduler_ok { value } else { Value::Null };
    }
    let queue_path = root.join("localai-web-rs/target/release/console-state.json");
    let queue_read = read_json(&queue_path);
    let queue = queue_read.as_ref().cloned().unwrap_or(Value::Null);
    let health_path = queue_path.with_file_name("commander-health.json");
    let health = read_json(&health_path);
    out["commander"] = commander_health::project(
        health.as_ref().ok(),
        &queue,
        now,
        (queue_read.is_err() && queue_path.try_exists().unwrap_or(true))
            || (health.is_err() && health_path.try_exists().unwrap_or(true)),
    );
    out["metrics"]["consoleQueued"] = json!(queue["queue"].as_array().map(Vec::len));
    out["metrics"]["consolePreparationFailures"] = json!(queue["queue"].as_array().map(|items| {
        items
            .iter()
            .filter(|t| queue["preparation_failures"][text(t, "key")].is_array())
            .count()
    }));
    out["sources"] = json!([
        source(
            "privacy",
            "作業本文の保護",
            false,
            None,
            "保護設定の原本を取得できません。本人指定の製品名と実測状態を表示し、作業本文と未確認の名称は伏せています。"
        ),
        source(
            "todos",
            "共有ToDo",
            todo.is_some(),
            modified(todo_path),
            "記録された状態と実測進捗だけを取得"
        ),
        source(
            "jobs",
            "ジョブ",
            job_result.is_ok(),
            None,
            "記録された実行状態だけを取得。元の更新時刻は記録ごとに異なります。"
        ),
        source(
            "scheduler",
            "CPU・メモリ・ジョブ枠",
            scheduler_ok,
            scheduler_at,
            if scheduler_ok {
                "常駐監視から取得"
            } else {
                "監視記録が古いか取得できません"
            }
        ),
        source(
            "commander",
            "司令塔の巡回",
            out["commander"]["status"] == "working",
            modified(&health_path),
            "巡回完了の記録から取得"
        )
    ]);
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
    let policy = priority_policy(root);
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
        None,
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
    let assignments = attribution::assignments(
        db.and_then(|db| db["items"].as_array())
            .map(Vec::as_slice)
            .unwrap_or(&[]),
        js,
        &policy,
    );
    let task_priorities = cc_scheduler::priority::todo_priorities_with_assignments(
        db.unwrap_or(&empty_db),
        &policy,
        &assignments,
    );
    let task_ranks = &task_priorities.ranks;
    if let Some(items) = db.and_then(|v| v["items"].as_array()) {
        for item in items {
            let is_done = text(item, "status") == "done";
            if is_done && !fresh(todo_stamp(item), now, RECENT_MS) {
                continue;
            }
            let associated = assignments.assigned(item["id"].as_u64().unwrap_or(0));
            let history_jobs = assignments.history(item["id"].as_u64().unwrap_or(0));
            for j in associated {
                used.insert(text(j, "id").to_owned());
            }
            let current = assignments.current(item["id"].as_u64().unwrap_or(0));
            let identity = assignments.record(item, &policy);
            let raw_project = text(item, "project");
            let owned = &policy["items"][item["id"].as_u64().unwrap_or(0).to_string()];
            let display_project = owned["project"]
                .as_str()
                .or_else(|| item["owner_project"].as_str())
                .unwrap_or(raw_project);
            let project = project_name(&names, display_project);
            let client = private_todo(item, db, &policy, &names, &mut privacy, &assignments);
            let project = if client {
                catalog::find(display_project)
                    .map(|p| p.name.to_owned())
                    .unwrap_or_else(|| "クライアント案件".into())
            } else {
                project
            };
            let project = attribution::target_for(identity, item, project);
            let media = project == "youtube-daily";
            let current = if media {
                media_current(item, associated, db, true)
            } else {
                current
            };
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
                    && history_jobs.first().is_some_and(|j| {
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
                    current,
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
            let history = history_jobs.iter().filter(|j| failed(j)).count();
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
                "productIdentity":identity.metadata(),"jobProductIdentity":current.map(|job|assignments.target(job).metadata()),"codeProduct":attribution::code_product(item),
                "recordedStatus":item["status"],"priority":
                    if identity==cc_scheduler::product_identity::Resolution::Other {Some(cc_scheduler::priority::item_rank(item,&policy))}else{item["id"].as_u64().and_then(|id|task_ranks.get(&id).copied())},"waitingKind":wait_kind,
                "processingJobKey":current.map(|job|cc_scheduler::product_identity::job_key(text(job,"id"))),"primaryJob":current.map(|job|safe_id("job",text(job,"id"))),"jobStatus":current.map(|job|text(job,"status")),
                "task":task,"waitKind":wait_kind,"questionUrl":question_url,"status":status,"progress":progress,"updatedAt":todo_stamp(item).and_then(|n|chrono::DateTime::from_timestamp_millis(n as i64)).map(|t|t.to_rfc3339()),
                "detail":detail,"historyFailures":history});
            if media {
                worker["processingJobKey"] = media_processing_key(current);
            }
            if !client {
                worker["factsSource"] =
                    json!(format!("共有ToDo #{}", item["id"].as_u64().unwrap_or(0)));
                worker["note"] = json!(
                    crate::console_ui::clean(&names, text(item, "note"))
                        .chars()
                        .take(360)
                        .collect::<String>()
                );
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
        let identity = assignments.target(j);
        let client = assignments.sensitive(j)
            || privacy.is_private_job(j)
            || identity == cc_scheduler::product_identity::Resolution::Unknown
            || project == "クライアント案件"
            || names.project(text(j, "cwd")) == "クライアント案件"
            || cc_scheduler::priority::item_rank(j, &policy) == 10
            || cc_scheduler::priority::item_rank(&json!({"project":inferred}), &policy) == 10;
        let project = if client {
            catalog::find(&inferred)
                .map(|p| p.name.to_owned())
                .unwrap_or_else(|| "クライアント案件".into())
        } else {
            project
        };
        let project = attribution::target_for(identity, j, project);
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
                "productIdentity":identity.metadata(),"codeProduct":attribution::code_product(j),
                "processingJobKey":cc_scheduler::product_identity::job_key(text(j,"id")),"primaryJob":safe_id("job",text(j,"id")),"jobStatus":j["status"],
                "task":task,"status":status,"progress":null,"updatedAt":j["finished_at"].as_str().or(j["started_at"].as_str()),
                "detail":if active(j){"共有ジョブの担当です。"}else{"過去の失敗記録です。現在の障害を示すものではありません。"},"historyFailures":usize::from(failed(j))});
        if !client {
            worker["report"] = json!(
                crate::console_ui::clean(&names, text(j, "last_message"))
                    .chars()
                    .take(360)
                    .collect::<String>()
            );
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
    json!({"metadataOk":todo.is_ok()&&jobs.is_ok(),"productBindingsConfigured":cc_scheduler::product_identity::bindings_configured(),"productBindingsOverridden":policy.as_object().map(|p|p.contains_key("product_identities")),"fetchedAt":now,"sourceOk":sources.iter().all(|s|s["ok"]==true || s["state"]=="unknown"),"sources":sources,
        "commander":commander,
        "metrics":metrics,"ownerQuestions":decisions["questions"],"decisionTasks":decisions["tasks"],"blockedDecisionTasks":decisions["blockedTasks"],
        "unassignedProducts":unassigned_products(&tree),"departments":departments(tree)})
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
    #[test]
    fn protected_progress_uses_fixed_activity_labels_and_only_real_timestamps() {
        let job = json!({"status":"running", "activity":{"type":"command_execution","command":"private command", "at":"2026-10-19T15:00:00Z"},"updated_at":"2026-10-20T00:02:00Z"});
        assert_eq!(protected_stage(Some(&job)), "コマンドを実行中");
        assert_eq!(
            protected_activity(Some(&job)).as_deref(),
            Some("2026-10-19T15:00:00+00:00")
        );
        let unknown = json!({"status":"running","activity":"private command", "updated_at":"2026-10-20T00:02:00Z"});
        assert_eq!(protected_stage(Some(&unknown)), "実行中（工程は未記録）");
        assert!(protected_activity(Some(&unknown)).is_none());
        let queued = json!({"status":"queued","last_event_at":"2026-10-19T15:00:00Z"});
        assert!(protected_activity(Some(&queued)).is_none());
        assert_eq!(
            protected_stage(Some(
                &json!({"status":"queued","activity":"running: private command"})
            )),
            "起動待ち"
        );
        assert_eq!(
            protected_stage(Some(
                &json!({"status":"running","activity":"mcp: private.connector"})
            )),
            "連携ツールを実行中"
        );
        assert_eq!(protected_stage(None), "担当の起動待ち");
        assert!(protected_activity(None).is_none());
    }
    use super::*;

    #[test]
    fn skills_count_remains_available_when_private_text_is_masked() {
        let root = tempfile::tempdir().unwrap();
        let skills_root = root.path().join("Skills");
        fs::create_dir_all(skills_root.join("example")).unwrap();
        fs::write(skills_root.join("example/SKILL.md"), "example definition").unwrap();
        let board = Operations::new(root.path().join("tools"), skills_root);
        board.refresh();
        let snapshot = board.snapshot();
        assert_eq!(snapshot["protectionMode"], "masked_metadata");
        assert_eq!(snapshot["skillsInventory"]["state"], "complete");
        assert_eq!(snapshot["skillsInventory"]["count"], 1);
        assert!(!snapshot.to_string().contains("example definition"));
    }
    #[test]
    fn archived_notes_stay_readable_in_detail_without_board_hydration() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();
        fs::create_dir_all(root.join("todo")).unwrap();
        fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
        fs::write(root.join("client-projects.txt"), "FixturePrivate\n").unwrap();
        let store = root.join("todo/todo.json");
        let notes: Vec<_> = (0..23)
            .map(|i| json!({"note":format!("保存したメモ{i}"),"updated":"2026-10-17 00:00"}))
            .collect();
        let mut db = json!({"projects":{"demo":"D:/fixture"},"items":[{"id":1,"project":"demo","title":"履歴の復旧","status":"doing","note":"current","note_history":notes}]});
        let receipt = cc_todo::todo_history::archive(&store, &mut db, 1, true).unwrap();
        cc_todo::save(&store, &db).unwrap();
        let board = Operations::new(root.into(), root.join("Skills"));
        assert_eq!(crate::console_ui::history_count(&db["items"][0]), 23);
        let first = board.worker_detail("todo-1", 0).unwrap();
        assert_eq!(first["total"], 23);
        assert_eq!(first["history"].as_array().unwrap().len(), 10);
        assert_eq!(first["history"][0]["note"], "保存したメモ0");
        assert_eq!(
            board.worker_detail("todo-1", 20).unwrap()["history"][2]["note"],
            "保存したメモ22"
        );
        let path = root
            .join("todo/note-history-v1")
            .join(receipt["archive"]["file"].as_str().unwrap());
        fs::write(path, b"{}").unwrap();
        // Counting does not touch historical bodies, but an explicit read must fail.
        assert_eq!(crate::console_ui::history_count(&db["items"][0]), 23);
        assert_eq!(board.worker_detail("todo-1", 0), Err(503));
    }
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
        let board = Operations::new(root.into(), root.join("Skills"));
        board.refresh();
        let snapshot = board.snapshot();
        let workers: Vec<_> = snapshot["departments"]
            .as_array()
            .unwrap()
            .iter()
            .flat_map(|dept| dept["products"].as_array().unwrap())
            .chain(snapshot["unassignedProducts"].as_array().unwrap())
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
            .chain(output["unassignedProducts"].as_array().unwrap())
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
        assert!(
            board["departments"]
                .as_array()
                .unwrap()
                .iter()
                .flat_map(|d| d["products"].as_array().unwrap())
                .chain(board["unassignedProducts"].as_array().unwrap())
                .flat_map(|p| p["workers"].as_array().unwrap())
                .any(|w| w["status"] == "attention")
        );
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
        assert!(
            board["departments"]
                .as_array()
                .unwrap()
                .iter()
                .flat_map(|d| d["products"].as_array().unwrap())
                .all(|p| p["workers"].as_array().unwrap().is_empty())
        );
        assert!(!temp.path().join("client-projects.txt").exists());
    }
    #[test]
    fn owner_catalogue_keeps_exact_lists_alias_workers_and_every_unlisted_registration() {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path();
        fs::create_dir_all(root.join("todo")).unwrap();
        fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
        fs::write(root.join("todo/todo.json"),json!({"projects":{
            "GoDD-Rosseta":"D:/public/a","GoDD Rosetta":"D:/public/b",
            "civ6-banseiki":"D:/public/game","ARPS":"D:/private",
            "Skills":"D:/public/skills","SecretClient":"D:/private/old",
            "SocialPilot":"D:/public/social"},"items":[
            {"id":1,"project":"GoDD-Rosseta","title":"サイトを修正する","status":"doing","progress":35},
            {"id":2,"project":"GoDD Rosetta","title":"secret free prose","status":"todo"},
            {"id":3,"project":"civ6-banseiki","status":"todo"},
            {"id":4,"project":"ARPS","title":"private customer data","status":"doing"},
            {"id":5,"project":"SecretClient","title":"履歴を検証する SecretClient","status":"done","updated_at":"2020-01-01T00:00:00Z"},
            {"id":6,"project":"SocialPilot","title":"サイトを修正する","status":"wait","wait_on":[1]}
        ]}).to_string()).unwrap();
        let snapshot = collect(root, 1_000_000);
        let list = |department| {
            snapshot["departments"]
                .as_array()
                .unwrap()
                .iter()
                .find(|d| d["name"] == department)
                .unwrap()["products"]
                .as_array()
                .unwrap()
                .clone()
        };
        assert_eq!(
            list("GoDD事業部")
                .iter()
                .map(|p| p["name"].as_str().unwrap())
                .collect::<Vec<_>>(),
            [
                "GoDD 13a13e1",
                "GoDD AO",
                "GoDD Agent",
                "GoDD Creators",
                "GoDD Genesis",
                "GoDD MCP α",
                "GoDD Press",
                "GoDD Rosetta",
                "GoDD Virtual Streamer"
            ]
        );
        assert_eq!(
            list("自社サービス部")
                .iter()
                .map(|p| p["name"].as_str().unwrap())
                .collect::<Vec<_>>(),
            [
                "ALT-LINE",
                "cashcow",
                "CoChara",
                "Geonaut",
                "Git道",
                "KaraokeMaker",
                "OmegaPortal",
                "PassShare",
                "QaJaRuEn",
                "Re:X",
                "Scoopa",
                "no-a"
            ]
        );
        assert_eq!(
            list("情報システム部")
                .iter()
                .map(|p| p["name"].as_str().unwrap())
                .collect::<Vec<_>>(),
            ["PCの道具", "local-ai-concole", "OpenCodex", "Skills"]
        );
        assert_eq!(
            list("ゲーム部")
                .iter()
                .map(|p| p["name"].as_str().unwrap())
                .collect::<Vec<_>>(),
            ["万世記", "PhosGaea"]
        );
        assert_eq!(
            list("受託部")
                .iter()
                .map(|p| p["name"].as_str().unwrap())
                .collect::<Vec<_>>(),
            ["YCC-HP", "ARPS", "GoDD MCP"]
        );
        let rosetta = list("GoDD事業部")
            .into_iter()
            .find(|p| p["name"] == "GoDD Rosetta")
            .unwrap();
        assert_eq!(rosetta["workers"].as_array().unwrap().len(), 2);
        assert_eq!(rosetta["workers"][0]["progress"], 35);
        assert!(
            list("自社サービス部")
                .iter()
                .all(|p| p["name"] != "Social Pilot")
        );
        let extras = snapshot["unlistedRegistrations"].as_array().unwrap();
        assert_eq!(extras.len(), 3);
        assert!(extras.iter().any(
            |p| p["name"] == "Skills" && p["previouslyAssignedDepartment"] == "情報システム部"
        ));
        assert!(
            extras
                .iter()
                .any(|p| p["name"] == "Social Pilot" && p["unfinishedTasks"] == 1)
        );
        assert!(extras.iter().any(|p| p["registeredTasks"] == 1
            && p["unfinishedTasks"] == 0
            && p["taskIds"] == json!([5])));
        for hidden in [
            "SecretClient",
            "secret free prose",
            "private customer data",
            "D:/private",
        ] {
            assert!(!snapshot.to_string().contains(hidden));
        }
        assert!(!root.join("client-projects.txt").exists());
        assert_eq!(snapshot["metadataOk"], true);
        assert_eq!(snapshot["sourceOk"], false);
    }
    #[test]
    fn membership_choice_is_scoped_to_the_verified_owner_question_and_resource_set() {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path();
        fs::create_dir_all(root.join("localai-web-rs/target/release")).unwrap();
        fs::create_dir_all(root.join("discord-ask")).unwrap();
        let id = "registration-0123456789abcdef";
        let path = root.join("localai-web-rs/target/release/operations-membership-choice.json");
        let mut choice = json!({"selection":"separate","source":"discord-owner-reply","todo_id":2473,
            "author_id":"100","question_thread_id":"300","question_message_id":"300","message_id":"301",
            "answered_at":"2026-10-12T14:00:00Z","registration_ids":[id]});
        fs::write(root.join("discord-ask/recovery-messages.json"),json!({"domain":"new-owner-recovery-messages","events":[
            {"event":"intent","request":{"request_id":"fixture","destination":{"owner":"100","guild":"200"},
                "operation":{"kind":"ask","todo_id":2473,"decision_original":{"resources":[{"id":id}]}}}},
            {"event":"verified","request_id":"fixture","phase":"question:0","receipt":{"thread_id":"300","message_id":"300"}}
        ]}).to_string()).unwrap();
        fs::write(&path, choice.to_string()).unwrap();
        let now = stamp(&json!("2026-10-12T14:10:00Z")).unwrap();
        let (ids, info) = membership_choice(root, now).unwrap();
        assert!(ids.contains(id));
        assert_eq!(info["selection"], "separate");
        assert_eq!(info["questionUrl"], "https://discord.com/channels/200/300");
        for (key, value) in [
            ("author_id", json!("other")),
            ("question_message_id", json!("299")),
            ("message_id", json!("299")),
            ("registration_ids", json!(["registration-ffffffffffffffff"])),
        ] {
            let original = choice[key].clone();
            choice[key] = value;
            fs::write(&path, choice.to_string()).unwrap();
            assert!(membership_choice(root, now).is_none());
            choice[key] = original;
        }
        assert!(!root.join("client-projects.txt").exists());
    }
    #[test]
    fn stale_and_future_monitor_records_cannot_report_current_capacity() {
        assert!(!fresh(Some(1), 300_000, 180_000));
        assert!(!fresh(Some(400_001), 300_000, 180_000));
        assert!(fresh(Some(290_000), 300_000, 180_000));
        assert_eq!(departments(Tree::new()).len(), 6);
    }
}
