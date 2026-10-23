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
   続けて、Gitの設定と参照をreader自身が読む。対象は、worktreeの `.git`、その登録先の `commondir`・`gitdir`・`HEAD`、
   共有repoの `config`、HEADの指すbranchと `refs/remotes/origin/main`（looseが無ければ `packed-refs`）、`shallow` である。
   各ファイルはguardの clear を確かめてから、reparse pointでないリンク数1のhandleで1回だけ読み、すぐ閉じる。
   共有repoはほかの多くのworktreeも使うので、読んだ後まで開いたままにしない。読んだbytesで次を確かめる。
   - `.git` は `gitdir: <絶対パス>` の1行で、登録先の `commondir` は `../..`、`gitdir` はこのworktreeの `.git` を指す。
   - `config` を厳密に解析する。`include`・`includeIf`・`url` の節を拒否する。core は通常の非bare repoのキーだけ、
     拡張は `worktreeConfig` だけを認める。origin のURLはちょうど1つで、固定のoriginと一致する。Gitと同じ意味に
     読めない書き方（継続行、見出しの後の値、旧形式の見出し、未知のescape、閉じない引用符、制御文字）も拒否する。
   - HEADの指すbranchが固定HEADのcommitを指し、`refs/remotes/origin/main` のcommitを求められる。
   - `packed-refs` はGitと同じに読む。先頭の行が `#` で始まれば、`# pack-refs with: ` と既知の特性
     （`peeled`・`fully-peeled`・`sorted`）だけを認める。Gitは `sorted` を宣言したファイルを二分探索するので、
     宣言があれば名前がbyte順に厳密な昇順であることを求める。宣言が無ければ、Gitと同じく並びを問わない。
   - `config.worktree`・`info/grafts`・`objects/info/alternates` が無い。`shallow` は40桁のcommitの行だけ。

   Gitは実行しない。objectsもreader自身が読む。読むのは共有repoのobjectsディレクトリにあるloose objectと、
   `objects/pack` の `pack-<40桁>.idx`（version 2）と対になる `.pack`（version 2か3）だけである。
   alternates・commit-graph・multi-pack-index・replace refは読まない。guardが `objects` と `objects/pack` の
   ディレクトリ、各index・pack・loose objectを1件ずつ clear と分類してから開く。`objects/pack` の項目は、
   分類の後で固定したhandleから `GetFileInformationByHandleEx` で列挙し、パスから開き直さない。固定の後で
   このディレクトリが改名され、同じ名前にjunctionが置かれても、列挙するのは固定したディレクトリの項目だけである。
   indexは末尾のchecksumを確かめ、packは先頭の件数と末尾のchecksumがindexの記録と一致する場合だけ使う。
   packの各entryは、そのままの形か、offset delta・名前を指すdeltaとして同じpackの中から組み立てる。
   組み立てはGitの `unpack_entry` と同じ順で行う。まずentryの見出しだけを基底までたどり、同じentryに戻る
   循環と、深さの上限を超える連鎖は、何も展開せずに拒否する。前の読取で組み立ててcacheに残したobjectに着いたときも、
   そのobjectの連鎖の深さを足して数える。次に基底を展開し、内側のdeltaから1つずつ展開して当てる。
   同時に持つのは、組み立て中のobject、delta 1つ、その結果だけである。どのobjectも、型と中身から計算したSHA-1の
   名前が求めた名前と一致する場合だけ使う。そのため、照合中に共有repoのobjectが増えたり、別のstoreを指す
   ファイルが一時的に置かれたりしても、readerが読む中身は変わらない。読取の量には上限を置く（次の節）。
   objectsで確かめるのは次の3つ。
   - 固定HEADのcommitのtreeが、固定treeと一致する。
   - `refs/remotes/origin/main` のcommitから届くcommitの中に、固定HEADがある。readerは固定HEADに着いた後も止まらず、
     届くcommitを全部読む。Gitの `merge-base --is-ancestor` は、答える前に、`origin/main` から届いて固定HEADからは
     届かないcommitを全部と、それぞれの親を読む（`commit-reach.c` の `paint_down_to_common`）。そのどこかに
     Gitが読めないcommitがあれば、Gitは履歴を拒否する（終了コード128）。これは実物と同じくcommit-graphが無い場合で、
     commit-graphがあれば、Gitが読むcommitはさらに少ない。readerが読む範囲はどちらの場合も含むので、readerも拒否する。
     `shallow` にあるcommitは、Gitのshallow cloneと同じく親を持たないものとして扱い、その先は読まない。
     読めない親があれば拒否する。
     commitはGitの `parse_commit_buffer` と同じに解析する。`tree <40桁>` と改行の後に1byte以上が要る。
     `parent ` で始まる行は、その後にこの行の長さ（48byte）を超えるbytesが続く場合だけ、
     `parent <40桁>` と改行の形の親として読む。Gitはそれより短い `parent ` の行を親として読まないので、
     readerはその形のcommitを拒否する。こうしてreaderが受け付けるcommitの親は、Gitが読む親と一致する。
   - 固定treeを再帰的にたどった一覧。`git ls-tree -r --full-tree` と同じく、通常ファイル・link・submoduleの
     entryを並べ、directoryは並べない。

   Gitは、調べたobjectの名前ごとに型を1つだけ記録し、同じ名前を別の型として使うと拒否する（`object.c` の
   `object_as_type`）。commitのtree行は名前をtreeとして、親の行は名前をcommitとして調べるので、1つの名前がtree行と
   親の行の両方に現れる履歴などは、Gitが拒否する。readerも名前ごとに型を1つだけ持つ。読んだobjectの型と、
   commitのtree行が指す名前のtree型を記録し、2つ目の型が付けば拒否する。親は、たどるときにcommitとして読むので、
   そのとき型が付く。shallowの境界のcommitの親は、Gitが調べないので、readerも読まず、型も付けない。
   正しいrepoでは名前ごとの型は1つなので、この確認が正しい履歴を拒否することはない。readerが読む範囲はGitより広い。
   そのため、固定HEADの祖先のうちGitが読まずに済んだcommitが壊れていれば、Gitが受け付ける履歴でもreaderは拒否する。
   広げたのは拒否する側だけである。

   shallowファイルの名前も型を持つ。Gitは最初にcommitを解析するときにshallowファイルを読み、そこにある名前を、
   履歴から届くかどうかに関係なく、すべてcommitとして調べる（`shallow.c` の `register_shallow`）。そのため、
   tree行や固定treeのdirectoryがshallowの名前を指すと、Gitは拒否する（`merge-base` は終了コード128、
   `ls-tree` は `object … is a commit, not a tree` を表示して異常終了する）。readerは、shallowの全ての名前に、
   storeを開く時点でcommit型を付ける。Gitはこの登録を最初のcommitのtree行を調べた後で行い、そのtreeとの
   食い違いは表示するだけで先へ進む。そのため、固定treeそのものがshallowに載っている形は、Gitは受け付けるが
   readerは拒否する。

   Gitが設定・別のstore・作業ファイルを読むことはない。guardは1件ずつ分類するので、rootがclearでも、
   分類していないファイルは読まない。
