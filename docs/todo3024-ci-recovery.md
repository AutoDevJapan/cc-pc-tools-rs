# PR253の正式CIを復旧する作業記録

2026-10-20 JST。親は共有ToDo3020、担当は3024。

- 元候補は `9a907d39c2f8d0ba810436f78464a4e6f7fa7ebc`。元の別モデルレビューは Sonnet 5.5、最新Must0。正本の未追跡記録は保持。
- 正式Windows run `39279299745` のログを通常のGitHub APIで取得。344件中342成功、2失敗、3skip。失敗した2件はdisplay APIのHTTP fixtureが、Consoleだけを試験用へ差し替え、Operationsを未初期化の本番用のままにしたため503になった。元候補の正式CIを成功とは扱わない。
- Linux runner27はListener不在。既存コンテナの `/runner` 1GiBと `/tmp` 64MiBが使用率100%。inode枯渇ではない。診断ログは既存の永続workへ圧縮保全し、同じコンテナを停止後、元のHidden/S4U/Limited定期タスクで再起動。初期tmpfsは674MiB/1GiBと4KiB/64MiBになり、Listener1系統でonline/busyを確認。
- Windows runner28はidleであることとWorker不在を確認して元の専用仮想アカウントのサービスを再起動。一時的なsession conflictの後15:56:45 UTCに再接続。runnerの対象repo、権限、ラベル、hook、Cargoゲート、隔離は変更していない。
- APIのrun/check表示がqueuedのままでも、個別jobログには実行と失敗の記録がある。判定を混同せず、後続は固定SHA、実job、upload、終端conclusionを照合する。

次の工程: 試験用Operationsを同じ隔離rootから初期化し、失敗2件と影響範囲を検査する。追加差分の別モデルレビュー後、PR253へ非forceで反映し、正式CI・通常マージ・mainと稼働を確認する。追加の常駐ポーリングは作らない。3021/3023の担当や本人の会話には触れない。

コンテナは既存の継続資源であり削除対象ではない。OS再起動後の復帰は今回も未検証。tmpfsへの蓄積の再発防止は既存の保守担当と照合し、必要な有限作業へ分ける。
