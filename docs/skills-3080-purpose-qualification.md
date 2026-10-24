# 元3080の操作ごとに出所を照合する

この仕様はToDo3686の受入基準である。本人の2026-10-24のDiscord回答aは、
元3080への今回の範囲追加であり、通常の保護分類や原台帳の復旧ではない。
本人の正規取得記録は私的な原保存先に保持し、公開するのは固定ハッシュだけとする。

## 信頼する入力

- 本人scopeの原配送記録のSHA-256と、本人・質問・返信・判断identityを固定する。
- 新規配置のmanifestはこの公開リポジトリのmainに通常マージされたコミットの
  `docs/skills-3080-operations/` 以下の1ファイルだけとする。Git blobの同じbytesを
  SHA-256検査・JSON解析・実操作に使う。JSONの重複キーや未知フィールドは拒否する。
- manifestは元3080、現writerのjob・保存spec・作業場所、固定公開sourceの
  commit/path/hash、個別許諾のscopeとhash、出力bytesとhashを列挙する。
  新規のsource取得はこの入口では行わない。取得拒否647件の回収経路を設けない。
- manifestの同じSHAを固定した別モデルの登録済みレビューspecと、そのjobの
  最終応答にある受入票を照合する。受入票は
  `skills-3080-review-v1`、`manifest_sha256`、`accepted:true`、`must:0`。
  起動時のmodel要求と、対応するnative turn_contextの実modelを区別する。

## 三つの独立した操作

`--qualify-skills3080 <request.json>` は新規配置の事前確認だけを行う。
`--create-skills3080 <request.json>` は同じ確認を再実行し、保持したbytesを
CreateNewで作成する。事前確認のboolやreceiptを権限として再利用しない。
`--receive-skills3080-result <request.json>` は固定された既登録public作業の
最終応答そのものと、必要な識別metadataだけを返す。

いずれも通常の `--guard-path` へ資格を追加しない。成功時も
`general_access_granted:false`、`original_registry_recovered:false` を返す。
混在フラグ・位置引数・未知操作・空入力は拒否する。
拒否は匿名reason codeだけを返し、名前・leaf path・任意エラー本文を返さない。

## 新規配置の実操作境界

中央rootと全物理親を、reparse pointを開かずに保持する。対象名・相対pathは
正規形だけに限定する。既存の対象ディレクトリが一つでもあれば、その本文を
開く前に全体を保留する。同名の別名・別pathへの再試行はしない。
作成時にも排他のディレクトリ作成とCREATE_NEWを使う。hardlink数は1を要求する。
出力はmanifestの保持bytesを使い、SKILL.mdを最後に公開する。
途中失敗時に既存物を消したり、生成物を成功扱いしたりしない。

## 結果の受領

ToDo/job/spec/元親・実作業場所を既存登録と照合してから、そのexact jobの
`last_message.md` を読む。識別metadataの投影はid、status、model、effort、
終了コード、thread、native turn_contextのmodel/effort/service tierに限定する。
結果bytesは同じjob記録のlast_messageとも一致させる。
全履歴・prompt・秘密設定・顧客本文・無関係jobの出力は返さない。
この入口は、別pathへコピーした拒否済み資料を読むhelperではない。

## 合成受入例

正例は、受入済みmanifest・public sourceと個別許諾・出力hash・不在target・
非link親・現writerが揃う新規操作、および固定public調査のexact結果。
負例は既存未知、junction/symlink/hardlink、path escape、変更hash、別job/spec/
親/所有世代、未受入許諾、同一モデルreview、未マージ配布、空/ゼロ化、
カード/顧客一致、647取得、一般分類への流用とする。

## 検証と配布

影響範囲はrepair-guardとそのCLIであり、関連Go test/vetを実行する。
合成入力と実行結果のSHA、別モデル原応答、固定headのCI終了、通常mergeの
読戻しを保存する。その後にだけ、merged sourceからWindows版をビルドする。
更新前の実物・source・証拠・backupの対応と並行変更を確認し、正規ソフト更新後に
実物hash、通常CLI、用途CLI、実対象の結果を読み返す。
この文書やテストだけでは配布・中央導入・元3080全体の完了を意味しない。

