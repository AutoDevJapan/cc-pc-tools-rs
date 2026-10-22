# Current PR execution, ToDo3059

The original four candidates remain unchanged. The finite original-authority
admission currently accepts only repository 1285649124, PR995, fixed head
cbd688871651e46d4821a4535d57eaa408105ff2 and its original secret job. New heads
and CodeQL input admission are not implemented by this finite repair. The
original ToDo3059 requirement for ordinary same-PR corrections without repeated
recompilation remains operationally unverified. The separate canonical
product-parent runtime handoff is implemented as described in
[the runtime specification](../../docs/product-ci-runtime-3059.md), but has not
executed a real job through this new route.
The finite constructor itself continues to reject new heads and CodeQL.
The generic planner remains source-bound and never follows an advancing branch.

The 2026-10-20 direct assignment requires: 「helperを同じ元PR995の通常修正にも
使える正規の固定候補経路へ直し」 and 「SHA定数の書換えだけで毎コミット停止する
設計を繰り返さず、許可対象をこの具体的repo/PRに限定して実行時にheadを固定し、
原証拠と照合する設計を選んでください。」 The original private assignment and
owner statements remain in the supplied operational specification; its contents
and product material are not copied into this public repository.

Original ToDo3059 acceptance (broader than the finite admission):

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

Runtime: the former `current-pr-inspect/job <pin-json>` and `current-pr-qualify`
entry points fail with `legacy_input_route_unavailable` before opening caller
input or a locator. The non-executable inputqual unit boundary retains the
original rejection tests. The broader planner's CodeQL support is not equivalent
to operational CodeQL input admission. The route cannot bootstrap, reconfigure,
release, merge or deploy a product.

The finite ToDo3089 origin repair adds a purpose qualification gate before this
reader. `current-pr-origin-qualify` checks the independently retained original
task specifications, outcome and receipts, constructs inputs and passes their
exact bytes through the official parser. It does not contact Actions or certify
CI success. Its executable-side locator file
contains private original references, never new authority. Both original
locations and independent evidence digests are pinned in the reviewed source.
Unknown/copy locations, changed source or evidence, missing scope and links fail
closed. The general protection classification is unchanged. See
[the finite origin specification](../../docs/current-ci-input-origin-3089.md).

The operator-created wrappers could not prove independent payload identity and
remain unavailable without being reopened. `current-pr-origin-qualify` constructs typed
in-memory inputs from the independently verified original receipt's final review
record and exact acceptance receipt. It does not read or copy the rejected three
files, or claim to have read the raw review response. The returned input hashes
are deterministic and bound to the same task/source/run/job; they are not new
owner permission. `current-pr-origin-inspect` and `current-pr-origin-job
<new-owned-dir> <Linux-helper> <gitleaks-ELF>` consume this same constructor's
verified bytes with all existing live API/queue/execution conditions intact.

Any explicit stop on the original work or this repair rejects qualification
even when status remains todo/doing/wait. Base is tied to the final candidate
receipt; context is the reviewed helper workflow constant and requires the live
API match before execution. Review and acceptance are representations from the
same independent original receipt; the raw reviewer response is not read, and
the receipt's 36-artifact assertion is retained rather than independently rerun.

## Additional answered two-source execution

The original single995 route above is unchanged. The separately qualified
`current-pr-batch-qualify` admits the original dependency939 from its independent
owner reply, complete source-diff snapshot and different-model source review.
Private references use an executable-side `batch-input-3059.json`; both original
locations and the verified bytes are pinned. This command certifies input only.

`current-pr-batch-inspect` and `current-pr-batch-job <new-owned-dir>
<Linux-helper> <gitleaks-ELF>` additionally require the original995 constructor.
Only the exact pair described in [the batch specification](../../docs/product-ci-secret-batch-3059.md)
can share the existing label. The actual assigned runner selects one fixed plan;
its live hooks and final source/auth proof must match that plan. Every invocation
remains ephemeral and serial, and any other queued job is rejected. No product
workflow, cancellation, general protection classification or original candidate
is changed. These finite routes do not implement future-head admission.

`current-pr-runtime-qualify/inspect/job` use the original product parent's
canonical handoff with newly verified original review and acceptance outputs.
Fixture checks accept frozen secret and CodeQL inputs for different PR995
heads without changing compiled SHA constants. PR995 has since merged; the
existing live verifier rejects merged PRs, so runtime-inspect/job cannot execute
that merged PR. Operational use and success through this route remain unverified.
It has no caller
path argument and does not reuse the formerly rejected three inputs. Its input
qualification and fixture checks are not a claim of new official job success.
