# 終了した正規Windows検査資源の通常退避

元ToDo3250、親2962。AutoDevJapan/civ6-banseiki、repository ID1392152644。
初回の実受入は正規safe-delete所有ID bbdecd35aa4599f01cf61a3bfaa272b1。
PR149/head2bd3a366/run39423559387/attempt1/job118630300541、prestart-1に対応する。
旧4378の原物・閉じた所有記録・3174の限定資格は変更も再利用もしない。

## 障害と受入条件

正規source作成がsource/.gitを作る一方、通常の削除計画は.gitを保護する。
削除計画の保護を維持し、終了した新規検査資源だけを既存quarantineで同一volumeへ移す。
任意のrootや外部manifest、CLI grant、allowlist、成功status、再試行予算を追加しない。
秘密・原診断・reparseはopaqueに保持し、本文・リンク先・reparse payloadを読まずhashもしない。

正規guardは根だけを分類する。子やcredentialの読取資格は付与しない。
safe-deleteは通常分類の合算結果に加え、専用のci_retirement_qualified=trueを要求する。
別の資格による許可を、この退避資格へ流用しない。資格不成立時だけ従来の通常退避へ戻り、
Git・secret名・reparse・外部hardlinkなどの通常保護をすべて維持する。
実所有者とソース、外部NTFS配備receipt、現用queue/helperの実hash、元safe-delete record、
同じqueueの正式ledger、元のcollect-end、特権CollectedRoot、同SCMの停止/pid0、
当該instanceのGitHub登録不在、元run/jobの不変IDを照合する。
動的なledger/logだけは正規atomic更新でleaf IDが変わるため、旧leaf metadataを借用せず、
固定したruntime祖先と新規時刻・所有者・single-link・no-reparseを要求して限定して読む。
読み取りhandleはFILE_SHARE_DELETEを含め、正規writerのatomic置換を妨げない。
生のWin32属性handleには字句変換だけで拡張長パスを付け、移動後の長いパスも検証する。
元の除外一覧はguardのメモリ内で全項目名と照合し、本文や一致名を出力しない。

## 有限範囲

- 原初回3件、PR149正常merge直後のmain push3件だけ。continuation、他repo、未知資源は拒否。
- 初回はPR149の明示head/tree/branch、通常mainはPR149の実merge commitを要求する。
- 元queuedAtから作成まで24時間以内、作成から回収まで24時間以内、prestart deferralは0〜5。
- 新規作成は2026-10-21 00:25 UTC以降。rootのvolume/file ID/birthを原所有recordと照合する。
- 現用runtime sourceは263bb8d以降かつ正規sourceの祖先。実queue/helper hashを外部receiptと照合する。
- collectingまたはneeds_recoveryの終了資源だけ。登録・成功判定・queue書込は行わない。
- 全項目の所有者Administrators、同volume、最大10,000子、通常file合計16GiB。
- 通常fileのhardlinkは全aliasがこのroot内に収まる場合だけ保持する。外部alias、reparseやdirectoryの複数linkは拒否する。他targetの通常hardlink保護は不変。
- .gitの初回metadata取得例外は、正規Plan.source()が示すruntime/runner/_work/civ6-banseiki/civ6-banseiki/.gitだけ。通常の他保護名は拒否する。

## 通常操作

既存の `quarantine --id <原ID> --reason windows` だけで資格を構築する。
collector/controllerと同じ既存claim lockを保持し、移動前に全一覧を取得する。
各reparseはOPEN_REPARSE_POINTによる自身の属性だけを記録し、配下へ降りない。
退避先に原receipt bytes、全一覧、未完了理由を保存する。
移動直前に終了資格を再検査し、同じ全項目を照合してから通常Move-Itemを実行する。
移動後に全一覧とroot volume/file ID/birth、元root不在を照合し、原recordをquarantinedへ閉じる。
失敗時は原物と理由と両場所を保持し、再登録・強制削除・別経路での移動を行わない。
理由には実測していない削除拒否を記載しない。
同一ユーザーの任意の同時改変を防ぐ新しいセキュリティ境界ではない。

## 検査・反映

影響範囲はsafe-delete、repair-guardと新しく利用する既存契約ライブラリ。
実Redは未実装の資格関数によるcompile failure。Greenは有限資格の正負例、旧3174の
非追跡/全一覧保持/失敗時record保持、正常source Git保持、他保護名/size上限拒否を含む。
別モデルmaxレビュー、同HEADの必要正式Windows/Go解析後に通常mergeする。
配布前に並行guard変更を再確認し、通常配布の旧bytesを保持する。
実bbdeの通常退避・閉じた原record・移動前後一致を確認してから完了する。
親2962へsource/PR/merge/installed hashes/原ID/未実施をCASで配送する。
製品再供給、親ledger変更、GPU/Civ6/Cargo、新費用、owner質問は本担当では行わない。
