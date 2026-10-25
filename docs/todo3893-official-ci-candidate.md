# ToDo3893: official public software purpose check

This branch is a finite synthetic check for parent ToDo3387. It is not a new
implementation owner, PR, distribution, runner registration, or CI supplier.
No live CLI entry point or general path classification is changed.

## Observed baseline

- Source `899700e15ba4953bb7cd8d6cb1cf4056a92020f0`, actual repository
  `AutoDevJapan/cc-pc-tools-rs`, GitHub ID `1406818713` (API read).
- `repair-guard/main_windows.go`: directory existence and a new safe-delete
  ownership receipt do not establish public software provenance.
- `owner_refresh_windows.go`: refresh is for a positively registered Git source,
  not an arbitrary new archive or directory.
- `ci3574_windows.go`: only the three original retirement roots, evidence bytes,
  receipt hashes, and physical identities qualify. It is not an SDK constructor.
- Installed help has no official software constructor. A call to the proposed
  `--construct-official-ci3893 synthetic-no-input` returned exit 2,
  `flag provided but not defined`; no input file was opened.
- ToDo3255 normal history API, offset 100, updated 2026-10-26 06:04 JST,
  supplies the existing source/distribution ownership. Its original broad scope
  for this check is superseded by the corrected ToDo3893 request. PR522 metadata
  independently confirms c745ecd8 merged as the source above. No other runtime
  or unclassified original evidence was read to infer an installed hash.

The candidate is `repair-guard/internal/officialci3893`. Only that new internal
package and a synthetic baseline test are added; existing production source,
help, classification, registered-source refresh and artifact qualifications
are unchanged. Parent ToDo3387 retains integration, the final separate-model
review, CI, merge, distribution, actual SDK acceptance, and resumption of the
existing CI worker. A successful check is not an SDK eligibility receipt.

Original registry recovery, original unknown data, credentials, old retirement
resources, and all existing parent scope/dependencies remain unchanged.

## Concrete candidate and trust boundaries

| Call/boundary | Candidate behavior | Parent's required live evidence |
| --- | --- | --- |
| `New(FixedBinding(...), Authority, ArchiveSource, Journal, NativeFS)` | Fixes the original task, repository, PR/head/tree, run/attempt/job, labels, official release/asset/URL/size/hash | Normal self-source registration and physical/source identity, actual GitHub self-owner/repository ID, canonical original ToDo/job and current legitimate 3888 actor. Source HEAD, source-proof digest, creation-receipt digest and current actor are trusted wiring, never request fields. |
| `Authority.Observe(ctx)` | Requires exact equality of that binding, current anchor identity, fresh verified creation, active verified job, official public asset, observation age 0–60 seconds | Independently obtain these facts from the normal APIs/registered source/creation issuer. The test's booleans are fixtures, not production evidence. Existing unknown roots must never be marked `FreshOwnedAnchor` by examining caller assertions. |
| `ArchiveSource.Open(ctx, fixedAsset)` | Size-bounds one read; hashes its bytes; passes the same bytes to ZIP parsing | Implement only the pinned official public download after metadata qualification. No live implementation/download was used here. Reader `Close` must unblock `Read`; cancellation closes it. |
| `Journal.Reserve/Load/Replace` | Pending receipt precedes physical creation; exact authenticated record comparison controls transitions | Reuse normal independently authenticated creation-receipt machinery with a purpose-specific domain, CreateNew, durable atomic updates, issuer/ACL/current physical identity and replay protection. Authenticate one byte buffer before decoding it. Do not implement a general registry or accept a request-supplied receipt/hash. |
| `NativeFS.CreateRoot/Install` | New random 128-bit child only. Atomic `NtCreateFile(FILE_CREATE)` returns the original directory handle relative to a pinned parent. Files use `CREATE_NEW`; all content is read back through the written handle. | Independently qualify a genuinely fresh, empty safe-delete-owned anchor and its current volume/file ID/birth. The old empty 3888 root is never restored/adopted. |
| `Resume/Consume` | Authenticate receipt; reauthorize purpose; verify exact tree, every directory/file identity and all bytes with pinned handles. Consumer gets only verified public byte snapshots, with no path grant. | Bind current normal actor lineage to the original 3888 task and the same immutable purpose. Do not reopen a pathname after verification. A real executable launcher and its dependency/registration lifecycle are a separate parent integration/acceptance step. |

The public source contains fixed nonsecret purpose metadata and synthetic inputs,
not private original prompt/queue/response bodies, original registry data or
credentials. This package has no default verifier, downloader, persistent store,
network client, subprocess launcher, administrative action or background worker.
Its native helpers are IO building blocks, not independent authorization APIs.
The only receipt authenticator executed by this check is a test-only HMAC store
over synthetic material. No actual persistent/ACL/DPAPI acceptance is claimed.

