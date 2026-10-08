//! ChatGPT のブラウザ版（専用プロフィールの Chrome）に質問を送り、回答の本文を返す CLI。

mod cdp;

use cdp::{Browser, Tab};
use clap::{Parser, Subcommand};
use serde_json::{Value, json};
use std::fs::{self, File};
use std::io;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode, Stdio};
use std::thread::sleep;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

const PAGE_JS: &str = include_str!("page.js");
const DEFAULT_HOME: &str = "D:/CC/Apps/chatgpt-web";
const DEFAULT_URL: &str = "https://chatgpt.com/";
const DEFAULT_DISCORD: &str = "D:/CC/Tools/discord-ask/discord_ask.exe";
const DISCORD_TOPIC: &str = "後片付けの記録";

/// 終了コード。エージェントはこれで次の行動を決める。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Code {
    Ok = 0,
    Failed = 1,
    Usage = 2,
    LoginRequired = 3,
    Blocked = 4,
    Timeout = 5,
    Busy = 6,
    Limited = 7,
}

#[derive(Debug)]
struct Fail {
    code: Code,
    msg: String,
}

fn fail(code: Code, msg: impl Into<String>) -> Fail {
    Fail {
        code,
        msg: msg.into(),
    }
}

impl From<io::Error> for Fail {
    fn from(e: io::Error) -> Self {
        fail(Code::Failed, e.to_string())
    }
}

type Res<T> = Result<T, Fail>;

#[derive(Parser)]
#[command(
    name = "chatgpt_web",
    about = "ChatGPT のブラウザ版に質問を送り、回答の本文を標準出力に返す",
    after_help = "終了コード: 0 回答あり / 1 失敗 / 2 引数の誤り / 3 ログインが必要 / 4 アカウント停止 / 5 時間切れ / 6 ほかの質問の実行中 / 7 利用上限"
)]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// 新しいチャットに質問を送り、回答が終わるまで待って本文を返す
    Ask {
        /// 質問の本文（UTF-8）
        #[arg(long)]
        prompt_file: PathBuf,
        /// 使うモデル（ChatGPT の URL の model= に渡す名前。例: gpt-5-thinking）
        #[arg(long)]
        model: Option<String>,
        /// 全体の制限時間（秒）。ほかの質問の終了待ちも含む
        #[arg(long, default_value_t = 900)]
        timeout: u64,
        /// 保守用：未ログインの画面でも送る（画面の作りの確認だけに使う。ログイン状態の記録はしない）
        #[arg(long, hide = true)]
        allow_logged_out: bool,
    },
    /// ログインできているかを画面で確かめて記録する（毎日の定期確認用）
    Check,
    /// 最後に確かめたログイン状態を表示する（ブラウザは起動しない）
    Status,
    /// 専用プロフィールの Chrome を見える窓でログイン画面に開く
    Login,
    /// 専用プロフィールの Chrome を閉じる
    Stop,
    /// 画面の作りを調べる保守用：URL を開き、JS のファイルか「page:式」を順に評価して結果を表示する（「wait:秒」で待つ）
    #[command(hide = true)]
    Probe {
        #[arg(long)]
        url: Option<String>,
        steps: Vec<String>,
    },
}

struct Env {
    home: PathBuf,
    url: String,
    discord: Option<PathBuf>,
}

impl Env {
    fn load() -> Self {
        let var = |k: &str| std::env::var(k).ok().filter(|v| !v.is_empty());
        let discord = match var("CHATGPT_WEB_DISCORD").as_deref() {
            Some("off") => None,
            Some(p) => Some(PathBuf::from(p)),
            None => Some(PathBuf::from(DEFAULT_DISCORD)),
        };
        Self {
            home: PathBuf::from(var("CHATGPT_WEB_HOME").unwrap_or_else(|| DEFAULT_HOME.into())),
            url: var("CHATGPT_WEB_URL").unwrap_or_else(|| DEFAULT_URL.into()),
            discord,
        }
    }
    fn profile(&self) -> PathBuf {
        self.home.join("profile")
    }
}

