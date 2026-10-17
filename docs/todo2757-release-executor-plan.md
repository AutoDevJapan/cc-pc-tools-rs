# 固定マージ後の配備CIを動かす実装計画

ToDo2757の次工程。これは実装前の計画であり、runner登録・CI・配備の成功を意味しない。
元2639/2752/他3製品/旧停止・依存の状態は変更しない。

## 確認できた基点

- 対象はrepo1285649124/PR1002。固定mergeは41090d19329b81dd1bd076f3336d723823be4b82。
  tree007b84082008ea9effd5052f63856273aabbfb17、両親はf3d946cbと20c441dfの順。
- 同mergeの公式CodeQL run38941323954/workflow331939928/attempt1は4言語とも実success。
  全4sessionの実HEAD/tree、PID0、container回収、runner不在、lock解放を確認済み。
  旧20cの成功の転記ではない。
- 公式job原logは3件保存済み。Rust job117109408625の原log出力はCLIが端末制御文字で拒否した。
  CLIの保護解除flagや別API/別認証/別取得経路は使わない。拒否と未取得を保持する。
  4件目のrunner原logとcheckout/context/receiptは保存済みだが、公式job原logの代替とは呼ばない。
- promotionはpreconditions、reusable検証2本、promotionの4job。
  full verificationは31step、backend integrationは12step。後者6stepにcompose操作がある。
- 固定YAMLをRAM解析した範囲で、検証2workflowと呼出し側にsecret参照・inheritは無い。
  promotion jobにはsecret参照が30箇所ある。暗黙のGitHub tokenを含む実行権限は別に確認する。
  top-level env、参照script、workflow_call宣言など未確認の伝播も実装前に調べる。
- 固定sourceのPrd environmentを実API照合し、required reviewers0/protection rules0を確認。
  これは顧客契約に人間承認が無い証明ではない。明示契約があれば維持する。
- 現正式SDK imageは89988fa8…、guestはnonroot/RO/cap-drop ALL/no-new-privileges、
  2CPU/8GiB/512pids、tmpfs、host bind無し。Docker socket/daemon無し。

## 変更する製品sourceの範囲

1. 41090だけを親にしたCI修復childを作り、commit/treeを固定する。
2. 3workflowの計4個のhosted runs-onを、repoと役割を固定した専用labelへ変更する。
   preconditions/full/backend/promoteをすべて対象とする。preconditions/promoteをHostedに
   残す案は予算停止を解決しないため採用しない。汎用labelは登録しない。
3. permissions/environment/secrets/if/needs/env/uses/steps/on/concurrencyは保存する。
   compose検査をプロセス起動へ置き換えない。検査削除・skip・手動successはしない。
4. trustの既存正典127dd323…/SHA2566b2285ed…は計算アルゴリズムとして固定する。
   元sourceと候補の実行内容を別モデルが確定してから、同じPRでreviewed digestを更新する。
   raw workflow SHA256143e9ef9…をsemantic digestと同一視しない。
5. アプリ機能/UI、別環境、新費用、Hosted予算、認証範囲は増やさない。

## CC側の受付と実行

- 新しい有限profileをproduct-ciに追加する。原4候補、一般open PR条件、CodeQL受付は維持する。
- `releasePlan`はrepo/PR/固定child head/tree/固定base/役割/workflow digest/run/attempt/
  専用label/正規化compose model digestを保持する。利用者が任意値を選べる入口にしない。
- child PRは同repo/private/admin/nonfork/許可owner、単独parent41090、正確なhead/tree/
  branchを照合する。merge後は既知child、tree、順序付きparents、実mainとの一致を要求する。
  実merge/run/jobを受領後に固定し、未知の最新headへ追従しない。
- O_EXCL排他、登録前queue再照合、hook開始/完了時の再照合、guest実HEAD/treeと認証除去、
  nonceによる所有確認と回収を再利用する。常駐bridgeの固定repoや受付は書き換えない。
- preconditions/full/backendは検証profile、promoteは別profile・別instanceとする。
  役割ごとの専用labelは異なる。promoteは同じ最終sourceの元業務gateと両検証が
  公式successになってからだけ登録する。原drain/smoke/release gateを維持する。
