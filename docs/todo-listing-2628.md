# 共有ToDoの有限取得（ToDo2628）

2026-10-15 00:10 JST、稼働中の `cc-console todo list` は終了2、
`cli_error / response too large` を返した（元2626の実測）。
旧GET `/api/agent/todo` は `board()` から全原記録をitemsとgroupsに複製して返す。
CLIの16MiB上限を撤廃せず、原記録の削除・圧縮・状態変更で回避しない。
親2488のPR144、merge `0b703782a81e5c262f771caa849e766872591b2e` の修復は保持する。

## 取得契約

- CLI `todo list` はGET `/api/agent/todo/list` に切り替える。既存の生API、表示API、書込APIは互換性を維持する。
- 一覧は番号の昇順で最大100件、既定100件。続きは `--after <nextAfter>`。
- `--id <番号>` はその原ToDoを取得する。存在しなければ404。idとafterの併用は拒否する。
- 担当とジョブ参照はSHA256の不透明なハンドルで返す。同一文字列の同一担当を識別できるが、名前や本文を返さず、参照だけで担当の稼働成功を主張しない。
- 状態、進捗、依存の番号を取得する。ジョブと依存は各64件。件数と次のオフセットを必ず返す。続きは `--id <番号> --link-offset <nextOffset>`。各配列の終端はそれぞれのnextOffsetがnullであることを確認する。
- `--revision <前ページのrevision>` は同じ原記録の取得を照合する。変更があれば409。最初のページからやり直す。照合値は正規読取後のJSONのSHA256であり、原bytesの保存証跡ではない。
- `data.items` を返し、`metadataOnly=true / protectionMode=masked_metadata` を明示する。保護設定の有無にかかわらず、このAPIには原本文・履歴・パス・製品名を含めない。本文が必要な既存表示機能は従来の保護条件を維持する。
- サーバーとCLIの応答上限は1MiB。その他CLIコマンドの16MiB上限も維持する。CLIは保護モードと型を検証し、明示した項目だけを出力する。HTTP失敗やアプリケーション失敗は成功にしない。
- APIは原台帳の正規64MiB読取を使用する。GETは書込、backup作成、ジョブの起動、保護解除、委譲の有効化を行わない。紛失した原保護設定を作らない。

## 検査と反映

17MiB超の履歴、混在ジョブ参照、保護設定欠落、ページ・番号・依存取得、更新競合、不正なクエリ、通常HTTP、HTTP失敗、途中切断、Content-Lengthとchunked両方の上限超過を検査する。
関連nextestを担当の専用生成先で実行する。最終workspace/doc/Clippyは既存Build speed CIで確認する。
別モデルレビューはspec付き共有ToDoへ登録する。Mustを直し、固定HEADのCI成功後にマージする。

稼働CLIの初期guard判定は `classification_unverified=true / client_resource=true`。
対象の来歴を正規の保護判定で確認できるまで実行・差替えしない。これは審査ツールによる拒否ではなく原保護設定の未復旧による保護判定である。
配布時は稼働sourceとの並行変更、旧・新実bytes/hash、固定HEADを記録する。
サーバーの更新は既存taskと管理者窓口を使い、新PIDとhealthを照合する。他担当の常駐を無断で終了しない。
実CLI終了0/ok=trueと原台帳bytes保持の実確認まで、配布・復旧完了とは報告しない。
元2467の原記録復旧、元2626の再開確認、全製品の完了、GUI/OS再起動の検証はこの修復の成果に含めない。
