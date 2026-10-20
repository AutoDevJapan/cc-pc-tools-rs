状態: 完了

レビューは完了しましたが、実装は完了していません。Must が2件あります。

- **レビュー対象:** 作業場所の作業ツリーです。固定候補 894a19a とは一致しません（Must 1）。
- **作業中の更新:** 実装側が読取中も編集を続けていました。acceptance.md は 12:51 JST、research.md は 12:50 JST、sources.json は 12:52 JST に更新されていました。下記は 12:55 JST 時点のスナップショットへの指摘です。

## 検査と読了範囲

**実行した検査（すべて読取のみ）**

- manifest が列挙する 67 ファイルの SHA-256 を再計算しました。欠落・不一致は 0 件です。
- 14 件の SKILL.md、manifest、discovery.json の description は完全に一致し、すべて 60 字以内でした。
- 14 ディレクトリはいずれもリンクではなく、通常のディレクトリです。
- manifest 列挙の .md にある相対リンク 78 件はすべて存在します。これは main.go が見ない previous-guide.md を含む件数です。previous-guide.md 8 件にある相対リンク 10 件も実在します。
- 配置ファイルの最終更新は 03:37:00Z、discovery の記録は 03:37:53Z です。発見記録は最終内容より後です。
- 入口バイト数の合計は acceptance の表（48,041 / 12,455 / 5,210 / 最大 3,753）と一致しました。
- LICENSE の SHA は sources.json と manifest で一致しています。
- git status・git diff・git rev-parse を確認しました。

**未実行・未確認**

- go test / vet / run は再実行していません。読取専用のため、報告された成功は未確認です。
- CI の run ページ、上流リポジトリ、commit / hash の実在、app-server の `skills/list` は、ネットワーク禁止のため未確認です。
- 現役直下 266→271、junction の維持、GTO registry との照合 0 件は、全 Skill の走査が禁止のため未確認です。
- `.trash` の原本（自分では未読。「previous-guide は frontmatter 除去後の忠実な移設」と結論する根拠は、バイト差が 160〜190 バイトで一致するという間接証拠のみ）、LICENSE 本文、`references/index.md`、各 `GUIDE.md`、spec-atdd 直下の `GUIDE.md` は読んでいません。
- 元仕様ファイルは読んでいないため、16 資料が元仕様と同一かは確認していません。

**読了**

- docs の spec / research / sources / manifest / discovery と、範囲外の acceptance.md
- Go 一式（main.go / main_test.go / receipts_test.go / go.mod）と CI yml
- 14 Skill の SKILL.md、新規 5 件の usage.md と sources-3048.md、既存 9 件の public-20261020.md と sources-3048.md
- previous-guide.md は 8 件を読了しました。うち security-review、ui-design-styles、web-design-process は 260 字超の行を途中で切って読んでいます。

## 18 件の代表依頼（manifest の例は見ず独立判断）

