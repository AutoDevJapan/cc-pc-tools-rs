//! Shared Cargo policy read by both the gate and scheduler.
use serde::{Deserialize, Serialize};
use std::{fs, io, path::Path};
pub const CONFIG_FILE: &str = "cargo-gate.json";
pub const STATE_FILE: &str = "cargo-admission-state.json";
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
        slot_ceiling(self.max_slots, cores)
    }
    pub fn validate(&self) -> io::Result<()> {
        if self.max_slots == Some(0)
            || !(1..=10).contains(&self.jobs_per_build)
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
pub fn slot_ceiling(configured: Option<usize>, cores: usize) -> usize {
    configured
        .unwrap_or((cores / 4).max(1))
        .clamp(1, cores.max(1))
}
impl Config {
    pub fn read(dir: &Path) -> io::Result<Self> {
        let config: Self = match fs::read(dir.join(CONFIG_FILE)) {
            Ok(bytes) => serde_json::from_slice(&bytes)?,
            Err(e) if e.kind() == io::ErrorKind::NotFound => Self::default(),
            Err(e) => return Err(e),
        };
        config.validate()?;
        Ok(config)
    }
}
pub fn configured_ceiling(dir: &Path, cores: usize) -> io::Result<usize> {
    Ok(Config::read(dir)?.max_slots(cores))
}