fn main() -> ExitCode {
    let cli = Cli::parse();
    let env = Env::load();
    let result = match cli.cmd {
        Cmd::Ask {
            prompt_file,
            model,
            timeout,
            allow_logged_out,
        } => ask(
            &env,
            &prompt_file,
            model.as_deref(),
            timeout,
            allow_logged_out,
        ),
        Cmd::Check => check(&env),
        Cmd::Status => status(&env),
        Cmd::Login => login(&env),
        Cmd::Stop => stop(&env),
        Cmd::Probe { url, steps } => probe(&env, url.as_deref(), &steps),
    };
    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(f) => {
            eprintln!("chatgpt_web: {}", f.msg);
            ExitCode::from(f.code as u8)
        }
    }
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

// ---------- 入力 ----------

fn read_prompt(path: &Path) -> Res<String> {
    let raw = fs::read(path)
        .map_err(|e| fail(Code::Usage, format!("質問のファイルを読めません: {e}")))?;
    let text = String::from_utf8(raw)
        .map_err(|_| fail(Code::Usage, "質問のファイルが UTF-8 ではありません"))?;
    let text = text
        .strip_prefix('\u{feff}')
        .unwrap_or(&text)
        .replace("\r\n", "\n");
    if text.trim().is_empty() {
        return Err(fail(Code::Usage, "質問のファイルが空です"));
    }
    Ok(text)
}

fn chat_url(base: &str, model: Option<&str>) -> Res<String> {
    let Some(m) = model else {
        return Ok(base.to_string());
    };
    if m.is_empty()
        || !m
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || "-._".contains(c))
    {
        return Err(fail(
            Code::Usage,
            format!("モデル名に使えない文字があります: {m}"),
        ));
    }
    let sep = if base.contains('?') { '&' } else { '?' };
    Ok(format!("{base}{sep}model={m}"))
}

// ---------- 同時実行の制限 ----------

/// プロセス終了時に OS が解放するファイルロック。期限まで空きを待つ。
fn lock(home: &Path, deadline: Instant) -> Res<File> {
    fs::create_dir_all(home)?;
    let f = File::options()
        .create(true)
        .truncate(false)
        .write(true)
        .open(home.join("ask.lock"))?;
    loop {
        match f.try_lock() {
            Ok(()) => return Ok(f),
            Err(fs::TryLockError::WouldBlock) if Instant::now() < deadline => {
                sleep(Duration::from_millis(500))
            }
            Err(fs::TryLockError::WouldBlock) => {
                return Err(fail(
                    Code::Busy,
                    "ほかの質問が実行中のため、時間内に始められませんでした",
                ));
            }
            Err(fs::TryLockError::Error(e)) => return Err(e.into()),
        }
    }
}

// ---------- ログイン状態の記録 ----------

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Login {
    LoggedIn,
    LoggedOut,
    Blocked,
}

impl Login {
    fn key(self) -> &'static str {
        match self {
            Login::LoggedIn => "logged_in",
            Login::LoggedOut => "logged_out",
            Login::Blocked => "blocked",
        }
    }
    fn from_key(k: &str) -> Option<Self> {
        [Login::LoggedIn, Login::LoggedOut, Login::Blocked]
            .into_iter()
            .find(|l| l.key() == k)
    }
    fn fail(self) -> Fail {
        match self {
            Login::LoggedIn => fail(Code::Ok, ""),
            Login::LoggedOut => fail(
                Code::LoginRequired,
                "ChatGPT にログインしていません（ログインが切れました）。復旧後に `chatgpt_web login` でログインし直してください",
            ),
            Login::Blocked => fail(
                Code::Blocked,
                "ChatGPT のアカウントが止められている表示が出ました。対処はオーナーが決めます",
            ),
        }
    }
}

