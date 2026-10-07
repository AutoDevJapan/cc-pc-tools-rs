pub mod task;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    fs::{self, File, OpenOptions},
    io::{Read, Write},
    net::{TcpListener, TcpStream},
    path::{Path, PathBuf},
    time::{Duration, Instant},
};
use tempfile::NamedTempFile;

pub type Result<T> = std::result::Result<T, Box<dyn std::error::Error + Send + Sync>>;
pub const ROOT: &str = "D:/CC/Tools/chrome-bookmark-queue";
pub const PORT: u16 = 17643;
pub const FOLDERS: [&str; 22] = [
    "案件・業務管理",
    "LLMモデル",
    "ローカルLLM・推論",
    "AIゲートウェイ・基盤",
    "コーディングエージェント",
    "Claude Code・Codex 拡張",
    "スキル・仕様駆動",
    "MCP・記憶・ブラウザ操作",
    "エージェント・Bot",
    "論文・学習資料",
    "ノーコード・サイト構築",
    "デザイン・UI",
    "開発ライブラリ・インフラ",
    "3Dモデル・ゲーム",
    "画像生成・OCR",
    "音声・音楽",
    "動画生成サービス",
    "動画生成OSS・編集",
    "マーケティング・集客",
    "文章・スライド生成",
    "通貨取引関連",
    "調査・お得情報",
];

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Entry {
    pub id: String,
    pub url: String,
    pub name: String,
    pub folder: String,
}
#[derive(Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Receipt {
    pub id: String,
    pub status: String,
    pub bookmark_id: String,
}
#[derive(Serialize, Deserialize)]
pub struct Completed {
    pub entry: Entry,
    pub result: Receipt,
}
#[derive(Serialize, Deserialize)]
struct Config {
    token: String,
}

