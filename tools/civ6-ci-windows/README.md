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
(`replace cc/civ6-ci-contract => ../civ6-ci-contract`). Parent integration imports this Windows module from fixed commit b2db8b30fb363df81f777f9bcdbc258094c1dddc, retaining the newer contract merged by PR249 (3423a38, public repositories accepted with fixed owner/source checks). This module uses the contract's
`Plan`, `Validate`, `Labels`, `Verify`, `VerifyAssigned`, `RequireSuccess`, `CheckEvent`, `MatchRunPath` and its shared
constants/helpers (`ValidSHA`, `Ref`, `OS`); it never decides
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
`[self-hosted, Windows, X64, civ6-ci-windows, civ6-ci-run-<run>-<attempt>-<job>-Windows]`, the original unrouted PR149 pins are
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
| `bootstrap.exe prepare <root> <runner.zip>` | CC-AdminBroker (admin) | plan.json, official zip | copies immutable `helper.exe`; **claims the fixed shared service identity first** (see below); materializes **the pinned commit from GitHub objects** (`contract.Verify` first) into RAM then `<root>/runtime` only; installs rustup/Rust 1.95.0 + stable under the new CARGO_HOME/RUSTUP_HOME; grants ACEs on the new root/runtime only; writes `prepared.json`. Single use, exit 0/1. |
| `helper.exe execute <root>` | controller (non-admin) **inside the scheduler's Cargo allocation** | registration token on **stdin** (RAM only) | enters `cargo_gate.exe test --target-dir <root target>` as a child; `contract.Verify` + own-runner-absent check; token goes through a one-message pipe to the service (kernel client/server PIDs checked) and only into the official configure input; waits for the service to stop; checks start/complete/after (below). Exit 0 only for a verified successful job. |
| `helper.exe stop <root>` | controller | - | one SCM stop request, refused unless the shared service is currently bound to this root; the Job Object drain is confirmed by `execute`'s hold, not by this command. |
| `helper.exe status <root>` | controller | - | one line: `{"instance","service","service_present","state","pid","exit_code","prepared.json"}` (SCM state 1 = stopped). |
| `bootstrap.exe collect <root>` | CC-AdminBroker | stopped service, PID 0, protected launcher drain report | holds the identity lock for the whole run; removes the two service ACEs and the launcher-report grant, **resets the ACL of every descendant** to what it inherits from the root, resets the owner of the whole root to Administrators, returns the root to the controller, **verifies recursively that no entry grants the shared SID**, checks the account has no process and the service is idle and still bound to this root, **keeps** the shared service and its one profile, writes `collection-<ns>.json` (`service_retained`, `service_idle_confirmed`, `owner_reset`, `tree_acl_reset`, `tree_entries_verified_without_service_grant`, `shared_profile_retained`, profile counts) and, as the very last step, the durable marker `HKLM\SOFTWARE\CC\civ6-ci-windows\<service>\CollectedRoot` (REG_SZ naming an owned root; the key's DACL is replaced on every write: SYSTEM/Administrators full, authenticated users read; measured: a non-admin write is denied). Every entry's DACL is read through its own handle opened with `FILE_FLAG_OPEN_REPARSE_POINT`, so no entry is judged by a link target (`tree_links_verified_without_service_grant` counts links); exit 0 or 1. |
| `bootstrap.exe collect-unstarted <root>` | CC-AdminBroker | queued, never-started real job | abort path (untouched launcher report, no worker/authentication artifacts, zero-step queued job, service idle). |
| `helper.exe audit <root>` | controller | - | read-only `contract.Verify` + runner/job state. |

`hook-start`, `verify-source`, `hook-completed`, `service`, `worker` are internal (job hooks run
in the service account; the controller never relies on them for GitHub reads).

Ledger events on stdout (one JSON object per line, for the queue's durable ledger):
`execute-begin`, `job-start-verified`, `job-complete-verified` (a job shorter than the 10 s poll may skip these two; the
mandatory success evidence is `job-success-verified` after the final `RequireSuccess`),
`job-success-verified`, `execute-end {ok, service_started, service_stopped, fence}`, `collect-begin`,
`collect-end {complete, shared_profile_retained}`. Every event carries `instance`, `run_id`, `attempt`,
`job_id`. Do not use the CC-AdminBroker "exit=" line as the result: read `collect-end`
and `collection-*.json`. The broker reports 0 for a nonzero script exit.

## Admission and verification points

Phase mapping: before registration `contract.Verify`; job start and completion
`contract.VerifyAssigned(plan, "CC-CIW-<instance>")` (the matrix `GITHUB_JOB` is shared by all OS
expansions, so only the unique runner name identifies the assigned job); after the drain
`contract.RequireSuccess` (completed, success, steps present, on this runner), checked only once the
job is terminal and the registration is gone. `Verify` itself covers (repo owner/ID/non-fork/admin, current branch/PR, official
run/attempt/job/labels, whole source tree, reviewed bytes of all three workflows at both
`head` and the actual workflow commit) is run **before registration** (`prepare` and
`execute`), when the job **starts** on this unique runner, when it **completes**, and
**after** the service has drained. The controller polls the target job every 10 s
(`monitor_windows.go`); a policy mismatch stops the service at once, GitHub read
failures are tolerated for up to 5 consecutive polls (the 6th ends the run). While the job is still queued the plan is also re-verified once a minute, and a job that is
completed without ever being assigned to this runner (cancelled while queued) ends the run at once, so it does not hold the Cargo slot. The job's runner must be
the unique runner name `CC-CIW-<instance>` with the default Windows labels, `civ6-ci-windows`, and the shared contract's unique `JobLabel(plan)`.
Only this plan's unique registration is inspected; the long-lived workers and the Linux
ephemeral runner are neither read nor affected. The hook checks the actual SDK context
with `contract.CheckEvent` (PR merge context vs checked-out head are separate), source
HEAD/tree/index/raw bytes, credential-key absence and the shared gate allocation.
Success requires the three hook receipts, the service result, and an official job
`completed/success` on this runner with the registration gone - listener exit alone is not success.

Registration uses `--ephemeral --disableupdate --labels civ6-ci-windows,<JobLabel(plan)>` (default
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
`collect` completes. A normal error after a successful start makes exactly one stop attempt (never repeated).
After it, and also on the success path, one routine (`holdUntilDrained`, observation only) keeps the gate child -
and so the shared Cargo slot - alive until SCM reports STOPPED **and** the protected launcher report proves the
Job Object drained (`job_drained` for this account SID, or a published report whose launcher never created a worker).
An unreadable SCM state is never taken as a stop: polling continues to the deadline. The deadline is anchored at the service
start and **shared**: the controller sends the absolute run deadline (service start + 60 s + `seconds`) through the
registration pipe, the launcher refuses to resume a worker after it and never runs one past it, and the hold lasts
90 s longer (Job drain budget), so slow startup cannot make the worker outlive the hold. A STOPPED service without drain proof
is released early only when a **complete** process census of its account is empty; the non-admin controller cannot classify
every process (the saved collections show one unreadable process), so in practice that case holds to the deadline.
It always ends with `fence=true`/an error because the drain was not proven. A forced kill of the
controller/gate is **not** survivable by this helper (the service's own deadline applies); the fence is the protection.

## One fixed Windows service identity (parent decision 2026-10-19)

Measured on this PC: every per-job virtual service account (24 in ProfileList, `evidence/profile-residue-measurement-20261019.json`)
kept its profile directory and a loaded hive after the service was deleted, so a per-job account grows the OS
profile set without bound (the old 2736 `DeleteProfileW` attempts were refused with sharing violation 32). The
parent therefore fixed the identity:

- The SCM service and its virtual account are one fixed name for this repository, `CC-CIW-civ6-banseiki`, and one
  `CC-CIW-civ6-fixture` for credential-free fixtures. They are owned by this module only (no 2736/other CC reuse).
  The service is demand-start, never a resident runner. Its OS profile/hive is **one** bounded continuing resource,
  recorded by `collect` as `shared_profile_retained` with counts; it is not per-job residue and is not claimed as cleaned.
- Everything per job stays unique: `instance` (nonce), root, plan, runtime/SDK/credentials/source/temp/cache,
  the official runner name `CC-CIW-<instance>`, the registration pipe and the ephemeral registration.
- `prepare` (`ensureService`) creates the service on first use and otherwise **re-points it** at the new job
  (`ChangeServiceConfigW` image path; the controller SD now also holds QUERY_CONFIG so start/stop can check the binding).
  The whole read-check-reconfigure runs under one exclusive byte-range lock on `<ProgramData>\CC\civ6-ci-windows\<service>.lock`, where ProgramData comes from the OS known
  folder (`SHGetKnownFolderPath`), never from the caller's environment, so every phase locks the same file. The file and its folder are
  plain (no links), inheritance is removed, the non-admin controller only gets read/write data (no DELETE, WRITE_DAC, WRITE_OWNER); the lock
  is shared with `start`, `stop` and `collect` and dies with the process. It refuses
  unless the service is STOPPED with PID 0, the account has no process, the existing image path parses strictly
  (`<root>\helper.exe service <root>`, owned-root form) and the previous job left **durable collection proof**: its root equals the
  registry marker `CollectedRoot`. The proof does not depend on the old path, so a deleted, kept or quarantined (moved) root are all
  fine once collected, and a root moved before collection is refused. Parsing trouble refuses instead of assuming "no previous job".
  One-time migration: a fixture identity created before the marker existed needs the marker set once by an administrator; the real
  identity does not exist yet.
- `start` (under the lock) refuses unless the service's image path names this plan's root and the root is not already collected;
  `stop` checks the binding and issues its request under the lock (its wait for STOPPED is outside it); `collect` holds the lock for
  the whole run. A delayed command for an earlier job therefore cannot start or stop the next job's worker, and nothing can start
  while a collection changes ACLs.
- `collect` (see the table) never deletes the service and tries no profile deletion/unload; there is no `cleanup-profile` command
  any more. The whole-tree reset is needed because the worker can disable inheritance below the root and grant any principal anything;
  removing two inherited grants does not undo that. Cost: time proportional to the tree (budget `cleanup_seconds`).
- Legacy per-job profiles from earlier fixtures (6a8d1e39..., c4809f99..., 052658b1...) remain as measured residue
  (ToDo2381 owns recovery); no retry or alternate path exists here.

Measured on the one fixture identity, first with three consecutive jobs (normal, cancel, normal;
`evidence/shared-identity-fixtures-20261019.json`: distinct instances and service PIDs, worker exit 0/125/0, Job Object drained each
time, queryable account process count 0 at each collect, CC-CIW profile count 4 = 3 legacy + 1 shared, no growth), then with
an intermediate design (`evidence/shared-identity-fixtures-2-20261019.json`; that "previous root must be deleted" rule is superseded),
and finally with the current collect/lock/marker code (`evidence/shared-identity-fixtures-3-20261019.json`): normal A; cancel B prepared while
A's collected root still existed (allowed); a **stale `stop` from A's helper was refused** ("belongs to another job") while B owned the service;
C prepared and left uncollected, so a following D prepare was **refused** ("no durable collection marker"); C then ran and was collected.
Each collect verified 30 entries below its root without a service grant; worker exit 0/125/0; `fence=false`; profile count 4 throughout.
Console values were read from the run output; the B and C collection files were saved before deletion. With the final lock/marker code
(`evidence/shared-identity-fixtures-4-20261019.json`) a normal and a cancel job ran back to back: 30 entries and 0 links verified each, the non-admin controller
could read but not write the marker, and the lock file ACL was the minimal one above. Recorded as observed: sequential prepare allowed after collection, an uncollected
predecessor refused, a stale stop refused. Not observed live: a link below a job root, two processes contending for the lock, a collect racing a start,
a different `ProgramData` environment (unit-tested only), a root moved before collection, and the slow/refused-stop paths.

## Verification actually performed (2026-10-19)

- `go test -race ./...`, `go vet ./...`, `go build -trimpath` (windows/amd64) and `go vet` for
  linux/amd64; credential-free unit tests for plan validation, event/context checks (including
  PR merge context vs head), hook receipts, audit phases (before/running/finished), unique-runner
  registration, collection identity, the run monitor, unstarted collection, SCM/ACL helpers.
- Real service lifecycle fixtures with the **non-admin** controller and admin prepare/collect through
  CC-AdminBroker. First per-job-service design (superseded, `evidence/fixtures-20261019.json`): normal (worker exit 0,
  3 Job members drained) and cancel (SCM stop, exit 125, 4 members drained). Current fixed-identity design
  (`evidence/shared-identity-fixtures-20261019.json`): normal, cancel, normal on the shared fixture identity, with the
  hold-until-drained stop boundary (`fence=false` with launcher drain proof each time). Roots were removed with safe-delete.
  Not exercised live: lock contention between two real processes, a collect racing a start, a slow/refused stop, an
  unclassifiable-census hold, a root that was moved before collection, a link below a job root, and the real (non-fixture) identity
  `CC-CIW-civ6-banseiki`. The deadline, hold, image-path and lock-path rules are unit-tested only; the marker rule has two sequential live cases.

## CI of this module

This branch adds no workflow of its own. The parent's `.github/workflows/civ6-ci.yml` runs race/vet/build
for every `tools/civ6-ci-*` module per OS on the PC runners; integrate through that workflow.
Local results: `go test -race ./...`, `go vet ./...` and `go build -trimpath` pass for this module.

## Queue contract notes

One Windows worker at a time. The workflow requests and the registration supplies a selecting label containing
the run ID, attempt, fixed job key and matrix OS. Native-driver, live-chain, Rust Windows, another run and another
attempt therefore do not match the same listener. The hook still verifies the actual assignment. The shared identity
also refuses a second `prepare` until the previous job was collected. The queue admits only one Windows plan at a time,
count each admitted run/attempt/job once in the 12-job budget, and never rerun a failed one automatically.
A cancelled or failed collection of that case is handled like any other.

## Not done (by design of this child)

No official registration, no product source materialization, no real Rust/native/live execution,
no GitHub Actions run. `contract.Verify` against the live repository, the full
`prepare` source materialization with the new contract and the monitor against a real job are
exercised only by synthetic replies here and must be proven by the parent's PR149 run.
Runtime fixtures (`runtime_fixture`) from the old module were not re-run.

## Known limits found by the last review (all conditional, none reproduced; review ToDo2999 reported Must 0)

- The lock folder/file and the marker key get an explicit protected DACL for the principals this module names, but an already existing
  resource with an unexpected owner, an extra explicit grant from an earlier controller SID, or a writable ancestor is not detected and
  not rejected. The measured facts are only: the non-admin controller could read but not set the marker value, and the lock file showed the
  minimal ACL on this PC. Delete/subkey/DACL-change denials were not measured separately.
- `evidence/shared-identity-fixtures-4-20261019.json` names its code as "working tree on top of 772fbce"; no binary digest ties it to a
  commit. The last live run (normal job, 30 entries, 0 links) used the final collect code and is recorded here, not in a saved file.