/// 状態が変わったときに Discord へ残す文。同じ状態が続くときは何も送らない。
fn transition_message(prev: Option<Login>, new: Login) -> Option<&'static str> {
    if prev == Some(new) {
        return None;
    }
    Some(match new {
        Login::LoggedIn => {
            "ChatGPT ブラウザ版（chatgpt_web）のログインを確認しました。今日から ask で質問を回せます。"
        }
        Login::LoggedOut => {
            "ChatGPT ブラウザ版（chatgpt_web）はログインしていない状態です。ask は終了コード3で止まります。アカウントが復旧したら chatgpt_web login でログインし直します。"
        }
        Login::Blocked => {
            "ChatGPT ブラウザ版（chatgpt_web）でアカウントが止められている表示が出ました。ask は終了コード4で止まります。対処はオーナーが決めます。"
        }
    })
}

fn read_state(home: &Path) -> Option<Value> {
    serde_json::from_str(&fs::read_to_string(home.join("state.json")).ok()?).ok()
}

fn record_login(env: &Env, new: Login) {
    let prev = read_state(&env.home).and_then(|v| v["login"].as_str().and_then(Login::from_key));
    let changed = prev != Some(new);
    let mut st = read_state(&env.home).unwrap_or_else(|| json!({}));
    st["login"] = json!(new.key());
    st["checked_at"] = json!(now());
    if changed {
        st["since"] = json!(now());
    }
    let tmp = env.home.join("state.json.tmp");
    if fs::create_dir_all(&env.home).is_ok()
        && fs::write(&tmp, serde_json::to_vec_pretty(&st).unwrap_or_default()).is_ok()
    {
        let _ = fs::rename(&tmp, env.home.join("state.json"));
    }
    if let Some(msg) = transition_message(prev, new) {
        discord_log(env, msg);
    }
}

/// 「後片付けの記録」スレッドへ通知なしで残す。失敗しても本処理は止めない。
fn discord_log(env: &Env, text: &str) {
    let Some(exe) = &env.discord else { return };
    let mut cmd = Command::new(exe);
    cmd.args(["log", "--topic", DISCORD_TOPIC, "--text", text])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x0800_0000); // CREATE_NO_WINDOW
    }
    let Ok(mut child) = cmd.spawn() else {
        eprintln!("chatgpt_web: Discord へ記録できませんでした");
        return;
    };
    let deadline = Instant::now() + Duration::from_secs(60);
    while Instant::now() < deadline {
        if let Ok(Some(_)) = child.try_wait() {
            return;
        }
        sleep(Duration::from_millis(200));
    }
    let _ = child.kill();
    let _ = child.wait();
    eprintln!("chatgpt_web: Discord への記録が時間内に終わりませんでした");
}

// ---------- 画面の操作 ----------

fn js(tab: &mut Tab, call: &str) -> Res<Value> {
    Ok(tab
        .cdp
        .eval(&format!("(() => {{\n{PAGE_JS}\nreturn {call};\n}})()"))?)
}

/// 画面がログイン済み・未ログイン・停止のどれかに落ち着くまで待つ。
fn wait_login(tab: &mut Tab, deadline: Instant) -> Res<Login> {
    let page_deadline = deadline.min(Instant::now() + Duration::from_secs(90));
    let mut last = String::new();
    loop {
        // 読み込み中の評価失敗（遷移中など）は待って再試行する
        if let Ok(v) = js(tab, "state()") {
            last = v.as_str().unwrap_or_default().to_string();
            match last.as_str() {
                "logged_in" => return Ok(Login::LoggedIn),
                "logged_out" => return Ok(Login::LoggedOut),
                "blocked" => return Ok(Login::Blocked),
                _ => {}
            }
        }
        if Instant::now() >= page_deadline {
            let code = if page_deadline == deadline {
                Code::Timeout
            } else {
                Code::Failed
            };
            return Err(fail(
                code,
                match last.as_str() {
                    "challenge" => "ChatGPT の画面で人間確認（Cloudflare）が出たまま進みません。`chatgpt_web login` の窓で確認を済ませてください".into(),
                    s if page_deadline == deadline => format!("ChatGPT の画面を制限時間内に確認できませんでした（状態: {s}）"),
                    s => format!("ChatGPT の画面を判別できません（状態: {s}）。画面の作りが変わった可能性があります"),
                },
            ));
        }
        sleep(Duration::from_secs(1));
    }
}

