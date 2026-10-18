# Linux runner起動差分の有限レビュー

元2736の新しいLinux起動修復だけを、既存OAuthサブスクのSonnetで読み取り専用レビューする。
原2797は完了済みで、当件はその固定spec・モデル・結果・関係を変更/再実行するものではない。
root2736の単独下位review。子ジョブ起動、編集、ToDo更新、commit、push、PR、ブラウザ、
外部送信、テスト、Cargo、Docker、GPU、Civ6、認証/秘密/原製品sourceの読取は禁止。
最終応答だけに結果を返す。結果ファイルを作れないことを失敗理由にしない。

## 固定対象

base c1dbdcd5701483fe4bbcc0faf2f2015482714bc7
head 2bc1dcd5cfe5d12f59db2a38e6a6474b2721cb4c
worktree D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci

主要差分は tools/product-ci/pr148_linux_guest.go と
tools/product-ci/pr148_listener_startup_test.go。必要な境界として
tools/product-ci/pr148_linux_process_linux.go のpr148Filesystem/pr148ExecRestricted、
tools/product-ci/pr148_linux_run.go のlistener起動/終了とhook照合、
tools/product-ci/README.md のPR148起動の追記を読む。
同headのGit対象と作業コピーが異なる場合は、固定headを読み、差分がある事実を返す。
広い履歴/他helper/他製品/保護設定を探索しない。

規則はD:/CC/AGENTS.mdと当worktree/AGENTS.md。新しいfull workspace検査をしない。
関連説明はTODO-2736-CHECKPOINT.md先頭「2026-10-19 00:38」節だけ。
公開SDKの原scriptは親が認証なし有限containerから取得した
当worktree/codex-owned-184a58eedf4f923741f160dfe7cfd5b7/sdk-public-runner-scripts-003834.sh
だけ追加読取可。これはSDKのrun.sh/run-helper.sh.templateで、製品source・認証ではない。
原listener logや他の恒久evidence/外部receipt/旧profileは読まない。

## 確認済みの事実と仕様

旧実装はread-onlyの/runnerで公式run.shを開始した。公式scriptはrun-helper.sh.templateを
run-helper.shへcpし、失敗しても補助scriptを呼ぶ。その終了を通常0へ置き換える。
実行ではrun-helper.shが見つからず終了した。実listener exit0でも開始/完了hookがなく、
親は失敗として保存。正式Rust run39141771645の両OS jobはqueued/steps0のまま。
コンテナと登録、専用lockは回収済み。製品CargoやCiv6/GPUは開始していない。

修正は公式helperと同じRunner.Listener runを、同じ既存Landlock/seccomp環境で直接execする。
境界の権限や許可path、元の検査コマンド/OS/timeout/source/認証経路は変更しない。
再起動するshell loopを挟まず、元のlistener終了コードを返す。ephemeral/disableupdateは維持。
起動probeは設定しない実SDKのlistenerを同じ境界で開始し、未設定のexit1と文言を確認する。
probe成功は実製品job成功や登録成功の証明ではない。

親はGoの関連TestPR148群、vet、Windows/Linux buildを全てexit0で確認済み。
実SDK startup probeは4.05秒/exit0、内部listenerの未設定exit1を検証した。
これは親の実行結果であり、レビュー者自身が実行したとは書かない。
修正版による実製品job再試行は、当レビュー後の未実施事項。

## 観点と応答

公式run-helperと直接runの実害ある差、終了コード伝播、隔離とsignal/process lifetime、
probeの検証が実際の起動失敗を捕捉するか、過大な成功主張を点検する。
許可範囲や原2797を再設計せず、根拠のある現差分のMustを示す。
最終返信は日本語2500字以内。固定base/head、実際に読んだファイル、未読範囲、
実行検査0を明記。Must/Suggestion/Nit、file:line、根拠、具体的な修正を分類する。
Mustなしも明記する。技術的な選択をオーナーへ質問しない。
