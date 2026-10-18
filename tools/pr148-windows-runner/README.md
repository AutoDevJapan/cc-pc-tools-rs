# PR148 finite Windows runner helper

Parent integration update: the real plan now pins the explicit routing-only new
HEAD/tree, with PRHead and WorkflowSHA equal to that HEAD, and requires the exact
Actions job ID. The only accepted event is the full same-branch workflow_dispatch.
Original 75a/cecb/base pins remain immutable ancestry and implementation evidence;
credential-free fixtures retain the original pins and cannot enter official hooks.
Both OS helpers import `cc/product-ci/pr148scope` for identical workflow bytes and
full tree comparison. The parent product-ci module must be present beside this
module when building; no installed shared runner or admission setting is changed.
The real target is the fixed workspace's `tools/target`, preserving scenario paths.

ToDo2790 adds a controller-only, bounded GitHub object materializer, new Rustup
homes, installed MSVC/PowerShell/Git discovery under the filtered service token,
and fixed Rust tool-entry guards. See TODO-2790-CHECKPOINT.md in the worktree
root for the exact source, binary and synthetic fixture evidence. Earlier
2749/2763/2764 results below still apply only to their recorded source.
No product source was fetched or materialized by the child, no official runner
was registered, and no product CI success is claimed. Parent integration and
independent review remain required before formal use.

Independent Windows amd64 Go module for parent ToDo2736. It never edits the
product, registers a runner during a fixture, starts Civ6/GPU, changes the shared
bridge/gate/SDK ACLs, or reads the old 2710/2730 accounts/resources. Implementation
references are `f7acf6d:tools/ci-check-2710/{winservice_windows.go,
windows_checks_windows.go,windows_execution_windows.go,process_windows.go}`.
The 2746 Job Object fixture is prior evidence, not a substitute for the new
virtual-account service lifecycle tested here.

## Input contract

Create a new root with `safe-delete.exe create --project todo-2749 --content ...`.
Its 32-digit ownership ID is `instance`; root basename must be
`codex-owned-<instance>`. Write credential-free `plan.json` there. The JSON schema
is the tagged `Plan` struct in `contract.go`; unknown fields are rejected. All
of repository, repository ID, PR148, original branch and immutable ancestry pins,
base and the parent's Windows label are fixed constants. `seconds` is 1..14400.
`controller_sid` is the parent's current user SID, never another user's token.
Optional `cleanup_seconds` reserves 60..14400 seconds for each collection ACL
propagation, with zero/omission selecting 1800 seconds. The plan is fixed before
the runtime is populated, so parent chooses a conservative estimated upper bound
for artifact scale, not a measured future size. Early completion adds no wait.
Allow at least `2 * cleanup_seconds + overhead` for a real collection and
`4 * cleanup_seconds + overhead` for a fixture (use 1800 when omitted). At the
maximum setting this is up to 8/16 hours plus the other collection steps;
preparation retains its shorter 30-second operation limit. A timed-out or denied
collection mutation remains uncertain and is not automatically replayed.
No input accepts an arbitrary command, callback, executable or download URL.

For a real runner supply actual `run_id`, `attempt`, job ID (`test`,
`native-driver`, `live-chain`), exact API job display `job_name`, workflow path,
`pr_head`, actual `workflow_sha`, SHA256 of actual workflow bytes, official
win-x64 runner version and archive SHA256. Source HEAD/tree are explicit new pins whose sole parent is original
75a4051/cecb9cd. API comparison accepts exactly the canonical changes to all
three workflows, preserving every other tree entry and mode.
The original command bodies, OS, timeout and failure propagation are reviewed
by the parent; this helper does not generate or shorten product commands.

The parent must acquire the official `actions/runner` release archive and pass the
archive path. `prepare` independently reads the versioned official release asset
metadata through `gh_gate.exe`, binds name/URL/version/SHA256, then hashes the same opened bytes used for extraction and
rejects traversal, ADS, links, duplicate paths and bounded-size violations.
There is no credential in a plan or in a service command line.

