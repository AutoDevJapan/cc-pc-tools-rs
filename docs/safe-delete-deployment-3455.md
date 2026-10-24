# safe-delete 1本の更新（ToDo3455）

元2962のPR385は通常マージ `6c57c3e78a799b1e253ab15944dd5e9899cae55e` 済み。
この工程は既存の `safe-delete.exe` 1本だけを更新する。guard、queue、helper、設定、
台帳、製品jobは更新しない。元3258のguard配布と、この工程の完了を区別する。

## 仕様と受入条件

- 最新mainの独立した公開Git取得からsafe-deleteをビルドする。実source、tree、
  VCS、依存、SHAと原ビルド記録を保存し、旧候補のSHAを付け替えない。
- 旧版は元3250のPR351原配布記録を別の独立anchorにする。原記録SHA
  `aa5f0f4abcf567be59751eb8b71a6a99fa0f12e34b8d1a7289bece0dd3376086`、
  記録上の旧版SHA `c77e78ee0755df35d36e641a3505c87cd25313aa8110046f18e98da4c463332f`。
  記録上の値と現在一致の実測を区別する。原byteは変更しない。
- 非配布readerは既存のmodel runtime engineを維持し、固定データだけをoverlayする。
  callerからmanifest、hash、root、grantを受け取らない。元client境界、固定source、
  proof、NTFS identityと最終再照合を保つ。一般データへの許可を発行しない。
- 関連テストでリンク、別名、改変、未知source、任意manifest、別target、旧版不一致を
  拒否する。別モデルmaxレビューと正式CI・通常マージのsourceを照合する。
- 既存AdminBrokerと固定installerでbyte排他、NTFS identity、CAS、元owner/group/DACL、
  旧版backupを保ち1本だけ更新する。失敗時は原結果を保存し、盲目的に戻さない。
- 配置後の用途資格、SHA、source、build、ACL、backup、非破壊helpを読み返す。
  小さな自分のfixture以外の清掃を実行しない。元2962/3258へ実コマンドと結果を返す。
- guardのbyte/sourceと既存retain5、JPEG、56資産の結果が変わらないことを確認する。

## 実測した区切り

2026-10-24の開始時mainは `0e16e81798ff4463c6ae6bcf6c4c57a695d9d13c`。
通常guardはsafe-deleteを `unverified` と返し、既存の
`--verify-model-runtime-update` はexit 1、`qualified:false` だった（更新前Red）。
元3369の指定3資料と元3250の原配布記録は、現guardによる個別clear確認後に参照した。
原記録SHAは上記と一致。現在実行物のbyteはまだ読んでいない。

元3344/3369は完了、3258のguard配布段階は完了。3258/2962はそれぞれ既存依存待ちで、
safe-deleteの別writerは一覧にない。GitHub OAuthの復旧3454は既存担当が進めており、
重複起動せず、復旧後の通常APIで実ownerと正式チェックを照合する。
この時点では独立公開checkout、固定reader、レビュー、実更新、読み返しは未実施だった。

最新mainの独立公開checkoutから候補を一度ビルドし、safe-deleteのGoテストとvetが成功。
候補SHAは `d5be8270541c177ee811dc5ecee2f1c117590e6aa132e942598eb096596286ef`、
実VCS revisionは上記main、`vcs.modified=false`。原build receiptを常設source内に保存した。
検証専用readerは既存関数を変更せず、2つの固定データ表だけをGo overlayで結合した。
readerはsource、原旧receipt、候補receipt、候補byte、元client境界、物理pathを照合する構成にした。
正常用途の実測はexit 1、`qualified:false` だった。この単一の拒否値からは、各照合の
成功、現物と期待旧SHA・新候補SHAの一致／不一致、正確な失敗段階を判定できない。
未知の現在SHAの直接計算・出力、対象のコピー、新baselineへの採用は行っていない。

現物metadataは5,077,504 byte、更新UTC `2026-10-23T14:44:11Z`。
元3369の観測（5,009,920 byte、PR351原記録）とは変化している。
後続PR399/ToDo3451の完了を正規ToDoとGitHub連携で確認した。
PR399は `702c35757df244b1ae46730a6b434d1272ad34f9` から通常マージ
`67652b0a908d79096356625c690af85c716f1786` 済み。これだけでは現物byteの証拠にしない。
その原配布記録を確認してから新たなbaselineを結ぶ。元PR351の原本とpinは保持する。

2026-10-24、既存GitHub連携から実owner `AutoDevJapan` とrepo ID `1406818713` を
確認できた。PCの絶対パスのGitHub CLIは同じ時点でHTTP403を返した。
3454による既存OAuth復旧と、連携による正常APIの観測を区別する。

その後3454が正常完了し、本担当でも同じ絶対CLIの通常API成功を確認した。
後続原配布記録の有限な読取checkは3466へ登録した。実装・配布は3455が保持する。
関係する旧版不一致、リンク、別名、改変、未知source、任意manifest、別targetと
既存のNTFS再照合を含むテストは49件成功、失敗0・skip 0。
2つの固定data表以外は全宣言のAST比較でも一致した。

専用の非配布installerを準備し、自分の所有fixtureだけを既存AdminBrokerでテストした。
元owner/group/DACL、旧byteのbackup、配置先NTFS identityを保つ正常置換と、
旧版不一致、リンク、既存backup、並行writer、最後の照合失敗、path差替えの拒否が成功。
旧backupは検証済みbyte copyであり、元inodeを移動したと偽らない。
継承フラグと共有handleに関するfixture上の失敗を直し、失敗原ログも保持した。
Windowsの[handleを使うACL API](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-setsecurityinfo)と
[handleによるrename](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info)を使う。
原byteのdeny-write handleとbyte排他を保ち、現物へ適用する前に別モデルmaxで確認する。

