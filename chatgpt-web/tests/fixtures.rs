//! ChatGPT の画面を模した tests/fixtures/ の HTML を、実際の Chrome で操作して確かめる。
//! Chrome が無い開発環境では画面試験を飛ばす。CI では Chrome を必須とする。

use std::fs;
use std::net::{SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Command, Output, Stdio};
use std::thread::sleep;
use std::time::{Duration, Instant};

fn chrome_available() -> bool {
    let available = std::env::var_os("CHATGPT_WEB_CHROME").is_some()
        || ["ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"]
            .iter()
            .filter_map(std::env::var_os)
            .any(|b| {
                PathBuf::from(b)
                    .join(r"Google\Chrome\Application\chrome.exe")
                    .is_file()
            });
    assert!(
        available || std::env::var_os("CI").is_none(),
        "CI requires Chrome"
    );
    available
}

fn fixture_url(name: &str) -> String {
    let p = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(name);
    format!("file:///{}", p.to_string_lossy().replace('\\', "/"))
}

fn fixture_command(home: &Path, page: &str, args: &[&str]) -> Command {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_chatgpt_web"));
    cmd.args(args)
        .env("CHATGPT_WEB_HOME", home)
        .env("CHATGPT_WEB_URL", fixture_url(page))
        .env("CHATGPT_WEB_DISCORD", "off")
        .env("CHATGPT_WEB_FIXTURE", "1")
        .env("CHATGPT_WEB_HEADLESS", "1");
    cmd
}

fn run(home: &Path, page: &str, args: &[&str]) -> Output {
    fixture_command(home, page, args).output().unwrap()
}

fn ask(home: &Path, page: &str, prompt: &str, extra: &[&str]) -> Output {
    let q = home.join("q.md");
    fs::create_dir_all(home).unwrap();
    fs::write(&q, prompt).unwrap();
    let mut args = vec!["ask", "--prompt-file", q.to_str().unwrap()];
    args.extend_from_slice(extra);
    run(home, page, &args)
}

fn login_state(home: &Path) -> String {
    let v: serde_json::Value =
        serde_json::from_str(&fs::read_to_string(home.join("state.json")).unwrap()).unwrap();
    v["login"].as_str().unwrap().to_string()
}

/// 自分で起動した Chrome が終了し、CDP のポートが閉じていること
fn assert_browser_closed(home: &Path) {
    let Ok(text) = fs::read_to_string(home.join("profile/DevToolsActivePort")) else {
        return;
    };
    let port: u16 = text.lines().next().unwrap().parse().unwrap();
    let addr = SocketAddr::from(([127, 0, 0, 1], port));
    assert!(
        TcpStream::connect_timeout(&addr, Duration::from_secs(1)).is_err(),
        "Chrome が残っています"
    );
}

fn stdout(o: &Output) -> String {
    String::from_utf8_lossy(&o.stdout).into_owned()
}

fn stderr(o: &Output) -> String {
    String::from_utf8_lossy(&o.stderr).into_owned()
}

#[test]
fn fixture_chrome_runs_headless_and_closes() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let mut child = fixture_command(
        dir.path(),
        "chat.html",
        &["probe", "page:navigator.userAgent", "wait:2"],
    )
    .stdout(Stdio::piped())
    .stderr(Stdio::piped())
    .spawn()
    .unwrap();
    // Read and connect to the port while Chrome is alive. Chrome may remove
    // DevToolsActivePort on shutdown, so checking only that file afterwards
    // would not prove the browser actually exited.
    let deadline = Instant::now() + Duration::from_secs(30);
    let active = loop {
        if let Ok(text) = fs::read_to_string(dir.path().join("profile/DevToolsActivePort"))
            && let Some(port) = text.lines().next().and_then(|s| s.parse::<u16>().ok())
        {
            let addr = SocketAddr::from(([127, 0, 0, 1], port));
            if TcpStream::connect_timeout(&addr, Duration::from_secs(1)).is_ok() {
                break Some(addr);
            }
        }
        if child.try_wait().unwrap().is_some() || Instant::now() >= deadline {
            break None;
        }
        sleep(Duration::from_millis(50));
    };
    let o = child.wait_with_output().unwrap();
    assert_eq!(o.status.code(), Some(0), "{}", stderr(&o));
    assert!(stdout(&o).contains("HeadlessChrome/"), "{}", stdout(&o));
    let addr = active.expect("Chrome's live CDP port was not observed");
    assert!(
        TcpStream::connect_timeout(&addr, Duration::from_secs(1)).is_err(),
        "Chrome is still listening after probe exited"
    );
}

