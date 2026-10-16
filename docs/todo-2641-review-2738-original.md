状態: 完了

固定head `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07` に対する、供給された3差分の有限レビューを終えました。結論は Must 1件（条件付き）、Suggestion 2件、Nit 3件です。

- reviewed_base: `9ee640e763091c4688aaf6204e06f9fb1b493695`
- reviewed_head: `b194d66524ef86330bb73ca3f22f9e5dd6ae0a07`
- complete_diff_read: true（index 1〜3 の base/head 本文と patch を読みました）
- 未読の指定ファイル: 0/3
- 未確認: index 2・3 で変更のない大半の領域は、変更点に関わる関数と呼び出し元だけを確認し、全行は監査していません。
- 取得元の照合: owner と blob の SHA1/bytes は、司令塔の再照合値を前提にしました。私はシェルも通信も使わず、再計算していません。
- 保存: 閲覧専用のため、結果と checkpoint は保存していません。保存と反映は親2641と司令塔にお願いします。

## Must

**M1（マージ条件）: index 1（CodeQL workflow）の行77・行117。** 2つの job の実行先が、ホスト型の共通ラベルから専用ランナーのラベルに変わっています。このラベルは特定のToDoとrepoに結び付いており、使い捨てのランナーにだけ存在します。

- このPRをそのまま main に取り込むと、main の push・週次 schedule・PR の CodeQL が、存在しないランナーを待って止まります。
- 今回の公式 run が通ったのは、評価用の候補 branch で専用ランナーを立てたからです。main では再現しません。
- 修正案は2つです。
  - (a) マージ前に2行を元のホスト型ラベルへ戻す。
  - (b) 評価用の差分を別 branch に分け、このPRには入れない。
- 後から切り替えたい場合は、`runs-on: ${{ vars.<変数名> || '元のホスト型ラベル' }}` のように、変数が未設定なら元のラベルに戻る形にします。

## Suggestion

**S1: 実行の範囲。** 公式 run は workflow_dispatch で動きました。PR 専用のステップは、index 1 の `github.event_name == 'pull_request'` の条件（Detect CodeQL-relevant changes と、changes job 内の checkout）で、dispatch では実行されません。そのため、専用ランナーに Node があり、`pull-requests: read` で変更ファイル検出が動くかは未検証です。「CodeQL が動いた」という主張を PR 経路まで広げないでください。

**S2: index 2 の異常系42と脅威表 T17。** 新しい挙動の説明は「実装状況」の1行（行835）にだけあります。異常系42と T17 の試験行にも「request と response で pseudo-header の種類が逆の場合」を足すと、仕様の追跡が一貫します。実装では次の2経路で `HTTP_ADMIT_H2_PSEUDO_HEADER_INVALID` になります。

- response に request 用の pseudo-header がある場合。
- request に `:status` がある場合。

## Nit

**N1: index 1 の先頭コメント。** 専用ランナーを使う理由が書かれていません。S1 の対応を入れるときに、ラベルの由来と戻し方を1行足してください。

**N2: index 3 の行1352。** 未知の pseudo-header（例 `:foo`）の拒否コードが request と response で違います。fail-closed に変わりはありません。

- request は先に pseudo-header 検査を通るので、`H2_PSEUDO_HEADER_INVALID` になります。
- response は先に header 構文検査を通るので、`HEADER_NAME_NOT_TOKEN` になります。
- この差は今回の差分より前からあります。

**N3: index 2 の仕様本文。** 「`UntilClose` を許すのは SSE に限る」と読める記述が残っています。実装と既存テストは、4xx/5xx の `ErrorBody` でも `UntilClose` を受理します（JSON の 2xx は拒否）。今回の差分外で、既存の記述です。

## 差分の判定（静的確認）

index 3 の変更は妥当です。変更前は、response で pseudo-header の名前だけを許可していました。値の検査を通る `:scheme` と `:authority` は response でも受理されていました。`:method` と `:path` は期待値が `None` のため、既に拒否されていました。

変更後は、message の種類ごとの許可集合（request は4種、response は `:status` のみ）に絞られます。この修正は request 側の結果を変えません。request の `:status` は以前も別の経路で拒否されており、拒否コードは同じです。

追加テストが検証しているのは次の点です。

- H2 と H3 の両方で、4種の request 用 pseudo-header を、先頭位置と2番目の位置に挿入した場合の拒否。
- `:status` の重複、欠落、通常 field の後ろへの出現の拒否。
- 正常な response が通ること。

型と借用は、コードを読んだ範囲では成立します。コンパイルとテストは実行していません。

index 2 は、テスト件数の数値を削除する変更と、上記の挙動を説明する1文の追加だけです。この1文は実装と一致し、過大主張はありません。

## 実行結果と未検証

- 実行結果: 私は何も実行していません。新headの native 検査（fmt/Clippy/nextest/doctest）は未実行です。通常CIも、予算による未開始のままと記録されています。
- 既存レビュー5520900843の Must0 の範囲（旧 c53dc14 の native と Spec）は蒸し返していません。今回見たのは、c53→b194 の差と、index 3 の追加差分に限ります。

## CodeQL 37件

固定analysisのSARIFは供給されていません。私の側では場所の対応付けを行えず、各ruleの判定は未実施です。

| 分類 | 件数 |
|---|---|
| 実害を確認できる | 0 |
| テスト用途などで説明できる | 0 |
| 追加検証が要る | 37 |

37件すべてを「追加検証が要る」に置いています。誤検知や Must0 には倒していません。元37件の削除やdismissは行っていません。

静的に言えるのは、供給された本文の範囲だけです。

- index 1 には、キャッシュ利用も、特権トリガー（`pull_request_target` や `workflow_run`）も見当たりません。このため、Actions の27件（direct-cache 2、poisonable-step 25）を index 1 に帰属させる根拠はありません。他の workflow の確認が要ります。
- index 3 には、鍵・IV・salt に当たる固定値が見当たりません。`rust/hard-coded-cryptographic-value` の4件を index 3 に帰属させる根拠はありません。
- JS の6件（sanitization 3、redos 2、shell-command 1）に関係するコードは、供給された差分にありません。

## 親2641へ

- マージ条件は M1 です。解消してから通常のマージ手順に進んでください。
- 37件の分類には、固定analysisのSARIFと非秘密の workflow／JS／Rust ソースを、場所の対応付けができる環境で突き合わせる別工程が要ります。
- 本レビューは固定head `b194d66…` にだけ適用されます。後続のheadへ適用したと推定しないでください。