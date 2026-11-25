//! Windows lifetime for the one-shot capacity version bridge.
use std::{
    ffi::OsStr,
    fs::File,
    io::{self, PipeReader, PipeWriter, pipe},
    mem::{size_of, size_of_val, zeroed},
    os::windows::{
        ffi::OsStrExt,
        io::{AsRawHandle, FromRawHandle, OwnedHandle},
    },
    path::Path,
    ptr::{null, null_mut},
    thread::{self, JoinHandle},
    time::{Duration, Instant},
};
use windows_sys::Win32::{
    Foundation::{DUPLICATE_SAME_ACCESS, DuplicateHandle, HANDLE, WAIT_OBJECT_0},
    System::{
        IO::CancelSynchronousIo,
        JobObjects::{
            CreateJobObjectW, JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
            JOBOBJECT_BASIC_ACCOUNTING_INFORMATION, JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
            JobObjectBasicAccountingInformation, JobObjectExtendedLimitInformation,
            QueryInformationJobObject, SetInformationJobObject, TerminateJobObject,
        },
        Threading::{
            CREATE_NO_WINDOW, CreateProcessW, DeleteProcThreadAttributeList,
            EXTENDED_STARTUPINFO_PRESENT, GetCurrentProcess, GetExitCodeProcess,
            InitializeProcThreadAttributeList, LPPROC_THREAD_ATTRIBUTE_LIST, PROCESS_INFORMATION,
            PROC_THREAD_ATTRIBUTE_HANDLE_LIST, PROC_THREAD_ATTRIBUTE_JOB_LIST, STARTF_USESTDHANDLES,
            STARTUPINFOEXW, STARTUPINFOW, UpdateProcThreadAttribute, WaitForSingleObject,
        },
    },
};

const EXIT_GRACE: Duration = Duration::from_secs(5);

pub(super) struct CapacityProcess {
    job: OwnedHandle,
    child: OwnedHandle,
}

impl CapacityProcess {
    pub(super) fn spawn(
        program: &Path,
        credentials: &Path,
    ) -> io::Result<(Self, PipeWriter, PipeReader)> {
        // The unnamed, non-inheritable job is owned solely by the CLI.
        let job = unsafe { CreateJobObjectW(null(), null()) };
        if job.is_null() {
            return Err(io::Error::last_os_error());
        }
        let job = unsafe { OwnedHandle::from_raw_handle(job) };
        let mut limits: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = unsafe { zeroed() };
        limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
        if unsafe {
            SetInformationJobObject(
                job.as_raw_handle(),
                JobObjectExtendedLimitInformation,
                (&limits as *const JOBOBJECT_EXTENDED_LIMIT_INFORMATION).cast(),
                size_of_val(&limits) as u32,
            )
        } == 0
        {
            return Err(io::Error::last_os_error());
        }

        let (child_read, input) = pipe()?;
        let (output, child_write) = pipe()?;
        let null_output = File::options().write(true).open(r"\\.\NUL")?;
        let child_read = inheritable(&child_read)?;
        let child_write = inheritable(&child_write)?;
        let child_error = inheritable(&null_output)?;
        let stdio = [
            child_read.as_raw_handle(),
            child_write.as_raw_handle(),
            child_error.as_raw_handle(),
        ];
        let jobs = [job.as_raw_handle()];
        let mut attributes = StartupAttributes::new()?;
        attributes.set(PROC_THREAD_ATTRIBUTE_HANDLE_LIST as usize, &stdio)?;
        // Assign at creation. Spawning suspended and assigning afterward would
        // still leave a window where a killed CLI could orphan a suspended child.
        attributes.set(PROC_THREAD_ATTRIBUTE_JOB_LIST as usize, &jobs)?;

        let mut startup: STARTUPINFOEXW = unsafe { zeroed() };
        startup.StartupInfo.cb = size_of::<STARTUPINFOEXW>() as u32;
        startup.StartupInfo.dwFlags = STARTF_USESTDHANDLES;
        startup.StartupInfo.hStdInput = stdio[0];
        startup.StartupInfo.hStdOutput = stdio[1];
        startup.StartupInfo.hStdError = stdio[2];
        startup.lpAttributeList = attributes.as_ptr();

        let program = bridge_application(program)?;
        let mut application: Vec<u16> = program.as_os_str().encode_wide().collect();
        if application.contains(&0) {
            return Err(io::ErrorKind::InvalidInput.into());
        }
        application.push(0);
        // CreateProcessW takes UTF-16 argv encoded as one mutable command line.
        let mut command = Vec::new();
        for argument in [
            program.as_os_str(),
            OsStr::new("session"),
            OsStr::new("--config"),
            credentials.as_os_str(),
        ] {
            append_arg(&mut command, argument)?;
        }
        command.push(0);
        let mut process: PROCESS_INFORMATION = unsafe { zeroed() };
        if unsafe {
            CreateProcessW(
                application.as_ptr(),
                command.as_mut_ptr(),
                null(),
                null(),
                1,
                CREATE_NO_WINDOW | EXTENDED_STARTUPINFO_PRESENT,
                null(),
                null(),
                (&startup as *const STARTUPINFOEXW).cast::<STARTUPINFOW>(),
                &mut process,
            )
        } == 0
        {
            return Err(io::Error::last_os_error());
        }
        let child = unsafe { OwnedHandle::from_raw_handle(process.hProcess) };
        let _primary_thread = unsafe { OwnedHandle::from_raw_handle(process.hThread) };
        // io::pipe creates synchronous handles on Windows. Keep their native
        // Read/Write implementations so the pipe workers can be canceled.
        Ok((Self { job, child }, input, output))
    }

