use cc_ask::*;
use serde_json::json;

#[cfg(windows)]
#[test]
fn structured_child_output_reads_past_text_capture_cap_and_requires_complete_success() {
    let argv = |script: &str| -> Vec<String> {
        [
            "powershell.exe",
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            script,
        ]
        .into_iter()
        .map(str::to_owned)
        .collect()
    };
    let large = argv(
        r#"[Console]::Write('{"padding":"' + ('x' * (9 * 1024 * 1024)) + '","last_reader":{"id":"tail","thread_id":"session-at-end"}}')"#,
    );
    let value = run_hidden_json(&large, 45).unwrap();
    assert_eq!(value["padding"].as_str().unwrap().len(), 9 * 1024 * 1024);
    assert_eq!(value["last_reader"]["thread_id"], "session-at-end");
    for script in [
        r#"[Console]::Write('{"private":"unfinished')"#,
        r#"[Console]::Write('{"private":"tail"} extra')"#,
        r#"[Console]::Write('{"private":"failed"}'); exit 7"#,
    ] {
        let error = run_hidden_json(&argv(script), 45).unwrap_err();
        assert!(!error.contains("private"));
        assert!(!error.contains("unfinished"));
        assert!(!error.contains("tail"));
        assert!(!error.contains("failed\""));
    }
}

#[test]
fn snooze_registration_uses_latest_deadline_and_does_not_revive_an_older_one() {
    let older = json!({"thread_id":"200","open":false,"remind_not_before":1790982000.,
        "remind_not_before_iso":"2026-10-03T08:00:00+09:00"});
    for latest in [
        json!({"thread_id":"200","open":true,"remind_not_before":1790985600.,
            "remind_not_before_iso":"2026-10-03T09:00:00+09:00"}),
        json!({"thread_id":"200","open":true}),
    ] {
        let mut state = json!({"questions":[older.clone(),latest.clone()]});
        register_question(
            &mut state,
            &json!({"id":"200","name":"設定確認待ち"}),
            "203",
        );
        assert_eq!(state["questions"][0]["open"], false);
        assert_eq!(state["questions"][1]["open"], false);
        assert_eq!(state["questions"][2]["open"], true);
        assert_eq!(
            state["questions"][2].get("remind_not_before"),
            latest.get("remind_not_before")
        );
        assert_eq!(
            state["questions"][2].get("remind_not_before_iso"),
            latest.get("remind_not_before_iso")
        );
    }
}

#[test]
fn snooze_survives_intake_question_registration_only_for_its_own_thread() {
    for open in [true, false] {
        let original = json!({"thread_id":"200","after":"201","open":open,
            "remind_not_before":1790985600.,"remind_not_before_iso":"2026-10-03T09:00:00+09:00"});
        let unrelated = json!({"thread_id":"100","open":true});
        let mut state = json!({"questions":[original,unrelated.clone()]});
        register_question(
            &mut state,
            &json!({"id":"200","name":"設定確認待ち"}),
            "202",
        );
        assert_eq!(state["questions"][0]["open"], false);
        assert_eq!(state["questions"][1], unrelated);
        assert_eq!(state["questions"][2]["thread_id"], "200");
        assert_eq!(state["questions"][2]["after"], "202");
        assert_eq!(state["questions"][2]["open"], true);
        assert_eq!(state["questions"][2]["remind_not_before"], 1790985600.);
        assert_eq!(
            state["questions"][2]["remind_not_before_iso"],
            "2026-10-03T09:00:00+09:00"
        );
        register_question(&mut state, &json!({"id":"100","name":"別の質問"}), "203");
        assert!(state["questions"][3].get("remind_not_before").is_none());
    }
}
use std::{
    io::{BufRead, BufReader},
    process::{Command, Stdio},
    time::Duration,
};

