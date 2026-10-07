//! Read bounded session metadata/events; never tool output or model credentials.
use crate::{automation::*, core::*, resume::path_key};
use serde_json::{json, Value};
use std::{
    io::{BufRead, BufReader, Read, Seek, SeekFrom},
    path::Path,
    time::Duration,
};

pub fn summary(path: &Path) -> Result<Option<Value>> {
    let mut file = std::fs::File::open(path).map_err(|_| "session unavailable")?;
    let mut first = String::new();
    BufReader::new((&mut file).take(65536))
        .read_line(&mut first)
        .map_err(|_| "session metadata unavailable")?;
    let meta: Value = serde_json::from_str(&first).map_err(|_| "invalid session metadata")?;
    if s(&meta["type"]) != "session_meta" {
        return Ok(None);
    }
    let meta = &meta["payload"];
    if meta["source"].get("subagent").is_some() || truth(&meta["parent_thread_id"]) {
        return Ok(None);
    }
    let Ok(sid) = uuid::Uuid::parse_str(s(&meta["id"])) else {
        return Ok(None);
    };
    let len = file
        .metadata()
        .map_err(|_| "session metadata unavailable")?
        .len();
    let offset = len.saturating_sub(1024 * 1024);
    file.seek(SeekFrom::Start(offset))
        .map_err(|_| "session seek failed")?;
    let row = json!({"session_id":sid.to_string(),"session_path":path.to_string_lossy(),"cwd":meta["cwd"],"last_activity":0,"completed_at":0,"turn_id":"","verdict":"partial","final_excerpt":""});
    read_tail(file, len - offset, offset > 0, row)
}

fn read_tail(
    file: impl Read,
    limit: u64,
    discard_first: bool,
    mut row: Value,
) -> Result<Option<Value>> {
    // Bound the read to the observed snapshot, even if a writer appends meanwhile.
    let mut reader = BufReader::new(file.take(limit));
    if discard_first {
        // The arbitrary byte offset may be inside a UTF-8 character. Discard the
        // incomplete first record as bytes, before attempting any text decoding.
        let mut discard = Vec::new();
        reader
            .read_until(b'\n', &mut discard)
            .map_err(|_| "session read failed")?;
    }
    let mut bytes = Vec::new();
    loop {
        bytes.clear();
        if reader
            .read_until(b'\n', &mut bytes)
            .map_err(|_| "session read failed")?
            == 0
        {
            break;
        }
        let line = match std::str::from_utf8(&bytes) {
            Ok(line) => line,
            // A live writer can be part-way through the last UTF-8 character.
            // Complete corrupt records and real I/O errors still fail the read.
            Err(error) if bytes.last() != Some(&b'\n') && error.error_len().is_none() => break,
            Err(_) => return Err("session read failed".into()),
        };
        if !line.contains("\"event_msg\"") {
            continue;
        }
        let Ok(e) = serde_json::from_str::<Value>(line) else {
            continue;
        };
        if s(&e["type"]) != "event_msg" {
            continue;
        }
        let p = &e["payload"];
        let at = epoch(&e["timestamp"]);
        if [
            "task_started",
            "task_complete",
            "turn_aborted",
            "token_count",
            "item_completed",
        ]
        .contains(&s(&p["type"]))
        {
            row["last_activity"] = json!(epoch(&row["last_activity"]).max(at));
        }
        match s(&p["type"]) {
            "task_started" => {
                if truth(&p["root_turn_id"]) && p["root_turn_id"] != p["turn_id"] {
                    continue;
                }
                row["turn_id"] = p["turn_id"].clone();
                row["completed_at"] = json!(at);
                row["verdict"] = json!("partial");
                row["final_excerpt"] = json!("");
            }
            "task_complete" => {
                if truth(&row["turn_id"]) && row["turn_id"] != p["turn_id"] {
                    continue;
                }
                let text = s(&p["last_agent_message"]);
                row["turn_id"] = p["turn_id"].clone();
                row["completed_at"] = json!(at);
                row["verdict"] = json!(if text.trim_start().starts_with("状態: 完了") {
                    "completed"
                } else if result_state(text) == "状態: 要判断" {
                    "needs_input"
                } else if result_state(text) == "状態: 継続中" || text.contains("未完了") {
                    "not_done"
                } else {
                    "partial"
                });
                row["final_excerpt"] = json!(safe_text(text));
            }
            "turn_aborted" => {
                if truth(&p["root_turn_id"]) && p["root_turn_id"] != p["turn_id"] {
                    continue;
                }
                if truth(&p["turn_id"]) && p["turn_id"] != row["turn_id"] {
                    continue;
                }
                row["completed_at"] = json!(at);
                row["verdict"] = json!("partial");
            }
            _ => {}
        }
    }
    Ok((epoch(&row["completed_at"]) > 0.).then_some(row))
}

