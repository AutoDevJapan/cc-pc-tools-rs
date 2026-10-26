use crate::policy::{self, C_DELETE_ROOT, D_DELETE_ROOT};
use serde_json::{Value, json};
use std::{
    ffi::OsStr,
    fs::{self, OpenOptions},
    io::{self, Read, Write},
    os::windows::{ffi::OsStrExt, fs::OpenOptionsExt, io::AsRawHandle, process::CommandExt},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    ptr, thread,
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};

const RUNTIME: &str = r"D:\CC\Tools\cleanup-8h";
const SCRUB: &str = r"D:\CC\Tools\client-records\scrub391.exe";
const READ_ATTRIBUTES: u32 = 0x80;
const DELETE: u32 = 0x10000;
const REPARSE: u32 = 0x400;
const DIRECTORY: u32 = 0x10;
const NO_WINDOW: u32 = 0x08000000;

#[path = "owned.rs"]
mod owned;

#[path = "builds.rs"]
mod builds;

#[repr(C)]
#[derive(Default)]
struct FileTime {
    low: u32,
    high: u32,
}
#[repr(C)]
#[derive(Default)]
struct FileInfo {
    attributes: u32,
    created: FileTime,
    accessed: FileTime,
    written: FileTime,
    serial: u32,
    size_high: u32,
    size_low: u32,
    links: u32,
    index_high: u32,
    index_low: u32,
}
#[repr(C)]
struct Disposition {
    delete_file: i32,
}
#[repr(C)]
struct TokenUser {
    sid: *mut u8,
    attributes: u32,
}

#[link(name = "kernel32")]
unsafe extern "system" {
    fn CreateFileW(
        path: *const u16,
        access: u32,
        share: u32,
        security: *const u8,
        disposition: u32,
        flags: u32,
        template: isize,
    ) -> isize;
    fn GetFileInformationByHandle(handle: isize, info: *mut FileInfo) -> i32;
    fn GetFinalPathNameByHandleW(handle: isize, path: *mut u16, length: u32, flags: u32) -> u32;
    fn GetLongPathNameW(path: *const u16, expanded: *mut u16, length: u32) -> u32;
    #[cfg(test)]
    fn GetShortPathNameW(path: *const u16, short: *mut u16, length: u32) -> u32;
    fn SetFileInformationByHandle(
        handle: isize,
        class: u32,
        info: *const Disposition,
        length: u32,
    ) -> i32;
    fn CloseHandle(handle: isize) -> i32;
    fn GetDiskFreeSpaceExW(
        path: *const u16,
        available: *mut u64,
        total: *mut u64,
        free: *mut u64,
    ) -> i32;
    fn GetCurrentProcess() -> isize;
    fn LocalFree(memory: isize) -> isize;
}
#[link(name = "advapi32")]
unsafe extern "system" {
    fn OpenProcessToken(process: isize, access: u32, token: *mut isize) -> i32;
    fn GetTokenInformation(
        token: isize,
        class: u32,
        info: *mut u8,
        length: u32,
        returned: *mut u32,
    ) -> i32;
    fn ConvertSidToStringSidW(sid: *const u8, text: *mut *mut u16) -> i32;
}

