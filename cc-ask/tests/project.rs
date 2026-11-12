mod common;
use cc_ask::{commands::*, project::*};
use common::Harness;
use serde_json::json;
use std::process::Command;

#[test]
fn worktree_mapping_and_folder_fallback() {
    for (folder, project) in [
        (r"D:\CC\repos\alpha-wt\fix\src", "alpha"),
        ("D:/CC/Worktrees/alpha/fix/src", "alpha"),
        (r"D:\CC\civ6-banseiki-wt\fix", "civ6-banseiki"),
        ("D:/CC/Worktrees/civ6-banseiki/fix", "civ6-banseiki"),
        ("D:/CC/repos/alpha-wt/", "alpha"),
        ("D:/CC/repos/alpha", "alpha"),
        ("D:/scratch/plain/", "plain"),
        ("D:/CC/Tools", "PCツール"),
        (r"d:\cc\tools\rs\cc-ask", "PCツール"),
    ] {
        assert_eq!(project_from_folder(folder), project, "{folder}");
    }
}

#[test]
fn project_uses_git_toplevel_of_recorded_cwd_and_falls_back_when_git_fails() {
    let mut h = Harness::new();
    h.fake
        .borrow_mut()
        .git_roots
        .insert("D:/repos/alpha-wt/fix".into(), "D:/repos/alpha".into());
    let names = ProjectNames::load(&h.ctx.client_projects).unwrap();
    assert_eq!(
        determine_project(&mut h.ctx, "D:/repos/alpha-wt/fix/src", &names),
        "alpha"
    );
    assert_eq!(
        h.fake.borrow().git_calls[0],
        (
            "D:/repos/alpha-wt/fix/src".into(),
            vec!["rev-parse".into(), "--show-toplevel".into()]
        )
    );
    h.fake.borrow_mut().git_error = true;
    assert_eq!(
        determine_project(&mut h.ctx, "D:/CC/Worktrees/alpha/fix/src", &names),
        "alpha"
    );
    assert_eq!(
        determine_project(&mut h.ctx, "D:/scratch/plain", &names),
        "plain"
    );
}

#[test]
fn existing_project_prefix_or_name_is_not_duplicated() {
    assert_eq!(thread_title("alpha", "Codex", "確認"), "【alpha】確認");
    assert_eq!(
        thread_title("alpha", "Codex", "【alpha】確認"),
        "【alpha】確認"
    );
    assert_eq!(
        thread_title("alpha", "", "ALPHA の確認"),
        "【alpha】ALPHA の確認"
    );
    assert_eq!(
        question_body("alpha", "OWN", "【alpha】\n確認"),
        "【alpha】\n<@OWN> 確認\n\n-# このスレッドに返信してください。"
    );
}

#[test]
fn ask_posts_project_first_and_saves_it_without_leaking_client_names() {
    let mut h = Harness::new();
    std::fs::write(
        &h.ctx.client_projects,
        "\u{feff}# test clients\r\nSecretClient\r\n\r\n",
    )
    .unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "【SECRETCLIENT】\r\nSecretClient の確認です").unwrap();
    cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "【SecretClient】SecretClient の確認".into(),
            agent: "Codex".into(),
            body_file: body,
            cwd: Some("D:/CC/Worktrees/secretclient/fix".into()),
            ..Default::default()
        },
    )
    .unwrap();
    let state = h.state();
    assert_eq!(state["questions"][0]["project"], "クライアント案件");
    assert_eq!(
        state["questions"][0]["title"],
        "【クライアント案件】クライアント案件 の確認"
    );
    assert_eq!(
        state["questions"][0]["cwd"],
        "D:/CC/Worktrees/secretclient/fix"
    );
    let fake = h.fake.borrow();
    assert_eq!(fake.messages["300"][0]["content"], "【クライアント案件】\n<@OWN> クライアント案件 の確認です\n\n-# このスレッドに返信してください。");
    assert!(!json!(fake.http_calls)
        .to_string()
        .to_lowercase()
        .contains("secretclient"));
}

#[test]
fn reminder_text_includes_project_and_removes_both_prefix_orders() {
    let expected = "<@OWN> 【alpha】「実装方針」の件で、上の質問への回答をお願いしたいです！🙇（1時間たったので再送です）";
    for title in [
        "【alpha】[Codex] 実装方針",
        "[Codex] 【alpha】実装方針",
        "[Codex] 実装方針",
    ] {
        assert_eq!(reminder_text("alpha", "OWN", title), expected);
    }
}

