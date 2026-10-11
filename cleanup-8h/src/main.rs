#[cfg(not(windows))]
fn main() {
    eprintln!("cleanup-8h is Windows-only");
    std::process::exit(2);
}

#[cfg(windows)]
mod windows {
    use serde_json::Value;
    use std::{
        collections::HashMap,
        ffi::OsStr,
        fs::{self, OpenOptions},
        io::{self, Write},
        os::windows::{ffi::OsStrExt, fs::MetadataExt},
        path::{Path, PathBuf},
        process::Command,
        ptr,
        time::{SystemTime, UNIX_EPOCH},
    };

    const TODO_DB: &str = r"D:\CC\Tools\todo\todo.json";
    const WORKSPACES: &str = r"D:\CC\Tools\agent-workspaces";
    const DELETE_ROOT: &str = r"D:\削除希望";
    const C_DELETE_ROOT: &str = r"C:\Users\makhmeto\Downloads\削除希望";
    const C_TEMP: &str = r"C:\Users\makhmeto\AppData\Local\Temp";
    const C_WORKTREES: &str = r"C:\Users\makhmeto\Desktop\dev";
    const SAFE_DELETE_RECEIPTS: &str = r"D:\CC\Tools\safe-delete\receipts-go";
    const SAFE_DELETE: &str = r"D:\CC\Tools\safe-delete\safe-delete.exe";
    const SCRUB: &str = r"D:\CC\Tools\client-records\scrub391.exe";
    const LOG: &str = r"D:\CC\Tools\cleanup-8h\owned-target-runs.jsonl";
    const REPARSE_POINT: u32 = 0x400;
    const ERROR_MORE_DATA: u32 = 234;

    #[repr(C)]
    #[derive(Clone, Copy, Default)]
    struct FileTime {
        low: u32,
        high: u32,
    }

    #[repr(C)]
    #[derive(Clone, Copy, Default)]
    struct UniqueProcess {
        pid: u32,
        started: FileTime,
    }

    #[repr(C)]
    #[derive(Clone, Copy)]
    struct RmProcessInfo {
        process: UniqueProcess,
        app_name: [u16; 256],
        service_name: [u16; 64],
        app_type: i32,
        app_status: u32,
        session_id: u32,
        restartable: i32,
    }

    impl Default for RmProcessInfo {
        fn default() -> Self {
            Self {
                process: UniqueProcess::default(),
                app_name: [0; 256],
                service_name: [0; 64],
                app_type: 0,
                app_status: 0,
                session_id: 0,
                restartable: 0,
            }
        }
    }

    #[link(name = "rstrtmgr")]
    unsafe extern "system" {
        fn RmStartSession(handle: *mut u32, flags: u32, key: *mut u16) -> u32;
        fn RmRegisterResources(
            handle: u32,
            file_count: u32,
            files: *const *const u16,
            app_count: u32,
            apps: *const u8,
            service_count: u32,
            services: *const *const u16,
        ) -> u32;
        fn RmGetList(
            handle: u32,
            needed: *mut u32,
            count: *mut u32,
            processes: *mut RmProcessInfo,
            reboot_reasons: *mut u32,
        ) -> u32;
        fn RmEndSession(handle: u32) -> u32;
    }

    #[link(name = "kernel32")]
    unsafe extern "system" {
        fn GetCurrentProcess() -> isize;
        fn GetLastError() -> u32;
        fn LocalFree(memory: isize) -> isize;
        fn GetDiskFreeSpaceExW(
            path: *const u16,
            available: *mut u64,
            total: *mut u64,
            free: *mut u64,
        ) -> i32;
        fn CreateFileW(
            path: *const u16,
            access: u32,
            share: u32,
            security: *const u8,
            disposition: u32,
            flags: u32,
            template: isize,
        ) -> isize;
        fn GetFileInformationByHandle(handle: isize, info: *mut ByHandleFileInfo) -> i32;
        fn CloseHandle(handle: isize) -> i32;
    }

    #[link(name = "advapi32")]
    unsafe extern "system" {
        fn OpenProcessToken(process: isize, access: u32, token: *mut isize) -> i32;
        fn GetTokenInformation(
            token: isize,
            information_class: u32,
            information: *mut u8,
            information_length: u32,
            return_length: *mut u32,
        ) -> i32;
        fn ConvertSidToStringSidW(sid: *const u8, string_sid: *mut *mut u16) -> i32;
    }

    #[link(name = "shell32")]
    unsafe extern "system" {
        fn SHEmptyRecycleBinW(window: isize, root: *const u16, flags: u32) -> i32;
    }

    #[repr(C)]
    struct TokenUser {
        sid: *mut u8,
        _attributes: u32,
    }

    #[repr(C)]
    #[derive(Default)]
    struct ByHandleFileInfo {
        attributes: u32,
        created: FileTime,
        accessed: FileTime,
        written: FileTime,
        volume_serial: u32,
        size_high: u32,
        size_low: u32,
        links: u32,
        index_high: u32,
        index_low: u32,
    }

