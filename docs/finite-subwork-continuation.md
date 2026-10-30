# 有限作業の終了結果から継続する

関連する回帰: [#644](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/644)。

依存待ちの解除時には、消す予約の担当IDを`last_job`へ保存する。
有限subworkでは、現在世代のworker receiptと最新の登録jobを照合して終了結果を選ぶ。
通常ToDoの選び方は変更しない。
固定spec・所有・世代・終了と排出を確認できた場合だけ、通常prepareの保存時に
古いpointerを回復する。元のpointerと原workspace receiptは保持する。

## requestの有効期間

保存requestが有効なのは、受付時と同じresume世代・依存世代だけである。
どちらかが変わればrequestは監査用に残したまま不活性とし、通常経路へ戻す。
古いrequestのために後続の結果や更新されたspecを拒否しない。

正常終了・lost・回復取消・quota以外の失敗では、通常のsessionとモデル選択を保つ。
親jobの生存、追加のquota照会、過去の混雑対処の通算回数を、新しい起動条件にしない。
元sourceと使用済み履歴は書き換えず、新しい通常実行と実quota失敗の回復episodeを区別する。

同じ世代で未消費の明示契約は、従来どおり実親・scope・排出・fresh OAuthを照合する。
配布済みの旧v1形式では、任意のreceipt snapshotが無いschemaもそのまま検証する。
Start後の移譲は厳密に次の1世代に限り、原proofの世代を書き換えない。

## 依存解除後の実quota失敗

旧requestの有無に関係なく、現在の実親が`continue-subwork`で新しい世代を明示受付する。
親は受付前に子の完了だけを待って退席せず、現契約を登録してから通常dispatchと受領を行う。
親jobへの環境変数の書換えで受付を代行しない。

```text
todo.exe continue-subwork TASK_ID --source-job SOURCE_JOB_ID --parent-job-id CURRENT_PARENT_JOB_ID --resume-generation CURRENT_RESUME_GENERATION --dependency-generation CURRENT_DEPENDENCY_GENERATION --spec-sha256 REGISTERED_SPEC_SHA256
```

未受付は`SubworkParentContinuationRequired`として5分後の技術再確認へ残す。
表示理由は`subwork_parent_continuation_required`とし、本人判断待ちにはしない。
policyによるquota再試行では、最新の失敗sourceの使用済み回数と履歴に今回の1回も加える。
本人指定のモデルを置き換えず、policyの再試行計画は追加せずに元履歴を保持する。
実quota失敗の予算終了は記録と入口の両方で同じ`source_id`を照合する。

## 確認する境界

- 依存待ち解除後に最新担当の結果を選び、通常ToDoの3回目の準備も最新結果を使う。
- 古いrequest、親の交代、quota以外の失敗、spec更新、回数上限に達した正常結果を分ける。
- 実quotaの使用済み予算、旧形式互換、登録・Start・最終queueの世代とscopeを再照合する。
- 偽caller、別job・所有・spec・scope・世代、排出未確認、未完依存、本人停止を拒否する。
- 枯渇・未確認OAuth、API鍵だけの経路、追加支出、予算の巻き戻しを拒否する。
- Windows実行版の更新後に、固定spec・scopeの実行、原結果の受領、ACKの前進を確認する。

受付、起動、実操作、原結果の受領は別の結果として扱い、登録やプロセスの存在だけで復旧としない。
