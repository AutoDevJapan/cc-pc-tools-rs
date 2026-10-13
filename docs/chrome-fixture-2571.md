# Chrome fixture 修復（親2510）

対象: AutoDevJapan/cc-pc-tools-rs PRIVATE、main 18886ecea9f3e6c699ed4067a636c98b1ed6fc6d。
親2510の稼働runner/runtimeは変更・停止しない。修復PRのマージと新mainの全CI確認は親2510の担当。

## 2026-10-13 調査段階

- 正規Build speed 38397534668: Windows全件2059pass/2fail/5skip。成功扱いに変更しない。
- check_on_logged_in_page_succeeds_and_status_reads_it: 0.223秒でChrome exit21。
- composer_seen_before_its_script_loads_waits_for_page_initialization: 60.024秒でscript未要求。元assert順ではChrome stderrが隠れる。
- 専用worktreeの修正前関連2件は2pass（14.336秒）。サービスrunnerでの原因未解決。
- Chromium 155のexit21はPROFILE_IN_USE。起動移譲の正常終了とは異なる。stderrを捨てていたためlock失敗の具体的理由は元ログでは不明。
- fixtureかつheadlessの場合だけ、専用profile内にChrome stderrを保存し、起動失敗時に先頭8192byteまで表示する。通常のブラウザは記録しない。
- 遅延scriptのjoinを必ず回収し、CLIの失敗を先にassertして隠さない。script送信・回答・終了の検証と期限は維持。

一次資料: https://raw.githubusercontent.com/chromium/chromium/155.0.8059.39/chrome/browser/process_singleton_win.cc
結果コード: https://raw.githubusercontent.com/chromium/chromium/main/chrome/common/chrome_result_codes.h

残作業: 実runnerで限定診断→根拠ある修正→関連検査/Clippy→固定HEAD CI全件/doc（同sourceは重複させない）→新差分の別モデルMust0→親へPR/commit/実ログ返却。
既存PR147は再openしない。本人のChrome/認証/未知資料/runner _diag/GPU/課金APIは扱わない。

## 診断後の観測（未修復）

- 06c4609の専用worktree: Chrome/cc-chrome73pass、Clippy成功。
- 同HEADの実Windows runner: https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38401129768 、73pass/0skip・Clippy成功。再現なし。
- 67461ceに新規12profile×各2起動の有限検査を追加。起動、CDP終了、Windows lockfileのwrite-open可能またはNotFoundを各回assert。失敗のretryではない。
- 手元24起動すべて成功（97.389秒）。実Windows runner https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/38401860707 も74pass/0skip・Clippy成功（215.497秒）。排他残留を観測していない。
- 原因調査を別モデルの共有ToDo2572へ登録、specとsubwork登録を確認。初回profileと再利用の両方を説明できる仮説を区別する。
- 次は全workspaceの実runner検査。`.config/nextest.toml`には新しい排他検査も既存の全thread隔離を維持する理由を明記。対象の変更により既存Build speedのPR検査が起動する。

成功した診断検査を根拠ある原因修正として扱わない。新sourceで起動失敗が発生すればfixture stderrを限定して読む。診断だけのPR161はdraftのまま保持。
