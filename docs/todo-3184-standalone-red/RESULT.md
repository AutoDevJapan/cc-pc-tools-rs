# ToDo3184: 旧実装で単独ジョブの非表示漏れを再現

指定された1件を1回だけ測定し、期待するRedを確認した。コンパイルは成功し、非表示にするはずの `private-job-title-marker` が残るassertで失敗した。nextestの終了コードは100。

| 項目 | 実測結果 |
| --- | --- |
| 旧実装 | `bf60fb319626210e1cb914610e63e46c2eb34c79` |
| テストの取得元 | `4a0c61f594e600b00e0f3d03702974496aec388a` |
| 差し替えたファイル | `localai-web-rs/src/operations_product_identity_tests.rs` のみ |
| テストblob | `d2f8416afb882ca71ff2bb49112d2eb9de9474f4` |
| 開始 | 2026-10-21 05:04:42.1265158 +09:00 |
| 終了 | 2026-10-21 05:05:23.0245449 +09:00 |
| コンパイル | 成功、test profile、37.11秒 |
| 選択数と実行数 | 1 binaryから1件を選択して1回実行、91件を除外 |
| 個別結果 | 0成功、1失敗、対象テストの所要時間0.048秒 |
| 失敗箇所 | 差し替えたテストの453行目、`private-job-title-marker` の非表示assert |
| 実終了コード | 100（通常のcargo_gate経由のnextest） |
| ソースの照合 | 全1182ファイルが意図したGit blobと一致し、実行前後で変更なし |

実行したコマンド:

```text
cargo nextest run --locked -p localai-web-rs --lib -E 'test(mcp_standalone_job_keeps_all_private_links_when_its_primary_is_old_done)' --no-fail-fast
```

通常の `cargo_gate.exe` とsccacheを使用した。生成先は所有付き一時領域内の専用targetで、ビルドとテストの並列上限を10に指定し、ゲートによる負荷調整を維持した。コンソールはCreateNoWindowで起動した。

テストは443行目で単独job行の存在を確認し、444〜452行目で `todo-6` の不在を確認してから、453行目で失敗した。これらの前提assertを通過したことが、逐次実行順序と実際のpanic位置から分かる。構文エラー、欠落、先行assertの失敗ではない。454行目の `private-job-report-marker` のassertには到達しておらず、その成否は測定していない。

`board()` は固定時刻1,000,000msで架空の入力を処理する。古い完了済みのid6に `updated_at` を与えていない。テストが使う保護一覧はtempfile内の架空データであり、PCの保護一覧原本や顧客の資源は開いていない。

全文ログは `stdout.txt` と `stderr.txt`。stdoutは0 byteで、コンパイル、選択数、個別結果、panic、集計はstderrに全量残っている。開始・終了・引数・環境の明示指定は `run-start.json`、`run-exit.json`、`process.json`。実行前後のSHA256一覧とGit blob一覧、検査結果も同じフォルダに保持した。ログの改行を保持するため、このフォルダだけGitのテキスト変換を無効にした。

初回の展開前検査では、tracked属性の `eol=crlf` による別ファイルの改行変換を検出した。Cargo起動前に止め、一時的な属性用作業場所から変換なしのarchiveを作り直した。最終的に全1182ファイルをGitの生blobと照合してから、テスト1ファイルだけを重ねた。事前検出の記録は `archive-preflight.json` と `archive-initial-sha256.json` に残した。原資料と親の作業ソースは保持している。

この担当の対象は旧実装での1件の測定だけ。現在候補のGreen、自動検査、別モデルレビュー、マージ、配布は親3103が行う。
