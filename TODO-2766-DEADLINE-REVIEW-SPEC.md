# GoDD Press 2766：期限切れの必須指摘を修正した差分を確認する

親2766の有限な読取レビュー。あなたは既に割り当てられた担当であり、別の子ジョブ・ToDo・質問・Discord投稿を起動しない。ソース修正、保存、commit、API mutation、VM/runner起動は不要。結果を最終返信だけで返す。2766/2642の完了を待たない。

## 固定対象と仕様

repo AutoDevJapan/cc-pc-tools-rs（1406818713）。元Pressは1386702183/PR175の通常CIで、親の製品merge/配布/全体doneは対象外。物理source/公式job/OS意味/完全log/回収の実証を保持し、API fixtureやhookだけで公式成功としない。

固定base f1c3c265f32f72067dc39494d8a864b37500ad22 → head 7d6f73337e32a9d0d7e9f0e0b43dff374039461c / tree 7b0d93f8afeaf5433b9ab5d2f745165e0c9d82b8。別worktreeの作業HEADや後続記録をこの候補と混同せず、git show <固定head>:<path> と git diff base head を使う。

元3272は68ac605/tree72cc7c6の指定32file読取を完了。実job20261021-140954-todo-3272、実turn_context anthropic/claude-sonnet-5-5/max、原応答との最終応答一致を親が確認。Must1/Suggestion3/Nit2をREVIEW-2766-ADMISSION-3272.mdに保存。今回そのMustの修正と付随差分だけを確認し、元32fileの全調査を繰り返さない。

## 読む対象

元応答REVIEW-2766-ADMISSION-3272.mdを全文。
次は固定headの全文を読む：
- tools/ci-guest-2737/job_policy.go
- tools/ci-guest-2737/job_policy_windows_test.go
- tools/ci-guest-2737/timeout_windows_test.go
- tools/ci-guest-2737/press_deadline_linux_test.go
- tools/ci-guest-2737/job_rust_probe_linux.go
- tools/windows-ci-runner-2737/host.go
- tools/windows-ci-runner-2737/channel.go
- tools/windows-ci-runner-2737/channel_test.go
- tools/windows-ci-runner-2737/contract.go
- tools/windows-ci-runner-2737/deadline_test.go
- tools/windows-ci-runner-2737/press_test.go
- tools/product-ci/pressci/admission_test.go

次は全差分と変更関数・直接の呼出し元だけ：
- tools/ci-guest-2737/guest_linux.go（guest構造体、serve live-job分岐、runGuestのlistener寿命）
- tools/ci-guest-2737/job_linux.go（serveLiveJobのplan受理→source読取→contextとprobe呼出し）
- tools/ci-guest-2737/job_windows.go（validOfficialBinding、既存hostのsocket deadline）
- tools/windows-ci-runner-2737/guest_windows.go（seed.Validate後のchannelServer生成、Runの既存timeout）
- tools/ci-source-2737/sourcepacket.go、signed_payload_test.go（gofmtだけか、diff -wも使って確認）

必要な局所だけ追加可：timeout_windows.go/pressciのRoleJob/Windows session.go/SDK guest_service_windows.go・transportのHTTP寿命。未読のsecure_files*/source_materialization/normal_ci_admission本体や元製品source/PC folder/名簿/保護一覧/guard/manifest/認証へ探索を広げない。分からない点は未確認として返す。

## 修正と確認した事実

