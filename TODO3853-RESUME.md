# ToDo3853 再開記録

元依頼: Discordの質問回答と一般受付の競合を明示の1件だけ正規復旧する。
2913と3852の再起動、製品実装、Chrome操作、過去全件の移管は範囲外。

- 開始base: e1a4c21d（origin/mainと一致）。branch: scheduler/todo-3853-discord。
- 実所有者: AutoDevJapan/cc-pc-tools-rs。専用worktreeの通常guardはclear。
- 作業job: 20261026-002957-todo-3853。重いCargo/GPU/Civ6の割当なし。
- 既存source-linkは元/受付/保守の同一projectを要求し、終了済み受付のjob確認がない。
  既存captureのforeign拒否は保持し、source-repairという別契約を追加する。
- 3852はsucceeded/exit0の実jobを持ち2913だけにwait。2913は現在の質問waitと
  元worker/resume_generation=5を保持。復旧直前に再照合する。
- 今回作成した一時領域の所有ID: e529a241a3f12ef9f152d81844b0d862。
  path: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-e529a241a3f12ef9f152d81844b0d862。
  終了時はsafe-delete plan→返された通常削除を別call→finish。拒否時はquarantine。
- 残り: 合成Red、実装・関連テスト、別モデルread-only review、最新候補CI、PR/merge、
  hash照合付き原子更新、通常lease下で1件復旧、metadata/hashと配送readbackを保存。
- 管理chatの記録先キー: work/reply2913-newpost-1563926324979175525。
  本文・秘密・添付は記録しない。実読/実反映は配送とは別に追う。