#[test]
fn ask_returns_the_answer_as_markdown_and_closes_chrome() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let home = dir.path();
    let o = ask(
        home,
        "chat.html",
        "設計を見てください\n2行目 <tag> & \"quote\"",
        &["--model", "gpt-5-thinking", "--timeout", "120"],
    );
    let out = stdout(&o);
    assert_eq!(
        o.status.code(),
        Some(0),
        "stdout={out} stderr={}",
        stderr(&o)
    );
    for want in [
        "### Answer",
        "Model: `gpt-5-thinking` and **bold**.",
        "```rust\nfn main() {\n    println!(\"hi\");\n}\n```",
        "- first\n- second\n  - nested",
        "1. one\n2. two",
        "| k | v |\n| --- | --- |\n| a | 1 |",
        "> 設計を見てください\n> 2行目 <tag> & \"quote\"",
    ] {
        assert!(out.contains(want), "missing {want:?} in:\n{out}");
    }
    assert!(!out.contains("Copy code"), "{out}");
    assert_eq!(login_state(home), "logged_in");
    assert_browser_closed(home);
}

#[test]
fn logged_out_page_exits_3_and_is_recorded() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let o = ask(dir.path(), "logged-out.html", "q", &["--timeout", "60"]);
    assert_eq!(o.status.code(), Some(3), "{}", stderr(&o));
    assert!(stderr(&o).contains("ログインしていません"));
    assert_eq!(login_state(dir.path()), "logged_out");
    let s = run(dir.path(), "logged-out.html", &["status"]);
    assert_eq!(s.status.code(), Some(3));
    assert_browser_closed(dir.path());
}

#[test]
fn deactivated_account_exits_4() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let o = run(dir.path(), "deactivated.html", &["check"]);
    assert_eq!(o.status.code(), Some(4), "{}", stderr(&o));
    assert!(stderr(&o).contains("止められている"));
    assert_eq!(login_state(dir.path()), "blocked");
}

#[test]
fn check_on_logged_in_page_succeeds_and_status_reads_it() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let o = run(dir.path(), "chat.html", &["check"]);
    assert_eq!(o.status.code(), Some(0), "{}", stderr(&o));
    let s = run(dir.path(), "chat.html", &["status"]);
    assert_eq!(s.status.code(), Some(0), "{}", stderr(&s));
    assert_browser_closed(dir.path());
}

#[test]
fn observed_owner_composer_is_detected_without_the_old_id() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let result = run(
        dir.path(),
        "chat.html",
        &[
            "probe",
            "page:(()=>{const b=q('#prompt-textarea');b.removeAttribute('id');b.setAttribute('role','textbox');b.setAttribute('aria-label','Ask ChatGPT');return !!q(COMPOSER);})()",
            "page:state()",
        ],
    );
    assert_eq!(result.status.code(), Some(0), "{}", stderr(&result));
    assert!(stdout(&result).contains("true"));
    assert!(stdout(&result).contains("logged_in"));
    assert_browser_closed(dir.path());
}

