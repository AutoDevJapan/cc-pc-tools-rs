# 終了したWindows検査資源1件の退避

ToDo3174、親2962、[Issue322](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/322)。
対象は正規safe-deleteが新規作成したID4378ff571926b0ddbec4ed99368a6405だけ。
元PR149/head2bd3a366/run39423559387/attempt1/job118630300541に対応する。
旧原製品フォルダー、未知192件、顧客、兄弟root、queueの書込は対象外。

## 受入条件

元の所有記録、元stage/source、公開結果、ledger、独立NTFS観測、外部配備証拠を
保存済み原bytesのSHA256で照合する。元source37d5359から作成された資源であり、
正規collector bc152/PR291による回収の原結果も固定SHAで照合する。
GitHubの固定run/attempt/job APIで実repository ID、HEAD、branch、workflowを照合する。

collectorが正常に変更した所有者を古い3107資格のまま扱わない。
全SID grant除去、Job排出、owned process 0を確認済みの正規collect結果と、
管理者が保護するCollectedRoot、同じSCM image、停止状態/pid0を要求する。
既存のcontroller/collectorと同じclaim lockを移動前から記録終了まで保持する。
新規の所有登録、CLI grant、汎用allowlist、guardの緩和を作らない。
資格は2026-10-22 00:00 UTCまでで、閉じたreceiptは再利用しない。

レビュー3182はOpus5.5/maxで固定4a950を全4file読了し、Mustを1件指摘した。
回収後のowner変化で通常のclient guardが未確認へ戻るため、退避直前にも
`clientGuard(root)`を要求する。repair-guardには回収済みの同rootだけの資格を加える。
元6証拠、固定した回収結果と回収後属性一覧、同じ作成source/配備実体/公開API、
保護された回収marker、実root/親のID/owner/birthと現在root属性を再照合する。
子やreparse、その配下へ新しい分類を与えず、現在のdenyは維持する。

## 回収後の属性観測

2026-10-21の属性専用handleによる非追跡観測では3,626項目（根を含む）を確認した。
通常ファイルの合計は1,728,620,709 bytes。本文、認証情報、リンク先は未読。
元観測3,598項目の欠落、file ID変更、作成時刻変更は0だった。
元観測後の追加28件はroot直下15件とSDK bootstrap/bin直下13件だった。
以前のreparse1件に加えて12件が作成され、現在のreparseは13件、hardlinkは0。
すべての現在所有者が正規collect後のAdministratorsだった。

新しい全属性一覧の原bytesを担当所有保管先に保存しSHA256で固定した。
原一覧と現在一覧をsourceで結び、元全項目のID/volume/birthの保持も検査する。
この一覧と一致する全項目だけを移動する。新しい項目、交換、属性/時刻/size/ownerの
変化、未記録reparse、hardlink、件数超過は拒否する。
含まれていた元のGit管理ファイルも属性だけを保持し本文は開かない。
他対象のGit保護、祖先、reparse、size、件数、hardlinkの通常境界は変えない。

## 正規操作と保持の確認

既存の`quarantine --id <固定ID> --reason windows`だけが固定資格を構築する。
plan/finishは従来どおりで、planはreparseを拒否する。
既存quarantineの同一ドライブへの通常Move-Itemを使う。
移動前に新しい退避先へ原receipt bytesと全属性一覧、未完了の理由を書く。
移動直前と移動後に同じ全項目・root ID/volume/birth・opaque reparse自身を照合し、
元場所不在と退避先存在も確認した場合だけ原receiptをquarantinedへ閉じる。
原診断を含む全rootを保持し、リンク解除・target走査・別APIによる削除を行わない。

通常ファイルやreparse payloadの内容hashは取らない。同じNTFS実体とsize/属性/時刻を
保持する同一volumeのrenameで無損失を確認し、終了とclaim lockで既存writerを排除する。
同じユーザーによる任意の同時改変を防ぐ新しいセキュリティ境界とはしない。
移動後照合に失敗したら元receiptを閉じず、理由・両場所・原診断を保持する。
再移動・別経路削除・元recordの作り直しで成功扱いにしない。

## 検査と配布

Go race/vetはsafe-delete module（既存Gemini回帰を含む）に絞る。
架空のreparseで非追跡・属性自身の読取・別リンク拒否・hardlink/owner変更拒否・
固定一覧変更拒否・通常plan/quarantineの拒否継続を確認する。
実Move-Itemを通る成功・保持照合失敗・receipt更新失敗も架空の資源で検査する。
保持照合失敗で移動確認済みと表示せず、元receiptをownedのまま残す。
実対象は固定資格と属性走査のみを確認済みで、候補の実移動は未実施。
別モデルmaxのMust解消、同HEADの正式Windows safe-deleteとGo解析成功後に通常mergeし、
最新mainの並行変更を確認して通常配布する。現用の正規quarantine結果とclosed receiptを
確認するまで完了にしない。GPU/Civ6/Cargo、製品job再実行、新費用、owner質問は行わない。
source/PR/merge/配布hash/実操作結果/未実施を親2962へCASで配送する。
