//! Real files and OS locks; no terminal history or registration is mocked away.
use super::*;
use std::{
    collections::BTreeMap,
    io,
    path::PathBuf,
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, AtomicUsize, Ordering},
    },
};

struct HistoryRuntime {
    epoch: AtomicU64,
    records: AtomicUsize,
    terminal_records: AtomicUsize,
    inventories: AtomicUsize,
    pauses: AtomicUsize,
    paths: Mutex<Vec<PathBuf>>,
}

impl Runtime for HistoryRuntime {
    fn env(&self) -> BTreeMap<String, String> {
        BTreeMap::new()
    }
    fn mono(&self) -> f64 {
        0.
    }
    fn epoch(&self) -> f64 {
        self.epoch.load(Ordering::Relaxed) as f64
    }
    fn proc_created(&self, pid: u32) -> Option<u64> {
        (pid == 444).then_some(44)
    }
    fn summary_mcp_servers(&self, _: &str, _: &Path) -> io::Result<Vec<String>> {
        self.inventories.fetch_add(1, Ordering::Relaxed);
        Ok(Vec::new())
    }
    fn pause_idle_agent(
        &self,
        _: u32,
        _: u64,
        _: Option<u64>,
        _: Option<&Path>,
    ) -> io::Result<Option<Box<dyn Paused>>> {
        self.pauses.fetch_add(1, Ordering::Relaxed);
        Ok(None) // A virtual identity never reaches the native process APIs.
    }
    fn before_load(&self, path: &Path) -> io::Result<()> {
        if path.file_name().is_some_and(|n| n == "job.json") {
            self.records.fetch_add(1, Ordering::Relaxed);
            if path
                .parent()
                .and_then(Path::file_name)
                .is_some_and(|n| n.to_string_lossy().starts_with("terminal-"))
            {
                self.terminal_records.fetch_add(1, Ordering::Relaxed);
            }
        }
        self.paths.lock().unwrap().push(path.into());
        Ok(())
    }
    fn call(&self, _: Call) -> io::Result<Reply> {
        Err(io::Error::other("unexpected external call"))
    }
    fn spawn(&self, _: &Spawn) -> io::Result<Box<dyn Running>> {
        Err(io::Error::other("unexpected spawn"))
    }
    fn spawn_supervisor(&self, _: &Path, _: &Path, _: &Path, _: &str) -> io::Result<bool> {
        Err(io::Error::other("unexpected supervisor"))
    }
}

struct HistoryFixture {
    _dir: tempfile::TempDir,
    ctx: Context,
    rt: Arc<HistoryRuntime>,
}

impl HistoryFixture {
    fn new(terminals: usize) -> Self {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path().join("tool");
        fs::create_dir_all(root.join("jobs")).unwrap();
        let rt = Arc::new(HistoryRuntime {
            epoch: AtomicU64::new(100),
            records: AtomicUsize::new(0),
            terminal_records: AtomicUsize::new(0),
            inventories: AtomicUsize::new(0),
            pauses: AtomicUsize::new(0),
            paths: Mutex::new(Vec::new()),
        });
        let ctx = Context {
            jobs: root.join("jobs"),
            home: dir.path().join("home"),
            codex_home: dir.path().join("codex"),
            gto: root.join("unused"),
            executable: root.join("unused"),
            root,
            rt: rt.clone(),
        };
        let f = Self { _dir: dir, ctx, rt };
        for n in 0..terminals {
            f.put(&format!("terminal-{n}"), "succeeded");
        }
        f.put("active", "queued");
        drop(f.ctx.queue_lock().unwrap());
        f.rt.epoch.store(131, Ordering::Relaxed);
        f.rt.records.store(0, Ordering::Relaxed);
        f.rt.terminal_records.store(0, Ordering::Relaxed);
        f.rt.paths.lock().unwrap().clear();
        f
    }
    fn put(&self, id: &str, status: &str) {
        fs::create_dir_all(self.ctx.job_dir(id)).unwrap();
        self.ctx.save(&json!({"id":id,"status":status,"name":id,"work_owner":{"repository":"fixture/repo","worktree":id}})).unwrap();
    }
}

#[test]
fn boundary_snapshot_uses_complete_candidates_while_another_scanner_holds_the_lock() {
    let f = HistoryFixture::new(16);
    let lock = FileLock::try_acquire(&f.ctx.jobs.join(".queue-scan.lock")).unwrap();
    let before = fs::read(f.ctx.jobs.join(".queue-index.json")).unwrap();
    let jobs = f
        .ctx
        .yield_snapshot()
        .expect("boundary read must not attempt reconciliation")
        .expect("completed candidates");
    assert_eq!(
        jobs.iter().map(|j| s(j, "id")).collect::<Vec<_>>(),
        ["active"]
    );
    assert_eq!(f.rt.terminal_records.load(Ordering::Relaxed), 0);
    assert_eq!(
        fs::read(f.ctx.jobs.join(".queue-index.json")).unwrap(),
        before
    );
    drop(lock);
}

