# GoDD Unslopの配布と文体評価を再開するための契約

配布物と評価入力の出所を確認し、元の用途で扱うための専用処理を追加する。
一般のパス分類や保護台帳は変更せず、呼出し元が指定したパス・ハッシュ・成功フラグを許可の根拠にしない。

## 今回の実装と、残っている接続

実装したのは、ビルドの記録を確認する処理、配布準備の処理、文体2例の入力を取り出す処理、履歴の存在を確認する処理である。
実ファイルを保持してビルドするproviderと共通CLIへの接続は、まだ行っていない。
合成データのテストが通っても、インストールや独立評価が済んだことにはしない。

旧リリースには、ビルドの正常終了と、同じ実行物でCLI・MCPが動いた記録がある。
一方、完全なビルド入力とCargo JSONは取得していなかった。
そのため、旧リリース用の入口は `missing_compiled_input_provenance` を返し、実行物を開かない。
既知のハッシュ、作者が書いた要約、現在のソースの走査結果を、当時の完全な入力記録の代わりに使わない。

必要な証拠を取得する正式ビルドは、レビュー済みの正常なproducerから開始する。
目的のない再実行を避けることと、配布に必要な未確認条件を満たすビルドを省くことは分けて扱う。
旧リリースの成功記録と、新しく作る成果物の記録は、それぞれ保持する。

## ビルド開始前から記録する

`captureUnslopRelease` は、通常controllerが結び付けた `unslopProducerSession` だけを受け取る。
このinterfaceを、JSONや環境変数から作った証拠の読込口にしない。
新しいビルドの対象は、固定したUnslopのソースと専用の新しいnamespaceである。
ソースを変更する場合は、固定したcommit・tree・入力一覧もレビューし直す。

| 段階 | 正常なproviderが行うこと | 不足した場合 |
| --- | --- | --- |
| `Before` | 固定commitの10ファイルをGit blobと保持バッファで照合する。埋込みSkillも含める。Cargo.lockで確認した依存、使用する設定、toolchain、namespaceのidentityも記録する | ビルドを始めない |
| `Build` | 共有ゲートを使い、固定コマンドを1回だけ実行する。新namespace、オフラインの依存、Cargo JSON、元の実call、子プロセスの開始・Job割当・終了・回収を結ぶ | 成果物を開かない |
| `After` | 同じ保持ファイルとnamespaceを再確認する。ファイルの追加、内容・identity・設定・依存の変化を確認する | 成果物を配布しない |
| `WithArtifact` | Cargoが報告した新しい実行物と、その許されたaliasを保持する。後続へ渡すのは、確認した同じバッファだけにする | 配布記録を発行しない |
| `Acknowledge` / `Close` | producerとのACK、終了回収、入力snapshotや設定の後片付けを確認する | 未完了の生成物を保持し、理由を返す |
| `stage` / `Commit` | 同じバッファをCreateNewの非公開pendingへ書く。最後にexportを再保持し、内容・identity・ACLを照合してから、上書きせず正式なreceiptを発行する | pendingを失敗のまま保持する |

Cargo JSONでは、対象のbinと固定された出力先、`fresh=false`、最後の `build-finished` の成功を確認する。
出力の重複、途中で切れたJSON、別の出力先、過去のFresh出力は受け入れない。
保持処理のcallbackが呼ばれなかった場合、二度呼ばれた場合、callbackの後で保持処理が失敗した場合も、正式なreceiptは発行しない。

`unslopCapturedManifest` は、入力一覧の構造と前後の同一性を確認する処理である。
それだけで、実ファイルを読み取ったことや、コンパイラが使った全入力を確認したことにはしない。
実際のblob・依存checksum・全入力集合・保持ハンドルの確認は、native providerの責任として残る。

## 元のインストール先へ反映する

