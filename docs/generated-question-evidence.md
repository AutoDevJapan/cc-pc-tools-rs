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
  原Codex sessionの先頭メタデータと、`ToDo<番号>`で始まるユーザー依頼の行番号・SHAを指定する。
  元ToDoは生成記録のtodoと実ユーザー依頼の両方で一致させる。
- `proof_path`、`proof_sha256`: 正規の生成証拠をまとめた原JSON。
- `images`: 元path、sha256、size、record_path、record_sha256、prompt_sha256、option、revision、order。
  生成記録はnew_generation/image_gen__imagegen/functions.exec、同じsession・cell・順番・元pathとし、
  requestはpromptとtransparent_background、inputImagesは空とする。参照画像や既存原本は対象外。
- `events`: 実行call/resultと、非同期の場合のwait call/resultの行番号・SHA・kind・name・call_id。
  `exec_cell_id`も指定する。原rollout内の対応、実行時刻、imagegen呼出しを照合する。
  結果にあるPNGのdata URLと出力hintの元pathを解析し、元画像の全バイトのSHA・サイズと一致させる。
- `created_at`は実生成callの時刻、`expires_at`はそこから7日以内。
- `content_review`: new_generation、fictional_data、no_customer_or_secret_contentをtrueとし、
  内容を確認した根拠をreferenceに記録する。これは作成担当の内容確認記録であり、
  検証器が画像の意味を自動判定したことにはしない。

生成sessionと画像原本は固定のCodex保存先に限定し、証拠JSON・生成記録には既存guardの適格判定を要求する。
コピー先を元画像として登録できない。資格は元ToDoが継続中の間だけ有効とし、停止・完了後は拒否する。
別ToDo、別の本文/decision、別本人/送信先、変更済み画像、偽造した要約・tool名・応答バイトは拒否する。

用途資格の署名鍵はWindows DPAPIで同じユーザーへ結び付け、固定の用途別保存先だけで使う。
鍵や資格データは原子操作で新規公開し、既存データを上書きしない。同じ登録は再試行できる。
署名が正しくても原証拠と元画像の検証を省略しない。原Codex監査ファイルと本人PCのユーザー実行環境を
信頼境界とし、その環境自体の侵害まで検出したと主張しない。

一般guardのclient_resource/classification_unverified、原保護設定、CI入力やソフト更新の資格は変更しない。
資格結果をlogや任意ファイルの送信へ流用しない。UIの本人承認待ちもこの資格では解除しない。
