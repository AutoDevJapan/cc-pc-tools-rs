# 普通投稿の結合が失敗した段階を確認する

`unattended-runner intake bind --input <JSON> --diagnostic` は、
通常の結合処理を実行し、失敗時に固定の段階だけをJSONで返す。
同じ指定は `intake watch` の失敗にも使える。
成功時の出力、終了コード、元の照合・保留・保存・配送の条件は変わらない。
指定を付けない呼出しの `intake unavailable` / `busy` の出力も維持する。

返すJSONのdomainは `discord-intake-diagnostic-v1`。
本文、入力ファイル、保存先、認証値、生のエラー、指定した対象IDは出さない。
stageはコードで定めた語に限る。

| stage | 確認する箇所 |
| --- | --- |
| arguments / binding_input / history_watch_input | 引数と私的な入力の資格・形式 |
| saved_state_loader / busy | 既存の保存状態と通常の排他 |
| receipt_registry | 既存の質問・配送記録 |
| source_identity / saved_request | 保存済みの本人と投稿の対応 |
| product_context / configured_request_context | 製品別・指定報告の専用経路 |
| original_question_context / dedicated_context / conversation_context | 元の質問世代や専用文脈 |
| binding_contract / binding_version / source_verification | 結合契約・現在版・正規APIによる再照合 |
| target_task_scope | 元ToDoの取得・登録範囲・プロジェクト |
| binding_operation / history_watch | その他の結合・保存・配送処理 |

`application` と `state_change` は `not_inferred` を返す。
結合の保存後に配送だけ失敗する場合もあるため、
再試行前に `intake status` で元番号・版・段階を読み返す。
この診断から担当の実読、実反映、完了や保護設定の復旧を推定しない。

関連する配送記録の容量と発見処理の診断は
[配送記録の読取上限](discord-receipt-bounds.md)を参照する。
