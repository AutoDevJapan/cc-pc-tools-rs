# 元1626の期限付き再開を接続する（ToDo2709、実装途中）

## 現在の結果

元1626は2026-10-16の実ジョブで稼働中。停止も重複起動もしていない。
Goの候補判定とその検査を追加した。既存の1分巡回への接続、キュー受付、実ジョブの結合は未実装。
このソースを配備しても再開機能は増えない。登録・候補判定を自動再開の成功と扱わない。

根因は `unattended-runner/retry.go` が確認済みPC修復の3プロジェクトしか期限解除せず、通常schedulerが `dispatchable:false` の元1626を選ばないこと。
`todo product-resume` は資格を検証・保存するだけで、起動しない。

## 保持する対象

元1626だけ。既存guardの資格、元仕様のSHA256、元Git作業場所、2026-10-21 00:00 UTCより前という期限を維持する。
元2475、別日付の制作、新Suno依頼、他の製品やToDoへ拡大しない。資格の延長も自動実行の許可の変更もしない。

`product_retry.go` の候補判定は副作用を持たない。元項目のJSONをコピーして保持し、停止・判断待ち・未完依存・期限前・失効を除外する。
保存済み資格は候補の絞込みにだけ使う。実行直前の正式guardと `todo product-resume` の検証を代替しない。
同じ待機の開始時刻・期限・仕様ハッシュ・作業場所から、標準CLI用の32桁キーを固定する。
再送やクラッシュ復旧で新しいキーを発行しない。

原資格のセッションと最新の実担当セッションは別の証拠である。
原資格は保持し、再開先は `worker_receipt.job` と `scheduler.job` が一致する実担当から照合する。
保存された原資格のセッションを無条件に再開先へ使わない。

## 正規APIに不足している処理

2026-10-16のmain `970697ee3ccf3c4e183bf36f0ef197d8f8ee14aa` を確認した。

- `cc-jobs/src/cli.rs` はToDoの共有ロック内で再開条件待ちを確認し、waitを保持したままの受付を拒否する。
- `cc-todo/src/ready.rs` の `resume-ready` は受付前にwaitを削除する。直後に落ちると、元1626は通常schedulerの対象にならず取り残される。
- `cc-todo/src/lib.rs` の通常 `start` は、実ジョブと同一キー・範囲の照合を行わず、直前の本人停止やblockを上書きできる。
- `cc-todo/src/product_resume.rs` は稼働中担当があれば拒否する。受付後のクラッシュからその担当を元ToDoへ結合する経路には使えない。

したがってGoの事前確認→解除→受付→通常startという順では、本人停止との競合とクラッシュによる取りこぼしを防げない。
Goがtodo.jsonを書き換えたり、CLIへToDo番号を渡さずに受付する方法で回避しない。

親2706へ不足を具体的に渡した:
https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/180#issuecomment-6209678407
https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/180#issuecomment-6209747388

必要なのは元1626専用の正式な起動意図と結合のAPI。共有ロック内で原wait、資格、期限、停止、依存、元担当、実session、cwd、仕様を照合する。
意図を永続化し、既存CLIの同じキーで通常キューへ受付し、実jobのキー・元ToDo・session・cwd・仕様との一致を再照合して結合する。
job.jsonとtodo.jsonは別ファイルなので、ロックを保持するだけでは両方を同時に保存できない。
受付後のクラッシュは保存済み意図と同キーの実jobから復旧し、本人停止や新しい依存・waitを上書きしない。
結合前のキュー入場でも停止・依存・資格失効を再確認する。
APIの正式引数と結果が未確定の間は、推測したAPIを本番から呼ばない。

## 稼働版・配置

既存 `CC-DiscordAskTick` は同じVBSから `D:/CC/Tools/discord-ask/unattended-runner.exe` を実行している。
2026-10-16の健康記録はAPI確認成功、scheduler成功、エラーなし。これは製品再開を証明しない。

配置済みGo binaryの埋込み情報:

- Go 1.27.0、windows/amd64、trimpath
- VCS revision `0808120c28350082d2c79d1c5fddb808388de58a`、modified=true
- SHA256 `B3B4CB60C71B9037F7A12466B8AA92D6356F0A96DE91A7116A3EA35C192A405C`

modified=trueのため、そのcommitだけから配置済みソース全体が一致するとは断定できない。
配置先と標準ジョブCLIの `scrub391 --guard-path` は保護・分類未確認を返した。実行版は差し替えていない。
別名exe、別task、新loop、guard例外を作って配備の拒否効果を迂回しない。

## 検査と未達

Goの架空DBで、期限前・失効・本人stop・block・完了・未完依存・資格不一致・元担当不一致・他ToDo除外・原状態保持・同一キーを確認した。
Windowsで `go test -race -timeout 120s ./...` と `go vet ./...` は成功。
これは実キューのcrash復旧、live照合、実job結合の成功ではない。
別モデルの設計確認でも既存API不足を確認。最終差分のMust0、固定HEADのCI、マージ、通常配置、自然に期限を迎えた元1626の実自動再開は未達。