pub fn sessions(root: &Path, since: f64) -> Result<Vec<Value>> {
    Ok(session_inventory(root, since)?.0)
}

fn count_issue(issues: &mut Value, reason: &str) {
    issues[reason] = json!(n(&issues[reason]) + 1);
}

fn summary_issue(error: &str) -> &'static str {
    match error {
        "session read failed" => "read",
        "invalid session metadata" => "invalid_metadata",
        "session unavailable" => "open",
        "session seek failed" => "seek",
        _ => "metadata",
    }
}

fn excluded_directory(ctx: &Context, cwd: &str, issues: &mut Value) -> Result<bool> {
    match protected(ctx, cwd) {
        Err(error) if error == "working directory unavailable" => {
            // Cleaned-up worktrees are expected in historical session records.
            // Exclude this row without weakening deny-list or ToDo read errors.
            count_issue(issues, "working_directory");
            Ok(true)
        }
        other => other,
    }
}

fn session_inventory(root: &Path, since: f64) -> Result<(Vec<Value>, Value)> {
    let root = root
        .canonicalize()
        .map_err(|_| "sessions root unavailable")?;
    let mut stack = vec![root.clone()];
    let mut rows = vec![];
    let mut issues = json!({});
    while let Some(dir) = stack.pop() {
        let entries = match std::fs::read_dir(dir) {
            Ok(entries) => entries,
            Err(_) => {
                count_issue(&mut issues, "directory");
                continue;
            }
        };
        for e in entries {
            let e = match e {
                Ok(e) => e,
                Err(_) => {
                    count_issue(&mut issues, "entry");
                    continue;
                }
            };
            let kind = match e.file_type() {
                Ok(kind) => kind,
                Err(_) => {
                    count_issue(&mut issues, "file_type");
                    continue;
                }
            };
            if kind.is_symlink() {
                continue;
            }
            let path = e.path();
            let resolved = match path.canonicalize() {
                Ok(path) => path,
                Err(_) => {
                    count_issue(&mut issues, "path");
                    continue;
                }
            };
            if !resolved.starts_with(&root) {
                continue;
            }
            if kind.is_dir() {
                stack.push(path);
            } else if path.extension().is_some_and(|x| x == "jsonl") {
                let modified = match e.metadata().and_then(|m| m.modified()) {
                    Ok(time) => time,
                    Err(_) => {
                        count_issue(&mut issues, "time");
                        continue;
                    }
                }
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_secs_f64();
                if modified < since {
                    continue;
                }
                match summary(&path) {
                    Ok(Some(row)) if epoch(&row["completed_at"]) >= since => rows.push(row),
                    Ok(_) => {}
                    Err(error) => {
                        // One unreadable transcript must not hide unrelated work.
                        // Keep it out of resume decisions and report only fixed
                        // reason codes/counts, never its path or transcript text.
                        count_issue(&mut issues, summary_issue(&error));
                    }
                }
            }
        }
    }
    rows.sort_by(|a, b| epoch(&a["completed_at"]).total_cmp(&epoch(&b["completed_at"])));
    Ok((rows, issues))
}

pub fn session_busy(path: &Path) -> bool {
    #[cfg(windows)]
    {
        use std::os::windows::fs::OpenOptionsExt;
        std::fs::OpenOptions::new()
            .read(true)
            .share_mode(1 | 4)
            .open(path)
            .is_err()
    }
    #[cfg(not(windows))]
    {
        let _ = path;
        true
    }
}

