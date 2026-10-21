# 親2737への引き継ぎ

Windows runnerの接続処理を実装し、Windows上の合成検査を通しました。
固定bec99388の全23ファイルは別モデルgpt-6-lunaで静的確認され、Must0でした。
提案2件を修正した固定b60e85eの追加6ファイルも、別モデルgpt-6-lunaでMust0でした。
本ToDo2828の有限実装は完了です。
親による実VM・公式runnerの受入は、別の完了条件です。
製品の通常CIが成功したという報告ではありません。

## 成果の範囲

- 作業ブランチ: scheduler/todo-2828-godd-mcp-windows-ci-guest。
  最終Go実装HEADはb60e85e7f66abb3329264bfc74895a3d3c2be10f、
  treeはe192d0a34d9c546e146575681d3d9365c6185ee0。
  後続コミットは受領記録と文書のみで、Go実装は同一。
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
| 新モジュールとguest entry | 最終実装のgo test -race -mod=readonly -timeout 90s ./... 成功4.280s、go vet ./... 成功 |
| 取り込んだsourcepacket | go test -timeout 90s ./... 成功23.793s、go vet ./... 成功 |
| SDKおよび既存cmd利用側 | go test -mod=readonly -timeout 90s ./... 成功5.130s、go vet ./... 成功 |
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
- 再開後の提案修正: 空directoryが大量にある場合のentry/深さ上限、走査開始前の取消し、
  別runner/起動前/排出状態が矛盾する失敗eventを保存前に拒否すること。

初回のコンパイルで、内部hex関数とencoding/hexの名前衝突、Windows SID.Stringの
複数戻り値の扱いを検出して修正した。gofmtをmodule cwdからrepo相対pathで呼んだ
初回のpath誤りも修正した。これらは実VMの失敗ではない。上表は修正後の結果。
test stdoutの記録元は本ジョブのexec結果であり、公式CI原ログではない。
ToDo2830の原応答・hashと実モデルの補足は[reviews](reviews/README.md)に保持する。
追加レビュー2833はbase93c0c42→headb60e85eの6対象を全て読み、Must0/Suggestion1/Nit0。
実modelはjob.jsonと正規rolloutのturn_context.modelでgpt-6-lunaと確認済み。
実装側gpt-6.1-solとは別モデルで、レビューの検査実行は0件。
提案1件は「走査中に取消す決定的な検査」の追加で、実装の不具合指摘ではない。
現行検査は走査開始前の取消しを確認し、entry間・再帰前の取消し確認は静的照合のみ。
この検査範囲を明記し、本有限実装に検査用注入APIを追加しない判断とした。

## 最終の変更ファイルと統合方法

- 新規tools/windows-ci-runner-2737/: contract.go / seed.goに公開型と固定pin、
  channel.go / frame.goにSDK経由の有限通信、host.go / session.goにhostとの段階契約、
  guest_windows.go / process_windows.go / logbound_windows.go / archive.goに固定guest処理。
  cmd/windows-ci-runner-agent/main_windows.goがSCMとhookの入口。
  *_test.goは合成検査、README.mdは全仕様、reviews/は原応答と受領hash。
- SDKの機能差分はtools/windows-ci-sdk-2737/guest_service_windows.goのみ。
- 必要な依存は固定版のtools/ci-source-2737、tools/product-ci/normalci、
  tools/windows-ci-sdk-2737を元のpathに取り込んだ。親での重複実装を作らない。
- 親はこの固定版から新moduleとSDKのcallbackを統合し、既存normalci/sourcepacketの
  改善を保持する。親の新しいroute/label差分を古い版へ戻す取り込みは行わない。
  READMEのPublicSeeds / NewSeed / Dial / HostSession / Controllerの契約で直接linkする。

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
