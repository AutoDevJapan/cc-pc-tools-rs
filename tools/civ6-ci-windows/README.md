# civ6-ci-windows (ToDo2965, parent ToDo2962)

Independent Windows amd64 Go module that serves the three Windows jobs of
AutoDevJapan/civ6-banseiki (repo ID 1392152644) with **one official ephemeral
GitHub Actions runner per job**, on this PC, without Actions minutes:
`test (windows-latest)` (rust.yml), `native-driver` and `live-chain`.
It is a new module. The old `tools/pr148-windows-runner`, `tools/product-ci`,
ToDo2736 and the live Linux27/Windows28 runners are not edited or stopped. The
audited SCM / virtual-account / filtered-token / Job Object / fixed Git
materializer / Cargo-gate / collection code was carried over and adapted.

The shared identity policy is the parent's module `cc/civ6-ci-contract`
(`replace cc/civ6-ci-contract => ../civ6-ci-contract`). `tools/civ6-ci-contract` in this branch is
the parent's commit `6332ffff4a33a6a14852719776aa7604e925de05` (PR245) imported unchanged with
plain `git checkout <commit> -- tools/civ6-ci-contract`; the parent integrates the final version.
This module only calls `Plan.Validate`, `Labels`, `Verify`, `VerifyAssigned`, `RequireSuccess` and `CheckEvent`; it never decides
repository/source/workflow policy itself. Nothing here registers a runner, fetches
product source or runs real Rust unless the queue drives the CLI below.
No GPU, Civ6, API key, child Codex or visible console is used (`CREATE_NO_WINDOW`).

## Plan (`<root>/plan.json`, no secrets)

One flat JSON document, unknown fields rejected. The shared fields are exactly the
parent's `contract.Plan` tags; the rest are local operational fields.

| field | meaning |
| --- | --- |
| `version` 1, `repo_id` 1392152644 | fixed |
| `head`, `tree` | the tested commit and its tree (what `actions/checkout` materializes) |
| `event` | `pull_request` \| `push` \| `workflow_dispatch` (never `dynamic`: CodeQL is Linux) |
| `branch`, `pr` | actual source branch; same-repo PR number (main: 0) |
| `context_sha` | actual `GITHUB_SHA`; for a PR this is the **merge** commit and differs from `head` |
| `workflow_sha`, `workflow_ref`, `workflow`, `workflow_digest` | actual workflow commit/ref/path and SHA-256 of its bytes |
| `run_id`, `attempt`, `job_id`, `job`, `job_name` | the official run/attempt/job (`test (windows-latest)`, `native-driver`, `live-chain`) |
| `instance` | 32 lowercase hex = the safe-delete ownership id; root basename `codex-owned-<instance>` |
| `root` | absolute, clean path of that safe-delete root |
| `runner_version`, `runner_digest` | pinned official `actions-runner-win-x64-<v>.zip` release asset and SHA-256 |
| `controller_sid` | SID of the user that runs `execute`/`stop`/`status` (non-admin) |
| `seconds` 1..14400, `cleanup_seconds` 60..14400 or 0 (=1800) | run deadline; per-ACL-propagation budget |
| `fixture` \| `runtime_fixture` | credential-free lifecycle fixtures only; never official |

`Plan.validate` = local checks + `contract.Plan.Validate` + Windows-only rules: workflow
and job must be one of the three, OS must be Windows, labels must be exactly
`[self-hosted, Windows, X64, civ6-ci-windows]`, the original unrouted PR149 pins are
rejected for official use, runner pin required.

Example (pull request; compact):
`{"version":1,"repo_id":1392152644,"head":"<40>","tree":"<40>","event":"pull_request","branch":"feature/x","pr":150,"context_sha":"<merge 40>","workflow_sha":"<merge 40>","workflow_ref":"AutoDevJapan/civ6-banseiki/.github/workflows/rust.yml@refs/pull/150/merge","workflow":".github/workflows/rust.yml","workflow_digest":"<64>","run_id":1,"attempt":1,"job_id":2,"job":"test","job_name":"test (windows-latest)","instance":"<32>","root":"...","runner_version":"2.338.0","runner_digest":"f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f","controller_sid":"S-1-5-21-...","seconds":3600}`

