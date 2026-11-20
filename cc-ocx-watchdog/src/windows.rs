use crate::{Config, Identity};
use serde::Serialize;
use std::ffi::OsStr;
use std::mem::{size_of, zeroed};
use std::os::windows::ffi::OsStrExt;
use std::path::Path;
use windows_sys::Win32::Foundation::{
    CloseHandle, FILETIME, HANDLE, WAIT_ABANDONED, WAIT_OBJECT_0, WAIT_TIMEOUT,
};
use windows_sys::Win32::NetworkManagement::IpHelper::{
    GetExtendedTcpTable, MIB_TCPROW_OWNER_PID, TCP_TABLE_OWNER_PID_LISTENER,
};
use windows_sys::Win32::Networking::WinSock::AF_INET;
use windows_sys::Win32::System::ProcessStatus::{GetProcessMemoryInfo, PROCESS_MEMORY_COUNTERS_EX};
use windows_sys::Win32::System::Threading::{
    CreateMutexW, GetProcessTimes, OpenProcess, PROCESS_QUERY_INFORMATION,
    PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_SYNCHRONIZE, PROCESS_TERMINATE, PROCESS_VM_READ,
    QueryFullProcessImageNameW, ReleaseMutex, TerminateProcess, WaitForSingleObject,
};

pub fn wide(value: impl AsRef<OsStr>) -> Vec<u16> {
    value.as_ref().encode_wide().chain(Some(0)).collect()
}

pub fn current_user_sid() -> Result<String, String> {
    process_user_sid(unsafe { windows_sys::Win32::System::Threading::GetCurrentProcess() })
}
pub(crate) fn process_user_sid(process: HANDLE) -> Result<String, String> {
    use windows_sys::Win32::Foundation::LocalFree;
    use windows_sys::Win32::Security::Authorization::ConvertSidToStringSidW;
    use windows_sys::Win32::Security::{GetTokenInformation, TOKEN_QUERY, TOKEN_USER, TokenUser};
    use windows_sys::Win32::System::Threading::OpenProcessToken;
    let mut token = std::ptr::null_mut();
    if unsafe { OpenProcessToken(process, TOKEN_QUERY, &mut token) } == 0 {
        return Err("process_token".into());
    }
    let result = (|| {
        let mut required = 0;
        unsafe {
            GetTokenInformation(token, TokenUser, std::ptr::null_mut(), 0, &mut required);
        }
        if required < size_of::<TOKEN_USER>() as u32 || required > 64 * 1024 {
            return Err("token_user_size".into());
        }
        // TOKEN_USER contains a pointer: use pointer-aligned storage, not Vec<u8> alignment.
        let mut data = vec![0usize; (required as usize).div_ceil(size_of::<usize>())];
        if unsafe {
            GetTokenInformation(
                token,
                TokenUser,
                data.as_mut_ptr().cast(),
                required,
                &mut required,
            )
        } == 0
        {
            return Err("token_user".into());
        }
        let user = unsafe { &*data.as_ptr().cast::<TOKEN_USER>() };
        let mut sid = std::ptr::null_mut();
        if unsafe { ConvertSidToStringSidW(user.User.Sid, &mut sid) } == 0 {
            return Err("sid_string".into());
        }
        let mut len = 0;
        while len < 512 && unsafe { *sid.add(len) } != 0 {
            len += 1;
        }
        let result = if len == 512 {
            Err("sid_string_size".into())
        } else {
            Ok(String::from_utf16_lossy(unsafe {
                std::slice::from_raw_parts(sid, len)
            }))
        };
        unsafe {
            LocalFree(sid.cast());
        }
        result
    })();
    unsafe {
        CloseHandle(token);
    }
    result
}
fn filetime(t: FILETIME) -> u64 {
    ((t.dwHighDateTime as u64) << 32) | t.dwLowDateTime as u64
}

