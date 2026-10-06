use cc_ask::civ6_idle::check_idle;
use cc_live::{
    chain::{CIV_EXECUTABLES, REPORTER_COMMAND},
    Call, Output, Paths, Runner,
};
use serde_json::{json, Value};
use std::{collections::HashSet, fs, io::Write, path::PathBuf};

struct Fixture {
    _root: tempfile::TempDir,
    paths: Paths,
    state: PathBuf,
    log: PathBuf,
    fake: Fake,
}
impl Fixture {
    fn new() -> Self {
        let root = tempfile::tempdir().unwrap();
        let paths = Paths {
            here: root.path().join("live"),
            jobs: root.path().join("jobs"),
            wt: root.path().join("worktrees"),
            ..Paths::default()
        };
        fs::create_dir_all(paths.here.join("queue")).unwrap();
        Self {
            state: root.path().join("idle.json"),
            log: root.path().join("tick.log"),
            fake: Fake {
                now: 1000.0,
                game: Some(CIV_EXECUTABLES[0]),
                calls: vec![],
                alive: HashSet::new(),
                live_chain_pid: None,
                paths: paths.clone(),
                log: root.path().join("tick.log"),
                inject_holder: false,
                inject_waiter: false,
                replace_dead_holder: false,
                fail_tasklist: false,
                fail_kill: false,
            },
            paths,
            _root: root,
        }
    }
    fn tick(&mut self, elapsed: f64) -> Result<bool, String> {
        self.fake.now = 1000.0 + elapsed;
        check_idle(&mut self.fake, &self.paths, &self.state)
    }
    fn owner(&self, info: Value) {
        fs::write(self.paths.here.join("civ6.lock"), info.to_string()).unwrap();
    }
    fn job(&self, name: &str, status: &str) {
        let dir = self.paths.jobs.join(name);
        fs::create_dir_all(&dir).unwrap();
        fs::write(dir.join("job.json"), json!({"status":status}).to_string()).unwrap();
    }
    fn waiter(&self, info: Value) {
        fs::write(self.paths.here.join("queue/waiter.json"), info.to_string()).unwrap();
    }
    fn wait_five_minutes(&mut self) {
        for t in [0., 60., 120., 180., 240.] {
            assert!(!self.tick(t).unwrap());
        }
    }
    fn kills(&self) -> Vec<&str> {
        self.fake
            .calls
            .iter()
            .filter(|c| c.args[0] == "taskkill")
            .map(|c| c.args[2].as_str())
            .collect()
    }
}

#[test]
fn direct_lock_kept_without_competitor_then_revoked_at_thirty_minutes_once() {
    let mut f = Fixture::new();
    f.owner(json!({"job":"owner"}));
    f.job("owner", "running");
    let artifacts = f.paths.live().join("tools/civ6-autotest/artifacts/old");
    fs::create_dir_all(&artifacts).unwrap();
    fs::File::create(artifacts.join("steps.jsonl"))
        .unwrap()
        .set_times(
            fs::FileTimes::new()
                .set_modified(std::time::UNIX_EPOCH + std::time::Duration::from_secs(1000)),
        )
        .unwrap();
    let todo = f.paths.here.join("todo.json");
    f.fake.now = 10000.;
    assert!(!cc_ask::civ6_idle::check_fairness(&mut f.fake, &f.paths, &todo).unwrap());
    assert!(f.paths.here.join("civ6.lock").exists());
    f.job("waiter", "running");
    f.waiter(json!({"job":"waiter","since":10000}));
    f.fake.now = 11799.;
    assert!(!cc_ask::civ6_idle::check_fairness(&mut f.fake, &f.paths, &todo).unwrap());
    assert!(f.kills().is_empty());
    f.fake.now = 11800.;
    assert!(cc_ask::civ6_idle::check_fairness(&mut f.fake, &f.paths, &todo).unwrap());
    assert!(!f.paths.here.join("civ6.lock").exists());
    assert_eq!(f.kills(), CIV_EXECUTABLES);
    assert!(!cc_ask::civ6_idle::check_fairness(&mut f.fake, &f.paths, &todo).unwrap());
    assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
    let revoked: Value =
        serde_json::from_slice(&fs::read(f.paths.here.join("civ6-revoked.json")).unwrap()).unwrap();
    assert_eq!(revoked["owner"]["job"], "owner");
}