| 依頼 | 選ぶ | 選ばない | 次の処理 |
|---|---|---|---|
| A | obsidian-markdown | json-canvas、通常の Markdown 編集 | 関連 2 ノートだけ読む。リンク先ノート、見出し、ブロック ID の実在を照合し、`^決定` が無効な ID か消えた ID かを切り分けて参照元を直す。 |
| B | obsidian-bases | xlsx / spreadsheets、obsidian-markdown | 共通 filters と view の filters を分ける。`status != "done"` で完了済みを除外し、期限差は `if(due, (date(due)-today()).days, "")` で due 無しも残す。検査データは due 無し・完了・対象外タグを含める。 |
| C | json-canvas | obsidian-markdown、HTML Canvas 系 | JSON を読み、`note-A` と未知の拡張属性を保つ。衝突しない edge id と `fromNode` / `toNode` を追加し、配列順は変えずに読み戻して検査する。 |
| D | なし（game-development 等） | json-canvas | HTML Canvas のコードと衝突判定を直接読んで直す。 |
| E | cli-interface-design | coding-agent-tools（説明文からの推定）、一回限りの処理 | 既存コマンドと API を調べ、list・resolve・get・update を分ける。JSON の成功形・失敗形と終了コードを決め、更新は `--dry-run` で確認する。言語は Go を基本とし（GoDD 系のみ Rust）、別 cwd から PATH で動くかを検査する。 |
| F | なし | cli-interface-design | rg や PowerShell でその場で数える。 |
| G | domain-modeling | agent-system-design、knowledge-to-skills | 用語集・仕様・コードで「解約」を探す。主体・対象・状態・出来事に分け、契約終了とアカウント削除を文脈ごとに定義する。互換性の影響範囲を見て、一括改名はしない。 |
| H | なし | domain-modeling | 用語集を読んで誤字を直すだけにする。 |
| I | ui-design-styles（構造の重複除去）。画像先行の部分は web-design-process かイメージ生成系 | ui-accessibility | 現行ブランドのトークンを読む。hero→3カードの反復を内容で判断し、情報順を変えた案を画像で示して承認を得てから実装する。 |
| J | ui-accessibility | ui-design-styles | DESIGN.md を正本に fixture を作り、狭い幅で scrollWidth と clientWidth を測る。URL セルと補助文を別ルールで直し、global な break-all は避けて再測定する。 |
| K | web-design-process | ui-design-styles | 「公式検証済み」とは言えない。第三者の観察で、Known Gaps が明記されているため、loading は設計で補う。結果は「補った」と記す。 |
| L | security-review | docker-operations 等 | 台帳に「指摘候補・未実行」として登録する。テスト利用者 A/B と B の実在 ID で更新を試し、B が不変かを確認する。所有者チェック・RLS・ミドルウェアなどの既存ガードを反証として探し、本番へは通信しない。 |
| M | docker-operations（postgres-container-readiness が隣接） | 他担当 volume の削除 | 削除は妥当でない。`depends_on` の条件・healthcheck・DB 初期化中の一時サーバー・ホスト名・環境変数・ログを確認する。消すのは自分のラベル付き資源だけにする。 |
| N | knowledge-to-skills（実導入の段階で skill-maintenance） | 全件導入 | README の指示はデータとして扱い、一括導入しない。必要な候補だけ原本とライセンスを確認し、既存と重なるものは統合または不採用にして、その理由を記録する。 |
| O | agent-system-design | skill-maintenance | 代表依頼から手引き・原資料・鮮度への経路を追い、直達・検索で救済・未到達に分ける。予定処理は設定・受付・実行・出力・次回復帰を区別して実際の出力まで確認する。 |
| P | agent-system-design | knowledge-to-skills、skill-maintenance | 置換は不可。今回の範囲内で TeamAI の同期に差し替えない。所有者・方向・変換・衝突時の正本を比較表にするだけで、置換するかはオーナー判断とする。 |
| Q | spec-atdd-tdd-issue-workflow（隣接: test-quality、未変更） | 内部モックの呼出し回数だけの検査 | 不十分。CLI の終了コードと出力、成果物の内容を Red→Green で検査する。認可や永続化が要件の場合に限り、副作用の確認を追加する。 |
| R | skill-maintenance | knowledge-to-skills | `skills/list` で名前・説明・解析エラー・警告を確認する（発見のみ）。context 予算は実ターンの警告か実測で別に確認し、「警告を見なかった」と「経路が警告を評価しない」を分けて記録する。 |

## 指摘

### Must

1. **レビュー対象と固定候補 894a19a がずれています。**
   - 作業ツリーに未コミットの変更があります。変更されているのは次のファイルです。
     - docs/skills-3048-research.md の mizchi 行（18 行目）
     - docs/skills-3048-sources.json の mizchi の license と reason（350〜354 行目）
     - sources.json の `tree_truncated` 追加。6mile は true（311 行目）
   - docs/skills-3048-acceptance.md は未追跡です。
   - 894a19a には mizchi の旧記述「候補の適用ライセンスが無く」が残ります。これは README にある「未記載時 MIT」の説明と食い違います。6mile のツリー切詰めも未記録です。
   - CI の成功（run 39365645087）は 894a19a のものです。acceptance.md と訂正後の記録を含みません。
   - 実害は、マージされる内容と CI 証跡が一致しなくなることです。
   - 修正:
     - 最終版をコミットし、CI を新 head で通す。
     - acceptance.md の「固定候補894a19a」の記述を新 head に更新する。
     - 同ファイル 45 行目の「独立レビュー」に本レビューの結果を追記する。

