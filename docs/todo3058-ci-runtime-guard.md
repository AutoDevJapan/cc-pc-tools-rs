# ToDo3058: 万世紀CIの配備先を限定して照合する

親2962の独立前提を修復する。原保護名簿の復旧2467、旧192件、顧客、未知source、Tools全体へ対象を広げない。既存3011/PR252の3runtimeは変更しない。親のcoreコードと3workflowも変更しない。

## 正本と所有根拠

`repair-guard/civ6-ci-2962-contract.json` がビルドへ埋め込まれる正本。
実remote/APIで確認したAutoDevJapan/cc-pc-tools-rs（ID1406818713）の固定sourceだけを使う。
親の固定sourceパス、下限commit ab45cb5d221077fa65484c706b3353cdc4867c07、その子孫である現在HEAD、origin、物理一致、実GitHub所有者とIDを照合する。
sourceの作業コピーや配備先の設定から対象パスを追加しない。
Gitの環境変数による別repoへの差替えを除外する。運用中は親の固定sourceと外部証跡を保持し、このworktreeを使い捨てとして整理しない。

## 作成と既存資源の区別

1. 親が自分の固定sourceの別モデルレビューと正式CI成功を確認した後、正規guardでrootを照合する。正本の作成下限2026-10-20 05:50 UTC以降、未作成かつ外部証跡も無い場合だけrootが分類される。子や他のroot、配備後に消失したrootの再作成は分類されない。
2. 親は排他を取り、rootをexclusive mkdirする。既に存在した場合は初回作成として扱わず停止する。失敗したmkdirの後から既存rootを採用しない。
3. 親は直後に属性専用handleで取得したrootと親のNTFS識別情報・所有者を、固定sourceの `docs/civ6-ci-2962-deployment.private.json` に保存する。root配下の自己申告receiptだけでは既存rootを分類しない。
4. 以降はこの外部証跡が必須。domainと埋込contractの実バイトSHA256が一致し、証跡のSourceHeadが下限commit以降かつ現在HEADの祖先であることを要求する。物理パスと所有者、volume/file ID/作成時刻が一致したときだけ既存rootを分類する。
5. 正確な未作成leafは、直近の親が既に証跡で分類される場合だけ作成対象になる。親はexclusive createで配備し、通常の上書きをしない。作成後に各objectの識別情報を外部証跡へ保存する。記録していない既存objectは拒否される。
6. 更新は `civ6-ci-{queue,windows,route}.exe.pr<1〜999999>.{tmp,backup}` と、同形式のqueue JSON・配備receiptだけ。親は更新先と現用の識別情報・実バイトpinを照合してatomic replaceし、置換後の新IDを保存する。勝手に未知existingをbackup名へ改名して採用しない。

親のレビュー・CI成功は親の配備工程で確認する必須条件であり、guardの成功だけでレビュー済み・CI済み・配備可能と判定しない。
rootの実作成時刻は正本の下限以降、各objectの実作成時刻はroot以降を要求する。root・object・外部証跡の所有者は、正本に固定した本人SIDを要求する。親directoryの所有者は証跡との一致を確認し、親とrootの所有者が同一であることには依存しない。証跡の自己申告だけでは、下限より前のrootや別所有者を昇格できない。時刻は属性から取得し、証跡の値だけでは判定しない。

`mkdir`直後・証跡保存前に停止した場合は、そのrootを自動採用しない。親が保存済みのexclusive-create結果を残し、属性だけの照合と空rootの回収に必要な限定資格を別途固定レビューで取得する。本guardにunknownrootの削除・自動再作成・代替pathの許可は追加しない。作成処理を無条件に繰り返さない。

## 証跡の形式

外部証跡は `civ6Deployment` の厳格なJSON。重複キー、大文字小文字違いのalias、未知field、末尾値、4MiB超、リンクは拒否する。

- `Domain`: `civ6-ci-2962-deployment-v1`
- `ContractSHA256`: contractのファイルバイトのSHA256（Git blobの改行と作業コピーの改行を混同しない）
- `SourceHead`: 配備時の親sourceの実HEAD。現在HEADへの祖先関係も実Gitで確認する。
- `Parent`, `Root`: `Volume`, `ID`, `Created`, `Owner`。directoryのSize/Written/Changedは照合しない。
- `Objects`: rootからの相対パス（`/`区切り）→属性記録。通常fileは `Volume`, `ID`, `Created`, `Owner`, `Size`, `Written`, `Changed` をすべて照合し、link数1を要求する。
- root直下fileには reviewed byte pinを記録した `SHA256` が必須。公式zipは証跡のdigest文字列と104063355bytesが正本に一致することを照合する。guardは実内容のSHA256を検証しない。配備時・利用時の実バイトhash検査は親と既存queueのpin検査が担当する。guardは対象本文を開かない。

root配下の `deployment-receipt.json` は通常配備のbyte pinsの控え。これ自体も外部証跡に記録し、権限追加の根拠には使わない。外部証跡は秘密を含めず、許可済みsourceの通常証跡としてatomic保存する。guardは証跡を作成・更新しない。

## stateの範囲

`state`、ledger・lock、cache、run-attempt-job形式のentries/evidence、および実コードが生成する限定名だけ。詳細は `civ6Name`。既存directory/fileはそれぞれ外部証跡の物理IDが必要。新しい未作成leafの資格から既存unknownへ自動昇格しない。
ledgerのatomic replace、mutable stateの書換え後は、親の正規管理工程が新識別情報を保存するまで再guardは拒否する。queue自体へ動的な許可権限を追加しない。
queueは自分が生成するstateの書込ごとにscrub391を呼ばないため、上記はqueueのtickを停止させる条件ではない。stateの再調査・修復を行う担当への分類条件である。名前と親directoryの所有者だけで既存fileを許す変更は、依頼のunknownexisting拒否を破るため採用しない。
evidenceの未知原名・深いコピー、runner展開、bootstrap/helperの実行物、任意入力、Linux用lockは資格対象外。実際のジョブ実行物は既存safe-delete所有資源とhelperの独立検査を維持する。
state内の固定admin script名はqueueの生成出力を分類するだけで、実行許可の追加ではない。

## 受入条件と確認範囲

- 未作成rootのみ成功。未作成子の一括許可と未知existingrootは失敗。
- 外部証跡とroot/親/sourceが一致すると既存root成功。不一致・欠落・偽形式は失敗。
- 正確な6leafと有限更新名、限定stateだけ成功。未知file、既存未記録、変更後file、hardlink、junction/reparse/link、他source、現行denyは失敗。
- この追加分はruntime/設定/stateの本文を読まず、固定sourceの配備証跡と属性を照合する。既存のlive deny、Git origin、所有者APIの確認は維持し、秘密値や原名簿名を出さない。
- repair-guardのGo test/race/vet、別モデル固定差分レビュー、正式CI、通常merge、最新main由来の通常scrub391配布、正規guardの再実測を行う。
- runtime本体の作成/配備/起動とqueue設定の部分CAS接続は親2962に残す。本工程はrootやruntimeを作らない。

初期の既存guardでは依頼のrootはclassification_unverified/client_resourceともtrueだった。初回の拒否後、この担当は同rootを読取・作成・配備・起動していない。再実測も正規guardだけを使う。
