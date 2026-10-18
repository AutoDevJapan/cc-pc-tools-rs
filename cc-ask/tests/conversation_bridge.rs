use cc_ask::{
    ask_gate::{OwnerOnly, BLOCKED_MESSAGE},
    commands::AskArgs,
    conversation_bridge as bridge,
    core::{Effects, Output, Result},
};
use serde_json::{json, Value};
use std::{
    fs,
    path::{Path, PathBuf},
    process::Command,
};

struct Fixture {
    root: tempfile::TempDir,
    body: PathBuf,
    decision: PathBuf,
}

impl Fixture {
    fn new() -> Self {
        let root = tempfile::tempdir().unwrap();
        let body = root.path().join("source.txt");
        let decision = root.path().join("decision-source.json");
        fs::write(&body, "画面案を選んでください").unwrap();
        fs::write(&decision, json!({"todo_id":2823,"resources":[{"kind":"service","id":"fixture-resource"}],"decision":"direction","scope":{"origin_thread_id":"500","specification":"fixture"}}).to_string()).unwrap();
        fs::write(root.path().join("state.json"), "lost-legacy-sentinel").unwrap();
        fs::write(
            root.path().join("question-recovery-required.json"),
            "recovery-sentinel",
        )
        .unwrap();
        let f = Self {
            root,
            body,
            decision,
        };
        f.mode(r#"{"domain":"discord-conversation-mode-v1","enabled":true}"#);
        f
    }
    fn mode(&self, value: &str) {
        fs::write(self.root.path().join("conversation-mode.json"), value).unwrap();
    }
    fn ask(&self) -> AskArgs {
        AskArgs {
            title: "画面案の選択".into(),
            body_file: self.body.clone(),
            decision_file: Some(self.decision.clone()),
            owner_only: Some(OwnerOnly::Direction),
            ..Default::default()
        }
    }
    fn unchanged_legacy(&self) {
        assert_eq!(
            fs::read(self.root.path().join("state.json")).unwrap(),
            b"lost-legacy-sentinel"
        );
        assert_eq!(
            fs::read(self.root.path().join("question-recovery-required.json")).unwrap(),
            b"recovery-sentinel"
        );
    }
}

#[derive(Default)]
struct Fake {
    calls: Vec<(Vec<String>, u64)>,
    bodies: Vec<Vec<u8>>,
    attachments: Vec<Vec<u8>>,
    output: Option<Output>,
    error: bool,
    mutate_source: Option<PathBuf>,
    http_calls: usize,
    guard_calls: Vec<String>,
    deny_images: bool,
    busy_once: bool,
}

fn argument<'a>(args: &'a [String], key: &str) -> &'a str {
    args.windows(2)
        .find(|pair| pair[0] == key)
        .map(|pair| pair[1].as_str())
        .unwrap_or("")
}

impl Effects for Fake {
    fn http(&mut self, _: &str, _: &str, _: Option<&Value>, _: bool) -> Result<Value> {
        self.http_calls += 1;
        Err("bridge must not use HTTP".into())
    }
    fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
        if args
            .first()
            .is_some_and(|arg| arg.ends_with("/scrub391.exe"))
        {
            assert_eq!(timeout, 10);
            self.guard_calls.push(argument(args, "--guard-path").into());
            return Ok(Output {
                code: 0,
                stdout:
                    json!({"client_resource":self.deny_images,"classification_unverified":false})
                        .to_string(),
                stderr: String::new(),
            });
        }
        self.calls.push((args.to_vec(), timeout));
        if self.busy_once {
            self.busy_once = false;
            return Ok(Output {
                code: 2,
                stdout: "busy\n".into(),
                stderr: String::new(),
            });
        }
        if let Some(path) = self.mutate_source.take() {
            fs::write(path, "changed-after-snapshot").unwrap();
        }
        self.bodies
            .push(fs::read(argument(args, "--body-file")).unwrap());
        for pair in args.windows(2).filter(|pair| pair[0] == "--attach") {
            self.attachments.push(fs::read(&pair[1]).unwrap());
        }
        if self.error {
            return Err("private child body fixture-token private-path".into());
        }
        if let Some(output) = self.output.clone() {
            return Ok(output);
        }
        let request = argument(args, "--request-id");
        let value = if args[4] == "ask" {
            json!({"request_id":request,"phase":"waiting","todo_id":2823,"thread_id":"600","progress_thread_id":"601","wait_requested":true})
        } else {
            json!({"domain":"discord-conversation-v1","delivery":"verified","request_id":request,"thread_id":"600","message_id":"601","reused":false})
        };
        Ok(Output {
            code: 0,
            stdout: value.to_string(),
            stderr: String::new(),
        })
    }
}

