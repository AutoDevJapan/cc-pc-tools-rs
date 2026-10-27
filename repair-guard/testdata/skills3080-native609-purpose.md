# 同じ3686で、長いセッションログの修復をWindows版へ反映する

元3080 / Issue283の同じ16資料の拡張に必要なソフトウェア更新である。
元の限定修復a、既存のOAuth枠、担当別の作業場所、元資料・旧版の保持を保つ。
新しい本人判断、PC操作、費用、APIキー、一般guardの解除は要らない。

親3080の修復PR609は、固定head `4862bacfc2dea41d56b51e03931fca16614254d7` / base `86fc9f06ca01a6cb8a11720fcfb5a884c5209f56` で別セッションのセルフレビューとラバーダックを受け、双方Must/Suggestion/Nitが0だった。
実Luna/max・原返信一致を親が独立照合し、原票を保持している。
Windows CIは共有実行機の先行作業を待っているため、親が全CI成功と通常マージを確認するまで配布しない。
この依頼書の公開、担当の起動、準備、ビルド、レビュー、CI、マージ、配布、配布後の元request成功は別々に記録する。

修復は、正規ログが256MiBを超えても同じhandleの最初の長さまでをstreamで読み、起動時指示の先頭4MiBと記録上限、元のsession/cwd・時刻・入力・最終SHA・call ID・重複・非reparse・保護を保つものだ。
作成callにも同じsnapshotを使い、読取専用の確認事項は専用見出しと非空本文、同じPUBLIC入力の完全pinへ結ぶ。
CIのテスト用フォルダーは既存の実体path fixtureへ直し、合成GOTMPDIRの別名で旧4失敗から新4成功を確認した。
原ログ・原job/spec/prompt・原返信を切り詰めたり書き換えたりしない。

配布前に、次の実条件を解決する。

1. 親の4033と4115は親3080のレビューであり、あなた3686のソフト更新確認へ親を偽装して流用しない。
   現行 `skillsRuntimeQualification` は `skillsReceive(..., false)` から、3686に正規登録された実レビューと `SKILLS3080_IMPLEMENTATION_REVIEW <head>` / `SKILLS3080_CODE_ACCEPTANCE` を照合する。
   同じ3686の既存レビュー3712と、必要な既存の別セッションを通常の共有ToDoから使い、実作者・登録時origin・実launch親・実Luna/max・原返信・同じ最終headを保つ。
   全修正指摘を解消し、既読の親レビューとソフト更新用の検証を同じ成功にしない。
2. 親の配布前の独立読戻しでは、現用 `scrub391.exe` は13315072B、SHA `16386d55e418ce271cfed923edede1e7472bb93aa691b15409bcdb99753b9899`、Go Windows/amd64 / module `github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard`、VCS revision `61de98373f8cb481302394be11d7d75cfef08a52`、modified=falseだった。
   この値を現在値と決めつけず、実物・公開mainのsource・既存の通常配布記録を再照合する。
   PR609のsourceにある `skillsGuardBaselines` はこの組をまだ含まないので、新候補の更新確認を成功と推定しない。
   必要なら、候補作成前の独立した読戻しを先に公開記録へ保存し、実証された既存ソフトの組だけを、通常のレビュー・CI・マージで受入する。
   callerが任意の旧SHAを許可する方式、unknownの許可、現在のsourceを候補で自己申告する方式にはしない。
   原のbaseline・旧backupと既存発行の由来を保持する。
3. この登録は同じ有限な用途限定の修復・配布であり、必要なsource修正も自分の既存worktreeで進める。
   自分を新しい司令塔へ変えて引継ぎだけで終えず、正常な別モデルレビュー・CI失敗の原因修正・通常マージ・実Windows更新と読戻しまで追う。
   Root3080のCC_JOB_IDやwriter、別担当のreview parentを偽装しない。

実施時は次を守る。

- 登録spec、実job、作業場所、実Git所有者AutoDevJapan/cc-pc-tools-rs、guard、AGENTS/CLAUDEと最新の本人指示を読む。
- 自分の途中成果と元の登録scopeを保持し、現用の並行更新・旧backupを確認する。
- 固定PUBLIC sourceから正常な実装を準備し、まとまった必要修正だけを行う。
  必要な元PR609のマージを待つ間は、ソフト更新の条件・自分の既存レビュー・独立した実物照合を進める。
  未マージの候補を、データの資格や一般アクセスの根拠として実行しない。
- WindowsのGo CLIだけを、受入済みのmerged sourceからビルドする。
  `--verify-skills3080-update` はあなたの実writer3686、source/candidateとも実actorの作業場所内、reviewed guard全ビルド入力一致、tracked変更なし、Go buildinfo modified=falseへ結ぶ。
  必要なCodeAcceptanceがない、現物が未知、並行変更がある、CI/マージ未達なら、その部分だけを止めて原因を直す。
- 旧版を保持する既存の通常Windows置換を行い、候補・旧物・backup・配布後の実hash/sourceと使用可否を再確認する。
  必要な管理者処理だけCC-AdminBrokerを使い、UACやconsoleを出さない。
  検証前のforce rollback、原ログ切詰め、排他削除や正規審査の迂回をしない。
- 重いCargo、GPU、Civ6は使わない。
  同じ最終sourceの成功した必要テストを再利用し、Goの関連runtime・利用側・負例と配布の実受入を確認する。
- 親へ実job/spec/source/reviewed head/merge/CI・原返信/実モデル・新旧hash/backup・正常verify・配布後CLI・未達/次の手を返す。
  本人の対応が不要ならメンションやnotifyを付けない。

型付き入力に残す旧 `create-travel3-214717.json` / SHA `11070b772c86f7ffb459be2f75eaf6d0a436ce9dfdf0d9247eb81c6e433ede51` は、同じ有限な拡張の結び付きを示す既存PUBLIC資料であり、作成をやり直す指示ではない。
旧3件/18file・原review4083・旧issuer/ID/原本と実導入147件を保持する。
今回未配置の次3件/21fileは親が担当し、operation SHA `5df4ba1cf1946271e5d03d6b6e02009f0c29d2c8025a9b8be0161a3d8aefae70` のCreateをあなたは実行しない。
未知の中央本文/backup、原保護台帳、取得保留647件、未取得428件、別部署/他Org/UIへ範囲を広げない。
原の全16資料/73220出現は未完了である。
親が更新後の同じ受領requestと新3件の配置・同ID/buffer・発見・実適用・最終レビュー/CI/マージを検証するまで、登録や配布だけを全体完了にしない。
