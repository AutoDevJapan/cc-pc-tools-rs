# 同じPRの修正を再コンパイルせず検査する

Issue267 / 元ToDo3059の残要件。対象は repository1285649124 / PR995。
元4候補、確定939/995の有限batch、旧入力を開かない条件は維持する。

製品担当3056が元の独立helper sourceの `.ci-work` へ
`current-ci-handoff-3056.private.json` を保存する。sourceの場所は既存の正式
locatorの `root` とそのGit originで固定する。元の保存済み仕様の置き場所は
schedulerの保管領域なので、新しい入力の保存先には使わない。
これは本人の新しい許可ではなく、
元の依頼に対する製品担当の確定入力である。任意のcallerのパスや自己申告だけの
pinは受け付けない。元3059/3056の仕様の場所とhash、明示停止と状態を確認する。
旧工程の待機関係そのものを、永久の実行前提にはしない。

handoffは version=1、todo_id=3059、origin_todo_id=3056 と固定repo/PR、
head_sha/head_tree/base_sha/run_id/attempt/job_ids/kind/context_shaを持つ。
review/acceptanceはそれぞれ file/sha256 を持つ別々の原記録への参照。
各原記録は origin_todo_id/repo_id/pr/head_sha/head_tree/kind/success/must と
output_file/output_sha256 を持つ。成功した同じ版の原出力を別々に照合する。
JSONの未知field、重複key、role交換、同じ原出力の使い回しは拒否する。

新handoff、各原記録と出力は読み出す前に正規guardで確認する。未分類や保護対象、
旧拒否入力の参照をこの経路で救済しない。原出力を新しいファイルへコピーしない。
読んだ同じbytesをRAMで固定して公式parserへ渡し、ログへ本文を出さない。

`current-pr-runtime-qualify` は入力だけを検証する。
`current-pr-runtime-inspect` はさらに既存のlive source/workflow/run/attempt/jobと
queueを確認する。`current-pr-runtime-job <new-owned-dir> <Linux-helper>
[gitleaks-ELF]` はその同じplanを既存の隔離実行へ渡す。
secretは1jobとcontextが必須、CodeQLは確定matrixと空contextを要求する。
新しいheadのためにhelperの定数や配布版を変更しない。

受入: 同じPRの異なる確定head/runを同じbinaryで受理し、secretとCodeQLを
公式parser/plannerへ接続する。別repo/PR/未知kind、旧headの原証拠、停止、
変更済み原出力、読取不可の新原記録をtoken/Docker/runner登録前に拒否する。
以後のAPI変化、実runner割当、source/auth hookと全owned回収は既存の判定を保つ。
fixtureの成功を実CI成功と呼ばない。元995の同head5検査と939の成功は再実行しない。

handoffの例（SHAやパスは実測した値へ置き換える）:

```json
{
  "version": 1, "todo_id": 3059, "origin_todo_id": 3056,
  "repo_id": 1285649124, "pr": 995,
  "head_sha": "<40桁>", "head_tree": "<40桁>", "base_sha": "<40桁>",
  "run_id": 123, "attempt": 1, "job_ids": [456],
  "kind": "secret", "context_sha": "<40桁>",
  "review": {"file": "<元のreview結果の絶対パス>", "sha256": "<64桁>"},
  "acceptance": {"file": "<元のacceptance結果の絶対パス>", "sha256": "<64桁>"}
}
```

各結果の例:

```json
{
  "origin_todo_id": 3056, "repo_id": 1285649124, "pr": 995,
  "head_sha": "<同じ40桁>", "head_tree": "<同じ40桁>",
  "kind": "review", "success": true, "must": 0,
  "output_file": "<実際の原出力の絶対パス>", "output_sha256": "<64桁>"
}
```

製品担当は同じ版の全読と別モデルの実行元、必要な受入の完了を確認してから
canonical handoffを発行する。この発行者を信頼する境界は従来の親のhandoffと同じ。
helperが原文からレビュー結論を推論したり、guardの結果を著者の証明と解釈したりは
しない。原結果と原出力は別物として検証し、不足時は発行しない。
`must`の欠落/nullも未確認として拒否する。CodeQLではkind=`codeql`、
context_sha=`""`、job_idsに正式matrixの全IDを昇順で指定する。
