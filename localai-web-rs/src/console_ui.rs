//! Shared presentation for the Console and its existing music pages.
const SHELL: &str = include_str!("console_shell.html");
const MARKER: &str = "<meta name=\"local-ai-console-shell\" content=\"1\">";

use cc_ask::task_title::Names;
use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use std::sync::LazyLock;

const CLIENT: &str = "クライアント案件";
static REDACTOR: LazyLock<cc_redact::Redactor> =
    LazyLock::new(|| cc_redact::Redactor::with_env(std::env::vars()));
fn text<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key].as_str().unwrap_or("")
}
fn rows<'a>(value: &'a Value, key: &str) -> &'a [Value] {
    value[key].as_array().map(Vec::as_slice).unwrap_or(&[])
}
fn secret_pattern() -> &'static regex::Regex {
    // Same credential patterns as Operations, without its summary truncation.
    static SECRET: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(
        r#"(?i)(?:(?:api[_ -]?key|(?:access|refresh|id|discord|client)[_ -]?(?:token|secret)|token|password|passwd|secret|authorization|パスワード|認証コード)["']?\s*[:=]\s*["']?\S+|\bBearer\s+\S+|\b(?:sk-|ghp_|gho_|github_pat_)[A-Za-z0-9_-]+)"#
    ).unwrap()
    });
    &SECRET
}
pub(crate) fn clean(names: &Names, value: &str) -> String {
    static LOCATION: LazyLock<regex::Regex> =
        LazyLock::new(|| regex::Regex::new(r"https?://\S+|[A-Za-z]:[/\\]\S+").unwrap());
    let value = cc_ask::task_title::canonical_text(&names.redact(&REDACTOR.text(value)));
    let value = secret_pattern().replace_all(&value, "[非表示]");
    LOCATION.replace_all(&value, "[非表示]").into_owned()
}
fn spec_path(names: &Names, value: &str) -> String {
    let bytes = value.as_bytes();
    if bytes.len() < 3
        || !bytes[0].is_ascii_alphabetic()
        || bytes[1] != b':'
        || !matches!(bytes[2], b'/' | b'\\')
        || value.chars().any(char::is_control)
        || names.project(value) == CLIENT
    {
        return "[非表示]".into();
    }
    secret_pattern()
        .replace_all(&REDACTOR.text(value), "[非表示]")
        .into_owned()
}
fn timestamp(value: &Value) -> Option<String> {
    if let Some(seconds) = value.as_u64() {
        return i64::try_from(seconds)
            .ok()
            .and_then(|n| chrono::DateTime::from_timestamp(n, 0))
            .map(|date| date.to_rfc3339());
    }
    let raw = value.as_str()?;
    if let Ok(date) = chrono::DateTime::parse_from_rfc3339(raw) {
        return Some(date.to_rfc3339());
    }
    ["%Y-%m-%d %H:%M", "%Y-%m-%d %H:%M:%S"]
        .iter()
        .find_map(|format| {
            chrono::NaiveDateTime::parse_from_str(raw, format)
                .ok()?
                .and_local_timezone(chrono::FixedOffset::east_opt(9 * 3600)?)
                .single()
                .map(|date| date.to_rfc3339())
        })
}
fn updated(value: &Value) -> Option<String> {
    ["at", "updated_at", "updatedAt", "timestamp", "updated"]
        .iter()
        .find_map(|key| timestamp(&value[*key]))
}
fn status(value: &Value) -> &str {
    let status = text(value, "status");
    if matches!(
        status,
        "todo"
            | "doing"
            | "block"
            | "wait"
            | "done"
            | "queued"
            | "starting"
            | "running"
            | "waiting"
            | "pending"
            | "succeeded"
            | "completed"
            | "failed"
            | "cancelled"
            | "lost"
    ) {
        status
    } else {
        "unknown"
    }
}
fn job_id(value: &Value) -> String {
    use std::hash::{Hash, Hasher};
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    text(value, "id").hash(&mut hash);
    format!("job-{:016x}", hash.finish())
}
#[cfg(test)]
fn linked(item: &Value, job: &Value) -> bool {
    item["id"]
        .as_u64()
        .is_some_and(|id| job["todo_id"].as_u64() == Some(id))
        || ["id", "thread_id", "resume_session"].iter().any(|key| {
            job[*key].as_str().is_some_and(|id| {
                !id.is_empty()
                    && (item["job"].as_str() == Some(id)
                        || item["scheduler"]["job"].as_str() == Some(id)
                        || rows(item, "jobs")
                            .iter()
                            .any(|saved| saved.as_str() == Some(id)))
            })
        })
}
#[cfg(test)]
fn private_item(names: &Names, db: &Value, item: &Value) -> bool {
    [
        text(item, "project"),
        text(item, "cwd"),
        text(item, "spec"),
        db["projects"][text(item, "project")].as_str().unwrap_or(""),
    ]
    .iter()
    .any(|value| names.project(value) == CLIENT)
}
/// Read only metadata to establish privacy before considering any job message.
pub fn private_job(names: &Names, db: &Value, job: &Value) -> bool {
    PrivacyIndex::new(names, db).is_private_job(job)
}

// Request-local indexes over the same immutable snapshots. No record messages,
// filesystem reads or cross-request state are used to establish privacy.
pub struct PrivacyIndex<'a> {
    names: &'a Names,
    db: &'a Value,
    values: HashMap<String, bool>,
    private_paths: HashSet<String>,
    item_private: Vec<bool>,
    items_by_id: HashMap<u64, Vec<usize>>,
    items_by_job: HashMap<&'a str, Vec<usize>>,
}

impl<'a> PrivacyIndex<'a> {
    pub fn new(names: &'a Names, db: &'a Value) -> Self {
        let mut index = Self {
            names,
            db,
            values: HashMap::new(),
            private_paths: HashSet::new(),
            item_private: Vec::with_capacity(rows(db, "items").len()),
            items_by_id: HashMap::new(),
            items_by_job: HashMap::new(),
        };
        if let Some(projects) = db["projects"].as_object() {
            for (project, path) in projects {
                let path = path.as_str().unwrap_or("");
                if index.client_value(project) || index.client_value(path) {
                    let base = path.replace('\\', "/").trim_end_matches('/').to_lowercase();
                    if !base.is_empty() {
                        index.private_paths.insert(base);
                    }
                }
            }
        }
        for (position, item) in rows(db, "items").iter().enumerate() {
            let private = [
                text(item, "project"),
                text(item, "cwd"),
                text(item, "spec"),
                db["projects"][text(item, "project")].as_str().unwrap_or(""),
            ]
            .into_iter()
            .any(|value| index.client_value(value));
            index.item_private.push(private);
            if let Some(id) = item["id"].as_u64() {
                index.items_by_id.entry(id).or_default().push(position);
            }
            for id in item["job"]
                .as_str()
                .into_iter()
                .chain(item["scheduler"]["job"].as_str())
                .chain(rows(item, "jobs").iter().filter_map(Value::as_str))
                .filter(|id| !id.is_empty())
            {
                index.items_by_job.entry(id).or_default().push(position);
            }
        }
        index
    }

