# capacity-audit-3h

This Go tool restores the existing `CC-CapacityAudit-3h` task. It **does not delete files**. The task runs every three hours without a console and uses elevated S4U logon, `StartWhenAvailable`, `IgnoreNew` and a five-minute execution limit.

Each run measures actual C/D volume free and used bytes and the change since the previous run. E is measured only to report whether its required 500 GiB margin remains; its directories are not scanned. The tool also measures the logical file lengths of the known pagefile, hibernation file, swapfile and Docker virtual disk without opening their contents.

Bounded metadata scans cover the known Windows/application directories. Owner data, machine data, operations and deletion-request trees require a verified negative result from the real client-resource guard. When the original protection settings are unavailable, these categories are recorded as `protected_unclassified`, not as zero size. No private path, file name, task text or raw error goes to stdout or the aggregate run log.

Each category has a 25-second / 150,000-entry budget and the whole run has a two-minute budget. A checkpoint preserves partial directory traversal between runs; the starting category rotates. The checkpoint cannot expand the compiled roots. Reparse points and paths with linked ancestors are skipped. An unavailable path, read error, partial scan, completed metadata scan and whole-drive scan are distinct: `whole_drive_scan` is always false. Reported category sizes are logical lengths, not disk allocation, and may include Windows hard links. A completed metadata traversal is not an atomic filesystem snapshot.

The runtime stores a checkpoint in `D:\CC\Tools\capacity-audit-3h\state.json` and aggregate receipts in `runs.jsonl`. The OS lock prevents concurrent checkpoint updates. Corrupt/empty state fails closed; the tool never treats it as a recovered original registry. State and log symlinks are rejected, and checkpoints are replaced through a synced temporary file.

```powershell
go test -race -count=1 ./...
go vet ./...
go build -trimpath -ldflags '-s -w -H=windowsgui' -o capacity-audit-3h.exe .
```

Tests cover boundaries, resumed traversal without duplicate counts, expired budgets, category rotation, checkpoint corruption, an outside-root checkpoint, a real Windows junction and the OS single-flight lock. The parent's Rust workspace test does not include this separate Go module; its Go checks above must be retained in the integrated validation record.
