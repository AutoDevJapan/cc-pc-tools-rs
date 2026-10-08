#![cfg(windows)]

use safe_delete_recovery::powershell_literal as quote;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    ffi::OsStr,
    fs::{self, OpenOptions},
    io::Write,
    os::windows::{ffi::OsStrExt, fs::MetadataExt, process::CommandExt},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    time::{Duration, Instant},
};

type Result<T> = std::result::Result<T, &'static str>;
const ID: &str = "dee85bb67fa66449ba0330f0fbe049f1";
const LIMIT: usize = 10000;
const REPARSE: u32 = 0x400;

#[derive(Serialize, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
struct Identity {
    version: u32,
    id: String,
    root: String,
    path: String,
    volume: u32,
    file_id: u64,
    created_at: String,
    project: String,
    content: String,
}
#[derive(Serialize, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
struct Receipt {
    #[serde(flatten)]
    identity: Identity,
    integrity: String,
    state: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    destination: Option<String>,
}
#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Node {
    volume: u32,
    file_id: u64,
    size: u64,
    links: u32,
    attributes: u32,
    written: u64,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Ticket {
    id: String,
    source: String,
    destination: String,
    nodes: BTreeMap<String, Node>,
}
struct Repo {
    name: &'static str,
    origin: &'static str,
    head: String,
}
struct Config {
    store: PathBuf,
    temp: PathBuf,
    quarantine: PathBuf,
    repos: Vec<Repo>,
    guard: PathBuf,
}

impl Config {
    fn production() -> Result<Self> {
        let parent = std::env::current_exe()
            .map_err(|_| "EXE_PATH")?
            .parent()
            .ok_or("EXE_PATH")?
            .to_owned();
        Ok(Self {
            store: parent.join("receipts-go"),
            temp: std::env::temp_dir(),
            quarantine: PathBuf::from(r"C:\Users\makhmeto\Downloads\削除希望"),
            guard: PathBuf::from("D:/CC/Tools/client-records/scrub391.exe"),
            repos: vec![
                Repo {
                    name: "kcaldav",
                    origin: "https://github.com/katsushi2441/kcaldav.git",
                    head: "e4a3ebd701a86d23fadfde4ae8be27d9bb6ec77c".into(),
                },
                Repo {
                    name: "plane-jp",
                    origin: "https://github.com/katsushi2441/plane-jp.git",
                    head: "1c5078295bc8a33966d0b22ee77cd0194f49e1dc".into(),
                },
            ],
        })
    }
    fn receipt_path(&self) -> PathBuf {
        self.store.join(format!("{ID}.json"))
    }
    fn ticket_path(&self) -> PathBuf {
        self.store.join(format!("{ID}.download-quarantine.json"))
    }
    fn destination(&self) -> PathBuf {
        self.quarantine
            .join(format!("20261008-todo-2001-tool-verification-{}", &ID[..8]))
    }

