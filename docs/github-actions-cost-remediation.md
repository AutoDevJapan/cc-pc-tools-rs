# GitHub Actionsの重複実行を抑える（ToDo2501）

## 2026-10-15の確認と追加是正

PR145は2026-10-13にマージ済み。PCへの移行も、別担当のPR147・161・165で完了した。
2026-10-15のAPI確認で、WindowsとLinuxのrepo専用runnerがonlineかつidleだった。
最新mainのCodeQL4言語はLinux runner27で成功し、直近の全workspace検査・doctest・
ClippyはWindows runner28で成功した。親PR144もマージ済みである。
sourceの重複検査を追加で起動せず、固定SHAと実行機ID・各工程の成功を照合した。

移行後に追加されたCodeQL・Product CI helper・Agent progress tools・PC runner acceptanceには、
手動rerunを初回PRと同じグループへ入れる設定が残っていた。この4件を既存9件と同じ規則へそろえる。
初回PRだけをPR番号でまとめ、rerunとPR以外はrun IDへ分ける。旧定義とのグループも分離する。
新しいPR検査を追加するときはBuild speedの実行制御を参照する。
fork除外、実行前のsource検証、検査コマンド、OS、CodeQLの言語と権限は維持する。
追加是正のレビュー・固定HEADのCI・マージは、この段落を作成した時点では未完了。

2026-10-15 04:25 UTCの組織利用額はActions net 2,000 USD、gross 2,043.5115 USD。
10月14日・15日のnet増分は0 USDだった。Actions予算は2,000 USD・上限後の追加利用停止で、
今回の担当は変更していない。この停止には予算上限の効果が含まれ、PR145だけの因果効果や
確定請求額の減少とは扱わない。自社sourceと確認できない資源は匿名の会計metadataだけを扱う。
OS再起動・ログオン・週次の実時刻でのrunner復帰は、この監査では未検証である。

以下は2026-10-13時点の記録である。

## 完了した工程と残る工程

2026-10-13、AutoDevJapanの既存OAuthで10月の組織利用額を取得した。
確定請求ではなく、取得時点の現利用額である。元資料は変更していない。
顧客保護の原本が未復旧なので、確認済みの自社ソース以外は会計情報だけを匿名集計した。
顧客名・未分類のリポジトリ名・認証値はこの記録に含めない。

- Actions総額: 2,040.8897 USD、割引後: 1,998.6982 USD。
- Linux: 208,035分、Windows: 65,484分、macOS: 2,176分。
- 自社の`AutoDevJapan/cc-pc-tools-rs`: 割引後139.7184 USD、17,528分。
- 上位3件の未分類資源: 割引後の合計1,242.9006 USD。ソース変更・実行停止は未実施。
- Code SecurityとSecret Protectionの利用額合計: 53.8065 USD。契約は変更していない。

このworktreeで8件の検査ワークフローへPR単位の実行制御を追加した。
同じPRで新しいコミットが来たら古い実行を取り消す。
workflow名で分けるため、異なる検査は互いに取り消さない。
PRの初回実行だけをPR単位でまとめる。再実行とPR以外はrun IDで分ける。
古いheadの手動再実行が最新headの初回検査を取り消さないようにする。
実行グループに世代を付け、修正前のworkflowを使う古い再実行からも分離する。
mainや手動実行の必要な検査も維持する。
検査コマンド、実行環境、パス条件、権限、成果物の保持は変えていない。
親のPR144のソースと現在の必須検査には変更を加えていない。

原因の具体例: PR144では旧コミットのBuild speedが04:01:36、04:10:33、04:16:28、
04:26:42 UTCに起動し、最初の3件も新しいコミットの公開後に検査を続けた。
最新コミットの実行38346463064は2075成功・5失敗・5省略で終了した。
失敗5件はcleanup-8hのWindowsテストで、親担当が修正する必要がある。
同じ失敗へ無条件に全件再実行を重ねない。

別PR145を作成した。8件のYAMLをactionlint 1.7.12で確認した。
Claudeの実レビューで手動再実行が最新headを取り消す恐れを確認し、8件とも修正した。
固定HEADのCI成功、修正版の別モデル確認、マージ、設定反映後の観測はまだ完了していない。
PCの検査用runnerはリポジトリAPI上0件。組織runnerの照会は既存OAuth権限で403。
新OAuth権限や契約は追加せず、runner移行は資源割当と隔離の確認後に進める。
この担当にはCargo・GPUの割当がないため、PCで重いビルドは起動していない。
読むだけの確認2503でrepo adminとrunner不在を実確認した。
本体の移行実装はCargoが割り当てられる共有ToDo2505へ条件を保持して分けた。
別モデル確認はToDo2502で、親が提供した有限の差分だけをClaude OAuthへ送る。
未分類のworktreeや保護設定を変更してレビューの閲覧範囲を広げていない。

## 再開手順

1. `D:/CC/AGENTS.md`とこの記録を読み、親PR144の状態を確認する。
2. 必要な旧PR実行だけを、現在のPR headと照合して標準のcancel経路で停止する。
   最新head・main・保護対象の検査は取り消さない。
3. この8ファイルだけの変更を別PRにする。YAMLと式をactionlintで検査する。
4. 別モデルで検査を維持していることをレビューし、Mustを直す。CI成功後にマージする。
5. 親が追加する検査ワークフローには、親の修復と衝突しない時点で同じ設定を追加する。
6. 取得時刻の異なる組織利用額と実行記録で増加率を測る。
   設定変更・実行停止・検査成功と、実請求額の減少を混同しない。

匿名の会計集計と実行観測の原記録は、依頼指定の
`D:/CC/Work/billing-audit-20261013/github-cost-remediation.json`に保持する。
同じ場所の`github-cost-handoff.md`で未完了を引き継ぐ。
会計APIにはworkflow単位の金額がないため、実行の経過時間を金額や課金分数へ換算しない。

参考: [GitHubの組織利用額API](https://docs.github.com/en/rest/billing/usage)、
[実行制御の仕様](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)。
