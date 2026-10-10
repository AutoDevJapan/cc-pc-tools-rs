//! Scope build processes to one operation, including grandchildren.
use std::process::{Child, Command};

#[cfg(windows)]
mod platform {
    use super::*;
    use std::{
        collections::BTreeMap,
        os::windows::{
            io::{AsRawHandle, BorrowedHandle, FromRawHandle, OwnedHandle},
            process::CommandExt,
        },
        time::{Duration, Instant},
    };
    use windows_sys::Win32::{
        Foundation::{
            CloseHandle, GetLastError, ERROR_INVALID_PARAMETER, ERROR_MORE_DATA, FILETIME, HANDLE,
            INVALID_HANDLE_VALUE, WAIT_OBJECT_0, WAIT_TIMEOUT,
        },
        System::{
            Diagnostics::ToolHelp::{
                CreateToolhelp32Snapshot, Thread32First, Thread32Next, TH32CS_SNAPTHREAD,
                THREADENTRY32,
            },
            JobObjects::{
                AssignProcessToJobObject, CreateJobObjectW, IsProcessInJob,
                JobObjectBasicAccountingInformation, JobObjectBasicProcessIdList,
                JobObjectExtendedLimitInformation, QueryInformationJobObject,
                SetInformationJobObject, TerminateJobObject,
                JOBOBJECT_BASIC_ACCOUNTING_INFORMATION, JOBOBJECT_BASIC_PROCESS_ID_LIST,
                JOBOBJECT_EXTENDED_LIMIT_INFORMATION, JOB_OBJECT_LIMIT_ACTIVE_PROCESS,
                JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
            },
            Threading::{
                GetProcessTimes, OpenProcess, OpenThread, ResumeThread, WaitForSingleObject,
                CREATE_SUSPENDED, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_SYNCHRONIZE,
                THREAD_SUSPEND_RESUME,
            },
        },
    };

    pub struct Tree(Option<Job>);
    struct Job {
        handle: OwnedHandle,
        members: BTreeMap<(u32, u64), OwnedHandle>,
        stopped: bool,
        stop_deadline: Option<Instant>,
    }
    impl Drop for Tree {
        fn drop(&mut self) {
            if let Some(job) = &mut self.0 {
                if !job.stopped {
                    // Closing a kill-on-close job only requests termination. Drain
                    // the same pinned processes on unwind/early-return as on cancel.
                    if let Err(error) = job.stop() {
                        eprintln!("Could not confirm build process cleanup: {error}");
                    }
                }
            }
        }
    }
    fn error() -> String {
        std::io::Error::last_os_error().to_string()
    }

    impl Tree {
        pub fn spawn(
            cmd: &mut Command,
            flags: u32,
            managed: bool,
        ) -> Result<(Child, Self), String> {
            if !managed {
                cmd.creation_flags(flags);
                return cmd
                    .spawn()
                    .map(|child| (child, Self(None)))
                    .map_err(|e| e.to_string());
            }
            let job = unsafe { CreateJobObjectW(std::ptr::null(), std::ptr::null()) };
            if job.is_null() {
                return Err(error());
            }
            let mut tree = Self(Some(Job {
                handle: unsafe { OwnedHandle::from_raw_handle(job) },
                members: BTreeMap::new(),
                stopped: false,
                stop_deadline: None,
            }));
            let mut limits: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = unsafe { std::mem::zeroed() };
            limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
            if unsafe {
                SetInformationJobObject(
                    job,
                    JobObjectExtendedLimitInformation,
                    &limits as *const _ as *const _,
                    std::mem::size_of_val(&limits) as u32,
                )
            } == 0
            {
                return Err(error());
            }
            // Assign the child before any application code can create grandchildren.
            cmd.creation_flags(flags | CREATE_SUSPENDED);
            let mut child = cmd.spawn().map_err(|e| e.to_string())?;
            if unsafe { AssignProcessToJobObject(job, child.as_raw_handle() as HANDLE) } == 0 {
                let reason = error();
                let _ = child.kill();
                let _ = child.wait();
                return Err(format!("Could not contain build process: {reason}"));
            }
            // Retain the spawn handle itself, before any application thread runs.
            // The root may exit before stop(), while its descendants remain.
            let pinned = unsafe { BorrowedHandle::borrow_raw(child.as_raw_handle()) }
                .try_clone_to_owned()
                .map_err(|e| e.to_string())?;
            tree.0.as_mut().unwrap().retain(child.id(), pinned)?;
            if let Err(reason) = resume(child.id()) {
                return match tree.stop(&mut child) {
                    Ok(()) => Err(reason),
                    Err(cleanup) => {
                        Err(format!("{reason}; build process cleanup failed: {cleanup}"))
                    }
                };
            }
            Ok((child, tree))
        }

