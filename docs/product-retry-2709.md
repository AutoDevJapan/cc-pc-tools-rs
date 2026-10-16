# 元1626の期限付き再開を既存の1分巡回へ接続する（ToDo2709）

元1626は再試行の時刻を過ぎても、製品担当が再開されなかった。Go巡回はPC修復3プロジェクトだけを扱い、通常schedulerはdispatchable:falseの元1626を選ばない。資格保存やwait解除だけでは起動まで接続されなかった。

## 接続

`retry.go` は元1626の期限内候補を、従来の修復候補と同じ保存カーソル・1回4件の巡回へ入れる。既存の10秒の再試行処理枠を使い、別loopやtaskを作らない。元1626を一般のresume-readyへ渡さない。

`product_retry.go` の副作用なし候補判定は、期限・停止・判断待ち・未完依存・保存資格・元担当の一致を確認する。同じwaitのsince/not_before・元spec SHA・元worktreeからBASEキーを固定する。候補は実行の許可ではない。

`product_retry_dispatch.go` が配置済みの正式APIを呼ぶ。

1. `todo.exe product-retry-reserve 1626 --key BASE --expected-worker 観測担当`。既存verify-product-resume guardとqualificationの核を使い、実PID・最新session・stop・依存・本文・元snapshotを正式APIの共有lock内で再照合する。元waitは残る。
2. 返却run_executableとrun_argsを、そのまま引数配列で実行する。固定の標準CLIであることと元ToDo・launchキーの対応を確認する。BASEを返却EFFECTIVEキーへ取り違えず、session/model/cwd/scopeをGoで再構築しない。
3. 実jobIDだけの応答を確認し、`todo.exe product-retry-bind 1626 --key EFFECTIVE --job 実job`を呼ぶ。結合が保存されるまで実モデルの起動は通常queueにより保留される。

continued_existing:trueなら、その同じqueued担当を通常queueへ任せる。run/bindを追加で呼ばない。reserved/boundのlaunchedは0で、受付や結合を実モデル起動と区別する。

応答喪失・busy・期限到来は既存巡回の次回へ戻る。同じBASEで正式APIが同じ意図と実jobを回収する。終端した未結合試行を更新するかはAPIが決め、試行0/1/2の上限もAPIが維持する。Goは別キーを作って再起動しない。

元の正式意図が、原snapshot・hash・actualsession・jobを保持する。Goの`product-retry-observation.json`は最終の受付/結合/保留の観測だけであり、起動権限や第二の台帳として使わない。

## 範囲

元1626のみ、既存guardの資格、元spec SHA、元worktree、2026-10-21 00:00 UTCより前を保持する。dispatchable:false、元model/effort/sandbox/profile/network/add_dirsと通常queueの起動間隔・CPU/RAM・target/resource・優先順位を保つ。

原資格sessionは最新の実sessionと別の証拠。資格の古いsessionを再開先に渡さない。元2475、新Suno依頼、他ToDo、別製品、旧質問、名簿やguard例外へ広げない。

## 検査

Goの架空DBと偽CLIで以下を確認する。実Rust APIのロック・PID・queue検査の代わりとはしない。

- 期限前・失効・stop・block・未完依存・live/busyのとき起動しない。
- 正式APIの返却配列と最新sessionを保持し、結合後も原spec/needs/note/dispatchableを保持する。
- 受付後の応答喪失、stdoutへのログ混入、結合応答喪失から、同キーの1job・1世代・1回の偽モデル起動へ収束する。
- 受付後の本人stop・新依存・段階間の失効で、元waitを維持し未結合モデル起動は0。
- EFFECTIVEキーのrotateとcontinued_existingを扱い、別exe・別ToDo・壊れた応答は実行しない。

WindowsのGo race/vet成功。親APIのPR186では固定b08fcd0の全workspace2248PASS/5skip・doctest・Clippy・CodeQL・関連検査成功。ここで同じRust全件検査は重ねない。

## 稼働版の来歴

既存CC-DiscordAskTickは同VBSから `D:/CC/Tools/discord-ask/unattended-runner.exe` を実行する。配置前SHA256はB3B4CB60C71B9037F7A12466B8AA92D6356F0A96DE91A7116A3EA35C192A405C。元2488の保存配備記録と一致する。

稼働版のGo情報は1.27.0/windows/amd64/trimpath、VCS0808120/modified:true。0808120に、その直後の91edbbaのrunner.go差分だけを当てて同じGoで再ビルドした。binaryはGo build IDの1箇所だけが異なり、それ以外の全バイトを同一と照合した。build ID部分を同じ値にしたSHA256は双方9FC6654A7D0FE1D5BAA7B9AB9E25C155851AEB327852E6F6E6C5CECCB1452955。元ソースの内容はこの照合で確認し、元binary全体のSHAが再ビルドと一致したとは書かない。

同runtimeの通常配置は、固定ソースのレビュー・CI・merge後、元SHAと既存runner.lockを再照合しrollbackを保持して行う。guardの配置先に対する保護・分類未確認の結果は、そのまま記録する。通常実行審査の拒否を別名exe/task/loopで迂回しない。

ソース成功、配置成功、自然dueの実再開成功は別の受入条件。元1626の既存稼働中担当を止めず、自然dueの実job・実ツール・保存・二重起動0を確認するまで本件をdoneにしない。
