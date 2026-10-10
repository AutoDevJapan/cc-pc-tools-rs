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
