# ToDo3411：PR393のWindows CIを有限に修復

親はToDo3392（job20261023-102927-todo-3392）。固定baseは `90dd0c09f34541183672f6e1afda0f74fd363b05`。作業branchは `scheduler/todo-3411-3392-ci`、担当jobは `20261023-114022-todo-3411`。
本工程の修復と確認を親へ返す。PR393の正式CIの成功・レビュー・統合・マージは未確認で、親が担当する。別PRとレビュー担当は作っていない。

## 原因と再現

[失敗したCI](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/39888787786/job/120203241992)は2336件中1件が失敗した。保存snapshotはqueued、agent0、wait理由なし、admission errorなし、index complete、120024ms。supervisorのPID/作成時刻の一致だけでは実行の進行を示さない。元のテストは単独では67.140秒で成功した。

原因として再現したのは、偽helperの失敗ログの時刻だった。productionはmetadata取得前の時刻を採り、未来の失敗ログを受け入れない。偽helperはログ照会に答えた時刻を失敗時刻として生成していた。照会が1秒以上遅れると、実際には終了済みの試行が「未来の失敗」となり、account attributionが未確認になる。その結果、`capacity_wait_pending=true`、900秒の期限を保存してqueuedへ戻る。supervisorは期限前の分岐で3秒ずつ待ち、次のadmissionもagent起動も行わない。これはOS/fixtureのロック待ちではなく、保存されたcooldownを守る分岐である。

同じ実supervisor・queue・偽CLIの経路で、偽のログ応答だけへ1500msの遅延を注入すると、3.163秒でこのqueued状態を再現してテストを失敗させた。元snapshotの全主要項目と一致し、追加診断ではpool verified=false、wait count1と900秒の期限も確認した。遅延は再現入力であり、成功を待つ固定sleepやtimeout延長ではない。回帰テストは誤ったcooldownへ入った時点で失敗し、900秒を待たない。

元CIには再試行回数・期限・pool状態・子supervisor.logが残っていないため、その履歴の全分岐を一意に証明したとはしない。一方、この時刻の誤りは最小経路でRedになり、修正後に同条件でGreenになった。shared hostの実queue/stateや本物のToDoロックは変更していない。外側のテスト環境のCC_TODO_HOMEも専用の空ディレクトリへ限定した。過去のCI PIDを現在のPCで操作していない。

## 変更とテスト

偽helperが試行時刻を記録し、後のログ照会でも同じ時刻を返すようにした。productionの未来/stale判定・候補・再試行上限は保つ。元テストのprompt/cwd/session保持と、同モデル3回・別モデル2回の切替後の停止も確認した。診断へretry回数・期限・pool状態・合成supervisor.logを追加し、元の容量テストも作成時刻を照合したOS handleで終了確認する。

その後のClippyで `capacity.rs:72` の既存nonminimal_boolが実際に失敗した。`!Some(nonempty)` を同値の `is_none_or(empty)` へ直した。null/非配列/空配列ではtrue、非空配列ではfalseを保つ。

| 確認 | ソース | 結果 |
|---|---|---|
| 元テスト、変更前 | base90 | PASS 67.140s |
| 1500ms遅延のRed | base90＋ea82da05と同じテスト差分 | FAIL 3.163s、exit100 |
| 遅延回帰＋元once/always契約 | 2a9abe59 | 2件PASS、10.924s / 68.161s |
| 関連parity/E2E/pool_retry/queue_lock | 2a9abe59 | 76件PASS、57.177s |
| Clippy初回 | 2a9abe59 | 既存条件式でFAIL、exit101 |
| 条件式修正後のcapacity/provenance | 43773d1c | 45件PASS、2.705s |
| fixture/parityのClippy -D warnings | 43773d1c | PASS |
| cc-jobs doctest | 43773d1c | PASS、対象0件 |

2a9abe59から43773d1cの変更は条件式の同値化だけで、fixture/parity/pool_retryの入力は同じ。正常結果を無意味に重複実行せず、追加変更の影響範囲を確認した。全workspace/release、実AI、GPU/Civ6は実行していない。通常Cargoゲート・sccache・専用targetを使用した。

cc-askとその依存ソース、Cargo.toml/Cargo.lock、共有設定の差分は0。親のcc-ask候補のコンパイル入力へ影響する変更はない。統合後のGit HEADや配布用receiptの版照合は親が通常手順で行う。

## 証拠と残り

全stdout/stderr、コマンド、HEAD、開始/終了、実exitは担当worktreeの `.ci-work/<label>.*` に保持し、8実行の一覧を `evidence-manifest.private.json` で読み返した。Red実行時のmetadata HEADはbase90で、当時の未コミット差分をea82da05に保存した。巨大ログはGitへ複写していない。

| stderrログまたは原資料 | SHA256 |
|---|---|
| 原CI failed.private.log（親が保持） | 7610BFA2D412E09F7A9AABD4A9CF2881989896D7D0857BC9DF0A042AE27DCB5F |
| red-slow-observation.stderr.log | 42B650C638F7A2A01373B33E9A6686831BD893C21EEA7E7F145D635EF1BC9356 |
| green-capacity.stderr.log | C1AFCEF43D0DF3779E045B02E093F2A9027A64496662416C7812074F03BF9680 |
| related-capacity-queue.stderr.log | 5A721A1AE9C21B57164C24A0C61154E3B63F7EE4A0C5A3F5BEEC600EFC93AAA8 |
| final-capacity-guards.stderr.log | D298C29ED10A01A2421050432F7CFA8437417334F7EE2EEDCF231412898FB475 |
| clippy-final.stderr.log | 4EA83A58AA1EE011F973A20EDE9CCB1B319F82071F6E5C3E36A71F9FEF22B348 |

記録した4fixture rootの残存プロセスは0。関連テストも所有プロセスの終了確認を含めて成功した。他担当のプロセスは停止していない。

清掃は一部保留：所有targetの2,362,961,975 bytes（3083項目）をsafe-deleteのsize指定で退避対象にした。正規quarantineがCドライブを選びQUARANTINE_CROSS_DRIVEで操作前に停止したため、Skillの同じドライブの退避手順で `D:/削除希望/20261023-todo-3411-ci-cargo-target-b2a56cba/files` へ移動した。リンク/Gitデータ/稼働writer不在、移動前後の件数・容量一致、元パス不在を確認した。原所有receiptは変更せず、`manual-quarantine.private.json` と退避先の理由.txtに記録した。**容量は未回収**。次は既存の定期清掃で回収し、本人判断は不要。親はこの保留も通常の報告へ含める。

親3392の残りは、このbranchのbase..HEAD差分を確認してPR393へ統合し、別モデルのMustレビューと統合HEADの正式CIを確認すること。本工程の正常終了をPR393全体のCI成功へ読み替えず、元のcc-ask配布・障害全体の工程を保持する。独立checkなので親の完了待ちや本人判断blockへ変更しない。
