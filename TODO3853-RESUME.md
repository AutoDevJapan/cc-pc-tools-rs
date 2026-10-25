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

## 2026-10-26 実装中

- Issue513。Red commit3ffede09で通常foreign拒否と新CLI終了1を確認。
- source-repair実装後、33種類の誤関連付け/未終了/資格不足の拒否、5種類の途中失敗と
  応答紛失後の再開、既存source-link回帰が成功。配備済みtodo.exeを隔離DBへ接続し、
  元worker/世代/jobを保った全文instruction登録と再呼出しの不変性も成功。
- 保護未確認の原一般受付ファイルは開かない。新契約のsource-sha256は通常受付の
  ordinaryVersion。原ファイルはstatだけ保持し、正規APIから新たにcanonicalを作る。
  原bytesのhashを確認したと主張しない。既存source-linkのrawSHA契約は変更しない。
- 共通Source資格は元製品の登録rootで通常refresh成功を確認した。作業場所のSource資格を
  コピーや例外登録で作らない。個別の非Source内部記録は一般guardの許可対象へ広げない。

## 固定版レビューと追加修正

- PR515。2fe29625に対する通常CIを開始済み。関連race 24.550sとvetが成功。
- 最初のレビュー3855はAnthropic OAuthが100%のため未起動。OpenAI OAuthを正規refreshし、
  健康な未消費4枠を確認。既存catalogの別モデルgpt-6-sol/maxで3856を通常登録した。
  3855は3856への依存wait。直接の子ジョブ起動はしていない。
- 3856はjob20261026-011308-todo-3856、read-only、実native turn_contextもgpt-6-sol/max。
  2fe29625の指定11ファイルを読了し、Must1（別jobでの再開）とSuggestion1（通常巡回の
  添付metadataだけの変化）を返した。実データ・配布・実反映を確認したレビューではない。
- 別jobの再開は新たなRedで状態公開前/後とも失敗を確認して修正。最初の関連記録は
  不変に保ち、現在の正規actorと古いactorの履歴・終了を独立に確認する。旧actorがまだ
  runningなら拒否する。追加したraceテストは成功。通常巡回のmetadata照合も追加中。
- 次はこの追加差分の読取レビュー、変更した最終候補のCI、既存手順のhash/原子更新。
  実投稿のcanonical作成と配送はまだ行っていない。管理chatの同じ指定キーへmetadataを保存済み。
