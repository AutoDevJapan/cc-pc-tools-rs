# ToDo2910 再開記録

最新の2026-10-19主2903の工程指示に従い、現在継承先2839/2850/2913〜2917の7業務を同じ業務の完了または本人停止まで継続する。5件だけ継続しAO/13aを旧復旧期限で止める旧記録は訂正する。原資格1626/2407/旧動画2475と原資源の10/21期限、旧192件の未復旧は維持する。

## 受領と現在の修正

- 再割当先は `scheduler/todo-2910-godd`。実jobは環境変数CC_JOB_IDと通常ToDoを照合する。Native UUIDや旧jobを現在jobとして結合しない。
- 実GitHub所有者AutoDevJapan、repository ID 1406818713をAPIで再確認。既存PR235を引き継ぐ。旧head `8107c0574cc6915d0d55eb941b2c431540552889` は5件版であり、7件版としてmerge/配布しない。
- 旧headの正式CI14成功/1neutralは旧版の証跡だけ。原2924のSonnet5.5/11steps/Must0と、同2924の通常補完job `20261019-133054-todo-2924` のSonnet5.5/6steps/exit0/Must0/S2/N1を受領。補完は試験差分251行/7hunkと影響実装を読了し、未読の変更行0。原応答SHAは082850F5B33F937D5257C3113DE4A292D20286C203EAE2B6AFFF5D89D114CB6EとF25DE98ACFBA990DF16C7538ECACACA2C5DE2364F52A3DDDD64888FF7AF4E942。新7件版へ流用しない。
- 旧PRの8107と並行main `ced27d8` を通常mergeでこのbranchへ統合。PR234の既存CI資源分類、PR236の動画分類を保持。main_windows.go/他Go分岐/Rustには今回の編集を加えない。
- current_windows.goのAO/13aに同じongoing属性を設定。固定7件の同repo/実ID/root/spec/本文SHA、実deny、status=todo/doing/waitかつdispatchable=true、正規子の親doing/waitと物理Git照合を維持する。
- 7件の現在受領/pathの期限前後・停止/完了/無効化・別root/spec/SHAと、7件の正規子のonline/offline拒否を検査する。原資格のAPIが期限後に原記録読取前でscope_expiredを返すことも確認する。
- AO2839/13a2850は正式資格APIが成功し、同じ固定specの実SHAも宣言値と一致した。固定specと元担当は編集・置換していない。新しい製品actorは作らない。
- Rust受取の根拠は `cc-scheduler/src/current_succession.rs::verify_current_product`。全fieldとroot/specパス/本文の3fingerprint、正式guardのpath判定を照合し、別の絶対期限は参照しない。Rustの実装は変えない。
- 必読指定の原資料は正式guardがclassification_unverified/client_resource=trueを返したため本文未読。今回チャットの具体的最新指示と参照可能な既存source/docsで独立工程を続行し、保護設定や旧原本を書き換えない。

## 残る工程

1. 7件版の関連Go検査を完了し、固定headの全影響差分・新しい正規子の試験を別モデルで読了させ、Mustを解消する。不変更試験全体の無制限通読は条件にしない。
2. 同じ最終headの正式CIを確認し、既存PR235を固定HEADで通常mergeする。
3. 最新mainを同じ2910へ取り込み、レビュー済みcurrentファイルの一致と並行変更保持を照合。結合sourceが変わった影響範囲だけ関連Go検査を選ぶ。
4. 最新mainのcleanなsourceから正式guardのみをtrimpath build。実source revision/module tree/候補SHAを記録。CC-AdminBrokerで現SHAをCAS比較し、backup付きFile.Replaceと配布後hashを検証する。現版の観測値は配布直前に取得し直す。
5. 正式実行物で現在7資格の全field/3fingerprint、rootと予定/実worktree、旧actor拒否を照合する。AO/13aは元担当と未完了条件を保持する。
6. 通常schedulerの同2913〜2917が実job/専用worktree/実コマンド/変更または監査成果/検査まで進むことを確認。登録・API・queued/steps0を実再開へ加算しない。
7. 元2834と主2903へ実事実を返し、5件の実作業復帰後に2910を完了。製品完成は各現在ToDoへ保持する。

詳細はこのworktreeの `todo2910-resume.private.json`。safe-delete所有ID `248beccb10819befd144b65690437512` の一時フォルダに旧レビューCLI結果と未実行の配布用手順を保持する。必要な証拠をworktreeへ保存してから不要物を通常回収する。branch変更後は終了・待機前にcheckpoint-workspaceで実HEADを保存。ほかToDoだけを待つ場合は `todo wait 2910 --on <実依存番号>` を記録する。
