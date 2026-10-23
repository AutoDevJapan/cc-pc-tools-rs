# 方向記録版todo.exe 1本の更新資格を独立readerで照合する（ToDo3463・親3190）

対応Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/408 。親Issueは #325、生成元はPR #376 の通常merge。
この工程は読取専用の資格確認だけを扱う。`todo.exe` の置換・配布、ToDo 3050/3084/3110/3190 の状態変更、
本人回答の記録と質問の終了は親3190が担当する。共有guard（`repair-guard`）の source・profile・配布物は変更しない。

## 入口と出力

```text
todo-update-reader-3190.exe --verify-todo-runtime-update D:/CC/Tools/todo/todo.exe
```

`-verify-todo-runtime-update` と `=` 付きの形も受け付ける。引数はこの1組だけで、パスは絶対パスに限る。
callerからmanifest、hash、source、root、ToDo番号、許可ディレクトリを受け取らない。
不足・重複・追加の引数と相対パスは、照合を始めずに標準出力へ `{"qualified":false}`、終了コード1を返す。
引数なしは使い方を標準エラーへ出して終了コード2、`-h` と `--help` は終了コード0で、標準出力は空。

資格を得た場合だけ、終了コード0で次の固定項目を返す。

| 項目 | 値 |
| --- | --- |
| `qualified` | `true` |
| `domain` / `todo_id` / `verifier_todo_id` | `todo-runtime-update-3190-v1` / 3190 / 3463 |
| `target_kind` / `target` | `todo` / `D:/CC/Tools/todo/todo.exe` |
| `current_sha256` | 照合した現在の旧SHA（固定値と一致した場合だけ） |
| `new_sha256` / `new_bytes` | 新候補の固定SHAとサイズ |
| `source_repository` / `source_root` / `source_head` / `source_tree` | 固定source |
| `build_receipt_sha256` / `build_job` / `build_command` / `build_log_sha256` / `dep_info_sha256` | 原生成記録 |
| `inputs_verified` / `dep_info_entries` / `local_crates` | 照合した件数 |
| `baseline_qualified_by` / `baseline_source_receipt_sha256` | 旧版の資格を与えた既存用途とその証拠SHA |
| `data_access_granted` / `original_registry_recovered` | 常に `false` |

拒否は標準出力に `{"qualified":false}` だけを出し、終了コード1。標準エラーには固定の理由コード
（`argument`、`target_path`、`target_leaf`、`baseline_qualification`、`baseline_mismatch`、`source_root`、
`source_git`、`receipt`、`log`、`dep_info`、`input`、`closure`、`candidate`、
`guard_classification`、`recheck`）だけを出す。`argument` は引数の不足・重複・追加と相対パスで、照合を始めずに返す。
未知の現在SHA、パスの中身、receiptの本文は出さない。
返却時にhandleは閉じるので、この結果は将来の置換を予約するものではない。置換の直前の再照合と
有限CASは親3190の配布工程が行う。置換後の状態はこのreaderの対象外で、置換後に同じ照合を求めない。

## 固定値

| 対象 | 固定値 |
| --- | --- |
| 対象leaf | `D:/CC/Tools/todo/todo.exe` |
| 旧版 | SHA256 `f8710b516d3ecdd37564d7a1ab978c8689ab1bdd2ffaec4150d0e4bed4c0b983` |
| 旧版の資格 | 配布済み `D:/CC/Tools/client-records/scrub391.exe --verify-capacity-runtime-update` の結果が domain `capacity-runtime-update-3085`、todo_id 3085、target_kind `todo`、old `4b351db6…364f`、new `f8710b51…b983`、source receipt `a3bc15ae…6e86`、data access false と完全に一致 |
| source | `D:/CC/Work/unattended-operations-20261013-wt/todo-3190-3110`、origin `AutoDevJapan/cc-pc-tools-rs`、HEAD `0e16e81798ff4463c6ae6bcf6c4c57a695d9d13c`、tree `31d45a745d27271420e764f6dff493e3d58b3731`、`origin/main` の祖先 |
| build receipt | `todo3190-build-0e16e817.private.json`、SHA256 `20345f443566c16553dbd959d5cd07b53b8aa0e1579a55bff1a614ab0f75087c` |
| build log | `todo3190-release-0e16e817.private.log`、SHA256 `1fe224b19ba4d5265711e0119e554a035aa15b7cc7f4b0da327478bafb9fe1ef` |
| dep-info | `target/release/todo.d`、SHA256 `da3ce6ae3fcebe72fb5cecb701fc23efa70571086a5cd73a7d7642aca1e96941` |
| 新候補 | `todo3190-todo-0e16e817.private.exe`、SHA256 `d4392e6326f1868e2ffddc31a80e83d3170f93d639b1a04656066be387180718`、8,017,920 bytes |
| 件数 | 入力126件（dep-info 116件と manifest/lock 10件）、ローカルcrate 8件 |

