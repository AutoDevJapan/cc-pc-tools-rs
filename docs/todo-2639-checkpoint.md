# ToDo2639 再開記録（2026-10-17）

監視日時の破損で重大アラートまで消える不具合の修正を、固定PR1002で継続する。原1636/2016/2274の停止・依存・未完了は変更していない。この独立工程を原製品全体の完了にしない。

## 今回の実確認

- GitHub APIのrepository ID 1285649124でAutoDevJapan所有・private・admin・nonforkを確認。名前はAPI経路のメモリ内だけで使用する。
- PR1002はopen/draft=true。head=b93e126b172adbb29ad2081af4732f4b42f8bc95、base=f3d946cb0f7e33b8cb5cbc0858091e583ca61350。mergeable=true/clean。5ファイル差分。
- 同headのcheck-runsを実読戻しし、下記6件すべてcompleted/successを確認。チェックの再実行・dispatch・手動success作成は0。

| check ID | 名前 | 実結果 |
| --- | --- | --- |
| 116905217657 | CodeQL | success |
| 116903806365 | test | success |
| 116903790733 | Analyze (rust) | success |
| 116903790683 | Analyze (actions) | success |
| 116903790615 | Analyze (python) | success |
| 116903790529 | Analyze (javascript-typescript) | success |

原956でのlocked Ruff/format/mypy220files/backend2236passed、および後日の3結合検査成功は元2648からの配送記録として保持する。b93でbackend全件を再実行した結果ではない。b93と956のアプリ部分のtree一致、b93のNode185passed、公式CodeQL/secret成功の原記録は指定された操作証拠保管先にある。生ログを再表示しない。

AGENTS.md/CLAUDE.md/.github/CODEOWNERSを固定GitHub sourceから読んだ。配備先の制約、既存required context、release safety gateの区別、日本語のGitHub文章、自動生成ブロックの保持を引き継ぐ。顧客固有の人間レビュー必須条件とrelease gateの扱いもレビュー工程で再確認する。

## 独立レビューの登録

元App差分と追加CI差分に対する別モデルレビューは未完了だったため、重複工程がないことを確認し、共有ToDo2735を登録した。モデルはclaude-sonnet-5-5、種別review、元2639/今回のjobに紐付けた。登録後、保存specの存在・dispatchable=true・親2639へのsubwork記録を実読戻しした。登録をレビュー成功と扱わない。子Codexジョブを直接起動していない。

登録した指示は docs/todo-2639-review-spec.md。対象は固定baseからb93への5ファイル。GitHub API読取だけを認め、source/PR/CI/配備の変更や重い検査は認めない。結果は子worktreeの docs/todo-2639-review-result.md とその実commitで返す。

## 次の一手

1. ToDo2735の完了後、schedulerが記録するそのworktreeで結果ファイルと実commit・対象head/base/treeを読む。別モデルの実レビューであることを確認し、Mustを修正する。単なる受付・起動・ファイル存在を合格としない。
2. 修正が必要ならGitHubの同PR/同branchで通常のcommit/pushを行う。新しいheadへ固定値を更新する必要があれば元証拠と分けて保存し、影響する検査・別モデルレビューを確認する。現在の固定値を黙って追従しない。
3. Must0・顧客固有規則・必要CI・release gateを確認した後、draft解除と固定headを指定した通常マージへ進む。元PR173/174/183/185/187や完了CIを再実行しない。
4. マージ後の実source/必要検査と既存Prdへの反映を、実結果と未実行に分けて追う。新費用、別Org、非Prd配備、旧製品フォルダ、保護設定の操作は範囲外。

今の親は他ToDoのレビュー完了だけを待つため、正式な `todo.exe wait 2639 --on 2735` を記録して枠を譲る。オーナーの判断・PC操作は不要。今回新しい検査用コンテナ・ブラウザタブ・一時フォルダは作成していない。重いCargo/GPU処理は開始していない。
