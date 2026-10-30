# ToDo4246：本人の保留を所有待ちの再評価で保持する

親3387の有限なMust M1、同じ境界のS1/S2だけを処置する。
新しい司令塔・巡回・reader・全体設定は作らず、実タスクの解除、別モデルレビュー、
CI、PR、配布、Discord送信、元4023の受入は親が行う。

## 再開情報

- 作業：`scheduler/todo-4246-3387`、job `20261030-085825-todo-4246`。
- 開始HEAD：`0d919ed9d0bd68e7005ce2168b6aeaa5225e5a0e`。
- 固定実装 `05a18daddcc4a47eb0ffeb3d748d26c5c594755f` を
  `0abca7fb` として通常のcherry-pickで取り込んだ。
- 指定BASE `9c9a1f30ee0dc5dde0fd1af99035cbc76adaa4e5` のtreeは
  `f8d17addb2fe9c0f77773adee02f3467af721e45` と一致し、固定実装とのコード差分は0
  （文書の履歴追記だけが異なる）。
- 回帰テストを先に追加し、Redを実測（nextest exit100、1件失敗）。M1/S1/S2の修正・Green・関連テスト・
  最終コミット・checkpoint・一時生成物の清掃が残る。
- 一時物の所有ID：`fb358fdfedb926f235710748ada866ad`（Cの一時補助）、
  `ca355ffba5561fca35a0723685ed6ebd`（このworktree内のCargo target）。
  清掃はsafe-deleteのplanを別呼出しで実行してfinishする。

原保護一覧・失われたregistry・原prompt・秘密・実SDK・実タスクのJSONには触れない。
実行する再評価は架空fixtureだけ。現用実行物や設定を更新しない。

