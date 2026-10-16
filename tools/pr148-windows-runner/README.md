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
No input accepts an arbitrary command, callback, executable or download URL.

For a real runner supply actual `run_id`, `attempt`, job ID (`test`,
`native-driver`, `live-chain`), exact API job display `job_name`, workflow path,
`pr_head`, actual `workflow_sha`, SHA256 of actual workflow bytes, official
win-x64 runner version and archive SHA256. Source HEAD/tree remain the original
75a4051/cecb9cd even when the parent makes a separately reviewed routing-only PR
commit. API comparison permits changes only to the three existing workflows.
The original command bodies, OS, timeout and failure propagation are reviewed
by the parent; this helper does not generate or shorten product commands.

The parent must acquire the official `actions/runner` release archive and its
official SHA256, compare that distribution metadata independently, and pass the
archive path. `prepare` hashes the same opened bytes used for extraction and
rejects traversal, ADS, links, duplicate paths and bounded-size violations.
There is no credential in a plan or in a service command line.

## Four separate phases

Build this module into the owned root (`go build -trimpath -o <root>/bootstrap.exe .`).
Use CC-AdminBroker for the administrative phases, with no visible console/UAC.

1. `bootstrap.exe prepare <root> [official-runner.zip]` copies an immutable helper,
   makes a new demand-start service and virtual service account, and grants it RX
   on control files and write access only on the new `runtime` subtree. It does
   not start/register an official runner. Do not repeat prepare after failure.
2. `bootstrap.exe execute <root>` holds the existing `cargo_gate.exe` admission
   for the entire real run, with a dedicated target and limits 1..10. Parent must
   have a scheduler Cargo allocation. The existing parent helper feeds the
   registration token from RAM to stdin; a private one-message named pipe then
   transfers it to this exact service. Configuration runs with
   `ACTIONS_RUNNER_INPUT_TOKEN` only in the child memory environment (the official
   parser removes it). Token never enters argv, SCM, helper logs or a host token
   file. The new account's official runner necessarily writes authentication to
   `<root>/runtime/runner/.credentials` and `.credentials_rsaparams` (and runner
   configuration `.runner`). These files stay restricted to the service account,
   SYSTEM and administrators. Never read, print or copy them into evidence.
3. `bootstrap.exe stop <root>` requests SCM stop and waits finitely. Cancellation
   terminates the owned Job Object, retains member handles and verifies root,
   members and active-zero before recording drain. Unknown/timeout is failure.
4. `bootstrap.exe collect <root>` requires stopped service/PID0 and a successful
   drain receipt. Real mode also requires fresh API identity and repository
   runner count zero. It removes only the service and its new-root ACEs, and
   records process/profile/hive/classes separately. Any residue is incomplete.
   It never unloads/deletes a profile behind a denied cleanup or removes a root.
   Preserve raw evidence + SHA/bytes first. Then safe-delete plan, its normal
   deletion in a separate call, and finish; a rejection is not retried/bypassed.

For a **new credential-free fixture account only**, `cleanup-profile <root>`
is a separate single-attempt Windows `DeleteProfileW` operation. It requires
the matching creation receipt, service absence and zero owned processes. A
durable attempt marker precedes the OS call. Refusal is retained without an
unload/retry/alternate cleanup path. It does not accept any other SID/profile.
Real runner profile cleanup belongs to the parent integration. Always check
fresh profile/hive/classes absence; the delete API return alone is not proof.

`prepare` and `collect` require administrator access. `execute`/`stop` require
SCM start/stop access; use the same existing broker when needed. Real execution
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

Before starting the service, the parent must materialize **the fixed real Git
checkout** into the runner's exact `_work/civ6-banseiki/civ6-banseiki` path inside
the new runtime, without checkout credentials. Otherwise the start hook fails
closed before workflow steps. The start hook, `verify-source <root>` workflow
guard after checkout and completed hook all verify actual Git HEAD/tree/index,
tracked bytes, same-repo credential-free origin and absence of checkout auth
keys/files. The parent must retain the original checkout commands with
persist-credentials false and install the intermediate guard before product
commands. A changed/dirty checkout is failure, not an automatic reset.

There is deliberately no arbitrary privileged adapter for Rust/C/PowerShell.
Original Rust/Windows, native-driver and live-chain unit commands stay in the
reviewed workflow. The shared gate, SDK and sccache must already be usable by the
filtered new service token without new shared grants. Lack of that access is a
remaining integration condition, not permission to broaden it. No live game.

## Credential-free service fixtures

Set `fixture` to `normal` or `cancel`. No runner archive, source, network,
registration or Cargo resource is used. Use a separate safe-delete root/account
for each fixture. A real filtered worker creates one live child and proves it
cannot reopen the launcher or overwrite the immutable plan. Normal worker exit
and explicit SCM cancellation both go through the service's Job Object drain.
The controller records the actual child PID/handle completion and collection
results. This adds service-account integration to 2746's existing job fixture.

`go test -v ./...`, `go vet ./...`, `go build -trimpath ...` are the checks for
this module. Fixture results prove only the stated finite service lifecycle.
They do not prove official runner registration, real checkout isolation, SDK/
gate compatibility, original product tests, or a successful GitHub CI job.

Official behavior references:
[runner input handling](https://github.com/actions/runner/blob/main/src/Runner.Listener/CommandSettings.cs)
and [job hooks](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/run-scripts).
