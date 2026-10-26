//! Completed registered PC-tool work only; no path/owner registration interface.
use super::*;
use cc_jobs::{Context, FileLock};
use std::collections::{BTreeMap, BTreeSet};

const JOBS_ROOT: &str = r"D:\CC\Tools\codex-jobs";
const TODO: &str = r"D:\CC\Tools\todo\todo.json";
const MAX_ROWS: usize = 512;
const MAX_TARGETS: u64 = 4;
const PROFILE_LOCKS: [&str; 3] = [".cargo-lock", ".cargo-build-lock", ".cargo-artifact-lock"];

#[derive(Default)]
pub(super) struct BuildStats {
    inspected: u64,
    candidates: u64,
    candidate_bytes: u64,
    removed_bytes: u64,
    completed: u64,
    absent: u64,
    retained: u64,
    retained_bytes: u64,
    unmeasured: u64,
    partial_deletions: u64,
    bounded: bool,
    cursor_unverified: bool,
    reasons: BTreeMap<&'static str, u64>,
}
impl BuildStats {
    fn retain(&mut self, reason: &'static str, bytes: Option<u64>) {
        self.retained += 1;
        if let Some(n) = bytes {
            self.retained_bytes = self.retained_bytes.saturating_add(n);
        } else {
            self.unmeasured += 1;
        }
        *self.reasons.entry(reason).or_default() += 1;
    }
    pub(super) fn partial(&self) -> bool {
        self.bounded || self.cursor_unverified || self.retained > 0 || self.partial_deletions > 0
    }
    pub(super) fn json(&self) -> Value {
        json!({"scope":"registered_done_cc_pc_tools_target_debug", "inspected":self.inspected,
            "candidates":self.candidates,"candidate_logical_bytes":self.candidate_bytes,
            "actual_removed_bytes":self.removed_bytes,"artifact_sets_verified_absent":self.completed,
            "already_absent_or_empty":self.absent,"retained":self.retained,
            "retained_measured_remaining_bytes":self.retained_bytes,"retained_bytes_unmeasured":self.unmeasured,
            "partial_deletions":self.partial_deletions,"limit_or_deadline":self.bounded,
            "cursor_unverified":self.cursor_unverified,
            "retention_reasons":self.reasons,"profile_lock_scaffolding_retained":true,
            "whole_pc_scan":false})
    }
}

fn cursor_after(ctx: &Context, path: &Path, stats: &mut BuildStats) -> u64 {
    if matches!(fs::symlink_metadata(path),Err(e) if e.kind()==io::ErrorKind::NotFound) {
        return u64::MAX;
    }
    match read(ctx, path, 4096).ok().and_then(|v| v["after"].as_u64()) {
        Some(value) => value,
        None => {
            stats.cursor_unverified = true;
            u64::MAX
        }
    }
}

type Check<T> = Result<T, &'static str>;
fn field<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
fn key(path: &Path) -> String {
    normalized(&path.to_string_lossy()).to_lowercase()
}
fn same(a: &Path, b: &Path) -> bool {
    key(a) == key(b)
}
fn inside(a: &Path, b: &Path) -> bool {
    let (a, b) = (key(a), key(b));
    a == b || b.starts_with(&(a + "\\"))
}
fn absolute(path: &Path) -> bool {
    let text = path.to_string_lossy();
    let bytes = text.as_bytes();
    path.is_absolute()
        && bytes.len() > 3
        && matches!(bytes[0].to_ascii_uppercase(), b'C' | b'D')
        && bytes[1] == b':'
        && matches!(bytes[2], b'\\' | b'/')
        && !text.contains('\0')
        && !text[2..].contains(':')
}

// Pin before opening an existing lock: FileLock's normal create semantics must
// not manufacture ownership, replace a link, or write target files in dry-run.
struct HeldLock {
    _lock: FileLock,
    _pin: Pinned,
    _parents: Vec<Pinned>,
}
fn lock(path: &Path) -> Check<HeldLock> {
    let parents =
        pin_ancestors(path.parent().ok_or("LOCK_UNAVAILABLE")?).map_err(|_| "LOCK_UNAVAILABLE")?;
    let pin = Pinned::open(path, false).map_err(|_| "LOCK_UNAVAILABLE")?;
    if pin.directory() || pin.info.links != 1 {
        return Err("LOCK_UNAVAILABLE");
    }
    let lock = FileLock::try_acquire(path).map_err(|_| "LOCK_BUSY")?;
    Ok(HeldLock {
        _lock: lock,
        _pin: pin,
        _parents: parents,
    })
}

struct Profiles(BTreeMap<String, HeldLock>);
impl Profiles {
    fn load(root: &Path) -> Check<Self> {
        let mut locks = BTreeMap::new();
        for name in PROFILE_LOCKS {
            match fs::symlink_metadata(root.join(name)) {
                Ok(_) => {
                    let held = lock(&root.join(name))?;
                    let info = &held._pin.info;
                    if info.attributes & 3 != 0 || info.size_low != 0 || info.size_high != 0 {
                        return Err("CARGO_PROFILE_LOCK_UNVERIFIED");
                    }
                    locks.insert(name.to_owned(), held);
                }
                Err(e) if e.kind() == io::ErrorKind::NotFound && name != ".cargo-lock" => (),
                Err(_) => return Err("CARGO_PROFILE_LOCK_UNVERIFIED"),
            }
        }
        Ok(Self(locks))
    }
    fn validate(&self, root: &Path) -> Check<()> {
        for name in PROFILE_LOCKS {
            match (self.0.get(name), fs::symlink_metadata(root.join(name))) {
                (None, Err(e)) if e.kind() == io::ErrorKind::NotFound => (),
                (Some(held), Ok(_)) => {
                    let current = Pinned::open(&root.join(name), false)
                        .map_err(|_| "PROFILE_LOCKS_CHANGED")?;
                    if current.directory() || stamp(&current) != stamp(&held._pin) {
                        return Err("PROFILE_LOCKS_CHANGED");
                    }
                }
                _ => return Err("PROFILE_LOCKS_CHANGED"),
            }
        }
        Ok(())
    }
    fn only(&self, root: &Path) -> Check<bool> {
        self.validate(root)?;
        for (n, entry) in fs::read_dir(root)
            .map_err(|_| "CONTENTS_UNVERIFIED")?
            .enumerate()
        {
            if n >= 40_000 {
                return Err("ENTRY_LIMIT");
            }
            let name = entry.map_err(|_| "CONTENTS_UNVERIFIED")?.file_name();
            if !self.0.keys().any(|expected| name == OsStr::new(expected)) {
                return Ok(false);
            }
        }
        Ok(true)
    }
}

