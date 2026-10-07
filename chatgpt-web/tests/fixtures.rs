//! ChatGPT の画面を模した tests/fixtures/ の HTML を、実際の Chrome で操作して確かめる。
//! Chrome が無い環境では何もせずに成功する。

use std::fs;
use std::net::{SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Command, Output};
use std::time::Duration;

fn chrome_available() -> bool {
    std::env::var_os("CHATGPT_WEB_CHROME").is_some()
        || ["ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"]
            .iter()
            .filter_map(std::env::var_os)
            .any(|b| {
                PathBuf::from(b)
                    .join(r"Google\Chrome\Application\chrome.exe")
                    .is_file()
            })
}

fn fixture_url(name: &str) -> String {
    let p = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(name);
    format!("file:///{}", p.to_string_lossy().replace('\\', "/"))
}

fn run(home: &Path, page: &str, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_chatgpt_web"))
        .args(args)
        .env("CHATGPT_WEB_HOME", home)
        .env("CHATGPT_WEB_URL", fixture_url(page))
        .env("CHATGPT_WEB_DISCORD", "off")
        .output()
        .unwrap()
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
                "wait:2",
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
        &["--timeout", "60", "--allow-logged-out"],
    );
    assert_eq!(o.status.code(), Some(0), "{}", stderr(&o));
    assert!(stdout(&o).contains("### Answer"));
    assert!(!dir.path().join("state.json").exists());
    assert_browser_closed(dir.path());
}