        pub fn stop(&mut self, child: &mut Child) -> Result<(), String> {
            let Some(job) = &mut self.0 else {
                return Ok(());
            };
            job.stop()?;
            // The root's retained handle has already signaled: this reap cannot
            // precede or extend the bounded process-tree drain.
            child.wait().map_err(|e| e.to_string())?;
            Ok(())
        }

        pub fn observe(&mut self) -> Result<(), String> {
            if let Some(job) = &mut self.0 {
                job.capture(Instant::now() + Duration::from_secs(30))?;
            }
            Ok(())
        }
        pub fn managed(&self) -> bool {
            self.0.is_some()
        }
        pub fn stop_confirmed(&self) -> bool {
            self.0.as_ref().is_none_or(|job| job.stopped)
        }
        pub fn stop_deadline(&self) -> Option<Instant> {
            self.0.as_ref().and_then(|job| job.stop_deadline)
        }

        #[cfg(test)]
        #[allow(dead_code)] // Used by the parity integration test including this source.
        pub fn restrict_termination_for_test(
            &mut self,
            deadline: Instant,
        ) -> Result<OwnedHandle, String> {
            use windows_sys::Win32::{
                Foundation::DuplicateHandle, System::Threading::GetCurrentProcess,
            };
            const JOB_OBJECT_QUERY: u32 = 0x0004;
            const JOB_OBJECT_SET_ATTRIBUTES: u32 = 0x0002;
            let job = self.0.as_mut().ok_or("test requires a managed job")?;
            let mut restricted = std::ptr::null_mut();
            let current = unsafe { GetCurrentProcess() };
            if unsafe {
                DuplicateHandle(
                    current,
                    job.handle.as_raw_handle(),
                    current,
                    &mut restricted,
                    JOB_OBJECT_QUERY | JOB_OBJECT_SET_ATTRIBUTES,
                    0,
                    0,
                )
            } == 0
            {
                return Err(error());
            }
            let restricted = unsafe { OwnedHandle::from_raw_handle(restricted) };
            job.stop_deadline = Some(deadline);
            Ok(std::mem::replace(&mut job.handle, restricted))
        }
    }

    impl Job {
        fn retain(&mut self, pid: u32, handle: OwnedHandle) -> Result<(), String> {
            let (mut created, mut exited, mut kernel, mut user) = (
                FILETIME::default(),
                FILETIME::default(),
                FILETIME::default(),
                FILETIME::default(),
            );
            if unsafe {
                GetProcessTimes(
                    handle.as_raw_handle(),
                    &mut created,
                    &mut exited,
                    &mut kernel,
                    &mut user,
                )
            } == 0
            {
                return Err(error());
            }
            let born = (u64::from(created.dwHighDateTime) << 32) | u64::from(created.dwLowDateTime);
            self.members.entry((pid, born)).or_insert(handle);
            Ok(())
        }