fn read(ctx: &Context, path: &Path, max: u64) -> Check<Value> {
    let _parents =
        pin_ancestors(path.parent().ok_or("STATE_UNVERIFIED")?).map_err(|_| "STATE_UNVERIFIED")?;
    let p = Pinned::open(path, false).map_err(|_| "STATE_UNVERIFIED")?;
    if p.directory()
        || p.info.links != 1
        || (u64::from(p.info.size_high) << 32 | u64::from(p.info.size_low)) > max
    {
        return Err("STATE_UNVERIFIED");
    }
    ctx.read_json(path).map_err(|_| "STATE_UNVERIFIED")
}
fn rows(db: &Value) -> Check<&Vec<Value>> {
    db["items"].as_array().ok_or("TODO_UNVERIFIED")
}
fn selected(row: &Value) -> bool {
    row["project"] == "cc-pc-tools-rs"
        && row["status"] == "done"
        && row["id"].as_u64().is_some_and(|x| x > 0)
}
fn path_for(row: &Value) -> Check<PathBuf> {
    let path = PathBuf::from(field(&row["scheduler"], "worktree"));
    let text = path.to_string_lossy();
    if !absolute(&path)
        || text[2..].contains(':')
        || path.components().any(|c| {
            matches!(
                c,
                std::path::Component::ParentDir | std::path::Component::CurDir
            )
        })
    {
        return Err("WORKTREE_BOUNDARY");
    }
    Ok(path)
}
fn job(ctx: &Context, id: &str) -> Check<Value> {
    if !cc_jobs::queue::valid_job_id(id) {
        return Err("JOB_UNVERIFIED");
    }
    let v = read(ctx, &ctx.job_dir(id).join("job.json"), 1024 * 1024)?;
    if v["id"] != id || !v.is_object() {
        return Err("JOB_UNVERIFIED");
    }
    Ok(v)
}
fn live(ctx: &Context, j: &Value) -> Check<bool> {
    for k in ["pid", "codex_pid"] {
        if !j[k].is_null() && j[k].as_u64().is_none_or(|x| x == 0 || x > u32::MAX as u64) {
            return Err("PROCESS_UNVERIFIED");
        }
        if ctx
            .is_ours_checked(j, k)
            .map_err(|_| "PROCESS_UNVERIFIED")?
        {
            return Ok(true);
        }
    }
    Ok(
        !["succeeded", "failed", "cancelled"].contains(&field(j, "status"))
            || j["work_owner_live"] == true,
    )
}
fn original(ctx: &Context, row: &Value, work: &Path) -> Check<(Value, Vec<HeldLock>)> {
    let id = field(&row["scheduler"], "job");
    let ids = row["jobs"]
        .as_array()
        .filter(|ids| !ids.is_empty() && ids.len() <= 1000)
        .ok_or("ORIGIN_UNVERIFIED")?;
    if !ids.iter().any(|j| j == id) {
        return Err("ORIGIN_UNVERIFIED");
    }
    let mut held = Vec::new();
    for jid in ids {
        let jid = jid.as_str().ok_or("ORIGIN_UNVERIFIED")?;
        if !cc_jobs::queue::valid_job_id(jid) {
            return Err("ORIGIN_UNVERIFIED");
        }
        held.push(lock(&ctx.job_dir(jid).join("state.lock"))?);
        let j = job(ctx, jid)?;
        if build_reference(&j, &work.join("target/debug"))
            || same(Path::new(field(&j, "cwd")), work) && relative_build_reference(&j)
        {
            return Err("SAVED_BUILD_REFERENCE_RETAINED");
        }
        if j["todo_id"] != row["id"] || live(ctx, &j)? {
            return Err("ORIGIN_ACTIVE_OR_UNKNOWN");
        }
        let waits = ctx.job_dir(jid).join("resource-waits");
        match pin_ancestors(&waits) {
            Ok(_pins) => {
                let entries = fs::read_dir(&waits).map_err(|_| "RESOURCE_UNVERIFIED")?;
                for (n, e) in entries.enumerate() {
                    if n >= 256 {
                        return Err("RESOURCE_UNVERIFIED");
                    }
                    let m = read(ctx, &e.map_err(|_| "RESOURCE_UNVERIFIED")?.path(), 8192)?;
                    if m["job_id"] != jid
                        || m["pid"].as_u64().is_none_or(|p| p == 0)
                        || ctx
                            .is_ours_checked(&m, "pid")
                            .map_err(|_| "RESOURCE_UNVERIFIED")?
                    {
                        return Err("RESOURCE_HELD");
                    }
                }
            }
            Err(e) if e.kind() == io::ErrorKind::NotFound => (),
            Err(_) => return Err("RESOURCE_UNVERIFIED"),
        }
    }
    let j = job(ctx, id)?;
    if j["status"] != "succeeded"
        || j["exit_code"] != 0
        || cc_jobs::parse_iso(field(&j, "finished_at"))
            .is_none_or(|at| at <= 0. || at > ctx.rt.epoch())
        || !same(Path::new(field(&j, "cwd")), work)
        || !same(Path::new(field(&j["work_owner"], "worktree")), work)
        || j["work_owner"]["branch"] != row["scheduler"]["branch"]
    {
        return Err("ORIGIN_UNVERIFIED");
    }
    Ok((j, held))
}
fn related(row: &Value, other: &Value, work: &Path) -> bool {
    other["id"] != row["id"]
        && other["status"] != "done"
        && (other["wait_on"]
            .as_array()
            .is_some_and(|xs| xs.contains(&row["id"]))
            || other["subwork"]["parent_todo_id"] == row["id"]
            || (!field(&other["scheduler"], "worktree").is_empty()
                && inside(work, Path::new(field(&other["scheduler"], "worktree"))))
            || (!field(&other["subwork"]["owner"], "worktree").is_empty()
                && inside(
                    work,
                    Path::new(field(&other["subwork"]["owner"], "worktree")),
                )))
}
// A saved explicit build-source reference is not a disposable-cache declaration.
// Inspect canonical metadata in memory only; never print its text or open specs.
fn build_reference(value: &Value, debug: &Path) -> bool {
    match value {
        Value::String(s) => {
            let needle = key(debug);
            let text = s.replace('/', "\\").to_lowercase();
            text.match_indices(&needle).any(|(offset, _)| {
                text[offset + needle.len()..]
                    .chars()
                    .next()
                    .is_none_or(|c| {
                        c == '\\'
                            || c.is_whitespace()
                            || matches!(c, '\'' | '"' | '`' | ')' | ']' | '}' | ';' | ',')
                    })
            })
        }
        Value::Array(xs) => xs.iter().any(|v| build_reference(v, debug)),
        Value::Object(xs) => xs.values().any(|v| build_reference(v, debug)),
        _ => false,
    }
}
fn relative_build_reference(value: &Value) -> bool {
    match value {
        Value::String(s) => {
            let text = s.replace('/', "\\").to_lowercase();
            text.match_indices("target\\debug").any(|(offset, _)| {
                let before = &text[..offset];
                let before = before.strip_suffix(".\\").unwrap_or(before);
                let boundary = before.chars().next_back().is_none_or(|c| {
                    c.is_whitespace() || matches!(c, '\'' | '"' | '`' | '(' | '[' | '=')
                });
                boundary
                    && text[offset + 12..].chars().next().is_none_or(|c| {
                        c == '\\'
                            || c.is_whitespace()
                            || matches!(c, '\'' | '"' | '`' | ')' | ']' | ';' | ',')
                    })
            })
        }
        Value::Array(xs) => xs.iter().any(relative_build_reference),
        Value::Object(xs) => xs.values().any(relative_build_reference),
        _ => false,
    }
}
fn state(
    ctx: &Context,
    store: &Path,
    expected: &Value,
    work: &Path,
    origin: &Value,
    until: Instant,
    active: &[String],
) -> Check<()> {
    let db = read(ctx, store, 64 * 1024 * 1024)?;
    let rows = rows(&db)?;
    let matches: Vec<_> = rows.iter().filter(|r| r["id"] == expected["id"]).collect();
    if matches.len() != 1 || matches[0] != expected || !selected(matches[0]) {
        return Err("ORIGINAL_STATE_CHANGED");
    }
    if rows.iter().any(|r| related(expected, r, work)) {
        return Err("DEPENDENT_WORK_RETAINED");
    }
    let debug = work.join("target/debug");
    if build_reference(&db, &debug)
        || rows.iter().any(|row| {
            same(Path::new(field(&row["scheduler"], "worktree")), work)
                && relative_build_reference(row)
        })
        || relative_build_reference(origin)
    {
        return Err("SAVED_BUILD_REFERENCE_RETAINED");
    }
    // The existing owner index was freshly reconciled under the queue lock.
    // Re-read its active/queued records, never terminal prompts/logs/history.
    for id in active {
        if Instant::now() >= until {
            return Err("JOB_SCAN_LIMIT");
        }
        let raw = job(ctx, id);
        let complete = raw.as_ref().is_ok_and(|j| {
            !field(j, "status").is_empty()
                && Path::new(field(j, "cwd")).is_absolute()
                && ["repository", "git_common", "worktree", "branch"]
                    .iter()
                    .all(|key| !field(&j["work_owner"], key).is_empty())
                && Path::new(field(&j["work_owner"], "worktree")).is_absolute()
        });
        let (j, unknown) = match raw {
            Ok(j) if complete => (j, false),
            observed => {
                if let Ok(j) = &observed
                    && (j["todo_id"] == expected["id"]
                        || [field(j, "cwd"), field(&j["work_owner"], "worktree")]
                            .iter()
                            .any(|p| {
                                !p.is_empty()
                                    && (inside(work, Path::new(p)) || inside(Path::new(p), work))
                            }))
                {
                    return Err("OTHER_JOB_ACTIVE_OR_WAITING");
                }
                let claim = ctx
                    .published_job_work_claim(id)
                    .map_err(|_| "OTHER_OWNER_SCOPE_UNVERIFIED")?;
                let owners: Vec<_> = rows
                    .iter()
                    .filter(|row| {
                        row["id"] == claim["todo_id"]
                            && row["jobs"]
                                .as_array()
                                .is_some_and(|ids| ids.iter().any(|j| j == id))
                    })
                    .collect();
                if owners.len() != 1 {
                    return Err("OTHER_OWNER_SCOPE_UNVERIFIED");
                }
                (claim, true)
            }
        };
        if same(Path::new(field(&j, "cwd")), work) && relative_build_reference(&j) {
            return Err("SAVED_BUILD_REFERENCE_RETAINED");
        }
        if build_reference(&j, &debug) {
            return Err("SAVED_BUILD_REFERENCE_RETAINED");
        }
        let owner = &j["work_owner"];
        let touches = j["todo_id"] == expected["id"]
            || j["related_todo_ids"]
                .as_array()
                .is_some_and(|xs| xs.contains(&expected["id"]))
            || [field(&j, "cwd"), field(owner, "worktree")]
                .iter()
                .any(|p| {
                    !p.is_empty() && (inside(work, Path::new(p)) || inside(Path::new(p), work))
                })
            || (!field(owner, "branch").is_empty()
                && owner["branch"] == expected["scheduler"]["branch"]
                && owner["git_common"] == origin["work_owner"]["git_common"]);
        if touches && (unknown || live(ctx, &j)?) {
            return Err("OTHER_JOB_ACTIVE_OR_WAITING");
        }
    }
    Ok(())
}
fn git(ctx: &Context, root: &Path, args: &[&str], until: Instant) -> Check<String> {
    if Instant::now() >= until {
        return Err("DEADLINE");
    }
    let mut argv = vec![
        "git".into(),
        "--no-optional-locks".into(),
        "-C".into(),
        root.to_string_lossy().into_owned(),
    ];
    argv.extend(args.iter().map(|s| (*s).to_owned()));
    let out = ctx
        .rt
        .call(cc_jobs::Call::new(
            argv,
            vec![],
            until
                .saturating_duration_since(Instant::now())
                .as_secs_f64()
                .min(5.),
        ))
        .map_err(|_| "GIT_UNVERIFIED")?;
    if out.code != 0 || out.stdout.len() > 1024 * 1024 {
        return Err("GIT_UNVERIFIED");
    }
    String::from_utf8(out.stdout).map_err(|_| "GIT_UNVERIFIED")
}
fn repository(url: &str) -> Option<String> {
    let path = url
        .trim()
        .strip_prefix("https://github.com/")
        .or_else(|| url.trim().strip_prefix("git@github.com:"))?
        .trim_end_matches('/')
        .trim_end_matches(".git");
    let parts: Vec<_> = path.split('/').collect();
    (parts.len() == 2
        && ["AutoDevJapan", "BM1314", "mashfromband"]
            .iter()
            .any(|o| parts[0].eq_ignore_ascii_case(o))
        && parts[1].eq_ignore_ascii_case("cc-pc-tools-rs"))
    .then(|| path.to_ascii_lowercase())
}
fn source(
    ctx: &Context,
    db: &Value,
    row: &Value,
    j: &Value,
    work: &Path,
    until: Instant,
) -> Check<String> {
    let registered = Path::new(field(j, "registered_cwd"));
    if !absolute(registered)
        || !same(
            registered,
            Path::new(field(&db["projects"], "cc-pc-tools-rs")),
        )
    {
        return Err("REGISTRATION_UNVERIFIED");
    }
    let gitroot = git(ctx, work, &["rev-parse", "--show-toplevel"], until)?;
    if !same(Path::new(gitroot.trim()), work) {
        return Err("WORKTREE_BOUNDARY");
    }
    // The Git pointer/marker itself must not redirect through a reparse point.
    let _marker = Pinned::open(&work.join(".git"), false).map_err(|_| "GIT_BOUNDARY")?;
    let common = git(
        ctx,
        work,
        &["rev-parse", "--path-format=absolute", "--git-common-dir"],
        until,
    )?;
    let _git_pins = pin_ancestors(Path::new(common.trim())).map_err(|_| "GIT_BOUNDARY")?;
    let origin = git(ctx, work, &["remote", "get-url", "origin"], until)?;
    let repo = repository(&origin).ok_or("REPOSITORY_UNVERIFIED")?;
    if j["work_owner"]["repository"] != repo
        || !same(
            Path::new(common.trim()),
            Path::new(field(&j["work_owner"], "git_common")),
        )
    {
        return Err("OWNERSHIP_CHANGED");
    }
    let branch = git(ctx, work, &["symbolic-ref", "--short", "HEAD"], until)?;
    if branch.trim() != field(&row["scheduler"], "branch") {
        return Err("OWNERSHIP_CHANGED");
    }
    let worktrees = git(ctx, work, &["worktree", "list", "--porcelain"], until)?;
    if !worktrees
        .lines()
        .filter_map(|l| l.strip_prefix("worktree "))
        .any(|p| same(Path::new(p), work))
    {
        return Err("WORKTREE_UNREGISTERED");
    }
    if !git(
        ctx,
        work,
        &["status", "--porcelain=v1", "--untracked-files=all"],
        until,
    )?
    .trim()
    .is_empty()
    {
        return Err("SOURCE_NOT_SAVED");
    }
    git(
        ctx,
        work,
        &["merge-base", "--is-ancestor", "HEAD", "@{upstream}"],
        until,
    )
    .map_err(|_| "SOURCE_NOT_PUSHED")?;
    if !git(ctx, work, &["ls-files", "-z", "--", "target/debug"], until)?.is_empty() {
        return Err("TRACKED_BUILD_DATA");
    }
    git(
        ctx,
        work,
        &["check-ignore", "-q", "--", "target/debug/"],
        until,
    )
    .map_err(|_| "BUILD_NOT_IGNORED")?;
    git(ctx, work, &["rev-parse", "HEAD"], until)
}

