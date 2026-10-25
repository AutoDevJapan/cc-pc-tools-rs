# 3255の実行context抽出の先行実装

起点は元3255。元Issue446/PR448とPR486の受入条件は変更しない。
実データの読み口を接続しない内部ロジックを扱う。初回の準備はPC内の保存までとし、
その後、元の作業ブランチを保持してmainからこの3ファイルだけを別ブランチへ移した。
この差分だけを正式CIと既存監査役の読取レビューへ進める。共有ToDoやworkerを増やさず、
過去に読了したレビューは再起動しない。受領前にレビュー・CI・通常merge済みと扱わない。

## 入出力と境界

入力は将来の正規producerから渡される、範囲を限定済みのUTF-8 JSONLメタデータ。
内部関数はio.Readerだけを受け、ファイル・ネットワーク・認証情報を開かない。
CLI入口、任意path引数、原store/nativeのreader、guard分岐は追加しない。

呼出側が別途結ぶnative thread ID、native turn ID、開始・終了時刻に対して、
session_metaのidとturn_contextのturn_idを完全一致で照合する。
session_idやroot_turn_id、開始前context、別turnの値で補完しない。
入力全体を上限1MiB、1行64KiB、JSON深さ64以内に限定し、重複keyを拒否する。
読取失敗・途切れた末尾・破損・曖昧な複数contextでは観測値を一切返さない。

唯一の対象contextを読み終えた場合だけ、modelとeffortを別々に抽出する。
同caseの既知model IDであるgpt-6.1-solとgpt-6-lunaをそのまま返す。
要求modelとの一致で観測を捨てたり、aliasを変換したりしない。
不明なmodel値を出力せず、欠落/null/不正/未認識を区別する。
effortも実際の欄だけを使い、欠落時にmaxを補わない。providerは推定しない。

出力は限定した観測項目だけで、本文、path、native識別子、認証値を含めない。
source_qualified、execution_verified、access_grantedは常にfalse。
native IDと時刻の入力整合を、元assignment/応答との同一性や閲覧資格へ昇格させない。

## 合成データでの受入条件

- Given: 同じnative thread/turnと時刻内の唯一のcontext。
  When: 観測modelが要求側と異なり、観測effortがlow。
  Then: 実欄のmodelとlowを返し、要求値・設定値へ置き換えない。
- Given: session_id/root_turn_idだけが一致、別turn、開始前/終了後、または欠落欄。
  Then: 対象観測として採用せず、欄の欠落を別値で補わない。
- Given: 途中読取エラー、破損、重複key、複数の対象context、上限超過。
  Then: 途中まで見えた値も返さずunavailableとする。
- Given: 本文や私的値に似せた合成の未知field。
  Then: 出力の固定key集合に追加されず、資格・完了を主張しない。

## 形式の根拠と残件

既存cc-jobsのpoll_session/observe_turn_contextと、公式
[Codex rust-v0.163.0 TurnContextItem](https://github.com/openai/codex/blob/rust-v0.163.0/codex-rs/protocol/src/protocol.rs#L3417-L3457)
を参照した。turn_idはoptionalなので、無い記録を時刻だけで一致させない。
[SessionMeta](https://github.com/openai/codex/blob/rust-v0.163.0/codex-rs/protocol/src/protocol.rs#L3245-L3254)
のidと、root threadを示すsession_idも分ける。
この公開形式を、PC内の版一致や元実行の証明へ使わない。

正規producerの所有/source/配布対応、元assignment/応答とnative IDの正式な対応、
権限を維持する限定reader、別モデルの実行証明、レビュー、CI、通常merge、配布、
実データ受入は未完。この内部関数だけでは歴史的4559の取得を完了しない。
PR486の固定f727/正式13success、元PR448のf340/正式6success/読了M0、
実四件比較0・固定二件snapshot0と、原仕様・本人回答・未知依存を保持する。

## 元4559との入力整合を確かめる接続処理

将来のproducerが渡す対応情報について、元4559のassignment・bot・task・原応答ID、
開始終了時刻・実行回数・completed/doneが保存済みの値と完全一致した場合だけ、
渡されたstream取得関数を一度呼ぶ。native thread/turnの値は欠落・同値を拒否する。
stream自身が返す対応情報を読取前後に照合し、取得・読取・再照合・closeのエラー、
または情報の変化があれば途中の値を返さない。取得済みstreamは終了までに閉じる。

この処理もファイルやネットワークを開かず、CLI入口を持たない。対応情報の一致は
呼出元の申告同士の整合だけで、資料の真正性・原本の不変性・同時差替えへの原子的な
保証ではない。source_qualified、execution_verified、access_grantedはfalseを維持する。
正規producerによる資料の資格確認と原実行/nativeの正式な対応の取得は別途必要である。

- Given: 元実行の固定情報とstream側の対応情報が一致し、有効な対象contextがある。
  Then: そのcontextの値だけを返し、streamを一度だけ閉じる。資格や実行証明は返さない。
- Given: 原応答・時刻・回数などが異なる、またはnative識別子が無い。
  Then: stream取得関数を呼ばずunavailableとする。
- Given: 読取前後に対応情報が変わる、照合/closeが失敗する、または読取が失敗する。
  Then: 取得済みstreamを閉じ、部分的に見えた観測値を返さない。

## OMBの通信記録からnative識別子の候補を得る処理

公開producerの `server/drivers/native.ts` は通信を `at/dir/source/msg` で包む。
`codex.ts` の thread/start・thread/resume、turn/start に対する応答には、返された
native thread/turn IDがある。この包みをCodex本来のcontext JSONLとして扱わない。

新しい内部関数はio.Readerと開始終了時刻だけを受け、範囲内の同じRPC IDで結ばれた
thread応答→turn要求と応答→同じthread/turnの正常終了を順番に確認する。
返された識別子が無いときに、要求のthread IDやsession.startedで補わない。
RPC失敗、別のturn、重複・複数の実行、途中の読取エラー、破損・境界超過では
途中で得た識別子も捨てる。入力1MiB、行64KiB、深さ64、重複key拒否を維持する。

戻り値は非公開の識別子候補だけで、model・effort・要求本文は取り出さない。
ファイル・通信・認証を開かず、CLIやJSON出力を追加しない。識別子候補を元4559の
対応情報へ自動的に付けたり、contextを合成したりしない。原記録の資格、元assignment・
応答との対応、配布版の一致は別途必要で、これだけで正式な対応を証明しない。

- Given: 対象時間内で要求IDと各応答が一致し、同じnative thread/turnが正常終了する。
  Then: 応答に実際にある識別子候補だけを返す。要求model/effortは返さない。
- Given: 要求だけ、応答ID違い、返却thread欠落、別thread/turnの終了、複数の実行。
  Then: 候補を返さず、要求値から補わない。
- Given: 正常な組の後に読取失敗・破損・重複key・上限超過がある。
  Then: 先に見えた候補も返さない。
