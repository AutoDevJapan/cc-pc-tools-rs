# ToDo3011: Discord unattended runner の3資源を分類する

2026-10-19、親2962の依頼。消失した原保護名簿の復旧ではない。`unattended-runner.exe`、`discord-ask-tick-hidden.vbs`、`unattended/config.json` の3件だけを、検証済み修復repoの固定ソースが名指しする運用資源として分類する。

## 名指しの根拠

- 実行物とVBS: `unattended-runner/install-tick.ps1`
- 設定: `unattended-runner/conversation_cli.go` の既定パス
- 上記の対応は `discordask_windows_test.go` がソース文字列で照合する。

## 判定

`repair-guard/discordask_windows.go` は次の正確な葉だけを受け付ける。

- `D:/CC/Tools/discord-ask/unattended-runner.exe` と `discord-ask-tick-hidden.vbs`（実在する通常ファイル）
- `D:/CC/Tools/discord-ask/unattended/config.json`（実在する通常ファイル）
- 同ディレクトリの更新用名: `unattended-runner-pr<番号>.atomic-backup.exe` と `unattended-runner-install-pr<番号>.private.json`（未作成も可）

各葉に、親ディレクトリの物理一致、リンク・junction・reparse不在、現行denyに該当しないこと、検証済みorigin（`sourceVerified(base)`）を要求する。兄弟ファイル（会話記録、通知用の認証ファイル、他の設定など）と親ディレクトリは対象外。分類は本文を開かず、設定の値や認証ファイルを読まない。新しい許可台帳・CLI引数は作らない。

## 配備と実確認

配備後の記録は `docs/todo3011-checkpoint.md` に残す。