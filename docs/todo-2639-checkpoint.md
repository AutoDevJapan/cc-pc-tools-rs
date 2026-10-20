# ToDo2639 再開記録（2026-10-17）

監視日時の破損で重大アラートまで消える不具合の修正を、固定PR1002で継続する。原1636/2016/2274の停止・依存・未完了は変更していない。この独立工程を原製品全体の完了にしない。

## 今回の実確認

- GitHub APIのrepository ID 1285649124でAutoDevJapan所有・private・admin・nonforkを確認。名前はAPI経路のメモリ内だけで使用する。
- PR1002はopen/draft=true。head=b93e126b172adbb29ad2081af4732f4b42f8bc95、base=f3d946cb0f7e33b8cb5cbc0858091e583ca61350。mergeable=true/clean。5ファイル差分。
- 同headのcheck-runsを実読戻しし、下記6件すべてcompleted/successを確認。チェックの再実行・dispatch・手動success作成は0。

| check ID | 名前 | 実結果 |
| --- | --- | --- |
| 116905217657 | CodeQL | success |
| 116903806365 | test | success |
| 116903790733 | Analyze (rust) | success |
| 116903790683 | Analyze (actions) | success |
| 116903790615 | Analyze (python) | success |
| 116903790529 | Analyze (javascript-typescript) | success |

原956でのlocked Ruff/format/mypy220files/backend2236passed、および後日の3結合検査成功は元2648からの配送記録として保持する。b93でbackend全件を再実行した結果ではない。b93と956のアプリ部分のtree一致、b93のNode185passed、公式CodeQL/secret成功の原記録は指定された操作証拠保管先にある。生ログを再表示しない。

AGENTS.md/CLAUDE.md/.github/CODEOWNERSを固定GitHub sourceから読んだ。配備先の制約、既存required context、release safety gateの区別、日本語のGitHub文章、自動生成ブロックの保持を引き継ぐ。顧客固有の人間レビュー必須条件とrelease gateの扱いもレビュー工程で再確認する。

## 独立レビューの登録

元App差分と追加CI差分に対する別モデルレビューは未完了だったため、重複工程がないことを確認し、共有ToDo2735を登録した。モデルはclaude-sonnet-5-5、種別review、元2639/今回のjobに紐付けた。登録後、保存specの存在・dispatchable=true・親2639へのsubwork記録を実読戻しした。登録をレビュー成功と扱わない。子Codexジョブを直接起動していない。

登録した指示は docs/todo-2639-review-spec.md。対象は固定baseからb93への5ファイル。GitHub API読取だけを認め、source/PR/CI/配備の変更や重い検査は認めない。結果は子worktreeの docs/todo-2639-review-result.md とその実commitで返す。

## 次の一手

1. ToDo2735の完了後、schedulerが記録するそのworktreeで結果ファイルと実commit・対象head/base/treeを読む。別モデルの実レビューであることを確認し、Mustを修正する。単なる受付・起動・ファイル存在を合格としない。
2. 修正が必要ならGitHubの同PR/同branchで通常のcommit/pushを行う。新しいheadへ固定値を更新する必要があれば元証拠と分けて保存し、影響する検査・別モデルレビューを確認する。現在の固定値を黙って追従しない。
3. Must0・顧客固有規則・必要CI・release gateを確認した後、draft解除と固定headを指定した通常マージへ進む。元PR173/174/183/185/187や完了CIを再実行しない。
4. マージ後の実source/必要検査と既存Prdへの反映を、実結果と未実行に分けて追う。新費用、別Org、非Prd配備、旧製品フォルダ、保護設定の操作は範囲外。

前回は `todo.exe wait 2639 --on 2735` を記録して枠を譲った。以下が再開後の最新状態。

## 最新状態：追加修正を保存済み、レビューと正式CIは未完了

2026-10-17の再開job20261017-005528-todo-2639で、Sonnet2735の原応答を受領した。jobのmodel=anthropic/claude-sonnet-5-5・succeeded/exit0を確認。原応答SHA256は24618823b5e1a8b5617c4643587da4cf95ebe5524d98275155bf7a7394ce0ec6。Must0。readonly担当のファイル保存が失敗していたため、親が原応答をhash付きで保存した。原5ファイルのレビューはやり直さない。

