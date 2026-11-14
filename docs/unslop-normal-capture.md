# 通常のcaptureへ所有を引き継ぐ

通常の起動処理をつなぐには、workerが使っている入力とtoolchainのleaseを、回収を確認するまで同じ担当が持つ必要があります。
内部のCaptureOwnedは、providerのopenRetainedで実際に取得したsessionにだけ所有capabilityを発行し、同じcontrollerのcaptureを実行します。
回収が済まなかった場合は、そのcapabilityと元のlive sessionを[保持owner](unslop-retained-owner.md)へ引き継ぎます。
通常factory、保存先の取得、dispatcher、インストールは有効化していません。
beginが失敗してもsessionを返した場合は、そのsessionと取得済みleaseをcontrollerが保持します。
返却結果のowned_inputs_retainedがtrueとなり、sessionやエラーの本文は返しません。nilとerrorだけが返る場合は、未使用のleaseを一度だけ閉じます。
途中取得したsessionの回収方法が未確定なら、その保存結果だけでCloseや通常ownerへの移管を許可しません。
owned_inputs_retainedは、元のsessionだけでなく、移管待ちや保持ownerに残る入力参照も含みます。
移管後の未回収・解放中・解放失敗ではtrueを保ち、解放が終わればfalseになります。Close失敗後のtrueは資源参照を残している意味で、無効になったcapabilityを再利用できる意味ではありません。

## 接続部が持つもの

通常callerは、ほかの呼出しが触らない区切りで、元live sessionとleaseのslotを渡します。
接続部は、providerが発行したcapabilityの元session・保持バッファ・controllerの世代と、captureのstartが返した実flight・Doneを確認してからslotを引き受けます。
成功後は元slotを空にし、移管が終わるまでは接続部が同じ参照を保持します。
別のsessionは、同じバッファを返しても引き受けません。
同じlive pointerの中でflightやDoneを差し替えた場合、別controllerの結果や閉じたcapabilityを渡した場合も拒否します。
このflightの固定はbegin時ではなく、Buildが呼ぶstartから実flightが返った時点で行います。
nonceと入力manifestも同じcaptureへ結び、結果を受け取った後はnative identityの一致も確認します。
capabilityは取得元providerの台帳に登録された一回の所有であり、callerが作ったstructや同じtupleからは発行できません。
一度使った取得元から古いsessionを再取得したり、閉じたproofを別の世代へ移したりもしません。

所有slotを移すたびにwrapperも移し、前のwrapperからHeldやCloseを呼べないようにします。
最終Closeは、capabilityを先に無効化してから元sessionへ一度だけ行います。
Closeが失敗してもcapabilityを再利用できる状態へ戻さず、ownerの資源参照と失敗reportは残します。
保持ownerは発行済みwrapperの元sessionだけを一段たどり、実fileやhandleを別wrapperで重ねて所有しないか確認します。未知のCloserを展開しません。

Transferは既存のdurable controllerのRetainを使います。
保存失敗やownerの上限で移管できない間も、入力とleaseを閉じずに保持し、同じ接続部で再試行できます。
移管後に保存だけが失敗した場合は、返された同じticketを残し、再登録せず保存を再試行します。
Statusは、移管前に入力を保持している状態と、ownerが持つreportを区別して返します。
本文、元path、自由文のエラー、実行許可をStatusへ含めません。

移管後のDrainAndSaveは、同じcontroller・owner・ticketを使います。
取消は同じflightへのAbortを一度だけ要求し、回収が不明な間はCloseやForgetを行いません。
実回収の確認後は既存ownerが一度だけ解放し、同じreportの保存が成功した後にForgetします。
解放または保存に失敗した場合も、その結果を持った同じownerを残します。
保存とForgetが完了したreportは接続部にも残し、再呼出しには同じ保存結果を返します。

## まだ必要な接続

toolchain providerのopenRetainedには、検証したleaseの所有をcallerへ渡す内部入口があります。
通常factoryはまだ有効になっておらず、保存先rootの利用資格もこの接続部からは作れません。
JSON、path、hash、成功フラグからleaseやrootを組み立てる入口もありません。

既存CaptureはWithHeldのcallback内で動き、その終了時にproviderがleaseを閉じます。
この借用経路からは所有capabilityを発行せず、内部のCaptureOwnedと区別します。
通常接続には、利用資格を満たしたproviderと保存先をCaptureOwnedへ渡すcallerが別途必要です。

owned captureのCloseは、同じflightの実結果にnative identityとStarted・Assigned・WaitVerified・Reapedがそろうまで入力を保持します。
既存の借用経路のCloseや通常dispatcherは変更しておらず、そちらの接続条件は別に残っています。
消費済みDoneから将来の回収を推測したり、PIDだけでownerを作り直したりはしません。
実handleを持つnative reaper、通常factory、root取得、dispatcherは今回の新規接続部から変更していません。

## 先に確認する条件

| 前提 | 操作 | 必要な結果 |
| --- | --- | --- |
| 元slotと、controllerが観測した同じsession・leaseがある | 接続部へ渡す | 元slotを空にし、同じ参照を接続部が保持する。CloseやAbortは始めない |
| beginがsessionとerrorを同時に返す | captureを終了する | sessionとleaseを保持してowned_inputs_retainedを返す。入力のCloseやAbortは行わない |
| session、バッファの実体、照合値、controllerの世代が異なる | 接続部へ渡す | 拒否し、元slotと資源を変えない |
| 別sessionが同じバッファを返す、または同live pointerのflightが差し替わる | 接続部へ渡す | 拒否し、元slot・両session・両flightを保持する。CloseやAbortは行わない |
| 移管前に保存が失敗する、またはownerが満杯 | Transferする | 元入力を接続部に保持し、同じcontrollerで再試行できる |
| 移管後にreport保存が失敗する | 再びTransferする | 同じticketの保存だけを再試行し、再登録しない |
| 移管済みのworkerがまだ回収されていない | 取消済みcontextでDrainAndSaveする | Abortは一度だけ、CloseとForgetは0。ownerと入力を保持する |
| 同じflightの回収を確認できない結果が届く | DrainAndSaveする | reportを保存できても入力を解放せず、後から来た別結果で書き換えない |
| 実回収後のCloseが失敗する | 再びDrainAndSaveする | 失敗reportとownerを残し、同じCloseを繰り返さない |
| 解放は終わったがreportを保存できない | 保存を再試行する | 入力を二度閉じず、保存後だけForgetする |

合成テストは、元の保持slotを渡す境界と保存・取消・再試行の契約を確認するためのものです。
正例もproviderの取得処理を通してcapabilityを発行し、captureのstartが返した実体を記録します。
nativeの入力読取とworker起動は合成sessionで置き換えるため、実OSへの接続を証明するテストではありません。
ソース準備や合成テストの成功を、通常factoryの実接続、実OSの回収、インストール、元の文章評価2件の完了には広げません。
