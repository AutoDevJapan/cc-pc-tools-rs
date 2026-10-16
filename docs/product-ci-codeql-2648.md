# 製品用CodeQL処理の再開記録（元2648）

## 保存したソース

PR174のブランチは `scheduler/todo-2648-product-codeql`。
初版は `6a00cddf594e2a7f0e7c52f7691a5f88f10bf396`。
既にマージ・配備したPR173を繰り返さない。
独立したGo module `tools/product-ci` だけを変更する。
既存Linux bridge、runner、guard、原製品フォルダは変更しない。

追加差分は、ジョブの開始・終了時に独立した認証付き受付へ問い合わせ、
待機中に原headが進んだ場合も拒否する。受付は同repo/run/attempt/SHAに限定し、
コマンド実行機能やCargo機能を公開しない。HTTPの代理設定と転送を使わない。
hookを公式runnerのアプリケーションディレクトリの外へ置く。
実コンテナの作成成否が不明な場合は、後続のsmokeを作らない。

## 実測と未完了を分ける

初版ではWindowsのGo unit/vet、Windows/Linuxのビルドが成功した。
初版の実SDKコンテナでは、正常・中止の2経路で実childを観測し、
停止したPIDが0であることと回収を確認した。これは当時の初版の結果である。

2026-10-15朝の再開後、GitHub CLIはHTTP401/Bad credentials。
既存GitHub連携では実repo1406818713、AutoDevJapan/private/adminを確認できる。
PR174はopen、headは上記初版。Build38615445941はfailure。
nextestのstep11がin_progressの記録で終わり、doc/Clippyはpending。
ジョブログ116029235362の取得はサーバー側の404/BlobNotFoundだった。
OS停止原因や失敗したテストを、この不足したログから推定しない。
CodeQL38615445496、Go38615445521は取得時点でqueued。
同じ失敗runを盲目に再実行せず、既存runnerを止めない。

新差分のunit/vetは成功した。再開後の実Docker検査はcreate/inspectが応答せず、
期限切れとなった。別の有限read-only照会は
`Docker Desktop is unable to start` を返した。新差分の実guest受付を
成功としない。2つの作成意図を所有証拠へ保持し、daemon復帰後、
同じ名前・instance・image・所有labelで存在と回収を照合する。
元2488の稼働構成・本体・コンテナを並行修正しない。

別モデルの有限レビューは既存ToDo2660に保存済み。準備段階のtool_failedで
まだ実レビューは無い。Must0として扱わない。同2648や同じレビューを
新規ToDo・別担当・独自監視へ複製しない。

## 再開する順番

1. 元2648の最新noteと元2488の稼働証拠を読み、現物のブランチを保つ。
2. 既存2660へ最新ソースの有限レビューを渡し、実応答のMustを直す。
3. 既存の認証・Docker復帰を照合し、不確実なowned instanceを先に確認する。
4. 新差分の実guest受付・正常/中止回収、最新sourceの必要CIを確認する。
5. 通常マージ・独立Go処理の配備後、優先順に原候補の実CodeQLを追う。
6. 原受入のRust/Windows/Lua/DB等を省かず、実log/hash/cleanupを原ToDoへ配送する。

原4候補の全検査、CodeQLの正式結果、マージと配備は未完了。
runner登録、route変更、手動success statusは実行していない。
本人PC操作、新費用、AI APIキー、GPU、Discord投稿はこの続行で行わない。
