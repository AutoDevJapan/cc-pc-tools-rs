# 元ToDo2639：マージ後のCIと既存Prd反映を完了する

元2639の有限な独立工程。本人にしか決められない未指定要件・新費用を増やさず、必要な技術作業を自律で進める。既存OAuthサブスク枠だけを使い、AI APIキー・追加支出・Hosted予算増額・GPU・Civ6・本人PC操作・Terminal/UACは禁止。割当Cargoは必要な検査にだけ使う。元1636/2016/2274、他3製品、元2648/2710等の停止・依存・未完了は変更しない。

## 確定済みの成果と新しい未完了条件

GitHub repository ID1285649124、AutoDevJapan/private/admin/nonfork。名称はメモリ内API経路だけで使い、stdout・会話・ファイル名・報告へ出さない。PC上の旧製品folder/除外一覧/秘密/guard/registry/owner-work登録は操作しない。拒否された登録や操作の代替実行は禁止。他Orgの環境、新しい非Prd環境、未知のrepo/headは対象外。

PR1002は2026-10-17 02:39 JSTに親2639が通常mergeした。固定PRhead20c441df545a88f4a839d7e906b5aeb4f1708ae3、merge/main=41090d19329b81dd1bd076f3336d723823be4b82、両tree=007b84082008ea9effd5052f63856273aabbfb17。mergeの親は原basef3d946cb0f7e33b8cb5cbc0858091e583ca61350と20cの2件。PRはclosed/merged=true。draft解除・mergeを繰り返さない。原PR173/174/183/185/187/188等の完了操作も繰り返さない。

原App/CI差分5ファイルはSonnet2735でMust0。追加4ファイルはSonnet2743でMust0、原応答SHA256 c4ae16bb34d53b502e085d04644447472b58ab86b9e58630783bc8cc83ff48f1。親はsnapshotにcommit/rollback/close/beginがなく、projectionがString castを使い、modelがDateTime(timezone=True)であることも固定sourceから確認した。SuggestionはMustへ自動昇格しない。未来日時等の既存仕様や製品方向を広げない。

20cの正式CIは元2744で全6check success（CodeQL4言語+集約、通常secret/test）、Node185pass/skip0。親の同byte検査でRuff/format/mypy/監視50pass/skip0（実PostgreSQLの3timezone含む）。元956のbackend2236と別途3結合成功は旧sourceの証拠として区別する。マージ前の実設定を固定sourceの監査scriptで隔離実行しGitHub security boundary verified。未解決review thread0。ruleset/供給元/権限/予算を変えず通常mergeした。

原証拠：親worktreeの .ci-work/todo2639-evidence-20261017/、および D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/9c9448c8d6dd1c91e0b46f4cc6438d2c/ のfinal-delivery.private.json/manifest-final.private.json。生log/source/秘密/顧客名を表示しない。必要なbyte/hashと確定値だけ返す。

## 現在の失敗を実確認した

- merge41090に対するpromotion run38941324877、workflow ID306106154、event pushはcompleted/failure。
- preconditions check/job117109404912はsteps0。実注釈は `The job was not started because an Actions budget is preventing further use.`。業務preconditionを実行して落ちた結果ではない。
- verify-backend-integration117109423278、verify側117109423311、promote117109424787はすべてskipped/steps0。配布成功とは扱わない。
- repositoryの特定変数DEPLOY_ENABLEDをGETし値trueと確認済み。秘密変数やsecret値を列挙・取得していない。変更していない。
- merge41090の公式CodeQL run38941323954/workflow ID331939928はqueued。jobはPython117109408190、JS117109408508、Rust117109408625、Actions117109408843。PRhead20cの成功をこのrunの成功には書き換えない。
- 新mergeのGitHub deploymentは最初のGETでは0件。GitHub deploymentが0件というだけでVercel/RailwayのPrd不在とは断定しない。

## 実施範囲