pub fn questions_match(row: &Value, q: &Value) -> bool {
    if s(&q["session_id"]) == s(&row["session_id"]) && q["turn_id"] == row["turn_id"] {
        return true;
    }
    if path_key(s(&q["cwd"])) != path_key(s(&row["cwd"])) {
        return false;
    }
    let text = s(&row["final_excerpt"]);
    let tid = s(&q["thread_id"]);
    let title = regex::Regex::new(r"^\[[^\]]+\]\s*")
        .unwrap()
        .replace(s(&q["title"]), "");
    (!tid.is_empty() && text.contains(tid))
        || (title.chars().count() >= 8 && text.contains(title.as_ref()))
}

/// A stopped session is resumed only while its owning ToDo is still doing.
/// Scheduler-owned work and explicit holds remain under their existing dispatcher.
pub fn allowed_todo(ctx: &Context, row: &Value, jobs: &[Value]) -> Result<bool> {
    let db = read_or(&ctx.todo, json!({"items":[]}))?;
    let owning: Vec<_> = jobs
        .iter()
        .filter(|j| [s(&j["thread_id"]), s(&j["resume_session"])].contains(&s(&row["session_id"])))
        .collect();
    Ok(db["items"].as_array().into_iter().flatten().any(|i| {
        n(&i["id"]) != 57
            && s(&i["status"]) == "doing"
            && !truth(&i["scheduler"])
            && i["jobs"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|id| owning.iter().any(|j| j["id"] == *id))
    }))
}

