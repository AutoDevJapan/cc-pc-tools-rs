//! Shared to-do list for every agent on this PC, with a stall watchdog.
//!
//! Items belong to a project (a repository path). An item is open until done. The watchdog (run every minute by
//! discord_ask.py tick) notices a project that has open, unblocked items but no running Codex job under its path,
//! and posts a notice to the owner on Discord, so work never silently stops (owner, 2026-09-30: 16:00-21:00 lost).
//!
//!   todo.exe project civ6 C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki
//!   todo.exe add civ6 "誓約を実装する" [--note "..."]
//!   todo.exe list [civ6] [--all]
//!   todo.exe start <id> [--job <codex job id>]
//!   todo.exe block <id> "オーナーの判断待ちの理由"
//!   todo.exe wait <id> --on <prerequisite id>
//!   todo.exe done <id> [--note "PR #81"]
//!   todo.exe watch

use clap::{Parser, Subcommand};
use serde_json::{Value, json};
use std::{
    fs::{self, File, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};

pub type Result<T> = std::result::Result<T, String>;
pub mod ready;
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
#[derive(clap::Args, Debug, Default)]
pub struct SubworkOptions {
    #[arg(long, requires_all=["parent_job_id", "subwork_kind"], value_parser=clap::value_parser!(i64).range(1..))]
    pub subwork_of: Option<i64>,
    #[arg(long, requires = "subwork_of")]
    pub parent_job_id: Option<String>,
    #[arg(long, requires = "subwork_of", value_parser=["research", "check", "review"])]
    pub subwork_kind: Option<String>,
}
#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(args_override_self = true, infer_long_args = true)]
    Project { name: String, path: String },
    /// Rename one project and its display text without changing paths or task state.
    RenameProject {
        from: String,
        to: String,
        #[arg(long, help = "Additional display-name alias; repeat as needed")]
        replace_name: Vec<String>,
        #[arg(
            long,
            requires = "github_to",
            help = "GitHub owner/repo whose web URLs should be updated"
        )]
        github_from: Option<String>,
        #[arg(long, requires = "github_from")]
        github_to: Option<String>,
        #[arg(long, help = "Validate and report counts without saving")]
        dry_run: bool,
    },
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
        #[command(flatten)]
        subwork: SubworkOptions,
    },
    /// Register finite independent work; only repair the explicitly recorded owner wait.
    Subwork {
        id: i64,
        #[command(flatten)]
        relation: SubworkOptions,
        #[arg(long)]
        repair_owner_wait: bool,
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
    /// Update the note or saved specification without changing task state or ownership.
    Edit {
        id: i64,
        #[arg(long, required_unless_present = "spec")]
        note_file: Option<PathBuf>,
        #[arg(long, required_unless_present = "note_file")]
        spec: Option<PathBuf>,
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
    /// Wait for every specified ToDo to finish; this is not an owner decision.
    Wait {
        id: i64,
        #[arg(long, required = true, value_parser=clap::value_parser!(i64).range(1..))]
        on: Vec<i64>,
        #[arg(long, value_parser=clap::value_parser!(u8).range(0..=100))]
        progress: Option<u8>,
    },
    /// Wait for a scheduled readiness check without repeatedly launching a worker.
    WaitReady {
        id: i64,
        #[arg(long, value_parser=["chatgpt-plus"])]
        condition: String,
        #[arg(long, value_parser=clap::value_parser!(u8).range(0..=100))]
        progress: Option<u8>,
    },
    /// Release only this recorded wait after a fresh readiness check succeeds.
    ResumeReady {
        id: i64,
        #[arg(long, value_parser=["chatgpt-plus"])]
        condition: String,
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
    /// Reconcile stale unlaunched stall reservations with accepted intake; never launch jobs.
    ReconcileIntake,
    /// Recheck this item's automatic owner wait; retain explicit prerequisites.
    ReevaluateOwnerWait { id: i64 },
    /// Read-only recovery inspection; never reserve, enqueue, launch or send messages.
    WatchPreview {
        #[arg(long, value_parser=clap::value_parser!(i64).range(1..))]
        id: Vec<i64>,
    },
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

fn subwork_relation(
    db: &Value,
    item: &Value,
    options: &SubworkOptions,
    paths: &Paths,
    repair: bool,
) -> Result<Value> {
    let mut jobs =
        cc_scheduler::common::jobs_from(&paths.jobs, true).map_err(|e| e.0.to_owned())?;
    cc_job_policy::subwork::mark_live(&mut jobs, |j| {
        ["pid", "codex_pid"].iter().any(|key| {
            j[*key].as_u64().is_some_and(|pid| {
                cc_scheduler::native::process_birth(pid as u32)
                    .is_some_and(|birth| j[format!("{key}_created")].as_u64() == Some(birth))
            })
        })
    });
    cc_job_policy::subwork::register(
        db,
        item,
        options.subwork_of.ok_or("--subwork-of is required")?,
        options
            .parent_job_id
            .as_deref()
            .ok_or("--parent-job-id is required")?,
        options
            .subwork_kind
            .as_deref()
            .ok_or("--subwork-kind is required")?,
        &jobs,
        repair,
    )
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
        subwork,
    } = args
    else {
        unreachable!()
    };
    let project = &cc_task_title::canonical_project(project);
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
    let display = |text: &str| {
        if project == cc_task_title::PRODUCT_NAME {
            cc_task_title::canonical_text(text)
        } else {
            text.into()
        }
    };
    let mut item = json!({"id":id,"project":project,"title":display(title),"note":display(note),"status":"todo","jobs":[],"created":now(),"created_s":time_s(),"updated":now()});
    if subwork.subwork_of.is_some() {
        if spec.is_none() {
            return Err("独立作業には --spec が必要です".into());
        }
        item["spec"] = json!(spec.unwrap());
        item["dispatchable"] = json!(true);
        item["subwork"] = subwork_relation(db, &item, subwork, paths, false)?;
    }
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
fn edit_text(source: &Path) -> Result<String> {
    if source.as_os_str().is_empty() || fs::metadata(source).map_err(err)?.len() > 1024 * 1024 {
        return Err("編集するファイルは1 MiB以内で指定してください".into());
    }
    let text = fs::read_to_string(source).map_err(err)?;
    let text = text
        .strip_prefix('\u{feff}')
        .unwrap_or(&text)
        .replace("\r\n", "\n")
        .replace('\r', "\n");
    if text.trim_matches(python_whitespace).is_empty() {
        return Err("編集するファイルの本文は空にできません".into());
    }
    Ok(text)
}

/// Validate every input before changing the caller's snapshot. run() holds todo.lock
/// across this operation and the atomic store replacement.
pub fn edit_item(
    db: &mut Value,
    id: i64,
    note_file: Option<&Path>,
    spec: Option<&Path>,
    paths: &Paths,
) -> Result<()> {
    if note_file.is_none() && spec.is_none() {
        return Err("--note-file か --spec を指定してください".into());
    }
    let mut item = find(db, id)?.clone();
    let previous_updated = item["updated"].clone();
    let note = note_file.map(edit_text).transpose()?;
    let spec_text = spec.map(edit_text).transpose()?;
    // Malformed histories must not be silently discarded.
    for field in ["note_history", "spec_history"] {
        if item.get(field).is_some_and(|v| !v.is_array()) {
            return Err(format!("invalid {field}"));
        }
    }
    let mut changed = false;
    if let Some(note) = note
        && item["note"] != note
    {
        if let Some(old) = item.get("note").cloned() {
            cc_scheduler::common::push(
                &mut item["note_history"],
                json!({"note":old,"updated":previous_updated}),
            );
        }
        item["note"] = json!(note);
        changed = true;
    }
    if let Some(text) = spec_text {
        // Repeating an identical recovery edit must not add another copy or history entry.
        let identical = match item["spec"].as_str().filter(|p| !p.is_empty()) {
            Some(path) => match fs::read_to_string(path) {
                Ok(old) => old == text,
                // A missing old file can be repaired; its path remains in history.
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => false,
                Err(e) => return Err(err(e)),
            },
            None => false,
        };
        if !identical {
            let folder = paths
                .store
                .parent()
                .ok_or("invalid store path")?
                .join("specs");
            fs::create_dir_all(&folder).map_err(err)?;
            let target = folder.join(format!("{id}-{}.md", uuid::Uuid::new_v4().simple()));
            let mut file = OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(&target)
                .map_err(err)?;
            file.write_all(text.as_bytes()).map_err(err)?;
            file.sync_all().map_err(err)?;
            let target = cc_scheduler::common::resolve(&target).map_err(|e| e.0.to_owned())?;
            if let Some(old) = item.get("spec").cloned() {
                cc_scheduler::common::push(
                    &mut item["spec_history"],
                    json!({"spec":old,"updated":previous_updated}),
                );
            }
            item["spec"] = json!(target.to_string_lossy());
            changed = true;
        }
    }
    if changed {
        item["updated"] = json!(now());
        *find(db, id)? = item;
    }
    Ok(())
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
                i["project"] == *name
                    && matches!(i["status"].as_str(), Some("todo" | "doing"))
                    && !cc_job_policy::todo_wait::waiting(db, i)
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
    summary::publish(api, cfg, paths, text)
}
pub trait Notifier {
    fn notify(&mut self, paths: &Paths, text: &str, mention: bool) -> Result<()>;
    fn report(&mut self, paths: &Paths, _id: i64, _source: &str, text: &str) -> Result<()> {
        self.notify(paths, text, false)
    }
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
/// Validate the complete rename before touching the caller's snapshot.
pub fn rename_project(
    db: &mut Value,
    from: &str,
    to: &str,
    extra: &[String],
    github: Option<(&str, &str)>,
) -> Result<Value> {
    if from.trim().is_empty() || to.trim().is_empty() || extra.iter().any(|s| s.trim().is_empty()) {
        return Err("製品名と置換する名前は空にできません".into());
    }
    if let Some((from, to)) = github {
        cc_task_title::replace_github_repo_urls("", from, to).map_err(err)?;
    }
    let projects = db["projects"].as_object().ok_or("invalid projects")?;
    let source = projects.get(from).cloned();
    if source.is_none() && !projects.contains_key(to) {
        return Err("改名するプロジェクトがありません".into());
    }
    if from != to && source.is_some() && projects.contains_key(to) {
        return Err("改名先のプロジェクトが既にあります".into());
    }
    if let Some(stalls) = db.get("stalls") {
        let stalls = stalls.as_object().ok_or("invalid stalls")?;
        if from != to && stalls.contains_key(from) && stalls.contains_key(to) {
            return Err("改名先の停滞記録が既にあります".into());
        }
    }
    let items = db["items"].as_array().ok_or("invalid items")?;
    for item in items
        .iter()
        .filter(|i| i["project"] == from || i["project"] == to)
    {
        for key in ["title", "note", "notes"] {
            if item.get(key).is_some_and(|v| !v.is_string()) {
                return Err(format!("invalid display field {key}"));
            }
        }
    }
    let mut aliases = vec![from.to_owned()];
    aliases.extend_from_slice(extra);
    let mut project_items = 0;
    let mut text_items = 0;
    let mut fields = 0;
    let mut github_fields = 0;
    for item in db["items"].as_array_mut().unwrap() {
        if item["project"] != from && item["project"] != to {
            continue;
        }
        if item["project"] != to {
            item["project"] = json!(to);
            project_items += 1;
        }
        let mut changed = false;
        for key in ["title", "note", "notes"] {
            if let Some(text) = item.get(key).and_then(Value::as_str) {
                let mut replacement = cc_task_title::replace_display_names(text, &aliases, to);
                if to == cc_task_title::PRODUCT_NAME {
                    replacement = cc_task_title::canonical_text(&replacement);
                }
                if let Some((github_from, github_to)) = github {
                    let urls = cc_task_title::replace_github_repo_urls(
                        &replacement,
                        github_from,
                        github_to,
                    )
                    .map_err(err)?;
                    if urls != replacement {
                        github_fields += 1;
                    }
                    replacement = urls;
                }
                if replacement != text {
                    item[key] = json!(replacement);
                    changed = true;
                    fields += 1;
                }
            }
        }
        if changed {
            text_items += 1;
        }
    }
    let renamed = from != to && source.is_some();
    if renamed {
        let projects = db["projects"].as_object_mut().unwrap();
        projects.shift_remove(from);
        projects.insert(to.into(), source.unwrap());
    }
    if from != to
        && let Some(stalls) = db.get_mut("stalls").and_then(Value::as_object_mut)
        && let Some(stall) = stalls.shift_remove(from)
    {
        stalls.insert(to.into(), stall);
    }
    Ok(
        json!({"project":to,"projects_renamed":usize::from(renamed),"items_renamed":project_items,"text_items_changed":text_items,"fields_changed":fields,"github_fields_changed":github_fields}),
    )
}

pub fn execute(db: &mut Value, cmd: &Command, paths: &Paths) -> Result<Vec<String>> {
    let supplied = match cmd {
        Command::Add { progress, .. }
        | Command::Start { progress, .. }
        | Command::Block { progress, .. } => *progress,
        Command::Wait { progress, .. } | Command::WaitReady { progress, .. } => *progress,
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
        Command::Project { name, path } => {
            db["projects"][cc_task_title::canonical_project(name)] = json!(path)
        }
        Command::RenameProject {
            from,
            to,
            replace_name,
            github_from,
            github_to,
            ..
        } => {
            output.push(
                rename_project(
                    db,
                    from,
                    to,
                    replace_name,
                    github_from.as_deref().zip(github_to.as_deref()),
                )?
                .to_string(),
            );
        }
        Command::Add { .. } => output.push(format!("added {}", add_item(db, cmd, paths)?)),
        Command::Subwork {
            id,
            relation,
            repair_owner_wait,
        } => {
            let current = db["items"]
                .as_array()
                .and_then(|items| items.iter().find(|i| i["id"] == *id))
                .ok_or_else(|| format!("no item {id}"))?;
            let registered = subwork_relation(db, current, relation, paths, *repair_owner_wait)?;
            let row = find(db, *id)?;
            row["subwork"] = registered;
            if *repair_owner_wait {
                let parent = row["subwork"]["parent_todo_id"].clone();
                row["wait_on"] = json!(
                    row["wait_on"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .filter(|id| **id != parent)
                        .cloned()
                        .collect::<Vec<_>>()
                );
                row.as_object_mut().unwrap().remove("work_owner_job");
                if row["wait_on"].as_array().is_some_and(Vec::is_empty) {
                    row.as_object_mut().unwrap().remove("wait_on");
                    if row["status"] == "wait" {
                        row["status"] = json!("todo");
                        row["dependency_ready"] = json!(true);
                        if let Some(scheduler) = row["scheduler"].as_object_mut() {
                            scheduler.insert("not_before".into(), json!(0));
                        }
                    }
                }
            }
            row["updated"] = json!(now());
            output.push(format!("registered subwork {id}"));
        }
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
                for id in cc_job_policy::todo_wait::pending_on(db, i) {
                    extra.push_str(&format!(" — ToDo {id} 待ち"));
                }
                if i["wait_ready"]["condition"] == "chatgpt-plus" {
                    extra.push_str(" — ChatGPT Plusの復旧確認待ち");
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
            let item = db["items"]
                .as_array()
                .and_then(|items| items.iter().find(|i| i["id"] == *id))
                .ok_or_else(|| format!("no item {id}"))?;
            if cc_job_policy::todo_wait::waiting(db, item) {
                return Err("待っているToDoが未完了です。完了すると自動で再開します".into());
            }
            if !item["wait_ready"].is_null() && !ready::confirmed_production(&item["wait_ready"])? {
                return Err("再開条件をまだ確認できていません。定期確認後に再開します".into());
            }
            cc_job_policy::todo_wait::release_ready(db);
            let i = find(db, *id)?;
            i.as_object_mut()
                .ok_or("invalid item")?
                .remove("wait_ready");
            if i["status"] == "done" && progress.is_none() {
                i.as_object_mut().ok_or("invalid item")?.remove("progress");
            }
            if job.is_empty() {
                cc_scheduler::common::resume_without_worker(i);
            } else {
                cc_scheduler::common::assign_worker(i, job);
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
            }
            i["updated"] = json!(now());
        }
        Command::Edit {
            id,
            note_file,
            spec,
        } => edit_item(db, *id, note_file.as_deref(), spec.as_deref(), paths)?,
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
            i.as_object_mut().ok_or("invalid item")?.remove("wait_on");
            i.as_object_mut()
                .ok_or("invalid item")?
                .remove("wait_ready");
            cc_job_policy::todo_wait::release_ready(db);
        }
        Command::WaitReady {
            id,
            condition,
            progress,
        } => {
            ready::register(db, *id, condition)?;
            if let Some(percent) = progress {
                set_progress(find(db, *id)?, *percent)?;
            }
        }
        Command::ResumeReady { id, condition } => {
            let resumed = ready::resume_production(db, *id, condition)?;
            output.push(json!({"id":id,"resumed":resumed}).to_string());
        }
        Command::Wait { id, on, progress } => {
            cc_job_policy::todo_wait::validate(db, *id, on)?;
            let i = find(db, *id)?;
            // An explicit prerequisite replaces automatic owner provenance.
            i.as_object_mut().unwrap().remove("work_owner_job");
            i.as_object_mut().unwrap().remove("work_owner_wait_on");
            let on: std::collections::BTreeSet<_> = on.iter().copied().collect();
            i["wait_on"] = json!(on);
            if i["status"] != "block" {
                i["status"] = json!("wait");
            }
            i.as_object_mut()
                .ok_or("invalid item")?
                .remove("dependency_ready");
            if let Some(percent) = progress {
                set_progress(i, *percent)?;
            }
            if let Some(recovery) = i["stall_recovery"].as_object_mut() {
                recovery.remove("pending");
                recovery.remove("report");
            }
            i["updated"] = json!(now());
            cc_job_policy::todo_wait::release_ready(db);
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
        Command::Watch
        | Command::ReconcileIntake
        | Command::WatchPreview { .. }
        | Command::ReevaluateOwnerWait { .. } => unreachable!(),
    }
    Ok(output)
}
pub fn run(cli: Cli, paths: &Paths, notifier: &mut dyn Notifier) -> Result<()> {
    if let Command::ReevaluateOwnerWait { id } = &cli.command {
        let changed = recovery::reevaluate_owner_wait_files(paths, *id)?;
        println!("{}", json!({"reevaluated":id,"released":changed}));
        return Ok(());
    }
    if let Command::WatchPreview { id } = &cli.command {
        let report = recovery::preview_files(paths, id)?;
        println!("{report}");
        return Ok(());
    }
    if matches!(cli.command, Command::ReconcileIntake) {
        use recovery::RecoveryBackend;
        let _watch = Lock::acquire(&paths.store.with_file_name("watch.lock"), Duration::ZERO)?;
        let result = recovery::RealRecovery::new(paths).reconcile_intake(paths)?;
        println!(
            "{}",
            json!({"accepted":result.accepted,"migrated":result.migrated,"deferred":result.deferred})
        );
        return Ok(());
    }
    if matches!(cli.command, Command::Watch) {
        return recovery::run_watch(paths, notifier, &mut recovery::RealRecovery::new(paths));
    }
    let _lock = Lock::acquire(&paths.lock, Duration::from_secs(30))?;
    let mut db = load(&paths.store)?;
    let before_rename =
        matches!(cli.command, Command::RenameProject { dry_run: false, .. }).then(|| db.clone());
    let output = execute(&mut db, &cli.command, paths)?;
    // Listing is read-only; keep the shared byte lock but do not rewrite or back up state.
    if matches!(
        cli.command,
        Command::List { .. } | Command::RenameProject { dry_run: true, .. }
    ) {
        for line in output {
            println!("{line}");
        }
        return Ok(());
    }
    if before_rename.as_ref().is_some_and(|before| *before != db) {
        let backup = paths
            .store
            .with_file_name(format!("todo.before-rename-{}.json", uuid::Uuid::new_v4()));
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&backup)
            .map_err(err)?;
        file.write_all(&fs::read(&paths.store).map_err(err)?)
            .map_err(err)?;
        file.sync_all().map_err(err)?;
    }
    save(&paths.store, &db)?;
    for line in output {
        println!("{line}");
    }
    drop(_lock);
    // Notify the ordinary watchdog immediately after committing a completed
    // prerequisite. The production path uses the same existing admission gates.
    // Isolated CLI stores never launch the PC's production workers.
    if matches!(
        cli.command,
        Command::Done { .. } | Command::Wait { .. } | Command::ResumeReady { .. }
    ) && paths.store == Paths::production().store
        && db["items"]
            .as_array()
            .is_some_and(|items| items.iter().any(|i| i["dependency_ready"] == true))
    {
        let mut wake = std::process::Command::new(std::env::current_exe().map_err(err)?);
        wake.arg("watch")
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null());
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            wake.creation_flags(0x08000000);
        }
        if let Err(error) = wake.spawn() {
            eprintln!("完了待ちの解除を保存しました。見張りは次の定期実行で再開します: {error}");
        }
    }
    Ok(())
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
    fn report(&mut self, paths: &Paths, id: i64, source: &str, text: &str) -> Result<()> {
        let cfg = load_job(&self.config)?;
        summary::publish_report(self, &cfg, paths, &format!("{id}:{source}"), text)
    }
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
        let payload = if method == "GET" || body.is_null() {
            Vec::new()
        } else {
            serde_json::to_vec(body).map_err(err)?
        };
        let (status, raw) = transport.request(method, path, &payload, token)?;
        let value: Value = if raw.is_empty() {
            Value::Null
        } else {
            serde_json::from_slice(&raw).map_err(|_| {
                format!(
                    "invalid Discord response: {method} status={status} bytes={}",
                    raw.len()
                )
            })?
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
pub mod summary;