#[derive(PartialEq, Eq)]
struct Stamp {
    volume: u32,
    id: u64,
    birth: u64,
    written: u64,
    size: u64,
    attributes: u32,
    links: u32,
}
fn stamp(p: &Pinned) -> Stamp {
    let i = &p.info;
    Stamp {
        volume: i.serial,
        id: u64::from(i.index_high) << 32 | u64::from(i.index_low),
        birth: u64::from(i.created.high) << 32 | u64::from(i.created.low),
        written: u64::from(i.written.high) << 32 | u64::from(i.written.low),
        size: u64::from(i.size_high) << 32 | u64::from(i.size_low),
        attributes: i.attributes,
        links: i.links,
    }
}
struct Node {
    path: PathBuf,
    stamp: Stamp,
}
struct Plan {
    nodes: Vec<Node>,
    bytes: u64,
}
struct Artifacts {
    targets: BTreeSet<String>,
    units: BTreeMap<String, BTreeSet<String>>,
    fingerprints: BTreeMap<String, BTreeSet<String>>,
    build_dirs: BTreeSet<String>,
    out_dirs: BTreeSet<String>,
    roots: BTreeSet<String>,
    examples: BTreeSet<String>,
}
fn base36(s: &str, min: usize, max: usize) -> bool {
    (min..=max).contains(&s.len())
        && s.bytes()
            .all(|b| b.is_ascii_digit() || b.is_ascii_lowercase())
}
fn marker_kind(prefix: &str, target: &Value) -> bool {
    let Some(kinds) = target["kind"].as_array() else {
        return false;
    };
    let has = |kind: &str| kinds.iter().any(|v| v == kind);
    match prefix {
        "lib-" | "test-lib-" => ["lib", "rlib", "proc-macro", "dylib", "cdylib", "staticlib"]
            .iter()
            .any(|k| has(k)),
        "bin-" | "test-bin-" => has("bin"),
        "example-" | "test-example-" => has("example"),
        "test-integration-test-" => has("test"),
        "test-bench-" => has("bench"),
        "run-build-script-" | "build-script-" => has("custom-build"),
        _ => false,
    }
}
impl Artifacts {
    fn load(ctx: &Context, work: &Path, until: Instant) -> Check<Self> {
        let mut call = cc_jobs::Call::new(
            vec![
                ctx.rt
                    .which("cargo.exe")
                    .ok_or("CARGO_METADATA_UNVERIFIED")?,
                "metadata".into(),
                "--locked".into(),
                "--offline".into(),
                "--format-version".into(),
                "1".into(),
            ],
            vec![],
            until
                .saturating_duration_since(Instant::now())
                .as_secs_f64()
                .min(10.),
        );
        call.cwd = Some(work.to_path_buf());
        let out = ctx.rt.call(call).map_err(|_| "CARGO_METADATA_UNVERIFIED")?;
        if out.code != 0 || out.stdout.len() > 4 * 1024 * 1024 {
            return Err("CARGO_METADATA_UNVERIFIED");
        }
        let metadata: Value =
            serde_json::from_slice(&out.stdout).map_err(|_| "CARGO_METADATA_UNVERIFIED")?;
        if !same(
            Path::new(field(&metadata, "target_directory")),
            &work.join("target"),
        ) {
            return Err("CARGO_TARGET_CHANGED");
        }
        let packages = metadata["packages"]
            .as_array()
            .ok_or("CARGO_METADATA_UNVERIFIED")?;
        let targets = metadata["packages"]
            .as_array()
            .ok_or("CARGO_METADATA_UNVERIFIED")?
            .iter()
            .flat_map(|p| p["targets"].as_array().into_iter().flatten())
            .filter_map(|t| t["name"].as_str())
            .map(|s| s.replace('-', "_"))
            .collect();
        let fingerprint = work.join("target/debug/.fingerprint");
        let _pins = pin_ancestors(&fingerprint).map_err(|_| "CARGO_FINGERPRINT_UNVERIFIED")?;
        let mut fingerprints: BTreeMap<String, BTreeSet<String>> = BTreeMap::new();
        let mut build_dirs = BTreeSet::new();
        let mut out_dirs = BTreeSet::new();
        let mut roots = BTreeSet::new();
        let mut examples = BTreeSet::new();
        let mut units: BTreeMap<String, BTreeSet<String>> = BTreeMap::new();
        for (count, entry) in fs::read_dir(&fingerprint)
            .map_err(|_| "CARGO_FINGERPRINT_UNVERIFIED")?
            .enumerate()
        {
            if count >= 40_000 || Instant::now() >= until {
                return Err("ENTRY_LIMIT");
            }
            let path = entry.map_err(|_| "CARGO_FINGERPRINT_UNVERIFIED")?.path();
            let p = Pinned::open(&path, false).map_err(|_| "CARGO_FINGERPRINT_UNVERIFIED")?;
            if !p.directory() {
                return Err("CARGO_FINGERPRINT_UNVERIFIED");
            }
            let name = path
                .file_name()
                .and_then(|n| n.to_str())
                .ok_or("CARGO_FINGERPRINT_UNVERIFIED")?;
            let (package, hash) = name
                .rsplit_once('-')
                .ok_or("CARGO_FINGERPRINT_UNVERIFIED")?;
            if hash.len() != 16 || !hash.bytes().all(|b| b.is_ascii_hexdigit()) {
                return Err("CARGO_FINGERPRINT_UNVERIFIED");
            }
            let package_targets: BTreeSet<_> = packages
                .iter()
                .filter(|p| p["name"] == package)
                .flat_map(|p| p["targets"].as_array().into_iter().flatten())
                .filter_map(|t| t["name"].as_str())
                .map(|n| n.replace('-', "_"))
                .collect();
            if package_targets.is_empty() {
                return Err("CARGO_FINGERPRINT_UNVERIFIED");
            }
            // Cargo owns a unit only when its corresponding marker and JSON
            // metadata both exist. An arbitrary file copied into deps is not proof.
            for (entry_count, e) in fs::read_dir(&path)
                .map_err(|_| "CARGO_FINGERPRINT_UNVERIFIED")?
                .enumerate()
            {
                if entry_count >= 256 || Instant::now() >= until {
                    return Err("ENTRY_LIMIT");
                }
                let e = e.map_err(|_| "CARGO_FINGERPRINT_UNVERIFIED")?;
                let n = e.file_name().to_string_lossy().into_owned();
                if n.ends_with(".json")
                    && [
                        "lib-",
                        "bin-",
                        "example-",
                        "test-",
                        "build-script-",
                        "run-build-script-",
                    ]
                    .iter()
                    .any(|prefix| n.starts_with(prefix))
                {
                    let marker = path.join(n.strip_suffix(".json").unwrap());
                    if let (Ok(pin), Ok(metadata)) = (
                        Pinned::open(&marker, false),
                        read(ctx, &e.path(), 256 * 1024),
                    ) {
                        let valid = !pin.directory()
                            && pin.info.links == 1
                            && metadata["rustc"].is_u64()
                            && metadata["deps"].is_array()
                            && metadata["local"].is_array();
                        if valid {
                            let marker = n.strip_suffix(".json").unwrap();
                            for prefix in [
                                "test-integration-test-",
                                "test-example-",
                                "test-bench-",
                                "test-lib-",
                                "test-bin-",
                                "run-build-script-",
                                "build-script-",
                                "lib-",
                                "bin-",
                                "example-",
                            ] {
                                if let Some(unit) = marker.strip_prefix(prefix).filter(|unit| {
                                    package_targets.contains(&unit.replace('-', "_"))
                                        && packages
                                            .iter()
                                            .filter(|p| p["name"] == package)
                                            .flat_map(|p| {
                                                p["targets"].as_array().into_iter().flatten()
                                            })
                                            .any(|t| {
                                                t["name"].as_str().is_some_and(|name| {
                                                    name.replace('-', "_") == unit.replace('-', "_")
                                                }) && marker_kind(prefix, t)
                                            })
                                }) {
                                    let files = fingerprints.entry(name.to_owned()).or_default();
                                    files.extend([
                                        marker.to_owned(),
                                        n.clone(),
                                        format!("dep-{marker}"),
                                        format!("output-{marker}"),
                                        "invoked.timestamp".into(),
                                    ]);
                                    units
                                        .entry(hash.to_ascii_lowercase())
                                        .or_default()
                                        .insert(unit.replace('-', "_"));
                                    let unit_name = unit.replace('-', "_");
                                    if prefix == "bin-" || prefix == "lib-" {
                                        for target in packages
                                            .iter()
                                            .filter(|p| p["name"] == package)
                                            .flat_map(|p| {
                                                p["targets"].as_array().into_iter().flatten()
                                            })
                                            .filter(|t| {
                                                t["name"].as_str().is_some_and(|n| {
                                                    n.replace('-', "_") == unit_name
                                                })
                                            })
                                        {
                                            let Some(raw) = target["name"].as_str() else {
                                                continue;
                                            };
                                            let kinds = target["kind"]
                                                .as_array()
                                                .ok_or("CARGO_METADATA_UNVERIFIED")?;
                                            if prefix == "bin-" && kinds.iter().any(|k| k == "bin")
                                            {
                                                for ext in ["exe", "pdb", "d"] {
                                                    roots.insert(format!("{raw}.{ext}"));
                                                }
                                            }
                                            if prefix == "lib-"
                                                && kinds.iter().any(|k| {
                                                    k == "lib"
                                                        || k == "rlib"
                                                        || k == "proc-macro"
                                                        || k == "cdylib"
                                                        || k == "dylib"
                                                        || k == "staticlib"
                                                })
                                            {
                                                for ext in ["rlib", "rmeta", "d"] {
                                                    roots.insert(format!("lib{unit_name}.{ext}"));
                                                }
                                                roots.insert(format!("{unit_name}.d"));
                                                if kinds.iter().any(|k| {
                                                    k == "proc-macro"
                                                        || k == "cdylib"
                                                        || k == "dylib"
                                                }) {
                                                    for ext in ["dll", "pdb", "lib", "exp"] {
                                                        roots.insert(format!("{unit_name}.{ext}"));
                                                    }
                                                }
                                            }
                                        }
                                    }
                                    if prefix == "example-" || prefix == "test-example-" {
                                        for ext in ["exe", "pdb", "d"] {
                                            examples.insert(format!("{unit_name}.{ext}"));
                                        }
                                    }
                                    if unit == "build-script-build" {
                                        build_dirs.insert(name.to_owned());
                                        if prefix == "run-build-script-" {
                                            let output = work
                                                .join("target/debug/build")
                                                .join(name)
                                                .join("root-output");
                                            let _parents = pin_ancestors(
                                                output.parent().ok_or("CARGO_OUTPUT_UNVERIFIED")?,
                                            )
                                            .map_err(|_| "CARGO_OUTPUT_UNVERIFIED")?;
                                            let file = Pinned::open(&output, false)
                                                .map_err(|_| "CARGO_OUTPUT_UNVERIFIED")?;
                                            if file.directory()
                                                || file.info.links != 1
                                                || file.info.size_high != 0
                                                || file.info.size_low > 4096
                                            {
                                                return Err("CARGO_OUTPUT_UNVERIFIED");
                                            }
                                            let mut bytes = Vec::new();
                                            fs::File::open(&output)
                                                .map_err(|_| "CARGO_OUTPUT_UNVERIFIED")?
                                                .take(4097)
                                                .read_to_end(&mut bytes)
                                                .map_err(|_| "CARGO_OUTPUT_UNVERIFIED")?;
                                            if bytes.len() > 4096 {
                                                return Err("CARGO_OUTPUT_UNVERIFIED");
                                            }
                                            let text = String::from_utf8(bytes)
                                                .map_err(|_| "CARGO_OUTPUT_UNVERIFIED")?;
                                            if !same(
                                                Path::new(text.trim()),
                                                &output.parent().unwrap().join("out"),
                                            ) {
                                                return Err("CARGO_OUTPUT_UNVERIFIED");
                                            }
                                            out_dirs.insert(name.to_owned());
                                        }
                                    }
                                    break;
                                }
                            }
                        }
                    }
                }
            }
        }
        Ok(Self {
            targets,
            units,
            fingerprints,
            build_dirs,
            out_dirs,
            roots,
            examples,
        })
    }
    fn known(&self, relative: &Path, directory: bool) -> bool {
        let parts: Vec<_> = relative
            .iter()
            .map(|x| x.to_string_lossy().into_owned())
            .collect();
        if parts.is_empty() {
            return directory;
        }
        let name = parts.last().unwrap();
        let extension = Path::new(name)
            .extension()
            .and_then(|x| x.to_str())
            .unwrap_or("");
        let stem = Path::new(name)
            .file_stem()
            .and_then(|x| x.to_str())
            .unwrap_or("");
        let unit = |text: &str| {
            text.rsplit_once('-').is_some_and(|(name, hash)| {
                self.units.get(hash).is_some_and(|units| {
                    units.contains(&name.replace('-', "_"))
                        || name
                            .strip_prefix("lib")
                            .is_some_and(|name| units.contains(&name.replace('-', "_")))
                })
            })
        };
        let binary = matches!(
            extension,
            "exe"
                | "dll"
                | "pdb"
                | "d"
                | "rlib"
                | "rmeta"
                | "lib"
                | "exp"
                | "a"
                | "o"
                | "obj"
                | "bc"
        );
        if parts.len() == 1 {
            return if directory {
                ["deps", "build", ".fingerprint", "incremental", "examples"]
                    .contains(&name.as_str())
            } else {
                PROFILE_LOCKS.contains(&name.as_str()) || self.roots.contains(name)
            };
        }
        match parts[0].as_str() {
            "deps" => !directory && parts.len() == 2 && binary && unit(stem),
            "examples" => {
                !directory
                    && parts.len() == 2
                    && binary
                    && (unit(stem) || self.examples.contains(name))
            }
            ".fingerprint" => {
                if parts.len() == 2 {
                    return directory && self.fingerprints.contains_key(name);
                }
                parts.len() == 3
                    && !directory
                    && self
                        .fingerprints
                        .get(&parts[1])
                        .is_some_and(|files| files.contains(name))
            }
            "build" => {
                if parts.len() == 2 {
                    return directory && self.build_dirs.contains(name);
                }
                if !self.build_dirs.contains(&parts[1]) {
                    return false;
                }
                if parts.len() == 3 {
                    return if directory {
                        name == "out" && self.out_dirs.contains(&parts[1])
                    } else {
                        self.out_dirs.contains(&parts[1])
                            && ["output", "stderr", "root-output", "invoked.timestamp"]
                                .contains(&name.as_str())
                            || binary
                                && (stem == "build-script-build"
                                    || stem == "build_script_build" && extension == "pdb"
                                    || stem.starts_with("build_script_build-") && unit(stem))
                    };
                }
                parts[2] == "out"
                    && self.out_dirs.contains(&parts[1])
                    && (directory || matches!(extension, "rs" | "o" | "a" | "lib"))
            }
            "incremental" => {
                let Some((target, key)) = parts[1].rsplit_once('-') else {
                    return false;
                };
                if !base36(key, 13, 13)
                    || !self.targets.contains(target)
                    || !self.units.values().any(|names| names.contains(target))
                {
                    return false;
                }
                if parts.len() == 2 {
                    return directory;
                }
                let session = parts[2].strip_suffix(".lock").unwrap_or(&parts[2]);
                let tokens: Vec<_> = session.split('-').collect();
                let session_base = tokens.len() >= 3
                    && tokens[0] == "s"
                    && base36(tokens[1], 8, 12)
                    && base36(tokens[2], 7, 7);
                if parts.len() == 3 {
                    return session_base
                        && (directory && tokens.len() == 4 && base36(tokens[3], 25, 25)
                            || !directory && tokens.len() == 3 && name.ends_with(".lock"));
                }
                parts.len() == 4
                    && !directory
                    && session_base
                    && tokens.len() == 4
                    && base36(tokens[3], 25, 25)
                    && (matches!(
                        name.as_str(),
                        "dep-graph.bin"
                            | "query-cache.bin"
                            | "work-products.bin"
                            | "metadata.rmeta"
                            | "thin-lto-past-keys.bin"
                    ) || extension == "o" && base36(stem, 25, 25)
                        || name
                            .strip_suffix(".pre-lto.bc")
                            .is_some_and(|stem| base36(stem, 25, 25)))
            }
            _ => false,
        }
    }
}
fn preserved(relative: &Path) -> bool {
    let parts: Vec<_> = relative
        .iter()
        .map(|p| p.to_string_lossy().to_ascii_lowercase())
        .collect();
    let generated = parts.len() >= 4 && parts[0] == "build" && parts[2] == "out";
    parts.iter().enumerate().any(|(i, p)| {
        let generated_source = generated
            && i >= 3
            && matches!(
                Path::new(p).extension().and_then(|x| x.to_str()),
                Some("rs" | "c" | "cpp" | "h")
            );
        !generated_source
            && (policy::retained_name(p)
                || ["receipt.json", "ledger.json", "runtime-manifest.json"].contains(&p.as_str()))
    })
}
fn inventory(
    root: &Path,
    artifacts: &Artifacts,
    budget: &Budget,
    stats: &mut Stats,
) -> Check<Plan> {
    fn visit(
        root: &Path,
        path: &Path,
        plan: &mut Plan,
        artifacts: &Artifacts,
        budget: &Budget,
        stats: &mut Stats,
        depth: usize,
    ) -> Check<()> {
        budget.check(stats).map_err(|_| "DEADLINE_OR_ENTRY_LIMIT")?;
        if depth > 64 || plan.nodes.len() >= 40_000 {
            return Err("ENTRY_LIMIT");
        }
        let rel = path.strip_prefix(root).map_err(|_| "BUILD_BOUNDARY")?;
        if !rel.as_os_str().is_empty() && preserved(rel) {
            return Err("SOURCE_OR_EVIDENCE_RETAINED");
        }
        let p = Pinned::open(path, false).map_err(|_| "LINK_OR_BUSY")?;
        if p.info.links == 0 || p.directory() && p.info.links != 1 || p.info.attributes & 3 != 0 {
            return Err("LINK_OR_ATTRIBUTES");
        }
        if !artifacts.known(rel, p.directory()) {
            return Err("UNKNOWN_BUILD_CONTENT");
        }
        if depth == 1
            && p.directory()
            && !["deps", "build", ".fingerprint", "incremental", "examples"]
                .iter()
                .any(|n| path.file_name() == Some(OsStr::new(n)))
        {
            return Err("UNKNOWN_BUILD_CONTENT");
        }
        if depth == 1
            && !p.directory()
            && !PROFILE_LOCKS
                .iter()
                .any(|n| path.file_name() == Some(OsStr::new(n)))
            && !matches!(
                path.extension().and_then(|e| e.to_str()),
                Some("exe" | "dll" | "pdb" | "d" | "rlib" | "rmeta" | "lib" | "exp" | "a")
            )
        {
            return Err("UNKNOWN_BUILD_CONTENT");
        }
        if depth > 0 && !PROFILE_LOCKS.iter().any(|n| rel == Path::new(n)) {
            if !p.directory() {
                plan.bytes = plan
                    .bytes
                    .checked_add(stamp(&p).size)
                    .ok_or("SIZE_OVERFLOW")?;
            }
            plan.nodes.push(Node {
                path: path.to_path_buf(),
                stamp: stamp(&p),
            });
        }
        if p.directory() {
            for e in fs::read_dir(path).map_err(|_| "CONTENTS_UNVERIFIED")? {
                visit(
                    root,
                    &e.map_err(|_| "CONTENTS_UNVERIFIED")?.path(),
                    plan,
                    artifacts,
                    budget,
                    stats,
                    depth + 1,
                )?;
            }
        }
        Ok(())
    }
    let mut plan = Plan {
        nodes: Vec::new(),
        bytes: 0,
    };
    visit(root, root, &mut plan, artifacts, budget, stats, 0)?;
    let mut groups: BTreeMap<(u32, u64, u64), u32> = BTreeMap::new();
    for node in plan
        .nodes
        .iter()
        .filter(|node| node.stamp.attributes & DIRECTORY == 0)
    {
        *groups
            .entry((node.stamp.volume, node.stamp.id, node.stamp.birth))
            .or_default() += 1;
    }
    plan.bytes = 0;
    let mut seen = BTreeSet::new();
    for node in plan
        .nodes
        .iter()
        .filter(|node| node.stamp.attributes & DIRECTORY == 0)
    {
        let identity = (node.stamp.volume, node.stamp.id, node.stamp.birth);
        if groups.get(&identity) != Some(&node.stamp.links) {
            return Err("LINK_OR_ATTRIBUTES");
        }
        if seen.insert(identity) {
            plan.bytes = plan
                .bytes
                .checked_add(node.stamp.size)
                .ok_or("SIZE_OVERFLOW")?;
        }
    }
    Ok(plan)
}
impl Plan {
    fn hold(&self, budget: &Budget, stats: &mut Stats) -> Check<Vec<Option<Pinned>>> {
        let mut pins: Vec<Option<Pinned>> = self.nodes.iter().map(|_| None).collect();
        // Pin all directory names first. One exclusive handle per physical
        // file then protects every hardlink; duplicate names use metadata only.
        for (index, n) in self
            .nodes
            .iter()
            .enumerate()
            .filter(|(_, n)| n.stamp.attributes & DIRECTORY != 0)
        {
            budget.check(stats).map_err(|_| "DEADLINE_OR_ENTRY_LIMIT")?;
            let p = Pinned::open(&n.path, false).map_err(|_| "RUNTIME_OR_OPEN_FILE")?;
            if stamp(&p) != n.stamp {
                return Err("BUILD_CONTENT_CHANGED");
            }
            pins[index] = Some(p);
        }
        let mut seen = BTreeSet::new();
        for (index, n) in self
            .nodes
            .iter()
            .enumerate()
            .rev()
            .filter(|(_, n)| n.stamp.attributes & DIRECTORY == 0)
        {
            budget.check(stats).map_err(|_| "DEADLINE_OR_ENTRY_LIMIT")?;
            let first = seen.insert((n.stamp.volume, n.stamp.id, n.stamp.birth));
            let p = if first {
                Pinned::open(&n.path, true)
            } else {
                Pinned::attributes(&n.path)
            }
            .map_err(|_| "RUNTIME_OR_OPEN_FILE")?;
            if stamp(&p) != n.stamp {
                return Err("BUILD_CONTENT_CHANGED");
            }
            if first {
                pins[index] = Some(p);
            }
        }
        Ok(pins)
    }
    fn remove(
        &self,
        root: &Path,
        mut pins: Vec<Option<Pinned>>,
        budget: &Budget,
        stats: &mut Stats,
        profiles: &Profiles,
    ) -> Check<()> {
        // Postorder from our complete immutable set of exclusive handles.
        let mut removed_links: BTreeMap<(u32, u64, u64), u32> = BTreeMap::new();
        for n in self.nodes.iter().rev() {
            let held = pins.pop().ok_or("BUILD_CONTENT_CHANGED")?;
            budget.check(stats).map_err(|_| "DEADLINE_OR_ENTRY_LIMIT")?;
            let dir = n.stamp.attributes & DIRECTORY != 0;
            let identity = (n.stamp.volume, n.stamp.id, n.stamp.birth);
            let removed = *removed_links.get(&identity).unwrap_or(&0);
            let remaining = n
                .stamp
                .links
                .checked_sub(removed)
                .ok_or("BUILD_CONTENT_CHANGED")?;
            let p = match held {
                Some(p) => p,
                None => {
                    let p = Pinned::open(&n.path, true).map_err(|_| "RUNTIME_OR_OPEN_FILE")?;
                    let mut current = stamp(&p);
                    current.links = n.stamp.links;
                    if current != n.stamp || p.info.links != remaining {
                        return Err("BUILD_CONTENT_CHANGED");
                    }
                    p
                }
            };
            let p = if dir {
                drop(p);
                let p = Pinned::open(&n.path, true).map_err(|_| "DELETE_INCOMPLETE")?;
                let actual = stamp(&p);
                if actual.volume != n.stamp.volume
                    || actual.id != n.stamp.id
                    || actual.birth != n.stamp.birth
                    || actual.attributes != n.stamp.attributes
                {
                    return Err("BUILD_CONTENT_CHANGED");
                }
                p
            } else {
                p
            };
            let bytes = p
                .remove_verified_links(if dir { 1 } else { remaining })
                .map_err(|_| "DELETE_INCOMPLETE")?;
            if !dir {
                *removed_links.entry(identity).or_default() += 1;
            }
            stats.logical_bytes = stats.logical_bytes.saturating_add(bytes);
            stats.builds.removed_bytes = stats.builds.removed_bytes.saturating_add(bytes);
            if dir {
                stats.removed_directories += 1;
            } else {
                stats.removed_files += 1;
            }
        }
        if self.nodes.iter().any(|n| !matches!(fs::symlink_metadata(&n.path), Err(e) if e.kind()==io::ErrorKind::NotFound)) { return Err("ABSENCE_UNVERIFIED"); }
        if !profiles.only(root)? {
            return Err("ARTIFACT_SET_NOT_EMPTY");
        }
        Ok(())
    }
    fn apply(
        &self,
        root: &Path,
        pins: Vec<Option<Pinned>>,
        budget: &Budget,
        stats: &mut Stats,
        profiles: &Profiles,
    ) {
        let before = stats.removed_files + stats.removed_directories;
        if let Err(e) = profiles
            .validate(root)
            .and_then(|()| self.remove(root, pins, budget, stats, profiles))
        {
            stats.builds.partial_deletions +=
                u64::from(stats.removed_files + stats.removed_directories > before);
            stats.failures += 1;
            // The old plan does not measure added/replaced/otherwise changed data.
            stats.builds.retain(e, None);
        } else {
            stats.builds.completed += 1;
        }
    }
}

