//! Shared ceiling for the Cargo gate and the scheduler's non-reserving probe.
use std::{fs, io, path::Path};
pub const CONFIG_FILE: &str = "cargo-gate.json";
pub fn slot_ceiling(configured: Option<usize>, cores: usize) -> usize {
    configured
        .unwrap_or((cores / 4).max(1))
        .clamp(1, cores.max(1))
}
pub fn configured_ceiling(dir: &Path, cores: usize) -> io::Result<usize> {
    let config: serde_json::Value = match fs::read(dir.join(CONFIG_FILE)) {
        Ok(bytes) => serde_json::from_slice(&bytes)?,
        Err(e) if e.kind() == io::ErrorKind::NotFound => serde_json::json!({}),
        Err(e) => return Err(e),
    };
    if !config.is_object() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "cargo-gate.json must be an object",
        ));
    }
    let max = match config.get("max_slots") {
        None | Some(serde_json::Value::Null) => None,
        Some(value) => Some(
            value
                .as_u64()
                .and_then(|n| usize::try_from(n).ok())
                .filter(|n| *n > 0)
                .ok_or_else(|| {
                    io::Error::new(
                        io::ErrorKind::InvalidData,
                        "invalid max_slots in cargo-gate.json",
                    )
                })?,
        ),
    };
    Ok(slot_ceiling(max, cores))
}
