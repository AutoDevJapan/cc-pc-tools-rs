# ToDo 2762: fixed-source CodeQL triage

固定した GoDD MCP α PR 3613 の head `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07` / tree `f7eea82c96449b7749f98dd6c4b0344b64f1d651` を確認した。調査中もGitHubのPR headとbaseは変わらず、PRはopenのままだった。

## 結果

固定analysisは計37件。実害を確認できた件数は0件だが、JavaScriptの6件とActionsの25件は未確認を残す。これはMust 0件という意味ではない。

| ルール | 件数 | 分類 | 判断 |
| --- | ---: | --- | --- |
| `actions/cache-poisoning/direct-cache` | 2 | 根拠付き非該当 | 該当workflowは通常の `pull_request` とmain限定の `push` で動く。PR実行のcacheはpull request merge refに閉じ、mainのcacheへ書き込めない。`pull_request_target` と `workflow_run` は使っていない。 |
| `actions/cache-poisoning/poisonable-step` | 25 | 追加検証が必要 | 4 workflowに手動実行と入力がある。入力からcheckout refを選ぶ経路とcacheを使う経路があり、未信頼PR refが渡せるか、実行者・検証・cache write scopeを追加確認する必要がある。 |
| `js/incomplete-multi-character-sanitization` | 1 | 追加検証が必要 | 非fixtureのsource。変換回数・長さ条件だけでは不十分で、入力元から最終消費先まで未確定。 |
| `js/incomplete-sanitization` | 1 | 追加検証が必要 | 非fixtureのsource。入力・変換・sinkのつながりと有効な境界処理を未確定。 |
| `js/incomplete-url-substring-sanitization` | 1 | 追加検証が必要 | 非fixtureのsource。URLとして使う値の入力元、検証方式、最終用途を未確定。 |
| `js/redos` | 2 | 追加検証が必要 | 1件は非fixture、1件はtest/fixture配下。どちらも攻撃者制御の入力と長さ上限・実行時間の増え方を確定できていない。 |
| `js/shell-command-injection-from-environment` | 1 | 追加検証が必要 | test/fixture配下の検出だが、環境値の信頼元と本番コードへの到達性を未確認。test pathだけでは非該当としない。 |
| `rust/hard-coded-cryptographic-value` | 4 | 根拠付き非該当 | 4位置すべてが `cfg(test)` module内。本番crateでは条件付きコンパイルにより除外される。 |

合計は非該当6件、追加検証31件、確認済み実害0件。追加検証の内訳はActions 25件、JavaScript 6件。

親2641は2737の待ちを維持する。2762の確認結果を受領した後も、製品全体の完了とは扱わない。

## 証拠と残作業

- 公式CodeQL run `38893804340` は5 job成功。SARIF集計はActions 27、JavaScript 6、Rust 4、Python 0で、全37件のrule/countと一致した。
- Actions 25件は、`workflow_dispatch` 入力によるref選択、未信頼refを拒否する検証、実行者の権限、cacheへの保存scopeを個別に照合する。未信頼のrefが既定ブランチscopeへ保存され得るなら、refを固定の許可リストにし、低信頼実行からのcache書き込みを分離する。
- JavaScript 6件は入力境界・データフロー・消費先、正規表現の計算量と入力上限を固定source上で追加確認する。
- 固定SARIFのSHA256: Actions `80f830c9790a52872913efe26f25a5f8c06b2727e1e897424dd1a9fd1ce3b787`、JavaScript `254b0e071c31c3a7487d8085f943aecbe664a2069808670fb9cb956c7ce6c195`、Rust `a38bcf2aa930f89d7a55b14f1bf49e068c78e64ba36b72e362fb716f505996f8`。
- 公式結果metadataのSHA256は `39cf2f18f3e8123d2ed0edcf0367f69d452491faabcd65cd75314a7a243d6ed7`。対応ログはbyte数とSHA256 `a5cb57150624c8b83c94ee2fedd48a313acc332070c289df2a180e0aa0876f74` がproofと一致した。
- 公式GitHub資料のcache境界では、`pull_request` 実行はpull request merge refへ保存し、default branch scopeへ書き込めない。cache actionの自動保存はcache missかつjob成功時だけ。CodeQL query helpもdefault branchへ書けるtriggerを検出対象にしている。
- 生のSARIF位置・source path・fixture名・値を含む対応表は `D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459/todo-2762-codeql-findings.private.json` に保存した。SHA256は `1be0da7ca73e690fca93da400ed797023301d8b7b5805bb3216237f8d8608e83`。親2641の元noteにもこの保存先とhashを記録する。

本工程では製品コードを変更せず、SARIF alertも削除・dismissしていない。Hosted job、runner、Cargo検査は起動していない。親2641が追加確認・修正・別差分レビュー・PR手順を続ける。
