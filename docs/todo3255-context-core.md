# 3255の実行context抽出の先行実装

起点は元3255。元Issue446/PR448とPR486の受入条件は変更しない。
今回は実データの読み口を接続しない内部ロジックの準備で、GitHubへの新しい
Issue/PR登録、共有ToDo、worker、レビューの起動は行わない。
別差分の正式レビュー・CI・通常mergeが済んだ実装としては扱わない。

## 入出力と境界

入力は将来の正規producerから渡される、範囲を限定済みのUTF-8 JSONLメタデータ。
内部関数はio.Readerだけを受け、ファイル・ネットワーク・認証情報を開かない。
CLI入口、任意path引数、原store/nativeのreader、guard分岐は追加しない。

呼出側が別途結ぶnative thread ID、native turn ID、開始・終了時刻に対して、
session_metaのidとturn_contextのturn_idを完全一致で照合する。
session_idやroot_turn_id、開始前context、別turnの値で補完しない。
入力全体を上限1MiB、1行64KiB、JSON深さ64以内に限定し、重複keyを拒否する。
読取失敗・途切れた末尾・破損・曖昧な複数contextでは観測値を一切返さない。

唯一の対象contextを読み終えた場合だけ、modelとeffortを別々に抽出する。
同caseの既知model IDであるgpt-6.1-solとgpt-6-lunaをそのまま返す。
要求modelとの一致で観測を捨てたり、aliasを変換したりしない。
不明なmodel値を出力せず、欠落/null/不正/未認識を区別する。
effortも実際の欄だけを使い、欠落時にmaxを補わない。providerは推定しない。

出力は限定した観測項目だけで、本文、path、native識別子、認証値を含めない。
source_qualified、execution_verified、access_grantedは常にfalse。
native IDと時刻の入力整合を、元assignment/応答との同一性や閲覧資格へ昇格させない。

## 合成データでの受入条件

- Given: 同じnative thread/turnと時刻内の唯一のcontext。
  When: 観測modelが要求側と異なり、観測effortがlow。
  Then: 実欄のmodelとlowを返し、要求値・設定値へ置き換えない。
- Given: session_id/root_turn_idだけが一致、別turn、開始前/終了後、または欠落欄。
  Then: 対象観測として採用せず、欄の欠落を別値で補わない。
- Given: 途中読取エラー、破損、重複key、複数の対象context、上限超過。
  Then: 途中まで見えた値も返さずunavailableとする。
- Given: 本文や私的値に似せた合成の未知field。
  Then: 出力の固定key集合に追加されず、資格・完了を主張しない。

## 形式の根拠と残件

既存cc-jobsのpoll_session/observe_turn_contextと、公式
[Codex rust-v0.163.0 TurnContextItem](https://github.com/openai/codex/blob/rust-v0.163.0/codex-rs/protocol/src/protocol.rs#L3417-L3457)
を参照した。turn_idはoptionalなので、無い記録を時刻だけで一致させない。
[SessionMeta](https://github.com/openai/codex/blob/rust-v0.163.0/codex-rs/protocol/src/protocol.rs#L3245-L3254)
のidと、root threadを示すsession_idも分ける。
この公開形式を、PC内の版一致や元実行の証明へ使わない。

正規producerの所有/source/配布対応、元assignment/応答とnative IDの正式な対応、
権限を維持する限定reader、別モデルの実行証明、レビュー、CI、通常merge、配布、
実データ受入は未完。この内部関数だけでは歴史的4559の取得を完了しない。
PR486の固定f727/正式13success、元PR448のf340/正式6success/読了M0、
実四件比較0・固定二件snapshot0と、原仕様・本人回答・未知依存を保持する。
