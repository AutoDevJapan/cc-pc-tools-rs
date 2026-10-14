# 共有ToDoの有限取得（ToDo2628）

2026-10-15 00:10 JST、稼働中の `cc-console todo list` は終了2、
`cli_error / response too large` を返した（元2626の実測）。
旧GET `/api/agent/todo` は `board()` から全原記録をitemsとgroupsに複製して返す。
CLIの16MiB上限を撤廃せず、原記録の削除・圧縮・状態変更で回避しない。
親2488のPR144、merge `0b703782a81e5c262f771caa849e766872591b2e` の修復は保持する。

## 取得契約

旧CLIの全件・本文付き出力から、`data.items`の有限な機械用metadataへ変更する。
`ok:true / exit 0`は要求したページの成功である。`complete`は先頭からの全件、または番号指定の全参照を1応答で取得できた場合だけtrueにする。
全件走査は`nextAfter`がnullまで番号順に取得し、各行の参照はその番号を`--id`で取得して各nextOffsetのnullを確認する。後続ページのcompleteだけで全件取得としない。

- CLI `todo list` はGET `/api/agent/todo/list` に切り替える。既存の生API、表示API、書込APIは互換性を維持する。
- 一覧は番号の昇順で最大100件、既定100件。続きは `--after <nextAfter>`。
- `--id <番号>` はその原ToDoを取得する。存在しなければ404。idとafterの併用は拒否する。
- 担当は正規`scheduler.job`、続いて`worker_receipt.job`、単一`job`を参照する。`activeJob`と互換欄`agent`は同じjobハンドルを返す。`schedulerPhase`は固定許可リストとnull/unknownのみ。
- ハンドルはjob用の種類を前置したSHA256で、同一性の照合用である。暗号化や推測耐性を保証しない。名前や本文を返さず、参照だけで担当の稼働成功を主張しない。
- ジョブidの抽出と未確認参照数はtodo.exeの共通`job_refs`を再利用する。正規idだけをハンドル化し、object形や不正な文字列は`jobsUnverified`に数え、原本を変更しない。cc-todo側の変更はこの共通読取関数の公開と`pending_on`の再公開だけであり、保存・起動の動作は変更しない。
- 状態には`recovery_unverified`を含め、進捗、原依存の番号と共通`pending_on`が判定した未完依存`waitPending`を区別する。不正な依存参照は`waitUnverified`に数える。`waitReady`はchatgpt-plus/retry-time/unknownと数値のnotBeforeのみを返す。
- ジョブ、原依存、未完依存は各64件。件数と次のオフセットを必ず返す。続きは `--id <番号> --link-offset <nextOffset>`。各配列の終端はそれぞれのnextOffsetがnullであることを確認する。
- `--revision <前ページのrevision>` は返すmetadataを照合する。状態・担当・依存の変更があれば409。本文や履歴だけの更新では変更しない。最初のページからの技術的再試行は最大3回とし、尽きたら不完全として次回へ残す。照合値は原bytesの保存証跡ではない。
- `data.items` を返し、`metadataOnly=true / protectionMode=masked_metadata` を明示する。保護設定の有無にかかわらず、このAPIには原本文・履歴・パス・製品名を含めない。本文が必要な既存表示機能は従来の保護条件を維持する。
- サーバーとCLIの封筒込み応答上限は1MiB。その他CLIコマンドの16MiB上限も維持する。CLIは保護モード、型、要求番号/offset/revision、件数と継続値を検証し、明示した項目だけを出力する。HTTP失敗やアプリケーション失敗は成功にしない。
- APIは原台帳の正規64MiB読取を使用する。GETは書込、backup作成、ジョブの起動、保護解除、委譲の有効化を行わない。紛失した原保護設定を作らない。

## 検査と反映

17MiB超の履歴、混在ジョブ参照、保護設定欠落、ページ・番号・原依存と未完依存、正規担当とphase、更新競合/本文のみ更新、不正なクエリ、通常HTTP、HTTP失敗の分類、途中切断、1MiBちょうど/超過、Content-Lengthとchunked両方の上限超過を検査する。
実APIのreply/redactを通った封筒をCLIと同じvalidatorで検証し、原台帳・queue state・mark・backupなしを確認する。
関連nextestを担当の専用生成先で実行する。最終workspace/doc/Clippyは既存Build speed CIで確認する。
別モデルレビューはspec付き共有ToDoへ登録する。Mustを直し、固定HEADのCI成功後にマージする。

稼働CLIの初期guard判定は `classification_unverified=true / client_resource=true`。
対象の来歴を正規の保護判定で確認できるまで実行・差替えしない。これは審査ツールによる拒否ではなく原保護設定の未復旧による保護判定である。
配布時は稼働sourceとの並行変更、旧・新実bytes/hash、固定HEADを記録する。
配布順はサーバー、health/API検証、CLI、実取得検証。新CLIと旧サーバーの混在は取得失敗を返すため成功としない。
サーバーの更新は既存taskと管理者窓口を使い、新PIDとhealthを照合する。他担当の常駐を無断で終了しない。
実CLI終了0/ok=trueと原台帳bytes保持の実確認まで、配布・復旧完了とは報告しない。
元2467の原記録復旧、元2626の再開確認、全製品の完了、GUI/OS再起動の検証はこの修復の成果に含めない。
