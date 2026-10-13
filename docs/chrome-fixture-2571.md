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

## 原因の再現と修正

- 36f5ce8の実Build speed38402534580は2058pass/5fail/5skip。全件で再現し、doc/Clippyは後続のため未実行。
- 22:19:29〜22:19:34 JST、担当の別profile/別アカウントによるCPU Chromeのロック保持実験と重なって、runnerのfresh profile generation4 launch0と後続4件が0.2秒程度でexit21。fixture stderrはProcessSingleton作成失敗を報告し、profile lockfile自体の共有違反は報告しない。
- 保持実験のChrome自身はlockfile共有違反0x20を報告し、約5秒後exit21。起動器が実Chromeの21をそのまま返すことを確認。
- 担当の実験入口と専用runnerサービスは双方Windows Session0。Chromeの起動用mutexはprofile共通の `Local\ChromeProcessSingletonStartup!`。既存mutexをCreateMutexWで開く際はMUTEX_ALL_ACCESSが必要で、作成元のtokenのdefault DACLが使われる。別アカウントの起動が重なれば、profileが別でもaccess deniedとなる。
- 修正はheadless Chromeをspawnする前に同じ名前のmutexのhandleを取得し、CDP準備まで保持する。所有権は取得せず、Chrome自身のロック取得を妨げない。別アカウントが作成したmutexを開けない場合はspawn前に待つ。DACL/token/権限を変えず、Chromeの非zero終了は依然として失敗。待機とCDP準備に既存の30秒を共有し、失敗後のChrome再起動はしない。
- 新しいnative Windows検査は固有名の検査用mutexだけにdeny DACLを設定し、実access deniedの観測後に解放して受付成功を確認。受付がmutex所有権を保持しないこと、継続したaccess denialが同じ起動期限で失敗することも確認。Chromeの実mutexや既存資源のDACLはテストで変更しない。
- 手元CDP/native関連6件が6pass。実runnerでの修正HEAD検証と新差分の別モデルレビューが残る。

Windows API: https://learn.microsoft.com/en-us/windows/win32/api/synchapi/nf-synchapi-createmutexw
名前空間: https://learn.microsoft.com/en-us/windows/win32/termserv/kernel-object-namespaces

別モデル原因調査2572はモデル枠待ち。通常コンソールのdelegateもdelegation_disabledを返すため、停止設定は不変更。実行済みレビューとして扱わない。