    fn load(&self, id: &str) -> Result<Receipt> {
        if id != ID {
            return Err("SCOPE_NOT_AUTHORIZED");
        }
        checked(&self.receipt_path())?;
        let file = node(&self.receipt_path())?;
        if file.links != 1 || file.size > 8192 || file.attributes & 0x10 != 0 {
            return Err("INVALID_RECEIPT_FILE");
        }
        let r: Receipt =
            serde_json::from_slice(&fs::read(self.receipt_path()).map_err(|_| "RECEIPT_READ")?)
                .map_err(|_| "INVALID_RECEIPT_FILE")?;
        let expected = self.temp.join(format!("codex-owned-{ID}"));
        if r.identity.version != 2
            || r.identity.id != ID
            || r.identity.project != "todo-2001"
            || r.identity.content != "tool-verification"
            || !same(Path::new(&r.identity.root), &self.temp)
            || !same(Path::new(&r.identity.path), &expected)
        {
            return Err("RECEIPT_SCOPE_MISMATCH");
        }
        let json = serde_json::to_vec(&r.identity).map_err(|_| "RECEIPT_JSON")?;
        if format!("{:x}", Sha256::digest(json)) != r.integrity {
            return Err("RECEIPT_INTEGRITY_MISMATCH");
        }
        if r.state != "owned" && r.state != "quarantined" {
            return Err("RECEIPT_NOT_OWNED");
        }
        checked(&expected)?;
        Ok(r)
    }
    fn client_guard(&self, path: &Path) -> Result<()> {
        let output = hidden(&self.guard, &[OsStr::new("--guard-path"), path.as_os_str()])?;
        let guard: serde_json::Value =
            serde_json::from_slice(&output).map_err(|_| "CLIENT_GUARD_FAILED")?;
        if guard.get("client_resource").and_then(|x| x.as_bool()) != Some(false) {
            return Err("CLIENT_RESOURCE");
        }
        Ok(())
    }
    fn validate(&self, r: &Receipt, path: &Path) -> Result<BTreeMap<String, Node>> {
        checked(path)?;
        self.client_guard(path)?;
        let n = node(path)?;
        if n.attributes & 0x10 == 0
            || n.volume != r.identity.volume
            || n.file_id != r.identity.file_id
        {
            return Err("TARGET_IDENTITY_CHANGED");
        }
        let nodes = scan(path, &self.repos)?;
        for repo in &self.repos {
            validate_repo(&path.join(repo.name), repo)?;
        }
        Ok(nodes)
    }
    fn plan(&self, id: &str) -> Result<serde_json::Value> {
        let r = self.load(id)?;
        if r.state != "owned" {
            return Err("RECEIPT_NOT_OWNED");
        }
        let path = Path::new(&r.identity.path);
        let nodes = self.validate(&r, path)?;
        let dest = self.destination();
        checked(&dest)?;
        self.client_guard(&dest)?;
        if path.components().next() != dest.components().next() {
            return Err("QUARANTINE_CROSS_DRIVE");
        }
        let prepared = present(&self.ticket_path())?;
        if present(&dest)? && !prepared {
            return Err("QUARANTINE_ALREADY_PREPARED");
        }
        let bytes: u64 = nodes
            .values()
            .filter(|x| x.attributes & 0x10 == 0)
            .map(|x| x.size)
            .sum();
        let why = format!(
            "退避状態: 未完了（移動前）。\n元の場所: {}\n大きさ: {bytes} byte\n理由: 所有一時データ内の公開配布物にGit管理領域があり、通常の削除計画と退避が停止した。\n取得元・commit・未変更・外部リンクなしを確認し、Gitを含む全内容を保持して退避する。\n所有ID: {ID}\n",
            r.identity.path
        );
        let ticket = Ticket {
            id: ID.into(),
            source: r.identity.path.clone(),
            destination: dest.to_string_lossy().into_owned(),
            nodes,
        };
        let ticket_bytes = serde_json::to_vec_pretty(&ticket).map_err(|_| "TICKET_JSON")?;
        // Publish intent before creating the destination so an interrupted plan can resume.
        write_or_verify(&self.ticket_path(), &ticket_bytes, 8 * 1024 * 1024)?;
        fs::create_dir_all(&self.quarantine).map_err(|_| "QUARANTINE_PARENT_CREATE")?;
        checked(&dest)?;
        if !present(&dest)? {
            fs::create_dir(&dest).map_err(|_| "QUARANTINE_DESTINATION_CREATE")?;
        }
        for entry in fs::read_dir(&dest).map_err(|_| "QUARANTINE_METADATA_FAILED")? {
            if entry.map_err(|_| "QUARANTINE_METADATA_FAILED")?.file_name()
                != OsStr::new("理由.txt")
            {
                return Err("QUARANTINE_DESTINATION_NOT_EMPTY");
            }
        }
        write_or_verify(&dest.join("理由.txt"), why.as_bytes(), 8192)?;
        Ok(
            serde_json::json!({"id": ID, "action": "submit_quarantine_to_exec_tool", "bytes": bytes,
            "entries": ticket.nodes.len(), "destination": dest, "reason": "owned_public_download_git",
            "powershell_command": format!("Move-Item -LiteralPath {} -Destination {} -ErrorAction Stop", quote(path), quote(&dest.join("files"))) }),
        )
    }
    fn finish(&self, id: &str) -> Result<serde_json::Value> {
        let mut r = self.load(id)?;
        checked(&self.ticket_path())?;
        let tn = node(&self.ticket_path())?;
        if tn.links != 1 || tn.size > 8 * 1024 * 1024 {
            return Err("INVALID_TICKET");
        }
        let t: Ticket =
            serde_json::from_slice(&fs::read(self.ticket_path()).map_err(|_| "TICKET_READ")?)
                .map_err(|_| "INVALID_TICKET")?;
        let dest = self.destination();
        if t.id != ID || t.source != r.identity.path || !same(Path::new(&t.destination), &dest) {
            return Err("TICKET_SCOPE_MISMATCH");
        }
        if r.state == "quarantined" && r.destination.as_deref() != Some(t.destination.as_str()) {
            return Err("RECEIPT_DESTINATION_MISMATCH");
        }
        if present(Path::new(&r.identity.path))? {
            return Err("TARGET_STILL_EXISTS");
        }
        let files = dest.join("files");
        let nodes = self.validate(&r, &files)?;
        if nodes != t.nodes {
            return Err("CONTENT_CHANGED_AFTER_PLAN");
        }
        let why = dest.join("理由.txt");
        checked(&why)?;
        if node(&why)?.links != 1 {
            return Err("REASON_HARD_LINK");
        }
        let completed =
            "退避状態: 完了。元の場所の不在と、退避先の識別番号・全内容の一致を確認した。\n";
        if node(&why)?.size > 8192 {
            return Err("INVALID_REASON_FILE");
        }
        let reason = fs::read(&why).map_err(|_| "REASON_READ_FAILED")?;
        if !reason
            .windows(completed.len())
            .any(|s| s == completed.as_bytes())
        {
            let mut f = OpenOptions::new()
                .append(true)
                .open(why)
                .map_err(|_| "REASON_UPDATE_FAILED")?;
            f.write_all(completed.as_bytes())
                .map_err(|_| "REASON_UPDATE_FAILED")?;
            f.sync_all().map_err(|_| "REASON_UPDATE_FAILED")?;
        }
        if r.state == "owned" {
            r.state = "quarantined".into();
            r.destination = Some(t.destination.clone());
            let writing = self.receipt_path().with_extension("recovery-writing");
            write_or_verify(
                &writing,
                &serde_json::to_vec(&r).map_err(|_| "RECEIPT_JSON")?,
                8192,
            )?;
            fs::rename(writing, self.receipt_path()).map_err(|_| "RECEIPT_COMMIT_FAILED")?;
        }
        Ok(
            serde_json::json!({"id":ID, "state":"quarantined", "source_absent":true,
            "destination":dest, "bytes": nodes.values().filter(|x| x.attributes & 0x10 == 0).map(|x| x.size).sum::<u64>(),
            "entries":nodes.len(), "inventory_verified":true}),
        )
    }
}

