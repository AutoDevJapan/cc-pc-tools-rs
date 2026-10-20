//! Bounded, content-free resource observation. HTTP only reads the cached result.
use serde_json::{json, Value};
use std::{
    collections::{BTreeMap, BTreeSet, VecDeque},
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex, RwLock,
    },
};
#[path = "resource_sources.rs"]
mod sources;
pub use sources::job_metadata;

pub const MAX_PROCESSES: usize = 4096;
pub const HISTORY_SAMPLES: usize = 41;
pub const STALE_MS: u64 = 45_000;
const WINDOW_MS: u64 = 600_000;

#[derive(Clone, Debug)]
pub struct Process {
    pub pid: u32,
    pub parent_pid: u32,
    pub created: Option<u64>,
    pub name: String,
    pub cpu_seconds: Option<f64>,
    pub private_bytes: Option<u64>,
    pub working_set_bytes: Option<u64>,
}
#[derive(Clone, Debug)]
pub struct Sample {
    pub at_ms: u64,
    pub tick_ms: u64,
    pub logical_cpus: u32,
    pub system_times: [u64; 3],
    pub total_bytes: u64,
    pub available_bytes: u64,
    pub processes: Vec<Process>,
}
struct Frame {
    at_ms: u64,
    tick_ms: u64,
    system_times: [u64; 3],
    processes: BTreeMap<u32, Process>,
    summary: Value,
}
#[derive(Default)]
pub struct History {
    frames: VecDeque<Frame>,
}

