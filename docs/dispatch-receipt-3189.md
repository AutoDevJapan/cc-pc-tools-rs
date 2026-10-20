# 通常作業の起動応答喪失を照合して回収する

元3126 / 修復3189。完了済み製品修復と3084・3110の未完了は変更しない。
Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/327。

## 仕様

- 対象は既存の確認済みPC修復3projectだけ。既存の正式guardをproject/worktreeへ適用する。
  原保護台帳の代替・復旧や、未知の作業の採用を行わない。
  今回は既存Git作業場所だけを扱い、共通のGit管理領域と実branchも照合する。
- `scheduler dispatch-receipt --id <番号>` は1件の資格と不一致理由だけを返す。
  raw行、起動鍵、prompt/spec本文、他の作業や秘密は出力しない。起動・取消・通知・保存をしない。
  preparedの予約と一意な実起動が一致してから、外部の正式guardを呼ぶ。
- `--apply --expected-state-sha256 <検査が返したSHA>` は、同じ資格を再検査し、
  元行全体とprojectがロック内でも一致する場合だけ実担当を採用する。
  不適格なapplyと競合は失敗を返す。保存済み実回収receiptと同じ世代の再実行は
  already_recorded / saved=falseを返し、新たな書き込みをしない。
- 通常作業ではkey/todo_id/project/cwd/model/effort/親情報に加え、保存された正規scheduler
  promptの場所、現specの原文、元の登録場所、資源、および実ジョブに保存されたpromptを照合する。
  specや資源の不一致、別世代、競合担当、不明な実行状態、停止、質問、保護は無変更。
  親付き独立作業の既存照合は維持する。
- `wait` の終了済み担当は履歴と結果だけを回収する。元のwait_on/メモ/仕様/branch/HEADを保ち、
  終了済みjobを現在のrunning担当として保存しない。未完依存を解除しない。
  phaseはwaiting、resumeはtrueにし、last_jobと世代ごとの結果受領を記録する。
  keyは正規の依存解除で退役し、次のprepareが新しいkeyを作る。
- 自動回収で1件が失敗しても、そのIDの失敗を記録して他の項目の巡回を続ける。
  その回で回収に失敗・競合したIDは依存解除から除外し、次の巡回で実回収できるまでkeyを保つ。
  他の完了済み依存は解除する。正規CLIのDone/Start/Waitは従来の契約を保つため、
  元3126の回収は3189をdoneにする前に実施し、savedまたはalready_recordedを確認する。
- Gitの比較は既存のresolveでjunction等を実体へ解決してから行う。元のguardと
  同じGit共通管理領域・実branchという条件は保ち、作業場所やbranchを書き換えない。
- scope/prompt_readがnullなら前段の条件で止まり未評価である。failedは確認した不一致だけを示す。
  不適格なapplyはReceiptIneligible、入力形式の誤りはInputRejectedと区別する。
  dependency_readyは読取結果の真偽値だけを返す。
- 特別なレビュー再資格化や有限調査で別指示を追加したpromptは、今回の通常作業の範囲に広げない。
- 修復巡回も同じ照合を使い、次の正規担当が通常checkpointへ進めるようにする。
  checkpoint自体の本人・担当・worktree・branch・HEADの条件は緩和しない。

## 受入検査

1. 起動応答を失った通常作業が、実記録と一致した場合だけ回収される。
2. 待機中の正常終了結果を回収しても、未完依存と元資料は残り、再実行は無変更。
3. spec/資源/世代/実prompt/親/担当/モデル/保護/停止の不一致と行の競合は無変更。
4. 回収後の通常再開担当で既存checkpointが実branch/HEADを保存でき、旧担当や他担当は拒否する。
5. 既存独立作業、依存解除、checkpointの関連検査、対象クレートのdoctestを確認する。

実行記録、Red/Green、固定base/headレビュー、CI、merge、実行物のsource/hash、
元3126へのCAS付き結果配送を再開記録へ残す。登録や候補作成を実検証と数えない。
