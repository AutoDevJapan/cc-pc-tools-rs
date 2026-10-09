//! Windows output capture with no blocking reader workers or pending I/O.
//! The operation owns every pipe and byte buffer until completion or error.
use crate::process::Output;
use std::{
    fs::File,
    os::windows::io::{AsRawHandle, FromRawHandle, OwnedHandle},
    sync::atomic::{AtomicU64, Ordering},
    time::{Duration, Instant},
};
use windows_sys::Win32::{
    Foundation::{
        GetLastError, ERROR_BROKEN_PIPE, ERROR_NO_DATA, ERROR_PIPE_CONNECTED, GENERIC_WRITE,
        HANDLE, INVALID_HANDLE_VALUE,
    },
    Storage::FileSystem::{CreateFileW, FILE_ATTRIBUTE_NORMAL, OPEN_EXISTING},
};

#[link(name = "kernel32")]
unsafe extern "system" {
    fn CreateNamedPipeW(
        name: *const u16,
        open_mode: u32,
        pipe_mode: u32,
        instances: u32,
        output_size: u32,
        input_size: u32,
        timeout: u32,
        security: *const windows_sys::Win32::Security::SECURITY_ATTRIBUTES,
    ) -> HANDLE;
    fn ConnectNamedPipe(handle: HANDLE, overlapped: *mut std::ffi::c_void) -> i32;
    fn ReadFile(
        handle: HANDLE,
        buffer: *mut std::ffi::c_void,
        size: u32,
        read: *mut u32,
        overlapped: *mut std::ffi::c_void,
    ) -> i32;
}

// Only this factory can construct a reader. Standard ChildStdout/PipeReader
// handles lack FILE_WRITE_ATTRIBUTES, so their wait mode cannot be changed.
pub struct Pipe(File);
static SERIAL: AtomicU64 = AtomicU64::new(0);

pub fn pipe() -> Result<(Pipe, File), String> {
    const PIPE_ACCESS_INBOUND: u32 = 0x0001;
    const FILE_FLAG_FIRST_PIPE_INSTANCE: u32 = 0x00080000;
    const PIPE_NOWAIT: u32 = 0x0001;
    let name = format!(
        "\\\\.\\pipe\\cc-live-output-{}-{:p}-{}",
        std::process::id(),
        &SERIAL,
        SERIAL.fetch_add(1, Ordering::Relaxed)
    );
    let name: Vec<u16> = name.encode_utf16().chain(Some(0)).collect();
    let source = unsafe {
        CreateNamedPipeW(
            name.as_ptr(),
            PIPE_ACCESS_INBOUND | FILE_FLAG_FIRST_PIPE_INSTANCE,
            PIPE_NOWAIT,
            1,
            65536,
            65536,
            0,
            std::ptr::null(),
        )
    };
    if source == INVALID_HANDLE_VALUE {
        return Err(std::io::Error::last_os_error().to_string());
    }
    let source = unsafe { OwnedHandle::from_raw_handle(source) };
    let sink = unsafe {
        CreateFileW(
            name.as_ptr(),
            GENERIC_WRITE,
            0,
            std::ptr::null(),
            OPEN_EXISTING,
            FILE_ATTRIBUTE_NORMAL,
            std::ptr::null_mut(),
        )
    };
    if sink == INVALID_HANDLE_VALUE {
        return Err(std::io::Error::last_os_error().to_string());
    }
    let sink = unsafe { OwnedHandle::from_raw_handle(sink) };
    if unsafe { ConnectNamedPipe(source.as_raw_handle(), std::ptr::null_mut()) } == 0
        && unsafe { GetLastError() } != ERROR_PIPE_CONNECTED
    {
        return Err(std::io::Error::last_os_error().to_string());
    }
    Ok((Pipe(File::from(source)), File::from(sink)))
}

struct Stream {
    pipe: Pipe,
    bytes: Vec<u8>,
    eof: bool,
}

pub struct Capture {
    streams: Vec<Stream>,
}