Suggestionの実害と必要性を確認し、次の4ファイルを修正した。

- アラート表の途中に入った説明段落を表の後へ移し、日時破損の復旧手順を加えた。
- PostgreSQLの実heartbeatを使い、UTC/Asia-Kolkata/America-New-Yorkでtext変換後の正常性・日時・age・critical誤報なしを確認する回帰testを加えた。schemaはランダムに分離し終了時に削除する。
- required testの専用実行機、gitleaksの版/SHA、固定対象の拒否条件、queued時の復旧と回収をrunbookに記した。
- CLAUDE.mdの「checkoutしない」という古い記述を実workflowに合わせた。自動生成stackブロックは変更していない。

新固定headは **20c441df545a88f4a839d7e906b5aeb4f1708ae3**、treeは **007b84082008ea9effd5052f63856273aabbfb17**。唯一の親は旧b93。同PR1002・同branchにforce=falseで1回だけref更新した。API即時PR反映は遅れたが、read-only再照合でPRhead/branchheadが両方20cと一致した。refのmutation再試行は0。compare APIでも4ファイル・1commitだけと確認。backend/app・workflow・scriptsのbytesはb93から変更していない。他head/merge-refは追わない。

### 今回実行した検査

固定b93のGitHub archiveへ新4ファイルだけを重ねた検証sourceで、locked依存・Ruff check/format・mypy・元監視testと新PostgreSQL testの **合計50passed/skip0** を確認した。pytest4.82秒。テスト済みの各ファイルSHA256をreceiptへ固定し、GitHubへのupload blobのGit SHA1とも照合した。これは新headの正式CIの成功ではない。

uvとPostgreSQLは元2648と同じimmutable imageを使い、非root・readonly・tmpfs・capdrop・CPU/RAM/pids制限、hostsource/profile/socket/GPUなしで実行した。依存取得後の外向通信を切り、同namespaceの独立PostgreSQLへTCPで接続した。終了後、両containerのrunning=false/PID0とrm -v成功を確認。今回新たにimageやvolumeは作っていない。

原log2857bytes、SHA256 f3740324680cf9f7bf79e99fb332d34cbd3193f80b21f1cda3e15a958821f570。原review、acceptance receipt/log、commit proposal、Go操作補助の5ファイルを、このworktreeの `.ci-work/todo2639-evidence-20261017/` へコピーして全SHA一致を確認。manifest.private.jsonで一覧を保持する。Gitのignore対象であり、製品コードや生ログをこの修復sourceのcommitへ入れていない。

所有tempはC:/Users/makhmeto/AppData/Local/Temp/codex-owned-d84c967c6eb3b9abf993ad85a20fb9b9、所有ID=d84c967c6eb3b9abf993ad85a20fb9b9。まだ後続担当が読むため保持する。後続完了後に必要証拠の保持を確認し、safe-deleteのplan→別呼出し通常削除→finishで片付ける。拒否は正規quarantineへ進め、迂回しない。

### 設定の実読戻し

active repository ruleset18884840はrequired test/App15368、approval0/codeowner必須false/thread解決必須。Actionsはselected/SHA pin必須。fork workflow実行・write token・secret送付はfalse。顧客固有の人間レビュー必須規則を新しく発見していない。保護設定・権限・予算は変更0。

### 次の依存と再開手順

- **ToDo2743**：追加4ファイルのSonnetレビュー。specはdocs/todo-2639-review-followup-spec.md。原5ファイルは再レビューしない。readonly環境でのファイル保存は要求せず、実モデルの原応答とhashを親が受け取る。
- **ToDo2744**：新20c固定の正式CI。specはdocs/todo-2639-ci-followup-spec.md。旧b93固定のtoolが新20cを拒否するのは正常。正規の修復sourceで候補SHA1行だけを更新し、他候補と拒否条件を保ち、別モデルレビュー・必要CI・通常merge・正式配布後に既存queuedの公式CodeQL4言語/secretを直列で実行する。必要なCargoはこの子へ割当申請し、親は重複実行しない。

