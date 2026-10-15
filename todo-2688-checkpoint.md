# ToDo 2688 checkpoint

- Base: main `5090345eecd94a0b464621a4e9a09c42005e3689`, clean; branch `fix/native-recovery-cancel-openprocess-race`.
- Ownership guard: this worktree was verified outside protected client resources.
- Original CI failure: Windows run `38687135081`, job `116264465894`; `test_native_recovery_cancel_preserves_intent_after_process_tree_stops` failed at `parity.rs:10911` with `inspect/OpenProcess: ACCESS_DENIED` (OS error 5). The old CI log contains no PID or image for the denied open.
- Reproduction attempts before design change: focused E2E passed 20 serial repetitions; all 411 `cc-jobs` tests passed; full workspace passed 2180/2180 (5 configured skips). Therefore the exact failed PID/process was not identified, and those passes are not claimed as a fix.
- Design change in progress: each new Windows supervisor is assigned at startup to a named Job Object keyed by its PID and creation time. Descendants inherit membership. Recovery cancellation uses the same Job Object and waits for its active-member count to reach zero; `ACCESS_DENIED` is accepted only if querying that same object proves active=0. Job assignment/query/termination failures remain errors.
- Measured limitation: after the supervisor itself exits, Windows removes the named-object lookup while surviving children remain. Lost-supervisor recovery therefore falls back to the retained-handle tree walk, preserving the existing lost-job scenario. The active-supervisor recurrence uses the Job Object path.
- Tests after the design change: the native recovery-cancel and lost-job E2Es passed together; full `cc-jobs` nextest passed 411/411 with 0 skipped. The modified recovery E2Es also passed during that run.
- Next: complete final-source full-workspace nextest, doctest and Clippy; review the process ownership design; commit/push; request a read-only review through shared ToDo `--spec`; resolve Must findings; open PR, verify all required CI, merge and confirm main; write the requested result JSON and cleanup record.
- Exact original Windows CI PID and image remain unavailable. Do not claim a proven instance-level cause; report that the redesign removes snapshot-PID lookup from the active-supervisor cancellation path.

## Current final validation checkpoint

- Added the Windows Job Object path for active supervisor cancellation. The active recovery-cancel E2E and lost-supervisor fallback E2E both passed; `cc-jobs` passed 411/411.
- Full workspace on this source passed 2179/2180 with 5 configured skips; the one failure was `cc-ask::resume::no_origin_repository_creates_and_reuses_worktree_from_committed_head`. It passed alone, and all 769 `cc-ask` tests passed with 1 configured skip. No source change was made for it because it did not reproduce and the test uses a unique temporary local Git repository.
- Doctests passed. Workspace Clippy exposed an existing `nonminimal_bool` warning in `localai-web-rs/src/todo_listing.rs`; the expression was simplified. After this edit, `localai-web-rs` passed 337 tests with 3 configured skips.
- Next: rerun workspace Clippy after the fix; then perform the single final workspace nextest for this exact source. If the cc-ask test fails again, capture and resolve the concrete failure before proceeding. Then finish the independent review, PR/CI/merge, report JSON, and cleanup.
