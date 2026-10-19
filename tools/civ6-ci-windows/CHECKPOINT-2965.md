# ToDo2965 checkpoint (2026-10-19)

Branch scheduler/todo-2965-windows-runner-2962 (pushed). Latest code HEAD: see `git log`.
Delivered module: tools/civ6-ci-windows (+ imported parent contract 6332ffff at tools/civ6-ci-contract).

Done: module implementation, Go test -race/vet/build, real normal+cancel service fixtures
(evidence/fixtures-20261019.json), roots removed with safe-delete (all three finished source_absent),
README with CLI/Plan/pins/residue contract.

Open:
1. Independent review ToDo2968 (gpt-6-luna, fixed head f2afd85; later commits only add VerifyAssigned/
   RequireSuccess wiring, README, contract import, workflow removal). Result file is written in
   the 2968 worktree (todo-2965-review-result.md). Fix any Must, rerun go test -race/vet.
2. Append delivery record to parent ToDo2962 note (read current note first; use
   `todo.exe edit 2962 --note-file <f> --expected-note-sha256 <sha>`; never overwrite other writers).
3. Residue note for ToDo2381: two new profile/hive/classes residues from the fixtures
   (instances 6a8d1e39e07cf51213e482b895e1865e, c4809f99f51ee782852c62f2439e36d1); no retry/unload.
4. Do not register runners, fetch product source or run real Rust here. No PR (parent integrates the fixed commit;
   the parent's civ6-ci.yml covers CI).
