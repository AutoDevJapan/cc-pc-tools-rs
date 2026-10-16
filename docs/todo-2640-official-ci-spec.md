# 万世記 PR148の通常CIを追加費用なしで回復する有限工程

元の実装担当はToDo2640、元製品受入はToDo880。本人2026-10-15の直接依頼による独立工程であり、元880の停止・依存・未完を解除しない。対象はGitHub APIで所有を確認したAutoDevJapan/civ6-banseiki、repo_id1392152644、通常PR148だけ。PC上の旧製品フォルダや保護設定には触れない。

## 今回必要な結果

PR148の残る通常CIを、追加費用のない正式なGitHub Actions実行として成功させ、実run/job/checkと原証拠を元2640へ返す。手動のsuccess status、checks APIによる合格の転記、過去のテスト結果だけを示す合格扱いは禁止。これは終了済みの検査資源の後片付け待ちとは独立した工程。

起動時点のsource headは75a4051305b6f47c70ddb59a8d2bf9015666f12c、treeはcecb9cd535d135a1eb4407882db532c325f42860、base/mainは1100676d1ed421794c252fb4b15ed3d954e21a5b、原branchはcodex/todo2640-workspace-doctests-20261015。起動時に実owner/private/nonfork/admin、同head/base repository、PR状態、実branchrefをAPIで再照合する。sourceが変わっていたら勝手に旧headへ戻さず変更の出所と範囲を調べる。

## 確認済みの原因と実結果

2026-10-17 00:35 JSTのAPI確認ではPRはopen/draft/mergeable、base/headは上記と一致。repo専用runnerは0件。

- Rust tools and Lua tests: run38892356460、Ubuntu job116944644598、Windows job116944644412。両jobのstepsは空、conclusion=failure。annotationは「The job was not started because an Actions budget is preventing further use.」。これはテスト失敗ではなく開始前のHosted予算制限。
- Native Civ6 driver: run38892356435、job116944644189、同headの開始前failure。
- Shared Civ6 live chain: run38892356407、job116944644332、同headの開始前failure。
- CodeQL: run38892349061のactions/rust双方success。解析1966901102/1966891652は同head、error空、results_count=0。完了したCodeQLを再実行しない。
- ToDo2730: 同headの関連driver_safety48件と回帰1件は両OS成功。Linux workspace1835件+example15件=1850件成功。Windows1853件成功とGit所有者拒否1件の限定再検査成功。両OSfmt/doctest/全対象Clippy成功。全件の重複実行なし。初回リンカー準備失敗とGit拒否の原ログは保持。
- ToDo2710: 旧74f023c0ee4b460f05f87dc3db5ceaf75e999a8aのnative/Lua/138scenario/live-chain成功。75aではrunner.rsだけが変わり、それ以外1119エントリーが旧treeと一致。旧nativeなどを75aで実行したとは書かない。
- Haikuレビュー2647: 固定rust.ymlのdoctest2行、Must0。レビュー2729: runner.rsのAlreadyExists変更slice、Must0。後者のsource連鎖へのSuggestionは、元Error::from(io::Error)もErrorKind::Ioとvalue.to_string()だけであり連鎖を保存しないことを元2640がGitHub APIで確認した。追加の連鎖後退なし。

## 実行の範囲と手順

