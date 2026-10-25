# 未起動レビュー3649の保存済み受付情報

元3255が、元3387の固定レビュー3649を再登録せずに調査するための読取専用入口。
既存の `required-checks-metadata` の取得済みsnapshotや実行回数は変更しない。

`record-recovery review-admission-metadata` は追加引数を受け付けない。
既存の固定保存先を64MiB上限で一読し、3649/localai-web-rsの一意な行だけを返す。
登録文字列、本文、題名、パス、受付key、他の行、認証情報は出力しない。
参照先・モデルサービス・quota口座・Git・scheduler実行物も開かない。

返す情報は、保存された状態、dispatchable、実行参照配列の長さ、停止と依存の有無、
行自身とschedulerのselection/selection_input/console_request.optionsに記録された
model・effort・origin、preparation_failureの固定理由、resource_waitの固定理由と期限、
not_beforeである。モデルと理由は固定の語彙に制限する。
欠落/null/型不正/語彙外を分け、origin欠落をpolicyへ補わない。
保存selectionや口座に由来しない期限だけから、実行モデル・枯渇・本人指定を断定しない。
producerの版は実際のビルド情報だけから返し、未取得ならunavailableとする。

構文破損、重複key、重複3649、所属違い、読取失敗では結果を返さない。
行や参照・状態・停止・依存・期限・モデル・所有・全体設定の変更はない。
固定出力の `launch_performed`、`fallback_authorized`、`execution_verified`、
`data_access_granted`、`live_state_changed` は常にfalse。
元2191の要求Hash・原レビュー・2174依存の出所は別の未完として保持する。

受入条件は、既存CLIの引数拒否、合成データの正例、unknown保持、全出力keyの固定、
秘密値の非出力、異常入力拒否、読取前の引数拒否を確認すること。
レビュー・同sourceのCI・通常マージ・生成物と実sourceの照合後にだけ実取得する。
この読取段階を未起動caseのfallback実装や3649起動の完了にはしない。
