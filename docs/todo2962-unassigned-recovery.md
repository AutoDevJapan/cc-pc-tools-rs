# 回収済み・未割当jobの予約を再開する

[Issue320](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/320)は、PR149の初回検査資源で
collectorの不具合を直しても、queueが`needs_recovery`から復旧できない問題を扱う。
製品の検査はqueued・runner ID0・steps0で、成功したとはしていない。

## 明示した復旧手順

先に担当が正規collectorで回収し、原作成記録に対応するsafe-deleteで削除または退避する。
この前提を満たしてから、固定設定と予約キーだけを与えて
`civ6-ci-queue resume-unassigned --config <固定設定> --entry <run/attempt/job>`を実行する。
定期tickが不明なfenceを自動解除する機能は追加しない。

- tickとWindows OSの排他を持ち、停止した同じ予約・同じfenceだけを扱う。
  開始したhelper/controllerが生きている間、成功済み、別OS、期限切れ、延期上限では拒否する。
  元の24時間と最大6回の未開始延期の制限を増やさない。
- safe-deleteの正確な原ID・job名・root・v2 checksumを照合する。
  `source_absent`または`quarantined`の閉じた記録だけを認め、元場所の不在を再確認する。
  退避時は、その場所の同じvolume/file IDのディレクトリを非追跡で確認する。
  記録を作り直さず、リンクや未知の資源へ許可を広げない。
- 固定helperの読取専用`verify-collected`で、管理者保護された最終回収root、同じSCM画像、
  停止・PID0、参照できた同じservice accountのprocessが0件、GitHubの当該登録不在を再確認する。
  読めなかったprocess数はeventとproof.jsonへ保持し、その不在を主張しない。`scm_stopped`はSCMの観測だけである。`n`が0でも全processを読めた保証ではない。管理者による回収markerと停止した同じSCMを根拠にする。`n`が正なら再開を拒否する。
  rootが消えた後の照合用Planは、元queueの固定identityから生成する。
  `execute_before=1`はこの読取専用照合のschema用で、実行・登録・回収には使わない。
- 外部照合の前後で新しいAPI readerを使い、実所有・固定source/workflow等の既存契約と、
  同じjob/run/attempt/headがqueued・runner ID0・空runner名・steps0であることを確認する。
  開始済み・完了済みjobを再実行しない。
- 旧失敗結果を`entries/<key>/recovery-<延期回数>/original-result.json`へatomicに保管する。
  回収照合の出力と、失敗原本SHA・未割当確認・閉じた回収記録を`proof.json`へ保存する。
  この復旧履歴の`manifest_sha256`は`proof.json`のSHA256であり、製品成果物のhashではない。
- 最後に同じ予約だけをreservedへ戻し、履歴を追加する。予算キーを追加せず、
  手動の成功status・公式検査の成功判定を作らない。次の通常tickが再供給する。

旧結果を保管してから台帳commitまでに中断した場合は、同じコマンドが保管済み結果を読み、
外部証拠を再照合して続行する。台帳commit後の再提出は拒否し、二重延期・二重予約を防ぐ。
他の主体による同時の外部GitHub変更を原子的に止める保証ではない。
後続helperも通常の実行前照合を行い、割当やsourceが変われば実行を拒否する。

## 検査と実運用の区別

回帰fixtureでは元のtickだけだとfenceが残ることを確認した。
新CLIの正常な再供給、同じ予約1件の保持、二重提出拒否、archive後中断からの再開、
割当済み/開始済み/完了済み/別source/回収証拠不足/生存process/期限/回数/取消/
外部照合中の割当変化を検査する。固定helper出力のidentity不一致も拒否する。
関連するqueueとWindows helperのGo race/vet、別モデル、必要正式CIを確認して通常mergeする。

実際の初回rootではSDKが作ったreparse1件により、正規safe-deleteのplanとquarantineが
`LINK_OR_REPARSE_POINT`で停止した。別の削除・移動経路は試さず原物を保持し、
独立工程3174がsafe-deleteだけの限定した正規修正と退避を担当する。
この復旧CLIの実操作、PR149の正式受入・merge・次の通常供給・hash・回収は未完了である。
