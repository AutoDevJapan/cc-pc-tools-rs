SKILLS3080_READ_ONLY_REVIEW_TASK_V1
# 復旧状態と論文要旨の二手順を確認する

対象は `create-health-abstract-public.json` の `openclaw-health-guardian` と `abstract-logic-writer` の二つ、全12出力だけである。
OpenClawは健康観測・cooldown/回数による抑止・復旧試行・実回復の判定を扱い、元のmacOS実装の移植や実稼働を主張しない。
論文要旨は元の命題・語の意味・根拠を保つ推敲を扱い、独自scoreを研究の真実や学術品質の証明にしない。
両方とも判断手順であり、元資料のscript・daemon・JSONassets・raw corpusは同梱・実行していない。

公開manifestは `skills-3080-create-v2`、出典metadataは `skills-3080-source-v2` を使う。
この形式名と先頭のレビュー形式、公開ファイルの参照先は、読み取り・作成・再利用に必要な技術識別子である。
実際の担当・会話・個人の作業場所・内部仕様や本人回答の識別値は公開データへ持ち込まず、正規の別記録へ保持する。

個別MIT通知と元本文は、同じdirectoryの固定したバイトを保持する。
公開archiveの所有者・公開状態・固定commitと選択した4つのGit blob/サイズを一次metadataへ照合し、保存済みの同じbufferのSHA-256・サイズ・Git blobとUTF-8を確認した。
この照合は、独立した著作者の権利や、別script・外部service・dataset/重み・第三者素材の権利を証明するものではない。
権利者名が無い箇所も原通知のまま保持し、別人の著者名を推定して足さない。

作成者側の構造・日本語description・参照・hash・正負例チェッカーは成功した。
これは作成者側の検査であり、内容レビュー担当が実行した検査とは区別する。
既存Skillの本文・現在の存在・同等性は、この固定入力だけで確認済みとしない。
同じ版のCI、通常merge、CreateNew、同bufferの読戻し・発見と実用途での確認は別の受入条件で、manifestや読了だけでは導入完了にしない。
