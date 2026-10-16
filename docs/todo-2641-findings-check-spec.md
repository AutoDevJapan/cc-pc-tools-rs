# GoDD MCP αの解析37件の実害を固定ソースで確認する

元2641の有限check工程。2738の既済3差分レビューや2761の追加差分レビューを繰り返さない。
2738にはSARIFが供給されず37件すべて未評価だったため、この未確認部分だけを確認する。
原2737/2740の公式CI・guest準備は別担当であり、同じ検査やrunnerを新規に起動しない。

## 固定対象と範囲

元本人依頼2026-10-15は、GitHub上の所有確認済み原リポジトリから価値のある未完工程を一つ実装し、検査・別モデルレビュー・通常PRへ進めること。原18/1153の停止/依存/未完を保ち、これを製品全体の完了にしない。
対象はGitHub実repo_id1154066087 / AutoDevJapan/GoDD-MCP-Alpha / PR3613、解析sourceは固定 `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07`、tree `f7eea82c96449b7749f98dd6c4b0344b64f1d651`。実owner/private/admin/nonfork/同head+base repo/open/refを取得前にAPIで確認する。候補変更を検知したら、固定した旧解析と新headを区別し、原解析を新しい結果へ読み替えない。
原製品フォルダ、保護一覧、guard、owner-work登録、登録manifestを開いたり変更したりしない。ホストへ製品checkoutを作らない。原GitHubの必要最小限のソースだけをRAM/専用の検証guestで扱う。

## 入力

公式CodeQL run38893804340は全4言語とgateの5job成功。これは37件の安全性を証明しない。
固定analysisのSARIFは次の指定証拠rootだけから読む。
`D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459`
`repo-1154066087-analysis-*.private.sarif` と同repoのofficial-final/log-proofをプログラム内で対応付ける。別SHAのmain open alertを混ぜない。
SARIF本文・snippet・位置・fixture識別名・値をmodel/stdout/報告へ出さず、rule/count/hashだけ扱う。位置から必要なGitHub blobへの対応付けはRAM内のプログラムだけで行い、秘密情報を読む手法を使わない。コマンドエラー、例外、raw API応答やテストlogにも識別名や秘密が混じり得るので、表示せずprivate証拠へ保存し、要約だけ返す。

- actions/cache-poisoning/direct-cache 2、actions/cache-poisoning/poisonable-step 25
- js/incomplete-multi-character-sanitization 1、js/incomplete-sanitization 1、js/incomplete-url-substring-sanitization 1、js/redos 2、js/shell-command-injection-from-environment 1
- rust/hard-coded-cryptographic-value 4、Python0

親のpath種別だけのRAM照合では、Actions27はworkflow、JS2はtest/fixture path、JS4とRust4はその他sourceだった。これは意味解析やproduction除外の証明ではない。同じpath区分だけを繰り返して完了しない。
原37件を削除/dismissせず、原SARIFとhashを保持する。

## 確認すること

1. 各rule群を実害確認/根拠付きの非該当/追加検証必要へ、件数と証拠を対応付けて分類する。単なるtest名、固定値の存在、CodeQLのseverity、親差分と無関係なことだけを結論の根拠にしない。
2. Actionsではuntrusted codeを実行するtrigger/refと、cacheを保存・復元するscope/条件/信頼の境界を確かめる。PR専用cacheとmainのcacheの分離、特権workflowからの復元、action実装の保存条件も必要なら公式の固定一次資料で照合する。Hosted jobを起動せず、追加課金・権限増加・cache削除をしない。
3. JSでは変換結果の消費先・入力境界と長さ上限・反復置換の必要性・regexの入力での増え方・shellの環境値の信頼を区別する。Rustでは固定値がproductionの暗号処理へ到達するか、cfg(test)やfixtureだけに排除されるかを実証する。値・source snippet・対象の識別名を出力しない。
4. 意味のある検査が必要な範囲だけ、割り当てられたCargo資源と共有gate/専用target/sccache、使い捨ての専用資源で実行する。原2737/2710の全件Rust/Node検査を重ねない。必要な新しい検査が作れない・実行不能なら、その部分を未確認として保持する。新規の使い捨て実装はPowerShell/Go、GoDD製品の新コードはRust。Pythonを新しく書かない。
5. 実害が確認できた群は、親が修正できる具体的な方針と同じ原箇所をRAMで解決できる対応証拠をprivateへ保存する。生の位置や値を親の会話/最終返信へ出さない。勝手に原PR headを更新したり、製品の公開APIや仕様を変えたりしない。コード修正・追加差分レビュー・merge/Prdは親2641が行う。

## 完了と返すもの

総数37と各rule群の分類、判断の根拠となる非秘密の条件・実検査exit/hash、未確認の具体的な不足を返す。実害0件とMust0を混同しない。全群で実証できなければ、その範囲を未確認のまま配送し、受付だけを全件完了にしない。
原証拠のhashと新しいprivate対応証拠のhash/保存先を元2641のnoteへ配送する。最終返信は日本語、rule/countと必要な結果だけ。親や原2737の完了を待つ依存を作らず、この有限確認を返す。別の人やモデルが既確認範囲を全件再読するレビューを追加しない。

起動時にD:/CC/AGENTS.md、元2641の最新note/spec、原GitHubのAGENTS.md/CLAUDE.mdを読み、元の許可範囲を広げない。新費用/APIキーAI/Hosted予算変更/GPU/Civ6/本人PC操作/Terminal/UAC/直接子Codexは禁止。自分のtempはsafe-delete create/plan/別通常削除/finishを使う。既存の回収拒否や他担当の資源を片付けない。成果を自身のbranchへcommit/pushし、Windows GUIのschedulerはHidden/Wait/PassThruで実終了とcheckpoint原本を確認する。原2641の状態はスケジューラーに任せる。
