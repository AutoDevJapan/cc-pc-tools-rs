# Linux runner and the Windows Cargo gate

## Bounded additional capacity (ToDo3122)

The default compiled profile remains `primary`; its names, labels, finite mode,
admission and resident lifecycle are unchanged. Build both Windows host and
Linux guest with `-ldflags '-X main.normalBuildProfile=burst-3122'` for the single
additional slot. Its container/image are `todo3122-linux-capacity[:local]`, runner
name is `cc-pc-tools-linux-3122`, and bridge port is 17862. It retains the existing
`cc-pc-tools-linux` label to accept already queued jobs without redispatch.
The fixed build profile cannot be selected by a job's environment.

The additional slot uses official ephemeral registration without `--replace`,
the normal current-head hook and the same Cargo gate. Its supervisor runs one
`--once` session, drains it and reclaims only its owned scratch, then both guest
and host exit. A host exit is not proof that the GitHub job passed.

`bridge.exe pool <this-worktree>/.ci-work/target` supplies at most one such host
at a time, at most 16 per invocation, with a two-hour admission window. Before
each one it reads current same-repository queued jobs and fresh Windows/VM
capacity. Windows must have at least 24 GiB free, commit below 85%, CPU below
75%, and 30 GiB free on each of C/D; the Docker VM must have 9 GiB available.
The additional container is separately capped at 4 CPUs, 8 GiB and 1024 PIDs.
Use the existing hidden S4U/Limited Task Scheduler management pattern, IgnoreNew,
and a five-minute recurrence to recheck remaining work. The pool holds loopback
port 17863 to reject duplicate managers before registration. It stops when the
queue drains or admission fails, never interrupts an active job to meet its
admission deadline, and records its stage in `.ci-runtime/pool-status.private.json`.
`bridge.exe queue` returns the observed eligible count and oldest UTC queue age.
Keep this supporting worktree/runtime while the task uses it; remove only owned
test resources after verification. Reboot recovery is not claimed.

The bridge keeps ToDo2511's finite mode available and adds a resident mode for
ordinary `AutoDevJapan/cc-pc-tools-rs` CI. The finite invocation still requires
one fixed revision and its approved task branch, uses port 17851 and the
`todo2511-cc-pc-tools-linux` container, and accepts only its original event set.

## Resident mode

The resident runner is repo-scoped, named `cc-pc-tools-linux`, and uses the
`cc-pc-tools-linux` label. It runs in `todo2510-normal-linux`; the host bridge
listens only on `127.0.0.1:17852`. The host process obtains a short-lived runner
registration token from the existing OAuth `gh_gate.exe` and sends it through
Docker stdin. It does not put the token in arguments, Docker environment,
source files, or logs. The runner's working files and credentials live in
container filesystems; `/runner` and `/tmp` are tmpfs mounts.

Create both finite and normal containers with Docker's `--init` so orphaned
descendants are reaped. Hosts reject missing, null or false init settings before
registration. Existing containers must be recreated with `--init` while idle.
Before requesting a registration token, the host inspects the normal-mode
container's image, effective UID, dropped capabilities, `no-new-privileges`,
init/reaper setting, devices, host binds, persistent mounts, and tmpfs options. `/runner` must allow
execution for the official runner scripts; `/tmp` must remain `noexec`. A
container-side preflight checks that the scripts are executable, the tmpfs
mounts are writable, and no Docker socket exists. The Cargo target must be a
real directory below this worktree's `.ci-work`, without symlink substitution.
When the same container already has a registered runner, a host-task restart
atomically refreshes only the bridge key and leaves the runner session intact.
It obtains a new GitHub registration token only when the container's tmpfs has
lost its runner configuration.

The pre-job hook reads the GitHub event JSON. For pushes it accepts only the
repository's `main` branch and requires the event commit to equal the current
remote `main` HEAD. For pull requests it requires the event's head SHA, head
branch, head repository, and base repository to identify a same-repository PR
to `main`; the current remote head for that branch must still equal the event
head SHA. It never selects the pull request merge SHA for the source revision.
The Cargo adapter repeats those checks and verifies that the checkout's actual
HEAD is the approved head before asking the Windows gate to run Cargo. Workflow
checkout steps must explicitly check out `github.event.pull_request.head.sha`
for pull requests; a merge-ref checkout is rejected at the first Cargo call.

