# 回答済みの新しいDiscord質問を通常の照会で見つける

元ToDo3392。本人が2026-10-23に報告した再質問障害の有限修正。
既存の司令塔、会話保存、回答配送、画像readerは増設しない。

## 再現

新しい会話で質問・本人回答を保存し、`conversation replies --output` の
正規API照合で回答を確認できても、通常の `lookup-decision --decision-file`
は旧質問経路だけを検索し `found:false` を返す。
2026-10-23の元2497に対する現在版の本人回答で実機再現した。
私的原票は担当worktreeの `.ci-work` に保持し、個人情報はGitへ入れない。
取得成功と担当への配送・実読・適用・質問終了は別の段階である。

## 仕様

- 既存の `conversation-mode.json` が有効な場合、通常の `lookup-decision` は
  新しい質問の正規保存記録を検索する。無効なら旧経路を保つ。不正設定や
  読取失敗を「該当なし」にしない。旧台帳の初期化・復元・一斉移行はしない。
- 設定済み本人・guild・forum、元ToDo、具体的資源、固定の判断種類、完全な
  判断範囲が一致する質問だけを選ぶ。既存の正規化を再利用し、担当・job・題名・
  言い換え・子ToDoの呼出し情報を新判断にしない。入力の `todo_id` は元番号を保つ。
- 質問があれば、新規投稿せず元URL・元質問・元判断範囲と現在の正規本人回答を
  既存のread-only返信処理から返す。自由記述・空本文・メンションだけの返信も
  勝手にa/bへ解釈しない。添付の意味は既存readerで担当が確認する。
- 返信の編集版を既存の版情報とともに保持する。受領や回答の存在を実装承認・
  実読・実反映・質問終了へ読み替えない。後の訂正・停止も現在の回答として返す。
- 質問が送信準備中なら `found:true` と再送抑止を返し、別の質問を作らせない。
  複数の正規候補やAPI確認失敗はエラーにし、推測で一つを選ばない。
- 照会はGETだけを使う。新しいロック・会話記録・受領印・状態を作らず、担当を
  起動せず、投稿・メンション・closeを増やさない。既存askの排他と重複抑止を保つ。
- 新しい仕様・危険範囲・画像版を判断範囲へ明記した質問は一致させない。
  本文の類似度で承認を流用しない。

## 受入

1. Given 新方式で回答済みの質問、When 通常lookupを別担当/再起動から繰り返す、
   Then 元URL・元質問・本人回答と版が戻り、保存bytes・投稿・メンションが増えない。
2. Given 新しい資源・判断範囲・画像版、When lookupする、Then 他scopeの回答を
   返さず、新しい判断を求める既存askの動作を保つ。
3. 未回答・送信準備中・後の自由記述訂正・空/mention本文・アーカイブ後の返信、
   破損状態・偽の本人/guild/forum・API失敗を選んだ関連テストで確認する。
4. 別モデルレビュー・関連CI・通常マージ後、配布版の通常lookupで実例を再照合する。
   送信なしの成功と既存の受領数を比較し、回答を元担当の実操作へ接続する。
   配送・実読・適用・closeが未確認なら、その段階を未確認と報告する。

新しい履歴収集器や回答配送の並行修正はしない。3110/3153/3327/3387と
2457→2467の既存工程・原票・待機を保持する。

## ToDo3396による固定版のテスト結果（2026-10-23）

指定されたRed/GreenテストとWindows用releaseビルドを完了した。
これはレビュー3397のMust修正前の結果であり、配布を許可する結果ではない。

- `tested_head=f80470bd6cd5d24e4e078ffdf9eab28d4980b917`
- `release_eligible=false`
- `must_remaining=2`
- base: `17c62fa6fe628951152b8933512f6dfb04d7df45`
- Red: `26ccd51bf733b76bb50ff7e14a12b911d40128b4`
- Green tree: `1b2e0bbfb1fc3fb11369cad42191cc65610027d6`
- 実行ジョブ: `20261023-091818-todo-3396`
- 保存ブランチ: `scheduler/todo-3396-3392-check`

親3392のworktreeと共有runtime/stateを編集せず、自分のworktree内で実行した。
既存のCargoゲートとsccacheを使い、一つのtargetで以下を順に実行した。
全工程の開始・終了時に、指定したHEADとtrackedファイルの変更なしを確認した。
実装・テストの修正はない。この結果追記のコミットはビルド後の文書変更であり、
候補実行物のソースHEADは上記の固定Greenである。

