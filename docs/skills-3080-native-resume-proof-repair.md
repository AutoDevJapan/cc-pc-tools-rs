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
