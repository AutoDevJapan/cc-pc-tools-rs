# ToDo2860: 公開 pnpm action の有限調査

範囲: 公開固定 action、許可済み公開SDK image、自分の新規container、合成fixtureのみ。
親の製品、checkout、実行ログ、SARIF、VM、disk、route、公式run、既存containerは対象外。
独立PR・マージなし。GPU・Cargo・APIキーなし。自分の共有ToDo状態はschedulerが管理。

開始branch: scheduler/todo-2860-pnpm。開始HEAD: 6d267ff。
job: 20261019-034852-todo-2860。
所有temp: fd1a98aa3b1e1482519a9afe616a9b2e。
temp path: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-fd1a98aa3b1e1482519a9afe616a9b2e。

完了: 起動規則、関連スキル、worktree保護判定(false)、cleanな作業branchを確認。
公開action取得/hash保存。所有container de6b0e0f6490fda7c1a905d88af7c49b0d72f9f874aa7d4625d5846b8639e50d
（todo-2860-pnpm-20261019、1CPU/1GiB/no-new-privileges/cap-drop ALL/host mount無し）。
SDK Node20=v20.20.2、Node24=v24.21.0、image system Node=v18.20.4/npm9.2.0。
action本体Node24 + system PATH18でpaths[0] undefinedを再現。
PATH先頭だけSDK Node20へ変更すると同じ合成pnpm@10.11.0のactionはexit0。
固定bootstrap @pnpm/exe11.25.0の公開tarをlockのsha512で照合。
setup.js:60のimport.meta.dirnameがownDir、64でpath.resolveへ渡る。
node18.19.1の公式tarを取得しSHASUMS256と一致を確認（まだ未実行）。
再開直前item_18は公開package消失によりsource読取失敗、lock JSONの空keyで解析失敗。
npm失敗後のディレクトリ再読取はせず、公開tar展開とConvertFrom-Json -AsHashtableで解決。
完了追記: exact18.19.1でもexit1の同分類を確認。SDK PATH24/npm11.19.0でexit0。
SDK PATH20/system npm9.2.0固定でもexit0。原因と修正案はRESULT.mdへ保存。
全5対照は1仮説の検査であり、別仮説は実行していない。
所有container停止→Running=false/PID0→rm -v→不在を確認。元SDK imageは保持。
所有tempはplanが示した通常削除を別呼出しで実行し、finish=source_absentを保存。
文書/JSONの構文と参照・差分書式を照合済み。Rust/製品codeの変更がないためCargo検査は不要。
親向け要約はPARENT-HANDOFF.txt。自分のToDoへnoteとして保存し、状態更新はschedulerに任せる。
成果はこの作業branchでcommit/pushし、最終HEADはscheduler checkpointと終了報告で渡す。
残る製品側の確認・修正は親2737の範囲であり、本調査に未実施の製品工程を含めない。

仮説枠: (1) action本体とPATH上のNode選択、(2) allowlistから欠ける公開runner環境、
(3) 必要な場合のみ公開packageManager入力。未実施は実施と数えない。
branchは変更しない。変更が必要ならscheduler checkpoint-workspaceを実行する。

確認済み結論: 公開@pnpm/exe11.25.0のimport.meta.dirnameがNode18でundefinedになる。
推奨案: 検査済みSDK Node24のbinをaction子環境のPATH先頭へ置く。
親の環境には適用していない。原製品CIの成功とは報告しない。
