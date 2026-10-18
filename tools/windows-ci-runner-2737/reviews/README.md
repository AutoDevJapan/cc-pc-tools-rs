# 静的レビューの受領記録

ToDo2830の原応答を改変せず保存する。最初の応答は準備失敗で対象未読のため合格ではない。
後の2応答は同じ固定head bec99388の23ファイルを読み、Must0/Suggestion2/Nit0とした。
GitHub APIで版とtreeを開始・終了時に照合し、実機検査は行っていない。

原応答は実装担当のモデルを確認できないため、別モデルの条件を未確認としていた。
再開した実装担当が、既存job.jsonと各threadの正規rolloutのturn_context.modelだけを
照合した。実装はgpt-6.1-sol、レビューはgpt-6-lunaであり、モデルは異なる。
要求したモデル名だけで判断せず、実turn_contextも一致することを確認した。
認証値や原sourceを読んでいない。session全文はコピーしていない。
原応答の自己申告「GPT-6（Codex）」は原文を保ち、上記補足と区別する。

receipt.jsonに原応答のjob、SHA256、固定base/head/treeとモデル確認元を保存する。
後続の提案修正は診断探索の総entry/深さ制限と失敗eventの整合確認であり、
親2737の実VM・runner・公式CIの受入には読み替えない。

追加レビューToDo2833は固定b60e85eの6対象を全て読み、Must0/Suggestion1/Nit0。
追加原応答とhashはreceipt-2833.jsonに保持する。実モデルはgpt-6-lunaで、
job.jsonとthread01a14fc5-796c-7730-b1f5-477e47d0f779のturn_context.modelを照合した。
レビュー担当の検査実行は0件。提案は走査中取消しの決定的検査の追加であり、
実装不具合ではないため検査用APIを増やさず、現行の検査範囲をHANDOFFに明記した。
