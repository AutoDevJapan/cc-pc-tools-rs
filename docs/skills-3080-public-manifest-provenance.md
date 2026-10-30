# 公開するSkill案と内部の作成証明を分ける

公開するmanifestは出典、個別notice、出力先、実bytesとhashを記録する。
内部のjob、workspace、spec、本人回答の識別子を公開データの必須項目にしない。
実際の作者・作成者・現在の利用者は、元の正規登録と実行記録に結んで別々に保持する。
既存の公開原本・作成済みSkill・backup・原job/spec/prompt/nativeは変更しない。

新しい公開形式は`skills-3080-create-v2`とする。
公開するtop levelはdomain、sources、filesだけで、writer、todo_id、decisionは含めない。
元の公開内容をレビューした実authorは、正常受領した正式レビューの保存specから確認する。
書き込むcreatorは、現在の実actorと既存の本人回答の正規証明で確認する。
この情報を公開manifestへ後付けせず、CreateNewに成功した正規issuerの内部証明へ結ぶ。
manifestのSHA、原request、レビュー、producer、原native call、現在object/hashの検証を維持する。

名前を変えた参照のsource metadataには、出典tuple・原bytes・Git blobと個別noticeを残す。
公開するmetadataにreformulation_writerを要求せず、公開出典の対応を内部creatorの識別と分ける。
同じbasenameの古いmetadataと、旧manifest・旧producerによる作成証明は互換契約として保つ。
新形式のprivate証明が欠ける、別author、別creator、別manifest、別requestなら受け入れない。
公開形式に内部識別子を紛れ込ませても、新形式として受け入れない。

consumerの実行中の入力確認と、完了した結果の受領は同じ起動時版の証明を使う。
現在の可変な指示ファイルや任意の先行user、自己申告hashを期待値にしない。
公開project文書は、既に正常CreateNewしたissuerの公開commit証明に結ぶ同Git blobを使う。
networkを追加で有効にせず、現在の中央本文を読む前に利用者の入力・世代・環境を確認する。
authorとcreatorが過去の登録、consumerが現在の登録でも、それぞれの実IDと厳密な窓を保つ。

一般guard・元保護台帳・未知の中央対象・取得拒否・本人停止・費用の条件は変更しない。
正負例、関連Goテストとvet、正式二役の全指摘解消、同headのCI、通常merge、
同ソースのWindows更新、親による正常原結果・現在buffer・発見と実適用を別々に確認する。
登録、レビュー終了、配置、利用、全16資料の完了を混同しない。
