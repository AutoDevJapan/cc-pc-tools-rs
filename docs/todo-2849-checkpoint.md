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

## Review 2859 follow-up and final test update

- Review 2859 read the full Go diff and parent API/Rust contract. It reported Must 0, Suggestion 6 and Nit 4, but clearly listed unavailable unchanged definitions. The contract confirms the success receipt has exactly the documented 9 fields; owner/full-name/repository ID are validated inside Go and intentionally are not additional receipt fields.
- Addressed the substantive suggestions in the next source change: the junction test now has a committed repository and a positive plain-container control; a real wrong-origin repo fixture is rejected; the GH identity argument vector and parser have fixed valid/invalid tests; date boundaries use injected time at start/end; fingerprint vectors cover forward/backslash, mixed case and trailing separators; registered root/spec fingerprints come from the stored project/item fields; and `currentScopePathAllowed` routing has direct tests. The documented guard-path remains resource classification; scheduler authorization is the task-specific receipt, whose expiry is tested.
- The current succession CLI exits nonzero if writing its success receipt to stdout fails. The success receipt test pins its exact 9-key schema.
- After these changes, `go test -race ./...`, `go vet ./...`, candidate CLI ToDo 2850 success/unknown-ID refusal all passed. PR223 requires fresh CI and one final independent review for the latest source before merge.

## Additional 2859 review follow-up

- Pinned the identity request to the absolute `C:/Program Files/GitHub CLI/gh.exe` and `--hostname github.com`; its argument vector is tested. The GitHub response parser rejects empty output, multiple lines, empty owner, negative/bad ID, mismatched owner/repository/ID, and extra fields.
- Added deterministic scope-boundary checks: UTC scope start and the final nanosecond before the end succeed; before-start and exact end fail. The parent contract applies this time-limited receipt to scheduler authority; `guard-path` remains resource classification and is not itself task authorization.
- Added junction positive-control/negative cases with a committed fixture, actual origin mismatch through `sourceOriginVerified`, path hash vectors for slash/backslash, mixed case and repeated trailing separators, and receipt fingerprints derived from the stored project/item path values. The receipt test pins exactly nine contract fields and checks both raw and normalized path forms are absent.
- The final diff will be reviewed again with the exact contract and supporting unchanged guard helpers included. Local `go test -race ./...`, `go vet ./...` and candidate CLI acceptance passed after the changes. Official CI must rerun on the next PR head.

## Final review 2859 actions

- Confirmed and tested all scope boundaries requested in review: the plain junction-container control admits its planned task while the junction target is rejected; actual wrong-origin Git metadata fails; the GH request argument list is pinned; identity parsing rejects empty/multiline/malformed/mismatched values; date edges and path spelling variants use fixed expectations; the receipt uses the stored project/item paths; and the success schema has exactly nine fields.
- GitHub CLI receives only the saved OAuth configuration: the child environment filters `GH_TOKEN`, `GITHUB_TOKEN`, their enterprise equivalents, and `GH_HOST`, while preserving user config and proxy settings. The executable and `--hostname github.com` are explicit. This final environment-filtered candidate was exercised successfully against ToDo 2850 using the local OAuth login; unknown ID still returned generic false/exit 1.
- After the final changes, `go test -race ./...`, `go vet ./...`, and both candidate CLI cases passed. The prior 2859 review included contract omissions; the next required independent review will contain the full contract plus all relevant unchanged guard helpers. CI must run again for the final pushed head.

## Item_92 and review 2863 follow-up

- The stopped job's item_92 was a `gh pr checks 223` status query. It exited 1 because required checks were still pending; it was not a failing test or build. Current shared ToDo 2849 remains doing; ToDo 2850 remains unstarted with `--needs cargo`.
- Review 2863 reported Must 0, Suggestion 4 and Nit 2. Its main remaining suggestion concerned malformed CLI values and combined modes; these now produce only `{"verified":false}` with exit 1. The GH API request is pinned to the absolute executable and github.com, and token/host overrides are filtered.
- Date checks and scope edges are now covered. `currentScopePathAllowed` classifies a resource only; Rust must require the finite, task-bound receipt before dispatch. Read-only inspection of the merged Rust implementation confirmed that a false receipt skips reconciliation and dependency release rather than terminating or releasing an existing job; a transient network failure defers work safely.
- The path fingerprint now hashes only the contract's slash-normalized, trailing-slash-trimmed, lowercased absolute path. Nonabsolute, extended-prefix and noncanonical dot/duplicate-separator inputs fail closed. Tests cover stored DB path variants, backslashes, case, repeated trailing separators, a plain-container junction control, junction-root rejection, wrong origin and the nine-field schema.
- Final Go `go test -race ./...` and `go vet ./...` passed. Candidate CLI tests passed for valid ToDo 2850, unknown ID, malformed ID and conflicting modes. These remain candidate results; official PR checks must rerun on the new head. The exact latest diff and all relevant parent contract/source context are registered in review 2863; because these small CLI/path changes followed that review, a final delta review is still required.
