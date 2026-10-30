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
