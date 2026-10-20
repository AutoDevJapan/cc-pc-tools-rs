# ToDo3107: 初回Windows検査資源の資格

親2962のPR149初回受入で正規safe-deleteが新規作成した1件を照合する。
[Issue292](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/292)とPRは本仕様、回帰検査、配布記録を対応付ける。原名簿の復旧ではない。

## 入力と成功条件

- 正確なrootは`owned3107Root`、所有IDは`4378ff571926b0ddbec4ed99368a6405`。
  作成記録の実時刻は2026-10-20 12:39:02.942647 UTC。
- 原safe-delete記録1件、元stageの固定source/実行物pin、公開結果、外部NTFS配備証拠、
  正規ledgerの保存バイト、属性だけの独立観測を、レビュー済みコードのSHA256で結ぶ。
  原記録は再生成しない。保存コピーと観測は専用worktreeの所有済み保管先に保持する。
  元stageの2証跡も同じ保管先の保存済みコピーを使い、元Tempの整理に依存しない。
  資源回収と資格失効までこのworktreeと所有済み保管先を保持する。
- 固定sourceは37d5359af2dcbbd047dea1f25dad75aa3d0a8f3a以降。
  sourceの実origin/物理パス/実GitHub所有者とID、既存runtimeの資格も要求する。
- 公開先の実所有者AutoDevJapan、repo ID1392152644、PR149、HEAD2bd3a366、
  run39423559387/attempt1/job118630300541、rust workflowの固定digestを照合する。
  公式APIではrunとjobの固定identityを確認し、queuedやfailureを成功へ読み替えない。
  runはattempt1の専用APIを使い、固定branchとPR149への対応も確認する。
  workflowパスは素の形と同じパスの`@refs/pull/149/merge`付きだけを認める。
- root/親のvolume・file ID・作成時刻・所有者は観測と一致必須。
  作成記録の時刻とNTFS作成時刻は異なる値として照合する。
- 正確なrootと、属性観測に実在した子だけを分類する。各対象とその祖先は
  実ID・作成時刻・所有者が一致し、通常fileはsize/write/change/link数1も一致必須。
  未作成・新しい子・交換・reparse・hardlink・現行denyは拒否する。
  観測側のreparseとrootより古い子も拒否する。資格の期限は2026-10-22 00:00 UTC。
  外部照合の後にも現在時刻を確認する。期限後は元ToDoから必要範囲を再評価する。

## 失敗と境界

不一致、欠落、読取不可、API失敗、期限切れは分類未確認を返す。
新しいCLI引数、汎用allowlist、登録・grant更新処理は追加しない。
今回の追加は旧2736、兄弟、全Temp、原製品folder、未知192件を分類しない。
既存3058/PR270や他の資格の判定は変更しない。

属性観測では3,598項目を確認し、hardlinkは0、reparseは1だった。
そのreparseとその配下は分類しない。root成功を全子の検証成功と表示しない。
通常ユーザーによる部分観測はACLで停止したため、既存管理者窓口で属性のみ再観測した。
ACLや所有者の変更、root本文の読取は実施していない。

## 検査と完了条件

先に正常な固定proofと不一致の受入検査を作り、Redを確認する。
Go race/vetはrepair-guard全体（既存資格との回帰を含む）に限定する。
別モデルmaxレビューのMustを解消し、同HEADの必要Windows正式CIと解析を確認して通常mergeする。
最新mainの並行変更を含む通常scrub391を配布し、hashと正規再guardを記録する。
無変更module、不要な他OSの移植・受入を追加の完了条件にしない。

サービス失敗のログ診断、credentials、停止、再collect、削除、実行、queue復旧は
親2962の後続工程。この担当は実施せず、失敗rootと原資料を保持する。
