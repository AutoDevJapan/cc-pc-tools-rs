mod common;
use cc_ask::{ask_gate::*, commands::*};
use common::Harness;
use std::process::Command;

#[test]
fn routine_permissions_are_blocked_in_title_or_body() {
    for title in [
        "【GoDD-Press】Chromeの検査用タブで4画面幅を測る許可",
        "Chromeで画面を確認してよいか",
        "ログインしてもいいですか",
        "検査を実行する許可",
        "ビルドしてよろしいですか",
        "一時ファイルを作成してよいか",
        "一時フォルダを削除していいか",
        "片付けを進めてよいか",
        "共有枠を使ってよいか",
        "再試行してもよいか",
        "作業を続行してよいか",
        "Localのモデルに任せてよいか",
        "毎月の支払いを調べてよいか",
        "所有権の試験2行に注記を追加してよいか",
        "顧客の公開サイトのChrome検査の許可",
        "非公開タブでビルドする許可",
        "May I build and test?",
        "Chromeを閉じてよいですか",
        "ビルドを走らせて良いですか",
        "テストをやってもよいですか",
        "確認させてもらってもよいですか",
        "検証を実行しても差し支えないですか",
        "Ｃｈｒｏｍｅの検査の許　可",
        "検査のため一時ファイルを完全削除してよいか",
        "検査結果のログを送信してよいか",
        "テスト用のデザインを変更してよいか",
        "公開サイトを確認してもよいか",
        "公開サイトを検査してよいか",
        "ファイルハンドルを使って検証してよいか",
        "payloadを検証してよいか",
        "cargo build --releaseを実行してよいか",
        "課金済みアカウントでログインしてもよいか",
        "顧客サイトの請求画面を確認してよいか",
        "返金画面のテストを実行してよいか",
    ] {
        assert!(blocked(title, "", None), "title: {title}");
        assert!(blocked("相談", title, None), "body: {title}");
    }
    assert!(blocked("画面の検査", "許可をお願いします", None));
    assert!(blocked(
        "Chromeの検査の許可",
        "追加費用はありません。公開しない。",
        None
    ));
    assert!(blocked("【費用の上限】Chromeの検査の許可", "", None));
    assert!(blocked("相談", "【ログインしてよいか】", None));
    for context in [
        "予算内です",
        "月額0円のため",
        "有料プランは使いません",
        "費用をかけていません",
        "公開していません",
        "取り消せない操作ではありません",
        "不可逆ではない",
        "有料プランを使う予定はなく、通常の検査のみです",
    ] {
        assert!(
            blocked("Chromeの検査の許可", context, None),
            "negative/background: {context}"
        );
    }
}

#[test]
fn genuine_owner_decisions_and_non_permissions_pass() {
    for title in [
        "月5ドルまで使ってよいか。復元の検査もします",
        "Chromeで決済して有料プランを購入してよいか",
        "検査結果を一般公開してよいか",
        "検査結果の公開範囲を決める許可",
        "ライセンスの利用条件に同意して検査してよいか",
        "顧客に納期を約束してよいか。画面を確認します",
        "製品の方向性を変更してよいか。画面を作ります",
        "本番DBを完全削除してよいか。検査済み",
        "ビルド結果の報告",
        "画面案を選んでください",
        "スマホでの承認をお願いします",
        "この方向で進めてよいか。A案を採用します",
        "画面案Aで進めてよいか",
        "本番環境へデプロイしてよいか。テスト済み",
        "リポジトリをpublicにしてよいか。検証済み",
        "5 USDまで使って検証してよいか",
        "従量課金のAPIを使ってよいか。検証用",
        "May I publish this? tests passed",
        "Can I spend $20 for a test run?",
        "5.5ドルの予算を使って検査してよいか",
        "元に戻せない操作を進めてよいか",
        "費用なし、画面案Aで進めてよいか",
        "ポリシーを公開して問題ないか。確認済み",
        "月5ドルを払って問題ないか。検証用",
        "本番DBを完全削除して問題ないか。検証済み",
        "顧客へ納期を連絡して問題ないか。確認済み",
        "A案でよいですか。実装を進めます",
        "製品名はこれでよいですか。実装を進めます",
    ] {
        assert!(!blocked(title, "", None), "title: {title}");
    }
}

