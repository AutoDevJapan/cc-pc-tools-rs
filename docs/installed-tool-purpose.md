# Fixed installed development tools

The normal classifier delegates only the following canonical executable paths
to a dedicated provider. This change does not declare the containing directories
or adjacent configuration, credentials, source trees or other executables clear.

| Tool | Fixed installation | Public release |
| --- | --- | --- |
| Git | `C:/Program Files/Git/cmd/git.exe` | Git for Windows 2.54.0.windows.1 |
| GitHub CLI | `C:/Program Files/GitHub CLI/gh.exe` | GitHub CLI 2.101.0 |
| Go | `C:/Program Files/Go/bin/go.exe` | Go 1.27.0, Windows amd64 |
| Go formatter | `C:/Program Files/Go/bin/gofmt.exe` | Go 1.27.0, Windows amd64 |
| rustup | `C:/Users/makhmeto/.cargo/bin/rustup.exe` | rustup 1.29.3, Windows x86_64 MSVC |
| Node.js | `C:/Users/makhmeto/AppData/Roaming/fnm/node-versions/v22.21.0/installation/node.exe` | Node.js 22.21.0, Windows x64 |
| Claude Code | `C:/Users/makhmeto/.local/bin/claude.exe` | Claude Code 2.1.310 through 2.1.329, Windows x64 (eighteen releases) |

The intended use is ordinary development of the existing owner repositories.
The provider performs no command, update, login, purchase or signing operation.
A clear result is a classification snapshot of one public executable, not a
proof that all DLLs, compiler subprocesses, standard libraries or credentials
used by that command have been admitted. Original operation permissions and
previous operation-specific refusals remain independent conditions.

## Source of the fixed public bytes

The hashes of the first five rows are compiled into `owner_installed_tools_windows.go`.
On 2026-11-14 the corresponding complete public archives were obtained over
HTTPS, hashed, and the exact ZIP entry was hashed from those authenticated
archive bytes. Installed executable contents were not used to manufacture the
expected digest. No vendor signature verification is claimed for these five
rows; the two rows without an installer registration are described in their own
section below.