| 工程 | 実コマンド | 実終了コード | 結果 | 全ログの接頭辞 |
| --- | --- | --- | --- | --- |
| Red | `cargo nextest run --locked -p cc-ask -E 'test(normal_lookup_finds_current_answer_without_writes_or_another_question)'` | 100 | 1件失敗、838件対象外 | `red` |
| Green | `cargo nextest run --locked -p cc-ask -E 'test(recovery_messages::) \| binary(decision_identity) \| binary(conversation_bridge)'` | 0 | 141件成功、698件対象外 | `green-nextest` |
| doctest | `cargo test --locked -p cc-ask --doc` | 0 | 対象0件 | `green-doctest` |
| Clippy | `cargo clippy --locked -p cc-ask --all-targets -- -D warnings` | 0 | 成功 | `green-clippy` |
| release | `cargo build --locked --release -p cc-ask --bin discord_ask` | 0 | 成功 | `green-release-build` |

Redはコンパイル成功後、指定テストの`found`に対するassertionで失敗した。
実際の値は`Bool(false)`、期待値は`true`、失敗文は
`normal lookup lost the new conversation answer`であり、別のコンパイルエラーではない。
Red起動前に記録スクリプトの空ブランチ名の扱いを修正したが、Cargo未起動のため
テストの失敗やソース修正には数えない。

原ログと候補は、次の所有記録付き領域に保持する。

```text
D:/CC/Work/unattended-operations-20261013-wt/todo-3396-rust/codex-owned-3c742e5e873a79d827750b0433bcd2ca/
```

各接頭辞の`.stdout.log`と`.stderr.log`が省略のない原出力で、対応する`.json`に
実argv・開始終了時刻・HEAD・終了コード・出力byte数・SHA256を保存した。
Redのstderrは6,595byte、Greenは19,999byte、doctestはstdout113byte/stderr93byte、
Clippyはstderr4,318byte、releaseはstderr5,045byte。それ以外のstdoutは0byte。
`test-results.json`は原ログから作った件別結果、`run-summary.json`は全工程の記録である。

候補の絶対パスと識別情報は以下のとおり。共有ツールへの配布は実施していない。

```text
D:/CC/Work/unattended-operations-20261013-wt/todo-3396-rust/codex-owned-3c742e5e873a79d827750b0433bcd2ca/candidate/discord_ask.exe
SHA256: 51e035867c447956db07c39028d057532399ec6643dcee47d25cfbe8a7736c59
size: 11952640 byte
```

`release-input-before.json`と`release-input-after.json`は、Windows用Cargo metadataから
normal/buildの推移的依存149件と、同worktree内の6クレートを選んだ入力記録である。
6クレートはcc-ask、cc-chrome、cc-job-policy、cc-live、cc-local-llm、cc-task-title。
各クレートのテスト専用ファイルも含む保守的な範囲で、tracked入力177ファイルの
実bytesのSHA256、Cargo.lock、関連Cargo設定のfingerprintがビルド前後で一致した。
registry依存はCargo.lockの版・source・checksumで固定し、cache内の全ソースを
独立に再ハッシュしたとは主張しない。metadata、releaseのdep-info/fingerprint、
名前を限定したコンパイル設定のfingerprintも原ログ領域に残した。
実ツールはrustc/cargo 1.95.0、hostは`x86_64-pc-windows-msvc`だった。

Mustは、本人返信・元質問・`original_decision`の出力に伏せ字処理がない点と、
Verified未確認のPending/Postedを`delivery:not_sent`と返す点の2件である。
親3392が修正後の新HEADで関連テスト、Clippy、ビルド、別モデルレビュー、CIを確認し、
最終候補の入力を照合してから通常配布と実受入を行う。この固定版の成功を流用しない。

不要な自分のtargetは8,364,717,124byte、4,349ファイルだった。清掃ツールのplanが
容量を理由に退避を指定し、標準quarantineは`QUARANTINE_CROSS_DRIVE`で事前停止した。
同ドライブ退避手順で祖先・配下のreparseなし、trackedなし、全Cargo終了を確認し、
`D:/削除希望/20261023-todo-3396-cargo-target-8ab4cdc9/owned-target`へ通常移動した。
元の場所の不在と移動後の件数・容量一致を確認した。`理由.txt`と保存一覧を残し、
実削除・容量回復は未実施（0byte）として既存清掃へ引き継ぐ。本人判断は不要。
所有記録を退避済み・削除済みへ偽装せず、候補とテスト原記録は別領域に保持した。
