//! Live Cargo admission. All contenders serialize the occupancy snapshot and
//! reservation; slot byte locks remain compatible with already running gates.
use super::*;
use serde::{Deserialize, Serialize};
use std::{fs, thread, time::Duration};

pub use cc_job_policy::cargo::CONFIG_FILE;
#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(default, deny_unknown_fields)]
pub struct Config {
    pub max_slots: Option<usize>,
    pub jobs_per_build: usize,
    pub min_free_memory_gib: f64,
    pub grow_below_cpu_percent: f64,
    pub pause_at_cpu_percent: f64,
    /// Give a newly started compiler time to appear in the next CPU sample.
    pub settle_seconds: f64,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            max_slots: None,
            jobs_per_build: 10,
            min_free_memory_gib: 16.,
            grow_below_cpu_percent: 75.,
            pause_at_cpu_percent: 90.,
            settle_seconds: 2.,
        }
    }
}
impl Config {
    pub fn max_slots(&self, cores: usize) -> usize {
        cc_job_policy::cargo::slot_ceiling(self.max_slots, cores)
    }
    fn validate(&self) -> io::Result<()> {
        if self.max_slots == Some(0)
            || self.jobs_per_build == 0
            || !self.min_free_memory_gib.is_finite()
            || self.min_free_memory_gib < 0.
            || !self.grow_below_cpu_percent.is_finite()
            || !(0. ..=100.).contains(&self.grow_below_cpu_percent)
            || !self.pause_at_cpu_percent.is_finite()
            || !(self.grow_below_cpu_percent..=100.).contains(&self.pause_at_cpu_percent)
            || !self.settle_seconds.is_finite()
            || !(0. ..=60.).contains(&self.settle_seconds)
        {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "invalid cargo-gate.json policy",
            ));
        }
        Ok(())
    }
}
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
}
#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct Sample {
    pub cpu_percent: f64,
    pub free_memory_gib: f64,
}
/// Unknown/invalid measurements never admit. The middle band keeps the last
/// capacity; a healthy measurement allows growth by one, bounded by core count.
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
        _ => Err(io::Error::other(
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
}
/// One admission attempt, with injectable counters for deterministic tests.
/// No environmental slot setting is consulted, including inherited old values.
pub fn try_admit(
    dir: &Path,
    cores: usize,
    sample: impl FnOnce() -> io::Result<Sample>,
) -> io::Result<Option<Permit>> {
    let _admission = match FileLock::acquire_queue_timeout(
        &dir.join("cargo-admission.lock"),
        Duration::from_secs(5),
    ) {
        Ok(lock) => lock,
        Err(e) if occupied(&e) || e.kind() == io::ErrorKind::TimedOut => return Ok(None),
        Err(e) => return Err(e),
    };
    let cfg: Config = read_json(&dir.join(CONFIG_FILE))?;
    cfg.validate()?;
    let state_path = dir.join("cargo-admission-state.json");
    let mut state: State = match read_json(&state_path) {
        Ok(state) => state,
        Err(e) if e.kind() == io::ErrorKind::InvalidData => {
            eprintln!("cargo gate: invalid admission history; rebuilding from live counters");
            State::default()
        }
        Err(e) => return Err(e),
    };
    if !state.last_admitted.is_finite() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "invalid Cargo admission state",
        ));
    }
    let max = cfg.max_slots(cores);
    // Do not hold free slot locks during measurement: old gates do not use
    // our admission lock and must remain able to start in the meantime.
    let measured = sample()?;
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
        save_state(&state_path, &state)?;
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
    state.measurement = Some(measured);
    state.active_at_admission = active + 1;
    state.jobs_at_admission = jobs;
    save_state(&state_path, &state)?;
    Ok(Some(Permit { _slot: slot, jobs }))
}
pub fn wait(dir: &Path, env: &BTreeMap<String, String>, cores: usize) -> io::Result<Permit> {
    let mut marker = None;
    loop {
        if let Some(permit) = try_admit(dir, cores, native_sample)? {
            return Ok(permit);
        }
        if marker.is_none() {
            marker = crate::queue_status::ResourceWait::new(env, SLOT_WAIT);
        }
        thread::sleep(Duration::from_millis(500));
    }
}