- 検証profileに配備用の秘密情報を渡さない。必要な暗黙tokenの権限は元workflowどおりに限定する。
  registration tokenとcallback鍵は既存と同じRAM/stdin方式で渡し、表示・永続化しない。

## Docker/Composeの有限broker

- guestにhost socketを渡さず、専用Go shimから鍵付きの限定host brokerを呼ぶ。
  外部要求はoperation IDと境界を確認した最小値だけ。任意argv、shell、URL、host pathは禁止。
- brokerは同source/run/attempt/実job/役割を毎回再照合し、その役割の監査済みoperationだけを実行する。
  親が固定sourceから作るsealed operation tableを使い、guestから新table/digestを選ばせない。
- 元のcompose定義とDockerfileをRAMで正規化し、実行内容を独立レビューする。
  image/command/entrypoint/healthcheck/depends_on/env/volume/port/networkを固定modelへ束縛する。
  未知image/tag、privileged、cap_add、host bind、host network、devices、socketは拒否する。
- product sourceはAPIのRAMまたは所有guestの領域だけで扱う。PCの旧product folderや
  新しいproduct source directoryを作らない。symlink/path traversal/多重rootと過大contextを拒否する。
- buildが必要なら固定Dockerfileと固定contextだけを使う。builderも割当資源内で動くことを
  実測・検証する。未検証のprivileged builderや無制限BuildKitを便宜的に有効化しない。
  できなければ該当buildの最小修復を同source範囲で設計し、独立レビューする。
- サービスはnonce付き専用networkとscratchだけを使う。元テストのlocalhost/service-name依存を
  確認し、実通信を維持する。状態/port/healthを実物に照合し、shimが成功を捏造しない。
- 各container/サービス/builderのCPU・memory・pids上限と、同時合計が割当内であることを確認する。
  Rustビルドが実際に必要なら既存共有Cargoゲートを使う。別repo用bridgeの制限を外して流用しない。
- 切断/timeout/正常終了ともguestとサービスを排出し、PID0を確認してからcontainer/network/
  volume/作成imageを回収する。実runner登録不在と全所有資源0を確認した後だけ排他を解放する。
  不確かな作成や回収はreceiptを残し、所有nonceだけを正規回復する。新monitorは作らない。

## 実装前に埋める有限なデータ

1. 全docker/compose呼出しのsubcommand/flag/固定・変数引数/条件と元の出力の利用箇所。
2. compose modelとDockerfileの固定blob、build context、サービス依存、volume/portと実行場所。
3. SDK内の必要toolと版、書込/cache領域、元setup actionの互換性、必要なnative/Cargo処理。
4. 元preconditionsとDEPLOY/script/smokeの実drain手順・検証可能な終了条件・確認flag。
5. workflow secret/token/環境の全伝播と、顧客固有の人間承認の有無。
6. trust正典のsemantic計算/受領書の保存場所と、役割ごとの実workflow/run/job ID。

不明値を仮定して許可tableを作らない。この計画だけでprofileを有効化しない。

## 実装・受入・完了

- 変更対象のGo検査と負例、実正常/切断/timeout/不確かなcreate/回収のfixtureを実行する。
  異repo/head/tree/parent/run/attempt/role/digest/鍵、未知operation/argv/資源指定を拒否する。
- source commit→通常PR→別モデルMust0→必要CI→通常merge→正式配布を守る。
  未配布版、即席runner、拒否後の別経路で製品jobを起動しない。
- その後、子の公式CodeQL/secret/元full/backend/releaseと業務gateを実行する。
  steps0/skipped/受付/登録だけを成功に数えない。同sourceの既済検査は重ねない。
- 実際に必要なdrainを行い、既存Prdだけを正規配備する。実runtime revisionとreadiness/smoke、
  原log bytes/hash、全所有資源の回収を確認する。Rust原logの拒否は未完として別に保持する。
- 完了条件が揃った実結果を2752へ配送する。製品全体や親をこの工程でdoneにしない。