        fn capture(&mut self, deadline: Instant) -> Result<(), String> {
            let mut capacity = 16usize;
            loop {
                // ULONG_PTR storage keeps the variable-length Win32 structure
                // aligned. Read both counts, including on ERROR_MORE_DATA.
                let header = std::mem::offset_of!(JOBOBJECT_BASIC_PROCESS_ID_LIST, ProcessIdList);
                let bytes = header + capacity * std::mem::size_of::<usize>();
                let mut buffer = vec![0usize; bytes.div_ceil(std::mem::size_of::<usize>())];
                let list = buffer
                    .as_mut_ptr()
                    .cast::<JOBOBJECT_BASIC_PROCESS_ID_LIST>();
                let success = unsafe {
                    QueryInformationJobObject(
                        self.handle.as_raw_handle(),
                        JobObjectBasicProcessIdList,
                        list.cast(),
                        bytes as u32,
                        std::ptr::null_mut(),
                    )
                };
                let reason = if success == 0 {
                    unsafe { GetLastError() }
                } else {
                    0
                };
                if success == 0 && reason != ERROR_MORE_DATA {
                    return Err(std::io::Error::from_raw_os_error(reason as i32).to_string());
                }
                let (assigned, count) = unsafe {
                    (
                        (*list).NumberOfAssignedProcesses as usize,
                        (*list).NumberOfProcessIdsInList as usize,
                    )
                };
                if count > capacity {
                    return Err("Invalid build job member count".into());
                }
                let pids =
                    unsafe { std::slice::from_raw_parts(buffer.as_ptr().byte_add(header), count) };
                for &pid in pids {
                    let pid = u32::try_from(pid).map_err(|_| "Invalid build job process ID")?;
                    // Even retained members do not suppress a fresh job query.
                    // Keeping a handle alive also pins its PID against reuse.
                    if self.members.keys().any(|&(known, _)| known == pid) {
                        continue;
                    }
                    let handle = unsafe {
                        OpenProcess(
                            PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                            0,
                            pid,
                        )
                    };
                    if handle.is_null() {
                        let reason = unsafe { GetLastError() };
                        if reason == ERROR_INVALID_PARAMETER {
                            continue; // Exited and destroyed before we could open it.
                        }
                        return Err(std::io::Error::from_raw_os_error(reason as i32).to_string());
                    }
                    let handle = unsafe { OwnedHandle::from_raw_handle(handle) };
                    let mut member = 0;
                    if unsafe {
                        IsProcessInJob(
                            handle.as_raw_handle(),
                            self.handle.as_raw_handle(),
                            &mut member,
                        )
                    } == 0
                    {
                        return Err(error());
                    }
                    if member != 0 {
                        self.retain(pid, handle)?;
                    } // A reused PID outside this job never becomes an owned process.
                }
                if success != 0 && assigned <= count {
                    return Ok(());
                }
                if Instant::now() >= deadline {
                    return Err("Build job member enumeration did not settle".into());
                }
                capacity = assigned.max(capacity * 2);
                if capacity > 1_048_576 {
                    return Err("Build job member list too large".into());
                }
            }
        }

        fn stop(&mut self) -> Result<(), String> {
            self.stop_until(Instant::now() + Duration::from_secs(30))
        }

        fn stop_until(&mut self, deadline: Instant) -> Result<(), String> {
            // The first stop owns the deadline. Retry and Drop may inspect or
            // request termination again, but cannot acquire another wait budget.
            let deadline = *self.stop_deadline.get_or_insert(deadline);
            if self.stopped {
                return Ok(());
            }
            // Close admission before collecting final handles: a member cannot
            // create a runnable child between the snapshot and termination.
            let mut limits: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = unsafe { std::mem::zeroed() };
            limits.BasicLimitInformation.LimitFlags =
                JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | JOB_OBJECT_LIMIT_ACTIVE_PROCESS;
            limits.BasicLimitInformation.ActiveProcessLimit = 0;
            let mut failure = if unsafe {
                SetInformationJobObject(
                    self.handle.as_raw_handle(),
                    JobObjectExtendedLimitInformation,
                    &limits as *const _ as *const _,
                    std::mem::size_of_val(&limits) as u32,
                )
            } == 0
            {
                Some(error())
            } else {
                None
            };
            if let Err(error) = self.capture(deadline) {
                failure.get_or_insert(error);
            }
            // A partial enumeration or gate failure must still stop the owned job.
            if unsafe { TerminateJobObject(self.handle.as_raw_handle(), 1) } == 0 {
                failure.get_or_insert_with(error);
            }
            loop {
                if let Err(error) = self.capture(deadline) {
                    failure.get_or_insert(error);
                }
                let mut accounting: JOBOBJECT_BASIC_ACCOUNTING_INFORMATION =
                    unsafe { std::mem::zeroed() };
                if unsafe {
                    QueryInformationJobObject(
                        self.handle.as_raw_handle(),
                        JobObjectBasicAccountingInformation,
                        &mut accounting as *mut _ as *mut _,
                        std::mem::size_of_val(&accounting) as u32,
                        std::ptr::null_mut(),
                    )
                } == 0
                {
                    failure.get_or_insert_with(error);
                    break;
                }
                if accounting.ActiveProcesses == 0 {
                    break;
                }
                if Instant::now() >= deadline {
                    failure.get_or_insert("Build process descendants did not exit".into());
                    break;
                }
                std::thread::sleep(Duration::from_millis(10));
            }
            // ActiveProcesses is accounting, not a substitute for process-object
            // signaling. In particular, descendants with null output pipes may
            // still be shutting down after the root/pipe readers finish.
            for (&(pid, born), handle) in &self.members {
                let remaining = deadline.saturating_duration_since(Instant::now());
                let millis = remaining.as_millis().min(u128::from(u32::MAX - 1)) as u32;
                match unsafe { WaitForSingleObject(handle.as_raw_handle(), millis) } {
                    WAIT_OBJECT_0 => {}
                    WAIT_TIMEOUT => {
                        failure.get_or_insert(format!(
                            "Build process did not signal exit: pid={pid} created={born}"
                        ));
                    }
                    _ => {
                        failure.get_or_insert_with(error);
                    }
                }
            }
            if let Some(error) = failure {
                Err(error)
            } else {
                self.stopped = true;
                Ok(())
            }
        }
    }

