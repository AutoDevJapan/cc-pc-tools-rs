# PC transport

The existing Rust domain code still validates commands and edits complete JSON.
The Go bridge owns HTTP, lease renewal and the owner-protected sync credential.
The bridge never loads an admin credential.

## Configuration and activation

`todo-cloud-bridge configure --config <absolute-path>` accepts one JSON object on
stdin: `baseURL`, `syncToken`, `clientId`, `cachePath` (an absolute `todo.json`
path). It emits no credential. Windows encrypts the complete configuration with
current-user DPAPI; other systems require an owner-only file. Migration code may
call `transport.SaveConfig(path, Config)` and `transport.LoadConfig(path)`.
Go fields are `BaseURL`, `SyncToken`, `ClientID`, `CachePath`.
Use one unique ClientID per PC and one shared byte lock/cache/config for its
processes. Do not copy the same ClientID to another PC. Expired/old leases use
bounded retries; holding a matching client label never authorizes stealing a
different live process's byte lock.

Upgrade every process that can write the cache before activating remote mode.
After the initial cloud import and asset upload, run the finite command
`todo-cloud-bridge cutover --config <absolute-path> --bridge <absolute-helper>`.
It freezes the last local snapshot with the original byte lock, preserves a raw
digest-named backup, pulls into an isolated staging cache, commits the final local
delta, installs the canonical cache/revision, and only then atomically installs
`todo.remote.json`. Its marker contains only the absolute `bridge` executable
path and `credentials` configuration path. A failure or uncertain commit leaves
the marker inactive and retains the original bytes. JSON output reports the
backup path, digest, revision, activation and release state without a credential.
An already active cache rejects another cutover. A failed lease release after
activation is reported without silently undoing the successful switch.

Missing or invalid
credentials, failed HTTP, stale revisions and lost leases never enable local
writes. Removing a marker while `todo.json.remote-revision` remains is an error,
not a request to return to local authority.

`session --config <path>` is the Rust transaction protocol. Its JSON-lines control
frames are `begin` with `cachePath`, `commit` with `documentPath`, and `close`.
Commit candidates are temporary files in the cache directory, with names starting
`.todo-remote-`. The caller must already hold the existing OS byte lock at offset
zero, length one, throughout the session and until the child exits. Responses
contain only `ok`, `revision`, or a fixed error code. EOF releases the lease.
The Rust caller starts the bridge without a console window.

`current-note` with `cachePath` and a positive `itemId` is a separate, one-shot
read in this protocol. It requires the same active configuration and local byte
lock, calls only `GET /api/v1/items/{id}/note`, and exits after its response.
It does not acquire a server lease, begin a write session, or refresh the cache.
Its response contains `ok` and `currentNote`, with exactly `id`, `note`,
`noteSha256`, `revision`, and `stateId`. The server reads those fields in one
read transaction. Missing/null notes use the empty-string digest; invalid note
types and failed reads return an error without a cache fallback. Only this
exchange accepts the bounded larger reply needed for a note; ordinary control
replies keep their 4 KiB limit. Upgrade the server, bridge, then CLI before use.

One `watch --config <path>` process per cache can pull browser changes every five
seconds. Its own OS lock prevents duplicate watchers. `sync --config <path>` runs
one pull. Both use the legacy byte lock; ordinary readers only read the shared
cache. No per-task watchers or services are installed by this package.
Each watch poll first fetches `GET /api/v1/version` with `revision` and `stateId`.
If they match the small cache sidecar and the cache file has the recorded size,
the poll does not open/parse/serialize the document, acquire the ToDo byte lock,
or acquire a distributed lease. Changed versions alone call `sync`. A v1 server
without the endpoint falls back to a normal bounded sync.
`session`, `sync` and `watch` require the valid activation marker; they cannot
replace the latest local ledger before the explicit cutover.

## Transaction and recovery

