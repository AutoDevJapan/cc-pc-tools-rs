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