#[allow(clippy::too_many_arguments)] // All paths here come from the canonical row, not CLI inputs.
fn candidate(
    ctx: &Context,
    store: &Path,
    expected: &Value,
    work: &Path,
    apply: bool,
    budget: &Budget,
    stats: &mut Stats,
    guard: impl Fn(&Path) -> io::Result<bool>,
) -> Check<()> {
    let _work = pin_ancestors(work).map_err(|_| "WORKTREE_BOUNDARY")?;
    let root = work.join("target/debug");
    // Pin every ancestor before inspecting a target; never resolve a junction away.
    let _target = match pin_ancestors(root.parent().ok_or("BUILD_BOUNDARY")?) {
        Ok(p) => p,
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            stats.builds.absent += 1;
            return Ok(());
        }
        Err(_) => return Err("BUILD_BOUNDARY"),
    };
    match fs::symlink_metadata(&root) {
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            stats.builds.absent += 1;
            return Ok(());
        }
        Err(_) => return Err("BUILD_UNVERIFIED"),
        Ok(_) => (),
    }
    for path in [work, root.as_path()] {
        if guard(path).map_err(|_| "GUARD_UNAVAILABLE")? {
            return Err("GUARD_UNKNOWN_OR_PROTECTED");
        }
    }
    let _root = pin_ancestors(&root).map_err(|_| "BUILD_BOUNDARY")?;
    let gatekey =
        cc_jobs::cache::resolved(root.parent().ok_or("BUILD_BOUNDARY")?).ok_or("BUILD_BOUNDARY")?;
    let _cargo = lock(&cc_jobs::gate::target_lock(&ctx.root, &gatekey))?;
    let profiles = Profiles::load(&root)?;
    if profiles.only(&root)? {
        stats.builds.absent += 1;
        return Ok(());
    }
    let _queue = lock(&ctx.root.join("queue.lock"))?;
    let _jobs = pin_ancestors(&ctx.jobs).map_err(|_| "JOBS_UNVERIFIED")?;
    let active = ctx
        .active_job_ids_snapshot()
        .map_err(|_| "ACTIVE_JOB_INDEX_UNVERIFIED")?;
    let (j, _states) = original(ctx, expected, work)?;
    let _todo = lock(&store.with_file_name("todo.lock"))?;
    let db = read(ctx, store, 64 * 1024 * 1024)?;
    state(ctx, store, expected, work, &j, budget.until, &active)?;
    let head = source(ctx, &db, expected, &j, work, budget.until)?;
    let artifacts = Artifacts::load(ctx, work, budget.until)?;
    let plan = inventory(&root, &artifacts, budget, stats)?;
    if plan.nodes.is_empty() {
        stats.builds.absent += 1;
        return Ok(());
    }
    stats.builds.candidates += 1;
    stats.builds.candidate_bytes = stats.builds.candidate_bytes.saturating_add(plan.bytes);
    let pins = match plan.hold(budget, stats) {
        Ok(p) => p,
        Err(e) => {
            stats.builds.retain(e, None);
            return Ok(());
        }
    };
    if !apply {
        return Ok(());
    }
    // Re-read original state/source after potentially slow enumeration. The
    // queue, state, ToDo, target and Cargo profile locks remain held throughout.
    if let Err(e) = state(ctx, store, expected, work, &j, budget.until, &active).and_then(|()| {
        if source(ctx, &db, expected, &j, work, budget.until)? != head {
            Err("SOURCE_CHANGED")
        } else {
            Ok(())
        }
    }) {
        stats.builds.retain(e, None);
        return Ok(());
    }
    plan.apply(&root, pins, budget, stats, &profiles);
    Ok(())
}

