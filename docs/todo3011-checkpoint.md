# ToDo3011 配備記録（2026-10-20 JST、完了）

親2962向け。ソース、PR、guard結果、配備の実施/未実施を分けて記す。

## ソース・PR
- PR252: repair-guard にDiscord無人runnerの3資源の限定資格を追加。tested head d60a2c801321bac76479a4fcb7aeac8fd54b7f69、通常merge 425f90a4a08a200bf74f50990ad8a8848ae7ec29。
- 別モデル審査(共有ToDo3014, anthropic/claude-sonnet-5-5): Must0。Suggestion3件（ハードリンク未検出、junction不可時のskip、product-ci回帰テスト）は実害が小さく、既存テストが担うため修正なし。応答SHA256 C61A51C78A2F9FE676E8D535795034D773ED9416AE567EFB2208CF1D1A442D16。
- 正式CI: 全ジョブ成功。go (Windows, unattended-runner) は初回「branch head check unavailable」（入口の失敗、テスト失敗ではない）で、失敗ジョブの再実行(attempt2)が成功。
- merge後のmain(425f90a)でrepair-guard全件go testが成功(78s)。

## guard反映
- 反映前の実測: 3件とも client_resource=true / classification_unverified=true。
- 配布: main 425f90a から go build -trimpath（vcs.modified=false、CGO_ENABLED=1）。新exe SHA256 EDD50DDF87D859BB254B72D9333B1E8590E107DAD548B3130E43DF0F17BBAD4A。
  同一ボリュームの所有dirから File.Replace で D:/CC/Tools/client-records/scrub391.exe を更新。直前に旧exe(43a26e09、別担当が配備した版でmainの祖先)のSHA256 2068860B…C962 を照合し、backup=scrub391.exe.before-pr252-2068860B。
- 反映後の再照合: unattended-runner.exe / unattended/config.json / discord-ask-tick-hidden.vbs は client_resource=false / classification_unverified=false。original_registry_recovered=false のまま。
  兄弟 unattended/conversation-questions.json はtrueのまま保護（導入前の候補exeでも discord-notify.json・.bak・discord_ask.exe は true）。

## runner配備
- 配布exe: 親のstaging（sourc c9fe0934, tree b9cc66a = main と同一）。使用前にsize 11289600 / SHA256 C2242BF47CF2DEA2DD99CEF469610DC8DC3076470B672BFBD52828AA2C11E8C6 / buildinfo vcs c9fe093 modified=false を再照合。
- 旧exe(vcs c2dc169, SHA256 13B9F3DFD9D05EEB70CD5A909DA9609C65972262EE23AC5578582343173FCB2A, 11281920 bytes)の並行変更なしを置換直前に確認。
- 12:31:22 JST に D: 内で File.Replace。backup=D:/CC/Tools/discord-ask/unattended-runner-pr252.atomic-backup.exe（旧SHAと一致）。窓なし。配布後の実SHA256=C2242BF4…11E8C6、11289600 bytes。
- 設定とVBSは未変更（読取も書込もなし）。civ6_ci_queue は未設定（親2962が実施）。
- 既存task CC-DiscordAskTick: 置換後の12:32:07の実行がLastTaskResult 0で完了。次の12:33:07の実行も開始。

## 注意事項
- 資格成立前に installed exe のSHA256を1回読んだ（内容解析・起動なし）。
- 一時領域は safe-delete で plan→別削除→finish(source_absent) 済み（C:の検証用、D:のguard/runner配備用）。