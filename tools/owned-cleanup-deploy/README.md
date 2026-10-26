# 元3757の既存ソフト2本の配布

既存のsafe-deleteとcleanup-8hだけを更新する有限なGo補助。
初回の2本更新は`--check`/`--apply`、追加世代は
`--native-proof-check`/`--native-proof-apply`を使う。
どちらもcallerのpath・hash・manifestを受け付けない。
一般guard、原保護台帳、清掃対象、既存タスクの設定は変更しない。

原safe-deleteの根拠は元3650の正規成功イベントitem_174を、その原JSON行のSHAで
固定したもの。Issue471→PR475→元3650をたどって取得し、旧byteの観測から原記録を
生成していない。cleanupは元2491の原manifestを保持し、後続の更新は元2488が
2026-10-13に保存した原handoffのbyteと、正規ToDoのspec_history参照を照合する。
これはソフトの更新来歴であり、一時データの原create/release受領には使わない。
候補2本はreview/CI/merge
済み89f2411のビルドbyteを固定し、Rust候補は自分のCargo出力から単一リンクの
同byteコピーを用いる。コピーを再ビルドや元の配布証拠とは呼ばない。

通常の検証CLIが当該ソフトを未分類にすることと、元依頼で指定された具体的な
ソフト更新の審査を分ける。原証拠と対象を設定済みの自動審査へ提示し、受理された
この用途だけを照合する。一般のデータへ許可を出さず、未分類をclearにしない。
審査拒否があれば同じ操作を別経路で行わない。

新旧byte、原証拠、ソース、リンク・実体を確認してから更新する。祖先を保持し、
書込み・削除を他handleへ共有しない原実体handleを使う。自分のDELETE権で
renameし、直前に元handleの最終パスも照合する。stageとbackupの既存名を上書きせず、
元実体のrenameと新stageの公開は両方ReplaceIfExists=falseで行う。owner/group/DACLを
原handleからstageへコピーする。競合名は保持し、未知のcurrentをbackupへ昇格させない。
旧実体は同じ場所の`.before-todo3757`へ保持する。

既存1h清掃のrun.lockを保持し、cleanup consumer→safe-delete producerの順で更新する。
run.lockは既存のものだけを開き、不在を自分で補わない。2本とも事前照合に成功する
までどちらも変更しない。公開失敗時は元の実体を、名前が
存在しない場合に限って上書きなしで戻す。途中のstage/backupを自動的に削除しない。
片方だけ成功した場合もresultsの各状態と全体失敗を分ける。stepsはstage作成・
元実体の退避・新実体の公開・確認・復元の試行/成否・公開名の有無を別々に返す。
公開後の確認失敗でもupdatedはtrue、verifiedはfalseとなる。復元失敗も専用の
エラーにし、公開名が無い状態を隠さない。途中物を勝手に削除して再試行しない。

初回の実読取照合で、safe-deleteは元3650の旧SHAと一致したが、cleanup現用は
元2491の古いmanifestのSHAに一致しなかった。`--apply`は未実行、実更新は0。
cleanupの後続配布は、元2488の2026-10-13 16:46の保存履歴に残るSHA 6975fc...を
取得した。新しいpinはこの独立した原履歴に由来し、未知のcurrentを観測して
作った値ではない。原handoffの固定SHAと元ToDoの参照を照合し、現物一致を
確認した元3daの実--checkは両方qualified:true/changed:falseだった。原2491の
古いSHAを後続版の証拠に書き換えない。現在の補助のMust修正後の最終審査・配布は
別に確認する。
この未一致を、この補助のfixture成功や実物清掃の成功へ読み替えない。

