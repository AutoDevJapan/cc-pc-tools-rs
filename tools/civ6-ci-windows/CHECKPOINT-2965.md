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

Review 2979 (c2fea70) found RR1-RR5; fixed: complete-census/no-worker drain proof, shared absolute worker deadline, strict image-path parse,
mutex + bound-to-root checks, predecessor root must be deleted (collect returns the root to the controller).

Open
1. Independent re-review of those fixes (shared ToDo, spec todo-2965-rereview3-spec.md).
2. Append the delivery to parent ToDo2962 note and the 3 legacy residues (6a8d1e39..., c4809f99..., 052658b1...) to ToDo2381
   (read the current note first; edit with --expected-note-sha256).
3. Not done by design: real registration, product source, real Rust, live Verify.