#[test]
fn conversation_bridge_mode_requires_exact_two_field_schema_without_activation() {
    let root = tempfile::tempdir().unwrap();
    let absent = root.path().join("absent-home");
    assert!(!bridge::enabled(&absent).unwrap());
    assert!(!absent.exists());
    let f = Fixture::new();
    assert!(bridge::enabled(f.root.path()).unwrap());
    f.mode(r#"{"domain":"discord-conversation-mode-v1","enabled":false}"#);
    assert!(!bridge::enabled(f.root.path()).unwrap());
    for value in [
        "{",
        "null",
        "[]",
        "{}",
        r#"{"domain":"wrong","enabled":true}"#,
        r#"{"domain":"discord-conversation-mode-v1"}"#,
        r#"{"enabled":true}"#,
        r#"{"domain":"discord-conversation-mode-v1","enabled":"true"}"#,
        r#"{"domain":"discord-conversation-mode-v1","enabled":true,"extra":0}"#,
        r#"{"domain":"discord-conversation-mode-v1","enabled":false,"enabled":true}"#,
        r#"{"domain":"wrong","domain":"discord-conversation-mode-v1","enabled":true}"#,
        r#"{"domain":"discord-conversation-mode-v1","enabled":true} {}"#,
    ] {
        f.mode(value);
        assert!(bridge::enabled(f.root.path()).is_err());
        assert_eq!(
            fs::read_to_string(f.root.path().join("conversation-mode.json")).unwrap(),
            value
        );
        f.unchanged_legacy();
    }
    assert!(!f.root.path().join("conversation-bridge").exists());
}

#[test]
fn conversation_bridge_busy_lease_retries_exact_immutable_command() {
    let f = Fixture::new();
    let mut fake = Fake {
        busy_once: true,
        ..Fake::default()
    };
    let result = bridge::try_log(f.root.path(), &mut fake, "進捗", "本文", None, false, &[])
        .unwrap()
        .unwrap();
    assert_eq!(fake.calls.len(), 2);
    assert_eq!(fake.calls[0].0, fake.calls[1].0);
    assert_eq!(fake.http_calls, 0);
    assert_eq!(result["delivery"], "verified");
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_disabled_and_dry_run_never_start_children_or_snapshot_inputs() {
    let f = Fixture::new();
    let mut fake = Fake::default();
    f.mode(r#"{"domain":"discord-conversation-mode-v1","enabled":false}"#);
    assert!(
        bridge::try_log(f.root.path(), &mut fake, "進捗", "本文", None, false, &[])
            .unwrap()
            .is_none()
    );
    let mut args = f.ask();
    args.body_file = f.root.path().join("missing-body");
    assert!(bridge::try_ask(f.root.path(), &mut fake, &args)
        .unwrap()
        .is_none());
    fs::remove_file(f.root.path().join("conversation-mode.json")).unwrap();
    assert!(
        bridge::try_post(f.root.path(), &mut fake, "500", &args.body_file, false, &[])
            .unwrap()
            .is_none()
    );
    f.mode("malformed-mode");
    args.dry_run = true;
    assert!(bridge::try_ask(f.root.path(), &mut fake, &args)
        .unwrap()
        .is_none());
    assert!(fake.calls.is_empty());
    assert_eq!(fake.http_calls, 0);
    assert!(!f.root.path().join("conversation-bridge").exists());
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_log_forwards_fixed_runner_arguments_and_immutable_body() {
    let f = Fixture::new();
    let mut fake = Fake {
        mutate_source: Some(f.body.clone()),
        ..Default::default()
    };
    let result = bridge::try_log(
        f.root.path(),
        &mut fake,
        "作業の進捗",
        "ignored-text",
        Some(&f.body),
        true,
        &[],
    )
    .unwrap()
    .unwrap();
    assert_eq!(fake.calls.len(), 1);
    let (args, timeout) = &fake.calls[0];
    assert_eq!(
        &args[..3],
        [
            "D:/CC/Tools/discord-ask/unattended-runner.exe",
            "conversation",
            "--config"
        ]
    );
    assert_eq!(
        Path::new(&args[3]),
        f.root.path().join("unattended/config.json")
    );
    assert_eq!(args[4], "log");
    assert_eq!(argument(args, "--title"), "作業の進捗");
    assert!(argument(args, "--thread").is_empty());
    assert!(args.iter().any(|value| value == "--notify"));
    assert_eq!(*timeout, 65);
    assert_eq!(
        fake.bodies,
        vec!["画面案を選んでください".as_bytes().to_vec()]
    );
    assert_ne!(Path::new(argument(args, "--body-file")), f.body);
    assert_eq!(result["delivery"], "verified");
    assert_eq!(fake.http_calls, 0);
    assert!(!result.to_string().contains("画面案を選んでください"));
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_log_identity_uses_content_title_notify_and_image_bytes() {
    let f = Fixture::new();
    let other_body = f.root.path().join("other-body.txt");
    fs::write(&other_body, "画面案を選んでください\r\n").unwrap();
    let first_image = f.root.path().join("first.png");
    let other_image = f.root.path().join("renamed.png");
    fs::write(&first_image, b"\x89PNG\r\n\x1a\nfixture-one").unwrap();
    fs::copy(&first_image, &other_image).unwrap();
    let mut fake = Fake::default();
    let mut ids = Vec::new();
    for (title, body_file, notify, image) in [
        ("進捗", &f.body, false, &first_image),
        ("進捗", &other_body, false, &other_image),
        ("別の進捗", &f.body, false, &first_image),
        ("進捗", &f.body, true, &first_image),
    ] {
        bridge::try_log(
            f.root.path(),
            &mut fake,
            title,
            "",
            Some(body_file),
            notify,
            &[image.clone()],
        )
        .unwrap();
        ids.push(argument(&fake.calls.last().unwrap().0, "--request-id").to_owned());
    }
    assert_eq!(ids.len(), 4);
    assert_eq!(ids[0], ids[1]);
    assert_ne!(ids[0], ids[2]);
    assert_ne!(ids[0], ids[3]);
    fs::write(&first_image, b"\x89PNG\r\n\x1a\nfixture-two").unwrap();
    bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "",
        Some(&f.body),
        false,
        &[first_image],
    )
    .unwrap();
    assert_ne!(
        ids[0],
        argument(&fake.calls.last().unwrap().0, "--request-id")
    );
    fs::write(&f.body, "別の本文").unwrap();
    bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "",
        Some(&f.body),
        false,
        &[other_image],
    )
    .unwrap();
    assert_ne!(
        ids[0],
        argument(&fake.calls.last().unwrap().0, "--request-id")
    );
    assert_eq!(fake.http_calls, 0);
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_images_use_same_hashed_snapshot_and_videos_audio_are_rejected() {
    let f = Fixture::new();
    let image = f.root.path().join("source.png");
    let bytes = b"\x89PNG\r\n\x1a\nfixture-image";
    fs::write(&image, bytes).unwrap();
    let mut fake = Fake {
        mutate_source: Some(image.clone()),
        ..Default::default()
    };
    bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "本文",
        None,
        false,
        &[image.clone()],
    )
    .unwrap();
    assert_eq!(fake.attachments, vec![bytes.to_vec()]);
    let args = &fake.calls[0].0;
    assert_ne!(Path::new(argument(args, "--attach")), image);
    let snapshot = Path::new(argument(args, "--attach"));
    let receipt: Value =
        serde_json::from_slice(&fs::read(snapshot.with_extension("source.json")).unwrap()).unwrap();
    assert_eq!(receipt["domain"], "discord-conversation-media-source-v1");
    assert_eq!(
        Path::new(receipt["source_path"].as_str().unwrap()),
        fs::canonicalize(&image).unwrap()
    );
    assert_eq!(
        Path::new(receipt["snapshot_path"].as_str().unwrap()),
        snapshot
    );
    assert_eq!(receipt["size"], bytes.len());
    assert!(receipt["sha256"]
        .as_str()
        .is_some_and(|hash| hash.len() == 64));
    for extension in ["mp4", "mp3", "zip", "exe", "md"] {
        let path = f.root.path().join(format!("unsupported.{extension}"));
        fs::write(&path, "fixture").unwrap();
        assert!(bridge::try_log(
            f.root.path(),
            &mut fake,
            "進捗",
            "本文",
            None,
            false,
            &[path]
        )
        .is_err());
    }
    assert_eq!(fake.calls.len(), 1);
    assert_eq!(fake.guard_calls.len(), 1);
    // This wrapper never claims the sender's independent image protection succeeded.
    assert_eq!(fake.http_calls, 0);
}

#[test]
fn conversation_bridge_media_size_limits_precede_any_copy_or_guard() {
    for total_limit in [false, true] {
        let f = Fixture::new();
        let mut paths = Vec::new();
        for index in 0..if total_limit { 3 } else { 1 } {
            let path = f.root.path().join(format!("image-{index}.png"));
            let file = fs::File::create(&path).unwrap();
            file.set_len(if total_limit {
                8 * 1024 * 1024
            } else {
                10 * 1024 * 1024 + 1
            })
            .unwrap();
            paths.push(path);
        }
        let mut fake = Fake::default();
        assert!(bridge::try_log(
            f.root.path(),
            &mut fake,
            "進捗",
            "本文",
            None,
            false,
            &paths
        )
        .is_err());
        assert!(fake.calls.is_empty());
        assert!(fake.guard_calls.is_empty());
        assert_eq!(
            fs::read_dir(f.root.path().join("conversation-bridge"))
                .unwrap()
                .count(),
            0
        );
    }
}

#[test]
fn conversation_bridge_media_provenance_is_immutable_across_same_byte_aliases() {
    let f = Fixture::new();
    let first = f.root.path().join("first.png");
    let second = f.root.path().join("second.png");
    fs::write(&first, b"\x89PNG\r\n\x1a\nfixture").unwrap();
    fs::copy(&first, &second).unwrap();
    let mut fake = Fake::default();
    bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "本文",
        None,
        false,
        &[first.clone()],
    )
    .unwrap();
    let receipt = Path::new(argument(&fake.calls[0].0, "--attach")).with_extension("source.json");
    let original = fs::read(&receipt).unwrap();
    bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "本文",
        None,
        false,
        &[second],
    )
    .unwrap();
    assert_eq!(
        argument(&fake.calls[0].0, "--request-id"),
        argument(&fake.calls[1].0, "--request-id")
    );
    assert_eq!(fs::read(&receipt).unwrap(), original);
    fs::write(&receipt, b"{}").unwrap();
    assert!(bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "本文",
        None,
        false,
        &[first]
    )
    .is_err());
    assert_eq!(fake.calls.len(), 2);
    assert_eq!(fs::read(&receipt).unwrap(), b"{}");
}

