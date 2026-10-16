# GoDD MCP αのMust修正だけを確認する

元2641の有限read-only別モデルレビューです。既済2738の3ファイルレビューを繰り返しません。対象はb194→012150の追加差分2ファイルだけです。シェル・通信・ファイル保存・ToDo操作・CI起動・コード変更を行わず、供給した差分だけを読み最終応答を返してください。これは複数ファイルなのでHaikuではなくSonnetを指定します。子Codexを直接起動しません。

reviewed_base: b194d66524ef86330bb73ca3f22f9e5dd6ae0a07
reviewed_head: 012150cb853c1366b79783b25f5d825aef0c9824
candidate_tree: 99c3a9a454088e4ddb01a02595cfddc3d435192f
原repo_id1154066087、実owner AutoDevJapan/private/admin/nonfork/sameRepo head+baseを実装側がAPIで確認しています。原PR3613と元branchは旧b194を保持し、旧固定検査2737は止めません。候補は同repoの codex/todo2641-codeql-route-fix-20261017 へ保存した実commitです。新しいPRやmerge/Prdは行っていません。

## 仕様と既存レビュー
元2738はb194にMust1/Suggestion2/Nit3。Must M1は、CodeQLの専用一時runner labelがmain push/週次/PRへ残るとrunner不在で停止することです。今回はレビュー案(a)に従い、2つのruns-on scalarを元mainのubuntu-latestへ戻します。workflow全体のblobは元mainと同じd1d593aad61699a96e6d2a131d8c1faea584782dです。条件・権限・4言語・queries・concurrency・gate・タイムアウトを変えていません。元のHosted予算未開始失敗は解消済みとはしません。
S2はHTTP/2・HTTP/3のrequest/response用pseudo-headerを逆にした拒否をSpecの異常系42とT17でも追跡すること。仕様の2行だけを追記しました。runtimeやnative/Rust/JSには今回追加変更がありません。原Issue1100はopen、純粋coreでI/O/runtime/MCP/npm未接続です。配信prompt/mindset/seedを追加しません。

## 実確認と未確認
GitHub compareで追加差分は以下2fileだけ。元mainから候補への全差分はnativeとSpecだけで、.github差分0。APIでnativeの元b194と候補blobが同じことを確認。旧c53のnative受入/新b194のCodeQL解析成功を、新012150の実行成功へ読み替えません。候補のCI/CodeQL/関連検査は未実行です。37件の解析指摘も今回のreviewの対象外で、未判定のまま残します。

## 供給差分（全2件）

### .github/workflows/codeql.yml
```diff
@@ -74,7 +74,7 @@ jobs:
   # opt-in PRだけ既存のdeny-by-default path detectorを呼ぶ。
   changes:
     name: Detect CodeQL-relevant changes
-    runs-on: cc-product-2648-1154066087-codeql
+    runs-on: ubuntu-latest
     timeout-minutes: 5
     outputs:
       requested: ${{ env.RUN_CODEQL_ANALYSIS }}
@@ -114,7 +114,7 @@ jobs:
     # 変更判定失敗はfull解析へ倒すが、run自体のcancelは必ず尊重する。
     # mainは直前差分に依存しない。通常PRの明示的requested=falseだけを省略する。
     if: ${{ !cancelled() && (needs.changes.result != 'success' || needs.changes.outputs.requested != 'false') && (github.event_name != 'pull_request' || needs.changes.result != 'success' || needs.changes.outputs.codeql != 'false') }}
-    runs-on: cc-product-2648-1154066087-codeql
+    runs-on: ubuntu-latest
     timeout-minutes: 30
     strategy:
       fail-fast: false
```

