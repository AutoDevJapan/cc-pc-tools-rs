use crate::*;
use chrono::{DateTime, Local, SecondsFormat};
use std::{
    collections::BTreeMap,
    fs::{self, File, OpenOptions},
    io::{self, Read, Write},
    path::{Path, PathBuf},
    process::{Child, Command, Stdio},
    sync::Arc,
    time::{Duration, Instant},
};

pub fn now_iso() -> String {
    Local::now().to_rfc3339_opts(SecondsFormat::Secs, true)
}
pub fn parse_iso(t: &str) -> Option<f64> {
    DateTime::parse_from_rfc3339(t)
        .map(|d| d.timestamp_millis() as f64 / 1000.)
        .ok()
        .or_else(|| {
            chrono::NaiveDateTime::parse_from_str(t, "%Y-%m-%dT%H:%M:%S%.f")
                .ok()
                .and_then(|n| {
                    use chrono::TimeZone;
                    Local
                        .from_local_datetime(&n)
                        .single()
                        .map(|d| d.timestamp_millis() as f64 / 1000.)
                })
        })
}
pub fn normalize_uuid(text: &str) -> Option<String> {
    let t = text
        .strip_prefix("urn:uuid:")
        .unwrap_or(text)
        .trim_matches(['{', '}'])
        .replace('-', "");
    if t.len() != 32 || !t.bytes().all(|c| c.is_ascii_hexdigit()) {
        return None;
    }
    let t = t.to_lowercase();
    Some(format!(
        "{}-{}-{}-{}-{}",
        &t[..8],
        &t[8..12],
        &t[12..16],
        &t[16..20],
        &t[20..]
    ))
}

