#![cfg_attr(windows, windows_subsystem = "windows")]
//! Test-only native process tree. Avoid a PowerShell startup race in cancellation checks.
use std::process::{Command, Stdio};
use std::time::Duration;

fn main() {
    let mut args = std::env::args_os().skip(1);
    match args.next().as_deref().and_then(std::ffi::OsStr::to_str) {
        Some("descendant") => std::thread::sleep(Duration::from_secs(30)),
        Some("parent") => {
            let marker = args.next().expect("marker path");
            assert!(args.next().is_none());
            let mut command = Command::new(std::env::current_exe().unwrap());
            command
                .arg("descendant")
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::null());
            #[cfg(windows)]
            {
                use std::os::windows::process::CommandExt;
                command.creation_flags(0x08000000);
            }
            let mut child = command.spawn().unwrap();
            #[cfg(windows)]
            let born = {
                use std::os::windows::io::AsRawHandle;
                use windows_sys::Win32::{
                    Foundation::FILETIME, System::Threading::GetProcessTimes,
                };
                let (mut created, mut exited, mut kernel, mut user) = (
                    FILETIME::default(),
                    FILETIME::default(),
                    FILETIME::default(),
                    FILETIME::default(),
                );
                assert_ne!(
                    unsafe {
                        GetProcessTimes(
                            child.as_raw_handle(),
                            &mut created,
                            &mut exited,
                            &mut kernel,
                            &mut user,
                        )
                    },
                    0
                );
                (u64::from(created.dwHighDateTime) << 32) | u64::from(created.dwLowDateTime)
            };
            #[cfg(not(windows))]
            let born = 0u64;
            std::fs::write(marker, format!("{} {born}", child.id())).unwrap();
            let _ = child.wait();
        }
        _ => panic!("parent MARKER | descendant"),
    }
}
