# 新しいDiscord会話の個別ファイルを照合する

ToDo2823で作成する新しい質問と本人回答を、元担当が読み取れるようにする。
`repair-guard` の既存 `allowed(path)` に1つの分岐を追加する。
原保護台帳の復旧や、新しいプロジェクトの登録・配備・自動起動には使わない。
結果の `original_registry_recovered=false` は維持する。

## 対象

固定の `D:/CC/Tools/discord-ask/unattended/conversations/<request_id>` 内で、
以下の通常ファイルだけを検査して許可する。

- `question.json`
- `replies/<正の10進message_id>.json`

`request_id` は先頭がASCII英数字で、全体が英数字・`_`・`-`の80文字以内。
ディレクトリ自体、会話index、`question.txt`、`decision.json`、最新返信一覧、
note、ready、報告、編集履歴、未知のファイル名は対象にしない。
リンク・junction・その他のreparse pointを含むパスも対象にしない。

## 来歴の確認

次の全てを満たす場合だけ、個別ファイルを許可する。

1. 固定homeの `conversation-questions.json` のdomainが `discord-conversation-v1`。
   その送信先が、固定の本人notify設定にあるguild・forum・本人・application IDと一致する。
   検査する質問がindexに正確なキーで存在し、phaseが
   `posted / waiting / answered / applied / closed` のいずれか。
   予約だけの `reserved` は許可しない。
   indexの `aliases / logs / topics` は共存できる。aliasに保存した別IDの原intentは
   許可対象を増やさず、実際の `questions` にあるcanonical質問の個別ファイルだけを扱う。
2. 質問のprojectとToDo番号が、既存の全ToDo内のただ1件の元ToDoと一致する。
   登録先rootが存在し、既存の `allowed(root)` が許可する。
   `owner`、未登録、未知・保護対象のrootでは許可しない。
   rootが会話home内にある場合と、そのリンクによる別名は、再帰的な判定を呼ぶ前に拒否する。
3. 質問の本文・判断・receiptの参照パスが固定home下の予測可能な正確なパスと一致する。
   `question.json` の生bytesのSHA256がindexの `question_receipt_sha256` と一致する。
   質問JSONは既定の平坦な11文字列フィールドだけで、domain、sender request、
   文字列ToDo ID、本人・bot・guild、starterとthread、進捗thread、fingerprintが一致する。
4. 既存の新しい送信記録 `recovery-messages.json` のdomainが
   `new-owner-recovery-messages`、versionが1。
   ただ1件の元intentとbot記録、および検証済みの `question:0` / `progress` が一致する。
   元intentのToDo・元投稿・質問fingerprint・送信先を照合する。
   元の質問本文のSHAと判断identityのcanonical SHAもindexに一致する。
   元投稿を別途修復した記録は、この小さな拡張では扱わない。
   Rustの送信側定義と、2026-10-18に親担当が取得した本番のkeys-onlyメタデータは、
   version=1およびdestinationの `guild / forum / owner / request_forum` で一致した。
5. 返信の場合、固定homeの新しい `unattended-state.json` のdomainと送信先を照合する。
   その `requests[message_id]` と対象ファイルの本人・thread・guild・会話ID・本文・
   編集時刻・作成/取得時刻・元質問context・返信参照・過去版等が一致する。
   取得元は `discord-api-v10`、record versionは2。
   ToDoは取り込み時の0または元番号、phaseは新会話の保存・配送・適用済みに限る。
   配送後の `reported` の更新は許可を変えない。state最上位の
   `conversation_notice_turn / conversation_report_cursor` も共存できる。
   本人・本文・context・origin等の来歴は引き続き一致を要求する。
6. 返信内のcontextでも、新しいAPI取得のdomain・本人・元質問・sender request・
   元本文・判断identityを照合する。選択した本人返信が1件だけ存在し、
   そのID・本文・時刻が対象ファイルと一致する。
   context内の他の本人返信も同じ質問・本人・判断範囲に結び付くことを確認する。

旧台帳や失われた除外一覧を新規作成しない。
既存の非client判定だけを元rootの資格として使い、rootの許可範囲は増やさない。
元の `legacyMatch` は対象・証跡・rootの検査前後で維持する。
原本にある秘密値、元台帳の行、本文、project名やパスは出力しない。
guardのCLI出力は従来のboolメタデータのままとする。

## 読取と保存途中の扱い

全JSONで重複キー、末尾の別JSON、深すぎる構造を拒否し、数値IDを正確に読む。
通常ファイルの同一性と物理パスを確認して、上限付きで読み取る。
検査した同じbytesを解析し、最後に各証跡のSHAが変わっていないことを確認する。
SHAの期待値を、検査対象からその場で作り直して許可しない。

質問indexと送信記録、返信ファイルと新しい受信状態を別々に照合する。
これはローカルの正規取得記録同士の整合性確認であり、独自の電子署名ではない。
返信ファイルだけが先に更新され、受信状態がまだ古ければ保護を維持する。
状態が揃った後の同じ検査は通る。guardは記録を更新・修復せず、配送や起動もしない。

## 検査

Windowsの `repair-guard` モジュールで以下を実行する。

```text
go test -race ./...
go vet ./...
```

新しいテストは合成した設定・ToDo・質問・送信記録・返信状態だけを使う。
元rootの資格と除外判定は注入し、本番の私的台帳や返信を読まない。
正しい質問・返信、配送後の状態、本人編集の保存待ちと反映後、未知ファイル、
本人/hash/元質問/判断範囲の不一致、元rootの拒否・自己参照、リンク、破損JSON、
重複キー、更新途中の証跡、既存除外の優先を確認する。

本番実行物のビルド・配置と、実際の元rootおよび新返信ファイルでのreadbackは親担当で行う。
