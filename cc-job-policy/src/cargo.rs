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
    pub test_threads: usize,
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
            test_threads: 8,
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
            || !(1..=10).contains(&self.test_threads)
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
        let mut config = config;
        match fs::read(dir.join("queue-config.json")) {
            Ok(bytes) => {
                let value: serde_json::Value = serde_json::from_slice(&bytes)?;
                if !value.is_object() {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        "queue-config.json must be an object",
                    ));
                }
                let limits: QueueCargoLimits = serde_json::from_value(value)?;
                limits.validate()?;
                if let Some(slots) = limits.cargo_slots {
                    config.max_slots = Some(slots);
                }
                if let Some(jobs) = limits.cargo_jobs_per_build {
                    config.jobs_per_build = jobs;
                }
                if let Some(tests) = limits.test_threads {
                    config.test_threads = tests;
                }
            }
            Err(e) if e.kind() == io::ErrorKind::NotFound => (),
            Err(e) => return Err(e),
        }
        config.validate()?;
        Ok(config)
    }
}

/// Optional queue overrides; unrelated job admission fields belong to cc-jobs.
#[derive(Clone, Debug, Default, Deserialize)]
#[serde(default)]
pub struct QueueCargoLimits {
    pub cargo_slots: Option<usize>,
    pub cargo_jobs_per_build: Option<usize>,
    pub test_threads: Option<usize>,
}
impl QueueCargoLimits {
    pub fn validate(&self) -> io::Result<()> {
        if self.cargo_slots == Some(0)
            || self
                .cargo_jobs_per_build
                .is_some_and(|n| !(1..=10).contains(&n))
            || self.test_threads.is_some_and(|n| !(1..=10).contains(&n))
        {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "invalid queue Cargo limits",
            ));
        }
        Ok(())
    }
}
pub fn configured_ceiling(dir: &Path, cores: usize) -> io::Result<usize> {
    Ok(Config::read(dir)?.max_slots(cores))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn queue_limits_override_gate_policy_and_null_restores_it() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(
            dir.path().join(CONFIG_FILE),
            r#"{"max_slots":4,"jobs_per_build":6,"test_threads":2}"#,
        )
        .unwrap();
        let queue = dir.path().join("queue-config.json");
        fs::write(
            &queue,
            r#"{"cargo_slots":2,"cargo_jobs_per_build":8,"test_threads":8,"adaptive":{}}"#,
        )
        .unwrap();
        let cfg = Config::read(dir.path()).unwrap();
        assert_eq!(
            (cfg.max_slots(32), cfg.jobs_per_build, cfg.test_threads),
            (2, 8, 8)
        );
        fs::write(
            &queue,
            r#"{"cargo_slots":null,"cargo_jobs_per_build":null,"test_threads":null}"#,
        )
        .unwrap();
        let cfg = Config::read(dir.path()).unwrap();
        assert_eq!(
            (cfg.max_slots(32), cfg.jobs_per_build, cfg.test_threads),
            (4, 6, 2)
        );
    }
    #[test]
    fn malformed_and_zero_or_excessive_limits_are_rejected() {
        let dir = tempfile::tempdir().unwrap();
        let queue = dir.path().join("queue-config.json");
        for bad in [
            "{broken",
            r#"{"cargo_slots":0}"#,
            r#"{"cargo_jobs_per_build":11}"#,
            r#"{"test_threads":0}"#,
            r#"{"test_threads":11}"#,
            r#"{"test_threads":"8"}"#,
            "[]",
            "null",
        ] {
            fs::write(&queue, bad).unwrap();
            assert!(Config::read(dir.path()).is_err(), "accepted {bad}");
        }
        fs::remove_file(&queue).unwrap();
        assert_eq!(Config::read(dir.path()).unwrap().test_threads, 8);
    }
}
