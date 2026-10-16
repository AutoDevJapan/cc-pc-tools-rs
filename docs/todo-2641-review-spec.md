# GoDD MCP αのHTTP修正と解析指摘を別モデルで確認する

元ToDo2641の有限read-onlyレビュー。実装担当はgpt-6系なので、既存OAuthのClaude Sonnet枠を指定する。複数ファイルを確認するためHaikuには渡さない。子Codexジョブの直接起動、コード変更、CI起動、PR merge、Prd変更はしない。

起動時にD:/CC/AGENTS.mdと元2641のnote/specを読み、実GitHub ownerをAPIで確認する。原対象はAutoDevJapan/GoDD-MCP-Alpha、repo_id1154066087、private/admin/nonfork、PR3613、sameRepo head/base、branch `codex/todo2641-http-response-pseudo-20261015`。固定head `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07`、tree `f7eea82c96449b7749f98dd6c4b0344b64f1d651`、base `9ee640e763091c4688aaf6204e06f9fb1b493695`。取得前後に一致確認し、別headは別物として記録する。

## 対象

GitHub APIから取得する原AGENTS.md/CLAUDE.mdと次の3差分および必要最小限の周辺契約。ホストの旧製品フォルダ、原保護一覧、guard、owner-work登録へ触らず、新しい製品checkoutも作らない。

- godd-mcp-alpha/native/src/http_message_admission.rs
- documents/spec/cursorlens-proxy-http-message-boundary-admission-design.md
- .github/workflows/codeql.yml

HTTP responseは:statusのみ、requestは既存4種のみを受理し、重複/順序/必須検査を維持する。純粋coreでありI/O/runtime/MCP/npm/peer実測は未接続。元Issue1100を閉じない。配信prompt/mindset/seed/UIは変更していない。既存レビュー5520900843は旧c53dc14のnative/SpecにMust0/Suggestion1/Nit0、Suggestionは既存正常request回帰と重複するため未採用と記録されている。現在のCodeQL route差分も含め、実害のある欠陥・既存検査の抜け・過大主張を確認する。

## 確認済みの事実と限界

- 旧c53dc14に対する独立Linux/Windows/Rust/Node受入は2710が配送済み。旧回収のLinux2部分log70件に最終manifestが無いこと、新しい独立cohortと原baselineのSHA照合成功を区別する。回収の未完は2381待ち。
- 新b194は旧c53からCodeQL runs-on scalar2箇所のみ変更。新headのnative検査実行済みとは言わない。
- CodeQL公式run38893804340は4言語とpolicy/gateの5job成功、全analysisが新b194/error空。これは指摘0ではない。
- 通常CI38893656960のVerify/CI Gateはfailure。公式annotationはActions予算による未開始。Windows/PR tree receiptはskipped。別の独立工程が通常CIの回復を担当する。このレビューでは検査を起動せず、静的確認と未検証を区別して返す。

## CodeQL37件の扱い

固定analysisのSARIFだけを採用する。mainの別SHAのopen alertをこの結果へ混ぜない。保存先はD:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459 の repo-1154066087-analysis-*.private.sarif と同repoのofficial-final/log-proof。SARIF本文・snippet・値・位置・fixture識別名をmodel/stdout/報告へ出さず、rule/count/hashだけ扱う。必要な場所の対応付けはプログラム内だけで行い、原GitHubの非秘密workflowや変更sourceを別途取得して評価する。秘密や識別名を読んだり表示したりする必要がある手法は使わない。

Actions: actions/cache-poisoning/direct-cache 2、actions/cache-poisoning/poisonable-step 25。JS: js/incomplete-multi-character-sanitization 1、js/incomplete-sanitization 1、js/incomplete-url-substring-sanitization 1、js/redos 2、js/shell-command-injection-from-environment 1。Rust: rust/hard-coded-cryptographic-value 4。Python0。

各rule群を、実害を確認できるもの/テスト用途などで説明できるもの/追加検証が要るものへ証拠付きで分ける。確証なく誤検知やMust0へ落とさない。実害があると判断したら、秘密や禁止された位置を出さず修正方針と件数を親2641へ返す。元37件を黙って削除・dismissしない。公式の一次資料が必要ならGitHub/CodeQL公式資料だけ参照し、ドメイン事業者へはアクセスしない。

## 返し方

Must/Suggestion/Nitを日本語で返す。原3差分の指摘は上の許可されたfile:lineと具体的修正を添える。解析37件はrule/countと検証の理由だけとし、原log/SARIFの内容・識別名・秘密値・位置を返さない。静的確認、実行結果、未確認を分け、レビュー対象headと原応答を自分の作業branchへ保存・commit/pushする。元2641のnoteへ非秘密の結論と原応答の保存先を配送する。原PRへ投稿するなら本文は日本語、self-reviewはCOMMENTEDで元headを指定する。コードや他Orgの環境を変更しない。

通常CIの担当や親2641の完了を待つ条件は作らず、この固定sourceの有限レビューを終えて返す。新費用/APIキーAI/GPU/Civ6/本人PC操作/Terminal/UACは禁止。自分の新規tempはsafe-deleteの手順に従い、原証拠・他担当のtempを片付けない。終了前に自身のscheduler checkpointを保存する。
