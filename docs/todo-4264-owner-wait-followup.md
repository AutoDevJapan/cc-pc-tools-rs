# ToDo4264：所有待ち再評価の追加修正

親3387のIssue642/PR643に向けた有限なRust工程。
既存4142・4246・4250の担当を再実行せず、回答履歴、readiness値、
保存直前の依存変更、会話待ちの4点だけを修正する。
Go、共有実行物、実タスクの再評価、親のブランチ、PRとDiscordは操作しない。
別モデルレビュー、正式CI、統合と配布は親の工程に残す。

## 基準と変更

開始はmain `38f09f17d786d05a89876be7bc5dd1df866f9dba`。
指定Rust成果 `4ae5f8372c1e4c231b8e2d547594e08a61f051b9` を
`5894e725cd525647a5e396765bb7e9829eca8b08` で通常mergeした。
双方をancestorに保ち、親の4aeからforceなしで受領できる。
登録された元sourceの5パスと自分の作業場所を正規guardで確認し、
取り込んだ関連3ソースのGit blobは4aeと一致した。
原保護一覧、raw prompt、本物の台帳、実4023/3992/3684は読取・操作しない。

- `scheduler.question` は未回答のとき保留する。
  正規の回答履歴があれば質問履歴だけでは止めず、質問と回答自体は保持する。
  close pending、block、停止・cancel、停止する旨の回答、既存HOLDは引き続き優先する。
- `wait_ready` はnull/不存在だけを無効値とする。
  false/空文字でも保持し、下流`todo_wait::release_item`との不一致で所有印だけが外れる保存を防ぐ。
  ほかのHOLDのfalse/空文字の扱いは維持する。
- 最終ToDo lock内の最新storeで、候補とownerの両方を再判定する。
  別の依存先がdoneからtodoへ戻った場合も保存しない。
  外部guard/Gitをlock内へ追加せず、既存のsource CASとjob/liveness照合を維持する。
- `capacity_cycle`が扱う`conversation_wait`を、この入口でも保留として保持する。
  共有HOLD_FIELDSやほかの利用側は変更しない。

## Redと検査範囲

テストを先にcommitし、4aeと同じ実装でnextestを実行した。
28件中24件成功・4件が期待したRed（Cargo exit100）で、コンパイルは成功した。
false/空文字のreadinessについて8条件すべてで保存1を観測し、候補直下の2条件では
status=waitのままwait_on/所有印が外れた実際の部分保存をログに残した。
依存競合は両側を報告する形へ補強し、追加のRedで候補・owner両方の保存1を確認した。
最初の4失敗と追加1失敗は異なる実行記録であり、合算しない。

修正後の入口28テストはすべて成功した。
回答済みは完全解除のstatus、wait_on、所有印、dependency_readyと回答履歴を確認する。
未回答・停止・readiness不正・会話待ちは保存0とbytes不変を確認する。
2回目guardで依存を戻すテストでは、その並行変更以外のbytes不変を確認する。
旧成果の22/10/3件や312条件を今回の新しい成功件数へ流用しない。

関連するCLIのdirection/conversation/capacity/recovery、policyの所有・依存・cancel、
schedulerのtargeted dispatchとSystemHost/strict、対象doctestとClippyを選ぶ。
同じ所有付きtarget、共有Cargo gate、sccacheで順番に実行する。
全workspaceや無関係なGoのテストは追加しない。
全stdout/stderr・実引数・時刻・exitは[evidence](evidence/todo-4264/)に保存する。

## 配布候補と残る工程

最終ソースを固定してからtodo CLIだけを一度release buildする。
debug fixtureや旧候補は流用せず、候補は`release_eligible:false`とする。
候補のSHA256/size、実toolchain、VCS対応、Cargo.lock・manifest・dep-infoを含む
入力閉包と各Git blob/mode/raw SHAを保存し、候補と完全記録の場所を親へ返す。
親による新しい別モデル確認・同headの正式CI・通常merge・旧hash/ACL CASの確認が済むまで未配布。
この工程の完了を元3387全体や実4023の再開・SDK/全製品の完了へ広げない。

## 再開と清掃

作業ブランチは`scheduler/todo-4264-work`。途中成果を同ブランチへpushする。
実jobとHEADによるcheckpointを通常CLIで保存し、同じ入力で読み返す。
初回はscheduler.lockの取得期限でTimeoutErrorとなり、通常のbyte lockの占有を確認した。
共有のlockや担当は操作せず、最終HEADで有限に再試行する。
候補・証拠・再開情報を保持してから、自分の所有付き一時rootだけをsafe-deleteで片付ける。
最終結果、入力閉包、checkpointと清掃の結果はこの文書の末尾へ追記する。

