# ChatGPT app recovery observer

Windows専用の独立したGoモジュールです。登録済みAUMID
`OpenAI.Codex_2p2nqsd0c76g0!App` を現在ユーザー・現在の対話セッションに限定して
読み取り、純粋な判定と永続状態を検査します。対象ホストはMinerPCです。
既存runnerへの接続、配布、常駐、定期実行、実アプリの自動復旧は含みません。

## 実行

Go 1.27の標準ライブラリだけを使います。外部依存の取得は不要です。

```powershell
# 説明のみ。OSの観測も状態の書込みも行わない。
go run ./cmd/observer

# 明示した1回だけのOS観測。状態は既存のTemp配下か、作業用Gitの
# .ci-work配下にある絶対パスのJSONへ保存する。親フォルダは事前に用意する。
go run ./cmd/observer observe-once --state <scratch-directory>/observer.json

go test -race -timeout 120s ./...
go vet ./...
```

観測には10秒の期限と、有限のプロセス走査・最大32イベントの上限があります。
APIの呼出しが期限を超えて戻った場合も候補を要求へ進めません。
CLIに実起動を有効にする引数・設定はありません。出力には必ず
`live_activation_enabled=false` と `live_activation_calls=0` を含めます。
現在の停止指示源は未接続であり、ネイティブ終了意図も常に不明です。
通常のPC操作や、サインイン・アプリ終了などを本人に求めません。

## 観測と判定

- Windowsの現在ユーザー向けパッケージ登録、登録manifestのApp、実プロセスの
  AUMID・package・SID・session・実行画像・作成時刻を照合します。
  同名の別アプリやコマンド実行用の子プロセスを対象の代用にしません。
- プロセスはpresent / absent / unknownの3値です。アクセス拒否やPIDの再利用は
  不明になります。直前のconnectedやプロセス不在、CLIの認証状態から
  「本人は停止していない」という証拠を作りません。
- 初回はApplicationの最新レコードの番号と時刻の指紋だけを基準として保存します。
  次回は同じ基準レコードの指紋を確認し、その後かつ5分以内のApplication Hang
  1002だけを最大32件読みます。初回の過去再生や古い履歴の全走査を行いません。
  レコードの消失・再利用、epoch変更は不明として保持します。
- 1002はハング候補です。package/AppとPID・作成時刻が、過去に同じユーザーと
  sessionで確認したプロセス世代と一致し、そのPID世代の実不在も確認できた場合
  だけ異常終了候補にします。同一世代の別レコードは同じincidentにまとめます。
- 本人停止・延期・キャンセル・Quit・サインアウトを優先します。停止cursorが
  不明でも要求しません。停止generationの変更と新プロセスは旧候補を失効させます。
  時計逆行・イベント時刻異常・ログreset・対象不整合は永続的な不明状態とし、
  時間経過だけで解除しません。破損した状態を空の正常状態に置き換えません。

## 保存と注入境界

`Decide` / `Advance` は純粋な処理です。`Engine` は時刻、プロセス、イベント、
停止指示、保存を注入して1回で返します。`FileStore` はWindowsのカーネルロックを
待たずに取得します。利用側はLoad→判定→Saveの間、同じロックを保持します。
ロックはプロセス消失時もOSが解放します。保存は同じフォルダの一時ファイル、
flush、close、置換とwrite-throughを順に行い、内容のSHA256も検査します。

要求前にincidentの消費を永続化します。保存後の結果が不明、要求前にrunnerが消失、
模擬要求が失敗した場合も同じincidentを再要求しません。これは最大1回の要求抑止で
あり、exactly-onceの起動成功保証ではありません。保存結果が不確かなときは
`consume_state=unknown` を返します。実OSへの起動手段は実装していません。

同一incidentは最大1回、別incident間は10分、rolling 1時間では最大3回です。
事前の読取再確認は最大2回・3分以内で、次回時刻を状態と結果に保存します。
独自loopやsleep監視はありません。抑止や結果不明を時間だけで解除しません。

保存・出力するのは対象/事象/プロセス世代の指紋、cursor、時刻、段階、理由、
消費状態と試行数です。イベントの生message、全プロセスのcommand line、
認証DB・認証値・私的ログは読み取り対象にしません。

## 検査の範囲

`testdata/cases.json` は32件の合成受入条件と必要なdefaults/limitsだけです。
`synthetic_no_stop_confirmed` は検査専用の仮定で、MinerPCの観測事実ではありません。
通常の`Tick`にはこの仮定を作る入口がありません。模擬要求を数えるadapterは
`_test.go`だけに存在し、positive caseでもlive OS activationは0です。

32ケースに加え、同時呼出し、要求前後の保存故障、保存後のrunner消失、停止競合、
PID再利用、状態reload、初回baseline、時刻/回数境界、CLIの有限性を検査します。
独立した検査がproductionのimportとnative API bindingを調べ、起動APIが存在しない
ことと、positive fake caseが実際に消費後の境界へ到達することを確認します。

最初に無条件で候補とする仮実装で32件を実行し、抑止ケースの失敗を確認しました。
その後の実装で32件をGreenにしました。実アプリのkill、Quit、サインアウト、
認証、実起動による障害試験は実施しません。既存workflow・共有runtimeも変更しません。

## Windows一次資料

登録とプロセスidentityは[Package query API](https://learn.microsoft.com/en-us/windows/win32/appxpkg/functions)、
有限イベント読取は[EvtQuery](https://learn.microsoft.com/en-us/windows/win32/api/winevt/nf-winevt-evtquery)と
[EvtCreateRenderContext](https://learn.microsoft.com/en-us/windows/win32/api/winevt/nf-winevt-evtcreaterendercontext)、
排他と置換は[LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex)と
[MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw)を参照します。
これらのAPIの存在を、ネイティブQuit/サインアウト意図を判定する契約の証明にしません。
