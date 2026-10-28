# 同じ3686で正規の担当再開の照合を修復する

元3080 / Issue283の指定16資料の拡張に必須の、同じ3686の有限な追加修復である。
本人の限定修復aが指定した、公開出典・個別許諾・固定hashと実writerを証明する新規CreateNewと登録済み公開結果の正規経路だけを対象にする。
一般guard、原の私的保護一覧、本人の権限・禁止・費用・契約、647件の取得拒否は変更しない。
新しい実装ToDoや同じ本人質問は作らず、同じ3686が修復・テスト・別モデルレビュー・CI・通常merge・Windows更新・親の実受入まで追う。

## 再現した不具合

親の実jobは `20261027-175645-todo-3080`、元specのSHAは `91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435` である。
固定0a083ef / manifest a02c1eacの残高案1件・7ファイルは、セルフ4076と独立4115の実Luna/max・全文10入力/7出力・全分類0および同headのCI成功後、PR621/merge deef7563へ通常反映済みで、実配置はまだ0である。
Creator4193の実job `20261028-193641-todo-4193` も正常終了し、元の不変spec `8fe56f7849c68d022eeaa4bc3b5b97494754d8b2dd26e8856e977b2a010864f6`、同固定0a083efとmanifest a02c1eacを保つ。
その正規結果2178バイトのSHAは `aac5b49c8139f8fad0469aadc0ac3becc071b6f54403c52979ba822140376c80` だが、現在の通常CLIの `--receive-skills3080-result` は `qualified:false, reason:writer` で受領前に止まる。
親は別path・コピー・genericレビューによる代理受領を行っていない。

現在の配布物は元3686のPR618から正規更新したF8、SHA `f8b2ba6e66e60b06bbbc46c2c0f3d6eedc0208d40444c30e37b70afe29db4fc7`、source `a71330f543969b4585f01b7359a84a2fa528c9f2` である。
親は元Creatorの完了を正常ToDoで照合し、実際に継続中の同じ担当を `todo start 3080 --job <actual CC_JOB_ID>` で正規に再開した。
本件の実Metadataでは、scheduler.keyは明示nullで、正のresume_generation、同じ世代のworker_receipt、実jobとの対応がそろう。
同じF8ソースの読取専用診断では、仕様・現登録・最新job・実PIDの同じbirth・実行中・cwd・branch・sourceの全条件が通り、旧launch_keyとの一致だけがfalseだった。
診断から結果本文や中央データを開かず、元記録・権限を変更していない。

正規の担当割当は `cc-scheduler/src/common.rs` の `assign_worker` / `advance_resume` が世代とworker_receiptを保存する。
`cc-todo/src/answered_wait.rs` も、明示nullのscheduler.key・同じ実job・正しい世代を伴うcanonical manual assignmentを既に区別している。
根拠のないキーを記録へ戻す操作や、過去jobを現在の担当に偽装する操作で直さない。

## 直す境界と受入

1. 自分の原worktree、実GitHub owner/idとguard、同3686の既存状態を確認する。
   原成功済みの全source、PR615/618、旧9be/50c/F8のbackup・ID・ACL・全review/spec/job/nativeを保つ。
   受入済みmainのguardソースから変更し、親の未受入の評価追加や別担当の差分をコードPRへ混ぜない。
2. 現行writer照合が、上の正規割当を拒むことを合成metadataのRedで再現する。
   正常な非空キーの完全一致は従前どおりに扱う。
   nullキーの代替は、正規CLIが保存するcanonicalな世代・worker_receipt・同じ実jobに結ぶ。
   PID/birth・live/current/latest・実task/プロジェクト・spec・実cwd/branch/remote・停止/質問/待機・作者とconsumerの区別など、既存の残りの条件を維持する。
3. 欠落・空文字・非空の誤キー、別job/世代・0/負/小数/異型の世代、重複/case aliasのキー、旧世代・停止・別spec/cwd/source/PIDの負例を持つ。
   文字列への変換でnullと空文字を同一視せず、未知や偽の割当を受入しない。
   元caller、Job、ToDoやworker_receiptを手書き・編集して試験を通さない。
4. 修復は同じ用途のactor照合に限る。
   結果、Create、forward、consume、ソフト更新で共有している利用側を調べ、必要な回帰テストとvetを行う。
   実corpus・中央Skills・元結果本文をテストから読み書きせず、旧alias/prefix/個別notice/unknown/実ID/同bufferの条件は変えない。
5. 成功した旧typedレビューを再編集・再起動して新しいソフトの受入に流用しない。
   同じ3686の未凍結の既存役割を調べ、なければ必要な有限レビューを通常ToDoへspec付きで登録する。
   同じ最終headで、作者と異なるLuna/max・両origin owner_requestのセルフと独立レビューを受け、実native・原応答・全Must/Suggestion/Nit0を確認する。
   本人の既答は元3080・3669・3686の残る複数ファイルレビューだけに適用し、他担当や全体設定へ使わない。
6. 正確headのCI成功後に通常PRをmergeする。
   同じmergedソースからWindows/amd64 CLIを一度生成し、全raw build入力・source/modified=false・候補SHA・現F8と両最終レビューを、既存の正常なverify/update経路で照合する。
   旧F8と以前のbackupを保持して、必要な管理者工程だけ既存CC-AdminBrokerで実行し、配置後の実hash/source/ID・同bufferとCLIを確認する。
   verifyが拒否したら原因を直し、直接置換・審査省略・別経路で通さない。
7. 親へソース・レビュー・CI・merge・Windows実更新・現在の正規CLIの結果を区別して返す。
   親は、同じ30cee5c5の元受領request、同じCreator4193・8fe・aac5の原結果を再検証し、原文全読・独立native・受入の後だけ、元a02cの新規作成一度、7ファイルの実ID/hash/読戻し、発見と現在指示への実適用へ進む。
   登録やソフト更新を、配置1件・全16資料の完了へ変えない。

Goの関連検査を使い、重いCargo・GPU・Civ6、APIキー・新費用、新しい恒久権限は使わない。
console/UACを出さず、途中成果を同じ作業ブランチへcommitし、再開情報を保存する。
通常の技術修復を本人の再判断・PC操作待ちにしない。

## 2026-10-28の実結果と入力の訂正

上の8fe/193641/2178Bは診断時点の履歴であり、受入へ使っていない。8feは親が標準instructionsへ補足を追加した不適合で、productionの厳密な比較を緩めず、親が同10入力の標準instructions版e904へ正規に改訂した。
同4193の新実job211001は、正規結果request1c6eで実1804B/55e7ac24の原文を受領し、同じnativeのLuna/maxも独立照合できた。Must0/Suggestion1/Nit1が残ったのでCreateには使っていない。原文は実改行と独立したacceptance行を持ち、transport破損とは扱わない。

Actor修復は原3686のPR624/最終4284eef/merge034520dへ通常反映された。親の同じ正規結果受領は、新Windows実行版SHA eab69e4fc8e52f2ad6858ef4e8498e8cc6030ac8ce7f788251a1aae476dc5d55でqualified:trueとなった。Windows更新全体の証跡受入は同じ修復担当の結果と別に追い、hashだけで全工程の成功とはしない。

未配置の残高1案を、既読Root19から準備した3案と合わせ、next-fourの同28出力へ進める。旧manifest/元source/既存154件を保持し、source.jsonの限定notice適用を明示し、属性の重複を整理した。新しい同固定版の全文Creator、セルフレビューとラバーダック、CI/通常merge、正規CreateNew・ID/hash読戻し・発見・現在の指示による適用はまだ残る。全16資料の完了や一般guard解除を主張しない。