#[test]
fn lock_child_fixture() {
    if let Ok(path) = std::env::var("CC_ASK_LOCK_FIXTURE") {
        let _lock = ByteLock::acquire(std::path::Path::new(&path), Duration::ZERO).unwrap();
        println!("held");
        use std::io::Write;
        std::io::stdout().flush().unwrap();
        std::thread::sleep(Duration::from_secs(30));
    }
}
#[test]
fn test_live_lock_is_not_stolen_by_age_and_process_death_releases_it() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("state.lock");
    let mut cmd = Command::new(std::env::current_exe().unwrap());
    cmd.args(["--exact", "lock_child_fixture", "--nocapture"])
        .env("CC_ASK_LOCK_FIXTURE", &path)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x08000000);
    }
    let mut child = cmd.spawn().unwrap();
    let lines = BufReader::new(child.stdout.take().unwrap()).lines();
    for line in lines {
        if line.unwrap() == "held" {
            break;
        }
    }
    let file = std::fs::OpenOptions::new().write(true).open(&path).unwrap();
    file.set_times(
        std::fs::FileTimes::new()
            .set_modified(std::time::SystemTime::now() - Duration::from_secs(1200)),
    )
    .unwrap();
    assert!(ByteLock::acquire(&path, Duration::ZERO).is_err());
    child.kill().unwrap();
    child.wait().unwrap();
    assert!(ByteLock::acquire(&path, Duration::from_secs(1)).is_ok());
}
#[test]
fn unicode_chunks_and_crlf_preserve_python_line_boundaries() {
    assert_eq!(chunks("ああ\r\nい", 3), ["ああ\r", "\nい"]);
    assert_eq!(chunks("abcd\nef\n", 5), ["abcd\n", "ef\n"]);
    assert_eq!(chunks("", 2000), Vec::<String>::new());
    assert_eq!(chunks("😀😀😀", 2), ["😀😀", "😀"]);
}
#[test]
fn redact_headers_webhooks_and_fields() {
    let text=safe_text("Authorization: Bearer example-secret\napi_key=example-key\nhttps://discord.com/api/webhooks/123/secret");
    assert!(!text.contains("example-secret"));
    assert!(!text.contains("example-key"));
    assert!(!text.contains("/webhooks/"));
    assert_eq!(safe_text("普通の記録"), "普通の記録");
}
#[test]
fn diagnostics_preserve_causes_and_error_classes_without_secrets_or_extra_log_lines() {
    let error = "job configuration missing\nAuthorization: Bot example-secret\napi_key=example-key";
    let detail = error_detail(error);
    assert!(detail.starts_with("RuntimeError: job configuration missing "));
    assert!(!detail.contains("example-secret"));
    assert!(!detail.contains("example-key"));
    assert!(!detail.contains(['\r', '\n']));
    assert_eq!(
        error_detail("OSError: file missing"),
        "OSError: file missing"
    );
    assert_eq!(error_type("OSError: file missing"), "OSError");
    let root = tempfile::tempdir().unwrap();
    let missing = root.path().join("missing.json");
    let error = read_json(&missing).unwrap_err();
    assert!(error.contains("missing.json"));
    assert_eq!(error_type(&error), "OSError");
    let broken = root.path().join("broken.json");
    std::fs::write(&broken, "{\nINVALID").unwrap();
    let error = read_json(&broken).unwrap_err();
    assert!(error.contains("broken.json") && error.contains("line 2"));
    assert_eq!(error_type(&error), "ValueError");
}

#[test]
fn thread_ownership_requires_explicit_metadata_or_a_labeled_assignment() {
    for prompt in [
        "スレッドID: 100\n依頼を実装する",
        "Discordスレッド：100",
        "複数を担当する。100|案A;200|案B",
    ] {
        assert!(prompt_thread_owner(prompt, "100"), "{prompt}");
    }
    for prompt in [
        "Fix intake; affected threads e.g. 100, 200.",
        "過去ログ: スレッド100でエラー",
        "> スレッドID: 100",
        "スレッドID: 1000",
        "https://discord.com/channels/G/100 の失敗を調査",
        "```text\nスレッドID: 100\n```",
        "~~~\n100|案A;200|案B\n~~~",
        "    スレッドID: 100",
    ] {
        assert!(!prompt_thread_owner(prompt, "100"), "{prompt}");
    }
    assert!(!thread_owner(
        &json!({"discord_thread_id":"200"}),
        "スレッドID: 100",
        "100"
    ));
    assert!(thread_owner(
        &json!({"discord_thread_ids":["100","200"]}),
        "",
        "100"
    ));
    let inferred = json!({"discord_thread_id":"200","discord_thread_ids":["100","200"],"discord_thread_ids_source":"prompt"});
    assert!(thread_owner(&inferred, "", "100"));
    assert!(!explicit_thread_owner(&inferred, "100"));
    assert_eq!(
        prompt_thread_ids("複数を担当する。100|案A;200|案B"),
        ["100", "200"]
    );
}
#[test]
fn json_state_preserves_unknown_fields_and_types() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("state.json");
    let value = json!({"questions":[{"thread_id":"1554428850946187309","open":false}],"future":{"x":1},"integer":9007199254740993u64});
    atomic_json(&path, &value, 2).unwrap();
    assert_eq!(read_json(&path).unwrap(), value);
}
#[test]
fn external_command_prefers_adjacent_exe_and_keeps_python_fallback() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("scheduler.py");
    std::fs::write(&path, "").unwrap();
    let args = vec!["step".into()];
    let old = command_argv(&path, &args);
    assert_eq!(old[old.len() - 2], path.to_string_lossy());
    std::fs::write(path.with_extension("exe"), "").unwrap();
    assert_eq!(
        command_argv(&path, &args),
        vec![
            path.with_extension("exe").to_string_lossy().to_string(),
            "step".into()
        ]
    );
}

#[cfg(windows)]
#[test]
fn replacement_retries_windows_sharing_violation() {
    use std::os::windows::fs::OpenOptionsExt;
    let temp = tempfile::tempdir().unwrap();
    let target = temp.path().join("state.json");
    let source = temp.path().join("state.tmp");
    std::fs::write(&target, "old").unwrap();
    std::fs::write(&source, "new").unwrap();
    let held = std::fs::OpenOptions::new()
        .read(true)
        .share_mode(3)
        .open(&target)
        .unwrap();
    let error = std::fs::rename(&source, &target).unwrap_err();
    assert!(matches!(error.raw_os_error(), Some(5 | 32 | 33)));
    assert!(retryable_replace_error(&error));
    let release = std::thread::spawn(move || {
        std::thread::sleep(Duration::from_millis(80));
        drop(held);
    });
    replace_file(&source, &target).unwrap();
    release.join().unwrap();
    assert_eq!(std::fs::read_to_string(&target).unwrap(), "new");
    for code in [5, 32, 33] {
        assert!(retryable_replace_error(&std::io::Error::from_raw_os_error(
            code
        )));
    }
    assert!(!retryable_replace_error(
        &std::io::Error::from_raw_os_error(2)
    ));
}
