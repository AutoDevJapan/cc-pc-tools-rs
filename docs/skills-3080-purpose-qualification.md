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
対象名には小文字英数字と単語間のhyphenを用い、`3d-cloth-material` のように
数字から始まるSkill名も同じ入口で検証する。先頭・末尾・連続hyphen、大小文字の
別名、path escape、Windowsの予約名等は受け入れない。名前の先頭文字を理由に
正規の原名を変更させず、固定source・個別許諾・review・現writer・不在の条件を
同じ対象へ適用する。この命名修正は通常の保護分類や既存対象の許可を変えない。
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

レビューのoriginは、子の自己申告だけで受け入れない。正規の親3080/3686の
登録済みjob履歴にoriginがあり、子の登録・実job・実親jobのToDo/cwd/開始時刻が
一致することを確認する。現在の消費者は独立して、生存中の最新writerとして照合する。
同じ固定source/head（または同じmanifest SHA）の受入済みレビューは、通常の
継続で親のjobが進んでも再利用できる。過去のauthorを現在のwriterと偽らない。
架空のorigin、他ToDo、未登録・未来の世代、現writerの古い世代は拒否する。
native射影は厳密に `started_at <= timestamp <= finished_at` の範囲とする。

本人指定の旧3676の代替3709は、既受領の1件だけを固定task/job/spec/final SHAで
照合する。root登録のレビュー全体を認める変更ではない。原jobのLuna/maxと
owner_requestの指定元、対応nativeの実値を保持する。

## 既取得sourceの実bytes

manifestに許可repo/commitとhashを書くだけでは出典の証明にしない。原Cの
catalog SHAを `2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c`
へ固定し、15 repoの原content index・commit記録・完全treeの独立ハッシュを
コンパイル済みの [固定値](../repair-guard/skills3080_source_anchors_windows.go) と照合する。
6mileのindexは本人から引き継いだ `799bd907...2219607` とも一致する。
6mileの完全treeは原収集で保存したflat配列、他14 repoはtruncated=falseの
recursive tree記録であり、これを同じ形式だったと扱わない。

保持した同じstream bytesをハッシュ計算と対象metadataの選択に使い、EOFと
全体ハッシュの一致後にだけ利用する。Skill primaryがcatalog上で取得済みであり、
固定commit/path、index、treeのGit blob、bytes数が一致することを求める。
個別license/noticeも同じindex/treeの正確なpathとroleへ結ぶ。
対象sourceと適用noticeの保存objectだけを展開し、SHA-256・Git blob・bytes数と、レビューした
出力の保持bytesを一致させる。欠落、link/hardlink、変更された証拠、取得保留は拒否する。

catalogに列挙されない取得済み支持文は、固定indexの `support_text` とMarkdown拡張子を
要求する。同じmanifestに含まれる検証済み `skill_entry` のpackage内に限り、完全treeに
別の近いSKILL.mdがあれば拒否する。primaryと同じ宣言noticeへ結び、完全treeにある
適用root/個別noticeも、固定index/objectと出力の同じbytesへ結ぶ。
catalogに明示された未読/保留を、この経路で読み替えない。metadata・package・treeの
照合はobject展開より先に行う。catalog/index/treeの書換えや新規取得はしない。

この照合は出典の実体を証明する。許諾の適用範囲は引き続き固定manifestの
個別審査・別モデル受入を要し、root許諾を第三者素材へ包括適用しない。
新規取得、647本文の取得、中央の既存本文の読取を行わない。

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

PR476から配布済みのa5da版については、以前の別モデルレビュー・CI・97入力・
配布/backup対応を親が受入済みの記録と、今回の実物SHA/Go buildinfo、元PRの
MERGEDを照合した[ソフトの固定証拠](evidence/skills-3080-guard-installed-a5da.json)を
次回更新の既知baselineに用いる。候補自身が申告した任意hashを追加せず、同じ
固定実行物とsource revisionの組だけを扱う。分類や資料への許可は追加しない。

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

