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
        fn NtQueryInformationProcess(
            handle: HANDLE,
            class: u32,
            buffer: *mut std::ffi::c_void,
            length: u32,
            returned: *mut u32,
        ) -> i32;
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
        resume_on_drop: bool,
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
            if !self.resume_on_drop {
                return;
            }
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
        fn keep_paused(&mut self) {
            self.resume_on_drop = false;
        }
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
        browser_controller: bool,
    }
    // Read only from a handle whose creation time was checked. Never log argv:
    // CUA bootstraps can contain private paths and connection credentials.
    fn browser_controller(handle: HANDLE, name: &str) -> bool {
        let Some(local) = std::env::var_os("LOCALAPPDATA") else {
            return false;
        };
        let mut path = vec![0u16; 32768];
        let mut size = path.len() as u32;
        if unsafe { QueryFullProcessImageNameW(handle, 0, path.as_mut_ptr(), &mut size) } == 0 {
            return false;
        }
        let path = std::path::PathBuf::from(String::from_utf16_lossy(&path[..size as usize]));
        let codex = std::path::PathBuf::from(local).join("OpenAI/Codex");
        if !matches!(name, "node.exe" | "codex.exe") {
            return false;
        }
        command_line(handle).is_some_and(|cmd| browser_invocation(&path, &codex, name, &cmd))
    }
    fn command_line(handle: HANDLE) -> Option<String> {
        let mut needed = 0;
        unsafe {
            NtQueryInformationProcess(handle, 60, std::ptr::null_mut(), 0, &mut needed);
        }
        if !(16..=1024 * 1024).contains(&needed) {
            return None;
        }
        // usize alignment is required for the returned UNICODE_STRING.
        let mut buffer = vec![0usize; (needed as usize).div_ceil(std::mem::size_of::<usize>())];
        if unsafe {
            NtQueryInformationProcess(handle, 60, buffer.as_mut_ptr().cast(), needed, &mut needed)
        } < 0
        {
            return None;
        }
        #[repr(C)]
        struct UnicodeString {
            length: u16,
            maximum: u16,
            data: *const u16,
        }
        let text = unsafe { &*buffer.as_ptr().cast::<UnicodeString>() };
        let start = text.data as usize;
        let low = buffer.as_ptr() as usize;
        let high = low + buffer.len() * std::mem::size_of::<usize>();
        if text.length % 2 != 0
            || !start.is_multiple_of(std::mem::align_of::<u16>())
            || start < low
            || start
                .checked_add(text.length as usize)
                .is_none_or(|end| end > high)
        {
            return None;
        }
        Some(String::from_utf16_lossy(unsafe {
            std::slice::from_raw_parts(text.data, text.length as usize / 2)
        }))
    }
    fn browser_invocation(path: &Path, codex: &Path, name: &str, cmd: &str) -> bool {
        let cmd = cmd.trim_start();
        let args = if let Some(quoted) = cmd.strip_prefix('"') {
            quoted.split_once('"').map(|(_, rest)| rest)
        } else {
            cmd.split_once(char::is_whitespace).map(|(_, rest)| rest)
        };
        let Some(args) = args else {
            return false;
        };
        if name == "node.exe" {
            let installed = path
                .strip_prefix(codex.join("runtimes/cua_node"))
                .is_ok_and(|relative| {
                    let parts: Vec<_> = relative.components().collect();
                    parts.len() == 3
                        && parts[1].as_os_str() == "bin"
                        && parts[2].as_os_str() == "node.exe"
                });
            // The installed runtime can also run ordinary user commands. Only
            // CUA's eval bootstrap and its trusted worker are controllers.
            let flags: Vec<_> = args.split_whitespace().collect();
            return installed
                && flags.first() == Some(&"--eval")
                && ((flags.contains(&"--session-id") && flags.contains(&"--working-dir"))
                    || args.contains("trusted-worker.js"));
        }
        name == "codex.exe"
            && path.starts_with(codex.join("bin"))
            && args.split_whitespace().next() == Some("app-server")
    }
    fn inspect(pid: u32, name: &str) -> io::Result<Option<(u64, bool)>> {
        unsafe {
            let h = Handle(OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                0,
                pid,
            ));
            if h.0.is_null() {
                let error = io::Error::last_os_error();
                // A ToolHelp snapshot also contains processes that exited meanwhile.
                return if error.raw_os_error() == Some(ERROR_INVALID_PARAMETER as i32) {
                    Ok(None)
                } else {
                    Err(error)
                };
            }
            match WaitForSingleObject(h.0, 0) {
                WAIT_OBJECT_0 => return Ok(None),
                WAIT_TIMEOUT => (),
                _ => return Err(io::Error::last_os_error()),
            }
            let mut ts: [FILETIME; 4] = std::mem::zeroed();
            if GetProcessTimes(h.0, &mut ts[0], &mut ts[1], &mut ts[2], &mut ts[3]) == 0 {
                return Err(io::Error::last_os_error());
            }
            Ok(Some((
                ((ts[0].dwHighDateTime as u64) << 32) | ts[0].dwLowDateTime as u64,
                browser_controller(h.0, name),
            )))
        }
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
                        let Some((created, browser_controller)) = inspect(*pid, name)? else {
                            continue;
                        };
                        if *pid != root && found.get(parent).is_none_or(|pc| created < *pc) {
                            continue;
                        }
                        found.insert(*pid, created);
                        result.push(Process {
                            pid: *pid,
                            parent: *parent,
                            name: name.clone(),
                            created,
                            browser_controller,
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
        processes
            .iter()
            .filter(|p| p.name == "codex.exe" && !p.browser_controller)
            .count()
            <= 1
            && processes.iter().all(|p| {
                baseline.is_some_and(|at| p.created < at)
                    || p.browser_controller
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
            resume_on_drop: true,
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
            resume_on_drop: true,
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
    #[cfg(test)]
    mod tests {
        use super::*;
        fn process(pid: u32, parent: u32, name: &str, browser_controller: bool) -> Process {
            Process {
                pid,
                parent,
                name: name.into(),
                created: 100,
                browser_controller,
            }
        }
        #[test]
        fn browser_role_requires_installed_image_and_controller_arguments() {
            let codex = Path::new("C:/Users/owner/AppData/Local/OpenAI/Codex");
            let app = codex.join("bin/version/codex.exe");
            let node = codex.join("runtimes/cua_node/version/bin/node.exe");
            assert!(browser_invocation(
                &app,
                codex,
                "codex.exe",
                "\"C:/path with spaces/codex.exe\" app-server --listen stdio://"
            ));
            assert!(!browser_invocation(
                &app,
                codex,
                "codex.exe",
                "codex.exe exec --json"
            ));
            assert!(!browser_invocation(
                Path::new("C:/worker/codex.exe"),
                codex,
                "codex.exe",
                "codex.exe app-server"
            ));
            assert!(browser_invocation(
                &node,
                codex,
                "node.exe",
                "node.exe --eval bootstrap --session-id id --working-dir cwd"
            ));
            assert!(browser_invocation(
                &node,
                codex,
                "node.exe",
                "node.exe --eval trusted-worker.js"
            ));
            assert!(!browser_invocation(
                &node,
                codex,
                "node.exe",
                "node.exe build.js"
            ));
            assert!(!browser_invocation(
                &node,
                codex,
                "node.exe",
                "node.exe --eval background_work"
            ));
            assert!(!browser_invocation(
                Path::new("C:/worker/node.exe"),
                codex,
                "node.exe",
                "node.exe --eval trusted-worker.js"
            ));
        }
        #[test]
        fn late_browser_helpers_do_not_block_but_workers_and_second_agents_do() {
            let mut tree = vec![
                process(1, 0, "codex.exe", false),
                process(2, 1, "node_repl.exe", false),
                process(3, 2, "codex.exe", true),
                process(4, 2, "node.exe", true),
            ];
            assert!(controllers(&tree, 1, Some(10)));
            tree.push(process(5, 4, "cargo.exe", false));
            assert!(!controllers(&tree, 1, Some(10)));
            tree.pop();
            tree.push(process(6, 2, "codex.exe", false));
            assert!(!controllers(&tree, 1, Some(10)));
            tree.pop();
            tree.push(process(7, 2, "node.exe", false));
            assert!(!controllers(&tree, 1, Some(10)));
        }
    }
}
#[cfg(windows)]
pub use native::{idle_agent, recover, supervisor};