fn open_chat(
    env: &Env,
    browser: &mut Browser,
    url: &str,
    deadline: Instant,
    anon: bool,
) -> Res<Tab> {
    let mut tab = browser.new_tab(url, false)?;
    let login = wait_login(&mut tab, deadline);
    if let Ok(l) = login
        && !anon
    {
        record_login(env, l);
    }
    match login {
        Ok(Login::LoggedIn) => Ok(tab),
        Ok(Login::LoggedOut) if anon => Ok(tab),
        Ok(l) => {
            browser.close_tab(tab);
            Err(l.fail())
        }
        Err(e) => {
            browser.close_tab(tab);
            Err(e)
        }
    }
}

fn send_prompt(tab: &mut Tab, prompt: &str) -> Res<()> {
    let lit = serde_json::to_string(prompt).map_err(io::Error::other)?;
    let pasted = js(tab, &format!("paste({lit})"))?.as_i64().unwrap_or(-1);
    if pasted <= 0 {
        // 貼り付けを受け付けない画面では、文字入力として入れる
        js(tab, "focusComposer()")?;
        tab.cdp.call("Input.insertText", json!({"text": prompt}))?;
        if js(tab, "composerLength()")?.as_i64().unwrap_or(-1) <= 0 {
            return Err(fail(Code::Failed, "入力欄に質問を入れられませんでした"));
        }
    }
    let until = Instant::now() + Duration::from_secs(30);
    while !js(tab, "clickSend()")?.as_bool().unwrap_or(false) {
        if Instant::now() >= until {
            return Err(fail(
                Code::Failed,
                "送信ボタンが押せる状態になりませんでした",
            ));
        }
        sleep(Duration::from_millis(300));
    }
    let until = Instant::now() + Duration::from_secs(30);
    loop {
        if js(tab, "poll()")?["users"].as_u64().unwrap_or(0) > 0 {
            return Ok(());
        }
        if Instant::now() >= until {
            return Err(fail(
                Code::Failed,
                "質問を送信できませんでした（画面に依頼文が表示されません）",
            ));
        }
        sleep(Duration::from_millis(300));
    }
}

/// 回答が終わるまで待つ。生成中の表示が消え、本文の長さが2回続けて変わらなければ完了とする。
fn wait_answer(env: &Env, tab: &mut Tab, deadline: Instant, anon: bool) -> Res<String> {
    let mut stable = 0;
    let mut last_len = u64::MAX;
    loop {
        let p = js(tab, "poll()")?;
        if p["limit"].as_bool() == Some(true) {
            return Err(fail(
                Code::Limited,
                "ChatGPT の利用上限に達しました。時間をおいて再実行してください",
            ));
        }
        if let Some(l) = p["state"].as_str().and_then(Login::from_key)
            && l != Login::LoggedIn
            && !(anon && l == Login::LoggedOut)
        {
            record_login(env, l);
            return Err(l.fail());
        }
        let streaming = p["streaming"].as_bool().unwrap_or(true);
        let len = p["len"].as_u64().unwrap_or(0);
        if !streaming && p["error"].as_bool() == Some(true) {
            return Err(fail(
                Code::Failed,
                "ChatGPT が回答の途中でエラーを表示しました",
            ));
        }
        if !streaming && len > 0 && p["assistants"].as_u64().unwrap_or(0) > 0 {
            stable = if len == last_len { stable + 1 } else { 0 };
            if stable >= 2 {
                let text = js(tab, "answer()")?
                    .as_str()
                    .unwrap_or_default()
                    .to_string();
                if !text.is_empty() {
                    return Ok(text);
                }
            }
        } else {
            stable = 0;
        }
        last_len = len;
        if Instant::now() >= deadline {
            return Err(fail(Code::Timeout, "制限時間内に回答が終わりませんでした"));
        }
        sleep(Duration::from_secs(1));
    }
}

