# 2994から独立したWindows実行部分の実装

対象は AutoDevJapan/PassShare ID1131498455 public main。親ToDo2994。
書込所有はこのmoduleの `cmd/runner/**` だけ。親はinternal/contract、workflow、
planner CLI、go.mod/go.sumを持つ。既存製品・runner・共有ゲートは変更しない。
親2994のworktree/HEADを通常Gitで確認し、このmoduleの契約を利用する。

固定参照 `28c9b9587345f082821c62af804365cf1a64bfe9` の
`tools/civ6-ci-windows`（共有Gitで通常git show可能）を参考に、専用の
`cmd/runner`へ隔離・source materializer・Cargo gate・Job Object drain・
専用service SID・collectの実装と関連検査を移す。原コードは変更しない。
import契約は `cc/passshare-ci-windows/internal/contract`。
この契約はPlan、Validate、Labels、Verify、VerifyAssigned、RequireSuccess、
ObserveJob、CheckEvent、MatchRunPath、OriginalHead/OriginalTree fixture marker、
WindowsLabel、ValidSHA、Ref、OS、ReadAPI、Run/Jobを提供する。
一般PR/forkを受けずmain pushまたは唯一の固定候補branchのdispatchのみ。
workflow/jobはdesktop-build.yml/windows/Windows buildの一組だけ。

独立した専用serviceとfixture service名、state/lock/registry namespaceを使う。
本人profile不読、共有SDK/gate/sccache RX、token stdin/RAM、CREATE_NO_WINDOW、
Jobのactive0/保持handle signalled、継続profile1件の計測、per-job ACL回収を維持。
node22/pnpm10.11.0とMSVC/WebView2/Tauri buildを使える環境を整える。
toolchainはstableで必要な物だけ。共有資源へ包括的ACLを追加しない。
新GPU/Civ6・APIキー・有料API・実製品ビルド・公式登録はこの子では禁止。
正式job/source/hashの実証は親2994が一度だけ行う。fixtureは成功と呼ばない。
ランナー配布のpinは公式release APIを再照合して使う。
実fixtureはsafe-delete所有root、admin操作はCC-AdminBrokerのみ。
prepare/execute/stop/collect CLIを維持し、必要なら親へ具体的CLI差を配送する。
fixtureで正常/取消/再利用を実測し回収を確認する。Cargoが必要なら割当内。

起動時共通AGENTS、作業先AGENTS、費用/Windows優先policy、関連Skillを読む。
自分の共有ToDoはschedulerが状態更新。子Codex直接起動不可。
意味ある区切りでscheduler branchへcommit/pushし、branch変更時checkpoint。
関連Go race/vet/buildを実施、原固定sourceとの差と未検証点をREADMEへ保存。
最後は実commit/fixture/回収/未達を親2994へnote配送、通常完了通知無し。
別モデルレビューと正式CIは親が統合後に実施する。