    pub(super) fn finish(self) -> bool {
        let deadline = Instant::now() + EXIT_GRACE;
        if !wait_handle(self.child.as_raw_handle(), EXIT_GRACE) {
            self.stop();
            return false;
        }
        let Self { job, child } = self;
        let mut code = u32::MAX;
        let got_status = unsafe { GetExitCodeProcess(child.as_raw_handle(), &mut code) };
        // Release our process handle before checking the job accounting.
        drop(child);
        let drained = wait_empty(&job, deadline);
        if !drained {
            stop_job(&job, None);
        }
        got_status != 0 && code == 0 && drained
    }

    pub(super) fn stop(self) {
        let Self { job, child } = self;
        stop_job(&job, Some(child));
    }
}

fn stop_job(job: &OwnedHandle, child: Option<OwnedHandle>) {
    unsafe { TerminateJobObject(job.as_raw_handle(), 1) };
    let deadline = Instant::now() + EXIT_GRACE;
    if let Some(child) = child {
        let _ = wait_handle(child.as_raw_handle(), EXIT_GRACE);
    }
    let _ = wait_empty(job, deadline);
    // The caller closes the last job handle even if explicit termination failed.
}

fn wait_empty(job: &OwnedHandle, deadline: Instant) -> bool {
    loop {
        match active_processes(job) {
            Some(0) => return true,
            Some(_) if Instant::now() < deadline => thread::sleep(Duration::from_millis(10)),
            _ => return false,
        }
    }
}

fn active_processes(job: &OwnedHandle) -> Option<u32> {
    let mut accounting: JOBOBJECT_BASIC_ACCOUNTING_INFORMATION = unsafe { zeroed() };
    let ok = unsafe {
        QueryInformationJobObject(
            job.as_raw_handle(),
            JobObjectBasicAccountingInformation,
            (&mut accounting as *mut JOBOBJECT_BASIC_ACCOUNTING_INFORMATION).cast(),
            size_of_val(&accounting) as u32,
            null_mut(),
        )
    };
    (ok != 0).then_some(accounting.ActiveProcesses)
}

fn wait_handle(handle: HANDLE, timeout: Duration) -> bool {
    let milliseconds = timeout.as_millis().min(u128::from(u32::MAX - 1)) as u32;
    (unsafe { WaitForSingleObject(handle, milliseconds) }) == WAIT_OBJECT_0
}

