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
    fn open(pid: u32, expected: Option<u64>, entry: Option<&PROCESSENTRY32W>) -> io::Result<Self> {
        unsafe {
            let handle = OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                0,
                pid,
            );
            if handle.is_null() {
                return Err(io::Error::last_os_error());
            }
            let mut times = [std::mem::zeroed::<FILETIME>(); 4];
            if GetProcessTimes(
                handle,
                &mut times[0],
                &mut times[1],
                &mut times[2],
                &mut times[3],
            ) == 0
            {
                let error = io::Error::last_os_error();
                CloseHandle(handle);
                return Err(error);
            }
            if expected.is_some_and(|value| value != ticks(times[0])) {
                CloseHandle(handle);
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "creation mismatch",
                ));
            }
            let mut probe = Self {
                handle,
                pid,
                parent: 0,
                created: ticks(times[0]),
                image: "other",
                threads: 0,
            };
            if let Some(entry) = entry {
                probe.refresh(entry);
            }
            Ok(probe)
        }
    }
    fn refresh(&mut self, entry: &PROCESSENTRY32W) {
        let end = entry
            .szExeFile
            .iter()
            .position(|c| *c == 0)
            .unwrap_or(entry.szExeFile.len());
        self.image = match String::from_utf16_lossy(&entry.szExeFile[..end])
            .to_ascii_lowercase()
            .as_str()
        {
            "cmd.exe" => "cmd.exe",
            "fixture.exe" => "fixture.exe",
            "bun.exe" => "bun.exe",
            "conhost.exe" => "conhost.exe",
            "werfault.exe" => "werfault.exe",
            _ => "other",
        };
        self.parent = entry.th32ParentProcessID;
        self.threads = entry.cntThreads;
    }
    fn sample(&self) -> Value {
        unsafe {
            let wait = WaitForSingleObject(self.handle, 0);
            let wait_error = (wait == WAIT_FAILED).then(|| GetLastError());
            let live = match wait {
                WAIT_TIMEOUT => Some(true),
                WAIT_OBJECT_0 => Some(false),
                _ => None,
            };
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
            let mut exit = 0;
            let exit_ok = GetExitCodeProcess(self.handle, &mut exit) != 0;
            let exit_error = (!exit_ok).then(|| GetLastError());
            json!({"pid":self.pid,"ppid":self.parent,"created":self.created,"image":self.image,
                "threads":self.threads,"live":live,"wait_result":wait,"wait_error":wait_error,
                "exit_code":if live==Some(false) && exit_ok {Some(exit)} else {None},
                "exit_error":exit_error,"exit_100ns":if live==Some(false) {Some(ticks(times[1]))} else {None},
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
#[derive(Default)]
struct Discovery {
    unopened_children: usize,
    rejected_children: usize,
    open_errors: Vec<Option<i32>>,
    confirmation_errors: Vec<Option<i32>>,
}
fn discover(rows: &[PROCESSENTRY32W], probes: &mut Vec<Probe>, report: &mut Discovery) {
    // First pin candidate identities, then confirm their PPID in a new snapshot.
    // A stale row must not attach a recycled PID to a verified parent.
    loop {
        let mut candidates = Vec::new();
        for row in rows {
            if probes.len() + candidates.len() == MAX_PROCESSES {
                break;
            }
            if probes.iter().any(|p| p.pid == row.th32ProcessID) {
                continue;
            }
            if probes.iter().any(|p| p.pid == row.th32ParentProcessID) {
                match Probe::open(row.th32ProcessID, None, None) {
                    Ok(child) => candidates.push(child),
                    Err(error) => {
                        report.unopened_children += 1;
                        if report.open_errors.len() < MAX_PROCESSES {
                            report.open_errors.push(error.raw_os_error());
                        }
                    }
                }
            }
        }
        if candidates.is_empty() {
            return;
        }
        let current = match entries() {
            Ok(rows) => rows,
            Err(error) => {
                report.confirmation_errors.push(error.raw_os_error());
                return;
            }
        };
        let before = probes.len();
        for mut child in candidates {
            if let Some(row) = current.iter().find(|row| row.th32ProcessID == child.pid)
                && let Some(parent) = probes.iter().find(|p| p.pid == row.th32ParentProcessID)
                && child.created >= parent.created
            {
                child.refresh(row);
                probes.push(child);
            } else {
                report.rejected_children += 1;
            }
        }
        if before == probes.len() || probes.len() == MAX_PROCESSES {
            return;
        }
    }
}
fn prefix(path: &Path, limit: u64) -> io::Result<Vec<u8>> {
    let mut bytes = Vec::new();
    File::open(path)?.take(limit).read_to_end(&mut bytes)?;
    Ok(bytes)
}
fn read_state<T>(result: &io::Result<T>) -> Value {
    match result {
        Ok(_) => json!({"readable":true}),
        Err(error) if error.kind() == io::ErrorKind::NotFound => json!({"missing":true}),
        Err(error) => json!({"unreadable":true,"io_error":error.raw_os_error()}),
    }
}
#[cfg(test)]
pub(super) fn stale_parent_candidate(anchor: (u32, u64), candidate: u32) -> Value {
    let mut probes = vec![Probe::open(anchor.0, Some(anchor.1), None).unwrap()];
    let mut row: PROCESSENTRY32W = unsafe { std::mem::zeroed() };
    row.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;
    row.th32ProcessID = candidate;
    row.th32ParentProcessID = anchor.0;
    discover(&[row], &mut probes, &mut Discovery::default());
    json!(probes.iter().map(|p| p.pid).collect::<Vec<_>>())
}
fn file_state(path: &Path) -> Value {
    match fs::metadata(path) {
        Ok(metadata) => json!({"bytes":metadata.len()}),
        Err(error) if error.kind() == io::ErrorKind::NotFound => json!({"missing":true}),
        Err(error) => json!({"io_error":error.raw_os_error()}),
    }
}
pub(super) fn log_state(path: &Path) -> Value {
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
    let mut root_errors = Vec::new();
    let mut probes = identities
        .iter()
        .take(MAX_PROCESSES)
        .filter_map(|(pid, created)| {
            match Probe::open(
                *pid,
                Some(*created),
                rows.iter().find(|row| row.th32ProcessID == *pid),
            ) {
                Ok(probe) => Some(probe),
                Err(error) => {
                    root_errors.push(error.raw_os_error());
                    None
                }
            }
        })
        .collect::<Vec<_>>();
    let unavailable = identities.len().saturating_sub(probes.len());
    let mut discovery = Discovery::default();
    discover(rows, &mut probes, &mut discovery);
    let first = probes.iter().map(Probe::sample).collect::<Vec<_>>();
    let at = Instant::now();
    if !probes.is_empty() {
        std::thread::sleep(Duration::from_secs(1));
    }
    let second_listing = entries();
    if let Ok(rows) = &second_listing {
        for probe in &mut probes {
            if let Some(row) = rows.iter().find(|row| row.th32ProcessID == probe.pid) {
                probe.refresh(row);
            }
        }
        discover(rows, &mut probes, &mut discovery);
    }
    let second = probes.iter().map(Probe::sample).collect::<Vec<_>>();
    let rec = root.join("rec");
    let input_read = prefix(&rec.join("stdin.bin"), 64);
    let input = input_read.as_deref().unwrap_or_default();
    let stdin_role = if input.starts_with(b"other root") {
        "other"
    } else if input.starts_with(b"dependency") {
        "child"
    } else if input.starts_with(b"hi") {
        "parent"
    } else if input_read
        .as_ref()
        .is_err_and(|e| e.kind() == io::ErrorKind::NotFound)
    {
        "missing"
    } else if input_read.is_err() {
        "unreadable"
    } else {
        "unknown"
    };
    let call_read = prefix(&rec.join("call.json"), 8192);
    let parsed_call = call_read
        .as_ref()
        .ok()
        .and_then(|bytes| serde_json::from_slice::<Value>(bytes).ok());
    let call = parsed_call.as_ref().unwrap_or(&Value::Null);
    let cwd = call["cwd"].as_str().unwrap_or("").replace('\\', "/");
    let cwd_role = |name: &str| {
        cwd.eq_ignore_ascii_case(&root.join(name).to_string_lossy().replace('\\', "/"))
    };
    let call_cwd_role = if cwd_role("other-work") {
        "other"
    } else if cwd_role("parent-work") {
        "parent"
    } else if call_read
        .as_ref()
        .is_err_and(|e| e.kind() == io::ErrorKind::NotFound)
    {
        "missing"
    } else if call_read.is_err() {
        "unreadable"
    } else {
        "unknown"
    };
    let sleep_read = prefix(&rec.join("sleep.pid"), 32);
    let sleep_pid = sleep_read
        .as_ref()
        .ok()
        .cloned()
        .and_then(|bytes| String::from_utf8(bytes).ok())
        .and_then(|text| text.trim().parse::<u32>().ok());
    let mut jobs = Vec::new();
    let mut jobs_skipped = 0;
    let mut jobs_truncated = false;
    let mut job_errors = Vec::new();
    let job_listing = fs::read_dir(root.join("tool/jobs"));
    let job_listing_state = read_state(&job_listing);
    if let Ok(entries) = job_listing {
        let mut paths = entries
            .filter_map(|entry| match entry {
                Ok(entry) => Some(entry),
                Err(error) => {
                    jobs_skipped += 1;
                    if job_errors.len() < 16 {
                        job_errors.push(error.raw_os_error());
                    }
                    None
                }
            })
            .take(8193)
            .map(|entry| entry.path())
            .collect::<Vec<_>>();
        paths.sort();
        jobs_truncated = paths.len() > 16;
        for path in paths.into_iter().take(16) {
            let bytes = prefix(&path.join("job.json"), 65536);
            match bytes
                .as_ref()
                .ok()
                .and_then(|bytes| serde_json::from_slice::<Value>(bytes).ok())
            {
                Some(job) => {
                    let role = if job["parent_job_id"].as_str().is_some_and(|s| !s.is_empty()) {
                        "child"
                    } else {
                        "root"
                    };
                    jobs.push(json!({"role":role,"pid":job["pid"].as_u64(),"agent_pid":job["codex_pid"].as_u64(),
                    "events":file_state(&path.join("events.jsonl")),"stderr":log_state(&path.join("stderr.log")),"supervisor":log_state(&path.join("supervisor.log"))}));
                }
                None => {
                    jobs_skipped += 1;
                    if job_errors.len() < 16 {
                        job_errors.push(bytes.err().and_then(|error| error.raw_os_error()));
                    }
                }
            }
        }
    }
    json!({"samples":[first,second],"sample_interval_ms":at.elapsed().as_millis(),"unavailable_roots":unavailable,
        "process_limit_reached":probes.len()==MAX_PROCESSES,"first_snapshot_ok":listing.is_ok(),"second_snapshot_ok":second_listing.is_ok(),"first_snapshot_error":listing.err().and_then(|e|e.raw_os_error()),
        "second_snapshot_error":second_listing.err().and_then(|e|e.raw_os_error()),
        "root_open_errors":root_errors,"unopened_children":discovery.unopened_children,"child_open_errors":discovery.open_errors,
        "rejected_children":discovery.rejected_children,"confirmation_errors":discovery.confirmation_errors,
        "stdin_role":stdin_role,"stdin_file":file_state(&rec.join("stdin.bin")),"stdin_read":read_state(&input_read),
        "call_cwd_role":call_cwd_role,"call_file":file_state(&rec.join("call.json")),"call_read":read_state(&call_read),"call_parse_ok":parsed_call.is_some(),
        "sleep_pid":sleep_pid,"sleep_read":read_state(&sleep_read),"jobs":jobs,"jobs_listing":job_listing_state,
        "jobs_skipped":jobs_skipped,"jobs_truncated":jobs_truncated,"job_errors":job_errors})
}