pub struct MutexGuard(HANDLE);
impl MutexGuard {
    pub fn acquire() -> Result<Self, String> {
        Self::acquire_named("Global\\CC-OcxHealthWatchdog")
    }
    fn acquire_named(name: &str) -> Result<Self, String> {
        let name = wide(name);
        let handle = unsafe { CreateMutexW(std::ptr::null(), 0, name.as_ptr()) };
        if handle.is_null() {
            return Err("create_mutex".into());
        }
        let result = unsafe { WaitForSingleObject(handle, 0) };
        if result != WAIT_OBJECT_0 && result != WAIT_ABANDONED {
            unsafe {
                CloseHandle(handle);
            }
            return Err("another_watchdog_is_running".into());
        }
        Ok(Self(handle))
    }
}
impl Drop for MutexGuard {
    fn drop(&mut self) {
        unsafe {
            ReleaseMutex(self.0);
            CloseHandle(self.0);
        }
    }
}

pub fn listener_pid(port: u16) -> Result<Option<u32>, String> {
    let mut bytes = 0;
    unsafe {
        GetExtendedTcpTable(
            std::ptr::null_mut(),
            &mut bytes,
            0,
            AF_INET as u32,
            TCP_TABLE_OWNER_PID_LISTENER,
            0,
        );
    }
    for _ in 0..3 {
        if bytes == 0 || bytes > 16 * 1024 * 1024 {
            return Err("tcp_table_size".into());
        }
        let mut buffer = vec![0u8; bytes as usize];
        let code = unsafe {
            GetExtendedTcpTable(
                buffer.as_mut_ptr().cast(),
                &mut bytes,
                0,
                AF_INET as u32,
                TCP_TABLE_OWNER_PID_LISTENER,
                0,
            )
        };
        if code == 122 {
            continue;
        } // ERROR_INSUFFICIENT_BUFFER if listeners changed while collecting.
        if code != 0 || buffer.len() < 4 {
            return Err(format!("tcp_table: {code}"));
        }
        let count = u32::from_ne_bytes(buffer[..4].try_into().unwrap()) as usize;
        let stride = size_of::<MIB_TCPROW_OWNER_PID>();
        if count > (buffer.len() - 4) / stride {
            return Err("tcp_table_bounds".into());
        }
        for index in 0..count {
            let row = unsafe {
                std::ptr::read_unaligned(
                    buffer
                        .as_ptr()
                        .add(4 + index * stride)
                        .cast::<MIB_TCPROW_OWNER_PID>(),
                )
            };
            if u16::from_be(row.dwLocalPort as u16) == port
                && (row.dwLocalAddr == u32::from_ne_bytes([127, 0, 0, 1]) || row.dwLocalAddr == 0)
            {
                return Ok(Some(row.dwOwningPid));
            }
        }
        return Ok(None);
    }
    Err("tcp_table_churn".into())
}

pub struct Process {
    handle: HANDLE,
    pub identity: Identity,
}

#[derive(Debug, PartialEq, Eq)]
pub enum TerminationOutcome {
    Confirmed,
    Pending,
}
impl Drop for Process {
    fn drop(&mut self) {
        unsafe {
            CloseHandle(self.handle);
        }
    }
}

#[derive(Debug, Serialize)]
pub struct Metrics {
    pub working_set: usize,
    pub private_bytes: usize,
    pub cpu_100ns: u64,
}

fn same_path(a: &Path, b: &Path) -> bool {
    a.to_string_lossy()
        .replace('/', "\\")
        .eq_ignore_ascii_case(&b.to_string_lossy().replace('/', "\\"))
}

