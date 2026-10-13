# Linux runner and the Windows Cargo gate

This finite ToDo2511 bridge uses the installed `cargo_gate.exe`, its dynamic
CPU/RAM admission, and the same `cargo-build.lock.N` Windows byte locks. Linux
never reserves an independent build slot. A dedicated Windows target key maps
to the container's `/work/target`. The real-Cargo adapter passes the gate's
clamped arguments, `CARGO_BUILD_JOBS`, `RUST_TEST_THREADS` and nextest cap to Linux.

The host binds port 17851 on loopback. A fresh random 256-bit bearer key is sent
through stdin to the container and retained only in RAM and container tmpfs.
The HTTP interface accepts finite Cargo commands and checks the exact checkout
and remote branch HEAD. A job-start hook rejects foreign repositories, branches,
revisions and PR events before workflow steps. The registration token is obtained
by the existing OAuth gh gate and passed to the official runner via stdin and
`RUNNER_INPUT_TOKEN`; it is never a command argument or a Docker environment value.

No Docker socket, host directory, user profile, email or host authentication
file is mounted. Run Docker with `--cap-drop ALL --security-opt no-new-privileges`,
no GPU/device/privileged flags, non-root user 1001, and `/tmp` tmpfs. Runner
credentials created by official tools stay inside the dedicated container.
The repo-scoped runner is ephemeral, named `cc-pc-tools-linux-2511`, with label
`cc-pc-tools-linux`. The bridge checks ADMIN without requesting new permissions.

The Docker exec process keeps stdin open for the duration of each gate command.
Cancellation closes stdin; the Linux supervisor kills and reaps the Cargo
process group before returning to the gate, so the lock remains held throughout
termination. A host/gate crash or machine reboot still needs separate recovery
verification; do not claim those cases from normal cancellation tests.

Build the standalone Go module for Windows and Linux (CGO disabled). The Docker
build context contains only the Linux binary and the checksum-verified official
runner archive. The Dockerfile is deliberately outside the Rust workspace and
does not change the parent's Windows code or existing CI/CodeQL workflows.

Start: `bridge.exe host REVISION scheduler/todo-2511-... ABSOLUTE_HOST_TARGET`.
The remote branch must already point to REVISION. Stop the dedicated container
after the ephemeral job finishes, and retain private results for parent2510.
