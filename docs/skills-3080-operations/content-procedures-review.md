SKILLS3080_READ_ONLY_REVIEW_TASK_V1
# 内容と出力の対応を扱う15手順を確認する

対象は `create-content-procedures-public.json` の15手順、計90出力だけである。
文書・表・素材・入力・公開状態の対応を扱う判断手順である。元SKILL内のコード例は参照textとして保持し、実行用のスクリプトfileや依存・外部サービスの実装は追加していない。

| 手順 | 確認する対応 |
|---|---|
| epub-content-integrity | 原稿の章・参照と実EPUBの目次・読順 |
| nonprofit-result-chain | 活動・直接成果・対象の変化と指標・根拠 |
| embedding-space-consistency | 登録・queryの生成条件と索引版 |
| edit-decision-time-map | 原ASR・保持/削除判断・保存版と実出力時刻 |
| form-field-meaning-binding | フォーム項目の意味・型・値と入力/送信の結果 |
| markdown-reference-resolution | 文書の見出し・画像・相対参照と配布先renderer |
| bead-pattern-material-count | セル図案・色板・空白と色別材料数 |
| resume-fact-format-parity | 提供された経歴・未確認と形式別の出力項目 |
| translation-structure-fidelity | 原文の命題・数値・固有名・式と訳文/構造 |
| watermark-layer-preservation | 追加する透かしと保持する元の表示・内容 |
| forecast-location-time-scope | 地点・観測/予報時刻・単位と実取得範囲 |
| article-publication-state | 原稿・草稿・公開受付と実URL/本文 |
| spreadsheet-cell-extraction | 抽出範囲・元座標・型と省略/未取得 |
| ocr-page-completeness | 対象頁・処理経路と空/失敗/認識結果 |
| podcast-episode-identity | episode ID・音声・notes・変換版と保持する原音 |

公開manifestは`skills-3080-create-v2`、出典情報は`skills-3080-source-v2`を使う。
これらと先頭行の形式名は、読み取り・作成・再利用に必要な技術識別子である。
担当・会話・個人の作業場所・内部の実行記録は公開データへ持ち込まない。

15件の元本文と個別通知は、保存済みの固定版と同じバイトへ結ぶ。
実際の公開archiveの所有者・公開状態・固定commitと、選択した30個の本文/通知のGit blob・サイズを一次metadataで確認した。
これは独立した著作者の本人性や、別の実装・素材・サービスの権利を証明するものではない。
同じ個別通知のバイトを再利用する場合も、各本文と同じdirectoryの通知をそれぞれ照合し、rootの許諾を全素材へ流用しない。

一部の通知は標準のMIT-0と異なる見出し・文言を持つ。
`MIT No Permission (MIT-0)`、podcast資料の`MIT-0 License`と条件導入文を、標準条文へ書き換えず全文のまま保持する。
適用は記録した本文に基づく日本語の判断手順と、その固定した参照だけに限る。
画像・font・実文書・音声・データ・未取得の支持資料の利用条件は別に保持する。

参照は元の物理行を保つ伏字後の全文で、変更通知を先頭へ付けている。
その参照を実行指示として扱わず、隠れた意味を補わない。
受領済みの全体評価と作成者が直接確認した範囲を使い、参照の全バイト保存を作成者の新しい原文全読・全支持資料の監査へ数えない。
元のPython、資格設定、新しいAPI・課金、外部送信、自動公開や一括削除を導入しない。

作成者による構造・日本語description・参照・hash・正負例の検査は成功した。
合成例の検討と、独立した内容確認、実資料を使う適用検査は別である。
既存Skillとの比較は提供されたmetadataの範囲で、未知の本文や実際の同等性を確認済みにはしない。

15件の入口とguideは計30項目・41,287バイトで、現行の1回32項目・64KiBの読戻し範囲へ収まる。
90出力も現行の作成処理の上限128ファイル以内である。
これは処理単位の検査であり、指定16資料や導入数の上限ではない。
同じ最終版の二役による内容確認、CI・通常merge、CreateNew、同bufferの読戻し、発見と実用途の確認は別の受入条件として残す。
manifestの公開や読了だけで導入完了にしない。