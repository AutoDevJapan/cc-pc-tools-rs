# 元3632のレビュー対応

対象: base f14136d514dbd4a7d0bd0eaea76ed211f7cc9c9e / head9c9afc718ac2426ccb96f482ca950c624528c530。
原応答は親の正規job waitで受領し、私的実行記録へ保存した。実行モデルはanthropic/claude-opus-5-5/max。14指定ファイルの読了と周辺調査、実行していないテストの区別を受領した。読み取りレビューの終了を実装完了とは扱わない。

- Must1: all-driveの失敗時もC/D/E計測・旧形式の保存・判定キューを続行する。失敗した状態は保持し、新しい匿名結果では未取得をnullで表示する。全処理後は非ゼロ終了して詳細調査の障害を隠さない。状態の書込上限を読込と同じ8MiBにし、幅の広い木は深さ優先のカーソルと小さい一覧キャッシュで処理する。破損/超過状態、旧処理の継続、キュー、保存と不存在でなく保持されることを検証した。
- Must2: ctx.Errがあるときはpartialとし、同じ処理位置を残す。実際のguard障害とは区別する。ディレクトリとファイルの両方で期限切れを再現し、再開で重複/欠落がないことを検証した。
- Must3: コンパイル対象を依存パッケージまで取得し、Goファイル・埋込・モジュール定義・go.sumを固定する。guardは39入力（tools/civ6-ci-contractの7入力とgo.sum2個を含む）。現用2971c1…のGo build infoは2fd1555…/modified=false。そのsourceから変更前baseまでrepair-guardとローカル依存に差分0、祖先関係も確認した。アプリとguardは別々のclean commitから再生成し、実行物のbuild infoと一致するsource commit・flags・Go versionを記録した。配備時も祖先、production入力、実行物のbuild identityを照合する。
- Must4: 代替のローカル列挙にはWin32_Volume DeviceIDを使い、Get-Volumeと同じGUIDへ統一した。正常→一次取得失敗→正常で幽霊の切断行が生じない。ネットワークドライブは別に列挙し、provider識別が未知なら容量基準も未知とする。
- 追加修正: 大きい全デバイス結果を64KiB制限の既存ToDo入力へ重複格納しない。68,508bytesの再現でRed、独立した詳細結果を保ちつつ旧キューが通るGreenを確認した。

Suggestion: Eを含むguard付きメタデータ調査の説明を訂正。ディスク/区画の容量をnullableにし、マウント先の変更では同一volumeのカーソルを付け替える。質問のdecision.todo_idが示す元番号と、次の周期で待機を解除しないことを明記した。除外した名前を含み得るカーソルは私的stateに限定し、匿名の結果・status・ログ本文に出さない。元の保護stateをモデルへ読ませない区分を維持する。

Nit: 新しい2つの受領JSONを-textへ固定し、ボリューム4個の表現を訂正した。置換helperのstage/backupの3085という接尾辞は互換のため保持するが、3628専用backupRootで分離し、実物の旧新hashを照合する。

検証: 容量28テスト（race/現用ToDoの隔離CLI/Windows実機/置換ロールバックを含む）とvet成功。実機5ボリューム・7パーティション・3ディスク・3drive guard結果。guard更新関連4テストとvet成功。修正後の別モデル確認・最終CI・マージ・実配備は別に追跡する。