#[test]
fn conversation_bridge_media_cannot_hide_sensitive_original_components() {
    let f = Fixture::new();
    let directory = f.root.path().join("private-secrets");
    fs::create_dir(&directory).unwrap();
    let source = directory.join("image.png");
    fs::write(&source, b"\x89PNG\r\n\x1a\nfixture").unwrap();
    let mut fake = Fake::default();
    assert!(bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "本文",
        None,
        false,
        &[source]
    )
    .is_err());
    assert!(fake.calls.is_empty());
    assert!(fake.guard_calls.is_empty());
}

#[test]
fn conversation_bridge_protected_source_never_reaches_the_go_sender() {
    let f = Fixture::new();
    let image = f.root.path().join("source.png");
    fs::write(&image, b"\x89PNG\r\n\x1a\nfixture-image").unwrap();
    let mut fake = Fake {
        deny_images: true,
        ..Default::default()
    };
    assert!(bridge::try_log(
        f.root.path(),
        &mut fake,
        "進捗",
        "本文",
        None,
        false,
        &[image]
    )
    .is_err());
    assert_eq!(fake.guard_calls.len(), 1);
    assert!(fake.calls.is_empty());
    assert_eq!(fake.http_calls, 0);
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_post_preserves_explicit_thread_and_different_thread_changes_identity() {
    let f = Fixture::new();
    let mut fake = Fake::default();
    for thread in ["500", "501"] {
        bridge::try_post(f.root.path(), &mut fake, thread, &f.body, false, &[]).unwrap();
    }
    assert_eq!(fake.calls.len(), 2);
    assert_eq!(argument(&fake.calls[0].0, "--thread"), "500");
    assert_eq!(argument(&fake.calls[1].0, "--thread"), "501");
    assert_ne!(
        argument(&fake.calls[0].0, "--request-id"),
        argument(&fake.calls[1].0, "--request-id")
    );
    assert_eq!(argument(&fake.calls[0].0, "--title"), "既存の作業への報告");
    assert!(!fake.calls[0].0.iter().any(|value| value == "--notify"));
    assert!(bridge::try_post(f.root.path(), &mut fake, "0", &f.body, false, &[]).is_err());
    assert_eq!(fake.calls.len(), 2);
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_ask_keeps_gate_decision_origin_and_canonical_wait() {
    let f = Fixture::new();
    fs::write(&f.body, "検査の許可をお願いします").unwrap();
    let mut fake = Fake::default();
    let mut args = f.ask();
    args.title = "Chromeの検査の許可".into();
    args.owner_only = None;
    assert_eq!(
        bridge::try_ask(f.root.path(), &mut fake, &args).unwrap_err(),
        BLOCKED_MESSAGE
    );
    assert!(fake.calls.is_empty());
    assert!(!f.root.path().join("conversation-bridge").exists());
    args.owner_only = Some(OwnerOnly::Direction);
    args.thread = Some("500".into());
    let result = bridge::try_ask(f.root.path(), &mut fake, &args)
        .unwrap()
        .unwrap();
    assert_eq!(fake.calls.len(), 1);
    let argv = &fake.calls[0].0;
    assert_eq!(argv[4], "ask");
    assert_eq!(argument(argv, "--todo-id"), "2823");
    assert_eq!(argument(argv, "--origin-thread"), "500");
    assert_eq!(argument(argv, "--title"), args.title);
    assert!(argv.iter().any(|value| value == "--wait"));
    assert_ne!(Path::new(argument(argv, "--decision-file")), f.decision);
    let saved: Value =
        serde_json::from_slice(&fs::read(argument(argv, "--decision-file")).unwrap()).unwrap();
    assert_eq!(saved["todo_id"], 2823);
    assert_eq!(saved["scope"]["origin_thread_id"], "500");
    assert_eq!(result["phase"], "waiting");
    assert!(!result.to_string().contains("画面案を選んでください"));
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_ask_identity_is_stable_across_json_key_order_and_body_path() {
    let f = Fixture::new();
    let mut fake = Fake::default();
    let mut args = f.ask();
    bridge::try_ask(f.root.path(), &mut fake, &args).unwrap();
    let first = argument(&fake.calls[0].0, "--request-id").to_owned();
    let mut value: Value = serde_json::from_slice(&fs::read(&f.decision).unwrap()).unwrap();
    value.as_object_mut().unwrap().sort_keys();
    fs::write(&f.decision, serde_json::to_string_pretty(&value).unwrap()).unwrap();
    let other_body = f.root.path().join("same-body.txt");
    fs::copy(&f.body, &other_body).unwrap();
    args.body_file = other_body;
    bridge::try_ask(f.root.path(), &mut fake, &args).unwrap();
    assert_eq!(first, argument(&fake.calls[1].0, "--request-id"));
    args.title = "別の画面案の選択".into();
    bridge::try_ask(f.root.path(), &mut fake, &args).unwrap();
    assert_ne!(first, argument(&fake.calls[2].0, "--request-id"));
    assert_eq!(fake.calls.len(), 3);
}

#[test]
fn conversation_bridge_ask_rejects_missing_decision_mismatched_origins_and_attachments() {
    let f = Fixture::new();
    let mut fake = Fake::default();
    let mut args = f.ask();
    args.decision_file = None;
    assert!(bridge::try_ask(f.root.path(), &mut fake, &args)
        .unwrap_err()
        .contains("--decision-file"));
    args.decision_file = Some(f.decision.clone());
    args.thread = Some("501".into());
    assert!(bridge::try_ask(f.root.path(), &mut fake, &args).is_err());
    args.thread = None;
    for value in [
        json!({"todo_id":0,"origin_thread_id":"500"}),
        json!({"todo_id":"2823","origin_thread_id":"500"}),
        json!({"todo_id":2823}),
        json!({"todo_id":2823,"origin_thread_id":"500","scope":{"origin_thread_id":"501"}}),
    ] {
        fs::write(&f.decision, value.to_string()).unwrap();
        assert!(bridge::try_ask(f.root.path(), &mut fake, &args).is_err());
    }
    fs::write(
        &f.decision,
        json!({"todo_id":2823,"origin_thread_id":"500"}).to_string(),
    )
    .unwrap();
    args.attach.push(f.root.path().join("image.png"));
    assert!(bridge::try_ask(f.root.path(), &mut fake, &args).is_err());
    assert!(fake.calls.is_empty());
    assert!(!f.root.path().join("conversation-bridge").exists());
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_failures_never_fallback_or_expose_child_text_and_paths() {
    let f = Fixture::new();
    let private = "private-question-body fixture-token attachment-private-path";
    let mut fake = Fake {
        output: Some(Output {
            code: 1,
            stdout: private.into(),
            stderr: private.into(),
        }),
        ..Default::default()
    };
    let error =
        bridge::try_log(f.root.path(), &mut fake, "進捗", "本文", None, false, &[]).unwrap_err();
    assert!(!error.contains("private"));
    assert!(!error.contains("fixture-token"));
    assert_eq!(fake.calls.len(), 1);
    fake.output = None;
    fake.error = true;
    assert_eq!(
        bridge::try_log(f.root.path(), &mut fake, "進捗", "本文", None, false, &[]).unwrap_err(),
        error
    );
    assert_eq!(fake.calls.len(), 2);
    assert_eq!(
        argument(&fake.calls[0].0, "--request-id"),
        argument(&fake.calls[1].0, "--request-id")
    );
    assert_eq!(fake.http_calls, 0);
    fake.error = false;
    fake.output = Some(Output { code:0, stdout:json!({"domain":"discord-conversation-v1","delivery":"verified","request_id":"fixture-request","thread_id":"600","body":private,"intent":{"body":private},"body_file":private,"decision_file":private,"attachments":[private],"title":private,"token":private}).to_string(), stderr:private.into() });
    let result = bridge::try_log(f.root.path(), &mut fake, "進捗", "本文", None, false, &[])
        .unwrap()
        .unwrap();
    assert_eq!(
        result,
        json!({"domain":"discord-conversation-v1","delivery":"verified","request_id":"fixture-request","thread_id":"600"})
    );
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_changed_saved_snapshot_prevents_another_child() {
    let f = Fixture::new();
    let mut fake = Fake::default();
    bridge::try_log(f.root.path(), &mut fake, "進捗", "本文", None, false, &[]).unwrap();
    fs::write(
        argument(&fake.calls[0].0, "--body-file"),
        "changed-snapshot",
    )
    .unwrap();
    assert!(bridge::try_log(f.root.path(), &mut fake, "進捗", "本文", None, false, &[]).is_err());
    assert_eq!(fake.calls.len(), 1);
    assert_eq!(fake.http_calls, 0);
    f.unchanged_legacy();
}

fn binary(f: &Fixture) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_discord_ask"));
    command
        .env("CC_ASK_HOME", f.root.path())
        .env("CC_ASK_CONFIG", f.root.path().join("missing-config.json"));
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    command
}

#[test]
fn conversation_bridge_cli_invalid_mode_fails_before_legacy_config_or_api() {
    let f = Fixture::new();
    f.mode("malformed-mode");
    let output = binary(&f)
        .args(["log", "--topic", "進捗", "--text", "本文"])
        .output()
        .unwrap();
    assert!(!output.status.success());
    assert!(output.stdout.is_empty());
    assert!(String::from_utf8_lossy(&output.stderr).contains("新しい質問・報告の設定が不正"));
    assert!(!f.root.path().join("conversation-bridge").exists());
    f.unchanged_legacy();
}

#[test]
fn conversation_bridge_cli_gate_precedes_enabled_mode_and_dry_run_uses_legacy_path() {
    let f = Fixture::new();
    fs::write(&f.body, "検査の許可をお願いします").unwrap();
    let output = binary(&f)
        .args(["ask", "--title", "Chromeの検査の許可", "--body-file"])
        .arg(&f.body)
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(78));
    assert!(output.stdout.is_empty());
    assert!(String::from_utf8_lossy(&output.stderr).contains("聞かずに進めること"));
    f.mode("malformed-mode");
    let output = binary(&f)
        .args(["ask", "--title", "画面案の選択", "--body-file"])
        .arg(&f.body)
        .args(["--dry-run", "--owner-only", "direction"])
        .output()
        .unwrap();
    assert!(!output.status.success());
    assert!(output.stdout.is_empty());
    assert!(!String::from_utf8_lossy(&output.stderr).contains("新しい質問・報告の設定が不正"));
    assert!(!f.root.path().join("conversation-bridge").exists());
    f.unchanged_legacy();
}
