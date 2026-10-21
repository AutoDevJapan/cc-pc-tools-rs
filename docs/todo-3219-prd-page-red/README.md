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

## 実測結果

2026-10-21 09:02:19.0498212〜09:02:58.1757563 +09:00、指定のCargoコマンドを1回だけ実行した。
通常の `cargo.cmd` が呼ぶものと同じ `cargo_gate.exe` を `CreateNoWindow=true` で起動し、
この担当専用targetと通常sccacheを使った。追加のCargoコマンドは実行していない。

コンパイルは35.85秒で成功。nextest run IDは `5af14874-89bf-4f19-9046-db2f39088c72`。
選択は1 binary / 1 testで、0 passed / 1 failed / 0 skipped、Cargoの終了コードは100。
失敗は `prd_page_preservation.rs:11:5` のSHA256比較であり、コンパイル失敗や起動失敗ではない。

| 比較対象 | LF正規化後のSHA256 |
| --- | --- |
| 公開 `console_page::index()` の実際の戻り値 | `ef55500abbbb0fb2ea4430d4196285b3cccffa8d291fdfb50e9f1f1fa7f7448b` |
| 現在のPrd実応答から固定された期待値 | `335ad98840c97d0767b8e78bf8166f48c6044a1f2d1a8a25c3bdea831320abf9` |

これにより、固定候補が現在のPrdページを保持しないことをRustの実テストで確認した（Red）。
この結果だけで親の修復・配布・新UI承認を完了とはしない。

実行前後の1,185ファイルはすべて元のGit blobと一致し、集約SHA256も同じだった。
testの固定bytes、renderer、画面asset、`Cargo.lock` を変更していない。
`invocation.json` は1回の実行とPIDを、`run-result.json` は実際のCargo exit100を保存する。
記録用PowerShell自身の終了コードとは区別する。

全文の生stdoutは0 byte、SHA256は `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`。
全文の生stderrは9,002 byte、SHA256は `3593e92e5dffe6040b6cf61df8af486021c860cb2346874593002b336193ee53`。
テスト本体のstdout/stderrは、nextestが外側のstderrへまとめた内容も含めてそのまま保存した。
`.gitattributes` の `-text` で、ログがcommit時に改行変換されないようにしている。

## 保存と後片付け

測定用gate PID61992は終了後に不在と確認した。一時領域の通常planは
6,172項目 / 2,303,864,602 byteのため `quarantine` / reason `size` を指定した。
測定記録をcommit/pushしてGitHub側のHEAD `6a0a6cda199ceae7ae945edebb0f3d976b0f4088` の一致と、
生ログがcommit後も同じバイト列であることを照合した。
通常のquarantineは別ドライブへの移動を選び、`QUARANTINE_CROSS_DRIVE` で移動前に終了した。
これは削除命令や自動承認レビューによる拒否ではない。

このため、今回の明示指示に従い同じDドライブの新規の削除希望フォルダーへ手作業で退避した。
`D:/削除希望/20261021-todo-3219-prd-page-red-530173a8/data` に内容を保持している。
退避先は作成前に存在しないことを確認し、既存のデータや他担当の成果を開いていない。
この新規退避先のguardは未分類だったため、許可の根拠は今回の同一ドライブ退避の明示指示と
所有領域の照合とし、既存の保護対象へのアクセス許可へ広げていない。

rootを含む6,173項目について、移動前・直前・移動後のfile ID、属性、記録した時刻、
サイズ、SHA256、全hardlinkのtree内名数とOS link数を照合した。
全祖先と全項目のreparse不在、測定プロセスと子孫の終了、rootのvolume/file IDも確認した。
前後の全項目の集約値は `e0712125e30e062f9e4f868ecf9733888258a959281556f10c12599d17e6ed69` で一致。
元の所有領域の不在を確かめてfinishし、`source_absent` を受領した。削除したという意味ではない。
退避先の `理由.txt` とpreservation manifestにも完了を記録している。

照合に使った小さなGo製の一時検査道具も、別のsafe-delete所有領域で作成した。
こちらはplanが返した通常削除を別呼出しで実行し、finishの `source_absent` を確認した。
追加のRustビルド・GPU・モデル起動は行っていない。

最終保存では、この後片付けの記録も同じ担当branchへcommit/pushする。
scheduler用の実HEAD付きrequest/応答と終了時の再開メモは、担当worktreeの
`.ci-work/todo3219-checkpoint-{request,response}.private.json` /
`.ci-work/todo3219-resume.private.md` に保存し、正規checkpointの成功を確認してから終了する。
親が受領する原応答は通常のCLI経由とし、通知・メンション・受領helper・子Codexは追加しない。
