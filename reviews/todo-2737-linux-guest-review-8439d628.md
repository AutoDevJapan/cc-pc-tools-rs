# ToDo2737 Linux guest 補助レビュー

対象: `tools/ci-guest-2737/` at `8439d62850b7e9cc5b1632ca90183c40514a8923`（完全なcommit SHA）

## Must（1）

- `tools/ci-guest-2737/host_windows.go:334-345, 456-465` — `New-VM` 後、`vm-created.private.json` の書込みより前に `Set-VM`、`Set-VMMemory`、COM/DVD設定などが失敗すると、管理者処理がエラーを返す。`createVM` はID未設定の `s` を返し、`smoke` は `s.ID != ""` の場合だけ後始末を登録するため、作成済みVMがHyper-Vに残る。`Start-VM` が失敗した場合も、作成receiptがあっても同じ経路で読み戻されず、VMが残り得る。試行ごとのVM名と構成が残って資源を占有し、失敗したゲストが停止済みと確認されないままになる。`New-VM` 直後にID・name・notes・rootをreceiptへ記録し、管理者処理が失敗した場合もreceiptを読み込んで厳密な所有照合後にOff・メモリ0・除去を試みる。照合できない場合は既存の「要照合」状態を明示して残す。

## Suggestion（0）

なし。

## Nit（0）

なし。

READMEとチェックポイントは、Hyper-V fixtureの成功を原製品source・公式runner登録・workflow・Cargo・通常CIの実行または成功と区別している。TLSの相互証明書pin、instance/COMに結び付くreadiness、fixture限定要求、実Linux子プロセスとgroup消滅の確認、通常時のVM回収・window非表示・AdminBroker利用は指定範囲で整合していた。

**Must 1件、Suggestion 0件、Nit 0件。**
