# 新しく生成した質問画像の用途資格

通常のguard分類が未分類の画像を、一律に安全へ分類し直さない。
今回の本人依頼のために正規imagegenが新しく作った、秘密や顧客内容のない確認画像だけを、
特定のオーナー質問へ結び付ける。通常の質問送信と同じ設定済み本人・guild・forumを使う。

```text
unattended-runner.exe --check-generated-question-evidence --evidence <evidence.json>
unattended-runner.exe --register-generated-question-media --evidence <evidence.json>
discord_ask.exe ask ... --body-file <body> --decision-file <decision> --attach <original-image>
```

最初のコマンドは原証拠の読取検証だけを行う。次のコマンドが用途別の資格データを登録する。
通常のaskは登録を元pathと質問scopeで引き、同じ原証拠を再検証する。
新しい画像にも同じコマンドと形式を使う。画像ごとのコード追加・ビルド・配布は不要。
質問本文やdecisionを変える場合は、新しいscopeの証拠を検証して登録し直す。

証拠JSONのdomainは`owner-generated-question-evidence-v1`。必要な項目:

- `scope`: todo_id、question_sha256、decision_sha256、owner_id、guild_id、forum_id。
  本文は正規CLIが使う改行を正規化した全文、decisionは同CLIの正規JSONのSHA256。
- `session`、`rollout_path`、`session_record_sha256`、`assignment_event`。
  当該ToDo専用の原Codex sessionの先頭メタデータと、最初の実ユーザー依頼の行番号・SHAを指定する。
  依頼は`ToDo<番号>`または`ToDo番号<番号>`（任意の接頭語`担当`）で始める。
  専任担当への定型文「本人の最新依頼を実行する専任担当。」がある場合は、1回だけ除いた最初の行を依頼の見出しとする。
  この行の引用外にToDo番号を1個だけ明記する。説明を添えた見出しや空白・コロンで区切った番号にも対応する。
  本文の別の行・引用・コードから番号を探したり、未知の導入文を読み飛ばしたりしない。
  元ToDoは生成記録のtodoと実ユーザー依頼の両方で一致させる。
  後の別件に触れた文や引用へ付け替えない。AGENTSと環境情報の導入文は作業依頼と区別する。
- `proof_path`、`proof_sha256`: 正規の生成証拠をまとめた原JSON。
- `images`: 元path、sha256、size、record_path、record_sha256、prompt_sha256、option、revision、order。
  生成記録はnew_generation/image_gen__imagegen/functions.exec、同じsession・cell・順番・元pathとし、
  requestはpromptと任意のtransparent_background、inputImagesは空とする。参照画像や既存原本は対象外。
- `events`: 実行call/resultと、非同期の場合のwait call/resultの行番号・SHA・kind・name・call_id。
  `exec_cell_id`も指定する。原rollout内の対応、実行時刻、imagegen呼出しを照合する。
  結果にあるPNGのdata URLと出力hintの元pathを解析し、元画像の全バイトのSHA・サイズと一致させる。
  execの文字列にimagegenがあるだけでは許可しない。許可した構文を解析し、実tool戻り値だけが
  generatedImageへ渡ることを確認する。画像の差替・別load結果・組み込み関数の差替は拒否する。
  元rolloutは最大256MiB、1行36MiBまで読む。上限を超えた場合も分類を緩和しない。
- `created_at`は実生成callの時刻とタイムゾーン・小数秒を含めて一致させる。
  表示用に時差を足したり、小数秒を切り落とした値は拒否する。`expires_at`はそこから7日以内。
- `content_review`: new_generation、fictional_data、no_customer_or_secret_contentをtrueとし、
  内容を確認した根拠をreferenceに記録する。これは作成担当の内容確認記録であり、
  検証器が画像の意味を自動判定したことにはしない。

生成sessionと画像原本は固定のCodex保存先に限定し、証拠JSON・生成記録には既存guardの適格判定を要求する。
コピー先を元画像として登録できない。資格は元ToDoが継続中の間だけ有効とし、停止・完了後は拒否する。
別ToDo、別の本文/decision、別本人/送信先、変更済み画像、要約だけによる元path/ToDo・tool名・応答バイトの差替は拒否する。

用途資格の署名鍵はWindows DPAPIで同じユーザーへ結び付け、固定の用途別保存先だけで使う。
鍵や資格データは原子操作で新規公開し、既存データを上書きしない。同じ登録は再試行できる。
署名が正しくても原証拠と元画像の検証を省略しない。原Codex監査ファイルと本人PCのユーザー実行環境を
信頼境界とし、その環境自体の侵害まで検出したと主張しない。

proof JSONはguardと全バイトSHAを確認する補助資料であり、その全内容を解釈して権限を作るものではない。
生成記録のprompt本文・option・revisionは申告された記録をSHAで固定する。動的loadで組み立てた
prompt全文や画像の意味を自動的に再評価したとは主張しない。元ToDo・tool戻り値・元path/画像バイトの
差替は実native記録との照合で拒否する。送信の確認までproof・生成記録・原rollout・元画像を保持する。

DPAPI鍵を失った場合は安全側で拒否し、未検証の資格を上書きしない。先に元の同ユーザーの鍵を復旧する。
復旧できない場合は、AI担当が用途別ストアだけを履歴として退避し、同じ固定保存先で鍵を新規作成して、
未期限の原証拠をcheck/registerし直す。画像原本の移動・コピー、一般guardの変更、元の質問・回答待ちの
削除を復旧手順に含めない。原証拠が不足・期限切れなら再登録を成功扱いにしない。

一般guardのclient_resource/classification_unverified、原保護設定、CI入力やソフト更新の資格は変更しない。
資格結果をlogや任意ファイルの送信へ流用しない。UIの本人承認待ちもこの資格では解除しない。

新しい生成の最小wrapper（functions.exec内）:

```javascript
const result = await tools.image_gen__imagegen({prompt:"仮データによる確認画像",transparent_background:false});
generatedImage(result);
```

promptの文字列・loadからのreplace/concat、通常のstore、直接awaitの戻り値出力に対応する。
並列生成は各tool promiseをPromise.allSettledへ順に渡し、同じ順のforでfulfilledのresult.valueを
generatedImageへ渡す。画像の戻り値をload経由で置き換えない。未知のJavaScript構文は拒否する。
