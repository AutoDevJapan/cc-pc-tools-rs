//! Completed registered PC-tool work only; no path/owner registration interface.
use super::*;
use cc_jobs::{Context, FileLock};
use std::collections::BTreeMap;

const JOBS_ROOT: &str = r"D:\CC\Tools\codex-jobs";
const TODO: &str = r"D:\CC\Tools\todo\todo.json";
const MAX_ROWS: usize = 512;
const MAX_TARGETS: u64 = 4;
const MAX_JOBS: usize = 20_000;
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
        self.bounded || self.retained > 0 || self.partial_deletions > 0
    }
    pub(super) fn json(&self) -> Value {
        json!({"scope":"registered_done_cc_pc_tools_target_debug", "inspected":self.inspected,
            "candidates":self.candidates,"candidate_logical_bytes":self.candidate_bytes,
            "actual_removed_bytes":self.removed_bytes,"artifact_sets_verified_absent":self.completed,
            "already_absent_or_empty":self.absent,"retained":self.retained,
            "retained_measured_remaining_bytes":self.retained_bytes,"retained_bytes_unmeasured":self.unmeasured,
            "partial_deletions":self.partial_deletions,"limit_or_deadline":self.bounded,
            "retention_reasons":self.reasons,"profile_lock_scaffolding_retained":true,
            "whole_pc_scan":false})
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
fn state(
    ctx: &Context,
    store: &Path,
    expected: &Value,
    work: &Path,
    origin: &Value,
    until: Instant,
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
    // Canonical job metadata only. Do not read prompts, logs, credentials or
    // trust a stale queue cache that could omit a waiting/legacy worker.
    let _jobs = pin_ancestors(&ctx.jobs).map_err(|_| "JOBS_UNVERIFIED")?;
    for (n, e) in fs::read_dir(&ctx.jobs)
        .map_err(|_| "JOBS_UNVERIFIED")?
        .enumerate()
    {
        if n >= MAX_JOBS || Instant::now() >= until {
            return Err("JOB_SCAN_LIMIT");
        }
        let e = e.map_err(|_| "JOBS_UNVERIFIED")?;
        let id = e.file_name().to_string_lossy().into_owned();
        if !cc_jobs::queue::valid_job_id(&id)
            || !e.file_type().map_err(|_| "JOBS_UNVERIFIED")?.is_dir()
        {
            continue;
        }
        let j = job(ctx, &id)?;
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
        if touches && live(ctx, &j)? {
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
fn inventory(root: &Path, budget: &Budget, stats: &mut Stats) -> Check<Plan> {
    fn visit(
        root: &Path,
        path: &Path,
        plan: &mut Plan,
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
        if p.info.links != 1 || p.info.attributes & 3 != 0 {
            return Err("LINK_OR_ATTRIBUTES");
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
    visit(root, root, &mut plan, budget, stats, 0)?;
    Ok(plan)
}
impl Plan {
    fn hold(&self, budget: &Budget, stats: &mut Stats) -> Check<Vec<Pinned>> {
        let mut pins = Vec::with_capacity(self.nodes.len());
        for n in &self.nodes {
            budget.check(stats).map_err(|_| "DEADLINE_OR_ENTRY_LIMIT")?;
            // Directory readers pin names while leaving child metadata APIs usable.
            // Every file is already exclusive before the first deletion.
            let p = Pinned::open(&n.path, n.stamp.attributes & DIRECTORY == 0)
                .map_err(|_| "RUNTIME_OR_OPEN_FILE")?;
            if stamp(&p) != n.stamp {
                return Err("BUILD_CONTENT_CHANGED");
            }
            pins.push(p);
        }
        Ok(pins)
    }
    fn remove(&self, mut pins: Vec<Pinned>, budget: &Budget, stats: &mut Stats) -> Check<()> {
        // Postorder from our complete immutable set of exclusive handles.
        for n in self.nodes.iter().rev() {
            let p = pins.pop().ok_or("BUILD_CONTENT_CHANGED")?;
            budget.check(stats).map_err(|_| "DEADLINE_OR_ENTRY_LIMIT")?;
            let dir = p.directory();
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
            let bytes = p.remove().map_err(|_| "DELETE_INCOMPLETE")?;
            stats.logical_bytes = stats.logical_bytes.saturating_add(bytes);
            stats.builds.removed_bytes = stats.builds.removed_bytes.saturating_add(bytes);
            if dir {
                stats.removed_directories += 1;
            } else {
                stats.removed_files += 1;
            }
        }
        if self.nodes.iter().any(|n| !matches!(fs::symlink_metadata(&n.path), Err(e) if e.kind()==io::ErrorKind::NotFound)) { return Err("ABSENCE_UNVERIFIED"); }
        Ok(())
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
    let mut _profiles = Vec::new();
    for name in PROFILE_LOCKS {
        match fs::symlink_metadata(root.join(name)) {
            Ok(_) => _profiles.push(lock(&root.join(name))?),
            Err(e) if e.kind() == io::ErrorKind::NotFound && name != ".cargo-lock" => (),
            Err(_) => return Err("CARGO_PROFILE_LOCK_UNVERIFIED"),
        }
    }
    let _queue = lock(&ctx.root.join("queue.lock"))?;
    let (j, _states) = original(ctx, expected, work)?;
    let _todo = lock(&store.with_file_name("todo.lock"))?;
    let db = read(ctx, store, 64 * 1024 * 1024)?;
    state(ctx, store, expected, work, &j, budget.until)?;
    let head = source(ctx, &db, expected, &j, work, budget.until)?;
    let plan = inventory(&root, budget, stats)?;
    if plan.nodes.is_empty() {
        stats.builds.absent += 1;
        return Ok(());
    }
    stats.builds.candidates += 1;
    stats.builds.candidate_bytes = stats.builds.candidate_bytes.saturating_add(plan.bytes);
    let pins = match plan.hold(budget, stats) {
        Ok(p) => p,
        Err(e) => {
            stats.builds.retain(e, Some(plan.bytes));
            return Ok(());
        }
    };
    if !apply {
        return Ok(());
    }
    // Re-read original state/source after potentially slow enumeration. The
    // queue, state, ToDo, target and Cargo profile locks remain held throughout.
    if let Err(e) = state(ctx, store, expected, work, &j, budget.until).and_then(|()| {
        if source(ctx, &db, expected, &j, work, budget.until)? != head {
            Err("SOURCE_CHANGED")
        } else {
            Ok(())
        }
    }) {
        stats.builds.retain(e, Some(plan.bytes));
        return Ok(());
    }
    let before = stats.builds.removed_bytes;
    if let Err(e) = plan.remove(pins, budget, stats) {
        stats.builds.partial_deletions += 1;
        stats.failures += 1;
        stats.builds.retain(
            e,
            Some(
                plan.bytes
                    .saturating_sub(stats.builds.removed_bytes - before),
            ),
        );
    } else {
        stats.builds.completed += 1;
    }
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
        let after = match fs::symlink_metadata(&cursor) {
            Err(e) if e.kind() == io::ErrorKind::NotFound => u64::MAX,
            Ok(_) => read(&ctx, &cursor, 4096)?["after"]
                .as_u64()
                .ok_or("CURSOR_UNVERIFIED")?,
            Err(_) => return Err("CURSOR_UNVERIFIED"),
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
            let mut ctx = Context::installed().unwrap();
            ctx.root = root.join("tools/codex-jobs");
            ctx.jobs = ctx.root.join("jobs");
            fs::create_dir_all(&ctx.jobs).unwrap();
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
                b"[package]\nname=\"fixture\"\nversion=\"0.1.0\"\n",
            )
            .unwrap();
            fs::create_dir(work.join("src")).unwrap();
            fs::write(work.join("src/main.rs"), b"fn main() {}\n").unwrap();
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
            fs::create_dir_all(work.join("target/release")).unwrap();
            fs::write(work.join("target/debug/.cargo-lock"), []).unwrap();
            fs::write(work.join("target/debug/deps/cache.bin"), [7; 17]).unwrap();
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
        assert!(f.work.join("target/debug/deps/cache.bin").is_file());
        assert_eq!(stats.builds.candidate_bytes, 17);
        assert_eq!(stats.builds.removed_bytes, 0);
        let mut apply = Stats::default();
        f.run(true, false, &mut apply).unwrap();
        assert!(!f.work.join("target/debug/deps").exists());
        assert!(f.work.join("target/debug/.cargo-lock").is_file());
        assert_eq!(apply.builds.removed_bytes, 17);
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
        assert!(a.work.join("target/debug/deps/cache.bin").exists());
        assert_eq!(stats.builds.removed_bytes, 17);
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
            state(&f.ctx, &f.store, &f.row, &f.work, &f.origin, until),
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
            state(&f.ctx, &f.store, &f.row, &f.work, &f.origin, until),
            Err("OTHER_JOB_ACTIVE_OR_WAITING")
        );
        let mut running = f.origin.clone();
        running["pid"] = json!(std::process::id());
        running["pid_created"] = json!(cc_jobs::proc_created(std::process::id()).unwrap());
        assert!(live(&f.ctx, &running).unwrap());
        assert!(f.work.join("target/debug/deps/cache.bin").exists());
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
        assert!(f.work.join("target/debug/deps/cache.bin").exists());
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
        assert!(f.work.join("target/debug/deps/cache.bin").exists());
        f.kept();
    }
    #[test]
    fn open_runtime_or_evidence_prevents_any_deletion_and_unknown_size_is_not_zero() {
        let f = Fixture::new();
        let open = fs::File::open(f.work.join("target/debug/deps/cache.bin")).unwrap();
        let mut stats = Stats::default();
        f.run(true, false, &mut stats).unwrap();
        assert_eq!(stats.builds.completed, 0);
        assert_eq!(stats.builds.removed_bytes, 0);
        assert_eq!(stats.builds.retained_bytes, 17);
        drop(open);
        fs::write(
            f.work.join("target/debug/deps/receipt.json"),
            b"original evidence",
        )
        .unwrap();
        let err = f.run(true, false, &mut stats).unwrap_err();
        assert_eq!(err, "SOURCE_OR_EVIDENCE_RETAINED");
        stats.builds.retain(err, None);
        assert_eq!(stats.builds.unmeasured, 1);
        assert_eq!(stats.builds.retained_bytes, 17);
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
        let exe = f.work.join("target/debug/fixture-runtime.exe");
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
        assert!(f.work.join("target/debug/deps/cache.bin").exists());
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
        let plan = inventory(&root, &budget, &mut stats).unwrap();
        fs::write(root.join("deps/cache.bin"), [1; 18]).unwrap();
        assert!(plan.hold(&budget, &mut stats).is_err());
        let plan = inventory(&root, &budget, &mut stats).unwrap();
        let pins = plan.hold(&budget, &mut stats).unwrap();
        fs::write(root.join("deps/new.bin"), [2; 7]).unwrap();
        assert_eq!(
            plan.remove(pins, &budget, &mut stats),
            Err("DELETE_INCOMPLETE")
        );
        assert_eq!(stats.builds.removed_bytes, 18);
        assert_eq!(stats.builds.completed, 0);
        assert_eq!(fs::read(root.join("deps/new.bin")).unwrap(), vec![2; 7]);
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
            inventory(&f.work.join("target/debug"), &budget, &mut Stats::default()).err(),
            Some("DEADLINE_OR_ENTRY_LIMIT")
        );
        assert!(f.work.join("target/debug/deps/cache.bin").exists());
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
            inventory(&root, &f.budget(), &mut Stats::default()).err(),
            Some("LINK_OR_ATTRIBUTES")
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
            inventory(&root, &f.budget(), &mut Stats::default()).err(),
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
