# PR148送信停止を解消する正規修復案の限定調査

原2736の有限なread-only下位research。実装・検査担当2790とreview2797の循環修復は完了済みで、同じ2797の固定spec/モデル/範囲を変えない。新review ToDo・直接Codex・孫subworkは作らない。

## 目的と今回の終端

原2736が同じ技術停止条件を時間だけで再確認する状態を解消するため、下記の既知原因について、正規の修復候補とその実施境界をコードの根拠付きで返す。自分で調査できる技術事項をオーナーへ再質問しない。修復担当が実在することや、修復済みであることを推測しない。

現sourceを読み、必要なら未適用の候補差分を最終返信のコード欄へ示してよい。ソース・稼働guardの編集、差分の適用、commit/push/PR作成/merge/配備はこのresearchの範囲外。原2736が候補と権限境界を受け取り、未受入のまま実送信しない。結果保存・ToDo更新・commitをread-only担当へ要求しない。最終返信が正本で、原2736がcc-jobsの原応答を保存・配送する。

## 固定の対象と根拠

対象はGitHub実owner AutoDevJapan/repo1406818713のcc-pc-tools-rsの、原2736保存source d668ebd6ff76fea594971bd449ce5ba8e6dd5203。

読取対象はこのsourceの次のものだけ。

- `cc-redact/src/lib.rs`、`cc-redact/tests/redact.rs`、`cc-redact/Cargo.toml`
- `cc-gh-gate/src/lib.rs`、`cc-gh-gate/README.md`、`cc-gh-gate`内の関連テスト（正規の入力/JSON送信とredactor接続だけ）
- `tools/product-ci/codeql_bootstrap.go`の`githubMutationCommand`、`tools/product-ci/codeql.go`の`mutateJSON`
- `tools/product-ci/pr148_route.go`、`tools/product-ci/pr148scope/{tree,workflow}.go`
- 原2736の`TODO-2736-CHECKPOINT.md`先頭3節と、本指示内に記載した確定事実

未知のフォルダ・原製品folder・環境変数・秘密・認証profile・配備先・原log/source/SARIF・原保護台帳やguard設定にはアクセスしない。公開一次資料が必要なら、既存source/READMEの根拠を優先し、ネットワークやブラウザは使わず不足を明記する。

## 再現済みの事実（再実行しない）

製品はAutoDevJapan/civ6-banseiki、repo1392152644、PR148、open/draft、同head/base repo。
head75a4051305b6f47c70ddb59a8d2bf9015666f12c、treececb9cd535d135a1eb4407882db532c325f42860、base1100676d1ed421794c252fb4b15ed3d954e21a5b、branch codex/todo2640-workspace-doctests-20261015を保持する。

原2736の通常helper PR189は703e5edを元にmerge3af45ee2で通常merge済み。別モデルMust0を受領し、PR head全9checkは8success/1skip、merge head全8checkは7success/1skip。これらは製品CIの成功ではない。2790のWindows受入も完了済み。既存検査を重ねない。

正規gh_gate/同OAuth/workflow scope経由で、レビュー済み3workflowをJSON UTF-8のgit tree contentとしてPOSTした初回操作は、全tree比較が`proposal bytes differ: .github/workflows/live-chain.yml`を検出してexit1。commit POST/ref PATCHへ到達せず、製品refは元75aのまま。次の二つの診断も成功済みで、繰り返さない。

1. 認証値なしのRust fixtureで、既存`cc_redact::Redactor::new().text("with:\n  persist-credentials: false\n")`が、このfalseを`[REDACTED]`へ置換することを実測した。秘密や環境変数を読んでいない。
2. read-only Go診断で元treeをGit object IDから再現し、共通生成した3workflowのこの真偽値だけを置換した予測tree bb477c675dee11b8a232ceddcb71221c4ac38264がGitHub上の実unreferenced treeと完全一致した。他path/mode不変も確認した。正規生成案treeはa6851bd86e08b0f89eca3c4b30fbf7d4e292244f。

実gateの記録SHA256は6EBB40B8B580FDAE37C7F611E4FFBBFE1AD1246AB593FE1AA787749E122B7DFE。新binaryの配布・修復受入receiptは未確認。原資料は保持済みで、本文を読み直す必要はない。

## 候補に必要な条件

- 原の真偽値とworkflow bytesを正規の同じ経路で保持する修復を検討する。別API/認証/符号化/false式等への表現変更で、止まった同payloadを通す案は採用しない。
- 稼働中の送信フィルター・原保護設定・allowlist/manifest・共有resident bridgeの許可の変更、無効化、特例付与は行わない。原2736のguard不変更条件をこのresearchで広げない。
- 認証値の実読取、未知宛先への送信、既知token/credential assignment/credential JSON field/environment literalの保護を弱める一般例外は候補にしない。
- 既存の非秘密設定を誤判定する場所を、呼出し元からJSON処理とtext処理まで具体的に追う。source候補が保護仕様の変更に当たる箇所は明示し、現scope内で適用可能と勝手に断定しない。
- すべてのbooleanやcredential名のfalseを無条件で素通しする案、新しい許可リストで送信可否を変える案、guardを読まずに書く配布経路は採用しない。
- 候補の成立に必要な回帰検査、既存の保護を維持する負例、同じ最終sourceで再利用できる結果と新検査を分ける。Cargo/Go実行、CI、サービス、runner、コンテナ、GPU/Civ6、PC再起動は0。

## 返信形式

1. 呼出し/値の変換経路と、誤判定が発生する行・関数の根拠。
2. 正規の修復候補を1案。未適用の最小差分、または修復できない場合の具体的なコード上の理由。
3. 保持する保護、変更になる保護、必要な受入/正規実施担当の条件。実在未確認の担当を割当済みとは書かない。
4. 必要な限定テストと負例。実行0・未実証を明記する。
5. 原2736へ返す次の1工程。原2640の完了を待つ逆依存、保護scopeの手書き変更、同じ停止操作の反復を作らない。

1回の読取で終わるHaiku作業ではなく、上記calleeをまたぐ調査なので既存OAuth Sonnetを使う。AI APIキー/新課金/新契約/直接子Codexは使わない。元880/134/995/996の停止・依存・未完は保持する。
