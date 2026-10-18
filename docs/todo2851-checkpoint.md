# ToDo2851 再開情報

## 03:13 JST 更新

レビュー2854は実Sonnet5.5/succeeded/exit0/55steps/Must0で受領済み。
担当はgitを起動できなかったが、baseのworktreeとheadの6対象を全文比較した。
実装側がbaseの3既存fileをGit blobと照合し、全件一致を確認した。
原応答と照合記録はtodo2851-review2854.log / todo2851-review2854.private.json。
正式exeだけを使う最終adapter4回帰、利用側574件（既存skip3）、対象4libraryの
doctest（対象0件）、Clippy -D warningsは成功。繰り返さない。

追加指示の直接subworkについて、子自身の現在の資格と通常審査の回帰を追加。
固定path SHA、Runnerの失敗と正式exeの呼出し、名簿復旧/読取不能/doingの
worktree拒否も確認した。追加検査の初回は不正subworkの正常なErrを検査側が
unwrapしたため1件失敗。正常な拒否をassertするよう検査だけを修正し、その1件を
再実行して成功。初回5passと修正後1passを別のlogに保持し、実装修正とは扱わない。

追加差分は検査と文書のみで、稼働処理はレビュー済みb265770から変更していない。
Go2849のcurrent-succession-delivery.private.jsonは候補APIまでで、正式merge/配布は
falseのまま。候補分類を正式受付へ読み替えない。次は追加差分レビュー、最新headの
公式CI、通常merge、並行変更保持、scheduler正式更新、Go部分配布とAO2839の実受付。

## 対象と保持条件

元2834の現在の製品継承先だけをschedulerへ接続するRust担当。
Go側は2849が所有。親worktreeの
`docs/todo2834-current-succession-contract.md`が原契約。
原名簿/auth/config、未知の名前、旧192件、元1161/1368/1476/2179を復旧しない。
GPU・Civ6・APIキー・新費用は使わない。

## 確認済み

- 正式guardでこの作業場所はclient_resource=false。
- GitHub APIでAutoDevJapan/cc-pc-tools-rs、repo ID1406818713を確認。
- 開始baseはc9f0b26df6014c5473e110996b58cda18dff151e。
- 既定falseのBackend API、正式exeのreceipt照合、NotFound限定受付、
  次のrepair-stepの結果受領と依存解放を実装。
- scheduler初回nextest: 191 passed / 0 skipped、run ID
  c6f70188-1903-4814-a5a2-cafa335e6d03。
  その後、未配布exe時に別interpreterへ落とさず正式exeだけを呼ぶ修正を追加。

## 残り

1. 最終ソースの関連回帰、利用側、doctest、Clippyの結果確認。
2. 固定base/headの別モデルレビューを通常ToDo --specで登録し、Mustを解消。
3. PRの公式CI成功、通常merge。
4. 2849のGo部分配布を照合し、並行変更を保持した最新mainから正式schedulerを更新。
5. 同じAO2839の通常受付、実job/worktree/成果と次の自然巡回を確認。
6. 元2834へ通常CLIで実結果を返し、製品別の通知なしDiscordへ反映。

2849全体のdone待ちは相互依存になるため行わない。部分配布の未達だけなら
同じ項目の有限wait-retryを使う。本人判断待ちへ変更しない。

検査用一時フォルダはsafe-delete所有ID 22e983ea3cc29fe59f11b5eb48322e51。
その所有記録を使って最後にplan→通常削除→finishする。
