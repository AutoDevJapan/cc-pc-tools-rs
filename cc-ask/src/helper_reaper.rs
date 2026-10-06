//! Reap only positively identified stale Codex helper sets, never arbitrary trees.
use crate::core::Result;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    path::Path,
};

const TICKS_PER_SECOND: u64 = 10_000_000;
const GRACE: u64 = 2 * 60 * 60 * TICKS_PER_SECOND;
const MAX_SAMPLE_GAP: u64 = 5 * 60 * TICKS_PER_SECOND;
const STEAM_LIMIT: u64 = 4 * 1024 * 1024 * 1024;
const LIMIT: usize = 50;

#[derive(Clone, Debug)]
pub struct Process {
    pub pid: u32,
    pub parent: u32,
    /// Native FILETIME, not rounded WMI time. Zero means unknown: never reap.
    pub created: u64,
    pub image: String,
    pub command: String,
    pub bytes: u64,
    pub private_bytes: u64,
    pub cpu: Option<u64>,
    pub excluded: bool,
}
type Table = BTreeMap<u32, Process>;

#[derive(Clone, Debug, Serialize, Deserialize)]
struct Sample {
    created: u64,
    cpu: u64,
    at: u64,
    idle_since: u64,
}
#[derive(Default, Serialize, Deserialize)]
struct Watch {
    samples: BTreeMap<u32, Sample>,
    pending_helpers: u64,
    pending_steam: u64,
    pending_bytes: u64,
    last_log: u64,
    #[serde(default)]
    waiting: Vec<Exit>,
}
#[derive(Clone, Serialize, Deserialize)]
struct Exit {
    pid: u32,
    created: u64,
    bytes: u64,
    steam: bool,
}
impl Watch {
    fn confirm(&mut self, exit: &Exit) {
        if exit.steam {
            self.pending_steam += 1;
        } else {
            self.pending_helpers += 1;
        }
        self.pending_bytes += exit.bytes;
    }
    fn reconcile(&mut self, processes: &[Process]) -> (usize, u64) {
        let mut count = 0;
        let mut bytes = 0;
        for exit in std::mem::take(&mut self.waiting) {
            let gone = match processes.iter().find(|p| p.pid == exit.pid) {
                None => true,
                Some(p) => p.created != 0 && p.created != exit.created,
            };
            if gone {
                self.confirm(&exit);
                count += 1;
                bytes += exit.bytes;
            } else {
                self.waiting.push(exit);
            }
        }
        (count, bytes)
    }
    fn requested(&self, p: &Process) -> bool {
        self.waiting
            .iter()
            .any(|exit| exit.pid == p.pid && exit.created == p.created)
    }
    fn observe(&mut self, processes: &[Process], now: u64) {
        let mut next = BTreeMap::new();
        for p in processes {
            if p.created == 0
                || !matches!(
                    name(p).as_str(),
                    "node.exe"
                        | "node_repl.exe"
                        | "stdio-shim-v2.exe"
                        | "godd-native.exe"
                        | "conhost.exe"
                )
            {
                continue;
            }
            let Some(cpu) = p.cpu else {
                continue;
            };
            let idle_since = self
                .samples
                .get(&p.pid)
                .filter(|old| {
                    old.created == p.created
                        && old.cpu == cpu
                        && now >= old.at
                        && now - old.at <= MAX_SAMPLE_GAP
                })
                .map_or(now, |old| old.idle_since);
            next.insert(
                p.pid,
                Sample {
                    created: p.created,
                    cpu,
                    at: now,
                    idle_since,
                },
            );
        }
        self.samples = next;
    }
    fn idle(&self, p: &Process, now: u64) -> bool {
        self.samples.get(&p.pid).is_some_and(|s| {
            s.created == p.created
                && Some(s.cpu) == p.cpu
                && now >= s.at
                && now - s.at <= MAX_SAMPLE_GAP
                && now >= s.idle_since
                && now - s.idle_since >= GRACE
        })
    }
}
fn watch_path(jobs: &Path) -> std::path::PathBuf {
    jobs.join("helper-memory-watch.json")
}
fn load_watch(path: &Path) -> Result<Watch> {
    match fs::read(path) {
        Ok(data) => serde_json::from_slice(&data).map_err(|_| "memory watch state invalid".into()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Watch::default()),
        Err(_) => Err("memory watch state unreadable".into()),
    }
}
fn save_watch(path: &Path, watch: &Watch) -> Result<()> {
    use std::io::Write;
    let mut temp = tempfile::NamedTempFile::new_in(path.parent().ok_or("watch parent missing")?)
        .map_err(|_| "watch temporary file failed")?;
    serde_json::to_writer(&mut temp, watch).map_err(|_| "watch serialization failed")?;
    temp.flush()
        .and_then(|_| temp.as_file().sync_all())
        .map_err(|_| "watch sync failed")?;
    crate::core::replace_file(temp.path(), path)
}
fn steam_target(p: &Process, table: &Table) -> bool {
    if p.excluded
        || p.created == 0
        || name(p) != "steamwebhelper.exe"
        || p.bytes.max(p.private_bytes) <= STEAM_LIMIT
    {
        return false;
    }
    let image = norm(&p.image);
    let Some(base) = image.strip_suffix("/bin/cef/cef.win64/steamwebhelper.exe") else {
        return false;
    };
    let mut current = p;
    let mut seen = BTreeSet::new();
    while seen.insert(current.pid) {
        let Some(parent) = table.get(&current.parent) else {
            return false;
        };
        if parent.excluded || parent.created == 0 || parent.created > current.created {
            return false;
        }
        if norm(&parent.image) == format!("{base}/steam.exe") {
            return true;
        }
        if norm(&parent.image) != image {
            return false;
        }
        current = parent;
    }
    false
}

