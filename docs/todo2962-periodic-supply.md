# 万世紀CIの定期供給を時間切れで省略しない

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/287

ToDo2962の本番受入で、PR265をマージ・専用runtimeを配備し、既存CC-DiscordAskTickへ設定を部分CAS接続した。
通常tickの実行結果はciv6_ci_queue_skipped_no_timeで、キューのtick.lockも生成されなかった。
既存の受付・scheduler・報告・再試行で50秒のcontextが尽き、最後のCI供給まで進めなかった。

既存処理の50秒を保ち、その後に同じ親context内でpin済みCI queueの15秒を与える。
pipe排出5秒を含む全体上限は70秒。既存タスクの実ExecutionTimeLimitはPT2Mで、設定は変更しない。
既存VBS・1分周期・IgnoreNew・窓非表示・lock・CLI単発配信50秒・pin検査・供給上限を保つ。
新しい監視や常駐処理、token・runner登録・製品jobは追加しない。製品workflowは供給実証まで公開しない。

受入: 通常phaseが自分の期限に達しても、未失効の親からCI queueに有効な期限を渡す。
親の取消・期限は無視しない。通常phaseの状態とerrorを保持し、子contextを解放する。
Red: TestPeriodicPhasesの未実装で失敗。Green: queue既存テストと新しい期限境界、unattended-runner race/vet。
別モデルMust解消・同HEAD正式CI・通常merge・配布後に、既存定期処理の実供給を確認する。

## 2026-10-20 実機への反映と受入の途中結果

- 修正は独立レビュー3101（Sonnet5.5/max）でMust0、同4557dbeのWindows unattended-runner・Go解析・保護処理の必要検査が成功した。変更していないmoduleとRust workspace/CI設定の差分0を確認した。本人の影響範囲別検査・Windows優先方針に従い通常PR288をmerge c21b2a5へ反映した。残る自動検査は未完了として保持し、成功と読み替えていない。
- 同4557dbeの実行物（SHA256 B88EAAE4305143E51FB3463DE8A1C91285364D601166E9E95022688C9FF1F49D）を配置した。現用の旧SHAを再確認して同driveのatomic replaceを行い、旧byteを保管した。タスク設定とVBSは変えていない。
- 通常周期の起動器→更新版unattended-runner→CI queueという親子関係を、生成時刻とPIDで実観測した。通常処理が50秒を使った後にqueueが始まっている。最大65秒を使う回は次の1分起動がIgnoreNewで省略され、実効周期が約2分になる。同時workerは増やさない。
- PR149は元f3e26f4の子2bd3a366f8974ba54df83927102aa5bdad3983e5へ3workflowだけを公開した。全tree比較とbranch再読が一致した。製品source・mode・他pathの変更は無い。https://github.com/AutoDevJapan/civ6-banseiki/pull/149
- 新HEADでRust Windows/native-driver/live-chainの3件と標準CodeQL2件が作成された。この時点のPC queue受付は3件、最初のRust jobは実機準備中だった。CodeQL actions成功・Rust実行中という当時の観測は、以下の最新結果で更新する。
- 最初のPC3＋CodeQL2、merge後のPC3＋CodeQL2、移行検証最大2という計12件の上限を維持する。publishや同じ正式jobの無意味な再実行は繰り返さない。

## 2026-10-21 09:28 JSTの実結果

- 標準CodeQLの2解析は同じ製品HEADで成功した。Windowsの最初の資源は、SDK準備後、キャッシュのポートがWindowsの予約範囲と重なり停止した。GitHub jobは未割当・steps0で、製品検査は始まっていなかった。
- 公開repoを回収時に拒否する不具合はPR291、空きポートの割当はPR319、回収後の未開始予約を再開する処理はPR321で通常mergeした。各固定HEADの必要正式検査と別モデルMust0を確認した。SDKのproxyは新規資源内のhardlinkにし、通常cleanupを妨げるreparseを増やさない。
- 初回資源はPR323の限定した正規処理で退避済み。元場所不在、閉じた原所有記録、退避先の元ディレクトリIDと原診断の保持を確認した。再開CLIも管理者の回収marker・同じSCMの停止とGitHubの未割当状態を確認して成功した。読めないprocessを不在とせず、調査範囲と件数を証拠に残した。
- 現用helper・queueとその固定pinを更新し、既存定期処理のpinだけを変更した。初期3件の予算キーを保ったまま、同じRust jobの予約を1回再開した。既存の通常周期が新しい所有資源を作り、現在はprepare中である。製品jobの再作成・rerunは0件。
- 製品HEADは2bd3a366のまま。Windows3件の正式検査・成果物hash・正常cleanup、PR149のmerge、merge後の通常供給と回収、継続運用の有効化は未完了。基盤のmergeや配置だけを製品CIの成功とはしない。
