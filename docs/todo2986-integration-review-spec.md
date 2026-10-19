# PassShareの最後の4ファイルを確認する（2986）

別モデルの読み取り専用レビュー。最終返信だけを返し、編集/保存/commit/ToDo更新は求めない。
GPU/Civ6/重Cargo/新費用/APIキー/秘密/ブラウザ/子Codex直接起動は禁止。
起動時D:/CC/AGENTS.md、作業AGENTS.mdと本人Windows完成優先方針を読む。
対象は正規APIで確認するpublic AutoDevJapan/PassShare（ID1131498455）PR720だけ。

前段の固定HEAD8d4a21b73f3ad6301ba37ecb056b221c9d5c626eはレビュー3012が確認中。
今回HEAD8398204b1befb3d21dea461baee052b7188ea52fとの差は次の4ファイルだけ。
1. .github/workflows/desktop-build.yml
2. .github/workflows/op-cli-build.yml
3. apps/desktop/src-tauri/gen/schemas/acl-manifests.json
4. apps/desktop/src-tauri/gen/schemas/desktop-schema.json
Cargo/JS依存/共有WASM/Quality Gate/Mobile等の前段の確認を全面的に重ねない。
固定compareをAPIで読み、上記以外の差分がないことを照合する。
補助コードはcc-pc-tools-rsの固定6aa134d28e277fab6ecd2f4ea5e030f05b5f696eにある
tools/passshare-ci/desktop.go、desktop_test.go、main.goの追加呼出し部分、
testdata/before/desktop-windows-reviewed.yml。親worktreeのgit objectから読むことができる。
親worktreeはD:/CC/Work/unattended-operations-20261013-wt/todo-2986-passshare-ci。
未知のPC製品sourceを読まず、scrub391保護判定と拒否非迂回を守る。

Windows2workflowの基点は3010が唯一の書込担当として作成/レビューしたPR721の
e19dbf27ff1a27df344dba52bcde436552464b79。本人の最新方針と正規ToDo3010のnoteが
最終統合を2986へ渡し、3010は並行編集しないことを明記した。
同じ2ファイルのPRを作り直していない。今回commitは8d4a21bとe19dbf2を両親に持ち、原成果を保持。
CLI workflowはe19dbf2と実バイト同一。Windows以外のmatrixを外し、Windowsの元build/artifactは保持。
Desktopはe19dbf2のWindows checks/commands/env/途中失敗伝播/artifact条件を保持し、
job内の関連変更判定だけを加える。workflowのPR/main入口とpermissionsは不変。
同じ実検査済みchangedPaths（Git/Bash17ケース）を使い、共有依存、lockfile、workflow、
toolchain、消費資料、frontend/cloud-assets.jsonとpublic/cloud-crypto/*を対象に含める。
PRはbaseからの全差分で選択し、main pushは古いrunが取り消されても両clientを実施する。
無関係なPRはWindowsのチェックをskipし、Windowsの配布buildも起動しない。
元の型検査をnative compileの前へ移動する。Go候補検証器とWindows鍵保管・native smokeを落とさない。
実構造比較でOSとscope以外のcommand/env/step/artifactを保持し、専用Goテスト/vet/actionlint成功。
PC上の正式self-hosted移管・同一成果物hash/回収・Windows版全体の完成は別2957/2994で未達。
その未達をこのPRで達成したとは報告せず、未供給labelや新費用は追加していない。

生成定義2ファイルは8d4a21bの正規Native WebView run39261188728/job118088948209が
Rust回帰試験成功後のgit diff --exit-codeで返した差を、固定API原bytesへ適用したもの。
元CSP/実画面/IPC/ネットワーク/保存/失敗経路等の受入workflowは無変更。drift検査を削除/免除しない。
git apply --checkが成功し、生成後blobはログindexと完全一致：
acl=b7e734d38c3f2bf0d4f257a7896332271c6b54ac、desktop schema=c2202ed85785caaf57f12c34137d930945617694。
raw patchは親.ci-work/generated-schema.private.patch、原ログはpromoted-verify-failure.private.log。
データの実差はcore:appのallow/deny-exitとcore:windowのallow/deny-set-fullscreen-on-monitorの定義追加。
既存default_permissionはすべて同じで、capability/CSPの付与設定は変えない。
JSONを構造として比較し、実際の許可拡大や不要な差分/検査弱体化がないか確認する。
親はローカルCargoを重ねず、正規CIの実生成差分をそのまま使った。修正後の正式CIは今回初実行中。

前段8d4a21bではQuality Gate、Desktop全体/doctest、Mobile全体/React/JS/API、root/native audit、
DB E2Eが正式成功。監査は免除空。Native WebViewは上記2生成物のdriftだけを観測した段階で失敗。
新候補の正式CIと今回の別モデル確認が完了するまでmergeしない。

実害あるMust/Suggestion/Nitを該当箇所と具体的根拠・修正案で返す。
固定base/head、読んだ全ファイル、未読、実行した検査/未実行を分ける。
読取が終わればMustがあっても先頭「状態: 完了」。実装/CI/mergeは元2986が続ける。