6. receipt・log・dep-info・各入力・新候補は、それぞれ同じ方法で開いて保持し、guardの clear を確かめてから読む。
   読んだbytesそのものをhashして解析する。
7. receiptの全項目を固定値と照合する。未知の項目と `data_access_granted` が false でない記録は拒否する。
8. 各入力は、固定treeの通常ファイル（mode 100644/100755）のblobとreceiptのblobが一致し、
   working fileのSHA256がreceiptと一致することを確かめる。さらに、保持したhandleから読んだbytesの
   Git blob値をreader自身が計算し、固定treeのblobと照合する。一致とみなすのは、bytesがblobそのものか、
   `core.autocrlf=true` でLFをすべてCRLFにした形の場合だけ。NUL、単独のCR、CRLFとLFの混在は変換として認めない。
   入力以外の追跡ファイルは分類も読取もしないので、その変更は結果を左右しない。
9. 新候補のサイズとSHA256を固定値とreceiptの両方に照合する。
10. 最後に、保持した全handleの同一性（volume・file ID・サイズ・更新時刻・リンク数・属性）と実パス、
    source rootと `objects`・`objects/pack` ディレクトリの実パスを再確認する。読んで閉じたGitの設定と参照は開き直し、同一性と
    SHA256が最初に読んだときと同じことを確かめる。無かったファイル（`config.worktree`、`info/grafts`、
    `objects/info/alternates`、looseで無かったref、`shallow` など）が今も無いことも確かめる。
    この再確認が拒否できるのは、再確認の時点で残っている変更である。途中で作られて再確認の前に消えたファイルは
    検出できない。そのためreaderは、alternates・grafts・worktreeごとの設定を読む処理を持たず、Gitも実行しない。
    objectは名前で中身を確かめるので、再確認の対象にしない。
    `objects/pack` は固定したhandleから列挙したので、列挙した項目は再確認するディレクトリのものである。
    固定の後でこのディレクトリが改名されれば、実パスが変わるので再確認で拒否する。
    入力は書込を共有しないhandleで保持しているので、照合中には変えられない。共有repoのconfigやrefは
    ほかのworktreeが書き換えることがあり、照合中に書き換わると `recheck` で拒否される。その場合は再実行する。
    共有repoで自動maintenanceがrepackすると、照合中にloose objectやpackが消え、`source_git` で拒否されることもある。
    この拒否も安全な側の結果で、再実行すればよい。

## objectsの読取の上限

objectsを読む量にはprofileの `objectLimits` で上限を置き、超えれば `source_git` で拒否する。
共有repoのobjectsが、メモリや読取の量を使い尽くす形に細工されていても、readerが使う量は上限の中に収まる。
実物の必要量は、reader自身の数え方で、配布済みguardの分類を通して測った値である（5回目の修正の後、
計測用の一時的な版で1回。`origin/main` は `c6b5ceaa`）。readerは `origin/main` から届くcommitを全部読むので、
commit数と読取byteは `origin/main` が進むと増える。どちらの上限も、実物の必要量の50倍以上ある。

