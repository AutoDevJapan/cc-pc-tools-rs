# ToDo4088の再開情報

## 2026-10-27 20時の再開

現担当job `20261027-195622-todo-4088`、ブランチは同じ。
4091は18:59に正常終了し、実モデルOpus5.5/maxをnative_turn_contextのmetadataで照合。
Must5件ほかの原結果を正規のジョブ結果受領で取得した。
[受領と修正対応](evidence/owner-rule-4088-review-corrections.md)に全件の対応を残す。
mainの台帳stream射影と有限retryを取り込んだ。
20時の修正コードは`81d7da7f`と、その後の一時所有のtyped分類の追加。

容量監査のrace/vetは成功。guard全件raceは81d7da7fで389.824秒で成功。
その後のOwner関連差分も33.533秒でrace成功、vet終了0。合成fixtureと実物メタデータも確認済み。
Rustの画像添付互換性テストはCargo割当の子4125へ固定81d7da7fとして登録した。
cc-askのコードはその後変更していない。

最新方針により、修正後の同じ固定コミットについて別モデルmaxのセルフレビューと
独立したラバーダックを受け、すべての修正指摘を解消する。
再レビュー4126（Opus5.5/max）、ラバーダック4127（Sonnet5.5/max）を通常登録済み。
両者はまだ未着手のため、最終候補の実読と実モデルは未確認。
同じブランチのCIを成功させて通常マージし、最新マージソースから配布する。
senderの画像互換性を先に配布し、その後guard・容量監査を配布する。
senderの実ビルドにもCargo割当を使い、元担当は直接Cargoを起動しない。
実配布は未実施。2467の反映記録と元質問のcloseも未実施。

今回の所有付き検証フォルダーID: `176cb5e621bca702f5fe23dfd3def46c`（作業先直下）。
Goのテスト出力、Cargo検証の依頼を保持する。前のCの一時フォルダーの一般分類は未確認のため、
旧入力一覧・スクリプトを別経路で読まない。配布時はこの依頼の正規メタデータから入力を再確認する。
新しいDの所有フォルダーへ実物の対象メタデータを再取得した。
`live-targets.private.json`は対象対応、`reviewed-live-result.private.json`は匿名結果。
通常CLIの最終候補では6対象すべてclearだが、配布後に再確認する。
登録の旧boolean specは参照にせず読み飛ばし、projectsと対象入力metadataの1回の射影を追加した。

配布準備: `distribution-before.private.json`に現在のsender/容量監査/guardのSHAとACLを保存。
現用guardは444eae87、SHA `c80d60ab470e52ef9fbe53c1e039d1fcdc31429e291c3f156a953b9b6059d02f`。
この版は取り込んだmainの祖先で、旧81700412の版に戻さない。
新しい`install-reviewed.ps1`はsender→容量監査→guardの順に、旧版保持・atomic replaceと
SHA/所有者・グループ・DACLを確認する。SACLは未検証と記録する。まだ実行していない。
スクリプトSHAは`1e82d55f483157ebc49966c656a8898981bc1201b3e1599f5cfa494f6e4dd031`。
入力`distribution-builds.private.json`はmerge commit、review_and_ci_verified、artifacts配列。
各artifactはname(sender/capacity/guard)、path（この所有フォルダー内のexe）、sha256、source_commit。
review/CIを実確認してからtrueを書く。最新マージ版のビルド以外を配らない。
`.godd/`は現在の起動処理が作った未追跡物として保持し、コミットや清掃に含めない。

以下は11時の記録として保持する。

依頼: 元2467の本人回答による受託分類をrepair-guardへ実装し、通常配布する。
開始時base: `817004120a458b3a37830478512da0aa8040c9d0`。
作業ブランチ: `scheduler/todo-4088-adj-bm1314-misc`。変更していない。

完了: 起動時の方針・対象AGENTS・配布3258・実装を確認。
実GitHub所有者AutoDevJapan、repository ID1406818713をOAuthの通常CLIで確認。
本人回答と元質問は元2467に保存済みであり再質問しない。

完了: Issue591を作成し、合成fixtureでRedを確認して`ca9c791b`へ保存。
新規の受入テストと既存の方針・更新テストはGreen。
指定3フォルダー、運用基盤、重なる受託登録、実repository ID、所有付き一時フォルダーの
分類を実装し、カード候補の格納先の確認も修正した。
race全件（453.602秒）・最終Owner関連race（28.440秒）・vet・カタログ一致は成功。
コード確認点は`ffd8ddc46431fe76d7a70dde7040a09ee1e2c858`。
詳細は[evidenceのテスト記録](evidence/owner-rule-4088-test-results.md)。
PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/592 。
別モデルmaxのレビュー4091を、親4088のsubworkとして通常受付済み。
指定モデルはclaude-opus-5-5、effort max。まだ起動されていないため、実モデルは未照合。
本人への質問・PC操作・新費用は不要。

実物5対象を一時Go overlayから本物の通常CLI入口で分類し、候補版でclearを確認。
実際の配置済みguardを更新した結果ではない。元の実物の内容は開いていない。
ShortsとOMB関連の各1つの実行先は元から不存在で、場所の分類だけを確認した。

配布前のguardのSHA-256:
`94c08be91776df277a6a3442e71f291103345641558657e1872e36520ad924e0`。
実物のGo build情報は開始点`817004120a458b3a37830478512da0aa8040c9d0`、modified=false。
配置後にこの版が変わっていたら、並行配布のソースとの包含を確認してから基準を採り直す。
所有者・グループ・DACLの基準を私的記録へ保持済み。SACLは未検証。

次: 4091の最終応答・実モデルのturn_context・固定base/headと実読範囲を照合し、Mustを修正。
CI成功と最新headを確認して通常マージする。別モデルレビュー未受領のままマージしない。
作業ブランチは維持して、cleanであればマージ済み最新mainをffし、最新ソースから配布物を作る。
必要な生成カタログとGoのリポジトリ内依存に並行変更があれば、その影響範囲を確認する。

検証フォルダーに`install-4088.ps1`を準備済み（未実行）。
`build.private.json`に実ビルドのcommitとsha256を書き、同フォルダーの`guard-4088.exe`を入力とする。
同スクリプトは旧版SHAとACLの一致、同じ配置先でのatomic replace、旧版保持、SHAとACLの読返しを行う。
既存の管理者窓口で実行する。出力は匿名の結果だけであり、SACLは未検証と明記する。
準備スクリプトのSHA-256は`67a972aeb08d1db8334854b866a909ed90cad12fd45165491c675cf9262fc207`。

配布後は`live-targets.private.json`にある具体的な場所を、配置したguardの通常CLIで判定する。
入力を出力せず、元ToDo・場所の指紋・用途・存在・分類・根拠だけを残す。
元2467へ配布SHAと変更点、3327/3489/2655の結果を返し、2655にも同じ対象の結果を返す。
2655の修正の配布・切替受入は2655側、利用枠待ち4件の解消は本4088の範囲外。
2467の回答反映ファイルの保存や質問closeを本4088が代行しない。
資源割当なし。Cargo・GPU・Civ6は使わない。APIキーを使わない。

所有付き検証フォルダーID: `6391f28490a15a27cac73972b0ac2815`。
safe-delete createで作成済み。検証後はSkillに従いplan・通常削除・finishする。
検証フォルダーには原テスト結果、匿名結果、入力対応、未実行の配置スクリプトがあるため、
配布完了まで保持する。待機のために原証拠を削除しない。
原一覧・秘密・受託の原データは開かず、原資料を保持する。
共有ToDoの本人項目の状態はスケジューラーが更新する。
