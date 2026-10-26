use super::{strict, Host, ReadError, Result};
use std::{
    fs::File,
    io::{self, Read},
    os::windows::{fs::MetadataExt, io::AsRawHandle, process::CommandExt},
    path::Path,
    process::{Command, Stdio},
    sync::mpsc,
    thread,
    time::{Duration, Instant},
};
use windows_sys::Win32::Storage::FileSystem::{
    GetFileInformationByHandle, BY_HANDLE_FILE_INFORMATION,
};

/// The normal local host: fixed guard, no profile or environment-selected data roots.
pub struct SystemHost {
    deadline: Instant,
}
impl Default for SystemHost {
    fn default() -> Self {
        Self {
            deadline: Instant::now() + Duration::from_secs(45),
        }
    }
}
impl SystemHost {
    fn check_deadline(&self) -> Result<()> {
        if Instant::now() >= self.deadline {
            Err(ReadError("query_deadline"))
        } else {
            Ok(())
        }
    }
}
fn io_error(e: io::Error) -> ReadError {
    ReadError(match e.kind() {
        io::ErrorKind::NotFound => "not_found",
        io::ErrorKind::PermissionDenied => "permission_denied",
        _ => "io_error",
    })
}
fn no_links(path: &Path) -> Result<()> {
    for p in path.ancestors() {
        let meta = std::fs::symlink_metadata(p).map_err(io_error)?;
        if meta.file_attributes() & 0x400 != 0 {
            return Err(ReadError("link_rejected"));
        }
    }
    Ok(())
}
fn capture(stream: impl Read + Send + 'static) -> mpsc::Receiver<io::Result<Vec<u8>>> {
    let (tx, rx) = mpsc::channel();
    thread::spawn(move || {
        let mut bytes = Vec::new();
        let result = stream.take(16385).read_to_end(&mut bytes).map(|_| bytes);
        let _ = tx.send(result);
    });
    rx
}
fn guard_verdict(stdout: &[u8]) -> Result<bool> {
    let report = strict::parse(stdout).map_err(|_| ReadError("guard_unavailable"))?;
    if report["ok"] != true {
        return Err(ReadError("guard_unavailable"));
    }
    match (
        report["access_allowed"].as_bool(),
        report["client_resource"].as_bool(),
        report["classification_unverified"].as_bool(),
    ) {
        (Some(true), Some(false), Some(false)) => Ok(true),
        (Some(false), Some(true), Some(false)) => Ok(false),
        (Some(false), Some(true), Some(true)) => Err(ReadError("classification_unverified")),
        _ => Err(ReadError("guard_unavailable")),
    }
}
impl Host for SystemHost {
    fn read(&mut self, path: &Path, limit: usize) -> Result<Vec<u8>> {
        self.check_deadline()?;
        no_links(path)?;
        // Use the ordinary READ|WRITE|DELETE sharing contract. A read-only API
        // must not make the existing snapshot writer's atomic replace fail.
        let mut file = File::open(path).map_err(io_error)?;
        if !file.metadata().map_err(io_error)?.is_file() {
            return Err(ReadError("invalid_record"));
        }
        let mut info: BY_HANDLE_FILE_INFORMATION = unsafe { std::mem::zeroed() };
        if unsafe { GetFileInformationByHandle(file.as_raw_handle(), &mut info) } == 0 {
            return Err(io_error(io::Error::last_os_error()));
        }
        if info.nNumberOfLinks != 1 {
            return Err(ReadError("link_rejected"));
        }
        let mut bytes = Vec::new();
        (&mut file)
            .take(limit as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(io_error)?;
        if bytes.len() > limit {
            return Err(ReadError("too_large"));
        }
        no_links(path)?;
        self.check_deadline()?;
        Ok(bytes)
    }
    fn guard(&mut self, path: &Path) -> Result<bool> {
        self.check_deadline()?;
        let mut command = Command::new("D:/CC/Tools/client-records/scrub391.exe");
        command
            .arg("--guard-path")
            .arg(path)
            .env_clear()
            .creation_flags(0x08000000)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        // The installed guard verifies Git origins through `git`. Keep that
        // normal dependency available even when the caller has an empty PATH.
        // Never inherit an arbitrary tool search path or a different host/profile.
        command.env("PATH", "C:/Program Files/Git/cmd");
        for key in [
            "SYSTEMROOT",
            "WINDIR",
            "USERPROFILE",
            "LOCALAPPDATA",
            "APPDATA",
            "TEMP",
            "TMP",
        ] {
            if let Some(value) = std::env::var_os(key) {
                command.env(key, value);
            }
        }
        // Reuse the existing suspended-spawn/Job Object implementation so the
        // fixed guard's Git children cannot escape timeout cleanup by racing spawn.
        let (mut child, group) = cc_ask::ProcessGroup::spawn(&mut command)
            .map_err(|_| ReadError("guard_unavailable"))?;
        let out = capture(child.stdout.take().unwrap());
        let err = capture(child.stderr.take().unwrap());
        let deadline = self.deadline.min(Instant::now() + Duration::from_secs(15));
        let status = loop {
            match child.try_wait() {
                Ok(Some(status)) => break status,
                Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(20)),
                _ => {
                    group.stop(&mut child);
                    let _ = child.wait();
                    self.check_deadline()?;
                    return Err(ReadError("guard_unavailable"));
                }
            }
        };
        // The fixed guard does not intentionally leave background services.
        // Also close inherited pipes held by its descendants after root exit.
        group.stop(&mut child);
        let _ = child.wait();
        let stdout = out
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .map_err(|_| ReadError("guard_unavailable"))?
            .map_err(io_error)?;
        let stderr = err
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .map_err(|_| ReadError("guard_unavailable"))?
            .map_err(io_error)?;
        if !status.success() || stdout.len() > 16384 || stderr.len() > 16384 {
            return Err(ReadError("guard_unavailable"));
        }
        self.check_deadline()?;
        guard_verdict(&stdout)
    }
    fn now(&self) -> String {
        chrono::Utc::now().to_rfc3339()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn guard_classification_failure_is_not_an_observed_permission_deny() {
        assert_eq!(guard_verdict(br#"{"ok":true,"access_allowed":true,"client_resource":false,"classification_unverified":false}"#),Ok(true));
        assert_eq!(guard_verdict(br#"{"ok":true,"access_allowed":false,"client_resource":true,"classification_unverified":false}"#),Ok(false));
        assert_eq!(guard_verdict(br#"{"ok":true,"access_allowed":false,"client_resource":true,"classification_unverified":true}"#),Err(ReadError("classification_unverified")));
        for bytes in [br#"{}"#.as_slice(),br#"{"ok":true,"access_allowed":true,"client_resource":true,"classification_unverified":false}"#] {
            assert_eq!(guard_verdict(bytes),Err(ReadError("guard_unavailable")));
        }
    }
    #[test]
    fn query_deadline_is_shared_by_reads_and_guards() {
        let mut host = SystemHost {
            deadline: Instant::now(),
        };
        assert_eq!(
            host.read(Path::new("D:/never-read"), 10).unwrap_err(),
            ReadError("query_deadline")
        );
        assert_eq!(
            host.guard(Path::new("D:/never-spawn")).unwrap_err(),
            ReadError("query_deadline")
        );
    }
}
