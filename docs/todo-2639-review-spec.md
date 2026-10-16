# 固定PRの別モデルレビュー（元ToDo2639）

この工程は複数ファイルをまたぐ読み取り専用レビュー。claude-sonnet-5-5 の既存OAuthサブスク枠を使う。子ジョブを直接起動しない。GPU、Cargo、Civ6、新費用、APIキーAI、本人PC操作、Terminal/UACは禁止。

## 対象と権限

- 元ToDo2639の独立工程だけを扱う。元1636/2016/2274の停止・依存・未完了を維持する。
- GitHub APIで repository ID 1285649124 の実ownerがAutoDevJapan、private=true、fork=falseであることを照合する。名前はメモリ内だけでAPIパスへ使い、stdout・会話・報告・ファイル名へ出さない。
- PR1002のbase=f3d946cb0f7e33b8cb5cbc0858091e583ca61350、head=b93e126b172adbb29ad2081af4732f4b42f8bc95、tree=8adce246c0dece6d2b5f70c7b521e5d39ef23750を照合する。別head/merge-refへ追従しない。
- 製品ソース・規則はGitHubの固定blobからのみ読む。PC上の旧製品フォルダ、除外一覧、秘密、保護設定、owner-work登録へ触れない。秘密・顧客名・生ログを表示しない。API応答の全量出力は禁止。内容はPowerShell/Go内部で取得・デコードし、対象リポジトリ名と派生表記をメモリ内で伏せてから必要なソースだけ読む。認証値を取得しない。
- ソース・PR・環境の変更、チェック成功の手動作成、再実行・dispatch・新CIの開始は禁止。この工程はレビュー結果だけを作る。

## 読むもの

まず固定headのAGENTS.md、CLAUDE.md、.github/CODEOWNERS、docs/runbooks/github-required-check-boundary.mdを読む。顧客固有の人間レビュー必須規則があれば、その箇所を顧客名なしで示す。自動生成されたstackブロックを変更しない。

Issue850と固定baseからheadへの全5ファイル差分を照合する：

1. backend/app/observability.py
2. backend/tests/test_observability.py
3. docs/runbooks/observability-slo.md
4. .github/workflows/backend-ci.yml
5. scripts/deployment-workflow.test.mjs

関連する型・helper・SLO判定・元仕様の該当節は、同じ固定GitHub sourceから必要な範囲だけ読む。web思考外注には顧客案件の内容を渡さない。

## 要件と確認済みの事実

壊れたworkerのlast_seen_atでsnapshot全体を落とさず、そのworkerをcriticalとし、他workerおよび売り違い重大アラートを保持する。欠損と測定不能を区別し、DB接続/SQL障害を握り潰さない。UI・製品方向・Prd設定を変えない。

元ca3のRuff format2件を956で修正。956のlocked依存でruff check/format、mypy220files、backend2236passed/3 opt-in skippedを実行し、その後残3独立Docker結合検査も3passed。b93と956の全top-level treeは.github/scripts以外一致した記録がある。backend全件をb93で再実行したとは主張しない。

b93のNode185passed/0skip。CodeQL run38879883760のJS/TS・Python・Actions・Rustが全success、解析results0。通常secret run38879889344/job116903806365もsuccess/no leaks found。2026-10-17のGitHub API読戻しで同headの全6check-runs（CodeQL集約を含む）がcompleted/success、PRはopen/draft=true、mergeable=true/cleanを確認した。これらのCIを重複実行しない。

公式生ログは指定された操作証拠保管先 D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016 のmanifest-*.private.jsonで原bytes/SHA256を保持する。ログ本文は出力しない。Go helperのレビューは製品のレビューではないため、この工程では元App差分と追加CI差分を実際に確認する。

## 観点と成果物

日時変換のSQLite/PostgreSQL差、部分列取得、ORM identity-mapの影響、null/invalid/missingの扱い、status/fresh/critical、回帰テストが壊れた実装を検出するか、実CI/required checkを弱めていないか、元runbookとの整合を見る。検査済み事実と矛盾する懸念は要実機確認として根拠を示す。

Must/Suggestion/Nitに分類し、ファイル:行・実害・具体修正を日本語で記す。MustがなければMust0と明記する。未確認を成功としない。顧客名・名前入りURL・生ログ・秘密は記載しない。

自分のworktreeの docs/todo-2639-review-result.md に確認したhead/base/tree、対象、結果、未確認事項を保存し、自分の作業ブランチへそのファイルだけcommitする。親worktreeは編集しない。最後に結果ファイルの絶対パスと実commitを返す。別ブランチへ変更した場合だけscheduler checkpoint-workspaceの正式手順を使う。この有限レビューだけを完了とし、元2639や原製品全体をdoneにしない。