#[derive(Clone, Debug)]
pub struct Call {
    pub cwd: Option<PathBuf>,
    pub argv: Vec<String>,
    pub input: Vec<u8>,
    pub timeout: f64,
    pub flags: u32,
}
impl Call {
    pub fn new(argv: Vec<String>, input: Vec<u8>, timeout: f64) -> Self {
        Self {
            cwd: None,
            argv,
            input,
            timeout,
            flags: NO_WINDOW,
        }
    }
}
#[derive(Clone, Debug)]
pub struct Reply {
    pub code: i64,
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
}
pub fn notifier_overrides(
    argv: &[String],
    inherited: &BTreeMap<String, String>,
    saved: &BTreeMap<String, String>,
) -> BTreeMap<String, String> {
    let notifier = argv
        .first()
        .and_then(|a| Path::new(a).file_name())
        .is_some_and(|name| name.to_string_lossy().eq_ignore_ascii_case("godd-ao.exe"))
        && argv.get(1).is_some_and(|a| a == "notify")
        && argv.get(2).is_some_and(|a| a == "task");
    if !notifier {
        return BTreeMap::new();
    }
    [
        "GODD_DISCORD_BOT_TOKEN",
        "GODD_DISCORD_TASK_CHANNEL_ID",
        "GODD_DISCORD_OWNER_ID",
    ]
    .into_iter()
    .filter(|name| {
        !inherited
            .iter()
            .any(|(key, value)| key.eq_ignore_ascii_case(name) && !value.is_empty())
    })
    .filter_map(|name| {
        saved
            .get(name)
            .filter(|value| !value.is_empty())
            .map(|value| (name.to_string(), value.clone()))
    })
    .collect()
}
#[cfg(windows)]
fn saved_notifier_environment() -> BTreeMap<String, String> {
    use winreg::{RegKey, enums::HKEY_CURRENT_USER};
    let mut saved = BTreeMap::new();
    if let Ok(key) = RegKey::predef(HKEY_CURRENT_USER).open_subkey("Environment") {
        for name in [
            "GODD_DISCORD_BOT_TOKEN",
            "GODD_DISCORD_TASK_CHANNEL_ID",
            "GODD_DISCORD_OWNER_ID",
        ] {
            if let Ok(value) = key.get_value::<String, _>(name) {
                saved.insert(name.into(), value);
            }
        }
    }
    if let Some(home) = std::env::var_os("USERPROFILE")
        && let Ok(bytes) = fs::read(Path::new(&home).join(".godd/discord-notify.json"))
        && let Ok(cfg) = serde_json::from_slice::<Value>(&bytes)
    {
        merge_notifier_config(&mut saved, &cfg);
    }
    saved
}
#[cfg(windows)]
fn merge_notifier_config(saved: &mut BTreeMap<String, String>, cfg: &Value) {
    for (name, field) in [
        ("GODD_DISCORD_TASK_CHANNEL_ID", "task_channel_id"),
        ("GODD_DISCORD_OWNER_ID", "owner_user_id"),
    ] {
        if let Some(value) = cfg[field].as_str().filter(|value| !value.is_empty())
            && saved.get(name).is_none_or(String::is_empty)
        {
            saved.insert(name.into(), value.into());
        }
    }
}
#[cfg(all(test, windows))]
mod notifier_config_tests {
    use super::*;
    #[test]
    fn empty_registry_settings_use_config_without_copying_extra_credentials() {
        let mut saved = BTreeMap::from([
            ("GODD_DISCORD_TASK_CHANNEL_ID".into(), String::new()),
            ("GODD_DISCORD_OWNER_ID".into(), String::new()),
        ]);
        merge_notifier_config(
            &mut saved,
            &json!({"task_channel_id":"forum","owner_user_id":"owner","OPENAI_API_KEY":"excluded"}),
        );
        assert_eq!(
            saved,
            BTreeMap::from([
                ("GODD_DISCORD_TASK_CHANNEL_ID".into(), "forum".into()),
                ("GODD_DISCORD_OWNER_ID".into(), "owner".into())
            ])
        );
        merge_notifier_config(
            &mut saved,
            &json!({"task_channel_id":"other","owner_user_id":"other"}),
        );
        assert_eq!(saved["GODD_DISCORD_TASK_CHANNEL_ID"], "forum");
    }
}
/// All network-facing programs (OpenCodex, Docker/Jev and GTO) and process actions
/// cross this boundary. Tests substitute fakes and never call Discord.
pub trait Runtime: Send + Sync {
    /// None on unsupported platforms and fixture runtimes.
    fn boot_epoch(&self) -> Option<f64> {
        None
    }
    fn recovery_protected(&self, _cwd: &str) -> io::Result<bool> {
        Ok(false)
    }
    /// Fixture runtimes use virtual checkout metadata; production reads local Git files.
    fn work_checkout(&self, cwd: &str) -> Value {
        cc_job_policy::work_owner::checkout(cwd)
    }
    fn queue_machine_sample(
        &self,
        cfg: &crate::adaptive::AdaptiveConfig,
    ) -> crate::adaptive::MachineSample {
        crate::adaptive::machine_sample(cfg)
    }
    fn queue_quota_sample(
        &self,
        provider: &str,
        at: f64,
        timeout: u64,
    ) -> crate::adaptive::QuotaSnapshot {
        let unavailable = || crate::adaptive::QuotaSnapshot {
            at,
            ..Default::default()
        };
        let env = self.env();
        let Some(appdata) = env.get("APPDATA") else {
            return unavailable();
        };
        let package = Path::new(appdata).join("npm/node_modules/@bitkyc08/opencodex");
        // Execute the bundled runtime directly; a timed-out sample has no shell
        // grandchild left behind. This command refreshes OAuth quotas only.
        let argv = vec![
            package
                .join("node_modules/bun/bin/bun.exe")
                .to_string_lossy()
                .into_owned(),
            package.join("bin/ocx.mjs").to_string_lossy().into_owned(),
            "account".into(),
            if provider == "anthropic" {
                "list"
            } else {
                "refresh"
            }
            .into(),
            provider.into(),
            "--json".into(),
        ];
        let mut argv = argv;
        if provider == "anthropic" {
            argv.extend(["--quota".into(), "--refresh".into()]);
        }
        let reply = self.call(Call::new(argv, vec![], timeout as f64));
        if provider == "openai" {
            let cli = reply
                .as_ref()
                .ok()
                .filter(|r| r.code == 0)
                .and_then(|r| serde_json::from_slice::<Value>(&r.stdout).ok());
            let cache = env
                .get("USERPROFILE")
                .and_then(|home| {
                    std::fs::read(Path::new(home).join(".opencodex/codex-quota-cache.json")).ok()
                })
                .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok())
                .unwrap_or(Value::Null);
            return crate::adaptive::parse_codex_quotas(cli.as_ref(), &cache, at);
        }
        match reply {
            Ok(reply) if reply.code == 0 => serde_json::from_slice::<Value>(&reply.stdout)
                .map(|v| crate::adaptive::parse_quotas(&v, at))
                .unwrap_or_else(|_| unavailable()),
            _ => unavailable(),
        }
    }
    /// Production uses the installed redaction data. Test runtimes provide
    /// their own fixture root without modifying the owner's installed files.
    fn task_title_tools(&self) -> PathBuf {
        PathBuf::from(cc_task_title::TOOLS)
    }
    fn task_job_title(&self, job: &Value, prompt: &str) -> io::Result<String> {
        cc_task_title::installed_job_title(job, prompt, &self.task_title_tools())
    }
    /// Query public server metadata with plugins/apps disabled. Never retain
    /// transport environments or print the CLI's response to diagnostics.
    fn summary_mcp_servers(&self, codex: &str, cwd: &Path) -> io::Result<Vec<String>> {
        let mut call = Call::new(
            vec![
                "cmd.exe".into(),
                "/d".into(),
                "/c".into(),
                codex.into(),
                "mcp".into(),
                "list".into(),
                "--json".into(),
                "-c".into(),
                "features.plugins=false".into(),
                "-c".into(),
                "features.apps=false".into(),
            ],
            vec![],
            20.,
        );
        call.cwd = Some(cwd.into());
        let reply = self.call(call)?;
        if reply.code != 0 {
            return Err(io::Error::other("summary MCP inventory unavailable"));
        }
        let entries: Vec<Value> = serde_json::from_slice(&reply.stdout)
            .map_err(|_| io::Error::other("invalid summary MCP inventory"))?;
        entries
            .iter()
            .map(|entry| {
                let name = entry["name"]
                    .as_str()
                    .ok_or_else(|| io::Error::other("missing MCP name"))?;
                if name.is_empty()
                    || name.len() > 120
                    || !name
                        .chars()
                        .all(|c| c.is_ascii_alphanumeric() || "_-".contains(c))
                {
                    return Err(io::Error::other("invalid MCP name"));
                }
                Ok(name.to_string())
            })
            .collect()
    }
    /// Freeze only idle Codex controllers. None means a tool/worker is still live.
    fn pause_idle_agent(
        &self,
        pid: u32,
        created: u64,
        baseline: Option<u64>,
        marker: Option<&Path>,
    ) -> io::Result<Option<Box<dyn Paused>>> {
        crate::pause::idle_agent(pid, created, baseline, marker)
    }
    fn recover_paused(&self, path: &Path) -> io::Result<()> {
        crate::pause::recover(path)
    }
    fn pause_supervisor(&self, pid: u32, created: u64) -> io::Result<Box<dyn Paused>> {
        crate::pause::supervisor(pid, created)
    }
    fn env(&self) -> BTreeMap<String, String> {
        std::env::vars().collect()
    }
    fn diagnostic(&self, message: &str) {
        let _ = writeln!(TextWriter(std::io::stderr().lock()), "{message}");
    }
    fn now(&self) -> String {
        now_iso()
    }
    fn epoch(&self) -> f64 {
        parse_iso(&self.now()).unwrap_or(0.)
    }
    fn mono(&self) -> f64;
    fn sleep(&self, s: f64) {
        std::thread::sleep(Duration::from_secs_f64(s.max(0.)));
    }
    fn pid(&self) -> u32 {
        std::process::id()
    }
    fn cores(&self) -> usize {
        std::thread::available_parallelism()
            .map(usize::from)
            .unwrap_or(2)
    }
    fn which(&self, name: &str) -> Option<String> {
        which(name, &self.env())
    }
    fn proc_created(&self, pid: u32) -> Option<u64> {
        proc_created(pid)
    }
    fn call(&self, call: Call) -> io::Result<Reply>;
    fn spawn(&self, spec: &Spawn) -> io::Result<Box<dyn Running>>;
    fn spawn_supervisor(&self, exe: &Path, root: &Path, dir: &Path, jid: &str) -> io::Result<bool>;
    fn spawn_supervisor_identified(
        &self,
        exe: &Path,
        root: &Path,
        dir: &Path,
        jid: &str,
    ) -> io::Result<(bool, Option<u32>)> {
        self.spawn_supervisor(exe, root, dir, jid)
            .map(|detached| (detached, None))
    }
    fn kill_tree(&self, pid: u32) -> io::Result<()> {
        self.call(Call::new(
            vec![
                "taskkill".into(),
                "/T".into(),
                "/F".into(),
                "/PID".into(),
                pid.to_string(),
            ],
            vec![],
            20.,
        ))
        .map(|_| ())
    }
    fn before_load(&self, _path: &Path) -> io::Result<()> {
        Ok(())
    }
    fn before_save(&self, _path: &Path, _job: &Value) -> io::Result<()> {
        Ok(())
    }
    fn replace(&self, src: &Path, dst: &Path) -> io::Result<()> {
        fs::rename(src, dst)
    }
}
/// A failed/deferred transition automatically resumes all frozen processes.
pub trait Paused: Send {
    fn stop(&mut self) -> io::Result<()>;
    fn processes(&self) -> Vec<(u32, u64)>;
    fn arm_process(&mut self, pid: u32, created: u64) -> io::Result<()>;
    fn keep_paused(&mut self);
}
pub struct System {
    start: Instant,
}
impl Default for System {
    fn default() -> Self {
        Self {
            start: Instant::now(),
        }
    }
}
pub fn hidden_command(exe: &str) -> Command {
    let mut c = Command::new(exe);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        c.creation_flags(NO_WINDOW);
    }
    c
}
pub fn which(name: &str, env: &BTreeMap<String, String>) -> Option<String> {
    let path = env.iter().find(|(k, _)| k.eq_ignore_ascii_case("PATH"))?.1;
    for dir in std::env::split_paths(path) {
        let p = dir.join(name);
        if p.is_file() {
            return Some(p.to_string_lossy().into());
        }
        if Path::new(name).extension().is_none() {
            for ext in [".exe", ".cmd", ".bat"] {
                let p = dir.join(format!("{name}{ext}"));
                if p.is_file() {
                    return Some(p.to_string_lossy().into());
                }
            }
        }
    }
    None
}
pub struct Spawn {
    pub argv: Vec<String>,
    pub prompt: PathBuf,
    pub cwd: PathBuf,
    pub env: BTreeMap<String, String>,
    pub flags: u32,
}
pub trait Running: Send {
    fn pid(&self) -> u32;
    fn stdout(&mut self) -> Option<Box<dyn Read + Send>>;
    fn stderr(&mut self) -> Option<Box<dyn Read + Send>>;
    fn poll(&mut self) -> io::Result<Option<i64>>;
    fn kill(&mut self) -> io::Result<()>;
}
impl Running for Child {
    fn pid(&self) -> u32 {
        self.id()
    }
    fn stdout(&mut self) -> Option<Box<dyn Read + Send>> {
        self.stdout.take().map(|s| Box::new(s) as _)
    }
    fn stderr(&mut self) -> Option<Box<dyn Read + Send>> {
        self.stderr.take().map(|s| Box::new(s) as _)
    }
    fn poll(&mut self) -> io::Result<Option<i64>> {
        self.try_wait()
            .map(|s| s.map(|s| s.code().map(|c| c as u32 as i64).unwrap_or(1)))
    }
    fn kill(&mut self) -> io::Result<()> {
        Child::kill(self)
    }
}
impl Runtime for System {
    fn boot_epoch(&self) -> Option<f64> {
        #[cfg(windows)]
        {
            Some(
                self.epoch()
                    - unsafe { windows_sys::Win32::System::SystemInformation::GetTickCount64() }
                        as f64
                        / 1000.,
            )
        }
        #[cfg(not(windows))]
        {
            None
        }
    }
    fn recovery_protected(&self, cwd: &str) -> io::Result<bool> {
        let reply = self.call(Call::new(
            vec![
                "D:/CC/Tools/client-records/scrub391.exe".into(),
                "--guard-path".into(),
                cwd.into(),
            ],
            vec![],
            10.,
        ))?;
        if reply.code != 0 {
            return Err(io::Error::other("recovery resource guard unavailable"));
        }
        serde_json::from_slice::<Value>(&reply.stdout)
            .ok()
            .and_then(|v| v["client_resource"].as_bool())
            .ok_or_else(|| io::Error::other("invalid recovery resource guard"))
    }
    fn epoch(&self) -> f64 {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs_f64()
    }
    fn env(&self) -> BTreeMap<String, String> {
        std::env::vars()
            .map(|(k, v)| (k.to_uppercase(), v))
            .collect()
    }
    fn mono(&self) -> f64 {
        self.start.elapsed().as_secs_f64()
    }
    fn call(&self, call: Call) -> io::Result<Reply> {
        let mut cmd = hidden_command(&call.argv[0]);
        if let Some(cwd) = &call.cwd {
            cmd.current_dir(cwd);
        }
        cmd.args(&call.argv[1..])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        #[cfg(windows)]
        if call.argv.get(1).is_some_and(|a| a == "notify")
            && call.argv.get(2).is_some_and(|a| a == "task")
        {
            cmd.envs(notifier_overrides(
                &call.argv,
                &self.env(),
                &saved_notifier_environment(),
            ));
        }
        let mut child = cmd.spawn()?;
        let mut input = child.stdin.take().unwrap();
        let bytes = call.input;
        std::thread::spawn(move || {
            let _ = input.write_all(&bytes);
        });
        let mut out = child.stdout.take().unwrap();
        let mut err = child.stderr.take().unwrap();
        let (tx, rx) = std::sync::mpsc::channel();
        let tx2 = tx.clone();
        std::thread::spawn(move || {
            let mut b = Vec::new();
            let r = out.read_to_end(&mut b);
            let _ = tx.send((true, r, b));
        });
        std::thread::spawn(move || {
            let mut b = Vec::new();
            let r = err.read_to_end(&mut b);
            let _ = tx2.send((false, r, b));
        });
        let until = Instant::now() + Duration::from_secs_f64(call.timeout.max(0.001));
        let mut reply = Reply {
            code: 0,
            stdout: vec![],
            stderr: vec![],
        };
        let (mut done, mut code) = (0, None);
        loop {
            if Instant::now() >= until {
                let _ = child.kill();
                let _ = child.wait();
                return Err(io::Error::new(io::ErrorKind::TimedOut, "TimeoutExpired"));
            }
            if let Ok((isout, r, b)) = rx.try_recv() {
                r?;
                if isout {
                    reply.stdout = b
                } else {
                    reply.stderr = b
                }
                done += 1;
            }
            if code.is_none() {
                code = child
                    .try_wait()?
                    .map(|s| s.code().map(|c| c as u32 as i64).unwrap_or(1));
            }
            if let (2, Some(code)) = (done, code) {
                reply.code = code;
                return Ok(reply);
            }
            std::thread::sleep(Duration::from_millis(5));
        }
    }
    fn spawn(&self, spec: &Spawn) -> io::Result<Box<dyn Running>> {
        let mut c = hidden_command(&spec.argv[0]);
        c.args(&spec.argv[1..])
            .stdin(File::open(&spec.prompt)?)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .current_dir(&spec.cwd)
            .env_clear()
            .envs(&spec.env);
        Ok(Box::new(c.spawn()?))
    }
    /// Start the supervisor outside the caller's job object. Returns False if that was refused.
    fn spawn_supervisor(&self, exe: &Path, root: &Path, dir: &Path, jid: &str) -> io::Result<bool> {
        #[cfg(windows)]
        {
            spawn_supervisor_closed_handles(exe, root, dir, jid).map(|result| result.0)
        }
        #[cfg(not(windows))]
        {
            let log = OpenOptions::new()
                .create(true)
                .append(true)
                .open(dir.join("supervisor.log"))?;
            let mut c = hidden_command(&exe.to_string_lossy());
            c.args(["_supervise", jid])
                .stdin(Stdio::null())
                .stdout(log.try_clone()?)
                .stderr(log.try_clone()?)
                .current_dir(root);
            #[cfg(windows)]
            {
                use std::os::windows::process::CommandExt;
                c.creation_flags(NO_WINDOW | 0x200 | 0x01000000);
            }
            match c.spawn() {
                Ok(_) => Ok(true),
                Err(_) => {
                    let mut log = log;
                    log.write_all(b"warning: breakaway from the caller's job object was refused; this job is killed if the caller's job object closes\n")?;
                    #[cfg(windows)]
                    {
                        use std::os::windows::process::CommandExt;
                        c.creation_flags(NO_WINDOW | 0x200);
                    }
                    c.spawn()?;
                    Ok(false)
                }
            }
        }
    }
    fn spawn_supervisor_identified(
        &self,
        exe: &Path,
        root: &Path,
        dir: &Path,
        jid: &str,
    ) -> io::Result<(bool, Option<u32>)> {
        #[cfg(windows)]
        {
            spawn_supervisor_closed_handles(exe, root, dir, jid)
                .map(|(detached, pid)| (detached, Some(pid)))
        }
        #[cfg(not(windows))]
        {
            self.spawn_supervisor(exe, root, dir, jid)
                .map(|detached| (detached, None))
        }
    }
}
/// Creation time (FILETIME) of a live process, else None. Compared with the value recorded at start,
/// so a reused PID is never mistaken for our process (and never killed).
#[cfg(windows)]
pub fn proc_created(pid: u32) -> Option<u64> {
    use windows_sys::Win32::{Foundation::*, System::Threading::*};
    if pid == 0 {
        return None;
    }
    unsafe {
        let h = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if h.is_null() {
            return None;
        }
        let mut code = 0;
        let mut ts = [FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        }; 4];
        let ok = GetExitCodeProcess(h, &mut code) != 0
            && code == 259
            && GetProcessTimes(h, &mut ts[0], &mut ts[1], &mut ts[2], &mut ts[3]) != 0;
        CloseHandle(h);
        ok.then_some(((ts[0].dwHighDateTime as u64) << 32) | ts[0].dwLowDateTime as u64)
    }
}
#[cfg(not(windows))]
pub fn proc_created(_pid: u32) -> Option<u64> {
    None
}

