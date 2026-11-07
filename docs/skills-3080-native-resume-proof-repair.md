# 再開した作者と新規レビューの運用指示を照合する

同じ用途限定修復の追加ケース。公開出典・個別notice・固定hashと実担当に結ぶ新規Skillと、
その登録済みpublic担当の原結果だけを扱う。通常guard・原保護台帳・未知の中央9対象・
既存Skill/backup・取得拒否647件・本人停止・新費用の条件は変更しない。

## 実際の停止点

状態移行と保管先照合のmanifestは`create-state-and-cloud-procedures.json`。
固定入力cfe6d3e16101910ff79a55fa6e7def7b6d1acbf5とmanifest SHA
2d56531652603394f7ff0283712e149f13f8a3acde450bc63fc15c9c7ed8dc84は、12出力の提案であり未配置。
全7チェックは終了して失敗0だが、原結果の正規受領と内容の受入前なので未マージである。

先に既存レビューを再利用した登録は、immutable originと新しいtyped authorが不一致だった。
その`result_binding`は正しい拒否として保持し、originや原結果を編集して受入へ変えない。
その後、今回の実authorを元登録から固定した2つの正式reviewを登録した。
対象のmetadataは元3080・現在author・同じ固定manifestを選択するtyped specから限定できる。
登録元・実launch親・実model/effort・session/cwdと時間窓を確認済みの正常終了reviewでも、
通常`--receive-skills3080-result`は`native_context`を返した。原finalの直接読取や別pathは未使用。

現在の作者は古いsessionを正規に再開している。native初頭4MiBで得るbootstrapは古い運用指示で、
作者の今回job窓には新しいbootstrapがなく、正常schedulerのtask入力が1件ある。
新規reviewのbootstrapは更新後の運用指示で、bodyの長さとhashが異なる。
単なるprefix追加として旧bodyを削った残りが一致する関係でもない。
review側の完了nativeはdeny-writeで開け、実modelとmax・session/cwdも一致した。
匿名の実測は[再現のmetadata](evidence/skills-3080-native-resume-proof.json)に保持する。

`skillsReviewNative`は元authorの初回bootstrap digestを取り出し、
`skillsNativeWindow`が新規review自身の初回digestとの完全一致を求める。
今回の2つの値ではこの条件が成立しない。他の段階にも失敗があるか確認し、
この差だけが唯一の原因と推測して終えない。

## 修復と受入

- 長期sessionの古い初回、現在jobで正常配送されたowner policy、今回の保存spec/入力、
  新規reviewのruntime bootstrap、途中に追加された任意user指示を設計上区別する。
  author・origin・現在consumerと厳密な時間窓を保持する。
- 同じ本文の自己申告hashだけで資格を作らない。bootstrapの独立した出所を証明する。
  childのdigestをそのまま期待値へ置く、未知のdigestをbaselineに足す、
  arbitrary prefix/AGENTSラベルを除去する、任意の先行userをcommon指示にする修正はしない。
- 正常な運用指示の更新と、scope外の追加指示・偽bootstrap・違うcwd/環境/世代・
  後付けspecを区別できる正例と反例を先に作る。実測した旧/新の差と同じ条件で再現し、
  多重user/final、commentaryだけ、時間外、異なる原結果、追記/短縮やwrite共有の拒否も保つ。
- nativeの実原本・job/spec/prompt・原finalは変更しない。修復後に同じ正式review結果を
  正常receiverで受け取る。内容指摘の有無はまだ未確認で、登録・終了をMust0へ数えない。
- 元3686の同じ担当で原因調査、必要な関連Goテストとvet、正式二役レビュー、CI、通常merge、
  同ソースのWindows生成と正規verify/apply、親の同buffer/CLI再検証まで完遂する。
  Goの既存実装を使い、Cargo/GPU/Civ6や追加API枠を使用しない。

調査用に読むnativeは、宣言manifestに結ぶ今回authorと、そのauthorが元登録した
同manifestの正式public reviewだけの起動/入力/終了照合へ限定する。
必要な指示本文は内部照合にとどめ、原文・tool引数・秘密・無関係sessionを出力/公開しない。
履歴本文全体をモデルへ返さず、再現資料は型・時刻・長さ・hashと匿名の失敗段階にする。

これは一般の保護解除や中央の再作成の依頼ではない。全16資料の拡張は元3080で継続し、
親が原結果・残る指摘・実配置・発見と適用を確認するまで全体完了としない。

## 指示の出所と照合順

作者の初回bootstrapは過去のセッションの事実として保持する。
保存spec・scheduler prompt・launch receipt・実行時に配送されたowner policyは、
今回のjobの入力として従来どおり別に照合する。
新しいPUBLICレビューのbootstrapを作者の古い本文へ書き換えない。

初回起動のレビューでは、Codex 0.165.0が起動時に記録したfull `world_state`の
`agents_md.directory/text`との整合を確認し、別に記録したモデル要求へ出所を照合する。
execのsession、task_startedのturn、bootstrap、runtime snapshot、最初のturn_contextを
同じjob窓・cwd・モデルへ結び、runtimeが記録した全文とbootstrapの全byteを照合する。
同じnative内の本文とcheckpointだけを独立した真正性の証明にしない。
[記録済み入力との照合](skills-3080-recorded-runtime-input.md)に従い、正常親の事前準備と
通常dispatchに結ぶ別のrequest objectも確認する。
現在の可変な指示ファイルや、後から書いたsnapshot・本文の自己申告hashは使わない。

プロジェクト側は同じPUBLIC commitのGitHub treeで確認した`AGENTS.md` blobへ結ぶ。
この部分のBOM・改行・文書端の表現だけを比較用に揃え、任意のprefixやラベルを除去しない。
新runtimeではsnapshotが欠ける・異なる・遅れる場合に旧digestへdowngradeしない。
同PUBLIC project原本の欠落・取得失敗でも旧digestへ戻らない。
以前のruntimeで同じ原authorのbootstrapを確認する旧契約は別に保つ。
一般guard・原台帳・任意の指示ファイル・設定・秘密の読取は変更しない。
受領CLIへ指示本文を返さない。

新しい出所を使う場合は、初回起動のmetadataとjob窓内のsession開始を確認する。
bootstrapとタスク・最終応答は同じ長さを固定したnative handleから読み、
重複user/final、別の世代、別cwd・環境、未知の追加指示を拒否する。
元のauthor・現在のconsumer・原結果のIDとhashの照合は省略しない。
全job窓の`user_message` eventも確認し、登録入力と同じ本文・同じ記録時刻の
一つの表現だけを認める。内容違い・重複・時刻違い・添付付きeventは拒否する。

Windows更新では、以前のPR番号を再利用せず、固定したreviewed headに対応する
実際の通常マージをGitHubから選ぶ。repository ID、head、main、merge SHAを確認し、
そのPRの必須CI、Actionsアプリ、固定したworkflow ID/path、run/job/check-runの対応を
既存の共通verify/apply/recover入口で照合する。
既に配布したWindows版のhash/sourceの組は、生成前の独立観測で両更新処理へ保持する。
これらのコード確認と、正式レビュー・CI・配布・親の受入は別の完了条件である。
Windowsの生成中は、確認済みsourceのfile handleと物理的な親を保持し、
書き換え・置換を拒否した同じ入力をcompiler終了後まで照合する。
directory監視を先に開始し、監視下で未追跡・ignore対象を含む入力一覧を照合する。
監視前から存在した余分なsourceと、一時追加後に消されたsourceをどちらも拒否する。
