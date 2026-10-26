use super::{strict, Host, ReadError, Result};
use std::{
    fs::File,
    io::{self, Read},
    os::windows::{
        fs::{MetadataExt, OpenOptionsExt},
        io::AsRawHandle,
        process::CommandExt,
    },
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
pub struct SystemHost;
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
impl Host for SystemHost {
    fn read(&mut self, path: &Path, limit: usize) -> Result<Vec<u8>> {
        no_links(path)?;
        // Deny write/delete sharing for this read. The second observation still
        // verifies files replaced between reads; it cannot prove earlier history.
        let mut file = File::options()
            .read(true)
            .share_mode(1)
            .open(path)
            .map_err(io_error)?;
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
        Ok(bytes)
    }
    fn guard(&mut self, path: &Path) -> Result<bool> {
        let mut command = Command::new("D:/CC/Tools/client-records/scrub391.exe");
        command
            .arg("--guard-path")
            .arg(path)
            .env_clear()
            .creation_flags(0x08000000)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
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
        let mut child = command.spawn().map_err(io_error)?;
        let out = capture(child.stdout.take().unwrap());
        let err = capture(child.stderr.take().unwrap());
        let deadline = Instant::now() + Duration::from_secs(15);
        let status = loop {
            match child.try_wait() {
                Ok(Some(status)) => break status,
                Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(20)),
                _ => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err(ReadError("guard_unavailable"));
                }
            }
        };
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
        let report = strict::parse(&stdout).map_err(|_| ReadError("guard_unavailable"))?;
        Ok(report["ok"] == true
            && report["access_allowed"] == true
            && report["client_resource"] == false
            && report["classification_unverified"] == false)
    }
    fn now(&self) -> String {
        chrono::Utc::now().to_rfc3339()
    }
}
