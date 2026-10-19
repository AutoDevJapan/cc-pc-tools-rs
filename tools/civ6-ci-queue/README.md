# civ6-ci-queue（ToDo2962の独立CIキュー）

対象は AutoDevJapan/civ6-banseiki（repo ID 1392152644）だけ。リポジトリ名・IDはコードの定数で、
設定やGitHubの値からは変えられない。既存の無人runner（unattended-runner）の通常tickが、
設定済みのときだけ `civ6-ci-queue.exe tick --config <path>` を1回呼ぶ（新しい常駐監視・定期タスクは無い）。

## 1回のtick（最大約12秒、runner側は15秒で打ち切る）

1. reconcile: 記録済みsupervisorのPID＋作成時刻を確かめる（PIDだけでは生存とみなさない）。
   supervisorが結果ファイルを残していれば台帳へ取り込み、無ければOS単位のfenceを置いて新workerを止める。
   fence中は読み取り専用のhelper `status` が `{"clean":true}` を返し、かつ所有rootが回収済みの場合だけ解除する。
2. launch: 予約済みで未起動のjobを、OSごとに最大1件、固定の自分自身（`worker --entry`）としてCREATE_NO_WINDOWで切り離して起動。
3. scan/admit: queued/in_progressのofficial runから、同じrepoの現在のPR/main、三workflow＋CodeQL 2 jobだけ
   を固定Planにし、親の `Verify` を通ったものだけを台帳へ一度だけ予約する（予算: PR149 6件、通常push 6件）。
4. もう一度launch。

重いjobはtick本体で待たない。workerは自分のOSロックを持ち、helperの `prepare` → `execute`（登録tokenはstdinのみ）→ `collect` を順に一度だけ実行し、
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

親のcontractをリンクするには `-tags civ6contract` と、go.modへの `require/replace cc/civ6-ci-contract => ../civ6-ci-contract` が必要（親の統合で追加）。
タグ無しのビルドは `unwiredContract` で何も受け付けない（fail-closed）。

## helperへの要求（未確定のまま固定したadapter）

- 動詞: `prepare|execute|stop|collect|collect-unstarted|status <owned-root>`、planは `<root>/plan.json`。Linux helperの動詞はWindows helperに合わせる想定。
- runner名: Windows `CC-CIW-<instance>`、Linux `CC-CIL-<instance>`（仮定）。
- `status` は標準出力に `{"clean":bool}`。
- 登録token取得（POST `actions/runners/registration-token`）はgh gate経由。gateが拒否する場合は親が経路を決める。

## 設定例（配備は親）

```json
{
  "version": 1,
  "home": "D:\CC\State\civ6-ci-queue",
  "gh": {"path": "D:\CC\Tools\codex-jobs\gh_gate.exe", "sha256": "<64 hex>"},
  "safe_delete": {"path": "D:\CC\Tools\safe-delete\safe-delete.exe", "sha256": "<64 hex>"},
  "helpers": {
    "Windows": {"exe": {"path": "...", "sha256": "..."}, "seconds": 3600, "runner_version": "2.x.y",
                "runner_digest": "<64 hex>", "controller_sid": "S-1-5-21-..."},
    "Linux": {"exe": {"path": "...", "sha256": "..."}, "seconds": 3600}
  },
  "continuation": {"enabled": false}
}
```
