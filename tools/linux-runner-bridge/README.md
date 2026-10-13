# Linux runner and the Windows Cargo gate

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

Before requesting a registration token, the host inspects the normal-mode
container's image, effective UID, dropped capabilities, `no-new-privileges`,
devices, host binds, persistent mounts, and tmpfs options. `/runner` must allow
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
overrides, other manifests, and unsupported Cargo commands are rejected. The
Windows `cargo_gate.exe` remains the only admission controller and takes the
shared per-target byte locks. Its CPU/RAM admission and clamped Cargo/test
parallelism are forwarded to Linux. Cancellation closes the Docker exec stdin.
The Linux supervisor sends SIGTERM, then SIGKILL after three seconds, and
confirms that the Cargo process group is gone before returning to the Windows
gate. It continues holding the shared target lock past the force-kill deadline
if Linux has not yet confirmed process-group exit.

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
