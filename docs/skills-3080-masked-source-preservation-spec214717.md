# 取得済み原本と、伏字付きの表示を分けて保持する

元Issue283・ToDo3080の同じ16資料の拡張で、未配置の旅行3案を通常のCreateNewへ結ぶ。
固定入力23fcのmanifest11070b77は別モデル全文4083でMust0、CIと通常PR587のmerge4931まで確認したが、現用94c/817の正規qualifyは`qualified:false / reason:source`を返した。新規作成は0である。

原因は`skillsSourceClaimsPreserved`と`skillsSourceObject`が、取得済みの原本と同じraw byteのファイルを配置出力へ必須としていることにある。
今回の原本は元の取得済みarchiveへ保持し、原SHA・Git blob・lengthと個別MITを検証したうえで、親が実際に読めた伏字付き表示だけを出力案へ入れている。原本にある未読の伏字行を、受入を通すために公開・復元しない。

## 受入条件

- Given: 元の固定public source・取得済みcatalog/index/完全tree/commit・原本のSHA/length/Git blob・個別noticeの全byteが従来の独立した検証を通る。
- When: そのSkillの出力に、原文を行順のまま保った`<source-directory-name>/references/original-skill.masked.txt`があり、伏字は`[WITHHELD physical line N]`という該当行番号のmarkerだけである。
- Then: 原本は元archiveに残したまま、実際の出力byteを原本から計算した表示へ照合し、他のactor/review/Must0・CreateNew・現在ID/hashの条件へ進める。callerの「安全な要約」という自己申告だけでは受け入れない。
- 見えている行の変更、行の欠落・追加・並替え、別行番号、別target、全行伏字、個別noticeの省略・伏字、support/notice等の別role、原本/tuple/hash・未取得647等の条件違反は拒否する。伏字表示には少なくとも1つの伏字と空白以外の見えている行を必要とする。
- この表示形式はLFを用い、元のLF/CRLFの物理行と終端改行の有無を照合する。元SHAはraw原本のまま、表示SHAとは分ける。元のrawコピーがある従来の受入は保つ。

新しい分岐は既存の固定取得済みsourceの`skill_entry`で、名前が正確に`SKILL.md`のprimaryだけに限る。原本の内部検証、notice全byte、登録者・別モデル全文review・出力SHA、中央の不在/新規作成、元の秘密・顧客・未分類保護を変更しない。
伏字数を秘密の数や不存在証明、raw全文読了、著者性の独立確認、新しい許諾とは扱わない。未読の行と不明な旧reader条件は補わない。

既存の合成取得済みcorpus fixtureで正負例を先に確認し、実travel3の変更していないmanifestにも同じ条件を照合する。別モデルのMust・必要なCI・通常merge・同じ3686の正規Windows配布後、親の元の同じrequestで再確認し、CreateNewと現在の18buffer・発見・適用まで続ける。登録だけで全16資料を完了扱いしない。
