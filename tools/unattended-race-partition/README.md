# 1つのGo packageのrace testを2分割で実行する

`unattended-race-partition` は、1つのGo packageのtest binaryを1回だけcompileし、
そのbinaryが列挙した全topcaseを、実行順を保った排他的で完全な2つのpartitionに分けて順に実行する。

`unattended-runner` は1 packageに700件を超えるtopcaseを持つ。hosted Windowsでのrace testは
直近の成功runで416〜579秒かかり、600秒のpackage期限を超えた3回は、それぞれ別のtestで、
そのtestの開始から4〜14秒の時点で止まった。特定のtestのdeadlockと断定できる証拠はなく、
package全体の時間予算が足りないので、test・subtest・各test内の期限を変えずに、
同じbinaryのまま期限をpartitionごとに分ける。

## 仕様

1. `-dir` で `go list ./...` と `go list .` が同じ1 packageを返すこと。別のpackageがあると実行しない。
2. 専用の一時directoryへ `go test -c -o`（既定で `-race` 付き）で1回だけcompileし、SHA-256を記録する。
3. 同じbinaryの `-test.list .*` の出力を一覧とする。Test・Fuzz・Exampleを出力順（実行順）に対象とし、
   `-bench` なしの `go test` と同じくBenchmarkは実行しない。Goのcase名でない行、重複、
   改行で終わらない出力、対象0件は失敗とする。
4. 前半の `ceil(n/2)` 件をpartition 1、残りをpartition 2とする。partition 1には、名前を
   `regexp.QuoteMeta` でescapeした完全一致のpattern `^(?:A|B)$` を `-test.run` で渡し、
   partition 2には同じpatternを `-test.skip` で渡す。一覧に出ないcaseがあってもpartition 2で開始し、
   計画外として報告されるので、黙って欠落しない。
5. 各partitionを `-test.paniconexit0 -test.v=test2json -test.timeout=<期限>` で、package directoryから
   環境変数を引き継いで実行する。期限は `-partition-timeout` と、`-suite-timeout` の残りから
   停止猶予1分を引いた値の小さい方。`go test` と同じく、期限から1分過ぎても終わらないbinaryは停止する。
6. 一覧の取得後と各partitionの前後で、binaryのSHA-256がcompile直後と同じことを確認する。
7. test2jsonの枠行（先頭が `^V` の `=== RUN`）から開始したtopcaseを集め、partitionごとに計画との
   欠落・計画外・重複が0で、両partitionの合併が一覧と一致し、交差が0であることを確認する。
8. 片方が失敗しても両方を実行する。失敗があればexit 1、引数の誤りはexit 2。

## 受入条件

| 条件 | test |
| --- | --- |
| 全caseを1回ずつ実行し、subtest・Fuzz seed・Exampleを保ち、Benchmarkは実行しない | `TestRunPassesWithBothPartitionsOfOneBinary` |
| 一方が失敗しても両partitionを実行し、失敗を返す | `TestRunRunsBothPartitionsAfterAFailure` |
| partitionの期限でGoのtimeout traceを出して失敗し、未開始のcaseを示す | `TestRunReportsAPartitionDeadline` |
| test中の `os.Exit(0)` を成功にしない | `TestRunTreatsOSExitAsAFailure` |
| 一覧に出ないcaseは計画外の開始として失敗する | `TestSecondPartitionReportsACaseMissingFromTheCensus` |
| compile後に差し替えたbinaryは実行しない | `TestPartitionRefusesABinaryReplacedAfterTheCensus` |
| 対象0件・複数package・不正な一覧・2件未満・長すぎるcommand lineで失敗する | `TestRunFailsClosedWithoutRunnableCases`、`TestRunFailsClosedForSeveralPackages`、`TestParseCensusFailsClosed`、`TestPlanPartitionsNeedsTwoCases`、`TestCheckCommandLineFailsClosedAboveTheLimit` |
| patternは完全一致でmetacharacterをescapeし、分割は排他的で完全 | `TestExactNamePatternEscapesMetacharacters`、`TestPlanPartitionsIsExclusiveCompleteAndOrdered`、`TestVerifyCoverageNeedsUnionAndNoIntersection` |
| partitionの期限をsuite全体の期限内に収める | `TestEffectiveTimeoutStaysInsideTheSuiteDeadline` |

## 使い方

```
go run . -dir ../../unattended-runner -partition-timeout 600s -suite-timeout 22m
```

`.github/workflows/unattended-readiness.yml` は、Windowsの `unattended-runner` だけでこの手順を使う。
Linuxとほかのmoduleは従来どおり `go test -race ./...` を実行する。