    fn client_value(&mut self, value: &str) -> bool {
        if let Some(&private) = self.values.get(value) {
            return private;
        }
        let private = self.names.project(value) == CLIENT;
        self.values.insert(value.to_owned(), private);
        private
    }

    fn linked_items(&self, job: &Value) -> Vec<usize> {
        let mut items = Vec::new();
        if let Some(positions) = job["todo_id"]
            .as_u64()
            .and_then(|id| self.items_by_id.get(&id))
        {
            items.extend_from_slice(positions);
        }
        for id in ["id", "thread_id", "resume_session"]
            .into_iter()
            .filter_map(|key| job[key].as_str())
            .filter(|id| !id.is_empty())
        {
            if let Some(positions) = self.items_by_job.get(id) {
                items.extend_from_slice(positions);
            }
        }
        // Multiple aliases may identify the same edge. Keep original item order
        // and preserve distinct records even when their IDs are duplicated.
        items.sort_unstable();
        items.dedup();
        items
    }

    /// Reuse one index for a batch; only metadata is inspected, never messages.
    pub fn is_private_job(&mut self, job: &Value) -> bool {
        let items = self.linked_items(job);
        self.private_job(job, &items)
    }

    fn private_job(&mut self, job: &Value, items: &[usize]) -> bool {
        [
            text(job, "project"),
            text(job, "cwd"),
            self.db["projects"][text(job, "project")]
                .as_str()
                .unwrap_or(""),
        ]
        .into_iter()
        .any(|value| self.client_value(value))
            || items.iter().any(|&position| self.item_private[position])
            || self.private_cwd(text(job, "cwd"))
    }

    fn private_cwd(&self, cwd: &str) -> bool {
        let cwd = cwd.replace('\\', "/").to_lowercase();
        self.private_paths.contains(&cwd)
            || cwd.match_indices('/').any(|(at, _)| {
                let base = &cwd[..at];
                self.private_paths.contains(base)
                    || base
                        .strip_suffix("-wt")
                        .is_some_and(|base| self.private_paths.contains(base))
            })
    }
}

struct DisplayIndex {
    item_private: Vec<bool>,
    job_private: Vec<bool>,
    item_jobs: Vec<Vec<usize>>,
    job_items: Vec<Vec<usize>>,
}

impl DisplayIndex {
    fn new(names: &Names, db: &Value, jobs: &[Value]) -> Self {
        let mut privacy = PrivacyIndex::new(names, db);
        let mut item_jobs = vec![Vec::new(); privacy.item_private.len()];
        let mut job_items = Vec::with_capacity(jobs.len());
        let mut job_private = Vec::with_capacity(jobs.len());
        for (position, job) in jobs.iter().enumerate() {
            let items = privacy.linked_items(job);
            job_private.push(privacy.private_job(job, &items));
            for &item in &items {
                item_jobs[item].push(position);
            }
            job_items.push(items);
        }
        Self {
            item_private: privacy.item_private,
            job_private,
            item_jobs,
            job_items,
        }
    }
}
pub(crate) fn history(names: &Names, item: &Value) -> Vec<Value> {
    history_page(names, item, 0, usize::MAX)
}
pub(crate) fn history_count(item: &Value) -> usize {
    ["history", "note_history", "spec_history"]
        .iter()
        .map(|key| rows(item, key).len())
        .sum()
}
pub(crate) fn history_page(names: &Names, item: &Value, offset: usize, limit: usize) -> Vec<Value> {
    // Sort metadata first, then redact only the requested page. Full historical
    // notes must not be materialized on every 15-second board refresh.
    let mut entries: Vec<_> = ["history", "note_history", "spec_history"]
        .into_iter()
        .flat_map(|key| rows(item, key).iter().map(move |entry| (key, entry)))
        .collect();
    entries.sort_by_key(|(_, entry)| {
        updated(entry).and_then(|raw| chrono::DateTime::parse_from_rfc3339(&raw).ok())
    });
    entries.into_iter().skip(offset).take(limit).map(|(key, entry)| {
        if key == "spec_history" {
            return json!({"at":updated(entry), "note":format!("以前の指示書: {}", spec_path(names, text(entry, "spec")))});
        }
            let message = entry.as_str().unwrap_or_else(|| {
                ["note", "message", "summary", "status"]
                    .iter()
                    .find_map(|key| entry[*key].as_str())
                    .unwrap_or("内容未記録")
            });
            json!({"at":updated(entry), "note":clean(names, message)})
        })
        .collect()
}
pub(crate) fn question_url(item: &Value) -> Option<String> {
    static URL: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(r"^https://(?:www\.)?discord\.com/channels/\d+/\d+(?:/\d+)?/?$").unwrap()
    });
    ["questionUrl", "question_url"]
        .iter()
        .map(|key| text(item, key))
        .chain(text(item, "note").split_whitespace())
        .find(|value| URL.is_match(value))
        .map(str::to_owned)
}

