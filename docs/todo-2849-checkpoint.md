# ToDo 2849 checkpoint — 2026-10-19

## Confirmed

- Started from clean branch `scheduler/todo-2849-2834` at `c9f0b26df6014c5473e110996b58cda18dff151e`; repository origin is the verified AutoDevJapan PC-tools repository.
- Re-read current owner instructions, root AGENTS.md and project AGENTS.md. No project CLAUDE.md was present. This worktree is not classified as a client resource.
- PR221 is merged. Its guard admits current AO successor 2839 and one specified handoff only. PR222 is an unrelated open GitHub gate change; its source remains untouched.
- Verified GoDD 13a13e1 owner/repository ID `AutoDevJapan/GoDD-13a13e1` / `1283948432`, registered project root and physical Git top-level in the parent's private inventory. PR558 is existing unfinished work with an unresolved original acceptance decision; do not re-ask or overlap it. Issue 569 has its own explicit current contract.
- Registered current successor ToDo 2850 through `todo add --spec --needs cargo`. It is a new current task, not recovered legacy work. Its saved immutable spec is `D:/CC/Tools/todo/specs/2850-cdf16525f6ab44b1a3f5e6d1f9337359.md` (SHA256 `207951d442f732e489326ef92819c3ee2674bdb38b64605d3043e50e198a0d95`). It remains unexecuted pending Cargo allocation.
- Extended `repair-guard/current_windows.go` with a finite declaration for the exact repository, owner, repository ID, project root, task ID, saved spec path and SHA. Existing AO behavior remains on the same exact qualification path. Added a regression test for exact task/repository/root/spec binding and rejection of altered task/repository.
- `go test -race ./...` and `go vet ./...` in `repair-guard` both passed after fixing a test fixture argument mismatch.

## Remaining

- Implementation checkpoint commit `0d48ef511829cf3100e490dc3820c9554d106ca1` is pushed. Review instructions are captured in a separate read-only subwork.
- Independent read-only review ToDo 2852 is registered for fixed source commit `0d48ef511829cf3100e490dc3820c9554d106ca1`; response not yet received. Address any Must findings.
- Reconcile with any intervening main update (including PR222), run relevant CI, merge only after review and CI pass, then update the official guard from the new source after checking installation source/hash and concurrent changes.
- Verify official CLI admission of precisely ToDo 2850 and denial of altered/sibling scopes. Only then can scheduler creation execute the 13a task; wait for assigned Cargo capacity and verify actual Issue 569 work/result. Registration is not execution.
- After the higher-priority 13a task is genuinely progressing, continue the remaining explicitly named products in owner priority order, one bounded intake at a time. Preserve existing active work and exact old owner decisions. Don't mark this parent complete while a product or natural scheduler continuation is unverified.

## Limits

No original 192-item registry, original manifest, lost question, legacy task, or product completion has been restored. No GPU, Civ6, Cargo build, API key, new expense, browser session, or domain provider was used. No product source repository was changed.



## Update after opening PR

- Draft PR 223: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/223. The guard implementation is commit `0d48ef511829cf3100e490dc3820c9554d106ca1`; PR currently contains that fixed source plus this repository checkpoint/review-note commit.
- The first official CI snapshot on head `5c33acca367383b1057943e3b704ca92d6f29c98` had repair-guard, Go platform checks and CodeQL pending/queued. It is not a CI pass. New docs-only checkpoint commits may cause another run; verify the final PR head's checks before merge.
- Review ToDo 2852 is registered with Sonnet 5.5 and the exact source commit; it is still todo and no response is available yet.
- Successor ToDo 2850 is registered with `--needs cargo` and remains todo. No product-source work, tests, or scheduler continuation have occurred for it.

## Formal succession API added

- Implemented `scrub391.exe --verify-current-succession <id>` in the Go guard. Only the finite declarations for AO/2839 and 13a13e1/2850 are selectable. Success returns exactly one receipt containing origin ToDo 2834, current task/project, SHA256 fingerprints for registered root and spec path, spec-content SHA256, and `original_registry_recovered:false`. Failure returns only `verified:false` and exit 1; no path or original-registry contents are emitted.
- API qualification also requires the current registered item/project/root/spec, unchanged spec bytes, plain physical root, matching origin, current finite date window, and a live OAuth `gh api` check matching repository numeric ID, full name, and owner. The child process is hidden; its response is used only in memory.
- The separate `guard-path` operation classifies resource paths only. Scheduler task authorization must use this ToDo-specific receipt before ordinary resource/root/worktree checks; it does not infer caller identity from a path.
- The first independent review (2852) found Must 0, Suggestion 4, Nit 1 on the pre-API source commit. The API now checks actual GitHub identity (Suggestion 1); declaration tests fix exact entry count and all root/spec/hash/ID values (Suggestion 2); 13a-specific existing/planned worktree, branch, origin, independent common-dir and junction checks were added (Suggestion 3). The resource-classification versus task-authorization boundary is documented (Suggestion 4). The real registered worktree-container classification remains for the official installed-CLI acceptance step (Nit 1).
- Final source after the API and expanded tests passes `go test -race ./...` and `go vet ./...` in `repair-guard`. PR223's earlier CI applies to the previous head only; the new head must get its own full required CI and a fresh independent review.

## Review 2858 follow-up and hardened final candidate

- Review 2858 read the complete earlier source diff and reported Must 0, 5 suggestions, 2 nits, while noting its prompt lacked the parent API contract and surrounding source. No Must result is being inferred beyond that limited review.
- Addressed the concrete hardening suggestions: GitHub identity requests now call the absolute installed `gh.exe` with `--hostname github.com`; the validity clock is injected and tested at/beyond both date boundaries; path hashes have independently computed fixed vectors and normalized-path leakage checks; the GitHub identity TSV parser has valid and malformed/mismatched table tests; and the finite path-routing loop is directly tested against exact, wrong-project, and unqualified cases. Generic failures are asserted for all tested refusal paths.
- The receipt deliberately retains the exact parent contract fields. Repository owner, full name and numeric ID are checked inside the Go guard before `verified:true`; they are not additional receipt fields. The Rust consumer compares the documented project, task, origin, root/spec fingerprints and unrecovered flag.
- On the current Go source, `go test -race ./...` and `go vet ./...` passed in `repair-guard`. Candidate CLI for current ToDo 2850 passed with exit 0 and returned the fixed receipt; unknown ID returned only `verified:false` with exit 1. These are candidate results, not installed-CLI acceptance.
- PR223 must receive a fresh full CI run and a new independent review of the final diff. The prior PR checks do not apply after this code change.