/// Serialize starting codex and cancellation across the two processes.
/// Locks byte zero, without reading or writing it; interoperates with msvcrt.locking.
pub struct FileLock {
    file: File,
    /// Sidecar describing the holder (pid + operation only, never secrets).
    owner: Option<PathBuf>,
}
fn lock_owner_path(path: &Path) -> PathBuf {
    let mut name = path.as_os_str().to_owned();
    name.push(".owner");
    PathBuf::from(name)
}
/// Short holder description for timeout diagnostics; empty when unknown.
fn lock_owner_note(path: &Path) -> String {
    match std::fs::read_to_string(lock_owner_path(path)) {
        Ok(text) if text.len() <= 512 => format!("; last recorded holder {}", text.trim()),
        _ => String::new(),
    }
}
impl FileLock {
    /// Record who holds this lock and why. Best effort: diagnostics must never
    /// change locking behaviour. Removed again when the lock is released.
    pub fn set_owner(&mut self, path: &Path, operation: &str) {
        let owner = lock_owner_path(path);
        let note = format!(
            "{{\"pid\":{},\"operation\":{},\"since_epoch\":{}}}",
            std::process::id(),
            serde_json::Value::from(operation),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map_or(0, |d| d.as_secs())
        );
        if std::fs::write(&owner, note).is_ok() {
            self.owner = Some(owner);
        }
    }
    pub fn acquire_queue(path: &Path) -> io::Result<Self> {
        Self::acquire_queue_timeout(path, Duration::from_secs(60))
    }
    /// Queue the lock request in the kernel instead of repeatedly competing
    /// with newly arriving supervisors. A cancelled request is drained before
    /// its OVERLAPPED structure or event can be dropped.
    pub fn acquire_queue_timeout(path: &Path, timeout: Duration) -> io::Result<Self> {
        #[cfg(windows)]
        {
            use std::os::windows::{fs::OpenOptionsExt, io::AsRawHandle};
            use windows_sys::Win32::{
                Foundation::*,
                Storage::FileSystem::*,
                System::{IO::*, Threading::*},
            };
            let started = Instant::now();
            let file = OpenOptions::new()
                .create(true)
                .truncate(false)
                .read(true)
                .write(true)
                .custom_flags(FILE_FLAG_OVERLAPPED)
                .open(path)?;
            struct Event(HANDLE);
            impl Drop for Event {
                fn drop(&mut self) {
                    unsafe {
                        CloseHandle(self.0);
                    }
                }
            }
            let event = Event(unsafe { CreateEventW(std::ptr::null(), 1, 0, std::ptr::null()) });
            if event.0.is_null() {
                return Err(io::Error::last_os_error());
            }
            let mut overlapped = OVERLAPPED {
                hEvent: event.0,
                ..Default::default()
            };
            if unsafe {
                LockFileEx(
                    file.as_raw_handle() as _,
                    LOCKFILE_EXCLUSIVE_LOCK,
                    0,
                    1,
                    0,
                    &mut overlapped,
                )
            } != 0
            {
                return Ok(Self { file, owner: None });
            }
            let error = io::Error::last_os_error();
            if error.raw_os_error() != Some(ERROR_IO_PENDING as i32) {
                return Err(error);
            }
            let milliseconds = timeout
                .saturating_sub(started.elapsed())
                .as_millis()
                .min((u32::MAX - 1) as u128) as u32;
            let wait = unsafe { WaitForSingleObject(event.0, milliseconds) };
            let wait_error = (wait == WAIT_FAILED).then(io::Error::last_os_error);
            if wait != WAIT_OBJECT_0 {
                unsafe {
                    CancelIoEx(file.as_raw_handle() as _, &overlapped);
                }
            }
            let mut bytes = 0;
            if unsafe { GetOverlappedResult(file.as_raw_handle() as _, &overlapped, &mut bytes, 1) }
                != 0
            {
                // Completion may win the race with cancellation.
                return Ok(Self { file, owner: None });
            }
            let completion_error = io::Error::last_os_error();
            if let Some(error) = wait_error {
                return Err(error);
            }
            if wait == WAIT_TIMEOUT {
                return Err(io::Error::new(
                    io::ErrorKind::TimedOut,
                    format!(
                        "lock wait exceeded {} ms: {completion_error}{}",
                        timeout.as_millis(),
                        lock_owner_note(path)
                    ),
                ));
            }
            Err(completion_error)
        }
        #[cfg(not(windows))]
        {
            let _ = timeout;
            Self::try_acquire(path)
        }
    }
    pub fn acquire(path: &Path) -> io::Result<Self> {
        Self::acquire_queue_timeout(path, Duration::from_secs(10))
    }
    pub fn try_acquire(path: &Path) -> io::Result<Self> {
        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .read(true)
            .open(path)?;
        lock_byte(&file, false)?;
        Ok(Self { file, owner: None })
    }
}
impl Drop for FileLock {
    fn drop(&mut self) {
        if let Some(owner) = self.owner.take() {
            let _ = std::fs::remove_file(owner);
        }
        let _ = lock_byte(&self.file, true);
    }
}
#[cfg(windows)]
fn lock_byte(f: &File, unlock: bool) -> io::Result<()> {
    use std::os::windows::io::AsRawHandle;
    use windows_sys::Win32::{Storage::FileSystem::*, System::IO::OVERLAPPED};
    unsafe {
        let mut o: OVERLAPPED = std::mem::zeroed();
        let ok = if unlock {
            UnlockFileEx(f.as_raw_handle() as _, 0, 1, 0, &mut o)
        } else {
            LockFileEx(
                f.as_raw_handle() as _,
                LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY,
                0,
                1,
                0,
                &mut o,
            )
        };
        if ok == 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(())
        }
    }
}
#[cfg(not(windows))]
fn lock_byte(_f: &File, _unlock: bool) -> io::Result<()> {
    Err(io::Error::new(
        io::ErrorKind::Unsupported,
        "Windows byte-range lock required",
    ))
}

