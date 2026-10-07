# ChromeへのGitHub登録（2026-10-07）

このPC専用のManifest V3拡張とRust CLI・サーバーを作る。ポート17643の127.0.0.1だけで、トークン認証を使う。ChromeのMasashi（Profile 3）に導入する。

CLIでURL・「名前 – 何ができるか」・用途フォルダをpendingへ登録する。拡張はブックマーク バー > ビジネス > 副業に限って追加する。同一URLは追加せず、用途フォルダがなければ副業内に作る。成功・既存の結果だけdoneへ移す。権限はbookmarksと固定ポートだけ。外部送信なし。サーバーは窓のない定期タスクで起動する。

今日のruns/2026-10-07.jsonの候補からyomiyasuを除く12件のREADMEを読み、名前と分類を決めて登録する。daily-promptとchrome-bookmarks-sync-safe-edit Skillを更新する。

完了条件: Rust実装、拡張実装、窓なし配置、12件登録、毎日処理とSkill更新、RustとChrome API模擬試験、別モデルレビューMust 0、AutoDevJapan/cc-pc-tools-rsへのPRマージ、Discordスレッド1557264054454587453へaskで導入手順と「入れた」の返信依頼。

実装ジョブ20261007-180422-req-chrome-bookmark-extensionが担当する。commit・push・PR・CI・別モデル後のmergeは許可済み。APIキーなし。拡張のJavaScript以外の新規コードはRust。一時フォルダはsafe-delete所有記録とplan/finishで整理する。