impl Process {
    pub fn open(pid: u32, expected_exe: &Path) -> Result<Self, String> {
        Self::open_with_access(pid, expected_exe, false)
    }
    fn open_with_access(pid: u32, expected_exe: &Path, inspect: bool) -> Result<Self, String> {
        let handle = unsafe {
            OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION
                    | PROCESS_TERMINATE
                    | PROCESS_SYNCHRONIZE
                    | if inspect {
                        PROCESS_QUERY_INFORMATION | PROCESS_VM_READ
                    } else {
                        0
                    },
                0,
                pid,
            )
        };
        if handle.is_null() {
            return Err("open_process".into());
        }
        let mut process = Self {
            handle,
            identity: Identity { pid, born: 0 },
        };
        let mut path = vec![0u16; 32768];
        let mut len = path.len() as u32;
        if unsafe { QueryFullProcessImageNameW(handle, 0, path.as_mut_ptr(), &mut len) } == 0 {
            return Err("process_path".into());
        }
        if !same_path(
            Path::new(&String::from_utf16_lossy(&path[..len as usize])),
            expected_exe,
        ) {
            return Err("unexpected_executable".into());
        }
        process.identity.born = process.times()?.0;
        Ok(process)
    }
    fn times(&self) -> Result<(u64, u64), String> {
        let (mut created, mut exited, mut kernel, mut user): (
            FILETIME,
            FILETIME,
            FILETIME,
            FILETIME,
        ) = unsafe { zeroed() };
        if unsafe {
            GetProcessTimes(
                self.handle,
                &mut created,
                &mut exited,
                &mut kernel,
                &mut user,
            )
        } == 0
        {
            return Err("process_times".into());
        }
        Ok((filetime(created), filetime(kernel) + filetime(user)))
    }
    pub fn age_ms(&self) -> u64 {
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as u64;
        now.saturating_sub(self.identity.born / 10_000 - 11_644_473_600_000)
    }
    pub fn metrics(&self) -> Option<Metrics> {
        let mut metrics: PROCESS_MEMORY_COUNTERS_EX = unsafe { zeroed() };
        metrics.cb = size_of::<PROCESS_MEMORY_COUNTERS_EX>() as u32;
        if unsafe {
            GetProcessMemoryInfo(
                self.handle,
                (&mut metrics as *mut PROCESS_MEMORY_COUNTERS_EX).cast(),
                metrics.cb,
            )
        } == 0
        {
            return None;
        }
        Some(Metrics {
            working_set: metrics.WorkingSetSize,
            private_bytes: metrics.PrivateUsage,
            cpu_100ns: self.times().ok()?.1,
        })
    }
    pub fn terminate_if_still_owner(&self, config: &Config) -> Result<TerminationOutcome, String> {
        self.verify_owner(config)?;
        if listener_pid(config.port)? != Some(self.identity.pid) {
            return Err("listener_changed".into());
        }
        // This is the handle held across all failed probes, not a newly opened PID.
        match unsafe { WaitForSingleObject(self.handle, 0) } {
            WAIT_TIMEOUT => {}
            WAIT_OBJECT_0 => return Err("already_exited".into()),
            _ => return Err("process_wait_failed".into()),
        }
        if unsafe { TerminateProcess(self.handle, 1) } == 0 {
            return Err("terminate_process".into());
        }
        if unsafe { WaitForSingleObject(self.handle, 5000) } != WAIT_OBJECT_0 {
            return Ok(TerminationOutcome::Pending);
        }
        Ok(TerminationOutcome::Confirmed)
    }
    fn verify_owner(&self, config: &Config) -> Result<(), String> {
        crate::ownership::verify_or_else(&config.home, self.identity.pid, config.port, || {
            crate::process_owner::verify(self.handle, self.identity, config)
        })
    }
}

pub fn discover(config: &Config) -> Result<Option<Process>, String> {
    let Some(pid) = listener_pid(config.port)? else {
        return Ok(None);
    };
    // Open first: every proof and the eventual termination refer to this incarnation.
    let process =
        Process::open_with_access(pid, &config.expected_exe, config.expected_cli.is_some())?;
    process.verify_owner(config)?;
    if listener_pid(config.port)? != Some(pid) {
        return Err("listener_changed".into());
    }
    Ok(Some(process))
}

