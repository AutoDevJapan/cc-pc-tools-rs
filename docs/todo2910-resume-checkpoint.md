# ToDo2910 再開記録

対象は現在の5業務2913〜2917の実作業復帰。製品の完成と旧原192件の復旧は別条件として残す。

2026-10-19の再割当先は `scheduler/todo-2910-godd`。旧作業場所とPR235の成果は保持し、同じ2910で引き継ぐ。別担当のGo/Rust/配布を巻き戻さない。

## 受領した事実

- 実GitHub所有者AutoDevJapan、repository ID 1406818713をAPIで再確認した。
- PR235はhead `8107c0574cc6915d0d55eb941b2c431540552889`、元base `b62aa042f2f5b7b98fc652124d314df8182e57dc`。全14成功/1neutralを2026-10-19 13:25 JSTに確認。再実行していない。
- 原レビュー2924の正規CLI結果はSonnet5.5、11steps、exit0、Must0/Suggestion2/Nit2。原応答SHA256は `082850F5B33F937D5257C3113DE4A292D20286C203EAE2B6AFFF5D89D114CB6E`。試験差分末尾を未読と明記しているため、同じ2924を不足範囲だけの指示へ更新して通常再開する。別の製品actorは作らない。
- 原レビューのSuggestion1について、Rustの `cc-scheduler/src/current_succession.rs::verify_current_product` を親が読了。正式guardの資格、全識別field、root/spec path/本文の3fingerprintとpath判定を照合し、独自の絶対期限は参照していない。任意の改善だけで8107を変更しない。
- 正式guardは別担当のPR234を含むsource `5411cac009326b69bc725fc7ff6e7163b64297ff`、trimpath/modified=false、実SHA256 `0B24C92204DB1C26AD56EB8EE12E4E105F92D425BF0A7063A0F0A524FA268E5E` を観測。現branchを最新main `1123681cae4da1a1c01f718563ed4fa656203cba` へfast-forwardした。
- 必読指定の原資料と旧進捗票は正式guardがclassification_unverified/client_resource=trueを返したため本文未読。今回チャットの具体的指示と参照可能な既存source/docsで独立工程を進める。保護設定や旧原本を書き換えない。

## 次の工程

1. 同2924の未読変更行の補完を正規schedulerで受領し、原応答/固定head/実モデル/範囲を記録。実害のMustだけを解消する。
2. 固定8107と正式CIの成功を再照合し、PR235を通常mergeする。
3. 最新mainを同2910のbranchへ統合。現在ファイルとレビュー済みblobの一致、他担当の変更保持を照合する。結合sourceが変わった範囲だけ関連Go検査を実行する。
4. 最新mainのcleanなsourceから正式guardをtrimpath build。CC-AdminBrokerで現SHAをCAS比較し、backup付き原子的更新。source revision/module tree/実SHAを残す。
5. 正式実行物で5件の現在資格全field/3fingerprint、rootと正規worktree/実worktree、旧actor拒否を確認。
6. 通常schedulerの同5件が実job/専用worktree/実コマンド/変更または監査成果/検査まで進むことを確認。queued/steps0を実再開に加算しない。
7. 元2834と主2903へ実事実を返し、5件の実作業復帰後に2910を完了する。製品全体の未完条件は各ToDoへ保持。

再開時はこのworktreeの `todo2910-resume.private.json` と同2924の通常CLI結果を照合する。branchを変えた場合は終了・待機前にschedulerのcheckpoint-workspaceで実HEADを保存する。ほかToDoだけの待機は `todo wait 2910 --on 2924` を使う。
