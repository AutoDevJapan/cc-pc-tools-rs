# ToDo4084 再開記録

元の質問と現在の実担当を正規に結ぶ有限な修復を担当する。
既存3751は別ケースのwriterで、共有送信部品の更新前に同じmainと現用版を照合する。
現在3751は3651待ちで、実ジョブは正常終了している。過去の依頼範囲は広げない。

- 開始HEAD: 817004120a458b3a37830478512da0aa8040c9d0。
- 作業branch: scheduler/todo-4084-localaiconsole-3264-3350。
- 実owner/guard: 通常remoteとAPI、正規guardで確認。
- 正規received-input: 現在の担当と今回の修復依頼の入力・仕様版・実ジョブを照合済み。
- 原trackerには過去job/委譲edgeがない。現在の受付記録を過去へ遡及しない。
- 新図の通常guardは、正規ソースidentity更新後にclear。旧図の原保護には触れていない。
- 合成Redを確認後、現在の受付だけを検証するcontrollerを実装。変更/停止/別scopeの拒否、同一再試行、画像SHAの読返し、正規ToDo CLIでの元trackerと実担当への回答全文保存が成功。
- 正規received-inputと実リポジトリを使う送信なしの受入が成功。通常の再読取で一時的な未確認から復帰した。画像・質問本文をこの受入では開かず、ToDoを書き換えず、送信していない。
- Rustの会話関連47件と、ロック内の現actor/旧actorの確認が成功。
- PR564は同HEADの全CI成功と実別モデルレビューMust0を確認したがmainとの競合でマージ不可。既存writerは待機中のため、そのheadをこのbranchへ統合。pending reconciliationと回復済みintakeの制約を保持し、共有部品を巻き戻さない。
- 残り: 統合後の関連テスト、別モデルレビュー、CI、merge、配布、正規askの配送確認。
- 一時領域所有ID: d8623961f7c8e84ae628c83bbddf6f6a。
- 私的な入力・実結果は公開repoへ加えない。
- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/588。

## 別モデル4089の受領と再試行の修正

固定e87f4208の19ファイルをFable/maxが読了、Must0。原応答と実jobを私的領域に保持。
Suggestionのうち、予約中の正常再開と、正常なAPI再照合後の補助登録hash変化による
拒否を2件のRedで再現した。予約証拠をanchorと現在の資格に分けて修正し、Greenを確認。
元trackerのwaitとprivateリポジトリは元依頼の条件として保持し、仕様にも記載した。
両修正の関連テストと差分レビュー、最終CI/配布を続ける。質問・画像はまだ送っていない。
