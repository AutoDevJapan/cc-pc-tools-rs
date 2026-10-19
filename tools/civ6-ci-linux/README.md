# civ6-ci-linux（ToDo2964 / 親ToDo2962）

内部の実装ディレクトリ名であり、製品名ではない。AutoDevJapan/civ6-banseiki（repo ID 1392152644）の
Linux 側ジョブ（Rust の `test (ubuntu-latest)` と既存 CodeQL の 2 言語 `Analyze (actions)` / `Analyze (rust)`）を、
新しい資格情報なしの Docker 名前空間の中で、公式の ephemeral runner 1 台に 1 ジョブだけ実行する有限 helper。
ジョブの選択・再実行・予算管理は呼び出し側（キュー担当）が持つ。この helper は入力からコマンドを受け取らない。

## CLI（キュー担当が呼ぶ入口）

```
civ6-ci-linux.exe run <plan-json> <new-safe-delete-owned-root> <Linux-guest-binary> <official-pwsh-tar>
```

| 引数 | 内容 |
| --- | --- |
| plan-json | 親 contract の Plan（下記）。秘密を含まない。64 KiB 以下、未知フィールドは拒否 |
| new-safe-delete-owned-root | `safe-delete create` が作った **新しい空の** `codex-owned-<32hex>` フォルダ。`safe-delete plan --id` が同じパスを返すことを確認する |
| Linux-guest-binary | **同じソース・同じ -tags** で `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` ビルドした ELF（ホストに置く固定配置のレビュー済み実行物） |
| official-pwsh-tar | 公式 PowerShell v7.6.7 linux-x64.tar.gz。SHA256 は固定（docker.go の `powerShellSHA`）で、違えば拒否 |

終了コード: 0 = 公式ジョブが **この helper の一意な runner 上で success** と親の `RequireSuccess` が確認できた場合だけ。
1 = それ以外（失敗理由は stderr に 1 行。トークン・ログ本文は出さない）。2 = 引数の誤り。
成功時は stdout に 1 行の JSON（`status`, `kind`, `run_id`, `job_id`, `log_sha256`, `log_bytes`）を出す。
listener の終了コードだけでは成功にしない。失敗時に自動 rerun はしない。

### Plan JSON（親 `tools/civ6-ci-contract` の固定形）

`version, repo_id, head, tree, event, branch, pr, context_sha, workflow_sha, workflow_ref, workflow, workflow_digest, run_id, attempt, job_id, job, job_name`。
この helper が受け付けるのは次の 2 種類だけ。

| workflow | job | job_name | runner ラベル |
| --- | --- | --- | --- |
| `.github/workflows/rust.yml` | `test` | `test (ubuntu-latest)` | `self-hosted, Linux, X64, civ6-ci-linux`（既定ラベル + 1 つ） |
| `dynamic/github-code-scanning/codeql` | `analyze` | `Analyze (actions)` / `Analyze (rust)` | `cc-product-2648-1392152644-codeql` のみ（既定ラベルなし） |

ラベルは親の `Labels(plan)` をそのまま使い、上の 2 通り以外は登録前に拒否する。native-driver / live-chain / Windows 行は対象外。

## 親 contract への接続

- 統合後の既定ビルドは `contract_bridge.go` を必ず含み、
  `cc/civ6-ci-contract` の `Validate / Labels / Verify / VerifyAssigned / RequireSuccess / CheckEvent` を呼ぶ。
  Plan は厳格 JSON で変換するため、両 module のフィールド差は拒否になる。
- `go.mod` に `require cc/civ6-ci-contract v0.0.0` と `replace cc/civ6-ci-contract => ../civ6-ci-contract` を統合済み。
  ホスト用exeとゲスト用ELFは同じsourceから通常ビルドする（hookのCheckEventはゲスト内で動く）。
- 親のコードは任意の command / callback を受け取らない。親へ渡す `ReadAPI` は GET 専用で、次の許可リストだけを読める
  （`api.go` の `readAllowed`）。親の `Verify` が別の endpoint を読む場合は、ここへ追加して配送する。
  `repositories/1392152644`、`repos/AutoDevJapan/civ6-banseiki/` 配下の `pulls/N`・`git/commits|trees/<sha>`・`git/ref/heads/<branch>`・
  `contents/<path>?ref=<40hex>`・`actions/runs[/N[/attempts/N]/jobs]`・`actions/jobs/N`・`actions/runners`・`code-scanning/default-setup`・`compare/<sha>...<sha>`。
- 変更系 API は 2 つだけ: runner 登録トークンの POST（RAM のみ。stdin でゲストへ渡し、記録しない）と、自分の一意な runner 名の DELETE。

## 実行の流れ（有限・1 回限り）

1. Plan 検査（形・kind・親 `Validate`・ラベル）→ 所有ルート確認 → ELF/pwsh ハッシュ確認 → 親 `Verify`。
2. Rust のみ: `Plan.WorkflowSHA` のコミットから workflow を取得し、`WorkflowDigest` と一致するバイト列から実行方針を導く
   （rustup 1 行 + 許可された cargo 行だけ。未知の cargo/rustup 行は代替せず拒否。doctest 行は任意）。
