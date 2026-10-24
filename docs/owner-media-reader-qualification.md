# 本人返信の添付記録を正規readerで読む

Issue [398](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/398)、ToDo3440。
既存の質問・返信の資格照合に、通常producerが作る型付きの添付観測と画像scopeを結ぶ。
原保護一覧を再作成せず、元root・本人・カード・明示停止の扱いを保持する。

## 入出力と受入条件

- 固定会話homeの `conversations/<request>/owner-media/` 内の
  `<message>-<observation-sha>.private.json` と
  `<message>-<observation-sha>-<attachment>.scope.private.json` だけを扱う。
  latest、note、expected、未知のファイル、ディレクトリ、リンクは資格にならない。
- 共通の質問・送信記録・元ToDo・元rootの照合を再利用する。canonical返信を既存関数で
  現在の受信状態と照合し、その生bytesのSHAを観測記録のsource SHAと比較する。
- 型付きの観測は元request/sender/ToDo/質問/本人/guild/thread/本文SHA/作成・編集時刻に
  一致する。観測の生bytesのSHAはファイル名と一致する。添付は既知の配列で、
  正の一意なIDを通常producerの順に並べ、sizeが既知の非負整数である。
- scopeは同じ観測のただ1件の添付IDとsize、同じ本人・本文版・元ToDo・threadに一致する。
  期間はproducerの作成時刻から1msの有限な区間そのものとし、終端は過去である。
  画像scopeのsizeは1〜20MiB。観測の添付0件や対象外sizeは画像読取成功にしない。
- 全証跡を上限付きで読み、重複キー・未知フィールド・誤った型・原本の欠落を拒否し、
  最後に同じ証跡のhashと物理パスを再確認する。原本を更新・コピーしない。
- 実qualification関数の合成fixtureで正例と本人/ToDo/世代/source SHA/観測hash/
  添付ID/size/期間/保存途中/保護対象/リンクの負例を確認する。
- 正規merge・既存CLI経路の配布後に実guardから正式history-attachmentへの入力読取を
  確認する。画像内容の実読・製品の実反映・質問closeはこの修復の成功へ含めない。

## 変更前の再現

実資格関数 `conversationAllowed` に同じproducer形式の観測とscopeを渡すテストを先に
実行し、既存の質問・返信は通るのに観測とscopeが拒否されることを保存する。
実ファイルのないパスの判定や、guardを置き換えたreaderだけを再現証拠にしない。