| 上限 | 値 | 実物の必要量 |
| --- | --- | --- |
| `objects/pack` の項目 | 4,096 | 20 |
| indexとpackの組 | 64 | 6 |
| indexの合計 | 256MiB | 529,584 bytes |
| 読取byte | 512MiB | 6,755,668 bytes |
| deltaの連鎖（cacheに残したobjectの連鎖を含む） | 4,095（Gitが書く最大） | 3 |
| `origin/main` から届いて読むcommit | 65,536 | 1,291 |
| 固定treeの下のentry（directoryを含む） | 65,536 | 1,657 |
| 一覧のパスの合計 | 16MiB | 53,698 bytes |

読取byteには、packから読んだ圧縮bytes（読んだ時点で数える）、packのentryとdeltaで組み立てたobjectの大きさ、
loose objectのファイルの大きさと展開後の大きさを数える。大きさは、そのbytesを確保する前に数える。
中身の無いブロックで水増しした圧縮データも、読んだ分だけ数えるので上限で止まる。
1つのobjectは16MiBまで、cacheに残す組み立て済みのobjectは合計64MiBまでで、これは以前と同じである。
型を記録する名前の数は、読むcommit、そのtree行、固定treeの下のentryの上限で決まる。実物では2,663件だった。

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

テストは `tools/todo-update-reader-3190` の `go test`。合成したGit repo（実物と同じく、別の場所のrepoに属する
linked worktree）、ファイル、receiptと、guardのCLI応答を模した関数で、実物の配布先・guard・親の作業場所を使わずに確かめる。
理由コードも照合し、別の理由による偶然の拒否を合格にしない。objectの読取は、Git自身の出力
（`git cat-file --batch-all-objects`、`git ls-tree`、`git rev-list`、`git log`、`git merge-base`、`git verify-pack`）と比べる。
テストが起動するGitには `-c maintenance.auto=false` を付け、合成repoをテストが書いたとおりに保つ。Git 2.54.0は
commitの後に自動maintenanceを切り離して動かし、既定のstrategy（geometric）は、`objects/17` にloose objectが
2つあるだけでrepackして、looseのファイルを消す（`builtin/gc.c` の `too_many_loose_objects`）。

| 条件 | テスト |
| --- | --- |
| 正しい組だけが資格を得る | `TestQualifiesFixedUpdate` |
| 旧版の資格の後でだけ旧targetを読む | `TestBaselineIsReadOnlyAfterPurposeQualification` |
| 旧/新SHAの相違 | `TestRejectsBaselineMismatch`、`TestRejectsCandidateMismatch` |
| source/receipt/log/dep-info の改変と欠落 | `TestRejectsTamperedOrMissingEvidence`、`TestRejectsSourceChanges` |
| 別origin、HEAD/treeの移動、未merge | `TestRejectsSourceChanges` |
| 親・兄弟・ADS・別名、任意入力 | `TestRejectsOtherPaths`、`TestCLIAcceptsOnlyThePurposeFlag` |
| hardlink、junction | `TestRejectsLinks` |
| 照合中の置換・変更 | `TestPinnedFilesResistReplacement`、`TestRejectsChangesBeforeRecheck`、`TestObjectsIgnoreAlternates` |
| 元client一致 | `TestRejectsProtectedResources` |
| data access の拡大 | `TestRejectsDataAccessExpansion` |
| closure の過不足 | `TestRejectsClosureGaps` |
| Gitを実行せず、設定・別のstore・作業ファイルを読まない。読む全ファイルをguardが先に分類する。入力以外の保護された追跡ファイルは結果を左右しない | `TestNeverReadsWorkingFiles`、`TestReadsNoGitConfiguration`、`TestObjectsIgnoreAlternates`、`TestQualifiesFixedUpdate`、`TestRejectsProtectedResources` |
| Gitの設定と参照をreader自身がguardの分類後に読み、Gitと同じ意味に解釈する | `TestReadsGitMetadataItself`、`TestConfigParserAgreesWithGit`、`TestConfigParserRefusesWhatItDoesNotRead`、`TestPackedRefs`、`TestPackedRefsAsGitReadsThem` |
| objectをGitと同じに読む（loose、pack、offset delta、名前を指すdelta、commit・tree・祖先） | `TestObjectStoreAgreesWithGit`、`TestQualifiesFromPacks` |
| commitの親をGitと同じに読む。Gitが親として読まない `parent ` の行と、Gitが拒否する形を拒否する | `TestParseCommitAgreesWithGit`、`TestRefusesParentsGitDoesNotRead` |
| 1つの名前に2つの型を付ける履歴（shallowの名前を含む）を、Gitと同じく拒否する | `TestRefusesNamesOfTwoTypes`、`TestRefusesShallowNamesOfTrees` |
| 固定HEADに着いた後も、Gitの `merge-base` が読むcommitを読み、Gitが拒否する履歴を拒否する。正しい側枝は受け付ける | `TestReadsEveryCommitGitReads` |
| `objects/pack` を固定したhandleから列挙し、差し替えたjunctionの先を列挙・分類・読取しない | `TestListsOnlyThePinnedPackDirectory` |
| deltaの循環を何も展開せずに拒否し、連鎖の深さ（cacheに残したobjectの連鎖を含む）・読取byte・packの数・項目数・indexの合計・commit数・treeのentry数・パスの合計に上限を置く | `TestDeltaCyclesInflateNothing`、`TestDeltaDepthLimit`、`TestDeltaDepthCountsKeptBases`、`TestReadLimitCountsEveryByte`、`TestReadLimit`、`TestObjectLimits` |
| 壊れたobject・別のobject・保護されたobjectのファイルを拒否する | `TestRejectsDamagedObjects`、`TestApplyDeltaRefusesMalformedDeltas` |
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

