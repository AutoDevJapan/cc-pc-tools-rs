use cc_todo::*;
use clap::Parser;
use serde_json::{Value, json};
use std::os::windows::fs::OpenOptionsExt;
use std::os::windows::process::CommandExt;
use std::{
    fs,
    path::{Path, PathBuf},
    process::{Command as Process, Stdio},
    time::Duration,
};

const CREATE_NO_WINDOW: u32 = 0x08000000;
fn rename_fixture() -> Value {
    json!({"projects":{"old":{"path":"C:/old","unknown":[1,2]},"other":"C:/other"},
    "items":[{"id":1,"project":"old","title":"【old】Re-Xの画面を直す","note":"oldの記録 https://host/old old_API C:/old/a","notes":"oldを確認","status":"doing","jobs":["active"],"progress":50,"wait_on":[7],"updated":"unchanged","spec":"C:/old/spec.md","scheduler":{"key":"old-key","unknown":true},"unknown":[1]},
    {"id":2,"project":"other","title":"oldの連携","note":"old","jobs":[],"status":"done"}],
    "stalls":{"old":{"last":123,"pending":"old-id"},"other":{"last":456}},"next_id":88,"unknown":{"old":"keep"}})
}
#[test]
fn rename_project_keeps_every_non_display_field_and_other_project() {
    let temp = Temp::new();
    let original = rename_fixture();
    save(&temp.paths().store, &original).unwrap();
    let args = ["rename-project", "old", "Re:X"];
    let before = fs::read(&temp.paths().store).unwrap();
    assert!(
        rust_command(&temp.0, &["rename-project", "old", "Re:X", "--dry-run"])
            .status
            .success()
    );
    assert_eq!(fs::read(&temp.paths().store).unwrap(), before);
    let out = rust_command(&temp.0, &args);
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let counts: Value = serde_json::from_slice(&out.stdout).unwrap();
    assert_eq!(counts["items_renamed"], 1);
    assert_eq!(counts["fields_changed"], 3);
    let mut expected = original;
    expected["projects"]["Re:X"] = expected["projects"]
        .as_object_mut()
        .unwrap()
        .remove("old")
        .unwrap();
    expected["stalls"]["Re:X"] = expected["stalls"]
        .as_object_mut()
        .unwrap()
        .remove("old")
        .unwrap();
    expected["items"][0]["project"] = json!("Re:X");
    expected["items"][0]["title"] = json!("【Re:X】Re:Xの画面を直す");
    expected["items"][0]["note"] = json!("Re:Xの記録 https://host/old old_API C:/old/a");
    expected["items"][0]["notes"] = json!("Re:Xを確認");
    assert_eq!(load(&temp.paths().store).unwrap(), expected);
    assert!(rust_command(&temp.0, &args).status.success());
    assert_eq!(load(&temp.paths().store).unwrap(), expected);
}
#[test]
fn rename_collision_and_malformed_selected_text_do_not_write() {
    let temp = Temp::new();
    for malformed in [false, true] {
        let mut db = rename_fixture();
        if malformed {
            db["items"][0]["notes"] = json!(["keep"]);
        } else {
            db["projects"]["Re:X"] = json!("C:/different");
        }
        save(&temp.paths().store, &db).unwrap();
        let before = fs::read(&temp.paths().store).unwrap();
        assert!(
            !rust_command(&temp.0, &["rename-project", "old", "Re:X"])
                .status
                .success()
        );
        assert_eq!(fs::read(&temp.paths().store).unwrap(), before);
    }
}
#[test]
fn rename_only_updates_authorized_github_urls_in_selected_notes() {
    let temp = Temp::new();
    let mut db = rename_fixture();
    let note = "https://github.com/Org/old/pull/1 https://api.github.com/repos/Org/old C:/old/a";
    db["items"][0]["note"] = json!(note);
    db["items"][1]["note"] = json!(note);
    save(&temp.paths().store, &db).unwrap();
    let before = fs::read(&temp.paths().store).unwrap();
    assert!(
        !rust_command(
            &temp.0,
            &[
                "rename-project",
                "old",
                "Re:X",
                "--github-from",
                "invalid",
                "--github-to",
                "Org/Re-X"
            ]
        )
        .status
        .success()
    );
    assert_eq!(fs::read(&temp.paths().store).unwrap(), before);
    let out = rust_command(
        &temp.0,
        &[
            "rename-project",
            "old",
            "Re:X",
            "--github-from",
            "Org/old",
            "--github-to",
            "Org/Re-X",
        ],
    );
    assert!(out.status.success());
    assert_eq!(
        serde_json::from_slice::<Value>(&out.stdout).unwrap()["github_fields_changed"],
        1
    );
    let renamed = load(&temp.paths().store).unwrap();
    assert_eq!(
        renamed["items"][0]["note"],
        "https://github.com/Org/Re-X/pull/1 https://api.github.com/repos/Org/old C:/old/a"
    );
    assert_eq!(renamed["items"][1], db["items"][1]);
}
#[test]
fn rename_missing_optional_stalls_keeps_unknown_fields_and_backs_up_only_changes() {
    let temp = Temp::new();
    let db = json!({"projects":{"old":"C:/old","other":"C:/other"},"items":[{"id":1,"project":"old","title":"oldの作業"}],"unknown":99});
    save(&temp.paths().store, &db).unwrap();
    let before = fs::read(&temp.paths().store).unwrap();
    for _ in 0..2 {
        assert!(
            rust_command(&temp.0, &["rename-project", "old", "Re:X"])
                .status
                .success()
        );
    }
    let renamed = load(&temp.paths().store).unwrap();
    assert!(renamed.get("stalls").is_none());
    assert_eq!(renamed["unknown"], 99);
    let backups: Vec<_> = fs::read_dir(&temp.0)
        .unwrap()
        .flatten()
        .filter(|e| {
            e.file_name()
                .to_string_lossy()
                .starts_with("todo.before-rename-")
        })
        .collect();
    assert_eq!(backups.len(), 1);
    assert_eq!(fs::read(backups[0].path()).unwrap(), before);
}
#[test]
fn legacy_project_registration_and_add_cannot_restore_historical_display_names() {
    let temp = Temp::new();
    let former = concat!("x", "LIFE");
    assert!(
        rust_command(&temp.0, &["project", former, "C:/old"])
            .status
            .success()
    );
    assert!(
        rust_command(
            &temp.0,
            &["add", "Re-X", "Re-Xの作業", "--note", "Re-Xを確認"]
        )
        .status
        .success()
    );
    let db = load(&temp.paths().store).unwrap();
    assert!(db["projects"].get(former).is_none());
    assert!(db["projects"].get("Re-X").is_none());
    assert_eq!(db["projects"]["Re:X"], "C:/old");
    assert_eq!(db["items"][0]["project"], "Re:X");
    assert_eq!(db["items"][0]["title"], "Re:Xの作業");
    assert_eq!(db["items"][0]["note"], "Re:Xを確認");
}
#[test]
fn rename_stall_collision_is_rejected_and_rerun_moves_a_remaining_source_stall() {
    let temp = Temp::new();
    let mut db = rename_fixture();
    db["stalls"]["Re:X"] = json!({"keep":true});
    save(&temp.paths().store, &db).unwrap();
    let before = fs::read(&temp.paths().store).unwrap();
    assert!(
        !rust_command(&temp.0, &["rename-project", "old", "Re:X"])
            .status
            .success()
    );
    assert_eq!(fs::read(&temp.paths().store).unwrap(), before);
    db["stalls"].as_object_mut().unwrap().shift_remove("Re:X");
    let project = db["projects"]
        .as_object_mut()
        .unwrap()
        .shift_remove("old")
        .unwrap();
    db["projects"]["Re:X"] = project;
    db["items"][0]["project"] = json!("Re:X");
    save(&temp.paths().store, &db).unwrap();
    assert!(
        rust_command(&temp.0, &["rename-project", "old", "Re:X"])
            .status
            .success()
    );
    let renamed = load(&temp.paths().store).unwrap();
    assert_eq!(renamed["stalls"]["Re:X"], db["stalls"]["old"]);
    assert!(renamed["stalls"].get("old").is_none());
}
#[test]
fn rename_waits_for_lock_and_reads_the_latest_concurrent_update() {
    let temp = Temp::new();
    let paths = temp.paths();
    save(&paths.store, &rename_fixture()).unwrap();
    let lock = Lock::acquire(&paths.lock, Duration::ZERO).unwrap();
    let mut child = Process::new(env!("CARGO_BIN_EXE_todo"));
    child
        .creation_flags(CREATE_NO_WINDOW)
        .env("CC_TODO_HOME", &temp.0)
        .args(["rename-project", "old", "Re:X"])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    let mut child = child.spawn().unwrap();
    std::thread::sleep(Duration::from_millis(250));
    assert!(
        child.try_wait().unwrap().is_none(),
        "rename did not wait for the store lock"
    );
    let mut current = load(&paths.store).unwrap();
    current["items"][0]["jobs"] = json!(["active", "concurrent"]);
    current["items"][0]["progress"] = json!(75);
    current["items"][1]["note"] = json!("concurrent other project update");
    save(&paths.store, &current).unwrap();
    drop(lock);
    let out = child.wait_with_output().unwrap();
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let renamed = load(&paths.store).unwrap();
    assert_eq!(renamed["items"][0]["jobs"], current["items"][0]["jobs"]);
    assert_eq!(renamed["items"][0]["progress"], 75);
    assert_eq!(renamed["items"][1], current["items"][1]);
    assert_eq!(renamed["items"][0]["project"], "Re:X");
}
#[test]
fn rust_lock_child() {
    let Some(path) = std::env::var_os("CC_LOCK_PROBE") else {
        return;
    };
    let _guard = Lock::acquire(Path::new(&path), Duration::ZERO).unwrap();
    if let Some(ready) = std::env::var_os("CC_LOCK_READY") {
        fs::write(ready, "ready").unwrap();
        std::thread::sleep(Duration::from_secs(30));
    }
}
#[test]
fn cli_state_transitions_preserve_jobs_notes_and_failed_writes() {
    let temp = Temp::new();
    for args in [
        vec!["project", "a", "C:/repo"],
        vec!["add", "a", "日本語", "--note", "メモ"],
        vec!["start", "1", "--job", "old"],
        vec!["block", "1", "待ち"],
        vec!["start", "1", "--job", "new"],
        vec!["done", "1", "--note", "完了"],
    ] {
        let out = rust_command(&temp.0, &args);
        assert!(
            out.status.success(),
            "{:?}",
            String::from_utf8_lossy(&out.stderr)
        );
    }
    let d = load(&temp.paths().store).unwrap();
    assert_eq!(d["items"][0]["status"], "done");
    assert_eq!(d["items"][0]["jobs"], json!(["old", "new"]));
    assert_eq!(d["items"][0]["note"], "完了");
    for args in [
        vec!["start", "-1"],
        vec!["done", "999"],
        vec!["add", "bad", "x"],
    ] {
        let before = fs::read(&temp.paths().store).unwrap();
        assert!(!rust_command(&temp.0, &args).status.success());
        assert_eq!(fs::read(&temp.paths().store).unwrap(), before);
    }
}
#[test]
fn cli_wait_persists_repeated_dependencies_and_done_releases_after_reload() {
    let temp = Temp::new();
    for args in [
        vec!["project", "a", "C:/repo"],
        vec!["add", "a", "依存元"],
        vec!["add", "a", "待ち先1"],
        vec!["add", "a", "待ち先2"],
        vec!["start", "1", "--job", "old", "--progress", "50"],
        vec!["wait", "1", "--on", "2", "--on", "3"],
    ] {
        assert!(rust_command(&temp.0, &args).status.success());
    }
    let d = load(&temp.paths().store).unwrap();
    assert_eq!(d["items"][0]["wait_on"], json!([2, 3]));
    assert_eq!(d["items"][0]["status"], "wait");
    let before = fs::read(&temp.paths().store).unwrap();
    let listed = rust_command(&temp.0, &["list"]);
    assert!(listed.status.success());
    assert!(String::from_utf8_lossy(&listed.stdout).contains("ToDo 2 待ち"));
    assert_eq!(before, fs::read(&temp.paths().store).unwrap());
    assert!(
        !rust_command(&temp.0, &["wait", "2", "--on", "1"])
            .status
            .success()
    );
    assert_eq!(before, fs::read(&temp.paths().store).unwrap());
    assert!(rust_command(&temp.0, &["done", "2"]).status.success());
    assert_eq!(
        load(&temp.paths().store).unwrap()["items"][0]["status"],
        "wait"
    );
    assert!(rust_command(&temp.0, &["done", "3"]).status.success());
    let d = load(&temp.paths().store).unwrap();
    assert_eq!(d["items"][0]["status"], "todo");
    assert_eq!(d["items"][0]["dependency_ready"], true);
    assert_eq!(d["items"][0]["jobs"], json!(["old"]));
    assert_eq!(d["items"][0]["progress"], 50);
}
#[test]
fn save_retries_real_windows_sharing_violation_until_handle_released() {
    let t = Temp::new();
    let p = t.paths();
    save(&p.store, &empty()).unwrap();
    // Permit reads/writes, but deliberately deny FILE_SHARE_DELETE: MoveFileEx must fail.
    let handle = fs::OpenOptions::new()
        .read(true)
        .share_mode(3)
        .open(&p.store)
        .unwrap();
    let store = p.store.clone();
    let mut replacement = empty();
    replacement["marker"] = json!("replacement after release");
    let expected = replacement.clone();
    let (tx, rx) = std::sync::mpsc::channel();
    let worker = std::thread::spawn(move || tx.send(save(&store, &replacement)).unwrap());
    let tmp = p
        .store
        .with_file_name(format!("todo.json.{}.tmp", std::process::id()));
    let deadline = std::time::Instant::now() + Duration::from_secs(5);
    while !tmp.exists() {
        assert!(std::time::Instant::now() < deadline);
        std::thread::sleep(Duration::from_millis(5));
    }
    assert!(
        matches!(
            rx.recv_timeout(Duration::from_millis(150)),
            Err(std::sync::mpsc::RecvTimeoutError::Timeout)
        ),
        "save must retry while the handle denies replacement"
    );
    assert_eq!(load(&p.store).unwrap(), empty());
    drop(handle);
    rx.recv_timeout(Duration::from_secs(5)).unwrap().unwrap();
    worker.join().unwrap();
    assert_eq!(load(&p.store).unwrap(), expected);
    assert!(!tmp.exists());
}
#[test]
fn save_sharing_retry_exhaustion_preserves_original_and_temporary() {
    let t = Temp::new();
    let p = t.paths();
    save(&p.store, &empty()).unwrap();
    let handle = fs::OpenOptions::new()
        .read(true)
        .share_mode(3)
        .open(&p.store)
        .unwrap();
    let mut replacement = empty();
    replacement["marker"] = json!("not committed");
    let error = save(&p.store, &replacement).unwrap_err();
    assert!(
        error.contains("os error 32")
            || error.contains("os error 33")
            || error.contains("os error 5"),
        "{error}"
    );
    assert_eq!(load(&p.store).unwrap(), empty());
    let tmp = p
        .store
        .with_file_name(format!("todo.json.{}.tmp", std::process::id()));
    assert_eq!(load(&tmp).unwrap(), replacement);
    drop(handle);
}
#[test]
fn normalized_trailing_directory_separators_and_drive_roots_match_python() {
    assert_eq!(normalized("C:/Project/"), "c:\\project");
    assert_eq!(normalized("C:\\Project\\\\"), "c:\\project");
    assert_eq!(normalized("C:/"), "c:\\");
    assert_eq!(normalized("C:\\"), "c:\\");
    assert_eq!(normalized("C:/project/sub/../"), "c:\\project");
}
struct Temp(PathBuf);
impl Temp {
    fn new() -> Self {
        let p = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join(".test-tmp")
            .join(uuid::Uuid::new_v4().simple().to_string());
        fs::create_dir_all(&p).unwrap();
        Self(p)
    }
    fn paths(&self) -> Paths {
        Paths::at(&self.0, &self.0.join("jobs"))
    }
}
impl Drop for Temp {
    fn drop(&mut self) {
        fs::remove_dir_all(&self.0).unwrap();
    }
}
fn cli(args: &[&str]) -> Command {
    Cli::try_parse_from(std::iter::once("todo").chain(args.iter().copied()))
        .unwrap()
        .command
}
fn empty() -> Value {
    json!({"projects":{},"items":[],"next_id":1,"stalls":{}})
}
fn db() -> Value {
    json!({"projects":{"a":"A","b":"B","c":"C","owner":"O"},"items":[
    {"id":1,"project":"a","title":"x","note":"","jobs":[],"status":"todo"},
    {"id":2,"project":"b","title":"y","note":"","jobs":[],"status":"doing"},
    {"id":3,"project":"c","title":"z","note":"","jobs":[],"status":"block"},
    {"id":4,"project":"c","title":"w","note":"","jobs":[],"status":"done"},
    {"id":5,"project":"owner","title":"owner","note":"","jobs":[],"status":"todo"}
],"next_id":6,"stalls":{}})
}
#[derive(Default)]
struct Fake {
    calls: Vec<(String, String, Value)>,
    notices: Vec<(String, bool)>,
    fail: bool,
}
impl Api for Fake {
    fn call(&mut self, m: &str, p: &str, b: &Value) -> Result<Value> {
        self.calls.push((m.into(), p.into(), b.clone()));
        if self.fail {
            Err("fake failure".into())
        } else {
            Ok(json!({"id":"123"}))
        }
    }
}
impl Notifier for Fake {
    fn notify(&mut self, _: &Paths, text: &str, mention: bool) -> Result<()> {
        self.notices.push((text.into(), mention));
        if self.fail {
            Err("fake failure".into())
        } else {
            Ok(())
        }
    }
}