fn same(a: &Path, b: &Path) -> bool {
    a.components()
        .map(|c| c.as_os_str().to_string_lossy().to_ascii_lowercase())
        .eq(b
            .components()
            .map(|c| c.as_os_str().to_string_lossy().to_ascii_lowercase()))
}
fn protected(name: &str) -> bool {
    [".codex", ".agents", ".ssh", "openmausbot"]
        .iter()
        .any(|x| name.eq_ignore_ascii_case(x))
}
fn checked(path: &Path) -> Result<()> {
    if !path.is_absolute()
        || path
            .components()
            .any(|c| matches!(c, std::path::Component::ParentDir))
    {
        return Err("INVALID_PATH");
    }
    for p in path.ancestors() {
        if let Some(name) = p.file_name() {
            let name = name.to_string_lossy();
            if protected(&name) || name.eq_ignore_ascii_case(".git") {
                return Err("PROTECTED_PATH");
            }
        }
        match fs::symlink_metadata(p) {
            Ok(m) if m.file_attributes() & REPARSE != 0 => return Err("LINK_OR_REPARSE_POINT"),
            Ok(m) if p != path && !m.is_dir() => return Err("ANCESTOR_NOT_DIRECTORY"),
            Ok(_) => (),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => (),
            Err(_) => return Err("PATH_METADATA_FAILED"),
        }
    }
    Ok(())
}
fn present(path: &Path) -> Result<bool> {
    match fs::symlink_metadata(path) {
        Ok(_) => Ok(true),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(false),
        Err(_) => Err("PATH_METADATA_FAILED"),
    }
}
fn write_new(path: &Path, bytes: &[u8]) -> Result<()> {
    checked(path)?;
    let mut f = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
        .map_err(|_| "RECORD_CREATE_FAILED")?;
    f.write_all(bytes)
        .and_then(|()| f.sync_all())
        .map_err(|_| "RECORD_WRITE_FAILED")
}
fn write_or_verify(path: &Path, bytes: &[u8], limit: u64) -> Result<()> {
    checked(path)?;
    if present(path)? {
        let n = node(path)?;
        if n.links != 1 || n.size > limit || n.attributes & 0x10 != 0 {
            return Err("INVALID_RECOVERY_RECORD");
        }
        if fs::read(path).map_err(|_| "RECOVERY_RECORD_READ")? != bytes {
            return Err("RECOVERY_RECORD_CHANGED");
        }
        Ok(())
    } else {
        write_new(path, bytes)
    }
}
fn scan(root: &Path, repos: &[Repo]) -> Result<BTreeMap<String, Node>> {
    let mut nodes = BTreeMap::new();
    let mut pending = vec![root.to_owned()];
    let mut links: BTreeMap<(u32, u64), (u32, u32)> = BTreeMap::new();
    while let Some(path) = pending.pop() {
        let relative = path
            .strip_prefix(root)
            .map_err(|_| "PATH_OUTSIDE_ROOT")?
            .to_string_lossy()
            .replace('\\', "/");
        let name = path.file_name().ok_or("INVALID_PATH")?.to_string_lossy();
        if protected(&name) {
            return Err("PROTECTED_PATH");
        }
        if name.eq_ignore_ascii_case(".git")
            && !repos.iter().any(|r| relative == format!("{}/.git", r.name))
        {
            return Err("UNDECLARED_GIT_METADATA");
        }
        let n = node(&path)?;
        if name.eq_ignore_ascii_case(".git") && n.attributes & 0x10 == 0 {
            return Err("EXTERNAL_GITDIR");
        }
        if nodes.len() >= LIMIT {
            return Err("ENTRY_LIMIT_EXCEEDED");
        }
        if n.attributes & 0x10 != 0 {
            for child in fs::read_dir(&path).map_err(|_| "CONTENT_METADATA_FAILED")? {
                let child = child.map_err(|_| "CONTENT_METADATA_FAILED")?;
                if nodes.len() + pending.len() >= LIMIT {
                    return Err("ENTRY_LIMIT_EXCEEDED");
                }
                pending.push(child.path());
            }
        } else {
            let count = links.entry((n.volume, n.file_id)).or_insert((0, n.links));
            if count.1 != n.links {
                return Err("HARD_LINK");
            }
            count.0 += 1;
        }
        nodes.insert(relative, n);
    }
    if links.values().any(|(inside, total)| inside != total) {
        return Err("HARD_LINK");
    }
    Ok(nodes)
}
fn hidden(exe: &Path, args: &[&OsStr]) -> Result<Vec<u8>> {
    let mut c = Command::new(exe);
    c.args(args)
        .creation_flags(0x08000000)
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    // Isolate Git from inherited repository/config/credential environment.
    for (key, _) in std::env::vars_os() {
        if key
            .to_string_lossy()
            .to_ascii_uppercase()
            .starts_with("GIT_")
        {
            c.env_remove(key);
        }
    }
    c.env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_CONFIG_GLOBAL", "NUL")
        .env("GIT_TERMINAL_PROMPT", "0");
    let mut child = c.spawn().map_err(|_| "VALIDATOR_START_FAILED")?;
    let out = child.stdout.take().ok_or("VALIDATOR_OUTPUT")?;
    let reader = std::thread::spawn(move || {
        use std::io::Read;
        let mut b = Vec::new();
        out.take(1024 * 1024 + 1).read_to_end(&mut b).map(|_| b)
    });
    let start = Instant::now();
    let status = loop {
        match child.try_wait() {
            Ok(Some(s)) => break s,
            Ok(None) if start.elapsed() < Duration::from_secs(30) => {
                std::thread::sleep(Duration::from_millis(20))
            }
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                return Err("VALIDATOR_TIMEOUT");
            }
        }
    };
    let output = reader
        .join()
        .map_err(|_| "VALIDATOR_OUTPUT")?
        .map_err(|_| "VALIDATOR_OUTPUT")?;
    if !status.success() || output.len() > 1024 * 1024 {
        return Err("VALIDATOR_FAILED");
    }
    Ok(output)
}
fn git(path: &Path, args: &[&str]) -> Result<String> {
    let fixed = [
        "--no-optional-locks",
        "-c",
        "core.fsmonitor=false",
        "-c",
        "core.hooksPath=NUL",
        "-C",
    ];
    let mut argv: Vec<&OsStr> = fixed.iter().map(OsStr::new).collect();
    argv.push(path.as_os_str());
    argv.extend(args.iter().map(OsStr::new));
    String::from_utf8(hidden(Path::new("git.exe"), &argv)?)
        .map(|s| s.trim().into())
        .map_err(|_| "GIT_OUTPUT")
}
fn validate_repo(path: &Path, repo: &Repo) -> Result<()> {
    let gitdir = path.join(".git");
    if node(&gitdir)?.attributes & 0x10 == 0 {
        return Err("EXTERNAL_GITDIR");
    }
    for special in [
        "commondir",
        "gitdir",
        "worktrees",
        "modules",
        "objects/info/alternates",
        "objects/info/http-alternates",
    ] {
        if present(&gitdir.join(special))? {
            return Err("EXTERNAL_OR_SHARED_GIT_METADATA");
        }
    }
    if !same(
        Path::new(&git(path, &["rev-parse", "--show-toplevel"])?),
        path,
    ) {
        return Err("GIT_TOP_MISMATCH");
    }
    if git(path, &["rev-parse", "HEAD"])? != repo.head {
        return Err("GIT_HEAD_CHANGED");
    }
    if git(
        path,
        &["config", "--local", "--get-all", "remote.origin.url"],
    )? != repo.origin
    {
        return Err("GIT_ORIGIN_CHANGED");
    }
    if !git(path, &["status", "--porcelain=v1", "--untracked-files=no"])?.is_empty() {
        return Err("GIT_TRACKED_CHANGES");
    }
    if !git(
        path,
        &["rev-list", "--branches", "--not", "--remotes=origin"],
    )?
    .is_empty()
    {
        return Err("GIT_UNPUSHED_COMMITS");
    }
    if !git(path, &["for-each-ref", "--format=%(refname)", "refs/stash"])?.is_empty() {
        return Err("GIT_STASH");
    }
    Ok(())
}

