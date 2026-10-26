# cleanup-8h

The historical task name `CC-CleanupDeletionRequests-8h` is retained, but it runs **hourly**. Production roots are compiled in: `C:\Users\makhmeto\Downloads\削除希望` and `D:\削除希望`. No option expands those roots. No arguments and `--dry-run` inspect only; the task passes `--apply`.

The previous automatic scans of workspaces, desktop development trees and temporary receipt trees were removed after the unexplained source/settings loss. This replacement never scans or deletes those trees, and never prunes Docker or AI models.

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
