# PR148 finite Windows runner helper

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
of repository, repository ID, PR148, original branch, original source HEAD/tree,
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
win-x64 runner version and archive SHA256. Source HEAD/tree remain the original
75a4051/cecb9cd even when the parent makes a separately reviewed routing-only PR
commit. API comparison permits changes only to the three existing workflows.
The original command bodies, OS, timeout and failure propagation are reviewed
by the parent; this helper does not generate or shorten product commands.

The parent must acquire the official `actions/runner` release archive and pass the
archive path. `prepare` independently reads the versioned official release asset
metadata through `gh_gate.exe`, binds name/URL/version/SHA256, then hashes the same opened bytes used for extraction and
rejects traversal, ADS, links, duplicate paths and bounded-size violations.
There is no credential in a plan or in a service command line.

## Four separate phases

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

Actual workflow SHA must be the test merge with exactly base/PR-head parents,
and its workflow digest must match both merge and PR-head bytes. Successful
execution additionally requires all three context-bound hook/source receipts in
the launcher's protected result, actual completed/success job on this unique
runner, and no remaining registration. Terminal observation has a finite budget.
Hook receipts share the job account's writable runtime. They detect missing or
mismatched stages, but cannot authenticate execution against malicious code in
that same account. The launcher protects its own measured drain/exit report;
it does not turn a job-written receipt into independent host attestation.

Before starting the service, the parent must materialize **the fixed real Git
checkout** into the runner's exact `_work/civ6-banseiki/civ6-banseiki` path inside
the new runtime, without checkout credentials. Otherwise the start hook fails
closed before workflow steps. The start hook, `verify-source <root>` workflow
guard after checkout and completed hook all verify actual Git HEAD/tree/index,
tracked bytes, same-repo credential-free origin and absence of checkout auth
keys/files. The parent must retain the original checkout commands with
persist-credentials false, explicit `ref: 75a4051305b6f47c70ddb59a8d2bf9015666f12c`, and install the intermediate guard before product
commands. A changed/dirty checkout is failure, not an automatic reset.
Parent materialization into the service-only workspace is a separate privileged
integration boundary, outside this helper and this child task. Before commands,
untracked/ignored files and execution-affecting Git config are rejected. After
commands, tracked bytes/HEAD/tree/auth remain checked; generated untracked build
output is allowed. Materialize with byte-preserving Git settings (autocrlf off).

There is deliberately no arbitrary privileged adapter for Rust/C/PowerShell.
Original Rust/Windows, native-driver and live-chain unit commands stay in the
reviewed workflow. The shared gate, SDK and sccache must already be usable by the
filtered new service token without new shared grants. Lack of that access is a
remaining integration condition, not permission to broaden it. No live game.
The empty new cargo/rustup homes and SDK PATH preparation still need actual
integration by the parent. This account boundary is not a full VM sandbox.
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
