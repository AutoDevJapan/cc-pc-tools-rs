use serde_json::{json, Value};
use std::os::windows::{io::AsRawHandle, process::CommandExt};
use std::{
    fs::{self, File, OpenOptions},
    io::{Read, Write},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    thread,
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};
use windows_sys::Win32::{
    Storage::FileSystem::{
        LockFileEx, MoveFileExW, UnlockFileEx, LOCKFILE_EXCLUSIVE_LOCK, LOCKFILE_FAIL_IMMEDIATELY,
        MOVEFILE_REPLACE_EXISTING,
    },
    System::IO::OVERLAPPED,
};

#[derive(Clone, Debug, PartialEq)]
pub struct Error(pub &'static str);
pub type Result<T> = std::result::Result<T, Error>;

/// A caller explicitly assigning a new worker supersedes the old worker's result.
/// Keep the saved specification, worktree, answers and job history intact.
pub fn assign_worker(item: &mut Value, job: &str) {
    let same_receipt = item["worker_receipt"]["job"] == job
        && item["worker_receipt"]["generation"] == item["resume_generation"]
        && item["scheduler"]["job"] == job
        && item["status"] == "doing";
    if !same_receipt {
        advance_resume(item, Some(job));
    }
    // A worker now owns the released item, so the one-shot "prerequisite finished" marker ends.
    if let Some(fields) = item.as_object_mut() {
        fields.remove("dependency_ready");
    }
    let old_job = item["scheduler"]["job"].clone();
    if truth(&old_job) && (old_job != job || item["scheduler"]["phase"] == "finished") {
        record_result(item, &old_job, "superseded");
    }
    if !item["scheduler"].is_object() {
        item["scheduler"] = json!({});
    }
    if item["scheduler"]["job"] == job {
        return;
    }
    let m = &mut item["scheduler"];
    if m["job"].is_string() {
        m["last_job"] = m["job"].clone();
    }
    m["job"] = json!(job);
    m["key"] = Value::Null;
    m["phase"] = json!("running");
    for field in [
        "pending_cancel",
        "notice_pending",
        "selection",
        "not_before",
    ] {
        m.as_object_mut().unwrap().remove(field);
    }
}

/// A durable per-ToDo ledger: the same job result cannot be applied in a later generation.
pub fn result_received(item: &Value, job: &Value) -> bool {
    !s(job).is_empty() && item["processed_results"].get(s(job)).is_some()
}

pub fn record_result(item: &mut Value, job: &Value, reason: &str) {
    if s(job).is_empty() || result_received(item, job) {
        return;
    }
    if !item["processed_results"].is_object() {
        item["processed_results"] = json!({});
    }
    item["processed_results"][s(job)] = json!({
        "generation": item["resume_generation"].as_u64().unwrap_or(0),
        "reason": reason,
    });
}

fn advance_resume(item: &mut Value, job: Option<&str>) {
    item["resume_generation"] = add_count(&item["resume_generation"], 1);
    item["worker_receipt"] = json!({"job":job,"generation":item["resume_generation"]});
}

/// Start without --job still invalidates a result being read by the scheduler.
/// Retain live ownership, but retire an already finished legacy job.
pub fn resume_without_worker(item: &mut Value) {
    advance_resume(item, None);
    let job = item["scheduler"]["job"].clone();
    if item["scheduler"]["phase"] == "finished" || result_received(item, &job) {
        record_result(item, &job, "superseded");
        merge(
            &mut item["scheduler"],
            json!({
                "last_job":job,"job":null,"key":null,"phase":"resumed","resume":true,"not_before":0
            }),
        );
        remove(&mut item["scheduler"], "pending_cancel");
        remove(&mut item["scheduler"], "notice_pending");
    }
}
impl From<std::io::Error> for Error {
    fn from(e: std::io::Error) -> Self {
        Self(match e.kind() {
            std::io::ErrorKind::NotFound => "FileNotFoundError",
            std::io::ErrorKind::PermissionDenied => "PermissionError",
            std::io::ErrorKind::InvalidData => "UnicodeDecodeError",
            _ => "OSError",
        })
    }
}
impl From<serde_json::Error> for Error {
    fn from(_: serde_json::Error) -> Self {
        Self("JSONDecodeError")
    }
}
pub fn now() -> f64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs_f64()
}
pub fn iso(t: f64) -> String {
    use chrono::{Local, TimeZone};
    Local
        .timestamp_opt(t.floor() as i64, 0)
        .single()
        .unwrap()
        .to_rfc3339_opts(chrono::SecondsFormat::Secs, false)
}
pub fn epoch(v: &Value) -> f64 {
    if let Some(n) = v.as_f64() {
        return n;
    }
    let Some(s) = v.as_str() else { return 0. };
    if let Ok(t) = chrono::DateTime::parse_from_rfc3339(s) {
        return t.timestamp() as f64 + t.timestamp_subsec_nanos() as f64 / 1e9;
    }
    use chrono::TimeZone;
    for fmt in ["%Y-%m-%dT%H:%M:%S%.f", "%Y-%m-%d %H:%M:%S%.f", "%Y-%m-%d"] {
        if let Ok(t) = chrono::NaiveDateTime::parse_from_str(s, fmt) {
            if let Some(t) = chrono::Local.from_local_datetime(&t).single() {
                return t.timestamp() as f64 + t.timestamp_subsec_nanos() as f64 / 1e9;
            }
        }
    }
    0.
}
pub fn s(v: &Value) -> &str {
    v.as_str().unwrap_or("")
}
pub fn n(v: &Value) -> f64 {
    v.as_f64().unwrap_or(0.)
}
pub fn pystr(v: &Value) -> String {
    match v {
        Value::String(x) => x.clone(),
        Value::Bool(true) => "True".into(),
        Value::Bool(false) => "False".into(),
        Value::Null => "None".into(),
        _ => v.to_string(),
    }
}
pub fn add_count(v: &Value, increment: i64) -> Value {
    if v.is_null() {
        return json!(increment);
    }
    if let Some(i) = v.as_i64() {
        return json!(i + increment);
    }
    if let Some(i) = v.as_u64() {
        return json!(i + increment as u64);
    }
    json!(n(v) + increment as f64)
}
pub fn truth(v: &Value) -> bool {
    match v {
        Value::Null => false,
        Value::Bool(b) => *b,
        Value::Number(_) => n(v) != 0.,
        Value::String(x) => !x.is_empty(),
        Value::Array(x) => !x.is_empty(),
        Value::Object(x) => !x.is_empty(),
    }
}
pub fn arr(v: &Value) -> Vec<Value> {
    v.as_array().cloned().unwrap_or_default()
}
pub fn merge(v: &mut Value, data: Value) {
    if !v.is_object() {
        *v = json!({});
    }
    for (k, val) in data.as_object().unwrap() {
        v[k] = val.clone();
    }
}
pub fn remove(v: &mut Value, k: &str) {
    if let Some(o) = v.as_object_mut() {
        o.remove(k);
    }
}
pub fn push(v: &mut Value, x: Value) {
    if !v.is_array() {
        *v = json!([]);
    }
    v.as_array_mut().unwrap().push(x);
}
pub fn read_json(path: &Path, default: Value) -> Result<Value> {
    match fs::read_to_string(path) {
        Ok(t) => Ok(serde_json::from_str(&t)?),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(default),
        Err(e) => Err(e.into()),
    }
}
pub fn encode(v: &Value, indent: usize) -> String {
    use serde::Serialize;
    let mut buf = Vec::new();
    let pad = vec![b' '; indent];
    let mut se = serde_json::Serializer::with_formatter(
        &mut buf,
        serde_json::ser::PrettyFormatter::with_indent(&pad),
    );
    v.serialize(&mut se).unwrap();
    String::from_utf8(buf).unwrap()
}
pub fn backup(path: &Path) -> Result<()> {
    let target = PathBuf::from(format!("{}.bak-20260930d", path.display()));
    if path.exists() && !target.exists() {
        match OpenOptions::new().write(true).create_new(true).open(target) {
            Ok(mut f) => {
                f.write_all(&fs::read(path)?)?;
            }
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {}
            Err(e) => return Err(e.into()),
        }
    }
    Ok(())
}
fn wide(path: &Path) -> Vec<u16> {
    use std::os::windows::ffi::OsStrExt;
    path.as_os_str().encode_wide().chain(Some(0)).collect()
}
pub fn write_json(path: &Path, v: &Value, indent: usize) -> Result<()> {
    backup(path)?;
    let temp = path.with_file_name(format!(
        "{}.{}.tmp",
        path.file_name().unwrap().to_string_lossy(),
        std::process::id()
    ));
    {
        let mut f = File::create(&temp)?;
        f.write_all(encode(v, indent).as_bytes())?;
        f.sync_all()?;
    }
    for i in 0..50 {
        if unsafe {
            MoveFileExW(
                wide(&temp).as_ptr(),
                wide(path).as_ptr(),
                MOVEFILE_REPLACE_EXISTING,
            )
        } != 0
        {
            return Ok(());
        }
        let e = std::io::Error::last_os_error();
        if e.kind() != std::io::ErrorKind::PermissionDenied || i == 49 {
            return Err(e.into());
        }
        thread::sleep(Duration::from_millis(20));
    }
    Err(Error("PermissionError"))
}

