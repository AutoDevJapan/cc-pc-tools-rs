# 元3884の追加レビューの実行記録

`record-recovery private-review-runtime-metadata` は、元3884の新しい差分を
既存監査役が読んだ一件だけを、既存の資格付き実行記録readerへ接続する。
原3649・3879の読了をやり直したり、原2191のレビューへ置き換えたりしない。

固定するのは、通常OMB APIで返されたassignment ID、開始・終了時刻、
原最終応答のUTF-8 SHA256である。modelとeffortは固定値にせず、対応する
正常Codex APIのthread/turnと最終応答の全byte一致を確かめたうえで、
同じ資格・同じWindows handleのcontextだけから観測する。

既存readerのsource・Codex実行物・物理path・所有・読取前後の同一性照合と、
未知値のunavailable、本文やpathを出さない出力を維持する。
旧レビューの固定配列と取得入口は変更しない。このadapter自身の新しい
buildを読んだ一意なレビューだけは、既存のcompiled revision照合で追加できる。
固定IDを引数や設定で差し替える入口は作らない。

受入条件:

- Given: 固定原応答・時刻・回数と正常native応答が一致する。
  Then: 元3884の対象一件と、存在すれば同buildの追加レビューだけを観測する。
- Given: ID、時刻、原応答のいずれかが異なる、または重複する。
  Then: unavailableとし、原nativeの読取資格へ進めない。
- Given: CLIへ引数を渡す。
  Then: 取得関数を呼ばずexit 2を返す。

source保存、合成テスト、設定成功だけでは実モデルの証明にならない。
この入口は一般guard、原状態・依存・受付、private製品のsourceや実要求の
閲覧資格を変更せず、製品のレビュー・テスト・配布全体を完了としない。