## Proposed CLI after parent integration

`ParseCall` and `Execute` are typed, executable proposals, exercised with fixtures.
They are not wired into the installed guard:

```text
--construct-official-ci3893
--resume-official-ci3893 <32 lowercase hex receipt ID>
--consume-official-ci3893 <32 lowercase hex receipt ID>
```

There is no `--root`, `--hash`, `--url`, repository/head/run override, receipt JSON,
or general `--guard-path` in this protocol. Extra/duplicate/unknown arguments fail.
Results have `purpose_verified`, `receipt_id`, `state`, and always false
`original_registry_recovered`, `access_allowed`, `launch_performed`. A purpose
result must not be converted into a general guard result by the parent.

Each operation has a two-minute context budget. Receipt lifetime is thirty
minutes from one creation-time sample. A restart of the engine can verify the
same completed authenticated receipt; it does not refresh the deadline. A
different actor/registration requires canonical, independently verified lineage
in the parent's adapter, not a caller-selected change to an old receipt.

`pending` and `consuming` receipts left by interruption are never adopted.
`complete -> consuming` uses an atomic whole-record compare before one callback;
the terminal states are `consumed` or `failed`. If the callback finishes but its
terminal receipt save fails, the result is an explicit error and the preserved
`consuming` state prevents replay. There is no automatic re-execution or claim of
rollback. Partial roots remain identified by the independent pending receipt
for ordinary owned cleanup; failure does not authorize reading old resources.

## Refusal and same-bytes boundaries

- Independent authority mismatch or stop fails before archive access or creation.
  Fixed self-repo/source proof, actor/ToDo, product repo/owner/head/tree,
  run/attempt/job/labels, anchor receipt/current identity, asset identity/hash/URL
  and fresh observation all have negative controls.
- One immutable archive buffer is both hash input and ZIP input. ZIP paths reject
  traversal, absolute/device/ADS names, backslashes, trailing dot/space, reserved
  devices, non-ASCII aliases, duplicate/case-colliding file/directory names,
  symlinks, encryption, CRC/size errors and file/directory collisions. Limits are
  16,000 entries and 1 GiB expanded data; no extraction to existing arbitrary data.
- Exact created root, directory and file identities include volume, file ID and
  birth time. Ancestors and consumed files remain held without write/delete
  sharing. Unknown/missing tree entries are rejected using bounded metadata
  enumeration before file-content reads. Every known file is then read once,
  hashed and delivered from that same buffer while the handles remain open.
- Actual synthetic Windows tests cover identical-byte file replacement, root
  replacement, directory replacement retaining the file identity, changed bytes,
  hardlinks, root/file ADS, and a junction entirely within the owned fixture.
  The byte consumer also attempts replacement while handles are held; writes
  and root rename fail, and the callback sees the original verified bytes.
- Tampered/copy-to-sibling receipts, changed binding/asset/physical identity,
  partial saves, stale/future/extended timestamps and replay all fail. A barrier
  test proves only one concurrent consumer can enter. A cancellation test proves
  the synthetic blocking archive reader is closed without issuing a receipt.

