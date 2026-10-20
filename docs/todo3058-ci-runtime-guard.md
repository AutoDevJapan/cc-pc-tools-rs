# ToDo3058: 万世紀CIの配備先を限定して照合する

親2962の独立前提を修復する。原保護名簿の復旧2467、旧192件、顧客、未知source、Tools全体へ対象を広げない。既存3011/PR252の3runtimeは変更しない。親のcoreコードと3workflowも変更しない。

## 正本と所有根拠

`repair-guard/civ6-ci-2962-contract.json` がビルドへ埋め込まれる正本。
実remote/APIで確認したAutoDevJapan/cc-pc-tools-rs（ID1406818713）の固定sourceだけを使う。
親の固定sourceパス、下限commit ab45cb5d221077fa65484c706b3353cdc4867c07、その子孫である現在HEAD、origin、物理一致、実GitHub所有者とIDを照合する。
sourceの作業コピーや配備先の設定から対象パスを追加しない。

## 作成と既存資源の区別

1. 親が自分の固定sourceの別モデルレビューと正式CI成功を確認した後、正規guardでrootを照合する。未作成ならrootだけが分類される。子や他のrootは分類されない。
2. 親は排他を取り、rootをexclusive mkdirする。既に存在した場合は初回作成として扱わず停止する。失敗したmkdirの後から既存rootを採用しない。
3. 親は直後に属性専用handleで取得したrootと親のNTFS識別情報・所有者を、固定sourceの `docs/civ6-ci-2962-deployment.private.json` に保存する。root配下の自己申告receiptだけでは既存rootを分類しない。
4. 以降はこの外部証跡が必須。domain、埋込contractの実バイトSHA256、親sourceの現在HEADが一致し、物理パスと所有者、volume/file ID/作成時刻が一致したときだけ既存rootを分類する。
5. 正確な未作成leafは、直近の親が既に証跡で分類される場合だけ作成対象になる。親はexclusive createで配備し、通常の上書きをしない。作成後に各objectの識別情報を外部証跡へ保存する。記録していない既存objectは拒否される。
6. 更新は `civ6-ci-{queue,windows,route}.exe.pr<1〜999999>.{tmp,backup}` と、同形式のqueue JSON・配備receiptだけ。親は更新先と現用の識別情報・実バイトpinを照合してatomic replaceし、置換後の新IDを保存する。勝手に未知existingをbackup名へ改名して採用しない。

親のレビュー・CI成功は親の配備工程で確認する必須条件であり、guardの成功だけでレビュー済み・CI済み・配備可能と判定しない。

## 証跡の形式

外部証跡は `civ6Deployment` の厳格なJSON。重複キー、未知field、末尾値、4MiB超、リンクは拒否する。

- `Domain`: `civ6-ci-2962-deployment-v1`
- `ContractSHA256`: contractのファイルバイトのSHA256（Git blobの改行と作業コピーの改行を混同しない）
- `SourceHead`: 親sourceの実HEAD
- `Parent`, `Root`: `Volume`, `ID`, `Created`, `Owner`。directoryのSize/Written/Changedは照合しない。
- `Objects`: rootからの相対パス（`/`区切り）→属性記録。通常fileは `Volume`, `ID`, `Created`, `Owner`, `Size`, `Written`, `Changed` をすべて照合し、link数1を要求する。
- root直下fileには reviewed byte pinの `SHA256` が必須。公式zipは正本のdigestと104063355bytesに固定。配備時・利用時の実バイトhash検査は親と既存queueのpin検査が担当する。guardは対象本文を開かない。

root配下の `deployment-receipt.json` は通常配備のbyte pinsの控え。これ自体も外部証跡に記録し、権限追加の根拠には使わない。外部証跡は秘密を含めず、許可済みsourceの通常証跡としてatomic保存する。guardは証跡を作成・更新しない。

## stateの範囲

`state`、ledger・lock、cache、run-attempt-job形式のentries/evidence、および実コードが生成する限定名だけ。詳細は `civ6Name`。既存directory/fileはそれぞれ外部証跡の物理IDが必要。新しい未作成leafの資格から既存unknownへ自動昇格しない。
ledgerのatomic replace、mutable stateの書換え後は、親の正規管理工程が新識別情報を保存するまで再guardは拒否する。queue自体へ動的な許可権限を追加しない。
evidenceの未知原名・深いコピー、runner展開、bootstrap/helperの実行物、任意入力、Linux用lockは資格対象外。実際のジョブ実行物は既存safe-delete所有資源とhelperの独立検査を維持する。
state内の固定admin script名はqueueの生成出力を分類するだけで、実行許可の追加ではない。

## 受入条件と確認範囲

- 未作成rootのみ成功。未作成子の一括許可と未知existingrootは失敗。
- 外部証跡とroot/親/sourceが一致すると既存root成功。不一致・欠落・偽形式は失敗。
- 正確な6leafと有限更新名、限定stateだけ成功。未知file、既存未記録、変更後file、hardlink、junction/reparse/link、他source、現行denyは失敗。
- 分類はsourceの証跡だけを読み、runtime/設定/state本文や通知設定・秘密・原名簿名を出さない。
- repair-guardのGo test/race/vet、別モデル固定差分レビュー、正式CI、通常merge、最新main由来の通常scrub391配布、正規guardの再実測を行う。
- runtime本体の作成/配備/起動とqueue設定の部分CAS接続は親2962に残す。本工程はrootやruntimeを作らない。

初期の既存guardでは依頼のrootはclassification_unverified/client_resourceともtrueだった。初回の拒否後、この担当は同rootを読取・作成・配備・起動していない。再実測も正規guardだけを使う。