fn name(raw: &str) -> String {
    let lower = raw.to_ascii_lowercase();
    let stem = lower.strip_suffix(".exe").unwrap_or(&lower);
    if matches!(
        stem,
        "cargo"
            | "cargo_gate"
            | "rustc"
            | "sccache"
            | "go"
            | "cl"
            | "link"
            | "lld"
            | "cmake"
            | "ninja"
            | "msbuild"
            | "codex"
            | "codex_jobs"
            | "claude"
            | "opencodex"
            | "localai-web-rs"
            | "chrome"
            | "node"
            | "bun"
            | "msmpeng"
            | "dwm"
            | "system"
            | "memory compression"
            | "svchost"
            | "powershell"
            | "pwsh"
            | "vmmem"
            | "vmmemwsl"
            | "docker"
            | "com.docker.backend"
            | "dotnet"
            | "csc"
            | "gcc"
            | "cc1"
            | "cc1plus"
    ) {
        lower
    } else {
        "未確認のプロセス".into()
    }
}
fn build(p: &Process) -> bool {
    matches!(
        p.name.trim_end_matches(".exe"),
        "cargo"
            | "cargo_gate"
            | "rustc"
            | "go"
            | "cl"
            | "link"
            | "lld"
            | "cmake"
            | "ninja"
            | "msbuild"
            | "gcc"
            | "cc1"
            | "cc1plus"
            | "csc"
    )
}
fn parent(p: &Process, ps: &BTreeMap<u32, Process>) -> Option<u32> {
    let candidate = ps.get(&p.parent_pid)?;
    (candidate.pid != p.pid && candidate.created? <= p.created?).then_some(candidate.pid)
}
fn chain(pid: u32, ps: &BTreeMap<u32, Process>) -> Vec<u32> {
    let mut out = Vec::new();
    let mut next = Some(pid);
    while let Some(pid) = next {
        if out.len() == 64 || out.contains(&pid) {
            break;
        }
        out.push(pid);
        next = ps.get(&pid).and_then(|p| parent(p, ps));
    }
    out
}
fn same<'a>(p: &Process, frame: &'a Frame) -> Option<&'a Process> {
    let old = frame.processes.get(&p.pid)?;
    (p.created.is_some() && p.created == old.created).then_some(old)
}
fn signed_delta(current: u64, old: u64) -> Option<i64> {
    i64::try_from(i128::from(current) - i128::from(old)).ok()
}
fn valid_jobs(jobs: &Value, now: u64) -> &[Value] {
    if jobs["truncated"] != true
        && jobs["observedAt"]
            .as_u64()
            .is_some_and(|at| at <= now && now - at <= STALE_MS)
    {
        jobs["jobs"].as_array().map(Vec::as_slice).unwrap_or(&[])
    } else {
        &[]
    }
}
fn identity(value: &Value, ps: &BTreeMap<u32, Process>) -> Option<u32> {
    let pid = u32::try_from(value["pid"].as_u64()?).ok()?;
    let birth = value["created"].as_str()?.parse::<u64>().ok()?;
    (birth > 0 && ps.get(&pid)?.created == Some(birth)).then_some(pid)
}
fn job_for<'a>(pid: u32, jobs: &'a [Value], ps: &BTreeMap<u32, Process>) -> Option<&'a Value> {
    for ancestor in chain(pid, ps) {
        let matching: Vec<_> = jobs
            .iter()
            .filter(|job| {
                job["roots"].as_array().is_some_and(|roots| {
                    roots
                        .iter()
                        .any(|root| identity(root, ps) == Some(ancestor))
                })
            })
            .collect();
        if matching.len() > 1 {
            return None;
        }
        if let Some(job) = matching.first() {
            return Some(job);
        }
    }
    None
}
impl History {
    pub fn observe(&mut self, mut sample: Sample, jobs: &Value) -> Value {
        if self
            .frames
            .back()
            .is_some_and(|old| sample.tick_ms <= old.tick_ms || sample.at_ms < old.at_ms)
        {
            self.frames.clear();
        }
        while self
            .frames
            .front()
            .is_some_and(|old| sample.tick_ms.saturating_sub(old.tick_ms) > WINDOW_MS)
        {
            self.frames.pop_front();
        }
        let truncated = sample.processes.len() > MAX_PROCESSES;
        let mut ps = BTreeMap::new();
        let mut duplicate = BTreeSet::new();
        for mut p in sample.processes.drain(..).take(MAX_PROCESSES) {
            if p.pid == 0 {
                continue;
            } // idle time is not CPU consumption.
            p.name = name(&p.name);
            p.cpu_seconds = p.cpu_seconds.filter(|n| n.is_finite() && *n >= 0.);
            if p.created.is_none_or(|at| at == 0) {
                p.created = None;
                p.cpu_seconds = None;
                p.private_bytes = None;
                p.working_set_bytes = None;
            }
            if ps.insert(p.pid, p.clone()).is_some() {
                duplicate.insert(p.pid);
            }
        }
        for pid in &duplicate {
            ps.remove(pid);
        }
        let previous = self.frames.back();
        let dt = previous
            .map(|old| sample.tick_ms - old.tick_ms)
            .filter(|dt| (250..=STALE_MS).contains(dt));
        let machine_cpu = previous
            .zip(dt)
            .filter(|_| (1..=64).contains(&sample.logical_cpus))
            .and_then(|(old, _)| {
                let d: Vec<_> = sample
                    .system_times
                    .iter()
                    .zip(old.system_times)
                    .map(|(n, o)| n.checked_sub(o))
                    .collect();
                let idle = d[0]?;
                let total = d[1]?.checked_add(d[2]?)?;
                (total > 0 && idle <= total).then(|| 100. * (total - idle) as f64 / total as f64)
            });
        let mut rows = BTreeMap::new();
        for p in ps.values() {
            let old = previous.and_then(|old| same(p, old));
            let cpu_delta = old
                .and_then(|old| p.cpu_seconds.zip(old.cpu_seconds))
                .filter(|(n, o)| n >= o)
                .map(|(n, o)| n - o)
                .filter(|_| dt.is_some());
            let cpu = cpu_delta
                .zip(dt)
                .filter(|_| sample.logical_cpus > 0)
                .map(|(d, ms)| 100. * d / (ms as f64 / 1000.) / f64::from(sample.logical_cpus))
                .filter(|n| n.is_finite() && (0. ..=100.).contains(n));
            let delta = old
                .and_then(|old| p.private_bytes.zip(old.private_bytes))
                .and_then(|(n, o)| signed_delta(n, o))
                .filter(|_| dt.is_some());
            let baseline = self.frames.iter().find_map(|frame| {
                same(p, frame).and_then(|old| old.private_bytes.map(|bytes| (frame.tick_ms, bytes)))
            });
            let growth = p
                .private_bytes
                .zip(baseline)
                .and_then(|(n, (_, o))| signed_delta(n, o));
            let layer = if p.name.starts_with("vmmem") {
                "vm"
            } else {
                "host-process"
            };
            rows.insert(p.pid, json!({"pid":p.pid,"created":p.created.map(|n|n.to_string()),
                "parentPid":parent(p,&ps),"name":p.name,"layer":layer,
                "cpuSeconds":p.cpu_seconds,"cpuDeltaSeconds":cpu_delta,"cpuPercent":cpu,
                "privateBytes":p.private_bytes,"workingSetBytes":p.working_set_bytes,
                "privateDeltaBytes":delta,"growthBytes":growth,
                "growthWindowMs":baseline.map(|(tick,_)|sample.tick_ms-tick),
                "metricsState":if p.cpu_seconds.is_none() || p.private_bytes.is_none(){"partial"}else{"ok"}}));
        }
        let current_jobs = valid_jobs(jobs, sample.at_ms);
        let mut builds = Vec::new();
        let mut build_roots = BTreeSet::new();
        let mut membership: BTreeMap<u32, BTreeSet<u32>> = BTreeMap::new();
        for pid in ps.keys().copied() {
            if let Some(root) = chain(pid, &ps)
                .into_iter()
                .rev()
                .find(|id| ps.get(id).is_some_and(build))
            {
                membership.entry(root).or_default().insert(pid);
            }
        }
        for (pid, members) in membership {
            let p = &ps[&pid];
            build_roots.insert(p.pid);
            let job = job_for(p.pid, current_jobs, &ps);
            let markers: Vec<_> = job
                .filter(|job| job["markerState"] == "ok")
                .and_then(|job| job["markers"].as_array())
                .into_iter()
                .flatten()
                .filter(|marker| identity(marker, &ps).is_some_and(|pid| members.contains(&pid)))
                .filter(|marker| {
                    marker["since"]
                        .as_u64()
                        .is_some_and(|at| at <= sample.at_ms)
                })
                .collect();
            let cpu_observed = members.iter().any(|pid| {
                rows[pid]["cpuDeltaSeconds"]
                    .as_f64()
                    .is_some_and(|d| d > 0.00001)
            });
            let state = if markers.iter().any(|m| m["kind"] == "cargo-active") {
                "executing"
            } else if markers.iter().any(|m| m["kind"] == "cargo") {
                "waiting"
            } else if cpu_observed {
                "processing"
            } else {
                "unverified"
            };
            let elapsed = p
                .created
                .and_then(|birth| birth.checked_sub(116_444_736_000_000_000))
                .map(|ticks| ticks / 10_000)
                .and_then(|at| sample.at_ms.checked_sub(at))
                .map(|ms| ms / 1000);
            builds.push(json!({"pid":p.pid,"created":p.created.map(|n|n.to_string()),"tool":p.name,
                "state":state,"elapsedSeconds":elapsed,"processCount":members.len(),
                "job":job.map(|job|json!({"key":job["key"],"todoId":job["todoId"],"product":job["product"]}))}));
        }
        let observed_builds = builds.len();
        let count = |state: &str| builds.iter().filter(|b| b["state"] == state).count();
        let build_counts = json!({"observed":observed_builds,"processing":count("processing"),"executing":count("executing"),
            "waiting":count("waiting"),"unverified":count("unverified"),"truncated":observed_builds>64});
        builds.truncate(64);
        let mut selected = build_roots;
        for key in [
            "cpuPercent",
            "privateBytes",
            "privateDeltaBytes",
            "growthBytes",
        ] {
            let mut order: Vec<_> = rows.values().filter(|p| p[key].is_number()).collect();
            order.sort_by(|a, b| {
                b[key]
                    .as_f64()
                    .partial_cmp(&a[key].as_f64())
                    .unwrap_or(std::cmp::Ordering::Equal)
                    .then_with(|| a["pid"].as_u64().cmp(&b["pid"].as_u64()))
            });
            selected.extend(
                order
                    .into_iter()
                    .take(20)
                    .filter_map(|p| p["pid"].as_u64().map(|n| n as u32)),
            );
        }
        // Keep denied rows visible when the table is small, without unbounded HTTP output.
        if rows.len() <= 128 {
            selected.extend(rows.keys().copied());
        }
        let unavailable = ps
            .values()
            .filter(|p| p.created.is_none() || p.cpu_seconds.is_none() || p.private_bytes.is_none())
            .count();
        let host = json!({"cpuPercent":machine_cpu,"logicalCpus":sample.logical_cpus,
            "totalPhysicalBytes":sample.total_bytes,"availablePhysicalBytes":sample.available_bytes,
            "usedPhysicalBytes":sample.total_bytes.checked_sub(sample.available_bytes),
            "memoryBasis":"physical; process privateBytes is commit, not physical attribution"});
        let summary = json!({"at":sample.at_ms,"cpuPercent":machine_cpu,"usedPhysicalBytes":host["usedPhysicalBytes"]});
        self.frames.push_back(Frame {
            at_ms: sample.at_ms,
            tick_ms: sample.tick_ms,
            system_times: sample.system_times,
            processes: ps,
            summary,
        });
        while self.frames.len() > HISTORY_SAMPLES {
            self.frames.pop_front();
        }
        json!({"schemaVersion":1,"state":if truncated||!duplicate.is_empty(){"partial"}else if dt.is_none(){"collecting"}else{"ready"},
            "sampledAt":sample.at_ms,"ageMs":0,"sampleIntervalMs":dt,"currentAvailable":true,"host":host,
            "processCount":rows.len(),"metricsUnavailable":unavailable,"processesTruncated":truncated,
            "processes":selected.into_iter().take(128).filter_map(|pid|rows.remove(&pid)).collect::<Vec<_>>(),
            "buildCounts":build_counts,"builds":builds,
            "history":self.frames.iter().map(|f|f.summary.clone()).collect::<Vec<_>>(),
            "historyLimit":HISTORY_SAMPLES,"growthState":if self.frames.len()<2{"collecting"}else{"observed_interval_only"},
            "jobMetadataState":if current_jobs.is_empty(){"unverified_or_empty"}else{"observed"},
            "notes":["working setは共有ページを含むため合算しません。私有メモリはcommit量です。",
                "VMとコンテナは別の層です。ホストへ足しません。増加は観測区間の差で、原因の断定ではありません。"]})
    }
}