impl Capture {
    pub fn new(pipes: Vec<Pipe>) -> Self {
        let streams = pipes
            .into_iter()
            .map(|pipe| {
                // PIPE_NOWAIT is used for synchronous, owned reads, not overlapped
                // I/O. Empty live pipes return ERROR_NO_DATA instead of waiting for
                // a writer's EOF. There is never another reader on this handle.
                Stream {
                    pipe,
                    bytes: Vec::new(),
                    eof: false,
                }
            })
            .collect();
        Self { streams }
    }

    pub fn poll(&mut self) -> Result<(), String> {
        for stream in &mut self.streams {
            // A busy writer cannot starve process cancellation or deadlines.
            for _ in 0..16 {
                if stream.eof {
                    break;
                }
                let mut buffer = [0u8; 4096];
                let mut count = 0u32;
                // std::fs::File::read normalizes ERROR_NO_DATA to Ok(0) on
                // Windows. Preserve the native distinction between a live,
                // empty pipe and EOF instead of losing late child output.
                if unsafe {
                    ReadFile(
                        stream.pipe.0.as_raw_handle(),
                        buffer.as_mut_ptr().cast(),
                        buffer.len() as u32,
                        &mut count,
                        std::ptr::null_mut(),
                    )
                } != 0
                {
                    if count == 0 {
                        stream.eof = true;
                        break;
                    }
                    stream.bytes.extend_from_slice(&buffer[..count as usize]);
                } else {
                    match unsafe { GetLastError() } {
                        ERROR_NO_DATA => break,
                        ERROR_BROKEN_PIPE => {
                            stream.eof = true;
                            break;
                        }
                        reason => {
                            return Err(format!(
                                "Output capture failed: {}",
                                std::io::Error::from_raw_os_error(reason as i32)
                            ))
                        }
                    }
                }
            }
        }
        Ok(())
    }

    pub fn finish(
        mut self,
        status: Result<i32, String>,
        stop_confirmed: bool,
        deadline: Instant,
    ) -> Result<Output, String> {
        if !stop_confirmed {
            // No extra I/O or join after an unconfirmed stop. Dropping these
            // owned nonblocking handles ends all capture work immediately.
            return Err(self.error(status, "build process exit unconfirmed; output incomplete; capture has no pending I/O or reader workers"));
        }
        while !self.streams.iter().all(|stream| stream.eof) {
            if Instant::now() >= deadline {
                return Err(self.error(
                    status,
                    "output incomplete: original cleanup deadline exhausted",
                ));
            }
            if let Err(error) = self.poll() {
                return Err(self.error(status, &error));
            }
            if !self.streams.iter().all(|stream| stream.eof) {
                std::thread::sleep(
                    deadline
                        .saturating_duration_since(Instant::now())
                        .min(Duration::from_millis(1)),
                );
            }
        }
        let (stdout, stderr) = self.text();
        let tail_budget = if self.streams.len() == 1 { 4000 } else { 2000 };
        Ok(Output {
            code: status.map_err(|reason| {
                format!(
                    "{reason}\n{}{}",
                    crate::tail(&stdout, tail_budget),
                    crate::tail(&stderr, tail_budget)
                )
            })?,
            stdout,
            stderr,
        })
    }

    fn text(&self) -> (String, String) {
        let decode = |bytes: &[u8]| {
            String::from_utf8_lossy(bytes)
                .replace("\r\n", "\n")
                .replace('\r', "\n")
        };
        (
            decode(&self.streams[0].bytes),
            self.streams
                .get(1)
                .map_or_else(String::new, |stream| decode(&stream.bytes)),
        )
    }

    fn error(&self, status: Result<i32, String>, failure: &str) -> String {
        let (stdout, stderr) = self.text();
        let tail_budget = if self.streams.len() == 1 { 4000 } else { 2000 };
        let reason = status
            .err()
            .unwrap_or_else(|| "command output not confirmed".into());
        format!(
            "{reason}; {failure}\n{}{}",
            crate::tail(&stdout, tail_budget),
            crate::tail(&stderr, tail_budget)
        )
    }
}