    #[derive(Default)]
    struct RunStats {
        scanned: u64,
        eligible: u64,
        removed: u64,
        skipped_unmapped: u64,
        skipped_active: u64,
        skipped_client: u64,
        skipped_link: u64,
        skipped_uncommitted: u64,
        skipped_evidence: u64,
        skipped_busy: u64,
        skipped_hardlink: u64,
        failed: u64,
        logical_bytes: u64,
        errors: Vec<String>,
        recycle_c: Option<RecycleResult>,
        recycle_d: Option<RecycleResult>,
    }

    #[derive(Clone, Copy)]
    struct RecycleResult {
        removed_entries: u64,
        method: &'static str,
    }

    pub fn run() -> i32 {
        let before = match free_snapshot() {
            Ok(v) => v,
            Err(e) => {
                eprintln!("capacity_snapshot_error={}", error_kind(&e));
                return 1;
            }
        };
        let mut stats = RunStats::default();
        for (drive, result) in [('C', &mut stats.recycle_c), ('D', &mut stats.recycle_d)] {
            match clear_recycle_bin(drive) {
                Ok(value) => *result = Some(value),
                Err(e) => {
                    stats.failed += 1;
                    stats.errors.push(error_kind(&e));
                }
            }
        }
        match todo_statuses() {
            Ok(statuses) => {
                let mut targets = Vec::new();
                let mut owner_overrides = HashMap::new();
                for root in [
                    Path::new(WORKSPACES),
                    Path::new(DELETE_ROOT),
                    Path::new(C_DELETE_ROOT),
                    Path::new(C_WORKTREES),
                ] {
                    let mut root_targets = Vec::new();
                    match collect_targets(
                        root,
                        &statuses,
                        &mut root_targets,
                        None,
                        &mut owner_overrides,
                        &mut stats,
                    ) {
                        Ok(()) => targets.extend(root_targets),
                        Err(e) => {
                            stats.failed += 1;
                            stats.errors.push(error_kind(&e));
                        }
                    }
                }
                if let Err(e) = collect_owned_temp_targets(
                    &statuses,
                    &mut targets,
                    &mut owner_overrides,
                    &mut stats,
                ) {
                    stats.failed += 1;
                    stats.errors.push(error_kind(&e));
                }
                stats.scanned = targets.len() as u64;
                for target in targets {
                    if let Err(e) = clean_target(
                        &target,
                        owner_overrides.get(&target).copied(),
                        &statuses,
                        &mut stats,
                    ) {
                        stats.failed += 1;
                        stats.errors.push(error_kind(&e));
                    }
                }
            }
            Err(e) => {
                stats.failed += 1;
                stats.errors.push(error_kind(&e));
            }
        }
        let after = match free_snapshot() {
            Ok(v) => v,
            Err(e) => {
                stats.failed += 1;
                stats.errors.push(error_kind(&e));
                before
            }
        };
        let log_ok = append_log(before, after, &stats).is_ok();
        println!(
            "recycle_c={}({}) recycle_d={}({}) artifacts_scanned={} artifacts_removed={} logical_GiB={:.2} C_free_GiB={:.2}->{:.2} D_free_GiB={:.2}->{:.2} E_free_GiB={:.2}->{:.2} skipped_active={} skipped_client={} skipped_link={} skipped_uncommitted={} skipped_evidence={} skipped_busy={} skipped_hardlink={} failures={} log_ok={}",
            stats.recycle_c.map_or("failed", |x| x.method),
            stats.recycle_c.map_or(0, |x| x.removed_entries),
            stats.recycle_d.map_or("failed", |x| x.method),
            stats.recycle_d.map_or(0, |x| x.removed_entries),
            stats.scanned,
            stats.removed,
            stats.logical_bytes as f64 / 1_073_741_824.0,
            before[0] as f64 / 1_073_741_824.0,
            after[0] as f64 / 1_073_741_824.0,
            before[1] as f64 / 1_073_741_824.0,
            after[1] as f64 / 1_073_741_824.0,
            before[2] as f64 / 1_073_741_824.0,
            after[2] as f64 / 1_073_741_824.0,
            stats.skipped_active,
            stats.skipped_client,
            stats.skipped_link,
            stats.skipped_uncommitted,
            stats.skipped_evidence,
            stats.skipped_busy,
            stats.skipped_hardlink,
            stats.failed,
            log_ok
        );
        if stats.failed != 0 || !log_ok { 1 } else { 0 }
    }

