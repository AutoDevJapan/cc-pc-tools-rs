# ToDo 2762: fixed-source CodeQL triage

固定した GoDD MCP α PR 3613 のhead `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07` / tree `f7eea82c96449b7749f98dd6c4b0344b64f1d651` を確認した。調査中もGitHubのPR headとbaseは変わらず、PRはopenのままだった。

## 結果

固定analysisは計37件。Actionsに条件付きの危険経路13件を確認したが、実際に悪用された証拠はない。JavaScriptの6件は追加確認が必要で、Must 0件という意味ではない。

| ルール | 件数 | 分類 | 判断 |
| --- | ---: | --- | --- |
| `actions/cache-poisoning/direct-cache` | 2 | 根拠付き非該当 | 該当workflowは通常の `pull_request` とmain限定の `push` で動く。PR実行のcacheはpull request merge refに閉じ、mainのcacheへ書き込めない。`pull_request_target` と `workflow_run` は使っていない。 |
| `actions/cache-poisoning/poisonable-step` | 25 | 13件は条件付き危険経路、12件は根拠付き非該当 | 12件は自由入力のrefをcheckoutし、同じworkflow内でcacheを使う。入力は空でないことだけを確認し、許可リストや固定SHAには制限していない。mainを選んで未信頼refを渡すと、成功時のcache保存がmain scopeへ入る。別の1件は自由入力が実行stepへ届き、その後cacheを保存する。残る12件は `workflow_dispatch` が選んだbranch上のcodeだけを実行し、そのbranch scopeにcacheを保存する。PR専用scopeからmainへ書き戻す経路はない。 |
| `js/incomplete-multi-character-sanitization` | 1 | 追加検証が必要 | 非fixtureで複数文字の変換パターンを検出。未信頼入力か、変換後の値が安全性に関わる用途へ届くかを確定できていない。 |
| `js/incomplete-sanitization` | 1 | 追加検証が必要 | 非fixtureで不完全な文字列置換を検出。入力元と最終出力先のつながり、必要な文脈別encodingを未確認。 |
| `js/incomplete-url-substring-sanitization` | 1 | 追加検証が必要 | 非fixtureでURL部分文字列の判定を検出。攻撃者制御のURLか、hostnameを解析して完全一致の許可リストを使うべき用途かを未確認。 |
| `js/redos` | 2 | 追加検証が必要 | 1件は非fixture、1件はtest/fixture配下。正規表現の危険パターンはあるが、攻撃者制御の入力・上限・最悪時の実行時間を確定できていない。 |
| `js/shell-command-injection-from-environment` | 1 | 追加検証が必要 | SARIFのflow位置はtest/fixture内にある。環境値を外部から操作できる実行経路か、CIだけのテスト値かを確認できておらず、test pathだけでは非該当としない。 |
| `rust/hard-coded-cryptographic-value` | 4 | 根拠付き非該当 | 4位置すべてが `cfg(test)` module内。本番crateでは条件付きコンパイルにより除外される。 |

全体では非該当18件、条件付き危険経路13件、追加検証6件。非該当18件は `direct-cache` 2件、branch scopeで隔離された `poisonable-step` 12件、Rust 4件。追加検証6件はJavaScriptです。実際の悪用・cache汚染は観測していません。

親2641は2737の待ちを維持する。2762の確認結果を受領した後も、製品全体の完了とは扱わない。

## 証拠と残作業

- 公式CodeQL run `38893804340` は5 job成功。SARIF集計はActions 27、JavaScript 6、Rust 4、Python 0で、全37件のrule/countと一致した。
- 条件付き危険経路13件を直すには、ref入力を既知の信頼済みcommitへ許可リスト制限し、低信頼codeを実行するjobでcacheを書かない。実行stepに渡す自由入力はshellコマンドへ補間しない。
- JavaScript 6件のquery patternはソースにあるが、CodeQLのpattern検出だけでは実害にならない。string escapingは出力文脈に合うencoderを使い、multi-character sanitizerは安全なライブラリまたは収束までの反復を検討する。URL検査はparseしたhostnameの完全一致許可リストへ替える。ReDoSは曖昧なbacktrackingを避け、入力上限を入口で適用する。環境値はshell文字列へ連結せず、固定実行ファイルと引数配列で渡す。これらは入力元と最終消費先を確認した後に適用する。
- 固定SARIFのSHA256: Actions `80f830c9790a52872913efe26f25a5f8c06b2727e1e897424dd1a9fd1ce3b787`、JavaScript `254b0e071c31c3a7487d8085f943aecbe664a2069808670fb9cb956c7ce6c195`、Rust `a38bcf2aa930f89d7a55b14f1bf49e068c78e64ba36b72e362fb716f505996f8`。
- 公式結果metadataのSHA256は `39cf2f18f3e8123d2ed0edcf0367f69d452491faabcd65cd75314a7a243d6ed7`。対応ログはbyte数とSHA256 `a5cb57150624c8b83c94ee2fedd48a313acc332070c289df2a180e0aa0876f74` がproofと一致した。
- 公式GitHub資料のcache境界では、`pull_request` 実行はpull request merge refへ保存し、default branch scopeへ書き込めない。cache actionの自動保存はcache missかつjob成功時だけ。CodeQL query helpもdefault branchへ書けるtriggerを検出対象にしている。
- 固定repo設定の読取結果は、default `GITHUB_TOKEN` 権限がwrite、Actionsが有効、組織のactor/event制限policyが0件。`workflow_dispatch` を実行するにはwrite権限が必要。設定は変更していない。
- 固定された `setup-node` manifestのSHA256は `5d765941ab5d8bef27f08e81b0b041cdb2df2050ea0261dc925d157a2bafbd2b`。cache設定と成功時のpost-saveがあり、1件はauto-cacheにも対応する。対応ログを含む非公開証拠に条件判定を保存した。
- 生のSARIF位置・source path・fixture名・値を含む対応表は `D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459/todo-2762-codeql-findings.private.json` に保存した。SHA256は `cd76dc5eb902d4c00ada1cfd1b5fb581ddedf252963edc087518f196fd3a622e`。親2641の元noteにもこの保存先とhashを記録する。

本工程では製品コードを変更せず、SARIF alertも削除・dismissしていない。Hosted job、runner、Cargo検査は起動していない。親2641が追加確認・修正・別差分レビュー・PR手順を続ける。