#[test]
fn reminder_migrates_legacy_cwd_and_redacts_saved_project_and_title() {
    let mut h = Harness::new();
    h.fake.borrow_mut().messages.get_mut("100").unwrap().clear();
    h.fake.borrow_mut().messages.insert("100".into(), vec![]);
    std::fs::write(&h.ctx.client_projects, "SecretClient\n").unwrap();
    h.save(&json!({"questions":[
        {"thread_id":"200","after":"0","title":"[Codex] 方針","cwd":"D:/CC/civ6-banseiki-wt/fix","open":true},
        {"thread_id":"100","after":"0","title":"【SecretClient】[Claude] SecretClient の方針","project":"SecretClient","open":true}
    ]}));
    cmd_remind(&mut h.ctx).unwrap();
    let fake = h.fake.borrow();
    assert_eq!(fake.posts.len(), 2);
    assert_eq!(
        fake.posts[0].1["content"],
        reminder_text("civ6-banseiki", "OWN", "方針")
    );
    assert_eq!(
        fake.posts[1].1["content"],
        reminder_text("クライアント案件", "OWN", "クライアント案件 の方針")
    );
    assert!(!json!(fake.http_calls)
        .to_string()
        .to_lowercase()
        .contains("secretclient"));
    let state = h.state();
    assert_eq!(state["questions"][0]["project"], "civ6-banseiki");
    assert_eq!(state["questions"][1]["project"], "クライアント案件");
    assert_eq!(
        state["questions"][1]["title"],
        "【クライアント案件】[Claude] クライアント案件 の方針"
    );
}

#[test]
fn dry_run_uses_live_formatting_without_http_or_state_writes() {
    let mut h = Harness::new();
    std::fs::remove_file(h.ctx.here.join("state.json")).unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "【alpha】\n確認").unwrap();
    let result = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "確認".into(),
            agent: "Codex".into(),
            body_file: body,
            cwd: Some("D:/CC/Worktrees/alpha/fix".into()),
            dry_run: true,
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(result["project"], "alpha");
    assert_eq!(result["title"], "【alpha】確認");
    assert_eq!(result["body"], question_body("alpha", "OWN", "確認"));
    assert_eq!(result["reminder"], reminder_text("alpha", "OWN", "確認"));
    assert!(h.fake.borrow().http_calls.is_empty());
    assert!(!h.ctx.here.join("state.json").exists());
}

#[test]
fn unreadable_client_list_prevents_posting() {
    let mut h = Harness::new();
    h.ctx.client_projects = h.root.path().join("missing.txt");
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "確認").unwrap();
    assert!(cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "確認".into(),
            body_file: body,
            ..Default::default()
        }
    )
    .is_err());
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[test]
fn missing_legacy_names_uses_existing_privacy_catalog_for_known_product_dryrun() {
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "191秒全編の評価を確認してください。").unwrap();
    let value = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "全編評価".into(),
            body_file: body,
            cwd: Some("D:/repos/KaraokeMaker".into()),
            dry_run: true,
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(value["project"], "KaraokeMaker");
    assert!(h.fake.borrow().http_calls.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    assert!(!h.ctx.client_projects.exists());
}

#[test]
fn missing_legacy_names_rejects_unknown_project_without_http() {
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "確認してください。").unwrap();
    let result = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "確認".into(),
            body_file: body,
            cwd: Some("D:/repos/unknown-catalog-fixture".into()),
            ..Default::default()
        },
    );
    assert!(result.is_err());
    assert!(h.fake.borrow().http_calls.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
}

#[test]
fn missing_registry_rejects_unqualified_body_before_read_and_post() {
    for proof in [
        json!({"ok":true,"access_allowed":false,"client_resource":true,"classification_unverified":false}),
        json!({"ok":true,"access_allowed":false,"client_resource":true,"classification_unverified":true}),
        json!({"ok":true,"access_allowed":"true","client_resource":false,"classification_unverified":false}),
        json!({"ok":true,"client_resource":false,"classification_unverified":false}),
    ] {
        let mut h = Harness::new();
        std::fs::remove_file(&h.ctx.client_projects).unwrap();
        h.fake.borrow_mut().source_guard_output = Some(cc_ask::core::Output {
            stdout: proof.to_string(),
            ..Default::default()
        });
        let result = cmd_ask(
            &mut h.ctx,
            &AskArgs {
                title: "評価してください".into(),
                body_file: h.root.path().join("not-created-body.md"),
                cwd: Some("D:/repos/KaraokeMaker".into()),
                dry_run: true,
                ..Default::default()
            },
        );
        let error = result.unwrap_err();
        assert!(error.contains("question source"), "{error}");
        assert!(h.fake.borrow().http_calls.is_empty());
        assert!(h.fake.borrow().posts.is_empty());
    }
}

