# 新規2件の実配置と確認範囲

MCPサーバーの設計手順と、移動距離ごとのカーソル軌跡の手順を中央へ新規配置した。既存28件に加わる2件で、全16資料の完了ではない。原3048と既存の原本・バックアップを保持する。

## 出典と権利

6mileの掲載名hxl-code-reviewerの本文9092Bは、Anthropic公式の固定dbd4588版mcp-builderとbyte一致する。許諾根拠は公式の個別Apache-2.0通知11345B。archive側の11357B通知と区別し、公式の著作権表示と派生ファイルの変更表示を保持した。SDK・支持ソフト・サービスの権利や動作を受け入れたものではない。Anthropic照合は、指定16資料の候補の出所を補強するためである。

MengToの固定398cd41版pointer-trail-emitter本文10566BとMIT通知を保持した。距離の前の余りを引く配置式、上限後に完全な未消費距離を捨てる処理へ訂正し、Canvas2D/カメラ面/実描画の範囲を分けた。作者のdemo・画像・外部素材の許諾や実行は含めない。両件の原文は評価対象の資料と表示し、その命令を自動実行しない。

固定本文・通知・日本語の入口・変更点は[レビュー済みbundle](skills-3080-mcp-pointer-reviewed-bundle.json)に、個別のカタログidentityは[設計記録](skills-3080-mcp-pointer-designs.json)に保存した。

## 実操作とテスト

既存のnew-only installerで、未作成の2directoryへ10fileを作成し読戻した。SKILL.mdは参照がそろった後に公開され、既存Skillの上書きは0。配置後の既存3048 Go checkerは、明示2件の構造・短いdescription・参照・全file hash・正負例でPASSした。[配置manifest](skills-3080-installed-mcp-pointer-manifest.json)と[実結果](skills-3080-mcp-pointer-installation-evidence.json)を結び、JSONの整形変更と実ファイルのbyte列を混同しない。

Sonnet5.5/maxの3564は10fileと明示A/B/Cを確認し、Must2件を返した。親は原応答全文とjob/nativeを照合し、原文参照と変更表示を修正した。3569は固定V2の6変更file/全12hunkを読み、Must0。追加Suggestion1/Nit5は、詳細guideに既にある先頭c個の方式の入口補足と表記改善等で、理由を添えて保留した。レビュー済みV2から配置時の改変はない。

明示Aは検索MCPの設計と4つの受入条件、Bは余り/上限の数値例、Cは適用外の判断を確認した。自作Goモデルの5テストも整数間隔でPASSした。これは実MCPサーバー・実ブラウザ描画・CPU/GPU性能の成功ではない。UIへ使う際は同じ対象の画像と本人回答を保持する。

## Codexでの発見

実Codex0.163.0の新しいapp-serverでskills/list(forceReload)を実行した。匿名のmetadata328中、対象2件の中央path・description一致・enabled=trueを確認し、解析エラーは0。プロセス終了と出力回収も確認した。

scan traversal警告のmarkerは2件残る。context-budget markerが0でも、全context予算や暗黙のモデル選択を確認したことにはしない。[探索上限の根拠](skills-3080-discovery-limit-evidence.json)に記録した、非dot directory2015と上限2000の比較や、この警告の実原因との対応も未確定のまま保持する。配置後の新しいモデルターンは実施していない。

既存4件の比較は分類未確認で保持し、本文読書き0・容量不明。今回の新規2件の具体操作の通常審査を、中央全体の保護分類の復旧へ読み替えない。647件の取得拒否も維持し、同じ取得や別経路を試していない。

探索警告に伴う既存の発見漏れを調べるため、元3048の14対象と元3080の33採用identity（新2件を含む）もmetadataで照合した。重なりを除く46対象がすべて発見されenabled=true、解析0だった。既存名/pathは保存せず、公開済み対象集合のSHAと件数だけを保存した。この時点の管理対象のmetadata確認であり、全旧ファイルのbyte一致や追加前後の完全な同一性は証明しない。
