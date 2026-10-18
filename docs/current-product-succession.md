# 現在の継承先の受付（元2834、実装2851）

原保護名簿の復旧と、現在の本人指定による新しい継承先の受付を分ける。
原名簿が存在しない場合に限り、正式guardの読み取り専用API
`scrub391.exe --verify-current-succession <id>` を利用する。
原名簿を作成せず、旧担当や未知の項目の状態を変更しない。

## 資格の照合

Backendの`current_product_allowed(item, registered_root)`は既定false。
RealBackendは正式exeを40秒の期限で呼び、exit0のJSONだけを使う。
欠落、未対応、異常終了、不正JSONは不成立。応答本文はログへ出さない。

以下をすべて照合する。

- domainが`current-product-succession-v1`、verifiedがboolのtrue。
- origin_todo_idが2834、todo_idとprojectが現在の項目と一致。
- original_registry_recoveredがboolのfalse。
- 登録rootとspecのパス、および保存されたspec本文のSHA256が一致。

パスはbackslashをslashへ置換し、末尾slashを除去、小文字化したUTF-8を
ハッシュする。specは正式APIの成功後に読み、1MiBを超えるファイルと読取失敗を拒否する。
実体・Git所有者・現在の有限宣言の検証はGo guardが担当する。
Rust側も正式guardによるroot検査と通常のworktree検査を維持する。

## 通常の受付と巡回

dispatchのNotFound分岐だけに資格を追加する。既存のPC修復3projectと
元2407の条件を維持する。存在する名簿のdenyや読取不能を資格で上書きしない。
通常のprepare、停止、依存、資源、容量、担当競合、起動世代、作業場所の
照合を経る。準備と再測定の後にも資格を確認し、途中のspec/root変更や
名簿の復旧があれば今回の起動を延期する。

既存の有限なrepair-stepは資格を持つ現在の担当の結果を受け取り、依存が
完了した待機を通常受付へ返す。既存worktreeもguardで確認する。
旧質問の送信・返信処理や未知の担当の復旧へ処理を広げない。

現在の主担当が正規登録した直接のreview/check/researchの子も、Go APIが
その子自身のidとspecのfingerprintを返す場合だけ同じ比較を通る。Go guardは
固定済みの現在製品IDを親として照合し、親の現資格、親の保存jobsにある
origin_job_id、todo.exeが保存した直接subwork関係、子の新IDと保存spec、同じ
登録project/root、親worktreeのrepository/common-dir/top-level/branchを検証する。
origin_job_idは親のjobs履歴に結び付く。親は子を登録した後に依存待ちへ
入るため、最新jobとの一致は要求しない。子worktreeは親rootの標準`-wt`
配下で子IDとschedulerのtitleから作る正規名に限定する。schedulerのpathが
未記録ならこの名前を照合し、記録済みなら記録された同じworktreeだけを許可する。
titleが変わって予定名と記録済みpathが一致しなくなった場合は不成立にする。
slugは小文字ASCII英数字だけを残す。Go/Rustで小文字化結果が異なり、予定名が
一致しない場合は許可せず拒否する。
親root本体や同IDの別名worktreeは拒否する。予定pathが既に存在する場合も
guardでrepository/common-dir/branchと実体を確認する。存在するworktreeは
同じrepository/common-dirと`scheduler/todo-<子ID>-` branchを確認する。
子specの保存ファイル名にある乱数はファイル識別子で、本文hashではありません。
本文hashは読み取った実バイトから作り、Rust側で同じspecバイトと照合します。
失敗は`{"verified":false}`だけを返す。親の資格・停止・依存・通常の
subwork審査や独立worktree条件はRust側でも維持する。

## 検査と正式更新

`cc-scheduler`の回帰検査と、公開Backendを使う`cc-todo`、`cc-shepherd`、
`localai-web-rs`の関連検査、対象ライブラリのdoctestとClippyを選ぶ。
RealBackendの検査は正式APIの入力、期限、全field、spec本文の変更、欠落・
過大spec、保護root、旧番号・別projectを照合する。
dispatchの検査は停止・未完依存・資源・既存担当・存在するdeny・読取不能・
準備中の変更を含める。次のrepair-stepで重複起動せず結果を受領することも確認する。

別モデルの固定base/headレビューと公式CIの成功後に通常mergeする。
Go側の部分配布と現mainの並行変更を照合し、最新mainからschedulerだけを
生成・正式更新する。AO2839の実job/worktree/成果と次の自然巡回を確認する。
単体検査、CLI分類、受付、実操作、配布は別の結果として記録する。
