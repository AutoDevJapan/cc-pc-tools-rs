# 保存済みのUnslop projectを1件だけ照合する

Unslopの元ToDo 4431に保存されたprojectkeyを、正常な登録情報から取得するための限定した入口です。
project名やフォルダー名から推測せず、保存record、project登録、実producerのGit origin、既存のGitHub identity cacheが、同じUnslop repository ID `1406909338` に一致した場合だけ返します。

```text
scrub391.exe --resolve-unslop-saved-project
scrub391.exe --resolve-unslop-saved-project --association-revision <前回の64桁revision>
```

成功時は終了コード0、未確認・不一致・入力不正は2で、JSONを1件返します。
既存の `--guard-path` やmasked一覧の応答は変えません。
resolverの呼出し箇所を含めてレビュー・CI・正式反映するまで、この入口をinstalled版で利用できるとは扱いません。

## 正常な取得経路

既存の `ownerReadProjectSnapshotUsing` と `ownerTaskRegistrationProjection` を使い、一度の読取から元4431のmetadataとproject登録を取得します。
指定できるToDo番号・repository ID・store path・認可bool・identity callbackはありません。
ToDoの本文、note、履歴、認証情報は応答へ含めません。

取得したproject登録のrootとrecordのcwdについて、既存の `ownerRuleWith`、`ownerRuleRecordOriginSource` を適用します。
さらに、登録rootと実producerそれぞれのGit originが固定の `AutoDevJapan/GoDD-Unslop` であり、既存の封印済みidentity cacheの数値IDとownerも一致することを確認します。
別のnested repositoryが登録rootのidentityを借りることは認めません。

ここで使う登録snapshotの `current()` は、既存providerのファイルidentity・size・mtime・通常保護を読み戻す確認です。
トランザクションや、攻撃者による同identity・同size・mtime復元まで検出する保証ではありません。
別々のDB読取を「同じsnapshot」と呼ぶ実装へ置き換えないでください。

## 結果の使い方

成功応答は `scope: saved_project_association`、固定の `todo_id`・`repository_id`、`project_key`、`association_revision` を含みます。
`qualified` はこの保存済み関連付けの照合結果であり、ファイルを読む権限やプロセスを起動する権限ではありません。

revisionは選択された関連付けの比較値です。
秘密の認証値、署名、認可トークンではなく、既存のmasked一覧のrevisionとも別です。
利用側は各回で正常providerを通し、必要なら前回revisionを渡して変更を拒否します。
保存されたprojectkeyの再利用だけで現在の関連付けやhostの資格を認定しません。

projectkeyが返った後の `todo list <projectkey>` は、別の本文出力を伴う操作です。
その実行は元の対象範囲を確認してから行い、このresolver候補や合成テストの成功を実project lookupへ代用しません。
CodexとClaude Codeの両方からの同等業務結果は、同じ正式版をそれぞれの正規hostから使って確認します。

## 未確認や失敗の区分

| reason | 意味 |
| --- | --- |
| `unslop_project_resolved` | 固定の保存済み関連付けを照合した |
| `unslop_project_invalid_request` | 固定入口以外の入力、または不正なrevisionだった |
| `unslop_project_registration_unavailable` | 正常な登録snapshotを取得できなかった |
| `unslop_project_not_found` | 元4431が存在しない |
| `unslop_project_record_invalid` | 重複IDや矛盾したrecordだった |
| `unslop_project_metadata_missing` | 必要なproject登録・cwd・repository等がない |
| `unslop_project_source_protected` | 元の保護判定により利用できない |
| `unslop_project_source_unavailable` | 既存のsource資格が成立していない |
| `unslop_project_repository_unavailable` | 正常なGit originを取得できない |
| `unslop_project_identity_unavailable` | 正常な封印済みidentityを取得できない |
| `unslop_project_association_mismatch` | 保存登録・実producer・固定repoの関連が一致しない |
| `unslop_project_snapshot_changed` | 通常providerの最終確認でsnapshotの変化を検出した |
| `unslop_project_revision_mismatch` | 前回の関連付けrevisionと異なる |
| `unslop_project_deadline` | 既存と同じ750msの処理期限を超えた、または取消された |

失敗応答はprojectkey・revision・path・自由文のエラーを返しません。
identityが不足したときは元の正常なidentity更新工程へ戻し、別のtoken、DB、CLIやAPIで認可を代用しません。

## テストと現時点の範囲

テストの正例も、所有する一時ファイルを正常な登録snapshot readerで開くところから開始します。
Git/identityの対照値は合成であり、installed版の所有資格を発行するものではありません。
正例、unknown、重複、同名別ID、保護、nested repository、変更後revision、処理中のsnapshot変更、取消、CLI引数の秘密非出力を準備しています。
