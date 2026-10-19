# ToDo3030: Codex 0.163.0候補の独立検証

## 判定

候補のWindows起動・常設進捗欄・`/progress`・終了は合格した。
ただし、記録された検査済みバイナリとSHA256が異なるため、**現在のハッシュ契約のままでは配備不可**。
過去の検査済み実行物との全バイト差は未確定である。ハッシュを合わせるだけの
`source.json`・manifest変更は行っていない。配備、launcher変更、本人の現在会話への適用は未実施。
これは親ToDo3021へ返す有限な独立検証であり、独立PRの作成・マージは行わない。

## 固定ソースと実物

| 対象 | 実測・記録 |
| --- | --- |
| 対象リポジトリの固定source commit | `7c1fe857547eb7e6850361752a26d9dfe868528e` |
| 公式tag / commit | `rust-v0.163.0` / `83086188ae0614bd882818ff1aa23f3194962012` |
| 公式tag object | `129b8ec7ee6f4511557a4856279f529025c25af4` |
| 検査済みpatch適用後commit | `e5578209650641348c778829c8a0b83eb6753dc1` |
| patch SHA256 | `21dd906d4ceace003835e8b0a59517e9a35187df7c702ddda75e4a9c1b3f96b7` |
| ToDo3029候補 SHA256 | `618bf6dcd243463d9058f0ce68b9e43ba2a2b2ac75c715f87ad40d06e453433f` |
| source.jsonのtested binary SHA256 | `bcebff4cf935c2bd728bbed130c3c21a329ac58727677326d6a1556124b46969` |
| source.json作業コピー SHA256（CRLF） | `a99be90209cb9a6bb00526f563c24b58d738a72873531946d69011df2abd7494` |
| 固定commitのsource.json blob SHA256（LF） | `e56a69c0df288586812eb5234bbd74c782fcfcd6ed0c5461302428b8fecea491` |
| 公式npm Codex SHA256 | `2692c8c7bb2cf8b0aefe638741fc90236f4b824c59578ce84994f9de58200993` |
| 公式npm host SHA256 | `07f3869f0849cd54b31cec1f86ec796f004501bd3a215135c1facc883a2b9988` |

候補と公式npm配布物は、ToDo3029の作業場所内の
`tools/codex-progress-tui/.ci-work/todo3029/delivery/` に原本を保持する。
今回、その原本を編集・移動・削除していない。絶対パスは同名JSONへ保存した。

3029の実ソースは `source/codex-rs/codex-rs/` にある。外側の
`source/codex-rs/` が公式Git checkoutで、そのHEADは公式commitのまま、patchは未コミット変更。
検査済み3023 checkoutのGitオブジェクト・indexを読み取りに使い、次を実行した。
`GIT_OPTIONAL_LOCKS=0` を指定し、主担当のソース・indexを書き換えていない。

```text
git --git-dir=<3023/upstream/.git> --work-tree=<3029/source/codex-rs> diff --no-ext-diff --exit-code e5578209650641348c778829c8a0b83eb6753dc1 -- codex-rs
```

終了0・差分なし。公式tag objectとtagの指すcommitも3029 checkoutで一致した。
本worktreeの `tools/codex-progress-tui` は固定source commitから変更されていなかった。

## npm tarballの記録不備

公式packageは `@openai/codex@0.163.0-win32-x64`。
保持tarballの実SHA512は次の128桁である。

```text
410190feaf5d089e42e63aa1805af0c9ca561b7b3323214d4e13222a5ea162dbba37f78cdd077b4ad7ac82708501fcd5103f08904dcdde81fbd6ccf3725b9dab
```

Base64へ変換した値は、source.jsonのnpm integrityと完全一致した。
展開済みpackage.jsonの版、Codex、hostの実SHA256も一致した。
一方、source.jsonの `npm_tarball_sha512` は末尾が `25bdab` の**127桁**であり、
実物の末尾 `25b9dab` と異なる。3029 build receiptは正しい実SHA512を保持している。
今回この固定sourceファイルを修正していない。

## 既存検査の再利用

原記録は `D:/CC/Tools/codex-jobs/jobs/20261020-001045-todo-3023/events.jsonl`。
完了イベントの終了コード・出力・コマンドを確認した。source照合が一致したので
新しいCargo・release build・全件検査は実行していない。
既存実行は共有gate、専用target、sccacheを使用している。

| 完了イベント | 検査と実結果 |
| --- | --- |
| item_190、02:01:55 JST | 関連nextest 5件成功、5724件は選択外。終了0 |
| item_166、01:59:12 JST | TUI Clippy `-D warnings` 成功、doctest失敗0・ignore 2、CLI release build成功。終了0 |
| item_206、02:12:00 JST | 全TUI nextestは5720成功・1失敗・8skip・315 leaky。終了1 |
| item_40、02:58:17 JST | 未変更の公式0.163.0でも同じdynamic-toolテストがdeadline elapsedで失敗。終了1 |

成功した関連nextestの選択式は次のとおり。

```text
cargo_gate.exe nextest run --locked --release -p codex-tui -E 'test(previous_phase_does_not_claim_running_for_finished_unknown_or_queued) | test(measured_progress_is_preserved_unknown_is_not_zero_and_test_copy_is_natural) | test(terminal_controls_and_bidi_are_not_rendered_and_small_dimensions_are_bounded) | test(parallel_progress_filters_waits_reports_unknown_and_expired_work) | test(parallel_progress_native_requires_live_processing_and_no_approval_or_input_wait)'
cargo_gate.exe clippy --locked --release -p codex-tui --lib -- -D warnings
cargo_gate.exe test --locked --release -p codex-tui --doc
cargo_gate.exe build --locked --release -p codex-cli --bin codex
```

