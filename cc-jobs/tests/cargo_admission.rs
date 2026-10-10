#![cfg(windows)]
use cc_jobs::{FileLock, gate::admission::*};
use std::{
    fs, io,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

struct Temp(PathBuf);
impl Temp {
    fn new() -> Self {
        static SEQ: AtomicU64 = AtomicU64::new(0);
        let path = std::env::temp_dir().join(format!(
            "cargo-admission-test-{}-{}",
            std::process::id(),
            SEQ.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&path).unwrap();
        Self(path)
    }
    fn policy(&self, slots: usize) {
        fs::write(
            self.0.join(CONFIG_FILE),
            serde_json::to_vec(&Config {
                max_slots: Some(slots),
                settle_seconds: 0.,
                ..Default::default()
            })
            .unwrap(),
        )
        .unwrap();
    }
    fn admit(&self, cpu: f64, memory: f64) -> io::Result<Option<Permit>> {
        try_admit(&self.0, 32, || {
            Ok(Sample {
                cpu_percent: cpu,
                free_memory_gib: memory,
            })
        })
    }
}
impl Drop for Temp {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
#[test]
fn healthy_machine_grows_beyond_three_and_shares_jobs_by_active_builds() {
    let t = Temp::new();
    t.policy(8);
    let held: Vec<_> = (0..8)
        .map(|n| {
            let permit = t.admit(50., 32.).unwrap().unwrap();
            assert_eq!(permit.jobs, (32 / (n + 1)).min(10));
            permit
        })
        .collect();
    assert_eq!(held.len(), 8);
    assert!(t.admit(50., 32.).unwrap().is_none());
    drop(held);
    assert_eq!(t.admit(50., 32.).unwrap().unwrap().jobs, 10);
}
#[test]
fn queue_overrides_reload_slots_build_jobs_and_test_threads() {
    let t = Temp::new();
    t.policy(8);
    let file = t.0.join("queue-config.json");
    fs::write(
        &file,
        r#"{"cargo_slots":2,"cargo_jobs_per_build":8,"test_threads":3,"fixed_max_running":100}"#,
    )
    .unwrap();
    let first = t.admit(50., 32.).unwrap().unwrap();
    assert_eq!((first.jobs, first.test_threads), (8, 3));
    let second = t.admit(50., 32.).unwrap().unwrap();
    assert_eq!((second.jobs, second.test_threads), (8, 3));
    assert!(t.admit(50., 32.).unwrap().is_none());
    fs::write(
        &file,
        r#"{"cargo_slots":3,"cargo_jobs_per_build":2,"test_threads":7}"#,
    )
    .unwrap();
    let third = t.admit(50., 32.).unwrap().unwrap();
    assert_eq!((third.jobs, third.test_threads), (2, 7));
    fs::write(&file, r#"{"cargo_slots":1}"#).unwrap();
    assert!(t.admit(50., 32.).unwrap().is_none());
    drop((first, second, third));
    assert_eq!(t.admit(50., 32.).unwrap().unwrap().jobs, 10);
    fs::write(&file, r#"{"test_threads":0}"#).unwrap();
    assert_eq!(
        t.admit(50., 32.).err().unwrap().kind(),
        io::ErrorKind::InvalidData
    );
}
#[test]
fn cargo_gate_applies_separate_test_limits_to_the_real_child_boundary() {
    let t = Temp::new();
    fs::write(t.0.join(CONFIG_FILE), r#"{"min_free_memory_gib":0,"grow_below_cpu_percent":100,"pause_at_cpu_percent":100,"settle_seconds":0}"#).unwrap();
    fs::write(
        t.0.join("queue-config.json"),
        r#"{"cargo_jobs_per_build":8,"test_threads":3}"#,
    )
    .unwrap();
    let cores = std::thread::available_parallelism().unwrap().get();
    let build = cores.min(8);
    let tests = cores.min(3);
    for (input, expected, rust_tests, nextest_tests) in [
        (
            vec!["nextest", "run", "--build-jobs=32", "--test-threads=32"],
            format!("nextest run --build-jobs={build} --test-threads={tests}"),
            tests,
            tests,
        ),
        (
            vec!["test", "--", "--test-threads", "32"],
            format!("test -- --test-threads {tests}"),
            tests,
            32,
        ),
        (vec!["test"], "test".into(), tests, 32),
        (
            vec!["test", "--", "--test-threads=1"],
            "test -- --test-threads=1".into(),
            1,
            32,
        ),
    ] {
        let output = cc_jobs::hidden_command(env!("CARGO_BIN_EXE_cargo_gate"))
            .arg("--echo-test-limits")
            .args(input)
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env("CODEX_JOBS_CARGO_LOCK_DIR", &t.0)
            .env("CARGO_TARGET_DIR", t.0.join("target"))
            .env("CARGO_BUILD_JOBS", "32")
            .env("RUST_TEST_THREADS", "32")
            .env("NEXTEST_TEST_THREADS", "32")
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        assert_eq!(
            String::from_utf8(output.stdout).unwrap().trim(),
            format!("{build}|{rust_tests}|{nextest_tests}|{expected}")
        );
    }
}
#[test]
fn exact_thresholds_pause_new_builds_and_middle_band_keeps_capacity() {
    let cfg = Config::default();
    let sample = |cpu_percent, free_memory_gib| Sample {
        cpu_percent,
        free_memory_gib,
    };
    assert_eq!(capacity(&cfg, 32, 3, sample(74.99, 16.)), Some(4));
    assert_eq!(capacity(&cfg, 32, 3, sample(75., 16.)), Some(3));
    assert_eq!(capacity(&cfg, 32, 3, sample(89.99, 16.)), Some(3));
    assert_eq!(capacity(&cfg, 32, 3, sample(90., 32.)), None);
    assert_eq!(capacity(&cfg, 32, 3, sample(0., 15.99)), None);
    assert_eq!(capacity(&cfg, 32, 3, sample(f64::NAN, 32.)), None);
    assert_eq!(capacity(&cfg, 32, 3, sample(10., f64::NAN)), None);
    assert_eq!(cfg.max_slots(32), 8);
    assert_eq!(cfg.max_slots(2), 1);
    let t = Temp::new();
    assert!(t.admit(90., 32.).unwrap().is_none());
    assert!(t.admit(0., 15.99).unwrap().is_none());
    assert!(t.admit(0., 16.).unwrap().is_some());
}
#[test]
fn policy_is_reread_when_growing_or_reducing_capacity() {
    let t = Temp::new();
    t.policy(1);
    let first = t.admit(40., 32.).unwrap().unwrap();
    assert!(t.admit(40., 32.).unwrap().is_none());
    t.policy(2);
    let second = t.admit(40., 32.).unwrap().unwrap();
    t.policy(1);
    assert!(t.admit(40., 32.).unwrap().is_none());
    drop(first);
    assert!(
        t.admit(40., 32.).unwrap().is_none(),
        "slot above the reduced ceiling still counts"
    );
    drop(second);
    assert!(t.admit(40., 32.).unwrap().is_some());
}
#[test]
fn legacy_slot_owners_are_counted_and_release_after_crash_or_drop() {
    let t = Temp::new();
    t.policy(2);
    let a = FileLock::try_acquire(&t.0.join("cargo-build.lock.0")).unwrap();
    let b = FileLock::try_acquire(&t.0.join("cargo-build.lock.7")).unwrap();
    assert!(t.admit(40., 32.).unwrap().is_none());
    drop(a);
    let permit = t.admit(40., 32.).unwrap().unwrap();
    assert_eq!(permit.jobs, 10);
    drop((permit, b));
    assert!(t.admit(40., 32.).unwrap().is_some());
}
#[test]
fn settling_period_prevents_a_burst_using_the_same_load_measurement() {
    let t = Temp::new();
    let first = t.admit(40., 32.).unwrap().unwrap();
    assert!(t.admit(40., 32.).unwrap().is_none());
    drop(first);
    assert!(
        t.admit(40., 32.).unwrap().is_some(),
        "no active build needs no settling"
    );
}
#[test]
fn native_counter_delta_includes_idle_kernel_time_and_rejects_unknown_values() {
    assert_eq!(cpu_percent([100, 200, 100], [150, 300, 200]), Some(75.));
    assert_eq!(cpu_percent([100, 200, 100], [100, 200, 100]), None);
    assert_eq!(cpu_percent([100, 200, 100], [99, 300, 200]), None);
    assert_eq!(cpu_percent([0, 0, 0], [10, 1, 1]), None);
}
#[test]
fn concurrent_admissions_cannot_exceed_live_ceiling() {
    let t = Temp::new();
    t.policy(2);
    let permits = std::thread::scope(|scope| {
        let workers: Vec<_> = (0..16)
            .map(|_| scope.spawn(|| t.admit(40., 32.).unwrap()))
            .collect();
        workers
            .into_iter()
            .filter_map(|w| w.join().unwrap())
            .collect::<Vec<_>>()
    });
    assert!(!permits.is_empty());
    assert!(permits.len() <= 2);
}

#[test]
fn concurrent_admission_respects_a_shared_settling_period() {
    let t = Temp::new();
    fs::write(
        t.0.join(CONFIG_FILE),
        serde_json::to_vec(&Config {
            settle_seconds: 60.,
            ..Default::default()
        })
        .unwrap(),
    )
    .unwrap();
    let permits = std::thread::scope(|scope| {
        let workers: Vec<_> = (0..16)
            .map(|_| scope.spawn(|| t.admit(40., 32.).unwrap()))
            .collect();
        workers
            .into_iter()
            .filter_map(|w| w.join().unwrap())
            .collect::<Vec<_>>()
    });
    assert_eq!(
        permits.len(),
        1,
        "the admission mutex must serialize the cooldown check and reservation"
    );
}
#[test]
fn malformed_or_unreadable_policy_and_measurement_failures_are_errors() {
    let t = Temp::new();
    fs::write(t.0.join(CONFIG_FILE), "{").unwrap();
    assert!(t.admit(40., 32.).is_err());
    t.policy(0);
    assert!(t.admit(40., 32.).is_err());
    t.policy(2);
    assert!(try_admit(&t.0, 32, || Err(io::Error::other("counter failed"))).is_err());
    assert!(t.admit(40., 32.).unwrap().is_some());
}

#[test]
fn yesterday_capacity_never_grows_today_in_the_middle_band() {
    let t = Temp::new();
    t.policy(8);
    let mut held: Vec<_> = (0..8)
        .map(|_| t.admit(40., 32.).unwrap().unwrap())
        .collect();
    held.truncate(2);
    assert!(t.admit(80., 32.).unwrap().is_none());
    assert!(t.admit(90., 32.).unwrap().is_none());
    assert!(t.admit(40., 32.).unwrap().is_some());
}

#[test]
fn damaged_history_is_rebuilt_and_atomic_snapshot_replaces_old_state() {
    let t = Temp::new();
    fs::write(t.0.join("cargo-admission-state.json"), "{broken").unwrap();
    let permit = t.admit(40., 32.).unwrap().unwrap();
    let value: serde_json::Value =
        serde_json::from_slice(&fs::read(t.0.join("cargo-admission-state.json")).unwrap()).unwrap();
    assert_eq!(value["active_at_admission"], 1);
    assert!(!t.0.join("cargo-admission-state.json.tmp").exists());
    drop(permit);
    assert!(t.admit(40., 32.).unwrap().is_some());
}

#[test]
fn diagnostic_snapshot_sharing_failure_does_not_abort_a_build() {
    use std::os::windows::fs::OpenOptionsExt;
    let t = Temp::new();
    let permit = t.admit(40., 32.).unwrap().unwrap();
    drop(permit);
    let _reader = fs::OpenOptions::new()
        .read(true)
        .share_mode(1)
        .open(t.0.join(STATE_FILE))
        .unwrap();
    assert!(
        t.admit(40., 32.).unwrap().is_some(),
        "admission history is advisory; actual slot locks and live counters still control builds"
    );
}

#[test]
fn cpu_measurement_does_not_block_free_legacy_slot_locks() {
    let t = Temp::new();
    assert!(
        try_admit(&t.0, 32, || {
            let lock = FileLock::try_acquire(&t.0.join("cargo-build.lock.0"))?;
            drop(lock);
            Ok(Sample {
                cpu_percent: 40.,
                free_memory_gib: 32.,
            })
        })
        .unwrap()
        .is_some()
    );
}

#[test]
fn waiting_process_rereads_config_despite_its_inherited_one_slot_setting() {
    use cc_jobs::hidden_command;
    use std::{
        process::{Child, Stdio},
        thread,
        time::{Duration, Instant},
    };
    struct OwnedChild(Option<Child>);
    impl Drop for OwnedChild {
        fn drop(&mut self) {
            if let Some(child) = &mut self.0 {
                let _ = child.kill();
                let _ = child.wait();
            }
        }
    }
    let t = Temp::new();
    let policy = |max_slots| {
        let tmp = t.0.join("config.tmp");
        fs::write(
            &tmp,
            serde_json::to_vec(&Config {
                max_slots: Some(max_slots),
                min_free_memory_gib: 0.,
                grow_below_cpu_percent: 100.,
                pause_at_cpu_percent: 100.,
                settle_seconds: 0.,
                ..Default::default()
            })
            .unwrap(),
        )
        .unwrap();
        fs::rename(tmp, t.0.join(CONFIG_FILE)).unwrap();
    };
    policy(1);
    let job_dir = t.0.join("jobs/test-config-reload");
    fs::create_dir_all(&job_dir).unwrap();
    fs::write(job_dir.join("job.json"), "{}").unwrap();
    let lock = FileLock::try_acquire(&t.0.join("cargo-build.lock.0")).unwrap();
    let mut child = OwnedChild(Some(
        hidden_command(env!("CARGO_BIN_EXE_cargo_gate"))
            .arg("--echo-env")
            .env(
                "CODEX_JOBS_REAL_CARGO",
                env!("CARGO_BIN_EXE_cc_jobs_fixture"),
            )
            .env("CODEX_JOBS_CARGO_GATE", t.0.join("cargo_gate.exe"))
            .env("CODEX_JOBS_CARGO_LOCK_DIR", &t.0)
            .env("CODEX_JOBS_CARGO_SLOTS", "1")
            .env("CC_JOB_ID", "test-config-reload")
            .env("CARGO_TARGET_DIR", t.0.join("target"))
            .env_remove("CODEX_JOBS_CARGO_LOCK_HELD")
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap(),
    ));
    let deadline = Instant::now() + Duration::from_secs(30);
    while !job_dir.join("resource-waits").exists() {
        assert!(
            Instant::now() < deadline,
            "gate never entered the wait loop"
        );
        assert!(child.0.as_mut().unwrap().try_wait().unwrap().is_none());
        thread::sleep(Duration::from_millis(10));
    }
    assert!(child.0.as_mut().unwrap().try_wait().unwrap().is_none());
    let broken = t.0.join("config.tmp");
    fs::write(&broken, "{invalid").unwrap();
    fs::rename(broken, t.0.join(CONFIG_FILE)).unwrap();
    loop {
        let seen = fs::read_dir(job_dir.join("resource-waits"))
            .unwrap()
            .filter_map(Result::ok)
            .any(|entry| {
                fs::read(entry.path())
                    .ok()
                    .and_then(|bytes| serde_json::from_slice::<serde_json::Value>(&bytes).ok())
                    .is_some_and(|value| value["reason"] == "cargoの設定を読み取れないため待機中")
            });
        if seen {
            break;
        }
        assert!(
            Instant::now() < deadline,
            "invalid config did not produce a retryable wait"
        );
        assert!(
            child.0.as_mut().unwrap().try_wait().unwrap().is_none(),
            "a transient config error killed the waiting gate"
        );
        thread::sleep(Duration::from_millis(10));
    }
    policy(2);
    // On one-core machines both ceilings are 1; assert the unchanged ceiling
    // instead. Supported production/CI machines have at least two cores.
    if std::thread::available_parallelism().unwrap().get() == 1 {
        drop(lock);
    } else {
        let _keep_legacy_lock = lock;
        while child.0.as_mut().unwrap().try_wait().unwrap().is_none() {
            assert!(
                Instant::now() < deadline,
                "old environment froze the slot ceiling"
            );
            thread::sleep(Duration::from_millis(10));
        }
    }
    let output = child.0.take().unwrap().wait_with_output().unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert_eq!(String::from_utf8(output.stdout).unwrap().trim(), "1");
}