pub fn canonical_url(input: &str) -> Result<String> {
    let u = url::Url::parse(input)?;
    let parts: Vec<_> = u.path().trim_matches('/').split('/').collect();
    if u.scheme() != "https"
        || u.host_str() != Some("github.com")
        || !u.username().is_empty()
        || u.password().is_some()
        || u.port().is_some()
        || parts.len() != 2
        || parts.iter().any(|p| {
            p.is_empty()
                || !p
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b"-_.".contains(&b))
        })
    {
        return Err("HTTPSのGitHubリポジトリURLを指定してください".into());
    }
    let repo = parts[1].strip_suffix(".git").unwrap_or(parts[1]);
    if repo.is_empty() || parts[0] == "." || parts[0] == ".." || repo == "." || repo == ".." {
        return Err("リポジトリ名が不正です".into());
    }
    Ok(format!(
        "https://github.com/{}/{}",
        parts[0].to_ascii_lowercase(),
        repo.to_ascii_lowercase()
    ))
}
fn valid_id(id: &str) -> bool {
    id.len() == 64
        && id
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
impl Entry {
    pub fn new(url: &str, name: &str, folder: &str) -> Result<Self> {
        let url = canonical_url(url)?;
        if name.trim() != name
            || name.contains('\u{feff}')
            || name.len() > 512
            || name.chars().any(char::is_control)
            || !name
                .split_once(" – ")
                .is_some_and(|(a, b)| !a.trim().is_empty() && !b.trim().is_empty())
            || !FOLDERS.contains(&folder)
        {
            return Err("表示名か用途フォルダが不正です".into());
        }
        let id = format!("{:x}", Sha256::digest(url.as_bytes()));
        Ok(Self {
            id,
            url,
            name: name.into(),
            folder: folder.into(),
        })
    }
    pub fn validate(&self) -> Result<()> {
        let e = Self::new(&self.url, &self.name, &self.folder)?;
        if *self != e {
            return Err("キューのURLかIDが不正です".into());
        }
        Ok(())
    }
}
fn atomic_json(path: &Path, value: &impl Serialize) -> Result<()> {
    let mut tmp = NamedTempFile::new_in(path.parent().ok_or("親フォルダなし")?)?;
    serde_json::to_writer_pretty(&mut tmp, value)?;
    tmp.as_file_mut().sync_all()?;
    tmp.persist(path)?;
    Ok(())
}
pub struct Queue {
    root: PathBuf,
}
impl Queue {
    pub fn new(root: impl Into<PathBuf>) -> Self {
        Self { root: root.into() }
    }
    fn lock(&self) -> Result<File> {
        fs::create_dir_all(&self.root)?;
        let f = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(self.root.join("queue.lock"))?;
        f.lock()?;
        for dir in ["pending", "done"] {
            fs::create_dir_all(self.root.join(dir))?;
        }
        Ok(f)
    }
    pub fn init(&self) -> Result<()> {
        let _guard = self.lock()?;
        let cfg = self.root.join("config.json");
        if !cfg.exists() {
            atomic_json(
                &cfg,
                &Config {
                    token: format!(
                        "{}{}",
                        uuid::Uuid::new_v4().simple(),
                        uuid::Uuid::new_v4().simple()
                    ),
                },
            )?;
        }
        let token = self.token()?;
        let ext = self.root.join("extension");
        fs::create_dir_all(&ext)?;
        for (name, content) in [
            ("manifest.json", include_str!("../extension/manifest.json")),
            ("core.js", include_str!("../extension/core.js")),
            ("background.js", include_str!("../extension/background.js")),
        ] {
            fs::write(ext.join(name), content)?;
        }
        fs::write(
            ext.join("config.js"),
            format!("export const TOKEN = {};\n", serde_json::to_string(&token)?),
        )?;
        Ok(())
    }
    pub fn token(&self) -> Result<String> {
        let c: Config = serde_json::from_reader(File::open(self.root.join("config.json"))?)?;
        if !valid_id(&c.token) {
            return Err("認証設定が不正です".into());
        }
        Ok(c.token)
    }
    pub fn add(&self, entry: &Entry) -> Result<&'static str> {
        entry.validate()?;
        let _guard = self.lock()?;
        for dir in ["pending", "done"] {
            if self.path(dir, &entry.id).exists() {
                return Ok(dir);
            }
        }
        atomic_json(&self.path("pending", &entry.id), entry)?;
        Ok("added")
    }
    fn path(&self, dir: &str, id: &str) -> PathBuf {
        self.root.join(dir).join(format!("{id}.json"))
    }
    pub fn pending(&self) -> Result<Vec<Entry>> {
        let _guard = self.lock()?;
        let mut paths: Vec<_> =
            fs::read_dir(self.root.join("pending"))?.collect::<std::io::Result<Vec<_>>>()?;
        paths.sort_by_key(|p| p.file_name());
        let mut out = Vec::new();
        for p in paths {
            if p.path().extension().is_none_or(|e| e != "json") {
                continue;
            }
            let read = || -> Result<Entry> {
                let e: Entry = serde_json::from_reader(File::open(p.path())?)?;
                e.validate()?;
                if p.file_name().to_str() != Some(&format!("{}.json", e.id)) {
                    return Err("ファイル名とIDが違います".into());
                }
                Ok(e)
            };
            match read() {
                Ok(e) => out.push(e),
                Err(e) => eprintln!("invalid queue {}: {e}", p.file_name().to_string_lossy()),
            }
            if out.len() == 32 {
                break;
            }
        }
        Ok(out)
    }
    pub fn complete(&self, receipt: Receipt) -> Result<()> {
        if !valid_id(&receipt.id)
            || !["created", "exists"].contains(&receipt.status.as_str())
            || receipt.bookmark_id.is_empty()
            || receipt.bookmark_id.len() > 128
            || receipt.bookmark_id.chars().any(char::is_control)
        {
            return Err("登録結果が不正です".into());
        }
        let _guard = self.lock()?;
        let done = self.path("done", &receipt.id);
        let pending = self.path("pending", &receipt.id);
        if !pending.exists() {
            if done.exists() {
                return Ok(());
            }
            return Err("未登録のキューIDです".into());
        }
        let entry: Entry = serde_json::from_reader(File::open(&pending)?)?;
        entry.validate()?;
        atomic_json(
            &done,
            &Completed {
                entry,
                result: receipt,
            },
        )?;
        fs::remove_file(pending)?;
        Ok(())
    }
}