/// Explicit display allowlist. Never clone machine records or reservation state.
pub fn display_todos(names: &Names, db: &Value, jobs: &[Value]) -> Value {
    let index = DisplayIndex::new(names, db, jobs);
    let items: Vec<_> = rows(db, "items").iter().enumerate().map(|(position, item)| {
        let client = index.item_private[position] || index.item_jobs[position].iter().any(|&job| index.job_private[job]);
        let linked_jobs: Vec<_> = index.item_jobs[position].iter().map(|&job| &jobs[job]).map(|job|
            json!({"id":job_id(job), "name":if client { "担当".into() } else { clean(names, text(job, "name")) }, "status":status(job)})).collect();
        let waits: Vec<_> = rows(item, "wait_on").iter().filter_map(Value::as_u64).collect();
        let mut displayed = json!({"id":item["id"].as_u64(), "project":if client { CLIENT.into() } else { clean(names, &cc_ask::task_title::canonical_project(text(item, "project"))) },
            "title":if client { "クライアント案件の作業（内容は非表示）".into() } else { clean(names, text(item, "title")) },
            "status":status(item), "note":if client { String::new() } else { clean(names, text(item, "note")) },
            "progress":item["progress"].as_u64().filter(|p| *p <= 100),
            "agent":if client { "担当".into() } else { clean(names, text(item, "agent")) },
            "updated_at":updated(item), "linked_jobs":linked_jobs, "wait_on":waits,
            "questionUrl":if client { None } else { question_url(item) },
            "spec":if client || text(item, "spec").is_empty() { None } else { Some(spec_path(names, text(item, "spec"))) },
            "history":if client { Vec::new() } else { history(names, item) }});
        let facts=crate::work_facts::project(item,client,|value|clean(names,value));
        displayed.as_object_mut().unwrap().extend(facts.as_object().unwrap().clone());
        displayed
    }).collect();
    json!({"items":items})
}
pub fn display_jobs(names: &Names, db: &Value, jobs: &[Value]) -> Value {
    let index = DisplayIndex::new(names, db, jobs);
    json!({"jobs":jobs.iter().enumerate().map(|(position, job)| {
        let client = index.job_private[position];
        let title = job["task_title"].as_str().unwrap_or_else(|| text(job, "name"));
        let mut displayed = json!({"id":job_id(job), "task_title":if client { "クライアント案件の作業（内容は非表示）".into() } else { clean(names, title) },
            "status":status(job), "started_at":timestamp(&job["started_at"]).or_else(|| timestamp(&job["created_at"])),
            "finished_at":timestamp(&job["finished_at"]), "summary":if client { String::new() } else { clean(names, text(job, "last_message")) }});
        let items = rows(db, "items");
        let source = job["todo_id"].as_u64()
            .and_then(|id| index.job_items[position].iter().find(|&&item| items[item]["id"].as_u64() == Some(id)))
            .or_else(|| index.job_items[position].first())
            .map(|&item| &items[item]);
        let facts=crate::work_facts::project(source.unwrap_or(job),client,|value|clean(names,value));
        displayed["factsSource"] = json!(if client { "内容は非表示".into() }
            else if let Some(item) = source { item["id"].as_u64().map(|id| format!("共有ToDo #{id}"))
                .unwrap_or_else(|| "共有ToDo（番号未記録）".into()) }
            else { String::from("ジョブの記録") });
        // Preserve existing final-report history as a separate field. Explicit
        // outcome facts never treat that report or process exit as a result.
        displayed["report"] = displayed["summary"].clone();
        displayed.as_object_mut().unwrap().extend(facts.as_object().unwrap().clone());
        displayed
    }).collect::<Vec<_>>()})
}
pub fn handoff_job(job: &Value) -> bool {
    text(job, "name").starts_with("orch-")
        && matches!(
            status(job),
            "succeeded" | "completed" | "failed" | "cancelled" | "lost"
        )
}
pub fn display_handoff(
    names: &Names,
    db: &Value,
    jobs: &[Value],
    enabled: bool,
    last_tick: u64,
    pr_count: usize,
    prs_error: bool,
) -> Value {
    let todos = display_todos(names, db, jobs);
    let displayed_jobs = display_jobs(names, db, jobs);
    let mut output = format!(
        "自動委譲: {}\n\n対応中・判断待ちのToDo\n",
        if enabled { "有効" } else { "停止" }
    );
    for item in rows(&todos, "items")
        .iter()
        .filter(|item| matches!(status(item), "doing" | "block" | "wait"))
    {
        output.push_str(&format!(
            "#{} [{}] {}: {}\n  {}\n",
            item["id"],
            status(item),
            text(item, "project"),
            text(item, "title"),
            text(item, "note")
        ));
    }
    output.push_str("\n実行中のジョブ\n");
    for job in rows(&displayed_jobs, "jobs")
        .iter()
        .filter(|job| matches!(status(job), "queued" | "starting" | "running" | "waiting"))
    {
        output.push_str(&format!(
            "{} [{}] {}\n",
            text(job, "id"),
            status(job),
            text(job, "task_title")
        ));
    }
    output.push_str(&format!("\n未完了のPR: {pr_count}件\n"));
    let decisions: Vec<_> = jobs.iter().filter(|job| handoff_job(job))
        .take(10).map(|job| json!({"id":job_id(job), "summary":if private_job(names, db, job) { "クライアント案件の記録（内容は非表示）".into() } else { clean(names, text(job, "last_message")) }})).collect();
    json!({"text":output, "decisions":decisions, "prs_error":if prs_error { Some("PRの状態を取得できません。") } else { None },
        "delegation":{"enabled":enabled, "last_tick":timestamp(&json!(last_tick))}})
}

/// Keep existing forms, CSRF values, routes, media and scripts intact.
pub fn decorate_html(html: &str) -> String {
    if html.contains(MARKER) {
        return html.to_owned();
    }
    let at = html
        .as_bytes()
        .windows(7)
        .rposition(|candidate| candidate.eq_ignore_ascii_case(b"</body>"))
        .unwrap_or(html.len());
    let mut output = String::with_capacity(html.len() + SHELL.len() + MARKER.len());
    output.push_str(&html[..at]);
    output.push_str(MARKER);
    output.push_str(SHELL);
    output.push_str(&html[at..]);
    output
}

#[cfg(test)]
mod tests {
    use super::*;