// ---------- サブコマンド ----------

fn ask(env: &Env, prompt_file: &Path, model: Option<&str>, timeout: u64, anon: bool) -> Res<()> {
    let deadline = Instant::now() + Duration::from_secs(timeout);
    let prompt = read_prompt(prompt_file)?;
    let url = chat_url(&env.url, model)?;
    let _lock = lock(&env.home, deadline)?;
    let mut browser = Browser::open(&env.profile(), false)?;
    let mut tab = open_chat(env, &mut browser, &url, deadline, anon)?;
    let result =
        send_prompt(&mut tab, &prompt).and_then(|()| wait_answer(env, &mut tab, deadline, anon));
    browser.close_tab(tab);
    let text = result?;
    println!("{text}");
    Ok(())
}

fn check(env: &Env) -> Res<()> {
    let deadline = Instant::now() + Duration::from_secs(600);
    let _lock = lock(&env.home, deadline)?;
    let mut browser = Browser::open(&env.profile(), false)?;
    let tab = open_chat(env, &mut browser, &env.url, deadline, false)?;
    browser.close_tab(tab);
    println!("ChatGPT にログインできています");
    Ok(())
}

fn status(env: &Env) -> Res<()> {
    let st = read_state(&env.home).unwrap_or_else(|| json!({}));
    let Some(login) = st["login"].as_str().and_then(Login::from_key) else {
        return Err(fail(
            Code::LoginRequired,
            "まだ確認していません。`chatgpt_web check` を実行してください",
        ));
    };
    if login != Login::LoggedIn {
        return Err(login.fail());
    }
    let ago = now().saturating_sub(st["checked_at"].as_u64().unwrap_or(0)) / 3600;
    println!("ログインできています（{ago} 時間前に確認）");
    Ok(())
}

fn login(env: &Env) -> Res<()> {
    let mut browser = Browser::open(&env.profile(), true)?;
    let login_url = format!(
        "{}/auth/login",
        env.url
            .split('?')
            .next()
            .unwrap_or(DEFAULT_URL)
            .trim_end_matches('/')
    );
    let launched = browser.launched();
    browser.new_tab(&login_url, !launched)?;
    // 自分で起動した Chrome もログイン作業のため開いたままにする（閉じるのは `chatgpt_web stop`）
    std::mem::forget(browser);
    println!(
        "専用プロフィールの Chrome でログイン画面を開きました。ログイン後に `chatgpt_web check` を実行してください"
    );
    Ok(())
}

fn stop(env: &Env) -> Res<()> {
    let _lock = lock(&env.home, Instant::now() + Duration::from_secs(30))?;
    let port_file = env.profile().join("DevToolsActivePort");
    let Some((port, path)) = fs::read_to_string(&port_file)
        .ok()
        .as_deref()
        .and_then(cdp::parse_active_port)
    else {
        println!("起動していません");
        return Ok(());
    };
    match cdp::Cdp::connect(port, &path) {
        Ok(mut c) => {
            let _ = c.call("Browser.close", json!({}));
            println!("専用プロフィールの Chrome を閉じました");
        }
        Err(_) => println!("起動していません"),
    }
    Ok(())
}

