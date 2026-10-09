#![cfg_attr(windows, windows_subsystem = "windows")]
//! Test-only native process tree with explicit readiness and release barriers.
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

fn publish(marker: &Path) {
    #[cfg(windows)]
    let born = {
        use windows_sys::Win32::{
            Foundation::FILETIME,
            System::Threading::{GetCurrentProcess, GetProcessTimes},
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
                    GetCurrentProcess(),
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
    let staging = marker.with_extension("staging");
    std::fs::write(&staging, format!("{} {born}", std::process::id())).unwrap();
    std::fs::rename(staging, marker).unwrap();
}

fn spawn(mode: &str, marker: &Path) -> Child {
    let mut command = Command::new(std::env::current_exe().unwrap());
    command
        .arg(mode)
        .arg(marker)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    command.spawn().unwrap()
}

fn wait_release(marker: &Path) {
    let deadline = Instant::now() + Duration::from_secs(60);
    while !marker.exists() {
        assert!(Instant::now() < deadline, "fixture release barrier expired");
        std::thread::sleep(Duration::from_millis(1));
    }
}

#[expect(
    clippy::zombie_processes,
    reason = "This fixture deliberately exits its root with live descendants; the owning job must drain them"
)]
fn exit_with_live_descendants(marker: &Path) {
    publish(&marker.with_extension("root"));
    let _child = spawn("worker", marker);
    wait_release(&marker.with_extension("root-release"));
}

fn main() {
    let mut args = std::env::args_os().skip(1);
    let mode = args.next().unwrap();
    let marker = args.next().expect("marker path");
    let marker = Path::new(&marker);
    assert!(args.next().is_none());
    match mode.to_str().unwrap() {
        "descendant" => {
            publish(marker);
            // Lifetime is controlled by the test, rather than a fixed sleep.
            wait_release(&marker.with_extension("leaf-release"));
        }
        "worker" | "worker-delayed" => {
            publish(&marker.with_extension("worker"));
            if mode == "worker-delayed" {
                wait_release(&marker.with_extension("spawn-release"));
            }
            let mut child = spawn("descendant", marker);
            let _ = child.wait();
        }
        "parent-exit" => exit_with_live_descendants(marker),
        "parent" | "parent-delayed" => {
            publish(&marker.with_extension("root"));
            let worker = if mode == "parent-delayed" {
                "worker-delayed"
            } else {
                "worker"
            };
            let mut child = spawn(worker, marker);
            let _ = child.wait();
        }
        _ => panic!("unknown fixture mode"),
    }
}
