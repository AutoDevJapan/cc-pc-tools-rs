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
    fn proc_created(&self, _: u32) -> Option<u64> {
        None
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
        .expect("boundary read must not attempt reconciliation");
    assert_eq!(
        jobs.iter().map(|j| s(j, "id")).collect::<Vec<_>>(),
        ["active"]
    );
    assert_eq!(f.rt.terminal_records.load(Ordering::Relaxed), 0);
    assert_eq!(
        fs::read(f.ctx.jobs.join(".queue-index.json")).unwrap(),
        before
    );
    assert!(FileLock::try_acquire(&f.ctx.jobs.join(".queue-scan.lock")).is_err());
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
                .is_empty()
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
        let jobs = f.ctx.yield_snapshot().unwrap();
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
    let snapshot = f.ctx.yield_snapshot().unwrap();
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
#[ignore = "controlled before/after measurement with 8,713 real job files"]
fn boundary_snapshot_history_perf() {
    let f = HistoryFixture::new(8712);
    let mut elapsed_us = Vec::new();
    for _ in 0..3 {
        let started = std::time::Instant::now();
        let snapshot = f.ctx.yield_snapshot().unwrap();
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
