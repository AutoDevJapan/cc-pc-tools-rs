# 稼働証拠の常設とConsoleの派生成果の分類（3344 / 元3338）

本人の「じゃあ、解消して。」を保持した元3338の依頼に基づく技術修復。
3081は完了し、PR371の統合と2本の配布を親が受領した。3344が後続のguardの
単一writerになる。3340のGo観測部品、原保護台帳、禁止3件と変更管理は変更しない。

## 入力と証拠

- `D:/CC/Policies/runtime-source-dependencies-3338.json` は稼働入力の保持登録。
  旧入力を移動・削除して試さない。公開された正確なGitコミットを通常のGitで取得し、
  対応するsource/byte/独立receiptのSHAと物理パスを確認して常設する。
- 複製したbuild receiptは元の全バイトとSHAを保ち、別の移管receiptでコピーの事実を記録する。
  原build時のcwd/path/timeを複製先の値へ改変しない。
- 元3264のhandoff、manifest、全109件の分類結果、元53ファイルと派生56ファイルの
  対応を使う。private repositoryは実ID1429803672 / AutoDevJapan/local-ai-console。
  payloadの普通の閲覧は正常分類後だけとし、分類器の限定hash照合と区別する。
- 保留fixture2件、未知、別owner、別hash、変更source、リンク、元client一致は拒否する。
  生成設定3件とpackage manifestの5依存変更も元の生成・移管記録に結ぶ。
- OMBの旧画像と拒否receiptを保存する。新captureの原tool call/result/元path/画像SHA/
  loopback URL/目視確認を使う。古い画像や要約を実tool記録へ書き換えない。

## 受入条件

1. 旧Tempや終了するworktreeを参照しない固定された常設source/receiptから、通常guardが
   既存launcherの分類を継続する。対応と独立証拠を欠く場合は拒否する。
2. 合成テストで旧一時source不在の正例、改変receipt/source/byte、リンク、別owner、
   親・隣接・ADS、過去client一致の負例を確認する。実稼働入力は壊さない。
3. 既存guardの通常入口が派生56件を出所付きで正常分類する。53→56の対応は
   元hash・Git blob・固定commit・独立manifestを確認し、単なるパス許可にしない。
4. 別モデルの固定差分レビュー、関連Go検査、正式CI、通常merge、正規配布、
   配布したbyteの通常guard受入を確認する。
5. 元担当との単一writerを保ってConsoleを既存private repoへ保存し、remote commit/treeと
   56実bytesを照合する。UI実装・稼働版の置換・保留fixtureは含めない。
6. 新しい非秘密OMB公式loopback画面を正規captureし、同じ通常Discord
   `log --attach`の実添付と読み返しを確認する。配送とUI実装承認を区別する。

## 検査と終了の区分

変更前に振る舞いのRedを実行し、最小の修復でGreenを確認する。Goのguardとその
実入口・利用側を対象とし、Cargo/GPU/Civ6は起動しない。sourceの取得、コピー、
分類の合格、CI、merge、配布、外部保存、画像配送は各々の実結果で判定する。
親への実結果は `D:/OMB/workspace/recovery-20261022/permanent-provenance-result.json`。
未完条件と再開手順を別のprivate receiptにも残す。

## 実測した第1区切り

- Issue375。旧分類器は実candidateのpackage manifestを未分類と返した。
  `TestModelUpdateRuntimeInputsArePermanent` は旧worktree依存を理由にRedとなった。
- 公開元の7固定commitをGitで取得し、元の独立receiptと候補をSHA一致で複製した。
  原receiptのcwdや生成時刻は変更していない。旧sourceは残した。
- Console53件はWindowsの改行も含めて原sourceと同じバイトを複製し、公開Gitの
  53blob、旧/新SHA、追跡sourceの差分なしを照合した。これはGit取得後の明示した
  byteコピーであり、新しい生成・ビルド証拠と主張していない。
- `go test -count=1 -run 'TestPermanent|TestModelUpdate' .` は成功。
  Consoleの合成Git/ファイル境界検査は78.97秒で成功。最初の50秒の検査補助の期限は
  短すぎたため、元の時間切れを保持し、240秒の有限枠で同じ関連検査を完了した。
- 検査用の新しいguardは実candidate manifestを通常の`--guard-path`で正常分類した。
  この時点では本番配布・全56件の受入・外部保存・画像配送は未実施。
- 実private repoは既に3339の新Consoleを保存し、mainは別の新しい履歴だった。
  古い56件のarchiveでmainを上書きしない。元担当を保持し、正常分類と正規送信審査後に
  この旧1commit/56bytesを別の保存用refで照合する。

## OMBの新しいcapture

今回の担当がChromeの公式`http://127.0.0.1:38799/`を新規タブで開き、
`mcp__cua_repl.js`の実`getScreenshot()`戻り値を目視確認した。親には実行済みの
正確なwrapperと検証入力を`omb-capture-wrapper-3344.json`で先に返した。
原Codex記録のsession/open call/result/capture call/resultを行番号とSHAで固定する。
変数を書き換える別CUA呼出し、別URL・別session・別tool・別の戻り値は拒否する。
toolが返すJPEGを初めてファイルに書く専用exporterを使い、旧PNGをコピーしない。
toolがディスクpathを返したとは主張しない。新しい元pathと実バイトを別途結ぶ。

証拠は当該通常logの稼働報告用で、質問画像の資格として登録しない。
原tool記録と、その戻り値を実際に見た非秘密確認が不足すれば通常分類しない。
UI実装承認はfalseのまま。元の画像と拒否receiptを保持する。
確認用タブは作成時の正式所有記録をinventoryと照合して個別closeし、closed1/failed0。

`--export-omb-runtime-capture`は入力pathやmanifestを受け付けず、固定した新captureだけを
新規作成する。既存の同byteは再試行でき、別byteを上書きしない。通常の添付は
配布したguardの`--guard-path`、同じ`discord_ask log --attach`で実受入する。

配布するguard Aはこの機能を含む固定public sourceから独立に生成する。別のreader B
だけへAの実SHAと原build receiptを結び、自己SHAの循環を作らずに正規CASで配布する。
A/Bのsourceと生成物、最終mergeと実配布、通常の分類受入は分けて記録する。

全guardの合成回帰はdf400542で405合格・既存1省略、192.32秒。vetは成功した。
その後の残るConsole receipt参照と旧source不在の合成検査は関連範囲で成功した。
実Aはb4872860を通常Gitで別途取得した常設sourceから生成し、元build receiptを
別ファイルに保存した。Bは4cd89bd2の別のGit取得先から生成した読取用の実行物。
これは別source/実生成/独立receiptの照合であり、別担当がbuildしたとの主張ではない。
通常のソフト資格検査で現在のguardからAへの一致を確認した。実置換はまだ行っていない。
配布手順は[有限な配布スクリプト](install-permanent-provenance-3344.ps1)を使う。