/// The proxy's health answer needs the listener's pid, not a handle to its process.
/// `open_process` means this account could not open the listener's process with the access the
/// watchdog asks for, which includes termination. One cause is a listener that outranks this
/// account, such as an elevated token under a restricted medium-integrity watchdog; the error
/// does not say which. The pid from the TCP table only checks the answer. It is never opened or
/// terminated here, so health is measured while termination stays unavailable. Every other
/// discovery error returns `None`.
pub fn listener_when_unopenable(config: &Config, error: &str) -> Option<u32> {
    if error != "open_process" {
        return None;
    }
    listener_pid(config.port).ok().flatten()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn refuses_wrong_executable_and_releases_mutex() {
        assert!(Process::open(std::process::id(), Path::new("C:\\not-the-current.exe")).is_err());
        let name = format!("Global\\CC-OcxWatchdog-Test-{}", std::process::id());
        let guard = MutexGuard::acquire_named(&name).unwrap();
        let other_name = name.clone();
        // Mutex is recursive on the same thread; exercise contention on another thread.
        assert!(
            std::thread::spawn(move || MutexGuard::acquire_named(&other_name).is_err())
                .join()
                .unwrap()
        );
        drop(guard);
        assert!(MutexGuard::acquire_named(&name).is_ok());
    }
    #[test]
    fn tcp_table_finds_real_listener() {
        let socket = std::net::TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, 0)).unwrap();
        assert_eq!(
            listener_pid(socket.local_addr().unwrap().port()).unwrap(),
            Some(std::process::id())
        );
    }
    #[test]
    fn process_identity_and_metrics_are_live() {
        let p = Process::open(std::process::id(), &std::env::current_exe().unwrap()).unwrap();
        assert!(p.identity.born > 0);
        assert!(p.age_ms() < 60_000);
        assert!(p.metrics().unwrap().private_bytes > 0);
    }
    #[test]
    fn user_sid_is_from_process_token() {
        let sid = current_user_sid().unwrap();
        assert!(sid.starts_with("S-1-"));
        assert!(
            sid.chars()
                .all(|c| c.is_ascii_digit() || c == 'S' || c == '-')
        );
    }
    fn fixture_config(port: u16) -> Config {
        Config {
            home: "C:\\fixture\\home".into(),
            expected_exe: "C:\\fixture\\bun.exe".into(),
            expected_cli: None,
            port,
            task: "fixture".into(),
            log_dir: "C:\\fixture\\log".into(),
            discord_exe: None,
            timeout_ms: 1_000,
            interval_ms: 1_000,
            startup_grace_ms: 0,
            cooldown_ms: 0,
        }
    }
    #[test]
    fn only_open_process_falls_back_to_the_listener_pid() {
        let socket = std::net::TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, 0)).unwrap();
        let config = fixture_config(socket.local_addr().unwrap().port());
        assert_eq!(
            listener_when_unopenable(&config, "open_process"),
            Some(std::process::id())
        );
        for other in [
            "process_path",
            "unexpected_executable",
            "listener_changed",
            "tcp_table_churn",
            "",
        ] {
            assert_eq!(listener_when_unopenable(&config, other), None, "{other}");
        }
        drop(socket);
        assert_eq!(listener_when_unopenable(&config, "open_process"), None);
    }
    #[test]
    fn answer_of_an_unopenable_listener_is_checked_against_its_pid() {
        use std::io::{Read, Write};
        let socket = std::net::TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, 0)).unwrap();
        let port = socket.local_addr().unwrap().port();
        let config = fixture_config(port);
        let pid = std::process::id();
        let server = std::thread::spawn(move || {
            for _ in 0..2 {
                let (mut stream, _) = socket.accept().unwrap();
                let mut request = [0u8; 1024];
                assert!(stream.read(&mut request).unwrap() > 0);
                let body = format!("{{\"service\":\"opencodex\",\"status\":\"ok\",\"pid\":{pid}}}");
                stream
                    .write_all(format!("HTTP/1.0 200 OK\r\n\r\n{body}").as_bytes())
                    .unwrap();
            }
        });
        let listener = listener_when_unopenable(&config, "open_process").unwrap();
        assert_eq!(listener, pid);
        assert!(crate::probe(port, std::time::Duration::from_secs(2), listener).is_ok());
        assert!(crate::probe(port, std::time::Duration::from_secs(2), listener + 1).is_err());
        server.join().unwrap();
    }
    #[test]
    fn a_health_answer_without_a_handle_never_arms_termination() {
        // Health measured from the port alone has no identity, so failures never accumulate.
        let mut streak = crate::FailureStreak::default();
        for _ in 0..10 {
            assert!(!streak.observe(None, false, true));
            assert_eq!(streak.failures(), 0);
        }
    }
}