#[test]
fn missing_registry_known_project_still_rejects_credentials_before_post() {
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "password=synthetic-private-value").unwrap();
    let error = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            title: "評価してください".into(),
            body_file: body,
            cwd: Some("D:/repos/KaraokeMaker".into()),
            dry_run: true,
            ..Default::default()
        },
    )
    .unwrap_err();
    assert!(error.contains("credential-like"));
    assert!(!error.contains("synthetic-private-value"));
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[cfg(windows)]
#[test]
fn qualified_source_lease_keeps_named_bytes_until_consumer_finishes() {
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "評価してください。").unwrap();
    let args = AskArgs {
        title: "評価".into(),
        body_file: body.clone(),
        cwd: Some("D:/repos/KaraokeMaker".into()),
        ..Default::default()
    };
    let lease =
        qualify_question_sources(&mut *h.ctx.effects, &h.ctx.client_projects, &args).unwrap();
    assert!(std::fs::write(&body, "changed").is_err());
    assert!(std::fs::rename(&body, h.root.path().join("moved.md")).is_err());
    assert_eq!(
        std::fs::read_to_string(&body).unwrap(),
        "評価してください。"
    );
    drop(lease);
    std::fs::write(&body, "changed after release").unwrap();
}

#[test]
fn missing_registry_qualified_routine_question_keeps_gate_before_config_or_http() {
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "許可をお願いします").unwrap();
    let result = qualify_question_sources(
        &mut *h.ctx.effects,
        &h.ctx.client_projects,
        &AskArgs {
            title: "Chromeの画面の計測".into(),
            body_file: body,
            cwd: Some("D:/repos/KaraokeMaker".into()),
            ..Default::default()
        },
    );
    assert!(matches!(result,Err(error) if error == cc_ask::ask_gate::BLOCKED_MESSAGE));
    assert!(h.fake.borrow().http_calls.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
}

#[test]
fn qualified_input_limit_rejects_body_and_decision_before_child_or_post() {
    for decision_too_large in [false, true] {
        let mut h = Harness::new();
        std::fs::remove_file(&h.ctx.client_projects).unwrap();
        let body = h.root.path().join("body.md");
        let decision = h.root.path().join("decision.json");
        std::fs::write(
            &body,
            if decision_too_large {
                "評価してください。"
            } else {
                "x"
            },
        )
        .unwrap();
        let oversized = if decision_too_large { &decision } else { &body };
        let file = std::fs::File::create(oversized).unwrap();
        file.set_len((1 << 20) + 1).unwrap();
        drop(file);
        let result = qualify_question_sources(
            &mut *h.ctx.effects,
            &h.ctx.client_projects,
            &AskArgs {
                title: "評価".into(),
                body_file: body,
                decision_file: decision_too_large.then_some(decision),
                cwd: Some("D:/repos/KaraokeMaker".into()),
                ..Default::default()
            },
        );
        assert!(matches!(result,Err(error) if error.contains("1 MiB")));
        assert!(h.fake.borrow().http_calls.is_empty());
        assert!(h.fake.borrow().launches.is_empty());
    }
}