1. D:/CC/AGENTS.md、対象プロジェクトの規則、元2640のdocs/todo-2640-checkpoint.md、本specを読む。製品の規則はGitHub APIから必要範囲を取得する。原製品のAGENTS.md/CLAUDE.mdは当該headで404、documents/repository-rules.mdが存在する。元製品フォルダへ探しに行かない。
2. 元2648の独立Go helper tools/product-ci、tools/ci-check-2710と正式runnerの既存方式を調べ、必要な通常Rust/native/live-chainの有限実行経路を実装する。新規PC用コードはGo、既存Rustは維持。拒否されたowner-work登録、登録manifest生成、guard変更、共有resident bridgeの許可拡張、旧製品sourceのclone/checkoutは一切しない。
3. sourceはGitHub APIから、検査専用の新しい有限隔離資源にだけ供給する。専用OS/runnerの同repo・same-source・run/attempt・実Git HEAD/tree・認証不在と共通Cargo gateを維持する。runnerの登録・受付・起動だけで成功扱いしない。登録token等は必要な既存helperのRAM経路だけを使い、モデル/画面/ファイル/原ログへ出さない。
4. Hostedの同失敗runを無条件rerunしても解決しない。検査コマンド・OS・失敗伝播・Lua/scenario/native/live条件を保つ、同PR上の必要最小限のrunner経路変更はこの工程の範囲に含む。変更はGitHub APIで通常の同branchに非forceでcommitし、前後の実headと比較を保存する。新しいsourceになった場合は本体変更と検査経路だけの変更を区別し、根拠ある再利用と本当に必要な再検査を分ける。未来のmain検査を恒久的に実行不能にするroutingは採用しない。
5. helper/製品workflow変更を別モデルで有限レビューし、Mustを直す。子Codexジョブを直接起動せず、必要な独立レビューは共有ToDoに--spec付きで登録する。単一ファイルだけならHaiku、それ以外は既存OAuthの別モデル。新AI APIキー、従量課金、契約追加、Hosted予算増額は禁止。
6. 割り当てられたCargo枠で通常CIを実行する。civ6/GPUの起動は禁止。live-chainのunit検査は実ゲーム受入とは別。実ゲームを起動しない。共有gateと専用target、既存の直列条件を保持する。既に成功した同sourceの全workspaceを正規チェックに関係なく重ねて回さない。正式jobの必要実行と証拠のsourceを明記する。
7. この工程は製品PR148のマージを行わず、実commit・run/job/check、必要全件の網羅、OS/source/tree、原logsのbytes/SHA256/exit、別モデル応答、変更理由、残failureを元2640へ配送する。通常マージと独立工程の完了は元2640が行う。helperの通常PRは別モデルMust解消と必要CI成功後に自身でマージし、必要なGo helperだけ既存配備先へ反映してよい。並行変更と実hashを確認する。

## 証拠と終了条件

正規証拠はD:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459配下。manifest-final-parent.private.jsonが元/保存SHA対応を保持する。CodeQLのrepo-1392152644-official-final.private.jsonとrepo-1392152644-codeql-75a.private.logは恒久保存済み。2730の詳細はD:/CC/Work/unattended-operations-20261013-wt/todo-2730-windows-rust/TODO-2730-CHECKPOINT.mdとcodex-owned-ac8143dbbc50a8cf65b2987dfb99ee96/execution-index.private.json。原log/source/SARIF本文はstdoutやモデルへ一括表示しない。

旧2710/2730の未回収root/profile/hiveは既存2381で待機中。この工程からそのroot/profileや登録/保護を変更しない。cleanupの成功とCIの成功を混ぜない。自分の新規資源はsafe-delete createで所有記録を作り、原証拠を恒久保存して全SHA一致を確認してからplan→別通常削除→finish。拒否された同操作の再試行や別手段の回避は禁止。plan指定のquarantineも拒否なら保持し、既存整理担当へ具体的に渡す。自分のcontainer/volume/runner登録/lock/サービス/process/追加ACEを終了確認し、残留は未完として保持。

原880/134/995/996の停止・依存・未完、元2640のPR保存、許可の限定範囲を保持する。新UI/製品方向、費用、公開範囲、顧客との新約束、他Org、ドメイン事業者、Terminal/UAC、本人PC操作、旧製品フォルダ・原保護設定に触れない。Rakko以外のドメイン事業者へアクセスしない。上位の仕事が待つ場合は安全な区切りで保存して枠を譲る。

独立工程の実装/検査配送後に元2640の完了を待つ依存は作らない。自分の後片付けだけで待つ場合は正規todo waitを記録し、元2640へ検査終了の結果を先に渡す。作業ブランチを変えたらscheduler checkpoint-workspaceで自分のToDo/job/expected_branch/branch/実HEADを保存する。意味のある区切りでこの工程のworktreeへcommitし、秘密・無関係な変更を含めない。
