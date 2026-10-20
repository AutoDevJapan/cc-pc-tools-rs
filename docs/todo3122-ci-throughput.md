# ToDo3122: Linux CIの実待機を解消する

通常Linux runnerの単一枠に、CodeQLと他の正式検査が並ぶ。PR286のActions解析は
2026-10-20 12:45:46Zに作成され、14:13:52Zにrunner27を取得した。待機88.1分、
実行56秒で成功した。この自然な割当を追加是正の効果には含めない。

## 範囲と受入条件

- 既存通常runnerを停止せず、既存PCの測定した余力内で追加枠を1台だけ提供する。
  通常と追加枠でcontainer、bridge接続先、runner名、hostのCargo targetを分離する。
- 同じrepoの現在headだけを受け付ける既存hook、OAuth、Cargoゲート、process-groupの
  排出、診断保持、UID1001/init/capdrop/no-new-privileges/tmpfsの制限を維持する。
  外部fork、他repo、古いhead、任意の追加枠は受け付けない。
- 追加枠はコンパイル時に固定するprofileで選ぶ。workflowやジョブの環境変数から
  接続先を変えない。許可するprofileは従来のprimaryと、この作業のburst-3122だけ。
  同じ旧ラベルを提供し、既存キューのjobを再発火せず受け取る。
- 追加枠は4CPU/8GiB/1024PID、別のworktree/targetと1ジョブ限りのephemeral登録を使う。登録前に
  PCとDocker VMの空きRAM/CPUを測り、停止時は自分のrunnerをidleにして排出し回収する。
  稼働中の検査を取消さない。必要な回数だけ原キューと余力を再測定して起動し、
  1回の受入/排出後にhostも終了する。無制限のrunner/常駐を増やさない。
- PR296などの現在headをAPIで取り直す。jobのcreated_at、runner割当、実開始、
  完了、conclusionを区別し、追加runnerが実際にcurrent-head jobを完走した証拠を残す。
  sourceの変更・登録・Listener起動だけでは完了にしない。
- 別モデルの固定差分レビュー、関連Go検査と必要CI、PR/merge、実運用の証拠を確認する。
  原製品のmerge/配布は元担当が行う。顧客repoと他担当の本体を変更しない。

## 時刻の扱い

GitHubの日時はUTCの元文字列を保持する。PowerShellはConvertFrom-Jsonの
`-DateKind String`とDateTimeOffsetを使う。上の既知2時刻で88.1分になることを
検算し、queueにあるjobのstarted_atを実開始として扱わない。

一次資料: [GitHubのrunner割当](https://docs.github.com/en/actions/reference/runners/self-hosted-runners)。
同じlabelsを持つonline/idle runnerが割当対象であり、1台のrunnerは同時に1jobを実行する。

## 有限の再供給

既存bridgeの`pool TARGET`入口を、Windowsの同じ定期タスク管理方式で実行する。
1回のpoolは最大16回、入場する時間は2時間まで。1回ずつ本物のephemeral jobの
終了と排出を待ち、次の現在headの未割当キューと資源を再取得する。成功済みjobを
再実行する操作はない。原検査の成功はGitHubの個別job APIで別に確認する。

hostは空きRAM24GiB以上（追加8GiBを使っても16GiB残す）、commit85%未満、
CPU75%未満、C/D各30GiB以上を要求する。Docker VMは空き9GiB以上を要求し、
追加8GiBと残り1GiBを確保する。不明/不足/API失敗時はその回を終了する。
タスクのIgnoreNewとpool専用loopback portの保持で管理処理の重複を防ぐ。
子hostも独立portを保持してから登録する。追加profileは`--replace`を使わない。

タスクの反復は5分ごとに同じ入口だけを起動する。待機がなければ登録せず終了する。
PCのOS再起動/ログオン後の復帰は未検証のままとし、実検証なしに保証しない。
追加containerはこの管理が使う継続資源と、検査終了後に回収する資源を区別する。
