# 旧レビュー3094の受領と修正

固定head `dabcb32a1c0db48e65c71e1c220d03d42016ed41` の原応答を、元の依頼と具体的なファイルを添えた正規の自動実行審査で読取許可を受けて取得した。原応答SHA-256は `b259bfebf5f5455a6b0fad80d483aa8a3e1c6c640ed4012b795e81e3a51b39e2`。Mustは0件。読了は旧10候補の設計と集計器であり、その後の変更や今回生成した14件の配置データの承認とは区別する。

ToDo3100は、既存CLIに汎用の保護分類確定機能がないことを確認した。分類未確認を許可とは扱わない。今回の結果読取は個別の通常審査で許可された操作で、guardの分類や設定を変更したものではない。新しい配置操作も固定データと元依頼を示す個別審査の対象とし、読取許可を配置全体の許可へ拡大しない。647件の既拒否取得は別のまま保持する。

| 指摘 | 対応と検証範囲 |
| --- | --- |
| 起動条件 | 14件すべてに上流の明示起動設定と、派生版の起動方針・理由を保存。現在のcreator方針に従い暗黙選択を維持し、用途を狭い説明と手順で限定する。明示起動だけへ設定を変えたとは主張しない。 |
| shoehornの用途 | テストコード専用で本番コードへimportしないことと、非テスト側へのimportがない確認を追加。 |
| 障害診断 | 取得前の秘密欄除外、テストモード、性能の基準測定と反証可能な複数仮説を追記。 |
| 学習とcommit前検査 | 既存知見との重なりを明記し、既存の記録先を優先。本人が明示した型検査を削らず、通常案は軽いcommit前整形とpre-push/CIへ分ける。 |
| 原文・許諾の保持 | 配置データには入口ごとの原文全文、MIT原文、固定出典JSON、参照を含む。原文はupstream-original.txtで、別のSKILL.mdとして発見されない。実際の永続配置は未実施で、配置時に全ファイルを読戻し照合する。 |
| noticeの改行 | 3つの許諾原文にgitの-textを指定。候補検査でMIT原文のSHAと著作権表示を確認。 |
| 数値の整合 | 全unit、本文の有無、重複、15repoの入口数、全batchの候補数と固定版をCIで照合。 |
| 中断時の集計 | queueをpartialへ書き、Syncとrename後に行数・SHA付きsummaryを公開。失敗と既存出力拒否を検査。 |
| 規模拡張 | batchとsource assessmentをglobで検査し、候補名の重複を検出。原文の入口以外、凍結・beta状態、root許諾と個別素材の違いを明記。 |

配置データは原本へ書く前に、既存3048検査器の同じ構造・参照・ハッシュ検査をメモリ内で実行する。これが通っても、原本配置・Codex発見・実ターン評価の成功とは扱わない。配置処理は新規のみを扱い、既存targetは全件事前確認で拒否する。参照資料を作って照合した後、SKILL.mdを最後に作る。中断時は同期した記録と部分成果を残し、上書きで修復しない。

上流の`.agents/invocation.md`、`SCOPE.md`、凍結miscの説明、betaの説明を固定版で読み、各候補へ上流状態と採用理由を追記した。完全な固定treeでLICENSE/NOTICE/COPYINGに該当するpathはrootのLICENSEだけだった。この確認は、上流の全参照資料や素材を包括的に採用する判断ではない。

## 3116の実ターン受入と3120のDocker案レビュー

3116は冒頭metadataの14名・正しい配置先を確認し、5本文を意図的に開いて5正例へ適用した。近い16負例では対象への過剰選択は無かった。1ターン内の境界評価で、独立16モデル試験や実製品の実行確認ではない。使用量全体は未観測。見える会話にcontext警告を観測しなかったことだけを記録する。

3120は固定4cc10d5のDocker8原文を全文読み、Git blob・tree・原文・許諾・初回14件の実ファイルを独立照合した。Must0。ただしApache再配布に必要なS1と、plan/起動metadataのS4は配置前の条件なので今回反映する。

