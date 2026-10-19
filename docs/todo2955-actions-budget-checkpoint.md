# ToDo 2955: GitHub Actions予算停止の確認

確認日時: 2026-10-19 17時台 JST

## 確認できた状態

- 実所有者 `AutoDevJapan` の private repository `GoDD-13a13e1` で、PR 575 の固定HEAD `ae070f21b6bdc639f21669403853b30d215d7c81` に対する正式check `117874503396` と workflow run `39194597297` を読み直した。annotation は「The job was not started because an Actions budget is preventing further use.」で、job は0 step、runner未割当のまま終了している。
- AutoDevJapan の GitHub 課金画面は Actions 予算を `$2,000 / $2,000` 使用済み、状態 `Usage blocked` と表示した。Actions分の含有利用枠も `3,000 / 3,000 min` で、次回リセットまで約13日と表示された。
- 組織runner画面にself-hosted runnerはなく、標準hosted runnerは0 active jobs。対象repositoryの正式runner APIも0台を返した。
- このPCでは `Runner.Listener` プロセスが1つ動いていたが、所有者・登録先・対象job・空き状況を照合できない。他担当のrunnerとして保持し、停止・登録変更・流用はしていない。
- 組織runner APIの追加閲覧には現OAuthにない `admin:org` 権限が要る。権限の追加は行っていない。課金画面とrunner画面はログイン済みChromeから読み取り確認した。
- ToDo 2757 は特定repoのマージ済みPRに対する有限なCI実行・配布作業で、組織の予算設定や複数製品共通の予算停止を変更する作業ではない。重複する設定変更・runner登録は見つからず、連携・変更もしていない。

## 今回の操作

- 追加のworkflow再実行、runner登録、workflow変更、budget変更は行っていない。開始前に同じ予算拒否が確定しているため、空振りの再実行はしなかった。
- GitHubの請求設定、支払方法、追加枠、Security契約は変更していない。Actionsの月間予算とSecurity継続判断は別件として扱う。
- 通常の検査をする既存runnerは見つからず、このToDoへの資源割当もない。そのため、今の課金周期中に既存の許可範囲だけで正式CIを再開する経路は確認できなかった。

## 再開手順

次の課金周期開始後に一度だけ予算・含有分の状態を再読する。停止が解けており、対象repoの実runnerまたは通常hosted経路が受け付け可能なら、対象製品の現行固定HEADを再確認して代表1件だけ通常CIを開始し、開始・実検査・結果を記録する。停止が続く場合は、追加費用を伴わない新しい正式経路が現れたかを一度確認してから、阻害要因を親2903と対象ToDoへ届ける。予算・契約・支払手段は変更しない。

関連確認:

- `https://github.com/AutoDevJapan/GoDD-13a13e1/actions/runs/39194597297`
- `https://github.com/AutoDevJapan/GoDD-13a13e1/pull/575`
- GitHub公式説明: [Actions課金](https://docs.github.com/en/billing/concepts/product-billing/github-actions)、[self-hosted runner API](https://docs.github.com/en/rest/actions/self-hosted-runners?apiVersion=2026-03-10)

この記録は予算停止の確認であり、CI成功・runner利用可能・製品全体の再開を示すものではない。
