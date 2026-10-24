# ToDo3726: Windows job.json replacement

元3700/3686の未完了と配送を維持し、cc-jobsだけを修復する。別司令塔・モデル/認証・課金・保護原本・全体queue設定は変更しない。Cargoは通常割当済み。

起点: `8ed37bbecc70ef87f8d55955f7a690f36101bfdd`。
作業branch: `scheduler/todo-3726-windows-3700-3686`。
開始job: `20261025-043658-todo-3726`。

所有記録: `9281a173abd026b4b8f04bda4a4c52f4`。
一時データはworktree直下の同IDの`codex-owned-`フォルダだけ。
ここへCargo target、完全テストログ、検査用データを保存する。
終了時はsafe-deleteのplan→別呼出し削除→finish。拒否は正規quarantine。

## 確認済み

- 最新D:/CC/AGENTS.md、owner-agent-conduct、repo AGENTS、Windows置換・共有ToDo・安全清掃・日本語報告・レビューのSkillを読んだ。
- GitHub APIで実owner `AutoDevJapan/cc-pc-tools-rs`。worktreeのguardはclear/client_resource=false。原台帳の復旧を意味しない。
- 元3700の指定2identityファイルは双方failed、pid/codex_pid不在、Archive、os error5。原job本文は読んでいない。
- 現用exe SHA256 `98e8279c8225e7fa553b5e1a182b4292003ff5a5793234f1e7bb963ed1c12084`。mtime/現checkoutからソース一致を推測しない。旧2458のsidecarは別hashで、現用の証明に使えない。
- 現sourceはtmp書込・renameを各1秒/50回再試行する。`supervise_attempt`の進捗saveが失敗するとwork全体のErrとなり、生存中の子を停止してfailedへ進む。
- 新規Windowsテストを追加中。初回compileのimport漏れを修正。実挙動のRedはこれから。

## 残り

1. Windows実共有・解放・期限超過・ACL拒否と進捗失敗のRedを確認。
2. 表示専用更新だけを延期し、起動/所有/再開ID/終端は必須のまま修復。backoff、再保存、false-success/二重起動/後始末/結果返信を確認。
3. cc-jobsと逆依存の関連テスト、doc、Clippy。全workspaceは不要。
4. 通常ToDo/spec経由の固定差分・別モデルレビューを登録し、実job/nativeを照合、Mustを解消。元3080/3669/3686のLuna指定をこの新修復へ拡大しない。
5. 固定HEADの正式CI成功後PRを通常merge。並行変更を取り込み、現用のsource/hashと比較して最新merged sourceを正規配布。実Windowsの継続・結果返信・終了を確認。
6. 原3700/3686へ具体結果と残件を正規に返し、checkpointを保存。自分のToDo状態はschedulerが更新する。

本人の判断・PC操作は不要。登録・テスト追加だけで完了にしない。
