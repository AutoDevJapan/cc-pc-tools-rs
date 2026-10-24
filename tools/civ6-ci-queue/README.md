# civ6-ci-queue（ToDo2962の独立CIキュー）

対象は AutoDevJapan/civ6-banseiki（repo ID 1392152644）だけ。リポジトリ名・IDはコードの定数で、
設定やGitHubの値からは変えられない。既存の無人runner（unattended-runner）の通常tickが、
設定済みのときだけ `civ6-ci-queue.exe tick --config <path>` を1回呼ぶ（新しい常駐監視・定期タスクは無い）。

## 1回のtick（最大約12秒、runner側は15秒で打ち切る）

1. reconcile: 記録済みsupervisorのPID＋作成時刻を確かめる（PIDだけでは生存とみなさない）。
   supervisorが結果ファイルを残していれば台帳へ取り込み、無ければOS単位のfenceを置いて新workerを止める。
   外部への問い合わせは行わず、fenceは結果ファイルかparentのhelper復旧modeでだけ解除される。
2. launch: 予約済みで未起動のjobを、OSごとに最大1件、固定の自分自身（`worker --entry`）としてCREATE_NO_WINDOWで切り離して起動。
3. scan/admit: queued/in_progressのofficial runから、同じrepoの現在のPR/main、三workflowのWindows 3 jobだけ
   を固定Planにし、親の `Verify` と登録候補の全ラベル包含検査を通ったものだけを台帳へ一度だけ予約する（最初3件・通常push 3件）。
   Linuxのアプリjobと標準hostedのCodeQLはPC側の受付・供給をしない。
4. もう一度launch。

重いjobはtick本体で待たない。workerは自分のOSロックを持ち、OS別adapter（下記）でhelperの手順を一度だけ実行し、
公式のjob成功（`RequireSuccess`）を別に確認する。helperの終了コードだけでは成功にしない。失敗jobを自動rerunしない。

## 記録（台帳 ledger.json）

queued / started / official / cleanup を別の項目として保存する。予算の予約は台帳作成と同じ原子的書き込みで一度だけ。
同じrun/attempt/jobは2度登録されない。拒否は10分（5回以上で6時間）のbackoff。予算超過は台帳の外に5分のbackoffだけを残す。
PCの計6件の後の自然継続は `continuation.enabled` と親がreview済みsourceのcommitを設定したときだけ有効。既定は無効。
別途、PR/merge後のCodeQL計4解析と移行用最大2解析を含め、有限受入は合計12件以内で記録する。

## 回収

owned rootは `safe-delete create` で新規作成（既存フォルダの登録はしない）。collect成功後に原log・SHA256を
`evidence/<run-attempt-job>/` へ複製・照合し、manifestのSHA256を台帳へ保存してから、`safe-delete plan` の対象と
台帳のrootが一致する場合だけリンクを含まない通常ディレクトリを削除して `finish`。削除が拒否されたら再試行せず `quarantine`。
作成・停止・回収が不確かなら `needs_recovery` とOS fenceを保存する。

## 固定する外部実行物

gh（既存の `gh_gate.exe` など）、safe-delete、OS別helperは設定内の絶対パスとSHA-256 pinだけを受け付け、起動のたびにpinを再照合する。
入力・GitHub・台帳から実行ファイル、引数、コマンド、callbackを受け取らない。GitHub読み取りはGETのみで、repoの固定エンドポイントの許可リスト外
（secrets、logs、artifacts、runners等を含む）を拒否する。queueは登録tokenを取得しない。Windowsのgate子が監査後に既存OAuthで1回取得し、RAMと検証済みpipeだけで渡す。

## 検査

```
go test -race ./...
go vet ./...
go build ./...
```

共通contractはgo.modのrequire/replaceで通常ビルドへ接続する。追加のbuild tagは不要。
未接続なら受け付けない。

## OS別adapter（各helperの実CLIに合わせた固定手順）

共通: workerは新しいowned rootを作ってからadapterを一度だけ呼び、adapterは「回収を確認できたか」「helper自身の成功」「公式runner名」を返す。
公式成功は、その後に親の `RequireSuccess`（固有runner名）で別に確認する。helperの終了コードだけでは成功にしない。

