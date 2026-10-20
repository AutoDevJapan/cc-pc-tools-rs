# Current PR execution, ToDo3059

The original four candidates remain unchanged. This additional route admits
only repository 1285649124, PR995. The operator freezes the reviewed source,
tree, base, run, attempt, official job and review/acceptance evidence hashes in
an input JSON. A normal correction in this same PR requires fresh evidence and
a new invocation; it does not require recompiling a SHA constant. No running
invocation follows an advancing branch.

The 2026-10-20 direct assignment requires: 「helperを同じ元PR995の通常修正にも
使える正規の固定候補経路へ直し」 and 「SHA定数の書換えだけで毎コミット停止する
設計を繰り返さず、許可対象をこの具体的repo/PRに限定して実行時にheadを固定し、
原証拠と照合する設計を選んでください。」 The original private assignment and
owner statements remain in the supplied operational specification; its contents
and product material are not copied into this public repository.

Acceptance:

1. Given the explicit PR and unchanged original evidence, secret and required
   default-setup CodeQL jobs accept the frozen head/tree/run/attempt/job.
2. A different repo/PR, fork, changed head/tree/base/context, run or attempt,
   changed executable workflow, missing/changed evidence or unknown job kind
   fails before Docker creation, token acquisition or runner registration.
3. Existing source/auth hooks, live API callbacks, queue exclusion, namespace
   isolation, CPU/RAM limits and complete owned-resource teardown remain active.
   Evidence roles cannot substitute for one another. Execution needs a queued,
   unassigned frozen job; hooks require the actual assigned runner. Missing API
   assignment visibility has only three bounded reads, never a source fallback.
4. Inspect/registration/listener completion and fixture passes do not certify
   official CI. Record actual job/check results, original log hashes and CodeQL
   uploads against the frozen source separately, after normal helper CI/review,
   merge and finite deployment with source/binary hash comparison.
5. The original four candidates, merged-release route and unrelated native
   acceptance remain unchanged. No product source/CSV is copied or modified.

The original observed head was ff9be3d7c060bef61f56e191d56c560133cf2c00;
run39372426644/job118463049564 waited for the dedicated secret label. At
2026-10-20T05:13Z the API reported this run cancelled and PR995 advanced.
That observation is not authorization to execute its successor without a new
fixed input and corresponding original review/acceptance evidence.

Runtime: `current-pr-inspect <pin-json>` verifies the evidence and the complete
API/queue plan. `current-pr-job <pin-json> <new-owned-dir> <Linux-helper>
[gitleaks-ELF]` uses exactly the same verified input and existing ephemeral
executor. Only secret and default CodeQL kinds are accepted; this route cannot
bootstrap, reconfigure, release, merge or deploy a product.
