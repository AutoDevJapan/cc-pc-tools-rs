# ToDo2887 読み取り専用担当で外部コマンドが 0xC0000142 になる原因と修復

2026-10-19 JST。元2851（元2834）から独立した実行環境修復。コードは変更していない。

## 原因（実測）

- 失敗するのは user32.dll を読み込む実行ファイル（whoami、tasklist、where、powershell、pwsh、git）。読み込まない hostname、findstr、cmd 組込み、Go 製 scrub391 は成功する。終了コードは -1073741502（0xC0000142、DLL 初期化失敗）。
- Codex の Windows 制限トークン方式は、sandbox 内の子プロセスを `Winsta0\CodexSandboxDesktop-<乱数>` の起動情報で作る。desktop 自体は呼び出し元プロセスの現在の window station に作る。呼び出し元が Session 0 の S4U（window station 名は Service-0x..-..$）だと、desktop は Service 側にあり、起動情報は Winsta0 を指すため user32 の初期化に失敗する。Interactive の Session 1 では Winsta0 が現在の station と一致して成功する。
- 実測: 同じ `codex sandbox -P :read-only` を Session 0 から実行すると whoami/tasklist/where/pwsh/git が全て失敗し、probe の startup desktop は Winsta0\CodexSandboxDesktop-…、session 0、LoadLibrary(user32) が「DLL initialization routine failed」。Interactive タスク（Session 1）から実行すると全て成功し、LoadLibrary(user32) も成功。
- 2026-10-13 のログオン前復帰（ToDo2492）と無人運用の整備で DiscordAskTick などが S4U（Session 0）になり、以後 sandbox 内で user32 を使うコマンドが失敗した。sandbox.log でも 10-12 までは 0xC0000142 が 0 件、10-13 以降に出現。`windows.sandbox_private_desktop=false` は現行 Codex では効かなかった。elevated 方式は別の setup refresh エラー（使用中の node_repl.exe）で起動できなかった。

## 修復（設定のみ・可逆）

- 既存タスク CC-DiscordAskTick を S4U/Limited から Interactive/Limited（同一利用者、Session 1）へ変更。トリガー 3 件・Hidden・IgnoreNew・実行上限 PT2M は保持。変更前の定義は `D:/CC/Tools/discord-ask/unattended/tick-s4u-before-2887.xml`。以後 tick → unattended-runner → scheduler → codex_jobs → codex が Session 1 で起動する。
- ログオン前復帰を失わないため、同じ 1 分周期＋起動時の S4U フォールバック CC-DiscordAskTick-NoLogon を追加。実体は `D:/CC/Tools/discord-ask/discord-ask-tick-nologon.cmd`：Session 1 以上に explorer.exe があれば何もせず、無ければ従来の tick を実行する。runner 側のファイルロック（busy）で二重実行は起きない。
- sandbox の全体設定、承認、保護境界、read-only は変更していない。Session 0 の window station/desktop に一時的に付けた Everyone の ACE（効果なし）は元に戻した。

## 検証

- Interactive タスクから `codex sandbox -P :read-only`（AO の作業場所）: pwsh の読取成功・書込は write-denied（read-only 維持）、正式 guard は ok:true・classification_unverified:false・client_resource:false、git rev-parse HEAD は c3c29666f0dbc048d71ec6dfbb26a699fbaa29f5。
- 変更後の tick は unattended-runner.exe と scheduler.exe が Session 1 で動作した。

## 残り・制約

- 実 scheduler 経由の読取専用 job（2872/2874）が Session 1 で pwsh/guard/git を実行する結果は、次の起動後に job の events.jsonl で確認する。
- OS 再起動後ログオンなしの間は Session 0 のフォールバックが動くが、その間の sandbox 付き job は引き続き 0xC0000142 になる（Codex 側の既知の制約）。この状態は未検証の復帰条件であり、解決済みとは扱わない。
- 元に戻す: `Register-ScheduledTask` で tick-s4u-before-2887.xml を CC-DiscordAskTick に登録し直し、CC-DiscordAskTick-NoLogon を削除する。
- Codex 本体の修正（desktop を Winsta0 に固定している点）は上流の問題。
