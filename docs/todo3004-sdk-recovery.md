# ToDo3004: PassShare検査SDKの回収

元2997・親2994。cmd/runnerと実runner/service/profileは変更しない。
所有確認済み3rootの回収だけ。未使用の保護対象は保持中。

## 実装と検査（2026-10-19）

safe-deleteに固定所有記録の照合付きsdk-links退避を追加した。
plan/一般quarantineのリンク・Git・外部hardlink保護は変更しない。
13相対symlinkに加え、実物2rootでWindows生成の内部絶対junctionを発見した。
ジャンクションは走査せず、raw reparse SHA256と元の文字列を保持する。
退避後の絶対参照は元rootを指して切れるが、実体は同時に保存して照合する。
GoのEvalSymlinksではjunctionの最終実体を確認できなかったため、OSの
GetFinalPathNameByHandleWで移動前の最終パスを確認する。

Go race/vet/buildと合成fixtureで、相対symlink、内部junction、外部参照拒否、
変更拒否、一般planの拒否、有限receiptの拒否を確認した。
実物のinspect-sdk（読み取りだけ）も3root成功。service停止/PID0を別途確認済み。

| 所有ID | inventory（root除く） | 通常ファイルbytes |
| --- | ---: | ---: |
| 7155bdd37afc8e75e4dbf39509a5f18c | 7873 | 935313295 |
| c0d0c333fca8d97bab2fb8219b1ee1a8 | 7884 | 938846684 |
| 5eac22dc8041f9b3a471dbb392f86b99 | 7878 | 938945621 |

合計2,813,105,600 bytes。これは移動前の内容量であり、削除量や空き増加ではない。

## 再開と残作業

同じscheduler/todo-3004-passshare-sdk-2997ブランチで継続する。
検査用生成物はsafe-delete所有ID141fa64610dd2fafc76bbb1cd77e07b2。
この中のreceipts-goは読み取り検査用の原所有記録のコピーであり、正式記録を
更新していない。実回収時は配備済み道具の正規receipts-goを使う。

残作業: 別モデルレビュー、CI、PRマージ、並行変更照合後の配備、3回の正規退避、
元path不在/保存先全inventory/正式所有記録の確認、親2994/元2997へnote配送。
検査用生成物はplanの返却コマンドを別呼出に提出してfinishする。
通常の完了通知は行わない。失敗は正確な段階と残りを保存し、無限再試行しない。