Only Cargo subcommands used for checks are accepted. The only explicit manifest
path allowed is the repository-relative `cc-bookmark-queue/Cargo.toml`.
Absolute or parent-relative paths, Cargo configuration and target-directory
overrides, other manifests, and unsupported Cargo commands are rejected.
This is a cooperative gate for trusted repository jobs using bridge-mediated
Cargo. It is not a kernel boundary against arbitrary native commands. These
builds use Windows `cargo_gate.exe` for admission and the
shared per-target byte locks. Its CPU/RAM admission and clamped Cargo/test
parallelism are forwarded to Linux. Cancellation closes the Docker exec stdin.
The Linux supervisor sends SIGTERM, then SIGKILL after three seconds, and
confirms kernel process-group absence before returning to the Windows gate.
Successful leader exits also drain any background descendants before returning
the original exit result. If force-kill exceeds its deadline, one diagnostic
reports that the target remains occupied.
The direct child is waited concurrently, and init reaps orphaned descendants.
Unreaped zombies and unknown states retain the lock past the force-kill deadline;
a process-list snapshot is never authority to release it. Descendants deliberately
escaping the group with `setsid` are outside this process-group contract.

The runner uses non-root UID 1001, drops all Linux capabilities, enables
`no-new-privileges`, and has no GPU devices, Docker socket, host bind mount,
host profile, email, or host authentication files. The Docker network is used
only for the GitHub runner and loopback bridge traffic. No API key is required.

Build the standalone Go module for Windows and Linux with CGO disabled. Build
the Docker image with only the Linux bridge binary and the checksum-verified
official runner archive in its context. Keep the image/container names specific
to this normal mode; do not remove the separate ToDo2511 container.

Start the resident host process with:

```text
bridge.exe host normal <absolute-worktree>/.ci-work/target
```

The normal container must already exist and use the matching image, tmpfs
mounts, and isolation options. The bridge starts it when stopped, then runs
until the runner exits. A normal OS restart has not been tested by this mode;
do not report reboot recovery as verified without a separate test.

### Bounded resident scratch and diagnostics (ToDo3026)

Normal mode uses the existing supervisor for successive `run.sh --once`
sessions. Registration remains repository-scoped and reusable; it does not
register an ephemeral runner for each job. The pinned official runner supports
`--once` ([upstream implementation](https://github.com/actions/runner/blob/v2.338.0/src/Runner.Listener/Runner.cs)); upstream warns this flag may be
deprecated. Upgrading the official runner must recheck this lifecycle contract.
The existing pre-job admission hook and Windows Cargo gate are unchanged.

Each session owns a unique scratch directory below the existing noexec `/tmp`.
`TMPDIR`, `TMP`, and `TEMP` point to that directory. It is reclaimed only after
the listener exits, the kernel confirms its process group is gone, and no other
worker or unknown process remains in the container. An escaped or unreaped
process prevents cleanup and the next session. Each idle observation retries
for at most 30 seconds, so the existing short-lived host probes and init's
reaping window do not strand scratch. Zombies remain protected until reaped;
an ESRCH while reading `/proc` means that process has disappeared. The single supervisor holds a
nonblocking kernel lock across sessions; the host's existing recovery path
recognizes it between listeners. No new polling task is introduced.

Gated Cargo starts through a separate Docker exec and does not inherit the
worker's environment. The host forwards only the fixed normal-mode marker; the
guest derives the scratch path from the active owned diagnostic session and
rejects missing, completed, or substituted sessions. No caller-supplied scratch
path or host environment is forwarded. Finite mode keeps its original behavior.

`/runner/_diag` points to a session-owned directory below the existing persistent
`/work/bridge-diagnostics-todo3026`. Only directories bearing the bridge owner
record and an end marker written after drain are retention candidates.
After each session, retain at most eight ended sessions and 64 MiB of their
uncompressed files, removing oldest ended sessions first. This is an idle
retention bound, not permission to truncate active diagnostics. Unfinished
evidence is kept; more than eight unfinished sessions or 64 MiB of unfinished
data prevents further sessions pending finite recovery. At the next locked,
verified-idle start, owned interrupted scratch is reclaimed and a distinct
`interrupted` marker closes only those diagnostics. It never writes `complete`
or alters any job result. Their original bytes remain until normal retention
expires them. Unknown ownership stays unfinished and is never reclaimed.
Owner records are written to a pending file, synced, and renamed into place.
An idle restart can remove an empty owned allocation shell or finish publishing
an intact pending owner record. Nonempty data without a valid record is kept.
Owner access may be restored on same-filesystem scratch directories only;
files and symlink targets keep their permissions. The supervisor lock is
close-on-exec. Links, special files,
unknown entries, and unreadable ownership records fail closed.

Existing configuration diagnostics are renamed intact within `/runner`, outside
automatic retention. Existing `/tmp` entries and authentication files are never
swept. `/work/todo3024-diag-before-restart.tar.gz` is outside the owned diagnostic
root and remains preserved. Deployment must use the original scheduled task
while idle, preserve the existing `/work` data, and verify matching host/image/
container artifacts before a normal GitHub job is accepted. Merely restarting
the old task does not install this change.

The live-proc test runs inside the CI Worker. Its explicit test baseline uses
PID plus kernel start time to distinguish existing test-framework processes
from newly created probe processes. Production always uses a nil baseline and
real `/proc` reads, and still rejects any Worker or Listener ancestor. No
environment variable or runner argument enables the test baseline.
