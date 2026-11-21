# Owned native export for an exact job attempt

The exporter records evidence from one normally admitted Codex job attempt.
Reading an export starts no additional AI thread or turn and does not grant
source access or inspect a private history directory. The CLI entry requires a
deployed revision whose supervisor and selector have passed their acceptance
controls.

## Producer contract

The existing normal supervisor owns admission, the state lock, child creation,
process containment and finite cancellation. It freezes the actual prompt bytes
once. Generic FrozenInput::write_to records sent bytes and flush only; it cannot
confirm closure. Capture::owned_input_writer checks the same actual Child handle,
takes Child.stdin and closes that owned writer exactly once after writing the
same immutable buffer. writer_closed records the local Close result; eof remains
unknown because it does not observe the child reading EOF. Reading a prompt file
after launch, or flushing a borrowed writer, is insufficient.

After that exact child and controller generation are known, begin_native_capture
checks the saved job ID, purpose, started_at and PID/creation tuples against
actual retained process handles. It creates one new per-attempt directory inside
that job. Caller paths, hashes, record IDs, a newest-file scan or a JSON success
flag cannot mint the in-memory producer object. Captures of old installed jobs
are unavailable; no fabricated receipt backfill is supported.

The stdout/stderr adapters tee the original bytes before the existing event
pump redacts, adds timestamps and re-encodes. The legacy masked logs remain
unchanged. Each original stream is bounded to 32 MiB and each native frame to
2 MiB. Overflow, read error, write/Sync/Close failure or early adapter drop keeps
failure/unknown status; a terminal exit code does not turn it into EOF. Fatal
read failure is sticky across subsequent reads. Interrupted is retried within
the adapter and does not erase a real read failure.

Evidence contains the prompt-as-sent file, original stdout/stderr, launch and
collection receipts and exact-length SHA-256/file-identity facts. New files use
CreateNew and no write/delete sharing. Native file identity and hashes are read
from the same held file and original bytes. Each tee keeps its original writer
FileID/creation and updates a CNG SHA-256 over the actual written bytes. At EOF
it seals size/identity/digest against a readback on that held writer before
Close. Later path reads must equal that seal; they cannot issue a new origin
from replacement bytes. Same-account storage access remains
an operating-system trust boundary; a hash is not independent authorization.

## What collection means

Only the normal supervisor can reclaim its retained non-breakaway Job members;
the exporting controller itself and unrelated processes are excluded. This
module does not run a separate watchdog. At collection it queries the retained
child handle for signalled state and exit status and the same Job handle for
actual active process count. When the supervisor itself is the remaining member,
active count 1 can support “all other Job children collected” only if both the
controller and child were confirmed members at launch, the controller generation
is still the same and breakaway was disabled. Missing Job support, unreadable
handles, extra members or failed observation remain failed/unknown. No external
peer handle, detached unrelated process or global cleanup claim is made.

The collection keeps OS signalled observation separate from actual standard
Child wait/reap. observe_child_wait checks the same child handle birth then
calls try_wait on the real Child and records its result. The shared supervisor
must connect that method to its owned Child; without the call wait stays unknown. Pipe EOF is separately observed by
the read adapters. The controller remains alive while publishing; its final exit
belongs to the outer normal job consumer.

## Exact selector

The consumer takes only an exact normal job ID. Under the existing state lock it
loads the producer-written registry and matches the generation and root/child
PID+creation tuples. It derives the per-attempt directory from that registry,
rechecks the export file's recorded identity and reads a bounded same-handle
buffer. No caller supplies a source path or expected hash. Every selected disk
leaf still needs the normal source guard; source classification is distinct
from the origin/purpose checks.

The metadata response includes original_complete, byte lengths/digests, a
bounded binding-sidecar reference and all collection observations. The producer
and consumer share a 128 KiB compact-receipt limit. Native references live in a
separate binding.private.json with its own 32 MiB budget and recorded file
identity/hash; the selector revalidates that same artifact. Serialization rejects
excess bytes before creating an unreadable receipt. A budget failure publishes
no FinishedExport/registry and retains a complete=false finish-failure record. Unknown fields do not become
empty successful assertions. Original stream readback checks the same file
identity, size and SHA against the producer receipt before returning bytes.

A native exec event that exposes thread_id is recorded with its exact 1-based
record/pointer. turn_id is recorded only if emitted as a valid nonempty ID. A missing key
remains unknown; an explicit null, wrong type, empty or invalid string marks the
binding malformed and cannot enter unique-turn resolution. An event without an ID does
not receive an invented turn UUID. Completed native item IDs are retained for
matching the normal app-server reply. Started/completed command and MCP-tool
records and completed assistant messages have separate raw record references.
An assistant message alone is not relabelled as a final answer; actual finish
and unsupported item families are retained separately.

