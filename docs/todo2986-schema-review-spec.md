# PassShareの生成定義と前回未読の補足確認（2986）

読み取り専用の別モデル確認。最終返信だけ返し、編集/保存/commit/ToDo更新を求めない。
GPU/Civ6/重Cargo/ブラウザ/APIキー/新費用/秘密/子Codex直接起動は禁止。
起動時D:/CC/AGENTS.md、作業AGENTS.md、本人Windows完成優先方針を読む。
実対象はpublic AutoDevJapan/PassShare ID1131498455 / PR720だけ。未知のPC製品treeを使わず、
GitHub APIの固定contents/compareと確認済み補助repoのgit objectを読む。scrub391/拒否非迂回を守る。

固定base8398204b1befb3d21dea461baee052b7188ea52f、今回HEADd96fbabe6559e6b55eb8d5be2e10889f03a9a472。
差分は5ファイルだけ：
- apps/desktop/src-tauri/gen/schemas/{desktop-schema.json,windows-schema.json}
- apps/mobile/src-tauri/gen/schemas/{acl-manifests.json,desktop-schema.json,windows-schema.json}
元main c67bfb8から20ファイルになったが、前段レビュー3012と3015の読了を全面的に重ねない。
3012は8d4a21bの12ファイルを確認済みで、Must1は生成定義の更新漏れ。
3015は8d4a21b→8398204の4ファイル（Windows2workflowとDesktop生成定義2）を読取中。

追加で、3012が未読を明記した次だけを補足する：
- 今回HEADのapps/desktop/pnpm-lock.yamlとapps/mobile/pnpm-lock.yamlの更新差分/解決された版。
  元mainとのdiff、JS manifest/Rust lock整合を読む。Cargo2blob全体は2996/3001/3006のMust0と
  同一性確認を保持し、再び全面レビューしない。JS lockはRust tauri2.12.4にAPI2.12.4、
  CLI2.12.3、desktop plugin-updater2.14.0が合い、不必要な依存を更新していないか確認。
- cc-pc-tools-rs固定53c9b524e07e6ee537db487738101dc7ae7695b3 の
  tools/passshare-ci/{schema.go,schema_test.go,main_test.go,main.goのschema入口部分}。
  main_test.goは前回本文未読だったので今回読み、実Git17ケース/集約6/DB4の根拠を確認。
  親worktree D:/CC/Work/unattended-operations-20261013-wt/todo-2986-passshare-ci のgit objectで読める。
  既読のmain.goの他の部分やWorkflow本体は必要な接続以外を繰り返さない。

実失敗と今回修復：
1. 8d4a21bの正式Native WebView run39261188728/job118088948209でRust回帰成功後に
   git diff --exit-codeがTauri2.12更新の生成定義2件を検知した。差分を固定API原本へ適用。
2. 8398204のNative WebView run39263444799/job118096806375では内容は同じだったが、
   git applyのautocrlfと通常hash-objectのfilterを取り違え、実payloadがCRLFになってdrift失敗した。
   今回はLF bytesの--no-filters raw blobを取り、base64 POST blobのGitHub返却SHAと照合してから
   treeへsha指定した。正しいdesktop schema blob=c2202ed85785caaf57f12c34137d930945617694、
   rawSHA256=f0e08336353666f6174241964bfe0260cca6379fa7659825433b1263f82a91bd。
3. Desktop/windows旧schemaは旧desktopと同一blob75edcb5なので新しい同一LFへそろえた。
   Mobileの旧desktop/windowsも同一blobec639bc。実Desktop差分の4共通定義hunkを
   Mobileへcontext check付きで適用して両aliasをそろえた。Mobile schema raw blob=b21db6734acad5542effb66994a133e8c16d21c0。
4. Mobile ACLは、実Desktopの旧→新が4つの共有定義追加だけであり、旧Mobileのそのpluginが
   旧Desktopと意味上同一であることを厳格に確認し、新定義だけを移した。
   core:appのallow/deny-exit、core:windowのallow/deny-set-fullscreen-on-monitor。
   旧default_permission/grant/他plugin/他valueは不変。Mobile ACL raw blob=6d32ee95626145825148513063f13a5587621022。
   Go helperはRust struct field順をRawMessageで保ち、名前mapだけを整列する。
   実Desktop差分が他の意味を変えた場合やdefault grantが変わる場合は拒否する。
5. Mobileデータを新しく実Rust生成したという主張はしない。共有する実定義を限定して適用し、
   新候補の正規CIのdrift/実renderer受入で最終検証する。新CargoビルドはPCで行っていない。

原jobログ、Git patch、元blob一覧、rawSHA/blob照合は親.ci-workの
generated-schema.private.patch / mobile-schema.private.patch / all-schema-receipt.private.json /
schema-base-blobs.private.json / promoted-verify-failure.private.log / integrated-verify-failure.private.log。
原ログの巨大JSONを全文表示せず、必要な差分を構造として読む。
新candidateはsource Cargo/JS/共有WASMを前段と一切変えていない。WASMの40入力/hashは不変。
認可capability/CSPやdrift検査、Native WebViewの実renderer受入を削除/免除していない。
構造以外のbyte順・改行・JSON序列がRustの再生成で一致するか、CI未確認と区別して指摘する。

確認済み：Goの限定transfer/既存grant変更拒否検査、vet成功。raw5blobをGitHub応答と全照合。
初回のmap全体再直列化がRust struct順と合わず拒否された原結果を保持し、その方式は使っていない。
同じ最終sourceの必要検査を無目的にrerunしていない。正式CIは新candidateで実行中。

Must/Suggestion/Nitをファイル/具体的根拠/修正案で返す。固定base/head、読了全ファイル、未読、
実行検査と未実行を分ける。読取完了ならMustが残っても先頭「状態: 完了」。実装/CI/mergeは親2986が追う。