Official runner asset (read from `actions/runner` latest on 2026-10-19; `prepare`
re-reads the versioned release through `gh_gate.exe` and requires name, URL,
version and digest to match, then hashes the same opened bytes it extracts):
`v2.338.0`, `actions-runner-win-x64-2.338.0.zip`,
`sha256:f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f`.
The queue downloads the archive and passes its path to `prepare`.

## CLI (exact inputs and outputs)

Build into the owned root: `go build -trimpath -o <root>/bootstrap.exe .`
(create the root with `safe-delete.exe create --project <p> --content <c>`; the
`id` it returns is `instance`). All stdout lines below are credential-free JSON.

| command | who | input | result |
| --- | --- | --- | --- |
| `bootstrap.exe prepare <root> <runner.zip>` | CC-AdminBroker (admin) | plan.json, official zip | copies immutable `helper.exe`; materializes **the pinned commit from GitHub objects** (`contract.Verify` first) into RAM then `<root>/runtime` only; installs rustup/Rust 1.95.0 + stable under the new CARGO_HOME/RUSTUP_HOME; creates the demand-start service `CC-CIW-<instance>` (virtual account) and ACEs on the new root/runtime only; writes `prepared.json`. Single use, exit 0/1. |
| `helper.exe execute <root>` | controller (non-admin) **inside the scheduler's Cargo allocation** | registration token on **stdin** (RAM only) | enters `cargo_gate.exe test --target-dir <root target>` as a child; `contract.Verify` + own-runner-absent check; token goes through a one-message pipe to the service (kernel client/server PIDs checked) and only into the official configure input; waits for the service to stop; checks start/complete/after (below). Exit 0 only for a verified successful job. |
| `helper.exe stop <root>` | controller | - | SCM stop; the service Job Object drain is confirmed or it is a failure. |
| `helper.exe status <root>` | controller | - | one line: `{"instance","service","service_present","state","pid","exit_code","prepared.json"}` (SCM state 1 = stopped). |
| `bootstrap.exe collect <root>` | CC-AdminBroker | stopped service, PID 0, protected launcher drain report | removes the service and the two ACEs, checks processes/profile/hive/classes, writes `collection-<ns>.json`; exit 0, **3 = residue only**, 1 = failure. |
| `bootstrap.exe collect-unstarted <root>` | CC-AdminBroker | queued, never-started real job | abort path (SCM 1077, no worker artifacts, zero-step queued job). |
| `bootstrap.exe cleanup-profile <root>` | admin | credential-free fixture plan only | one `DeleteProfileW` attempt; real plans are refused. |
| `helper.exe audit <root>` | controller | - | read-only `contract.Verify` + runner/job state. |

`hook-start`, `verify-source`, `hook-completed`, `service`, `worker` are internal (job hooks run
in the service account; the controller never relies on them for GitHub reads).