現在の3080担当は、親が独立に特定した既知public review3847の原応答も受領できる。
対象task/job/spec/final digestの4つを固定し、同じ親配下でも別の非公開reviewは受け付けない。
現spec/job、永続admission、元author、read-only実行、必須final digest、対応nativeと
現在consumerの条件は維持する。応答の受領と、manifestの内容受入は別であり、
正しいacceptance markerがない原応答はCreateNewの承認には使えない。

通常の継続で仕様が進んだ元3686の既知public結果は、現在specの代用にしない。
対象は実登録済み `20261025-211205-todo-3686` の原602f仕様に限定する。
独立に保持した原specとsaved promptのdigest、実launch receipt、原3080担当、登録世代、
cwd/thread/model/時刻を照合する。実runner parentはnullのまま保持する。
現在のconsumerは、生存中・最新・未停止の3080担当でなければならない。
必須の `final_sha256` はjob metadataと同じ原final fileに照合し、さらに実時間窓内の
native user入力にsaved promptが含まれ、native finalが同じbytesであることを求める。
原sessionへの通常の追記は初期長のsnapshotで読み、後続世代のturn/finalは含めない。
任意の過去job/path、現specへの偽装、spec巻戻しや偽receiptは認めない。
一般guardの保留原票を成功へ書き換える処理はない。

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
最終応答は次の1行を含む。actual native modelはauthorと別にする。
GPT/Claudeにはmaxを要求する。既存OAuthのGoogle Antigravityが返した実catalogでは
Gemini 3.1 Proはlow/high、Gemini 3.8 Flashはlow/medium/highを提供しており、
この2モデルにはhighを要求する。
指定・jobの記録・対応native contextの実値を一致させ、highをmaxだったと報告しない。

```text
SKILLS3080_CODE_ACCEPTANCE {"domain":"skills-3080-code-review-v1","head":"<固定head>","accepted":true,"must":0}
```

現在の実行物は公開済み3258の実SHAとGo VCS revisionを独立したbaselineとして
照合する。旧版を保持した通常Windows更新の前後で同じ用途資格を確認する。
本コマンドはファイルの置換・管理者権限の追加・一般guardの解除を実行しない。
未review/未マージの候補はこの資格で実行・配置しない。

置換後の照合に失敗した場合は、実target・backupのハッシュと使用可否を記録する。
targetが既知の新SHA、backupが既知の旧SHAで、並行変更がないことを確認できた
場合だけ、同じ通常Windows置換で新物を別のbackupへ保持しつつ旧版を復旧する。
どちらかが未知なら自動で消去・force rollbackせず、用途操作を止め、正規の
再照合と復旧手順へ進む。`installed:false` だけで旧版へ戻ったとは報告しない。

実際のコマンド・requestとsource/evidence/EXEのhash、対象別の未達理由は
担当の私的配布記録と元ToDoに保存する。原保護台帳は未復旧のまま保持する。

## 作成済み出力を後から使うとき

公開レビューのraw受領は、[起動前の型付き公開入力](skills-3080-public-review.md)と
既存の固定移行を使う。main mergeと内容レビュー後の配置受入とは分ける。

現在のCreateNew成功結果は、作成時のファイルIDを永続化していない。
成功件数と現在のhashだけを作成済み出力の読取資格へ転用しない。
過去14の[合成反例と当時の修復案](skills-3080-created-resource-provenance.md)は保持する。
今後の新規作成については、[forward発行と同buffer消費](skills-3080-forward-consumption.md)
を実装する。作成時OS IDや長期の物理連続性を本人の新しい必須条件へ昇格させず、
原作成の履歴、許可された公開内容、現在の消費handle、未証明の物理連続性を分ける。
この未達は、過去の正規作成・配布や、既に検証済みの使用成功を無効にするものではない。
