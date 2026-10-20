# civ6-ci-queue（ToDo2962の独立CIキュー）

対象は AutoDevJapan/civ6-banseiki（repo ID 1392152644）だけ。リポジトリ名・IDはコードの定数で、
設定やGitHubの値からは変えられない。既存の無人runner（unattended-runner）の通常tickが、
設定済みのときだけ `civ6-ci-queue.exe tick --config <path>` を1回呼ぶ（新しい常駐監視・定期タスクは無い）。

## 1回のtick（最大約12秒、runner側は15秒で打ち切る）

1. reconcile: 記録済みsupervisorのPID＋作成時刻を確かめる（PIDだけでは生存とみなさない）。
   supervisorが結果ファイルを残していれば台帳へ取り込み、無ければOS単位のfenceを置いて新workerを止める。
   外部への問い合わせは行わず、fenceは結果ファイルかparentのhelper復旧modeでだけ解除される。
2. launch: 予約済みで未起動のjobを、OSごとに最大1件、固定の自分自身（`worker --entry`）としてCREATE_NO_WINDOWで切り離して起動。
3. scan/admit: queued/in_progressのofficial runから、同じrepoの現在のPR/main、三workflow＋CodeQL 2 jobだけ
   を固定Planにし、親の `Verify` を通ったものだけを台帳へ一度だけ予約する（予算: PR149 6件、通常push 6件）。
4. もう一度launch。

重いjobはtick本体で待たない。workerは自分のOSロックを持ち、OS別adapter（下記）でhelperの手順を一度だけ実行し、
公式のjob成功（`RequireSuccess`）を別に確認する。helperの終了コードだけでは成功にしない。失敗jobを自動rerunしない。

## 記録（台帳 ledger.json）

queued / started / official / cleanup を別の項目として保存する。予算の予約は台帳作成と同じ原子的書き込みで一度だけ。
同じrun/attempt/jobは2度登録されない。拒否は10分（5回以上で6時間）のbackoff。予算超過は台帳の外に5分のbackoffだけを残す。
12件の後の自然継続は `continuation.enabled` と親がreview済みsourceのcommitを設定したときだけ有効。既定は無効。

## 回収

owned rootは `safe-delete create` で新規作成（既存フォルダの登録はしない）。collect成功後に原log・SHA256を
`evidence/<run-attempt-job>/` へ複製・照合し、manifestのSHA256を台帳へ保存してから、`safe-delete plan` の対象と
台帳のrootが一致する場合だけリンクを含まない通常ディレクトリを削除して `finish`。削除が拒否されたら再試行せず `quarantine`。
作成・停止・回収が不確かなら `needs_recovery` とOS fenceを保存する。

## 固定する外部実行物

gh（既存の `gh_gate.exe` など）、safe-delete、OS別helperは設定内の絶対パスとSHA-256 pinだけを受け付け、起動のたびにpinを再照合する。
入力・GitHub・台帳から実行ファイル、引数、コマンド、callbackを受け取らない。GitHub読み取りはGETのみで、repoの固定エンドポイントの許可リスト外
（secrets、logs、artifacts、runners等を含む）を拒否する。登録tokenはRAMだけで、helperのstdinにのみ渡す。

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
  `execute <root>`（登録tokenはstdinのみ）と `stop` は root/helper.exe（同じSHA-256のpin）を管理者なしで実行。
  `execute-end` が無い場合だけ stop を1回試みる。終端eventがある場合はhelper自身の停止処理を重ねない。
  collectは必ず行い、brokerの終了コードではなく `collect-end{complete:true}` だけで回収を認める。
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

