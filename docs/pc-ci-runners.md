# PC専用CIの運用

このリポジトリのWindows検査とLinux検査は、リポジトリ専用のself-hosted runnerへ送る。GitHubの予算上限や契約は変更しない。runnerが停止した場合はジョブを待機させ、Hosted runnerへ自動で切り替えない。

Windowsのラベルは `cc-pc-tools-windows`、Linuxは `cc-pc-tools-linux`。repoが `AutoDevJapan/cc-pc-tools-rs` であり、PRのheadも同じrepoである場合だけ実行する。runner開始hookは別repo、fork、未対応イベントを拒否する。Windowsはcheckout前の最初のstepでジョブ専用の読み取りtokenを使ってbranchの最新SHAを照合する。PRのmerge用SHAではなくhead SHAを固定して取得する。過去のHosted検査の失敗を手動statusで成功へ変更しない。

Windowsの `pc-ci` は事前に検査して専用runtimeへ配備するGo製のCargo shimと入場確認を提供する。Cargoは既存の `D:/CC/Tools/codex-jobs/cargo_gate.exe` を呼び、生成先ごとの排他とPC共通のCPU/RAM受付、Cargo/nextestの並列上限を維持する。Rustは既存の共有toolchainを読み取りで利用し、registryはrunner専用のCargo homeへ置く。workflowからtoolchainや共有ゲート設定を書き換えない。

runnerのruntimeとworkは担当専用worktree配下の `.ci-runtime` と `.ci-work` に置く。作成前不在と担当所有を私的な運用記録へ残し、Gitから除外する。登録tokenは既存OAuthのrepo管理権限で取得し、RAMから公式runnerの `ACTIONS_RUNNER_INPUT_TOKEN` へ渡す。tokenを引数、ソース、報告、標準出力へ書かず、公式runnerが作る認証ファイルを人やAIの調査対象にしない。Org runner権限を増やさない。

Windowsサービスは専用仮想アカウントを使い、本人profileを読み取れないことを実サービスの検査で確かめる。hook、shim、runner環境設定はサービスから読み取りだけとし、ジョブから書き換えさせない。必要な操作は管理者窓口を使い、コンソール/UACの窓を出さない。検証済みの常駐runtimeを誤って検査生成物として削除しない。

既存の全workspace nextest、doctest、全target Clippy、Chrome/ffmpegの実検査、Windows/Linuxの個別検査、Goのrace/vet/buildを維持する。外部資源の直列設定は `.config/nextest.toml` のままとする。マージ前に固定headの必要CIと別モデルのMust解消を確認する。重い検査は同じ生成先で順に行い、同じ最終ソースの全件検査を重複させない。

運用結果、失敗理由、再開手順は `docs/runner-result.private.json` と `docs/runner-handoff.private.md` に保存する。登録、起動、固定sourceの検査成功、マージ、常駐復帰確認を別の到達点として記録する。資格情報や請求数字はGitへ入れない。
