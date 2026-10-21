# ToDo3238：固定理由コードを出すschedulerの有限な確認

## 今回の範囲と再開手順

2026-10-21の最新訂正に従い、親3085の固定HEAD
`ec080023bd28e4fa1522a1ef085ae664bf4765a3` から診断用schedulerだけを生成する。
親のブランチは `scheduler/todo-3085-capacity-decision`。
作業ジョブは `20261021-175844-todo-3238`、割り当て資源はcargo。

同じHEADの正式CI run39564547033 / job119103572935の成功を再利用する。
親の受領記録と完全ログを照合し、指定5検査の各PASS1行とClippyの完了を記録する。
この担当ではnextestとClippyを実行しない。旧92096faの476件成功は別の履歴とする。

既存の所有ID `0c7d583da6c03192db12646d4a0493d3` の生成先を再利用する。
通常の共有Cargoゲートとsccacheを使い、最大10並列の範囲を保つ。

1. 親ソース、既存生成先の所有、実ジョブのモデル・推論設定・資源を照合する。
2. `cc-scheduler` の `scheduler` だけを `build --release --locked` で1回生成する。
3. 生成終了後、同じバイト列を `.ci-work/scheduler-3085-subwork-reason.exe` へ
   上書きせずコピーする。SHA256・サイズ一致と独立したリンク数1を確認する。
4. 新候補の `--help` と `dispatch-subwork --help` だけを実行する。

各段階の前後で親HEAD・ブランチ・変更なしとソースの指紋を照合する。
完全なstdout/stderr・argv・終了コード・出力のSHA256、実ジョブ、生成物の絶対パスを
`.ci-work/3085-cargo-subwork-reason.private.json` に保存する。
途中状態は `.ci-work/3238-subwork-reason-*` に保存し、再開時は先に結果と
プロセスの終了を照合する。既に成功したビルドを繰り返さない。

親ソース・本番・共有実行ファイルは変更しない。旧receipt・候補は保持する。
todoのrelease、全件テスト、実3090 dispatch、別モデル起動、PR作成・マージは対象外。
親3085とレビュー3090は未完のまま、この有限確認の結果だけを親へ返す。
本人の判断は不要。共有ToDoの状態更新はスケジューラーに任せる。
この作業ブランチ名は変更しない。

## 現在の状態

親ソースは指定HEAD・ブランチで変更なし。CI完全ログのSHA256は
`23cb8f8d08f12486bb8eaf8ce5510a21d4bc5407949378f5a897370ce3643a8a` と一致した。
指定5検査の各PASS1行とClippyの完了行を確認した。ビルドと候補のhelpはこれから行う。