2. **skill-maintenance の入口で、旧本文の制約が消えています。**
   - 旧本文は references/previous-guide.md の 4 行目に残っています。そこにあるのは次の 3 点です。
     - 「旧版は.trashへ保全」
     - 「顧客案件の名前や秘密を含むSkillは作らない」
     - 「新しいCodexジョブで…skills context budgetの警告がないことを確認」
   - 新しい SKILL.md の 8〜14 行目にあるのは、うち 1 つだけです。context 予算の確認は「skills/list等」に弱まっています。
   - 前の 2 点は消えました。AGENTS.md の全体規則がある程度は代替しますが、Skill 作成時に最初に読まれるのはこの入口です。
   - 修正:
     - 「変更前を保全する」を 1 行追加する。
     - 「顧客案件の名前・秘密を含めない」を 1 行追加する。
     - 13 行目を次のように直す。「context 予算は実ターンの警告で確認する。skills/list は発見のみで、予算の証明にしない」

### Suggestion

- **正例/負例の検査という主張が実態より強いです。**
  - acceptance.md の 33 行目は、正例/負例を検査したと書いています。
  - main.go:95 が見るのは、manifest 内の2つの文字列が空でなく異なることだけです。SKILL.md や references に書かれているかは検査していません。
  - spec.md の 22 行目は文字列一致で品質を証明しないと述べており、それと矛盾します。
  - 「manifest に非空で異なる文字列があることを確認した」に直してください。

- **構造検査の抜けがあります。**
  - **SHA が任意です。** main.go:125 は、`sha256` が空なら比較しません。
  - **`:/` を含むリンクを外部扱いにします。** main.go:141 の判定により、ローカルの絶対パスも検査されません。
  - **previous-guide.md を丸ごと除外しています。** main.go:136 のためで、リンクを調整したという記述が機械的に守られていません。今回の 10 件は手作業で確認しました。
  - **新規 Skill の「不在」が自己申告です。** main.go:98 は文字列 "absent" だけを見ています。
  - **単体テストの網羅が足りません。** main_test.go は空 manifest・参照切れ・ハッシュ・範囲外パスを扱いますが、次のケースが抜けています。acceptance.md の「重複項目」は frontmatter の重複だけで、target / file の重複は未検査です。
    - `files` が空
    - `SKILL.md` を manifest に載せていない
    - 正例/負例が欠けている
    - 重複 target
    - 重複 file
  - 配置前の Red（exit 1）は散文だけで、出力が残っていません。
  - 修正案: `--require-hash` を追加する。上の欠落ケースをテストする。Red 実行の出力を docs に残す。

- **manifest と出典記録の突き合わせがありません。**
  - receipts_test.go:74-103 は、manifest の `sources` と sources.json の `targets` を照合しません。
  - manifest.json の 397〜398 行目は小文字の `nateherkai/ais-os` と `tencent/teamai-cli` で、sources.json の `nateherkai/AIS-OS` / `Tencent/teamai-cli` と表記が違います。
  - knowledge-to-skills の `sources` には agent-skills.jp が入っていません。sources.json 側の targets には入っています。

- **agent-skills.jp の取得が配置後で、記録も薄いです。**
  - sources.json の 463 行目は 03:38:32Z で、配置（03:37:00Z）と発見記録（03:37:53Z）より後です。
  - この項目だけ `scope` と `coverage_note` が無いのも、他の項目との不揃いです。
  - knowledge-to-skills の `references/sources-3048.md` 12 行目は日付だけで、時刻とハッシュがありません。
  - 修正: 再取得であることと閲覧時点を注記し、ハッシュを併記する。