## requestの正確なフィールド

JSONはUTF-8、重複キーなし、未知フィールドなしとする。CLIは操作名とrequestの
絶対パスの2引数だけを受け付ける。requestは実actorの割当作業場所の中に置く。

共通の必須項目は `domain:"skills-3080-operation-v1"` と `writer`。
writerは `task`（3080、結果受領/配布確認は修復3686も可）、`job`、
`spec_sha256`、`workspace` を持つ。環境変数CC_JOB_ID、正規ToDo登録、
現job、保存spec、processの生存と生成時刻、Git branch/originを実操作時に照合する。

新規配置用はさらに `manifest_commit`（40桁）、`manifest_path`
（`docs/skills-3080-operations/` 内）、`manifest_sha256`（64桁）と
`review` を持つ。reviewは `task`、`job`、`spec_sha256` と、任意の
`final_sha256`。reviewの登録specに
`SKILLS3080_MANIFEST_SHA256 <manifestのSHA256>` を固定する。

manifestは `domain:"skills-3080-create-v1"`、`todo_id:3080`、
`decision_identity_sha256`、共通writer、`sources`、`files` を持つ。
sourceは `repository`、`commit`、`path`、`sha256`、
`acquisition:"already-acquired"`、`license`。licenseは `path`、
`sha256`、`scope`、`accepted:true`。出典は元依頼の固定15公開Git repoに限る。
この版はサイト資料からの新規配置を受け付けず、原資料の評価は親に残る。
fileは `target`（中央rootからの相対path）、`sha256`、`content_base64`。
原文と許諾のhashが、保全する出力ファイルのhashに実際に含まれることも要求する。
各新規ディレクトリにはSKILL.mdが必要で、出力hashを受け入れたレビューと結ぶ。

結果受領用はmanifest/reviewを付けず `result` を付ける。resultは
`task`、`job`、`spec_sha256`、任意の `final_sha256`。
元3674と3669は原specのSHAをコードに固定する。元3676は本人が指定した
既登録reviewに限定し、その保存specとjobのfinite_reviewの対応を要求する。
修復3686の新しいコードレビューは正規subwork関係と固定headのmarkerを要求する。
依頼・結果・モデル情報を別jobに流用しない。

```text
scrub391.exe --qualify-skills3080 <CreateNew-requestの絶対パス>
scrub391.exe --create-skills3080 <同じrequestの絶対パス>
scrub391.exe --receive-skills3080-result <result-requestの絶対パス>
```

## このソフトの更新だけに使う資格

`--verify-skills3080-update <request>` は新たなデータの読取資格を作らない。
共通writer（3686）、reviewと、`runtime` を要求する。runtimeは
`source_root`、`source_head`、`reviewed_head`、`candidate`、
`candidate_sha256`。sourceとcandidateは実actorの作業場所内に限定する。
source_headとreviewed_headが公開mainの祖先で、reviewされたguardの全ビルド入力が
同一、追跡ソース変更なし、候補PEのGo buildinfoが同じWindows/amd64/module/revision
かつmodified=falseであることを確認する。

コードレビューの登録specは `SKILLS3080_IMPLEMENTATION_REVIEW <固定head>`、
最終応答は次の1行を含む。actual native modelはauthorと別で、effortはmaxを要求する。

```text
SKILLS3080_CODE_ACCEPTANCE {"domain":"skills-3080-code-review-v1","head":"<固定head>","accepted":true,"must":0}
```

現在の実行物は公開済み3258の実SHAとGo VCS revisionを独立したbaselineとして
照合する。旧版を保持した通常Windows更新の前後で同じ用途資格を確認する。
本コマンドはファイルの置換・管理者権限の追加・一般guardの解除を実行しない。
未review/未マージの候補はこの資格で実行・配置しない。

実際のコマンド・requestとsource/evidence/EXEのhash、対象別の未達理由は
担当の私的配布記録と元ToDoに保存する。原保護台帳は未復旧のまま保持する。