// Normally the job exit closes the only child-side handles. Keep close bounded
// even if an OS pipe worker has not yet returned, and never report that as success.
pub(super) fn join_pipe<T>(worker: JoinHandle<T>) -> Option<T> {
    if wait_handle(worker.as_raw_handle(), EXIT_GRACE) {
        return worker.join().ok();
    }
    let deadline = Instant::now() + Duration::from_secs(1);
    loop {
        // Repeat within the deadline in case the worker was between I/O calls.
        unsafe { CancelSynchronousIo(worker.as_raw_handle()) };
        if wait_handle(worker.as_raw_handle(), Duration::from_millis(20)) {
            let _ = worker.join();
            break;
        }
        if Instant::now() >= deadline {
            break;
        }
    }
    None
}

fn inheritable(handle: &impl AsRawHandle) -> io::Result<OwnedHandle> {
    let current = unsafe { GetCurrentProcess() };
    let mut copy = null_mut();
    if unsafe {
        DuplicateHandle(
            current,
            handle.as_raw_handle(),
            current,
            &mut copy,
            0,
            1,
            DUPLICATE_SAME_ACCESS,
        )
    } == 0
    {
        return Err(io::Error::last_os_error());
    }
    Ok(unsafe { OwnedHandle::from_raw_handle(copy) })
}

struct StartupAttributes(Vec<usize>);

impl StartupAttributes {
    fn new() -> io::Result<Self> {
        let mut size = 0;
        // The first call returns the storage size.
        unsafe { InitializeProcThreadAttributeList(null_mut(), 2, 0, &mut size) };
        if size == 0 {
            return Err(io::Error::last_os_error());
        }
        let mut storage = vec![0usize; size.div_ceil(size_of::<usize>())];
        if unsafe { InitializeProcThreadAttributeList(storage.as_mut_ptr().cast(), 2, 0, &mut size) }
            == 0
        {
            return Err(io::Error::last_os_error());
        }
        Ok(Self(storage))
    }

    fn as_ptr(&mut self) -> LPPROC_THREAD_ATTRIBUTE_LIST {
        self.0.as_mut_ptr().cast()
    }

    // Value arrays remain in the caller's scope through CreateProcessW.
    fn set<const N: usize>(&mut self, attribute: usize, handles: &[HANDLE; N]) -> io::Result<()> {
        if unsafe {
            UpdateProcThreadAttribute(
                self.as_ptr(),
                0,
                attribute,
                handles.as_ptr().cast(),
                size_of_val(handles),
                null_mut(),
                null_mut(),
            )
        } == 0
        {
            return Err(io::Error::last_os_error());
        }
        Ok(())
    }
}

impl Drop for StartupAttributes {
    fn drop(&mut self) {
        unsafe { DeleteProcThreadAttributeList(self.as_ptr()) };
    }
}

fn bridge_application(program: &Path) -> io::Result<std::path::PathBuf> {
    let mut application = program.to_owned();
    // Preserve Command's resolution of an absolute executable path without .exe.
    if !program
        .extension()
        .is_some_and(|ext| ext.as_encoded_bytes().eq_ignore_ascii_case(b"exe"))
    {
        let mut name = program.as_os_str().to_os_string();
        name.push(".exe");
        if std::fs::symlink_metadata(&name).is_ok() {
            application = name.into();
        }
    }
    // Normalizing the existing absolute path also handles long Windows paths.
    application.canonicalize()
}

fn append_arg(command: &mut Vec<u16>, arg: &OsStr) -> io::Result<()> {
    if !command.is_empty() {
        command.push(b' ' as u16);
    }
    command.push(b'"' as u16);
    let mut slashes = 0;
    for unit in arg.encode_wide() {
        match unit {
            0 => return Err(io::ErrorKind::InvalidInput.into()),
            0x5c => {
                slashes += 1;
                continue;
            }
            0x22 => slashes = slashes * 2 + 1,
            _ => {}
        }
        command.extend(std::iter::repeat_n(b'\\' as u16, slashes));
        command.push(unit);
        slashes = 0;
    }
    command.extend(std::iter::repeat_n(b'\\' as u16, slashes * 2));
    command.push(b'"' as u16);
    Ok(())
}
