//! Work accounting from the resident's privacy-restricted processing evidence.
//! Never scans transcripts, credentials, repositories, or original protection records.
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::collections::{BTreeMap, BTreeSet};
use std::io::{Read, Write};
use std::path::Path;

#[derive(Default, Serialize, Deserialize)]
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

#[derive(Default, Serialize, Deserialize)]
#[serde(default)]
struct Day {
    first_at: i64,
    last_at: i64,
    duration_observed: bool,
    projects: BTreeMap<String, Project>,
}

#[derive(Default, Serialize, Deserialize)]
#[serde(default)]
struct Project {
    name: String,
    jobs: BTreeSet<String>,
    intervals: Vec<(i64, i64)>,
}

struct Sample {
    at: i64,
    until: i64,
    complete: bool,
    jobs: BTreeMap<String, String>,
}

impl Ledger {
    pub(super) fn load(path: &Path) -> Self {
        let loaded = (|| {
            let meta = std::fs::symlink_metadata(path)?;
            if !meta.is_file() || meta.len() > 8 * 1024 * 1024 {
                return Err(std::io::Error::other("invalid observation cache"));
            }
            let file = std::fs::File::open(path)?;
            let ledger: Self = serde_json::from_reader(file.take(8 * 1024 * 1024))?;
            if ledger.version != 1 {
                return Err(std::io::Error::other("unknown observation version"));
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
        if self.version != 1 {
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
                    let job = super::hash(job);
                    if jobs.contains_key(&job) {
                        continue;
                    }
                    jobs.insert(job.clone(), key.clone());
                    let project = day.projects.entry(key.clone()).or_default();
                    project.name = name.to_owned();
                    project.jobs.insert(job);
                }
            }
        }
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
                    day.duration_observed |= previous.complete && proof["state"] == "complete";
                    for (job, key) in &jobs {
                        if previous.jobs.get(job) != Some(key) {
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
        self.version = 1;
        self.previous = Some(Sample {
            at,
            until: until.unwrap(),
            complete: proof["state"] == "complete",
            jobs,
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
        let active: i64 = all_intervals.iter().map(|(s, e)| e - s).sum();
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
            json!({"date":date,"jobs":day.map(|_|jobs.len()),"active_ms":if day.is_some_and(|d|d.duration_observed)||!intervals.is_empty(){Some(intervals.iter().map(|(s,e)|e-s).sum::<i64>())}else{None}})
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
            "warnings":warnings,"totals":{"active_ms":duration_known.then_some(active),"jobs":available.then_some(jobs.len()),"merged_prs":null,"tokens":null},
            "projects":projects,"timeline":if available{timeline}else{Vec::new()},"history":history})
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn board(at: i64, rows: Value) -> Value {
        json!({"metadataOk":true,"workforce":{"processing":{"state":"complete","observedAt":at,"validUntil":at+30_000}},"departments":[{"products":[{"id":"fixed-product","name":"Product","workers":rows}]}]})
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
}
