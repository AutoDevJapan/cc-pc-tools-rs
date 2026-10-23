# ToDo2640 再開記録（2026-10-17）

PR148の通常CI4件が開始前のHosted予算制限で失敗している。Draftを維持し、追加費用なしの正式なCI回復工程をCargo枠付きの共有ToDoへ分ける。元880の製品全体を完了とはしない。

## 実際に照合した状態

- GitHub所有者AutoDevJapan、private/admin/nonfork、repo1392152644、PR148の同head/base repositoryをAPIで確認した。
- PR148はopen/draft/mergeable。head75a4051305b6f47c70ddb59a8d2bf9015666f12c、base/main1100676d1ed421794c252fb4b15ed3d954e21a5b。branchはcodex/todo2640-workspace-doctests-20261015。
- 元のdoctest追加2行と、runner.rsのAlreadyExists文脈だけが変更された。現diffは2file、追加15行/削除1行。UIやMODは変更されていない。
- PRの通常チェック: Rust両OS、native-driver、live-chainの4件がfailure。Rustの実annotationと両OSsteps=[]を再確認した。repo専用runner=0。これらの失敗を成功へ書き換えず、マージしていない。
- CodeQL38892349061は同headのactions/rust双方success、errors空/results0。2730の両OSRust/doc/Clippy/fmt実結果を受領した。旧74SHAのnative/Lua/138scenario/live結果は旧sourceの成功として保持する。
- AGENTS.md/CLAUDE.mdは製品固定headで404。documents/repository-rules.mdの必須doctest、別モデルMust解消と同HEADのCI成功後の担当マージ規則を確認した。
- GPU/Civ6/Cargoを本担当では起動していない。Terminal/UAC/本人PC操作/新費用/APIキー利用はない。

## 別モデルの実回答

正規job.jsonのmodel/status/finished_at/last_messageだけを取得した。2647のanthropic/claude-haiku-5-5は2026-10-15 01:17:56にsucceeded、固定workflowのMust0。doctestが両OSに入り、失敗がjobへ伝わることを確認。未検証だったlib targetとdoctestの実挙動は2730の両OS成功で補った。名称のNitと将来キャッシュのSuggestionは今回の機能条件を変えず採用しない。

2729の同モデルは2026-10-16 20:16:43にsucceeded、runner.rsの供給sliceだけのMust0。disk/hashをレビュアーが独立照合したとはしない。2730がsliceと実runner/source SHA一致を補った。source連鎖へのSuggestionは、GitHub APIでerror.rsの元実装もIo分類とvalue.to_string()だけだと実確認したため、後退の実害なし。既存testは固定exists文脈と拒否を両OSで検証済み。既存fileへの文言Nitも拒否挙動を変えず採用しない。

## 実検査と未回収を分ける

2730のLinux1850件、Windows1853件と失敗1件の限定再検査、関連48件/回帰、両OSdoc/Clippy/fmt成功を保持。リンカー準備失敗とGit所有者拒否の原exit/logを保存し、全件初回successと書き換えない。旧native/Lua等は新SHAで実行した結果ではない。

原証拠はD:/CC/Tools/product-ci/evidence/todo-2648-20261015/resume-20261016/d6eb340d94b85fa2d879b467a597d459と対応manifest-final-parent.private.json。CodeQL原full logは353569bytes、SHA256 53B5C712CBC883A4D2C9A5DE63D75E51C026CFFD5CD52697B68D4DB968FBBA73。旧Tempは親が全SHA保管後に回収済みであり、Temp不在を証拠消失としない。

2710/2730の新profile/hive/検査source/SDKの回収は既存2381で待機中。Access denied、PROTECTED_PATH、ENTRY_LIMIT_EXCEEDEDを保持し、本担当から再試行/迂回/旧root操作を行わない。この回収待ちを、通常CI回復の待ち先にはしない。

## 次の工程

1. docs/todo-2640-official-ci-spec.mdをspecとして、通常CI4件の正式回復をCargo枠付きの共有ToDoへ1件登録する。重複登録しない。待ち先番号と受付結果はこの記録へ追記する。
2. PR148へ現在の2file差分、別モデル実結果、両OS実検査、CodeQL成功、残る通常CI失敗を更新する。受付だけを実成功としない。
3. このworktreeのscheduler/todo-2640-workへ本記録とspecだけをcommit/pushして実HEADを保存する。製品sourceをこのPCの旧folderへ複製しない。
4. 正式todo waitで通常CI回復工程だけを待つ。依存終了後は同PRの実source/必要チェックを再照合して、Mustなし・必要CI成功の場合に固定HEADで通常マージする。未回収は各作成元の整理工程に残す。

6完了条件（要件/非重複、修正commit、PR保存、別モデルreview、必要全CI、merge）のうち4件を確認。4/6を四捨五入した67%まで。5番目と6番目は未達。元880/134/995/996の状態を変更しない。製品Prd/ゲーム受入の完了も主張しない。

