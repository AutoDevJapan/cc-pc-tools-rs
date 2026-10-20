# ToDo3011 checkpoint

- 2026-10-19 実測: 現行scrub391 (vcs 2977911) は3件とも classification_unverified=true / client_resource=true。
- 実装: repair-guard/discordask_windows.go を追加、runtime_windows.go の共通検査を抽出。go test/vet/race 成功（ローカル）。
- 未完: PR作成、別モデルレビュー、正式CI、merge、scrub391配布、3件再照合、runner配備、既存task実起動。