    fn assert_matches_legacy(names: &Names, db: &Value, jobs: &[Value]) {
        let before = (db.clone(), jobs.to_vec());
        let index = DisplayIndex::new(names, db, jobs);
        assert_eq!(
            index.item_private,
            rows(db, "items")
                .iter()
                .map(|item| private_item(names, db, item))
                .collect::<Vec<_>>()
        );
        assert_eq!(
            index.job_private,
            jobs.iter()
                .map(|job| legacy_private_job(names, db, job))
                .collect::<Vec<_>>()
        );
        assert_eq!(
            index.item_jobs,
            rows(db, "items")
                .iter()
                .map(|item| jobs
                    .iter()
                    .enumerate()
                    .filter(|(_, job)| linked(item, job))
                    .map(|(i, _)| i)
                    .collect::<Vec<_>>())
                .collect::<Vec<_>>()
        );
        assert_eq!(
            index.job_items,
            jobs.iter()
                .map(|job| rows(db, "items")
                    .iter()
                    .enumerate()
                    .filter(|(_, item)| linked(item, job))
                    .map(|(i, _)| i)
                    .collect::<Vec<_>>())
                .collect::<Vec<_>>()
        );
        assert_eq!(
            display_todos(names, db, jobs),
            legacy_display_todos(names, db, jobs)
        );

        // Job facts deliberately gain a shared-ToDo source. All other fields
        // remain the frozen output; choose that source with the old scan oracle.
        let mut expected = legacy_display_jobs(names, db, jobs);
        for (position, job) in jobs.iter().enumerate() {
            let items = rows(db, "items");
            let source = job["todo_id"]
                .as_u64()
                .and_then(|id| items.iter().find(|item| item["id"].as_u64() == Some(id)))
                .or_else(|| items.iter().find(|item| linked(item, job)));
            let private = legacy_private_job(names, db, job);
            let facts = crate::work_facts::project(source.unwrap_or(job), private, |value| {
                clean(names, value)
            });
            expected["jobs"][position]
                .as_object_mut()
                .unwrap()
                .extend(facts.as_object().unwrap().clone());
            expected["jobs"][position]["factsSource"] = json!(if private {
                "内容は非表示".into()
            } else if let Some(item) = source {
                item["id"]
                    .as_u64()
                    .map(|id| format!("共有ToDo #{id}"))
                    .unwrap_or_else(|| "共有ToDo（番号未記録）".into())
            } else {
                String::from("ジョブの記録")
            });
        }
        assert_eq!(display_jobs(names, db, jobs), expected);
        assert_eq!((db.clone(), jobs.to_vec()), before);
    }

    #[test]
    fn indexed_display_matches_scan_for_every_link_source_and_masks_linked_private_items() {
        let names = Names::parse("SecretClient");
        let mut cases = 0;
        for source in ["job", "scheduler.job", "jobs", "todo_id"] {
            for job_key in ["id", "thread_id", "resume_session"] {
                if source == "todo_id" && job_key != "id" {
                    continue;
                }
                let mut db = json!({"projects":{"public":"D:/public","client_alias":"D:/SecretClient"},"items":[
                    {"id":1,"project":"public","title":"artificial-hidden-public","note":"artificial-hidden-note","status":"doing",
                     "history":["artificial-hidden-history"],"spec":"D:/public/spec.md","questionUrl":"https://discord.com/channels/1/2"},
                    {"id":2,"project":"client_alias","title":"artificial-hidden-client","status":"block","note":"artificial-hidden-note"}
                ]});
                let mut job = json!({"id":"different-id","status":"running","cwd":"D:/public","name":"artificial-hidden-job","last_message":"artificial-hidden-report"});
                if source == "todo_id" {
                    db["items"][0]["id"] = json!(2);
                    job["todo_id"] = json!(2);
                } else {
                    job[job_key] = json!("edge");
                    for item in db["items"].as_array_mut().unwrap() {
                        match source {
                            "scheduler.job" => item["scheduler"] = json!({"job":"edge"}),
                            "jobs" => item["jobs"] = json!(["edge", "edge", null, 17]),
                            _ => item["job"] = json!("edge"),
                        }
                    }
                }
                let jobs = vec![
                    job,
                    json!({"id":"unlinked","status":"succeeded","name":"公開ジョブ","last_message":"公開報告"}),
                ];
                let index = DisplayIndex::new(&names, &db, &jobs);
                assert_eq!(index.item_jobs, vec![vec![0], vec![0]]);
                assert_eq!(index.job_items, vec![vec![0, 1], vec![]]);
                assert_eq!(index.job_private, vec![true, false]);
                assert_matches_legacy(&names, &db, &jobs);
                let output = json!({"todo":display_todos(&names,&db,&jobs),"jobs":display_jobs(&names,&db,&jobs)}).to_string();
                assert!(!output.contains("artificial-hidden"));
                assert!(!output.contains("SecretClient"));
                cases += 1;
            }
        }
        assert_eq!(cases, 10);
    }

    #[test]
    fn indexed_privacy_preserves_registered_path_boundaries_aliases_and_duplicate_records() {
        let names = Names::parse("SecretClient");
        let db = json!({"projects":{"SecretClient":"D:/neutral-base/","client_alias":"D:/SecretClient"},"items":[
            {"id":7,"project":"public","job":"edge","scheduler":{"job":"edge"},"jobs":["edge","edge",""]},
            {"id":7,"project":"public","jobs":["edge"]},
            {"id":"7","project":"client_alias","job":"private-edge"},
            {"project":"public","job":"","jobs":["",null,7]}
        ]});
        let jobs = vec![
            json!({"id":"edge","thread_id":"edge","resume_session":"edge","todo_id":7,"cwd":"D:/public"}),
            json!({"id":"edge","todo_id":7,"cwd":"D:/public"}),
            json!({"id":"","todo_id":"7","cwd":"D:/public"}),
            json!({"id":"private-edge","cwd":"D:/public"}),
            json!({"id":"direct-alias","project":"client_alias","cwd":"D:/public"}),
            json!({"id":"root","cwd":"d:\\NEUTRAL-base"}),
            json!({"id":"child","cwd":"D:/neutral-base/child"}),
            json!({"id":"worktree","cwd":"D:/neutral-base-wt/child"}),
            json!({"id":"similar","cwd":"D:/neutral-base-other/child"}),
            json!({"id":"bare-worktree","cwd":"D:/neutral-base-wt"}),
            json!({"id":"similar-worktree","cwd":"D:/neutral-base-wt-other/child"}),
        ];
        let index = DisplayIndex::new(&names, &db, &jobs);
        assert_eq!(
            index.item_jobs,
            vec![vec![0, 1], vec![0, 1], vec![3], vec![]]
        );
        assert_eq!(
            index.job_private,
            vec![false, false, false, true, true, true, true, true, false, false, false]
        );
        assert_matches_legacy(&names, &db, &jobs);
        let mut reusable = PrivacyIndex::new(&names, &db);
        assert_eq!(
            jobs.iter()
                .map(|job| reusable.is_private_job(job))
                .collect::<Vec<_>>(),
            index.job_private
        );
        // A caller may create and discard jobs while retaining the batch index.
        assert!(reusable.is_private_job(&json!({"cwd":"D:/neutral-base/next"})));
        assert!(!reusable.is_private_job(&json!({"cwd":"D:/public/next"})));
    }

