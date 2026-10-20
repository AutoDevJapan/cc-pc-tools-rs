# ToDo3058 再開記録

- 対象: repair-guardの有限資格。親2962のcore/workflowと3011の3runtimeは未変更。
- 作業branch: scheduler/todo-3058-ci。開始HEAD: 46bf1346b612e4d99cdddd14399023dd908c0eac。
- 正規repo owner/API: AutoDevJapan/cc-pc-tools-rs、1406818713を確認。
- 実装: 埋込canonical contract、未作成rootのみの初回資格、固定source/owner/origin/下限commit、外部配備証跡のroot/parent/object ID/所有者照合、有限leaf/state名、unknownexisting拒否。
- 全テスト/race/vetを実行中。専用testは成功。
- 未完: 別モデルレビュー、正式CI、通常merge、最新mainからのscrub391通常配布、該当path再guard。runtime本体の初回配備・起動は親の担当。
- owned一時領域: c6cbe2be4d1fe9825801b9a575d1ad49。safe-delete作成済み。終了時にplan/通常削除/finish、拒否ならquarantine。
- rootの初回拒否後の内容読取・作成・配備・起動は未実施。GPU/Civ6/Cargo/新費用/本人質問/Discord完了通知なし。