新headの公式checkは登録直後の実GETでは5件queued。test=117068929592、Actions=117068906037、Rust=117068905828、Python=117068905768、JS/TS=117068905439。旧b93の6成功を新headの合格にしない。元toolの固定値・guardをこの親では書き換えない。

PR1002の題名と本文を日本語で現在の変更・検査済み範囲・未完了条件へ更新し、API読戻しで本文の完全一致・新head・draft=trueを確認した。同じ説明更新を繰り返さない。新規Discord質問・投稿は0。

親は `todo.exe wait 2639 --on 2743 --on 2744 --progress 50` で正式に依存待ちにする。独立レビューと新正式CIが揃ったら、実head/tree/結果を確認しMustを修正した後に通常PRへの切替と固定headでのマージ・既存Prd反映を進める。本人の判断・PC操作は不要。原1636/2016/2274・他3製品は完了扱いにしていない。

## 最新状態：PR1002はマージ済み、Prd反映は開始前のCI失敗で未完了

2026-10-17 02:39 JST、job20261017-023228-todo-2639で元PR1002を通常mergeした。固定PRhead20c441df545a88f4a839d7e906b5aeb4f1708ae3を--match-head-commitで指定し、auto/admin/bypassを使っていない。draft解除・merge各1回。API読戻しでclosed/merged=trueとmerge/main **41090d19329b81dd1bd076f3336d723823be4b82** を確認した。mergeの両親は原basef3d946cbと20c、treeは検査・レビュー対象と同一の **007b84082008ea9effd5052f63856273aabbfb17**。このmergeを再実行しない。

### レビューとマージ前の実確認

Sonnet2743の原応答を受領。model=anthropic/claude-sonnet-5-5、succeeded/exit0、complete_diff_read=true、Must0。原応答SHA256はc4ae16bb34d53b502e085d04644447472b58ab86b9e58630783bc8cc83ff48f1。原文とjob/model/exit/hashを `.ci-work/todo2639-evidence-20261017/review2743-*` に保存した。

Suggestionは実装と照合して仕分けた。snapshotはcommit/rollback/close/beginを呼ばず、同transactionでString castを使い、modelの日時列はDateTime(timezone=True)。今回の接続設定付き受入はskip0であり、opt-in未実行を成功と扱っていない。これらの静的事実から、timezone経路の未読による懸念をMustとは扱わない。Prdの実DDL全体を今回検査したとは主張しない。DB種別ごとの詳しい復旧、実行機の操作例、配布archiveとELFのhashの区別等は文書の改善点として保持し、現時点のruntime不具合や正式CIの失敗原因とは混同しない。正常なPrdデータを推測で修正しない旨をPR本文にも記録した。

元2744の実結果とGitHub APIを照合し、20cの全6check（CodeQL4言語/集約/通常test）completed/successを確認した。Nodeの同source/tree/exit0/原logSHA256 dd194bbda8821df2e80944f87e41f87c1470e1e22c54a0bfb5f28122c77ea256も実ファイルと一致。Node185pass/skip0のcontainer回収・tree消滅をreceiptで確認した。正式toolは修復sourceのPR188/mergefa594cecから配布済みであり、未配布toolの実行ではない。

マージ直前に固定sourceのgithub-required-check-policy.mjsを、既存immutable Node imageの非root/readonly/network-none/CPU-RAM制限containerで実行し、**GitHub security boundary verified** を確認。実APIから取得したruleset/App/check/policyを使い、保護設定の変更は0。検証containerはPID0とrm -vを確認。監査receiptはsecurity-audit.private.json。GraphQLのquery引数はWindowsのcommand-line errorになったため、同じ正式CLIでJSON stdinを使って読取を完了した。これは審査拒否ではない。正しいGraphQL応答で未解決review thread0を確認した。

### マージ後の実失敗と残作業

Prdの自動promotionはrun **38941324877** / workflow306106154でfailure。preconditions check117109404912はsteps0で、実注釈はActions budgetによる開始拒否。verify-backend-integration117109423278、verify側117109423311、promote117109424787はskipped/steps0。実際の業務precondition・release gate・Prd配備はまだ実行されていない。失敗を「無関係」として放置せず、元工程の残作業として追う。

