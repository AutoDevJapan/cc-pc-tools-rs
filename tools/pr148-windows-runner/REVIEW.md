# Review 2755 response and remaining verification

Original: Claude Sonnet5.5, base d6a46b0 / code6275c09. Raw response and SHA/bytes
are retained under evidence-private/20261017/review-2755. It read all 13 original
Go files; three primary reference files and the 2746 prior evidence were unread.
This ledger is an implementation response, not a second model approval.

| Finding | Candidate fix in 3ba895f | Validation / limit |
|---|---|---|
| M1 token receiver | Check kernel pipe server PID against running SCM PID, temporarily pin a SYNCHRONIZE-only process handle during validation, then use the connected pipe | Kernel PID API unit test; no real token transferred |
| M2 fake admission | execute always calls gate; dedicated callback verifies actual live parent image, LOCK_HELD and bounds; service forwards bounded authenticated-controller allocation rather than inventing flag | Environment-only forgery rejected; actual shared gate run belongs to parent Cargo allocation |
| M3 permissions | Manager CONNECT only outside prepare; controller gets START/STOP/QUERY on new service only; control preflight never walks runtime | Restricted own-token process creation denied by Windows5 even via broker; no alternate method attempted. Source regression remains opt-in; nonadmin runtime not claimed |
| M4 unbounded collect | Two bounded inheritance-removal operations, direct boundary DACL queries, per-action immutable attempts/results, no per-file subprocesses | Both final fixture services deleted, installed grants0; profiles remain; mutation refusal never retried |
| M5 success evidence | Protected retained launcher report; match all 3 hook/source receipts; post audit requires exact runner/completed/success and zero registration within finite budget | Hook/context/job unit regressions; no official runner/job executed here |
| M6 inaccurate records | Capture actual account SID, TokenGroups result and post-termination exit; explicit no-LoadUserProfile request distinguished from SCM profile state; legacy evidence labeled nonmeasurement | Final normal0/cancel125, AU DenyOnly measured true; old source claims/raw evidence retained separately |

Other valuable findings addressed: protected launcher-only drain authority and
SCM/census checks before deletion (S1); explicit launcher PID in both fixtures
(S2); official version/asset digest API binding and opened-byte bounds (S4);
merge parents plus merge/PR workflow digest equality and finite completion poll
(S5); fixed workspace, pre-command extra-file rejection, dangerous config-key
rejection and pinned checkout documentation (S6); exclusive control-file creation
(S8); unreadable census count and journaled collection resume (S9); stop races
and terminal-state ordering (S10); explicit SDK/empty-home/cache integration
limits (S11); committed final source and binary evidence correspondence (S12).

Deliberate limits: This retains the specified prior AU-disabled service model,
not a VM sandbox or new medium-integrity scheme (S3). Controller/admin is trusted;
its ability to change its own root is not an adversarial boundary. Handle-pinned
file/plan attestation and hostile-controller TOCTOU hardening are not claimed
(S7/S8). The original commands may generate ignored outputs after testing;
strict untracked/ignored rejection is before commands, while the completed hook
still checks tracked source and authentication (S6). SCM profile creation timing
is not diagnosed by a no-LoadUserProfile-request field; residue stays incomplete.
Repeated verify-source calls intentionally fail the single-receipt contract.
No source/gate/SDK/bridge/allowlist/old cleanup permissions were broadened.

Final service fixtures exercised changed code, not unchanged reruns. Restricted
controller launch and the PowerShell cancellation observer's missing controller
exit code are preserved as failures/limits. The service's measured exit, stop,
drain and final snapshots remain separate evidence. A follow-up model review is
required before parent integration; do not infer Must0 from this ledger.

## Follow-up 2763

Claude Sonnet5.5 confirmed original M1-M6 resolved in code3ba895f, Must0. It read
all 18 Go files, the four pinned f7acf6d primary files and the prior2746 evidence,
and independently ran Go tests (11 pass / 2 opt-in skip), vet and build. Raw
reply/SHA/bytes are preserved in evidence-private/20261017/review-2763. The public
review-2763-summary.json includes the final fixture binary fingerprints and
separates the previous3 plus subsequent2 profile/hive/classes residues (known
pending total5; no fresh physical recheck or refused cleanup retry).

The new bounded delta does not inherit that review approval: ordinary controller
errors now perform one stop attempt before returning; collection has a plan-bound
cleanup budget rather than always30seconds; pipe open uses anonymous QoS. Unit
coverage uses that actual pipe-opening path and validates cleanup budget bounds.
New service/account fixtures were not repeated for these control-only changes.

2763 S1: the normal error-return omission is addressed. Abrupt controller/gate
termination or an uncertain stop is still an integration risk, not a claimed
crash-safe shared-gate lease. Parent must supervise/fence that condition before
live use; this task does not broaden or replace the shared gate/bridge.
S2: cleanup_seconds defaults to1800 per ACL operation, accepts60..14400, and must
be selected from actual artifact scale and broker timeout. Failed/uncertain
attempts stay immutable and cannot be replayed. Large-workspace propagation has
not been measured here.
S3: README now states that job-writable hook receipts detect missing/mismatched
stages, not malicious same-account forgery. Launcher-owned process/drain/exit
measurements remain separate. S4: the existing fixture already successfully
opens its same-token grandchild with QUERY_LIMITED_INFORMATION|SYNCHRONIZE before
the explicit launcher QUERY_LIMITED_INFORMATION attempt returns ACCESS_DENIED;
that is a positive access control on the unprotected child, not a test that
removes protection from the real launcher. Do not rerun service fixtures or
weaken the protected launcher solely to restate it.
S5: official runner/hook/source/gate/SDK behavior remains parent integration work.
S6: public binary fingerprints and the known total5 residues are now explicit.
The M1 documentation says temporary identity pinning, not a handle retained for
the whole run. No new credentials, registration, production check or cleanup
success is inferred from source review or these documentation corrections.

## Final bounded review 2764

Claude Sonnet5.5 reviewed all 11 files of faa535c..da2b45e (7 Go plus four
records/documents), read the surrounding start/query/stop/gate context and the
2763 original reply, and found Must0 with no regression of M1-M6. It independently
ran Go tests (12 pass / 2 opt-in skip), vet and Windows build successfully. The
current Go files are identical to da2b45e; subsequent changes are record/prose
corrections. Raw reply, SHA and bytes are in evidence-private/20261017/review-2764.

Its small documentation suggestions are applied: gate retention is conditional
on a live controller/gate and confirmed orderly stop; handle pinning lasts only
through peer validation; cleanup budget is a conservative pre-run estimate, with
2x/4x operation budgets plus overhead for real/fixture collection. This does not
claim actual large-workspace cleanup timing or actual failure-injection tests.
The proposed new function-variable seam/table tests are not added to this
already reviewed control code solely for testing: the source transition analysis
is explicitly separate from actual SCM error/timeout validation. Those practical
failure cases remain parent integration checks when the real gate is allocated.
No fixture, denied operation, account or service was rerun to refresh this prose.

Source implementation/review is complete and ready for parent delivery. Official runner/auth/source,
SDK/gate behavior, nonadmin runtime, official CI and crash-safe admission remain
unverified as documented. All owned temporary roots have recorded source_absent;
five profile/hive/classes residues remain with existing cleanup ToDo2381. They
are not hidden behind a completed implementation flag or a fake cleanup success.