## 最終結果

実行ソースは`0c910d370b59a501dd3cf58ebf54ef65f61ffb46`、
treeは`4fd532a2d5b344f8931217d033b33f432287ed97`。
以後は証拠とこの文書だけを追加し、Rust/Goのソースは変更していない。

| 対象 | 実結果 | 記録 |
|---|---|---|
| 入口の回帰 | 28件成功 | `green-owner-wait.*` |
| 関連CLI | 13件成功 | `green-related-cli.*` |
| 所有・依存・cancelのpolicy | 21件成功 | `green-policy.*` |
| SystemHostとtargeted dispatch | 6件成功 | `green-scheduler-boundaries.*` |
| duplicate JSON等のstrict入力 | 4件成功 | `green-strict-input.*` |
| Clippy | todo/policy全targetとscheduler libraryで成功、warningsをエラー扱い | `clippy-*` |
| doctest | 関連3 libraryでコマンド成功、各0件 | `docs-related.*` |
| Rust format | 変更2ファイルで成功 | `format-check.*` |
| releaseとCLI入口 | todoのみ1回build、help成功、実再評価0 | `release-todo.*`、`release-cli-help.*` |

入口のGreenは修正内容をcommitする直前に同じソースで行い、
ほかのGreen/Clippy/doc/releaseは上記source commitで行った。
原Redは期待したテストの失敗で、コンパイル失敗ではない。
候補記録用の補助処理にはパス区切りの分類不備が1回あり、同じ候補のまま補助処理を直した。
これをRedへ数えず、追加release buildも行っていない。

### 未配布の候補と入力

- candidate：このworktreeの`.ci-work/candidate/todo.exe`、8,771,584 bytes。
- SHA256：`369c82aaaa942161032b429608003e353e2d9f5c149505fed4a7690919652530`。
- 状態：`release_eligible:false`、配布0。既存Goのコード・設定・実行物を更新していない。
- rustc：1.95.0、commit `59807616e1fa2540724bfbac14d7976d7e4a3860`、
  host `x86_64-pc-windows-msvc`。shimではなく実toolchainのexeもhashで記録した。
- 完全な記録：`.ci-work/candidate/release-closure.private.json`、1,417,050 bytes、
  SHA256 `4250ef4b2d93c94d04a18e3952bfe0df411a574819fb383904dad820b1f690b3`。
- Cargoが宣言した入力2,849件（Git入力152、registry/native入力2,673、生成物23、
  Cargo設定1）とdep-info195件を保存した。
  公開用の`artifacts.json`にもGit入力のmode/blob/raw SHAと候補・閉包のhashを残す。
  dep-infoと必要な生成物を`.ci-work/candidate`内に保持し、PDBも同じ場所へ残した。
  Cargoの認証ファイルは開かず、設定値を表示していない。
  MSVC/Windows SDKの全配布物を収めた再現環境archiveではない。
  対象は明記したCargo入力と実compiler/toolchainである。

`final-source-inputs.json`はテストを含む最終関連ソースのGit blobとraw SHAを記録する。
`fix-from-4ae.patch`は同じRustの4aeからの追加差分だけで、main由来のGo変更を巻き戻さない。
親のremote branchは読み取り照合時にも4aeのままだった。
新しい別モデルレビュー・同headの正式CI・通常merge・旧hash/ACL CASと実配布・
実タスクの受入は、この子工程では実施していない。

### 清掃と親への保留報告

所有ID `54d7c4749feb42fe35f5482956923eac` の一時rootは3,934,735,176 bytes、5,745 entries。
safe-deleteのplanが大きさを理由にquarantineを指定したため、通常の退避を実行した。
退避先は`C:/Users/makhmeto/Downloads/削除希望/20261030-todo-4264-cargo-and-fixtures-54d7c474`。
state=quarantined、元root不在と退避先の存在を確認し、候補・閉包のSHAも再確認した。
容量はまだ回収していない。
次は既存の清掃工程で所有記録と退避物を扱う。本人の判断は不要。
親はこの新しい保留を1回だけ記録し、同じ保留を再送しない。
他担当のtarget、旧退避物、共有cacheは操作していない。

checkpointは初回の一時的なscheduler lock競合後、source commitで正規保存と
同じ入力の`--check`に成功した。
最終の証拠commit後も、実job/branch/HEADで正規checkpointと読返しを行い、
自己参照でHEADを変えないよう`.ci-work`の実行記録へ保存する。
有限な修正・テスト・候補作成の成功は、元3387全体や実4023再開の完了を意味しない。
