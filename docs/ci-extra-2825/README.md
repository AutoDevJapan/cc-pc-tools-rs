# PC用CIを1件だけ追加実行した記録

既存のリポジトリ専用runnerを別名・別ポート・別作業場所へ分け、CPU 2・メモリ10GiBで1件を実行した。正式なGitHub jobの成功と、runner登録・コンテナ・派生イメージの撤去を確認した。

- 元のコード: `2f66679a854de1491052e3fbaa5e6bd8407d3c4f` の `tools/linux-runner-bridge`
- `profile.patch` は一時設定の全変更。通常のsource照合・隔離・Cargoゲートを保ち、`run.sh --once` の成功・失敗後に終了する。
- Goのrace検査・vet、Linux上の単体検査、実Windowsホストの登録96異常系で検証した。最後の異常系では2.985秒で終了し、常駐listenerへ移らなかった。
- 別モデル確認で見つかった「異常終了後に常駐へ移る」問題は修正し、Must残なしを確認した。
- 実行結果: https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/39128322214/job/117678062170

バイナリの指紋と回収結果は `manifest.json` に記録した。