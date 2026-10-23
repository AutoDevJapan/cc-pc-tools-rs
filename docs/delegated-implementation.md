# 別担当による回答の実反映を記録する

Issueと実装は同じ受入条件で管理する。実データ・本人原文・私的IDは公開しない。

## 仕様

`conversation source-verify` は正規APIの現在の質問・回答版と、保存済みの
canonical per-reply sourceを比較する。saved/todo0でも原本の検証は可能とする。
inspectionのSHA、本文の一致だけ、担当への配送だけでは実反映としない。
原本がない場合や編集された場合は拒否する。`source-reacquire` は現在のAPI版を
別のimmutableなcanonical世代へ取得し、旧原本と旧intakeの参照を保持する。
消失した原本を空作成せず、元の世代を上書きしない。旧SHAを新世代のSHAと偽らない。

`conversation implementation-record` は元質問・元ToDo・別担当の通常登録・
project/root・現在の実ジョブ・実行場所・元依頼への担当割当・revision・具体的な
実行結果と検証を結び付ける。用途別入力はimmutableに保存する。
ToDo writerは同じロックで両方のスナップショットを照合し、元ToDoに原文と
配送参照と反映証拠を追記する。本人の取消、元の停止、待ち、jobs、仕様は保持する。
記録後の正常読戻しに成功して初めて記録済みを返す。

`conversation close` は全現在返信に検証済みの実反映または従来の仕様記録が
あることを確認し、archive PATCH後のGETと再照合に成功した場合だけ終了を返す。
実反映の記録自体は質問へ投稿せず、自動で閉じない。説明の送信とAPI読戻しは
実担当が先に終え、同じ担当が明示的にcloseする。元の作業の再起動は行わない。
旧形式の票は実ジョブの原記録も必須とし、jobsの文字列だけで受理しない。

## 受入条件

- 架空の別担当が、取消済み元作業を保持して実反映を記録し、正常読戻しできる。
- 現在版・編集時刻・質問・原source・actor/job所有・project/rootが違えば拒否する。
- 実ジョブ欠落、別担当所有、実行場所や割当が違う場合は書き込まない。
- 競合時は両ToDoを保持する。同じ入力の再実行と応答消失後の読戻しは重複しない。
- 全現在返信が反映されるまでcloseしない。新返信・編集・新しい停止も照合する。
- PATCHの応答消失はGETで回復し、GET未確認を終了と報告しない。

## 利用順

```text
unattended-runner.exe conversation source-verify --request-id <元request> --message-id <本人reply>
unattended-runner.exe conversation source-reacquire --request-id <元request> --message-id <本人reply>
unattended-runner.exe conversation implementation-record --request-id <元request> --message-id <本人reply> --actor-todo-id <実担当> --source-sha256 <確認したcanonical SHA> --revision <実反映の40桁commit> --evidence-file <UTF-8 JSON>
unattended-runner.exe conversation close --request-id <元request>
```

再取得は原本が現在版と一致しない場合だけ使う。正常一致は同じ世代を返す。
evidence-fileは `completion`, `execution`, `origin_saved`, `identity`, `applied_at`
だけを持つ。各文字列には実施した成果、具体的なコマンド・結果・配布と読戻し、
元作業の保持、APIと原本の照合、取得以降の実反映確認日時を記す。
jobは引数で指定せず現在の `CC_JOB_ID` を使う。同じ証拠で再実行する。
別版・別成果を既存の反映へ上書きしない。記録コマンドは投稿・closeを行わない。
その戻り値の `question_closed_get_verified:false` は、この記録コマンドでは
GETによる終了確認を行わない意味である。終了済みかどうかはcloseの結果で確認する。
通常tickも別担当の記録を元作業の継続報告へ変換しない。記録後に編集された回答は
再取得を必要とし、元のcanonical sourceを保持する。担当の作業フォルダーを
片付けた後も、保存済みの実ジョブ・prompt・scopeによる反映票の照合は継続できる。

既存の独立チェックは不足の再現証拠として再利用する。新入口のGreenと
新実装の別モデルレビューは別に取得する。配布は最新mainと並行writerを照合して
必要な実行ファイルだけを更新する。保護判定の再配布は行わない。
