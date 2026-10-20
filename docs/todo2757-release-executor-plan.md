# 固定マージ後の配備CIを動かす実装計画
> 2026-10-20の実結果: 元の公式Hosted経路が固定41090で実行でき、元release
> run38941324877/attempt2と既存Prdへの配備・smokeが成功した。この作業では予算を
> 増やしていない。以下は予算停止が続いた場合の実装前の案として保持する。
> 現在の完了条件を満たすためだけに新runner、CI child、brokerを追加しない。
> 実結果と残る証拠上の制限はtodo2757-pr205-delivery.mdの先頭を参照する。

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
4. trustの既存正典blob127dd323392582c4082060e1c1a7b42f13d6085c/
   SHA2566b2285edc2756d9810b651d874fb6579ab10e788a2f900b3ce326c14e9ac60efは
   baseの計算アルゴリズムとして固定し、変更候補が自身の基準を書き換えないようにする。
   元sourceと候補の実行内容を別モデルが確定した原応答を先に保存する。
   その後に固定baseの同じ関数で独立再計算し、指示どおり同じPRの別コミットで
   reviewed digestの受領値だけを更新する。旧基準と応答hashを保持する。
   計算アルゴリズム自体の変更が必要なら、信頼境界の変更として別の独立レビューを行う。
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
  ただし元の全権限・伝播が未確認の間はprofile登録を拒否する。read-onlyと推測して登録したり、
  元workflowのpermissionsを便宜的に書き換えたりしない。検証に必要な最小権限と元の明示権限が
  両立することを確認し、その固定profileだけを受け付ける。

## Docker/Composeの有限broker

- guestにhost socketを渡さず、専用Go shimから鍵付きの限定host brokerを呼ぶ。
  外部要求はoperation IDと境界を確認した最小値だけ。任意argv、shell、URL、host pathは禁止。
- brokerは同source/run/attempt/実job/役割を毎回再照合し、その役割の監査済みoperationだけを実行する。
  親が固定sourceから作るsealed operation tableを使い、guestから新table/digestを選ばせない。
  実jobはhostが保持する登録nonce/runner IDとGitHubのjobs応答から一意に特定する。
  guestのjob/run/attempt申告だけで同一性を確定しない。
- 元のcompose定義とDockerfileをRAMで正規化し、実行内容を独立レビューする。
  image/command/entrypoint/healthcheck/depends_on/env/volume/port/networkを固定modelへ束縛する。
  未知image/tag、privileged、cap_add、host bind、host network、devices、socketは拒否する。
- product sourceはAPIのRAMまたは所有guestの領域だけで扱う。PCの旧product folderや
  新しいproduct source directoryを作らない。symlink/path traversal/多重rootと過大contextを拒否する。
- buildが必要なら固定Dockerfileと固定contextだけを使う。builderも割当資源内で動くことを
  実測・検証する。未検証のprivileged builderや無制限BuildKitを便宜的に有効化しない。
  できなければ該当buildの最小修復を同source範囲で設計し、独立レビューする。
  contextはhostが固定API tree/blobから検証した全byteをRAM内で封印する。
  build直前に全context/各blobのhashを再照合し、その同じ封印byteをbuilderへ渡す。
  guestが書き換えられるcheckout、guest申告hash、再読したpathからcontextを作らない。
- サービスはnonce付き専用networkとscratchだけを使う。元テストのlocalhost/service-name依存を
  確認し、実通信を維持する。状態/port/healthを実物に照合し、shimが成功を捏造しない。
- 各container/サービス/builderのCPU・memory・pids上限と、同時合計が割当内であることを確認する。
  Rustビルドが実際に必要なら既存共有Cargoゲートを使う。別repo用bridgeの制限を外して流用しない。
  新検証profileの同時合計上限は2CPU/8GiB/512pidsとし、guest/サービス/builderへ事前に分配する。
  各実配分と各phaseの合計を固定operation tableに含め、実測前には有効化しない。
  元CodeQL profileの実2CPU/8GiB/512pidsは今回の変更対象にしない。
- 切断/timeout/正常終了ともguestとサービスを排出し、PID0を確認してからcontainer/network/
  volume/作成imageを回収する。実runner登録不在と全所有資源0を確認した後だけ排他を解放する。
  不確かな作成や回収はreceiptを残し、所有nonceだけを正規回復する。新monitorは作らない。

### broker鍵とテスト実行者の分離

- broker鍵はhostが各run/attempt/runner instanceに発行し、host登録記録へ束縛する。
  product sourceやテスト実行者のenv/argv/読み取れるfileには置かない。
- guest内の転送agentが必要なら、テスト実行者UID1001とは異なる非root UID1002で動かす。
  hostからagentのstdinへRAMで渡し、UID1002専用領域とprocessにだけ保持する。
  同UID化、setuid/cap追加、host socket公開はしない。実kernelで他UIDの/proc/fd/memoryと鍵が
  UID1001から読めないことを確認するまでは、この案を有効化しない。
- テスト側shimに渡すのは有限operation IDと検証する最小値だけ。
  テストがshimを直接呼んでも固定profileを超えないことを負例と実物で確認する。

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
  この原logの要件も完了条件に残す。拒否の迂回や成功への置換では閉じない。
- 完了条件が揃った実結果を2752へ配送する。製品全体や親をこの工程でdoneにしない。

## gateに数える実結果の対応

| source | run/attempt | gateへの算入 |
|---|---|---|
| 41090 | 38941323954/1 | このsourceのCodeQL4言語だけ。実successを保存済み |
| 41090 | 38941324877/1 | budget停止/steps0。業務preconditions/release成功には算入しない |
| 固定したCI child | 実取得して固定する | そのchildのPR/CodeQL/secret検査。未来のrunは未実行 |
| 固定したchild merge | 実取得して固定する | そのmergeの元業務preconditions/full/backend/release/Prd照合 |

base mainが41090から未知の木へ進んだら登録・配備を止め、原結果を保持する。
既知childの正確なmergeだけを別の固定receiptで照合し、他headへ追従しない。
PID0は停止後のDocker inspectでRunning=falseかつState.Pid=0を確認する意味。
元業務gateはsourceにあるdeployment有効化・drain・必要CIなどの実preconditionsを指し、
budget停止の注釈をその評価結果とは扱わない。