#[test]
fn library_gate_precedes_all_effects_and_dry_run() {
    let mut h = Harness::new();
    let body = h.root.path().join("q.txt");
    std::fs::write(&body, "一時ファイルを作成してよいか").unwrap();
    let before = h.state();
    for dry_run in [false, true] {
        let error = cmd_ask(
            &mut h.ctx,
            &AskArgs {
                title: "相談".into(),
                body_file: body.clone(),
                attach: vec![h.root.path().join("missing-attachment")],
                dry_run,
                ..Default::default()
            },
        )
        .unwrap_err();
        assert_eq!(error, BLOCKED_MESSAGE);
    }
    assert_eq!(h.state(), before);
    let effects = h.fake.borrow();
    assert!(effects.http_calls.is_empty());
    assert!(effects.llm_calls.is_empty());
    assert!(effects.runs.is_empty());
}

#[test]
fn all_owner_reasons_are_recorded_in_posted_questions() {
    for reason in [
        OwnerOnly::Money,
        OwnerOnly::Publish,
        OwnerOnly::Rights,
        OwnerOnly::Customer,
        OwnerOnly::Direction,
        OwnerOnly::Irreversible,
        OwnerOnly::Access,
    ] {
        let mut h = Harness::new();
        let body = h.root.path().join("q.txt");
        std::fs::write(&body, "検査してよいか").unwrap();
        let args = AskArgs {
            title: "検査の許可".into(),
            body_file: body,
            owner_only: Some(reason),
            ..Default::default()
        };
        cmd_ask(&mut h.ctx, &args).unwrap();
        assert_eq!(h.state()["questions"][0]["owner_only"], reason.as_str());
        assert!(!h.fake.borrow().posts.is_empty() || !h.fake.borrow().http_calls.is_empty());
    }
}

#[test]
fn cli_blocks_before_config_and_returns_78() {
    let dir = tempfile::tempdir().unwrap();
    let body = dir.path().join("q.txt");
    std::fs::write(&body, "許可をお願いします").unwrap();
    let out = Command::new(env!("CARGO_BIN_EXE_discord_ask"))
        .args(["ask", "--title", "Chromeの画面の計測", "--body-file"])
        .arg(&body)
        .env("CC_ASK_CONFIG", dir.path().join("missing.json"))
        .env("CC_ASK_HOME", dir.path().join("not-created"))
        .output()
        .unwrap();
    assert_eq!(out.status.code(), Some(78));
    assert!(String::from_utf8_lossy(&out.stderr).contains("聞かずに進めること"));
    assert!(!dir.path().join("not-created").exists());
    let invalid = Command::new(env!("CARGO_BIN_EXE_discord_ask"))
        .args(["ask", "--title", "検査の許可", "--body-file"])
        .arg(&body)
        .args(["--owner-only", "other"])
        .env("CC_ASK_CONFIG", dir.path().join("missing.json"))
        .env("CC_ASK_HOME", dir.path().join("not-created"))
        .output()
        .unwrap();
    assert_eq!(invalid.status.code(), Some(2));
}

#[test]
fn historical_owner_decisions_pass_and_explicit_routine_permissions_stop() {
    let rows: serde_json::Value =
        serde_json::from_str(include_str!("fixtures/ask-history-20261007.json")).unwrap();
    let rows = rows.as_array().unwrap();
    assert_eq!(rows.len(), 335);
    let mut stopped = 0;
    let mut owners = 0;
    for row in rows {
        let title = row["title"].as_str().unwrap();
        let denied = blocked(title, "", None);
        if row["classification"] == "owner" {
            owners += 1;
            assert!(!denied, "genuine owner question: {title}");
        }
        if row["expected_blocked"] == true {
            stopped += 1;
            assert!(denied, "unnecessary permission: {title}");
        }
    }
    assert_eq!(stopped, 8);
    assert_eq!(owners, 100);
}
