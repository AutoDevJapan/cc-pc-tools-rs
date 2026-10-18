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
残り: exact18.19.1の対照、推奨PATH24の成功確認、結論、停止/PID0/除去、temp safe-delete。

仮説枠: (1) action本体とPATH上のNode選択、(2) allowlistから欠ける公開runner環境、
(3) 必要な場合のみ公開packageManager入力。未実施は実施と数えない。
branchは変更しない。変更が必要ならscheduler checkpoint-workspaceを実行する。