- Windows（tools/civ6-ci-windows）: pin済みexeを root/bootstrap.exe へ複製し、plan.json（共有Plan＋instance/root/runner pin/controller SID/seconds）を root に保存。
  `prepare <root> <runner.zip>` と `collect|collect-unstarted <root>` はCC-AdminBroker（固定形のps1を生成）経由、
  `execute <root>`（credential入力なし）と `stop` は root/helper.exe（同じSHA-256のpin）を管理者なしで実行。
  execute全体は親が保持するJob Objectへ生成時に所属させ、期限後はgateを含む全子孫の終了を保持handleとActive=0で確認する。
  `execute-begin` があり `execute-end` が無い場合だけ stop を1回試みる。終端eventがある場合はhelper自身の停止処理を重ねない。
  プロセスツリーの終了が未確認なら回収を始めずfenceを保持する。終了を確認した場合だけ回収し、broker呼出しが成功し `collect-end{complete:true}` がある場合だけ回収を認める。
  helperがservice未開始を明示した場合、またはbeginが無く全子孫の終了を確認できた場合は、初めからcollect-unstartedを使う。通常collectの拒否後に切り替えない。
  固定serviceとその1件のprofileは継続用資源。`shared_profile_retained` はjobごとの残留ではない。
  旧helperの `complete:false,residue_only:true` は回収成功として受け付けない。回収不明時は次のWindows workerを止める。
- Linux（tools/civ6-ci-linux）: one-shot `civ6-ci-linux.exe run <plan-json> <新しい空のowned root> <guest ELF> <pwsh tar>`。
  plan-jsonはroot外（entry directory）。登録tokenはhelperが自分で取得するので、queueは供給しない。
  helperが残す `result.private.json` から Namespace.Removed・RegistrationGone・LockReleased を回収確認とし、Namespace.Name を固有runner名にする。
  receiptが無く intent も無い場合だけ「資源を作る前の拒否」とみなす。

## 未確定のまま残す点

- Linux helperにはstatus/collect復旧modeがある。queueの不明なfenceは自動解除しない。
- 親の実イベント（GITHUB_EVENT_NAME/ref）は最初の正式job後に記録する。
- 継続運用（`continuation.enabled`）は、親がreview済みsourceで配備するまで無効。

## 設定例（配備は親）

```json
{
  "version": 1,
  "home": "D:\\CC\\State\\civ6-ci-queue",
  "gh": {"path": "D:\\CC\\Tools\\codex-jobs\\gh_gate.exe", "sha256": "<64 hex>"},
  "safe_delete": {"path": "D:\\CC\\Tools\\safe-delete\\safe-delete.exe", "sha256": "<64 hex>"},
  "admin_broker": {"path": "D:\\CC\\Tools\\admin-broker\\run-as-admin.ps1", "sha256": "<64 hex>"},
  "helpers": {
    "Windows": {"exe": {"path": "...", "sha256": "..."}, "seconds": 3600, "runner_version": "2.338.0",
                "runner_digest": "<64 hex>", "controller_sid": "S-1-5-21-...",
                "runner_archive": {"path": "...", "sha256": "<same runner digest>"}},
    "Linux": {"exe": {"path": "...", "sha256": "..."}, "seconds": 3600,
              "guest": {"path": "...", "sha256": "..."}, "pwsh_tar": {"path": "...", "sha256": "..."}}
  },
  "continuation": {"enabled": false}
}
```


正式job未開始の専用collectorが成功し、証拠保存とowned rootの回収が済んだ場合だけ、同じ予約を5〜40分のbackoffで再供給する。実行済みの正式jobは自動再実行しない。各未開始回の証拠は別ディレクトリへ保持し、予算キーを重複させない。

未開始の延期は6回到達または予約から24時間で打ち切り、needs_recoveryとOS fenceを保存する。各回の理由・時刻・回収証拠を台帳へ追記する。未起動collectorの成功だけでは再供給しない。期限・queue busy・明示された一時的通信障害だけ再供給し、credential拒否/形不正、監査不一致、helper生成・pin検証の失敗はfailedとして保存する。正式jobの再実行には使わない。

## 未実行sourceの移行（ToDo3576）

`migrate-unexecuted-main392 --config <path> --entry <旧key> --replacement <新run/1/job>`
は固定旧3枠から固定main合流世代へ一対一で受付する。旧予算・期限・履歴を保持し、
`unexecuted_source_migrations`へ別のproof関係を記録する。
旧replace-expiredを別headに流用しない。通常tickは移行やrerunを生成しない。
実台帳操作は親2962だけが担当する。
詳細は[移行手順とschema](../../docs/todo3576-source-migration.md)を参照。