| 指摘 | 今回の対応 |
| --- | --- |
| 3120 S1 | 選択入口自身のApache宣言、既知のDocker許諾原文SHA、個別scope/完全tree/入れ子notice無しを確認。SKILL.md本体へ出典・固定版・日本語への改変・公式版でない旨を表示し、動的なlicense名で参照を生成する。原文とApache原文は個別Skill内へ保持する固定データを作る。 |
| S2・S3・S6・S7 | 未確認OAuthでは静的レビューまでと明記。evalのAPIキー自動転送、restricted、旧sandboxラッパー、host command、volumeサイズ、global範囲、近い負例を補足。MCP・APIキー・GPUを勝手に追加しない。 |
| S4 | 8つのskill.yamlは既取得supplementから読み、8つのagents/openai.yamlは固定commitの公開YAMLだけを取得。全16の本文SHAと、追加8の固定Git blob SHAを確認した。MDX/text/templateの既拒否647には該当せず、再取得していない。8つともallow_implicit_invocationはtrue。付属MCP依存は資料として保持し、中央へ登録しない。batch03へsource_path/起動方針/上流状態/個別許諾を記録。 |
| S5 | Apacheを含むnotice/metadata/plan/tree、初回実配置manifest・発見数・件数の関係をCIで確認。Dockerの候補区分もbatchと照合し、元の8草案を別の8件として足さない。 |
| 3104の配置処理 | 個別許諾を型付きで検査。書込み無しpreflight、全target確認、receiptの中央外への限定、開始時bundleSHA/rootと失敗行を追加。2番目の既存targetで最初も作らない反例、途中失敗でprefix成果/journalを保持する反例、Windows祖先junction拒否を確認。SKILL.mdは同期済みpartialから新規hardlinkで原子的に公開し、既存名を置換しない。 |
| 3104の旧14入口 | shoehornの説明とgrillingの壁打ち先の改善提案は、配布済みv2と旧版を保った更新手順が必要。新規専用applyで上書きして直したとは扱わない。次の限定更新として保持する。 |

Apacheの正例が旧MIT限定で失敗するRedを確認し、生成後の改変表示/原文保持と不完全な許諾根拠の拒否をGreenで確認した。installer/catalogの対象test/vet、Windowsjunction反例、同じ3048検査器による8件40ファイルのメモリ内検査は成功。まだ中央への追加8件の配置は0。旧14件は固定079f9d9のbundleのままで、再renderや上書きはしていない。

CIの残るGo CodeQLは、Linux専用のcc-pc-tools-linux runnerを待っていた。GitHub上のrunnerはonline/busyで、実行中の他ジョブを止めていない。共有runner待ちの既存担当3122があり、新しい同じ修復担当は作らない。最新の検査成功と新しい配置データの別モデル確認後に通常マージする。

## 3138の指摘修正（配置前）

固定90e66e9に対する別モデルレビュー3138はMust1。docker-docsのskill.yamlにある暗黙選択trueがplanではnullになっていた。保存した原文16件とplanの値を全件照合する回帰検査を追加し、修正前にこの1件で失敗するRed、修正後に16件一致のGreenを確認した。

同レビューの具体的なS1/S6に従い、runとstandalone sbx4入口へ「CLI未導入・版不明・必要OAuth互換未確認なら、モデルやdaemonを起動せず静的レビューで停止」を本文へ追加した。configの接続確認は公式資料か本人許可済み成功記録に限定し、試しモデル呼出しを根拠にしない。Prd/専用入口/experimentalの表現も指摘の範囲で補った。

再生成したDocker v2は8件40ファイル。SHA-256は5fb94978bb2627ea0b219c0a989ad3b4578f0d093a539d6027e93aa108060ddc。変更16ファイルは入口8とsource.json8だけで、上流原文・許諾原文・sources.mdは旧レビュー版と同一。既存3048の同じメモリ内検査とcatalogの対象test-count1/vetは成功した。配置ツールの実行コードはレビュー済み90e66e9から変更していない。3138のMustはこの回帰検査で修正確認済みで、別の新規機能を追加したものではない。

