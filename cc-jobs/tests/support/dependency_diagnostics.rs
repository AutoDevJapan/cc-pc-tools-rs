//! Bounded, read-only evidence collected after a fixture has already timed out.
//! No commands, command lines, arbitrary log text or prompt bytes are emitted.
use serde_json::{Value, json};
use std::{
    fs::{self, File},
    io::{self, Read, Seek, SeekFrom},
    path::Path,
    time::{Duration, Instant},
};
use windows_sys::Win32::{
    Foundation::*,
    System::{Diagnostics::ToolHelp::*, Threading::*},
};

const MAX_PROCESSES: usize = 64;

struct Probe {
    handle: HANDLE,
    pid: u32,
    parent: u32,
    created: u64,
    image: &'static str,
    threads: u32,
}
impl Drop for Probe {
    fn drop(&mut self) {
        unsafe { CloseHandle(self.handle) };
    }
}
fn ticks(t: FILETIME) -> u64 {
    ((t.dwHighDateTime as u64) << 32) | t.dwLowDateTime as u64
}
impl Probe {
    fn open(pid: u32, expected: Option<u64>, entry: Option<&PROCESSENTRY32W>) -> Option<Self> {
        unsafe {
            let handle = OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                0,
                pid,
            );
            if handle.is_null() {
                return None;
            }
            let mut times = [std::mem::zeroed::<FILETIME>(); 4];
            if GetProcessTimes(
                handle,
                &mut times[0],
                &mut times[1],
                &mut times[2],
                &mut times[3],
            ) == 0
                || expected.is_some_and(|value| value != ticks(times[0]))
            {
                CloseHandle(handle);
                return None;
            }
            let name = entry.map(|entry| {
                let end = entry
                    .szExeFile
                    .iter()
                    .position(|c| *c == 0)
                    .unwrap_or(entry.szExeFile.len());
                String::from_utf16_lossy(&entry.szExeFile[..end]).to_ascii_lowercase()
            });
            let image = match name.as_deref() {
                Some("cmd.exe") => "cmd.exe",
                Some("fixture.exe") => "fixture.exe",
                Some("bun.exe") => "bun.exe",
                Some("conhost.exe") => "conhost.exe",
                Some("werfault.exe") => "werfault.exe",
                _ => "other",
            };
            Some(Self {
                handle,
                pid,
                parent: entry.map_or(0, |entry| entry.th32ParentProcessID),
                created: ticks(times[0]),
                image,
                threads: entry.map_or(0, |entry| entry.cntThreads),
            })
        }
    }
    fn sample(&self) -> Value {
        unsafe {
            let mut times = [std::mem::zeroed::<FILETIME>(); 4];
            if GetProcessTimes(
                self.handle,
                &mut times[0],
                &mut times[1],
                &mut times[2],
                &mut times[3],
            ) == 0
            {
                return json!({"pid":self.pid,"created":self.created,"io_error":GetLastError()});
            }
            json!({"pid":self.pid,"ppid":self.parent,"created":self.created,"image":self.image,
                "threads":self.threads,"live":WaitForSingleObject(self.handle,0)==WAIT_TIMEOUT,
                "kernel_100ns":ticks(times[2]),"user_100ns":ticks(times[3])})
        }
    }
}
fn entries() -> io::Result<Vec<PROCESSENTRY32W>> {
    unsafe {
        let handle = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
        if handle == INVALID_HANDLE_VALUE {
            return Err(io::Error::last_os_error());
        }
        struct Snapshot(HANDLE);
        impl Drop for Snapshot {
            fn drop(&mut self) {
                unsafe { CloseHandle(self.0) };
            }
        }
        let _owner = Snapshot(handle);
        let mut entry: PROCESSENTRY32W = std::mem::zeroed();
        entry.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;
        if Process32FirstW(handle, &mut entry) == 0 {
            return Err(io::Error::last_os_error());
        }
        let mut rows = vec![entry];
        while Process32NextW(handle, &mut entry) != 0 {
            if rows.len() == 8192 {
                return Err(io::Error::other("process snapshot bound exceeded"));
            }
            rows.push(entry);
        }
        if GetLastError() != ERROR_NO_MORE_FILES {
            return Err(io::Error::last_os_error());
        }
        Ok(rows)
    }
}
fn discover(rows: &[PROCESSENTRY32W], probes: &mut Vec<Probe>) {
    // Retaining every selected process prevents PID reuse while descendants
    // and the second CPU sample are observed. Older children are not adopted.
    loop {
        let before = probes.len();
        for row in rows {
            if probes.len() == MAX_PROCESSES {
                return;
            }
            if probes.iter().any(|p| p.pid == row.th32ProcessID) {
                continue;
            }
            if let Some(parent) = probes.iter().find(|p| p.pid == row.th32ParentProcessID)
                && let Some(child) = Probe::open(row.th32ProcessID, None, Some(row))
                && child.created >= parent.created
            {
                probes.push(child);
            }
        }
        if before == probes.len() {
            return;
        }
    }
}
fn prefix(path: &Path, limit: u64) -> io::Result<Vec<u8>> {
    let mut bytes = Vec::new();
    File::open(path)?.take(limit).read_to_end(&mut bytes)?;
    Ok(bytes)
}
fn file_state(path: &Path) -> Value {
    match fs::metadata(path) {
        Ok(metadata) => json!({"bytes":metadata.len()}),
        Err(error) if error.kind() == io::ErrorKind::NotFound => json!({"missing":true}),
        Err(error) => json!({"io_error":error.raw_os_error()}),
    }
}
fn log_state(path: &Path) -> Value {
    let result = (|| -> io::Result<Value> {
        let mut file = File::open(path)?;
        let length = file.metadata()?.len();
        file.seek(SeekFrom::Start(length.saturating_sub(1024)))?;
        let mut tail = Vec::new();
        file.take(1024).read_to_end(&mut tail)?;
        let text = String::from_utf8_lossy(&tail).to_ascii_lowercase();
        let markers = [
            "panic",
            "plugin env_vars",
            "breakaway",
            "history scan metrics",
            "timeout",
            "os error 5",
            "os error 32",
            "warning",
        ]
        .into_iter()
        .filter(|marker| text.contains(marker))
        .collect::<Vec<_>>();
        Ok(json!({"bytes":length,"tail_bytes":tail.len(),"markers":markers}))
    })();
    match result {
        Ok(value) => value,
        Err(error) if error.kind() == io::ErrorKind::NotFound => json!({"missing":true}),
        Err(error) => json!({"io_error":error.raw_os_error()}),
    }
}
pub(super) fn capture(root: &Path, identities: &[(u32, u64)]) -> Value {
    let listing = entries();
    let rows = listing.as_deref().unwrap_or(&[]);
    let mut probes = identities
        .iter()
        .take(MAX_PROCESSES)
        .filter_map(|(pid, created)| {
            Probe::open(
                *pid,
                Some(*created),
                rows.iter().find(|row| row.th32ProcessID == *pid),
            )
        })
        .collect::<Vec<_>>();
    let unavailable = identities.len().saturating_sub(probes.len());
    discover(rows, &mut probes);
    let first = probes.iter().map(Probe::sample).collect::<Vec<_>>();
    let at = Instant::now();
    if !probes.is_empty() {
        std::thread::sleep(Duration::from_secs(1));
    }
    let second_listing = entries();
    if let Ok(rows) = &second_listing {
        for probe in &mut probes {
            if let Some(row) = rows.iter().find(|row| row.th32ProcessID == probe.pid) {
                probe.threads = row.cntThreads;
            }
        }
        discover(rows, &mut probes);
    }
    let second = probes.iter().map(Probe::sample).collect::<Vec<_>>();
    let rec = root.join("rec");
    let input = prefix(&rec.join("stdin.bin"), 64).unwrap_or_default();
    let stdin_role = if input.starts_with(b"other root") {
        "other"
    } else if input.starts_with(b"dependency") {
        "child"
    } else if input.starts_with(b"hi") {
        "parent"
    } else {
        "unknown"
    };
    let call: Value = prefix(&rec.join("call.json"), 8192)
        .ok()
        .and_then(|bytes| serde_json::from_slice(&bytes).ok())
        .unwrap_or(Value::Null);
    let cwd = call["cwd"].as_str().unwrap_or("").replace('\\', "/");
    let cwd_role = |name: &str| {
        cwd.eq_ignore_ascii_case(&root.join(name).to_string_lossy().replace('\\', "/"))
    };
    let call_cwd_role = if cwd_role("other-work") {
        "other"
    } else if cwd_role("parent-work") {
        "parent"
    } else {
        "unknown"
    };
    let sleep_pid = prefix(&rec.join("sleep.pid"), 32)
        .ok()
        .and_then(|bytes| String::from_utf8(bytes).ok())
        .and_then(|text| text.trim().parse::<u32>().ok());
    let mut jobs = Vec::new();
    if let Ok(entries) = fs::read_dir(root.join("tool/jobs")) {
        let mut paths = entries
            .filter_map(Result::ok)
            .map(|entry| entry.path())
            .collect::<Vec<_>>();
        paths.sort();
        for path in paths.into_iter().take(16) {
            if let Some(job) = prefix(&path.join("job.json"), 65536)
                .ok()
                .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok())
            {
                let role = if job["parent_job_id"].as_str().is_some_and(|s| !s.is_empty()) {
                    "child"
                } else {
                    "root"
                };
                jobs.push(json!({"role":role,"pid":job["pid"],"agent_pid":job["codex_pid"],
                    "events":file_state(&path.join("events.jsonl")),"stderr":log_state(&path.join("stderr.log")),"supervisor":log_state(&path.join("supervisor.log"))}));
            }
        }
    }
    json!({"samples":[first,second],"sample_interval_ms":at.elapsed().as_millis(),"unavailable_roots":unavailable,
        "process_limit_reached":probes.len()==MAX_PROCESSES,"first_snapshot_ok":listing.is_ok(),"second_snapshot_ok":second_listing.is_ok(),"first_snapshot_error":listing.err().and_then(|e|e.raw_os_error()),
        "second_snapshot_error":second_listing.err().and_then(|e|e.raw_os_error()),
        "stdin_role":stdin_role,"stdin_file":file_state(&rec.join("stdin.bin")),
        "call_cwd_role":call_cwd_role,"call_file":file_state(&rec.join("call.json")),"sleep_pid":sleep_pid,"jobs":jobs})
}
