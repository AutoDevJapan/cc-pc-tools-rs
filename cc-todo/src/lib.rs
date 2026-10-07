//! Shared to-do list for every agent on this PC, with a stall watchdog.
//!
//! Items belong to a project (a repository path). An item is open until done. The watchdog (run every minute by
//! discord_ask.py tick) notices a project that has open, unblocked items but no running Codex job under its path,
//! and posts a notice to the owner on Discord, so work never silently stops (owner, 2026-09-30: 16:00-21:00 lost).
//!
//!   python todo.py project civ6 C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki
//!   python todo.py add civ6 "誓約を実装する" [--note "..."]
//!   python todo.py list [civ6] [--all]
//!   python todo.py start <id> [--job <codex job id>]   # mark in progress, optionally link a job
//!   python todo.py block <id> "理由"                   # waiting on the owner or something external
//!   python todo.py done <id> [--note "PR #81"]
//!   python todo.py watch                               # stall check (called by tick)

use clap::{Parser, Subcommand};
use serde_json::{Value, json};
use std::{
    fs::{self, File, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};

pub type Result<T> = std::result::Result<T, String>;
pub const STALL_S: f64 = 1200.;
pub const REMENTION_S: f64 = 3600.;

#[derive(Parser, Debug)]
#[command(
    name = "todo",
    disable_help_subcommand = true,
    about = "Shared to-do list for every agent on this PC, with a stall watchdog."
)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Command,
}
#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(args_override_self = true, infer_long_args = true)]
    Project { name: String, path: String },
    #[command(args_override_self = true, infer_long_args = true)]
    Add {
        project: String,
        title: String,
        #[arg(long, default_value = "")]
        note: String,
        #[arg(long, value_parser=clap::builder::ValueParser::new(|s: &str| Ok::<PathBuf,String>(PathBuf::from(s))), help="UTF-8 prompt file to copy into specs/ and authorize automatic dispatch")]
        spec: Option<PathBuf>,
        #[arg(long)]
        model: Option<String>,
        #[arg(long, value_parser=["none","minimal","low","medium","high","xhigh","max"])]
        effort: Option<String>,
        #[arg(long, value_parser=["civ6","cargo"], help="exclusive resource; repeat if needed")]
        needs: Vec<String>,
        #[arg(long, value_parser=clap::value_parser!(u8).range(0..=100))]
        progress: Option<u8>,
    },
    #[command(args_override_self = true, infer_long_args = true)]
    List {
        project: Option<String>,
        #[arg(long)]
        all: bool,
    },
    #[command(args_override_self = true, infer_long_args = true)]
    Start {
        #[arg(allow_negative_numbers = true)]
        id: i64,
        #[arg(long, default_value = "")]
        job: String,
        #[arg(long, value_parser=clap::value_parser!(u8).range(0..=100))]
        progress: Option<u8>,
    },
    #[command(args_override_self = true, infer_long_args = true)]
    Block {
        #[arg(allow_negative_numbers = true)]
        id: i64,
        reason: String,
        #[arg(long, value_parser=clap::value_parser!(u8).range(0..=100))]
        progress: Option<u8>,
    },
    #[command(args_override_self = true, infer_long_args = true)]
    Done {
        #[arg(allow_negative_numbers = true)]
        id: i64,
        #[arg(long, default_value = "")]
        note: String,
    },
    /// Record measured completion without changing status, job ownership or the task note.
    Progress {
        #[arg(allow_negative_numbers = true)]
        id: i64,
        #[arg(value_parser=clap::value_parser!(u8).range(0..=100), required_unless_present="clear")]
        percent: Option<u8>,
        #[arg(long, conflicts_with = "percent")]
        clear: bool,
    },
    #[command(args_override_self = true, infer_long_args = true)]
    Watch,
}

