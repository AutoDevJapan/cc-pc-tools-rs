# ToDo2749 再開記録

2026-10-17。作業branchはscheduler/todo-2749-pr148-windows-runner。
元2736のspec/checkpoint、2730の公開記録、固定f7acf6dの4ファイル、
2746のJob Object証拠を参照した。PR148の実API head/base/branchとrepo IDは指定一致。
旧資源、製品source、製品workflow、正式登録、Cargo/GPU/Civ6は操作していない。

独立Go moduleに契約・準備・有限サービス・制限token・Job Object排出・停止・回収・
読取専用API照合・job hookを実装中。Goの6単体テスト/vet/build成功。
正常終了fixtureは実施し、実子PID29904を観測、Job Object内3PIDの保持handle/active0を確認。
最初の失敗は専用サービスからオーナーTemp親の属性を読もうとしたことが原因。
管理者の準備時は祖先検査を維持し、サービス自身は新しい所有treeだけ検査するよう修正。
元の失敗ログは保持。製品や旧フォルダの権限は変更していない。
取消fixtureは既存CC-AdminBrokerに投入済み。実行結果の受領と別モデルレビュー、
profile/hiveの回収、親への最終配送が残る。

所有一時root: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-6fc78dd68e2b5d16c4844b92686bd55d。
safe-delete IDは6fc78dd68e2b5d16c4844b92686bd55d。bootstrapだけをビルドした。
このrootの最初のサービスは子を起動する前に失敗し、service不在/process0/追加ACE0を
確認した。profile/hive/classesは残留。cleanup-profileはまだ試していない。

追加rootはすべて同じTemp直下のsafe-delete所有記録がある。
- c9d9cca272f24c790d36ca354d71ab27: 操作script・bootstrap・原証拠の一時保存。
- eea3b1b7f1f197c40c078507dd5dbcf3: 正常fixture。親metadata失敗を同じ新serviceで
  診断修正後に成功。serviceは停止済み、まだcollectしていない。
- b1e5125d2f46f9e809ee18d76693f2f5: 取消fixture。cancel.ps1を既存admin窓口へ投入済み。
  2026-10-17 02:44 JSTの確認ではserviceはまだ作成されておらず、結果を待つ。

02:44後にtoken groupのDenyOnlyビットをworker自身で検査する処理も追加しGo検査成功。
成功済み正常fixtureとこれから受領する取消fixtureは、その追加前のbinaryである。
コード差分と実binary SHAを分けて残し、未実測を実測済みとは書かない。

実runner/認証/source/正式CIは親統合工程の未達条件として返す。
読み取った仕様の範囲、旧2730のcleanup拒否、親2640/880の未完は維持する。
