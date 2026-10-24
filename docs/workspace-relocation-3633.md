# 共有していた作業場所から元の担当を再開する

ToDo3633 / [Issue441](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/441)。
元3331のPR388は正常終了したが、その後に元3327が同じ読取範囲の不足を
継続仕様として保存した。旧実装を再実行せず、継続仕様と過去成果を保持する。
月間全体の監査を3331の完了条件へ変更しない。

## 入出力と照合

`scheduler relocate-workspace --request-file <JSON>` は無変更の資格確認。
同じ要求に `--apply --expected-state-sha256 <確認結果>` を付けた場合だけ、
通常のToDoトランザクションで元番号の保存先を移す。起動・Git変更・通知はしない。

対象は既存のcc-pc-tools-rsの通常作業。要求は元番号、正常終了した元job、
共有元の現在の担当jobと実branch/HEAD、既存の専用worktreeと実branch/HEAD、
正規guardが読取可能とした継続仕様の参照元とSHA256を指定する。
原登録rootのguard、Gitの実repository、共通管理先、物理worktreeを照合する。
保存済み旧branchは元jobの受付時work_ownerと一致し、共有元の現在のbranchは
現在の別担当の受付時所有記録と通常ToDoの割当の両方に一致する必要がある。
単に他番号のbranchを許可する処理へ変更しない。

JSONの必須項目は `id`、`source_job`、`shared_owner_job`、`source_branch`、
`source_head`、`worktree`、`branch`、`head`、`spec_reference`、`spec_sha256`。
未知の項目は拒否する。確認結果は `eligible`、`saved`、`already_recorded`、
`state_sha256`、`spec_sha256` と固定の失敗理由だけを返す。
旧specの本文・原行・予約鍵・他担当の本文を出力しない。

保存specは選択した元ToDoの正規specs内の一件だけを用途付きで読み、
guard済み参照元の原byteとSHAが一致し、元番号と専用worktree/branchの指定が
含まれることを照合する。一般guardの例外・旧台帳復元・別コピーは作らない。
specの内容をstdoutへ出さない。不一致は原資料を保持して拒否する。

停止、未完依存、別project/owner、未知の来歴、同じ元番号の実行中・不明worker、
専用先の競合workerは拒否する。共有元の確認済み別担当にはGitの読取だけを行う。
元の全行、project登録、job metadata、spec byte、両場所の実branch/HEADと
専用先の作業状態を確認結果のSHAへ結ぶ。共有元はbranch/HEADだけを読み、
親のファイル保存やindex更新と競合しない。Gitには `--no-optional-locks` を指定する。
遅いguardはqueue lockの前で実行し、指示byteを保持するhandleと同じ物理pathを
維持して、通常queueの排他中にjob・行・指示・Gitを再照合する。
ToDoロック内で元行とprojectが同版の場合だけ保存する。

変更はschedulerの作業場所・branch・未起動予約の退役と移行receiptに限る。
仕様・note履歴・過去job結果・本人回答・資源・停止・依存・途中成果は保持する。
繰返しは同じ保存状態を再照合して無変更。次の通常prepareはこのreceiptを
再検証して専用場所を選び、元jobが欠けても旧共有元へ戻さない。
通常の所有権照合も保存済み専用先を使い、旧共有元の親への偽の依存を作らない。
照合不能のreceiptは通常schedulerの拒否条件に残し、別の作業場所を推定しない。
通常の容量・優先度・資源・
単一所有の判定は維持する。元番号の一意な実起動と最初の着手は別に確認する。

未起動の間に正規の継続spec又は専用先HEADが更新された場合は、同じ元job・同じ
専用先とbranchのまま、新しい確認とSHA付き保存でreceiptを更新できる。
旧receiptは `workspace_relocation_history` に保持する。既に次の担当が受け付けられた場合、
担当履歴や割当の来歴が不明な場合は更新しない。Consoleの未起動本文と結び付いた
予約も移行せず、本文を落とさない。

正常終了した旧担当が復元元に選ばれたpromptでは、その正常成果を保持することと、
元番号の `todo.exe history` を読み、正常終了後に追加された未完了も引き継ぐことを
明示する。履歴は新しい仕様へ書き換えない。移行後の別担当が実際に失敗した場合は、
通常の停止復旧指示を維持し、その失敗を正常終了と呼ばない。

## テストと実受入

実Gitと合成ToDo/job/processで、共有元を保持した移行、通常prepareの専用先選択、
仕様・履歴・未コミット成果の保持と繰返し無変更を確認する。
停止・依存・原仕様不一致・旧担当不一致・不明worker・別repository・
branch/HEAD差・確認後のnote/資源/停止/所有/project/spec変更を拒否する。
関連cc-schedulerテストとdoctest、別モデルMaxレビュー、同sourceのCI、
通常merge、並行更新を保持した現用配布後に元3331の正規再開を受け入れる。

## 再開記録

2026-10-24: 旧3331の正常jobとPR388のmergeを通常CLI/APIで照合した。
正常履歴には後続の継続仕様が残っている。旧保存spec四件の一般guardは
分類未確認で本文を開いていない。元3327の正規継続参照元はguard clearを確認し
読了した。実3331専用worktreeとbranchは残り、HEADはPR388のmergeである。
親3110の実行中worktree/branchは変更していない。次はRed/Greenと資格付き移行。

Redは5265b98dの受入テストで移行未実装による実失敗を確認した。
Greenはcc-scheduler 248件、追加の通常dispatch受入1件、最終移行5件、
追加した停止/競合の対照1件で成功。対象doctestとClippyも成功した。
他クレートが使うモデル判定・結果判定の公開APIは変更していない。
通常CIでは既存の影響範囲選択による利用側のテストも確認する。

候補による実3331の無変更確認では、現在の保存継続specがguard済み元参照と
byte一致し、資格を確認できた。実保存・実再開・レビュー・CI・配布は未実施。
原specの一般guard判定を変更した事実ではない。

3636のOpus5.5/maxレビューは旧35a26c47にMust3件を返し、正常完了した。
元job欠落時の旧場所復帰、Git statusのindex更新、未使用receiptの再照合不能を
追加Redで再現した。通常所有照合による親への誤待ちも別のRedで再現した。
指摘に合わせて移行後の記録を設計し直し、元job欠落時の拒否、同じ専用先だけの
履歴付き再資格化、共有indexを触らない照合、通常所有判定との結合を追加した。
拒否テストは正しい事前SHAと理由を照合し、最終Git読取後の実CAS競合も確認する。
3639のSonnet5.5/maxは58783a32の指示継承3file差分にMustなしで正常完了した。
継続仕様と履歴を一緒に読む表現、途中コミットの確認、CLIの絶対パス、旧receiptが
後発失敗にも残ることのassertを追補した。後発の実担当記録が消えた場合は、
履歴の末尾だけで推定せず、実際の保存済みlast_jobとの不一致を拒否する。

所有権の共有判定を変更したため、必要なCIでは既存の影響範囲選択による
cc-job-policyと全利用側を確認する。製品全体の再実装や原台帳復元は行わない。