pub fn expire(value: &mut Value, now: u64) {
    let age = value["sampledAt"]
        .as_u64()
        .and_then(|at| now.checked_sub(at));
    value["ageMs"] = json!(age);
    if value["sampledAt"].is_null() && value["state"] == "collecting" {
        value["currentAvailable"] = json!(false);
        return;
    }
    if value["state"] == "unavailable" || age.is_none_or(|age| age > STALE_MS) {
        if value["state"] != "unavailable" {
            value["state"] = json!("stale");
        }
        value["currentAvailable"] = json!(false);
        value["host"]["cpuPercent"] = Value::Null;
        for process in value["processes"].as_array_mut().into_iter().flatten() {
            for key in [
                "cpuPercent",
                "cpuDeltaSeconds",
                "privateDeltaBytes",
                "growthBytes",
            ] {
                process[key] = Value::Null;
            }
        }
        for build in value["builds"].as_array_mut().into_iter().flatten() {
            build["state"] = json!("unverified");
            build["job"] = Value::Null;
            build["elapsedSeconds"] = Value::Null;
        }
        value["jobMetadataState"] = json!("stale");
        value["buildCounts"] = json!({"observed":null,"processing":null,"executing":null,"waiting":null,"unverified":null,"truncated":null});
    }
}

pub struct Resources {
    enabled: bool,
    history: Mutex<History>,
    value: RwLock<Value>,
    docker: Arc<DockerCache>,
}
#[derive(Default)]
struct DockerCache {
    value: Mutex<Value>,
    busy: AtomicBool,
}
impl Default for Resources {
    fn default() -> Self {
        Self {
            enabled: false,
            history: Mutex::new(History::default()),
            value: RwLock::new(
                json!({"schemaVersion":1,"state":"collecting","sampledAt":null,"host":{},"processes":[],"builds":[],"history":[]}),
            ),
            docker: Arc::new(DockerCache::default()),
        }
    }
}
impl Resources {
    pub fn production() -> Self {
        Self {
            enabled: true,
            ..Self::default()
        }
    }
    pub fn enabled(&self) -> bool {
        self.enabled
    }
    pub fn refresh(&self, jobs: &Value) {
        if !self.enabled {
            return;
        }
        let result = sources::native_sample();
        let value = match result {
            Ok(sample) => self
                .history
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .observe(sample, jobs),
            Err(code) => {
                let mut old = self.snapshot();
                old["state"] = json!("unavailable");
                old["failureCode"] = json!(code);
                old
            }
        };
        *self.value.write().unwrap_or_else(|e| e.into_inner()) = value;
        self.refresh_docker();
    }
    fn refresh_docker(&self) {
        self.refresh_docker_with(sources::now_ms(), sources::docker_stats);
    }
    fn refresh_docker_with(
        &self,
        now: u64,
        acquire: impl FnOnce(&Value, u64) -> Value + Send + 'static,
    ) -> Option<std::thread::JoinHandle<()>> {
        let previous = self
            .docker
            .value
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone();
        if previous["attemptedAt"]
            .as_u64()
            .is_none_or(|at| at > now || now - at >= 60_000)
            && self
                .docker
                .busy
                .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
                .is_ok()
        {
            let cache = self.docker.clone();
            // One bounded finite worker, never a new resident or an HTTP-triggered scan.
            let worker = std::thread::Builder::new()
                .name("resource-docker".into())
                .spawn(move || {
                    let next = acquire(&previous, now);
                    *cache.value.lock().unwrap_or_else(|e| e.into_inner()) = next;
                    cache.busy.store(false, Ordering::Release);
                });
            match worker {
                Ok(worker) => return Some(worker),
                Err(_) => {
                    *self.docker.value.lock().unwrap_or_else(|e| e.into_inner()) = json!({"state":"unavailable","attemptedAt":now,
                    "sampledAt":null,"currentAvailable":false,"failureCode":"worker_unavailable","containers":[]});
                    self.docker.busy.store(false, Ordering::Release);
                }
            }
        }
        None
    }
    pub fn snapshot(&self) -> Value {
        self.snapshot_at(sources::now_ms())
    }
    pub fn snapshot_at(&self, now: u64) -> Value {
        let mut value = self.value.read().unwrap_or_else(|e| e.into_inner()).clone();
        value["docker"] = self
            .docker
            .value
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone();
        expire(&mut value, now);
        sources::expire_docker(&mut value["docker"], now);
        value
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn docker_collection_is_single_flight_and_never_blocks_snapshot_readers() {
        let resources = Resources::default();
        let (started, begin) = std::sync::mpsc::channel();
        let (release, blocked) = std::sync::mpsc::channel();
        let worker=resources.refresh_docker_with(100,move |_,now| {
            started.send(()).unwrap();blocked.recv().unwrap();
            json!({"state":"ready","sampledAt":now,"attemptedAt":now,"currentAvailable":true,"containers":[]})
        }).unwrap();
        begin.recv().unwrap();
        assert!(resources
            .refresh_docker_with(100, |_, _| panic!("duplicate collector"))
            .is_none());
        assert!(resources.snapshot_at(100)["sampledAt"].is_null());
        release.send(()).unwrap();
        worker.join().unwrap();
        assert_eq!(resources.snapshot_at(100)["docker"]["state"], "ready");
        assert!(resources
            .refresh_docker_with(101, |_, _| panic!("early refresh"))
            .is_none());
    }
    #[test]
    fn fixture_resources_do_not_collect_os_or_docker_data() {
        let resources = Resources::default();
        resources.refresh(&json!({}));
        assert!(!resources.enabled());
        assert!(resources.snapshot()["sampledAt"].is_null());
        assert!(Resources::production().enabled());
    }
}
