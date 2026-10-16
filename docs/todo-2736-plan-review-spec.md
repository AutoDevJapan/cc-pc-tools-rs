# PR148の有限CI経路案を別モデルで確認する

元2736の独立したread-only review。変更・検査実行・製品source複製・PR/ToDo更新・結果file保存は不要。最終応答で返す。
対象は元2736のworktree tools/product-ci/pr148_plan.go、pr148_plan_test.go、main.goのpr148-plan追加2行、go.mod/go.sum、README冒頭の説明。差分は8db9ee1から本specとともに保存されたcommitまで。後続の別file変更は範囲外。
元specはD:/CC/Tools/todo/specs/2736-4be31c9811d84906825cad887219f654.md。D:/CC/AGENTS.mdと同repo AGENTS.mdも確認する。保護対象名・秘密を読まない。旧製品folder・原証拠rootを探さない。

仕様: repo1392152644/PR148、75a/treececb/base1100だけ。原workflow三つのdigestを固定し、GitHub APIの同repo/private/nonfork/admin/branch/head/baseを前後照合。元4jobはHosted予算による開始前失敗でありテスト失敗ではない。新費用/予算変更、過去結果のstatus転記は禁止。新workflow案は通常Rust両OS/native/live unitの原commands・OS・timeout・失敗伝播を保持し、mainを恒久的に未実行にしない。原sourceは保持し、案はRAMだけでreceiptはmetadata/hashだけ。変更の送信・実runner・Cargoをこのモードでは行わずproposal_only=true/all_required_checks_success=false。

検証済み: Go test ./...とgo vet ./...成功、Windows build成功。合成fixtureで原commands/commentsのbytes保持、checkout head/auth、duplicate/alias/multidocument拒否、changed workflow/route/base/fork/branch/tree/startedjob/annotation/post-headの拒否を確認。実GitHub APIのpr148-planは原4job/3workflowを確認してreceipt作成に成功。最初はLinux jobのOS移行noticeを追加failureと誤認したため、予算annotationがちょうど1つ存在することを確認し全JSON valueのhashを記録するよう修正。これは原logのhashではない。製品検査・source取得・workflow変更・登録は0。既存CodeQLは再実行していない。

観点: scope/前後照合、YAML byte offsetと意味比較、main/他PRの経路、checkoutと認証、実検査との誤認、漏れた情報や意図しないAPI mutation。論点をMust/Suggestion/Nitとfile:line/具体修正で分類し、base/head/実際に読んだfileと未読範囲/静的reviewと実行を区別。Mustなしでも残る実行基盤は未完と明記。1500字以内。実機検証済みと異なる懸念も「要実機確認」で残し、根拠を添える。
親2736が原応答を保存・SHA/判断/ToDo完了を担当する。親2736の完了を待つ依存は作らない。
