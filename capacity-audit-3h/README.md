# capacity-audit-3h

This Go tool restores the existing `CC-CapacityAudit-3h` task. It **does not delete files**. The task runs every three hours without a console and uses elevated S4U logon, `StartWhenAvailable`, `IgnoreNew` and a five-minute execution limit.

Each run measures actual C/D volume free and used bytes and the change since the previous run. E is measured only to report whether its required 500 GiB margin remains; its directories are not scanned. The tool also measures the logical file lengths of the known pagefile, hibernation file, swapfile and Docker virtual disk without opening their contents.

Bounded metadata scans cover the known Windows/application directories. Owner data, machine data, operations and deletion-request trees require a verified negative result from the real client-resource guard. When the original protection settings are unavailable, these categories are recorded as `protected_unclassified`, not as zero size. No private path, file name, task text or raw error goes to stdout or the aggregate run log.

Each category has a 25-second / 150,000-entry budget and measurement has a two-minute budget. A checkpoint preserves partial directory traversal between runs; the starting category rotates. The checkpoint cannot expand the compiled roots. Reparse points and paths with linked ancestors are skipped. An unavailable path, read error, partial scan, completed metadata scan and whole-drive scan are distinct: `whole_drive_scan` is always false. Reported category sizes are logical lengths, not disk allocation, and may include Windows hard links. A completed metadata traversal is not an atomic filesystem snapshot.

After persisting measurements, the existing task gives its newly generated anonymous report and compiled finite instruction to the canonical `todo capacity-cycle` command, with a separate 45-second deadline. The ToDo lock admits one reusable decision row; the ordinary scheduler alone launches it. Another measurement retains any unfinished, stopped or waiting worker. A new cycle after completion archives its result and dispatch history and starts a fresh dispatch in the same workspace. It never resumes a completed cycle's conversation or inherits its retry counters. No additional Windows task, monitor or direct AI launcher is created.

`todo capacity-status` reads only this connection's anonymous active/latest measurement and result, without opening old scan logs or checkpoints. A missing decision result is null. The worker records investigated resources, uninvestigated categories, candidates, retention reasons, actual reclamation and question creation/sending/answer application separately. Unknown reclamation and verification counts remain null. `capacity-result` binds the result to the exact active generation; the normal `done` command rejects a missing result. Internal immutable cycle receipts preserve prior dispatch references; the anonymous status response excludes that history. Shared-row summaries retain sixteen completed cycles. Queue failure is recorded separately and makes the task fail while retaining the measurements for a normal retry.

The runtime stores a checkpoint in `D:\CC\Tools\capacity-audit-3h\state.json` and aggregate receipts in `runs.jsonl`. The OS lock prevents concurrent checkpoint updates. Corrupt/empty state fails closed; the tool never treats it as a recovered original registry. State and log symlinks are rejected, and checkpoints are replaced through a synced temporary file.

```powershell
go test -race -count=1 ./...
go vet ./...
go build -trimpath -ldflags '-s -w -H=windowsgui' -o capacity-audit-3h.exe .
```

Tests cover boundaries, resumed traversal without duplicate counts, expired budgets, category rotation, checkpoint corruption, an outside-root checkpoint, a real Windows junction and the OS single-flight lock. The parent's Rust workspace test does not include this separate Go module; its Go checks above must be retained in the integrated validation record.

Set `CAPACITY_TODO_TEST_EXE` to the newly built ToDo executable to include the isolated cross-tool CLI acceptance. It verifies one normally dispatchable row, duplicate-input convergence and readback without rewriting scheduling state. Deployment requires a clean tracked source at the reviewed merged commit, committed build evidence and exact old/new fingerprints. Its two software-update qualifications belong to the shared guard owner (ToDo3082); this module does not update the guard or widen protected data access.
