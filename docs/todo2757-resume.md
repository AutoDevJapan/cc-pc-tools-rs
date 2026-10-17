# マージ後CIの修復状況

最新到達点（2026-10-17 11:10 JST）: PR196の正式配布後、元merge41090の
公式CodeQL run38941323954/attempt1はPython・JS/TS・Actions・Rustの全4jobで実success。
全4sessionの実HEAD/tree、PID0、container回収、runner不在、lock解放を確認済み。
構造化原結果は.ci-work/todo2757-original-postmerge-codeql-final.private.json。
公式job原logは3件保存済み。Rust原logはCLIの端末制御文字保護による拒否で未取得。
保護解除flagや別経路は使わない。これを解析失敗や原log取得成功と取り違えない。
次の実装境界はtodo2757-release-executor-plan.md。1文書だけのHaikuレビュー2780を登録。
配備CIの実装・release/既存Prd revision/probeはまだ未完。以下の旧記録は履歴として保持する。

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

2026-10-17 10:50 JST追記: PR196はSonnet2777の固定4差分/4全文読了・Must0、
必要CI success8/neutral1/skipped1後に司令塔が通常merge cdd0537d79fc4e1601c53feba6ed48eccd13f3d5。
原応答SHA39D5357C181F8DE1B114D5F88C07BFBD27B84E23894383BC0BF14A518914BC51を
最終eventと照合して受領した。未読の周辺配線は親が元と同じproductPlan/前後照合を
sourceで確認し、open PRのbranch変更拒否は既存main_test.goでも維持されている。
module tree11edf6471ecdf766a2d444c3b114dd736d890dfdは検証headとmergeで一致。
Windows/Linuxの正式配布を完了し、同sourceでmerged-inspectの固定receipt検証を通過。

専用queueには別run38964598433が同41090/同workflow/attempt1の4jobで待機していた。
同repo/private/admin/nonfork、4jobすべてqueued/steps0/runner0、同専用labelを再照合し、
この重複だけを通常cancelへ1回送信。元run38941323954は維持し、取消し反映後の
正式merged-inspectはexit0。queue条件やlabel・runner受付は変更していない。

元公式run38941323954/attempt1のPython job117109408190は実success。
正式runner sessionもexit0、実HEAD41090/tree007b照合true、isolation確認、
停止時PID0、container回収、元runner登録不在、lock解放を確認。
公式原log171163bytes/SHA256 EF7E48CC1FF2F6A553EF31CE0436FF37AD14A2082138E9F0C50CB6EB2BABAF1Dを
表示せず保存した。原runner receipt/checkout/context/logのcopy全hash一致。
残3言語はまだ成功扱いにせず、同じ正式toolで次の1jobを実行中。

固定workflow3件のYAML構造をRAM解析し、全blob/bytes/SHA256を再照合した。
promotionはpreconditions→reusable2本→promotionの4job。full verificationは31step、
backend integrationは12stepで、後者の6stepにcompose操作がある。
条件式・環境値はopaque hashで保持しており、意味全文を確認したとは扱わない。
この実構造を基に隔離と元検査を維持する有限設計をSonnet2779へ登録した。
同sourceの3言語終了・release実装修復・元業務gate・Prd revision/probeは未完。

2026-10-17 10:04 JST追記: 診断PR194はSonnet2771の全7差分読了・Must0、
親のPR差分同一照合、CI success8/neutral1/skipped1を経て通常merge5c41e78。
同一module tree f5b510d39eb58aff51cb7ae53568725d2bba1fcbからWindows/Linuxを
ビルドし正式配布した。Windowsの原子的置換に空のbackup引数を渡した初回だけ
ArgumentExceptionで失敗。実hashを再照合し、backup先を指定した同じ置換操作で
復旧して成功した。旧exeとreceiptを保持。審査拒否や別経路の実行ではない。
配布後のmerged-inspectは元PR branch ref/not-foundで停止。製品jobは未起動。

同じ元branch・同APIのスラッシュ単位符号化による有限な要求形式診断も404。
別ref/API/認証は使用せず、URIが原因という実証は得られなかった。
個別のbranch削除や権限の原因は依然未確定。delete_branch_on_merge=trueは
観測済みだが、その値だけを個別branch削除の証拠とは扱わない。

次の正式修復は、merged専用検証が作業branchの存続に依存する点を直すもの。
実行sourceは固定mergeであり、作業branchは実行しない。元PR receiptの
head20c/base f3d/merge41090、両commitの実SHA/tree、順序付きparents、main41090を
すべて必須とする。元PRのbase SHA照合を追加し、変動しうる元branchのGETは
merged専用からだけ除く。404を捕捉して別refへ進む処理は追加しない。
通常open PRのbranch照合、他候補、runner/queue/資源/回収は維持する。
意図した専用検証の変更なので、診断PR194のレビューを流用せず、別PRで
全差分の独立レビュー・必要CI・通常merge・正式配布を行う。未配布版は実行しない。
Go race/unit/vetは最新の専用修復で成功。正式CodeQL・release・Prdは未完。

1. 配備CIの有限実行計画2780を受領し、実装前の固定operation/compose/build/SDK/
   token/drain/trust/native/Cargoの未確認データを埋め、正式な4役割の実行経路を実装する。
2. 元公式CodeQLのRust原log取得拒否を保持する。保護解除や別取得経路へ切り替えない。
3. 固定sourceのrelease safety gate/DEPLOY/smoke/trust正典を正確に確認し、
   Hosted固定を既存Linux基盤で直す有限経路を通常review/CI/merge/配布する。
4. 必要なら41090の子だけを通常PRで修復し、独立レビュー・trust更新・CIを行う。
5. 元preconditions/full verification/backend integration、既存Prd revision/probeを実証。
6. 自分の実結果を2752へ配送。原2639/2752/他製品をdoneにしない。

GPU/Civ6/本人PC操作/Terminal/UAC/APIキー/新費用/Hosted予算増額は禁止。
対象の名称・生source・生log・秘密をstdout/会話/ファイル名へ出さない。
製品sourceはAPIのRAMと正式有限containerのみ。旧製品folder/guard/registry/
owner-work/他担当資源は触らない。拒否の別経路実行は禁止。

一時検証用所有ID: f037fae3ce9199d172d830d3935fb8f0。
自分のcontainer/runner/Chrome/Prd変更はまだ0。