#[test]
fn boundary_snapshot_defers_missing_and_partial_indexes_without_starting_a_scan() {
    for partial in [false, true] {
        let f = HistoryFixture::new(0);
        let path = f.ctx.jobs.join(".queue-index.json");
        if partial {
            f.ctx
                .write_json(
                    &path,
                    &json!({"scan_complete":false,"scanned_at":0,"ids":["active"]}),
                )
                .unwrap();
        } else {
            fs::remove_file(&path).unwrap();
        }
        let before = fs::read(&path).ok();
        let _lock = FileLock::try_acquire(&f.ctx.jobs.join(".queue-scan.lock")).unwrap();
        assert!(
            f.ctx
                .yield_snapshot()
                .expect("boundary confirmation must defer without scanning")
                .is_none()
        );
        assert_eq!(fs::read(&path).ok(), before);
        assert_eq!(f.ctx.load("active").unwrap()["status"], "queued");
    }
}

#[test]
fn boundary_snapshot_reads_current_candidates_without_rescanning_terminal_history() {
    let f = HistoryFixture::new(256);
    f.put("active", "running");
    let before = fs::read(f.ctx.jobs.join(".queue-index.json")).unwrap();
    for _ in 0..3 {
        let jobs = f.ctx.yield_snapshot().unwrap().unwrap();
        assert_eq!(jobs.len(), 1);
        assert_eq!(jobs[0]["status"], "running");
        f.rt.epoch.fetch_add(31, Ordering::Relaxed);
    }
    assert_eq!(f.rt.records.load(Ordering::Relaxed), 3);
    assert_eq!(f.rt.terminal_records.load(Ordering::Relaxed), 0);
    assert_eq!(
        fs::read(f.ctx.jobs.join(".queue-index.json")).unwrap(),
        before
    );
}

#[test]
fn admission_still_reconciles_legacy_candidates_after_a_boundary_snapshot() {
    let f = HistoryFixture::new(16);
    f.put("legacy", "queued");
    f.put("registered", "queued");
    {
        let _gate = f
            .ctx
            .queue_lock_for("fixture_registration", std::time::Duration::from_secs(1))
            .unwrap();
        f.ctx.track_queued_job("registered").unwrap();
    }
    let snapshot = f.ctx.yield_snapshot().unwrap().unwrap();
    assert!(!snapshot.iter().any(|j| j["id"] == "legacy"));
    assert!(snapshot.iter().any(|j| j["id"] == "registered"));
    // queue_lock is the ordinary preparation path: it must discover the old
    // writer, independently of the preceding advisory boundary read.
    let _gate = f.ctx.queue_lock().unwrap();
    let mut ids: Vec<_> = f
        .ctx
        .admission_jobs()
        .unwrap()
        .iter()
        .map(|j| s(j, "id").to_owned())
        .collect();
    ids.sort();
    assert_eq!(ids, ["active", "legacy", "registered"]);
    assert_eq!(f.rt.terminal_records.load(Ordering::Relaxed), 16);
}

#[test]
fn actual_admission_prepares_history_even_when_boundary_candidates_are_complete() {
    let f = HistoryFixture::new(16);
    f.put("active", "succeeded");
    f.put("legacy", "running");
    let mut legacy = f.ctx.load("legacy").unwrap();
    legacy["queue_unreadable"] = json!(true);
    f.ctx.save(&legacy).unwrap();
    f.put("registered", "queued");
    f.ctx
        .write_json(
            &f.ctx.root.join("queue-config.json"),
            &json!({"max_running":1}),
        )
        .unwrap();
    {
        let _gate = f
            .ctx
            .queue_lock_for("fixture_registration", std::time::Duration::from_secs(1))
            .unwrap();
        f.ctx.track_queued_job("registered").unwrap();
    }
    assert!(
        f.ctx.admit("registered").unwrap().is_none(),
        "the unindexed legacy owner must occupy the only slot"
    );
    let registered = f.ctx.load("registered").unwrap();
    assert_eq!(registered["status"], "queued");
    assert_eq!(registered["queue_wait_reason"]["code"], "running_limit");
    assert_eq!(f.rt.terminal_records.load(Ordering::Relaxed), 16);
}