#[test]
fn explicit_empty_cwd_matches_normal_current_directory_fallback() {
    struct CurrentRoot(common::SharedFake);
    impl cc_ask::core::Effects for CurrentRoot {
        fn http(
            &mut self,
            method: &str,
            url: &str,
            body: Option<&serde_json::Value>,
            discord: bool,
        ) -> cc_ask::core::Result<serde_json::Value> {
            self.0.http(method, url, body, discord)
        }
        fn run(
            &mut self,
            args: &[String],
            timeout: u64,
        ) -> cc_ask::core::Result<cc_ask::core::Output> {
            if args.first().is_some_and(|arg| arg == "git") {
                assert!(!args[2].is_empty());
                return Ok(cc_ask::core::Output {
                    stdout: "D:/repos/KaraokeMaker\n".into(),
                    ..Default::default()
                });
            }
            self.0.run(args, timeout)
        }
    }
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    h.ctx.effects = Box::new(CurrentRoot(common::SharedFake(h.fake.clone())));
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "評価してください。").unwrap();
    qualify_question_sources(
        &mut *h.ctx.effects,
        &h.ctx.client_projects,
        &AskArgs {
            title: "評価".into(),
            body_file: body,
            cwd: Some(String::new()),
            ..Default::default()
        },
    )
    .unwrap();
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[cfg(windows)]
#[test]
fn qualified_input_rejects_original_parent_junction_before_consumption() {
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let destination = h.root.path().join("owned-target");
    std::fs::create_dir(&destination).unwrap();
    std::fs::write(destination.join("body.md"), "合成入力").unwrap();
    let link = h.root.path().join("source-link");
    let mut cmd = Command::new("cmd.exe");
    use std::os::windows::process::CommandExt;
    cmd.creation_flags(0x08000000)
        .args(["/d", "/c", "mklink", "/J"])
        .arg(&link)
        .arg(&destination);
    let output = cmd.output().unwrap();
    assert!(output.status.success());
    let result = qualify_question_sources(
        &mut *h.ctx.effects,
        &h.ctx.client_projects,
        &AskArgs {
            title: "評価".into(),
            body_file: link.join("body.md"),
            cwd: Some("D:/repos/KaraokeMaker".into()),
            ..Default::default()
        },
    );
    assert!(matches!(result,Err(error) if error.contains("identity")));
    std::fs::remove_dir(&link).unwrap();
    assert_eq!(
        std::fs::read_to_string(destination.join("body.md")).unwrap(),
        "合成入力"
    );
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[test]
fn existing_unreadable_names_source_is_not_treated_as_missing() {
    let h = Harness::new();
    assert!(ProjectNames::load(h.root.path()).is_err());
}

#[test]
fn original_decision_text_keeps_strict_registry_requirement() {
    let h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    assert!(ProjectNames::load(&h.ctx.client_projects).is_err());
    assert!(ProjectNames::load_for_owner_questions(&h.ctx.client_projects).is_ok());
}

#[test]
fn missing_legacy_names_accepts_exact_resume_prefix_of_known_product() {
    let h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let names = ProjectNames::load_for_owner_questions(&h.ctx.client_projects).unwrap();
    assert_eq!(names.label("resume-KaraokeMaker"), "resume-KaraokeMaker");
    assert_eq!(names.label("RESUME-KaraokeMaker"), "RESUME-KaraokeMaker");
    assert_eq!(names.label("notresume-KaraokeMaker"), "プロジェクト不明");
    assert_eq!(
        names.label("resume-unknown-catalog-fixture"),
        "プロジェクト不明"
    );
}

#[test]
fn missing_legacy_names_known_project_empty_title_uses_filtered_body() {
    let mut h = Harness::new();
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    let body = h.root.path().join("body.md");
    std::fs::write(&body, "191秒版を評価してよいですか？").unwrap();
    let result = cmd_ask(
        &mut h.ctx,
        &AskArgs {
            body_file: body,
            cwd: Some("D:/repos/KaraokeMaker".into()),
            dry_run: true,
            ..Default::default()
        },
    )
    .unwrap();
    assert!(result["title"]
        .as_str()
        .unwrap()
        .starts_with("【KaraokeMaker】"));
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[test]
fn real_git_nested_folder_and_worktree_dry_run() {
    let temp = tempfile::tempdir().unwrap();
    let repo = temp.path().join("alpha");
    let worktree = temp.path().join("alpha-wt").join("fix");
    std::fs::create_dir_all(repo.join("src")).unwrap();
    let git = |cwd: &std::path::Path, args: &[&str]| {
        let mut command = Command::new("git");
        command.arg("-C").arg(cwd).args(args);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let output = command.output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    };
    git(&repo, &["init"]);
    git(
        &repo,
        &[
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.test",
            "commit",
            "--allow-empty",
            "-m",
            "fixture",
        ],
    );
    git(
        &repo,
        &["worktree", "add", "-b", "fix", worktree.to_str().unwrap()],
    );
    std::fs::create_dir_all(worktree.join("src")).unwrap();
    let body = temp.path().join("body.md");
    let config = temp.path().join("config.json");
    let exclusions = temp.path().join("client-projects.txt");
    std::fs::write(&exclusions, "# isolated test exclusions\n").unwrap();
    std::fs::write(&body, "確認").unwrap();
    std::fs::write(&config, r#"{"owner_user_id":"OWN"}"#).unwrap();
    // The configured test temp directory may live under the PC tools tree,
    // whose plain repository folders deliberately share the PC tools label.
    let repo_project = if repo
        .to_string_lossy()
        .replace('\\', "/")
        .to_ascii_lowercase()
        .starts_with("d:/cc/tools/")
    {
        "PCツール"
    } else {
        "alpha"
    };
    for (cwd, project) in [
        (repo.join("src"), repo_project),
        (worktree.join("src"), "alpha"),
    ] {
        let mut command = Command::new(env!("CARGO_BIN_EXE_discord_ask"));
        command
            .current_dir(&cwd)
            .env("CC_ASK_CONFIG", &config)
            .env("CC_ASK_HOME", temp.path())
            .env("CC_ASK_CLIENT_PROJECTS", &exclusions)
            .args(["ask", "--dry-run", "--title", "確認", "--body-file"])
            .arg(&body)
            .args(["--agent", "Codex"]);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let output = command.output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let preview: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
        assert_eq!(preview["project"], project);
        assert_eq!(preview["title"], format!("【{project}】確認"));
        assert_eq!(preview["cwd"], cwd.to_string_lossy().as_ref());
    }
    assert!(!temp.path().join("state.json").exists());
}
