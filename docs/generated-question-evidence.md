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

停止した段階を調べるときは、同じcheckに`--diagnostic`を付ける。

```text
unattended-runner.exe --check-generated-question-evidence --evidence <evidence.json> --diagnostic
```

検証順序と読取条件は通常checkと同じで、最初の失敗だけをJSONの`stage`と`code`へ返す。
証拠・元画像・session・本文・内部エラーの文字列を出力せず、鍵や用途資格も作らない。
未成立は終了コード1、全検証成功は0と`validated=true / registered=false`を返す。
引数不正は従来どおりのエラーとなり、registerに診断指定を付けることはできない。

| stage | code | 確認できた段階 |
|---|---|---|
| `evidence_guard` | `original_evidence_not_qualified` | 原証拠の通常guardが利用を認めなかった。証拠本文は読んでおらず、後続条件は未確認 |
| `evidence_guard` | `original_evidence_guard_failed` | guardの実行・時間制限・応答形式の確認が正常に終わらなかった。証拠本文は読んでいない |
| `evidence_read` | `original_evidence_read_failed` | guard通過後、原証拠を正常に読み取れなかった |
| `evidence_schema` | `original_evidence_schema_invalid` | 読み取った原証拠が既存の厳密なJSON形式に適合しない |
| `content_review` | `fictional_data_required` | 現行形式の架空データ申告が成立しない |
| `content_review` | `content_review_incomplete` | 新規生成・秘密/顧客内容なしの申告または確認の根拠が不足している |
| `bundle_contract` | `original_scope_or_metadata_invalid` | domain・session・scope・元ToDoの継続状態などが既存契約に適合しない |
| `image_validation` | `original_generation_or_image_not_qualified` | 個別の原出所・実ツール応答・原画像の検証に進んだが、期限や元保護の確認などを含む条件が成立しない |
| `validation` | `evidence_not_qualified` | 分類済みの停止理由を持たない内部エラーに対する固定の予備表示。内部エラーの文字列は出さない |
| `complete` | `evidence_validated` | 通常checkの全条件が成立した。未登録なので送信資格やUI承認とは別 |

表は停止理由の分類であり、判定順ではない。基本のdomain・session・scope確認は内容申告より前に、
残るmetadataや元ToDoの継続状態などの確認は内容申告より後に行う。
`bundle_contract`から、内容申告や後続の画像検証を通ったとは推定しない。
guard未成立を、実際の顧客内容の検出や、後続の`fictional_data`だけが原因と断定しない。
内容は担当の自己申告であり、検証器が真偽を判定したものではない。実在する本人のデータを
架空と偽って申告しない。原証拠を通常経路で読めず、
本人の依頼・元ToDo・原出所・質問scopeを独立に結ぶ用途検証も存在しない場合は、
停止段階と未確認の入力を元担当へ返す。原監査の書換え、別コピー、一般guardの解除や
画像ごとの固定許可で代用しない。

証拠JSONのdomainは`owner-generated-question-evidence-v1`。必要な項目:

- `scope`: todo_id、question_sha256、decision_sha256、owner_id、guild_id、forum_id。
  本文は正規CLIが使う改行を正規化した全文、decisionは同CLIの正規JSONのSHA256。
- `session`、`rollout_path`、`session_record_sha256`、`assignment_event`。
  当該ToDo専用の原Codex sessionの先頭メタデータと、最初の実ユーザー依頼の行番号・SHAを指定する。
  依頼は`ToDo<番号>`または`ToDo番号<番号>`（任意の接頭語`担当`）で始める。
  本文の別の行・引用・コードから番号を探したり、自然文の導入を読み飛ばしたりしない。
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
