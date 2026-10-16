状態: 完了

PR175 のレビュー結果は Must 0件です。レビュー本文は保存できませんでした。下の本文をそのまま保存してください。

保存先の `D:/CC/Work/unattended-operations-20261013-wt/todo-2642-godd-press/REVIEW-2642-ORIGINAL.md` への書き込みを、読み取り専用の環境が拒否しました。今回の依頼は閲覧のみなので、別の経路での書き込みはしていません。

`gh` はこの環境で DLL 初期化エラーになり動かなかったため、GitHub コネクタの読み取り機能で同じ内容を確認しました。

# PR175 独立レビュー（ToDo2642 / 読み取り専用）

## 結論

**Must 0件**。execute.rs の修正は正しく、回帰テストも意図どおりです。CodeQL workflow の差分は runs-on 2行だけで、元の意味を保っています。CI完了・マージ可・リリース可・アラート無害とは判断していません。

## 確認した対象

- AutoDevJapan/GoDD-Press は ID1386702183、private、admin権限ありです。コネクタは fork 項目を返さないため、非forkは今回未確認です（親2642の確認値を保持）。
- PR175 は open / draft / mergeable です。head は開始時も終了時も b2b6d47（base 4dc5c33）で、移動はありません。
- compare は 3ファイル +54/-4、2コミットです。
- 参照は候補SHAの GitHub 読み取りだけです。ローカルのソースツリーは読んでいません。

## 正しさ

- execute.rs:116-117 を `parent.expected_execution_version.checked_add(1) != Some(plan.expected_execution_version)` に変更しています。i64::MAX では None になり、既存の `predecessor_binding_invalid` に入ります。terminal_in → fence → commit の既存経路を通るので、公開・記事・サイト版は動きません。
- 破損値が届く理由は次のとおりです。execute は先行予約を `load_in` で読むだけで、integrity を通しません。integrity() は i64::MAX を拒否し、hash にも含めます。
- mod.rs の validate()/validate_result()（213, 245行）の `+1` と、store.rs:192 の chain_anchor_in の `+1` は、先に integrity/validate を通るため安全です。
- Cargo.toml のリリース設定に overflow-checks はありません。修正前でも MAX+1 は i64::MIN に巻き戻ります。後続側の期待版は integrity で1以上と保証されるため、リリースでも結果は同じ `predecessor_binding_invalid` になります。
- つまり、修正前の panic は debug/test プロファイル限定です。回帰テストは overflow-checks 有効の実行でだけ RED になり、ネイティブの RED 実績（panic FAIL）と矛盾しません。
- execute.rs の他の先行比較は due_at の比較と `saturating_add(30)` だけで、追加の算術はありません。

## テストの確認

- worker.rs の claim_next は、期限内の running ジョブを選びません。先行予約を600秒、後続を+300秒で claim するため、後続の claim では先行側が選ばれず、後続が選ばれます。テストは plan_id でもこれを確認しています。
- corrupt_plan（許可リスト追加は tests:347、新テストは 2421-2466）は合成DBだけを使います。通常UPDATEの拒否 → トリガー DROP → 更新 → 同一SQLで復元 → 同一性確認という既存の手順を再利用しています。
- business_state（sites/themes/pages/revisions/journal/blog）の前後比較を行っています。published が None であること、先行予約が (pending, i64::MAX) であること、jobs が2件であることも確認しています。panic は execute の unwrap で検出されます。

## Suggestion

1. tests:2420-2466: jobs・audit_events・publication_schedules は business_state の対象外です。次を足すと「他を変えない」が強くなります。
   - 後続予約の完了時刻と結果列が空であること。
   - 監査 `site.publication_schedule_stopped` の理由が `predecessor_binding_invalid` であること。
   - 先行側のジョブが running のままであること。
2. 同テストは finish_job を呼びません。後続ジョブの完了 ACK と reconcile_terminal_jobs_in（worker.rs:307）の後も先行予約が pending のままかは未検証です。`complete()` 相当の呼び出しで足せます。
3. corrupt_plan の最初の `.is_err()` は、拒否理由がトリガーかを確認しません。エラーメッセージの照合を足すとよいです。
4. store.rs:192 と 361 の `+ 1` は今回の差分外です。192 は validate 後で安全です。361 は site.version が i64::MAX に破損した場合だけ debug で panic し得ます。checked_add への統一は別PRで足ります。

## Nit

- テスト名の「without panicking」は、リリースでは修正前も panic しない点をコメントで補うと誤解がありません。

## CodeQL workflow

- 差分は analyze と gate の runs-on を `ubuntu-24.04` から `cc-product-2648-1386702183-codeql` に変えた2行だけです。次の項目は元のままです。
  - 言語4つ、default queries、threat-models remote
  - permissions、イベントとブランチ、category、gate の判定、timeout
  - SHA固定の action
- 除外・dismiss・手動の成功ステータスはありません。
- 公式 workflow_dispatch の run 38893812340 は成功です。ただし同コミットの pull_request イベントの CodeQL run 38893666832 は、5ジョブすべて cancelled です。ステップがなく、原因は読み取りでは分かりません。PR上の CodeQL チェックが成功した証拠はないため、マージ前に専用ランナーで再実行が必要です。
- 専用ラベルが自己ホスト型ランナーなら、PRのコードを実行します。ephemeral か、PRごとに作業領域が掃除されるかは未確認です。親で確認してください。
- 既存アラート9件（JS4/Rust5）は未判定のままです。base と同一でも無害の根拠にはしません。SARIF の中身は読んでいません。

## 未検証・外部事項

- b2 でネイティブ検査は再実行していません。アプリとネイティブの木は a39a2b1 から不変で、b2 は `.github` だけの変更です。
- rust-core と junction-fixtures は、Actions 予算のため開始されないままです。通常CIの完了は未達です。
- マージ・リリース・メディア権利の判断は行っていません。
- レビュアーとして PR・状態は一切変更していません。