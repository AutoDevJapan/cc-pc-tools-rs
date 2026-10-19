# civ6-ci-linux 作業記録（ToDo2964 / 親2962）

## 親2962の統合後（最新。下段は元2964の履歴）

- PR246はmerge8837e5e済み。共通contractもPR245でmerge済み。
- 通常buildが共通contractを必ずlinkする。旧buildtagは不要。
- 検査は共通civ6-ci.ymlへ統合し、Windows/Linux race/vet/buildとLinux guest crossvetを維持。
- kernel flight lock、status/collect、phase別の一度限り回収記録を追加した。
- 実Dockerのcredential-free回収fixture成功、root/containerとも回収済み。
- review2983のパス綴り違い指摘は、runの絶対パス化・回収側Clean/Windows EqualFoldと
  完全なworker lock記録の解析で修正し、実回収経路の回帰検査を追加。
- 公開済みrepoの受付は親の固定owner/ID/current sourceで確認。forkはPC側Verifyで拒否し、
  公開workflowの外部PRは標準hosted OSへ割り当てる。
- 正式runner登録/製品source/製品Cargo/CodeQL/PR149受入はまだ未実施。

## 完了（2026-10-19）
- 新規 module tools/civ6-ci-linux（Go）の本体、単体テスト、README、別 workflow（.github/workflows/civ6-ci-linux.yml）
- 検証: go test -race / go vet（Windows と GOOS=linux）、-tags civ6contract 橋渡し検査（親 module を -modfile で差し替え）、
  実 Docker credential-free lifecycle（normal / cancel / receipt-loss）通過、名前空間の残り 0、所有フォルダは safe-delete finish 済み
- PR #246、別モデルレビュー 2970（gpt-6.1-sol、HEAD b29a7fb）: Must 1 / Should 1 → 両方修正済み
  - Must: git/ref/heads/<branch> の許可リストが「/」を拒否していた（親 verify.go の branchPath は「/」を保持）。許可して、
    親の実 Verify を通す回帰検査（contract_reads_test.go、-tags civ6contract）と拒否側の検査を追加。旧 regex では失敗することを確認済み
  - Should: validateRegistration を純粋関数に分け、configureRunner を差し替えて「不正入力では設定処理に一度も到達しない」+ 正常入力の対照を検査
- CI 失敗の原因と修正: Windows runner の「shell: bash」は WSL bash に解決されて失敗 → step の env（GOOS/GOARCH）に変更

## 残り
1. 修正 commit の push 後、PR #246 の CI（civ6-ci-linux.yml と既存 workflow）成功を確認し、通常 merge
2. 親 ToDo 2962 の note へ commit/PR/レビュー/検査/CLI 契約/未実施を保存（通常完了通知は送らない）

## 未実施（親の統合後）
- 実 source 取得、runner 登録、製品 Cargo、CodeQL、公式ジョブの hook 実イベント（GITHUB_EVENT_PATH が start hook で読めるか）、PR149 の正式 6 ジョブ
- CodeQL 2 言語が同じラベルを共有するときの割当順（queue.go は最古の queued job だけ登録を許す。違う割当は VerifyAssigned が拒否）

## 方針
- 親 contract が未マージのため default build は fail-closed（unwiredContract）。-tags civ6contract で接続（ホスト exe とゲスト ELF の両方）
- 親2957/2955/2962 へは wait しない（循環防止）