#[test]
fn expired_lock_without_step_artifacts_is_revoked_without_closing_game() {
    let mut f = Fixture::new();
    f.owner(json!({"job":"owner"}));
    f.job("owner", "running");
    f.job("waiter", "running");
    f.waiter(json!({"job":"waiter","since":1000}));
    f.fake.now = 2800.;
    let todo = f.paths.here.join("todo.json");
    assert!(cc_ask::civ6_idle::check_fairness(&mut f.fake, &f.paths, &todo).unwrap());
    assert!(!f.paths.here.join("civ6.lock").exists());
    assert!(f.kills().is_empty());
    assert!(f.fake.game.is_some());
    assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
}

#[test]
fn direct_lock_with_ready_scheduler_waiter_revoked_but_recent_step_keeps_game() {
    for age in [599., 600.] {
        let mut f = Fixture::new();
        f.owner(json!({"job":"owner"}));
        f.job("owner", "running");
        let todo = f.paths.here.join("todo.json");
        fs::write(&todo,json!({"items":[{"id":437,"created_s":1000,"status":"todo","dispatchable":true,"spec":"s.md","needs":["civ6"]}]}).to_string()).unwrap();
        let artifacts = f.paths.live().join("tools/civ6-autotest/artifacts");
        fs::create_dir_all(artifacts.join("older")).unwrap();
        fs::create_dir(artifacts.join("newer")).unwrap();
        for (name, t) in [("older", 1000.), ("newer", 2800. - age)] {
            let file = fs::File::create(artifacts.join(name).join("steps.jsonl")).unwrap();
            file.set_times(
                fs::FileTimes::new()
                    .set_modified(std::time::UNIX_EPOCH + std::time::Duration::from_secs_f64(t)),
            )
            .unwrap();
        }
        f.fake.now = 2800.;
        assert!(cc_ask::civ6_idle::check_fairness(&mut f.fake, &f.paths, &todo).unwrap());
        assert!(!f.paths.here.join("civ6.lock").exists());
        if age < 600. {
            assert!(f.kills().is_empty());
            assert!(f.fake.game.is_some());
        } else {
            assert_eq!(f.kills(), CIV_EXECUTABLES);
        }
        assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
    }
}

#[test]
fn tick_records_expiration_but_never_revokes_active_live_chain() {
    let mut f = Fixture::new();
    f.owner(json!({"job":"owner","live_chain":true,"pid":777,"acquired_at":1000}));
    f.job("owner", "running");
    f.fake.alive.insert(777);
    f.fake.live_chain_pid = Some(777);
    f.job("waiter", "running");
    f.waiter(json!({"job":"waiter","since":1000}));
    f.fake.now = 2800.;
    let todo = f.paths.here.join("todo.json");
    assert!(!cc_ask::civ6_idle::check_fairness(&mut f.fake, &f.paths, &todo).unwrap());
    let lock: Value =
        serde_json::from_slice(&fs::read(f.paths.here.join("civ6.lock")).unwrap()).unwrap();
    assert_eq!(lock["competitor_since"], 1000.);
    assert!(f.kills().is_empty());
    assert!(!f.log.exists());
}

