# 配布更新後も、受入済みの作成時の配布版を照合する

元Issue283・ToDo3080の同じ有限な拡張作業で、既に新規作成した公開Skillを正規の読戻しへ結ぶ。
2026-10-27の正常な`--consume-skills3080`は、旧担当175504が作成したillustration-craftについて`qualified:false / reason:creation_evidence / failure_stage:creation_header`を返し、本文を返さなかった。
最初に指定した`--consume-skills3080-created`は存在しないflagの入力エラーであり、資格確認の拒否や本文取得ではない。

作成時の配布版はSHA02c8cad7・source f30f9e76、現在の配布版はSHA43483253・source521c4c4dである。
現在のSkillsのソフトウェア照合表にはこの2組が無く、同じプログラムで作成してそのまま読み戻す場合だけ成立していた。
`skillsCreationHeaderBound`は元のnative作成記録を受領した後、manifestの参照や現在のファイルの読取より前にこの照合を行う。

## 受入条件

- Given: 修正候補を作る前から公開保存されている正規の作成・読戻しの観測で、配布版のSHAとsource commitが対応する。
- When: 後の配布版で、その作成記録と同じwriter・review・request・manifestを照合する。
- Then: 受入済みのソフトウェアの組を保持し、元の公開manifestの同じbyteと、別途の現在のID・hash・bufferの確認へ進める。
- 異なるSHA、同じSHAで異なるsource commit、別writer、別request/review/manifest、公開元の確認失敗は、元の公開manifestや現在の本文を読む前に拒否する。
- ソフトウェアの照合だけで元の保護設定・秘密・顧客の資産・未知の既存Skillや他のToDoへ権限を与えない。647件の取得審査、匿名中央9、元の原本・作成記録・読取条件は保持する。

## 独立した既存の証拠

02c8/f30は`evidence/official-ci3387-known-guard-baseline-f30f9e76.json`と、その元の`skills-3080-craft-create-evidence214717.json`、`skills-3080-codex3-after542-current20-evidence170221.json`に保存されている。
同じexact pairは既存Official CI側で既に受け入れられており、今回の候補が自己申告して新たに選ぶ値ではない。
4348/521cは`skills-3080-openai6-create-evidence214717.json`と`skills-3080-guard-current4348-observation214717.json`に、PR579の受入前から保存された実観測である。
これらは正規の作成時の配布版の観測であり、ソースとバイナリー全体の同一性や、過去の物理的な連続性・現在のOS writerを新しく証明したものではない。

修復はこの2組の既受入ソフトウェアの事実と対応するテスト・証拠だけに絞り、元のwriter・公開出典・個別通知・native呼出し・request・現在のobject/hashの検証を保つ。
今後のguard配布でも、更新前の配布版と既存作成記録の照合が保たれることを、更新前に保存した独立した観測と回帰テストで確認する。
別モデルのMust、関連Goテスト、CI・通常マージ、変更後sourceからのWindows実行物、正規配布とこの元の拒否ケースの正規再確認までを完了条件とする。
