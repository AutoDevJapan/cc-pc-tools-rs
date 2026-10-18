# 親2737への引き継ぎ

Windows runnerの接続処理を実装し、Windows上の合成検査を通しました。
別モデルの確認と親による実VM・公式runnerの受入は、別の完了条件です。
製品の通常CIが成功したという報告ではありません。

## 成果の範囲

- 作業ブランチ: scheduler/todo-2828-godd-mcp-windows-ci-guest。
  固定コード版はこの文書を追加したコミット。最終HEAD/treeとレビュー結果は
  TODO-2828-CHECKPOINT.mdと共有ToDo2828の引き継ぎ記録へ追記する。
- 新規モジュール tools/windows-ci-runner-2737 の公開型、全フィールド、段階、戻り値、
  seed、通信、固定pathとoffline stagingは [README.md](README.md) に記載。
- SDKの変更は guest_service_windows.go のみ。GuestBackend / RunGuestServiceBackendを
  追加し、既存のSCM・COM・mTLSを使う。runner modeではSDK準備APIを出さない。
  SDK旧5準備/6版確認を再実行していない。元2783のfolder/assetへアクセスしていない。
- sourcepacket / normalci は固定c373a18cから元のrepo pathへ取り込んだ。同機能の
  変更版を別pathに並立させず、M1/M2対策を保つ。既存の新しいproduct-ciは戻していない。
- 親の現行controllerは編集していない。原source取得・VM/runner/SCMの実起動、
  重Cargo、原PR操作、独立PR、merge、host昇格/GPU/Civ6は行っていない。

## 検査

このPCのGo1.27.0 / Windows amd64で確認した。仮のGitオブジェクト、archive、
TLS証明書、loopback、Windows pipeとテスト専用子プロセスだけを使った。

| 対象 | コマンドと結果 |
| --- | --- |
| 新モジュールとguest entry | go test -race -mod=readonly -timeout90s ./... 成功、go vet ./... 成功 |
| 取り込んだsourcepacket | go test -timeout90s ./... 成功23.793s、go vet ./... 成功 |
| SDKおよび既存cmd利用側 | go test -mod=readonly -timeout90s ./... 成功5.130s、go vet ./... 成功 |
| normalci公開型 | go test -mod=readonly ./normalci / go vet ./normalci 成功。元package自体にはtestがなく、新モジュールからbinding拒否を確認 |
| 書式 | gofmt / git diff --check 成功 |

成功した確認には次を含む。

- 誤source/run/role/attempt、legacy label、範囲外jobs/threads、入力付きplanの拒否。
- 実TLS1.3 mTLSと誤client証明書の拒否、同cloneへの再接続拒否。
- source-ready→configured→start→complete→runner-exitedの順序とpositive host claim。
- 登録IDを拒否した時にlistenerを起動しないこと、hook再入と別runの拒否。
- EOF/取消し時のhook解放、EOFが設定処理の途中でもその処理を止めること。
- 仮sourceのMaterialize/VerifyCheckoutと、CRLF変更を拒否する物理照合。
- 実Windows Job Objectで通常の親終了後も残る子孫を停止すること。
  取消し時も、停止前に保持した子孫ハンドルが終了状態になること。
- SDKの実Windows named-pipe期限検査と、合成COM nonce/transport/API検査。
- seedに秘密鍵を書かないこと、stock ZIPの不正hash/path/重複を拒否すること。

初回のコンパイルで、内部hex関数とencoding/hexの名前衝突、Windows SID.Stringの
複数戻り値の扱いを検出して修正した。gofmtをmodule cwdからrepo相対pathで呼んだ
初回のpath誤りも修正した。これらは実VMの失敗ではない。上表は修正後の結果。
test stdoutの記録元は本ジョブのexec結果であり、公式CI原ログではない。

## 親の次の工程

1. 別モデルの有限レビュー結果を反映した固定コードHEADから直接link/buildする。
   このToDoは親2737や元製品の完了を待たない。
2. 親の既存route/normalci.Planを、runとroleを含むlabel契約へ接続する。
   baselineのlegacy labelはNewSeedが意図的に拒否する。許可範囲は拡張しない。
   親が明示pinした製品head/tree/workflow hashをseedへ入れる。code SHAと混ぜない。
3. 親のcanonical Cargo gateを保持し、受領assetの照合と新cloneへのoffline stagingを
   READMEの固定path/service名/COM/TLS手順で行う。保持原diskは変更しない。
4. Controllerのqueued/registration intent/実numeric runnerID/offline inventoryと
   official claimをつなぐ。source/tokenはRAM/mTLSのみ。単一公式jobを受ける。
5. original Windows13工程/profile/直列/20分/権限/if/gateをそのまま実行し、
   実checkout後のproofと公式APIのjob completionを別々に確認する。
   SDK環境とstock config/run.cmdの実互換性は未検証。実payload ZIPの展開/起動も未実施。
6. complete hookでVMを消さない。runner-exitedと公式completion、VM停止/登録/lock回収を
   親が確認する。guestのDrainedはVMや登録が消えた証拠ではない。

このbranchから独立PR/mergeは作らない。親が固定差分を統合した後に必要なCIを行う。
このbranch単独のGitHub CI成功は主張しない。
