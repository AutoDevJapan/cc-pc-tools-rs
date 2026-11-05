# データ移行CLI

すべての結果は標準出力へJSON集計1件で返します。タイトル・本文・参照パス・認証値・サーバー応答本文は表示しません。失敗時の終了コードは1です。

```powershell
todo-cloud-migrate export --home <既存ホーム> --out <新しい出力ディレクトリ>
todo-cloud-migrate backup --home <todoHome> --out <専用backupRoot> --keep 3
todo-cloud-migrate verify --local --export <バックアップ世代のディレクトリ>
todo-cloud-migrate upload --config <DPAPI設定ファイル> --export <出力ディレクトリ> --assets-only
todo-cloud-migrate upload --config <DPAPI設定ファイル> --export <出力ディレクトリ>
todo-cloud-migrate verify --config <DPAPI設定ファイル> --export <出力ディレクトリ>
```

exportは既存の`todo.lock`のoffset 0、length 1をWindowsのLockFileExで排他し、`todo.json`を原bytesのまま`snapshot.json`へ保存します。rootは原bytesで64MiBまで、各assetは128MiBまで扱います。出力先の親ディレクトリは事前に用意し、出力先自体には未作成のパスを指定してください。履歴参照のidentity・digest・bytes・entriesを照合し、仕様ファイルとともに`assets/<SHA256>`へ保存します。manifestの最後の書込みと出力ディレクトリの公開までロックし、export終了時に解放します。

`spec`と`spec_history`の文字列参照を集計します。指定ホーム内の通常ファイルだけを読み込み、リンク・外部パス・除外対象は`unverified`として元のパスを保持します。外部パスの存在確認や探索は行いません。ホーム内の欠落は`missing`、取得失敗は`unavailable`に分け、`exists`が未確認ならnullです。履歴ファイルの欠落や不一致はexportを失敗させます。元JSONの未知フィールド・型・大文字小文字・ステータスは変更しません。

uploadはロックを取得しません。ローカル出力全体を検証してから、履歴・仕様ファイルを全aliasでPUTし、GETでハッシュとaliasを照合します。その後、rootを4MiB以下のchunkで送ってFinalizeします。`--assets-only`はrootのchunk送信もFinalizeも行わず、最終切替前の先行転送に使用できます。

assetの転送と照合は最大8workerで並列に実行します。同じSHAのaliasは1workerがmanifest内の順番で扱い、全aliasと原bytesのハッシュを確認したassetだけを`verifiedBlobs`へ加算します。manifestの配列や集計値は変更しません。同時処理には64MiBの予約枠を設け、各assetは原bytesのサイズに応じて最低1MiBを予約します。64MiB以上のassetは予約枠全体を使用し、最大128MiBのものも含めて単独で処理します。この予約量はプロセス全体のメモリ上限ではありません。

最初のエラーで他workerをキャンセルし、処理中のworkerをすべて終了させてからJSON結果を返します。部分的に保存されたblobやaliasは、同じexportによる再実行で内容を照合しながら再送できます。失敗したasset処理からrootの転送へは進まず、成功時も全workerの終了後にrootのchunkとFinalizeを直列で実行します。verifyのasset照合も同じ並列数と予約量に従います。

Finalizeは空の新規DBだけで成功します。同一root・元パスに対応する確定済みreceiptは再利用でき、途中のchunkやassetの再送も同じ内容に限り可能です。別rootで既存台帳を上書きしません。最終切替のfreeze・最新export・既存の書込み元の切替は呼出し側が担当します。

verifyはGETだけを実行し、root原bytes、各assetのSHA256とbyte数、元パスのalias、現在の`/state`のJSON値・型・キーを照合します。元exportから台帳が更新されていれば`remote_state_mismatch`を返します。assets転送・export成功だけで本番の切替完了とは扱いません。

ローカル出力には元データと参照パスが含まれます。公開リポジトリや共有ログへ追加せず、担当者が指定した保存先で管理してください。

backupは既存Exportを使い、`todo.json`の原bytes・manifest・履歴/specの原bytesとSHAを専用rootへ保存する有限コマンドです。`--keep`の既定値は3世代、指定値は1以上です。元ホームと保存先は、親子関係も含めて別の通常ディレクトリにしてください。親ディレクトリのリンクやjunctionも受け付けません。原本や既存世代を書き換えず、JSTの日付・世代番号・乱数を組み合わせた一意のディレクトリを作ります。同日かつ直近の有効世代とroot・asset・未取得の参照状態がすべて一致する場合は、その世代を再利用します。同日でも内容が変わった場合や、さらに前の内容へ戻った場合は新世代です。

専用rootの所有印は`.todo-cloud-backup-root.json`、各世代の所有・元ホーム・日時・世代・manifest SHAの記録は`backup.json`です。整理するのは両方が対応し、manifest・root・全assetのSHAが有効で、予定したメンバーだけが入っている世代に限ります。元ホーム・別所有のexport・所有印がないexport・破損・未知ファイル・symlink/junction/hardlinkは整理対象にしません。削除前に絶対パスの包含、保持したディレクトリ実体、ファイル実体とSHAを再確認し、列挙したファイルだけを削除します。ディレクトリの再帰削除は使いません。

