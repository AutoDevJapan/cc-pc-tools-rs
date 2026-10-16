状態: 完了

**Must: なし。**

固定HEAD `1c0ef903ee3dbdc1bd5f7f339fd89ee92500e4b4`（base `3002f5ec…` の1コミット先、4ファイル）、モデル claude-sonnet-5-5、検査は未実行です。ローカルのシェルが起動しないため、同じSHAをGitHub連携で読みました。実owner は AutoDevJapan（Organization）です。`AGENTS.md` と `docs/build-speed.md` は差分に含まれず、読んでいません。

**Suggestion**
1. `snapshot.rs:19,97`: layout と offset は ntexapi.h と一致します。ただし生成時刻と親PIDは Microsoft 文書では Reserved 欄です。生成時刻がずれた場合は候補照合で失敗側に倒れます。親PIDがずれた場合は子孫を黙って取りこぼし、停止が漏れます。`processes()` で、自プロセスの親PIDが `NtQueryInformationProcess` の値と一致するかを実行時に確認し、不一致なら Err にしてください。
2. `pause.rs:828`（仮説）: 古い辺の除外は「親の旧世代の生成時刻」との比較だけです。親PIDが別世代に再利用されていても、その新世代の子は除外を通ります。開けなければ fail-closed、開ければ停止対象に入ります。保持handleがPID再利用を防ぐ範囲では起きないはずです。prior のみの辺や vanished 保持が対象です。親エントリの created が found と違うときは、候補が新世代より前に生成された場合だけ通してください。
3. `pause.rs:839`: 開いた後の世代不一致で cancel 全体が Err になります。fail-closed として妥当です。呼び出し側に、回数上限付きの再スナップショットがあるか確認してください。
4. `pause.rs` の walk（root分岐）: root の `entry.created` と `root_created` を walk 内で比較していません。呼び出し側の確認に依存し、`ancestors` があると省略されます。`pid==root` で不一致なら Err にしてください。
5. `TODO-2722-RESUME.md`: リポジトリ直下の作業メモで、内容が「parser新設中・未接続」と実装済みの状態に合いません。マージ前に除いてください。

**Nit**
- `snapshot.rs:60`: 重複PIDや奇数長の名前が1件あるだけで、スナップショット全体が失敗します。fail-closed なので許容できます。
- `pause.rs:1385`: `RevertThread` の Drop 内で assert すると、unwind 中に abort し得ます。
- 「子の生成時刻が親より前」という除外は、壁時計の逆行に弱い前提です。旧実装も同じです。

**読解で確認できた点（実行はしていません）**
- 境界は checked 演算で検査しています。
- `next` は 96 以上を要求し、前進が保証されます。
- 名前ポインタは返却バッファ内の offset として検証します。
- バッファは 8byte 整列で確保し、`read_unaligned` で読みます。
- バッファ拡張には上限があります。
- parity.rs の固定は、取得済み全handleで live と wait_exit を確認し、Drop で残りを回収します。