S3の公開後partial削除失敗は保守的なfailed記録として残す。起きた場合はcompleteと呼ばず、公開済みtargetとjournalを保持し、実ファイルのSHAを照合して未作成targetだけの新固定bundleを作る。既存dirを消したり既存名を上書きして再実行しない。D上の自己所有領域で前回apply反例とhardlink公開を実行成功している。

## 配置後確認3151と次のKepano6入口

3151は固定932a887の実8入口を全文読み、実40ファイル・原文・許諾・16policy・台帳22行を照合し、Must0を返した。各8正例/8負例は1turn内の静的適用であり、独立8モデル試験や実CLI成功ではない。3138のMustだったdocker-docsのskill.yaml trueも実source.jsonと原文16/16で一致。runtimeコード差分0も確認した。この限定受入を22件の受入済へ反映し、全16資料や全supportを監査済みとは扱わない。

Kepanoの固定commit3ccff5338ea700537839b21900aa5358a0402c98/tree c9a52b408a89bd5a42b0ea6ab65c39221c1d64f2は完全で、標準入口6件を今回全文読取/SHA照合した。rootMITだけで入れ子notice無し。元3048がjson-canvas/obsidian-bases/obsidian-markdownの同じcommit/入口SHAを参照したことを公開sources記録と現役metadataで照合した。新しい統合や同名Skillの上書きはしていない。未読supportを未判定に保つ。

残る3件はbatch04として準備した。Defuddleは許可された公開HTMLの本文抽出、Knapは入力型/検査/上書きと衝突管理、Obsidian CLIは明示vault/pathと限定操作を扱う。上流のnpm install/npx、active vault/file、clipboard、eval/plugin reloadを自動で採用せず、実CLIの利用可否と静的な手順を分ける。UI変更は同じ画像・範囲の回答を必要とする本人方針を保持する。

CLI有無のGet-CommandではPATH上に3名が見つからなかった。未導入と断定せず未確認として扱い、CLI/app/vault・モデル・GPUは起動していない。未知の既存Skill本文を一括で読んでいない。

Kepano候補は3件15ファイル、最大入口2616bytes、bundle SHAbe842b7d3bbcc7b00a6b78a81f20310d1a2dbcf8dd5adcbaf75c83b6e662de54。公開原文とMITを個別に保持する。既存3048検査器のメモリ内検査、catalogの対象test-count1/vet、書込み無しpreflightは成功。Kepanoの実配置は0。新しい候補で25件となるが、導入済みは22件のまま。次の独立レビュー後に通常の個別実行審査と限定配置・発見へ進む。件数上限は置かない。

## 2026-10-21: 3165/3168受領と配置前修正
3165はb570/旧Kepano bundle全文と固定原文、MIT、treeを確認しMust0。具体Suggestion1〜6を反映し、CLI引数/対象不足/上書き/stdin/batch/代替通常手段と出典の適用範囲を補った。v2 bundle SHA 1c9d347e347a315d9c315c1bec8142743c4cbf5624512ab161789bf2aaf7b516。原文3/MIT3は不変。実CLI・vaultは起動していない。
3168は初回所有3名の実15 before SHAと提案15 after SHAを照合。Must1（source.jsonのCRLF）をLFへ修正し、指摘の期待SHA3/3に一致。意味内容はレビュー版と同一。反復指摘の説明2件とgrilling経路を更新する。独立レビューのSuggestion1〜5を実更新手順へ採用し、固定SHAと旧版保全を限定計画に記録。新規専用installerで既存を上書きしない。両方まだ実反映前の記録。
3165のモデル指示はClaude Sonnet5.5/max、静的3正例6負例1turn。3168もSonnet5.5/max、実モデルtrace/OAuth経路はレビュー内部で未確認。新しい説明の実選択やcontext予算の測定ではない。残る全候補の完了ではない。