pub fn run_rows(
    ctx: &mut Context,
    rows: &[Value],
    processes: &[String],
    since: f64,
    dry: bool,
    budget: &mut bool,
) -> Result<Value> {
    let path = ctx.here.join("patrol_state.json");
    let mut state = read_or(&path, json!({"stops":{},"last_run":0}))?;
    if !state["stops"].is_object() {
        return Err("invalid patrol ledger".into());
    }
    let jobs = jobs(ctx)?;
    let questions = ctx.load()?;
    let mut report = json!({"eligible":[],"waiting":[],"launched":[],"needs_input":[],"completed":0,"session_read_errors":{}});
    let now = ctx.now();
    for row in rows {
        if epoch(&row["completed_at"]) < since
            || now - epoch(&row["completed_at"]).max(epoch(&row["last_activity"])) < 1200.
        {
            continue;
        }
        let cwd = s(&row["cwd"]);
        if cwd.is_empty() || excluded_directory(ctx, cwd, &mut report["session_read_errors"])? {
            continue;
        }
        let stop = key(&format!("{}|{}", s(&row["session_id"]), s(&row["turn_id"])));
        if truth(&state["stops"][&stop]["job"]) {
            if !dry {
                let originals: Vec<_> = jobs
                    .iter()
                    .filter(|j| {
                        [s(&j["thread_id"]), s(&j["resume_session"])]
                            .contains(&s(&row["session_id"]))
                    })
                    .map(|j| s(&j["id"]).to_string())
                    .collect();
                link_todos(ctx, &originals, s(&state["stops"][&stop]["job"]))?;
            }
            continue;
        }
        if truth(&state["stops"][&stop]["delivered"]) {
            continue;
        }
        if s(&row["verdict"]) == "completed" {
            report["completed"] = json!(n(&report["completed"]) + 1);
            continue;
        }
        if s(&row["verdict"]) == "needs_input" {
            let delivered = questions["questions"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|q| questions_match(row, q));
            if delivered {
                if !dry {
                    state["stops"][&stop] = json!({"delivered":true});
                    atomic_json(&path, &state, 2)?;
                }
            } else {
                report["needs_input"]
                    .as_array_mut()
                    .unwrap()
                    .push(json!({"session":row["session_id"],"cwd":cwd}));
            }
            continue;
        }
        if !allowed_todo(ctx, row, &jobs)? {
            continue;
        }
        let launch = key(&format!("patrol|{stop}|resume"));
        if let Some(jid) = find_launch_job(ctx, &launch) {
            if !dry {
                let originals: Vec<_> = jobs
                    .iter()
                    .filter(|j| {
                        [s(&j["thread_id"]), s(&j["resume_session"])]
                            .contains(&s(&row["session_id"]))
                    })
                    .map(|j| s(&j["id"]).to_string())
                    .collect();
                link_todos(ctx, &originals, &jid)?;
                state["stops"][&stop] = json!({"job":jid,"launch":launch});
                atomic_json(&path, &state, 2)?;
            }
            continue;
        }
        // Re-read immediately before launch: new turns invalidate an old decision.
        let fresh = match summary(Path::new(s(&row["session_path"]))) {
            Ok(fresh) => fresh,
            Err(error) => {
                count_issue(&mut report["session_read_errors"], summary_issue(&error));
                continue;
            }
        };
        if fresh.as_ref().is_none_or(|r| {
            r["turn_id"] != row["turn_id"] || r["last_activity"] != row["last_activity"]
        }) {
            continue;
        }
        if busy(cwd, &jobs, processes) || session_busy(Path::new(s(&row["session_path"]))) {
            report["waiting"]
                .as_array_mut()
                .unwrap()
                .push(row["session_id"].clone());
            continue;
        }
        report["eligible"]
            .as_array_mut()
            .unwrap()
            .push(row["session_id"].clone());
        if dry || *budget || cooldown(&jobs, now) {
            continue;
        }
        *budget = true;
        let prompt = "最初に C:/Users/makhmeto/.codex/AGENTS.md を読んでください。前回の作業が途中で止まりました。共有ToDoと現行の担当を照合し、停止原因を直して続けてください。同じ領域で失敗が続けば設計を見直してください。判断待ちの項目だけ止め、discord-owner-ask Skillに従って質問してください。新しいコードはRustで書き、PythonとAPIキーを使わずOAuthのサブスク枠を使ってください。client-projects.txtの案件とOpenMausBotには触らないでください。完了・判断・残作業を共有ToDoに記録し、最後のメッセージは日本語で、先頭は厳密に「状態: 完了」「状態: 継続中」「状態: 要判断」のいずれか1行だけにする。全作業完了は完了。未完了・cargoなどの資源待ちで、自動再開しオーナー入力が不要なら継続中。要判断はオーナーだけが決める場合に限り、本文に具体的な質問とa/bの選択肢・推奨案を書く。継続中はメンションも通知もせず投稿し、未完了の共有ToDoを停滞監視とスケジューラーが続ける。質問のない要判断は継続中として扱い、誤用をログに残す。";
        let selection = pick_model(ctx, prompt, &[], Some("live_debug"))?;
        state["stops"][&stop] =
            json!({"launch":launch,"action":"resume","session":row["session_id"]});
        atomic_json(&path, &state, 2)?;
        let fresh = match summary(Path::new(s(&row["session_path"]))) {
            Ok(fresh) => fresh,
            Err(error) => {
                count_issue(&mut report["session_read_errors"], summary_issue(&error));
                continue;
            }
        };
        let latest_jobs = crate::automation::jobs(ctx)?;
        let latest_processes = ctx.effects.codex_directories()?;
        if fresh.as_ref().is_none_or(|r| {
            r["turn_id"] != row["turn_id"]
                || r["last_activity"] != row["last_activity"]
                || r["verdict"] != row["verdict"]
        }) || excluded_directory(ctx, cwd, &mut report["session_read_errors"])?
            || !allowed_todo(ctx, row, &latest_jobs)?
            || busy(cwd, &latest_jobs, &latest_processes)
            || session_busy(Path::new(s(&row["session_path"])))
        {
            report["waiting"]
                .as_array_mut()
                .unwrap()
                .push(row["session_id"].clone());
            continue;
        }
        // A failed transport can still have launched the child; reuse its stable key.
        let result = launch_job_with_session(
            ctx,
            &json!({"id":row["session_id"]}),
            prompt,
            &selection,
            &launch,
            false,
            Some(cwd),
            Some("patrol"),
            Some(s(&row["session_id"])),
        );
        let jid = match result {
            Ok(j) => j,
            Err(e) => find_launch_job(ctx, &launch).ok_or(e)?,
        };
        state["stops"][&stop]["job"] = json!(jid);
        atomic_json(&path, &state, 2)?;
        let originals: Vec<_> = jobs
            .iter()
            .filter(|j| {
                [s(&j["thread_id"]), s(&j["resume_session"])].contains(&s(&row["session_id"]))
            })
            .map(|j| s(&j["id"]).to_string())
            .collect();
        link_todos(ctx, &originals, &jid)?;
        report["launched"].as_array_mut().unwrap().push(json!(jid));
    }
    if !dry {
        state["last_run"] = json!(now);
        atomic_json(&path, &state, 2)?;
    }
    Ok(report)
}