    fn clear_recycle_bin(drive: char) -> io::Result<RecycleResult> {
        if !recycle_drive_allowed(drive) {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                "unsupported cleanup drive",
            ));
        }
        let root = wide(OsStr::new(&format!("{drive}:\\")));
        let hresult = unsafe { SHEmptyRecycleBinW(0, root.as_ptr(), 0x1 | 0x2 | 0x4) };
        if hresult >= 0 {
            return Ok(RecycleResult {
                removed_entries: 0,
                method: "shell",
            });
        }
        Ok(RecycleResult {
            removed_entries: empty_current_user_recycle_store(drive)?,
            method: "account_store",
        })
    }

    fn recycle_drive_allowed(drive: char) -> bool {
        matches!(drive, 'C' | 'D')
    }

    fn current_user_sid() -> io::Result<String> {
        const TOKEN_QUERY: u32 = 0x0008;
        const TOKEN_USER_CLASS: u32 = 1;
        let mut token = 0isize;
        let opened = unsafe { OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &mut token) };
        if opened == 0 {
            return Err(io::Error::last_os_error());
        }
        let result = (|| {
            let mut length = 0u32;
            unsafe {
                GetTokenInformation(token, TOKEN_USER_CLASS, ptr::null_mut(), 0, &mut length);
            }
            if length == 0 {
                let code = unsafe { GetLastError() };
                return Err(io::Error::from_raw_os_error(code as i32));
            }
            let word_count = (length as usize).div_ceil(std::mem::size_of::<usize>());
            let mut storage = vec![0usize; word_count];
            let ok = unsafe {
                GetTokenInformation(
                    token,
                    TOKEN_USER_CLASS,
                    storage.as_mut_ptr().cast(),
                    length,
                    &mut length,
                )
            };
            if ok == 0 {
                return Err(io::Error::last_os_error());
            }
            let user = unsafe { &*(storage.as_ptr().cast::<TokenUser>()) };
            let mut sid_text = ptr::null_mut();
            let converted = unsafe { ConvertSidToStringSidW(user.sid, &mut sid_text) };
            if converted == 0 {
                return Err(io::Error::last_os_error());
            }
            let mut sid_len = 0usize;
            unsafe {
                while *sid_text.add(sid_len) != 0 {
                    sid_len += 1;
                }
            }
            let sid = unsafe { String::from_utf16(std::slice::from_raw_parts(sid_text, sid_len)) }
                .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "user SID encoding"));
            unsafe {
                LocalFree(sid_text as isize);
            }
            sid
        })();
        unsafe {
            CloseHandle(token);
        }
        result
    }

    fn empty_current_user_recycle_store(drive: char) -> io::Result<u64> {
        let sid = current_user_sid()?;
        let root = PathBuf::from(format!(r"{drive}:\$Recycle.Bin\{sid}"));
        let metadata = match fs::symlink_metadata(&root) {
            Ok(value) => value,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(0),
            Err(e) => return Err(e),
        };
        if !safe_directory(&metadata) {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "recycle store is linked",
            ));
        }
        verify_safe_ancestors(&root)?;
        let mut removed = 0u64;
        for entry in fs::read_dir(&root)? {
            let entry = entry?;
            if entry
                .file_name()
                .to_string_lossy()
                .eq_ignore_ascii_case("desktop.ini")
            {
                continue;
            }
            let path = entry.path();
            let metadata = fs::symlink_metadata(&path)?;
            if !safe_entry(&metadata) {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "reparse point in recycle store",
                ));
            }
            if metadata.is_dir() {
                fs::remove_dir_all(path)?;
            } else {
                fs::remove_file(path)?;
            }
            removed += 1;
        }
        Ok(removed)
    }

    fn todo_statuses() -> io::Result<HashMap<u64, String>> {
        let root: Value = serde_json::from_slice(&fs::read(TODO_DB)?).map_err(invalid_data)?;
        let items = root
            .get("items")
            .and_then(Value::as_array)
            .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, "todo items unavailable"))?;
        let mut statuses = HashMap::with_capacity(items.len());
        for item in items {
            if let (Some(id), Some(status)) = (
                item.get("id").and_then(Value::as_u64),
                item.get("status").and_then(Value::as_str),
            ) {
                statuses.insert(id, status.to_owned());
            }
        }
        Ok(statuses)
    }

    fn collect_targets(
        root: &Path,
        statuses: &HashMap<u64, String>,
        out: &mut Vec<PathBuf>,
        inherited_owner: Option<u64>,
        owner_overrides: &mut HashMap<PathBuf, u64>,
        stats: &mut RunStats,
    ) -> io::Result<()> {
        let meta = match fs::symlink_metadata(root) {
            Ok(m) => m,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
            Err(e) => return Err(e),
        };
        if !safe_directory(&meta) {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "cleanup root is linked",
            ));
        }
        verify_safe_ancestors(root)?;
        if is_client_resource(root)? {
            stats.skipped_client += 1;
            return Ok(());
        }
        let mut stack = vec![(root.to_path_buf(), 0usize, inherited_owner)];
        while let Some((dir, depth, inherited_owner)) = stack.pop() {
            for entry in fs::read_dir(&dir)? {
                let entry = entry?;
                let path = entry.path();
                let meta = fs::symlink_metadata(&path)?;
                if !safe_entry(&meta) {
                    stats.skipped_link += 1;
                    continue;
                }
                if meta.is_dir() {
                    let mut descendant_owner = inherited_owner;
                    if depth == 0 {
                        let quarantined_root = is_deletion_request_root(root);
                        let mut guard_checked = false;
                        if quarantined_root {
                            if is_client_resource(&path)? {
                                stats.skipped_client += 1;
                                continue;
                            }
                            guard_checked = true;
                        }
                        if descendant_owner.is_none() {
                            descendant_owner = owner_id(&path);
                            if descendant_owner.is_none() {
                                stats.skipped_unmapped += 1;
                                continue;
                            }
                            if !guard_checked && is_client_resource(&path)? {
                                stats.skipped_client += 1;
                                continue;
                            }
                        }
                        if statuses.get(&descendant_owner.unwrap()).map(String::as_str)
                            != Some("done")
                        {
                            stats.skipped_active += 1;
                            continue;
                        }
                    }
                    if is_prunable_dir(root, &path) {
                        out.push(path.clone());
                        if let Some(id) = descendant_owner {
                            owner_overrides.insert(path, id);
                        }
                    } else if is_ignored_subtree(&entry.file_name().to_string_lossy()) {
                        continue;
                    } else {
                        stack.push((path, depth + 1, descendant_owner));
                    }
                }
            }
        }
        Ok(())
    }

    fn collect_owned_temp_targets(
        statuses: &HashMap<u64, String>,
        targets: &mut Vec<PathBuf>,
        owner_overrides: &mut HashMap<PathBuf, u64>,
        stats: &mut RunStats,
    ) -> io::Result<()> {
        let receipts_root = Path::new(SAFE_DELETE_RECEIPTS);
        let root_meta = match fs::symlink_metadata(receipts_root) {
            Ok(meta) => meta,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(()),
            Err(e) => return Err(e),
        };
        if !safe_directory(&root_meta) {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "safe-delete receipts root is linked",
            ));
        }
        verify_safe_ancestors(receipts_root)?;
        for entry in fs::read_dir(receipts_root)? {
            let entry = entry?;
            let metadata = fs::symlink_metadata(entry.path())?;
            if !safe_entry(&metadata) {
                stats.skipped_link += 1;
                continue;
            }
            if !metadata.is_file()
                || !entry
                    .path()
                    .extension()
                    .is_some_and(|ext| ext.eq_ignore_ascii_case("json"))
            {
                continue;
            }
            let receipt: Value = match fs::read(entry.path())
                .and_then(|bytes| serde_json::from_slice(&bytes).map_err(invalid_data))
            {
                Ok(value) => value,
                Err(_) => {
                    stats.skipped_unmapped += 1;
                    continue;
                }
            };
            if receipt.get("state").and_then(Value::as_str) != Some("owned") {
                continue;
            }
            let Some(receipt_id) = receipt.get("id").and_then(Value::as_str) else {
                stats.skipped_unmapped += 1;
                continue;
            };
            let Some(receipt_path) = receipt.get("path").and_then(Value::as_str) else {
                stats.skipped_unmapped += 1;
                continue;
            };
            let path = PathBuf::from(receipt_path);
            if path == Path::new(C_TEMP) || !path.starts_with(C_TEMP) {
                continue;
            }
            let project = receipt.get("project").and_then(Value::as_str).unwrap_or("");
            let content = receipt.get("content").and_then(Value::as_str).unwrap_or("");
            let Some(owner) = safe_delete_receipt_owner(project, content) else {
                stats.skipped_unmapped += 1;
                continue;
            };
            if statuses.get(&owner).map(String::as_str) != Some("done") {
                stats.skipped_active += 1;
                continue;
            }
            verify_safe_ancestors(&path)?;
            let path_meta = match fs::symlink_metadata(&path) {
                Ok(meta) => meta,
                Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e),
            };
            if !safe_directory(&path_meta) {
                stats.skipped_link += 1;
                continue;
            }
            if is_client_resource(&path)? {
                stats.skipped_client += 1;
                continue;
            }
            let plan = Command::new(SAFE_DELETE)
                .args(["plan", "--id", receipt_id])
                .output()?;
            if !plan.status.success() {
                stats.skipped_unmapped += 1;
                continue;
            }
            let plan: Value = match serde_json::from_slice(&plan.stdout) {
                Ok(value) => value,
                Err(_) => {
                    stats.skipped_unmapped += 1;
                    continue;
                }
            };
            let plan_path_matches = plan
                .get("path")
                .and_then(Value::as_str)
                .is_some_and(|planned| planned.eq_ignore_ascii_case(receipt_path));
            if plan.get("action").and_then(Value::as_str) != Some("submit_to_exec_tool")
                || plan.get("id").and_then(Value::as_str) != Some(receipt_id)
                || !plan_path_matches
                || plan
                    .get("powershell_command")
                    .and_then(Value::as_str)
                    .is_none_or(str::is_empty)
            {
                stats.skipped_unmapped += 1;
                continue;
            }
            let before = targets.len();
            collect_targets(
                &path,
                statuses,
                targets,
                Some(owner),
                owner_overrides,
                stats,
            )?;
            for candidate in &targets[before..] {
                owner_overrides.insert(candidate.clone(), owner);
            }
        }
        Ok(())
    }

    fn clean_target(
        path: &Path,
        owner_hint: Option<u64>,
        statuses: &HashMap<u64, String>,
        stats: &mut RunStats,
    ) -> io::Result<()> {
        verify_safe_ancestors(path)?;
        if is_client_resource(path)? {
            stats.skipped_client += 1;
            return Ok(());
        }
        let Some(id) = owner_hint.or_else(|| owner_id(path)) else {
            stats.skipped_unmapped += 1;
            return Ok(());
        };
        if statuses.get(&id).map(String::as_str) != Some("done") {
            stats.skipped_active += 1;
            return Ok(());
        }
        if has_uncommitted_ancestor(path)? {
            stats.skipped_uncommitted += 1;
            return Ok(());
        }
        let files = inspect_tree(path)?;
        if files.linked {
            stats.skipped_link += 1;
            return Ok(());
        }
        if files.evidence {
            stats.skipped_evidence += 1;
            return Ok(());
        }
        if files.external_hardlink {
            stats.skipped_hardlink += 1;
            return Ok(());
        }
        if restart_manager_busy(&files.paths)? {
            stats.skipped_busy += 1;
            return Ok(());
        }
        stats.eligible += 1;
        fs::remove_dir_all(path)?;
        stats.removed += 1;
        stats.logical_bytes = stats.logical_bytes.saturating_add(files.bytes);
        Ok(())
    }

    fn todo_id(path: &Path) -> Option<u64> {
        for component in path.ancestors() {
            if let Some(name) = component.file_name()
                && let Some(id) = todo_id_in_text(&name.to_string_lossy())
            {
                return Some(id);
            }
        }
        None
    }

    fn owner_id(path: &Path) -> Option<u64> {
        todo_id(path).or_else(|| quarantine_todo_id(path))
    }

    fn quarantine_todo_id(path: &Path) -> Option<u64> {
        let roots = [Path::new(DELETE_ROOT), Path::new(C_DELETE_ROOT)];
        let root = roots.iter().copied().find(|root| path.starts_with(root))?;
        for ancestor in path.ancestors() {
            if ancestor == root {
                break;
            }
            let reason = ancestor.join("理由.txt");
            let Ok(metadata) = fs::symlink_metadata(&reason) else {
                continue;
            };
            if !metadata.is_file() || !safe_entry(&metadata) {
                continue;
            }
            let content = fs::read_to_string(reason).ok()?;
            if let Some(id) = todo_id_in_reason(&content) {
                return Some(id);
            }
        }
        None
    }

    fn is_deletion_request_root(root: &Path) -> bool {
        [Path::new(DELETE_ROOT), Path::new(C_DELETE_ROOT)].contains(&root)
    }

    fn todo_id_in_reason(text: &str) -> Option<u64> {
        for line in text.lines() {
            let lower = line.trim().to_ascii_lowercase();
            if (lower.starts_with("元の場所") || lower.starts_with("original path"))
                && let Some(id) = todo_id_in_text(line)
            {
                return Some(id);
            }
        }
        None
    }

    fn todo_id_in_text(text: &str) -> Option<u64> {
        let lower = text.to_ascii_lowercase();
        for (index, _) in lower.match_indices("todo") {
            let digits: String = lower[index + 4..]
                .trim_start_matches(['-', '_', ' '])
                .chars()
                .take_while(char::is_ascii_digit)
                .collect();
            if let Ok(id) = digits.parse() {
                return Some(id);
            }
        }
        None
    }

    fn safe_delete_receipt_owner(project: &str, content: &str) -> Option<u64> {
        match (todo_id_in_text(project), todo_id_in_text(content)) {
            (Some(project_id), Some(content_id)) if project_id != content_id => None,
            (Some(id), _) | (_, Some(id)) => Some(id),
            (None, None) => None,
        }
    }

    fn is_prunable_dir(root: &Path, path: &Path) -> bool {
        let Some(name) = path.file_name().and_then(OsStr::to_str) else {
            return false;
        };
        if name.eq_ignore_ascii_case("target") {
            return true;
        }
        if !["temp", "tmp", ".temp", ".tmp", "cache", ".cache"]
            .iter()
            .any(|candidate| name.eq_ignore_ascii_case(candidate))
        {
            return false;
        }
        is_deletion_request_root(root)
            || path.ancestors().any(|ancestor| {
                ancestor
                    .file_name()
                    .map(|n| n.to_string_lossy().starts_with("codex-owned-"))
                    .unwrap_or(false)
            })
    }

    fn is_ignored_subtree(name: &str) -> bool {
        [".git", "node_modules", ".pnpm-store", ".cargo", "vendor"]
            .iter()
            .any(|candidate| name.eq_ignore_ascii_case(candidate))
    }

    fn is_evidence_name(name: &str) -> bool {
        let lower = name.to_ascii_lowercase();
        let ext = Path::new(&lower)
            .extension()
            .and_then(OsStr::to_str)
            .unwrap_or("");
        let evidence_name = [
            "verification",
            "test-result",
            "test_result",
            "junit",
            "coverage",
            "sarif",
            "report",
        ]
        .iter()
        .any(|marker| lower.contains(marker));
        matches!(ext, "log" | "jsonl" | "trx" | "tap")
            || (evidence_name
                && matches!(
                    ext,
                    "md" | "txt" | "json" | "xml" | "sarif" | "trx" | "tap" | "log" | "jsonl"
                ))
            || lower == "verification.md"
            || (lower.starts_with("test-results") && ext == "json")
            || matches!(lower.as_str(), "junit.xml" | "results.xml" | "coverage.xml")
    }

    fn has_uncommitted_ancestor(path: &Path) -> io::Result<bool> {
        for ancestor in path.ancestors().skip(1) {
            if !ancestor.join(".git").exists() {
                continue;
            }
            let upstream = Command::new("git")
                .arg("-C")
                .arg(ancestor)
                .args(["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"])
                .output()?;
            if !upstream.status.success() {
                return Ok(true);
            }
            let upstream = String::from_utf8_lossy(&upstream.stdout).trim().to_owned();
            let output = Command::new("git")
                .arg("-C")
                .arg(ancestor)
                .args(["status", "--porcelain=v1"])
                .output()?;
            if !output.status.success() {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "git status unavailable",
                ));
            }
            if !output.stdout.is_empty() {
                return Ok(true);
            }
            let ahead = Command::new("git")
                .arg("-C")
                .arg(ancestor)
                .args(["rev-list", "--count"])
                .arg(format!("{upstream}..HEAD"))
                .output()?;
            if !ahead.status.success() {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "git history unavailable",
                ));
            }
            if String::from_utf8_lossy(&ahead.stdout)
                .trim()
                .parse::<u64>()
                .unwrap_or(1)
                > 0
            {
                return Ok(true);
            }
            let relative = path.strip_prefix(ancestor).map_err(|_| {
                io::Error::new(io::ErrorKind::InvalidData, "target escaped repository")
            })?;
            let tracked = Command::new("git")
                .arg("-C")
                .arg(ancestor)
                .args(["ls-files", "--"])
                .arg(relative)
                .output()?;
            if !tracked.status.success() {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "git index unavailable",
                ));
            }
            return Ok(!tracked.stdout.is_empty());
        }
        Ok(false)
    }

    struct TreeInfo {
        paths: Vec<PathBuf>,
        bytes: u64,
        evidence: bool,
        linked: bool,
        external_hardlink: bool,
    }

    fn inspect_tree(root: &Path) -> io::Result<TreeInfo> {
        let mut paths = Vec::new();
        let mut stack = vec![root.to_path_buf()];
        let mut bytes = 0u64;
        let mut evidence = false;
        let mut linked = false;
        let mut links: HashMap<(u32, u64), (u64, u64)> = HashMap::new();
        while let Some(dir) = stack.pop() {
            for entry in fs::read_dir(&dir)? {
                let entry = entry?;
                let path = entry.path();
                let meta = fs::symlink_metadata(&path)?;
                if !safe_entry(&meta) {
                    linked = true;
                    continue;
                }
                if meta.is_dir() {
                    stack.push(path);
                    continue;
                }
                if is_evidence_name(&entry.file_name().to_string_lossy()) {
                    evidence = true;
                }
                bytes = bytes.saturating_add(meta.file_size());
                let (volume, file_id, link_count) = file_identity(&path)?;
                let item = links.entry((volume, file_id)).or_insert((0, link_count));
                item.0 += 1;
                paths.push(path);
            }
        }
        let external_hardlink = links.values().any(|(inside, total)| inside != total);
        Ok(TreeInfo {
            paths,
            bytes,
            evidence,
            linked,
            external_hardlink,
        })
    }

    fn verify_safe_ancestors(path: &Path) -> io::Result<()> {
        let mut current = Some(path);
        while let Some(p) = current {
            let meta = fs::symlink_metadata(p)?;
            if meta.file_attributes() & REPARSE_POINT != 0 {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "reparse-point ancestor",
                ));
            }
            if p.file_name()
                .map(|x| x.to_string_lossy().eq_ignore_ascii_case(".git"))
                .unwrap_or(false)
            {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "git metadata path",
                ));
            }
            current = p.parent();
        }
        Ok(())
    }

    fn safe_directory(meta: &fs::Metadata) -> bool {
        meta.is_dir() && meta.file_attributes() & REPARSE_POINT == 0
    }
    fn safe_entry(meta: &fs::Metadata) -> bool {
        meta.file_attributes() & REPARSE_POINT == 0
    }

    fn is_client_resource(path: &Path) -> io::Result<bool> {
        let output = Command::new(SCRUB).arg("--guard-path").arg(path).output()?;
        if !output.status.success() {
            return Err(io::Error::new(
                io::ErrorKind::PermissionDenied,
                "client guard failed",
            ));
        }
        let value: Value = serde_json::from_slice(&output.stdout).map_err(invalid_data)?;
        value
            .get("client_resource")
            .and_then(Value::as_bool)
            .ok_or_else(|| {
                io::Error::new(io::ErrorKind::InvalidData, "client guard response invalid")
            })
    }

    fn restart_manager_busy(paths: &[PathBuf]) -> io::Result<bool> {
        if paths.is_empty() {
            return Ok(false);
        }
        let mut session = 0u32;
        let mut key = [0u16; 33];
        let code = unsafe { RmStartSession(&mut session, 0, key.as_mut_ptr()) };
        if code != 0 {
            return Err(io::Error::from_raw_os_error(code as i32));
        }
        let result = (|| {
            for chunk in paths.chunks(4096) {
                let wide_paths: Vec<Vec<u16>> = chunk.iter().map(|p| wide(p.as_os_str())).collect();
                let pointers: Vec<*const u16> = wide_paths.iter().map(|p| p.as_ptr()).collect();
                let code = unsafe {
                    RmRegisterResources(
                        session,
                        pointers.len() as u32,
                        pointers.as_ptr(),
                        0,
                        ptr::null(),
                        0,
                        ptr::null(),
                    )
                };
                if code != 0 {
                    return Err(io::Error::from_raw_os_error(code as i32));
                }
                if restart_manager_has_users(session)? {
                    return Ok(true);
                }
            }
            Ok(false)
        })();
        let end = unsafe { RmEndSession(session) };
        match (result, end) {
            (Ok(busy), 0) => Ok(busy),
            (Err(e), _) => Err(e),
            (Ok(_), code) => Err(io::Error::from_raw_os_error(code as i32)),
        }
    }

    fn restart_manager_has_users(session: u32) -> io::Result<bool> {
        let mut needed = 0u32;
        let mut count = 0u32;
        let mut reasons = 0u32;
        let code = unsafe {
            RmGetList(
                session,
                &mut needed,
                &mut count,
                ptr::null_mut(),
                &mut reasons,
            )
        };
        if code == 0 {
            return Ok(count != 0);
        }
        if code != ERROR_MORE_DATA {
            return Err(io::Error::from_raw_os_error(code as i32));
        }
        let mut processes = vec![RmProcessInfo::default(); needed as usize];
        count = needed;
        let code = unsafe {
            RmGetList(
                session,
                &mut needed,
                &mut count,
                processes.as_mut_ptr(),
                &mut reasons,
            )
        };
        if code != 0 {
            return Err(io::Error::from_raw_os_error(code as i32));
        }
        Ok(count != 0)
    }

    fn free_bytes(root: &str) -> io::Result<u64> {
        let path = wide(OsStr::new(root));
        let mut available = 0u64;
        let mut total = 0u64;
        let mut free = 0u64;
        let ok =
            unsafe { GetDiskFreeSpaceExW(path.as_ptr(), &mut available, &mut total, &mut free) };
        if ok == 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(available)
        }
    }

    fn free_snapshot() -> io::Result<[u64; 3]> {
        Ok([
            free_bytes("C:\\")?,
            free_bytes("D:\\")?,
            free_bytes("E:\\")?,
        ])
    }

    fn file_identity(path: &Path) -> io::Result<(u32, u64, u64)> {
        const FILE_READ_ATTRIBUTES: u32 = 0x80;
        const SHARE_ALL: u32 = 0x1 | 0x2 | 0x4;
        const OPEN_EXISTING: u32 = 3;
        const INVALID_HANDLE: isize = -1;
        let name = wide(path.as_os_str());
        let handle = unsafe {
            CreateFileW(
                name.as_ptr(),
                FILE_READ_ATTRIBUTES,
                SHARE_ALL,
                ptr::null(),
                OPEN_EXISTING,
                0,
                0,
            )
        };
        if handle == INVALID_HANDLE {
            return Err(io::Error::last_os_error());
        }
        let mut info = ByHandleFileInfo::default();
        let ok = unsafe { GetFileInformationByHandle(handle, &mut info) };
        let error = if ok == 0 {
            Some(io::Error::last_os_error())
        } else {
            None
        };
        unsafe {
            CloseHandle(handle);
        }
        if let Some(error) = error {
            return Err(error);
        }
        let index = (u64::from(info.index_high) << 32) | u64::from(info.index_low);
        Ok((info.volume_serial, index, u64::from(info.links)))
    }

    fn append_log(before: [u64; 3], after: [u64; 3], s: &RunStats) -> io::Result<()> {
        fs::create_dir_all(Path::new(LOG).parent().expect("log parent"))?;
        let mut f = OpenOptions::new().create(true).append(true).open(LOG)?;
        let row = serde_json::json!({
            "time_unix": SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_secs(),
            "free_bytes_before": {"C":before[0],"D":before[1],"E":before[2]},
            "free_bytes_after": {"C":after[0],"D":after[1],"E":after[2]},
            "recycle_c_method": s.recycle_c.map(|x| x.method),
            "recycle_c_removed_entries": s.recycle_c.map(|x| x.removed_entries),
            "recycle_d_method": s.recycle_d.map(|x| x.method),
            "recycle_d_removed_entries": s.recycle_d.map(|x| x.removed_entries),
            "artifacts_scanned": s.scanned, "artifacts_eligible": s.eligible, "artifacts_removed": s.removed,
            "logical_bytes_removed": s.logical_bytes, "skipped_unmapped": s.skipped_unmapped,
            "skipped_active": s.skipped_active, "skipped_client": s.skipped_client, "skipped_link": s.skipped_link,
            "skipped_uncommitted": s.skipped_uncommitted,
            "skipped_evidence": s.skipped_evidence, "skipped_busy": s.skipped_busy,
            "skipped_hardlink": s.skipped_hardlink, "failed": s.failed, "error_kinds": s.errors,
        });
        serde_json::to_writer(&mut f, &row).map_err(invalid_data)?;
        f.write_all(b"\n")
    }

    fn error_kind(e: &io::Error) -> String {
        match e.kind() {
            io::ErrorKind::NotFound => "not_found",
            io::ErrorKind::PermissionDenied => "permission_denied",
            io::ErrorKind::InvalidData => "invalid_data",
            io::ErrorKind::WouldBlock => "would_block",
            _ => "io_error",
        }
        .to_owned()
    }
    fn invalid_data(_: impl std::error::Error) -> io::Error {
        io::Error::new(io::ErrorKind::InvalidData, "invalid cleanup metadata")
    }
    fn wide(s: &OsStr) -> Vec<u16> {
        s.encode_wide().chain(Some(0)).collect()
    }

    #[cfg(test)]
    mod tests {
        use super::{
            DELETE_ROOT, is_evidence_name, is_ignored_subtree, is_prunable_dir,
            recycle_drive_allowed, safe_delete_receipt_owner, todo_id, todo_id_in_reason,
            todo_id_in_text,
        };
        use std::path::Path;

        #[test]
        fn maps_workspace_and_quarantine_names_to_todo_owner() {
            assert_eq!(
                todo_id(Path::new(
                    r"D:\CC\Tools\agent-workspaces\todo-2301-docker\work\target"
                )),
                Some(2301)
            );
            assert_eq!(
                todo_id(Path::new(
                    r"D:\削除希望\20261011-cc-pc-tools-todo2164-build\target"
                )),
                Some(2164)
            );
            assert_eq!(
                todo_id(Path::new(r"D:\CC\Tools\agent-workspaces\misc\target")),
                None
            );
            assert_eq!(
                todo_id_in_text("original path: C:/work/todo-2240-build/target"),
                Some(2240)
            );
            assert_eq!(todo_id_in_text("no task identifier here"), None);
            assert_eq!(
                todo_id_in_reason("理由: todo-2000\r\n元の場所: D:/work/todo-2240/target"),
                Some(2240)
            );
            assert_eq!(safe_delete_receipt_owner("todo-2343", "cache"), Some(2343));
            assert_eq!(safe_delete_receipt_owner("todo-2343", "todo-2301"), None);
        }

        #[test]
        fn prunes_only_named_generated_directories() {
            let root = Path::new(r"D:\CC\Tools\agent-workspaces");
            assert!(is_prunable_dir(
                root,
                &root.join("todo-7/work/codex-owned-x/target")
            ));
            assert!(is_prunable_dir(
                root,
                &root.join("todo-7/work/codex-owned-x/.cache")
            ));
            assert!(!is_prunable_dir(root, &root.join("todo-7/repo/src/cache")));
            assert!(is_prunable_dir(
                Path::new(DELETE_ROOT),
                &Path::new(DELETE_ROOT).join("todo-7/temp")
            ));
            assert!(!is_prunable_dir(root, &root.join("todo-7/.pnpm-store")));
            assert!(is_ignored_subtree("node_modules"));
            assert!(is_ignored_subtree(".git"));
            assert!(!is_ignored_subtree("codex-owned-x"));
        }

        #[test]
        fn preserves_verification_evidence_files() {
            assert!(is_evidence_name("VERIFICATION.md"));
            assert!(is_evidence_name("nextest.log"));
            assert!(is_evidence_name("test-results.json"));
            assert!(is_evidence_name("coverage.xml"));
            assert!(is_evidence_name("scan.sarif"));
            assert!(!is_evidence_name("libexample.rlib"));
        }

        #[test]
        fn recycle_bin_cleanup_excludes_e() {
            assert!(recycle_drive_allowed('C'));
            assert!(recycle_drive_allowed('D'));
            assert!(!recycle_drive_allowed('E'));
        }
    }
}

#[cfg(windows)]
fn main() {
    std::process::exit(windows::run());
}