/// Batch successful terminations into one silent cleanup record, at most every 15 minutes.
pub fn flush_log(ctx: &mut crate::core::Context) -> Result<()> {
    let path = watch_path(&ctx.jobs);
    let mut watch = load_watch(&path)?;
    let now = (ctx.now() * TICKS_PER_SECOND as f64) as u64;
    if watch.pending_helpers + watch.pending_steam == 0
        || (watch.last_log != 0 && now.saturating_sub(watch.last_log) < 15 * 60 * TICKS_PER_SECOND)
    {
        return Ok(());
    }
    let text = format!("【GoDD-AO】メモリの見張りで、休止した Codex の補助プロセスを{}件、4 GiBを超えた Steam の画面用プロセスを{}件終了しました。終了前のメモリ使用量は合計約{:.2} GiBでした。",
        watch.pending_helpers, watch.pending_steam, watch.pending_bytes as f64 / 1073741824.);
    // Back off even on an ambiguous network failure; pending counts remain durable.
    watch.last_log = now;
    save_watch(&path, &watch)?;
    crate::commands::cmd_log(ctx, "後片付けの記録", &text, None, false)?;
    watch.pending_helpers = 0;
    watch.pending_steam = 0;
    watch.pending_bytes = 0;
    save_watch(&path, &watch)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
enum Kind {
    Cua,
    Repl,
    Kernel,
    Shim,
    Native,
    Console,
}

fn norm(s: &str) -> String {
    s.replace('\\', "/").to_ascii_lowercase()
}
fn name(p: &Process) -> String {
    norm(&p.image).rsplit('/').next().unwrap_or("").to_owned()
}
fn script(command: &str, ending: &str) -> bool {
    norm(command)
        .split_whitespace()
        .any(|x| x.trim_matches('"').ends_with(ending))
}
fn agent(p: &Process) -> bool {
    matches!(
        name(p).as_str(),
        "codex.exe"
            | "claude.exe"
            | "opencode"
            | "opencode.exe"
            | "zed.exe"
            | "code.exe"
            | "cursor.exe"
    ) || script(&p.command, "/codex.js")
}
fn batch(p: &Process) -> bool {
    // Also protect unregistered `codex exec` processes and all their descendants.
    matches!(name(p).as_str(), "codex.exe" | "node.exe")
        && (name(p) == "codex.exe" || script(&p.command, "/codex.js"))
        && p.command
            .split_whitespace()
            .any(|x| matches!(x.trim_matches('"'), "exec" | "e"))
}
fn interactive_codex(p: &Process) -> bool {
    // Desktop/app-server can own simultaneous threads. Only a known CLI TUI
    // is eligible for stale-duplicate collection; ambiguous owners stay protected.
    name(p) == "codex.exe"
        && norm(&p.image).contains("/vendor/")
        && norm(&p.image).ends_with("/bin/codex.exe")
        && !p.command.trim().is_empty()
        && !p.command.split_whitespace().any(|x| {
            matches!(
                x.trim_matches('"'),
                "agents"
                    | "exec"
                    | "e"
                    | "app-server"
                    | "mcp-server"
                    | "review"
                    | "login"
                    | "logout"
                    | "mcp"
                    | "plugin"
                    | "remote-control"
                    | "app"
                    | "update"
                    | "doctor"
                    | "completion"
                    | "sandbox"
                    | "debug"
                    | "apply"
                    | "a"
                    | "queue"
                    | "archive"
                    | "delete"
                    | "migrate-rollouts"
                    | "unarchive"
                    | "cloud"
                    | "features"
                    | "exec-server"
                    | "help"
                    | "--help"
                    | "-h"
                    | "--version"
                    | "-V"
            )
        })
}
fn kind(p: &Process) -> Option<Kind> {
    let image = norm(&p.image);
    if image == "d:/ai/openclawbridgebackup/stdio-shim-v2.exe"
        && (script(&p.command, "/godd.cjs") || script(&p.command, "/godd-native.exe"))
    {
        return Some(Kind::Shim);
    }
    // Positive image allowlist. Unrelated Node and the Codex JS launcher cannot enter it.
    if !image.contains("/openai/codex/runtimes/cua_node/") || script(&p.command, "/codex.js") {
        return None;
    }
    match name(p).as_str() {
        "node_repl.exe" if image.ends_with("/bin/node_repl.exe") => Some(Kind::Repl),
        "node.exe" if image.ends_with("/bin/node.exe") => {
            if script(&p.command, "/node_modules/@oai/cua-repl/bin/cua-repl.mjs") {
                Some(Kind::Cua)
            } else if script(&p.command, "/kernel.js") && p.command.contains("--session-id") {
                Some(Kind::Kernel)
            } else {
                None
            }
        }
        _ => None,
    }
}

/// Descendants are protected by PID even when an edge's creation time is uncertain.
/// This deliberately overprotects reused PIDs rather than risking a running job.
fn protect_tree(table: &Table, protected: &mut BTreeSet<u32>) {
    loop {
        let before = protected.len();
        for p in table.values() {
            if protected.contains(&p.parent) {
                protected.insert(p.pid);
            }
        }
        if protected.len() == before {
            break;
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Set {
    pub root: u32,
    pub members: Vec<u32>,
    pub reason: String,
}

/// A missing parent is evidence of orphaning only if every traversed edge is valid.
/// A present parent created after the child is a reused PID, not an orphan verdict.
fn owner(table: &Table, root: u32) -> (Option<u32>, bool) {
    let mut seen = BTreeSet::new();
    let mut current = root;
    while seen.insert(current) {
        let Some(p) = table.get(&current) else {
            return (None, false);
        };
        if p.created == 0 {
            return (None, false);
        }
        if agent(p) {
            return (Some(p.pid), false);
        }
        let Some(parent) = table.get(&p.parent) else {
            return (None, p.parent != 0);
        };
        if parent.created == 0 || parent.created > p.created {
            return (None, false);
        }
        current = parent.pid;
    }
    (None, false)
}

pub fn plan(processes: &[Process], jobs: &[u32], now: u64) -> Vec<Set> {
    plan_guarded(processes, jobs, now, None)
}
fn plan_guarded(
    processes: &[Process],
    jobs: &[u32],
    now: u64,
    recorded_job_start: Option<u64>,
) -> Vec<Set> {
    let table: Table = processes.iter().map(|p| (p.pid, p.clone())).collect();
    // Collect all protection before even considering candidates.
    let mut job_tree: BTreeSet<u32> = jobs.iter().copied().collect();
    job_tree.extend(
        table
            .values()
            .filter(|p| batch(p) || (name(p) == "codex.exe" && !interactive_codex(p)))
            .map(|p| p.pid),
    );
    let oldest_job = job_tree
        .iter()
        .map(|pid| table.get(pid).map_or(0, |p| p.created))
        .chain(recorded_job_start)
        .min();
    protect_tree(&table, &mut job_tree);
    let mut protected = job_tree;
    protected.extend(
        table
            .values()
            .filter(|p| agent(p) || p.created == 0 || now.saturating_sub(p.created) < GRACE)
            .map(|p| p.pid),
    );
    // Client applications and the protected bot, including their descendants,
    // are never maintenance targets. No files inside those projects are read.
    let mut excluded: BTreeSet<u32> = table
        .values()
        .filter(|p| {
            let text = format!("{} {}", norm(&p.image), norm(&p.command));
            p.excluded || text.contains("openmausbot") || text.contains("autoselling")
        })
        .map(|p| p.pid)
        .collect();
    protect_tree(&table, &mut excluded);
    protected.extend(excluded);

    let mut kinds: BTreeMap<u32, Kind> = table
        .values()
        .filter_map(|p| kind(p).map(|k| (p.pid, k)))
        .collect();
    // A runtime's trusted worker is part of its kernel only with verified ancestry.
    for p in table.values() {
        if norm(&p.image).contains("/openai/codex/runtimes/cua_node/")
            && name(p) == "node.exe"
            && script(&p.command, "/trusted-worker.js")
            && !script(&p.command, "/codex.js")
            && table
                .get(&p.parent)
                .is_some_and(|a| a.created != 0 && a.created <= p.created)
            && matches!(kinds.get(&p.parent), Some(Kind::Repl | Kind::Kernel))
        {
            kinds.insert(p.pid, Kind::Kernel);
        }
    }
    // Attach only recognized leaf helpers with a verified edge. An unknown child
    // continues to preserve the entire group rather than being killed recursively.
    loop {
        let before = kinds.len();
        for p in table.values() {
            if kinds.contains_key(&p.pid) || !kinds.contains_key(&p.parent) {
                continue;
            }
            if p.created == 0
                || table[&p.parent].created == 0
                || p.created < table[&p.parent].created
            {
                continue;
            }
            let image = norm(&p.image);
            let attached = match name(p).as_str() {
                "conhost.exe" if image == "c:/windows/system32/conhost.exe" => Some(Kind::Console),
                "godd-native.exe" if script(&table[&p.parent].command, &image) => {
                    Some(Kind::Native)
                }
                "node.exe"
                    if matches!(kinds[&p.parent], Kind::Shim)
                        && script(&p.command, "/godd.cjs") =>
                {
                    Some(Kind::Kernel)
                }
                _ => None,
            };
            if let Some(k) = attached {
                kinds.insert(p.pid, k);
            }
        }
        if kinds.len() == before {
            break;
        }
    }
    let mut groups: BTreeMap<u32, Vec<u32>> = BTreeMap::new();
    for pid in kinds.keys() {
        let mut root = *pid;
        let mut seen = BTreeSet::new();
        while seen.insert(root) {
            let p = &table[&root];
            let Some(parent) = table.get(&p.parent) else {
                break;
            };
            if !kinds.contains_key(&parent.pid) || parent.created == 0 || parent.created > p.created
            {
                break;
            }
            root = parent.pid;
        }
        groups.entry(root).or_default().push(*pid);
    }
    let mut newest: BTreeMap<(u32, Kind), u64> = BTreeMap::new();
    for root in groups.keys() {
        if let (Some(a), _) = owner(&table, *root) {
            newest
                .entry((a, kinds[root]))
                .and_modify(|t| *t = (*t).max(table[root].created))
                .or_insert(table[root].created);
        }
    }
    for (root, members) in &groups {
        if let (Some(a), _) = owner(&table, *root) {
            if newest[&(a, kinds[root])] == table[root].created {
                protected.extend(members);
            }
        }
    }
    let mut candidates = vec![];
    for (root, members) in groups {
        if members.iter().any(|p| protected.contains(p)) {
            continue;
        }
        // Do not split a helper set that has an unrecognized child or a reused edge.
        if table.values().any(|p| {
            members.contains(&p.parent)
                && (!members.contains(&p.pid) || p.created < table[&p.parent].created)
        }) {
            continue;
        }
        let (a, orphan) = owner(&table, root);
        // An intermediate parent can disappear while its owning job stays alive.
        // Without provenance, preserve orphans that could have been born in that job.
        if orphan && oldest_job.is_some_and(|start| start <= table[&root].created) {
            continue;
        }
        let reason = if orphan {
            "orphan: parent absent, creation chain verified".to_owned()
        } else if let Some(a) = a {
            if !interactive_codex(&table[&a]) || newest[&(a, kinds[&root])] <= table[&root].created
            {
                continue;
            }
            format!(
                "stale {:?}: interactive Codex {a} has a newer set",
                kinds[&root]
            )
        } else {
            continue;
        };
        candidates.push(Set {
            root,
            members,
            reason,
        });
    }
    candidates.sort_by_key(|s| (table[&s.root].created, s.root));
    let mut count = 0;
    candidates.retain(|s| {
        if count + s.members.len() > LIMIT {
            return false;
        }
        count += s.members.len();
        true
    });
    candidates
}

/// Unreadable job records disable reaping for this pass, rather than guessing.
struct Jobs {
    roots: Vec<u32>,
    oldest: Option<u64>,
}
fn job_roots(home: &Path) -> Result<Jobs> {
    let mut roots = vec![];
    let mut oldest = None;
    for entry in fs::read_dir(home.join("jobs")).map_err(|e| format!("job scan: {e}"))? {
        let entry = entry.map_err(|e| e.to_string())?;
        if !entry.file_type().map_err(|e| e.to_string())?.is_dir() {
            continue;
        }
        let path = entry.path().join("job.json");
        let data = match fs::read(&path) {
            Ok(data) => data,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => continue,
            Err(e) => return Err(format!("job read: {e}")),
        };
        let job: Value = serde_json::from_slice(&data).map_err(|e| format!("job parse: {e}"))?;
        let status = job["status"].as_str().ok_or("job status missing")?;
        if !["running", "starting", "queued"].contains(&status) {
            continue;
        }
        let start = ["pid_created", "codex_pid_created"]
            .iter()
            .filter_map(|key| job[*key].as_u64())
            .filter(|time| *time != 0)
            .min()
            .or_else(|| {
                let date =
                    chrono::DateTime::parse_from_rfc3339(job["created_at"].as_str()?).ok()?;
                let micros = date
                    .timestamp_micros()
                    .checked_add(11_644_473_600_000_000)?;
                u64::try_from(micros).ok()?.checked_mul(10)
            })
            .unwrap_or(0);
        oldest = Some(oldest.map_or(start, |t: u64| t.min(start)));
        for key in ["pid", "codex_pid"] {
            if let Some(pid) = job[key]
                .as_u64()
                .and_then(|p| u32::try_from(p).ok())
                .filter(|p| *p != 0)
            {
                // Even legacy jobs without creation times get their full tree protected.
                roots.push(pid);
            } else if !job[key].is_null() {
                return Err(format!("invalid job {key}"));
            }
        }
    }
    Ok(Jobs { roots, oldest })
}

fn revalidated(
    set: &Set,
    initial: &Table,
    fresh: &[Process],
    jobs: &[u32],
    now: u64,
    recorded_job_start: Option<u64>,
) -> bool {
    plan_guarded(fresh, jobs, now, recorded_job_start).contains(set)
        && set.members.iter().all(|pid| {
            fresh
                .iter()
                .any(|p| p.pid == *pid && p.created == initial[pid].created)
        })
}

#[cfg(windows)]
mod native {
    use super::*;
    use serde::Deserialize;
    use windows_sys::Win32::{
        Foundation::*,
        System::{Diagnostics::ToolHelp::*, ProcessStatus::*, Threading::*},
    };

    pub struct Handle(HANDLE);
    impl Drop for Handle {
        fn drop(&mut self) {
            unsafe {
                CloseHandle(self.0);
            }
        }
    }
    fn created(h: HANDLE) -> Option<u64> {
        let mut times: [FILETIME; 4] = unsafe { std::mem::zeroed() };
        let mut code = 0;
        unsafe {
            (GetExitCodeProcess(h, &mut code) != 0
                && code == 259
                && GetProcessTimes(
                    h,
                    &mut times[0],
                    &mut times[1],
                    &mut times[2],
                    &mut times[3],
                ) != 0)
                .then_some(
                    (u64::from(times[0].dwHighDateTime) << 32) | u64::from(times[0].dwLowDateTime),
                )
        }
    }
    fn cpu(h: HANDLE) -> Option<u64> {
        let mut times: [FILETIME; 4] = unsafe { std::mem::zeroed() };
        unsafe {
            (GetProcessTimes(
                h,
                &mut times[0],
                &mut times[1],
                &mut times[2],
                &mut times[3],
            ) != 0)
                .then(|| {
                    times[2..]
                        .iter()
                        .map(|t| (u64::from(t.dwHighDateTime) << 32) | u64::from(t.dwLowDateTime))
                        .sum()
                })
        }
    }
    fn private_bytes(h: HANDLE) -> u64 {
        let mut info: PROCESS_MEMORY_COUNTERS_EX = unsafe { std::mem::zeroed() };
        let size = std::mem::size_of_val(&info) as u32;
        info.cb = size;
        unsafe {
            if GetProcessMemoryInfo(h, &mut info as *mut _ as *mut PROCESS_MEMORY_COUNTERS, size)
                != 0
            {
                info.PrivateUsage as u64
            } else {
                0
            }
        }
    }
    fn steam_over_limit(h: HANDLE) -> bool {
        let mut info: PROCESS_MEMORY_COUNTERS_EX = unsafe { std::mem::zeroed() };
        let size = std::mem::size_of_val(&info) as u32;
        info.cb = size;
        unsafe {
            GetProcessMemoryInfo(h, &mut info as *mut _ as *mut PROCESS_MEMORY_COUNTERS, size) != 0
                && (info.PrivateUsage as u64).max(info.WorkingSetSize as u64) > STEAM_LIMIT
        }
    }
    fn open(pid: u32, access: u32) -> Option<Handle> {
        let h = unsafe { OpenProcess(access, 0, pid) };
        (!h.is_null()).then_some(Handle(h))
    }
    pub fn now() -> u64 {
        let elapsed = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap();
        116_444_736_000_000_000
            + elapsed.as_secs() * TICKS_PER_SECOND
            + u64::from(elapsed.subsec_nanos()) / 100
    }
    #[derive(Deserialize)]
    #[serde(rename_all = "PascalCase")]
    struct Row {
        process_id: u32,
        parent_process_id: u32,
        executable_path: Option<String>,
        command_line: Option<String>,
        creation_date: Option<String>,
        working_set_size: Option<u64>,
    }
    fn wmi_created(value: &str) -> Option<u64> {
        let date =
            chrono::NaiveDateTime::parse_from_str(value.get(..21)?, "%Y%m%d%H%M%S%.f").ok()?;
        let sign = match value.get(21..22)? {
            "+" => 1,
            "-" => -1,
            _ => return None,
        };
        let offset = sign * value.get(22..25)?.parse::<i64>().ok()? * 60;
        let unix_us = (date.and_utc().timestamp() - offset) * 1_000_000
            + i64::from(date.and_utc().timestamp_subsec_micros());
        u64::try_from(unix_us + 11_644_473_600_000_000).ok()
    }
    pub fn scan() -> Result<Vec<Process>> {
        crate::tick_runtime::check()?;
        let _span = crate::tick_runtime::Span::new("helper process inventory");
        let clients = fs::read_to_string("D:/CC/Tools/client-projects.txt")
            .map_err(|_| "client exclusions unreadable")?;
        let exclusions: Vec<_> = clients
            .lines()
            .map(str::trim)
            .filter(|s| !s.is_empty() && !s.starts_with('#'))
            .map(norm)
            .chain(std::iter::once("openmausbot".into()))
            .collect();
        static WMI_IN_FLIGHT: std::sync::LazyLock<std::sync::Arc<std::sync::atomic::AtomicBool>> =
            std::sync::LazyLock::new(|| {
                std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false))
            });
        let rows: Vec<Row> = crate::core::read_only_with_deadline(
            crate::tick_runtime::timeout(std::time::Duration::from_secs(2))?,
            WMI_IN_FLIGHT.clone(), "WMI inventory", || {
                let con = wmi::WMIConnection::new().map_err(std::io::Error::other)?;
                con.raw_query("SELECT ProcessId, ParentProcessId, ExecutablePath, CommandLine, CreationDate, WorkingSetSize FROM Win32_Process")
                    .map_err(std::io::Error::other)
            }).map_err(|e| format!("WMI processes: {e}"))?;
        let rows: BTreeMap<_, _> = rows.into_iter().map(|r| (r.process_id, r)).collect();
        // Toolhelp supplies a complete current PID table, including unreadable processes.
        let snapshot = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) };
        if snapshot == INVALID_HANDLE_VALUE {
            return Err("process snapshot failed".into());
        }
        let _snapshot = Handle(snapshot);
        let mut entry: PROCESSENTRY32W = unsafe { std::mem::zeroed() };
        entry.dwSize = std::mem::size_of_val(&entry) as u32;
        if unsafe { Process32FirstW(snapshot, &mut entry) } == 0 {
            return Err("process snapshot empty".into());
        }
        let mut result = vec![];
        loop {
            crate::tick_runtime::check()?;
            let pid = entry.th32ProcessID;
            let handle = open(pid, PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_VM_READ);
            let created = handle.as_ref().and_then(|h| created(h.0)).unwrap_or(0);
            let n = entry
                .szExeFile
                .iter()
                .position(|c| *c == 0)
                .unwrap_or(entry.szExeFile.len());
            let mut p = Process {
                pid,
                parent: entry.th32ParentProcessID,
                created,
                image: String::from_utf16_lossy(&entry.szExeFile[..n]),
                command: String::new(),
                bytes: 0,
                private_bytes: handle.as_ref().map_or(0, |h| private_bytes(h.0)),
                cpu: handle.as_ref().and_then(|h| cpu(h.0)),
                excluded: false,
            };
            if let Some(r) = rows.get(&pid).filter(|r| {
                r.parent_process_id == p.parent
                    && created != 0
                    && r.creation_date.as_deref().and_then(wmi_created) == Some(created / 10)
            }) {
                p.image = r.executable_path.clone().unwrap_or(p.image);
                p.command = r.command_line.clone().unwrap_or_default();
                p.bytes = r.working_set_size.unwrap_or(0);
            }
            let marker = format!("{} {}", norm(&p.image), norm(&p.command));
            p.excluded = exclusions.iter().any(|s| marker.contains(s));
            result.push(p);
            if unsafe { Process32NextW(snapshot, &mut entry) } == 0 {
                if unsafe { GetLastError() } != ERROR_NO_MORE_FILES {
                    return Err("incomplete process snapshot".into());
                }
                break;
            }
        }
        Ok(result)
    }
    /// Open all members before killing any. Handles pin process identity across PID reuse.
    pub fn pin(members: &[&Process]) -> Option<Vec<Handle>> {
        members
            .iter()
            .map(|p| {
                let h = open(
                    p.pid,
                    PROCESS_QUERY_LIMITED_INFORMATION
                        | PROCESS_VM_READ
                        | PROCESS_TERMINATE
                        | PROCESS_SYNCHRONIZE,
                )?;
                if created(h.0) != Some(p.created) {
                    return None;
                }
                // The final identity-pinned check detects CPU use since revalidation.
                // Steam is governed by memory, so its CPU activity does not block it.
                if name(p) != "steamwebhelper.exe" && cpu(h.0) != p.cpu {
                    return None;
                }
                if name(p) == "steamwebhelper.exe" && !steam_over_limit(h.0) {
                    return None;
                }
                let mut image = vec![0u16; 32768];
                let mut len = image.len() as u32;
                if unsafe { QueryFullProcessImageNameW(h.0, 0, image.as_mut_ptr(), &mut len) } == 0
                    || norm(&String::from_utf16_lossy(&image[..len as usize])) != norm(&p.image)
                {
                    return None;
                }
                Some(h)
            })
            .collect()
    }
    pub fn request(h: &Handle) -> bool {
        unsafe { TerminateProcess(h.0, 0) != 0 }
    }
    pub fn exited(h: &Handle) -> bool {
        unsafe { WaitForSingleObject(h.0, 1000) == WAIT_OBJECT_0 }
    }
}