JSONの`backup`には作成/再利用、保持/削除した世代数とbytes、保護して残した件数を返します。bytesは検証済みファイル長の合計で、ディスク空き容量の増加量ではありません。失敗は終了コード1と固定エラーコードで返し、途中失敗なら`inventoryComplete:false`とします。`created:true`の後に保持整理が失敗した場合、新しく検証できた世代は残ります。古い世代の部分削除や未知メンバーを、整理完了として数えません。部分物や所有未確認のものは自動で再帰削除せず、同じ担当の記録で状態を照合してください。

Windowsの既存定期実行から、同じ本人アカウント・Hiddenで上記backupを1日1回起動します。保存先・実行ファイルは固定した絶対パスとし、重複時は新しいインスタンスを起動しない設定にします。CLIにも専用rootの1バイト排他があり、競合時は`backup_lock_busy`を返します。終了コードとJSONを既存の運用記録で確認し、新しい監視ループや常駐処理は作りません。定期タスクの登録・初回実データ実行・翌日の実起動確認は運用担当の作業です。追加のクラウド保管・有料volume・APIキーを使いません。

復旧は次の順序で進めます。対象は所有を照合済みの既存Railwayプロジェクトの同じPrdサービスと既存volumeに限ります。具体的な対象ID・URL・実行受付はprivateの運用記録で保持し、公開文書には載せません。

1. 復旧する世代と作成時点を固定し、所有印・`backup.json`の元ホームが対象PCの`<todoHome>/todo.json`と一致することを確認します。`verify --local --export <世代>`でrootと全assetのSHA・bytes・履歴件数を確認します。このモードは通信や認証設定を使いません。破損した世代は使わず、別の有効世代を確認します。`missing`・`unavailable`・`unverified`の参照はmanifestに残したままとし、空本文を作ったり外部パスを探索したりしません。
2. 既存の同期タスク/watchと、CLI・Scheduler・Consoleなど同じ正本を書く処理を停止・排出し、PCにもクラウドにも書込みがない境界を確保します。元DBと最新PC cacheを内容・SHA・時点付きで保全します。所有する既存保存領域の範囲で空の新規DBを起動し、別環境や別所有者のDBへ切り替えません。既存DBを消して空にする操作や、追加料金の保存領域の作成はこの手順に含みません。
3. 所有と接続先を確認した既存DPAPI設定で`upload --config <設定> --export <世代>`を実行し、Finalizeの初回import revisionが1であることを確認します。続けて通常の`verify --config <設定> --export <世代>`でクラウド上のraw asset SHA、全alias、rootのJSON値・型・caseを照合します。空でないDBへの別rootの上書きは拒否されます。同期停止はこの確認が済んでも継続します。
4. 切替前の`todo.remote.json`と`todo.json.remote-revision`を、内容・SHAと組を保って別の一意な退避先へ移し、退避先のbytesが一致することを確認します。DPAPI設定の秘密値を出力せず、退避したmarkerを戻して旧DBへ自動復帰しないでください。markerだけ外して旧sidecarを残した状態は正常な切替開始状態ではありません。
5. PCの`todo.json`が選んだsnapshotと同じraw/SHAで、未反映の新しい変更がないことを確認します。不一致なら両方を保持して差分の出所を照合します。採用する復旧時点と必要な差分を確定するまでCutoverへ渡しません。旧cacheで復旧先を無条件に上書きしたり、revision/stateIdの値を書き換えて一致させたりしないでください。
6. 同期停止と旧状態の退避を保ったまま、検証済みのbridgeで`todo-cloud-bridge cutover --config <DPAPI設定> --bridge <bridge絶対パス> --expected-cloud-revision 1`を実行します。Cutoverは元の1バイトlockを保持し、PCの原bytesを別保存してから隔離したcacheで照合し、cache/sidecarを保存した後にmarkerを有効化します。クラウドがすでに別revisionへ進んでいたら停止し、その変更を照合します。失敗を理由に旧cacheを強制Commitしません。
7. Cutover結果の`activated:true`・lease解放、元rawの保存、markerが正しいbridge/DPAPI設定のパスを指すこと、sidecarのSHAが実cacheと一致し、revisionと空でないstateIdがクラウドの組に一致することを確認します。原snapshotとクラウド保存assetのraw SHAは保持し、cacheの表示順や空白の違いはJSON値・型・caseで照合します。その後に既存sync/watchを再開し、通常の読取りと許可済み更新が差分保存されることを確認します。
8. 同じrevisionで異なるstateId、過去revision、予期しない別分岐、応答喪失によるCommit結果未確定があれば、その時点のDB・cache・marker・sidecar・operation receiptを保持して照合します。正常な初回importと同一と決めつけず、書込みの自動再開や旧cacheによる強制上書きを行いません。lease解放だけ未確認なら、有効化済みという実結果を保持して解放状態を追い、切替全体を無かったことにしません。

このbackupの復旧単位は元rootと、Exportで取得できた履歴/specです。クラウドのRegistryやagent割当などrootから独立した記録は含みません。それらは同じ担当が保持している既存の記録と復旧時点を照合し、root復旧の成功だけで全データが戻ったとは扱いません。ここに記載した実運用の停止・退避・DB復旧・Cutover・定期実行設定は、このCLIが自動実行する処理とは別です。
