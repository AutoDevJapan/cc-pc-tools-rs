# ToDo3219: 現在のPrd画面を保持する検査を1回測定する

元ToDo3103 / Issue289 / PR302 の独立した測定記録。実装修復・配布・画像承認の完了は扱わない。

## 固定条件

- source: `b978286abbe1a729cb733325e58a36fc063aa393`
- command: `cargo nextest run --locked -p localai-web-rs --test prd_page_preservation --no-fail-fast`
- Prdの独立基準（LFへ正規化後のSHA256）: `335ad98840c97d0767b8e78bf8166f48c6044a1f2d1a8a25c3bdea831320abf9`
- 対象は公開関数 `localai_web_rs::console_page::index()` を実際に呼ぶ追加テスト1件だけ。
- 想定はコンパイル成功後のassert不一致、exit100、1件FAIL。実測するまでRedとはしない。
- 親の作業コピー・後続assetを使わない。HTTP・新server・UI実装・GPU・Civ6・別Cargo検査を追加しない。
- 自分の既存作業ブランチへ測定記録のみをcommit/pushする。親のcode/PR/ToDo状態は変更しない。

## 事前確認

2026-10-21、通常のclient guardは担当worktree・共通Git領域・所有付き一時領域を非保護対象と判定。
実remoteは `AutoDevJapan/cc-pc-tools-rs`、GitHub APIでrepository ID `1406818713` と所有者を照合。
既存OAuthの実利用者は `mashfromband` (ID `88025750`)。認証値を表示・保存していない。
起動時の `D:/CC/AGENTS.md`、worktreeの `AGENTS.md` とbuild/test関連文書を確認。`CLAUDE.md` は存在しない。
共有ToDo3219は既に担当jobへ割当済み。状態更新はschedulerに任せる。

Gitの `core.autocrlf=true` は変更しない。checkoutではなく生のGit archiveを使い、
展開した全通常ファイルのGit blob SHA1とSHA256を測定前後に照合する。
固定commit自身が追加した差分は `localai-web-rs/tests/prd_page_preservation.rs` の1ファイルだけ。
なお `18d4f5e` と固定commit全体の比較では、`console_ui.rs` の非表示関連5行削除と
`operations_product_identity_tests.rs` の追加も存在する。固定sourceの指定を優先し、内容は変更しない。
`console_page.rs` と画面assetの変更をこの測定で加えることはない。

## 再開時

1. `preflight.json` と所有記録を読み、`invocation.json` があればテストを再実行しない。
2. `run-result.json` と全文 `stdout.txt` / `stderr.txt`、各SHA256、前後のsource manifestを照合する。
3. 実行中なら保存したPID・開始時刻で同じ処理を確認する。終了済み結果を取り違えない。
4. 残る記録・commit/push・一時領域のplan→通常削除→finish・scheduler checkpointを完了する。

所有ID: `530173a87be6551df0272bbcc8d02eb0`。
一時領域: このworktree直下の `codex-owned-530173a87be6551df0272bbcc8d02eb0`。
この一時領域のsourceとtargetだけが後片付けの対象で、共通Git領域と他担当の成果は保持する。

準備中の最初のarchive起動は、`Get-Command git` が2つの実行ファイルを返して
実行パスの組立に失敗した。Git archiveもCargoも起動していない。実行パスを1件へ固定して準備を続行する。

通常のarchiveは `core.autocrlf=true` によりCRLFへ変換された。コマンド単位で
`core.autocrlf=false` / `core.eol=lf` としても、明示された属性により起動用の
`tools/codex-progress-tui/activation/codex.cmd` だけはCRLFになった。
一時worktree属性を用いたarchiveでも同じ結果だったため、その1ファイルは同じ固定commitの
`git cat-file blob` の生バイトで復元した。追加テスト・renderer・画面assetは変更していない。
この差は `archive-conversion.json` / `archive-cmd-probe.json` /
`archive-raw-restoration.json` に残した。元のGit設定・index・原資料は変更していない。

測定対象は所有領域の `source-fixed`。展開後の全1,185ファイルのGit blob SHA1が一致し、
SHA256の集約値は `53d22cf2b31074d41d7b99f8dceb7c46ba5f5d5c58064ac02c1fcfba30e7298d`。
対象test・renderer・index/workload/shellのCRLFと単独CRはすべて0。
古い変換済み展開フォルダーは測定に使わず、同じ所有領域とともに片付ける。

現段階: sourceの準備と全バイト一致確認が完了。テストは未実行。
