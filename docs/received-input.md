# 受領済みの入力を一件だけ再取得する

`received_input.exe --request-file <JSON>` は、通常hostに保存されたToDoと実ジョブ、
固定spec、schedulerのdispatch本文、ジョブが受領した本文を読み直す。
一覧・本文・環境変数は出力しない。CLIの保存先は固定し、別host/profileへの切替や
書込み、通知、起動、画像の資格化は行わない。

## 入出力

入力は `todo_id`、`job_id`、`registered_scope`、`source_kind` と、任意の `expected`。
source_kindは `scheduler_dispatch` または `scheduler_subwork`。未知の項目、重複キー、
不正なID・絶対パスでないscope、過大な入力は拒否する。
初回は本文を返さず、本文ごとのSHA256・バイト数とspecの参照名、既存の親edgeを返す。
再照会時は返された `version` を `expected` に指定し、同じ版を比較できる。
expectedは比較値であり、呼出側が書いた承認記録ではない。

成功条件は、一意なToDo、ToDoのjobsに存在する実job、同じ登録scope、通常の固定保存先、
同一のdispatch/受領本文、dispatch内の同一spec、既存の親edgeがすべて一致すること。
登録scopeと実作業先は正規guardへ渡す。spec/promptのリンクや保存領域外への参照は拒否する。
specは現在版または保存済みspec_historyに限る。履歴の版は、本文一致が一意な場合だけ選ぶ。
親edgeを本文やタイトルから推定しない。通常のscope_sourceは値を保持するが本人性の証明にしない。

出力の `source_kind` はschedulerの保存記録の種類であり、本人直入力やDiscord authorではない。
親edgeは、ToDoのsubworkと実jobのsubwork/parent_job_id、親の実jobとToDoを照合する。
構造化された元provider参照・本人性・画像等への対象対応は、この保存形式にはないため、
その不足を固定の理由で返す。現在の依頼の有効性や画像の承認可否は判定しない。

SHA256は指定した保存ファイルのバイト列そのものに対する値。
spec保存前の改行/BOM、provider変換前のnative raw行は復元も同一視もしない。
ファイルを二度取得し、選択行と登録scope、選択jobの照合項目、本文、親edgeが変化したら拒否する。
この比較は取得期間中の一致を確認するもので、過去の不変性や改変履歴の証明ではない。
他ToDoの進捗や選択jobのトークン更新だけでは拒否しない。

## 受け入れ条件

1. 正しい一件を取得し、同じversionで再照会でき、本文や他ToDoの内容を出力しない。
2. job/ToDo/source/spec版/親edge/登録scopeの不一致は固定理由と非0終了を返す。
3. 不在、権限拒否、リンク、取得中の変更は成功扱いしない。
4. LF/CRLF・BOM・UTF8をバイト単位で区別し、取得時のハッシュを元raw行hashと呼ばない。
5. 過大・重複・未知の要求を拒否し、保存された通常記録の未知フィールドは出力しない。
6. 子処理は正規guardだけを窓なし・限定環境・有限時間で呼び、認証値や本文をログへ出さない。
   guardがGitの出所照合を実行できるよう、通常の配置先 `C:/Program Files/Git/cmd` だけをPATHへ指定する。
   guardの分類未確認、権限拒否、応答不正は区別し、本人性や許可の不足へ読み替えない。

変更対象と利用側のテスト、doctest、Clippy、別モデルレビュー、CI、マージ、
固定ソースからのCLI配布と一件の実照会までを実装担当が確認する。