### 別モデルの再レビュー（ToDo3491）

gpt-6.1-sol・推論max（job `20261024-033144-todo-3491`、4分17秒、終了コード0）が、base `d9edfd4d`・head `308cb24d` の
変更5ファイルを差分と全文で読んだ。レビュー担当はテスト・ビルド・CIを実行していない。前回のMustは「一部解消」、
前回のSuggestion 2件とNitは解消と判定された。新たな指摘は Must 1件、Suggestion 1件。

- Must: `remote get-url` などは既存のGit設定を読む。system・global・repo・worktreeの設定にある `include.path` と
  `includeIf.*.path` を隔離していないので、設定が保護されたファイルをincludeすれば、guardの1件ずつの分類より先に
  Gitがその中身を読み得る。修正案は、既存の設定を読ませないGitの実行環境にすること、必要な設定はguardが分類して
  固定したbytesからincludeを追わずに取ること、include先を保護した回帰テストを足すこと。実環境にそうした設定が
  あるという指摘ではなく、Gitの仕様とコードからの判断である。
- Suggestion: テストでGitの呼び出しを許す判定を、サブコマンド名ではなく引数列の全体で照合する。

### 2回目のMustの修正

Gitに渡すのを固定objectsの読取だけにした（検証の順序の5）。worktreeの登録・HEAD・ref・config・shallowは、
guardの分類の後でreader自身が読む。Gitは設定を1つも読まない空のrepoで動き、呼び出しは引数まで固定の3回だけ。
最後の再照合では、読んで閉じたファイルの同一性とSHA256、無かったファイルが今も無いことを確かめる（検証の順序の10）。
Suggestionは、テストの許可判定を引数列全体の一致にして反映した。

- Red（commit `6bda9f63`、隔離した環境はstub）: fixtureを実物と同じlinked worktreeにした。`go test -count=1 ./...` は
  終了コード1、131秒。合格90、失敗48、skip 0、ビルド失敗0。global設定から保護ファイルをincludeした例は `source_git` で
  失敗し、Gitが実際にinclude先を開こうとしたことを示した。
- Green（commit `2f804f13`）: `go test -race -count=1 ./...` は終了コード0、144秒。上位27件、subtest込み153件が合格し、
  失敗0・skip 0・data race 0。`go vet ./...` とgofmtも差分なし。Redのテストだけでも、同じ実装で138件が合格した。
  修正と同時に足したテストは、親commitのobjectが無いshallow clone、bareや `core.worktree` の設定、再照合の前の
  refのpack・alternatesの追加・同じ大きさと日時でのrefの書換え、受け付けたconfigをGit自身（`git config --list`）も
  同じ値に読むこと、packed-refsの書式。
- 新しい照合の要所を壊した11の版を `go test -overlay` で当て、すべて対応するテストが失敗した。壊したのは、
  include・includeIf・urlの拒否、登録先の逆リンク、`config.worktree` の不在、global設定の無効化、shallowの受け渡し、
  読んで閉じたファイルの再照合、不在の再照合、SHA256の比較、HEADの照合、originの照合、alternatesの不在。
- 実物の親の作業場所は、別の場所の共有repoに属するlinked worktreeで、branchは `packed-refs` にだけあり、
  共有repoはshallow cloneである。configには800件を超えるbranchの節と core・extensions・remote があり、includeは無い。
  読んだメタデータはguardの分類で clear だった。configの中身はログや文書へ出していない。

### 別モデルの再レビュー（ToDo3505）

gpt-6.1-sol・推論max（job `20261024-051252-todo-3505`、6分28秒、終了コード0）が、base `308cb24d`・head `8b4f34b9` の
変更8ファイルを差分と全文で読んだ。レビュー担当はテスト・ビルド・CIを実行していない。前回のMustとSuggestionは
解消と判定された。新たな指摘は Must 2件で、どちらもコードとGitの一次資料からの判断であり、再現テストは伴わない。

- Must: `objects/info/alternates` が無いことを確かめた後でも、共有repoのobjectsは変更できる。Gitの起動前に
  alternatesが置かれると、Gitはその内容と参照先を分類なしで読み得る。再確認の前に消されれば、最後の不在確認も通る。
  `GIT_OBJECT_DIRECTORY` の指定は、読取先の隔離にはならない。