## Normal readonly API replies

A normally admitted reader controller owns software qualification, initialization,
bounded request/reply channels and finite pipe/process cleanup. The fixed stdio
adapter may start one owned readonly app-server process after its real launcher
capability is issued; it does not invent an endpoint or start a model writer.
Only thread/read, thread/turns/list and thread/items/list retrieve evidence. No
thread/start, turn/start, resume, inject, shell command or private rollout read
is permitted by this consumer.

For an actual native turn ID the selector requires that ID in the producer
binding. When a fresh native launch omits turn IDs, only a thread known by the
producer to be new and containing exactly one stored turn may be resolved.
Resumed launches without emitted turn IDs remain unknown. The later exact-turn
item query must include every completed item ID observed in native stdout;
foreign turns, duplicate IDs, missing native items, repeated cursors, excess
pages, wrong response IDs, duplicate/case-fold JSON keys or malformed schema
are rejected. Results have a 2 MiB frame limit, 32 MiB aggregate limit, 64-page
bound and at most a 45-second whole-connection deadline.

Each request is saved before exchange; each in-bound response is saved before
JSON/status/identity validation. Both remain byte-for-byte CreateNew artifacts,
with hashes, source method and exact job/thread/turn in the index. Error, malformed
and wrong-ID frames are evidence of refusal, not discarded before indexing.
Request/response persistence failure is separate from validation failure. A
timeout retains its request with no invented reply; overflow is not saved beyond
the existing bounds. A bounded reply already dequeued when the deadline expires is saved with
deadline_elapsed before the operation is rejected; a real timeout has no reply
artifact. Oversized frames remain outside the preservation budget.
Unique-turn resolution uses this same capture/index path. Responses are not normalized into response_item records or called
original.jsonl. A partial failed read retains a complete=false index and any
already saved raw frames; partial evidence is not a successful export.

No private history, auth/config file or protected source is read to fill model,
effort or instruction fields. Requested launch settings are labelled requested.
Actual model/effort and complete delivered instructions remain unknown unless
exposed by actual supported native records. The current exec stream does not
supply all v11 metadata/instruction fields. The frozen v11 scorer may therefore
remain INCONCLUSIVE; the exporter must not synthesize a PASS-shaped binding.

## Integration status and required shared CAS

The three new source files alone are not a callable installed feature. Required
shared connections are intentionally not represented by successful stubs:

- lib.rs module registration and one exact-job CLI metadata/read branch;
- normal attempt registration of the fixed evidence purpose, preserving normal
  sandbox, instruction loading, OAuth and existing launch-key idempotency;
- platform spawn connects Capture::owned_input_writer to that same Child and
  its taken stdin, and exposes the retained normal Job handle;
- jobs.rs tees both streams before legacy redaction, waits/drains with its
  finite native-attempt budget, calls finish and publishes FinishedExport under the same
  attempt CAS only after durable receipts;
- the outer normal consumer records actual Child wait/reap and controller
  termination, and supplies the existing qualified readonly API transport;
- pause/Job adapter supplies the real retained Job handle, never its name alone.

The source connection for those files is described below. Deployment still
requires one integrated revision with compile, independent review and the
necessary native controls. A source connection is not an observed installed
operation or a new software-admission issuer.

## Acceptance controls

The new tests exercise strict JSON schema and duplicates, raw stdin partial
write, raw stdout before masking, dropping a stream before EOF, CreateNew
collision, same-handle file hashes/identity, missing native turn IDs, exact
readonly response IDs/tuples/cursors and retained raw response bytes. Resumed or
ambiguous turn fallback, foreign turn/item substitutions, missing native items
and unsupported replies reject without private history fallback.

These are source-prepared tests until actually run. No test fixture makes an
arbitrary “verified”, job ID, hash or model field an origin capability. Windows
native collection, actual job integration and both Codex/Claude Code business
acceptance remain separate unmet conditions. This module currently implements
the Codex owned-exec producer only; it does not claim a Claude launcher.

Artifact files, duplicated process/Job handles and CNG provider/hash handles use
CloseOnce. Normal return, early read failure, partial construction, unwind and
Capture drop use its one-shot finalizer. The normal Runtime retains the actual
Child through wait and explicitly releases its primary process handle afterward.
Primary and close failures are retained separately. Failed explicit Close keeps
the original strongly owned resource in the same supervisor retention list,
without another Close attempt, with a fixed retained-close count. They keep original_complete
false and remain that owner's responsibility until the outer process is
actually collected. OS observation, std Child wait, pipe EOF and handle release
are separate fields; none substitutes for another.

## Repair and native acceptance status

