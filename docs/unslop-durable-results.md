# capture結果を保存してから保持情報を解放する

receiptが公開されても、その後のproviderの再確認やCloseが失敗することがあります。
receiptだけを成功の根拠にせず、同じgenerationの最後の結果まで保存して照合します。

## 保存する内容

内部controllerは開始前にgenerationを発行し、開始、receiptの照合値、公開直前、公開結果、provider終了後の結果を順に記録します。
generationの予約ファイルはCreateNewで作り、既存generationの再利用を拒否します。
各記録には直前の記録のハッシュを含め、欠落・別generation・未知の項目・同じ段階への異なる内容を拒否します。
記録は追記専用です。同じ番号への再試行は同じバイト列の場合だけ受け入れ、異なる内容を上書きしません。
未保存のeventは、書込みと同じバイト列の読み返しが終わるまでメモリにも保持します。
再試行中のlock取得や読取の一時失敗でもeventを捨てず、公開済みの事実を同じgenerationへ保存し直せます。
保存済み記録の読取は、全件の検証が終わってからメモリへ反映します。
最終結果の保存で読取が一時失敗しても、最後に検証できたreceiptを保ち、読取の回復後に同じ結果を再保存できます。異なるバイト列の記録は引き続き拒否します。
receiptの本文、入力本文、toolchainの実体、自由文のエラー、元pathは保存しません。
保存するのは固定理由、状態、必要なハッシュとgenerationです。

| 前提 | 操作 | 必要な結果 |
| --- | --- | --- |
| 開始記録を保存できない | captureを要求する | providerもcaptureも開始しない |
| receiptを公開した後でproviderのCloseが失敗する | 結果を保存する | 公開済みの事実とcleanup失敗を両方残す |
| 最終結果を保存できない | 同じcontrollerで再保存する | captureや公開を繰り返さず、保持した同じ結果だけを保存する |
| retained ownerが解放済みだが未保存 | Forgetする | 拒否し、ticketとreportを保持する |
| 保存と同じバイト列の読み返しが成功する | Forgetする | 同じgeneration・owner・reportの場合だけ解放する |
| 別generationの結果や異なる書込みが混ざる | 読み返す | 不一致として拒否する |
| 書込み途中または公開直前に停止する | 新しいcontrollerで復旧する | 保存済み段階を返し、captureや公開を再実行しない |
| 移管意図の保存が途中で失敗する | 同じownerへ再試行する | 予約した同じticketと意図を使う。別ownerへの切替は拒否する |
| 並行処理でownerの状態が進む | 保存結果を返す | SavedとReportを、実際に読み返した同じ記録へ結ぶ |

## 通常の呼出しへの接続

既存のcapture分岐は、内部のdurable controllerが接続されていれば、そのcontrollerを経由します。
接続はGoの型で渡すもので、JSONやpathや成功フラグから作る入口はありません。
通常constructorの実資格がそろう前に、factoryやruntimeを有効化しません。
storeもpath文字列から資格を作りません。通常callerが既に扱える保持済みdirectoryを渡し、storeは同identityのrootと親を保持します。
今回、実normal factoryへ渡す保存先の資格は新設していません。

保持入力の回収にはcontrollerのDrainAndSaveを使います。
保存失敗後はticketと結果が同じowner内に残り、再び保存できます。
取消済みcontextでは、保存が失敗しても同じflightへAbortを一度だけ要求します。この分岐でCloseやForgetは行いません。
保存前に同じcontroller・owner・ticket・flightの組合せを確認し、別ticketや別ownerへの要求、復旧したmetadataだけのcontrollerからの要求は取消先に触れず返します。
保存の読み返しに結び付いた内部proofがないForgetは拒否します。
保存APIは読み返したreportと記録のハッシュを返します。DrainAndSaveのReportはこの保存済みreportそのもので、先に取得した古い状態をSaved=trueと組み合わせません。
直接保存するretained ownerの記録は、そのownerをこのgenerationで観測した記録です。過去のcaptureがこのgenerationで開始された証拠にはしません。
captureも同じcontrollerで実施した場合は、providerの最終結果とownerが持つcapture結果の一致を確認します。
通常の移管入口では、実captureで観測したlive sessionと同じ保持バッファだけを受け入れ、ownerへの移管意図を先に保存します。
保存前にowner内でopaque ticketを予約し、同じcontrollerがowner・session・lease・ticketを保持します。
保存失敗の間に同じownerで別の登録が進んでも、その予約を置き換えません。予約中の入力は二重登録を拒否し、既存の件数上限にも含めます。
予約だけで移管が終わっていない場合は、RetryRetainedもtransfer_pendingを返し、回収やForgetは始めません。元slotを渡して同じownerへのRetainを再試行します。
既存WithHeldはcallback終了後にleaseを閉じるため、実unreaped workerの移管へ使うにはnative側の転送可能なlease経路が必要です。
この未接続を別のsessionや同じハッシュのバッファで代用しません。

## 再起動と確認の限界

復旧した結果は監査用metadataです。保存済みの成功結果も実行許可やインストール許可にはなりません。
未完了generationと未回収ownerは、再起動後も未確認として返します。
プロセス内のticketやOSハンドルをファイルから再生成したり、PIDだけで他のプロセスを回収したりはしません。
公開意図は保存済みでも公開結果が未保存なら、公開状態はunknownとして返します。
未完了の書込みは保存済みの最後の段階まで読み返し、同じプロセスがまだ持つ原結果でだけ再保存できます。失われた未保存結果をreceiptから推測しません。
実native reaper、通常factory、保存先の資格を満たす接続は別工程です。

今回のテストには合成captureと所有する一時ディレクトリを使います。
テストはソースを用意した段階で、コンパイルと実行結果は固定候補のWindows CIで確認します。