Immutable source blobs use at most four controller readers, all drained before
publication or failure return. The existing preparation deadline, per-blob/hash
checks and 256 MiB source bound remain in force. GET transport failures explicitly
reporting TLS handshake timeout, unexpected EOF or connection reset receive at
most three attempts with bounded backoff inside the parent deadline. Auth/policy
failures, size violations and invalid JSON are not retried. Error records contain
request hashes, byte counts, exit codes and status classes, never server text or
source contents. Cancellation and response-size errors are reported separately.
Output capture uses named buffers (not embedded ReaderFrom methods) so stdout's
size limit and stderr's 8 KiB preview cannot be bypassed by io.Copy. Run owns the
copy goroutines; a two-second WaitDelay bounds inherited-pipe cleanup after exit
or cancellation. A pipe timeout is rejected without a transport retry. Real
source-transfer timing still requires formal execution.

## Four separate phases

The host controller preserves AppData and explicitly configured gh/XDG config
locations through the Cargo gate so the same existing OAuth store remains in
use. Token environment variables are excluded. The worker retains its isolated
HOME/AppData; no owner config path or OAuth file is copied into its environment.

Build this module into the owned root (`go build -trimpath -o <root>/bootstrap.exe .`).
Use CC-AdminBroker for the administrative phases, with no visible console/UAC.

1. `bootstrap.exe prepare <root> [official-runner.zip]` copies an immutable helper,
   makes a new demand-start service and virtual service account, and grants it RX
   on control files and write access only on the new `runtime` subtree. It does
   not start/register an official runner. Do not repeat prepare after failure.
2. `<root>/helper.exe execute <root>` runs as the original controller, without
   elevation or a broker/token-file step. Preparation grants this SID only
   START/STOP/QUERY on this new service; controller preflight reads control files
   and ancestors without descending into the private runtime. Execution holds the existing `cargo_gate.exe` admission
   while controller/gate remain alive and orderly stop is confirmed, with a
   dedicated target and limits 1..10. Abrupt-stop limits are described below. Parent must
   have a scheduler Cargo allocation. The existing parent helper feeds the
   registration token from RAM to stdin; a private one-message named pipe then
   transfers it only after its kernel server PID matches the running SCM service
   and a temporary process handle confirms lifetime during that validation.
   The handle closes after validation; the connected pipe remains bound to its
   server endpoint. Configuration runs with
   `ACTIONS_RUNNER_INPUT_TOKEN` only in the child memory environment (the official
   parser removes it). Token never enters argv, SCM, helper logs or a host token
   file. The new account's official runner necessarily writes authentication to
   `<root>/runtime/runner/.credentials` and `.credentials_rsaparams` (and runner
   configuration `.runner`). These files stay restricted to the service account,
   SYSTEM and administrators. Never read, print or copy them into evidence.
3. `<root>/helper.exe stop <root>` requests SCM stop and waits finitely. Cancellation
   terminates the owned Job Object, retains member handles and verifies root,
   members and active-zero before recording drain. Unknown/timeout is failure.
4. `bootstrap.exe collect <root>` requires stopped service/PID0 and a successful
   protected launcher report plus the matching SCM exit result and a pre-delete
   process census. The report handle is opened before spawning children, then
   its DACL prevents the service account/job from reopening it. Real mode also requires fresh API identity and repository
   runner count zero. It removes only the service and its new-root ACEs, and
   records process/profile/hive/classes separately. Any residue is incomplete.
   It never unloads/deletes a profile behind a denied cleanup or removes a root.
   Preserve raw evidence + SHA/bytes first. Then safe-delete plan, its normal
   deletion in a separate call, and finish; a rejection is not retried/bypassed.
   Collection removes the two administrator-installed inheritable grants with
   bounded operations and reads their DACLs directly, rather than spawning an
   external process per target file. Each mutation has durable attempt/result
   files; failed or uncertain actions cannot be repeated. Fresh collection
   snapshots distinguish profile residue from service/ACL failure. Process
   census reports inaccessible processes instead of claiming a complete census.

For a **new credential-free fixture account only**, `cleanup-profile <root>`
is a separate single-attempt Windows `DeleteProfileW` operation. It requires
the matching creation receipt, service absence and zero owned processes. A
durable attempt marker precedes the OS call. Refusal is retained without an
unload/retry/alternate cleanup path. It does not accept any other SID/profile.
Real runner profile cleanup belongs to the parent integration. Always check
fresh profile/hive/classes absence; the delete API return alone is not proof.