The acceptance controls cover bounded-receipt/sidecar roundtrips (including 1000 small items and
limit-1/limit/limit+1), sticky fatal-read and Interrupted controls, rejected RPC
raw-byte preservation, common finalizer fault seams with strong-owner counts,
tee-seal replacement controls, and actual same-Child stdin fixture paths. The
fixture program is this crate's test executable, not an AI host; it records
input until EOF and is owned/reaped by the test. Generic writer and fixture
channels do not mint normal origin or source authority.

Source-prepared tests are distinct from executed controls. A sidecar/receipt
roundtrip alone is artifact-consumer evidence, not actual normal job publication
or installed CLI admission. No unrelated source, general guard condition
or producer/launch privilege is changed by this source slice.


## Normal supervisor and selector integration

The source connection registers the module in cc-jobs, adds the fixed purpose
run --native-evidence-purpose p33-skill-business-v11, and exposes export-native
<exact-job-id> --part metadata|prompt|stdout|stderr|origin. Other run commands keep
their existing behavior. Purpose and request digest must match on a launch-key
replay. No source path, expected hash or RPC method is accepted by the selector.

The fixed-purpose producer takes stdin from the actual owned Child and starts
its input writer alongside the original stdout/stderr tees. The capture child
is created suspended and resumes only after the same-handle capture and pipes
are connected; an earlier failure can collect that direct suspended child. The saved prompt
fact must match the frozen bytes before input can be marked persisted. Tee data
precedes legacy masking. A real retained Job handle and non-breakaway containment
bound the observed worker descendants; the controller is never counted as reaped
by its own publication. Cleanup and publication keep the generation CAS, actual
wait/reap, pipe EOF, handle-close outcomes and failure status separate.

If collection remains unconfirmed, the same controller retains the Child,
capture handles and pending reader/input threads. It saves complete=false
where possible and stays alive while those resources remain retained. The same
controller performs finite drain slices and only updates a matching generation;
a later successful drain never changes the failed business result to success. Receipt
write failure does not discard ownership. This is retention, not successful
collection; external supervisor collection and a native recovery lifecycle still
need acceptance. No global or unrelated process cleanup is claimed.

The origin selector has a fixed normal codex-launch.exe app-server stdio
adapter using the current Windows token's normal profile, initialize/initialized
and only readonly methods. Its new reader child is suspended until assigned to
its own Job, then resumed. The System runtime currently has no launcher issuer,
so origin returns Unsupported before spawning. A path/hash/bool cannot create
the NormalOriginLauncher capability; supplying its real provider is a remaining
connection responsibility, not a successful export. Request and bounded raw reply files stay unchanged;
late replies are saved and rejected. Cleanup signals the same owned writer to
finish and close stdin, observes normal exit and pipe-thread completion, then
collects only that reader Job if
needed. Forced, nonzero, incomplete or failed cleanup cannot produce origin
success. Executable software admission, normal profile ownership and H1 are
external requirements; source classification does not establish them.

These connections are source candidates until one integrated revision passes
compile, independent review and the required native controls. Runtime deployment,
Codex/Claude Code business acceptance, instruction/model visibility and actual
controller collection remain separate conditions. No unrelated issuer or general
guard condition is changed.


## Setup errors, ordering and readonly collection

Thread setup for input and both output pumps uses a fallible factory. An OS
thread-creation error enters the same normal attempt cleanup while the actual
suspended Child, capture and any previously created readers remain owned. No
resume occurs before all three setup steps succeed. The native job starts its
progress worker before acquiring that Child and keeps a completion guard across
normal and early-error paths. A failed completion remains the same owner's
responsibility; it does not spawn a replacement notification worker.

The readonly writer keeps its capacity-one FIFO. Enqueue waits only until the
existing whole-connection deadline or disconnect; a temporarily full queue is
not an invalid request. Thus initialized remains ahead of the first read without
requiring a larger queue or assuming an arbitrary delay proves delivery. The
writer's own sequential write/flush remains distinct from enqueue acceptance.

Readonly cleanup uses finite slices in the connection's owning CLI. It cannot
return to process exit while that child, Job or pipe thread remains uncollected.
The initial collection receipt and, when needed, a final drained receipt use
CreateNew. Receipt IO failure never drops the next owning state. A delayed drain
keeps the original operation failed even after resources converge; no static
owner list or separate daemon substitutes for this exit barrier.

Each Job query records its current success and active count separately from the
last observed count. Failure clears the current value and remains sticky for
operation success, even if a later query establishes collection. Child and Job
termination each record attempted before calling the API, plus accepted and
failed facts. Converged wait/EOF/close facts do not erase an earlier stop attempt
or turn forced/failed cleanup into normal operation success. The compatibility
terminate_attempted field projects either actual attempt, including API failure.


## Notification terminal states and raw stream completion