#[test]
fn original_python_stalled_test() {
    // Full port of test_todo.py: b runs; c only has blocked/done items.
    let got = stalled(&db(), 0., |path| path == "B")
        .into_iter()
        .map(|(n, _)| n)
        .collect::<Vec<_>>();
    assert_eq!(got, vec!["a"]);
}
#[test]
fn existing_maintenance_thread_is_created_once_and_reopened() {
    let t = Temp::new();
    let mut f = Fake::default();
    let p = t.paths();
    let cfg = json!({"task_channel_id":"456"});
    assert_eq!(stall_thread(&mut f, &cfg, &p.thread).unwrap(), "123");
    assert_eq!(stall_thread(&mut f, &cfg, &p.thread).unwrap(), "123");
    assert_eq!(fs::read_to_string(&p.thread).unwrap(), "123");
    assert_eq!(f.calls.len(), 2);
    assert_eq!(f.calls[0].0, "POST");
    assert_eq!(f.calls[0].1, "/channels/456/threads");
    assert_eq!(
        f.calls[0].2,
        json!({"name":"【PCツール】止まった作業の再開記録","message":{"content":"止まった作業の終了と再開を、通知を出さずに記録します。","flags":4096,"allowed_mentions":{"parse":[]}}})
    );
    assert_eq!(
        f.calls[1],
        (
            "PATCH".into(),
            "/channels/123".into(),
            json!({"archived":false})
        )
    );
}
#[test]
fn failed_thread_create_leaves_no_cache() {
    let t = Temp::new();
    let mut f = Fake {
        fail: true,
        ..Default::default()
    };
    assert!(stall_thread(&mut f, &json!({"task_channel_id":"456"}), &t.paths().thread).is_err());
    assert!(!t.paths().thread.exists());
}
#[test]
fn unicode_chunking_python_semantics() {
    assert_eq!(chunks("", 2000), Vec::<String>::new());
    assert_eq!(
        chunks("ab\ncd\n123456", 5),
        vec!["ab\n", "cd\n", "12345", "6"]
    );
    assert_eq!(
        chunks("ab\r\ncd\u{2028}ef", 4),
        vec!["ab\r\n", "cd\u{2028}", "ef"]
    );
}
#[test]
fn add_spec_freezes_bom_newlines_deduplicates_and_retains_unknown() {
    let t = Temp::new();
    let p = t.paths();
    let source = t.0.join("input.md");
    fs::write(&source, "\u{feff}日本語\r\nline\rfinal").unwrap();
    let mut d = empty();
    d["projects"] = json!({"a":"A"});
    d["scheduler_meta"] = json!({"keep":true});
    let args = cli(&[
        "add",
        "a",
        "title",
        "--note",
        "note",
        "--spec",
        source.to_str().unwrap(),
        "--model",
        "gpt-6",
        "--effort",
        "max",
        "--needs",
        "cargo",
        "--needs",
        "cargo",
        "--needs",
        "civ6",
    ]);
    assert_eq!(add_item(&mut d, &args, &p).unwrap(), 1);
    let i = &d["items"][0];
    assert_eq!(i["needs"], json!(["cargo", "civ6"]));
    assert_eq!(i["model"], "gpt-6");
    assert_eq!(i["effort"], "max");
    assert_eq!(i["dispatchable"], true);
    let frozen = PathBuf::from(i["spec"].as_str().unwrap());
    assert_eq!(fs::read_to_string(&frozen).unwrap(), "日本語\nline\nfinal");
    fs::write(&source, "changed").unwrap();
    assert_eq!(fs::read_to_string(&frozen).unwrap(), "日本語\nline\nfinal");
    assert_eq!(d["scheduler_meta"], json!({"keep":true}));
    assert_eq!(d["next_id"], 2);
}
#[test]
fn add_errors_are_transactional() {
    let t = Temp::new();
    let p = t.paths();
    let mut d = empty();
    d["projects"] = json!({"a":"A"});
    for args in [
        vec!["add", "missing", "x"],
        vec!["add", "a", "x", "--model", "bad/model"],
        vec!["add", "a", "x", "--needs", "cargo"],
        vec!["add", "a", "x", "--effort", "high"],
    ] {
        let before = d.clone();
        assert_eq!(
            add_item(&mut d, &cli(&args), &p).unwrap_err(),
            "add failed (ValueError)"
        );
        assert_eq!(d, before);
    }
    let src = t.0.join("s.md");
    for text in [
        vec![],
        b" \r\n\t".to_vec(),
        vec![b'x'; 1024 * 1024 + 1],
        vec![0xff],
    ] {
        fs::write(&src, text).unwrap();
        let before = d.clone();
        assert!(
            add_item(
                &mut d,
                &cli(&["add", "a", "x", "--spec", src.to_str().unwrap()]),
                &p
            )
            .is_err()
        );
        assert_eq!(d, before);
    }
    assert_eq!(
        add_item(
            &mut d,
            &cli(&[
                "add",
                "a",
                "x",
                "--spec",
                t.0.join("missing").to_str().unwrap()
            ]),
            &p
        )
        .unwrap_err(),
        "add failed (FileNotFoundError)"
    );
}
#[test]
fn plain_add_never_authorizes_and_empty_spec_matches_python() {
    let t = Temp::new();
    let mut d = empty();
    d["projects"] = json!({"a":"A"});
    add_item(
        &mut d,
        &cli(&["add", "a", "x", "--spec", "", "--model", ""]),
        &t.paths(),
    )
    .unwrap();
    assert!(d["items"][0].get("dispatchable").is_none());
    assert!(d["items"][0].get("needs").is_none());
}
#[test]
fn save_backup_once_unicode_unknown_and_no_tmp() {
    let t = Temp::new();
    let p = t.paths();
    fs::write(&p.store, "{\"projects\":{},\"items\":[],\"next_id\":1}").unwrap();
    let old = fs::read(&p.store).unwrap();
    let mut d = load(&p.store).unwrap();
    d["unknown"] = json!({"日本語":[1,true,null]});
    save(&p.store, &d).unwrap();
    let bak = PathBuf::from(format!("{}.bak-20260930d", p.store.display()));
    assert_eq!(fs::read(&bak).unwrap(), old);
    assert_eq!(load(&p.store).unwrap(), d);
    save(&p.store, &empty()).unwrap();
    assert_eq!(fs::read(&bak).unwrap(), old);
    assert!(
        !fs::read_dir(&t.0)
            .unwrap()
            .flatten()
            .any(|e| e.path().extension().is_some_and(|x| x == "tmp"))
    );
}
#[test]
fn large_state_roundtrip_all_fields_without_production_data() {
    // Exercise a complete large store using synthetic data on every host.
    // Production names and notes must never be copied into assertion output.
    let mut original = json!({"projects":{"PROBE":"C:/probe","probe":"C:/other"},"next_id":402,
        "stalls":{"probe":{"since":1234.5678901234567}},
        "unknown":{"日本語":["未知",null,true,{"integer":u64::MAX,"fraction":0.12345678901234566}]},"items":[]});
    for id in 1..=401 {
        original["items"]
            .as_array_mut()
            .unwrap()
            .push(json!({"id":id,"project":"probe",
            "title":format!("合成作業{id}"),"status":"doing","jobs":[format!("synthetic-{id}")],
            "note":"保存するUnicodeのメモ","progress":id%101,
            "scheduler":{"attempt":id,"worktree":format!("C:/probe-wt/{id}")},
            "extra":{"counter":id*123,"negative_zero":-0.0,"nested":[null,{"保持":true}]}}));
    }
    let t = Temp::new();
    save(&t.paths().store, &original).unwrap();
    assert_eq!(load(&t.paths().store).unwrap(), original);
    assert_eq!(original["items"].as_array().unwrap().len(), 401);
    assert_eq!(
        load(&t.paths().store).unwrap()["items"][0]["extra"]["negative_zero"]
            .as_f64()
            .unwrap()
            .to_bits(),
        (-0.0f64).to_bits()
    );
}
#[test]
fn job_filter_case_paths_worktrees_corrupt_and_display() {
    let t = Temp::new();
    let jobs = t.0.join("jobs");
    fs::create_dir(&jobs).unwrap();
    for (id, status, cwd) in [
        ("a", "running", "C:/project"),
        ("b", "queued", "c:/PROJECT/sub"),
        ("c", "starting", "C:/project-wt2/branch"),
        ("d", "done", "C:/project"),
        ("e", "running", "C:/project-other"),
        ("f", "running", "C:/projects"),
    ] {
        fs::create_dir(jobs.join(id)).unwrap();
        fs::write(
            jobs.join(id).join("job.json"),
            json!({"id":id,"status":status,"cwd":cwd}).to_string(),
        )
        .unwrap();
    }
    fs::create_dir(jobs.join("bad")).unwrap();
    fs::write(jobs.join("bad/job.json"), "bad").unwrap();
    let mut ids = running_jobs(&jobs, "C:\\project")
        .iter()
        .map(|i| i["id"].as_str().unwrap().to_owned())
        .collect::<Vec<_>>();
    ids.sort();
    assert_eq!(ids, vec!["a", "b", "c"]);
    assert_eq!(job_cwd(&jobs, "missing"), "");
    assert_eq!(job_cwd(&jobs, "bad"), "");
    fs::write(
        jobs.join("a/job.json"),
        json!({"cwd":"c:\\users\\makhmeto\\desktop\\dev\\ADJ\\repo-wt\\work"}).to_string(),
    )
    .unwrap();
    assert_eq!(job_cwd(&jobs, "a"), "repo-wt/work");
}
fn rust_command(home: &Path, args: &[&str]) -> std::process::Output {
    assert!(
        !args.contains(&"watch"),
        "no production network from subprocess tests"
    );
    Process::new(env!("CARGO_BIN_EXE_todo"))
        .creation_flags(CREATE_NO_WINDOW)
        .env("CC_TODO_HOME", home)
        .env("CC_TODO_JOBS", home.join("jobs"))
        .args(args)
        .output()
        .unwrap()
}
#[test]
fn list_never_rewrites_or_creates_state_backup() {
    let t = Temp::new();
    let p = t.paths();
    fs::write(&p.store, serde_json::to_vec(&db()).unwrap()).unwrap();
    let bytes = fs::read(&p.store).unwrap();
    let modified = fs::metadata(&p.store).unwrap().modified().unwrap();
    assert!(rust_command(&t.0, &["list", "--all"]).status.success());
    assert_eq!(fs::read(&p.store).unwrap(), bytes);
    assert_eq!(
        fs::metadata(&p.store).unwrap().modified().unwrap(),
        modified
    );
    assert!(!PathBuf::from(format!("{}.bak-20260930d", p.store.display())).exists());
    let empty = Temp::new();
    assert!(rust_command(&empty.0, &["list"]).status.success());
    assert!(!empty.paths().store.exists());
}
#[test]
fn cli_parser_rejects_invalid_choices_unknown_and_missing() {
    for args in [
        vec!["todo"],
        vec!["todo", "wat"],
        vec!["todo", "add", "a"],
        vec!["todo", "add", "a", "x", "--effort", "bad"],
        vec!["todo", "add", "a", "x", "--needs", "bad"],
        vec!["todo", "start", "x"],
        vec!["todo", "watch", "--all"],
    ] {
        assert!(Cli::try_parse_from(args).is_err());
    }
}
#[test]
fn byte_lock_exclusion_timeout_and_release() {
    let t = Temp::new();
    let p = t.paths();
    let lock = Lock::acquire(&p.lock, Duration::ZERO).unwrap();
    assert_eq!(
        Lock::acquire(&p.lock, Duration::from_millis(100))
            .err()
            .unwrap(),
        "todo.lock is busy"
    );
    drop(lock);
    assert!(Lock::acquire(&p.lock, Duration::ZERO).is_ok());
}
#[test]
fn rust_lock_interop_crash_and_concurrent_writes() {
    let t = Temp::new();
    let p = t.paths();
    let lock = Lock::acquire(&p.lock, Duration::ZERO).unwrap();
    let output = Process::new(std::env::current_exe().unwrap())
        .creation_flags(CREATE_NO_WINDOW)
        .args(["--exact", "rust_lock_child", "--nocapture"])
        .env("CC_LOCK_PROBE", &p.lock)
        .output()
        .unwrap();
    assert!(!output.status.success());
    drop(lock);
    let ready = t.0.join("ready");
    let mut child = Process::new(std::env::current_exe().unwrap())
        .creation_flags(CREATE_NO_WINDOW)
        .args(["--exact", "rust_lock_child", "--nocapture"])
        .env("CC_LOCK_PROBE", &p.lock)
        .env("CC_LOCK_READY", &ready)
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let deadline = std::time::Instant::now() + Duration::from_secs(10);
    while !ready.exists() {
        assert!(std::time::Instant::now() < deadline);
        std::thread::sleep(Duration::from_millis(20));
    }
    assert!(Lock::acquire(&p.lock, Duration::ZERO).is_err());
    child.kill().unwrap();
    child.wait().unwrap();
    assert!(Lock::acquire(&p.lock, Duration::ZERO).is_ok());
    assert!(rust_command(&t.0, &["project", "a", "A"]).status.success());
    let mut children = Vec::new();
    for n in 0..12 {
        children.push(
            Process::new(env!("CARGO_BIN_EXE_todo"))
                .creation_flags(CREATE_NO_WINDOW)
                .env("CC_TODO_HOME", &t.0)
                .env("CC_TODO_JOBS", &p.jobs)
                .args(["add", "a", &format!("task{n}")])
                .stdout(Stdio::null())
                .spawn()
                .unwrap(),
        );
    }
    for mut c in children {
        assert!(c.wait().unwrap().success());
    }
    let d = load(&p.store).unwrap();
    assert_eq!(d["items"].as_array().unwrap().len(), 12);
    assert_eq!(d["next_id"], 13);
    assert_eq!(
        d["items"]
            .as_array()
            .unwrap()
            .iter()
            .map(|i| i["id"].as_i64().unwrap())
            .collect::<Vec<_>>(),
        (1..=12).collect::<Vec<_>>()
    );
}
struct FakeHttp {
    responses: std::collections::VecDeque<(u32, Vec<u8>)>,
    sleeps: Vec<f64>,
    requests: usize,
}
impl HttpTransport for FakeHttp {
    fn request(&mut self, _: &str, _: &str, body: &[u8], token: &str) -> Result<(u32, Vec<u8>)> {
        assert_eq!(token, "fake-token");
        assert_eq!(
            serde_json::from_slice::<Value>(body).unwrap(),
            json!({"x":1})
        );
        self.requests += 1;
        Ok(self.responses.pop_front().unwrap())
    }
    fn sleep(&mut self, s: f64) {
        self.sleeps.push(s);
    }
}
fn http(responses: Vec<(u32, Value)>) -> FakeHttp {
    FakeHttp {
        responses: responses
            .into_iter()
            .map(|(s, v)| (s, serde_json::to_vec(&v).unwrap()))
            .collect(),
        sleeps: vec![],
        requests: 0,
    }
}
#[test]
fn injectable_http_retry_success_error_and_exhaustion() {
    let mut h = http(vec![
        (429, json!({"retry_after":0.5})),
        (200, json!({"id":"ok"})),
    ]);
    assert_eq!(
        discord_call(&mut h, "POST", "/x", &json!({"x":1}), "fake-token").unwrap(),
        json!({"id":"ok"})
    );
    assert_eq!(h.sleeps, vec![0.5]);
    assert_eq!(h.requests, 2);
    let mut h = http(vec![(403, json!({"message":"Forbidden"}))]);
    assert_eq!(
        discord_call(&mut h, "POST", "/x?secret=y", &json!({"x":1}), "fake-token").unwrap_err(),
        "Discord API POST /x failed: 403 Forbidden"
    );
    let mut h = http(vec![(429, json!({})); 3]);
    assert_eq!(
        discord_call(&mut h, "POST", "/x", &json!({"x":1}), "fake-token").unwrap_err(),
        "Discord API rate limited"
    );
    assert_eq!(h.sleeps, vec![1.; 3]);
    assert_eq!(h.requests, 3);
}
#[test]
fn progress_can_be_recorded_with_status_updates_and_unknown_stays_unknown() {
    let temp = Temp::new();
    let mut d = db();
    execute(&mut d, &cli(&["start", "1"]), &temp.paths()).unwrap();
    assert!(d["items"][0].get("progress").is_none());
    execute(
        &mut d,
        &cli(&["start", "1", "--job", "worker", "--progress", "60"]),
        &temp.paths(),
    )
    .unwrap();
    assert_eq!(d["items"][0]["progress"], 60);
    execute(
        &mut d,
        &cli(&["block", "1", "検査枠を待っています"]),
        &temp.paths(),
    )
    .unwrap();
    assert_eq!(d["items"][0]["progress"], 60);
    execute(&mut d, &cli(&["progress", "1", "0"]), &temp.paths()).unwrap();
    assert_eq!(d["items"][0]["status"], "block");
    assert_eq!(d["items"][0]["note"], "検査枠を待っています");
    assert_eq!(d["items"][0]["jobs"], json!(["worker"]));
    assert_eq!(d["items"][0]["progress"], 0);
    execute(&mut d, &cli(&["start", "1"]), &temp.paths()).unwrap();
    assert_eq!(d["items"][0]["progress"], 0);
    execute(&mut d, &cli(&["done", "1"]), &temp.paths()).unwrap();
    assert_eq!(d["items"][0]["progress"], 100);
    let before = d.clone();
    assert!(execute(&mut d, &cli(&["progress", "1", "70"]), &temp.paths()).is_err());
    assert_eq!(d, before);
    execute(&mut d, &cli(&["start", "1"]), &temp.paths()).unwrap();
    assert!(d["items"][0].get("progress").is_none());
}
#[test]
fn progress_range_is_validated_in_cli_and_core_before_mutation() {
    for value in ["-1", "101", "256", "1.5", "none"] {
        for args in [
            vec!["todo", "progress", "1", value],
            vec!["todo", "start", "1", "--progress", value],
            vec!["todo", "block", "1", "理由", "--progress", value],
        ] {
            assert!(Cli::try_parse_from(args).is_err());
        }
    }
    let temp = Temp::new();
    let mut d = db();
    let before = d.clone();
    assert!(
        execute(
            &mut d,
            &cc_todo::Command::Start {
                id: 1,
                job: String::new(),
                progress: Some(101)
            },
            &temp.paths()
        )
        .is_err()
    );
    assert_eq!(d, before);
    assert!(
        execute(
            &mut d,
            &cc_todo::Command::Progress {
                id: 1,
                percent: Some(200),
                clear: false
            },
            &temp.paths()
        )
        .is_err()
    );
    assert_eq!(d, before);
}
#[test]
fn explicit_unknown_progress_clears_only_the_metric() {
    let temp = Temp::new();
    let mut d = db();
    execute(
        &mut d,
        &cli(&["block", "1", "本人の承認待ち", "--progress", "60"]),
        &temp.paths(),
    )
    .unwrap();
    execute(&mut d, &cli(&["progress", "1", "--clear"]), &temp.paths()).unwrap();
    assert!(d["items"][0].get("progress").is_none());
    assert_eq!(d["items"][0]["status"], "block");
    assert_eq!(d["items"][0]["note"], "本人の承認待ち");
    assert!(Cli::try_parse_from(["todo", "progress", "1"]).is_err());
    assert!(Cli::try_parse_from(["todo", "progress", "1", "60", "--clear"]).is_err());
    execute(&mut d, &cli(&["done", "1"]), &temp.paths()).unwrap();
    let before = d.clone();
    assert!(execute(&mut d, &cli(&["progress", "1", "--clear"]), &temp.paths()).is_err());
    assert_eq!(d, before);
}
#[test]
fn progress_persists_and_legacy_fields_survive_real_rust_cli() {
    let temp = Temp::new();
    let mut d = db();
    d["items"][0]["unknown_field"] = json!({"keep":true});
    save(&temp.paths().store, &d).unwrap();
    assert!(
        rust_command(&temp.0, &["progress", "1", "60"])
            .status
            .success()
    );
    let d = load(&temp.paths().store).unwrap();
    assert_eq!(d["items"][0]["progress"], 60);
    assert_eq!(d["items"][0]["unknown_field"]["keep"], true);
    assert_eq!(d["items"][0]["status"], "todo");
    let before = fs::read(&temp.paths().store).unwrap();
    for args in [vec!["progress", "999", "50"], vec!["progress", "1", "101"]] {
        assert!(!rust_command(&temp.0, &args).status.success());
        assert_eq!(fs::read(&temp.paths().store).unwrap(), before);
    }
}
