# cleanup-8h

This binary extends the existing `CC-CleanupDeletionRequests-8h` scheduled task. It empties recycle bins on C and D only, then scans `D:\CC\Tools\agent-workspaces`, `D:\削除希望`, `C:\Users\makhmeto\Downloads\削除希望`, and `C:\Users\makhmeto\Desktop\dev` for directories named `target`, `temp`, `tmp`, `.temp`, `.tmp`, `cache`, or `.cache`. At each root it descends only into first-level directories whose names identify a ToDo in `done` state; for either deletion-request root it can resolve the ToDo ID from the saved original-path line in `理由.txt`. For C's Temp directory it follows only `safe-delete` receipts in `owned` state, confirms each plan with `safe-delete.exe plan`, and requires a ToDo ID in the saved project/content metadata. It never enumerates the whole Temp directory. Unmapped `codex-owned-<uuid>` folders and unrelated project trees are skipped. E is measured but its files and recycle bin are left untouched.

An artifact is eligible only when its path identifies a ToDo in `done` state. The cleaner applies the client-resource guard before descending into each owned workspace. Before removal it also checks every path component for reparse points, Git status/upstream/tracked files for an ancestor repository, verification evidence files, external hard links, and open-file owners through Windows Restart Manager. Large trees are registered and checked in batches so a busy process in any batch blocks removal. It skips `.git`, `node_modules`, `.pnpm-store`, `.cargo`, and `vendor` subtrees. A failed check leaves that artifact in place and records a result category. The global sccache path is outside the scanned roots and remains under its configured 30 GB limit. E-drive contents are not scanned or deleted.

The scheduled task keeps its existing name, elevated S4U principal, triggers, and eight-hour schedule. Deployment preserves the former executable as `cleanup-8h-legacy.exe` for rollback; the replacement does not run it because its all-drive recycle-bin behavior would touch E. Each run appends C/D/E free-space snapshots, C/D recycle-bin method and counts, removed logical bytes, skip counts, and failure categories to `D:\CC\Tools\cleanup-8h\owned-target-runs.jsonl`.

Build and focused verification:

```powershell
cargo nextest run --locked -p cleanup-8h
cargo build --locked -p cleanup-8h --release
```