struct Handle(isize);
impl Drop for Handle {
    fn drop(&mut self) {
        unsafe {
            CloseHandle(self.0);
        }
    }
}
struct Pinned {
    handle: Handle,
    info: FileInfo,
}
impl Pinned {
    fn open(path: &Path, deleting: bool) -> io::Result<Self> {
        if !path.is_absolute() {
            return Err(invalid());
        }
        let name = wide(path.as_os_str());
        // OPEN_REPARSE_POINT rejects rather than follows links. Ancestors are
        // held without SHARE_DELETE; files being deleted also deny writers.
        let raw = unsafe {
            CreateFileW(
                name.as_ptr(),
                READ_ATTRIBUTES | 1 | if deleting { DELETE } else { 0 },
                if deleting { 0 } else { 3 },
                ptr::null(),
                3,
                0x02000000 | 0x00200000,
                0,
            )
        };
        if raw == -1 {
            return Err(io::Error::last_os_error());
        }
        let handle = Handle(raw);
        let mut info = FileInfo::default();
        if unsafe { GetFileInformationByHandle(handle.0, &mut info) } == 0 {
            return Err(io::Error::last_os_error());
        }
        if info.attributes & REPARSE != 0 {
            return Err(invalid());
        }
        let mut final_name = vec![0u16; 32_768];
        let length = unsafe {
            GetFinalPathNameByHandleW(
                handle.0,
                final_name.as_mut_ptr(),
                final_name.len() as u32,
                0,
            )
        } as usize;
        if length == 0 || length >= final_name.len() {
            return Err(invalid());
        }
        let physical = String::from_utf16(&final_name[..length]).map_err(|_| invalid())?;
        // Expand only the spelling of existing input components. This API
        // retains junction names; the handle's final path must still match,
        // and every ancestor is separately pinned without following reparse points.
        let mut expanded_name = vec![0u16; 32_768];
        let expanded_length = unsafe {
            GetLongPathNameW(
                name.as_ptr(),
                expanded_name.as_mut_ptr(),
                expanded_name.len() as u32,
            )
        } as usize;
        if expanded_length == 0 || expanded_length >= expanded_name.len() {
            return Err(invalid());
        }
        let expected =
            String::from_utf16(&expanded_name[..expanded_length]).map_err(|_| invalid())?;
        if !normalized(&physical).eq_ignore_ascii_case(&normalized(&expected)) {
            return Err(invalid());
        }
        Ok(Self { handle, info })
    }
    fn directory(&self) -> bool {
        self.info.attributes & DIRECTORY != 0
    }
    fn remove(self) -> io::Result<u64> {
        if !self.directory() && self.info.links > 1 {
            return Err(invalid());
        }
        let disposition = Disposition { delete_file: 1 };
        if unsafe {
            SetFileInformationByHandle(
                self.handle.0,
                4,
                &disposition,
                std::mem::size_of::<Disposition>() as u32,
            )
        } == 0
        {
            return Err(io::Error::last_os_error());
        }
        let bytes = if self.directory() {
            0
        } else {
            (u64::from(self.info.size_high) << 32) | u64::from(self.info.size_low)
        };
        drop(self); // Finalize removal before recording a successful file.
        Ok(bytes)
    }
}