    #[test]
    fn job_facts_prioritize_primary_todo_fallback_in_record_order_and_hide_any_private_link() {
        let names = Names::parse("SecretClient");
        let db = json!({"projects":{"public":"D:/public","client_alias":"D:/SecretClient"},"items":[
            {"id":2155,"project":"public","jobs":["shared"],"work_status":{"summary":"関連作業","stage":"関連工程"}},
            {"id":2160,"project":"public","jobs":["shared"],"note":"要約: 画面を確認\n最終成果: 受入に合格\n最終成果の時刻: 2026-10-10T15:00:00+09:00\n現在の工程: 仕上げ\n停止理由: なし\n次の行動: 報告\n依存先: なし"},
            {"id":3,"project":"client_alias","job":"private","work_status":{"summary":"artificial-hidden-facts"}}
        ]});
        let jobs = vec![
            json!({"id":"shared","todo_id":2160,"status":"running","activity":"thinking","last_message":"最終報告"}),
            json!({"id":"shared","todo_id":9999,"status":"running","work_status":{"stage":"ジョブ自身の工程"}}),
            json!({"id":"unlinked","status":"succeeded","work_status":{"summary":"独立作業","stage":"独立工程"}}),
            json!({"id":"private","todo_id":2160,"status":"running"}),
            json!({"id":"bare","status":"running","activity":"thinking","finished_at":"2026-10-10T15:00:00+09:00"}),
        ];
        assert_matches_legacy(&names, &db, &jobs);
        let shown = display_jobs(&names, &db, &jobs);
        let view = rows(&shown, "jobs");
        assert_eq!(view.len(), 5);
        assert_eq!(
            view.iter()
                .map(|job| text(job, "factsSource"))
                .collect::<Vec<_>>(),
            vec![
                "共有ToDo #2160",
                "共有ToDo #2155",
                "ジョブの記録",
                "内容は非表示",
                "ジョブの記録"
            ]
        );
        let fields = [
            "summary",
            "lastResult",
            "lastResultAt",
            "currentStage",
            "stopReason",
            "nextAction",
            "dependencySummary",
            "dependencies",
        ];
        let facts: serde_json::Map<_, _> = fields
            .into_iter()
            .map(|key| (key.to_owned(), view[0][key].clone()))
            .collect();
        assert_eq!(
            Value::Object(facts),
            json!({"summary":"画面を確認","lastResult":"受入に合格","lastResultAt":"2026-10-10T15:00:00+09:00",
            "currentStage":"仕上げ","stopReason":"なし","nextAction":"報告","dependencySummary":"なし","dependencies":[]})
        );
        assert_eq!(view[0]["report"], "最終報告");
        assert_eq!(view[1]["currentStage"], "関連工程");
        assert_eq!(view[2]["currentStage"], "独立工程");
        assert_eq!(view[3]["currentStage"], "内容は非表示");
        assert_eq!(view[3]["report"], "");
        assert!(view[3]["lastResultAt"].is_null());
        assert_eq!(view[4]["currentStage"], "未記録");
        assert_eq!(view[4]["lastResult"], "未記録");
        assert!(view[4]["lastResultAt"].is_null());
        assert!(!shown.to_string().contains("artificial-hidden"));
    }

    // Frozen d18c4df algorithms: independent scanning oracle for index equivalence.
    fn legacy_private_job(names: &Names, db: &Value, job: &Value) -> bool {
        let cwd = text(job, "cwd").replace('\\', "/").to_lowercase();
        [
            text(job, "project"),
            text(job, "cwd"),
            db["projects"][text(job, "project")].as_str().unwrap_or(""),
        ]
        .iter()
        .any(|v| names.project(v) == CLIENT)
            || rows(db, "items")
                .iter()
                .any(|item| linked(item, job) && private_item(names, db, item))
            || db["projects"].as_object().is_some_and(|projects| {
                projects.iter().any(|(project, path)| {
                    let base = path
                        .as_str()
                        .unwrap_or("")
                        .replace('\\', "/")
                        .trim_end_matches('/')
                        .to_lowercase();
                    !base.is_empty()
                        && (cwd == base
                            || cwd.starts_with(&format!("{base}/"))
                            || cwd.starts_with(&format!("{base}-wt/")))
                        && (names.project(project) == CLIENT
                            || names.project(path.as_str().unwrap_or("")) == CLIENT)
                })
            })
    }
    fn legacy_display_todos(names: &Names, db: &Value, jobs: &[Value]) -> Value {
        let items: Vec<_> = rows(db, "items").iter().map(|item| {
        let client = private_item(names, db, item) || jobs.iter().any(|job| linked(item, job) && legacy_private_job(names, db, job));
        let linked_jobs: Vec<_> = jobs.iter().filter(|job| linked(item, job)).map(|job|
            json!({"id":job_id(job), "name":if client { "担当".into() } else { clean(names, text(job, "name")) }, "status":status(job)})).collect();
        let waits: Vec<_> = rows(item, "wait_on").iter().filter_map(Value::as_u64).collect();
        let mut displayed = json!({"id":item["id"].as_u64(), "project":if client { CLIENT.into() } else { clean(names, &cc_ask::task_title::canonical_project(text(item, "project"))) },
            "title":if client { "クライアント案件の作業（内容は非表示）".into() } else { clean(names, text(item, "title")) },
            "status":status(item), "note":if client { String::new() } else { clean(names, text(item, "note")) },
            "progress":item["progress"].as_u64().filter(|p| *p <= 100),
            "agent":if client { "担当".into() } else { clean(names, text(item, "agent")) },
            "updated_at":updated(item), "linked_jobs":linked_jobs, "wait_on":waits,
            "questionUrl":if client { None } else { question_url(item) },
            "spec":if client || text(item, "spec").is_empty() { None } else { Some(spec_path(names, text(item, "spec"))) },
            "history":if client { Vec::new() } else { history(names, item) }});
        let facts=crate::work_facts::project(item,client,|value|clean(names,value));
        displayed.as_object_mut().unwrap().extend(facts.as_object().unwrap().clone());
        displayed
    }).collect();
        json!({"items":items})
    }
    fn legacy_display_jobs(names: &Names, db: &Value, jobs: &[Value]) -> Value {
        json!({"jobs":jobs.iter().map(|job| {
        let client = legacy_private_job(names, db, job);
        let title = job["task_title"].as_str().unwrap_or_else(|| text(job, "name"));
        let mut displayed = json!({"id":job_id(job), "task_title":if client { "クライアント案件の作業（内容は非表示）".into() } else { clean(names, title) },
            "status":status(job), "started_at":timestamp(&job["started_at"]).or_else(|| timestamp(&job["created_at"])),
            "finished_at":timestamp(&job["finished_at"]), "summary":if client { String::new() } else { clean(names, text(job, "last_message")) }});
        let facts=crate::work_facts::project(job,client,|value|clean(names,value));
        // Preserve existing final-report history as a separate field. Explicit
        // outcome facts never treat that report or process exit as a result.
        displayed["report"] = displayed["summary"].clone();
        displayed.as_object_mut().unwrap().extend(facts.as_object().unwrap().clone());
        displayed
    }).collect::<Vec<_>>()})
    }