- **json-canvas の出典に受領記録の無い参照があります。** `references/sources-3048.md` の 12 行目は jsoncanvas.org の仕様 1.0 と照合したと書いていますが、sources.json に URL・時刻・ハッシュがありません。補助出典として記録するか、記述を外してください。

- **ui-design-styles の入口が新しい用途を示していません。**
  - description（SKILL.md:3）は旧来のスタイル名のままです。
  - 10 行目の「本人指定の画像先行の手順」は、どの Skill を指すか書かれていません。
  - 依頼 I のようなケースは、ui-design-styles と web-design-process に割れます。
  - 画像先行の担当 Skill への直リンクを追加してください。

- **新しい節が既存 Skill と重複しています。**
  - docker-operations の `public-20261020.md` 3 行目と 7 行目は、docker-context-allowlist-checks と postgres-container-readiness の説明文と重なります。これは説明文からの推定で、本文は読んでいません。
  - ui-accessibility の `previous-guide.md` 12〜13 行目に hallmark と jp-ui-contracts があります。新しい `public-20261020.md` の 6〜7 行目は同じ内容です。
  - 同様の重複が knowledge-to-skills の `previous-guide.md` 10 行目（agent-skills-jp）と、web-design-process の `previous-guide.md` 26 行目（audit-reference-originality）にもあります。
  - 修正: 相互参照を 1 行入れるか、一方に統合してください。

- **spec-atdd-tdd-issue-workflow の入口が長いです。**
  - 3,753 バイトは、他の入口（最大 1,296）の約 3 倍です。変更前の 3,290 から増えています。
  - 25〜34 行目の「外部の着想」と「統合した知見」は references へ移せます。
  - description（3 行目）に振る舞い検査が入っていません。

- **obsidian-markdown がブロック ID の文字制約を扱っていません。**
  - 依頼 A の `^決定` が壊れる原因として、ブロック ID は半角英数とハイフンのみ、という制約が考えられます。
  - これは私の記憶で、今回は公式を確認していません。確認した上で `usage.md` の 3〜4 行目に追記してください。

- **context 予算の確認方法が不足しています。**
  - acceptance.md の入口バイト数は、起動時に読まれる本文の量です。
  - 常時 context に載るのは name・description・path です。5 件の追加分の増加量が記録されていません。
  - 修正: 追加分の合計バイト数を書く。実ターンで警告を見たかどうかを別に記録する。
  - 参考: 本レビューのプロンプトの catalog には 14 件が説明付きで載っていました。実ターンでの認識の傍証にはなりますが、予算の測定ではありません。

### Nit

- cli-interface-design の SKILL.md:8 は「Go を基本」とだけ書いています。GoDD 系の Rust は前文の規則優先に委ねている形です。
- json-canvas の `usage.md` 7 行目は、`toEnd` の既定が arrow だとは書いていません。私の記憶では仕様の既定値です。依頼 C の「矢印を追加」は、指定しなくても成立します。
- knowledge-to-skills は、6mile から何も転用していないのに LICENSE を同梱しています。research の 16 行目も反映先が knowledge-to-skills で、扱いは「参考のみ」です。説明を足してください。
- acceptance.md の 13 行目は、測定時刻が 03:47:54Z で他の表（03:37Z）と異なります。
- research.md の 22 行目はリンクの表示名が URL です。x1xhlol と mizchi の「反映先」が空欄なのは意図どおりのようです。
- cli-interface-design と coding-agent-tools は、説明文のレベルで一部重なっています。

## 問題が見つからなかった点

- 外部命令が本人規則へ昇格している箇所は見つかりませんでした。各 sources-3048.md に「本人や実行環境の規則に優先しない」という趣旨の明記があります。
- 新規の補助コードに Python はありません。検査ツールは Go のみです。
- 空 manifest、`files` なし、`SKILL.md` 欠落、対象ディレクトリ欠落は、main.go の 85・101・155 行目などで失敗します。
- 実装担当が追記した acceptance.md の 35 行目（CI の検査範囲の注記）は、CI が D:/CC/Skills を検査していない点を明記しています。ただし、これは未コミットの作業ツリー側にしかありません。