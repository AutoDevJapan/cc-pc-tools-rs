# Windows検査のキャッシュ用ポート

[Issue318](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/318)は、PR149の正式検査前に
sccacheがWindowsの予約済みポートで停止した不具合を扱う。
初回資源では57272が予約範囲57232〜57331に入り、エラー10013だった。
SDK準備は成功したが、GitHub jobへの割当・製品の検査は未開始である。

## 入力・成功条件

- 既存の隔離worker、固定sccache実行物、Job Object、専用キャッシュを使う。
- ポートをnonceから予測せず、サーバー自身がport 0へbindしてWindowsに割り当てさせる。
  probe後の別bind、OS予約やFirewallの変更、共有サーバーの利用はしない。
- カーネルのTCP表から、起動した正確な子PIDが127.0.0.1でLISTENする1ポートを得る。
  別PID、外向きアドレス、非LISTEN、0、複数候補、壊れた表は採用しない。
- 確認した番号を同じworkerのメモリ内Planへ保持し、runnerと子ツールの環境へ渡す。
  JSON入力・ディスク上Planから番号を受け取らず、登録credentialも記録しない。
- 設定とcached-configの参照はその資源内へ明示する。既存個人設定は使わない。
- 15秒の起動上限、foreground、Job所属確認、終了回収を維持する。

## 検証

旧実装の回帰検査は「bind前に53690を予測した」で失敗した。
境界検査とWindowsの実TCP表による検査を通す。
明示した新規safe-delete資源だけで、インストール済みsccache 0.18.0を起動し、
実クライアントの`--show-stats`接続と終了後の待受不在を確認する。
このsmokeはRustビルド・共有service・runner登録・製品jobを実行しない。
最初のsmokeでは隔離AppDataディレクトリが無く設定初期化に失敗した。
試験資源へ必要な空ディレクトリを作った最終smokeは成功した。

関連Go moduleのrace/vet、別モデルレビュー、同HEADの必要正式Windows/Go解析を確認して
通常mergeする。変更の無い製品のRust検査を代用して重ねない。
PR149の正式受入、queue復旧、通常イベントでの再供給と回収は親2962の未完了条件である。

参照: [sccache 0.18のbind実装](https://github.com/mozilla/sccache/blob/v0.18.0/src/server.rs)、
[設定の参照先](https://github.com/mozilla/sccache/blob/v0.18.0/src/config.rs)。