struct Fake {
    now: f64,
    game: Option<&'static str>,
    calls: Vec<Call>,
    alive: HashSet<u64>,
    live_chain_pid: Option<u64>,
    paths: Paths,
    log: PathBuf,
    inject_holder: bool,
    inject_waiter: bool,
    replace_dead_holder: bool,
    fail_tasklist: bool,
    fail_kill: bool,
}
impl Runner for Fake {
    fn run(&mut self, c: Call) -> Result<Output, String> {
        assert_eq!(c.creationflags, cc_live::process::CREATE_NO_WINDOW);
        assert_eq!(c.timeout, Some(30.0));
        self.calls.push(c.clone());
        match c.args[0].as_str() {
            "tasklist" => {
                if self.fail_tasklist {
                    return Ok(Output {
                        code: 1,
                        stderr: "inspection failed".into(),
                        ..Output::default()
                    });
                }
                let filter = &c.args[2];
                let stdout = if let Some(pid) = filter.strip_prefix("PID eq ") {
                    assert_eq!(&c.args[3..], ["/FO", "CSV", "/NH"]);
                    if self.replace_dead_holder {
                        self.replace_dead_holder = false;
                        fs::write(
                            self.paths.here.join("civ6.lock"),
                            json!({"pid":777}).to_string(),
                        )
                        .unwrap();
                        self.alive.insert(777);
                    }
                    if let Some(game) = self.game.filter(|_| pid == "42") {
                        format!("\"{game}\",\"42\"")
                    } else if self.alive.contains(&pid.parse().unwrap()) {
                        let image = if self.live_chain_pid == pid.parse().ok() {
                            "live_chain.exe"
                        } else {
                            "worker.exe"
                        };
                        format!("\"{image}\",\"{pid}\"")
                    } else {
                        "INFO: no tasks".into()
                    }
                } else if let Some(game) = self.game.filter(|name| filter.ends_with(name)) {
                    format!("\"{game}\",\"42\"")
                } else {
                    "INFO: no tasks".into()
                };
                Ok(Output {
                    stdout,
                    ..Output::default()
                })
            }
            "taskkill" => {
                // The real shared lock must be claimed before the first destructive command.
                let lock = self.paths.here.join("civ6.lock");
                let owner: Value = serde_json::from_slice(&fs::read(&lock).unwrap()).unwrap();
                assert_eq!(owner["pid"], std::process::id());
                assert!(fs::OpenOptions::new()
                    .write(true)
                    .create_new(true)
                    .open(&lock)
                    .is_err());
                #[cfg(windows)]
                {
                    assert!(fs::remove_file(&lock).is_err());
                    assert!(fs::write(&lock, "replacement").is_err());
                }
                if self.fail_kill {
                    return Ok(Output {
                        code: 1,
                        stderr: "access denied".into(),
                        ..Output::default()
                    });
                }
                if self.game == Some(c.args[2].as_str()) {
                    self.game = None;
                }
                Ok(Output::default())
            }
            "powershell" => {
                assert!(self.game.is_none());
                assert_eq!(
                    c.args,
                    ["powershell", "-NoProfile", "-Command", REPORTER_COMMAND]
                );
                Ok(Output::default())
            }
            other => panic!("unexpected subprocess: {other}"),
        }
    }
    fn now(&mut self) -> f64 {
        if self.inject_waiter {
            self.inject_waiter = false;
            fs::write(
                self.paths.here.join("queue/new-waiter.json"),
                json!({"pid":888}).to_string(),
            )
            .unwrap();
            self.alive.insert(888);
        }
        if self.inject_holder {
            self.inject_holder = false;
            fs::write(
                self.paths.here.join("civ6.lock"),
                json!({"pid":777}).to_string(),
            )
            .unwrap();
            self.alive.insert(777);
        }
        self.now
    }
    fn sleep(&mut self, seconds: f64) -> Result<(), String> {
        self.now += seconds;
        Ok(())
    }
    fn print(&mut self, line: &str) -> Result<(), String> {
        // Model the scheduled task's stdout >> tick.log redirection.
        writeln!(
            fs::OpenOptions::new()
                .create(true)
                .append(true)
                .open(&self.log)
                .unwrap(),
            "{line}"
        )
        .unwrap();
        Ok(())
    }
}

#[test]
fn live_pid_holder_never_closes_game() {
    let mut f = Fixture::new();
    f.owner(json!({"pid":777}));
    f.fake.alive.insert(777);
    for t in [0., 60., 120., 180., 240., 300., 600.] {
        assert!(!f.tick(t).unwrap());
    }
    assert!(f.kills().is_empty());
    assert!(!f.state.exists());
    assert_eq!(
        fs::read_to_string(f.paths.here.join("civ6.lock")).unwrap(),
        "{\"pid\":777}"
    );
}

#[test]
fn queued_starting_and_running_job_holders_never_close_game() {
    for (key, status) in ["job", "owner_job"]
        .into_iter()
        .flat_map(|key| ["queued", "starting", "running"].map(|status| (key, status)))
    {
        let mut f = Fixture::new();
        f.owner(json!({key:"owner","pid":42}));
        f.job("owner", status);
        f.wait_five_minutes();
        assert!(!f.tick(300.).unwrap());
        assert!(f.kills().is_empty());
        assert!(!f.state.exists());
    }
}

#[test]
fn missing_lock_for_less_than_five_minutes_does_not_close() {
    let mut f = Fixture::new();
    f.wait_five_minutes();
    assert!(!f.tick(299.).unwrap());
    assert!(f.kills().is_empty());
    assert!(!f.log.exists());
    let state: Value = serde_json::from_slice(&fs::read(&f.state).unwrap()).unwrap();
    assert_eq!(state, json!({"first_seen":1000.0,"last_seen":1299.0}));
}

