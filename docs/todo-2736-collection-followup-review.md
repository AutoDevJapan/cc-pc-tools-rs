# 回収レビューの指摘対応を限定して確認する

主2736の有限read-onlyレビュー。既存OAuth Sonnet5.5のみ。元review2930はe05efa9の
単一code fileをHaikuが読み、Must1（未起動SCM0許容）とSuggestionを返し、親が受領済み。
今回はその後の指摘対応だけ。新たな製品/共有基盤の設計を増やさない。

固定base e05efa9410846a077ae6d61dedb6faba6804d4e7
固定head 375385c5bfc6799688cd0f012dbee7d95859070a
読むのは元親worktreeの以下3件だけ（自分の旧copyで置き換えない）。
D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci/tools/pr148-windows-runner/collect_windows.go
D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci/tools/pr148-windows-runner/collection_status_windows_test.go
D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci/tools/pr148-windows-runner/README.md
Git/追加file/元資源/auth/証拠/原logs/API/ブラウザ/テスト/build/Cargo/GPU/Civ6/編集/ToDo/子jobは禁止。
head/copyの一致は親が行う。返すのは最終応答だけ。読めない場合は代替探索せず具体エラーを返す。

指摘対応:
- collectionServiceStatusはstopped/PID0を要求し、unstartedは1077だけ。0も拒否し、
  新規・未開始サービスだけに限定したabortへ狭めた。普通collectのSCM終了結果と保護された
  launcher reportの照合は保持。helperは初回と削除直前の両方で呼ぶ。
- 削除直前に同handleの実state/PID/codeを再取得し、同SIDのqueryable process censusも再実行。
  変化/失敗/非0の対象processはservice削除前に返る。unreadable数は引き続き記録し、
  実Windowsでprotected processが1件あるため「全process可読」を新条件にしない。
- DeleteServiceのBOOL0はnil又はErrno0でも明示error。BOOL成功時はstale GetLastErrorを無視。
- 表形式のSCM状態/exit/PIDケース、BOOL/LastErrorの独立性、failed/uncertain/completedの
  journalが同mutationを再実行しないことを検査する。111 Go case pass/2 opt-in skip、vetと
  Windows buildはexit0。新しいservice/旧資源への追加実行は0。

受領済みの前提:
- e05efa9の実fixtureは新規・認証なしのみ。prepare/execute成功、両ACL解除/controller復元/
  service削除のjournal全OK、実DACL0/実service不在。profile/hiveは残り、別profile削除の
  初回DeleteProfileWが共有違反32で拒否された。全回収/製品CI成功とは主張しない。
- 親は再試行を禁止した旧5051を保持。root/runtimeのSID grant各1件とservice不在を原記録で保持。
  新sourceを旧5051に適用しない。失敗journalの書換/別API削除/許可拡張なし。
- research2929が仮説として挙げた「service削除後のname→SIDは必ず失敗」は、この新fixtureでは
  cleanup-profileのSID/prepare一致preflightを通ってDeleteProfileW32に達したため実測と違う。
  SID→nameの失敗は別方向で実測済み。初回の将来新規service回収が本phaseの範囲であり、
  存在しない旧serviceの一般復旧や保存SIDfallbackは追加しない。現行のfail closedを維持する。
- countGrantedSIDはroot/runtimeの対象Allowを直接SID比較する。全子孫を実走査したとは書かない。
  auth不存在/同queued job/steps0/runner無し/remote同一性などは前版から不変の依存契約。
- controller Full復元はcredential-free fixture/unstartedのこのnew root/runtimeだけ。
  普通collectでは保護された実launcher drain/AccountSID/SCM success一致が必要。

実害Must、Suggestion、Nitを行/根拠/最小修正案付きで返す。元Must対応、最新state再確認、
全returnのhandle排出、journal非再実行、既存許可範囲維持を確認し、読んだ範囲と実検査0を明記。