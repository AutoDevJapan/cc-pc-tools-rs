# ToDo2965 checkpoint (2026-10-19, updated)

Branch scheduler/todo-2965-windows-runner-2962 (pushed). Module tools/civ6-ci-windows; parent contract = main fec91d4
(content of 0d1e95b) at tools/civ6-ci-contract.

Done
- Implementation, Go test -race / vet (windows + linux) / build.
- Review 2968 (f2afd85) Must4 fixed; re-review 2974 (205a782) Must3 (R1 SCM read errors, R2 deadline anchor,
  R3 drain proof) fixed by one holdUntilDrained boundary (monitor_windows.go) with injected-clock tests.
- Parent decision: ONE fixed service/virtual-account identity (CC-CIW-civ6-banseiki; fixtures CC-CIW-civ6-fixture),
  per-job nonce/root/runner name. ensureService (scm_windows.go) claims or re-points it, refuses busy/uncollected;
  collect keeps the service + one profile and resets the owner; cleanup-profile removed.
- Real fixtures: normal/cancel/normal on the fixed fixture identity, profile count constant (evidence/*.json).
- All fixture roots removed through safe-delete.

Review 2979 (RR1-RR5) and 2984 (M1 moved-root isolation, M2 operation exclusion) addressed: whole-tree ACL reset + recursive proof +
durable CollectedRoot marker; file-based exclusive lock shared by prepare/start/stop/collect; stale-job refusal verified live.

Review 2993 (R4-M1 lock path from env + S1-S4) addressed at 7f83d96 and verified live (fixture identity).

Review 2999 (R4 fixes) returned Must 0 / Suggestion 4 / Nit 2; the cheap ones were applied (no-follow handle for every entry, stricter path rules,
marker NUL check, COM free on failure, README). The remaining Suggestions are documented as known limits in the README.

Open (not for this child)
- Parent integration of the fixed commit, queue2966 wiring to the README contract, official PR149 run and the 12-job budget.
- Not done by design: real registration, product source, real Rust, live Verify, the real identity CC-CIW-civ6-banseiki.
