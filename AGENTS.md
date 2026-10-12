# PC用Rustツールの作業規則

新しいコードはRustで書く。APIキーで課金される呼び出しは使わない。

ほかのToDoの完了だけを待って終了するときは、必ず `D:/CC/Tools/todo/todo.exe wait <自分のToDo番号> --on <待ち先の番号>` で記録する。複数なら `--on` を繰り返す。「完了待ち」とメモだけを書いたり、オーナーの判断待ち用の `block` を使ったりしない。待ち先がすべてdoneになるまで担当は自動起動されず、完了すると通常の実行枠で再開する。詳細は `D:/CC/Skills/shared-todo/SKILL.md` と [完了待ちの仕様](docs/todo-wait.md)。

Rustの検査は[ビルドとテストの規則](docs/build-speed.md)に従う。作業途中は変更したクレートと依存先の関連検査をnextestで回す。全workspaceはマージ前の最新ソースに対して1回だけ回し、doctestも確認する。同じtargetを使うCargoは順番に実行し、別worktreeの依存はsccacheで再利用する。

別モデルのレビューでMustを解消し、CIが通ったら担当がPRをマージする。実行ファイルを更新する前に、稼働中のソースとの並行変更を確認する。

実際のGitHub所有者がAutoDevJapan・BM1314・mashfrombandのレポジトリは、人が要件を決め、AIが設計・実装・検査・修正・別モデルレビュー・マージ・既存Prd反映・後片付けまで担当する（2026-10-12 オーナー直接指示）。通常の操作や古い人のレビュー待ちを理由に止めない。既存Discord指示・回答は元の対象と範囲を保って実行し、同じ判断を再質問しない。未指定の要件・新費用・権利・顧客との新約束など本人だけが決めることだけ具体的にDiscordへ1回質問する。

Vercel/Railwayのオーナー保有アカウント内の全プロジェクトはソロ開発として進め、他メンバーの存在だけを理由に承認待ちにしない。他OrgのGitHub環境と顧客固有の契約は変更しない。既存Prd限定・Rakko限定・秘密保護を維持する。パスの保護判定はscrub391 --guard-pathを使い、保護対象の名前・パスを報告しない。最新指示はD:/CC/AGENTS.mdを起動ごとに読む。詳細と確認範囲はdocs/owner-autonomy.mdに記録する。

`queue-config.json` は全体を書き直さず、`codex_jobs.exe update-queue-config --patch-file <JSON>` で必要な項目だけ更新する。既存の `fixed_max_running` はオーナー・司令塔の指定なので、省略やnullを含む配備用パッチでも保持する。解除は健康な自動計算値16以上を連続3回実測してから `update-queue-config --clear-fixed` を使い、CPUが目標70〜85%へ近づく測定記録も確認する。このコマンドが `queue-config.CHANGES.txt` に1行記録する。
