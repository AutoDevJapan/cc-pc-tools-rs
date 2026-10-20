# 通常Linux runnerの容量不足を防ぐ（ToDo3026）

元範囲は `D:/CC/Work/unattended-operations-20261013`。実remoteは
`AutoDevJapan/cc-pc-tools-rs`。元範囲と専用worktreeはscrub391で
client_resource=false。原一覧や公式runner認証ファイルは読まない。
親3024のPR253、3021/3023、別製品runnerの設定は変更しない。

## 受付・現状の照合（2026-10-20 JST）

- 親3024の履歴と `docs/todo3024-ci-recovery.md` を照合。351MiBの診断ログと
  終了済み一時生成物でtmpfsが満杯になり、既存タスクと同じコンテナの再起動で復旧した。
- 元タスク `CC-LinuxRunner-todo2510` はRunning。起動先は元2516の
  `.ci-runtime/bridge.exe host normal <同worktree>/.ci-work/target`。
- runner27 online/busy、Listener1、Worker1を確認。正式ジョブ実行中のため停止・更新0。
- 既存 `/runner` は676MiB/1GiB、`/tmp` は1.1MiB/64MiB。
  `/work/todo3024-diag-before-restart.tar.gz` は存在し、gzip検証成功。
- image ID: `sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011`。
  稼働コンテナと、このimmutable imageから作った登録無しの検査用コンテナの
  `/opt/bridge/bridge` は双方
  `a376c1195e72333cd023692fbdbcb5cfe036db287c5057db0dbc17246b6c245f`。
- 元タスクのWindows host SHA256は
  `1ccf146572738759b6b27a656beaf74f1ce62ae51908bad8bb8f866cb5b44dbc`。
  両OSのbuild metadataはGo1.27.0、revision `0b703782a81e5c262f771caa849e766872591b2e`、
  modified=true。元2516の保存記録は7c4106bと旧imageを指しており、現在の配備版と異なる。
  元2516の作業ソースは7c4106bでbridge範囲に未保存差分なし。
  同フォルダのLinux用bridgeファイルは旧d61afa0eであり、配備原本と断定しない。
  現物と原記録を保持し、次の配備は固定ソースから生成した両OS成果物を記録する。
- 既存workは約7GiB。コンテナを作り直す場合はidleで停止してworkを丸ごと保全・復元し、
  元圧縮資料のハッシュ一致を確認してから既存タスクを再開する。ソースや原資料を消さない。

## 実装

既存監督処理を1ジョブ単位のsessionへ変更。再登録や追加の監視常駐は増やさない。
一時領域は作成したsessionだけに限定し、kernelのprocess group不在と追加プロセス不在を
確認した後だけ回収する。診断ログはsession所有記録と終了印のあるものだけに保持上限を適用。
未知のファイル・symlink・特殊ファイル・未完了の原資料は保持する。
登録権限・ラベル・hook・Cargoゲート・tmpfsのexec/noexec・init・UID・capdrop・device・bindは不変更。
`--once` は公式2.338.0にあるが将来廃止予告があるため、runner更新時の受入条件として明記した。

CargoはWorkerとは別のDocker execで起動する。通常モードの固定識別だけを転送し、guest側で
現在の所有sessionから同じ一時領域を解決する。hostや依頼から任意のパスは受け付けず、
完了済みsessionや所有不一致なら実行を拒否する。有限モードの挙動は維持する。

## 確認済みの検査

- Windows: bridge関連 `go test -count=1 ./...` と `go vet ./...` 成功。
- Linux: 上記immutable imageの登録無し専用containerで関連パッケージ全体の
  `go test -race -count=1 -timeout 120s ./...` とvet成功。
  init、UID1001、network none、capdrop ALL、no-new-privileges、2CPU/2GiB/256PID、
  `/tmp` はnoexecの8MiB tmpfs。追加権限・GPU・Cargoなし。
- 正常と失敗終了を各12回、毎回2MiBの一時生成物で確認。計48MiBを8MiBの同一tmpfsで
  順に処理し、終了後0KiB。診断保持8回、キーと既存原資料のバイト一致も確認。
- 容量のちょうど上限と1バイト超過、件数超過、未完了保持、未知資料・symlink拒否を検査。
- 背景writerをkernel groupから排出してから回収する検査と、別Workerが残れば
  scratch・診断・未完了印を保持する検査が成功。既存group取消・zombieの検査も維持。

## 残工程

固定候補の別モデルレビュー、Must修正、必要な正式CI、通常merge、idleを実確認した
原タスク経由の配備、host/image/container照合、単一Listenerと通常GitHub job成功の受入。
受付・実装・検査を配備済みとは扱わない。親3024は本作業の完了待ちにしない。
OS再起動後の復帰は未検証。通常の完了通知は送らない。

PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/254 。読取レビューは共有ToDo3027へ
Sonnet指定で登録済み。初回受付時点では未起動であり、実モデル・原応答・Must0は未確認。
最初のコミット5e0ff5eとCargo経路の追加修正を区別し、レビュー対象を追加修正後の固定候補へ更新する。

PCの一時所有記録: `6073da9dd984eb8c2a2356d00031843a`（safe-delete create）。
追加検査コンテナ名: `todo3026-capacity-check`。検査工程終了時に削除する。

## レビュー3027への対応（再開時）

原応答はSonnet 5.5、対象はbase3423a38からhead3031f8c。補完読取も受領し、
対象差分に未読なし。原応答のSHA256は
`38683aa22acc5c91caa33f2f1b99e70c52871908c4651b895104ee567229989c`。
保存した原応答はGit対象外の `docs/runner-todo3026-review3027.private.json`。

Must1は、hostの15秒ごとの短いプローブやreaperと静止確認が重なり、一時領域を
残して監督処理が終了する点。最大30秒の有限再確認を追加し、ESRCHは消滅として扱う。
zombieは成功扱いせず、回収されるまで同じ期限内で待つ。
次の静止した起動では元所有の中断scratchを回収する。診断にはcompleteではなく
interruptedを記録し、停止済み診断として通常の保持上限を適用する。
これで実際の中断が繰り返されてもscratchが蓄積しない。元3024の保全資料は別領域に維持する。

Suggestionのlock継承はO_CLOEXECで修正。読み取り専用の生成ディレクトリは、
同じUID・同じfilesystemのscratch内ディレクトリだけ所有者の権限を足して回収する。
ファイルやリンク先の権限は変えない。ジョブ中の診断切詰めは行わず、保持上限が
静止時だけであることを明記する。旧診断は3024が再起動後の状態で保全しており、
既存の原資料を自動削除する移行は追加しない。

head3031f8cの正式CIはCodeQL4言語、Build speed、Windows/Linuxの通常Go検査が成功。
追加修正後は影響範囲の再検査と別モデル再確認、固定head正式CIが必要。