- Must: `packed-refs` の見出しを読み飛ばし、`sorted` の宣言と実際の並びを確かめていない。Gitは `sorted` を信じて
  二分探索するので、逆順の記録ではGitが解けないbranchを、readerだけが受け付け得る。コロンの後の空白を欠く見出しも、
  Gitは拒否するがreaderは受け付けていた。
- 合わせて、この文書の「照合中の変更は拒否する」という断定は、途中で作られて消える変更の検出を保証しないと指摘された。

### 3回目のMustの修正

Gitの実行をやめ、objectsもreader自身が読むようにした（検証の順序の5）。looseとpack（version 2のindex、
offset delta、名前を指すdelta）を読み、どのobjectも名前（SHA-1）で中身を確かめる。alternates・commit-graph・
multi-pack-index・replace refは読まないので、照合中に別のstoreを指すファイルが置かれても、読む中身は変わらない。
`packed-refs` はGitと同じに見出しと `sorted` を解釈し、宣言と並びが合わないファイルを拒否する。
再確認の説明は、検出できる範囲に合わせて書き直した（検証の順序の10）。

- Red（commit `3b9b98d3`）: `go test -race -count=1 ./...` は終了コード1、163秒。上位26件が合格、3件が失敗し、
  subtest込みで合格153、失敗6、skip 0、ビルド失敗0、data race 0。失敗はすべて意図した理由による。
  alternatesだけにあるobjectで資格を得た。宣言と逆順の `packed-refs` と、空白を欠く見出しの `packed-refs` でも
  資格を得た（どちらもGit自身は `origin/main` を解けない）。`packedRefs` は逆順・見出しの誤り・未知の特性を受け付けた。
- Green（commit `fe1d5941`）: `go test -race -count=1 ./...` は終了コード0、156秒。上位32件、subtest込み180件が合格し、
  失敗0・skip 0・data race 0。`go vet ./...` とgofmtも差分なし。readerが読んだ全object、commit、tree一覧、祖先は、
  looseだけ、offset delta、名前を指すdelta、packとlooseの混在の4通りの合成repoで、Git自身の出力と一致した。
- 照合の要所を壊した18の版を `go test -overlay` で当てた。最初は17が検出され、HEADのcommitのtreeと固定treeの照合を
  外した版が生き残った。存在しないtreeを固定したテストでは、後のtree一覧の読取でも拒否されていたためである。
  READMEだけが違う実在のtreeを固定値とreceiptに記録するテストへ替えると、その版は資格を出し、テストが失敗した。
  壊したのは、`sorted` の並び、見出しの空白、未知の特性、objectの名前の照合、indexとpackのguard分類、
  loose objectのguard分類、shallowの境界、packのchecksum、indexのchecksum、deltaの複写範囲、deltaの結果の大きさ、
  treeの再帰、祖先の判定、HEADのtreeの照合、読んで閉じたファイルの再照合、不在の再照合、includeの拒否、alternatesの不在。
- 実物の共有repoのobjects（loose 558件、pack 6件とそのindex、multi-pack-index）は、すべてリンク数1だった。
  固定HEADのcommitはpackの中にある。

### 別モデルの再レビュー（ToDo3513）

gpt-6.1-sol・推論max（job `20261024-061453-todo-3513`、7分2秒、終了コード0）が、base `8b4f34b9`・head `206181d4` の
変更12ファイルを差分と全文で読んだ。レビュー担当はテスト・ビルド・CIを実行していない。前回のMust 2件と文書の指摘は
解消と判定された。新たな指摘は Must 3件で、どれもコードとGitの一次資料からの判断であり、再現テストは伴わない。
SuggestionとNitはない。

- Must: 改行で終わらない `parent ` の行を親として読んでいた。`tree <固定tree>` と `parent <固定HEAD>` だけで末尾に
  改行のないcommitを正しい名前で置き、`origin/main` にすると、readerは固定HEADを祖先と判定する。
  Git 2.54.0はその行を親として読まないので、判定が食い違う。
- Must: `objects/pack` のhandleは改名を許し、列挙ではパスから開き直していた。固定の後で元のディレクトリを改名し、
  同じ名前にjunctionを置くと、guardが分類していない場所の項目名を列挙できる。最後の再確認では、列挙したことを取り消せない。
- Must: 展開したdeltaを持ったまま基底へ再帰していた。自分自身を基底にする16MiBのdeltaで、深さの上限までに
  約64GiBを確保し得る。64MiBのcacheの上限は、展開途中のデータを数えない。indexは全件を保持し、packの数と
  indexの合計にも上限がない。

### 4回目のMustの修正

3件とも直した（検証の順序の5と10、objectsの読取の上限）。

- commitをGitの `parse_commit_buffer` と同じ長さと改行の判定で解析する。Gitが親として読まない短い `parent ` の行は、
  commitごと拒否する。Gitより受け付ける範囲を狭めるだけにした。
