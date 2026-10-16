# 元2648の再開記録（2026-10-16）

同じworktreeと `scheduler/todo-2648-product-codeql` を使う。
reset/checkoutで古い枝へ戻さない。同2648や同じ検査の担当を増やさない。
最初に元2648の最新spec/noteと
`D:/CC/Tools/product-ci/evidence/todo-2648-20261015/parent-latest-note.txt`
を読む。本書より新しい原noteを優先する。
`docs/product-ci-codeql-2648.md` は10月15日時点の履歴として保つ。

## 実装、実検査、配布まで終えたこと

PR173/174/183/185/187はmerge済みで、再実行しない。
最後の実装は `edd3af3c195871ba6c2fd51e9ffcbccec1ec9b42`。
PR187の通常mergeは `a83aa2606e7354088cd23da111709729e4c82e35`。
両者のGo module treeは `b223ef91a223e15a480500aca55bef2f1144835f` で同一。
本書だけの保存は、配布済みの実装を変えない。

- 元advanced workflowを使う固定sourceの公式ephemeral検査を追加した。
  全blob/index/treeと実commit SHAを登録前に照合する。
  元言語、queries、policy/gate、no-buildの条件を保った。
- GitHub GETの明示的な一時通信障害だけを最大3回再試行する。
  権限/審査拒否、未知の失敗、mutationは再試行しない。
- PC CodeQLのexit137を原annotationから確認し、公式actionの
  init入力にthreads2/ram6144を指定した。実原ログでも同じ値を確認した。
- 最終eddのGo両OS38896596080、全workspace/doc/Clippy38896595921、
  CodeQL全4言語38896595929が成功した。2731/2733 Sonnetと
  正しいinlineを渡した2732 Haikuの最終レビューは全てMust0。
  古い別rootを読んだ2732の最初の応答は合格に数えない。
- merged sourceからGoだけを正規配布し、配置後inspectもexit0。
  Windows exeのSHA256は
  `E5D2C61AC5B0001A565A68C37561B1CBDAA6931EE13B8109D83BCFCBD6A2BE45`。
  Linux `product-ci-linux-edd3af3` は
  `8FA00AC4B89FAF934C00C3E547FD1D21054BB5A62474B4BE60481D97E4F35FFF`。
  旧exeはhash照合してatomic backupした。既存runtimeは配布していない。

## 4候補の実結果を配送したこと

| repo_id | 現在の固定head | 公式CodeQL run |
| --- | --- | --- |
| 1285649124 | b93e126b172adbb29ad2081af4732f4b42f8bc95 | 38879883760 |
| 1392152644 | 75a4051305b6f47c70ddb59a8d2bf9015666f12c | 38892349061 |
| 1154066087 | b194d66524ef86330bb73ca3f22f9e5dd6ae0a07 | 38893804340 |
| 1386702183 | b2b6d47b7f9e17617d16a9079a70de272bc3c04c | 38893812340 |

全候補で元の全解析言語とpolicy/gateが実success。
128はsecret38879889344とNode185も成功。
139は必要なRust修正後の両OS関連48件、Linux1850件、Windows1853件と
Git所有者拒否1件の限定再検査、doc/Clippy/fmtが成功した。
原sourceとの同一treeを根拠に再利用したApp/native検査は、
新headで全件再実行した結果と混ぜない。

解析成功は指摘ゼロではない。115には37件、138には9件の結果があり、
原SARIFと未判定事項を元2641/2642へ配送した。dismiss/除外は行っていない。
全ログのbytes/hash/source、終了と登録不在の証拠は元noteと指定evidenceへ保つ。
元2639/2640/2641/2642の製品全体の受入、実害修正、merge/Prdは残っている。
2648が製品4件全体をdoneするという意味ではない。

## 再開時に残ること

1. merged a83のBuild38898257026、Go38898256805、CodeQL38898257373は
   全successを確認済み。Windows全workspace2254件、doc/Clippy、Go両OS、
   CodeQL全4言語が成功した。全原ログのbytes/hashも保存した。
   この同じCIを重複実行しない。
2. 元2710/2730は自身の終了済み資源の回収を既存2381で待っている。
   親がそのroot/profileを重複変更しない。Windows専用profile/hive、
   helperのPROTECTED_PATH/ENTRY_LIMIT_EXCEEDEDを未完として保つ。
   拒否された同操作を別手段へ変えて達成しない。
3. 2710のLinux部分70recordは最終source manifestがなく、合格根拠に
   使わない。別のcertified cohortと原結果表を区別し、元2641の
   未確認工程を残す。原ログをモデルやstdoutへ表示しない。
4. 親の一時資源は所有/終了/hash保存を確認してsafe-deleteの
   plan→別通常削除→finishで回収する。他担当のデータは片付けない。
   自分の依存ToDoの完了だけで終了する場合は必ず正式なwaitを記録する。

顧客名・原path・名簿を表示せず、repo_idで扱う。原製品folder/guard/
manifestの変更や拒否の迂回、既存runner停止、Org権限追加、予算増額、
AI APIkey、GPU、Terminal/UAC、本人PC操作要求、Discord投稿を行わない。
本人判断待ちではなく、未完了は継続中として保持する。