`collect-unstarted <root>` is a separate administrative abort path for a real
prepared resource whose worker never began. It requires stopped service/PID0,
zero queryable owned processes, no worker/hook/authentication artifacts, the
unchanged two-byte initial launcher report and the exact queued official job
with zero steps and no runner. Authentication files are tested for absence only.
The normal collection API checks and mutation journals still apply, including
after a prior successful service-delete phase. It restores only this new root
and runtime's original controller grant after removing the service grant, so
normal safe-delete can follow. It does not delete profiles or claim CI success.
Started resources still require the protected launcher drain report through
ordinary `collect`. A failed or uncertain mutation is never retried.

`prepare` and `collect` require administrator access through CC-AdminBroker.
`execute`/`stop` use the preparation's service-specific controller grant. Real execution
must run under the parent's existing OAuth/API context for read-only audits.
No API keys, new daemon, owner token impersonation or SDK grant is introduced.

## Admission and source guards

`audit <root>` reads only the exact repository/PR/commit/workflow/run/attempt/jobs
and repository runner list via the existing `gh_gate.exe`. Owner/private/nonfork,
same repository, original base/branch, routing-only changes, workflow bytes,
actual job label/name and residual registrations must match. Hooks reject every
different repo/PR/source/run/attempt/job/workflow/runner context and require the
original shared Cargo admission, dedicated target, bounded parallelism and
sccache. Registration uses ephemeral/no-default-labels/disableupdate and one
unique instance name; it never replaces another registration.

`execute` always enters the shared gate. Its dedicated `test` callback validates
LOCK_HELD, limits, and an actual live direct parent at the fixed cargo_gate.exe
path. Environment flags alone cannot bypass it. The authenticated controller
forwards those validated allocation values to the service; the service validates
the bounded message and its kernel client PID, and does not manufacture a gate
flag. Hook env values are propagated allocation claims, not a separate kernel
admission proof. The trusted controller and installed gate are the boundary;
this is not protection from a malicious operator who already owns the token.

Ordinary controller errors after a successful start now make one owned stop
attempt before returning from the gate callback (including SCM open/query
errors). A refused or uncertain stop is returned, not retried. This does not
make detached SCM lifetime crash-safe: forced controller/gate termination or
an unconfirmed stop may leave service work until its finite deadline. Parent
integration must supervise orderly stop/drain before releasing admission and
must fence an uncertain run before starting another. Extending/releasing the
shared gate or using a new resident bridge to conceal this limit is outside
this helper. Do not claim full-run admission retention after an abrupt kill.

Actual workflow SHA equals the explicitly pinned dispatch HEAD, and its digest
must match the canonical proposal and actual source bytes. Successful
execution additionally requires all three context-bound hook/source receipts in
the launcher's protected result, actual completed/success job on this unique
runner, and no remaining registration. Terminal observation has a finite budget.
Hook receipts share the job account's writable runtime. They detect missing or
mismatched stages, but cannot authenticate execution against malicious code in
that same account. The launcher protects its own measured drain/exit report;
it does not turn a job-written receipt into independent host attestation.

Real `prepare` now materializes the exact explicit dispatch HEAD into the fixed
workspace, before installing its new virtual service. GitHub OAuth remains in
the controller. Commit/tree/blob JSON enters bounded RAM; the complete recursive
tree, every blob and the exact reconstructed commit must reproduce Git object
IDs. Routing verification first enforces the sole original parent and canonical
three-workflow delta. Loose objects, detached HEAD, shallow boundary, credential-
free origin, exact bytes and index are created without clone/checkout or an
arbitrary host command. Unsupported path/mode is a failure, never an omission.
The materializer has an eight-minute API budget and finite object/RAM bounds.

