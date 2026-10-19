# Worker scheduling repair

The combined source preserves the original reviewed changes for Cargo relay
admission, bounded repair execution, redundant question withdrawal and the
known product-ci distribution leaves. The legacy registry remains unrecovered.

## Process identity and unavailable metrics

System process birth metadata is read without additional process privileges.
The query is dynamically resolved, limited to eight attempts and sixteen MiB,
and validated against the current process's GetProcessTimes result. Only a
matching PID, parent and birth before and after Toolhelp supplements an unknown
birth. Image pointers and private process contents are never followed.

The public query and record-chain contract are documented by
[Microsoft](https://learn.microsoft.com/en-us/windows/win32/api/winternl/nf-winternl-ntquerysysteminformation).
The timing prefix is checked against the
[phnt ABI](https://github.com/winsiderss/systeminformer/blob/master/phnt/include/ntexapi.h)
and the current-process handle on every query. Unavailable or changed layouts
retain unknown identity. The tested installation is x64; x86 is unverified.

Birth metadata does not authorize termination. Cancellation retains its own
handle-based identity and access checks. A descendant whose CPU or memory
metrics cannot be read remains part of the process tree; its missing values
must not be treated as zero. Consequently, legacy CPU-based hang detection and
memory-footprint sampling conservatively defer for that tree. The operation
stall repair remains a separate worker-owned change. This prevents visible
subsets of a tree from being mistaken for complete resource measurements.

## Validation

The combined scheduler and ToDo source passed 385 nextest cases with no skips
and its doctests. The shared Go runner's combined-source tests use the real
Rust CLI. Native acceptance observed three continuously present processes
whose handles were inaccessible: the preceding scheduler falsely reported
Cargo busy while the repaired snapshot admitted a free build slot. No task
was launched by that read-only comparison. Official CI remains required before
deployment; the local acceptance is not a replacement for it.