旧版の資格はToDo3085の旧配布の証拠であり、このreaderはその結果を「現在の配布物が f871 である」
前提の確認にだけ使う。容量用途の資格を今回の更新資格へ名前だけ変えて流用しない。

## 検証の順序

1. 指定パスが固定leafと字句的に一致することを確かめる。親ディレクトリ、兄弟、ADS（`:`）、末尾の点、
   `\\?\`・UNC・短い別名は一致しないので拒否する。
2. 対象leafと全祖先が reparse point でない実ディレクトリ・通常ファイルであることを確かめ、
   書込・削除・改名を共有しない読取handleで開く。この時点では中身を読まない。
   handleの実パス、リンク数1、reparse属性なしを確認する。
3. 同じhandleを保持したまま、配布済みguardの既存の容量用途を子プロセスで呼ぶ。結果の項目と値が
   上の表と完全に一致し、終了コード0の場合だけ次へ進む。元client一致などで容量用途が拒否すれば止める。
4. その後でだけ同じhandleから対象を読み、固定の旧SHAと一致することを確かめる。違えば拒否し、値を出さない。
5. source rootの祖先と自身が reparse point でないこと、実パス、guardの `--guard-path` が clear であることを確かめる。
   Gitは `GIT_*` 環境を除き、窓を出さず、期限付きで呼ぶ。worktreeの最上位、origin、HEAD、tree、
   `origin/main` の祖先であることを確かめる。Gitに読ませるのは参照・設定・オブジェクトだけで
   （`rev-parse`、`remote get-url`、`merge-base`、`ls-tree`）、作業ファイルは開かせない。
   guardは1件ずつ分類するので、rootがclearでも、分類していない追跡ファイルをGitに読ませない。
6. receipt・log・dep-info・各入力・新候補は、それぞれ同じ方法で開いて保持し、guardの clear を確かめてから読む。
   読んだbytesそのものをhashして解析する。
7. receiptの全項目を固定値と照合する。未知の項目と `data_access_granted` が false でない記録は拒否する。
8. 各入力は、固定treeの通常ファイル（mode 100644/100755）のblobとreceiptのblobが一致し、
   working fileのSHA256がreceiptと一致することを確かめる。さらに、保持したhandleから読んだbytesの
   Git blob値をreader自身が計算し、固定treeのblobと照合する。一致とみなすのは、bytesがblobそのものか、
   `core.autocrlf=true` でLFをすべてCRLFにした形の場合だけ。NUL、単独のCR、CRLFとLFの混在は変換として認めない。
   入力以外の追跡ファイルは分類も読取もしないので、その変更は結果を左右しない。
9. 新候補のサイズとSHA256を固定値とreceiptの両方に照合する。
10. 最後に保持した全handleの同一性（volume・file ID・サイズ・更新時刻・リンク数・属性）と実パス、
    source rootの実パス、worktreeの最上位・origin・HEADを再確認する。照合中の変更は拒否する。
    入力は書込を共有しないhandleで保持しているので、照合中には変えられない。

## compiler closure の確認

dep-info はコンパイラが読んだローカルファイルの一覧である。次の条件をすべて満たす場合だけ、
receiptの126件の入力が、原dep-infoの116件と manifest/lock の10件にちょうど対応するとみなす。

- dep-info の対象が `<source>/target/release/todo.exe` で、全項目がsource root内の入力に含まれる。
- 入力からdep-infoの項目を除いた残りが、`Cargo.lock`、workspaceの `Cargo.toml`、
  dep-infoの各項目を含むcrateの `Cargo.toml` とちょうど一致する。どのcrateにも属さない項目は拒否する。
- logの最終行が release の `Finished` で、ほかは `Compiling` 行だけ。source root内のcrateとして
  コンパイルされた集合が、dep-infoから求めたcrateの集合と一致する。source root外のローカルpathは拒否する。
- 固定treeの各crateとrootに `build.rs`、`.cargo/config(.toml)` がなく、rootに `rust-toolchain(.toml)` がない。
  各 `Cargo.toml` に `build =` の指定がない。

次のものは入力に含まれず、このreaderは照合しない。toolchain の版、利用者・上位ディレクトリのCargo設定と
環境変数（`RUSTFLAGS` やsccacheなど）、worktree内の未追跡ファイル（未追跡の `build.rs` や
`.cargo/config.toml` など。固定treeに無いことだけを確かめる）、registry crate の展開済みsourceの実bytes
（`--locked` と `Cargo.lock` のchecksumで版を固定）。このreaderは再ビルドによる再現性の証明ではなく、
候補・原記録・固定sourceの相互の一致を確かめる。

## 受入条件とテスト

テストは `tools/todo-update-reader-3190` の `go test`。合成したGit repo、ファイル、receiptと、
guardのCLI応答を模した関数で、実物の配布先・guard・親の作業場所を使わずに確かめる。
理由コードも照合し、別の理由による偶然の拒否を合格にしない。

| 条件 | テスト |
| --- | --- |
| 正しい組だけが資格を得る | `TestQualifiesFixedUpdate` |
| 旧版の資格の後でだけ旧targetを読む | `TestBaselineIsReadOnlyAfterPurposeQualification` |
| 旧/新SHAの相違 | `TestRejectsBaselineMismatch`、`TestRejectsCandidateMismatch` |
| source/receipt/log/dep-info の改変と欠落 | `TestRejectsTamperedOrMissingEvidence`、`TestRejectsSourceChanges` |
| 別origin、HEAD/treeの移動、未merge | `TestRejectsSourceChanges` |
| 親・兄弟・ADS・別名、任意入力 | `TestRejectsOtherPaths`、`TestCLIAcceptsOnlyThePurposeFlag` |
| hardlink、junction | `TestRejectsLinks` |
| 照合中の置換・変更 | `TestPinnedFilesResistReplacement`、`TestRejectsChangesBeforeRecheck` |
| 元client一致 | `TestRejectsProtectedResources` |
| data access の拡大 | `TestRejectsDataAccessExpansion` |
| closure の過不足 | `TestRejectsClosureGaps` |
| Gitに作業ファイルを読ませない。入力以外の保護された追跡ファイルは結果を左右しない | `TestGitNeverReadsWorkingFiles`、`TestQualifiesFixedUpdate`、`TestRejectsProtectedResources` |
| 入力のbytesが固定treeのblob（そのままか、LFをすべてCRLFにした形）である | `TestInputsMustBeTheCommittedBytes`、`TestCheckedOutForms` |
| 実物の固定値 | `TestProductionProfileBindsOneFixedUpdate` |

symlinkの作成は権限が必要なため、合成テストは junction と hardlink で reparse 属性とリンク数の拒否を確かめる。
symlink も同じ reparse 属性で拒否される。

## 実測と未完了

### 最初の実装のRed

commit `5b22ab24` は仕様、CLI、固定値、受入テストだけを含み、`verifyUpdate` は常に `unimplemented` を返す。
`tools/todo-update-reader-3190` で `go test -count=1 ./...` は終了コード1。検証系の14テストが
subtest込み102行すべて `unimplemented` か `exactLeaf` の未実装で失敗し、fixture作成の失敗は0。
CLIの4テストはこの時点で実装済みの契約として合格した。コンパイル失敗をRedに数えていない。

### 最初の実装のGreen

`go test -race -count=1 ./...` は終了コード0。上位18件、subtest込み106件が合格し、失敗0・skip 0・
data race 0、136秒。`go vet ./...` とgofmtも差分なし。Cargo、GPU、Civ6は使っていない。

照合の要所を壊した8つの版を `go test -overlay` で当て、7つは対応するテストが失敗した
（容量用途の未知の項目、読取の順序、hardlink、最後の再照合、receiptの重複key、build script、入力のguard分類）。
祖先のreparse確認を外した版は生き残った。handleの実パス照合が祖先のjunctionを独立に拒否するためで、
junctionの拒否自体はテストで確認している。

### 別モデルのレビュー（ToDo3482）

gpt-6.1-sol・推論max（job `20261024-022652-todo-3482`、6分34秒、終了コード0）が、base `dbb1c79c`・head `d9edfd4d` の
変更10ファイルを全文読んだ。レビュー担当はテスト・ビルド・CIを実行していない。結果は Must 1件、Suggestion 2件、Nit 1件。

- Must: 追跡ファイル全体への `git diff HEAD` が、1件ずつの保護判定より先にGitへ作業ファイルを読ませていた。
  126件の入力以外にも読取が広がり、テストはreader自身の読取だけを見ていたので検出できなかった。
- Suggestion: closureの節の「repo内の入力として過不足ない」が広く読める。PR本文でも、実測、正式CI、レビュー、
  merge、merge後の照合を分けて書く。
- Nit: 理由コードの一覧に `argument` がない。

### Mustの修正

Gitに読ませるのを参照・設定・オブジェクト（`rev-parse`、`remote get-url`、`merge-base`、`ls-tree`）だけにし、
作業ツリー全体の差分確認を外した。代わりに、guardの分類と固定の後で読んだ各入力のbytesから reader 自身が
Git blob値を計算し、固定treeのblobと照合する（検証の順序の8）。入力以外の追跡ファイルは分類も読取もしない。
Suggestion と Nit はこの文書へ反映した。

- Red（commit `dd4b0273`、照合関数はstub）: `go test -count=1 ./...` は終了コード1、96秒。失敗はテスト6件と
  subtest 24件、合格83、skip 0、ビルド失敗0で、すべて意図した理由による。正例と保護対象の18 subtestで、
  Gitの呼び出しに `git diff` が記録された。保護したREADMEの日時か中身を変えると `source_git` で拒否された。
  CRLFとLFが混在した入力は資格を得ていた。
- Green（commit `74c87e30`）: `go test -race -count=1 ./...` は終了コード0、98.5秒。上位21件、subtest込み113件が合格し、
  失敗0・skip 0・data race 0。`go vet ./...` とgofmtも差分なし。
- 新しい照合の要所を壊した3つの版（blob照合を呼ばない、NULの確認を外す、CRとLFの対の確認を外す）は、
  すべて対応するテストが失敗した。

### 実物への事前の観測（merge前）

最初の実装から生成した検査用readerで、実物に対して1回照合した。終了コード0、9.6秒で、
`qualified:true`、current `f8710b51…b983`、new `d4392e63…0718`（8,017,920 bytes）、
source `0e16e817…` / tree `31d45a74…`、receipt `20345f44…087c`、入力126件・dep-info 116件・crate 8件、
`data_access_granted:false` を返した。

修正後のcommit `74c87e30` から `go build -trimpath` で生成したreader（SHA256 `c4fb0ad3…c11b`、vcs.modified=false）
でも実物を1回照合した。終了コード0、8.7秒で、同じ固定値の `qualified:true`（入力126件・dep-info 116件・crate 8件、
`data_access_granted:false`）を返した。実物の126件はすべて、固定treeのblobそのものか、LFをすべてCRLFにした形だった。

どちらもmerge前のsourceによる観測で、最終の結果はmerge後のsourceから生成したreaderで取り直す。
対象の置換、guardの変更、ToDoの状態変更はしていない。

補足として、親の作業場所に残るCargoの出力 `target/release/todo.exe` は、配布用コピーと同じ
SHA256 `d4392e63…0718`、同じサイズと更新時刻で、`target/release/deps/todo.exe` とhardlinkされている。
リンクを持つためreaderの照合対象ではなく、コピーと原出力が同じbytesであることの観測として記録する。

### 未完了

修正前のhead `d9edfd4d` では、専用workflow（windows、run 39994455364）とCodeQL（run 39994455228）が成功した。
修正後のheadの正式CI、修正の別モデル再レビュー、通常merge、merge後のsourceから生成したreaderによる実物の照合、
親3190への結果の返却が残る。