1. 最初に実GitHub所有・同PRのmerged状態・merge SHA/親/tree・現在mainと、上記run/jobの現状態を読戻す。41090以外のheadへ勝手に追従しない。同Prdの既存配備がGit連携で更新済みか、既存の正規CLI/ログイン済みオーナーChromeで非破壊確認する。必要な規則、release safety gate、deployment smoke、workflow trust policy/digestを固定sourceから読む。顧客固有の人間承認があれば保持する。
2. 予算を上げず、既存の正規実行環境でマージ後の公式CIとrelease gateを実行できるように必要な修復を行う。最新版の正規product-ci（元2745/PR188・正式merge fa594cec7bee92bffa3fbd8ab815e3c03a189efc）と既存Linux実行基盤をまず調べる。旧toolがopen PR/旧固定sourceを要求する条件を直接書き換えて実行しない。
3. 足りない機能の実装修復が必要なら、所有をAPI確認した正規の修復source repository ID1406818713に、有限な同repo/同PRの実merged receipt（41090/007b/両親）を検証する正式工程として実装する。未知repo/fork/未知workflow/勝手な最新mainの実行を許可しない。他3候補の固定値、排他、resource制限、secret保護、guard、登録manifest、queue設定を変更しない。source commit・通常PR・別モデルレビューMust0・必要CI・merge・正式配布の順を守る。未配布toolや即席jobで元の拒否を回避しない。
4. product側workflowのHosted固定が根因で修正を要する場合も、41090を基点とする必要な有限CI修復だけを通常branch/PRで行い、自分の新commit/treeを固定して全差分・レビュー・必要検査を保持する。App機能やUIを増やさない。reviewed semantic digestはテストを緑にする追随値ではないため、workflow trustの正典に従い独立レビューで実行内容を確定してから同じPRで更新する。チェック削除/skip/権限拡大/任意labelの汎用runner登録/手動successで合格にしない。必要修正とその子commitの作成許可を、未知の他headへの追従許可へ広げない。
5. 元preconditionsの業務条件とfull verification・backend integrationを維持し、steps0/skippedを成功にしない。実在する既存Prdだけへ正規の配備処理で反映し、実source revisionと非破壊readiness/smokeを照合する。実際に必要なdrainを行わず確認flagを真にしない。新環境・新費用・秘密値の読取は行わない。追加契約が必要という仮定で止めず、既存枠・既存CLI・本人Chrome・既存管理者窓口で技術的な操作を完了する。
6. 現在の失敗・queuedを保持しつつ、各実行のsource/head/tree/run/job/供給元、成功・失敗・未実行、配備revision、probe、原log bytes/SHA256、containerのPID0/回収/runner登録不在/排他解放を保存する。前sourceの合格を新sourceの再実行と表現しない。新tempはsafe-delete create→plan→別呼出し通常削除→finish、拒否は正規quarantine。自分のChromeタブは工程終了時にowner_chromeの記録と照合して閉じる。原資料・他担当の資源は保持する。

## 担当と完了条件

子Codexジョブを直接起動しない。独立した必要工程は共有ToDoへ--spec/必要資源付きで登録する。別モデルが1ファイルだけ読むならHaiku5.5、複数ファイルならSonnet等を使う。親2639が状態と成果を回収するため、親をこの子へ依存させる。親2639のdone、原製品全体のdone、旧停止/依存の解除はこの子では行わない。

この限定工程の完了は、上記のマージ後CI/必要release gateの実結果が揃い、既存Prdで対象修正のrevisionと非破壊probeが確認でき、自分の検証資源の後片付けまで終わった時だけ。実装や登録だけで完了としない。技術的な外部待ちは正式wait/wait-retryで保持し、同じ確認だけの担当を繰り返し作らない。オーナーだけが決める新費用・権利等が実際に必要な場合だけ、元判断とDiscord履歴を確認して具体的に1回質問する。