TaskProgress owns its actual notification worker JoinHandle. Pending means that
worker is still live or has not yet been joined. Done requires both the normal
completion message and a successful join; Failed includes a disconnected
completion sender, a panicked worker, or thread creation failure. A disconnected
sender is never treated as success, and its terminated worker is not waited on
as if future completion messages could arrive. Closure is requested once;
subsequent calls observe and join the same worker without starting a replacement.

Native progress collection keeps failed notification status separate from a
completed process/pipe collection. Its fixed receipt records worker start, join,
completion-message and channel-failure facts. A failed worker cannot turn the
business run into success. Ordinary nonnative close still returns false at its
existing shutdown cap while the same live worker remains pending, and can return
true after normal completion and join. A failed notification delivery remains
existing notification data, distinct from worker collection.
For the native purpose, durable job status stays live while this collection is
pending. Collection runs outside state/queue locks. The final publication then
rechecks the same job generation and held process identities, current session
fields and cancellation, and publishes the terminal result once. Existing wait
consumers cannot observe a succeeded row before a later collection failure.
The native worker drains already queued notices and is joined before this
commit. Its final notice is then delivered through the existing bounded notify
call in the same controller, not through a replacement worker. Delivery failure
remains notification data and does not reverse the durable result.
Normal nonnative publication and notification delivery semantics are unchanged.

The readonly shutdown signal stops stdin work and reply forwarding only. It does
not end raw stdout acquisition. Both connection streams use the original same-
handle byte tee, bounded by 32 MiB per stream, and require real EOF, a matching
size/hash/file-identity seal and Close outcomes. Buffered trailing lines are
preserved after the last requested reply, after a full forwarding queue, and
after the reply consumer is dropped. Read failure, overflow, early drop or EOF
not observed remain failed/unknown. Thread exit and Job emptiness alone cannot
make raw_streams_complete true.

## Native attempt budget

NATIVE_ATTEMPT_BUDGET_SECS is 600 seconds for the native-evidence purpose only.
This author-selected finite evidence-capture budget is not an owner-requested
business deadline. It bounds a single representative acceptance run so a stuck
host cannot consume its supervisor indefinitely before cleanup starts. Input,
policy and normal nonnative job timeouts are unchanged. Exceeding this budget
returns failure and starts same-owner collection; uncollected ownership may
continue after the business deadline. A task needing a longer representative
capture requires a reviewed budget change; this source does not silently retry,
extend the deadline or claim completion. Stream-budget overflow also fails the
capture and may stop that native evidence run rather than silently truncating it.

The origin directory is a single CreateNew attempt: a failed capture leaves its
original evidence in place and another request for that directory is refused.
The capture Job can include normally inherited supervisor helper processes;
active-count and same-owner cleanup observations therefore remain conservative
and are not proof about unrelated or external processes.

## One notification lifecycle, two independent outcomes

The same TaskProgress instance owns a delivery lifecycle shared with its actual
worker. Only the start retry's observed ao_notify acceptance opens the gate for
updates and finish. Four failed start attempts leave start failed and finish
suppressed, even when the worker returns normally and is successfully joined.
A joined worker and a successful notification delivery are separate facts.

Ordinary completion and cancellation still use the worker's original start
gate. Native final delivery runs in the same controller after collection and
one durable terminal publication, but must claim finish through that exact
lifecycle and matching job generation. It does not create a replacement worker.
A finish claim is consumed once, with the existing four-attempt/deadline bound.
Failed delivery is recorded without reversing the published business result;
failed worker collection still prevents native business success.

The pre-publication notification-collection receipt remains immutable. A
separate CreateNew notification-delivery.private.json records the same owner,
terminal status, actual start/update/finish attempt outcomes and worker facts
after publication. Neither receipt grants runtime access. Closing again does
not restart a worker or repeat a finish attempt. Pending collection, failed
start, failed delivery and failed collection are never collapsed into one bool.

The existing TaskProgress finish-retry entry also consumes this same finish
claim and requires confirmed collection; it cannot bypass the observed start
gate. Calling it before collection or with another attempt refuses the call
without consuming the later legitimate finish. The generic low-level notification
retry remains available for existing standalone calls, and never mints a
TaskProgress worker's accepted-start state.

Registered-export read diagnostics distinguish receipt, collection and binding
components using fixed categories (stable read, exact registered fact, strict
JSON, attempt binding, collection equality). Validation remains exact. A category
is not proof of an OS/kernel cause or permission to accept a different object.

The actual nested-Job capture fixture requires process-per-test isolation (the
normal nextest execution used by CI). Its controller is assigned irreversibly to
its test-owned Job. The helper entry tests do nothing without their controlled
environment; their standalone PASS does not count as an actual owned capture.
