# OpenCodexの稼働版への反映調査（ToDo2655）

## 結論と対象

2026-10-15 02:33 JST現在、修正の稼働版への反映と切替受入は未完了。
既存proxyは2.83.0。正規updaterのlatest候補2.84.0にも今回の修正は
含まれていないことを公開Gitの該当ソースで確認した。候補の更新を実行しても
元2646の修正を配布できないため、更新・再起動・設定変更は実施していない。

対象は元2646の修正を現在のOpenCodexへ反映する工程だけ。
本人の「Selected model is at capacity. Please try a different model」が出たら
別の既存OAuthサブスク枠へ自動切替して続行する要件を維持する。
原1936はrecovery_unverified・66%のまま。本工程で原台帳・全製品を完了扱いにしない。

## ソースとCIの照合

GitHub REST APIで所有者mashfromband、repo ID 1358770414を再確認した。
[PR3](https://github.com/mashfromband/opencodex/pull/3)はmergedで、baseはdev。
devは統合ブランチであり、新しい稼働配備環境ではない。

- PR head: `1c45c0373808ecbeb059fea7780c45c18064683b`
- mergeと現在のdev: `2dd27f61851944e191375436f31a3a1272dae911`
- headとmergeのtreeは同一: `7841dff7dca9c0765d09a81c06f17203624febd0`
- [マージ後CI38603135829](https://github.com/mashfromband/opencodex/actions/runs/38603135829)
  は同merge SHAのpush、completed/success。
  Linux4分割、macOS2分割、gates、npm全3OS、keyring全3OS、docker、storage、API usageはsuccess。
  Windows本体suiteとmacOS controlのskipは成功へ数えない。
- 同CIのartifacts APIはtotal_count=0。合格したソースの配布パッケージは取得できていない。
- 元2646の最新noteは上記head、merge、CIと一致。
  元担当の別モデルgpt-6-lunaによるMust 0の記録はソースレビューの証拠であり、稼働受入ではない。

## 現在の正規接続と状態

正規CLI `opencodex status --json`、`service status`、`system health --json`、
`ready --json`を使った。認証ファイルと環境全体は読んでいない。

| 観測 | 結果 |
|---|---|
| 接続 | standalone、127.0.0.1:10100、remote hub未接続 |
| CLI/proxy | 両方2.83.0、version skewなし |
| proxy | running、PID 47256、healthz正常 |
| authenticated system health | service=opencodex、status=ok、version=2.83.0、同PID |
| spend ledger | degraded=false。全機能正常の証明ではない |
| ready | ready=false、status=failed、同PID・同ポート。非0終了 |
| startup診断 | at-risk、service stale、service viable=false、rebootSafe=false |
| service status | Task Schedulerによる既存サービスがinstalled/running |
| opencodex-proxy既存タスク | Ready、実行上限PT0S、IgnoreNew、最終結果0 |
| CC-ResidentGuard | Ready、実行上限PT2M、IgnoreNew、02:30:50の最終結果0、次回02:35:49 |

readyとstartup診断は別の観測で、staleをready失敗の直接原因とは断定しない。
公開2.83.0ソース `src/server/readiness.ts` は、起動後syncが例外/null/ok不成立のとき
failedへ一度だけ遷移する。ready APIには詳細原因が保存・公開されない。
現在のsync失敗の具体原因は未取得。私的なログ・設定を読んで原因を推測していない。
health正常を復帰health、ready成功、モデル実応答、OS再起動受入へ昇格させない。

`agent fallback status --json`は既存候補6件とpollMs=60000を返した。
候補にはGPT系とanthropic/claude-sonnet-5-5、anthropic/claude-opus-5-5がある。
このpoll intervalは障害モデルの60秒停止期限を実測した証拠ではない。
既存Anthropic accountの正規一覧はtype=oauth、active=true、health=Healthy。
これは資格情報を表示しない状態観測であり、切替先の実応答や全候補の許可を証明しない。
GPU/localモデル、新アカウント、APIキー、推測した候補は選択・起動していない。

## 正規updaterと公開パッケージの由来

`system update check --json`はcurrentVersion=2.83.0、latestVersion=2.84.0、
channel=latest、installer=npm、canUpdate=trueを返した。
これはソフト更新機能の表示であり、保護された配置先への変更許可ではない。
公開npm registryのmetadataは次の通り。値はパッケージ実体の実測hashではない。

| version | npm gitHead | 公開日時(UTC) | npm shasum |
|---|---|---|---|
| 2.83.0 | 33b321853aff76045617824793cc7cbc1d4084e7 | 2026-10-10T19:53:12.872Z | a7d3416119d97cd28d1ae025689d695e2dd814da |
| 2.84.0 | 6955343008e76e96d1c89bb32a9fc49f50e44fb9 | 2026-10-13T02:01:11.502Z | 118e179e26699cec5d6d19b315b5f7d578c8c5ed |

両versionのrepositoryはlidge-jun/opencodex。updaterもそのリリースURLを返す。
これは参照のみ。他所有者のリポジトリ・配備環境は変更していない。
所有forkのmergeのpackage.jsonはversion=2.47.0のまま、repositoryも上流を指す。
そのまま公開npmのlatestを選んでもforkのmerge SHAへ対応しない。

公開GitHub contents APIのbase64をメモリ内で復号し、実bytesのSHA256を計算した。
ソースの作業コピーやcloneは作っていない。2.83.0と2.84.0の下記2ファイルは
同じGit blobと実SHA256で、PR3の修正ソースとは異なる。

| ソース | Git blob | bytes SHA256 |
|---|---|---|
| 上流両version errors.ts | 4393663415455dad371498766765ffcaf0ab20e5 | 9ad976f3547f1cbaab1e5500c5e3f516180f56dc46d786729b85bab25b29f24a |
| fork merge errors.ts | 98ce789ff33796493ef4bd23a4fdc08a832c465f | e8d5b3272b7efece704bb32192faa62e812ec5b68cc54ba6ef8886b47e64f855 |
| 上流両version subagent-model-fallback.ts | 7f9e22648dd149087eefefa6aa6d0191a18a30be | 1e776de821e7f0cf2121f57b5d2f1b604bc81eb39571c68f722107d518461602 |
| fork merge subagent-model-fallback.ts | e2c955bb545d32d1e85c33d8c8945f392dd85edd | 8017d7ffba3468a5b86f45012e542600d42512d9a6e84eaee493be448116266e |

上流fallbackはisRateLimitOrQuotaFailureMessageだけで失敗記録を判定する。
forkはclassifyErrorのserver_is_overloadedも判定する。
明示capacityの正規表現はfork errors.tsだけに存在する。
npmのgitHeadに対応するソースの比較であり、インストール実体やtarball bytesは未照合。

正規 `system update run` の宣言引数はchannel latest/previewとrestart on/offだけ。
固定fork SHA・候補ファイル指定は宣言されていない。
既存ADR0001によれば更新は隠れたworkerを使い、npm側のstop/install/start後に
更新に対応するPID/versionと安定healthを照合する。バックアップ付き更新の実成功、
更新失敗時の原版維持は今回未検証。主TUIを止める操作もしていない。

## 保護と再開条件

稼働CLIが返したパッケージ配置先を `scrub391 --guard-path` で判定した結果は、
client_resource=true、classification_unverified=true、original_registry_recovered=false。
実際の案件資源だと断定はできないが、未確認なので元資源の直接読取・hash取得・書換えは停止した。
保護された配置先のパスは本記録へ転記しない。
これはguardの読取結果であり、今回の自動承認レビュー拒否やWindowsアクセスエラーではない。
正常なmetadata/status/helpは許可された観測として使い、別folderへの複製・別手段の操作はしない。
拒否済み10件scope登録、manifest/guard変更、旧不存在フォルダの読取は再実行しない。

再開時は本記録と元2646の最新noteを読み、現在dev・updater候補・healthを正規経路で再照合する。
現時点の通常updaterは上流npmタグを使うので、固定forkの成果物を指定する経路は確認できていない。
これは未解決の配布課題であり、以下の更新条件が現在成立しているとは扱わない。
当面の再開操作は6時間後の有限な読取再確認とする。`system update check --json`でlatestを取得し、
公開registryのgitHeadに対応する2ファイルを本記録の修正blob/bytesと照合する。
変更がなければ更新をせず結果を保存し、同じToDoのwait-retryへ戻す。
正規guardの判定も読取だけで確認し、未確認のままなら元資源を操作しない。
ソースのパッケージ化や候補の指定を新たに実装する工程は未実施で、
既存経路・保護範囲を確認できずに新規配布や別のインストール経路を作る代替には進まない。
元の保護範囲が正規に解決し、固定修正ソースに対応する通常更新経路が確認された場合だけ、
候補と稼働実体の実hash、既存タスクによる復帰、バックアップ/失敗時維持を検証して更新する。
上流latestが修正を含むと推測せず、上のblob/実bytesとの対応を確認する。
新公開・npm他所有者への配布・新サービス・新費用・新権限で穴埋めしない。

その後の制御受入では同じ元会話/依頼を維持し、正規capacity失敗を記録し、
60秒停止、認証拒否との区別、既存OAuth枠への選択、実際の依頼続行まで確認する。
出力済みstreamの再送やunit passだけを続行成功にしない。
現在、この一連の稼働受入を開始できる正規注入経路も未確認。
不明な内部APIの呼出し・実OAuth障害の強制・稼働設定変更は行わない。

完了条件5件（source/packageと稼働版の対応、復帰health、正規切替実受入、
更新失敗時維持と所有cleanup、元ToDoへの結果配送）のうち、最初の4件は未達。
調査終了だけでToDo2655をdoneにしない。本人へ技術手順・PC操作・本人性の再質問は不要。
ここでいう復帰healthは通常更新後のproxy復帰を対象とする。
OS再起動・ログオフ後の復帰は未検証であり、今回の完了条件へ追加しない。
必要になった場合も、まず正規startup診断と既存タスクの読取で確認できる範囲を調べ、
主TUIの停止を伴うOS再起動や本人PC操作を本工程で要求しない。

## 再実行不要と所有物

GitHubの元2646修正・同ソースのCI・元レビューは完了済みで、初回commitへ戻さない。
今回の変更は本調査記録のみ。GPU/Cargo/子Codex直接起動、Chrome新規タブ、
Docker検証資源、サービス停止は0件。
調査後のレビューspecとToDo配送用noteに所有一時フォルダを1件作成し、
所有ID `dbe612049f4c8863d9470e432d234015` の3項目10,705 bytesを
plan→別呼出し通常削除→finishで整理した。finish結果はsource_absent。
原specはToDo2658へコピー済みで、配送結果は元2646と2655へ正規editで保存し読み戻した。
02:39の再確認でもproxyはPID47256・2.83.0・health正常で、調査前と一致した。

## 別モデルの確認と残る処置

単一文書レビューを正規subworkのToDo2658へ登録し、
実ジョブ `20261015-023823-todo-2658` のstatus APIで
anthropic/claude-haiku-5-5、succeeded、exit0と最終回答を受領した。
直接の子Codex起動ではない。レビューは稼働受入ではない。

- Must 1（配布経路の不足）は採用した。上に現経路の限界、具体的な有限再確認、
  未実装の配布工程を追記した。実際の配布経路自体は未解決なので、Must 0や反映完了とは報告しない。
- Must 2（OS再起動の検証を条件に加える提案）は本工程へ追加しない。
  本人の主TUI停止禁止と元の更新後復帰受入の範囲を維持し、OS復帰未検証を明記した。
- Suggestion（稼働2.83.0とfork 2.47.0の差）は、正規status/system healthで稼働版を照合済み。
  追加の同じ照合をせず、上流パッケージとforkソースを混同しない説明を維持した。
- 再質問を提案するNitは採用せず、元依頼・元ToDo・元回答の範囲を保持して読む。

文書をレビュー済みとして保存したことと、稼働製品を完成させたことを区別する。
完了条件5件のうち結果配送だけ達成した（1/5）。残る4条件は未達。
未解決の配布経路と保護判定を有限の技術再確認として保持し、owner判断blockは使わない。
