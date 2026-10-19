# ToDo 3021 配布記録の根拠

固定source commit `7c1fe857547eb7e6850361752a26d9dfe868528e` とPR255 merge commit `842e513191c2ced4364f8382b3a8f9654d01f9d3` の一致を確認した後、ToDo3030の候補実物を別の隠しConPTYで再利用検証した。

## 検証済みCodex実行物

- 相対配布元: `D:/CC/Work/unattended-operations-20261013-wt/todo-3029-codex-tui-0-163-0/tools/codex-progress-tui/.ci-work/todo3029/delivery/codex.exe`
- SHA256: `618bf6dcd243463d9058f0ce68b9e43ba2a2b2ac75c715f87ad40d06e453433f`
- `--version`: `codex-cli 0.163.0`
- 元patch source: `e5578209650641348c778829c8a0b83eb6753dc1`
- ToDo3030の同一実行物ConPTY受入: 常設欄、`/progress`、partial警告、resize、`/quit` exit 0 成功。attempt5のみを根拠に採用。
- ToDo3023の固定sourceに対する関連nextest5件、release Clippy、doctest、Codex buildは成功記録を再利用。全件suiteの失敗と公式baselineの同一失敗も記録済み。ToDo3030は追加Cargoなし。
- 過去に記録されたtested binary SHA `bcebff4c...` との全byte差は未説明。PE timestamp変動とbuild時commit stampの有無を確認したが、寄与を断定していない。ここでtested binary欄を今回受入した候補の実測SHAへ変更するのは、既知の旧SHAを改ざんするのでなく、source由来と実物を保持した新しい検査対象を明示するため。

## 公式npm tarball

- package: `@openai/codex@0.163.0-win32-x64`
- source.jsonの既存npm integrityは保持tarballと一致。
- Codex SHA256 `2692c8c7bb2cf8b0aefe638741fc90236f4b824c59578ce84994f9de58200993` と公式host SHA256 `07f3869f0849cd54b31cec1f86ec796f004501bd3a215135c1facc883a2b9988` はsource.jsonと一致。
- 実tarball SHA512: `410190feaf5d089e42e63aa1805af0c9ca561b7b3323214d4e13222a5ea162dbba37f78cdd077b4ad7ac82708501fcd5103f08904dcdde81fbd6ccf3725b9dab`。
- 既存source.jsonのhexは127桁で、上記完全値と一致しない。integrityとの整合を維持して完全な値に修正する。

## 変更範囲と未実施事項

この記録と `source.json` 以外は変更していない。launcher、patch、manifest、collector、現在の配備物は未変更。配備前にこのsource.json差分を別モデルでレビューし、Mustなしを確認する。レビュー後にのみcodex/hostの既知実物を既存の導入手順で配備し、launcher経路・hash・manifest・通常の隠しTUI・公式exec経路を受け入れる。本人の現会話には触れない。