    #[test]
    fn outcome_fields_are_shared_by_task_and_job_views_and_private_fields_are_hidden() {
        let names = Names::parse("SecretClient\n");
        let note = "要約: 表示を更新\n最終成果: フォーム検査に合格\n最終成果の時刻: 2026-10-10T15:00:00+09:00\n現在の工程: 配備\n停止理由: なし\n次の行動: 画面を確認\n依存先: なし";
        let db = json!({"projects":{"tools":"D:/demo/tools","alias":"D:/demo/SecretClient"},"items":[
            {"id":1,"project":"tools","title":"画面を整える","status":"doing","note":note},
            {"id":2,"project":"alias","title":"SecretClient","status":"doing","note":note}
        ]});
        let jobs = vec![
            json!({"id":"j1","todo_id":1,"status":"running","cwd":"D:/demo/tools","activity":"thinking","last_message":"最終報告だけでは成果と断定しない","note":note}),
        ];
        let shown = display_todos(&names, &db, &jobs);
        assert_eq!(shown["items"][0]["currentStage"], "配備");
        assert_eq!(
            shown["items"][0]["lastResultAt"],
            "2026-10-10T15:00:00+09:00"
        );
        assert!(shown["items"][1]["lastResultAt"].is_null());
        assert_eq!(shown["items"][1]["nextAction"], "内容は非表示");
        let projected = display_jobs(&names, &db, &jobs);
        assert_eq!(projected["jobs"][0]["lastResult"], "フォーム検査に合格");
        assert_eq!(
            projected["jobs"][0]["report"],
            "最終報告だけでは成果と断定しない"
        );
        let bare = display_jobs(
            &names,
            &db,
            &[
                json!({"id":"j2","status":"succeeded","finished_at":"2026-10-10T15:00:00+09:00","last_message":"running thinking"}),
            ],
        );
        assert!(bare["jobs"][0]["lastResultAt"].is_null());
        assert_eq!(bare["jobs"][0]["currentStage"], "未記録");
    }

    #[test]
    fn quoted_multiline_secrets_and_basic_auth_are_removed_from_notes_and_history() {
        let names = Names::parse("");
        let note = "password=\"artificial alpha beta\"\nsecret='artificial gamma\ndelta'\nAuthorization: Basic dXNlcjpwYXNzd29yZA==\n正常な本文";
        let db = json!({"projects":{"demo":"D:/demo"},"items":[
            {"id":1,"project":"demo","status":"doing","note":note,
             "note_history":[{"note":note}]}]});
        let response = display_todos(&names, &db, &[]);
        let output = response.to_string();
        for forbidden in ["alpha", "beta", "gamma", "delta", "dXNlcjpwYXNzd29yZA=="] {
            assert!(!output.contains(forbidden), "secret residue in display");
        }
        assert!(response["items"][0]["note"]
            .as_str()
            .unwrap()
            .contains("正常な本文"));
        assert!(response["items"][0]["history"][0]["note"]
            .as_str()
            .unwrap()
            .contains("正常な本文"));
    }

    #[test]
    fn display_projects_allowlisted_records_and_merges_actual_old_contents() {
        let names = Names::parse("SecretClient\n");
        let db = json!({"projects":{"demo":"D:/CC/Work/demo","alias":"D:/CC/Work/SecretClient"},"items":[
            {"id":1,"project":"demo","title":"保存を確認","note":"現在のメモ password=artificial-password\n次の行","status":"doing","progress":60,"agent":"担当A","updated":"2026-10-10 09:15","jobs":["public-job"],"spec":"D:/CC/Tools/todo/specs/1-new.md",
                "history":[{"message":"着手した","timestamp":"2026-10-10T08:00:00+09:00","unexpected":"artificial-unlisted"}],
                "note_history":[{"note":"旧メモの具体的な本文\n再開条件を残す token=artificial-token","updated":"2026-10-10 08:30"}],
                "spec_history":[{"spec":"D:/CC/Tools/todo/specs/1-old.md","updated":"2026-10-10 08:45","body":"artificial-spec-body"}],"worker_receipt":{"prompt":"artificial-reservation"}},
            {"id":2,"project":"alias","title":"artificial-client-title","note":"artificial-client-note","status":"block","agent":"SecretClient担当","jobs":["SecretClient-job"],"spec":"D:/SecretClient/new.md",
                "history":["artificial-client-history"],"note_history":[{"note":"artificial-client-old-note"}],"spec_history":[{"spec":"D:/SecretClient/old.md"}],"questionUrl":"https://discord.com/channels/1/2"},
            {"id":3,"project":"demo","cwd":"D:/SecretClient-wt/agent","status":"doing","title":"artificial-path-title","note":"artificial-path-note"}
        ]});
        let jobs = vec![
            json!({"id":"public-job","name":"公開ジョブ token=artificial-job-token","status":"running","cwd":"D:/CC/Work/demo","created_at":"2026-10-10T09:00:00+09:00","launch_key":"artificial-reservation"}),
            json!({"id":"SecretClient-job","name":"orch-artificial-client-job","status":"completed","cwd":"D:/CC/Work/SecretClient","last_message":"artificial-client-decision"}),
            json!({"id":"public-decision","name":"orch-public","status":"completed","cwd":"D:/CC/Work/demo","last_message":"次の作業を再開する secret=artificial-decision-secret"}),
        ];
        let before = (db.clone(), jobs.clone());
        let todos = display_todos(&names, &db, &jobs);
        let item = &todos["items"][0];
        assert_eq!(item["updated_at"], "2026-10-10T09:15:00+09:00");
        assert_eq!(item["note"], "現在のメモ [非表示]\n次の行");
        assert_eq!(
            item["history"],
            json!([
                {"at":"2026-10-10T08:00:00+09:00","note":"着手した"},
                {"at":"2026-10-10T08:30:00+09:00","note":"旧メモの具体的な本文\n再開条件を残す [非表示]"},
                {"at":"2026-10-10T08:45:00+09:00","note":"以前の指示書: D:/CC/Tools/todo/specs/1-old.md"}
            ])
        );
        assert_eq!(item["spec"], "D:/CC/Tools/todo/specs/1-new.md");
        for item in &todos["items"].as_array().unwrap()[1..] {
            assert_eq!(item["project"], CLIENT);
            assert_eq!(item["note"], "");
            assert_eq!(item["history"], json!([]));
            assert!(item["spec"].is_null());
            assert!(item["questionUrl"].is_null());
        }
        let displayed_jobs = display_jobs(&names, &db, &jobs);
        assert_eq!(
            displayed_jobs["jobs"][0]["task_title"],
            "公開ジョブ [非表示]"
        );
        assert_eq!(displayed_jobs["jobs"][1]["summary"], "");
        let handoff = display_handoff(&names, &db, &jobs, true, 1791591300, 2, true);
        assert!(text(&handoff, "text").contains("保存を確認\n  現在のメモ [非表示]\n次の行"));
        assert_eq!(
            handoff["decisions"][1]["summary"],
            "次の作業を再開する [非表示]"
        );
        let response = json!({"todo":todos,"jobs":displayed_jobs,"handoff":handoff}).to_string();
        for forbidden in [
            "SecretClient",
            "artificial-password",
            "artificial-token",
            "artificial-job-token",
            "artificial-decision-secret",
            "artificial-unlisted",
            "artificial-spec-body",
            "artificial-reservation",
            "artificial-client",
            "artificial-path",
        ] {
            assert!(
                !response.contains(forbidden),
                "unexpected display content: {forbidden}"
            );
        }
        assert_eq!((db, jobs), before);
    }