- `objects/pack` を、固定したhandleから `GetFileInformationByHandleEx` で列挙する。パスから開き直さない。
- deltaをGitの `unpack_entry` と同じ順で組み立てる。見出しだけで基底までたどり、循環と深さの超過は何も展開せずに
  拒否する。上限はprofileの `objectLimits` にまとめ、productionの値を固定値のテストで照合する。

結果は次のとおり。

- Red（commit `9fb6c2b5`）: 上限の構造体、列挙の直前に呼ぶ差し込み口、`openObjects` の引数だけを本体に足し、
  動作は変えていない。`go test -race -count=1 ./...` は終了コード1、192秒。上位32件が合格、8件が失敗し、
  subtest込みで合格186、失敗23、skip 0、ビルド失敗0、data race 0。失敗した8件はすべて新しいテストで、意図した理由による。
  - `parseCommit` は、Gitが親として読まないか拒否する5つの形を受け付けた。tree行だけ、tree行の改行なし、
    改行のないparent行、parent行で終わるcommit、2つ目のparent行の改行なしである。Git側の読み方は11の形すべてで想定と一致した。
  - 末尾に改行のない `parent <固定HEAD>` のcommitを `origin/main` にすると、Gitの `merge-base --is-ancestor` は
    終了コード1（祖先ではない）なのに、readerは資格を出した。
  - 列挙の直前に `objects/pack` をjunctionへ差し替えると、junctionの先にある `pack-ffff…` の名前がguardの分類まで届いた。
  - 自分自身を基底にするdeltaと、互いを基底にする2つのdelta（どちらも16KiB）で、拒否までに約251MBを確保した。
  - 深さ、読取byte、packの数、項目数、indexの合計、commit数、treeのentry数、パスの合計の上限は、どれも効いていなかった。
- Green（commit `7a008ac9`）: `go test -race -count=1 ./...` は終了コード0、195秒。上位40件、subtest込み214件が合格し、
  失敗0・skip 0・data race 0。`go vet ./...` とgofmtも差分なし。Greenではテストを3点強めた。循環の拒否で確保してよい量を
  1MiBから256KiBにした（見出しを深さの上限までたどる版を検出するため）。読取byteの計上を5つ（packの圧縮bytes、packの
  展開後の大きさ、deltaの結果、looseのファイル、looseの展開後の大きさ）に分け、どれか1つを外せば失敗するようにした。
  indexの合計は2組のpackで確かめ、1ファイルごとの上限と区別した。
- 照合の要所を壊した24の版を `go test -overlay` で当て、すべて対応するテストが失敗した。新しい照合では、循環の検出、
  深さの上限、読取byteの5つの計上、handleでの列挙、項目数、packの数、indexの合計、commit数、treeのentry数、
  directoryの計上、パスの合計、parent行の長さ、tree行の長さ、parent行の改行を壊した。以前の照合では、objectの名前、
  indexとpackのguard分類、packのchecksum、treeの再帰、祖先の判定、deltaの複写範囲を壊した。
- 途中、Greenの全体の実行が487秒かかった。同じ時間帯に同じテストをRedの本体とGreenで交互に2回ずつ動かすと、
  Greenは37〜41秒、Redは42〜47秒で、遅れはPCの混雑によるものだった。最終の実行は195秒である。

### 別モデルの再レビュー（ToDo3531）

gpt-6.1-sol・推論max（job `20261024-074948-todo-3531`、5分42秒、終了コード0）が、base `206181d4`・head `b3c0b718` の
変更10ファイルを差分と全文で読んだ。レビュー担当はテスト・ビルド・CIを実行していない。前回のMust 3件のうち、
`objects/pack` の列挙とdeltaの組み立ては解消、commitの解析は「一部解消」と判定された。新たな指摘は Must 1件と
Suggestion 1件で、どちらもコードとGitの一次資料からの判断であり、再現テストは伴わない。Nitはない。

- Must: commitが指す名前の型の食い違いを確かめていない。`tree <固定HEAD>` と `parent <固定HEAD>` のcommitを
  正しい名前で置いて `origin/main` にすると、readerは固定HEADに着いて資格を出す。Gitは固定HEADをtreeとcommitの
  両方としては扱えないので拒否する（`commit.c` のcommitの解析、`object.c` の型の照合）。
- Suggestion: cacheに当たるとたどるのをやめるので、深さの上限は展開していない部分にしか効かない。上限1でも、
  深さ1のobjectを先にcacheへ入れると、それを基底にする深さ2のobjectを受け付ける。

### 5回目のMustの修正

Mustを直し、同じ種類の食い違いも合わせて直した。Suggestionも直した（検証の順序の5、objectsの読取の上限）。

- Gitと同じく、名前ごとに型を1つだけ持つ。読んだobjectの型と、tree行が指す名前のtree型を記録し、2つ目の型が
  付けば拒否する。
- Gitの `merge-base` は、固定HEADに着いた後も、`origin/main` から届いて固定HEADから届かないcommitを全部読む。
  これまでのreaderは固定HEADに着くと止まったので、その先にあるGitが読めないcommitや、型の食い違うcommitを
  見逃していた。readerは `origin/main` から届くcommitを全部読むようにした。たどる順序に頼らないので、
  commitの日時は読まない。
