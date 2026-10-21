# 初回再供給の終了記録を照合する

PR149の元Rust job（39423559387/1/118630300541）の再供給は、監査済みqueueが
`prestart-1`へ記録を保存した。回収は成功したが退避が完了せず、GitHub jobは未開始である。
既存のruntime資格はこのqueue自身が作る延期用ディレクトリ名を認識していないため、
内容を読まず、固定した1世代の診断用の名前だけを通常の資格判定へ追加する。

- 対象は既存`D:/CC/Tools/civ6-ci-2962`の独立NTFS配備記録に束縛された出力だけ。
  対象keyと`prestart-1`はコードで固定する。他job、次の延期、全Tempを許可しない。
- queue自身のprepare/execute/collect等の構造化記録と、コピー済みのlauncher報告、
  service/drain/SDK-ready/キャッシュ起動記録だけを扱う。SDKの設定dump、runnerの診断、
  credentials、任意の拡張子・深いコピーは拒否する。
- 名前だけでは既存fileを採用しない。親は属性専用handleで実ID・作成時刻・owner・
  size/write/change・単一link・no-reparseを独立保存し、正規guardを再実行してから読む。
  既存のsource/実所有者/deny/祖先/外部receiptの検査は変更しない。
- 新しいTemp資源自体の本文・credentialの資格や、削除の許可は付けない。
  原資料、退避済み4378、失敗した再供給資源を保持する。

Redは、正しく記録した通常ログが既存判定で拒否されることを確認した。
Greenでは同じfileの許可と変更後拒否、未知file・別key・次世代・credential拒否を確かめる。
関連Go race/vet、別モデル、同HEADの必要正式CI、通常mergeと現用guardへの反映後に、
実出力の原因診断を行う。名前追加・候補判定だけを原因特定や製品検査成功としない。
