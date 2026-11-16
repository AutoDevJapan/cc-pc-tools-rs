# 通常Logの未送信target訂正と同request照合

通常の recovery-log では request_id、本文・題名・destination・notify・画像等を固定する。
誤った explicit thread により拒否された後も、先に保存したIntentは消さない。

## 同requestの読取

新版の同じ recovery-log に --check-only を付けると、proof-file無しの通常Logに限り
保存済みIntentの種別・旧thread・入力hash・alias・durable effect件数・訂正候補性を返す。
既存のledger/lockを要求し、通常OSロックを保持して読み、journalへの追記、HTTP、通知、起動をしない。
本文・token/configや全履歴を出力しない。inspection_only は配送や将来送信の資格ではない。
ライブthreadの確認は normal send 時にもう一度行う。
clarification/acknowledgement/owner-postのpurpose-bound checkは従来のproofを保持する。
旧版で通常check-onlyがsyntax2だった事実を、新版の成功へ書き換えない。

## 限定訂正

同じ通常senderは、同RIDに存在するsilent/text-only/explicit Logのうち、旧targetに通常の独立receiptがなく、
新targetに別の正規ordinary Logの verified log:0 receiptがある場合だけ訂正候補とする。
違いはthreadだけ。body/title/destination/notify/media/program/native/deadline/RIDの変更を許さない。
canonical/alias関係、他RIDとのsemantic重複、いずれかのdurable Pending/Posted/Verified/完了がある場合は拒否する。
GETで投稿が見つからないだけでPendingを解除しない。
Question/paired progress/owner-read receiptをordinary Logの資格へ流用せず、架空のquestion originsを作らない。

元Botとdestinationを既存normal APIのGETで照合してから、専用LogTargetCorrectedを既存journalへ原子的に追記する。
元Intentはそのまま保存し、同じRIDの後続reserveが訂正済みfingerprintを使う。
通常のPending-before-POST、quiet flags、同requestのGET readbackとretry/dup抑止は維持する。
別送信API/新RID/allowlist書換/手作業でのledger変更を復旧手段にしない。

## 配備と未達

本候補は新しい追加eventを読むsenderを必要とする。旧readerは未知eventを空のledgerへ変換してはならない。
通常の関連Rustテスト・型・Clippy/fmtと利用側確認、既存二者review/必要CI、
正常な本機installed source/配布tuple照合後に使う。private候補から本番journalへ追記しない。
installed binary hashだけでsource版一致を推定しない。

個別のrequest/元入力/配送先は非公開の運用caseへ保持する。
本番pending/配送をsource上の再現条件やprepared fixtureから成功・不在へ昇格しない。

保存済みclarification/acknowledgement/owner-post purpose journalに同RIDがあれば、proof省略時も通常target訂正を拒否する。
read-onlyのpurpose照合は既存のvalidated loadだけを使い、bindingを作成・削除・書換しない。