#[derive(Clone)]
pub struct Paths {
    pub store: PathBuf,
    pub lock: PathBuf,
    pub jobs: PathBuf,
    pub thread: PathBuf,
}
impl Paths {
    pub fn from_environment() -> Self {
        let mut paths = Self::production();
        if let Some(home) = std::env::var_os("CC_TODO_HOME") {
            paths = Self::at(Path::new(&home), &paths.jobs);
        }
        if let Some(jobs) = std::env::var_os("CC_TODO_JOBS") {
            paths.jobs = jobs.into();
        }
        paths
    }
    pub fn production() -> Self {
        Self::at(
            Path::new("D:/CC/Tools/todo"),
            Path::new("D:/CC/Tools/codex-jobs/jobs"),
        )
    }
    pub fn at(home: &Path, jobs: &Path) -> Self {
        Self {
            store: home.join("todo.json"),
            lock: home.join("todo.lock"),
            jobs: jobs.into(),
            thread: home.join("stall_thread.txt"),
        }
    }
}
fn err(e: impl std::fmt::Display) -> String {
    e.to_string()
}
pub fn time_s() -> f64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs_f64()
}
pub fn now() -> String {
    chrono::Local::now().format("%Y-%m-%d %H:%M").to_string()
}

/// OS byte lock: released immediately on a crash; never steal a live lock by age.
pub struct Lock(File);
impl Lock {
    pub fn acquire(path: &Path, timeout: Duration) -> Result<Self> {
        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .read(true)
            .open(path)
            .map_err(err)?;
        let deadline = Instant::now() + timeout;
        loop {
            if platform::lock(&file) {
                return Ok(Self(file));
            }
            if Instant::now() >= deadline {
                return Err("todo.lock is busy".into());
            }
            std::thread::sleep(Duration::from_millis(100));
        }
    }
}
impl Drop for Lock {
    fn drop(&mut self) {
        platform::unlock(&self.0);
    }
}
pub fn load(path: &Path) -> Result<Value> {
    if !path.exists() {
        return Ok(json!({"projects":{},"items":[],"next_id":1,"stalls":{}}));
    }
    serde_json::from_str(&fs::read_to_string(path).map_err(err)?).map_err(err)
}
pub fn save(path: &Path, db: &Value) -> Result<()> {
    let backup = PathBuf::from(format!("{}.bak-20260930d", path.display()));
    if path.exists() && !backup.exists() {
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(backup)
            .map_err(err)?;
        file.write_all(&fs::read(path).map_err(err)?).map_err(err)?;
    }
    let tmp = path.with_file_name(format!(
        "{}.{}.tmp",
        path.file_name().unwrap().to_string_lossy(),
        std::process::id()
    ));
    let mut file = File::create(&tmp).map_err(err)?;
    // Preserve unknown fields, insertion order, Unicode and Python's one-space indent.
    let pretty = serde_json::to_string_pretty(db).map_err(err)?;
    let one_space = pretty
        .lines()
        .map(|line| {
            let n = line.bytes().take_while(|b| *b == b' ').count();
            format!("{}{}", " ".repeat(n / 2), &line[n..])
        })
        .collect::<Vec<_>>()
        .join("\n");
    file.write_all(one_space.as_bytes()).map_err(err)?;
    file.flush().map_err(err)?;
    file.sync_all().map_err(err)?;
    drop(file);
    for attempt in 0..50 {
        match platform::replace(&tmp, path) {
            Ok(()) => return Ok(()),
            Err(e)
                if (e.kind() == std::io::ErrorKind::PermissionDenied
                    || matches!(e.raw_os_error(), Some(5 | 32 | 33)))
                    && attempt < 49 =>
            {
                std::thread::sleep(Duration::from_millis(20))
            }
            Err(e) => return Err(err(e)),
        }
    }
    unreachable!()
}

