//! Read only the owner fields. Never serialize a runtime record or its proof key.
use serde::Deserialize;
use std::fs::File;
use std::io::{ErrorKind, Read};
use std::path::Path;

#[derive(Deserialize)]
struct RuntimeOwner {
    pid: u32,
    port: u16,
}

fn record(path: &Path) -> Result<Option<Vec<u8>>, String> {
    let file = match File::open(path) {
        Ok(file) => file,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(None),
        Err(_) => return Err("owner_record_read".into()),
    };
    let mut bytes = Vec::new();
    file.take(4097)
        .read_to_end(&mut bytes)
        .map_err(|_| "owner_record_read")?;
    if bytes.len() > 4096 {
        return Err("owner_record_size".into());
    }
    Ok(Some(bytes))
}

/// A missing legacy PID file may use the runtime owner record. Existing conflicting,
/// malformed or unreadable records never authorize discovery or termination.
pub fn verify(home: &Path, pid: u32, port: u16) -> Result<(), String> {
    if pid == 0 || port == 0 {
        return Err("invalid_owner".into());
    }
    let pid_record = record(&home.join("ocx.pid"))?;
    let has_pid = pid_record.is_some();
    if let Some(bytes) = pid_record {
        let text = std::str::from_utf8(&bytes).map_err(|_| "pid_record_invalid")?;
        let text = text.trim();
        if text.is_empty() || !text.bytes().all(|byte| byte.is_ascii_digit()) {
            return Err("pid_record_invalid".into());
        }
        if text.parse::<u32>() != Ok(pid) {
            return Err("pid_record_disagrees".into());
        }
    }
    let Some(bytes) = record(&home.join("runtime-port.json"))? else {
        return if has_pid {
            Ok(())
        } else {
            Err("owner_record_missing".into())
        };
    };
    let owner: RuntimeOwner =
        serde_json::from_slice(&bytes).map_err(|_| "runtime_record_invalid")?;
    if owner.pid != pid || owner.port != port {
        return Err("runtime_record_disagrees".into());
    }
    Ok(())
}

/// Only absence may use another proof. Re-read after that proof so a conflicting
/// record published during inspection cannot be mistaken for continued absence.
pub fn verify_or_else(
    home: &Path,
    pid: u32,
    port: u16,
    missing: impl FnOnce() -> Result<(), String>,
) -> Result<(), String> {
    match verify(home, pid, port) {
        Err(error) if error == "owner_record_missing" => {
            missing()?;
            match verify(home, pid, port) {
                Err(error) if error == "owner_record_missing" => Ok(()),
                result => result,
            }
        }
        result => result,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};

    static NEXT: AtomicU64 = AtomicU64::new(0);
    struct Home(std::path::PathBuf);
    impl Home {
        fn new() -> Self {
            let path = std::env::temp_dir().join(format!(
                "ocx-owner-test-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            ));
            std::fs::create_dir(&path).unwrap();
            Self(path)
        }
        fn write(&self, name: &str, bytes: impl AsRef<[u8]>) {
            std::fs::write(self.0.join(name), bytes).unwrap();
        }
    }
    impl Drop for Home {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    #[test]
    fn alternative_proof_is_only_for_absence_and_rechecks_new_records() {
        let home = Home::new();
        home.write(
            "serving-runtimes.json",
            br#"{"runtimes":[{"pid":123,"port":10200}]}"#,
        );
        assert_eq!(
            verify(&home.0, 123, 10200),
            Err("owner_record_missing".into())
        );
        assert_eq!(verify_or_else(&home.0, 123, 10200, || Ok(())), Ok(()));
        assert_eq!(
            verify_or_else(&home.0, 123, 10200, || {
                home.write("ocx.pid", "124");
                Ok(())
            }),
            Err("pid_record_disagrees".into())
        );
        let called = std::cell::Cell::new(false);
        assert_eq!(
            verify_or_else(&home.0, 123, 10200, || {
                called.set(true);
                Ok(())
            }),
            Err("pid_record_disagrees".into())
        );
        assert!(!called.get());
    }

    #[test]
    fn missing_pid_uses_matching_runtime_without_exposing_unknown_fields() {
        let home = Home::new();
        home.write(
            "runtime-port.json",
            br#"{"pid":123,"port":10200,"attestationSecret":"synthetic-test-only"}"#,
        );
        assert_eq!(verify(&home.0, 123, 10200), Ok(()));
        assert_eq!(
            verify(&home.0, 124, 10200),
            Err("runtime_record_disagrees".into())
        );
        assert_eq!(
            verify(&home.0, 123, 10201),
            Err("runtime_record_disagrees".into())
        );
    }

    #[test]
    fn existing_pid_is_authoritative_including_conflicts_and_invalid_records() {
        let home = Home::new();
        home.write("runtime-port.json", br#"{"pid":123,"port":10200}"#);
        for raw in ["124", "", "+123", "123x", "0", "4294967296"] {
            home.write("ocx.pid", raw);
            assert!(verify(&home.0, 123, 10200).is_err(), "{raw}");
        }
        home.write("ocx.pid", " 123\n");
        assert_eq!(verify(&home.0, 123, 10200), Ok(()));
        home.write("runtime-port.json", br#"{"pid":124,"port":10200}"#);
        assert_eq!(
            verify(&home.0, 123, 10200),
            Err("runtime_record_disagrees".into())
        );
        std::fs::remove_file(home.0.join("runtime-port.json")).unwrap();
        assert_eq!(verify(&home.0, 123, 10200), Ok(()));
    }

    #[test]
    fn missing_malformed_duplicate_oversized_and_unreadable_records_refuse() {
        let home = Home::new();
        assert_eq!(
            verify(&home.0, 123, 10200),
            Err("owner_record_missing".into())
        );
        for raw in [
            "{}",
            "[]",
            "null",
            "{\"pid\":123,\"pid\":124,\"port\":10200}",
            "{\"pid\":123,\"port\":10200.5}",
            "{\"pid\":123,\"port\":65536}",
        ] {
            home.write("runtime-port.json", raw);
            assert_eq!(
                verify(&home.0, 123, 10200),
                Err("runtime_record_invalid".into())
            );
        }
        home.write("runtime-port.json", vec![b' '; 4097]);
        assert_eq!(verify(&home.0, 123, 10200), Err("owner_record_size".into()));
        std::fs::remove_file(home.0.join("runtime-port.json")).unwrap();
        std::fs::create_dir(home.0.join("runtime-port.json")).unwrap();
        assert_eq!(verify(&home.0, 123, 10200), Err("owner_record_read".into()));
    }
}