3. 単一 worker のロック `%TEMP%\civ6-ci-linux.lock`（O_EXCL）→ 新 namespace を作成（意図を先に fsync）。
4. **実 VM の MemAvailable ≥ 予算 + 1 GiB、host の空き物理メモリ ≥ 1 GiB、受領ドライブ空き ≥ 4 GiB** を確認（不足は待たずに拒否）。
   予算: Rust 10 GiB（swap なし）、CodeQL 8 GiB（swap なし）。2 CPU・512 PID。
5. Rust: 公式 pwsh と固定 source を stdin だけで投入。source は GitHub API の tree/blob/commit と全バイト一致を確認（PR の merge context ではなく実 head）。
6. 親 `Verify` → キュー検査（同ラベルを他 run が使っていない、同 run 内では対象ジョブが最古の queued）→ 既存 runner に同ラベルなし。
7. 登録トークン → ゲストの `guest-runner` が公式 `config.sh --ephemeral --disableupdate` → listener。
   hook は `CheckEvent`（実 env と event payload）→ ホストの `VerifyAssigned`（この一意な runner 名に割り当て済み）→ 通過した場合だけ進む。
8. Rust の Cargo/Rustup は guest の `cargo`/`rustup` ラッパーからホストの callback（nonce・Plan 一致・固定 argv・1 回限り）へ。
   ホストは **既存の `cargo_gate.exe`** を保持して実行し（同じ target 排他・CPU/RAM 判定・並列 1）、guest 側は seccomp + Landlock の
   PID グループ配下で実行し、終了を PID 0 まで確認してからゲートを離す。
9. 終了後: hook 証跡（start/complete が同じ Plan）→ source 再確認 → 親 `RequireSuccess`（最大 30 回・1 秒間隔）。
10. 回収: callback server 停止 → namespace を stop（PID 0 確認）→ `rm -v` → 自分の runner 名の登録削除と不在確認 → ロック解除。
    namespace が消えていない、または登録の不在が確認できない場合は **ロックを残す**（キュー担当は「占有中」と扱い、別 worker を起動しない）。

## 受領フォルダの中身（すべて `*.private.*`、コミットしない）

受領フォルダの `flight.lock` はcontrollerが回収終了まで保持するkernel byte lock。
`status <owned-root>` は元のcontrollerがこのlockを保持していれば拒否し、終了していれば
固定intent・Dockerの固有namespace・登録・global lockを読み取って `clean` を返す。
`collect <owned-root>` は同じ証跡とkernel lockの取得を要求し、残った所有namespaceだけを
停止・PID0照合・削除し、その固有runner名だけを登録解除する。sourceが進んだ場合も回収する。
各stop/remove/registration DELETEはfsyncしたO_EXCL記録で1回だけに制限し、
拒否・不確かな結果は再試行せずfenceを保持する。既に消えた資源は再操作せず不在を観測する。
回収はCIの成功を作らず、元run/jobだけを結果へ記録する。

`intent` / `namespace-intent` / `registration-intent` / `official-listener.private.log`（原ログ全文）/ `hook-start|complete` /
`cargo-<idx>-<ts>.private.log|json`（原ログ全文 + SHA + 保存完了）/ `gate-<nonce>-<idx>` / `result.private.json`（ロック取得後は成功・失敗とも必ず保存。
`AllRequiredChecksSuccess` は親の success 確認後だけ true）。会話へ生ログを出さない。受領フォルダは `safe-delete plan` → 通常削除 → `finish` で回収する。

## 境界（変更しないもの）

source 取得・runner 登録・製品 Cargo・CodeQL・route 変更は、この module では **credential-free fixture だけ** を実行した。
既存の `tools/product-ci`・`tools/pr148-windows-runner`・2736 の候補と台帳・稼働中 runner（Linux27/Windows28）は変更していない。
Docker socket・host bind・privileged・GPU は作成引数にも検査にも存在しない（`docker_test.go`）。

## 検証

- `go test -race ./...`、`go vet ./...`（Windows ホストと `GOOS=linux GOARCH=amd64`）、`go build -trimpath .`
- `CC_CIL_RECOVERY_FIXTURE_DIR` を新safe-delete所有rootにすると、
  `go test -run '^TestRecoveryRealOwnedNamespace$' -v -count=1` が実namespaceの中断相当から
  readonly status→所有collect→繰返し不在観測を確認する。runner/source/Cargo/GPUは使わない。
  親2962はこのfixture成功と実namespace消失、safe-delete finishを確認済み。
- `-tags civ6contract` の橋渡し検査（親 module を `-modfile` で差し替えて実行）
- 実 Docker の credential-free 検査（`CC_CIL_FIXTURE_DIR` に新しい safe-delete 所有フォルダを渡す）:
  `go test -run Lifecycle -v .` が normal / cancel / receipt-loss の 3 名前空間で、実 `cargo_gate.exe`・Landlock 境界・
  公式 listener の起動確認・実子プロセスの PID drain・名前空間の削除を確認する。
- 未実施（親の統合後に実行）: 実 source 取得、実 runner 登録、公式ジョブの hook 実イベント（`GITHUB_EVENT_PATH` が start hook 時点で読めるか）、
  製品 Cargo 全検査、CodeQL 2 言語、PR149 の正式 6 ジョブ。