    fn resume(pid: u32) -> Result<(), String> {
        let snapshot = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD, 0) };
        if snapshot == INVALID_HANDLE_VALUE {
            return Err(error());
        }
        let mut entry: THREADENTRY32 = unsafe { std::mem::zeroed() };
        entry.dwSize = std::mem::size_of::<THREADENTRY32>() as u32;
        let mut found = unsafe { Thread32First(snapshot, &mut entry) } != 0;
        while found {
            if entry.th32OwnerProcessID == pid {
                let thread = unsafe { OpenThread(THREAD_SUSPEND_RESUME, 0, entry.th32ThreadID) };
                unsafe {
                    CloseHandle(snapshot);
                }
                if thread.is_null() {
                    return Err(error());
                }
                let count = unsafe { ResumeThread(thread) };
                let reason = match count {
                    1 => None,
                    u32::MAX => Some(error()),
                    _ => Some(format!("Unexpected build thread suspend count: {count}")),
                };
                unsafe {
                    CloseHandle(thread);
                }
                return reason.map_or(Ok(()), Err);
            }
            found = unsafe { Thread32Next(snapshot, &mut entry) } != 0;
        }
        unsafe {
            CloseHandle(snapshot);
        }
        Err("Could not find the suspended build process thread".into())
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        use windows_sys::Win32::System::Threading::{CreateEventW, SetEvent};

        #[test]
        fn zero_active_count_does_not_replace_retained_handle_wait() {
            // A controllable unsignaled waitable stands in for a process object
            // still completing kernel teardown. No PID lookup or wall-clock
            // sleep can make this regression pass accidentally.
            let job = unsafe { CreateJobObjectW(std::ptr::null(), std::ptr::null()) };
            let event = unsafe { CreateEventW(std::ptr::null(), 1, 0, std::ptr::null()) };
            assert!(!job.is_null() && !event.is_null());
            let mut job = Job {
                handle: unsafe { OwnedHandle::from_raw_handle(job) },
                members: BTreeMap::from([((1, 2), unsafe { OwnedHandle::from_raw_handle(event) })]),
                stopped: false,
                stop_deadline: None,
            };
            let result = job.stop_until(Instant::now());
            assert!(result.unwrap_err().contains("did not signal exit"));
            assert!(!job.stopped);
            assert_ne!(unsafe { SetEvent(event) }, 0);
            job.stop_until(Instant::now()).unwrap();
            assert!(job.stopped);
        }
    }
}

#[cfg(not(windows))]
mod platform {
    use super::*;
    pub struct Tree;
    impl Tree {
        pub fn spawn(cmd: &mut Command, _: u32, managed: bool) -> Result<(Child, Self), String> {
            if managed {
                return Err(
                    "This Windows-only live tool requires Windows process-tree containment".into(),
                );
            }
            cmd.spawn()
                .map(|child| (child, Self))
                .map_err(|e| e.to_string())
        }
        pub fn stop(&mut self, _: &mut Child) -> Result<(), String> {
            Ok(())
        }
        pub fn observe(&mut self) -> Result<(), String> {
            Ok(())
        }
        pub fn managed(&self) -> bool {
            false
        }
    }
}

pub use platform::Tree;