#[test]
fn answer_errors_and_limits_have_their_own_codes() {
    if !chrome_available() {
        return;
    }
    for (page, code, text) in [
        ("chat-error.html", 1, "エラー"),
        ("chat-limit.html", 7, "利用上限"),
        ("chat-expire.html", 3, "ログイン"),
        ("chat-slow.html", 5, "制限時間"),
        ("challenge.html", 5, "人間確認"),
    ] {
        let dir = tempfile::tempdir().unwrap();
        let o = ask(dir.path(), page, "q", &["--timeout", "12"]);
        assert_eq!(o.status.code(), Some(code), "{page}: {}", stderr(&o));
        assert!(stderr(&o).contains(text), "{page}: {}", stderr(&o));
        assert!(stdout(&o).is_empty(), "{page}");
        assert_browser_closed(dir.path());
    }
}

#[test]
fn composer_seen_before_its_script_loads_waits_for_page_initialization() {
    if !chrome_available() {
        return;
    }
    use std::io::{BufRead, Read, Write};
    let server = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let address = server.local_addr().unwrap();
    server.set_nonblocking(true).unwrap();
    let handler = std::thread::spawn(move || {
        // This test asserts document initialization, not Chrome cold-start speed.
        // Keep the 2s blocked script and answer assertions; allow startup on CI hosts.
        let until = Instant::now() + Duration::from_secs(60);
        let mut script_sent = false;
        while Instant::now() < until {
            let Ok((mut socket, _)) = server.accept() else {
                sleep(Duration::from_millis(10));
                continue;
            };
            socket.set_nonblocking(false).unwrap();
            socket
                .set_read_timeout(Some(Duration::from_secs(2)))
                .unwrap();
            let mut request = Vec::new();
            let read = std::io::BufReader::new(&mut socket)
                .take(4096)
                .read_until(b'\n', &mut request);
            // Chrome can open and close a speculative connection without GET.
            // It is not a request, and must not stop the fixture server.
            if read.is_err() || !request.ends_with(b"\r\n") {
                continue;
            }
            let script = String::from_utf8_lossy(&request).starts_with("GET /mock.js ");
            let body = if script {
                // Composer DOM exists while its blocking script is still loading.
                // Premature paste/insertText would miss the event listeners.
                sleep(Duration::from_secs(2));
                include_str!("fixtures/mock.js")
            } else {
                include_str!("fixtures/chat.html")
            };
            let header = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: {}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                if script {
                    "text/javascript"
                } else {
                    "text/html; charset=utf-8"
                },
                body.len()
            );
            script_sent = script
                && socket.write_all(header.as_bytes()).is_ok()
                && socket.write_all(body.as_bytes()).is_ok();
            if !script && socket.write_all(header.as_bytes()).is_ok() {
                let _ = socket.write_all(body.as_bytes());
            }
            if script_sent {
                break;
            }
        }
        script_sent
    });
    // Explicitly cover the idle/closed connections that Chrome may speculate.
    drop(TcpStream::connect(address).unwrap());
    let idle = TcpStream::connect(address).unwrap();
    let dir = tempfile::tempdir().unwrap();
    let q = dir.path().join("q.md");
    fs::write(&q, "delayed script question").unwrap();
    let result = fixture_command(
        dir.path(),
        "chat.html",
        &[
            "ask",
            "--prompt-file",
            q.to_str().unwrap(),
            "--timeout",
            "60",
        ],
    )
    .env("CHATGPT_WEB_URL", format!("http://{address}/chat.html"))
    .output()
    .unwrap();
    drop(idle);
    assert!(
        handler.join().unwrap(),
        "must actually exercise a delayed script"
    );
    assert_eq!(result.status.code(), Some(0), "{}", stderr(&result));
    assert!(stdout(&result).contains("delayed script question"));
    assert_browser_closed(dir.path());
}

#[test]
fn second_ask_waits_for_the_lock_and_reports_busy() {
    let dir = tempfile::tempdir().unwrap();
    fs::create_dir_all(dir.path()).unwrap();
    let held = fs::File::options()
        .create(true)
        .truncate(false)
        .write(true)
        .open(dir.path().join("ask.lock"))
        .unwrap();
    held.lock().unwrap();
    let o = ask(dir.path(), "chat.html", "q", &["--timeout", "2"]);
    assert_eq!(o.status.code(), Some(6), "{}", stderr(&o));
    assert!(stderr(&o).contains("ほかの質問"));
}

