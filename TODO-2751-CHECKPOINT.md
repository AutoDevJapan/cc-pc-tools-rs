# PR148 Linux helperの再開記録

Linux用helperの実装と、認証情報を使わない検証は終了した。別モデルのレビュー、Mustの修正、親2736への最終配送は未完了。

## 固定した成果

- worktree: `D:/CC/Work/unattended-operations-20261013-wt/todo-2751-pr148-linux-runner-cargo`
- branch: `scheduler/todo-2751-pr148-linux-runner-cargo`。切替はしていない。
- 最初の保存: `52d2ff2`。修正後のコード: `0377dd0f66bfb69b58331cb497625b4f5e8b6ae5`。
- helperのmodule tree: `066dab0b76d6194121e092f0f4c14e9c73b541b9`。
- 元2736の計画は`822b4cb`から同じファイルを取り込み、既存のprobeも取り込んだ。共有resident bridgeと原製品には変更していない。
- `pr148-linux <pin-json> <new-owned-receipt-dir> <Linux-helper> <official-pwsh-tar>`を親向けに実装した。この子工程では呼び出していない。

## 実装した制約

受け付ける製品は元指定のrepo1392152644 / PR148のみ。元75aの固定tree/base/branchを保持する。実行用pinは元75aとは異なる新HEAD/treeを明示する。新commitの親が元75aだけであること、全indexのpath/type/modeと変更範囲、三workflowの生成案と全bytes一致をAPIで検証する。原HEADへ戻す操作や任意の最新sourceを受け付ける経路はない。

同branchの全OSを含む`workflow_dispatch`とrun/attempt/Linux jobを固定する。専用labelだけの公式ephemeral runnerを、新しい非root・init付き・readonly containerへ配置する。hostのbind/socket/GPUは渡さない。PowerShell assetは親が確認したSHAで照合し、stdinで新tmpfsへ渡す。Cargo/Rustupには新しいtmpfs homeを使い、guestのtargetは原scenarioが参照する`tools/target`のまま。

有限callbackは元workflowのCargo引数集合だけを受け、repo/run/attempt/source/nonceと実jobの割当を毎回照合する。hostのcommand/path/envをHTTPから受け付けない。ホストの共有Cargo gateが追加したtargetと並列数は検証後、guestの固定targetへ対応付ける。fmtは元gate自身の非ビルド扱いを維持する。

Landlockでworkerから本物のCargo/toolchainの実行・読取とhelper/planの改変を拒否する。実Cargoの全子孫にはseccompを継承させ、プロセス群からの離脱と監督プロセスへの操作を拒否する。入れ子の直接Cargoも、既に取得した同じpermitとプロセス群の内側に残る。callbackの再入・再送は拒否する。

guest supervisorが実Cargoを始める前のhandshakeでPGIDをhostへ渡す。正常/取消しとも群を排出し、hostが別のexecでkernelの群不在を確認する。単なるCLI exitやguestのファイルだけでgateを解放しない。群の確認が不能ならowned namespaceを止める。それも確認できない異常時は、証拠を残してgateを保持する復旧待ちとなる。この異常経路を新しい常駐サービスの成功とは扱わない。

source取得/全blob/index tree/実HEADの検証には既存のmaterializerを使う。start/complete hookとhostの前後で、job/source/workflow/OS/checkout認証不在を確認する。runnerのexitから正式CIの成功を登録するAPIはない。

## 検査済みのこと

同じ最終コードの検査は繰り返さない。

- Go unit: subtestを含む119 pass、3 skip、failure 0。
- Go vet: exit 0。
- Windows buildとLinux amd64 cross build: exit 0。
- Windows binary SHA256: `7CE9A8A99D8A4CED2915C76FB477085CBD22710E5DB1C3197799E0C26CA9F8F4`。
- Linux binary SHA256: `DE3DC6578DEA5094B7140CBAC754C6FF82CA1AF0F2C6D020EA07D9B5857C4EC3`。
- 新namespace、実guest HTTP callback、実host共有gate、合成Rust crateを通した正常/取消しfixture: 両方pass。正常exit 0、取消しexit 125。実子プロセスを観測後、gate解放時に不在を確認した。
- workerがSDKのCargoを直接実行すること、コピーすること、toolchainを読むこと、proc経由で読むこと、helper/planを改変することは実kernelで拒否された。
- これはSDK既存toolchainを使ったfixture。Rust 1.95.0による原製品検査、全scenario、公式runner登録、製品sourceの取得/展開、workflow書込/dispatchは実行していない。正式jobの成功は未確認。

最終fixtureは7.203秒でpass。unit/vet/buildと同じコードをその後0377dd0へcommitした。build時点のGit metadataは未commit差分ありだが、実行したGo source bytesは保存後と同じ。新しいcommit名だけを理由に再検査しない。

## 失敗の経緯と自分の回収

最初のcreateではDocker JSON配列の切り出しを誤り、所有照合を拒否した。修正した。次の合成Rust文字列へ、回収用Go testを誤って混ぜたためfixture compileが失敗した。この回収testは除去した。回収を早く実行して自分の実行中fixtureを1個止めてしまったため、そのnamespaceの停止・PID0・削除を独立確認した後、ハンドルと開始時刻を固定した自分のcallbackだけを終了した。他担当の資源には触れていない。

標準入力をGoのコピーgoroutine経由で渡すと、正常終了後もEOFまで待つ問題も修正した。現在は元のpipe handleを渡す。後続の直接gate fixtureと、実HTTP fixtureの正常/取消しは成功した。

作成した全10個のnamespaceは、停止・PID0・削除を確認済み。最終Docker照会は当該cc.todo=2736のcontainer 0件。新runner登録、Chrome、GPU、Civ6、service、host製品checkoutは作っていない。

原証拠は一時rootの所有ID `505512cad16f3e4ab1aafd0dc86cc3e1`で作成した。67個・108300 bytesのJSON/logを、永続保管先`.ci-work/todo2751-evidence-20261017`へコピーし、全SHA256を照合した。`manifest.private.json`のSHA256は`DF7EED005B9A11450255395BD7CDB5DB3778044AF1C8B319875882FEFC1B8E62`。失敗時のJSON/logも保管した。イベント履歴の一部は元jobのevents.jsonlにも残る。

一時rootの削除はまだ行っていない。永続証拠を確認後、safe-deleteのplan→別通常削除→finishを実行する。拒否時は同操作を繰り返さない。永続保管先は原証拠として保持する。

## 残り

1. 固定0377dd0の全変更と既存有限adapterを、通常共有ToDoへ登録した独立Sonnet reviewerに確認してもらう。登録番号と原応答はここへ追記する。直接子Codexは起動しない。
2. Mustを価値で判定して直し、変更した範囲だけ検査・追加レビューする。製品source/正式実行は親2736に残す。
3. helper branchをpushし、実HEAD/module tree・Go/fixture実結果・別モデル原応答・残留/未達を元2736へnote追記配送する。親2736/2640の完了を待つ逆依存を作らない。
4. 所有rootを通常回収し、正規scheduler checkpointを保存する。レビューだけを待って終了するなら`todo.exe wait 2751 --on <review-id>`を必ず記録する。

2026-10-17 10:38台の再開では、元jobのitem_78がexit0で完了し、Go119 pass/3 skip/failure0とcontainer不在を確認した記録を先に読み直した。完了済み検査は再実行していない。