実物に対するinstallerのvalidateはexit 1で拒否し、実物の置換は0。
後続の独立原証拠が未結合なので、実配置や用途資格のGreenはまだ達成していない。
PR399のsafe-delete sourceと候補の最新mainは差分0で、正式CIの同source結果を照合する。

再開時は作業場所の `.ci-work/3455-resume.private.json` と原receiptを照合する。
常設のsource、原receipt、reader、controller、backupは次回更新にも必要な証拠として保持する。

## 原調査とレビューの受領後（2026-10-24）

3466の実Sonnet5.5/maxによる有限checkは完了した。現在byteに結べる後続3451の
原build/配置記録は、参照できた範囲では見つからなかった。存在しないとの断定ではない。
元の実行担当から実在する原path/SHA・原コマンド・Go出力を正常配送してもらうよう、
2962/3258の通常noteへ依頼した。同じ場所を再走査する担当は増やしていない。
未知の現在値を認める受入条件の変更は採用していない。

3469の実Opus5.5/maxは固定8fileを全文読み、SHAとbyte数が一致し、Must0だった。
読取レビューの完了と実更新の完了は区別する。原応答はそのまま保持した。
置換競合や中断後の確認に関するSuggestionを受け、元v1を保全したままv2を作成した。

- 新byteがあるだけで再配布完了にせず、直前のcomplete記録、元backupとのACL、
  配置先とbackupのNTFS identityを照合する。stoppedや記録欠落は拒否する。
- 最後の照合後に旧streamが別名へ動かされた場合も成功扱いにしない。
- security descriptorをpinned handleから読み、ネイティブ呼出し中のGCによる解放を防ぐ。
- 一時名を実行されない拡張子にし、別directoryへのbackup移動と停止後の各拒否をテストした。

v2の既存AdminBroker上の関連テストは12個、subtest込み33件成功、失敗0。
vetも成功し、実際の公開sourceに対する昇格時のGit照合も確認した。
readerの元49件は同じ入力として再利用し、overlay対応表が2fileだけであることと、
初期化式にcall・関数リテラルがないことの追加2件が成功した。原engineは変更していない。

原build receiptの短縮したcommandにmoduleのcwdが無かった点は、新しい注記で
実際のcwdと絶対出力pathを補った。原receiptのbyte/SHAは変更していない。
この注記を過去の原buildや後続3451の原配置記録として使わない。
readerのGoは1.27.0、safe-delete候補は1.27.1と実物のbuild情報から確認した。

最新main `dbb1c79c30e16b3ef7a894bb16ccd3eacccf3471` に対しても候補0e16の
safe-delete/reader/Go依存の差分は0だった。元buildのhead/hashを付け替えていない。
v2の変更差分は追加の固定レビューへ回す。実物への適用は引き続き0で、
原old証拠の受領と実qualifiedのGreenが必要である。

## 追加レビューと復旧記録の修正（2026-10-24）

3484の実Opus5.5/maxによる追加差分レビューは正常終了し、Must0だった。
12対象のSHAとbyte数は一致した。Go本体・テスト・手順は読了し、元49件のログは
Action・テスト名・時刻を確認した範囲である。全ログ本文の読了とは扱わない。

同じ復旧領域への2回の指摘を受け、v2を保持してv3で事前確認と停止記録を揃えた。
候補のpin、3つの残物名、既存journalをvalidate/applyの共通preflightで確認する。
既存の途中backupがあっても、先に新しいstageを作らない。途中journalは自動再開しない。
staged/backupのNTFS identity・ACLを同期保存し、置換前・置換後・結果不明を区別する。
作成途中で元identityが記録できなかった残物は、不明のまま保全し勝手に片付けない。

元v2で「残物が増える」「置換後の失敗で元識別情報を失う」の2件Redを実測し、
v3で同じ2件がGreenになった。関連する管理者テスト14個、subtest込み45件とvetも成功した。
backup移動先には異なる継承ACEを実際に付け、元ACLが保たれることを確認した。
再開状態やpath差替えの拒否テストは、対象のエラーまで照合した。実行中の呼出し元を調べ、元担当と更新区間を
調整してから適用する手順を残した。queueや定期taskの設定は本担当で変更していない。

readerはGo1.27.0と同じ2input overlayで別の所有出力先へ再ビルドし、元SHAと一致した。
v3のテストexeも、入力3fileを固定した新しいビルドで実行済みexeと同byteになった。
Program FilesのGo1.27.0と、moduleの要求で自動選択されるGo1.27.1を記録で区別した。
前注記の「actual prior tool invocation」は再現用の説明として訂正し、過去の生の
tool-call原記録を今回取得したとは主張しない。元receiptと前注記のbyteは保持する。
ignoredのprivate controllerはpublic commitに含まれるとせず、sourceの固定SHAを別記する。

最新main `b6d1da58dea2740b3d948aaf04c060fb43ab1510` まで候補入力の差分は0。
v1/v2は過去の証拠として保持し、今後のapplyには使わない。未知のcurrentを受理せず、
元3451の原配布証拠、追加dataの固定レビュー、実Green、実配布と受入が引き続き必要である。
同一SIDの最後のnamespace競合では、競合fileを上書きした後に失敗を検出し得る限界も
明記した。これを攻撃者へのセキュリティ境界や、失われた別fileを復元する機能とは主張しない。
この限界は、正常な用途資格のGreenとは別に、指定1本だけを更新する受入条件に関わる
未充足事項である。競合時に別fileを上書きしないことを確認できるまでは、v3の成功テストや
用途資格を実更新の根拠にしない。実更新は引き続き0で、この文書の訂正も配布完了ではない。
