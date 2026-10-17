# `cc/ci-source-2737`

This package is a RAM-only source packet boundary for the fixed GoDD MCP Alpha
CI source. It is a module, not a product checkout command or an executor.

## API

- `Pin` requires lowercase 40-character commit and tree SHA-1 values and a
  lowercase 64-character SHA-256 digest of `.github/workflows/ci.yml`.
- `Fetch(ctx, pin)` uses only the configured OAuth `gh_gate.exe` CLI. The
  repository, owner, PR, and head ref are package constants. It verifies the
  repository/permissions, same-repository open PR, branch ref, commit/tree,
  workflow bytes, complete recursive tree, and repository/ref again after the
  reads. It is deliberately not tested against the real GitHub source here.
- `Encode(packet)` and `Decode(raw, expectedPin)` use a 256 MiB bounded strict
  JSON envelope. Decode rejects unknown/duplicate fields, multiple documents,
  invalid object hashes, and any packet whose Pin differs from the caller's
  expected Pin. The packet is sensitive source data: keep it in RAM and pass it
  only through the intended protected pipe. Do not log, persist, or attach it.
- `Materialize(ctx, packet, expectedPin, freshGuestRoot)` is a guest-side API.
  Call it only inside the newly isolated Linux guest, with a new empty root,
  Git installed, and no checkout credentials. This package does not expose a
  host checkout CLI. Transport authentication (including mTLS) belongs to the
  host/guest transport adapter and is outside this module.

The GitHub archive is bounded at 128 MiB compressed and expanded, individual
files at 64 MiB, and entries at 200,000. The tar parser requires a single common
top-level directory and regular files only. It rejects links, devices, absolute
or traversal paths, Windows separators/streams/device names, `.git`, duplicate
or case-colliding paths, oversized data, and files absent from or inconsistent
with the API tree. Gitlinks are restored as index entries only; their targets
are never fetched.

Materialization hashes each archive blob with `git hash-object --no-filters`,
restores the API's original index modes, checks `write-tree`, writes only the
original commit payload, and confirms detached `HEAD` and `HEAD^{tree}`. Git
system/global configuration is isolated, an empty template is requested, and
local config key names are inspected without emitting values; remote,
credential, filter, submodule, and hooks-path keys cause rejection. Source
commands, filters, hooks, and submodules are not run. Windows builds are for
host integration only; Linux guest execution supplies POSIX executable-mode
semantics. Windows filesystem collisions and reserved path forms are rejected
before materialization.

Integration in ToDo2737 additionally verifies each file after writing it,
including Unix executable modes, before issuing `physical_blobs_verified`.
The materializer and completion hook now share `VerifyCheckout`. It opens every
path component without following links, reads and hashes through that same
handle, rejects multi-link files and revalidates file/directory identities.
Linux uses openat/O_NOFOLLOW; Windows rejects reparse points and pins ancestors
without write/delete sharing. A final-file Lstat or lexical containment is not
the physical proof. A regular, byte-identical file under a linked parent is
rejected, including links at the root or above it.

Only the bounded real Git metadata needed for HEAD/tree/index is copied into a
new private proof directory; Git commands use that snapshot instead of reopening
the untrusted source. Authentication/filter/hook/include/extension configuration
is rejected before snapshot persistence. The checked config bytes are reused,
not read again. Gitdir/common-dir indirection, alternates, replacement refs and
secondary worktree config are rejected. Directory/file identities and metadata
stamps are rechecked before returning. Snapshots are local to the isolated
guest for real product source and removed on return; Windows tests contain only
synthetic data. No manifest, original guard, permission or external allowlist is
added to accept a link.
The original commit payload is reconstructed when GitHub's unsigned commit
response has null verification payload, and accepted only if the pinned Git
SHA is reproduced. Signed payload candidates have the same strict SHA check.
Git's global PAX archive metadata is not interpreted as a filesystem member.
Output buffers do not expose a promoted unbounded `ReadFrom`, and archive
trailer draining shares the expanded-byte bound. All inherited GIT_* overrides
are removed from materialization commands.

Both Windows and real Linux synthetic Git tests passed after integration.
Real API Fetch for the pinned b194 revision passed; host source stayed in RAM.
The mTLS adapter then materialized 1,904 original blobs in a dedicated Linux
guest and verified the saved files, HEAD and tree. VM stop/memory zero/removal
passed. This is source preparation only: no product commands, Cargo tests,
runner registration, workflow execution or GitHub success status was performed.

Receipts contain only repository/PR numeric IDs, pinned hashes, archive digest,
and blob count. Errors are fixed and do not include source paths, names, or
contents.

## Local synthetic verification

Run `go test ./...`, `go vet ./...`, and cross-build with `GOOS=windows` and
`GOOS=linux`. Tests create only their own `testing.T.TempDir` synthetic Git
fixtures. They verify actual commit/tree/blob reproduction, ignored tracked
content, disabled clean filters, gitlink non-fetch behavior, archive and hash
tampering, strict Pin/JSON handling, and unsafe tar boundaries. No test fetches
or executes the original product source.