## 登録とGitHub反映の実受領

- 通常CI回復はToDo2736を--spec/--needs cargo付きで1件登録した。状態todo、spec保存先D:/CC/Tools/todo/specs/2736-1f7ebf215c6a4feebc259a047c9a19fb.md、needs=[cargo]を読み返した。原specと保存specのSHA256はともにE0E86F943AB1E78DCE063CCA56D3C4BBC03CEF4DC77DCCDCB01092BE1700EE86。登録時点で実検査は開始していない。子ジョブを直接起動していない。
- PR148のタイトルと本文を現在の2file差分・実検査・レビュー・残CIへ書き換え、同head/open/draftと本文の完全一致をAPIで読み返した。
- レビュー指摘の扱いをコメント6219044451へ1件だけ記録した。URL: https://github.com/AutoDevJapan/civ6-banseiki/pull/148#issuecomment-6219044451 。レビュー実応答は正規job.jsonに保持し、実行成功や製品全体完了へ拡大していない。
- 元2640はToDo2736の正式完了待ちにする。2736は元2640の完了を待たない。2710/2730のcleanup待ちとの循環を作らない。
- 実登録を読み返し、2736の通常担当20261017-004026-todo-2736がqueuedで受付済みだった。2736のToDoはdoing、実テスト開始は未確認。元2640はstatus=wait/wait_on=[2736]/progress=67、元job所有・specを保持している。
- 保存commit4b88876c7706a628d6c2b8504ab54bd43e1eacf1をremoteで照合した。scheduler checkpointも同branch/実HEAD/元jobでlaunched=0の保存を読戻し確認した。最終追記後も新しい実HEADを通常checkpointで保存する。
- 初回checkpointはInputRejectedだった。GUI subsystemのscheduler.exeをPowerShellから直接呼ぶと完了前に後続のnote/wait操作へ進み、行全体の照合と競合し得ることを実装から確認した。同じ内容の正規呼出しはその後に保存済み。以後は非表示Start-Processの-Wait/-PassThruで終了とexitを受け取り、状態変更を並行させない。guard変更や拒否操作の別経路は使っていない。
- 恒久CodeQL原logを再計測し、353569bytesと前記SHA256の一致を確認した。原log本文は表示していない。

## 2026-10-23 再開・マージ直前の照合

前節は2026-10-17時点の履歴。通常CIの停止条件は、2736から配送された2026-10-20の実結果で解消している。2736は自己作成資源の回収だけを2381に待っており、その状態を変更しない。

- 実APIでAutoDevJapan/civ6-banseiki（repo1392152644）のpublic/nonfork/admin、同head/base repoを確認。公開は既済の2988によるもので、本担当は公開・予算を変更していない。
- PR148のheadはafabad463b1e3f8eb74cfaf9ac2b885776bb8c1a、baseは1100676d1ed421794c252fb4b15ed3d954e21a5b。合成merge b5a571cd844f077b81123892b56deace17b41d5fと候補のtreeはf1dd8d999e76e269145627f921d2ddeff9d13bb9で一致。draft/open/mergeable/cleanを確認した。
- 最終差分は4file（20行追加・1行削除）。doctest追加、AlreadyExistsの安定した文脈、3workflowのevent SHA checkoutだけ。75a以降の追加差分は3workflowだけで、元のOS・コマンド・timeout・失敗伝播を維持している。
- head一致の正式7checkはすべてcompleted/success。Rust39386622982、native39386622979、live39386622996、CodeQL39386619421。Windows1854件・Linux1850件、両OSのfmt/doctestコマンド/release/138scenario、native173件（既存ignore1）、live166件（既存ignore3）が成功。現時点のdoctest実例は0件で、実例を新設したとはしない。実ゲーム/GPU/Civ6は起動していない。
- 固定資料のSonnet5.5レビュー3072/3073の原文・受領記録・実モデル記録を読み、Must0と範囲を確認。checkout後のmerge tree照合・raw log保存のSuggestionは対応済み。元2647/2729のレビュー範囲を広げない。CodeQLの過去の警告スレッドはresolved、未解決0件をAPIで確認した。
- 2736の受入packetは20205bytes/SHA256 3388DF4B591386AABF3E4D1F0638DDCDFD90451EA15AD35FA3AEA229568D96AD。指定18証拠の実bytes/SHAと、6原job logの実bytes/SHAがすべて一致した。原文・raw logはコミットせず、作成元の恒久rootに保持する。
- 現在の6完了条件は5/6（83%）。残るのはPR148の通常マージと反映確認。固定head一致でready→通常mergeし、実merge tree/mainを確認する。追加のビルド・同じ成功CIの手動再実行は行わない。
- 自己作成資源の残留（2736/2381）、元880/134/995/996の停止・依存・製品受入は別の未完として保持する。旧製品フォルダ・原保護設定は操作しない。本人判断は不要。
