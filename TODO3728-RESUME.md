# 元3728の再開記録

対象は技術待機の明示短縮だけ。元3669/3080の製品作業と元3726の保存修復は別担当が継続する。
開始HEAD: d2f26986。ブランチ: scheduler/todo-3728-3700-3669。

完了: 最新方針と対象規則、公開AutoDevJapan所有、保護判定clear、readyの原因と元3700の保存証拠を確認。
元3669はwait_on3720、retry-timeの旧期限1792947550、3720はdoneであることを開始時点に確認した。
状態変化を明示し、通常ロック内で照合して期限だけを早めるCLIを設計した。

完了: Redは45f9eb96の新CLI未実装で2件失敗、実装後のready/retry_soonerは15件成功。期限短縮・古い状態/別台帳/別世代拒否・所有保持・本人条件・保存失敗を確認。

完了: 0a7b54c3のcc-todo、cc-scheduler、cc-jobs、localai-web-rsの関連1,579件成功。f87babd4の未知条件保持後の関連301件成功。3ライブラリのdoc（各0件）と4クレートall-targets Clippy警告0。原ログは所有付き一時フォルダに保存。

別モデルレビュー3736はspec付きの通常subworkとして登録済み。通常方針のgpt-6-astra/maxを指定し、元3669等の本人限定Luna指定を使っていない。開始時点は未割当。dispatch-oneは共有ロックbusy、dispatch-subworkはTimeoutErrorで未起動。設定変更や直接の子job起動をせず、既存schedulerへ残す。

PR474/Issue472。f87babd4の配布用ビルドは成功したが、並行mainのPR465がcc-askを変更したため、その修正を取り込んだ版を使う。旧バイナリを配布しない。
現用todo.exeの開始時ハッシュは9e0a50d254f99ccc682fb437a2ac85f3176bfb8fa98d06fdd53074b7832363c6。配布前に再照合する。
正規checkpointは0a7b54c3を保存し読戻し済み。以後のHEADも終了前に保存する。

次: 統合した固定headの別モデルレビューとCI、Must解消、通常merge、todo.exe配布とハッシュ/source読戻し、元3669の保存・解除・通常継続を実確認する。登録やビルドだけで完了にしない。
元3700/3669へ結果を返す。本人入力は不要。

一時データ所有ID: c966c77efd12b0e5eb5a6c270cb5813c（safe-deleteのcreateで作成）。終了時にplan、通常削除、finishで回収する。
配布の一時フォルダ所有ID: 4e83fbb6331fde4137e582e656f334ed（同じworktree内）。