fn probe(env: &Env, url: Option<&str>, steps: &[String]) -> Res<()> {
    let _lock = lock(&env.home, Instant::now() + Duration::from_secs(30))?;
    let mut browser = Browser::open(&env.profile(), false)?;
    let mut tab = browser.new_tab(url.unwrap_or(&env.url), false)?;
    for step in steps {
        if let Some(secs) = step.strip_prefix("wait:") {
            sleep(Duration::from_secs(
                secs.parse()
                    .map_err(|_| fail(Code::Usage, "wait: の秒数が不正です"))?,
            ));
            continue;
        }
        // 「page:式」は page.js の関数を使って評価する（例: page:state()）
        let v = match step.strip_prefix("page:") {
            Some(call) => js(&mut tab, call)?,
            None => tab.cdp.eval(&fs::read_to_string(step)?)?,
        };
        println!(
            "== {step}
{}",
            v.as_str()
                .map(str::to_string)
                .unwrap_or_else(|| v.to_string())
        );
    }
    browser.close_tab(tab);
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn model_is_passed_as_url_parameter_and_validated() {
        assert_eq!(
            chat_url("https://chatgpt.com/", None).unwrap(),
            "https://chatgpt.com/"
        );
        assert_eq!(
            chat_url("https://chatgpt.com/", Some("gpt-5-thinking")).unwrap(),
            "https://chatgpt.com/?model=gpt-5-thinking"
        );
        assert_eq!(
            chat_url("file:///x.html?a=1", Some("o3")).unwrap(),
            "file:///x.html?a=1&model=o3"
        );
        for bad in ["", "a b", "x&y=1", "日本"] {
            assert_eq!(
                chat_url("https://chatgpt.com/", Some(bad))
                    .unwrap_err()
                    .code,
                Code::Usage,
                "{bad}"
            );
        }
    }

    #[test]
    fn prompt_file_strips_bom_and_rejects_empty() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("q.md");
        fs::write(&p, "\u{feff}line1\r\nline2").unwrap();
        assert_eq!(read_prompt(&p).unwrap(), "line1\nline2");
        fs::write(&p, " \n").unwrap();
        assert_eq!(read_prompt(&p).unwrap_err().code, Code::Usage);
        fs::write(&p, [0xff, 0xfe, 0x00]).unwrap();
        assert_eq!(read_prompt(&p).unwrap_err().code, Code::Usage);
        assert_eq!(
            read_prompt(&dir.path().join("none")).unwrap_err().code,
            Code::Usage
        );
    }

    #[test]
    fn discord_is_written_only_when_login_state_changes() {
        assert!(transition_message(None, Login::LoggedOut).is_some());
        assert!(transition_message(Some(Login::LoggedOut), Login::LoggedOut).is_none());
        assert!(
            transition_message(Some(Login::LoggedOut), Login::LoggedIn)
                .unwrap()
                .contains("ログインを確認")
        );
        assert!(
            transition_message(Some(Login::LoggedIn), Login::Blocked)
                .unwrap()
                .contains("終了コード4")
        );
        assert!(transition_message(Some(Login::LoggedIn), Login::LoggedIn).is_none());
    }

    #[test]
    fn state_file_records_since_only_on_change() {
        let dir = tempfile::tempdir().unwrap();
        let env = Env {
            home: dir.path().into(),
            url: DEFAULT_URL.into(),
            discord: None,
        };
        record_login(&env, Login::LoggedOut);
        let first = read_state(&env.home).unwrap();
        assert_eq!(first["login"], "logged_out");
        record_login(&env, Login::LoggedOut);
        assert_eq!(read_state(&env.home).unwrap()["since"], first["since"]);
        record_login(&env, Login::LoggedIn);
        assert_eq!(read_state(&env.home).unwrap()["login"], "logged_in");
    }

    #[test]
    fn lock_reports_busy_after_deadline() {
        let dir = tempfile::tempdir().unwrap();
        let held = lock(dir.path(), Instant::now()).unwrap();
        let started = Instant::now();
        let err = lock(dir.path(), Instant::now() + Duration::from_millis(800)).unwrap_err();
        assert_eq!(err.code, Code::Busy);
        assert!(started.elapsed() >= Duration::from_millis(800));
        drop(held);
        lock(dir.path(), Instant::now()).unwrap();
    }
}