#[test]
fn bad_arguments_exit_2() {
    let dir = tempfile::tempdir().unwrap();
    let o = ask(dir.path(), "chat.html", "q", &["--model", "a b"]);
    assert_eq!(o.status.code(), Some(2));
    let o = ask(dir.path(), "chat.html", "  ", &[]);
    assert_eq!(o.status.code(), Some(2));
}

#[test]
fn current_ui_mock_answers_and_reports_errors() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let o = ask(
        dir.path(),
        "chat2026.html",
        "一行目\n二行目 <b>",
        &["--model", "o3", "--timeout", "120"],
    );
    let out = stdout(&o);
    assert_eq!(
        o.status.code(),
        Some(0),
        "stdout={out} stderr={}",
        stderr(&o)
    );
    let want = "### Answer\n\nModel: `o3`\n\n- Apple\n- Banana\n\n```\nfn main() {}\n```\n\n一行目\n二行目 <b>";
    assert_eq!(out.trim_end(), want);
    assert_eq!(login_state(dir.path()), "logged_in");
    assert_browser_closed(dir.path());
    for (page, code, text) in [
        ("chat2026-error.html", 1, "エラー"),
        ("chat2026-slow.html", 5, "制限時間"),
    ] {
        let dir = tempfile::tempdir().unwrap();
        let o = ask(dir.path(), page, "q", &["--timeout", "12"]);
        assert_eq!(o.status.code(), Some(code), "{page}: {}", stderr(&o));
        assert!(stderr(&o).contains(text), "{page}: {}", stderr(&o));
        assert_browser_closed(dir.path());
    }
}

/// 2026-10 に実際の chatgpt.com から記録した画面（スクリプト・見た目の属性を除いたもの）を読む
#[test]
fn recorded_pages_are_read_correctly() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let probe = |page: &str| {
        let o = run(
            dir.path(),
            page,
            &[
                "probe",
                "page:state()",
                "page:JSON.stringify(poll())",
                "page:answer()",
            ],
        );
        assert_eq!(o.status.code(), Some(0), "{page}: {}", stderr(&o));
        stdout(&o)
    };
    let out = probe("real-logged-out.html");
    assert!(out.contains("== page:state()\nlogged_out\n"), "{out}");
    assert!(out.contains(r#""users":0,"assistants":0"#), "{out}");
    let out = probe("real-streaming.html");
    assert!(
        out.contains(r#""users":1,"assistants":1,"streaming":true"#),
        "{out}"
    );
    let out = probe("real-answer.html");
    assert!(
        out.contains(r#""users":1,"assistants":1,"streaming":false"#),
        "{out}"
    );
    assert!(out.contains(r#""error":false,"limit":false"#), "{out}");
    let want = "### Plan\n\nThis is a **simple** plan using `markdown` formatting.\n\n- Apple\n- Banana\n  - Yellow\n\n1. Start\n2. Finish\n\n| Item | Value |\n| --- | --- |\n| Fruit | Apple |\n\n```\nfn main() {\n    println!(\"Hello\");\n}\n```";
    assert!(out.contains(want), "{out}");
}

#[test]
fn allow_logged_out_sends_without_recording_login_state() {
    if !chrome_available() {
        return;
    }
    let dir = tempfile::tempdir().unwrap();
    let o = ask(
        dir.path(),
        "logged-out.html",
        "q",
        &["--timeout", "120", "--allow-logged-out"],
    );
    assert_eq!(o.status.code(), Some(0), "{}", stderr(&o));
    assert!(stdout(&o).contains("### Answer"));
    assert!(!dir.path().join("state.json").exists());
    assert_browser_closed(dir.path());
}
