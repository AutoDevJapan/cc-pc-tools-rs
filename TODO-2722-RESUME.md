ToDo2722 再開記録（2026-10-16）
対象は隔離されたCCソースのWindows取消しだけ。親のtools/product-ci、製品ソース、runtime、runnerは変更しない。
元HEAD 3002f5eceaffb1a1534f289a0e6e0740270d7193。branch scheduler/todo-2722-work を保持。
元の失敗原ログSHA256 0E71B444EBF117E6A37544EEFC0E86B23A614A79DFF800B867D6538E2CA5A601。
同じRustソースの関連8件は作業場所では全部成功（baseline.log）。仮説の単純な同名列挙はsourceと一致せず、walkは数値の親PIDで絞ってからOpenProcessする。ただし生成時刻の下限を確認する前にOpenProcessし、古い親PID関係の保護プロセスで失敗し得る。原ログに候補の生成時刻はなく、元候補が実際に無関係だったかは未確定。
現在の修正案: kernel snapshotにある生成時刻と親子関係を同時取得し、親より古い候補を開く前に除外。対象候補の不明さ、取得失敗、保持済み世代不一致は失敗のまま。snapshot parserを新設中、pause.rsへの接続と回帰テストは未完。
検査生成先: このworktreeのtarget。共有cargo.cmd→cargo_gate、sccacheは既存設定。baselineは終了した。
所有一時領域: f77d57a1dfc7225219380a22f27a30aa（C:/Users/makhmeto/AppData/Local/Temp/codex-owned-同ID）。baseline.logと以後のログをここへ保存。safe-delete plan→別delete→finishで回収。保持すべき根拠は作業記録へhashを残す。
未完: 決定的RED/GREEN、実ツリー消滅、関連検査、別モデルレビュー、最終workspace/doc/Clippy、commit/push、親への結果配送、生成物回収。
依頼の具体specは独立PR/mergeを要求する一方、実行メッセージの後段は独立PR/mergeを禁止する。後段の制限を優先し、独立PRを作らずcommit/pushと検査結果を元2648へ返す。親ソースの編集もしない。

実装接続と関連52件のGREEN、cc-jobs全targets Clippy成功。DACLを空にした所有fixtureとスレッド限定SeDebug無効化で、実OpenProcess拒否→kernel birth取得→古い候補非inspect/整合候補拒否を確認。失われたsupervisorのE2Eは全fixtureを事前保持し、同じハンドルのシグナルで全終了を確認する。REDは旧walkでforeign candidates were opened: [2,1,3]を確認。
残りは別モデルレビュー、最終workspace/doc/Clippy、親への配送と回収。

