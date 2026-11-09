# Recover one native image question without sending it

`unattended-runner --check-native-question-evidence --evidence <private-json>`
validates a retained local Playwright screenshot and its original reference
against selected native tool records. It makes no Discord request, registers no
wait, and never interprets an accepted question as an owner answer.

The evidence uses `owner-native-question-evidence-v1` and binds the original
request, ToDo, body and canonical decision hashes, configured owner/guild/forum,
origin, job, worktree, dedicated session, first assignment and ordered images.
It additionally pins the successful single-worker Playwright command event,
original retention call, original question call, literal body/decision creation,
ordered native image previews, and the original async call/accepted result.
Native records are read from the canonical session store with bounded sizes and
line hashes. Evidence and async argument/result objects reject unknown fields
and duplicate keys. Native record envelopes, job/task metadata and guard output
are projected from their existing extensible schemas; selected native records
are additionally pinned by their complete line hashes. Unknown executable
wrappers, different bytes, paths, tasks, recipients and expired evidence fail
closed.

This bounded capture contract currently supports exactly two images: an
existing reference within the originating worktree, followed by a retained
`test-results/live-3d-*/live-3d-dark-round-end.png` output from the successful
single-worker `e2e/live-3d.spec.ts` command. `copy_result_event` pins its successful
hash readback. `async_title` and `async_options` must match the original native
call, with no intervening user or tool event after the image previews. The
original question cell permits only its single exact sender call and literal
read-only `Get-Content` diagnostics; changing cwd or adding another sender call
is rejected.

The current producer file can have changed after another test run. Its current
contents never replace the pinned original preview. Only the exact retained
image, current original attachment bytes, original copy lineage and current self
producer/task may establish this purpose. Existing exclusions and explicit
protection remain effective. The general classifier, imagegen and Go-drawing
qualification contracts are not changed. An unattributed output location is
not reclassified; this check establishes only this native question purpose.

Successful output has domain `owner-native-question-proof-v1`,
`validated:true`, `send_allowed:false`, original scope, image hashes/sizes and
async call association. `--diagnostic` returns only the fixed failed stage.
Neither output is publication permission or UI approval.

## Check and stage the unchanged intent

Use `discord_ask recovery-native-intent` with the same original `--request-id`,
`--todo-id`, `--body-file`, `--decision-file`, `--title`, `--origin-thread`,
ordered absolute `--attach` paths and `--expected-image-sha256` values, plus
`--evidence <private-json>`.

Without `--stage`, the command only checks the proof and existing ledger. It
creates no lock, intent, HTTP reservation, question, notification or wait.
With `--stage`, the task-control and recovery writer locks protect the current
check and append. A missing original unsent intent and its native association
are published together in one atomic ledger update, then read back. This is a
new recovery observation, not a fabricated historical delivery receipt.

Retries reuse exact bytes and association without appending again. An existing
other request for the decision, changed input, unknown outcome, or existing
send reservation is not rebound. The async-associated intent remains
stage-only: old ask/resume paths cannot send it by omitting its purpose flag.
The original async question is retained, and no duplicate external question is
created. Owner-answer recovery and any later delivery require their own exact
normal association; this command does not authorize either.

The CLI reports only original IDs, hashes, association and fixed state:
`http_performed:false`, `wait_registered:false`, `delivery:not_sent`,
`approval:not_inferred`. A staged receipt does not mean the images reached the
owner or the product is complete.

## Validation and distribution

The new Go tests use synthetic in-memory records. Rust integration tests verify
check-only, atomic stage, idempotent retry, conflicts and HTTP/wait absence.
Keep existing sender/receiver, source, imagegen, program-image and secret
boundaries in the regression scope. Distribute only reviewed binaries built
from the same merged source, preserve previous software, and record/read back
the actual binary hashes before running the original request check/stage.

Install and verify the new sender and runner before staging the production
ledger. Staging adds `native_evidence` and `native_association`, which older
strict Rust readers do not understand. A pre-stage rollback can restore the
previous software directly. After staging, keep a reader that understands the
new events; restoring an old sender requires an explicit compatible migration
that preserves every ledger event. Never discard the ledger or remove the
native association to make an old reader or sender accept it.