The canonical workflow still adds no guard step. `prepare` installs this finite
helper as rustup/cargo/rustc/rustdoc/rustfmt at the new CARGO_HOME/bin, also the
path toolchain actions normally prepend. The first official Rust invocation
checks the post-checkout context, actual HEAD/tree/index/raw tracked bytes and
credential absence, then creates the existing `verify-source` receipt. Subsequent
invocations recheck the source. Raw hashing also catches assume-unchanged index
entries and bypasses Git filters. Active worktree configuration, Git metadata links, alternates, replacement
objects and worktree config are refused before ordinary Git object operations.
The original argv, output and exit code are forwarded to the same new SDK under
the filtered account, inheriting the launcher's Job Object. The entry itself
checks membership in some Job, not that Job's identity. The source tools/target
remains the actual target. The entry requires this instance's service SID, AU deny-only token and Job
Object membership. It forwards the job environment so action-added PATH entries
and explicit compiler selections survive. Rustup auto self-update is disabled
in this new home so an update does not replace these guards.
There is deliberately no arbitrary privileged adapter for Rust/C/PowerShell.
Original Rust/Windows, native-driver and live-chain unit commands stay in the
reviewed workflow. The shared gate and installed SDK executables must be usable by the
filtered new service token without new shared grants. Lack of that access is a
remaining integration condition, not permission to broaden it. No live game.
The new SDK is initialized by fixed non-administrative commands under the same
Job Object. Official rustup-init bytes are verified against the public SHA256;
Rust1.95.0 and stable with fmt/clippy are installed into new homes. Existing
MSVC is selected by the fixed installed vswhere/VsDevCmd interface, with only
SDK environment keys retained. Installed Git, PowerShell, sccache and shared gate
read access are measured without owner-profile discovery or shared ACL changes.
The controller holds the actual shared gate for the full orderly service run;
the service never manufactures a new gate admission or claims direct lock access.
The same installed sccache binary runs in a new finite foreground process with
an owned cache and port. The kernel listener PID and Job membership are checked.
The helper configures its own server for compiler work in this Job. This does not prevent arbitrary job code from opening other loopback ports, including the owner cache server; the account boundary is not a network sandbox.
Shared cache files/configuration are unchanged; dependency reuse there is not
claimed. Its lifetime ends with this service's Job Object.
This account boundary is not a full VM sandbox.
Runner credentials are accessible to its own job account; the restriction keeps
them out of the owner profile/host logs, not away from code in that same account.
Go strings cannot be reliably zeroized; RAM-only token handling is best effort,
with byte/UTF16 buffers cleared and no token file/argv/log delivery path.

## Credential-free service fixtures

Set `fixture` to `normal` or `cancel`. No runner archive, source, network,
registration or Cargo resource is used. Use a separate safe-delete root/account
for each fixture. A real filtered worker creates one live child and proves it
cannot reopen the launcher or overwrite the immutable plan. Normal worker exit
and explicit SCM cancellation both go through the service's Job Object drain.
The controller records the actual child PID/handle completion and collection
results. This adds service-account integration to 2746's existing job fixture.

`integration_windows_test.go` provides an opt-in controller regression. Compile
it into a safe-delete-owned control root (`go test -c -o <control>/tests.exe`).
For each newly prepared normal/cancel plan, set PR148_TEST_ROOT and run only
TestNonAdminFixtureController. It disables administrator membership/privileges
on the caller's own token, proves SCM-create and runtime-list access are denied,
then exercises allowed START/QUERY/STOP and reads the protected actual exit/drain
report. It never takes another account's token or starts an official runner.

`go test -v ./...`, `go vet ./...`, `go build -trimpath ...` are the checks for
this module. Fixture results prove only the stated finite service lifecycle.
They do not prove official runner registration, real checkout isolation, SDK/
gate compatibility, original product tests, or a successful GitHub CI job.

