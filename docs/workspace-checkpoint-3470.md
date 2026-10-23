# mainから本人の作業ブランチへの再開情報保存

ToDo3470。既存のcheckpoint-workspaceが、正規に割り当てた旧mainを入力時に拒否する。
旧値を架空の作業ブランチへ置き換えず、同じ入口で実際の移行を保存する。

今回追加する旧ブランチはmainだけ。保存先は従来どおり同じToDo番号の
`scheduler/todo-<id>-<suffix>`に限る。他の旧ブランチへの一般化はしない。
mainの場合、入力のjobが対象行の現在のscheduler.jobかつ正確なtodo_idを持ち、
受付時のwork_owner.branchがmainで、work_ownerのrepository/git_common/worktreeが
現在の実Gitと一致する必要がある。元担当が正常終了しても、現在の所有記録が
同じで競合がなければ保存できる。過去の別担当の履歴だけでは許可しない。

元のstatus、資源、許可、停止、依存、登録source、worktree、jobは変えない。
両パスの保護判定、同じGit管理領域、競合と実branch/HEADの照合、queue lockと
対象行全体・project登録のCASを維持する。Git操作は読み取りだけ。
同じ要求の再実行と--checkでも所有・実状態・CASを省略しない。

受入テストは実Gitと合成のToDo/job/processだけを使う。mainから同じ番号の
ブランチへの保存、--check無変更、同要求の再実行、元行・未コミット成果の保持、
通常の再開処理による読取を確認する。別ToDo、過去job、場所/保存所有/管理領域の
不一致、HEAD差、古い期待値、任意の保存先/旧ブランチ、拒否資源、競合、途中の
note/依存/停止/担当/project変更を拒否する。privateの原資料はテストへ入れない。

実施順は回帰テストの失敗確認、実装修正、影響するcc-schedulerのテストと
doctest/Clippy、固定base/headの別モデルレビュー、CI、通常マージ、実行版の
並行変更を保持した配布、現在所有を再照合した正規保存、親への結果保存とする。
受付や--check成功を実保存の完了と報告しない。