- cacheに残すobjectに、その連鎖の深さを持たせる。cacheに当たった場合も、連鎖全体の深さで上限を確かめる。

結果は次のとおり。

- Red（commit `edbf9629`）: 新しい3つのテストを `go test -race -count=1` で実行し、終了コード1、14秒。失敗はすべて
  意図した理由による。Gitの `merge-base --is-ancestor` が終了コード128で拒否する6つの履歴で、readerは資格を出した。
  指摘の形（tree行と親の行が固定HEADを指す）、tree行と親の行が同じcommitを指す形、固定HEADより古い側枝の先に
  無いcommitがある形、側枝の先でtree行が固定HEADを指す形である。側枝の2つは、合流commitの親の並びを2通りにした。
  上限1で、cacheに残った深さ1のdeltaを基底にする深さ2の連鎖を読んだ。親の行が固定treeを指す履歴は既に拒否されていた。
  正しい側枝は、両方の並びでGitが終了コード0を返し、readerも資格を出した。data race・panic・ビルド失敗は0。
- Green（commit `c2ff8584`）: `go test -race -count=1 ./...` は終了コード0、268秒（変異テストと同時に実行）。
  上位43件、subtest込み226件が合格し、失敗0・skip 0・data race 0。`go vet ./...` とgofmtも差分なし。
- 照合の要所を壊した34の版を `go test -overlay` で当て、すべて対応するテストが失敗した。新しい10の版は、
  固定HEADで止まる、最初の親だけをたどる、tree行の型を記録しない、読んだobjectの型を記録しない、型の食い違いを許す、
  tree行をcommit型として記録する、cacheの深さを足さない、cacheの深さを0として残す、深さの比較を1つずらす、
  shallowの境界の親をたどる、である。固定HEADで止まる版は、側枝を先に挙げた合流でだけ検出された。
  親の並びを2通りにしたのはこのためである。前の24の版は、新しいコードに合わせて当て直し、すべて検出された。

### 正式CIの失敗（run 40040005613）と別モデルの再レビュー（ToDo3545）

5回目の修正のhead `4ed83158` で、専用workflow（windows、run 40040005613）が失敗した。CodeQL（run 40040005526）は成功した。
失敗したのは `TestObjectsIgnoreAlternates` の1件で、fixtureのloose commit（`178adb4b…`）のファイルが、テストが動かす前に
消えていた。readerの変更ではなく、テストの合成repoで動いたGitの自動maintenanceが原因だった。Git 2.54.0は、
commitの後にmaintenanceを切り離して動かす。既定のstrategy（geometric）は、`objects/17` だけを数えてloose objectの
総数を見積もり（`builtin/gc.c` の `too_many_loose_objects`。閾値は100を256で割って切り上げた1）、`17` で始まる名前の
loose objectが2つあればrepackしてlooseのファイルを消す。fixtureのcommitの名前は時刻で変わるので、まれにこの条件に当たる。
手元でも、`objects/17` に2つのloose objectを置いたrepoで普通にcommitすると、0.5秒以内に `objects/17` が消えて
packが1つできた。`-c maintenance.auto=false` を付けると、5秒後も残った。

gpt-6.1-sol・推論max（job `20261024-083553-todo-3545`、4分30秒、終了コード0）が、base `b3c0b718`・head `4ed83158` の
変更7ファイルを差分と全文で読んだ。レビュー担当はテスト・ビルド・CIを実行していない。前回のSuggestionは解消、
前回のMustは「一部解消」と判定された。新たな指摘は Must 1件で、コードとGitの一次資料からの判断であり、再現テストは
伴わない。SuggestionとNitはない。

- Must: shallowの名前を境界の印として持つだけで、commit型を付けていない。Gitは `register_shallow` で、履歴から
  届かない名前にもcommit型を付ける。届かないshallowの名前をtree行が指すcommitを正しい名前で置いて `origin/main` に
  すると、Gitは型の食い違いで終了コード128を返すが、readerは資格を出す。

### 6回目の修正

- テストが起動するGitに `-c maintenance.auto=false` を付けた（commit `59414fcd`）。readerは変えていない。
- shallowファイルの全ての名前に、storeを開く時点でcommit型を付ける（検証の順序の5）。

結果は次のとおり。

- Red（commit `64b28eb2`）: 新しい2つの形を `go test -race -count=1` で実行し、終了コード1。どちらも意図した理由で
  失敗した。届かないshallowの名前をtree行が指す履歴では、Gitの `merge-base --is-ancestor` が終了コード128なのに、
  readerは資格を出した。固定treeのdirectoryをshallowに載せると、Gitの `ls-tree -r` は失敗したのに、readerは資格を出した。
  ほかの3つの型の形は合格した。data race・panic・ビルド失敗は0。
- Green（commit `bb3bb1be`）: `go test -race -count=1 ./...` は終了コード0、255秒（変異テストと同時に実行）。
  上位44件、subtest込み228件が合格し、失敗0・skip 0・data race 0。`go vet ./...` とgofmtも差分なし。
