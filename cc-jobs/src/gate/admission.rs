//! Live Cargo admission. All contenders serialize the occupancy snapshot and
//! reservation; slot byte locks remain compatible with already running gates.
use super::*;
use serde::{Deserialize, Serialize};
use std::{fs, thread, time::Duration};

pub use cc_job_policy::cargo::{CONFIG_FILE, Config, STATE_FILE};
fn read_json<T: serde::de::DeserializeOwned + Default>(path: &Path) -> io::Result<T> {
    match fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(|e| {
            io::Error::new(
                io::ErrorKind::InvalidData,
                format!("{}: {e}", path.display()),
            )
        }),
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(T::default()),
        Err(e) => Err(e),
    }
}
#[derive(Default, Deserialize, Serialize)]
struct State {
    capacity: usize,
    last_admitted: f64,
    #[serde(default)]
    measurement: Option<Sample>,
    #[serde(default)]
    active_at_admission: usize,
    #[serde(default)]
    jobs_at_admission: usize,
    #[serde(default)]
    test_threads_at_admission: usize,
}
#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct Sample {
    pub cpu_percent: f64,
    pub free_memory_gib: f64,
}
/// Unknown/invalid measurements never admit. The middle band never grows live
/// occupancy; a healthy measurement allows one more, bounded by core count.
pub fn capacity(cfg: &Config, cores: usize, previous: usize, sample: Sample) -> Option<usize> {
    if !sample.cpu_percent.is_finite()
        || !(0. ..=100.).contains(&sample.cpu_percent)
        || !sample.free_memory_gib.is_finite()
        || sample.free_memory_gib < cfg.min_free_memory_gib
        || sample.cpu_percent >= cfg.pause_at_cpu_percent
    {
        return None;
    }
    let previous = previous.clamp(1, cfg.max_slots(cores));
    Some(if sample.cpu_percent < cfg.grow_below_cpu_percent {
        previous.saturating_add(1).min(cfg.max_slots(cores))
    } else {
        previous
    })
}
pub fn cpu_percent(before: [u64; 3], after: [u64; 3]) -> Option<f64> {
    let idle = after[0].checked_sub(before[0])?;
    let total = after[1]
        .checked_sub(before[1])?
        .checked_add(after[2].checked_sub(before[2])?)?;
    if total == 0 || idle > total {
        return None;
    }
    Some(100. * (total - idle) as f64 / total as f64)
}
fn native_sample() -> io::Result<Sample> {
    let before = crate::adaptive::machine_resources();
    thread::sleep(Duration::from_millis(250));
    let after = crate::adaptive::machine_resources();
    let cpu = before
        .cpu_times
        .zip(after.cpu_times)
        .and_then(|(a, b)| cpu_percent(a, b));
    match (cpu, after.free_memory_gib) {
        (Some(cpu_percent), Some(free_memory_gib)) => Ok(Sample {
            cpu_percent,
            free_memory_gib,
        }),
        _ => Err(io::Error::new(
            io::ErrorKind::WouldBlock,
            "cannot measure Cargo gate CPU or free memory",
        )),
    }
}
fn now() -> f64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs_f64()
}
fn occupied(error: &io::Error) -> bool {
    matches!(error.raw_os_error(), Some(32) | Some(33))
}
fn warn_history() {
    static WARNED: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
    if !WARNED.swap(true, std::sync::atomic::Ordering::Relaxed) {
        eprintln!(
            "cargo gate: admission history unavailable; using live CPU, memory and slot locks"
        );
    }
}
fn record_state(path: &Path, state: &State) {
    if save_state(path, state).is_err() {
        warn_history();
    }
}
fn save_state(path: &Path, state: &State) -> io::Result<()> {
    // The admission lock serializes writers; a crash can leave the temporary
    // snapshot, but never half a JSON document at the authoritative path.
    let tmp = path.with_extension("json.tmp");
    fs::write(&tmp, serde_json::to_vec(state)?)?;
    fs::rename(tmp, path)
}
pub struct Permit {
    _slot: FileLock,
    pub jobs: usize,
    pub test_threads: usize,
}
/// One admission attempt, with injectable counters for deterministic tests.
/// No environmental slot setting is consulted, including inherited old values.
pub fn try_admit(
    dir: &Path,
    cores: usize,
    sample: impl FnOnce() -> io::Result<Sample>,
) -> io::Result<Option<Permit>> {
    let mut reason = SLOT_WAIT;
    attempt(dir, cores, sample, &mut reason)
}
fn attempt(
    dir: &Path,
    cores: usize,
    sample: impl FnOnce() -> io::Result<Sample>,
    reason: &mut &'static str,
) -> io::Result<Option<Permit>> {
    let _admission = match FileLock::acquire_queue_timeout(
        &dir.join("cargo-admission.lock"),
        Duration::from_secs(5),
    ) {
        Ok(lock) => lock,
        Err(e) if occupied(&e) || e.kind() == io::ErrorKind::TimedOut => return Ok(None),
        Err(e) => return Err(e),
    };
    let cfg = Config::read(dir)
        .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, format!("cargo-gate.json: {e}")))?;
    let state_path = dir.join(STATE_FILE);
    let mut state: State = match read_json(&state_path) {
        Ok(state) => state,
        Err(_) => {
            warn_history();
            State::default()
        }
    };
    let max = cfg.max_slots(cores);
    // Do not hold free slot locks during measurement: old gates do not use
    // our admission lock and must remain able to start in the meantime.
    let measured = sample()?;
    if measured.free_memory_gib < cfg.min_free_memory_gib {
        *reason = "cargoの空きメモリ不足で待機中";
        return Ok(None);
    }
    if measured.cpu_percent >= cfg.pause_at_cpu_percent {
        *reason = "cargoのCPU使用率が高いため待機中";
        return Ok(None);
    }
    // Include locks created by older/larger policies even after max_slots is
    // reduced. Hold every free lock until reservation so legacy contenders
    // cannot invalidate our occupancy snapshot.
    let mut paths = super::slot_paths(dir, max);
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        if entry
            .file_name()
            .to_str()
            .and_then(|s| s.strip_prefix("cargo-build.lock."))
            .is_some_and(|s| s.parse::<usize>().is_ok())
            && !paths.contains(&entry.path())
        {
            paths.push(entry.path());
        }
    }
    let mut active = 0;
    let mut free = Vec::new();
    for path in paths {
        match FileLock::try_acquire(&path) {
            Ok(lock) => free.push((path, lock)),
            Err(e) if occupied(&e) => active += 1,
            Err(e) => return Err(e),
        }
    }
    let at = now();
    // A backwards clock rebases the settling period instead of waiting for
    // wall time to catch up. Nothing is interrupted when capacity falls.
    if state.last_admitted > at {
        state.last_admitted = at;
        record_state(&state_path, &state);
    }
    if active > 0 && at - state.last_admitted < cfg.settle_seconds {
        return Ok(None);
    }
    // Capacity tracks live occupancy. A capacity from a previous quiet period
    // must not authorize growth while today's CPU is in the middle band.
    let Some(capacity) = capacity(&cfg, cores, active, measured) else {
        return Ok(None);
    };
    if active >= capacity {
        return Ok(None);
    }
    let Some((_, slot)) = free.into_iter().find(|(path, _)| {
        path.file_name()
            .and_then(|s| s.to_str())
            .and_then(|s| s.strip_prefix("cargo-build.lock."))
            .and_then(|s| s.parse::<usize>().ok())
            .is_some_and(|n| n < max)
    }) else {
        return Ok(None);
    };
    state.capacity = capacity;
    state.last_admitted = now();
    let jobs = super::jobs_limit(cores, active + 1).min(cfg.jobs_per_build);
    let test_threads = super::jobs_limit(cores, active + 1).min(cfg.test_threads);
    state.measurement = Some(measured);
    state.active_at_admission = active + 1;
    state.jobs_at_admission = jobs;
    state.test_threads_at_admission = test_threads;
    record_state(&state_path, &state);
    Ok(Some(Permit {
        _slot: slot,
        jobs,
        test_threads,
    }))
}
pub fn wait(dir: &Path, env: &BTreeMap<String, String>, cores: usize) -> io::Result<Permit> {
    let mut marker = None;
    let mut marked_reason = "";
    let mut reported_error = String::new();
    loop {
        let mut reason = SLOT_WAIT;
        match attempt(dir, cores, native_sample, &mut reason) {
            Ok(Some(permit)) => return Ok(permit),
            Ok(None) => reported_error.clear(),
            Err(e)
                if matches!(
                    e.kind(),
                    io::ErrorKind::InvalidData | io::ErrorKind::WouldBlock
                ) =>
            {
                reason = if e.kind() == io::ErrorKind::InvalidData {
                    "cargoの設定を読み取れないため待機中"
                } else {
                    "cargoの負荷を再測定するため待機中"
                };
                let message = e.to_string();
                if reported_error != message {
                    eprintln!("cargo gate: {message}; retrying");
                    reported_error = message;
                }
            }
            Err(e) => return Err(e),
        }
        if marked_reason != reason {
            drop(marker.take());
            marker = crate::queue_status::ResourceWait::new(env, reason);
            marked_reason = reason;
        }
        thread::sleep(Duration::from_millis(500));
    }
}
