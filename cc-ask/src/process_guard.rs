//! Own transient child trees; intentionally detached Codex supervisors may break away.
use crate::core::Result;
use std::process::{Child, Command};

#[cfg(windows)]
mod platform {
    use super::*;
    use std::os::windows::{io::AsRawHandle, process::CommandExt};
    use windows_sys::Win32::{
        Foundation::*,
        System::{Diagnostics::ToolHelp::*, JobObjects::*, Threading::*},
    };
    pub struct Group(HANDLE);
    impl Drop for Group {
        fn drop(&mut self) {
            unsafe {
                CloseHandle(self.0);
            }
        }
    }
    fn error() -> String {
        std::io::Error::last_os_error().to_string()
    }
    impl Group {
        fn limits(&self, kill_on_close: bool) -> Result<()> {
            let mut limits: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = unsafe { std::mem::zeroed() };
            limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_BREAKAWAY_OK
                | if kill_on_close {
                    JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
                } else {
                    0
                };
            if unsafe {
                SetInformationJobObject(
                    self.0,
                    JobObjectExtendedLimitInformation,
                    &limits as *const _ as _,
                    std::mem::size_of_val(&limits) as u32,
                )
            } == 0
            {
                Err(format!("SetInformationJobObject: {}", error()))
            } else {
                Ok(())
            }
        }
        pub fn spawn(command: &mut Command) -> Result<(Child, Self)> {
            let handle = unsafe { CreateJobObjectW(std::ptr::null(), std::ptr::null()) };
            if handle.is_null() {
                return Err(format!("CreateJobObjectW: {}", error()));
            }
            let group = Self(handle);
            group.limits(true)?;
            command.creation_flags(CREATE_NO_WINDOW | CREATE_SUSPENDED);
            let mut child = command.spawn().map_err(|e| format!("spawn suspended child: {e}"))?;
            if unsafe { AssignProcessToJobObject(handle, child.as_raw_handle() as _) } == 0 {
                let reason = format!("AssignProcessToJobObject: {}", error());
                let _ = child.kill();
                return Err(reason);
            }
            if let Err(reason) = resume(child.id()) {
                group.stop(&mut child);
                return Err(reason);
            }
            Ok((child, group))
        }
        pub fn stop(&self, child: &mut Child) {
            // The owning handle pins membership; no PID ancestry guesses or /T.
            unsafe {
                TerminateJobObject(self.0, 1);
            }
            let _ = child.kill();
        }
        pub fn completed(&self) -> Result<()> {
            // Successful launchers can leave legitimate daemons behind. Their
            // inherited output handles cannot block our file-based capture.
            self.limits(false)
        }
    }
    fn resume(pid: u32) -> Result<()> {
        let handle = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD, 0) };
        if handle == INVALID_HANDLE_VALUE {
            return Err(format!("CreateToolhelp32Snapshot: {}", error()));
        }
        struct Snapshot(HANDLE);
        impl Drop for Snapshot {
            fn drop(&mut self) {
                unsafe {
                    CloseHandle(self.0);
                }
            }
        }
        let _snapshot = Snapshot(handle);
        let mut entry: THREADENTRY32 = unsafe { std::mem::zeroed() };
        entry.dwSize = std::mem::size_of_val(&entry) as u32;
        let mut found = unsafe { Thread32First(handle, &mut entry) } != 0;
        while found {
            if entry.th32OwnerProcessID == pid {
                let thread = unsafe { OpenThread(THREAD_SUSPEND_RESUME, 0, entry.th32ThreadID) };
                if thread.is_null() {
                    return Err(format!("OpenThread for suspended child: {}", error()));
                }
                let count = unsafe { ResumeThread(thread) };
                let result = if count == u32::MAX {
                    Err(format!("ResumeThread for suspended child: {}", error()))
                } else {
                    Ok(())
                };
                unsafe {
                    CloseHandle(thread);
                }
                return result;
            }
            found = unsafe { Thread32Next(handle, &mut entry) } != 0;
        }
        Err("suspended child's primary thread is unavailable".into())
    }
}
#[cfg(unix)]
mod platform {
    use super::*;
    use std::os::unix::process::CommandExt;
    pub struct Group(i32);
    unsafe extern "C" {
        fn kill(pid: i32, signal: i32) -> i32;
    }
    impl Group {
        pub fn spawn(command: &mut Command) -> Result<(Child, Self)> {
            command.process_group(0);
            let child = command.spawn().map_err(|e| e.to_string())?;
            let id = i32::try_from(child.id()).map_err(|e| e.to_string())?;
            Ok((child, Self(id)))
        }
        pub fn stop(&self, child: &mut Child) {
            unsafe {
                kill(-self.0, 9);
            }
            let _ = child.kill();
        }
        pub fn completed(&self) -> Result<()> {
            Ok(())
        }
    }
}
pub use platform::Group;
