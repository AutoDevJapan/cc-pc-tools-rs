状態: 完了

読取レビューだけ完了。Must 0件、Suggestion 3件、Nit 3件。

固定: base f1c3c265f32f72067dc39494d8a864b37500ad22 → head 7d6f73337e32a9d0d7e9f0e0b43dff374039461c / tree 7b0d93f8afeaf5433b9ab5d2f745165e0c9d82b8。git show/diffだけで読み、作業HEADは使っていない。

読了: 元応答3272と指定12ファイルを固定head全文。差分は guest_linux/job_linux/job_windows/guest_windows の変更関数と周辺、sourcepacket.go と signed_payload_test.go（空白・;・,を除くと本文一致、gofmtのみ）。追加局所: job_contract、timeout_windows、host_windows、gate_windows、windows_job_windows、windows_clone_windows、session.go、pressci route/admission(plan)、SDK guest_service/transport。未読: secure_files*、source_materialization、normal_ci_admission本体、親2766の起動script/scheduler（最大実行時間は不明、local-gatesは380分必要）。

Must: なし。13roleの外側期限は読取範囲で全て元予算以上。Linux: guest socket role+10(job_linux.go:160)、listener 起動時=workflow最長+15・受理後はPressだけrole+15(:164-167、validLinuxJob通過後)、host socket role+6(job_windows.go:259)。Windows: host/channel role+6(host.go:40,51 channel.go:97)、Run role+3(guest_windows.go:268)。αは受理後40・boot45・Windows26分のまま（plan待ちは40→1分に縮むが、hostはheader+planを即送る job_windows.go:269）。listener満了はos.Exit(1)で排出なし(guest_linux.go:354-360,382)だが、先にsocket期限→ack reader cancel(job_linux.go:226-229)→runner kill(:283)→終端event送信が済む。SDKのHTTP WriteTimeout 60分(transport.go:318)はchannel.go:106がhijack後に解除するため無影響（実server設定のテストはなし）。JobName同一性、probeは記録のみ(job_windows.go:355,382)、Rust版選択も確認。

Suggestion
1. 共有予算role+20(timeout_windows.go:19、起点host_windows.go:482)にgate待ち(gate_windows.go:188-192)、Fetch、VM準備(Windowsはclone/VM/COM/Dial windows_job_windows.go:237-257、段ごとの上限の合計は14分超あり)、guest準備、公式完了待ち・ログ保存が全部入る。元予算ぎりぎりまで走るroleだけ親が先に切る。確度中(所要未測定)。修正: gate admitted後にrole+20を開始、またはpre-job許容を別定数で加え、dial/HostSession時点の残りがrole+6未満なら早期fail。
2. テストが踏まない配線2つ。(a) press_deadline_linux_test.go:59はtimerにnilを渡すため、job_linux.go:164-167のReset、guest_linux.go:79のg.lifetime、:345のAfterFunc(guestLifetime)が未実行。(b) 本番の plan: seed.Plan(guest_windows.go:79)を消してもzero planが26分に落ちるだけで全テストが通る。修正: AfterFunc(50ms)を渡し「Pressは発火せずαは発火」を見るテスト、newChannelServer(seed)を抽出しrole+6を見るテスト。
3. Windows Run(role+3)とsession(role+6)の差は3分。転送・prepare・configure(最大3分 guest_windows.go:244)が超えるとsessionが先に切れ、timeout分類とdrain証跡を失う(αと同定数)。Pressは+10程度に。

Nit
- swapped-shardsはshard1↔2固定(admission_test.go:245)。選択roleがshard3/4・非shardでは別jobの不一致で拒否される。選択shardと隣を入替える。
- guestLifetimeのrole一覧(job_policy.go:82)がpressci.RoleJobと別管理。RoleJobから導出。
- ±1秒許容(press_deadline_linux_test.go:65、deadline_test.go:59、channel_test.go:100)は高負荷でflakeしうる。

3272の指摘: Suggestion1はfixtureが素path・PR event・候補head_sha(admission_test.go:192-210)で作者の実GET記載と一致。私の新規GETなし。local-checksのdispatch run実形は未観測（αの素path通過は3272の読取止まり）。Suggestion2・3とNitは解消。

検査(私): 変更Go 18ファイルの固定head blobをgofmt -l(全OK、base版は陽性対照で検出)、git diff --check exit0、Linux既存testがserveの40分設定に依存しないことを読取確認。未実施: go test/vet/race、実API/CI、VM/runner、Web取得。作者の実行結果は未再現。