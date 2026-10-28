# 元3686：原sourceと改訂後namespaceの対応を照合する

親の実manifest 5ab55cfca8778374e86862905e9c39d5bfe11bea2ae6e3e33ec82f82e7607f89
は改名した3案を含む。PR615は個別notice・固定改変表示・Windows配布を修復したが、
原basenameだけで参照先を決める条件では、この実4案28出力の資格が通らなかった。
親の追加指摘を取り込む前に通常マージした点は修正し、全28の受入完了とは報告しない。

parse-qr→parse-payment-qr、payroll→payroll-batch-reconciliation、
payment-status→payment-status-reconciliationの原pathは変更しない。
同じreviewed manifestに既に存在するnamespace/references/source.jsonのsourceを
strictに読み、repository/commit/path/SHA/acquisition/個別licenseの全項目を選択sourceと
一致させ、reformulation_writerを元manifestのwriter jobへ結ぶ。metadata自体のbody SHA、
同namespaceのSKILL.md・全byteのLICENSE.txt・masked原参照を要求する。
同sourceの複数対応、矛盾、不在、別出典/notice/writer/namespaceを拒否する。

元sourceと新namespaceが異なるbatchでは、同名のrlm-controllerも含む全masked primaryに
明示metadataを要求する。reformulation_writerのない旧metadataは新たなaliasを与えず、
既存basename照合だけに残す。旧旅行3案の受入済みmanifestは同byteのまま通る。

claims照合と実saved object照合は同じ対応を使う。metadataは原本文の代わりではなく、
元catalog/index/tree/commit全体pin、原role・bytes・SHA・Git blob、元notice全byte、
原physical line/marker・末尾LF・UTF8/NUL/CR・実可視と伏字の双方を引き続き要求する。
任意の同名参照、support_text、script、改訂名の改名権限や新しい許諾へ一般化しない。
原コーパスや中央への書込みをテストから始めず、実manifestの純粋回帰と合成原本で確認する。

成功済み3712/3896/3740/3877、親の成功済み内容レビューと各原spec/job/native/原返信は
凍結したまま保持する。未受入だった既存3870(self、原Must2)と3876(rubber、原Must1)を
同じ3686の既存Luna/max・両owner_requestで使い、旧originと今回launch親を分ける。
新更新のruntime資格はこの2taskを役割ごと固定し、同正確head/domain・全指摘0・実nativeを
要求する。既に配置したPR615の同一candidateに対する3740/3877は、その固定source/
reviewed head/SHAが全て一致するreadonly readbackだけに保持し、新apply/recoveryに使わない。

候補生成前に独立観測した現用50c817a8/source34cd8877の一組だけを両既存updaterへ保持する。
旧9beと元実backup/ID/ACL/source、647取得保留・428未取得・未知中央を保持する。
同最終版の独立2確認・CI・通常merge、clean merged sourceから一度生成、正常verify/
apply/配置後CLIと、親自身の実4案28buffer/ID・発見・明示適用を別々に確認する。
