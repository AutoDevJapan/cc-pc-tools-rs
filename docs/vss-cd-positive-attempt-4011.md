# 4011：C→D候補の撤回と読取診断の記録

C→Dの書込み候補は、2026-10-28のレビュー4139/4140を受けて撤回した。
現在のC→C関連付けを保持したまま移す、文書化された成功経路がないためである。
snapshotが0件になっても、この問題は解消しない。0件になるのを待つ工程も次の実行予定から外した。
OSの保存先は変更していない。現在のVSSデータを削除・縮小・無効化して切替を強制しない。
元の依頼全体は未完了であり、この候補の撤回を修復完了とは扱わない。

## 撤回の根拠

[AddDiffArea](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-adddiffarea)
のRemarksは、source volumeにsnapshotまたは関連付けがある場合の新しい関連付け作成を認めない。
同APIのclient supportはNone supportedである。
[ChangeDiffAreaMaximumSize](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-changediffareamaximumsize)
には、既存関連付けを正の値で置き換える保証がない。
[vssadmin resize](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/vssadmin-resize-shadowstorage)
は最大容量を変えるコマンドであり、snapshot消失の警告もある。
0件だった時刻にも残ったC→C/50GiBを、正のresizeで消せるとは仮定しない。
[MigrateDiffAreas](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nn-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt2)
は将来用の予約であり、代替手段にしない。

## 現在のsourceができること

既存のfolder名とCLI名は履歴を追えるように保持した。新しい製品や常駐CLIではない。
Goの引数は `--contender` と `--inspect-cd <preflight.ps1>` の読取りだけである。
`--attempt-cd` はCOM・子process・票へのアクセスより前にexit2で拒否する。
PowerShell runnerも `Mode Inspect` しか受け付けない。
resize起動、成功扱いのC→D判定、resizeの文章分類、native書込claim消費をsourceから除去した。
このため、旧S1のCP932出力、書込後phase、書込前排他、暗号化の比較、claimのテストは
現sourceの実行経路に存在しない。将来の移行を承認した意味にはしない。
移行案を再設計するならPITの有効状態・設定所有と、C/Dの暗号化を含む保持条件から別に確認する。

読取診断は、同じCOM threadの空集合を開始し、別processのStart拒否、匿名metadata、
取得後の別objectのStart拒否を記録して、自分が開始した集合だけAbort/Releaseする。
IN_PROGRESSは何らかの集合が進行中であることを示す。集合の持主の同一性を証明しない。
context0x1d、slot順序、EVENT_S_NOSUBSCRIBERSの分類は前回の確認を保持する。
子の総期限70秒・外側90秒、CREATE_NO_WINDOW、64KiBの回収上限は維持した。
実HEAD・clean source・固定hash・pin後のfresh buildと元票の追記専用も保持する。

## 実行済みの履歴

10/27 10:52の元probeはStart0→別object80042316→Abort00040202だった。
元checkerがAbortを未確認とした原票は書き換えず、別の可用性確認で正常受付を確認した。
11:19のread-only診断は別process80042316と匿名metadata取得を確認し、書込みは0だった。
22:02:43～46のrunner Inspectはsource69f18f43からfresh buildして起動・回収まで完了した。
3snapshot/2pointと既存割当があり、live_preflight_not_met_no_write/exit1で保持した。
別processと取得後の別objectは両方80042316、親は所有1/Abort完了1/Release1だった。
これらの取得済み票は再利用・上書き・削除・再試行しない。
positive-cd-request/result/native-startは未作成であり、書込モードは一度も実行していない。
過去sourceと原票はGit/private原位置へ保持した。現在sourceの読取を新しくOSで実行する予定もない。

## レビューと検査

4139は固定9bc09cadの19対象全文を読み、Must0/修正5件。
4140は同じ19対象全文を読み、重要3/中3/軽微2件を返した。両方実Opus5.5/maxの別session。
書込み経路の撤回により両方の実害を除き、残す読取部分と文書を同じ変更後headで再確認する。
旧版の合格を新しい版へ流用しない。PR568は修復の受入未完を示すdraftのまま。

現sourceのGo vet・build・関連testが成功した。17種類の変更・未確認metadataを拒否する例、
書込flagの拒否、contender状態、64KiB上限、総期限切れを確認した。
期限のテストは実在するcmd.exeを正常期限で成功させた対照と、同じ実行物を期限切れで
未起動にする比較を使う。存在しない実行物の検索失敗を「期限で未起動」の証拠にしない。
実際の旧flagの起動もexit2/読取専用usageで終えた。OSでのCOM診断の再試行はしていない。

[保持調査の現在値と次の工程](vss-retention-4011.md)に、夜間の結果、PIT、I/Oの有限測定を記録する。