| Archive | SHA-256 | ZIP entry | Executable SHA-256 |
| --- | --- | --- | --- |
| [MinGit 2.54.0 64-bit](https://github.com/git-for-windows/git/releases/tag/v2.54.0.windows.1) | `04f937e1f0918b17b9be6f2294cb2bb66e96e1d9832d1c298e2de088a1d0e668` | `cmd/git.exe` | `81ef35ae005ca9318018d18e3327578ce939fb99feaad6b2d7c8ab15f3de8db5` |
| [GitHub CLI 2.101.0](https://github.com/cli/cli/releases/tag/v2.101.0) | `bc6c814367b193cd8e713611d61e36013c0ef843b8f516458fe3eda039192794` | `bin/gh.exe` | `df05089117a23889b3f6ea08d7644849afe7b6025647a09f33b45c71f1418f75` |
| [Go 1.27.0 Windows amd64](https://go.dev/dl/) | `f0c0a0d33ba94f4d2c5dbc887334ce678b21813504ddb3aafcb06e60a5a667c4` | `go/bin/go.exe` | `7d828191ba32519a9c9361789ab647486236ed45c660889196c7770a8ff1985c` |
| [Go 1.27.0 Windows amd64](https://go.dev/dl/) | `f0c0a0d33ba94f4d2c5dbc887334ce678b21813504ddb3aafcb06e60a5a667c4` | `go/bin/gofmt.exe` | `e7252077f308fe8344091a168b24cb56d1366e7d8f5c64c2086ce17d262e3eef` |
| [rustup-init.exe 1.29.3](https://static.rust-lang.org/rustup/archive/1.29.3/x86_64-pc-windows-msvc/rustup-init.exe) | `38027d51d69a5d05493627a0634dbea52d6cd793c8f072d2373d3f4163eec7db` | the file itself | `38027d51d69a5d05493627a0634dbea52d6cd793c8f072d2373d3f4163eec7db` |

The `gofmt.exe` pin comes from the same Go 1.27.0 archive as `go.exe`. The
archive was downloaded again over HTTPS on 2026-11-16 and equals both the digest
published by the vendor and the digest pinned for `go.exe`; the same ZIP reader
reproduced the pinned `go.exe` entry digest before it hashed the `gofmt.exe`
entry. Installed executables were not read to produce either value.

The `rustup.exe` pin is the vendor's published setup file for rustup 1.29.3
(12,839,424 bytes), downloaded over HTTPS on 2026-11-16. Its digest equals the
`.sha256` file that the vendor publishes beside it. rustup copies the setup file
to `rustup.exe` on installation, and its self update downloads exactly this
archive path before replacing the binary, so the installed file is expected to
be byte-identical; the provider decides only by comparing the held bytes.
The official Windows installer records `DisplayName` and `DisplayVersion`,
and its normal self-update updates `DisplayVersion`; it does not record a
publisher. This provider deliberately uses only the exact `DisplayName` in
`Software\Microsoft\Windows\CurrentVersion\Uninstall\Rustup` under the
current-user hive as installation context. The held-file public pin determines
the admitted release, independently of the registry's version string. A later
rustup release changes those bytes and remains unknown until a reviewed source
change pins the new release. Proxy paths receive no classification grant; the
closed same-object inventory below only supports verification of canonical
`rustup.exe`.

The per-user rustup row qualifies only while the guard runs as the account that
owns that profile, because its installation context is read from that account's
current-user hive and its token SID is the trusted user; under any other
account, such as a service or a broker, the origin or owner check fails closed
and the row stays unknown.

Changing a version or expected hash requires a reviewed source change. There is
no caller-supplied path, publisher, receipt, digest, JSON plan or general alias
exception. The existing uninstall registration must also report the exact
version and publisher for the machine-installed tools; the rustup row uses the
fixed current-user `DisplayName` context described above. That registration
supplies installation context only; the public-byte comparison supplies the
independent content check.

## Rows without an installer registration

Claude Code's native installer and an fnm-extracted Node.js archive write no
uninstall registration, so the registry observations of the rows above have
nothing to read for them. These two rows are marked `noRegistration` in the
compiled table. Such a plan skips only the two registry observations. Every
other control in this document is unchanged: the exclusions, the closed alias
inventory, the read-sharing opens, the identity, size, link-count, ACL and
parent-chain proof, the single bounded read hashed against the public pin, and
the rechecks after the read. The content authority is the held-byte comparison
with the vendor pin, as for every other row; registry text was never authority.

A plan is refused before any object is opened unless it has one of two compiled
shapes. A registered plan declares no budget. A registration-free plan names no
registry value, publisher or user hive, and it declares a time budget of at most
one minute. Either shape needs a positive content size within the single
compiled read bound of 512 MiB; the largest pinned release is about 384 MB.

| Executable | Fixed installation | Public pin |
| --- | --- | --- |
| Node.js 22.21.0 | `C:/Users/makhmeto/AppData/Roaming/fnm/node-versions/v22.21.0/installation/node.exe` | [`node-v22.21.0-win-x64.zip`](https://nodejs.org/dist/v22.21.0/node-v22.21.0-win-x64.zip), SHA-256 `84d31df6571c3c7156707265bf51e2a021656fa1584bf6f4486b792dcde54d7d`; ZIP entry `node-v22.21.0-win-x64/node.exe`, SHA-256 `47cd67453da180a39cdf5daedb89862667b8e627e60c116607874633450cd30d`, 85,766,656 bytes |
| Claude Code 2.1.310 through 2.1.329 | `C:/Users/makhmeto/.local/bin/claude.exe` | the `win32-x64` entry of each signed release manifest, for example [2.1.329](https://downloads.claude.ai/claude-code-releases/2.1.329/manifest.json); eighteen releases |

Node.js. The `SHASUMS256.txt` of release v22.21.0 lists the archive and the
standalone `win-x64/node.exe`. On 2026-11-20 its detached Ed25519 signature was
verified with an independent OpenPGP verifier against the release key
`5BE8A3F6C8A5C01D106C0AD820B1A390B168D356`; the key file comes from the
`nodejs/release-keys` repository and the fingerprint is listed in the Node.js
README. One flipped byte of the list fails that check. The archive was downloaded
over HTTPS and equals the listed digest, and the `node.exe` entry streamed from
it equals the listed value.

Claude Code. Every release publishes `manifest.json` with a detached OpenPGP
signature `manifest.json.sig` made with the Claude Code release signing key whose
primary fingerprint is `31ddde24ddfab679f42d7bd2baa929ff1a7ecace`, the
fingerprint printed on the vendor's [setup page](https://code.claude.com/docs/en/setup).
On 2026-11-20 the manifests of the eighteen releases 2.1.310 through 2.1.329
were verified against that key with the same independent verifier; one flipped
manifest byte fails. Releases 2.1.313 and 2.1.320 have no manifest under that
prefix. For each release the compiled `sha` and `size` are the manifest's
`win32-x64` checksum and size, and `archiveSHA` is the SHA-256 of the signed
manifest file. The vendor binary of 2.1.329 was also downloaded over HTTPS and
streamed through SHA-256 without being stored: its digest equals the signed
checksum and its length is the signed size of 384,179,872 bytes. The installed
`claude.exe` was not read to produce any value.

Several releases of one path. The vendor published 28 signed Claude Code
releases between 2026-10-14 and 2026-11-19 and the native installer updates
itself, so the path carries one plan per pinned release, in table order. Each
plan is judged by the complete controller and the first clear result wins. A
protected exclusion, a cancellation or a failed cleanup does not depend on the
release and ends the search at once. Any other refusal is ranked by how far the
held object got: a release whose size matched and whose bytes did not is
reported as `installed_tool_bytes_mismatch`, ahead of the size mismatches
(`installed_tool_identity_mismatch`) of the other releases. A size that matches
no release is refused before any byte is read, so the extra plans cost only
handle opens. One deadline covers the path: two seconds for registered rows,
otherwise the largest budget its plans declare (30 seconds for Claude Code, 20
seconds for Node.js). The routing predicate of the policy only asks whether the
path is declared, which does not depend on how many releases are pinned.

Reading a refusal for these rows. `installed_tool_identity_unavailable` means the
file or one of its ancestors could not be opened, because it is absent, denied
or in use. `installed_tool_alias_mismatch` means the file has a hardlink name that the
plan does not list. `installed_tool_parent_untrusted` means an ancestor descriptor
grants replacement rights to another principal. `installed_tool_identity_mismatch`
means the size, link count, descriptor or final name of the held file differs
from the plan; a release outside the table is the usual cause, a descriptor that
the ACL rule rejects is another. `installed_tool_bytes_mismatch` means the size
equals a pinned release and the digest does not.

A release outside the table, such as a Claude Code release newer than 2.1.329,
stays unknown until a reviewed source change pins it. Nothing in this change
disables the vendor's updater or alters any setting of the owner. Both rows lie
in the profile of the account that runs the guard, so, as for rustup, a clear
result attests the held bytes at the time of the check only.

Codex. No Codex row is declared. The Microsoft Store package keeps its version in
its installation path and changes it with every update, so an exact-path row would
go stale; no vendor digest of the packaged executable is pinned here, and a digest
taken from the installed file would be the self-attestation these rows exclude. A
row would need a path rule by package family or an origin check that understands
the package registration, which is a separate capability and decision.

## Validation and handle ownership

Legacy client exclusions, unresolved exclusions and suspected owner-card paths
remain ahead of this provider. The provider checks every declared alias against
those exclusions again. Ordinary files retain the existing single-link policy.

Git's observed pair `cmd/git.exe` and `cmd/git-lfs.exe` is admitted as one
object for its canonical operation. The secondary name does not acquire its own
general classification grant, and the pair must be the entire hardlink set.
The GitHub CLI, Go and Go formatter objects must each have exactly one name.

The [official rustup 1.29.3 proxy lists](https://github.com/rust-lang/rustup/blob/1.29.3/src/lib.rs)
and [installation procedure](https://github.com/rust-lang/rustup/blob/1.29.3/src/cli/self_update.rs)
allow proxies to be symbolic links or, when necessary, hardlinks to canonical
`rustup.exe`. The fixed same-object inventory consists of that canonical name
and these thirteen optional names in its fixed installation directory:
`rustc.exe`, `rustdoc.exe`, `cargo.exe`, `rust-lldb.exe`, `rust-gdb.exe`,
`rust-gdbgui.exe`, `rls.exe`, `cargo-clippy.exe`, `clippy-driver.exe`,
`cargo-miri.exe`, `rust-analyzer.exe`, `rustfmt.exe` and `cargo-fmt.exe`.
A singleton, a subset of these proxies or the complete fourteen-name hardlink
set can be observed. A symbolic link or unrelated existing proxy is not assumed
to be a hardlink and is not opened by this inventory. Every name actually
returned by the native hardlink enumeration must belong to the compiled set,
the canonical name must be present, and no other alias is permitted.

Each observed alias must pass the existing exclusions and be held with read
sharing only before content is read. All held aliases must have the same
volume/file ID/creation identity, ACL and trusted parent-chain proof; their link
count must equal the complete observed inventory. The exact selected set must
still match after the read, even if a changed set would also fit the permitted
proxy names. Existing exclusions are checked again, and the final held-object
state is rechecked after installation metadata. The fourteen-name bound follows
the fixed release's complete set rather than a caller-selected limit. None of
these optional proxy paths can select the rustup classification plan itself.
No reparse-point ancestor or alternate final path is accepted.

Every retained directory in the installation's parent chain also has its
identity, final name, owner and DACL checked from that same handle. Its proof
is rechecked together with the leaf before returning. For the directory that
holds the executable, effective add-file, add-directory, delete-child, delete
and ACL/owner-change rights for untrusted principals prevent a clear result,
because a neighboring file could be loaded with the executable. For every
higher ancestor only the rights that could remove, rename or re-own an existing
component do: delete-child, delete, ACL or owner change, and generic write or
all. Add-only entries, such as the Authenticated Users add-subdirectory entry of
a stock system volume root or the add-file entry of a data volume root, cannot
replace an existing component and are accepted there. Delete on a volume root
cannot remove the root and is not counted. An `INHERIT_ONLY` ACE is not effective on
the current object; each actual descendant and executable descriptor is checked
separately, so an effective inherited write on the leaf still fails. Unknown
descriptor forms remain unknown.

Access masks are read from the descriptor text. Windows spells a mask with a
two-letter name only when its bits match one exactly, and it uses the Directory
Service names for single file-system bits: `CC` for read-data, `LC` for
add-subdirectory and `DT` for delete-child. Stock volume roots and per-user
tool trees contain such names. The nine Directory Service names (`CC`, `DC`,
`LC`, `SW`, `RP`, `WP`, `DT`, `LO`, `CR`) and `FW` are recognized and keep their
bit values as file-system rights, so `LC` in an allow entry for an untrusted
principal is an add-subdirectory right. Any other name, in an allow or a deny
entry, keeps the descriptor unknown.

The controller verifies the installation registration, opens all fixed names,
then reads a single bounded buffer from a held object. That exact buffer is
hashed against the public pin. The controller rechecks full identities, size,
link counts, final names, ACLs, alias inventory and installation metadata before
returning. The owner may be SYSTEM, Administrators, the current user or
TrustedInstaller; write-capable allow entries for other principals are rejected.
Unknown ACL forms fail closed. No ACL or executable is changed.

All opened files and ancestor handles are closed once, including failure paths;
all retained content buffers are cleared. A close failure or cancellation
prevents a clear result. The context of a classification (two seconds for the registered rows, the
largest declared budget of the path otherwise) bounds controller loops but
cannot forcibly interrupt a synchronous Windows kernel call. Returning clear
does not keep handles across a later caller's process launch and does not claim
an all-time filesystem transaction.

For the per-user rustup row a clear result attests the held bytes at the time of
the check only: any process of the profile owner account can replace the
executable or the user-owned directories above it, so the descriptor rules give
no later-integrity argument there as they do for the machine-installed rows.

Registry, hardlink-search, token and file/parent-handle closes are accounted for
separately from the operation which used them. Internal wrapped errors retain
their primary/cancellation and cleanup markers through `errors.Is`; no native
message leaves the provider. The final fixed classification basis preserves
the controller's refusal and adds `_cancelled` and/or `_cleanup_failed` when
both facts occur. For example, a content mismatch followed by cancellation and
failed cleanup is `installed_tool_bytes_mismatch_cancelled_cleanup_failed`.
No combination can become clear. A read interrupted solely by cancellation
returns the cancellation basis without inventing a content mismatch. Open
cancellation likewise does not invent a failed identity observation. Actual
read failures use `installed_tool_read_failed`; a complete-buffer digest
mismatch retains its own basis even when cancellation also occurs.
Normal classification, diagnostics and
`--classification-diagnostic=false` share the same installed-tool callback set.

## Verification status and rollout

PR733 `4df6e610151c1d3c6465fb49aeb2146162aec6a4` and the subsequent
`8126e6d16275395be837fe56ee4ae4d41ce27119` passed their hosted Windows
controller tests, vet and build. Those historical results apply to those inputs
only. On 2026-11-16, fixed source
[557537a12a4c85be5a14a85b29d32c2a3e1122a9](https://github.com/AutoDevJapan/cc-pc-tools-rs/commit/557537a12a4c85be5a14a85b29d32c2a3e1122a9)
passed its [Windows verifier job](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/43877356543/job/133333761017),
including seventeen exact returned-REG_SZ helper cases and eight isolated
volatile-HKCU cases through native Origin, with no failures or skips in those
cases, followed by vet and build. Its [readiness workflow](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/43877356543)
completed with ten successful jobs and one conditional bootstrap job skipped;
its [CodeQL workflow](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/43877356532)
completed with four successful jobs. A skipped job is not a successful execution.

Those hosted results cover that fixed predecessor, including its strict
registry comparison and descriptor-role rules. The subsequent closed rustup
hardlink inventory first ran in source
[3eecbc740791ad23bef70383df5b79fd0d959628](https://github.com/AutoDevJapan/cc-pc-tools-rs/commit/3eecbc740791ad23bef70383df5b79fd0d959628)
on 2026-11-16. That [Windows verifier job](https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/43890157033/job/133378856560)
compiled and passed seven of the eight new roots, but the native owned-hardlink
inventory test failed during name selection. Its phase and physical temp-path
spelling were not printed, so the exact historical cause is not established;
vet and build after that test were not reached. Those seven passes do not make
the native test or whole candidate successful.

The fixture correction resolves only its own temporary directory, proves that
the original and resolved names refer to the same object, and records fixed
phase/count/boolean diagnostics without printing paths. Production path and
alias rules remain unchanged. A separate owned long/short-name control and the
new fixed-source verification remain unexecuted until the next candidate runs.
No local Go compile, test, vet, build or installed-tool acceptance was performed
for these changes. Public archive verification and synthetic or isolated-registry
tests remain separate from the unperformed installed-byte comparison.

The candidate must receive independent review and a Windows build/test through
an already qualified normal producer. The installed guard must then be updated
through its existing purpose-checked software-update operation, followed by
same-installed-object identity/hash/build information and normal consumer
readback. A hosted compile without a retained output and provenance is not a
distributed helper. If no such producer or qualified update input is presently
available, that concrete dependency remains unresolved; a different executable
path or another actor must not run a denied tool to manufacture the evidence.

Structural controller tests keep the Go rows on one registration and archive
and the rustup row on one per-user installation. Prepared hardlink controls
cover singleton, partial and complete official inventories; unknown names;
changed sets; different identities, descriptors and parent proofs; exclusions
before opening and after reading; cancellation and cleanup failure; and refusal
to classify a proxy path as canonical rustup. The existing exact-REG_SZ tests
and ancestor-role tests remain in the source. These prepared controls do not
claim that the current PC has any particular hardlink layout or trusted ACL.

The registration-free rows, the selection among several pinned releases of one
path, the per-path time budget and the larger read bound are prepared source and
tests. Their author did not compile or run them: no Go compile, test, vet or
build was performed, so their first compile and test is the hosted Windows
verifier of the commit that adds them. The pins were checked offline against
vendor-signed documents as described above, but no installed `claude.exe` or
`node.exe` was read, hashed or started. Unknown remain whether the installed files
equal a pinned release, whether the installed `claude.exe` has other hardlinks (an
extra name makes the closed inventory refuse it), and which Node.js executable
each host starts for its tool children.

Actual current executable acceptance, normal producer/distribution and
same-installed-object readback remain incomplete. Representative use from both
Codex and Claude Code is also required and has not been demonstrated here.
Neither source tests nor a hosted build grants installed-tool or downstream
product acceptance.
