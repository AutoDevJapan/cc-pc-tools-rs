# GoDD Pressの解析9件の実害を固定ソースで確認する

親2642の有限check工程。既済2739の3差分レビューを繰り返さない。未評価の9件だけを判定する。原製品ソースの変更、PR更新、merge、配布、他CIの起動を行わない。親の完了を待つ依存を作らない。

## 固定対象

GitHub実repo ID1386702183 / AutoDevJapan/GoDD-Press / PR175。解析candidate `b2b6d47b7f9e17617d16a9079a70de272bc3c04c`、tree `dac21168dfdb55b007da5c06f36c05e66de0a9f7`、base `4dc5c332d54d19325f122df1f0e4a52e9dd3a629`。開始と終了に実owner/private/admin/nonfork/同head+base repo/open/refをAPIで確認する。headが動いても旧解析を新しいheadの結果へ読み替えない。

本人2026-10-15の独立工程は、所有確認した原GitHubから価値のある未完工程を一つ実装・検査・別モデルレビュー・通常PRへ進めるもの。原29/2191の停止/依存/未完は保持する。今回の有限判定を製品全体doneにしない。原製品フォルダ、保護一覧、guard、owner-work登録や登録manifestには触れない。ホストへ製品checkoutを作らず、必要なGitHub blobをRAMだけで扱う。

## 入力と秘密の保持

公式CodeQL run38893812340は全4言語とgateの5job成功、全analysisがcandidateSHA/error空。結果は `rust/hard-coded-cryptographic-value` 5件、`js/functionality-from-untrusted-source` 4件。Actions/Python0件。

指定証拠rootは `D:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459`。同rootの `repo-1386702183-analysis-*.private.sarif` とofficial-final/log-proof、上位の対応manifestを使い、原bytes/SHA256を照合する。別SHAのopen-alert一覧を候補結果へ混ぜない。

SARIF本文・snippet・位置・fixture識別名・値をモデル/stdout/報告へ出さない。rule/count/hashだけを出す。位置からGitHub blobへの対応はプログラムのRAM内で行い、秘密情報を読む手法を使わない。raw APIエラーや原ログもそのまま表示しない。実害確認には単なるpathやtest属性では足りない。意味の判定に必要な情報は、非秘密の制御フローと条件に限定して要約する。元の内容を表示して制約を回避しない。十分な意味解析ができない部分は未確認を正確に返す。

親のAPI照合では全9artifact blobはbaseとcandidateで一致。JS4件はtests下のHTML、Rust5件は位置より前にtest属性があった。これは関数への包含や到達不能性、無害性の証明ではなく、同じpath判定だけで完了しない。

## 判定と成果

1. 各rule群を実害確認/根拠付き非該当/追加検証必要へ件数で分類する。総数9と対応を保つ。severityやbase同一だけで判定しない。
2. Rustは固定値がテスト関数・cfg(test)内部に実際に包含されるか、production暗号処理から到達するか、外部データや配布物へ出るかを確認する。単に先行test属性を見つけたことを包含証明にしない。値や識別名を表示しない。
3. JSはHTMLの入力元、scriptの取得元と利用先、fixtureの公開/配布への混入、テスト実行時の外部通信・不変な検証/隔離条件を確認する。testディレクトリ名だけで許容しない。外部サービスへの書込みやscriptの勝手な実行はしない。
4. 必要な意味検査は割当資源の範囲で行い、重いCargo/GPU/Chrome/containers/VMは起動しない。検証用に新規コードが必要ならGo/PowerShell。GoDDアプリの変更は親だけがRustで行う。元全件検査を重ねない。
5. 確認できた実害の具体的な修正方針と、親が元箇所をRAM内で再解決できるprivate対応証拠を保存する。ソースsnippet/秘密/生の識別名を親の会話や最終返信へ出さない。SARIFを削除・除外・dismissしない。

最終返信にはrule/count、非秘密の根拠、実検査exit/hash、未確認事項とprivate証拠の保存先を返す。親2739のMust0を今回9件の安全性へ広げない。判定0件を実害0件と書かない。親の最新noteを置換せず、必要なら保存済みspecと同じ元2642へ追記する。自身のbranchへ再開記録をcommit/pushし、scheduler checkpointを実終了付きで保存する。GUI exeはStart-Process -WindowStyle Hidden -Wait -PassThruで終了コードを確認する。

起動時にD:/CC/AGENTS.md、原GitHubのAGENTS.md/CLAUDE.md、元2642の最新note/specを読む。APIキーAI/新費用/Hosted予算変更/GPU/Civ6/本人PC操作/Terminal/UAC/Discord送信/子Codex直接起動は禁止。自分の一時領域はsafe-delete create/plan/別通常削除/finish。原2710/2648/2730の未回収資源は変更しない。有限結果を返し、親2642や通常CI担当の完了を待たない。
