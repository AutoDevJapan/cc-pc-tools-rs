use cc_jobs::*;
use std::{
    io,
    path::Path,
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
};

struct Probe {
    primary: Option<Option<u64>>,
    observed: Option<Option<u64>>,
    observations: AtomicUsize,
    mutations: AtomicUsize,
}
impl Runtime for Probe {
    fn now(&self) -> String {
        "2026-10-27T00:00:00+09:00".into()
    }
    fn mono(&self) -> f64 {
        0.
    }
    fn proc_created_checked(&self, _: u32) -> io::Result<Option<u64>> {
        self.primary
            .ok_or_else(|| io::Error::from(io::ErrorKind::PermissionDenied))
    }
    fn observed_process_birth(&self, _: u32) -> io::Result<Option<u64>> {
        self.observations.fetch_add(1, Ordering::Relaxed);
        self.observed
            .ok_or_else(|| io::Error::other("observation unavailable"))
    }
    fn call(&self, _: Call) -> io::Result<Reply> {
        Err(io::Error::other("unexpected command"))
    }
    fn spawn(&self, _: &Spawn) -> io::Result<Box<dyn Running>> {
        Err(io::Error::other("unexpected spawn"))
    }
    fn spawn_supervisor(&self, _: &Path, _: &Path, _: &Path, _: &str) -> io::Result<bool> {
        Err(io::Error::other("unexpected supervisor"))
    }
    fn kill_tree(&self, _: u32) -> io::Result<()> {
        self.mutations.fetch_add(1, Ordering::Relaxed);
        Ok(())
    }
}
fn context(root: &Path, runtime: Arc<Probe>) -> Context {
    Context {
        root: root.into(),
        jobs: root.join("jobs"),
        home: root.into(),
        codex_home: root.into(),
        gto: root.join("unused"),
        executable: root.join("unused.exe"),
        rt: runtime,
    }
}
fn probe(primary: Option<Option<u64>>, observed: Option<Option<u64>>) -> Arc<Probe> {
    Arc::new(Probe {
        primary,
        observed,
        observations: AtomicUsize::new(0),
        mutations: AtomicUsize::new(0),
    })
}
fn job() -> Value {
    json!({"id":"terminal","status":"succeeded","codex_pid":77,"codex_pid_created":100})
}

#[test]
fn denied_reused_pid_does_not_keep_a_finished_job_running() {
    let temp = tempfile::tempdir().unwrap();
    let rt = probe(None, Some(Some(200)));
    let ctx = context(temp.path(), rt.clone());
    assert!(!ctx.is_ours(&job(), "codex_pid"));
    assert!(!ctx.running_job(&job()));
    assert!(!ctx.is_ours_checked(&job(), "codex_pid").unwrap());
    assert!(rt.kill_owned_tree(77, 100).is_err());
    assert_eq!(rt.mutations.load(Ordering::Relaxed), 0);
}

#[test]
fn matching_or_unknown_observation_never_grants_a_mutation_handle() {
    let temp = tempfile::tempdir().unwrap();
    for observed in [Some(Some(100)), Some(Some(0)), Some(None), None] {
        let rt = probe(None, observed);
        let ctx = context(temp.path(), rt.clone());
        assert!(ctx.is_ours(&job(), "codex_pid"));
        assert!(ctx.is_ours_checked(&job(), "codex_pid").is_err());
        assert!(rt.kill_owned_tree(77, 100).is_err());
        assert_eq!(rt.mutations.load(Ordering::Relaxed), 0);
    }
}