struct Request {
    method: String,
    path: String,
    headers: Vec<(String, String)>,
    body: Vec<u8>,
}
fn read_request(stream: &mut TcpStream) -> Result<Request> {
    stream.set_read_timeout(Some(Duration::from_secs(2)))?;
    stream.set_write_timeout(Some(Duration::from_secs(2)))?;
    let deadline = Instant::now() + Duration::from_secs(2);
    let mut buf = Vec::new();
    while !buf.ends_with(b"\r\n\r\n") {
        if buf.len() >= 8192 || Instant::now() >= deadline {
            return Err("request limit".into());
        }
        stream.set_read_timeout(Some(
            deadline
                .saturating_duration_since(Instant::now())
                .max(Duration::from_millis(1)),
        ))?;
        let mut b = [0];
        stream.read_exact(&mut b)?;
        buf.push(b[0]);
    }
    let text = std::str::from_utf8(&buf)?;
    let mut lines = text.split("\r\n");
    let line: Vec<_> = lines
        .next()
        .ok_or("request line")?
        .split_whitespace()
        .collect();
    if line.len() != 3 || line[2] != "HTTP/1.1" {
        return Err("request line".into());
    }
    let mut headers = Vec::new();
    for line in lines.filter(|s| !s.is_empty()) {
        let (k, v) = line.split_once(':').ok_or("header")?;
        let k = k.to_ascii_lowercase();
        if headers.iter().any(|(old, _)| old == &k) {
            return Err("duplicate header".into());
        }
        headers.push((k, v.trim().to_string()));
    }
    if headers.iter().any(|(k, _)| k == "transfer-encoding") {
        return Err("transfer encoding".into());
    }
    let len: usize = headers
        .iter()
        .find(|(k, _)| k == "content-length")
        .map(|(_, v)| v.parse())
        .transpose()?
        .unwrap_or(0);
    if len > 4096 {
        return Err("body limit".into());
    }
    let mut body = vec![0; len];
    let mut offset = 0;
    while offset < len {
        if Instant::now() >= deadline {
            return Err("body timeout".into());
        }
        stream.set_read_timeout(Some(
            deadline
                .saturating_duration_since(Instant::now())
                .max(Duration::from_millis(1)),
        ))?;
        let count = stream.read(&mut body[offset..])?;
        if count == 0 {
            return Err("incomplete body".into());
        }
        offset += count;
    }
    Ok(Request {
        method: line[0].into(),
        path: line[1].into(),
        headers,
        body,
    })
}
fn dispatch(q: &Queue, token: &str, r: Request) -> (u16, serde_json::Value) {
    let header = |name: &str| {
        r.headers
            .iter()
            .find(|(k, _)| k == name)
            .map(|(_, v)| v.as_str())
    };
    if header("host") != Some("127.0.0.1:17643")
        || header("authorization") != Some(format!("Bearer {token}").as_str())
    {
        return (401, serde_json::json!({"error":"unauthorized"}));
    }
    if header("origin").is_some_and(|o| {
        !o.strip_prefix("chrome-extension://")
            .is_some_and(|id| id.len() == 32 && id.bytes().all(|b| (b'a'..=b'p').contains(&b)))
    }) {
        return (403, serde_json::json!({"error":"origin"}));
    }
    match (r.method.as_str(), r.path.as_str()) {
        ("GET", "/pending") => match q.pending() {
            Ok(e) => (200, serde_json::json!({"entries":e})),
            Err(_) => (500, serde_json::json!({"error":"queue read"})),
        },
        ("POST", "/result") if header("content-type") == Some("application/json") => {
            match serde_json::from_slice::<Receipt>(&r.body)
                .map_err(|e| -> Box<dyn std::error::Error + Send + Sync> { Box::new(e) })
                .and_then(|e| q.complete(e))
            {
                Ok(()) => (200, serde_json::json!({"ok":true})),
                Err(_) => (400, serde_json::json!({"error":"invalid result"})),
            }
        }
        _ => (404, serde_json::json!({"error":"not found"})),
    }
}
pub fn serve(q: Queue) -> Result<()> {
    let token = q.token()?;
    let listener = TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, PORT))?;
    for connection in listener.incoming() {
        let mut stream = match connection {
            Ok(stream) => stream,
            Err(e) => {
                eprintln!("accept: {e}");
                continue;
            }
        };
        let (code, value) = match read_request(&mut stream) {
            Ok(r) => dispatch(&q, &token, r),
            Err(_) => (400, serde_json::json!({"error":"bad request"})),
        };
        let body = value.to_string();
        let _ = write!(
            stream,
            "HTTP/1.1 {code} Result\r\nContent-Type: application/json\r\nContent-Length: {}\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n{body}",
            body.len()
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    fn queue() -> (tempfile::TempDir, Queue) {
        let root = if let Ok(parent) = std::env::var("CC_BOOKMARK_TEST_ROOT") {
            tempfile::tempdir_in(parent).unwrap()
        } else {
            tempfile::tempdir().unwrap()
        };
        let q = Queue::new(root.path());
        q.init().unwrap();
        (root, q)
    }
    fn entry() -> Entry {
        Entry::new(
            "https://github.com/Example/Tool.git/",
            "Tool – 開発を補助する",
            "開発ライブラリ・インフラ",
        )
        .unwrap()
    }
    fn request(method: &str, path: &str, token: &str, body: Vec<u8>) -> Request {
        Request {
            method: method.into(),
            path: path.into(),
            headers: vec![
                ("host".into(), "127.0.0.1:17643".into()),
                ("authorization".into(), format!("Bearer {token}")),
                ("content-type".into(), "application/json".into()),
            ],
            body,
        }
    }
    #[test]
    fn init_preserves_token_and_generates_minimal_extension() {
        let (_root, q) = queue();
        let token = q.token().unwrap();
        q.init().unwrap();
        assert_eq!(q.token().unwrap(), token);
        let m: serde_json::Value =
            serde_json::from_reader(File::open(q.root.join("extension/manifest.json")).unwrap())
                .unwrap();
        assert_eq!(m["permissions"], serde_json::json!(["bookmarks"]));
        assert_eq!(
            m["host_permissions"],
            serde_json::json!(["http://127.0.0.1:17643/*"])
        );
        assert!(
            fs::read_to_string(q.root.join("extension/config.js"))
                .unwrap()
                .contains(&token)
        );
    }
    #[test]
    fn canonical_repositories_only() {
        assert_eq!(
            canonical_url("https://github.com/Example/Tool.git/?tab=readme#usage").unwrap(),
            "https://github.com/example/tool"
        );
        for url in [
            "http://github.com/a/b",
            "https://github.com/a/b/tree/main",
            "https://evil.example/a/b",
            "https://user@github.com/a/b",
            "https://github.com:17643/a/b",
            "https://github.com/a/.git",
        ] {
            assert!(canonical_url(url).is_err(), "{url}");
        }
    }
    #[test]
    fn validates_names_categories_and_ids() {
        assert!(Entry::new("https://github.com/a/b", "Tool", "デザイン・UI").is_err());
        assert!(Entry::new("https://github.com/a/b", "Tool – ok\n", "デザイン・UI").is_err());
        assert!(Entry::new("https://github.com/a/b", "Tool – ok", "../escape").is_err());
        let mut e = entry();
        e.id = "a".repeat(64);
        assert!(e.validate().is_err());
        assert_eq!(FOLDERS.len(), 22);
    }
    #[test]
    fn add_deduplicates_and_receipt_is_replayable() {
        let (_root, q) = queue();
        let e = entry();
        assert_eq!(q.add(&e).unwrap(), "added");
        assert_eq!(q.add(&e).unwrap(), "pending");
        q.complete(Receipt {
            id: e.id.clone(),
            status: "created".into(),
            bookmark_id: "42".into(),
        })
        .unwrap();
        assert!(q.pending().unwrap().is_empty());
        assert_eq!(q.add(&e).unwrap(), "done");
        q.complete(Receipt {
            id: e.id.clone(),
            status: "exists".into(),
            bookmark_id: "42".into(),
        })
        .unwrap();
        let c: Completed =
            serde_json::from_reader(File::open(q.path("done", &e.id)).unwrap()).unwrap();
        assert_eq!(c.entry, e);
        assert_eq!(c.result.status, "created");
    }
    #[test]
    fn invalid_result_keeps_pending_and_cannot_escape() {
        let (_root, q) = queue();
        let e = entry();
        q.add(&e).unwrap();
        for (id, status, bookmark_id) in [
            (e.id.as_str(), "failed", "42"),
            (e.id.as_str(), "created", ""),
            ("../../escape", "created", "42"),
            (
                "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                "created",
                "42",
            ),
        ] {
            assert!(
                q.complete(Receipt {
                    id: id.into(),
                    status: status.into(),
                    bookmark_id: bookmark_id.into()
                })
                .is_err()
            );
        }
        assert_eq!(q.pending().unwrap(), vec![e]);
    }
    #[test]
    fn concurrent_producers_create_one_complete_json() {
        let (root, q) = queue();
        let handles: Vec<_> = (0..8)
            .map(|_| {
                let path = root.path().to_owned();
                std::thread::spawn(move || Queue::new(path).add(&entry()).unwrap())
            })
            .collect();
        let results: Vec<_> = handles.into_iter().map(|h| h.join().unwrap()).collect();
        assert_eq!(results.iter().filter(|&&r| r == "added").count(), 1);
        assert_eq!(q.pending().unwrap(), vec![entry()]);
    }
    #[test]
    fn partial_or_corrupt_json_does_not_block_valid_entries() {
        let (_root, q) = queue();
        q.add(&entry()).unwrap();
        fs::write(q.root.join("pending/bad.json"), "{").unwrap();
        fs::write(q.root.join("pending/writing.tmp"), "{").unwrap();
        assert_eq!(q.pending().unwrap(), vec![entry()]);
    }
    #[test]
    fn authenticated_http_flow_and_rejections() {
        let (_root, q) = queue();
        let e = entry();
        q.add(&e).unwrap();
        let t = q.token().unwrap();
        assert_eq!(
            dispatch(&q, &t, request("GET", "/pending", "bad", vec![])).0,
            401
        );
        let mut r = request("GET", "/pending", &t, vec![]);
        r.headers
            .push(("origin".into(), "https://evil.example".into()));
        assert_eq!(dispatch(&q, &t, r).0, 403);
        let mut r = request("GET", "/pending", &t, vec![]);
        r.headers[0].1 = "evil.example".into();
        assert_eq!(dispatch(&q, &t, r).0, 401);
        let (code, body) = dispatch(&q, &t, request("GET", "/pending", &t, vec![]));
        assert_eq!(code, 200);
        assert_eq!(body["entries"][0]["id"], e.id);
        assert_eq!(
            dispatch(&q, &t, request("POST", "/result", &t, b"{}".to_vec())).0,
            400
        );
        assert_eq!(q.pending().unwrap().len(), 1);
        let body = serde_json::to_vec(&Receipt {
            id: e.id,
            status: "exists".into(),
            bookmark_id: "bookmark-guid".into(),
        })
        .unwrap();
        assert_eq!(
            dispatch(&q, &t, request("POST", "/result", &t, body)).0,
            200
        );
        assert!(q.pending().unwrap().is_empty());
    }
    fn parse_wire(wire: &[u8]) -> Result<Request> {
        let listener = TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, 0))?;
        let addr = listener.local_addr()?;
        let bytes = wire.to_owned();
        let sender = std::thread::spawn(move || {
            let mut s = TcpStream::connect(addr).unwrap();
            s.write_all(&bytes).unwrap();
            s.shutdown(std::net::Shutdown::Write).unwrap();
        });
        let (mut socket, _) = listener.accept()?;
        let result = read_request(&mut socket);
        sender.join().unwrap();
        result
    }
    #[test]
    fn http_parser_accepts_json_and_rejects_bad_framing() {
        let r = parse_wire(
            b"POST /result HTTP/1.1\r\nHost: 127.0.0.1:17643\r\nContent-Length: 2\r\n\r\n{}",
        )
        .unwrap();
        assert_eq!(r.body, b"{}");
        for raw in [
            b"GET /pending HTTP/1.1\r\nHost: x\r\nHost: y\r\n\r\n".as_slice(),
            b"POST /result HTTP/1.1\r\nContent-Length: 999999\r\n\r\n",
            b"POST /result HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n",
            b"POST /result HTTP/1.1\r\nContent-Length: 2\r\n\r\n{",
        ] {
            assert!(parse_wire(raw).is_err());
        }
    }
    #[test]
    fn task_is_interactive_hidden_and_not_time_limited() {
        let xml = task::task_xml("DOMAIN\\Owner");
        assert!(xml.contains("<Command>wscript.exe</Command>"));
        assert!(xml.contains("<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>"));
        assert!(xml.contains("<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>"));
        assert!(task::hidden_launcher().contains(", 0, True)"));
        assert!(task::hidden_launcher().contains("WScript.Quit rc"));
    }
}
