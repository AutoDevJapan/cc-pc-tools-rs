# Watchdog software distribution

This finite extension belongs to the existing executable updater. It preserves
the current S4U/Limited task, its launcher action, triggers and settings.
It does not invoke the watchdog's install command, edit tasks, stop services,
grant general access, or turn a proxy health answer into process ownership.

## Current state

The source contains a controller, a native held-file transaction, a fixed
one-build evidence consumer, and owned tests. The normal update and build
issuers return fixed unavailable errors. No installation is enabled.
The existing two-tool update paths are unchanged.
Watchdog file acquisition uses its own ownership scope. Rejected partial pins,
acquired ancestors and temporary name-verification handles each receive one
explicit Close attempt. A failed Close stays referenced and is reported through
cleanup failure and reconciliation; later success does not clear that fact.

The exact predecessor's independently issued installation/build origin, current
file identity and maintenance purpose are still missing. They must be bound to
the same live target generation before opening its metadata or body. Unknown
installed bytes, an old hash, a caller path/hash, JSON, or a previously clear
guard verdict are not that origin. The normal issuer must also qualify the
candidate artifact and keep it held through the operation. The Guard-target
issuer is a separate target and is not reused.

## Build product

The intended command is the documented release build of one binary:

    cargo build --release --locked --manifest-path cc-ocx-watchdog/Cargo.toml --bin ocx_health_watchdog --message-format=json

It must run through the existing Cargo admission and owned-child collector once
after normal tool qualification is restored. This slice adds no workflow,
runner, scheduler, build retry, installed tool invocation or model call.

The producer seam claims a one-shot reservation, compares source, complete
compiler inputs, configuration and toolchain identities before/after, requires
exit zero with reaped child and complete streams, then consumes one nonfresh
bin artifact and final successful build-finished message. The same retained
body is checked for Windows x64 PE/GUI subsystem and complete length/hash, then
its full identity is rechecked. Cargo's executable string is comparison data;
it never selects a file to open. Failed partial products close both owned
resources once and retain a cleanup failure.

The compiled source pin is PR773's merged revision. Current main's later Guard
changes are separate. Selecting a later build revision requires an explicit
pin/source-closure delta and review; it does not silently rewrite this pin.
The normal Cargo-gate executor, complete input inventory, durable reservation
and product-to-update issuer are not connected by this candidate.

## Update state

The fixed production target is the existing watchdog executable. Stage,
backup and failed leaves are fixed siblings for this generation; collisions
are retained. The private owned-file fixture uses the same transaction.

1. Recheck the issued origin/artifact generation and inspect the held target.
2. Compare the current task's XML digest, S4U/Limited launcher definition and the
   two disabled watchdog tasks. A check operation performs no stop or write.
3. Stop and collect only the resident-owned watchdog instance and retain the
   launch hold. The adapter must prove exact task/process/image ownership and
   keep the PT1M trigger from restarting the old image during publication.
4. Create a stage without replacing an existing leaf, copy the same candidate
   body, sync, read back and copy the predecessor's owner/group/DACL.
5. Close the stage write handle once so Windows can map the executable. Reopen
   without write access and compare full identity, bytes and descriptor. This
   checked interval is not an atomic namespace lease against an uncooperative
   same-user writer.
6. Rename held old to backup, then held stage to public name. Both native calls
   have ReplaceIfExists=false. Recheck named objects, bodies and security.
7. Keep task XML unchanged, release the hold and observe the new watchdog task
   instance. Its own mapped image proof is independent of proxy health.
8. Require six increasing same-instance health_only samples spanning 50 seconds:
   healthy, no proxy identity/metrics, ineligible, zero failure/write counts and
   no termination/service events. The caller supplies no such evidence to the
   normal entry; the resident adapter must observe it.
9. On publish/postcheck/observation/cancellation failure, use a finite recovery
   context for this generation's undo. Reestablish the launch hold and prove
   the published owned instance reaped when necessary. Move only the same held
   candidate to the failed leaf, then return only the same held predecessor to
   an absent public name. Conflicting names and unknown stop state forbid undo.
10. Release the recovery hold, attempt every owned close once and preserve
    primary failure, publication, restoration and cleanup as separate facts.

A successful rollback does not make the update successful. Published remains
true when publication occurred, even after restoration. Unconfirmed cleanup or
a changed task remains a reconciliation requirement. Stage, backup and failed
files are not automatically deleted. The resident adapter's Close must release
a hold even after a partial StopAndHold or ReleaseHold failure and report any
unreaped child; a close error keeps the resource referenced for reconciliation.

The actual scheduler stop/hold/observe adapter is still unavailable, and task
XML is not a stop capability. A health_only sample also is not a stop grant.
The deferred native files adapter first holds a read-only baseline with delete
sharing, then acquires DELETE-capable transaction handles in Stage after the
controller's resident stop/hold. It compares both objects to the issued origin.
Opening a mapped executable with DELETE rights may fail; Check does not request
that right. The stopped native transaction is also used directly by the owned
nonrunning file fixtures. Actual task/image stop and launch remain untested.
The normal result always keeps data_access_granted and general_guard_changed
false. Check JSON is an observation and cannot be replayed as apply authority.

## Validation and remaining acceptance

Acceptance requires independent review and successful CI for the final source,
followed by one formal Windows build and distribution of that same artifact.
Normal issuers must bind the reviewed source, retained artifact and qualified
current target to the same update generation before installation. The scheduler
stop/hold/observe adapter must be connected, the existing S4U/Limited startup
task definition must remain unchanged, and the new watchdog instance must pass
its runtime observation after replacement. Source validation and CI do not
establish installed success or a runtime grant.

OpenCodex's representative CLI acceptance from Codex and Claude Code requires
normal qualification of the actual nested Bun. HTTP 200 is recorded separately.

References:

- [Cargo JSON messages](https://doc.rust-lang.org/cargo/reference/external-tools.html#json-messages)
- [FILE_RENAME_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info)
- [Go owned command collection](https://pkg.go.dev/os/exec#Cmd)