Ledger events on stdout (one JSON object per line, for the queue's durable ledger):
`execute-begin`, `job-start-verified`, `job-complete-verified`,
`execute-end {ok, service_started, service_stopped, fence}`, `collect-begin`,
`collect-end {complete, residue_only}`. Every event carries `instance`, `run_id`, `attempt`,
`job_id`. Do not use the CC-AdminBroker "exit=" line as the result: read `collect-end`
and `collection-*.json`. The broker reports 0 for a nonzero script exit.

## Admission and verification points

Phase mapping: before registration `contract.Verify`; job start and completion
`contract.VerifyAssigned(plan, "CC-CIW-<instance>")` (the matrix `GITHUB_JOB` is shared by all OS
expansions, so only the unique runner name identifies the assigned job); after the drain
`contract.RequireSuccess` (completed, success, steps present, on this runner), checked only once the
job is terminal and the registration is gone. `Verify` itself covers (repo owner/private/non-fork/admin, current branch/PR, official
run/attempt/job/labels, whole source tree, reviewed bytes of all three workflows at both
`head` and the actual workflow commit) is run **before registration** (`prepare` and
`execute`), when the job **starts** on this unique runner, when it **completes**, and
**after** the service has drained. The controller polls the target job every 10 s
(`monitor_windows.go`); a policy mismatch stops the service at once, GitHub read
failures are tolerated for at most 6 consecutive polls. The job's runner must be
`CC-CIW-<instance>` with exactly the labels `self-hosted, Windows, X64, civ6-ci-windows`.
Only this plan's unique registration is inspected; the long-lived workers and the Linux
ephemeral runner are neither read nor affected. The hook checks the actual SDK context
with `contract.CheckEvent` (PR merge context vs checked-out head are separate), source
HEAD/tree/index/raw bytes, credential-key absence and the shared gate allocation.
Success requires the three hook receipts, the service result, and an official job
`completed/success` on this runner with the registration gone - listener exit alone is not success.

Registration uses `--ephemeral --disableupdate --labels civ6-ci-windows` (default
labels `self-hosted, Windows, X64` are kept so it matches the reviewed `runs-on` array).

## Runtime pins and shared resources (read/execute only, no shared ACL change)

- `D:\CC\Tools\codex-jobs\gh_gate.exe` (OAuth GitHub reads; no API key), `cargo_gate.exe`
  (the allocation is held while the controller and the service are alive; released only
  after SCM stop, which happens after the Job Object is drained),
  `D:\CC\Tools\sccache\sccache.exe` (own finite foreground server, own cache dir and port).
- rustup-init from `static.rust-lang.org` verified against its published SHA-256;
  toolchains `1.95.0` (default) and `stable` with rustfmt/clippy; installed MSVC (vswhere),
  Git for Windows, PowerShell 7 discovered by fixed path.
- Source and registration token never come from an existing product folder or an owner
  profile; the token is never in argv, SCM, a file or a log.

## Crash and uncertainty

`execute-begin` without `execute-end`, or `fence=true`, means the service may still own the
namespace: the queue must not start another Windows worker until `status` shows state 1 and
`collect` completes. A normal error after a successful start makes exactly one stop attempt; a
refused/uncertain stop is returned, not retried. A forced kill of the controller/gate is **not**
survivable by this helper (the service's own deadline applies); the fence is the protection.

## Collection and the profile residue (measured, not hidden)

`collect` verifies: service absent, owned process census 0 (unreadable processes disclosed),
launcher drain report, both ACEs removed (DACL re-read), then reports profile/hive/classes.
Measured 2026-10-19 on this PC: **every** virtual service account ever created here (24 listed in
ProfileList, incl. both new fixtures) keeps its `C:\Windows\ServiceProfiles\<service>` directory
and a **loaded** `HKU\<sid>` hive after the service is deleted, although no logon session of
that account exists any more. The old 2736 cleanup attempts (`DeleteProfileW`) were refused with a
sharing violation (32). So each official job adds one such residue until a safe OS-level fix is
found. Contract: collection exits 3 (`residue_only`) with the same receipts; there is no
unload/retry/alternate cleanup path here, `cleanup-profile` stays limited to credential-free fixtures,
and the queue must count the residue and report it to the OS recovery owner (ToDo2381) instead of
retrying. The helper does not try to hide it. A persistent per-OS service identity would avoid
the growth but contradicts the "new service SID per job" requirement; that is the parent's decision.

## Verification actually performed (2026-10-19)

- `go test -race ./...`, `go vet ./...`, `go build -trimpath` (windows/amd64) and `go vet` for
  linux/amd64; credential-free unit tests for plan validation, event/context checks (including
  PR merge context vs head), hook receipts, audit phases (before/running/finished), unique-runner
  registration, collection identity, the run monitor, unstarted collection, SCM/ACL helpers.
- Real new service lifecycle fixtures with the **non-admin** controller and admin prepare/collect
  through CC-AdminBroker: `normal` (worker exit 0, 3 Job members drained, AU deny-only token, launcher
  reopen denied) and `cancel` (explicit SCM stop, worker exit 125, 4 members drained, `collect-end
  residue_only`). Raw summaries: `evidence/fixtures-20261019.json`. Roots were removed with safe-delete.
  The normal fixture ran a binary with the execute events but before the collect events and the
  residue-only exit code; the cancel fixture ran after both.

## Not done (by design of this child)

No official registration, no product source materialization, no real Rust/native/live execution,
no GitHub Actions run. `contract.Verify` against the live repository, the full
`prepare` source materialization with the new contract and the monitor against a real job are
exercised only by synthetic replies here and must be proven by the parent's PR149 run.
Runtime fixtures (`runtime_fixture`) from the old module were not re-run.