#[repr(C)]
#[derive(Default)]
struct WinInfo {
    attr: u32,
    created: [u32; 2],
    accessed: [u32; 2],
    written: [u32; 2],
    volume: u32,
    size_high: u32,
    size_low: u32,
    links: u32,
    index_high: u32,
    index_low: u32,
}
#[link(name = "kernel32")]
unsafe extern "system" {
    fn CreateFileW(
        name: *const u16,
        access: u32,
        share: u32,
        security: *const u8,
        disposition: u32,
        flags: u32,
        template: isize,
    ) -> isize;
    fn GetFileInformationByHandle(handle: isize, info: *mut WinInfo) -> i32;
    fn CloseHandle(handle: isize) -> i32;
}
fn node(path: &Path) -> Result<Node> {
    let name: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
    let mut info = WinInfo::default();
    // Metadata access only, with OPEN_REPARSE_POINT; never follows a junction.
    let ok = unsafe {
        let h = CreateFileW(name.as_ptr(), 0x80, 7, std::ptr::null(), 3, 0x02200000, 0);
        if h == -1 {
            return Err("IDENTITY_READ_FAILED");
        }
        let ok = GetFileInformationByHandle(h, &mut info);
        CloseHandle(h);
        ok
    };
    if ok == 0 {
        return Err("IDENTITY_READ_FAILED");
    }
    if info.attr & REPARSE != 0 {
        return Err("LINK_OR_REPARSE_POINT");
    }
    Ok(Node {
        volume: info.volume,
        file_id: ((info.index_high as u64) << 32) | info.index_low as u64,
        size: ((info.size_high as u64) << 32) | info.size_low as u64,
        links: info.links,
        attributes: info.attr,
        written: ((info.written[1] as u64) << 32) | info.written[0] as u64,
    })
}
fn run() -> Result<serde_json::Value> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() != 3 || args[1] != "--id" {
        return Err("INVALID_ARGUMENTS");
    }
    let c = Config::production()?;
    match args[0].as_str() {
        "plan" => c.plan(&args[2]),
        "finish" => c.finish(&args[2]),
        _ => Err("INVALID_ACTION"),
    }
}
fn main() {
    match run() {
        Ok(v) => println!("{v}"),
        Err(code) => {
            println!("{}", serde_json::json!({"ok":false,"code":code}));
            std::process::exit(2);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ops::{Deref, DerefMut};
    use std::sync::atomic::{AtomicU64, Ordering};
    static SERIAL: AtomicU64 = AtomicU64::new(0);
    struct Fixture {
        config: Config,
        base: PathBuf,
        identity: Node,
    }
    impl Deref for Fixture {
        type Target = Config;
        fn deref(&self) -> &Config {
            &self.config
        }
    }
    impl DerefMut for Fixture {
        fn deref_mut(&mut self) -> &mut Config {
            &mut self.config
        }
    }
    // Only test-created cases: this is never compiled into the production tool.
    fn remove_fixture(base: &Path, identity: &Node) -> Result<()> {
        let owner =
            PathBuf::from(std::env::var_os("SAFE_DELETE_TEST_ROOT").ok_or("TEST_ROOT_MISSING")?);
        checked(&owner)?;
        checked(base)?;
        if base.parent() != Some(owner.as_path())
            || !base
                .file_name()
                .ok_or("INVALID_TEST_PATH")?
                .to_string_lossy()
                .starts_with("case-")
        {
            return Err("TEST_PATH_OUTSIDE_OWNER");
        }
        let current = node(base)?;
        if current.volume != identity.volume || current.file_id != identity.file_id {
            return Err("TEST_IDENTITY_CHANGED");
        }
        let mut pending = vec![base.to_owned()];
        while let Some(p) = pending.pop() {
            let m = fs::symlink_metadata(&p).map_err(|_| "TEST_METADATA_FAILED")?;
            if m.file_attributes() & REPARSE != 0 {
                continue;
            }
            if m.is_dir() {
                for entry in fs::read_dir(&p).map_err(|_| "TEST_METADATA_FAILED")? {
                    pending.push(entry.map_err(|_| "TEST_METADATA_FAILED")?.path());
                }
            } else if m.permissions().readonly() {
                let mut permissions = m.permissions();
                permissions.set_readonly(false);
                fs::set_permissions(&p, permissions).map_err(|_| "TEST_ATTRIBUTE_FAILED")?;
            }
        }
        // Windows remove_dir_all unlinks reparse points rather than traversing them.
        fs::remove_dir_all(base).map_err(|_| "TEST_FIXTURE_CLEANUP_FAILED")
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            if let Err(code) = remove_fixture(&self.base, &self.identity) {
                if std::thread::panicking() {
                    eprintln!("test fixture retained: {code}");
                } else {
                    panic!("test fixture cleanup failed: {code}");
                }
            }
        }
    }
    #[test]
    #[ignore = "explicit recovery for cases generated by earlier versions in this owned test root"]
    fn retire_prior_generated_fixtures() {
        let owner = PathBuf::from(std::env::var_os("SAFE_DELETE_TEST_ROOT").unwrap());
        // The exact owned root is created by this task. No other root is accepted for legacy retirement.
        assert!(same(
            &owner,
            Path::new(
                r"C:\Users\makhmeto\AppData\Local\Temp\codex-owned-3d76fd4c38f5355b9378be03e062914a"
            )
        ));
        checked(&owner).unwrap();
        for entry in fs::read_dir(&owner).unwrap() {
            let path = entry.unwrap().path();
            if path
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("case-")
            {
                let n = node(&path).unwrap();
                remove_fixture(&path, &n).unwrap();
            }
        }
    }
    #[test]
    fn real_go_receipt_checksum_is_compatible() {
        let identity = Identity {
            version: 2,
            id: ID.into(),
            root: r"C:\Users\makhmeto\AppData\Local\Temp".into(),
            path: format!(r"C:\Users\makhmeto\AppData\Local\Temp\codex-owned-{ID}"),
            volume: 2229119083,
            file_id: 15481123719934670,
            created_at: "2026-10-08T15:34:13.325996+09:00".into(),
            project: "todo-2001".into(),
            content: "tool-verification".into(),
        };
        assert_eq!(
            format!(
                "{:x}",
                Sha256::digest(serde_json::to_vec(&identity).unwrap())
            ),
            "3451328d31fcb9f36297c8d085cb3743de54fafec0b248ed14e23c1b18f68ce7"
        );
    }
    fn fixture() -> Fixture {
        let owner = std::env::var_os("SAFE_DELETE_TEST_ROOT")
            .expect("tests require an owned safe-delete folder");
        let base = PathBuf::from(owner).join(format!(
            "case-{}-{}",
            std::process::id(),
            SERIAL.fetch_add(1, Ordering::SeqCst)
        ));
        fs::create_dir(&base).unwrap();
        let mut c = Config::production().unwrap();
        if let Some(guard) = std::env::var_os("SAFE_DELETE_TEST_GUARD") {
            c.guard = guard.into();
        }
        c.temp = base.clone();
        c.store = base.join("receipts");
        c.quarantine = base.join("quarantine");
        let mut fixture = Fixture {
            config: c,
            identity: node(&base).unwrap(),
            base: base.clone(),
        };
        let c = &mut fixture.config;
        fs::create_dir(&c.store).unwrap();
        let target = base.join(format!("codex-owned-{ID}"));
        fs::create_dir(&target).unwrap();
        for repo in &mut c.repos {
            let path = target.join(repo.name);
            fs::create_dir(&path).unwrap();
            git(&path, &["init", "-b", "main"]).unwrap();
            fs::write(path.join("source.txt"), b"public fixture\n").unwrap();
            git(&path, &["add", "source.txt"]).unwrap();
            git(
                &path,
                &[
                    "-c",
                    "user.name=Fixture",
                    "-c",
                    "user.email=fixture@example.invalid",
                    "commit",
                    "-m",
                    "fixture",
                ],
            )
            .unwrap();
            repo.head = git(&path, &["rev-parse", "HEAD"]).unwrap();
            git(&path, &["remote", "add", "origin", repo.origin]).unwrap();
            git(
                &path,
                &["update-ref", "refs/remotes/origin/main", &repo.head],
            )
            .unwrap();
        }
        let n = node(&target).unwrap();
        let identity = Identity {
            version: 2,
            id: ID.into(),
            root: base.to_string_lossy().into_owned(),
            path: target.to_string_lossy().into_owned(),
            volume: n.volume,
            file_id: n.file_id,
            created_at: "2026-10-08T15:34:13.325996+09:00".into(),
            project: "todo-2001".into(),
            content: "tool-verification".into(),
        };
        let integrity = format!(
            "{:x}",
            Sha256::digest(serde_json::to_vec(&identity).unwrap())
        );
        let r = Receipt {
            identity,
            integrity,
            state: "owned".into(),
            destination: None,
        };
        write_new(&c.receipt_path(), &serde_json::to_vec(&r).unwrap()).unwrap();
        fixture
    }
    fn source(c: &Config) -> PathBuf {
        c.temp.join(format!("codex-owned-{ID}"))
    }
    #[test]
    fn receipt_digest_matches_go_and_tampering_is_rejected() {
        let c = fixture();
        let mut r = c.load(ID).unwrap();
        r.identity.content = "different".into();
        fs::write(c.receipt_path(), serde_json::to_vec(&r).unwrap()).unwrap();
        assert!(matches!(c.load(ID), Err("RECEIPT_SCOPE_MISMATCH")));
    }
    #[test]
    fn valid_plan_preserves_git_and_finish_checks_whole_inventory() {
        let c = fixture();
        let source = source(&c);
        fs::write(source.join("trial-session"), b"disposable fixture").unwrap();
        let plan = c.plan(ID).unwrap();
        assert!(
            plan["powershell_command"]
                .as_str()
                .unwrap()
                .starts_with("Move-Item -LiteralPath")
        );
        assert!(matches!(c.finish(ID), Err("TARGET_STILL_EXISTS")));
        // Test-only rename models the separately submitted ordinary Move-Item.
        fs::rename(&source, c.destination().join("files")).unwrap();
        let result = c.finish(ID).unwrap();
        assert_eq!(result["state"], "quarantined");
        assert_eq!(c.load(ID).unwrap().state, "quarantined");
        assert!(c.destination().join("files/kcaldav/.git").is_dir());
        assert!(c.destination().join("files/trial-session").is_file());
        assert!(!source.exists());
        c.finish(ID).unwrap();
    }
    #[test]
    fn dirty_tracked_file_is_rejected_without_preparing_destination() {
        let c = fixture();
        fs::write(source(&c).join("kcaldav/source.txt"), b"unsaved work").unwrap();
        assert!(matches!(c.plan(ID), Err("GIT_TRACKED_CHANGES")));
        assert!(!c.destination().exists());
        assert!(source(&c).is_dir());
    }
    #[test]
    fn other_receipts_and_nested_git_and_protected_bot_are_rejected() {
        let c = fixture();
        assert!(matches!(
            c.plan("00000000000000000000000000000000"),
            Err("SCOPE_NOT_AUTHORIZED")
        ));
        fs::create_dir(source(&c).join("OpenMausBot")).unwrap();
        assert!(matches!(c.plan(ID), Err("PROTECTED_PATH")));
        let c = fixture();
        fs::create_dir_all(source(&c).join("extra/.git")).unwrap();
        assert!(matches!(c.plan(ID), Err("UNDECLARED_GIT_METADATA")));
    }
    #[test]
    fn external_git_alternates_and_unpushed_history_are_rejected() {
        let c = fixture();
        fs::write(
            source(&c).join("kcaldav/.git/objects/info/alternates"),
            b"C:/elsewhere",
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("EXTERNAL_OR_SHARED_GIT_METADATA")));
        let c = fixture();
        let repo = source(&c).join("kcaldav");
        git(
            &repo,
            &[
                "update-ref",
                "refs/remotes/origin/main",
                "0000000000000000000000000000000000000000",
            ],
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("GIT_UNPUSHED_COMMITS")));
    }
    #[test]
    fn external_hardlink_is_rejected() {
        let c = fixture();
        fs::hard_link(
            source(&c).join("kcaldav/source.txt"),
            c.temp.join("external.txt"),
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("HARD_LINK")));
        assert!(!c.destination().exists());
    }
    #[test]
    fn replaced_source_identity_and_changed_moved_inventory_are_rejected() {
        let c = fixture();
        let src = source(&c);
        fs::rename(&src, c.temp.join("original")).unwrap();
        fs::create_dir(&src).unwrap();
        assert!(matches!(c.plan(ID), Err("TARGET_IDENTITY_CHANGED")));
        let c = fixture();
        c.plan(ID).unwrap();
        fs::rename(source(&c), c.destination().join("files")).unwrap();
        fs::write(c.destination().join("files/new-data"), b"unexpected write").unwrap();
        assert!(matches!(c.finish(ID), Err("CONTENT_CHANGED_AFTER_PLAN")));
        assert_eq!(c.load(ID).unwrap().state, "owned");
    }
    #[test]
    fn trailing_separator_and_interrupted_plan_and_receipt_commit_resume() {
        let mut c = fixture();
        c.temp = PathBuf::from(format!("{}\\", c.temp.display()));
        c.load(ID).unwrap();
        let first = c.plan(ID).unwrap();
        let second = c.plan(ID).unwrap();
        assert_eq!(first, second);
        let src = source(&c);
        fs::rename(src, c.destination().join("files")).unwrap();
        let mut r = c.load(ID).unwrap();
        r.state = "quarantined".into();
        r.destination = Some(c.destination().to_string_lossy().into_owned());
        let writing = c.receipt_path().with_extension("recovery-writing");
        write_new(&writing, &serde_json::to_vec(&r).unwrap()).unwrap();
        c.finish(ID).unwrap();
        c.finish(ID).unwrap();
        let reason = fs::read_to_string(c.destination().join("理由.txt")).unwrap();
        assert_eq!(reason.matches("退避状態: 完了。").count(), 1);
    }
    #[test]
    fn destination_collision_and_gitdir_pointer_are_rejected() {
        let c = fixture();
        fs::create_dir_all(c.destination()).unwrap();
        assert!(matches!(c.plan(ID), Err("QUARANTINE_ALREADY_PREPARED")));
        let c = fixture();
        let p = source(&c).join("kcaldav/.git");
        fs::rename(&p, source(&c).join("git-preserved")).unwrap();
        fs::write(p, b"gitdir: C:/external\n").unwrap();
        assert!(matches!(c.plan(ID), Err("EXTERNAL_GITDIR")));
    }
    #[test]
    fn junction_and_missing_client_guard_are_rejected() {
        let c = fixture();
        let link = source(&c).join("junction");
        let cmd = format!(
            "New-Item -ItemType Junction -Path {} -Target {} | Out-Null",
            quote(&link),
            quote(&c.store)
        );
        hidden(
            Path::new("pwsh.exe"),
            &[
                OsStr::new("-NoProfile"),
                OsStr::new("-NonInteractive"),
                OsStr::new("-Command"),
                OsStr::new(&cmd),
            ],
        )
        .unwrap();
        assert!(matches!(c.plan(ID), Err("LINK_OR_REPARSE_POINT")));
        let mut c = fixture();
        c.guard = c.temp.join("missing-guard.exe");
        assert!(matches!(c.plan(ID), Err("VALIDATOR_START_FAILED")));
        assert!(!c.destination().exists());
    }
}