全件の失敗名は
`app::tests::session_lifecycle_requests::dynamic_tool_requests_ignore_other_namespaces_and_dispatch_tui_namespace`。
baseline検査は `nextest run --offline --release -p codex-tui -j 1 -E 'test(dynamic_tool_requests_ignore_other_namespaces_and_dispatch_tui_namespace)'`。
公式tagのCargo.lockを内部版へ正規化する必要があり、このbaselineだけ `--locked` なしで実行されている。
この有限検証は固定ソースを変更しないため、公式由来の失敗を修正したとは扱わない。

PR255のheadは指定source commit、mergeは本worktreeの開始HEAD
`842e513191c2ced4364f8382b3a8f9654d01f9d3` と一致する。
今回 `gh pr view 255 --repo AutoDevJapan/cc-pc-tools-rs --json state,mergeCommit,headRefOid,statusCheckRollup`
でMERGED、Windows・CodeQL各解析のSUCCESSを再確認した。Cursor BugbotはNEUTRAL。
3028の別モデルレビュー原結果も確認し、Must/Should/Nitなし。
同レビューは今回見つけた127桁の記録不備や候補のハッシュ差の解消を保証するものではない。

## 候補そのもののWindows受入

候補実物を起動し `--version` は `codex-cli 0.163.0`、終了0・stderr空。
同じSHA256の候補を専用の隠れたConPTYで起動した。最終検査attempt5は次の結果だった。

- 常設欄が実データを取得し「作業中 1」「未計測」「一部の状態を取得できません」を表示。
- `/progress` を送信した後の差分に「並列担当の進捗」と実データ行、部分取得警告を確認。
- 幅120→60→148の変更APIが成功し、検査中のクラッシュなし。
- 一覧をEscで閉じ、`/quit` により終了0・stderr空。PID73472は終了済み。
- 検査用sessionは `01a155ad-6480-7a81-af6b-409aeb43e8a2`。本人のsessionは操作していない。

空の専用CODEX_HOMEと空の検査用Git作業場所を作り、認証不要のloopback providerだけを指定した。
モデルへの入力は送らず、OAuth・APIキー・認証情報・GPUは利用していない。
collectorは既存の読取専用経路を使い、状態の部分取得を成功した完全一覧に見せていない。
新しい常駐処理はない。

初期の検査ハーネスでは、親のリダイレクトされた標準ハンドルを引き継ぎ
`stdin is not a terminal` となった。ConPTY子のSTARTUPINFOで
`STARTF_USESTDHANDLES` とnullの標準ハンドルを指定して解消した。
検査用ホームのtrustキーはWindowsの正規化された小文字・バックスラッシュのパスに合わせた。
不完全だったattempt1〜4は受入成功の根拠に含めない。
ハーネス自身はCREATE_NO_WINDOW、子はPSEUDOCONSOLE属性で起動し、可視コンソールを要求しない。
標準ハンドル継承の根拠は[Microsoft terminalの説明](https://github.com/microsoft/terminal/discussions/15814)。

## バイト差の説明範囲と親3021の次工程

3029 receiptの非再現性の証拠を維持する。最初のunstamped版は
`ed0a81d0c137105a0913bdc3b210e8e0f09684ac130743ba65652aa319886cd8`。
二つの3029ビルドのPE timestampはそれぞれ19:03:34、19:09:26 UTC。
今回、候補のPE値 `0x6ad66ae6`（2026-10-19T19:09:26Z）も直接確認した。
単なるtimestamp差だけで過去のtested hashを説明できたとはしない。

さらに、過去のitem_149/item_166のコマンドにはSTABLE_GIT_COMMIT指定がなく、
3029最終ビルドには `e557820...` の指定がある。公式 `build-info/src/lib.rs` の
macroは未指定時に `dev` を埋め込むため、同じソースでもビルド入力が異なる。
絶対パスなど他の入力差の寄与は未確定である。
過去の記録にある `upstream/target/release/codex.exe` と
`upstream/codex-rs/target/release/codex.exe` はどちらも現存しない。
広いディスク走査で探していないため、別の場所に原本が無いとは断定しない。

親3021には次を返す。

1. 既知の保存先・元記録からtested binary原本を回収できるなら、候補とのPE・埋込commit・セクション差を直接比較する。
2. 原本を回収できない場合、過去のbyte同一性は未証明として保持する。必要なら親の範囲で
   stamp・linker timestamp・パス等を固定した再現性検査を設計する。今回その追加buildは行っていない。
3. 今回の候補に対する受入結果と過去のsourceテストを別々の証拠として扱う。
   過去のtested hashを候補へ書き換えるだけで済ませず、配備対象を明記した実行物の検証契約を確定する。
4. npm integrityを根拠に、127桁のSHA512記録を親の変更として正す場合も、別モデルでhash契約を再確認する。

今回の変更はこの報告と `todo-3030-revalidation.json` だけ。
patch・source.json・launcher・manifest・collector・配備先は不変。
自分の検査用プロセスが0件であることを確認し、保持対象3029の成果物は片付けていない。