- Must: Linux live-jobの旧40分socketと45分listener、Windows host/channel旧26分が元90/360分を切っていた。新Linuxはplan受理後にrole+10分のsocket、boot時には当該workflowの閉じたrole上限+15分、Pressのlive受付時にはrole+15分へtimer更新。αの40/45分は保つ。Windowsはseed.Planからrole+6分をhost context/socketとservice channelへ設定し、Runのrole+3分を包む。αの26分は維持。元workflowのtimeoutは変更していない。
- Suggestion1: PR175の既存3 run38893666794/38893667129/38893666823の実GETを今回確認。event=pull_request、pathはいずれも素の.github/workflows/<name>.yml、head_shaはb2候補。全13job APIもhead_sha候補、runner0で元予算起動前failure。現在の照合はこの実形と一致し、二次要約を理由にmergeへ置換しない。物理GITHUB_SHAとhookは別のmerge pinのまま。新runの実形は実行時に再照合する。
- Suggestion2: Pressの返却JobNameをcompiled policy.Nameにも一致させ、全13roleとWindows両roleの改変拒否を追加。αの非空ルールは保持。
- Suggestion3: admission全正負fixtureを13role全部へ展開。4 shardの別labelを実際に入れ替えた拒否を追加（元attempt違いの拒否も保持）。原workflowと公式結果の代用にはしない。
- Nit: sourceの2fileをgofmt。Linuxの観測用Rust probeをPress workspace/Rust1.95.0へ選択、αは1.97.1。観測はsourceproofやcompiler全体のprovenanceではない。
- 旧host.goだけのGo overlay＋新回帰テストではWindows両roleが「26分、必要96分」で実FAIL/exit1。新sourceの全runner race11.035s/exit0、実mTLS channelもαとPress両roleで実deadlineを確認。alpha26分を保持。
- guest全module Windows race18.888s/exit0、最終13roleのdeadline/JobName focused race1.274s/exit0。product-ci pressci全race4.603s/exit0。3module vet全exit0、guest最終vetもexit0。
- 最終Linux test binaryをGOOS linux/amd64で生成。既存source-free SDK image sha256:89988...69011の自分の通信なし/read-only/CPU1/RAM512MiB/GPU・host bind・socketなしコンテナで、新11 Linux roleをserveLiveJobの実制御frame受理まで通して、source packetだけ故意に不正として停止。全11roleの実socket期限確認がPASS/exit0。製品source/runner/Cargo/CIは0。このコンテナはPID0/exit0をinspectしrm -v後に不在を確認。新image作成0、既存imageは保持。
- 製品head b2、PR175 open/Draft、base4dc、実merge f3aは不変で、GitHubへ書込み0。元CodeQL成功はb2のまま、再実行0。公式通常CIはまだ未成功。
- Windows retained SDKはServer2025/Rust1.97.1。元Windows2022/Rust1.95.0との意味・選択、Chrome/Node等の不足確認/準備は次の実装工程であり今回の成功範囲に含めない。main側local-checksが既に変更されており、最小runs-on変更のPR merge成立も未確認。このレビューへその実装や新guest準備を要求しない。

## 重点

元の13timeoutを外側context/socket/listenerが短くしないか、Pressだけのtimer更新と期限/取消し/排出、未承認planの期限延長、古いαへの回帰、返却JobNameの同一性、4shardの本当のlabel入替え、probeを成功根拠へ昇格させない点。外側に未読の期限があれば、場所・影響・確度を具体的に返す。

## 一次資料・規則・返却

AGENTS.md、D:/CC/AGENTS.md、D:/CC/Policies/owner-agent-conduct-20261021.mdを適用。必要ならGitHub公式workflow timeout/events REST run/job、Go公式time.Timer/Contextだけ。ネット調査は今回の差分に必要なものに限定。GPU/AI APIキー/新費用/本人PC操作/Terminal/UAC/Discordは禁止。

読取が終われば厳密な先頭「状態: 完了」、次段落に「読取レビューだけ完了」と書き、Mustが残ってもレビュー自体は終える。固定base/head/tree、全文読了・追加局所・未読、Must/Suggestion/Nitとfile:line・具体的修正、実行検査と未実施を3000字以内で返す。Go実行や新API取得は要求しない。結果保存/commit/ToDo更新/子ジョブはしない。実modelは親が実job/対応turn_contextで照合する。