pub type SharedRuntime = Arc<dyn Runtime>;
/// A TextIO-style stdout/stderr writer. Python translates LF to CRLF on Windows.
pub struct TextWriter<W: Write>(pub W);
impl<W: Write> Write for TextWriter<W> {
    fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
        #[cfg(windows)]
        {
            let mut b = Vec::with_capacity(bytes.len());
            for c in bytes {
                if *c == b'\n' {
                    b.push(b'\r');
                }
                b.push(*c);
            }
            self.0.write_all(&b)?;
            Ok(bytes.len())
        }
        #[cfg(not(windows))]
        {
            self.0.write(bytes)
        }
    }
    fn flush(&mut self) -> io::Result<()> {
        self.0.flush()
    }
}

/// Python's close_fds=True: only the three explicitly listed stdio handles are inherited.
/// Rust Command on Windows otherwise leaks the caller's capture pipes to the supervisor,
/// making a nominally detached `run` wait for the entire job (or its grandchildren).
#[cfg(windows)]
fn spawn_supervisor_closed_handles(
    exe: &Path,
    root: &Path,
    dir: &Path,
    jid: &str,
) -> io::Result<(bool, u32)> {
    use std::os::windows::{ffi::OsStrExt, io::AsRawHandle};
    use windows_sys::Win32::{Foundation::*, System::Threading::*};
    struct Owned(HANDLE);
    impl Drop for Owned {
        fn drop(&mut self) {
            unsafe {
                CloseHandle(self.0);
            }
        }
    }
    struct Attributes(LPPROC_THREAD_ATTRIBUTE_LIST);
    impl Drop for Attributes {
        fn drop(&mut self) {
            unsafe {
                DeleteProcThreadAttributeList(self.0);
            }
        }
    }
    let mut log = OpenOptions::new()
        .create(true)
        .append(true)
        .open(dir.join("supervisor.log"))?;
    let nul = OpenOptions::new().read(true).open("NUL")?;
    unsafe {
        let process = GetCurrentProcess();
        let dup = |file: &File| -> io::Result<Owned> {
            let mut h = std::ptr::null_mut();
            if DuplicateHandle(
                process,
                file.as_raw_handle() as _,
                process,
                &mut h,
                0,
                1,
                DUPLICATE_SAME_ACCESS,
            ) == 0
            {
                return Err(io::Error::last_os_error());
            }
            Ok(Owned(h))
        };
        let stdin = dup(&nul)?;
        let stdout = dup(&log)?;
        let stderr = dup(&log)?;
        let handles = [stdin.0, stdout.0, stderr.0];
        let mut size = 0;
        InitializeProcThreadAttributeList(std::ptr::null_mut(), 1, 0, &mut size);
        let mut storage = vec![0usize; size.div_ceil(std::mem::size_of::<usize>())];
        let ptr = storage.as_mut_ptr() as LPPROC_THREAD_ATTRIBUTE_LIST;
        if InitializeProcThreadAttributeList(ptr, 1, 0, &mut size) == 0 {
            return Err(io::Error::last_os_error());
        }
        let attributes = Attributes(ptr);
        if UpdateProcThreadAttribute(
            ptr,
            0,
            PROC_THREAD_ATTRIBUTE_HANDLE_LIST as usize,
            handles.as_ptr() as _,
            std::mem::size_of_val(&handles),
            std::ptr::null_mut(),
            std::ptr::null(),
        ) == 0
        {
            return Err(io::Error::last_os_error());
        }
        let mut info = STARTUPINFOEXW::default();
        info.StartupInfo.cb = std::mem::size_of::<STARTUPINFOEXW>() as u32;
        info.StartupInfo.dwFlags = STARTF_USESTDHANDLES;
        info.StartupInfo.hStdInput = stdin.0;
        info.StartupInfo.hStdOutput = stdout.0;
        info.StartupInfo.hStdError = stderr.0;
        info.lpAttributeList = attributes.0;
        let application = exe.as_os_str().encode_wide().chain([0]).collect::<Vec<_>>();
        let cwd = root
            .as_os_str()
            .encode_wide()
            .chain([0])
            .collect::<Vec<_>>();
        let spawn = |breakaway: bool| -> io::Result<u32> {
            let mut line = format!("\"{}\" _supervise {jid}", exe.display())
                .encode_utf16()
                .chain([0])
                .collect::<Vec<_>>();
            let mut pi = PROCESS_INFORMATION::default();
            let flags = NO_WINDOW
                | CREATE_NEW_PROCESS_GROUP
                | EXTENDED_STARTUPINFO_PRESENT
                | if breakaway {
                    CREATE_BREAKAWAY_FROM_JOB
                } else {
                    0
                };
            if CreateProcessW(
                application.as_ptr(),
                line.as_mut_ptr(),
                std::ptr::null(),
                std::ptr::null(),
                1,
                flags,
                std::ptr::null(),
                cwd.as_ptr(),
                &info.StartupInfo,
                &mut pi,
            ) == 0
            {
                return Err(io::Error::last_os_error());
            }
            CloseHandle(pi.hThread);
            CloseHandle(pi.hProcess);
            Ok(pi.dwProcessId)
        };
        if let Ok(pid) = spawn(true) {
            return Ok((true, pid));
        }
        log.write_all(b"warning: breakaway from the caller's job object was refused; this job is killed if the caller's job object closes\n")?;
        log.flush()?;
        let pid = spawn(false)?;
        Ok((false, pid))
    }
}
