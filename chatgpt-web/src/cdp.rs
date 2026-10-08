//! 専用プロフィールの Chrome を起動・再利用し、CDP（WebSocket）で操作する最小限の処理。

use serde_json::{Value, json};
use std::fs;
use std::io;
use std::net::{SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread::sleep;
use std::time::{Duration, Instant};
use tungstenite::{Message, WebSocket};

const IO_TIMEOUT: Duration = Duration::from_secs(30);

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
        let mut child = cmd.spawn()?;
        let deadline = Instant::now() + Duration::from_secs(30);
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
            if let Ok(Some(_)) = child.try_wait() {
                return Err(io::Error::other(
                    "Chrome がすぐ終了しました。専用プロフィールの Chrome が CDP なしで開いている場合は閉じてから再実行してください",
                ));
            }
            sleep(Duration::from_millis(200));
        }
        let _ = child.kill();
        let _ = child.wait();
        Err(io::Error::other("Chrome の CDP に接続できませんでした"))
    }

    pub fn launched(&self) -> bool {
        self.child.is_some()
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

impl Drop for Browser {
    fn drop(&mut self) {
        if let Some(child) = self.child.take() {
            let _ = self.cdp.call("Browser.close", json!({}));
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
