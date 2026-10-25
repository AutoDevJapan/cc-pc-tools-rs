# 作成後の公開Skillを別の実担当が使う

通常の`--guard-path`は変更しない。正規のforward CreateNewが発行したv2証拠の
ある新規出力だけを、`--consume-skills3080-admitted`で同bufferへ読み取る。
元creatorが終了していても、元producerが更新されていても照合できる。
原台帳・未知9・旧14/28・647、継続性と現在OS writerが未証明であることは保つ。

通常の共有ToDoへ登録する読み取り専用reviewの**全仕様**を、以下のJSONにする。
将来のcaller job IDや任意のwriterを仕様に書かない。実callerは現在の
`CC_JOB_ID`、登録・最新世代、admission、実PID、生存、source/cwdから導く。

```json
{
  "domain":"skills-3080-consumer-v1",
  "instructions":"skillsConsumerInstructionsの完全な文字列",
  "author":{"task":3080,"job":"登録時の実親","spec_sha256":"原親の保存spec SHA","workspace":"元3080のworktree"},
  "author_spec_path":"D:/CC/Tools/todo/specs/3080-保存ID.md",
  "creator_spec_path":"D:/CC/Tools/todo/specs/3080-原creatorの保存ID.md",
  "manifest_commit":"mainへmerge済み固定commit",
  "manifest_path":"docs/skills-3080-operations/固定manifest.json",
  "manifest_sha256":"固定manifest SHA256",
  "review":{"task":1234,"job":"原review job","spec_sha256":"保存spec SHA","final_sha256":"原final SHA"},
  "consume":{
    "creator":{"task":3080,"job":"原creator job","spec_sha256":"原保存spec SHA","workspace":"原creator worktree"},
    "creation_request":"原の実CreateNew requestの絶対path",
    "creation_request_sha256":"原requestのSHA256",
    "native_call_id":"call_原CreateNewの実呼出しID",
    "indices":[0,1]
  }
}
```

未知field・文章の追記・任意指示・別親・write可能なsandbox・未登録・古いcaller・
偽admissionは拒否する。scopeは現在consumerの起動前specと実launch prompt、
実nativeのsession/cwd/開始から実観測時刻までの入力・model/effortへ結ぶ。
後付けの別依頼や未知の追加user入力を拒否し、返却前にも同じ照合を行う。
running jobへ架空の終了時刻を保存せず、assistant本文も入力証明には使わない。
共通の起動指示は、元authorのsessionから得る指示digestと厳密なcwd/環境形式で
識別する。任意の先行userを共通指示扱いにしない。正規scheduler prompt・固定の
担当関係文/guidance・実execution-promptも照合し、仕様後の自由文を認めない。
正規登録は元3080の実authorから`--subwork-of 3080 --parent-job-id <実author>
--subwork-kind review --spec <JSON>`を使う。Luna/maxの指定は元の本人回答の範囲を
保持する。読み取り専用の担当はrequestファイルの作成やToDo更新をしない。

入口は原creatorの保存spec/launch、mainの公開manifest、既取得出典・個別notice、
元nativeの厳密な開始終了窓にある実CreateNewとその発行証拠を再検証する。
原producerは現在の実行版、又は独立に受入済みのソフトウェアSHA/revisionに
一致するものだけを認める。任意のreceiptが自称するproducerは認めない。
元の正規producerが作成時にレビュー受入を確認した事実をnativeで照合し、
新callerを原creatorへ偽装してrawレビューを読み直すことはしない。

読み取り専用でnetwork=falseだった実3880は、旧経路の公開main再照会が含まれる
manifest段階で止まり、本文を受け取れなかった。この原結果を保持する。
修復後の消費は、新規作成時に受入済みproducerが確認してnativeへ発行した
PUBLIC/main/notice/reviewの証拠を再利用する。exact request・原call・元の時間窓・
producer・manifest tupleを先に一致させ、その後だけ同じ物理Git sourceの保存objectを
SHA256へ照合する。取得や新しいnetwork許可は行わない。
現在のインストール済みソフトの観測と、公開性を発行した過去producerの証明を分ける。
新しいCreateNewの公開main照会は従来どおり必要で、通常guardは変更しない。

その後だけ、元のdirectory/作成IDと現在の非link・single-link handleを照合し、
期待する公開bytesと実際の同handleのread bufferを比較する。全選択分の照合後、
現在callerと停止状態を再確認して本文を返す。最大32index・64KiB。
新規不在確認やCreateNewは一切繰り返さない。

正規CLI成功と、担当が返された指示を読んで実際に適用したことは別に確認する。
原文を受領する親は同じ`--receive-skills3080-result`でregistered childと元spec・
public creation provenance・native入力/finalを結ぶ。これは内容reviewの受入票や
一般の中央フォルダの許可にはならない。
