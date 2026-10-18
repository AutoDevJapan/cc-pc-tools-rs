# ToDo2828: Windows guestの有限差分を別モデルで確認する

読み取り専用の別モデルレビューです。自分が実際に使ったmodelを最終返信に明記してください。
実装担当と異なるmodelが確認できない場合は、その不足を明記して合格へ読み替えないこと。
実装/ビルド/試験/VM/SCM/runner/原source/GPU/Cargo/PR/merge/Discord操作を行わない。
子Codexは直接起動しない。結果は最終返信に返し、結果ファイル/commit/親ToDo更新は求めない。
元2828/親2737は未完。レビュー担当が親を待つ循環は作らない。

## 固定対象

repo AutoDevJapan/cc-pc-tools-rs id1406818713 private/admin/nonfork。
実装作業場所:
D:/CC/Work/unattended-operations-20261013-wt/todo-2828-godd-mcp-windows-ci-guest
branch scheduler/todo-2828-godd-mcp-windows-ci-guest
base f03eb1f (依存だけのimport commit。git rev-parseで完全SHAを確認する)
head bec99388b45319bfed7e3db67f1cbd24422883fd
tree 6c5c18608b0d7940178baaa97938e6dd0a8fb5cd

`git show <fixed-head>:<path>` と固定base/headのdiffで読む。動く作業コピーの現在HEADは
対象にしない。対象は上記base→headの23fileのみ:
tools/windows-ci-runner-2737/ 全ファイル、tools/windows-ci-sdk-2737/guest_service_windows.go、
TODO-2828-CHECKPOINT.md。headにまだないこのレビューspec自身は対象差分でない。
開始/終了で固定head/treeと対象file一覧を確認し、読んだfileと未読を明示する。

## 読む仕様・境界

tools/windows-ci-runner-2737/README.md, HANDOFF.mdを全文読む。
特に固定public seed/typed Go API/各段階/戻り値/Offline staging/service/COM/TLS/path契約。
AGENTS.md, docs/test-build-scope.md, D:/CC/AGENTS.mdの適用範囲を維持する。
sourcepacket/normalciは親c373a18c2aa455051226bc9569f0edfc7b526a3eから同じrepo pathへ
importした不変共通実装。M1 normalci.Plan同process link/M2 secure checkout proofを緩めず、
別実装を並立しない。必要な局所参照はtools/ci-source-2737/*.go、
tools/product-ci/normalci/*.go、tools/windows-ci-sdk-2737/{protocol,transport,guest_service_windows}.go
と既存cmd利用側、Job Objectの元パターンpc-ci/service_windows.goに限定してよい。
元製品folder/owner-work/登録manifest/client一覧/guardの中身/2783のfolder・assetを読まない。
scrub391 --guard-pathによる対象判定は許可される。APIキー/認証値/原sourceを読まない。

## 主な受入条件

- 固定対象はrepo1154066087/AutoDevJapan/GoDD-MCP-Alpha/PR3613/元ref。
  product head/tree/workflowSHA256はhostが明示pin。code SHAと混ぜず次headを勝手に選ばない。
- role windows-native-checkのみ、positive run/job/attempt、Inputs空、name todo2737-<fresh32hex>。
  labelは厳密に cc-product-2737-1154066087-windows-windows-native-check-<RunID十進>。
  親のbaseline normalciはlegacy labelで、この実装はそれを拒否する。親のroute/normalci
  接続修正は2737の明記済み残工程であり、本子への同機能複製を要求しない。
- canonical Cargo gateはhost2737が保持。public seedのsource/nonce/1..10jobs/threadsと
  guest Planを一致させる。self-claimed env markerでhost gate通過を主張しない。
- TLS private keyはRAMのみ。COMはsame clone nonce/IP/server hash bootstrapだけ。
  bulk source/SDK/tokenをCOMへ流さず、SDKの既存mTLS/SCM/COMを再利用する。
- requestに任意program/path/URL/env/shellを含めない。固定stock runner release/hashで
  config.cmd ephemeral/no-default-labels/disableupdate、tokenはRAM/mTLS/secret envのみ。
  hostがqueued/registration intentを確認後に渡し、実runnerID/name/labelを保存する
  positive ack前にはlistener/run.cmdを起動しない。
- sourcepacket Materialize/VerifyCheckoutは専用guestだけ。物理HEAD/tree/index/blob/modeと
  checkout auth不在を返す。startはpre-materialized tree、completeは実checkout後のproof。
  original Windows13工程/profile/直列/20分/権限/if/gateをstock workflowで保持し、
  手書き13commandコピー/CRLF変換を行わない。
- stock同期start/complete hookのcontext/source/ref/event/workflow/inputをhost official claim
  に照合しsingle-use/positive ackだけ進む。complete hookは公式job完了前、そこでVMを消さない。
- cancel/EOF/timeout/service-stopでWindows Job Object treeをdrain。同handleで子孫とrootの
  終了確認、PID再利用を生存と誤認しない。console CREATE_NO_WINDOW。ログbounded/private/hash。
  SDK/環境はclone専用固定path。host profile/owner credential/API-key AI/GPU/Civ6を持ち込まない。
- VM/host gate/registration回収と実official acceptは親の工程。mockを実成功へ読み替えない。

## 実施済みの検査

実Windows/Go1.27: 新module go test -race -mod=readonly -timeout90s ./... 成功3.967sとvet成功。
同module通常検査成功。sourcepacket go test ./... 23.793s/vet、SDK unit tests5.130s/vet
(SCMや5SDK6版の再実行ではない)、normalci型package/vet成功。
合成TLS/COM/Windows pipe/誤pin,run,role,legacy/input/再入/EOF-config/pending-hook取消し、
仮source物理proof/CRLF変化拒否、実JobObjectで残存子孫の通常/取消終了を確認。
初回のコンパイルではhex命名衝突とSID.String複数値、gofmt cwd誤りを修正済み。
原source/実VM/runner登録/SDK再準備/重Cargo/別PR/mergeは0件。
stock ZIPは公式release APIのURL/hashを確認してpinしたが実payload展開/runner起動は未実施。
元SDKassetは受領済みreceiptを参照したのみ。原diskは読んでいない。

## 出力

日本語で最大6000字。Must/Suggestion/Nitごとにfile:line、具体的trigger、実害と有限な修正案。
不確かな懸念は要実機確認と区別。読めていない対象を合格にせず明記する。
固定base/head/tree、実model、読んだ全file、tests0/静的レビューの区別を必ず返す。
Must0でも元2737や製品CIの完了と扱わない。