    #[test]
    fn privacy_follows_linked_todos_registered_paths_and_client_worktrees() {
        let names = Names::parse("SecretClient");
        let db = json!({"projects":{"alias":"D:/SecretClient","SecretClient":"D:/neutral"},"items":[
            {"id":12,"project":"alias","jobs":["linked"],"status":"doing","note":"artificial-private"},
            {"id":13,"project":"public","jobs":["path-job"],"status":"doing","note":"artificial-associated"}
        ]});
        for job in [
            json!({"id":"linked","cwd":"D:/neutral-other"}),
            json!({"id":"todo-job","todo_id":12,"cwd":"D:/neutral-other"}),
            json!({"id":"path-job","cwd":"D:/neutral-wt/agent"}),
            json!({"id":"other-job","thread_id":"linked","cwd":"D:/common"}),
            json!({"id":"session-job","resume_session":"linked","cwd":"D:/common"}),
        ] {
            assert!(private_job(&names, &db, &job));
            assert_eq!(display_jobs(&names, &db, &[job])["jobs"][0]["summary"], "");
        }
        let jobs = vec![json!({"id":"path-job","cwd":"D:/neutral-wt/agent"})];
        assert_eq!(display_todos(&names, &db, &jobs)["items"][1]["note"], "");
    }

    // Run the actual request/render functions with artificial responses and a minimal DOM.
    // No browser, network, PC records, JS source-string assertions or Cargo subprocess.
    const SHELL_RESPONSE_TEST: &str = r#"
const assert = require('node:assert/strict');
const input = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
class Element {
  constructor(tag='div', cls='', text='') { this.tagName=tag.toUpperCase(); this.className=cls; this.ownText=text; this.childNodes=[]; this.dataset={}; this.value=''; this.open=false; }
  get textContent() { return this.ownText+this.childNodes.map(node=>node.textContent).join(''); }
  set textContent(text) { this.ownText=String(text); this.childNodes=[]; }
  append(...nodes) { this.childNodes.push(...nodes); }
  replaceChildren(...nodes) { this.ownText=''; this.childNodes=nodes; }
  get firstChild() { return this.childNodes[0]; }
  contains(node) { return node!=null && (this===node || this.childNodes.some(child=>child.contains(node))); }
  querySelectorAll(selector) { const result=[]; for(const child of this.childNodes){if(child.tagName==='DETAILS' && child.dataset.record && (!selector.includes('[open]') || child.open))result.push(child);result.push(...child.querySelectorAll(selector));}return result; }
}
const document={activeElement:null};
const window={getSelection:()=>null};
const make=(tag,cls,text)=>new Element(tag,cls,text);
const todoList=make('div'),jobsHost=make('div'),handoffHost=make('div');
const todoSearch=make('input'),todoProject=make('select'),todoStatus=make('select'),jobsSearch=make('input');
todoStatus.value='open';
let sharedData={},sharedSignature={},pendingShared={},sharedComposing=false;
const musicBusy=new Set(),sources=new Map(),calls=[];
const receive=(key,patch)=>sources.set(key,{...(sources.get(key)||{}),...patch});
const shared={open:true},operationsView={classList:{contains:()=>true}};
const refreshAgents=()=>{throw new Error('unredacted fallback called');};
let nextResponse=null;
const fetch=async path=>{calls.push(path);const key=path.split('/').pop();return {ok:true,status:200,json:async()=>nextResponse===null?input.responses[key]:nextResponse};};
function production(start,end){const at=input.shell.indexOf(start);const stop=input.shell.indexOf(end,at+start.length);assert(at>=0&&stop>at);return input.shell.slice(at,stop);}
const functions=production('    const array=value=>','    const waiting=')+
  production('    function sharedResponse(', '    function retrySource(')+
  production('    const short=value=>','    if(operationsView){');
const run=new Function('assert','document','window','make','todoList','jobsHost','handoffHost','todoSearch','todoProject','todoStatus','jobsSearch','musicBusy','sources','receive','shared','operationsView','refreshAgents','fetch','input',
  'let sharedData={},sharedSignature={},pendingShared={},sharedComposing=false;'+functions+`
return (async()=>{
  loadShared(); await new Promise(resolve=>setImmediate(resolve));
  for(const key of ['todo','jobs','handoff']) {
    assert.equal(sources.get(key).error,'');
    assert.deepEqual(sources.get(key).data,input.responses[key].data);
    assert(sources.get(key).receivedAt>0);
  }
  assert(todoList.textContent.includes('保存を確認'));
  assert(todoList.textContent.includes('現在のメモの本文'));
  assert(todoList.textContent.includes('旧メモの具体的な本文'));
  assert(todoList.textContent.includes('以前の指示書: D:/CC/Tools/todo/specs/1-old.md'));
  assert(todoList.textContent.includes('更新 '+time('2026-10-10T09:15:00+09:00')));
  assert(todoList.textContent.includes(time('2026-10-10T08:30:00+09:00')+' · 旧メモの具体的な本文'));
  assert(jobsHost.textContent.includes('接続を確認する · 実行中'));
  for(const host of [todoList,jobsHost]){
    const card=host.childNodes[0],facts=card.childNodes.find(child=>child.tagName==='DL');
    assert(facts,'facts must exist outside collapsed details');
    for(const label of ['作業要約','最終成果の時刻','現在の工程','停止理由','次の行動','依存先'])assert(facts.textContent.includes(label));
    assert(facts.textContent.includes('未記録'));
    assert(card.childNodes.find(child=>child.tagName==='DETAILS').open===false);
  }
  jobsSearch.value='画面受入';renderJobs();assert(jobsHost.textContent.includes('接続を確認する · 実行中'));
  jobsSearch.value='該当なし';renderJobs();assert(jobsHost.textContent.includes('検索に一致するジョブはありません'));
  jobsSearch.value='';renderJobs();
  assert(handoffHost.textContent.includes('対応中・判断待ちのToDo'));
  assert(handoffHost.textContent.includes('次の作業を再開する'));
})();`);
// Error cases run separately below with the same real functions and retained DOM.
const checks=functions+`
return (async()=>{
  for(const key of ['todo','jobs','handoff']) {
    await request(key,'/api/agent/display/'+key,data=>applyShared(key,data));
    const good=sources.get(key),host=key==='todo'?todoList:key==='jobs'?jobsHost:handoffHost,rendered=host.textContent;
    for(const bad of [{ok:false,version:1,data:input.responses[key].data},{ok:true,version:2,data:input.responses[key].data},{ok:true,version:1,data:{}},{ok:true,version:1,data:[]},input.responses[key].data,{ok:true,version:1,data:{items:[null],jobs:[null],decisions:[null],text:'bad'}},{ok:true,version:1,data:{items:[{}],jobs:[{}],decisions:[{}],text:'bad'}}]) {
      setResponse(bad);
      await request(key,'/api/agent/display/'+key,data=>applyShared(key,data));
      assert(sources.get(key).error);
      assert.equal(sources.get(key).data,good.data);
      assert.equal(sources.get(key).receivedAt,good.receivedAt);
      assert.equal(host.textContent,rendered);
    }
    setResponse(null);
    await request(key,'/api/agent/display/'+key,data=>applyShared(key,data));
    assert.equal(sources.get(key).error,'');
  }
  const raw={ok:false,departments:[]};setResponse(raw);
  await request('operations','/api/operations',data=>assert.equal(data,raw));
  assert.equal(sources.get('operations').error,'');
})();`;
(async()=>{
  await run(assert,document,window,make,todoList,jobsHost,handoffHost,todoSearch,todoProject,todoStatus,jobsSearch,musicBusy,sources,receive,shared,operationsView,refreshAgents,fetch,input);
  assert.deepEqual(calls.slice(0,3),['/api/agent/display/todo','/api/agent/display/jobs','/api/agent/display/handoff']);
  const verify=new Function('assert','document','window','make','todoList','jobsHost','handoffHost','todoSearch','todoProject','todoStatus','jobsSearch','musicBusy','sources','receive','shared','operationsView','refreshAgents','fetch','input','setResponse','sharedData','sharedSignature','pendingShared','sharedComposing',checks);
  await verify(assert,document,window,make,todoList,jobsHost,handoffHost,todoSearch,todoProject,todoStatus,jobsSearch,musicBusy,sources,receive,shared,operationsView,refreshAgents,fetch,input,value=>nextResponse=value,{}, {}, {}, false);
  process.stdout.write('shared display response/render regression passed\n');
})().catch(error=>{console.error(error);process.exitCode=1;});
"#;

