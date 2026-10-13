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
    use std::collections::{HashMap, HashSet};
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
    #[derive(Debug)]
    struct NativeError {
        stage: &'static str,
        source: io::Error,
    }
    impl std::fmt::Display for NativeError {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "{}: {}", self.stage, self.source)
        }
    }
    impl std::error::Error for NativeError {
        fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
            Some(&self.source)
        }
    }
    fn at(stage: &'static str, source: io::Error) -> io::Error {
        io::Error::new(source.kind(), NativeError { stage, source })
    }
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
                terminate_running(h.0, || Ok(()))?;
            }
            for h in &self.handles {
                if unsafe { WaitForSingleObject(h.0, 2000) } != WAIT_OBJECT_0 {
                    return Err(io::Error::other("paused process did not exit"));
                }
            }
            Ok(())
        }
    }
    // The hook is private and has no environment/CLI control. Tests use it to
    // complete a real process exit precisely between the wait and termination.
    fn terminate_running(
        handle: HANDLE,
        before_terminate: impl FnOnce() -> io::Result<()>,
    ) -> io::Result<()> {
        match unsafe { WaitForSingleObject(handle, 0) } {
            WAIT_OBJECT_0 => return Ok(()),
            WAIT_TIMEOUT => (),
            _ => return Err(at("stop/WaitForSingleObject", io::Error::last_os_error())),
        }
        before_terminate()?;
        if unsafe { TerminateProcess(handle, 0) } == 0 {
            // Capture first: the wait can overwrite the thread's last error.
            let error = io::Error::last_os_error();
            // Windows also returns ACCESS_DENIED when a process exits between
            // the initial wait and TerminateProcess. Only the SAME retained,
            // creation-checked handle's signal proves this is an exit race.
            // Termination can still be draining I/O; use the existing bounded
            // exit wait, never retry termination or reopen a PID here.
            if error.raw_os_error() == Some(ERROR_ACCESS_DENIED as i32)
                && unsafe { WaitForSingleObject(handle, 2000) } == WAIT_OBJECT_0
            {
                return Ok(());
            }
            return Err(at("stop/TerminateProcess", error));
        }
        Ok(())
    }
    fn freeze(pid: u32, created: u64, frozen: &mut Frozen) -> io::Result<()> {
        if !freeze_running(pid, created, frozen)? {
            return Err(io::Error::other("process exited before pause"));
        }
        Ok(())
    }
    fn freeze_running(pid: u32, created: u64, frozen: &mut Frozen) -> io::Result<bool> {
        if pid == std::process::id() {
            return Err(io::Error::other("cannot pause the calling process"));
        }
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
                let err = io::Error::last_os_error();
                return if err.raw_os_error() == Some(ERROR_INVALID_PARAMETER as i32) {
                    Ok(false)
                } else {
                    Err(at("freeze/OpenProcess", err))
                };
            }
            let mut ts: [FILETIME; 4] = std::mem::zeroed();
            if GetProcessTimes(h.0, &mut ts[0], &mut ts[1], &mut ts[2], &mut ts[3]) == 0
                || (((ts[0].dwHighDateTime as u64) << 32) | ts[0].dwLowDateTime as u64) != created
            {
                return Err(io::Error::other("process identity changed before pause"));
            }
            if WaitForSingleObject(h.0, 0) == WAIT_OBJECT_0 {
                return Ok(false);
            }
            if NtSuspendProcess(h.0) < 0 {
                if WaitForSingleObject(h.0, 0) == WAIT_OBJECT_0 {
                    return Ok(false);
                }
                return Err(io::Error::other("process pause failed"));
            }
            frozen.handles.push(h);
            Ok(true)
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
        let Some(argv) = windows_args(cmd) else {
            return false;
        };
        let args = &argv[1..];
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
            // Strip every eval payload: flags or filenames inside JS strings
            // are not actual bootstrap arguments.
            let mut flags = Vec::new();
            let mut iter = args.iter();
            while let Some(arg) = iter.next() {
                if matches!(arg.as_str(), "--eval" | "-e") {
                    iter.next();
                } else {
                    flags.push(arg.as_str());
                }
            }
            let worker_root = codex
                .parent()
                .and_then(Path::parent)
                .map(|p| p.join("Temp"));
            let worker = flags.iter().any(|arg| {
                worker_root.as_ref().is_some_and(|root| {
                    Path::new(arg).strip_prefix(root).is_ok_and(|relative| {
                        let parts: Vec<_> = relative.components().collect();
                        parts.len() == 2
                            && parts[0].as_os_str().to_string_lossy().starts_with(".tmp")
                            && parts[1].as_os_str() == "trusted-worker.js"
                    })
                })
            });
            return installed
                && args.first().map(String::as_str) == Some("--eval")
                && ((flags.contains(&"--session-id") && flags.contains(&"--working-dir"))
                    || worker);
        }
        name == "codex.exe"
            && path.starts_with(codex.join("bin"))
            && args.first().map(String::as_str) == Some("app-server")
    }
    fn windows_args(cmd: &str) -> Option<Vec<String>> {
        let wide: Vec<_> = cmd.trim_start().encode_utf16().chain(Some(0)).collect();
        let mut count = 0;
        unsafe {
            let args = windows_sys::Win32::UI::Shell::CommandLineToArgvW(wide.as_ptr(), &mut count);
            if args.is_null() {
                return None;
            }
            let result = std::slice::from_raw_parts(args, count as usize)
                .iter()
                .map(|arg| {
                    let mut len = 0;
                    while *(*arg).add(len) != 0 {
                        len += 1;
                    }
                    String::from_utf16_lossy(std::slice::from_raw_parts(*arg, len))
                })
                .collect();
            LocalFree(args.cast());
            Some(result)
        }
    }
    fn inspect(pid: u32, name: &str) -> io::Result<Option<(u64, bool, bool)>> {
        Ok(
            inspect_lifetime(pid, name)?
                .map(|(created, browser, live, _)| (created, browser, live)),
        )
    }
    fn inspect_lifetime(pid: u32, name: &str) -> io::Result<Option<(u64, bool, bool, u64)>> {
        Ok(inspect_retained(pid, name)?.map(|(info, _)| info))
    }
    type Lifetime = (u64, bool, bool, u64);
    fn inspect_retained(pid: u32, name: &str) -> io::Result<Option<(Lifetime, Handle)>> {
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
                    Err(at("inspect/OpenProcess", error))
                };
            }
            let live = match WaitForSingleObject(h.0, 0) {
                WAIT_OBJECT_0 => false,
                WAIT_TIMEOUT => true,
                _ => {
                    return Err(at(
                        "inspect/WaitForSingleObject",
                        io::Error::last_os_error(),
                    ));
                }
            };
            let mut ts: [FILETIME; 4] = std::mem::zeroed();
            if GetProcessTimes(h.0, &mut ts[0], &mut ts[1], &mut ts[2], &mut ts[3]) == 0 {
                return Err(at("inspect/GetProcessTimes", io::Error::last_os_error()));
            }
            Ok(Some((
                (
                    ((ts[0].dwHighDateTime as u64) << 32) | ts[0].dwLowDateTime as u64,
                    browser_controller(h.0, name),
                    live,
                    ((ts[1].dwHighDateTime as u64) << 32) | ts[1].dwLowDateTime as u64,
                ),
                h,
            )))
        }
    }
    fn tree(
        root: u32,
        root_created: u64,
        prior: &HashMap<u32, u64>,
        mut ancestors: Option<&mut Vec<Handle>>,
    ) -> io::Result<(Vec<Process>, HashMap<u32, u64>)> {
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
            if ancestors.is_none() && crate::proc_created(root) != Some(root_created) {
                return Err(io::Error::other("root process identity changed"));
            }
            let Some(ancestors) = ancestors.as_mut() else {
                return walk(root, root_created, prior, &all, inspect);
            };
            // Retained root/descendant handles prevent reuse of their PIDs.
            // For exited ancestors discovered in this snapshot, bound children
            // by the actual exit time; a vanished unobserved ancestor is not
            // enough evidence to authorize terminating its apparent children.
            let mut lifetimes = HashMap::new();
            for (&pid, &created) in prior {
                let Some(((actual, _, _, exited), handle)) = inspect_retained(pid, "")? else {
                    return Err(io::Error::other(
                        "cancellation ancestor identity unavailable",
                    ));
                };
                if actual != created {
                    return Err(io::Error::other("cancellation ancestor identity changed"));
                }
                lifetimes.insert(pid, (actual, exited));
                ancestors.push(handle);
            }
            walk(root, root_created, prior, &all, |pid, name| {
                let Some(((created, browser, live, exited), handle)) = inspect_retained(pid, name)?
                else {
                    return Err(io::Error::other(
                        "cancellation ancestor vanished during inspection",
                    ));
                };
                let parent = all.iter().find(|p| p.0 == pid).map(|p| p.1);
                if pid != root
                    && parent
                        .and_then(|p| lifetimes.get(&p))
                        .is_some_and(|(_, end)| *end != 0 && created > *end)
                {
                    return Err(io::Error::other(
                        "descendant was created after ancestor exited",
                    ));
                }
                lifetimes.insert(pid, (created, exited));
                ancestors.push(handle);
                Ok(Some((created, browser, live)))
            })
        }
    }
    // Keep exited ancestors in the topology, including during the second
    // snapshot after freezing. Their live children must still block a yield.
    fn walk(
        root: u32,
        root_created: u64,
        prior: &HashMap<u32, u64>,
        all: &[(u32, u32, String)],
        mut inspect: impl FnMut(u32, &str) -> io::Result<Option<(u64, bool, bool)>>,
    ) -> io::Result<(Vec<Process>, HashMap<u32, u64>)> {
        let mut found = prior.clone();
        found.insert(root, root_created);
        let mut checked = HashSet::new();
        let mut result = Vec::new();
        loop {
            let before = checked.len();
            for (pid, parent, name) in all {
                if (*pid == root || found.contains_key(parent)) && !checked.contains(pid) {
                    checked.insert(*pid);
                    let Some((created, browser_controller, live)) = inspect(*pid, name)? else {
                        // The snapshot's PID has vanished. Retain the parent
                        // edge conservatively, but never freeze that PID.
                        let ancestor = found.get(parent).copied().unwrap_or(root_created);
                        found.entry(*pid).or_insert(ancestor);
                        continue;
                    };
                    if prior.get(pid).is_some_and(|old| *old != created) {
                        return Err(io::Error::other(
                            "descendant identity changed between snapshots",
                        ));
                    }
                    if *pid != root && found.get(parent).is_none_or(|pc| created < *pc) {
                        continue;
                    }
                    found.insert(*pid, created);
                    if !live {
                        continue;
                    }
                    result.push(Process {
                        pid: *pid,
                        parent: *parent,
                        name: name.clone(),
                        created,
                        browser_controller,
                    });
                }
            }
            if checked.len() == before {
                break;
            }
        }
        Ok((result, found))
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
        let (before, topology) = tree(pid, created, &HashMap::new(), None)?;
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
        let (after, _) = tree(pid, created, &topology, None)?;
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
    /// Stop an owned tree using retained native handles. `taskkill /T` needs
    /// an authentication lookup even for the caller's own children and fails
    /// under the CI service's filtered token. No other identity is required.
    pub fn stop_tree(pid: u32, created: u64) -> io::Result<()> {
        let mut frozen = Frozen {
            handles: Vec::new(),
            marker: None,
            armed: Vec::new(),
            resume_on_drop: true,
        };
        if !freeze_running(pid, created, &mut frozen)? {
            // An exited root can still have live descendants. Without a
            // retained root identity, cancellation must not report success.
            return Err(io::Error::other(
                "root exited before cancellation could retain its identity",
            ));
        }
        let mut topology = HashMap::from([(pid, created)]);
        let mut retained = HashSet::from([(pid, created)]);
        let mut ancestors = Vec::new();
        // Freeze parents before re-enumerating so descendants cannot keep
        // extending the tree. On failure Drop resumes the retained survivors.
        for _ in 0..32 {
            let (processes, observed) = tree(pid, created, &topology, Some(&mut ancestors))?;
            topology = observed;
            let mut added = false;
            for process in processes {
                if retained.insert((process.pid, process.created)) {
                    freeze_running(process.pid, process.created, &mut frozen)?;
                    added = true;
                }
            }
            if !added {
                frozen.stop()?;
                let (survivors, observed) = tree(pid, created, &topology, Some(&mut ancestors))?;
                topology = observed;
                if survivors.is_empty() {
                    frozen.resume_on_drop = false;
                    return Ok(());
                }
            }
        }
        Err(io::Error::other("owned process tree did not stabilize"))
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
        struct OwnedChild(std::process::Child);
        impl OwnedChild {
            fn sleeping() -> Self {
                use std::os::windows::process::CommandExt;
                Self(
                    std::process::Command::new("powershell.exe")
                        .args([
                            "-NoProfile",
                            "-NonInteractive",
                            "-Command",
                            "Start-Sleep -Seconds 60",
                        ])
                        .creation_flags(0x08000000)
                        .spawn()
                        .unwrap(),
                )
            }
        }
        impl Drop for OwnedChild {
            fn drop(&mut self) {
                let _ = self.0.kill();
                let _ = self.0.wait();
            }
        }
        #[test]
        fn cancellation_exit_between_wait_and_terminate_is_not_a_permission_failure() {
            let child = OwnedChild::sleeping();
            let created = crate::proc_created(child.0.id()).unwrap();
            let mut frozen = Frozen {
                handles: Vec::new(),
                marker: None,
                armed: Vec::new(),
                resume_on_drop: true,
            };
            freeze(child.0.id(), created, &mut frozen).unwrap();
            let handle = frozen.handles[0].0;
            let mut exited_in_gap = false;
            let result = terminate_running(handle, || {
                if unsafe { TerminateProcess(handle, 0) } == 0 {
                    return Err(io::Error::last_os_error());
                }
                exited_in_gap = unsafe { WaitForSingleObject(handle, 2000) } == WAIT_OBJECT_0;
                if !exited_in_gap {
                    return Err(io::Error::other("fixture exit did not complete"));
                }
                Ok(())
            });
            assert!(
                exited_in_gap,
                "the real exit must occur after the initial unsignaled wait"
            );
            assert!(
                result.is_ok(),
                "signaled same-identity handle rejected: {result:?}"
            );
        }
        #[test]
        fn cancellation_denied_for_a_live_handle_keeps_the_original_error() {
            let mut child = OwnedChild::sleeping();
            let (_, handle) = inspect_retained(child.0.id(), "").unwrap().unwrap();
            // Query/synchronize access deliberately excludes PROCESS_TERMINATE.
            let result = terminate_running(handle.0, || Ok(()));
            let still_alive = child.0.try_wait().unwrap().is_none();
            let error = result.unwrap_err();
            let original = &error
                .get_ref()
                .unwrap()
                .downcast_ref::<NativeError>()
                .unwrap()
                .source;
            assert_eq!(original.raw_os_error(), Some(ERROR_ACCESS_DENIED as i32));
            assert!(still_alive);
            assert!(error.to_string().starts_with("stop/TerminateProcess:"));
        }
        #[test]
        fn cancellation_unknown_handle_never_attempts_termination() {
            let mut attempted = false;
            let result = terminate_running(INVALID_HANDLE_VALUE, || {
                attempted = true;
                Ok(())
            });
            assert!(!attempted);
            assert!(
                result
                    .unwrap_err()
                    .to_string()
                    .starts_with("stop/WaitForSingleObject:")
            );
        }
        #[test]
        fn cancellation_rejects_its_caller_without_suspending_it() {
            let pid = std::process::id();
            assert!(stop_tree(pid, crate::proc_created(pid).unwrap()).is_err());
        }
        #[test]
        fn cancellation_uses_the_recorded_creation_time() {
            use std::os::windows::process::CommandExt;
            let mut child = std::process::Command::new("powershell.exe")
                .args([
                    "-NoProfile",
                    "-NonInteractive",
                    "-Command",
                    "Start-Sleep -Seconds 60",
                ])
                .creation_flags(0x08000000)
                .spawn()
                .unwrap();
            let created = crate::proc_created(child.id()).unwrap();
            let rejected = stop_tree(child.id(), created + 1).is_err();
            let remained_alive = child.try_wait().unwrap().is_none();
            let stopped = stop_tree(child.id(), created);
            // Always drain the owned child even when an assertion would fail.
            let _ = child.kill();
            child.wait().unwrap();
            assert!(rejected && remained_alive);
            stopped.unwrap();
            assert!(stop_tree(child.id(), created).is_err());
        }
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
                "node.exe --eval bootstrap --eval load -- C:/Users/owner/AppData/Local/Temp/.tmpWorker/trusted-worker.js cwd"
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
                &node,
                codex,
                "node.exe",
                "node.exe --eval \"console.log('foo --session-id --working-dir foo')\""
            ));
            assert!(!browser_invocation(
                &node,
                codex,
                "node.exe",
                "node.exe --eval \"console.log('trusted-worker.js')\""
            ));
            assert!(!browser_invocation(
                &node,
                codex,
                "node.exe",
                "node.exe --eval bootstrap --eval \"'--session-id'\" --eval \"'--working-dir'\""
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
        #[test]
        fn exited_ancestor_stays_in_topology_until_its_live_worker_finishes() {
            // The wrapper existed in the snapshot, then exited before inspect.
            // Both an exited handle and an already vanished PID must preserve
            // traversal to its worker. The next snapshot lacks the wrapper.
            for vanished in [false, true] {
                let all = vec![
                    (3, 2, "cargo.exe".into()),
                    (2, 1, "cmd.exe".into()),
                    (1, 0, "codex.exe".into()),
                ];
                let (live, topology) = walk(1, 10, &HashMap::new(), &all, |pid, _| {
                    Ok(if pid == 2 && vanished {
                        None
                    } else {
                        Some((10 + u64::from(pid), false, pid != 2))
                    })
                })
                .unwrap();
                assert!(topology.contains_key(&2));
                assert!(live.iter().any(|p| p.pid == 3));
                assert!(!live.iter().any(|p| p.pid == 2));
                assert!(!controllers(&live, 1, None));
                let (again, _) = walk(
                    1,
                    10,
                    &topology,
                    &[all[0].clone(), all[2].clone()],
                    |pid, _| Ok(Some((10 + u64::from(pid), false, true))),
                )
                .unwrap();
                assert!(again.iter().any(|p| p.pid == 3));
                assert!(!controllers(&again, 1, None));
                assert!(
                    walk(1, 10, &topology, &all, |pid, _| Ok(Some((
                        100 + u64::from(pid),
                        false,
                        true
                    ))))
                    .is_err()
                );
            }
        }
    }
}
#[cfg(windows)]
pub use native::{idle_agent, recover, stop_tree, supervisor};
