# ToDo3154: 固定版のRed検査（途中保存）

元ToDo3081 / Issue277の独立check。製品ソースとテストは変更していない。
52bc2dd900b68bfee86e3f5acf9d7378d0c760dbに固定して指定3コマンドを実行した。
すべてコンパイル成功後に実exit100、選択6件は0合格・6失敗。

| コマンド | 実結果 | 最初の失敗箇所 |
|---|---|---|
| cc-job-policy / model_effort | 0合格・1失敗・2選択外 | `same_model("opus", "anthropic/claude-opus-5-5")` がfalse（5行） |
| cc-jobs / model_effort | 0合格・2失敗・2選択外 | managed jobのmax引数が無い（45行）、未知Claudeにdefault tierが無い（69行） |
| cc-jobs / parity | 0合格・3失敗・388選択外 | 単独reviewの引数競合（1947行）、旧担当のstatusがqueuedのまま（1968行）、`run -h` の旧baseline不一致（11876行） |

単独reviewの実失敗は `--model and --auto-model are mutually exclusive` のErrをunwrapしたもの。
登録後のmodel/作者未確定/作者一覧のassertに到達しておらず、作者の不正推定を実証したとは数えない。
別名検査のSonnet/preview、managed検査のowner overrideとClaudeのループ、
unknown Claudeのfast_mode=false、queued検査のmissing/unlinkedは未到達。
helpの`run -h`より後のケースも未到達。原ログに全選択結果と実panicを保存した。

次に5a82f08a57d0774c4e01fb0e8760a57a750a13c2へ固定し、同じtargetでfinite_reviewを1回実行する。
この2版の差分は`cc-job-policy/tests/finite_review.rs`の追加だけ。
Greenや修正後の実装を検査した結果ではない。mainへマージしない。
