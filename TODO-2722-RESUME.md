# ToDo2722: Windows取消しの独立検査結果

隔離されたCCソースでの修正、実検査、別モデルレビューを完了した。親2648のソース、tools/product-ci、配布runtime、既存runner、製品ソースは変更していない。独立checkの後段制限に従い、独立PR作成・mergeは行わない。

## 固定ソースと実原因の確認範囲

実装commit: 1c0ef903ee3dbdc1bd5f7f339fd89ee92500e4b4。cc-jobs tree: 6a1447bb0dbea1ffce979d814a4cc6b94f542973。作業branch: scheduler/todo-2722-work。基点: 3002f5eceaffb1a1534f289a0e6e0740270d7193。以後のcommitは記録だけでコードを変えていない。

元CI38876424070/job116892467856はlost supervisor取消しでAccessDenied。原ログSHA256: 0E71B444EBF117E6A37544EEFC0E86B23A614A79DFF800B867D6538E2CA5A601。

元のwalkは数値の親PIDで候補を絞っており、単純な同名process列挙という仮説はsourceと一致しなかった。ただし生成時刻下限の確認がOpenProcess後なので、再利用された親PIDの古い辺が保護候補を先に開く経路を決定的REDで再現した。修正では同一kernel snapshotのbirth/parentを使い、親より古い候補をopen前に除外する。保持済みbirth不一致、snapshot後の世代変更、birth不明、整合する候補のAccessDeniedはfail-closedを維持した。

原候補のbirthは原logにない。元候補が実際に無関係だった断定や、旧CIそのものを成功とする主張はしない。

## 実検査

- 関連nextest: 52 passed、0 failed。原ソースbaselineの関連8件は8 passed。旧walkで古い候補を開くREDを確認した。
- 最終コードの全workspace nextest: 1回のみ、2198 passed、0 failed、原5 skipped、590.997秒。run ID: 7a96352e-5dc7-4024-9f12-8564d8f8cf8f。
- 実DACL拒否の所有fixtureでkernel birthの取得と古い候補の非inspectを確認。同じ拒否対象がowned候補と整合する場合はAccessDeniedで失敗する対照を確認。
- lost supervisor E2Eはsupervisorだけ停止、全childの生存を事前保持handleで確認してcancelし、同じ全handleの終了を検証。全件内でも1.424秒で成功。
- workspace doctest exit0。16 summaryは全テスト0件であり、存在しないテストを実行したとは扱わない。
- workspace全targets Clippy -D warnings exit0、cc-jobs fmtcheck exit0。
- 追加のworkspace fmtcheckだけexit1。先頭のRust2021のimport整形等はbaseとの該当3file差分0。必要CIはfmtを要求しない。有限scope外の全体書換えを加えず、任意検査の失敗として原出力を保持した。

## 別モデルレビューと提案への対応

有限review2723の最終job: 20261016-184722-todo-2723。モデル: anthropic/claude-sonnet-5-5。fixedHEAD: 1c0ef903ee3dbdc1bd5f7f339fd89ee92500e4b4。原文はreview-2723-received-original.mdに無変更で保存。SHA256: 5CEF21DE21DE04E4A88434D85CDB5DB3EA77290E892A37ADEFD9050A80CBF424。Must0。検査はreviewerでは未実行。

先行する182523/184123/184423の3jobはシェルのDLL初期化失敗0xC0000142で未読。これらをレビュー成功に数えない。最終jobは同SHAをGitHub連携で読み、実owner AutoDevJapanを確認しレビューを完了した。AGENTS.mdとdocs/build-speed.mdはreviewerが未読と申告している。主担当は両文書を読み、検査・配布禁止・scope条件を照合した。

提案は価値と実装事実で判断し、コードは追加変更しない。
1. Snapshot ABIの実行時の親PID二重照合: 将来のOS layout変更に対する追加対策という提案。現行OSで実snapshotと保持birthの一致、実所有treeの全終了を検証済みで、layoutの不一致を示す実害は確認されていない。固定layoutと境界検査を保ち、この有限修復では新しいnative queryを加えない。将来のABI変更リスクは残る。
2. 親の世代再利用: cancellationはprior全件のbirthを保持handleで再確認し、全発見ancestorのhandleを保持する。parent PIDが別世代へ再利用される前提はこの経路で成立しない。idle側もwalkの世代不一致でtree全体を返さず、freezeはtree成功後である。
3. 世代不一致の再snapshot: 不明な対象を再試行で成功にせず、要求どおりfail-closedのまま返す。boundedな停止ループはstop_treeに既存32回、ただし身元エラーは継続させない。
4. root birthの追加判定: walkは開始時にfound.insert(root, root_created)し、候補ごとにfound.get(pid)とentry.createdの一致を検査している。rootもこの既存判定を通るため重複しない。
5. 古い作業メモ: 本記録を完了時点の内容へ書き直した。過去記録はGit履歴に保持。クラッシュ再開用記録を残す直接指示があるため、ファイル自体を削除しない。
Nitの不正snapshot全体拒否は維持。テスト専用Dropのassertはcleanup異常を隠さない既存方針で、通常経路の成功を確認済み。壁時計逆行とbirth順序の前提は旧実装から維持しており、今回緩めていない。

## 証拠の所在

所有領域: C:/Users/makhmeto/AppData/Local/Temp/codex-owned-f77d57a1dfc7225219380a22f27a30aa。親検証用の原ログは保持し、不要物と混同しない。checks.json、review-receipt.json、checkpoint-finalのstdout/stderrもここに保持。

- related-final.log: 6667 bytes / 589EE25045B4E358A3DE6DA59990CA60B3D48ABA099CFBA9C2B6489B6D843F1A
- workspace.log: 285060 bytes / 988AB79B5B5EDA5787327355B68BED1DBD0BECA027E644D708538547F3D04F65
- doctest.log: 2380 bytes / 454E25E528DD4D2B6AAFE04946D5099FAE8E6CB1E86D9340BCDFF391B56036CB
- clippy-workspace.log: 4951 bytes / 0BDBE26E108F567C767EE0DA856A6CBAA6D68B1FEFB5A651100BAA365D813518

## 後片付けと親への残工程

専用targetは初回の所有記録がないため偽造登録しない。7193file、15135104079 bytes（hardlink名を含む論理量）。132group/264名のhardlinkが全て同target内、外部hardlink0、reparse0、同target imageの稼働0を確認。既存生成物の規則でD:/削除希望/2026-10-16-todo-2722-target-191324/targetへ移動済み。元target不在、移動先7193file/15135104079 bytes一致を確認し、target-quarantine.jsonと理由.txtに完了を記録した。容量回収済みとは扱わない。

元2648へ、固定コード・実検査・review原文hash・後片付けを配送する。親でこの修正を取り込み、同headの必要CIと通常mergeを確認する工程は親の責任範囲に残る。2026-10-16再開時点のPR185はOPEN/head ddc914fc7530f3e9095228d53eaeb914680f8ede、mergeCommitなし。主担当のsourceやPRをこのcheckから編集せず、親完了を待つ依存も登録しない。

2026-10-16 最終記録: 清掃通知をdiscord_ask log --topic 後片付けの記録で1回実行したが、exit1/QuestionStateRecoveryRequired（質問台帳・本人回答の原記録復旧待ち）により投稿は成立しなかった。自動承認レビューの拒否とは区別する。raw stdout/stderrを証拠領域のcleanup-log.stdout/stderrへ保存。同じ投稿の再試行や別経路での送信、共有台帳の空原本作成・変更は行わない。親2648の記録へこの未送信を明記する。整理自体は上記のとおり実施済み。
