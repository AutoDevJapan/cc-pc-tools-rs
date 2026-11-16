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

The intended use is ordinary development of the existing owner repositories.
The provider performs no command, update, login, purchase or signing operation.
A clear result is a classification snapshot of one public executable, not a
proof that all DLLs, compiler subprocesses, standard libraries or credentials
used by that command have been admitted. Original operation permissions and
previous operation-specific refusals remain independent conditions.

## Source of the fixed public bytes

The five executable hashes are compiled into `owner_installed_tools_windows.go`.
On 2026-11-14 the corresponding complete public archives were obtained over
HTTPS, hashed, and the exact ZIP entry was hashed from those authenticated
archive bytes. Installed executable contents were not used to manufacture the
expected digest. No vendor signature verification is claimed.

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

Changing a version or expected hash requires a reviewed source change. There is
no caller-supplied path, publisher, receipt, digest, JSON plan or general alias
exception. The existing uninstall registration must also report the exact
version and publisher for the machine-installed tools; the rustup row uses the
fixed current-user `DisplayName` context described above. That registration
supplies installation context only; the public-byte comparison supplies the
independent content check.

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
prevents a clear result. The two-second context bounds controller loops but
cannot forcibly interrupt a synchronous Windows kernel call. Returning clear
does not keep handles across a later caller's process launch and does not claim
an all-time filesystem transaction.

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
hardlink inventory change has additional controller test source and requires
its own fixed-input hosted verification and independent review. No local Go
compile, test, vet, build or actual installed-tool acceptance was performed for
that change. The owned-file native enumeration/held-identity test is also only
prepared source until the new fixed candidate runs. Public archive verification and synthetic or isolated-registry
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

Actual current executable acceptance, normal producer/distribution and
same-installed-object readback remain incomplete. Representative use from both
Codex and Claude Code is also required and has not been demonstrated here.
Neither source tests nor a hosted build grants installed-tool or downstream
product acceptance.