Windowsのfixtureは新旧SHA、元inode/ACL保持、候補/旧版差替え、hardlink、stage/backup、
退避直前・退避先・公開直前の競合を検証する。nativeの上書きを有効にしたRedで
競合名を失う2ケースを実測し、上書きなしへ戻したGreenとrace/vetが成功した。
最初のfixture読取共有modeとvetのpointer表現の誤りは原失敗logに保持した。
別モデルのMust2は、確認後の競合rename拒否と復元失敗の表示で修正した。
元の削除共有と復元エラーの混同へ戻したRedで3ケースを検出し、修正後のGreenを
確認した。元DACLをstageと異なるprotectedへ設定し、権限コピーを外すRedも検出する。
前向きslashの本番と同じパス表記、復元成功/失敗の実物配置、公開後の確認失敗を
fixtureで確認する。Hostedの生security descriptor比較は、内部offset/paddingを除き
owner/groupのSID・ACL本体・protected状態を厳密に比較するよう修正した。

## 正規実行記録の引用符対応を反映する追加更新

PR496の通常merge後、既存CC-AdminBroker経由で2本を16:54:28 JSTに実更新した。
旧object/新旧SHA、owner/group/DACL一致とstage不在を読み返し、既存1h清掃も
16:58:44に実発火して終了0を確認した。データの実回収はこの配布に含めない。

新しいsafe-deleteで作った自分の4096Bの受入データは、元createの正規記録が
二重引用符の`-Command`形式だったためreleaseがORIGINAL_CREATE_UNVERIFIEDを
返して保持された。引用符の両形式を受理し、echo/未知shell/複合実行を拒否する
8b65cb4fのclean buildだけを`--native-proof-check`/`--native-proof-apply`で反映する。
callerからpath/hash/receiptは受け取らない。これはsoftware更新の追加世代であり、
データの所有IDごとの例外ではない。

追加世代の旧SHAは、3757自身の元broker成功イベントitem_18の原行と出力SHAに
固定する。コピーしたreceiptや未知currentから作る基準は使わない。consumerは
既に配布したSHA7fbc49...のまま保持し、変更されていれば止まる。旧版を
`.before-todo3757-native-proof`へ別に保持し、最初の`.before-todo3757`を変更しない。
原イベント・双方のソース・実体・同じ清掃ロックを確認する。元の2本更新の
`--check`/`--apply`を繰り返したり、途中物を新しい既知値へ昇格させたりしない。

merge後のHosted CIは1件のrenameでRENAME_COLLISION_OR_BUSYを返した。
元の出力にはnative errorの詳細がなく、その実行のWin32番号は未確定のまま保持する。
更新処理はWin32のエラー詳細を返し、共有違反32だけを同じ固定handleと宛先で
最大4回・各25ms待ちで確認する。衝突183、権限5、その他の失敗は直ちに保持する。
同じ対象の上書き・ハンドルの取り直し・権限緩和・審査拒否の再試行は行わない。
後のCIでは公開後の確認失敗も検出した。確認を省略せず、rename済みhandleの名前
だけに依存しないようにした。元pinを保持したまま公開名・backup名を新しく
メタデータ読取で開き、volume/file IDが保持中の元handleと同じ実体かを照合する。共有write/deleteを
許可する新しい読取handleも、先に保持したpinの排他を解除しない。確認失敗はread・
bytes・公開名の実体に分けて記録し、元失敗でどの条件だったかは推測しない。
追加の実PC診断では、同じvolume/file IDを保持したままrename後のCreationTimeだけが
変わったことを記録した。公開名の照合は、新しく開いたhandleと保持中handleの
volume/file IDを照合し、CreationTimeの異なるcacheをidentityに含めない。
元pinは閉じずに同じfile IDを保持するため、別物への差替えやID再利用は受理しない。
これは配布補助のstage/backupの照合であり、safe-deleteの原birth受領は変更しない。
最後に元のAPI失敗の原因も再現できた。Win32へ渡すFileNameの終端NULを取り除き、
byte countだけに依存していたため、後続memoryの内容によって別の名前になる。
旧bufferの後ろに非0の64byteを置くRedでは、実際に指定と異なる名前へのrenameを
検出した。UTF-16終端NULを明示してFileNameLengthのbyte countからだけ除外する
修正でGreen。公式の[FILE_RENAME_INFO定義](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info)
のFileName説明と照合した。後続memoryの内容に依存する処理を残していない。
