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

保存specは選択した元ToDoの正規specs内の一件だけを用途付きで読み、
guard済み参照元の原byteとSHAが一致し、元番号と専用worktree/branchの指定が
含まれることを照合する。一般guardの例外・旧台帳復元・別コピーは作らない。
specの内容をstdoutへ出さない。不一致は原資料を保持して拒否する。

停止、未完依存、別project/owner、未知の来歴、同じ元番号の実行中・不明worker、
専用先の競合workerは拒否する。共有元の確認済み別担当にはGitの読取だけを行う。
元の全行、project登録、job metadata、spec byte、両場所の実branch/HEADと
作業状態を確認結果のSHAへ結ぶ。schedulerと通常queueの排他中に再照合し、
ToDoロック内で元行とprojectが同版の場合だけ保存する。

変更はschedulerの作業場所・branch・未起動予約の退役と移行receiptに限る。
仕様・note履歴・過去job結果・本人回答・資源・停止・依存・途中成果は保持する。
繰返しは同じ保存状態を再照合して無変更。次の通常prepareはこのreceiptを
再検証して専用場所を選び、旧共有元へ戻さない。通常の容量・優先度・資源・
単一所有の判定は維持する。元番号の一意な実起動と最初の着手は別に確認する。

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
