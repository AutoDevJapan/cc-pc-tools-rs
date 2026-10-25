# 未起動3900のOAuth再評価

元3902。対象は既存jobs/schedulerの通常受付だけ。SDK実装3894、3649の固定特例、
3669の実行後復旧、原registry、認証、runnerを変更しない。

3900の現在仕様はSHA256 `e8b9f72b283c1add07eb93e0b554fd8d6b3598bec4e4ef7fa5cd64d1d28c418e`。
親は3894/job20261026-064014-todo-3894、実作者はnative_turn_contextでSol/max。
固定公開差分のheadはb635432cffbefe979bfe7bbb9c62bd071deb51bb。レビュー内容の読取は3900が行う。

## 入出力と受入条件

`scheduler reevaluate-unstarted-review --id 3900` は正常な現在行、仕様、作者の実行観測、
Git所有・予約、実worker不在、fresh OAuthを自分で取得し、適格性と全行SHAだけを返す。
`--apply --expected-state-sha256 SHA` は同じ入力のCASで選択票を保存する。起動しない。
IDの流用、任意JSON票、quota期限の強制上書き入口は作らない。

現在のmodel/effort originがpolicy、jobs空、reviewの元関係・scope/specが一致し、
停止・依存がなく、既存quota専用期限だけで待つ場合に限る。古い・未知のoriginは
policyへ補わず、本人指定は保持する。元のmodel/effort、履歴、元期限と理由は保存する。

通常OAuth一覧でAPI-keyが0と確認してからquotaを更新する。Anthropicの現在の枯渇と、
具体的な公開namespace自身に対応するOpenAIの健康な既存codex OAuth・追加課金無効を
照合する。未確認・古い・未来のhealth、全枠枯渇、API-keyだけ、namespace不明は拒否する。
モデルは元policy指定Luna/maxを、全作者と異なる場合だけ選ぶ。

通常prepare、jobs登録、最終queue→state→todo admissionで同じ仕様・元行の入力・世代・
停止依存・元作者・scope・現在checkout・公開namespaceを再照合する。通常の既定選択で
枯渇したOpusへ戻さない。queued jobのhealth失敗には既存の有限再確認を使う。
新しいjobの二重作成を拒否し、worker受領による通常の世代更新だけを同job票で許す。

## 検証と配布

意味のあるRed→Green、型/CLI異常、legacy hint/本人指定、作者除外、health境界、
spec/世代/停止/依存変更と重複受付をテストする。変更クレートと利用側のnextest/doc/
Clippyを専用target/共有gate/sccacheで実行する。別実モデルmaxと同head正式CIの後に
通常mergeする。配布は正規のsource/current hash/ACL/並行producerのCASに従う。
source候補、現用更新、3900実nativeのmodel/effort/spec/scope一致を別々に記録する。
SDK reviewの原応答は親3894が受領する。この担当はその読了を代行しない。
