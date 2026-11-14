# Fixed installed development tools

The normal classifier delegates only the following canonical executable paths
to a dedicated provider. This change does not declare the containing directories
or adjacent configuration, credentials, source trees or other executables clear.

| Tool | Fixed installation | Public release |
| --- | --- | --- |
| Git | `C:/Program Files/Git/cmd/git.exe` | Git for Windows 2.54.0.windows.1 |
| GitHub CLI | `C:/Program Files/GitHub CLI/gh.exe` | GitHub CLI 2.101.0 |
| Go | `C:/Program Files/Go/bin/go.exe` | Go 1.27.0, Windows amd64 |

The intended use is ordinary development of the existing owner repositories.
The provider performs no command, update, login, purchase or signing operation.
A clear result is a classification snapshot of one public executable, not a
proof that all DLLs, compiler subprocesses, standard libraries or credentials
used by that command have been admitted. Original operation permissions and
previous operation-specific refusals remain independent conditions.

## Source of the fixed public bytes

The three executable hashes are compiled into `owner_installed_tools_windows.go`.
On 2026-11-14 the corresponding complete public archives were obtained over
HTTPS, hashed, and the exact ZIP entry was hashed from those authenticated
archive bytes. Installed executable contents were not used to manufacture the
expected digest. No vendor signature verification is claimed.

| Archive | SHA-256 | ZIP entry | Executable SHA-256 |
| --- | --- | --- | --- |
| [MinGit 2.54.0 64-bit](https://github.com/git-for-windows/git/releases/tag/v2.54.0.windows.1) | `04f937e1f0918b17b9be6f2294cb2bb66e96e1d9832d1c298e2de088a1d0e668` | `cmd/git.exe` | `81ef35ae005ca9318018d18e3327578ce939fb99feaad6b2d7c8ab15f3de8db5` |
| [GitHub CLI 2.101.0](https://github.com/cli/cli/releases/tag/v2.101.0) | `bc6c814367b193cd8e713611d61e36013c0ef843b8f516458fe3eda039192794` | `bin/gh.exe` | `df05089117a23889b3f6ea08d7644849afe7b6025647a09f33b45c71f1418f75` |
| [Go 1.27.0 Windows amd64](https://go.dev/dl/) | `f0c0a0d33ba94f4d2c5dbc887334ce678b21813504ddb3aafcb06e60a5a667c4` | `go/bin/go.exe` | `7d828191ba32519a9c9361789ab647486236ed45c660889196c7770a8ff1985c` |

Changing a version or expected hash requires a reviewed source change. There is
no caller-supplied path, publisher, receipt, digest, JSON plan or general alias
exception. The existing uninstall registration must also report the exact
version and publisher. That registration supplies installation context only;
the public-byte comparison supplies the independent content check.

## Validation and handle ownership

Legacy client exclusions, unresolved exclusions and suspected owner-card paths
remain ahead of this provider. The provider checks every declared alias against
those exclusions again. Ordinary files retain the existing single-link policy.

Only Git's observed pair `cmd/git.exe` and `cmd/git-lfs.exe` is admitted as one
object for this canonical operation. The secondary name does not acquire its
own general classification grant. Both names must be the entire hardlink set,
must have the same volume/file ID/creation identity and ACL, and must remain
pinned with read sharing only. Other tools must each have exactly one name.
No reparse-point ancestor or alternate final path is accepted.

Every retained directory in the installation's parent chain also has its
identity, final name, owner and DACL checked from that same handle. Its proof
is rechecked together with the leaf before returning. Effective add-file,
add-directory, delete-child, delete and ACL/owner-change rights for untrusted
principals prevent a clear result. An `INHERIT_ONLY` ACE is not effective on
the current object; each actual descendant and executable descriptor is checked
separately, so an effective inherited write on the leaf still fails. Unknown
descriptor forms remain unknown.

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
controller tests, vet and build. Those results apply to those inputs only.
The current Open/Read cancellation correction has additional test source and
requires its own fixed-input hosted verification. The local
Git, Go and gofmt executables have not been used to manufacture a successful
local result. No actual installed-executable qualification, update or normal
consumer acceptance is claimed by either source-test result. Public archive
verification remains separate from the unperformed installed-byte comparison.

The candidate must receive independent review and a Windows build/test through
an already qualified normal producer. The installed guard must then be updated
through its existing purpose-checked software-update operation, followed by
same-installed-object identity/hash/build information and normal consumer
readback. A hosted compile without a retained output and provenance is not a
distributed helper. If no such producer or qualified update input is presently
available, that concrete dependency remains unresolved; a different executable
path or another actor must not run a denied tool to manufacture the evidence.