merge41090のCodeQL run **38941323954** / workflow331939928は最初のGETでqueued。Python117109408190、JS117109408508、Rust117109408625、Actions117109408843。20cと同treeでも、この公式runをsuccessと書き換えない。特定repository変数DEPLOY_ENABLEDは実GETでtrue。変更0、他変数/secret値の取得0。最初のGitHub deployment GETは0件であり、Vercel/RailwayのPrd不在とは断定していない。

これを完了させる有限な独立工程を **ToDo2752** に登録した。specはdocs/todo-2639-postmerge-delivery-spec.md。親2639とのsubwork・保存spec・dispatchable・needs cargoを読戻し確認。子Codexジョブを直接起動していない。既存資源で必要な正式CI/release gate/既存Prdの実revisionと非破壊probeを確認する。新費用・予算増額・保護設定変更・秘密値の取得・汎用実行機・手動successは認めない。旧toolのopen-PR固定を直接変更して回避せず、機能が足りなければ有限なmerged receiptに束縛した通常の実装修復・別モデルレビュー・必要CI・merge・正式配布を経る。

### 後片付けと次の再開

終了済み所有temp d84c967c6eb3b9abf993ad85a20fb9b9は、証拠5ファイルの保存先との全SHA一致と製品4ファイルのcommit/mergeを確認後、safe-delete plan→別呼出し通常削除→finish source_absentで回収した。5,437,830bytes/27entries。元CのTempパスを今後の原資料の参照先にしない。`.ci-work/todo2639-evidence-20261017/` と元2744の恒久保管先を使う。後片付けのDiscord logは1回送信を試み、exit1の原応答をcleanup-discord-receipt.private.jsonへ保存。再送ループや新しい質問は作っていない。送信済みとは報告しない。

次回は2752の実結果を読み、現在の固定source/必要検査/Prdのrevisionとprobe/回収を照合する。登録だけでPrd成功・元2639完了としない。2752の完了を待つだけなので、親は正式な `todo.exe wait 2639 --on 2752` を記録して枠を譲る。本人の判断・PC操作は不要。原1636/2016/2274と他製品は未完了のまま保持する。

## 2026-10-20：この独立した修正工程の完了

元2752からマージ後CI・Prd反映の独立確認を受領した。原応答と実確認JSONの指紋を照合し、親でも正式CodeQL4言語と配備4ジョブの実success、更新対象8サービスのsuccess、APIのHTTP200/ready/対象revision一致を読み戻した。新たな配備や成功検査の再実行は0。この修正工程の受入は [完了記録](todo-2639-completion.md) のとおり揃っている。

今回更新されなかった旧サービスも調べた。build/deployの原ログを別々に取得し、起動前検査のRuntimeErrorを確認。workerとAPIそれぞれの必要設定について、管理CLIから値を出さない専用プログラムでキーの有無だけを確認した。公式API連携設定は両方とも未登録で、固定sourceでは未登録なら空の設定となり、API pollingのvalidatorが起動を拒否する。稼働中配備も0件である。過去の例外文字列全体や全原因を復元したとは主張しない。新しい店舗設定・資格情報を推測で追加せず、旧失敗履歴と元の全体タスクを維持する。

本番詳細は公開sourceへ増やさず、このworktreeのGit管理外 `.ci-work/todo2639-evidence-20261017/` に保存した。final-parent-verification-20261020.private.json、completion-manifest-20261020.private.json、独立確認原JSON、設定の有無だけの記録、旧ログ、確認プログラムsourceを保持する。元の全体タスク・他製品・保護記録を完了としない。

一時データは所有記録のplan→別呼出し通常削除→finish source_absentで回収した。今回のGo確認プログラムと実行物2件で4,073,400bytes。新規container/runner/Chrome/GPU/Cargo/Prd変更は0。通常の完了通知・メンションは送らない。共有ToDoの完了状態はschedulerへ返す最終結果で更新させる。