fn normalized(path: &str) -> String {
    path.strip_prefix(r"\\?\")
        .unwrap_or(path)
        .replace('/', "\\")
        .trim_end_matches('\\')
        .to_owned()
}
fn pin_ancestors(path: &Path) -> io::Result<Vec<Pinned>> {
    let mut result = Vec::new();
    let ancestors: Vec<_> = path
        .ancestors()
        .filter(|p| !p.as_os_str().is_empty())
        .collect();
    for ancestor in ancestors.into_iter().rev() {
        let pinned = Pinned::open(ancestor, false)?;
        if !pinned.directory() {
            return Err(invalid());
        }
        result.push(pinned);
    }
    Ok(result)
}

#[derive(Default)]
struct Stats {
    builds: builds::BuildStats,
    candidates: u64,
    removed_files: u64,
    removed_directories: u64,
    logical_bytes: u64,
    retained_recovery_or_source: u64,
    retained_unknown: u64,
    retained_links_or_busy: u64,
    recycle_metadata_retained: u64,
    failures: u64,
    expired: bool,
    visited: u64,
    owned_candidates: u64,
    owned_candidate_bytes: u64,
    owned_removed_bytes: u64,
    owned_completed: u64,
    owned_retained: u64,
    owned_finish_failed: u64,
    owned_retained_unmeasured: u64,
    owned_retained_bytes: u64,
    owned_reasons: std::collections::BTreeMap<String, u64>,
}
struct Budget {
    until: Instant,
    max_entries: u64,
}
impl Budget {
    fn check(&self, stats: &mut Stats) -> io::Result<()> {
        if Instant::now() >= self.until || stats.visited >= self.max_entries {
            stats.expired = true;
            return Err(io::Error::new(io::ErrorKind::TimedOut, "cleanup deadline"));
        }
        stats.visited += 1;
        Ok(())
    }
}

fn retention_tree(
    path: &Path,
    budget: &Budget,
    stats: &mut Stats,
    depth: usize,
) -> io::Result<bool> {
    budget.check(stats)?;
    if depth > 64 {
        return Err(invalid());
    }
    if policy::retained_name(&path.file_name().unwrap_or_default().to_string_lossy()) {
        return Ok(true);
    }
    let pinned = Pinned::open(path, false)?;
    if !pinned.directory() {
        return Ok(pinned.info.links > 1);
    }
    for entry in fs::read_dir(path)? {
        if retention_tree(&entry?.path(), budget, stats, depth + 1)? {
            return Ok(true);
        }
    }
    Ok(false)
}

fn delete_tree(
    root: &Path,
    path: &Path,
    apply: bool,
    budget: &Budget,
    stats: &mut Stats,
    depth: usize,
) -> io::Result<()> {
    budget.check(stats)?;
    if depth > 64 || !policy::strict_child(root, path) {
        return Err(invalid());
    }
    if policy::retained_name(&path.file_name().unwrap_or_default().to_string_lossy()) {
        return Err(invalid());
    }
    let pinned = Pinned::open(path, false)?;
    let identity = (
        pinned.info.serial,
        pinned.info.index_high,
        pinned.info.index_low,
    );
    if pinned.directory() {
        for entry in fs::read_dir(path)? {
            delete_tree(root, &entry?.path(), apply, budget, stats, depth + 1)?;
        }
    }
    if apply {
        let directory = pinned.directory();
        drop(pinned);
        // Exclusive deletion is acquired after enumeration has closed its own
        // reader. Recheck identity after the upgrade, never remove a replacement.
        let deleting = Pinned::open(path, true)?;
        if identity
            != (
                deleting.info.serial,
                deleting.info.index_high,
                deleting.info.index_low,
            )
            || directory != deleting.directory()
        {
            return Err(invalid());
        }
        let bytes = deleting.remove()?;
        stats.logical_bytes = stats.logical_bytes.saturating_add(bytes);
        if directory {
            stats.removed_directories += 1;
        } else {
            stats.removed_files += 1;
        }
    }
    Ok(())
}

// A single fixed guard executable is run without a console, with bounded
// output to a file (no inherited stdout pipe can extend the deadline).
struct OwnedCapture(PathBuf);
impl Drop for OwnedCapture {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

fn guard_unknown(path: &Path) -> io::Result<bool> {
    let capture = Path::new(RUNTIME).join(format!(
        "guard-{}-{}.tmp",
        std::process::id(),
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos()
    ));
    let file = OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(&capture)?;
    let _capture_cleanup = OwnedCapture(capture.clone());
    let mut child = Command::new(SCRUB)
        .arg("--guard-path")
        .arg(path)
        .creation_flags(NO_WINDOW)
        .stdin(Stdio::null())
        .stdout(Stdio::from(file))
        .stderr(Stdio::null())
        .spawn()?;
    let until = Instant::now() + Duration::from_secs(10);
    let status = loop {
        if let Some(status) = child.try_wait()? {
            break status;
        }
        if Instant::now() >= until {
            let _ = child.kill();
            let _ = child.wait();
            return Err(io::Error::new(io::ErrorKind::TimedOut, "guard deadline"));
        }
        thread::sleep(Duration::from_millis(20));
    };
    if !status.success() {
        return Err(invalid());
    }
    let mut bytes = Vec::new();
    fs::File::open(&capture)?
        .take(4097)
        .read_to_end(&mut bytes)?;
    if bytes.len() > 4096 {
        return Err(invalid());
    }
    let value: Value = serde_json::from_slice(&bytes).map_err(|_| invalid())?;
    Ok(!guard_clear(&value))
}

fn guard_clear(value: &Value) -> bool {
    value.get("ok").and_then(Value::as_bool) == Some(true)
        && value.get("access_allowed").and_then(Value::as_bool) == Some(true)
        && value
            .get("classification_unverified")
            .and_then(Value::as_bool)
            == Some(false)
        && value.get("client_resource").and_then(Value::as_bool) == Some(false)
        && value.get("protection_status").and_then(Value::as_str) == Some("clear")
}

fn clean_deletion_root(
    root: &Path,
    apply: bool,
    budget: &Budget,
    stats: &mut Stats,
) -> io::Result<()> {
    if !policy::authorized_root(root) {
        return Err(invalid());
    }
    match fs::symlink_metadata(root) {
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(e),
        Ok(_) => (),
    }
    let _pins = pin_ancestors(root)?;
    for entry in fs::read_dir(root)? {
        budget.check(stats)?;
        let path = entry?.path();
        stats.candidates += 1;
        if policy::retained_name(&path.file_name().unwrap_or_default().to_string_lossy()) {
            stats.retained_recovery_or_source += 1;
            continue;
        }
        // Do not descend into an unclassified private directory while the
        // original protection registry is still unavailable.
        match guard_unknown(&path) {
            Ok(true) => {
                stats.retained_unknown += 1;
                continue;
            }
            Err(_) => {
                stats.failures += 1;
                continue;
            }
            Ok(false) => (),
        }
        if owned::clean_candidate(&path, apply, budget, stats)? {
            continue;
        }
        match retention_tree(&path, budget, stats, 0) {
            Ok(true) => {
                stats.retained_recovery_or_source += 1;
                continue;
            }
            Err(e) if e.kind() == io::ErrorKind::TimedOut => return Err(e),
            Err(_) => {
                stats.retained_links_or_busy += 1;
                continue;
            }
            Ok(false) => (),
        }
        if let Err(e) = delete_tree(root, &path, apply, budget, stats, 0) {
            if e.kind() == io::ErrorKind::TimedOut {
                return Err(e);
            }
            stats.failures += 1;
        }
    }
    Ok(())
}

fn current_sid() -> io::Result<String> {
    let mut raw = 0;
    if unsafe { OpenProcessToken(GetCurrentProcess(), 8, &mut raw) } == 0 {
        return Err(io::Error::last_os_error());
    }
    let token = Handle(raw);
    let mut length = 0;
    unsafe {
        GetTokenInformation(token.0, 1, ptr::null_mut(), 0, &mut length);
    }
    if length == 0 {
        return Err(invalid());
    }
    let mut storage = vec![0usize; (length as usize).div_ceil(std::mem::size_of::<usize>())];
    if unsafe { GetTokenInformation(token.0, 1, storage.as_mut_ptr().cast(), length, &mut length) }
        == 0
    {
        return Err(io::Error::last_os_error());
    }
    let user = unsafe { &*storage.as_ptr().cast::<TokenUser>() };
    let mut text = ptr::null_mut();
    if unsafe { ConvertSidToStringSidW(user.sid, &mut text) } == 0 {
        return Err(io::Error::last_os_error());
    }
    let mut n = 0;
    unsafe {
        while *text.add(n) != 0 {
            n += 1;
        }
    }
    let result =
        String::from_utf16(unsafe { std::slice::from_raw_parts(text, n) }).map_err(|_| invalid());
    unsafe {
        LocalFree(text as isize);
    }
    result
}

fn clean_recycle(
    drive: char,
    sid: &str,
    apply: bool,
    budget: &Budget,
    stats: &mut Stats,
) -> io::Result<()> {
    if !matches!(drive, 'C' | 'D') || !sid.starts_with("S-1-") || sid.contains(['\\', '/']) {
        return Err(invalid());
    }
    let root = PathBuf::from(format!(r"{drive}:\$Recycle.Bin\{sid}"));
    match fs::symlink_metadata(&root) {
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(e),
        Ok(_) => (),
    }
    let _pins = pin_ancestors(&root)?;
    for entry in fs::read_dir(&root)? {
        budget.check(stats)?;
        let entry = entry?;
        let name = entry.file_name().to_string_lossy().to_string();
        let Some(suffix) = name.strip_prefix("$I") else {
            continue;
        };
        stats.candidates += 1;
        let info_pin = match Pinned::open(&entry.path(), false) {
            Ok(p)
                if !p.directory()
                    && p.info.size_low <= 65564
                    && p.info.size_high == 0
                    && p.info.links == 1 =>
            {
                p
            }
            _ => {
                stats.recycle_metadata_retained += 1;
                continue;
            }
        };
        let bytes = fs::read(entry.path())?;
        let Some(original) = policy::recycle_original(&bytes) else {
            stats.recycle_metadata_retained += 1;
            continue;
        };
        if original.as_bytes().first().map(u8::to_ascii_uppercase) != Some(drive as u8) {
            stats.recycle_metadata_retained += 1;
            continue;
        }
        if policy::retained_original(&original) {
            stats.retained_recovery_or_source += 1;
            continue;
        }
        if guard_unknown(Path::new(&original))? {
            stats.retained_unknown += 1;
            continue;
        }
        let data_path = root.join(format!("$R{suffix}"));
        match retention_tree(&data_path, budget, stats, 0) {
            Ok(false) => (),
            Ok(true) => {
                stats.retained_recovery_or_source += 1;
                continue;
            }
            Err(e) if e.kind() == io::ErrorKind::TimedOut => return Err(e),
            Err(_) => {
                stats.retained_links_or_busy += 1;
                continue;
            }
        }
        if let Err(e) = delete_tree(&root, &data_path, apply, budget, stats, 0) {
            if e.kind() == io::ErrorKind::TimedOut {
                return Err(e);
            }
            stats.failures += 1;
            continue;
        }
        drop(info_pin);
        // Remove metadata only after the corresponding data was removed. A
        // failed pair keeps its $I record available for later restoration.
        if apply && let Err(_e) = delete_tree(&root, &entry.path(), true, budget, stats, 0) {
            stats.failures += 1;
        }
    }
    Ok(())
}

fn snapshot() -> [Option<u64>; 3] {
    ['C', 'D', 'E'].map(|drive| {
        let mut free = 0;
        let root = wide(OsStr::new(&format!("{drive}:\\")));
        (unsafe { GetDiskFreeSpaceExW(root.as_ptr(), ptr::null_mut(), ptr::null_mut(), &mut free) }
            != 0)
            .then_some(free)
    })
}

pub fn run() -> i32 {
    let args: Vec<_> = std::env::args().skip(1).collect();
    let apply = match args.as_slice() {
        [] => false,
        [arg] if arg == "--dry-run" => false,
        [arg] if arg == "--apply" => true,
        _ => return 2,
    };
    let _runtime_pins = match pin_ancestors(Path::new(RUNTIME)) {
        Ok(p) => p,
        Err(_) => return 1,
    };
    let lock_name = wide(Path::new(RUNTIME).join("run.lock").as_os_str());
    let raw = unsafe {
        CreateFileW(
            lock_name.as_ptr(),
            0x80000000 | 0x40000000,
            0,
            ptr::null(),
            if apply { 4 } else { 3 },
            0x00200000,
            0,
        )
    };
    if raw == -1 {
        return if io::Error::last_os_error().raw_os_error() == Some(32) {
            0
        } else {
            1
        };
    }
    let _lock = Handle(raw);
    let start = Instant::now();
    let budget = Budget {
        until: start + Duration::from_secs(480),
        max_entries: 200_000,
    };
    let before = snapshot();
    let mut stats = Stats::default();
    for root in [C_DELETE_ROOT, D_DELETE_ROOT] {
        if clean_deletion_root(Path::new(root), apply, &budget, &mut stats).is_err() {
            stats.failures += 1;
        }
    }
    match current_sid() {
        Ok(sid) => {
            for drive in ['C', 'D'] {
                if clean_recycle(drive, &sid, apply, &budget, &mut stats).is_err() {
                    stats.failures += 1;
                }
            }
        }
        Err(_) => stats.failures += 1,
    }
    // A failed new candidate must never bypass the older deletion/recycle work.
    // Keep a separate finite budget after those independent stages.
    builds::run(apply, &budget, &mut stats);
    let after = snapshot();
    let delta: Vec<_> = before
        .iter()
        .zip(after)
        .map(|(b, a)| b.zip(a).map(|(b, a)| i128::from(a) - i128::from(b)))
        .collect();
    let row = json!({
        "schema": 3, "time_unix": SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_secs(),
        "mode": if apply {"apply"} else {"dry_run"}, "roots": ["C_deletion_requests", "D_deletion_requests", "C_owner_recycle", "D_owner_recycle"],
        "free_bytes_before": {"C":before[0],"D":before[1],"E":before[2]},
        "free_bytes_after": {"C":after[0],"D":after[1],"E":after[2]},
        "net_free_change_bytes": {"C":delta[0],"D":delta[1],"E":delta[2]},
        "candidates": stats.candidates, "removed_files": stats.removed_files, "removed_directories":stats.removed_directories,
        "logical_bytes_removed":stats.logical_bytes,"retained_recovery_or_source":stats.retained_recovery_or_source,
        "retained_unknown":stats.retained_unknown,"retained_links_or_busy":stats.retained_links_or_busy,
        "recycle_metadata_retained":stats.recycle_metadata_retained,"failures":stats.failures,"deadline_reached":stats.expired,
        "owned_candidates":stats.owned_candidates,"owned_candidate_logical_bytes":stats.owned_candidate_bytes,
        "owned_actual_removed_bytes":stats.owned_removed_bytes,
        "owned_completed":stats.owned_completed,"owned_retained":stats.owned_retained,
        "owned_finish_failed":stats.owned_finish_failed,"owned_retention_reasons":stats.owned_reasons,
        "owned_retained_plan_remaining_bytes":stats.owned_retained_bytes,"owned_retained_bytes_unmeasured":stats.owned_retained_unmeasured,
        "completed_builds":stats.builds.json(),
        "partial":stats.expired || stats.retained_unknown > 0 || stats.failures > 0 || stats.retained_links_or_busy > 0 || stats.owned_retained > 0 || stats.owned_finish_failed > 0 || stats.builds.partial(),
        "elapsed_ms": start.elapsed().as_millis(), "workspace_scan": "registered_completed_pc_tools_debug_only", "all_pc_cleanup_complete":false,"e_files_touched":false,
    });
    let log = Path::new(RUNTIME).join("owned-target-runs.jsonl");
    let log_ok = !apply
        || OpenOptions::new()
            .append(true)
            .create(true)
            .share_mode(1)
            .custom_flags(0x00200000)
            .open(log)
            .and_then(|mut f| {
                let mut info = FileInfo::default();
                if unsafe { GetFileInformationByHandle(f.as_raw_handle() as isize, &mut info) } == 0
                    || info.attributes & (REPARSE | DIRECTORY) != 0
                    || info.links > 1
                {
                    return Err(invalid());
                }
                serde_json::to_writer(&mut f, &row).map_err(|_| invalid())?;
                f.write_all(b"\n")?;
                f.sync_all()
            })
            .is_ok();
    println!("{row}");
    if log_ok && stats.failures == 0 && before[0].is_some() && before[1].is_some() {
        0
    } else {
        1
    }
}

fn invalid() -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, "cleanup boundary or metadata")
}
fn wide(value: &OsStr) -> Vec<u16> {
    value.encode_wide().chain(Some(0)).collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};
    static NEXT: AtomicU64 = AtomicU64::new(0);
    fn fixture() -> PathBuf {
        let root = std::env::temp_dir().join(format!(
            "cleanup-2491-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&root).unwrap();
        root
    }
    fn budget() -> Budget {
        Budget {
            until: Instant::now() + Duration::from_secs(10),
            max_entries: 1000,
        }
    }
    #[test]
    fn short_name_retains_the_same_pinned_cleanup_boundary() {
        let root = fixture();
        let name = wide(root.as_os_str());
        let mut short_name = vec![0u16; 32_768];
        let length = unsafe {
            GetShortPathNameW(
                name.as_ptr(),
                short_name.as_mut_ptr(),
                short_name.len() as u32,
            )
        } as usize;
        assert!(length > 0 && length < short_name.len());
        let short_root = PathBuf::from(String::from_utf16(&short_name[..length]).unwrap());
        let mut long_name = vec![0u16; 32_768];
        let long_length = unsafe {
            GetLongPathNameW(
                name.as_ptr(),
                long_name.as_mut_ptr(),
                long_name.len() as u32,
            )
        } as usize;
        assert!(long_length > 0 && long_length < long_name.len());
        let long_root = String::from_utf16(&long_name[..long_length]).unwrap();
        assert_ne!(
            normalized(&short_root.to_string_lossy()),
            normalized(&long_root),
            "regression fixture requires a real short-name alias"
        );
        let child = short_root.join("finished");
        fs::create_dir(&child).unwrap();
        fs::write(child.join("cache.bin"), [7; 4096]).unwrap();
        fs::write(root.join("sibling.bin"), [8; 16]).unwrap();
        let pins = pin_ancestors(&short_root).unwrap();
        let mut stats = Stats::default();
        delete_tree(&short_root, &child, true, &budget(), &mut stats, 0).unwrap();
        assert!(!child.exists());
        assert!(root.join("sibling.bin").exists());
        assert_eq!(stats.logical_bytes, 4096);
        assert_eq!(stats.removed_files, 1);
        assert!(delete_tree(&short_root, &short_root, true, &budget(), &mut stats, 0).is_err());
        drop(pins);
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn pinned_deletion_removes_only_children_and_preserves_sibling() {
        let root = fixture();
        let child = root.join("finished");
        fs::create_dir(&child).unwrap();
        fs::write(child.join("cache.bin"), [7; 4096]).unwrap();
        fs::write(root.join("sibling.bin"), [8; 16]).unwrap();
        let _pins = pin_ancestors(&root).unwrap();
        let mut stats = Stats::default();
        delete_tree(&root, &child, true, &budget(), &mut stats, 0).unwrap();
        assert!(!child.exists());
        assert!(root.join("sibling.bin").exists());
        assert_eq!(stats.logical_bytes, 4096);
        assert_eq!(stats.removed_files, 1);
        assert!(delete_tree(&root, &root, true, &budget(), &mut stats, 0).is_err());
        drop(_pins);
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn partial_failure_does_not_remove_open_file_or_claim_complete_tree() {
        let root = fixture();
        let child = root.join("finished");
        fs::create_dir(&child).unwrap();
        fs::write(child.join("cache.bin"), [1; 16]).unwrap();
        let open = fs::File::open(child.join("cache.bin")).unwrap();
        let mut stats = Stats::default();
        assert!(delete_tree(&root, &child, true, &budget(), &mut stats, 0).is_err());
        assert!(child.exists());
        assert_eq!(stats.removed_files, 0);
        drop(open);
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn source_neighbor_and_expired_budget_are_retained() {
        let root = fixture();
        let child = root.join("finished");
        fs::create_dir(&child).unwrap();
        fs::write(child.join("Cargo.toml"), b"source").unwrap();
        assert!(retention_tree(&child, &budget(), &mut Stats::default(), 0).unwrap());
        let expired = Budget {
            until: Instant::now(),
            max_entries: 1000,
        };
        let mut stats = Stats::default();
        assert!(delete_tree(&root, &child, true, &expired, &mut stats, 0).is_err());
        assert!(child.join("Cargo.toml").exists());
        assert!(stats.expired);
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn linked_child_and_external_hardlink_are_retained() {
        let root = fixture();
        let child = root.join("cache.bin");
        fs::write(&child, b"retain").unwrap();
        fs::hard_link(&child, root.join("external.bin")).unwrap();
        assert!(retention_tree(&child, &budget(), &mut Stats::default(), 0).unwrap());
        assert!(delete_tree(&root, &child, true, &budget(), &mut Stats::default(), 0).is_err());
        assert_eq!(fs::read(root.join("external.bin")).unwrap(), b"retain");
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn junction_and_renamed_root_cannot_redirect_cleanup() {
        let root = fixture();
        let outside = fixture();
        fs::write(outside.join("keep.bin"), b"outside").unwrap();
        let linked = root.join("linked");
        let status = Command::new("cmd.exe")
            .args(["/d", "/c", "mklink", "/J"])
            .arg(&linked)
            .arg(&outside)
            .creation_flags(NO_WINDOW)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .unwrap();
        assert!(status.success(), "could not create owned junction fixture");
        assert!(retention_tree(&linked, &budget(), &mut Stats::default(), 0).is_err());
        assert!(delete_tree(&root, &linked, true, &budget(), &mut Stats::default(), 0).is_err());
        let through_link = linked.join("keep.bin");
        assert!(Pinned::open(&through_link, false).is_err());
        assert!(pin_ancestors(&through_link).is_err());
        assert!(
            delete_tree(
                &root,
                &through_link,
                true,
                &budget(),
                &mut Stats::default(),
                0
            )
            .is_err()
        );
        assert_eq!(fs::read(outside.join("keep.bin")).unwrap(), b"outside");
        let pins = pin_ancestors(&root).unwrap();
        assert!(fs::rename(&root, root.with_extension("moved")).is_err());
        drop(pins);
        fs::remove_dir(&linked).unwrap();
        fs::remove_dir_all(root).unwrap();
        fs::remove_dir_all(outside).unwrap();
    }

    #[test]
    fn owned_guard_capture_is_removed_on_error_and_success() {
        let root = fixture();
        let capture = root.join("owned-capture.tmp");
        for fail in [false, true] {
            let result = (|| -> io::Result<()> {
                let file = OpenOptions::new()
                    .create_new(true)
                    .write(true)
                    .open(&capture)?;
                let _cleanup = OwnedCapture(capture.clone());
                drop(file);
                if fail {
                    return Err(invalid());
                }
                Ok(())
            })();
            assert_eq!(result.is_err(), fail);
            assert!(!capture.exists());
        }
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn exclusive_run_lock_denies_a_second_instance() {
        let root = fixture();
        let name = wide(root.join("run.lock").as_os_str());
        let first = unsafe {
            CreateFileW(
                name.as_ptr(),
                0x80000000 | 0x40000000,
                0,
                ptr::null(),
                4,
                0x00200000,
                0,
            )
        };
        assert_ne!(first, -1);
        let lock = Handle(first);
        let second = unsafe {
            CreateFileW(
                name.as_ptr(),
                0x80000000 | 0x40000000,
                0,
                ptr::null(),
                4,
                0x00200000,
                0,
            )
        };
        assert_eq!(second, -1);
        assert_eq!(io::Error::last_os_error().raw_os_error(), Some(32));
        drop(lock);
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn partial_success_counts_only_the_file_actually_removed() {
        let root = fixture();
        let first = root.join("first.bin");
        let busy = root.join("busy.bin");
        fs::write(&first, [1; 32]).unwrap();
        fs::write(&busy, [2; 64]).unwrap();
        let reader = fs::File::open(&busy).unwrap();
        let mut stats = Stats::default();
        delete_tree(&root, &first, true, &budget(), &mut stats, 0).unwrap();
        assert!(delete_tree(&root, &busy, true, &budget(), &mut stats, 0).is_err());
        assert!(!first.exists());
        assert!(busy.exists());
        assert_eq!(stats.removed_files, 1);
        assert_eq!(stats.logical_bytes, 32);
        drop(reader);
        fs::remove_dir_all(root).unwrap();
    }
}
