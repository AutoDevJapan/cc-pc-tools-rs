# 既存6手順への重複掲載の統合と、次18用途評価の受入

元ToDo3080 / Issue283。実担当は`20261030-095653-todo-3080`、Sol/max。
全16資料・全73,220出現は未完了であり、この記録は次の有限な差分を扱う。

## 既存の6つのSkillへ同じ出典の掲載を結ぶ

`aspnet-core`、`chatgpt-apps`、`linear`、`security-ownership-map`、
`security-threat-model`、`winui-app`は、元のCreator4049で受入済みの固定出典と
日本語の適用手順を保つ。同じcanonical URLと保存本文SHAを持つサイト掲載6件を、
元の上流出現と個別noticeへ結び、既存の手順を使う統合として記録する。
統合数は掲載出現の件数であり、新しい6つのSkillやファイルの作成ではない。

上流は`openai/skills@49f948faa9258a0c61caceaf225e179651397431`の各
`skills/.curated/<target>/SKILL.md`と、その同じdirectoryの`LICENSE.txt`。
noticeは対象によって異なる3つのSHAを持ち、rootのlicenseへまとめて置き換えない。
適用は既に受け入れたprimary workflowだけで、支持のscript・template・asset・
第三者資料・サービス利用やサイト編集文の権利まで広げない。

元サイト記録のrepo・commit・pathは空のまま残し、解決できた上流の固定tupleを
別の欄に記録する。原サイトが独立の作者であることや、空欄を実測したGit情報へ
変えることはしない。同じ本文SHAだけを許諾の根拠にしていない。

現在の担当は、元の新規配置requestと凍結manifest・Creator4049を結んだ正規consumeで、
現在の12指示ファイル・21,416 bytesを同じbufferから読み、各用途の正例と負例を
確認した。保存原文と個別noticeも同じ固定manifestのhash・bytes・Git blobへ照合するが、
その機械照合を現在の担当による上流本文の新しい意味読取とは数えない。
原Creatorは編集・再起動・再資格化しない。

fresh Codex app-serverの`skills/list`では6件すべてが有効で、説明も保持した指示と一致し、
parse errorは0件だった。返却metadataは484件、走査上限の警告は2件あり、
全catalogの発見や実model turnのcontext予算の証明にはしない。
確認用app-serverのprocess tree終了も確認した。アプリ・UI・サービスへの実操作は0。

## 元の担当による18件の可視本文評価

同じ原reader`20261030-033405-todo-4134`の111〜128を、3つの原packetから受け入れた。
親は全用途・所見・改訂・正負例・未確認を読み、実Sol/max・同じthread/cwd・
時刻範囲・固定B0仕様と、実launch parentがnullであることを照合した。
元の親・現在のconsumer・null・欠落・空文字を付け替えない。

原の3packetは3,178物理行・2,824可視行・354伏字行・25範囲で、raw fullはfalse。
伏字の意味、各memberの文脈・支持・個別許諾・現在の実在や最終採否は未確認のまま。
親の新しい原本文・支持の読取や、新しい許諾・最終採否へ加算しない。

従来の229意味記録・67context・全伏字1件は完全保持し、全体snapshotは
247可視意味＋held1、processed248、member metadata274、原packet70となった。
今回の18件にはheldを入れず、既存の全伏字1を可視意味として再加算しない。
原packetの全欄を再構成したSHA、原struct順のmember SHA、source tupleと
実reader/consumer・境界を、既存の共通checkerで確認する。

## 実全件queue

| 単位 | 件数 |
|---|---:|
| 保存された全出現 | 73,220 |
| 評価記録がある出現 | 1,635 |
| 最終記録がある出現 | 194 |
| 評価と最終記録の交差 | 188 |
| 評価記録がない旧最終記録 | 6 |
| 評価と最終記録の和集合 | 1,641 |
| 評価済みで現在の採否が必要 | 1,447 |
| 本文未取得 | 428 |
| 追加取得の審査を保持 | 647 |
| 意味と個別権利の評価が必要 | 70,504 |

各集合は独立した件数として足さない。新規164は不変、作業版の統合は9から15となる。
元の除外記録15件の再確認と未知中央9件を保ち、残る最終判断は73,026出現。
raw/workflowの旧固定bucket588/807は範囲が異なるため変更しない。

manifest SHAは`51a0eeb89ecc54e89c1c27479366fb436e615f68b9e1226d0c05efc13ef5d45e`、
実queue SHAは`1db41b9f74283ac84306cd4ea47fb754afba6fd3aeedd556a4d8433c7bc30950`。
81 assessment artifactと台帳から全件を生成し、実bytesと完了summaryを読み返した。

実データの初回生成で、未知のサイトGitパスを必須とする旧検査の失敗を確認した。
`site:`出現でrepo・commit・pathが本当に不明な場合だけ空のまま扱い、
catalogがサイト以外、またはrepo/commit/pathが既知なら厳密な一致を維持する。
取得元のpathをサイトの値として捏造する修正にはしていない。

追加点検では、個別noticeを重複原文で置き換える、保存bytesを変える、台帳の
解決済みrepoや適用範囲を変える4反例が通ることを先にRedで再現し、
元manifestの両ファイルと、台帳の解決tuple・notice・限定範囲への照合を追加した。
既知repo照合の前回の負例と、未知siteの正負例も保持する。

修正後の関連Goパッケージ全体は、実queueと固定snapshotを渡して10.279秒で成功し、
vetも成功した。旧snapshotの検査を再利用し、原packet・member・actorとnullの改変、
個別noticeの置換・台帳との不一致を拒否する。証拠の件数項目名を誤った初回のテスト失敗は
原データを変更せず検査側の参照を訂正し、成功結果と区別して保持した。

この差分の二役レビュー・最終headのCI・通常mergeは、記録時点では未受入。
新規のSkill作成、中央ファイル変更、junction変更、秘密参照、外部サービス操作、
APIキーや新費用は0。停止した元研究の再開修復4261は別に実配布と実結果まで追う。