#[test]
fn five_minutes_closes_both_renderers_once_and_scoped_reporter_and_logs_one_line() {
    for renderer in CIV_EXECUTABLES {
        let mut f = Fixture::new();
        f.fake.game = Some(renderer);
        f.wait_five_minutes();
        // The countdown survives a fresh runner with no observations in memory.
        f.fake = Fake {
            live_chain_pid: None,
            now: 1300.0,
            game: Some(renderer),
            calls: vec![],
            alive: HashSet::new(),
            paths: f.paths.clone(),
            log: f.log.clone(),
            inject_holder: false,
            inject_waiter: false,
            replace_dead_holder: false,
            fail_tasklist: false,
            fail_kill: false,
        };
        assert!(f.tick(300.).unwrap());
        assert!(!f.tick(360.).unwrap());
        assert_eq!(f.kills(), CIV_EXECUTABLES);
        assert_eq!(
            f.fake
                .calls
                .iter()
                .filter(|c| c.args[0] == "powershell")
                .count(),
            1
        );
        assert!(!f.paths.here.join("civ6.lock").exists());
        assert!(!f.state.exists());
        let log = fs::read_to_string(&f.log).unwrap();
        assert_eq!(log.lines().count(), 1);
        assert!(log.contains("Civ6を終了しました"));
    }
}

#[test]
fn dead_pid_or_finished_job_holder_allows_cleanup() {
    for owner in [
        json!({"pid":777}),
        json!({"job":"finished"}),
        json!({"job":"missing"}),
    ] {
        let mut f = Fixture::new();
        f.job("finished", "completed");
        f.owner(owner);
        f.wait_five_minutes();
        assert!(f.tick(300.).unwrap());
        assert_eq!(f.kills(), CIV_EXECUTABLES);
        let log = fs::read_to_string(&f.log).unwrap();
        assert_eq!(log.lines().count(), 2);
        assert_eq!(log.matches("ロックを削除しました").count(), 1);
    }
}

#[test]
fn game_pid_lock_is_removed_immediately_and_reaper_can_close_game() {
    for renderer in CIV_EXECUTABLES {
        let mut f = Fixture::new();
        f.fake.game = Some(renderer);
        f.owner(json!({"pid":42,"purpose":"Civ6の停止は禁止。"}));
        assert!(!f.tick(0.).unwrap());
        assert!(!f.paths.here.join("civ6.lock").exists());
        assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
        for t in [60., 120., 180., 240.] {
            assert!(!f.tick(t).unwrap());
        }
        assert!(f.tick(300.).unwrap());
        assert_eq!(f.kills(), CIV_EXECUTABLES);
        assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 2);
    }
}

#[test]
fn finished_job_overrides_live_pid_and_dead_lock_is_removed_without_a_game() {
    for key in ["job", "owner_job"] {
        for status in ["succeeded", "failed", "cancelled", "lost", "completed"] {
            let mut f = Fixture::new();
            f.fake.game = None;
            f.fake.alive.insert(777);
            f.job("owner", status);
            f.owner(json!({key:"owner","pid":777}));
            assert!(!f.tick(0.).unwrap());
            assert!(!f.paths.here.join("civ6.lock").exists());
            assert!(f.kills().is_empty());
            assert!(!f.state.exists());
            assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
            assert!(!f.tick(60.).unwrap());
            assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
        }
    }
}

#[test]
fn dead_lock_is_removed_while_live_waiter_can_proceed() {
    let mut f = Fixture::new();
    f.owner(json!({"pid":42}));
    f.waiter(json!({"job":"waiter"}));
    f.job("waiter", "queued");
    assert!(!f.tick(0.).unwrap());
    assert!(!f.paths.here.join("civ6.lock").exists());
    assert!(f.paths.here.join("queue/waiter.json").exists());
    assert!(f.kills().is_empty());
    assert!(!f.state.exists());
    assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
}

#[test]
fn reporter_and_steam_pid_locks_are_removed_without_killing_the_client() {
    for image in ["FiraxisBugReporter.exe", "steam.exe"] {
        let mut f = Fixture::new();
        f.fake.game = Some(image);
        f.owner(json!({"pid":42}));
        assert!(!f.tick(0.).unwrap());
        assert!(!f.paths.here.join("civ6.lock").exists());
        assert!(!f.state.exists());
        assert!(f.kills().is_empty());
        assert_eq!(fs::read_to_string(&f.log).unwrap().lines().count(), 1);
    }
}

#[test]
fn active_owner_replacing_dead_owner_before_protected_recheck_is_preserved() {
    let mut f = Fixture::new();
    f.owner(json!({"pid":42}));
    f.fake.replace_dead_holder = true;
    assert!(!f.tick(0.).unwrap());
    let owner: Value =
        serde_json::from_slice(&fs::read(f.paths.here.join("civ6.lock")).unwrap()).unwrap();
    assert_eq!(owner, json!({"pid":777}));
    assert!(f.kills().is_empty());
    assert!(!f.log.exists());
    assert!(!f.state.exists());
}

