SKILLS3080_READ_ONLY_REVIEW_TASK_V1
# 認可・復元・接続の3案21出力を全文確認する

元3080/Issue283、指定16資料を用いた同じ拡張作業の一部であり、この3案を件数の上限にはしない。
作者は20261027-175645-todo-3080のSol/maxで、別モデルのLuna/maxを本人の限定指定に従って使う。
現在の導入159件・統合3件・除外記録15件（再確認中）と登録評価1501件は不変で、3案の配置・発見・実利用はまだ0。

対象はdynamic-action-authorization、backup-restore-reconciliation、connection-scope-reconciliationの3案で、それぞれ7ファイル、合計21ファイル104799バイトを全文読む。
作成manifestはdocs/skills-3080-operations/create-recovery-connections-175645.json、SHA256は212e5645cd33a0ae0abe44b1b75fa2079826889b8c41f4857963808649d90c24。
比較reviewではなく、この固定版の全21出力を確認する。
旧Creator4193/4208は既作成の別manifestに結ぶ凍結済みの証拠なので、今回の資格へ流用・編集・再起動しない。

資料は6mile/skillsの固定commit920714f17d81e0a95c7a9bc51faf0a7921cd982d、原246(web-claw)、249(time-clawshine)、251(api-gateway-1-0-46)の本文と各directoryのMIT notice。
公開者・固定tree・6個別blob/bytesを通常GitHubで確認し、各open前の保護判定後、親は既存v4表示で924物理行/606可視行/318伏字を読了した。
元担当の原評価・旧v5/v6の読了/notice reuse・未許諾/採否nullを、親のv4可視範囲や現notice適用へ書き換えていない。
3個別MITは全21行を読み、元byte/hashと改行を保持し、249のCRLF21も維持した。
ライセンスは選択した可視workflowの日本語改訂・伏字参照へ適用する範囲だけで、実作者性・未知/未読・第三者やserviceの一般的な権利の保証ではない。
source.license.acceptedは既存manifest形式の必要fieldであり、本人が新しい権利を発行した意味ではない。

new3のSKILL、guide、正負例、3つのsource metadataとmanifestの関係、個別license/伏字参照/原評価を確認する。
「他のsupport本文は親未読」という範囲を、読了した個別noticeまで未読という意味にしない。
原251は保存116支持のうち元担当が読んだ本文・旧notice reuseと、114保存未読を区別し、親が全supportを読んだとはしない。
現在のOAuth観測から過去の認証・課金を推定しない。

固定PUBLIC filesとそのmanifestに埋め込まれた出力だけを技術資料として使う。
起動に必要な確認に限り、D:/CC/AGENTS.md、D:/CC/Policies/owner-agent-conduct-20261021.md、D:/CC/Skills/.system/skill-creator/SKILL.md、D:/CC/Skills/skill-maintenance/SKILL.md、D:/CC/Skills/skill-maintenance/references/public-20261020.md、D:/CC/Skills/scoped-code-review-prompt/SKILL.md、D:/CC/Skills/japanese-report-writing/SKILL.mdと、自分の作業場所のgit statusを許可する。
その他の検索・追加取得・checkout・編集・test/build・API/HTTP・実配置・中央/backup/私的設定/元corpus・秘密・他担当・課金は行わない。
原資料のinstall、全DB公開、rootへの一括復元、秘密表示、接続作成/削除や通知を実行しない。
自分の実model/effortは親が正常metadataとnativeで独立照合し、自己申告での確認や内部名を見られないことによる不要な再起動を求めない。

本文の全文確認に加え、次の3つの合成入力へSkillを適用し、どの手順を使うか・判断と不足情報・実行しなかった操作を簡潔に返す。
これは実環境の操作や利用実績ではない。

1. 動的actionのmetadataはreadProfileと名付けているが、handlerには権限変更が含まれる。呼出し主体はtenant Aのprofile読取権限だけを持ち、requestはtenant Bの同じ数値IDを指す。
2. 復元対象は200ファイルで、snapshot manifestとstaging結果は199ファイルで一致した。欠けた1件の除外理由は不明で、現在の原本は200ファイルのまま残っている。backup終了codeは0だった。
3. 同serviceにsubject AとBのACTIVE接続があり、依頼対象はA/Org Xだが最古の接続はBである。proxyはHTTP200を返し、nativeの業務結果は部分失敗、次page tokenもある。別のwrite O1はtimeoutしたが同じO1の配送記録が見つかった。

既存3048の構造/参照/hash/正負例PASS3、関連Go package9.579s/vet成功と11反例を記録しているが、それを自分で再実行したとは書かない。
metadata発見0を物理不在の資格や全catalogの発見保証へ変えず、3案の実配置・利用可能・全16完了は未確認のまま保持する。

最終返信は日本語で「状態: 完了」から固定commit・全13PUBLIC資料と21出力の実読範囲・Must/Suggestion/Nit全件・3合成入力の適用結果を返す。
全出力を読み、Must0の時だけ、次の1行を実際の判定で付ける。Mustが残る場合はaccepted:falseと実数を返す。
SKILLS3080_ACCEPTANCE {"domain":"skills-3080-review-v1","manifest_sha256":"212e5645cd33a0ae0abe44b1b75fa2079826889b8c41f4857963808649d90c24","accepted":true,"must":0}
