# ToDo 2754 handoff

## Completed in this worktree

- Implemented the independent `cc/ci-source-2737` Go module in this directory.
- Added bounded in-memory Fetch/Encode/Decode and guest-only Materialize APIs.
- Added synthetic real-Git tests for commit/tree/blob reconstruction, ignored
  tracked content, clean-filter bypass, non-fetched gitlinks, packet tampering,
  strict JSON, and archive path/type/duplicate boundaries.
- Committed and pushed as `7221708d922bc6bd79b30eebbb534bc62bbe0a93` on
  `scheduler/todo-2754-godd-mcp-ram-git`.
- Saved the scheduler workspace checkpoint for job
  `20261017-025125-todo-2754` at the same branch and HEAD.
- Local results: `go test ./...`, `go vet ./...`, and Windows/Linux amd64
  cross-builds succeeded. The synthetic Git integration test ran on Windows.

## Not performed; parent integration owns these steps

- No API request fetched the original repository source or workflow. `Fetch`
  was not exercised against GitHub.
- No Linux guest execution, mTLS transport, or original CI run was performed.
- The caller must provide the pinned workflow SHA-256 and confirm the fixed
  workflow path against the parent task's approved source metadata before use.
- Parent ToDo 2737 owns integration, independent review, and any subsequent
  official CI. This module does not create a product PR or change the original
  repository.

## Resume

Use this worktree and branch; confirm `git status` is clean and HEAD matches the
commit above. Run tests from `tools/ci-source-2737` with `go test ./...` and
`go vet ./...`. Do not fetch or persist original product source from this task.
