# 撤回した保存先切替と読取り専用診断

既存C→Cの関連付けを保持したまま、正のresizeでC→Dに置換する文書化された成功経路はない。
snapshotが0件でもこの問題は解消しないため、候補と0件待ちを撤回した。
保存先は変更せず、既存snapshotを削除・縮小・無効化して切替を強制しない。

[AddDiffArea](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-adddiffarea)
は既存snapshotまたは関連付けがあるsourceへの追加を認めず、client supportもNone supportedである。
[ChangeDiffAreaMaximumSize](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nf-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt-changediffareamaximumsize)
には正の値で既存関連付けを置き換える保証がない。
[MigrateDiffAreas](https://learn.microsoft.com/en-us/windows/win32/api/vsmgmt/nn-vsmgmt-ivssdifferentialsoftwaresnapshotmgmt2)
は将来用の予約であり、代替手段にしない。

## 残した診断

`tools/vss-4011/cd-attempt` は履歴を追うための既存名で、保存先変更はできない。
Goは `--contender` と `--inspect-cd <preflight.ps1>` だけを受け付ける。
旧 `--attempt-cd` はCOM・子process・票へアクセスする前にexit2、PowerShellのMode Attemptも起動前に拒否する。
resize起動、書込後の分類、CLI文章の復号、書込claim消費はsourceから除去した。

Inspectは空集合を開始し、別processのStart拒否、匿名metadata、取得後の別objectのStart拒否を記録する。
最後に自分が作成した空集合だけAbort/Releaseする。
IN_PROGRESSは進行中集合の存在を示すだけで、持主の同一性を証明しない。
総期限70秒・外側90秒、CREATE_NO_WINDOW、64KiBの出力上限を維持する。

実HEAD・clean source・固定hash・pin後のfresh buildを照合し、実行票は追記専用とする。
取得済みの固定票は保持し、同じ診断を再実行しない。
11:19の読取りでは別processの80042316を、22:02では別processと取得後の80042316を記録した。
所有1・Abort完了1・Release1も確認した。後者は3snapshot/2pointを検出し、書込なしで終了した。
保存先変更の票は未作成であり、書込モードを一度も実行していない。

## テストと条件

Go vet・build・関連テストが成功した。17種類の変更・未確認metadataを拒否する例、
旧引数の拒否、contender状態、出力上限、実在exeの正常対照と期限切れ未起動を確認した。
PowerShell 5.1でも構文と撤回済みModeの拒否を確認した。

将来、移行を再設計する場合は、PITの有効状態・設定所有とC/Dの暗号化を含む保持条件から再確認する。
現在のsnapshotや設定を変える許可として、この診断を使わない。
[保持修復の現状](vss-retention-4011.md)は未完了である。
