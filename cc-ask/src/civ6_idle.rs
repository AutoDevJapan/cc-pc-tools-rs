//! Release an abandoned Civ6 after five minutes without a holder or live waiter.
use crate::core::{atomic_json, timestamp, ByteLock, Context, Effects, Result};
use cc_live::{chain, Call, Output, Paths, Runner};
use serde_json::{json, Value};
use std::{
    fs::{self, File, OpenOptions},
    io::{ErrorKind, Read, Seek, SeekFrom, Write},
    path::{Path, PathBuf},
    time::{Duration, Instant},
};

pub const IDLE_SECONDS: f64 = 300.0;

struct EffectsRunner<'a> {
    effects: &'a mut dyn Effects,
    start: Instant,
}
impl Runner for EffectsRunner<'_> {
    fn run(&mut self, call: Call) -> Result<Output> {
        debug_assert_eq!(call.creationflags, cc_live::process::CREATE_NO_WINDOW);
        let out = self
            .effects
            .run(&call.args, call.timeout.unwrap_or(30.0).ceil() as u64)?;
        Ok(Output {
            code: out.code,
            stdout: out.stdout,
            stderr: out.stderr,
        })
    }
    fn now(&mut self) -> f64 {
        self.effects.now()
    }
    fn monotonic(&mut self) -> f64 {
        self.start.elapsed().as_secs_f64()
    }
    fn sleep(&mut self, seconds: f64) -> Result<()> {
        std::thread::sleep(crate::tick_runtime::timeout(Duration::from_secs_f64(
            seconds,
        ))?);
        crate::tick_runtime::check()
    }
}

pub fn cmd_civ6_idle(ctx: &mut Context) -> Result<()> {
    let paths = Paths {
        here: ctx.civ6_home.clone(),
        jobs: ctx.jobs.join("jobs"),
        ..Paths::default()
    };
    check_idle(
        &mut EffectsRunner {
            effects: ctx.effects.as_mut(),
            start: Instant::now(),
        },
        &paths,
        &ctx.here.join("civ6-idle.json"),
    )
    .map(|_| ())
}

/// Runs before dispatch, has no Discord effects, and leaves active chains alone.
pub fn cmd_civ6_fairness(ctx: &mut Context) -> Result<()> {
    let paths = Paths {
        here: ctx.civ6_home.clone(),
        jobs: ctx.jobs.join("jobs"),
        ..Paths::default()
    };
    check_fairness(
        &mut EffectsRunner {
            effects: ctx.effects.as_mut(),
            start: Instant::now(),
        },
        &paths,
        &ctx.todo,
    )
    .map(|_| ())
}
pub fn check_fairness(r: &mut impl Runner, p: &Paths, todo: &Path) -> Result<bool> {
    let Some(mut lock) = cc_live::fairness::Lock::open(p)? else {
        return Ok(false);
    };
    if !chain::holder_alive(r, p, &owner_info(lock.info.to_string().as_bytes())?)? {
        return Ok(false);
    }
    if !lock.expired(r, p, todo)? || lock.info["live_chain"] == true {
        return Ok(false);
    }
    let previous = lock.info.clone();
    let latest = cc_live::fairness::newest_artifact_step(p)?;
    let may_close = latest.is_some_and(|t| r.now() - t >= cc_live::fairness::STEP_IDLE_SECONDS);
    // Install our live PID under the protected handle before any closure/removal.
    lock.info = json!({"pid":std::process::id(), "purpose":"Civ6 fairness cleanup"});
    lock.save()?;
    let guard = Civ6Guard {
        path: p.here.join("civ6.lock"),
        file: Some(lock.file),
        owned: true,
        reclaimed: false,
    };
    let closed = if may_close {
        chain::close_civ6(r)
    } else {
        Ok(())
    };
    // Persist the revoked owner so its old scheduler allocation cannot block the waiter.
    cc_live::write_json(
        p.here.join("civ6-revoked.json"),
        &json!({"owner":previous,"revoked_at":r.now()}),
    )?;
    guard.release()?;
    r.print(&format!(
        "{} Civ6のロックを回収しました（待機者が現れてから30分経過、ゲーム終了={}）。",
        timestamp(),
        may_close && closed.is_ok()
    ))?;
    closed?;
    Ok(true)
}

fn remove_state(path: &Path) -> Result<()> {
    match fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e.to_string()),
    }
}

fn owner_info(bytes: &[u8]) -> Result<Value> {
    let info: Value = serde_json::from_slice(bytes).map_err(|e| e.to_string())?;
    if info["owner_job"].as_str().is_some_and(|j| !j.is_empty())
        || info["job"].as_str().is_some_and(|j| !j.is_empty())
        || info["pid"].as_u64().is_some_and(|pid| pid > 0)
        || info["pid"]
            .as_str()
            .and_then(|pid| pid.parse::<u64>().ok())
            .is_some_and(|pid| pid > 0)
    {
        Ok(info)
    } else {
        Err("Civ6 owner record has no valid job or pid".into())
    }
}

fn live_holder(r: &mut impl Runner, p: &Paths) -> Result<bool> {
    match fs::read(p.here.join("civ6.lock")) {
        Ok(bytes) => {
            if chain::holder_alive(r, p, &owner_info(&bytes)?)? {
                return Ok(true);
            }
            // Recheck under a handle that denies deletion/writes. Replace the
            // stale owner with our live PID before releasing the handle, so a
            // contender cannot install its own lock in the removal interval.
            let Some(guard) = Civ6Guard::claim(r, p)? else {
                return Ok(true);
            };
            let reclaimed = guard.reclaimed;
            guard.release()?;
            if reclaimed {
                r.print(&format!(
                    "{} 終了済みのCiv6利用者のロックを削除しました。",
                    timestamp()
                ))?;
            }
            Ok(false)
        }
        Err(e) if e.kind() == ErrorKind::NotFound => Ok(false),
        Err(e) => Err(e.to_string()),
    }
}

