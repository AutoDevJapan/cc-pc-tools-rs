# PR148のLinux正式runnerを有限helperとして実装する

元2736の独立したGo実装。元製品2640/880の依存・未完・停止を解除しない。実GitHub APIで照合するAutoDevJapan/civ6-banseiki、repo1392152644、PR148だけ。新しい共有ToDoで正常割当を使い、直接子Codexを起動しない。GPU/Civ6、APIキー、課金、新常駐、他Org、旧製品folder/原guard/登録manifest/共有resident bridge変更は不可。

目的: tools/product-ciに、通常Rust/Linuxの元jobを本物のActions ephemeral runnerで実行する有限経路を実装する。この子工程では製品sourceの取得/展開/実テスト、GitHubへのworkflow書込/dispatch/runner登録はしない。親2736がレビュー済みhelperを統合して実行する。子はGo検査とcredential-freeな実namespace/共有Cargo gate fixtureまで。原製品の旧検査を重複実行しない。

起動時D:/CC/AGENTS.md、同repo AGENTS.md、元2736のspecとTODO-2736-CHECKPOINT.mdを読む。対象codeは元2736 commit822b4cbのtools/product-ci/pr148_plan.goと現branchに追加されたpr148_linux_probe_test.go、既存tools/product-ci/{codeql.go,codeql_admission.go,codeql_queue.go,source_materialization.go,lifecycle.go}、tools/linux-runner-bridgeの有限Cargo adapterだけ。旧bridgeの受付や権限を変更・再利用しない。元2648/2710/2730のroot/SID/profile/lock/拒否cleanupへ触れない。

固定原source75a4051305b6f47c70ddb59a8d2bf9015666f12c、treececb9cd535d135a1eb4407882db532c325f42860、base1100676d1ed421794c252fb4b15ed3d954e21a5b、branch codex/todo2640-workspace-doctests-20261015。
親のpr148-planは三workflowの原digestと4jobのHosted予算による未開始failureをAPI再検証済み。route/checkoutだけの案をRAMで生成し、原commands/OS/timeout/失敗伝播を保持してmain/他PRはHostedを維持する。現製品sourceは未変更。
将来親がこの案を同branchへ非force更新した新headを検査する。旧headを戻したり任意sourceを受け入れず、親が作る新commitは原75aだけを親に持ち、変更がproposePR148で生成した三workflowだけと全bytes一致すること、他のtreeエントリー不変をGitHub APIで実証して新head/treeを明示固定する。元75aも黙って最新として扱わない。勝手な追加変更があれば拒否して具体理由を返す。

既存のcredential-free SDK image sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011 をimmutable baseとして新しい専用containerへ使う。新nameはtodo2736を含め、cc.instance所有とsafe-delete createしたreceipt rootを使う。nonroot/init/readonly/no bind/no socket/no GPU、有限CPU/RAM/PID/tmpfs、正常/取消し/不明createの回収とcandidate排他を保持。
親は実probeでこのSDKの公式runner/Rustup/Cargoは存在、pwshは不在、rootはreadonly、workはwritableと確認した。公開PowerShell v7.6.7 linux-x64.tar.gzをGitHub公式assetから取得しSHA256 6896f9e3a6b0a354bf470835a96da76d2f801cd50dcf047ad7c4563bf93bf431を照合、stdinで新guest tmpfsへ供給して起動成功。imageを変更せず回収済み。原失敗probeも保持。SDK準備を正式CI成功としない。
原workflowはrustup toolchain install 1.95.0 --profile minimal --component rustfmt、cargo fmt、cargo test --locked --manifest-path tools/Cargo.toml --workspace --all-targets、同doctest、cargo build --locked --release --manifest-path tools/Cargo.toml --workspace --bins、pwshの全scenario --check。これらの原bytesを変えない。original rustupコマンドで導入できる新しいtmpfs CARGO_HOME/RUSTUP_HOMEを用意する。原scenarioが./tools/target/releaseを参照するのでguest実targetを別pathへずらさない。

新finite callbackだけでhost共有cargo_gate.exeへ接続し、1本ずつ専用targetのhost lockを取る。元bridgeのserverや許可を拡張しない。引数は今回Linux原commandsの正確な集合だけ、run/attempt/repo/sourceとnonceを毎回照合。caller指定のhost command/path/環境やDocker socketを渡さない。gateが追加するhost --target-dirと並列argsを正確に扱い、guesttarget/共有lockの対応を維持する。直接/入れ子Cargoをゲートなしで通す経路を作らない。Cargo process群の正常/取消し排出が実証されるまでhost gateを解放しない。失敗exit/log/bytes/SHAを保持する。
公式runnerはsame-repoだけ、RAMのtoken stdin、no-default-labels/ephemeral/disableupdate、label cc-product-2736-1392152644-ubuntu-latest。sourceはGitHub API→新guest stdinだけ、元commit bytes/全blob/index tree/実HEADを検証する。親の元branch同headのworkflow_dispatch全jobを固定し、run/attempt/OS/workflow digest/実GitHEAD-tree/checkout認証不在をstart/completeとhost前後で照合する。原PRイベントはmerge SHAになり得るため安易にsource headと混同しない。別run/jobはfail closed。親が固定新headの重複automatic runを同sourceのfull dispatchで置き換えた場合だけ元未実行runのcancelを検討し、ここではcancelしない。
正式job/run/checkと原log確認は親の工程。listener exit、SDK、fixture、登録をsuccessとして転記するAPIは作らない。

Go unit/vet/Windows+Linux buildと新credential-free正常/取消しfixtureを実行。共有Cargo gateを使うfixtureは割当cargo枠を守る。製品検査ではなくfixtureと明記。独立OAuth別モデルレビューを通常共有ToDo --specで依頼しMustを解消する。原検査条件と検証済みprobe結果を渡す。単一fileだけならHaiku、複数fileならSonnet等。レビュー待ち終了時は正規todo waitを使う。
helper差分を専用branchにcommit/pushし、実hash/実検査/別モデル応答/残留と未達を元2736へnote追記して配送。親2736/2640の完了は待たず、製品PRは変更しない。自分の資源を恒久証拠SHA照合後safe-deleteのplan→別通常delete→finish、拒否は再試行せず記録。scheduler checkpoint-workspaceを正規保存する。本人PC操作/再起動を前提にしない。