Atomic directory creation follows Microsoft's
[NtCreateFile contract](https://learn.microsoft.com/en-us/windows/win32/api/winternl/nf-winternl-ntcreatefile)
and [OBJECT_ATTRIBUTES](https://learn.microsoft.com/en-us/windows/win32/api/ntdef/ns-ntdef-_object_attributes).
Handle sharing and stream checks follow
[CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew),
[FindFirstStreamW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-findfirststreamw)
and [FindNextStreamW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-findnextstreamw).
ZIP parsing uses Go's [archive/zip](https://pkg.go.dev/archive/zip).

## Recorded test sequence

The actual source Red was `TestOfficialCI3893LegacyPurposeBoundary` temporarily
expecting the proposed constructor to exist in `parseGuardArgs`. It failed with
`flag provided but not defined: -construct-official-ci3893`, exit 1. The installed
CLI independently returned exit 2. Neither call consumed an input file. The final
baseline test preserves that legacy refusal; the new package's constructor and
protocol tests provide the separate synthetic Green. No fake live acceptance is
inferred from a deliberately failing scaffold.

During implementation, mixed slash spellings in the synthetic fixture anchor
failed the Windows path walk. Canonicalizing the already trusted anchor before
the walk corrected it. Manual reading also found and fixed a second clock sample
in receipt expiry and the Mkdir/Open directory-creation gap; both have executable
regressions. These are candidate corrections, not repairs deployed to the PC.

Tests use Go 1.27.0, Windows/amd64, `GOMAXPROCS=2`, `-p=2`; race uses the existing
UCRT64 GCC. All fixture files live in this job's one safe-delete-owned folder.
No Cargo, GPU or Civ6 resource was used. Final race/vet counts, cleanup outcome
and exact source/branch/HEAD are retained in the job's private resume receipt.

| Final check | Result |
| --- | --- |
| Candidate `go test -race -p=2 -json -count=1 ./internal/officialci3893` | 16 top-level tests + 85 subcases passed (101 pass events), elapsed 1.265 s; failures/skip/race warnings 0 |
| Existing guard `go test -race -p=2 -json -count=1 -run '^(TestOfficialCI3893\|TestCI3574)' .` | 4 top-level tests + 16 subcases passed (20 pass events), elapsed 1.052 s; failures/skip/race warnings 0 |
| `go vet -p=2 ./internal/officialci3893 .` | Exit 0 |

The second run covers the unchanged CLI/unknown-data boundary and the existing
three-root retirement proof contract using synthetic accessors. It does not
open the original retirement receipts. Compilation time is not included in the
reported Go package elapsed times. No full guard test suite, Cargo suite, release
build or formal CI run is claimed.

## Parent's remaining work (not completed by this check)

1. Review/adapt this fixed candidate in parent3387's source; implement the normal
   independent authority, creation receipt, durable authentication/CAS and official
   downloader adapters. Keep original registry recovery false and general
   classification/artifact contracts unchanged. Confirm installed guard/source
   correspondence through the normal 3255 distribution owner; only help and normal
   source/PR/note metadata were observed by this check, not a live runtime hash/ACL.
2. Complete the final separate-model review, required formal CI, integration/merge
   and normal distribution under parent ownership. This branch has no independent
   Issue/PR, merge, deployment or installed runtime update.
3. Accept the exact real official archive and SDK under that new normal constructor:
   verify actual layout, limits/memory, empty-directory needs, executable dependency
   handling, failure/expiry and crash recovery, live receipt storage/ACL and current
   physical identities. Synthetic tests do not establish actual SDK eligibility.
4. Only then use the parent's successful normal production-acceptance receipt to
   resume the same 3888 CI supplier for the fixed private run. This check neither
   queries nor changes the real queue, obtains registration tokens, configures or
   starts Runner.Listener, reruns/cancels CI, or marks the original whole task done.

All original 2191/3387/2467 work, holds, prohibitions and dependencies remain with
their original owners. No new task, AI child/reviewer, Discord message, owner
question, API key use or new spending is part of this check.

## Owned cleanup and resume handoff

The one safe-delete fixture ownership ID is
`a660f4360ee6f0dd489d62879d7ccd9f`. All tests and their native processes finished;
the fixture contains only this check's synthetic files/logs and three synthetic
junctions. No SDK, original data, or another actor's work is in it. Normal `plan`
and the prescribed `quarantine --reason windows` both returned exit 2,
`LINK_OR_REPARSE_POINT`, before a command/move was produced. This is the cleanup
helper's metadata refusal, not an automatic approval rejection or an OS denial.
No alternate delete path, registry exemption or guard alteration was attempted.

Metadata measurement without following those junctions found 241 ordinary file
names, 599 directories, and three reparse entries. Default stream logical bytes
are 261,917; including file and directory ADS gives **262,013 bytes**. Hardlink
names are counted separately; this is logical content size, not allocated disk
space or a promised recovery amount. Recovered bytes are zero. The original owned
folder remains in place; `finish` was not called because the source still exists.

Parent must retain this one cleanup hold with the original ownership receipt:
use the existing cleanup/source owner to obtain a normal identity-bound,
non-following plan for these self-created reparse entries, perform only the
resulting authorized owned cleanup, then finish that same receipt. Do not delete
or adopt another root, widen general guard policy, or ask the owner to clean it.
Owner judgment is not required. The parent quiet-delivers this new hold once as
part of the requested check result; no Discord message was sent by this check.

The first normal checkpoint call returned `TimeoutError`. Source inspection
locates this error at scheduler/todo lock acquisition; the response does not
identify which lock/holder caused it. No shared lock or raw store was opened or
changed. A later normal `--check` returned `eligible=true`, all eleven checks
true, no competing worker, and no launch. The final committed HEAD is passed to
the normal saving call again, with the initial branch kept unchanged. Exact
requests/results and the final outcome are in this worktree's private
`todo3893` resume receipt. No task status or permission is changed by this check.
