# PR253の正式CIを復旧する作業記録

2026-10-20 JST。親は共有ToDo3020、担当は3024。

- 元候補は `9a907d39c2f8d0ba810436f78464a4e6f7fa7ebc`。元の別モデルレビューは Sonnet 5.5、最新Must0。正本の未追跡記録は保持。
- 正式Windows run `39279299745` のログを通常のGitHub APIで取得。344件中342成功、2失敗、3skip。失敗した2件はdisplay APIのHTTP fixtureが、Consoleだけを試験用へ差し替え、Operationsを未初期化の本番用のままにしたため503になった。元候補の正式CIを成功とは扱わない。
- Linux runner27はListener不在。既存コンテナの `/runner` 1GiBと `/tmp` 64MiBが使用率100%。inode枯渇ではない。診断ログは既存の永続workへ圧縮保全し、同じコンテナを停止後、元のHidden/S4U/Limited定期タスクで再起動。初期tmpfsは674MiB/1GiBと4KiB/64MiBになり、Listener1系統でonline/busyを確認。
- Windows runner28はidleであることとWorker不在を確認して元の専用仮想アカウントのサービスを再起動。一時的なsession conflictの後15:56:45 UTCに再接続。runnerの対象repo、権限、ラベル、hook、Cargoゲート、隔離は変更していない。
- APIのrun/check表示がqueuedのままでも、個別jobログには実行と失敗の記録がある。判定を混同せず、後続は固定SHA、実job、upload、終端conclusionを照合する。

次の工程: 試験用Operationsを同じ隔離rootから初期化し、失敗2件と影響範囲を検査する。追加差分の別モデルレビュー後、PR253へ非forceで反映し、正式CI・通常マージ・mainと稼働を確認する。追加の常駐ポーリングは作らない。3021/3023の担当や本人の会話には触れない。

コンテナは既存の継続資源であり削除対象ではない。OS再起動後の復帰は今回も未検証。tmpfsへの蓄積の再発防止は既存の保守担当と照合し、必要な有限作業へ分ける。

追加差分の検査記録:
- 0c99f7eの全localai検査は260成功/84失敗/3skip。jobsの親を参照する試験用launcherの配置も合わせる必要があり、c9fecf2で修正。
- 最終c9fecf2でlocalai-web-rs nextest 344成功/0失敗/3skip（13.216秒）、doctest成功（対象0）、全target Clippy成功。全workspace検査とrelease buildは追加で実行していない。
- 稼働版のSHA256は元配備記録と一致。保護されたjobs APIはschema1/ok、masked_metadata、metadataOnlyを維持し97msで応答。
- 追加差分の読取レビューは共有ToDo3025へ登録（実起動・Must0の確認は未完）。モデル名はCLI仕様に従う短い別名claude-sonnet-5-5。anthropic/を付ける引数はValueErrorで登録されなかったため、重複登録はない。
- 作業ブランチをscheduler/todo-3024-pr253-fixへ切替え、checkpoint-workspaceを元のjob/id/expected_branchと照合して保存した。同ブランチはGitHubへpush済み。元PRのheadにはまだ反映していない。

進捗率は6条件（runner復旧・追加修正の影響範囲検査・追加差分レビュー・PR正式CI・通常merge・main/稼働確認）のうち2条件を確認した33%。

再開時のレビュー受領: ToDo3025の原応答と実turn_contextを照合し、Sonnet5.5、固定9a907d3..c9fecf2のMust0を確認。対象ソースはc9fecf2から不変。試験未実行というレビューの留保は、担当の保存済みnextest344成功・doctest0件・Clippy成功と区別した。任意のrootをHTTPから指定する経路は無い。初期snapshotの503検査も残るため、追加の任意Nitを理由に本番コードや試験を変更しない。文書のSuggestionは検査経緯を含む追記と表現修正で反映。

正式なPR検査と通常マージ（2026-10-20 JST）:
- 最終HEAD 9a178dadddd3da22a7895ccb6e177ae3776b22a6のWindows2件とCodeQL4言語はすべてsuccess。3workflow（39284507336 / 39284507371 / 39284507490）もcompleted/success。成功した既存runを再実行していない。
- Code scanning analyses 1982503113（Go）、1982494802（Actions）、1982489102（Rust）、1982437442（JavaScript/TypeScript）は、すべて同じ最終HEADのrefs/pull/253/headで保存され、error/warningは空。merge refの解析と混同しない。
- 追加差分の実Sonnet5.5レビューMust0を照合し、match-head-commitを指定した通常mergeを実行。PR253は2026-10-19T17:02:17ZにMERGED、mainのmerge commitは7177b396141e961388531b9019a30216ff9a8e07。第一親3423a387、第二親9a178dadの2親と、レビュー候補の祖先関係をGitで確認。
- mainの自動実行はCodeQL39290685102、Agent progress39290685972、Build speed39290685227。mainの終端検証は未完。失敗なら原因を直し、同じ正常な全件検査を重ねない。
- マージ後の稼働APIは103ms、schema1/ok、masked_metadata/metadataOnlyを保持。設置binaryのハッシュは既報C63A5E80…のまま。追加差分は試験準備と同じ本番定数への生成委譲であり、表示動作を変える配備は今回追加していない。

残りはmainの正式結果と同sourceの保存、稼働確認の最終照合。6条件中5条件を確認した83%。本人のPC操作と追加の判断は不要。

最終受入（2026-10-20 JST）:
- main7177b396141e961388531b9019a30216ff9a8e07の3workflowと必要6jobがすべてcompleted/success。条件付きpackage-toolsは本来の条件どおりskip。最新remote mainも同SHAを照合した。
- CodeQLのmain保存はRust1982672814、Actions1982629602、JavaScript/TypeScript1982622866、Go1982567232。4categoryすべてrefs/heads/main、同merge commit、error/warning空をAPIで確認した。
- 最後の実稼働確認は進捗入口307/no-store、保護済みjobs API schema1/ok、39ms。進捗値は0〜100または欠落、工程は固定語彙、contentAvailableは全件false。既存の設置binary SHA256はC63A5E8028E90EE720D3D5CC0350C48361CEFA8336233A466A909CB833C30B61で不変。main全体から新たに再ビルドしたとの主張はしない。
- 専用runner27/28はonline/idle。継続運用する元のcontainer/runtimeは保持。本人の会話・3021/3023・正本2903の未追跡記録に触れていない。
- 終了済みの自分のCargo生成物3,112ファイル/2,991,465,749バイトは、作成証跡・祖先と配下のリンク不在・稼働使用者不在・追跡ファイル不在を照合し、Skillの所有記録なし既存生成物の手順で同じDドライブの削除希望へ退避した。元の不在と件数/容量一致を確認。空き容量の回収とは扱わない。原ソースと検査証跡は保持した。

この有限の正式CI・通常merge・main/稼働確認は6条件すべて達成。親3020の常設TUI受入と保護設定復旧は別担当の未完了条件として維持し、製品全体の完了とは混同しない。