反映先は、既存のUnslopのインストール先を維持する。
新しい成果物の正常receipt、同じ実行物でのCLI・MCP受入、現在の旧実行物のidentityがそろってから、既存の更新controllerへ渡す。
旧ハッシュを知っていることだけで、現在の読取資格があるとは扱わない。

配布準備では、旧実行物のOwner・Group・DACLを保持し、同じ親にある固定stage・backupだけをCreateNewで作る。
同じハッシュでもidentityが変わったもの、既存名との衝突、途中の取消、部分書込は失敗として残す。
準備の成功と、インストール先の切替・起動確認の成功は別々に記録する。
既存のcontrollerを弱めたり、Cargoのaliasを削除して一般guardを通したりしない。

## 元の文体2例を評価側へ渡す

元の入力と応答について、case ID、4つの本文ハッシュ、使用した実行物、正常終了、元のthread・turn・command item、ファイルidentityを正常providerで照合する。
privateなthread IDや保管パスは公開sourceへ埋め込まず、controller側の元用途に結び付いた参照として保持する。
packet readerへ渡すのは、固定された4つのleaf名だけであり、任意のパスを受け取る入口は設けない。

`inspectUnslopEvaluationOrigin` が使うのは、正常な履歴APIの `thread/read(includeTurns=false)` と `thread/turns/list(itemsView=notLoaded)` だけである。
本文は読まない。
正しいthreadと完了turnが確認できても、元の実行物・終了コード・入力と応答のhash・identityの組が欠けていれば、`original_producer_binding_unavailable` を返す。
履歴metadataの取得を、元packetを読む許可や読了へ置き換えない。

packetを扱える条件が成立した場合だけ、確認した同じ保持バッファから原文・今回のcontext・生成本文を取り出す。
未知の実行情報、相手の設定、認証、思考内容は評価側へ返さない。
1件でも失敗した場合は、途中までの本文も返さない。
readerは評価の合否を作らず、作者の判定を独立評価へコピーしない。
元の2例を再生成しない。

## CLIと接続先

`runUnslopPurpose` は、以下の固定flagを処理する。
正常なproviderが接続されていない場合も、理由をJSONで返して終了コード1とする。
余分な引数や未知のflagは、本文を出さず終了コード2とする。

| flag | 接続がない場合の理由 |
| --- | --- |
| `--capture-unslop-release` | `missing_producer_provenance` |
| `--verify-unslop-update` / `--prepare-unslop-update` | `missing_compiled_input_provenance` |
| `--inspect-unslop-evaluation-origin` | `normal_history_provider_unavailable` |
| `--read-unslop-evaluation-packet` | `unslop_origin_provider_unavailable` |

共通dispatcherへの専用routingは、そのファイルの既存所有担当が行う。
routingを追加しただけでは、出所を確認するfactoryが成立したことにはしない。

既存の `softwareBuildSourceMatches`、`runSoftwareBuildWorker`、`withSoftwareBuildOutput`、`softwareBuildCompletionMatches`、`writeSoftwareBuildReceiptContext` を、native側の保持・ACK・終了回収・非上書きreceipt発行の接続元として使う。
既存処理の製品固有のパスやsource条件を、Unslopへそのまま流用しない。
履歴の正常な取得経路は別に接続し、原tupleが取得不能なら成功へ変えない。

## 受入の区分

合成fixtureでは、正常な組、出所不足、前後で変わる入力・identity、不正なCargo JSON、子の失敗・回収不足、保持処理の失敗、取消、部分作成、後片付け失敗、CLIへの理由伝達を確認する。
実際の配布・評価の完了には、さらに次が必要である。

- 正常なproducerを接続し、開始前から証拠を取得した同じ成果物を正式receiptへ結ぶ。
- 同じ成果物でCLI・MCPを確認し、元のインストール先へ反映して起動を確認する。
- 元packetのtupleを正常providerで照合し、評価側が読んだ本文と同じ入力に対する独立結論を得る。

provider未接続のテスト成功を、これらの完了として報告しない。
