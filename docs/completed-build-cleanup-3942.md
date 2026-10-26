# 完了したPCツール作業のビルド生成物を1時間清掃で回収する

元3942。既存Rust cleanup-8hを拡張する。新しい常駐・Windowsタスク・所有登録は作らない。
3757は2026-10-26 16:25にdone。旧jobの不要なwait再登録で依存世代が変わったため、
通常のcancel/再割当で復旧した。現在job20261026-164814-todo-3942とRootの世代2が一致。
完了済み3757へのwaitは再登録しない。
safe-deleteのソース・配布対象は本件で変更しない。

## 受入条件

- 通常の共有ToDoストアでproject=cc-pc-tools-rs/status=doneの登録worktreeを選ぶ。
  schedulerの元job、CWD、work_owner、実Git remoteの所有者・branch・commonを照合する。
  新しい任意pathの所有登録や、コピーした未知データの資格化は行わない。
- 最初の対象はこのPCの自社リポジトリのignored `target/debug`だけ。C/D内の絶対パスと
  worktree境界を検証し、祖先・子のreparse、外部hardlink、未保存ソースを保持する。
  source/Git/release、原receipt・検証記録、現用の実行物、他担当の領域を削除しない。
- queue/ToDoとCargoの既存OS lockを資格確認から削除まで保持する。原状態を再照合し、
  別の実行中・待機ジョブ、未完依存、保持・現用のファイルがあれば当該対象を保持する。
  profileのlockとそのディレクトリはCargoの並行開始を防ぐため残し、成果の実不在と区別する。
- default/--dry-runはファイル変更なし。--applyだけ既存ハンドル削除を使う。全体期限・件数・
  1回の対象数を制限し、新入口の失敗で従来の削除希望・ごみ箱処理を失わない。
- 候補論理bytes、実削除bytes、free before/after、実不在、保持・未測定・部分失敗を分ける。
  私的なファイル名や元仕様をログへ出さない。既削除8件の51,237,263,964Bを再加算しない。
  一部の走査終了を全PC清掃完了としない。同じ保持を毎回Discord送信しない。
- 境界、並行開始、元状態変化、未知guard、ソース・現用保持、実削除・部分失敗を回帰テストする。
  変更と利用側だけテストし、別の実モデルのレビュー、同headのCI後に通常PRをマージする。
- 元配布票/CAS/ACL/既存AdminBrokerでcleanupだけ反映する。fingerprintと既存1時間タスクの
  実発火・結果を照合する。不要な本件生成物を回収し、必要な証拠とソースを保持する。

## 実装・検証記録

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/545 。
変更範囲のWindows nextestは35件成功、Clippyも成功。実行中exe、別プロセスのCargo lock競合、
未知guard、未pushソース、原状態変化、junction/hardlink、置換・部分失敗を含む。
cleanup-8hはbinaryのみでdoctest対象なし。既存cc-jobs依存を追加したため、最終CIはロック変更の
通常選択規則に従って全workspace/doctestを1回実行する。PCでは同じ全件を重ねない。
元の管理caseと3757の原記録は変更しない。詳細な原結果は本担当の所有証拠フォルダーへ保存する。

初回レビュー3949は実job20261026-170920-todo-3949、native turn_context
01a17742-80f9-73e3-90e6-fae38ec11207のgpt-6-sol/max、62 steps、終了0。
固定a46543ec→0d90458aの7対象を読了し、未知の子の生成元確認・最終ルート不在・変化後残量の
Must3件を返した。原応答SHA256は8880b93c349b02875a4bcde4cf4566ac696888ef25aa978cf81c5f693b9fb1ce。
Cargo metadataとfingerprint照合、最終再列挙、変化時の残量未測定で修正した。保存された明示的な
ビルド出所参照も保持する。件数上限とcursorに関するSuggestionも、通常active snapshotの読取APIと
破損cursorの保持・先頭からの有限走査へ反映した。追加差分のレビューはこれから受ける。

配布には既存owned-cleanup-deployのCAS/ACL/no-replace処理を再利用する新世代を用意した。
対象はcleanup-8hだけ。safe-deleteのソース・配布byteは変更しない。
配布pinは初期0d90458a候補のままで、レビュー修正後のbuild/manifestへ差し替えるまでは実配布しない。
現用版更新・通常タスク発火・本実装による実物の回収はまだ0。

追加レビュー3954: 実job20261026-175927-todo-3954、native
01a17770-2289-7962-bf63-3760f67419fe、gpt-6-sol/max、50 steps、終了0。
固定0d90458a→89b9eeb2の9対象読了。原応答SHA256
9d53f29f76133e3130bac6ceeb49783ddcd0d812fb69df977eb44443e12cde2e。
fingerprintディレクトリと実unitの対応、終了状態でも生存する別job PID、後から現れる
profile lock名についてMust3件を受け、個別Cargo package/marker照合、全canonical metadataの
無変更・有限なPID照合、保持した通常lock実体の再確認へ修正した。相対build出所参照と、
破損cursor原本を保持しながら有限な位置を進めるSuggestionも反映。

自分のCargo出力で内部hardlinkを実測したため、全リンクが同じ資格済み計画内にある場合だけ
既存ハンドル削除を再利用する。外部リンクは保持、候補/実削除bytesは物理ファイルごとに1回。
初回・追加レビューの旧範囲を最新sourceの承認へ流用しない。追加修正は別途固定レビューする。
配布候補は既知の自分のRust出力を単一リンクへbyte一致コピーし、元出力と一致を配布票へ残す。
コピーを再ビルドや回収量に数えない。

レビュー3957は実job20261026-193008-todo-3957、native01a177c3-818e-77a2-8e49-e96f984ace34、
gpt-6-sol/max/62 steps/終了0。固定89b9eeb2→663834fcの9対象読了。
原応答SHA9774cec19d46127e020b6ad641622d7020fc8d628e5e6b854e48454bb97470b6。
直下/exampleの出力種類とmarkerの対応、先行する終了jobの相対出所参照のMust2件を受け、
実marker/kindからの出力名列挙と全登録jobの参照確認に修正した。
最初のCI548ではWindows全体3002件中2995成功/7失敗/9skip。7件は自分のfixtureで、
runnerのTemp短縮名とGitの長い名前の比較不一致が原因。新しく作ったfixtureだけを物理名へ
正規化し、製品側の境界確認は緩めず修正した。関係ない失敗として放置していない。
また実機の元3570の不明なjobを変更せず、通常公開済みwork claimとToDoのjob履歴で
その保持範囲を限定する。3570自身の終了や削除資格には使わず、独立の3757等を一律停止しない。

実機受入3960は通常schedulerの独立工程として、現在mainのcleanup-8hを通常Cargo gateで
1回checkする。実際の生成元・終了jobを持つ不要な再生成物を用意し、正常Root done後に既存1hタスクで
回収する。任意pathの所有登録・未知dataのコピー・Root/jobの手書きは行わない。
正常な担当が作る既存Rust生成物の実受入であり、新しい常駐や清掃taskの追加ではない。
