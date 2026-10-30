# 修正後レビューの受領と互換性の追加修正（4088）

以下は`3db162894c0d82c5e792260c8d1ff4f2fa972661`を読んだ結果であり、
読取の正常終了を修正完了へ読み替えていない。
原応答は正規のジョブ結果受領で取得した。

| 担当 | 実行 | 実行時の観測 | 元結果の受領SHA-256 |
|---|---|---|---|
| セルフレビュー4126 | `20261028-012403-todo-4126` | `native_turn_context`、Opus5.5/max | `99a36b5a8f5a4d87175c0e458f951bc55ef75edd3a4d459d44f13e7828624c10` |
| ラバーダック4127 | `20261028-012905-todo-4127` | `native_turn_context`、Sonnet5.5/max | `391971372563e9bdfad9af71292c69ddacd3a7903ca33d98bfb652a0660acc54` |

4126はMust3・Suggestion6・Nit3、4127はMust1・Suggestion6・Nit3を返した。
両者は差分20ファイルと関係する利用側・既存の資格を読み、テストは実行していない。
配布スクリプト、私的な入力、実際のjob/spec/会話の本文は読んでいない。
以降の表は実装側の対応であり、追加確認の合格ではない。

| 指摘の範囲 | 対応と回帰テスト |
|---|---|
| v2・ordinary-onlyの一時所有が旧資格を失う | 運用受領と退役の適格性を一か所へまとめた。旧形式自身は許可せず、独立した既存資格で通常分類から通ることと、受託producerが優先することを確認。 |
| 照合済み会話の資格を失う | 会話単位の既存照合を使い、質問・返信だけを`verified_conversation_record`で返す。未照合の本文・一覧・状態は拒否する。既存の会話fixtureを通常の分類入口へ接続した。 |
| 一覧から漏れた会話・台帳コピー | Discord保管先全体とOMB workspace、既知のrecord storeの複製を混在として扱う。宣言済みの実行物・設定と、照合済み個別会話の資格は維持する。本文・受信状態・バックアップの配下をテスト。 |
| OMB readerの未帰属の扱い | 固定nativeだけを既存のmetadata用途へ接続。source/binaryはclear、正常APIとの結合は必須。陽性受託と未結合を拒否するテストを追加。 |
| 容量監査の報告に区別が出ない | schemaと従来statusは保ち、categoryのprotection_basisとdriveの根拠別件数をJSON出力へ追加。再開後の報告もテスト。 |
| 無関係な行の型・重複・Unicode | 無視する内容の許容度を通常射影へ合わせ、対象のメタデータだけを厳格に確認する。nullのtodo_id、無関係な不正値、対象の重複・不正を分けてテスト。 |
| 環境によるホーム・Tempの誤同定 | 固定した境界でGit探索を止め、環境変数を除いた場合をテスト。 |
| 読取中の置換と未登録の誤分類 | snapshotを取得できない場合も正常な置換の観測があれば一回再読。未登録はreference_unverifiedで、一般受領へ戻さない。読取中の実ReplaceFileW、置換二回、単一射影、陽性の先行、期限を本番query関数で確認。 |
| 出所不明が一般受領に戻る | producerの不許可は陽性を保持し、それ以外をstored_record_origin_unverifiedへ集約。混在記録のdeadlineも内部のReceiptRestrictedで一般受領を拒否。独立した個別会話の証明だけを維持する。 |
| 取得不能とGit矛盾の混同 | rev-parseの失敗と実際の不一致を分けて確認。 |
| remoteのない新規自社repo | remote一覧が正常に空の場合だけ場所の根拠を使い、origin以外のremoteやGit失敗と分ける。 |
| 一時producerの重複照会 | typed producerの一回の確認を使い、同じoriginとsource qualificationを繰り返さない。 |
| 書式だけの差分・重複したテスト行 | Rust 2021の元のimport/改行へ戻し、別分岐に隠れていたdepartmentの行を削除。専用の独立テストは維持。 |
| 配布と戻し方 | [配布の確認と復帰](../owner-rule-4088-rollout.md)へ確認の方法と条件を分けて記録した。 |

## テストの受領と確認範囲

Cargo割当の4125（job `20261027-224938-todo-4125`）は、固定`81d7da7f`で
conversation_bridgeの28件すべてとcc-askのdoctest（対象0件）に成功した。
独立したquestion_imagesというテスト名はないため、同APIを通るconversation_bridgeを選んだ。
その後のRustの変更は元の書式へ戻すだけで、画像資格の分岐・テスト内容は変えていない。
同じコードの有効なテストとCIを再利用し、元担当はCargo割当を持たないため直接実行していない。

この再開で、guard全件のraceは`9f0c7a8f`のコードで309.736秒、内部2パッケージも成功した。
その後の混在記録の限定を含むOwner・Conversation・Model・Retirement・Owned・Discord・CI3574の
関連raceは51.075秒で成功、vetは終了0。
容量監査の全race/vetは成功（2.587秒）。
record-recoveryは、最初の全件実行でホームのGit管理内に置いたfixtureを拒否した。
コードの制約を弱めず、テストのTEMP/TMPだけをWindowsのコード外のTempへ分け、
全raceを8.555秒、vetを終了0で確認した。変更したOMB関連のraceも先行して1.951秒で成功。
実物の受託データ・カード・除外一覧・native本文をテストしていない。
