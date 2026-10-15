# ToDo 2688 checkpoint

- Base: main `5090345eecd94a0b464621a4e9a09c42005e3689`, tree fetched clean; branch `fix/native-recovery-cancel-openprocess-race`.
- Ownership guard: verified this worktree is not a protected client resource.
- Confirmed failure: Windows CI run `38687135081`, job `116264465894`; recovery-cancel E2E failed at `parity.rs:10911` with `inspect/OpenProcess: ACCESS_DENIED` (OS error 5). The full run had 2179/2180 pass, 5 skip.
- Focused E2E: one initial run plus 20 serial repetitions passed on this PC; not evidence that the race is fixed.
- Current uncommitted implementation adds PID/parent/image/prior-retention context to snapshot-candidate inspection errors and emits owned fixture identity/liveness details only when the recovery E2E fails.
- Next: run `cc-jobs` parity tests with configured parallelism and investigate process ownership/enumeration races from any diagnostics. Do not treat access denied as absence/success. Then fix and add a deterministic regression test that preserves cancellation intent, timeout, and real process-exit assertions.
- Not yet done: broad workspace tests, doctests, Clippy, independent model review, PR/CI/merge, requested result JSON, owned temporary/process cleanup.