Official behavior references:
[runner input handling](https://github.com/actions/runner/blob/main/src/Runner.Listener/CommandSettings.cs)
and [job hooks](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/run-scripts).

Repository-wide runner-zero checks require Linux and Windows official listeners
to run serially. The full Rust OS matrix remains in the same dispatch. Collection
uses immutable repo/run/source identity plus registration absence after protected
local drain; it does not require a successful or still-running CI job, or a live
PR branch, and never certifies CI success. A failed collection API read retains
the stopped runtime and its original mutation journal without repeating deletes.

## New runtime fixtures (ToDo2790)

`runtime_fixture: true` is valid only with a normal/cancel fixture plan. It
cannot authorize an official runner. Preparation supplies a compiled-in synthetic
Git/API source, not the product. Real gate admission is used, then the filtered
service initializes the new SDK and runs the tiny synthetic unit with both
1.95.0 and +stable. Changed tracked source must refuse a tool before execution;
a real Cargo error must retain exit101. The cancel fixture publishes readiness
only after a live Rust test writes its PID, then SCM stop must drain Cargo, Rust,
cache and worker processes together. No product workspace/native/live/scenario
result or official GitHub run is inferred from these fixtures.

## Actual checkout connection

Use a new safe-delete root beneath a SHORT registered helper worktree for Node and MSVC
checkout (`safe-delete create --root <short-helper-worktree> ...`). Its existing
ancestors must allow metadata reads by the service. Node20 resolves each main
script ancestor: the owner's private Temp path failed on the owner's profile
metadata in the credential-free probe. That profile ACL was not changed. New
workspace roots use existing readable ancestor ACLs. The long child worktree also exceeded the MSVC standard-library filename limit; the short original registered worktree passed the same checkout and Rust commands without shared changes. Only the new source is
assigned to the newly created service SID.

Preparation supplies a new HOME/.gitconfig with the exact source safe.directory.
GIT_CONFIG_GLOBAL points there. No owner configuration or credential is copied.
The source guard also rejects credential/execution keys in this new global file.
Checkout's sparse-checkout disable leaves an inactive config.worktree after
unsetting extensions.worktreeConfig. Only literal disabled sparse-checkout core
values and disabled index.sparse are accepted in that inert file; the activation key and all other content
are rejected. SDK initialization rehashes all five guards after installing the
new toolchains and refuses missing/corrupt environment state. Direct cargo-fmt,
cargo-clippy and clippy-driver proxies remain outside the entry-guard contract;
the original cargo commands enter through the guarded cargo executable.

The runtime fixture uses public actions/checkout v4 resolved to a full commit,
checks dist/index.js against its Git blob, and uses its declared Node20 engine
from the official checksum-verified public distribution. Its actual main and
post programs run against a loopback smart-Git server containing only synthetic
objects. The local server accepts only the two upload-pack routes. Synthetic
transport URL and placeholder auth are isolated from product API/OAuth. After
checkout and post, raw source/index/HEAD/tree, global/local auth absence, service
ownership and ordinary Git without a per-command safe.directory are checked
before adapting the fixture transport URL for the unchanged Rust entry guard.
In the saved acceptance fixtures the NUL observation ran first on the same
source and returned exit 0, with a safe.directory warning. The subsequent new
HOME main/post results therefore prove a repeated checkout, not pristine-source
checkout. Their raw ordering and exit codes must accompany the evidence.
No official Actions runner, workflow dispatch or product checkout is inferred.

The fixture's resolved checkout commit and Node ZIP digest are SDK evidence;
the Node distribution is not the official runner's bundled Node. Public SHA
verification detects corrupt bytes but is not an independent signature check.
The shared workflow transformer is designed to preserve the checkout action version;
the parent must match its resolved version before claiming product acceptance.

The source owner operation has a 30-second limit and prepare is single-use. This
limit is not measured on the product tree. A timeout makes prepare fail and leaves
partial preparation, never a successful runtime or an invitation to retry prepare.
Preserve the failure and report that new resource's service/ACL/files to its cleanup
task. The normal collect command requires a protected launcher drain report and
cannot be assumed to accept a resource whose preparation failed before launch.
Synthetic success does not qualify product-scale preparation.
SDK guard hashes detect accidental corruption; the same service account can
write the tool directory, so these checks do not defeat malicious job code.

The archived acceptance helper has VCS 23274f6+modified and Go content correlated
to cd7dbd3; it is not a clean binary built at 9b4a529. Its SHA matches both prepared
receipts. The later SID conversion has race/vet/build evidence, but no new service,
Rust or cancellation fixture run. See TODO-2790-CHECKPOINT.md for exact hashes.

Suggestions S2/S5 from the d5e04c5 review remain evidence limits: the real product
materializer's eight-minute sequential API budget is not measured by synthetic
fixtures; uncertain original commit bytes fail closed. Each normal/cancel result
must keep its own exact source and observed controller exit. The latest source
must obtain its own finite review; the earlier Must0 is not a blanket approval.
