//! Work accounting from the resident's privacy-restricted processing evidence.
//! Never scans transcripts, credentials, repositories, or original protection records.
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};
use std::io::{Read, Write};
use std::path::Path;

#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub(super) struct Ledger {
    version: u32,
    days: BTreeMap<String, Day>,
    updated_at: i64,
    #[serde(skip)]
    previous: Option<Sample>,
    #[serde(skip)]
    available: bool,
    #[serde(skip)]
    storage_error: bool,
}

#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
struct Day {
    first_at: i64,
    last_at: i64,
    duration_observed: bool,
    jobs_observed: bool,
    projects: BTreeMap<String, Project>,
}

#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
struct Project {
    name: String,
    jobs: BTreeSet<String>,
    intervals: Vec<(i64, i64)>,
}

#[derive(Clone)]
struct Sample {
    at: i64,
    until: i64,
    complete: bool,
    jobs: BTreeMap<String, String>,
    deadlines: BTreeMap<String, i64>,
}

impl Ledger {
    pub(super) fn revision(&self) -> i64 {
        self.updated_at
    }
    pub(super) fn storage_failed(&self) -> bool {
        self.storage_error
    }
    pub(super) fn record_storage_failure(&mut self, failed: bool) {
        self.storage_error = failed;
    }
    pub(super) fn load(path: &Path) -> Self {
        let loaded = (|| {
            let meta = std::fs::symlink_metadata(path)?;
            if !meta.is_file() || meta.len() > 8 * 1024 * 1024 {
                return Err(std::io::Error::other("invalid observation cache"));
            }
            let file = std::fs::File::open(path)?;
            let ledger: Self = serde_json::from_reader(file.take(8 * 1024 * 1024))?;
            if ledger.version != 2 {
                // A derived cache with old session keys must not double count
                // the same executions. Reading does not rewrite the old file.
                return Ok(Self::default());
            }
            Ok(ledger)
        })();
        match loaded {
            Ok(value) => value,
            Err(error) => Self {
                storage_error: error.kind() != std::io::ErrorKind::NotFound,
                ..Self::default()
            },
        }
    }

    pub(super) fn save(&mut self, path: &Path) {
        if self.version != 2 {
            return;
        }
        let result = (|| {
            let mut file = tempfile::NamedTempFile::new_in(
                path.parent()
                    .ok_or_else(|| std::io::Error::other("missing cache parent"))?,
            )?;
            serde_json::to_writer(&mut file, self)?;
            file.flush()?;
            file.as_file().sync_all()?;
            file.persist(path).map_err(|error| error.error)?;
            Ok::<_, std::io::Error>(())
        })();
        self.storage_error = result.is_err();
    }

