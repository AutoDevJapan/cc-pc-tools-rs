# 同一利用者のログオン前の非UI復帰（ToDo2492）

親2488からの独立作業。2490は完了のまま保持する。書込範囲はresident-guard/、deploy-resident-2490/、docs/readiness-service-repair.mdとこの文書。親のDiscord受付、スケジューラー、ToDo、清掃は変更しない。

## 計画

既存利用者と同じSID、S4U、Limitedの専用検査タスクをCC-AdminBrokerから登録し、実行する。GUI実行版で窓を作らず、ユーザーが一致すること、同じプロフィールの場所、セッション番号、昇格していないことを測定する。健康API3種類と既存Codex/Claudeの認証状態確認コマンドだけを読み、秘密・メール・組織名・応答本文は出さない。OAuthの保存先や認証方式は変えず、既存トークンをコピーしない。匿名の公開HTTPS疎通も認証状態とは別に記録する。

MicrosoftはS4Uを、パスワードを保存せず非対話デスクトップで動くログオン方式と説明している。またネットワークと暗号化ファイルへの制約があるため、今回の実測をWindowsのネットワーク資格情報や暗号化されたデータ全般の利用許可と混同しない。参照: [TASK_LOGON_TYPE](https://learn.microsoft.com/en-us/windows/win32/api/taskschd/ne-taskschd-task_logon_type)、[BootTrigger](https://learn.microsoft.com/en-us/windows/win32/taskschd/boottrigger)。

S4Uの実測が成立すれば、稼働中の非UIサービスを止めず、既存タスクへ同じ権限のS4U主体と起動時のトリガーを設定する。Chromeと本人のGUIには変更しない。必要なタスクだけを追加変更し、原定義を秘密のない形で保持する。本人操作・画面ロックの迂回・自動ログイン・再起動・ログアウト・更新抑止は行わない。親のCC-DiscordAskTickには触らない。

## 実測

2026-10-13 11:53 JST、最終実行版の専用S4Uタスクが終了0で完了した。本人と同じSID、同じプロフィール、Session 0、管理者グループが有効でないこと、Mediumの整合性を確認した。健康API3種類が正常。Codexの`login status`とClaudeの`auth status`がともに終了0でサブスクの保存済み認証状態を読み取れた。Discordの公開gatewayへ、資格情報なしのHTTPSで200も確認した。

認証状態を読み取れたことを、モデル実行・残りの利用枠・OAuthの更新成功とは扱わない。親が別に報告したAnthropic経路の401と両立する。Codexの公式説明も、状態確認の終了0は資格情報の存在を示すものとしている。参照: [Codex CLI login status](https://learn.chatgpt.com/docs/developer-commands?surface=cli)。

## 固有の条件と実装

最初のS4U・Limitedの検査では、同じ利用者・プロフィール・Session 0を確認したが、TokenElevationの印がtrueだった。設定上のLimitedだけで実際の権限を判定せず、正規のSAFER NORMALUSERレベルを開いて自分の子プロセスのWindowsアクセストークンだけを下げ、同じS4Uセッションで実行する実装にした。アカウント、グループ、Windowsの認証・UAC・権限割当・ロック画面を変更していない。OAuthトークンや保存済みパスワードはコピー・移管していない。

このPCでは、元のプロセスのトークンにTOKEN_ADJUST_DEFAULTを要求するとAccess Denied 5になった。最低限のQuery/Duplicate/Assign Primaryで開き、正規のSAFER APIが作るNORMALUSERトークンへMediumを設定すると実行できた。資格情報を指定するログオンAPI、他セッションのトークンの取得、AppLockerを無効にするMAKE_INERT/SANDBOX_INERT、Windowsのポリシー変更を使用していない。

子プロセスでもTokenElevationの印はtrueのままだったが、管理者SIDは有効ではなく、整合性はMediumだった。実際の利用者SID・管理者SIDの状態・整合性を照合して判定する。IsTokenRestrictedは制限SIDのリストだけを検査するため、今回falseだったことは権限削減の失敗を示さない。参照: [SAFER NORMALUSER](https://learn.microsoft.com/en-us/windows/win32/api/winsafer/nf-winsafer-safercreatelevel)、[SaferComputeTokenFromLevel](https://learn.microsoft.com/en-us/windows/win32/api/winsafer/nf-winsafer-safercomputetokenfromlevel)、[IsTokenRestricted](https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-istokenrestricted)。

子プロセスはCreateProcessAsUserで自分の権限を下げた同一利用者として起動する。Sessionを変更せず、パスワードを使わず、非対話のデスクトップを継承する。検査と監視の1回分は子を110秒まで待つ。従来から常駐するサービスの起動モードだけは、タスクの既存の常駐寿命を維持し、追加の再起動ループを作らない。

## 配置した既存タスク

以下の既存タスクを、同じ利用者・S4U・Limitedにした。元のログオン/定期トリガーと設定を保持し、起動時トリガーだけを追加した。設定変更前後で3つのサービスの待受PIDが同じだったことを照合した。稼働中のサービスを停止・移動・重複起動していない。

| 既存タスク | 起動後の待機 | 以後の処理 |
| --- | --- | --- |
| opencodex-proxy | 20秒 | 既存の非UI起動経路を継続 |
| local-ai-web-autostart-10012 | 30秒 | 同じ実行版・作業場所でサーバーを起動し、寿命全体をタスクから追う |
| godd-ao-semantic-proxy | 60秒 | OpenCodexの正常応答を待ち、既存経路を継続 |
| CC-ResidentGuard | 90秒 | 既存の5分間隔と起動の抑制を維持 |
| CC-OcxHealthWatchdog | 120秒 | 既存の所有確認付き監視を継続 |

サービス起動モードは固定した4種類だけを受け付ける。HTTPが正常な場合、またはポートが開いた/判定できない場合は新しいサービスを重ねない。起動直前に再確認し、サービスごとのWindows mutexでも重複を抑える。Operationsは元のVBSが非同期で終了していたため、同じサーバー実行版と作業場所を使ってタスクが寿命全体を追うようにした。ChromeやGUIアプリの起動モードは受け付けない。

配置先は `D:/CC/Tools/resident-guard/resident-guard.exe`。最終SHA256は `4515C5BAF01E97D72BA9AD738A0F0B03739166095E46FCA2DCDF7FEAEBA51B72`。専用検査の結果は `deploy-resident-2490/boot-probe-state-2492/session.json`。原5タスクの定義と主体は、秘密を含まない既知の起動定義を検査してから `deploy-resident-2490/boot-tasks-before-2492/` に保存した。`restore-boot-tasks-2492.ps1` は元のInteractive/Limited定義を戻す手順であり、今回実行していない。

## 検査結果

- 最終ソースのGo検査は39件成功、失敗0、vet成功。うち実タスクの定義読取3件は明示した検査用環境変数でのみ実行する。
- 同じ利用者を保持して管理者グループを無効にし、Mediumへ下げるWindowsトークンの検査が通った。GUI/未知の起動対象を拒否し、起動処置を固定したパスと引数だけに限定する検査が通った。
- 既存2490の状態保存、連続再起動の抑制、曖昧な起動、タイムアウト、出力上限、mutexとログ容量の検査は維持した。
- Linux用の検査プログラムのコンパイルも成功。Linuxで動作させたとは扱わない。
- 専用のS4U検査は最終版で11:53:15〜11:53:18に終了0。資格情報やWindows設定の変更なし。
- 専用のS4U起動検査は11:55 JSTに終了0。稼働中のOperationsへ起動要求を出し、元のPIDを維持し、追加の待受を作らなかった。検査用タスクは削除した。
- 本番CC-ResidentGuardはS4Uで11:40、11:45、最終版の11:50、11:55にも実際の健康記録を更新した。各回3サービス正常、起動要求0、処理約2.27秒。最終の実行結果と次回は `deploy-resident-2490/boot-final-acceptance-2492.json` に保存した。
- 専用S4U検査タスク2件は削除済み。終了と参照不在と自分のSHAを照合した不要な検査バイナリ7件（52,766,208バイト）を削除した。ソース・検証の結果・原設定・配置済み実行版と復帰用バイナリは保持した。Linuxの一時検査プログラムも削除した。

## 残る制約

- OSのコールドブートやログアウト/ロックは実施していない。今回確認したのは、本人がログオンしている同じPCで別のS4U Session 0を正常に実行できることと、正規の起動時タスクの配置。3つの本番サービスは元のPIDを保持しており、実際にS4Uで再起動した検査ではない。
- 再起動後の最初の本人ログオンがない状態で、プロフィールの読み込み、暗号化ファイル・DPAPIで守られたデータの利用、OAuth更新が可能かは未検証。資格情報の移管や認証方式の変更で埋めていない。
- 認証状態確認の終了0は、有効なモデル経路・利用枠・外部の再認証不要を保証しない。親のモデル経路の実行検査と区別する。
- ChromeとGUIは元の本人のInteractiveのまま。OS再起動後、ログオン前にGUIを使えるとは扱わない。本人へPC操作を依頼していない。
- Windowsのネットワーク資格情報を必要とする接続や暗号化ファイル全般の利用を、今回の匿名HTTPS200で可能としない。

親のCC-DiscordAskTick、unattended-runner、scheduler、ToDo、cleanupには変更していない。個別のコミットは作っていない。別モデルの確認、全workspace、CIとマージは親が統合する。

## 追加変更ファイル

resident-guard/: `main.go`、`native_windows.go`、`native_other.go`、`boot_probe.go`、`boot_probe_windows.go`、`boot_probe_test.go`、`restricted_windows.go`、`restricted_windows_test.go`、`service_launcher.go`、`service_launcher_windows.go`、`service_launcher_test.go`、`native_windows_test.go`。

deploy-resident-2490/: `.gitignore`、今回専用のS4U検査/再配置/非重複起動検査/復帰用ps1。文書はこのファイルと `docs/readiness-service-repair.md`。実行版・検査結果・原定義のXMLはGitへ入れない。