/// Freeze a supplied UTF-8 prompt in the store; titles alone never authorize dispatch.
pub fn add_item(db: &mut Value, args: &Command, paths: &Paths) -> Result<i64> {
    let Command::Add {
        project,
        title,
        note,
        spec,
        model,
        effort,
        needs,
        progress,
    } = args
    else {
        unreachable!()
    };
    let spec = spec.as_ref().filter(|p| !p.as_os_str().is_empty());
    if db["projects"].get(project).is_none() {
        return Err("add failed (ValueError)".into());
    }
    if let Some(m) = model.as_ref().filter(|m| !m.is_empty())
        && (m.len() > 80
            || !m.as_bytes()[0].is_ascii_alphanumeric()
            || !m
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_.-".contains(&b)))
    {
        return Err("add failed (ValueError)".into());
    }
    if (model.as_ref().is_some_and(|m| !m.is_empty()) || effort.is_some() || !needs.is_empty())
        && spec.is_none()
    {
        return Err("add failed (ValueError)".into());
    }
    let id = db["next_id"].as_i64().ok_or("invalid next_id")?;
    let mut item = json!({"id":id,"project":project,"title":title,"note":note,"status":"todo","jobs":[],"created":now(),"created_s":time_s(),"updated":now()});
    if let Some(percent) = progress {
        set_progress(&mut item, *percent)?;
    }
    if let Some(source) = spec {
        let spec_result = (|| -> std::io::Result<PathBuf> {
            if fs::metadata(source)?.len() > 1024 * 1024 {
                return Err(std::io::Error::other("spec exceeds 1 MiB"));
            }
            let text = fs::read_to_string(source)?;
            let text = text
                .strip_prefix('\u{feff}')
                .unwrap_or(&text)
                .replace("\r\n", "\n")
                .replace('\r', "\n");
            if text.trim_matches(python_whitespace).is_empty() {
                return Err(std::io::Error::other("spec is empty"));
            }
            let folder = paths.store.parent().unwrap().join("specs");
            fs::create_dir_all(&folder)?;
            let target = folder.join(format!("{id}-{}.md", uuid::Uuid::new_v4().simple()));
            // Unique filenames also survive a crash between copying the spec and saving the item.
            let mut file = OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(&target)?;
            file.write_all(text.as_bytes())?;
            file.flush()?;
            file.sync_all()?;
            let resolved = fs::canonicalize(target)?;
            let name = resolved.to_string_lossy();
            Ok(if let Some(unc) = name.strip_prefix("\\\\?\\UNC\\") {
                PathBuf::from(format!("\\\\{unc}"))
            } else {
                PathBuf::from(name.strip_prefix("\\\\?\\").unwrap_or(&name))
            })
        })();
        let target = spec_result.map_err(|e| match e.kind() {
            std::io::ErrorKind::Other => "add failed (ValueError)".to_owned(),
            std::io::ErrorKind::NotFound => "add failed (FileNotFoundError)".to_owned(),
            std::io::ErrorKind::PermissionDenied => "add failed (PermissionError)".to_owned(),
            std::io::ErrorKind::InvalidData => "add failed (UnicodeDecodeError)".to_owned(),
            _ => "add failed (OSError)".to_owned(),
        })?;
        item["spec"] = json!(target.to_string_lossy());
        item["dispatchable"] = json!(true);
        let mut unique = Vec::new();
        for n in needs {
            if !unique.contains(n) {
                unique.push(n.clone());
            }
        }
        item["needs"] = json!(unique);
        if let Some(m) = model.as_ref().filter(|m| !m.is_empty()) {
            item["model"] = json!(m);
        }
        if let Some(e) = effort {
            item["effort"] = json!(e);
        }
    }
    db["items"]
        .as_array_mut()
        .ok_or("invalid items")?
        .push(item);
    db["next_id"] = json!(id + 1);
    Ok(id)
}
pub fn find(db: &mut Value, id: i64) -> Result<&mut Value> {
    db["items"]
        .as_array_mut()
        .ok_or("invalid items")?
        .iter_mut()
        .find(|i| i["id"].as_i64() == Some(id))
        .ok_or_else(|| format!("no item {id}"))
}
fn python_whitespace(c: char) -> bool {
    c.is_whitespace() || matches!(c, '\u{1c}' | '\u{1d}' | '\u{1e}' | '\u{1f}')
}

/// Where a job works, shown relative to dev/ADJ so the owner can tell repositories and worktrees apart.
pub fn job_cwd(jobs: &Path, id: &str) -> String {
    let Ok(job) = load_job(&jobs.join(id).join("job.json")) else {
        return String::new();
    };
    let cwd = job["cwd"].as_str().unwrap_or("").replace('\\', "/");
    let base = "C:/Users/makhmeto/Desktop/dev/ADJ/";
    if cwd.to_lowercase().starts_with(&base.to_lowercase()) {
        cwd[base.len()..].to_owned()
    } else {
        cwd
    }
}
fn load_job(path: &Path) -> Result<Value> {
    serde_json::from_str(&fs::read_to_string(path).map_err(err)?).map_err(err)
}
pub fn normalized(path: &str) -> String {
    let p = std::path::absolute(if path.is_empty() {
        Path::new(".")
    } else {
        Path::new(path)
    })
    .unwrap_or_else(|_| PathBuf::from(path));
    let mut normalized = p.to_string_lossy().replace('/', "\\").to_lowercase();
    // ntpath.abspath strips trailing separators from directories but preserves drive roots.
    while normalized.len() > 3 && normalized.ends_with('\\') {
        normalized.pop();
    }
    normalized
}
/// Codex jobs still running whose working directory is the project or one of its worktrees (<path>-wt/...).
pub fn running_jobs(jobs: &Path, path: &str) -> Vec<Value> {
    let base = normalized(path);
    let mut out = Vec::new();
    let Ok(entries) = fs::read_dir(jobs) else {
        return out;
    };
    for entry in entries.flatten() {
        let Ok(job) = load_job(&entry.path().join("job.json")) else {
            continue;
        };
        if !matches!(
            job["status"].as_str(),
            Some("queued" | "starting" | "running")
        ) {
            continue;
        }
        let cwd = normalized(job["cwd"].as_str().unwrap_or(""));
        if cwd == base
            || cwd.starts_with(&(base.clone() + "\\"))
            || cwd.starts_with(&(base.clone() + "-wt"))
        {
            out.push(job);
        }
    }
    out
}
/// Projects with open, unblocked items and no running job. Pure apart from `running` (injected for tests).
pub fn stalled(
    db: &Value,
    _now_s: f64,
    mut running: impl FnMut(&str) -> bool,
) -> Vec<(String, Vec<Value>)> {
    let mut result = Vec::new();
    for (name, path) in db["projects"].as_object().unwrap() {
        if name == "owner" {
            continue;
        }
        let items: Vec<_> = db["items"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|i| {
                i["project"] == *name && matches!(i["status"].as_str(), Some("todo" | "doing"))
            })
            .cloned()
            .collect();
        if !items.is_empty() && !running(path.as_str().unwrap()) {
            result.push((name.clone(), items));
        }
    }
    result
}
pub trait Api {
    fn call(&mut self, method: &str, path: &str, body: &Value) -> Result<Value>;
}
/// One long-lived 「作業の停滞」 post in the task forum, reopened when archived.
pub fn stall_thread(api: &mut dyn Api, cfg: &Value, path: &Path) -> Result<String> {
    if path.exists() {
        let id = fs::read_to_string(path)
            .map_err(err)?
            .trim_matches(python_whitespace)
            .to_owned();
        api.call(
            "PATCH",
            &format!("/channels/{id}"),
            &json!({"archived":false}),
        )?;
        return Ok(id);
    }
    let thread = api.call("POST", &format!("/channels/{}/threads", cfg["task_channel_id"].as_str().ok_or("invalid task_channel_id")?), &json!({"name":"【PCツール】止まった作業の再開記録","message":{"content":"止まった作業の終了と再開を、通知を出さずに記録します。","flags":4096,"allowed_mentions":{"parse":[]}}}))?;
    let id = thread["id"]
        .as_str()
        .ok_or("invalid Discord thread response")?
        .to_owned();
    fs::write(path, &id).map_err(err)?;
    Ok(id)
}
pub fn chunks(text: &str, limit: usize) -> Vec<String> {
    let mut out = Vec::new();
    let mut current = String::new();
    let mut size = 0;
    // Python splitlines recognizes more separators than Rust lines(), and preserves their endings.
    let chars: Vec<char> = text.chars().collect();
    let mut start = 0;
    let mut end = 0;
    while end < chars.len() {
        let c = chars[end];
        end += 1;
        if matches!(
            c,
            '\n' | '\r'
                | '\u{b}'
                | '\u{c}'
                | '\u{1c}'
                | '\u{1d}'
                | '\u{1e}'
                | '\u{85}'
                | '\u{2028}'
                | '\u{2029}'
        ) || end == chars.len()
        {
            if c == '\r' && chars.get(end) == Some(&'\n') {
                end += 1;
            }
            for piece in chars[start..end].chunks(limit) {
                if size + piece.len() > limit {
                    out.push(std::mem::take(&mut current));
                    size = 0;
                }
                current.extend(piece);
                size += piece.len();
            }
            start = end;
        }
    }
    if !current.is_empty() {
        out.push(current);
    }
    out
}
pub fn notify(
    api: &mut dyn Api,
    cfg: &Value,
    paths: &Paths,
    text: &str,
    _mention: bool,
) -> Result<()> {
    // Never mention or notify the owner: a stall is for the agents and the scheduler to resolve, not him
    // (2026-10-01: 「それって俺に報告して何をして欲しいの？自己解決するならメンションしないでほしい」). `mention` is ignored.
    for part in chunks(text, 2000) {
        let id = stall_thread(api, cfg, &paths.thread)?;
        api.call(
            "POST",
            &format!("/channels/{id}/messages"),
            &json!({"content":part,"flags":4096,"allowed_mentions":{"parse":[]}}),
        )?;
    }
    Ok(())
}
pub trait Notifier {
    fn notify(&mut self, paths: &Paths, text: &str, mention: bool) -> Result<()>;
}
pub fn set_progress(item: &mut Value, percent: u8) -> Result<()> {
    if percent > 100 {
        return Err("進み具合は0〜100で記録してください".into());
    }
    if item["status"] == "done" && percent != 100 {
        return Err("完了した作業の進み具合は100です。再開してから変更してください".into());
    }
    item["progress"] = json!(percent);
    Ok(())
}
pub fn execute(db: &mut Value, cmd: &Command, paths: &Paths) -> Result<Vec<String>> {
    let supplied = match cmd {
        Command::Add { progress, .. }
        | Command::Start { progress, .. }
        | Command::Block { progress, .. } => *progress,
        Command::Progress { percent, .. } => *percent,
        _ => None,
    };
    if supplied.is_some_and(|p| p > 100) {
        return Err("進み具合は0〜100で記録してください".into());
    }
    if let Command::Progress { percent, clear, .. } = cmd
        && percent.is_some() == *clear
    {
        return Err("進み具合の数値か --clear を指定してください".into());
    }
    let mut output = Vec::new();
    match cmd {
        Command::Project { name, path } => db["projects"][name] = json!(path),
        Command::Add { .. } => output.push(format!("added {}", add_item(db, cmd, paths)?)),
        Command::List { project, all } => {
            for i in db["items"].as_array().ok_or("invalid items")? {
                if project
                    .as_ref()
                    .is_some_and(|p| !p.is_empty() && i["project"] != *p)
                    || (!all && i["status"] == "done")
                {
                    continue;
                }
                let jobs = i["jobs"].as_array().ok_or("invalid jobs")?;
                let project = i["project"].as_str().ok_or("invalid project")?;
                let where_ = if let Some(j) = jobs.last() {
                    job_cwd(&paths.jobs, j.as_str().ok_or("invalid job")?)
                } else {
                    db["projects"][project].as_str().unwrap_or("").to_owned()
                };
                let mut extra = if jobs.is_empty() {
                    String::new()
                } else {
                    format!(
                        " [{}]",
                        jobs.iter()
                            .map(|j| j.as_str().unwrap())
                            .collect::<Vec<_>>()
                            .join(", ")
                    )
                };
                if !where_.is_empty() {
                    extra.push_str(&format!(" @ {where_}"));
                }
                if let Some(percent) = i["progress"].as_u64().filter(|p| *p <= 100) {
                    extra.push_str(&format!(" — 進み具合 {percent}%"));
                }
                let note = i["note"].as_str().ok_or("invalid note")?;
                output.push(format!(
                    "{:>3} {:<5} {:<8} {}{extra}{}",
                    i["id"].as_i64().ok_or("invalid id")?,
                    i["status"].as_str().ok_or("invalid status")?,
                    project,
                    i["title"].as_str().ok_or("invalid title")?,
                    if note.is_empty() {
                        String::new()
                    } else {
                        format!(" — {note}")
                    }
                ));
            }
        }
        Command::Start { id, job, progress } => {
            let i = find(db, *id)?;
            if i["status"] == "done" && progress.is_none() {
                i.as_object_mut().ok_or("invalid item")?.remove("progress");
            }
            i["status"] = json!("doing");
            if let Some(percent) = progress {
                set_progress(i, *percent)?;
            }
            if !job.is_empty() {
                let jobs = i["jobs"].as_array_mut().ok_or("invalid jobs")?;
                if !jobs.contains(&json!(job)) {
                    jobs.push(json!(job));
                }
                cc_scheduler::common::assign_worker(i, job);
            }
            i["updated"] = json!(now());
        }
        Command::Block {
            id,
            reason,
            progress,
        } => {
            let i = find(db, *id)?;
            if i["status"] == "done" && progress.is_none() {
                i.as_object_mut().ok_or("invalid item")?.remove("progress");
            }
            i["status"] = json!("block");
            if let Some(percent) = progress {
                set_progress(i, *percent)?;
            }
            i["note"] = json!(reason);
            i["updated"] = json!(now());
        }
        Command::Done { id, note } => {
            let i = find(db, *id)?;
            i["status"] = json!("done");
            set_progress(i, 100)?;
            if !note.is_empty() {
                i["note"] = json!(note);
            }
            i["updated"] = json!(now());
        }
        Command::Progress { id, percent, .. } => {
            let item = find(db, *id)?;
            if percent.is_none() && item["status"] == "done" {
                return Err(
                    "完了した作業の進み具合は100です。再開してから未記録に戻してください".into(),
                );
            }
            if let Some(percent) = percent {
                set_progress(item, *percent)?;
            } else {
                item.as_object_mut()
                    .ok_or("invalid item")?
                    .remove("progress");
            }
            item["updated"] = json!(now());
        }
        Command::Watch => unreachable!(),
    }
    Ok(output)
}
pub fn run(cli: Cli, paths: &Paths, notifier: &mut dyn Notifier) -> Result<()> {
    if matches!(cli.command, Command::Watch) {
        return recovery::run_watch(paths, notifier, &mut recovery::RealRecovery::new(paths));
    }
    let _lock = Lock::acquire(&paths.lock, Duration::from_secs(30))?;
    let mut db = load(&paths.store)?;
    let output = execute(&mut db, &cli.command, paths)?;
    for line in output {
        println!("{line}");
    }
    // Listing is read-only; keep the shared byte lock but do not rewrite or back up state.
    if matches!(cli.command, Command::List { .. }) {
        return Ok(());
    }
    save(&paths.store, &db)
}

pub struct Discord {
    pub config: PathBuf,
}
impl Discord {
    pub fn production() -> Self {
        Self {
            config: PathBuf::from(std::env::var_os("USERPROFILE").unwrap_or_default())
                .join(".godd/discord-notify.json"),
        }
    }
}
impl Notifier for Discord {
    fn notify(&mut self, paths: &Paths, text: &str, mention: bool) -> Result<()> {
        let cfg = load_job(&self.config)?;
        notify(self, &cfg, paths, text, mention)
    }
}
impl Api for Discord {
    fn call(&mut self, method: &str, path: &str, body: &Value) -> Result<Value> {
        let token = platform::token()?;
        discord_call(&mut NativeHttp, method, path, body, &token)
    }
}
pub trait HttpTransport {
    fn request(
        &mut self,
        method: &str,
        path: &str,
        body: &[u8],
        token: &str,
    ) -> Result<(u32, Vec<u8>)>;
    fn sleep(&mut self, seconds: f64);
}
struct NativeHttp;
impl HttpTransport for NativeHttp {
    fn request(
        &mut self,
        method: &str,
        path: &str,
        body: &[u8],
        token: &str,
    ) -> Result<(u32, Vec<u8>)> {
        platform::http(method, path, body, token)
    }
    fn sleep(&mut self, seconds: f64) {
        std::thread::sleep(Duration::from_secs_f64(seconds));
    }
}
pub fn discord_call(
    transport: &mut dyn HttpTransport,
    method: &str,
    path: &str,
    body: &Value,
    token: &str,
) -> Result<Value> {
    for _ in 0..3 {
        let (status, raw) =
            transport.request(method, path, &serde_json::to_vec(body).map_err(err)?, token)?;
        let value: Value = if raw.is_empty() {
            Value::Null
        } else {
            serde_json::from_slice(&raw).map_err(|_| "invalid Discord response".to_owned())?
        };
        if status == 429 {
            transport.sleep(value["retry_after"].as_f64().unwrap_or(1.).max(0.));
            continue;
        }
        if status >= 400 {
            return Err(format!(
                "Discord API {method} {} failed: {status} {}",
                path.split('?').next().unwrap(),
                value["message"].as_str().unwrap_or("")
            ));
        }
        return Ok(value);
    }
    Err("Discord API rate limited".into())
}
mod platform;
pub mod recovery;