### documents/spec/cursorlens-proxy-http-message-boundary-admission-design.md
```diff
@@ -535,7 +535,7 @@ F8 は「usage 欠損時に 0 を実測として採用する」「経路によ
 39. 展開後バイト数 / 受信バイト数が `max_decoded_body_ratio` 超過: `HTTP_ADMIT_DECODED_RATIO_EXCEEDED`
 40. `Connection` が framing 決定 field（`Content-Length` / `Transfer-Encoding` / `Host`）を列挙: `HTTP_ADMIT_CONNECTION_NAMES_FRAMING_FIELD`
 41. adapter が caller 由来 field を outbound 集合へ入れようとした: `HTTP_ADMIT_OUTBOUND_HEAD_NOT_GENERATED`（内部不変条件違反）
-42. h2 / h3 の pseudo-header が重複・欠落・出現順違反: `HTTP_ADMIT_H2_PSEUDO_HEADER_INVALID`
+42. h2 / h3 の pseudo-header が重複・欠落・出現順違反、または request と response で種類が逆: `HTTP_ADMIT_H2_PSEUDO_HEADER_INVALID`
 43. h2 / h3 message に connection-specific field が出現: `HTTP_ADMIT_H2_CONNECTION_SPECIFIC_FIELD`
 44. response head が上限超過: `HTTP_ADMIT_RESPONSE_HEAD_TOO_LARGE`
 45. response の `Content-Type` が欠落・重複・許可集合外で分類不能: `HTTP_ADMIT_RESPONSE_CONTENT_TYPE_UNCLASSIFIABLE`
@@ -579,7 +579,7 @@ Issue の受け入れ条件 3 本と検証節（parser/property/fuzz test、smug
 | T14 | SSE event の肥大化 / 無限 event / 終端欠落 | 上限なし | `max_sse_event_bytes` / `max_sse_events` / `require_sse_terminator` | fault injection: 各 1 件。終端欠落では**既に流したバイト列は取り消さず**、outcome が記録され connection が再利用されないことを確認（L5 の明示的検証） |
 | T15 | client lane と telemetry lane の内容乖離 | text chunk のみ client へ、tool call は telemetry のみ（F7） | `AdmittedResponse` は 1 回だけ構築され両 lane が共有（D7） | 差分テスト: 同一 response に対する 2 lane の内容を比較し、**telemetry にのみ存在する内容が無い**こと。tee の buffer 挙動は #792 の責務として本テストの対象外とする |
 | T16 | 偽 usage metadata / 単位不整合 | usage 欠損を 0 とし、経路により cost が 10^6 倍ずれる（F8） | `Option<u64>` による欠損保持、boundary で金額へ変換しない、observed counter との並置（D9） | 単体テスト: usage 欠損時に receipt が `null` を出し `0` を出さないこと。**receipt schema に金額フィールドが存在しない**ことを型テストで固定。乖離時に `HTTP_ADMIT_USAGE_UNRECONCILED` が情報として記録されるが拒否されないこと |
-| T17 | HTTP/2 / HTTP/3 経路での判定の抜け | 該当なし（upstream は runtime 任せ） | L2 の写像表、および小文字 field name / `:authority` と `host` の一貫性 | h2/h3 test: pseudo-header 重複 / 欠落 / 出現順違反、`transfer-encoding`、DATA frame 合計長と `content-length` の不一致、大文字 field name、authority userinfo、重複・不一致 `host` を拒否。H2 は scheme 正規化で等しい authority/host を受理し、H3 は両 field の値が同一の場合だけ受理することを確認 |
+| T17 | HTTP/2 / HTTP/3 経路での判定の抜け | 該当なし（upstream は runtime 任せ） | L2 の写像表、および小文字 field name / `:authority` と `host` の一貫性 | h2/h3 test: pseudo-header 重複 / 欠落 / 出現順違反、request と response の pseudo-header 種類の取り違え、`transfer-encoding`、DATA frame 合計長と `content-length` の不一致、大文字 field name、authority userinfo、重複・不一致 `host` を拒否。H2 は scheme 正規化で等しい authority/host を受理し、H3 は両 field の値が同一の場合だけ受理することを確認 |
 | T18 | 正当な client の誤拒否（false positive）と npm 互換性 | — | `profile: strict` の既定値が実 client を通すこと | 回帰 corpus: Cursor IDE / Claude Code / Codex / Kimi CLI / Antigravity IDE が送る実 request head を GoDD 側で採取した集合（upstream の fixture は複製しない）。**全件 ALLOW** であること。加えて npm compatibility test: TypeScript 層が header 操作 API を export していないこと、既存 MCP stdio 契約が変わらないことを確認 |

 fuzz test は `cargo-fuzz` で `decide_request_head` / `decide_body_framing` / `decide_outbound_head` / `decide_response_framing` / `project_headers` の 5 純粋関数を対象とする（いずれも I/O を持たないため fuzzing が容易であることが、純粋関数へ分離する設計上の副次的な利点である）。不変条件は「panic しない」「`AdmittedRequest` / `AdmittedResponse` / `OutboundHead` を構築しない」「`HeaderProjection` の key 集合が allowlist の部分集合である」の 3 点。
```

## 返答
日本語でreviewed_base/head、complete_diff_read、読んだ差分2件、Must/Suggestion/Nitと具体的修正を返してください。M1の原因がこの候補で解消するかを明示し、静的妥当性と未実行のCIを区別してください。既済HTTP修正全体や37件を再監査せず、追加差分だけ確認してください。元2738の原応答・取得元/最初の失敗は保持します。終了時は最終返信だけを返し、保存不能を理由に繰り返しません。結果保存は実装側が元jobのlast_message.mdから行います。新費用/APIキーAI/GPU/本人PC操作/Terminal/UACは禁止です。
