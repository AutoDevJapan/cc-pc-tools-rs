# civ6-ci-linux 作業記録（ToDo2964 / 親2962）

## 完了（2026-10-19）
- 新規 module tools/civ6-ci-linux（Go）の本体、単体テスト、README、別 workflow（.github/workflows/civ6-ci-linux.yml）
- 検証: go test -race / go vet（Windows と GOOS=linux）、-tags civ6contract 橋渡し検査（親 module を -modfile で差し替え）、
  実 Docker credential-free lifecycle（normal / cancel / receipt-loss）通過、名前空間の残り 0、所有フォルダは safe-delete finish 済み
- 区切り commit 4356c73 / 699bbfb、branch scheduler/todo-2964-linux-runner-2962 を push、PR #246 作成
- 別モデルレビュー: ToDo 2970（gpt-6.1-sol・読み取り専用・spec 付き）を登録

## 残り
1. 2970 の結果（Must/Should）を受領して直し、同じ検査を影響範囲だけ再実行
2. PR #246 の CI（civ6-ci-linux.yml と既存 workflow）成功を確認し、Must 0 なら通常 merge
3. 親 ToDo 2962 の note へ commit/PR/レビュー/検査/CLI 契約/未実施を保存（通常完了通知は送らない）

## 未実施（親の統合後）
- 実 source 取得、runner 登録、製品 Cargo、CodeQL、公式ジョブの hook 実イベント（GITHUB_EVENT_PATH が start hook で読めるか）、PR149 の正式 6 ジョブ

## 方針
- 親 contract が未マージのため default build は fail-closed（unwiredContract）。-tags civ6contract で接続（ホスト exe とゲスト ELF の両方）
- 親2957/2955/2962 へは wait しない（循環防止）。待つのはレビュー 2970 だけ
