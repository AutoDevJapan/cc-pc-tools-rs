# cleanup-8h

The historical task name `CC-CleanupDeletionRequests-8h` is retained, but it runs **hourly**. Production roots are compiled in: `C:\Users\makhmeto\Downloads\削除希望` and `D:\削除希望`. No option expands those roots. No arguments and `--dry-run` inspect only; the task passes `--apply`.

The previous broad scans of workspaces, desktop development trees and temporary receipt trees were removed after the unexplained source/settings loss. A separate finite entry now considers only registered completed PC-tool worktrees' ignored `target/debug` artifacts, as described below. It never prunes Docker or AI models.

Physical ancestors are pinned; reparse points and mismatched physical names are rejected. Source, repository markers, verification records and recovery material including ToDo 2467 are retained. Unknown resources use the real scrub391 guard. Unavailable protection or `classification_unverified` retains a candidate before descending. The original registry is not synthesized or replaced with an empty list.

The cleaner enumerates a pinned directory, upgrades each object to an exclusive delete handle and checks file identity again. Open files, external hard links and changed identities are retained. Deletion uses `SetFileInformationByHandle`, rather than path-based recursive removal. Counts include only successful deletions. The root cannot be deleted. Limits are 200,000 entries and eight minutes; an OS file lock and `IgnoreNew` prevent overlapping runs.

Recycle cleanup covers the current owner's C/D stores. Each $I record is checked in memory. Missing or unsupported metadata, source/recovery origins and unclassified resources are retained. $R data is deleted before its $I record. No whole-bin shell purge is used. E is measured only.

Release binaries use the Windows GUI subsystem. The guard uses CREATE_NO_WINDOW, a ten-second deadline and bounded output capture. Aggregate receipts in `D:\CC\Tools\cleanup-8h\owned-target-runs.jsonl` contain counts, logical removed bytes, actual free-space snapshots and net changes. Private names, original recycle paths, task bodies and secrets are absent. Concurrent writers can affect the net changes.

The existing task uses elevated S4U logon, hourly repetition, StartWhenAvailable, IgnoreNew, a ten-minute limit and two bounded retries. Deployment and prior task XML are in deploy-2491/. The parent integrates source/review/CI/merge.

Focused checks: `cargo nextest run --locked -p cleanup-8h`, `cargo clippy --locked -p cleanup-8h --all-targets -- -D warnings`, and `cargo build --release --locked -p cleanup-8h`. Tests exercise real Windows deletion, open-file failure, junctions, pinned-root rename, hard links, source adjacency, expired deadlines, run locks, root boundaries and recycle metadata.

Owned safe-delete quarantine wrappers use the same hourly run and lock. The fixed
Go producer verifies original create/release/move outputs, current ToDo and source
preservation, ordinary protection and physical identity. A finite child holds the
original ID lock while Rust validates its entire metadata plan and deletes only
the `files` payload with the existing exclusive handles. Rust then requests finish
and checks the matching ID and canonical `quarantine_removed` result. The wrapper
and original evidence remain. Qualification failures, incomplete deletion and
finish failures are distinct; a failed owned wrapper never falls through to generic
deletion. Legacy wrappers lacking provenance are retained. Candidate logical bytes,
actual removed bytes, unmeasured retained candidates and net Windows free space are
recorded separately. No new task, root or guard exception is introduced.

The Windows integration test compiles the Go test producer and exercises the actual
Go/Rust process protocol with synthetic canonical records and disposable data.
It also checks canonical finish and evidence retention. Go is required for this
affected test; the existing Windows CI already provisions it.

Completed-build reclamation (Issue 545 / ToDo3942) reads the canonical ToDo and job
APIs. Only project `cc-pc-tools-rs`, status `done`, the recorded successful final
job and its exact registered Git worktree qualify. Current remote ownership,
common directory, branch, saved/pushed source and ignored/untracked debug data are
checked. A waiting/live sibling, dependent unfinished work or live resource marker
retains that target. No arbitrary path or ownership registration is accepted.
The existing queue's active-ID snapshot is read under queue.lock. Current canonical
job metadata is inspected with a deadline, including queued siblings and terminal
jobs whose supervisors remain alive, even when the admission index omits them.
Published unknown IDs are preserved. Jobs/index remain unchanged; no prompt or
event history is read.
An unavailable or incomplete observation retains the candidate. Explicit saved
debug-build references in canonical metadata also retain their original artifacts.

Existing queue/state/ToDo byte locks, the exact shared Cargo target lock and Cargo's
own profile locks are held from qualification through deletion. Ancestors and all
artifact files are pinned. Every file must acquire exclusive deletion access
before the first removal, so a running executable or open dependency retains the
whole set. Reparse points, external hardlinks, source/evidence and unknown top-level content
are retained. Generated source under Cargo build-script OUT_DIR remains rebuildable.
Cargo metadata supplies real target names. Hashed dependency outputs must match
the unit name and hash of Cargo fingerprint markers plus their valid JSON metadata.
Only known Cargo output forms are accepted; an arbitrary file in deps or another
unknown child retains the whole set. Final absence also requires that re-enumerating
the root finds only pinned profile locks. Changed/partial contents have unmeasured
remaining bytes, rather than subtracting from an obsolete plan.
Fingerprint directories and marker names are bound individually to real Cargo
package/target metadata. Incremental sessions use known rustc names; build OUT_DIR
also requires Cargo's matching root-output record. A profile lock is allowed only
as the same pinned regular zero-length file, never just a matching basename.
Hardlinks are accepted only when every physical link is inside this complete
qualified plan. A unique exclusive handle protects each object before deletion;
all aliases are rechecked, and bytes count once when the final link is removed.
Existing deletion-request and retirement entry points still reject hardlinks.
The profile directory and its lock files stay in place to prevent a direct Cargo
start during reclamation; receipts report artifact absence separately from that
scaffolding. Source, Git, release and original records are never deletion targets.

The new stage runs after the older independent stages, for at most 90 seconds,
512 canonical rows and four measured artifact sets per hourly invocation. It uses
a numeric scheduling cursor only in apply mode; the cursor never grants authority.
An unreadable/corrupt cursor is preserved and reported. A separate numeric recovery
position advances the same finite pass without replacing that original.
Default/dry-run changes no target, cursor or durable receipt. Schema 3 adds anonymous
candidate/removed/retained/unmeasured/partial/absence statistics. Empty or previously
removed artifacts are not reclaimed twice. No Discord sender, new task, ownership
exception or whole-PC completion claim is added.
