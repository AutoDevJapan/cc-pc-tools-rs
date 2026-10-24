# OAuth枠不足後の独立レビューの継続（ToDo3669）

元3080 / Issue283 / PR447の独立レビューで、利用可能性を確認して選んだ
Luna/policy/maxが通常レビューのOpusへ戻り、同じ枯渇枠で停止した。
通常レビューの既定は維持し、確認済みのquota復旧だけを保持する。

## 受入と変更範囲

1. 共有ToDoの通常受付、schedulerの準備、jobsのCLI登録と最終起動を通し、
   同じレビューの旧実行がquotaで終了・排出済みで、Anthropic枯渇とOpenAIの
   利用可能な既存OAuth枠を確認できる場合にLuna/policy/max/Fast要求を保つ。
   本人の個別指定と復旧のpolicy指定を区別する。
2. 元job、所有世代、仕様、worktree、権限、作者モデルを照合する。
   古い記録、偽の記録、未排出、本人停止、未完依存、作者と同モデル、
   未確認の利用枠、APIキーだけの枠、追加支出が有効な枠では特例を認めない。
   queueの排他・二重起動防止とcapacity/429の分類・有限予算は保持する。
3. 一時的なprovider枯渇を作者の恒久除外へ入れない。通常レビューは
   review_profileを使う。全体のモデル既定、口座、guard、UI、中央Skills、
   queue設定、資源上限は変更しない。
4. 生のquota返信、アカウントID、メール、秘密値を新記録へ残さない。
   要求、登録、起動、nativeの観測、上流実効値、完了を区別する。
5. 影響クレートと利用側のテスト、doctest、Clippyを共有Cargoゲート経由で行う。
   Red/Green、別モデルレビュー、同じheadのCI、通常マージ、Windows実行版の
   正規更新・読戻し、元3661の限定復旧を別々に記録する。

## 再開記録

- 開始ソース: 76bc178702511ddea00f8136730eec31056252c5（確認時のorigin/main）。
- 実GitHub owner/publicをAPI確認した。親b249caaf/PR447は変更しない。
- 原todo.jsonと原3661/job.jsonはguardがunverifiedとして読取りを許可しなかった。
  別パスやファイル属性による探索はしない。許可された親の集約証拠では、
  原3661はcancelled・旧process不在・レビュー未完と記録されている。
- 現在: 受入固定と再現テストの追加。修復、Green、別モデルレビュー、CI、
  マージ、Windows更新、実復旧は未実施。
- 自分のブランチはscheduler/todo-3669-oauth-3080のまま。変更していない。