/// Nonblocking OS lock. A second tick returns immediately; reboot releases it.
/// OS byte lock: released immediately on a crash; never steal a live lock by age.
pub struct ByteLock {
    f: File,
}
impl ByteLock {
    pub fn acquire(path: &Path, timeout: Duration) -> Result<Option<Self>> {
        let f = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(path)?;
        let deadline = Instant::now() + timeout;
        loop {
            let mut ov: OVERLAPPED = unsafe { std::mem::zeroed() };
            if unsafe {
                LockFileEx(
                    f.as_raw_handle(),
                    LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY,
                    0,
                    1,
                    0,
                    &mut ov,
                )
            } != 0
            {
                return Ok(Some(Self { f }));
            }
            let e = std::io::Error::last_os_error();
            if e.raw_os_error() != Some(33) && e.raw_os_error() != Some(32) {
                return Err(e.into());
            }
            if Instant::now() >= deadline {
                return Ok(None);
            }
            thread::sleep(Duration::from_millis(100));
        }
    }
}
impl Drop for ByteLock {
    fn drop(&mut self) {
        let mut ov: OVERLAPPED = unsafe { std::mem::zeroed() };
        unsafe {
            UnlockFileEx(self.f.as_raw_handle(), 0, 1, 0, &mut ov);
        }
    }
}
pub const NO_WINDOW: u32 = 0x08000000;
#[derive(Clone, Debug)]
pub struct Invocation {
    pub program: String,
    pub args: Vec<String>,
    pub cwd: Option<String>,
    pub timeout: Option<Duration>,
    pub git_prompt_off: bool,
}
#[derive(Clone, Debug)]
pub struct Output {
    pub code: i32,
    pub stdout: String,
    pub stderr: String,
}
pub trait Runner {
    fn run(&mut self, command: &Invocation) -> Result<Output>;
}
pub struct SystemRunner;
impl Runner for SystemRunner {
    fn run(&mut self, c: &Invocation) -> Result<Output> {
        let mut cmd = Command::new(&c.program);
        cmd.args(&c.args)
            .creation_flags(NO_WINDOW)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if let Some(p) = &c.cwd {
            cmd.current_dir(p);
        }
        if c.git_prompt_off {
            cmd.env("GIT_TERMINAL_PROMPT", "0");
        }
        let mut child = cmd.spawn()?;
        let mut stdout = child.stdout.take().unwrap();
        let mut stderr = child.stderr.take().unwrap();
        let out = thread::spawn(move || {
            let mut buf = Vec::new();
            stdout.read_to_end(&mut buf).map(|_| buf)
        });
        let err = thread::spawn(move || {
            let mut buf = Vec::new();
            stderr.read_to_end(&mut buf).map(|_| buf)
        });
        let start = Instant::now();
        let status = loop {
            if let Some(status) = child.try_wait()? {
                break status;
            }
            if c.timeout.is_some_and(|t| start.elapsed() >= t) {
                let _ = child.kill();
                let _ = child.wait();
                return Err(Error("TimeoutExpired"));
            }
            thread::sleep(Duration::from_millis(10));
        };
        let bytes = out.join().map_err(|_| Error("RuntimeError"))??;
        let stderr = err.join().map_err(|_| Error("RuntimeError"))??;
        Ok(Output {
            code: status.code().unwrap_or(1),
            stdout: String::from_utf8_lossy(&bytes).replace("\r\n", "\n"),
            stderr: String::from_utf8_lossy(&stderr).replace("\r\n", "\n"),
        })
    }
}
pub fn invocation(args: &[&str], timeout: Option<u64>) -> Invocation {
    Invocation {
        program: args[0].into(),
        args: args[1..].iter().map(|x| x.to_string()).collect(),
        cwd: None,
        timeout: timeout.map(Duration::from_secs),
        git_prompt_off: false,
    }
}
pub fn tool_command(path: &Path, args: &[&str], timeout: Option<u64>) -> Invocation {
    let exe = path.with_extension("exe");
    let mut c = if exe.exists() {
        invocation(&[exe.to_str().unwrap()], timeout)
    } else {
        let python = "C:/Users/makhmeto/AppData/Local/Programs/Python/Python312/python.exe";
        invocation(&[python, path.to_str().unwrap()], timeout)
    };
    c.args.extend(args.iter().map(|x| x.to_string()));
    c
}
pub fn jobs_from(root: &Path, validate: bool) -> Result<Vec<Value>> {
    let mut out = vec![];
    if !root.exists() {
        return Ok(out);
    }
    for dir in fs::read_dir(root)? {
        let path = dir?.path().join("job.json");
        if let Ok(v) = read_json(&path, Value::Null) {
            if v.is_object() && (!validate || (v.get("id").is_some() && v.get("status").is_some()))
            {
                out.push(v);
            }
        }
    }
    if validate {
        out.sort_by(|a, b| s(&a["created_at"]).cmp(s(&b["created_at"])));
    }
    Ok(out)
}
pub fn absolute(path: &Path) -> Result<PathBuf> {
    let path = std::path::absolute(path)?;
    let mut clean = PathBuf::new();
    for c in path.components() {
        use std::path::Component;
        match c {
            Component::ParentDir => {
                clean.pop();
            }
            Component::CurDir => {}
            _ => clean.push(c),
        }
    }
    Ok(clean)
}
pub fn norm(path: &str) -> String {
    absolute(Path::new(if path.is_empty() { "." } else { path }))
        .unwrap()
        .to_string_lossy()
        .replace('/', "\\")
        .to_lowercase()
}
/// Python Path.resolve(strict=False): resolve existing junctions, then append missing descendants.
pub fn resolve(path: &Path) -> Result<PathBuf> {
    let original = absolute(path)?;
    let mut ancestor = original.as_path();
    let mut missing = Vec::new();
    while !ancestor.exists() {
        let Some(name) = ancestor.file_name() else {
            return Ok(original);
        };
        missing.push(name.to_owned());
        ancestor = ancestor.parent().ok_or(Error("OSError"))?;
    }
    let resolved = fs::canonicalize(ancestor)?;
    let text = resolved.to_string_lossy();
    let mut resolved = if let Some(unc) = text.strip_prefix(r"\\?\UNC\") {
        PathBuf::from(format!(r"\\{}", unc))
    } else if let Some(path) = text.strip_prefix(r"\\?\") {
        PathBuf::from(path)
    } else {
        resolved
    };
    for name in missing.into_iter().rev() {
        resolved.push(name);
    }
    Ok(resolved)
}