#[test]
fn live_pid_or_job_waiter_prevents_cleanup_and_resets_timer() {
    for status in [None, Some("queued"), Some("starting"), Some("running")] {
        let mut f = Fixture::new();
        f.wait_five_minutes();
        if let Some(status) = status {
            f.job("waiter", status);
            f.waiter(json!({"job":"waiter"}));
        } else {
            f.waiter(json!({"pid":888}));
            f.fake.alive.insert(888);
        }
        assert!(!f.tick(300.).unwrap());
        assert!(f.kills().is_empty());
        assert!(!f.state.exists());
        fs::remove_file(f.paths.here.join("queue/waiter.json")).unwrap();
        assert!(!f.tick(360.).unwrap());
    }
}

#[test]
fn dead_waiter_does_not_block_cleanup_and_is_not_deleted() {
    let mut f = Fixture::new();
    f.waiter(json!({"pid":888}));
    f.wait_five_minutes();
    assert!(f.tick(300.).unwrap());
    assert!(f.paths.here.join("queue/waiter.json").exists());
}

#[test]
fn holder_appearing_just_before_claim_is_preserved() {
    let mut f = Fixture::new();
    f.wait_five_minutes();
    f.fake.inject_holder = true;
    assert!(!f.tick(300.).unwrap());
    assert!(f.kills().is_empty());
    assert!(!f.state.exists());
    assert_eq!(
        fs::read_to_string(f.paths.here.join("civ6.lock")).unwrap(),
        "{\"pid\":777}"
    );
}

#[test]
fn waiter_appearing_just_before_claim_is_rechecked_and_prevents_closure() {
    let mut f = Fixture::new();
    f.wait_five_minutes();
    f.fake.inject_waiter = true;
    assert!(!f.tick(300.).unwrap());
    assert!(f.kills().is_empty());
    assert!(!f.state.exists());
    assert!(!f.paths.here.join("civ6.lock").exists());
    assert!(f.paths.here.join("queue/new-waiter.json").exists());
}

#[test]
fn holder_appearing_during_idle_period_restarts_full_countdown() {
    let mut f = Fixture::new();
    f.wait_five_minutes();
    f.owner(json!({"pid":777}));
    f.fake.alive.insert(777);
    assert!(!f.tick(300.).unwrap());
    f.fake.alive.clear();
    for t in [360., 420., 480., 540., 600.] {
        assert!(!f.tick(t).unwrap());
    }
    assert!(f.tick(660.).unwrap());
}

#[test]
fn missing_observations_and_backwards_clock_restart_countdown() {
    for next in [600., -1.] {
        let mut f = Fixture::new();
        f.wait_five_minutes();
        assert!(!f.tick(next).unwrap());
        assert!(f.kills().is_empty());
        let state: Value = serde_json::from_slice(&fs::read(&f.state).unwrap()).unwrap();
        assert_eq!(state["first_seen"], 1000. + next);
    }
}

#[test]
fn malformed_holder_or_waiter_prevents_cleanup_and_clears_timer() {
    for waiter in [false, true] {
        let mut f = Fixture::new();
        f.wait_five_minutes();
        let path = f.paths.here.join(if waiter {
            "queue/broken.json"
        } else {
            "civ6.lock"
        });
        fs::write(&path, "{").unwrap();
        assert!(f.tick(300.).is_err());
        assert!(f.kills().is_empty());
        assert!(!f.state.exists());
        assert_eq!(fs::read_to_string(path).unwrap(), "{");
    }
}

#[test]
fn unreadable_job_record_does_not_allow_closure() {
    let mut f = Fixture::new();
    f.wait_five_minutes();
    f.job("owner", "running");
    f.owner(json!({"job":"owner"}));
    fs::write(f.paths.jobs.join("owner/job.json"), "{").unwrap();
    assert!(f.tick(300.).is_err());
    assert!(f.kills().is_empty());
    assert!(!f.state.exists());
}

#[test]
fn failed_process_inspection_or_failed_kill_is_not_a_success() {
    let mut f = Fixture::new();
    f.wait_five_minutes();
    f.fake.fail_tasklist = true;
    assert!(f.tick(300.).is_err());
    assert!(f.kills().is_empty());
    assert!(!f.state.exists());
    let mut f = Fixture::new();
    f.wait_five_minutes();
    f.fake.fail_kill = true;
    assert!(f.tick(300.).is_err());
    assert_eq!(f.kills(), CIV_EXECUTABLES);
    assert!(!f.log.exists());
    assert!(!f.state.exists());
    assert!(!f.paths.here.join("civ6.lock").exists());
}