pub fn run(jobs: &Path, dry_run: bool) -> Result<String> {
    let chrome = cc_chrome::sweep(&jobs.join("jobs"), None, dry_run)
        .map(|s| {
            format!(
                "chrome-tabs: closed={} gone={} kept={} failed={} deferred={}",
                s.closed, s.gone, s.kept, s.failed, s.deferred
            )
        })
        .unwrap_or_else(|e| format!("chrome-tabs: sweep failed: {e}"));
    #[cfg(not(windows))]
    {
        let _ = (jobs, dry_run);
        Err("helper reaper requires Windows".into())
    }
    #[cfg(windows)]
    {
        let roots = job_roots(jobs)?;
        let processes = native::scan()?;
        let now = native::now();
        let path = watch_path(jobs);
        let mut watch = load_watch(&path)?;
        let (confirmed, confirmed_bytes) = if dry_run {
            (0, 0)
        } else {
            watch.reconcile(&processes)
        };
        watch.observe(&processes, now);
        let helper_sets = plan_guarded(&processes, &roots.roots, now, roots.oldest);
        let helper_eligible = helper_sets.len();
        let table: Table = processes.into_iter().map(|p| (p.pid, p)).collect();
        let mut sets: Vec<_> = helper_sets
            .into_iter()
            .filter(|set| {
                set.members
                    .iter()
                    .all(|pid| !watch.requested(&table[pid]) && watch.idle(&table[pid], now))
            })
            .collect();
        let helper_ready = sets.len();
        for p in table
            .values()
            .filter(|p| !watch.requested(p) && steam_target(p, &table))
            .take(10)
        {
            sets.push(Set {
                root: p.pid,
                members: vec![p.pid],
                reason: "steam: over 4 GiB".into(),
            });
        }
        let count: usize = sets.iter().map(|s| s.members.len()).sum();
        let bytes: u64 = sets
            .iter()
            .flat_map(|s| &s.members)
            .map(|p| table[p].bytes.max(table[p].private_bytes))
            .sum();
        if dry_run {
            let helpers = table.values().filter(|p| kind(p).is_some()).count();
            let mut out = format!("dry-run: {count} processes, {:.1} MB, {} sets (limit {LIMIT}); identified_helpers={helpers}, job_roots={}, helper_eligible={helper_eligible}, helper_idle_ready={helper_ready}, steam_over_limit={}", bytes as f64 / 1048576., sets.len(), roots.roots.len(), sets.len() - helper_ready);
            for s in sets {
                out.push_str(&format!("\nset {}: {:?}; {}", s.root, s.members, s.reason));
                for pid in s.members {
                    let p = &table[&pid];
                    out.push_str(&format!(
                        "\n  PID {pid} parent={} created={} image={} ({:.1} MB)",
                        p.parent,
                        p.created,
                        p.image,
                        p.bytes as f64 / 1048576.
                    ));
                }
            }
            return Ok(format!("{chrome}\n{out}"));
        }
        // Establish the observation durably before authorizing any termination.
        save_watch(&path, &watch)?;
        let mut killed = confirmed;
        let mut freed = confirmed_bytes;
        let mut skipped = 0;
        let mut failed = 0;
        let mut scan_error = String::new();
        for s in sets {
            if crate::tick_runtime::check().is_err() {
                break;
            }
            // Re-read job protection and the entire table before each whole set.
            let (fresh_roots, fresh) =
                match job_roots(jobs).and_then(|roots| native::scan().map(|p| (roots, p))) {
                    Ok(value) => value,
                    Err(error) => {
                        scan_error = crate::core::safe_text(&error).replace(['\r', '\n'], " ");
                        break;
                    }
                };
            let steam = s.reason.starts_with("steam:");
            let valid = if steam {
                let fresh_table: Table = fresh.iter().map(|p| (p.pid, p.clone())).collect();
                fresh_table.get(&s.root).is_some_and(|p| {
                    p.created == table[&s.root].created && steam_target(p, &fresh_table)
                })
            } else {
                revalidated(
                    &s,
                    &table,
                    &fresh,
                    &fresh_roots.roots,
                    native::now(),
                    fresh_roots.oldest,
                ) && s.members.iter().all(|pid| {
                    fresh
                        .iter()
                        .find(|p| p.pid == *pid)
                        .is_some_and(|p| watch.idle(p, native::now()))
                })
            };
            if !valid {
                skipped += s.members.len();
                continue;
            }
            let members: Vec<_> = s
                .members
                .iter()
                .filter_map(|pid| fresh.iter().find(|p| p.pid == *pid))
                .collect();
            let Some(handles) = native::pin(&members) else {
                skipped += members.len();
                continue;
            };
            // No taskkill /T, ancestry kill, or recursive process termination anywhere.
            for (p, h) in members.iter().zip(&handles) {
                if crate::tick_runtime::check().is_err() {
                    break;
                }
                if native::request(h) {
                    let exit = Exit {
                        pid: p.pid,
                        created: p.created,
                        bytes: p.bytes.max(p.private_bytes),
                        steam,
                    };
                    watch.waiting.push(exit.clone());
                    // Save accepted requests before waiting. A timeout leaves the
                    // identity for a later complete PID snapshot to confirm once.
                    save_watch(&path, &watch)?;
                    if native::exited(h) {
                        watch
                            .waiting
                            .retain(|e| !(e.pid == exit.pid && e.created == exit.created));
                        watch.confirm(&exit);
                        killed += 1;
                        freed += exit.bytes;
                        save_watch(&path, &watch)?;
                    }
                } else {
                    failed += 1;
                }
            }
        }
        Ok(format!("{chrome}\nhelper-reaper: killed={killed} freed_mb={:.1} planned={count} skipped={skipped} failed={failed} waiting={} scan_failed={}{}", freed as f64 / 1048576., watch.waiting.len(), !scan_error.is_empty(), if scan_error.is_empty() { String::new() } else { format!(" error={scan_error}") }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    const NOW: u64 = 100_000 * TICKS_PER_SECOND;
    fn p(pid: u32, parent: u32, seconds: u64, image: &str, command: &str) -> Process {
        Process {
            pid,
            parent,
            created: seconds * TICKS_PER_SECOND,
            image: image.into(),
            command: command.into(),
            bytes: 1048576,
            private_bytes: 1048576,
            cpu: Some(0),
            excluded: false,
        }
    }
    fn main(pid: u32) -> Process {
        p(
            pid,
            999,
            1,
            "C:/cli/vendor/x86_64-pc-windows-msvc/bin/codex.exe",
            "codex.exe",
        )
    }
    fn cua(pid: u32, parent: u32, seconds: u64) -> Process {
        p(
            pid,
            parent,
            seconds,
            "C:/OpenAI/Codex/runtimes/cua_node/hash/bin/node.exe",
            "node.exe C:/bin/node_modules/@oai/cua-repl/bin/cua-repl.mjs",
        )
    }
    fn repl(pid: u32, parent: u32, seconds: u64) -> Process {
        p(
            pid,
            parent,
            seconds,
            "C:/OpenAI/Codex/runtimes/cua_node/hash/bin/node_repl.exe",
            "node_repl.exe",
        )
    }
    fn kernel(pid: u32, parent: u32, seconds: u64) -> Process {
        p(
            pid,
            parent,
            seconds,
            "C:/OpenAI/Codex/runtimes/cua_node/hash/bin/node.exe",
            "node.exe C:/temp/kernel.js --session-id abc",
        )
    }
    fn duplicates() -> Vec<Process> {
        vec![
            main(1),
            cua(2, 1, 10),
            repl(3, 2, 11),
            kernel(4, 3, 12),
            cua(5, 1, 20),
            repl(6, 5, 21),
        ]
    }
    #[test]
    fn running_job_tree_protected_including_deep_non_helper_ancestors() {
        let mut procs = duplicates();
        procs.push(p(7, 1, 2, "C:/shell.exe", ""));
        procs.push(cua(8, 7, 9));
        assert_eq!(plan(&procs, &[1], NOW), vec![]);
        assert_eq!(plan(&procs, &[999], NOW), vec![]);
    }
    #[test]
    fn job_supervisor_and_codex_roots_protected_while_unrelated_orphan_is_reaped() {
        let mut codex = main(10);
        codex.created = 20 * TICKS_PER_SECOND;
        let procs = vec![
            p(1, 999, 20, "C:/codex_jobs.exe", "supervise"),
            p(2, 1, 21, "C:/cmd.exe", "wrapper"),
            codex,
            cua(3, 2, 30),
            repl(4, 3, 31),
            kernel(5, 4, 32),
            cua(11, 10, 30),
            cua(12, 10, 40),
            cua(20, 888, 10),
            repl(21, 20, 11),
        ];
        let result = plan(&procs, &[1, 10], NOW);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].members, vec![20, 21]);
    }
    #[test]
    fn newest_set_protected_stale_whole_set_killed() {
        let result = plan(&duplicates(), &[], NOW);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].members, vec![2, 3, 4]);
        assert!(result[0].reason.starts_with("stale Cua:"));
    }
    #[test]
    fn true_orphan_whole_set_killed() {
        let result = plan(
            &[cua(2, 999, 10), repl(3, 2, 11), kernel(4, 3, 12)],
            &[],
            NOW,
        );
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].members, vec![2, 3, 4]);
        assert!(result[0].reason.starts_with("orphan:"));
    }
    #[test]
    fn reused_parent_pid_never_misjudged_as_orphan_or_owner() {
        let parent = p(999, 888, 30, "C:/bin/unrelated.exe", "");
        assert_eq!(
            plan(&[cua(2, 999, 10), repl(3, 2, 11), parent], &[], NOW),
            vec![]
        );
        let mut procs = duplicates();
        procs[0].created = 30 * TICKS_PER_SECOND;
        assert_eq!(plan(&procs, &[], NOW), vec![]);
    }
    #[test]
    fn any_recent_or_unknown_member_preserves_whole_set() {
        for created in [0, NOW - GRACE + 1] {
            let mut procs = duplicates();
            procs[2].created = created;
            assert_eq!(plan(&procs, &[], NOW), vec![]);
        }
    }
    #[test]
    fn unregistered_codex_exec_and_js_launcher_never_killed() {
        let mut procs = duplicates();
        procs[0].command = "codex.exe exec --json".into();
        assert_eq!(plan(&procs, &[], NOW), vec![]);
        let js = p(
            10,
            999,
            10,
            "C:/OpenAI/Codex/runtimes/cua_node/hash/bin/node.exe",
            "node C:/bin/codex.js exec",
        );
        assert_eq!(
            plan(&[js, cua(11, 10, 11), cua(12, 10, 12)], &[], NOW),
            vec![]
        );
    }
    #[test]
    fn independent_newest_kind_and_editors_protected() {
        let mut procs = duplicates();
        procs.extend([repl(7, 1, 30), repl(8, 1, 40)]);
        let result = plan(&procs, &[], NOW);
        assert_eq!(
            result.iter().map(|s| s.members.clone()).collect::<Vec<_>>(),
            vec![vec![2, 3, 4], vec![7]]
        );
        for image in [
            "claude.exe",
            "opencode.exe",
            "Zed.exe",
            "Code.exe",
            "Cursor.exe",
        ] {
            procs[0].image = image.into();
            assert_eq!(plan(&procs, &[], NOW), vec![]);
        }
    }
    #[test]
    fn unrelated_node_and_unrecognized_descendants_block_set() {
        let mut procs = duplicates();
        procs.push(p(7, 3, 13, "C:/node.exe", "node server.js"));
        assert_eq!(plan(&procs, &[], NOW), vec![]);
        assert_eq!(
            plan(&[p(2, 999, 10, "C:/node.exe", "node server.js")], &[], NOW),
            vec![]
        );
    }
    #[test]
    fn fifty_pid_limit_never_splits_set() {
        let mut procs = vec![];
        for i in 1..=30 {
            procs.extend([cua(i * 2, 999, 10), repl(i * 2 + 1, i * 2, 11)]);
        }
        let result = plan(&procs, &[], NOW);
        assert_eq!(result.len(), 25);
        assert_eq!(result.iter().map(|s| s.members.len()).sum::<usize>(), 50);
        assert!(result.iter().all(|s| s.members.len() == 2));
    }
    #[test]
    fn job_read_failure_aborts_and_legacy_running_job_is_protected() {
        let root = tempfile::tempdir().unwrap();
        fs::create_dir_all(root.path().join("jobs/a")).unwrap();
        let path = root.path().join("jobs/a/job.json");
        fs::write(&path, r#"{"status":"running","pid":1,"codex_pid":2}"#).unwrap();
        assert_eq!(job_roots(root.path()).unwrap().roots, vec![1, 2]);
        fs::write(&path, "broken").unwrap();
        assert!(job_roots(root.path()).is_err());
    }
    #[test]
    fn recheck_aborts_on_new_job_pid_reuse_or_disappearing_newest_set() {
        let initial = duplicates();
        let table = initial.iter().map(|p| (p.pid, p.clone())).collect();
        let set = plan(&initial, &[], NOW).remove(0);
        assert!(revalidated(&set, &table, &initial, &[], NOW, None));
        assert!(!revalidated(&set, &table, &initial, &[1], NOW, None));
        let mut reused = initial.clone();
        reused[1].created += TICKS_PER_SECOND;
        assert!(!revalidated(&set, &table, &reused, &[], NOW, None));
        let disappeared = initial[..4].to_vec();
        assert!(!revalidated(&set, &table, &disappeared, &[], NOW, None));
        let mut recent_child = initial.clone();
        recent_child.push(kernel(7, 3, 99_999));
        assert!(!revalidated(&set, &table, &recent_child, &[], NOW, None));
    }
    #[test]
    fn desktop_servers_unknown_commands_and_exec_alias_are_not_tuis() {
        for command in [
            "",
            "codex.exe e --json",
            "codex.exe app-server",
            "codex.exe mcp-server",
            "codex.exe exec-server",
            "codex.exe agents",
        ] {
            let mut procs = duplicates();
            procs[0].command = command.into();
            assert_eq!(plan(&procs, &[], NOW), vec![]);
        }
        let mut desktop = duplicates();
        desktop[0].image = "C:/Apps/Codex/Codex.exe".into();
        assert_eq!(plan(&desktop, &[], NOW), vec![]);
    }
    #[test]
    fn vanished_intermediate_parent_does_not_expose_running_job_helper() {
        let procs = vec![
            p(1, 999, 20, "C:/codex_jobs.exe", ""),
            cua(2, 777, 30),
            repl(3, 2, 31),
        ];
        assert_eq!(plan(&procs, &[1], NOW), vec![]);
        // Even when the supervisor PID has been reused, its recorded start protects.
        let mut reused = procs.clone();
        reused[0].created = 40 * TICKS_PER_SECOND;
        assert_eq!(
            plan_guarded(&reused, &[1], NOW, Some(20 * TICKS_PER_SECOND)),
            vec![]
        );
        assert_eq!(plan_guarded(&procs, &[], NOW, Some(0)), vec![]);
    }
    #[test]
    fn idle_requires_two_hours_of_continuous_samples_and_same_cpu() {
        let proc = cua(2, 999, 10);
        let mut watch = Watch::default();
        let begin = NOW;
        watch.observe(std::slice::from_ref(&proc), begin);
        assert!(!watch.idle(&proc, begin));
        for step in 1..=120 {
            let at = begin + step * 60 * TICKS_PER_SECOND;
            watch.observe(std::slice::from_ref(&proc), at);
            assert_eq!(watch.idle(&proc, at), step == 120);
        }
        let mut active = proc.clone();
        active.cpu = Some(1);
        assert!(!watch.idle(&active, begin + GRACE));
        watch.observe(std::slice::from_ref(&active), begin + GRACE);
        assert!(!watch.idle(&active, begin + GRACE));
    }
    #[test]
    fn gaps_unknown_cpu_reuse_and_clock_rollback_reset_observation() {
        for mode in 0..4 {
            let proc = cua(2, 999, 10);
            let mut watch = Watch::default();
            watch.observe(std::slice::from_ref(&proc), NOW);
            let mut changed = proc.clone();
            let at = match mode {
                0 => NOW + GRACE,
                1 => {
                    changed.cpu = None;
                    NOW + TICKS_PER_SECOND
                }
                2 => {
                    changed.created += 1;
                    NOW + TICKS_PER_SECOND
                }
                _ => NOW - TICKS_PER_SECOND,
            };
            watch.observe(std::slice::from_ref(&changed), at);
            assert!(!watch.idle(&changed, at));
        }
    }
    #[test]
    fn known_console_leaf_is_reaped_but_unrelated_console_is_not() {
        let mut procs = duplicates();
        procs.push(p(7, 2, 13, "C:/Windows/system32/conhost.exe", ""));
        procs.push(p(8, 999, 13, "C:/Windows/system32/conhost.exe", ""));
        assert_eq!(plan(&procs, &[], NOW)[0].members, vec![2, 3, 4, 7]);
        procs[6].image = "C:/fake/conhost.exe".into();
        assert_eq!(plan(&procs, &[], NOW), vec![]);
    }
    #[test]
    fn shim_native_group_requires_exact_parent_command_and_protects_bot_and_client() {
        let mut procs = vec![
            main(1),
            p(
                2,
                1,
                10,
                "D:/AI/OpenClawBridgeBackup/stdio-shim-v2.exe",
                "shim D:/CC/bin/godd-native.exe serve",
            ),
            p(3, 2, 11, "D:/CC/bin/godd-native.exe", "godd-native serve"),
            p(
                4,
                1,
                20,
                "D:/AI/OpenClawBridgeBackup/stdio-shim-v2.exe",
                "shim D:/CC/bin/godd-native.exe serve",
            ),
        ];
        assert_eq!(plan(&procs, &[], NOW)[0].members, vec![2, 3]);
        for hint in ["OpenMausBot", "autoselling"] {
            procs[1].command = format!("shim D:/CC/bin/godd-native.exe --cwd D:/{hint}");
            assert_eq!(plan(&procs, &[], NOW), vec![]);
        }
    }
    #[test]
    fn steam_limit_checks_private_memory_and_verified_steam_ancestry() {
        let base = "C:/Program Files (x86)/Steam";
        let steam = p(1, 999, 10, &format!("{base}/steam.exe"), "");
        let mut web = p(
            2,
            1,
            11,
            &format!("{base}/bin/cef/cef.win64/steamwebhelper.exe"),
            "",
        );
        let table = |web: &Process| {
            [steam.clone(), web.clone()]
                .into_iter()
                .map(|p| (p.pid, p))
                .collect()
        };
        web.private_bytes = STEAM_LIMIT;
        assert!(!steam_target(&web, &table(&web)));
        web.private_bytes += 1;
        assert!(steam_target(&web, &table(&web)));
        web.bytes = STEAM_LIMIT + 1;
        web.private_bytes = 0;
        assert!(steam_target(&web, &table(&web)));
        web.created = steam.created - 1;
        assert!(!steam_target(&web, &table(&web)));
        web.created = 11 * TICKS_PER_SECOND;
        web.parent = 888;
        assert!(!steam_target(&web, &table(&web)));
        web.parent = 1;
        web.image = "C:/fake/steamwebhelper.exe".into();
        assert!(!steam_target(&web, &table(&web)));
    }
    #[test]
    fn watch_round_trip_preserves_pending_exits_and_contains_no_commands() {
        let root = tempfile::tempdir().unwrap();
        let path = watch_path(root.path());
        let mut watch = Watch {
            pending_helpers: 3,
            pending_steam: 1,
            pending_bytes: 42,
            ..Watch::default()
        };
        watch.observe(&[cua(2, 999, 10)], NOW);
        save_watch(&path, &watch).unwrap();
        let read = load_watch(&path).unwrap();
        assert_eq!(
            (read.pending_helpers, read.pending_steam, read.pending_bytes),
            (3, 1, 42)
        );
        let raw = fs::read_to_string(&path).unwrap();
        assert!(!raw.contains("command") && !raw.contains("node.exe"));
        fs::write(&path, "broken").unwrap();
        assert!(load_watch(&path).is_err());
    }
    #[test]
    fn delayed_exit_survives_restart_and_is_counted_once_after_pid_reuse() {
        let proc = cua(2, 999, 10);
        let mut watch = Watch {
            waiting: vec![Exit {
                pid: proc.pid,
                created: proc.created,
                bytes: 42,
                steam: false,
            }],
            ..Watch::default()
        };
        assert!(watch.requested(&proc));
        assert_eq!(watch.reconcile(std::slice::from_ref(&proc)), (0, 0));
        let root = tempfile::tempdir().unwrap();
        let path = watch_path(root.path());
        save_watch(&path, &watch).unwrap();
        let mut watch = load_watch(&path).unwrap();
        let mut unknown = proc.clone();
        unknown.created = 0;
        assert_eq!(watch.reconcile(&[unknown]), (0, 0));
        let mut reused = proc.clone();
        reused.created += 1;
        assert_eq!(watch.reconcile(&[reused]), (1, 42));
        save_watch(&path, &watch).unwrap();
        let mut watch = load_watch(&path).unwrap();
        assert_eq!(watch.reconcile(&[]), (0, 0));
        assert_eq!(
            (
                watch.pending_helpers,
                watch.pending_steam,
                watch.pending_bytes
            ),
            (1, 0, 42)
        );
    }
    #[test]
    fn missing_steam_process_confirms_timeout_without_requesting_again() {
        let mut watch = Watch {
            waiting: vec![Exit {
                pid: 7,
                created: 10,
                bytes: STEAM_LIMIT + 1,
                steam: true,
            }],
            ..Watch::default()
        };
        assert_eq!(watch.reconcile(&[]), (1, STEAM_LIMIT + 1));
        assert!(watch.waiting.is_empty());
        assert_eq!(watch.pending_steam, 1);
        assert_eq!(watch.reconcile(&[]), (0, 0));
        assert_eq!(watch.pending_steam, 1);
    }
    #[cfg(windows)]
    #[test]
    fn owned_child_proves_pinned_identity_and_actual_termination() {
        use std::{
            io::BufRead,
            os::windows::io::AsRawHandle,
            os::windows::process::CommandExt,
            process::{Command, Stdio},
        };
        if let Some(ready) = std::env::var_os("CC_MEMORY_TEST_CHILD") {
            fs::write(ready, "ready").unwrap();
            let mut line = String::new();
            std::io::stdin().lock().read_line(&mut line).unwrap();
            return;
        }
        struct Child(std::process::Child);
        impl Drop for Child {
            fn drop(&mut self) {
                let _ = self.0.kill();
                let _ = self.0.wait();
            }
        }
        let root = tempfile::tempdir().unwrap();
        let ready = root.path().join("ready");
        let mut child = Child(Command::new(std::env::current_exe().unwrap())
            .args(["--exact", "helper_reaper::tests::owned_child_proves_pinned_identity_and_actual_termination", "--nocapture"])
            .env("CC_MEMORY_TEST_CHILD", &ready).stdin(Stdio::piped())
            .stdout(Stdio::null()).stderr(Stdio::null()).creation_flags(0x08000000)
            .spawn().unwrap());
        for _ in 0..100 {
            if ready.exists() {
                break;
            }
            std::thread::sleep(std::time::Duration::from_millis(20));
        }
        assert!(ready.exists());
        // Query our owned child directly: identity/termination does not require global WMI access.
        let mut times: [windows_sys::Win32::Foundation::FILETIME; 4] =
            unsafe { std::mem::zeroed() };
        assert_ne!(
            unsafe {
                windows_sys::Win32::System::Threading::GetProcessTimes(
                    child.0.as_raw_handle() as _,
                    &mut times[0],
                    &mut times[1],
                    &mut times[2],
                    &mut times[3],
                )
            },
            0
        );
        let ticks = |t: &windows_sys::Win32::Foundation::FILETIME| {
            (u64::from(t.dwHighDateTime) << 32) | u64::from(t.dwLowDateTime)
        };
        let p = &Process {
            pid: child.0.id(),
            parent: std::process::id(),
            created: ticks(&times[0]),
            image: std::env::current_exe()
                .unwrap()
                .to_string_lossy()
                .into_owned(),
            command: String::new(),
            bytes: 0,
            private_bytes: 0,
            cpu: Some(ticks(&times[2]) + ticks(&times[3])),
            excluded: false,
        };
        assert_ne!(p.created, 0);
        let mut wrong = p.clone();
        wrong.created += 1;
        assert!(native::pin(&[&wrong]).is_none());
        wrong = p.clone();
        wrong.image = "C:/fake.exe".into();
        assert!(native::pin(&[&wrong]).is_none());
        wrong = p.clone();
        wrong.cpu = p.cpu.map(|t| t + 1);
        assert!(native::pin(&[&wrong]).is_none());
        let handles = native::pin(&[p]).expect("idle owned child can be pinned");
        assert!(native::request(&handles[0]));
        assert!(native::exited(&handles[0]));
        assert!(child.0.wait().is_ok());
    }
}