#[test]
fn retention_snapshot_disproves_only_a_kernel_birth_after_a_valid_terminal_interval() {
    let end = parse_iso("2026-10-26T00:00:00+09:00").unwrap();
    let filetime = |epoch: f64| ((epoch + 11_644_473_600.) * 10_000_000.) as u64;
    for (case, observed, status, finish, expected) in [
        (
            "reused",
            Some(Some(filetime(end + 120.))),
            "succeeded",
            "2026-10-26T00:00:00+09:00",
            false,
        ),
        (
            "earlier",
            Some(Some(filetime(end - 120.))),
            "succeeded",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
        (
            "close",
            Some(Some(filetime(end + 30.))),
            "succeeded",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
        (
            "no-birth",
            Some(Some(0)),
            "succeeded",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
        (
            "missing",
            Some(None),
            "succeeded",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
        (
            "denied",
            None,
            "succeeded",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
        (
            "bad-end",
            Some(Some(filetime(end + 120.))),
            "succeeded",
            "unknown",
            true,
        ),
        (
            "reversed",
            Some(Some(filetime(end + 120.))),
            "succeeded",
            "2020-01-01T00:00:00Z",
            true,
        ),
        (
            "future",
            Some(Some(filetime(end + 1_000_000_000.))),
            "succeeded",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
        (
            "running",
            Some(Some(filetime(end + 120.))),
            "running",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
        (
            "queued",
            Some(Some(filetime(end + 120.))),
            "queued",
            "2026-10-26T00:00:00+09:00",
            true,
        ),
    ] {
        let temp = tempfile::tempdir().unwrap();
        let rt = probe(None, observed);
        let ctx = context(temp.path(), rt.clone());
        std::fs::create_dir(&ctx.jobs).unwrap();
        let _lock = FileLock::try_acquire(&ctx.root.join("queue.lock")).unwrap();
        let id = "20261026-000001-todo-1";
        let dir = ctx.job_dir(id);
        std::fs::create_dir(&dir).unwrap();
        let record = json!({"id":id,"status":status,"created_at":"2026-10-25T00:00:00+09:00","finished_at":finish,"codex_pid":77});
        let original = serde_json::to_vec(&record).unwrap();
        std::fs::write(dir.join("job.json"), &original).unwrap();
        let index = serde_json::to_vec(
            &json!({"scan_complete":true,"scanned_at":ctx.rt.epoch(),"ids":[id]}),
        )
        .unwrap();
        std::fs::write(ctx.jobs.join(".queue-index.json"), &index).unwrap();
        assert_eq!(
            ctx.active_job_ids_snapshot()
                .unwrap()
                .contains(&id.to_owned()),
            expected,
            "{case}"
        );
        assert_eq!(std::fs::read(dir.join("job.json")).unwrap(), original);
        assert_eq!(
            std::fs::read(ctx.jobs.join(".queue-index.json")).unwrap(),
            index
        );
        assert!(ctx.is_ours_checked(&record, "codex_pid").is_err());
        assert_eq!(rt.mutations.load(Ordering::Relaxed), 0);
        if case == "reused" {
            assert!(rt.observations.load(Ordering::Relaxed) > 0);
        }
    }
}

#[cfg(windows)]
#[test]
fn native_observation_matches_the_current_process_handle() {
    let rt = System::default();
    let pid = std::process::id();
    let expected = rt.proc_created_checked(pid).unwrap().unwrap();
    assert!(expected > 0);
    assert_eq!(rt.observed_process_birth(pid).unwrap(), Some(expected));
    assert_eq!(rt.observed_process_birth(0).unwrap(), None);
}

#[test]
fn successful_handle_checks_do_not_query_the_fallback() {
    let temp = tempfile::tempdir().unwrap();
    for (primary, expected) in [(Some(100), true), (Some(200), false), (None, false)] {
        let rt = probe(Some(primary), Some(Some(999)));
        let ctx = context(temp.path(), rt.clone());
        assert_eq!(ctx.is_ours(&job(), "codex_pid"), expected);
        assert_eq!(rt.observations.load(Ordering::Relaxed), 0);
    }
}

#[test]
fn mutation_counter_has_a_positive_control_with_the_existing_handle_check() {
    let rt = probe(Some(Some(100)), Some(Some(999)));
    rt.kill_owned_tree(77, 100).unwrap();
    assert_eq!(rt.mutations.load(Ordering::Relaxed), 1);
    assert_eq!(rt.observations.load(Ordering::Relaxed), 0);
}

#[test]
fn missing_or_zero_recorded_birth_stays_unknown() {
    let temp = tempfile::tempdir().unwrap();
    for value in [Value::Null, json!(0)] {
        let rt = probe(None, Some(Some(200)));
        let ctx = context(temp.path(), rt);
        let mut j = job();
        j["codex_pid_created"] = value;
        assert!(ctx.is_ours(&j, "codex_pid"));
        assert!(ctx.is_ours_checked(&j, "codex_pid").is_err());
    }
}