pub(super) fn run(apply: bool, outer: &Budget, stats: &mut Stats) {
    let result = (|| -> Check<()> {
        let mut ctx = Context::installed().map_err(|_| "STATE_UNAVAILABLE")?;
        ctx.root = JOBS_ROOT.into();
        ctx.jobs = ctx.root.join("jobs");
        let store = Path::new(TODO);
        let db = read(&ctx, store, 64 * 1024 * 1024)?;
        let mut candidates: Vec<_> = rows(&db)?.iter().filter(|r| selected(r)).collect();
        candidates.sort_by_key(|r| std::cmp::Reverse(r["id"].as_u64().unwrap()));
        let cursor = Path::new(RUNTIME).join("completed-build-cursor.json");
        let after = cursor_after(&ctx, &cursor, &mut stats.builds);
        // Preserve a damaged original and advance the same finite sweep with a
        // separate numeric position. Neither cursor supplies deletion authority.
        let (after, cursor) = if stats.builds.cursor_unverified {
            let recovery = cursor.with_file_name("completed-build-cursor-recovery.json");
            let mut state = BuildStats::default();
            let after = cursor_after(&ctx, &recovery, &mut state);
            if state.cursor_unverified {
                return Err("CURSOR_RECOVERY_UNVERIFIED");
            }
            (after, recovery)
        } else {
            (after, cursor)
        };
        if !candidates.iter().any(|r| r["id"].as_u64().unwrap() < after) { /* start a new finite pass */
        } else {
            candidates.retain(|r| r["id"].as_u64().unwrap() < after);
        }
        let budget = Budget {
            until: outer.until.min(Instant::now() + Duration::from_secs(90)),
            max_entries: outer.max_entries.min(stats.visited + 180_000),
        };
        let mut last = None;
        for row in candidates.iter().take(MAX_ROWS) {
            if Instant::now() >= budget.until || stats.builds.candidates >= MAX_TARGETS {
                stats.builds.bounded = true;
                break;
            }
            stats.builds.inspected += 1;
            last = row["id"].as_u64();
            let work = match path_for(row) {
                Ok(p) => p,
                Err(e) => {
                    stats.builds.retain(e, None);
                    continue;
                }
            };
            // Missing worktrees/targets are never re-created or counted as reclaimed.
            if matches!(fs::symlink_metadata(&work),Err(e) if e.kind()==io::ErrorKind::NotFound) {
                stats.builds.absent += 1;
                continue;
            }
            if let Err(e) = candidate(
                &ctx,
                store,
                row,
                &work,
                apply,
                &budget,
                stats,
                guard_unknown,
            ) {
                stats.builds.retain(e, None);
            }
        }
        stats.builds.bounded |= candidates.len() > MAX_ROWS;
        if apply && let Some(last) = last {
            // Cursor is scheduling only, never evidence of ownership/completion.
            let tmp = cursor.with_extension(format!("{}.tmp", std::process::id()));
            let mut f = OpenOptions::new()
                .create_new(true)
                .write(true)
                .open(&tmp)
                .map_err(|_| "CURSOR_WRITE_FAILED")?;
            serde_json::to_writer(&mut f, &json!({"after":last}))
                .map_err(|_| "CURSOR_WRITE_FAILED")?;
            f.sync_all().map_err(|_| "CURSOR_WRITE_FAILED")?;
            drop(f);
            fs::rename(tmp, cursor).map_err(|_| "CURSOR_WRITE_FAILED")?;
        }
        Ok(())
    })();
    if let Err(e) = result {
        stats.builds.retain(e, None);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    const FINGERPRINT_JSON: &[u8] = br#"{"rustc":1,"deps":[],"local":[]}"#;

    struct Fixture {
        root: PathBuf,
        work: PathBuf,
        store: PathBuf,
        row: Value,
        origin: Value,
        ctx: Context,
    }
    impl Fixture {
        fn new() -> Self {
            let root = std::env::temp_dir().join(format!(
                "cleanup3942-{}-{}",
                std::process::id(),
                SystemTime::now()
                    .duration_since(UNIX_EPOCH)
                    .unwrap()
                    .as_nanos()
            ));
            let work = root.join("work");
            fs::create_dir_all(&work).unwrap();
            // Hosted Windows TEMP may use an 8.3 spelling. Canonicalize only
            // this newly created fixture, without relaxing production boundaries.
            let root = PathBuf::from(normalized(
                &fs::canonicalize(&root).unwrap().to_string_lossy(),
            ));
            let work = root.join("work");
            let mut ctx = Context::installed().unwrap();
            ctx.root = root.join("tools/codex-jobs");
            ctx.jobs = ctx.root.join("jobs");
            fs::create_dir_all(&ctx.jobs).unwrap();
            fs::write(
                ctx.jobs.join(".queue-index.json"),
                serde_json::to_vec(
                    &json!({"scan_complete":true,"scanned_at":ctx.rt.epoch(),"ids":[]}),
                )
                .unwrap(),
            )
            .unwrap();
            let store = root.join("tools/todo/todo.json");
            fs::create_dir_all(store.parent().unwrap()).unwrap();
            fs::write(ctx.root.join("queue.lock"), []).unwrap();
            fs::write(store.with_file_name("todo.lock"), []).unwrap();
            let until = Instant::now() + Duration::from_secs(30);
            for args in [
                vec!["init", "-b", "scheduler/todo-1-fixture"],
                vec!["config", "user.name", "fixture"],
                vec!["config", "user.email", "fixture@example.invalid"],
            ] {
                git(&ctx, &work, &args, until).unwrap();
            }
            fs::write(work.join(".gitignore"), b"target/\n").unwrap();
            fs::write(
                work.join("Cargo.toml"),
                b"[workspace]\n\n[package]\nname=\"fixture\"\nversion=\"0.1.0\"\n",
            )
            .unwrap();
            fs::create_dir(work.join("src")).unwrap();
            fs::write(work.join("src/main.rs"), b"fn main() {}\n").unwrap();
            fs::write(work.join("src/lib.rs"), b"pub fn fixture() {}\n").unwrap();
            fs::write(
                work.join("Cargo.lock"),
                b"version = 4\n\n[[package]]\nname = \"fixture\"\nversion = \"0.1.0\"\n",
            )
            .unwrap();
            git(&ctx, &work, &["add", "."], until).unwrap();
            git(
                &ctx,
                &work,
                &["-c", "core.hooksPath=NUL", "commit", "-m", "fixture"],
                until,
            )
            .unwrap();
            // Synthetic canonical records only; no network or production ToDo is mutated.
            git(
                &ctx,
                &work,
                &[
                    "remote",
                    "add",
                    "origin",
                    "https://github.com/AutoDevJapan/cc-pc-tools-rs.git",
                ],
                until,
            )
            .unwrap();
            git(
                &ctx,
                &work,
                &[
                    "update-ref",
                    "refs/remotes/origin/scheduler/todo-1-fixture",
                    "HEAD",
                ],
                until,
            )
            .unwrap();
            git(
                &ctx,
                &work,
                &[
                    "branch",
                    "--set-upstream-to=origin/scheduler/todo-1-fixture",
                ],
                until,
            )
            .unwrap();
            let id = "20261026-000000-todo-1";
            let row = json!({"id":1,"project":"cc-pc-tools-rs","status":"done","jobs":[id],"scheduler":{"job":id,"branch":"scheduler/todo-1-fixture","worktree":work}});
            let origin = json!({"id":id,"status":"succeeded","todo_id":1,"cwd":work,"registered_cwd":work,"exit_code":0,"finished_at":"2026-10-26T00:00:00+09:00","work_owner":{"worktree":work,"branch":"scheduler/todo-1-fixture","git_common":work.join(".git"),"repository":"autodevjapan/cc-pc-tools-rs"}});
            fs::create_dir(ctx.job_dir(id)).unwrap();
            fs::write(ctx.job_dir(id).join("state.lock"), []).unwrap();
            fs::write(
                ctx.job_dir(id).join("job.json"),
                serde_json::to_vec(&origin).unwrap(),
            )
            .unwrap();
            fs::write(
                &store,
                serde_json::to_vec(&json!({"projects":{"cc-pc-tools-rs":work},"items":[row]}))
                    .unwrap(),
            )
            .unwrap();
            fs::create_dir_all(work.join("target/debug/deps")).unwrap();
            fs::create_dir_all(work.join("target/debug/.fingerprint/fixture-0123456789abcdef"))
                .unwrap();
            fs::write(
                work.join("target/debug/.fingerprint/fixture-0123456789abcdef/lib-fixture.json"),
                FINGERPRINT_JSON,
            )
            .unwrap();
            fs::write(
                work.join("target/debug/.fingerprint/fixture-0123456789abcdef/lib-fixture"),
                [],
            )
            .unwrap();
            fs::create_dir_all(work.join("target/debug/.fingerprint/fixture-fedcba9876543210"))
                .unwrap();
            fs::write(
                work.join("target/debug/.fingerprint/fixture-fedcba9876543210/bin-fixture.json"),
                FINGERPRINT_JSON,
            )
            .unwrap();
            fs::write(
                work.join("target/debug/.fingerprint/fixture-fedcba9876543210/bin-fixture"),
                [],
            )
            .unwrap();
            fs::create_dir_all(work.join("target/release")).unwrap();
            fs::write(work.join("target/debug/.cargo-lock"), []).unwrap();
            fs::write(
                work.join("target/debug/deps/libfixture-0123456789abcdef.rmeta"),
                [7; 17],
            )
            .unwrap();
            fs::write(work.join("target/release/keep.exe"), [8; 19]).unwrap();
            let target = cc_jobs::cache::resolved(work.join("target")).unwrap();
            fs::write(cc_jobs::gate::target_lock(&ctx.root, &target), []).unwrap();
            Self {
                root,
                work,
                store,
                row,
                origin,
                ctx,
            }
        }
        fn budget(&self) -> Budget {
            Budget {
                until: Instant::now() + Duration::from_secs(30),
                max_entries: 200_000,
            }
        }
        fn artifacts(&self) -> Artifacts {
            let mut artifacts = Artifacts {
                targets: BTreeSet::from(["fixture".into()]),
                fingerprints: BTreeMap::from([(
                    "fixture-0123456789abcdef".into(),
                    BTreeSet::from([
                        "lib-fixture".into(),
                        "lib-fixture.json".into(),
                        "dep-lib-fixture".into(),
                        "output-lib-fixture".into(),
                        "invoked.timestamp".into(),
                    ]),
                )]),
                build_dirs: BTreeSet::new(),
                out_dirs: BTreeSet::new(),
                roots: BTreeSet::from([
                    "fixture.exe".into(),
                    "fixture.pdb".into(),
                    "fixture.d".into(),
                    "libfixture.rmeta".into(),
                    "libfixture.rlib".into(),
                ]),
                examples: BTreeSet::new(),
                units: BTreeMap::from([(
                    "0123456789abcdef".into(),
                    BTreeSet::from(["fixture".into()]),
                )]),
            };
            artifacts.fingerprints.insert(
                "fixture-fedcba9876543210".into(),
                BTreeSet::from([
                    "bin-fixture".into(),
                    "bin-fixture.json".into(),
                    "dep-bin-fixture".into(),
                    "output-bin-fixture".into(),
                    "invoked.timestamp".into(),
                ]),
            );
            artifacts.units.insert(
                "fedcba9876543210".into(),
                BTreeSet::from(["fixture".into()]),
            );
            artifacts
        }
        fn bytes(&self) -> u64 {
            17 + 2 * FINGERPRINT_JSON.len() as u64
        }
        fn run(&self, apply: bool, guard: bool, stats: &mut Stats) -> Check<()> {
            candidate(
                &self.ctx,
                &self.store,
                &self.row,
                &self.work,
                apply,
                &self.budget(),
                stats,
                |_| Ok(guard),
            )
        }
        fn kept(&self) {
            assert!(self.work.join("src/main.rs").is_file());
            assert!(self.work.join(".git/HEAD").is_file());
            assert_eq!(
                fs::read(self.work.join("target/release/keep.exe")).unwrap(),
                vec![8; 19]
            );
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            fs::remove_dir_all(&self.root).unwrap();
        }
    }

    #[test]
    fn actual_candidate_dry_run_then_apply_preserves_source_git_release_and_locks() {
        let f = Fixture::new();
        let mut stats = Stats::default();
        let before = fs::read(&f.store).unwrap();
        let job_before = fs::read(f.ctx.job_dir(field(&f.origin, "id")).join("job.json")).unwrap();
        f.run(false, false, &mut stats).unwrap();
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .is_file()
        );
        assert_eq!(stats.builds.candidate_bytes, f.bytes());
        assert_eq!(stats.builds.removed_bytes, 0);
        let mut apply = Stats::default();
        f.run(true, false, &mut apply).unwrap();
        assert!(!f.work.join("target/debug/deps").exists());
        assert!(f.work.join("target/debug/.cargo-lock").is_file());
        assert_eq!(apply.builds.removed_bytes, f.bytes());
        assert_eq!(apply.builds.completed, 1);
        f.kept();
        assert_eq!(fs::read(&f.store).unwrap(), before);
        assert_eq!(
            fs::read(f.ctx.job_dir(field(&f.origin, "id")).join("job.json")).unwrap(),
            job_before
        );
        let mut repeat = Stats::default();
        f.run(true, false, &mut repeat).unwrap();
        assert_eq!(repeat.builds.removed_bytes, 0);
        assert_eq!(repeat.builds.completed, 0);
    }
    #[test]
    fn unknown_guard_retains_one_target_and_independent_valid_target_still_runs() {
        let a = Fixture::new();
        let b = Fixture::new();
        let mut stats = Stats::default();
        assert_eq!(
            a.run(true, true, &mut stats),
            Err("GUARD_UNKNOWN_OR_PROTECTED")
        );
        b.run(true, false, &mut stats).unwrap();
        assert!(
            a.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        assert_eq!(stats.builds.removed_bytes, b.bytes());
        a.kept();
        b.kept();
    }
    #[test]
    fn active_origin_waiting_sibling_and_original_state_change_retain_actual_files() {
        let f = Fixture::new();
        let until = f.budget().until;
        let mut changed = f.row.clone();
        changed["status"] = json!("doing");
        fs::write(
            &f.store,
            serde_json::to_vec(&json!({"items":[changed]})).unwrap(),
        )
        .unwrap();
        assert_eq!(
            state(&f.ctx, &f.store, &f.row, &f.work, &f.origin, until, &[]),
            Err("ORIGINAL_STATE_CHANGED")
        );
        fs::write(
            &f.store,
            serde_json::to_vec(&json!({"items":[f.row]})).unwrap(),
        )
        .unwrap();
        let id = "20261026-000001-todo-1";
        fs::create_dir(f.ctx.job_dir(id)).unwrap();
        fs::write(
            f.ctx.job_dir(id).join("job.json"),
            serde_json::to_vec(
                &json!({"id":id,"todo_id":1,"status":"queued","cwd":f.root.join("elsewhere")}),
            )
            .unwrap(),
        )
        .unwrap();
        assert_eq!(
            state(
                &f.ctx,
                &f.store,
                &f.row,
                &f.work,
                &f.origin,
                until,
                &[id.to_owned()]
            ),
            Err("OTHER_JOB_ACTIVE_OR_WAITING")
        );
        let mut running = f.origin.clone();
        running["pid"] = json!(std::process::id());
        running["pid_created"] = json!(cc_jobs::proc_created(std::process::id()).unwrap());
        assert!(live(&f.ctx, &running).unwrap());
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        f.kept();
    }
    #[test]
    fn a_saved_runtime_build_source_reference_keeps_the_original_artifacts() {
        let f = Fixture::new();
        let debug = f.work.join("target/debug");
        let proof = json!({"id":2,"status":"done","note":format!("runtime source {}\\fixture.exe",debug.display())});
        fs::write(
            &f.store,
            serde_json::to_vec(&json!({"items":[f.row,proof]})).unwrap(),
        )
        .unwrap();
        assert_eq!(
            state(
                &f.ctx,
                &f.store,
                &f.row,
                &f.work,
                &f.origin,
                f.budget().until,
                &[]
            ),
            Err("SAVED_BUILD_REFERENCE_RETAINED")
        );
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        f.kept();
        assert!(!build_reference(
            &json!(format!("{}-sibling/file", debug.display())),
            &debug
        ));
        assert!(relative_build_reference(&json!(
            "source .\\target\\debug\\fixture.exe"
        )));
        assert!(relative_build_reference(&json!(
            "source target/debug/fixture.exe"
        )));
        assert!(!relative_build_reference(&json!(
            "D:/other/target/debug/fixture.exe"
        )));
        assert!(!relative_build_reference(&json!(
            "target/debug-sibling/fixture.exe"
        )));
        fs::write(&f.store,serde_json::to_vec(&json!({"items":[f.row,{"id":2,"status":"done","scheduler":{"worktree":f.work},"note":"runtime source target/debug/fixture.exe"}]})).unwrap()).unwrap();
        assert_eq!(
            state(
                &f.ctx,
                &f.store,
                &f.row,
                &f.work,
                &f.origin,
                f.budget().until,
                &[]
            ),
            Err("SAVED_BUILD_REFERENCE_RETAINED")
        );
    }
    #[test]
    fn prior_finished_job_relative_build_reference_still_retains_the_resource() {
        let f = Fixture::new();
        let id = "20261025-000001-todo-1";
        let dir = f.ctx.job_dir(id);
        fs::create_dir(&dir).unwrap();
        fs::write(dir.join("state.lock"), []).unwrap();
        fs::write(dir.join("job.json"),serde_json::to_vec(&json!({"id":id,"todo_id":1,"status":"succeeded","cwd":f.work,"last_message":"runtime source target/debug/fixture.exe"})).unwrap()).unwrap();
        let mut row = f.row.clone();
        row["jobs"].as_array_mut().unwrap().push(json!(id));
        fs::write(
            &f.store,
            serde_json::to_vec(&json!({"projects":{"cc-pc-tools-rs":f.work},"items":[row]}))
                .unwrap(),
        )
        .unwrap();
        assert_eq!(
            candidate(
                &f.ctx,
                &f.store,
                &row,
                &f.work,
                true,
                &f.budget(),
                &mut Stats::default(),
                |_| Ok(false)
            ),
            Err("SAVED_BUILD_REFERENCE_RETAINED")
        );
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        f.kept();
    }
    #[test]
    fn a_metadata_target_without_its_kind_marker_does_not_qualify_a_root_output() {
        let f = Fixture::new();
        fs::remove_dir_all(
            f.work
                .join("target/debug/.fingerprint/fixture-fedcba9876543210"),
        )
        .unwrap();
        let output = f.work.join("target/debug/fixture.exe");
        fs::write(&output, b"unique source data").unwrap();
        assert_eq!(
            f.run(true, false, &mut Stats::default()),
            Err("UNKNOWN_BUILD_CONTENT")
        );
        assert_eq!(fs::read(output).unwrap(), b"unique source data");
        f.kept();
    }
    #[test]
    fn prior_finished_job_directory_only_references_are_retained() {
        let f = Fixture::new();
        let id = "20261025-000002-todo-1";
        let dir = f.ctx.job_dir(id);
        fs::create_dir(&dir).unwrap();
        fs::write(dir.join("state.lock"), []).unwrap();
        let mut row = f.row.clone();
        row["jobs"].as_array_mut().unwrap().push(json!(id));
        fs::write(
            &f.store,
            serde_json::to_vec(&json!({"projects":{"cc-pc-tools-rs":f.work},"items":[row]}))
                .unwrap(),
        )
        .unwrap();
        for reference in ["target/debug", "target/debug/", ".\\target\\debug"] {
            fs::write(dir.join("job.json"),serde_json::to_vec(&json!({"id":id,"todo_id":1,"status":"succeeded","cwd":f.work,"last_message":reference})).unwrap()).unwrap();
            assert_eq!(
                candidate(
                    &f.ctx,
                    &f.store,
                    &row,
                    &f.work,
                    true,
                    &f.budget(),
                    &mut Stats::default(),
                    |_| Ok(false)
                ),
                Err("SAVED_BUILD_REFERENCE_RETAINED")
            );
        }
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        f.kept();
    }
    #[test]
    fn dependencies_dirty_source_and_changed_ownership_are_retained() {
        let f = Fixture::new();
        let db = read(&f.ctx, &f.store, 64 * 1024 * 1024).unwrap();
        assert!(related(
            &f.row,
            &json!({"id":2,"status":"doing","wait_on":[1]}),
            &f.work
        ));
        assert!(related(
            &f.row,
            &json!({"id":2,"status":"todo","subwork":{"owner":{"worktree":f.work}}}),
            &f.work
        ));
        let mut wrong = f.origin.clone();
        wrong["work_owner"]["repository"] = json!("outsider/cc-pc-tools-rs");
        assert_eq!(
            source(&f.ctx, &db, &f.row, &wrong, &f.work, f.budget().until),
            Err("OWNERSHIP_CHANGED")
        );
        fs::write(f.work.join("src/main.rs"), b"unsaved source\n").unwrap();
        assert_eq!(
            source(&f.ctx, &db, &f.row, &f.origin, &f.work, f.budget().until),
            Err("SOURCE_NOT_SAVED")
        );
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
    }
    #[test]
    fn saved_but_unpublished_source_is_not_cleanup_qualified() {
        let f = Fixture::new();
        let db = read(&f.ctx, &f.store, 64 * 1024 * 1024).unwrap();
        let until = f.budget().until;
        fs::write(
            f.work.join("src/main.rs"),
            b"fn main() { println!(\"new\"); }\n",
        )
        .unwrap();
        git(&f.ctx, &f.work, &["add", "src/main.rs"], until).unwrap();
        git(
            &f.ctx,
            &f.work,
            &["-c", "core.hooksPath=NUL", "commit", "-m", "unpublished"],
            until,
        )
        .unwrap();
        assert_eq!(
            source(&f.ctx, &db, &f.row, &f.origin, &f.work, until),
            Err("SOURCE_NOT_PUSHED")
        );
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        f.kept();
    }
    #[test]
    fn open_runtime_or_evidence_prevents_any_deletion_and_unknown_size_is_not_zero() {
        let f = Fixture::new();
        let open = fs::File::open(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta"),
        )
        .unwrap();
        let mut stats = Stats::default();
        f.run(true, false, &mut stats).unwrap();
        assert_eq!(stats.builds.completed, 0);
        assert_eq!(stats.builds.removed_bytes, 0);
        assert_eq!(stats.builds.retained_bytes, 0);
        drop(open);
        fs::write(
            f.work.join("target/debug/deps/receipt.json"),
            b"original evidence",
        )
        .unwrap();
        let err = f.run(true, false, &mut stats).unwrap_err();
        assert_eq!(err, "SOURCE_OR_EVIDENCE_RETAINED");
        stats.builds.retain(err, None);
        assert_eq!(stats.builds.unmeasured, 2);
        assert_eq!(stats.builds.retained_bytes, 0);
        f.kept();
    }
    #[test]
    fn same_os_cargo_gate_and_profile_locks_stop_a_concurrent_start() {
        let f = Fixture::new();
        let target = cc_jobs::cache::resolved(f.work.join("target")).unwrap();
        for path in [
            cc_jobs::gate::target_lock(&f.ctx.root, &target),
            f.work.join("target/debug/.cargo-lock"),
        ] {
            let held = lock(&path).unwrap();
            let status = Command::new(std::env::current_exe().unwrap())
                .args([
                    "--exact",
                    "windows::builds::tests::lock_child",
                    "--nocapture",
                ])
                .env("CLEANUP3942_LOCK_CHILD", &path)
                .creation_flags(NO_WINDOW)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .status()
                .unwrap();
            assert!(status.success());
            drop(held);
            assert!(FileLock::try_acquire(&path).is_ok());
        }
    }
    #[test]
    fn lock_child() {
        if let Some(path) = std::env::var_os("CLEANUP3942_LOCK_CHILD") {
            assert!(FileLock::try_acquire(Path::new(&path)).is_err());
        }
    }
    #[test]
    fn a_running_executable_retains_the_entire_build_set() {
        let f = Fixture::new();
        let exe = f.work.join("target/debug/fixture.exe");
        fs::copy(std::env::current_exe().unwrap(), &exe).unwrap();
        let ready = f.root.join("child-ready");
        struct Running(std::process::Child);
        impl Drop for Running {
            fn drop(&mut self) {
                let _ = self.0.kill();
                let _ = self.0.wait();
            }
        }
        let mut child = Running(
            Command::new(&exe)
                .args(["--exact", "windows::builds::tests::runtime_child"])
                .env("CLEANUP3942_RUNTIME_READY", &ready)
                .creation_flags(NO_WINDOW)
                .stdin(Stdio::piped())
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .spawn()
                .unwrap(),
        );
        let until = Instant::now() + Duration::from_secs(10);
        while !ready.exists() && Instant::now() < until {
            thread::sleep(Duration::from_millis(10));
        }
        assert!(ready.exists());
        let mut stats = Stats::default();
        let result = f.run(true, false, &mut stats);
        child.0.stdin.take().unwrap().write_all(&[1]).unwrap();
        assert!(child.0.wait().unwrap().success());
        assert!(result.is_ok());
        assert_eq!(stats.builds.completed, 0);
        assert_eq!(stats.builds.removed_bytes, 0);
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        assert!(exe.exists());
        f.kept();
    }
    #[test]
    fn runtime_child() {
        if let Some(ready) = std::env::var_os("CLEANUP3942_RUNTIME_READY") {
            fs::write(ready, b"ready").unwrap();
            std::io::stdin().read_exact(&mut [0]).unwrap();
        }
    }
    #[test]
    fn replacement_and_partial_failure_do_not_claim_absence_or_full_bytes() {
        let f = Fixture::new();
        let root = f.work.join("target/debug");
        let mut stats = Stats::default();
        let budget = f.budget();
        let plan = inventory(&root, &f.artifacts(), &budget, &mut stats).unwrap();
        fs::write(root.join("deps/libfixture-0123456789abcdef.rmeta"), [1; 18]).unwrap();
        assert!(plan.hold(&budget, &mut stats).is_err());
        let plan = inventory(&root, &f.artifacts(), &budget, &mut stats).unwrap();
        let pins = plan.hold(&budget, &mut stats).unwrap();
        fs::write(root.join("deps/new.bin"), [2; 7]).unwrap();
        let profiles = Profiles::load(&root).unwrap();
        plan.apply(&root, pins, &budget, &mut stats, &profiles);
        assert_eq!(stats.builds.removed_bytes, 18);
        assert_eq!(stats.builds.completed, 0);
        assert_eq!(stats.builds.unmeasured, 1);
        assert_eq!(stats.builds.retained_bytes, 0);
        assert_eq!(fs::read(root.join("deps/new.bin")).unwrap(), vec![2; 7]);
        f.kept();
    }
    #[test]
    fn unknown_children_are_not_adopted_by_extension_or_fingerprint_hash() {
        let f = Fixture::new();
        for name in ["notes.md", "notes-0123456789abcdef.rmeta"] {
            let path = f.work.join("target/debug/deps").join(name);
            fs::write(&path, b"unique original").unwrap();
            assert_eq!(
                inventory(
                    &f.work.join("target/debug"),
                    &f.artifacts(),
                    &f.budget(),
                    &mut Stats::default()
                )
                .err(),
                Some("UNKNOWN_BUILD_CONTENT")
            );
            assert!(
                f.work
                    .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                    .exists()
            );
            fs::remove_file(path).unwrap();
        }
        f.kept();
    }
    #[test]
    fn fingerprint_directory_aliases_and_arbitrary_incremental_objects_are_unknown() {
        let f = Fixture::new();
        let root = f.work.join("target/debug");
        for relative in [
            ".fingerprint/notes-0123456789abcdef/invoked.timestamp",
            ".fingerprint/fixture-0123456789abcdef/lib-notes",
            "incremental/fixture-arbitrary/saved.obj",
            "incremental/fixture-123456789abcd/s-0123456789-0123456-0123456789abcdef012345678/saved.obj",
            "build/notes-0123456789abcdef/out/private.rs",
        ] {
            let path = root.join(relative);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(&path, b"unique data").unwrap();
            assert_eq!(
                inventory(&root, &f.artifacts(), &f.budget(), &mut Stats::default()).err(),
                Some("UNKNOWN_BUILD_CONTENT")
            );
            fs::remove_file(&path).unwrap();
            // Keep only the pre-existing normal Cargo fixture layout.
            let top = relative.split('/').next().unwrap();
            if top == "incremental" || top == "build" {
                fs::remove_dir_all(root.join(top)).unwrap();
            } else if relative.contains("notes-") {
                fs::remove_dir(root.join(".fingerprint/notes-0123456789abcdef")).unwrap();
            }
        }
        f.kept();
    }
    #[test]
    fn only_the_exact_msvc_script_pdb_alias_is_allowed_in_a_verified_build_unit() {
        let f = Fixture::new();
        let root = f.work.join("target/debug");
        let dir = root.join("build/fixture-0123456789abcdef");
        fs::create_dir_all(&dir).unwrap();
        let mut a = f.artifacts();
        a.build_dirs.insert("fixture-0123456789abcdef".into());
        a.units
            .get_mut("0123456789abcdef")
            .unwrap()
            .insert("build_script_build".into());
        fs::write(dir.join("build_script_build-0123456789abcdef.pdb"), [3; 37]).unwrap();
        fs::hard_link(
            dir.join("build_script_build-0123456789abcdef.pdb"),
            dir.join("build_script_build.pdb"),
        )
        .unwrap();
        let budget = f.budget();
        let mut stats = Stats::default();
        let profiles = Profiles::load(&root).unwrap();
        let plan = inventory(&root, &a, &budget, &mut stats).unwrap();
        let pins = plan.hold(&budget, &mut stats).unwrap();
        plan.apply(&root, pins, &budget, &mut stats, &profiles);
        assert_eq!(stats.builds.completed, 1);
        assert_eq!(stats.builds.removed_bytes, f.bytes() + 37);
        assert!(!a.known(
            Path::new("build/notes-0123456789abcdef/build_script_build.pdb"),
            false
        ));
        assert!(!a.known(Path::new("build/fixture-0123456789abcdef/saved.pdb"), false));
        f.kept();
    }
    #[test]
    fn a_new_optional_profile_name_is_never_an_allowed_lock_directory() {
        let f = Fixture::new();
        let root = f.work.join("target/debug");
        let budget = f.budget();
        let mut stats = Stats::default();
        let profiles = Profiles::load(&root).unwrap();
        let plan = inventory(&root, &f.artifacts(), &budget, &mut stats).unwrap();
        let pins = plan.hold(&budget, &mut stats).unwrap();
        fs::create_dir(root.join(".cargo-artifact-lock")).unwrap();
        fs::write(root.join(".cargo-artifact-lock/original.bin"), b"keep").unwrap();
        assert_eq!(profiles.only(&root), Err("PROFILE_LOCKS_CHANGED"));
        plan.apply(&root, pins, &budget, &mut stats, &profiles);
        assert_eq!(stats.builds.completed, 0);
        assert_eq!(stats.builds.removed_bytes, 0);
        assert_eq!(stats.builds.unmeasured, 1);
        assert_eq!(
            fs::read(root.join(".cargo-artifact-lock/original.bin")).unwrap(),
            b"keep"
        );
    }
    #[test]
    fn a_terminal_sibling_with_a_live_pid_keeps_unopened_artifacts() {
        let f = Fixture::new();
        let id = "20261026-000002-todo-2";
        let dir = f.ctx.job_dir(id);
        fs::create_dir(&dir).unwrap();
        let pid = std::process::id();
        fs::write(dir.join("job.json"),serde_json::to_vec(&json!({"id":id,"todo_id":2,"status":"succeeded","cwd":f.work,"pid":pid,"pid_created":f.ctx.rt.proc_created(pid)})).unwrap()).unwrap();
        assert_eq!(
            f.run(true, false, &mut Stats::default()),
            Err("OTHER_JOB_ACTIVE_OR_WAITING")
        );
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
        f.kept();
    }
    #[test]
    fn unreadable_registered_worker_retains_its_published_scope_without_blocking_an_independent_tree()
     {
        let f = Fixture::new();
        let id = "20261026-000002-todo-2";
        let dir = f.ctx.job_dir(id);
        fs::create_dir(&dir).unwrap();
        fs::write(dir.join("job.json"), b"null").unwrap();
        let other = f.root.join("independent");
        for same_scope in [false, true] {
            fs::write(dir.join("job.json"), b"null").unwrap();
            let tree = if same_scope { &f.work } else { &other };
            let claim = json!({"todo_id":2,"work_owner":{"repository":"autodevjapan/cc-pc-tools-rs","git_common":f.work.join(".git"),"worktree":tree,"branch":if same_scope {"scheduler/todo-1-fixture"} else {"scheduler/todo-2-fixture"}}});
            fs::write(f.ctx.jobs.join(".queue-index.json"),serde_json::to_vec(&json!({"scan_complete":true,"scanned_at":f.ctx.rt.epoch(),"ids":[id],"owners":{id:claim}})).unwrap()).unwrap();
            fs::write(
                &f.store,
                serde_json::to_vec(&json!({"items":[f.row,{"id":2,"status":"todo","jobs":[id]}]}))
                    .unwrap(),
            )
            .unwrap();
            let result = state(
                &f.ctx,
                &f.store,
                &f.row,
                &f.work,
                &f.origin,
                f.budget().until,
                &[id.into()],
            );
            if same_scope {
                assert_eq!(result, Err("OTHER_JOB_ACTIVE_OR_WAITING"));
            } else {
                assert_eq!(result, Ok(()));
            }
            assert_eq!(fs::read(dir.join("job.json")).unwrap(), b"null");
            let partial =
                serde_json::to_vec(&json!({"id":id,"todo_id":2,"status":"running","cwd":other}))
                    .unwrap();
            fs::write(dir.join("job.json"), &partial).unwrap();
            let result = state(
                &f.ctx,
                &f.store,
                &f.row,
                &f.work,
                &f.origin,
                f.budget().until,
                &[id.into()],
            );
            if same_scope {
                assert_eq!(result, Err("OTHER_JOB_ACTIVE_OR_WAITING"));
            } else {
                assert_eq!(result, Ok(()));
            }
            assert_eq!(fs::read(dir.join("job.json")).unwrap(), partial);
        }
        f.kept();
    }
    #[test]
    fn cargo_hardlinks_inside_the_plan_are_removed_once_and_external_links_are_retained() {
        let f = Fixture::new();
        let root = f.work.join("target/debug");
        let source = root.join("deps/libfixture-0123456789abcdef.rmeta");
        fs::hard_link(&source, root.join("libfixture.rmeta")).unwrap();
        let mut stats = Stats::default();
        f.run(true, false, &mut stats).unwrap();
        assert_eq!(stats.builds.completed, 1);
        assert_eq!(stats.builds.removed_bytes, f.bytes());
        assert!(!source.exists());
        assert!(!root.join("libfixture.rmeta").exists());
        f.kept();
        let external = Fixture::new();
        let file = external
            .work
            .join("target/debug/deps/libfixture-0123456789abcdef.rmeta");
        fs::hard_link(&file, external.root.join("retained.rmeta")).unwrap();
        assert_eq!(
            external.run(true, false, &mut Stats::default()),
            Err("LINK_OR_ATTRIBUTES")
        );
        assert!(file.exists());
        external.kept();
    }
    #[test]
    fn a_new_root_file_prevents_absence_and_marks_remaining_bytes_unmeasured() {
        let f = Fixture::new();
        let root = f.work.join("target/debug");
        let budget = f.budget();
        let mut stats = Stats::default();
        let plan = inventory(&root, &f.artifacts(), &budget, &mut stats).unwrap();
        let pins = plan.hold(&budget, &mut stats).unwrap();
        fs::write(root.join("new-notes.md"), b"new original").unwrap();
        let profiles = Profiles::load(&root).unwrap();
        plan.apply(&root, pins, &budget, &mut stats, &profiles);
        assert_eq!(stats.builds.completed, 0);
        assert_eq!(stats.builds.partial_deletions, 1);
        assert_eq!(stats.builds.unmeasured, 1);
        assert_eq!(stats.builds.retained_bytes, 0);
        assert_eq!(stats.builds.removed_bytes, f.bytes());
        assert_eq!(
            fs::read(root.join("new-notes.md")).unwrap(),
            b"new original"
        );
        f.kept();
    }
    #[test]
    fn source_paths_boundaries_and_deadline_are_not_bypassed() {
        for path in [
            r"E:\old-data",
            r"C:relative",
            r"\\server\share",
            r"C:\work\..\other",
            r"C:\work:stream",
        ] {
            let mut row = json!({"scheduler":{"worktree":path}});
            assert!(path_for(&row).is_err());
            row["status"] = json!("done");
            assert!(!selected(&row));
        }
        assert!(repository("https://github.com.evil/AutoDevJapan/cc-pc-tools-rs").is_none());
        assert!(repository("https://github.com/other/cc-pc-tools-rs").is_none());
        assert!(!preserved(Path::new("build/pkg/out/generated.rs")));
        assert!(preserved(Path::new("deps/original.rs")));
        let f = Fixture::new();
        let budget = Budget {
            until: Instant::now(),
            max_entries: 100,
        };
        assert_eq!(
            inventory(
                &f.work.join("target/debug"),
                &f.artifacts(),
                &budget,
                &mut Stats::default()
            )
            .err(),
            Some("DEADLINE_OR_ENTRY_LIMIT")
        );
        assert!(
            f.work
                .join("target/debug/deps/libfixture-0123456789abcdef.rmeta")
                .exists()
        );
    }
    #[test]
    fn corrupt_scheduling_cursor_does_not_grant_authority_or_stop_a_fresh_scan() {
        let f = Fixture::new();
        let path = f.root.join("cursor.json");
        let mut stats = BuildStats::default();
        fs::write(&path, b"invalid original cursor").unwrap();
        assert_eq!(cursor_after(&f.ctx, &path, &mut stats), u64::MAX);
        assert!(stats.cursor_unverified);
        assert_eq!(fs::read(&path).unwrap(), b"invalid original cursor");
        fs::write(&path, br#"{"after":42}"#).unwrap();
        let mut stats = BuildStats::default();
        assert_eq!(cursor_after(&f.ctx, &path, &mut stats), 42);
        assert!(!stats.cursor_unverified);
    }
    #[test]
    fn child_junction_and_external_hardlink_are_preserved() {
        let f = Fixture::new();
        let root = f.work.join("target/debug");
        let outside = f.root.join("outside");
        fs::create_dir(&outside).unwrap();
        fs::write(outside.join("original.bin"), [9; 11]).unwrap();
        fs::hard_link(outside.join("original.bin"), root.join("deps/hard.bin")).unwrap();
        assert_eq!(
            inventory(&root, &f.artifacts(), &f.budget(), &mut Stats::default()).err(),
            Some("UNKNOWN_BUILD_CONTENT")
        );
        fs::remove_file(root.join("deps/hard.bin")).unwrap();
        let linked = root.join("deps/linked");
        let status = Command::new("cmd.exe")
            .args(["/c", "mklink", "/J"])
            .arg(linked.to_string_lossy().replace('/', "\\"))
            .arg(outside.to_string_lossy().replace('/', "\\"))
            .creation_flags(NO_WINDOW)
            .stdout(Stdio::null())
            .status()
            .unwrap();
        assert!(status.success());
        assert_eq!(
            inventory(&root, &f.artifacts(), &f.budget(), &mut Stats::default()).err(),
            Some("LINK_OR_BUSY")
        );
        fs::remove_dir(&linked).unwrap();
        assert_eq!(fs::read(outside.join("original.bin")).unwrap(), vec![9; 11]);
        let saved = f.root.join("saved-target");
        fs::rename(f.work.join("target"), &saved).unwrap();
        let status = Command::new("cmd.exe")
            .args(["/c", "mklink", "/J"])
            .arg(f.work.join("target").to_string_lossy().replace('/', "\\"))
            .arg(saved.to_string_lossy().replace('/', "\\"))
            .creation_flags(NO_WINDOW)
            .stdout(Stdio::null())
            .status()
            .unwrap();
        assert!(status.success());
        assert_eq!(
            f.run(true, false, &mut Stats::default()),
            Err("BUILD_BOUNDARY")
        );
        fs::remove_dir(f.work.join("target")).unwrap();
        fs::rename(saved, f.work.join("target")).unwrap();
        f.kept();
    }
}