pub fn run(ctx: &mut Context, root: &Path, dry: bool, budget: &mut bool) -> Result<Value> {
    if !dry {
        reject_legacy_launch()?;
    }
    let Some(since) = enabled(ctx)? else {
        return Ok(json!({"disabled":true}));
    };
    let _lock = if dry {
        None
    } else {
        Some(ByteLock::acquire(
            &ctx.here.join("patrol.lock"),
            Duration::from_secs(60),
        )?)
    };
    let state = read_or(&ctx.here.join("patrol_state.json"), json!({}))?;
    if !dry && ctx.now() - epoch(&state["last_run"]) < 600. {
        return Ok(json!({"not_due":true}));
    }
    let (rows, issues) = session_inventory(root, since)?;
    let processes = ctx.effects.codex_directories()?;
    let mut report = run_rows(ctx, &rows, &processes, since, dry, budget)?;
    for (reason, count) in issues.as_object().unwrap() {
        report["session_read_errors"][reason] =
            json!(n(&report["session_read_errors"][reason]) + n(count));
    }
    Ok(report)
}

pub fn sessions_root() -> std::path::PathBuf {
    std::env::var_os("CODEX_HOME")
        .map(std::path::PathBuf::from)
        .unwrap_or_else(|| {
            std::path::PathBuf::from(std::env::var_os("USERPROFILE").unwrap_or_default())
                .join(".codex")
        })
        .join("sessions")
}

#[cfg(windows)]
pub fn codex_directories() -> Result<Vec<String>> {
    use windows_sys::Win32::{
        Foundation::*,
        System::{Diagnostics::Debug::ReadProcessMemory, Diagnostics::ToolHelp::*, Threading::*},
    };
    #[repr(C)]
    struct Basic {
        reserved: usize,
        peb: usize,
        reserved2: [usize; 2],
        pid: usize,
        reserved3: usize,
    }
    #[link(name = "ntdll")]
    unsafe extern "system" {
        fn NtQueryInformationProcess(
            h: HANDLE,
            class: u32,
            buf: *mut Basic,
            len: u32,
            ret: *mut u32,
        ) -> i32;
    }
    struct Handle(HANDLE);
    impl Drop for Handle {
        fn drop(&mut self) {
            unsafe {
                CloseHandle(self.0);
            }
        }
    }
    unsafe fn directory(pid: u32) -> Option<String> {
        let h = OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, 0, pid);
        if h.is_null() {
            return None;
        }
        let _h = Handle(h);
        let mut b: Basic = std::mem::zeroed();
        if NtQueryInformationProcess(
            h,
            0,
            &mut b,
            std::mem::size_of::<Basic>() as u32,
            std::ptr::null_mut(),
        ) != 0
        {
            return None;
        }
        let read = |address: usize, size: usize| -> Option<Vec<u8>> {
            let mut out = vec![0; size];
            let mut used = 0;
            (ReadProcessMemory(h, address as _, out.as_mut_ptr() as _, size, &mut used) != 0
                && used == size)
                .then_some(out)
        };
        let params = usize::from_le_bytes(read(b.peb + 0x20, 8)?.try_into().ok()?);
        let raw = read(params + 0x38, 16)?;
        let len = u16::from_le_bytes(raw[..2].try_into().ok()?) as usize;
        if len == 0 || !len.is_multiple_of(2) {
            return None;
        }
        let address = usize::from_le_bytes(raw[8..16].try_into().ok()?);
        let data = read(address, len)?;
        String::from_utf16(
            &data
                .as_chunks::<2>()
                .0
                .iter()
                .map(|c| u16::from_le_bytes([c[0], c[1]]))
                .collect::<Vec<_>>(),
        )
        .ok()
    }
    unsafe {
        let snap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
        if snap == INVALID_HANDLE_VALUE {
            return Err("process inventory unavailable".into());
        }
        let _snap = Handle(snap);
        let mut e: PROCESSENTRY32W = std::mem::zeroed();
        e.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;
        if Process32FirstW(snap, &mut e) == 0 {
            return Err("process inventory unavailable".into());
        }
        let mut out = vec![];
        loop {
            let len = e
                .szExeFile
                .iter()
                .position(|c| *c == 0)
                .unwrap_or(e.szExeFile.len());
            if String::from_utf16_lossy(&e.szExeFile[..len]).eq_ignore_ascii_case("codex.exe") {
                out.push(directory(e.th32ProcessID).unwrap_or_default());
            }
            if Process32NextW(snap, &mut e) == 0 {
                if GetLastError() != ERROR_NO_MORE_FILES {
                    return Err("incomplete process inventory".into());
                }
                break;
            }
        }
        Ok(out)
    }
}
#[cfg(not(windows))]
pub fn codex_directories() -> Result<Vec<String>> {
    Err("Windows process inventory required".into())
}

