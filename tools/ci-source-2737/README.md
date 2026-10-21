# `cc/ci-source-2737`

This package is a RAM-only source packet boundary for the fixed GoDD MCP Alpha
CI source. It is a module, not a product checkout command or an executor.

ToDo2766 adds one explicit `product: press-2766` profile. It accepts only
private AutoDevJapan repository1386702183, Draft PR175, its fixed task branch,
and base4dc5c332d54d19325f122df1f0e4a52e9dd3a629. `workflow` must be exactly
`local-checks.yml`, `rust-v2-https.yml`, or `windows-hidden-fixtures.yml`.
The head, tree and selected workflow digest remain explicit immutable pins.
The empty product/workflow pair retains the original MCP α contract. Unknown
products, paths, partial profile selection and CodeQL are refused before API
reads. This module adds no host materialization command, workflow mutation or
dispatch. The separately linked finite `pressci` resolver now connects the
guest controller to the two Windows roles and original Linux roles; that delta
still needs peer review and actual guest acceptance before live use. Physical
materialization tests use public synthetic Git repositories, not original
product source on the host.

PR execution additionally pins `pull_head_sha` and `merge_base_sha`. `head_sha`
then denotes the actual execution merge, never the candidate. Both live refs
and the PR merge SHA must agree before and after Fetch, and the actual commit
payload must have those exact two ordered parents. `VerifyCheckout` inspects
that payload from its anchored snapshot. The empty profile cannot select merge
fields. Signed commits are reconstructed from the exact API unsigned payload
and detached signature (including its possible final continuation); every
accepted reconstruction must reproduce the real pinned Git object ID. No
normalized timestamp, missing newline or substitute commit is trusted.

## API

- `Pin` requires lowercase 40-character commit and tree SHA-1 values and a
  lowercase 64-character SHA-256 digest of the selected compiled workflow path
  (`.github/workflows/ci.yml` for the empty MCP α profile).
- `Fetch(ctx, pin)` uses only the configured OAuth `gh_gate.exe` CLI. The
  repository, owner, PR, and head ref are package constants. It verifies the
  repository/permissions, same-repository open PR, branch ref, commit/tree,
  workflow bytes, complete recursive tree, and repository/ref again after the
  reads. Unit tests use public synthetic source. ToDo2766 also exercised the
  real fixed Press merge packet in RAM: 49,306,964 archive bytes, 2,812 tree
  entries, 2,315 blobs; no host checkout or guest success was claimed.
  A rejection reports a fixed stage only. Identity-stage diagnostics distinguish
  the repository, pull request, and branch ref, and distinguish an identity
  mismatch from API start/context/drain/exit/JSON/capture/denied/transport/other
  failure. Complete nonzero-exit stderr is bounded to 64 KiB and classified in
  RAM; truncated capture is rejected without classifying its prefix. They do not
  contain response bodies, endpoints, identity values, or underlying error text.
  These diagnostics add no retry and do not relax any identity check.
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
active or unknown secondary worktree config are rejected. Directory/file identities and metadata
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
# Bounded metadata rejection diagnostics

`MetadataFailure(err)` returns a detached, validated `MetadataDiagnostic` only
for a rejection from the Git metadata snapshot stage. Its code and kind are
closed enums; the other fields are the observed entry count, file size and
running byte total. It contains no path, object ID, config key, content or OS
error text. The existing `source_metadata` hook code remains unchanged and may
carry this optional detail. An error at another proof stage has no such detail.

The 128 MiB per-file, 512 MiB aggregate and 200,000-entry limits, anchored reads,
link rejection, HEAD/tree/index checks and physical path revalidation remain in
force. A sparse oversized synthetic object verifies the file-limit category;
it does not prove that a pack limit caused the original CI failure. The original
failure still needs an observation from the actual completion hook.

## Inactive sparse-checkout residue

The completion hook in run 39201344542 attempt 2 rejected an 83-byte regular
`.git/config.worktree` left by checkout's native `git sparse-checkout disable`
followed by removal of `extensions.worktreeConfig`. Read-only examination of
the stopped, owned guest disk found only `core.sparseCheckout=false`,
`core.sparseCheckoutCone=false` and `index.sparse=false`; its primary config
contained no extension keys. The public synthetic Git fixture reproduces the
same bytes and SHA256
`443a5f645c23c3d0c0aa09f634b2ad111d46ef61946b598a2fb311678ab47454`.
[Git's configuration documentation](https://git-scm.com/docs/git-config#FILES)
describes secondary worktree configuration as conditional on that extension.

The verifier accepts only those exact inactive bytes, after rejecting every
`extensions.*` key in the primary configuration. It reads them through the
existing anchored, no-follow, single-link file handle and revalidates that
identity before returning. They count toward the existing aggregate bound and
are never copied into the private Git snapshot. HEAD/tree/index and physical
blob verification still run. No original source or metadata is rewritten and
no guard, registry, permission or authorization manifest is added or changed.
An active extension, changed/extra/empty config, external identical hardlink,
directory or symlink is rejected. The fixture obtains the positive bytes from
Git itself; it does not manufacture an acceptance manifest.

The old verifier failed the positive native-Git reproduction. After the fix,
Windows source-module race/vet and both Windows consumers passed. Actual Linux
source tests passed (17 top-level tests) and guest tests passed (22, with two
SDK opt-in tests explicitly skipped); Linux vet and test-binary compilation
also passed. This is regression evidence, not a successful rerun of the
official completion hook or Windows's 13 original Cargo steps.
