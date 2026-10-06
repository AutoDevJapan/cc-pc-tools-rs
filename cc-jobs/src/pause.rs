use crate::platform::Paused;
use std::{io, path::Path};

#[cfg(not(windows))]
pub fn idle_agent(
    _: u32,
    _: u64,
    _: Option<u64>,
    _: Option<&Path>,
) -> io::Result<Option<Box<dyn Paused>>> {
    Ok(None)
}
#[cfg(not(windows))]
pub fn recover(_: &Path) -> io::Result<()> {
    Ok(())
}
#[cfg(not(windows))]
pub fn supervisor(_: u32, _: u64) -> io::Result<Box<dyn Paused>> {
    Err(io::Error::other("process pause requires Windows"))
}

#[cfg(windows)]
mod native {
    use super::*;
    use std::collections::HashMap;
    use windows_sys::Win32::{
        Foundation::*,
        System::{Diagnostics::ToolHelp::*, Threading::*},
    };
    #[link(name = "ntdll")]
    unsafe extern "system" {
        fn NtSuspendProcess(handle: HANDLE) -> i32;
        fn NtResumeProcess(handle: HANDLE) -> i32;
    }
    struct Handle(HANDLE);
    unsafe impl Send for Handle {}
    impl Drop for Handle {
        fn drop(&mut self) {
            unsafe {
                CloseHandle(self.0);
            }
        }
    }
    struct Frozen {
        handles: Vec<Handle>,
        marker: Option<std::path::PathBuf>,
        armed: Vec<(u32, u64)>,
    }
    fn record(path: &Path, value: &serde_json::Value) -> io::Result<()> {
        let tmp = path.with_extension(format!("{}.tmp", std::process::id()));
        std::fs::write(&tmp, serde_json::to_vec(value).map_err(io::Error::other)?)?;
        std::fs::rename(tmp, path)
    }
    impl Frozen {
        fn record(&self) -> io::Result<()> {
            if let Some(path) = &self.marker {
                record(
                    path,
                    &serde_json::json!({"holder":[std::process::id(),crate::proc_created(std::process::id())],"processes":self.armed,"complete":false}),
                )?;
            }
            Ok(())
        }
    }
    impl Drop for Frozen {
        fn drop(&mut self) {
            for h in self.handles.iter().rev() {
                unsafe {
                    NtResumeProcess(h.0);
                }
            }
            if let Some(path) = &self.marker {
                let _ = record(path, &serde_json::json!({"complete":true}));
            }
        }
    }
    impl Paused for Frozen {
        fn arm_process(&mut self, pid: u32, created: u64) -> io::Result<()> {
            self.armed.push((pid, created));
            self.record()
        }
        fn processes(&self) -> Vec<(u32, u64)> {
            self.handles
                .iter()
                .filter_map(|h| unsafe {
                    let pid = GetProcessId(h.0);
                    let mut ts: [FILETIME; 4] = std::mem::zeroed();
                    (GetProcessTimes(h.0, &mut ts[0], &mut ts[1], &mut ts[2], &mut ts[3]) != 0)
                        .then_some((
                            pid,
                            ((ts[0].dwHighDateTime as u64) << 32) | ts[0].dwLowDateTime as u64,
                        ))
                })
                .collect()
        }
        fn stop(&mut self) -> io::Result<()> {
            for h in self.handles.iter().rev() {
                unsafe {
                    if WaitForSingleObject(h.0, 0) != WAIT_OBJECT_0 && TerminateProcess(h.0, 0) == 0
                    {
                        return Err(io::Error::last_os_error());
                    }
                }
            }
            for h in &self.handles {
                if unsafe { WaitForSingleObject(h.0, 2000) } != WAIT_OBJECT_0 {
                    return Err(io::Error::other("paused process did not exit"));
                }
            }
            Ok(())
        }
    }
    fn freeze(pid: u32, created: u64, frozen: &mut Frozen) -> io::Result<()> {
        unsafe {
            let h = Handle(OpenProcess(
                PROCESS_SUSPEND_RESUME
                    | PROCESS_QUERY_LIMITED_INFORMATION
                    | PROCESS_TERMINATE
                    | PROCESS_SYNCHRONIZE,
                0,
                pid,
            ));
            if h.0.is_null() {
                return Err(io::Error::last_os_error());
            }
            let mut ts: [FILETIME; 4] = std::mem::zeroed();
            if GetProcessTimes(h.0, &mut ts[0], &mut ts[1], &mut ts[2], &mut ts[3]) == 0
                || (((ts[0].dwHighDateTime as u64) << 32) | ts[0].dwLowDateTime as u64) != created
            {
                return Err(io::Error::other("process identity changed before pause"));
            }
            if NtSuspendProcess(h.0) < 0 {
                return Err(io::Error::other("process pause failed"));
            }
            frozen.handles.push(h);
            Ok(())
        }
    }
    #[derive(Clone)]
    struct Process {
        pid: u32,
        parent: u32,
        name: String,
        created: u64,
    }
    fn tree(root: u32, root_created: u64) -> io::Result<Vec<Process>> {
        unsafe {
            let snapshot = Handle(CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0));
            if snapshot.0 == INVALID_HANDLE_VALUE {
                return Err(io::Error::last_os_error());
            }
            let mut entry: PROCESSENTRY32W = std::mem::zeroed();
            entry.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;
            let mut all = Vec::new();
            if Process32FirstW(snapshot.0, &mut entry) == 0 {
                return Err(io::Error::last_os_error());
            }
            loop {
                let n = entry
                    .szExeFile
                    .iter()
                    .position(|v| *v == 0)
                    .unwrap_or(entry.szExeFile.len());
                all.push((
                    entry.th32ProcessID,
                    entry.th32ParentProcessID,
                    String::from_utf16_lossy(&entry.szExeFile[..n]).to_ascii_lowercase(),
                ));
                if Process32NextW(snapshot.0, &mut entry) == 0 {
                    break;
                }
            }
            if crate::proc_created(root) != Some(root_created) {
                return Err(io::Error::other("root process identity changed"));
            }
            let mut found = HashMap::from([(root, root_created)]);
            let mut result = Vec::new();
            loop {
                let before = result.len();
                for (pid, parent, name) in &all {
                    if (*pid == root || found.contains_key(parent))
                        && !result.iter().any(|p: &Process| p.pid == *pid)
                    {
                        let created = crate::proc_created(*pid).ok_or_else(|| {
                            io::Error::other("descendant process identity unavailable")
                        })?;
                        if *pid != root && found.get(parent).is_none_or(|pc| created < *pc) {
                            continue;
                        }
                        found.insert(*pid, created);
                        result.push(Process {
                            pid: *pid,
                            parent: *parent,
                            name: name.clone(),
                            created,
                        });
                    }
                }
                if result.len() == before {
                    break;
                }
            }
            Ok(result)
        }
    }
    fn controllers(processes: &[Process], root: u32, baseline: Option<u64>) -> bool {
        // A second native Codex is a delegated job: let it finish. Leaf Node
        // commands, shells, cargo, live_chain, browsers etc. are tools, not idle controllers.
        processes.iter().filter(|p| p.name == "codex.exe").count() <= 1
            && processes.iter().all(|p| {
                baseline.is_some_and(|at| p.created < at)
                    || match p.name.as_str() {
                        "cmd.exe" => p.pid == root,
                        "node.exe" => {
                            p.pid == root
                                || processes
                                    .iter()
                                    .any(|c| c.parent == p.pid && c.name == "codex.exe")
                                || processes.iter().any(|child| {
                                    child.parent == p.pid && child.name == "node_repl.exe"
                                })
                        }
                        "codex.exe"
                        | "node_repl.exe"
                        | "codex-code-mode-host.exe"
                        | "conhost.exe" => true,
                        _ => false,
                    }
            })
    }
    pub fn idle_agent(
        pid: u32,
        created: u64,
        baseline: Option<u64>,
        marker: Option<&Path>,
    ) -> io::Result<Option<Box<dyn Paused>>> {
        let before = tree(pid, created)?;
        if !controllers(&before, pid, baseline) {
            return Ok(None);
        }
        let mut frozen = Frozen {
            handles: Vec::new(),
            marker: marker.map(Path::to_path_buf),
            armed: before.iter().map(|p| (p.pid, p.created)).collect(),
        };
        frozen.record()?;
        freeze(pid, created, &mut frozen)?;
        for p in &before {
            if p.pid != pid {
                freeze(p.pid, p.created, &mut frozen)?;
            }
        }
        // Freezing and enumerating are separate OS operations. Reject new
        // children, including a command that raced the initial snapshot.
        let after = tree(pid, created)?;
        if after.len() != before.len()
            || !after.iter().all(|p| {
                before
                    .iter()
                    .any(|b| b.pid == p.pid && b.created == p.created)
            })
        {
            return Ok(None);
        }
        Ok(Some(Box::new(frozen)))
    }
    pub fn supervisor(pid: u32, created: u64) -> io::Result<Box<dyn Paused>> {
        let mut frozen = Frozen {
            handles: Vec::new(),
            marker: None,
            armed: Vec::new(),
        };
        freeze(pid, created, &mut frozen)?;
        Ok(Box::new(frozen))
    }
    pub fn recover(path: &Path) -> io::Result<()> {
        let bytes = match std::fs::read(path) {
            Ok(b) => b,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
            Err(e) => return Err(e),
        };
        let value: serde_json::Value = serde_json::from_slice(&bytes).map_err(io::Error::other)?;
        if value["complete"] != false {
            return Ok(());
        }
        let (holder, created): (u32, u64) =
            serde_json::from_value(value["holder"].clone()).map_err(io::Error::other)?;
        if crate::proc_created(holder) == Some(created) {
            return Ok(());
        }
        let processes: Vec<(u32, u64)> =
            serde_json::from_value(value["processes"].clone()).map_err(io::Error::other)?;
        for (pid, created) in processes {
            if crate::proc_created(pid) != Some(created) {
                continue;
            }
            unsafe {
                let h = Handle(OpenProcess(
                    PROCESS_SUSPEND_RESUME | PROCESS_QUERY_LIMITED_INFORMATION,
                    0,
                    pid,
                ));
                if h.0.is_null() {
                    return Err(io::Error::last_os_error());
                }
                let mut ts: [FILETIME; 4] = std::mem::zeroed();
                if GetProcessTimes(h.0, &mut ts[0], &mut ts[1], &mut ts[2], &mut ts[3]) == 0 {
                    return Err(io::Error::last_os_error());
                }
                if (((ts[0].dwHighDateTime as u64) << 32) | ts[0].dwLowDateTime as u64) != created {
                    continue;
                }
                if NtResumeProcess(h.0) < 0 {
                    return Err(io::Error::other("abandoned pause could not resume"));
                }
            }
        }
        record(path, &serde_json::json!({"complete":true}))
    }
}
#[cfg(windows)]
pub use native::{idle_agent, recover, supervisor};