#[cfg(test)]
mod reader_tests {
    use super::*;
    use std::io::{self, Cursor};

    fn row() -> Value {
        json!({"completed_at":0,"last_activity":0,"turn_id":"","verdict":"partial","final_excerpt":""})
    }

    fn event(kind: &str, at: &str) -> String {
        json!({"type":"event_msg","timestamp":at,"payload":{"type":kind,"turn_id":"t1","last_agent_message":"状態: 完了"}}).to_string() + "\n"
    }

    #[test]
    fn captured_length_excludes_append_and_truncated_tail_keeps_last_complete_event() {
        let start = event("task_started", "2026-10-02T18:00:00Z");
        let complete = event("task_complete", "2026-10-02T19:00:00Z");
        let combined = start.clone() + &complete;
        let bounded = read_tail(Cursor::new(&combined), start.len() as u64, false, row())
            .unwrap()
            .unwrap();
        assert_eq!(bounded["verdict"], "partial");
        assert_eq!(bounded["turn_id"], "t1");
        assert_eq!(bounded["completed_at"], json!(1790964000.0));
        assert_eq!(bounded["last_activity"], json!(1790964000.0));
        let truncated = &combined.as_bytes()[..start.len() + complete.len() / 2];
        assert_eq!(
            read_tail(Cursor::new(truncated), combined.len() as u64, false, row())
                .unwrap()
                .unwrap(),
            bounded
        );
        let full = read_tail(Cursor::new(&combined), combined.len() as u64, false, row())
            .unwrap()
            .unwrap();
        assert_eq!(full["verdict"], "completed");
        assert_eq!(full["completed_at"], json!(1790967600.0));
    }

    #[test]
    fn io_errors_after_an_event_or_during_discard_are_not_swallowed() {
        struct Failing(Cursor<Vec<u8>>);
        impl Read for Failing {
            fn read(&mut self, buffer: &mut [u8]) -> io::Result<usize> {
                let count = self.0.read(buffer)?;
                if count == 0 {
                    Err(io::Error::other("injected I/O failure"))
                } else {
                    Ok(count)
                }
            }
        }
        for (bytes, discard) in [
            (
                event("task_started", "2026-10-02T18:00:00Z").into_bytes(),
                false,
            ),
            (vec![0x97, 0xa5], true),
        ] {
            assert_eq!(
                read_tail(Failing(Cursor::new(bytes)), 1024 * 1024, discard, row()).unwrap_err(),
                "session read failed"
            );
        }
    }
}