After the local byte lock, acquire a server lease, stage fresh changes, and
atomically replace the cache. A digest binds the cached bytes to the saved
revision and state ID in `todo.json.remote-revision`; no state fields are added
to the original root JSON. An unchanged Begin reuses verified raw bytes without
JSON parsing/serialization. Each server revision has a fresh persistent random
`stateId`; changes carry the preceding `parentStateId`. Equal revision numbers
must have equal nonempty state IDs, and deltas must join the cache's identity
chain. A replaced/rolled-back/forked ledger, or a v1 sidecar without identity,
fetches a fresh snapshot rather than mixing histories. An HTTP failure never installs a
partial delta. The lease renews every 25 seconds. A renewal failure prohibits
further commits in that session.
Lease deadlines use local monotonic elapsed time and the server's remaining
`ttlMs`, conservatively measured from request send time. `expiresAt` is not
compared with the PC wall clock. v1 servers retain the fixed 90-second TTL.
An old locked lease may retry with the same request ID for up to 20 seconds;
Begin has a 65-second network budget within the Rust guard's 75-second timeout.

Save computes item upserts/deletions and the complete changed root metadata.
Untouched raw leaves, including decimal spellings normalized by Rust's f64
reader, are retained when they denote the same existing value. Actual changes,
integer/float distinctions, signed zero, explicit null and missing fields win.
Commit pins the lease and revision. Before POST, its exact request body, ID,
base revision/state ID, candidate digest and a nonsecret upstream-namespace hash
are flushed to an owner-protected `.remote-pending` journal (current-user DPAPI
on Windows, mode 0600 elsewhere). An uncertain response queries the operation
receipt and resends those same bytes and ID. It never replays a domain edit at a
new revision. Exhaustion remains `todo_remote_commit_unconfirmed`, distinct from
a rejected write. The CLI uses exit 76 and a structured outcome message rather
than generic exit 1; the Scheduler and question writer retain that classification.
Do not automatically repeat the domain command to resolve this outcome.

The next Begin obtains a new exclusive lease before looking up pending operation
receipts. A saved receipt confirms application. A 404 proves non-application only
when the old lease ID is no longer valid; that private recovery body is archived.
No new domain mutation runs until pending resolution and a fresh pull complete.
The journal is never sent to another upstream namespace. Tokens and task text
are absent from the small diagnostic event log.

A confirmed commit returns success even when accepted-change GET or cache
persistence fails. The pending receipt plus `.remote-invalid` durably request
repair; an unchanged version poll must still sync in this condition. The next
Begin fresh-pulls before allowing another mutation. Successful refresh applies
the accepted server change so metadata such as `next_id` is canonical.
The complete lease-renew/asset-upload/commit/receipt/cache operation shares one
60-second context within the Rust guard's 75-second timeout. Confirmation takes
precedence over a subsequent cache-pull timeout.

On a missing/mismatched cache sidecar or digest, preserve all existing bytes as
`todo.json.foreign-<sha256>` and record a durable diagnostic before replacing the
cache. Broken or oversized bytes are stream-preserved too. Preservation failure
stops replacement. These files are evidence for reconciliation, not a writable
second authority.

After renewing the lease, newly referenced owned spec/history assets upload
through the same authenticated client before the root POST. Validate digest,
byte count, exact source path and kind in the HTTP metadata. Missing unchanged
legacy references are not reopened. Failed/partial uploads do not commit root
references; identical uploads are safe to retry. Staging/cutover callers bind
their real PC home with `Session.SetOwnedHome` before Commit and must retain
staging journals on unresolved outcomes.
New commits also send optional `baseStateId` for atomic revision/state CAS. Commit
receipts and imports retain their assigned state ID on idempotent retries.

Snapshots/candidates/cache documents remain bounded by the existing 64 MiB
reader ceiling. The HTTP envelope ceiling is 72 MiB. Cache JSON is compact and
does not expand HTML characters. Existing Rust writers also check their formatted
output against 64 MiB; deploying this transport does not raise that limit. Legacy
history/spec assets remain separate and their original path fields are retained.

## Tests

`go test -race ./internal/transport` exercises fixtures and an httptest adapter
to the real store, without contacting deployed services. To also exercise the
fresh Rust binaries, supply:

- `TODO_REMOTE_TEST_CLI`: built `todo` binary
- `TODO_REMOTE_TEST_BRIDGE`: built `todo-cloud-bridge` binary
- `TODO_REMOTE_TEST_SCHEDULER`: scheduler library test executable
- `TODO_REMOTE_TEST_ASK`: question-management library test executable

The last two invoke their ignored `bridge_transaction_fixture` test against the
same synthetic HTTP ledger and DPAPI config, never the installed tools/data.
