# マージ後CIの修復状況

ToDo2757。元2639/独立確認2752の状態・停止・依存を保持する。
開始時branchはscheduler/todo-2757-pr-ci、開始HEADはd6a46b0。
修復source repository1406818713のAutoDevJapan/private/admin/nonforkをAPI確認した。
原資料と他担当の資源は変更しない。

## 確認と今回の変更

2026-10-17、対象1285649124/PR1002のclosed/merged、head20c441、
merge/current main41090d19329b81dd1bd076f3336d723823be4b82、
tree007b84082008ea9effd5052f63856273aabbfb17、順序付き両親を再確認した。
CodeQL38941323954/workflow331939928/attempt1は4ジョブqueued。
promotion38941324877はfailure。前sourceの成功を転記しない。

tools/product-ciだけに、この実merged receiptと元の4ジョブを固定する
専用の計画・実行経路を追加した。元のopen PR検証と4候補の固定値は維持。
新経路も共通排他・正式runner・hook・実HEAD/tree・認証除去・回収を使う。
WindowsのGo race/unit/vetは成功。実jobは未起動、未配布版で実行しない。

## 2026-10-17 08:02 JSTの再開確認

補助側のPR190は通常merge66324ea080c33d620d506075d54f439949f14baaを経て
正式配布済み。実行ファイルとLinux helperのSHA256は保存receiptと一致。
これは製品41090のCI成功や本番反映ではない。

同じ公式ゲートに構造化argvで、元と同じGETを送った実結果は以下。
metadata、PR1002、固定main参照はHTTP200。mainの実SHAは41090と一致。
PRはclosed/merged、head20c、merge41090、base main、同repoを実確認した。
続く元PRの作業ブランチ参照はHTTP404。認証scopeはrepoを含む。
404だけでは、branch削除・URI解釈・個別のアクセス権のいずれかは確定できない。
別ref・別API・別認証は試していない。製品job・runner・本番変更は0。

前回の「main参照NOT_FOUND」という段階の帰属は今回の実測と異なる。
元のresume.jsonと履歴は変更せず保持している。今回のHTTP status、request ID、
bytes/SHA256は.ci-work/todo2757-fixed-ref-diagnosis.private.jsonに保存。
生の本文・stderr・識別名は保存せずRAM内で処理した。

配布済みhelperは段階を消して同じエラーを返すため、失敗を誤帰属しうる。
今回の補助側修正では、段階と固定分類だけを返す。受付条件、参照、候補、
retry件数、排他・回収は変えない。404後の別経路実行や成功扱いはない。
追加した失敗段階・後続読取停止・秘匿の回帰検査を含むGo race/unitとvetは成功。
新しい差分は別モデル確認・正規CI・通常merge・正式配布がまだ必要。

## 残る工程

1. 診断修正の独立Sonnetレビュー、Must修正、必要CI、通常PR/merge、正式配布。
   元branch参照404の未確定原因も保持し、受付条件を緩めて実行しない。
2. 正式配布から同CodeQL4ジョブを順次実行し、公式結果・原log指紋・回収を確認。
3. 固定sourceのrelease safety gate/DEPLOY/smoke/trust正典を調べ、
   Hosted固定を既存Linux基盤で直す有限経路を確定する。
4. 必要なら41090の子だけを通常PRで修復し、独立レビュー・trust更新・CIを行う。
5. 元preconditions/full verification/backend integration、既存Prd revision/probeを実証。
6. 自分の実結果を2752へ配送。原2639/2752/他製品をdoneにしない。

GPU/Civ6/本人PC操作/Terminal/UAC/APIキー/新費用/Hosted予算増額は禁止。
対象の名称・生source・生log・秘密をstdout/会話/ファイル名へ出さない。
製品sourceはAPIのRAMと正式有限containerのみ。旧製品folder/guard/registry/
owner-work/他担当資源は触らない。拒否の別経路実行は禁止。

一時検証用所有ID: f037fae3ce9199d172d830d3935fb8f0。
自分のcontainer/runner/Chrome/Prd変更はまだ0。