#[test]
fn context_limit_defers_an_unknown_index_instead_of_treating_it_as_no_descendants() {
    for state in ["missing", "partial", "corrupt"] {
        let f = HistoryFixture::new(0);
        let mut parent = f.ctx.load("active").unwrap();
        parent["status"] = json!("running");
        parent["thread_id"] = json!("01a15769-07cb-71c2-9a62-cfc92e712233");
        parent["context_pct_exact"] = json!(99.);
        parent["started_at"] = json!(f.ctx.rt.now());
        parent["codex_pid"] = json!(444);
        parent["codex_pid_created"] = json!(44);
        f.ctx.save(&parent).unwrap();
        let events = ["item.started", "item.completed"].map(|kind| json!({"type":kind,"_ts":parent["started_at"],"item":{"id":"completed-command","type":"command_execution"}}).to_string()+"\n").concat();
        fs::write(f.ctx.job_dir("active").join("events.jsonl"), events).unwrap();
        let path = f.ctx.jobs.join(".queue-index.json");
        let complete = fs::read(&path).unwrap();
        assert!(
            f.ctx.context_rotation_due(&parent, &[]),
            "positive control: empty candidates would rotate this live job"
        );
        assert!(
            f.ctx.yield_may_be_due(&parent),
            "positive control: a complete candidate index permits rotation checks"
        );
        match state {
            "missing" => fs::remove_file(&path).unwrap(),
            "partial" => f
                .ctx
                .write_json(&path, &json!({"ids":["active"],"scan_complete":false}))
                .unwrap(),
            _ => fs::write(&path, "{").unwrap(),
        }
        assert!(
            !f.ctx.yield_may_be_due(&parent),
            "{state}: unknown descendants must defer before inventory or pause"
        );
        assert!(!f.ctx.yield_running_tool_boundary(&parent).unwrap());
        assert_eq!(f.rt.inventories.load(Ordering::Relaxed), 0);
        assert_eq!(f.rt.pauses.load(Ordering::Relaxed), 0);
        fs::write(&path, complete).unwrap();
        assert!(f.ctx.yield_may_be_due(&parent));
    }
}

#[test]
fn completed_boundary_reconciles_legacy_descendants_before_inventory_or_pause() {
    let f = HistoryFixture::new(16);
    let mut parent = f.ctx.load("active").unwrap();
    parent["status"] = json!("running");
    parent["thread_id"] = json!("01a15769-07cb-71c2-9a62-cfc92e712233");
    parent["context_pct_exact"] = json!(99.);
    parent["started_at"] = json!(f.ctx.rt.now());
    parent["codex_pid"] = json!(444);
    parent["codex_pid_created"] = json!(44);
    f.ctx.save(&parent).unwrap();
    let events = ["item.started", "item.completed"].map(|kind| json!({"type":kind,"_ts":parent["started_at"],"item":{"id":"completed-command","type":"command_execution"}}).to_string()+"\n").concat();
    fs::write(f.ctx.job_dir("active").join("events.jsonl"), events).unwrap();
    f.put("legacy-descendant", "running");
    let mut descendant = f.ctx.load("legacy-descendant").unwrap();
    descendant["parent_job_id"] = json!("active");
    f.ctx.save(&descendant).unwrap();
    assert!(
        f.ctx
            .context_rotation_due(&parent, &f.ctx.yield_snapshot().unwrap().unwrap()),
        "stale advisory candidates do not yet contain the old writer's child"
    );
    assert!(!f.ctx.yield_running_tool_boundary(&parent).unwrap());
    assert_eq!(f.rt.inventories.load(Ordering::Relaxed), 0);
    assert_eq!(f.rt.pauses.load(Ordering::Relaxed), 0);
    assert!(
        f.ctx
            .admission_snapshot()
            .unwrap()
            .iter()
            .any(|j| j["id"] == "legacy-descendant")
    );
    // Positive control: the identical live boundary reaches both injected
    // operations when the child has really finished and history is renewed.
    descendant["status"] = json!("succeeded");
    f.ctx.save(&descendant).unwrap();
    f.rt.epoch.fetch_add(31, Ordering::Relaxed);
    assert!(!f.ctx.yield_running_tool_boundary(&parent).unwrap());
    assert_eq!(f.rt.inventories.load(Ordering::Relaxed), 1);
    assert_eq!(f.rt.pauses.load(Ordering::Relaxed), 1);
}

#[test]
fn destructive_transition_rejects_history_that_aged_after_preparation() {
    let f = HistoryFixture::new(16);
    let before = fs::read(f.ctx.jobs.join(".queue-index.json")).unwrap();
    let _gate = f
        .ctx
        .queue_lock_for("fixture_commit", std::time::Duration::from_secs(1))
        .unwrap();
    let error = f.ctx.fresh_admission_jobs().unwrap_err();
    assert_eq!(error.code, 75);
    assert_eq!(
        fs::read(f.ctx.jobs.join(".queue-index.json")).unwrap(),
        before
    );
    assert_eq!(f.rt.terminal_records.load(Ordering::Relaxed), 0);
    f.rt.epoch.store(100, Ordering::Relaxed);
    assert_eq!(f.ctx.fresh_admission_jobs().unwrap().len(), 1);
}

#[test]
#[ignore = "controlled before/after measurement with 8,713 real job files"]
fn boundary_snapshot_history_perf() {
    let f = HistoryFixture::new(8712);
    let mut elapsed_us = Vec::new();
    for _ in 0..3 {
        let started = std::time::Instant::now();
        let snapshot = f.ctx.yield_snapshot().unwrap().unwrap();
        elapsed_us.push(started.elapsed().as_micros());
        assert_eq!(snapshot.len(), 1);
        assert_eq!(snapshot[0]["id"], "active");
        f.rt.epoch.fetch_add(31, Ordering::Relaxed);
    }
    eprintln!(
        "boundary_history_perf: {}",
        json!({"history_count":8713,"active_count":1,"calls":3,"elapsed_us":elapsed_us,"job_reads":f.rt.records.load(Ordering::Relaxed),"terminal_reads":f.rt.terminal_records.load(Ordering::Relaxed)})
    );
}
