# 未回収workerの入力を保持するowner

子プロセスの回収を確認できない間は、そのプロセスが使った入力やtoolchainを閉じずに保持する必要があります。
この内部モジュールは、元のlive sessionから入力・toolchain session・実行結果の受取先をまとめて引き受け、その後の回収と解放を管理します。
通常の起動処理や実OSの回収処理との接続は別工程で、toolchainの利用資格を発行する機能もありません。

## 入力と所有

通常のcontrollerが、ほかの呼出しと競合しない区切りで、元live sessionのslotと、providerが移管したsoftwareRustHeldSessionのslotを渡します。
成功時にslotを空にし、元live/ownedflightを空の状態へ移します。同じDoneを旧sessionと新ownerが二重消費しないための移管です。
旧liveやbaseのCloseは空のclosed状態で戻りますが、別に保持した旧ownedflightやrawflightを呼び続けることはできません。
rawflightを外部へ残したまま同時に呼ぶことはできません。移管のためのcaller間排他は通常controller/nativeadapterで結ぶ必要があります。

registryはstrong referenceを保持し、返すopaque ticketは同registry内の照会にだけ使います。
文字列のowner ID、PID、JSON、callerのreaped=trueは、解放の証拠として受け取りません。
登録数はcontrollerが指定する有限の上限を持ち、満杯で移管できない場合は元slotを変更せず返します。
同じleaseや内部入力がすでに登録されている場合、またはDone・Abortが欠ける場合も移管を断ります。
flightのpointerが別でも、同じDoneチャネルを共有する登録は断ります。
別registryへ同じ資源を重ねて渡さないことは、単一controllerが所有権を移管する際の前提です。

## DrainとClose

Drainは移管されたexact ownedflight.Doneから一度だけ実結果を受け取ります。
Started・Assigned・WaitVerified・Reapedと、NativeProcessのPID/Creationがそろった結果だけを回収事実として扱います。
ProtocolComplete、ACK、終了コードの成功とは別です。非ゼロ終了でも実回収が確認できれば入力を閉じられますが、製品の成功にはしません。

結果が来ていない場合、回収の確認が足りない場合、Doneが結果なしで閉じた場合は、Closeを始めません。
子プロセスのTimedOutがtrueでも、実回収を示す上記の条件がすべて成立すれば解放できます。
取消は元flightへAbortを一度要求するだけで、registryの資源を外しません。
待機の取消・期限切れと、実子プロセスのTimedOutは別の欄へ残します。別のcleanup contextで再びDrainできます。
並行Drainは同じ待機を共有し、leaseのClose中に再入した要求は進行中の状態を返して二度閉じません。

回収確認後にtoolchainを再確認し、capture入力と追加inputを閉じ、最後にtoolchain sessionを一度閉じます。
同じpointerの追加closerは重複排除します。Closeが失敗しても再実行せず、失敗とowner referenceを残します。
stateがreleasedになるのは、回収後のtoolchain再確認とCloseがすべて成功した場合だけです。
これは改稿やビルドの成功を示す値ではありません。captureのQualifiedは常にfalseを保ち、公開済みreceiptを示す値とcapture/toolchainの両Reasonを残します。

owner自身は監視goroutineやtimerを作りません。既supervisorが明示的にDrainを呼ぶ必要があり、自動運用へ接続済みとはしません。
回収済み・解放成功の記録を保存した後だけregistryから忘れられます。失敗中のownerを忘れて入力を失う入口は作りません。
Forget自体が確認するのはreleased状態です。記録の永続保存とForgetの順序は、接続するcontroller側で保証する必要があります。

| state | 意味 | 同じ資源への次の操作 |
| --- | --- | --- |
| retained | 回収を待っている、または回収の確認が足りない | 取消・照会・再Drainが可能。解放はしない |
| release_in_progress | 回収を確認し、一度だけ解放している | 再入した呼出しには進行中の状態を返す |
| released | 回収後の再確認と解放が完了した | 結果を保持し、記録後にForgetできる |
| release_failed | 回収後の再確認または解放に失敗した | 結果と参照を保持する。同じCloseは繰り返さない |

## 既存nativeadapterの不足

provider側には、Origin→Open→matcher/Recheckの後に自動Closeせずsessionを移管するopenRetained内部variantが必要です。
WithHeldのcallbackからheld viewだけを外へ返す方法は使いません。正常factoryは目的・ACL・currentobjectの実受入が未成立なら取得不能を保ちます。
元のunslopOwnedFlight receiverを別に保持したcallerへのnil guardも、通常controllerを接続する前に必要です。
このモジュールは移管元を空にしますが、既存receiverのReady・SendACK・Done・Abortの実装は変更していません。

既flight.Doneが最終的にReaped=falseを返した後は、そのchannelだけから将来の回収を追加証明できません。
この場合は保持を続けます。Job/processの実handleを所有する正規native reaperへ移管し、同identityのWaitとJob回収を証明できる接続が別途必要です。
その接続をPIDやboolで代用したり、channelが閉じたことだけで解放したりしません。

## 先に用意する受入

| Given（前提） | When（操作） | Then（期待する結果） |
| --- | --- | --- |
| 実行結果がまだ届いていない | Closeを取消または期限切れにする | 入力とleaseを閉じず、Abortは一度だけ要求する |
| 同じflightから実回収を示す結果が届いた | Drainを呼ぶ | 入力、追加input、toolchainの順に一度だけ閉じる |
| Reapedがfalseの結果を受け取った | 取消、再Drain、別の結果投入を行う | ownerを保持し、別結果を回収の証拠にしない |
| すでにleaseかDoneを引き受けている | 重複する移管を要求する | 新しいslotを変更せず、移管を断る |
| 回収後の解放が進んでいる | Closeへ再入する | 進行中の状態を返し、二度閉じない |

- 終了結果が来る前のClose要求・取消ではinput/toolchain Closeが0、ownerとcapture状態が残る。
- 同じflightの実回収結果を受けた後だけinput→toolchainの順に各1回解放し、再Close/再Drainは同じ結果を返す。
- incomplete/native identity欠落/未回収の各結果はClose0。protocolやexitの失敗と回収事実を混同しない。
- 並行Drain、再入Close、重複closer、移管の二重呼出、上限超過で二重消費や状態喪失を起こさない。
- Close失敗でも結果とowner referenceを残し、同じcloserを再試行しない。公開済みreceiptを削除・再Commitしない。
- 回収不明の結果を受け取った後の取消でもAbortを一度だけ要求し、その後の別結果で回収済みに書き換えない。
- 待機期限切れだけでWorkerTimedOutをtrueにせず、新しいcleanup contextで同じownerの回収を続けられる。
- 回収の確認ができた場合もできなかった場合も、受け取った終了結果のrawとstdoutを消去し、レポートへ転記しない。
- Reapedの必須確認だけを除くmutantが未回収ケースで失敗することを、正常工具の検証工程で確認する。

この文書と合成テストは実OSの回収・実lease移管・実authority・再起動耐性の証明ではありません。
ローカルGo/gofmtの資格が成立していないため、新コードのコンパイル/テスト/整形は未実行です。
合成テストの実行とReaped条件を外す負対照は、同じソースを使うWindowsのCIへ引き継ぎます。
