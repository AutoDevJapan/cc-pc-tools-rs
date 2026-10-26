# 質問画像に対応する入力参照の照合

既存Codex AppServerから一つの入力を読み直し、指定された本文版と画像識別子を照合する。
元画像、生成監査、質問証拠、用途資格ストアは開かず、登録・送信・モデル実行はしない。
この結果は入力参照の照合だけであり、本人ID、画像の出所・内容、元ToDoとの対応、画像の用途資格、UI承認を証明しない。

```text
generated-question-input.exe --thread-id <UUID> --turn-id <UUID> --item-id <UUID> --input-sha256 <本文UTF-8のSHA256> --image-name exec-<UUID>.png --image-sha256 <原画像の既知SHA256>
```

画像名と画像SHAは少なくとも一方を指定する。
このコマンドに画像のパス、実行ファイル、接続先、任意のAPI methodを渡すことはできない。
本番の取得口は既存の `D:/CC/Tools/codex-progress-tui/codex-launch.exe app-server --listen stdio://` だけで、Windowsでは窓を表示しない。
既存ログインを利用し、APIキーを選択する環境変数は子へ渡さない。

## 照合順序

1. 引数のUUID、SHA、画像名と重複指定を検証し、不正なら取得口を起動しない。
2. `thread/read(includeTurns=false)` でthread IDと`cli`または`vscode`の種別を確認する。
3. `thread/turns/list(itemsView=notLoaded)` を1ページ20turn、最大8ページで読み、指定turnが同じroot IDを持つことと、turn開始時刻が未来でないことを確認する。
4. そのturnの先頭1itemを取得し、指定item ID、userMessage、ちょうど1つのtextであることを確認する。
5. 同じitemだけを`thread/items/read`で再取得し、turn・item・本文版の一致を確認する。
6. 本文のSHAが引数と一致し、画像の識別子が一意で指定対象と一致することを確認する。

スケジューラ起動文やToDo割当等の既知の自動入力は拒否するが、文頭検査で人間性を証明したとは扱わない。
同じOSユーザーの永続記録と正規AppServerは信頼境界であり、その環境の侵害や、user roleで記録された入力の作成者を証明する機能ではない。
画像名とSHAがあっても、質問の意味・権限範囲との対応は別に確認する。

本文版は、APIが返す単一textを正規化せずUTF-8へ符号化したSHA256とする。
原native行のSHAや署名ではない。
itemの発言時刻がnullでもturn開始時刻を発言時刻に代入せず、このコマンドでは利用期限を作らない。
後続の用途検証は、元の保護条件、期限、元ToDo、生成イベント、原画像の同一バイト、質問scopeを別に検証する必要がある。

## 結果と限界

結果は常に次の5項目だけで、入力本文、ID、パス、APIエラー、認証情報を出力しない。

```json
{"domain":"generated-question-input-reference-v1","source_matched":true,"stage":"complete","code":"source_reference_matched","asset_qualified":false}
```

成功は終了0、入力や照合の未成立は終了1と固定codeを返す。
画像を名指す現在入力がなければ`image_reference_missing`となり、指定HTMLの構造や過去の一般指示を、原画像への明示的な名指しとして書き換えない。
正常APIで元入力を取得できたことと、原画像を質問へ添付できることを区別する。

既存の `--check-generated-question-evidence` とそのguard、架空データ契約、登録/消費時の検証、署名、期限は変更しない。
この部品の成功を元画像の一般allowlistや既存guardの解除に使わない。

一次資料: [Codex App Server](https://learn.chatgpt.com/docs/app-server#list-thread-items)。
実際に利用するfieldは、導入済みCLIが生成するJSON schemaでも確認する。
