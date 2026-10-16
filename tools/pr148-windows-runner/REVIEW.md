# Review 2755 response and remaining verification

Original: Claude Sonnet5.5, base d6a46b0 / code6275c09. Raw response and SHA/bytes
are retained under evidence-private/20261017/review-2755. It read all 13 original
Go files; three primary reference files and the 2746 prior evidence were unread.
This ledger is an implementation response, not a second model approval.

| Finding | Candidate fix in 3ba895f | Validation / limit |
|---|---|---|
| M1 token receiver | Check kernel pipe server PID against running SCM PID, retain SYNCHRONIZE-only process handle, recheck before writing | Kernel PID API unit test; no real token transferred |
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