    pub(super) fn observe(&mut self, board: &Value, now: i64) {
        let proof = &board["workforce"]["processing"];
        let at = proof["observedAt"].as_i64();
        let until = proof["validUntil"].as_i64();
        let valid = (board["metadataOk"] == true || board["sourceOk"] == true)
            && matches!(proof["state"].as_str(), Some("complete" | "partial"))
            && at.is_some_and(|at| at > 0 && at <= now && now - at <= 30_000)
            && until.is_some_and(|until| until >= now);
        if !valid {
            self.available = false;
            self.previous = None;
            return;
        }
        let at = at.unwrap();
        // A reader may see the same resident generation several times. Count it once.
        if at <= self.updated_at {
            self.available = at == self.updated_at;
            return;
        }
        let today = super::day(at);
        let mut jobs = BTreeMap::new();
        let mut deadlines = BTreeMap::new();
        let products = board["departments"]
            .as_array()
            .into_iter()
            .flatten()
            .flat_map(|department| department["products"].as_array().into_iter().flatten())
            .chain(board["unassignedProducts"].as_array().into_iter().flatten());
        let day = self.days.entry(today.clone()).or_default();
        if day.first_at == 0 {
            day.first_at = at;
        }
        day.last_at = at;
        for product in products {
            let Some(name) = product["name"]
                .as_str()
                .filter(|name| !name.is_empty() && name.len() <= 512)
            else {
                continue;
            };
            let key = product["id"].as_str().unwrap_or(name).to_owned();
            for row in product["workers"].as_array().into_iter().flatten() {
                if row["workState"] != "processing" {
                    continue;
                }
                for job in row["references"]["sessions"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(Value::as_str)
                    .filter(|job| !job.is_empty() && job.len() <= 512)
                {
                    let deadline = if let Some(sessions) = proof["sessions"].as_array() {
                        sessions
                            .iter()
                            .find(|session| session["key"] == job)
                            .and_then(|session| session["validUntil"].as_i64())
                    } else {
                        until
                    };
                    let Some(deadline) = deadline.filter(|deadline| *deadline >= now) else {
                        continue;
                    };
                    let job = super::hash(job);
                    if jobs.contains_key(&job) {
                        continue;
                    }
                    jobs.insert(job.clone(), key.clone());
                    deadlines.insert(job.clone(), deadline);
                    let project = day.projects.entry(key.clone()).or_default();
                    project.name = name.to_owned();
                    project.jobs.insert(job);
                }
            }
        }
        let complete = proof["state"] == "complete"
            && proof["verifiedCount"].as_u64() == Some(jobs.len() as u64);
        self.days.get_mut(&today).unwrap().jobs_observed |= complete || !jobs.is_empty();
        if let Some(previous) = &self.previous {
            // Only adjacent, fresh observations certify a sampled interval. Never
            // bridge a stop, failed acquisition, restart, or a different product.
            if previous.at < at && at - previous.at <= 30_000 && previous.until >= at {
                let names: BTreeMap<_, _> = self
                    .days
                    .get(&today)
                    .into_iter()
                    .flat_map(|day| &day.projects)
                    .map(|(key, project)| (key.clone(), project.name.clone()))
                    .collect();
                for (date, start, end) in super::split_interval(previous.at, at) {
                    let day = self.days.entry(date).or_default();
                    if day.first_at == 0 {
                        day.first_at = start;
                    }
                    day.last_at = end;
                    day.duration_observed |= previous.complete && complete;
                    day.jobs_observed |= complete || !jobs.is_empty();
                    for (job, key) in &jobs {
                        if previous.jobs.get(job) != Some(key)
                            || previous.deadlines.get(job).is_none_or(|until| *until < at)
                        {
                            continue;
                        }
                        let Some(name) = names.get(key).cloned() else {
                            continue;
                        };
                        let day = self.days.get_mut(&super::day(start)).unwrap();
                        let project = day.projects.entry(key.clone()).or_default();
                        project.name = name;
                        project.jobs.insert(job.clone());
                        super::merge_interval(&mut project.intervals, start, end);
                    }
                }
            }
        }
        let earliest = super::day(super::midnight(now) - 6 * super::DAY);
        self.days
            .retain(|date, _| date >= &earliest && date <= &today);
        self.updated_at = at;
        self.available = true;
        self.version = 2;
        self.previous = Some(Sample {
            at,
            until: until.unwrap(),
            complete,
            jobs,
            deadlines,
        });
    }

    pub(super) fn snapshot(&self, date: &str, now: i64) -> Value {
        let day = self.days.get(date);
        let available = day.is_some();
        let mut projects = Vec::new();
        let mut all_intervals = Vec::new();
        let mut jobs = BTreeSet::new();
        for (key, project) in day.into_iter().flat_map(|d| &d.projects) {
            let active: i64 = project.intervals.iter().map(|(s, e)| e - s).sum();
            for &(start, end) in &project.intervals {
                super::merge_interval(&mut all_intervals, start, end);
            }
            jobs.extend(project.jobs.iter().cloned());
            projects.push(json!({"name":project.name,"path":key,"jobs":project.jobs.len(),
                "active_ms":if project.intervals.is_empty(){None}else{Some(active)},
                "run_ms":null,"tokens":null,"commits":null,"merged_prs":null,"todos_done":null,"tool_calls":null,
                "summary":[{"text":format!("実作業を観測したジョブ {}件",project.jobs.len())}],"results":[]}));
        }
        let duration_known = day.is_some_and(|d| d.duration_observed) || !all_intervals.is_empty();
        let active: i64 = projects
            .iter()
            .filter_map(|p| p["active_ms"].as_i64())
            .sum();
        let base = super::date_ms(date).unwrap_or(0);
        let timeline: Vec<_> = (0..96)
            .map(|slot| {
                let start = base + slot * 15 * 60_000;
                let end = start + 15 * 60_000;
                let entries: Vec<_> = day
                    .into_iter()
                    .flat_map(|d| d.projects.values())
                    .filter_map(|p| {
                        let ms: i64 = p
                            .intervals
                            .iter()
                            .map(|(s, e)| (end.min(*e) - start.max(*s)).max(0))
                            .sum();
                        (ms > 0).then(|| json!({"name":p.name,"active_ms":ms}))
                    })
                    .collect();
                json!({"time":format!("{:02}:{:02}",slot/4,slot%4*15),"projects":entries})
            })
            .collect();
        let history: Vec<_> = (0..7).rev().map(|i|{
            let date=super::day(base-i*super::DAY);let day=self.days.get(&date);let mut intervals=Vec::new();let mut jobs=BTreeSet::new();
            for p in day.into_iter().flat_map(|d|d.projects.values()) {jobs.extend(&p.jobs);for &(s,e) in &p.intervals{super::merge_interval(&mut intervals,s,e)}}
            let active=day.into_iter().flat_map(|d|d.projects.values()).flat_map(|p|&p.intervals).map(|(s,e)|e-s).sum::<i64>();
            json!({"date":date,"jobs":day.filter(|d|d.jobs_observed).map(|_|jobs.len()),"active_ms":if day.is_some_and(|d|d.duration_observed)||!intervals.is_empty(){Some(active)}else{None}})
        }).collect();
        let mut warnings = Vec::new();
        if !available {
            warnings.push("この日の仕事量は未取得です。");
        }
        if !self.available || now - self.updated_at > 45_000 {
            warnings.push("現在の観測を取得できていません。最終確認時点の値を保持しています。");
        }
        if self.storage_error {
            warnings.push("観測履歴の保存を確認できていません。");
        }
        json!({"date":date,"updated_at":(self.updated_at>0).then(||super::jst_time(self.updated_at)),"collecting":!available,
            "measurement":{"state":if available{"partial"}else{"unavailable"},"kind":"processing_observations","first_at":day.map(|d|super::jst_time(d.first_at)),"last_at":day.map(|d|super::jst_time(d.last_at)),"source":"operations.workforce.processing"},
            "warnings":warnings,"totals":{"active_ms":duration_known.then_some(active),"jobs":day.filter(|d|d.jobs_observed).map(|_|jobs.len()),"merged_prs":null,"tokens":null},
            "projects":projects,"timeline":if available{timeline}else{Vec::new()},"history":history})
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn board(at: i64, rows: Value) -> Value {
        let count = rows
            .as_array()
            .into_iter()
            .flatten()
            .filter(|r| r["workState"] == "processing")
            .flat_map(|r| r["references"]["sessions"].as_array().into_iter().flatten())
            .filter_map(Value::as_str)
            .collect::<BTreeSet<_>>()
            .len();
        json!({"metadataOk":true,"workforce":{"processing":{"state":"complete","observedAt":at,"validUntil":at+30_000,"verifiedCount":count}},"departments":[{"products":[{"id":"fixed-product","name":"Product","workers":rows}]}]})
    }
    fn rows() -> Value {
        json!([{"primaryJob":"old","references":{"sessions":["one"]},"workState":"processing","tokens":999999,"note":"private fixture"},{"primaryJob":"old","references":{"sessions":["one"]},"workState":"processing"},{"primaryJob":"waiting","workState":"resource_wait"}])
    }
    #[test]
    fn counts_unique_processing_and_only_adjacent_observed_intervals() {
        let mut ledger = Ledger::default();
        let at = 1_792_500_000_000;
        ledger.observe(&board(at, rows()), at);
        assert!(ledger.snapshot(&super::super::day(at), at)["totals"]["active_ms"].is_null());
        ledger.observe(&board(at + 15_000, rows()), at + 15_000);
        let snapshot = ledger.snapshot(&super::super::day(at), at + 15_000);
        assert_eq!(snapshot["totals"]["jobs"], 1);
        assert_eq!(snapshot["totals"]["active_ms"], 15_000);
        assert!(snapshot["totals"]["tokens"].is_null());
        assert!(!snapshot.to_string().contains("private fixture"));
        assert!(!snapshot.to_string().contains("999999"));
        ledger.observe(&board(at + 15_000, rows()), at + 15_000);
        assert_eq!(
            ledger.snapshot(&super::super::day(at), at + 15_000)["totals"]["active_ms"],
            15_000
        );
        ledger.observe(&Value::Null, at + 20_000);
        ledger.observe(&board(at + 25_000, rows()), at + 25_000);
        assert_eq!(
            ledger.snapshot(&super::super::day(at), at + 25_000)["totals"]["active_ms"],
            15_000
        );
        ledger.observe(&board(at + 70_000, rows()), at + 70_000);
        assert_eq!(
            ledger.snapshot(&super::super::day(at), at + 70_000)["totals"]["active_ms"],
            15_000
        );
    }

    #[test]
    fn real_operations_native_receipt_reaches_row_progress_and_observed_workload() {
        use crate::operations::Operations;
        use localai_web_rs::native_activity::{Activity, Close, Register};
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path();
        std::fs::create_dir_all(root.join("todo")).unwrap();
        std::fs::create_dir_all(root.join("codex-jobs/jobs")).unwrap();
        std::fs::write(
            root.join("todo/todo.json"),
            json!({"projects":{"tools":"D:/fixture"},"items":[
                {"id":1,"project":"tools","status":"doing"},
                {"id":2,"project":"tools","status":"wait","wait_on":[1]}
            ]})
            .to_string(),
        )
        .unwrap();
        let board = Operations::new(root.into(), root.join("skills"));
        board.refresh();
        let id = "00000000-0000-7000-8000-000000000031";
        let registration = board
            .register_native(Register {
                session_id: id.into(),
                todo_id: 1,
                role: "work".into(),
                business_project: None,
            })
            .unwrap();
        let now = || {
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_millis() as u64
        };
        let actual = now();
        let generation = registration["generation"].as_u64().unwrap();
        board
            .record_native(Activity {
                session_id: id.into(),
                generation,
                actual_at_ms: actual,
                event_id: "actual-tool-result".into(),
                tool: "exec_command".into(),
                kind: "test".into(),
                executed: true,
                exit_code: Some(0),
            })
            .unwrap();
        let snapshot = board.snapshot();
        let rows: Vec<_> = snapshot["departments"]
            .as_array()
            .unwrap()
            .iter()
            .flat_map(|d| d["products"].as_array().unwrap())
            .flat_map(|p| p["workers"].as_array().unwrap())
            .collect();
        let worker = rows.iter().find(|row| row["id"] == "todo-1").unwrap();
        assert!(
            worker.get("processingJobKey").is_some(),
            "exercise the production strict-key shape, including null"
        );
        assert_eq!(worker["workState"], "processing");
        assert_eq!(worker["status"], "working");
        assert!(worker["primaryJob"].is_null());
        assert_eq!(worker["stopReason"], "");
        assert_eq!(
            rows.iter().find(|row| row["id"] == "todo-2").unwrap()["workState"],
            "dependency_wait"
        );
        let mut ledger = Ledger::default();
        let at = now() as i64;
        ledger.observe(&snapshot, at);
        assert_eq!(
            ledger.snapshot(&super::super::day(at), at)["totals"]["jobs"],
            1
        );
        board
            .close_native(Close {
                session_id: id.into(),
                generation,
                ended_at_ms: now(),
                outcome: "success".into(),
            })
            .unwrap();
        let closed = board.snapshot();
        assert!(closed["workforce"]["processing"]["sessions"]
            .as_array()
            .unwrap()
            .is_empty());
    }
    #[test]
    fn empty_observations_differ_from_unavailable_and_restart_does_not_bridge() {
        let mut ledger = Ledger::default();
        let at = 1_792_500_000_000;
        let date = super::super::day(at);
        assert!(ledger.snapshot(&date, at)["totals"]["jobs"].is_null());
        ledger.observe(&board(at, json!([])), at);
        assert_eq!(ledger.snapshot(&date, at)["totals"]["jobs"], 0);
        ledger.observe(&board(at + 15_000, json!([])), at + 15_000);
        assert_eq!(
            ledger.snapshot(&date, at + 15_000)["totals"]["active_ms"],
            0
        );
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("observations.json");
        ledger.save(&path);
        let mut next = Ledger::load(&path);
        next.observe(&board(at + 25_000, rows()), at + 25_000);
        assert_eq!(next.snapshot(&date, at + 25_000)["totals"]["active_ms"], 0);
        assert!(next.snapshot("2000-01-01", at)["totals"]["jobs"].is_null());
    }

    #[test]
    fn missing_job_links_remain_unknown_and_parallel_products_split_at_midnight() {
        let at = super::super::date_ms("2026-10-21").unwrap() - 15_000;
        let mut ledger = Ledger::default();
        let mut missing = board(at, json!([]));
        missing["workforce"]["processing"]["verifiedCount"] = json!(1);
        ledger.observe(&missing, at);
        let unknown = ledger.snapshot("2026-10-20", at);
        assert!(unknown["totals"]["jobs"].is_null());
        assert!(unknown["totals"]["active_ms"].is_null());

        let mut ledger = Ledger::default();
        let mut input = board(at, rows());
        input["departments"][0]["products"]
            .as_array_mut()
            .unwrap()
            .push(json!({
            "id":"other-product","name":"Other","workers":[{
            "workState":"processing","references":{"sessions":["second"]}}]}));
        input["workforce"]["processing"]["verifiedCount"] = json!(2);
        ledger.observe(&input, at);
        let next = at + 30_000;
        input["workforce"]["processing"]["observedAt"] = json!(next);
        input["workforce"]["processing"]["validUntil"] = json!(next + 30_000);
        ledger.observe(&input, next);
        for date in ["2026-10-20", "2026-10-21"] {
            let value = ledger.snapshot(date, next);
            assert_eq!(value["totals"]["jobs"], 2);
            assert_eq!(value["totals"]["active_ms"], 30_000);
        }
    }

    #[test]
    fn an_expired_job_does_not_bridge_a_gap_or_hide_another_product() {
        let at = 1_792_500_000_000;
        let mut input = board(at, rows());
        input["departments"][0]["products"]
            .as_array_mut()
            .unwrap()
            .push(json!({
            "id":"other-product","name":"Other","workers":[{
            "workState":"processing","references":{"sessions":["second"]}}]}));
        input["workforce"]["processing"]["verifiedCount"] = json!(2);
        input["workforce"]["processing"]["sessions"] = json!([
            {"key":"one","validUntil":at+1_000},{"key":"second","validUntil":at+30_000}]);
        let mut ledger = Ledger::default();
        ledger.observe(&input, at);
        let next = at + 15_000;
        input["workforce"]["processing"]["observedAt"] = json!(next);
        input["workforce"]["processing"]["validUntil"] = json!(next + 30_000);
        input["workforce"]["processing"]["sessions"] = json!([
            {"key":"one","validUntil":next+30_000},{"key":"second","validUntil":next+30_000}]);
        ledger.observe(&input, next);
        let value = ledger.snapshot(&super::super::day(at), next);
        let projects = value["projects"].as_array().unwrap();
        assert!(projects.iter().find(|p| p["name"] == "Product").unwrap()["active_ms"].is_null());
        assert_eq!(
            projects.iter().find(|p| p["name"] == "Other").unwrap()["active_ms"],
            15_000
        );
        assert_eq!(value["totals"]["active_ms"], 15_000);
    }
}
#[test]
fn obsolete_key_version_is_not_counted_twice_after_upgrade() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("observations.json");
    std::fs::write(&path, br#"{"version":1,"days":{},"updated_at":123}"#).unwrap();
    let ledger = Ledger::load(&path);
    assert_eq!(
        ledger.revision(),
        0,
        "the previous key representation must start a new derived ledger"
    );
    assert!(!ledger.storage_failed());
    assert!(
        String::from_utf8(std::fs::read(path).unwrap())
            .unwrap()
            .contains("123"),
        "read must not rewrite the old derived cache"
    );
}