    #[test]
    fn shell_renders_three_shared_envelopes_and_retains_last_success_on_invalid_response() {
        use std::{
            io::Write,
            process::{Command, Stdio},
        };
        let names = Names::parse("");
        let db = json!({"projects":{"demo":"D:/CC/Work/demo"},"items":[{"id":1,"project":"demo","title":"保存を確認","note":"現在のメモの本文","status":"doing","updated":"2026-10-10 09:15",
            "note_history":[{"note":"旧メモの具体的な本文","updated":"2026-10-10 08:30"}],
            "spec_history":[{"spec":"D:/CC/Tools/todo/specs/1-old.md","updated":"2026-10-10 08:45"}]}]});
        let jobs = vec![
            json!({"id":"public-job","name":"接続を確認する","status":"running","cwd":"D:/CC/Work/demo","work_status":{"stage":"画面受入"}}),
            json!({"id":"public-decision","name":"orch-public","status":"succeeded","cwd":"D:/CC/Work/demo","last_message":"次の作業を再開する"}),
        ];
        let responses = json!({"todo":{"ok":true,"version":1,"data":display_todos(&names,&db,&jobs)},
            "jobs":{"ok":true,"version":1,"data":display_jobs(&names,&db,&jobs)},
            "handoff":{"ok":true,"version":1,"data":display_handoff(&names,&db,&jobs,true,1791591300,0,false)}});
        let mut command = Command::new("node");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let mut child = command
            .args(["-e", SHELL_RESPONSE_TEST])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .expect("Node is required for the shell response regression");
        child
            .stdin
            .take()
            .unwrap()
            .write_all(
                json!({"shell":SHELL,"responses":responses})
                    .to_string()
                    .as_bytes(),
            )
            .unwrap();
        let output = child.wait_with_output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        assert_eq!(
            String::from_utf8_lossy(&output.stdout),
            "shared display response/render regression passed\n"
        );
    }

    #[test]
    fn preserves_music_form_and_media_and_decorates_only_once() {
        let html = "<!doctype html><BoDy><form action='/music/drafts' method='post'><input name='csrf' value='test-only'></form><audio src='/music/example/audio'></audio></BoDy>";
        let rendered = decorate_html(html);
        assert!(rendered.contains("<form action='/music/drafts' method='post'><input name='csrf' value='test-only'></form>"));
        assert!(rendered.contains("<audio src='/music/example/audio'></audio>"));
        assert!(rendered.ends_with("</BoDy>"));
        assert_eq!(rendered.matches(MARKER).count(), 1);
        assert_eq!(decorate_html(&rendered), rendered);
    }

    #[test]
    fn fragment_gets_shell_without_changing_existing_message() {
        let rendered = decorate_html("<h1>音楽制作</h1><p>接続を確認してください。</p>");
        assert!(rendered.starts_with("<h1>音楽制作</h1><p>接続を確認してください。</p>"));
        assert_eq!(rendered.matches(MARKER).count(), 1);
    }
}