fn live_waiter(r: &mut impl Runner, p: &Paths) -> Result<bool> {
    let entries = match fs::read_dir(p.here.join("queue")) {
        Ok(entries) => entries,
        Err(e) if e.kind() == ErrorKind::NotFound => return Ok(false),
        Err(e) => return Err(e.to_string()),
    };
    for entry in entries {
        let path = entry.map_err(|e| e.to_string())?.path();
        if path.extension().is_none_or(|ext| ext != "json") {
            continue;
        }
        let bytes = match fs::read(&path) {
            Ok(bytes) => bytes,
            // A waiter removes its record after obtaining the lock.
            Err(e) if e.kind() == ErrorKind::NotFound => continue,
            Err(e) => return Err(e.to_string()),
        };
        if chain::holder_alive(r, p, &owner_info(&bytes)?)? {
            return Ok(true);
        }
    }
    Ok(false)
}

/// Keep the shared lock readable, but deny writes and deletion until cleanup ends.
/// A contender can observe our live PID; it cannot remove a stale record while
/// we inspect it or acquire the lock between our check and taskkill.
struct Civ6Guard {
    path: PathBuf,
    file: Option<File>,
    owned: bool,
    reclaimed: bool,
}
impl Civ6Guard {
    fn claim(r: &mut impl Runner, p: &Paths) -> Result<Option<Self>> {
        let path = p.here.join("civ6.lock");
        let mut opts = OpenOptions::new();
        opts.read(true).write(true);
        #[cfg(windows)]
        {
            use std::os::windows::fs::OpenOptionsExt;
            opts.share_mode(1); // FILE_SHARE_READ: deny write/delete, including other claimants.
        }
        let (file, created) = match opts.clone().create_new(true).open(&path) {
            Ok(file) => (file, true),
            Err(e) if e.kind() == ErrorKind::AlreadyExists => {
                (opts.open(&path).map_err(|e| e.to_string())?, false)
            }
            Err(e) => return Err(e.to_string()),
        };
        let mut guard = Self {
            path,
            file: Some(file),
            owned: created,
            reclaimed: false,
        };
        let file = guard.file.as_mut().unwrap();
        if !created {
            let mut bytes = Vec::new();
            file.read_to_end(&mut bytes).map_err(|e| e.to_string())?;
            if chain::holder_alive(r, p, &owner_info(&bytes)?)? {
                return Ok(None);
            }
            guard.owned = true;
            guard.reclaimed = true;
        }
        file.seek(SeekFrom::Start(0)).map_err(|e| e.to_string())?;
        file.set_len(0).map_err(|e| e.to_string())?;
        file.write_all(
            json!({"pid":std::process::id(), "purpose":"idle Civ6 cleanup"})
                .to_string()
                .as_bytes(),
        )
        .and_then(|_| file.sync_all())
        .map_err(|e| e.to_string())?;
        Ok(Some(guard))
    }

    fn release(mut self) -> Result<()> {
        drop(self.file.take());
        remove_state(&self.path)?;
        self.owned = false;
        Ok(())
    }
}
impl Drop for Civ6Guard {
    fn drop(&mut self) {
        drop(self.file.take());
        // Until removal the record names this still-live process, so compliant
        // contenders cannot steal it in the brief interval after handle close.
        if self.owned {
            let _ = fs::remove_file(&self.path);
        }
    }
}

/// State and commands are injectable; this returns true only after confirmed closure.
pub fn check_idle(r: &mut impl Runner, p: &Paths, state: &Path) -> Result<bool> {
    let _serial = ByteLock::acquire(&state.with_extension("lock"), Duration::ZERO)?;
    let result = check_inner(r, p, state);
    // An uncertain observation breaks the consecutive idle period.
    if result.is_err() {
        remove_state(state)?;
    }
    result
}

fn check_inner(r: &mut impl Runner, p: &Paths, state: &Path) -> Result<bool> {
    // Retire dead locks even when the game is absent or a live waiter exists.
    let holder = live_holder(r, p)?;
    if !chain::civ6_running(r)? || holder || live_waiter(r, p)? {
        remove_state(state)?;
        return Ok(false);
    }
    let now = r.now();
    let prior = match fs::read(state) {
        Ok(bytes) => serde_json::from_slice::<Value>(&bytes).ok(),
        Err(e) if e.kind() == ErrorKind::NotFound => None,
        Err(e) => return Err(e.to_string()),
    };
    let first_seen = prior
        .and_then(|v| {
            let first = v["first_seen"].as_f64()?;
            let last = v["last_seen"].as_f64()?;
            // Restart after a missed observation or a backwards wall-clock jump.
            (first <= last && last <= now && now - last <= 120.0).then_some(first)
        })
        .unwrap_or(now);
    atomic_json(state, &json!({"first_seen":first_seen, "last_seen":now}), 0)?;
    if now - first_seen < IDLE_SECONDS {
        return Ok(false);
    }

    let Some(_guard) = Civ6Guard::claim(r, p)? else {
        remove_state(state)?;
        return Ok(false);
    };
    // Holding the shared lock prevents a scenario from starting during closure.
    if live_waiter(r, p)? || !chain::civ6_running(r)? {
        remove_state(state)?;
        return Ok(false);
    }
    chain::close_civ6(r)?;
    remove_state(state)?;
    // cmd.exe holds tick.log without sharing writes while redirecting stdout.
    // Use the existing redirected stream instead of reopening that file.
    r.print(&format!(
        "{} Civ6を終了しました（利用者と待機者がいない状態が5分継続）。",
        timestamp()
    ))?;
    Ok(true)
}
