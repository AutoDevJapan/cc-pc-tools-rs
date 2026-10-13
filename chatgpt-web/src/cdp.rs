//! 専用プロフィールの Chrome を起動・再利用し、CDP（WebSocket）で操作する最小限の処理。

use serde_json::{Value, json};
use std::fs;
use std::io::{self, Read};
use std::net::{SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread::sleep;
use std::time::{Duration, Instant};
use tungstenite::{Message, WebSocket};

const IO_TIMEOUT: Duration = Duration::from_secs(30);

#[cfg(windows)]
mod startup {
    use super::*;
    use std::ffi::c_void;

    #[link(name = "kernel32")]
    unsafe extern "system" {
        fn CreateMutexW(attributes: *const c_void, owned: i32, name: *const u16) -> *mut c_void;
        fn CloseHandle(handle: *mut c_void) -> i32;
    }

    pub(super) struct Admission(*mut c_void);
    impl Drop for Admission {
        fn drop(&mut self) {
            // Each successful CreateMutexW returns one owned kernel handle.
            unsafe { CloseHandle(self.0) };
        }
    }

    pub(super) fn admit(deadline: Instant) -> io::Result<Admission> {
        let name: Vec<u16> = "Local\\ChromeProcessSingletonStartup!\0"
            .encode_utf16()
            .collect();
        admit_named(&name, deadline, || {})
    }

    fn admit_named(
        name: &[u16],
        deadline: Instant,
        mut denied: impl FnMut(),
    ) -> io::Result<Admission> {
        loop {
            if Instant::now() >= deadline {
                return Err(io::Error::new(
                    io::ErrorKind::TimedOut,
                    "Chrome の起動用mutexの受付期限を過ぎました",
                ));
            }
            // Do not acquire ownership: Chrome itself must acquire this mutex.
            // Holding a handle reserves its default, same-user DACL until CDP
            // is ready. A different service/user waits BEFORE launching Chrome,
            // rather than receiving Chrome exit21 from its global startup lock.
            // Neither the existing DACL nor the caller's token is changed.
            let handle = unsafe { CreateMutexW(std::ptr::null(), 0, name.as_ptr()) };
            if !handle.is_null() {
                return Ok(Admission(handle));
            }
            let error = io::Error::last_os_error();
            if error.raw_os_error() != Some(5) {
                return Err(error);
            }
            denied();
            sleep(
                Duration::from_millis(20).min(deadline.saturating_duration_since(Instant::now())),
            );
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        use std::sync::atomic::{AtomicU64, Ordering};

        #[repr(C)]
        struct SecurityAttributes {
            length: u32,
            descriptor: *mut c_void,
            inherit: i32,
        }
        #[link(name = "advapi32")]
        unsafe extern "system" {
            fn ConvertStringSecurityDescriptorToSecurityDescriptorW(
                text: *const u16,
                revision: u32,
                descriptor: *mut *mut c_void,
                size: *mut u32,
            ) -> i32;
        }
        #[link(name = "kernel32")]
        unsafe extern "system" {
            fn LocalFree(memory: *mut c_void) -> *mut c_void;
            fn WaitForSingleObject(handle: *mut c_void, milliseconds: u32) -> u32;
            fn ReleaseMutex(handle: *mut c_void) -> i32;
        }

        fn denied_holder() -> (Vec<u16>, Admission) {
            static NEXT: AtomicU64 = AtomicU64::new(0);
            let name: Vec<u16> = format!(
                "Local\\CC-Chrome-2571-Test-{}-{}\0",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            )
            .encode_utf16()
            .collect();
            // Only this newly created test object gets a restrictive DACL. It
            // reproduces access denial without changing Chrome's real mutex,
            // the owner's token, the service, or any pre-existing resource.
            let sddl: Vec<u16> = "D:(D;;GA;;;WD)\0".encode_utf16().collect();
            let mut descriptor = std::ptr::null_mut();
            assert_ne!(
                unsafe {
                    ConvertStringSecurityDescriptorToSecurityDescriptorW(
                        sddl.as_ptr(),
                        1,
                        &mut descriptor,
                        std::ptr::null_mut(),
                    )
                },
                0
            );
            let attributes = SecurityAttributes {
                length: std::mem::size_of::<SecurityAttributes>() as u32,
                descriptor,
                inherit: 0,
            };
            let handle = unsafe {
                CreateMutexW(
                    (&attributes as *const SecurityAttributes).cast(),
                    0,
                    name.as_ptr(),
                )
            };
            unsafe { LocalFree(descriptor) };
            assert!(!handle.is_null());
            (name, Admission(handle))
        }

        #[test]
        fn different_identity_access_denial_waits_before_chrome_is_launched() {
            let (name, holder) = denied_holder();
            let (tx, rx) = std::sync::mpsc::channel();
            let waiter = std::thread::spawn(move || {
                let mut signalled = false;
                let admission = admit_named(&name, Instant::now() + Duration::from_secs(5), || {
                    if !signalled {
                        tx.send(()).unwrap();
                        signalled = true;
                    }
                })
                .unwrap();
                // Admission holds only a handle; Chrome can acquire ownership.
                assert_eq!(unsafe { WaitForSingleObject(admission.0, 0) }, 0);
                assert_ne!(unsafe { ReleaseMutex(admission.0) }, 0);
                signalled
            });
            let observed = rx.recv_timeout(Duration::from_secs(5));
            drop(holder);
            let waited = waiter.join().unwrap();
            assert!(observed.is_ok(), "must exercise real Windows access denial");
            assert!(waited);
        }

        #[test]
        fn persistent_access_denial_does_not_extend_the_startup_deadline() {
            let (name, _holder) = denied_holder();
            let mut denials = 0;
            let result = admit_named(&name, Instant::now() + Duration::from_millis(50), || {
                denials += 1
            });
            assert!(denials > 0);
            assert_eq!(result.err().unwrap().kind(), io::ErrorKind::TimedOut);
        }
    }
}

pub struct Cdp {
    ws: WebSocket<TcpStream>,
    next: u64,
}

impl Cdp {
    pub fn connect(port: u16, path: &str) -> io::Result<Self> {
        let addr = SocketAddr::from(([127, 0, 0, 1], port));
        let stream = TcpStream::connect_timeout(&addr, Duration::from_secs(5))?;
        stream.set_read_timeout(Some(IO_TIMEOUT))?;
        stream.set_write_timeout(Some(IO_TIMEOUT))?;
        let url = format!("ws://127.0.0.1:{port}{path}");
        let (ws, _) = tungstenite::client(url.as_str(), stream).map_err(io::Error::other)?;
        Ok(Self { ws, next: 0 })
    }

    /// CDP のコマンドを送り、同じ id の応答を返す。途中のイベントは読み捨てる。
    pub fn call(&mut self, method: &str, params: Value) -> io::Result<Value> {
        self.next += 1;
        let id = self.next;
        let msg = json!({"id": id, "method": method, "params": params}).to_string();
        self.ws.send(Message::text(msg)).map_err(io::Error::other)?;
        let deadline = Instant::now() + IO_TIMEOUT * 2;
        while Instant::now() < deadline {
            let text = match self.ws.read().map_err(io::Error::other)? {
                Message::Text(t) => t,
                Message::Close(_) => return Err(io::Error::other("CDP の接続が閉じられました")),
                _ => continue,
            };
            let v: Value = serde_json::from_str(text.as_str()).map_err(io::Error::other)?;
            if v["id"].as_u64() != Some(id) {
                continue;
            }
            if let Some(err) = v.get("error") {
                return Err(io::Error::other(format!("{method}: {err}")));
            }
            return Ok(v["result"].clone());
        }
        Err(io::Error::new(
            io::ErrorKind::TimedOut,
            format!("{method} の応答がありません"),
        ))
    }

    /// ページ内で式を評価して値を返す（JSON で表せる値のみ）。
    pub fn eval(&mut self, expr: &str) -> io::Result<Value> {
        let r = self.call(
            "Runtime.evaluate",
            json!({"expression": expr, "returnByValue": true, "awaitPromise": true}),
        )?;
        if let Some(ex) = r.get("exceptionDetails") {
            return Err(io::Error::other(format!("ページ内の処理でエラー: {ex}")));
        }
        Ok(r["result"]["value"].clone())
    }
}

/// `DevToolsActivePort` は1行目にポート、2行目にブラウザの WebSocket パスを持つ。
pub fn parse_active_port(text: &str) -> Option<(u16, String)> {
    let mut lines = text.lines();
    let port = lines.next()?.trim().parse().ok().filter(|p| *p != 0)?;
    let path = lines.next()?.trim();
    path.starts_with("/devtools/browser/")
        .then(|| (port, path.to_string()))
}

pub fn find_chrome() -> Option<PathBuf> {
    if let Some(p) = std::env::var_os("CHATGPT_WEB_CHROME") {
        return Some(PathBuf::from(p));
    }
    ["ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"]
        .iter()
        .filter_map(std::env::var_os)
        .map(|base| PathBuf::from(base).join(r"Google\Chrome\Application\chrome.exe"))
        .find(|p| p.is_file())
}

pub struct Browser {
    pub cdp: Cdp,
    pub port: u16,
    /// 自分で起動した場合だけ持つ。終了時に閉じる。
    child: Option<Child>,
}

fn chrome_command(chrome: &Path, profile: &Path, visible: bool, headless: bool) -> Command {
    let mut cmd = Command::new(chrome);
    cmd.arg(format!("--user-data-dir={}", profile.display()))
        .args([
            "--remote-debugging-port=0",
            "--no-first-run",
            "--no-default-browser-check",
            "--lang=en-US",
            "--accept-lang=en-US,en",
            "--disable-backgrounding-occluded-windows",
            "--disable-renderer-backgrounding",
            "--disable-background-timer-throttling",
            "--window-size=1280,900",
        ])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    if headless {
        cmd.arg("--headless=new");
    } else if !visible {
        cmd.arg("--window-position=-32000,-32000");
    }
    cmd.arg("about:blank");
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }
    cmd
}

impl Browser {
    /// 専用プロフィールの Chrome が CDP 付きで動いていれば再利用し、なければ起動する。
    /// 通常は `visible` が false のとき画面外に窓を置く。
    /// 試験では `CHATGPT_WEB_HEADLESS=1` で窓を作らずに起動する。
    pub fn open(profile: &Path, visible: bool) -> io::Result<Self> {
        let port_file = profile.join("DevToolsActivePort");
        if let Some((port, path)) = fs::read_to_string(&port_file)
            .ok()
            .as_deref()
            .and_then(parse_active_port)
            && let Ok(mut cdp) = Cdp::connect(port, &path)
            && cdp.call("Browser.getVersion", json!({})).is_ok()
        {
            return Ok(Self {
                cdp,
                port,
                child: None,
            });
        }
        let chrome = find_chrome().ok_or_else(|| {
            io::Error::other("Chrome が見つかりません（CHATGPT_WEB_CHROME で指定できます）")
        })?;
        fs::create_dir_all(profile)?;
        let _ = fs::remove_file(&port_file);
        let headless =
            std::env::var_os("CHATGPT_WEB_HEADLESS").as_deref() == Some(std::ffi::OsStr::new("1"));
        let mut cmd = chrome_command(&chrome, profile, visible, headless);
        // Only local, headless fixtures may record Chrome diagnostics. Never
        // collect stderr from the owner's logged-in browser/profile.
        let diagnostics = headless
            && std::env::var_os("CHATGPT_WEB_FIXTURE").as_deref()
                == Some(std::ffi::OsStr::new("1"));
        let diagnostic_path = profile.join("chrome-startup.log");
        if diagnostics {
            cmd.arg("--enable-logging=stderr")
                .stderr(fs::File::create(&diagnostic_path)?);
        }
        let deadline = Instant::now() + Duration::from_secs(30);
        #[cfg(windows)]
        let _startup_admission = if headless {
            Some(startup::admit(deadline)?)
        } else {
            None
        };
        let mut child = cmd.spawn()?;
        while Instant::now() < deadline {
            if let Some((port, path)) = fs::read_to_string(&port_file)
                .ok()
                .as_deref()
                .and_then(parse_active_port)
                && let Ok(cdp) = Cdp::connect(port, &path)
            {
                return Ok(Self {
                    cdp,
                    port,
                    child: Some(child),
                });
            }
            if let Ok(Some(status)) = child.try_wait()
                && !status.success()
            {
                return Err(startup_error(
                    format!(
                        "Chrome の起動処理が失敗しました（終了コード {:?}）",
                        status.code()
                    ),
                    diagnostics.then_some(diagnostic_path.as_path()),
                ));
            }
            // On Windows the Chrome launcher can exit successfully before its
            // replacement browser writes DevToolsActivePort. The owned profile's
            // live CDP endpoint, rather than the launcher PID, certifies readiness.
            sleep(Duration::from_millis(200));
        }
        let _ = child.kill();
        let _ = child.wait();
        Err(startup_error(
            "Chrome の CDP に接続できませんでした".into(),
            diagnostics.then_some(diagnostic_path.as_path()),
        ))
    }

    /// 新しいタブを開き、そのページへ接続する。
    pub fn new_tab(&mut self, url: &str, new_window: bool) -> io::Result<Tab> {
        let r = self.cdp.call(
            "Target.createTarget",
            json!({"url": url, "newWindow": new_window}),
        )?;
        let id = r["targetId"]
            .as_str()
            .ok_or_else(|| io::Error::other("タブを開けませんでした"))?
            .to_string();
        let cdp = Cdp::connect(self.port, &format!("/devtools/page/{id}"))?;
        Ok(Tab { cdp, id })
    }

    pub fn close_tab(&mut self, tab: Tab) {
        let _ = self
            .cdp
            .call("Target.closeTarget", json!({"targetId": tab.id}));
    }
}

fn startup_error(mut message: String, diagnostic_path: Option<&Path>) -> io::Error {
    if let Some(path) = diagnostic_path
        && let Ok(file) = fs::File::open(path)
    {
        let mut bytes = Vec::new();
        if file.take(8192).read_to_end(&mut bytes).is_ok() && !bytes.is_empty() {
            message.push_str("\nChrome fixture stderr (first 8192 bytes):\n");
            message.push_str(&String::from_utf8_lossy(&bytes));
        }
    }
    io::Error::other(message)
}

impl Drop for Browser {
    fn drop(&mut self) {
        if let Some(child) = self.child.take() {
            let _ = self.cdp.call("Browser.close", json!({}));
            // The launcher may already have exited while the browser is still
            // shutting down. Keep the profile alive until that browser stops.
            let deadline = Instant::now() + Duration::from_secs(15);
            let addr = SocketAddr::from(([127, 0, 0, 1], self.port));
            while Instant::now() < deadline
                && TcpStream::connect_timeout(&addr, Duration::from_millis(200)).is_ok()
            {
                sleep(Duration::from_millis(50));
            }
            reap(child);
        }
    }
}

fn reap(mut child: Child) {
    let deadline = Instant::now() + Duration::from_secs(15);
    while Instant::now() < deadline {
        if let Ok(Some(_)) = child.try_wait() {
            return;
        }
        sleep(Duration::from_millis(200));
    }
    let _ = child.kill();
    let _ = child.wait();
}

pub struct Tab {
    pub cdp: Cdp,
    id: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fixture_startup_error_keeps_exit_and_bounds_diagnostics() {
        let dir = tempfile::tempdir().unwrap();
        let log = dir.path().join("startup.log");
        fs::write(&log, "x".repeat(9000)).unwrap();
        let error = startup_error("exit 21".into(), Some(&log)).to_string();
        assert!(error.starts_with("exit 21\n"));
        assert!(error.ends_with(&"x".repeat(8192)));
        assert!(!error.contains(&"x".repeat(8193)));
        assert_eq!(startup_error("exit 21".into(), None).to_string(), "exit 21");
    }

    #[test]
    fn test_chrome_has_no_window_even_when_visible_is_requested() {
        for visible in [false, true] {
            let cmd = chrome_command(
                Path::new("chrome"),
                Path::new("test-profile"),
                visible,
                true,
            );
            let args: Vec<_> = cmd.get_args().map(|a| a.to_string_lossy()).collect();
            assert!(args.iter().any(|a| a == "--headless=new"));
            assert!(!args.iter().any(|a| a.starts_with("--window-position=")));
            assert!(args.iter().any(|a| a == "--remote-debugging-port=0"));
        }
    }

    #[test]
    fn production_chrome_keeps_its_window_behavior() {
        for visible in [false, true] {
            let cmd = chrome_command(Path::new("chrome"), Path::new("profile"), visible, false);
            let args: Vec<_> = cmd.get_args().map(|a| a.to_string_lossy()).collect();
            assert!(!args.iter().any(|a| a.starts_with("--headless")));
            assert_eq!(
                args.iter().any(|a| a == "--window-position=-32000,-32000"),
                !visible
            );
        }
    }

    #[test]
    fn active_port_file_is_parsed_strictly() {
        assert_eq!(
            parse_active_port("9222\n/devtools/browser/abc\n"),
            Some((9222, "/devtools/browser/abc".into()))
        );
        assert_eq!(parse_active_port("0\n/devtools/browser/abc"), None);
        assert_eq!(parse_active_port("9222\n"), None);
        assert_eq!(parse_active_port("x\n/devtools/browser/abc"), None);
        assert_eq!(parse_active_port("9222\n/devtools/page/abc"), None);
    }
}