- 照合の要所を壊した35の版を `go test -overlay` で当て、すべて対応するテストが失敗した。新しい1つの版は、
  shallowの名前に型を付けない。

### 実物への事前の観測（merge前）

最初の実装から生成した検査用readerで、実物に対して1回照合した。終了コード0、9.6秒で、
`qualified:true`、current `f8710b51…b983`、new `d4392e63…0718`（8,017,920 bytes）、
source `0e16e817…` / tree `31d45a74…`、receipt `20345f44…087c`、入力126件・dep-info 116件・crate 8件、
`data_access_granted:false` を返した。

修正後のcommit `74c87e30` から `go build -trimpath` で生成したreader（SHA256 `c4fb0ad3…c11b`、vcs.modified=false）
でも実物を1回照合した。終了コード0、8.7秒で、同じ固定値の `qualified:true`（入力126件・dep-info 116件・crate 8件、
`data_access_granted:false`）を返した。実物の126件はすべて、固定treeのblobそのものか、LFをすべてCRLFにした形だった。

2回目の修正のcommit `2f804f13` から `go build -trimpath` で生成したreader（SHA256 `2528d5cc…1825`、vcs.modified=false）
でも実物を1回照合した。終了コード0、10.0秒で、同じ固定値の `qualified:true`（入力126件・dep-info 116件・crate 8件、
`data_access_granted:false`）を返した。reader自身が実物のconfig・HEAD・ref（branchは `packed-refs`、`origin/main` はloose）と
`shallow` を読んで受け付け、Gitは空のrepoで固定objectsとshallowの写しだけを読んだ。

3回目の修正のcommit `fe1d5941` から `go build -trimpath` で生成したreader（SHA256 `fec6e90d…10dd`、vcs.modified=false）
でも実物を1回照合した。終了コード0、11.8秒で、同じ固定値の `qualified:true`（入力126件・dep-info 116件・crate 8件、
`data_access_granted:false`）を返した。Gitは実行せず、reader自身が実物のpackから固定HEADのcommitを読み、
`origin/main` からの祖先とtreeの一覧を求めた。

4回目の修正のcommit `7a008ac9` から `go build -trimpath` で生成したreader（SHA256 `efde5add…21a9`、vcs.modified=false）
でも実物を1回照合した。終了コード0、15.0秒で、同じ固定値の `qualified:true`（入力126件・dep-info 116件・crate 8件、
`data_access_granted:false`）を返した。`objects/pack` は固定したhandleから列挙し、読取の量は上限の中に収まった。

5回目の修正のcommit `c2ff8584` から `go build -trimpath` で生成したreader（SHA256 `341d866a…9bec`、vcs.modified=false）
でも実物を1回照合した。終了コード0、15.2秒で、同じ固定値の `qualified:true`（入力126件・dep-info 116件・crate 8件、
`data_access_granted:false`）を返した。`origin/main`（`c6b5ceaa`）から届く1,291件のcommitを全部読み、拒否はなかった。

6回目の修正のcommit `bb3bb1be` から `go build -trimpath` で生成したreader（SHA256 `a148a4c9…525e`、vcs.modified=false）
でも実物を1回照合した。終了コード0、15.2秒で、同じ固定値の `qualified:true`（入力126件・dep-info 116件・crate 8件、
`data_access_granted:false`）を返した。

いずれもmerge前のsourceによる観測で、最終の結果はmerge後のsourceから生成したreaderで取り直す。
対象の置換、guardの変更、ToDoの状態変更はしていない。

補足として、親の作業場所に残るCargoの出力 `target/release/todo.exe` は、配布用コピーと同じ
SHA256 `d4392e63…0718`、同じサイズと更新時刻で、`target/release/deps/todo.exe` とhardlinkされている。
リンクを持つためreaderの照合対象ではなく、コピーと原出力が同じbytesであることの観測として記録する。

### 未完了

修正前のhead `d9edfd4d` では、専用workflow（windows、run 39994455364）とCodeQL（run 39994455228）が成功した。
1回目の修正のhead `308cb24d` でも、専用workflow（windows、run 40002129622）とCodeQL（run 40002129620）が成功した。
2回目の修正のhead `8b4f34b9` でも、専用workflow（windows、run 40015024286）とCodeQL（run 40015024382）が成功した。
3回目の修正のhead `206181d4` でも、専用workflow（windows、run 40025021344）とCodeQL（run 40025021288）が成功した。
4回目の修正のhead `b3c0b718` でも、専用workflow（windows、run 40034507365）とCodeQL（run 40034507248）が成功した。
5回目の修正のhead `4ed83158` では、CodeQL（run 40040005526）が成功し、専用workflow（run 40040005613）は
テストの合成repoで動いた自動maintenanceのために失敗した（前の節）。
6回目の修正後のheadの正式CI、修正の別モデル再レビュー、通常merge、merge後のsourceから生成したreaderによる
実物の照合、親3190への結果の返却が残る。
